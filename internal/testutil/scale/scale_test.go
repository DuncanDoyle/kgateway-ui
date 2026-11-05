package scale

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

func countKinds(objs []*unstructured.Unstructured) map[string]int {
	c := map[string]int{}
	for _, o := range objs {
		c[o.GetKind()]++
	}
	return c
}

func key(ns, name string) string { return ns + "/" + name }

// delegation maps each HTTPRoute to the HTTPRoutes its rules' backendRefs name.
func delegation(objs []*unstructured.Unstructured) map[string][]string {
	out := map[string][]string{}
	for _, o := range objs {
		if o.GetKind() != "HTTPRoute" {
			continue
		}
		k := key(o.GetNamespace(), o.GetName())
		out[k] = nil
		rules, _, _ := unstructured.NestedSlice(o.Object, "spec", "rules")
		for _, r := range rules {
			refs, _, _ := unstructured.NestedSlice(r.(map[string]any), "backendRefs")
			for _, ref := range refs {
				m := ref.(map[string]any)
				if m["kind"] == "HTTPRoute" {
					out[k] = append(out[k], key(m["namespace"].(string), m["name"].(string)))
				}
			}
		}
	}
	return out
}

// children counts the distinct HTTPRoutes some HTTPRoute delegates to.
func delegatedChildren(objs []*unstructured.Unstructured) int {
	seen := map[string]bool{}
	for _, cs := range delegation(objs) {
		for _, c := range cs {
			seen[c] = true
		}
	}
	return len(seen)
}

// maxDepth is the longest delegation path from a route no one delegates to.
func maxDepth(objs []*unstructured.Unstructured) int {
	g := delegation(objs)
	isChild := map[string]bool{}
	for _, cs := range g {
		for _, c := range cs {
			isChild[c] = true
		}
	}
	var depth func(string) int
	depth = func(k string) int {
		d := 0
		for _, c := range g[k] {
			d = max(d, 1+depth(c))
		}
		return d
	}
	best := 0
	for k := range g {
		if !isChild[k] {
			best = max(best, depth(k))
		}
	}
	return best
}

func maxFanOut(objs []*unstructured.Unstructured) int {
	best := 0
	for _, cs := range delegation(objs) {
		best = max(best, len(cs))
	}
	return best
}

func TestWorldMatchesTier(t *testing.T) {
	objs, nss := World(Large, 1), testutil.ScaleNamespaces(Large.Namespaces)
	c := countKinds(objs)
	if c["Gateway"] != 100 || c["ListenerSet"] != 200 || c["Backend"] != 200 || len(nss) != 100 {
		t.Fatalf("%v", c)
	}
	if got := c["HTTPRoute"]; got != 2000 {
		t.Fatalf("HTTPRoutes %d", got)
	}
	for _, k := range []string{"GRPCRoute", "TCPRoute", "TLSRoute"} {
		if c[k] != 100 {
			t.Fatalf("%s %d, want 5%% of 2000", k, c[k])
		}
	}
	if d := delegatedChildren(objs); d < 190 || d > 210 {
		t.Fatalf("delegated %d", d)
	}
	if maxDepth(objs) > 3 || maxFanOut(objs) > 50 {
		t.Fatal("delegation shape")
	}
	a, b := World(Large, 1), World(Large, 1)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("not deterministic")
	}
}

// Each tier reaches its depth and fan-out bounds exactly; only Extreme has
// the 8-layer lattice, whose depth is exempt from MaxDepth.
func TestWorldShapePerTier(t *testing.T) {
	for _, tier := range Tiers {
		t.Run(tier.Name, func(t *testing.T) {
			objs := World(tier, 1)
			c := countKinds(objs)
			if c["Gateway"] != tier.Gateways || c["ListenerSet"] != tier.ListenerSets || c["Backend"] != tier.Backends || c["HTTPRoute"] != tier.HTTPRoutes {
				t.Fatalf("counts %v", c)
			}
			want := int(math.Round(float64(tier.HTTPRoutes) * tier.DelegatedPct / 100))
			if d := delegatedChildren(objs); d != want {
				t.Fatalf("delegated %d, want %d", d, want)
			}
			wantDepth := max(tier.MaxDepth, tier.LatticeDepth)
			if d, f := maxDepth(objs), maxFanOut(objs); d != wantDepth || f != tier.MaxFanOut {
				t.Fatalf("depth %d fan-out %d, want %d and %d", d, f, wantDepth, tier.MaxFanOut)
			}
		})
	}
}

