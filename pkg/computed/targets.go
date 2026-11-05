package computed

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// TargetRef is a policy's spec.targetRefs entry as written. Its namespace is the policy's (GEP-713).
type TargetRef struct {
	Group, Kind, Name string
	// SectionName is empty when not written.
	SectionName string
}

// TargetSelector is a policy's spec.targetSelectors entry as written.
type TargetSelector struct {
	Group, Kind string
	MatchLabels map[string]string
	// SectionName is empty when not written.
	SectionName string
}

// TargetInput is one policy's targets and the lookups the rule may use
// (computed-rules "Policy targets").
type TargetInput struct {
	Policy     objects.Ref
	TargetRefs []TargetRef
	Selectors  []TargetSelector
	// Lookup returns the cached object of a kind by namespace and name.
	Lookup func(gk objects.GroupKind, ns, name string) (*unstructured.Unstructured, bool)
	// Labelled reports whether some cached object of a kind in ns has every label in sel.
	Labelled func(gk objects.GroupKind, ns string, sel map[string]string) bool
	// Served reports whether a kind is served by the API server and watched by the UI.
	Served func(gk objects.GroupKind) bool
	// ScopeKnown is false when the install's discovery scope is unknown (D-024).
	ScopeKnown bool
}

// Target outcomes, as TargetResult.Outcome carries them.
const (
	TargetFound           = "found"
	TargetNotFound        = "not-found"
	TargetSectionNotFound = "section-not-found"
	TargetMatchesNothing  = "matches-nothing"
	TargetUnknown         = "unknown"
)

// TargetResult is one target's outcome. Index counts TargetRefs first, then
// Selectors; a selector's Ref carries its group, kind and section, with no name.
type TargetResult struct {
	Index   int
	Ref     TargetRef
	Outcome string
	// Verifiable is true when a current ancestor can be tied to this target alone (design §7.6).
	Verifiable bool
}

// PolicyTargeter is an optional interface of a Rules: it resolves a policy's
// targets for the computed KGWUI-016. A rule set without it raises no computed 016.
type PolicyTargeter interface {
	PolicyTargets(in TargetInput) []TargetResult
}

// PolicyTargeter returns the selected rule set's targeter, or nil when it has none.
func (s Selection) PolicyTargeter() PolicyTargeter {
	t, _ := s.Rules.(PolicyTargeter)
	return t
}

// DecodeTargets returns a policy's spec.targetRefs and spec.targetSelectors as written.
// Entries that are not objects are skipped.
func DecodeTargets(u *unstructured.Unstructured) ([]TargetRef, []TargetSelector) {
	var refs []TargetRef
	var sels []TargetSelector
	list, _, _ := unstructured.NestedSlice(u.Object, "spec", "targetRefs")
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			refs = append(refs, TargetRef{Group: stringField(m, "group"), Kind: stringField(m, "kind"), Name: stringField(m, "name"), SectionName: stringField(m, "sectionName")})
		}
	}
	list, _, _ = unstructured.NestedSlice(u.Object, "spec", "targetSelectors")
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		labels, _, _ := unstructured.NestedStringMap(m, "matchLabels")
		sels = append(sels, TargetSelector{Group: stringField(m, "group"), Kind: stringField(m, "kind"), MatchLabels: labels, SectionName: stringField(m, "sectionName")})
	}
	return refs, sels
}

func stringField(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}
