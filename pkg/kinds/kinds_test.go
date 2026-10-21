package kinds

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakediscovery "k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"
)

func discovery(lists ...*metav1.APIResourceList) *fakediscovery.FakeDiscovery {
	return &fakediscovery.FakeDiscovery{Fake: &clienttesting.Fake{Resources: lists}}
}

func list(gv string, resources ...string) *metav1.APIResourceList {
	l := &metav1.APIResourceList{GroupVersion: gv}
	for _, r := range resources {
		l.APIResources = append(l.APIResources, metav1.APIResource{Name: r})
	}
	return l
}

func TestResolvePicksFirstServedVersionAndSkipsMissing(t *testing.T) {
	descs := []Descriptor{
		{Group: "gateway.networking.k8s.io", Kind: "TLSRoute", Resource: "tlsroutes", Versions: []string{"v1", "v1alpha2"}, Namespaced: true, Category: CategoryRoute},
		{Group: "gateway.kgateway.dev", Kind: "HTTPListenerPolicy", Resource: "httplistenerpolicies", Versions: []string{"v1alpha1"}, Namespaced: true, Category: CategoryPolicy},
		{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Resource: "httproutes", Versions: []string{"v1"}, Namespaced: true, Category: CategoryRoute},
	}
	dc := discovery(
		list("gateway.networking.k8s.io/v1", "httproutes", "gateways"),
		list("gateway.networking.k8s.io/v1alpha2", "tlsroutes"),
		// gateway.kgateway.dev/v1alpha1 is served but without httplistenerpolicies (removed on main)
		list("gateway.kgateway.dev/v1alpha1", "trafficpolicies"),
	)
	got, err := Resolve(dc, descs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 served kinds, got %d: %+v", len(got), got)
	}
	if got[0].Kind != "TLSRoute" || got[0].Version != "v1alpha2" {
		t.Errorf("TLSRoute should fall back to v1alpha2, got %+v", got[0])
	}
	if got[1].Kind != "HTTPRoute" || got[1].GVR().Resource != "httproutes" {
		t.Errorf("unexpected %+v", got[1])
	}
}

func TestUpstreamDescriptorsAreComplete(t *testing.T) {
	want := map[string]Category{
		"GatewayClass": CategoryGatewayClass, "Gateway": CategoryGateway, "ListenerSet": CategoryListenerSet,
		"XListenerSet": CategoryListenerSet, "HTTPRoute": CategoryRoute, "GRPCRoute": CategoryRoute,
		"TCPRoute": CategoryRoute, "TLSRoute": CategoryRoute, "BackendTLSPolicy": CategoryPolicy,
		"TrafficPolicy": CategoryPolicy, "ListenerPolicy": CategoryPolicy, "HTTPListenerPolicy": CategoryPolicy,
		"BackendConfigPolicy": CategoryPolicy,
	}
	got := map[string]Category{}
	for _, d := range Upstream() {
		got[d.Kind] = d.Category
		if d.Resource == "secrets" {
			t.Fatal("Secrets must never be registered")
		}
	}
	for k, c := range want {
		if got[k] != c {
			t.Errorf("%s: want category %q, got %q", k, c, got[k])
		}
	}
}

// A kind listed twice is resolved once.
func TestResolveDedupesByGroupKind(t *testing.T) {
	d := Descriptor{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Resource: "httproutes", Versions: []string{"v1"}}
	got, err := Resolve(discovery(list("gateway.networking.k8s.io/v1", "httproutes")), []Descriptor{d, d})
	if err != nil || len(got) != 1 {
		t.Fatalf("want one served kind, got %+v (%v)", got, err)
	}
}

// The experimental-only kinds match kgateway v2.3.11 and v2.4.7
// pluginsdk/collections/setup.go (D-018 §3): v2.3 gates every TCPRoute, v2.4
// and later watch v1 TCPRoute always. An unknown version is conservative.
func TestExperimentalOnly(t *testing.T) {
	cases := []struct {
		group, kind, version        string
		want23, want24, wantUnknown bool
	}{
		{gwxGroup, "XListenerSet", "v1alpha1", true, true, true},
		{gwGroup, "ListenerSet", "v1", false, false, false},
		{gwGroup, "TCPRoute", "v1alpha2", true, true, true},
		{gwGroup, "TCPRoute", "v1", true, false, true},
		{gwGroup, "TLSRoute", "v1alpha2", true, true, true},
		{gwGroup, "TLSRoute", "v1alpha3", true, true, true},
		{gwGroup, "TLSRoute", "v1", false, false, false},
		{gwGroup, "HTTPRoute", "v1", false, false, false},
	}
	for _, tc := range cases {
		s := Served{Descriptor: Descriptor{Group: tc.group, Kind: tc.kind}, Version: tc.version}
		for _, c := range []struct {
			ctrl string
			want bool
		}{
			{"2.3.11", tc.want23}, {"v2.3.0", tc.want23}, {"2.3.0-beta.1", tc.want23},
			{"2.2.4", tc.want23},
			{"2.4.7", tc.want24}, {"v2.4.0", tc.want24}, {"2.5.0", tc.want24}, {"2.34.0", tc.want24}, {"3.0.0", tc.want24},
			{"", tc.wantUnknown}, {"main", tc.wantUnknown}, {"dev", tc.wantUnknown},
		} {
			if got := s.ExperimentalOnly(c.ctrl); got != c.want {
				t.Errorf("%s/%s %s on controller %q: want %v, got %v", tc.group, tc.kind, tc.version, c.ctrl, c.want, got)
			}
		}
	}
}
