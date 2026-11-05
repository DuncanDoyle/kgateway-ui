package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/computedtest"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// refsRouteAs is shop/name on infra/edge with ResolvedRefs=False/reason, the
// condition message msg, and refs as its backendRefs.
func refsRouteAs(name, reason, msg string, refs ...any) *unstructured.Unstructured {
	gw := testutil.GwRef("infra", "edge")
	c := testutil.Cond("ResolvedRefs", metav1.ConditionFalse, reason)
	c.Message = msg
	u := testutil.HTTPRoute("shop", name, []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), c}})
	return testutil.WithRules(u, map[string]any{"backendRefs": refs})
}

// cartSvc is the backend the groups world's routes cannot find.
var cartSvc = objects.Ref{Kind: "Service", Namespace: "shop", Name: "cart"}

// hideCart hides the Service shop/cart, the cause of the "Missing backend" group.
var hideCart = AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != cartSvc })

// groupsWorld: web and api name the missing Service shop/cart, one by the
// defaults and one written out (one Missing backend group); x and y need a
// grant to store Services (one Not permitted group); two names two refs.
func groupsWorld(t *testing.T, authz Authorizer) (*engine.Engine, *httptest.Server) {
	t.Helper()
	e, srv, _ := startWith(t, setup{authz: authz, catalog: fixCatalog(nil), rulesFor: computedtest.NoViews}, world(
		refsRouteAs("web", "BackendNotFound", "Service not found", map[string]any{"name": "cart", "port": int64(80)}),
		refsRouteAs("api", "BackendNotFound", "Service not found", map[string]any{"group": "", "kind": "Service", "name": "cart", "port": int64(80)}),
		refsRouteAs("x", "RefNotPermitted", "no grant", map[string]any{"name": "cart", "namespace": "store"}),
		refsRouteAs("y", "RefNotPermitted", "no grant", map[string]any{"name": "pay", "namespace": "store"}),
		refsRouteAs("two", "BackendNotFound", "Service not found", map[string]any{"name": "a"}, map[string]any{"name": "b"}),
	)...)
	return e, srv
}

// groupPages pages GET findings?view=groups&query from cursor, following next
// (or prev when back), and returns the group ids in page order.
func groupPages(t *testing.T, srv *httptest.Server, query, cursor string, back bool) ([]string, []Page[GroupRow]) {
	t.Helper()
	next := func(c string) string { return findingsBase + "?view=groups&" + query + "&cursor=" + url.QueryEscape(c) }
	path := findingsBase + "?view=groups&" + query
	if cursor != "" {
		path = next(cursor)
	}
	var ids []string
	var pages []Page[GroupRow]
	for range 1000 {
		pg := get[Page[GroupRow]](t, srv, path, http.StatusOK)
		pages = append(pages, pg)
		for _, r := range pg.Items {
			ids = append(ids, r.ID)
		}
		c := pg.Next
		if back {
			c = pg.Prev
		}
		if c == "" {
			return ids, pages
		}
		path = next(c)
	}
	t.Fatalf("%s: no last page", query)
	return nil, nil
}

// rowOf returns the row of rows whose code is code and members n.
func rowOf(t *testing.T, rows []GroupRow, code string, n int) GroupRow {
	t.Helper()
	for _, r := range rows {
		if r.Code == code && r.Members == n {
			return r
		}
	}
	t.Fatalf("no %s group of %d in %+v", code, n, rows)
	return GroupRow{}
}

// Design §5.2, §17.1 and R77: the grouped view pages every group in rank
// order, one row per group; omitted and written core refs to one backend share
// a "Missing backend" group, and 014s of one shape a "Not permitted" group.
func TestFindingsGroupsView(t *testing.T) {
	e, srv := groupsWorld(t, nil)
	var want []string
	for _, g := range e.Current().Model.Installs[0].Diagnosis.Groups {
		want = append(want, g.ID)
	}
	all := get[Page[GroupRow]](t, srv, findingsBase+"?view=groups", http.StatusOK)
	if got := groupIDs(all.Items); !slices.Equal(got, want) || all.Total != len(want) || all.Next != "" {
		t.Fatalf("groups %v (total %d), want %v", got, all.Total, want)
	}
	fwd, pages := groupPages(t, srv, "limit=1", "", false)
	if !slices.Equal(fwd, want) || pages[0].Total != len(want) {
		t.Fatalf("paged forward %v, want %v", fwd, want)
	}
	last := pages[len(pages)-1]
	back, _ := groupPages(t, srv, "limit=1", last.Prev, true)
	if slices.Reverse(back); !slices.Equal(back, want[:len(want)-1]) {
		t.Fatalf("paged back %v, want %v", back, want[:len(want)-1])
	}

	mb := rowOf(t, all.Items, health.CodeRefUnresolved, 2)
	if mb.Cause == nil || mb.Cause.Ref != cartSvc || mb.AffectedTotal != 2 || mb.Restricted || mb.Title != "Backend not found" ||
		mb.Affected[0].Name != "api" || mb.Affected[1].Name != "web" {
		t.Errorf("missing backend row %+v", mb)
	}
	if np := rowOf(t, all.Items, health.CodeRefNotPermitted, 2); np.Cause != nil || np.AffectedTotal != 2 {
		t.Errorf("not permitted row %+v", np)
	}
	// The unattributed 013 is in "Other", on its own route.
	if o := rowOf(t, all.Items, health.CodeRefUnresolved, 1); o.Cause == nil || o.Cause.Name != "two" {
		t.Errorf("unattributed row %+v", o)
	}

	filtered := func(query string) []GroupRow {
		return get[Page[GroupRow]](t, srv, findingsBase+"?view=groups&"+query, http.StatusOK).Items
	}
	if rows := filtered("code=KGWUI-014"); len(rows) != 1 || rows[0].Members != 2 {
		t.Errorf("code filter %+v", rows)
	}
	if rows := filtered("code=KGWUI-013&q=two"); len(rows) != 1 || rows[0].Cause == nil || rows[0].Cause.Name != "two" {
		t.Errorf("q filter %+v", rows)
	}
	// q matches a group by its cause's identity too.
	if rows := filtered("q=shop/cart&code=KGWUI-013"); len(rows) != 1 || rows[0].ID != mb.ID {
		t.Errorf("cause identity %+v", rows)
	}
}

