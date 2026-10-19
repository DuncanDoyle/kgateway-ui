package install

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// snapshot builds a synced Snapshot from namespaces and fixture objects.
func snapshot(t *testing.T, nss []*corev1.Namespace, objs ...*unstructured.Unstructured) *cache.Snapshot {
	t.Helper()
	return &cache.Snapshot{Objects: testutil.ObjectsByGVR(t, objs...), Namespaces: nss, Synced: true}
}

func resolved(id string, scopeJSON string) Resolved {
	ns, name := splitID(id)
	return Resolved{Install: Install{Namespace: ns, Name: name, ControllerNames: []string{testutil.Controller}}, Scope: ParseSelectors(scopeJSON)}
}

func splitID(id string) (string, string) {
	for i := range id {
		if id[i] == '/' {
			return id[:i], id[i+1:]
		}
	}
	return "", id
}

func names(objs []OwnedObject) []string {
	var out []string
	for _, o := range objs {
		out = append(out, o.Obj.GetNamespace()+"/"+o.Obj.GetName())
	}
	return out
}

var teamA = `[{"matchLabels":{"team":"a"}}]`

func TestAssignGatewaysRoutesAndPolicies(t *testing.T) {
	nss := []*corev1.Namespace{
		testutil.Namespace("infra", map[string]string{"team": "a"}),
		testutil.Namespace("shop", map[string]string{"team": "a"}),
		testutil.Namespace("blog", nil),
	}
	snap := snapshot(t, nss,
		testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.GatewayClass("istio", "istio.io/gateway-controller", true),
		testutil.Gateway("infra", "edge", "kgateway", nil),
		testutil.Gateway("infra", "mesh", "istio", nil),
		// Cross-namespace parentRef: the route's own namespace (shop) decides scope.
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{testutil.GwRef("infra", "edge")}),
		testutil.HTTPRoute("blog", "posts", []objects.ParentRef{testutil.GwRef("infra", "edge")}),
		testutil.HTTPRoute("shop", "istio-only", []objects.ParentRef{testutil.GwRef("infra", "mesh")}),
		testutil.TrafficPolicy("shop", "tp"),
		testutil.TrafficPolicy("blog", "tp-out"),
	)
	a := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw/kgateway", teamA)})
	o := a.PerInstall["kgw/kgateway"]
	if len(o.Gateways) != 1 || o.Gateways[0].GetName() != "edge" {
		t.Fatalf("gateways: %d", len(o.Gateways))
	}
	if got := names(o.Routes); len(got) != 1 || got[0] != "shop/cart" {
		t.Fatalf("routes: %v (istio-only must be ignored, blog/posts is out of scope)", got)
	}
	if len(a.Unwatched) != 1 || a.Unwatched[0].Route.String() != "blog/posts" || a.Unwatched[0].Gateway.String() != "infra/edge" {
		t.Fatalf("unwatched: %+v", a.Unwatched)
	}
	if got := names(o.Policies); len(got) != 1 || got[0] != "shop/tp" {
		t.Fatalf("policies: %v", got)
	}
	if len(o.GatewayClasses) != 1 || o.GatewayClasses[0].GetName() != "kgateway" {
		t.Fatalf("classes: %d", len(o.GatewayClasses))
	}
}

func TestAssignRouteViaListenerSet(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("infra", nil), testutil.Namespace("shop", nil)}
	ls := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "ListenerSet",
		"metadata": map[string]any{"name": "ls", "namespace": "shop"},
		"spec":     map[string]any{"parentRef": map[string]any{"name": "edge", "namespace": "infra"}},
	}}
	snap := snapshot(t, nss,
		testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.Gateway("infra", "edge", "kgateway", nil), ls,
		testutil.HTTPRoute("shop", "r", []objects.ParentRef{{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "shop", Name: "ls"}}),
	)
	o := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw/kgateway", "[]")}).PerInstall["kgw/kgateway"]
	if len(o.ListenerSets) != 1 || len(o.Routes) != 1 {
		t.Fatalf("listenersets=%d routes=%d", len(o.ListenerSets), len(o.Routes))
	}
}

