package kgw23

import (
	"github.com/DuncanDoyle/snellius/pkg/computed/viewa"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// viewA is this set's view A: the shared rules with kgateway's route kinds.
// kgateway compares a route's kind only, not its group, so view A does too.
var viewA = viewa.Options{AllowedKinds: routeKinds, MatchKindOnly: true}

// routeKinds returns the route kinds kgateway admits on a listener's protocol,
// before allowedRoutes.kinds narrows them (rules §4.2).
func routeKinds(l objects.Listener) []objects.GroupKind {
	gwKinds := func(kinds ...string) []objects.GroupKind {
		out := make([]objects.GroupKind, 0, len(kinds))
		for _, k := range kinds {
			out = append(out, objects.GroupKind{Group: objects.GatewayAPIGroup, Kind: k})
		}
		return out
	}
	switch l.Protocol {
	case "HTTP", "HTTPS":
		return gwKinds("HTTPRoute", "GRPCRoute")
	case "TLS":
		// A TLS listener without a tls block is not Terminate.
		if l.TLS != nil && l.TLS.Mode == "Terminate" {
			return gwKinds("TLSRoute", "TCPRoute")
		}
		return gwKinds("TLSRoute")
	case "TCP":
		return gwKinds("TCPRoute")
	case "UDP":
		return nil
	}
	return gwKinds("HTTPRoute")
}
