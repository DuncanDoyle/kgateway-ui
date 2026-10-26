package objects

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// BackendView is the decoded kgateway Backend.
type BackendView struct {
	Type       string
	Conditions []metav1.Condition
}

// RouteHostnames holds spec.hostnames of a GRPCRoute or TLSRoute.
type RouteHostnames struct {
	Hostnames []string
}

// DecodeBackend decodes a kgateway Backend. It never fails.
func DecodeBackend(u *unstructured.Unstructured) *BackendView {
	t, _ := field(u, "spec", "type").(string)
	return &BackendView{Type: t, Conditions: Conditions(u)}
}

// DecodeRouteHostnames decodes spec.hostnames of any route kind. It never fails.
func DecodeRouteHostnames(u *unstructured.Unstructured) *RouteHostnames {
	return &RouteHostnames{Hostnames: stringList(field(u, "spec", "hostnames"))}
}

// RouteView is the decoded spec and status any route kind shares
// (GRPCRoute, TCPRoute, TLSRoute), for callers that do not need a full
// HTTPRouteView. Fields are as RouteParentRefs, RouteParents and
// RouteBackendTargets return them.
type RouteView struct {
	Hostnames  []string
	ParentRefs []ParentRef
	// WrittenRefs are ParentRefs as written, index-aligned.
	WrittenRefs []WrittenRef
	Parents     []RouteParentStatus
	Targets     []BackendTarget
}

// DecodeRoute decodes the shared fields of any route kind in one pass. It never fails.
func DecodeRoute(u *unstructured.Unstructured) *RouteView {
	rv, ns := view(u, vParentRefs|vParents), u.GetNamespace()
	return &RouteView{Hostnames: stringList(field(u, "spec", "hostnames")), ParentRefs: parentRefs(rv, ns),
		WrittenRefs: writtenRefs(rv), Parents: routeParents(rv, ns), Targets: RouteBackendTargets(u)}
}

// RouteBackendTargets returns the spec.rules[].backendRefs and RequestMirror
// targets of any route kind (GRPCRoute, TCPRoute, TLSRoute), with Gateway API
// defaults applied. Use BackendTargets for a decoded HTTPRoute. It never fails.
func RouteBackendTargets(u *unstructured.Unstructured) []BackendTarget {
	ns := u.GetNamespace()
	var out []BackendTarget
	mirrors := func(fs any) {
		for _, f := range decodeFilters(fs, ns) {
			if f.Mirror != nil {
				out = append(out, BackendTarget{Ref: f.Mirror.Backend, Mirror: true})
			}
		}
	}
	for _, r := range list(field(u, "spec", "rules")) {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		mirrors(m["filters"])
		for _, b := range list(m["backendRefs"]) {
			var raw rawBackendRef
			if !decodeInto(b, &raw) {
				continue
			}
			out = append(out, BackendTarget{Ref: raw.ref("Service", ns)})
			if bm, ok := b.(map[string]any); ok {
				mirrors(bm["filters"])
			}
		}
	}
	return out
}
