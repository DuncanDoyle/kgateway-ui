package api

import (
	"encoding/json"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
)

const pathsURL = listBase + "/paths"

// withPathMemo replaces the handler's path memo, for tests.
func withPathMemo(m *paths.Memo) HandlerOption { return func(c *handlerConfig) { c.pathMemo = m } }

// hideEdge hides the Gateway infra/edge and nothing else.
func hideEdge(_ *http.Request, ref objects.Ref) bool {
	return ref.Kind != "Gateway" || ref.Name != "edge"
}

// pathsWorld is explorerWorld plus the Gateway infra/other with shop/on-other
// on it: a 1:3 weighted rule and a redirect rule. Service shop/cart-svc has
// one ready endpoint on port 80; the other Services are not in the cache.
// shop/via-ls, on the ListenerSet infra/ls1, sends to cart-svc.
func pathsWorld() []runtime.Object {
	other := testutil.GatewayWithListeners("infra", "other", "kgateway", []objects.Listener{lst("web", 8080, "")}, up("web", 1))
	weighted := func(name string, w int64) map[string]any {
		return map[string]any{"name": name, "port": int64(80), "weight": w}
	}
	redirect := testutil.Rule(matchPath("/old"))
	redirect["filters"] = []any{map[string]any{"type": "RequestRedirect", "requestRedirect": map[string]any{"path": map[string]any{"type": "ReplaceFullPath", "replaceFullPath": "/new"}}}}
	onOther := testutil.WithRules(testutil.HTTPRoute("shop", "on-other", []objects.ParentRef{otherRef}, testutil.AcceptedBy(otherRef, testutil.Controller)),
		backendRule([]any{weighted("cart-svc", 1), weighted("other-svc", 3)}), redirect)
	name, port, ready := "http", int32(80), true
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "cart-svc", UID: "svc-cart-svc"},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Ports: []corev1.ServicePort{{Name: name, Port: port}}}}
	es := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "cart-svc-x", UID: "es-cart-svc",
		Labels: map[string]string{discoveryv1.LabelServiceName: "cart-svc"}}, AddressType: discoveryv1.AddressTypeIPv4,
		Ports:     []discoveryv1.EndpointPort{{Name: &name, Port: &port}},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}}}}
	objs := explorerWorld(other, onOther, svc, es)
	for _, o := range objs {
		if u, ok := o.(*unstructured.Unstructured); ok && u.GetKind() == "HTTPRoute" && u.GetName() == "via-ls" {
			testutil.WithRules(u, backendRule([]any{svcRef("cart-svc")}))
		}
	}
	return objs
}

// pathsCases are the paths goldens: every path, and the same with infra/edge hidden.
func pathsCases(t *testing.T) []goldenCase {
	_, all, _ := startWith(t, setup{endpoints: true}, pathsWorld()...)
	_, hidden, _ := startWith(t, setup{endpoints: true, authz: AuthorizerFunc(hideEdge)}, pathsWorld()...)
	return []goldenCase{
		{"paths", all, pathsURL},
		{"paths-restricted", hidden, pathsURL},
	}
}

// TestGoldenPaths pins the 6.2 paths bodies (design §5, §9.5, §13).
func TestGoldenPaths(t *testing.T) { runGolden(t, pathsCases(t)) }

// pathKey names a row by its Gateway, route chain, rule and target.
func pathKey(r PathRowDTO) string {
	var b strings.Builder
	b.WriteString(r.Gateway.Name)
	for _, rt := range r.Routes {
		b.WriteString(" " + rt.Namespace + "/" + rt.Name)
	}
	b.WriteString(" " + r.Action)
	if r.Backend != nil {
		b.WriteString(" " + r.Backend.Kind + ":" + r.Backend.Name)
	}
	return b.String()
}

func pathKeys(p PathsResponse) []string {
	out := []string{}
	for _, r := range p.Rows {
		out = append(out, pathKey(r))
	}
	return out
}

