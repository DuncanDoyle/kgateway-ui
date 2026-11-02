package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/computedtest"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/diagnose"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

const findingsBase = "/api/v1/installs/kgw.kgateway/findings"

// hideBroken hides the route shop/broken, as the Phase 1 goldens do.
var hideBroken = AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != route("shop", "broken") })

// findingsWorld is the Phase 1 golden world: broken (KGWUI-002, ResolvedRefs
// BackendNotFound), denied (KGWUI-002, RefNotPermitted), blog/posts
// (KGWUI-004) and young (KGWUI-003, inside the grace period).
func findingsWorld(t *testing.T, authz Authorizer) (*engine.Engine, *httptest.Server) {
	t.Helper()
	gw := testutil.GwRef("infra", "edge")
	cond := func(reason string) testutil.RouteParent {
		return testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
			Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, reason)}}
	}
	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, cond("BackendNotFound"))
	denied := testutil.HTTPRoute("shop", "denied", []objects.ParentRef{gw}, cond("RefNotPermitted"))
	e, srv, _ := startWith(t, setup{authz: authz, late: []*unstructured.Unstructured{testutil.HTTPRoute("shop", "young", []objects.ParentRef{gw})},
		rulesFor: computedtest.NoViews}, frozenWorld(broken, denied)...)
	return e, srv
}

// allPages pages GET base?query from srv, from cursor, following next (or prev when back) to the end.
func allPages(t *testing.T, srv *httptest.Server, base, query, cursor string, back bool) (ids []string, pages []Page[FindingRow]) {
	t.Helper()
	path := base + "?" + query
	if cursor != "" {
		path += "&cursor=" + url.QueryEscape(cursor)
	}
	for range 1000 {
		pg := get[Page[FindingRow]](t, srv, path, http.StatusOK)
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
		path = base + "?" + query + "&cursor=" + url.QueryEscape(c)
	}
	t.Fatalf("%s: no last page", query)
	return nil, nil
}

// largeSnap serves a fixed snapshot.
type largeSnap struct{ s *cache.Snapshot }

func (s largeSnap) Snapshot() *cache.Snapshot { return s.s }

// largeContradicting serves the Large tier with every view A check
// contradicting, computed past every window so each KGWUI-012 has matured.
func largeContradicting(t *testing.T) (*httptest.Server, *engine.Model) {
	t.Helper()
	snap := scale.Snapshot(scale.Large.Contradicting(), 1)
	for _, objs := range snap.Objects {
		slices.SortFunc(objs, func(x, y *unstructured.Unstructured) int {
			return strings.Compare(x.GetNamespace()+"/"+x.GetName(), y.GetNamespace()+"/"+y.GetName())
		})
	}
	now := testutil.Now
	e := engine.New(engine.Config{Snapshots: largeSnap{snap}, Served: testutil.ServedUpstream(), Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour), Metrics: okMetrics("kgw.kgateway"),
		Checks: health.Upstream(), Now: func() time.Time { return now }})
	e.Compute()
	now = now.Add(max(engine.Window, health.Grace) + time.Second)
	m := e.Compute()
	srv := httptest.NewServer(NewHandler(e, fstest.MapFS{}, nil))
	t.Cleanup(srv.Close)
	return srv, m
}

