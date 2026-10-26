package explore

import (
	"cmp"
	"encoding/binary"
	"slices"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Pending reasons that do not come from a status entry.
const (
	pendingNoStatus      = "no status from the controller"
	pendingParentMissing = "parent not found"
)

// programmedType is kgateway's route programming condition (2.4).
const programmedType = "kgateway.dev/Programmed"

// aux is what Build needs about a row until the index is filled. It is
// index-aligned with builder.rows and sorted with it.
type aux struct {
	// status are the row's status.parents; specRefs its spec.parentRefs.
	status   []objects.RouteParentStatus
	specRefs []objects.ParentRef
	// edges are the HTTPRoutes of the row's reported edges.
	edges []objects.Ref
	// targets are the row's backend targets; nil for an HTTPRoute until index.
	targets []objects.BackendTarget
	// written are the spec parentRefs as written, index-aligned with specRefs.
	written []objects.WrittenRef
}

// builder holds Build's per-call state.
type builder struct {
	in  Input
	dec objects.Decoder
	// rows become Index.Routes in place; aux is index-aligned with them.
	rows    []RouteRow
	aux     []aux
	at      map[objects.Ref]int
	lsGW    map[objects.Ref]*objects.Ref // owned ListenerSet → its Gateway, shared by every link
	ownedGW map[objects.Ref]bool
	parents map[objects.GroupKind]bool // served Gateway and ListenerSet kinds
	unknown bool                       // a required check is not OK
	worst   map[objects.Ref]Worst
	// disagrees are the objects with KGWUI-012; disagreements counts disagreeing checks per object.
	disagrees     map[objects.Ref]bool
	disagreements map[objects.Ref]int
}

// Build builds the index of one install. It is pure: the same input gives
// the same rows in the same order, whatever the snapshot's order.
func Build(in Input) *Index {
	b := &builder{in: in, dec: in.Views, lsGW: map[objects.Ref]*objects.Ref{}, ownedGW: map[objects.Ref]bool{}, parents: map[objects.GroupKind]bool{}}
	if b.dec == nil {
		b.dec = objects.Direct{}
	}
	for _, k := range in.Served {
		if k.Category == kinds.CategoryGateway || k.Category == kinds.CategoryListenerSet {
			b.parents[objects.GroupKind{Group: k.Group, Kind: k.Kind}] = true
		}
	}
	b.findings()
	ix := &Index{}
	if o := in.Owned; o != nil {
		for _, g := range o.Gateways {
			b.ownedGW[objects.RefOf(g)] = true
		}
		lsViews := make([]*objects.ListenerSetView, len(o.ListenerSets))
		for i, ls := range o.ListenerSets {
			lsViews[i] = b.dec.ListenerSet(ls)
			g := lsViews[i].Parent.Ref()
			b.lsGW[objects.RefOf(ls)] = &g
		}
		n := len(o.Routes) + len(o.Unattached) + len(o.Declared) + len(o.NoParent) + len(in.Assignment.Unwatched)
		b.at, b.rows, b.aux = make(map[objects.Ref]int, n), make([]RouteRow, 0, n), make([]aux, 0, n)
		b.routes()
		b.sort()
		ix.Routes = b.rows
		b.index(ix)
		b.views(ix)
		b.gateways(ix, lsViews)
	}
	ix.parentKinds = b.parents
	ix.Findings = b.worst
	ix.admitted = b.admit(ix)
	return ix
}

// findings fills the worst finding per object and the install-wide unknown rule.
func (b *builder) findings() {
	b.worst = map[objects.Ref]Worst{}
	fs := b.in.Health.Findings
	for i := range fs {
		f := &fs[i]
		if f.Resource == nil || (f.Severity != health.SeverityError && f.Severity != health.SeverityWarning) {
			continue
		}
		b.worst[*f.Resource] = b.worst[*f.Resource].add(f)
		if f.Code == health.CodeComputedDisagrees {
			if b.disagrees == nil {
				b.disagrees = map[objects.Ref]bool{}
			}
			b.disagrees[*f.Resource] = true
		}
	}
	b.unknown = requiredNotOK(b.in.Health)
}

func (b *builder) state(ref objects.Ref) health.State { return b.worst[ref].state(b.unknown) }

// rowFor returns the index of u's row, creating it on first use. Rows are
// addressed by index because the slice may still grow.
func (b *builder) rowFor(k kinds.Served, u *unstructured.Unstructured, owned bool) int {
	ref := objects.RefOf(u)
	i, ok := b.at[ref]
	if !ok {
		i = len(b.rows)
		r, a := RouteRow{Ref: ref, Kind: k, Obj: u}, aux{}
		if isHTTPRoute(k.Group, k.Kind) {
			v := b.dec.HTTPRoute(u)
			r.View, r.Hostnames, a.status, a.specRefs, a.written = v, v.Hostnames, v.Parents, v.ParentRefs, v.WrittenRefs
		} else {
			v := b.dec.Route(u)
			r.Hostnames, a.status, a.specRefs, a.targets, a.written = v.Hostnames, v.Parents, v.ParentRefs, v.Targets, v.WrittenRefs
		}
		b.at[ref] = i
		b.rows, b.aux = append(b.rows, r), append(b.aux, a)
	}
	b.rows[i].Owned = b.rows[i].Owned || owned
	return i
}

// link adds l to row i unless it already has a link for the same parentRef.
func (b *builder) link(i int, l ParentLink) {
	r := &b.rows[i]
	if slices.ContainsFunc(r.Parents, func(x ParentLink) bool { return x.Ref == l.Ref && x.Target == l.Target }) {
		return
	}
	specs := b.aux[i].specRefs
	l.Spec = slices.Index(specs, l.Ref)
	for j := l.Spec + 1; l.Spec >= 0 && j < len(specs); j++ {
		if specs[j] == l.Ref {
			l.Dups = append(l.Dups, j)
		}
	}
	r.Parents = append(r.Parents, l)
}

// routes collects every row of design §3.5.
func (b *builder) routes() {
	o := b.in.Owned
	// One backing array for the owned routes' links; each row gets a capped window.
	n := 0
	for _, r := range o.Routes {
		n += len(r.Parents) + len(r.RouteParents)
	}
	arena := make([]ParentLink, n)
	for _, r := range o.Routes {
		i := b.rowFor(r.Kind, r.Obj, true)
		if k := len(r.Parents) + len(r.RouteParents); b.rows[i].Parents == nil && k > 0 {
			b.rows[i].Parents, arena = arena[:0:k], arena[k:]
		}
		for _, p := range r.Parents {
			b.link(i, b.fromStatus(i, p))
		}
		for _, p := range r.RouteParents {
			b.link(i, b.fromStatus(i, p))
			if a := &b.aux[i]; !slices.Contains(a.edges, p.Ref()) {
				a.edges = append(a.edges, p.Ref())
			}
		}
	}
	for _, u := range o.Unattached {
		i := b.rowFor(u.Route.Kind, u.Route.Obj, false)
		b.link(i, ParentLink{Target: u.Parent.Ref(), Ref: u.Parent, Attachment: AttachmentPending, Pending: pendingParentMissing})
		b.rows[i].Flags |= FlagPending
	}
	for _, c := range o.Declared {
		i := b.rowFor(c.Route.Kind, c.Route.Obj, false)
		b.link(i, ParentLink{Target: c.Parent.Ref(), Ref: c.Parent, Attachment: AttachmentDeclared})
		b.rows[i].Flags |= FlagDeclared
	}
	b.unwatchedRows()
	for _, r := range o.NoParent {
		i := b.rowFor(r.Kind, r.Obj, false)
		b.rows[i].Flags |= FlagNoParent
	}
}

// unwatchedRows adds this install's unwatched routes (KGWUI-004). Assign
// records one target per route; the row links every spec parentRef that
// names a parent of this install, so a route naming a Gateway and its
// ListenerSet is under both. An implicit child links the recorded target.
func (b *builder) unwatchedRows() {
	id := b.in.Install.ID()
	want := map[objects.Ref]objects.Ref{} // route → recorded target
	for _, u := range b.in.Assignment.Unwatched {
		if u.Install == id {
			want[u.Route] = u.Gateway
		}
	}
	if len(want) == 0 || b.in.Snapshot == nil {
		return
	}
	// One pass over the snapshot's routes, in snapshot order; rows are sorted later.
	for _, k := range kinds.ByCategory(b.in.Served, kinds.CategoryRoute) {
		for _, u := range b.in.Snapshot.Objects[k.GVR()] {
			target, ok := want[objects.RefOf(u)]
			if !ok {
				continue
			}
			i := b.rowFor(k, u, false)
			named := false
			for _, p := range b.aux[i].specRefs {
				if p.Ref() == target || b.ourParent(p.Ref()) {
					b.link(i, b.unwatched(p))
					named = true
				}
			}
			if !named {
				b.link(i, b.unwatched(objects.ParentRef{Group: target.Group, Kind: target.Kind, Namespace: target.Namespace, Name: target.Name}))
			}
		}
	}
}

// ourParent reports whether ref is a Gateway, ListenerSet or HTTPRoute the install owns.
func (b *builder) ourParent(ref objects.Ref) bool {
	if b.ownedGW[ref] || b.lsGW[ref] != nil {
		return true
	}
	if isHTTPRoute(ref.Group, ref.Kind) {
		_, ok := b.in.Owned.Route(ref)
		return ok
	}
	return false
}

func (b *builder) unwatched(p objects.ParentRef) ParentLink {
	return ParentLink{Target: p.Ref(), Ref: p, Via: b.via(p.Ref()), Attachment: AttachmentUnwatched}
}

// via returns the Gateway of an owned ListenerSet target.
func (b *builder) via(target objects.Ref) *objects.Ref {
	return b.lsGW[target]
}

// fromStatus decides the attachment of row i to parent p from the status entry
// of the install's controllers, current entries only (Phase 1 freshness),
// and adds the row's flags (design §6.1).
func (b *builder) fromStatus(i int, p objects.ParentRef) ParentLink {
	r, status := &b.rows[i], b.aux[i].status
	l := ParentLink{Target: p.Ref(), Ref: p, Via: b.via(p.Ref())}
	var st *objects.RouteParentStatus
	for j := range status {
		if status[j].ParentRef == p && slices.Contains(b.in.Install.ControllerNames, status[j].ControllerName) {
			st = &status[j]
			break
		}
	}
	if st == nil {
		l.Attachment, l.Pending = AttachmentPending, pendingNoStatus
		r.Flags |= FlagPending
		return l
	}
	if why := health.RoutePending(r.Obj, st.Conditions); why != "" {
		l.Attachment, l.Pending = AttachmentPending, why
		r.Flags |= FlagPending
		return l
	}
	// Current: Accepted and ResolvedRefs are present and neither is Unknown.
	cs := st.Conditions
	acc := objects.FindCondition(cs, "Accepted")
	if acc.Status == metav1.ConditionTrue {
		l.Attachment = AttachmentAccepted
		r.Flags |= FlagAccepted
	} else {
		l.Attachment = AttachmentNotAccepted
		r.Flags |= FlagNotAccepted
		if acc.Reason == "RouteRuleReplaced" || acc.Reason == "RouteRuleDropped" {
			r.Flags |= FlagNotProgrammed // 2.3 reports these through Accepted
		}
	}
	if c := objects.FindCondition(cs, "ResolvedRefs"); c.Status == metav1.ConditionFalse {
		r.Flags |= FlagRefsUnresolved
	}
	if c := objects.FindCondition(cs, programmedType); c != nil && c.Status == metav1.ConditionFalse {
		r.Flags |= FlagNotProgrammed
	}
	for _, t := range []string{"Accepted", "ResolvedRefs", programmedType} {
		if c := objects.FindCondition(cs, t); c != nil && c.Status == metav1.ConditionFalse {
			l.Cond = c
			break
		}
	}
	return l
}

// compareRefs orders by namespace, name, group and kind (design §3.6 tie-break).
// It stops at the first field that differs: Index.Row calls it per probe.
func compareRefs(a, b objects.Ref) int {
	if c := strings.Compare(a.Namespace, b.Namespace); c != 0 {
		return c
	}
	if c := strings.Compare(a.Name, b.Name); c != 0 {
		return c
	}
	if c := strings.Compare(a.Group, b.Group); c != 0 {
		return c
	}
	return strings.Compare(a.Kind, b.Kind)
}

func compareParentRefs(a, b objects.ParentRef) int {
	return cmp.Or(compareRefs(a.Ref(), b.Ref()), cmp.Compare(a.SectionName, b.SectionName), cmp.Compare(a.Port, b.Port))
}

// sort puts the rows in (namespace, name, group, kind) order in place, with
// their aux. Parents follow spec order, then parentRef order; reported
// parent routes are sorted.
func (b *builder) sort() {
	sort.Sort(byRef{b.rows, b.aux})
	for i := range b.rows {
		r, a := &b.rows[i], &b.aux[i]
		specIdx := func(p objects.ParentRef) int {
			if j := slices.Index(a.specRefs, p); j >= 0 {
				return j
			}
			return len(a.specRefs)
		}
		slices.SortStableFunc(r.Parents, func(x, y ParentLink) int {
			return cmp.Or(cmp.Compare(specIdx(x.Ref), specIdx(y.Ref)), compareParentRefs(x.Ref, y.Ref), compareRefs(x.Target, y.Target))
		})
		slices.SortFunc(a.edges, compareRefs)
		r.ParentRoutes = a.edges
		r.State = b.state(r.Ref)
		r.search = search(r.Ref, r.Hostnames)
	}
}

// byRef sorts rows and their aux together.
type byRef struct {
	rows []RouteRow
	aux  []aux
}

func (s byRef) Len() int           { return len(s.rows) }
func (s byRef) Less(i, j int) bool { return compareRefs(s.rows[i].Ref, s.rows[j].Ref) < 0 }
func (s byRef) Swap(i, j int) {
	s.rows[i], s.rows[j] = s.rows[j], s.rows[i]
	s.aux[i], s.aux[j] = s.aux[j], s.aux[i]
}

// search is the lower-cased "namespace/name" and hostnames, one per line.
func search(ref objects.Ref, hosts []string) string {
	var sb strings.Builder
	sb.WriteString(strings.ToLower(ref.Namespace + "/" + ref.Name))
	for _, h := range hosts {
		sb.WriteByte('\n')
		sb.WriteString(strings.ToLower(h))
	}
	return sb.String()
}

// appendOnce appends id unless it is already last, so a row is listed once per key.
func appendOnce[K comparable](m map[K][]RowID, k K, id RowID) {
	if l := m[k]; len(l) == 0 || l[len(l)-1] != id {
		m[k] = append(l, id)
	}
}

// index fills the route indexes from the sorted rows; b.aux is in row order.
func (b *builder) index(ix *Index) {
	ix.ByParent, ix.ByParentRoute, ix.Declared = map[objects.Ref][]RowID{}, map[objects.Ref][]RowID{}, map[objects.Ref][]RowID{}
	ix.ByBackend, ix.ByNamespace, ix.ParentRoutes = map[objects.Ref][]BackendUse{}, map[string][]RowID{}, map[objects.Ref][]objects.Ref{}
	var uses []BackendUse // reused per row
	var targets []objects.Ref
	for i := range ix.Routes {
		r, id := &ix.Routes[i], RowID(i)
		appendOnce(ix.ByNamespace, r.Ref.Namespace, id)
		if r.Flags&FlagNoParent != 0 {
			ix.NoParent = append(ix.NoParent, id)
		}
		for _, l := range r.Parents {
			switch {
			case l.Attachment == AttachmentDeclared:
				appendOnce(ix.Declared, l.Target, id)
				continue
			case l.Attachment == AttachmentUnwatched:
			case !b.parents[objects.GroupKind{Group: l.Target.Group, Kind: l.Target.Kind}]:
				continue // reported edges and missing HTTPRoute parents
			}
			appendOnce(ix.ByParent, l.Target, id)
			if l.Via != nil {
				appendOnce(ix.ByParent, *l.Via, id)
			}
		}
		for _, p := range r.ParentRoutes {
			appendOnce(ix.ByParentRoute, p, id)
		}
		if len(r.ParentRoutes) > 0 {
			ix.ParentRoutes[r.Ref] = r.ParentRoutes
		}
		// One use per backend; Mirror only when every mention is a mirror.
		uses, targets = uses[:0], targets[:0]
		a := &b.aux[i]
		if r.View != nil {
			a.targets = objects.BackendTargets(r.View)
		}
		for _, t := range a.targets {
			if j := slices.Index(targets, t.Ref); j >= 0 {
				uses[j].Mirror = uses[j].Mirror && t.Mirror
				continue
			}
			targets = append(targets, t.Ref)
			uses = append(uses, BackendUse{Row: id, Mirror: t.Mirror})
		}
		for j, t := range targets {
			ix.ByBackend[t] = append(ix.ByBackend[t], uses[j])
		}
	}
	for i := range ix.Routes {
		ix.Routes[i].Children = len(ix.ByParentRoute[ix.Routes[i].Ref])
	}
}

// gateways fills the Gateway and ListenerSet rows; lsViews is index-aligned
// with Owned.ListenerSets.
func (b *builder) gateways(ix *Index, lsViews []*objects.ListenerSetView) {
	o := b.in.Owned
	gws := slices.Clone(o.Gateways)
	slices.SortFunc(gws, func(x, y *unstructured.Unstructured) int { return compareRefs(objects.RefOf(x), objects.RefOf(y)) })
	ix.Gateways = make([]GatewayRow, len(gws))
	gwAt := make(map[objects.Ref]int, len(gws))
	for i, u := range gws {
		ref := objects.RefOf(u)
		v := b.dec.Gateway(u)
		g := GatewayRow{Ref: ref, Obj: u, View: v, State: b.state(ref), Routes: ix.ByParent[ref],
			Members: ix.members(ref, v.Listeners), Disagreements: b.disagreements[ref]}
		for _, l := range v.ListenerStatus {
			g.AttachedRoutes += l.AttachedRoutes
		}
		for _, id := range g.Routes {
			if slices.ContainsFunc(ix.Routes[id].Parents, func(l ParentLink) bool {
				return l.Attachment == AttachmentNotAccepted && (l.Target == ref || l.Via != nil && *l.Via == ref)
			}) {
				g.NotAccepted = append(g.NotAccepted, id)
			}
		}
		ix.Gateways[i], gwAt[ref] = g, i
	}
	order := make([]int, len(o.ListenerSets))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(x, y int) int {
		return compareRefs(objects.RefOf(o.ListenerSets[x]), objects.RefOf(o.ListenerSets[y]))
	})
	ix.ListenerSets = make([]ListenerSetRow, len(order))
	for i, j := range order {
		u, v := o.ListenerSets[j], lsViews[j]
		ref := objects.RefOf(u)
		l := ListenerSetRow{Ref: ref, Obj: u, View: v, Parent: v.Parent.Ref(), State: b.state(ref), Routes: ix.ByParent[ref],
			Members: ix.members(ref, v.Listeners), Disagreements: b.disagreements[ref]}
		if c := objects.FindCondition(v.Conditions, "Accepted"); c != nil && c.Status != metav1.ConditionTrue {
			l.AcceptedReason = c.Reason
		}
		if g, ok := gwAt[l.Parent]; ok {
			l.ParentFound = true
			ix.Gateways[g].ListenerSets = append(ix.Gateways[g].ListenerSets, RowID(i))
		}
		ix.ListenerSets[i] = l
	}
}

