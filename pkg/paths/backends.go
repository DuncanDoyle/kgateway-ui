package paths

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// BackendsInput is what Backends reads besides the topology and projection.
type BackendsInput struct {
	ProjectInput
	// Served are the model's served kinds, for the in-scope Backends.
	Served []kinds.Served
	// Consumers are the install's non-route backend consumers.
	Consumers []objects.Consumer
	// Visible admits a path row by its deps and Can a ref; nil admits all (design §13).
	Visible func(explore.DepSet) bool
	Can     func(objects.Ref) bool
	// Only, when set, lists that backend alone, if it is listed.
	Only *objects.Ref
}

// Backend is one listed backend as a viewer may see it (design §8.1).
// Everything it holds is visible to that viewer.
type Backend struct {
	Ref objects.Ref
	// Type is a kgateway Backend's spec.type; "" for other kinds.
	Type string
	// Paths are its visible path rows; Confirmed those on an attachment its
	// route's status accepts.
	Paths, Confirmed int
	// Routes write it; Gateways carry its paths. Both are sorted.
	Routes, Gateways []objects.Ref
	Consumers        []objects.Consumer
	// Ports are the distinct ports written for it, sorted; 0 is left out.
	Ports []int32
	// Endpoints are a Service's per Ports entry the cache knows, or one
	// not-applicable summary for a Backend.
	Endpoints []*Endpoints
	// Own are its own distinct signals, as Problem orders them: endpoints,
	// KGWUI-002, else the judge's verdict.
	Own []Signal
	// Problem is the worst of its path problems, its own signals and the
	// attachment signals of routes that write it with no path to it.
	Problem Problem
}

// Used is the number of visible routes and consumers that use b.
func (b *Backend) Used() int { return len(b.Routes) + len(b.Consumers) }

// BackendSet is the listed backends, in no order, and whether any backend,
// path row, route or consumer was left out for the viewer.
type BackendSet struct {
	Items  []Backend
	Hidden bool
}

// backends builds a BackendSet.
type backends struct {
	t     *Topology
	p     *Projection
	in    BackendsInput
	set   BackendSet
	index map[objects.Ref]int
	// rowRoutes are, per item, the routes with any path row to it, visible or not.
	rowRoutes [][]objects.Ref
	// sets are, per item, the distinct signal sets of its visible rows.
	sets [][][]Signal
	// listed is set, per item, by a reason to list it the viewer may see.
	listed []bool
	// accepted memoises per attachment whether its status accepts it: 1 yes, -1 no.
	accepted []int8
	found    map[objects.Ref][]*health.Finding
}

// Backends lists the backends of design §8.1 from t, projected by p: every
// in-scope Backend, every backendRef or mirror target an owned route writes
// (attached or not), and every consumer's backend. A backend, row, route or
// consumer the viewer may not see is left out and counted nowhere.
func Backends(t *Topology, p *Projection, in BackendsInput) BackendSet {
	b := collect(t, p, in)
	b.finish()
	return b.set
}

// Membership is what Backends lists before any viewer check that the index
// cannot answer per ref: the in-scope Backends and each target's path rows.
// It holds only refs and row indices, so the path memo may keep it.
// Sorted slices, not maps: this is built on a cold request and must stay small.
type Membership struct {
	// rows are the path rows with a target, sorted by it.
	rows []int32
	// inScope are the in-scope Backends, sorted.
	inScope []objects.Ref
}

// NewMembership records the in-scope Backends of in and the path rows of t.
func NewMembership(t *Topology, in BackendsInput) *Membership {
	m := &Membership{}
	for k, row := range t.Rows {
		if _, ok := t.target(row); ok {
			m.rows = append(m.rows, int32(k)) // #nosec G115 -- row counts fit in int32
		}
	}
	slices.SortFunc(m.rows, func(a, b int32) int { return m.targetOf(t, a).Compare(m.targetOf(t, b)) })
	if snap := in.Snapshot; snap != nil {
		scope := in.Model.Resolved.Scope
		for _, k := range kinds.ByCategory(in.Served, kinds.CategoryBackend) {
			for _, u := range snap.Objects[k.GVR()] {
				if scope.Contains(u.GetNamespace(), named(snap.Namespaces, u.GetNamespace())) {
					m.inScope = append(m.inScope, objects.RefOf(u))
				}
			}
		}
	}
	slices.SortFunc(m.inScope, compareRefs)
	return m
}