func TestAssignAmbiguousGateway(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("shared", map[string]string{"team": "a", "env": "x"})}
	snap := snapshot(t, nss, testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.Gateway("shared", "gw", "kgateway", nil))
	a := Assign(snap, testutil.ServedUpstream(), []Resolved{
		resolved("kgw-a/a", teamA), resolved("kgw-b/b", `[{"matchLabels":{"env":"x"}}]`),
	})
	if len(a.Ambiguous) != 1 || len(a.Ambiguous[0].Installs) != 2 {
		t.Fatalf("want 1 ambiguous gateway claimed by 2 installs, got %+v", a.Ambiguous)
	}
	if len(a.PerInstall["kgw-a/a"].Gateways)+len(a.PerInstall["kgw-b/b"].Gateways) != 0 {
		t.Fatal("an ambiguous gateway must not be assigned to any install")
	}
}

func TestDisjointInstallsSplitCleanly(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("app-a", map[string]string{"team": "a"}), testutil.Namespace("app-b", map[string]string{"team": "b"})}
	snap := snapshot(t, nss, testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.Gateway("app-a", "gw", "kgateway", nil), testutil.Gateway("app-b", "gw", "kgateway", nil))
	a := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw-a/a", teamA), resolved("kgw-b/b", `[{"matchLabels":{"team":"b"}}]`)})
	if len(a.PerInstall["kgw-a/a"].Gateways) != 1 || len(a.PerInstall["kgw-b/b"].Gateways) != 1 || len(a.Ambiguous) != 0 {
		t.Fatalf("unexpected split: a=%d b=%d ambiguous=%d", len(a.PerInstall["kgw-a/a"].Gateways), len(a.PerInstall["kgw-b/b"].Gateways), len(a.Ambiguous))
	}
}

func TestStaleStatusOutOfScopeIsIgnored(t *testing.T) {
	// A namespace deselected after kgateway wrote status: the old status must not make the route count.
	nss := []*corev1.Namespace{testutil.Namespace("infra", map[string]string{"team": "a"}), testutil.Namespace("old", nil)}
	stale := testutil.HTTPRoute("old", "r", []objects.ParentRef{testutil.GwRef("infra", "edge")}, testutil.AcceptedBy(testutil.GwRef("infra", "edge"), testutil.Controller))
	snap := snapshot(t, nss, testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.Gateway("infra", "edge", "kgateway", nil), stale)
	a := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw/kgateway", teamA)})
	if len(a.PerInstall["kgw/kgateway"].Routes) != 0 {
		t.Fatal("out-of-scope route with stale kgateway status must not be assigned")
	}
}

// A parentRef or ListenerSet parent in another API group never
// resolves to a Gateway API Gateway with the same namespace/name.
func TestForeignGroupReferencesDoNotResolve(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("infra", nil), testutil.Namespace("shop", nil)}
	foreignGw := objects.ParentRef{Group: "example.com", Kind: "Gateway", Namespace: "infra", Name: "edge"}
	ls := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "ListenerSet",
		"metadata": map[string]any{"name": "ls", "namespace": "shop"},
		"spec":     map[string]any{"parentRef": map[string]any{"group": "example.com", "kind": "Gateway", "name": "edge", "namespace": "infra"}},
	}}
	snap := snapshot(t, nss,
		testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.Gateway("infra", "edge", "kgateway", nil), ls,
		testutil.HTTPRoute("shop", "foreign", []objects.ParentRef{foreignGw}),
	)
	o := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw/kgateway", "[]")}).PerInstall["kgw/kgateway"]
	if len(o.Routes) != 0 || len(o.ListenerSets) != 0 || len(o.Unattached) != 0 {
		t.Fatalf("foreign-group references must not resolve: routes=%v listenersets=%d unattached=%d", names(o.Routes), len(o.ListenerSets), len(o.Unattached))
	}
}

// A route attached to Gateways of two installs carries, per install,
// only the parentRefs that resolve to that install's Gateways.
func TestRouteParentsAreScopedToTheInstall(t *testing.T) {
	nss := []*corev1.Namespace{
		testutil.Namespace("infra-a", map[string]string{"team": "a"}),
		testutil.Namespace("infra-b", map[string]string{"env": "x"}),
		testutil.Namespace("shop", map[string]string{"team": "a", "env": "x"}),
	}
	ga, gb := testutil.GwRef("infra-a", "edge"), testutil.GwRef("infra-b", "edge")
	snap := snapshot(t, nss, testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.Gateway("infra-a", "edge", "kgateway", nil), testutil.Gateway("infra-b", "edge", "kgateway", nil),
		testutil.HTTPRoute("shop", "both", []objects.ParentRef{ga, gb}))
	a := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw-a/a", teamA), resolved("kgw-b/b", `[{"matchLabels":{"env":"x"}}]`)})
	for id, want := range map[string]objects.ParentRef{"kgw-a/a": ga, "kgw-b/b": gb} {
		rs := a.PerInstall[id].Routes
		if len(rs) != 1 || len(rs[0].Parents) != 1 || rs[0].Parents[0] != want {
			t.Fatalf("%s: want only parent %+v, got %+v", id, want, rs)
		}
	}
}

