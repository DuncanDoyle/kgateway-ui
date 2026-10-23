package explore

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

var edgeGW = objects.Ref{Group: "gateway.networking.k8s.io", Kind: "Gateway", Namespace: "infra", Name: "edge"}

// upIx builds an index where parents maps a route name to its reported
// parent routes and gws names the routes attached to edgeGW.
func upIx(parents map[string][]string, gws ...string) *Index {
	all := map[string]bool{}
	for c, ps := range parents {
		all[c] = true
		for _, p := range ps {
			all[p] = true
		}
	}
	for _, g := range gws {
		all[g] = true
	}
	var rows []RouteRow
	for n := range all {
		r := hr(n, "")
		if slices.Contains(gws, n) {
			r.Parents = []ParentLink{{Target: edgeGW, Attachment: AttachmentAccepted}}
		}
		rows = append(rows, r)
	}
	ix := hand(rows...)
	ix.ParentRoutes = map[objects.Ref][]objects.Ref{}
	for c, ps := range parents {
		for _, p := range ps {
			ix.ParentRoutes[routeRef("shop", c)] = append(ix.ParentRoutes[routeRef("shop", c)], routeRef("shop", p))
		}
	}
	return ix
}

func all(objects.Ref) bool { return true }

// render prints a path root first, "?" for a restricted step.
func render(p AncestorPath) string {
	var s []string
	for _, st := range p.Steps {
		if st.Restricted {
			s = append(s, "?")
		} else {
			s = append(s, st.Ref.Name)
		}
	}
	out := strings.Join(s, ">")
	if p.Cycle {
		out += " (cycle)"
	}
	return out
}

func renders(ps []AncestorPath) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, render(p))
	}
	return out
}

func TestAncestorsChain(t *testing.T) {
	ix := upIx(map[string][]string{"child": {"parent"}}, "parent")
	got, trunc := ix.Ancestors(routeRef("shop", "child"), all)
	if want := []string{"edge>parent>child"}; !reflect.DeepEqual(renders(got), want) || trunc {
		t.Fatalf("paths %v truncated %v, want %v", renders(got), trunc, want)
	}
}

func TestAncestorsDiamond(t *testing.T) {
	ix := upIx(map[string][]string{"c": {"b2", "b1"}, "b1": {"a"}, "b2": {"a"}}, "a")
	got, trunc := ix.Ancestors(routeRef("shop", "c"), all)
	if want := []string{"edge>a>b1>c", "edge>a>b2>c"}; !reflect.DeepEqual(renders(got), want) || trunc {
		t.Fatalf("paths %v truncated %v, want %v", renders(got), trunc, want)
	}
}

func TestAncestorsCycleMarker(t *testing.T) {
	ix := upIx(map[string][]string{"a": {"b"}, "b": {"a", "root"}}, "root")
	got, _ := ix.Ancestors(routeRef("shop", "a"), all)
	if want := []string{"a>b>a (cycle)", "edge>root>b>a"}; !reflect.DeepEqual(renders(got), want) {
		t.Fatalf("paths %v, want %v", renders(got), want)
	}
	if !got[0].Cycle || got[1].Cycle {
		t.Fatalf("cycle flags %v %v", got[0].Cycle, got[1].Cycle)
	}
}

// lattice builds layers of two routes, each parented by both routes above.
func lattice(layers int, withGateway bool) (*Index, objects.Ref) {
	p := map[string][]string{}
	name := func(l int, x string) string { return fmt.Sprintf("l%d%s", l, x) }
	for l := 1; l < layers; l++ {
		for _, x := range []string{"a", "b"} {
			p[name(l, x)] = []string{name(l-1, "a"), name(l-1, "b")}
		}
	}
	p["child"] = []string{name(layers-1, "a"), name(layers-1, "b")}
	var gws []string
	if withGateway {
		gws = []string{name(0, "a"), name(0, "b")}
	} else {
		p[name(0, "a")], p[name(0, "b")] = nil, nil
	}
	return upIx(p, gws...), routeRef("shop", "child")
}

func TestAncestorsLatticeTruncates(t *testing.T) {
	ix, child := lattice(8, true)
	got, trunc, visits := ix.walkUp(child, all)
	if len(got) != MaxPaths || !trunc {
		t.Fatalf("%d paths truncated %v, want %d and true", len(got), trunc, MaxPaths)
	}
	// 2^8 paths exist; the walk must stop long before building them.
	if visits > MaxNodes {
		t.Fatalf("%d node visits, bound is %d", visits, MaxNodes)
	}
	t.Logf("%d visits for %d paths", visits, len(got))
}

