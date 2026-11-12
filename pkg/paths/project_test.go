package paths_test

import (
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/internal/testutil/scale"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/computed/rulesets"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
	"github.com/DuncanDoyle/snellius/pkg/scopeverify"
)

var gwEdge = testutil.GwRef("infra", "edge")

// endpointsSnap is worldSnap with Services and EndpointSlices watched and holding svcs and eps.
func endpointsSnap(t *testing.T, svcs []cache.ServiceFact, eps []cache.SliceFact, objs ...*unstructured.Unstructured) *cache.Snapshot {
	t.Helper()
	s := worldSnap(t, "v2.4.7", nil, objs...)
	s.Access[cache.ServicesGVR], s.Access[cache.EndpointSlicesGVR] = cache.Access{OK: true}, cache.Access{OK: true}
	s.Services, s.EndpointSlices = svcs, eps
	return s
}

// project runs the engine over snap, adds extra findings to its only
// install's report, and projects its topology with endpoints capability cap.
func project(t *testing.T, snap *cache.Snapshot, cap string, extra ...health.Finding) (*paths.Topology, *paths.Projection, *engine.Model) {
	t.Helper()
	return projectAt(t, snap, cap, []time.Duration{0}, extra...)
}

// projectAt is project after one engine computation at each offset from
// testutil.Now, projecting the last.
func projectAt(t *testing.T, snap *cache.Snapshot, cap string, at []time.Duration, extra ...health.Finding) (*paths.Topology, *paths.Projection, *engine.Model) {
	t.Helper()
	return projectWith(t, snap, cap, at, nil, nil, extra...)
}

// projectWith is projectAt with the engine's rule selection rulesFor (nil
// for the real one) and mutate applied to the install's model before projecting.
func projectWith(t *testing.T, snap *cache.Snapshot, cap string, at []time.Duration, rulesFor func(string) computed.Selection,
	mutate func(*engine.InstallModel), extra ...health.Finding) (*paths.Topology, *paths.Projection, *engine.Model) {
	t.Helper()
	now := testutil.Now
	e := engine.New(engine.Config{Snapshots: staticSnap{snap}, Served: testutil.ServedUpstream(), Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour), Checks: health.Upstream(),
		Now: func() time.Time { return now }, RulesFor: rulesFor})
	var m *engine.Model
	for _, d := range at {
		now = testutil.Now.Add(d)
		m = e.Compute()
	}
	if len(m.Installs) != 1 {
		t.Fatalf("installs: %d", len(m.Installs))
	}
	i := &m.Installs[0]
	i.Health.Findings = append(i.Health.Findings, extra...)
	if mutate != nil {
		mutate(i)
	}
	top := paths.Build(paths.Input{Index: i.Index, Views: i.Computed, Decode: objects.Direct{}, Delegates: i.Rules.Delegator() != nil})
	p := paths.Project(top, paths.ProjectInput{Model: i, Next: m.Next, Snapshot: m.Snapshot, Now: now, EndpointsCap: cap})
	if len(p.Problems) != len(top.Rows) || len(p.Shares) != len(top.Rows) || len(p.Endpoints) != len(top.Rows) || len(p.Order) != len(top.Rows) {
		t.Fatalf("projection not aligned with %d rows", len(top.Rows))
	}
	return top, p, m
}

// matured are offsets past KGWUI-023's hold.
var matured = []time.Duration{0, health.EndpointsHold + time.Second}

// cart is Service shop/cart with ports, and its one slice with ready counts per port.
func cart(ports []cache.ServicePort, ready []int, total int) ([]cache.ServiceFact, []cache.SliceFact) {
	sp := make([]cache.SlicePort, len(ports))
	for i, p := range ports {
		sp[i] = cache.SlicePort{Name: p.Name, Port: p.Target}
	}
	return []cache.ServiceFact{{Namespace: "shop", Name: "cart", Type: "ClusterIP", Ports: ports}},
		[]cache.SliceFact{{Namespace: "shop", Service: "cart", Ports: sp, Ready: ready, Total: total, AddressType: "IPv4"}}
}

