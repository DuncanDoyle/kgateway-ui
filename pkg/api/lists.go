package api

import (
	"errors"
	"net/http"
	"slices"

	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/explore"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// list is one list request after its install was found and its query parsed.
type list struct {
	h  *handler
	r  *http.Request
	p  *engine.Published
	i  *engine.InstallModel
	ix *explore.Index
	q  explore.Query
	v  *view
	l  linker
	f  facts
}

// startList finds the install and parses the query, or writes 503, 404 or 400.
func (h *handler) startList(w http.ResponseWriter, r *http.Request, kind listKind) *list {
	p, i := h.find(w, r)
	if i == nil {
		return nil
	}
	q, err := parseQuery(r, kind)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return nil
	}
	ix := i.Index
	if ix == nil {
		ix = &explore.Index{}
	}
	v := h.view(r)
	l := linkerFor(v, p.Model, i)
	return &list{h: h, r: r, p: p, i: i, ix: ix, q: q, v: v, l: l, f: facts{ix: ix, v: v, l: l}}
}

func (ls *list) routeRef(id explore.RowID) objects.Ref { return ls.ix.Routes[id].Ref }
func (ls *list) gwRef(id explore.RowID) objects.Ref    { return ls.ix.Gateways[id].Ref }
func (ls *list) lsRef(id explore.RowID) objects.Ref    { return ls.ix.ListenerSets[id].Ref }

