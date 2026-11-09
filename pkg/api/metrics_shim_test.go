package api

import (
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
)

// metricsSource is the controller metrics a test serves per install ID.
type metricsSource interface {
	Get(id string) (ctrlmetrics.Result, bool)
	IDs() []string
}

// IDs implements metricsSource.
func (f fakeMetrics) IDs() []string { return slices.Sorted(maps.Keys(f)) }

// IDs implements metricsSource.
func (l *liveMetrics) IDs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Sorted(maps.Keys(l.m))
}

// served is base's values (none when nil) with src's results under
// ctrlmetrics.Key with maxAge, published as the collector does: again only
// when a result changes, CollectedAt included, each time later than the last,
// and with a new fingerprint for a result after a stale one.
func served(src metricsSource, maxAge time.Duration, base func() collect.Snapshot) func() collect.Snapshot {
	var (
		mu        sync.Mutex
		at        time.Time
		last      map[string]ctrlmetrics.Result
		recovered uint64
	)
	store := collect.New(nil, func() time.Time { return at })
	return func() collect.Snapshot {
		var out collect.Snapshot
		if base != nil {
			out = base()
		}
		if src == nil {
			return out
		}
		mu.Lock()
		defer mu.Unlock()
		cur := map[string]ctrlmetrics.Result{}
		for _, id := range src.IDs() {
			if r, ok := src.Get(id); ok {
				cur[id] = r
			}
		}
		same := func(a, b ctrlmetrics.Result) bool { return a.SameContent(b) && a.CollectedAt.Equal(b.CollectedAt) }
		if last == nil || !maps.EqualFunc(cur, last, same) {
			// Each publication is later than the last, as the collector's store clock is.
			next := at.Add(time.Nanosecond)
			for id, r := range cur {
				if r.CollectedAt.After(next) {
					next = r.CollectedAt
				}
				if prev, had := last[id]; had && prev.SameContent(r) && prev.Stale(r.CollectedAt, maxAge) {
					recovered++
				}
			}
			at = next
			res := ctrlmetrics.Results{ByInstall: cur, MaxAge: maxAge}
			store.Publish(ctrlmetrics.Key, res, res.Fingerprint()^recovered, 0)
			last = cur
		}
		m := store.Snapshot()
		vals := maps.Clone(out.Values)
		if vals == nil {
			vals = map[string]collect.Value{}
		}
		vals[ctrlmetrics.Key] = m.Values[ctrlmetrics.Key]
		return collect.Snapshot{Values: vals, Print: out.Print ^ m.Print}
	}
}