func (m *Membership) targetOf(t *Topology, k int32) objects.Ref {
	ref, _ := t.target(t.Rows[k])
	return ref
}

// Listed reports whether Backends lists ref for the viewer of in, over t:
// the reasons of collect, for one ref.
func (m *Membership) Listed(t *Topology, in BackendsInput, ref objects.Ref) bool {
	can := func(x objects.Ref) bool { return in.Can == nil || in.Can(x) }
	if !can(ref) {
		return false
	}
	if _, ok := slices.BinarySearchFunc(m.inScope, ref, compareRefs); ok {
		return true
	}
	if ix := in.Model.Index; ix != nil {
		for _, u := range ix.ByBackend[ref] {
			if row := &ix.Routes[u.Row]; row.Owned && can(row.Ref) {
				return true
			}
		}
	}
	for _, c := range in.Consumers {
		if c.BackendRef == ref && can(c.Consumer) {
			return true
		}
	}
	i, _ := slices.BinarySearchFunc(m.rows, ref, func(k int32, r objects.Ref) int { return m.targetOf(t, k).Compare(r) })
	for ; i < len(m.rows) && m.targetOf(t, m.rows[i]) == ref; i++ {
		if in.Visible == nil || in.Visible(t.Deps(t.Rows[m.rows[i]])) {
			return true
		}
	}
	return false
}

// collect adds every candidate backend and marks the ones a visible reason lists.
func collect(t *Topology, p *Projection, in BackendsInput) *backends {
	b := &backends{t: t, p: p, in: in, index: map[objects.Ref]int{}}
	b.rows()
	b.written()
	b.inScope()
	b.consumers()
	return b
}

func (b *backends) can(ref objects.Ref) bool { return b.in.Can == nil || b.in.Can(ref) }

func (b *backends) wanted(ref objects.Ref) bool { return b.in.Only == nil || *b.in.Only == ref }

// add returns ref's item, adding it on first sight; nil when the viewer may not see it.
func (b *backends) add(ref objects.Ref) *Backend {
	if i, ok := b.index[ref]; ok {
		return &b.set.Items[i]
	}
	if !b.can(ref) {
		b.set.Hidden = true
		return nil
	}
	b.index[ref] = len(b.set.Items)
	b.set.Items = append(b.set.Items, Backend{Ref: ref})
	b.rowRoutes, b.sets, b.listed = append(b.rowRoutes, nil), append(b.sets, nil), append(b.listed, false)
	return &b.set.Items[len(b.set.Items)-1]
}

// insert adds v to the sorted s once.
func insert[T any](s []T, v T, compare func(T, T) int) []T {
	i, ok := slices.BinarySearchFunc(s, v, compare)
	if ok {
		return s
	}
	return slices.Insert(s, i, v)
}

func compareRefs(a, b objects.Ref) int { return a.Compare(b) }

// rows adds the targets of the path rows.
func (b *backends) rows() {
	t := b.t
	for k, r := range t.Rows {
		ref, ok := t.target(r)
		if !ok || !b.wanted(ref) {
			continue
		}
		it := b.add(ref)
		if it == nil {
			continue
		}
		i := b.index[ref]
		chain := t.chain(r)
		route := t.Refs[t.routes[chain[len(chain)-1]].ref]
		b.rowRoutes[i] = insert(b.rowRoutes[i], route, compareRefs)
		if b.in.Visible != nil && !b.in.Visible(t.Deps(r)) {
			b.set.Hidden = true
			continue
		}
		b.listed[i] = true
		it.Paths++
		if b.confirmed(r) {
			it.Confirmed++
		}
		it.Routes = insert(it.Routes, route, compareRefs)
		it.Gateways = insert(it.Gateways, t.Gateway(r), compareRefs)
		var port int32
		if m, ok := t.Mirror(r); ok {
			port = m.Port
		} else if br, ok := t.Backend(r); ok {
			port = br.Port
		}
		it.addPort(port)
		pr := b.p.Problems[k]
		if stateRank(pr.State) < stateRank(it.Problem.State) || it.Problem.State == "" {
			it.Problem.State = pr.State
		}
		if len(pr.Signals) > 0 && !slices.ContainsFunc(b.sets[i], func(s []Signal) bool { return &s[0] == &pr.Signals[0] }) {
			b.sets[i] = append(b.sets[i], pr.Signals)
		}
	}
}

