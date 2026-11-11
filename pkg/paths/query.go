package paths

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// Sort keys (design §5); the empty key is SortProblem.
const (
	SortProblem  = "problem"
	SortGateway  = "gateway"
	SortRoute    = "route"
	SortBackend  = "backend"
	SortHostname = "hostname"
)

// Mirror filter values; the empty value is MirrorInclude.
const (
	MirrorInclude = "include"
	MirrorOnly    = "only"
	MirrorExclude = "exclude"
)

// Page limits, as the explorer's.
const (
	DefaultLimit = explore.DefaultLimit
	MaxLimit     = explore.MaxLimit
)

// Query is one path list request (design §5). Zero fields do not filter.
type Query struct {
	// Gateways match a row's Gateway by namespace and name; a ListenerSet row has its parent's.
	Gateways []objects.Ref
	// Listener is a listener name.
	Listener string
	// Route matches any route of the row's chain.
	Route *objects.Ref
	// Backends match the row's backendRef or mirror target; a delegation is no backend.
	Backends []objects.Ref
	// Namespace matches the namespace of any route of the chain.
	Namespace string
	States    []health.State
	// Kind is the root route's kind, such as GRPCRoute.
	Kind   string
	Mirror string
	// Q matches, ignoring case, any hostname, a route name of the chain or the backend name.
	Q string
	// Sort orders by its key, Desc reversing it; ties keep the problem-first order.
	Sort   string
	Desc   bool
	Cursor string
	// Limit is 1–MaxLimit; 0 means DefaultLimit.
	Limit int
}

// Page is one page of rows, as indexes into Topology.Rows, and the summary
// of every visible row the filters match.
type Page struct {
	Rows       []int32
	Next, Prev string
	Summary    Summary
}

// Summary counts the filtered rows a viewer may see (design §5, §13). Routes
// are the routes whose rule a row is on; Backends exclude delegations.
type Summary struct {
	Paths, Routes, Gateways, Backends int
	// ByState holds every state, zero counts included.
	ByState map[health.State]Counts
	// EndpointsNotRead is set when a row's endpoints failed or were not read.
	EndpointsNotRead bool
	// Hidden is set when visible dropped any row.
	Hidden bool
	// Unmemoised is set by the caller when the topology was over the memo cap.
	Unmemoised bool
}

// Counts are a Summary's counts for one state.
type Counts struct{ Paths, Routes, Gateways, Backends int }

// states are the problem states, in problem order.
var states = []health.State{health.StateError, health.StateWarning, health.StateUnknown, health.StateHealthy}

// query is a validated Query with its decoded cursor.
type query struct {
	Query
	q      string // lower-cased Q
	cur    *anchor
	before bool
}

func badQuery(format string, args ...any) error {
	return fmt.Errorf("%w: %s", explore.ErrBadQuery, fmt.Sprintf(format, args...))
}

// prepare validates q and decodes its cursor.
func prepare(q Query) (query, error) {
	q.Sort = cmp.Or(q.Sort, SortProblem)
	q.Mirror = cmp.Or(q.Mirror, MirrorInclude)
	if !slices.Contains([]string{SortProblem, SortGateway, SortRoute, SortBackend, SortHostname}, q.Sort) {
		return query{}, badQuery("sort %q", q.Sort)
	}
	if !slices.Contains([]string{MirrorInclude, MirrorOnly, MirrorExclude}, q.Mirror) {
		return query{}, badQuery("mirror %q", q.Mirror)
	}
	switch {
	case q.Limit == 0:
		q.Limit = DefaultLimit
	case q.Limit < 0 || q.Limit > MaxLimit:
		return query{}, badQuery("limit %d", q.Limit)
	}
	for _, s := range q.States {
		if !slices.Contains(states, s) {
			return query{}, badQuery("state %q", s)
		}
	}
	p := query{Query: q, q: strings.ToLower(q.Q)}
	if q.Cursor != "" {
		a, before, err := decodeCursor(q.Cursor, q.Sort, q.Desc)
		if err != nil {
			return query{}, err
		}
		p.cur, p.before = &a, before
	}
	return p, nil
}

// Run pages the rows of t, projected by p, that match q and that visible
// admits. visible runs on every row's deps before filtering, counting and
// paging (design §13); nil admits every row. Bad input is explore.ErrBadQuery
// or explore.ErrBadCursor.
func Run(t *Topology, p *Projection, q Query, visible func(explore.DepSet) bool) (Page, error) {
	qq, err := prepare(q)
	if err != nil {
		return Page{}, err
	}
	tl := newTally(t)
	var es []int32
	for _, i := range p.Order {
		if visible != nil && !visible(t.Deps(t.Rows[i])) {
			tl.s.Hidden = true
			continue
		}
		if qq.match(t, p, i) {
			es = append(es, i)
			tl.add(p, i)
		}
	}
	// es is in problem-first order, so a stable sort keeps it for ties.
	switch {
	case qq.Sort == SortProblem && !qq.Desc:
	case qq.Sort == SortProblem:
		slices.SortStableFunc(es, func(a, b int32) int {
			return cmp.Compare(stateRank(p.Problems[b].State), stateRank(p.Problems[a].State))
		})
	default:
		slices.SortStableFunc(es, func(a, b int32) int { return qq.primary(t, a, b) })
	}
	pg := qq.page(t, p, es)
	pg.Summary = tl.summary()
	return pg, nil
}

