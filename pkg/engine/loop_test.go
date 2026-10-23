package engine

import (
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
)

// fakeSource is a snapshotter and fingerprinter whose store can change
// without notifying, and whose Snapshot can block after copying.
type fakeSource struct {
	mu    sync.Mutex
	base  *cache.Snapshot
	print cache.Fingerprint
	now   func() time.Time
	// gate, if set, runs after the copy, before Snapshot returns.
	gate func()
}

func (f *fakeSource) Snapshot() *cache.Snapshot {
	f.mu.Lock()
	s := *f.base
	s.TakenAt, s.Source = f.now(), f.print
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		gate()
	}
	return &s
}

func (f *fakeSource) Fingerprint() cache.Fingerprint {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.print
}

// setBase replaces the stored objects and fingerprint together.
func (f *fakeSource) setBase(b *cache.Snapshot, p cache.Fingerprint) {
	f.mu.Lock()
	f.base, f.print = b, p
	f.mu.Unlock()
}

func (f *fakeSource) setPrint(p cache.Fingerprint) {
	f.mu.Lock()
	f.print = p
	f.mu.Unlock()
}

// setGate sets the gate for the following snapshots; nil removes it.
func (f *fakeSource) setGate(g func()) {
	f.mu.Lock()
	f.gate = g
	f.mu.Unlock()
}

// blockOnce makes the next Snapshot signal taken after its copy and wait for
// release; later snapshots pass.
func (f *fakeSource) blockOnce() (taken, release chan struct{}) {
	taken, release = make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	f.setGate(func() {
		once.Do(func() {
			taken <- struct{}{}
			<-release
		})
	})
	return taken, release
}

// fprint returns a fingerprint of one kind with n objects and the given access.
func fprint(n int, a cache.Access) cache.Fingerprint {
	return cache.Fingerprint{Kinds: map[string]cache.KindPrint{"routes": {Count: n, Hash: uint64(n) * 7919}}, Access: map[string]cache.Access{"routes": a}}
}

var accessOK = cache.Access{OK: true}

// metricsFunc adapts a function to ctrlmetrics.Source.
type metricsFunc func(id string) (ctrlmetrics.Result, bool)

func (f metricsFunc) Get(id string) (ctrlmetrics.Result, bool) { return f(id) }

// harness runs an engine loop over a fakeSource on a fake clock.
type harness struct {
	t      *testing.T
	e      *Engine
	c      *clocktesting.FakeClock
	src    *fakeSource
	sig    *trigger.Signal
	ev     chan LoopEvent
	cancel context.CancelFunc
	// stopped is closed when Run returns.
	stopped chan struct{}
	// runs counts EvPublished after the start-up publication; confirms counts EvConfirmed.
	runs, confirms int
}

// newHarness builds the engine; mut adjusts the config before New.
func newHarness(t *testing.T, mut func(*Config)) *harness {
	t.Helper()
	c := testutil.FakeClock()
	src := &fakeSource{base: snapshot(t), print: fprint(1, accessOK), now: c.Now}
	h := &harness{t: t, c: c, src: src, sig: trigger.New(), ev: make(chan LoopEvent, 4096)}
	cfg := config(nil, metricsAt(testutil.Now), c.Now)
	cfg.Snapshots, cfg.MetricsMaxAge, cfg.Triggers, cfg.Clock = src, 0, h.sig, c
	cfg.OnLoop = func(e LoopEvent) {
		select {
		case h.ev <- e:
		default: // Fatal is for the test goroutine; the next await then times out
			t.Errorf("loop event buffer full (%d): a test steps without awaiting events", cap(h.ev))
		}
	}
	if mut != nil {
		mut(&cfg)
	}
	h.e = New(cfg)
	return h
}

// start runs the loop and waits for the start-up publication, which runs do not count.
func (h *harness) start(safety time.Duration) {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.t.Cleanup(cancel)
	h.stopped = make(chan struct{})
	go func() {
		defer close(h.stopped)
		h.e.Run(ctx, safety)
	}()
	h.await(EvPublished)
	h.await(EvArmed)
	h.runs = 0
}

// await receives events up to and including one of kind want.
func (h *harness) await(want LoopEventKind) LoopEvent {
	h.t.Helper()
	for {
		e := testutil.Recv(h.t, h.ev, "loop event")
		switch e.Kind {
		case EvPublished:
			h.runs++
		case EvConfirmed:
			h.confirms++
		}
		if e.Kind == want {
			return e
		}
	}
}

