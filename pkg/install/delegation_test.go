package install

import (
	"cmp"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

func TestReportedEdgeNeedsEligibility(t *testing.T) {
	p := testutil.HTTPRouteRef("shop", "api")
	ctl := []string{testutil.Controller}
	edge := func(u *unstructured.Unstructured) bool {
		_, ok := ReportedEdge(objects.DecodeHTTPRoute(u), p.Ref(), ctl)
		return ok
	}
	if !edge(testutil.HTTPRoute("shop", "c", []objects.ParentRef{p}, testutil.AcceptedBy(p, testutil.Controller))) {
		t.Fatal("matching spec + status is an edge")
	}
	if edge(testutil.HTTPRoute("shop", "c", []objects.ParentRef{testutil.HTTPRouteRef("shop", "other")}, testutil.AcceptedBy(p, testutil.Controller))) {
		t.Fatal("a removed ref keeps no edge")
	}
	if !edge(testutil.HTTPRoute("shop", "c", nil, testutil.AcceptedBy(p, testutil.Controller))) {
		t.Fatal("empty list falls back to status")
	}
	if edge(testutil.HTTPRoute("shop", "c", nil, testutil.AcceptedBy(p, "example.com/other"))) {
		t.Fatal("another controller's entry is no edge")
	}
	if edge(testutil.HTTPRoute("shop", "c", nil, testutil.AcceptedBy(testutil.HTTPRouteRef("shop", "other"), testutil.Controller))) {
		t.Fatal("an entry for another parent is no edge to p")
	}
	// Equal after defaults: a ref written without its namespace (2.4 writes the entry as the spec ref).
	short := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Name: "api"}
	if !edge(testutil.HTTPRoute("shop", "c", []objects.ParentRef{short}, testutil.AcceptedBy(short, testutil.Controller))) {
		t.Fatal("refs equal after defaults make an edge")
	}
	gw := testutil.GwRef("infra", "edge")
	if edge(testutil.HTTPRoute("shop", "c", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller), testutil.AcceptedBy(p, testutil.Controller))) {
		t.Fatal("a non-empty list without p keeps no implicit edge")
	}
	entry, ok := ReportedEdge(objects.DecodeHTTPRoute(testutil.HTTPRoute("shop", "c", nil, testutil.AcceptedBy(p, testutil.Controller))), p.Ref(), ctl)
	if !ok || entry.ParentRef != p {
		t.Fatalf("entry: %+v", entry)
	}
}

// delegationWorld: install kgw/kgateway watches team=a (infra, shop); blog is unwatched.
func delegationWorld(t *testing.T, routes ...*unstructured.Unstructured) *Owned {
	t.Helper()
	nss := []*corev1.Namespace{testutil.Namespace("infra", map[string]string{"team": "a"}),
		testutil.Namespace("shop", map[string]string{"team": "a"}), testutil.Namespace("blog", nil)}
	objs := append([]*unstructured.Unstructured{testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.GatewayClass("other", "example.com/other", true),
		testutil.Gateway("infra", "edge", "kgateway", nil), testutil.Gateway("infra", "foreign", "other", nil)}, routes...)
	return Assign(snapshot(t, nss, objs...), testutil.ServedUpstream(), []Resolved{resolved("kgw/kgateway", teamA)}, objects.Direct{}).PerInstall["kgw.kgateway"]
}

// routeParents returns the RouteParents of owned route shop/name, failing when it is not owned.
func routeParents(t *testing.T, o *Owned, name string) []objects.ParentRef {
	t.Helper()
	r, ok := o.Route(objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: name})
	if !ok {
		t.Fatalf("shop/%s not owned; routes %v", name, names(o.Routes))
	}
	return r.RouteParents
}

