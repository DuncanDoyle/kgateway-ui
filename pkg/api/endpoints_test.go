package api

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// backendWorld is world with route shop/cart sending to Service shop/cart port
// 8080, whose one EndpointSlice has one endpoint that is not ready.
func backendWorld() []runtime.Object {
	objs := world()
	for _, o := range objs {
		if u, ok := o.(*unstructured.Unstructured); ok && u.GetKind() == "HTTPRoute" && u.GetName() == "cart" {
			u.Object["spec"].(map[string]any)["rules"] = []any{map[string]any{"backendRefs": []any{map[string]any{"name": "cart", "port": int64(8080)}}}}
		}
	}
	no, name := false, "http"
	port := int32(8080)
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "cart", UID: "svc-cart"},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Ports: []corev1.ServicePort{{Name: name, Port: port}}}}
	es := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "cart-x", UID: "es-cart",
		Labels: map[string]string{discoveryv1.LabelServiceName: "cart"}}, AddressType: discoveryv1.AddressTypeIPv4,
		Ports:     []discoveryv1.EndpointPort{{Name: &name, Port: &port}},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &no}}}}
	return append(objs, svc, es)
}

// hideRefs denies the refs it holds.
type hideRefs map[objects.Ref]bool

func (h hideRefs) Authorize(_ *http.Request, ref objects.Ref) bool { return !h[ref] }

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
// Service with its evidence and the route affected; no membership source means
// the evidence says membership is not available.
func TestBackendNoReadyEndpointsAtTheAPI(t *testing.T) {
	h := newHarness(t, setup{endpoints: true}, backendWorld()...)
	f, d := endpointsDetail(t, h)
	if f.Severity != health.SeverityWarning || f.Slug != "backend-no-ready-endpoints" || f.Resource == nil || f.Resource.Ref != cartSvc ||
		f.Message != "Requests to this backend get 503" {
		t.Fatalf("finding %+v", f)
	}
	ev, ok := d.Evidence.(*health.EndpointsEvidence)
	want := &health.EndpointsEvidence{Service: "cart", Port: 8080, PortName: "http", Ready: 0, Total: 1, Slices: 1,
		Membership: &health.EndpointsMembership{Reason: membershipUnavailable}}
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

// Membership is asked for the routes' Gateways on the detail only, as
// evidence; a Gateway or Pod the caller may not view is left out.
func TestBackendMembershipOnDemand(t *testing.T) {
	edgeGw := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}
	var asked []objects.Ref
	healthy, total := int64(0), int64(1)
	member := func(_ context.Context, _ install.Resolved, _ *cache.Snapshot, svc objects.Ref, port int32, gws []objects.Ref) health.EndpointsMembership {
		if svc != cartSvc || port != 8080 {
			t.Errorf("asked for %s port %d", svc, port)
		}
		asked = append(asked, gws...)
		return health.EndpointsMembership{Available: true, Gateways: []health.GatewayMembership{
			{Gateway: edgeGw, Pod: "edge-0", Cluster: "kube_shop_cart_8080", Healthy: &healthy, Total: &total}}}
	}
	for _, hidden := range []bool{false, true} {
		asked = nil
		deny := hideRefs{}
		if hidden {
			deny[objects.Ref{Kind: "Pod", Namespace: "infra", Name: "edge-0"}] = true
		}
		h := newHarness(t, setup{endpoints: true, authz: deny, opts: []HandlerOption{WithMembership(member)}}, backendWorld()...)
		_, d := endpointsDetail(t, h)
		if !reflect.DeepEqual(asked, []objects.Ref{edgeGw}) {
			t.Fatalf("asked for %+v", asked)
		}
		m := d.Evidence.(*health.EndpointsEvidence).Membership
		if hidden {
			if m == nil || !m.Available || len(m.Gateways) != 0 {
				t.Fatalf("hidden Pod: %+v", m)
			}
			continue
		}
		if m == nil || !m.Available || len(m.Gateways) != 1 || m.Gateways[0].Pod != "edge-0" || *m.Gateways[0].Total != 1 {
			t.Fatalf("membership %+v", m)
		}
	}
}

// D-034 at the API: endpoints off gives "not available", never 008 or unknown.
func TestBackendEndpointsOffAtTheAPI(t *testing.T) {
	h := newHarness(t, setup{}, backendWorld()...)
	r := h.at(t, testutil.Now)
	c := checkOf(r, "backend endpoints")
	if c == nil || c.OK || c.Unavailable != health.AvailOff || findingCode(r, health.CodeBackendNoReadyEndpoints) != nil ||
		findingCode(r, health.CodeSignalUnavailable) != nil || r.State == health.StateUnknown {
		t.Fatalf("state %s check %+v findings %+v", r.State, c, r.Findings)
	}
}
