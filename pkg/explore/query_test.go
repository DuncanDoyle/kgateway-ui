package explore

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// hr is a hand-built route row in namespace shop.
func hr(name string, st health.State) RouteRow {
	return RouteRow{Ref: routeRef("shop", name), State: st}
}

// hand builds an index from rows the way Build orders and indexes them.
func hand(rows ...RouteRow) *Index {
	slices.SortFunc(rows, func(a, b RouteRow) int { return compareRefs(a.Ref, b.Ref) })
	ix := &Index{Routes: rows, ByNamespace: map[string][]RowID{}, ByParent: map[objects.Ref][]RowID{}}
	for i := range rows {
		r := &ix.Routes[i]
		r.search = search(r.Ref, r.Hostnames)
		appendOnce(ix.ByNamespace, r.Ref.Namespace, RowID(i))
		for _, l := range r.Parents {
			appendOnce(ix.ByParent, l.Target, RowID(i))
		}
	}
	return ix
}

// names returns the route names of p's rows.
func names(ix *Index, p Page) []string {
	out := []string{}
	for _, id := range p.IDs {
		out = append(out, ix.Routes[id].Ref.Name)
	}
	return out
}

func mustRoutes(t *testing.T, ix *Index, q Query, visible func(objects.Ref) bool) Page {
	t.Helper()
	p, err := ix.QueryRoutes(q, visible)
	if err != nil {
		t.Fatalf("QueryRoutes(%+v): %v", q, err)
	}
	return p
}

// walk pages forward from the first page, then back from the last, and
// returns the names of each page in visiting order.
func walk(t *testing.T, ix *Index, q Query) (fwd, back [][]string) {
	t.Helper()
	p := mustRoutes(t, ix, q, nil)
	pages := []Page{p}
	for p.Next != "" && len(pages) < 20 {
		q.Cursor = p.Next
		p = mustRoutes(t, ix, q, nil)
		pages = append(pages, p)
	}
	for _, pg := range pages {
		fwd = append(fwd, names(ix, pg))
	}
	back = append(back, names(ix, p))
	for p.Prev != "" && len(back) < 20 {
		q.Cursor = p.Prev
		p = mustRoutes(t, ix, q, nil)
		back = append(back, names(ix, p))
	}
	return fwd, back
}

func TestEachFilterAloneAndCombined(t *testing.T) {
	svc := func(name string) map[string]any { return map[string]any{"name": name, "port": int64(80)} }
	mirror := func(name string) map[string]any {
		return map[string]any{"type": "RequestMirror", "requestMirror": map[string]any{"backendRef": svc(name)}}
	}
	rule := func(backends []any, filters ...any) map[string]any {
		r := testutil.Rule()
		r["backendRefs"] = backends
		if len(filters) > 0 {
			r["filters"] = filters
		}
		return r
	}
	be := map[string]any{"group": "gateway.kgateway.dev", "kind": "Backend", "name": "be1"}
	api := testutil.HTTPRouteRef("shop", "api")
	rejected := status(gw, cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners", ""), resolved)
	grpc := testutil.WithRules(testutil.HTTPRoute("shop", "g1", []objects.ParentRef{gw}, accepted(gw)), rule([]any{svc("cart")}))
	grpc.SetAPIVersion("gateway.networking.k8s.io/v1")
	grpc.SetKind("GRPCRoute")
	ix := newWorld(
		testutil.WithHostnames(testutil.WithRules(testutil.HTTPRoute("shop", "r1", []objects.ParentRef{gw}, accepted(gw)), rule([]any{svc("cart"), be}, mirror("mirror-svc"))), "Cart.Example.com"),
		testutil.WithRules(testutil.HTTPRoute("shop", "r2", []objects.ParentRef{gw}, rejected), rule([]any{svc("both")})),
		testutil.WithRules(testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, accepted(gw)), rule([]any{testutil.DelegateTo("shop", "child")})),
		testutil.HTTPRoute("shop", "declared", []objects.ParentRef{api}),
		testutil.HTTPRoute("shop", "pending", []objects.ParentRef{gw}),
		testutil.HTTPRoute("infra", "i1", []objects.ParentRef{gw}, accepted(gw)),
		grpc,
	).build(t)
	service := func(name string) objects.Ref { return objects.Ref{Kind: "Service", Namespace: "shop", Name: name} }
	backend := objects.Ref{Group: "gateway.kgateway.dev", Kind: "Backend", Namespace: "shop", Name: "be1"}
	gk := func(kind string) objects.GroupKind {
		return objects.GroupKind{Group: objects.GatewayAPIGroup, Kind: kind}
	}
	apiRef := routeRef("shop", "api")
	for _, tc := range []struct {
		name string
		q    Query
		want []string
	}{
		{"none", Query{}, []string{"i1", "api", "declared", "g1", "pending", "r1", "r2"}},
		{"namespace", Query{Namespaces: []string{"infra"}}, []string{"i1"}},
		{"two namespaces", Query{Namespaces: []string{"infra", "shop"}}, []string{"i1", "api", "declared", "g1", "pending", "r1", "r2"}},
		{"kind", Query{Kinds: []objects.GroupKind{gk("GRPCRoute")}}, []string{"g1"}},
		{"parent gateway", Query{Parent: &gwObj}, []string{"i1", "api", "g1", "pending", "r1", "r2"}},
		// A declared candidate is never a child: only attachment=declared lists it (design §4.2).
		{"parent httproute", Query{Parent: &apiRef}, []string{}},
		{"attachment", Query{Parent: &gwObj, Attachment: AttachmentNotAccepted}, []string{"r2"}},
		{"declared", Query{Parent: &apiRef, Attachment: AttachmentDeclared}, []string{"declared"}},
		{"state", Query{States: FlagPending | FlagDeclared}, []string{"declared", "pending"}},
		{"q name", Query{Q: "SHOP/R"}, []string{"r1", "r2"}},
		{"q hostname", Query{Q: "cart.example"}, []string{"r1"}},
		{"backend", Query{Backends: []objects.Ref{service("cart")}}, []string{"g1", "r1"}},
		{"two backends OR-ed", Query{Backends: []objects.Ref{backend, service("both")}}, []string{"r1", "r2"}},
		{"mirror target", Query{Backends: []objects.Ref{service("mirror-svc")}}, []string{"r1"}},
		{"delegation ref target", Query{Backends: []objects.Ref{apiRef, routeRef("shop", "child")}}, []string{}},
		{"backend and kind", Query{Backends: []objects.Ref{service("cart")}, Kinds: []objects.GroupKind{gk("HTTPRoute")}}, []string{"r1"}},
		{"namespace, state and q", Query{Namespaces: []string{"shop"}, States: FlagAccepted, Q: "1"}, []string{"g1", "r1"}},
		{"parent and backend", Query{Parent: &gwObj, Backends: []objects.Ref{service("both")}}, []string{"r2"}},
		{"parent, attachment and namespace", Query{Parent: &gwObj, Attachment: AttachmentAccepted, Namespaces: []string{"infra"}}, []string{"i1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := mustRoutes(t, ix, tc.q, nil)
			if got := names(ix, p); !slices.Equal(got, tc.want) || p.Total != len(tc.want) {
				t.Fatalf("got %q total %d, want %q", got, p.Total, tc.want)
			}
		})
	}
	// Health filters on the row state; the expected set comes from the rows.
	for _, st := range healthStates {
		want := []string{}
		for _, r := range ix.Routes {
			if r.State == st {
				want = append(want, r.Ref.Name)
			}
		}
		if got := names(ix, mustRoutes(t, ix, Query{Health: []health.State{st}}, nil)); !slices.Equal(got, want) {
			t.Errorf("health %s: got %q, want %q", st, got, want)
		}
	}
}

