package paths_test

import (
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
	"github.com/DuncanDoyle/snellius/pkg/scopeverify"
)

// counted returns a build func that counts its calls in n.
func counted(n *int, build func() *paths.Topology) func() *paths.Topology {
	return func() *paths.Topology { *n++; return build() }
}

func empty() *paths.Topology { return paths.Build(paths.Input{}) }

// source is a snapshot source whose HTTPRoutes print as h.
func source(h uint64) cache.Fingerprint {
	return cache.Fingerprint{Kinds: map[string]cache.KindPrint{"httproutes": {Count: 1, Hash: h}}}
}

// TestMemoTopologyKey: a topology is reused for an equal source and views
// generation of its install, whatever the Seq; anything else rebuilds, and
// the install keeps one generation.
func TestMemoTopologyKey(t *testing.T) {
	m := paths.NewMemo(nil)
	n := 0
	build := counted(&n, empty)
	k := paths.TopoKey{Install: "a", Source: source(1), Views: 7, Seq: 1}
	t1, ok := m.Topology(k, build)
	// An equal source in a new value, as each snapshot carries, is the same generation.
	again, ok2 := m.Topology(paths.TopoKey{Install: "a", Source: source(1), Views: 7, Seq: 2}, build)
	if !ok || !ok2 || again != t1 || n != 1 {
		t.Fatalf("reuse: ok %v %v, same %v, builds %d", ok, ok2, again == t1, n)
	}
	for _, other := range []paths.TopoKey{{Install: "a", Source: source(2), Views: 7, Seq: 3}, {Install: "a", Source: source(1), Views: 8, Seq: 4},
		{Install: "b", Source: source(1), Views: 7, Seq: 5}} {
		before := n
		if _, ok := m.Topology(other, build); !ok || n != before+1 {
			t.Fatalf("%+v: not rebuilt", other)
		}
	}
	// Install a holds one generation: its first key was replaced.
	before := n
	if m.Topology(paths.TopoKey{Install: "a", Source: source(1), Views: 7, Seq: 6}, build); n != before+1 {
		t.Fatal("old generation kept")
	}
}

// TestMemoTopologyOlderBuildNotKept: a slow build for an older publication
// does not replace the newer generation stored while it ran.
func TestMemoTopologyOlderBuildNotKept(t *testing.T) {
	m := paths.NewMemo(nil)
	newer := paths.TopoKey{Install: "a", Source: source(2), Views: 2, Seq: 5}
	var stored *paths.Topology
	old, ok := m.Topology(paths.TopoKey{Install: "a", Source: source(1), Views: 1, Seq: 3}, func() *paths.Topology {
		stored, _ = m.Topology(newer, empty)
		return empty()
	})
	if !ok || old == stored {
		t.Fatal("older build")
	}
	if got, _ := m.Topology(newer, func() *paths.Topology { t.Fatal("newer generation replaced"); return nil }); got != stored {
		t.Fatal("newer generation not held")
	}
}

// computer recomputes one engine over a snapshot that each Compute copies,
// as the cache returns a new snapshot every time.
type computer struct {
	snap *cache.Snapshot
	e    *engine.Engine
}

func (c *computer) Snapshot() *cache.Snapshot { s := *c.snap; return &s }

func newComputer(snap *cache.Snapshot) *computer {
	c := &computer{snap: snap}
	c.e = engine.New(engine.Config{Snapshots: c, Served: testutil.ServedUpstream(), Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour), Checks: health.Upstream(),
		Now: func() time.Time { return testutil.Now }})
	return c
}

// topology computes and returns the memo's topology for the only install.
func (c *computer) topology(t *testing.T, m *paths.Memo) (*paths.Topology, uint64) {
	t.Helper()
	md := c.e.Compute()
	i := &md.Installs[0]
	k := paths.TopoKey{Install: i.Resolved.ID(), Source: md.Snapshot.Source, Views: i.Computed.Generation, Seq: md.Seq}
	top, ok := m.Topology(k, func() *paths.Topology {
		return paths.Build(paths.Input{Index: i.Index, Views: i.Computed, Decode: objects.Direct{}, Delegates: i.Rules.Delegator() != nil})
	})
	if !ok {
		t.Fatal("not memoised")
	}
	return top, i.Computed.Generation
}

