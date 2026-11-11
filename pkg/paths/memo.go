package paths

import (
	"sync"
	"time"

	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/engine"
)

// TopologyCap is the topology memo's cap per install generation, in Bytes (design §14).
const TopologyCap = 2 << 20

// TopoKey is a topology's generation: its install, snapshot and computed
// views. Only the exact pointers match; equal values do not.
type TopoKey struct {
	Install  string
	Snapshot *cache.Snapshot
	Views    *computed.Views
}

// Memo holds, per install, one topology generation and one projection of it
// (design §4.3). It is viewer-independent; callers authorise every row per
// request, memo hits included. The zero Memo is ready to use.
type Memo struct {
	// Now is the clock a projection's Next is checked against; nil is time.Now.
	Now func() time.Time
	// Cap is the topology cap in bytes; 0 is TopologyCap.
	Cap int

	mu       sync.Mutex
	installs map[string]*memoEntry
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
func (m *Memo) Topology(key TopoKey, build func() *Topology) (*Topology, bool) {
	m.mu.Lock()
	if e := m.installs[key.Install]; e != nil && e.key == key {
		m.mu.Unlock()
		return e.topo, true
	}
	m.mu.Unlock()
	t := build()
	limit := m.Cap
	if limit == 0 {
		limit = TopologyCap
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.Bytes() > limit {
		delete(m.installs, key.Install)
		return t, false
	}
	if m.installs == nil {
		m.installs = map[string]*memoEntry{}
	}
	m.installs[key.Install] = &memoEntry{key: key, topo: t}
	return t, true
}

// Projection returns t's projection for pub's model, projecting on a miss.
// It is kept only for the install's memoised topology, and never reused at
// or after its Next, nor for another model, even over the same snapshot.
func (m *Memo) Projection(pub *engine.Published, install string, t *Topology, project func() *Projection) *Projection {
	if pub == nil {
		return project()
	}
	now := time.Now
	if m.Now != nil {
		now = m.Now
	}
	m.mu.Lock()
	if e := m.installs[install]; e != nil && e.topo == t && e.model == pub.Model && e.proj != nil && !expired(e.proj, now()) {
		m.mu.Unlock()
		return e.proj
	}
	m.mu.Unlock()
	p := project()
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.installs[install]; e != nil && e.topo == t {
		e.model, e.proj = pub.Model, p
	}
	return p
}

// expired reports whether p's Next has come: at its instant, strict or not.
func expired(p *Projection, now time.Time) bool {
	return !p.Next.IsZero() && !now.Before(p.Next.At)
}
