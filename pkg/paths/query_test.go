package paths_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
)

var gwSide = testutil.GwRef("infra", "side")

// queryWorld has 7 rows: shop/r1 on both listeners of infra/edge with two
// hostnames and refs a and b; other/r2 on infra/side with a ref to cart and a
// mirror to shadow; GRPCRoute shop/g on infra/side to g1. Only a is ready.
func queryWorld(t *testing.T) (*paths.Topology, *paths.Projection) {
	t.Helper()
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{1}, 1)
	svcs[0].Name, eps[0].Service = "a", "a"
	mirror := map[string]any{"type": "RequestMirror", "requestMirror": map[string]any{"backendRef": map[string]any{"name": "shadow", "port": int64(8080)}}}
	r1 := testutil.WithHostnames(testutil.WithRules(testutil.HTTPRoute("shop", "r1", []objects.ParentRef{gwEdge}, testutil.AcceptedBy(gwEdge, testutil.Controller)),
		rule([]any{svc("a"), svc("b")})), "a.example.com", "b.example.com")
	r2 := testutil.WithHostnames(testutil.WithRules(testutil.HTTPRoute("other", "r2", []objects.ParentRef{gwSide}, testutil.AcceptedBy(gwSide, testutil.Controller)),
		rule([]any{svc("cart")}, mirror)), "z.example.com")
	g := kindRoute(testutil.WithRules(testutil.HTTPRoute("shop", "g", []objects.ParentRef{gwSide}, testutil.AcceptedBy(gwSide, testutil.Controller)),
		rule([]any{svc("g1")})), "gateway.networking.k8s.io/v1", "GRPCRoute")
	side := testutil.Gateway("infra", "side", "kgateway", nil, testutil.Listener{Name: "http", Conds: ready})
	top, p, _ := project(t, endpointsSnap(t, svcs, eps, edge("one", "two"), side, r1, r2, g), "on")
	if len(top.Rows) != 7 {
		t.Fatalf("rows: %d", len(top.Rows))
	}
	return top, p
}

// run is paths.Run that fails the test on an error.
func run(t *testing.T, top *paths.Topology, p *paths.Projection, q paths.Query, visible func(explore.DepSet) bool) paths.Page {
	t.Helper()
	pg, err := paths.Run(top, p, q, visible)
	if err != nil {
		t.Fatalf("query %+v: %v", q, err)
	}
	return pg
}

// backendOf is a row's backend or mirror target; false for a written action or a delegation.
func backendOf(top *paths.Topology, r paths.Row) (objects.Ref, bool) {
	if m, ok := top.Mirror(r); ok {
		return m.Backend, true
	}
	if b, ok := top.Backend(r); ok && b.Delegation == nil {
		return b.Ref, true
	}
	return objects.Ref{}, false
}

// TestRunFilters: each filter selects exactly the rows its predicate holds
// for, in the default problem-first order, and selects some but not all.
func TestRunFilters(t *testing.T) {
	top, p := queryWorld(t)
	routeR2 := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "other", Name: "r2"}
	gwRef := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}
	healthy := 0
	for _, pr := range p.Problems {
		if pr.State == health.StateHealthy {
			healthy++
		}
	}
	if healthy == 0 || healthy == len(p.Problems) {
		t.Fatalf("want mixed states, %d of %d healthy", healthy, len(p.Problems))
	}
	cases := []struct {
		name string
		q    paths.Query
		want func(r paths.Row, i int32) bool
	}{
		{"gateway", paths.Query{Gateways: []objects.Ref{gwRef}}, func(r paths.Row, _ int32) bool { return top.Gateway(r) == gwRef }},
		{"listener", paths.Query{Listener: "two"}, func(r paths.Row, _ int32) bool { return top.Listener(r).Name == "two" }},
		{"route", paths.Query{Route: &routeR2}, func(r paths.Row, _ int32) bool { return slices.Contains(top.Chain(r), routeR2) }},
		{"backend", paths.Query{Backends: []objects.Ref{ref("shop", "b"), ref("other", "shadow")}}, func(r paths.Row, _ int32) bool {
			b, ok := backendOf(top, r)
			return ok && (b == ref("shop", "b") || b == ref("other", "shadow"))
		}},
		{"namespace", paths.Query{Namespace: "other"}, func(r paths.Row, _ int32) bool { return top.Chain(r)[0].Namespace == "other" }},
		{"state", paths.Query{States: []health.State{health.StateHealthy}}, func(_ paths.Row, i int32) bool { return p.Problems[i].State == health.StateHealthy }},
		{"kind", paths.Query{Kind: "GRPCRoute"}, func(r paths.Row, _ int32) bool { return top.Chain(r)[0].Kind == "GRPCRoute" }},
		{"mirror only", paths.Query{Mirror: "only"}, func(r paths.Row, _ int32) bool { _, ok := top.Mirror(r); return ok }},
		{"mirror exclude", paths.Query{Mirror: "exclude"}, func(r paths.Row, _ int32) bool { _, ok := top.Mirror(r); return !ok }},
		// q matches any hostname, not only the first.
		{"q hostname", paths.Query{Q: "B.EXAMPLE"}, func(r paths.Row, _ int32) bool { return slices.Contains(top.Hostnames(r), "b.example.com") }},
		{"q route", paths.Query{Q: "r2"}, func(r paths.Row, _ int32) bool { return top.Chain(r)[0].Name == "r2" }},
		{"q backend", paths.Query{Q: "g1"}, func(r paths.Row, _ int32) bool { b, ok := backendOf(top, r); return ok && b.Name == "g1" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.q.Limit = paths.MaxLimit
			var want []int32
			for _, i := range p.Order {
				if c.want(top.Rows[i], i) {
					want = append(want, i)
				}
			}
			if len(want) == 0 || len(want) == len(top.Rows) {
				t.Fatalf("predicate selects %d of %d rows", len(want), len(top.Rows))
			}
			pg := run(t, top, p, c.q, nil)
			if !slices.Equal(pg.Rows, want) || pg.Summary.Paths != len(want) || pg.Next != "" || pg.Prev != "" {
				t.Fatalf("rows %v, want %v; summary %+v", pg.Rows, want, pg.Summary)
			}
		})
	}
}

