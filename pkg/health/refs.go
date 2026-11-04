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
	// delegation marks a backendRef that delegates to child routes: it has no
	// served kind of its own.
	delegation bool
}

// objRef decodes a written ref map with defKind and namespace ns as defaults.
func objRef(m map[string]any, defKind, ns string) (objects.Ref, bool) {
	group, hasGroup := m["group"].(string)
	kind, hasKind := m["kind"].(string)
	r := objects.Ref{Group: group, Kind: kind, Namespace: ns}
	r.Name, _ = m["name"].(string)
	if !hasKind || kind == "" {
		r.Kind = defKind
	}
	if n, _ := m["namespace"].(string); n != "" {
		r.Namespace = n
	}
	// As objects.DecodeHTTPRoute classifies delegation: group and kind both written.
	del := hasGroup && hasKind && (group == objects.GatewayAPIGroup && kind == "HTTPRoute" || group == "delegation.kgateway.dev" && kind == "label")
	return r, del
}

// routeBackendRefs returns every spec.rules[].backendRefs[] entry of route u and
// every RequestMirror backendRef in their filters and the rules'. Group defaults
// to core, kind to Service and namespace to u's.
func routeBackendRefs(u *unstructured.Unstructured) []writtenRef {
	rules, _, _ := unstructured.NestedSlice(u.Object, "spec", "rules")
	var out []writtenRef
	add := func(m map[string]any, path string) {
		r, del := objRef(m, "Service", u.GetNamespace())
		out = append(out, writtenRef{RefLocation{Path: path, Ref: r}, del})
	}
	mirrors := func(fs any, path string) {
		for k, f := range asList(fs) {
			if m, ok := asMap(asMap(f)["requestMirror"])["backendRef"].(map[string]any); ok {
				add(m, fmt.Sprintf("%s.filters[%d].requestMirror.backendRef", path, k))
			}
		}
	}
	for i, r := range rules {
		rm := asMap(r)
		for j, b := range asList(rm["backendRefs"]) {
			path := fmt.Sprintf("spec.rules[%d].backendRefs[%d]", i, j)
			add(asMap(b), path)
			mirrors(asMap(b)["filters"], path)
		}
		mirrors(rm["filters"], fmt.Sprintf("spec.rules[%d]", i))
	}
	return out
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

func asList(v any) []any { l, _ := v.([]any); return l }

// delegationRoute reports whether route u delegates or is a delegation child:
// a delegation cycle is RefNotPermitted too, so no ref can be singled out.
func (in Input) delegationRoute(u *unstructured.Unstructured, refs []writtenRef) bool {
	if slices.ContainsFunc(refs, func(r writtenRef) bool { return r.delegation }) {
		return true
	}
	route := func(group, kind string) bool { return group == objects.GatewayAPIGroup && kind == "HTTPRoute" }
	for _, p := range asList(asMap(u.Object["spec"])["parentRefs"]) {
		if g, _ := asMap(p)["group"].(string); route(g, fmt.Sprint(asMap(p)["kind"])) {
			return true
		}
	}
	return slices.ContainsFunc(in.routeParents(u), func(s objects.RouteParentStatus) bool { return route(s.ParentRef.Group, s.ParentRef.Kind) })
}

// listenerRefs returns the certificateRefs of the listener named name of
// Gateway or ListenerSet u (kind Secret, u's namespace by default), and the
// CA certificate refs of the Gateway's frontend TLS for its port. complete is
// false for a ListenerSet whose Gateway is not owned or has CA refs, whose
// refs live on another object.
func (in Input) listenerRefs(u *unstructured.Unstructured, name string) (out []writtenRef, complete bool) {
	gateway := func(g *unstructured.Unstructured) bool {
		gvk := g.GroupVersionKind()
		return gvk.Group == objects.GatewayAPIGroup && gvk.Kind == "Gateway"
	}
	var ls []objects.Listener
	var parent *unstructured.Unstructured
	if gateway(u) {
		ls, parent = in.views().Gateway(u).Listeners, u
	} else {
		v := in.views().ListenerSet(u)
		ls = v.Listeners
		for _, g := range in.Owned.Gateways {
			if r := objects.RefOf(g); r.Name == v.Parent.Name && r.Namespace == v.Parent.Namespace {
				parent = g
			}
		}
	}
	port, found := int32(0), false
	for i, l := range ls {
		if l.Name != name {
			continue
		}
		port, found = l.Port, true
		if l.TLS == nil {
			continue
		}
		for j, r := range l.TLS.CertificateRefs {
			out = append(out, writtenRef{RefLocation: RefLocation{Path: fmt.Sprintf("spec.listeners[%d].tls.certificateRefs[%d]", i, j), Ref: r}})
		}
	}
	if parent == nil {
		return out, false
	}
	ca := gatewayCARefs(parent, port, found)
	if parent != u {
		return out, len(ca) == 0
	}
	return append(out, ca...), true
}

// gatewayCARefs returns the frontend validation caCertificateRefs of Gateway g
// that apply to port: the perPort entry for it, else the default. Without a
// known port it returns every entry's.
func gatewayCARefs(g *unstructured.Unstructured, port int32, known bool) []writtenRef {
	front := asMap(asMap(asMap(g.Object["spec"])["tls"])["frontend"])
	var out []writtenRef
	refs := func(tls any, path string) {
		for k, r := range asList(asMap(asMap(tls)["validation"])["caCertificateRefs"]) {
			ref, _ := objRef(asMap(r), "", g.GetNamespace())
			out = append(out, writtenRef{RefLocation: RefLocation{Path: fmt.Sprintf("%s.validation.caCertificateRefs[%d]", path, k), Ref: ref}})
		}
	}
	perPort := false
	for n, pp := range asList(front["perPort"]) {
		p, _ := asMap(pp)["port"].(int64)
		if !known || int32(p) == port { //nolint:gosec // a port fits in int32
			refs(asMap(pp)["tls"], fmt.Sprintf("spec.tls.frontend.perPort[%d].tls", n))
			perPort = known
		}
	}
	if !perPort {
		refs(front["default"], "spec.tls.frontend.default")
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
	// attributable is false when the condition may concern a ref outside refs.
	var attributable bool
	if listener != "" {
		refs, attributable = in.listenerRefs(u, listener)
	} else {
		refs = routeBackendRefs(u)
		attributable = !in.delegationRoute(u, refs)
	}
	// field holds the refs the condition can be about: 014 every cross-namespace
	// ref, delegating ones included; 013 every ref.
	field := refs
	if rule == refNotPermitted {
		field = nil
		for _, r := range refs {
			if r.Ref.Namespace != u.GetNamespace() {
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
	ev.Attributed = attributable && len(ev.Candidates) == 1
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
