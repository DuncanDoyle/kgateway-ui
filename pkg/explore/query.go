package explore

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Query errors; the handler answers both with 400 (design §7.2).
var (
	ErrBadCursor = errors.New("bad cursor")
	ErrBadQuery  = errors.New("bad query")
)

// Sort keys (design §7.2).
const (
	sortName   = "name"
	sortKind   = "kind"
	sortHealth = "health"
	sortRoutes = "routes" // Gateways only
)

// Page limits (design §7.2).
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// Sort is a list order. The empty key is name. Every order ends with the
// ascending (namespace, name, group, kind) tie-break; for name that tuple is
// the order itself, so name.desc reverses it.
type Sort struct {
	Key  string
	Desc bool
}

// ListenerUnknown is the listener filter value that selects unknown refs.
// No listener name can contain "_", so it never shadows a listener.
const ListenerUnknown = "_unknown"

// Query is one list request after parsing (design §7.2). Zero fields do not
// filter. Kinds, Parent, Attachment, Listener, Backends and States apply to
// routes only; Parent on ListenerSets is a Gateway.
type Query struct {
	Namespaces []string
	Kinds      []objects.GroupKind
	Parent     *objects.Ref
	Attachment Attachment
	// Listener needs a Gateway or ListenerSet Parent: a listener name selects
	// its computed members; ListenerUnknown the rows whose ref to Parent is unknown.
	Listener string
	// Backends are OR-ed through ByBackend: specs only, never the backend object.
	Backends []objects.Ref
	States   Flag
	Health   []health.State
	Q        string
	Sort     Sort
	// Limit is 1–MaxLimit; 0 means DefaultLimit.
	Limit  int
	Cursor string
}

// Page is one page of a list. IDs index the list the query ran on. Total
// counts every visible row that matches, not only this page.
type Page struct {
	IDs        []RowID
	Total      int
	Next, Prev string
}

// sortVal is a row's sort value: group and kind for kind; n is the health
// rank or the member count, -1 when the caller has no count.
type sortVal struct {
	group, kind string
	n           int
}

// entry is one matching row with its sort key.
type entry struct {
	id  RowID
	ref objects.Ref
	v   sortVal
}

// healthStates are in ascending sort order: health.desc lists errors first.
var healthStates = []health.State{health.StateHealthy, health.StateWarning, health.StateUnknown, health.StateError}

// validHealth reports whether s is one of healthStates.
func validHealth(s health.State) bool { return slices.Contains(healthStates, s) }

// healthRank returns s's position in healthStates. A state outside them,
// such as "", ranks as unknown, so sorting and cursors never fail.
func healthRank(s health.State) int {
	if i := slices.Index(healthStates, s); i >= 0 {
		return i
	}
	return slices.Index(healthStates, health.StateUnknown)
}

// compareEntries orders a and b under s (design §3.6).
func compareEntries(a, b entry, s Sort) int {
	c := 0
	switch s.Key {
	case sortName:
		c = compareRefs(a.ref, b.ref)
	case sortKind:
		c = cmp.Or(cmp.Compare(a.v.group, b.v.group), cmp.Compare(a.v.kind, b.v.kind))
	case sortRoutes:
		// A row without a count (-1) sorts after rows with one, either way.
		if none := cmp.Compare(noCount(a.v.n), noCount(b.v.n)); none != 0 {
			return none
		}
		c = cmp.Compare(a.v.n, b.v.n)
	default:
		c = cmp.Compare(a.v.n, b.v.n)
	}
	if s.Desc {
		c = -c
	}
	return cmp.Or(c, compareRefs(a.ref, b.ref))
}

// noCount is 1 for a row without a member count, else 0.
func noCount(n int) int {
	if n < 0 {
		return 1
	}
	return 0
}

// query is a validated query with its decoded cursor.
type query struct {
	Query
	cur *cursor
	q   string // lower-cased Q
}

// prepare validates q for a list whose sort keys are keys.
func prepare(q Query, keys ...string) (query, error) {
	if q.Sort.Key == "" {
		q.Sort.Key = sortName
	}
	if !slices.Contains(keys, q.Sort.Key) {
		return query{}, fmt.Errorf("%w: sort %q", ErrBadQuery, q.Sort.Key)
	}
	switch {
	case q.Limit == 0:
		q.Limit = DefaultLimit
	case q.Limit < 0 || q.Limit > MaxLimit:
		return query{}, fmt.Errorf("%w: limit %d", ErrBadQuery, q.Limit)
	}
	for _, h := range q.Health {
		if !validHealth(h) {
			return query{}, fmt.Errorf("%w: health %q", ErrBadQuery, h)
		}
	}
	p := query{Query: q, q: strings.ToLower(q.Q)}
	if q.Cursor != "" {
		c, err := decodeCursor(q.Cursor, q.Sort)
		if err != nil {
			return query{}, err
		}
		p.cur = &c
	}
	return p, nil
}

