// Port of kgw23 for v2.4.7; keep in sync.

package kgw24

import (
	"reflect"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

func pm(typ, value string) objects.Match {
	return objects.Match{Path: &objects.PathMatch{Type: typ, Value: value}}
}

func prefix(v string) objects.Match { return pm("PathPrefix", v) }

func kv(name, value string) objects.KeyMatch {
	return objects.KeyMatch{Type: "Exact", Name: name, Value: value}
}

func withHeaders(m objects.Match, h ...objects.KeyMatch) objects.Match { m.Headers = h; return m }
func withQuery(m objects.Match, q ...objects.KeyMatch) objects.Match   { m.QueryParams = q; return m }
func withMethod(m objects.Match, v string) objects.Match               { m.Method = v; return m }

func TestFitClauses(t *testing.T) {
	re := objects.KeyMatch{Type: "RegularExpression", Name: "x", Value: "1"}
	for _, c := range []struct {
		name          string
		parent, child objects.Match
		ok            bool
		clause        computed.FitClause
	}{
		{"parent exact", pm("Exact", "/api"), prefix("/api"), false, computed.ClausePathType},
		{"parent regex", pm("RegularExpression", "/api.*"), prefix("/api"), false, computed.ClausePathType},
		{"raw prefix", prefix("/api"), prefix("/apiv2"), true, ""},
		{"child type not checked", prefix("/api"), pm("Exact", "/api/x"), true, ""},
		{"other prefix", prefix("/api"), prefix("/other"), false, computed.ClausePathPrefix},
		{"header missing", withHeaders(prefix("/api"), kv("x", "1")), prefix("/api"), false, computed.ClauseHeader},
		{"header equal", withHeaders(prefix("/api"), kv("x", "1")), withHeaders(prefix("/api"), kv("x", "1")), true, ""},
		{"header other type", withHeaders(prefix("/api"), kv("x", "1")), withHeaders(prefix("/api"), re), false, computed.ClauseHeader},
		{"query missing", withQuery(prefix("/api"), kv("x", "1")), prefix("/api"), false, computed.ClauseQuery},
		{"query equal", withQuery(prefix("/api"), kv("x", "1")), withQuery(prefix("/api"), kv("x", "1")), true, ""},
		{"query other type", withQuery(prefix("/api"), kv("x", "1")), withQuery(prefix("/api"), re), false, computed.ClauseQuery},
		{"method other", withMethod(prefix("/api"), "GET"), withMethod(prefix("/api"), "POST"), false, computed.ClauseMethod},
		{"method none", withMethod(prefix("/api"), "GET"), prefix("/api"), false, computed.ClauseMethod},
		{"method equal", withMethod(prefix("/api"), "GET"), withMethod(prefix("/api"), "GET"), true, ""},
		// Beyond the brief: a child method under a parent without one, and extra child headers.
		{"child method only", prefix("/api"), withMethod(prefix("/api"), "POST"), true, ""},
		{"trailing slash parent", prefix("/api/"), prefix("/api"), false, computed.ClausePathPrefix},
		{"header name case", withHeaders(prefix("/api"), kv("X", "1")), withHeaders(prefix("/api"), kv("x", "1")), false, computed.ClauseHeader},
		{"extra child header", withHeaders(prefix("/api"), kv("x", "1")), withHeaders(prefix("/api"), kv("a", "0"), kv("x", "1")), true, ""},
	} {
		ok, clause := fits(c.parent, c.child)
		if ok != c.ok || clause != c.clause {
			t.Errorf("%s: fits = %v, %q; want %v, %q", c.name, ok, clause, c.ok, c.clause)
		}
	}
}

func TestOmittedMatchesUnderPrefix(t *testing.T) {
	child := &objects.HTTPRouteView{Rules: []objects.Rule{{Index: 0}}}
	rules, kept := viewC(child, prefix("/api"))
	want := computed.EffectiveRule{Index: 0, Reason: &computed.FitFailure{Rule: 0, Match: 0, Clause: computed.ClauseDefaultSlash, Parent: "/api", Child: "/"}}
	if kept || len(rules) != 1 || !reflect.DeepEqual(rules[0], want) {
		t.Errorf("under /api: %+v, %v; want %+v dropped", rules, kept, want)
	}

	rules, kept = viewC(child, prefix("/"))
	want = computed.EffectiveRule{Index: 0, Kept: true, Matches: []objects.Match{prefix("/")}}
	if !kept || len(rules) != 1 || !reflect.DeepEqual(rules[0], want) {
		t.Errorf("under /: %+v, %v; want %+v kept", rules, kept, want)
	}

	// Under an Exact parent the clause is path-type, not default-slash.
	rules, kept = viewC(child, pm("Exact", "/api"))
	want = computed.EffectiveRule{Index: 0, Reason: &computed.FitFailure{Rule: 0, Match: 0, Clause: computed.ClausePathType, Parent: "Exact", Child: "/"}}
	if kept || len(rules) != 1 || !reflect.DeepEqual(rules[0], want) {
		t.Errorf("under Exact /api: %+v, %v; want %+v dropped", rules, kept, want)
	}

	// Inherit mode merges the default PathPrefix / into the parent's path.
	inherit := &objects.HTTPRouteView{InheritParentMatcher: true, Rules: []objects.Rule{{Index: 0}}}
	rules, kept = viewC(inherit, prefix("/api"))
	want = computed.EffectiveRule{Index: 0, Kept: true, Matches: []objects.Match{prefix("/api")}}
	if !kept || len(rules) != 1 || !reflect.DeepEqual(rules[0], want) {
		t.Errorf("inherit under /api: %+v, %v; want %+v kept", rules, kept, want)
	}
}

func TestMerge(t *testing.T) {
	for _, c := range []struct {
		name                string
		parent, child, want objects.Match
	}{
		{"cleaned", prefix("/api"), prefix("/"), prefix("/api")},
		{"child type kept", prefix("/api"), pm("Exact", "/x"), pm("Exact", "/api/x")},
		{"nil child path", prefix("/api"), objects.Match{}, prefix("/api")},
		{"parent type not checked", pm("Exact", "/api"), prefix("/x"), prefix("/api/x")},
		{"headers, parent wins, sorted",
			withHeaders(prefix("/api"), kv("b", "2"), kv("a", "1")),
			withHeaders(prefix("/"), kv("c", "4"), kv("b", "3")),
			withHeaders(prefix("/api"), kv("a", "1"), kv("b", "2"), kv("c", "4"))},
		{"query, parent wins, sorted",
			withQuery(prefix("/api"), kv("b", "2"), kv("a", "1")),
			withQuery(prefix("/"), kv("c", "4"), kv("b", "3")),
			withQuery(prefix("/api"), kv("a", "1"), kv("b", "2"), kv("c", "4"))},
		{"parent method wins", withMethod(prefix("/api"), "GET"), withMethod(prefix("/"), "POST"), withMethod(prefix("/api"), "GET")},
		{"child method kept", prefix("/api"), withMethod(prefix("/"), "POST"), withMethod(prefix("/api"), "POST")},
		// Beyond the brief: a trailing slash is cleaned away, as path.Join does.
		{"trailing slash", prefix("/api"), prefix("/x/"), prefix("/api/x")},
	} {
		if got := merge(c.parent, c.child); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: merge = %+v; want %+v", c.name, got, c.want)
		}
	}

	// merge never writes through to its inputs.
	parent, child := withHeaders(prefix("/api"), kv("a", "1")), withHeaders(prefix("/x"), kv("b", "2"))
	merge(parent, child)
	if child.Path.Value != "/x" || len(child.Headers) != 1 || len(parent.Headers) != 1 {
		t.Errorf("merge mutated its inputs: %+v, %+v", parent, child)
	}
}

