package api

import (
	"net/http"
	"slices"

	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// link is admit(install, caller, ref) of design §7.1, the one place that
// joins admission, existence and the Authorizer. It is none when ref is
// denied, absent, never served or not admitted; detail when it is admitted
// and its kind has a detail view; else yaml. A referenced candidate needs a
// referrer the caller may view. Admission never follows chains.
func (h *handler) link(r *http.Request, m *engine.Model, i *engine.InstallModel, ref objects.Ref) Link {
	if i.Index == nil || !h.authz.Authorize(r, ref) {
		return LinkNone
	}
	a, ok := i.Index.Admission(ref)
	if !ok {
		return LinkNone
	}
	candidate := a.Owned || a.Reason == explore.ReasonFinding || (a.Reason == explore.ReasonReferenced && h.anyVisible(r, a.Referrers))
	if !candidate && !a.NoParent {
		return LinkNone
	}
	if u, d := stored(m, ref); u == nil && d == nil {
		return LinkNone
	}
	if candidate && slices.Contains(explore.DetailKinds(m.Served), objects.GroupKind{Group: ref.Group, Kind: ref.Kind}) {
		return LinkDetail
	}
	return LinkYAML
}

// linked is ref with its link.
func (h *handler) linked(r *http.Request, m *engine.Model, i *engine.InstallModel, ref objects.Ref) LinkedRef {
	return LinkedRef{Ref: ref, Link: h.link(r, m, i, ref)}
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
