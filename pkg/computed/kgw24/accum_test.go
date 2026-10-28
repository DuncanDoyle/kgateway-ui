// Port of kgw23 for v2.4.7; keep in sync.

package kgw24

import (
	"fmt"
	"slices"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

var (
	accParent = routeRef("shop", "parent")
	accChild  = routeRef("shop", "child")
	repA      = computed.ReportKey{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "gw", Name: "a"}
	repB      = computed.ReportKey{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Name: "parent"}
)

func rowKey(rule, ref int, child objects.Ref) computed.RowKey {
	return computed.RowKey{Parent: accParent, Rule: rule, Ref: ref, Child: child}
}

// ctx is a context under prefix p with outcome o, reporter repA and deps {parent, child}.
func ctx(p string, o computed.RowOutcome) computed.Context {
	return computed.Context{ParentMatch: prefix(p), Path: computed.PathOf(computed.PathStep{Route: accParent, Match: prefix("/")}),
		Reporter: repA, Outcome: o, Deps: []objects.Ref{accChild, accParent}}
}

func unknownCtx(p string, why computed.UnknownReason) computed.Context {
	c := ctx(p, computed.OutcomeUnknown)
	c.Unknown = why
	return c
}

func never(objects.Ref) bool { return false }

// one runs a single row through an accumulator and returns it.
func one(t *testing.T, b computed.Bounds, key computed.RowKey, meta computed.RowMeta, incomplete func(objects.Ref) bool, cs ...computed.Context) computed.Row {
	t.Helper()
	a := newAccumulator(b)
	a.open(key, meta)
	for _, c := range cs {
		a.add(key, c)
	}
	rows := a.rows(incomplete)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	return rows[0]
}

func TestAggregateOutcomes(t *testing.T) {
	drop := ctx("/a", computed.OutcomeDropped)
	drop.Reason = &computed.FitFailure{Rule: 0, Match: 0, Clause: computed.ClausePathPrefix, Parent: "/a", Child: "/b"}
	att := ctx("/b", computed.OutcomeAttached)
	cut := unknownCtx("/c", computed.UnknownTooLarge)
	isChild := func(r objects.Ref) bool { return r == accChild }
	for _, c := range []struct {
		name       string
		cs         []computed.Context
		incomplete func(objects.Ref) bool
		want       computed.RowOutcome
		unknown    computed.UnknownReason
		someNot    bool
	}{
		{"all attached", []computed.Context{att, att}, never, computed.OutcomeAttached, "", false},
		{"all dropped", []computed.Context{drop, drop}, never, computed.OutcomeDropped, "", false},
		{"all rejected", []computed.Context{ctx("/a", computed.OutcomeRejectedHostnames)}, never, computed.OutcomeRejectedHostnames, "", false},
		{"all cycle", []computed.Context{ctx("/a", computed.OutcomeCycle)}, never, computed.OutcomeCycle, "", false},
		{"attached and dropped", []computed.Context{att, drop}, never, computed.OutcomePartial, "", false},
		{"attached with unknown", []computed.Context{att, cut}, never, computed.OutcomeAttached, "", true},
		{"attached with incomplete child", []computed.Context{att}, isChild, computed.OutcomeAttached, "", true},
		{"Dropped on the paths computed; other paths not computed", []computed.Context{drop, cut}, never, computed.OutcomeUnknown, computed.UnknownTooLarge, false},
		{"dropped with incomplete child is never dropped", []computed.Context{drop}, isChild, computed.OutcomeUnknown, computed.UnknownContext, false},
		{"rejected with incomplete child", []computed.Context{ctx("/a", computed.OutcomeRejectedHostnames)}, isChild, computed.OutcomeRejectedHostnames, "", true},
		{"rejected with unknown", []computed.Context{drop, ctx("/a", computed.OutcomeRejectedHostnames), cut}, never, computed.OutcomeRejectedHostnames, "", true},
		{"attached and cycle", []computed.Context{att, ctx("/a", computed.OutcomeCycle)}, never, computed.OutcomePartial, "", false},
		{"attached and rejected", []computed.Context{att, ctx("/a", computed.OutcomeRejectedHostnames)}, never, computed.OutcomePartial, "", false},
		{"rejected before cycle", []computed.Context{ctx("/a", computed.OutcomeCycle), ctx("/a", computed.OutcomeRejectedHostnames)}, never, computed.OutcomeRejectedHostnames, "", false},
		{"cycle before unresolved", []computed.Context{ctx("/a", computed.OutcomeUnresolved), ctx("/a", computed.OutcomeCycle)}, never, computed.OutcomeCycle, "", false},
		{"unresolved before dropped", []computed.Context{drop, ctx("/a", computed.OutcomeUnresolved)}, never, computed.OutcomeUnresolved, "", false},
		{"empty outcome counts as unknown", []computed.Context{drop, ctx("/a", "")}, never, computed.OutcomeUnknown, computed.UnknownContext, false},
		{"forbidden is always known", []computed.Context{ctx("/a", computed.OutcomeForbidden), cut}, isChild, computed.OutcomeForbidden, "", false},
		{"scope unknown wins", []computed.Context{att, unknownCtx("/a", computed.UnknownScope)}, never, computed.OutcomeUnknown, computed.UnknownScope, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := one(t, computed.DefaultBounds, rowKey(0, 0, accChild), computed.RowMeta{}, c.incomplete, c.cs...)
			if r.Outcome != c.want || r.Unknown != c.unknown || r.SomeNotComputed != c.someNot {
				t.Errorf("got (%s, %q, %v), want (%s, %q, %v)", r.Outcome, r.Unknown, r.SomeNotComputed, c.want, c.unknown, c.someNot)
			}
			if c.want == computed.OutcomeDropped && r.Reason != drop.Reason {
				t.Errorf("Reason = %v, want the dropped context's", r.Reason)
			}
		})
	}
}