func (it *Backend) addPort(port int32) {
	if port != 0 {
		it.Ports = insert(it.Ports, port, cmp.Compare[int32])
	}
}

// confirmed reports whether row r's attachment is accepted by its route's status.
func (b *backends) confirmed(r Row) bool {
	ix := b.in.Model.Index
	if ix == nil {
		return false
	}
	if b.accepted == nil {
		b.accepted = make([]int8, len(b.t.atts))
	}
	if v := b.accepted[r.att]; v != 0 {
		return v > 0
	}
	t := b.t
	root := t.Refs[t.routes[t.chain(r)[0]].ref]
	parent := int(t.atts[r.att].parent)
	b.accepted[r.att] = -1
	if id, ok := ix.Row(root); ok {
		for _, l := range ix.Routes[id].Parents {
			if (l.Spec == parent || slices.Contains(l.Dups, parent)) && l.Attachment == explore.AttachmentAccepted {
				b.accepted[r.att] = 1
			}
		}
	}
	return b.accepted[r.att] > 0
}

// written adds the targets owned routes write, attached or not. A route with
// no path row to a target adds its written ports and its attachment signals.
func (b *backends) written() {
	ix := b.in.Model.Index
	if ix == nil {
		return
	}
	each := func(ref objects.Ref, uses []explore.BackendUse) {
		var it *Backend
		for _, u := range uses {
			row := &ix.Routes[u.Row]
			if !row.Owned {
				continue
			}
			if it == nil {
				if it = b.add(ref); it == nil {
					return
				}
			}
			if !b.can(row.Ref) {
				b.set.Hidden = true
				continue
			}
			it.Routes = insert(it.Routes, row.Ref, compareRefs)
			i := b.index[ref]
			b.listed[i] = true
			if _, ok := slices.BinarySearchFunc(b.rowRoutes[i], row.Ref, compareRefs); ok {
				continue
			}
			for _, port := range writtenPorts(row, ref) {
				it.addPort(port)
			}
			b.sets[i] = append(b.sets[i], b.attachmentSignals(row))
		}
	}
	if b.in.Only != nil {
		each(*b.in.Only, ix.ByBackend[*b.in.Only])
		return
	}
	for ref, uses := range ix.ByBackend {
		each(ref, uses)
	}
}

// writtenPorts are the ports route row writes for target, in its rules'
// backendRefs and RequestMirror filters.
func writtenPorts(row *explore.RouteRow, target objects.Ref) []int32 {
	var rules []objects.Rule
	if row.View != nil {
		rules = row.View.Rules
	} else {
		rules = objects.DecodeRouteRules(row.Obj)
	}
	var out []int32
	mirrors := func(fs []objects.Filter) {
		for _, f := range fs {
			if f.Mirror != nil && f.Mirror.Backend == target {
				out = append(out, f.Mirror.Port)
			}
		}
	}
	for i := range rules {
		mirrors(rules[i].Filters)
		for _, br := range rules[i].BackendRefs {
			if br.Ref == target && br.Delegation == nil {
				out = append(out, br.Port)
			}
			mirrors(br.Filters)
		}
	}
	return out
}

