//go:build bench

package engine

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/internal/testutil/benchgate"
	"github.com/DuncanDoyle/snellius/internal/testutil/scale"
	"github.com/DuncanDoyle/snellius/internal/testutil/scale/measure"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/trigger"
)

// The 2.1 budgets of design §9.6 at Large, over a real cache on fake clients.

// world is a settled cache over tier's world with its change signal drained.
type world struct {
	c      *cache.Cache
	update func(context.Context, *unstructured.Unstructured) error
	sig    *trigger.Signal
	routes []*unstructured.Unstructured // the seeded HTTPRoutes
	gvr    schema.GroupVersionResource  // HTTPRoutes
}

// newWorld is scale.Live under the names these tests use.
func newWorld(tb testing.TB, tier scale.Tier, now func() time.Time) *world {
	tb.Helper()
	return fromLive(scale.Live(tb, tier, now))
}

func fromLive(l *scale.LiveWorld) *world {
	return &world{c: l.Cache, update: l.Update, sig: l.Signal, routes: l.Routes, gvr: l.RouteGVR}
}

// engine returns an engine over w's cache with static metrics that never go
// stale, driven by clk (nil: the real clock), reporting loop events on a
// buffered channel; mut, if set, adjusts the config.
func (w *world) engine(tb testing.TB, clk *clocktesting.FakeClock, mut ...func(*Config)) (*Engine, chan LoopEvent) {
	ev := make(chan LoopEvent, 1<<16)
	cfg := config(nil, metricsAt(testutil.Now), nil)
	// Static metrics never go stale here, as a scraped cluster's do not.
	cfg.Snapshots, cfg.Triggers, cfg.Collected = w.c, w.sig, served(metricsAt(testutil.Now), 0)
	if clk != nil {
		cfg.Clock = clk
	}
	for _, m := range mut {
		m(&cfg)
	}
	cfg.OnLoop = func(e LoopEvent) {
		select {
		case ev <- e:
		default:
			tb.Errorf("loop event buffer full")
		}
	}
	return New(cfg), ev
}

// runLoop starts e.Run with safety until the test ends, then waits for it to return.
func runLoop(t *testing.T, e *Engine, safety time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		e.Run(ctx, safety)
	}()
	t.Cleanup(func() {
		cancel()
		testutil.Recv(t, stopped, "Run to return")
	})
}

// awaitKind receives events up to one of kind k.
func awaitKind(t *testing.T, ev <-chan LoopEvent, k LoopEventKind) LoopEvent {
	t.Helper()
	for {
		if e := testutil.Recv(t, ev, "loop event"); e.Kind == k {
			return e
		}
	}
}

// percentile is the q-th percentile (nearest rank) of sorted d.
func percentile(d []time.Duration, q int) time.Duration {
	i := (len(d)*q + 99) / 100
	return d[max(i, 1)-1]
}

