package api

import (
	"sync"

	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
)

// linker labels references for one request against one model (design
// §7.1). Build one per request with h.linker, so the detail kinds and the
// listed backends are computed once, not per reference.
type linker struct {
	v      *view
	m      *engine.Model
	i      *engine.InstallModel
	detail map[objects.GroupKind]bool
	// backends are the refs backendDetail serves this caller, built on first use; nil links no backend.
	backends func() map[objects.Ref]bool
}

// linkerFor returns the linker for the caller of v and install i of m, without backend links.
func linkerFor(v *view, m *engine.Model, i *engine.InstallModel) linker {
	detail := map[objects.GroupKind]bool{}
	for _, gk := range explore.DetailKinds(m.Served) {
		detail[gk] = true
	}
	return linker{v: v, m: m, i: i, detail: detail}
}

// linker returns the linker for the caller of v and install i of p. Its
// backend links come from t and proj when given, else from h.pathModel.
func (h *handler) linker(v *view, p *engine.Published, i *engine.InstallModel, t *paths.Topology, proj *paths.Projection) linker {
	l := linkerFor(v, p.Model, i)
	l.backends = sync.OnceValue(func() map[objects.Ref]bool {
		if t == nil {
			t, proj, _ = h.pathModel(p, i)
		}
		return h.listedBackends(p, i, v, t, proj)
	})
	return l
}

// link is admit, except that a ref backendDetail serves the caller is
// backend unless it is detail. Detail kinds are never backends in practice;
// should a route write one, its detail page wins.
func (l linker) link(ref objects.Ref) Link {
	k := l.admit(ref)
	if k != LinkDetail && l.backends != nil && l.v.can(ref) && l.backends()[ref] {
		return LinkBackend
	}
	return k
}

// admit is admit(install, caller, ref) of design §7.1, the one place that
// joins admission, existence and the Authorizer. It is none when ref is
// denied, absent, never served or not admitted; detail when it is admitted
// and its kind has a detail view; else yaml. A referenced or selected
// candidate needs a referrer the caller may view. Admission never follows chains.
func (l linker) admit(ref objects.Ref) Link {
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
