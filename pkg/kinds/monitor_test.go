package kinds

import (
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDiffReportsAddedAndRemovedVersions(t *testing.T) {
	hr := Served{Descriptor: Descriptor{Group: "gateway.networking.k8s.io", Resource: "httproutes"}, Version: "v1"}
	tls := Served{Descriptor: Descriptor{Group: "gateway.networking.k8s.io", Resource: "tlsroutes"}, Version: "v1alpha2"}
	tlsV1 := tls
	tlsV1.Version = "v1"
	c := Diff([]Served{hr, tls}, []Served{hr, tlsV1})
	if c.String() != "added tlsroutes.gateway.networking.k8s.io/v1; removed tlsroutes.gateway.networking.k8s.io/v1alpha2" {
		t.Fatalf("a version change is one added and one removed kind: %q", c.String())
	}
	if !Diff([]Served{hr}, []Served{hr}).Empty() {
		t.Fatal("identical sets must have no change")
	}
}

// failingDiscovery fails every call after the first n.
type failingDiscovery struct {
	DiscoveryClient
	n int
}

func (f *failingDiscovery) ServerResourcesForGroupVersion(gv string) (*metav1.APIResourceList, error) {
	if f.n <= 0 {
		return nil, errors.New("apiserver unavailable")
	}
	f.n--
	return f.DiscoveryClient.ServerResourcesForGroupVersion(gv)
}

// A failed refresh keeps the last result instead of clearing a reported change.
func TestMonitorKeepsLastChangeOnError(t *testing.T) {
	descs := []Descriptor{{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Resource: "httproutes", Versions: []string{"v1"}}}
	dc := &failingDiscovery{DiscoveryClient: discovery(list("gateway.networking.k8s.io/v1")), n: 1}
	m := NewMonitor(dc, descs, []Served{{Descriptor: descs[0], Version: "v1"}})
	if err := m.Refresh(); err != nil || m.Change().String() != "removed httproutes.gateway.networking.k8s.io/v1" {
		t.Fatalf("removal not detected: %v %q", err, m.Change().String())
	}
	if err := m.Refresh(); err == nil || m.Change().Empty() {
		t.Fatalf("a failed refresh must return its error and keep the change: %v %+v", err, m.Change())
	}
}
