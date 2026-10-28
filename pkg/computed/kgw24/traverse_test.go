// Port of kgw23 for v2.4.7; keep in sync.

package kgw24

import (
	"reflect"
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil/scale"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

var (
	tGateway = objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "gw"}
	tGwKey   = computed.ReportKey{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "gw"}
)

// hr builds an in-scope HTTPRoute for traversal tests.
type hr struct{ r computed.DelegRoute }

func route23(ns, name string) *hr {
	return &hr{computed.DelegRoute{Ref: routeRef(ns, name), View: &objects.HTTPRouteView{}}}
}

// top makes the route an owned top-level route with a Gateway parentRef.
func (b *hr) top() *hr {
	b.r.Owned, b.r.TopLevel = true, true
	b.r.View.ParentRefs = append(b.r.View.ParentRefs, objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "gw"})
	b.r.View.WrittenRefs = append(b.r.View.WrittenRefs, objects.WrittenRef{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "gw"})
	return b
}

func (b *hr) owned() *hr { b.r.Owned = true; return b }

func (b *hr) hosts(h ...string) *hr { b.r.View.Hostnames = h; return b }

func (b *hr) inherit() *hr { b.r.View.InheritParentMatcher = true; return b }

func (b *hr) label(v string) *hr {
	b.r.View.Labels = map[string]string{objects.DelegationLabelKey: v}
	return b
}

// parentOf adds an explicit HTTPRoute parentRef naming p.
func (b *hr) parentOf(p objects.Ref) *hr {
	b.r.View.ParentRefs = append(b.r.View.ParentRefs, objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: p.Namespace, Name: p.Name})
	b.r.View.WrittenRefs = append(b.r.View.WrittenRefs, objects.WrittenRef{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: p.Namespace, Name: p.Name})
	return b
}

// rule adds a rule with matches ms (nil when omitted) and backendRefs.
func (b *hr) rule(ms []objects.Match, refs ...objects.BackendRef) *hr {
	b.r.View.Rules = append(b.r.View.Rules, objects.Rule{Index: len(b.r.View.Rules), Matches: ms, BackendRefs: refs})
	return b
}

func at(ps ...string) []objects.Match {
	out := make([]objects.Match, 0, len(ps))
	for _, p := range ps {
		out = append(out, prefix(p))
	}
	return out
}

func toName(ns, name string) objects.BackendRef {
	return objects.BackendRef{Ref: routeRef(ns, name), Delegation: &objects.DelegationRef{Mode: objects.DelegationName, Namespace: ns, Name: name}}
}

func toWildcard(ns string) objects.BackendRef {
	return objects.BackendRef{Ref: objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: ns, Name: "*"},
		Delegation: &objects.DelegationRef{Mode: objects.DelegationWildcard, Namespace: ns}}
}

func toLabel(ns, v string) objects.BackendRef {
	return objects.BackendRef{Ref: objects.Ref{Group: "delegation.kgateway.dev", Kind: "label", Namespace: ns, Name: v},
		Delegation: &objects.DelegationRef{Mode: objects.DelegationLabel, Namespace: ns, Label: v}}
}

func service(ns, name string) objects.BackendRef {
	return objects.BackendRef{Ref: objects.Ref{Kind: "Service", Namespace: ns, Name: name}}
}

// world builds a DelegationInput: routes sorted, every top-level route a root
// with deps {Gateway, route}.
func world(bs ...*hr) computed.DelegationInput {
	in := computed.DelegationInput{Wildcard: known(install.WildcardAll), Bounds: computed.DefaultBounds}
	for _, b := range bs {
		in.Routes = append(in.Routes, b.r)
	}
	slices.SortFunc(in.Routes, func(x, y computed.DelegRoute) int { return x.Ref.Compare(y.Ref) })
	for _, r := range in.Routes {
		if r.TopLevel {
			in.Roots = append(in.Roots, computed.Root{Route: r.Ref, Deps: []objects.Ref{tGateway, r.Ref}, Keys: []computed.ReportKey{tGwKey}})
		}
	}
	return in
}

func delegate(in computed.DelegationInput) computed.DelegationResult { return New().Delegation(in) }

// row finds a row or fails.
func row(t *testing.T, res computed.DelegationResult, p *hr, rule, ref int, c *hr) computed.Row {
	t.Helper()
	k := computed.RowKey{Parent: p.r.Ref, Rule: rule, Ref: ref}
	if c != nil {
		k.Child = c.r.Ref
	}
	for _, r := range res.Rows {
		if r.Key == k {
			return r
		}
	}
	t.Fatalf("no row %+v in %v", k, rowKeys(res))
	return computed.Row{}
}

func rowKeys(res computed.DelegationResult) []computed.RowKey {
	var out []computed.RowKey
	for _, r := range res.Rows {
		out = append(out, r.Key)
	}
	return out
}

func hasRow(res computed.DelegationResult, k computed.RowKey) bool {
	return slices.ContainsFunc(res.Rows, func(r computed.Row) bool { return r.Key == k })
}

// ctxOutcomes lists each context's outcome.
func ctxOutcomes(r computed.Row) []computed.RowOutcome {
	var out []computed.RowOutcome
	for _, c := range r.Contexts {
		out = append(out, c.Outcome)
	}
	return out
}

func signal(t *testing.T, res computed.DelegationResult, p *hr, key computed.ReportKey) computed.ParentSignal {
	t.Helper()
	for _, s := range res.Signals {
		if s.Parent == p.r.Ref && s.Key == key {
			return s
		}
	}
	t.Fatalf("no signal for %v %+v in %+v", p.r.Ref, key, res.Signals)
	return computed.ParentSignal{}
}

// under is the 2.4 reporter key of a child of p with an empty or full parentRef.
func under(p *hr) computed.ReportKey {
	return computed.ReportKey{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: p.r.Ref.Namespace, Name: p.r.Ref.Name}
}

func pathOf(c computed.Context) []string {
	var out []string
	for _, s := range c.Path.Steps() {
		out = append(out, s.Route.Name+" "+pathValue(s.Match))
	}
	return out
}