// fail writes 400 for a query the index rejects; anything else is a bug.
func fail(w http.ResponseWriter, err error) {
	if errors.Is(err, explore.ErrBadQuery) || errors.Is(err, explore.ErrBadCursor) {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeErr(w, http.StatusInternalServerError, "internal error")
}

// page wraps items with pg's totals and cursors.
func page[T any](ls *list, pg explore.Page, items []T, denied int) Page[T] {
	return Page[T]{Install: ls.i.Resolved.ID(), Items: items, Total: pg.Total, Next: pg.Next, Prev: pg.Prev,
		DeniedInInstall: denied, CollectedAt: wireTime(ls.p.ConfirmedAt)}
}

func (h *handler) routes(w http.ResponseWriter, r *http.Request) {
	ls := h.startList(w, r, listRoutes)
	if ls == nil {
		return
	}
	ls.v.borrow()
	defer ls.v.release()
	pg, err := ls.ix.QueryRoutes(ls.q, ls.v.visible())
	if err != nil {
		fail(w, err)
		return
	}
	items := make([]RouteRow, 0, len(pg.IDs))
	for _, id := range pg.IDs {
		items = append(items, ls.routeRow(id))
	}
	writeJSON(w, page(ls, pg, items, ls.v.deniedInInstall(len(ls.ix.Routes), ls.routeRef)))
}

// routeRow is route row id on the wire.
func (ls *list) routeRow(id explore.RowID) RouteRow {
	r := &ls.ix.Routes[id]
	out := RouteRow{Ref: ls.l.linked(r.Ref), Hostnames: r.Hostnames, Parents: make([]ParentSummary, 0, len(r.Parents)),
		ParentRoutes: make([]LinkedRef, 0, len(r.ParentRoutes)), Children: r.Children, Flags: ls.ix.RowFlags(id, ls.v.visible()).Names(), Health: r.State}
	if out.Hostnames == nil {
		out.Hostnames = []string{}
	}
	for _, p := range r.Parents {
		out.Parents = append(out.Parents, ls.parentSummary(id, p))
	}
	// Reported edges are the row's own status (rule 8); link() decides each.
	for _, p := range r.ParentRoutes {
		out.ParentRoutes = append(out.ParentRoutes, ls.l.linked(p))
	}
	if !ls.v.all {
		out.Children = ls.v.recount(ls.ix.ByParentRoute[r.Ref], ls.routeRef)
	}
	if len(ls.q.Backends) > 0 {
		out.Mirror = ls.mirrorOnly(id)
	}
	return out
}

// parentSummary is one parent link on the wire. Via comes from the
// ListenerSet's spec, so it is sent only when the caller may view both the
// ListenerSet and its Gateway (rule 3). Computed is view A for row id's parentRef.
func (ls *list) parentSummary(id explore.RowID, p explore.ParentLink) ParentSummary {
	s := ParentSummary{Ref: ls.l.linked(p.Target), SectionName: p.Ref.SectionName, Port: p.Ref.Port, Attachment: p.Attachment}
	if p.Via != nil {
		if ls.v.can(p.Target) && ls.v.can(*p.Via) {
			v := ls.l.linked(*p.Via)
			s.Via = &v
		} else {
			s.ViaRestricted = true
		}
	}
	switch {
	case p.Cond != nil:
		s.ConditionType, s.Reason, s.Message = p.Cond.Type, p.Cond.Reason, p.Cond.Message
	case p.Attachment == explore.AttachmentPending:
		s.Reason = p.Pending
	}
	if f := ls.ix.Facts(id, p); f != nil {
		a := ls.f.attachment(f)
		for k := range p.Dups {
			if d := ls.ix.DupFacts(id, p, k); d != nil {
				a = mergeAttachment(a, ls.f.attachment(d))
			}
		}
		s.Computed = &a
	}
	return s
}

// mirrorOnly reports whether every use of row id among the queried
// backends is a RequestMirror. It reads the row's own spec only (rule 2a).
func (ls *list) mirrorOnly(id explore.RowID) bool {
	matched := false
	for _, b := range ls.q.Backends {
		uses := ls.ix.ByBackend[b]
		j, ok := slices.BinarySearchFunc(uses, id, func(u explore.BackendUse, id explore.RowID) int { return int(u.Row - id) })
		if !ok {
			continue
		}
		if !uses[j].Mirror {
			return false
		}
		matched = true
	}
	return matched
}

func (h *handler) gateways(w http.ResponseWriter, r *http.Request) {
	ls := h.startList(w, r, listGateways)
	if ls == nil {
		return
	}
	pg, err := ls.ix.QueryGateways(ls.q, ls.v.visible())
	if err != nil {
		fail(w, err)
		return
	}
	items := make([]GatewayListRow, 0, len(pg.IDs))
	for _, id := range pg.IDs {
		g := &ls.ix.Gateways[id]
		row := GatewayListRow{Ref: ls.l.linked(g.Ref), Health: g.State, AttachedRoutes: g.AttachedRoutes,
			NotAccepted: ls.notAccepted(g), ListenerSets: ls.v.recount(g.ListenerSets, ls.lsRef), Disagreements: g.Disagreements, Addresses: []string{}}
		if v := g.View; v != nil {
			row.Class, row.Listeners = v.Class, len(v.Listeners)
			row.Addresses = append(row.Addresses, v.StatusAddresses...)
		}
		if n, ok := ls.ix.GatewayMemberCount(id); ok {
			row.Routes = &n
		}
		items = append(items, row)
	}
	writeJSON(w, page(ls, pg, items, ls.v.deniedInInstall(len(ls.ix.Gateways), ls.gwRef)))
}

// notAccepted counts g's not-accepted routes the caller may view, through
// visible ListenerSets only (rule 3, rule 5).
func (ls *list) notAccepted(g *explore.GatewayRow) int {
	if ls.v.all {
		return len(g.NotAccepted)
	}
	n := 0
	for _, id := range g.NotAccepted {
		if ls.v.can(ls.ix.Routes[id].Ref) && ls.ix.Under(id, g.Ref, explore.AttachmentNotAccepted, ls.v.can) {
			n++
		}
	}
	return n
}

func (h *handler) listenerSets(w http.ResponseWriter, r *http.Request) {
	ls := h.startList(w, r, listListenerSets)
	if ls == nil {
		return
	}
	pg, err := ls.ix.QueryListenerSets(ls.q, ls.v.visible())
	if err != nil {
		fail(w, err)
		return
	}
	items := make([]ListenerSetRow, 0, len(pg.IDs))
	for _, id := range pg.IDs {
		s := &ls.ix.ListenerSets[id]
		row := ListenerSetRow{Ref: ls.l.linked(s.Ref), Parent: ls.l.linked(s.Parent), Health: s.State, AcceptedReason: s.AcceptedReason,
			Disagreements: s.Disagreements}
		if s.View != nil {
			row.Listeners = len(s.View.Listeners)
		}
		if n, ok := ls.ix.ListenerSetMemberCount(id); ok {
			row.Routes = &n
		}
		items = append(items, row)
	}
	writeJSON(w, page(ls, pg, items, ls.v.deniedInInstall(len(ls.ix.ListenerSets), ls.lsRef)))
}
