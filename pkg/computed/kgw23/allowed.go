package kgw23

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// allowedKinds returns the route kinds a listener accepts: the protocol
// defaults, narrowed by allowedRoutes.kinds (rules §4.2).
func allowedKinds(l objects.Listener) []objects.GroupKind {
	gk := func(kinds ...string) []objects.GroupKind {
		out := make([]objects.GroupKind, 0, len(kinds))
		for _, k := range kinds {
			out = append(out, objects.GroupKind{Group: objects.GatewayAPIGroup, Kind: k})
		}
		return out
	}
	var defaults []objects.GroupKind
	switch l.Protocol {
	case "HTTP", "HTTPS":
		defaults = gk("HTTPRoute", "GRPCRoute")
	case "TLS":
		// A TLS listener without a tls block is not Terminate.
		if l.TLS != nil && l.TLS.Mode == "Terminate" {
			defaults = gk("TLSRoute", "TCPRoute")
		} else {
			defaults = gk("TLSRoute")
		}
	case "TCP":
		defaults = gk("TCPRoute")
	case "UDP":
		return nil
	default:
		defaults = gk("HTTPRoute")
	}
	if len(l.AllowedRoutes.Kinds) == 0 {
		return defaults
	}
	var out []objects.GroupKind
	for _, d := range defaults {
		for _, k := range l.AllowedRoutes.Kinds {
			// A nil group means the Gateway API group.
			g := k.Group
			if g == "" {
				g = objects.GatewayAPIGroup
			}
			if g == d.Group && k.Kind == d.Kind {
				out = append(out, d)
				break
			}
		}
	}
	return out
}

// nsVerdict is the outcome of a listener's namespace admission check.
type nsVerdict struct {
	// OK is true when the route's namespace is admitted.
	OK bool
	// SelectorError is true when From is Selector and the selector is nil or invalid.
	SelectorError bool
	// UsesSelector is true when From is Selector.
	UsesSelector bool
	// Unknown is set when the verdict needs namespace labels that are unavailable.
	Unknown computed.UnknownReason
}

// namespaces decides whether routeNS passes the listener's
// allowedRoutes.namespaces. resourceNS is the namespace of the Gateway or
// ListenerSet that owns the listener (rules §4.2).
func namespaces(l objects.Listener, resourceNS, routeNS string, nsLabels map[string]map[string]string, labelsOK bool) nsVerdict {
	switch l.AllowedRoutes.Namespaces.From {
	case "", "Same":
		return nsVerdict{OK: routeNS == resourceNS}
	case "All":
		return nsVerdict{OK: true}
	case "Selector":
		v := nsVerdict{UsesSelector: true}
		if l.AllowedRoutes.Namespaces.Selector == nil {
			v.SelectorError = true
			return v
		}
		if !labelsOK {
			v.Unknown = computed.UnknownNamespaceLabels
			return v
		}
		sel, err := metav1.LabelSelectorAsSelector(l.AllowedRoutes.Namespaces.Selector)
		if err != nil {
			v.SelectorError = true
			return v
		}
		// A missing Namespace has no labels, so only an empty selector matches it.
		v.OK = sel.Matches(labels.Set(nsLabels[routeNS]))
		return v
	}
	return nsVerdict{}
}
