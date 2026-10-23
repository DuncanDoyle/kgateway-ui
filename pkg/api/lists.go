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
	h       *handler
	r       *http.Request
	p       *engine.Published
	i       *engine.InstallModel
	ix      *explore.Index
	q       explore.Query
	l       linker
	visible func(objects.Ref) bool // nil under AllowAll (design §7.4 rule 10)
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
	ls := &list{h: h, r: r, p: p, i: i, ix: ix, q: q, l: h.linker(r, p.Model, i)}
	if !allowsAll(h.authz) {
		ls.visible = func(ref objects.Ref) bool { return h.authz.Authorize(r, ref) }
	}
	return ls
}

// can reports whether the caller may view ref.
func (ls *list) can(ref objects.Ref) bool { return ls.visible == nil || ls.visible(ref) }

// count is the number of ids whose ref the caller may view (design §7.4 rule 5).
func (ls *list) count(ids []explore.RowID, ref func(explore.RowID) objects.Ref) int {
	if ls.visible == nil {
		return len(ids)
	}
	n := 0
	for _, id := range ids {
		if ls.visible(ref(id)) {
			n++
		}
	}
	return n
}

// denied is deniedInInstall: the caller's hidden rows among all n rows of
// the list, whatever the filters (design §7.4 rule 2).
func (ls *list) denied(n int, ref func(explore.RowID) objects.Ref) int {
	if ls.visible == nil {
		return 0
	}
	d := 0
	for i := range n {
		if !ls.visible(ref(explore.RowID(i))) { // #nosec G115 -- a row count fits in int32
			d++
		}
	}
	return d
}

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
	pg, err := ls.ix.QueryRoutes(ls.q, ls.visible)
	if err != nil {
		fail(w, err)
		return
	}
	routeRef := func(id explore.RowID) objects.Ref { return ls.ix.Routes[id].Ref }
	items := make([]RouteRow, 0, len(pg.IDs))
	for _, id := range pg.IDs {
		items = append(items, ls.routeRow(id))
	}
	writeJSON(w, page(ls, pg, items, ls.denied(len(ls.ix.Routes), routeRef)))
}

// routeRow is route row id on the wire.
func (ls *list) routeRow(id explore.RowID) RouteRow {
	r := &ls.ix.Routes[id]
	out := RouteRow{Ref: ls.l.linked(r.Ref), Hostnames: r.Hostnames, Parents: make([]ParentSummary, 0, len(r.Parents)),
		ParentRoutes: make([]LinkedRef, 0, len(r.ParentRoutes)), Children: r.Children, Flags: r.Flags.Names(), Health: r.State}
	if out.Hostnames == nil {
		out.Hostnames = []string{}
	}
	for _, p := range r.Parents {
		out.Parents = append(out.Parents, ls.parentSummary(p))
	}
	// Reported edges are the row's own status (rule 8); link() decides each.
	for _, p := range r.ParentRoutes {
		out.ParentRoutes = append(out.ParentRoutes, ls.l.linked(p))
	}
	if ls.visible != nil {
		kids := ls.ix.ByParentRoute[r.Ref]
		out.Children = ls.count(kids, func(id explore.RowID) objects.Ref { return ls.ix.Routes[id].Ref })
	}
	if len(ls.q.Backends) > 0 {
		out.Mirror = ls.mirrorOnly(id)
	}
	return out
}

// parentSummary is one parent link on the wire. Via comes from the
// ListenerSet, not the route, so a hidden Gateway is restricted (rule 3).
func (ls *list) parentSummary(p explore.ParentLink) ParentSummary {
	s := ParentSummary{Ref: ls.l.linked(p.Target), SectionName: p.Ref.SectionName, Port: p.Ref.Port, Attachment: p.Attachment}
	if p.Via != nil {
		if ls.can(*p.Via) {
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
	pg, err := ls.ix.QueryGateways(ls.q, ls.visible)
	if err != nil {
		fail(w, err)
		return
	}
	routeRef := func(id explore.RowID) objects.Ref { return ls.ix.Routes[id].Ref }
	lsRef := func(id explore.RowID) objects.Ref { return ls.ix.ListenerSets[id].Ref }
	items := make([]GatewayListRow, 0, len(pg.IDs))
	for _, id := range pg.IDs {
		g := &ls.ix.Gateways[id]
		row := GatewayListRow{Ref: ls.l.linked(g.Ref), Health: g.State, AttachedRoutes: g.AttachedRoutes,
			NotAccepted: ls.count(g.NotAccepted, routeRef), ListenerSets: ls.count(g.ListenerSets, lsRef), Addresses: []string{}}
		if v := g.View; v != nil {
			row.Class, row.Listeners = v.Class, len(v.Listeners)
			row.Addresses = append(row.Addresses, v.StatusAddresses...)
		}
		items = append(items, row)
	}
	gwRef := func(id explore.RowID) objects.Ref { return ls.ix.Gateways[id].Ref }
	writeJSON(w, page(ls, pg, items, ls.denied(len(ls.ix.Gateways), gwRef)))
}

func (h *handler) listenerSets(w http.ResponseWriter, r *http.Request) {
	ls := h.startList(w, r, listListenerSets)
	if ls == nil {
		return
	}
	pg, err := ls.ix.QueryListenerSets(ls.q, ls.visible)
	if err != nil {
		fail(w, err)
		return
	}
	items := make([]ListenerSetRow, 0, len(pg.IDs))
	for _, id := range pg.IDs {
		s := &ls.ix.ListenerSets[id]
		row := ListenerSetRow{Ref: ls.l.linked(s.Ref), Parent: ls.l.linked(s.Parent), Health: s.State, AcceptedReason: s.AcceptedReason}
		if s.View != nil {
			row.Listeners = len(s.View.Listeners)
		}
		items = append(items, row)
	}
	lsRef := func(id explore.RowID) objects.Ref { return ls.ix.ListenerSets[id].Ref }
	writeJSON(w, page(ls, pg, items, ls.denied(len(ls.ix.ListenerSets), lsRef)))
}