// Paging forward and back through every KGWUI-012 at Large gives the same
// set, once each, in every order; rank pages follow the diagnosis.
func TestFindingsPaging(t *testing.T) {
	srv, m := largeContradicting(t)
	i := &m.Installs[0]
	base := "/api/v1/installs/" + i.Resolved.ID() + "/findings"
	var want []string
	for _, k := range i.Diagnosis.Rank {
		if f := i.Health.Findings[k]; f.Code == health.CodeComputedDisagrees {
			want = append(want, f.ID)
		}
	}
	if len(want) < 3000 {
		t.Fatalf("%d KGWUI-012 at Large, want the worst case of more than 3,000", len(want))
	}
	for _, sort := range []string{"", "rank", "since", "-since", "code", "-code"} {
		t.Run("sort="+sort, func(t *testing.T) {
			q := "code=" + health.CodeComputedDisagrees + "&limit=200"
			if sort != "" {
				q += "&sort=" + sort
			}
			fwd, pages := allPages(t, srv, base, q, "", false)
			if pages[0].Total != len(want) || pages[0].Prev != "" {
				t.Fatalf("total %d prev %q, want %d and none", pages[0].Total, pages[0].Prev, len(want))
			}
			if !slices.Equal(slices.Sorted(slices.Values(fwd)), slices.Sorted(slices.Values(want))) {
				t.Fatalf("forward: %d ids, want the %d KGWUI-012 once each", len(fwd), len(want))
			}
			if sort == "" || sort == "rank" {
				if !slices.Equal(fwd, want) {
					t.Fatal("rank pages are not in diagnosis rank order")
				}
			}
			// Back from the last page's prev cursor: the same pages, in reverse.
			last := pages[len(pages)-1]
			back, bpages := allPages(t, srv, base, q, last.Prev, true)
			back = append(back, ids(last.Items)...)
			if len(bpages) != len(pages)-1 {
				t.Fatalf("%d pages back, want %d", len(bpages), len(pages)-1)
			}
			var rebuilt []string
			for k := len(bpages) - 1; k >= 0; k-- {
				rebuilt = append(rebuilt, ids(bpages[k].Items)...)
			}
			rebuilt = append(rebuilt, ids(last.Items)...)
			if !slices.Equal(rebuilt, fwd) || len(back) != len(fwd) {
				t.Fatal("paging back does not give the forward pages")
			}
		})
	}
}

func ids(rows []FindingRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// every returns the IDs of the model's findings that keep returns true for, in rank order.
func every(i *engine.InstallModel, keep func(health.Finding) bool) []string {
	out := []string{}
	for _, k := range i.Diagnosis.Rank {
		if f := i.Health.Findings[k]; keep(f) {
			out = append(out, f.ID)
		}
	}
	return out
}

func TestFindingsFilters(t *testing.T) {
	e, srv := findingsWorld(t, nil)
	i := &e.Current().Model.Installs[0]
	var denied health.Finding
	for _, f := range i.Health.Findings {
		if f.Resource != nil && f.Resource.Name == "denied" {
			denied = f
		}
	}
	httproute := objects.GatewayAPIGroup + "/HTTPRoute"
	for _, tc := range []struct {
		query string
		keep  func(health.Finding) bool
		n     int
	}{
		{"", func(health.Finding) bool { return true }, 4},
		{"severity=error", func(f health.Finding) bool { return f.Severity == health.SeverityError }, 2},
		{"severity=warning&severity=info", func(f health.Finding) bool { return f.Severity != health.SeverityError }, 2},
		{"code=KGWUI-004", func(f health.Finding) bool { return f.Code == "KGWUI-004" }, 1},
		{"code=KGWUI-002&code=KGWUI-003", func(f health.Finding) bool { return f.Code == "KGWUI-002" || f.Code == "KGWUI-003" }, 3},
		{"code=ACME-001", func(health.Finding) bool { return false }, 0},
		{"kind=" + httproute, func(f health.Finding) bool { return f.Resource != nil && f.Resource.Kind == "HTTPRoute" }, 4},
		{"kind=core/Service", func(health.Finding) bool { return false }, 0},
		{"namespace=blog", func(f health.Finding) bool { return f.Resource != nil && f.Resource.Namespace == "blog" }, 1},
		{"group=" + denied.Group, func(f health.Finding) bool { return f.Group == denied.Group }, 1},
		{"q=DENIED", func(f health.Finding) bool { return f.ID == denied.ID }, 1},
		{"q=" + url.QueryEscape("ReferenceGrant"), func(f health.Finding) bool { return f.ID == denied.ID }, 1},
		{"q=" + url.QueryEscape("parent Gateway infra/edge"), func(f health.Finding) bool { return strings.Contains(f.Detail, "parent Gateway infra/edge") }, 3},
		{"severity=error&namespace=shop&q=den", func(f health.Finding) bool { return f.ID == denied.ID }, 1},
		{"severity=info&namespace=blog", func(health.Finding) bool { return false }, 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got, pages := allPages(t, srv, findingsBase, tc.query, "", false)
			want := every(i, tc.keep)
			if len(want) != tc.n || !slices.Equal(got, want) || pages[0].Total != tc.n {
				t.Errorf("got %v (total %d), want %v (%d)", got, pages[0].Total, want, tc.n)
			}
		})
	}
	for _, q := range []string{"severity=fatal", "code=nope", "code=", "kind=HTTPRoute", "kind=a/b/c", "group=g1", "group=f1-x", "namespace=",
		"sort=-rank", "sort=name", "sort=since.desc", "limit=0", "limit=201", "q=a&q=b", "group=" + denied.Group + "&group=" + denied.Group,
		"bogus=1", "cursor=nope", "health=error"} {
		get[ErrorResponse](t, srv, findingsBase+"?"+q, http.StatusBadRequest)
	}
	// Foreign cursors: a route list's, and one of another findings sort.
	routes := get[Page[RouteRow]](t, srv, "/api/v1/installs/kgw.kgateway/routes?limit=1", http.StatusOK)
	since := get[Page[FindingRow]](t, srv, findingsBase+"?sort=since&limit=1", http.StatusOK)
	if routes.Next == "" || since.Next == "" {
		t.Fatal("no cursors to reuse")
	}
	get[ErrorResponse](t, srv, findingsBase+"?cursor="+url.QueryEscape(routes.Next), http.StatusBadRequest)
	get[ErrorResponse](t, srv, findingsBase+"?sort=-since&cursor="+url.QueryEscape(since.Next), http.StatusBadRequest)
	get[ErrorResponse](t, srv, "/api/v1/installs/kgw.kgateway/routes?cursor="+url.QueryEscape(since.Next), http.StatusBadRequest)
	get[ErrorResponse](t, srv, "/api/v1/installs/nope/findings", http.StatusNotFound)
}