func TestAncestorPrunedParentMatch(t *testing.T) {
	c := route23("shop", "c").rule(at("/other/item"), service("shop", "be"))
	p := route23("shop", "p").rule(at("/api", "/other"), toName("shop", "c"))
	r := route23("shop", "r").top().rule(at("/api"), toName("shop", "p"))
	res := delegate(world(r, p, c))

	if got := row(t, res, r, 0, 0, p); got.Outcome != computed.OutcomeAttached {
		t.Fatalf("R→P %s", got.Outcome)
	}
	got := row(t, res, p, 0, 0, c)
	if len(got.Contexts) != 1 || got.Outcome != computed.OutcomeDropped {
		t.Fatalf("P→C %s with %d contexts", got.Outcome, len(got.Contexts))
	}
	cx := got.Contexts[0]
	if pathValue(cx.ParentMatch) != "/api" || cx.Reason == nil || cx.Reason.Clause != computed.ClausePathPrefix {
		t.Fatalf("context %+v reason %+v", cx.ParentMatch, cx.Reason)
	}
	if s := signal(t, res, p, under(r)); !slices.Equal(s.Reasons, []string{computed.ReasonBackendNotFound}) {
		t.Fatalf("P signal %v", s.Reasons)
	}
}

func TestOneChildTwoMatchesOneRule(t *testing.T) {
	c := route23("shop", "c").rule(at("/api/x"), service("shop", "be"))
	p := route23("shop", "p").top().rule(at("/api", "/v2"), toName("shop", "c"))
	res := delegate(world(p, c))
	got := row(t, res, p, 0, 0, c)
	if got.Outcome != computed.OutcomePartial || !slices.Equal(ctxOutcomes(got), []computed.RowOutcome{computed.OutcomeAttached, computed.OutcomeDropped}) {
		t.Fatalf("row %s %v", got.Outcome, ctxOutcomes(got))
	}
	// The /v2 context keeps no child, so P gets BackendNotFound (D-021 item 9).
	if s := signal(t, res, p, tGwKey); !slices.Equal(s.Reasons, []string{computed.ReasonBackendNotFound}) {
		t.Fatalf("P signal %v", s.Reasons)
	}
}

func TestOneChildTwoRules(t *testing.T) {
	c := route23("shop", "c").rule(at("/api/x"), service("shop", "be"))
	p := route23("shop", "p").top().rule(at("/v2"), toName("shop", "c")).rule(at("/api"), toName("shop", "c"))
	res := delegate(world(p, c))
	if got := row(t, res, p, 0, 0, c).Outcome; got != computed.OutcomeDropped {
		t.Fatalf("rule 0: %s", got)
	}
	if got := row(t, res, p, 1, 0, c).Outcome; got != computed.OutcomeAttached {
		t.Fatalf("rule 1: %s", got)
	}
}

func TestHostnameChild(t *testing.T) {
	fits := route23("shop", "fits").hosts("a.example.com").rule(at("/api/x"), service("shop", "be"))
	fails := route23("shop", "fails").hosts("a.example.com").rule(at("/other"), service("shop", "be"))
	p := route23("shop", "p").top().rule(at("/api"), toName("shop", "fits")).rule(at("/api"), toName("shop", "fails"))
	res := delegate(world(p, fits, fails))
	if got := row(t, res, p, 0, 0, fits).Outcome; got != computed.OutcomeRejectedHostnames {
		t.Fatalf("fits: %s", got)
	}
	// Hostnames are checked after the fit (delegation.go V4:83-91).
	if got := row(t, res, p, 1, 0, fails).Outcome; got != computed.OutcomeDropped {
		t.Fatalf("fails: %s", got)
	}
	if s := signal(t, res, p, tGwKey); !slices.Equal(s.Reasons, []string{computed.ReasonBackendNotFound}) {
		t.Fatalf("P signal %v", s.Reasons)
	}
}

func TestCycle(t *testing.T) {
	// B's wildcard selects A (on the path), B itself (on the path) and S.
	a := route23("loop", "a").top().rule(at("/a"), toName("loop", "b"))
	b := route23("loop", "b").rule(at("/a/b"), toWildcard("loop"))
	s := route23("loop", "s").rule(at("/a/b/s"), service("loop", "be"))
	res := delegate(world(a, b, s))
	for _, c := range []*hr{b, s} {
		if got := row(t, res, b, 0, 0, c); got.Outcome != computed.OutcomeCycle {
			t.Fatalf("B→%s: %s", c.r.Ref.Name, got.Outcome)
		}
	}
	// A is top-level, so its Gateway parentRef forbids B from selecting it.
	if got := row(t, res, b, 0, 0, a).Outcome; got != computed.OutcomeForbidden {
		t.Fatalf("B→A: %s", got)
	}
	if got := signal(t, res, b, under(a)); !slices.Equal(got.Reasons, []string{computed.ReasonRefNotPermitted}) {
		t.Fatalf("B signal %v", got.Reasons)
	}
	// A cycle through a child that permits any parent.
	a2 := route23("loop2", "a").top().rule(at("/a"), toName("loop2", "b"))
	b2 := route23("loop2", "b").rule(at("/a"), toName("loop2", "c"))
	c2 := route23("loop2", "c").parentOf(b2.r.Ref).rule(at("/a"), toName("loop2", "b"))
	res = delegate(world(a2, b2, c2))
	if got := row(t, res, c2, 0, 0, b2).Outcome; got != computed.OutcomeCycle {
		t.Fatalf("C→B: %s", got)
	}
	if got := signal(t, res, c2, under(b2)); !slices.Equal(got.Reasons, []string{computed.ReasonRefNotPermitted}) {
		t.Fatalf("C signal %v", got.Reasons)
	}
}

func TestDiamond(t *testing.T) {
	c := route23("d", "c").rule(at("/x/c"), service("d", "be"))
	b1 := route23("d", "b1").rule(at("/x"), toName("d", "c"))
	b2 := route23("d", "b2").rule(at("/x"), toName("d", "c"))
	a := route23("d", "a").top().rule(at("/x"), toName("d", "b1")).rule(at("/x"), toName("d", "b2"))
	res := delegate(world(a, b1, b2, c))
	for _, b := range []*hr{b1, b2} {
		got := row(t, res, b, 0, 0, c)
		if got.Outcome != computed.OutcomeAttached || len(got.Contexts) != 1 {
			t.Fatalf("%s→C %s with %d contexts", b.r.Ref.Name, got.Outcome, len(got.Contexts))
		}
		// C's key is under its own parent; the context's reporter is that parent's key under A.
		if got.ChildKey != under(b) || !got.WritesEntry {
			t.Fatalf("%s→C key %+v writes %v", b.r.Ref.Name, got.ChildKey, got.WritesEntry)
		}
		if got.Contexts[0].Reporter != under(a) {
			t.Fatalf("%s→C reporter %+v", b.r.Ref.Name, got.Contexts[0].Reporter)
		}
	}
	if got := row(t, res, a, 0, 0, b1).Contexts[0].Reporter; got != tGwKey {
		t.Fatalf("root reporter %+v", got)
	}
}

