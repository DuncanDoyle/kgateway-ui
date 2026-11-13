package paths

import (
	"slices"
	"sync"
	"time"

	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/engine"
)

// TopologyCap is the topology memo's cap per install generation, in Bytes (design §14).
const TopologyCap = 2 << 20

// TopoKey is a topology's generation: its install's snapshot source and
// computed views generation, and the publication that asked for it.
type TopoKey struct {
	Install string
	// Source is the snapshot's Source; equal sources describe the same objects.
	Source cache.Fingerprint
	// Views is the install's computed.Views.Generation; 0 without views.
	Views uint64
	// Seq is the publication's Model.Seq; an older key never replaces a newer one.
	Seq uint64
}

// same reports whether k and o name one generation, whatever their Seq.
func (k *TopoKey) same(o *TopoKey) bool {
	return k.Install == o.Install && k.Views == o.Views && k.Source.Equal(o.Source)
}

// Memo holds, per install, one topology generation and one projection of it
// (design §4.3). It is viewer-independent; callers authorise every row per
// request, memo hits included.
//
// Invariant: the memo keeps no pointer to a Model, Snapshot, Index or object,
// so an old publication is collectable as soon as the engine drops it. A
// Topology shares only decoded rules and view C matches; a Projection holds
// only data it made. A projection is keyed by its model's Seq, not the model.
type Memo struct {
	clock clock.PassiveClock
	cap   int

	mu       sync.Mutex
	installs map[string]*memoEntry
	// pruned is the Seq of the last publication whose installs pruned the memo.
	pruned uint64
}

// NewMemo returns an empty memo whose projections expire by c; nil is the real clock.
func NewMemo(c clock.PassiveClock) *Memo { return NewMemoCap(c, TopologyCap) }

// NewMemoCap is NewMemo with a topology cap of capBytes instead of TopologyCap.
func NewMemoCap(c clock.PassiveClock, capBytes int) *Memo {
	if c == nil {
		c = clock.RealClock{}
	}
	return &Memo{clock: c, cap: capBytes, installs: map[string]*memoEntry{}}
}

// memoEntry is one install's generation. proj projects topo for the model
// whose Seq is seq, and members lists topo's backends for the one whose Seq
// is membersSeq; each is nil until one is stored.
type memoEntry struct {
	key        TopoKey
	topo       *Topology
	seq        uint64
	proj       *Projection
	membersSeq uint64
	members    *Membership
}

// Prune drops the entries of installs that publication m does not have. It
// runs once per newer Seq; older or repeated publications return at once.
func (m *Memo) Prune(md *engine.Model) {
	if md == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if md.Seq <= m.pruned {
		return
	}
	m.pruned = md.Seq
	for id := range m.installs {
		if !slices.ContainsFunc(md.Installs, func(i engine.InstallModel) bool { return i.Resolved.ID() == id }) {
			delete(m.installs, id)
		}
	}
}

// Topology returns key's topology, building it on a miss. false means it is
// over the cap and not kept; the install's older generation is dropped too.
// A build for an older publication than the stored one is not kept.
func (m *Memo) Topology(key TopoKey, build func() *Topology) (*Topology, bool) {
	m.mu.Lock()
	if e := m.installs[key.Install]; e != nil && e.key.same(&key) {
		e.key.Seq = max(e.key.Seq, key.Seq)
		t := e.topo
		m.mu.Unlock()
		return t, true
	}
	m.mu.Unlock()
	t := build()
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.installs[key.Install]
	newer := e == nil || e.key.Seq <= key.Seq
	if t.Bytes() > m.cap {
		if newer {
			delete(m.installs, key.Install)
		}
		return t, false
	}
	if newer {
		m.installs[key.Install] = &memoEntry{key: key, topo: t}
	}
	return t, true
}

// Projection returns t's projection for pub's model, projecting on a miss.
// It is kept only for the install's memoised topology, and never reused at
// or after its Next, nor for another model, even over the same snapshot.
func (m *Memo) Projection(pub *engine.Published, install string, t *Topology, project func() *Projection) *Projection {
	if pub == nil || pub.Model == nil {
		return project()
	}
	now := m.clock.Now()
	m.mu.Lock()
	seq := pub.Model.Seq
	if e := m.installs[install]; e != nil && e.topo == t && e.proj != nil && e.seq == seq && !expired(e.proj, now) {
		p := e.proj
		m.mu.Unlock()
		return p
	}
	m.mu.Unlock()
	p := project()
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.installs[install]; e != nil && e.topo == t && (e.proj == nil || e.seq <= seq) {
		e.seq, e.proj = seq, p
	}
	return p
}

// Membership returns t's backend membership for pub's model, building it on
// a miss. Like a projection, it is kept only for the memoised topology and
// never reused for another model.
func (m *Memo) Membership(pub *engine.Published, install string, t *Topology, build func() *Membership) *Membership {
	if pub == nil || pub.Model == nil {
		return build()
	}
	seq := pub.Model.Seq
	m.mu.Lock()
	if e := m.installs[install]; e != nil && e.topo == t && e.members != nil && e.membersSeq == seq {
		ms := e.members
		m.mu.Unlock()
		return ms
	}
	m.mu.Unlock()
	ms := build()
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.installs[install]; e != nil && e.topo == t && (e.members == nil || e.membersSeq <= seq) {
		e.membersSeq, e.members = seq, ms
	}
	return ms
}

// expired reports whether p's Next has come: at its instant, strict or not.
func expired(p *Projection, now time.Time) bool {
	return !p.Next.IsZero() && !now.Before(p.Next.At)
}
