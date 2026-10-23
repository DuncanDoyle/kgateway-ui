package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

const listBase = "/api/v1/installs/kgw.kgateway"

// Explorer fixture refs.
var (
	edgeRef = testutil.GwRef("infra", "edge")
	ls1Ref  = objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls1"}
	apiRef  = testutil.HTTPRouteRef("shop", "api")
)

func svcRef(name string) map[string]any { return map[string]any{"name": name, "port": int64(80)} }

func mirrorFilter(name string) map[string]any {
	return map[string]any{"type": "RequestMirror", "requestMirror": map[string]any{"backendRef": svcRef(name)}}
}

// backendRule is a rule with backends and filters.
func backendRule(backends []any, filters ...any) map[string]any {
	r := testutil.Rule()
	r["backendRefs"] = backends
	if len(filters) > 0 {
		r["filters"] = filters
	}
	return r
}

func beRef(name string) map[string]any {
	return map[string]any{"group": "gateway.kgateway.dev", "kind": "Backend", "name": name}
}

// explorerWorld is world plus a ListenerSet and one route per attachment:
// shop/cart (world) accepted; shop/via-ls through ls1; shop/rejected not
// accepted; shop/pending no status; shop/api a delegating parent with
// shop/child its reported child and shop/declared declaring it; blog/posts (world) and blog/sub unwatched, sub
// naming shop/api; shop/backends and shop/mirror-only name backends.
func explorerWorld(extra ...runtime.Object) []runtime.Object {
	cond := testutil.Cond
	rejected := testutil.RouteParent{Ref: edgeRef, Controller: testutil.Controller, Conds: []metav1.Condition{
		{Type: "Accepted", Status: metav1.ConditionFalse, Reason: "NotAllowedByListeners", Message: "no listener allows this kind", ObservedGeneration: 1, LastTransitionTime: metav1.NewTime(testutil.Now.Add(-time.Hour))},
		cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}}
	objs := []runtime.Object{
		testutil.ListenerSet("infra", "ls1", edgeRef, nil),
		testutil.WithHostnames(testutil.HTTPRoute("shop", "via-ls", []objects.ParentRef{ls1Ref}, testutil.AcceptedBy(ls1Ref, testutil.Controller)), "ls.example.com"),
		testutil.HTTPRoute("shop", "rejected", []objects.ParentRef{edgeRef}, rejected),
		testutil.HTTPRoute("shop", "pending", []objects.ParentRef{edgeRef}),
		testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
			backendRule([]any{testutil.DelegateTo("shop", "child")})),
		testutil.HTTPRoute("shop", "declared", []objects.ParentRef{apiRef}),
		testutil.HTTPRoute("shop", "child", []objects.ParentRef{apiRef}, testutil.AcceptedBy(apiRef, testutil.Controller)),
		testutil.HTTPRoute("blog", "sub", []objects.ParentRef{apiRef}),
		testutil.WithRules(testutil.HTTPRoute("shop", "backends", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
			backendRule([]any{svcRef("cart-svc"), beRef("be1")}, mirrorFilter("mirror-svc"))),
		testutil.WithRules(testutil.HTTPRoute("shop", "mirror-only", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
			backendRule([]any{svcRef("other-svc")}, mirrorFilter("mirror-svc"))),
		testutil.Backend("shop", "be1", nil),
	}
	return world(append(objs, extra...)...)
}

func routeNames(p Page[RouteRow]) []string {
	out := []string{}
	for _, r := range p.Items {
		out = append(out, r.Ref.Namespace+"/"+r.Ref.Name)
	}
	return out
}

func routeByName(t *testing.T, p Page[RouteRow], name string) RouteRow {
	t.Helper()
	for _, r := range p.Items {
		if r.Ref.Namespace+"/"+r.Ref.Name == name {
			return r
		}
	}
	t.Fatalf("no row %s in %v", name, routeNames(p))
	return RouteRow{}
}

func TestListParamsAndErrors(t *testing.T) {
	var many []runtime.Object
	for i := range 60 {
		many = append(many, testutil.HTTPRoute("shop", fmt.Sprintf("r%02d", i), []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)))
	}
	_, srv, _ := startWith(t, setup{}, world(many...)...)

	p := get[Page[RouteRow]](t, srv, listBase+"/routes", 200)
	if len(p.Items) != 50 || p.Total != 62 || p.Next == "" || p.Prev != "" || p.Install != "kgw.kgateway" {
		t.Fatalf("default page: %d items, total %d, next %q, prev %q, install %q", len(p.Items), p.Total, p.Next, p.Prev, p.Install)
	}
	one := get[Page[RouteRow]](t, srv, listBase+"/routes?limit=1", 200)
	for _, q := range []string{
		"limit=0", "limit=201", "limit=-1", "limit=x", "state=bogus", "sort=bogus", "sort=name.up", "sort=.desc", "health=sick",
		"attachment=pending", "attachment=declared&parent=gateway.networking.k8s.io/Gateway/infra/edge",
		"attachment=bogus&parent=gateway.networking.k8s.io/Gateway/infra/edge",
		"cursor=garbage", "cursor=" + one.Next + "&sort=health", "listener=https", "unknown=1", "q=a&q=b",
		"parent=gateway.networking.k8s.io/Gateway/infra", "kind=HTTPRoute", "sort=routes",
	} {
		getErr(t, srv, listBase+"/routes?"+q, 400)
	}
	for _, q := range []string{"kind=gateway.networking.k8s.io/HTTPRoute", "state=pending", "parent=gateway.networking.k8s.io/Gateway/infra/edge", "backend=core/Service/shop/x"} {
		getErr(t, srv, listBase+"/gateways?"+q, 400)
	}
	getErr(t, srv, listBase+"/listenersets?parent=gateway.networking.k8s.io/HTTPRoute/shop/cart", 400)
	getErr(t, srv, listBase+"/listenersets?state=pending", 400)
	// Valid values of every parameter.
	for _, path := range []string{
		"/routes?sort=health.desc&health=healthy&health=error&namespace=shop&namespace=blog&q=R0&kind=gateway.networking.k8s.io/HTTPRoute&state=accepted&state=pending&limit=200",
		"/routes?cursor=" + one.Next, "/gateways?sort=routes.desc", "/listenersets?parent=gateway.networking.k8s.io/Gateway/infra/edge&sort=kind.asc",
	} {
		get[map[string]any](t, srv, listBase+path, 200)
	}
	for _, l := range []string{"routes", "gateways", "listenersets"} {
		getErr(t, srv, "/api/v1/installs/nope/"+l, 404)
	}
	unsynced := httptest.NewServer(NewHandler(staticModel{&engine.Model{Snapshot: &cache.Snapshot{}}}, fstest.MapFS{}, nil))
	t.Cleanup(unsynced.Close)
	for _, l := range []string{"routes", "gateways", "listenersets"} {
		getErr(t, unsynced, listBase+"/"+l, 503)
	}
	if b := wire(t, srv, listBase+"/routes?limit=0"); !bytes.Contains(b, []byte(`"error": "bad query: limit`)) {
		t.Fatalf("a 400 carries the error shape:\n%s", b)
	}
}