// Design §14 rule 17: q never matches a restricted finding's hidden message
// or detail, nor an evidence-restricted finding's hidden detail, and total
// counts restricted findings under every other filter.
func TestFindingsFiltersRevealNothingHidden(t *testing.T) {
	e, all := findingsWorld(t, nil)
	_, some := findingsWorld(t, hideBroken)
	var broken health.Finding
	for _, f := range e.Current().Model.Installs[0].Health.Findings {
		if f.Resource != nil && f.Resource.Name == "broken" {
			broken = f
		}
	}
	total := func(srv *httptest.Server, q string) int {
		return get[Page[FindingRow]](t, srv, findingsBase+"?"+q, http.StatusOK).Total
	}
	for _, text := range []string{broken.Message, broken.Detail, "BackendNotFound"} {
		q := "q=" + url.QueryEscape(text)
		if total(all, q) != 1 || total(some, q) != 0 {
			t.Errorf("%s: allow-all %d, restricted %d; want 1 and 0", q, total(all, q), total(some, q))
		}
	}
	for _, q := range []string{"", "severity=error", "code=KGWUI-002", "kind=gateway.networking.k8s.io/HTTPRoute", "namespace=shop",
		"group=" + broken.Group, "q=broken", "q=shop/broken", "sort=since", "sort=-code"} {
		if a, s := total(all, q), total(some, q); a != s || a == 0 {
			t.Errorf("%q: allow-all total %d, restricted %d", q, a, s)
		}
	}
	rows := get[Page[FindingRow]](t, some, findingsBase+"?q=broken", http.StatusOK).Items
	if len(rows) != 1 || !rows[0].Restricted || rows[0].Message != "" || rows[0].Detail != "" || rows[0].Since != nil ||
		rows[0].ID != broken.ID || rows[0].Group != broken.Group || rows[0].Resource == nil || rows[0].Resource.Link != LinkNone {
		t.Errorf("restricted row %+v", rows)
	}
	// The same ranks for every caller (rule 18).
	if a, s := ids(get[Page[FindingRow]](t, all, findingsBase, http.StatusOK).Items), ids(get[Page[FindingRow]](t, some, findingsBase, http.StatusOK).Items); !slices.Equal(a, s) {
		t.Errorf("rank order differs by caller: %v, %v", a, s)
	}

	// An evidence-restricted finding's hidden detail never matches either.
	ev, evAll, _ := startWith(t, setup{disputes: true}, evidenceWorld()...)
	_, evSome, _ := startWith(t, setup{disputes: true, authz: hideEvidence}, evidenceWorld()...)
	hidden := 0
	for _, f := range ev.Current().Model.Installs[0].Health.Findings {
		if f.Detail == "" || len(f.Evidence) == 0 || f.Resource == nil || !hideEvidence(nil, *f.Resource) || allowsEvery(f.Evidence) {
			continue
		}
		hidden++
		q := "q=" + url.QueryEscape(f.Detail)
		if total(evAll, q) == 0 || total(evSome, q) != 0 {
			t.Errorf("%s detail %q: allow-all %d, restricted %d", f.Code, f.Detail, total(evAll, q), total(evSome, q))
		}
		row := findingDetail(t, evSome, f.ID).Finding
		if !row.EvidenceRestricted || row.Detail != restrictedEvidence {
			t.Errorf("%s: row %+v, want evidence restricted", f.Code, row)
		}
	}
	if hidden == 0 {
		t.Fatal("no evidence-restricted finding in the world")
	}
}