// attachmentSignals are route row's attachment failures and pending
// attachments on the parents the viewer may see.
func (b *backends) attachmentSignals(row *explore.RouteRow) []Signal {
	var sigs []Signal
	for _, l := range row.Parents {
		if l.Spec < 0 && len(l.Dups) == 0 || !b.can(l.Target) {
			continue
		}
		switch {
		case l.Attachment == explore.AttachmentNotAccepted && l.Cond != nil:
			sigs = append(sigs, Signal{Kind: SignalRouteStatus, Code: l.Cond.Type, Ref: row.Ref, State: health.StateError})
		case l.Attachment == explore.AttachmentPending:
			sigs = append(sigs, Signal{Kind: SignalWaiting, Ref: row.Ref, State: health.StateUnknown})
		}
	}
	return sigs
}

// inScope adds every Backend in the install's scope, used or not.
func (b *backends) inScope() {
	snap := b.in.Snapshot
	if snap == nil {
		return
	}
	scope := b.in.Model.Resolved.Scope
	for _, k := range kinds.ByCategory(b.in.Served, kinds.CategoryBackend) {
		for _, u := range snap.Objects[k.GVR()] {
			ref := objects.RefOf(u)
			if b.wanted(ref) && scope.Contains(u.GetNamespace(), named(snap.Namespaces, u.GetNamespace())) && b.add(ref) != nil {
				b.listed[b.index[ref]] = true
			}
		}
	}
}

// consumers adds the backends of the consumers the viewer may see.
func (b *backends) consumers() {
	for _, c := range b.in.Consumers {
		if !b.wanted(c.BackendRef) {
			continue
		}
		it := b.add(c.BackendRef)
		if it == nil {
			continue
		}
		if !b.can(c.Consumer) {
			b.set.Hidden = true
			continue
		}
		b.listed[b.index[c.BackendRef]] = true
		it.Consumers = append(it.Consumers, c)
		it.addPort(int32(c.Port)) // #nosec G115 -- a port fits in int32
	}
}

// finish sets each item's type, endpoints, own signals and problem.
func (b *backends) finish() {
	var need map[string]bool
	for i := range b.set.Items {
		if r := b.set.Items[i].Ref; isService(r) {
			if need == nil {
				need = map[string]bool{}
			}
			need[r.Namespace+"/"+r.Name] = true
		}
	}
	f := &facts{snap: b.in.Snapshot, need: func(ns, name string) bool { return need[ns+"/"+name] }}
	if len(b.set.Items) > 0 {
		b.found = map[objects.Ref][]*health.Finding{}
		for j := range b.in.Model.Health.Findings {
			fd := &b.in.Model.Health.Findings[j]
			if fd.Resource != nil {
				if _, ok := b.index[*fd.Resource]; ok {
					b.found[*fd.Resource] = append(b.found[*fd.Resource], fd)
				}
			}
		}
	}
	for i := range b.set.Items {
		it := &b.set.Items[i]
		switch {
		case isService(it.Ref):
			for _, port := range it.Ports {
				ph := portHealthOf(b.in.ProjectInput, f, portKey{it.Ref.Namespace, it.Ref.Name, port}, b.found[it.Ref])
				if ph.ep != nil {
					it.Endpoints = append(it.Endpoints, ph.ep)
				}
				if ph.sig != nil {
					it.Own = append(it.Own, *ph.sig)
				}
				it.Own = append(it.Own, ph.found...)
			}
		case isBackend(it.Ref):
			u := object(b.in.Snapshot, b.in.Served, it.Ref)
			if u != nil {
				it.Type = objects.DecodeBackend(u).Type
			}
			it.Endpoints = []*Endpoints{{Complete: EndpointsNotApplicable}}
			// KGWUI-002 exists only for a Backend a route uses; with no visible route
			// the judge alone decides, so a hidden route's use shows nothing (D-077).
			found := b.found[it.Ref]
			if len(it.Routes) == 0 {
				found = nil
			}
			it.Own = backendSignals(nil, b.in.Model, it.Ref, u, found)
		}
		it.Own = merge("", nil, it.Own).Signals
		it.Problem = merge(it.Problem.State, b.sets[i], it.Own)
	}
	// A backend only hidden rows, routes or consumers use is not listed.
	kept := b.set.Items[:0]
	for i := range b.set.Items {
		if b.listed[i] {
			kept = append(kept, b.set.Items[i])
		} else {
			b.set.Hidden = true
		}
	}
	b.set.Items = kept
}