// getErr GETs path, wants status and the ErrorResponse shape: a JSON
// object whose only key is a non-empty "error".
func getErr(t *testing.T, srv *httptest.Server, path string, status int) ErrorResponse {
	t.Helper()
	b := wire(t, srv, path)
	var w struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    map[string]any    `json:"body"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	msg, ok := w.Body["error"].(string)
	if w.Status != status || len(w.Body) != 1 || !ok || msg == "" || w.Headers["Content-Type"] != "application/json" {
		t.Fatalf("GET %s: want %d with the error shape, got\n%s", path, status, b)
	}
	return ErrorResponse{Error: msg}
}

func TestRoutesFilteredByParentIncludeListenerSetRoutes(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, explorerWorld()...)
	gw := get[Page[RouteRow]](t, srv, listBase+"/routes?parent=gateway.networking.k8s.io/Gateway/infra/edge", 200)
	if !slices.Contains(routeNames(gw), "shop/via-ls") || !slices.Contains(routeNames(gw), "shop/cart") || slices.Contains(routeNames(gw), "shop/declared") {
		t.Fatalf("Gateway parent rows %v", routeNames(gw))
	}
	r := routeByName(t, gw, "shop/via-ls")
	if len(r.Parents) != 1 || r.Parents[0].Ref.Ref != ls1Ref.Ref() || r.Parents[0].Via == nil || r.Parents[0].Via.Ref != edgeRef.Ref() ||
		r.Parents[0].Via.Link != LinkDetail || r.Parents[0].ViaRestricted || r.Parents[0].Attachment != "accepted" || r.Parents[0].Ref.Link != LinkDetail {
		t.Fatalf("via-ls parents %+v", r.Parents)
	}
	ls := get[Page[RouteRow]](t, srv, listBase+"/routes?parent=gateway.networking.k8s.io/ListenerSet/infra/ls1", 200)
	if got := routeNames(ls); !slices.Equal(got, []string{"shop/via-ls"}) || ls.Total != 1 {
		t.Fatalf("ListenerSet parent rows %v", got)
	}
	if !slices.Equal(r.Hostnames, []string{"ls.example.com"}) || !slices.Equal(r.Flags, []string{"accepted"}) || r.Health != "healthy" || r.Mirror {
		t.Fatalf("via-ls row %+v", r)
	}
}

func TestAttachmentSubFilters(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, explorerWorld()...)
	const gw, api = "parent=gateway.networking.k8s.io/Gateway/infra/edge", "parent=gateway.networking.k8s.io/HTTPRoute/shop/api"
	for _, tc := range []struct {
		q    string
		want []string
	}{
		{gw + "&attachment=not-accepted", []string{"shop/rejected"}},
		{gw + "&attachment=pending", []string{"shop/pending"}},
		{gw + "&attachment=unwatched", []string{"blog/posts"}},
		{api + "&attachment=declared", []string{"shop/declared"}},
		{api + "&attachment=unwatched", []string{"blog/sub"}},
		{api + "&attachment=accepted", []string{"shop/child"}},
		{api + "&attachment=not-accepted", []string{}},
		{api + "&attachment=pending", []string{}},
		// No attachment: reported edges only; unwatched and declared rows need their filter (design §5.3, §4.2).
		{api, []string{"shop/child"}},
	} {
		p := get[Page[RouteRow]](t, srv, listBase+"/routes?"+tc.q, 200)
		if got := routeNames(p); !slices.Equal(got, tc.want) || p.Total != len(tc.want) {
			t.Errorf("%s: rows %v total %d, want %v", tc.q, got, p.Total, tc.want)
		}
	}
	// The detail's childCount counts what the child-route list returns.
	kids := get[Page[RouteRow]](t, srv, listBase+"/routes?"+api, 200)
	if d := get[RouteDetail](t, srv, routeDetail+"shop/api", 200); d.ChildCount != kids.Total {
		t.Errorf("childCount %d, list total %d", d.ChildCount, kids.Total)
	}
	all := get[Page[RouteRow]](t, srv, listBase+"/routes?limit=200", 200)
	rej := routeByName(t, all, "shop/rejected").Parents[0]
	if rej.Attachment != "not-accepted" || rej.ConditionType != "Accepted" || rej.Reason != "NotAllowedByListeners" || rej.Message != "no listener allows this kind" {
		t.Errorf("rejected parent %+v", rej)
	}
	if pen := routeByName(t, all, "shop/pending").Parents[0]; pen.Attachment != "pending" || pen.Reason == "" || pen.ConditionType != "" {
		t.Errorf("pending parent %+v", pen)
	}
	// An unwatched route naming an owned HTTPRoute links it as unwatched (Task 18).
	sub := routeByName(t, all, "blog/sub")
	if len(sub.Parents) != 1 || sub.Parents[0].Ref.Ref != apiRef.Ref() || sub.Parents[0].Attachment != "unwatched" || sub.Parents[0].Ref.Link != LinkDetail {
		t.Errorf("blog/sub parents %+v", sub.Parents)
	}
	if a, c := routeByName(t, all, "shop/api"), routeByName(t, all, "shop/child"); a.Children != 1 || len(c.ParentRoutes) != 1 ||
		c.ParentRoutes[0].Ref != apiRef.Ref() || c.ParentRoutes[0].Link != LinkDetail {
		t.Errorf("reported edge: api children %d, child parentRoutes %+v", a.Children, c.ParentRoutes)
	}
	if d := routeByName(t, all, "shop/declared"); !slices.Contains(d.Flags, "declared") || d.Parents[0].Attachment != "declared" {
		t.Errorf("declared row %+v", d)
	}
}

func TestBackendFilter(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, explorerWorld()...)
	for _, tc := range []struct {
		q      string
		want   []string
		mirror []bool
	}{
		{"backend=core/Service/shop/cart-svc", []string{"shop/backends"}, []bool{false}},
		{"backend=gateway.kgateway.dev/Backend/shop/be1", []string{"shop/backends"}, []bool{false}},
		{"backend=core/Service/shop/mirror-svc", []string{"shop/backends", "shop/mirror-only"}, []bool{true, true}},
		{"backend=core/Service/shop/mirror-svc&backend=core/Service/shop/cart-svc", []string{"shop/backends", "shop/mirror-only"}, []bool{false, true}},
		{"backend=core/Service/shop/other-svc&backend=gateway.kgateway.dev/Backend/shop/be1", []string{"shop/backends", "shop/mirror-only"}, []bool{false, false}},
		// Delegation refs never match.
		{"backend=gateway.networking.k8s.io/HTTPRoute/shop/child", []string{}, nil},
		{"backend=core/Service/blog/cart-svc", []string{}, nil},
	} {
		p := get[Page[RouteRow]](t, srv, listBase+"/routes?"+tc.q, 200)
		var mirror []bool
		for _, r := range p.Items {
			mirror = append(mirror, r.Mirror)
		}
		if got := routeNames(p); !slices.Equal(got, tc.want) || !slices.Equal(mirror, tc.mirror) || p.Total != len(tc.want) {
			t.Errorf("%s: rows %v mirror %v, want %v %v", tc.q, got, mirror, tc.want, tc.mirror)
		}
	}
	for _, q := range []string{"core/Service/shop", "core/Service/shop/", "/Service/shop/cart-svc", "core/Service/shop/a/b", ""} {
		getErr(t, srv, listBase+"/routes?backend="+q, 400)
	}
	// mirror is wire-only under a backend filter.
	if r := routeByName(t, get[Page[RouteRow]](t, srv, listBase+"/routes", 200), "shop/mirror-only"); r.Mirror {
		t.Error("mirror set without a backend filter")
	}
}

func TestCollectedAtIsConfirmedAt(t *testing.T) {
	ams, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, setup{zone: ams}, explorerWorld()...)
	confirmed := testutil.Now.Add(7 * time.Second)
	srv := httptest.NewServer(NewHandler(confirmedAt{h.e, confirmed.In(ams)}, fstest.MapFS{}, nil))
	t.Cleanup(srv.Close)
	want := confirmed.UTC().Format(time.RFC3339Nano)
	for _, l := range []string{"routes", "gateways", "listenersets"} {
		if got := get[map[string]any](t, srv, listBase+"/"+l, 200)["collectedAt"]; got != want {
			t.Errorf("%s collectedAt %v, want %s", l, got, want)
		}
	}
}

func TestListenerSetsOfOneGatewayPaged(t *testing.T) {
	var sets []runtime.Object
	for i := range 200 {
		sets = append(sets, testutil.ListenerSet("infra", fmt.Sprintf("ls-%03d", i), edgeRef, nil))
	}
	_, srv, _ := startWith(t, setup{}, world(sets...)...)
	path := listBase + "/listenersets?parent=gateway.networking.k8s.io/Gateway/infra/edge"
	seen, pages := map[string]bool{}, 0
	for cur := ""; ; pages++ {
		q := path
		if cur != "" {
			q += "&cursor=" + cur
		}
		p := get[Page[ListenerSetRow]](t, srv, q, 200)
		if p.Total != 200 || len(p.Items) != 50 {
			t.Fatalf("page %d: total %d, %d items", pages, p.Total, len(p.Items))
		}
		for _, r := range p.Items {
			if r.Parent.Ref != edgeRef.Ref() || r.Parent.Link != LinkDetail || r.Ref.Link != LinkDetail || r.Listeners != 1 || r.Health != "healthy" {
				t.Fatalf("row %+v", r)
			}
			seen[r.Ref.Name] = true
		}
		if cur = p.Next; cur == "" {
			pages++
			break
		}
	}
	if pages != 4 || len(seen) != 200 {
		t.Fatalf("%d pages, %d distinct rows", pages, len(seen))
	}
	g := get[Page[GatewayListRow]](t, srv, listBase+"/gateways", 200)
	if len(g.Items) != 1 || g.Items[0].ListenerSets != 200 {
		t.Fatalf("gateway rows %+v", g.Items)
	}
}

func TestGatewayListRow(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, explorerWorld()...)
	g := get[Page[GatewayListRow]](t, srv, listBase+"/gateways", 200)
	if len(g.Items) != 1 || g.Total != 1 || g.DeniedInInstall != 0 {
		t.Fatalf("gateways %+v", g)
	}
	r := g.Items[0]
	if r.Ref.Ref != edgeRef.Ref() || r.Ref.Link != LinkDetail || r.Class != "kgateway" || r.NotAccepted != 1 || r.ListenerSets != 1 || r.Addresses == nil {
		t.Fatalf("edge row %+v", r)
	}
}

// restrictedAuthz denies the routes shop/secret and shop/child, the Gateway
// infra/edge and the Backend shop/be-x.
func restrictedAuthz() Authorizer {
	return AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool {
		hidden := ref.Namespace == "shop" && (ref.Name == "secret" || ref.Name == "child") || ref.Kind == "Gateway" && ref.Name == "edge" || ref.Name == "be-x"
		return !hidden
	})
}

// probeRoutes are a visible and a hidden route naming the Backend shop/be-x.
func probeRoutes() []runtime.Object {
	return []runtime.Object{
		testutil.WithRules(testutil.HTTPRoute("shop", "uses-be", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)), backendRule([]any{beRef("be-x")})),
		testutil.WithHostnames(testutil.WithRules(testutil.HTTPRoute("shop", "secret", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
			backendRule([]any{beRef("be-x")})), "secret.example.com"),
	}
}

func TestListFiltersCannotProbeDeniedRows(t *testing.T) {
	objs := append(probeRoutes(), testutil.Backend("shop", "be-x", nil))
	_, srv, _ := startWith(t, setup{authz: restrictedAuthz()}, explorerWorld(objs...)...)
	for _, q := range []string{"", "q=secret", "q=secret.example.com", "namespace=shop", "state=accepted", "health=healthy",
		"parent=gateway.networking.k8s.io/Gateway/infra/edge", "backend=gateway.kgateway.dev/Backend/shop/be-x", "sort=health.desc"} {
		p := get[Page[RouteRow]](t, srv, listBase+"/routes?"+q, 200)
		if p.DeniedInInstall != 2 || slices.Contains(routeNames(p), "shop/secret") || p.Total != len(p.Items) {
			t.Errorf("%q: deniedInInstall %d, rows %v, total %d", q, p.DeniedInInstall, routeNames(p), p.Total)
		}
	}
	if p := get[Page[RouteRow]](t, srv, listBase+"/routes?backend=gateway.kgateway.dev/Backend/shop/be-x", 200); !slices.Equal(routeNames(p), []string{"shop/uses-be"}) {
		t.Errorf("backend rows %v", routeNames(p))
	}
	// The ListenerSet's Gateway is hidden: via carries no identity.
	all := get[Page[RouteRow]](t, srv, listBase+"/routes?limit=200", 200)
	if p := routeByName(t, all, "shop/via-ls").Parents[0]; p.Via != nil || !p.ViaRestricted || p.Ref.Link != LinkDetail {
		t.Errorf("via-ls parent %+v", p)
	}
	if a := routeByName(t, all, "shop/api"); a.Children != 0 {
		t.Errorf("a hidden child is counted: %d", a.Children)
	}
	if p := routeByName(t, all, "shop/cart").Parents[0]; p.Ref.Link != LinkNone || p.Via != nil {
		t.Errorf("a denied Gateway parent links none: %+v", p)
	}
	g := get[Page[GatewayListRow]](t, srv, listBase+"/gateways", 200)
	if len(g.Items) != 0 || g.Total != 0 || g.DeniedInInstall != 1 {
		t.Errorf("gateways %+v", g)
	}
	s := get[Page[ListenerSetRow]](t, srv, listBase+"/listenersets", 200)
	if len(s.Items) != 1 || s.DeniedInInstall != 0 || s.Items[0].Parent.Link != LinkNone {
		t.Errorf("listenersets %+v", s)
	}
}

// A denied Backend and an absent one give byte-identical responses (rule 2a).
func TestBackendFilterDeniedEqualsAbsent(t *testing.T) {
	_, denied, _ := startWith(t, setup{authz: restrictedAuthz()}, explorerWorld(append(probeRoutes(), testutil.Backend("shop", "be-x", nil))...)...)
	_, absent, _ := startWith(t, setup{authz: restrictedAuthz()}, explorerWorld(probeRoutes()...)...)
	const path = listBase + "/routes?backend=gateway.kgateway.dev/Backend/shop/be-x"
	if a, b := wire(t, denied, path), wire(t, absent, path); !bytes.Equal(a, b) {
		t.Fatalf("denied and absent backends differ:\n%s\n%s", a, b)
	}
}

// A hidden ListenerSet hides what its spec says: no via, and a route through
// it is not under its Gateway, in rows, totals or counts (rule 3).
func TestHiddenListenerSetHidesVia(t *testing.T) {
	cond := testutil.Cond
	// shop/ls-rejected is not accepted by edge only through ls1.
	lsRejected := testutil.HTTPRoute("shop", "ls-rejected", []objects.ParentRef{ls1Ref}, testutil.RouteParent{Ref: ls1Ref, Controller: testutil.Controller,
		Conds: []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners"), cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}})
	hideLS := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Kind != "ListenerSet" })
	_, srv, _ := startWith(t, setup{authz: hideLS}, explorerWorld(lsRejected)...)
	_, all, _ := startWith(t, setup{}, explorerWorld(lsRejected)...)
	const edge = "parent=gateway.networking.k8s.io/Gateway/infra/edge"

	rows := get[Page[RouteRow]](t, srv, listBase+"/routes?limit=200", 200)
	if p := routeByName(t, rows, "shop/via-ls").Parents[0]; p.Via != nil || !p.ViaRestricted || p.Ref.Link != LinkNone {
		t.Errorf("via-ls parent %+v", p)
	}
	for _, q := range []string{edge, edge + "&attachment=accepted", edge + "&attachment=not-accepted"} {
		p, full := get[Page[RouteRow]](t, srv, listBase+"/routes?"+q, 200), get[Page[RouteRow]](t, all, listBase+"/routes?"+q, 200)
		names, fullNames := routeNames(p), routeNames(full)
		fullNames = slices.DeleteFunc(fullNames, func(n string) bool { return n == "shop/via-ls" || n == "shop/ls-rejected" })
		if !slices.Equal(names, fullNames) || p.Total != len(names) {
			t.Errorf("%s: rows %v total %d, want %v", q, names, p.Total, fullNames)
		}
	}
	// The route's own parentRef still lists it under the ListenerSet (rule 8).
	if p := get[Page[RouteRow]](t, srv, listBase+"/routes?parent=gateway.networking.k8s.io/ListenerSet/infra/ls1", 200); p.Total != 2 {
		t.Errorf("under ls1: %v", routeNames(p))
	}
	g, ga := get[Page[GatewayListRow]](t, srv, listBase+"/gateways", 200).Items[0], get[Page[GatewayListRow]](t, all, listBase+"/gateways", 200).Items[0]
	if g.NotAccepted != 1 || ga.NotAccepted != 2 || g.ListenerSets != 0 {
		t.Errorf("notAccepted %d (all %d), listenerSets %d", g.NotAccepted, ga.NotAccepted, g.ListenerSets)
	}
	// Hiding only the Gateway restricts via too.
	hideGW := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Kind != "Gateway" })
	_, gwHidden, _ := startWith(t, setup{authz: hideGW}, explorerWorld()...)
	if p := routeByName(t, get[Page[RouteRow]](t, gwHidden, listBase+"/routes?limit=200", 200), "shop/via-ls").Parents[0]; p.Via != nil || !p.ViaRestricted || p.Ref.Link != LinkDetail {
		t.Errorf("via-ls parent with a hidden Gateway %+v", p)
	}
}
