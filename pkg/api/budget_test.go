//go:build bench

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"runtime/metrics"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

// The 2.2 request and stream budgets of design §9.6 at Large.

// requestsPerMix is the sample count per filter mix (design §9.6 method).
const requestsPerMix = 10_000

// percentile is the q-th percentile (nearest rank) of sorted d.
func percentile(d []time.Duration, q int) time.Duration {
	i := (len(d)*q + 99) / 100
	return d[max(i, 1)-1]
}

// latencies sends n GETs of path through h from c goroutines and returns the
// sorted ServeHTTP durations. Any status but 200 fails tb.
func latencies(tb testing.TB, h http.Handler, path string, n, c int) []time.Duration {
	out := make([]time.Duration, n)
	var next atomic.Int64
	var bad atomic.Value
	var wg sync.WaitGroup
	for range c {
		wg.Go(func() {
			req := httptest.NewRequest(http.MethodGet, path, nil) // the mux writes to it: one per goroutine
			for i := next.Add(1) - 1; i < int64(n); i = next.Add(1) - 1 {
				w := httptest.NewRecorder()
				t0 := time.Now()
				h.ServeHTTP(w, req)
				out[i] = time.Since(t0)
				if w.Code != http.StatusOK {
					bad.Store(fmt.Sprintf("GET %s: %d %s", path, w.Code, w.Body))
				}
			}
		})
	}
	wg.Wait()
	if b := bad.Load(); b != nil {
		tb.Fatal(b)
	}
	slices.Sort(out)
	return out
}

// listRows is the item count and total of one list response from h.
func listRows(tb testing.TB, h http.Handler, path string) (items, total int) {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	var page struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	if w.Code != http.StatusOK {
		tb.Fatalf("GET %s: %d %s", path, w.Code, w.Body)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		tb.Fatal(err)
	}
	return len(page.Items), page.Total
}

// widest is the viewable key of m with the most rows.
func widest(m map[objects.Ref][]explore.RowID, keep func(objects.Ref) bool) objects.Ref {
	var best objects.Ref
	for r, ids := range m {
		if keep(r) && (len(ids) > len(m[best]) || len(ids) == len(m[best]) && r.String() < best.String()) {
			best = r
		}
	}
	return best
}

func refParam(r objects.Ref) string { return r.Group + "/" + r.Kind + "/" + r.Namespace + "/" + r.Name }

