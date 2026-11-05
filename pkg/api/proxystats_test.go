package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

const (
	listenerType = "envoy.config.listener.v3.Listener"
	routeType    = "envoy.config.route.v3.RouteConfiguration"
	endpointType = "envoy.config.endpoint.v3.ClusterLoadAssignment"
)

// fakeProxyStats is a proxystats.Source over raw counters per Gateway; a
// Gateway it has no entry for is not observed.
type fakeProxyStats struct {
	mu   sync.Mutex
	pods map[objects.Ref][]proxystats.PodStats
}

func (f *fakeProxyStats) set(gw objects.Ref, ps ...proxystats.PodStats) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pods[gw] = ps
}

func (f *fakeProxyStats) lookup(t proxystats.Target) (proxystats.PodStats, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.pods[t.Gateway], func(p proxystats.PodStats) bool { return p.UID == t.ID })
	if i < 0 {
		return proxystats.PodStats{}, false
	}
	return f.pods[t.Gateway][i], true
}

// Targets implements proxystats.Source.
func (f *fakeProxyStats) Targets(_ install.Resolved, gw objects.Ref, _ *cache.Snapshot) ([]proxystats.Target, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ps, ok := f.pods[gw]
	ts := make([]proxystats.Target, 0, len(ps))
	for _, p := range ps {
		ts = append(ts, proxystats.Target{ID: p.UID, Name: p.Name, Gateway: gw, Addr: p.UID})
	}
	return ts, ok
}

// Fetch implements proxystats.Source; a Pod with Err fails.
func (f *fakeProxyStats) Fetch(_ context.Context, t proxystats.Target) ([]byte, error) {
	if p, ok := f.lookup(t); ok && p.Err != "" {
		return nil, errors.New(p.Err)
	}
	return []byte(t.ID), nil
}

// Interpret implements proxystats.Source.
func (f *fakeProxyStats) Interpret(t proxystats.Target, _ []byte) (proxystats.PodStats, error) {
	p, _ := f.lookup(t)
	p.UID, p.Name, p.Gateway = t.ID, t.Name, t.Gateway
	return p, nil
}

// statsHarness is xdsHarness with the real proxy stats collector over a fake
// source; at scrapes once at the engine time and publishes before computing.
type statsHarness struct {
	*harness
	src   *fakeProxyStats
	col   *proxystats.Collector
	store *collect.Store
	fc    *clocktesting.FakeClock
}

func newStatsHarness(t *testing.T, s setup, lm *liveMetrics, objs ...runtime.Object) *statsHarness {
	t.Helper()
	sh := &statsHarness{src: &fakeProxyStats{pods: map[objects.Ref][]proxystats.PodStats{}}, fc: testutil.FakeClock()}
	sh.store = collect.New(nil, sh.fc.Now)
	s.collected = sh.store.Snapshot
	installs := func() ([]proxystats.Install, *cache.Snapshot) {
		p := sh.e.Current()
		out := make([]proxystats.Install, 0, len(p.Model.Installs))
		for _, im := range p.Model.Installs {
			in := proxystats.Install{Resolved: im.Resolved}
			for _, g := range im.Owned.Gateways {
				in.Gateways = append(in.Gateways, objects.RefOf(g))
			}
			out = append(out, in)
		}
		return out, p.Model.Snapshot
	}
	sh.col = proxystats.New(proxystats.Config{Source: sh.src, Every: 10 * time.Second, Clock: sh.fc, Installs: installs})
	sh.harness = xdsHarness(t, s, lm, objs...)
	return sh
}

// scrape runs one collector round at at and publishes it.
func (sh *statsHarness) scrape(t *testing.T, at time.Time) {
	t.Helper()
	sh.fc.SetTime(at)
	st, fp, ok := sh.col.Round(context.Background())
	if !ok {
		t.Fatal("no model for the proxy stats round")
	}
	sh.store.Publish(proxystats.Key, st, fp, 30*time.Second)
}

// at scrapes at at and returns /health computed at at.
func (sh *statsHarness) at(t *testing.T, at time.Time) HealthResponse {
	t.Helper()
	sh.scrape(t, at)
	return sh.harness.at(t, at)
}

