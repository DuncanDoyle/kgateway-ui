package objects

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// Decoder decodes typed views. Implementations may cache.
type Decoder interface {
	Gateway(u *unstructured.Unstructured) *GatewayView
	ListenerSet(u *unstructured.Unstructured) *ListenerSetView
	HTTPRoute(u *unstructured.Unstructured) *HTTPRouteView
	Backend(u *unstructured.Unstructured) *BackendView
	RouteHostnames(u *unstructured.Unstructured) *RouteHostnames
}

// Direct is a Decoder that decodes on every call.
type Direct struct{}

var _ Decoder = Direct{}

// Gateway decodes u with DecodeGateway.
func (Direct) Gateway(u *unstructured.Unstructured) *GatewayView { return DecodeGateway(u) }

// ListenerSet decodes u with DecodeListenerSet.
func (Direct) ListenerSet(u *unstructured.Unstructured) *ListenerSetView { return DecodeListenerSet(u) }

// HTTPRoute decodes u with DecodeHTTPRoute.
func (Direct) HTTPRoute(u *unstructured.Unstructured) *HTTPRouteView { return DecodeHTTPRoute(u) }

// Backend decodes u with DecodeBackend.
func (Direct) Backend(u *unstructured.Unstructured) *BackendView { return DecodeBackend(u) }

// RouteHostnames decodes u with DecodeRouteHostnames.
func (Direct) RouteHostnames(u *unstructured.Unstructured) *RouteHostnames {
	return DecodeRouteHostnames(u)
}

// decodeInto decodes m into dst and reports whether m is a well-formed
// object. On failure dst is the zero value, never a partial one.
func decodeInto[T any](m any, dst *T) bool {
	var zero T
	*dst = zero
	mm, ok := m.(map[string]any)
	if !ok {
		return false
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(mm, dst); err != nil {
		*dst = zero
		return false
	}
	return true
}

// field returns the value at path in u, or nil.
func field(u *unstructured.Unstructured, path ...string) any {
	v, _, _ := unstructured.NestedFieldNoCopy(u.Object, path...)
	return v
}

// list returns v as a list, or nil when it is not one.
func list(v any) []any {
	l, _ := v.([]any)
	return l
}

// stringList returns the string items of v; other items are skipped.
func stringList(v any) []string {
	var out []string
	for _, i := range list(v) {
		if s, ok := i.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func orDefault(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

type rawGroupKind struct {
	Group *string `json:"group"`
	Kind  string  `json:"kind"`
}

// groupKinds applies the RouteGroupKind default group gateway.networking.k8s.io.
func groupKinds(in []rawGroupKind) []GroupKind {
	if in == nil {
		return nil
	}
	out := make([]GroupKind, 0, len(in))
	for _, k := range in {
		out = append(out, GroupKind{Group: orDefault(k.Group, GatewayAPIGroup), Kind: k.Kind})
	}
	return out
}

// rawObjRef is a BackendObjectReference or SecretObjectReference.
type rawObjRef struct {
	Group     *string `json:"group"`
	Kind      *string `json:"kind"`
	Namespace *string `json:"namespace"`
	Name      string  `json:"name"`
	Port      *int32  `json:"port"`
}

// ref applies the defaults: core group, kind defKind, namespace ns.
func (r rawObjRef) ref(defKind, ns string) Ref {
	out := Ref{Group: deref(r.Group), Kind: orDefault(r.Kind, defKind), Namespace: ns, Name: r.Name}
	if r.Namespace != nil && *r.Namespace != "" {
		out.Namespace = *r.Namespace
	}
	return out
}