// TestBudgetListRequests: per filter mix, 10,000 sequential requests give
// p95 < 50 ms and p99 < 100 ms, and 10,000 from 20 goroutines p95 < 100 ms;
// the deepest delegated child's detail p95 < 50 ms. Run under AllowAll, whose
// answers are precomputed, and under a restricting Authorizer.
func TestBudgetListRequests(t *testing.T) {
	for _, a := range []struct {
		name  string
		authz Authorizer
	}{{"allow-all", nil}, {"restricted", restricting}} {
		t.Run(a.name, func(t *testing.T) {
			h, m := largeHandlerWith(t, a.authz)
			ix := m.Installs[0].Index
			can := func(r objects.Ref) bool { return a.authz == nil || a.authz.Authorize(nil, r) }
			kindIs := func(k string) func(objects.Ref) bool {
				return func(r objects.Ref) bool { return r.Kind == k && can(r) }
			}
			gw, ls := widest(ix.ByParent, kindIs("Gateway")), widest(ix.ByParent, kindIs("ListenerSet"))
			parent := widest(ix.ByParentRoute, can)
			// The namespace with the most viewable not-accepted rows, so namespace+state returns rows.
			var ns string
			best := 0
			for n, ids := range ix.ByNamespace {
				if !can(objects.Ref{Namespace: n}) {
					continue
				}
				c := 0
				for _, id := range ids {
					if ix.Routes[id].Flags&explore.FlagNotAccepted != 0 {
						c++
					}
				}
				if c > best || c == best && c > 0 && n < ns {
					ns, best = n, c
				}
			}
			// The viewable Gateway listener with the most members, for the listener mix.
			var lk explore.ListenerKey
			for k, ms := range ix.ByListener {
				if k.Resource.Kind != "Gateway" || !can(k.Resource) {
					continue
				}
				if n := len(ix.ByListener[lk]); len(ms) > n || len(ms) == n && k.Resource.String()+k.Listener < lk.Resource.String()+lk.Listener {
					lk = k
				}
			}
			if lk.Listener == "" {
				t.Fatal("the Large tier has no listener members")
			}
			deepest, depth := deepestChild(ix, can)
			if depth == 0 || len(ix.ByParentRoute[parent]) == 0 {
				t.Fatal("the Large tier has no delegation")
			}
			t.Logf("parents: Gateway %s (%d rows), ListenerSet %s (%d rows), listener %s/%s (%d members), HTTPRoute %s (%d children); namespace %s (%d rows); deepest child %s at depth %d",
				gw, len(ix.ByParent[gw]), ls, len(ix.ByParent[ls]), lk.Resource, lk.Listener, len(ix.ByListener[lk]), parent, len(ix.ByParentRoute[parent]), ns, len(ix.ByNamespace[ns]), deepest, depth)
			base := "/api/v1/installs/" + m.Installs[0].Resolved.ID()
			mixes := []struct{ name, path string }{
				{"none", base + "/routes"},
				{"parent-gateway", base + "/routes?parent=" + refParam(gw)},
				{"parent-listenerset", base + "/routes?parent=" + refParam(ls)},
				{"parent+listener", base + "/routes?parent=" + refParam(lk.Resource) + "&listener=" + lk.Listener},
				{"namespace+state", base + "/routes?namespace=" + ns + "&state=not-accepted"},
				{"q", base + "/routes?q=r-01"},
				{"sort=health", base + "/routes?sort=health.desc"},
				{"delegation-widest-parent", base + "/routes?parent=" + refParam(parent)},
			}
			for _, mx := range mixes {
				items, total := listRows(t, h, mx.path)
				t.Logf("%-25s %d rows on the page, total %d", mx.name, items, total)
				if items == 0 {
					t.Fatalf("%s: GET %s returned no rows; it would measure nothing", mx.name, mx.path)
				}
				seq := latencies(t, h, mx.path, requestsPerMix, 1)
				conc := latencies(t, h, mx.path, requestsPerMix, 20)
				t.Logf("%-25s sequential p50 %v p95 %v p99 %v (budget p95 < 50 ms, p99 < 100 ms); 20 clients p50 %v p95 %v p99 %v (budget p95 < 100 ms)",
					mx.name, rd(percentile(seq, 50)), rd(percentile(seq, 95)), rd(percentile(seq, 99)),
					rd(percentile(conc, 50)), rd(percentile(conc, 95)), rd(percentile(conc, 99)))
				if p := percentile(seq, 95); p >= 50*time.Millisecond {
					t.Errorf("%s: sequential p95 %v, budget < 50 ms", mx.name, p)
				}
				if p := percentile(seq, 99); p >= 100*time.Millisecond {
					t.Errorf("%s: sequential p99 %v, budget < 100 ms", mx.name, p)
				}
				if p := percentile(conc, 95); p >= 100*time.Millisecond {
					t.Errorf("%s: 20-client p95 %v, budget < 100 ms", mx.name, p)
				}
			}
			d := latencies(t, h, base+"/routes/gateway.networking.k8s.io/HTTPRoute/"+deepest.Namespace+"/"+deepest.Name, requestsPerMix, 1)
			t.Logf("%-25s sequential p50 %v p95 %v p99 %v (budget p95 < 50 ms)", "detail-deepest-child", rd(percentile(d, 50)), rd(percentile(d, 95)), rd(percentile(d, 99)))
			if p := percentile(d, 95); p >= 50*time.Millisecond {
				t.Errorf("deepest child detail p95 %v, budget < 50 ms", p)
			}
		})
	}
}

// TestBudgetHealth reports /health's response size and latency, 1,000
// sequential requests and 1,000 from 20 goroutines (a worst-case response
// is about 2 MiB), for the consistent
// world and the worst case (every check matured), under AllowAll and a
// restricting Authorizer. §9.6 sets no /health budget, so nothing is asserted.
func TestBudgetHealth(t *testing.T) {
	for _, tier := range []scale.Tier{scale.Large, scale.Large.Contradicting()} {
		for _, a := range []struct {
			name  string
			authz Authorizer
		}{{"allow-all", nil}, {"restricted", restricting}} {
			t.Run(tier.Name+"/"+a.name, func(t *testing.T) {
				h, m := tierHandler(t, tier, a.authz)
				f, c := matured(m)
				path := "/api/v1/installs/" + m.Installs[0].Resolved.ID() + "/health"
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
				if rec.Code != http.StatusOK {
					t.Fatalf("GET %s: %d", path, rec.Code)
				}
				seq := latencies(t, h, path, 1000, 1)
				conc := latencies(t, h, path, 1000, 20)
				t.Logf("%s %s: %d findings (%d KGWUI-012 of %d checks), %d B (%.1f KiB); sequential p50 %v p95 %v p99 %v; 20 clients p50 %v p95 %v p99 %v",
					tier.Name, a.name, len(m.Installs[0].Health.Findings), f, c, rec.Body.Len(), float64(rec.Body.Len())/1024,
					rd(percentile(seq, 50)), rd(percentile(seq, 95)), rd(percentile(seq, 99)),
					rd(percentile(conc, 50)), rd(percentile(conc, 95)), rd(percentile(conc, 99)))
				if tier.Contradict && (f != c || c == 0) {
					t.Fatalf("worst case: %d KGWUI-012 of %d checks", f, c)
				}
			})
		}
	}
}

