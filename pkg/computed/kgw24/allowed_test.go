package kgw24

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

func gk(kind string) objects.GroupKind {
	return objects.GroupKind{Group: objects.GatewayAPIGroup, Kind: kind}
}

func TestAllowedKindsDefaults(t *testing.T) {
	for _, c := range []struct {
		name string
		l    objects.Listener
		want []objects.GroupKind
	}{
		{"http", objects.Listener{Protocol: "HTTP"}, []objects.GroupKind{gk("HTTPRoute"), gk("GRPCRoute")}},
		{"https", objects.Listener{Protocol: "HTTPS"}, []objects.GroupKind{gk("HTTPRoute"), gk("GRPCRoute")}},
		{"tls terminate", objects.Listener{Protocol: "TLS", TLS: &objects.ListenerTLS{Mode: "Terminate"}}, []objects.GroupKind{gk("TLSRoute"), gk("TCPRoute")}},
		{"tls passthrough", objects.Listener{Protocol: "TLS", TLS: &objects.ListenerTLS{Mode: "Passthrough"}}, []objects.GroupKind{gk("TLSRoute")}},
		{"tls without block", objects.Listener{Protocol: "TLS"}, []objects.GroupKind{gk("TLSRoute")}},
		{"tcp", objects.Listener{Protocol: "TCP"}, []objects.GroupKind{gk("TCPRoute")}},
		{"udp", objects.Listener{Protocol: "UDP"}, nil},
		{"other", objects.Listener{Protocol: "FOO"}, []objects.GroupKind{gk("HTTPRoute")}},
	} {
		if got := allowedKinds(c.l); !reflect.DeepEqual(got, c.want) && (len(got) != 0 || len(c.want) != 0) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAllowedKindsIntersection(t *testing.T) {
	https := objects.Listener{Protocol: "HTTPS"}
	for _, c := range []struct {
		name  string
		proto string
		kinds []objects.GroupKind
		want  []objects.GroupKind
	}{
		// The decoder defaults a nil group, so a real nil group arrives with the Gateway API group.
		{"defaulted group", "HTTPS", []objects.GroupKind{gk("GRPCRoute")}, []objects.GroupKind{gk("GRPCRoute")}},
		{"explicit empty group", "HTTPS", []objects.GroupKind{{Group: "", Kind: "HTTPRoute"}}, nil},
		{"explicit empty list", "HTTPS", []objects.GroupKind{}, nil},
		{"explicit group", "HTTPS", []objects.GroupKind{gk("HTTPRoute")}, []objects.GroupKind{gk("HTTPRoute")}},
		{"foreign group", "HTTPS", []objects.GroupKind{{Group: "example.com", Kind: "HTTPRoute"}}, nil},
		{"kind not in defaults", "HTTP", []objects.GroupKind{gk("TCPRoute")}, nil},
		{"requested order and duplicates kept", "HTTPS", []objects.GroupKind{gk("GRPCRoute"), gk("HTTPRoute"), gk("GRPCRoute")}, []objects.GroupKind{gk("GRPCRoute"), gk("HTTPRoute"), gk("GRPCRoute")}},
	} {
		l := https
		l.Protocol = c.proto
		l.AllowedRoutes.Kinds = c.kinds
		if got := allowedKinds(l); !reflect.DeepEqual(got, c.want) && (len(got) != 0 || len(c.want) != 0) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func sel(from string, s *metav1.LabelSelector) objects.Listener {
	return objects.Listener{AllowedRoutes: objects.AllowedRoutes{Namespaces: objects.RouteNamespaces{From: from, Selector: s}}}
}

func TestNamespacesFrom(t *testing.T) {
	web := &metav1.LabelSelector{MatchLabels: map[string]string{"team": "web"}}
	labels := map[string]map[string]string{"shop": {"team": "web"}, "other": {"team": "retail"}}
	bad := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "k", Operator: "Bogus"}}}
	dne := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "team", Operator: metav1.LabelSelectorOpDoesNotExist}}}
	notIn := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "team", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"web"}}}}
	for _, c := range []struct {
		name        string
		l           objects.Listener
		resNS, rtNS string
		labels      map[string]map[string]string
		labelsOK    bool
		want        nsVerdict
	}{
		{"same ok", sel("Same", nil), "gw", "gw", nil, true, nsVerdict{OK: true}},
		{"same other ns", sel("Same", nil), "gw", "shop", nil, true, nsVerdict{}},
		{"default is same", sel("", nil), "gw", "gw", nil, true, nsVerdict{OK: true}},
		{"listenerset ns, not gateway", sel("Same", nil), "ls", "gw", nil, true, nsVerdict{}},
		{"listenerset ns match", sel("Same", nil), "ls", "ls", nil, true, nsVerdict{OK: true}},
		{"all", sel("All", nil), "gw", "anywhere", nil, true, nsVerdict{OK: true}},
		{"selector match", sel("Selector", web), "gw", "shop", labels, true, nsVerdict{OK: true, UsesSelector: true}},
		{"selector mismatch", sel("Selector", web), "gw", "other", labels, true, nsVerdict{UsesSelector: true}},
		{"selector missing ns", sel("Selector", web), "gw", "gone", labels, true, nsVerdict{UsesSelector: true}},
		{"empty selector missing ns", sel("Selector", &metav1.LabelSelector{}), "gw", "gone", labels, true, nsVerdict{OK: true, UsesSelector: true}},
		{"nil selector", sel("Selector", nil), "gw", "shop", labels, true, nsVerdict{SelectorError: true, UsesSelector: true}},
		{"invalid selector", sel("Selector", bad), "gw", "shop", labels, true, nsVerdict{SelectorError: true, UsesSelector: true}},
		{"labels unavailable", sel("Selector", web), "gw", "shop", nil, false, nsVerdict{UsesSelector: true, Unknown: computed.UnknownNamespaceLabels}},
		{"labels unavailable not needed", sel("All", nil), "gw", "shop", nil, false, nsVerdict{OK: true}},
		{"unknown from acts as same", sel("Bogus", nil), "gw", "gw", nil, true, nsVerdict{OK: true}},
		{"unknown from other ns", sel("Bogus", nil), "gw", "shop", nil, true, nsVerdict{}},
		{"invalid selector, labels unavailable", sel("Selector", bad), "gw", "shop", nil, false, nsVerdict{SelectorError: true, UsesSelector: true}},
		{"DoesNotExist, missing ns", sel("Selector", dne), "gw", "gone", labels, true, nsVerdict{UsesSelector: true}},
		{"NotIn, missing ns", sel("Selector", notIn), "gw", "gone", labels, true, nsVerdict{UsesSelector: true}},
		{"DoesNotExist, label-less ns", sel("Selector", dne), "gw", "bare", map[string]map[string]string{"bare": {}}, true, nsVerdict{OK: true, UsesSelector: true}},
	} {
		if got := namespaces(c.l, c.resNS, c.rtNS, c.labels, c.labelsOK); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}