// Status follows the generated parentRefs: every non-delegated parentRef has
// an entry, exactly one in FailedEvery is NotAllowedByListeners, the mean
// parentRef count matches the tier, and every child has an Accepted entry
// per delegating parent.
func TestWorldStatus(t *testing.T) {
	objs := World(Large, 7)
	parents := map[string][]string{} // child → delegating parents
	for p, cs := range delegation(objs) {
		for _, c := range cs {
			parents[c] = append(parents[c], p)
		}
	}
	refs, failed, routes := 0, 0, 0
	backends := map[string]bool{}
	for _, o := range objs {
		switch o.GetKind() {
		case "HTTPRoute", "GRPCRoute", "TCPRoute", "TLSRoute":
		default:
			continue
		}
		k := key(o.GetNamespace(), o.GetName())
		var gwRefs int
		for _, p := range objects.RouteParentRefs(o) {
			if p.Kind != "HTTPRoute" {
				gwRefs++
			}
		}
		st := objects.RouteParents(o)
		if ps := parents[k]; len(ps) > 0 {
			if gwRefs != 0 || len(st) != len(ps) {
				t.Fatalf("child %s: %d gateway refs, %d entries for %d parents", k, gwRefs, len(st), len(ps))
			}
			for _, s := range st {
				if s.ParentRef.Kind != "HTTPRoute" || objects.FindCondition(s.Conditions, "Accepted") == nil {
					t.Fatalf("child %s entry %+v", k, s)
				}
			}
		} else {
			routes++
			refs += gwRefs
			if len(st) != gwRefs {
				t.Fatalf("%s: %d refs, %d entries", k, gwRefs, len(st))
			}
			for _, s := range st {
				if c := objects.FindCondition(s.Conditions, "Accepted"); c.Status == "False" {
					if c.Reason != "NotAllowedByListeners" {
						t.Fatalf("%s: %s", k, c.Reason)
					}
					failed++
				}
			}
		}
		rules, _, _ := unstructured.NestedSlice(o.Object, "spec", "rules")
		for _, r := range rules {
			brs, _, _ := unstructured.NestedSlice(r.(map[string]any), "backendRefs")
			for _, br := range brs {
				if m := br.(map[string]any); m["kind"] == "Backend" {
					backends[key(m["namespace"].(string), m["name"].(string))] = true
				}
			}
		}
	}
	if failed != refs/FailedEvery {
		t.Fatalf("failed %d of %d parentRefs, want %d", failed, refs, refs/FailedEvery)
	}
	if mean := float64(refs) / float64(routes); math.Abs(mean-Large.ParentRefsMean) > 0.05 {
		t.Fatalf("parentRefs mean %.3f, want %.1f", mean, Large.ParentRefsMean)
	}
	if len(backends) != Large.Backends {
		t.Fatalf("%d of %d Backends referenced", len(backends), Large.Backends)
	}
}

// Each Gateway has one Selector listener for the labelled namespaces, and
// those namespaces exist.
func TestWorldSelectorListeners(t *testing.T) {
	labelled := 0
	for _, n := range testutil.ScaleNamespaces(Typical.Namespaces) {
		if n.Labels["team"] == "web" {
			labelled++
		}
	}
	if labelled != Typical.Namespaces/4 {
		t.Fatalf("labelled namespaces %d", labelled)
	}
	for _, o := range World(Typical, 1) {
		if o.GetKind() != "Gateway" {
			continue
		}
		ls, _, _ := unstructured.NestedSlice(o.Object, "spec", "listeners")
		sel := 0
		for _, l := range ls {
			if from, _, _ := unstructured.NestedString(l.(map[string]any), "allowedRoutes", "namespaces", "from"); from == "Selector" {
				sel++
			}
		}
		if len(ls) != Typical.ListenersPerGateway || sel != 1 {
			t.Fatalf("%s: %d listeners, %d selector", o.GetName(), len(ls), sel)
		}
	}
}

// Different seeds give different worlds.
func TestWorldSeedMatters(t *testing.T) {
	if reflect.DeepEqual(World(Typical, 1), World(Typical, 2)) {
		t.Fatal("seed ignored")
	}
}

