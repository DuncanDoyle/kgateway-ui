package engine

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale/measure"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrllogs"
	"github.com/DuncanDoyle/kgateway-ui/pkg/diagnose"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

// scaleEngine is an upstream engine over tier's scale snapshot with fresh
// metrics. Under Contradict it has computed once and its clock is past the
// window and the grace end, so every check that can mature does.
func scaleEngine(tier scale.Tier) *Engine { return snapshotEngine(scale.Snapshot(tier, 1), tier) }

// snapshotEngine is scaleEngine over snap.
func snapshotEngine(snap *cache.Snapshot, tier scale.Tier) *Engine {
	now := testutil.Now
	cfg := config(snap, metricsAt(testutil.Now), func() time.Time { return now })
	cfg.MetricsMaxAge = 0
	e := New(cfg)
	if tier.Contradict {
		e.Compute()
		now = now.Add(max(Window, health.Grace) + time.Second)
	}
	return e
}

// forgetViews makes e's next Compute recompute every view, as when their
// input changed; the memo's old inputs and results stay live until then.
func (e *Engine) forgetViews() {
	for _, m := range e.views {
		m.set = false
	}
}

// BenchmarkCompute measures one warm Compute per iteration: the engine has
// already seen the snapshot once, as after the first run in production. The
// views are recomputed each time; the hit sub-benchmark reuses them.
func BenchmarkCompute(b *testing.B) {
	for _, tier := range append(slices.Clone(scale.Tiers), scale.Large.Contradicting()) {
		b.Run(tier.Name, func(b *testing.B) {
			e := scaleEngine(tier)
			e.Compute()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				e.forgetViews()
				e.Compute()
			}
		})
	}
	b.Run("large-hit", func(b *testing.B) {
		e := scaleEngine(scale.Large)
		e.Compute()
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			e.Compute()
		}
	})
	b.Run(measuredTier.Name, func(b *testing.B) {
		e, _, _ := measureEngine(b)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			e.forgetViews()
			e.Compute()
		}
	})
}

// BenchmarkColdCompute measures the first Compute of a fresh engine, before any window matures.
func BenchmarkColdCompute(b *testing.B) {
	for _, tier := range []scale.Tier{scale.Large, scale.Large.Contradicting()} {
		snap := scale.Snapshot(tier, 1)
		b.Run(tier.Name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				b.StopTimer()
				e := New(config(snap, metricsAt(testutil.Now), func() time.Time { return testutil.Now }))
				b.StartTimer()
				e.Compute()
			}
		})
	}
}

// liveSnapshot is tier's world from a settled cache, whose Source has the kind
// prints check memos key on; the worst case comes from LivePhase3.
func liveSnapshot(tb testing.TB, tier scale.Tier) *cache.Snapshot {
	tb.Helper()
	if tier.IsWorst {
		return scale.LivePhase3(tb, tier, nil).Cache.Snapshot()
	}
	return scale.Live(tb, tier, nil).Cache.Snapshot()
}

