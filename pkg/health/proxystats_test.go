package health

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

const (
	listenerType = "envoy.config.listener.v3.Listener"
	endpointType = "envoy.config.endpoint.v3.ClusterLoadAssignment"
)

// statsPod is a scraped edge proxy Pod with apply set.
func statsPod(name string, apply func(*proxystats.Pod)) proxystats.Pod {
	yes := true
	p := proxystats.Pod{UID: "pod/infra/" + name, Name: name, Connected: &yes, ServerState: "live"}
	if apply != nil {
		apply(&p)
	}
	return p
}

func disconnected(p *proxystats.Pod) { no := false; p.Connected = &no }

func warming(p *proxystats.Pod) { n := int64(2); p.WarmingListeners = &n }

// collected is a proxy stats value holding gws for the healthy world's install.
func collected(at time.Time, gws ...proxystats.Gateway) collect.Snapshot {
	st := proxystats.Stats{"kgw.kgateway": gws, "other.kgateway": {{Gateway: testutil.GwRef("infra", "edge").Ref(), Observed: true,
		Pods: []proxystats.Pod{statsPod("other-0", disconnected)}}}}
	return collect.Snapshot{Values: map[string]collect.Value{proxystats.Key: {State: collect.OK, Data: st, Fingerprint: 7, At: at}}}
}

func edgeStats(pods ...proxystats.Pod) proxystats.Gateway {
	return proxystats.Gateway{Gateway: testutil.GwRef("infra", "edge").Ref(), Observed: true, Pods: pods}
}

// statsInput is the healthy world at now with edge proxies edge-0 (Ready) and edge-1 (not Ready).
func statsInput(t *testing.T, now time.Time) Input {
	t.Helper()
	in := healthyWorld().input(t)
	in.Now, in.MetricsMaxAge = now, 0
	yes, no := true, false
	edge := objects.RefOf(in.Owned.Gateways[0])
	in.Process = &ProcessData{Controller: process.Set{Observed: true}, Proxies: map[objects.Ref]process.Set{edge: {Observed: true,
		Processes: []process.Process{{ID: "pod/infra/edge-0", Name: "edge-0", Ready: &yes}, {ID: "pod/infra/edge-1", Name: "edge-1", Ready: &no}}}}}
	return in
}

func findingOn(r Report, code string) *Finding {
	for i := range r.Findings {
		if r.Findings[i].Code == code {
			return &r.Findings[i]
		}
	}
	return nil
}

func checkNamed(r Report, name string) *CheckResult {
	for i := range r.Checks {
		if r.Checks[i].Name == name {
			return &r.Checks[i]
		}
	}
	return nil
}

