package proxystats_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/clock"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/rulesets"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

func ptr[T any](v T) *T { return &v }

func capture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The Task 20 captures, unfiltered, read into the stats of the §8.8 table only.
func TestParseCaptures(t *testing.T) {
	cases := []struct {
		file string
		want proxystats.PodStats
	}{
		{"v2.3.11-stats-ok.txt", proxystats.PodStats{LDS: proxystats.Pair{Success: 1}, CDS: proxystats.Pair{Success: 1}, Uptime: 35}},
		{"v2.3.11-stats-rejecting.txt", proxystats.PodStats{LDS: proxystats.Pair{Success: 1}, CDS: proxystats.Pair{Rejected: 384, Success: 1},
			RDS: map[string]int64{"listener~8080": 485}, Uptime: 115}},
		{"v2.4.7-stats-ok.txt", proxystats.PodStats{LDS: proxystats.Pair{Success: 1}, CDS: proxystats.Pair{Success: 1}, Uptime: 20}},
		{"v2.4.7-stats-rejecting.txt", proxystats.PodStats{LDS: proxystats.Pair{Success: 1}, CDS: proxystats.Pair{Rejected: 712, Success: 1},
			RDS: map[string]int64{"listener~8080": 812}, Uptime: 165}},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			got, err := proxystats.Kube{}.Interpret(proxystats.Target{ID: "u", Name: "p"}, capture(t, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			want := tc.want
			want.Connected, want.WarmingListeners, want.WarmingClusters, want.ServerState = ptr(true), ptr(int64(0)), ptr(int64(0)), "live"
			want.UID, want.Name = "u", "p"
			if !samePodStats(got, want) {
				t.Fatalf("got %s\nwant %s", show(got), show(want))
			}
		})
	}
}

func samePodStats(a, b proxystats.PodStats) bool {
	eqP := func(x, y *int64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return a.UID == b.UID && a.Name == b.Name && a.Gateway == b.Gateway && (a.Connected == nil) == (b.Connected == nil) &&
		(a.Connected == nil || *a.Connected == *b.Connected) && eqP(a.WarmingListeners, b.WarmingListeners) && eqP(a.WarmingClusters, b.WarmingClusters) &&
		a.LDS == b.LDS && a.CDS == b.CDS && maps.Equal(a.RDS, b.RDS) && maps.Equal(a.Clusters, b.Clusters) &&
		a.ServerState == b.ServerState && a.Uptime == b.Uptime && a.Err == b.Err
}

func show(p proxystats.PodStats) string {
	s := fmt.Sprintf("%+v", p)
	if p.Connected != nil {
		s += fmt.Sprintf(" connected=%v", *p.Connected)
	}
	return s
}

// A stat name outside the filter is ignored, even when it shares a prefix or suffix.
func TestStatOutsideFilterIgnored(t *testing.T) {
	body := strings.Join([]string{
		"listener_manager.lds.update_rejected_total: 9",
		"foo.server.state: 1",
		"server.state_x: 1",
		"http.x.rds.y.update_success: 4",
		"listener.0.0.0.0_8080.server.uptime: 7",
		"cluster.a.b.update_rejected: 2",
		"cluster.a.update_attempt: 3",
		"cluster_manager.cds.update_rejected: 5",
		"server.state: 1",
		"listener_manager.lds.update_rejected: not-a-number",
		"",
	}, "\n")
	got, err := proxystats.Kube{}.Interpret(proxystats.Target{}, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	want := proxystats.PodStats{Clusters: map[string]int64{"a.b": 2}, CDS: proxystats.Pair{Rejected: 5}, ServerState: "draining"}
	if !samePodStats(got, want) {
		t.Fatalf("got %s\nwant %s", show(got), show(want))
	}
}

// The request reads only the stats listener's /stats with the one filter (D-037).
func TestKubeRequestsFilteredStats(t *testing.T) {
	var got *url.URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL
		_, _ = io.WriteString(w, "server.state: 0\n")
	}))
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	k := proxystats.Kube{Client: srv.Client(), Port: int32(p)}
	body, err := k.Fetch(context.Background(), proxystats.Target{Addr: net.JoinHostPort(host, port)})
	if err != nil || string(body) != "server.state: 0\n" {
		t.Fatalf("fetch: %q %v", body, err)
	}
	q := got.Query()
	if got.Path != "/stats" || !q.Has("usedonly") || q.Get("filter") != proxystats.StatsFilter || len(q) != 2 {
		t.Fatalf("request %s", got)
	}
	if (proxystats.Kube{}).Port != 0 || proxystats.StatsPort != 9091 {
		t.Fatal("the default port is the stats listener's 9091")
	}
}

// installA is the upstream install in kgw.
func installOf(t *testing.T, ns string) install.Resolved {
	t.Helper()
	d := testutil.ControllerDeployment(ns, "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	in, ok := install.UpstreamDetector{}.Detect(d)
	if !ok {
		t.Fatal("upstream detector")
	}
	return install.Resolved{Install: in}
}

// proxyDeployment is gw's kgateway proxy Deployment in ns, selecting by the safe name.
func proxyDeployment(ns, gw string) *appsv1.Deployment {
	safe := process.SafeGatewayLabelValue(gw)
	sel := map[string]string{"gateway.networking.k8s.io/gateway-name": safe}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: safe, UID: types.UID("proxy/" + ns + "/" + gw),
			Labels:          map[string]string{"app.kubernetes.io/managed-by": "kgateway", "app.kubernetes.io/component": "proxy", "gateway.networking.k8s.io/gateway-name": safe},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "gateway.networking.k8s.io/v1", Kind: "Gateway", Name: gw}}},
		Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: sel}},
	}
}

