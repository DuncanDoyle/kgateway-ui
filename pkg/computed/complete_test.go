package computed

import (
	"fmt"
	"slices"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

func cRef(name string) objects.Ref {
	return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "c", Name: name}
}

func sortEdges(es []Edge) []Edge {
	slices.SortFunc(es, func(a, b Edge) int {
		if c := a.Parent.Compare(b.Parent); c != 0 {
			return c
		}
		return a.Child.Compare(b.Child)
	})
	return es
}

func TestCompletenessSeeds(t *testing.T) {
	// s → a → b; x → y is unrelated; r has an uncertain key only.
	s, a, b, x, y, r := cRef("s"), cRef("a"), cRef("b"), cRef("x"), cRef("y"), cRef("r")
	key := ReportKey{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "gw", Name: "g"}
	res := DelegationResult{
		Reach:         sortEdges([]Edge{{s, a}, {a, b}, {x, y}, {r, x}}),
		Uncertain:     []Uncertain{{Route: s, Rule: 0, Ref: 0, Reason: UnknownTooLarge}},
		UncertainKeys: []RouteKey{{Route: r, Key: key}},
	}
	c := NewCompleteness(res)
	for _, in := range []objects.Ref{s, a, b} {
		if !c.Incomplete(in) {
			t.Fatalf("%s complete", in.Name)
		}
	}
	// An uncertain key leaves the root's descendants complete: the evaluation is the same under every key.
	for _, out := range []objects.Ref{x, y, r} {
		if c.Incomplete(out) {
			t.Fatalf("%s incomplete", out.Name)
		}
	}
	if !c.IncompleteKey(r, key) || c.IncompleteKey(r, ReportKey{Name: "other"}) || !c.IncompleteKey(a, ReportKey{}) {
		t.Fatal("IncompleteKey")
	}
	res.Cap = CapEdges
	if c := NewCompleteness(res); !c.Incomplete(y) || !c.Incomplete(cRef("anything")) {
		t.Fatal("the edge cap must make every route incomplete")
	}
	if (Completeness{}).Incomplete(s) {
		t.Fatal("zero Completeness is complete")
	}
}

func TestClosureIsLinear(t *testing.T) {
	// s → 100 a's, each a → every one of 499 b's: 50,000 edges, b's reached 100 times each.
	s := cRef("s")
	var es []Edge
	for i := range 100 {
		a := cRef(fmt.Sprintf("a%03d", i))
		es = append(es, Edge{s, a})
		for j := range 499 {
			es = append(es, Edge{a, cRef(fmt.Sprintf("b%03d", j))})
		}
	}
	sortEdges(es)
	if len(es) != 50_000 {
		t.Fatalf("%d edges", len(es))
	}
	visits := 0
	in := closure(es, []objects.Ref{s, s}, func() { visits++ })
	if visits != len(es) {
		t.Fatalf("%d edge visits, want %d", visits, len(es))
	}
	if len(in) != 1+100+499 {
		t.Fatalf("closure has %d routes", len(in))
	}
}