func TestQuirks(t *testing.T) {
	// D-021 item 7. Quirk 1: refs a, b in one rule; ref 1 carries a's children too.
	qa := route23("q", "c7a").rule(at("/q1/a"), service("q", "be"))
	qb := route23("q", "c7b").rule(at("/q1/b"), service("q", "be"))
	// Quirk 2: c7c under /q1 (rule 0) and /q2 (rule 1); c7d only under /q2, after c7c's key is overwritten.
	qc := route23("q", "c7c").rule(at("/q1/c", "/q2/c"), service("q", "be"))
	qd := route23("q", "c7d").rule(at("/q1/d"), service("q", "be"))
	p := route23("q", "api").top().
		rule(at("/q1"), toName("q", "c7a"), toName("q", "c7b")).
		rule(at("/q1"), toName("q", "c7c")).
		rule(at("/q2"), toName("q", "c7d"), toName("q", "c7c"))
	res := delegate(world(p, qa, qb, qc, qd))

	if got := row(t, res, p, 0, 1, qa).Outcome; got != computed.OutcomeAttached {
		t.Fatalf("quirk 1: ref 1 carries c7a: %s", got)
	}
	if got := row(t, res, p, 0, 0, qa).Outcome; got != computed.OutcomeAttached {
		t.Fatalf("c7a under ref 0: %s", got)
	}
	if hasRow(res, computed.RowKey{Parent: p.r.Ref, Rule: 0, Ref: 0, Child: qb.r.Ref}) {
		t.Fatal("ref 0 must not carry c7b")
	}
	// Rule 1's c7c key holds rule 2's list [c7d, c7c], so c7d attaches under /q1.
	if got := row(t, res, p, 1, 0, qd).Outcome; got != computed.OutcomeAttached {
		t.Fatalf("quirk 2: c7d under /q1: %s", got)
	}
	if got := row(t, res, p, 2, 0, qd).Outcome; got != computed.OutcomeDropped {
		t.Fatalf("c7d under /q2: %s", got)
	}
	if got := row(t, res, p, 2, 1, qc).Outcome; got != computed.OutcomeAttached {
		t.Fatalf("c7c under /q2: %s", got)
	}
	if s := signal(t, res, p, tGwKey); !slices.Equal(s.Reasons, []string{computed.ReasonBackendNotFound}) {
		t.Fatalf("P signal %v", s.Reasons)
	}
}

func TestNotReached(t *testing.T) {
	c := route23("shop", "c").rule(at("/o/c"), service("shop", "be"))
	o := route23("shop", "o").owned().rule(at("/o"), toName("shop", "c"))
	res := delegate(world(o, c))
	got := row(t, res, o, 0, 0, c)
	if got.Outcome != computed.OutcomeUnknown || got.Unknown != computed.UnknownNotReached || got.Allows != computed.AllowsAny ||
		got.RefAsWritten != *toName("shop", "c").Delegation {
		t.Fatalf("row %+v", got)
	}
	// A non-owned unreached parent has no rows.
	n := route23("shop", "n").rule(at("/n"), toName("shop", "c"))
	if res := delegate(world(n, c)); len(res.Rows) != 0 {
		t.Fatalf("rows %v", rowKeys(res))
	}
}

func TestThreeLevelMergedContext(t *testing.T) {
	g := route23("m", "g").rule(at("/api/v1/g"), service("m", "be"))
	c := route23("m", "c").inherit().rule(at("/v1"), toName("m", "g"))
	r := route23("m", "r").top().rule(at("/api"), toName("m", "c"))
	res := delegate(world(r, c, g))
	got := row(t, res, c, 0, 0, g)
	if got.Outcome != computed.OutcomeAttached || pathValue(got.Contexts[0].ParentMatch) != "/api/v1" {
		t.Fatalf("grandchild %s under %q", got.Outcome, pathValue(got.Contexts[0].ParentMatch))
	}
}

func TestUnresolvedRef(t *testing.T) {
	r := route23("shop", "r").top().rule(at("/api"), toName("shop", "missing"))
	res := delegate(world(r))
	got := row(t, res, r, 0, 0, nil)
	if got.Outcome != computed.OutcomeUnresolved || got.Unresolved != computed.UnresolvedMissing || len(got.Contexts) != 1 {
		t.Fatalf("row %+v", got)
	}
	if s := signal(t, res, r, tGwKey); !slices.Equal(s.Reasons, []string{computed.ReasonBackendNotFound}) {
		t.Fatalf("signal %v", s.Reasons)
	}
}

func TestRootReportsUnderAcceptedKeysOnly(t *testing.T) {
	// The root has two Gateway parentRefs; view A accepted only the first (Root.Keys).
	other := computed.ReportKey{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "gw2"}
	r := route23("rk", "r").top().rule(at("/api"), toName("rk", "missing"))
	r.r.View.ParentRefs = append(r.r.View.ParentRefs, objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "gw2"})
	r.r.View.WrittenRefs = append(r.r.View.WrittenRefs, objects.WrittenRef{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "gw2"})
	unknownKey := computed.ReportKey{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: "gw3"}
	in := world(r)
	in.Roots[0].UncertainKeys = []computed.ReportKey{unknownKey}
	res := delegate(in)
	if len(res.Signals) != 1 || res.Signals[0].Key != tGwKey {
		t.Fatalf("signals %+v, want only under %+v", res.Signals, tGwKey)
	}
	if got := row(t, res, r, 0, 0, nil).Reporters; !slices.Equal(got, []computed.ReportKey{tGwKey}) || slices.Contains(got, other) {
		t.Fatalf("reporters %+v", got)
	}
	want := []computed.RouteKey{{Route: r.r.Ref, Key: unknownKey}}
	if !reflect.DeepEqual(res.UncertainKeys, want) {
		t.Fatalf("uncertain keys %+v, want %+v", res.UncertainKeys, want)
	}
}

