package health

import (
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

const endpointsCheckName = "backend endpoints"

var cartService = objects.Ref{Kind: "Service", Namespace: "shop", Name: "cart"}

// backendRef is a written backendRef to name on port, with extra fields.
func backendRef(name string, port int64, extra map[string]any) map[string]any {
	m := map[string]any{"name": name, "port": port}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// withRefs returns route u with one rule holding refs.
func withRefs(u *unstructured.Unstructured, refs ...map[string]any) *unstructured.Unstructured {
	bs := make([]any, 0, len(refs))
	for _, r := range refs {
		bs = append(bs, r)
	}
	u.Object["spec"].(map[string]any)["rules"] = []any{map[string]any{"backendRefs": bs}}
	return u
}

// endpointsWorld is the healthy world with route shop/cart to Service shop/cart
// port 8080 (Service port "http"), whose one slice has no ready endpoint.
type endpointsWorld struct {
	w      *world
	svcs   []cache.ServiceFact
	slices []cache.SliceFact
	access cache.Access
}

func newEndpointsWorld() *endpointsWorld {
	w := healthyWorld()
	w.objs[2] = withRefs(w.objs[2], backendRef("cart", 8080, nil))
	return &endpointsWorld{
		w:      w,
		svcs:   []cache.ServiceFact{{Namespace: "shop", Name: "cart", Type: "ClusterIP", Ports: []cache.ServicePort{{Name: "http", Port: 8080, Target: 8080}}}},
		slices: []cache.SliceFact{{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{{Name: "http", Port: 8080}}, Ready: []int{0}, Total: 2}},
		access: cache.Access{OK: true},
	}
}

func (e *endpointsWorld) input(t *testing.T, at time.Time) Input {
	t.Helper()
	in := e.w.input(t)
	in.Now = at
	in.Snapshot.Access[cache.ServicesGVR], in.Snapshot.Access[cache.EndpointSlicesGVR] = e.access, cache.Access{OK: true}
	in.Snapshot.Services, in.Snapshot.EndpointSlices = e.svcs, e.slices
	return in
}

// run evaluates e at each offset from testutil.Now on one Store and returns the last report.
func (e *endpointsWorld) run(t *testing.T, st *States, offsets ...time.Duration) Report {
	t.Helper()
	var r Report
	for _, d := range offsets {
		r = Evaluate(e.input(t, testutil.Now.Add(d)), Upstream(), freshMetrics(Runtime{States: st}))
	}
	return r
}

func endpointFindings(r Report) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Code == CodeBackendNoReadyEndpoints {
			out = append(out, f)
		}
	}
	return out
}

// §11: no ready endpoint for the Service port for 30 s raises one 023 warning
// on the Service, since the drop; at 29 s there is none.
func TestBackendNoReadyEndpointsAfterHold(t *testing.T) {
	e, st := newEndpointsWorld(), NewStates()
	r := e.run(t, st, 0)
	if fs := endpointFindings(r); len(fs) != 0 || !r.Next.Equal(testutil.Now.Add(EndpointsHold)) {
		t.Fatalf("at 0 s: %+v next %v", fs, r.Next)
	}
	if fs := endpointFindings(e.run(t, st, 29*time.Second)); len(fs) != 0 {
		t.Fatalf("at 29 s: %+v", fs)
	}
	r = e.run(t, st, 31*time.Second)
	fs := endpointFindings(r)
	if len(fs) != 1 {
		t.Fatalf("at 31 s: %+v", r.Findings)
	}
	f := fs[0]
	if f.Severity != SeverityWarning || f.Slug != "backend-no-ready-endpoints" || *f.Resource != cartService ||
		f.Message != "This backend has no ready endpoints" || f.Since == nil || !f.Since.Equal(testutil.Now) || f.ID == "" || r.State != StateWarning {
		t.Fatalf("finding %+v state %s", f, r.State)
	}
	want := &EndpointsEvidence{Service: "cart", Port: 8080, PortName: "http", Ready: 0, Total: 2, Slices: 1,
		Routes: []objects.Ref{{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "cart"}}}
	if !reflect.DeepEqual(f.Facts, want) {
		t.Fatalf("evidence %+v, want %+v", f.Facts, want)
	}
	if c := checkNamed(r, endpointsCheckName); c == nil || !c.OK || c.Required {
		t.Fatalf("check %+v", c)
	}
}