// analyzerStage returns e's analyzer stage (checks, groups, rank, index) for its only
// install, with inputs built as compute builds them; memo sets the input prints.
// A non-nil scope replays every other check's results, so phase3Checks times design §16.3's scope.
// With scrape > 0 as the metrics max age, each run reads a new scrape of unchanged content.
func analyzerStage(tb testing.TB, e *Engine, memo bool, scope []string, scrape time.Duration) func() *diagnose.Diagnosis {
	tb.Helper()
	m := e.Compute()
	snap, now, served := m.Snapshot, e.cfg.Now(), e.cfg.Served
	found, _ := install.Discover(snap, e.cfg.Detectors)
	all := install.Resolve(found, e.cfg.Scope)
	asg := install.Assign(snap, served, all, e.decode)
	if len(m.Installs) != 1 || len(m.MetricsRead) != 1 {
		tb.Fatalf("installs %d, metrics reads %d", len(m.Installs), len(m.MetricsRead))
	}
	im, read := m.Installs[0], m.MetricsRead[0]
	var kc kinds.Change
	if e.cfg.KindsChange != nil {
		kc = e.cfg.KindsChange()
	}
	var col collect.Snapshot
	if e.cfg.Collected != nil {
		col = e.cfg.Collected()
	}
	cat := benchCatalog(served)
	if e.cfg.Catalog != nil {
		cat = e.cfg.Catalog()
	}
	res := im.Resolved
	maxAge, scrapes := e.cfg.MetricsMaxAge, 0
	if scrape > 0 {
		maxAge = scrape
		read.Result.CollectedAt = now.Add(-time.Second) // fresh under maxAge
	}
	ver := e.cfg.Verifier.Verify(scopeverify.Input{Install: res, Snapshot: snap, Served: served, Metrics: read.Result, HasMetrics: read.Present,
		MetricsMaxAge: maxAge, Now: now, Views: e.decode})
	in := health.Input{Install: res, Owned: im.Owned, Assignment: asg, Snapshot: snap, Served: served, KindsChange: kc, Metrics: read.Result,
		HasMetrics: read.Present, MetricsMaxAge: maxAge, FirstSeen: e.firstSeen(now), Verify: ver, Now: now, Platform: e.cfg.Platform,
		ProxyMode: e.cfg.ProxyMode, Computed: im.Computed, Views: e.decode, Catalog: cat}
	in.Process, in.GatewayAPI, in.Rules = im.Process, gatewayAPI(im.Rules), im.Rules
	checks := e.cfg.Checks
	if scope != nil {
		checks = scopedChecks(tb, e, in, col, scope)
	}
	run := func() *diagnose.Diagnosis {
		if scrape > 0 {
			// A new scrape, 1 or 2 s old, so each run's CollectedAt differs from the last.
			scrapes++
			read.Result.CollectedAt = now.Add(-time.Duration(scrapes%2+1) * time.Second)
			in.Metrics = read.Result
		}
		rt := health.Runtime{States: e.states[res.ID()], Collected: col}
		var pc *printCtx
		if memo {
			pc = &printCtx{snap: snap, res: res, all: all, read: read, now: now, maxAge: maxAge, ver: &ver, views: im.Computed, kc: kc, cat: cat, col: col,
				proc: im.Process}
			rt.Print = func(set health.InputSet) (uint64, bool) { return e.inputPrint(set, pc) }
		}
		rep := health.Evaluate(in, checks, rt)
		d := e.diagnosis(&rep, diagnose.Input{Install: res, Owned: im.Owned, Snapshot: snap, Served: served, Views: e.decode, IDs: rt.States.GroupIDs()}, pc)
		if len(rep.Findings) != len(im.Health.Findings) || len(d.Groups) != len(im.Diagnosis.Groups) {
			tb.Fatalf("the stage gave %d findings in %d groups, the model %d in %d", len(rep.Findings), len(d.Groups), len(im.Health.Findings), len(im.Diagnosis.Groups))
		}
		return d
	}
	run()
	return run
}

// phase3Checks are the checks of design §16.3's analyzer-stage scope (ruling
// R35): KGWUI-001 as re-specified, 019–022, 025 and 026. The 013–017 come from
// the resource status check, replayed but identified, grouped and ranked.
var phase3Checks = []string{"xDS rejects", "process restarts", "probe failures", "stuck rollouts", "version skew", "proxy config warming", "proxy connection"}

// replayed is a check whose Run returns its results from one earlier run.
// Its inputs never change, so a memoized stage reuses it as an unchanged check.
type replayed struct {
	health.Check
	fs []health.Finding
	cr health.CheckResult
}

func (replayed) Inputs() health.InputSet { return health.InputSet{Owned: true} }

func (r replayed) Run(health.RunContext) ([]health.Finding, health.CheckResult) { return r.fs, r.cr }

// recorder reads every named check's output on its run; it raises nothing.
type recorder struct {
	names []string
	outs  map[string]health.CheckOutput
}

func (*recorder) Name() string { return "replay recorder" }

