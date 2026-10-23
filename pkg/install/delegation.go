package install

import (
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// ReportedEdge returns child's status entries that give it a reported edge
// to the HTTPRoute parent (design §4.2 conditions 2 and 3): each is from one
// of controllers, its parentRef resolves to parent, and it is eligible: a
// current spec parentRef equals it after defaults, or the spec list is empty.
// A child naming parent twice (by section or port) has one entry per ref.
// Freshness does not matter. Scope and ownership are the caller's to check.
func ReportedEdge(child *objects.HTTPRouteView, parent objects.Ref, controllers []string) ([]*objects.RouteParentStatus, bool) {
	var out []*objects.RouteParentStatus
	for i := range child.Parents {
		e := &child.Parents[i]
		if e.ParentRef.Ref() != parent || !slices.Contains(controllers, e.ControllerName) {
			continue
		}
		if len(child.ParentRefs) != 0 && !slices.Contains(child.ParentRefs, e.ParentRef) {
			continue
		}
		if !slices.ContainsFunc(out, func(x *objects.RouteParentStatus) bool { return x.ParentRef == e.ParentRef }) {
			out = append(out, e)
		}
	}
	return out, len(out) > 0
}

// httpRouteKind returns the served HTTPRoute kind.
func httpRouteKind(served []kinds.Served) (kinds.Served, bool) {
	for _, k := range kinds.ByCategory(served, kinds.CategoryRoute) {
		if k.Group == objects.GatewayAPIGroup && k.Kind == "HTTPRoute" {
			return k, true
		}
	}
	return kinds.Served{}, false
}

// httpRoutes are the snapshot's HTTPRoutes with their views, decoded once
// per recompute.
type httpRoutes struct {
	kind  kinds.Served
	objs  []*unstructured.Unstructured
	views []*objects.HTTPRouteView
	byRef map[objects.Ref]int
}

// decodeHTTPRoutes decodes the served HTTPRoutes through dec; nil when the
// kind is not served.
func decodeHTTPRoutes(snap *cache.Snapshot, served []kinds.Served, dec objects.Decoder) *httpRoutes {
	k, ok := httpRouteKind(served)
	if !ok {
		return nil
	}
	objs := snap.Objects[k.GVR()]
	h := &httpRoutes{kind: k, objs: objs, views: make([]*objects.HTTPRouteView, len(objs)), byRef: make(map[objects.Ref]int, len(objs))}
	for i, u := range objs {
		h.views[i] = dec.HTTPRoute(u)
		h.byRef[objects.RefOf(u)] = i
	}
	return h
}

// isHTTPRoute reports whether group/kind is HTTPRoute.
func isHTTPRoute(group, kind string) bool {
	return group == objects.GatewayAPIGroup && kind == "HTTPRoute"
}

// delegate gives each install the in-scope HTTPRoutes reached from its owned
// HTTPRoutes over reported edges only (design §4.2). A breadth-first pass per
// install walks a reverse index of status entries built once; a visited set
// stops cycles, so the cost is linear in routes plus edges. A child is owned
// once, with every reported parent in RouteParents.
func delegate(h *httpRoutes, insts []Resolved, a *Assignment, inScope func(Resolved, string) bool) {
	if h == nil {
		return
	}
	k, routes, views, byRef := h.kind, h.objs, h.views, h.byRef
	children := map[objects.Ref][]int{} // parent HTTPRoute → routes with a status entry for it
	for i := range routes {
		for _, p := range views[i].Parents {
			if !isHTTPRoute(p.ParentRef.Group, p.ParentRef.Kind) {
				continue
			}
			r := p.ParentRef.Ref()
			if l := children[r]; len(l) == 0 || l[len(l)-1] != i {
				children[r] = append(l, i)
			}
		}
	}
	for _, in := range insts {
		o := a.PerInstall[in.ID()]
		o.routeIdx = make(map[objects.Ref]int, len(o.Routes))
		visited := make([]bool, len(routes))
		var queue []int
		for j, r := range o.Routes {
			ref := objects.RefOf(r.Obj)
			o.routeIdx[ref] = j
			if i, ok := byRef[ref]; ok && r.Kind.Group == k.Group && r.Kind.Kind == k.Kind {
				visited[i] = true
				queue = append(queue, i)
			}
		}
		for len(queue) > 0 {
			pi := queue[0]
			queue = queue[1:]
			pref := objects.RefOf(routes[pi])
			for _, ci := range children[pref] {
				c := routes[ci]
				if !inScope(in, c.GetNamespace()) {
					continue
				}
				es, ok := ReportedEdge(views[ci], pref, in.ControllerNames)
				if !ok {
					continue
				}
				cref := objects.RefOf(c)
				j, owned := o.routeIdx[cref]
				if !owned {
					j = len(o.Routes)
					o.routeIdx[cref] = j
					o.Routes = append(o.Routes, OwnedObject{Kind: k, Obj: c})
				}
				for _, e := range es {
					o.Routes[j].RouteParents = append(o.Routes[j].RouteParents, e.ParentRef)
				}
				if !visited[ci] {
					visited[ci] = true
					queue = append(queue, ci)
				}
			}
		}
	}
}

// ownedHTTPRoute reports whether ref is an HTTPRoute owned by o.
func ownedHTTPRoute(o *Owned, ref objects.Ref) bool {
	if !isHTTPRoute(ref.Group, ref.Kind) {
		return false
	}
	_, ok := o.Route(ref)
	return ok
}

// candidates adds, after the ownership pass, the declared candidates, the
// no-parent rows and the out-of-scope routes that name an owned HTTPRoute
// (KGWUI-004) of each install (design §4.2; PF10). Out-of-scope routes are
// never candidates. A route already unwatched for an install through a
// Gateway or ListenerSet gets no second entry.
func candidates(h *httpRoutes, insts []Resolved, a *Assignment, inScope func(Resolved, string) bool) {
	if h == nil {
		return
	}
	unwatched := map[string]map[objects.Ref]bool{} // install → routes already in a.Unwatched
	for _, u := range a.Unwatched {
		if unwatched[u.Install] == nil {
			unwatched[u.Install] = map[objects.Ref]bool{}
		}
		unwatched[u.Install][u.Route] = true
	}
	for _, in := range insts {
		id := in.ID()
		o := a.PerInstall[id]
		for i, u := range h.objs {
			v, ref := h.views[i], objects.RefOf(u)
			if !inScope(in, u.GetNamespace()) {
				if unwatched[id][ref] {
					continue
				}
				// A spec ref names an owned route, or an empty list reports one.
				var parent objects.Ref
				for _, p := range v.ParentRefs {
					if ownedHTTPRoute(o, p.Ref()) {
						parent = p.Ref()
						break
					}
				}
				if parent == (objects.Ref{}) && len(v.ParentRefs) == 0 {
					for _, e := range v.Parents {
						if slices.Contains(in.ControllerNames, e.ControllerName) && ownedHTTPRoute(o, e.ParentRef.Ref()) {
							parent = e.ParentRef.Ref()
							break
						}
					}
				}
				if parent != (objects.Ref{}) {
					a.Unwatched = append(a.Unwatched, UnwatchedRoute{Install: id, Route: ref, Gateway: parent})
				}
				continue
			}
			if len(v.ParentRefs) == 0 {
				if !slices.ContainsFunc(v.Parents, func(e objects.RouteParentStatus) bool { return slices.Contains(in.ControllerNames, e.ControllerName) }) {
					o.NoParent = append(o.NoParent, OwnedObject{Kind: h.kind, Obj: u})
				}
				continue
			}
			var seen []objects.Ref // parents already declared for this route
			for _, p := range v.ParentRefs {
				pref := p.Ref()
				if slices.Contains(seen, pref) || !ownedHTTPRoute(o, pref) {
					continue
				}
				if _, ok := ReportedEdge(v, pref, in.ControllerNames); ok {
					continue
				}
				seen = append(seen, pref)
				o.Declared = append(o.Declared, DeclaredCandidate{Route: OwnedObject{Kind: h.kind, Obj: u}, Parent: p})
			}
		}
	}
}

// assignBackends gives each install the in-scope Backends that a backendRef
// or mirror of an owned HTTPRoute names by full identity, and records every
// such ref (design §4.3).
func assignBackends(snap *cache.Snapshot, served []kinds.Served, insts []Resolved, a *Assignment, inScope func(Resolved, string) bool, h *httpRoutes) {
	bks := kinds.ByCategory(served, kinds.CategoryBackend)
	if h == nil || len(bks) == 0 {
		return
	}
	isBackend := func(r objects.Ref) bool {
		return slices.ContainsFunc(bks, func(k kinds.Served) bool { return k.Group == r.Group && k.Kind == r.Kind })
	}
	for _, in := range insts {
		o := a.PerInstall[in.ID()]
		refs := map[objects.Ref]bool{}
		for _, r := range o.Routes {
			i, ok := h.byRef[objects.RefOf(r.Obj)]
			if !ok || r.Kind.Group != h.kind.Group || r.Kind.Kind != h.kind.Kind {
				continue
			}
			for _, t := range objects.BackendTargets(h.views[i]) {
				if isBackend(t.Ref) && !refs[t.Ref] {
					refs[t.Ref] = true
					o.ReferencedBackends = append(o.ReferencedBackends, t.Ref)
				}
			}
		}
		if len(refs) == 0 {
			continue
		}
		for _, k := range bks {
			for _, b := range snap.Objects[k.GVR()] {
				if refs[objects.RefOf(b)] && inScope(in, b.GetNamespace()) {
					o.Backends = append(o.Backends, b)
				}
			}
		}
	}
}
