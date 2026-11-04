package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/computedtest"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/diagnose"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
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
	for _, sort := range []string{"", "rank", "rank.asc", "since", "since.asc", "since.desc", "code", "code.desc"} {
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
			if sort == "" || strings.HasPrefix(sort, "rank") {
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
		{"code=KGWUI-013&code=KGWUI-014&code=KGWUI-003", func(f health.Finding) bool {
			return f.Code == "KGWUI-013" || f.Code == "KGWUI-014" || f.Code == "KGWUI-003"
		}, 3},
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
		"sort=rank.desc", "sort=-since", "sort=-rank", "sort=name", "sort=since.down", "sort=.desc", "sort=since.desc.asc", "limit=0", "limit=201", "q=a&q=b", "group=" + denied.Group + "&group=" + denied.Group,
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
	get[ErrorResponse](t, srv, findingsBase+"?sort=since.desc&cursor="+url.QueryEscape(since.Next), http.StatusBadRequest)
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
	for _, q := range []string{"", "severity=error", "code=KGWUI-013", "kind=gateway.networking.k8s.io/HTTPRoute", "namespace=shop",
		"group=" + broken.Group, "q=broken", "q=shop/broken", "sort=since", "sort=code.desc"} {
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

// pagedFinding is a finding of a paging test: its own resource, so its group's rank keys are its own.
type pagedFinding struct {
	name, code string
	sev        health.Severity
	since      int // minutes after testutil.Now; negative is none
}

// pagedIndex returns fs and their index, grouped and ranked as the engine does.
func pagedIndex(ps []pagedFinding) ([]health.Finding, *diagnose.Index) {
	fs := make([]health.Finding, 0, len(ps))
	for _, p := range ps {
		r := route("shop", p.name)
		f := health.Finding{ID: "f1-" + p.name, Code: p.code, Severity: p.sev, Resource: &r}
		if p.since >= 0 {
			at := testutil.Now.Add(time.Duration(p.since) * time.Minute)
			f.Since = &at
		}
		fs = append(fs, f)
	}
	rep := &health.Report{Findings: fs}
	return rep.Findings, diagnose.Build(rep, diagnose.Input{}, diagnose.Families()).Index
}

// pageIDs returns the IDs of p's findings.
func pageIDs(fs []health.Finding, p diagnose.Page) []string {
	out := make([]string, 0, len(p.Items))
	for _, k := range p.Items {
		out = append(out, fs[k].ID)
	}
	return out
}

// Cursors keep keyset semantics (design §13.1): after findings before the
// anchor vanish, new ones arrive and since values change, every unchanged
// finding appears exactly once across the pages, forward and back.
func TestFindingsCursorSurvivesChanges(t *testing.T) {
	base := []pagedFinding{
		{"a", "KGWUI-004", health.SeverityWarning, 0}, {"b", "KGWUI-002", health.SeverityError, 1},
		{"c", "KGWUI-003", health.SeverityWarning, 2}, {"d", "KGWUI-002", health.SeverityWarning, 3},
		{"e", "KGWUI-001", health.SeverityWarning, -1}, {"f", "KGWUI-003", health.SeverityInfo, 5},
		{"g", "KGWUI-004", health.SeverityError, 6},
	}
	// Each change maps the findings shown before it to the new findings and the names it changed.
	changes := map[string]func(shown []string) ([]pagedFinding, []string){
		"shown and earlier ones vanish": func(shown []string) ([]pagedFinding, []string) {
			return without(base, shown), nil
		},
		"new ones arrive around the vanished anchor": func(shown []string) ([]pagedFinding, []string) {
			fresh := []pagedFinding{{"x", "KGWUI-000", health.SeverityError, 0}, {"y", "KGWUI-009", health.SeverityInfo, 9}, {"z", "KGWUI-002", health.SeverityWarning, 1}}
			return append(without(base, shown[len(shown)-1:]), fresh...), []string{"x", "y", "z"}
		},
		"since values change": func(shown []string) ([]pagedFinding, []string) {
			ps := slices.Clone(base)
			var moved []string
			for k := range ps {
				if slices.Contains(shown, "f1-"+ps[k].name) || ps[k].name == "g" {
					ps[k].since = 10 + k
					moved = append(moved, ps[k].name)
				}
			}
			return ps, moved
		},
	}
	for _, sort := range []diagnose.Sort{{Key: diagnose.SortRank}, {Key: diagnose.SortSince}, {Key: diagnose.SortSince, Desc: true},
		{Key: diagnose.SortCode}, {Key: diagnose.SortCode, Desc: true}} {
		for name, change := range changes {
			for _, back := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s.%v/%s/back=%v", sort.Key, sort.Desc, name, back), func(t *testing.T) {
					fs, ix := pagedIndex(base)
					// The page the client holds: the first one, or the last one when paging back.
					held, err := ix.Page(fs, "kgw.kgateway", sort, 2, "", nil)
					for back && err == nil && held.Next != "" {
						held, err = ix.Page(fs, "kgw.kgateway", sort, 2, held.Next, nil)
					}
					if err != nil {
						t.Fatal(err)
					}
					shown := pageIDs(fs, held)
					next, changed := change(shown)
					fs2, ix2 := pagedIndex(next)
					seen := map[string]int{}
					for _, id := range shown {
						seen[id]++
					}
					c := held.Next
					if back {
						c = held.Prev
					}
					for n := 0; c != ""; n++ {
						if n > len(fs2) {
							t.Fatal("no last page")
						}
						p, err := ix2.Page(fs2, "kgw.kgateway", sort, 2, c, nil)
						if err != nil {
							t.Fatal(err)
						}
						for _, id := range pageIDs(fs2, p) {
							seen[id]++
						}
						if c = p.Next; back {
							c = p.Prev
						}
					}
					for _, p := range next {
						if id := "f1-" + p.name; !slices.Contains(changed, p.name) && seen[id] != 1 {
							t.Errorf("unchanged %s seen %d times (held %v): %v", id, seen[id], shown, seen)
						}
					}
				})
			}
		}
	}
}

