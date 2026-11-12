package paths

import (
	"cmp"
	"fmt"
	"slices"
	"time"
	"unsafe"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/explore"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

// Signal kinds: where a problem's signal comes from (design §4.5).
const (
	SignalGateway     = "gateway"
	SignalListener    = "listener"
	SignalRouteStatus = "route-status"
	SignalWaiting     = "waiting"
	SignalFinding     = "finding"
	SignalDelegation  = "delegation"
	SignalCoverage    = "coverage"
)

// Endpoint completeness (design §9.2). Only a complete read establishes zero.
const (
	EndpointsComplete      = "complete"
	EndpointsFailed        = "failed"
	EndpointsUnavailable   = "unavailable"
	EndpointsNotApplicable = "not-applicable"
)

// EndpointsOn is the endpoints capability state in which counts are read.
const EndpointsOn = "on"

// Signal is one existing signal on a row's chain. Code is a finding code, a
// condition type, a capability state or a view C reason; Ref is its object.
// Listener names the listener of Ref a listener's signal is about; "" otherwise.
type Signal struct {
	Kind     string
	Code     string
	Ref      objects.Ref
	State    health.State
	Listener string
}

// Problem is a row's worst signal state and the signals that are not healthy.
type Problem struct {
	State   health.State
	Signals []Signal
}

// Endpoints is a row's backend port endpoint summary. Ready and Total are set
// only for a complete read.
type Endpoints struct {
	Complete     string
	Ready, Total *int
	ReadAt       time.Time
	Port         int32
	PortName     string
}

// ProjectInput is what Project reads besides the topology.
type ProjectInput struct {
	Model *engine.InstallModel
	// Next is engine.Model.Next of the publication that Model is part of.
	Next     engine.Deadline
	Snapshot *cache.Snapshot
	Now      time.Time
	// EndpointsCap is the endpoints capability state: on, off, denied or failing.
	EndpointsCap string
}

// Projection is the health of a topology's rows for one model publication.
// Problems, Shares and Endpoints are index-aligned with Topology.Rows.
type Projection struct {
	Problems  []Problem
	Shares    []Share
	Endpoints []*Endpoints
	// Order are the row indexes, problem first, then Gateway, listener, route, rule and RowID.
	Order []int32
	// Next is ProjectInput.Next: the projection is not reused once it is due.
	Next engine.Deadline
	// digests are each row's content digest, made once per projection for Digest.
	digests []uint64
}

// gatewayCodes are the Gateway-scoped findings that describe its proxies.
var gatewayCodes = map[string]bool{health.CodeXDSRejected: true, health.CodeProxyNotReady: true, health.CodeConfigWithheld: true,
	health.CodeProxyConfigWarming: true, health.CodeProxyDisconnected: true}

// stateRank orders states for a problem: error, warning, unknown, healthy.
func stateRank(s health.State) int {
	switch s {
	case health.StateError:
		return 0
	case health.StateWarning:
		return 1
	case health.StateUnknown:
		return 2
	}
	return 3
}

// sevState is a finding severity's state; info is healthy.
func sevState(s health.Severity) health.State {
	switch s {
	case health.SeverityError:
		return health.StateError
	case health.SeverityWarning:
		return health.StateWarning
	}
	return health.StateHealthy
}

// attSignals are an attachment's Gateway, listener and parent status signals.
type attSignals struct {
	sigs []Signal
	// refsFalse is set when the parent status has a current ResolvedRefs=False.
	refsFalse bool
	done      bool
}

// refFinding is a KGWUI-013 or 014 on a route: its code, candidate paths and attribution.
type refFinding struct {
	code       string
	paths      []string
	attributed bool
}

type portKey struct {
	ns, name string
	port     int32
}

// portHealth is a Service port's shared summary, its waiting or coverage
// signal, and its active KGWUI-023 findings.
type portHealth struct {
	ep    *Endpoints
	sig   *Signal
	found []Signal
}

type projector struct {
	t        *Topology
	in       ProjectInput
	findings map[objects.Ref][]*health.Finding
	atts     []attSignals
	ctxs     map[int32][]Signal
	edges    map[[2]objects.Ref]*Signal
	refs     map[objects.Ref][]refFinding
	ports    map[portKey]*portHealth
	facts    facts
	backends map[objects.Ref]*unstructured.Unstructured
	shares   map[*objects.Rule][]Share
	// sets interns the rows' signal sets by content; key is its scratch key.
	sets map[string][]Signal
	key  []byte
	// na are the shared not-applicable summaries of Backend refs, per port.
	na map[int32]*Endpoints
	// entries are entryChecks' table; nil until first used.
	entries map[[2]objects.Ref][]computed.Class
	// setDigests are the interned signal sets' digests, by their first signal.
	setDigests map[*Signal]uint64
}

// Project maps each row of t to its problem: the worst existing signal along
// its whole chain (design §4.5). It adds no checks. in.Model must be set.
func Project(t *Topology, in ProjectInput) *Projection {
	p := &projector{t: t, in: in, findings: map[objects.Ref][]*health.Finding{}, atts: make([]attSignals, len(t.atts)),
		ctxs: map[int32][]Signal{}, edges: map[[2]objects.Ref]*Signal{}, refs: map[objects.Ref][]refFinding{}, ports: map[portKey]*portHealth{},
		backends: map[objects.Ref]*unstructured.Unstructured{}, shares: map[*objects.Rule][]Share{}, sets: map[string][]Signal{}, na: map[int32]*Endpoints{},
		setDigests: map[*Signal]uint64{}}
	p.facts.snap = in.Snapshot
	for i := range in.Model.Health.Findings {
		f := &in.Model.Health.Findings[i]
		if f.Resource != nil {
			p.findings[*f.Resource] = append(p.findings[*f.Resource], f)
		}
	}
	if in.Model.Owned != nil {
		for _, b := range in.Model.Owned.Backends {
			p.backends[objects.RefOf(b)] = b
		}
	}
	out := &Projection{Problems: make([]Problem, len(t.Rows)), Shares: make([]Share, len(t.Rows)), Endpoints: make([]*Endpoints, len(t.Rows)),
		Order: make([]int32, len(t.Rows)), Next: in.Next, digests: make([]uint64, len(t.Rows))}
	// ids are the rows' RowID hashes, made once for the digests and the order's ties.
	ids := make([]uint64, len(t.Rows))
	var sigs []Signal
	for i, r := range t.Rows {
		ids[i] = t.rowHash(r)
		sigs = p.row(sigs[:0], r, out, i)
		pr := Problem{State: health.StateHealthy}
		for _, s := range sigs {
			if stateRank(s.State) < stateRank(pr.State) {
				pr.State = s.State
			}
		}
		if len(sigs) > 0 {
			pr.Signals = p.intern(sigs)
		}
		out.Problems[i] = pr
		out.digests[i] = p.digest(r, ids[i], out, i)
		out.Order[i] = int32(i) // #nosec G115 -- a row count fits in int32
	}
	// Ties fall to the rule as written, then RowID, which a cursor anchor holds (compareAnchor).
	slices.SortFunc(out.Order, func(a, b int32) int {
		ra, rb := t.Rows[a], t.Rows[b]
		if c := cmp.Or(cmp.Compare(stateRank(out.Problems[a].State), stateRank(out.Problems[b].State)),
			t.Gateway(ra).Compare(t.Gateway(rb)), cmp.Compare(t.Listener(ra).Name, t.Listener(rb).Name),
			t.compareChains(ra, rb), cmp.Compare(t.Rule(ra).Index, t.Rule(rb).Index), compareSpec(ra.ref, ra.mirror, rb.ref, rb.mirror)); c != 0 {
			return c
		}
		return cmp.Or(cmp.Compare(ids[a], ids[b]), cmp.Compare(ra.dup, rb.dup))
	})
	return out
}

// intern returns the stored signal set equal to sigs, storing a copy on first sight.
func (p *projector) intern(sigs []Signal) []Signal {
	p.key = p.key[:0]
	for _, s := range sigs {
		p.key = append(append(append(p.key, s.Kind...), 0), s.Code...)
		p.key = append(append(append(append(append(p.key, 0), s.Ref.Group...), 0), s.Ref.Kind...), 0)
		p.key = append(append(append(append(append(p.key, s.Ref.Namespace...), 0), s.Ref.Name...), 0), s.State...)
		p.key = append(append(append(p.key, 0), s.Listener...), 1)
	}
	if set, ok := p.sets[string(p.key)]; ok {
		return set
	}
	set := slices.Clip(slices.Clone(sigs))
	p.sets[string(p.key)] = set
	return set
}

// Bytes estimates the projection's retained size: its row tables plus each
// distinct signal set and endpoint summary once. Strings shared with the
// topology and the model are not counted.
func (p *Projection) Bytes() int {
	n := int(unsafe.Sizeof(Projection{})) + cap(p.Problems)*int(unsafe.Sizeof(Problem{})) + cap(p.Shares)*int(unsafe.Sizeof(Share{}))
	n += cap(p.Endpoints)*int(unsafe.Sizeof((*Endpoints)(nil))) + cap(p.Order)*4 + cap(p.digests)*8
	sets := map[*Signal]bool{}
	for _, pr := range p.Problems {
		if len(pr.Signals) > 0 && !sets[&pr.Signals[0]] {
			sets[&pr.Signals[0]] = true
			n += cap(pr.Signals) * int(unsafe.Sizeof(Signal{}))
		}
	}
	eps := map[*Endpoints]bool{}
	for _, e := range p.Endpoints {
		if e != nil && !eps[e] {
			eps[e] = true
			n += int(unsafe.Sizeof(Endpoints{})) + len(e.PortName)
			if e.Ready != nil {
				n += 2 * int(unsafe.Sizeof(0))
			}
		}
	}
	return n
}

// compareChains orders two rows' route chains, root first.
func (t *Topology) compareChains(a, b Row) int {
	ca, cb := t.chain(a), t.chain(b)
	for i := 0; i < len(ca) && i < len(cb); i++ {
		if c := t.Refs[t.routes[ca[i]].ref].Compare(t.Refs[t.routes[cb[i]].ref]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(ca), len(cb))
}

// row appends row i's signals to sigs and sets its share and endpoints in out.
func (p *projector) row(sigs []Signal, r Row, out *Projection, i int) []Signal {
	t := p.t
	chain := t.chain(r)
	routes := make([]objects.Ref, len(chain))
	for k, ri := range chain {
		routes[k] = t.Refs[t.routes[ri].ref]
	}
	att := p.attachment(r.att, routes[0])
	sigs = append(sigs, att.sigs...)
	sigs = append(sigs, p.delegation(r.ctx, routes)...)
	for _, rt := range routes[1:] {
		for _, f := range p.findings[rt] {
			if f.Code == health.CodeRuleReplaced {
				sigs = append(sigs, Signal{Kind: SignalFinding, Code: f.Code, Ref: rt, State: sevState(f.Severity)})
			}
		}
	}
	// ResolvedRefs=False on the attachment is this row's unless a 013 or 014
	// rules it out. A root route's 013 or 014 counts only on a parent whose own
	// status says ResolvedRefs=False (D-014). Known over-report: on a delegated
	// row the root's ResolvedRefs=False is always an error, candidates unread.
	last := routes[len(routes)-1]
	var rfs []refFinding
	if len(routes) > 1 || att.refsFalse {
		rfs = p.refFindings(last)
	}
	path := ""
	if len(rfs) > 0 {
		path = rowPath(t.Rule(r).Index, int(r.ref), int(r.mirror))
	}
	var attributed, candidate bool
	for _, f := range rfs {
		if path != "" && slices.Contains(f.paths, path) {
			candidate, attributed = true, attributed || f.attributed
			sigs = append(sigs, Signal{Kind: SignalFinding, Code: f.code, Ref: last, State: health.StateError})
		}
	}
	if att.refsFalse && (len(rfs) == 0 || len(routes) > 1) {
		sigs = append(sigs, Signal{Kind: SignalRouteStatus, Code: "ResolvedRefs", Ref: routes[0], State: health.StateError})
	}

	out.Shares[i] = Share{State: ShareNotShown}
	if m, ok := t.Mirror(r); ok {
		out.Shares[i] = MirrorShare(&m)
		return p.backend(sigs, m.Backend, m.Port, true, out, i)
	}
	b, ok := t.Backend(r)
	if !ok {
		return sigs
	}
	rule := t.Rule(r)
	ss, ok := p.shares[rule]
	if !ok {
		ss = Shares(rule)
		p.shares[rule] = ss
	}
	out.Shares[i] = ss[r.ref].WithAttribution(attributed, candidate && !attributed)
	if b.Delegation != nil {
		return sigs
	}
	return p.backend(sigs, b.Ref, b.Port, b.Weight > 0, out, i)
}

// rowPath is the spec path of a row's backendRef or mirror target, as KGWUI-013 and 014 name it.
func rowPath(rule, ref, mirror int) string {
	switch {
	case ref >= 0 && mirror >= 0:
		return fmt.Sprintf("spec.rules[%d].backendRefs[%d].filters[%d].requestMirror.backendRef", rule, ref, mirror)
	case ref >= 0:
		return fmt.Sprintf("spec.rules[%d].backendRefs[%d]", rule, ref)
	case mirror >= 0:
		return fmt.Sprintf("spec.rules[%d].filters[%d].requestMirror.backendRef", rule, mirror)
	}
	return ""
}

// refFindings are route's KGWUI-013 and 014 findings.
func (p *projector) refFindings(route objects.Ref) []refFinding {
	if rfs, ok := p.refs[route]; ok {
		return rfs
	}
	var rfs []refFinding
	for _, f := range p.findings[route] {
		ev, ok := f.Facts.(*health.RefsEvidence)
		if !ok || f.Code != health.CodeRefUnresolved && f.Code != health.CodeRefNotPermitted {
			continue
		}
		rf := refFinding{code: f.Code, attributed: ev.Attributed}
		for _, c := range ev.Candidates {
			rf.paths = append(rf.paths, c.Path)
		}
		rfs = append(rfs, rf)
	}
	p.refs[route] = rfs
	return rfs
}

// attachment returns attachment a's signals, root its route.
func (p *projector) attachment(a int32, root objects.Ref) *attSignals {
	s := &p.atts[a]
	if !s.done {
		p.attachmentInto(s, a, root)
	}
	return s
}

// attachmentInto derives attachment a's signals into s, root its route.
func (p *projector) attachmentInto(s *attSignals, a int32, root objects.Ref) {
	s.done = true
	t, idx := p.t, p.in.Model.Index
	at := &t.atts[a]
	gw, res := t.Refs[at.gateway], t.Refs[at.resource]
	var listeners []objects.ListenerStatus
	var resObj *unstructured.Unstructured
	if id, ok := idx.FindGateway(gw); ok && idx.Gateways[id].View != nil {
		row := &idx.Gateways[id]
		s.sigs = conditions(s.sigs, SignalGateway, gw, row.Obj, row.View.Conditions, "Accepted", "Programmed")
		listeners, resObj = row.View.ListenerStatus, row.Obj
	} else {
		s.sigs = append(s.sigs, Signal{Kind: SignalWaiting, Ref: gw, State: health.StateUnknown})
	}
	if res != gw {
		listeners, resObj = nil, nil
		if id, ok := idx.FindListenerSet(res); ok && idx.ListenerSets[id].View != nil {
			row := &idx.ListenerSets[id]
			s.sigs = conditions(s.sigs, SignalGateway, res, row.Obj, row.View.Conditions, "Accepted", "Programmed")
			listeners, resObj = row.View.ListenerStatus, row.Obj
		}
	}
	name := t.listeners[at.listener].Name
	n := len(s.sigs)
	if i := slices.IndexFunc(listeners, func(l objects.ListenerStatus) bool { return l.Name == name }); i >= 0 {
		s.sigs = conditions(s.sigs, SignalListener, res, resObj, listeners[i].Conditions, "Accepted", "Programmed")
		if c := objects.FindCondition(listeners[i].Conditions, "ResolvedRefs"); c != nil && c.Status == metav1.ConditionFalse && !objects.IsStale(resObj, *c) {
			s.sigs = append(s.sigs, Signal{Kind: SignalListener, Code: c.Type, Ref: res, State: health.StateError})
		}
	} else {
		s.sigs = append(s.sigs, Signal{Kind: SignalWaiting, Code: "listener", Ref: res, State: health.StateUnknown})
	}
	for k := n; k < len(s.sigs); k++ {
		s.sigs[k].Listener = name
	}
	for _, f := range p.findings[gw] {
		if st := sevState(f.Severity); gatewayCodes[f.Code] && st != health.StateHealthy {
			s.sigs = append(s.sigs, Signal{Kind: SignalFinding, Code: f.Code, Ref: gw, State: st})
		}
	}
	s.sigs = p.parent(s, root, int(at.parent))
}

// AttachmentProblems is the own problem of each row's attachment, index-aligned
// with rows: its Gateway, listener and route parent status signals as Project
// derives them, a current ResolvedRefs=False included. Nothing is retained.
func AttachmentProblems(t *Topology, in ProjectInput, rows []int32) []Problem {
	p := &projector{t: t, in: in, findings: map[objects.Ref][]*health.Finding{}}
	gws := map[objects.Ref]bool{}
	for _, k := range rows {
		gws[t.Gateway(t.Rows[k])] = true
	}
	for i := range in.Model.Health.Findings {
		if f := &in.Model.Health.Findings[i]; f.Resource != nil && gws[*f.Resource] {
			p.findings[*f.Resource] = append(p.findings[*f.Resource], f)
		}
	}
	out := make([]Problem, len(rows))
	for j, k := range rows {
		r := t.Rows[k]
		var s attSignals
		root := t.Refs[t.routes[t.chain(r)[0]].ref]
		p.attachmentInto(&s, r.att, root)
		sigs := s.sigs
		if s.refsFalse {
			sigs = append(sigs, Signal{Kind: SignalRouteStatus, Code: "ResolvedRefs", Ref: root, State: health.StateError})
		}
		pr := Problem{State: health.StateHealthy, Signals: sigs}
		for _, sg := range sigs {
			if stateRank(sg.State) < stateRank(pr.State) {
				pr.State = sg.State
			}
		}
		out[j] = pr
	}
	return out
}

// parent appends the signals of route's status for spec parentRef i to s.sigs.
func (p *projector) parent(s *attSignals, route objects.Ref, i int) []Signal {
	idx := p.in.Model.Index
	id, ok := idx.Row(route)
	if !ok {
		return append(s.sigs, Signal{Kind: SignalWaiting, Ref: route, State: health.StateUnknown})
	}
	links := idx.Routes[id].Parents
	j := slices.IndexFunc(links, func(l explore.ParentLink) bool { return l.Spec == i || slices.Contains(l.Dups, i) })
	if j < 0 || links[j].Attachment != explore.AttachmentAccepted && links[j].Attachment != explore.AttachmentNotAccepted {
		return append(s.sigs, Signal{Kind: SignalWaiting, Ref: route, State: health.StateUnknown})
	}
	// ResolvedRefs is left to ref attribution; a programming failure beside it is every row's.
	for _, c := range [2]*metav1.Condition{links[j].Cond, links[j].Programming} {
		switch {
		case c == nil:
		case c.Type == "ResolvedRefs":
			s.refsFalse = true
		default:
			s.sigs = append(s.sigs, Signal{Kind: SignalRouteStatus, Code: c.Type, Ref: route, State: health.StateError})
			// The route's KGWUI-015 comes from such a failure on one of its parents.
			for _, f := range p.findings[route] {
				if f.Code == health.CodeRuleReplaced && !slices.ContainsFunc(s.sigs, func(g Signal) bool { return g.Code == f.Code && g.Ref == route }) {
					s.sigs = append(s.sigs, Signal{Kind: SignalFinding, Code: f.Code, Ref: route, State: sevState(f.Severity)})
				}
			}
		}
	}
	return s.sigs
}

// conditions appends kind signals for the condition types of u: waiting when
// missing, pending or older than u's generation, an error when False.
func conditions(sigs []Signal, kind string, ref objects.Ref, u *unstructured.Unstructured, cs []metav1.Condition, types ...string) []Signal {
	for _, ty := range types {
		c := objects.FindCondition(cs, ty)
		switch {
		case c == nil || c.Status == metav1.ConditionUnknown || c.Reason == "Pending" || u != nil && objects.IsStale(u, *c):
			sigs = append(sigs, Signal{Kind: SignalWaiting, Code: ty, Ref: ref, State: health.StateUnknown})
		case c.Status == metav1.ConditionFalse:
			sigs = append(sigs, Signal{Kind: kind, Code: ty, Ref: ref, State: health.StateError})
		}
	}
	return sigs
}

// delegation returns the view C signals of path context ctx on chain routes:
// its own state and every delegation hop's, each unknown at worst.
func (p *projector) delegation(ctx int32, routes []objects.Ref) []Signal {
	if sigs, ok := p.ctxs[ctx]; ok {
		return sigs
	}
	var sigs []Signal
	pc := &p.t.ctxs[ctx]
	var unknown computed.UnknownReason
	if pc.unknown > 0 {
		unknown = p.t.unknowns[pc.unknown-1]
	}
	if code := delegCode(unknown, pc.flags); code != "" {
		sigs = append(sigs, Signal{Kind: SignalDelegation, Code: code, Ref: routes[len(routes)-1], State: health.StateUnknown})
	}
	for k := 1; k < len(routes); k++ {
		if s := p.edge(routes[k-1], routes[k]); s != nil && !slices.Contains(sigs, *s) {
			sigs = append(sigs, *s)
		}
		for _, s := range p.hop(routes[k-1], routes[k], k == len(routes)-1) {
			if !slices.Contains(sigs, s) {
				sigs = append(sigs, s)
			}
		}
	}
	p.ctxs[ctx] = sigs
	return sigs
}

// hop are the signals of child's entry for its parent route. View C's
// child-entry checks decide whether an entry is expected and current: none
// expected (2.3 writes none under the key) gives nothing, missing, stale or
// undecided gives waiting. A current entry's False conditions are errors,
// except the UnsupportedValue a compatible check expects from another
// context's hostname rejection; a programming failure beside it still counts.
// Without a check, the status alone decides.
// The last child's ResolvedRefs=False is left to its 013 and 014 when it has any.
func (p *projector) hop(parent, child objects.Ref, last bool) []Signal {
	waiting := []Signal{{Kind: SignalWaiting, Ref: child, State: health.StateUnknown}}
	classes, checked := p.entryChecks()[[2]objects.Ref{parent, child}]
	expected, tolerate, pending := !checked, false, false
	for _, c := range classes {
		switch c {
		case computed.ClassNotObservable:
		case computed.ClassCompatible:
			expected, tolerate = true, true
		case computed.ClassContradicting:
			expected = true
		default: // missing, stale, unknown, inconclusive, unexpected
			expected, pending = true, true
		}
	}
	idx := p.in.Model.Index
	id, ok := idx.Row(child)
	switch {
	case !expected:
		return nil
	case !ok:
		return waiting
	}
	var sigs []Signal
	found := false
	for _, l := range idx.Routes[id].Parents {
		if l.Target != parent {
			continue
		}
		found = true
		if l.Attachment != explore.AttachmentAccepted && l.Attachment != explore.AttachmentNotAccepted {
			pending = true
			continue
		}
		for _, c := range [2]*metav1.Condition{l.Cond, l.Programming} {
			switch {
			case c == nil, c.Type == "ResolvedRefs" && last && len(p.refFindings(child)) > 0,
				tolerate && c.Type == "Accepted" && c.Reason == reasonUnsupportedValue:
				continue
			}
			sigs = append(sigs, Signal{Kind: SignalRouteStatus, Code: c.Type, Ref: child, State: health.StateError})
		}
	}
	if pending || !found {
		sigs = append(sigs, waiting...)
	}
	return sigs
}

// reasonUnsupportedValue is the Accepted reason of kgateway's hostname stage.
const reasonUnsupportedValue = "UnsupportedValue"

// entryChecks are the classes of view C's child-entry checks per (parent, child), built on first use.
func (p *projector) entryChecks() map[[2]objects.Ref][]computed.Class {
	if p.entries != nil {
		return p.entries
	}
	p.entries = map[[2]objects.Ref][]computed.Class{}
	if v := p.in.Model.Computed; v != nil {
		for i := range v.Checks {
			c := &v.Checks[i]
			if parent, ok := c.Parent(); ok && c.ID.Kind == computed.KindChildEntry {
				k := [2]objects.Ref{parent, c.On}
				p.entries[k] = append(p.entries[k], c.Class)
			}
		}
	}
	return p.entries
}

// edge is the signal of every view C row from parent to child, merged; nil when certain.
func (p *projector) edge(parent, child objects.Ref) *Signal {
	k := [2]objects.Ref{parent, child}
	if s, ok := p.edges[k]; ok {
		return s
	}
	var s *Signal
	if v := p.in.Model.Computed; v != nil {
		rows := v.Delegation.Rows
		i, _ := slices.BinarySearchFunc(rows, parent, func(r computed.Row, t objects.Ref) int { return r.Key.Parent.Compare(t) })
		var flags uint8
		var unknown computed.UnknownReason
		for ; i < len(rows) && rows[i].Key.Parent == parent; i++ {
			if rows[i].Key.Child == child {
				flags |= rowFlags(&rows[i])
				unknown = cmp.Or(unknown, rows[i].Unknown)
			}
		}
		if code := delegCode(unknown, flags); code != "" {
			s = &Signal{Kind: SignalDelegation, Code: code, Ref: child, State: health.StateUnknown}
		}
	}
	p.edges[k] = s
	return s
}

// delegCode names a view C state that is not certain: its unknown reason,
// else unconfirmable, not-computed or truncated; "" when certain.
func delegCode(unknown computed.UnknownReason, flags uint8) string {
	switch {
	case unknown != "":
		return string(unknown)
	case flags&flagUnconfirmable != 0:
		return "unconfirmable"
	case flags&flagNotComputed != 0:
		return "not-computed"
	case flags&flagTruncated != 0:
		return "truncated"
	}
	return ""
}

// backend appends the signals of target ref on port to sigs and sets row i's
// endpoints. traffic is false for a weight-0 ref, whose empty port is no problem.
func (p *projector) backend(sigs []Signal, ref objects.Ref, port int32, traffic bool, out *Projection, i int) []Signal {
	switch {
	case ref.Group == "" && ref.Kind == "Service":
		ph := p.port(portKey{ref.Namespace, ref.Name, port})
		out.Endpoints[i] = ph.ep
		if ph.sig != nil && (traffic || ph.sig.Kind == SignalCoverage) {
			sigs = append(sigs, *ph.sig)
		}
		if traffic {
			sigs = append(sigs, ph.found...)
		}
	case ref.Group == kinds.KgatewayGroup && ref.Kind == "Backend":
		if p.na[port] == nil {
			p.na[port] = &Endpoints{Complete: EndpointsNotApplicable, Port: port}
		}
		out.Endpoints[i] = p.na[port]
		sigs = backendSignals(sigs, p.in.Model, ref, p.backends[ref], p.findings[ref])
	}
	return sigs
}

// backendSignals appends the signals of kgateway Backend ref, whose object is
// b (nil when unknown) and whose findings are found: its KGWUI-002, else the
// rule set judge's verdict: an error when rejected, waiting when it cannot judge.
// KGWUI-002 covers only Backends a route uses, so an unused one relies on the judge.
func backendSignals(sigs []Signal, m *engine.InstallModel, ref objects.Ref, b *unstructured.Unstructured, found []*health.Finding) []Signal {
	n := len(sigs)
	for _, f := range found {
		if f.Code == health.CodeConditionFalse {
			sigs = append(sigs, Signal{Kind: SignalFinding, Code: f.Code, Ref: ref, State: sevState(f.Severity)})
		}
	}
	if j := m.Rules.BackendJudge(); b != nil && j != nil && len(sigs) == n {
		switch v, c := j.JudgeBackend(b); v {
		case computed.BackendRejected:
			code := "Accepted"
			if c != nil {
				code = c.Type
			}
			// The Backend's own condition, as a Gateway's: kind gateway names an object's own status.
			sigs = append(sigs, Signal{Kind: SignalGateway, Code: code, Ref: ref, State: health.StateError})
		case computed.BackendUnknown:
			sigs = append(sigs, Signal{Kind: SignalWaiting, Code: "Accepted", Ref: ref, State: health.StateUnknown})
		}
	}
	return sigs
}

// port returns Service port k's summary, counted as KGWUI-023 counts it.
func (p *projector) port(k portKey) *portHealth {
	if ph, ok := p.ports[k]; ok {
		return ph
	}
	ph := portHealthOf(p.in, &p.facts, k, p.findings[objects.Ref{Kind: "Service", Namespace: k.ns, Name: k.name}])
	p.ports[k] = ph
	return ph
}

// facts finds Service facts and their slices in a snapshot, building its
// tables on first use. need, when set, keeps only the slices of those Services.
type facts struct {
	snap   *cache.Snapshot
	need   func(ns, name string) bool
	svcs   map[string]*cache.ServiceFact
	slices map[string][]*cache.SliceFact
}

// service returns Service ns/name's fact and slices; a nil fact when the cache lacks it.
func (f *facts) service(ns, name string) (*cache.ServiceFact, []*cache.SliceFact) {
	if f.svcs == nil {
		f.svcs, f.slices = map[string]*cache.ServiceFact{}, map[string][]*cache.SliceFact{}
		for i := range f.snap.Services {
			s := &f.snap.Services[i]
			if f.need == nil || f.need(s.Namespace, s.Name) {
				f.svcs[s.Namespace+"/"+s.Name] = s
			}
		}
		for i := range f.snap.EndpointSlices {
			s := &f.snap.EndpointSlices[i]
			if f.need == nil || f.need(s.Namespace, s.Service) {
				f.slices[s.Namespace+"/"+s.Service] = append(f.slices[s.Namespace+"/"+s.Service], s)
			}
		}
	}
	return f.svcs[ns+"/"+name], f.slices[ns+"/"+name]
}

// portHealthOf is Service port k's summary in f, counted as KGWUI-023 counts
// it, with the Service's findings found.
func portHealthOf(in ProjectInput, f *facts, k portKey, found []*health.Finding) *portHealth {
	ph := &portHealth{}
	snap := in.Snapshot
	ref := objects.Ref{Kind: "Service", Namespace: k.ns, Name: k.name}
	coverage := func(complete, code string) *portHealth {
		ph.ep = &Endpoints{Complete: complete, Port: k.port}
		ph.sig = &Signal{Kind: SignalCoverage, Code: code, Ref: ref, State: health.StateUnknown}
		if snap != nil {
			ph.ep.ReadAt = snap.TakenAt
		}
		return ph
	}
	switch {
	case snap == nil:
		return coverage(EndpointsUnavailable, string(health.AvailNotObserved))
	case in.EndpointsCap == string(health.AvailDenied), in.EndpointsCap == string(health.AvailFailing):
		return coverage(EndpointsFailed, in.EndpointsCap)
	case in.EndpointsCap != EndpointsOn:
		return coverage(EndpointsUnavailable, cmp.Or(in.EndpointsCap, string(health.AvailOff)))
	case !health.EndpointsRead(snap, k.ns):
		return coverage(EndpointsUnavailable, string(health.AvailNotObserved))
	}
	svc, slices := f.service(k.ns, k.name)
	if svc != nil && svc.Type == "ExternalName" {
		ph.ep = &Endpoints{Complete: EndpointsNotApplicable, Port: k.port, ReadAt: snap.TakenAt}
		return ph
	}
	// A Service or port the cache lacks is waiting for 013 from the route's status, never healthy.
	ev, ready := health.PortEndpoints(svc, slices, k.port)
	if ev == nil {
		ph.sig = &Signal{Kind: SignalWaiting, Code: "missing", Ref: ref, State: health.StateUnknown}
		return ph
	}
	ph.ep = &Endpoints{Complete: EndpointsComplete, Ready: &ev.Ready, Total: &ev.Total, ReadAt: snap.TakenAt, Port: k.port, PortName: ev.PortName}
	// The severity of an empty port is KGWUI-023's; while 023 holds or skips the ref it is unknown.
	for _, f := range found {
		if e, ok := f.Facts.(*health.EndpointsEvidence); ok && f.Code == health.CodeBackendNoReadyEndpoints && e.Port == k.port {
			ph.found = append(ph.found, Signal{Kind: SignalFinding, Code: f.Code, Ref: ref, State: sevState(f.Severity)})
		}
	}
	if !ready && len(ph.found) == 0 {
		ph.sig = &Signal{Kind: SignalWaiting, Code: health.CodeBackendNoReadyEndpoints, Ref: ref, State: health.StateUnknown}
	}
	return ph
}
