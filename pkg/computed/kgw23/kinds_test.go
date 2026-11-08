package kgw23

import (
	"reflect"
	"testing"

	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

func gk(kind string) objects.GroupKind {
	return objects.GroupKind{Group: objects.GatewayAPIGroup, Kind: kind}
}

func TestRouteKinds(t *testing.T) {
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
		if got := routeKinds(c.l); !reflect.DeepEqual(got, c.want) && (len(got) != 0 || len(c.want) != 0) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// kgateway compares a route's kind only: another group's HTTPRoute attaches.
func TestViewAMatchesKindOnly(t *testing.T) {
	gw := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"}
	rt := computed.Route{Ref: objects.Ref{Group: "example.com", Kind: "HTTPRoute", Namespace: "infra", Name: "x"},
		ParentRefs: []objects.ParentRef{{Group: gw.Group, Kind: gw.Kind, Namespace: gw.Namespace, Name: gw.Name}}, Written: make([]objects.WrittenRef, 1)}
	res := New().Attachment(computed.AttachmentInput{Routes: []computed.Route{rt},
		Resources: []computed.Resource{{Ref: gw, Listeners: []objects.Listener{{Name: "http", Protocol: "HTTP", Port: 80}}}}})
	if len(res.Outcomes) != 1 || res.Outcomes[0].Outcome != computed.OutcomeAccepted {
		t.Fatalf("outcomes %+v", res.Outcomes)
	}
}
