// Port of kgw23 for v2.4.7; keep in sync.

package kgw24

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// edge finds child c's effective edge from parent p or fails.
func edge(t *testing.T, res computed.DelegationResult, c, p *hr) computed.EffectiveEdge {
	t.Helper()
	for _, e := range res.Effective[c.r.Ref].Edges {
		if e.Parent == p.r.Ref {
			return e
		}
	}
	t.Fatalf("no edge %s → %s in %+v", p.r.Ref.Name, c.r.Ref.Name, res.Effective[c.r.Ref])
	return computed.EffectiveEdge{}
}

func matchPaths(cs []computed.Context) []string {
	var out []string
	for _, c := range cs {
		out = append(out, pathValue(c.ParentMatch))
	}
	return out
}

func TestEffectiveProjection(t *testing.T) {
	t.Run("21 contexts on one edge", func(t *testing.T) {
		var ps []string
		for i := range 21 {
			ps = append(ps, fmt.Sprintf("/p/%02d", i))
		}
		c := route23("ep", "c").inherit().rule(at("/c"), service("ep", "be"))
		p := route23("ep", "p").top().rule(at(ps...), toName("ep", "c"))
		in := world(p, c)
		in.Bounds.ContextsPerRow = 30 // the row keeps all 21; the edge keeps 20
		res := delegate(in)
		if r := row(t, res, p, 0, 0, c); len(r.Contexts) != 21 || r.ContextsTruncated {
			t.Fatalf("row kept %d", len(r.Contexts))
		}
		e := edge(t, res, c, p)
		if len(e.Contexts) != 20 || !e.ContextsTruncated || e.Mode != "inherit" || !slices.Equal(matchPaths(e.Contexts), ps[:20]) {
			t.Fatalf("edge %d contexts, truncated %v, mode %q: %v", len(e.Contexts), e.ContextsTruncated, e.Mode, matchPaths(e.Contexts))
		}
	})
	t.Run("an edge from two rows, one truncated", func(t *testing.T) {
		c := route23("et", "c").rule(at("/a/c", "/b/c", "/d/c"), service("et", "be"))
		p := route23("et", "p").top().rule(at("/a"), toName("et", "c")).rule(at("/b", "/d"), toName("et", "c"))
		in := world(p, c)
		in.Bounds.ContextsPerRow = 1
		res := delegate(in)
		e := edge(t, res, c, p)
		if !slices.Equal(matchPaths(e.Contexts), []string{"/a", "/b"}) || !e.ContextsTruncated || e.Mode != "default" {
			t.Fatalf("edge %v truncated %v mode %q", matchPaths(e.Contexts), e.ContextsTruncated, e.Mode)
		}
	})
	t.Run("an edge at the install cap", func(t *testing.T) {
		c1 := route23("ei", "c1").rule(at("/a/1"), service("ei", "be"))
		c2 := route23("ei", "c2").rule(at("/a/2"), service("ei", "be"))
		p := route23("ei", "p").top().rule(at("/a"), toName("ei", "c1")).rule(at("/a"), toName("ei", "c2"))
		in := world(p, c1, c2)
		in.Bounds.ContextsPerInstall = 1
		res := delegate(in)
		if e := edge(t, res, c1, p); len(e.Contexts) != 1 || e.ContextsTruncated {
			t.Fatalf("C1 edge %+v", e)
		}
		e := edge(t, res, c2, p)
		if e.Contexts == nil || len(e.Contexts) != 0 || !e.ContextsTruncated || len(e.Deps) == 0 {
			t.Fatalf("C2 edge %+v, want non-nil empty contexts, truncated, with deps", e)
		}
	})
	t.Run("21 parent edges", func(t *testing.T) {
		c := route23("pe", "c").rule(at("/x/c"), service("pe", "be"))
		bs := []*hr{c}
		var want []objects.Ref
		for i := range 21 {
			p := route23("pe", fmt.Sprintf("p%02d", i)).top().rule(at("/x"), toName("pe", "c"))
			bs = append(bs, p)
			want = append(want, p.r.Ref)
		}
		res := delegate(world(bs...))
		ce := res.Effective[c.r.Ref]
		var got []objects.Ref
		for _, e := range ce.Edges {
			got = append(got, e.Parent)
		}
		if !ce.Truncated || !slices.Equal(got, want[:20]) {
			t.Fatalf("truncated %v, parents %v", ce.Truncated, got)
		}
	})
	t.Run("quirk 1 repeats are one context", func(t *testing.T) {
		// C is handed to both refs of the rule under the one /q context.
		c := route23("eq", "c").rule(at("/q/c"), service("eq", "be"))
		d := route23("eq", "d").rule(at("/q/d"), service("eq", "be"))
		p := route23("eq", "p").top().rule(at("/q"), toName("eq", "c"), toName("eq", "d"))
		res := delegate(world(p, c, d))
		if len(row(t, res, p, 0, 1, c).Contexts) != 1 {
			t.Fatal("quirk 1 row missing")
		}
		if e := edge(t, res, c, p); len(e.Contexts) != 1 || e.ContextsTruncated {
			t.Fatalf("edge %v truncated %v", matchPaths(e.Contexts), e.ContextsTruncated)
		}
	})
	t.Run("no view C, no edge", func(t *testing.T) {
		// A forbidden child and an unresolved ref have no effective matcher.
		f := route23("en", "f").parentOf(routeRef("en", "other")).rule(at("/n/f"), service("en", "be"))
		p := route23("en", "p").top().rule(at("/n"), toName("en", "f"), toName("en", "missing"))
		res := delegate(world(p, f))
		if _, ok := res.Effective[f.r.Ref]; ok || len(res.Effective) != 0 {
			t.Fatalf("effective %+v", res.Effective)
		}
	})
}

func TestEffectiveEqualsTraversal(t *testing.T) {
	g := route23("ev", "g").rule(at("/api/v1/g"), service("ev", "be"))
	h := route23("ev", "h").rule(at("/elsewhere"), service("ev", "be"))
	c := route23("ev", "c").inherit().rule(at("/v1"), toName("ev", "g"), toName("ev", "h")).rule(nil, service("ev", "be"))
	d := route23("ev", "d").rule(at("/api/d"), toName("ev", "g"))
	r := route23("ev", "r").top().rule(at("/api", "/other"), toName("ev", "c"), toName("ev", "d"))
	res := delegate(world(r, c, d, g, h))
	n := 0
	for child, ce := range res.Effective {
		for _, e := range ce.Edges {
			for _, cx := range e.Contexts {
				n++
				found := slices.ContainsFunc(res.Rows, func(rw computed.Row) bool {
					return rw.Key.Parent == e.Parent && rw.Key.Child == child &&
						slices.ContainsFunc(rw.Contexts, func(rc computed.Context) bool { return reflect.DeepEqual(rc, cx) })
				})
				if !found || len(cx.Rules) == 0 {
					t.Fatalf("%s → %s context %v is not a traversal context", e.Parent.Name, child.Name, pathOf(cx))
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("no effective contexts")
	}
	// G under C carries C's merged match, and its dropped sibling H keeps its reason.
	if cx := edge(t, res, g, c).Contexts[0]; pathValue(cx.ParentMatch) != "/api/v1" {
		t.Fatalf("G's parent match %s", pathValue(cx.ParentMatch))
	}
	if cx := edge(t, res, h, c).Contexts[0]; cx.Rules[0].Kept || cx.Rules[0].Reason == nil {
		t.Fatalf("H's rule %+v", cx.Rules[0])
	}
}
