package computed

import (
	"slices"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

func dep(n string) objects.Ref { return objects.Ref{Kind: "Gateway", Namespace: "infra", Name: n} }

// Equal sets share one backing slice, and equal unions are computed once.
func TestDepSetsShareEqualSets(t *testing.T) {
	s := NewDepSets()
	a, b := []objects.Ref{dep("a"), dep("c")}, []objects.Ref{dep("a"), dep("c")}
	if ca, cb := s.Intern(a), s.Intern(b); &ca[0] != &a[0] || &cb[0] != &a[0] {
		t.Fatal("equal sets not shared")
	}
	u1 := s.Union(a, []objects.Ref{dep("b")})
	u2 := s.Union(b, []objects.Ref{dep("b")})
	if !slices.Equal(u1, []objects.Ref{dep("a"), dep("b"), dep("c")}) || &u1[0] != &u2[0] {
		t.Fatalf("unions %v %v not one shared set", u1, u2)
	}
	if u := s.Union(u1, a); &u[0] != &u1[0] {
		t.Fatal("a subset grew the set")
	}
	if s.Intern(nil) != nil || s.Union(nil, nil) != nil {
		t.Fatal("empty sets not kept empty")
	}
}
