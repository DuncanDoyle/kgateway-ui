package api

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/diagnose"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/proxystats"
)

const nackDetail = base + "/gateways/infra/nack"

func stepTitles(ss []NextStep) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.Title)
	}
	return out
}

// A rejecting Gateway's page carries its rejects per type, the status
// contradiction, the controller's snapshot, the three next steps and the
// objects changed shortly before the first reject.
func TestGatewayDetailDataPlaneWithRejects(t *testing.T) {
	h := diagGoldenWorld(t)
	nack := get[GatewayDetail](t, h.srv, nackDetail, 200)
	dp := nack.DataPlane
	if dp == nil || !dp.Rejecting || dp.StatusSaysHealthy || dp.EvidenceRestricted || dp.Unavailable || len(dp.Rejects) != 2 {
		t.Fatalf("nack data plane %+v", dp)
	}
	for _, r := range dp.Rejects {
		if r.Streams != 2 || !r.FirstSeen.Equal(testutil.Now.Add(10*time.Second)) || r.FirstSeen.Location() != time.UTC {
			t.Fatalf("nack reject %+v", r)
		}
	}
	if !slices.IsSortedFunc(dp.Rejects, func(a, b DataPlaneTypes) int { return strings.Compare(a.TypeURL, b.TypeURL) }) {
		t.Fatalf("rejects not in type order: %+v", dp.Rejects)
	}
	if got := stepTitles(dp.NextSteps); !slices.Equal(got, []string{diagnose.StepEnvoyReason, diagnose.StepProxyLog, diagnose.StepProxyCounters}) {
		t.Fatalf("steps %v", got)
	}
	if got := refNames(dp.RecentlyChanged); !slices.Equal(got, []string{"HTTPRoute shop/fresh-n"}) || dp.RecentlyChanged[0].Link != LinkDetail {
		t.Fatalf("recently changed %v", dp.RecentlyChanged)
	}
	if s := dp.Snapshot; s == nil || *s != (SnapshotDTO{By: "controller", Listeners: 1, Routes: 1, Clusters: 4, Endpoints: 5}) {
		t.Fatalf("snapshot %+v", s)
	}
	if !dp.ProxyStats.Available || nack.Proxies == nil {
		t.Fatalf("proxy stats %+v, proxies %+v", dp.ProxyStats, nack.Proxies)
	}
	if edge := get[GatewayDetail](t, h.srv, gwDetail, 200).DataPlane; edge == nil || !edge.Rejecting || !edge.StatusSaysHealthy || len(edge.Rejects) != 1 {
		t.Fatalf("edge data plane %+v", edge)
	}
}

// Without a reject: no rejects, steps or recently changed objects; the
// Proxies section lists the Gateway's processes; a self-managed one is not observed.
func TestGatewayDetailDataPlaneWithoutRejects(t *testing.T) {
	h := procsWorld(t, setup{})
	calm := get[GatewayDetail](t, h.srv, base+"/gateways/infra/calm", 200)
	dp := calm.DataPlane
	if dp == nil || dp.Rejecting || dp.Rejects == nil || len(dp.Rejects) != 0 || dp.NextSteps == nil || len(dp.NextSteps) != 0 ||
		dp.RecentlyChanged == nil || len(dp.RecentlyChanged) != 0 || dp.StatusSaysHealthy {
		t.Fatalf("calm data plane %+v", dp)
	}
	p := calm.Proxies
	if p == nil || !p.Observed || p.Restricted || p.Ready == nil || *p.Ready != 1 || p.Desired == nil || *p.Desired != 1 || len(p.Processes) != 1 ||
		p.Processes[0].Name != "calm-0" || p.Processes[0].Version != "2.4.7" {
		t.Fatalf("calm proxies %+v", p)
	}
	edge := get[GatewayDetail](t, h.srv, gwDetail, 200).Proxies
	if edge == nil || len(edge.Processes) != 1 || edge.Processes[0].RestartsRecent == nil || *edge.Processes[0].RestartsRecent != 1 {
		t.Fatalf("edge proxies %+v", edge)
	}
	lone := get[GatewayDetail](t, h.srv, base+"/gateways/infra/lone", 200)
	if lone.Proxies == nil || lone.Proxies.Observed || len(lone.Proxies.Processes) != 0 || lone.DataPlane.ProxyStats.Unavailable != health.AvailNotObserved {
		t.Fatalf("self-managed lone: proxies %+v, data plane %+v", lone.Proxies, lone.DataPlane)
	}
}

