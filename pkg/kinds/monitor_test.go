package kinds

import (
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
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

// pending reports whether sig has a notification waiting, consuming it.
func pending(sig *trigger.Signal) bool {
	select {
	case <-sig.C():
		return true
	default:
		return false
	}
}

// Refresh notifies only when the Change differs from the previous one.
func TestMonitorNotifiesOnlyOnChange(t *testing.T) {
	hr := Descriptor{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Resource: "httproutes", Versions: []string{"v1"}}
	tls := Descriptor{Group: "gateway.networking.k8s.io", Kind: "TLSRoute", Resource: "tlsroutes", Versions: []string{"v1"}}
	watched := []Served{{Descriptor: hr, Version: "v1"}, {Descriptor: tls, Version: "v1"}}
	dc := discovery(list("gateway.networking.k8s.io/v1", "httproutes"))
	m := NewMonitor(dc, []Descriptor{hr, tls}, watched)
	m.OnChange = trigger.New()

	if err := m.Refresh(); err != nil || !pending(m.OnChange) {
		t.Fatalf("a first change must notify: %v", err)
	}
	if err := m.Refresh(); err != nil || pending(m.OnChange) {
		t.Fatalf("an unchanged answer must not notify: %v", err)
	}
	dc.Resources = []*metav1.APIResourceList{list("gateway.networking.k8s.io/v1")}
	if err := m.Refresh(); err != nil || !pending(m.OnChange) {
		t.Fatalf("a further removed kind must notify: %v", err)
	}
}