// interner maps refs to Index.Refs indexes. A route row's index is its RowID;
// other refs are added on first use. Source dep slices are shared between
// results, so each one is converted once and its DepSet reused.
type interner struct {
	ix *Index
	// routes are the served route kinds.
	routes map[objects.GroupKind]bool
	at     map[objects.Ref]int32
	refs   []objects.Ref
	seen   map[depKey]DepSet
	// offSeen caches offPath's results as seen caches deps'.
	offSeen map[depKey]DepSet
	// buf is the current arena chunk for new DepSets.
	buf []int32
}

// depKey identifies a source dep slice by its first element and length.
type depKey struct {
	p *objects.Ref
	n int
}

// deps interns src without changing it.
func (n *interner) deps(src []objects.Ref) DepSet {
	if len(src) == 0 {
		return nil
	}
	k := depKey{&src[0], len(src)}
	if d, ok := n.seen[k]; ok {
		return d
	}
	if cap(n.buf)-len(n.buf) < len(src) {
		n.buf = make([]int32, 0, max(1024, len(src)))
	}
	start := len(n.buf)
	for _, r := range src {
		n.buf = append(n.buf, n.ref(r))
	}
	d := DepSet(n.buf[start:len(n.buf):len(n.buf)])
	n.seen[k] = d
	return d
}