func pod(name string, apply func(*proxystats.PodStats)) proxystats.PodStats {
	yes := true
	p := proxystats.PodStats{UID: "pod/infra/" + name, Name: name, Connected: &yes, ServerState: "live"}
	if apply != nil {
		apply(&p)
	}
	return p
}

func gwRef(ns, name string) objects.Ref {
	return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: ns, Name: name}
}

func cleanMetrics(uid, name string) ctrlmetrics.Metrics {
	return ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{}, Managed: map[ctrlmetrics.ManagedKey]float64{}, ManagedPresent: true,
		PerPod: []ctrlmetrics.PodMetrics{{UID: uid, Name: name, Active: map[ctrlmetrics.TypeKey]float64{}, Total: map[ctrlmetrics.TypeKey]float64{}}}}
}

// edgeStatsWorld has the edge proxy with Pods edge-0 (Ready) and edge-1 (not Ready).
func edgeStatsWorld(extra ...runtime.Object) []runtime.Object {
	d := edgeProxy(2, 1)
	run := corev1.ContainerStatus{State: runningSince(testutil.Old.Time)}
	return append(procWorld(nil, d, edgePod(d, "edge-0", true, run), edgePod(d, "edge-1", false, run)), extra...)
}

func findingCode(h HealthResponse, code string) *FindingDTO {
	for i := range h.Findings {
		if h.Findings[i].Code == code {
			return &h.Findings[i]
		}
	}
	return nil
}

// A Ready proxy disconnected or warming gives 026 or 025 at 61 s, none at 59 s;
// the not-Ready edge-1 is not named, and hidden Pods hide the evidence.
func TestProxyConditionsAtTheAPI(t *testing.T) {
	cases := []struct {
		code, slug, check string
		apply             func(*proxystats.PodStats)
	}{
		{health.CodeProxyDisconnected, "proxy-disconnected", "proxy connection", func(p *proxystats.PodStats) { no := false; p.Connected = &no }},
		{health.CodeProxyConfigWarming, "proxy-config-warming", "proxy config warming", func(p *proxystats.PodStats) { n := int64(1); p.WarmingClusters = &n }},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			lm := newLiveMetrics()
			lm.set("kgw.kgateway", cleanMetrics("pod/kgw/kgateway-0", "kgateway-0"))
			sh := newStatsHarness(t, setup{proxyPods: true}, lm, edgeStatsWorld()...)
			sh.src.set(gwRef("infra", "edge"), pod("edge-0", c.apply), pod("edge-1", c.apply))
			r := sh.at(t, testutil.Now)
			if f := findingCode(r, c.code); f != nil || !checkOf(r, c.check).OK {
				t.Fatalf("at 0 s: %+v %+v", f, checkOf(r, c.check))
			}
			if f := findingCode(sh.at(t, testutil.Now.Add(59*time.Second)), c.code); f != nil {
				t.Fatalf("at 59 s: %+v", f)
			}
			r = sh.at(t, testutil.Now.Add(61*time.Second))
			f := findingCode(r, c.code)
			if f == nil || f.Slug != c.slug || f.Severity != health.SeverityWarning || f.Resource == nil || f.Resource.Ref != gwRef("infra", "edge") ||
				f.Since == nil || !f.Since.Equal(testutil.Now) {
				t.Fatalf("at 61 s: %s %+v", r.State, r.Findings)
			}
			ev, ok := f.Facts.(*health.ProxyStatsEvidence)
			if !ok || len(ev.Pods) != 1 || ev.Pods[0].Pod != "edge-0" || strings.Contains(f.Detail, "edge-1") {
				t.Fatalf("evidence %+v detail %q", f.Facts, f.Detail)
			}
			if body := string(wire(t, sh.srv, healthPath)); !strings.Contains(body, `"kind": "proxy-stats"`) {
				t.Fatalf("body %s", body)
			}
		})
	}
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", cleanMetrics("pod/kgw/kgateway-0", "kgateway-0"))
	noPods := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Kind != "Pod" })
	sh := newStatsHarness(t, setup{proxyPods: true, authz: noPods}, lm, edgeStatsWorld()...)
	sh.src.set(gwRef("infra", "edge"), pod("edge-0", cases[0].apply))
	sh.at(t, testutil.Now)
	f := findingCode(sh.at(t, testutil.Now.Add(61*time.Second)), health.CodeProxyDisconnected)
	if f == nil || !f.EvidenceRestricted || f.Facts != nil || f.Detail != restrictedEvidence || f.Message == "" {
		t.Fatalf("hidden proxy Pods: %+v", f)
	}
}

