package kgw23

import (
	"reflect"
	"testing"
)

func TestIntersect(t *testing.T) {
	for _, c := range []struct {
		l       string
		r, want []string
		ok      bool
	}{
		{"", nil, nil, true},
		{"a.example.com", nil, []string{"a.example.com"}, true},
		{"", []string{"x.com"}, []string{"x.com"}, true},
		{"a.example.com", []string{"a.example.com"}, []string{"a.example.com"}, true},
		{"a.example.com", []string{"b.example.com"}, nil, false},
		{"*.example.com", []string{"a.b.example.com"}, []string{"a.b.example.com"}, true}, // any depth
		{"*.example.com", []string{"example.com"}, nil, false},
		{"a.example.com", []string{"*.example.com"}, []string{"a.example.com"}, true},
		{"*.example.com", []string{"*.a.example.com"}, []string{"*.a.example.com"}, true}, // the more specific
		{"*.a.example.com", []string{"*.example.com"}, []string{"*.a.example.com"}, true},
		{"A.example.com", []string{"a.example.com"}, nil, false}, // case-sensitive
		{"*.example.com", []string{"a.example.com", "a.example.com", "b.example.com"}, []string{"a.example.com", "b.example.com"}, true},
		// Edge cases beyond the brief: equal wildcards, unrelated wildcards, exact vs non-matching wildcard.
		{"*.example.com", []string{"*.example.com"}, []string{"*.example.com"}, true},
		{"*.example.com", []string{"*.other.com"}, nil, false},
		{"a.other.com", []string{"*.example.com"}, nil, false},
		{"", []string{"x.com", "x.com"}, []string{"x.com", "x.com"}, true}, // no listener hostname: kept as written
		{"*.example.com", []string{"a.example.com", "*.example.com"}, []string{"a.example.com", "*.example.com"}, true},
		{"*x.com", []string{"ax.com"}, nil, false}, // wildcard needs the "*." prefix
		{"a.example.com", []string{"b.example.com", "a.example.com"}, []string{"a.example.com"}, true},
	} {
		ok, eff := intersect(c.l, c.r)
		if ok != c.ok || (len(eff) != 0 || len(c.want) != 0) && !reflect.DeepEqual(eff, c.want) {
			t.Errorf("intersect(%q, %v) = %v, %v; want %v, %v", c.l, c.r, ok, eff, c.ok, c.want)
		}
	}
}