func TestHealthFilter(t *testing.T) {
	ix := hand(hr("a", health.StateError), hr("b", health.StateHealthy), hr("c", health.StateWarning), hr("d", health.StateUnknown))
	for _, tc := range []struct {
		h    []health.State
		want []string
	}{
		{[]health.State{health.StateError}, []string{"a"}},
		{[]health.State{health.StateWarning, health.StateUnknown}, []string{"c", "d"}},
	} {
		if got := names(ix, mustRoutes(t, ix, Query{Health: tc.h}, nil)); !slices.Equal(got, tc.want) {
			t.Errorf("health %v: got %q, want %q", tc.h, got, tc.want)
		}
	}
}

func TestSortsBothWaysWithTieBreak(t *testing.T) {
	kind := func(r RouteRow, k string) RouteRow { r.Ref.Kind = k; return r }
	ix := hand(
		hr("c", health.StateError), hr("a", health.StateHealthy), hr("e", health.StateError),
		kind(hr("b", health.StateWarning), "GRPCRoute"), kind(hr("d", health.StateUnknown), "TCPRoute"),
		RouteRow{Ref: routeRef("alpha", "z"), State: health.StateHealthy},
	)
	for _, tc := range []struct {
		sort Sort
		want []string
	}{
		{Sort{}, []string{"z", "a", "b", "c", "d", "e"}},
		{Sort{Key: "name", Desc: true}, []string{"e", "d", "c", "b", "a", "z"}},
		// Ties keep the ascending (namespace, name, group, kind) order both ways.
		{Sort{Key: "kind"}, []string{"b", "z", "a", "c", "e", "d"}},
		{Sort{Key: "kind", Desc: true}, []string{"d", "z", "a", "c", "e", "b"}},
		{Sort{Key: "health"}, []string{"z", "a", "b", "d", "c", "e"}},
		{Sort{Key: "health", Desc: true}, []string{"c", "e", "d", "b", "z", "a"}},
	} {
		t.Run(tc.sort.Key+"/"+direction(tc.sort.Desc), func(t *testing.T) {
			if got := names(ix, mustRoutes(t, ix, Query{Sort: tc.sort}, nil)); !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			// Cursors page in the same order, both ways.
			fwd, back := walk(t, ix, Query{Sort: tc.sort, Limit: 4})
			want := [][]string{tc.want[:4], tc.want[4:]}
			if !reflect.DeepEqual(fwd, want) {
				t.Fatalf("forward pages %q, want %q", fwd, want)
			}
			if slices.Reverse(back); !reflect.DeepEqual(back, want) {
				t.Fatalf("backward pages %q, want %q", back, want)
			}
		})
	}
}

