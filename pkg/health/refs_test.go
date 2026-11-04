package health

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	fakediscovery "k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// served is a catalog of a cluster with core, Gateway API and kgateway kinds.
func served(unknown ...string) kinds.Catalog {
	return kinds.NewCatalog([]schema.GroupKind{{Kind: "Service"}, {Kind: "Secret"}, {Kind: "ConfigMap"}, {Group: objects.GatewayAPIGroup, Kind: "HTTPRoute"},
		{Group: "gateway.kgateway.dev", Kind: "Backend"}}, unknown)
}

// refRoute is shop/cart on infra/edge with ResolvedRefs=False/reason and one
// rule holding refs as its backendRefs.
func refRoute(reason, msg string, refs ...map[string]any) *unstructured.Unstructured {
	gw := testutil.GwRef("infra", "edge")
	bad := testutil.Cond("ResolvedRefs", metav1.ConditionFalse, reason)
	bad.Message = msg
	u := testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), bad}})
	brs := make([]any, 0, len(refs))
	for _, r := range refs {
		brs = append(brs, r)
	}
	return testutil.WithRules(u, map[string]any{"backendRefs": brs})
}

// tlsGateway is infra/edge whose listener https has ResolvedRefs=False/reason
// and certificateRefs refs.
func tlsGateway(reason string, refs ...map[string]any) *unstructured.Unstructured {
	bad := testutil.Cond("ResolvedRefs", metav1.ConditionFalse, reason)
	bad.Message = "controller says " + reason
	gw := testutil.Gateway("infra", "edge", "kgateway", nil, testutil.Listener{Name: "https", Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed"), bad}})
	crs := make([]any, 0, len(refs))
	for _, r := range refs {
		crs = append(crs, r)
	}
	l := gw.Object["spec"].(map[string]any)["listeners"].([]any)[0].(map[string]any)
	l["protocol"], l["tls"] = "HTTPS", map[string]any{"certificateRefs": crs}
	return gw
}

// refsReport evaluates w with catalog c.
func refsReport(t *testing.T, w *world, c kinds.Catalog) Report {
	t.Helper()
	in := w.input(t)
	in.Catalog = c
	return Evaluate(in, Upstream(), Runtime{})
}

// only returns the one finding with code on res, failing t otherwise.
func only(t *testing.T, r Report, code, res string) (Finding, *RefsEvidence) {
	t.Helper()
	fs := findingsFor(r, code, res)
	if len(fs) != 1 {
		t.Fatalf("want one %s on %s, got %+v", code, res, r.Findings)
	}
	ev, ok := fs[0].Facts.(*RefsEvidence)
	if !ok {
		t.Fatalf("%s on %s: evidence %#v, want refs", code, res, fs[0].Facts)
	}
	return fs[0], ev
}

func loc(path string, r objects.Ref) RefLocation { return RefLocation{Path: path, Ref: r} }

func svc(ns, name string) objects.Ref { return objects.Ref{Kind: "Service", Namespace: ns, Name: name} }

// Design §7.3: explanation 1 (kind not served) wins; otherwise the controller's
// message, clipped. Neither leaves a KGWUI-002 for the condition.
func TestRouteRefUnresolved(t *testing.T) {
	typo := map[string]any{"group": "delegation.gateway.example", "kind": "Service", "name": "cart"}
	plain := map[string]any{"name": "cart", "port": int64(80)}
	long := "a" + strings.Repeat("é", 4096)
	cases := []struct {
		name, reason, msg, message, explanation string
		refs                                    []map[string]any
		catalog                                 kinds.Catalog
		want                                    []RefLocation
	}{
		{"typo'd group", "BackendNotFound", "", "No kind `Service` in group `delegation.gateway.example`. Check the group.", RefExplainKindNotServed,
			[]map[string]any{plain, typo}, served(),
			[]RefLocation{loc("spec.rules[0].backendRefs[1]", objects.Ref{Group: "delegation.gateway.example", Kind: "Service", Namespace: "shop", Name: "cart"})}},
		{"core kind not served", "InvalidKind", "", "No kind `Servce` in group `core`. Check the group.", RefExplainKindNotServed,
			[]map[string]any{{"kind": "Servce", "name": "cart"}}, served(),
			[]RefLocation{loc("spec.rules[0].backendRefs[0]", objects.Ref{Kind: "Servce", Namespace: "shop", Name: "cart"})}},
		{"group served: the controller's message", "BackendNotFound", long, "Backend not found", RefExplainController,
			[]map[string]any{plain}, served(), []RefLocation{loc("spec.rules[0].backendRefs[0]", svc("shop", "cart"))}},
		// Review Focus 2: a group whose discovery failed is never "check the group".
		{"served state unknown", "BackendNotFound", "", "Backend not found", RefExplainController,
			[]map[string]any{typo}, served("delegation.gateway.example"),
			[]RefLocation{loc("spec.rules[0].backendRefs[0]", objects.Ref{Group: "delegation.gateway.example", Kind: "Service", Namespace: "shop", Name: "cart"})}},
		{"no catalog", "BackendNotFound", "", "Backend not found", RefExplainController,
			[]map[string]any{typo}, kinds.Catalog{},
			[]RefLocation{loc("spec.rules[0].backendRefs[0]", objects.Ref{Group: "delegation.gateway.example", Kind: "Service", Namespace: "shop", Name: "cart"})}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := healthyWorld()
			w.objs[2] = refRoute(tc.reason, tc.msg, tc.refs...)
			r := refsReport(t, w, tc.catalog)
			f, ev := only(t, r, CodeRefUnresolved, "shop/cart")
			if f.Slug != "ref-unresolved" || f.Severity != SeverityError || f.Message != tc.message || ev.Explanation != tc.explanation {
				t.Fatalf("finding %+v evidence %+v", f, ev)
			}
			if !reflect.DeepEqual(ev.Candidates, tc.want) || ev.Attributed != (len(tc.want) == 1) {
				t.Fatalf("candidates %+v attributed %v, want %+v", ev.Candidates, ev.Attributed, tc.want)
			}
			if tc.msg != "" && (!strings.Contains(f.Detail, "…[truncated]") || len(f.Detail) > 1024+200) {
				t.Fatalf("controller message not clipped: %d bytes", len(f.Detail))
			}
			if has(r, CodeConditionFalse, "", "shop/cart") || r.State != StateError {
				t.Fatalf("want no KGWUI-002 and state error: %s %+v", r.State, r.Findings)
			}
		})
	}
}

// R84: a delegation-only parent's BackendNotFound is 013, worded as today
// (explanation 4); delegation refs are never "kind not served".
func TestDelegationBackendNotFoundIs013(t *testing.T) {
	w := healthyWorld()
	w.objs[2] = refRoute("BackendNotFound", "", testutil.DelegateLabel("shop", "team"))
	f, ev := only(t, refsReport(t, w, served()), CodeRefUnresolved, "shop/cart")
	if f.Message != DelegationNoChildMessage || ev.Explanation != RefExplainController || !strings.HasPrefix(f.Detail, DelegationNoChildDetail) {
		t.Fatalf("finding %+v evidence %+v", f, ev)
	}
}

// Review Focus 1: omitted and explicit core refs are one identity; a
// same-namespace ref is never a 014 candidate.
func TestRouteRefNotPermitted(t *testing.T) {
	omitted := map[string]any{"name": "cart", "namespace": "store"}
	explicit := map[string]any{"group": "", "kind": "Service", "name": "cart", "namespace": "store"}
	local := map[string]any{"name": "cart"}
	other := map[string]any{"name": "pay", "namespace": "billing"}
	core := loc("spec.rules[0].backendRefs[1]", svc("store", "cart"))
	cases := []struct {
		name, message, explanation string
		refs                       []map[string]any
		want                       []RefLocation
	}{
		{"one cross-namespace ref, omitted group and kind", "Reference not permitted (missing ReferenceGrant)", RefExplainController,
			[]map[string]any{local, omitted}, []RefLocation{core}},
		{"one cross-namespace ref, explicit core group", "Reference not permitted (missing ReferenceGrant)", RefExplainController,
			[]map[string]any{local, explicit}, []RefLocation{core}},
		{"two cross-namespace refs: not attributed", "Reference not permitted (missing ReferenceGrant)", RefExplainController,
			[]map[string]any{local, omitted, other}, []RefLocation{core, loc("spec.rules[0].backendRefs[2]", svc("billing", "pay"))}},
		{"wrong group: never a missing grant", "No kind `Backend` in group `gateway.kgateway.example`. Check the group.", RefExplainKindNotServed,
			[]map[string]any{local, {"group": "gateway.kgateway.example", "kind": "Backend", "name": "be", "namespace": "store"}},
			[]RefLocation{loc("spec.rules[0].backendRefs[1]", objects.Ref{Group: "gateway.kgateway.example", Kind: "Backend", Namespace: "store", Name: "be"})}},
		// A kgateway-specific field (policy or extension ref): the controller's message, no candidate.
		{"kgateway-specific field", "Reference not permitted (missing ReferenceGrant)", RefExplainController,
			[]map[string]any{local}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := healthyWorld()
			w.objs[2] = refRoute("RefNotPermitted", "denied by the controller", tc.refs...)
			r := refsReport(t, w, served())
			f, ev := only(t, r, CodeRefNotPermitted, "shop/cart")
			if f.Slug != "ref-not-permitted" || f.Severity != SeverityError || f.Message != tc.message || ev.Explanation != tc.explanation ||
				!strings.Contains(f.Detail, "denied by the controller") {
				t.Fatalf("finding %+v evidence %+v", f, ev)
			}
			if !reflect.DeepEqual(ev.Candidates, tc.want) || ev.Attributed != (len(tc.want) == 1) {
				t.Fatalf("candidates %+v attributed %v, want %+v", ev.Candidates, ev.Attributed, tc.want)
			}
			if has(r, CodeConditionFalse, "", "shop/cart") || has(r, CodeRefUnresolved, "", "shop/cart") {
				t.Fatalf("one condition, one finding: %+v", r.Findings)
			}
		})
	}
}

