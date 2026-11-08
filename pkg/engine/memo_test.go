package engine

import (
	"fmt"
	"hash/fnv"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/diagnose"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// memoSource serves base as the cache would: stamped at now, with a Source
// fingerprint of exactly its objects and access states.
type memoSource struct {
	base *cache.Snapshot
	now  *time.Time
}

func (m *memoSource) Snapshot() *cache.Snapshot {
	s := *m.base
	s.TakenAt = *m.now
	s.Source = sourceOf(&s)
	return &s
}

// edit replaces base's object of u's ref in kind g with u.
func (m *memoSource) edit(t *testing.T, g schema.GroupVersionResource, u *unstructured.Unstructured) {
	t.Helper()
	s := *m.base
	s.Objects = maps.Clone(s.Objects)
	us := slices.Clone(s.Objects[g])
	i := slices.IndexFunc(us, func(x *unstructured.Unstructured) bool { return objects.RefOf(x) == objects.RefOf(u) })
	if i < 0 {
		t.Fatalf("no %s", objects.RefOf(u))
	}
	us[i] = u
	s.Objects[g] = us
	m.base = &s
}

// setAccess sets base's access to g.
func (m *memoSource) setAccess(g schema.GroupVersionResource, a cache.Access) {
	s := *m.base
	s.Access = maps.Clone(s.Access)
	s.Access[g] = a
	m.base = &s
}

// sourceOf fingerprints s by (uid, resourceVersion) per kind, as the cache does.
func sourceOf(s *cache.Snapshot) cache.Fingerprint {
	f := cache.Fingerprint{Kinds: map[string]cache.KindPrint{}, Access: map[string]cache.Access{}}
	addKey := func(g schema.GroupVersionResource, key string) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(key))
		k := f.Kinds[g.String()]
		k.Count++
		k.Hash += h.Sum64()
		f.Kinds[g.String()] = k
	}
	add := func(g schema.GroupVersionResource, o metav1.Object) {
		addKey(g, fmt.Sprintf("%s\x00%s", o.GetUID(), o.GetResourceVersion()))
	}
	for g, a := range s.Access {
		f.Access[g.String()], f.Kinds[g.String()] = a, cache.KindPrint{}
	}
	for g, us := range s.Objects {
		for _, u := range us {
			add(g, u)
		}
	}
	for _, n := range s.Namespaces {
		add(cache.NamespacesGVR, n)
	}
	for _, p := range s.Pods {
		add(cache.PodsGVR, p)
	}
	for _, d := range s.Deployments {
		add(cache.DeploymentsGVR, d)
	}
	// Service and slice facts carry no resourceVersion; their content stands in for it.
	for _, v := range s.Services {
		addKey(cache.ServicesGVR, fmt.Sprintf("%+v", v))
	}
	for _, v := range s.EndpointSlices {
		addKey(cache.EndpointSlicesGVR, fmt.Sprintf("%+v", v))
	}
	return f
}

// counted wraps a check and counts its runs.
type counted struct {
	health.Check
	runs *int
}

func (c counted) Run(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
	*c.runs++
	return c.Check.Run(ctx)
}

// probe is a check with the given inputs whose Run is run.
type probe struct {
	name string
	in   health.InputSet
	run  func(ctx health.RunContext) ([]health.Finding, health.CheckResult)
}

func (p probe) Name() string                                                     { return p.name }
func (p probe) Inputs() health.InputSet                                          { return p.in }
func (p probe) Run(ctx health.RunContext) ([]health.Finding, health.CheckResult) { return p.run(ctx) }

// okRun returns one info finding with detail and an OK result stamped at Now.
func okRun(name, detail string) func(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
	return func(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
		return []health.Finding{{Code: "ACME-001", Severity: health.SeverityInfo, Install: ctx.Install.ID(), Message: name, Detail: detail}},
			health.CheckResult{Name: name, OK: true, CollectedAt: ctx.Now}
	}
}