func TestGatewaysSortByVisibleRoutes(t *testing.T) {
	gref := func(name string) objects.Ref {
		return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: name}
	}
	under := func(r RouteRow, gw string) RouteRow {
		r.Parents = []ParentLink{{Target: gref(gw), Attachment: AttachmentAccepted}}
		return r
	}
	ix := hand(under(hr("a", ""), "g1"), under(hr("b", ""), "g1"), under(hr("c", ""), "g2"))
	ix.Gateways = []GatewayRow{{Ref: gref("g1"), Routes: []RowID{0, 1}}, {Ref: gref("g2"), Routes: []RowID{2}}, {Ref: gref("g3")}}
	hideAB := func(r objects.Ref) bool { return r.Kind == "Gateway" || r.Name == "c" }
	for _, tc := range []struct {
		name    string
		visible func(objects.Ref) bool
		desc    bool
		want    []RowID
	}{
		{"all visible", nil, true, []RowID{0, 1, 2}},
		{"hidden routes do not count", hideAB, true, []RowID{1, 0, 2}},
		{"ascending", hideAB, false, []RowID{0, 2, 1}},
	} {
		p, err := ix.QueryGateways(Query{Sort: Sort{Key: "routes", Desc: tc.desc}}, tc.visible)
		if err != nil || !slices.Equal(p.IDs, tc.want) {
			t.Errorf("%s: got %v %v, want %v", tc.name, p.IDs, err, tc.want)
		}
	}
}

func TestQueryListenerSets(t *testing.T) {
	ls := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	ix := newWorld(
		testutil.Gateway("infra", "other", "kgateway", nil),
		testutil.ListenerSet("infra", "ls", gw, nil),
		testutil.ListenerSet("infra", "ls2", testutil.GwRef("infra", "other"), nil),
		testutil.HTTPRoute("shop", "through", []objects.ParentRef{ls}, accepted(ls)),
	).build(t)
	lsNames := func(p Page) []string {
		out := []string{}
		for _, id := range p.IDs {
			out = append(out, ix.ListenerSets[id].Ref.Name)
		}
		return out
	}
	other := testutil.GwRef("infra", "other").Ref()
	for _, tc := range []struct {
		name string
		q    Query
		want []string
	}{
		{"all", Query{}, []string{"ls", "ls2"}},
		{"parent", Query{Parent: &gwObj}, []string{"ls"}},
		{"other parent", Query{Parent: &other}, []string{"ls2"}},
		{"q", Query{Q: "LS2"}, []string{"ls2"}},
		{"name desc", Query{Sort: Sort{Key: "name", Desc: true}}, []string{"ls2", "ls"}},
	} {
		p, err := ix.QueryListenerSets(tc.q, nil)
		if err != nil || !slices.Equal(lsNames(p), tc.want) {
			t.Errorf("%s: got %q %v, want %q", tc.name, lsNames(p), err, tc.want)
		}
	}
}

