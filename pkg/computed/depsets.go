package computed

import (
	"encoding/binary"
	"hash/maphash"
	"slices"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// DepSets hash-conses the finished dependency sets of one delegation run, so
// equal sets share one backing slice however often rows and contexts hold
// them (rules §5.7). Aggregates grow in a DepAcc and are interned only when
// finished, so their superseded prefixes are never kept. Interned slices are
// read-only. The zero value is not usable; call NewDepSets.
type DepSets struct {
	seed maphash.Seed
	// byHash holds the canonical sets by content hash; same maps a slice
	// already seen to its canonical set, and num numbers the canonical sets.
	byHash map[uint64][][]objects.Ref
	same   map[depID][]objects.Ref
	num    map[depID]uint32
	// done memoises Finish by its sorted member numbers.
	done map[string][]objects.Ref
}

// DepAcc gathers the sets of one growing aggregate; DepSets.Finish unites them.
type DepAcc struct{ sets [][]objects.Ref }

// Add adds s, skipping an empty set or a repeat of the last one.
func (a *DepAcc) Add(s []objects.Ref) {
	if n := len(a.sets); len(s) == 0 || n > 0 && idOf(a.sets[n-1]) == idOf(s) {
		return
	}
	a.sets = append(a.sets, s)
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
		num: map[depID]uint32{}, done: map[string][]objects.Ref{}}
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
		t.num[id] = uint32(len(t.num)) //nolint:gosec // one per distinct set
	}
	t.same[id] = c
	return c
}

// Finish is the canonical sorted union of a's sets; a is emptied.
func (t *DepSets) Finish(a *DepAcc) []objects.Ref {
	sets := a.sets
	a.sets = nil
	switch len(sets) {
	case 0:
		return nil
	case 1:
		if s := sets[0]; slices.IsSortedFunc(s, objects.Ref.Compare) && !hasAdjacentDup(s) {
			return t.Intern(s)
		}
	}
	nums := make([]uint32, len(sets))
	for i, s := range sets {
		nums[i] = t.num[idOf(t.Intern(s))]
	}
	slices.Sort(nums)
	nums = slices.Compact(nums)
	key := make([]byte, 0, 4*len(nums))
	for _, n := range nums {
		key = binary.LittleEndian.AppendUint32(key, n)
	}
	if u, ok := t.done[string(key)]; ok {
		return u
	}
	n := 0
	for _, s := range sets {
		n += len(s)
	}
	all := make([]objects.Ref, 0, n)
	for _, s := range sets {
		all = append(all, s...)
	}
	slices.SortFunc(all, objects.Ref.Compare)
	u := t.Intern(slices.Clone(slices.Compact(all))) // a right-sized copy: all may be far larger
	t.done[string(key)] = u
	return u
}

func hasAdjacentDup(s []objects.Ref) bool {
	for i := 1; i < len(s); i++ {
		if s[i] == s[i-1] {
			return true
		}
	}
	return false
}