// memoEngine is an upstream engine over snapshot(t) on a settable clock,
// without metrics staleness, with checks in place of the upstream ones when given.
func memoEngine(t *testing.T, checks ...health.Check) (*Engine, *memoSource, *time.Time) {
	t.Helper()
	now := testutil.Now
	src := &memoSource{base: snapshot(t), now: &now}
	cfg := config(nil, metricsAt(testutil.Now), func() time.Time { return now })
	cfg.Snapshots, cfg.Collected = src, served(metricsAt(testutil.Now), 0)
	if len(checks) > 0 {
		cfg.Checks = checks
	}
	return New(cfg), src, &now
}

// report is install kgw.kgateway's health in m.
func report(t *testing.T, m *Model) health.Report {
	t.Helper()
	for _, im := range m.Installs {
		if im.Resolved.ID() == "kgw.kgateway" {
			return im.Health
		}
	}
	t.Fatal("no kgw.kgateway")
	return health.Report{}
}

// detailOf is the detail of rep's finding from check name, or "".
func detailOf(rep health.Report, name string) string {
	for _, f := range rep.Findings {
		if f.Message == name {
			return f.Detail
		}
	}
	return ""
}

// annotated returns a copy of u with an annotation and a new resourceVersion.
func annotated(u *unstructured.Unstructured, rv string) *unstructured.Unstructured {
	c := u.DeepCopy()
	c.SetAnnotations(map[string]string{"note": rv})
	c.SetResourceVersion(rv)
	return c
}

// §16.2: an annotation-only edit to a kind a check does not declare skips it;
// an edit to a kind it declares runs it.
func TestUnchangedInputsSkipCheck(t *testing.T) {
	var runs int
	c := counted{probe{"pods probe", health.InputSet{Kinds: []objects.GroupKind{{Kind: "Pod"}}}, okRun("pods probe", "")}, &runs}
	e, src, _ := memoEngine(t, append(health.Upstream(), c)...)
	first := report(t, e.Compute())
	gws := gvr(t, "Gateway")
	src.edit(t, gws, annotated(src.base.Objects[gws][0], "2"))
	second := report(t, e.Compute())
	if runs != 1 {
		t.Fatalf("an annotation edit to an undeclared kind ran the check: %d runs, want 1", runs)
	}
	if !reflect.DeepEqual(first.Findings, second.Findings) || !reflect.DeepEqual(first.Checks, second.Checks) {
		t.Fatalf("the skipped check's output changed:\n%+v\n%+v", first, second)
	}
	pod := src.base.Pods[0].DeepCopy()
	pod.ResourceVersion = "2"
	s := *src.base
	s.Pods = []*corev1.Pod{pod}
	src.base = &s
	e.Compute()
	if runs != 2 {
		t.Fatalf("an edit to a declared kind must run the check: %d runs, want 2", runs)
	}
}

// §16.2: a check's wake time runs it once due; before, it is skipped and the
// wake time stays the model's deadline. A check that declares Clock always runs.
func TestDueDeadlineRunsCheck(t *testing.T) {
	var runs, clockRuns int
	wake := testutil.Now.Add(30 * time.Second)
	waker := counted{probe{"waker", health.InputSet{}, func(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
		ctx.WakeAt(wake)
		return okRun("waker", fmt.Sprint(!ctx.Now.Before(wake)))(ctx)
	}}, &runs}
	clocked := counted{probe{"clocked", health.InputSet{Clock: true}, okRun("clocked", "")}, &clockRuns}
	e, _, now := memoEngine(t, waker, clocked)
	e.Compute()
	*now = wake.Add(-time.Nanosecond)
	if m := e.Compute(); runs != 1 || !m.Next.At.Equal(wake) {
		t.Fatalf("before the wake time: %d runs, next %v; want 1 run, next %v", runs, m.Next.At, wake)
	}
	*now = wake.Add(time.Nanosecond)
	if d := detailOf(report(t, e.Compute()), "waker"); runs != 2 || d != "true" {
		t.Fatalf("past the wake time: %d runs, detail %q; want a second run", runs, d)
	}
	if clockRuns != 3 {
		t.Fatalf("a Clock check ran %d times in 3 computes", clockRuns)
	}
}

