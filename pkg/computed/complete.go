package computed

import (
	"slices"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Completeness says which aggregates of a delegation result are complete
// (rules §8.5). An aggregate on a route in a reach closure is incomplete:
// the missing work may reach it. The zero value has every route complete.
type Completeness struct {
	// all is set when the edge cap was hit: every route is incomplete.
	all bool
	// in are the routes of every reach closure.
	in map[objects.Ref]struct{}
	// keys are the root report keys whose view A outcome is unknown.
	keys map[RouteKey]struct{}
}

// NewCompleteness computes r's reach closures: from every uncertain point's
// route over r.Reach, transitively (rules §8.5). Each edge is walked once.
func NewCompleteness(r DelegationResult) Completeness {
	if r.Cap == CapEdges {
		return Completeness{all: true}
	}
	var c Completeness
	if len(r.Uncertain) > 0 {
		seeds := make([]objects.Ref, len(r.Uncertain))
		for i, u := range r.Uncertain {
			seeds[i] = u.Route
		}
		c.in = closure(r.Reach, seeds, nil)
	}
	if len(r.UncertainKeys) > 0 {
		c.keys = make(map[RouteKey]struct{}, len(r.UncertainKeys))
		for _, k := range r.UncertainKeys {
			c.keys[k] = struct{}{}
		}
	}
	return c
}

// Incomplete reports whether route is in a reach closure.
func (c Completeness) Incomplete(route objects.Ref) bool {
	if c.all {
		return true
	}
	_, ok := c.in[route]
	return ok
}

// IncompleteKey reports whether a check on route under key is incomplete:
// route is in a reach closure, or key is one of route's Root.UncertainKeys.
// An uncertain key does not seed a closure, since the evaluation below a root
// is the same under every key; only the root's own report under it is uncertain.
func (c Completeness) IncompleteKey(route objects.Ref, key ReportKey) bool {
	if c.Incomplete(route) {
		return true
	}
	_, ok := c.keys[RouteKey{Route: route, Key: key}]
	return ok
}

// closure is every route reachable from seeds over reach, seeds included.
// reach is sorted by Parent, so a route's edges are one run; each run is
// walked once, when its route is first reached. visit, when set, is called
// per edge walked.
func closure(reach []Edge, seeds []objects.Ref, visit func()) map[objects.Ref]struct{} {
	in := make(map[objects.Ref]struct{}, len(seeds))
	queue := make([]objects.Ref, 0, len(seeds))
	for _, s := range seeds {
		if _, ok := in[s]; !ok {
			in[s] = struct{}{}
			queue = append(queue, s)
		}
	}
	for len(queue) > 0 {
		r := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		i, _ := slices.BinarySearchFunc(reach, r, func(e Edge, t objects.Ref) int { return e.Parent.Compare(t) })
		for ; i < len(reach) && reach[i].Parent == r; i++ {
			if visit != nil {
				visit()
			}
			if c := reach[i].Child; !has(in, c) {
				in[c] = struct{}{}
				queue = append(queue, c)
			}
		}
	}
	return in
}

func has(m map[objects.Ref]struct{}, r objects.Ref) bool {
	_, ok := m[r]
	return ok
}