// Listener refs: 013 for InvalidCertificateRef quotes the controller and never
// judges the Secret; 014 names the cross-namespace Secret refs. Keyed by listener.
func TestListenerRefs(t *testing.T) {
	local := map[string]any{"name": "tls"}
	remote := map[string]any{"name": "wild", "namespace": "certs"}
	w := healthyWorld()
	w.objs[1] = tlsGateway("InvalidCertificateRef", local, remote)
	f, ev := only(t, refsReport(t, w, served()), CodeRefUnresolved, "infra/edge")
	want := []RefLocation{loc("spec.listeners[0].tls.certificateRefs[0]", objects.Ref{Kind: "Secret", Namespace: "infra", Name: "tls"}),
		loc("spec.listeners[0].tls.certificateRefs[1]", objects.Ref{Kind: "Secret", Namespace: "certs", Name: "wild"})}
	if f.Message != "Invalid certificate reference" || !strings.Contains(f.Detail, "controller says InvalidCertificateRef") || ev.Explanation != RefExplainController ||
		!reflect.DeepEqual(ev.Candidates, want) || ev.Attributed {
		t.Fatalf("013: %+v %+v", f, ev)
	}

	w = healthyWorld()
	w.objs[1] = tlsGateway("RefNotPermitted", local, remote)
	in := w.input(t)
	in.Catalog = served()
	f, ev = only(t, Evaluate(in, Upstream(), Runtime{}), CodeRefNotPermitted, "infra/edge")
	if !reflect.DeepEqual(ev.Candidates, want[1:]) || !ev.Attributed || f.Message != "Reference not permitted (missing ReferenceGrant)" {
		t.Fatalf("014: %+v %+v", f, ev)
	}

	// The key is the listener name, so two listeners stay apart.
	other := healthyWorld()
	other.objs[1] = tlsGateway("RefNotPermitted", local, remote)
	l := other.objs[1].Object["spec"].(map[string]any)["listeners"].([]any)[0].(map[string]any)
	l["name"] = "https2"
	st := other.objs[1].Object["status"].(map[string]any)["listeners"].([]any)[0].(map[string]any)
	st["name"] = "https2"
	g, _ := only(t, refsReport(t, other, served()), CodeRefNotPermitted, "infra/edge")
	if g.ID == f.ID {
		t.Fatalf("listeners https and https2 share id %s", f.ID)
	}
}