// offPath interns src without its HTTPRoutes. Every route in a view B or C
// dependency set is on the result's path, and those never restrict (rules
// §5.6, D-024). TestContextDeps in kgw23 and kgw24 pins that invariant,
// which keeps dropping by kind sound.
func (n *interner) offPath(src []objects.Ref) DepSet {
	if len(src) == 0 {
		return nil
	}
	k := depKey{&src[0], len(src)}
	if d, ok := n.offSeen[k]; ok {
		return d
	}
	if cap(n.buf)-len(n.buf) < len(src) {
		n.buf = make([]int32, 0, max(1024, len(src)))
	}
	start := len(n.buf)
	for _, r := range src {
		if !isHTTPRoute(r.Group, r.Kind) {
			n.buf = append(n.buf, n.ref(r))
		}
	}
	var d DepSet
	if len(n.buf) > start {
		d = DepSet(n.buf[start:len(n.buf):len(n.buf)])
	}
	n.offSeen[k] = d
	return d
}

// ref returns r's index, adding it on first use.
func (n *interner) ref(r objects.Ref) int32 {
	if n.routes[objects.GroupKind{Group: r.Group, Kind: r.Kind}] {
		if id, ok := n.ix.Row(r); ok {
			return int32(id)
		}
	}
	i, ok := n.at[r]
	if !ok {
		i = int32(len(n.refs)) // #nosec G115 -- a ref count fits in int32
		n.at[r] = i
		n.refs = append(n.refs, r)
	}
	return i
}

