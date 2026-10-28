package kgw23

import (
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// TestChildKeys is the 2.3 half of rules §11.3 (moved here from Task 59, PF3).
func TestChildKeys(t *testing.T) {
	parent := routeRef("shop", "api")
	synth := computed.ReportKey{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "api"}
	full := objects.WrittenRef{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "api"}
	withPort, noNS, section, other := full, full, full, full
	withPort.Port, withPort.PortSet = 8080, true
	noNS.Namespace = ""
	section.SectionName = "http"
	other.Name = "web"
	for _, c := range []struct {
		name   string
		refs   []objects.WrittenRef
		writes bool
	}{
		{"empty parentRefs", nil, true},
		{"explicit full ref", []objects.WrittenRef{full}, true},
		{"only a port", []objects.WrittenRef{withPort}, true},
		{"namespace omitted", []objects.WrittenRef{noNS}, false},
		{"sectionName set", []objects.WrittenRef{section}, false},
		{"another parent only", []objects.WrittenRef{other}, false},
		{"two refs differing only by port", []objects.WrittenRef{full, withPort}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			key, writes := childKey(&objects.HTTPRouteView{WrittenRefs: c.refs}, "shop", parent)
			if key != synth || writes != c.writes {
				t.Fatalf("got %+v %v, want %+v %v", key, writes, synth, c.writes)
			}
		})
	}
}
