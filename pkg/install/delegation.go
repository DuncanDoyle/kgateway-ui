package install

import (
	"slices"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// ReportedEdge returns child's status entry that gives it a reported edge to
// the HTTPRoute parent (design §4.2 conditions 2 and 3): the entry is from one
// of controllers, its parentRef resolves to parent, and it is eligible: a
// current spec parentRef equals it after defaults, or the spec list is empty.
// Freshness does not matter. Scope and ownership are the caller's to check.
func ReportedEdge(child *objects.HTTPRouteView, parent objects.Ref, controllers []string) (*objects.RouteParentStatus, bool) {
	for i := range child.Parents {
		e := &child.Parents[i]
		if e.ParentRef.Ref() != parent || !slices.Contains(controllers, e.ControllerName) {
			continue
		}
		if len(child.ParentRefs) == 0 || slices.Contains(child.ParentRefs, e.ParentRef) {
			return e, true
		}
	}
	return nil, false
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

// delegate gives each install the in-scope HTTPRoutes reached from its owned
// HTTPRoutes over reported edges only (design §4.2). A breadth-first pass per
// install walks a reverse index of status entries built once; a visited set
// stops cycles, so the cost is linear in routes plus edges. A child is owned
// once, with every reported parent in RouteParents.
func delegate(snap *cache.Snapshot, served []kinds.Served, insts []Resolved, a *Assignment, inScope func(Resolved, string) bool, dec objects.Decoder) {
	k, ok := httpRouteKind(served)
	if !ok {
		return
	}
	routes := snap.Objects[k.GVR()]
	views := make([]*objects.HTTPRouteView, len(routes))
	byRef := make(map[objects.Ref]int, len(routes))
	children := map[objects.Ref][]int{} // parent HTTPRoute → routes with a status entry for it
	for i, u := range routes {
		views[i] = dec.HTTPRoute(u)
		byRef[objects.RefOf(u)] = i
		for _, p := range views[i].Parents {
			if p.ParentRef.Group != objects.GatewayAPIGroup || p.ParentRef.Kind != "HTTPRoute" {
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
				e, ok := ReportedEdge(views[ci], pref, in.ControllerNames)
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
				o.Routes[j].RouteParents = append(o.Routes[j].RouteParents, e.ParentRef)
				if !visited[ci] {
					visited[ci] = true
					queue = append(queue, ci)
				}
			}
		}
	}
}
