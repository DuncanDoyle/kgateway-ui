package explore

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
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
	// By is the owned object that names a referenced candidate.
	By *objects.Ref
	// NoParent marks a no-parent row; alone it admits the YAML view only.
	NoParent bool
}

// Admission returns why ref is admitted, or false when it is not.
func (ix *Index) Admission(ref objects.Ref) (Admission, bool) {
	a, ok := ix.admitted[ref]
	return a, ok
}

// admit fills the admission table of design §7.1: owned objects first, then
// current findings' resources, then what owned objects name by parentRef or
// backendRef, then no-parent rows. Admission does not follow chains.
func (b *builder) admit(ix *Index) map[objects.Ref]Admission {
	o := b.in.Owned
	n := len(ix.Routes) + len(b.in.Health.Findings)
	if o != nil {
		n += len(o.GatewayClasses) + len(o.Gateways) + len(o.ListenerSets) + len(o.Backends) + len(o.Policies)
	}
	m := make(map[objects.Ref]Admission, n)
	if o != nil {
		for _, l := range [][]*unstructured.Unstructured{o.GatewayClasses, o.Gateways, o.ListenerSets, o.Backends} {
			for _, u := range l {
				m[objects.RefOf(u)] = Admission{Owned: true}
			}
		}
		for _, l := range [][]install.OwnedObject{o.Routes, o.Policies} {
			for _, r := range l {
				m[objects.RefOf(r.Obj)] = Admission{Owned: true}
			}
		}
	}
	for _, f := range b.in.Health.Findings {
		if f.Resource != nil {
			if _, ok := m[*f.Resource]; !ok {
				m[*f.Resource] = Admission{Reason: ReasonFinding}
			}
		}
	}
	served := map[objects.GroupKind]bool{}
	for _, k := range b.in.Served {
		served[objects.GroupKind{Group: k.Group, Kind: k.Kind}] = true
	}
	refer := func(by *objects.Ref, ref objects.Ref) {
		if _, ok := m[ref]; !ok && served[objects.GroupKind{Group: ref.Group, Kind: ref.Kind}] {
			m[ref] = Admission{Reason: ReasonReferenced, By: by}
		}
	}
	for i := range ix.Routes {
		r := &ix.Routes[i]
		if !r.Owned {
			continue
		}
		d := b.drafts[i]
		for _, p := range d.specRefs {
			refer(&r.Ref, p.Ref())
		}
		for _, t := range d.targets {
			refer(&r.Ref, t.Ref)
		}
		if r.View != nil {
			for _, rule := range r.View.Rules {
				for _, br := range rule.BackendRefs {
					if dl := br.Delegation; dl != nil && dl.Mode == objects.DelegationName {
						refer(&r.Ref, objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: dl.Namespace, Name: dl.Name})
					}
				}
			}
		}
	}
	for i := range ix.ListenerSets {
		refer(&ix.ListenerSets[i].Ref, ix.ListenerSets[i].Parent)
	}
	for _, id := range ix.NoParent {
		ref := ix.Routes[id].Ref
		a := m[ref]
		a.NoParent = true
		m[ref] = a
	}
	return m
}
