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
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
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
	l := scale.Live(tb, tier, now)
	return &world{c: l.Cache, update: l.Update, sig: l.Signal, routes: l.Routes, gvr: l.RouteGVR}
}

// engine returns an engine over w's cache with static metrics that never go
// stale, driven by clk (nil: the real clock), reporting loop events on a
// buffered channel; mut, if set, adjusts the config.
func (w *world) engine(tb testing.TB, clk *clocktesting.FakeClock, mut ...func(*Config)) (*Engine, chan LoopEvent) {
	ev := make(chan LoopEvent, 1<<16)
	cfg := config(nil, metricsAt(testutil.Now), nil)
	// Static metrics never go stale here, as a scraped cluster's do not.
	cfg.Snapshots, cfg.Triggers, cfg.MetricsMaxAge = w.c, w.sig, 0
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
		t.Errorf("event-to-publication p95 %v, budget ≤ 1.2 s", p95)
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
	s.col.OnResult = w.sig
	e, ev := w.engine(t, clk, func(c *Config) { c.Metrics, c.MetricsMaxAge = s.col, maxAge })
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
// fingerprint, metrics identity and kinds, compared and swapped in.
func tick(tb testing.TB, e *Engine) {
	p := e.Current()
	if !e.liveSource(p, time.Now()).Equal(p.Source) || !e.confirm(p, p.ConfirmedAt) {
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
			t.Errorf("confirm tick median at Large %v, budget ≤ 5 ms", d[len(d)/2])
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

// matureAt is a time past every window opened at testutil.Now, and past a
// missing child entry's grace end.
var matureAt = testutil.Now.Add(max(Window, health.Grace) + time.Second)

// largeEngine is an engine over tier's snapshot on a settable clock, with
// metrics that never go stale. Under Contradict it computes once at
// testutil.Now and moves the clock to matureAt, so the next Compute matures
// every check.
func largeEngine(snap *cache.Snapshot, tier scale.Tier) (*Engine, *time.Time) {
	now := new(time.Time)
	*now = testutil.Now
	cfg := config(snap, metricsAt(testutil.Now), func() time.Time { return *now })
	cfg.MetricsMaxAge = 0
	e := New(cfg)
	if tier.Contradict {
		e.Compute()
		*now = matureAt
	}
	return e, now
}

// TestBudgetWarmRecompute: at Large, the median of 20 warm Computes ≤ 150 ms
// with views B and C (design §9.6, 2.4)
// and the median of 5 cold ones ≤ 1 s, for the consistent world and the worst
// case. Allocation per warm Compute and the persistence step are reported (D-022).
func TestBudgetWarmRecompute(t *testing.T) {
	const mib = 1 << 20
	for _, tier := range largeCases {
		t.Run(tier.Name, func(t *testing.T) {
			snap := scale.Snapshot(tier, 1)
			cold := make([]time.Duration, 5)
			for i := range cold {
				// The first Compute of a fresh engine, before any window matures.
				e := New(config(snap, metricsAt(testutil.Now), func() time.Time { return testutil.Now }))
				t0 := time.Now()
				e.Compute()
				cold[i] = time.Since(t0)
			}
			e, now := largeEngine(snap, tier)
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
				t.Errorf("cold recompute median %v, budget ≤ 1 s", cold[len(cold)/2])
			}
			if warm[len(warm)/2] > 150*time.Millisecond {
				t.Errorf("warm recompute median %v, budget ≤ 150 ms", warm[len(warm)/2])
			}
		})
	}
}

// heapBaseline is the median "retained" HeapInuse of the 2.1 engine at Large
// (docs/phases/phase-2/bench/2.2-baseline-heap.txt).
const heapBaseline = 25411584

// heapChildEnv makes TestBudgetHeap measure in this process and print one line.
const heapChildEnv = "KGWUI_HEAP_CHILD"

// heapRun is one child's measurement: HeapInuse at the baseline's three
// points, and the HeapAlloc freed by dropping each part in order.
type heapRun struct {
	Start, Snapshot, Retained uint64
	Parts                     []heapPart
}

type heapPart struct {
	Name  string
	Freed int64
}

// TestBudgetHeap: measured as the 2.1 baseline was, one fresh process per run
// and five runs, the median retained heap at Large ≤ 32 MiB above it, for the
// consistent world and the worst case. Each part is then dropped in turn; its
// freed HeapAlloc is reported per kind.
func TestBudgetHeap(t *testing.T) {
	if name := os.Getenv(heapChildEnv); name != "" {
		i := slices.IndexFunc(largeCases, func(x scale.Tier) bool { return x.Name == name })
		heapChild(t, largeCases[i])
		return
	}
	for _, tier := range largeCases {
		t.Run(tier.Name, func(t *testing.T) { heapBudget(t, tier) })
	}
}

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
	t.Logf("median retained %d B (%.1f MiB); %+.1f MiB over the 2.1 baseline %d B (budget ≤ 32 MiB)", med.Retained, float64(med.Retained)/mib, float64(over)/mib, heapBaseline)
	for _, p := range med.Parts {
		t.Logf("  %-28s %7.2f MiB", p.Name, float64(p.Freed)/mib)
	}
	if over > 32*mib {
		t.Errorf("retained heap %.1f MiB above the baseline, budget ≤ 32 MiB", float64(over)/mib)
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
	snap := scale.Snapshot(tier, 1)
	st := stats()
	r.Snapshot = st.HeapInuse
	snapAlloc := st.HeapAlloc
	e, _ := largeEngine(snap, tier)
	e.Compute()
	m := e.Compute()
	before := stats()
	r.Retained = before.HeapInuse
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
	drop("health: findings", func() { m.Installs[0].Health.Findings = nil })
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
	runtime.KeepAlive(e)
	runtime.KeepAlive(m)
	runtime.KeepAlive(snap)
	j, _ := json.Marshal(r)
	fmt.Println("heap: " + string(j))
}

// The 2.4 dense-memory budget (D-025): the edge cap keeps the densest
// delegation inside the chart's 512Mi memory limit.

// denseMemChildEnv makes TestBudgetDenseMemory measure one edge cap in this process.
const denseMemChildEnv = "KGWUI_DENSE_MEM_CHILD"

// denseMemRun is one child's measurement: the peak heap sampled during the
// cold Compute and during the warm ones, and the view B rows.
type denseMemRun struct {
	Edges, Rows                  int
	ColdHeapInuse, WarmHeapInuse uint64
	ColdHeapAlloc, WarmHeapAlloc uint64
}

// TestBudgetDenseMemory: DenseSelection(100, 1000) with realistic status, in a
// fresh process with no GOMEMLIMIT, at the default edge cap: peak heap
// (HeapInuse) ≤ 432 MiB. RSS is logged, not gated: its margin is too thin for
// CI. Twice the default and 50,000 are measured and reported (D-025).
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
	for _, edges := range slices.Compact([]int{def, 2 * def, 50_000}) {
		r, rss := denseMemParent(t, edges)
		heap := max(r.ColdHeapInuse, r.WarmHeapInuse)
		t.Logf("edges %6d: %6d rows; peak RSS %4d MiB; peak HeapInuse cold %4d MiB warm %4d MiB; peak HeapAlloc cold %4d MiB warm %4d MiB",
			edges, r.Rows, rss/mib, r.ColdHeapInuse/mib, r.WarmHeapInuse/mib, r.ColdHeapAlloc/mib, r.WarmHeapAlloc/mib)
		if edges != def {
			continue
		}
		if heap > 432*mib {
			t.Errorf("peak heap %d MiB at the default edge cap %d, budget ≤ 432 MiB", heap/mib, def)
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
	r.ColdHeapInuse, r.ColdHeapAlloc = sample(func() { m = e.Compute() })
	r.WarmHeapInuse, r.WarmHeapAlloc = sample(func() {
		for range 5 {
			e.forgetViews()
			m = e.Compute()
		}
	})
	if len(m.Installs) != 1 || m.Installs[0].Computed == nil {
		t.Fatalf("installs %d", len(m.Installs))
	}
	r.Rows = len(m.Installs[0].Computed.Delegation.Rows)
	runtime.KeepAlive(snap)
	j, _ := json.Marshal(r)
	fmt.Println("densemem: " + string(j))
}