// paths lists the PathPrefix values of each HTTPRoute's rules that delegate
// (delegating) and of every rule (all), by route key.
func paths(objs []*unstructured.Unstructured) (delegating, all map[string][]string) {
	delegating, all = map[string][]string{}, map[string][]string{}
	for _, o := range objs {
		if o.GetKind() != "HTTPRoute" {
			continue
		}
		k := key(o.GetNamespace(), o.GetName())
		rules, _, _ := unstructured.NestedSlice(o.Object, "spec", "rules")
		for _, r := range rules {
			rm := r.(map[string]any)
			ms, _, _ := unstructured.NestedSlice(rm, "matches")
			refs, _, _ := unstructured.NestedSlice(rm, "backendRefs")
			deleg := slices.ContainsFunc(refs, func(b any) bool { return b.(map[string]any)["kind"] == "HTTPRoute" })
			for _, m := range ms {
				v, _, _ := unstructured.NestedString(m.(map[string]any), "path", "value")
				all[k] = append(all[k], v)
				if deleg {
					delegating[k] = append(delegating[k], v)
				}
			}
		}
	}
	return delegating, all
}

// Every match a delegator hands a child fits one of the child's matches, and
// every child match fits one incoming match, so view B attaches every child
// under every context (PF2). Under Contradict the child entries are gone and
// each delegating parent reports ResolvedRefs=False/BackendNotFound.
func TestWorldChildrenFitEveryDelegator(t *testing.T) {
	for _, tier := range []Tier{Typical, Large, Extreme} {
		t.Run(tier.Name, func(t *testing.T) {
			objs := World(tier, 1)
			deleg, all := paths(objs)
			incoming := map[string][]string{}
			for p, cs := range delegation(objs) {
				for _, c := range cs {
					incoming[c] = append(incoming[c], deleg[p]...)
				}
			}
			if len(incoming) == 0 {
				t.Fatal("no children")
			}
			// The fit of rules §6: a raw string prefix under a PathPrefix parent.
			fits := func(parent, child string) bool { return strings.HasPrefix(child, parent) }
			for c, in := range incoming {
				for _, m := range in {
					if !slices.ContainsFunc(all[c], func(cm string) bool { return fits(m, cm) }) {
						t.Fatalf("%s: no match fits incoming %s (has %v)", c, m, all[c])
					}
				}
				uniq := slices.Compact(slices.Sorted(slices.Values(in)))
				for _, cm := range all[c] {
					n := 0
					for _, m := range uniq {
						if fits(m, cm) {
							n++
						}
					}
					if n != 1 {
						t.Fatalf("%s: match %s fits %d incoming matches", c, cm, n)
					}
				}
			}
			byKey := map[string]*unstructured.Unstructured{}
			for _, o := range World(tier.Contradicting(), 1) {
				if o.GetKind() == "HTTPRoute" {
					byKey[key(o.GetNamespace(), o.GetName())] = o
				}
			}
			for c := range incoming {
				for _, s := range objects.RouteParents(byKey[c]) {
					if s.ParentRef.Kind == "HTTPRoute" {
						t.Fatalf("contradicting %s keeps child entry %+v", c, s.ParentRef)
					}
				}
			}
			for p := range deleg {
				for _, s := range objects.RouteParents(byKey[p]) {
					if rr := objects.FindCondition(s.Conditions, "ResolvedRefs"); rr == nil || rr.Status != "False" || rr.Reason != "BackendNotFound" {
						t.Fatalf("contradicting parent %s: ResolvedRefs %+v", p, rr)
					}
				}
			}
			for _, o := range objs {
				if _, ok := deleg[key(o.GetNamespace(), o.GetName())]; ok && o.GetKind() == "HTTPRoute" {
					for _, s := range objects.RouteParents(o) {
						if rr := objects.FindCondition(s.Conditions, "ResolvedRefs"); rr == nil || rr.Status != "True" {
							t.Fatalf("consistent parent %s: ResolvedRefs %+v", o.GetName(), rr)
						}
					}
				}
			}
		})
	}
}

