package engine

import (
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

type staticSnap struct{ s *cache.Snapshot }

func (s staticSnap) Snapshot() *cache.Snapshot { return s.s }

// metricsAt returns reject-free metrics collected at t.
type metricsAt time.Time

func (m metricsAt) Get(string) (ctrlmetrics.Result, bool) {
	return ctrlmetrics.Result{CollectedAt: time.Time(m), Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{}}}, true
}

// gvr returns the upstream served GVR of kind.
func gvr(t *testing.T, kind string) schema.GroupVersionResource {
	t.Helper()
	for _, s := range testutil.ServedUpstream() {
		if s.Kind == kind {
			return s.GVR()
		}
	}
	t.Fatal(kind)
	return schema.GroupVersionResource{}
}

// snapshot is a synced snapshot of one controller in kgw, a Gateway infra/edge
// with its proxy, the kgateway class and objs.
func snapshot(t *testing.T, objs ...*unstructured.Unstructured) *cache.Snapshot {
	t.Helper()
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	all := append([]*unstructured.Unstructured{testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.Gateway("infra", "edge", "kgateway", nil)}, objs...)
	access := map[schema.GroupVersionResource]cache.Access{}
	for _, k := range testutil.ServedUpstream() {
		access[k.GVR()] = cache.Access{OK: true}
	}
	return &cache.Snapshot{Objects: testutil.ObjectsByGVR(t, all...), Access: access,
		Namespaces:  []*corev1.Namespace{testutil.Namespace("kgw", nil), testutil.Namespace("infra", nil), testutil.Namespace("shop", nil)},
		Deployments: []*appsv1.Deployment{d, testutil.ProxyDeployment("infra", "edge", 1, 1)}, Pods: []*corev1.Pod{testutil.ControllerPod(d, "p", true)},
		Synced: true, TakenAt: testutil.Now}
}

// config is the upstream engine configuration over snap with a 30 s metrics age limit.
func config(snap *cache.Snapshot, m ctrlmetrics.Source, now func() time.Time) Config {
	return Config{Snapshots: staticSnap{snap}, Served: testutil.ServedUpstream(), Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour), Metrics: m,
		Checks: health.Upstream(), MetricsMaxAge: 30 * time.Second, Now: now}
}

func hasFinding(rep health.Report, code, res, detail string) bool {
	for _, f := range rep.Findings {
		if f.Code == code && (res == "" || (f.Resource != nil && f.Resource.String() == res)) && (detail == "" || f.Detail == detail) {
			return true
		}
	}
	return false
}

// Grace runs from when the engine first saw the current (uid, generation).
// An annotation-only update does not restart it; a generation bump does;
// objects present at the first computation get grace from UI start even if
// they were created long before.
func TestGraceRunsFromFirstSeenGeneration(t *testing.T) {
	route := testutil.HTTPRoute("shop", "r", []objects.ParentRef{testutil.GwRef("infra", "edge")}) // created an hour ago, no status
	now := testutil.Now
	e := New(config(snapshot(t, route), metricsAt(testutil.Now), func() time.Time { return now }))
	severity := func() health.Severity {
		t.Helper()
		for _, f := range e.Compute().Installs[0].Health.Findings {
			if f.Code == health.CodeRouteUnattached && f.Resource != nil && f.Resource.String() == "shop/r" {
				return f.Severity
			}
		}
		t.Fatal("no KGWUI-003 on shop/r")
		return ""
	}
	if got := severity(); got != health.SeverityInfo {
		t.Fatalf("present at UI start: grace runs from start, want info, got %s", got)
	}
	now = now.Add(health.Grace + time.Second)
	if got := severity(); got != health.SeverityWarning {
		t.Fatalf("after grace: want warning, got %s", got)
	}
	route.SetAnnotations(map[string]string{"touched": "yes"}) // metadata-only update: generation unchanged
	route.SetResourceVersion("2")
	if got := severity(); got != health.SeverityWarning {
		t.Fatalf("an annotation update must not restart grace, got %s", got)
	}
	route.SetGeneration(2) // spec change
	if got := severity(); got != health.SeverityInfo {
		t.Fatalf("a generation bump must restart grace, got %s", got)
	}
	now = now.Add(health.Grace + time.Second)
	if got := severity(); got != health.SeverityWarning {
		t.Fatalf("after grace for generation 2: want warning, got %s", got)
	}
}

// First-seen entries of objects that are gone are pruned, so a recreated
// object with the same uid and generation starts a new grace period.
func TestFirstSeenIsPrunedForVanishedObjects(t *testing.T) {
	route := testutil.HTTPRoute("shop", "r", []objects.ParentRef{testutil.GwRef("infra", "edge")})
	snap := snapshot(t, route)
	now := testutil.Now
	e := New(config(snap, metricsAt(testutil.Now), func() time.Time { return now }))
	e.Compute()
	routes := snap.Objects[gvr(t, "HTTPRoute")]
	delete(snap.Objects, gvr(t, "HTTPRoute"))
	now = now.Add(health.Grace + time.Second)
	e.Compute()
	if len(e.seen) != 2 { // the GatewayClass and the Gateway
		t.Fatalf("seen must only hold present objects, got %d entries", len(e.seen))
	}
	snap.Objects[gvr(t, "HTTPRoute")] = routes
	if !hasFinding(e.Compute().Installs[0].Health, health.CodeRouteUnattached, "shop/r", "") ||
		e.Current().Installs[0].Health.State != health.StateUnknown {
		t.Fatal("a reappearing object must be inside a fresh grace period")
	}
}