// One ready endpoint at 20 s resets the hold; it restarts at the next drop.
func TestBackendReadyEndpointResetsHold(t *testing.T) {
	e, st := newEndpointsWorld(), NewStates()
	e.run(t, st, 0)
	e.slices[0].Ready = []int{1}
	e.run(t, st, 20*time.Second)
	e.slices[0].Ready = []int{0}
	if fs := endpointFindings(e.run(t, st, 25*time.Second, 31*time.Second, 54*time.Second)); len(fs) != 0 {
		t.Fatalf("29 s after the second drop: %+v", fs)
	}
	fs := endpointFindings(e.run(t, st, 56*time.Second))
	if len(fs) != 1 || !fs[0].Since.Equal(testutil.Now.Add(25*time.Second)) {
		t.Fatalf("31 s after the second drop: %+v", fs)
	}
}

// Cases the rule leaves alone: each gives no 023, even after the hold.
func TestBackendNoReadyEndpointsExclusions(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	route := func(w *world, refs ...map[string]any) { w.objs[2] = withRefs(w.objs[2], refs...) }
	status := func(w *world, conds ...metav1.Condition) {
		u := testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: conds})
		w.objs[2] = withRefs(u, backendRef("cart", 8080, nil))
	}
	accepted := testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")
	cases := []struct {
		name   string
		mutate func(e *endpointsWorld)
	}{
		{"one ready endpoint", func(e *endpointsWorld) { e.slices[0].Ready = []int{1} }},
		{"ready in the other address family", func(e *endpointsWorld) {
			e.slices[0].AddressType = "IPv4"
			e.slices = append(e.slices, cache.SliceFact{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{{Name: "http", Port: 8080}},
				Ready: []int{1}, Total: 1, AddressType: "IPv6"})
		}},
		{"TCP port ready, UDP port of the same number empty", func(e *endpointsWorld) {
			e.svcs[0].Ports = []cache.ServicePort{{Name: "dns-udp", Port: 8080, Target: 8080, Protocol: "UDP"}, {Name: "http", Port: 8080, Target: 8080, Protocol: "TCP"}}
			e.slices[0].Ports = append(e.slices[0].Ports, cache.SlicePort{Name: "dns-udp", Port: 8080})
			e.slices[0].Ready = []int{1, 0}
		}},
		{"ready summed over slices", func(e *endpointsWorld) {
			e.slices = append(e.slices, cache.SliceFact{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{{Name: "http", Port: 8080}}, Ready: []int{1}, Total: 1})
		}},
		{"ExternalName", func(e *endpointsWorld) { e.svcs[0].Type = "ExternalName" }},
		{"Service missing", func(e *endpointsWorld) { e.svcs = nil }},
		{"port not on the Service", func(e *endpointsWorld) { route(e.w, backendRef("cart", 9999, nil)) }},
		{"weight 0", func(e *endpointsWorld) { route(e.w, backendRef("cart", 8080, map[string]any{"weight": int64(0)})) }},
		{"ResolvedRefs False", func(e *endpointsWorld) {
			status(e.w, accepted, testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound"))
		}},
		{"ResolvedRefs stale", func(e *endpointsWorld) {
			c := testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")
			status(e.w, accepted, c)
			e.w.objs[2].SetGeneration(2)
		}},
		{"ResolvedRefs from another controller", func(e *endpointsWorld) {
			u := testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, "example.com/other"))
			e.w.objs[2] = withRefs(u, backendRef("cart", 8080, nil))
		}},
		{"no ResolvedRefs", func(e *endpointsWorld) { status(e.w, accepted) }},
		{"delegation ref", func(e *endpointsWorld) {
			route(e.w, backendRef("cart", 8080, map[string]any{"group": objects.GatewayAPIGroup, "kind": "HTTPRoute"}))
		}},
		{"Backend kind", func(e *endpointsWorld) {
			route(e.w, backendRef("cart", 8080, map[string]any{"group": "gateway.kgateway.dev", "kind": "Backend"}))
		}},
		{"Hostname ref", func(e *endpointsWorld) {
			route(e.w, backendRef("cart", 8080, map[string]any{"group": "networking.istio.io", "kind": "Hostname"}))
		}},
		{"mirror target", func(e *endpointsWorld) {
			e.svcs = append(e.svcs, cache.ServiceFact{Namespace: "shop", Name: "main", Type: "ClusterIP", Ports: []cache.ServicePort{{Port: 80, Target: 80}}})
			e.slices = append(e.slices, cache.SliceFact{Namespace: "shop", Service: "main", Ports: []cache.SlicePort{{Port: 80}}, Ready: []int{1}, Total: 1})
			mirror := map[string]any{"type": "RequestMirror", "requestMirror": map[string]any{"backendRef": backendRef("cart", 8080, nil)}}
			main := backendRef("main", 80, map[string]any{"filters": []any{mirror}})
			e.w.objs[2] = withRefs(e.w.objs[2], main)
			e.w.objs[2].Object["spec"].(map[string]any)["rules"].([]any)[0].(map[string]any)["filters"] = []any{mirror}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEndpointsWorld()
			c.mutate(e)
			r := e.run(t, NewStates(), 0, 31*time.Second)
			if fs := endpointFindings(r); len(fs) != 0 {
				t.Fatalf("023 raised: %+v", fs)
			}
			if chk := checkNamed(r, endpointsCheckName); chk == nil || !chk.OK {
				t.Fatalf("check %+v", chk)
			}
		})
	}
}

