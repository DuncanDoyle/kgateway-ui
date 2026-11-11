package paths

import (
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
type Memo struct {
	clock clock.PassiveClock
	cap   int

	mu       sync.Mutex
	installs map[string]*memoEntry
}

// NewMemo returns an empty memo whose projections expire by c; nil is the real clock.
func NewMemo(c clock.PassiveClock) *Memo {
	if c == nil {
		c = clock.RealClock{}
	}
	return &Memo{clock: c, cap: TopologyCap, installs: map[string]*memoEntry{}}
}

// memoEntry is one install's generation. proj projects topo for model.
type memoEntry struct {
	key   TopoKey
	topo  *Topology
	model *engine.Model
	proj  *Projection
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
	if e := m.installs[install]; e != nil && e.topo == t && e.model == pub.Model && e.proj != nil && !expired(e.proj, now) {
		p := e.proj
		m.mu.Unlock()
		return p
	}
	m.mu.Unlock()
	p := project()
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.installs[install]; e != nil && e.topo == t && (e.model == nil || e.model.Seq <= pub.Model.Seq) {
		e.model, e.proj = pub.Model, p
	}
	return p
}

// expired reports whether p's Next has come: at its instant, strict or not.
func expired(p *Projection, now time.Time) bool {
	return !p.Next.IsZero() && !now.Before(p.Next.At)
}
