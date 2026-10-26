package kgw23

import (
	"encoding/binary"
	"hash"
	"hash/fnv"
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
	kept int
	// h and b hash one context at a time; b is reused across contexts.
	h hash.Hash64
	b []byte
}

// rowAcc is one row's running aggregate.
type rowAcc struct {
	row computed.Row
	// seen counts the contexts per outcome index; n is their total.
	seen [len(outcomes)]int
	n    int
	// unknown is the first unknown reason seen; scope is set once any context is scope-unknown.
	unknown computed.UnknownReason
	scope   bool
	// sharedDeps is true while row.Deps is a caller's slice, so it must be cloned before it grows.
	sharedDeps bool
}

func newAccumulator(b computed.Bounds) *accumulator {
	return &accumulator{bounds: b, byKey: map[computed.RowKey]*rowAcc{}, h: fnv.New64a()}
}

// open starts a row. It is called once per row before any add.
func (a *accumulator) open(key computed.RowKey, meta computed.RowMeta) {
	a.byKey[key] = &rowAcc{row: computed.Row{Key: key, RefAsWritten: meta.RefAsWritten, Allows: meta.Allows,
		Unresolved: meta.Unresolved}}
}

// add folds c into the row of key. c's slices are shared, never copied.
func (a *accumulator) add(key computed.RowKey, c computed.Context) {
	r := a.byKey[key]
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
	d := a.digest(&c)
	r.row.ContextDigest += d // wrapping sum: order-independent
	r.row.KeyDigests[c.Reporter] += d
	if len(r.row.Contexts) < a.bounds.ContextsPerRow && a.kept < a.bounds.ContextsPerInstall {
		r.row.Contexts = append(r.row.Contexts, c)
		a.kept++
	} else {
		r.row.ContextsTruncated = true
	}
}

// addDeps merges deps into the row's sorted set. The first sorted set is
// shared rather than copied, since contexts' Deps are read-only.
func (r *rowAcc) addDeps(deps []objects.Ref) {
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
	for _, k := range keys {
		r := a.byKey[k]
		r.aggregate(incomplete)
		out = append(out, r.row)
	}
	return out
}

// nonAttached are the known outcomes besides attached and forbidden, the
// first present winning when no context attached (our call: an outcome the
// controller signals wins over a silent drop).
var nonAttached = []computed.RowOutcome{computed.OutcomeRejectedHostnames, computed.OutcomeCycle,
	computed.OutcomeUnresolved, computed.OutcomeDropped}

// aggregate sets the row's outcome from its contexts (rules §5.5).
func (r *rowAcc) aggregate(incomplete func(objects.Ref) bool) {
	row := &r.row
	switch {
	case r.scope:
		row.Outcome, row.Unknown = computed.OutcomeUnknown, computed.UnknownScope
		return
	case r.count(computed.OutcomeForbidden) > 0:
		row.Outcome = computed.OutcomeForbidden
		return
	case r.n == 0 && row.Key.Child == (objects.Ref{}):
		row.Outcome = computed.OutcomeUnresolved
		return
	case r.n == 0:
		// Selected, but no context reached it.
		row.Outcome, row.Unknown = computed.OutcomeUnknown, computed.UnknownNotReached
		return
	}
	missing := r.count(computed.OutcomeUnknown) > 0 || incomplete(row.Key.Child)
	if att := r.count(computed.OutcomeAttached); att > 0 {
		row.Outcome, row.SomeNotComputed = computed.OutcomeAttached, missing
		if att+r.count(computed.OutcomeUnknown) < r.n {
			row.Outcome = computed.OutcomePartial
		}
		return
	}
	if missing {
		row.Outcome, row.Unknown = computed.OutcomeUnknown, r.unknown
		if row.Unknown == "" {
			row.Unknown = computed.UnknownContext // known contexts, but the child is in a reach closure
		}
		return
	}
	for _, o := range nonAttached {
		if r.count(o) > 0 {
			row.Outcome = o
			return
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

// digest hashes c's incoming match, path, reporter and outcome with FNV-64a
// over a length-prefixed encoding, as the 2.3 fingerprints do.
func (a *accumulator) digest(c *computed.Context) uint64 {
	a.b = a.b[:0]
	a.match(c.ParentMatch)
	a.num(len(c.Path))
	for _, s := range c.Path {
		a.ref(s.Route)
		a.match(s.Match)
	}
	k := c.Reporter
	a.str(k.Group)
	a.str(k.Kind)
	a.str(k.Namespace)
	a.str(k.Name)
	a.str(k.SectionName)
	a.str(string(c.Outcome))
	a.h.Reset()
	_, _ = a.h.Write(a.b)
	return a.h.Sum64()
}

func (a *accumulator) str(s string) {
	a.b = binary.AppendUvarint(a.b, uint64(len(s)))
	a.b = append(a.b, s...)
}

func (a *accumulator) num(n int) { a.b = binary.AppendVarint(a.b, int64(n)) }

func (a *accumulator) ref(r objects.Ref) {
	a.str(r.Group)
	a.str(r.Kind)
	a.str(r.Namespace)
	a.str(r.Name)
}

func (a *accumulator) match(m objects.Match) {
	if m.Path == nil {
		a.num(0)
	} else {
		a.num(1)
		a.str(m.Path.Type)
		a.str(m.Path.Value)
	}
	for _, kms := range [][]objects.KeyMatch{m.Headers, m.QueryParams} {
		a.num(len(kms))
		for _, km := range kms {
			a.str(km.Type)
			a.str(km.Name)
			a.str(km.Value)
		}
	}
	a.str(m.Method)
}
