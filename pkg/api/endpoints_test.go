package api

import (
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

var notReady = false

// backendWorld is world with route shop/cart sending to Service shop/cart port
// 8080, whose one EndpointSlice has one endpoint with ready set to ready, plus extra.
func backendWorld(ready *bool, extra ...runtime.Object) []runtime.Object {
	objs := world(extra...)
	for _, o := range objs {
		if u, ok := o.(*unstructured.Unstructured); ok && u.GetKind() == "HTTPRoute" && u.GetName() == "cart" {
			u.Object["spec"].(map[string]any)["rules"] = []any{map[string]any{"backendRefs": []any{map[string]any{"name": "cart", "port": int64(8080)}}}}
		}
	}
	name := "http"
	port := int32(8080)
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "cart", UID: "svc-cart"},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Ports: []corev1.ServicePort{{Name: name, Port: port}}}}
	es := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "cart-x", UID: "es-cart",
		Labels: map[string]string{discoveryv1.LabelServiceName: "cart"}}, AddressType: discoveryv1.AddressTypeIPv4,
		Ports:     []discoveryv1.EndpointPort{{Name: &name, Port: &port}},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: ready}}}}
	return append(objs, svc, es)
}

// endpointsDetail is the 023 on Service shop/cart at 31 s past the harness
// time, and its detail.
func endpointsDetail(t *testing.T, h *harness) (FindingDTO, FindingDetail) {
	t.Helper()
	h.awaitCache(t, "Services and EndpointSlices", func(s *cache.Snapshot) bool { return len(s.Services) > 0 && len(s.EndpointSlices) > 0 })
	h.at(t, testutil.Now)
	r := h.at(t, testutil.Now.Add(health.EndpointsHold+time.Second))
	f := findingCode(r, health.CodeBackendNoReadyEndpoints)
	if f == nil {
		t.Fatalf("no 023: %s %+v %+v", r.State, r.Findings, checkOf(r, "backend endpoints"))
	}
	return *f, get[FindingDetail](t, h.srv, "/api/v1/installs/kgw.kgateway/findings/"+f.ID, 200)
}

// §11 at the API: Service and EndpointSlice objects in, a 023 warning on the
// Service with its evidence and the route affected.
func TestBackendNoReadyEndpointsAtTheAPI(t *testing.T) {
	h := newHarness(t, setup{endpoints: true}, backendWorld(&notReady)...)
	f, d := endpointsDetail(t, h)
	if f.Severity != health.SeverityWarning || f.Slug != "backend-no-ready-endpoints" || f.Resource == nil || f.Resource.Ref != cartSvc ||
		f.Message != "This backend has no ready endpoints" {
		t.Fatalf("finding %+v", f)
	}
	ev, ok := d.Evidence.(*health.EndpointsEvidence)
	want := &health.EndpointsEvidence{Service: "cart", Port: 8080, PortName: "http", Ready: 0, Total: 1, Slices: 1}
	if !ok || !reflect.DeepEqual(ev, want) {
		t.Fatalf("evidence %+v, want %+v", d.Evidence, want)
	}
	route := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "cart"}
	if len(d.Affected.Items) != 1 || d.Affected.Items[0].Ref != route {
		t.Fatalf("affected %+v", d.Affected)
	}
	if body := string(wire(t, h.srv, "/api/v1/installs/kgw.kgateway/findings/"+f.ID)); strings.Contains(body, "routes") {
		t.Fatalf("the evidence's routes reached the wire: %s", body)
	}
}

// An endpoint whose ready condition is unset counts as ready through the
// cache's transform, so it gives no 023.
func TestBackendReadyUnsetAtTheAPI(t *testing.T) {
	h := newHarness(t, setup{endpoints: true}, backendWorld(nil)...)
	h.awaitCache(t, "Services and EndpointSlices", func(s *cache.Snapshot) bool { return len(s.Services) > 0 && len(s.EndpointSlices) > 0 })
	h.at(t, testutil.Now)
	r := h.at(t, testutil.Now.Add(health.EndpointsHold+time.Second))
	if c := checkOf(r, "backend endpoints"); c == nil || !c.OK || findingCode(r, health.CodeBackendNoReadyEndpoints) != nil {
		t.Fatalf("check %+v findings %+v", c, r.Findings)
	}
}

// D-034 at the API: endpoints off gives "not available", never 008 or unknown.
func TestBackendEndpointsOffAtTheAPI(t *testing.T) {
	h := newHarness(t, setup{}, backendWorld(&notReady)...)
	r := h.at(t, testutil.Now)
	c := checkOf(r, "backend endpoints")
	if c == nil || c.OK || c.Unavailable != health.AvailOff || findingCode(r, health.CodeBackendNoReadyEndpoints) != nil ||
		findingCode(r, health.CodeSignalUnavailable) != nil || r.State == health.StateUnknown {
		t.Fatalf("state %s check %+v findings %+v", r.State, c, r.Findings)
	}
}

// afterHold is setup with endpoints on and controller metrics still fresh after the 023 hold.
func afterHold(authz Authorizer) setup {
	m := okMetrics("kgw.kgateway")
	r := m["kgw.kgateway"]
	r.CollectedAt = testutil.Now.Add(health.EndpointsHold + time.Second)
	m["kgw.kgateway"] = r
	return setup{endpoints: true, authz: authz, metrics: m}
}