func TestCursorsAcrossRecomputes(t *testing.T) {
	rows := func(skip string, extra ...RouteRow) *Index {
		var rs []RouteRow
		for _, n := range []string{"a", "b", "c", "d", "e", "f"} {
			if n != skip {
				rs = append(rs, hr(n, health.StateHealthy))
			}
		}
		return hand(append(rs, extra...)...)
	}
	a := rows("")
	q := Query{Limit: 2}
	p1 := mustRoutes(t, a, q, nil)
	if got := names(a, p1); !slices.Equal(got, []string{"a", "b"}) || p1.Prev != "" || p1.Next == "" {
		t.Fatalf("page 1 %q prev %q next %q", got, p1.Prev, p1.Next)
	}
	next := func(ix *Index, cur string) []string {
		t.Helper()
		q := q
		q.Cursor = cur
		return names(ix, mustRoutes(t, ix, q, nil))
	}
	t.Run("insertion before the anchor", func(t *testing.T) {
		b := rows("", hr("aa", health.StateHealthy))
		if got := next(b, p1.Next); !slices.Equal(got, []string{"c", "d"}) {
			t.Fatalf("got %q, want strictly after b", got)
		}
	})
	t.Run("anchor deleted", func(t *testing.T) {
		c := rows("b")
		if got := next(c, p1.Next); !slices.Equal(got, []string{"c", "d"}) {
			t.Fatalf("got %q, want where b would sort", got)
		}
	})
	t.Run("prev from page 2 is page 1", func(t *testing.T) {
		p2 := mustRoutes(t, a, Query{Limit: 2, Cursor: p1.Next}, nil)
		p := mustRoutes(t, a, Query{Limit: 2, Cursor: p2.Prev}, nil)
		if !slices.Equal(p.IDs, p1.IDs) || p.Prev != "" || p.Next != p1.Next {
			t.Fatalf("prev page %q prev %q, want page 1", names(a, p), p.Prev)
		}
	})
	t.Run("prev after a deleted first row", func(t *testing.T) {
		p2 := mustRoutes(t, a, Query{Limit: 2, Cursor: p1.Next}, nil) // c, d
		if got := next(rows("c"), p2.Prev); !slices.Equal(got, []string{"a", "b"}) {
			t.Fatalf("got %q, want the rows before c", got)
		}
	})
	t.Run("descending ties", func(t *testing.T) {
		ix := hand(hr("a", health.StateError), hr("b", health.StateHealthy), hr("c", health.StateError),
			hr("d", health.StateWarning), hr("e", health.StateError), hr("f", health.StateHealthy))
		fwd, back := walk(t, ix, Query{Sort: Sort{Key: "health", Desc: true}, Limit: 2})
		want := [][]string{{"a", "c"}, {"e", "d"}, {"b", "f"}}
		if !reflect.DeepEqual(fwd, want) {
			t.Fatalf("forward %q, want %q", fwd, want)
		}
		if slices.Reverse(back); !reflect.DeepEqual(back, want) {
			t.Fatalf("backward %q, want %q", back, want)
		}
	})
	t.Run("sort value moves", func(t *testing.T) {
		hq := Query{Sort: Sort{Key: "health", Desc: true}, Limit: 2}
		m := func(bState, aaState health.State) *Index {
			return hand(hr("a", health.StateError), hr("aa", aaState), hr("b", bState),
				hr("c", health.StateWarning), hr("d", health.StateHealthy), hr("f", health.StateHealthy))
		}
		before := m(health.StateError, health.StateHealthy) // a, b | c, aa | d, f
		pg := mustRoutes(t, before, hq, nil)
		if got := names(before, pg); !slices.Equal(got, []string{"a", "b"}) {
			t.Fatalf("page 1 %q", got)
		}
		// b becomes healthy and moves past its own anchor: the pager sees it again.
		moved := m(health.StateHealthy, health.StateHealthy) // a, c, aa, b, d, f
		hq.Cursor = pg.Next
		p2 := mustRoutes(t, moved, hq, nil)
		hq.Cursor = p2.Next
		p3 := mustRoutes(t, moved, hq, nil)
		if g2, g3 := names(moved, p2), names(moved, p3); !slices.Equal(g2, []string{"c", "aa"}) || !slices.Equal(g3, []string{"b", "d"}) {
			t.Fatalf("after b moved: %q then %q, want [c aa] then [b d]", g2, g3)
		}
		// aa becomes an error and moves before the anchor: this pager misses it.
		raised := m(health.StateError, health.StateError) // a, aa, b, c, d, f
		hq.Cursor = pg.Next
		if got := names(raised, mustRoutes(t, raised, hq, nil)); !slices.Equal(got, []string{"c", "d"}) {
			t.Fatalf("after aa moved: %q, want [c d]", got)
		}
		// No page ever holds a row twice.
		for _, ix := range []*Index{before, moved, raised} {
			fwd, _ := walk(t, ix, Query{Sort: hq.Sort, Limit: 2})
			for _, p := range fwd {
				if c := slices.Compact(slices.Sorted(slices.Values(p))); len(c) != len(p) {
					t.Fatalf("duplicate within page %q", p)
				}
			}
		}
	})
}

func TestBadCursors(t *testing.T) {
	ix := hand(hr("a", health.StateError), hr("b", health.StateHealthy), hr("c", health.StateHealthy))
	healthNext := mustRoutes(t, ix, Query{Sort: Sort{Key: "health"}, Limit: 1}, nil).Next
	nameNext := mustRoutes(t, ix, Query{Limit: 1}, nil).Next
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for _, tc := range []struct {
		name, cursor string
		sort         Sort
	}{
		{"not base64", "!!!", Sort{}},
		{"padded base64", base64.URLEncoding.EncodeToString([]byte(`{"s":"nameX"}`)), Sort{}},
		{"not json", enc("nope"), Sort{}},
		{"too long", strings.Repeat("A", maxCursor+1), Sort{}},
		{"another sort", nameNext, Sort{Key: "health"}},
		{"another direction", healthNext, Sort{Key: "health", Desc: true}},
		{"unknown field", enc(`{"s":"name","d":"asc","b":false,"t":["shop","a","g","HTTPRoute"],"x":1}`), Sort{}},
		{"short tuple", enc(`{"s":"name","d":"asc","b":false,"t":["shop","a"]}`), Sort{}},
		{"value on name", enc(`{"s":"name","d":"asc","b":false,"v":"x","t":["shop","a","g","HTTPRoute"]}`), Sort{}},
		{"unknown health", enc(`{"s":"health","d":"asc","b":false,"v":"meh","t":["shop","a","g","HTTPRoute"]}`), Sort{Key: "health"}},
		{"null health", enc(`{"s":"health","d":"asc","b":false,"v":null,"t":["shop","a","g","HTTPRoute"]}`), Sort{Key: "health"}},
		{"trailing data", enc(`{"s":"name","d":"asc","b":false,"t":["shop","a","g","HTTPRoute"]}{}`), Sort{}},
	} {
		if _, err := ix.QueryRoutes(Query{Sort: tc.sort, Cursor: tc.cursor}, nil); !errors.Is(err, ErrBadCursor) {
			t.Errorf("%s: err %v, want ErrBadCursor", tc.name, err)
		}
	}
	// A cursor of exactly 1 KiB (768 JSON bytes) is read; its anchor sorts after every row.
	head, tail := `{"s":"name","d":"asc","b":false,"t":["zz","`, `","g","HTTPRoute"]}`
	long := enc(head + strings.Repeat("z", 768-len(head)-len(tail)) + tail)
	if len(long) != maxCursor {
		t.Fatalf("fixture is %d bytes", len(long))
	}
	if p, err := ix.QueryRoutes(Query{Cursor: long}, nil); err != nil || len(p.IDs) != 0 {
		t.Fatalf("long cursor: %v %v", p.IDs, err)
	}
}