// DenseSelection: every parent selects the whole namespace and every route
// fits every parent's match.
func TestDenseSelection(t *testing.T) {
	objs := DenseSelection(3, 5)
	c := countKinds(objs)
	if c["HTTPRoute"] != 8 || c["Gateway"] != 1 || c["GatewayClass"] != 1 {
		t.Fatalf("%v", c)
	}
	deleg, all := paths(objs)
	if len(deleg) != 3 {
		t.Fatalf("parents %v", deleg)
	}
	for _, o := range objs {
		if o.GetKind() != "HTTPRoute" {
			continue
		}
		k := key(o.GetNamespace(), o.GetName())
		if _, parent := deleg[k]; parent {
			rules, _, _ := unstructured.NestedSlice(o.Object, "spec", "rules")
			ref := rules[0].(map[string]any)["backendRefs"].([]any)[0].(map[string]any)
			if ref["name"] != "*" || ref["namespace"] != DenseNamespace || len(objects.RouteParentRefs(o)) != 1 {
				t.Fatalf("parent %s: ref %v", k, ref)
			}
			continue
		}
		if o.GetNamespace() != DenseNamespace || len(objects.RouteParentRefs(o)) != 0 || len(objects.RouteParents(o)) != 3 {
			t.Fatalf("child %s", k)
		}
		for _, ps := range deleg {
			if !strings.HasPrefix(all[k][0], ps[0]) {
				t.Fatalf("child %s %v does not fit %v", k, all[k], ps)
			}
		}
	}
	if !reflect.DeepEqual(objs, DenseSelection(3, 5)) {
		t.Fatal("not deterministic")
	}
	// A route's status keeps the first 32 parents' entries, the Gateway API MaxItems.
	for _, o := range DenseSelection(40, 1) {
		if o.GetKind() != "HTTPRoute" || o.GetNamespace() != DenseNamespace {
			continue
		}
		ps := objects.RouteParents(o)
		if len(ps) != 32 || ps[0].ParentRef.Name != "p-000" || ps[31].ParentRef.Name != "p-031" {
			t.Fatalf("child %s has %d status entries", o.GetName(), len(ps))
		}
	}
}

// phase3Counts are the Phase 3 columns of the design's tier table, in the
// order services, referenced, slices, pods per Gateway, events, CRDs.
var phase3Counts = map[string][6]int{
	"typical": {600, 300, 700, 2, 50, 10},
	"large":   {5000, 2000, 6000, 2, 1000, 10},
	"extreme": {25000, 10000, 30000, 3, 10000, 10},
}

func TestTierPhase3Counts(t *testing.T) {
	for _, tier := range Tiers {
		w := phase3Counts[tier.Name]
		got := [6]int{tier.Services, tier.ReferencedServices, tier.EndpointSlices, tier.ProxyPodsPerGateway, tier.WarningEvents, tier.GatewayAPICRDs}
		if got != w {
			t.Errorf("%s: %v, want %v", tier.Name, got, w)
		}
	}
}

// restarts is the total container restart count of pods.
func restarts(pods []*corev1.Pod) (n int) {
	for _, p := range pods {
		for _, cs := range p.Status.ContainerStatuses {
			n += int(cs.RestartCount)
		}
	}
	return n
}

// unready counts referenced Services whose every EndpointSlice endpoint is not ready.
func unready(t *testing.T, tier Tier, svcs []*corev1.Service, slices []*discoveryv1.EndpointSlice) (n int) {
	t.Helper()
	ready := map[string]bool{}
	for _, es := range slices {
		k := es.Namespace + "/" + es.Labels[discoveryv1.LabelServiceName]
		for _, e := range es.Endpoints {
			if e.Conditions.Ready != nil && *e.Conditions.Ready {
				ready[k] = true
			}
		}
	}
	for _, s := range svcs {
		if s.Labels[testutil.ScaleReferencedLabel] == "true" && !ready[s.Namespace+"/"+s.Name] {
			n++
		}
	}
	return n
}

