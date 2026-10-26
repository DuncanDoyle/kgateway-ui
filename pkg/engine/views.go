package engine

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// computeViews runs sel's views A, B and C over one install, within bounds,
// and cross-checks them. The checks' State, Mature and Since are left for
// the persistence step.
func computeViews(sel computed.Selection, res install.Resolved, owned *install.Owned, snap *cache.Snapshot,
	served []kinds.Served, dec objects.Decoder, bounds computed.Bounds,
) *computed.Views {
	in := attachmentInput(res, owned, snap, served, dec)
	att := sel.Rules.Attachment(in)
	a := computed.CheckAttachment(res.ID(), sel, in, att)
	din := delegationInput(res, owned, snap, served, dec, att)
	din.Bounds = bounds
	v := &computed.Views{RuleSet: sel.Rules.Version(), Note: sel.Note, Attachment: att, Delegation: sel.Rules.Delegation(din)}
	d := computed.CheckDelegation(res.ID(), sel, din, &v.Delegation)
	v.Checks = append(append(make([]computed.CheckResult, 0, len(a)+len(d)), a...), d...)
	return v
}

// httpRoutes maps the snapshot's HTTPRoutes by ref.
func httpRoutes(snap *cache.Snapshot, served []kinds.Served) map[objects.Ref]*unstructured.Unstructured {
	for _, k := range kinds.ByCategory(served, kinds.CategoryRoute) {
		if k.Group == objects.GatewayAPIGroup && k.Kind == "HTTPRoute" {
			us := snap.Objects[k.GVR()]
			m := make(map[objects.Ref]*unstructured.Unstructured, len(us))
			for _, u := range us {
				m[objects.RefOf(u)] = u
			}
			return m
		}
	}
	return map[objects.Ref]*unstructured.Unstructured{}
}

// attachmentInput is view A's input for one install (rules §3.1, §4.6): its
// owned Gateways and ListenerSets, and the owned in-scope routes whose
// parentRefs name one of them. Views come from dec, so they are shared and read-only.
func attachmentInput(res install.Resolved, owned *install.Owned, snap *cache.Snapshot, served []kinds.Served, dec objects.Decoder) computed.AttachmentInput {
	labels, ok := install.NamespaceLabels(snap)
	in := computed.AttachmentInput{NamespaceLabels: labels, NamespacesOK: ok, Experimental: res.Settings.Experimental,
		ControllerNames: res.ControllerNames, ScopeUnknown: res.Scope.Mode == install.ScopeUnknown}
	if owned == nil {
		return in
	}
	in.Resources = make([]computed.Resource, 0, len(owned.Gateways)+len(owned.ListenerSets))
	for _, u := range owned.Gateways {
		v := dec.Gateway(u)
		in.Resources = append(in.Resources, computed.Resource{Ref: objects.RefOf(u), Generation: u.GetGeneration(),
			Listeners: v.Listeners, ListenerStatus: v.ListenerStatus})
	}
	for _, u := range owned.ListenerSets {
		// v.Parent is objects.ListenerSetParent(u), decoded once.
		v := dec.ListenerSet(u)
		in.Resources = append(in.Resources, computed.Resource{Ref: objects.RefOf(u), Parent: v.Parent.Ref(), Generation: u.GetGeneration(),
			Listeners: v.Listeners, ListenerStatus: v.ListenerStatus, Acceptance: acceptance(u, v)})
	}

	// The experimental classification depends only on the kind and controller version.
	ctrl := res.KgatewayVersion()
	exp := map[objects.GroupKind]bool{}
	for _, k := range kinds.ByCategory(served, kinds.CategoryRoute) {
		exp[objects.GroupKind{Group: k.Group, Kind: k.Kind}] = k.ExperimentalOnly(ctrl)
	}
	in.Routes = make([]computed.Route, 0, len(owned.Routes))
	for _, r := range owned.Routes {
		// Delegated children without a Gateway or ListenerSet parent are view B's.
		if len(r.Parents) == 0 {
			continue
		}
		rt := computed.Route{Ref: objects.RefOf(r.Obj), Generation: r.Obj.GetGeneration(),
			ExperimentalOnly: exp[objects.GroupKind{Group: r.Kind.Group, Kind: r.Kind.Kind}]}
		if r.Kind.Group == objects.GatewayAPIGroup && r.Kind.Kind == "HTTPRoute" {
			v := dec.HTTPRoute(r.Obj)
			rt.Hostnames, rt.ParentRefs, rt.Written, rt.Parents = v.Hostnames, v.ParentRefs, v.WrittenRefs, v.Parents
		} else {
			v := dec.Route(r.Obj)
			rt.Hostnames, rt.ParentRefs, rt.Written, rt.Parents = v.Hostnames, v.ParentRefs, v.WrittenRefs, v.Parents
		}
		in.Routes = append(in.Routes, rt)
	}
	return in
}

