package engine

import (
	"fmt"
	"hash/fnv"
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
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
	add := func(g schema.GroupVersionResource, o metav1.Object) {
		h := fnv.New64a()
		_, _ = fmt.Fprintf(h, "%s\x00%s", o.GetUID(), o.GetResourceVersion())
		k := f.Kinds[g.String()]
		k.Count++
		k.Hash += h.Sum64()
		f.Kinds[g.String()] = k
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
	cfg.Snapshots, cfg.MetricsMaxAge = src, 0
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

// xDS rejects (Clock) runs every time; a narrow metrics check waking at the
// staleness deadline turns unavailable exactly then.
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
	narrow := counted{probe{"narrow metrics", health.InputSet{Metrics: true}, func(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
		at := ctx.Metrics.CollectedAt.Add(maxAge)
		if !ctx.HasMetrics || !ctx.Now.Before(at) {
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
	if xdsRuns != 3 {
		t.Fatalf("xDS rejects ran %d times in 3 computes", xdsRuns)
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
