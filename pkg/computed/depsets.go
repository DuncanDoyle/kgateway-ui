package computed

import (
	"hash/maphash"
	"slices"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// DepSets hash-conses the sorted dependency sets of one delegation run, so
// equal sets share one backing slice however often rows and contexts hold
// them (rules §5.7). Interned slices are read-only. The zero value is not
// usable; call NewDepSets.
type DepSets struct {
	seed maphash.Seed
	// byHash holds the canonical sets by content hash; same maps a slice
	// already seen to its canonical set.
	byHash map[uint64][][]objects.Ref
	same   map[depID][]objects.Ref
	// unions memoises Union by its canonical operands.
	unions map[[2]depID][]objects.Ref
}

// depID names a slice by its first element and length.
type depID struct {
	p *objects.Ref
	n int
}

func idOf(s []objects.Ref) depID {
	if len(s) == 0 {
		return depID{}
	}
	return depID{&s[0], len(s)}
}

// NewDepSets returns an empty intern table.
func NewDepSets() *DepSets {
	return &DepSets{seed: maphash.MakeSeed(), byHash: map[uint64][][]objects.Ref{}, same: map[depID][]objects.Ref{},
		unions: map[[2]depID][]objects.Ref{}}
}

// Intern returns the canonical set equal to s, which becomes canonical
// itself when it is the first. s is never written; an empty s is returned as is.
func (t *DepSets) Intern(s []objects.Ref) []objects.Ref {
	if len(s) == 0 {
		return s
	}
	id := idOf(s)
	if c, ok := t.same[id]; ok {
		return c
	}
	// An order-dependent fold of each ref's hash; equality is checked below.
	sum := uint64(len(s))
	for _, r := range s {
		sum = sum*1099511628211 ^ maphash.Comparable(t.seed, r)
	}
	c := s
	if i := slices.IndexFunc(t.byHash[sum], func(x []objects.Ref) bool { return slices.Equal(x, s) }); i >= 0 {
		c = t.byHash[sum][i]
	} else {
		t.byHash[sum] = append(t.byHash[sum], s)
	}
	t.same[id] = c
	return c
}

// Union is the canonical union of the sorted, duplicate-free sets a and b.
func (t *DepSets) Union(a, b []objects.Ref) []objects.Ref {
	a, b = t.Intern(a), t.Intern(b)
	switch {
	case len(a) == 0:
		return b
	case len(b) == 0 || idOf(a) == idOf(b):
		return a
	}
	k := [2]depID{idOf(a), idOf(b)}
	if u, ok := t.unions[k]; ok {
		return u
	}
	u := t.Intern(union(a, b))
	t.unions[k] = u
	return u
}

// union merges sorted sets; it returns a when b adds nothing, and never writes either.
func union(a, b []objects.Ref) []objects.Ref {
	if !slices.ContainsFunc(b, func(r objects.Ref) bool { _, ok := slices.BinarySearchFunc(a, r, objects.Ref.Compare); return !ok }) {
		return a
	}
	out := make([]objects.Ref, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch c := a[i].Compare(b[j]); {
		case c < 0:
			out = append(out, a[i])
			i++
		case c > 0:
			out = append(out, b[j])
			j++
		default:
			out = append(out, a[i])
			i, j = i+1, j+1
		}
	}
	out = append(out, a[i:]...)
	return append(out, b[j:]...)
}