// union32 is the union of a and b; it shares one when the other adds nothing, else sorts.
func union32(a, b DepSet) DepSet {
	switch {
	case len(b) == 0 || slices.Equal(a, b):
		return a
	case len(a) == 0:
		return b
	}
	out := append(slices.Clone(a), b...)
	slices.Sort(out)
	return slices.Compact(out)
}

// views fills view A into the index: listener facts, per-ref facts, the
// listener index, cross-checks and the computed flags (design §3.5).
func (b *builder) views(ix *Index) {
	v := b.in.Computed
	if v == nil {
		return
	}
	ix.RuleSet, ix.RuleNote = v.RuleSet, v.Note
	a := &v.Attachment
	// Route rows take the first indexes; the map holds resources and Namespaces.
	n := &interner{ix: ix, routes: map[objects.GroupKind]bool{}, at: map[objects.Ref]int32{}, seen: map[depKey]DepSet{}, offSeen: map[depKey]DepSet{},
		refs: make([]objects.Ref, len(ix.Routes), len(ix.Routes)+len(a.Counts))}
	for i := range ix.Routes {
		n.refs[i] = ix.Routes[i].Ref
	}
	for _, k := range kinds.ByCategory(b.in.Served, kinds.CategoryRoute) {
		n.routes[objects.GroupKind{Group: k.Group, Kind: k.Kind}] = true
	}
	ix.Listeners = make(map[ListenerKey]ListenerFacts, len(a.Counts))
	for i := range a.Counts {
		c := &a.Counts[i]
		ix.Listeners[ListenerKey{c.Resource, c.Listener}] = ListenerFacts{Count: c.Count, Known: c.Unknown == "", Unknown: c.Unknown,
			Error: c.Error, IsolationNote: c.IsolationNote}
	}
	b.refFacts(ix, n)
	ix.ByListener = make(map[ListenerKey][]Member, len(a.Counts))
	// Members come grouped by listener: each group takes a window of one arena.
	arena := make([]Member, len(a.Members))
	for i := range a.Members {
		m := &a.Members[i]
		id, ok := ix.Row(m.Route)
		if !ok {
			continue
		}
		deps := n.deps(m.Deps)
		if f := ix.Routes[id].facts(m.ParentRef, m.Resource); f != nil {
			for j := range f.Listeners {
				if h := &f.Listeners[j]; h.Listener == m.Listener {
					h.Hostnames, h.Deps = m.Hostnames, deps
				}
			}
		}
		// An unknown count presents no members, even when the rules list some (setting-unknown).
		k := ListenerKey{m.Resource, m.Listener}
		if !ix.Listeners[k].Known {
			continue
		}
		ms, ok := ix.ByListener[k]
		if !ok {
			j := i + 1
			for j < len(a.Members) && a.Members[j].Resource == k.Resource && a.Members[j].Listener == k.Listener {
				j++
			}
			ms = arena[i:i:j]
		}
		ix.ByListener[k] = append(ms, Member{Row: id, ParentRef: m.ParentRef, Hostnames: m.Hostnames, Deps: deps})
	}
	for _, ms := range ix.ByListener {
		slices.SortFunc(ms, func(x, y Member) int { return cmp.Or(cmp.Compare(x.Row, y.Row), cmp.Compare(x.ParentRef, y.ParentRef)) })
	}
	b.checks(ix, v.Checks)
	for i := range ix.Routes {
		r := &ix.Routes[i]
		if b.disagrees[r.Ref] {
			r.Flags |= FlagDisagrees
		}
		b.listenerUnknown(ix, i, n)
	}
	b.delegation(ix, n, v)
	ix.Refs = n.refs
}