// withRoute is a copy of snap with u in place of the HTTPRoute of the same ref.
func withRoute(t *testing.T, snap *cache.Snapshot, u *unstructured.Unstructured) *cache.Snapshot {
	t.Helper()
	s := *snap
	s.Objects = maps.Clone(s.Objects)
	for g, us := range s.Objects {
		if i := slices.IndexFunc(us, func(x *unstructured.Unstructured) bool { return objects.RefOf(x) == objects.RefOf(u) }); i >= 0 {
			us = slices.Clone(us)
			us[i] = u
			s.Objects[g] = us
			return &s
		}
	}
	t.Fatalf("no %s", objects.RefOf(u))
	return nil
}

// TestMemoTopologyAcrossComputes: a recompute over an unchanged source and
// views reuses the topology; a weight edit and a view A change each rebuild it.
func TestMemoTopologyAcrossComputes(t *testing.T) {
	weighted := func(w int64) *unstructured.Unstructured {
		return accepted("r", map[string]any{"name": "a", "port": int64(8080), "weight": w})
	}
	snap := worldSnap(t, "v2.4.7", nil, edge("http"), weighted(1))
	snap.Source = source(1)
	c, m := newComputer(snap), paths.NewMemo(nil)
	t1, g1 := c.topology(t, m)
	if t2, g2 := c.topology(t, m); t2 != t1 || g2 != g1 {
		t.Fatalf("unchanged: rebuilt %v, generations %d %d", t2 != t1, g1, g2)
	}
	// The cache's source changes with the route's resourceVersion.
	c.snap = withRoute(t, c.snap, weighted(5))
	c.snap.Source = source(2)
	t3, g3 := c.topology(t, m)
	if b, _ := t3.Backend(t3.Rows[0]); t3 == t1 || b.Weight != 5 {
		t.Fatalf("weight edit: rebuilt %v, weight %d", t3 != t1, b.Weight)
	}
	// A second listener changes view A; the source is held equal to show the generation alone rebuilds.
	c.snap = worldSnap(t, "v2.4.7", nil, edge("http", "more"), weighted(5))
	c.snap.Source = source(2)
	t4, g4 := c.topology(t, m)
	if t4 == t3 || g4 == g3 || len(t4.Rows) != 2 {
		t.Fatalf("view A change: rebuilt %v, generations %d %d, %d rows", t4 != t3, g3, g4, len(t4.Rows))
	}
}

// TestMemoTopologyCap: over the cap a topology is not kept, the install's
// older generation is dropped, and a query over it pages the same rows as
// over a memoised one.
func TestMemoTopologyCap(t *testing.T) {
	if paths.TopologyCap != 2<<20 {
		t.Fatalf("cap %d", paths.TopologyCap)
	}
	snap := endpointsSnap(t, nil, nil, edge("one", "two"), accepted("r", svc("a"), svc("b")), accepted("s", svc("c")))
	_, i := buildWith(t, snap)
	in := paths.Input{Index: i.Index, Views: i.Computed, Decode: objects.Direct{}, Delegates: i.Rules.Delegator() != nil}
	n := 0
	build := counted(&n, func() *paths.Topology { return paths.Build(in) })
	k := paths.TopoKey{Install: "a", Source: source(1), Views: 1, Seq: 2}
	small := paths.TopoKey{Install: "a", Seq: 1}

	memoised, ok := paths.NewMemo(nil).Topology(k, build)
	if !ok {
		t.Fatalf("%d bytes not kept under the default cap", memoised.Bytes())
	}
	m := paths.NewMemo(nil)
	m.SetCap(memoised.Bytes() - 1)
	if _, ok := m.Topology(small, empty); !ok {
		t.Fatal("empty topology not kept")
	}
	before := n
	over, ok := m.Topology(k, build)
	if ok || n != before+1 {
		t.Fatalf("over the cap: kept %v, builds %d", ok, n-before)
	}
	if _, ok := m.Topology(k, build); ok || n != before+2 {
		t.Fatal("an over-cap topology was kept")
	}
	if _, ok := m.Topology(small, counted(&n, empty)); !ok || n != before+3 {
		t.Fatal("the older generation was kept past an over-cap one")
	}

	in2 := paths.ProjectInput{Model: i, EndpointsCap: "on", Snapshot: snap}
	pm, po := paths.Project(memoised, in2), paths.Project(over, in2)
	for q := (paths.Query{Limit: 2}); ; {
		a, b := run(t, memoised, pm, q, nil), run(t, over, po, q, nil)
		if !slices.Equal(a.Rows, b.Rows) || a.Next != b.Next || a.Summary.Paths != b.Summary.Paths || a.Summary.Paths != 6 {
			t.Fatalf("memoised %+v, unmemoised %+v", a, b)
		}
		if q.Cursor = a.Next; q.Cursor == "" {
			break
		}
	}
}

