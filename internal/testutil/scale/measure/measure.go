// Package measure builds the measurement world of the 3.2 and 3.3 budgets
// (design §16.3): scale.LivePhase3 with its optional watches, controller
// metrics with rejects per Gateway, observed probe failures, a stub proxy
// stats source and the controller log reader over a stub stream at LogRate,
// both published through the real collector store. It never imports the engine.
package measure

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/kgw24"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrllogs"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrllogs/ctrllogstest"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

// Every is the proxy stats scrape interval, the metrics interval.
const Every = 10 * time.Second

// LogRate is the controller log's marked lines per second (design §16.3).
const LogRate = 50

// Controller Pod of every scale world (scale.Live).
const (
	ctrlUID  = "pod/kgw/kgateway-0"
	ctrlName = "kgateway-0"
)

// World is the measurement world over a settled cache.
type World struct {
	Live *scale.LiveWorld
	Tier scale.Tier
	// Metrics serves the tier's controller capture for every install.
	Metrics ctrlmetrics.Source
	// Store holds the proxy stats; the engine reads Store.Snapshot.
	Store *collect.Store
	// Stats scrapes the stub source; Installs feeds its rounds.
	Stats    *proxystats.Collector
	Installs func() ([]proxystats.Install, *cache.Snapshot)
	// Logs reads LogSource's "xds error" lines, one per Gateway in turn, with the
	// kgateway 2.4 parser; Scrape and Run publish it to Store.
	Logs      *ctrllogs.Collector
	LogSource *ctrllogstest.Source
	logsCtx   context.Context
	logsOff   bool
	mu        sync.Mutex
}

// New builds tier's measurement world on clk, which also stamps the cache, the
// store and the scrapes. Under IsWorst every Gateway rejects, its proxies are
// disconnected and warming, and every Pod with an Unhealthy Event fails its
// probe three more times after the watch started.
func New(tb testing.TB, tier scale.Tier, clk clock.WithTicker) *World {
	tb.Helper()
	w := &World{Live: scale.LivePhase3(tb, tier, clk.Now), Tier: tier}
	if tier.IsWorst {
		w.failProbes(tb, clk.Now())
	}
	m, err := ctrlmetrics.Parse(strings.NewReader(string(w.Live.Metrics)))
	if err != nil {
		tb.Fatal(err)
	}
	m.PerPod[0].UID, m.PerPod[0].Name = ctrlUID, ctrlName
	w.Metrics = static{ctrlmetrics.Result{Metrics: m, CollectedAt: clk.Now()}}
	w.Store = collect.New(w.Live.Signal, clk.Now)
	src := &stubSource{worst: tier.IsWorst, n: map[string]int64{}}
	w.Stats = proxystats.New(proxystats.Config{Source: src, Every: Every, Clock: clk, Installs: func() ([]proxystats.Install, *cache.Snapshot) {
		w.mu.Lock()
		f := w.Installs
		w.mu.Unlock()
		if f == nil {
			return nil, nil
		}
		return f()
	}})
	w.LogSource = &ctrllogstest.Source{Rate: LogRate, Clock: clk, Line: xdsLine(tier)}
	parser := computed.Selection{Rules: kgw24.New()}.LogParser()
	w.Logs = ctrllogs.New(ctrllogs.Config{Source: w.LogSource, Every: Every, Clock: clk, Installs: func() ([]ctrllogs.Install, bool) {
		w.mu.Lock()
		f, off := w.Installs, w.logsOff
		w.mu.Unlock()
		if off {
			return nil, true
		}
		if f == nil {
			return nil, false
		}
		ins, snap := f()
		if snap == nil {
			return nil, false
		}
		out := make([]ctrllogs.Install, 0, len(ins))
		for _, in := range ins {
			out = append(out, ctrllogs.Install{Resolved: in.Resolved, Parser: parser, Gateways: in.Gateways})
		}
		return out, true
	}, Stats: func() (proxystats.Stats, ctrllogs.StatsState) {
		v, ok := w.Store.Snapshot().Get(proxystats.Key)
		if !ok {
			return nil, ctrllogs.StatsPending
		}
		if s, isStats := v.Data.(proxystats.Stats); v.State == collect.OK && isStats {
			return s, ctrllogs.StatsReady
		}
		return nil, ctrllogs.StatsUnavailable
	}})
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)
	w.logsCtx = ctx
	return w
}

