package api

import (
	"net/http"
	"slices"

	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// linker labels references for one request against one model (design
// §7.1). Build one per request with h.linker, so the detail kinds are
// computed once, not per reference.
type linker struct {
	h      *handler
	r      *http.Request
	m      *engine.Model
	i      *engine.InstallModel
	detail map[objects.GroupKind]bool
}

// linker returns the linker for the caller of r and install i of m.
func (h *handler) linker(r *http.Request, m *engine.Model, i *engine.InstallModel) linker {
	detail := map[objects.GroupKind]bool{}
	for _, gk := range explore.DetailKinds(m.Served) {
		detail[gk] = true
	}
	return linker{h: h, r: r, m: m, i: i, detail: detail}
}

// link is admit(install, caller, ref) of design §7.1, the one place that
// joins admission, existence and the Authorizer. It is none when ref is
// denied, absent, never served or not admitted; detail when it is admitted
// and its kind has a detail view; else yaml. A referenced candidate needs a
// referrer the caller may view. Admission never follows chains.
func (l linker) link(ref objects.Ref) Link {
	h, r := l.h, l.r
	if l.i.Index == nil || !h.authz.Authorize(r, ref) {
		return LinkNone
	}
	a, ok := l.i.Index.Admission(ref)
	if !ok {
		return LinkNone
	}
	candidate := a.Owned || a.Reason == explore.ReasonFinding || (a.Reason == explore.ReasonReferenced && h.anyVisible(r, a.Referrers))
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

// anyVisible reports whether the caller of r may view at least one of refs.
func (h *handler) anyVisible(r *http.Request, refs []objects.Ref) bool {
	if allowsAll(h.authz) {
		return len(refs) > 0
	}
	return slices.ContainsFunc(refs, func(ref objects.Ref) bool { return h.authz.Authorize(r, ref) })
}

// allVisible reports whether the caller of r may view every one of refs.
func (h *handler) allVisible(r *http.Request, refs []objects.Ref) bool {
	if allowsAll(h.authz) {
		return true
	}
	return !slices.ContainsFunc(refs, func(ref objects.Ref) bool { return !h.authz.Authorize(r, ref) })
}