// compareRowKeys orders delegation rows by parent, rule, ref and child,
// refs as compareRefs orders them (design §3.6).
func compareRowKeys(a, b *computed.RowKey) int {
	return cmp.Or(compareRefs(a.Parent, b.Parent), cmp.Compare(a.Rule, b.Rule), cmp.Compare(a.Ref, b.Ref), compareRefs(a.Child, b.Child))
}

// delegation fills views B and C into the index: the rows in order, their
// per-parent and per-ref windows, DepSets, child-entry checks, the dropped
// flag and the effective edges (design §3.5). The windows share one array.
func (b *builder) delegation(ix *Index, n *interner, v *computed.Views) {
	d := &v.Delegation
	ix.DelegationCap = d.Cap
	el := eligibility(n, v)
	if len(d.Rows) > 0 {
		delegRows(ix, n, d.Rows, v.Checks, el)
		b.refScopes(ix, n)
	}
	if len(d.Effective) > 0 {
		effective(ix, n, d.Effective, el)
	}
}

// delegRows copies rows into ix.DelegRows, sorted, and indexes them.
func delegRows(ix *Index, n *interner, src []computed.Row, checks []computed.CheckResult, el *eligible) {
	// The views list rows in RowKey order, which agrees with ours; sort only when not.
	order := make([]int, len(src))
	for i := range order {
		order[i] = i
	}
	for i := 1; i < len(src); i++ {
		if compareRowKeys(&src[i-1].Key, &src[i].Key) > 0 {
			slices.SortStableFunc(order, func(x, y int) int { return compareRowKeys(&src[x].Key, &src[y].Key) })
			break
		}
	}
	rows := make([]DelegRow, len(src))
	nctx, parents, refs := 0, 0, 0
	for i, j := range order {
		rows[i].Row = src[j]
		nctx += len(src[j].Contexts)
		if k := &src[j].Key; i == 0 || k.Parent != rows[i-1].Key.Parent {
			parents, refs = parents+1, refs+1
		} else if k.Rule != rows[i-1].Key.Rule || k.Ref != rows[i-1].Key.Ref {
			refs++
		}
	}
	ix.DelegRows = rows
	ix.Delegation, ix.RefRows = make(map[objects.Ref][]int, parents), make(map[RefKey][]int, refs)
	// order now holds 0..n-1: each parent and ref takes a window of it.
	for i := range order {
		order[i] = i
	}
	type checkKey struct {
		child, parent objects.Ref
		key           computed.ReportKey
	}
	byKey := map[checkKey]*computed.CheckResult{}
	for i := range checks {
		if c := &checks[i]; c.ID.Kind == computed.KindChildEntry {
			if p, ok := c.Parent(); ok {
				byKey[checkKey{c.On, p, c.Key}] = c
			}
		}
	}
	ctxDeps := make([]DepSet, nctx)
	pLo, rLo, owned := 0, 0, false
	for i := range rows {
		r := &rows[i]
		k := &r.Key
		if i == 0 || k.Parent != rows[i-1].Key.Parent {
			id, ok := ix.Row(k.Parent)
			pLo, rLo, owned = i, i, ok && ix.Routes[id].Owned
		} else if k.Rule != rows[i-1].Key.Rule || k.Ref != rows[i-1].Key.Ref {
			rLo = i
		}
		// Close the windows at the last row of a ref and of a parent.
		if last := i+1 == len(rows); last || rows[i+1].Key.Parent != k.Parent {
			ix.Delegation[k.Parent] = order[pLo : i+1 : i+1]
			ix.RefRows[RefKey{k.Parent, k.Rule, k.Ref}] = order[rLo : i+1 : i+1]
		} else if nx := &rows[i+1].Key; nx.Rule != k.Rule || nx.Ref != k.Ref {
			ix.RefRows[RefKey{k.Parent, k.Rule, k.Ref}] = order[rLo : i+1 : i+1]
		}
		r.Deps = el.with(n.offPath(r.Row.Deps), k.Parent)
		if isUnreached(r) && len(r.Deps) > 0 && k.Child != (objects.Ref{}) {
			if ix.Unreached == nil {
				ix.Unreached = map[objects.Ref][]int{}
			}
			ix.Unreached[k.Child] = append(ix.Unreached[k.Child], i)
		}
		if c := len(r.Contexts); c > 0 {
			r.ContextDeps, ctxDeps = ctxDeps[:c:c], ctxDeps[c:]
			for j := range r.Contexts {
				r.ContextDeps[j] = n.offPath(r.Contexts[j].Deps)
			}
		}
		if k.Child == (objects.Ref{}) {
			continue
		}
		r.Check = byKey[checkKey{k.Child, k.Parent, r.ChildKey}]
		switch r.Outcome {
		case computed.OutcomeDropped, computed.OutcomeForbidden, computed.OutcomeRejectedHostnames:
			if id, ok := ix.Row(k.Child); ok && owned {
				c := &ix.Routes[id]
				c.Flags |= FlagDropped
				if !slices.ContainsFunc(c.DroppedDeps, func(d DepSet) bool { return slices.Equal(d, r.Deps) }) {
					c.DroppedDeps = append(c.DroppedDeps, r.Deps)
				}
			}
		}
	}
}