func TestForbiddenOnPathIsNoCycle(t *testing.T) {
	// The forbidden check runs before the cycle check (query/route.go V4:266-276).
	refs := []refSel{{rule: 0, ref: 0, key: refKey{name: "x"}, kids: []int32{0, 1}, allows: []computed.Allows{computed.AllowsOther, computed.AllowsAny}}}
	got := selectTree(refs, func(c int32) bool { return c == 0 })
	if got[0].err != errNone || !slices.Equal(got[0].kids, []int32{1}) {
		t.Fatalf("got %+v, want kids [1] and no error", got[0])
	}
	// End to end: B selects its top-level ancestor A, which forbids it.
	a := route23("fp", "a").top().rule(at("/a"), toName("fp", "b"))
	b := route23("fp", "b").rule(at("/a"), toName("fp", "a"))
	res := delegate(world(a, b))
	if got := row(t, res, b, 0, 0, a).Outcome; got != computed.OutcomeForbidden {
		t.Fatalf("B→A %s", got)
	}
	if s := signal(t, res, b, under(a)); !slices.Equal(s.Reasons, []string{computed.ReasonBackendNotFound}) {
		t.Fatalf("B signal %v", s.Reasons)
	}
}

func TestCutOpensQuirkRows(t *testing.T) {
	// Rule 0's ref 1 carries ref 0's child (quirk 1); the context cap cuts ref 1.
	k1 := route23("qc", "k1").rule(at("/r/1"), service("qc", "be"))
	k2 := route23("qc", "k2").rule(at("/r/2"), service("qc", "be"))
	r := route23("qc", "r").top().rule(at("/r"), toName("qc", "k1"), toName("qc", "k2"))
	in := world(r, k1, k2)
	in.Bounds.Contexts = 1
	res := delegate(in)
	for _, c := range []*hr{k1, k2} {
		if got := row(t, res, r, 0, 1, c); got.Outcome != computed.OutcomeUnknown || got.Unknown != computed.UnknownTooLarge || len(got.Contexts) != 0 || !got.ContextsTruncated {
			t.Fatalf("R ref 1 → %s: %s %s, %d contexts", c.r.Ref.Name, got.Outcome, got.Unknown, len(got.Contexts))
		}
	}
}

func TestCutContextsNotRetained(t *testing.T) {
	// Every flatten call after the first is cut: rows stay too-large with deps
	// and reporters, but no cut context is retained.
	k1 := route23("nr", "k1").rule(at("/r/1"), service("nr", "be"))
	k2 := route23("nr", "k2").rule(at("/r/2"), service("nr", "be"))
	r := route23("nr", "r").top().rule(at("/r"), toName("nr", "k1"), toName("nr", "k2")).rule(at("/r"), toName("nr", "k1"))
	in := world(r, k1, k2)
	in.Bounds.Contexts = 1
	w := &walker{in: &in, b: withDefaults(in.Bounds), routes: in.Routes, sigIdx: map[sigKey]int{}}
	w.acc = newAccumulator(w.b)
	w.selectAll()
	w.walkRoots()
	if w.acc.kept != 1 {
		t.Fatalf("retained %d contexts, want only the computed one", w.acc.kept)
	}
	res := delegate(in)
	for _, k := range []computed.RowKey{{Parent: r.r.Ref, Rule: 0, Ref: 1, Child: k1.r.Ref}, {Parent: r.r.Ref, Rule: 1, Ref: 0, Child: k1.r.Ref}} {
		got := row(t, res, r, k.Rule, k.Ref, map[objects.Ref]*hr{k1.r.Ref: k1, k2.r.Ref: k2}[k.Child])
		if got.Outcome != computed.OutcomeUnknown || got.Unknown != computed.UnknownTooLarge || len(got.Contexts) != 0 ||
			!got.ContextsTruncated || len(got.Deps) == 0 || !slices.Equal(got.Reporters, []computed.ReportKey{tGwKey}) {
			t.Fatalf("cut row %+v", got)
		}
	}
}

