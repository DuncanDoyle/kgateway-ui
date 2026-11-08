// Port of kgw24 for v2.5.0; keep in sync.

package kgw25

import (
	"reflect"
	"testing"

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