// Slices list a Service's ports in different orders: ready sums per port, not per index.
func TestBackendEndpointsPortOrderAcrossSlices(t *testing.T) {
	e := newEndpointsWorld()
	http, grpc := cache.SlicePort{Name: "http", Port: 8080}, cache.SlicePort{Name: "grpc", Port: 9000}
	e.svcs[0].Ports = append(e.svcs[0].Ports, cache.ServicePort{Name: "grpc", Port: 9000, Target: 9000})
	e.slices = []cache.SliceFact{
		{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{http, grpc}, Ready: []int{1, 0}, Total: 1},
		{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{grpc, http}, Ready: []int{0, 1}, Total: 1},
	}
	e.w.objs[2] = withRefs(e.w.objs[2], backendRef("cart", 8080, nil), backendRef("cart", 9000, nil))
	fs := endpointFindings(e.run(t, NewStates(), 0, 31*time.Second))
	if len(fs) != 1 || fs[0].Facts.(*EndpointsEvidence).Port != 9000 {
		t.Fatalf("want one 023, for grpc only: %+v", fs)
	}
}

// Ports match by name: ready endpoints on another port name leave this one empty.
func TestBackendEndpointsMatchPortByName(t *testing.T) {
	e := newEndpointsWorld()
	e.svcs[0].Ports = append(e.svcs[0].Ports, cache.ServicePort{Name: "grpc", Port: 9000, Target: 9000})
	e.slices[0].Ports = append(e.slices[0].Ports, cache.SlicePort{Name: "grpc", Port: 9000})
	e.slices[0].Ready = []int{0, 3}
	if fs := endpointFindings(e.run(t, NewStates(), 0, 31*time.Second)); len(fs) != 1 || fs[0].Facts.(*EndpointsEvidence).Port != 8080 {
		t.Fatalf("findings %+v", fs)
	}
	// An unnamed single-port Service matches the slice port named "".
	e = newEndpointsWorld()
	e.svcs[0].Ports[0].Name = ""
	e.slices[0].Ports[0].Name, e.slices[0].Ready = "", []int{0}
	if fs := endpointFindings(e.run(t, NewStates(), 0, 31*time.Second)); len(fs) != 1 {
		t.Fatalf("unnamed port: %+v", fs)
	}
}