// TestBudgetBurst: 1,000 updates within 1 s give at most 2 recomputes in the
// burst window + 2 s, and event-to-publication p95 ≤ 1.2 s. Real clock.
func TestBudgetBurst(t *testing.T) {
	const n = 1000
	w := newWorld(t, scale.Large, nil)
	e, ev := w.engine(t, nil)
	runLoop(t, e, 30*time.Second)
	if p := awaitKind(t, ev, EvPublished).Published; !p.Model.Snapshot.Synced {
		t.Fatal("start-up model is not synced")
	}
	awaitKind(t, ev, EvConfirmed) // idle: nothing pending, the live source matches

	// Distinct routes, each given a new resourceVersion the snapshot must show.
	if len(w.routes) < n {
		t.Fatalf("%d HTTPRoutes, want %d", len(w.routes), n)
	}
	sent := make([]time.Time, n)
	rv := func(i int) string { return strconv.Itoa(1_000_000 + i) }
	ctx := context.Background()
	start := time.Now()
	for i := range n {
		// Paced 1 ms apart so the fake watch, which panics at 100 undrained events, keeps up.
		time.Sleep(time.Until(start.Add(time.Duration(i) * time.Millisecond)))
		u := w.routes[i].DeepCopy()
		u.SetResourceVersion(rv(i))
		u.SetAnnotations(map[string]string{"burst": strconv.Itoa(i)})
		sent[i] = time.Now()
		if err := w.update(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	end := time.Now()
	if d := end.Sub(start); d > 1100*time.Millisecond {
		t.Fatalf("the burst took %v, not about 1 s: the host cannot pace it", d)
	}

	// Collect publications until the window closes and every update is published.
	window := end.Add(2 * time.Second)
	lat := make([]time.Duration, n)
	done := make([]bool, n)
	left, pubs := n, 0
	for left > 0 || time.Now().Before(window) {
		var e LoopEvent
		select {
		case e = <-ev:
		case <-time.After(time.Until(window)):
			if left > 0 {
				t.Fatalf("%d updates unpublished at the window's end", left)
			}
			continue
		}
		if e.Kind != EvPublished {
			continue
		}
		if !e.At.After(window) {
			pubs++
		}
		have := map[string]string{}
		for _, u := range e.Published.Model.Snapshot.Objects[w.gvr] {
			have[u.GetNamespace()+"/"+u.GetName()] = u.GetResourceVersion()
		}
		for i := range n {
			if !done[i] && have[w.routes[i].GetNamespace()+"/"+w.routes[i].GetName()] == rv(i) {
				done[i], lat[i] = true, e.At.Sub(sent[i])
				left--
			}
		}
	}
	slices.Sort(lat)
	p50, p95, p99 := percentile(lat, 50), percentile(lat, 95), percentile(lat, 99)
	t.Logf("burst %v; recomputes in burst + 2 s: %d (budget ≤ 2); event-to-publication p50 %v p95 %v p99 %v max %v (budget p95 ≤ 1.2 s)",
		end.Sub(start).Round(time.Millisecond), pubs, p50.Round(time.Millisecond), p95.Round(time.Millisecond), p99.Round(time.Millisecond), lat[n-1].Round(time.Millisecond))
	if pubs > 2 {
		t.Errorf("recomputes in the burst window + 2 s = %d, budget ≤ 2", pubs)
	}
	if p95 > 1200*time.Millisecond {
		benchgate.Timing(t, "event-to-publication p95 %v, budget ≤ 1.2 s", p95)
	}
}

// TestBudgetIdleTenMinutes: on fake clocks, 600 one-second steps with no
// object change and a real collector scraping unchanged metrics every 10 s
// recompute only on the safety timer and Model.Next deadlines, never show
// metrics stale, and confirm every second. Safety 30 s and metrics max age
// 45 s differ, so every run has one cause: the safety timer, as each run reads
// a scrape at most 10 s old and the deadline is ≥ 35 s away.
func TestBudgetIdleTenMinutes(t *testing.T) {
	const safety, every, maxAge = 30 * time.Second, 10 * time.Second, 45 * time.Second
	clk := testutil.FakeClock()
	w := newWorld(t, scale.Large, clk.Now)
	s := newScraping(managedBody, maxAge)
	e, ev := w.engine(t, clk, s.wire)
	runLoop(t, e, safety)
	awaitKind(t, ev, EvPublished) // the start-up publication, before any scrape
	awaitKind(t, ev, EvArmed)
	s.start(t, every, e.Installs)
	awaitKind(t, ev, EvTriggered) // the first result; this run is not counted either
	awaitKind(t, ev, EvArmed)
	s.cc.Step(time.Second)
	clk.Step(time.Second)
	prev := awaitKind(t, ev, EvPublished)
	awaitKind(t, ev, EvArmed)
	d := idleDrive{ev: ev, eng: clk, s: s, safety: safety, every: every}
	n, _ := d.run(t, prev, 600)
	t.Logf("idle 10 min: %d recomputes (%d safety timer, %d staleness deadline, %d both), %d confirmations, %d scrapes",
		n.runs, n.onSafety, n.onDeadline, n.onBoth, n.confirms, n.scrapes)
	if n.runs != 20 || n.onSafety != 20 || n.confirms != 600 || n.scrapes != 60 {
		t.Errorf("idle 10 min: %+v; want 20 recomputes, all on the safety timer, 600 confirmations, 60 scrapes", n)
	}
}

// tick is the confirm tick's work on an idle loop: the live source,
// fingerprint, collected print and kinds, compared and swapped in.
func tick(tb testing.TB, e *Engine) {
	p := e.Current()
	if !e.liveSource(time.Now()).Equal(p.Source) || !e.confirm(p, p.ConfirmedAt) {
		tb.Fatal("the idle tick did not confirm")
	}
}

// TestBudgetConfirmTick: the median of 50 confirm ticks at Large ≤ 5 ms; Extreme reported.
func TestBudgetConfirmTick(t *testing.T) {
	for _, tier := range []scale.Tier{scale.Large, scale.Extreme} {
		w := newWorld(t, tier, nil)
		e, _ := w.engine(t, nil)
		e.Compute()
		d := make([]time.Duration, 50)
		for i := range d {
			t0 := time.Now()
			tick(t, e)
			d[i] = time.Since(t0)
		}
		slices.Sort(d)
		t.Logf("confirm tick %s: median %v, max %v (budget at Large ≤ 5 ms)", tier.Name, d[len(d)/2], d[len(d)-1])
		if tier.Name == scale.Large.Name && d[len(d)/2] > 5*time.Millisecond {
			benchgate.Timing(t, "confirm tick median at Large %v, budget ≤ 5 ms", d[len(d)/2])
		}
	}
}

// BenchmarkConfirmTick measures one idle confirm tick (PF24): fingerprint,
// metrics encoding, compare and swap.
func BenchmarkConfirmTick(b *testing.B) {
	for _, tier := range []scale.Tier{scale.Large, scale.Extreme} {
		b.Run(tier.Name, func(b *testing.B) {
			w := newWorld(b, tier, nil)
			e, _ := w.engine(b, nil)
			e.Compute()
			b.ReportAllocs()
			for b.Loop() {
				tick(b, e)
			}
		})
	}
}

// The 2.2–2.4 budgets of design §9.6 at Large: the explorer's index,
// views A, B and C with their retained contexts, KGWUI-012 persistence and the decode cache.

// largeCases are the consistent Large world, whose statuses agree with view
// A, and the worst case, where every check contradicts and has matured.
var largeCases = []scale.Tier{scale.Large, scale.Large.Contradicting()}

// largeEngine is an engine over tier's snapshot on a settable clock, with
// metrics that never go stale. Under Contradict it computes once at
// testutil.Now and moves the clock to matureAt, so the next Compute matures
// every check.
func largeEngine(snap *cache.Snapshot, tier scale.Tier) (*Engine, *time.Time) {
	now := new(time.Time)
	*now = testutil.Now
	cfg := config(snap, metricsAt(testutil.Now), func() time.Time { return *now })
	cfg.Collected = served(metricsAt(testutil.Now), 0)
	e := New(cfg)
	if tier.Contradict {
		e.Compute()
		*now = matureAt
	}
	return e, now
}

// TestBudgetWarmRecompute: at Large, consistent, contradicting and worst case, warm median ≤ 150 ms
// with views B and C and cold median ≤ 1 s (design §9.6); allocation and persistence reported (D-022).
func TestBudgetWarmRecompute(t *testing.T) {
	const mib = 1 << 20
	for _, tier := range append(slices.Clone(largeCases), measuredTier) {
		t.Run(tier.Name, func(t *testing.T) {
			snap := scale.Snapshot(tier, 1)
			fresh := func() *Engine {
				return New(config(snap, metricsAt(testutil.Now), func() time.Time { return testutil.Now }))
			}
			var e *Engine
			now := new(time.Time)
			if tier.IsWorst {
				// The 3.2 measurement world: its metrics, proxy stats and process data.
				var w *measure.World
				var fc *clocktesting.FakeClock
				e, w, fc = measureEngine(t)
				snap, *now = e.cfg.Snapshots.Snapshot(), fc.Now()
				fresh = func() *Engine {
					cfg := config(snap, metricsAt(testutil.Now), func() time.Time { return testutil.Now })
					cfg.Collected = w.Store.Snapshot
					return New(cfg)
				}
			} else {
				e, now = largeEngine(snap, tier)
			}
			cold := make([]time.Duration, 5)
			for i := range cold {
				// The first Compute of a fresh engine, before any window matures.
				c := fresh()
				t0 := time.Now()
				c.Compute()
				cold[i] = time.Since(t0)
			}
			m := e.Compute()
			warm := make([]time.Duration, 20)
			alloc := make([]uint64, len(warm))
			var ms runtime.MemStats
			for i := range warm {
				e.forgetViews() // the gate is a recompute with views B and C
				runtime.ReadMemStats(&ms)
				a0 := ms.TotalAlloc
				t0 := time.Now()
				m = e.Compute()
				warm[i] = time.Since(t0)
				runtime.ReadMemStats(&ms)
				alloc[i] = ms.TotalAlloc - a0
			}
			hit := make([]time.Duration, 20)
			for i := range hit {
				t0 := time.Now()
				e.Compute()
				hit[i] = time.Since(t0)
			}
			slices.Sort(hit)
			// The persistence step alone, over the last model's checks: same IDs and prints keep every window.
			views := []*computed.Views{m.Installs[0].Computed}
			step := make([]time.Duration, 20)
			for i := range step {
				t0 := time.Now()
				e.persist.persist(*now, views, nil)
				step[i] = time.Since(t0)
			}
			slices.Sort(cold)
			slices.Sort(warm)
			slices.Sort(alloc)
			slices.Sort(step)
			t.Logf("%s: %d checks, %d KGWUI-012 findings, %d persistence entries", tier.Name, len(views[0].Checks), len(disagreements(m)), len(e.persist.m))
			coldNote := ""
			if tier.Contradict {
				coldNote = ", a first compute with no matured windows"
			}
			t.Logf("%s: cold median %v max %v%s (budget median ≤ 1 s); warm median %v p95 %v max %v (budget median ≤ 150 ms); warm alloc median %.1f MiB max %.1f MiB (reported, D-022); persistence step median %v max %v",
				tier.Name, cold[len(cold)/2].Round(time.Millisecond), cold[len(cold)-1].Round(time.Millisecond), coldNote,
				warm[len(warm)/2].Round(100*time.Microsecond), percentile(warm, 95).Round(100*time.Microsecond), warm[len(warm)-1].Round(100*time.Microsecond),
				float64(alloc[len(alloc)/2])/mib, float64(alloc[len(alloc)-1])/mib,
				step[len(step)/2].Round(time.Microsecond), step[len(step)-1].Round(time.Microsecond))
			t.Logf("%s: warm median with unchanged views input %v (reported)", tier.Name, hit[len(hit)/2].Round(100*time.Microsecond))
			if tier.Contradict && len(disagreements(m)) == 0 {
				t.Fatal("the worst case raised no KGWUI-012")
			}
			if !tier.Contradict && (len(disagreements(m)) != 0 || len(e.persist.m) != 0) {
				t.Fatalf("the consistent world has %d KGWUI-012 findings and %d windows", len(disagreements(m)), len(e.persist.m))
			}
			if cold[len(cold)/2] > time.Second {
				benchgate.Timing(t, "cold recompute median %v, budget ≤ 1 s", cold[len(cold)/2])
			}
			if warm[len(warm)/2] > 150*time.Millisecond {
				benchgate.Timing(t, "warm recompute median %v, budget ≤ 150 ms", warm[len(warm)/2])
			}
		})
	}
}

// withProgress is phase3Checks with 018's check, which declares Clock and so
// always runs; its rows are reported, not gated (Task 71).
var withProgress = append(slices.Clone(phase3Checks), "status progress")

// Analyzer stage gates from sub-phase 3.2 (design §16.3).
const (
	analyzerWarmBudget     = 20 * time.Millisecond
	analyzerMemoizedBudget = 2 * time.Millisecond
)

// allocBudget caps the median bytes and allocations of one run.
type allocBudget struct{ bytes, allocs uint64 }

// analyzerAllocBudget gates the §16.3-scope stage per world and mode (D-044):
// the 3.2 measurement (in the comments; the same on darwin/arm64 and amd64) plus about 10 %.
var analyzerAllocBudget = map[string]allocBudget{
	"large/warm, §16.3 scope":                                              {192_000, 710},      // 173,864 B, 642
	"large/memoized, §16.3 scope":                                          {32_000, 540},       // 28,904 B, 491
	"large/memoized, §16.3 scope, new scrape each run":                     {35_000, 555},       // 31,480 B, 503
	"large-contradicting-worst/warm, §16.3 scope":                          {5_570_000, 20_700}, // 5,061,760 B, 18,837
	"large-contradicting-worst/memoized, §16.3 scope":                      {88_000, 1_620},     // 80,024 B, 1,471
	"large-contradicting-worst/memoized, §16.3 scope, new scrape each run": {1_027_000, 5_870},  // 933,136 B, 5,335
}

// runAllocs is the median bytes and allocations of one call of run, over n calls.
func runAllocs[T any](n int, run func() T) (bytes, allocs uint64) {
	b, a := make([]uint64, n), make([]uint64, n)
	var m0, m1 runtime.MemStats
	for i := range n {
		runtime.ReadMemStats(&m0)
		run()
		runtime.ReadMemStats(&m1)
		b[i], a[i] = m1.TotalAlloc-m0.TotalAlloc, m1.Mallocs-m0.Mallocs
	}
	slices.Sort(b)
	slices.Sort(a)
	return b[n/2], a[n/2]
}

// TestBudgetAnalyzerStage: at Large and in the 3.2 measurement world, design
// §16.3's stage (identity, groups, rank and the Phase 3 checks; ruling R35)
// has a median ≤ 20 ms warm and ≤ 2 ms memoized, also when each run reads a new
// scrape at the binary's max age (R70); each scoped mode's allocations per run
// stay in analyzerAllocBudget. The whole stage, Phase 1–2 checks included, is reported.
func TestBudgetAnalyzerStage(t *testing.T) {
	tiers := []scale.Tier{scale.Large, measuredTier}
	measured := map[string]bool{}
	for k := range analyzerAllocBudget {
		// A budget whose world is not run would never gate.
		if w, _, _ := strings.Cut(k, "/"); !slices.ContainsFunc(tiers, func(x scale.Tier) bool { return x.Name == w }) {
			t.Errorf("allocation budget %q names no world this test runs", k)
		}
	}
	for _, tier := range tiers {
		t.Run(tier.Name, func(t *testing.T) {
			e := analyzerEngine(t, tier)
			for _, mode := range []struct {
				name   string
				memo   bool
				scope  []string
				budget time.Duration
				scrape time.Duration
			}{{"warm, §16.3 scope", false, phase3Checks, analyzerWarmBudget, 0}, {"memoized, §16.3 scope", true, phase3Checks, analyzerMemoizedBudget, 0},
				{"memoized, §16.3 scope, new scrape each run", true, phase3Checks, analyzerMemoizedBudget, prodMaxAge},
				{"warm, §16.3 scope and 018", false, withProgress, 0, 0}, {"memoized, §16.3 scope and 018", true, withProgress, 0, 0},
				{"warm, whole stage", false, nil, 0, 0}, {"memoized, whole stage", true, nil, 0, 0}} {
				run := analyzerStage(t, e, mode.memo, mode.scope, mode.scrape)
				d := make([]time.Duration, 31)
				for i := range d {
					t0 := time.Now()
					run()
					d[i] = time.Since(t0)
				}
				slices.Sort(d)
				med := d[len(d)/2]
				note := "reported"
				if mode.budget > 0 {
					note = fmt.Sprintf("budget median ≤ %v", mode.budget)
				}
				t.Logf("%s: %s median %v p95 %v max %v (%s)", tier.Name, mode.name, med.Round(10*time.Microsecond),
					percentile(d, 95).Round(10*time.Microsecond), d[len(d)-1].Round(10*time.Microsecond), note)
				if mode.budget > 0 && med > mode.budget {
					benchgate.Timing(t, "%s: %s median %v, budget ≤ %v", tier.Name, mode.name, med, mode.budget)
				}
				if _, gated := analyzerAllocBudget[tier.Name+"/"+mode.name]; !gated && mode.scope != nil {
					bytes, n := runAllocs(31, run)
					t.Logf("%s: %s allocates median %d B in %d allocations per run (reported)", tier.Name, mode.name, bytes, n)
				}
				if a, ok := analyzerAllocBudget[tier.Name+"/"+mode.name]; ok {
					measured[tier.Name+"/"+mode.name] = true
					bytes, n := runAllocs(31, run)
					t.Logf("%s: %s allocates median %d B in %d allocations per run (budget ≤ %d B, ≤ %d allocations)", tier.Name, mode.name, bytes, n, a.bytes, a.allocs)
					if bytes > a.bytes || n > a.allocs {
						t.Errorf("%s: %s allocates %d B in %d allocations per run, budget ≤ %d B, ≤ %d", tier.Name, mode.name, bytes, n, a.bytes, a.allocs)
					}
				}
			}
			// A renamed or dropped mode fails its budget instead of skipping it.
			for k := range analyzerAllocBudget {
				if strings.HasPrefix(k, tier.Name+"/") && !measured[k] {
					t.Errorf("allocation budget %q: no mode measured it", k)
				}
			}
		})
	}
}

// heapBaseline is the median "retained" HeapInuse of the 2.1 engine at Large
// (docs/phases/phase-2/bench/2.2-baseline-heap.txt).
const heapBaseline = 25411584

// heapChildEnv makes TestBudgetHeap measure in this process and print one line.
const heapChildEnv = "SNELLIUS_HEAP_CHILD"

// heapRun is one child's measurement: HeapInuse at the baseline's three
// points, and the HeapAlloc freed by dropping each part in order.
type heapRun struct {
	Start, Snapshot, Retained uint64
	// SnapshotAlloc and RetainedAlloc are HeapAlloc at the snapshot and retained points.
	SnapshotAlloc, RetainedAlloc uint64
	Parts                        []heapPart
	// Watches is the HeapAlloc freed by stopping a fresh measurement world's proxy Pod, Event and CRD watches;
	// Watches34 by then dropping its Service, EndpointSlice and list-only kind informers.
	Watches, Watches34 int64
}

type heapPart struct {
	Name  string
	Freed int64
}

// TestBudgetHeap: measured as the 2.1 baseline was, one fresh process per run
// and five runs, the median retained heap at Large ≤ 48 MiB (D-057) above it, for the
// consistent world and the worst case. Each part is then dropped in turn; its
// freed HeapAlloc is reported per kind. The 3.2 watches' state is gated as its own part; the 3.4 watches' is reported.
func TestBudgetHeap(t *testing.T) {
	cases := append(slices.Clone(largeCases), measuredTier, scale.Large.Paths())
	if name := os.Getenv(heapChildEnv); name != "" {
		i := slices.IndexFunc(cases, func(x scale.Tier) bool { return x.Name == name })
		heapChild(t, cases[i])
		return
	}
	for _, tier := range cases {
		t.Run(tier.Name, func(t *testing.T) { heapBudget(t, tier) })
	}
}

// heap31 is the 3.1 baseline's median retained heap over the 2.1 baseline per world
// (bench/3.1-baseline.txt); the measurement world is held to the contradicting one.
var heap31 = map[string]float64{"large": 23.2, "large-contradicting": 24.4, "large-contradicting-worst": 24.4, "large-paths": 23.2}

// heapBudget runs five heap children for tier and checks their median.
func heapBudget(t *testing.T, tier scale.Tier) {
	const mib = 1 << 20
	var runs []heapRun
	for range 5 {
		cmd := exec.Command(os.Args[0], "-test.run=^TestBudgetHeap$", "-test.count=1")
		cmd.Env = append(os.Environ(), heapChildEnv+"="+tier.Name)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("heap child: %v\n%s", err, out)
		}
		var r heapRun
		for line := range strings.Lines(string(out)) {
			if j, ok := strings.CutPrefix(line, "heap: "); ok {
				if err := json.Unmarshal([]byte(j), &r); err != nil {
					t.Fatal(err)
				}
			}
		}
		if r.Retained == 0 {
			t.Fatalf("heap child printed no result:\n%s", out)
		}
		runs = append(runs, r)
	}
	t.Logf("run  start (B)  snapshot (B)  retained (B)  engine+model over snapshot (B)")
	for i, r := range runs {
		t.Logf("%d    %d    %d      %d      %d", i+1, r.Start, r.Snapshot, r.Retained, int64(r.Retained)-int64(r.Snapshot))
	}
	slices.SortFunc(runs, func(a, b heapRun) int { return cmp.Compare(a.Retained, b.Retained) })
	med := runs[len(runs)/2]
	over := int64(med.Retained) - heapBaseline
	t.Logf("median retained %d B (%.1f MiB); %+.1f MiB over the 2.1 baseline %d B (budget ≤ 48 MiB); %+.1f MiB over the 3.1 baseline's %+.1f (budget ≤ +24 MiB)",
		med.Retained, float64(med.Retained)/mib, float64(over)/mib, heapBaseline, float64(over)/mib-heap31[tier.Name], heap31[tier.Name])
	for _, p := range med.Parts {
		t.Logf("  %-28s %7.2f MiB", p.Name, float64(p.Freed)/mib)
	}
	watches, watches34 := make([]int64, len(runs)), make([]int64, len(runs))
	for i, r := range runs {
		watches[i], watches34[i] = r.Watches, r.Watches34
	}
	slices.Sort(watches)
	slices.Sort(watches34)
	wm, wm34 := watches[len(watches)/2], watches34[len(watches34)/2]
	t.Logf("3.2 watches' retained state: median %d B (%.2f MiB), min %d, max %d (budget median ≤ %d B)",
		wm, float64(wm)/mib, watches[0], watches[len(watches)-1], watchesHeapBudget)
	t.Logf("3.4 watches' retained state (Services, EndpointSlices, list-only kinds): median %d B (%.2f MiB), min %d, max %d (reported)",
		wm34, float64(wm34)/mib, watches34[0], watches34[len(watches34)-1])
	if wm > watchesHeapBudget {
		t.Errorf("3.2 watches' retained state %d B, budget ≤ %d B", wm, watchesHeapBudget)
	}
	if over > 48*mib {
		t.Errorf("retained heap %.1f MiB above the baseline, budget ≤ 48 MiB", float64(over)/mib)
	}
	if d := float64(over)/mib - heap31[tier.Name]; d > 24 {
		t.Errorf("retained heap %+.1f MiB above the 3.1 baseline, budget ≤ +24 MiB", d)
	}
}

// heapChild measures one heapRun of tier and prints it.
func heapChild(t *testing.T, tier scale.Tier) {
	stats := func() runtime.MemStats {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m
	}
	var r heapRun
	r.Start = stats().HeapInuse
	var snap *cache.Snapshot
	var e *Engine
	var w *measure.World
	var m *Model
	fc := testutil.FakeClock()
	if tier.IsWorst {
		// The measurement world's cache and fake clients go before the snapshot is measured,
		// once every goroutine they started has returned.
		// No GC runs while the world builds: when the informers' collections run decides which
		// spans the snapshot shares with garbage, so HeapInuse would vary by up to 9 MB per child.
		gc := debug.SetGCPercent(-1)
		g := runtime.NumGoroutine()
		w = measure.New(t, tier, fc)
		snap = w.Live.Cache.Snapshot()
		w.Live.Stop()
		w.Live = nil
		for deadline := time.Now().Add(time.Minute); runtime.NumGoroutine() > g; time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%d goroutines after the world stopped, %d before it", runtime.NumGoroutine(), g)
			}
		}
		debug.SetGCPercent(gc)
	} else {
		snap = scale.Snapshot(tier, 1)
	}
	st := stats()
	r.Snapshot = st.HeapInuse
	snapAlloc := st.HeapAlloc
	r.SnapshotAlloc = snapAlloc
	if w != nil {
		e = measuredEngine(t, w, fc, snap)
	} else {
		e, _ = largeEngine(snap, tier)
		e.Compute()
	}
	m = e.Compute()
	before := stats()
	r.Retained, r.RetainedAlloc = before.HeapInuse, before.HeapAlloc
	if len(m.Installs) != 1 {
		t.Fatalf("installs %d", len(m.Installs))
	}
	ix := m.Installs[0].Index
	// Views shared by rows and the decode cache count where they are dropped last.
	prev := before.HeapAlloc
	drop := func(name string, f func()) {
		f()
		now := stats().HeapAlloc
		r.Parts = append(r.Parts, heapPart{name, int64(prev) - int64(now)})
		prev = now
	}
	drop("index: route rows", func() { ix.Routes = nil })
	drop("index: Gateway rows", func() { ix.Gateways = nil })
	drop("index: ListenerSet rows", func() { ix.ListenerSets = nil })
	drop("index: lookups", func() { m.Installs[0].Index = nil })
	// The engine keeps the last diagnosis, and the check Stores the last findings, so both go with them.
	drop("health: findings", func() { m.Installs[0].Health.Findings = nil })
	drop("diagnosis: groups and rank", func() { m.Installs[0].Diagnosis, e.diags = nil, nil })
	drop("process data", func() { m.Installs[0].Process = nil })
	drop("check Stores and findings", func() { e.states = nil })
	drop("controller log reader", func() {
		if w != nil {
			w.StopLogs()
		}
	})
	drop("proxy stats and collector", func() { e.cfg.Collected, w = nil, nil })
	drop("computed: checks", func() { m.Installs[0].Computed.Checks = nil })
	drop("computed: delegation", func() { m.Installs[0].Computed.Delegation = computed.DelegationResult{} })
	drop("computed: attachment", func() { m.Installs[0].Computed = nil })
	drop("persistence: windows", func() { e.persist = newPersistence() })
	drop("decode: HTTPRoute views", func() { e.decode.httpRoutes = viewCache[objects.HTTPRouteView]{} })
	drop("decode: route views", func() { e.decode.routes = viewCache[objects.RouteView]{} })
	drop("decode: route hostnames", func() { e.decode.routeHostname = viewCache[objects.RouteHostnames]{} })
	drop("decode: Gateway views", func() { e.decode.gateways = viewCache[objects.GatewayView]{} })
	drop("decode: ListenerSet views", func() { e.decode.listenerSets = viewCache[objects.ListenerSetView]{} })
	drop("decode: Backend views", func() { e.decode.backends = viewCache[objects.BackendView]{} })
	// What the engine and model still hold above the snapshot: the rest of health, overview, first-seen.
	r.Parts = append(r.Parts, heapPart{"rest of engine and model", int64(prev) - int64(snapAlloc)})
	r.Watches, r.Watches34 = watchesHeap(t, tier)
	runtime.KeepAlive(e)
	runtime.KeepAlive(m)
	runtime.KeepAlive(snap)
	runtime.KeepAlive(w)
	j, _ := json.Marshal(r)
	fmt.Println("heap: " + string(j))
}