func (r *recorder) Inputs() health.InputSet { return health.InputSet{Checks: r.names} }

func (r *recorder) Run(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
	r.outs = map[string]health.CheckOutput{}
	for _, n := range r.names {
		r.outs[n], _ = ctx.Output(n)
	}
	return nil, health.CheckResult{Name: r.Name(), OK: true}
}

// scopedChecks are e's checks with every check outside scope replayed from
// its output in one run of all of them over in, on scratch Stores so e's are
// untouched. Runs at the measurement world's earlier computes go first, so
// held findings (016, 018) mature as in the model. A replayed check's findings
// are still identified on every warm run.
func scopedChecks(tb testing.TB, e *Engine, in health.Input, col collect.Snapshot, scope []string) []health.Check {
	tb.Helper()
	rec := &recorder{}
	for _, c := range e.cfg.Checks {
		rec.names = append(rec.names, c.Name())
	}
	all := append(slices.Clone(e.cfg.Checks), rec)
	st := health.NewStates()
	for _, at := range []time.Time{testutil.Now, matureAt, in.Now} {
		if !at.After(in.Now) {
			run := in
			run.Now = at
			health.Evaluate(run, all, health.Runtime{States: st, Collected: col})
		}
	}
	out := make([]health.Check, 0, len(e.cfg.Checks))
	n := 0
	for _, c := range e.cfg.Checks {
		if slices.Contains(scope, c.Name()) {
			out = append(out, c)
			n++
			continue
		}
		o := rec.outs[c.Name()]
		fs := make([]health.Finding, 0, len(o.Findings))
		for _, f := range o.Findings {
			f.ID, f.Group = "", ""
			fs = append(fs, f)
		}
		out = append(out, replayed{c, fs, o.Result})
	}
	if n != len(scope) {
		tb.Fatalf("%d of the %d Phase 3 checks are configured", n, len(scope))
	}
	return out
}

// analyzerTiers are the BenchmarkAnalyzerStage worlds: Large, and the 3.2 measurement world.
func analyzerEngine(tb testing.TB, tier scale.Tier) *Engine {
	if tier.Name == measuredTier.Name {
		e, _, _ := measureEngine(tb)
		return e
	}
	return snapshotEngine(liveSnapshot(tb, tier), tier)
}

// BenchmarkAnalyzerStage measures the analyzer stage at Large and in the 3.2
// measurement world: warm runs every check, memoized reuses unchanged checks;
// "scope" replays the Phase 1–2 checks (design §16.3, ruling R35).
func BenchmarkAnalyzerStage(b *testing.B) {
	for _, mode := range []struct {
		name   string
		memo   bool
		scope  []string
		scrape time.Duration
	}{{"warm", false, nil, 0}, {"memoized", true, nil, 0}, {"warm-scope", false, phase3Checks, 0}, {"memoized-scope", true, phase3Checks, 0},
		{"memoized-scope-scrape", true, phase3Checks, prodMaxAge}} {
		b.Run(mode.name, func(b *testing.B) {
			for _, tier := range []scale.Tier{scale.Large, measuredTier} {
				b.Run(tier.Name, func(b *testing.B) {
					run := analyzerStage(b, analyzerEngine(b, tier), mode.memo, mode.scope, mode.scrape)
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						run()
					}
				})
			}
		})
	}
}

// matureAt is a time past every window opened at testutil.Now, and past a
// missing child entry's grace end.
var matureAt = testutil.Now.Add(max(Window, health.Grace) + time.Second)

// progressAt is 5 min after matureAt, when 003 warnings first show: 018's hold
// has matured, as has 016's past the policies' grace.
var progressAt = matureAt.Add(5 * time.Minute)

// prodMaxAge is the binary's metrics max age: 3 × the default 10 s interval (R70).
const prodMaxAge = 30 * time.Second

// measuredTier is the tier of the 3.2 measurement world (Task 46).
var measuredTier = scale.Large.Contradicting().Worst()