func TestExplicitEmptyMatches(t *testing.T) {
	parent := withHeaders(prefix("/api"), kv("x", "1"))
	child := &objects.HTTPRouteView{InheritParentMatcher: true, Rules: []objects.Rule{{Index: 0, Matches: []objects.Match{}}}}
	rules, kept := viewC(child, parent)
	want := []computed.EffectiveRule{{Index: 0, Kept: true, Matches: []objects.Match{parent}}}
	if !kept || !reflect.DeepEqual(rules, want) {
		t.Errorf("inherit: %+v, %v; want %+v", rules, kept, want)
	}

	child.InheritParentMatcher = false
	rules, kept = viewC(child, parent)
	want = []computed.EffectiveRule{{Index: 0, Reason: &computed.FitFailure{Rule: 0, Match: -1, Clause: computed.ClauseNoMatches, Parent: "/api"}}}
	if kept || !reflect.DeepEqual(rules, want) {
		t.Errorf("default: %+v, %v; want %+v", rules, kept, want)
	}
}

func TestViewCKeepsChildIffAnyRuleKept(t *testing.T) {
	child := &objects.HTTPRouteView{Rules: []objects.Rule{
		{Index: 0, Matches: []objects.Match{prefix("/other"), withMethod(prefix("/api/x"), "GET")}},
		{Index: 1, Matches: []objects.Match{prefix("/other"), prefix("/api/y")}},
	}}
	parent := withMethod(prefix("/api"), "GET")

	// Rule 0 keeps its second match; rule 1 fails on its first match's clause.
	rules, kept := viewC(child, parent)
	want := []computed.EffectiveRule{
		{Index: 0, Kept: true, Matches: []objects.Match{withMethod(prefix("/api/x"), "GET")}},
		{Index: 1, Reason: &computed.FitFailure{Rule: 1, Match: 0, Clause: computed.ClausePathPrefix, Parent: "/api", Child: "/other"}},
	}
	if !kept || !reflect.DeepEqual(rules, want) {
		t.Errorf("one rule kept: %+v, %v; want %+v", rules, kept, want)
	}

	rules, kept = viewC(child, prefix("/none"))
	if kept || len(rules) != 2 || rules[0].Kept || rules[1].Kept {
		t.Errorf("no rule kept: %+v, %v; want both dropped", rules, kept)
	}

	// Inherit mode keeps every match, merged.
	child.InheritParentMatcher = true
	rules, kept = viewC(child, parent)
	if !kept || len(rules) != 2 || !rules[0].Kept || !rules[1].Kept ||
		!reflect.DeepEqual(rules[1].Matches, []objects.Match{withMethod(prefix("/api/other"), "GET"), withMethod(prefix("/api/api/y"), "GET")}) {
		t.Errorf("inherit: %+v, %v", rules, kept)
	}
}
