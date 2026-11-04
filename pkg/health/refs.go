package health

import (
	"fmt"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Wording for KGWUI-013 and 014 (design §7.3).
const (
	// refKindNotServedMessage is explanation 1 for one ref kind: kind, group.
	refKindNotServedMessage = "No kind `%s` in group `%s`. Check the group."
	// refKindsNotServedMessage is explanation 1 when the refs name several kinds.
	refKindsNotServedMessage = "Some refs name a kind their group does not serve. Check the groups."
	// coreGroup names the core API group in messages.
	coreGroup = "core"
)

// refRule is how a ResolvedRefs=False condition maps to 013 or 014.
type refRule struct {
	code, slug string
}

var (
	refUnresolved   = refRule{CodeRefUnresolved, "ref-unresolved"}
	refNotPermitted = refRule{CodeRefNotPermitted, "ref-not-permitted"}
)

// refRuleFor returns the specific code for cond on a route parent or a
// listener; false leaves the condition to KGWUI-002 (D-030).
func refRuleFor(cond metav1.Condition, listener bool) (refRule, bool) {
	if cond.Type != "ResolvedRefs" || cond.Status != metav1.ConditionFalse {
		return refRule{}, false
	}
	switch {
	case cond.Reason == "RefNotPermitted":
		return refNotPermitted, true
	case !listener, cond.Reason == "InvalidCertificateRef":
		return refUnresolved, true
	}
	return refRule{}, false
}

// writtenRef is one ref of a failing field, with defaults applied.
type writtenRef struct {
	RefLocation
	// delegation marks a backendRef that delegates to child routes: no
	// served kind, and never a ReferenceGrant target.
	delegation bool
}

// routeBackendRefs returns every spec.rules[].backendRefs[] entry of route u.
// Group defaults to core, kind to Service and namespace to u's.
func routeBackendRefs(u *unstructured.Unstructured) []writtenRef {
	rules, _, _ := unstructured.NestedSlice(u.Object, "spec", "rules")
	var out []writtenRef
	for i, r := range rules {
		rm, _ := r.(map[string]any)
		brs, _ := rm["backendRefs"].([]any)
		for j, b := range brs {
			bm, _ := b.(map[string]any)
			group, hasGroup := bm["group"].(string)
			kind, hasKind := bm["kind"].(string)
			ref := objects.Ref{Group: group, Kind: kind, Namespace: u.GetNamespace()}
			ref.Name, _ = bm["name"].(string)
			if !hasKind || kind == "" {
				ref.Kind = "Service"
			}
			if ns, _ := bm["namespace"].(string); ns != "" {
				ref.Namespace = ns
			}
			// As objects.DecodeHTTPRoute classifies delegation: group and kind both written.
			del := hasGroup && hasKind && (group == objects.GatewayAPIGroup && kind == "HTTPRoute" || group == "delegation.kgateway.dev" && kind == "label")
			out = append(out, writtenRef{RefLocation{Path: fmt.Sprintf("spec.rules[%d].backendRefs[%d]", i, j), Ref: ref}, del})
		}
	}
	return out
}

// listenerCertRefs returns the certificateRefs of the listener named name of
// Gateway or ListenerSet u, as decoded by objects (kind Secret, u's namespace).
func (in Input) listenerCertRefs(u *unstructured.Unstructured, name string) []writtenRef {
	var ls []objects.Listener
	if gvk := u.GroupVersionKind(); gvk.Group == objects.GatewayAPIGroup && gvk.Kind == "Gateway" {
		ls = in.views().Gateway(u).Listeners
	} else {
		ls = in.views().ListenerSet(u).Listeners
	}
	var out []writtenRef
	for i, l := range ls {
		if l.Name != name || l.TLS == nil {
			continue
		}
		for j, r := range l.TLS.CertificateRefs {
			out = append(out, writtenRef{RefLocation: RefLocation{Path: fmt.Sprintf("spec.listeners[%d].tls.certificateRefs[%d]", i, j), Ref: r}})
		}
	}
	return out
}

// notServed reports whether discovery says r's group serves no kind r.Kind.
// A group discovery did not list completely is unknown, never not served.
func (in Input) notServed(r objects.Ref) bool {
	return in.Catalog.Known(r.Group) && !in.Catalog.Served(r.Group, r.Kind)
}

// refsFinding returns the 013 or 014 finding for cond on u, or false for a
// condition KGWUI-002 keeps. listener is the listener name, "" for a route
// parent. Explanation 1 (kind not served) wins over the controller's message.
func (in Input) refsFinding(u *unstructured.Unstructured, cond metav1.Condition, listener, extra string, owner []string) (Finding, bool) {
	rule, ok := refRuleFor(cond, listener != "")
	if !ok {
		return Finding{}, false
	}
	var refs []writtenRef
	if listener != "" {
		refs = in.listenerCertRefs(u, listener)
	} else {
		refs = routeBackendRefs(u)
	}
	// field holds the refs the condition can be about: 014 only cross-namespace,
	// non-delegating refs; 013 every ref.
	field := refs
	if rule == refNotPermitted {
		field = nil
		for _, r := range refs {
			if !r.delegation && r.Ref.Namespace != u.GetNamespace() {
				field = append(field, r)
			}
		}
	}
	var bad []RefLocation
	if rule == refNotPermitted || listener == "" {
		for _, r := range field {
			if !r.delegation && in.notServed(r.Ref) {
				bad = append(bad, r.RefLocation)
			}
		}
	}
	f := Finding{Code: rule.code, Slug: rule.slug, Severity: SeverityError, Install: in.Install.ID(), Resource: ref(u),
		Message: message(cond), Detail: detail(cond, extra), Since: since(cond), Key: slices.Clone(owner)}
	ev := &RefsEvidence{Explanation: RefExplainController}
	if len(bad) > 0 {
		ev.Explanation, ev.Candidates, f.Message = RefExplainKindNotServed, bad, kindNotServedMessage(bad)
	} else {
		for _, r := range field {
			ev.Candidates = append(ev.Candidates, r.RefLocation)
		}
		if listener == "" {
			delegationCondition(&f, u, cond)
		}
	}
	ev.Attributed = len(ev.Candidates) == 1
	f.Facts = ev
	return f, true
}

// kindNotServedMessage is explanation 1's message for the refs in bad.
func kindNotServedMessage(bad []RefLocation) string {
	first := bad[0].Ref
	for _, b := range bad[1:] {
		if b.Ref.Group != first.Group || b.Ref.Kind != first.Kind {
			return refKindsNotServedMessage
		}
	}
	group := first.Group
	if group == "" {
		group = coreGroup
	}
	return fmt.Sprintf(refKindNotServedMessage, first.Kind, group)
}