// §16.2: a check that writes its Store is still skipped while its inputs and
// wake time are unchanged; its Store keeps the value of its last run.
func TestStoreWritingCheckStillSkipped(t *testing.T) {
	var runs int
	writer := counted{probe{"writer", health.InputSet{}, func(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
		n, _ := health.Load[int](ctx.State, "n")
		ctx.State.Set("n", n+1)
		return okRun("writer", fmt.Sprint(n+1))(ctx)
	}}, &runs}
	e, _, now := memoEngine(t, writer)
	e.Compute()
	*now = now.Add(time.Minute)
	rep := report(t, e.Compute())
	if runs != 1 || rep.Findings[0].Detail != "1" {
		t.Fatalf("unchanged inputs: %d runs, detail %q; want 1 run, detail 1", runs, rep.Findings[0].Detail)
	}
	if !rep.Checks[0].CollectedAt.Equal(*now) {
		t.Fatalf("a reused result is stamped %v, want this compute's %v", rep.Checks[0].CollectedAt, *now)
	}
}

// xDS rejects declares no Clock, so with unchanged inputs it runs once; a narrow
// metrics check waking at the staleness deadline turns unavailable exactly then.
func TestMemoNeverOutlivesStaleness(t *testing.T) {
	var xdsRuns, narrowRuns int
	var xds health.Check
	for _, c := range health.Upstream() {
		if c.Name() == "xDS rejects" {
			xds = counted{c, &xdsRuns}
		}
	}
	const maxAge = 30 * time.Second
	stale := testutil.Now.Add(maxAge) // metricsAt collects at testutil.Now
	narrow := counted{probe{"narrow metrics", health.InputSet{Collected: []string{ctrlmetrics.Key}}, func(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
		v, _ := ctx.Collected(ctrlmetrics.Key)
		r, _, has := ctrlmetrics.Lookup(v, ctx.Install.ID())
		at := r.CollectedAt.Add(maxAge)
		if !has || !ctx.Now.Before(at) {
			return okRun("narrow metrics", "unavailable")(ctx)
		}
		ctx.WakeAt(at)
		return okRun("narrow metrics", "fresh")(ctx)
	}}, &narrowRuns}
	e, _, now := memoEngine(t, xds, narrow)
	*now = testutil.Now.Add(time.Second) // apart from the collection time, so a stamp is not ambiguous
	detail := func(rep health.Report) string { return detailOf(rep, "narrow metrics") }
	e.Compute()
	*now = stale.Add(-time.Nanosecond)
	if d := detail(report(t, e.Compute())); d != "fresh" || narrowRuns != 1 {
		t.Fatalf("before staleness: %q after %d runs; want fresh from the memo", d, narrowRuns)
	}
	*now = stale
	if d := detail(report(t, e.Compute())); d != "unavailable" || narrowRuns != 2 {
		t.Fatalf("at staleness: %q after %d runs; want unavailable from a second run", d, narrowRuns)
	}
	if xdsRuns != 1 {
		t.Fatalf("xDS rejects ran %d times in 3 computes; want 1", xdsRuns)
	}
}