// Proxy stats off: the data plane reads them not available with the reason, never unknown.
func TestGatewayDetailProxyStatsOff(t *testing.T) {
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", cleanMetrics("pod/kgw/kgateway-0", "kgateway-0"))
	sh := newStatsHarness(t, setup{proxyPods: true}, lm, edgeStatsWorld()...)
	sh.src.set(gwRef("infra", "edge"), pod("edge-0", nil))
	sh.at(t, testutil.Now)
	if dp := get[GatewayDetail](t, sh.srv, gwDetail, 200).DataPlane; dp == nil || !dp.ProxyStats.Available || dp.ProxyStats.Unavailable != "" {
		t.Fatalf("on: %+v", dp)
	}
	sh.store.Fail(proxystats.Key, collect.Off, proxystats.OffReason)
	sh.harness.at(t, testutil.Now.Add(time.Second))
	d := get[GatewayDetail](t, sh.srv, gwDetail, 200)
	if d.DataPlane == nil {
		t.Fatal("off: no data plane")
	}
	if s := d.DataPlane.ProxyStats; s.Available || s.Unavailable != health.AvailOff || s.Reason != proxystats.OffReason {
		t.Fatalf("off: %+v", s)
	}
	if d.Health == health.StateUnknown || d.DataPlane.Unavailable {
		t.Fatalf("stats off turned the Gateway unknown: %s %+v", d.Health, d.DataPlane)
	}
}

// Hidden controller Pods restrict the rejects and recently changed objects
// and hide the step naming a replica; hidden proxy Pods leave the counts only.
func TestGatewayDetailRestrictedPods(t *testing.T) {
	h := diagGoldenWorld(t)
	srv := authzSrv(t, h, deny(podRef("kgw", "kgateway-0"), podRef("kgw", "kgateway-1")))
	dp := get[GatewayDetail](t, srv, nackDetail, 200).DataPlane
	if dp == nil || !dp.Rejecting || !dp.EvidenceRestricted || len(dp.Rejects) != 0 || len(dp.RecentlyChanged) != 0 {
		t.Fatalf("hidden controller Pods: %+v", dp)
	}
	if got := stepTitles(dp.NextSteps); !slices.Equal(got, []string{diagnose.StepProxyLog, diagnose.StepProxyCounters}) {
		t.Fatalf("steps %v", got)
	}
	if body := string(wire(t, srv, nackDetail)); strings.Contains(body, "kgateway-0") || strings.Contains(body, "kgateway-1") {
		t.Fatalf("hidden controller Pod names in %s", body)
	}

	p := procsWorld(t, setup{})
	hidden := authzSrv(t, p, deny(podRef("infra", "edge-0")))
	e := get[GatewayDetail](t, hidden, gwDetail, 200).Proxies
	if e == nil || !e.Observed || e.Restricted || len(e.Processes) != 0 || e.Ready == nil || *e.Ready != 1 {
		t.Fatalf("hidden edge-0: %+v", e)
	}
	if body := string(wire(t, hidden, gwDetail)); strings.Contains(body, "edge-0") {
		t.Fatalf("hidden proxy Pod name in %s", body)
	}
}

// overviewDataPlane is the Data plane column of h's overview rows, by Gateway name.
func overviewDataPlane(t *testing.T, h *harness) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, r := range get[OverviewResponse](t, h.srv, base+"/overview", 200).Gateways {
		out[r.Ref.Name] = r.DataPlane
	}
	return out
}

// The overview's Data plane column (design §6): rejects while the Gateway has
// a KGWUI-001, unknown while the xDS rejects check is not OK, otherwise ok.
func TestOverviewRowDataPlane(t *testing.T) {
	if got := overviewDataPlane(t, diagGoldenWorld(t)); got["edge"] != DataPlaneRejects || got["nack"] != DataPlaneRejects {
		t.Fatalf("rejecting world %v", got)
	}
	if got := overviewDataPlane(t, procsWorld(t, setup{})); len(got) != 3 || got["edge"] != DataPlaneOK || got["calm"] != DataPlaneOK || got["lone"] != DataPlaneOK {
		t.Fatalf("clean world %v", got)
	}
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", cleanMetrics("pod/kgw/kgateway-0", "kgateway-0"))
	lm.fail("kgw.kgateway", "scrape kgateway-1: connection refused")
	u := xdsHarness(t, setup{}, lm, nackWorld()...)
	u.at(t, testutil.Now)
	if got := overviewDataPlane(t, u); got["edge"] != DataPlaneUnknown || got["nack"] != DataPlaneUnknown {
		t.Fatalf("xDS rejects unavailable %v", got)
	}
}