// R98: the grouped view lists the groups of the findings the flat view shows.
// shop/mixed has a 013 error and a 003 warning in one "Other" group: a filter
// on the 003 lists the group, and the row keeps its top member's code and severity.
func TestFindingsGroupsMatchNonTopMember(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	unreported := gw
	unreported.SectionName = "nope" // no status entry: a 003 warning past grace
	mixed := testutil.HTTPRoute("shop", "mixed", []objects.ParentRef{gw, unreported}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	mixed = testutil.WithRules(mixed, map[string]any{"backendRefs": []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}}})
	_, srv, _ := startWith(t, setup{catalog: fixCatalog(nil), rulesFor: computedtest.NoViews}, world(mixed)...)
	flat := get[Page[FindingRow]](t, srv, findingsBase+"?namespace=shop&code=KGWUI-003&code=KGWUI-013", http.StatusOK).Items
	if len(flat) != 2 || flat[0].Group != flat[1].Group {
		t.Fatalf("want a 013 and a 003 on shop/mixed in one group, got %+v", flat)
	}
	for _, q := range []string{"code=KGWUI-003", "severity=warning"} {
		rows := get[Page[GroupRow]](t, srv, findingsBase+"?view=groups&"+q, http.StatusOK).Items
		j := slices.IndexFunc(rows, func(r GroupRow) bool { return r.ID == flat[0].Group })
		if j < 0 {
			t.Fatalf("%s: group of shop/mixed not listed in %+v", q, rows)
		}
		if r := rows[j]; r.Code != health.CodeRefUnresolved || r.Severity != health.SeverityError || r.Members != 2 {
			t.Errorf("%s: row %+v, want the top member's code KGWUI-013 and severity error", q, r)
		}
	}
}

func groupIDs(rows []GroupRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// R77: a bad view, a bad filter value, a filter the grouped view does not
// take, and a cursor of the flat list give 400.
func TestFindingsGroupsBadQuery(t *testing.T) {
	_, srv := groupsWorld(t, nil)
	flat := get[Page[FindingRow]](t, srv, findingsBase+"?limit=1", http.StatusOK)
	grouped := get[Page[GroupRow]](t, srv, findingsBase+"?view=groups&limit=1", http.StatusOK)
	for _, q := range []string{"view=list", "view=", "view=groups&view=groups", "view=groups&severity=bad", "view=groups&code=x",
		"view=groups&kind=core/Service", "view=groups&namespace=shop", "view=groups&group=f1-aaaaaaaaaaaaaaaaaaaaaaaaaa", "view=groups&sort=since",
		"view=groups&limit=0", "view=groups&cursor=" + url.QueryEscape(flat.Next), "cursor=" + url.QueryEscape(grouped.Next)} {
		get[map[string]any](t, srv, findingsBase+"?"+q, http.StatusBadRequest)
	}
}

// Design §14 rule 13 and §5.2: a group whose cause the caller cannot view is a
// restricted row (severity, code, counts) and q matches it by identity only.
func TestFindingsGroupsRestricted(t *testing.T) {
	_, srv := groupsWorld(t, hideCart)
	rows := get[Page[GroupRow]](t, srv, findingsBase+"?view=groups", http.StatusOK).Items
	r := rowOf(t, rows, health.CodeRefUnresolved, 2)
	if !r.Restricted || r.Title != "" || len(r.Affected) != 0 || r.AffectedTotal != 0 || r.Since != nil || r.NextSteps != nil ||
		r.Severity != health.SeverityError || r.Cause == nil || r.Cause.Ref != cartSvc {
		t.Fatalf("restricted row %+v", r)
	}
	count := func(query string) int {
		n := 0
		for _, row := range get[Page[GroupRow]](t, srv, findingsBase+"?view=groups&code=KGWUI-013&"+query, http.StatusOK).Items {
			if row.ID == r.ID {
				n++
			}
		}
		return n
	}
	if count("q=not+found") != 0 {
		t.Error("q matched a restricted group by its members' message")
	}
	if count("q=shop/web") != 1 || count("q=shop/cart") != 1 {
		t.Error("q did not match a restricted group by identity")
	}
}