// xdsLine is a kgateway 2.4 "xds error" line for one of tier's Gateways in
// turn, rejecting the cluster the stub proxy stats name, so its name is kept.
func xdsLine(tier scale.Tier) func(int, time.Time) []byte {
	return func(n int, at time.Time) []byte {
		i := n % tier.Gateways
		ns, gw := testutil.ScaleNamespace(i%tier.Namespaces), testutil.ScaleGateway(i)
		return fmt.Appendf(nil, `{"time":%q,"level":"warn","msg":"xds error","component":"xds/envoy","gateway_name":%q,"gateway_ns":%q,`+
			`"resource":"envoy.config.cluster.v3.Cluster","error":"Error adding/updating cluster(s) kube_%s_%s_8080: Failed to initialize cipher suites NOT-A-CIPHER"}`,
			at.UTC().Format(time.RFC3339Nano), gw, ns, ns, gw)
	}
}

// StopLogs ends the controller log streams and waits for them.
func (w *World) StopLogs() {
	w.mu.Lock()
	w.logsOff = true
	w.mu.Unlock()
	w.Logs.Reconcile(w.logsCtx)
}

// SetInstalls sets what the stats rounds scrape.
func (w *World) SetInstalls(f func() ([]proxystats.Install, *cache.Snapshot)) {
	w.mu.Lock()
	w.Installs = f
	w.mu.Unlock()
}

// Scrape runs one stats round and publishes it, then the controller logs read
// up to the clock's now, as the collectors' Run does. The first Scrape starts the log streams.
func (w *World) Scrape(tb testing.TB) {
	tb.Helper()
	st, fp, ok := w.Stats.Round(context.Background())
	if !ok {
		tb.Fatal("no installs for the proxy stats round")
	}
	w.Store.Publish(proxystats.Key, st, fp, 3*Every)
	if !w.Logs.Reconcile(w.logsCtx) || w.Logs.Streams() == 0 {
		tb.Fatalf("the controller log round started %d streams", w.Logs.Streams())
	}
	ctx, cancel := context.WithTimeout(w.logsCtx, time.Minute)
	defer cancel()
	if err := w.LogSource.WaitIdle(ctx, w.Logs.Streams()); err != nil {
		tb.Fatalf("the controller log streams did not catch up: %v", err)
	}
	v, lfp := w.Logs.Value()
	w.Store.Publish(ctrllogs.Key, v, lfp, 3*Every)
}

// Run scrapes and reads the controller logs, publishing every Every, until ctx is done.
func (w *World) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Go(func() {
		_ = w.Logs.Run(ctx, func(key string, v any, fp uint64, ttl time.Duration) {
			w.Store.Publish(ctrllogs.CollectorName+"/"+key, v, fp, ttl)
		}, func(key string, st collect.State, reason string) {
			w.Store.Fail(ctrllogs.CollectorName+"/"+key, st, reason)
		})
	})
	_ = w.Stats.Run(ctx, func(key string, v any, fp uint64, ttl time.Duration) {
		w.Store.Publish(proxystats.CollectorName+"/"+key, v, fp, ttl)
	}, func(key string, st collect.State, reason string) {
		w.Store.Fail(proxystats.CollectorName+"/"+key, st, reason)
	})
	wg.Wait()
}