// notify sends a trigger and waits until the loop has recorded it.
func (h *harness) notify() {
	h.t.Helper()
	h.sig.Notify()
	h.await(EvTriggered)
}

// awaitThenStep receives up to want, then steps c by d.
func awaitThenStep(t *testing.T, ev <-chan LoopEvent, want LoopEventKind, c *clocktesting.FakeClock, d time.Duration) {
	t.Helper()
	for {
		if testutil.Recv(t, ev, "loop event").Kind == want {
			c.Step(d)
			return
		}
	}
}

// tickIdle steps one confirm interval on an idle loop and waits for its confirmation.
func (h *harness) tickIdle() LoopEvent {
	h.t.Helper()
	h.c.Step(ConfirmEvery)
	e := h.await(EvConfirmed)
	h.await(EvArmed)
	return e
}

func TestOneRecomputePerBurst(t *testing.T) {
	h := newHarness(t, nil)
	h.start(time.Hour)
	var last time.Time
	for range 500 { // 500 triggers in 100 ms
		h.notify()
		h.await(EvArmed)
		last = h.c.Now()
		h.c.Step(200 * time.Microsecond)
	}
	h.c.Step(last.Add(Quiet).Sub(h.c.Now()))
	if e := h.await(EvPublished); !e.At.Equal(last.Add(Quiet)) {
		t.Fatalf("published at %v, want last trigger + 250 ms = %v", e.At, last.Add(Quiet))
	}
	h.await(EvArmed)
	for range 5 {
		h.tickIdle()
	}
	if h.runs != 1 {
		t.Fatalf("runs = %d, want 1", h.runs)
	}
}

func TestStreamRecomputesOncePerSecond(t *testing.T) {
	h := newHarness(t, nil)
	h.start(time.Hour)
	t0 := h.c.Now()
	for i := 1; i <= 50; i++ { // a trigger every 100 ms for 5 s
		h.notify()
		h.await(EvArmed)
		h.c.Step(100 * time.Millisecond)
		if el := h.c.Now().Sub(t0); el%time.Second == 0 {
			if e := h.await(EvPublished); !e.At.Equal(h.c.Now()) {
				t.Fatalf("run at %v, want %v", e.At, h.c.Now())
			}
		}
	}
	if h.runs != 5 {
		t.Fatalf("runs = %d, want 5 (one per second)", h.runs)
	}
}

func TestTriggerDuringRunGivesOneMoreRun(t *testing.T) {
	h := newHarness(t, nil)
	h.start(time.Hour)
	taken, release := h.src.blockOnce()
	h.notify()
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	h.await(EvRunStarted)
	testutil.Recv(t, taken, "snapshot")
	for range 3 {
		h.notify()
	}
	close(release)
	h.await(EvPublished)
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	h.await(EvPublished)
	h.await(EvArmed)
	for range 3 {
		h.tickIdle()
	}
	if h.runs != 2 {
		t.Fatalf("runs = %d, want 2", h.runs)
	}
}

func TestSafetyTimerOnly(t *testing.T) {
	h := newHarness(t, nil)
	h.start(30 * time.Second)
	for range 29 {
		h.tickIdle()
	}
	if h.runs != 0 {
		t.Fatalf("runs at 29 s = %d, want 0", h.runs)
	}
	h.c.Step(ConfirmEvery)
	h.await(EvConfirmed) // PF14: the tick goes before the timer that fires with it
	if e := h.await(EvPublished); !e.At.Equal(testutil.Now.Add(30 * time.Second)) {
		t.Fatalf("safety run at %v", e.At)
	}

	h = newHarness(t, nil)
	h.start(2 * time.Second)
	for range 10 {
		h.tickIdle()
		h.c.Step(ConfirmEvery)
		h.await(EvPublished)
		h.await(EvArmed)
	}
	if h.runs != 10 {
		t.Fatalf("safety 2 s over 20 s: runs = %d, want 10", h.runs)
	}
}