// MetricsMaxAge reaches both the checks and the scope verifier: a result older
// than it is unavailable, so the install is unknown, never healthy.
func TestStaleMetricsMakeInstallUnknown(t *testing.T) {
	route := testutil.HTTPRoute("shop", "cart", []objects.ParentRef{testutil.GwRef("infra", "edge")}, testutil.AcceptedBy(testutil.GwRef("infra", "edge"), testutil.Controller))
	now := func() time.Time { return testutil.Now }
	fresh := New(config(snapshot(t, route), metricsAt(testutil.Now), now)).Compute().Installs[0].Health
	if fresh.State != health.StateHealthy {
		t.Fatalf("fresh metrics: want healthy, got %s %+v", fresh.State, fresh.Findings)
	}
	rep := New(config(snapshot(t, route), metricsAt(testutil.Now.Add(-31*time.Second)), now)).Compute().Installs[0].Health
	if rep.State != health.StateUnknown || !hasFinding(rep, health.CodeSignalUnavailable, "", "") {
		t.Fatalf("stale metrics: want unknown with KGWUI-008, got %s %+v", rep.State, rep.Findings)
	}
	if !hasFinding(rep, health.CodeSignalUnavailable, "", "controller metrics are stale") {
		t.Fatalf("the scope verifier must see the same age limit: %+v", rep.Findings)
	}
}

// The checks get the same served list as the assignment: a route whose parent
// kind is unreadable is not reported as attached to a missing parent.
func TestServedReachesChecks(t *testing.T) {
	orphan := testutil.HTTPRoute("shop", "orphan", []objects.ParentRef{testutil.GwRef("infra", "gone")})
	snap := snapshot(t, orphan)
	now := func() time.Time { return testutil.Now }
	if !hasFinding(New(config(snap, metricsAt(testutil.Now), now)).Compute().Installs[0].Health, health.CodeRouteUnattached, "shop/orphan", "") {
		t.Fatal("baseline: a missing parent must be KGWUI-003")
	}
	snap.Access[gvr(t, "Gateway")] = cache.Access{Err: "forbidden"}
	if rep := New(config(snap, metricsAt(testutil.Now), now)).Compute().Installs[0].Health; hasFinding(rep, health.CodeRouteUnattached, "shop/orphan", "") {
		t.Fatalf("an unreadable parent kind must not raise KGWUI-003: %+v", rep.Findings)
	}
}

func TestPinnedKeepsOneInstall(t *testing.T) {
	snap := snapshot(t)
	d2 := testutil.ControllerDeployment("kgw2", "kgateway2", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	snap.Deployments = append(snap.Deployments, d2)
	snap.Pods = append(snap.Pods, testutil.ControllerPod(d2, "p2", true))
	cfg := config(snap, metricsAt(testutil.Now), func() time.Time { return testutil.Now })
	cfg.Pinned = "kgw2/kgateway2"
	e := New(cfg)
	m := e.Compute()
	if len(m.Installs) != 1 || m.Installs[0].Resolved.ID() != "kgw2/kgateway2" || m.Pinned != "kgw2/kgateway2" {
		t.Fatalf("pinned model: %+v", m.Installs)
	}
	// Ambiguity is still computed over every install.
	if !hasFinding(m.Installs[0].Health, health.CodeAmbiguousOwnership, "infra/edge", "") {
		t.Fatalf("pinning must not hide ambiguous ownership: %+v", m.Installs[0].Health.Findings)
	}
	if ids := e.Installs(); len(ids) != 1 || ids[0].ID() != "kgw2/kgateway2" {
		t.Fatalf("Installs: %+v", ids)
	}
}

func TestComputeAt5000RoutesUnderOneSecond(t *testing.T) {
	var objs []*unstructured.Unstructured
	var nss []*corev1.Namespace
	for n := 0; n < 200; n++ {
		ns := fmt.Sprintf("ns-%03d", n)
		nss = append(nss, testutil.Namespace(ns, nil))
		objs = append(objs, testutil.Gateway(ns, "gw", "kgateway", nil))
		gw := testutil.GwRef(ns, "gw")
		for r := 0; r < 25; r++ {
			objs = append(objs, testutil.HTTPRoute(ns, fmt.Sprintf("r-%02d", r), []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)))
		}
	}
	snap := snapshot(t, objs...)
	snap.Namespaces = append(snap.Namespaces, nss...)
	e := New(config(snap, metricsAt(testutil.Now), func() time.Time { return testutil.Now }))
	budget := time.Second
	if raceEnabled {
		budget = 20 * time.Second
	}
	start := time.Now()
	m := e.Compute()
	if el := time.Since(start); el > budget {
		t.Fatalf("Compute took %s for 5000 routes (budget %s)", el, budget)
	}
	if len(m.Installs) != 1 || len(m.Installs[0].Owned.Routes) != 5000 {
		t.Fatalf("unexpected model: %d installs", len(m.Installs))
	}
	t.Logf("Compute over 5000 routes: %s", time.Since(start))
}
