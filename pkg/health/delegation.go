package health

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// graceStart returns the latest first-seen time of us. Each lookup goes
// through Input.FirstSeen, so an object still in grace schedules its own
// grace deadline in the engine and the latest one matures the finding.
func graceStart(in Input, us ...*unstructured.Unstructured) time.Time {
	var t time.Time
	for _, u := range us {
		if s := in.firstSeen(u); s.After(t) {
			t = s
		}
	}
	return t
}

// declaredFinding is KGWUI-011's status trigger for d (design §6.2): info
// while the later of the route's and the parent's current generation was
// first seen less than Grace ago, warning after. Its clock is its own.
func declaredFinding(in Input, d install.DeclaredCandidate) Finding {
	parent := d.Parent.Ref()
	us := []*unstructured.Unstructured{d.Route.Obj}
	if p, ok := in.Owned.Route(parent); ok {
		us = append(us, p.Obj)
	}
	sev := SeverityWarning
	if in.Now.Sub(graceStart(in, us...)) < Grace {
		sev = SeverityInfo
	}
	own := in.firstSeen(d.Route.Obj)
	return Finding{Code: CodeDelegatedNotAttached, Slug: "delegated-route-not-attached", Severity: sev, Install: in.Install.ID(),
		Resource: ref(d.Route.Obj), Message: DelegatedNotAttachedMessage, Detail: fmt.Sprintf(DelegatedNotAttachedDetail, parent.String()),
		Evidence: []objects.Ref{parent}, ResourceSince: &own, parent: d.Parent}
}

// delegationOnly reports whether u is an HTTPRoute whose every backendRef
// delegates and which has no ExtensionRef filter, so a BackendNotFound on it
// can only come from delegation (computed-rules §3.2, §8.2).
func delegationOnly(u *unstructured.Unstructured) bool {
	gvk := u.GroupVersionKind()
	if gvk.Group != objects.GatewayAPIGroup || gvk.Kind != "HTTPRoute" {
		return false
	}
	extension := func(fs []objects.Filter) bool {
		for _, f := range fs {
			if f.ExtensionRef != nil {
				return true
			}
		}
		return false
	}
	delegates := false
	for _, r := range objects.DecodeHTTPRoute(u).Rules {
		if extension(r.Filters) {
			return false
		}
		for _, b := range r.BackendRefs {
			if b.Delegation == nil || extension(b.Filters) {
				return false
			}
			delegates = true
		}
	}
	return delegates
}

// delegationCondition rewords a route's ResolvedRefs=False/BackendNotFound
// when only delegation can cause it (D-021 item 9); code and severity stay.
func delegationCondition(f *Finding, u *unstructured.Unstructured, c metav1.Condition) {
	if c.Type != "ResolvedRefs" || c.Reason != "BackendNotFound" || !delegationOnly(u) {
		return
	}
	f.Message, f.Detail = DelegationNoChildMessage, DelegationNoChildDetail+"; "+f.Detail
}