// measureEngine is an engine over the 3.2 measurement world on a fake clock:
// computed at testutil.Now around a proxy stats scrape, then at matureAt and
// progressAt after one more each, so every window, hold and proxy condition has matured.
func measureEngine(tb testing.TB) (*Engine, *measure.World, *clocktesting.FakeClock) {
	tb.Helper()
	fc := testutil.FakeClock()
	w := measure.New(tb, measuredTier, fc)
	return measuredEngine(tb, w, fc, w.Live.Cache.Snapshot()), w, fc
}

// benchCatalog serves served and core Services, Secrets and ConfigMaps.
func benchCatalog(served []kinds.Served) kinds.Catalog {
	gks := []schema.GroupKind{{Kind: "Service"}, {Kind: "Secret"}, {Kind: "ConfigMap"}}
	for _, s := range served {
		gks = append(gks, schema.GroupKind{Group: s.Group, Kind: s.Kind})
	}
	return kinds.NewCatalog(gks, nil)
}

// typoBackend is a backendRef whose group serves no such kind.
var typoBackend = map[string]any{"group": "gateway.kgateway.example", "kind": "Backend", "name": "typo"}

// withTypoRef returns snap with typoBackend added to the first route whose
// status has ResolvedRefs=False, so that 013 reaches explanation 1.
func withTypoRef(tb testing.TB, snap *cache.Snapshot) *cache.Snapshot {
	tb.Helper()
	for g, us := range snap.Objects {
		for i, u := range us {
			if u.GetKind() != "HTTPRoute" || !strings.Contains(fmt.Sprint(u.Object["status"]), "BackendNotFound") {
				continue
			}
			c := u.DeepCopy()
			rules, _, _ := unstructured.NestedSlice(c.Object, "spec", "rules")
			r := rules[0].(map[string]any)
			r["backendRefs"] = append(r["backendRefs"].([]any), maps.Clone(typoBackend))
			_ = unstructured.SetNestedSlice(c.Object, rules, "spec", "rules")
			c.SetResourceVersion(u.GetResourceVersion() + "-typo")
			out := *snap
			out.Objects = maps.Clone(snap.Objects)
			out.Objects[g] = slices.Clone(us)
			out.Objects[g][i] = c
			return &out
		}
	}
	tb.Fatal("no route with ResolvedRefs=False/BackendNotFound")
	return nil
}

// measuredEngine is measureEngine's engine over w's snapshot snap, with one
// typo'd backendRef and a Catalog, so 013 explanation 1 is measured too.
func measuredEngine(tb testing.TB, w *measure.World, fc *clocktesting.FakeClock, snap *cache.Snapshot) *Engine {
	tb.Helper()
	snap = withTypoRef(tb, snap)
	cfg := config(snap, w.Metrics, fc.Now)
	cfg.MetricsMaxAge, cfg.Collected = 0, w.Store.Snapshot
	cat := benchCatalog(cfg.Served)
	cfg.Catalog = func() kinds.Catalog { return cat }
	e := New(cfg)
	// The current model, as the app reads it, so no older model stays alive.
	w.SetInstalls(func() ([]proxystats.Install, *cache.Snapshot) { return statsInstalls(e.Current()) })
	e.Compute()
	w.Scrape(tb)
	e.Compute()
	for _, at := range []time.Time{matureAt, progressAt} {
		fc.SetTime(at)
		w.Scrape(tb)
		e.Compute()
	}
	measuredSanity(tb, e.Current().Model, w)
	return e
}

// statsInstalls are p's installs and Gateways, as the app feeds the proxy stats collector.
func statsInstalls(p *Published) ([]proxystats.Install, *cache.Snapshot) {
	if p == nil {
		return nil, nil
	}
	m := p.Model
	out := make([]proxystats.Install, 0, len(m.Installs))
	for _, im := range m.Installs {
		in := proxystats.Install{Resolved: im.Resolved}
		if im.Owned != nil {
			for _, g := range im.Owned.Gateways {
				in.Gateways = append(in.Gateways, objects.RefOf(g))
			}
		}
		out = append(out, in)
	}
	return out, m.Snapshot
}