// rd rounds d for logs.
func rd(d time.Duration) time.Duration {
	if d < time.Millisecond {
		return d.Round(time.Microsecond)
	}
	return d.Round(10 * time.Microsecond)
}

// cpuTime is the process's user + system CPU time.
func cpuTime(tb testing.TB) time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		tb.Fatal(err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// gcCPU reads the runtime's estimates of GC and used (not idle) CPU time, in seconds.
func gcCPU() (gc, used float64) {
	s := []metrics.Sample{{Name: "/cpu/classes/gc/total:cpu-seconds"}, {Name: "/cpu/classes/total:cpu-seconds"}, {Name: "/cpu/classes/idle:cpu-seconds"}}
	metrics.Read(s)
	return s[0].Value.Float64(), s[1].Value.Float64() - s[2].Value.Float64()
}

// TestBudgetSustainedStream: over a real cache and Run loop at Large, one
// HTTPRoute update per 100 ms for 10 min (-short: 60 s) while 20 clients poll
// /routes every 5 s gives ≤ 1 recompute per s and process CPU ≤ 15 % of one
// core (rusage delta; it bounds the engine's share), and the memory obtained
// from the OS stays ≤ half the chart's 512 MiB pod limit. GC share is logged.
// Both the consistent world and the worst case run, each in a fresh process
// so earlier tests' heap and CPU do not count; the worst case starts once
// every check has matured.
func TestBudgetSustainedStream(t *testing.T) {
	tiers := []scale.Tier{scale.Large, scale.Large.Contradicting()}
	if name := os.Getenv(streamChildEnv); name != "" {
		sustainedStream(t, tiers[slices.IndexFunc(tiers, func(x scale.Tier) bool { return x.Name == name })])
		return
	}
	for _, tier := range tiers {
		t.Run(tier.Name, func(t *testing.T) {
			args := []string{"-test.run=^TestBudgetSustainedStream$", "-test.count=1", "-test.v"}
			if testing.Short() {
				args = append(args, "-test.short")
			}
			cmd := exec.Command(os.Args[0], args...)
			cmd.Env = append(os.Environ(), streamChildEnv+"="+tier.Name)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("stream child: %v\n%s", err, out)
				return
			}
			for line := range strings.Lines(string(out)) {
				if strings.Contains(line, "budget_test.go") {
					t.Log(strings.TrimSpace(line))
				}
			}
		})
	}
}

// streamChildEnv makes TestBudgetSustainedStream run one world in this process.
const streamChildEnv = "KGWUI_STREAM_CHILD"