// One proxy failing beside a scraped one keeps the Gateway available and is
// counted; stats off are not available, and the install is not unknown.
func TestProxyStatsGapsAtTheAPI(t *testing.T) {
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", cleanMetrics("pod/kgw/kgateway-0", "kgateway-0"))
	sh := newStatsHarness(t, setup{proxyPods: true}, lm, edgeStatsWorld()...)
	sh.src.set(gwRef("infra", "edge"), pod("edge-0", nil), proxystats.PodStats{UID: "pod/infra/edge-1", Name: "edge-1", Err: "connection refused"})
	r := sh.at(t, testutil.Now)
	if c := checkOf(r, "proxy connection"); !c.OK || c.Detail != "Could not read stats from 1 proxy." || r.State == health.StateUnknown {
		t.Fatalf("%s %+v", r.State, c)
	}
	sh.store.Fail(proxystats.Key, collect.Off, proxystats.OffReason)
	r = sh.harness.at(t, testutil.Now.Add(time.Second))
	if c := checkOf(r, "proxy connection"); c.OK || c.Required || c.Unavailable != health.AvailOff || r.State == health.StateUnknown {
		t.Fatalf("off: %s %+v", r.State, c)
	}
}

// rejectsCounters sets the LDS pair and the grown RDS and EDS names' counters.
func rejectsCounters(lds proxystats.Pair, rds, eds int64) func(*proxystats.PodStats) {
	return func(p *proxystats.PodStats) {
		p.LDS = lds
		p.RDS = map[string]int64{"listener~8080": rds}
		p.Clusters = map[string]int64{"kube_shop_cart_8080": eds}
	}
}

func proxyTypes(t *testing.T, f *FindingDTO, pod string) map[string]health.ProxyTypeRejects {
	t.Helper()
	if f == nil {
		t.Fatal("no 001")
	}
	for _, p := range f.Facts.(*health.XDSRejectsEvidence).ProxyStats {
		if p.Pod == pod {
			out := map[string]health.ProxyTypeRejects{}
			for _, ty := range p.Types {
				out[ty.TypeURL] = ty
			}
			return out
		}
	}
	t.Fatalf("no proxy stats for %s in %+v", pod, f.Facts)
	return nil
}