// match reports whether row i passes q's filters.
func (q *query) match(t *Topology, p *Projection, i int32) bool {
	r := t.Rows[i]
	switch {
	case len(q.States) > 0 && !slices.Contains(q.States, p.Problems[i].State),
		q.Listener != "" && t.Listener(r).Name != q.Listener,
		q.Mirror == MirrorOnly && r.mirror < 0, q.Mirror == MirrorExclude && r.mirror >= 0:
		return false
	}
	if len(q.Gateways) > 0 {
		gw := t.Gateway(r)
		if !slices.ContainsFunc(q.Gateways, func(g objects.Ref) bool { return g.Namespace == gw.Namespace && g.Name == gw.Name }) {
			return false
		}
	}
	chain := t.chain(r)
	if q.Kind != "" && t.Refs[t.routes[chain[0]].ref].Kind != q.Kind {
		return false
	}
	if q.Route != nil || q.Namespace != "" {
		var route, ns bool
		for _, ri := range chain {
			rt := t.Refs[t.routes[ri].ref]
			route = route || q.Route != nil && rt == *q.Route
			ns = ns || rt.Namespace == q.Namespace
		}
		if q.Route != nil && !route || q.Namespace != "" && !ns {
			return false
		}
	}
	if len(q.Backends) == 0 && q.q == "" {
		return true
	}
	target, has := t.target(r)
	if len(q.Backends) > 0 && (!has || !slices.Contains(q.Backends, target)) {
		return false
	}
	return q.q == "" || has && contains(target.Name, q.q) || q.text(t, r, chain)
}

// text reports whether a hostname or a route name of the chain contains q.
func (q *query) text(t *Topology, r Row, chain []int32) bool {
	for _, h := range t.Hostnames(r) {
		if contains(h, q.q) {
			return true
		}
	}
	for _, ri := range chain {
		if contains(t.Refs[t.routes[ri].ref].Name, q.q) {
			return true
		}
	}
	return false
}

// contains reports whether s, lower-cased, contains the lower-cased sub.
func contains(s, sub string) bool { return strings.Contains(strings.ToLower(s), sub) }

// target is the row's backendRef or mirror target; false for a written
// action or a delegation.
func (t *Topology) target(r Row) (objects.Ref, bool) {
	if r.ref < 0 && r.mirror < 0 {
		return objects.Ref{}, false
	}
	rule := t.Rule(r)
	if r.mirror >= 0 {
		fs := rule.Filters
		if r.ref >= 0 {
			fs = rule.BackendRefs[r.ref].Filters
		}
		return fs[r.mirror].Mirror.Backend, true
	}
	b := &rule.BackendRefs[r.ref]
	if b.Delegation != nil && t.delegates {
		return objects.Ref{}, false
	}
	return b.Ref, true
}

// val is row r's sort value under sort: its target or its first hostname.
func (t *Topology) val(r Row, sort string) sortVal {
	switch sort {
	case SortBackend:
		ref, ok := t.target(r)
		return sortVal{has: ok, ref: ref}
	case SortHostname:
		if h := t.Hostnames(r); len(h) > 0 {
			return sortVal{has: true, host: h[0]}
		}
	}
	return sortVal{}
}