// measuredCodes are the codes the measurement world must raise on every Gateway.
var measuredCodes = []string{health.CodeXDSRejected, health.CodeProcessRestarting, health.CodeProbeFailing, health.CodeRolloutStuck,
	health.CodeVersionSkew, health.CodeProxyConfigWarming, health.CodeProxyDisconnected}

// analyzerCodes are the 3.3 codes the measurement world must raise; the
// families in sharedGroups must each hold a group with two or more findings.
var (
	analyzerCodes = []string{health.CodeRefUnresolved, health.CodeRefNotPermitted, health.CodeRuleReplaced, health.CodePolicyTargetNotFound,
		health.CodeListenerConflicted, health.CodeStatusNotProgressing}
	sharedGroups = []string{"Missing backend", "Not permitted", "Replaced by policy", "Status not progressing"}
)

// measuredSanity fails tb unless m raises measuredCodes on every Gateway, 001
// with proxy stats evidence, analyzerCodes in shared groups, every check is
// available, KGWUI-012 matured and w's log reader kept its full ring.
func measuredSanity(tb testing.TB, m *Model, w *measure.World) {
	tb.Helper()
	in := m.Installs[0]
	gws := len(in.Owned.Gateways)
	byCode := map[string]int{}
	withStats := 0
	for _, f := range in.Health.Findings {
		byCode[f.Code]++
		if ev, ok := f.Facts.(*health.XDSRejectsEvidence); ok && len(ev.ProxyStats) > 0 && f.ID != "" && f.Group != "" {
			withStats++
		}
	}
	// Probe failures land on the Pods that carry an Unhealthy Event: most, not all, Gateways.
	for _, c := range measuredCodes {
		if want := gws; byCode[c] < want && (c != health.CodeProbeFailing || byCode[c] == 0) {
			tb.Fatalf("%s on %d of %d Gateways: %v", c, byCode[c], gws, byCode)
		}
	}
	if !slices.ContainsFunc(in.Health.Findings, func(f health.Finding) bool {
		ev, ok := f.Facts.(*health.RefsEvidence)
		return ok && ev.Explanation == health.RefExplainKindNotServed
	}) {
		tb.Fatalf("no 013 or 014 reaches explanation 1: %v", byCode)
	}
	if withStats != gws || byCode[health.CodeComputedDisagrees] == 0 {
		tb.Fatalf("001 with proxy stats on %d of %d Gateways; %d KGWUI-012", withStats, gws, byCode[health.CodeComputedDisagrees])
	}
	for _, c := range in.Health.Checks {
		if c.Unavailable != "" || !c.OK && c.Name != "xDS rejects" {
			tb.Fatalf("check %+v", c)
		}
	}
	if in.Process == nil || len(in.Process.Proxies) != gws || in.Diagnosis == nil || len(in.Diagnosis.Groups) == 0 {
		tb.Fatalf("process data for %d Gateways, diagnosis %v", len(in.Process.Proxies), in.Diagnosis != nil)
	}
	for _, c := range analyzerCodes {
		if byCode[c] == 0 {
			tb.Fatalf("no %s: %v", c, byCode)
		}
	}
	shared, families := map[string]int{}, map[string]int{}
	for _, g := range in.Diagnosis.Groups {
		families[g.Family]++
		if len(g.Members) > 1 {
			shared[g.Family]++
		}
	}
	for _, f := range sharedGroups {
		if shared[f] == 0 {
			tb.Fatalf("no %q group with two or more findings: %v", f, shared)
		}
	}
	logs, _ := w.Logs.Value()
	if l := logs[in.Resolved.ID()]; len(l.Events) != ctrllogs.MaxEvents || l.State != ctrllogs.StateOn {
		tb.Fatalf("the log reader kept %d events, state %s", len(l.Events), l.State)
	}
	tb.Logf("measurement world: %d Gateways, %d findings %v, %d groups %v (%v with two or more findings), %d derived log events",
		gws, len(in.Health.Findings), byCode, len(in.Diagnosis.Groups), families, shared, ctrllogs.MaxEvents)
}

