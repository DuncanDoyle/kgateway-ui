package explore

import (
	"cmp"
	"slices"
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

// draft is a route row being built, with what it needs until it is sorted.
type draft struct {
	row RouteRow
	// status are the row's status.parents; specRefs its spec.parentRefs.
	status   []objects.RouteParentStatus
	specRefs []objects.ParentRef
	// edges are the HTTPRoutes of the row's reported edges.
	edges []objects.Ref
	// targets are the row's backend targets, set by index.
	targets []objects.BackendTarget
}

// builder holds Build's per-call state.
type builder struct {
	in      Input
	dec     objects.Decoder
	drafts  []*draft
	at      map[objects.Ref]*draft
	lsGW    map[objects.Ref]*objects.Ref // owned ListenerSet → its Gateway, shared by every link
	parents map[objects.GroupKind]bool   // served Gateway and ListenerSet kinds
	unknown bool                         // a required check is not OK
	worst   map[objects.Ref]Worst
	byRef   map[objects.Ref]*unstructured.Unstructured // snapshot routes, built on first use
}

// Build builds the index of one install. It is pure: the same input gives
// the same rows in the same order, whatever the snapshot's order.
func Build(in Input) *Index {
	b := &builder{in: in, dec: in.Views, lsGW: map[objects.Ref]*objects.Ref{}, parents: map[objects.GroupKind]bool{}}
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
		lsViews := make([]*objects.ListenerSetView, len(o.ListenerSets))
		for i, ls := range o.ListenerSets {
			lsViews[i] = b.dec.ListenerSet(ls)
			g := lsViews[i].Parent.Ref()
			b.lsGW[objects.RefOf(ls)] = &g
		}
		n := len(o.Routes) + len(o.Unattached) + len(o.Declared) + len(o.NoParent)
		b.at, b.drafts = make(map[objects.Ref]*draft, n), make([]*draft, 0, n)
		b.routes()
		ix.Routes = b.sorted()
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

// draftFor returns the draft of u, creating it on first use.
func (b *builder) draftFor(k kinds.Served, u *unstructured.Unstructured, owned bool) *draft {
	ref := objects.RefOf(u)
	d, ok := b.at[ref]
	if !ok {
		d = &draft{row: RouteRow{Ref: ref, Kind: k, Obj: u}}
		if isHTTPRoute(k.Group, k.Kind) {
			v := b.dec.HTTPRoute(u)
			d.row.View, d.row.Hostnames, d.status, d.specRefs = v, v.Hostnames, v.Parents, v.ParentRefs
		} else {
			d.row.Hostnames = b.dec.RouteHostnames(u).Hostnames
			d.status, d.specRefs = objects.RouteParents(u), objects.RouteParentRefs(u)
		}
		b.at[ref] = d
		b.drafts = append(b.drafts, d)
	}
	d.row.Owned = d.row.Owned || owned
	return d
}

// link adds l to d unless d already has a link for the same parentRef.
func (d *draft) link(l ParentLink) {
	if slices.ContainsFunc(d.row.Parents, func(x ParentLink) bool { return x.Ref == l.Ref && x.Target == l.Target }) {
		return
	}
	d.row.Parents = append(d.row.Parents, l)
}

// routes collects every row of design §3.5.
func (b *builder) routes() {
	o := b.in.Owned
	for _, r := range o.Routes {
		d := b.draftFor(r.Kind, r.Obj, true)
		d.row.Parents = slices.Grow(d.row.Parents, len(r.Parents)+len(r.RouteParents))
		for _, p := range r.Parents {
			d.link(b.fromStatus(d, p))
		}
		for _, p := range r.RouteParents {
			d.link(b.fromStatus(d, p))
			if !slices.Contains(d.edges, p.Ref()) {
				d.edges = append(d.edges, p.Ref())
			}
		}
	}
	for _, u := range o.Unattached {
		d := b.draftFor(u.Route.Kind, u.Route.Obj, false)
		d.link(ParentLink{Target: u.Parent.Ref(), Ref: u.Parent, Attachment: AttachmentPending, Pending: pendingParentMissing})
		d.row.Flags |= FlagPending
	}
	for _, c := range o.Declared {
		d := b.draftFor(c.Route.Kind, c.Route.Obj, false)
		d.link(ParentLink{Target: c.Parent.Ref(), Ref: c.Parent, Attachment: AttachmentDeclared})
		d.row.Flags |= FlagDeclared
	}
	id := b.in.Install.ID()
	for _, u := range b.in.Assignment.Unwatched {
		if u.Install != id {
			continue
		}
		k, obj, ok := b.lookup(u.Route)
		if !ok {
			continue
		}
		d := b.draftFor(k, obj, false)
		named := false
		for _, p := range d.specRefs {
			if p.Ref() == u.Gateway {
				d.link(b.unwatched(p))
				named = true
			}
		}
		if !named { // an implicit child: no spec ref names the parent
			g := u.Gateway
			d.link(b.unwatched(objects.ParentRef{Group: g.Group, Kind: g.Kind, Namespace: g.Namespace, Name: g.Name}))
		}
	}
	for _, r := range o.NoParent {
		b.draftFor(r.Kind, r.Obj, false).row.Flags |= FlagNoParent
	}
}

func (b *builder) unwatched(p objects.ParentRef) ParentLink {
	return ParentLink{Target: p.Ref(), Ref: p, Via: b.via(p.Ref()), Attachment: AttachmentUnwatched}
}

// via returns the Gateway of an owned ListenerSet target.
func (b *builder) via(target objects.Ref) *objects.Ref {
	return b.lsGW[target]
}

// lookup finds a route of a served kind in the snapshot by ref.
func (b *builder) lookup(ref objects.Ref) (kinds.Served, *unstructured.Unstructured, bool) {
	var k kinds.Served
	found := false
	for _, s := range kinds.ByCategory(b.in.Served, kinds.CategoryRoute) {
		if s.Group == ref.Group && s.Kind == ref.Kind {
			k, found = s, true
			break
		}
	}
	if !found || b.in.Snapshot == nil {
		return k, nil, false
	}
	if b.byRef == nil {
		b.byRef = map[objects.Ref]*unstructured.Unstructured{}
		for _, s := range kinds.ByCategory(b.in.Served, kinds.CategoryRoute) {
			for _, u := range b.in.Snapshot.Objects[s.GVR()] {
				b.byRef[objects.RefOf(u)] = u
			}
		}
	}
	u, ok := b.byRef[ref]
	return k, u, ok
}

// fromStatus decides the attachment of d to parent p from the status entry
// of the install's controllers, current entries only (Phase 1 freshness),
// and adds the row's flags (design §6.1).
func (b *builder) fromStatus(d *draft, p objects.ParentRef) ParentLink {
	l := ParentLink{Target: p.Ref(), Ref: p, Via: b.via(p.Ref())}
	var st *objects.RouteParentStatus
	for i := range d.status {
		if d.status[i].ParentRef == p && slices.Contains(b.in.Install.ControllerNames, d.status[i].ControllerName) {
			st = &d.status[i]
			break
		}
	}
	if st == nil {
		l.Attachment, l.Pending = AttachmentPending, pendingNoStatus
		d.row.Flags |= FlagPending
		return l
	}
	if why := health.RoutePending(d.row.Obj, st.Conditions); why != "" {
		l.Attachment, l.Pending = AttachmentPending, why
		d.row.Flags |= FlagPending
		return l
	}
	// Current: Accepted and ResolvedRefs are present and neither is Unknown.
	cs := st.Conditions
	acc := objects.FindCondition(cs, "Accepted")
	if acc.Status == metav1.ConditionTrue {
		l.Attachment = AttachmentAccepted
		d.row.Flags |= FlagAccepted
	} else {
		l.Attachment = AttachmentNotAccepted
		d.row.Flags |= FlagNotAccepted
		if acc.Reason == "RouteRuleReplaced" || acc.Reason == "RouteRuleDropped" {
			d.row.Flags |= FlagNotProgrammed // 2.3 reports these through Accepted
		}
	}
	if c := objects.FindCondition(cs, "ResolvedRefs"); c.Status == metav1.ConditionFalse {
		d.row.Flags |= FlagRefsUnresolved
	}
	if c := objects.FindCondition(cs, programmedType); c != nil && c.Status == metav1.ConditionFalse {
		d.row.Flags |= FlagNotProgrammed
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

// sorted returns the rows in (namespace, name, group, kind) order. Parents
// follow spec order, then parentRef order; reported parent routes are sorted.
func (b *builder) sorted() []RouteRow {
	slices.SortFunc(b.drafts, func(x, y *draft) int { return compareRefs(x.row.Ref, y.row.Ref) })
	rows := make([]RouteRow, len(b.drafts))
	for i, d := range b.drafts {
		r := &d.row
		specIdx := func(p objects.ParentRef) int {
			if j := slices.Index(d.specRefs, p); j >= 0 {
				return j
			}
			return len(d.specRefs)
		}
		slices.SortStableFunc(r.Parents, func(x, y ParentLink) int {
			return cmp.Or(cmp.Compare(specIdx(x.Ref), specIdx(y.Ref)), compareParentRefs(x.Ref, y.Ref), compareRefs(x.Target, y.Target))
		})
		slices.SortFunc(d.edges, compareRefs)
		r.ParentRoutes = d.edges
		r.State = b.state(r.Ref)
		r.search = search(r.Ref, r.Hostnames)
		rows[i] = *r
	}
	return rows
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

// index fills the route indexes from the sorted rows; b.drafts is in row order.
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
		b.drafts[i].targets = b.targets(r)
		for _, t := range b.drafts[i].targets {
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

// targets returns r's backendRefs and mirror targets; delegation refs are excluded.
func (b *builder) targets(r *RouteRow) []objects.BackendTarget {
	if r.View != nil {
		return objects.BackendTargets(r.View)
	}
	return objects.RouteBackendTargets(r.Obj)
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