func TestContextsPerRowCap(t *testing.T) {
	cs := make([]computed.Context, 0, 21)
	for i := range 20 {
		cs = append(cs, ctx(fmt.Sprintf("/a%d", i), computed.OutcomeDropped))
	}
	cs = append(cs, ctx("/b", computed.OutcomeAttached)) // only the 21st attaches
	r := one(t, computed.DefaultBounds, rowKey(0, 0, accChild), computed.RowMeta{}, never, cs...)
	if len(r.Contexts) != 20 || !r.ContextsTruncated {
		t.Fatalf("kept %d, truncated %v; want 20, true", len(r.Contexts), r.ContextsTruncated)
	}
	if r.Contexts[19].ParentMatch.Path.Value != "/a19" {
		t.Errorf("kept contexts not in add order: last is %v", r.Contexts[19].ParentMatch.Path)
	}
	if r.Outcome != computed.OutcomePartial {
		t.Errorf("Outcome = %s, want partial from all 21", r.Outcome)
	}
}

func TestInstallContextCap(t *testing.T) {
	b := computed.DefaultBounds
	b.ContextsPerInstall = 3
	a := newAccumulator(b)
	k0, k1 := rowKey(0, 0, accChild), rowKey(1, 0, accChild)
	a.open(k0, computed.RowMeta{})
	a.open(k1, computed.RowMeta{})
	for _, p := range []string{"/a", "/b", "/c"} {
		a.add(k0, ctx(p, computed.OutcomeAttached))
	}
	a.add(k1, ctx("/d", computed.OutcomeDropped))
	a.add(k1, ctx("/e", computed.OutcomeDropped))
	rows := a.rows(never)
	if len(rows[0].Contexts) != 3 || rows[0].ContextsTruncated {
		t.Errorf("row 0: kept %d, truncated %v; want 3, false", len(rows[0].Contexts), rows[0].ContextsTruncated)
	}
	if len(rows[1].Contexts) != 0 || !rows[1].ContextsTruncated || rows[1].Outcome != computed.OutcomeDropped {
		t.Errorf("row 1: kept %d, truncated %v, outcome %s; want 0, true, dropped",
			len(rows[1].Contexts), rows[1].ContextsTruncated, rows[1].Outcome)
	}
}

func TestRetentionIsStreaming(t *testing.T) {
	b := computed.DefaultBounds
	b.ContextsPerInstall = 100
	a := newAccumulator(b)
	keys := make([]computed.RowKey, 50)
	for i := range keys {
		keys[i] = rowKey(i, 0, accChild)
		a.open(keys[i], computed.RowMeta{})
	}
	for i := range 10_000 {
		a.add(keys[i%len(keys)], ctx(fmt.Sprintf("/p%d", i), computed.OutcomeAttached))
		// Count what the rows hold now, not what the accumulator says it holds.
		live := 0
		for _, r := range a.byKey {
			live += len(r.row.Contexts)
		}
		if live > 100 {
			t.Fatalf("after add %d: %d contexts retained, cap 100", i, live)
		}
	}
	total := 0
	for _, r := range a.rows(never) {
		total += len(r.Contexts)
	}
	if total != 100 {
		t.Errorf("retained %d, want 100", total)
	}
}