var httpPort = cache.ServicePort{Name: "http", Port: 8080, Target: 8080, Protocol: "TCP"}

// accepted is shop/name on infra/edge, accepted with resolved refs, with one rule holding refs.
func accepted(name string, refs ...any) *unstructured.Unstructured {
	return testutil.WithRules(testutil.HTTPRoute("shop", name, []objects.ParentRef{gwEdge}, testutil.AcceptedBy(gwEdge, testutil.Controller)), rule(refs))
}

// port is a backendRef to Service shop/name on port p.
func port(name string, p int64) map[string]any { return map[string]any{"name": name, "port": p} }

// unresolved is the status.parents entry of a route accepted on p whose refs do not resolve.
func unresolved(p objects.ParentRef) testutil.RouteParent {
	return testutil.RouteParent{Ref: p, Controller: testutil.Controller, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound"),
		testutil.Cond("kgateway.dev/Programmed", metav1.ConditionTrue, "Programmed")}}
}

func hasSignal(p paths.Problem, kind string, state health.State) bool {
	return slices.ContainsFunc(p.Signals, func(s paths.Signal) bool { return s.Kind == kind && s.State == state })
}

// A zero-ready port is unknown while KGWUI-023 holds, never healthy, and
// takes 023's warning once it matures; the counts alone never make an error.
func TestProjectZeroReadyFollows023(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{0}, 2)
	snap := endpointsSnap(t, svcs, eps, edge("http"), accepted("r", svc("cart")))
	_, p, m := project(t, snap, "on")
	pr := p.Problems[0]
	if pr.State != health.StateUnknown || !slices.ContainsFunc(pr.Signals, func(s paths.Signal) bool {
		return s.Kind == paths.SignalWaiting && s.Code == health.CodeBackendNoReadyEndpoints
	}) {
		t.Fatalf("held: problem %+v", pr)
	}
	ep := p.Endpoints[0]
	if ep == nil || ep.Complete != "complete" || ep.Ready == nil || *ep.Ready != 0 || *ep.Total != 2 || ep.Port != 8080 || ep.PortName != "http" ||
		!ep.ReadAt.Equal(m.Snapshot.TakenAt) {
		t.Fatalf("endpoints %+v", ep)
	}
	if p.Next != m.Next || m.Next.IsZero() {
		t.Fatalf("next %+v, want the publication's %+v", p.Next, m.Next)
	}
	_, p, _ = projectAt(t, snap, "on", matured)
	if pr := p.Problems[0]; pr.State != health.StateWarning || !slices.ContainsFunc(pr.Signals, func(s paths.Signal) bool {
		return s.Kind == paths.SignalFinding && s.Code == health.CodeBackendNoReadyEndpoints
	}) {
		t.Fatalf("matured: problem %+v", pr)
	}
}

// A listener that is not programmed makes its rows errors, healthy backend or not.
func TestProjectListenerNotProgrammed(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	gw := testutil.Gateway("infra", "edge", "kgateway", nil, testutil.Listener{Name: "http", Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionFalse, "Invalid")}})
	_, p, _ := project(t, endpointsSnap(t, svcs, eps, gw, accepted("r", svc("cart"))), "on")
	noEndpoints := func(s paths.Signal) bool { return s.Code == health.CodeBackendNoReadyEndpoints }
	if pr := p.Problems[0]; pr.State != health.StateError || !hasSignal(pr, "listener", health.StateError) || slices.ContainsFunc(pr.Signals, noEndpoints) {
		t.Fatalf("problem %+v", pr)
	}
}