// proxyPod is a running proxy Pod of d's ReplicaSet with ip.
func proxyPod(d *appsv1.Deployment, name, ip string) *corev1.Pod {
	lbls := maps.Clone(d.Spec.Selector.MatchLabels)
	lbls["pod-template-hash"] = "5d8f7"
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: d.Namespace, Name: name, UID: types.UID("pod/" + d.Namespace + "/" + name), Labels: lbls,
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: d.Name + "-5d8f7", Controller: ptr(true)}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: ip, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
}

func gwRef(ns, name string) objects.Ref {
	return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: ns, Name: name}
}

func snapshot(deps []*appsv1.Deployment, pods ...*corev1.Pod) *cache.Snapshot {
	return &cache.Snapshot{Deployments: deps, ProxyPods: pods, TakenAt: testutil.Now, Synced: true,
		Access: map[schema.GroupVersionResource]cache.Access{cache.ProxyPodsGVR: {OK: true}}}
}

// hostRT answers per request host; a missing host is refused.
type hostRT map[string]func(*http.Request) (*http.Response, error)

func (h hostRT) RoundTrip(r *http.Request) (*http.Response, error) {
	if f, ok := h[r.URL.Hostname()]; ok {
		return f(r)
	}
	return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
}

func body(s string) func(*http.Request) (*http.Response, error) {
	return func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(s))}, nil
	}
}

func collector(src proxystats.Source, ins func() ([]proxystats.Install, *cache.Snapshot)) *proxystats.Collector {
	return proxystats.New(proxystats.Config{Source: src, Every: 10 * time.Second, Installs: ins})
}

// tickerClock is a fake clock that reports each ticker it makes, so a step
// never runs before the collector waits on it.
type tickerClock struct {
	*clocktesting.FakeClock
	made chan struct{}
}

func (c tickerClock) NewTicker(d time.Duration) clock.Ticker {
	tk := c.FakeClock.NewTicker(d)
	c.made <- struct{}{}
	return tk
}

func static(snap *cache.Snapshot, ins ...proxystats.Install) func() ([]proxystats.Install, *cache.Snapshot) {
	return func() ([]proxystats.Install, *cache.Snapshot) { return ins, snap }
}

func podsOf(t *testing.T, st proxystats.Stats, id string, gw objects.Ref) proxystats.Gateway {
	t.Helper()
	for _, g := range st[id] {
		if g.Gateway == gw {
			return g
		}
	}
	t.Fatalf("install %s has no Gateway %s: %+v", id, gw, st[id])
	return proxystats.Gateway{}
}

// A refused listener, a timeout and a body of 1 MiB + 1 byte each fail only
// their Pod; the Gateway's other proxy keeps its stats.
func TestScrapeFailuresStayOnThePod(t *testing.T) {
	big := strings.Repeat("x", proxystats.MaxBody) + "\n"
	timeout := func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	cases := []struct {
		name string
		rt   func(*http.Request) (*http.Response, error)
		err  string
	}{
		{"refused", nil, "connection refused"},
		{"timeout", timeout, "timed out"},
		{"too large", body(big), "response over 1 MiB"},
		{"not found", func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("secret body text"))}, nil
		}, "HTTP 404"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(big) != proxystats.MaxBody+1 || proxystats.MaxBody != 1<<20 {
				t.Fatal("the body bound is 1 MiB")
			}
			d := proxyDeployment("infra", "edge")
			rt := hostRT{"10.0.0.1": body("control_plane.connected_state: 1\n")}
			if tc.rt != nil {
				rt["10.0.0.2"] = tc.rt
			}
			snap := snapshot([]*appsv1.Deployment{d}, proxyPod(d, "edge-1", "10.0.0.1"), proxyPod(d, "edge-2", "10.0.0.2"))
			in := proxystats.Install{Resolved: installOf(t, "kgw"), Gateways: []objects.Ref{gwRef("infra", "edge")}}
			src := proxystats.Kube{Client: &http.Client{Transport: rt}}
			// A 10 ms interval bounds each fetch at 5 ms, so the timeout case ends fast.
			c := proxystats.New(proxystats.Config{Source: src, Every: 10 * time.Millisecond, Installs: static(snap, in)})
			st, _, ok := c.Round(context.Background())
			if !ok {
				t.Fatal("no round")
			}
			g := podsOf(t, st, in.Resolved.ID(), gwRef("infra", "edge"))
			if !g.Observed || len(g.Pods) != 2 {
				t.Fatalf("gateway %+v", g)
			}
			good, bad := g.Pods[0], g.Pods[1]
			if good.Err != "" || good.Connected == nil || !*good.Connected {
				t.Fatalf("the other proxy keeps its stats: %+v", good)
			}
			if !strings.Contains(bad.Err, tc.err) || strings.Contains(bad.Err, "secret") || bad.Connected != nil {
				t.Fatalf("failing Pod: want %q, got %+v", tc.err, bad)
			}
		})
	}
}

