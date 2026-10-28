package api

import (
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// linker labels references for one request against one model (design
// §7.1). Build one per request with linkerFor, so the detail kinds are
// computed once, not per reference.
type linker struct {
	v      *view
	m      *engine.Model
	i      *engine.InstallModel
	detail map[objects.GroupKind]bool
}

// linkerFor returns the linker for the caller of v and install i of m.
func linkerFor(v *view, m *engine.Model, i *engine.InstallModel) linker {
	detail := map[objects.GroupKind]bool{}
	for _, gk := range explore.DetailKinds(m.Served) {
		detail[gk] = true
	}
	return linker{v: v, m: m, i: i, detail: detail}
}

// link is admit(install, caller, ref) of design §7.1, the one place that
// joins admission, existence and the Authorizer. It is none when ref is
// denied, absent, never served or not admitted; detail when it is admitted
// and its kind has a detail view; else yaml. A referenced or selected
// candidate needs a referrer the caller may view. Admission never follows chains.
func (l linker) link(ref objects.Ref) Link {
	if l.i.Index == nil || !l.v.can(ref) {
		return LinkNone
	}
	a, ok := l.i.Index.Admission(ref)
	if !ok {
		return LinkNone
	}
	byReferrer := a.Reason == explore.ReasonReferenced || a.Reason == explore.ReasonSelected
	candidate := a.Owned || a.Reason == explore.ReasonFinding || (byReferrer && l.v.anyOf(a.Referrers))
	if !candidate && !a.NoParent {
		return LinkNone
	}
	if u, d := stored(l.m, ref); u == nil && d == nil {
		return LinkNone
	}
	if candidate && l.detail[objects.GroupKind{Group: ref.Group, Kind: ref.Kind}] {
		return LinkDetail
	}
	return LinkYAML
}

// linked is ref with its link.
func (l linker) linked(ref objects.Ref) LinkedRef {
	return LinkedRef{Ref: ref, Link: l.link(ref)}
}
