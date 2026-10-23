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
type Change struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// Empty reports whether the served kinds match the watched kinds.
func (c Change) Empty() bool { return len(c.Added) == 0 && len(c.Removed) == 0 }

// String returns "added a, b; removed c", omitting an empty half.
func (c Change) String() string {
	var parts []string
	if len(c.Added) > 0 {
		parts = append(parts, "added "+strings.Join(c.Added, ", "))
	}
	if len(c.Removed) > 0 {
		parts = append(parts, "removed "+strings.Join(c.Removed, ", "))
	}
	return strings.Join(parts, "; ")
}

// Diff compares the served kinds now with the watched ones by group, version and resource.
func Diff(watched, now []Served) Change {
	key := func(s Served) string {
		if s.Group == "" {
			return s.Resource + "/" + s.Version
		}
		return s.Resource + "." + s.Group + "/" + s.Version
	}
	w, n := map[string]bool{}, map[string]bool{}
	for _, s := range watched {
		w[key(s)] = true
	}
	for _, s := range now {
		n[key(s)] = true
	}
	var c Change
	for k := range n {
		if !w[k] {
			c.Added = append(c.Added, k)
		}
	}
	for k := range w {
		if !n[k] {
			c.Removed = append(c.Removed, k)
		}
	}
	slices.Sort(c.Added)
	slices.Sort(c.Removed)
	return c
}

// Monitor re-resolves the served kinds and keeps how they differ from the
// watched set. The UI does not re-watch: a change asks for a restart.
type Monitor struct {
	dc      DiscoveryClient
	descs   []Descriptor
	watched []Served
	mu      sync.RWMutex
	cur     Change
	// OnChange, if set, is notified when a Refresh yields a Change that
	// differs from the previous one.
	OnChange *trigger.Signal
}

// NewMonitor returns a Monitor of descs against watched, the kinds the cache watches.
func NewMonitor(dc DiscoveryClient, descs []Descriptor, watched []Served) *Monitor {
	return &Monitor{dc: dc, descs: descs, watched: watched}
}

// Refresh resolves the served kinds once. On error the last Change is kept.
func (m *Monitor) Refresh() error {
	now, err := Resolve(m.dc, m.descs)
	if err != nil {
		return err
	}
	c := Diff(m.watched, now)
	m.mu.Lock()
	changed := c.String() != m.cur.String()
	m.cur = c
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