// The path list rejects what it cannot serve with 400 (design §5).
func TestPathsBadRequests(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, explorerWorld()...)
	for _, q := range []string{"bogus=1", "limit=0", "limit=201", "limit=x", "cursor=not-a-cursor", "gateway=edge", "gateway=infra/edge/x",
		"route=shop/api", "backend=core/Service/shop", "sort=route.sideways", "sort=.asc", "sort=name", "state=bad", "mirror=sometimes",
		"listener=", "q=a&q=b", "kind=HTTPRoute&kind=GRPCRoute"} {
		get[ErrorResponse](t, srv, pathsURL+"?"+q, http.StatusBadRequest)
	}
	// A cursor from another sort is a bad cursor.
	first := get[PathsResponse](t, srv, pathsURL+"?limit=1", 200)
	if first.Next == "" {
		t.Fatal("one row per page: no next cursor")
	}
	get[ErrorResponse](t, srv, pathsURL+"?limit=1&sort=gateway&cursor="+first.Next, http.StatusBadRequest)
	get[ErrorResponse](t, srv, "/api/v1/installs/nope/paths", http.StatusNotFound)
}

// Filters, paging and the summary at the API: limit 2 pages every row once,
// gateway= keeps one Gateway's rows, and the summary counts the filtered result.
func TestPathsFiltersAndPaging(t *testing.T) {
	_, srv, _ := startWith(t, setup{endpoints: true}, pathsWorld()...)
	all := get[PathsResponse](t, srv, pathsURL+"?limit=200", 200)
	if all.Summary.Paths != len(all.Rows) || all.Next != "" || all.Summary.Gateways != 2 || all.Summary.Hidden || all.Summary.Unmemoised {
		t.Fatalf("all: %d rows, summary %+v", len(all.Rows), all.Summary)
	}
	var paged []string
	for cur := ""; ; {
		p := get[PathsResponse](t, srv, pathsURL+"?limit=2&cursor="+cur, 200)
		if !reflect.DeepEqual(p.Summary, all.Summary) {
			t.Fatalf("summary changes with the page: %+v", p.Summary)
		}
		paged = append(paged, pathKeys(p)...)
		if cur = p.Next; cur == "" {
			break
		}
	}
	if !slices.Equal(paged, pathKeys(all)) {
		t.Fatalf("paged %v, want %v", paged, pathKeys(all))
	}
	other := get[PathsResponse](t, srv, pathsURL+"?gateway=infra/other", 200)
	for _, r := range other.Rows {
		if r.Gateway.Name != "other" {
			t.Fatalf("gateway=infra/other row on %s", r.Gateway.Name)
		}
	}
	// on-other: two weighted refs and a redirect.
	if other.Summary.Paths != 3 || other.Summary.Routes != 1 || other.Summary.Gateways != 1 || other.Summary.Backends != 2 {
		t.Fatalf("on-other summary %+v", other.Summary)
	}
	shares := map[string]float64{}
	for _, r := range other.Rows {
		switch {
		case r.Action == ActionRedirect:
			if r.Backend != nil || r.Share != nil || len(r.Matches) != 1 || r.Matches[0].Path.Value != "/old" {
				t.Fatalf("redirect row %+v", r)
			}
		case r.Share != nil:
			shares[r.Backend.Name] = *r.Share
		}
	}
	if shares["cart-svc"] != 25 || shares["other-svc"] != 75 {
		t.Fatalf("shares %v", shares)
	}
	mirrors := get[PathsResponse](t, srv, pathsURL+"?mirror=only", 200)
	if len(mirrors.Rows) == 0 {
		t.Fatal("no mirror rows")
	}
	for _, r := range mirrors.Rows {
		if !r.Mirror || r.MirrorPercent == nil || *r.MirrorPercent != 100 || r.Backend == nil || r.Backend.Name != "mirror-svc" {
			t.Fatalf("mirror row %+v", r)
		}
	}
}

// §13 at the API: with infra/edge hidden, its rows (ListenerSet ones
// included) are absent, counted nowhere, and summary.hidden is set. The
// Authorizer is asked on every request, memo hits included.
func TestPathsHideGatewayRows(t *testing.T) {
	var calls atomic.Int64
	authz := AuthorizerFunc(func(r *http.Request, ref objects.Ref) bool {
		calls.Add(1)
		return hideEdge(r, ref)
	})
	_, open, _ := startWith(t, setup{endpoints: true}, pathsWorld()...)
	_, srv, _ := startWith(t, setup{endpoints: true, authz: authz}, pathsWorld()...)
	all := get[PathsResponse](t, open, pathsURL+"?limit=200", 200)
	calls.Store(0)
	got := get[PathsResponse](t, srv, pathsURL+"?limit=200", 200)
	first := calls.Load()
	if first == 0 {
		t.Fatal("the Authorizer was not asked")
	}
	var want []string
	for _, r := range all.Rows {
		if r.Gateway.Name != "edge" {
			want = append(want, pathKey(r))
		}
	}
	if len(want) == len(all.Rows) || !slices.Equal(pathKeys(got), want) {
		t.Fatalf("rows %v, want %v", pathKeys(got), want)
	}
	if s := got.Summary; !s.Hidden || s.Paths != len(want) || s.Gateways != 1 || s.Unmemoised {
		t.Fatalf("summary %+v", s)
	}
	for _, r := range got.Rows {
		if r.ListenerSet != nil {
			t.Fatalf("a ListenerSet row of the hidden Gateway: %+v", r)
		}
	}
	again := get[PathsResponse](t, srv, pathsURL+"?limit=200", 200)
	if calls.Load() < 2*first || !reflect.DeepEqual(again, got) {
		t.Fatalf("memo hit: %d Authorizer calls after %d; body changed %v", calls.Load()-first, first, !reflect.DeepEqual(again, got))
	}
}