// liveHeap is HeapAlloc after a full GC.
func liveHeap() uint64 {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// matured reports whether every computed check of m raised KGWUI-012.
func matured(m *engine.Model) (findings, checks int) {
	in := m.Installs[0]
	for _, f := range in.Health.Findings {
		if f.Code == health.CodeComputedDisagrees {
			findings++
		}
	}
	if in.Computed != nil {
		checks = len(in.Computed.Checks)
	}
	return findings, checks
}

func sustainedStream(t *testing.T, tier scale.Tier) {
	const mib = 1 << 20
	dur := 10 * time.Minute
	if testing.Short() {
		dur = time.Minute
	}
	w := scale.Live(t, tier, nil)
	ev := make(chan engine.LoopEvent, 1<<16)
	e := engine.New(engine.Config{Snapshots: w.Cache, Triggers: w.Signal, Served: testutil.ServedUpstream(), Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour), Metrics: okMetrics("kgw.kgateway"),
		// Static metrics never go stale here, as a scraped cluster's do not.
		Checks: health.Upstream(), MetricsMaxAge: 0,
		OnLoop: func(e engine.LoopEvent) {
			select {
			case ev <- e:
			default:
				t.Errorf("loop event buffer full")
			}
		}})
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		e.Run(ctx, 30*time.Second)
	}()
	t.Cleanup(func() {
		cancel()
		testutil.Recv(t, stopped, "Run to return")
	})
	for testutil.Recv(t, ev, "loop event").Kind != engine.EvPublished { // the start-up publication
	}
	if tier.Contradict {
		// Windows mature on the real clock about 30 s after start-up.
		for deadline := time.Now().Add(2 * time.Minute); ; time.Sleep(500 * time.Millisecond) {
			if f, c := matured(e.Current().Model); f == c && c > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the worst case did not mature")
			}
		}
		testutil.Drain(ev)
	}
	h := NewHandler(e, fstest.MapFS{}, nil)
	path := "/api/v1/installs/" + e.Current().Model.Installs[0].Resolved.ID() + "/routes"

	// Count runs and their wall time from loop events while the stream runs.
	var runs, pubs int
	var busy time.Duration
	var runStart time.Time
	counted := make(chan struct{})
	collect, stopCollect := context.WithCancel(context.Background())
	go func() {
		defer close(counted)
		for {
			select {
			case x := <-ev:
				switch x.Kind {
				case engine.EvRunStarted:
					runs++
					runStart = x.At
				case engine.EvPublished:
					pubs++
					busy += x.At.Sub(runStart)
				}
			case <-collect.Done():
				return
			}
		}
	}()

	cpu0 := cpuTime(t)
	gc0, tot0 := gcCPU()
	start := time.Now()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var polls []time.Duration
	var pollMu sync.Mutex
	for c := range 20 {
		wg.Go(func() {
			// Staggered so the clients do not poll in lockstep.
			select {
			case <-time.After(time.Duration(c) * 250 * time.Millisecond):
			case <-stop:
				return
			}
			tick := time.NewTicker(5 * time.Second)
			defer tick.Stop()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			for {
				rec := httptest.NewRecorder()
				t0 := time.Now()
				h.ServeHTTP(rec, req)
				d := time.Since(t0)
				if rec.Code != http.StatusOK {
					t.Errorf("poll: %d %s", rec.Code, rec.Body)
				}
				pollMu.Lock()
				polls = append(polls, d)
				pollMu.Unlock()
				select {
				case <-tick.C:
				case <-stop:
					return
				}
			}
		})
	}
	updates := 0
	// The live heap after a full GC, at the first minute mark and at the end: growth would be a leak.
	var heap60 uint64
	tick := time.NewTicker(100 * time.Millisecond)
	for deadline := start.Add(dur); time.Now().Before(deadline); <-tick.C {
		if heap60 == 0 && dur > time.Minute && time.Since(start) >= time.Minute {
			heap60 = liveHeap()
		}
		u := w.Routes[updates%len(w.Routes)].DeepCopy()
		updates++
		u.SetResourceVersion(strconv.Itoa(1_000_000 + updates))
		u.SetAnnotations(map[string]string{"stream": strconv.Itoa(updates)})
		if err := w.Update(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}
	tick.Stop()
	close(stop)
	wg.Wait()
	wall := time.Since(start)
	cpu := cpuTime(t) - cpu0
	gc1, tot1 := gcCPU()
	stopCollect()
	<-counted

	if len(polls) == 0 {
		t.Fatal("no poll completed")
	}
	slices.Sort(polls)
	rate := float64(runs) / wall.Seconds()
	share := cpu.Seconds() / wall.Seconds()
	t.Logf("stream %v: %d updates, %d recomputes (%.2f/s, budget ≤ 1/s), %d publications; process CPU %v = %.1f %% of one core (budget ≤ 15 %%); recompute wall %v = %.1f %%; GC share %.1f %% of used CPU (%.1f %% of one core)",
		wall.Round(time.Millisecond), updates, runs, rate, pubs, cpu.Round(time.Millisecond), 100*share, busy.Round(time.Millisecond), 100*busy.Seconds()/wall.Seconds(), 100*(gc1-gc0)/(tot1-tot0), 100*(gc1-gc0)/wall.Seconds())
	t.Logf("polls: %d, p50 %v p95 %v max %v", len(polls), rd(percentile(polls, 50)), rd(percentile(polls, 95)), rd(polls[len(polls)-1]))
	heapEnd := liveHeap()
	if heap60 == 0 {
		heap60 = heapEnd // the -short run ends at the minute mark
	}
	t.Logf("%s: heap after GC %.1f MiB at 1 min, %.1f MiB at the end (%+.1f MiB)",
		tier.Name, float64(heap60)/mib, float64(heapEnd)/mib, (float64(heapEnd)-float64(heap60))/mib)
	f, c := matured(e.Current().Model)
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	t.Logf("%s: %d KGWUI-012 of %d checks at the end; memory from the OS %.1f MiB, heap in use %.1f MiB (budget ≤ 256 MiB, half the 512 MiB pod limit)",
		tier.Name, f, c, float64(ms.Sys)/mib, float64(ms.HeapInuse)/mib)
	if tier.Contradict && f != c {
		t.Errorf("worst case: %d KGWUI-012 of %d checks at the end", f, c)
	}
	if ms.Sys > 256*mib {
		t.Errorf("memory from the OS %.1f MiB, budget ≤ 256 MiB", float64(ms.Sys)/mib)
	}
	if rate > 1 {
		t.Errorf("%.2f recomputes per s, budget ≤ 1", rate)
	}
	if share > 0.15 {
		t.Errorf("process CPU %.1f %% of one core, budget ≤ 15 %%", 100*share)
	}
}