// TestRunSortHostname: sort=hostname orders by each row's first hostname,
// descending with desc, ties in problem-first order.
func TestRunSortHostname(t *testing.T) {
	top, p := queryWorld(t)
	first := func(i int32) string {
		if h := top.Hostnames(top.Rows[i]); len(h) > 0 {
			return h[0]
		}
		return ""
	}
	for _, desc := range []bool{false, true} {
		pg := run(t, top, p, paths.Query{Sort: paths.SortHostname, Desc: desc, Limit: paths.MaxLimit}, nil)
		var got []string
		for _, i := range pg.Rows {
			got = append(got, first(i))
		}
		// The GRPCRoute has no hostnames; rows without one sort last either way.
		want := []string{"a.example.com", "a.example.com", "a.example.com", "a.example.com", "z.example.com", "z.example.com", ""}
		if desc {
			want = []string{"z.example.com", "z.example.com", "a.example.com", "a.example.com", "a.example.com", "a.example.com", ""}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("desc %v: %q", desc, got)
		}
	}
}

// TestRunPaging: for every sort, three pages of limit 2 over 6 rows visit
// each row once in the full order; a cursor is stable when re-run, and prev
// from the last page returns the middle one.
func TestRunPaging(t *testing.T) {
	top, p := queryWorld(t)
	base := paths.Query{Mirror: "exclude"} // 6 rows
	for _, s := range []string{"", paths.SortProblem, paths.SortGateway, paths.SortRoute, paths.SortBackend, paths.SortHostname} {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s desc=%v", s, desc), func(t *testing.T) {
				q := base
				q.Sort, q.Desc, q.Limit = s, desc, paths.MaxLimit
				all := run(t, top, p, q, nil).Rows
				if len(all) != 6 {
					t.Fatalf("rows: %d", len(all))
				}
				q.Limit = 2
				var got []int32
				var pages []paths.Page
				for range 3 {
					pg := run(t, top, p, q, nil)
					if again := run(t, top, p, q, nil); !slices.Equal(again.Rows, pg.Rows) || again.Next != pg.Next {
						t.Fatalf("cursor %q not stable", q.Cursor)
					}
					got = append(got, pg.Rows...)
					pages = append(pages, pg)
					q.Cursor = pg.Next
				}
				if !slices.Equal(got, all) || pages[2].Next != "" || pages[0].Prev != "" {
					t.Fatalf("pages %v, want %v; last next %q", got, all, pages[2].Next)
				}
				q.Cursor = pages[2].Prev
				if back := run(t, top, p, q, nil); !slices.Equal(back.Rows, pages[1].Rows) {
					t.Fatalf("prev: %v, want %v", back.Rows, pages[1].Rows)
				}
			})
		}
	}
}

// TestRunBadInput: bad values are ErrBadQuery, bad cursors ErrBadCursor.
func TestRunBadInput(t *testing.T) {
	top, p := queryWorld(t)
	next := run(t, top, p, paths.Query{Limit: 2}, nil).Next
	for _, q := range []paths.Query{{Limit: -1}, {Limit: paths.MaxLimit + 1}, {Sort: "name"}, {Mirror: "some"}, {States: []health.State{"bad"}}} {
		if _, err := paths.Run(top, p, q, nil); !errors.Is(err, explore.ErrBadQuery) {
			t.Fatalf("%+v: %v", q, err)
		}
	}
	for _, q := range []paths.Query{{Cursor: "!"}, {Cursor: "e30"}, {Cursor: next, Sort: paths.SortGateway}, {Cursor: next, Desc: true}} {
		if _, err := paths.Run(top, p, q, nil); !errors.Is(err, explore.ErrBadCursor) {
			t.Fatalf("%+v: %v", q, err)
		}
	}
}