// A listener's signals name it: on a healthy Gateway, a listener whose
// Programmed is missing is waiting, and the signal says which listener; a
// Gateway's own signals name none.
func TestProjectListenerSignalsNameListener(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	gw := testutil.Gateway("infra", "edge", "kgateway", nil, testutil.Listener{Name: "http", Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")}})
	_, p, _ := project(t, endpointsSnap(t, svcs, eps, gw, accepted("r", svc("cart"))), "on")
	pr := p.Problems[0]
	if pr.State != health.StateUnknown || !slices.ContainsFunc(pr.Signals, func(s paths.Signal) bool {
		return s.Kind == paths.SignalWaiting && s.Code == "Programmed" && s.Listener == "http"
	}) {
		t.Fatalf("problem %+v", pr)
	}
	gw = testutil.Gateway("infra", "edge", "kgateway", []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")}, testutil.Listener{Name: "http", Conds: ready})
	_, p, _ = project(t, endpointsSnap(t, svcs, eps, gw, accepted("r", svc("cart"))), "on")
	if pr := p.Problems[0]; !slices.ContainsFunc(pr.Signals, func(s paths.Signal) bool {
		return s.Kind == paths.SignalWaiting && s.Code == "Programmed" && s.Listener == ""
	}) {
		t.Fatalf("gateway signal %+v", pr)
	}
}

// With one of two Service ports ready, only the other port's rows have a
// problem, and each row counts its port as KGWUI-023 does.
func TestProjectPerPort(t *testing.T) {
	grpc := cache.ServicePort{Name: "grpc", Port: 9000, Target: 9000, Protocol: "TCP"}
	svcs, eps := cart([]cache.ServicePort{httpPort, grpc}, []int{2, 0}, 2)
	snap := endpointsSnap(t, svcs, eps, edge("http"), accepted("r", port("cart", 8080), port("cart", 9000)))
	ss := []*cache.SliceFact{&eps[0]}
	for _, c := range []struct {
		at    []time.Duration
		empty health.State
	}{{[]time.Duration{0}, health.StateUnknown}, {matured, health.StateWarning}} {
		top, p, _ := projectAt(t, snap, "on", c.at)
		for i, r := range top.Rows {
			b, _ := top.Backend(r)
			want, anyReady := health.PortEndpoints(&svcs[0], ss, b.Port)
			ep := p.Endpoints[i]
			if ep == nil || ep.Ready == nil || *ep.Ready != want.Ready || *ep.Total != want.Total || ep.PortName != want.PortName {
				t.Fatalf("port %d: endpoints %+v, want %+v", b.Port, ep, want)
			}
			if st := p.Problems[i].State; anyReady && st != health.StateHealthy || !anyReady && st != c.empty {
				t.Fatalf("at %v port %d: state %s", c.at, b.Port, st)
			}
		}
	}
}

// Endpoints off, denied or failing make a Service row unknown, never healthy, with no count.
func TestProjectEndpointsCoverage(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	for _, c := range []struct{ cap, complete string }{{"off", "unavailable"}, {"denied", "failed"}, {"failing", "failed"}} {
		t.Run(c.cap, func(t *testing.T) {
			_, p, _ := project(t, endpointsSnap(t, svcs, eps, edge("http"), accepted("r", svc("cart"))), c.cap)
			pr, ep := p.Problems[0], p.Endpoints[0]
			if pr.State != health.StateUnknown || !hasSignal(pr, "coverage", health.StateUnknown) {
				t.Fatalf("problem %+v", pr)
			}
			if ep == nil || ep.Ready != nil || ep.Total != nil || ep.Complete != c.complete {
				t.Fatalf("endpoints %+v", ep)
			}
		})
	}
}

// A route with no status for this parent is waiting for the controller.
func TestProjectNoStatusIsWaiting(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	r := testutil.WithRules(testutil.HTTPRoute("shop", "r", []objects.ParentRef{gwEdge}), rule([]any{svc("cart")}))
	_, p, _ := project(t, endpointsSnap(t, svcs, eps, edge("http"), r), "on")
	if pr := p.Problems[0]; pr.State != health.StateUnknown || !hasSignal(pr, "waiting", health.StateUnknown) {
		t.Fatalf("problem %+v", pr)
	}
}

// A status older than the route's generation is waiting, not its stale error.
func TestProjectStaleStatusIsWaiting(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	r := testutil.WithRules(testutil.HTTPRoute("shop", "r", []objects.ParentRef{gwEdge}, unresolved(gwEdge)), rule([]any{svc("cart")}))
	r.SetGeneration(2)
	_, p, _ := project(t, endpointsSnap(t, svcs, eps, edge("http"), r), "on")
	if pr := p.Problems[0]; pr.State != health.StateUnknown || !hasSignal(pr, "waiting", health.StateUnknown) {
		t.Fatalf("problem %+v", pr)
	}
}

// An xDS reject on the Gateway is an error even when endpoints are unknown.
func TestProjectRejectBeatsUnknown(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	edgeRef := gwEdge.Ref()
	reject := health.Finding{Code: health.CodeXDSRejected, Severity: health.SeverityError, Resource: &edgeRef, Message: "Proxies reject this Gateway's config"}
	_, p, _ := project(t, endpointsSnap(t, svcs, eps, edge("http"), accepted("r", svc("cart"))), "off", reject)
	pr := p.Problems[0]
	if pr.State != health.StateError || !hasSignal(pr, "coverage", health.StateUnknown) ||
		!slices.ContainsFunc(pr.Signals, func(s paths.Signal) bool { return s.Code == health.CodeXDSRejected && s.Ref == edgeRef }) {
		t.Fatalf("problem %+v", pr)
	}
}

// An ExternalName Service has no endpoints to count.
func TestProjectExternalName(t *testing.T) {
	svcs := []cache.ServiceFact{{Namespace: "shop", Name: "cart", Type: "ExternalName"}}
	_, p, _ := project(t, endpointsSnap(t, svcs, nil, edge("http"), accepted("r", svc("cart"))), "on")
	if ep := p.Endpoints[0]; ep == nil || ep.Complete != "not-applicable" || ep.Ready != nil {
		t.Fatalf("endpoints %+v", ep)
	}
	if pr := p.Problems[0]; pr.State != health.StateHealthy {
		t.Fatalf("problem %+v", pr)
	}
}

// A ResolvedRefs failure attributed to ref b fails only b's share and row;
// unattributed, both refs may fail.
func TestProjectRefAttribution(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	a := append(svcs, cache.ServiceFact{Namespace: "shop", Name: "b", Type: "ClusterIP", Ports: []cache.ServicePort{httpPort}})
	eps = append(eps, cache.SliceFact{Namespace: "shop", Service: "b", Ports: []cache.SlicePort{{Name: "http", Port: 8080}}, Ready: []int{1}, Total: 1})
	route := func() *unstructured.Unstructured {
		return testutil.WithRules(testutil.HTTPRoute("shop", "r", []objects.ParentRef{gwEdge}, unresolved(gwEdge)), rule([]any{svc("cart"), svc("b")}))
	}
	// Service b is missing: KGWUI-013 attributes the failure to it.
	_, p, _ := project(t, endpointsSnap(t, svcs, eps, edge("http"), route()), "on")
	if p.Shares[0].State != paths.ShareShown || p.Shares[1].State != paths.ShareFailing || p.Shares[1].Percent != 50 {
		t.Fatalf("attributed shares %+v", p.Shares)
	}
	if p.Problems[0].State != health.StateHealthy || p.Problems[1].State != health.StateError ||
		!slices.ContainsFunc(p.Problems[1].Signals, func(s paths.Signal) bool { return s.Code == health.CodeRefUnresolved }) {
		t.Fatalf("attributed problems %+v", p.Problems)
	}
	// Both Services exist: the controller's reason names no ref.
	_, p, _ = project(t, endpointsSnap(t, a, eps, edge("http"), route()), "on")
	if p.Shares[0].State != paths.ShareMayFail || p.Shares[1].State != paths.ShareMayFail {
		t.Fatalf("unattributed shares %+v", p.Shares)
	}
}

// A Backend with Accepted=False is an error from KGWUI-002; it has no endpoints.
func TestProjectBackendNotAccepted(t *testing.T) {
	be := testutil.Backend("shop", "ext", []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "Invalid")})
	ref := map[string]any{"group": "gateway.kgateway.dev", "kind": "Backend", "name": "ext"}
	_, p, _ := project(t, endpointsSnap(t, nil, nil, edge("http"), be, accepted("r", ref)), "on")
	pr := p.Problems[0]
	if pr.State != health.StateError || !slices.ContainsFunc(pr.Signals, func(s paths.Signal) bool {
		return s.Code == health.CodeConditionFalse && s.Ref == objects.RefOf(be)
	}) {
		t.Fatalf("problem %+v", pr)
	}
	if ep := p.Endpoints[0]; ep == nil || ep.Complete != "not-applicable" {
		t.Fatalf("endpoints %+v", ep)
	}
}

// A delegated child that view C reaches only under unknown contexts is unknown.
func TestProjectDelegationUnknown(t *testing.T) {
	unread := corev1.EnvVar{Name: install.WildcardEnv, ValueFrom: &corev1.EnvVarSource{
		ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "cm"}, Key: "k"}}}
	labelled := func(u *unstructured.Unstructured) *unstructured.Unstructured {
		u.SetLabels(map[string]string{objects.DelegationLabelKey: "team"})
		return u
	}
	elsewhere := labelled(testutil.WithRules(testutil.HTTPRoute("other", "x", nil), prefixes([]string{"/api/x"}, svc("x"))))
	plain := child("plain", false, prefixes([]string{"/api/a"}, svc("cart")))
	api := testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{gwEdge}, testutil.AcceptedBy(gwEdge, testutil.Controller)),
		prefixes([]string{"/api"}, testutil.DelegateLabel("shop", "team"), testutil.DelegateTo("shop", "plain")))
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	snap := worldSnap(t, "v2.5.0", []corev1.EnvVar{unread}, edge("http"), api, labelled(child("cart", false, prefixes([]string{"/api/cart"}, svc("cart")))), elsewhere, plain)
	snap.Access[cache.ServicesGVR], snap.Access[cache.EndpointSlicesGVR] = cache.Access{OK: true}, cache.Access{OK: true}
	snap.Services, snap.EndpointSlices = svcs, eps
	top, p, _ := project(t, snap, "on")
	n := 0
	for i, r := range top.Rows {
		if c := top.Chain(r); len(c) == 2 && c[1] == route("shop", "plain") {
			n++
			if pr := p.Problems[i]; pr.State != health.StateUnknown || !hasSignal(pr, "delegation", health.StateUnknown) {
				t.Fatalf("plain row %d: %+v", i, pr)
			}
		}
	}
	if n == 0 {
		t.Fatal("no plain rows")
	}
}