// A route whose parent Gateway does not exist is kept as a
// diagnostic candidate in every install whose scope contains the route.
func TestRouteToMissingGatewayIsUnattached(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("infra", map[string]string{"team": "a"}), testutil.Namespace("shop", map[string]string{"team": "a"}),
		testutil.Namespace("blog", nil)}
	gone := testutil.GwRef("infra", "gone")
	snap := snapshot(t, nss, testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.Gateway("infra", "edge", "kgateway", nil),
		testutil.HTTPRoute("shop", "orphan", []objects.ParentRef{gone}),
		testutil.HTTPRoute("shop", "half", []objects.ParentRef{testutil.GwRef("infra", "edge"), gone}),
		testutil.HTTPRoute("blog", "elsewhere", []objects.ParentRef{gone}))
	o := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw/kgateway", teamA)}).PerInstall["kgw/kgateway"]
	if got := names(o.Routes); len(got) != 1 || got[0] != "shop/half" {
		t.Fatalf("only shop/half is owned (through infra/edge): %v", got)
	}
	var un []string
	for _, u := range o.Unattached {
		if u.Parent != gone {
			t.Fatalf("unexpected parent %+v", u.Parent)
		}
		un = append(un, u.Route.Obj.GetNamespace()+"/"+u.Route.Obj.GetName())
	}
	if len(un) != 2 || un[0] != "shop/orphan" || un[1] != "shop/half" {
		t.Fatalf("unattached: %v (blog/elsewhere is out of scope)", un)
	}
}

// A non-kgateway policy in scope belongs to the install while it has
// no ancestors yet, and stops belonging to it once another controller owns it.
func TestGenericPolicyWithoutStatusIsOwned(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("shop", nil)}
	other := testutil.PolicyAncestor{Ref: testutil.GwRef("shop", "mesh"), Controller: "istio.io/gateway-controller"}
	mine := testutil.PolicyAncestor{Ref: testutil.GwRef("shop", "edge"), Controller: testutil.Controller}
	snap := snapshot(t, nss,
		testutil.BackendTLSPolicy("shop", "new"),
		testutil.BackendTLSPolicy("shop", "istio", other),
		testutil.BackendTLSPolicy("shop", "ours", mine))
	o := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw/kgateway", "[]")}).PerInstall["kgw/kgateway"]
	if got := names(o.Policies); len(got) != 2 || got[0] != "shop/new" || got[1] != "shop/ours" {
		t.Fatalf("policies: %v", got)
	}
}

// An install whose scope is unknown does not compete with an install whose
// known scope contains the Gateway; it only claims Gateways nobody else does.
func TestUnknownScopeDoesNotMakeGatewaysAmbiguous(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("app-a", map[string]string{"team": "a"}), testutil.Namespace("other", nil)}
	snap := snapshot(t, nss, testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.Gateway("app-a", "gw", "kgateway", nil), testutil.Gateway("other", "gw", "kgateway", nil))
	unknown := resolved("kgw-b/b", `[{`)
	if unknown.Scope.Mode != ScopeUnknown {
		t.Fatalf("fixture: want unknown scope, got %s", unknown.Scope.Mode)
	}
	gwNames := func(o *Owned) []string {
		var out []string
		for _, g := range o.Gateways {
			out = append(out, g.GetNamespace()+"/"+g.GetName())
		}
		return out
	}

	a := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw-a/a", teamA), unknown})
	if len(a.Ambiguous) != 0 {
		t.Fatalf("known scope wins over unknown, want no ambiguity: %+v", a.Ambiguous)
	}
	if got := gwNames(a.PerInstall["kgw-a/a"]); len(got) != 1 || got[0] != "app-a/gw" {
		t.Fatalf("a: %v", got)
	}
	// other/gw is outside a's known scope, so the unknown-scope install claims it.
	if got := gwNames(a.PerInstall["kgw-b/b"]); len(got) != 1 || got[0] != "other/gw" {
		t.Fatalf("b: %v", got)
	}

	both := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw-a/a", `[{`), unknown})
	if len(both.Ambiguous) != 2 {
		t.Fatalf("two unknown scopes: want both gateways ambiguous, got %+v", both.Ambiguous)
	}
}
