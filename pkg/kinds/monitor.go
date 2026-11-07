package kinds

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
)

// Change is how the kinds the cluster serves now differ from the watched kinds.
// Entries are "resource.group/version" ("resource/version" for the core group), sorted.
// Added and Removed are judged kinds; list-only kinds ([Descriptor.ListOnly]) have their own fields.
type Change struct {
	Added           []string `json:"added,omitempty"`
	Removed         []string `json:"removed,omitempty"`
	ListOnlyAdded   []string `json:"listOnlyAdded,omitempty"`
	ListOnlyRemoved []string `json:"listOnlyRemoved,omitempty"`
}

// Empty reports whether the served kinds match the watched kinds.
func (c Change) Empty() bool {
	return !c.Judged() && len(c.ListOnlyAdded) == 0 && len(c.ListOnlyRemoved) == 0
}

// Judged reports whether a judged kind changed; a list-only change alone is not one.
func (c Change) Judged() bool { return len(c.Added) > 0 || len(c.Removed) > 0 }

// String returns "added a, b; removed c", omitting an empty half; list-only
// changes follow as "list-only added d; list-only removed e".
func (c Change) String() string {
	var parts []string
	for _, p := range []struct {
		label string
		keys  []string
	}{{"added ", c.Added}, {"removed ", c.Removed}, {"list-only added ", c.ListOnlyAdded}, {"list-only removed ", c.ListOnlyRemoved}} {
		if len(p.keys) > 0 {
			parts = append(parts, p.label+strings.Join(p.keys, ", "))
		}
	}
	return strings.Join(parts, "; ")
}

// ListOnly returns c's list-only half as "added a; removed b".
func (c Change) ListOnly() string {
	return Change{Added: c.ListOnlyAdded, Removed: c.ListOnlyRemoved}.String()
}

// Diff compares the served kinds now with the watched ones by group, version and resource.
func Diff(watched, now []Served) Change {
	key := func(s Served) string {
		if s.Group == "" {
			return s.Resource + "/" + s.Version
		}
		return s.Resource + "." + s.Group + "/" + s.Version
	}
	// Each key maps to whether its kind is list-only.
	w, n := map[string]bool{}, map[string]bool{}
	for _, s := range watched {
		w[key(s)] = s.ListOnly
	}
	for _, s := range now {
		n[key(s)] = s.ListOnly
	}
	var c Change
	for k, listOnly := range n {
		if _, ok := w[k]; !ok {
			if listOnly {
				c.ListOnlyAdded = append(c.ListOnlyAdded, k)
			} else {
				c.Added = append(c.Added, k)
			}
		}
	}
	for k, listOnly := range w {
		if _, ok := n[k]; !ok {
			if listOnly {
				c.ListOnlyRemoved = append(c.ListOnlyRemoved, k)
			} else {
				c.Removed = append(c.Removed, k)
			}
		}
	}
	for _, s := range [][]string{c.Added, c.Removed, c.ListOnlyAdded, c.ListOnlyRemoved} {
		slices.Sort(s)
	}
	return c
}

// Monitor re-resolves the served kinds and keeps how they differ from the
// watched set. The UI does not re-watch: a change asks for a restart. It also
// publishes the Catalog of every served kind.
type Monitor struct {
	dc      DiscoveryClient
	descs   []Descriptor
	watched []Served
	mu      sync.RWMutex
	cur     Change
	catalog Catalog
	// OnChange, if set, is notified when a Refresh yields a Change or Catalog that
	// differs from the previous one.
	OnChange *trigger.Signal
}

// NewMonitor returns a Monitor of descs against watched, the kinds the cache watches.
func NewMonitor(dc DiscoveryClient, descs []Descriptor, watched []Served) *Monitor {
	return &Monitor{dc: dc, descs: descs, watched: watched}
}

// Refresh resolves the served kinds and the Catalog once. On error the last
// Change is kept; a Catalog that cannot be listed keeps the last one.
func (m *Monitor) Refresh() error {
	now, err := Resolve(m.dc, m.descs)
	if err != nil {
		return err
	}
	c := Diff(m.watched, now)
	cat, catOK := discoverCatalog(m.dc)
	m.mu.Lock()
	changed := c.String() != m.cur.String()
	m.cur = c
	if catOK {
		changed = changed || cat.Key() != m.catalog.Key()
		m.catalog = cat
	}
	m.mu.Unlock()
	if changed {
		m.OnChange.Notify()
	}
	return nil
}

// Change returns the latest difference; it is empty until a Refresh finds one.
func (m *Monitor) Change() Change {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cur
}

// Catalog returns the latest Catalog; it is the zero Catalog until a Refresh lists one.
func (m *Monitor) Catalog() Catalog {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.catalog
}

// Run refreshes every interval until ctx is done.
func (m *Monitor) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := m.Refresh(); err != nil {
			slog.Warn("re-resolving served kinds failed; keeping the last result", "err", err)
		}
	}
}