func TestComputeCaps(t *testing.T) {
	leaf := func(ns, name, path string) *hr { return route23(ns, name).rule(at(path), service(ns, "be")) }
	run := func(t *testing.T, b computed.Bounds, bs ...*hr) computed.DelegationResult {
		t.Helper()
		in := world(bs...)
		in.Bounds = b
		res := delegate(in)
		if again := delegate(in); !reflect.DeepEqual(res, again) {
			t.Fatal("two runs differ")
		}
		return res
	}
	cut := func(t *testing.T, res computed.DelegationResult, p *hr, rule, ref int) {
		t.Helper()
		want := computed.Uncertain{Route: p.r.Ref, Rule: rule, Ref: ref, Reason: computed.UnknownTooLarge}
		if !slices.Contains(res.Uncertain, want) {
			t.Fatalf("no cut %+v in %+v", want, res.Uncertain)
		}
	}
	tooLarge := func(t *testing.T, r computed.Row) {
		t.Helper()
		if r.Outcome != computed.OutcomeUnknown || r.Unknown != computed.UnknownTooLarge {
			t.Fatalf("row %+v: %s %s", r.Key, r.Outcome, r.Unknown)
		}
	}
	t.Run("edges", func(t *testing.T) {
		// Selection runs in route order: P (1 edge), then R's rule 0 (1) and rule 1 (would be 3).
		x, k := leaf("a", "x", "/r/p/x"), leaf("a", "k", "/r/k")
		p := route23("a", "p").owned().rule(at("/r/p"), toName("a", "x"))
		r := route23("b", "r").top().rule(at("/r"), toName("a", "k")).rule(at("/r"), toName("a", "p"))
		res := run(t, computed.Bounds{Edges: 2}, p, x, k, r)
		if res.Cap != computed.CapEdges {
			t.Fatalf("cap %q", res.Cap)
		}
		cut(t, res, r, 1, 0)
		tooLarge(t, row(t, res, p, 0, 0, x))
		if got := row(t, res, r, 0, 0, k).Outcome; got != computed.OutcomeAttached {
			t.Fatalf("R→K %s", got)
		}
	})
	t.Run("contexts", func(t *testing.T) {
		g := leaf("c", "g", "/r/g")
		k1 := route23("c", "k1").rule(at("/r"), toName("c", "g"))
		k2 := leaf("c", "k2", "/r/k2")
		r := route23("c", "r").top().rule(at("/r"), toName("c", "k1")).rule(at("/r"), toName("c", "k2"))
		res := run(t, computed.Bounds{Contexts: 2}, r, k1, k2, g)
		if res.Cap != computed.CapContexts {
			t.Fatalf("cap %q", res.Cap)
		}
		cut(t, res, r, 1, 0)
		tooLarge(t, row(t, res, r, 1, 0, k2))
		if row(t, res, r, 0, 0, k1).Outcome != computed.OutcomeAttached || row(t, res, k1, 0, 0, g).Outcome != computed.OutcomeAttached {
			t.Fatal("rows before the cut must stay computed")
		}
	})
	t.Run("matchers", func(t *testing.T) {
		k1 := route23("m", "k1").rule(at("/r/1", "/r/2"), service("m", "be"))
		k2 := route23("m", "k2").rule(at("/r/3", "/r/4"), service("m", "be"))
		k3 := leaf("m", "k3", "/r/5")
		r := route23("m", "r").top().rule(at("/r"), toName("m", "k1")).rule(at("/r"), toName("m", "k2")).rule(at("/r"), toName("m", "k3"))
		res := run(t, computed.Bounds{Matchers: 4}, r, k1, k2, k3)
		if res.Cap != computed.CapMatchers {
			t.Fatalf("cap %q", res.Cap)
		}
		cut(t, res, r, 2, 0)
		tooLarge(t, row(t, res, r, 2, 0, k3))
		if row(t, res, r, 1, 0, k2).Outcome != computed.OutcomeAttached {
			t.Fatal("K2 fits the cap exactly")
		}
		// A cut evaluation is no evidence of a failure.
		for _, s := range res.Signals {
			if s.Parent == r.r.Ref {
				t.Fatalf("signal %+v", s)
			}
		}
	})
	t.Run("depth", func(t *testing.T) {
		// A path of more than Depth routes is cut there; other paths go on.
		e := leaf("dep", "e", "/d/b/c/e")
		c := route23("dep", "c").rule(at("/d/b/c"), toName("dep", "e"))
		b := route23("dep", "b").rule(at("/d/b"), toName("dep", "c"))
		a := route23("dep", "a").top().rule(at("/d"), toName("dep", "b"))
		y := leaf("dep", "y", "/z/y")
		z := route23("dep", "z").top().rule(at("/z"), toName("dep", "y"))
		res := run(t, computed.Bounds{Depth: 2}, a, b, c, e, y, z)
		if res.Cap != computed.CapDepth {
			t.Fatalf("cap %q", res.Cap)
		}
		cut(t, res, c, 0, 0)
		tooLarge(t, row(t, res, c, 0, 0, e))
		if r := row(t, res, c, 0, 0, e); len(r.Contexts) != 0 {
			t.Fatalf("cut row retains %d contexts", len(r.Contexts))
		}
		if row(t, res, b, 0, 0, c).Outcome != computed.OutcomeAttached || row(t, res, z, 0, 0, y).Outcome != computed.OutcomeAttached {
			t.Fatal("rows within the depth must stay computed")
		}
	})
}

func TestContextDeps(t *testing.T) {
	c := route23("dp", "c").rule(at("/a/b/c"), service("dp", "be"))
	b := route23("dp", "b").rule(at("/a/b"), toName("dp", "c"))
	a := route23("dp", "a").top().rule(at("/a"), toName("dp", "b"))
	res := delegate(world(a, b, c))
	want := []objects.Ref{tGateway, a.r.Ref, b.r.Ref}
	slices.SortFunc(want, compareRef)
	got := row(t, res, b, 0, 0, c)
	if !reflect.DeepEqual(got.Contexts[0].Deps, want) || !reflect.DeepEqual(got.Deps, want) {
		t.Fatalf("deps %v, row %v, want %v", got.Contexts[0].Deps, got.Deps, want)
	}
}

func TestReachGraphIgnoresFit(t *testing.T) {
	// D is not owned and dropped under R; O is owned and unreached; K is cut by the context cap.
	e := route23("rg", "e").rule(at("/elsewhere"), service("rg", "be"))
	d := route23("rg", "d").rule(at("/other"), toName("rg", "e"))
	l := route23("rg", "l").rule(at("/r/k/l"), service("rg", "be"))
	k := route23("rg", "k").rule(at("/r/k"), toName("rg", "l"))
	x := route23("rg", "x").rule(at("/x"), service("rg", "be"))
	o := route23("rg", "o").owned().rule(at("/o"), toName("rg", "x"))
	f := route23("rg", "f").parentOf(routeRef("rg", "someone")).rule(at("/r/f"), service("rg", "be"))
	r := route23("rg", "r").top().rule(at("/r"), toName("rg", "d"), toName("rg", "f")).rule(at("/r"), toName("rg", "k"))
	in := world(r, d, e, k, l, o, x, f)
	in.Bounds.Contexts = 3 // R rule 0 twice and rule 1: K's own ref is cut
	res := delegate(in)
	if res.Cap != computed.CapContexts {
		t.Fatalf("cap %q", res.Cap)
	}
	for _, want := range []computed.Edge{{Parent: d.r.Ref, Child: e.r.Ref}, {Parent: o.r.Ref, Child: x.r.Ref}, {Parent: k.r.Ref, Child: l.r.Ref}, {Parent: r.r.Ref, Child: d.r.Ref}} {
		if !slices.Contains(res.Reach, want) {
			t.Fatalf("no edge %v in %v", want, res.Reach)
		}
	}
	// A forbidden child is no edge.
	if slices.Contains(res.Reach, computed.Edge{Parent: r.r.Ref, Child: f.r.Ref}) {
		t.Fatal("forbidden child in Reach")
	}
	if !slices.IsSortedFunc(res.Reach, func(a, b computed.Edge) int { return cmpEdge(a, b) }) {
		t.Fatal("Reach not sorted")
	}
}

func cmpEdge(a, b computed.Edge) int {
	if c := a.Parent.Compare(b.Parent); c != 0 {
		return c
	}
	return a.Child.Compare(b.Child)
}

