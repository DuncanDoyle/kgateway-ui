package computed

import (
	"slices"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

func dep(n string) objects.Ref { return objects.Ref{Kind: "Gateway", Namespace: "infra", Name: n} }

// Equal sets share one backing slice, and equal aggregates finish as one set.
func TestDepSetsShareEqualSets(t *testing.T) {
	s := NewDepSets()
	a, b := []objects.Ref{dep("a"), dep("c")}, []objects.Ref{dep("a"), dep("c")}
	if ca, cb := s.Intern(a), s.Intern(b); &ca[0] != &a[0] || &cb[0] != &a[0] {
		t.Fatal("equal sets not shared")
	}
	x, y := DepAcc{}, DepAcc{}
	x.Add(a)
	x.Add([]objects.Ref{dep("b")})
	y.Add([]objects.Ref{dep("b")})
	y.Add(b)
	y.Add(a)
	u1, u2 := s.Finish(&x), s.Finish(&y)
	if !slices.Equal(u1, []objects.Ref{dep("a"), dep("b"), dep("c")}) || &u1[0] != &u2[0] {
		t.Fatalf("aggregates %v %v not one shared set", u1, u2)
	}
	one := DepAcc{}
	one.Add(b)
	if u := s.Finish(&one); &u[0] != &a[0] {
		t.Fatal("a one-set aggregate is not its canonical set")
	}
	if s.Intern(nil) != nil || s.Finish(&DepAcc{}) != nil {
		t.Fatal("empty sets not kept empty")
	}
}
