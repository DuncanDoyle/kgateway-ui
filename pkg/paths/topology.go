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
	att, chain, rule int32
	// ref is the backendRef index in the rule; -1 for a rule-level mirror or a rule with no backendRef.
	ref    int32
	depOff uint32
	depN   uint16
	// mirror is the RequestMirror filter index on the ref, or on the rule when ref is -1; -1 for none.
	mirror int16
	action Action
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
	hostnames         []string
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
	routes    []route
	// chains are route indexes, root first; a row's rules are its chain's last route's.
	chains [][]int32
	deps   []int32
	// delegates is Input.Delegates: whether a delegation backendRef delegates.
	delegates bool
}

// builder holds Build's lookup tables, which the Topology does not retain.
type builder struct {
	in     Input
	t      *Topology
	refs   map[objects.Ref]int32
	routes map[objects.Ref]int32 // route ref → its root chain index
	// listeners and sets intern listeners and dependency sets.
	listeners map[listenerKey]int32
	sets      map[string]uint32
	set       []int32 // scratch dependency set and its key
	key       []byte
}

type listenerKey struct {
	resource int32
	name     string
}

// Build derives the topology of in: one row per (view A attachment, rule,
// backendRef), one per rule with no backendRef, and one per RequestMirror.
func Build(in Input) *Topology {
	t := &Topology{delegates: in.Delegates}
	if in.Index == nil || in.Views == nil || in.Decode == nil {
		return t
	}
	b := &builder{in: in, t: t, refs: map[objects.Ref]int32{}, routes: map[objects.Ref]int32{},
		listeners: map[listenerKey]int32{}, sets: map[string]uint32{}}
	for i := range in.Views.Attachment.Members {
		b.member(&in.Views.Attachment.Members[i])
	}
	// The memo retains the tables, so they are copied to their length.
	t.Rows, t.Refs, t.deps = slices.Clone(t.Rows), slices.Clone(t.Refs), slices.Clone(t.deps)
	t.atts, t.listeners = slices.Clone(t.atts), slices.Clone(t.listeners)
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
	chain, ok := b.chain(m.Route)
	if !ok {
		return
	}
	a := attachment{resource: b.ref(m.Resource), hostnames: m.Hostnames}
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
	rules := b.t.routes[b.t.chains[chain][0]].rules
	for ri := range rules {
		b.rule(att, chain, int32(ri), &rules[ri], base) // #nosec G115 -- a rule index fits in int32
	}
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

// chain returns the root chain of route ref, decoding its rules once.
func (b *builder) chain(ref objects.Ref) (int32, bool) {
	if c, ok := b.routes[ref]; ok {
		return c, true
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
	c := int32(len(b.t.chains)) // #nosec G115 -- a chain count fits in int32
	b.t.chains = append(b.t.chains, []int32{ri})
	b.routes[ref] = c
	return c, true
}

// rule adds the rows of one rule: one per backendRef, or one with the
// written action, then one per RequestMirror. base is the attachment's deps.
func (b *builder) rule(att, chain, ri int32, r *objects.Rule, base []int32) {
	if len(r.BackendRefs) == 0 {
		b.add(Row{att: att, chain: chain, rule: ri, ref: -1, mirror: -1, action: writtenAction(r.Filters)}, base)
	}
	for i := range r.BackendRefs {
		br := &r.BackendRefs[i]
		ref := int32(i) // #nosec G115 -- a backendRef index fits in int32
		if br.Delegation != nil && b.in.Delegates {
			// A delegation is one row whose backend is the delegation; view C's
			// rows for the children it selects belong after it. Name mode
			// names the child, so it is a dependency; the other modes reveal only the parent's spec.
			if br.Delegation.Mode == objects.DelegationName {
				b.add(Row{att: att, chain: chain, rule: ri, ref: ref, mirror: -1}, base, b.ref(br.Ref))
			} else {
				b.add(Row{att: att, chain: chain, rule: ri, ref: ref, mirror: -1}, base)
			}
			continue
		}
		b.add(Row{att: att, chain: chain, rule: ri, ref: ref, mirror: -1}, base, b.ref(br.Ref))
		b.mirrors(Row{att: att, chain: chain, rule: ri, ref: ref}, br.Filters, base)
	}
	b.mirrors(Row{att: att, chain: chain, rule: ri, ref: -1}, r.Filters, base)
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
func (t *Topology) Hostnames(r Row) []string { return t.atts[r.att].hostnames }

// Chain is the row's route chain, root first.
func (t *Topology) Chain(r Row) []objects.Ref {
	c := t.chains[r.chain]
	out := make([]objects.Ref, len(c))
	for i, ri := range c {
		out[i] = t.Refs[t.routes[ri].ref]
	}
	return out
}

// Rule is the row's rule in the last route of its chain.
func (t *Topology) Rule(r Row) *objects.Rule {
	c := t.chains[r.chain]
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
func (t *Topology) Action(r Row) Action { return r.action }

// Deps is the row's dependency set: its attachment's, the Gateway, every route
// of its chain and its backend or mirror target, as indexes into Refs.
func (t *Topology) Deps(r Row) explore.DepSet {
	return explore.DepSet(t.deps[r.depOff : r.depOff+uint32(r.depN) : r.depOff+uint32(r.depN)])
}

// Bytes estimates the topology's retained size, for the memo cap (design §14).
// HTTPRoute rules are the decoder's and are not counted.
func (t *Topology) Bytes() int {
	n := int(unsafe.Sizeof(Topology{})) + cap(t.Rows)*int(unsafe.Sizeof(Row{})) + cap(t.deps)*4
	n += cap(t.Refs) * int(unsafe.Sizeof(objects.Ref{}))
	for _, r := range t.Refs {
		n += len(r.Group) + len(r.Kind) + len(r.Namespace) + len(r.Name)
	}
	n += cap(t.atts)*int(unsafe.Sizeof(attachment{})) + cap(t.listeners)*int(unsafe.Sizeof(Listener{}))
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
	n += cap(t.chains) * int(unsafe.Sizeof([]int32{}))
	for _, c := range t.chains {
		n += cap(c) * 4
	}
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