func TestDelegationShapesOwnership(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	ref := func(n string) objects.ParentRef { return testutil.HTTPRouteRef("shop", n) }
	ok := func(p objects.ParentRef) testutil.RouteParent { return testutil.AcceptedBy(p, testutil.Controller) }
	o := delegationWorld(t,
		testutil.HTTPRoute("shop", "a", []objects.ParentRef{gw}, ok(gw)),
		// Chain a → b → c; b implicit, c explicit.
		testutil.HTTPRoute("shop", "b", nil, ok(ref("a"))),
		testutil.HTTPRoute("shop", "c", []objects.ParentRef{ref("b")}, ok(ref("b"))),
		// Diamond: d is a child of a and of b.
		testutil.HTTPRoute("shop", "d", nil, ok(ref("a")), ok(ref("b"))),
		// Cycle a → x ⇄ y.
		testutil.HTTPRoute("shop", "x", []objects.ParentRef{ref("a"), ref("y")}, ok(ref("a")), ok(ref("y"))),
		testutil.HTTPRoute("shop", "y", []objects.ParentRef{ref("x")}, ok(ref("x"))),
		// Mixed: attached to the Gateway and a child of a.
		testutil.HTTPRoute("shop", "m", []objects.ParentRef{gw, ref("a")}, ok(gw), ok(ref("a"))),
		// Reported only by a parent no install owns.
		testutil.HTTPRoute("shop", "f", []objects.ParentRef{testutil.GwRef("infra", "foreign")}, testutil.AcceptedBy(testutil.GwRef("infra", "foreign"), "example.com/other")),
		testutil.HTTPRoute("shop", "orphan", nil, ok(ref("f"))),
		// A reported child outside the scope.
		testutil.HTTPRoute("blog", "out", nil, ok(ref("a"))),
	)
	if got := names(o.Routes); len(got) != 7 {
		t.Fatalf("owned routes %v, want a b c d x y m once each", got)
	}
	for name, want := range map[string][]objects.ParentRef{"a": nil, "b": {ref("a")}, "c": {ref("b")}, "d": {ref("a"), ref("b")},
		"x": {ref("a"), ref("y")}, "y": {ref("x")}, "m": {ref("a")}} {
		got := routeParents(t, o, name)
		slices.SortFunc(got, func(p, q objects.ParentRef) int { return cmp.Compare(p.Name, q.Name) })
		if !slices.Equal(got, want) {
			t.Errorf("%s RouteParents %v, want %v", name, got, want)
		}
	}
	if r, _ := o.Route(objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "m"}); !slices.Equal(r.Parents, []objects.ParentRef{gw}) {
		t.Errorf("mixed route keeps its Gateway parents: %v", r.Parents)
	}
}

func TestImplicitEdgeRules(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	api := testutil.HTTPRouteRef("shop", "api")
	ok := func(p objects.ParentRef) testutil.RouteParent { return testutil.AcceptedBy(p, testutil.Controller) }
	o := delegationWorld(t,
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, ok(gw)),
		testutil.HTTPRoute("shop", "implicit", nil, ok(api)),
		// Gained a Gateway parentRef; the old implicit entry is no longer eligible.
		testutil.HTTPRoute("shop", "gained", []objects.ParentRef{gw}, ok(gw), ok(api)),
	)
	if got := routeParents(t, o, "implicit"); !slices.Equal(got, []objects.ParentRef{api}) {
		t.Fatalf("implicit: %v", got)
	}
	if got := routeParents(t, o, "gained"); len(got) != 0 {
		t.Fatalf("gained a Gateway parentRef: the implicit edge is gone, got %v", got)
	}
}

// On 2.3 a child ref that omits the namespace or sets sectionName gets no
// status entry (computed-rules §7), so it is not a reported child.
func TestRefWithoutEntryIsNotAChild(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	short := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Name: "api"}
	sect := testutil.HTTPRouteRef("shop", "api")
	sect.SectionName = "rule-1"
	o := delegationWorld(t,
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.HTTPRoute("shop", "no-ns", []objects.ParentRef{short}),
		testutil.HTTPRoute("shop", "section", []objects.ParentRef{sect}),
	)
	if got := names(o.Routes); !slices.Equal(got, []string{"shop/api"}) {
		t.Fatalf("owned %v: a ref without a status entry is not owned through that ref", got)
	}
	if len(o.Unattached) != 0 {
		t.Fatalf("an existing HTTPRoute parent is not missing: %+v", o.Unattached)
	}
}