// D-030: 013 and 014 replace 002 for their conditions only; other False
// conditions on the same route parent or listener stay 002.
func TestSpecificCodeReplacesConditionFalse(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	w := healthyWorld()
	w.objs[2] = testutil.WithRules(testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}}),
		map[string]any{"backendRefs": []any{map[string]any{"name": "cart"}}})
	// A listener ResolvedRefs=False with a reason no specific code owns stays 002.
	w.objs[1] = tlsGateway("InvalidRouteKinds", map[string]any{"name": "tls"})
	r := refsReport(t, w, served())
	route := findingsFor(r, CodeConditionFalse, "shop/cart")
	if len(route) != 1 || route[0].Message != "Not allowed by the Gateway's listeners" || len(findingsFor(r, CodeRefUnresolved, "shop/cart")) != 1 {
		t.Fatalf("route: want one 002 for Accepted and one 013: %+v", r.Findings)
	}
	gwf := findingsFor(r, CodeConditionFalse, "infra/edge")
	if len(gwf) != 1 || !strings.Contains(gwf[0].Detail, "InvalidRouteKinds") || has(r, CodeRefUnresolved, "", "infra/edge") || has(r, CodeRefNotPermitted, "", "infra/edge") {
		t.Fatalf("listener: want one 002: %+v", r.Findings)
	}
}