// merge is the problem of a backend: the worst of state and its signals,
// the distinct signals ordered by state, kind, code and ref.
func merge(state health.State, sets [][]Signal, own []Signal) Problem {
	pr := Problem{State: cmp.Or(state, health.StateHealthy)}
	for _, set := range append(sets, own) {
		for _, s := range set {
			if !slices.Contains(pr.Signals, s) {
				pr.Signals = append(pr.Signals, s)
			}
			if stateRank(s.State) < stateRank(pr.State) {
				pr.State = s.State
			}
		}
	}
	slices.SortFunc(pr.Signals, func(a, b Signal) int {
		return cmp.Or(cmp.Compare(stateRank(a.State), stateRank(b.State)), strings.Compare(a.Kind, b.Kind), strings.Compare(a.Code, b.Code), a.Ref.Compare(b.Ref))
	})
	return pr
}

func isService(r objects.Ref) bool { return r.Group == "" && r.Kind == "Service" }

func isBackend(r objects.Ref) bool { return r.Group == kinds.KgatewayGroup && r.Kind == "Backend" }

// object is the stored object ref names, or nil; the cache sorts each kind by "namespace/name".
func object(snap *cache.Snapshot, served []kinds.Served, ref objects.Ref) *unstructured.Unstructured {
	if snap == nil {
		return nil
	}
	for _, k := range served {
		if k.Group != ref.Group || k.Kind != ref.Kind {
			continue
		}
		objs := snap.Objects[k.GVR()]
		key := ref.Namespace + "/" + ref.Name
		if j, ok := slices.BinarySearchFunc(objs, key, func(u *unstructured.Unstructured, key string) int {
			return strings.Compare(u.GetNamespace()+"/"+u.GetName(), key)
		}); ok {
			return objs[j]
		}
	}
	return nil
}

// named is the element of ns named name, or the zero value.
func named[N interface{ GetName() string }](ns []N, name string) N {
	var zero N
	if i := slices.IndexFunc(ns, func(n N) bool { return n.GetName() == name }); i >= 0 {
		return ns[i]
	}
	return zero
}

// Backend list sort keys (design §8.1); the empty key is BackendSortProblem.
const (
	BackendSortProblem = "problem"
	BackendSortName    = "name"
	BackendSortUsed    = "used"
)

// Values of BackendQuery.Used.
const (
	UsedYes = "yes"
	UsedNo  = "no"
)

// BackendQuery is one backend list request (design §8.1). Zero fields do not filter.
type BackendQuery struct {
	// Kind is the backend's kind, such as Service or Backend.
	Kind      string
	Namespace string
	States    []health.State
	// Used is yes for a backend a visible route or consumer uses, no for one none does.
	Used string
	// Gateways match a backend with a visible path through one of them, by namespace and name.
	Gateways []objects.Ref
	// Q matches, ignoring case, the backend's namespace/name.
	Q string
	// Sort orders by its key, Desc reversing it; ties keep namespace, name, group and kind order.
	Sort   string
	Desc   bool
	Cursor string
	// Limit is 1–MaxLimit; 0 means DefaultLimit.
	Limit int
}

// BackendPage is one page of a BackendSet's items, as indexes into Items,
// and the facets of the whole set.
type BackendPage struct {
	Items      []int
	Next, Prev string
	Facets     BackendFacets
}

// BackendFacets list the values of the gateway and namespace filters over the
// set's backends, which are the visible ones. Each facet applies every filter
// but its own, so a selected value leaves the others selectable. Both are
// sorted by name.
type BackendFacets struct {
	Gateways   []BackendGatewayFacet
	Namespaces []BackendNamespaceFacet
}

