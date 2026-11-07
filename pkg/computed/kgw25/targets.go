// Port of kgw24 for v2.5.0, selectors only; keep in sync.

package kgw25

import (
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// targetSections are the target kinds the rule judges, as in kgw24.
var targetSections = map[objects.GroupKind]string{
	{Group: objects.GatewayAPIGroup, Kind: "Gateway"}:            "listeners",
	{Group: objects.GatewayAPIGroup, Kind: "ListenerSet"}:        "listeners",
	{Group: "gateway.networking.x-k8s.io", Kind: "XListenerSet"}: "listeners",
	{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute"}:          "rules",
	{Group: objects.GatewayAPIGroup, Kind: "GRPCRoute"}:          "rules",
}

// PolicyTargets implements computed.PolicyTargeter for selectors only (D-058
// item 7). The controller reports a missing targetRef itself, so refs are
// unknown here; it never judges a selector (policy_target_status.go:241-242 at v2.5.0).
func (rules) PolicyTargets(in computed.TargetInput) []computed.TargetResult {
	out := make([]computed.TargetResult, 0, len(in.TargetRefs)+len(in.Selectors))
	for i, r := range in.TargetRefs {
		out = append(out, computed.TargetResult{Index: i, Ref: r, Outcome: computed.TargetUnknown})
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