// watchesHeapBudget is the 3.2 watches' median retained state per world: the
// measurement world's 1,450,216 B plus about 10 % (D-046).
const watchesHeapBudget = 1_600_000

// watchesHeap builds tier's measurement world and returns the HeapAlloc freed by stopping its
// proxy Pod, Event and CRD watches alone, then, once the cache has stopped, by dropping its Service,
// EndpointSlice and list-only kind informers. The fake clients' objects do not count.
func watchesHeap(t *testing.T, tier scale.Tier) (w32, w34 int64) {
	alloc := func() int64 {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return int64(m.HeapAlloc) //nolint:gosec // a heap size fits
	}
	w := measure.New(t, tier, testutil.FakeClock())
	defer w.Live.Stop()
	// A stopped watch's filter goroutine returns once its source closes; wait until the count holds.
	settle := func() {
		for deadline, n, same := time.Now().Add(time.Minute), runtime.NumGoroutine(), 0; same < 3; time.Sleep(20 * time.Millisecond) {
			if c := runtime.NumGoroutine(); c == n {
				same++
			} else {
				n, same = c, 0
			}
			if time.Now().After(deadline) {
				t.Fatal("goroutines did not settle after the watches stopped")
			}
		}
	}
	before := alloc()
	cache.StopOptional(w.Live.Cache)
	settle()
	mid := alloc()
	w.Live.Stop()
	w.Live.Cache.Wait()
	settle()
	stopped := alloc()
	var listed []schema.GroupVersionResource
	for _, s := range testutil.ServedUpstream() {
		if s.ListOnly {
			listed = append(listed, s.GVR())
		}
	}
	cache.DropListed(w.Live.Cache, listed...)
	w34 = stopped - alloc()
	runtime.KeepAlive(w)
	return before - mid, w34
}