// allowsEvery reports whether hideEvidence allows every ref.
func allowsEvery(refs []objects.Ref) bool {
	for _, r := range refs {
		if !hideEvidence(nil, r) {
			return false
		}
	}
	return true
}

func findingDetail(t *testing.T, srv *httptest.Server, id string) FindingDetail {
	t.Helper()
	return get[FindingDetail](t, srv, findingsBase+"/"+id, http.StatusOK)
}

// affectedSource serves src's model with extra affected objects in the group of finding id.
type affectedSource struct {
	ModelSource
	id    string
	cause *objects.Ref
	refs  []objects.Ref
}

func (s affectedSource) Current() *engine.Published {
	p := *s.ModelSource.Current()
	m := *p.Model
	m.Installs = slices.Clone(m.Installs)
	i := &m.Installs[0]
	d := *i.Diagnosis
	d.Groups = slices.Clone(d.Groups)
	for k := range d.Groups {
		g := &d.Groups[k]
		for _, j := range g.Members {
			if i.Health.Findings[j].ID == s.id {
				g.Affected = s.refs
				if s.cause != nil {
					g.Cause = s.cause
				}
			}
		}
	}
	i.Diagnosis = &d
	p.Model = &m
	return &p
}

func TestFindingDetailAffectedPaging(t *testing.T) {
	e, srv := findingsWorld(t, nil)
	var denied health.Finding
	for _, f := range e.Current().Model.Installs[0].Health.Findings {
		if f.Resource != nil && f.Resource.Name == "denied" {
			denied = f
		}
	}
	// 130 affected routes in two namespaces; every fifth one is hidden.
	var refs []objects.Ref
	for k := range 130 {
		refs = append(refs, route([]string{"a", "b"}[k%2], fmt.Sprintf("r-%03d", 129-k)))
	}
	hideFifth := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool {
		var n int
		_, err := fmt.Sscanf(ref.Name, "r-%d", &n)
		return err != nil || n%5 != 0
	})
	for _, tc := range []struct {
		name    string
		authz   Authorizer
		cause   *objects.Ref
		visible int
	}{{"allow-all", nil, nil, 130}, {"hidden fifth", hideFifth, nil, 104}, {"hidden cause", hideFifth, &[]objects.Ref{route("shop", "r-000")}[0], 0}} {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(t, affectedSource{ModelSource: e, id: denied.ID, cause: tc.cause, refs: refs}, tc.authz)
			path := findingsBase + "/" + denied.ID
			var seen []objects.Ref
			var pages []Page[LinkedRef]
			for p := path; ; {
				d := get[FindingDetail](t, s, p, http.StatusOK)
				if d.Finding.ID != denied.ID || d.Install != "kgw.kgateway" || d.Affected.Total != tc.visible || len(d.Affected.Items) > 50 {
					t.Fatalf("detail %+v", d)
				}
				pages = append(pages, d.Affected)
				for _, r := range d.Affected.Items {
					seen = append(seen, r.Ref)
				}
				if d.Affected.Next == "" {
					break
				}
				p = path + "?affectedCursor=" + url.QueryEscape(d.Affected.Next)
			}
			if len(seen) != tc.visible || !slices.IsSortedFunc(seen, compareAffected) {
				t.Fatalf("%d affected, want %d, sorted", len(seen), tc.visible)
			}
			for _, r := range seen {
				if tc.authz != nil && !tc.authz.Authorize(nil, r) {
					t.Errorf("hidden %v listed", r)
				}
			}
			if tc.visible == 0 {
				return
			}
			// Back from the last page.
			last := pages[len(pages)-1]
			if len(pages) > 1 {
				back := get[FindingDetail](t, s, path+"?affectedCursor="+url.QueryEscape(last.Prev), http.StatusOK).Affected
				if !slices.Equal(back.Items, pages[len(pages)-2].Items) {
					t.Errorf("prev page differs")
				}
			}
		})
	}
	for _, q := range []string{"?affectedCursor=nope", "?cursor=x", "?affectedCursor=a&affectedCursor=b"} {
		get[ErrorResponse](t, srv, findingsBase+"/"+denied.ID+q, http.StatusBadRequest)
	}
	since := get[Page[FindingRow]](t, srv, findingsBase+"?limit=1", http.StatusOK)
	get[ErrorResponse](t, srv, findingsBase+"/"+denied.ID+"?affectedCursor="+url.QueryEscape(since.Next), http.StatusBadRequest)
	get[ErrorResponse](t, srv, findingsBase+"/f1-aaaaaaaaaaaaaaaaaaaaaaaaaa", http.StatusNotFound)
	get[ErrorResponse](t, srv, "/api/v1/installs/nope/findings/"+denied.ID, http.StatusNotFound)
}