// Order is global: every error before any warning, then unknown, then healthy;
// within a state, by Gateway, listener, route and rule.
func TestProjectOrder(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	broken := testutil.Gateway("infra", "broken", "kgateway", nil, testutil.Listener{Name: "http", Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionFalse, "Invalid")}})
	calm := testutil.Gateway("infra", "calm", "kgateway", nil)
	calmRef := objects.RefOf(calm)
	disconnected := health.Finding{Code: health.CodeProxyDisconnected, Severity: health.SeverityWarning, Resource: &calmRef, Message: "A proxy is disconnected"}
	on := func(name string, gw objects.ParentRef) *unstructured.Unstructured {
		return testutil.WithRules(testutil.HTTPRoute("shop", name, []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
			rule([]any{svc("cart")}), rule([]any{svc("cart")}))
	}
	waiting := testutil.WithRules(testutil.HTTPRoute("shop", "a-waiting", []objects.ParentRef{gwEdge}), rule([]any{svc("cart")}))
	snap := endpointsSnap(t, svcs, eps, edge("http"), broken, calm, on("z-ok", gwEdge), waiting,
		on("y-broken", testutil.GwRef("infra", "broken")), on("x-calm", testutil.GwRef("infra", "calm")))
	snap.Deployments = append(snap.Deployments, testutil.ProxyDeployment("infra", "broken", 1, 1), testutil.ProxyDeployment("infra", "calm", 1, 1))
	top, p, _ := project(t, snap, "on", disconnected)
	rank := map[health.State]int{health.StateError: 0, health.StateWarning: 1, health.StateUnknown: 2, health.StateHealthy: 3}
	var got []health.State
	for k, i := range p.Order {
		got = append(got, p.Problems[i].State)
		if k == 0 {
			continue
		}
		prev, cur := top.Rows[p.Order[k-1]], top.Rows[i]
		rp, rc := rank[p.Problems[p.Order[k-1]].State], rank[p.Problems[i].State]
		if rp > rc || rp == rc && top.Gateway(prev) == top.Gateway(cur) && top.Chain(prev)[0] == top.Chain(cur)[0] && top.Rule(prev).Index > top.Rule(cur).Index {
			t.Fatalf("order %v at %d", got, k)
		}
	}
	want := []health.State{health.StateError, health.StateError, health.StateWarning, health.StateWarning,
		health.StateUnknown, health.StateHealthy, health.StateHealthy}
	if !slices.Equal(got, want) {
		t.Fatalf("states in order %v, want %v", got, want)
	}
}

