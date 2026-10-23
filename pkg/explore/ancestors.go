package explore

import (
	"cmp"
	"slices"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Up-chain bounds (design §5.3).
const (
	// MaxPaths is the number of complete paths a walk returns.
	MaxPaths = 5
	// MaxDepth is the most routes on one path.
	MaxDepth = 32
	// MaxNodes is the most route visits one walk makes.
	MaxNodes = 256
)

// AncestorStep is one step of an up-chain. A Restricted step has a zero Ref.
type AncestorStep struct {
	Ref        objects.Ref
	Restricted bool
}

// AncestorPath runs from the root (a Gateway or ListenerSet) down to the
// route asked about. Cycle marks a path whose first step is a route that
// already appears later on it.
type AncestorPath struct {
	Steps []AncestorStep
	Cycle bool
}

// Ancestors walks up from route over reported edges, depth-first with
// parents sorted by (namespace, name). It stops after MaxPaths paths, at
// MaxDepth routes per path, or after MaxNodes route visits, whichever comes
// first, and then reports truncated; it never counts what remains.
//
// A path ends at a Gateway or ListenerSet whose status reported the route as
// accepted or not accepted. A route with no such root yields no path.
//
// An ancestor that visible rejects becomes a Restricted step with no
// identity, and the walk does not continue past it, so a hidden route's own
// parents are never named (design §7.4 rule 3). Paths that are identical
// after redaction are returned once.
func (ix *Index) Ancestors(route objects.Ref, visible func(objects.Ref) bool) (paths []AncestorPath, truncated bool) {
	paths, truncated, _ = ix.walkUp(route, visible)
	return paths, truncated
}

// walkUp is Ancestors plus the number of route visits made.
func (ix *Index) walkUp(route objects.Ref, visible func(objects.Ref) bool) ([]AncestorPath, bool, int) {
	w := &walker{ix: ix, visible: visible, seen: map[string]bool{}}
	w.visit(route, true)
	// Fully visible paths first; restricted ones keep their relative order.
	slices.SortStableFunc(w.paths, func(a, b AncestorPath) int {
		return cmp.Compare(btoi(restricted(a)), btoi(restricted(b)))
	})
	return w.paths, w.truncated, w.visits
}

type walker struct {
	ix        *Index
	visible   func(objects.Ref) bool
	stack     []objects.Ref // routes from the asked one up to the current
	paths     []AncestorPath
	seen      map[string]bool
	visits    int
	truncated bool
}

// candidate is one parent of a route: another route, or a root.
type candidate struct {
	ref   objects.Ref
	route bool
}

// parents returns the reported parents of route, sorted.
func (w *walker) parents(route objects.Ref) []candidate {
	var out []candidate
	for _, p := range w.ix.ParentRoutes[route] {
		out = append(out, candidate{p, true})
	}
	if id, ok := w.ix.Row(route); ok {
		for _, l := range w.ix.Routes[id].Parents {
			if l.Attachment != AttachmentAccepted && l.Attachment != AttachmentNotAccepted || l.Target.Kind == "HTTPRoute" {
				continue
			}
			out = append(out, candidate{l.Target, false})
		}
	}
	// Visible parents first in name order; hidden ones last and unordered, so
	// the order never reflects hidden names.
	hidden := map[objects.Ref]bool{}
	for _, c := range out {
		hidden[c.ref] = !w.visible(c.ref)
	}
	slices.SortStableFunc(out, func(a, b candidate) int {
		if ha, hb := hidden[a.ref], hidden[b.ref]; ha || hb {
			return cmp.Compare(btoi(ha), btoi(hb))
		}
		return compareRefs(a.ref, b.ref)
	})
	return slices.CompactFunc(out, func(a, b candidate) bool { return a == b })
}

// visit enters route; start is true for the route asked about.
func (w *walker) visit(route objects.Ref, start bool) {
	if w.truncated {
		return
	}
	if slices.Contains(w.stack, route) {
		w.emit(AncestorStep{Ref: route}, true)
		return
	}
	if !start && !w.visible(route) {
		w.emit(AncestorStep{Restricted: true}, false)
		return
	}
	if w.visits == MaxNodes {
		w.truncated = true
		return
	}
	w.visits++
	cands := w.parents(route)
	if len(w.stack) == MaxDepth && len(cands) > 0 {
		w.truncated = true
		return
	}
	w.stack = append(w.stack, route)
	for _, c := range cands {
		switch {
		case w.truncated:
		case c.route:
			w.visit(c.ref, false)
		case w.visible(c.ref):
			w.emit(AncestorStep{Ref: c.ref}, false)
		default:
			w.emit(AncestorStep{Restricted: true}, false)
		}
	}
	w.stack = w.stack[:len(w.stack)-1]
}

// emit records the path that has tail above the current stack.
func (w *walker) emit(tail AncestorStep, cycle bool) {
	steps := make([]AncestorStep, 0, len(w.stack)+1)
	steps = append(steps, tail)
	key := []byte{}
	for i := len(w.stack) - 1; i >= 0; i-- {
		steps = append(steps, AncestorStep{Ref: w.stack[i]})
	}
	for _, s := range steps {
		key = append(key, s.Ref.Group+"|"+s.Ref.Kind+"|"+s.Ref.Namespace+"|"+s.Ref.Name+";"...)
		if s.Restricted {
			key = append(key, '!')
		}
	}
	if cycle {
		key = append(key, 'c')
	}
	if w.seen[string(key)] {
		return
	}
	if len(w.paths) == MaxPaths {
		w.truncated = true
		return
	}
	w.seen[string(key)] = true
	w.paths = append(w.paths, AncestorPath{Steps: steps, Cycle: cycle})
}

func restricted(p AncestorPath) bool {
	return slices.ContainsFunc(p.Steps, func(s AncestorStep) bool { return s.Restricted })
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