func TestBadQueries(t *testing.T) {
	ix := hand(hr("a", health.StateHealthy))
	ix.Gateways = []GatewayRow{}
	route := routeRef("shop", "a")
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"limit over", func() error { _, err := ix.QueryRoutes(Query{Limit: MaxLimit + 1}, nil); return err }},
		{"negative limit", func() error { _, err := ix.QueryRoutes(Query{Limit: -1}, nil); return err }},
		{"routes sort on routes", func() error { _, err := ix.QueryRoutes(Query{Sort: Sort{Key: "routes"}}, nil); return err }},
		{"unknown sort", func() error { _, err := ix.QueryRoutes(Query{Sort: Sort{Key: "age"}}, nil); return err }},
		{"unknown health", func() error { _, err := ix.QueryRoutes(Query{Health: []health.State{"meh"}}, nil); return err }},
		{"attachment without parent", func() error { _, err := ix.QueryRoutes(Query{Attachment: AttachmentAccepted}, nil); return err }},
		{"declared on a Gateway", func() error {
			_, err := ix.QueryRoutes(Query{Parent: &gwObj, Attachment: AttachmentDeclared}, nil)
			return err
		}},
		{"unknown attachment", func() error { _, err := ix.QueryRoutes(Query{Parent: &gwObj, Attachment: "maybe"}, nil); return err }},
		{"route filter on gateways", func() error { _, err := ix.QueryGateways(Query{States: FlagPending}, nil); return err }},
		{"parent on gateways", func() error { _, err := ix.QueryGateways(Query{Parent: &gwObj}, nil); return err }},
		{"listenerset parent not a Gateway", func() error { _, err := ix.QueryListenerSets(Query{Parent: &route}, nil); return err }},
	} {
		if err := tc.run(); !errors.Is(err, ErrBadQuery) {
			t.Errorf("%s: err %v, want ErrBadQuery", tc.name, err)
		}
	}
	if p, err := ix.QueryRoutes(Query{}, nil); err != nil || len(p.IDs) != 1 {
		t.Fatalf("default limit: %v %v", p, err)
	}
}

func TestTotalCountsVisibleRowsOnly(t *testing.T) {
	ix := hand(hr("a", ""), hr("b", ""), hr("c", ""), hr("d", ""), hr("e", ""))
	visible := func(r objects.Ref) bool { return r.Name != "b" && r.Name != "d" }
	p := mustRoutes(t, ix, Query{Limit: 2}, visible)
	if got := names(ix, p); !slices.Equal(got, []string{"a", "c"}) || p.Total != 3 {
		t.Fatalf("page 1 %q total %d", got, p.Total)
	}
	p = mustRoutes(t, ix, Query{Limit: 2, Cursor: p.Next}, visible)
	if got := names(ix, p); !slices.Equal(got, []string{"e"}) || p.Total != 3 || p.Next != "" {
		t.Fatalf("page 2 %q total %d next %q", got, p.Total, p.Next)
	}
	// The filter and the Authorizer compose: hidden rows never count.
	if p := mustRoutes(t, ix, Query{Q: "shop/b"}, visible); p.Total != 0 || len(p.IDs) != 0 {
		t.Fatalf("hidden match counted: %+v", p)
	}
}

// TestBackendFilterCannotProbe: a denied backend and an absent one give the
// same page (design §7.4 rule 2a).
func TestBackendFilterCannotProbe(t *testing.T) {
	be := map[string]any{"group": "gateway.kgateway.dev", "kind": "Backend", "name": "be1"}
	routes := func() []*unstructured.Unstructured {
		r := testutil.Rule()
		r["backendRefs"] = []any{be}
		return []*unstructured.Unstructured{
			testutil.WithRules(testutil.HTTPRoute("shop", "r1", []objects.ParentRef{gw}, accepted(gw)), r),
			testutil.HTTPRoute("shop", "r2", []objects.ParentRef{gw}, accepted(gw)),
		}
	}
	backend := objects.Ref{Group: "gateway.kgateway.dev", Kind: "Backend", Namespace: "shop", Name: "be1"}
	denyBackend := func(r objects.Ref) bool { return r != backend }
	absent := newWorld(routes()...).build(t)
	denied := newWorld(append(routes(), testutil.Backend("shop", "be1", nil))...).build(t)
	q := Query{Backends: []objects.Ref{backend}}
	pa, pd := mustRoutes(t, absent, q, denyBackend), mustRoutes(t, denied, q, denyBackend)
	if !reflect.DeepEqual(pa, pd) || !slices.Equal(names(absent, pa), []string{"r1"}) || pa.Total != 1 {
		t.Fatalf("absent %+v %q, denied %+v %q", pa, names(absent, pa), pd, names(denied, pd))
	}
	// A hidden route that names the backend is not counted.
	hideR1 := func(r objects.Ref) bool { return denyBackend(r) && r.Name != "r1" }
	if p := mustRoutes(t, denied, q, hideR1); p.Total != 0 || len(p.IDs) != 0 {
		t.Fatalf("hidden route counted: %+v", p)
	}
}