// A real closed port reads as refused through the default in-cluster transport.
func TestRefusedListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	p, _ := strconv.Atoi(port)
	d := proxyDeployment("infra", "edge")
	snap := snapshot([]*appsv1.Deployment{d}, proxyPod(d, "edge-1", "127.0.0.1"))
	in := proxystats.Install{Resolved: installOf(t, "kgw"), Gateways: []objects.Ref{gwRef("infra", "edge")}}
	st, _, _ := collector(proxystats.Kube{Port: int32(p)}, static(snap, in)).Round(context.Background())
	if g := podsOf(t, st, in.Resolved.ID(), gwRef("infra", "edge")); len(g.Pods) != 1 || g.Pods[0].Err != "connection refused" {
		t.Fatalf("refused: %+v", g)
	}
}

// A self-managed Gateway has no proxy Deployment: not observed, never failing.
func TestSelfManagedGatewayNotObserved(t *testing.T) {
	d := proxyDeployment("infra", "edge")
	snap := snapshot([]*appsv1.Deployment{d}, proxyPod(d, "edge-1", "10.0.0.1"))
	in := proxystats.Install{Resolved: installOf(t, "kgw"), Gateways: []objects.Ref{gwRef("infra", "edge"), gwRef("infra", "self")}}
	src := proxystats.Kube{Client: &http.Client{Transport: hostRT{"10.0.0.1": body("server.state: 0\n")}}}
	st, _, _ := collector(src, static(snap, in)).Round(context.Background())
	if g := podsOf(t, st, in.Resolved.ID(), gwRef("infra", "self")); g.Observed || len(g.Pods) != 0 {
		t.Fatalf("self-managed: %+v", g)
	}
	if g := podsOf(t, st, in.Resolved.ID(), gwRef("infra", "edge")); !g.Observed || len(g.Pods) != 1 || g.Pods[0].ServerState != "live" {
		t.Fatalf("managed: %+v", g)
	}
	// Without the proxy Pod watch nothing is observed.
	snap.Access = nil
	st, _, _ = collector(src, static(snap, in)).Round(context.Background())
	if g := podsOf(t, st, in.Resolved.ID(), gwRef("infra", "edge")); g.Observed {
		t.Fatalf("no proxy Pod watch: %+v", g)
	}
}

// Review Focus 3: long Gateway names sharing a truncated prefix keep their own proxies.
func TestLongGatewayNamesKeepTheirProxies(t *testing.T) {
	long := strings.Repeat("a", 55)
	n1, n2 := long+"-one-gateway-name", long+"-two-gateway-name"
	d1, d2 := proxyDeployment("infra", n1), proxyDeployment("infra", n2)
	d1.OwnerReferences, d2.OwnerReferences = nil, nil // label-only, as the scale world builds them
	snap := snapshot([]*appsv1.Deployment{d1, d2}, proxyPod(d1, "p1", "10.0.0.1"), proxyPod(d2, "p2", "10.0.0.2"))
	r := installOf(t, "kgw")
	for gw, want := range map[string]string{n1: "p1", n2: "p2"} {
		ts, ok := proxystats.Kube{}.Targets(r, gwRef("infra", gw), snap)
		if !ok || len(ts) != 1 || ts[0].Name != want || ts[0].Addr != "10.0.0."+want[1:]+":9091" {
			t.Fatalf("%s: %+v %v", gw, ts, ok)
		}
	}
}

// blockingSource blocks every Fetch until release is closed and records the
// most fetches in flight at once.
type blockingSource struct {
	targets           []proxystats.Target
	entered           chan struct{}
	release           chan struct{}
	inflight, maxSeen atomic.Int32
}

func (b *blockingSource) Targets(install.Resolved, objects.Ref, *cache.Snapshot) ([]proxystats.Target, bool) {
	return b.targets, true
}

func (b *blockingSource) Fetch(ctx context.Context, _ proxystats.Target) ([]byte, error) {
	n := b.inflight.Add(1)
	defer b.inflight.Add(-1)
	for {
		m := b.maxSeen.Load()
		if n <= m || b.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	b.entered <- struct{}{}
	<-b.release
	return []byte("{}"), nil
}

func (b *blockingSource) Interpret(t proxystats.Target, _ []byte) (proxystats.PodStats, error) {
	return proxystats.PodStats{}, nil
}

// At most 16 Pods of one install are scraped at once.
func TestAtMostSixteenScrapesPerInstall(t *testing.T) {
	src := &blockingSource{entered: make(chan struct{}, 64), release: make(chan struct{})}
	for i := range 40 {
		src.targets = append(src.targets, proxystats.Target{ID: fmt.Sprint(i), Name: fmt.Sprintf("p%02d", i), Gateway: gwRef("infra", "edge")})
	}
	in := proxystats.Install{Resolved: installOf(t, "kgw"), Gateways: []objects.Ref{gwRef("infra", "edge")}}
	// The interval keeps the fetch deadline far beyond the test.
	c := proxystats.New(proxystats.Config{Source: src, Every: time.Hour, Installs: static(snapshot(nil), in)})
	done := make(chan proxystats.Stats)
	go func() {
		st, _, _ := c.Round(context.Background())
		done <- st
	}()
	for range 16 {
		testutil.Recv(t, src.entered, "16 fetches in flight")
	}
	if n := src.inflight.Load(); n != 16 {
		t.Fatalf("in flight %d, want 16", n)
	}
	close(src.release)
	st := testutil.Recv(t, done, "the round")
	if m := src.maxSeen.Load(); m != 16 {
		t.Fatalf("at most %d fetches at once, want 16", m)
	}
	if g := podsOf(t, st, in.Resolved.ID(), gwRef("infra", "edge")); len(g.Pods) != 40 {
		t.Fatalf("%d Pods scraped", len(g.Pods))
	}
}

// scriptSource serves each target's next PodStats from a script; an empty
// script fails the fetch.
type scriptSource struct {
	mu      sync.Mutex
	targets map[objects.Ref][]proxystats.Target
	script  map[string][]proxystats.PodStats
}

func (s *scriptSource) Targets(_ install.Resolved, gw objects.Ref, _ *cache.Snapshot) ([]proxystats.Target, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts, ok := s.targets[gw]
	return ts, ok
}

func (s *scriptSource) Fetch(_ context.Context, t proxystats.Target) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.script[t.ID]) == 0 {
		return nil, errors.New("no stats")
	}
	// A scripted Err fails this fetch.
	if e := s.script[t.ID][0].Err; e != "" {
		s.script[t.ID] = s.script[t.ID][1:]
		return nil, errors.New(e)
	}
	return []byte(t.ID), nil
}