func TestAncestorsNodeBound(t *testing.T) {
	// No root anywhere, so no path completes and only the node bound ends the walk.
	ix, child := lattice(8, false)
	got, trunc, visits := ix.walkUp(child, all)
	if len(got) != 0 || !trunc || visits != MaxNodes {
		t.Fatalf("paths %d truncated %v visits %d, want 0, true, %d", len(got), trunc, visits, MaxNodes)
	}
	// A wide fan reaches the bound too.
	p := map[string][]string{}
	for i := range 300 {
		p["c"] = append(p["c"], fmt.Sprintf("p%03d", i))
	}
	_, trunc, visits = upIx(p).walkUp(routeRef("shop", "c"), all)
	if !trunc || visits != MaxNodes {
		t.Fatalf("fan: truncated %v visits %d, want true, %d", trunc, visits, MaxNodes)
	}
}

func TestAncestorsDepthBound(t *testing.T) {
	chain := func(n int) *Index {
		p := map[string][]string{}
		for i := 1; i < n; i++ {
			p[fmt.Sprintf("r%02d", i)] = []string{fmt.Sprintf("r%02d", i-1)}
		}
		return upIx(p, "r00")
	}
	got, trunc := chain(MaxDepth).Ancestors(routeRef("shop", fmt.Sprintf("r%02d", MaxDepth-1)), all)
	if len(got) != 1 || trunc || len(got[0].Steps) != MaxDepth+1 {
		t.Fatalf("at the bound: %d paths truncated %v", len(got), trunc)
	}
	got, trunc = chain(MaxDepth+1).Ancestors(routeRef("shop", fmt.Sprintf("r%02d", MaxDepth)), all)
	if len(got) != 0 || !trunc {
		t.Fatalf("past the bound: %d paths truncated %v", len(got), trunc)
	}
}

func TestAncestorsExactlyMaxPathsIsNotTruncated(t *testing.T) {
	p := map[string][]string{"c": {}}
	var gws []string
	for i := range MaxPaths {
		n := fmt.Sprintf("p%d", i)
		p["c"] = append(p["c"], n)
		gws = append(gws, n)
	}
	got, trunc := upIx(p, gws...).Ancestors(routeRef("shop", "c"), all)
	if len(got) != MaxPaths || trunc {
		t.Fatalf("%d paths truncated %v", len(got), trunc)
	}
}

func TestAncestorsOnlyReportedGatewayEdges(t *testing.T) {
	ix := upIx(map[string][]string{}, "c")
	ix.Routes[0].Parents[0].Attachment = AttachmentDeclared
	if got, _ := ix.Ancestors(routeRef("shop", "c"), all); len(got) != 0 {
		t.Fatalf("a declared edge is no up-chain: %v", renders(got))
	}
}

func TestAncestorsRestrictedStep(t *testing.T) {
	ix := upIx(map[string][]string{"child": {"mid"}, "mid": {"top"}}, "top")
	hide := func(name string) func(objects.Ref) bool {
		return func(r objects.Ref) bool { return r.Name != name }
	}
	cases := []struct {
		name, hidden string
		want         []string
	}{
		{"hidden middle route", "mid", []string{"?>child"}},
		{"hidden top route", "top", []string{"?>mid>child"}},
		{"hidden Gateway", "edge", []string{"?>top>mid>child"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := ix.Ancestors(routeRef("shop", "child"), hide(c.hidden))
			if !reflect.DeepEqual(renders(got), c.want) {
				t.Fatalf("paths %v, want %v", renders(got), c.want)
			}
			for _, p := range got {
				for _, s := range p.Steps {
					if s.Restricted && s.Ref != (objects.Ref{}) {
						t.Fatalf("restricted step carries %v", s.Ref)
					}
				}
			}
		})
	}
	// A hidden route's parents are not named, even though visible on their own.
	got, _ := ix.Ancestors(routeRef("shop", "child"), hide("mid"))
	for _, p := range got {
		for _, s := range p.Steps {
			if s.Ref.Name == "top" || s.Ref.Name == "edge" {
				t.Fatalf("hidden route leaked its ancestor %v", s.Ref)
			}
		}
	}
}

func TestAncestorsHiddenParentsCollapse(t *testing.T) {
	ix := upIx(map[string][]string{"c": {"h1", "h2", "v"}, "v": {}}, "v", "h1", "h2")
	got, _ := ix.Ancestors(routeRef("shop", "c"), func(r objects.Ref) bool { return r.Name != "h1" && r.Name != "h2" })
	// One redacted path, so the number of hidden parents is not shown.
	if want := []string{"?>c", "edge>v>c"}; !reflect.DeepEqual(renders(got), want) {
		t.Fatalf("paths %v, want %v", renders(got), want)
	}
}