// A Service the cache lacks while endpoints are on is waiting, never healthy.
func TestProjectMissingServiceIsWaiting(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	_, p, _ := project(t, endpointsSnap(t, svcs, eps, edge("http"), accepted("r", svc("gone"), port("cart", 9999))), "on")
	for i := range p.Problems {
		if pr := p.Problems[i]; pr.State != health.StateUnknown || !hasSignal(pr, paths.SignalWaiting, health.StateUnknown) || p.Endpoints[i] != nil {
			t.Fatalf("row %d: problem %+v endpoints %+v", i, pr, p.Endpoints[i])
		}
	}
}

// A 013 on the route counts only on a parent whose own status has
// ResolvedRefs=False: the other parent's rows stay healthy (D-014).
func TestProjectRefFindingGatedByParent(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	calm := testutil.GwRef("infra", "calm")
	r := testutil.WithRules(testutil.HTTPRoute("shop", "r", []objects.ParentRef{gwEdge, calm}, unresolved(gwEdge), testutil.AcceptedBy(calm, testutil.Controller)),
		rule([]any{svc("cart"), port("cart", 9999)}))
	snap := endpointsSnap(t, svcs, eps, edge("http"), testutil.Gateway("infra", "calm", "kgateway", nil), r)
	snap.Deployments = append(snap.Deployments, testutil.ProxyDeployment("infra", "calm", 1, 1))
	top, p, _ := project(t, snap, "on")
	for i, row := range top.Rows {
		b, _ := top.Backend(row)
		if b.Port != 9999 {
			continue
		}
		has013 := slices.ContainsFunc(p.Problems[i].Signals, func(s paths.Signal) bool { return s.Code == health.CodeRefUnresolved })
		switch top.Gateway(row).Name {
		case "edge":
			if p.Problems[i].State != health.StateError || !has013 || p.Shares[i].State != paths.ShareFailing {
				t.Fatalf("edge: %+v share %+v", p.Problems[i], p.Shares[i])
			}
		case "calm":
			if has013 || p.Problems[i].State == health.StateError || p.Shares[i].State != paths.ShareShown {
				t.Fatalf("calm: %+v share %+v", p.Problems[i], p.Shares[i])
			}
		}
	}
}