// /health findings carry the id and group that /findings lists, restricted ones too (rule 11).
func TestHealthFindingsGainIDAndGroup(t *testing.T) {
	for _, a := range []Authorizer{nil, hideBroken} {
		_, srv := findingsWorld(t, a)
		h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", http.StatusOK)
		rows := get[Page[FindingRow]](t, srv, findingsBase, http.StatusOK)
		byID := map[string]FindingRow{}
		for _, r := range rows.Items {
			byID[r.ID] = r
		}
		if len(h.Findings) != len(byID) || len(h.Findings) == 0 {
			t.Fatalf("health has %d findings, the list %d", len(h.Findings), len(byID))
		}
		for _, f := range h.Findings {
			r, ok := byID[f.ID]
			if !ok || f.Group == "" || r.Group != f.Group || r.Restricted != f.Restricted || r.Message != f.Message || r.Detail != f.Detail {
				t.Errorf("health finding %+v, list row %+v", f, r)
			}
		}
	}
}

// A cursor whose anchor finding is gone resumes at the anchor's old
// position, and a restricted anchor's cursor carries only its id and position.
func TestFindingsCursorVanishedAnchor(t *testing.T) {
	at := func(m int) *time.Time { s := testutil.Now.Add(time.Duration(m) * time.Minute); return &s }
	mk := func(names ...string) ([]health.Finding, *diagnose.Index) {
		var fs []health.Finding
		for k, n := range names {
			fs = append(fs, health.Finding{ID: "f1-" + n, Code: "KGWUI-00" + strconv.Itoa(k%3), Since: at(k)})
		}
		rank := make([]int, len(fs))
		for k := range rank {
			rank[k] = k
		}
		return fs, diagnose.NewIndex(fs, rank)
	}
	ids := func(fs []health.Finding, p diagnose.Page) []string {
		var out []string
		for _, k := range p.Items {
			out = append(out, fs[k].ID)
		}
		return out
	}
	fs, ix := mk("a", "b", "c", "d", "e")
	first, err := ix.Page(fs, diagnose.Sort{Key: diagnose.SortSince}, 2, "", nil)
	if err != nil || !slices.Equal(ids(fs, first), []string{"f1-a", "f1-b"}) {
		t.Fatalf("first page %v %v", ids(fs, first), err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(first.Next)
	if string(raw) != `{"s":"findings.since","d":"asc","b":false,"v":1,"t":["f1-b"]}` {
		t.Errorf("cursor %s", raw)
	}
	// b is gone: the next page starts at its old position, so c is not skipped.
	fs2, ix2 := mk("a", "c", "d", "e")
	next, err := ix2.Page(fs2, diagnose.Sort{Key: diagnose.SortSince}, 2, first.Next, nil)
	if err != nil || !slices.Equal(ids(fs2, next), []string{"f1-c", "f1-d"}) || next.Next == "" || next.Prev == "" {
		t.Errorf("after a vanished anchor: %v next %q prev %q %v", ids(fs2, next), next.Next, next.Prev, err)
	}
	if _, err := ix.Page(fs, diagnose.Sort{Key: diagnose.SortCode}, 2, first.Next, nil); !errors.Is(err, explore.ErrBadCursor) {
		t.Errorf("since cursor on the code sort: %v", err)
	}
}