// BackendGatewayFacet is a Gateway and the backends it has a visible path to.
type BackendGatewayFacet struct {
	Ref      objects.Ref
	Backends int
}

// BackendNamespaceFacet is a namespace and the backends in it.
type BackendNamespaceFacet struct {
	Name     string
	Backends int
}

// RunBackends pages the items of set that match q. Bad input is
// explore.ErrBadQuery or explore.ErrBadCursor.
func RunBackends(set BackendSet, q BackendQuery) (BackendPage, error) {
	q.Sort = cmp.Or(q.Sort, BackendSortProblem)
	switch {
	case !slices.Contains([]string{BackendSortProblem, BackendSortName, BackendSortUsed}, q.Sort):
		return BackendPage{}, badQuery("sort %q", q.Sort)
	case q.Used != "" && q.Used != UsedYes && q.Used != UsedNo:
		return BackendPage{}, badQuery("used %q", q.Used)
	case q.Limit < 0 || q.Limit > MaxLimit:
		return BackendPage{}, badQuery("limit %d", q.Limit)
	case q.Limit == 0:
		q.Limit = DefaultLimit
	}
	for _, s := range q.States {
		if !slices.Contains(states, s) {
			return BackendPage{}, badQuery("state %q", s)
		}
	}
	var cur *backendAnchor
	var before bool
	if q.Cursor != "" {
		a, b, err := decodeBackendCursor(q.Cursor, q.Sort, q.Desc)
		if err != nil {
			return BackendPage{}, err
		}
		cur, before = &a, b
	}
	qs := strings.ToLower(q.Q)
	var es []int
	gws, nss := map[objects.Ref]int{}, map[string]int{}
	for i := range set.Items {
		b := &set.Items[i]
		if q.matches(b, qs, facetNone) {
			es = append(es, i)
		}
		if q.matches(b, qs, facetGateway) {
			for _, g := range b.Gateways {
				gws[g]++
			}
		}
		if q.matches(b, qs, facetNamespace) {
			nss[b.Ref.Namespace]++
		}
	}
	slices.SortFunc(es, func(a, b int) int { return q.compare(anchorOfBackend(&set.Items[a]), anchorOfBackend(&set.Items[b])) })
	n := len(es)
	start, end := 0, min(n, q.Limit)
	if cur != nil && before {
		end = sort.Search(n, func(k int) bool { return q.compare(anchorOfBackend(&set.Items[es[k]]), *cur) >= 0 })
		start = max(0, end-q.Limit)
	} else if cur != nil {
		start = sort.Search(n, func(k int) bool { return q.compare(anchorOfBackend(&set.Items[es[k]]), *cur) > 0 })
		end = min(n, start+q.Limit)
	}
	pg := BackendPage{Items: slices.Clone(es[start:end]), Facets: backendFacets(gws, nss)}
	switch {
	case end > start:
		if end < n {
			pg.Next = encodeBackendCursor(q.Sort, q.Desc, false, anchorOfBackend(&set.Items[es[end-1]]))
		}
		if start > 0 {
			pg.Prev = encodeBackendCursor(q.Sort, q.Desc, true, anchorOfBackend(&set.Items[es[start]]))
		}
	case cur != nil && !before && start > 0:
		pg.Prev = encodeBackendCursor(q.Sort, q.Desc, true, *cur)
	case cur != nil && before && end < n:
		pg.Next = encodeBackendCursor(q.Sort, q.Desc, false, *cur)
	}
	return pg, nil
}

// backendFacets are the counts sorted by name; never nil.
func backendFacets(gws map[objects.Ref]int, nss map[string]int) BackendFacets {
	f := BackendFacets{Gateways: make([]BackendGatewayFacet, 0, len(gws)), Namespaces: make([]BackendNamespaceFacet, 0, len(nss))}
	for g, n := range gws {
		f.Gateways = append(f.Gateways, BackendGatewayFacet{Ref: g, Backends: n})
	}
	slices.SortFunc(f.Gateways, func(a, b BackendGatewayFacet) int {
		return cmp.Or(cmp.Compare(a.Ref.Namespace, b.Ref.Namespace), cmp.Compare(a.Ref.Name, b.Ref.Name))
	})
	for ns, n := range nss {
		f.Namespaces = append(f.Namespaces, BackendNamespaceFacet{Name: ns, Backends: n})
	}
	slices.SortFunc(f.Namespaces, func(a, b BackendNamespaceFacet) int { return cmp.Compare(a.Name, b.Name) })
	return f
}