// LDS (3,8) → (4,9) reads as rejects observed since the last scrape, with the
// grown names; no growth reads as none, and 001 keeps its id and since.
func TestXDSProxyStatsEvidenceAtTheAPI(t *testing.T) {
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", ctrlCapture(t, "v2.4.7-rejects-a", "kgw", "kgateway-0"), ctrlCapture(t, "v2.4.7-rejects-b", "kgw", "kgateway-1"))
	sh := newStatsHarness(t, setup{}, lm, nackWorld()...)
	nack := gwRef("infra", "nack")
	sh.src.set(nack, pod("nack-0", rejectsCounters(proxystats.Pair{Rejected: 3, Success: 8}, 4, 1)), pod("nack-1", nil))
	first := xdsFinding(sh.at(t, testutil.Now), "infra", "nack")
	if ty := proxyTypes(t, first, "nack-0"); ty[listenerType].Rejecting || ty[routeType].Rejecting {
		t.Fatalf("a first scrape is a baseline: %+v", ty)
	}

	t1 := testutil.Now.Add(10 * time.Second)
	sh.src.set(nack, pod("nack-0", rejectsCounters(proxystats.Pair{Rejected: 4, Success: 9}, 5, 2)), pod("nack-1", nil))
	f := xdsFinding(sh.at(t, t1), "infra", "nack")
	ty := proxyTypes(t, f, "nack-0")
	if l := ty[listenerType]; !l.Rejecting || l.RejectingSince == nil || !l.RejectingSince.Equal(t1) {
		t.Fatalf("listener %+v", l)
	}
	if r, e := ty[routeType], ty[endpointType]; !slices.Equal(r.EnvoyResources, []string{"listener~8080"}) || !slices.Equal(e.EnvoyResources, []string{"kube_shop_cart_8080"}) {
		t.Fatalf("names %+v %+v", r, e)
	}
	if other := proxyTypes(t, f, "nack-1"); other[listenerType].Rejecting {
		t.Fatalf("nack-1 saw no rejects: %+v", other)
	}
	if f.ID != first.ID || f.Since == nil || !f.Since.Equal(testutil.Now) {
		t.Fatalf("001 %s since %v, want %s since %v", f.ID, f.Since, first.ID, testutil.Now)
	}
	body := string(wire(t, sh.srv, healthPath))
	for _, want := range []string{`"proxyStats"`, `"rejecting": true`, `"rejectingSince"`, `"envoyResources"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body lacks %s: %s", want, body)
		}
	}

	f = xdsFinding(sh.at(t, t1.Add(10*time.Second)), "infra", "nack")
	if ty := proxyTypes(t, f, "nack-0"); ty[listenerType].Rejecting || ty[routeType].Rejecting || f.ID != first.ID || !f.Since.Equal(testutil.Now) {
		t.Fatalf("no growth since the last scrape: %+v, 001 %+v", ty, f)
	}

	noPods := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref.Kind != "Pod" || ref.Namespace != "infra" })
	lm2 := newLiveMetrics()
	lm2.set("kgw.kgateway", ctrlCapture(t, "v2.4.7-rejects-a", "kgw", "kgateway-0"), ctrlCapture(t, "v2.4.7-rejects-b", "kgw", "kgateway-1"))
	hidden := newStatsHarness(t, setup{authz: noPods}, lm2, nackWorld()...)
	hidden.src.set(nack, pod("nack-0", nil))
	if f := xdsFinding(hidden.at(t, testutil.Now), "infra", "nack"); f == nil || !f.EvidenceRestricted || f.Facts != nil {
		t.Fatalf("hidden proxy Pods: %+v", f)
	}
}

// Proxy stats with rejects never raise 001: with controller metrics failing
// xDS rejects stays unavailable (KGWUI-008), with clean metrics it is OK.
func TestNoRejectFindingFromProxyStatsAlone(t *testing.T) {
	for _, failing := range []bool{true, false} {
		lm := newLiveMetrics()
		lm.set("kgw.kgateway", cleanMetrics("pod/kgw/kgateway-0", "kgateway-0"))
		if failing {
			lm.fail("kgw.kgateway", "scrape kgateway-0: connection refused")
		}
		sh := newStatsHarness(t, setup{}, lm, nackWorld()...)
		nack := gwRef("infra", "nack")
		sh.src.set(nack, pod("nack-0", rejectsCounters(proxystats.Pair{Rejected: 3}, 1, 1)))
		sh.at(t, testutil.Now)
		sh.src.set(nack, pod("nack-0", rejectsCounters(proxystats.Pair{Rejected: 9}, 7, 7)))
		r := sh.at(t, testutil.Now.Add(10*time.Second))
		if xdsFinding(r, "infra", "nack") != nil {
			t.Fatalf("failing %v: proxy stats raised 001: %+v", failing, r.Findings)
		}
		c := checkOf(r, "xDS rejects")
		if failing && (c.OK || !c.Required || r.State != health.StateUnknown || findingOf(r, health.CodeSignalUnavailable) == nil) {
			t.Fatalf("metrics failing: %s %+v", r.State, c)
		}
		if !failing && !c.OK {
			t.Fatalf("metrics clean: %+v", c)
		}
	}
}

// A new UID, even with non-zero first counters, is neither a reject nor a
// recovery: no rejects read, and 001 keeps its id and since.
func TestProxyStatsResetIsNotAReject(t *testing.T) {
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", ctrlCapture(t, "v2.4.7-rejects-a", "kgw", "kgateway-0"), ctrlCapture(t, "v2.4.7-rejects-b", "kgw", "kgateway-1"))
	sh := newStatsHarness(t, setup{}, lm, nackWorld()...)
	nack := gwRef("infra", "nack")
	sh.src.set(nack, pod("nack-a", rejectsCounters(proxystats.Pair{Rejected: 3, Success: 8}, 4, 1)))
	first := xdsFinding(sh.at(t, testutil.Now), "infra", "nack")
	sh.src.set(nack, pod("nack-a", rejectsCounters(proxystats.Pair{Rejected: 4, Success: 9}, 5, 2)))
	if ty := proxyTypes(t, xdsFinding(sh.at(t, testutil.Now.Add(10*time.Second)), "infra", "nack"), "nack-a"); !ty[listenerType].Rejecting {
		t.Fatalf("nack-a grew: %+v", ty)
	}
	for _, step := range []time.Duration{20 * time.Second, 30 * time.Second} {
		sh.src.set(nack, pod("nack-b", rejectsCounters(proxystats.Pair{Rejected: 6, Success: 2}, 9, 3)))
		f := xdsFinding(sh.at(t, testutil.Now.Add(step)), "infra", "nack")
		ty := proxyTypes(t, f, "nack-b")
		for url, x := range ty {
			if x.Rejecting || len(x.EnvoyResources) > 0 {
				t.Fatalf("at %v the new UID reads as a reject on %s: %+v", step, url, x)
			}
		}
		if f.ID != first.ID || !f.Since.Equal(testutil.Now) || len(f.Facts.(*health.XDSRejectsEvidence).ProxyStats) != 1 {
			t.Fatalf("at %v: 001 %+v", step, f)
		}
	}
}

// A second install's disconnected proxy raises 026 only on that install.
func TestProxyStatsPerInstallAtTheAPI(t *testing.T) {
	d2 := testutil.ControllerDeployment("kgw2", "kgateway2", img, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"b"}}]`))
	objs := edgeStatsWorld(testutil.Namespace("kgw2", nil), testutil.Namespace("infra-b", map[string]string{"team": "b"}), d2,
		testutil.ControllerPod(d2, "kgateway2-0", true), testutil.Gateway("infra-b", "edge", "kgateway", nil), testutil.ProxyDeployment("infra-b", "edge", 1, 1))
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", cleanMetrics("pod/kgw/kgateway-0", "kgateway-0"))
	lm.set("kgw2.kgateway2", cleanMetrics("pod/kgw2/kgateway2-0", "kgateway2-0"))
	sh := newStatsHarness(t, setup{proxyPods: true}, lm, objs...)
	no := false
	sh.src.set(gwRef("infra", "edge"), pod("edge-0", nil))
	sh.src.set(gwRef("infra-b", "edge"), proxystats.PodStats{UID: "pod/infra-b/edge-0", Name: "edge-0", Connected: &no, ServerState: "live"})
	sh.at(t, testutil.Now)
	if r := sh.at(t, testutil.Now.Add(61*time.Second)); findingCode(r, health.CodeProxyDisconnected) != nil {
		t.Fatalf("first install: %+v", r.Findings)
	}
	r := get[HealthResponse](t, sh.srv, "/api/v1/installs/kgw2.kgateway2/health", 200)
	if f := findingCode(r, health.CodeProxyDisconnected); f == nil || f.Resource.Ref != gwRef("infra-b", "edge") {
		t.Fatalf("second install: %+v", r.Findings)
	}
}