// failProbes adds three probe failures, seen at at, to every Unhealthy Event,
// and waits until the cache has observed them. Updates go in batches the
// cache observes before the next, as the fake watch panics once 100 events queue.
func (w *World) failProbes(tb testing.TB, at time.Time) {
	tb.Helper()
	const batch = 50
	ctx := context.Background()
	want := map[string]int{} // per Pod UID, the probe failure increases updated so far
	for chunk := range slices.Chunk(testutil.ScaleEvents(w.Tier.Shape(), 1), batch) {
		for _, e := range chunk {
			if e.Reason != process.ProbeReason {
				continue
			}
			e.Count += 3
			e.LastTimestamp = metav1.NewTime(at)
			e.ResourceVersion = ""
			if _, err := w.Live.Kube.CoreV1().Events(e.Namespace).Update(ctx, e, metav1.UpdateOptions{}); err != nil {
				tb.Fatal(err)
			}
			want[string(e.InvolvedObject.UID)]++
		}
		w.awaitProbes(tb, want)
	}
	w.Live.Kube.ClearActions()
	testutil.Drain(w.Live.Signal.C())
}

// awaitProbes waits until the cache has observed want[uid] increases by three on every Pod.
func (w *World) awaitProbes(tb testing.TB, want map[string]int) {
	tb.Helper()
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(10 * time.Millisecond) {
		seen := 0
		for uid, obs := range w.Live.Cache.Snapshot().EventDeltas {
			n := 0
			for _, o := range obs {
				if o.Delta == 3 {
					n++
				}
			}
			if want[string(uid)] > 0 && n >= want[string(uid)] {
				seen++
			}
		}
		if seen == len(want) {
			return
		}
		if time.Now().After(deadline) {
			tb.Fatalf("the cache observed probe failures on %d of %d Pods", seen, len(want))
		}
	}
}

// static serves one metrics result for every install.
type static struct{ r ctrlmetrics.Result }

func (s static) Get(string) (ctrlmetrics.Result, bool) { return s.r, true }

// stubSource is proxystats.Kube's targets and interpretation over a fixed body,
// whose reject counters grow on every fetch of a Pod.
type stubSource struct {
	worst bool
	mu    sync.Mutex
	n     map[string]int64
}

func (s *stubSource) Targets(in install.Resolved, gw objects.Ref, snap *cache.Snapshot) ([]proxystats.Target, bool) {
	return proxystats.Kube{}.Targets(in, gw, snap)
}

func (s *stubSource) Fetch(_ context.Context, t proxystats.Target) ([]byte, error) {
	s.mu.Lock()
	s.n[t.ID]++
	n := s.n[t.ID]
	s.mu.Unlock()
	connected, warming, rejected := 1, 0, int64(0)
	if s.worst {
		connected, warming, rejected = 0, 1, n
	}
	cluster := fmt.Sprintf("kube_%s_%s_8080", t.Gateway.Namespace, t.Gateway.Name)
	return fmt.Appendf(nil, "cluster.%s.update_rejected: %d\ncluster_manager.cds.update_rejected: %d\ncluster_manager.cds.update_success: 10\n"+
		"cluster_manager.warming_clusters: %d\ncontrol_plane.connected_state: %d\nhttp.http.rds.listener~8080.update_rejected: %d\n"+
		"listener_manager.lds.update_rejected: %d\nlistener_manager.lds.update_success: 10\nlistener_manager.total_listeners_warming: 0\n"+
		"server.state: 0\nserver.uptime: 3600\n", cluster, rejected, rejected, warming, connected, rejected, rejected), nil
}

func (s *stubSource) Interpret(t proxystats.Target, body []byte) (proxystats.PodStats, error) {
	return proxystats.Kube{}.Interpret(t, body)
}

// Clock is the real clock moved by Off, so fixtures dated around testutil.Now
// hold for a real-time run.
type Clock struct {
	clock.RealClock
	Off time.Duration
}

// ShiftedTo is the real clock moved so that it reads at now.
func ShiftedTo(at time.Time) *Clock { return &Clock{Off: time.Until(at)} }

// Now implements clock.Clock.
func (c *Clock) Now() time.Time { return time.Now().Add(c.Off) }

// Since implements clock.Clock.
func (c *Clock) Since(t time.Time) time.Duration { return c.Now().Sub(t) }