// Over the topology memo cap the endpoint derives the rows per request, says
// so, and serves the rows a memoised topology does (design §14).
func TestPathsUnmemoised(t *testing.T) {
	_, memo, _ := startWith(t, setup{endpoints: true}, pathsWorld()...)
	_, over, _ := startWith(t, setup{endpoints: true, opts: []HandlerOption{withPathMemo(paths.NewMemoCap(nil, 1))}}, pathsWorld()...)
	for _, q := range []string{"?limit=3", "?sort=backend.desc&limit=200", "?state=unknown"} {
		a, b := get[PathsResponse](t, memo, pathsURL+q, 200), get[PathsResponse](t, over, pathsURL+q, 200)
		if a.Summary.Unmemoised || !b.Summary.Unmemoised {
			t.Fatalf("%s: unmemoised %v and %v", q, a.Summary.Unmemoised, b.Summary.Unmemoised)
		}
		b.Summary.Unmemoised = false
		if !reflect.DeepEqual(a.Rows, b.Rows) || !reflect.DeepEqual(a.Summary, b.Summary) || a.Next != b.Next {
			t.Fatalf("%s: rows differ over the cap", q)
		}
	}
}

// A rule set without a Delegator has no delegated rows: a delegation ref is an
// ordinary ref of kind HTTPRoute (design §4.2). A class-only install has no
// computed views: no rows, and the summary says the paths were not computed.
func TestPathsWithoutDelegator(t *testing.T) {
	_, srv, _ := startWith(t, setup{rulesFor: attachOnlyRules, disputes: true}, explorerWorld()...)
	got := get[PathsResponse](t, srv, pathsURL+"?route=gateway.networking.k8s.io/HTTPRoute/shop/api", 200)
	if len(got.Rows) != 1 {
		t.Fatalf("shop/api rows %v", pathKeys(got))
	}
	if r := got.Rows[0]; r.Delegated || r.Action != ActionBackend || r.Delegation != nil || r.Backend == nil || r.Backend.Kind != "HTTPRoute" || r.Backend.Name != "child" {
		t.Fatalf("delegation ref without a Delegator: %+v", r)
	}
	for _, r := range get[PathsResponse](t, srv, pathsURL+"?limit=200", 200).Rows {
		if r.Delegated {
			t.Fatalf("delegated row %s", pathKey(r))
		}
	}

	foreign := testutil.GwRef("infra", "foreign")
	co := testutil.HTTPRoute("shop", "co", []objects.ParentRef{foreign}, testutil.AcceptedBy(foreign, "example.com/other"))
	ruled := testutil.WithRules(testutil.HTTPRoute("shop", "ruled", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
		backendRule([]any{svcRef("a")}))
	h := newHarness(t, setup{detectors: []install.Detector{kgwDetector{}}, hooks: packHooks, classOnly: true}, world(co, ruled)...)
	co2 := get[PathsResponse](t, h.srv, "/api/v1/installs/example.com~2Fother/paths?limit=200", 200)
	if len(co2.Rows) != 0 || co2.Summary.Paths != 0 || co2.Summary.NotComputed != NotComputedNoAttachments {
		t.Fatalf("class-only: %d rows, summary %+v", len(co2.Rows), co2.Summary)
	}
	if k := get[PathsResponse](t, h.srv, pathsURL, 200); len(k.Rows) == 0 || k.Summary.NotComputed != "" {
		t.Fatalf("kgateway install: %d rows, notComputed %q", len(k.Rows), k.Summary.NotComputed)
	}
}

// With a Delegator the delegating rule is a delegate row and the child's
// rows hang under the root with the view C match (design §4.2).
func TestPathsDelegated(t *testing.T) {
	_, srv, _ := startWith(t, setup{}, explorerWorld()...)
	got := get[PathsResponse](t, srv, pathsURL+"?route=gateway.networking.k8s.io/HTTPRoute/shop/api&limit=200", 200)
	var deleg, child int
	for _, r := range got.Rows {
		switch {
		case r.Action == ActionDelegate:
			deleg++
			if r.Delegated || r.Backend != nil || r.Delegation == nil || r.Delegation.Name != "child" || r.ShareState != string(paths.ShareNotShown) {
				t.Fatalf("delegate row %+v", r)
			}
		case r.Delegated:
			child++
			if len(r.Routes) != 2 || r.Routes[1].Name != "child" || r.Matches == nil {
				t.Fatalf("child row %+v", r)
			}
		}
	}
	if deleg != 1 || child == 0 {
		t.Fatalf("delegate rows %d, child rows %d: %v", deleg, child, pathKeys(got))
	}
}

// Every row has an id, unique within the response's revision, and paging
// returns each id once. Two delegation refs of one rule, and one child
// reached under two inherited contexts, give rows that agree on Gateway,
// chain, rule and target, which only the id tells apart.
func TestPathsRowIDs(t *testing.T) {
	inherit := func(u *unstructured.Unstructured) *unstructured.Unstructured {
		u.SetAnnotations(map[string]string{objects.InheritParentMatcherAnnotation: "true"})
		return u
	}
	objs := append(explorerWorld(),
		testutil.WithRules(testutil.HTTPRoute("shop", "two", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
			delegRule([]string{"/a", "/b"}, testutil.DelegateTo("shop", "c"), testutil.DelegateTo("shop", "d"))),
		inherit(leaf("shop", "c", nil, "/c")), inherit(leaf("shop", "d", nil, "/d")))
	_, srv, _ := startWith(t, setup{disputes: true}, withControllerVersion("v2.5.0", objs)...)
	all := get[PathsResponse](t, srv, pathsURL+"?limit=200", 200)
	ids, keys := map[string]bool{}, map[string]int{}
	for _, r := range all.Rows {
		if r.ID == "" || ids[r.ID] {
			t.Fatalf("id %q empty or repeated: %v", r.ID, pathKeys(all))
		}
		ids[r.ID] = true
		keys[pathKey(r)+" rule "+strconv.Itoa(r.Rule)]++
	}
	if !slices.ContainsFunc(slices.Collect(maps.Values(keys)), func(n int) bool { return n > 1 }) {
		t.Fatalf("no rows that agree on everything but the id: %v", keys)
	}
	var paged []string
	for cur := ""; ; {
		p := get[PathsResponse](t, srv, pathsURL+"?limit=1&cursor="+cur, 200)
		for _, r := range p.Rows {
			paged = append(paged, r.ID)
		}
		if cur = p.Next; cur == "" {
			break
		}
	}
	if len(paged) != len(ids) {
		t.Fatalf("paged %d ids, want %d", len(paged), len(ids))
	}
	for _, id := range paged {
		if !ids[id] {
			t.Fatalf("paged id %q repeated or not in the full list", id)
		}
		delete(ids, id)
	}
}

// A child view C reaches only under unknown contexts has a null match on the
// wire, never its rule as written, and the note unknown.
func TestPathsUnknownMatchIsNull(t *testing.T) {
	unread := corev1.EnvVar{Name: install.WildcardEnv, ValueFrom: &corev1.EnvVarSource{
		ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "cm"}, Key: "k"}}}
	labelled := func(u *unstructured.Unstructured) *unstructured.Unstructured {
		u.SetLabels(map[string]string{objects.DelegationLabelKey: "team"})
		return u
	}
	rule := func(path string, refs ...any) map[string]any {
		r := testutil.Rule(matchPath(path))
		r["backendRefs"] = refs
		return r
	}
	objs := append(envWorld(true, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"a"}}]`), unread),
		testutil.WithRules(testutil.HTTPRoute("shop", "root", []objects.ParentRef{edgeRef}, testutil.AcceptedBy(edgeRef, testutil.Controller)),
			rule("/api", testutil.DelegateLabel("shop", "team"), testutil.DelegateTo("shop", "plain"))),
		labelled(testutil.WithRules(testutil.HTTPRoute("shop", "kid", nil), rule("/api/kid", svcRef("k")))),
		labelled(testutil.WithRules(testutil.HTTPRoute("infra", "elsewhere", nil), rule("/api/x", svcRef("x")))),
		testutil.WithRules(testutil.HTTPRoute("shop", "plain", nil), rule("/api/a", svcRef("a"))))
	_, srv, _ := startWith(t, setup{disputes: true}, withControllerVersion("v2.5.0", objs)...)
	got := get[PathsResponse](t, srv, pathsURL+"?route=gateway.networking.k8s.io/HTTPRoute/shop/plain", 200)
	if len(got.Rows) == 0 {
		t.Fatal("no rows for shop/plain")
	}
	for _, r := range got.Rows {
		if !r.Delegated || r.Matches != nil || r.Note != NoteUnknown || r.Problem.State == "healthy" {
			t.Fatalf("unknown-context row %+v", r)
		}
	}
	body := string(get[json.RawMessage](t, srv, pathsURL+"?route=gateway.networking.k8s.io/HTTPRoute/shop/plain", 200))
	if !strings.Contains(body, `"matches":null`) {
		t.Fatalf("matches not null on the wire: %s", body)
	}
}

// Facets at the API: a restricted viewer never sees the hidden Gateway as a
// facet, and gateway=A still lists B so another Gateway stays selectable.
func TestPathsFacets(t *testing.T) {
	_, open, _ := startWith(t, setup{endpoints: true}, pathsWorld()...)
	_, hidden, _ := startWith(t, setup{endpoints: true, authz: AuthorizerFunc(hideEdge)}, pathsWorld()...)
	names := func(f PathFacetsDTO) []string {
		var out []string
		for _, g := range f.Gateways {
			out = append(out, g.Ref.Namespace+"/"+g.Ref.Name)
		}
		return out
	}
	all := get[PathsResponse](t, open, pathsURL, 200)
	if got := names(all.Summary.Facets); !slices.Equal(got, []string{"infra/edge", "infra/other"}) {
		t.Fatalf("facets %v", got)
	}
	picked := get[PathsResponse](t, open, pathsURL+"?gateway=infra/other", 200)
	if got := names(picked.Summary.Facets); !slices.Equal(got, []string{"infra/edge", "infra/other"}) || picked.Summary.Gateways != 1 {
		t.Fatalf("gateway=infra/other: facets %v, summary %+v", got, picked.Summary)
	}
	if !reflect.DeepEqual(picked.Summary.Facets.Gateways, all.Summary.Facets.Gateways) {
		t.Fatalf("gateway facet counts change with the gateway filter: %+v", picked.Summary.Facets.Gateways)
	}
	restricted := get[PathsResponse](t, hidden, pathsURL+"?gateway=infra/edge", 200)
	if got := names(restricted.Summary.Facets); !slices.Equal(got, []string{"infra/other"}) {
		t.Fatalf("restricted facets %v", got)
	}
	for _, r := range all.Rows {
		for _, s := range r.Problem.Signals {
			if s.Kind == paths.SignalFinding && s.Title == "" {
				t.Fatalf("finding signal without a title: %+v", s)
			}
		}
	}
}

// A finding signal whose ref the viewer may not see carries no title, so a
// finding message never reaches a viewer who may not see its resource (§13).
func TestPathsFindingTitleHidden(t *testing.T) {
	gw := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}
	r, _ := http.NewRequest(http.MethodGet, pathsURL, nil)
	v := &view{a: AuthorizerFunc(hideEdge), r: r, seen: map[objects.Ref]bool{}}
	l := linker{v: v, i: &engine.InstallModel{}}
	titles := map[titleKey]string{{health.CodeProxyNotReady, gw}: "Proxy deployment not found"}
	p := paths.Problem{State: health.StateWarning, Signals: []paths.Signal{{Kind: paths.SignalFinding, Code: health.CodeProxyNotReady, Ref: gw, State: health.StateWarning}}}
	if got := problemDTO(l, titles, p).Signals[0]; got.Title != "" {
		t.Fatalf("hidden finding title leaked: %+v", got)
	}
	v.all = true
	if got := problemDTO(l, titles, p).Signals[0]; got.Title != "Proxy deployment not found" {
		t.Fatalf("visible finding lost its title: %+v", got)
	}
}