// A Gateway whose every proxy failed to scrape reads failing on its own page,
// while a scraped Gateway beside it stays available (§8.8, per Gateway).
func TestProxyStatsAvailabilityPerGateway(t *testing.T) {
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", cleanMetrics("pod/kgw/kgateway-0", "kgateway-0"))
	sh := newStatsHarness(t, setup{proxyPods: true}, lm, edgeStatsWorld(testutil.Gateway("infra", "calm", "kgateway", nil))...)
	sh.src.set(gwRef("infra", "edge"), proxystats.PodStats{UID: "pod/infra/edge-0", Name: "edge-0", Err: "connection refused"},
		proxystats.PodStats{UID: "pod/infra/edge-1", Name: "edge-1", Err: "connection refused"})
	sh.src.set(gwRef("infra", "calm"), pod("calm-0", nil))
	sh.at(t, testutil.Now)
	edge := get[GatewayDetail](t, sh.srv, gwDetail, 200).DataPlane
	if edge == nil || edge.ProxyStats.Available || edge.ProxyStats.Unavailable != health.AvailFailing || edge.ProxyStats.Reason != "Could not read stats from 2 proxies." {
		t.Fatalf("edge, every proxy failing: %+v", edge)
	}
	if calm := get[GatewayDetail](t, sh.srv, base+"/gateways/infra/calm", 200).DataPlane; calm == nil || !calm.ProxyStats.Available {
		t.Fatalf("calm, scraped: %+v", calm)
	}
}
