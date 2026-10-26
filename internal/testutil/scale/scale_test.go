package scale

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
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
}
