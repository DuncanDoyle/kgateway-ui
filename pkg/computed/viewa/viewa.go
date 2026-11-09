// Package viewa computes view A, spec-level Gateway API attachment (D-020 (A),
// rules §4): sectionName and port, allowedRoutes namespaces and kinds, and
// hostname intersection. Rule sets call Attach with their own Options and add
// implementation behaviour, such as kgateway's listener isolation, after it.
package viewa

import "github.com/DuncanDoyle/snellius/pkg/objects"

// Options are a rule set's parameters for view A.
type Options struct {
	// AllowedKinds returns the route kinds a listener admits by its protocol,
	// before allowedRoutes.kinds narrows them. It must be set.
	AllowedKinds func(l objects.Listener) []objects.GroupKind
	// MatchKindOnly matches a route to an allowed kind by kind alone,
	// ignoring its group. Without it the group must match too.
	MatchKindOnly bool
}