// Two routes to one Service port give one 023 naming both routes; two ports give two.
func TestBackendEndpointsOnePerServicePort(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	e := newEndpointsWorld()
	e.w.objs = append(e.w.objs, withRefs(testutil.HTTPRoute("shop", "checkout", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		backendRef("cart", 8080, nil)))
	fs := endpointFindings(e.run(t, NewStates(), 0, 31*time.Second))
	if len(fs) != 1 {
		t.Fatalf("findings %+v", fs)
	}
	routes := []objects.Ref{{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "cart"},
		{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "checkout"}}
	if got := fs[0].Facts.(*EndpointsEvidence).Routes; !reflect.DeepEqual(got, routes) {
		t.Fatalf("routes %+v", got)
	}
	e = newEndpointsWorld()
	e.svcs[0].Ports = append(e.svcs[0].Ports, cache.ServicePort{Name: "grpc", Port: 9000, Target: 9000})
	e.slices[0].Ports = append(e.slices[0].Ports, cache.SlicePort{Name: "grpc", Port: 9000})
	e.slices[0].Ready = append(e.slices[0].Ready, 0)
	e.w.objs[2] = withRefs(e.w.objs[2], backendRef("cart", 8080, nil), backendRef("cart", 9000, nil))
	fs = endpointFindings(e.run(t, NewStates(), 0, 31*time.Second))
	if len(fs) != 2 || fs[0].ID == fs[1].ID {
		t.Fatalf("two ports: %+v", fs)
	}
}

// D-034: endpoints off, denied or failing make 023 not available with the
// reason; never a KGWUI-008, never an unknown install.
func TestBackendEndpointsNotAvailable(t *testing.T) {
	cases := []struct {
		name   string
		access *cache.Access
		want   Availability
	}{
		{"off", nil, AvailOff},
		{"denied", &cache.Access{Err: "forbidden: services is forbidden", Denied: true}, AvailDenied},
		{"failing", &cache.Access{Err: "watch failed"}, AvailFailing},
		{"syncing", &cache.Access{}, AvailFailing},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEndpointsWorld()
			st := NewStates()
			var r Report
			for _, d := range []time.Duration{0, 31 * time.Second} {
				in := e.input(t, testutil.Now.Add(d))
				if c.access == nil {
					delete(in.Snapshot.Access, cache.ServicesGVR)
					delete(in.Snapshot.Access, cache.EndpointSlicesGVR)
				} else {
					in.Snapshot.Access[cache.ServicesGVR] = *c.access
				}
				r = Evaluate(in, Upstream(), freshMetrics(Runtime{States: st}))
			}
			chk := checkNamed(r, endpointsCheckName)
			if chk == nil || chk.OK || chk.Required || chk.Unavailable != c.want || chk.Error == "" {
				t.Fatalf("check %+v", chk)
			}
			if len(endpointFindings(r)) != 0 || has(r, CodeSignalUnavailable, "", "") || r.State != StateHealthy {
				t.Fatalf("state %s findings %+v", r.State, r.Findings)
			}
		})
	}
}

// A dual-stack Service has a slice per family for the same Pods: the evidence
// reports the larger family's counts, and Slices counts every slice.
func TestBackendEndpointsDualStackEvidence(t *testing.T) {
	e := newEndpointsWorld()
	e.slices[0].AddressType = "IPv4"
	e.slices = append(e.slices,
		cache.SliceFact{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{{Name: "http", Port: 8080}}, Ready: []int{0}, Total: 2, AddressType: "IPv6"},
		cache.SliceFact{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{{Name: "http", Port: 8080}}, Ready: []int{0}, Total: 1, AddressType: "IPv4"})
	fs := endpointFindings(e.run(t, NewStates(), 0, 31*time.Second))
	if len(fs) != 1 {
		t.Fatalf("findings %+v", fs)
	}
	if ev := fs[0].Facts.(*EndpointsEvidence); ev.Ready != 0 || ev.Total != 3 || ev.Slices != 3 {
		t.Fatalf("evidence %+v, want IPv4's total 3 of 3 slices", ev)
	}
}