// The 2.4 dense-memory budget (D-025): the edge cap keeps the densest
// delegation inside the chart's 512Mi memory limit.

// denseMemChildEnv makes TestBudgetDenseMemory measure one edge cap in this process.
const denseMemChildEnv = "SNELLIUS_DENSE_MEM_CHILD"

// The dense budgets at the default edge cap (D-039): the 2.4 measurement
// (160 MiB retained, 210 MiB cold, 224 on linux/amd64, 168 MiB per warm Compute)
// plus about 10 % of the larger value.
const (
	denseRetainedBudget  = 176 << 20
	denseAllocBudget     = 185 << 20
	denseColdAllocBudget = 246 << 20
)

// denseMemRun is one child's measurement: the bytes allocated by the cold
// Compute and per warm Compute, the heap retained after them, the peak heap sampled during the cold
// Compute and during the warm ones, and the view B rows.
type denseMemRun struct {
	Edges, Rows                  int
	ColdAlloc                    uint64
	WarmAllocPerCompute          uint64
	RetainedHeap                 uint64
	ColdHeapInuse, WarmHeapInuse uint64
	ColdHeapAlloc, WarmHeapAlloc uint64
}

// TestBudgetDenseMemory: DenseSelection(100, 1000) with realistic status, in a
// fresh process with no GOMEMLIMIT, at the default edge cap: the heap retained
// after warm Computes and the bytes allocated by the cold and per warm Compute stay in budget,
// as the median of five children (D-039). Peak heap and RSS depend on GC timing,
// so they are logged, not gated. Twice the default and 50,000 run once and are
// reported only (D-025).
func TestBudgetDenseMemory(t *testing.T) {
	if v := os.Getenv(denseMemChildEnv); v != "" {
		edges, err := strconv.Atoi(v)
		if err != nil {
			t.Fatal(err)
		}
		denseMemChild(t, edges)
		return
	}
	const mib = 1 << 20
	def := computed.DefaultBounds.Edges
	const gateRuns = 5
	for _, edges := range slices.Compact([]int{def, 2 * def, 50_000}) {
		runs := 1
		if edges == def {
			runs = gateRuns
		}
		var retained, cold, perCompute, peaks []uint64
		for range runs {
			r, rss := denseMemParent(t, edges)
			retained = append(retained, r.RetainedHeap)
			cold = append(cold, r.ColdAlloc)
			perCompute = append(perCompute, r.WarmAllocPerCompute)
			peaks = append(peaks, max(r.ColdHeapInuse, r.WarmHeapInuse))
			t.Logf("edges %6d: %6d rows; retained heap %4d MiB; allocated cold %4d MiB, per warm Compute %4d MiB; peak RSS %4d MiB; peak HeapInuse cold %4d MiB warm %4d MiB; peak HeapAlloc cold %4d MiB warm %4d MiB",
				edges, r.Rows, r.RetainedHeap/mib, r.ColdAlloc/mib, r.WarmAllocPerCompute/mib, rss/mib, r.ColdHeapInuse/mib, r.WarmHeapInuse/mib, r.ColdHeapAlloc/mib, r.WarmHeapAlloc/mib)
		}
		if edges != def {
			continue
		}
		median := func(v []uint64) uint64 { slices.Sort(v); return v[len(v)/2] }
		ret, cld, per := median(retained), median(cold), median(perCompute)
		t.Logf("edges %6d: medians over %d runs: retained heap %4d MiB, allocated cold %4d MiB, per warm Compute %4d MiB, peak HeapInuse %4d MiB (logged only)",
			edges, runs, ret/mib, cld/mib, per/mib, median(peaks)/mib)
		if ret > denseRetainedBudget {
			t.Errorf("median retained heap %d MiB at the default edge cap %d, budget ≤ %d MiB", ret/mib, def, denseRetainedBudget/mib)
		}
		if cld > denseColdAllocBudget {
			t.Errorf("median allocated by the cold Compute %d MiB at the default edge cap %d, budget ≤ %d MiB", cld/mib, def, denseColdAllocBudget/mib)
		}
		if per > denseAllocBudget {
			t.Errorf("median allocated per warm Compute %d MiB at the default edge cap %d, budget ≤ %d MiB", per/mib, def, denseAllocBudget/mib)
		}
	}
}

