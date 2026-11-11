// Package paths builds the path model of Phase 6 (design §4): one row per way
// traffic can flow from a Gateway listener through a route rule to a backend.
// Topology is derived from one snapshot generation and is immutable once built.
package paths

import (
	"encoding/binary"
	"slices"
	"unsafe"

	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// Action is what a row's rule does with a request, as written.
type Action uint8

// Actions. A rule with no backendRef has its written action; no intent is inferred.
const (
	ActionBackend Action = iota
	ActionRedirect
	ActionDirectResponse
	ActionNone
)

// Listener is the listener a row's attachment is on.
type Listener struct {
	Name, Protocol string
	// Port is 0 when unset.
	Port int32
}

// Row is one path: an attachment, a rule of the last route of its chain, and
// one backendRef or mirror of that rule. It holds indexes into its Topology.
type Row struct {
	// ctx is the row's path context: its chain and delegation state.
	att, ctx int32
	rule     int16
	// ref is the backendRef index in the rule; -1 for a rule-level mirror or a rule with no backendRef.
	ref    int16
	depOff uint32
	depN   uint16
	// mirror is the RequestMirror filter index on the ref, or on the rule when ref is -1; -1 for none.
	mirror int16
}

// Input is everything Build reads for one install. A nil Index, Views or
// Decode builds an empty topology.
type Input struct {
	// Index locates the attached routes, Gateways and ListenerSets.
	Index *explore.Index
	// Views holds view A; Delegation is set only with a Delegator.
	Views *computed.Views
	// Decode decodes HTTPRoute rules.
	Decode objects.Decoder
	// Delegates is true when the selection has a Delegator.
	Delegates bool
}

// attachment is one view A member with its resources resolved.
type attachment struct {
	resource, gateway int32 // Refs indexes
	listener          int32 // listeners index
	hostnames         int32 // hostnames index
}

// route is a route of some chain with its rules. HTTPRoute rules are the
// decoder's shared view; other kinds are decoded for the topology.
type route struct {
	ref   int32 // Refs index
	rules []objects.Rule
}

// Topology is the path rows of one install for one snapshot generation. It
// and everything its accessors return are read-only.
type Topology struct {
	Rows []Row
	// Refs is the dependency table; a row's Deps index it.
	Refs []objects.Ref

	atts      []attachment
	listeners []Listener
	// hostnames are the distinct hostname lists of the attachments.
	hostnames [][]string
	routes    []route
	// hops are the routes indexes of every chain, root first, back to back.
	// A row's rules are its chain's last route's.
	hops []int32
	// ctxs are the rows' path contexts; unknowns the reasons they index.
	ctxs     []pathCtx
	unknowns []computed.UnknownReason
	deps     []int32
	// delegates is Input.Delegates: whether a delegation backendRef delegates.
	delegates bool
}

// builder holds Build's lookup tables, which the Topology does not retain.
type builder struct {
	in     Input
	t      *Topology
	refs   map[objects.Ref]int32
	routes map[objects.Ref]int32 // route ref → its routes index
	// listeners, sets and chains intern listeners, dependency sets and chains (hops offsets).
	listeners map[listenerKey]int32
	hosts     map[string]int32
	sets      map[string]uint32
	chains    map[string]uint32
	set       []int32 // scratch dependency set and its key
	key       []byte
	// roots are the attachments of each route, and rootCtx its root path context.
	roots   map[objects.Ref][]rootAtt
	rootCtx map[int32]int32
	// states interns delegation rows' path contexts (delegation.go).
	states map[stateKey]int32
	base   []int32 // scratch attachment deps of a delegated row
}

// rootAtt is an attachment with its dependency set.
type rootAtt struct {
	att  int32
	base []int32
}

type listenerKey struct {
	resource int32
	name     string
}

// Build derives the topology of in: one row per (view A attachment, rule,
// backendRef), one per rule with no backendRef, and one per RequestMirror,
// then the same per view C context for delegated routes.
func Build(in Input) *Topology {
	t := &Topology{delegates: in.Delegates}
	if in.Index == nil || in.Views == nil || in.Decode == nil {
		return t
	}
	b := &builder{in: in, t: t, refs: map[objects.Ref]int32{}, routes: map[objects.Ref]int32{},
		listeners: map[listenerKey]int32{}, hosts: map[string]int32{}, sets: map[string]uint32{}, chains: map[string]uint32{},
		roots: map[objects.Ref][]rootAtt{}, rootCtx: map[int32]int32{}, states: map[stateKey]int32{}}
	for i := range in.Views.Attachment.Members {
		b.member(&in.Views.Attachment.Members[i])
	}
	b.delegated()
	// The memo retains the tables, so they are copied to their length.
	t.Rows, t.Refs, t.deps = slices.Clone(t.Rows), slices.Clone(t.Refs), slices.Clone(t.deps)
	t.hostnames = slices.Clone(t.hostnames)
	t.atts, t.listeners, t.hops, t.ctxs = slices.Clone(t.atts), slices.Clone(t.listeners), slices.Clone(t.hops), slices.Clone(t.ctxs)
	return t
}

// ref interns r into the dependency table.
func (b *builder) ref(r objects.Ref) int32 {
	if i, ok := b.refs[r]; ok {
		return i
	}
	i := int32(len(b.t.Refs)) // #nosec G115 -- a ref count fits in int32
	b.t.Refs = append(b.t.Refs, r)
	b.refs[r] = i
	return i
}

// member adds the rows of one view A member.
func (b *builder) member(m *computed.Member) {
	ri, ok := b.route(m.Route)
	if !ok {
		return
	}
	ctx, ok := b.rootCtx[ri]
	if !ok {
		ctx = b.ctx(pathCtx{off: b.chain([]int32{ri}), n: 1})
		b.rootCtx[ri] = ctx
	}
	a := attachment{resource: b.ref(m.Resource), hostnames: b.hostnames(m.Hostnames)}
	a.gateway = a.resource
	var ls []objects.Listener
	if id, ok := b.in.Index.FindGateway(m.Resource); ok && b.in.Index.Gateways[id].View != nil {
		ls = b.in.Index.Gateways[id].View.Listeners
	} else if id, ok := b.in.Index.FindListenerSet(m.Resource); ok && b.in.Index.ListenerSets[id].View != nil {
		row := &b.in.Index.ListenerSets[id]
		ls, a.gateway = row.View.Listeners, b.ref(row.Parent)
	}
	a.listener = b.listener(a.resource, m.Listener, ls)
	att := int32(len(b.t.atts)) // #nosec G115 -- an attachment count fits in int32
	b.t.atts = append(b.t.atts, a)

	base := make([]int32, 0, len(m.Deps)+1)
	for _, d := range m.Deps {
		base = append(base, b.ref(d))
	}
	base = append(base, a.gateway)
	b.roots[m.Route] = append(b.roots[m.Route], rootAtt{att: att, base: base})
	rules := b.t.routes[ri].rules
	for i := range rules {
		b.rule(att, ctx, i, &rules[i], base)
	}
}

// hostnames interns the hostname list h; nil and empty stay apart.
func (b *builder) hostnames(h []string) int32 {
	b.key = b.key[:0]
	if h != nil {
		b.key = append(b.key, 1)
	}
	for _, s := range h {
		b.key = append(append(b.key, s...), 0)
	}
	if i, ok := b.hosts[string(b.key)]; ok {
		return i
	}
	i := int32(len(b.t.hostnames)) // #nosec G115 -- a hostname list count fits in int32
	b.t.hostnames = append(b.t.hostnames, h)
	b.hosts[string(b.key)] = i
	return i
}

// listener interns the listener name of resource, whose spec listeners are ls.
func (b *builder) listener(resource int32, name string, ls []objects.Listener) int32 {
	k := listenerKey{resource, name}
	if i, ok := b.listeners[k]; ok {
		return i
	}
	l := Listener{Name: name}
	if i := slices.IndexFunc(ls, func(l objects.Listener) bool { return l.Name == name }); i >= 0 {
		l.Protocol, l.Port = ls[i].Protocol, ls[i].Port
	}
	i := int32(len(b.t.listeners)) // #nosec G115 -- a listener count fits in int32
	b.t.listeners = append(b.t.listeners, l)
	b.listeners[k] = i
	return i
}

// route returns the routes index of ref, decoding its rules once.
func (b *builder) route(ref objects.Ref) (int32, bool) {
	if i, ok := b.routes[ref]; ok {
		return i, true
	}
	id, ok := b.in.Index.Row(ref)
	if !ok {
		return 0, false
	}
	row := &b.in.Index.Routes[id]
	rt := route{ref: b.ref(ref)}
	if ref.Group == objects.GatewayAPIGroup && ref.Kind == "HTTPRoute" {
		rt.rules = b.in.Decode.HTTPRoute(row.Obj).Rules
	} else {
		rt.rules = objects.DecodeRouteRules(row.Obj)
	}
	ri := int32(len(b.t.routes)) // #nosec G115 -- a route count fits in int32
	b.t.routes = append(b.t.routes, rt)
	b.routes[ref] = ri
	return ri, true
}

// chain interns the chain of routes indexes rs, root first, and returns its hops offset.
func (b *builder) chain(rs []int32) uint32 {
	b.key = b.key[:0]
	for _, r := range rs {
		b.key = binary.LittleEndian.AppendUint32(b.key, uint32(r)) // #nosec G115 -- a route index is never negative
	}
	if off, ok := b.chains[string(b.key)]; ok {
		return off
	}
	off := uint32(len(b.t.hops)) // #nosec G115 -- the table fits in uint32
	b.t.hops = append(b.t.hops, rs...)
	b.chains[string(b.key)] = off
	return off
}

// ctx appends a path context and returns its index.
func (b *builder) ctx(p pathCtx) int32 {
	b.t.ctxs = append(b.t.ctxs, p)
	return int32(len(b.t.ctxs) - 1) // #nosec G115 -- a context count fits in int32
}

// rule adds the rows of rule ri in the path context ctx: one per backendRef,
// or one with the written action, then one per RequestMirror. base is the
// attachment's deps.
func (b *builder) rule(att, ctx int32, ri int, r *objects.Rule, base []int32) {
	rule := int16(ri) // #nosec G115 -- a route has at most 16 rules
	if len(r.BackendRefs) == 0 {
		b.add(Row{att: att, ctx: ctx, rule: rule, ref: -1, mirror: -1}, base)
	}
	for i := range r.BackendRefs {
		br := &r.BackendRefs[i]
		ref := int16(i) // #nosec G115 -- a rule has at most 16 backendRefs
		if br.Delegation != nil && b.in.Delegates {
			// A delegation is one row whose backend is the delegation, with its view C
			// rows' state; delegated() adds the children's rows. Name mode names the
			// child, so it is a dependency; the other modes reveal only the parent's spec.
			row := Row{att: att, ctx: b.refState(ctx, r.Index, i), rule: rule, ref: ref, mirror: -1}
			if br.Delegation.Mode == objects.DelegationName {
				b.add(row, base, b.ref(br.Ref))
			} else {
				b.add(row, base)
			}
			continue
		}
		b.add(Row{att: att, ctx: ctx, rule: rule, ref: ref, mirror: -1}, base, b.ref(br.Ref))
		b.mirrors(Row{att: att, ctx: ctx, rule: rule, ref: ref}, br.Filters, base)
	}
	b.mirrors(Row{att: att, ctx: ctx, rule: rule, ref: -1}, r.Filters, base)
}

// mirrors adds one row per RequestMirror filter in fs.
func (b *builder) mirrors(row Row, fs []objects.Filter, base []int32) {
	for i := range fs {
		if fs[i].Mirror == nil {
			continue
		}
		row.mirror = int16(i) // #nosec G115 -- a filter index fits in int16
		b.add(row, base, b.ref(fs[i].Mirror.Backend))
	}
}

// add appends row with the dependency set base ∪ extra, sorted. Equal sets,
// such as one route's on each listener of a Gateway, share their storage.
func (b *builder) add(row Row, base []int32, extra ...int32) {
	b.set = append(append(b.set[:0], base...), extra...)
	slices.Sort(b.set)
	b.set = slices.Compact(b.set)
	t := b.t
	row.depN = uint16(len(b.set)) // #nosec G115 -- a dependency set is a handful of refs
	b.key = b.key[:0]
	for _, d := range b.set {
		b.key = binary.LittleEndian.AppendUint32(b.key, uint32(d)) // #nosec G115 -- a ref index is never negative
	}
	off, ok := b.sets[string(b.key)]
	if !ok {
		off = uint32(len(t.deps)) // #nosec G115 -- the table fits in uint32
		t.deps = append(t.deps, b.set...)
		b.sets[string(b.key)] = off
	}
	row.depOff = off
	t.Rows = append(t.Rows, row)
}

// writtenAction is the action of a rule with no backendRef, from its filters as written.
func writtenAction(fs []objects.Filter) Action {
	for _, f := range fs {
		switch {
		case f.Type == "RequestRedirect":
			return ActionRedirect
		case f.ExtensionRef != nil && f.ExtensionRef.Group == kinds.KgatewayGroup && f.ExtensionRef.Kind == "DirectResponse":
			return ActionDirectResponse
		}
	}
	return ActionNone
}

// Gateway is the row's Gateway; a ListenerSet attachment projects to its parent.
func (t *Topology) Gateway(r Row) objects.Ref { return t.Refs[t.atts[r.att].gateway] }

// ListenerSet is the row's ListenerSet, or nil for a Gateway attachment.
func (t *Topology) ListenerSet(r Row) *objects.Ref {
	a := &t.atts[r.att]
	if a.resource == a.gateway {
		return nil
	}
	ls := t.Refs[a.resource]
	return &ls
}

// Listener is the row's listener.
func (t *Topology) Listener(r Row) Listener { return t.listeners[t.atts[r.att].listener] }

// Hostnames are the attachment's effective hostnames; nil for kinds without hostnames.
func (t *Topology) Hostnames(r Row) []string { return t.hostnames[t.atts[r.att].hostnames] }

// chain is the routes indexes of the row's chain, root first.
func (t *Topology) chain(r Row) []int32 {
	p := &t.ctxs[r.ctx]
	return t.hops[p.off : p.off+uint32(p.n)]
}

// Chain is the row's route chain, root first.
func (t *Topology) Chain(r Row) []objects.Ref {
	c := t.chain(r)
	out := make([]objects.Ref, len(c))
	for i, ri := range c {
		out[i] = t.Refs[t.routes[ri].ref]
	}
	return out
}

// Rule is the row's rule in the last route of its chain.
func (t *Topology) Rule(r Row) *objects.Rule {
	c := t.chain(r)
	return &t.routes[c[len(c)-1]].rules[r.rule]
}

// Backend is the row's backendRef; false for a mirror row or a rule with no
// backendRef. Without a Delegator, a ref to an HTTPRoute is ordinary.
func (t *Topology) Backend(r Row) (objects.BackendRef, bool) {
	if r.ref < 0 || r.mirror >= 0 {
		return objects.BackendRef{}, false
	}
	b := t.Rule(r).BackendRefs[r.ref]
	if !t.delegates {
		b.Delegation = nil
	}
	return b, true
}

// Mirror is the row's RequestMirror filter; false unless it is a mirror row.
func (t *Topology) Mirror(r Row) (objects.MirrorFilter, bool) {
	if r.mirror < 0 {
		return objects.MirrorFilter{}, false
	}
	rule := t.Rule(r)
	fs := rule.Filters
	if r.ref >= 0 {
		fs = rule.BackendRefs[r.ref].Filters
	}
	return *fs[r.mirror].Mirror, true
}

// Action is what the row's rule does: ActionBackend for a backendRef or a
// mirror, else the rule's written action.
func (t *Topology) Action(r Row) Action {
	if r.ref >= 0 || r.mirror >= 0 {
		return ActionBackend
	}
	return writtenAction(t.Rule(r).Filters)
}

// Deps is the row's dependency set: its attachment's, the Gateway, every route
// of its chain, its backend or mirror target and, on a delegated route, its
// view C row's, as indexes into Refs.
func (t *Topology) Deps(r Row) explore.DepSet {
	return explore.DepSet(t.deps[r.depOff : r.depOff+uint32(r.depN) : r.depOff+uint32(r.depN)])
}

// Bytes estimates the topology's retained size, for the memo cap (design §14).
// HTTPRoute rules and view C matches are shared, not counted.
func (t *Topology) Bytes() int {
	n := int(unsafe.Sizeof(Topology{})) + cap(t.Rows)*int(unsafe.Sizeof(Row{})) + cap(t.deps)*4
	n += cap(t.Refs) * int(unsafe.Sizeof(objects.Ref{}))
	for _, r := range t.Refs {
		n += len(r.Group) + len(r.Kind) + len(r.Namespace) + len(r.Name)
	}
	n += cap(t.atts)*int(unsafe.Sizeof(attachment{})) + cap(t.listeners)*int(unsafe.Sizeof(Listener{}))
	n += cap(t.hostnames) * int(unsafe.Sizeof([]string{}))
	for _, l := range t.listeners {
		n += len(l.Name) + len(l.Protocol)
	}
	n += cap(t.routes) * int(unsafe.Sizeof(route{}))
	for i := range t.routes {
		if k := t.Refs[t.routes[i].ref]; k.Group == objects.GatewayAPIGroup && k.Kind == "HTTPRoute" {
			continue
		}
		n += ruleBytes(t.routes[i].rules)
	}
	n += cap(t.hops)*4 + cap(t.ctxs)*int(unsafe.Sizeof(pathCtx{})) + cap(t.unknowns)*int(unsafe.Sizeof(computed.UnknownReason("")))
	return n
}

// ruleBytes estimates the retained size of decoded rules, filters as written excluded.
func ruleBytes(rules []objects.Rule) int {
	n := cap(rules) * int(unsafe.Sizeof(objects.Rule{}))
	for i := range rules {
		r := &rules[i]
		n += cap(r.GRPCMatches)*int(unsafe.Sizeof(objects.GRPCMatch{})) + cap(r.Filters)*int(unsafe.Sizeof(objects.Filter{}))
		n += cap(r.BackendRefs) * int(unsafe.Sizeof(objects.BackendRef{}))
		for _, b := range r.BackendRefs {
			n += len(b.Ref.Group) + len(b.Ref.Kind) + len(b.Ref.Namespace) + len(b.Ref.Name)
		}
	}
	return n
}