func TestPhase3Shapes(t *testing.T) {
	for _, tier := range Tiers {
		if tier.Name == "extreme" && testing.Short() {
			continue
		}
		t.Run(tier.Name, func(t *testing.T) {
			pods, events := testutil.ScaleProxyPods(tier.Shape(), 1), testutil.ScaleEvents(tier.Shape(), 1)
			svcs, slices := testutil.ScaleServices(tier.Shape(), 1), testutil.ScaleEndpointSlices(tier.Shape(), 1)
			if len(pods) != tier.Gateways*tier.ProxyPodsPerGateway || len(events) != tier.WarningEvents ||
				len(svcs) != tier.Services || len(slices) != tier.EndpointSlices || len(testutil.ScaleCRDs(tier.Shape())) != tier.GatewayAPICRDs {
				t.Fatalf("pods %d events %d services %d slices %d", len(pods), len(events), len(svcs), len(slices))
			}
			ref := 0
			for _, s := range svcs {
				if s.Labels[testutil.ScaleReferencedLabel] == "true" {
					ref++
				}
			}
			if ref != tier.ReferencedServices {
				t.Fatalf("referenced %d", ref)
			}
			for _, e := range events {
				if e.Type != corev1.EventTypeWarning {
					t.Fatalf("event type %s", e.Type)
				}
			}
			if restarts(pods) != 0 || unready(t, tier, svcs, slices) != 0 {
				t.Fatal("base tier has restarts or unready services")
			}
			if !reflect.DeepEqual(pods, testutil.ScaleProxyPods(tier.Shape(), 1)) || !reflect.DeepEqual(events, testutil.ScaleEvents(tier.Shape(), 1)) ||
				!reflect.DeepEqual(slices, testutil.ScaleEndpointSlices(tier.Shape(), 1)) || !reflect.DeepEqual(testutil.ScaleMetrics(tier.Shape(), 1), testutil.ScaleMetrics(tier.Shape(), 1)) {
				t.Fatal("not deterministic")
			}
			if reflect.DeepEqual(events, testutil.ScaleEvents(tier.Shape(), 2)) || reflect.DeepEqual(pods, testutil.ScaleProxyPods(tier.Shape(), 2)) ||
				reflect.DeepEqual(testutil.ScaleMetrics(tier.Shape(), 1), testutil.ScaleMetrics(tier.Shape(), 2)) {
				t.Fatal("seed ignored")
			}
		})
	}
}

func TestWorstCase(t *testing.T) {
	for _, base := range Tiers {
		if base.Name == "extreme" && testing.Short() {
			continue
		}
		t.Run(base.Name, func(t *testing.T) {
			tier := base.Worst()
			if !tier.IsWorst || base.IsWorst || tier.Name != base.Name+"-worst" {
				t.Fatalf("%+v", tier)
			}
			if got, want := restarts(testutil.ScaleProxyPods(tier.Shape(), 1)), (tier.Gateways+3)*tier.ProxyPodsPerGateway; got != want {
				t.Errorf("restarts %d", got)
			}
			if got, want := unready(t, tier, testutil.ScaleServices(tier.Shape(), 1), testutil.ScaleEndpointSlices(tier.Shape(), 1)), tier.ReferencedServices/testutil.ScaleNoReadyEvery; got != want {
				t.Errorf("unready referenced %d, want %d (5%%)", got, want)
			}
			m, err := ctrlmetrics.Parse(strings.NewReader(string(testutil.ScaleMetrics(tier.Shape(), 1))))
			if err != nil {
				t.Fatal(err)
			}
			if len(m.Rejects) != tier.Gateways+3 {
				t.Fatalf("rejecting Gateways %d, want %d", len(m.Rejects), tier.Gateways+3)
			}
			for gw, v := range m.Rejects {
				if v < 1 {
					t.Errorf("%v not rejecting", gw)
				}
			}
		})
	}
}

func TestLivePhase3Seeds(t *testing.T) {
	w := LivePhase3(t, Typical, func() time.Time { return testutil.Now })
	if len(w.CRDs) != Typical.GatewayAPICRDs || len(w.Metrics) == 0 {
		t.Fatalf("crds %d metrics %d", len(w.CRDs), len(w.Metrics))
	}
	if got := serviceRefs(w.Routes); len(got) != 0 {
		t.Fatalf("the 3.2 world names %d Services", len(got))
	}
	if Live(t, Typical, func() time.Time { return testutil.Now }).Metrics != nil {
		t.Fatal("Live seeds Phase 3")
	}
}

// serviceRefs is the set of Service names the routes' backendRefs name, by namespace/name.
func serviceRefs(routes []*unstructured.Unstructured) map[string]bool {
	out := map[string]bool{}
	for _, r := range routes {
		rules, _, _ := unstructured.NestedSlice(r.Object, "spec", "rules")
		for _, rule := range rules {
			refs, _ := rule.(map[string]any)["backendRefs"].([]any)
			for _, ref := range refs {
				if m := ref.(map[string]any); m["kind"] == "Service" {
					if m["namespace"] != r.GetNamespace() || m["port"] == nil {
						panic("service ref outside the route namespace or without a port")
					}
					out[r.GetNamespace()+"/"+m["name"].(string)] = true
				}
			}
		}
	}
	return out
}

func referencedSet(tier Tier) map[string]bool {
	out := map[string]bool{}
	for _, s := range testutil.ScaleServices(tier.Shape(), 1) {
		if s.Labels[testutil.ScaleReferencedLabel] == "true" {
			out[s.Namespace+"/"+s.Name] = true
		}
	}
	return out
}