func TestChangeDuringRunPublishesOldThenNew(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.ConfirmEvery = 100 * time.Millisecond })
	h.start(time.Hour)
	f1, f2 := fprint(2, accessOK), fprint(3, accessOK)
	h.src.setPrint(f1)
	taken, release := h.src.blockOnce()
	h.notify()
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	h.await(EvRunStarted)
	testutil.Recv(t, taken, "snapshot")
	h.src.setPrint(f2)
	h.notify()
	close(release)
	first := h.await(EvPublished).Published
	if !first.Source.Cache.Equal(f1) {
		t.Fatalf("first publication must carry the snapshot's F1: %+v", first.Source.Cache)
	}
	h.await(EvArmed)
	h.c.Step(50 * time.Millisecond) // a confirm tick before the second run is due
	h.await(EvConfirmRefused)
	if p := h.e.Current(); p != first || !p.ConfirmedAt.Equal(first.ConfirmedAt) {
		t.Fatal("a pending change must not confirm the old model")
	}
	h.await(EvArmed)
	h.c.Step(200 * time.Millisecond)
	second := h.await(EvPublished).Published
	if !second.Source.Cache.Equal(f2) {
		t.Fatalf("second publication must carry F2: %+v", second.Source.Cache)
	}
}

// Review Focus 1: the store moved but its handler has not run yet.
func TestConfirmTickCatchesChangeBeforeHandler(t *testing.T) {
	h := newHarness(t, nil)
	h.start(time.Hour)
	p1 := h.e.Current()
	f2 := fprint(2, accessOK)
	h.src.setPrint(f2) // no Notify: the handler is paused
	h.c.Step(ConfirmEvery)
	refusedAt := h.await(EvConfirmRefused).At
	if p := h.e.Current(); p != p1 || !p.ConfirmedAt.Equal(p1.ConfirmedAt) {
		t.Fatal("a mismatch must not confirm")
	}
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	e := h.await(EvPublished)
	if !e.Published.Source.Cache.Equal(f2) || !e.At.Equal(refusedAt.Add(Quiet)) {
		t.Fatalf("want F2 published %v after the refusal, got at %v", Quiet, e.At)
	}
	h.await(EvArmed)
	h.notify() // the handler resumes
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	if again := h.await(EvPublished).Published; !again.Source.Equal(e.Published.Source) || h.runs != 2 {
		t.Fatalf("the late trigger gives one more run with an equal source; runs = %d", h.runs)
	}
}

// Only access changes in the fingerprint: no confirmation, one run.
func TestAccessTransitionBlocksConfirmation(t *testing.T) {
	h := newHarness(t, nil)
	h.start(time.Hour)
	p1 := h.e.Current()
	h.src.setPrint(fprint(1, cache.Access{Err: "forbidden"}))
	h.c.Step(ConfirmEvery)
	h.await(EvConfirmRefused)
	if h.e.Current() != p1 {
		t.Fatal("an access transition must not confirm")
	}
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	if got := h.await(EvPublished).Published.Source.Cache.Access["routes"]; got.Err != "forbidden" {
		t.Fatalf("access = %+v", got)
	}
	h.await(EvArmed)
	h.tickIdle()
	if h.runs != 1 {
		t.Fatalf("runs = %d, want 1", h.runs)
	}
}

// Task 3: an empty kind whose first List failed can turn OK with no event,
// before the store is marked synced. Only the confirm tick catches it.
func TestConfirmTickCatchesSilentEmptyKindRecovery(t *testing.T) {
	h := newHarness(t, nil)
	h.src.setPrint(fprint(0, cache.Access{}))
	h.start(time.Hour)
	h.src.setPrint(fprint(0, accessOK))
	h.c.Step(ConfirmEvery)
	h.await(EvConfirmRefused)
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	if got := h.await(EvPublished).Published.Source.Cache.Access["routes"]; !got.OK {
		t.Fatalf("the recovered kind must be published OK, got %+v", got)
	}
}

func TestBlockedRunAgesConfirmedAt(t *testing.T) {
	h := newHarness(t, nil)
	h.start(time.Hour)
	p1 := h.e.Current()
	_, release := h.src.blockOnce()
	t.Cleanup(func() { close(release) })
	h.notify()
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	h.await(EvRunStarted)
	h.c.Step(10 * time.Second)
	h.await(EvConfirmRefused)
	if p := h.e.Current(); !p.ConfirmedAt.Equal(p1.ConfirmedAt) || !p.Model.ComputedAt.Equal(p1.Model.ComputedAt) {
		t.Fatalf("a hung run must not renew ConfirmedAt: %v", p.ConfirmedAt)
	}
}

