//go:build bench

package kgw23

import (
	"fmt"
	"runtime"
	"runtime/metrics"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
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

// fanIn is the re-review's dependency construction: roots roots, each accepted
// by gws Gateways of its own, delegate to one chain of depth routes whose last
// route wildcards into leaves leaves.
func fanIn(roots, gws, depth, leaves int) computed.DelegationInput {
	bs := make([]*hr, 0, roots+depth+leaves)
	chain := make([]*hr, depth)
	for i := range chain {
		chain[i] = route23("chain", fmt.Sprintf("c%03d", i)).owned()
		if i > 0 {
			chain[i].parentOf(chain[i-1].r.Ref)
		}
	}
	for i, b := range chain {
		if i+1 < depth {
			b.rule(at("/"), toName("chain", chain[i+1].r.Ref.Name))
		} else {
			b.rule(at("/"), toWildcard("leaves"))
		}
	}
	for i := range leaves {
		bs = append(bs, route23("leaves", fmt.Sprintf("l%05d", i)).owned().parentOf(chain[depth-1].r.Ref).rule(at("/"), service("leaves", "svc")))
	}
	tops := make([]*hr, roots)
	for i := range tops {
		tops[i] = route23("roots", fmt.Sprintf("r%02d", i)).top().rule(at("/"), toName("chain", chain[0].r.Ref.Name))
		chain[0].parentOf(tops[i].r.Ref)
	}
	bs = append(append(bs, chain...), tops...)
	in := world(bs...)
	for i := range in.Roots {
		rt := &in.Roots[i]
		rt.Deps, rt.Keys = []objects.Ref{rt.Route}, nil
		for g := range gws {
			ref := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: "infra", Name: fmt.Sprintf("%s-g%02d", rt.Route.Name, g)}
			rt.Deps = append(rt.Deps, ref)
			rt.Keys = append(rt.Keys, computed.ReportKey{Group: ref.Group, Kind: ref.Kind, Namespace: ref.Namespace, Name: ref.Name})
		}
	}
	return in
}

// fanInRetained is the live heap of fanIn's result.
func fanInRetained(t *testing.T, leaves int) uint64 {
	in := fanIn(20, 32, 90, leaves)
	before := liveHeap()
	res := delegate(in)
	total := liveHeap() - before
	if res.Cap != "" {
		t.Errorf("fan-in with %d leaves hit the %s cap", leaves, res.Cap)
	}
	runtime.KeepAlive(res)
	runtime.KeepAlive(in)
	return total
}

// TestBudgetDependencyAggregates: rows reached from many roots share their
// dependency sets, so the retained result stays small under every compute cap.
func TestBudgetDependencyAggregates(t *testing.T) {
	const mib = 1 << 20
	for _, leaves := range []int{2450, 9800} {
		total := fanInRetained(t, leaves)
		t.Logf("fan-in, 20 roots × 32 Gateways, chain 90, %d leaves: retained %.1f MiB", leaves, float64(total)/mib)
		if total > 64*mib {
			t.Errorf("fan-in with %d leaves retains %.1f MiB, want at most 64", leaves, float64(total)/mib)
		}
	}
}

// inheritFan is depth inheriting routes, each match adding 16 headers and 16
// query parameters, the last one wildcarding into leaves inheriting leaves of
// per matches each: every leaf merge copies the whole chain's keys.
func inheritFan(depth, leaves, per int) computed.DelegationInput {
	keyed := func(level int) objects.Match {
		m := prefix("/")
		for k := range 16 {
			m.Headers = append(m.Headers, kv(fmt.Sprintf("x-l%03d-h%02d", level, k), "v"))
			m.QueryParams = append(m.QueryParams, kv(fmt.Sprintf("l%03d-q%02d", level, k), "v"))
		}
		return m
	}
	bs := make([]*hr, depth, depth+leaves)
	for i := range bs {
		bs[i] = route23("chain", fmt.Sprintf("c%03d", i))
		if i == 0 {
			bs[i].top()
		} else {
			bs[i].owned().inherit().parentOf(bs[i-1].r.Ref)
		}
	}
	for i, b := range bs {
		to := toWildcard("leaves")
		if i+1 < depth {
			to = toName("chain", bs[i+1].r.Ref.Name)
		}
		b.rule([]objects.Match{keyed(i)}, to)
	}
	for i := range leaves {
		ms := make([]objects.Match, per)
		for j := range ms {
			ms[j] = prefix(fmt.Sprintf("/m%02d", j))
		}
		bs = append(bs, route23("leaves", fmt.Sprintf("l%05d", i)).owned().inherit().rule(ms, service("leaves", "svc")))
	}
	return world(bs...)
}

// TestBudgetInheritedMerges: a merge is charged by the keys it copies, so
// deep inherited matches reach the matcher cap before they exhaust memory.
func TestBudgetInheritedMerges(t *testing.T) {
	const mib = 1 << 20
	in := inheritFan(99, 9800, 8)
	before := liveHeap()
	res := delegate(in)
	total := liveHeap() - before
	t.Logf("inherit chain 99, 9,800 leaves × 8 matches: cap %q, retained %.1f MiB", res.Cap, float64(total)/mib)
	if total > 64*mib {
		t.Errorf("inherited merges retain %.1f MiB, want at most 64", float64(total)/mib)
	}
	runtime.KeepAlive(res)
	runtime.KeepAlive(in)
}

// hub is roots roots, each delegating to one hub that delegates to one leaf:
// the hub→leaf row gains a new root dependency from every root.
func hub(roots int) computed.DelegationInput {
	h := route23("hub", "hub").owned().rule(at("/"), toName("hub", "leaf"))
	bs := []*hr{h, route23("hub", "leaf").owned().rule(at("/"), service("hub", "svc"))}
	for i := range roots {
		bs = append(bs, route23("roots", fmt.Sprintf("r%05d", i)).top().rule(at("/"), toName("hub", "hub")))
	}
	return world(bs...)
}

// peakHeap is the most heap in use above the starting live heap while f
// runs, sampled every 50µs rather than after a GC.
func peakHeap(f func()) uint64 {
	base := liveHeap()
	s := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	var peak atomic.Uint64
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			metrics.Read(s)
			if v := s[0].Value.Uint64(); v > peak.Load() {
				peak.Store(v)
			}
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Microsecond):
			}
		}
	}()
	f()
	close(stop)
	<-done
	return peak.Load() - min(base, peak.Load())
}

// TestBudgetHubPeak: a growing aggregate keeps no superseded prefixes, so the
// peak heap of a run stays small while one row gains thousands of deps.
func TestBudgetHubPeak(t *testing.T) {
	const mib = 1 << 20
	in := hub(9000)
	var res computed.DelegationResult
	peak := peakHeap(func() { res = delegate(in) })
	t.Logf("hub, 9,000 roots: cap %q, peak heap %.1f MiB during the run", res.Cap, float64(peak)/mib)
	if res.Cap != "" {
		t.Errorf("hub hit the %s cap", res.Cap)
	}
	if peak > 64*mib {
		t.Errorf("hub peaks at %.1f MiB, want at most 64", float64(peak)/mib)
	}
	runtime.KeepAlive(in)
}