// groupsDiscovery serves ServerGroupsAndResources; failed group versions are
// left out with a partial error, as client-go does.
type groupsDiscovery struct {
	*fakediscovery.FakeDiscovery
	groups []*metav1.APIGroup
	failed bool
}

func (g *groupsDiscovery) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	var err error
	if g.failed {
		err = errors.New("unable to retrieve the complete list of server APIs: metrics.example/v1: the server is currently unable to handle the request")
	}
	return g.groups, g.Resources, err
}

// resourcesOnly is a discovery client without ServerGroupsAndResources, like
// the test fakes that wrap one.
type resourcesOnly struct{ kinds.DiscoveryClient }

func apiGroup(name string) *metav1.APIGroup {
	gv := "v1"
	if name != "" {
		gv = name + "/v1"
	}
	return &metav1.APIGroup{Name: name, Versions: []metav1.GroupVersionForDiscovery{{GroupVersion: gv, Version: "v1"}}}
}

// The Monitor publishes the Catalog from discovery, through to 013's explanation.
func TestMonitorCatalogExplainsRefs(t *testing.T) {
	lists := []*metav1.APIResourceList{
		{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "services", Kind: "Service"}, {Name: "services/status", Kind: "Service"}}},
		{GroupVersion: "gateway.networking.k8s.io/v1", APIResources: []metav1.APIResource{{Name: "httproutes", Kind: "HTTPRoute"}}},
	}
	fake := &fakediscovery.FakeDiscovery{Fake: &clienttesting.Fake{Resources: lists}}
	explain := func(dc kinds.DiscoveryClient, group string) string {
		t.Helper()
		m := kinds.NewMonitor(dc, nil, nil)
		if err := m.Refresh(); err != nil {
			t.Fatal(err)
		}
		w := healthyWorld()
		w.objs[2] = refRoute("BackendNotFound", "", map[string]any{"group": group, "kind": "Service", "name": "cart"})
		_, ev := only(t, refsReport(t, w, m.Catalog()), CodeRefUnresolved, "shop/cart")
		return ev.Explanation
	}
	groups := []*metav1.APIGroup{apiGroup(""), apiGroup(objects.GatewayAPIGroup), apiGroup("metrics.example")}
	full := &groupsDiscovery{FakeDiscovery: fake, groups: groups[:2]}
	partial := &groupsDiscovery{FakeDiscovery: fake, groups: groups, failed: true}
	for _, tc := range []struct {
		name  string
		dc    kinds.DiscoveryClient
		group string
		want  string
	}{
		{"typo'd group", full, "delegation.gateway.example", RefExplainKindNotServed},
		{"core Service served", full, "", RefExplainController},
		{"typo'd group beside a failed one", partial, "delegation.gateway.example", RefExplainKindNotServed},
		{"failed group is unknown", partial, "metrics.example", RefExplainController},
		// A client without ServerGroupsAndResources keeps working: no catalog.
		{"client without the call", resourcesOnly{fake}, "delegation.gateway.example", RefExplainController},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := explain(tc.dc, tc.group); got != tc.want {
				t.Fatalf("explanation %q, want %q", got, tc.want)
			}
		})
	}

	// A refresh whose listing fails outright keeps the last Catalog.
	m := kinds.NewMonitor(full, nil, nil)
	if err := m.Refresh(); err != nil {
		t.Fatal(err)
	}
	before := m.Catalog().Key()
	full.groups, full.failed = nil, true
	if err := m.Refresh(); err != nil || m.Catalog().Key() != before || !m.Catalog().Served("", "Service") {
		t.Fatalf("catalog after a failed listing: %v %q", err, m.Catalog().Key())
	}
	if got := m.Catalog().GroupsWithKind("Service"); !reflect.DeepEqual(got, []string{""}) {
		t.Fatalf("GroupsWithKind(Service) = %v", got)
	}
}