// denseMemParent runs one child at edges and returns its run and peak RSS in bytes.
func denseMemParent(t *testing.T, edges int) (denseMemRun, uint64) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestBudgetDenseMemory$", "-test.count=1")
	// The budget holds without a memory limit or a GC tuning.
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "GOMEMLIMIT=") || strings.HasPrefix(kv, "GOGC=")
	})
	cmd.Env = append(cmd.Env, denseMemChildEnv+"="+strconv.Itoa(edges))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("dense memory child: %v\n%s", err, out)
	}
	var r denseMemRun
	for line := range strings.Lines(string(out)) {
		if j, ok := strings.CutPrefix(line, "densemem: "); ok {
			if err := json.Unmarshal([]byte(j), &r); err != nil {
				t.Fatal(err)
			}
		}
	}
	if r.Rows == 0 {
		t.Fatalf("dense memory child printed no result:\n%s", out)
	}
	return r, maxRSS(t, cmd.ProcessState)
}

// maxRSS is a finished process's peak resident set in bytes.
func maxRSS(t *testing.T, ps *os.ProcessState) uint64 {
	ru, ok := ps.SysUsage().(*syscall.Rusage)
	if !ok {
		t.Skipf("no rusage on %s", runtime.GOOS)
	}
	if runtime.GOOS == "darwin" {
		return uint64(ru.Maxrss) //nolint:gosec // never negative
	}
	return uint64(ru.Maxrss) * 1024 //nolint:gosec // Linux reports KiB
}