// A skipped check inside the grace period keeps the grace end as the model's
// deadline, and runs once it passes.
func TestSkippedCheckKeepsGraceDeadline(t *testing.T) {
	var runs int
	routes := objects.GroupKind{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute"}
	graced := counted{probe{"graced", health.InputSet{Kinds: []objects.GroupKind{routes}}, func(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
		for _, u := range ctx.Snapshot.Objects[gvrOf(ctx, routes)] {
			if ctx.Now.Sub(ctx.FirstSeen(u)) < health.Grace {
				return okRun("graced", "pending")(ctx)
			}
		}
		return okRun("graced", "settled")(ctx)
	}}, &runs}
	e, src, now := memoEngine(t, graced)
	s := *src.base
	s.Objects = maps.Clone(s.Objects)
	hr := gvr(t, "HTTPRoute")
	s.Objects[hr] = []*unstructured.Unstructured{testutil.HTTPRoute("shop", "cart", []objects.ParentRef{testutil.GwRef("infra", "edge")})}
	src.base = &s
	e.Compute()
	end := testutil.Now.Add(health.Grace)
	*now = now.Add(10 * time.Second)
	if m := e.Compute(); runs != 1 || !report(t, m).Next.Equal(end) || m.Next.At.After(end) {
		t.Fatalf("inside grace: %d runs, report next %v, model next %v; want 1 run and the grace end %v", runs, report(t, m).Next, m.Next.At, end)
	}
	*now = end.Add(time.Second)
	if rep := report(t, e.Compute()); runs != 2 || rep.Findings[0].Detail != "settled" {
		t.Fatalf("past the grace end: %d runs, %+v; want a second run, settled", runs, rep.Findings)
	}
}

// gvrOf is the served GVR of gk.
func gvrOf(ctx health.RunContext, gk objects.GroupKind) schema.GroupVersionResource {
	for _, s := range ctx.Served {
		if s.Group == gk.Group && s.Kind == gk.Kind {
			return s.GVR()
		}
	}
	return schema.GroupVersionResource{}
}

// Review finding 2: with objects unchanged, a failed watch reruns cluster
// access, which reports it, and the recovery reruns it again.
func TestAccessFailureAndRecoveryRerunChecks(t *testing.T) {
	var runs int
	checks := health.Upstream()
	checks[0] = counted{checks[0], &runs}
	if checks[0].Name() != "cluster access" {
		t.Fatalf("first upstream check is %q", checks[0].Name())
	}
	e, src, _ := memoEngine(t, checks...)
	noAccess := func(rep health.Report) bool {
		return hasFinding(rep, health.CodeSignalUnavailable, "", "watch failed")
	}
	e.Compute()
	if rep := report(t, e.Compute()); runs != 1 || noAccess(rep) {
		t.Fatalf("unchanged: %d runs; want cluster access skipped", runs)
	}
	hr := gvr(t, "HTTPRoute")
	src.setAccess(hr, cache.Access{Err: "watch failed"})
	if rep := report(t, e.Compute()); runs != 2 || !noAccess(rep) {
		t.Fatalf("failed watch: %d runs, findings %+v; want a rerun reporting it", runs, rep.Findings)
	}
	src.setAccess(hr, cache.Access{OK: true})
	if rep := report(t, e.Compute()); runs != 3 || noAccess(rep) {
		t.Fatalf("recovered watch: %d runs, findings %+v; want a rerun without it", runs, rep.Findings)
	}
}

// A check declaring a kind the engine does not watch has no print, so it runs every time.
func TestUnwatchedKindAlwaysRuns(t *testing.T) {
	var runs int
	widgets := objects.GroupKind{Group: "acme.example.com", Kind: "Widget"}
	c := counted{probe{"widgets", health.InputSet{Kinds: []objects.GroupKind{widgets}}, okRun("widgets", "")}, &runs}
	e, _, _ := memoEngine(t, c)
	e.Compute()
	e.Compute()
	if runs != 2 {
		t.Fatalf("a check on an unwatched kind ran %d times in 2 computes", runs)
	}
}

// rejectsAt are metrics collected at t with edge rejecting Clusters on Pod p when on.
type rejectsAt struct {
	at time.Time
	on *bool
}

func (m rejectsAt) Get(string) (ctrlmetrics.Result, bool) {
	r := metricsAt(m.at)
	res, _ := r.Get("")
	if *m.on {
		k := ctrlmetrics.TypeKey{Gateway: ctrlmetrics.GatewayKey{Namespace: "infra", Name: "edge"}, TypeURL: "envoy.config.cluster.v3.Cluster"}
		res.Metrics.Rejects[k.Gateway] = 1
		res.Metrics.PerPod = []ctrlmetrics.PodMetrics{{UID: "pod/kgw/p", Name: "p", Active: map[ctrlmetrics.TypeKey]float64{k: 1}, Total: map[ctrlmetrics.TypeKey]float64{}}}
	}
	return res, true
}

// A spec edit before the first reject lists the route as recently changed; the
// memoized xDS check skips unchanged inputs and reruns on a later edit, which is not listed.
func TestRecentlyChangedThroughTheMemo(t *testing.T) {
	var runs int
	var xds health.Check
	for _, c := range health.Upstream() {
		if c.Name() == "xDS rejects" {
			xds = counted{c, &runs}
		}
	}
	edge := testutil.GwRef("infra", "edge")
	r, s := testutil.HTTPRoute("shop", "r", []objects.ParentRef{edge}), testutil.HTTPRoute("shop", "s", []objects.ParentRef{edge})
	now := testutil.Now
	on := false
	src := &memoSource{base: snapshot(t, r, s), now: &now}
	cfg := config(nil, rejectsAt{testutil.Now, &on}, func() time.Time { return now })
	cfg.Snapshots, cfg.Collected, cfg.Checks = src, served(rejectsAt{testutil.Now, &on}, 0), []health.Check{xds}
	e := New(cfg)
	bump := func(u *unstructured.Unstructured, rv string) {
		c := u.DeepCopy()
		c.SetGeneration(2)
		c.SetResourceVersion(rv)
		src.edit(t, gvr(t, "HTTPRoute"), c)
	}
	recent := func() []objects.Ref {
		for _, f := range report(t, e.Compute()).Findings {
			if f.Code == health.CodeXDSRejected {
				return f.Facts.(*health.XDSRejectsEvidence).RecentlyChanged
			}
		}
		t.Fatal("no 001")
		return nil
	}
	e.Compute() // the check's first run; r and s are seen now
	now = now.Add(time.Minute)
	bump(r, "r2")
	e.Compute()
	now, on = now.Add(time.Minute), true
	want := []objects.Ref{objects.RefOf(r)}
	if got := recent(); !reflect.DeepEqual(got, want) || runs != 3 {
		t.Fatalf("after the reject: %v in %d runs, want %v in 3", got, runs, want)
	}
	now = now.Add(time.Minute)
	if got := recent(); !reflect.DeepEqual(got, want) || runs != 3 {
		t.Fatalf("unchanged inputs: %v in %d runs, want %v from the memo", got, runs, want)
	}
	bump(s, "s2")
	if got := recent(); !reflect.DeepEqual(got, want) || runs != 4 {
		t.Fatalf("edit after the reject: %v in %d runs, want %v in 4", got, runs, want)
	}
}

// §16.2: when every check returns its memo's findings (a hit, or a rerun with equal
// findings), the report reuses the last findings and, while Owned is unchanged, the
// last diagnosis; each step equals an uncached compute.
func TestUnchangedChecksReuseReportAndDiagnosis(t *testing.T) {
	pods := health.InputSet{Kinds: []objects.GroupKind{{Kind: "Pod"}}}
	podLabel := func(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
		return okRun("labelled probe", ctx.Snapshot.Pods[0].Labels["probe"])(ctx)
	}
	checks := []health.Check{probe{"pods probe", pods, okRun("pods probe", "")}, probe{"labelled probe", pods, podLabel}}
	e, src, _ := memoEngine(t, checks...)
	plainCfg := e.cfg
	plainCfg.NoMemo = true
	plain := New(plainCfg)
	type step struct {
		rep  health.Report
		diag *diagnose.Diagnosis
	}
	compute := func() step {
		m, p := e.Compute(), plain.Compute()
		got, want := installOf(t, m), installOf(t, p)
		if !reflect.DeepEqual(got.Health.Findings, want.Health.Findings) || !reflect.DeepEqual(got.Diagnosis.Groups, want.Diagnosis.Groups) ||
			!reflect.DeepEqual(got.Diagnosis.Rank, want.Diagnosis.Rank) {
			t.Fatalf("memoized and uncached differ:\n%+v\n%+v", got.Diagnosis.Groups, want.Diagnosis.Groups)
		}
		return step{got.Health, got.Diagnosis}
	}
	same := func(a, b step) bool { return first(a.rep.Findings) == first(b.rep.Findings) }
	first := compute()
	second := compute()
	if !same(first, second) || first.diag != second.diag {
		t.Fatal("unchanged checks and Owned: the findings and diagnosis are not reused")
	}
	gws := gvr(t, "Gateway")
	src.edit(t, gws, annotated(src.base.Objects[gws][0], "2"))
	third := compute()
	if !same(second, third) || second.diag == third.diag {
		t.Fatal("an Owned edit no check declares: want the findings reused and the diagnosis rebuilt")
	}
	setPod := func(rv, label string) {
		pod := src.base.Pods[0].DeepCopy()
		pod.ResourceVersion = rv
		pod.Labels = map[string]string{"probe": label}
		s := *src.base
		s.Pods = []*corev1.Pod{pod}
		src.base = &s
	}
	setPod("2", "")
	fourth := compute()
	if !same(third, fourth) {
		t.Fatal("a declared edit reran the checks with equal findings: want the findings reused")
	}
	setPod("3", "changed")
	if fifth := compute(); same(fourth, fifth) || fourth.diag == fifth.diag {
		t.Fatal("a rerun with a changed finding: want new findings and a new diagnosis")
	}
}

// A check that wakes on the scrape time reruns after every new scrape of unchanged
// content (R70). Its equal findings keep the report's findings and the diagnosis,
// and each step equals an uncached compute.
func TestRerunWithEqualFindingsReusesDiagnosis(t *testing.T) {
	scraper := probe{"scrape probe", health.InputSet{Collected: []string{ctrlmetrics.Key}}, func(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
		v, _ := ctx.Collected(ctrlmetrics.Key)
		r, _, _ := ctrlmetrics.Lookup(v, ctx.Install.ID())
		ctx.WakeAt(r.CollectedAt.Add(time.Minute))
		return okRun("scrape probe", "")(ctx)
	}}
	var runs int
	engine := func(noMemo bool) (*Engine, *time.Time, *time.Time) {
		n, c := testutil.Now, testutil.Now
		cfg := config(nil, scrapedAt{&c}, func() time.Time { return n })
		cfg.Snapshots, cfg.Checks, cfg.NoMemo = &memoSource{base: snapshot(t), now: &n}, []health.Check{counted{scraper, &runs}}, noMemo
		return New(cfg), &n, &c
	}
	e, now, col := engine(false)
	plain, pnow, pcol := engine(true)
	var last InstallModel
	for i := range 4 {
		*now = testutil.Now.Add(time.Duration(i)*10*time.Second + time.Second)
		*col = now.Add(-time.Second)
		*pnow, *pcol = *now, *col
		before := runs
		got, want := installOf(t, e.Compute()), installOf(t, plain.Compute())
		if !reflect.DeepEqual(got.Health, want.Health) || !reflect.DeepEqual(got.Diagnosis.Groups, want.Diagnosis.Groups) ||
			!reflect.DeepEqual(got.Diagnosis.Rank, want.Diagnosis.Rank) {
			t.Fatalf("scrape %d: memoized and uncached differ:\n%+v\n%+v", i, got.Health, want.Health)
		}
		if i > 0 {
			if runs-before != 2 { // the memoized engine's rerun and the uncached one's run
				t.Fatalf("scrape %d: %d runs, want the check rerun on each engine", i, runs-before)
			}
			if first(got.Health.Findings) != first(last.Health.Findings) || got.Diagnosis != last.Diagnosis {
				t.Fatalf("scrape %d: equal findings from a rerun must reuse the findings and diagnosis", i)
			}
		}
		last = got
	}
}

// installOf is install kgw.kgateway's model in m.
func installOf(t *testing.T, m *Model) InstallModel {
	t.Helper()
	for _, im := range m.Installs {
		if im.Resolved.ID() == "kgw.kgateway" {
			return im
		}
	}
	t.Fatal("no kgw.kgateway")
	return InstallModel{}
}

// scrapedAt serves reject-free metrics collected at *at, as a collector scraping unchanged content.
type scrapedAt struct{ at *time.Time }

func (m scrapedAt) Get(id string) (ctrlmetrics.Result, bool) { return metricsAt(*m.at).Get(id) }

// scrapeEngine is the upstream engine over snapshot(t) with metrics collected at *col and a 45 s max age.
func scrapeEngine(t *testing.T) (e *Engine, now, col *time.Time) {
	t.Helper()
	n, c := testutil.Now, testutil.Now
	cfg := config(nil, scrapedAt{&c}, func() time.Time { return n })
	cfg.Snapshots, cfg.Collected = &memoSource{base: snapshot(t), now: &n}, served(scrapedAt{&c}, 45*time.Second)
	return New(cfg), &n, &c
}

// xdsResult is the xDS rejects check's result in m.
func xdsResult(t *testing.T, m *Model) health.CheckResult {
	t.Helper()
	for _, c := range report(t, m).Checks {
		if c.Name == "xDS rejects" {
			return c
		}
	}
	t.Fatal("no xDS rejects result")
	return health.CheckResult{}
}

// Fresh scrapes of unchanged content leave the staleness deadline at the latest
// scrape: a memoized check adds no earlier deadline, so an idle install recomputes
// only on the safety timer.
func TestIdleFreshScrapesAddNoDeadline(t *testing.T) {
	e, now, col := scrapeEngine(t)
	const maxAge = 45 * time.Second
	for i := 0; i <= 20; i++ {
		// A scrape every 10 s and a safety recompute every 30 s, 1 s after a scrape.
		*now = testutil.Now.Add(time.Duration(i)*30*time.Second + time.Second)
		*col = now.Add(-time.Second)
		m := e.Compute()
		if i < 3 {
			continue // the grace period's deadlines
		}
		if want := col.Add(maxAge); m.Next.At.Before(want) {
			t.Fatalf("at +%s: next deadline %+v, before the staleness deadline %v of the scrape at %v", now.Sub(testutil.Now), m.Next, want, *col)
		}
	}
}

// When scrapes stop, xDS rejects stays OK through C+max age and turns stale
// on the first recompute after it, which the check's wake time schedules.
func TestStoppedScrapesTurnXDSStaleOnTime(t *testing.T) {
	e, now, col := scrapeEngine(t)
	const maxAge = 45 * time.Second
	var m *Model
	for i := 0; i < 4; i++ { // fresh scrapes, all within the first run's max age, so the memo is reused
		*now = testutil.Now.Add(time.Duration(i)*10*time.Second + time.Second)
		*col = now.Add(-time.Second)
		m = e.Compute()
	}
	last := *col
	if want := (Deadline{At: last.Add(maxAge + time.Nanosecond)}); m.Next != want {
		t.Fatalf("after the last scrape: next %+v, want its staleness deadline %+v", m.Next, want)
	}
	*now = last.Add(maxAge)
	m = e.Compute()
	if r := xdsResult(t, m); !r.OK {
		t.Fatalf("at C+max age: %+v; want OK", r)
	}
	if want := (Deadline{At: last.Add(maxAge + time.Nanosecond)}); m.Next != want {
		t.Fatalf("at C+max age: next %+v, want %+v", m.Next, want)
	}
	*now = last.Add(maxAge + time.Nanosecond)
	if r := xdsResult(t, e.Compute()); r.OK || !strings.Contains(r.Error, "stale") {
		t.Fatalf("just after C+max age: %+v; want the stale error", r)
	}
}

// A memoized check that declares Catalog is skipped while the Catalog's key
// is unchanged and runs again when it changes.
func TestCatalogChangeRerunsCheck(t *testing.T) {
	var runs int
	c := counted{probe{"catalog probe", health.InputSet{Catalog: true}, func(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
		return okRun("catalog probe", ctx.Catalog.Key())(ctx)
	}}, &runs}
	cat := kinds.NewCatalog([]schema.GroupKind{{Kind: "Service"}}, nil)
	now := testutil.Now
	src := &memoSource{base: snapshot(t), now: &now}
	cfg := config(nil, metricsAt(testutil.Now), func() time.Time { return now })
	cfg.Snapshots, cfg.Collected, cfg.Checks = src, served(metricsAt(testutil.Now), 0), append(health.Upstream(), c)
	cfg.Catalog = func() kinds.Catalog { return cat }
	e := New(cfg)
	e.Compute()
	e.Compute()
	if runs != 1 {
		t.Fatalf("an unchanged Catalog ran the check: %d runs, want 1", runs)
	}
	cat = kinds.NewCatalog([]schema.GroupKind{{Kind: "Service"}, {Group: "acme.example", Kind: "Widget"}}, nil)
	if got := detailOf(report(t, e.Compute()), "catalog probe"); runs != 2 || got != cat.Key() {
		t.Fatalf("a changed Catalog must run the check: %d runs, detail %q", runs, got)
	}
}

// The backend endpoints check is memoized on Services and EndpointSlices: it
// skips unchanged ones and reruns when either changes.
func TestEndpointsChangeRerunsCheck(t *testing.T) {
	var runs int
	checks := health.Upstream()
	i := slices.IndexFunc(checks, func(c health.Check) bool { return c.Name() == "backend endpoints" })
	if i < 0 {
		t.Fatal("no backend endpoints check")
	}
	checks[i] = counted{checks[i], &runs}
	e, src, _ := memoEngine(t, checks...)
	set := func(f func(s *cache.Snapshot)) {
		s := *src.base
		s.Access = maps.Clone(s.Access)
		f(&s)
		src.base = &s
	}
	set(func(s *cache.Snapshot) {
		s.Access[cache.ServicesGVR], s.Access[cache.EndpointSlicesGVR] = cache.Access{OK: true}, cache.Access{OK: true}
		s.Services = []cache.ServiceFact{{Namespace: "shop", Name: "cart", Type: "ClusterIP", Ports: []cache.ServicePort{{Port: 80, Target: 80}}}}
		s.EndpointSlices = []cache.SliceFact{{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{{Port: 80}}, Ready: []int{1}, Total: 1}}
	})
	e.Compute()
	e.Compute()
	if runs != 1 {
		t.Fatalf("unchanged Services and EndpointSlices ran the check: %d runs, want 1", runs)
	}
	set(func(s *cache.Snapshot) {
		s.EndpointSlices = []cache.SliceFact{{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{{Port: 80}}, Ready: []int{0}, Total: 1}}
	})
	if e.Compute(); runs != 2 {
		t.Fatalf("a changed EndpointSlice must run the check: %d runs, want 2", runs)
	}
	set(func(s *cache.Snapshot) {
		s.Services = append(s.Services, cache.ServiceFact{Namespace: "shop", Name: "other", Type: "ClusterIP"})
	})
	if e.Compute(); runs != 3 {
		t.Fatalf("a changed Service must run the check: %d runs, want 3", runs)
	}
}

// Review 5.2 #2: without a max age the xDS check sets no wake time; a scrape
// of equal content with a new CollectedAt, published later than it was taken,
// still restamps the memoized result as an uncached compute stamps it.
func TestMemoRestampsScrapeTimeWithoutMaxAge(t *testing.T) {
	engine := func(noMemo bool) (*Engine, *time.Time, *collect.Store) {
		n := testutil.Now
		store := collect.New(nil, func() time.Time { return n })
		cfg := config(nil, metricsAt(testutil.Now), func() time.Time { return n })
		cfg.Snapshots, cfg.Collected, cfg.NoMemo = &memoSource{base: snapshot(t), now: &n}, store.Snapshot, noMemo
		return New(cfg), &n, store
	}
	e, now, store := engine(false)
	plain, pnow, pstore := engine(true)
	for i := range 3 {
		scraped := testutil.Now.Add(time.Duration(i) * 10 * time.Second)
		*now, *pnow = scraped.Add(time.Second), scraped.Add(time.Second)
		r, _ := metricsAt(scraped).Get("kgw.kgateway")
		res := ctrlmetrics.Results{ByInstall: map[string]ctrlmetrics.Result{"kgw.kgateway": r}}
		ctrlmetrics.Publish(store, res) // at now, a second after the scrape
		ctrlmetrics.Publish(pstore, res)
		got, want := xdsResult(t, e.Compute()), xdsResult(t, plain.Compute())
		if !got.CollectedAt.Equal(scraped) || !reflect.DeepEqual(got, want) {
			t.Fatalf("scrape %d: memoized %v, uncached %v; want the scrape time %v", i, got.CollectedAt, want.CollectedAt, scraped)
		}
	}
}