// TestRunVisibility: hiding infra/edge drops its rows before counting and
// paging; the summary counts only visible rows and says some are hidden.
func TestRunVisibility(t *testing.T) {
	top, p := queryWorld(t)
	edgeRef := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}
	hide := func(d explore.DepSet) bool {
		return !slices.ContainsFunc(d, func(i int32) bool { return top.Refs[i] == edgeRef })
	}
	all := run(t, top, p, paths.Query{}, nil)
	if all.Summary.Hidden || all.Summary.Gateways != 2 || all.Summary.Paths != 7 {
		t.Fatalf("all: %+v", all.Summary)
	}
	pg := run(t, top, p, paths.Query{Limit: 2}, hide)
	s := pg.Summary
	if !s.Hidden || s.Paths != 3 || s.Gateways != 1 || s.Routes != 2 || s.Backends != 3 {
		t.Fatalf("summary %+v", s)
	}
	n := 0
	for _, c := range s.ByState {
		n += c.Paths
	}
	if n != 3 {
		t.Fatalf("by state %+v", s.ByState)
	}
	var seen []int32
	for q := (paths.Query{Limit: 2}); ; {
		pg := run(t, top, p, q, hide)
		seen = append(seen, pg.Rows...)
		if q.Cursor = pg.Next; q.Cursor == "" {
			break
		}
	}
	for _, i := range seen {
		if top.Gateway(top.Rows[i]) == edgeRef {
			t.Fatalf("hidden row %d listed", i)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("paged %d rows", len(seen))
	}
	// A Gateway filter on the hidden Gateway finds nothing and still says rows are hidden.
	if pg := run(t, top, p, paths.Query{Gateways: []objects.Ref{edgeRef}}, hide); len(pg.Rows) != 0 || pg.Summary.Gateways != 0 || !pg.Summary.Hidden {
		t.Fatalf("hidden gateway filter: %+v", pg)
	}
}

// TestRunSharedBackendSummary: one Service with no ready endpoints behind 37
// routes on 4 Gateways summarises as 1 backend, 37 routes and 4 Gateways.
func TestRunSharedBackendSummary(t *testing.T) {
	svcs, eps := cart([]cache.ServicePort{httpPort}, []int{0}, 1)
	objs := []*unstructured.Unstructured{}
	for g := range 4 {
		objs = append(objs, testutil.Gateway("infra", fmt.Sprintf("gw%d", g), "kgateway", nil, testutil.Listener{Name: "http", Conds: ready}))
	}
	for r := range 37 {
		parent := testutil.GwRef("infra", fmt.Sprintf("gw%d", r%4))
		objs = append(objs, testutil.WithRules(testutil.HTTPRoute("shop", fmt.Sprintf("r%02d", r), []objects.ParentRef{parent},
			testutil.AcceptedBy(parent, testutil.Controller)), rule([]any{svc("cart")})))
	}
	objs = append(objs, accepted("ok", svc("other")))
	top, p, _ := projectAt(t, endpointsSnap(t, svcs, eps, append(objs, edge("http"))...), "on", matured)
	cartRef := ref("shop", "cart")
	pg := run(t, top, p, paths.Query{Backends: []objects.Ref{cartRef}}, nil)
	s := pg.Summary
	if s.Paths != 37 || s.Routes != 37 || s.Gateways != 4 || s.Backends != 1 || s.EndpointsNotRead {
		t.Fatalf("summary %+v", s)
	}
	for st, c := range s.ByState {
		if c.Paths > 0 && (c.Backends != 1 || st == health.StateHealthy) {
			t.Fatalf("%s: %+v", st, c)
		}
	}
	if all := run(t, top, p, paths.Query{}, nil).Summary; all.Paths != 38 || all.Routes != 38 || all.Gateways != 5 || all.Backends != 2 {
		t.Fatalf("all: %+v", all)
	}
}

// TestRunEndpointsNotRead: with the endpoints capability off, the summary
// says endpoints were not read, so a count never implies full coverage.
func TestRunEndpointsNotRead(t *testing.T) {
	top, p, _ := project(t, worldSnap(t, "v2.4.7", nil, edge("http"), accepted("r", svc("cart"))), "off")
	if s := run(t, top, p, paths.Query{}, nil).Summary; !s.EndpointsNotRead || s.Paths != 1 {
		t.Fatalf("summary %+v", s)
	}
}

// TestRunRouteMatchesChain: route= matches a delegated row through its root
// route and through its child route.
func TestRunRouteMatchesChain(t *testing.T) {
	r := root(prefixes([]string{"/api"}, testutil.DelegateTo("shop", "cart")))
	c := child("cart", true, prefixes([]string{"/cart"}, svc("c")))
	top, p, _ := project(t, worldSnap(t, "v2.5.0", nil, edge("http"), r, c), "on")
	rows := delegated(top)
	if len(rows) != 1 {
		t.Fatalf("delegated rows: %d", len(rows))
	}
	for _, rt := range []objects.Ref{route("shop", "api"), route("shop", "cart")} {
		pg := run(t, top, p, paths.Query{Route: &rt, Limit: paths.MaxLimit}, nil)
		if !slices.ContainsFunc(pg.Rows, func(i int32) bool { return len(top.Chain(top.Rows[i])) == 2 }) {
			t.Fatalf("route %v: rows %v", rt, pg.Rows)
		}
	}
}