// denseSnapshot is a synced snapshot of scale.DenseSelection(parents, routes)
// with one kgateway v2.4.7 controller, as scale.Snapshot builds a tier's.
func denseSnapshot(parents, routes int) *cache.Snapshot {
	byKind := map[schema.GroupKind]schema.GroupVersionResource{}
	access := map[schema.GroupVersionResource]cache.Access{}
	for _, s := range testutil.ServedUpstream() {
		byKind[schema.GroupKind{Group: s.Group, Kind: s.Kind}] = s.GVR()
		access[s.GVR()] = cache.Access{OK: true}
	}
	objs := map[schema.GroupVersionResource][]*unstructured.Unstructured{}
	for _, o := range scale.DenseSelection(parents, routes) {
		gvr := byKind[o.GroupVersionKind().GroupKind()]
		objs[gvr] = append(objs[gvr], o)
	}
	for _, g := range []schema.GroupVersionResource{cache.NamespacesGVR, cache.PodsGVR, cache.DeploymentsGVR} {
		access[g] = cache.Access{OK: true}
	}
	s := &cache.Snapshot{Objects: objs, Access: access, Synced: true, TakenAt: testutil.Now}
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	s.Namespaces = append(testutil.ScaleNamespaces(2), testutil.Namespace("kgw", nil))
	s.Deployments = append(s.Deployments, d, testutil.ProxyDeployment(scale.DenseParents, "dense", 1, 1))
	s.Pods = append(s.Pods, testutil.ControllerPod(d, "kgateway-0", true))
	return s
}

// BenchmarkDenseSelection measures one warm Compute over the dense-selection
// shape of design §9.6 and reports its rows and whether a cap was hit. Two
// fresh engines must agree on the cap and the last row's key and outcome: the
// cut is deterministic.
func BenchmarkDenseSelection(b *testing.B) {
	for _, c := range []struct {
		name            string
		parents, routes int
	}{{"large", 20, 500}, {"extreme", 100, 1000}} {
		b.Run(c.name, func(b *testing.B) {
			snap := denseSnapshot(c.parents, c.routes)
			deleg := func(e *Engine) computed.DelegationResult {
				m := e.Compute()
				if len(m.Installs) != 1 || m.Installs[0].Computed == nil {
					b.Fatalf("installs %d", len(m.Installs))
				}
				return m.Installs[0].Computed.Delegation
			}
			newEngine := func() *Engine {
				cfg := config(snap, metricsAt(testutil.Now), func() time.Time { return testutil.Now })
				cfg.Logger = slog.New(slog.DiscardHandler) // the cap warning would split a result line
				return New(cfg)
			}
			e := newEngine()
			first, second := deleg(e), deleg(newEngine())
			if len(first.Rows) == 0 || first.Cap != second.Cap || len(first.Rows) != len(second.Rows) ||
				first.Rows[len(first.Rows)-1].Key != second.Rows[len(second.Rows)-1].Key ||
				first.Rows[len(first.Rows)-1].Outcome != second.Rows[len(second.Rows)-1].Outcome {
				b.Fatalf("two runs differ: cap %q/%q, %d/%d rows", first.Cap, second.Cap, len(first.Rows), len(second.Rows))
			}
			last := first.Rows[len(first.Rows)-1]
			b.Logf("%s: %d parents × %d routes: cap %q, %d rows, last row %s rule %d ref %d → %s (%s)",
				c.name, c.parents, c.routes, first.Cap, len(first.Rows), last.Key.Parent, last.Key.Rule, last.Key.Ref, last.Key.Child, last.Outcome)
			capped := 0.0
			if first.Cap != "" {
				capped = 1
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				e.forgetViews()
				e.Compute()
			}
			b.ReportMetric(float64(len(first.Rows)), "rows")
			b.ReportMetric(capped, "capped")
		})
	}
}