func (s *scriptSource) Interpret(t proxystats.Target, _ []byte) (proxystats.PodStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.script[t.ID][0]
	s.script[t.ID] = s.script[t.ID][1:]
	return p, nil
}

func counters(lds, cds, uptime int64, rds map[string]int64) proxystats.PodStats {
	return proxystats.PodStats{LDS: proxystats.Pair{Rejected: lds}, CDS: proxystats.Pair{Rejected: cds}, Uptime: uptime, RDS: rds}
}

// Rejects are observed since the previous scrape per Pod UID: growth is a
// reject, a reset (lower counter, lower uptime or a new UID) is not, and a
// vanished UID leaves no state (Review Focus 2).
func TestRejectsObservedSinceLastScrape(t *testing.T) {
	gw := gwRef("infra", "edge")
	src := &scriptSource{targets: map[objects.Ref][]proxystats.Target{gw: {{ID: "u1", Name: "edge-1", Gateway: gw}}},
		script: map[string][]proxystats.PodStats{"u1": {
			counters(3, 8, 10, nil),                                  // first scrape: nothing to compare
			counters(4, 9, 20, map[string]int64{"listener~8080": 2}), // both grew; a new name with a count grew from 0
			counters(4, 9, 30, map[string]int64{"listener~8080": 2}), // no growth
			counters(1, 1, 40, nil),                                  // lower counters: a reset
			counters(9, 9, 5, map[string]int64{"listener~8080": 50}), // lower uptime: a restart, even though counters grew
		}}}
	clk := testutil.FakeClock()
	in := proxystats.Install{Resolved: installOf(t, "kgw"), Gateways: []objects.Ref{gw}}
	c := proxystats.New(proxystats.Config{Source: src, Every: 10 * time.Second, Clock: clk, Installs: static(snapshot(nil), in)})
	id := in.Resolved.ID()
	type want struct {
		lds, cds bool
		rds      []string
	}
	var since time.Time
	for i, w := range []want{{}, {true, true, []string{"listener~8080"}}, {}, {}, {}} {
		st, _, _ := c.Round(context.Background())
		p := podsOf(t, st, id, gw).Pods[0]
		if p.LDS.Seen != w.lds || p.CDS.Seen != w.cds || !slices.Equal(p.RDS, w.rds) {
			t.Fatalf("scrape %d: want %+v, got %+v", i, w, p)
		}
		if i == 1 {
			since = p.LDS.Since
			if !since.Equal(clk.Now()) {
				t.Fatalf("since %v, want %v", since, clk.Now())
			}
		}
		clk.Step(10 * time.Second)
	}
	if c.Tracked() != 1 {
		t.Fatalf("tracked %d", c.Tracked())
	}
	// The Pod is replaced: the old UID leaves nothing, the new one starts unknown.
	src.targets[gw] = []proxystats.Target{{ID: "u2", Name: "edge-2", Gateway: gw}}
	src.script["u2"] = []proxystats.PodStats{counters(7, 7, 100, nil)}
	st, _, _ := c.Round(context.Background())
	if p := podsOf(t, st, id, gw).Pods[0]; p.UID != "u2" || p.LDS.Seen || p.CDS.Seen {
		t.Fatalf("new UID: %+v", p)
	}
	if c.Tracked() != 1 {
		t.Fatalf("a vanished UID must leave no state: tracked %d", c.Tracked())
	}
	src.targets = map[objects.Ref][]proxystats.Target{}
	c.Round(context.Background())
	if c.Tracked() != 0 {
		t.Fatalf("no targets, tracked %d", c.Tracked())
	}
}