// D-082 at the API: a route sending to a Service with a 023 is warning with a
// backend-problem reason, in its row and its detail; an own error stays error;
// a route that only delegates to that route stays healthy.
func TestBackendProblemAtTheAPI(t *testing.T) {
	gw, front := testutil.GwRef("infra", "edge"), testutil.HTTPRouteRef("shop", "front")
	toCart := func(path string) map[string]any {
		return delegRule([]string{path}, map[string]any{"name": "cart", "port": int64(8080)})
	}
	unresolved := testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{
		testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}}
	h := newHarness(t, afterHold(nil), backendWorld(&notReady,
		testutil.WithRules(testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, unresolved), toCart("/broken")),
		testutil.WithRules(testutil.HTTPRoute("shop", "front", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
			delegRule([]string{"/api"}, testutil.DelegateTo("shop", "child"))),
		testutil.WithRules(testutil.HTTPRoute("shop", "child", []objects.ParentRef{front}, testutil.AcceptedBy(front, testutil.Controller)), toCart("/api/child")),
	)...)
	endpointsDetail(t, h)
	rows := get[Page[RouteRow]](t, h.srv, listBase+"/routes?limit=200", 200)
	for _, name := range []string{"shop/cart", "shop/child"} {
		r := routeByName(t, rows, name)
		if r.Health != health.StateWarning || !slices.Contains(r.Flags, "backend-problem") || r.Reason == nil ||
			r.Reason.Kind != health.ReasonBackendProblem || r.Reason.Code != health.CodeBackendNoReadyEndpoints ||
			r.Reason.Message != "This backend has no ready endpoints" || r.Reason.Backend == nil || r.Reason.Backend.Ref != cartSvc {
			t.Errorf("%s: health %s flags %v reason %+v", name, r.Health, r.Flags, r.Reason)
		}
	}
	if r := routeByName(t, rows, "shop/broken"); r.Health != health.StateError || r.Reason == nil || r.Reason.Kind != health.ReasonFinding {
		t.Errorf("shop/broken: health %s reason %+v", r.Health, r.Reason)
	}
	if r := routeByName(t, rows, "shop/front"); r.Health != health.StateHealthy || slices.Contains(r.Flags, "backend-problem") || r.Reason != nil {
		t.Errorf("shop/front: health %s flags %v reason %+v", r.Health, r.Flags, r.Reason)
	}
	d := get[RouteDetail](t, h.srv, routeDetail+"shop/cart", 200)
	if d.Health != health.StateWarning || d.Reason == nil || d.Reason.Kind != health.ReasonBackendProblem || d.Reason.Backend == nil || d.Reason.Backend.Ref != cartSvc {
		t.Errorf("shop/cart detail: health %s reason %+v", d.Health, d.Reason)
	}
	if p := get[Page[RouteRow]](t, h.srv, listBase+"/routes?state=backend-problem", 200); !slices.Equal(routeNames(p), []string{"shop/broken", "shop/cart", "shop/child"}) {
		t.Errorf("state=backend-problem: %v", routeNames(p))
	}
}

// A route to a ready Service stays healthy.
func TestReadyBackendKeepsRouteHealthy(t *testing.T) {
	h := newHarness(t, afterHold(nil), backendWorld(nil)...)
	h.awaitCache(t, "Services and EndpointSlices", func(s *cache.Snapshot) bool { return len(s.Services) > 0 && len(s.EndpointSlices) > 0 })
	h.at(t, testutil.Now)
	h.at(t, testutil.Now.Add(health.EndpointsHold+time.Second))
	if r := routeByName(t, get[Page[RouteRow]](t, h.srv, listBase+"/routes", 200), "shop/cart"); r.Health != health.StateHealthy || r.Reason != nil {
		t.Errorf("shop/cart: health %s reason %+v", r.Health, r.Reason)
	}
}

// A caller who may not view the Service gets the same warning, and a reason
// with the code but without the Service or the finding's message (D-013).
func TestBackendProblemForRestrictedCaller(t *testing.T) {
	hide := AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != cartSvc })
	h := newHarness(t, afterHold(hide), backendWorld(&notReady)...)
	endpointsDetail(t, h)
	r := routeByName(t, get[Page[RouteRow]](t, h.srv, listBase+"/routes", 200), "shop/cart")
	want := RouteReason{RowReason: health.RowReason{Kind: health.ReasonBackendProblem, Code: health.CodeBackendNoReadyEndpoints}}
	if r.Health != health.StateWarning || r.Reason == nil || *r.Reason != want {
		t.Errorf("shop/cart: health %s reason %+v", r.Health, r.Reason)
	}
	d := get[RouteDetail](t, h.srv, routeDetail+"shop/cart", 200)
	if d.Health != health.StateWarning || d.Reason == nil || d.Reason.RowReason != want.RowReason || d.Reason.Backend != nil {
		t.Errorf("shop/cart detail: health %s reason %+v", d.Health, d.Reason)
	}
	if body := rawGet(t, h.srv, listBase+"/routes"); strings.Contains(body, `"backend"`) {
		t.Errorf("the hidden backend reached the wire: %s", body)
	}
}
