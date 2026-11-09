// Port of kgw24 for v2.5.0; keep in sync.

package kgw25

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

func gw(ns, name string, ls ...objects.Listener) computed.Resource {
	return computed.Resource{Ref: objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: ns, Name: name}, Listeners: ls}
}

func lset(ns, name string, acc computed.Acceptance, ls ...objects.Listener) computed.Resource {
	r := gw(ns, name, ls...)
	r.Ref.Kind = "ListenerSet"
	r.Acceptance = acc
	return r
}

func route(kind, ns, name string, refs ...objects.ParentRef) computed.Route {
	return computed.Route{
		Ref:        objects.Ref{Group: objects.GatewayAPIGroup, Kind: kind, Namespace: ns, Name: name},
		ParentRefs: refs,
		Written:    make([]objects.WrittenRef, len(refs)),
	}
}

// pref is a parentRef to r with an optional sectionName.
func pref(r computed.Resource, section string) objects.ParentRef {
	return objects.ParentRef{Group: r.Ref.Group, Kind: r.Ref.Kind, Namespace: r.Ref.Namespace, Name: r.Ref.Name, SectionName: section}
}

func lis(name, proto string, port int32) objects.Listener {
	return objects.Listener{Name: name, Protocol: proto, Port: port}
}

func withFrom(l objects.Listener, from string, sel map[string]string) objects.Listener {
	l.AllowedRoutes.Namespaces.From = from
	if sel != nil {
		l.AllowedRoutes.Namespaces.Selector = &metav1.LabelSelector{MatchLabels: sel}
	}
	return l
}

func withHosts(r computed.Route, h ...string) computed.Route {
	r.Hostnames = h
	return r
}

func sameRefs(got, want []objects.Ref) bool {
	g, w := slices.Clone(got), slices.Clone(want)
	slices.SortFunc(g, compareRef)
	slices.SortFunc(w, compareRef)
	return slices.Equal(g, w)
}