// isUnreached reports whether no context reached row r: it is unknown,
// not-reached, with no dependencies of its own.
func isUnreached(r *DelegRow) bool {
	return r.Outcome == computed.OutcomeUnknown && r.Unknown == computed.UnknownNotReached && len(r.Row.Deps) == 0
}

// eligible holds, per route, the dependencies that decide whether a root
// reaches it: the view A outcome deps, without routes, of every top-level
// route that reaches it in the reach graph, itself included (rules §8.5).
// Whether a route is a root at all can depend on a hidden Namespace or
// target, so every row and edge below it carries them (design §7.4 rule 4,
// our call, 2.4). Sets are sorted and interned by content.
type eligible struct {
	at map[objects.Ref]DepSet
	// merged caches with per (source set, eligibility set).
	merged map[[2]depKey32]DepSet
	intern map[string]DepSet
}

// depKey32 identifies an interned DepSet by its first element and length.
type depKey32 struct {
	p *int32
	n int
}

func keyOf(d DepSet) depKey32 {
	if len(d) == 0 {
		return depKey32{}
	}
	return depKey32{&d[0], len(d)}
}

// eligibility builds eligible from view A's outcomes and the reach graph. The
// sets flow forward from each top-level route along reach edges until none
// grows; each grows at most once per distinct dependency, so the work is
// O(edges × dependencies), once per recompute.
func eligibility(n *interner, v *computed.Views) *eligible {
	el := &eligible{at: map[objects.Ref]DepSet{}, merged: map[[2]depKey32]DepSet{}, intern: map[string]DepSet{}}
	reach, outs := v.Delegation.Reach, v.Attachment.Outcomes
	if len(v.Delegation.Rows) == 0 && len(v.Delegation.Effective) == 0 {
		return el
	}
	id := make(map[objects.Ref]int32, len(reach)+len(outs))
	var refs []objects.Ref
	node := func(r objects.Ref) int32 {
		i, ok := id[r]
		if !ok {
			i = int32(len(refs)) // #nosec G115 -- a route count fits in int32
			id[r] = i
			refs = append(refs, r)
		}
		return i
	}
	// Reach is sorted by parent: each parent's children are one window of kids.
	kids := make([]int32, len(reach))
	for i, e := range reach {
		node(e.Parent)
		kids[i] = node(e.Child)
	}
	for i := range outs {
		node(outs[i].Route)
	}
	lo, hi := make([]int, len(refs)), make([]int, len(refs))
	for i, e := range reach {
		p := id[e.Parent]
		if hi[p] == 0 {
			lo[p] = i
		}
		hi[p] = i + 1
	}
	sets := make([]DepSet, len(refs))
	queued := make([]bool, len(refs))
	var queue []int32
	push := func(c int32, add DepSet) {
		if m := mergeSorted(sets[c], add); len(m) > len(sets[c]) {
			sets[c] = m
			if !queued[c] {
				queued[c], queue = true, append(queue, c)
			}
		}
	}
	for i := range outs {
		push(id[outs[i].Route], sortedDeps(n.offPath(outs[i].Deps)))
	}
	for len(queue) > 0 {
		p := queue[0]
		queue, queued[p] = queue[1:], false
		for _, c := range kids[lo[p]:hi[p]] {
			push(c, sets[p])
		}
	}
	for i, d := range sets {
		if len(d) > 0 {
			el.at[refs[i]] = el.interned(d)
		}
	}
	return el
}