func TestChildReportedByAnotherInstall(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("infra", map[string]string{"team": "a"}), testutil.Namespace("infra-b", map[string]string{"team": "b"}),
		testutil.Namespace("shared", map[string]string{"team": "a", "env": "b"})}
	gwA, gwB := testutil.GwRef("infra", "edge"), testutil.GwRef("infra-b", "edge")
	pa, pb := testutil.HTTPRouteRef("infra", "pa"), testutil.HTTPRouteRef("infra-b", "pb")
	ok := func(p objects.ParentRef) testutil.RouteParent { return testutil.AcceptedBy(p, testutil.Controller) }
	snap := snapshot(t, nss, testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.Gateway("infra", "edge", "kgateway", nil), testutil.Gateway("infra-b", "edge", "kgateway", nil),
		testutil.HTTPRoute("infra", "pa", []objects.ParentRef{gwA}, ok(gwA)),
		testutil.HTTPRoute("infra-b", "pb", []objects.ParentRef{gwB}, ok(gwB)),
		testutil.HTTPRoute("shared", "only-a", nil, ok(pa)),
		testutil.HTTPRoute("shared", "both", nil, ok(pa), ok(pb)),
	)
	// Both installs share the controller name; b watches team=b or env=b.
	a := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw-a/a", teamA),
		resolved("kgw-b/b", `[{"matchLabels":{"team":"b"}},{"matchLabels":{"env":"b"}}]`)}, objects.Direct{})
	oa, ob := a.PerInstall["kgw-a.a"], a.PerInstall["kgw-b.b"]
	if got := names(oa.Routes); !slices.Equal(got, []string{"infra/pa", "shared/only-a", "shared/both"}) {
		t.Fatalf("install a: %v", got)
	}
	if got := names(ob.Routes); !slices.Equal(got, []string{"infra-b/pb", "shared/both"}) {
		t.Fatalf("install b: %v (only-a is reported by a's parent only)", got)
	}
	for _, c := range []struct {
		o    *Owned
		want objects.ParentRef
	}{{oa, pa}, {ob, pb}} {
		r, _ := c.o.Route(objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shared", Name: "both"})
		if !slices.Equal(r.RouteParents, []objects.ParentRef{c.want}) {
			t.Errorf("each install counts only edges whose parent it owns: %v, want %v", r.RouteParents, c.want)
		}
	}
}

// HTTPRoute is a route-parent kind for the missing-parent check only: policy
// ownership (claimable) is unchanged (pre-flight PF8).
func TestHTTPRouteParentKindKeepsPolicyOwnership(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	api := testutil.HTTPRouteRef("shop", "api")
	anc := testutil.PolicyAncestor{Ref: api, Controller: testutil.Controller, Conds: nil}
	o := delegationWorld(t,
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.TrafficPolicy("shop", "on-route", anc),
		testutil.HTTPRoute("shop", "lost", []objects.ParentRef{testutil.HTTPRouteRef("shop", "gone")}),
	)
	if got := names(o.Policies); !slices.Equal(got, []string{"shop/on-route"}) {
		t.Fatalf("policies %v: a policy with an HTTPRoute ancestor stays owned", got)
	}
	if len(o.Unattached) != 1 || o.Unattached[0].Route.Obj.GetName() != "lost" || o.Unattached[0].Parent != testutil.HTTPRouteRef("shop", "gone") {
		t.Fatalf("a missing HTTPRoute parent is unattached: %+v", o.Unattached)
	}
}