// Carry-forward b: a proxy that keeps rejecting grows its counters on every
// scrape, but what is published, and so its fingerprint, holds still.
func TestKeptRejectingIsOneFingerprint(t *testing.T) {
	gw := gwRef("infra", "edge")
	var script []proxystats.PodStats
	for i := range 6 {
		p := counters(0, int64(100*i+i*i), int64(10*i+1), map[string]int64{"listener~8080": int64(97 * i)})
		p.Connected = ptr(true)
		script = append(script, p)
	}
	src := &scriptSource{targets: map[objects.Ref][]proxystats.Target{gw: {{ID: "u1", Name: "edge-1", Gateway: gw}}},
		script: map[string][]proxystats.PodStats{"u1": script}}
	clk := testutil.FakeClock()
	in := proxystats.Install{Resolved: installOf(t, "kgw"), Gateways: []objects.Ref{gw}}
	c := proxystats.New(proxystats.Config{Source: src, Every: 10 * time.Second, Clock: clk, Installs: static(snapshot(nil), in)})
	var prints []uint64
	for range 6 {
		_, fp, _ := c.Round(context.Background())
		prints = append(prints, fp)
		clk.Step(10 * time.Second)
	}
	// Round 1 has no baseline, round 2 starts the streak; from then on the print holds.
	if prints[0] == prints[1] || slices.ContainsFunc(prints[2:], func(p uint64) bool { return p != prints[1] }) {
		t.Fatalf("prints %x", prints)
	}
	// The streak ends: the print changes.
	src.script["u1"] = []proxystats.PodStats{script[5]}
	if _, fp, _ := c.Round(context.Background()); fp == prints[1] {
		t.Fatal("a reject that stops must change the print")
	}
}

// Review Focus 1: two installs' proxy stats and state never cross.
func TestProxyStatsPerInstall(t *testing.T) {
	dA, dB := proxyDeployment("infra", "a"), proxyDeployment("infra", "b")
	snap := snapshot([]*appsv1.Deployment{dA, dB}, proxyPod(dA, "a-1", "10.0.0.1"), proxyPod(dA, "a-2", "10.0.0.2"),
		proxyPod(dB, "b-1", "10.0.1.1"), proxyPod(dB, "b-2", "10.0.1.2"))
	rt := hostRT{
		"10.0.0.1": body("cluster_manager.cds.update_rejected: 1\n"), "10.0.0.2": body("cluster_manager.cds.update_rejected: 1\n"),
		"10.0.1.1": body("control_plane.connected_state: 0\n"), // 10.0.1.2 is refused
	}
	a := proxystats.Install{Resolved: installOf(t, "kgw"), Gateways: []objects.Ref{gwRef("infra", "a")}}
	b := proxystats.Install{Resolved: installOf(t, "kgw2"), Gateways: []objects.Ref{gwRef("infra", "b")}}
	ins := []proxystats.Install{a, b}
	c := collector(proxystats.Kube{Client: &http.Client{Transport: rt}}, func() ([]proxystats.Install, *cache.Snapshot) { return ins, snap })
	st, _, _ := c.Round(context.Background())
	if len(st) != 2 || len(st[a.Resolved.ID()]) != 1 || len(st[b.Resolved.ID()]) != 1 {
		t.Fatalf("stats per install: %+v", st)
	}
	names := func(g proxystats.Gateway) (out []string) {
		for _, p := range g.Pods {
			out = append(out, p.Name+":"+p.Err)
		}
		return out
	}
	if got := names(podsOf(t, st, a.Resolved.ID(), gwRef("infra", "a"))); !slices.Equal(got, []string{"a-1:", "a-2:"}) {
		t.Fatalf("install a: %v", got)
	}
	gb := podsOf(t, st, b.Resolved.ID(), gwRef("infra", "b"))
	if got := names(gb); !slices.Equal(got, []string{"b-1:", "b-2:connection refused"}) || !gb.Observed || *gb.Pods[0].Connected {
		t.Fatalf("install b: %v %+v", got, gb)
	}
	// The refused Pod has no counters to hold.
	if c.Tracked() != 3 {
		t.Fatalf("tracked %d, want each scraped Pod once", c.Tracked())
	}
	// Install b goes away: its state goes with it, a's stays.
	ins = []proxystats.Install{a}
	st, _, _ = c.Round(context.Background())
	if _, ok := st[b.Resolved.ID()]; ok || c.Tracked() != 2 {
		t.Fatalf("after b left: %+v, tracked %d", st, c.Tracked())
	}
}

// Run publishes under the core key with a TTL of three intervals, and only
// the toggle's off state when proxy stats are off.
func TestRunPublishesAndHonoursOff(t *testing.T) {
	if proxystats.Key != "proxy-stats/stats" || proxystats.CollectorName+"/"+proxystats.ValueKey != proxystats.Key {
		t.Fatalf("key %q", proxystats.Key)
	}
	type pub struct {
		key string
		fp  uint64
		ttl time.Duration
		st  collect.State
	}
	run := func(cfg proxystats.Config) (chan pub, context.CancelFunc, chan error) {
		got := make(chan pub, 16)
		ctx, cancel := context.WithCancel(context.Background())
		errc := make(chan error, 1)
		go func() {
			errc <- proxystats.New(cfg).Run(ctx,
				func(key string, _ any, fp uint64, ttl time.Duration) { got <- pub{key, fp, ttl, collect.OK} },
				func(key string, st collect.State, _ string) { got <- pub{key: key, st: st} })
		}()
		return got, cancel, errc
	}
	// Off: no Source is called, one off state is published.
	got, cancel, errc := run(proxystats.Config{Off: true, Every: 10 * time.Second})
	if p := testutil.Recv(t, got, "the off state"); p.key != proxystats.ValueKey || p.st != collect.Off {
		t.Fatalf("off: %+v", p)
	}
	cancel()
	if err := testutil.Recv(t, errc, "Run to return"); err != nil {
		t.Fatal(err)
	}

	clk := tickerClock{FakeClock: testutil.FakeClock(), made: make(chan struct{}, 1)}
	d := proxyDeployment("infra", "edge")
	snap := snapshot([]*appsv1.Deployment{d}, proxyPod(d, "edge-1", "10.0.0.1"))
	in := proxystats.Install{Resolved: installOf(t, "kgw"), Gateways: []objects.Ref{gwRef("infra", "edge")}}
	src := proxystats.Kube{Client: &http.Client{Transport: hostRT{"10.0.0.1": body("server.state: 0\n")}}}
	got, cancel, errc = run(proxystats.Config{Source: src, Every: 10 * time.Second, Clock: clk, Installs: static(snap, in)})
	first := testutil.Recv(t, got, "the first round")
	if first.key != proxystats.ValueKey || first.st != collect.OK || first.ttl != 30*time.Second || first.fp == 0 {
		t.Fatalf("first publish %+v", first)
	}
	testutil.Recv(t, clk.made, "the ticker")
	clk.Step(10 * time.Second)
	if p := testutil.Recv(t, got, "the next round"); p.fp != first.fp {
		t.Fatalf("unchanged stats keep their print: %x then %x", first.fp, p.fp)
	}
	cancel()
	if err := testutil.Recv(t, errc, "Run to return"); err != nil {
		t.Fatal(err)
	}
}