// with is d, in any order, united with the eligibility deps of parent.
func (el *eligible) with(d DepSet, parent objects.Ref) DepSet {
	e := el.at[parent]
	if len(e) == 0 {
		return d
	}
	k := [2]depKey32{keyOf(d), keyOf(e)}
	if m, ok := el.merged[k]; ok {
		return m
	}
	m := el.interned(mergeSorted(e, sortedDeps(d)))
	el.merged[k] = m
	return m
}

// interned returns the shared copy of the sorted set d.
func (el *eligible) interned(d DepSet) DepSet {
	b := make([]byte, 0, 4*len(d))
	for _, x := range d {
		b = binary.LittleEndian.AppendUint32(b, uint32(x)) // #nosec G115 -- indexes are non-negative
	}
	if s, ok := el.intern[string(b)]; ok {
		return s
	}
	el.intern[string(b)] = d
	return d
}

// sortedDeps is d sorted, without repeats; d itself when it already is.
func sortedDeps(d DepSet) DepSet {
	for i := 1; i < len(d); i++ {
		if d[i-1] >= d[i] {
			c := slices.Clone(d)
			slices.Sort(c)
			return slices.Compact(c)
		}
	}
	return d
}

// mergeSorted is the union of sorted a and b in one pass; a when b adds nothing.
func mergeSorted(a, b DepSet) DepSet {
	out := make(DepSet, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case j == len(b) || i < len(a) && a[i] < b[j]:
			out = append(out, a[i])
			i++
		case i == len(a) || b[j] < a[i]:
			out = append(out, b[j])
			j++
		default:
			out = append(out, a[i])
			i, j = i+1, j+1
		}
	}
	if len(out) == len(a) {
		return a
	}
	return out
}

// refScopes fills ix.RefScope: per delegation ref, the routes its selection
// reads (our call, 2.4). A name ref reads its target, present or not; a
// wildcard ref every in-scope HTTPRoute of its namespace; a label ref those
// of every namespace it may select from. Refs are interned in row order, so
// the dependency table does not depend on map order.
func (b *builder) refScopes(ix *Index, n *interner) {
	byNS := map[string]DepSet{}
	var all DepSet
	res, snap := b.in.Install, b.in.Snapshot
	if snap != nil {
		nss := make(map[string]int, len(snap.Namespaces))
		for i, ns := range snap.Namespaces {
			nss[ns.Name] = i
		}
		for _, k := range b.in.Served {
			if !isHTTPRoute(k.Group, k.Kind) {
				continue
			}
			for _, u := range snap.Objects[k.GVR()] {
				ns := u.GetNamespace()
				i, ok := nss[ns]
				if (ok && !res.Scope.Contains(ns, snap.Namespaces[i])) || (!ok && !res.Scope.Contains(ns, nil)) {
					continue
				}
				x := n.ref(objects.RefOf(u))
				byNS[ns], all = append(byNS[ns], x), append(all, x)
			}
		}
	}
	for ns, d := range byNS {
		byNS[ns] = sortedDeps(d)
	}
	all = sortedDeps(all)
	wild := res.Settings.WildcardNamespace
	if wild.Known && wild.Value == "" {
		wild.Value = install.WildcardAll // an empty value means all, as the selector reads it
	}
	ix.RefScope = make(map[RefKey]DepSet, len(ix.RefRows))
	for i := range ix.DelegRows {
		r := &ix.DelegRows[i]
		k := RefKey{r.Key.Parent, r.Key.Rule, r.Key.Ref}
		if _, done := ix.RefScope[k]; done {
			continue
		}
		as := r.RefAsWritten
		switch as.Mode {
		case objects.DelegationName:
			ix.RefScope[k] = DepSet{n.ref(objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: as.Namespace, Name: as.Name})}
		case objects.DelegationLabel:
			if !wild.Known || as.Namespace == wild.Value {
				ix.RefScope[k] = all
			} else {
				ix.RefScope[k] = byNS[as.Namespace]
			}
		default:
			ix.RefScope[k] = byNS[as.Namespace]
		}
	}
}

