package paths_test

import (
	"slices"
	"testing"
	"time"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/paths"
)

// counted returns a build func that counts its calls in n.
func counted(n *int, build func() *paths.Topology) func() *paths.Topology {
	return func() *paths.Topology { *n++; return build() }
}

// TestMemoTopologyKey: a topology is reused only on the exact snapshot and
// computed-views pointers of its install; anything else rebuilds.
func TestMemoTopologyKey(t *testing.T) {
	var m paths.Memo
	n := 0
	build := counted(&n, func() *paths.Topology { return paths.Build(paths.Input{}) })
	s1, v1 := &cache.Snapshot{}, &computed.Views{}
	k := paths.TopoKey{Install: "a", Snapshot: s1, Views: v1}
	t1, ok := m.Topology(k, build)
	if again, ok2 := m.Topology(k, build); !ok || !ok2 || again != t1 || n != 1 {
		t.Fatalf("reuse: ok %v %v, same %v, builds %d", ok, ok2, again == t1, n)
	}
	// An equal but distinct snapshot or views value is another generation.
	for _, other := range []paths.TopoKey{{Install: "a", Snapshot: &cache.Snapshot{}, Views: v1}, {Install: "a", Snapshot: s1, Views: &computed.Views{}},
		{Install: "b", Snapshot: s1, Views: v1}} {
		before := n
		if _, ok := m.Topology(other, build); !ok || n != before+1 {
			t.Fatalf("%+v: not rebuilt", other)
		}
	}
	// Install a holds one generation: its first key was replaced.
	before := n
	if m.Topology(k, build); n != before+1 {
		t.Fatal("old generation kept")
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
	k := paths.TopoKey{Install: "a", Snapshot: snap, Views: i.Computed}

	var big paths.Memo
	memoised, ok := big.Topology(k, build)
	if !ok {
		t.Fatalf("%d bytes not kept under the default cap", memoised.Bytes())
	}
	small := paths.Memo{Cap: memoised.Bytes() - 1}
	if _, ok := small.Topology(paths.TopoKey{Install: "a"}, func() *paths.Topology { return paths.Build(paths.Input{}) }); !ok {
		t.Fatal("empty topology not kept")
	}
	before := n
	over, ok := small.Topology(k, build)
	if ok || n != before+1 {
		t.Fatalf("over the cap: kept %v, builds %d", ok, n-before)
	}
	if _, ok := small.Topology(k, build); ok || n != before+2 {
		t.Fatal("an over-cap topology was kept")
	}
	if _, ok := small.Topology(paths.TopoKey{Install: "a"}, func() *paths.Topology { n++; return paths.Build(paths.Input{}) }); !ok || n != before+3 {
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
	now := testutil.Now
	m := paths.Memo{Now: func() time.Time { return now }}
	snap := &cache.Snapshot{}
	top, _ := m.Topology(paths.TopoKey{Install: "a", Snapshot: snap}, func() *paths.Topology { return paths.Build(paths.Input{}) })
	n := 0
	next := engine.Deadline{At: testutil.Now.Add(10 * time.Second), Strict: true}
	project := func() *paths.Projection { n++; return &paths.Projection{Next: next} }
	pub := &engine.Published{Model: &engine.Model{Snapshot: snap, Next: next}}

	p1 := m.Projection(pub, "a", top, project)
	if m.Projection(pub, "a", top, project) != p1 || n != 1 {
		t.Fatalf("not reused: %d builds", n)
	}
	confirmed := &engine.Published{Model: pub.Model, ConfirmedAt: now}
	now = next.At.Add(-time.Nanosecond)
	if m.Projection(confirmed, "a", top, project) != p1 || n != 1 {
		t.Fatalf("not reused across a confirmation or before Next: %d builds", n)
	}
	// Strict expiry: not reused at Next itself.
	now = next.At
	p2 := m.Projection(confirmed, "a", top, project)
	if p2 == p1 || n != 2 {
		t.Fatalf("reused at Next: %d builds", n)
	}
	now = testutil.Now
	republished := &engine.Published{Model: &engine.Model{Snapshot: snap}}
	if m.Projection(republished, "a", top, project) == p2 || n != 3 {
		t.Fatalf("reused across publications: %d builds", n)
	}
	top2, _ := m.Topology(paths.TopoKey{Install: "a", Snapshot: &cache.Snapshot{}}, func() *paths.Topology { return paths.Build(paths.Input{}) })
	if m.Projection(republished, "a", top2, project); n != 4 {
		t.Fatalf("reused on a new topology: %d builds", n)
	}
	other := paths.Build(paths.Input{})
	m.Projection(republished, "a", other, project)
	m.Projection(republished, "a", other, project)
	if n != 6 {
		t.Fatalf("an unheld topology's projection was memoised: %d builds", n)
	}
	if m.Projection(republished, "a", top2, project); n != 6 {
		t.Fatalf("the held topology's projection was dropped: %d builds", n)
	}
}