// R86: RefNotPermitted can come from a delegation cycle, so a route that
// delegates is never attributed, even with one cross-namespace backendRef.
func TestDelegatingRouteNeverAttributed(t *testing.T) {
	w := healthyWorld()
	w.objs[2] = refRoute("RefNotPermitted", "", map[string]any{"name": "cart", "namespace": "store"}, testutil.DelegateTo("shop", "cart"))
	_, ev := only(t, refsReport(t, w, served()), CodeRefNotPermitted, "shop/cart")
	if want := []RefLocation{loc("spec.rules[0].backendRefs[0]", svc("store", "cart"))}; !reflect.DeepEqual(ev.Candidates, want) || ev.Attributed {
		t.Fatalf("candidates %+v attributed %v, want %+v unattributed", ev.Candidates, ev.Attributed, want)
	}
}

// R86: a listener's CA certificate refs can be the ones not permitted, so
// they are candidates beside its certificateRefs.
func TestListenerCACertRefIsCandidate(t *testing.T) {
	w := healthyWorld()
	gw := tlsGateway("RefNotPermitted", map[string]any{"name": "wild", "namespace": "certs"})
	gw.Object["spec"].(map[string]any)["tls"] = map[string]any{"frontend": map[string]any{"default": map[string]any{"validation": map[string]any{
		"caCertificateRefs": []any{map[string]any{"group": "", "kind": "ConfigMap", "name": "ca", "namespace": "pki"}}}}}}
	w.objs[1] = gw
	_, ev := only(t, refsReport(t, w, served()), CodeRefNotPermitted, "infra/edge")
	want := []RefLocation{loc("spec.listeners[0].tls.certificateRefs[0]", objects.Ref{Kind: "Secret", Namespace: "certs", Name: "wild"}),
		loc("spec.tls.frontend.default.validation.caCertificateRefs[0]", objects.Ref{Kind: "ConfigMap", Namespace: "pki", Name: "ca"})}
	if !reflect.DeepEqual(ev.Candidates, want) || ev.Attributed {
		t.Fatalf("candidates %+v attributed %v, want %+v unattributed", ev.Candidates, ev.Attributed, want)
	}
}