// without returns ps but the findings with the given IDs.
func without(ps []pagedFinding, ids []string) []pagedFinding {
	return slices.DeleteFunc(slices.Clone(ps), func(p pagedFinding) bool { return slices.Contains(ids, "f1-"+p.name) })
}

// A cursor is opaque: it names no finding, and a cursor altered, from
// another sort or from another list is a bad cursor (design §14 rules 17, 18).
func TestFindingsCursorSealed(t *testing.T) {
	fs, ix := pagedIndex([]pagedFinding{{"a", "KGWUI-002", health.SeverityError, 0}, {"b", "KGWUI-002", health.SeverityError, 1}, {"c", "KGWUI-003", health.SeverityWarning, 2}})
	first, err := ix.Page(fs, "kgw.kgateway", diagnose.Sort{Key: diagnose.SortSince}, 1, "", nil)
	if err != nil || first.Next == "" {
		t.Fatalf("first page %+v %v", first, err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(first.Next)
	for _, s := range []string{"f1-a", "KGWUI-002", "since", "findings"} {
		if strings.Contains(string(raw), s) {
			t.Errorf("cursor reveals %q", s)
		}
	}
	flipped := []byte(first.Next)
	flipped[len(flipped)/2] ^= 1
	explorer := base64.RawURLEncoding.EncodeToString([]byte(`{"s":"name","d":"asc","b":false,"t":["shop","a"]}`))
	for name, tc := range map[string]struct {
		s diagnose.Sort
		c string
	}{
		"altered": {diagnose.Sort{Key: diagnose.SortSince}, string(flipped)}, "another sort": {diagnose.Sort{Key: diagnose.SortCode}, first.Next},
		"another direction": {diagnose.Sort{Key: diagnose.SortSince, Desc: true}, first.Next}, "another list": {diagnose.Sort{Key: diagnose.SortSince}, explorer},
	} {
		if _, err := ix.Page(fs, "kgw.kgateway", tc.s, 1, tc.c, nil); !errors.Is(err, explore.ErrBadCursor) {
			t.Errorf("%s: %v, want a bad cursor", name, err)
		}
	}
}

// q compares lower-cased runes, so a match may change length when lowered.
func TestFindingsQueryFoldsRunes(t *testing.T) {
	for _, tc := range []struct {
		s, q string
		want bool
	}{{"\u212Aelvin route", "kelvin", true}, {"ÉCOLE", "école", true}, {"Straße", "strasse", false}, {"shop/cart", "cart", true}} {
		if got := containsLower(tc.s, strings.ToLower(tc.q)); got != tc.want {
			t.Errorf("containsLower(%q, %q) = %v, want %v", tc.s, tc.q, got, tc.want)
		}
	}
}

// Descending since and code keep ties in rank order, and a nil since sorts last both ways.
func TestFindingsDescendingTies(t *testing.T) {
	at := func(m int) *time.Time { s := testutil.Now.Add(time.Duration(m) * time.Minute); return &s }
	// Rank order a..f (severity, then since); c and e share since 1 and code 002; d and f have no since.
	rep := &health.Report{Findings: []health.Finding{
		{ID: "f1-a", Code: "KGWUI-001", Severity: health.SeverityError, Since: at(2)}, {ID: "f1-b", Code: "KGWUI-003", Severity: health.SeverityWarning, Since: at(0)},
		{ID: "f1-c", Code: "KGWUI-002", Severity: health.SeverityWarning, Since: at(1)}, {ID: "f1-d", Code: "KGWUI-003", Severity: health.SeverityWarning},
		{ID: "f1-e", Code: "KGWUI-002", Severity: health.SeverityInfo, Since: at(1)}, {ID: "f1-f", Code: "KGWUI-001", Severity: health.SeverityInfo},
	}}
	fs, ix := rep.Findings, diagnose.Build(rep, diagnose.Input{}, diagnose.Families()).Index
	for _, tc := range []struct {
		s    diagnose.Sort
		want string
	}{
		{diagnose.Sort{Key: diagnose.SortSince}, "b c e a d f"},
		{diagnose.Sort{Key: diagnose.SortSince, Desc: true}, "a c e b d f"},
		{diagnose.Sort{Key: diagnose.SortCode}, "a f c e b d"},
		{diagnose.Sort{Key: diagnose.SortCode, Desc: true}, "b d c e a f"},
	} {
		p, err := ix.Page(fs, "kgw.kgateway", tc.s, 10, "", nil)
		var got []string
		for _, k := range p.Items {
			got = append(got, strings.TrimPrefix(fs[k].ID, "f1-"))
		}
		if err != nil || strings.Join(got, " ") != tc.want {
			t.Errorf("%+v: %v %v, want %s", tc.s, got, err, tc.want)
		}
	}
}

// A cursor names the install it was minted for: replayed against another install it is a bad cursor.
func TestFindingsCursorBoundToInstall(t *testing.T) {
	// kgw2 watches team=b; each install has two failing routes on its own Gateway.
	d2 := testutil.ControllerDeployment("kgw2", "kgateway2", img, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"b"}}]`))
	failing := func(ns, name string, gw objects.ParentRef) runtime.Object {
		return testutil.HTTPRoute(ns, name, []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
			Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	}
	gw, gwB := testutil.GwRef("infra", "edge"), testutil.GwRef("infra-b", "edge")
	objs := isolated(testutil.Namespace("kgw2", nil), testutil.Namespace("infra-b", map[string]string{"team": "b"}), testutil.Namespace("shop-b", map[string]string{"team": "b"}),
		d2, testutil.ControllerPod(d2, "kgateway2-0", true), edgeIn("infra-b", 2), testutil.ProxyDeployment("infra-b", "edge", 1, 1),
		failing("shop", "broken", gw), failing("shop", "lost", gw), failing("shop-b", "broken", gwB), failing("shop-b", "lost", gwB))
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw.kgateway", "kgw2.kgateway2"), objs...)
	for _, sort := range []string{"rank", "since.asc", "code.desc"} {
		q := "/findings?limit=1&sort=" + sort
		a := get[Page[FindingRow]](t, srv, "/api/v1/installs/kgw.kgateway"+q, http.StatusOK)
		b := get[Page[FindingRow]](t, srv, "/api/v1/installs/kgw2.kgateway2"+q, http.StatusOK)
		if a.Next == "" || b.Next == "" {
			t.Fatalf("%s: want two findings in each install, got %d and %d", sort, a.Total, b.Total)
		}
		get[Page[FindingRow]](t, srv, "/api/v1/installs/kgw.kgateway"+q+"&cursor="+url.QueryEscape(a.Next), http.StatusOK)
		get[map[string]any](t, srv, "/api/v1/installs/kgw2.kgateway2"+q+"&cursor="+url.QueryEscape(a.Next), http.StatusBadRequest)
	}
}

// Cursors order times as the index does, also outside the int64 nanosecond range (1678–2262).
func TestFindingsCursorFarTimes(t *testing.T) {
	var fs []health.Finding
	for k, year := range []int{2301, 1600, 2026, 2300} {
		at := time.Date(year, 1, 1, 0, 0, 0, k, time.UTC)
		r := route("shop", fmt.Sprint(year))
		fs = append(fs, health.Finding{ID: fmt.Sprintf("f1-%d", year), Code: "KGWUI-002", Severity: health.SeverityWarning, Resource: &r, Since: &at})
	}
	rep := &health.Report{Findings: fs}
	fs, ix := rep.Findings, diagnose.Build(rep, diagnose.Input{}, diagnose.Families()).Index
	for _, s := range []diagnose.Sort{{Key: diagnose.SortRank}, {Key: diagnose.SortSince}, {Key: diagnose.SortSince, Desc: true}, {Key: diagnose.SortCode}} {
		all, err := ix.Page(fs, "kgw.kgateway", s, 10, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		want := pageIDs(fs, all)
		// Page one at a time forward, then back from the last page.
		p, err := ix.Page(fs, "kgw.kgateway", s, 1, "", nil)
		fwd := pageIDs(fs, p)
		for err == nil && p.Next != "" && len(fwd) <= len(fs) {
			if p, err = ix.Page(fs, "kgw.kgateway", s, 1, p.Next, nil); err == nil {
				fwd = append(fwd, pageIDs(fs, p)...)
			}
		}
		back := pageIDs(fs, p)
		for err == nil && p.Prev != "" && len(back) <= len(fs) {
			if p, err = ix.Page(fs, "kgw.kgateway", s, 1, p.Prev, nil); err == nil {
				back = append(pageIDs(fs, p), back...)
			}
		}
		if err != nil || !slices.Equal(fwd, want) || !slices.Equal(back, want) {
			t.Errorf("%+v: forward %v, back %v, want %v (%v)", s, fwd, back, want, err)
		}
	}
}

// fixCatalog serves core Service and Secret, and Service in the extra groups.
func fixCatalog(extra []string, unknown ...string) func() kinds.Catalog {
	gks := []schema.GroupKind{{Kind: "Service"}, {Kind: "Secret"}, {Group: objects.GatewayAPIGroup, Kind: "HTTPRoute"}}
	for _, g := range extra {
		gks = append(gks, schema.GroupKind{Group: g, Kind: "Service"})
	}
	c := kinds.NewCatalog(gks, unknown)
	return func() kinds.Catalog { return c }
}

// refsRoute is shop/r on infra/edge with ResolvedRefs=False/reason and refs as its backendRefs.
func refsRoute(reason string, refs ...any) *unstructured.Unstructured {
	gw := testutil.GwRef("infra", "edge")
	u := testutil.HTTPRoute("shop", "r", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, reason)}})
	return testutil.WithRules(u, map[string]any{"backendRefs": refs})
}

// tlsEdge is the Gateway infra/tls whose listener https is RefNotPermitted and names certs/tls-cert.
func tlsEdge() *unstructured.Unstructured {
	gw := testutil.Gateway("infra", "tls", "kgateway", nil, testutil.Listener{Name: "https", Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed"),
		testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "RefNotPermitted")}})
	l := gw.Object["spec"].(map[string]any)["listeners"].([]any)[0].(map[string]any)
	l["protocol"], l["tls"] = "HTTPS", map[string]any{"certificateRefs": []any{map[string]any{"name": "tls-cert", "namespace": "certs"}}}
	return gw
}

// fixDetail returns the detail of the one code finding on resource ns/name.
func fixDetail(t *testing.T, srv *httptest.Server, code, ns, name string) FindingDetail {
	t.Helper()
	p := get[Page[FindingRow]](t, srv, findingsBase+"?code="+code+"&namespace="+ns, http.StatusOK)
	var id string
	for _, r := range p.Items {
		if r.Resource != nil && r.Resource.Name == name {
			if id != "" {
				t.Fatalf("two %s on %s/%s", code, ns, name)
			}
			id = r.ID
		}
	}
	if id == "" {
		t.Fatalf("no %s on %s/%s in %+v", code, ns, name, p.Items)
	}
	return findingDetail(t, srv, id)
}

// roundTrip parses y and renders it again, failing t when y does not parse.
func roundTrip(t *testing.T, y string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := sigsyaml.Unmarshal([]byte(y), &m); err != nil {
		t.Fatalf("YAML does not parse: %v\n%s", err, y)
	}
	again, err := sigsyaml.Marshal(m)
	if err != nil || string(again) != y {
		t.Fatalf("YAML does not round-trip: %v\n%s\nvs\n%s", err, y, again)
	}
	return m
}

// Design §7.7 and R77: one served group with the kind gives the corrected-group
// fragment; two, or an unknown served state, give none. 014 on a spec-defined
// field gives the grant in the target namespace, group "" for a defaulted core ref.
func TestSuggestedFixes(t *testing.T) {
	typo := map[string]any{"group": "delegation.gateway.example", "kind": "Service", "name": "cart", "port": int64(80)}
	crossNS := map[string]any{"name": "cart", "namespace": "store", "port": int64(80)}
	typoCross := map[string]any{"group": "delegation.gateway.example", "kind": "Service", "name": "cart", "namespace": "store"}
	route := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "r"}
	grant := func(ns, name string) objects.Ref {
		return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "ReferenceGrant", Namespace: ns, Name: name}
	}
	const groupBasis = "API discovery: only group `core` serves kind `Service`. This sets only group, kind, name and namespace; keep the port and other fields."
	groupYAML := func(ns string) map[string]any {
		return map[string]any{"group": "", "kind": "Service", "name": "cart", "namespace": ns}
	}
	cases := []struct {
		name, code, ns, res string
		obj                 *unstructured.Unstructured
		catalog             func() kinds.Catalog
		want                *SuggestedFix
		yaml                map[string]any
	}{
		{"013 one served group", health.CodeRefUnresolved, "shop", "r", refsRoute("BackendNotFound", typo), fixCatalog(nil),
			&SuggestedFix{Title: "Set the group to `core`", Target: LinkedRef{Ref: route}, Path: "spec.rules[0].backendRefs[0]",
				Basis: groupBasis}, groupYAML("shop")},
		{"013 two served groups", health.CodeRefUnresolved, "shop", "r", refsRoute("BackendNotFound", typo), fixCatalog([]string{"other.example"}), nil, nil},
		// Review Focus 2: an unknown group may serve the kind too.
		{"013 another group unknown", health.CodeRefUnresolved, "shop", "r", refsRoute("BackendNotFound", typo), fixCatalog(nil, "metrics.example"), nil, nil},
		{"013 served state unknown", health.CodeRefUnresolved, "shop", "r", refsRoute("BackendNotFound", typo), fixCatalog(nil, "delegation.gateway.example"), nil, nil},
		{"014 backendRef grant", health.CodeRefNotPermitted, "shop", "r", refsRoute("RefNotPermitted", crossNS), fixCatalog(nil),
			&SuggestedFix{Title: "Create a ReferenceGrant in `store`", Target: LinkedRef{Ref: grant("store", "allow-shop-httproute-service-cart")}, Create: true,
				Basis: "Gateway API: a ref to another namespace needs a ReferenceGrant in that namespace."},
			map[string]any{"apiVersion": "gateway.networking.k8s.io/v1beta1", "kind": "ReferenceGrant",
				"metadata": map[string]any{"name": "allow-shop-httproute-service-cart", "namespace": "store"},
				"spec": map[string]any{"from": []any{map[string]any{"group": objects.GatewayAPIGroup, "kind": "HTTPRoute", "namespace": "shop"}},
					"to": []any{map[string]any{"group": "", "kind": "Service", "name": "cart"}}}}},
		{"014 certificateRef grant", health.CodeRefNotPermitted, "infra", "tls", tlsEdge(), fixCatalog(nil),
			&SuggestedFix{Title: "Create a ReferenceGrant in `certs`", Target: LinkedRef{Ref: grant("certs", "allow-infra-gateway-secret-tls-cert")}, Create: true,
				Basis: "Gateway API: a ref to another namespace needs a ReferenceGrant in that namespace."},
			map[string]any{"apiVersion": "gateway.networking.k8s.io/v1beta1", "kind": "ReferenceGrant",
				"metadata": map[string]any{"name": "allow-infra-gateway-secret-tls-cert", "namespace": "certs"},
				"spec": map[string]any{"from": []any{map[string]any{"group": objects.GatewayAPIGroup, "kind": "Gateway", "namespace": "infra"}},
					"to": []any{map[string]any{"group": "", "kind": "Secret", "name": "tls-cert"}}}}},
		// R77: a wrong group is never a missing grant.
		{"014 wrong group", health.CodeRefNotPermitted, "shop", "r", refsRoute("RefNotPermitted", typoCross), fixCatalog(nil),
			&SuggestedFix{Title: "Set the group to `core`", Target: LinkedRef{Ref: route}, Path: "spec.rules[0].backendRefs[0]",
				Basis: groupBasis}, groupYAML("store")},
		{"014 wrong group, two served", health.CodeRefNotPermitted, "shop", "r", refsRoute("RefNotPermitted", typoCross), fixCatalog([]string{"other.example"}), nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, setup{catalog: tc.catalog, rulesFor: computedtest.NoViews}, world(tc.obj)...)
			d := fixDetail(t, srv, tc.code, tc.ns, tc.res)
			if tc.want == nil {
				if d.SuggestedFix != nil {
					t.Fatalf("want no fix, got %+v", d.SuggestedFix)
				}
				return
			}
			got := d.SuggestedFix
			if got == nil {
				t.Fatalf("no fix; evidence %+v", d.Evidence)
			}
			y := got.YAML
			got.YAML, got.Target.Link = "", ""
			if !reflect.DeepEqual(*got, *tc.want) {
				t.Fatalf("fix %+v\nwant %+v", *got, *tc.want)
			}
			if m := roundTrip(t, y); !reflect.DeepEqual(m, tc.yaml) {
				t.Fatalf("YAML %v, want %v", m, tc.yaml)
			}
		})
	}
}

// Design §7.7: two cross-namespace refs on the failing parent leave no single
// ref, so the detail lists both candidates and has no YAML.
func TestNoFixWithoutAttribution(t *testing.T) {
	a := map[string]any{"name": "cart", "namespace": "store"}
	b := map[string]any{"name": "pay", "namespace": "bank"}
	_, srv, _ := startWith(t, setup{catalog: fixCatalog(nil), rulesFor: computedtest.NoViews}, world(refsRoute("RefNotPermitted", a, b))...)
	d := fixDetail(t, srv, health.CodeRefNotPermitted, "shop", "r")
	ev, ok := d.Evidence.(*health.RefsEvidence)
	if !ok || ev.Attributed || len(ev.Candidates) != 2 || ev.Candidates[0].Path != "spec.rules[0].backendRefs[0]" || ev.Candidates[1].Path != "spec.rules[0].backendRefs[1]" {
		t.Fatalf("evidence %#v: want both candidates, not attributed", d.Evidence)
	}
	if d.SuggestedFix != nil {
		t.Fatalf("want no fix, got %+v", d.SuggestedFix)
	}
}

// Design §14 rule 15: a fix that names an object the caller may not view is
// dropped; the finding keeps its evidence.
func TestNoFixNamingHiddenObject(t *testing.T) {
	hideCart := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Namespace != "store" })
	_, srv, _ := startWith(t, setup{authz: hideCart, catalog: fixCatalog(nil), rulesFor: computedtest.NoViews},
		world(refsRoute("RefNotPermitted", map[string]any{"name": "cart", "namespace": "store"}))...)
	d := fixDetail(t, srv, health.CodeRefNotPermitted, "shop", "r")
	if d.Finding.Restricted || d.SuggestedFix != nil {
		t.Fatalf("restricted %v fix %+v: want a visible finding without a fix", d.Finding.Restricted, d.SuggestedFix)
	}
}

// Design §7.4, §14 row 015: a 015 naming a served policy links it as evidence and
// joins its "Replaced by policy" group; a caller who cannot view the policy gets
// evidenceRestricted and a restricted group row.
func TestRuleReplacedThroughTheAPI(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	cond := testutil.Cond("Accepted", metav1.ConditionFalse, "RouteRuleReplaced")
	cond.Message = "Replaced Rule (2): gateway.kgateway.dev/TrafficPolicy/shop/auth: ext auth unavailable"
	route := testutil.HTTPRoute("shop", "r", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{cond, testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}})
	tp := objects.Ref{Group: kinds.KgatewayGroup, Kind: "TrafficPolicy", Namespace: "shop", Name: "auth"}
	catalog := func() kinds.Catalog {
		return kinds.NewCatalog([]schema.GroupKind{{Kind: "Service"}, {Group: tp.Group, Kind: tp.Kind}}, nil)
	}
	hideTP := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != tp })
	for _, tc := range []struct {
		name   string
		authz  Authorizer
		hidden bool
	}{{"policy visible", nil, false}, {"policy hidden", hideTP, true}} {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, setup{authz: tc.authz, catalog: catalog, rulesFor: computedtest.NoViews}, world(route)...)
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw.kgateway/health", http.StatusOK)
			var got []FindingDTO
			for _, f := range findingsOn(h, "shop/r") {
				if f.Code == health.CodeConditionFalse {
					t.Fatalf("002 next to 015: %+v", f)
				}
				if f.Code == health.CodeRuleReplaced {
					got = append(got, f)
				}
			}
			if len(got) != 1 || got[0].Restricted || got[0].EvidenceRestricted != tc.hidden || (got[0].Facts == nil) != tc.hidden {
				t.Fatalf("015 findings %+v", got)
			}
			if !tc.hidden && got[0].Facts.EvidenceKind() != health.EvidenceRuleReplaced {
				t.Fatalf("evidence kind %q", got[0].Facts.EvidenceKind())
			}
			d := get[Diagnostics](t, srv, "/api/v1/installs/kgw.kgateway/diagnostics", http.StatusOK)
			i := slices.IndexFunc(d.Groups, func(g GroupRow) bool { return g.ID == got[0].Group })
			if i < 0 || d.Groups[i].Code != health.CodeRuleReplaced || d.Groups[i].Restricted != tc.hidden {
				t.Fatalf("group %q in %+v", got[0].Group, d.Groups)
			}
			if g := d.Groups[i]; !tc.hidden && (g.Cause == nil || g.Cause.Ref != tp || len(g.Affected) != 1 || g.Affected[0].String() != "shop/r") {
				t.Fatalf("policy group %+v", g)
			}
		})
	}
}

// Design §13.3: a 015's policy is a LinkedRef on the wire. The e2e fixture's
// no-such-policy does not exist, so it links to nothing; an owned policy that
// exists opens.
func TestRuleReplacedPolicyLink(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	route := func(policy string) *unstructured.Unstructured {
		cond := testutil.Cond("Accepted", metav1.ConditionFalse, "RouteRuleReplaced")
		cond.Message = "Replaced Rule (0): gateway.kgateway.dev/TrafficPolicy/shop/" + policy + ": policy not found"
		return testutil.HTTPRoute("shop", "r", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
			Conds: []metav1.Condition{cond, testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}})
	}
	catalog := func() kinds.Catalog {
		return kinds.NewCatalog([]schema.GroupKind{{Kind: "Service"}, {Group: kinds.KgatewayGroup, Kind: "TrafficPolicy"}}, nil)
	}
	accepted := testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Valid")}}
	for _, tc := range []struct {
		name, policy string
		objs         []runtime.Object
		absent       bool
	}{
		{"no-such-policy", "no-such-policy", world(route("no-such-policy")), true},
		{"existing policy", "auth", world(route("auth"), testutil.TrafficPolicy("shop", "auth", accepted)), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, setup{catalog: catalog, rulesFor: computedtest.NoViews}, tc.objs...)
			d := fixDetail(t, srv, health.CodeRuleReplaced, "shop", "r")
			ev, ok := d.Evidence.(*RuleEvidence)
			if !ok || ev.Policy == nil || ev.Policy.Name != tc.policy || ev.MatchIndex == nil || *ev.MatchIndex != 0 || (ev.Policy.Link == LinkNone) != tc.absent {
				t.Fatalf("evidence %#v", d.Evidence)
			}
			// /health sends the same wire shape: the policy with its link.
			h := get[map[string]any](t, srv, "/api/v1/installs/kgw.kgateway/health", http.StatusOK)
			var links []any
			for _, f := range h["findings"].([]any) {
				if m := f.(map[string]any); m["code"] == health.CodeRuleReplaced {
					links = append(links, m["evidence"].(map[string]any)["policy"].(map[string]any)["link"])
				}
			}
			if len(links) != 1 || links[0] != string(ev.Policy.Link) {
				t.Fatalf("health policy links %v, detail %s", links, ev.Policy.Link)
			}
		})
	}
}
