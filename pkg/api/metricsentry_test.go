package api

import (
	"strings"
	"testing"
	"time"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
)

// Phase 5 design §7.1: a metrics entry older than its max age makes 001 and
// 024 not available with the stale wording, while the Gateway page still
// shows the controller's last snapshot.
func TestStaleMetricsKeepTheLastSnapshot(t *testing.T) {
	lm := newLiveMetrics()
	lm.set("kgw.kgateway", ctrlCapture(t, "v2.4.7-rejects-a", "kgw", "kgateway-0"), ctrlCapture(t, "v2.4.7-rejects-b", "kgw", "kgateway-1"))
	h := xdsHarness(t, setup{}, lm, nackWorld()...)
	lm.mu.Lock()
	lm.now = func() time.Time { return testutil.Now } // one scrape, never renewed
	lm.mu.Unlock()
	h.at(t, testutil.Now)
	fresh := get[GatewayDetail](t, h.srv, nackDetail, 200).DataPlane
	if fresh == nil || fresh.Snapshot == nil || !fresh.Rejecting {
		t.Fatalf("fresh data plane %+v", fresh)
	}
	stale := h.at(t, testutil.Now.Add(31*time.Second))
	want := "Controller metrics are stale. Last collected at 2026-10-17T12:00:00Z"
	if c := checkOf(stale, "xDS rejects"); c == nil || c.OK || c.Error != want {
		t.Errorf("xDS rejects %+v; want %q", c, want)
	}
	if c := checkOf(stale, "withheld config"); c == nil || c.OK || c.Unavailable != health.AvailFailing || c.Error != want {
		t.Errorf("withheld config %+v; want not available, %q", c, want)
	}
	if xdsFinding(stale, "infra", "nack") != nil {
		t.Error("a stale entry raised 001")
	}
	dp := get[GatewayDetail](t, h.srv, nackDetail, 200).DataPlane
	if dp == nil || dp.Snapshot == nil || *dp.Snapshot != *fresh.Snapshot {
		t.Fatalf("stale data plane snapshot %+v, want the last one %+v", dp, fresh.Snapshot)
	}
}

// Two installs scraped at different times: each entry goes stale on its own age.
func TestMetricsEntriesAgeApart(t *testing.T) {
	d2 := testutil.ControllerDeployment("kgw2", "kgateway2", img, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, "[]"))
	objs := world(testutil.Namespace("kgw2", nil), d2, testutil.ControllerPod(d2, "kgateway2-0", true))
	m := okMetrics("kgw.kgateway", "kgw2.kgateway2")
	late := m["kgw2.kgateway2"]
	late.CollectedAt = testutil.Now.Add(20 * time.Second)
	m["kgw2.kgateway2"] = late
	h := newHarness(t, setup{metrics: m}, objs...)
	h.clk.set(testutil.Now.Add(35 * time.Second)) // 35 s after the first scrape, 15 s after the second
	h.e.Compute()
	first := checkOf(get[HealthResponse](t, h.srv, "/api/v1/installs/kgw.kgateway/health", 200), "xDS rejects")
	second := checkOf(get[HealthResponse](t, h.srv, "/api/v1/installs/kgw2.kgateway2/health", 200), "xDS rejects")
	if first == nil || !strings.HasPrefix(first.Error, "Controller metrics are stale") || second == nil || !second.OK {
		t.Fatalf("kgw %+v, kgw2 %+v; want the first stale and the second fresh", first, second)
	}
}
