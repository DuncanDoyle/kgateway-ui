package engine

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// computeViews runs sel's view A over one install and cross-checks it. The
// checks' State, Mature and Since are left for the persistence step.
func computeViews(sel computed.Selection, res install.Resolved, owned *install.Owned, snap *cache.Snapshot,
	served []kinds.Served, dec objects.Decoder,
) *computed.Views {
	in := attachmentInput(res, owned, snap, served, dec)
	att := sel.Rules.Attachment(in)
	return &computed.Views{RuleSet: sel.Rules.Version(), Note: sel.Note, Attachment: att,
		Checks: computed.CheckAttachment(res.ID(), sel, in, att)}
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
