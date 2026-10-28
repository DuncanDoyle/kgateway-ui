// Port of kgw23 for v2.4.7; keep in sync.

package kgw24

import (
	"cmp"
	"slices"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// edgeRows are the rows of one (parent, child) edge whose fit ran.
type edgeRows struct {
	parent objects.Ref
	rows   []*rowAcc
}

// tagged is a retained context: row r's context i, with its tag.
type tagged struct {
	tag ctxTag
	r   *rowAcc
	i   int
}

// project builds view C per child from the retained contexts (rules §5.7,
// §6). An edge exists where some row of (parent, child) ran the fit:
// a forbidden, cycle or unresolved row has no effective matcher. It runs
// after rows, so the rows hold their final contexts and deps.
func (w *walker) project() map[objects.Ref]computed.ChildEffective {
	// Rows are in parent order, so each child's edges come out sorted by parent.
	byChild := make([][]edgeRows, len(w.routes))
	n := 0
	for _, r := range w.acc.sorted {
		ci, k := r.child, r.row.Key
		if ci < 0 || r.count(computed.OutcomeAttached)+r.count(computed.OutcomeDropped)+r.count(computed.OutcomeRejectedHostnames) == 0 {
			continue
		}
		es := byChild[ci]
		if len(es) == 0 {
			n++
		}
		if len(es) > 0 && es[len(es)-1].parent == k.Parent {
			es[len(es)-1].rows = append(es[len(es)-1].rows, r)
			continue
		}
		byChild[ci] = append(es, edgeRows{parent: k.Parent, rows: []*rowAcc{r}})
	}
	if n == 0 {
		return nil
	}
	out := make(map[objects.Ref]computed.ChildEffective, n)
	for ci, es := range byChild {
		if len(es) == 0 {
			continue
		}
		mode := computed.ModeDefault
		if w.routes[ci].View.InheritParentMatcher {
			mode = computed.ModeInherit
		}
		ce := computed.ChildEffective{Truncated: len(es) > w.b.EffectiveEdges}
		ce.Edges = make([]computed.EffectiveEdge, 0, min(len(es), w.b.EffectiveEdges))
		for _, er := range es[:min(len(es), w.b.EffectiveEdges)] {
			e := w.edge(er.rows)
			e.Parent, e.Mode = er.parent, mode
			ce.Edges = append(ce.Edges, e)
		}
		out[w.routes[ci].Ref] = ce
	}
	return out
}

// edge merges the retained fit contexts of rows in traversal order, one per
// frame, at most ContextsPerEdge. Contexts is never nil, so an edge with none
// kept is sent as empty and truncated, never as complete. When the merge is a
// prefix of one row's contexts, it shares that row's slice (contexts are read-only).
func (w *walker) edge(rows []*rowAcc) computed.EffectiveEdge {
	var e computed.EffectiveEdge
	// Quirk 1 hands a child to several refs under one frame: one evaluation,
	// one context, the first in traversal order.
	if w.projSeen == nil {
		w.projSeen = map[uint32]int{}
	}
	clear(w.projSeen)
	all := w.projBuf[:0]
	for _, r := range rows {
		e.ContextsTruncated = e.ContextsTruncated || r.row.ContextsTruncated
		if len(e.Deps) != len(r.row.Deps) || len(e.Deps) > 0 && &e.Deps[0] != &r.row.Deps[0] {
			e.Deps = w.acc.deps.Union(e.Deps, r.row.Deps)
		}
		for i := range r.row.Contexts {
			switch r.row.Contexts[i].Outcome {
			case computed.OutcomeAttached, computed.OutcomeDropped, computed.OutcomeRejectedHostnames:
			default:
				continue
			}
			t := tagged{r.tags[i], r, i}
			if t.tag.frame == 0 {
				all = append(all, t)
			} else if j, ok := w.projSeen[t.tag.frame]; !ok {
				w.projSeen[t.tag.frame] = len(all)
				all = append(all, t)
			} else if t.tag.seq < all[j].tag.seq {
				all[j] = t
			}
		}
	}
	if len(rows) > 1 {
		slices.SortFunc(all, func(a, b tagged) int { return cmp.Compare(a.tag.seq, b.tag.seq) })
	}
	w.projBuf = all
	if len(all) > w.b.ContextsPerEdge {
		all, e.ContextsTruncated = all[:w.b.ContextsPerEdge], true
	}
	if len(all) > 0 && all[len(all)-1].i == len(all)-1 && !slices.ContainsFunc(all, func(t tagged) bool { return t.r != all[0].r }) {
		// One row's first contexts, in order: share them.
		e.Contexts = all[0].r.row.Contexts[:len(all):len(all)]
		return e
	}
	e.Contexts = make([]computed.Context, len(all))
	for j, t := range all {
		e.Contexts[j] = t.r.row.Contexts[t.i]
	}
	return e
}