// Known over-report: on a delegated row the root's ResolvedRefs=False is an
// error even when the root's 013 candidates rule out the delegation ref.
func TestProjectDelegatedRootRefsOverReport(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	api := testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{gwEdge}, unresolved(gwEdge)),
		prefixes([]string{"/api"}, testutil.DelegateTo("shop", "plain")), rule([]any{svc("gone")}))
	plain := child("plain", false, prefixes([]string{"/api/a"}, svc("cart")))
	snap := worldSnap(t, "v2.5.0", nil, edge("http"), api, plain)
	snap.Access[cache.ServicesGVR], snap.Access[cache.EndpointSlicesGVR] = cache.Access{OK: true}, cache.Access{OK: true}
	snap.Services, snap.EndpointSlices = svcs, eps
	top, p, _ := project(t, snap, "on")
	n := 0
	for i, r := range top.Rows {
		if len(top.Chain(r)) != 2 {
			continue
		}
		n++
		if pr := p.Problems[i]; pr.State != health.StateError || !slices.ContainsFunc(pr.Signals, func(s paths.Signal) bool {
			return s.Kind == paths.SignalRouteStatus && s.Code == "ResolvedRefs"
		}) {
			t.Fatalf("delegated row: %+v", pr)
		}
	}
	if n == 0 {
		t.Fatal("no delegated rows")
	}
}

