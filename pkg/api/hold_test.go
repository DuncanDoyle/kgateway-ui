package api

import (
	"context"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/scopeverify"
)

// wireCheck is check name in a raw JSON body.
func wireCheck(t *testing.T, body map[string]any, name string) map[string]any {
	t.Helper()
	cs, _ := body["checks"].([]any)
	for _, c := range cs {
		if m := c.(map[string]any); m["name"] == name {
			return m
		}
	}
	t.Fatalf("no check %q in %v", name, body["checks"])
	return nil
}

// A late route in grace: the health and diagnostics JSON carry the resource
// status hold with a UTC RFC 3339 until and a reason naming nothing; a check
// without a hold has no field; a caller who may not see the route gets the same hold.
func TestHoldOnTheWire(t *testing.T) {
	young := testutil.HTTPRoute("shop", "young", []objects.ParentRef{testutil.GwRef("infra", "edge")})
	h := newHarness(t, setup{late: []*unstructured.Unstructured{young}, zone: time.FixedZone("CET", 3600)}, world()...)
	want := map[string]any{"until": "2026-10-17T12:01:00Z", "count": float64(1), "reason": "Waiting for the controller to process resources"}
	hidden := authzSrv(t, h, deny(objects.RefOf(young)))
	for _, path := range []string{healthPath, diagPath} {
		for who, srv := range map[string]*httptest.Server{"full": h.srv, "restricted": hidden} {
			body := rawJSON(t, srv, path)
			if got := wireCheck(t, body, "resource status")["hold"]; !reflect.DeepEqual(got, want) {
				t.Fatalf("%s %s: hold %v, want %v", who, path, got, want)
			}
			if _, has := wireCheck(t, body, "watched namespaces")["hold"]; has {
				t.Fatalf("%s %s: a check without a hold has the field", who, path)
			}
		}
	}
	if r := want["reason"].(string); strings.Contains(r, "young") || strings.Contains(r, "shop") {
		t.Fatalf("reason names the route: %q", r)
	}
}

// holdSnapshot is the edge Gateway with route shop/cart to Service shop/cart
// port 8080, whose one EndpointSlice has no ready endpoint.
func holdSnapshot(t *testing.T) *cache.Snapshot {
	t.Helper()
	gw := testutil.GwRef("infra", "edge")
	route := testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller))
	route.Object["spec"].(map[string]any)["rules"] = []any{map[string]any{"backendRefs": []any{map[string]any{"name": "cart", "port": int64(8080)}}}}
	d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, 1)
	access := map[schema.GroupVersionResource]cache.Access{cache.ServicesGVR: {OK: true}, cache.EndpointSlicesGVR: {OK: true}}
	for _, k := range testutil.ServedUpstream() {
		access[k.GVR()] = cache.Access{OK: true}
	}
	return &cache.Snapshot{Objects: testutil.ObjectsByGVR(t, testutil.GatewayClass("kgateway", testutil.Controller, true), edge(1), route), Access: access,
		Namespaces:  []*corev1.Namespace{testutil.Namespace("kgw", nil), testutil.Namespace("infra", nil), testutil.Namespace("shop", nil)},
		Deployments: []*appsv1.Deployment{d, testutil.ProxyDeployment("infra", "edge", 1, 1)}, Pods: []*corev1.Pod{testutil.ControllerPod(d, "p", true)},
		Services:       []cache.ServiceFact{{Namespace: "shop", Name: "cart", Type: "ClusterIP", Ports: []cache.ServicePort{{Name: "http", Port: 8080, Target: 8080}}}},
		EndpointSlices: []cache.SliceFact{{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{{Name: "http", Port: 8080}}, Ready: []int{0}, Total: 1}},
		Synced:         true, TakenAt: testutil.Now}
}

// A running engine with unchanged inputs and no safety recompute: at the
// hold's until it recomputes by itself, and the API shows the finding, not the hold.
func TestRunningEngineDecidesAtHoldUntil(t *testing.T) {
	fc := testutil.FakeClock()
	ev := make(chan engine.LoopEvent, 1024)
	e := engine.New(engine.Config{Snapshots: largeSnap{holdSnapshot(t)}, Served: testutil.ServedUpstream(), Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour),
		Collected: served(okMetrics("kgw.kgateway"), 0, nil), Checks: health.Upstream(), Clock: fc, Now: fc.Now,
		OnLoop: func(x engine.LoopEvent) {
			select {
			case ev <- x:
			default:
				t.Errorf("loop event buffer full")
			}
		}})
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		e.Run(ctx, 0)
	}()
	t.Cleanup(func() {
		cancel()
		testutil.Recv(t, stopped, "Run to return")
	})
	await := func(want engine.LoopEventKind) {
		t.Helper()
		for testutil.Recv(t, ev, "loop event").Kind != want {
		}
	}
	await(engine.EvPublished)
	await(engine.EvArmed)
	srv := newServer(t, e, nil)
	until := testutil.Now.Add(health.EndpointsHold)
	h := get[HealthResponse](t, srv, healthPath, 200)
	if c := checkOf(h, "backend endpoints"); findingCode(h, health.CodeBackendNoReadyEndpoints) != nil || c.Hold == nil || !c.Hold.Until.Equal(until) || c.Hold.Count != 1 {
		t.Fatalf("holding: %+v %+v", c, h.Findings)
	}
	fc.SetTime(until)
	await(engine.EvPublished)
	h = get[HealthResponse](t, srv, healthPath, 200)
	if c := checkOf(h, "backend endpoints"); findingCode(h, health.CodeBackendNoReadyEndpoints) == nil || c.Hold != nil {
		t.Fatalf("at until: %+v %+v", c, h.Findings)
	}
}
