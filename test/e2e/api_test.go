//go:build e2e

// Package e2e checks the deployed UI's API against a real kgateway install
// created by test/e2e/setup.sh.
package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/overview"
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
)

var base = func() string {
	if u := os.Getenv("UI_URL"); u != "" {
		return u
	}
	return "http://127.0.0.1:18080"
}()

// get decodes path into v, retrying until it answers 200 or two minutes pass.
func get(t *testing.T, path string, v any) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	var last string // last error, or status and start of body, for the timeout message
	for {
		resp, err := http.Get(base + path)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer func() { _ = resp.Body.Close() }()
			if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
				t.Fatal(err)
			}
			return
		}
		if err != nil {
			last = err.Error()
		} else {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close()
			last = fmt.Sprintf("status %d: %s", resp.StatusCode, body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s never succeeded: %s", path, last)
		}
		time.Sleep(2 * time.Second)
	}
}

// status returns the status code of one GET of path.
func status(t *testing.T, path string) int {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// has reports a finding with code on res; a non-empty msg must match too.
func has(h api.HealthResponse, code, res string, msg ...string) bool {
	return slices.ContainsFunc(h.Findings, func(f api.FindingDTO) bool {
		return f.Code == code && f.Resource != nil && f.Resource.String() == res && (len(msg) == 0 || f.Message == msg[0])
	})
}

// tpMissingTarget is the finding expected for tp-missing-target. Only kgateway
// main reports TargetNotFound; v2.3.x and v2.4.x write no status for a policy
// whose target is missing, so after the grace period it is "not yet processed" (finding 7).
func tpMissingTarget() (code, msg string) {
	if os.Getenv("KGW_MAIN") == "1" {
		return health.CodeConditionFalse, "Policy target not found"
	}
	return health.CodeRouteUnattached, "Not yet processed by the controller"
}

func secondInstall() bool { return os.Getenv("SECOND_INSTALL") == "1" }

func TestRealCluster(t *testing.T) {
	var meta api.MetaResponse
	get(t, "/api/v1/meta", &meta)
	if meta.Platform == "" || !slices.Contains(meta.Capabilities, platform.Scope) || !slices.Contains(meta.Capabilities, platform.Workloads) {
		t.Fatalf("meta: platform=%q capabilities=%v", meta.Platform, meta.Capabilities)
	}

	var insts []api.InstallSummary
	get(t, "/api/v1/installs", &insts)
	want := 1
	if secondInstall() {
		want = 2
	}
	if len(insts) != want {
		t.Fatalf("installs: want %d got %+v", want, insts)
	}
	// Installs are sorted by ID; the ID is opaque and only displayed as namespace/name (D-016).
	if insts[0].ID != "kgw-a.kgateway" || insts[0].DisplayName != "kgw-a/kgateway" {
		t.Fatalf("install identity: %+v", insts[0])
	}
	if secondInstall() && (insts[1].ID != "kgw-b.kgateway" || insts[1].DisplayName != "kgw-b/kgateway") {
		t.Fatalf("second install identity: %+v", insts[1])
	}
	for _, i := range insts {
		if i.Readiness.State != overview.ReadinessReady {
			t.Fatalf("install %s readiness: %+v", i.ID, i.Readiness)
		}
	}

	var o api.OverviewResponse
	get(t, "/api/v1/installs/kgw-a.kgateway/overview", &o)
	counts := map[string]int{}
	for _, c := range o.Counts {
		counts[c.Kind] = c.Count
	}
	// Gateways edge and self; posts in the unlabelled blog namespace must not count.
	if counts["Gateway"] != 2 || counts["HTTPRoute"] != 2 {
		t.Fatalf("counts: %+v", counts)
	}

	// An owned object is served by install ID; another install's object is not.
	var route map[string]any
	get(t, "/api/v1/installs/kgw-a.kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/broken", &route)
	if md, _ := route["metadata"].(map[string]any); md["name"] != "broken" || md["namespace"] != "shop" {
		t.Fatalf("resource: metadata %+v", route["metadata"])
	}
	if secondInstall() {
		// Positive control: the same route serves app-b/edge under its own install.
		const gw = "/resources/gateway.networking.k8s.io/Gateway/app-b/edge"
		if code := status(t, "/api/v1/installs/kgw-b.kgateway"+gw); code != http.StatusOK {
			t.Fatalf("kgw-b's Gateway not served by kgw-b: status %d", code)
		}
		if code := status(t, "/api/v1/installs/kgw-a.kgateway"+gw); code != http.StatusNotFound {
			t.Fatalf("kgw-b's Gateway served by kgw-a: status %d", code)
		}
		var ob api.OverviewResponse
		get(t, "/api/v1/installs/kgw-b.kgateway/overview", &ob)
		countsB := map[string]int{}
		for _, c := range ob.Counts {
			countsB[c.Kind] = c.Count
		}
		if countsB["Gateway"] != 1 {
			t.Fatalf("kgw-b counts: %+v", countsB)
		}
	}

	// Findings settle once kgateway has written status and metrics were scraped.
	tpCode, tpMsg := tpMissingTarget()
	deadline := time.Now().Add(3 * time.Minute) // covers health.Grace for the policy
	for {
		var h api.HealthResponse
		get(t, "/api/v1/installs/kgw-a.kgateway/health", &h)
		ok := has(h, health.CodeConditionFalse, "shop/broken") && has(h, health.CodeNamespaceNotWatched, "blog/posts") &&
			has(h, tpCode, "shop/tp-missing-target", tpMsg) && has(h, health.CodeSelfManagedProxy, "infra/self") &&
			!has(h, health.CodeConditionFalse, "shop/good")
		xds := slices.ContainsFunc(h.Checks, func(c health.CheckResult) bool {
			return c.Name == "xDS rejects" && c.OK // in-cluster metrics scraping works
		})
		if ok && xds && h.State == health.StateError {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("health never settled: state=%s findings=%+v checks=%+v", h.State, h.Findings, h.Checks)
		}
		time.Sleep(3 * time.Second)
	}
}