// effective fills ix.Effective from view C. Children are interned in row
// order, so the dependency table does not depend on map order.
func effective(ix *Index, n *interner, src map[objects.Ref]computed.ChildEffective, el *eligible) {
	ne, nc := 0, 0
	for _, ce := range src {
		ne += len(ce.Edges)
		for j := range ce.Edges {
			nc += len(ce.Edges[j].Contexts)
		}
	}
	edges, outer, inner := make([]DepSet, ne), make([][]DepSet, ne), make([]DepSet, nc)
	ix.Effective = make(map[objects.Ref]EffectiveEntry, len(src))
	add := func(child objects.Ref) {
		ce, ok := src[child]
		if _, done := ix.Effective[child]; !ok || done {
			return
		}
		k := len(ce.Edges)
		e := EffectiveEntry{ChildEffective: ce, EdgeDeps: edges[:k:k], ContextDeps: outer[:k:k]}
		edges, outer = edges[k:], outer[k:]
		for j := range ce.Edges {
			ed := &ce.Edges[j]
			e.EdgeDeps[j] = el.with(n.offPath(ed.Deps), ed.Parent)
			c := len(ed.Contexts)
			e.ContextDeps[j], inner = inner[:c:c], inner[c:]
			for x := range ed.Contexts {
				e.ContextDeps[j][x] = n.offPath(ed.Contexts[x].Deps)
			}
		}
		ix.Effective[child] = e
	}
	for i := range ix.DelegRows {
		add(ix.DelegRows[i].Key.Child)
	}
	// An edge needs a row of its (parent, child), so this is normally empty.
	if len(ix.Effective) < len(src) {
		rest := make([]objects.Ref, 0, len(src)-len(ix.Effective))
		for c := range src {
			if _, ok := ix.Effective[c]; !ok {
				rest = append(rest, c)
			}
		}
		slices.SortFunc(rest, compareRefs)
		for _, c := range rest {
			add(c)
		}
	}
}

// refFacts fills each row's Computed from the outcomes, in one arena.
func (b *builder) refFacts(ix *Index, n *interner) {
	outs := b.in.Computed.Attachment.Outcomes
	rows := make([]RowID, len(outs))
	per := make([]int, len(ix.Routes))
	hits := 0
	for i := range outs {
		id, ok := ix.Row(outs[i].Route)
		if !ok {
			rows[i] = -1
			continue
		}
		rows[i] = id
		per[id]++
		hits += len(outs[i].Listeners)
	}
	arena, hitArena := make([]RefFacts, len(outs)), make([]ListenerHit, hits)
	for id, k := range per {
		if k > 0 {
			ix.Routes[id].Computed, arena = arena[:0:k], arena[k:]
		}
	}
	for i := range outs {
		o := &outs[i]
		if rows[i] < 0 {
			continue
		}
		f := RefFacts{Out: o, Deps: n.deps(o.Deps)}
		if k := len(o.Listeners); k > 0 {
			f.Listeners, hitArena = hitArena[:k:k], hitArena[k:]
			for j, l := range o.Listeners {
				f.Listeners[j] = ListenerHit{Listener: l, Deps: f.Deps}
			}
		}
		r := &ix.Routes[rows[i]]
		r.Computed = append(r.Computed, f)
	}
	for i := range ix.Routes {
		slices.SortStableFunc(ix.Routes[i].Computed, func(x, y RefFacts) int { return cmp.Compare(x.Out.ParentRef, y.Out.ParentRef) })
	}
}

// facts returns the facts of parentRef p to target, or nil.
func (r *RouteRow) facts(p int, target objects.Ref) *RefFacts {
	for i := range r.Computed {
		if f := &r.Computed[i]; f.Out.ParentRef == p && f.Out.Resource == target {
			return f
		}
	}
	return nil
}

// checks attaches each cross-check to its listener or parentRefs and counts disagreements.
func (b *builder) checks(ix *Index, cs []computed.CheckResult) {
	for i := range cs {
		c := &cs[i]
		if c.State == computed.StateDisagrees {
			if b.disagreements == nil {
				b.disagreements = map[objects.Ref]int{}
			}
			b.disagreements[c.On]++
		}
		switch c.ID.Kind {
		case computed.KindListenerCount:
			k := ListenerKey{c.On, c.Listener}
			if f, ok := ix.Listeners[k]; ok {
				f.Check = c
				ix.Listeners[k] = f
			}
		case computed.KindRouteAccepted:
			id, ok := ix.Row(c.On)
			if !ok {
				continue
			}
			r, written := &ix.Routes[id], b.aux[id].written
			// One check covers every parentRef with its report key (rules §8.2).
			for j := range r.Computed {
				if p := r.Computed[j].Out.ParentRef; p < len(written) && computed.KeyOf(written[p]) == c.Key {
					r.Computed[j].Check = c
				}
			}
		}
	}
}

// listenerUnknown flags row i when a status-accepted parentRef to a Gateway
// or ListenerSet has an unknown outcome or none with a listener.
func (b *builder) listenerUnknown(ix *Index, i int, n *interner) {
	r, a := &ix.Routes[i], &b.aux[i]
	for _, l := range r.Parents {
		if l.Attachment != AttachmentAccepted || !b.parents[objects.GroupKind{Group: l.Target.Group, Kind: l.Target.Kind}] {
			continue
		}
		var f *RefFacts
		if p := slices.Index(a.specRefs, l.Ref); p >= 0 {
			f = r.facts(p, l.Target)
		}
		switch {
		case f == nil:
			r.UnknownDeps = union32(r.UnknownDeps, DepSet{n.ref(r.Ref), n.ref(l.Target)})
		case f.Out.Unknown != "" || len(f.Listeners) == 0:
			r.UnknownDeps = union32(r.UnknownDeps, f.Deps)
		default:
			continue
		}
		r.Flags |= FlagListenerUnknown
	}
}

// members is the sorted distinct member rows of resource's listeners.
func (ix *Index) members(resource objects.Ref, ls []objects.Listener) []RowID {
	var out []RowID
	for _, l := range ls {
		for _, m := range ix.ByListener[ListenerKey{resource, l.Name}] {
			out = append(out, m.Row)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