// common tests the filters every list has.
func (q *query) common(ref objects.Ref, st health.State, search func() string) bool {
	return (len(q.Namespaces) == 0 || slices.Contains(q.Namespaces, ref.Namespace)) &&
		(len(q.Health) == 0 || slices.Contains(q.Health, st)) &&
		(q.q == "" || strings.Contains(search(), q.q))
}

// page sorts es, which is in row order, and slices the page around the
// cursor. A cursor anchor need not exist: the search finds where its tuple
// would sort (design §3.6).
func (q *query) page(es []entry) Page {
	s := q.Sort
	switch {
	case s.Key == sortName && !s.Desc:
	case s.Key == sortName:
		slices.Reverse(es)
	default:
		slices.SortStableFunc(es, func(a, b entry) int { return compareEntries(a, b, s) })
	}
	n := len(es)
	start, end := 0, min(n, q.Limit)
	if c := q.cur; c != nil && c.before {
		end = sort.Search(n, func(i int) bool { return compareEntries(es[i], c.anchor, s) >= 0 })
		start = max(0, end-q.Limit)
	} else if c != nil {
		start = sort.Search(n, func(i int) bool { return compareEntries(es[i], c.anchor, s) > 0 })
		end = min(n, start+q.Limit)
	}
	p := Page{IDs: make([]RowID, 0, end-start), Total: n}
	for _, e := range es[start:end] {
		p.IDs = append(p.IDs, e.id)
	}
	switch {
	case end > start:
		if end < n {
			p.Next = encodeCursor(s, false, es[end-1])
		}
		if start > 0 {
			p.Prev = encodeCursor(s, true, es[start])
		}
	// An empty page past rows that still exist turns back from the decoded
	// anchor; it echoes only the client's own cursor.
	case q.cur != nil && !q.cur.before && start > 0:
		p.Prev = encodeCursor(s, true, q.cur.anchor)
	case q.cur != nil && q.cur.before && end < n:
		p.Next = encodeCursor(s, false, q.cur.anchor)
	}
	return p
}

