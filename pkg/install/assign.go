package install

import (
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// OwnedObject is a route or policy together with its kind.
type OwnedObject struct {
	Kind kinds.Served
	Obj  *unstructured.Unstructured
	// Parents are the route's current spec.parentRefs that resolve to a Gateway
	// or ListenerSet of this install. Only their status entries
	// are evaluated. Empty for policies.
	Parents []objects.ParentRef
}

// UnattachedRoute is a route in an install's scope whose parentRef names a
// Gateway or ListenerSet that does not exist. It is a diagnostic candidate
// for KGWUI-003 and is not counted as owned.
type UnattachedRoute struct {
	Route  OwnedObject
	Parent objects.ParentRef
}

// Owned is everything assigned to one install.
type Owned struct {
	GatewayClasses []*unstructured.Unstructured
	Gateways       []*unstructured.Unstructured
	ListenerSets   []*unstructured.Unstructured
	Routes         []OwnedObject
	Policies       []OwnedObject
	Unattached     []UnattachedRoute
}

// AmbiguousGateway is a Gateway claimed by more than one install (KGWUI-009).
type AmbiguousGateway struct {
	Gateway  objects.Ref
	Installs []string
}

// UnwatchedRoute is a route that targets a Gateway of Install from a namespace
// that Install does not watch (KGWUI-004).
type UnwatchedRoute struct {
	Install string
	Route   objects.Ref
	Gateway objects.Ref
}

// Assignment maps resources to installs (design §4.3).
type Assignment struct {
	PerInstall map[string]*Owned
	Ambiguous  []AmbiguousGateway
	Unwatched  []UnwatchedRoute
}

// Assign assigns the snapshot's resources to insts. References are resolved
// by full group/kind/namespace/name identity.
func Assign(snap *cache.Snapshot, served []kinds.Served, insts []Resolved) Assignment {
	a := Assignment{PerInstall: map[string]*Owned{}}
	for _, in := range insts {
		a.PerInstall[in.ID()] = &Owned{}
	}
	nsByName := map[string]*corev1.Namespace{}
	for _, n := range snap.Namespaces {
		nsByName[n.Name] = n
	}
	inScope := func(in Resolved, ns string) bool { return in.Scope.Contains(ns, nsByName[ns]) }
	list := func(c kinds.Category) []OwnedObject {
		var out []OwnedObject
		for _, k := range kinds.ByCategory(served, c) {
			for _, o := range snap.Objects[k.GVR()] {
				out = append(out, OwnedObject{Kind: k, Obj: o})
			}
		}
		return out
	}

	// GatewayClass name → installs whose controller owns it.
	classOwners := map[string][]int{}
	for _, gc := range list(kinds.CategoryGatewayClass) {
		cn := objects.ControllerName(gc.Obj)
		for i, in := range insts {
			if slices.Contains(in.ControllerNames, cn) {
				classOwners[gc.Obj.GetName()] = append(classOwners[gc.Obj.GetName()], i)
				a.PerInstall[in.ID()].GatewayClasses = append(a.PerInstall[in.ID()].GatewayClasses, gc.Obj)
			}
		}
	}

	// Gateways: class controllerName ∩ namespace scope.
	gwOwner := map[objects.Ref]int{}
	exists := map[objects.Ref]bool{} // every Gateway and ListenerSet in the snapshot
	for _, gw := range list(kinds.CategoryGateway) {
		gref := objects.RefOf(gw.Obj)
		exists[gref] = true
		// An install with unknown scope competes only when no known scope contains the Gateway.
		var cands, unknown []int
		for _, i := range classOwners[objects.GatewayClassName(gw.Obj)] {
			switch {
			case insts[i].Scope.Mode == ScopeUnknown:
				unknown = append(unknown, i)
			case inScope(insts[i], gw.Obj.GetNamespace()):
				cands = append(cands, i)
			}
		}
		if len(cands) == 0 {
			cands = unknown
		}
		switch len(cands) {
		case 0:
		case 1:
			gwOwner[gref] = cands[0]
			o := a.PerInstall[insts[cands[0]].ID()]
			o.Gateways = append(o.Gateways, gw.Obj)
		default:
			amb := AmbiguousGateway{Gateway: gref}
			for _, i := range cands {
				amb.Installs = append(amb.Installs, insts[i].ID())
			}
			a.Ambiguous = append(a.Ambiguous, amb)
		}
	}

	// ListenerSets follow their parent Gateway when they are in scope themselves.
	lsGateway := map[objects.Ref]objects.Ref{}
	for _, ls := range list(kinds.CategoryListenerSet) {
		lref := objects.RefOf(ls.Obj)
		exists[lref] = true
		gref := objects.ListenerSetParent(ls.Obj).Ref()
		i, ok := gwOwner[gref]
		if !ok || !inScope(insts[i], ls.Obj.GetNamespace()) {
			continue
		}
		lsGateway[lref] = gref
		o := a.PerInstall[insts[i].ID()]
		o.ListenerSets = append(o.ListenerSets, ls.Obj)
	}

	// parentKinds are the served group/kinds a route parentRef can name; only
	// for these can the UI tell that the target does not exist.
	parentKinds := map[[2]string]bool{}
	for _, k := range append(kinds.ByCategory(served, kinds.CategoryGateway), kinds.ByCategory(served, kinds.CategoryListenerSet)...) {
		parentKinds[[2]string{k.Group, k.Kind}] = true
	}

	// Routes: the route's own namespace decides scope.
	for _, r := range list(kinds.CategoryRoute) {
		parents := map[int][]objects.ParentRef{} // install index → parentRefs resolving to its Gateways
		gateway := map[int]objects.Ref{}         // install index → first Gateway reached
		var order []int
		for _, p := range objects.RouteParentRefs(r.Obj) {
			pref := p.Ref()
			gref := pref
			if g, ok := lsGateway[pref]; ok {
				gref = g
			}
			if i, ok := gwOwner[gref]; ok {
				if _, seen := parents[i]; !seen {
					order = append(order, i)
					gateway[i] = gref
				}
				parents[i] = append(parents[i], p)
				continue
			}
			if parentKinds[[2]string{pref.Group, pref.Kind}] && !exists[pref] {
				for _, in := range insts {
					if inScope(in, r.Obj.GetNamespace()) {
						o := a.PerInstall[in.ID()]
						o.Unattached = append(o.Unattached, UnattachedRoute{Route: OwnedObject{Kind: r.Kind, Obj: r.Obj}, Parent: p})
					}
				}
			}
		}
		for _, i := range order {
			if inScope(insts[i], r.Obj.GetNamespace()) {
				o := a.PerInstall[insts[i].ID()]
				o.Routes = append(o.Routes, OwnedObject{Kind: r.Kind, Obj: r.Obj, Parents: parents[i]})
			} else {
				a.Unwatched = append(a.Unwatched, UnwatchedRoute{Install: insts[i].ID(), Route: objects.RefOf(r.Obj), Gateway: gateway[i]})
			}
		}
	}

	// ownerOf maps every assigned Gateway and ListenerSet to its install index.
	ownerOf := map[objects.Ref]int{}
	for ref, i := range gwOwner {
		ownerOf[ref] = i
	}
	for lref, gref := range lsGateway {
		ownerOf[lref] = gwOwner[gref]
	}

	// Policies.
	for _, p := range list(kinds.CategoryPolicy) {
		for i, in := range insts {
			if !inScope(in, p.Obj.GetNamespace()) {
				continue
			}
			if !claimable(p, i, in.ControllerNames, parentKinds, ownerOf) {
				continue
			}
			o := a.PerInstall[in.ID()]
			o.Policies = append(o.Policies, p)
		}
	}
	return a
}

// claimable reports whether an in-scope policy belongs to install i. An
// ancestor from i's controller names that is a Gateway or ListenerSet must be
// one i owns; if all such ancestors belong elsewhere, so does the policy. Any
// other ancestor from i's controller names (a backend, the status summary)
// claims the policy for i. Without such ancestors a kgateway policy belongs
// to i, and any other policy only when it has no ancestors at all.
func claimable(p OwnedObject, i int, controllers []string, parentKinds map[[2]string]bool, ownerOf map[objects.Ref]int) bool {
	as := objects.PolicyAncestors(p.Obj)
	mineByName := false
	for _, a := range as {
		if !slices.Contains(controllers, a.ControllerName) {
			continue
		}
		if !parentKinds[[2]string{a.AncestorRef.Group, a.AncestorRef.Kind}] {
			return true
		}
		mineByName = true
		if j, ok := ownerOf[a.AncestorRef.Ref()]; ok && j == i {
			return true
		}
	}
	if mineByName {
		return false
	}
	return p.Kind.Group == kinds.KgatewayGroup || len(as) == 0
}