func TestNarrowestIndexIsUsed(t *testing.T) {
	be := map[string]any{"group": "gateway.kgateway.dev", "kind": "Backend", "name": "be1"}
	r := testutil.Rule()
	r["backendRefs"] = []any{be}
	other := testutil.GwRef("infra", "other")
	ix := newWorld(
		testutil.Gateway("infra", "other", "kgateway", nil),
		testutil.WithRules(testutil.HTTPRoute("shop", "r1", []objects.ParentRef{gw}, accepted(gw)), r),
		testutil.HTTPRoute("shop", "r2", []objects.ParentRef{gw}, accepted(gw)),
		testutil.HTTPRoute("shop", "r3", []objects.ParentRef{other}, accepted(other)),
		testutil.HTTPRoute("infra", "i1", []objects.ParentRef{other}, accepted(other)),
		testutil.HTTPRoute("infra", "i2", []objects.ParentRef{other}, accepted(other)),
	).build(t)
	backend := objects.Ref{Group: "gateway.kgateway.dev", Kind: "Backend", Namespace: "shop", Name: "be1"}
	for _, tc := range []struct {
		name    string
		q       Query
		scanned int
	}{
		{"all rows", Query{}, 5},
		{"namespace", Query{Namespaces: []string{"infra"}}, 2},
		{"backend before namespace", Query{Backends: []objects.Ref{backend}, Namespaces: []string{"shop"}}, 1},
		{"parent before backend", Query{Parent: &gwObj, Backends: []objects.Ref{backend}}, 2},
		{"parent before namespace", Query{Parent: &gwObj, Namespaces: []string{"infra"}}, 2},
	} {
		if _, n, err := ix.queryRoutes(tc.q, nil); err != nil || n != tc.scanned {
			t.Errorf("%s: scanned %d (%v), want %d", tc.name, n, err, tc.scanned)
		}
	}
}

func TestUnexpectedHealthStateRanksAsUnknown(t *testing.T) {
	ix := hand(hr("a", ""), hr("b", "bogus"), hr("c", health.StateError), hr("d", health.StateHealthy), hr("e", health.StateUnknown))
	for _, desc := range []bool{false, true} {
		want := []string{"d", "a", "b", "e", "c"}
		if desc {
			want = []string{"c", "a", "b", "e", "d"}
		}
		fwd, back := walk(t, ix, Query{Sort: Sort{Key: "health", Desc: desc}, Limit: 1})
		if got := slices.Concat(fwd...); !slices.Equal(got, want) {
			t.Fatalf("desc=%v forward %q, want %q", desc, got, want)
		}
		if slices.Reverse(back); !reflect.DeepEqual(back, fwd) {
			t.Fatalf("desc=%v backward %q, want %q", desc, back, fwd)
		}
	}
}

func TestEmptyPageTurnsBack(t *testing.T) {
	model := func(keep ...string) *Index {
		var rs []RouteRow
		for _, n := range keep {
			rs = append(rs, hr(n, health.StateHealthy))
		}
		return hand(rs...)
	}
	a := model("a", "b", "c", "d", "e", "f")
	p1 := mustRoutes(t, a, Query{Limit: 2}, nil)
	p2 := mustRoutes(t, a, Query{Limit: 2, Cursor: p1.Next}, nil) // c, d
	for _, tc := range []struct {
		name     string
		ix       *Index
		cursor   string
		total    int
		turnBack bool // the empty page offers prev, else next
		want     []string
	}{
		{"successors deleted", model("a", "b", "c", "d"), p2.Next, 4, true, []string{"b", "c"}},
		{"anchor and successors deleted", model("a", "b", "c"), p2.Next, 3, true, []string{"b", "c"}},
		{"predecessors deleted", model("c", "d", "e", "f"), p2.Prev, 4, false, []string{"d", "e"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := mustRoutes(t, tc.ix, Query{Limit: 2, Cursor: tc.cursor}, nil)
			if len(p.IDs) != 0 || p.Total != tc.total {
				t.Fatalf("page %q total %d, want empty with total %d", names(tc.ix, p), p.Total, tc.total)
			}
			cur, other := p.Next, p.Prev
			if tc.turnBack {
				cur, other = p.Prev, p.Next
			}
			if cur == "" || other != "" {
				t.Fatalf("prev %q next %q", p.Prev, p.Next)
			}
			if got := names(tc.ix, mustRoutes(t, tc.ix, Query{Limit: 2, Cursor: cur}, nil)); !slices.Equal(got, tc.want) {
				t.Fatalf("turned to %q, want %q", got, tc.want)
			}
		})
	}
	// A page empty because nothing matches offers neither.
	if p := mustRoutes(t, model(), Query{Limit: 2, Cursor: p2.Next}, nil); p.Prev != "" || p.Next != "" {
		t.Fatalf("empty list: prev %q next %q", p.Prev, p.Next)
	}
}