// union merges row lists into one sorted list without duplicates.
func union(lists ...[]RowID) []RowID {
	if len(lists) == 1 {
		return lists[0]
	}
	var out []RowID
	for _, l := range lists {
		out = append(out, l...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// allRows returns 0..n-1.
func allRows(n int) []RowID {
	out := make([]RowID, n)
	for i := range out {
		out[i] = RowID(i) // #nosec G115 -- a row count fits in int32
	}
	return out
}

// QueryRoutes pages the route rows that match q and that visible admits.
// visible runs before Total, paging and cursors (design §7.4 rule 1); nil
// admits every row. The Backends filter reads the visible routes' specs
// through ByBackend and never looks up a backend (rule 2a).
func (ix *Index) QueryRoutes(q Query, visible func(objects.Ref) bool) (Page, error) {
	p, _, err := ix.queryRoutes(q, visible)
	return p, err
}

// queryRoutes is QueryRoutes; it also returns the number of candidates scanned.
func (ix *Index) queryRoutes(in Query, visible func(objects.Ref) bool) (Page, int, error) {
	q, err := prepare(in, sortName, sortKind, sortHealth)
	if err != nil {
		return Page{}, 0, err
	}
	if err := checkAttachment(q.Parent, q.Attachment); err != nil {
		return Page{}, 0, err
	}
	if q.Listener != "" && (q.Parent == nil || !ix.parentKinds[objects.GroupKind{Group: q.Parent.Group, Kind: q.Parent.Kind}]) {
		return Page{}, 0, fmt.Errorf("%w: listener needs a Gateway or ListenerSet parent", ErrBadQuery)
	}
	var backends map[RowID]bool
	if len(q.Backends) > 0 {
		backends = map[RowID]bool{}
		for _, b := range q.Backends {
			for _, u := range ix.ByBackend[b] {
				backends[u.Row] = true
			}
		}
	}
	// Candidates from the narrowest index: listener, parent, backend, then namespace.
	var cands []RowID
	switch {
	case q.Listener != "" && q.Listener != ListenerUnknown:
		cands = ix.listenerRows(ListenerKey{*q.Parent, q.Listener}, visible)
	case q.Parent != nil:
		cands = ix.ByParent[*q.Parent]
		if isHTTPRoute(q.Parent.Group, q.Parent.Kind) {
			// Child routes are reported edges only (design §5.3). Unwatched rows (in ByParent) and
			// declared candidates (§4.2) are listed only under their attachment filter, which Under applies.
			if q.Attachment == "" {
				cands = ix.ByParentRoute[*q.Parent]
			} else {
				cands = union(cands, ix.ByParentRoute[*q.Parent], ix.Declared[*q.Parent])
			}
		}
	case backends != nil:
		cands = make([]RowID, 0, len(backends))
		for id := range backends {
			cands = append(cands, id)
		}
		slices.Sort(cands)
	case len(q.Namespaces) > 0:
		lists := make([][]RowID, len(q.Namespaces))
		for i, ns := range q.Namespaces {
			lists[i] = ix.ByNamespace[ns]
		}
		cands = union(lists...)
	default:
		cands = allRows(len(ix.Routes))
	}
	es := make([]entry, 0, len(cands))
	for _, id := range cands {
		r := &ix.Routes[id]
		gk := objects.GroupKind{Group: r.Ref.Group, Kind: r.Ref.Kind}
		if !q.common(r.Ref, r.State, func() string { return r.search }) ||
			len(q.Kinds) > 0 && !slices.Contains(q.Kinds, gk) ||
			backends != nil && !backends[id] ||
			q.States != 0 && ix.RowFlags(id, visible)&q.States == 0 ||
			q.Listener == ListenerUnknown && !ix.unknownRef(id, *q.Parent, visible) ||
			visible != nil && !visible(r.Ref) ||
			q.Parent != nil && (q.Attachment != "" || visible != nil) && !ix.Under(id, *q.Parent, q.Attachment, visible) {
			continue
		}
		es = append(es, entry{id: id, ref: r.Ref, v: sortVal{group: r.Ref.Group, kind: r.Ref.Kind, n: healthRank(r.State)}})
	}
	return q.page(es), len(cands), nil
}

// listenerRows are the distinct rows with a member on k whose dependencies
// visible admits (design §7.4 rule 4a). A count known to be unknown has no
// members; one whose dependencies are hidden cannot be told unknown (rule 5).
func (ix *Index) listenerRows(k ListenerKey, visible func(objects.Ref) bool) []RowID {
	if f := ix.Listeners[k]; !f.Known && ix.Visible(f.Deps, visible) {
		return nil
	}
	ms := ix.ByListener[k]
	out := make([]RowID, 0, len(ms))
	for _, m := range ms {
		if (len(out) == 0 || out[len(out)-1] != m.Row) && ix.Visible(m.Deps, visible) {
			out = append(out, m.Row)
		}
	}
	return out
}

// unknownRef reports whether row id has a parentRef to parent itself whose
// outcome is unknown, with dependencies visible admits.
func (ix *Index) unknownRef(id RowID, parent objects.Ref, visible func(objects.Ref) bool) bool {
	for _, f := range ix.Routes[id].Computed {
		if f.Out.Resource == parent && f.Out.Unknown != "" && ix.Visible(f.Deps, visible) {
			return true
		}
	}
	return false
}

// RowFlags are row id's flags for a caller: listener-unknown needs its
// dependencies visible (design §7.4 rule 6); disagrees is a finding (rule 9).
func (ix *Index) RowFlags(id RowID, visible func(objects.Ref) bool) Flag {
	r := &ix.Routes[id]
	if r.Flags&FlagListenerUnknown != 0 && !ix.Visible(r.UnknownDeps, visible) {
		return r.Flags &^ FlagListenerUnknown
	}
	return r.Flags
}

// checkAttachment validates the attachment filter (design §7.2).
func checkAttachment(parent *objects.Ref, a Attachment) error {
	switch {
	case a == "":
		return nil
	case parent == nil:
		return fmt.Errorf("%w: attachment needs a parent", ErrBadQuery)
	case a == AttachmentDeclared && !isHTTPRoute(parent.Group, parent.Kind):
		return fmt.Errorf("%w: declared needs an HTTPRoute parent", ErrBadQuery)
	case !slices.Contains([]Attachment{AttachmentAccepted, AttachmentNotAccepted, AttachmentPending, AttachmentUnwatched, AttachmentDeclared}, a):
		return fmt.Errorf("%w: attachment %q", ErrBadQuery, a)
	}
	return nil
}

// Under reports whether route row id is listed under parent with
// attachment a, or any attachment when a is "". A link through a
// ListenerSet counts only when visible admits the ListenerSet, whose spec
// names the Gateway (design §7.4 rule 3); nil admits every one.
func (ix *Index) Under(id RowID, parent objects.Ref, a Attachment, visible func(objects.Ref) bool) bool {
	r := &ix.Routes[id]
	for _, l := range r.Parents {
		if a != "" && l.Attachment != a {
			continue
		}
		if l.Target == parent || l.Via != nil && *l.Via == parent && (visible == nil || visible(l.Target)) {
			return true
		}
	}
	// A reported edge lists a child under its HTTPRoute parent.
	return a == "" && slices.Contains(r.ParentRoutes, parent)
}

// routeOnly rejects the route filters on another list.
func routeOnly(q Query, parent bool) error {
	if len(q.Kinds) > 0 || q.Parent != nil && !parent || q.Attachment != "" || q.Listener != "" || len(q.Backends) > 0 || q.States != 0 {
		return fmt.Errorf("%w: a route filter on another list", ErrBadQuery)
	}
	return nil
}

// lowerName is the lower-cased "namespace/name" that q matches.
func lowerName(ref objects.Ref) string { return strings.ToLower(ref.Namespace + "/" + ref.Name) }

// QueryGateways pages the Gateway rows, as QueryRoutes does. The routes sort
// uses GatewayMemberCount, as the list row's routes column does.
func (ix *Index) QueryGateways(in Query, visible func(objects.Ref) bool) (Page, error) {
	if err := routeOnly(in, false); err != nil {
		return Page{}, err
	}
	q, err := prepare(in, sortName, sortKind, sortHealth, sortRoutes)
	if err != nil {
		return Page{}, err
	}
	es := make([]entry, 0, len(ix.Gateways))
	for i := range ix.Gateways {
		g := &ix.Gateways[i]
		if !q.common(g.Ref, g.State, func() string { return lowerName(g.Ref) }) || visible != nil && !visible(g.Ref) {
			continue
		}
		v := sortVal{group: g.Ref.Group, kind: g.Ref.Kind, n: healthRank(g.State)}
		if q.Sort.Key == sortRoutes {
			if n, ok := ix.GatewayMemberCount(RowID(i), visible); ok { // #nosec G115 -- a row count fits in int32
				v.n = n
			} else {
				v.n = -1
			}
		}
		es = append(es, entry{id: RowID(i), ref: g.Ref, v: v}) // #nosec G115 -- a row count fits in int32
	}
	return q.page(es), nil
}

// GatewayMemberCount is the routes column of Gateway row id and its sort
// value: its distinct member routes. ok is false when a listener's count is
// unknown, or has a dependency visible does not admit (design §7.4 rule 5).
func (ix *Index) GatewayMemberCount(id RowID, visible func(objects.Ref) bool) (n int, ok bool) {
	g := &ix.Gateways[id]
	if g.View == nil {
		return 0, false
	}
	return ix.memberCount(g.Ref, g.View.Listeners, g.Members, visible)
}

// ListenerSetMemberCount is GatewayMemberCount for ListenerSet row id.
func (ix *Index) ListenerSetMemberCount(id RowID, visible func(objects.Ref) bool) (n int, ok bool) {
	l := &ix.ListenerSets[id]
	if l.View == nil {
		return 0, false
	}
	return ix.memberCount(l.Ref, l.View.Listeners, l.Members, visible)
}

// memberCount is len(members) when every listener's count is known and
// visible. A count's deps cover its members', so none is then hidden.
func (ix *Index) memberCount(ref objects.Ref, ls []objects.Listener, members []RowID, visible func(objects.Ref) bool) (int, bool) {
	if ix.Listeners == nil {
		return 0, false
	}
	for _, l := range ls {
		f, ok := ix.Listeners[ListenerKey{ref, l.Name}]
		if !ok || !f.Known || !ix.Visible(f.Deps, visible) {
			return 0, false
		}
	}
	return len(members), true
}

// QueryListenerSets pages the ListenerSet rows, as QueryRoutes does. Parent,
// when set, is a Gateway.
func (ix *Index) QueryListenerSets(in Query, visible func(objects.Ref) bool) (Page, error) {
	if err := routeOnly(in, true); err != nil {
		return Page{}, err
	}
	if p := in.Parent; p != nil && (p.Group != objects.GatewayAPIGroup || p.Kind != "Gateway") {
		return Page{}, fmt.Errorf("%w: a ListenerSet parent is a Gateway", ErrBadQuery)
	}
	q, err := prepare(in, sortName, sortKind, sortHealth)
	if err != nil {
		return Page{}, err
	}
	// A Gateway row lists its ListenerSets; one the install does not own is scanned for.
	cands := allRows(len(ix.ListenerSets))
	if q.Parent != nil {
		if i, ok := slices.BinarySearchFunc(ix.Gateways, *q.Parent, func(g GatewayRow, ref objects.Ref) int { return compareRefs(g.Ref, ref) }); ok {
			cands = ix.Gateways[i].ListenerSets
		}
	}
	es := make([]entry, 0, len(cands))
	for _, id := range cands {
		l := &ix.ListenerSets[id]
		if q.Parent != nil && l.Parent != *q.Parent ||
			!q.common(l.Ref, l.State, func() string { return lowerName(l.Ref) }) || visible != nil && !visible(l.Ref) {
			continue
		}
		es = append(es, entry{id: id, ref: l.Ref, v: sortVal{group: l.Ref.Group, kind: l.Ref.Kind, n: healthRank(l.State)}})
	}
	return q.page(es), nil
}