// compareVals orders sort values, reversed by desc; none sorts last either way.
func compareVals(a, b sortVal, desc bool) int {
	if !a.has || !b.has {
		return cmp.Compare(boolInt(!a.has), boolInt(!b.has))
	}
	c := cmp.Or(a.ref.Compare(b.ref), cmp.Compare(a.host, b.host))
	if desc {
		return -c
	}
	return c
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// primary orders rows a and b by the sort's own key only, desc applied.
func (q *query) primary(t *Topology, a, b int32) int {
	ra, rb := t.Rows[a], t.Rows[b]
	c := 0
	switch q.Sort {
	case SortGateway:
		c = t.Gateway(ra).Compare(t.Gateway(rb))
	case SortRoute:
		c = t.compareChains(ra, rb)
	default:
		return compareVals(t.val(ra, q.Sort), t.val(rb, q.Sort), q.Desc)
	}
	if q.Desc {
		return -c
	}
	return c
}

// compareAnchor orders row i against a as the sorted rows are ordered: the
// sort's key, then the problem-first order, which Projection.Order follows.
func (q *query) compareAnchor(t *Topology, p *Projection, i int32, a *anchor) int {
	r := t.Rows[i]
	st := p.Problems[i].State
	c := 0
	switch q.Sort {
	case SortProblem:
		c = cmp.Compare(stateRank(st), stateRank(a.state))
	case SortGateway:
		c = t.Gateway(r).Compare(a.gateway)
	case SortRoute:
		c = t.compareChain(r, a.chain)
	}
	if q.Desc {
		c = -c
	}
	if q.Sort == SortBackend || q.Sort == SortHostname {
		c = compareVals(t.val(r, q.Sort), a.val, q.Desc)
	}
	return cmp.Or(c, cmp.Compare(stateRank(st), stateRank(a.state)), t.Gateway(r).Compare(a.gateway),
		cmp.Compare(t.Listener(r).Name, a.listener), t.compareChain(r, a.chain), cmp.Compare(t.Rule(r).Index, a.rule), cmp.Compare(i, a.row))
}

// compareChain orders the row's chain against refs, as compareChains does.
func (t *Topology) compareChain(r Row, refs []objects.Ref) int {
	c := t.chain(r)
	for i := 0; i < len(c) && i < len(refs); i++ {
		if d := t.Refs[t.routes[c[i]].ref].Compare(refs[i]); d != 0 {
			return d
		}
	}
	return cmp.Compare(len(c), len(refs))
}

// anchorOf is row i's cursor anchor under sort.
func anchorOf(t *Topology, p *Projection, i int32, sort string) *anchor {
	r := t.Rows[i]
	return &anchor{val: t.val(r, sort), state: p.Problems[i].State, gateway: t.Gateway(r), listener: t.Listener(r).Name,
		chain: t.Chain(r), rule: t.Rule(r).Index, row: i}
}

// page slices the sorted rows es around the cursor, as pkg/explore does. A
// cursor anchor need not exist: the search finds where it would sort.
func (q *query) page(t *Topology, p *Projection, es []int32) Page {
	n := len(es)
	start, end := 0, min(n, q.Limit)
	if c := q.cur; c != nil && q.before {
		end = sort.Search(n, func(k int) bool { return q.compareAnchor(t, p, es[k], c) >= 0 })
		start = max(0, end-q.Limit)
	} else if c != nil {
		start = sort.Search(n, func(k int) bool { return q.compareAnchor(t, p, es[k], c) > 0 })
		end = min(n, start+q.Limit)
	}
	pg := Page{Rows: slices.Clone(es[start:end])}
	switch {
	case end > start:
		if end < n {
			pg.Next = encodeCursor(q.Sort, q.Desc, false, anchorOf(t, p, es[end-1], q.Sort))
		}
		if start > 0 {
			pg.Prev = encodeCursor(q.Sort, q.Desc, true, anchorOf(t, p, es[start], q.Sort))
		}
	// An empty page past rows that still exist turns back from the decoded anchor.
	case q.cur != nil && !q.before && start > 0:
		pg.Prev = encodeCursor(q.Sort, q.Desc, true, q.cur)
	case q.cur != nil && q.before && end < n:
		pg.Next = encodeCursor(q.Sort, q.Desc, false, q.cur)
	}
	return pg
}

// tally counts rows into a Summary. Routes, Gateways and backends are Refs
// indexes; seen marks each with bit 0 for the total and bit 1+rank per state.
type tally struct {
	t                          *Topology
	routes, gateways, backends []uint8
	s                          Summary
	by                         [4]Counts
}

func newTally(t *Topology) *tally {
	n := len(t.Refs)
	return &tally{t: t, routes: make([]uint8, n), gateways: make([]uint8, n), backends: make([]uint8, n)}
}

// add counts row i.
func (c *tally) add(p *Projection, i int32) {
	t, r := c.t, c.t.Rows[i]
	rank := stateRank(p.Problems[i].State)
	bit := uint8(2) << rank
	by := &c.by[rank]
	mark := func(seen []uint8, k int32, total, per *int) {
		if seen[k]&1 == 0 {
			seen[k] |= 1
			*total++
		}
		if seen[k]&bit == 0 {
			seen[k] |= bit
			*per++
		}
	}
	c.s.Paths++
	by.Paths++
	chain := t.chain(r)
	mark(c.routes, t.routes[chain[len(chain)-1]].ref, &c.s.Routes, &by.Routes)
	mark(c.gateways, t.atts[r.att].gateway, &c.s.Gateways, &by.Gateways)
	if ref, ok := t.target(r); ok {
		// The target is always in the row's deps.
		for _, d := range t.Deps(r) {
			if t.Refs[d] == ref {
				mark(c.backends, d, &c.s.Backends, &by.Backends)
				break
			}
		}
	}
	if e := p.Endpoints[i]; e != nil && (e.Complete == EndpointsFailed || e.Complete == EndpointsUnavailable) {
		c.s.EndpointsNotRead = true
	}
}

func (c *tally) summary() Summary {
	c.s.ByState = make(map[health.State]Counts, len(states))
	for i, s := range states {
		c.s.ByState[s] = c.by[i]
	}
	return c.s
}