// An unknown wildcard setting (rules §3.3, §5.2): a label ref selects its own
// namespace's routes with certainty; routes elsewhere stay possible, with no
// edge. The ref keeps a zero-child row, unknown and setting-unknown, and no
// context of it records a delegation failure, since a possible child may fit.
func TestUnknownWildcardSelectsOwnNamespace(t *testing.T) {
	s1 := route23("shop", "s1").label("v").rule(at("/r/s1"), service("shop", "be"))
	s2 := route23("shop", "s2").label("v").rule(at("/x"), service("shop", "be"))
	t1 := route23("team", "t1").label("v").rule(at("/r/t1"), service("team", "be"))
	r := route23("shop", "r").top().rule(at("/r"), toLabel("shop", "v"))
	in := world(r, s1, s2, t1)
	in.Wildcard = install.Setting{} // valueFrom: unknown
	res := delegate(in)
	for _, e := range res.Reach {
		if e.Parent == r.r.Ref && e.Child == t1.r.Ref {
			t.Fatalf("edge %v to a possible child", e)
		}
	}
	if !slices.Contains(res.Reach, computed.Edge{Parent: r.r.Ref, Child: s1.r.Ref}) {
		t.Fatalf("no edge to the own-namespace child: %v", res.Reach)
	}
	for _, want := range []computed.Uncertain{
		{Route: r.r.Ref, Rule: 0, Ref: 0, Reason: computed.UnknownSetting},
		{Route: t1.r.Ref, Rule: computed.UncertainPossible, Ref: -1, Reason: computed.UnknownSetting},
	} {
		if !slices.Contains(res.Uncertain, want) {
			t.Fatalf("no %+v in %+v", want, res.Uncertain)
		}
	}
	if slices.ContainsFunc(res.Uncertain, func(u computed.Uncertain) bool { return u.Route == s1.r.Ref }) {
		t.Fatalf("own-namespace child is uncertain: %+v", res.Uncertain)
	}
	if got := row(t, res, r, 0, 0, s1); got.Outcome != computed.OutcomeAttached {
		t.Fatalf("s1 %s", got.Outcome)
	}
	// s2 is dropped in its only context, but r is uncertain, so it is never dropped (rules §5.5).
	if got := row(t, res, r, 0, 0, s2); got.Outcome != computed.OutcomeUnknown {
		t.Fatalf("s2 %s", got.Outcome)
	}
	if got := row(t, res, r, 0, 0, nil); got.Outcome != computed.OutcomeUnknown || got.Unknown != computed.UnknownSetting {
		t.Fatalf("zero-child row %s %s", got.Outcome, got.Unknown)
	}

	// Only dropped own children: still no BackendNotFound.
	only := route23("shop", "r").top().rule(at("/r"), toLabel("shop", "v"))
	in = world(only, s2, t1)
	in.Wildcard = install.Setting{}
	res = delegate(in)
	if len(res.Signals) != 0 {
		t.Fatalf("signals %+v from an uncertain selection", res.Signals)
	}
	if got := row(t, res, only, 0, 0, nil); got.Outcome != computed.OutcomeUnknown || got.Unknown != computed.UnknownSetting {
		t.Fatalf("zero-child row %s %s", got.Outcome, got.Unknown)
	}
}

// A possible child on the path may fail the ref with a cycle, so its own
// children are unknown there, never attached or dropped (rules §3.3, §5.4).
func TestUnknownWildcardPossibleChildOnPath(t *testing.T) {
	s1 := route23("shop", "s1").label("v").rule(at("/x/s1"), service("shop", "be"))
	x := route23("lab", "x").label("v").rule(at("/x"), toLabel("shop", "v"))
	r := route23("infra", "r").top().rule(at("/x"), toName("lab", "x"))
	in := world(r, x, s1)
	in.Wildcard = install.Setting{}
	res := delegate(in)
	if got := row(t, res, x, 0, 0, s1); got.Outcome != computed.OutcomeUnknown || got.Unknown != computed.UnknownSetting {
		t.Fatalf("x→s1 %s %s, want unknown setting-unknown", got.Outcome, got.Unknown)
	}
}

func TestPathCarriesDelegatingMatches(t *testing.T) {
	g := route23("pc", "g").rule(at("/api/v1/g"), service("pc", "be"))
	c := route23("pc", "c").inherit().rule(at("/v1"), toName("pc", "g"))
	r := route23("pc", "r").top().rule(at("/api"), toName("pc", "c"))
	res := delegate(world(r, c, g))
	cx := row(t, res, c, 0, 0, g).Contexts[0]
	if want := []string{"r /api", "c /api/v1"}; !slices.Equal(pathOf(cx), want) {
		t.Fatalf("path %v, want %v", pathOf(cx), want)
	}
	if !reflect.DeepEqual(cx.Path.Last().Match, cx.ParentMatch) {
		t.Fatal("the last step's match must equal ParentMatch")
	}
	if got := pathOf(row(t, res, r, 0, 0, c).Contexts[0]); !slices.Equal(got, []string{"r /api"}) {
		t.Fatalf("root path %v", got)
	}
}

func TestScopeUnknownRowsUnknown(t *testing.T) {
	c := route23("su", "c").rule(at("/a/c"), service("su", "be"))
	o := route23("su", "o").owned().rule(at("/o"), toName("su", "missing"))
	r := route23("su", "r").top().rule(at("/a"), toName("su", "c"))
	in := world(r, c, o)
	in.ScopeUnknown = true
	res := delegate(in)
	if len(res.Rows) != 2 {
		t.Fatalf("rows %v", rowKeys(res))
	}
	for _, rw := range res.Rows {
		if rw.Outcome != computed.OutcomeUnknown || rw.Unknown != computed.UnknownScope {
			t.Fatalf("row %+v: %s %s", rw.Key, rw.Outcome, rw.Unknown)
		}
	}
	want := computed.Uncertain{Route: r.r.Ref, Rule: computed.UncertainRoot, Ref: -1, Reason: computed.UnknownScope}
	if !slices.Contains(res.Uncertain, want) || len(res.Signals) != 0 {
		t.Fatalf("uncertain %+v signals %+v", res.Uncertain, res.Signals)
	}
}

// delegationInput builds tier t's views B and C input from the scale world.
func delegationInput(tb testing.TB, t scale.Tier) computed.DelegationInput {
	return inputOf(tb, scale.World(t, 1))
}