func TestGatewayRoutesSortMatchesColumn(t *testing.T) {
	ls := objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "infra", Name: "ls"}
	other := testutil.GwRef("infra", "other")
	ix := newWorld(
		testutil.Gateway("infra", "other", "kgateway", nil),
		testutil.Gateway("infra", "third", "kgateway", nil),
		testutil.ListenerSet("infra", "ls", other, nil),
		testutil.HTTPRoute("shop", "a", []objects.ParentRef{gw}, accepted(gw)),
		testutil.HTTPRoute("shop", "bad", []objects.ParentRef{gw}, status(gw, cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners", ""), resolved)),
		testutil.HTTPRoute("shop", "pending", []objects.ParentRef{gw}),
		testutil.HTTPRoute("shop", "o1", []objects.ParentRef{other}, accepted(other)),
		testutil.HTTPRoute("shop", "through", []objects.ParentRef{ls}, accepted(ls)),
	).build(t)
	for _, vis := range []struct {
		name string
		f    func(objects.Ref) bool
	}{
		{"all", nil},
		{"two hidden", func(r objects.Ref) bool { return r.Name != "bad" && r.Name != "pending" }},
	} {
		p, err := ix.QueryGateways(Query{Sort: Sort{Key: "routes", Desc: true}}, vis.f)
		if err != nil {
			t.Fatal(err)
		}
		var counts []int
		for _, id := range p.IDs {
			g := ix.Gateways[id].Ref
			n := ix.GatewayRouteCount(id, vis.f)
			if total := mustRoutes(t, ix, Query{Parent: &g}, vis.f).Total; n != total {
				t.Errorf("%s: %s column %d, routes?parent= total %d", vis.name, g, n, total)
			}
			counts = append(counts, n)
		}
		if !slices.IsSortedFunc(counts, func(a, b int) int { return b - a }) {
			t.Errorf("%s: routes.desc gives counts %v", vis.name, counts)
		}
	}
	// edge: a, bad, pending; other: o1 and through its ListenerSet; third: none.
	if got := []int{ix.GatewayRouteCount(0, nil), ix.GatewayRouteCount(1, nil), ix.GatewayRouteCount(2, nil)}; !slices.Equal(got, []int{3, 2, 0}) {
		t.Fatalf("counts %v", got)
	}
}

func TestHTTPRouteParentAttachments(t *testing.T) {
	api := testutil.HTTPRouteRef("shop", "api")
	stale := testutil.HTTPRoute("shop", "kid-pending", []objects.ParentRef{api}, accepted(api))
	stale.SetGeneration(2) // the status describes generation 1
	ix := newWorld(
		testutil.HTTPRoute("shop", "api", []objects.ParentRef{gw}, accepted(gw)),
		testutil.HTTPRoute("shop", "kid-ok", []objects.ParentRef{api}, accepted(api)),
		testutil.HTTPRoute("shop", "kid-bad", []objects.ParentRef{api}, status(api, cond("Accepted", metav1.ConditionFalse, "NotAllowedByParent", ""), resolved)),
		stale,
		testutil.HTTPRoute("shop", "kid-declared", []objects.ParentRef{api}),
	).build(t)
	parent := api.Ref()
	for _, tc := range []struct {
		a    Attachment
		want []string
	}{
		{"", []string{"kid-bad", "kid-ok", "kid-pending"}}, // reported children only
		{AttachmentAccepted, []string{"kid-ok"}},
		{AttachmentNotAccepted, []string{"kid-bad"}},
		{AttachmentPending, []string{"kid-pending"}},
		{AttachmentDeclared, []string{"kid-declared"}},
	} {
		if got := names(ix, mustRoutes(t, ix, Query{Parent: &parent, Attachment: tc.a}, nil)); !slices.Equal(got, tc.want) {
			t.Errorf("attachment %q: got %q, want %q", tc.a, got, tc.want)
		}
		// A non-nil visible takes the Under path; the rows are the same.
		all := func(objects.Ref) bool { return true }
		if got := names(ix, mustRoutes(t, ix, Query{Parent: &parent, Attachment: tc.a}, all)); !slices.Equal(got, tc.want) {
			t.Errorf("attachment %q with visible: got %q, want %q", tc.a, got, tc.want)
		}
	}
}

// TestPagingProperty checks paging against a brute-force order over random
// rows, visibility, sorts and limits. The seeds are fixed.
func TestPagingProperty(t *testing.T) {
	nss, kindNames := []string{"a", "b", "c"}, []string{"GRPCRoute", "HTTPRoute", "TCPRoute"}
	states := append(slices.Clone(healthStates), "")
	// brute orders visible entries from scratch, independent of compareEntries.
	brute := func(es []entry, s Sort) []RowID {
		less := func(x, y entry) bool {
			var c int
			switch s.Key {
			case "name":
				c = strings.Compare(x.ref.Namespace+"\x00"+x.ref.Name+"\x00"+x.ref.Group+"\x00"+x.ref.Kind, y.ref.Namespace+"\x00"+y.ref.Name+"\x00"+y.ref.Group+"\x00"+y.ref.Kind)
				if s.Desc {
					c = -c
				}
				return c < 0
			case "kind":
				c = strings.Compare(x.ref.Group+"/"+x.ref.Kind, y.ref.Group+"/"+y.ref.Kind)
			default:
				c = x.v.n - y.v.n
			}
			if s.Desc {
				c = -c
			}
			if c != 0 {
				return c < 0
			}
			return compareRefs(x.ref, y.ref) < 0
		}
		es = slices.Clone(es)
		sort.SliceStable(es, func(i, j int) bool { return less(es[i], es[j]) })
		out := []RowID{}
		for _, e := range es {
			out = append(out, e.id)
		}
		return out
	}
	for seed := uint64(1); seed <= 200; seed++ {
		rng := rand.New(rand.NewPCG(seed, 0))
		var rows []RouteRow
		seen := map[objects.Ref]bool{}
		for range rng.IntN(30) {
			r := RouteRow{Ref: objects.Ref{Group: objects.GatewayAPIGroup, Kind: kindNames[rng.IntN(3)], Namespace: nss[rng.IntN(3)], Name: fmt.Sprintf("r%d", rng.IntN(15))},
				State: states[rng.IntN(len(states))]}
			if !seen[r.Ref] {
				seen[r.Ref] = true
				rows = append(rows, r)
			}
		}
		ix := hand(rows...)
		hidden := map[objects.Ref]bool{}
		for _, r := range ix.Routes {
			hidden[r.Ref] = rng.IntN(4) == 0
		}
		visible := func(r objects.Ref) bool { return !hidden[r] }
		var es []entry
		for i, r := range ix.Routes {
			if visible(r.Ref) {
				es = append(es, entry{id: RowID(i), ref: r.Ref, v: sortVal{n: healthRank(r.State)}})
			}
		}
		for _, key := range []string{"name", "kind", "health"} {
			for _, desc := range []bool{false, true} {
				s, limit := Sort{Key: key, Desc: desc}, 1+rng.IntN(5)
				want := brute(es, s)
				q := Query{Sort: s, Limit: limit}
				var pages []Page
				for p := mustRoutes(t, ix, q, visible); ; {
					pages = append(pages, p)
					if p.Total != len(want) {
						t.Fatalf("seed %d %+v: total %d, want %d", seed, s, p.Total, len(want))
					}
					if p.Next == "" || len(pages) > len(want)+1 {
						break
					}
					q.Cursor = p.Next
					p = mustRoutes(t, ix, q, visible)
				}
				var got []RowID
				for _, p := range pages {
					got = append(got, p.IDs...)
				}
				if !slices.Equal(got, want) {
					t.Fatalf("seed %d %+v limit %d: forward %v, want %v", seed, s, limit, got, want)
				}
				for i := len(pages) - 1; i > 0; i-- {
					q.Cursor = pages[i].Prev
					if p := mustRoutes(t, ix, q, visible); !slices.Equal(p.IDs, pages[i-1].IDs) {
						t.Fatalf("seed %d %+v limit %d: prev of page %d is %v, want %v", seed, s, limit, i, p.IDs, pages[i-1].IDs)
					}
				}
			}
		}
	}
}

// FuzzDecodeCursor: the decoder never panics, and what it accepts re-encodes
// to a cursor that decodes to the same anchor.
func FuzzDecodeCursor(f *testing.F) {
	ix := hand(hr("a", health.StateError), hr("b", ""))
	for _, key := range []string{"name", "kind", "health"} {
		p, _ := ix.QueryRoutes(Query{Sort: Sort{Key: key}, Limit: 1}, nil)
		f.Add(p.Next, uint8(0), false)
	}
	f.Add(base64.RawURLEncoding.EncodeToString([]byte(`{"s":"routes","d":"desc","b":true,"v":3,"t":["","g","x","Gateway"]}`)), uint8(3), true)
	f.Add("!!!", uint8(0), false)
	keys := []string{sortName, sortKind, sortHealth, sortRoutes}
	f.Fuzz(func(t *testing.T, c string, key uint8, desc bool) {
		// Only validated sort keys reach the decoder.
		s := Sort{Key: keys[int(key)%len(keys)], Desc: desc}
		cur, err := decodeCursor(c, s)
		if err != nil {
			return
		}
		again, err := decodeCursor(encodeCursor(s, cur.before, cur.anchor), s)
		if err != nil || again != cur {
			t.Fatalf("round trip %+v → %+v, %v", cur, again, err)
		}
	})
}
