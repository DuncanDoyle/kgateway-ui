package kgw23

import (
	"slices"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// delegationLabel is the label a label-mode ref selects on (api/labels/delegation.go:7).
const delegationLabel = "delegation.kgateway.dev/label"

// nsLabel keys routes by namespace and delegation label value.
type nsLabel struct{ ns, value string }

// selector finds the children of a delegation ref (rules §5.2), mirroring
// kgateway's route index by namespace, (namespace, label) and label
// (pkg/krtcollections/policy.go V3:1183-1200). It is built once per recompute.
type selector struct {
	routes    []computed.DelegRoute
	byNS      map[string][]int
	byNSLabel map[nsLabel][]int
	byLabel   map[string][]int
	unwatched map[objects.Ref]bool
	// wildcard is the namespace whose label refs select everywhere; known is
	// false when the setting cannot be read.
	wildcard string
	known    bool
}

// newSelector indexes routes, which are sorted by (namespace, name), so every
// index list keeps that order. unwatched are the HTTPRoutes outside the scope.
func newSelector(routes []computed.DelegRoute, unwatched map[objects.Ref]bool, wildcard install.Setting) *selector {
	s := &selector{routes: routes, byNS: map[string][]int{}, byNSLabel: map[nsLabel][]int{}, byLabel: map[string][]int{},
		unwatched: unwatched, wildcard: wildcard.Value, known: wildcard.Known}
	if s.known && s.wildcard == "" {
		s.wildcard = install.WildcardAll // an empty value means all (envutils GetOrDefault, allowEmpty false)
	}
	for i, r := range routes {
		s.byNS[r.Ref.Namespace] = append(s.byNS[r.Ref.Namespace], i)
		if v, ok := r.View.Labels[delegationLabel]; ok {
			s.byNSLabel[nsLabel{r.Ref.Namespace, v}] = append(s.byNSLabel[nsLabel{r.Ref.Namespace, v}], i)
			s.byLabel[v] = append(s.byLabel[v], i)
		}
	}
	return s
}

// fetch returns the routes ref selects, sorted by (namespace, name), as
// fetchRoutesByRef does (pkg/kgateway/query/route.go V3:275-321). An empty
// selection is unresolved. With the wildcard setting unknown, a label ref
// selects nothing and is setting-unknown; possible then lists every route with
// that label value, as uncertain points only (rules §3.3).
func (s *selector) fetch(ref objects.DelegationRef) (children, possible []objects.Ref, unresolved computed.Unresolved, unknown computed.UnknownReason) {
	var idx []int
	switch ref.Mode {
	case objects.DelegationName:
		target := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: ref.Namespace, Name: ref.Name}
		if i, ok := slices.BinarySearchFunc(s.routes, target, func(r computed.DelegRoute, t objects.Ref) int { return r.Ref.Compare(t) }); ok {
			return []objects.Ref{s.routes[i].Ref}, nil, "", ""
		}
		if s.unwatched[target] {
			return nil, nil, computed.UnresolvedUnwatched, ""
		}
		return nil, nil, computed.UnresolvedMissing, ""
	case objects.DelegationWildcard:
		idx = s.byNS[ref.Namespace]
	case objects.DelegationLabel:
		if !s.known {
			return nil, s.refs(s.byLabel[ref.Label]), "", computed.UnknownSetting
		}
		// An explicit empty namespace (CRD-forbidden) selects nothing here; upstream selects everywhere.
		if ref.Namespace == s.wildcard {
			idx = s.byLabel[ref.Label]
		} else {
			idx = s.byNSLabel[nsLabel{ref.Namespace, ref.Label}]
		}
	}
	if len(idx) == 0 {
		return nil, nil, computed.UnresolvedMissing, ""
	}
	return s.refs(idx), nil, "", ""
}

// refs returns the Refs of the routes at idx; nil when idx is empty.
func (s *selector) refs(idx []int) []objects.Ref {
	if len(idx) == 0 {
		return nil
	}
	out := make([]objects.Ref, len(idx))
	for i, j := range idx {
		out[i] = s.routes[j].Ref
	}
	return out
}

// permits reports whether child, in childNS, may be selected by parent, as
// ChildRouteCanAttachToParentRef does (pkg/kgateway/utils/delegation/delegation.go
// V3:20-46; rules §5.3). It reads the refs as written: group and kind must be
// written, a missing namespace is the child's own, and sectionName and port
// are ignored.
func permits(child *objects.HTTPRouteView, childNS string, parent objects.Ref) (bool, computed.Allows) {
	if len(child.WrittenRefs) == 0 {
		return true, computed.AllowsAny
	}
	for _, w := range child.WrittenRefs {
		ns := w.Namespace
		if ns == "" {
			ns = childNS
		}
		if w.Group == objects.GatewayAPIGroup && w.Kind == "HTTPRoute" && w.Name == parent.Name && ns == parent.Namespace {
			return true, computed.AllowsThis
		}
	}
	return false, computed.AllowsOther
}
