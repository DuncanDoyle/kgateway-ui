// Port of kgw23 for v2.4.7; keep in sync.

package kgw24

import (
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// targetSections says how a known target kind names its sections: listeners
// (pkg/krtcollections/policy.go V4:512-518, :585-588) or named rules (V4:1446-1448).
var targetSections = map[objects.GroupKind]string{
	{Group: objects.GatewayAPIGroup, Kind: "Gateway"}:            "listeners",
	{Group: objects.GatewayAPIGroup, Kind: "ListenerSet"}:        "listeners",
	{Group: "gateway.networking.x-k8s.io", Kind: "XListenerSet"}: "listeners",
	{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute"}:          "rules",
	{Group: objects.GatewayAPIGroup, Kind: "GRPCRoute"}:          "rules",
}

// PolicyTargets implements computed.PolicyTargeter (computed-rules "Policy targets").
func (rules) PolicyTargets(in computed.TargetInput) []computed.TargetResult {
	n := len(in.TargetRefs) + len(in.Selectors)
	out := make([]computed.TargetResult, 0, n)
	for i, r := range in.TargetRefs {
		res := computed.TargetResult{Index: i, Ref: r, Outcome: refOutcome(in, r)}
		// One target whose ancestor is itself (a Gateway) or its route's Gateways (observed).
		k := r.Kind
		res.Verifiable = n == 1 && res.Outcome != computed.TargetUnknown && r.Group == objects.GatewayAPIGroup &&
			(k == "Gateway" || k == "HTTPRoute" || k == "GRPCRoute")
		out = append(out, res)
	}
	for j, s := range in.Selectors {
		out = append(out, computed.TargetResult{Index: len(in.TargetRefs) + j,
			Ref:     computed.TargetRef{Group: s.Group, Kind: s.Kind, SectionName: s.SectionName},
			Outcome: selectorOutcome(in, s)})
	}
	return out
}

// judged reports whether the rule may judge a kind: known, served and watched, with a known scope.
func judged(in computed.TargetInput, gk objects.GroupKind) bool {
	_, known := targetSections[gk]
	return in.ScopeKnown && known && in.Served != nil && in.Served(gk)
}

func refOutcome(in computed.TargetInput, r computed.TargetRef) string {
	gk := objects.GroupKind{Group: r.Group, Kind: r.Kind}
	if !judged(in, gk) || in.Lookup == nil {
		return computed.TargetUnknown
	}
	u, ok := in.Lookup(gk, in.Policy.Namespace, r.Name)
	switch {
	case !ok:
		return computed.TargetNotFound
	case r.SectionName != "" && !slices.Contains(sections(u, targetSections[gk]), r.SectionName):
		return computed.TargetSectionNotFound
	}
	return computed.TargetFound
}

// selectorOutcome never judges a selector's section: the matched objects are not known.
func selectorOutcome(in computed.TargetInput, s computed.TargetSelector) string {
	gk := objects.GroupKind{Group: s.Group, Kind: s.Kind}
	if !judged(in, gk) || in.Labelled == nil {
		return computed.TargetUnknown
	}
	// An empty matchLabels selects nothing (istio labels.Instance.Match; policy.go V4:839).
	ns := in.Policy.Namespace
	switch {
	case len(s.MatchLabels) == 0:
		return computed.TargetMatchesNothing
	case in.Labelled(gk, ns, s.MatchLabels):
		return computed.TargetFound
	case !in.GlobalPolicyNamespaceKnown:
		// The policy may be in the global policy namespace, whose selectors match everywhere.
		return computed.TargetUnknown
	case in.GlobalPolicyNamespace != "" && ns == in.GlobalPolicyNamespace && in.Labelled(gk, "", s.MatchLabels):
		// A policy in the global policy namespace selects in every namespace (policy.go V4:918-922).
		return computed.TargetFound
	}
	return computed.TargetMatchesNothing
}

// sections returns the listener names or the rule names of u; unnamed rules are no section.
func sections(u *unstructured.Unstructured, field string) []string {
	list, _, _ := unstructured.NestedSlice(u.Object, "spec", field)
	var names []string
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			if s, _ := m["name"].(string); s != "" {
				names = append(names, s)
			}
		}
	}
	return names
}
