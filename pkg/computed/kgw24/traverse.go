// Port of kgw23 for v2.4.7; keep in sync.

package kgw24

import (
	"cmp"
	"slices"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// refKey is kgateway's BackendMap key of a delegation ref: group, kind, name
// as written and namespace (pkg/kgateway/query/backendmap.go V4:24-32). So
// name "" and "*" are two keys, although both are the wildcard mode.
type refKey struct{ group, kind, name, ns string }

// errKind is a delegation ref's error in selectTree (rules §5.4).
type errKind uint8

const (
	errNone errKind = iota
	errUnresolved
	errCycle
)

// refSel is one delegation ref of a route and what it selects (rules §5.2,
// §5.3). It is computed once per recompute.
type refSel struct {
	// rule is the rule index; ref the backendRef index within the rule.
	rule, ref int
	key       refKey
	as        objects.DelegationRef
	// kids index the input routes, sorted by (namespace, name); allows is
	// each one's permission.
	kids       []int32
	allows     []computed.Allows
	unresolved computed.Unresolved
	// unknown is set when the selection is unknown: setting-unknown, or
	// too-large when the edge cap cut it.
	unknown computed.UnknownReason
	// partial is set when kids are certain but the selection may hold more:
	// a label ref under an unknown wildcard setting, whose possible routes
	// elsewhere carry the label (rules §3.3, §5.2).
	partial  computed.UnknownReason
	possible []int32
	// rows caches the open row of each kid, and zero the zero-child row, so
	// add skips the RowKey map.
	rows []*rowAcc
	zero *rowAcc
}

// evalSel is childrenFor's answer for one ref under one path: the
// children, an error, or unknown.
type evalSel struct {
	kids    []int32
	err     errKind
	unknown computed.UnknownReason
	// partial: kids are certain, but a possible child may also be selected.
	partial computed.UnknownReason
}

// routeSel is a route's delegation refs in (rule, ref) order, with the
// selectTree result for any path that holds none of its permitted children.
type routeSel struct {
	refs  []refSel
	plain []evalSel
	// permitted are the permitted selected children, sorted and unique: the
	// route's reach edges (rules §8.5). possible are the refs' possible
	// children, sorted and unique, which a path may also hold.
	permitted, possible []int32
}

// ruleRange is the range of refs of rule.
func (s *routeSel) ruleRange(rule int) (lo, hi int) {
	lo, _ = slices.BinarySearchFunc(s.refs, rule, func(r refSel, t int) int { return cmp.Compare(r.rule, t) })
	hi = lo
	for hi < len(s.refs) && s.refs[hi].rule == rule {
		hi++
	}
	return lo, hi
}

// frame is one incoming context of flatten (rules §5.1): the parent match,
// the path ending in the parent and that match, the parent's reporter keys
// and the context deps, for the parent's refs [lo, hi) of one rule. Its
// slices are shared and never written.
type frame struct {
	match objects.Match
	path  *computed.Path
	// keys has one entry for a child; a root reports under each of its
	// Gateway and ListenerSet parentRef keys.
	keys   []computed.ReportKey
	deps   []objects.Ref
	lo, hi int
	// memo holds each child's evaluation under this frame, when several refs
	// share it: quirk 1 hands a child to every later ref of its rule.
	memo map[int32]*kidEval
	// dig caches the context digest per outcome for a one-key frame; the
	// digest reads only the match, path, reporter and outcome.
	dig   [len(outcomes)]uint64
	digOK uint16
	// id names the frame in its retained contexts (ctxTag).
	id uint32
}

// kidEval is one child's evaluation under one frame (rules §5.4 flatten
// body): viewC, the outcome, and the frames the child is flattened under.
type kidEval struct {
	n       int // matcher steps charged per evaluation
	rules   []computed.EffectiveRule
	outcome computed.RowOutcome
	key     [1]computed.ReportKey
	frames  []frame
	built   bool
}

// newFrame is a frame for refs [lo, hi) under match m, extending path.
func newFrame(m objects.Match, path *computed.Path, step computed.PathStep, keys []computed.ReportKey, deps []objects.Ref, lo, hi int) frame {
	// Frames share their parent's path, so retained paths grow linearly.
	f := frame{match: m, path: path.Extend(step), keys: keys, deps: deps, lo: lo, hi: hi}
	if hi-lo > 1 {
		f.memo = map[int32]*kidEval{}
	}
	return f
}

type sigKey struct {
	parent objects.Ref
	key    computed.ReportKey
}

// walker runs views B and C for one install.
type walker struct {
	in     *computed.DelegationInput
	b      computed.Bounds
	routes []computed.DelegRoute
	sels   []routeSel
	acc    *accumulator

	edges, contexts, matchers int
	frames                    uint32
	cap                       string
	// edgeCut: the edge cap was hit, so every later selection is unknown.
	// halted: the contexts or matchers cap was hit, so every later flatten is cut.
	edgeCut, halted bool

	// projBuf and projSeen are the effective projection's scratch space.
	projBuf  []tagged
	projSeen map[uint32]int

	signals   []computed.ParentSignal
	sigIdx    map[sigKey]int
	uncertain []computed.Uncertain
}

// delegation computes views B and C (rules §5): selection and permission for
// every in-scope route, the context-aware traversal from the roots, the reach
// closures, the final row outcomes and the effective projection.
func delegation(in computed.DelegationInput) computed.DelegationResult {
	w := newWalker(in)
	w.run()
	return w.result()
}

// newWalker is a walker over in with its bounds defaulted.
func newWalker(in computed.DelegationInput) *walker {
	w := &walker{in: &in, b: withDefaults(in.Bounds), routes: in.Routes, sigIdx: map[sigKey]int{}}
	w.acc = newAccumulator(w.b)
	return w
}

// run selects every ref and walks the roots.
func (w *walker) run() {
	w.selectAll()
	if w.in.ScopeUnknown {
		// D-024: no traversal; every row is scope-unknown and every root uncertain.
		for _, r := range w.in.Roots {
			w.uncertain = append(w.uncertain, computed.Uncertain{Route: r.Route, Rule: computed.UncertainRoot, Ref: -1, Reason: computed.UnknownScope})
		}
	} else {
		w.walkRoots()
	}
	w.acc.tooLarge = w.cap != ""
}

// result completes the traversal: the reach closures first, since a row's
// final outcome reads them (rules §5.5, §8.5), then the effective projection.
func (w *walker) result() computed.DelegationResult {
	res := computed.DelegationResult{Signals: w.signals, Reach: w.reach(), Uncertain: w.sortedUncertain(),
		UncertainKeys: uncertainKeys(w.in.Roots), Cap: w.cap}
	res.Complete = computed.NewCompleteness(res)
	res.Rows = w.acc.rows(res.Complete.Incomplete)
	w.setChildKeys(res.Rows)
	res.Effective = w.project()
	slices.SortFunc(res.Signals, func(a, b computed.ParentSignal) int {
		return cmp.Or(a.Parent.Compare(b.Parent), a.Key.Compare(b.Key))
	})
	return res
}

// uncertainKeys lists every root's UncertainKeys, sorted by (Route, Key).
func uncertainKeys(roots []computed.Root) []computed.RouteKey {
	var out []computed.RouteKey
	for _, r := range roots {
		for _, k := range r.UncertainKeys {
			out = append(out, computed.RouteKey{Route: r.Route, Key: k})
		}
	}
	slices.SortFunc(out, func(a, b computed.RouteKey) int { return cmp.Or(a.Route.Compare(b.Route), a.Key.Compare(b.Key)) })
	return slices.Compact(out)
}

// withDefaults gives each zero field of b its DefaultBounds value.
func withDefaults(b computed.Bounds) computed.Bounds {
	d := computed.DefaultBounds
	or := func(v, def int) int { return cmp.Or(v, def) }
	return computed.Bounds{Edges: or(b.Edges, d.Edges), Contexts: or(b.Contexts, d.Contexts), Matchers: or(b.Matchers, d.Matchers),
		ContextsPerRow: or(b.ContextsPerRow, d.ContextsPerRow), ContextsPerInstall: or(b.ContextsPerInstall, d.ContextsPerInstall),
		EffectiveEdges: or(b.EffectiveEdges, d.EffectiveEdges), ContextsPerEdge: or(b.ContextsPerEdge, d.ContextsPerEdge)}
}

// frameID is a new frame's id, never 0.
func (w *walker) frameID() uint32 {
	w.frames++
	return w.frames
}

func (w *walker) hit(c string) {
	if w.cap == "" {
		w.cap = c
	}
}

// index is ref's index in the input routes.
func (w *walker) index(ref objects.Ref) (int, bool) {
	return slices.BinarySearchFunc(w.routes, ref, func(r computed.DelegRoute, t objects.Ref) int { return r.Ref.Compare(t) })
}

// selectAll selects and permits every delegation ref of every in-scope
// route, in route order, charging the edge cap (rules §5.7): a ref whose
// children would pass it, and every ref after it, is too-large. It opens the
// rows of owned parents, so an unreached one keeps them (rules §3.3).
func (w *walker) selectAll() {
	s := newSelector(w.routes, w.in.Unwatched, w.in.Wildcard)
	w.sels = make([]routeSel, len(w.routes))
	for pi := range w.routes {
		p, rs := &w.routes[pi], &w.sels[pi]
		for _, rule := range p.View.Rules {
			for bi, b := range rule.BackendRefs {
				if b.Delegation == nil {
					continue
				}
				sel := refSel{rule: rule.Index, ref: bi, key: refKey{b.Ref.Group, b.Ref.Kind, b.Ref.Name, b.Delegation.Namespace}, as: *b.Delegation}
				children, possible, unresolved, unknown := s.fetch(*b.Delegation)
				switch {
				case w.edgeCut:
					sel.unknown = computed.UnknownTooLarge
				case w.edges+len(children) > w.b.Edges:
					w.edgeCut, sel.unknown = true, computed.UnknownTooLarge
					w.hit(computed.CapEdges)
				default:
					w.edges += len(children) // forbidden included
					sel.unresolved = unresolved
					sel.kids, sel.allows = make([]int32, len(children)), make([]computed.Allows, len(children))
					sel.rows = make([]*rowAcc, len(children))
					for k, c := range children {
						i, _ := w.index(c)
						sel.kids[k] = int32(i) //nolint:gosec // route counts fit in int32
						_, sel.allows[k] = permits(w.routes[i].View, c.Namespace, p.Ref)
					}
					if unknown != "" {
						// Uncertainty is never a selection: the possible routes get no
						// edges, only uncertain points (rules §3.3, §8.5).
						sel.partial = unknown
						for _, r := range possible {
							i, _ := w.index(r)
							sel.possible = append(sel.possible, int32(i)) //nolint:gosec // route counts fit in int32
							w.uncertain = append(w.uncertain, computed.Uncertain{Route: r, Rule: computed.UncertainPossible, Ref: -1, Reason: unknown})
						}
					}
				}
				if u := cmp.Or(sel.unknown, sel.partial); u != "" {
					w.uncertain = append(w.uncertain, computed.Uncertain{Route: p.Ref, Rule: sel.rule, Ref: sel.ref, Reason: u})
				}
				rs.refs = append(rs.refs, sel)
			}
		}
		if len(rs.refs) == 0 {
			continue
		}
		rs.plain = selectTree(rs.refs, nil)
		for i := range rs.refs {
			r := &rs.refs[i]
			for k, c := range r.kids {
				if r.allows[k] != computed.AllowsOther {
					rs.permitted = append(rs.permitted, c)
				}
			}
			rs.possible = append(rs.possible, r.possible...)
			if !p.Owned || r.unknown != "" {
				continue
			}
			if r.unresolved != "" || r.partial != "" {
				w.open(pi, r, -1)
			}
			for _, c := range r.kids {
				w.open(pi, r, c)
			}
		}
		slices.Sort(rs.permitted)
		rs.permitted = slices.Compact(rs.permitted)
		slices.Sort(rs.possible)
		rs.possible = slices.Compact(rs.possible)
	}
}

// selectTree is getDelegatedChildren followed by BackendMap.get for every ref
// of one parent (rules §5.4; pkg/kgateway/query/route.go V4:233-292,
// query/backendmap.go V4:34-54). Children accumulate per rule, not per ref
// (quirk 1, V4:250), and are stored per ref key, so a later rule overwrites
// (quirk 2, V4:288). A forbidden child is skipped before the path-scoped
// cycle check (V4:267-276). Errors are never cleared, and win over children.
// onPath is nil for a path that holds none of the parent's permitted children.
// An unknown selection makes its key, and every later ref of its rule, unknown.
func selectTree(refs []refSel, onPath func(int32) bool) []evalSel {
	type state struct {
		key                   refKey
		items                 []int32
		has                   bool
		err                   errKind
		unknown, itemsUnknown computed.UnknownReason
	}
	states := make([]state, 0, len(refs))
	at := make([]int, len(refs)) // ref → its key's state
	for i := range refs {
		j := slices.IndexFunc(states, func(s state) bool { return s.key == refs[i].key })
		if j < 0 {
			j = len(states)
			states = append(states, state{key: refs[i].key})
		}
		at[i] = j
	}
	var acc []int32
	var accUnknown computed.UnknownReason
	for i := range refs {
		r, st := &refs[i], &states[at[i]]
		if i == 0 || r.rule != refs[i-1].rule {
			acc, accUnknown = nil, ""
		}
		switch {
		case r.unknown != "":
			st.unknown = cmp.Or(st.unknown, r.unknown)
			accUnknown = cmp.Or(accUnknown, r.unknown)
			continue
		case r.unresolved != "":
			st.err = errUnresolved
			continue
		}
		// A possible child on the path may fail the ref with a cycle, so its
		// children are unknown under this path.
		mayCycle := onPath != nil && slices.ContainsFunc(r.possible, onPath)
		for k, c := range r.kids {
			if r.allows[k] == computed.AllowsOther {
				continue
			}
			if onPath != nil && onPath(c) {
				st.err = errCycle
				continue
			}
			acc = append(acc, c)
		}
		// Clipped, so a later append to acc never writes into a stored list.
		st.items, st.has, st.itemsUnknown = slices.Clip(acc), true, accUnknown
		if mayCycle {
			st.itemsUnknown = cmp.Or(st.itemsUnknown, r.partial)
		}
		// Quirk 1 hands the possible children to the rule's later refs too.
		accUnknown = cmp.Or(accUnknown, r.partial)
	}
	out := make([]evalSel, len(refs))
	for i := range refs {
		st := &states[at[i]]
		switch {
		case st.err != errNone:
			out[i].err = st.err
		case st.unknown != "":
			out[i].unknown = st.unknown
		case st.has:
			out[i].kids, out[i].unknown, out[i].partial = st.items, st.itemsUnknown, refs[i].partial
		default:
			out[i].err = errUnresolved
		}
	}
	return out
}

// eval is childrenFor(P, ref, path): the memoised answer unless a permitted
// child of P is on the path, when selectTree runs again with the path.
func (w *walker) eval(pi, ri int, path *computed.Path) evalSel {
	rs := &w.sels[pi]
	byRef := func(i int32, t objects.Ref) int { return w.routes[i].Ref.Compare(t) }
	for s := range path.All() {
		_, permitted := slices.BinarySearchFunc(rs.permitted, s.Route, byRef)
		if _, possible := slices.BinarySearchFunc(rs.possible, s.Route, byRef); permitted || possible {
			return selectTree(rs.refs, func(c int32) bool {
				for s := range path.All() {
					if s.Route == w.routes[c].Ref {
						return true
					}
				}
				return false
			})[ri]
		}
	}
	return rs.plain[ri]
}

// walkRoots flattens every root rule match and delegation ref, roots by
// (namespace, name), rules and refs by index (rules §5.1, §5.7;
// translator/httproute/gateway_http_route_translator.go V4:74-79, 119, 190-211).
func (w *walker) walkRoots() {
	byRoute := func(a, b computed.Root) int { return a.Route.Compare(b.Route) }
	roots := w.in.Roots // the engine sorts them; copy only when it did not
	if !slices.IsSortedFunc(roots, byRoute) {
		roots = slices.SortedFunc(slices.Values(roots), byRoute)
	}
	for _, root := range roots {
		if root.Unknown {
			w.uncertain = append(w.uncertain, computed.Uncertain{Route: root.Route, Rule: computed.UncertainRoot, Ref: -1, Reason: computed.UnknownContext})
			continue
		}
		pi, ok := w.index(root.Route)
		if !ok || len(w.sels[pi].refs) == 0 {
			continue
		}
		rs, v := &w.sels[pi], w.routes[pi].View
		deps := slices.Clone(root.Deps)
		slices.SortFunc(deps, compareRef)
		// Only keys view A accepted, or the status entry's, are written under.
		keys, ds := root.Keys, withDep(slices.Compact(deps), root.Route)
		for _, rule := range v.Rules {
			lo, hi := rs.ruleRange(rule.Index)
			if lo == hi {
				continue
			}
			for _, m := range rootMatches(rule.Matches) {
				f := newFrame(m, nil, computed.PathStep{Route: root.Route, Match: m}, keys, ds, lo, hi)
				f.id = w.frameID()
				for ri := lo; ri < hi; ri++ {
					w.flatten(pi, ri, &f)
				}
			}
		}
	}
}

// rootMatches are a root rule's matches: the CRD default when omitted, and
// one empty match for an explicit empty list, as kgateway does
// (gateway_http_route_translator.go V4:75-79).
func rootMatches(ms []objects.Match) []objects.Match {
	switch {
	case ms == nil:
		return []objects.Match{defaultMatch()}
	case len(ms) == 0:
		return []objects.Match{{}}
	}
	return ms
}

// flatten is rules §5.4 flatten for P's ref ri under f
// (translator/httproute/delegation.go V4:26-102). The contexts cap is charged
// per call and the matchers cap per child before viewC; past either cap,
// everything is cut.
func (w *walker) flatten(pi, ri int, f *frame) {
	rs := &w.sels[pi].refs[ri]
	if !w.halted && w.contexts >= w.b.Contexts {
		w.halted = true
		w.hit(computed.CapContexts)
	}
	if w.halted {
		w.cut(pi, ri, f)
		return
	}
	w.contexts++
	if rs.unknown != "" {
		return // already an uncertain point; no child is known
	}
	ev := w.eval(pi, ri, f.path)
	parent := w.routes[pi].Ref
	// Forbidden children are skipped silently (query/route.go V4:267-269).
	for k, c := range rs.kids {
		if rs.allows[k] == computed.AllowsOther {
			w.add(pi, rs, c, f, computed.Context{Outcome: computed.OutcomeForbidden})
		}
	}
	switch {
	case ev.err == errUnresolved:
		// delegation.go V4:45-48 returns the error; setRouteAction reports it
		// (gateway_http_route_translator.go V4:207-209, query/utils.go V4:45-51).
		w.signal(parent, f, computed.ReasonBackendNotFound)
		w.add(pi, rs, -1, f, computed.Context{Outcome: computed.OutcomeUnresolved})
	case ev.err == errCycle:
		// One cycle fails the whole ref (query/utils.go V4:38-44).
		w.signal(parent, f, computed.ReasonRefNotPermitted)
		for k, c := range rs.kids {
			if rs.allows[k] != computed.AllowsOther {
				w.add(pi, rs, c, f, computed.Context{Outcome: computed.OutcomeCycle})
			}
		}
	case ev.unknown != "":
		w.uncertainRef(parent, rs, ev.unknown)
		for _, c := range ev.kids {
			w.add(pi, rs, c, f, computed.Context{Outcome: computed.OutcomeUnknown, Unknown: ev.unknown})
		}
	default:
		if ev.partial != "" {
			w.add(pi, rs, -1, f, computed.Context{Outcome: computed.OutcomeUnknown, Unknown: ev.partial})
		}
		w.children(pi, rs, ev.kids, f, ev.partial != "")
	}
}

// children fits each child to f's match (filterDelegatedChildren,
// delegation_helpers.go V4:33-107), then checks hostnames after the fit
// (delegation.go V4:83-91) and recurses into each kept child (V4:93-96).
// With no kept child the parent gets BackendNotFound (V4:98-100), unless a
// cap cut a child or the selection is partial: a possible child may fit.
func (w *walker) children(pi int, rs *refSel, kids []int32, f *frame, partial bool) {
	valid, cut := 0, false
	for _, ki := range kids {
		e := f.memo[ki]
		if !w.halted {
			n := 0
			if e != nil {
				n = e.n
			} else {
				n = matchCount(w.routes[ki].View)
			}
			// PF15: charge the child's matches before viewC; a call that would pass the cap is not made.
			if w.matchers+n > w.b.Matchers {
				w.halted = true
				w.hit(computed.CapMatchers)
			} else {
				w.matchers += n
			}
			if e == nil && !w.halted {
				e = w.evalKid(ki, n, f)
			}
		}
		if w.halted {
			cut = true
			w.add(pi, rs, ki, f, computed.Context{Outcome: computed.OutcomeUnknown, Unknown: computed.UnknownTooLarge})
			continue
		}
		c := computed.Context{Outcome: e.outcome, Rules: e.rules}
		if e.outcome == computed.OutcomeDropped {
			c.Reason = firstReason(e.rules)
		}
		w.add(pi, rs, ki, f, c)
		if e.outcome == computed.OutcomeAttached {
			valid++
			w.descend(pi, ki, e, f)
		}
	}
	if cut {
		w.uncertainRef(w.routes[pi].Ref, rs, computed.UnknownTooLarge)
	} else if valid == 0 && !partial {
		w.signal(w.routes[pi].Ref, f, computed.ReasonBackendNotFound)
	}
}

// evalKid runs viewC for child ki under f, memoised in f when f has a memo.
func (w *walker) evalKid(ki int32, n int, f *frame) *kidEval {
	kid := &w.routes[ki]
	e := &kidEval{n: n, outcome: computed.OutcomeAttached}
	var kept bool
	e.rules, kept = viewC(kid.View, f.match)
	switch {
	case !kept:
		e.outcome = computed.OutcomeDropped
	case len(kid.View.Hostnames) > 0:
		e.outcome = computed.OutcomeRejectedHostnames
	}
	if f.memo != nil {
		f.memo[ki] = e
	}
	return e
}

// descend flattens each effective match and delegation ref of each kept
// rule of child ki, in order (translateGatewayHTTPRouteRulesUtil on the
// clone, gateway_http_route_translator.go V4:74-92, 119, 190-211). The
// child's frames are built once per evaluation and reused by its repeats.
func (w *walker) descend(pi int, ki int32, e *kidEval, f *frame) {
	ks := &w.sels[ki]
	if len(ks.refs) == 0 {
		return
	}
	if !e.built {
		e.built = true
		kid := &w.routes[ki]
		e.key[0], _ = childKey(kid.View, kid.Ref.Namespace, w.routes[pi].Ref)
		deps := withDep(f.deps, kid.Ref)
		for _, er := range e.rules {
			if !er.Kept {
				continue
			}
			lo, hi := ks.ruleRange(er.Index)
			if lo == hi {
				continue
			}
			for _, m := range er.Matches {
				nf := newFrame(m, f.path, computed.PathStep{Route: kid.Ref, Match: m}, e.key[:], deps, lo, hi)
				nf.id = w.frameID()
				e.frames = append(e.frames, nf)
			}
		}
	}
	for i := range e.frames {
		nf := &e.frames[i]
		for ri := nf.lo; ri < nf.hi; ri++ {
			w.flatten(int(ki), ri, nf)
		}
	}
}

// cut records a flatten call past a cap: the (parent, ref) is uncertain and
// each of its rows gets an unknown, too-large context. Its rows are the ref's
// own selection plus any children quirks 1 and 2 hand it.
func (w *walker) cut(pi, ri int, f *frame) {
	rs := &w.sels[pi].refs[ri]
	w.uncertainRef(w.routes[pi].Ref, rs, computed.UnknownTooLarge)
	if rs.unknown != "" {
		return
	}
	c := computed.Context{Outcome: computed.OutcomeUnknown, Unknown: computed.UnknownTooLarge}
	if rs.unresolved != "" || rs.partial != "" {
		w.add(pi, rs, -1, f, c)
	}
	for _, k := range rs.kids {
		w.add(pi, rs, k, f, c)
	}
	if ev := w.eval(pi, ri, f.path); ev.err == errNone {
		for _, k := range ev.kids {
			// Own kids are sorted by index; skip those already given a context.
			if _, own := slices.BinarySearch(rs.kids, k); !own {
				w.add(pi, rs, k, f, c)
			}
		}
	}
}

// matchCount is the matcher steps viewC takes for child: its matches, at
// least one per rule.
func matchCount(v *objects.HTTPRouteView) int {
	n := 0
	for _, r := range v.Rules {
		n += max(1, len(r.Matches))
	}
	return n
}

// firstReason is the reason of the first dropped rule.
func firstReason(rules []computed.EffectiveRule) *computed.FitFailure {
	for _, r := range rules {
		if r.Reason != nil {
			return r.Reason
		}
	}
	return nil
}

// open opens row (P, ref, child), or returns it when open; child -1 is the
// zero-child row. Rows of rs's own kids and its zero-child row are cached in rs.
func (w *walker) open(pi int, rs *refSel, child int32) *rowAcc {
	slot := &rs.zero
	if child >= 0 {
		slot = nil
		if k, ok := slices.BinarySearch(rs.kids, child); ok {
			slot = &rs.rows[k]
		}
	}
	if slot != nil && *slot != nil {
		return *slot
	}
	r := w.openKey(pi, rs, child)
	if slot != nil {
		*slot = r
	}
	return r
}

// openKey is open through the RowKey map.
func (w *walker) openKey(pi int, rs *refSel, child int32) *rowAcc {
	key := computed.RowKey{Parent: w.routes[pi].Ref, Rule: rs.rule, Ref: rs.ref}
	if child >= 0 {
		key.Child = w.routes[child].Ref
	}
	if r, ok := w.acc.byKey[key]; ok {
		return r
	}
	meta := computed.RowMeta{RefAsWritten: rs.as, ScopeUnknown: w.in.ScopeUnknown}
	if child < 0 {
		if rs.partial == "" {
			meta.Unresolved = cmp.Or(rs.unresolved, computed.UnresolvedMissing)
		}
	} else {
		c := &w.routes[child]
		_, meta.Allows = permits(c.View, c.Ref.Namespace, key.Parent)
	}
	r := w.acc.open(key, meta)
	r.child = child
	return r
}

// add folds c into row (P, ref, child), once per reporter key of f. A
// too-large context is counted but never retained.
func (w *walker) add(pi int, rs *refSel, child int32, f *frame, c computed.Context) {
	r := w.open(pi, rs, child)
	c.ParentMatch, c.Path, c.Deps = f.match, f.path, f.deps
	cut := c.Unknown == computed.UnknownTooLarge
	for _, k := range f.keys {
		c.Reporter = k
		if cut {
			w.acc.addCounted(r, c, w.digest(f, &c))
		} else {
			w.acc.addTo(r, c, w.digest(f, &c), f.id)
		}
	}
}

// digest is c's ContextHasher sum, cached in a one-key frame by outcome.
func (w *walker) digest(f *frame, c *computed.Context) uint64 {
	if len(f.keys) != 1 {
		return w.acc.hasher.Sum(c)
	}
	o := outcomeIndex(c.Outcome)
	if f.digOK&(1<<o) == 0 {
		f.dig[o], f.digOK = w.acc.hasher.Sum(c), f.digOK|1<<o
	}
	return f.dig[o]
}

// signal records reason on parent's reporters (rules §5.4 parentSignal).
func (w *walker) signal(parent objects.Ref, f *frame, reason string) {
	for _, k := range f.keys {
		sk := sigKey{parent, k}
		i, ok := w.sigIdx[sk]
		if !ok {
			i = len(w.signals)
			w.sigIdx[sk] = i
			w.signals = append(w.signals, computed.ParentSignal{Parent: parent, Key: k})
		}
		s := &w.signals[i]
		if !slices.Contains(s.Reasons, reason) {
			s.Reasons = append(s.Reasons, reason)
		}
		s.Deps = unionRefs(s.Deps, f.deps)
	}
}

func (w *walker) uncertainRef(parent objects.Ref, rs *refSel, reason computed.UnknownReason) {
	w.uncertain = append(w.uncertain, computed.Uncertain{Route: parent, Rule: rs.rule, Ref: rs.ref, Reason: reason})
}

// sortedUncertain sorts the uncertain points by (Route, Rule, Ref), keeping
// the first reason of each.
func (w *walker) sortedUncertain() []computed.Uncertain {
	slices.SortStableFunc(w.uncertain, func(a, b computed.Uncertain) int {
		return cmp.Or(a.Route.Compare(b.Route), cmp.Compare(a.Rule, b.Rule), cmp.Compare(a.Ref, b.Ref))
	})
	return slices.CompactFunc(w.uncertain, func(a, b computed.Uncertain) bool {
		return a.Route == b.Route && a.Rule == b.Rule && a.Ref == b.Ref
	})
}

// reach is every (route → permitted selected child) edge, sorted.
func (w *walker) reach() []computed.Edge {
	n := 0
	for i := range w.sels {
		n += len(w.sels[i].permitted)
	}
	out := make([]computed.Edge, 0, n)
	for pi := range w.sels {
		for _, c := range w.sels[pi].permitted {
			out = append(out, computed.Edge{Parent: w.routes[pi].Ref, Child: w.routes[c].Ref})
		}
	}
	return out
}

// setChildKeys sets each child row's report key under its parent (rules §7).
func (w *walker) setChildKeys(rows []computed.Row) {
	for i := range rows {
		r := &rows[i]
		if r.Key.Child == (objects.Ref{}) {
			continue
		}
		if ci, ok := w.index(r.Key.Child); ok {
			c := &w.routes[ci]
			r.ChildKey, r.WritesEntry = childKey(c.View, c.Ref.Namespace, r.Key.Parent)
		}
	}
}

// withDep returns the sorted deps with r added; deps is never written.
func withDep(deps []objects.Ref, r objects.Ref) []objects.Ref {
	i, ok := slices.BinarySearchFunc(deps, r, compareRef)
	if ok {
		return deps
	}
	out := make([]objects.Ref, 0, len(deps)+1)
	out = append(out, deps[:i]...)
	out = append(out, r)
	return append(out, deps[i:]...)
}

// unionRefs merges sorted sets; it returns a or b unchanged when one holds
// the other's elements, and never writes either.
func unionRefs(a, b []objects.Ref) []objects.Ref {
	switch {
	case len(a) == 0:
		return b
	case len(b) == 0 || len(a) == len(b) && &a[0] == &b[0]:
		return a // nothing new, or the same shared set
	}
	if !slices.ContainsFunc(b, func(r objects.Ref) bool { _, ok := slices.BinarySearchFunc(a, r, compareRef); return !ok }) {
		return a
	}
	out := make([]objects.Ref, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch c := compareRef(a[i], b[j]); {
		case c < 0:
			out = append(out, a[i])
			i++
		case c > 0:
			out = append(out, b[j])
			j++
		default:
			out = append(out, a[i])
			i, j = i+1, j+1
		}
	}
	out = append(out, a[i:]...)
	return append(out, b[j:]...)
}
