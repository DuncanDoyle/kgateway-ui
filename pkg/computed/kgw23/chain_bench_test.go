//go:build bench

package kgw23

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
)

// chain is a valid delegation chain of n routes, each with one / match and one ref.
func chain(n int) computed.DelegationInput {
	bs := make([]*hr, n)
	for i := range bs {
		bs[i] = route23("chain", fmt.Sprintf("r%05d", i))
	}
	bs[0].top()
	for i, b := range bs {
		if i > 0 {
			b.parentOf(bs[i-1].r.Ref)
		}
		if i+1 < n {
			b.rule(at("/"), toName("chain", bs[i+1].r.Ref.Name))
		} else {
			b.rule(at("/"), service("chain", "svc"))
		}
	}
	return world(bs...)
}

// liveHeap is HeapAlloc after a full GC.
func liveHeap() uint64 {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// dropDeps clears every dependency set of res, leaving paths and the rest.
func dropDeps(res *computed.DelegationResult) {
	for i := range res.Rows {
		r := &res.Rows[i]
		r.Deps, r.KeyDeps = nil, nil
		for j := range r.Contexts {
			r.Contexts[j].Deps = nil
		}
	}
	for i := range res.Signals {
		res.Signals[i].Deps = nil
	}
	for k, ce := range res.Effective {
		for i := range ce.Edges {
			e := &ce.Edges[i]
			e.Deps = nil
			for j := range e.Contexts {
				e.Contexts[j].Deps = nil
			}
		}
		res.Effective[k] = ce
	}
}

// retained is the live heap of a chain's result under depth cap depth, in
// total and without its dependency sets.
func retained(n, depth int) (total, noDeps uint64) {
	in := chain(n)
	in.Bounds.Depth = depth
	before := liveHeap()
	res := delegate(in)
	total = liveHeap() - before
	dropDeps(&res)
	noDeps = liveHeap() - before
	runtime.KeepAlive(res)
	runtime.KeepAlive(in)
	return total, noDeps
}

// TestBudgetDeepChain: with the depth cap lifted, paths share prefixes, so the
// result without its dependency sets grows linearly; at the default cap the
// whole result stays small.
func TestBudgetDeepChain(t *testing.T) {
	const mib = 1 << 20
	var half uint64
	for _, n := range []int{1500, 3000} {
		total, noDeps := retained(n, n)
		t.Logf("chain %d, no depth cap: retained %.1f MiB, %.1f MiB without dependency sets", n, float64(total)/mib, float64(noDeps)/mib)
		if n == 1500 {
			half = noDeps
			continue
		}
		if noDeps > 16*mib || noDeps > 3*half {
			t.Errorf("chain of %d retains %.1f MiB without dependency sets (%.1f MiB at half depth), want linear and at most 16 MiB",
				n, float64(noDeps)/mib, float64(half)/mib)
		}
	}
	total, _ := retained(3000, computed.DefaultBounds.Depth)
	t.Logf("chain 3000, depth cap %d: retained %.2f MiB", computed.DefaultBounds.Depth, float64(total)/mib)
	if total > 4*mib {
		t.Errorf("chain of 3000 at the default depth cap retains %.2f MiB, want at most 4", float64(total)/mib)
	}
}