func TestConcurrentReadsSeeOnePublished(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.ConfirmEvery = 100 * time.Millisecond })
	h.start(time.Hour)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var reads atomic.Int64
	for range 8 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				p := h.e.Current()
				if !p.Source.Cache.Equal(p.Model.Snapshot.Source) || p.ConfirmedAt.Before(p.Model.Snapshot.TakenAt) {
					t.Errorf("torn publication: %+v", p)
					return
				}
				reads.Add(1)
			}
		})
	}
	for i := range 20 {
		h.src.setPrint(fprint(i+2, accessOK))
		h.notify()
		awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
		h.await(EvPublished)
		h.await(EvArmed)
		h.c.Step(100 * time.Millisecond)
		h.await(EvConfirmed)
		h.await(EvArmed)
	}
	close(stop)
	wg.Wait()
	if reads.Load() == 0 {
		t.Fatal("no reads")
	}
}

func TestConfirmAdvancesOnlyConfirmedAt(t *testing.T) {
	h := newHarness(t, nil)
	h.start(time.Hour)
	p0 := h.e.Current()
	if !p0.ConfirmedAt.Equal(p0.Model.Snapshot.TakenAt) || p0.ConfirmedAt.After(p0.Model.ComputedAt) {
		t.Fatalf("at publication ConfirmedAt is the snapshot time: %v, computed %v", p0.ConfirmedAt, p0.Model.ComputedAt)
	}
	for range 3 {
		e := h.tickIdle()
		p := h.e.Current()
		if e.Published != p || !p.ConfirmedAt.Equal(h.c.Now()) || p.Model != p0.Model || !p.Model.ComputedAt.Equal(p0.Model.ComputedAt) {
			t.Fatalf("a confirmation advances only ConfirmedAt: %+v", p)
		}
		if !p.ConfirmedAt.After(p.Model.ComputedAt) {
			t.Fatal("after a tick ConfirmedAt is past ComputedAt")
		}
	}
}

// severity returns the severity of KGWUI-003 on shop/r in p, or "".
func severity(p *Published) health.Severity {
	for _, f := range p.Model.Installs[0].Health.Findings {
		if f.Code == health.CodeRouteUnattached && f.Resource != nil && f.Resource.String() == "shop/r" {
			return f.Severity
		}
	}
	return ""
}

func TestGraceDeadlineTurnsFindingToWarning(t *testing.T) {
	h := newHarness(t, nil)
	h.src.base = snapshot(t, testutil.HTTPRoute("shop", "r", []objects.ParentRef{testutil.GwRef("infra", "edge")}))
	h.start(time.Hour)
	if s := severity(h.e.Current()); s != health.SeverityInfo {
		t.Fatalf("inside grace: %q", s)
	}
	h.c.Step(health.Grace - time.Nanosecond)
	h.await(EvConfirmed)
	h.await(EvArmed)
	if h.runs != 0 {
		t.Fatal("no run before the grace expiry")
	}
	h.c.Step(time.Nanosecond)
	e := h.await(EvPublished)
	if !e.At.Equal(testutil.Now.Add(health.Grace)) || severity(e.Published) != health.SeverityWarning {
		t.Fatalf("at T+60 s: run at %v, severity %q", e.At, severity(e.Published))
	}
	h.await(EvArmed)
	h.tickIdle()
	if h.runs != 1 {
		t.Fatalf("runs = %d, want 1", h.runs)
	}
}

func stale(p *Published) bool {
	return hasFinding(p.Model.Installs[0].Health, health.CodeSignalUnavailable, "", "controller metrics are stale")
}

func TestStrictMetricsDeadline(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.MetricsMaxAge = 30 * time.Second }) // CollectedAt C = testutil.Now
	h.start(time.Hour)
	h.c.Step(30*time.Second - Quiet)
	h.await(EvConfirmed)
	h.await(EvArmed)
	h.notify()
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	at := h.await(EvPublished)
	if !at.At.Equal(testutil.Now.Add(30*time.Second)) || stale(at.Published) {
		t.Fatalf("a run at exactly C+30 s keeps metrics current: at %v stale %v", at.At, stale(at.Published))
	}
	if next := at.Published.Model.Next; !next.Strict || !next.At.Equal(testutil.Now.Add(30*time.Second)) {
		t.Fatalf("the strict deadline must survive a run at its boundary: %+v", next)
	}
	awaitThenStep(t, h.ev, EvArmed, h.c, time.Nanosecond)
	after := h.await(EvPublished)
	if !stale(after.Published) {
		t.Fatal("the run after the boundary raises KGWUI-008 stale")
	}
}

// emptyScraper serves an empty metrics body.
type emptyScraper struct{}

