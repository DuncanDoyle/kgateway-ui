package explore

import (
	"cmp"
	"slices"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
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
		b.gateways(ix, lsViews)
	}
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
			r.View, r.Hostnames, a.status, a.specRefs = v, v.Hostnames, v.Parents, v.ParentRefs
		} else {
			v := b.dec.Route(u)
			r.Hostnames, a.status, a.specRefs, a.targets = v.Hostnames, v.Parents, v.ParentRefs, v.Targets
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
func compareRefs(a, b objects.Ref) int {
	return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Group, b.Group), cmp.Compare(a.Kind, b.Kind))
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
		g := GatewayRow{Ref: ref, Obj: u, View: v, State: b.state(ref), Routes: ix.ByParent[ref]}
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
		l := ListenerSetRow{Ref: ref, Obj: u, View: v, Parent: v.Parent.Ref(), State: b.state(ref), Routes: ix.ByParent[ref]}
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