func TestDepsAndReportersSurviveRetention(t *testing.T) {
	b := computed.DefaultBounds
	b.ContextsPerRow = 1
	late := ctx("/b", computed.OutcomeAttached)
	other := routeRef("shop", "grandparent")
	late.Reporter, late.Deps = repB, []objects.Ref{other, accParent}
	r := one(t, b, rowKey(0, 0, accChild), computed.RowMeta{}, never, ctx("/a", computed.OutcomeAttached), late)
	if len(r.Contexts) != 1 {
		t.Fatalf("kept %d contexts, want 1", len(r.Contexts))
	}
	if want := []objects.Ref{accChild, other, accParent}; !sameRefs(r.Deps, want) {
		t.Errorf("Deps = %v, want %v", r.Deps, want)
	}
	if len(r.Reporters) != 2 {
		t.Errorf("Reporters = %v, want repA and repB", r.Reporters)
	}
	if _, ok := keyDigest(r, repB); !ok {
		t.Errorf("KeyDigests lacks the dropped context's reporter")
	}
}

func TestUnretainedContextChangesDigest(t *testing.T) {
	build := func(last string) computed.Row {
		cs := make([]computed.Context, 0, 21)
		for i := range 20 {
			c := ctx(fmt.Sprintf("/a%d", i), computed.OutcomeAttached)
			if i%2 == 1 {
				c.Reporter = repB
			}
			cs = append(cs, c)
		}
		cs = append(cs, ctx(last, computed.OutcomeAttached)) // reporter repA, unretained
		return one(t, computed.DefaultBounds, rowKey(0, 0, accChild), computed.RowMeta{}, never, cs...)
	}
	x, y := build("/x"), build("/y")
	if !x.ContextsTruncated {
		t.Fatal("21st context was retained")
	}
	if x.ContextDigest == y.ContextDigest {
		t.Error("ContextDigest ignores the unretained context")
	}
	if kd(x, repA) == kd(y, repA) {
		t.Error("KeyDigests[repA] ignores the unretained context")
	}
	if kd(x, repB) != kd(y, repB) {
		t.Error("KeyDigests[repB] changed with another reporter's context")
	}
}

func TestDigestIsOrderIndependent(t *testing.T) {
	a, b := ctx("/a", computed.OutcomeAttached), ctx("/b", computed.OutcomeDropped)
	x := one(t, computed.DefaultBounds, rowKey(0, 0, accChild), computed.RowMeta{}, never, a, b)
	y := one(t, computed.DefaultBounds, rowKey(0, 0, accChild), computed.RowMeta{}, never, b, a)
	if x.ContextDigest != y.ContextDigest || kd(x, repA) != kd(y, repA) {
		t.Error("digests depend on add order")
	}
}

func TestRowMetaCarried(t *testing.T) {
	meta := computed.RowMeta{RefAsWritten: objects.DelegationRef{Mode: objects.DelegationLabel, Namespace: "shop", Label: "v"},
		Allows: computed.AllowsThis, Unresolved: computed.UnresolvedMissing}
	r := one(t, computed.DefaultBounds, rowKey(0, 0, accChild), meta, never, ctx("/a", computed.OutcomeAttached))
	if r.RefAsWritten != meta.RefAsWritten || r.Allows != meta.Allows || r.Unresolved != meta.Unresolved {
		t.Errorf("got (%v, %s, %s), want %v", r.RefAsWritten, r.Allows, r.Unresolved, meta)
	}
}

func TestTwoRowsOfOneChildAreSeparate(t *testing.T) {
	a := newAccumulator(computed.DefaultBounds)
	k1, k0 := rowKey(1, 0, accChild), rowKey(0, 0, accChild)
	a.open(k1, computed.RowMeta{})
	a.open(k0, computed.RowMeta{})
	a.add(k1, ctx("/a", computed.OutcomeAttached))
	a.add(k0, ctx("/a", computed.OutcomeDropped))
	rows := a.rows(never)
	if len(rows) != 2 || rows[0].Key != k0 || rows[1].Key != k1 {
		t.Fatalf("rows = %v, want (0,0,C) then (1,0,C)", rows)
	}
	if rows[0].Outcome != computed.OutcomeDropped || rows[1].Outcome != computed.OutcomeAttached {
		t.Errorf("outcomes = %s, %s; want dropped, attached", rows[0].Outcome, rows[1].Outcome)
	}
}

