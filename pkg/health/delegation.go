package health

import (
	"fmt"
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
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
		Evidence: []objects.Ref{parent}, ResourceSince: &own, parent: &d.Parent}
}

// computedTriggerFindings is KGWUI-011's computed and orphan triggers (design
// §6.2): one warning per mature delegated-not-attached or orphan-not-attached
// check, since its window start. Evidence leaves out the routes on the paths. None while the scope is unknown (D-024).
// conditionsCheck merges a computed one into the status trigger of its
// (route, parent).
func computedTriggerFindings(in Input) []Finding {
	v := in.Computed
	if v == nil || in.Install.Scope.Mode == install.ScopeUnknown {
		return nil
	}
	var fs []Finding
	var routes map[objects.Ref]*unstructured.Unstructured
	for i := range v.Checks {
		c := &v.Checks[i]
		if c.Class != computed.ClassTrigger || !c.Mature {
			continue
		}
		if routes == nil {
			routes = heldRoutes(in.Owned)
		}
		on, since := c.On, c.Since
		f := Finding{Code: CodeDelegatedNotAttached, Slug: "delegated-route-not-attached", Severity: SeverityWarning, Install: in.Install.ID(),
			Resource: &on, Message: ComputedNotAttachedMessage, Since: &since,
			// The detail quotes the routes on the paths by name only (design §7.4 rule 7, D-024).
			Evidence: withoutRoutes(c.Deps, c.Contributors)}
		if u := routes[on]; u != nil {
			own := in.firstSeen(u)
			f.ResourceSince = &own
		}
		switch c.ID.Kind {
		case computed.KindDelegatedNotAttached:
			parent, ok := c.Parent()
			if !ok {
				continue
			}
			f.Detail = computedReason(parent, c.Expected)
			f.parent = &objects.ParentRef{Group: parent.Group, Kind: parent.Kind, Namespace: parent.Namespace, Name: parent.Name}
		case computed.KindOrphan:
			f.Detail = OrphanDetail
		default:
			continue
		}
		fs = append(fs, f)
	}
	return fs
}

// computedReason is the computed trigger's reason: parent drops the route, or
// the route's parentRefs forbid it.
func computedReason(parent objects.Ref, outcome string) string {
	if outcome == string(computed.OutcomeForbidden) {
		return fmt.Sprintf(computedForbidden, parent.String())
	}
	return fmt.Sprintf(computedDropped, parent.String())
}

// withoutRoutes is deps without routes, a sorted subslice of it (computed.CheckResult.Contributors).
func withoutRoutes(deps, routes []objects.Ref) []objects.Ref {
	if len(routes) == 0 {
		return deps
	}
	out := make([]objects.Ref, 0, len(deps)-len(routes))
	for _, d := range deps {
		if _, found := slices.BinarySearchFunc(routes, d, objects.Ref.Compare); !found {
			out = append(out, d)
		}
	}
	return out
}

// heldRoutes maps the install's owned routes, declared candidates and routes
// with no parent by ref, built once per run; the first of duplicates wins.
func heldRoutes(o *install.Owned) map[objects.Ref]*unstructured.Unstructured {
	if o == nil {
		return map[objects.Ref]*unstructured.Unstructured{}
	}
	m := make(map[objects.Ref]*unstructured.Unstructured, len(o.Routes)+len(o.Declared)+len(o.NoParent))
	add := func(u *unstructured.Unstructured) {
		if r := objects.RefOf(u); m[r] == nil {
			m[r] = u
		}
	}
	for _, r := range o.Routes {
		add(r.Obj)
	}
	for _, d := range o.Declared {
		add(d.Route.Obj)
	}
	for _, r := range o.NoParent {
		add(r.Obj)
	}
	return m
}

// mergeTriggers adds the computed trigger findings cs to fs, whose status
// trigger findings start at declared. A computed finding on the (route,
// parent) of a status one is merged into it: the computed reason is
// appended, the higher severity and the computed window start kept, and the
// evidence united without routes (PF13, D-024). The caller has already read the status
// severity for its grace.
func mergeTriggers(fs []Finding, declared int, cs []Finding) []Finding {
	for _, c := range cs {
		i := -1
		if c.parent != nil {
			i = slices.IndexFunc(fs[declared:], func(f Finding) bool {
				return f.Code == CodeDelegatedNotAttached && *f.Resource == *c.Resource && f.parent != nil && f.parent.Ref() == c.parent.Ref()
			})
		}
		if i < 0 {
			fs = append(fs, c)
			continue
		}
		f := &fs[declared+i]
		f.Detail += ". " + c.Detail
		f.Since = c.Since
		if rank(c.Severity) < rank(f.Severity) {
			f.Severity = c.Severity
		}
		// Merging needs the same (route, parent): the status trigger's only
		// evidence is that parent, which the computed trigger puts on the
		// route's delegation paths. An ancestor, so no route is kept (D-024, rule 7).
		ev := make([]objects.Ref, 0, len(f.Evidence)+len(c.Evidence))
		for _, r := range slices.Concat(f.Evidence, c.Evidence) {
			if (r.Group != objects.GatewayAPIGroup || r.Kind != "HTTPRoute") && !slices.Contains(ev, r) {
				ev = append(ev, r)
			}
		}
		f.Evidence = ev
	}
	return fs
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