// The ref port matches the TCP Service port when a UDP port has the same number.
func TestBackendEndpointsPreferTCPPort(t *testing.T) {
	e := newEndpointsWorld()
	e.svcs[0].Ports = []cache.ServicePort{{Name: "dns-udp", Port: 8080, Target: 8080, Protocol: "UDP"}, {Name: "http", Port: 8080, Target: 8080, Protocol: "TCP"}}
	e.slices[0].Ports = append(e.slices[0].Ports, cache.SlicePort{Name: "dns-udp", Port: 8080})
	e.slices[0].Ready = []int{0, 3}
	fs := endpointFindings(e.run(t, NewStates(), 0, 31*time.Second))
	if len(fs) != 1 || fs[0].Facts.(*EndpointsEvidence).PortName != "http" {
		t.Fatalf("findings %+v", fs)
	}
}

// PortEndpoints counts each fixture's Service port exactly as 023's evidence does.
func TestPortEndpointsMatches023(t *testing.T) {
	grpcSvc := cache.ServicePort{Name: "grpc", Port: 9000, Target: 9000}
	fixtures := map[string]func(e *endpointsWorld){
		"one port": func(*endpointsWorld) {},
		"port order across slices": func(e *endpointsWorld) {
			http, grpc := cache.SlicePort{Name: "http", Port: 8080}, cache.SlicePort{Name: "grpc", Port: 9000}
			e.svcs[0].Ports = append(e.svcs[0].Ports, grpcSvc)
			e.slices = []cache.SliceFact{
				{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{http, grpc}, Ready: []int{0, 0}, Total: 1},
				{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{grpc, http}, Ready: []int{0, 0}, Total: 1},
			}
			e.w.objs[2] = withRefs(e.w.objs[2], backendRef("cart", 8080, nil), backendRef("cart", 9000, nil))
		},
		"unnamed port": func(e *endpointsWorld) {
			e.svcs[0].Ports[0].Name = ""
			e.slices[0].Ports[0].Name = ""
		},
		"two ports": func(e *endpointsWorld) {
			e.svcs[0].Ports = append(e.svcs[0].Ports, grpcSvc)
			e.slices[0].Ports = append(e.slices[0].Ports, cache.SlicePort{Name: "grpc", Port: 9000})
			e.slices[0].Ready = append(e.slices[0].Ready, 0)
			e.w.objs[2] = withRefs(e.w.objs[2], backendRef("cart", 8080, nil), backendRef("cart", 9000, nil))
		},
		"dual stack": func(e *endpointsWorld) {
			e.slices[0].AddressType = "IPv4"
			e.slices = append(e.slices,
				cache.SliceFact{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{{Name: "http", Port: 8080}}, Ready: []int{0}, Total: 2, AddressType: "IPv6"},
				cache.SliceFact{Namespace: "shop", Service: "cart", Ports: []cache.SlicePort{{Name: "http", Port: 8080}}, Ready: []int{0}, Total: 1, AddressType: "IPv4"})
		},
		"prefer TCP": func(e *endpointsWorld) {
			e.svcs[0].Ports = []cache.ServicePort{{Name: "dns-udp", Port: 8080, Target: 8080, Protocol: "UDP"}, {Name: "http", Port: 8080, Target: 8080, Protocol: "TCP"}}
			e.slices[0].Ports = append(e.slices[0].Ports, cache.SlicePort{Name: "dns-udp", Port: 8080})
			e.slices[0].Ready = []int{0, 3}
		},
	}
	for name, mutate := range fixtures {
		t.Run(name, func(t *testing.T) {
			e := newEndpointsWorld()
			mutate(e)
			fs := endpointFindings(e.run(t, NewStates(), 0, 31*time.Second))
			if len(fs) == 0 {
				t.Fatal("no 023 finding")
			}
			slices := make([]*cache.SliceFact, len(e.slices))
			for i := range e.slices {
				slices[i] = &e.slices[i]
			}
			for _, f := range fs {
				want := *f.Facts.(*EndpointsEvidence)
				got, anyReady := PortEndpoints(&e.svcs[0], slices, want.Port)
				want.Routes = nil
				if got == nil || anyReady || !reflect.DeepEqual(*got, want) {
					t.Fatalf("port %d: got %+v (ready %v), want %+v", want.Port, got, anyReady, want)
				}
			}
		})
	}
}