// A ListenerSet's CA refs live on its Gateway, so a ListenerSet listener is
// unattributed while that Gateway has CA refs for its port; a route mirror is a candidate.
func TestListenerSetAndMirrorCandidates(t *testing.T) {
	bad := testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "RefNotPermitted")
	ls := testutil.ListenerSet("infra", "more", testutil.GwRef("infra", "edge"), nil, testutil.Listener{Name: "extra", Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed"), bad}})
	l := ls.Object["spec"].(map[string]any)["listeners"].([]any)[0].(map[string]any)
	l["protocol"], l["tls"] = "HTTPS", map[string]any{"certificateRefs": []any{map[string]any{"name": "wild", "namespace": "certs"}}}
	for _, tc := range []struct {
		name       string
		perPort    int64
		attributed bool
	}{{"Gateway CA refs on the listener's port", 8081, false}, {"Gateway CA refs on another port", 9443, true}} {
		t.Run(tc.name, func(t *testing.T) {
			w := healthyWorld()
			w.objs[1].Object["spec"].(map[string]any)["tls"] = map[string]any{"frontend": map[string]any{"perPort": []any{map[string]any{"port": tc.perPort,
				"tls": map[string]any{"validation": map[string]any{"caCertificateRefs": []any{map[string]any{"group": "", "kind": "ConfigMap", "name": "ca"}}}}}}}}
			w.objs = append(w.objs, ls.DeepCopy())
			_, ev := only(t, refsReport(t, w, served()), CodeRefNotPermitted, "infra/more")
			if len(ev.Candidates) != 1 || ev.Attributed != tc.attributed {
				t.Fatalf("candidates %+v attributed %v, want one, attributed %v", ev.Candidates, ev.Attributed, tc.attributed)
			}
		})
	}

	w := healthyWorld()
	mirror := map[string]any{"type": "RequestMirror", "requestMirror": map[string]any{"backendRef": map[string]any{"name": "shadow", "namespace": "lab"}}}
	w.objs[2] = refRoute("RefNotPermitted", "", map[string]any{"name": "cart", "namespace": "store", "filters": []any{mirror}})
	_, ev := only(t, refsReport(t, w, served()), CodeRefNotPermitted, "shop/cart")
	want := []RefLocation{loc("spec.rules[0].backendRefs[0]", svc("store", "cart")),
		loc("spec.rules[0].backendRefs[0].filters[0].requestMirror.backendRef", svc("lab", "shadow"))}
	if !reflect.DeepEqual(ev.Candidates, want) || ev.Attributed {
		t.Fatalf("candidates %+v attributed %v, want %+v unattributed", ev.Candidates, ev.Attributed, want)
	}
}

// R87: a ListenerPolicy's clientCertificateValidation CA refs are resolved per
// listener, so one that targets the Gateway leaves no listener 014 attributed.
func TestListenerPolicyCARefNeverAttributed(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	lp := testutil.Policy("gateway.kgateway.dev/v1alpha1", "ListenerPolicy", "infra", "mtls", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")}})
	lp.Object["spec"] = map[string]any{
		"targetRefs": []any{map[string]any{"group": objects.GatewayAPIGroup, "kind": "Gateway", "name": "edge"}},
		"default": map[string]any{"clientCertificateValidation": map[string]any{"mode": "Require",
			"caCertificateRefs": []any{map[string]any{"group": "", "kind": "ConfigMap", "name": "ca", "namespace": "pki"}}}},
	}
	for _, tc := range []struct {
		name       string
		policy     bool
		attributed bool
	}{{"no ListenerPolicy", false, true}, {"ListenerPolicy with a CA ref", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			w := healthyWorld()
			w.objs[1] = tlsGateway("RefNotPermitted", map[string]any{"name": "wild", "namespace": "certs"})
			if tc.policy {
				w.objs = append(w.objs, lp.DeepCopy())
			}
			_, ev := only(t, refsReport(t, w, served()), CodeRefNotPermitted, "infra/edge")
			if len(ev.Candidates) != 1 || ev.Attributed != tc.attributed {
				t.Fatalf("candidates %+v attributed %v, want one, attributed %v", ev.Candidates, ev.Attributed, tc.attributed)
			}
			// A ListenerSet's listeners take the policy on its Gateway too.
			bad := testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "RefNotPermitted")
			ls := testutil.ListenerSet("infra", "more", gw, nil, testutil.Listener{Name: "extra", Conds: []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed"), bad}})
			l := ls.Object["spec"].(map[string]any)["listeners"].([]any)[0].(map[string]any)
			l["protocol"], l["tls"] = "HTTPS", map[string]any{"certificateRefs": []any{map[string]any{"name": "wild", "namespace": "certs"}}}
			w.objs = append(w.objs, ls)
			if _, ev := only(t, refsReport(t, w, served()), CodeRefNotPermitted, "infra/more"); ev.Attributed != tc.attributed {
				t.Fatalf("ListenerSet: attributed %v, want %v", ev.Attributed, tc.attributed)
			}
		})
	}
}