func TestUnresolvedRowHasZeroChild(t *testing.T) {
	key := rowKey(0, 0, objects.Ref{})
	r := one(t, computed.DefaultBounds, key, computed.RowMeta{Unresolved: computed.UnresolvedMissing}, func(objects.Ref) bool { return true })
	if r.Key.Child != (objects.Ref{}) || r.Outcome != computed.OutcomeUnresolved || r.Unresolved != computed.UnresolvedMissing {
		t.Errorf("got child %v, outcome %s, unresolved %q", r.Key.Child, r.Outcome, r.Unresolved)
	}
	if len(r.Contexts) != 0 || r.ContextsTruncated {
		t.Errorf("zero-child row has contexts %v, truncated %v", r.Contexts, r.ContextsTruncated)
	}
}

func TestForbiddenRowWithoutContexts(t *testing.T) {
	r := one(t, computed.DefaultBounds, rowKey(0, 0, accChild), computed.RowMeta{Allows: computed.AllowsOther}, never)
	if r.Outcome != computed.OutcomeForbidden {
		t.Errorf("Outcome = %s, want forbidden", r.Outcome)
	}
}

func TestScopeUnknownOpenOnlyRow(t *testing.T) {
	for _, key := range []computed.RowKey{rowKey(0, 0, accChild), rowKey(0, 0, objects.Ref{})} {
		meta := computed.RowMeta{Allows: computed.AllowsOther, Unresolved: computed.UnresolvedMissing, ScopeUnknown: true}
		r := one(t, computed.DefaultBounds, key, meta, never)
		if r.Outcome != computed.OutcomeUnknown || r.Unknown != computed.UnknownScope {
			t.Errorf("%v: got (%s, %q), want unknown, scope-unknown", key.Child, r.Outcome, r.Unknown)
		}
	}
}

func TestUnresolvedRowKeepsContextFacts(t *testing.T) {
	a, b := ctx("/a", computed.OutcomeUnresolved), ctx("/b", computed.OutcomeUnresolved)
	other := routeRef("shop", "grandparent")
	b.Reporter, b.Deps = repB, []objects.Ref{other, accParent}
	always := func(objects.Ref) bool { return true }
	r := one(t, computed.DefaultBounds, rowKey(0, 0, objects.Ref{}), computed.RowMeta{Unresolved: computed.UnresolvedMissing}, always, a, b)
	if r.Outcome != computed.OutcomeUnresolved {
		t.Errorf("Outcome = %s, want unresolved", r.Outcome)
	}
	if len(r.Reporters) != 2 || len(r.KeyDigests) != 2 || r.ContextDigest == 0 {
		t.Errorf("Reporters %v, KeyDigests %v, ContextDigest %d; want both reporters", r.Reporters, r.KeyDigests, r.ContextDigest)
	}
	if want := []objects.Ref{accChild, other, accParent}; !sameRefs(r.Deps, want) {
		t.Errorf("Deps = %v, want %v", r.Deps, want)
	}
	if len(r.Contexts) != 2 {
		t.Errorf("kept %d contexts, want 2", len(r.Contexts))
	}
}

func TestOneContextRowStaysSmall(t *testing.T) {
	r := one(t, computed.DefaultBounds, rowKey(0, 0, accChild), computed.RowMeta{}, never, ctx("/a", computed.OutcomeAttached))
	if cap(r.Contexts) > 2 {
		t.Errorf("cap(Contexts) = %d, want <= 2", cap(r.Contexts))
	}
}

// keyDigest is r's digest under reporter k, and whether k reported.
func keyDigest(r computed.Row, k computed.ReportKey) (uint64, bool) {
	if i, ok := slices.BinarySearchFunc(r.Reporters, k, computed.ReportKey.Compare); ok {
		return r.KeyDigests[i], true
	}
	return 0, false
}

func kd(r computed.Row, k computed.ReportKey) uint64 { d, _ := keyDigest(r, k); return d }