// inputOf builds views B and C input from objs' HTTPRoutes: every one owned,
// each with a Gateway or ListenerSet parent a root reporting under all of them.
func inputOf(tb testing.TB, objs []*unstructured.Unstructured) computed.DelegationInput {
	in := computed.DelegationInput{Wildcard: known(install.WildcardAll), Bounds: computed.DefaultBounds}
	for _, u := range objs {
		if u.GetKind() != "HTTPRoute" {
			continue
		}
		v := objects.DecodeHTTPRoute(u)
		r := computed.DelegRoute{Ref: objects.RefOf(u), Generation: u.GetGeneration(), View: v, Owned: true}
		var deps []objects.Ref
		var keys []computed.ReportKey
		for i, p := range v.ParentRefs {
			if p.Kind != "HTTPRoute" {
				r.TopLevel = true
				deps = append(deps, p.Ref())
				if k := computed.KeyOf(v.WrittenRefs[i]); !slices.Contains(keys, k) {
					keys = append(keys, k)
				}
			}
		}
		in.Routes = append(in.Routes, r)
		if r.TopLevel {
			in.Roots = append(in.Roots, computed.Root{Route: r.Ref, Deps: append(deps, r.Ref), Keys: keys})
		}
	}
	slices.SortFunc(in.Routes, func(x, y computed.DelegRoute) int { return x.Ref.Compare(y.Ref) })
	slices.SortFunc(in.Roots, func(x, y computed.Root) int { return x.Route.Compare(y.Route) })
	if len(in.Roots) == 0 {
		tb.Fatal("empty scale world")
	}
	return in
}

func benchmarkDelegation(b *testing.B, t scale.Tier) {
	in := delegationInput(b, t)
	res := New().Delegation(in)
	b.ReportMetric(float64(len(res.Rows)), "rows")
	b.ReportAllocs()
	for b.Loop() {
		New().Delegation(in)
	}
}

func BenchmarkDelegationLarge(b *testing.B)   { benchmarkDelegation(b, scale.Large) }
func BenchmarkDelegationExtreme(b *testing.B) { benchmarkDelegation(b, scale.Extreme) }

// incomplete fails unless each route is in a reach closure.
func incomplete(t *testing.T, res computed.DelegationResult, want bool, bs ...*hr) {
	t.Helper()
	for _, b := range bs {
		if got := res.Complete.Incomplete(b.r.Ref); got != want {
			t.Fatalf("%s: incomplete %v, want %v", b.r.Ref.Name, got, want)
		}
	}
}

func TestClosureFollowsReach(t *testing.T) {
	// Roots a and r. Q drops C, so C has a complete dropped row; P's ref to C is cut.
	v := route23("cf", "v").rule(at("/a/v"), service("cf", "be"))
	a := route23("cf", "a").top().rule(at("/a"), toName("cf", "v"))
	g := route23("cf", "g").rule(at("/x/g"), service("cf", "be"))
	c := route23("cf", "c").rule(at("/x"), toName("cf", "g"))
	q := route23("cf", "q").rule(at("/r/q"), toName("cf", "c"))
	p := route23("cf", "p").rule(at("/r/p"), toName("cf", "c"))
	r := route23("cf", "r").top().rule(at("/r"), toName("cf", "q")).rule(at("/r"), toName("cf", "p"))
	in := world(a, v, r, q, p, c, g)
	in.Bounds.Contexts = 4 // a→v, r→q, q→c, r→p; p→c is cut
	res := delegate(in)
	if !slices.Contains(res.Uncertain, computed.Uncertain{Route: p.r.Ref, Rule: 0, Ref: 0, Reason: computed.UnknownTooLarge}) {
		t.Fatalf("no cut at P: %+v", res.Uncertain)
	}
	incomplete(t, res, true, p, c, g)
	incomplete(t, res, false, a, v, r, q)
	if got := row(t, res, q, 0, 0, c); got.Outcome != computed.OutcomeUnknown || got.Unknown != computed.UnknownContext {
		t.Fatalf("Q→C %s %s, want unknown: C is in a reach closure", got.Outcome, got.Unknown)
	}
}

func TestDroppedNonOwnedRouteInClosure(t *testing.T) {
	// rules §11.5: D is not owned and dropped under R; P's ref to D is cut, so E below D is incomplete.
	e := route23("dn", "e").owned().rule(at("/other/e"), service("dn", "be"))
	d := route23("dn", "d").rule(at("/other"), toName("dn", "e"))
	p := route23("dn", "p").owned().rule(at("/r/p"), toName("dn", "d"))
	r := route23("dn", "r").top().rule(at("/r"), toName("dn", "d")).rule(at("/r"), toName("dn", "p"))
	in := world(r, p, d, e)
	in.Bounds.Contexts = 2 // r→d, r→p; p→d is cut
	res := delegate(in)
	incomplete(t, res, true, d, e)
	if got := row(t, res, r, 0, 0, d); got.Outcome != computed.OutcomeUnknown {
		t.Fatalf("R→D %s, want unknown", got.Outcome)
	}
}

func TestEdgeCapMakesEverythingIncomplete(t *testing.T) {
	x := route23("ec", "x").rule(at("/elsewhere"), service("ec", "be"))
	k := route23("ec", "k").rule(at("/r/k"), service("ec", "be"))
	u := route23("ec", "u").rule(at("/r/u"), service("ec", "be"))
	r := route23("ec", "r").top().rule(at("/r"), toName("ec", "x"), toName("ec", "k")).rule(at("/r"), toName("ec", "u"))
	in := world(r, x, k, u)
	in.Bounds.Edges = 2 // rule 1's ref passes the cap
	res := delegate(in)
	if res.Cap != computed.CapEdges {
		t.Fatalf("cap %q", res.Cap)
	}
	incomplete(t, res, true, r, x, k, u)
	if got := row(t, res, r, 0, 0, x); got.Outcome != computed.OutcomeUnknown {
		t.Fatalf("R→X %s, want unknown: a drop needs completeness", got.Outcome)
	}
	if got := row(t, res, r, 0, 1, k); got.Outcome != computed.OutcomeAttached || !got.SomeNotComputed {
		t.Fatalf("R→K %s, SomeNotComputed %v", got.Outcome, got.SomeNotComputed)
	}
}