// TestMemoProjection: a projection is reused within one model publication
// for one memoised topology, kept across a confirmation of that model, and
// rebuilt at its Next, on a new publication with the same snapshot, and on a
// new topology. A topology the memo does not hold is never memoised.
func TestMemoProjection(t *testing.T) {
	clk := clocktesting.NewFakePassiveClock(testutil.Now)
	m := paths.NewMemo(clk)
	top, _ := m.Topology(paths.TopoKey{Install: "a", Source: source(1), Seq: 1}, empty)
	n := 0
	next := engine.Deadline{At: testutil.Now.Add(10 * time.Second), Strict: true}
	project := func() *paths.Projection { n++; return &paths.Projection{Next: next} }
	pub := &engine.Published{Model: &engine.Model{Seq: 1, Next: next}}

	p1 := m.Projection(pub, "a", top, project)
	if m.Projection(pub, "a", top, project) != p1 || n != 1 {
		t.Fatalf("not reused: %d builds", n)
	}
	confirmed := &engine.Published{Model: pub.Model, ConfirmedAt: testutil.Now}
	clk.SetTime(next.At.Add(-time.Nanosecond))
	if m.Projection(confirmed, "a", top, project) != p1 || n != 1 {
		t.Fatalf("not reused across a confirmation or before Next: %d builds", n)
	}
	// Strict expiry: not reused at Next itself.
	clk.SetTime(next.At)
	p2 := m.Projection(confirmed, "a", top, project)
	if p2 == p1 || n != 2 {
		t.Fatalf("reused at Next: %d builds", n)
	}
	clk.SetTime(testutil.Now)
	republished := &engine.Published{Model: &engine.Model{Seq: 2}}
	if m.Projection(republished, "a", top, project) == p2 || n != 3 {
		t.Fatalf("reused across publications: %d builds", n)
	}
	// An older publication's projection does not replace the newer one.
	m.Projection(pub, "a", top, project)
	if m.Projection(republished, "a", top, project); n != 4 {
		t.Fatalf("an older projection replaced a newer one: %d builds", n)
	}
	top2, _ := m.Topology(paths.TopoKey{Install: "a", Source: source(2), Seq: 2}, empty)
	if m.Projection(republished, "a", top2, project); n != 5 {
		t.Fatalf("reused on a new topology: %d builds", n)
	}
	other := empty()
	m.Projection(republished, "a", other, project)
	m.Projection(republished, "a", other, project)
	if n != 7 {
		t.Fatalf("an unheld topology's projection was memoised: %d builds", n)
	}
	if m.Projection(republished, "a", top2, project); n != 7 {
		t.Fatalf("the held topology's projection was dropped: %d builds", n)
	}
}

// TestMemoConcurrent runs Topology and Projection from many goroutines over
// a few installs and generations; run it with -race. Every result is a
// topology built for its key and a projection of its model.
func TestMemoConcurrent(t *testing.T) {
	m := paths.NewMemo(nil)
	models := []*engine.Model{{Seq: 1}, {Seq: 2}, {Seq: 3}}
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Go(func() {
			for i := range 200 {
				gen := uint64((g + i) % 3)
				install := fmt.Sprint("i", i%2)
				k := paths.TopoKey{Install: install, Source: source(gen), Views: gen, Seq: gen + 1}
				top, _ := m.Topology(k, empty)
				model := models[gen]
				p := m.Projection(&engine.Published{Model: model}, install, top, func() *paths.Projection {
					return &paths.Projection{Next: model.Next}
				})
				if top == nil || p == nil {
					t.Error("nil result")
					return
				}
			}
		})
	}
	wg.Wait()
}
