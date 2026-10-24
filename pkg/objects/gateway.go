package objects

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Listener is a spec listener of a Gateway or ListenerSet with defaults applied.
type Listener struct {
	Name string
	// Port is 0 when unset.
	Port          int32
	Protocol      string
	Hostname      string
	TLS           *ListenerTLS
	AllowedRoutes AllowedRoutes
}

// ListenerTLS is a listener's TLS block. Certificate refs are identities
// only; they are never looked up. TLS options are not decoded.
type ListenerTLS struct {
	Mode            string
	CertificateRefs []Ref
}

// AllowedRoutes is a listener's allowedRoutes.
type AllowedRoutes struct {
	Namespaces RouteNamespaces
	Kinds      []GroupKind
}

// RouteNamespaces is allowedRoutes.namespaces; From defaults to Same.
type RouteNamespaces struct {
	From     string
	Selector *metav1.LabelSelector
}

// GatewayView is the decoded spec and status of a Gateway.
type GatewayView struct {
	Class                string
	Listeners            []Listener
	SpecAddresses        []string
	StatusAddresses      []string
	AttachedListenerSets *int32
	// AllowedListeners is spec.allowedListeners as written, nil when absent.
	AllowedListeners map[string]any
	Conditions       []metav1.Condition
	ListenerStatus   []ListenerStatus
}

// ListenerSetView is the decoded spec and status of a ListenerSet.
type ListenerSetView struct {
	Parent         ParentRef
	Listeners      []Listener
	Conditions     []metav1.Condition
	ListenerStatus []ListenerStatus
}

type rawListener struct {
	Name     string `json:"name"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"`
	Hostname string `json:"hostname"`
	TLS      *struct {
		Mode            *string     `json:"mode"`
		CertificateRefs []rawObjRef `json:"certificateRefs"`
	} `json:"tls"`
	AllowedRoutes struct {
		Namespaces struct {
			From     *string               `json:"from"`
			Selector *metav1.LabelSelector `json:"selector"`
		} `json:"namespaces"`
		Kinds []rawGroupKind `json:"kinds"`
	} `json:"allowedRoutes"`
}

// listeners decodes spec.listeners one entry at a time, so a malformed
// entry is a zero Listener and the rest still decode.
func listeners(u *unstructured.Unstructured) []Listener {
	items := list(field(u, "spec", "listeners"))
	if items == nil {
		return nil
	}
	out := make([]Listener, 0, len(items))
	for _, item := range items {
		var r rawListener
		if !decodeInto(item, &r) {
			out = append(out, Listener{})
			continue
		}
		l := Listener{Name: r.Name, Port: r.Port, Protocol: r.Protocol, Hostname: r.Hostname,
			AllowedRoutes: AllowedRoutes{
				Namespaces: RouteNamespaces{From: orDefault(r.AllowedRoutes.Namespaces.From, "Same"), Selector: r.AllowedRoutes.Namespaces.Selector},
				Kinds:      groupKinds(r.AllowedRoutes.Kinds),
			}}
		if r.TLS != nil {
			l.TLS = &ListenerTLS{Mode: orDefault(r.TLS.Mode, "Terminate")}
			for _, c := range r.TLS.CertificateRefs {
				l.TLS.CertificateRefs = append(l.TLS.CertificateRefs, c.ref("Secret", u.GetNamespace()))
			}
		}
		out = append(out, l)
	}
	return out
}

// addresses returns the value of each entry of the list at path.
func addresses(u *unstructured.Unstructured, path ...string) []string {
	var out []string
	for _, a := range list(field(u, path...)) {
		if m, ok := a.(map[string]any); ok {
			if s, ok := m["value"].(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// DecodeGateway decodes a Gateway. It never fails.
func DecodeGateway(u *unstructured.Unstructured) *GatewayView {
	rv := view(u)
	v := &GatewayView{
		Class:           GatewayClassName(u),
		Listeners:       listeners(u),
		SpecAddresses:   addresses(u, "spec", "addresses"),
		StatusAddresses: addresses(u, "status", "addresses"),
		Conditions:      rv.Status.Conditions,
		ListenerStatus:  listenerStatus(rv),
	}
	if n, ok := field(u, "status", "attachedListenerSets").(int64); ok {
		n32 := int32(n) // #nosec G115 -- a count of ListenerSets fits in int32
		v.AttachedListenerSets = &n32
	}
	if m, ok := field(u, "spec", "allowedListeners").(map[string]any); ok {
		v.AllowedListeners = copyValue(m).(map[string]any)
	}
	return v
}

// DecodeListenerSet decodes a ListenerSet. It never fails.
func DecodeListenerSet(u *unstructured.Unstructured) *ListenerSetView {
	rv := view(u)
	return &ListenerSetView{Parent: ListenerSetParent(u), Listeners: listeners(u), Conditions: rv.Status.Conditions, ListenerStatus: listenerStatus(rv)}
}
