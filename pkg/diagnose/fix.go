package diagnose

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// SuggestedFix is the one change a finding's evidence leaves (design §7.7).
// Create marks a new Target; otherwise YAML is the fragment at Path of Target.
// Names are every object the fix names: a caller who may not view one gets no fix.
type SuggestedFix struct {
	Title  string
	Target objects.Ref
	Create bool
	Path   string
	YAML   string
	Basis  string
	Names  []objects.Ref
}

// Wording of the suggested fixes (design §7.7).
const (
	// FixGrantTitle names the grant's namespace.
	FixGrantTitle = "Create a ReferenceGrant in `%s`"
	FixGrantBasis = "Gateway API: a ref to another namespace needs a ReferenceGrant in that namespace."
	// FixGroupTitle names the group to use.
	FixGroupTitle = "Set the group to `%s`"
	// FixGroupBasis names the group and the kind.
	FixGroupBasis = "API discovery: only group `%s` serves kind `%s`. This sets only group, kind, name and namespace; keep the port and other fields."
)

// grantFields are the refs the Gateway API spec requires a ReferenceGrant for
// (design §7.3): route backendRefs, mirrors included, and listener certificateRefs.
var grantFields = regexp.MustCompile(`^spec\.rules\[\d+\]\.(backendRefs\[\d+\](\.filters\[\d+\]\.requestMirror\.backendRef)?|filters\[\d+\]\.requestMirror\.backendRef)$|^spec\.listeners\[\d+\]\.tls\.certificateRefs\[\d+\]$`)

// maxName is the longest object name Kubernetes accepts.
const maxName = 253

// Fix returns f's suggested fix from its evidence alone, or nil. Only an
// attributed KGWUI-013 or 014 has one: the corrected group when discovery
// left one group with the kind, else for 014 the ReferenceGrant the spec requires.
func Fix(f *health.Finding) *SuggestedFix {
	ev, ok := f.Facts.(*health.RefsEvidence)
	if !ok || f.Resource == nil || !ev.Attributed || len(ev.Candidates) != 1 ||
		f.Code != health.CodeRefUnresolved && f.Code != health.CodeRefNotPermitted {
		return nil
	}
	c := ev.Candidates[0]
	switch {
	case ev.Explanation == health.RefExplainKindNotServed:
		if ev.Group == nil {
			return nil
		}
		return groupFix(*f.Resource, c, *ev.Group)
	case ev.Explanation == health.RefExplainController && f.Code == health.CodeRefNotPermitted && grantFields.MatchString(c.Path):
		return grantFix(*f.Resource, c.Ref)
	}
	return nil
}

// groupFix is the fragment at c.Path of referrer that sets group.
func groupFix(referrer objects.Ref, c health.RefLocation, group string) *SuggestedFix {
	to := c.Ref
	to.Group = group
	frag := map[string]any{"group": group, "kind": to.Kind, "name": to.Name, "namespace": to.Namespace}
	return &SuggestedFix{Title: fmt.Sprintf(FixGroupTitle, groupName(group)), Target: referrer, Path: c.Path, YAML: mustYAML(frag),
		Basis: fmt.Sprintf(FixGroupBasis, groupName(group), to.Kind), Names: []objects.Ref{referrer, to}}
}

// grantFix is the ReferenceGrant in to's namespace that lets referrer name to;
// spec-defined fields only, and an omitted group is the core group "".
func grantFix(referrer, to objects.Ref) *SuggestedFix {
	name := grantName(referrer, to)
	grant := map[string]any{
		"apiVersion": objects.GatewayAPIGroup + "/v1beta1",
		"kind":       "ReferenceGrant",
		"metadata":   map[string]any{"name": name, "namespace": to.Namespace},
		"spec": map[string]any{
			"from": []any{map[string]any{"group": referrer.Group, "kind": referrer.Kind, "namespace": referrer.Namespace}},
			"to":   []any{map[string]any{"group": to.Group, "kind": to.Kind, "name": to.Name}},
		},
	}
	target := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "ReferenceGrant", Namespace: to.Namespace, Name: name}
	return &SuggestedFix{Title: fmt.Sprintf(FixGrantTitle, to.Namespace), Target: target, Create: true, YAML: mustYAML(grant),
		Basis: FixGrantBasis, Names: []objects.Ref{referrer, to}}
}

// grantName is a valid object name for the grant from referrer to to. It
// names to's kind, and group unless core; a cut name ends in a hash of the whole.
func grantName(referrer, to objects.Ref) string {
	parts := []string{"allow", referrer.Namespace, referrer.Kind}
	if to.Group != "" {
		parts = append(parts, to.Group)
	}
	n := strings.ToLower(strings.Join(append(parts, to.Kind, to.Name), "-"))
	if len(n) <= maxName {
		return n
	}
	sum := sha256.Sum256([]byte(n))
	suffix := "-" + hex.EncodeToString(sum[:4])
	return strings.TrimRight(n[:maxName-len(suffix)], "-.") + suffix
}

// groupName is group as messages show it: "core" for the core group.
func groupName(group string) string {
	if group == "" {
		return "core"
	}
	return group
}

// mustYAML renders v, a tree of maps, slices and strings, which cannot fail.
func mustYAML(v any) string {
	b, err := yaml.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