// The Large world yields one install that owns every HTTPRoute: delegated
// children through their reported edges (Q12), each with a reported parent.
func TestScaleComputeSanity(t *testing.T) {
	snap := scale.Snapshot(scale.Large, 1)
	children := map[string]bool{}
	for _, r := range snap.Objects[gvr(t, "HTTPRoute")] {
		for _, p := range r.Object["status"].(map[string]any)["parents"].([]any) {
			if p.(map[string]any)["parentRef"].(map[string]any)["kind"] == "HTTPRoute" {
				children[r.GetNamespace()+"/"+r.GetName()] = true
			}
		}
	}
	m := New(config(snap, metricsAt(testutil.Now), func() time.Time { return testutil.Now })).Compute()
	if len(m.Installs) != 1 {
		t.Fatalf("installs %d", len(m.Installs))
	}
	owned, reported := 0, 0
	for _, r := range m.Installs[0].Owned.Routes {
		if r.Kind.Kind != "HTTPRoute" {
			continue
		}
		owned++
		if children[r.Obj.GetNamespace()+"/"+r.Obj.GetName()] && len(r.RouteParents) > 0 {
			reported++
		}
	}
	if len(children) == 0 || owned != scale.Large.HTTPRoutes || reported != len(children) {
		t.Fatalf("owned HTTPRoutes %d, want %d; children with a reported parent %d, want %d", owned, scale.Large.HTTPRoutes, reported, len(children))
	}
	// Views B and C agree with the consistent world: no delegation check is a candidate (PF2).
	v := m.Installs[0].Computed
	n := 0
	for _, c := range v.Checks {
		if c.ID.Kind == computed.KindListenerCount || c.ID.Kind == computed.KindRouteAccepted {
			continue
		}
		if n++; c.Candidate() {
			t.Fatalf("%s %s: %s, expected %s observed %s", c.ID.Kind, c.On, c.Class, c.Expected, c.Observed)
		}
	}
	if len(v.Delegation.Rows) == 0 || n == 0 {
		t.Fatalf("%d delegation rows, %d delegation checks", len(v.Delegation.Rows), n)
	}
}

// Every tier's statuses agree with the computed views, so the budgets
// measure realistic data: every check is compatible, except that a view B
// check may be unknown when a compute cap was hit (Extreme). Under
// Contradicting every view A and parent check contradicts and every child
// entry is missing; a parent whose own entry was dropped is stale.
func TestScaleViewsConsistent(t *testing.T) {
	tiers := append(slices.Clone(scale.Tiers), scale.Large.Contradicting())
	for _, tier := range tiers {
		t.Run(tier.Name, func(t *testing.T) {
			m := New(config(scale.Snapshot(tier, 1), metricsAt(testutil.Now), func() time.Time { return testutil.Now })).Compute()
			capped := m.Installs[0].Computed.Delegation.Cap != ""
			want := func(c computed.CheckResult) bool {
				viewA := c.ID.Kind == computed.KindListenerCount || c.ID.Kind == computed.KindRouteAccepted
				switch {
				case !tier.Contradict && !viewA && capped && c.Class == computed.ClassUnknown:
					return true
				case !tier.Contradict:
					return c.Class == computed.ClassCompatible
				case c.ID.Kind == computed.KindChildEntry:
					return c.Class == computed.ClassMissing
				case c.ID.Kind == computed.KindParentResolvedRefs && !c.ParentCurrent:
					return c.Class == computed.ClassStale
				}
				return c.Class == computed.ClassContradicting
			}
			checks := m.Installs[0].Computed.Checks
			bad := 0
			for _, c := range checks {
				if !want(c) {
					if bad++; bad <= 5 {
						t.Errorf("%s %s: %s, expected %s observed %s", c.ID.Kind, c.On, c.Class, c.Expected, c.Observed)
					}
				}
			}
			if bad > 0 || len(checks) == 0 {
				t.Fatalf("%d of %d checks not as expected", bad, len(checks))
			}
		})
	}
}