// Equal signal sets share storage, so the projection's size follows the
// distinct sets; logged at the large-paths world with endpoints on and off.
func TestProjectPathsWorldBytes(t *testing.T) {
	tier := scale.Large.Paths()
	for _, cap := range []string{"on", "off"} {
		_, p, _ := project(t, scale.Snapshot(tier, 1), cap)
		sets := map[*paths.Signal]bool{}
		bad := 0
		for _, pr := range p.Problems {
			if len(pr.Signals) > 0 {
				sets[&pr.Signals[0]] = true
				bad++
			}
		}
		if cap == "off" && bad == 0 {
			t.Fatal("endpoints off: no row has a problem")
		}
		if len(sets) >= bad && bad > 1 {
			t.Fatalf("%s: %d signal sets for %d rows: not shared", cap, len(sets), bad)
		}
		t.Logf("%s endpoints %s: %d rows, %d with signals, %d distinct sets, %d bytes", tier.Name, cap, len(p.Problems), bad, len(sets), p.Bytes())
	}
}

// parentWith is the status.parents entry of a route on p with Accepted=True
// and the given ResolvedRefs and kgateway.dev/Programmed conditions.
func parentWith(p objects.ParentRef, refs, programmed metav1.Condition) testutil.RouteParent {
	return testutil.RouteParent{Ref: p, Controller: testutil.Controller, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), refs, programmed}}
}

// Simultaneous failures: ResolvedRefs=False attributed to ref b does not hide
// the attachment's kgateway.dev/Programmed=False from ref cart's row, and a
// root route's KGWUI-015 counts on its rows.
func TestProjectRefsDoNotHideProgramming(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	refsFalse := testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")
	for _, reason := range []string{"Invalid", "RouteRuleDropped"} {
		st := parentWith(gwEdge, refsFalse, testutil.Cond("kgateway.dev/Programmed", metav1.ConditionFalse, reason))
		r := testutil.WithRules(testutil.HTTPRoute("shop", "r", []objects.ParentRef{gwEdge}, st), rule([]any{svc("cart"), svc("b")}))
		top, p, _ := project(t, endpointsSnap(t, svcs, eps, edge("http"), r), "on")
		for i := range top.Rows {
			pr := p.Problems[i]
			if pr.State != health.StateError || !slices.ContainsFunc(pr.Signals, func(s paths.Signal) bool {
				return s.Kind == paths.SignalRouteStatus && s.Code == "kgateway.dev/Programmed"
			}) {
				t.Fatalf("%s: row %d %+v", reason, i, pr)
			}
			if reason == "RouteRuleDropped" && !slices.ContainsFunc(pr.Signals, func(s paths.Signal) bool { return s.Code == health.CodeRuleReplaced }) {
				t.Fatalf("%s: row %d lacks the root's KGWUI-015: %+v", reason, i, pr)
			}
		}
	}
	// A root's KGWUI-015 alone, ResolvedRefs resolved, counts too.
	st := parentWith(gwEdge, testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs"),
		testutil.Cond("kgateway.dev/Programmed", metav1.ConditionFalse, "RouteRuleDropped"))
	r := testutil.WithRules(testutil.HTTPRoute("shop", "r", []objects.ParentRef{gwEdge}, st), rule([]any{svc("cart")}))
	_, p, _ := project(t, endpointsSnap(t, svcs, eps, edge("http"), r), "on")
	if pr := p.Problems[0]; !slices.ContainsFunc(pr.Signals, func(s paths.Signal) bool { return s.Code == health.CodeRuleReplaced }) {
		t.Fatalf("root KGWUI-015 missing: %+v", pr)
	}
}