// matches reports whether b passes q's filters, all but skip's; qs is the lower-cased Q.
func (q *BackendQuery) matches(b *Backend, qs string, skip facet) bool {
	switch {
	case q.Kind != "" && b.Ref.Kind != q.Kind,
		q.Namespace != "" && skip != facetNamespace && b.Ref.Namespace != q.Namespace,
		len(q.States) > 0 && !slices.Contains(q.States, b.Problem.State),
		// Use counts visible routes and consumers only, so a hidden one changes nothing (D-077).
		q.Used == UsedYes && b.Used() == 0, q.Used == UsedNo && b.Used() > 0,
		qs != "" && !strings.Contains(strings.ToLower(b.Ref.Namespace+"/"+b.Ref.Name), qs):
		return false
	}
	if len(q.Gateways) > 0 && skip != facetGateway {
		return slices.ContainsFunc(b.Gateways, func(g objects.Ref) bool {
			return slices.ContainsFunc(q.Gateways, func(w objects.Ref) bool { return w.Namespace == g.Namespace && w.Name == g.Name })
		})
	}
	return true
}

// backendAnchor is a backend's sort key; it need not exist in the set a cursor is used on.
type backendAnchor struct {
	state health.State
	used  int
	ref   objects.Ref
}

func anchorOfBackend(b *Backend) backendAnchor {
	return backendAnchor{state: b.Problem.State, used: b.Used(), ref: b.Ref}
}

// compare orders two anchors by the sort's key, reversed by Desc, then by
// namespace, name, group and kind.
func (q *BackendQuery) compare(a, b backendAnchor) int {
	c := 0
	switch q.Sort {
	case BackendSortProblem:
		c = cmp.Compare(stateRank(a.state), stateRank(b.state))
	case BackendSortUsed:
		c = cmp.Compare(a.used, b.used)
	}
	if q.Desc {
		c = -c
	}
	return cmp.Or(c, strings.Compare(a.ref.Namespace, b.ref.Namespace), strings.Compare(a.ref.Name, b.ref.Name),
		strings.Compare(a.ref.Group, b.ref.Group), strings.Compare(a.ref.Kind, b.ref.Kind))
}

// wireBackendCursor is the JSON inside a backend list cursor: the sort, the
// direction, B for a prev cursor, and the anchor's state P, use count U and ref R.
type wireBackendCursor struct {
	S string       `json:"s"`
	D string       `json:"d"`
	B bool         `json:"b"`
	P health.State `json:"p"`
	U int          `json:"u"`
	R []string     `json:"r"`
}

func encodeBackendCursor(sort string, desc, before bool, a backendAnchor) string {
	b, _ := json.Marshal(wireBackendCursor{S: sort, D: direction(desc), B: before, P: a.state, U: a.used, R: wireRef(a.ref)}) // strings and ints cannot fail
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeBackendCursor parses c for sort and desc. Every failure is explore.ErrBadCursor.
func decodeBackendCursor(c, sort string, desc bool) (a backendAnchor, before bool, err error) {
	if len(c) > maxCursor {
		return a, false, badCursor("too long")
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return a, false, badCursor("not base64url")
	}
	var w wireBackendCursor
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil || dec.More() {
		return a, false, badCursor("not a cursor")
	}
	if w.S != sort || w.D != direction(desc) {
		return a, false, badCursor("from another sort")
	}
	ref, ok := refOf(w.R)
	if !ok || !slices.Contains(states, w.P) || w.U < 0 {
		return a, false, badCursor("bad anchor")
	}
	return backendAnchor{state: w.P, used: w.U, ref: ref}, w.B, nil
}
