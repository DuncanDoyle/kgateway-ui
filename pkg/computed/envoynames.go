package computed

import "github.com/DuncanDoyle/kgateway-ui/pkg/objects"

// EnvoyNamer maps an Envoy resource name, such as a cluster or route config
// name from proxy stats, back to its Gateway API object (design §8.8). It is
// pinned per kgateway version and ships only with per-version fixtures.
type EnvoyNamer interface {
	Object(envoyName string) (objects.Ref, bool)
}

// EnvoyNamer returns the selected rule set's namer, or nil when it has none:
// Envoy names are then shown as Envoy names only. kgw23 and kgw24 have none.
func (s Selection) EnvoyNamer() EnvoyNamer {
	n, _ := s.Rules.(EnvoyNamer)
	return n
}