// denseMemChild computes the dense world once cold and five times warm at
// edges, sampling the heap every 5 ms, and prints one denseMemRun.
func denseMemChild(t *testing.T, edges int) {
	snap := denseSnapshot(100, 1000)
	cfg := config(snap, metricsAt(testutil.Now), func() time.Time { return testutil.Now })
	cfg.Logger = slog.New(slog.DiscardHandler)
	cfg.DelegationBounds.Edges = edges
	e := New(cfg)
	// sample runs f while sampling the heap and returns the peak HeapInuse and HeapAlloc.
	sample := func(f func()) (inuse, alloc uint64) {
		stop, done := make(chan struct{}), make(chan struct{})
		var ms runtime.MemStats
		read := func() {
			runtime.ReadMemStats(&ms)
			inuse, alloc = max(inuse, ms.HeapInuse), max(alloc, ms.HeapAlloc)
		}
		go func() {
			defer close(done)
			tk := time.NewTicker(5 * time.Millisecond)
			defer tk.Stop()
			for {
				select {
				case <-stop:
					return
				case <-tk.C:
					read()
				}
			}
		}()
		f()
		close(stop)
		<-done
		read()
		return inuse, alloc
	}
	var m *Model
	r := denseMemRun{Edges: edges}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	a0 := ms.TotalAlloc
	r.ColdHeapInuse, r.ColdHeapAlloc = sample(func() { m = e.Compute() })
	runtime.ReadMemStats(&ms)
	r.ColdAlloc = ms.TotalAlloc - a0
	const warm = 5
	a0 = ms.TotalAlloc
	r.WarmHeapInuse, r.WarmHeapAlloc = sample(func() {
		for range warm {
			e.forgetViews()
			m = e.Compute()
		}
	})
	runtime.ReadMemStats(&ms)
	r.WarmAllocPerCompute = (ms.TotalAlloc - a0) / warm
	// What the engine and the last model keep once the garbage is collected.
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&ms)
	r.RetainedHeap = ms.HeapAlloc
	runtime.KeepAlive(e)
	if len(m.Installs) != 1 || m.Installs[0].Computed == nil {
		t.Fatalf("installs %d", len(m.Installs))
	}
	r.Rows = len(m.Installs[0].Computed.Delegation.Rows)
	runtime.KeepAlive(snap)
	j, _ := json.Marshal(r)
	fmt.Println("densemem: " + string(j))
}