// acceptance is a ListenerSet's own acceptance from its current Accepted
// condition; a missing or stale one is NoStatus (rules §3.1, §3.3).
func acceptance(u *unstructured.Unstructured, v *objects.ListenerSetView) computed.Acceptance {
	c := objects.FindCondition(v.Conditions, "Accepted")
	switch {
	case c == nil || objects.IsStale(u, *c):
		return computed.NoStatus
	case c.Status == metav1.ConditionFalse:
		return computed.Denied
	}
	return computed.Accepted
}

// delegationInput is views B and C's input for one install (rules §5.1, §5.6):
// every in-scope HTTPRoute, those outside the scope, and the roots taken from
// view A a. Views come from dec, so they are shared and read-only.
func delegationInput(res install.Resolved, owned *install.Owned, snap *cache.Snapshot, served []kinds.Served, dec objects.Decoder, a computed.AttachmentResult) computed.DelegationInput {
	in := computed.DelegationInput{Wildcard: res.Settings.WildcardNamespace, ControllerNames: res.ControllerNames,
		Bounds: computed.DefaultBounds, ScopeUnknown: res.Scope.Mode == install.ScopeUnknown, Unwatched: map[objects.Ref]bool{}}
	nss := make(map[string]int, len(snap.Namespaces)) // name → index in snap.Namespaces
	for i, n := range snap.Namespaces {
		nss[n.Name] = i
	}
	inScope := func(ns string) bool {
		if i, ok := nss[ns]; ok {
			return res.Scope.Contains(ns, snap.Namespaces[i])
		}
		return res.Scope.Contains(ns, nil)
	}
	own := map[objects.Ref][]objects.ParentRef{} // owned HTTPRoute → its Gateway and ListenerSet parents
	if owned != nil {
		for _, r := range owned.Routes {
			if r.Kind.Group == objects.GatewayAPIGroup && r.Kind.Kind == "HTTPRoute" {
				own[objects.RefOf(r.Obj)] = r.Parents
			}
		}
	}
	objs := map[objects.Ref]*unstructured.Unstructured{}
	var us []*unstructured.Unstructured
	for _, k := range kinds.ByCategory(served, kinds.CategoryRoute) {
		if k.Group == objects.GatewayAPIGroup && k.Kind == "HTTPRoute" {
			us = snap.Objects[k.GVR()]
		}
	}
	{
		for _, u := range us {
			ref := objects.RefOf(u)
			if !inScope(u.GetNamespace()) {
				in.Unwatched[ref] = true
				continue
			}
			parents, isOwned := own[ref]
			objs[ref] = u
			in.Routes = append(in.Routes, computed.DelegRoute{Ref: ref, Generation: u.GetGeneration(), View: dec.HTTPRoute(u),
				Owned: isOwned, TopLevel: len(parents) > 0})
		}
	}
	slices.SortFunc(in.Routes, func(x, y computed.DelegRoute) int { return x.Ref.Compare(y.Ref) })
	in.Roots = computed.Roots(in.Routes, a, func(r computed.DelegRoute) (objects.Ref, computed.ReportKey, bool) {
		return statusTarget(res, r, objs[r.Ref], own[r.Ref])
	})
	return in
}

// statusTarget is the target and report key of r's first current
// Accepted=True status entry from the install's controllers that names one of
// parents, the route's Gateway and ListenerSet parents of this install.
func statusTarget(res install.Resolved, r computed.DelegRoute, u *unstructured.Unstructured, parents []objects.ParentRef) (objects.Ref, computed.ReportKey, bool) {
	for _, e := range r.View.Parents {
		if !slices.Contains(res.ControllerNames, e.ControllerName) {
			continue
		}
		c := objects.FindCondition(e.Conditions, "Accepted")
		if c == nil || c.Status != metav1.ConditionTrue || objects.IsStale(u, *c) {
			continue
		}
		if slices.ContainsFunc(parents, func(p objects.ParentRef) bool { return p.Ref() == e.ParentRef.Ref() }) {
			return e.ParentRef.Ref(), computed.KeyOf(e.Written), true
		}
	}
	return objects.Ref{}, computed.ReportKey{}, false
}