func (emptyScraper) Scrape(context.Context, *corev1.Pod, int32) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func TestMetricsResultTriggersOneRecompute(t *testing.T) {
	col := ctrlmetrics.NewCollector(emptyScraper{})
	h := newHarness(t, func(c *Config) { c.Metrics = col; col.OnResult = c.Triggers })
	h.start(time.Hour)
	if h.e.Current().Model.MetricsRead[0].Present {
		t.Fatal("no result before the first scrape")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = col.Run(ctx, time.Hour, h.e.Installs) }()
	h.await(EvTriggered)
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	if p := h.await(EvPublished).Published; !p.Model.MetricsRead[0].Present {
		t.Fatal("the run must use the scraped result")
	}
	h.await(EvArmed)
	h.tickIdle()
	if h.runs != 1 {
		t.Fatalf("runs = %d, want 1", h.runs)
	}
}

func TestConfirmNeverRestoresOlderModel(t *testing.T) {
	h := newHarness(t, nil)
	var p2 *Model
	h.e.beforeConfirmSwap = func() {
		if p2 == nil {
			p2 = h.e.Compute()
		}
	}
	h.start(time.Hour)
	h.c.Step(ConfirmEvery)
	h.await(EvConfirmRefused)
	if h.e.Current().Model != p2 {
		t.Fatal("a confirmation must not restore the model it read")
	}
}

func TestMetricsIdentityCoversContent(t *testing.T) {
	at := testutil.Now
	res := func(rejects float64) ctrlmetrics.Result {
		return ctrlmetrics.Result{CollectedAt: at, Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{{Namespace: "infra", Name: "edge"}: rejects},
			Managed: map[ctrlmetrics.ManagedKey]float64{{Namespace: "shop", Resource: "httproutes"}: 3, {Namespace: "infra", Resource: "gateways"}: 1}, ManagedPresent: true}}
	}
	enc := func(r ctrlmetrics.Result, present bool) uint64 {
		return encodeMetrics([]MetricsRead{{ID: "kgw.kgateway", Result: r, Present: present}})
	}
	if enc(res(0), true) == enc(res(1), true) {
		t.Fatal("same CollectedAt, different Rejects must encode differently")
	}
	if enc(ctrlmetrics.Result{}, false) == enc(ctrlmetrics.Result{}, true) {
		t.Fatal("absent and present empty results must encode differently")
	}
	a, b := res(1), res(1) // separate maps: iteration order must not matter
	if enc(a, true) != enc(b, true) {
		t.Fatal("equal results must encode equally")
	}
	errRes := res(0)
	errRes.Err = "no ready controller pod to scrape"
	if enc(errRes, true) == enc(res(0), true) {
		t.Fatal("Err must be part of the identity")
	}
}

func TestMetricsFingerprintIsEvidenceRead(t *testing.T) {
	gw := ctrlmetrics.GatewayKey{Namespace: "infra", Name: "edge"}
	mk := func(rejects float64) ctrlmetrics.Result {
		return ctrlmetrics.Result{CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{gw: rejects}, ManagedPresent: true}}
	}
	a, b := mk(1), mk(0)
	var gets atomic.Int64
	h := newHarness(t, func(c *Config) {
		c.Metrics = metricsFunc(func(string) (ctrlmetrics.Result, bool) {
			if gets.Add(1) == 1 {
				return a, true
			}
			return b, true
		})
	})
	h.start(time.Hour)
	p := h.e.Current()
	if gets.Load() != 1 {
		t.Fatalf("Compute must read metrics once per install, read %d times", gets.Load())
	}
	if !hasFinding(p.Model.Installs[0].Health, health.CodeXDSRejected, "", "") || p.Source.Metrics != encodeMetrics(p.Model.MetricsRead) ||
		p.Source.Metrics != encodeMetrics([]MetricsRead{{ID: p.Model.MetricsRead[0].ID, Result: a, Present: true}}) {
		t.Fatal("health and the source must both use A")
	}
	h.c.Step(ConfirmEvery)
	h.await(EvConfirmRefused)
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	q := h.await(EvPublished).Published
	if hasFinding(q.Model.Installs[0].Health, health.CodeXDSRejected, "", "") {
		t.Fatal("the run after the refusal must use B")
	}
	h.await(EvArmed)
	h.tickIdle()
	if h.runs != 1 {
		t.Fatalf("runs = %d, want 1", h.runs)
	}
}