func TestUnknownRootIsUncertain(t *testing.T) {
	g := route23("ur", "g").rule(at("/u/c/g"), service("ur", "be"))
	c := route23("ur", "c").rule(at("/u/c"), toName("ur", "g"))
	u := route23("ur", "u").top().rule(at("/u"), toName("ur", "c"))
	z := route23("ur", "z").rule(at("/z"), service("ur", "be"))
	k := route23("ur", "k").top().rule(at("/k"), toName("ur", "z"))
	in := world(u, c, g, k, z)
	for i := range in.Roots {
		if in.Roots[i].Route == u.r.Ref {
			in.Roots[i].Unknown, in.Roots[i].Keys = true, nil
		}
	}
	res := delegate(in)
	if !slices.Contains(res.Uncertain, computed.Uncertain{Route: u.r.Ref, Rule: computed.UncertainRoot, Ref: -1, Reason: computed.UnknownContext}) {
		t.Fatalf("uncertain %+v", res.Uncertain)
	}
	incomplete(t, res, true, u, c, g)
	incomplete(t, res, false, k)
	// K→Z drops Z completely: a known negative.
	if got := row(t, res, k, 0, 0, z); got.Outcome != computed.OutcomeDropped {
		t.Fatalf("K→Z %s", got.Outcome)
	}
}

func TestUnknownWildcardLabelRef(t *testing.T) {
	// rules §11.2: R's label ref has an unknown wildcard setting; S, in another namespace, may be
	// selected, and C is below S. A labelled route in R's own namespace would be selected for certain.
	c := route23("shop", "c").rule(at("/r/s/c"), service("shop", "be"))
	s := route23("lab", "s").label("v").rule(at("/r/s"), toName("shop", "c"))
	n := route23("shop", "n").rule(at("/n"), service("shop", "be"))
	r := route23("shop", "r").top().rule(at("/r"), toLabel("shop", "v"))
	in := world(r, s, c, n)
	in.Wildcard = install.Setting{} // valueFrom: unknown
	res := delegate(in)
	incomplete(t, res, true, r, s, c)
	incomplete(t, res, false, n)
}

func TestCutAfterDropBeforeAttach(t *testing.T) {
	// The /a context drops C; the cap cuts the /b context, which would attach it.
	c := route23("ca", "c").rule(at("/b/c"), service("ca", "be"))
	p := route23("ca", "p").top().rule(at("/a", "/b"), toName("ca", "c"))
	in := world(p, c)
	in.Bounds.Contexts = 1
	res := delegate(in)
	got := row(t, res, p, 0, 0, c)
	if got.Outcome != computed.OutcomeUnknown || got.Unknown != computed.UnknownTooLarge {
		t.Fatalf("row %s %s, want unknown too-large, not dropped", got.Outcome, got.Unknown)
	}
	incomplete(t, res, true, c)
}

func TestCutBeforeAnyContextAttachedComputed(t *testing.T) {
	// A attaches C; B's ref to C is cut before it reaches C, so A's row is attached with some paths not computed.
	c := route23("cb", "c").rule(at("/a/c", "/b/c"), service("cb", "be"))
	a := route23("cb", "a").top().rule(at("/a"), toName("cb", "c"))
	b := route23("cb", "b").top().rule(at("/b"), toName("cb", "c"))
	in := world(a, b, c)
	in.Bounds.Contexts = 1
	res := delegate(in)
	got := row(t, res, a, 0, 0, c)
	if got.Outcome != computed.OutcomeAttached || !got.SomeNotComputed || !slices.Equal(ctxOutcomes(got), []computed.RowOutcome{computed.OutcomeAttached}) {
		t.Fatalf("A→C %s SomeNotComputed %v contexts %v", got.Outcome, got.SomeNotComputed, ctxOutcomes(got))
	}
}

func TestDenseSelectionCut(t *testing.T) {
	// PF16: decoded directly; pkg/engine imports the rule sets.
	in := inputOf(t, scale.DenseSelection(100, 1000))
	res := delegate(in)
	edges := computed.DefaultBounds.Edges
	if res.Cap != computed.CapEdges || len(res.Rows) != edges {
		t.Fatalf("cap %q, %d rows", res.Cap, len(res.Rows))
	}
	cut := 0
	for _, u := range res.Uncertain {
		if u.Reason != computed.UnknownTooLarge || u.Rule != 0 || u.Ref != 0 {
			t.Fatalf("uncertain %+v", u)
		}
		cut++
	}
	// Each parent selects 1,000 routes; the parents past the cap are cut.
	if want := 100 - edges/1000; cut != want {
		t.Fatalf("%d cut parents, want %d", cut, want)
	}
	for _, r := range res.Rows {
		if r.Outcome != computed.OutcomeAttached || !r.SomeNotComputed {
			t.Fatalf("row %+v: %s, SomeNotComputed %v", r.Key, r.Outcome, r.SomeNotComputed)
		}
	}
	if again := delegate(in); !reflect.DeepEqual(res, again) {
		t.Fatal("two runs differ")
	}
}

// The consistent scale worlds attach every child under every context (PF2):
// no parent failure, every row attached, and Large inside every cap.
func TestScaleWorldViewBConsistent(t *testing.T) {
	for _, tier := range []scale.Tier{scale.Typical, scale.Large} {
		t.Run(tier.Name, func(t *testing.T) {
			in := delegationInput(t, tier)
			w := newWalker(in)
			w.run()
			res := w.result()
			if res.Cap != "" || len(res.Signals) != 0 || len(res.Uncertain) != 0 {
				t.Fatalf("cap %q, %d signals, %d uncertain", res.Cap, len(res.Signals), len(res.Uncertain))
			}
			for _, r := range res.Rows {
				if r.Outcome != computed.OutcomeAttached || r.SomeNotComputed {
					t.Fatalf("row %+v: %s", r.Key, r.Outcome)
				}
			}
			b := w.b
			t.Logf("%s: %d rows, contexts %d/%d, matchers %d/%d, edges %d/%d, retained %d/%d",
				tier.Name, len(res.Rows), w.contexts, b.Contexts, w.matchers, b.Matchers, w.edges, b.Edges, w.acc.kept, b.ContextsPerInstall)
			// Room to spare: the budget tier must never sit at a cap.
			if 4*w.contexts > 3*b.Contexts || 4*w.matchers > 3*b.Matchers || 4*w.edges > 3*b.Edges || 4*w.acc.kept > 3*b.ContextsPerInstall {
				t.Fatal("Large is within a quarter of a cap")
			}
		})
	}
}
