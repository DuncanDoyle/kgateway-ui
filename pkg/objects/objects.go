// Package objects provides typed, read-only views over unstructured Gateway API
// and kgateway objects. It decodes only the fields the UI needs and applies the
// Gateway API defaults, so callers never handle raw maps.
package objects

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GatewayAPIGroup is the API group of the Gateway API.
const GatewayAPIGroup = "gateway.networking.k8s.io"

// Ref identifies an object.
type Ref struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// String returns "namespace/name", or "name" for cluster-scoped objects.
func (r Ref) String() string {
	if r.Namespace == "" {
		return r.Name
	}
	return r.Namespace + "/" + r.Name
}

// RefOf returns the Ref of u.
func RefOf(u *unstructured.Unstructured) Ref {
	gv, _ := schema.ParseGroupVersion(u.GetAPIVersion())
	return Ref{Group: gv.Group, Kind: u.GetKind(), Namespace: u.GetNamespace(), Name: u.GetName()}
}

// ParentRef is a Gateway API parent or ancestor reference with defaults applied.
type ParentRef struct {
	Group       string `json:"group"`
	Kind        string `json:"kind"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	SectionName string `json:"sectionName,omitempty"`
	// Port is the parentRef port; 0 means unset (Gateway API ports start at 1).
	Port int32 `json:"port,omitempty"`
}

// Ref returns the identity of the referenced object. Section name and port
// are dropped: they select a listener, not a different object.
func (p ParentRef) Ref() Ref {
	return Ref{Group: p.Group, Kind: p.Kind, Namespace: p.Namespace, Name: p.Name}
}

// GroupKind is an API group and kind.
type GroupKind struct {
	Group string `json:"group"`
	Kind  string `json:"kind"`
}

// WrittenRef is a parentRef as written: omitted fields are empty strings and
// no defaults are applied. It is comparable, so it serves as a report key and
// a persistence identity (design §4.1, computed-rules §7).
type WrittenRef struct {
	Group, Kind, Namespace, Name, SectionName string
	// Port is the written port; PortSet reports whether one was written.
	Port    int32
	PortSet bool
}

type rawRef struct {
	Group       *string `json:"group"`
	Kind        *string `json:"kind"`
	Namespace   *string `json:"namespace"`
	Name        string  `json:"name"`
	SectionName *string `json:"sectionName"`
	Port        *int32  `json:"port"`
}

// written returns r as written.
func (r rawRef) written() WrittenRef {
	w := WrittenRef{Group: deref(r.Group), Kind: deref(r.Kind), Namespace: deref(r.Namespace), Name: r.Name, SectionName: deref(r.SectionName)}
	if r.Port != nil {
		w.Port, w.PortSet = *r.Port, true
	}
	return w
}

// resolve applies the Gateway API defaults: group gateway.networking.k8s.io,
// kind Gateway, and the referencing object's namespace.
func (r rawRef) resolve(defaultNS string) ParentRef {
	p := ParentRef{Group: GatewayAPIGroup, Kind: "Gateway", Namespace: defaultNS, Name: r.Name}
	if r.Group != nil {
		p.Group = *r.Group
	}
	if r.Kind != nil {
		p.Kind = *r.Kind
	}
	if r.Namespace != nil && *r.Namespace != "" {
		p.Namespace = *r.Namespace
	}
	if r.SectionName != nil {
		p.SectionName = *r.SectionName
	}
	if r.Port != nil {
		p.Port = *r.Port
	}
	return p
}

type routeView struct {
	Spec struct {
		ParentRefs []rawRef `json:"parentRefs"`
		Listeners  []struct {
			Name string `json:"name"`
		} `json:"listeners"`
	} `json:"spec"`
	Status struct {
		Parents []struct {
			ParentRef      rawRef             `json:"parentRef"`
			ControllerName string             `json:"controllerName"`
			Conditions     []metav1.Condition `json:"conditions"`
		} `json:"parents"`
		Ancestors []struct {
			AncestorRef    rawRef             `json:"ancestorRef"`
			ControllerName string             `json:"controllerName"`
			Conditions     []metav1.Condition `json:"conditions"`
		} `json:"ancestors"`
		Conditions []metav1.Condition `json:"conditions"`
		Listeners  []struct {
			Name           string             `json:"name"`
			AttachedRoutes int32              `json:"attachedRoutes"`
			SupportedKinds []rawGroupKind     `json:"supportedKinds"`
			Conditions     []metav1.Condition `json:"conditions"`
		} `json:"listeners"`
		Addresses []struct {
			Value string `json:"value"`
		} `json:"addresses"`
	} `json:"status"`
}

// view decodes the subset of fields used by this package. Unknown fields are
// ignored; a malformed field yields its zero value rather than an error,
// because the UI must keep rendering the rest of the cluster. Each field
// decodes on its own, so a malformed one never wipes another.
func view(u *unstructured.Unstructured) routeView {
	var v routeView
	spec, _ := u.Object["spec"].(map[string]any)
	decodeKey(spec, "parentRefs", &v.Spec.ParentRefs)
	decodeKey(spec, "listeners", &v.Spec.Listeners)
	st, _ := u.Object["status"].(map[string]any)
	decodeKey(st, "parents", &v.Status.Parents)
	decodeKey(st, "ancestors", &v.Status.Ancestors)
	decodeKey(st, "conditions", &v.Status.Conditions)
	decodeKey(st, "listeners", &v.Status.Listeners)
	decodeKey(st, "addresses", &v.Status.Addresses)
	return v
}

// RouteParentRefs returns spec.parentRefs with defaults applied.
func RouteParentRefs(u *unstructured.Unstructured) []ParentRef {
	return parentRefs(view(u), u.GetNamespace())
}

func parentRefs(v routeView, ns string) []ParentRef {
	out := make([]ParentRef, 0, len(v.Spec.ParentRefs))
	for _, r := range v.Spec.ParentRefs {
		out = append(out, r.resolve(ns))
	}
	return out
}

// WrittenParentRefs returns spec.parentRefs of any route kind as written,
// index-aligned with RouteParentRefs.
func WrittenParentRefs(u *unstructured.Unstructured) []WrittenRef { return writtenRefs(view(u)) }

func writtenRefs(v routeView) []WrittenRef {
	out := make([]WrittenRef, 0, len(v.Spec.ParentRefs))
	for _, r := range v.Spec.ParentRefs {
		out = append(out, r.written())
	}
	return out
}

// RouteParentStatus is one entry of a route's status.parents.
type RouteParentStatus struct {
	ParentRef ParentRef
	// Written is the status parentRef as written, for report keys.
	Written        WrittenRef `json:"-"`
	ControllerName string
	Conditions     []metav1.Condition
}

// RouteParents returns status.parents with defaults applied to each parentRef.
func RouteParents(u *unstructured.Unstructured) []RouteParentStatus {
	return routeParents(view(u), u.GetNamespace())
}

func routeParents(v routeView, ns string) []RouteParentStatus {
	out := make([]RouteParentStatus, 0, len(v.Status.Parents))
	for _, p := range v.Status.Parents {
		out = append(out, RouteParentStatus{ParentRef: p.ParentRef.resolve(ns), Written: p.ParentRef.written(),
			ControllerName: p.ControllerName, Conditions: p.Conditions})
	}
	return out
}

// AncestorStatus is one entry of a policy's status.ancestors.
type AncestorStatus struct {
	AncestorRef    ParentRef
	ControllerName string
	Conditions     []metav1.Condition
}

// PolicyAncestors returns status.ancestors with defaults applied.
func PolicyAncestors(u *unstructured.Unstructured) []AncestorStatus {
	v := view(u)
	out := make([]AncestorStatus, 0, len(v.Status.Ancestors))
	for _, a := range v.Status.Ancestors {
		out = append(out, AncestorStatus{AncestorRef: a.AncestorRef.resolve(u.GetNamespace()), ControllerName: a.ControllerName, Conditions: a.Conditions})
	}
	return out
}

// Conditions returns status.conditions.
func Conditions(u *unstructured.Unstructured) []metav1.Condition { return view(u).Status.Conditions }

// ListenerStatus is one entry of a Gateway's status.listeners.
type ListenerStatus struct {
	Name           string
	AttachedRoutes int32
	SupportedKinds []GroupKind
	Conditions     []metav1.Condition
}

// GatewayListeners returns status.listeners.
func GatewayListeners(u *unstructured.Unstructured) []ListenerStatus { return listenerStatus(view(u)) }

func listenerStatus(v routeView) []ListenerStatus {
	out := make([]ListenerStatus, 0, len(v.Status.Listeners))
	for _, l := range v.Status.Listeners {
		out = append(out, ListenerStatus{Name: l.Name, AttachedRoutes: l.AttachedRoutes, SupportedKinds: groupKinds(l.SupportedKinds), Conditions: l.Conditions})
	}
	return out
}

// GatewayAddresses returns the values of status.addresses.
func GatewayAddresses(u *unstructured.Unstructured) []string {
	var out []string
	for _, a := range view(u).Status.Addresses {
		out = append(out, a.Value)
	}
	return out
}

// GatewaySpecListeners returns the names in a Gateway's spec.listeners.
func GatewaySpecListeners(u *unstructured.Unstructured) []string {
	var out []string
	for _, l := range view(u).Spec.Listeners {
		out = append(out, l.Name)
	}
	return out
}

// GatewayClassName returns spec.gatewayClassName of a Gateway.
func GatewayClassName(u *unstructured.Unstructured) string {
	s, _, _ := unstructured.NestedString(u.Object, "spec", "gatewayClassName")
	return s
}

// ControllerName returns spec.controllerName of a GatewayClass.
func ControllerName(gatewayClass *unstructured.Unstructured) string {
	s, _, _ := unstructured.NestedString(gatewayClass.Object, "spec", "controllerName")
	return s
}

// ListenerSetParent returns spec.parentRef of a ListenerSet with defaults applied.
func ListenerSetParent(u *unstructured.Unstructured) ParentRef {
	m, _, _ := unstructured.NestedMap(u.Object, "spec", "parentRef")
	var r rawRef
	decodeInto(m, &r)
	return r.resolve(u.GetNamespace())
}

// FindCondition returns the condition of type typ, or nil.
func FindCondition(cs []metav1.Condition, typ string) *metav1.Condition {
	for i := range cs {
		if cs[i].Type == typ {
			return &cs[i]
		}
	}
	return nil
}

// IsStale reports whether c may describe an older generation of u than the
// current spec. A condition without observedGeneration (0) gives no evidence
// that it is current, so it counts as stale.
func IsStale(u *unstructured.Unstructured, c metav1.Condition) bool {
	return c.ObservedGeneration < u.GetGeneration()
}
