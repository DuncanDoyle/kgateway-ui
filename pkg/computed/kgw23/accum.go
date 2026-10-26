package kgw23

import (
	"maps"
	"slices"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// accumulator folds delegation contexts into rows as the traversal produces
// them (rules §5.7). Aggregates, Deps, Reporters and digests take every
// context; a context itself is kept only within the retention caps.
type accumulator struct {
	bounds computed.Bounds
	byKey  map[computed.RowKey]*rowAcc
	// kept counts the retained contexts of the install.
	kept   int
	hasher *computed.ContextHasher
	// sorted are the rows in RowKey order, set by rows.
	sorted []*rowAcc
	// tooLarge is set when a compute cap was hit, so a row with no context
	// may have been reached: it is too-large, not not-reached (rules §3.3).
	tooLarge bool
}

// rowAcc is one row's running aggregate.
type rowAcc struct {
	row computed.Row
	// seen counts the contexts per outcome index; n is their total.
	seen [len(outcomes)]int
	n    int
	// unknown is the first unknown reason seen; scope is set by the meta or by any scope-unknown context.
	unknown computed.UnknownReason
	scope   bool
	// sharedDeps is true while row.Deps is a caller's slice, so it must be cloned before it grows.
	sharedDeps bool
	// tags has one entry per retained context, for the effective projection.
	tags []ctxTag
	// child is the child's index in the walker's routes, or -1.
	child int32
}

// ctxTag is where a retained context came from: its install-wide retention
// order and its frame. Contexts of one frame and child are the same view C
// evaluation; frame 0 is never shared.
type ctxTag struct{ seq, frame uint32 }

func newAccumulator(b computed.Bounds) *accumulator {
	return &accumulator{bounds: b, byKey: map[computed.RowKey]*rowAcc{}, hasher: computed.NewContextHasher()}
}

// open starts a row and returns it. It is called once per row before any add.
func (a *accumulator) open(key computed.RowKey, meta computed.RowMeta) *rowAcc {
	r := &rowAcc{row: computed.Row{Key: key, RefAsWritten: meta.RefAsWritten, Allows: meta.Allows,
		Unresolved: meta.Unresolved}, scope: meta.ScopeUnknown, child: -1}
	a.byKey[key] = r
	return r
}

// add folds c into the row of key. c's slices are shared, never copied. A
// zero-child unresolved row gets its contexts too, for their reporters and deps.
func (a *accumulator) add(key computed.RowKey, c computed.Context) {
	a.addTo(a.byKey[key], c, a.hasher.Sum(&c), 0)
}

// addTo is add for an open row r, with d the context's ContextHasher sum and
// frame the id of the frame it was evaluated under.
func (a *accumulator) addTo(r *rowAcc, c computed.Context, d uint64, frame uint32) {
	a.fold(r, c, d, frame, true)
}

// addCounted folds c like addTo but never retains it: a context cut by a compute
// cap carries nothing a sample would show. The row is marked truncated, so
// its contexts never read as complete.
func (a *accumulator) addCounted(r *rowAcc, c computed.Context, d uint64) { a.fold(r, c, d, 0, false) }

func (a *accumulator) fold(r *rowAcc, c computed.Context, d uint64, frame uint32, retain bool) {
	if r.n == 0 {
		r.row.KeyDigests = map[computed.ReportKey]uint64{}
	}
	r.seen[outcomeIndex(c.Outcome)]++
	r.n++
	switch {
	case c.Outcome != computed.OutcomeUnknown:
	case c.Unknown == computed.UnknownScope:
		r.scope = true
	case r.unknown == "":
		r.unknown = c.Unknown
	}
	if r.row.Reason == nil {
		r.row.Reason = c.Reason
	}
	r.addDeps(c.Deps)
	if i, ok := slices.BinarySearchFunc(r.row.Reporters, c.Reporter, computed.ReportKey.Compare); !ok {
		r.row.Reporters = slices.Insert(r.row.Reporters, i, c.Reporter)
	}
	r.row.ContextDigest += d // wrapping sum: order-independent
	r.row.KeyDigests[c.Reporter] += d
	if n := len(r.row.Contexts); retain && n < a.bounds.ContextsPerRow && a.kept < a.bounds.ContextsPerInstall {
		if n == cap(r.row.Contexts) {
			// Double, but never past the per-row cap.
			r.row.Contexts = slices.Grow(r.row.Contexts, min(max(n, 1), a.bounds.ContextsPerRow-n))
		}
		r.row.Contexts = append(r.row.Contexts, c)
		r.tags = append(r.tags, ctxTag{seq: uint32(a.kept), frame: frame}) //nolint:gosec // bounded by ContextsPerInstall
		a.kept++
	} else {
		r.row.ContextsTruncated = true
	}
}

// addDeps merges deps into the row's sorted set. The first sorted set is
// shared rather than copied, since contexts' Deps are read-only.
func (r *rowAcc) addDeps(deps []objects.Ref) {
	if len(deps) == 0 || r.sharedDeps && len(deps) == len(r.row.Deps) && &deps[0] == &r.row.Deps[0] {
		return // nothing new: the same shared set again
	}
	if len(r.row.Deps) == 0 {
		if slices.IsSortedFunc(deps, compareRef) && !hasAdjacentDup(deps) {
			r.row.Deps, r.sharedDeps = deps, true
			return
		}
	}
	for _, d := range deps {
		i, ok := slices.BinarySearchFunc(r.row.Deps, d, compareRef)
		if ok {
			continue
		}
		if r.sharedDeps {
			r.row.Deps, r.sharedDeps = slices.Clone(r.row.Deps), false
		}
		r.row.Deps = slices.Insert(r.row.Deps, i, d)
	}
}

func hasAdjacentDup(s []objects.Ref) bool {
	for i := 1; i < len(s); i++ {
		if s[i] == s[i-1] {
			return true
		}
	}
	return false
}

// rows returns every row in RowKey order with its final outcome (rules §5.5).
// incomplete reports whether a child is in a reach closure (rules §8.5).
func (a *accumulator) rows(incomplete func(objects.Ref) bool) []computed.Row {
	keys := slices.SortedFunc(maps.Keys(a.byKey), computed.RowKey.Compare)
	out := make([]computed.Row, 0, len(keys))
	a.sorted = make([]*rowAcc, 0, len(keys))
	for _, k := range keys {
		r := a.byKey[k]
		r.aggregate(incomplete, a.tooLarge)
		out = append(out, r.row)
		a.sorted = append(a.sorted, r)
	}
	return out
}

// signalled are the known outcomes besides attached and forbidden, the first
// present winning when no context attached (rules §5.5): an outcome the
// controller signals wins over a silent drop.
var signalled = []computed.RowOutcome{computed.OutcomeRejectedHostnames, computed.OutcomeCycle,
	computed.OutcomeUnresolved, computed.OutcomeDropped}

// aggregate sets the row's outcome from its meta and contexts (rules §5.5).
func (r *rowAcc) aggregate(incomplete func(objects.Ref) bool, tooLarge bool) {
	row := &r.row
	switch {
	case r.scope:
		row.Outcome, row.Unknown = computed.OutcomeUnknown, computed.UnknownScope
		return
	case row.Allows == computed.AllowsOther || r.count(computed.OutcomeForbidden) > 0:
		row.Outcome = computed.OutcomeForbidden
		return
	case row.Key.Child == (objects.Ref{}):
		// The ref selected nothing, whatever the context.
		row.Outcome = computed.OutcomeUnresolved
		return
	case r.n == 0:
		// Selected, but no context reached it.
		row.Outcome, row.Unknown = computed.OutcomeUnknown, computed.UnknownNotReached
		if tooLarge {
			row.Unknown = computed.UnknownTooLarge
		}
		return
	}
	missing := r.count(computed.OutcomeUnknown) > 0 || incomplete(row.Key.Child)
	att := r.count(computed.OutcomeAttached)
	switch {
	case att > 0:
		row.Outcome, row.SomeNotComputed = computed.OutcomeAttached, missing
		if att+r.count(computed.OutcomeUnknown) < r.n {
			row.Outcome = computed.OutcomePartial
		}
	case r.count(computed.OutcomeRejectedHostnames) > 0 && missing:
		// A rejection is positive evidence, like attached (rules §8.5).
		row.Outcome, row.SomeNotComputed = computed.OutcomeRejectedHostnames, true
	case missing:
		row.Outcome, row.Unknown = computed.OutcomeUnknown, r.unknown
		if row.Unknown == "" {
			row.Unknown = computed.UnknownContext // known contexts, but the child is in a reach closure
		}
	default:
		for _, o := range signalled {
			if r.count(o) > 0 {
				row.Outcome = o
				return
			}
		}
	}
}

// outcomes are the per-context outcomes, in the order rowAcc.seen counts them.
var outcomes = [...]computed.RowOutcome{computed.OutcomeAttached, computed.OutcomeDropped, computed.OutcomeForbidden,
	computed.OutcomeRejectedHostnames, computed.OutcomeCycle, computed.OutcomeUnresolved, computed.OutcomeUnknown}

// outcomeIndex is o's index in outcomes; any other value counts as unknown.
func outcomeIndex(o computed.RowOutcome) int {
	if i := slices.Index(outcomes[:], o); i >= 0 {
		return i
	}
	return len(outcomes) - 1
}

// count is the number of contexts with outcome o.
func (r *rowAcc) count(o computed.RowOutcome) int { return r.seen[outcomeIndex(o)] }
