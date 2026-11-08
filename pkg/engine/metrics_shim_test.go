package engine

import (
	"maps"
	"sync"
	"time"

	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
)

// metricsSource is the controller metrics a test serves per install ID.
type metricsSource interface {
	Get(id string) (ctrlmetrics.Result, bool)
}

// metricsFunc adapts a function to metricsSource.
type metricsFunc func(id string) (ctrlmetrics.Result, bool)

func (f metricsFunc) Get(id string) (ctrlmetrics.Result, bool) { return f(id) }

// served publishes src's results for ids (kgw.kgateway when none) under
// ctrlmetrics.Key with maxAge, as the collector does: again only when a
// result changes, CollectedAt included, each time later than the last.
func served(src metricsSource, maxAge time.Duration, ids ...string) func() collect.Snapshot {
	if len(ids) == 0 {
		ids = []string{"kgw.kgateway"}
	}
	var (
		mu        sync.Mutex
		at        time.Time
		last      map[string]ctrlmetrics.Result
		recovered uint64
	)
	store := collect.New(nil, func() time.Time { return at })
	return func() collect.Snapshot {
		mu.Lock()
		defer mu.Unlock()
		cur := map[string]ctrlmetrics.Result{}
		for _, id := range ids {
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
				// As the collector: a result after a stale one moves the fingerprint.
				if prev, had := last[id]; had && prev.SameContent(r) && prev.Stale(r.CollectedAt, maxAge) {
					recovered++
				}
			}
			at = next
			res := ctrlmetrics.Results{ByInstall: cur, MaxAge: maxAge}
			store.Publish(ctrlmetrics.Key, res, res.Fingerprint()^recovered, 0)
			last = cur
		}
		return store.Snapshot()
	}
}

// metricsOf is install id's entry in m's collected controller metrics.
func metricsOf(m *Model, id string) (ctrlmetrics.Result, bool) {
	v, _ := m.Collected.Get(ctrlmetrics.Key)
	r, _, ok := ctrlmetrics.Lookup(v, id)
	return r, ok
}

// withScrape is col with r as install id's only metrics entry, published at
// r.CollectedAt with maxAge, as a new scrape of the collector would be.
func withScrape(col collect.Snapshot, id string, r ctrlmetrics.Result, maxAge time.Duration) collect.Snapshot {
	vals := maps.Clone(col.Values)
	if vals == nil {
		vals = map[string]collect.Value{}
	}
	res := ctrlmetrics.Results{ByInstall: map[string]ctrlmetrics.Result{id: r}, MaxAge: maxAge}
	old := vals[ctrlmetrics.Key].Fingerprint
	vals[ctrlmetrics.Key] = collect.Value{Data: res, Fingerprint: res.Fingerprint(), At: r.CollectedAt, State: collect.OK}
	return collect.Snapshot{Values: vals, Print: col.Print ^ old ^ res.Fingerprint()}
}