// The fingerprint ignores order: the same Pods listed in any order are one print.
func TestFingerprintIsOrderFree(t *testing.T) {
	d := proxyDeployment("infra", "edge")
	p1, p2 := proxyPod(d, "edge-1", "10.0.0.1"), proxyPod(d, "edge-2", "10.0.0.2")
	rt := hostRT{"10.0.0.1": body("server.state: 0\n"), "10.0.0.2": body("server.state: 1\n")}
	in := proxystats.Install{Resolved: installOf(t, "kgw"), Gateways: []objects.Ref{gwRef("infra", "edge")}}
	src := proxystats.Kube{Client: &http.Client{Transport: rt}}
	_, a, _ := collector(src, static(snapshot([]*appsv1.Deployment{d}, p1, p2), in)).Round(context.Background())
	_, b, _ := collector(src, static(snapshot([]*appsv1.Deployment{d}, p2, p1), in)).Round(context.Background())
	if a != b || a == 0 {
		t.Fatalf("prints %x %x", a, b)
	}
	if !bytes.Contains([]byte(proxystats.StatsFilter), []byte(`server\.uptime`)) {
		t.Fatal("the filter names server.uptime")
	}
}

// Envoy names stay Envoy names: no shipped rule set has a verified namer yet.
func TestShippedRuleSetsHaveNoEnvoyNamer(t *testing.T) {
	for _, v := range []string{"2.3.11", "2.4.7"} {
		sel := rulesets.Select(v)
		if sel.Rules == nil || sel.EnvoyNamer() != nil {
			t.Fatalf("%s: rules %v, namer %v", v, sel.Rules, sel.EnvoyNamer())
		}
	}
	if (computed.Selection{}).EnvoyNamer() != nil {
		t.Fatal("no rule set, no namer")
	}
}