// 026 and 025 on a Ready proxy: none at 59 s, one per Gateway at 61 s with
// since at the onset; the not-Ready edge-1 is never named.
func TestProxyConditionsMatureAfter60s(t *testing.T) {
	cases := []struct {
		code, check, message string
		apply                func(*proxystats.Pod)
		want                 ProxyPodStats
	}{
		{CodeProxyDisconnected, "proxy connection", "Proxy lost its connection to the controller", disconnected, ProxyPodStats{Pod: "edge-0", Connected: ptr(false), ServerState: "live"}},
		{CodeProxyConfigWarming, "proxy config warming", "Proxy is Ready but some config is still loading", warming, ProxyPodStats{Pod: "edge-0", Connected: ptr(true), WarmingListeners: ptr(int64(2)), ServerState: "live"}},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			st := NewStates()
			col := collected(testutil.Now, edgeStats(statsPod("edge-0", c.apply), statsPod("edge-1", c.apply)))
			run := func(at time.Time) Report {
				return Evaluate(statsInput(t, at), Upstream(), Runtime{States: st, Collected: col})
			}
			r := run(testutil.Now)
			if f := findingOn(r, c.code); f != nil || !checkNamed(r, c.check).OK || !r.Next.Equal(testutil.Now.Add(60*time.Second)) {
				t.Fatalf("at 0 s: %+v %+v next %v", f, checkNamed(r, c.check), r.Next)
			}
			if f := findingOn(run(testutil.Now.Add(59*time.Second)), c.code); f != nil {
				t.Fatalf("at 59 s: %+v", f)
			}
			r = run(testutil.Now.Add(61 * time.Second))
			f := findingOn(r, c.code)
			if f == nil || f.Severity != SeverityWarning || f.Resource.String() != "infra/edge" || f.Message != c.message || r.State != StateWarning ||
				f.Since == nil || !f.Since.Equal(testutil.Now) {
				t.Fatalf("at 61 s: %s %+v", r.State, r.Findings)
			}
			ev, ok := f.Facts.(*ProxyStatsEvidence)
			if !ok || !reflect.DeepEqual(ev.Pods, []ProxyPodStats{c.want}) || !strings.Contains(f.Detail, "edge-0") || strings.Contains(f.Detail, "edge-1") {
				t.Fatalf("evidence %+v detail %q", f.Facts, f.Detail)
			}
			if want := []objects.Ref{{Kind: "Pod", Namespace: "infra", Name: "edge-0"}}; !reflect.DeepEqual(f.Evidence, want) {
				t.Fatalf("evidence refs %+v", f.Evidence)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

// A not-Ready proxy raises nothing; a proxy the process data does not know is
// Ready only when Envoy reports itself live. The condition clearing restarts the 60 s.
func TestProxyConditionNeedsAReadyProxy(t *testing.T) {
	st := NewStates()
	run := func(at time.Time, pods ...proxystats.Pod) Report {
		return Evaluate(statsInput(t, at), Upstream(), Runtime{States: st, Collected: collected(testutil.Now, edgeStats(pods...))})
	}
	draining := func(p *proxystats.Pod) { disconnected(p); p.ServerState = "draining" }
	run(testutil.Now, statsPod("edge-1", disconnected), statsPod("edge-9", draining), statsPod("edge-7", disconnected))
	r := run(testutil.Now.Add(61*time.Second), statsPod("edge-1", disconnected), statsPod("edge-9", draining), statsPod("edge-7", disconnected))
	f := findingOn(r, CodeProxyDisconnected)
	if f == nil || len(f.Facts.(*ProxyStatsEvidence).Pods) != 1 || f.Facts.(*ProxyStatsEvidence).Pods[0].Pod != "edge-7" {
		t.Fatalf("only the live, unknown edge-7 is Ready: %+v", r.Findings)
	}
	// edge-7 reconnects for one scrape: the next disconnect starts again.
	run(testutil.Now.Add(62*time.Second), statsPod("edge-7", nil))
	if f := findingOn(run(testutil.Now.Add(100*time.Second), statsPod("edge-7", disconnected)), CodeProxyDisconnected); f != nil {
		t.Fatalf("a fresh disconnect is not yet 60 s old: %+v", f)
	}
}

// Off, failing, expired and absent stats are not available with a reason,
// never a finding, never required. Gateways without stats are named in the result.
func TestProxyStatsAvailability(t *testing.T) {
	failing := func(p *proxystats.Pod) { *p = proxystats.Pod{UID: p.UID, Name: p.Name, Err: "timed out"} }
	cases := []struct {
		name   string
		col    collect.Snapshot
		why    Availability
		ok     bool
		detail string
	}{
		{"absent", collect.Snapshot{}, AvailFailing, false, "proxy stats not collected yet"},
		{"off", collect.Snapshot{Values: map[string]collect.Value{proxystats.Key: {State: collect.Off, Reason: proxystats.OffReason}}}, AvailOff, false, proxystats.OffReason},
		{"expired", collect.Snapshot{Values: map[string]collect.Value{proxystats.Key: {State: collect.Unavailable, Reason: "expired"}}}, AvailFailing, false, "expired"},
		{"every proxy failing", collected(testutil.Now, edgeStats(statsPod("edge-0", failing))), AvailFailing, false, "No proxy stats for 1 of 1 Gateways."},
		{"not observed", collected(testutil.Now, proxystats.Gateway{Gateway: testutil.GwRef("infra", "edge").Ref()}), AvailNotObserved, false, "No proxy stats for 1 of 1 Gateways."},
		// One proxy failing beside a scraped one: the Gateway stays available, and the Pod is named in the count.
		{"one of two failing", collected(testutil.Now, edgeStats(statsPod("edge-0", failing), statsPod("edge-1", nil))), "", true, "1 proxy failed its stats scrape."},
		{"no Gateways", collected(testutil.Now), "", true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Evaluate(statsInput(t, testutil.Now), Upstream(), Runtime{Collected: c.col})
			for _, name := range []string{"proxy connection", "proxy config warming"} {
				cr := checkNamed(r, name)
				text := cr.Error
				if c.ok {
					text = cr.Detail
				}
				if cr.OK != c.ok || cr.Required || cr.Unavailable != c.why || text != c.detail {
					t.Fatalf("%s: %+v", name, cr)
				}
			}
			if r.State != StateHealthy || len(r.Findings) != 0 {
				t.Fatalf("state %s findings %+v", r.State, r.Findings)
			}
		})
	}
}

// One proxy failing its scrape keeps its condition start, so a flaky scrape
// does not restart the 60 s; it raises nothing while failing.
func TestProxyScrapeErrorKeepsTheStart(t *testing.T) {
	st := NewStates()
	run := func(at time.Time, p proxystats.Pod) Report {
		return Evaluate(statsInput(t, at), Upstream(), Runtime{States: st, Collected: collected(testutil.Now, edgeStats(p))})
	}
	run(testutil.Now, statsPod("edge-0", disconnected))
	if f := findingOn(run(testutil.Now.Add(30*time.Second), proxystats.Pod{UID: "pod/infra/edge-0", Name: "edge-0", Err: "timed out"}), CodeProxyDisconnected); f != nil {
		t.Fatalf("a failing Pod raises nothing: %+v", f)
	}
	if f := findingOn(run(testutil.Now.Add(61*time.Second), statsPod("edge-0", disconnected)), CodeProxyDisconnected); f == nil {
		t.Fatal("the start survives a failed scrape")
	}
}

// 001 quotes each scraped Pod's observed rejects and grown Envoy names, with
// the Pods as dependencies; proxy stats alone never raise 001.
func TestXDSEvidenceFromProxyStats(t *testing.T) {
	since := testutil.Now.Add(-10 * time.Second)
	rejecting := func(p *proxystats.Pod) {
		p.LDS = proxystats.Rejects{Seen: true, Since: since}
		p.RDS, p.RDSTruncated = []string{"listener~8080"}, true
		p.Clusters = []string{"kube_shop_cart_8080"}
	}
	gw := edgeStats(statsPod("edge-0", rejecting), statsPod("edge-1", nil), proxystats.Pod{UID: "u", Name: "edge-2", Err: "timed out"})
	in := statsInput(t, testutil.Now)
	r := Evaluate(in, Upstream(), Runtime{Collected: collected(testutil.Now, gw)})
	if f := findingOn(r, CodeXDSRejected); f != nil {
		t.Fatalf("proxy stats alone raised 001: %+v", f)
	}
	in.Metrics.Metrics.Rejects[ctrlmetrics.GatewayKey{Namespace: "infra", Name: "edge"}] = 1
	f := findingOn(Evaluate(in, Upstream(), Runtime{Collected: collected(testutil.Now, gw)}), CodeXDSRejected)
	if f == nil {
		t.Fatal("no 001")
	}
	s := since.UTC()
	want := []ProxyRejects{
		{Pod: "edge-0", Types: []ProxyTypeRejects{{TypeURL: listenerType, Rejecting: true, RejectingSince: &s}, {TypeURL: clusterType},
			{TypeURL: routeType, Rejecting: true, EnvoyResources: []string{"listener~8080"}, EnvoyResourcesTruncated: true},
			{TypeURL: endpointType, Rejecting: true, EnvoyResources: []string{"kube_shop_cart_8080"}}}},
		{Pod: "edge-1", Types: []ProxyTypeRejects{{TypeURL: listenerType}, {TypeURL: clusterType}, {TypeURL: routeType}, {TypeURL: endpointType}}},
	}
	if got := xdsFacts(t, f).ProxyStats; !reflect.DeepEqual(got, want) {
		t.Fatalf("proxy stats evidence\n got %+v\nwant %+v", got, want)
	}
	pods := []objects.Ref{{Kind: "Pod", Namespace: "infra", Name: "edge-0"}, {Kind: "Pod", Namespace: "infra", Name: "edge-1"}}
	for _, p := range pods {
		if !slicesContains(f.Evidence, p) {
			t.Fatalf("evidence refs %+v lack %v", f.Evidence, p)
		}
	}
}

func slicesContains(rs []objects.Ref, r objects.Ref) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}

// Another install's disconnected proxy on a Gateway of the same name raises nothing here.
func TestProxyStatsStayInTheirInstall(t *testing.T) {
	st := NewStates()
	for _, at := range []time.Duration{0, 61 * time.Second} {
		r := Evaluate(statsInput(t, testutil.Now.Add(at)), Upstream(), Runtime{States: st, Collected: collected(testutil.Now, edgeStats(statsPod("edge-0", nil)))})
		if f := findingOn(r, CodeProxyDisconnected); f != nil {
			t.Fatalf("other install's proxy: %+v", f)
		}
	}
}

// Stats off or failing clear the starts: after the outage the condition needs 60 s again.
func TestProxyStatsOutageRestartsTheAge(t *testing.T) {
	st := NewStates()
	stats := collected(testutil.Now, edgeStats(statsPod("edge-0", disconnected)))
	off := collect.Snapshot{Values: map[string]collect.Value{proxystats.Key: {State: collect.Failing, Reason: "collector stopped"}}}
	run := func(at time.Duration, col collect.Snapshot) *Finding {
		return findingOn(Evaluate(statsInput(t, testutil.Now.Add(at)), Upstream(), Runtime{States: st, Collected: col}), CodeProxyDisconnected)
	}
	run(0, stats)
	run(30*time.Second, off)
	if f := run(61*time.Second, stats); f != nil {
		t.Fatalf("the age survived the outage: %+v", f)
	}
	if f := run(122*time.Second, stats); f == nil || !f.Since.Equal(testutil.Now.Add(61*time.Second)) {
		t.Fatalf("61 s after the outage: %+v", f)
	}
}
