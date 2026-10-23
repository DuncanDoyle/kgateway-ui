package explore

import (
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// CandidateReason is why an object the install does not own is admitted.
type CandidateReason string

// Candidate reasons (design §7.1).
const (
	ReasonFinding    CandidateReason = "finding"
	ReasonReferenced CandidateReason = "referenced"
)

// Admission is why an object may be shown for the install (design §7.1).
type Admission struct {
	Owned  bool
	Reason CandidateReason
	// By is the first owned object that names a referenced candidate;
	// Referrers are all of them, in row order, then ListenerSet order. Under
	// a restricting Authorizer the handler shows the first visible one.
	By        *objects.Ref
	Referrers []objects.Ref
	// NoParent marks a no-parent row; alone it admits the YAML view only.
	NoParent bool
}

// Admission returns why ref is admitted, or false when it is not. Admitted
// does not mean the object exists: a referenced ref may name an absent one.
// The handler still looks the object up and applies the Authorizer.
func (ix *Index) Admission(ref objects.Ref) (Admission, bool) {
	id, row := ix.Row(ref)
	if row && ix.Routes[id].Owned {
		return Admission{Owned: true}, true
	}
	a, ok := ix.admitted[ref]
	if row && ix.Routes[id].Flags&FlagNoParent != 0 {
		a.NoParent, ok = true, true
	}
	return a, ok
}

// Row returns the row of route ref.
func (ix *Index) Row(ref objects.Ref) (RowID, bool) {
	i, ok := slices.BinarySearchFunc(ix.Routes, ref, func(r RouteRow, ref objects.Ref) int { return compareRefs(r.Ref, ref) })
	return RowID(i), ok // #nosec G115 -- a row count fits in int32
}

// admit fills the admission table of design §7.1 for what the rows do not
// answer: owned objects other than routes, then current findings'
// resources, then what owned objects name by parentRef or backendRef.
// Owned routes and no-parent rows are answered from the rows. Admission
// does not follow chains.
func (b *builder) admit(ix *Index) map[objects.Ref]Admission {
	m := map[objects.Ref]Admission{}
	o := b.in.Owned
	if o != nil {
		for _, l := range [][]*unstructured.Unstructured{o.GatewayClasses, o.Gateways, o.ListenerSets, o.Backends} {
			for _, u := range l {
				m[objects.RefOf(u)] = Admission{Owned: true}
			}
		}
		for _, p := range o.Policies {
			m[objects.RefOf(p.Obj)] = Admission{Owned: true}
		}
	}
	ownedRoute := func(ref objects.Ref) bool { id, ok := ix.Row(ref); return ok && ix.Routes[id].Owned }
	for _, f := range b.in.Health.Findings {
		if f.Resource == nil || ownedRoute(*f.Resource) {
			continue
		}
		if _, ok := m[*f.Resource]; !ok {
			m[*f.Resource] = Admission{Reason: ReasonFinding}
		}
	}
	served := map[objects.GroupKind]bool{}
	for _, k := range b.in.Served {
		served[objects.GroupKind{Group: k.Group, Kind: k.Kind}] = true
	}
	refer := func(by, ref objects.Ref) {
		if !served[objects.GroupKind{Group: ref.Group, Kind: ref.Kind}] || ownedRoute(ref) {
			return
		}
		a, ok := m[ref]
		switch {
		case !ok:
			a = Admission{Reason: ReasonReferenced}
		case a.Reason != ReasonReferenced:
			return // owned or a finding
		case a.Referrers[len(a.Referrers)-1] == by:
			return
		}
		a.Referrers = append(a.Referrers, by)
		m[ref] = a
	}
	for i := range ix.Routes {
		r := &ix.Routes[i]
		if !r.Owned {
			continue
		}
		a := &b.aux[i]
		for _, p := range a.specRefs {
			refer(r.Ref, p.Ref())
		}
		for _, t := range a.targets {
			refer(r.Ref, t.Ref)
		}
		if r.View != nil {
			for _, rule := range r.View.Rules {
				for _, br := range rule.BackendRefs {
					if dl := br.Delegation; dl != nil && dl.Mode == objects.DelegationName {
						refer(r.Ref, objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: dl.Namespace, Name: dl.Name})
					}
				}
			}
		}
	}
	for i := range ix.ListenerSets {
		refer(ix.ListenerSets[i].Ref, ix.ListenerSets[i].Parent)
	}
	for ref, a := range m {
		if a.Reason == ReasonReferenced {
			a.By = &a.Referrers[0]
			m[ref] = a
		}
	}
	return m
}