// A 3xx is never followed, in-cluster or through pods/proxy: the redirect
// target sees no request and the Pod fails with the status.
func TestRedirectsAreNeverFollowed(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "server.state: 0\n")
	}))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.RedirectHandler(target.URL+"/stats", http.StatusFound))
	t.Cleanup(redirect.Close)
	host, port, _ := net.SplitHostPort(redirect.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	tg := proxystats.Target{Name: "edge-1", Gateway: gwRef("infra", "edge"), Addr: net.JoinHostPort(host, port)}
	pods, err := kubernetes.NewForConfig(&rest.Config{Host: redirect.URL})
	if err != nil {
		t.Fatal(err)
	}
	for name, k := range map[string]proxystats.Kube{
		"default client":  {Port: int32(p)},
		"supplied client": {Client: &http.Client{}, Port: int32(p)},
		"pods/proxy":      {Kube: pods, Port: int32(p)},
	} {
		if _, err := k.Fetch(context.Background(), tg); err == nil || err.Error() != "HTTP 302" {
			t.Errorf("%s: want HTTP 302, got %v", name, err)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("the redirect target saw %d requests", n)
	}
	if tr, ok := proxystats.NewHTTPClient().Transport.(*http.Transport); !ok || tr.Proxy != nil {
		t.Fatal("Pod-IP scrapes never go through an environment proxy")
	}
}

// A Pod that matches the labels but is not controlled by the Deployment's
// ReplicaSet is never scraped.
func TestNotOwnedPodIsNotScraped(t *testing.T) {
	d := proxyDeployment("infra", "edge")
	owned := proxyPod(d, "edge-1", "10.0.0.1")
	stray := proxyPod(d, "stray", "10.0.0.2")
	stray.OwnerReferences = nil
	other := proxyPod(d, "other", "10.0.0.3")
	other.OwnerReferences[0].Name = "imposter-5d8f7"
	notCtl := proxyPod(d, "not-controller", "10.0.0.4")
	notCtl.OwnerReferences[0].Controller = nil
	ts, ok := proxystats.Kube{}.Targets(installOf(t, "kgw"), gwRef("infra", "edge"), snapshot([]*appsv1.Deployment{d}, owned, stray, other, notCtl))
	if !ok || len(ts) != 1 || ts[0].Name != "edge-1" {
		t.Fatalf("targets %+v", ts)
	}
}

// hangingSource never answers; every fetch ends with its context.
type hangingSource struct{ targets []proxystats.Target }

func (h hangingSource) Targets(install.Resolved, objects.Ref, *cache.Snapshot) ([]proxystats.Target, bool) {
	return h.targets, true
}
func (hangingSource) Fetch(ctx context.Context, _ proxystats.Target) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (hangingSource) Interpret(proxystats.Target, []byte) (proxystats.PodStats, error) {
	return proxystats.PodStats{}, nil
}

// With more than 96 hanging Pods (six waves of 16 past the fetch timeout),
// the round still ends before the next tick: the Gateway stays observed and
// every Pod times out.
func TestRoundEndsBeforeTheNextTick(t *testing.T) {
	var src hangingSource
	for i := range 120 {
		src.targets = append(src.targets, proxystats.Target{ID: fmt.Sprint(i), Name: fmt.Sprintf("p%03d", i), Gateway: gwRef("infra", "edge")})
	}
	in := proxystats.Install{Resolved: installOf(t, "kgw"), Gateways: []objects.Ref{gwRef("infra", "edge")}}
	// 400 ms: 200 ms per fetch, so 120 Pods alone would take 1.6 s.
	every := 400 * time.Millisecond
	c := proxystats.New(proxystats.Config{Source: src, Every: every, Installs: static(snapshot(nil), in)})
	start := time.Now()
	st, _, ok := c.Round(context.Background())
	if el := time.Since(start); !ok || el >= 2*every {
		t.Fatalf("round took %v, want under the %v interval plus slack", el, every)
	}
	g := podsOf(t, st, in.Resolved.ID(), gwRef("infra", "edge"))
	if !g.Observed || len(g.Pods) != 120 || slices.ContainsFunc(g.Pods, func(p proxystats.Pod) bool { return p.Err != "timed out" }) {
		t.Fatalf("gateway observed %v, %d Pods, first %+v", g.Observed, len(g.Pods), g.Pods[0])
	}
}

// Over the cap of 64 names the grown ones are kept first, and the Pod says
// names were dropped; a Source's long error is cut on a rune boundary.
func TestNameCapKeepsGrownNamesAndErrorsStayValid(t *testing.T) {
	gw := gwRef("infra", "edge")
	// 61 names, then 71: a60..a69 appear and zz-grown grows. A cap by name alone would drop zz-grown.
	before, after := map[string]int64{"zz-grown": 1}, map[string]int64{"zz-grown": 3}
	var want []string
	for i := range 70 {
		n := fmt.Sprintf("a%02d", i)
		if i < 60 {
			before[n] = 1
		} else {
			want = append(want, n)
		}
		after[n] = 1
	}
	want = append(want, "zz-grown")
	long := strings.Repeat("é", 100)
	src := &scriptSource{targets: map[objects.Ref][]proxystats.Target{gw: {{ID: "u1", Name: "edge-1", Gateway: gw}}},
		script: map[string][]proxystats.PodStats{"u1": {{RDS: before}, {RDS: after}, {Err: long}}}}
	in := proxystats.Install{Resolved: installOf(t, "kgw"), Gateways: []objects.Ref{gw}}
	c := proxystats.New(proxystats.Config{Source: src, Every: 10 * time.Second, Clock: testutil.FakeClock(), Installs: static(snapshot(nil), in)})
	c.Round(context.Background())
	st, _, _ := c.Round(context.Background())
	if p := podsOf(t, st, in.Resolved.ID(), gw).Pods[0]; !slices.Equal(p.RDS, want) || !p.RDSTruncated || p.ClustersTruncated {
		t.Fatalf("capped names: %+v", p)
	}
	st, _, _ = c.Round(context.Background())
	if e := podsOf(t, st, in.Resolved.ID(), gw).Pods[0].Err; !utf8.ValidString(e) || len(e) > 120 || !strings.HasPrefix(long, e) || e == "" {
		t.Fatalf("error text %q", e)
	}
}

// A failed scrape keeps the counters, so growth across it is still observed.
func TestGrowthAcrossAFailedScrape(t *testing.T) {
	gw := gwRef("infra", "edge")
	src := &scriptSource{targets: map[objects.Ref][]proxystats.Target{gw: {{ID: "u1", Name: "edge-1", Gateway: gw}}},
		script: map[string][]proxystats.PodStats{"u1": {counters(3, 8, 10, nil), {Err: "refused"}, counters(4, 8, 30, nil)}}}
	in := proxystats.Install{Resolved: installOf(t, "kgw"), Gateways: []objects.Ref{gw}}
	c := proxystats.New(proxystats.Config{Source: src, Every: 10 * time.Second, Clock: testutil.FakeClock(), Installs: static(snapshot(nil), in)})
	c.Round(context.Background())
	st, _, _ := c.Round(context.Background())
	if p := podsOf(t, st, in.Resolved.ID(), gw).Pods[0]; p.Err != "refused" || c.Tracked() != 1 {
		t.Fatalf("failed scrape: %+v, tracked %d", p, c.Tracked())
	}
	st, _, _ = c.Round(context.Background())
	if p := podsOf(t, st, in.Resolved.ID(), gw).Pods[0]; !p.LDS.Seen || p.CDS.Seen {
		t.Fatalf("growth across the failure: %+v", p)
	}
}

// A collector that stops publishing leaves a value that expires after three
// intervals and then reads Unavailable.
func TestStatsExpireAfterThreeIntervals(t *testing.T) {
	clk := tickerClock{FakeClock: testutil.FakeClock(), made: make(chan struct{}, 1)}
	store := collect.New(nil, clk.Now)
	d := proxyDeployment("infra", "edge")
	in := proxystats.Install{Resolved: installOf(t, "kgw"), Gateways: []objects.Ref{gwRef("infra", "edge")}}
	src := proxystats.Kube{Client: &http.Client{Transport: hostRT{"10.0.0.1": body("server.state: 0\n")}}}
	c := proxystats.New(proxystats.Config{Source: src, Every: 10 * time.Second, Clock: clk,
		Installs: static(snapshot([]*appsv1.Deployment{d}, proxyPod(d, "edge-1", "10.0.0.1")), in)})
	published := make(chan struct{}, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx, func(_ string, v any, fp uint64, ttl time.Duration) {
			store.Publish(proxystats.Key, v, fp, ttl)
			published <- struct{}{}
		}, func(_ string, st collect.State, r string) { store.Fail(proxystats.Key, st, r) })
	}()
	testutil.Recv(t, published, "the first round")
	cancel()
	if err := testutil.Recv(t, done, "Run to return"); err != nil {
		t.Fatal(err)
	}
	clk.Step(30*time.Second - time.Nanosecond)
	if v, _ := store.Snapshot().Get(proxystats.Key); v.State != collect.OK {
		t.Fatalf("before the TTL: %s", v.State)
	}
	clk.Step(time.Nanosecond)
	if v, _ := store.Snapshot().Get(proxystats.Key); v.State != collect.Unavailable || v.Data != nil {
		t.Fatalf("after the TTL: %+v", v)
	}
}