// A delegated child's own controller status for its parent route counts on
// its rows: missing or stale is waiting, rejected is an error, accepted is
// healthy. Its endpoints are ready throughout (D-006).
func TestProjectDelegatedChildStatus(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	api := testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{gwEdge}, testutil.AcceptedBy(gwEdge, testutil.Controller)),
		prefixes([]string{"/api"}, testutil.DelegateTo("shop", "plain")))
	apiRef := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "api"}
	rejected := testutil.RouteParent{Ref: apiRef, Controller: testutil.Controller, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowedByParent"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}}
	unsupported := testutil.RouteParent{Ref: apiRef, Controller: testutil.Controller, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionFalse, "UnsupportedValue"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}}
	// The parentRef written without its namespace: 2.3 writes no entry under the synthesized key.
	unkeyed := []objects.ParentRef{{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Name: "api"}}
	// expectRejection stands in for view C expecting the hostname stage's
	// rejection, as when another context of the child under api is
	// hostname-rejected: the child-entry check calls UnsupportedValue compatible.
	expectRejection := func(i *engine.InstallModel) {
		for k := range i.Computed.Checks {
			if c := &i.Computed.Checks[k]; c.ID.Kind == computed.KindChildEntry {
				c.Class = computed.ClassCompatible
			}
		}
	}
	for _, tc := range []struct {
		name    string
		version string
		parents []objects.ParentRef
		status  []testutil.RouteParent
		stale   bool
		mutate  func(*engine.InstallModel)
		want    health.State
	}{
		{"accepted", "v2.5.0", nil, []testutil.RouteParent{testutil.AcceptedBy(apiRef, testutil.Controller)}, false, nil, health.StateHealthy},
		{"missing", "v2.5.0", nil, nil, false, nil, health.StateUnknown},
		{"stale", "v2.5.0", nil, []testutil.RouteParent{testutil.AcceptedBy(apiRef, testutil.Controller)}, true, nil, health.StateUnknown},
		{"rejected", "v2.5.0", nil, []testutil.RouteParent{rejected}, false, nil, health.StateError},
		{"rejection view C does not expect", "v2.5.0", nil, []testutil.RouteParent{unsupported}, false, nil, health.StateError},
		{"2.3 writes no entry", "v2.3.0", unkeyed, nil, false, nil, health.StateHealthy},
		{"rejection view C expects", "v2.5.0", nil, []testutil.RouteParent{unsupported}, false, expectRejection, health.StateHealthy},
	} {
		plain := testutil.WithRules(testutil.HTTPRoute("shop", "plain", tc.parents, tc.status...), prefixes([]string{"/api/a"}, svc("cart")))
		if tc.stale {
			plain.SetGeneration(2)
		}
		snap := worldSnap(t, tc.version, nil, edge("http"), api, plain)
		snap.Access[cache.ServicesGVR], snap.Access[cache.EndpointSlicesGVR] = cache.Access{OK: true}, cache.Access{OK: true}
		snap.Services, snap.EndpointSlices = svcs, eps
		rules := func(string) computed.Selection { return rulesets.Select(tc.version) }
		top, p, _ := projectWith(t, snap, "on", []time.Duration{0}, rules, tc.mutate)
		n := 0
		for i, r := range top.Rows {
			if len(top.Chain(r)) != 2 {
				continue
			}
			n++
			if pr := p.Problems[i]; pr.State != tc.want {
				t.Errorf("%s: delegated row %+v, want %s", tc.name, pr, tc.want)
			}
		}
		if n == 0 {
			t.Fatalf("%s: no delegated rows", tc.name)
		}
	}
}