func TestOnComputeOnlyOnRecompute(t *testing.T) {
	var calls atomic.Int64
	h := newHarness(t, func(c *Config) { c.OnCompute = func(*Model) { calls.Add(1) } })
	h.start(time.Hour)
	for range 3 {
		h.tickIdle()
	}
	h.notify()
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	h.await(EvPublished)
	h.await(EvArmed)
	h.tickIdle()
	if calls.Load() != int64(h.runs)+1 || h.confirms == 0 { // +1: the start-up publication
		t.Fatalf("OnCompute calls = %d, runs = %d, confirms = %d", calls.Load(), h.runs, h.confirms)
	}
}

// Installs reads the current publication and is nil before the first.
func TestInstallsReadsCurrent(t *testing.T) {
	e := New(config(snapshot(t), metricsAt(testutil.Now), func() time.Time { return testutil.Now }))
	if e.Installs() != nil {
		t.Fatal("no installs before the first publication")
	}
	e.Compute()
	if ids := e.Installs(); len(ids) != 1 || ids[0].ID() != "kgw.kgateway" {
		t.Fatalf("Installs = %+v", ids)
	}
}

// Minor 1: grace deadlines only for objects health reads while in grace.
func TestAllGreenSchedulesNoGraceRecompute(t *testing.T) {
	route := testutil.HTTPRoute("shop", "cart", []objects.ParentRef{testutil.GwRef("infra", "edge")}, testutil.AcceptedBy(testutil.GwRef("infra", "edge"), testutil.Controller))
	h := newHarness(t, nil)
	h.src.base = snapshot(t, route)
	h.start(time.Hour)
	p := h.e.Current()
	if p.Model.Installs[0].Health.State != health.StateHealthy || !p.Model.Next.IsZero() {
		t.Fatalf("all green: state %s, Next %+v; want healthy and no deadline", p.Model.Installs[0].Health.State, p.Model.Next)
	}
	for range 61 {
		h.tickIdle()
	}
	if h.runs != 0 {
		t.Fatalf("runs = %d, want 0 past the grace period", h.runs)
	}
}

// An object added later matures exactly at its own grace instant.
func TestPendingObjectMaturesAtItsGraceInstant(t *testing.T) {
	h := newHarness(t, nil)
	h.start(time.Hour)
	h.c.Step(10 * time.Second)
	h.await(EvConfirmed)
	h.await(EvArmed)
	added := h.c.Now()
	h.src.setBase(snapshot(t, testutil.HTTPRoute("shop", "r", []objects.ParentRef{testutil.GwRef("infra", "edge")})), fprint(2, accessOK))
	h.notify()
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	p := h.await(EvPublished).Published
	seen := added.Add(Quiet)
	if severity(p) != health.SeverityInfo || p.Model.Next != (Deadline{At: seen.Add(health.Grace)}) {
		t.Fatalf("pending route: severity %q, Next %+v; want info and %v", severity(p), p.Model.Next, seen.Add(health.Grace))
	}
	h.await(EvArmed)
	h.c.Step(seen.Add(health.Grace - time.Nanosecond).Sub(h.c.Now()))
	h.await(EvConfirmed)
	h.await(EvArmed)
	h.c.Step(time.Nanosecond)
	e := h.await(EvPublished)
	if !e.At.Equal(seen.Add(health.Grace)) || severity(e.Published) != health.SeverityWarning {
		t.Fatalf("maturation run at %v severity %q; want %v warning", e.At, severity(e.Published), seen.Add(health.Grace))
	}
	if h.runs != 2 {
		t.Fatalf("runs = %d, want 2", h.runs)
	}
}

// Minor 2: a run in flight when ctx is cancelled is never published.
func TestNoPublicationAfterRunReturns(t *testing.T) {
	h := newHarness(t, nil)
	h.start(time.Hour)
	p1 := h.e.Current()
	taken, release := h.src.blockOnce()
	h.notify()
	awaitThenStep(t, h.ev, EvArmed, h.c, Quiet)
	h.await(EvRunStarted)
	testutil.Recv(t, taken, "snapshot")
	h.cancel()
	testutil.Recv(t, h.stopped, "Run to return")
	close(release)
	h.e.computeMu.Lock() // waits for the in-flight compute to finish
	cur := h.e.Current()
	h.e.computeMu.Unlock()
	if cur != p1 {
		t.Fatal("a compute finishing after Run returned must not be published")
	}
}