// pods/proxy errors are closed unread and never retried: a 500 with an
// oversized body and a 429 with Retry-After each fail the Pod once, with the code.
func TestPodProxyErrorsAreBoundedAndNotRetried(t *testing.T) {
	for _, tc := range []struct {
		code int
		hdr  string
	}{{http.StatusInternalServerError, ""}, {http.StatusTooManyRequests, "1"}} {
		t.Run(strconv.Itoa(tc.code), func(t *testing.T) {
			var hits atomic.Int32
			var path string
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				path = r.URL.Path + "?" + r.URL.RawQuery
				if tc.hdr != "" {
					w.Header().Set("Retry-After", tc.hdr)
				}
				w.WriteHeader(tc.code)
				// Far over MaxBody; the client must not read it.
				chunk := []byte(strings.Repeat("secret ", 1<<14))
				for range 64 {
					if _, err := w.Write(chunk); err != nil {
						return
					}
				}
			}))
			t.Cleanup(api.Close)
			kc, err := kubernetes.NewForConfig(&rest.Config{Host: api.URL})
			if err != nil {
				t.Fatal(err)
			}
			_, err = proxystats.Kube{Kube: kc}.Fetch(context.Background(), proxystats.Target{Name: "edge-1", Gateway: gwRef("infra", "edge")})
			if err == nil || err.Error() != "HTTP "+strconv.Itoa(tc.code) {
				t.Fatalf("want HTTP %d, got %v", tc.code, err)
			}
			if n := hits.Load(); n != 1 {
				t.Fatalf("%d requests, want 1 (no retry)", n)
			}
			want := "/api/v1/namespaces/infra/pods/http:edge-1:9091/proxy/stats?filter=" + url.QueryEscape(proxystats.StatsFilter) + "&usedonly="
			if path != want {
				t.Fatalf("path %s\nwant %s", path, want)
			}
		})
	}
}

// ParseEnvoy's own cap marks the list truncated, and only that list.
func TestParseCapMarksTheList(t *testing.T) {
	var b strings.Builder
	for i := range 4100 {
		fmt.Fprintf(&b, "http.http.rds.rc%04d.update_rejected: 1\n", i)
	}
	b.WriteString("cluster.c1.update_rejected: 1\n")
	p, err := proxystats.ParseEnvoy([]byte(b.String()))
	if err != nil || len(p.RDS) != 4096 || !p.RDSTruncated || p.ClustersTruncated || len(p.Clusters) != 1 {
		t.Fatalf("%v: %d RDS names, truncated %v/%v", err, len(p.RDS), p.RDSTruncated, p.ClustersTruncated)
	}
	gw := gwRef("infra", "edge")
	src := &scriptSource{targets: map[objects.Ref][]proxystats.Target{gw: {{ID: "u1", Name: "edge-1", Gateway: gw}}},
		script: map[string][]proxystats.PodStats{"u1": {{Clusters: map[string]int64{"c": 1}, ClustersTruncated: true}}}}
	in := proxystats.Install{Resolved: installOf(t, "kgw"), Gateways: []objects.Ref{gw}}
	st, _, _ := proxystats.New(proxystats.Config{Source: src, Every: 10 * time.Second, Installs: static(snapshot(nil), in)}).Round(context.Background())
	if q := podsOf(t, st, in.Resolved.ID(), gw).Pods[0]; !q.ClustersTruncated || q.RDSTruncated {
		t.Fatalf("source truncation: %+v", q)
	}
}
