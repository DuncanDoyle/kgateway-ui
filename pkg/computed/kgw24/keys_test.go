package kgw24

import (
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// TestChildKeys is the 2.4 half of rules §11.3: the permitting ref as written,
// always written.
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
		name string
		refs []objects.WrittenRef
		key  computed.ReportKey
	}{
		{"empty parentRefs", nil, synth},
		{"explicit full ref", []objects.WrittenRef{full}, synth},
		{"only a port", []objects.WrittenRef{withPort}, synth},
		{"namespace omitted", []objects.WrittenRef{noNS}, computed.KeyOf(noNS)},
		{"sectionName set", []objects.WrittenRef{section}, computed.KeyOf(section)},
		{"first permitting ref wins", []objects.WrittenRef{other, section, full}, computed.KeyOf(section)},
		{"two refs differing only by port", []objects.WrittenRef{full, withPort}, synth},
	} {
		t.Run(c.name, func(t *testing.T) {
			key, writes := childKey(&objects.HTTPRouteView{WrittenRefs: c.refs}, "shop", parent)
			if key != c.key || !writes {
				t.Fatalf("got %+v %v, want %+v true", key, writes, c.key)
			}
		})
	}
	// A namespace omitted resolves to the child's own: a child in another
	// namespace does not permit, so it has no key of its own.
	if key, writes := childKey(&objects.HTTPRouteView{WrittenRefs: []objects.WrittenRef{noNS}}, "orders", parent); writes || key != synth {
		t.Fatalf("other namespace: got %+v %v", key, writes)
	}
}

// TestChildKeysThroughTraversal checks that a hostname-rejected child and an
// attached child report under the permitting ref as written (rules §11.3).
func TestChildKeysThroughTraversal(t *testing.T) {
	for _, c := range []struct {
		name, child string
		ref         objects.WrittenRef
		hosts       []string
		outcome     computed.RowOutcome
	}{
		{"namespace omitted, attached", "c1", objects.WrittenRef{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Name: "api"}, nil, computed.OutcomeAttached},
		{"namespace omitted, hostname-rejected", "c2", objects.WrittenRef{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Name: "api"}, []string{"x.example.com"}, computed.OutcomeRejectedHostnames},
		{"sectionName set", "c3", objects.WrittenRef{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "api", SectionName: "http"}, nil, computed.OutcomeAttached},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := route23("shop", "api").top().rule(at("/api"), toName("shop", c.child))
			ch := route23("shop", c.child).hosts(c.hosts...).rule(at("/api/x"), service("shop", "svc"))
			ch.r.View.WrittenRefs = []objects.WrittenRef{c.ref}
			ch.r.View.ParentRefs = []objects.ParentRef{{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "api", SectionName: c.ref.SectionName}}
			res := delegate(world(p, ch))
			r := row(t, res, p, 0, 0, ch)
			if r.Outcome != c.outcome || r.ChildKey != computed.KeyOf(c.ref) || !r.WritesEntry {
				t.Fatalf("got %s %+v %v, want %s %+v true", r.Outcome, r.ChildKey, r.WritesEntry, c.outcome, computed.KeyOf(c.ref))
			}
		})
	}
}
