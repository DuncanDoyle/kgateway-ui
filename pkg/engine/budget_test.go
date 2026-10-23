//go:build bench

package engine

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
)

// The 2.1 budgets of design §9.6 at Large, over a real cache on fake clients.

// world is a settled cache over tier's world with its change signal drained.
type world struct {
	c      *cache.Cache
	dyn    *dynamicfake.FakeDynamicClient
	sig    *trigger.Signal
	routes []*unstructured.Unstructured // the seeded HTTPRoutes
	gvr    schema.GroupVersionResource // HTTPRoutes
}

// newWorld seeds fake clients with tier's world, one kgateway controller and a
// proxy per Gateway (as scale.Snapshot), starts a cache stamped by now and
// waits until it has settled.
func newWorld(tb testing.TB, tier scale.Tier, now func() time.Time) *world {
	tb.Helper()
	served := append(testutil.ServedUpstream(), kinds.Served{Descriptor: scale.BackendKind, Version: scale.BackendKind.Versions[0]})
	listKinds := map[schema.GroupVersionResource]string{}
	byKind := map[schema.GroupKind]schema.GroupVersionResource{}
	for _, s := range served {
		listKinds[s.GVR()] = s.Kind + "List"
		byKind[schema.GroupKind{Group: s.Group, Kind: s.Kind}] = s.GVR()
	}
	w := &world{dyn: dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds), sig: trigger.New()}
	w.gvr = byKind[schema.GroupKind{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute"}]
	for i, o := range scale.World(tier, 1) {
		gvr, ok := byKind[o.GroupVersionKind().GroupKind()]
		if !ok {
			tb.Fatalf("unserved kind %s", o.GetKind())
		}
		o.SetResourceVersion("1")
		o.SetUID(types.UID(strconv.Itoa(i)))
		if err := w.dyn.Tracker().Create(gvr, o, o.GetNamespace()); err != nil {
			tb.Fatal(err)
		}
		if gvr == w.gvr {
			w.routes = append(w.routes, o)
		}
	}
	typed := []runtime.Object{testutil.Namespace("kgw", nil)}
	for _, ns := range testutil.ScaleNamespaces(tier.Namespaces) {
		typed = append(typed, ns)
	}
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	typed = append(typed, d, testutil.ControllerPod(d, "kgateway-0", true))
	for i := range tier.Gateways {
		typed = append(typed, testutil.ProxyDeployment(testutil.ScaleNamespace(i%tier.Namespaces), "gw-"+pad3(i), 1, 1))
	}
	det := install.UpstreamDetector{}
	w.c = cache.New(cache.Config{Dynamic: w.dyn, Kube: kubefake.NewSimpleClientset(typed...), Served: served, Now: now, Changes: w.sig,
		PodSelectors: []labels.Selector{det.ControllerPodSelector()}, DeploymentSelectors: []labels.Selector{det.ControllerSelector(), det.ProxySelector()}})
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)
	w.c.Start(ctx)
	for deadline := time.Now().Add(time.Minute); !w.c.Settled(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			tb.Fatal("cache did not settle")
		}
	}
	testutil.Drain(w.sig.C()) // the settle notify precedes Settled
	return w
}

// pad3 matches scale's Gateway names, gw-000 ….
func pad3(i int) string {
	s := strconv.Itoa(i)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
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
		if _, err := w.dyn.Resource(w.gvr).Namespace(u.GetNamespace()).Update(ctx, u, metav1.UpdateOptions{}); err != nil {
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
// (production defaults: safety 30 s, metrics max age 3 × 10 s) recompute only
// at the safety instants and Model.Next deadlines, never show metrics stale,
// and confirm every second.
func TestBudgetIdleTenMinutes(t *testing.T) {
	const safety, every = 30 * time.Second, 10 * time.Second
	clk := testutil.FakeClock()
	w := newWorld(t, scale.Large, clk.Now)
	col := ctrlmetrics.NewCollector(newBodyScraper(managedBody))
	col.OnResult = w.sig
	e, ev := w.engine(t, clk, func(c *Config) { c.Metrics, c.MetricsMaxAge = col, 3*every })
	runLoop(t, e, safety)
	awaitKind(t, ev, EvPublished) // the start-up publication, before any scrape
	awaitKind(t, ev, EvArmed)
	cc := scrapeEvery(t, col, every, e.Installs, "kgw.kgateway")
	awaitKind(t, ev, EvTriggered) // the first result; this run is not counted either
	awaitKind(t, ev, EvArmed)
	cc.Step(time.Second)
	clk.Step(time.Second)
	prev := awaitKind(t, ev, EvPublished)
	awaitKind(t, ev, EvArmed)
	n := driveIdle(t, ev, clk, cc, col, "kgw.kgateway", prev, safety, every, 600)
	t.Logf("idle 10 min: %d recomputes (%d at the metrics staleness deadline, %d on the safety timer alone), %d confirmations, %d scrapes",
		n.runs, n.onDeadline, n.onSafety, n.confirms, n.scrapes)
	if n.runs != 20 || n.onDeadline != 20 || n.confirms != 600 || n.scrapes != 60 {
		t.Errorf("idle 10 min: %+v; want 20 recomputes, all at the deadline, 600 confirmations, 60 scrapes", n)
	}
}

// tick is the confirm tick's work on an idle loop: the live source,
// fingerprint, metrics identity and kinds, compared and swapped in.
func tick(tb testing.TB, e *Engine) {
	p := e.Current()
	if !e.liveSource(p).Equal(p.Source) || !e.confirm(p, p.ConfirmedAt) {
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