// Every referenced Service is named by a route backendRef and no other is.
func TestReferenceServicesMatchesReferenced(t *testing.T) {
	for _, tier := range Tiers {
		if tier.Name == "extreme" && testing.Short() {
			continue
		}
		t.Run(tier.Name, func(t *testing.T) {
			var routes []*unstructured.Unstructured
			for _, o := range World(tier, 1) {
				if o.GetKind() == "HTTPRoute" {
					routes = append(routes, o)
				}
			}
			if len(serviceRefs(routes)) != 0 {
				t.Fatal("World names Services")
			}
			referenceServices(tier, routes)
			if got, want := serviceRefs(routes), referencedSet(tier); !reflect.DeepEqual(got, want) {
				t.Fatalf("named %d Services, referenced %d", len(got), len(want))
			}
		})
	}
}

// The worst case adds Gateways over 63 characters, two sharing a 50-character prefix.
func TestWorstLongGateways(t *testing.T) {
	if len(testutil.ScaleLongGatewayObjects(Typical.Shape())) != 0 {
		t.Fatal("base tier has long Gateways")
	}
	tier := Typical.Worst()
	gws := testutil.ScaleLongGatewayObjects(tier.Shape())
	if len(gws) != 3 {
		t.Fatalf("%d long Gateways", len(gws))
	}
	labels := map[string]bool{}
	for _, g := range gws {
		if len(g.GetName()) <= 63 {
			t.Fatalf("%s is not over 63", g.GetName())
		}
		labels[testutil.SafeGatewayLabelValue(g.GetName())] = true
	}
	if gws[0].GetName()[:50] != gws[1].GetName()[:50] || len(labels) != 3 {
		t.Fatal("two Gateways must share a prefix and still have distinct label values")
	}
	found := map[string]int{}
	for _, p := range testutil.ScaleProxyPods(tier.Shape(), 1) {
		l := p.Labels["gateway.networking.k8s.io/gateway-name"]
		if len(l) > 63 {
			t.Fatalf("label %q over 63", l)
		}
		if labels[l] {
			found[l]++
		}
	}
	for l := range labels {
		if found[l] != tier.ProxyPodsPerGateway {
			t.Errorf("label %q has %d pods", l, found[l])
		}
	}
}

// LivePhase3 runs the proxy Pod, Event and CRD watches, and the process and
// proxy stats sources select every Gateway's Pods, long-named ones included,
// each on its own Pod IP; Events carry their first and last times.
func TestLivePhase3ProxiesSelected(t *testing.T) {
	tier := Typical.Worst()
	w := LivePhase3(t, tier, func() time.Time { return testutil.Now })
	snap := w.Cache.Snapshot()
	if len(snap.ProxyPods) != (tier.Gateways+3)*tier.ProxyPodsPerGateway || len(snap.CRDs) != tier.GatewayAPICRDs {
		t.Fatalf("proxy Pods %d, CRDs %d", len(snap.ProxyPods), len(snap.CRDs))
	}
	in, _ := install.UpstreamDetector{}.Detect(testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1))
	res := install.Resolved{Install: in}
	ips := map[string]bool{}
	gws := append(testutil.ScaleLongGatewayObjects(tier.Shape()), World(tier, 1)...)
	n := 0
	for _, gw := range gws {
		if gw.GetKind() != "Gateway" {
			continue
		}
		n++
		ref := objects.RefOf(gw)
		set := process.Kube{}.Proxies(res, ref, snap)
		targets, ok := proxystats.Kube{}.Targets(res, ref, snap)
		if !set.Observed || len(set.Processes) != tier.ProxyPodsPerGateway || !ok || len(targets) != tier.ProxyPodsPerGateway {
			t.Fatalf("%v: processes %+v, targets %v", ref, set, targets)
		}
		for _, tg := range targets {
			ips[tg.Addr] = true
		}
	}
	if n != tier.Gateways+3 || len(ips) != n*tier.ProxyPodsPerGateway {
		t.Fatalf("%d Gateways, %d distinct Pod IPs", n, len(ips))
	}
	for _, e := range testutil.ScaleEvents(tier.Shape(), 1) {
		if e.FirstTimestamp.IsZero() || e.LastTimestamp.IsZero() {
			t.Fatalf("event %s without its times", e.Name)
		}
	}
}
