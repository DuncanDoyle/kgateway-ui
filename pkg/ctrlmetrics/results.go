package ctrlmetrics

import (
	"cmp"
	"context"
	"encoding/binary"
	"hash/fnv"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/install"
)

// The collected value: the collector publishes one Results under Key.
const (
	CollectorName = "controller-metrics"
	ValueKey      = "results"
	Key           = CollectorName + "/" + ValueKey
)

// Results is one scrape per install ID, with the age beyond which an entry is
// stale. Entries age on their own: each Result keeps its CollectedAt.
type Results struct {
	ByInstall map[string]Result
	// MaxAge is 3 × the collection interval; zero never makes an entry stale.
	MaxAge time.Duration
}

// CollectOnce scrapes each install's ready controller Pods once, concurrently,
// and returns the results by install ID with maxAge. Pass only the installs
// of the kgateway pack (install.Install.Impl == "kgateway").
func CollectOnce(ctx context.Context, s Scraper, insts []install.Install, maxAge time.Duration) Results {
	c := NewCollector(s)
	out := Results{ByInstall: make(map[string]Result, len(insts)), MaxAge: maxAge}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, in := range insts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := c.Collect(ctx, in)
			mu.Lock()
			out.ByInstall[in.ID()] = r
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// Publish stores r under Key with no store expiry, so the next engine Compute
// over store reads it; staleness stays per entry. The store owns r afterwards.
func Publish(store *collect.Store, r Results) {
	store.Publish(Key, r, r.Fingerprint(), 0)
}

// Lookup returns install id's entry in v, a value collected under Key, and
// the max age. It is false unless v is OK and holds an entry for id.
func Lookup(v collect.Value, id string) (Result, time.Duration, bool) {
	rs, ok := v.Data.(Results)
	if v.State != collect.OK || !ok {
		return Result{}, 0, false
	}
	r, ok := rs.ByInstall[id]
	return r, rs.MaxAge, ok
}

// Fingerprint identifies r's content: each entry's install ID, Err and
// metrics, and MaxAge. CollectedAt is left out, as in Result.SameContent.
func (r Results) Fingerprint() uint64 {
	h := fnv.New64a()
	var b [8]byte
	u64 := func(v uint64) {
		binary.LittleEndian.PutUint64(b[:], v)
		_, _ = h.Write(b[:])
	}
	str := func(s string) {
		u64(uint64(len(s))) // length prefix keeps adjacent strings apart
		_, _ = h.Write([]byte(s))
	}
	flag := func(v bool) {
		if v {
			u64(1)
		} else {
			u64(0)
		}
	}
	u64(uint64(r.MaxAge)) //nolint:gosec // hashed bits
	gwOrder := func(a, b GatewayKey) int {
		return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
	}
	typeOrder := func(a, b TypeKey) int {
		return cmp.Or(gwOrder(a.Gateway, b.Gateway), cmp.Compare(a.TypeURL, b.TypeURL))
	}
	for _, id := range sortedKeys(r.ByInstall, cmp.Compare[string]) {
		res := r.ByInstall[id]
		str(id)
		str(res.Err)
		m := res.Metrics
		flag(m.ManagedPresent)
		rejects := sortedKeys(m.Rejects, gwOrder)
		u64(uint64(len(rejects)))
		for _, k := range rejects {
			str(k.Namespace)
			str(k.Name)
			u64(math.Float64bits(m.Rejects[k]))
		}
		managed := sortedKeys(m.Managed, func(a, b ManagedKey) int {
			return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Resource, b.Resource))
		})
		u64(uint64(len(managed)))
		for _, k := range managed {
			str(k.Namespace)
			str(k.Resource)
			u64(math.Float64bits(m.Managed[k]))
		}
		snapshot := sortedKeys(m.Snapshot, gwOrder)
		u64(uint64(len(snapshot)))
		for _, k := range snapshot {
			str(k.Namespace)
			str(k.Name)
			v := m.Snapshot[k]
			for _, n := range []int{v.Listeners, v.Routes, v.Clusters, v.Endpoints, v.Secrets} {
				u64(uint64(n)) //nolint:gosec // a count; only its bits matter
			}
		}
		u64(uint64(len(m.PerPod)))
		for _, p := range m.PerPod {
			str(p.UID)
			str(p.Name)
			flag(p.DroppedPresent)
			u64(math.Float64bits(p.Dropped))
			for _, series := range []map[TypeKey]float64{p.Active, p.Total} {
				keys := sortedKeys(series, typeOrder)
				u64(uint64(len(keys)))
				for _, k := range keys {
					str(k.Gateway.Namespace)
					str(k.Gateway.Name)
					str(k.TypeURL)
					u64(math.Float64bits(series[k]))
				}
			}
			str(p.BuildVersion)
			flag(p.PerClientPresent)
			withheld := sortedKeys(p.Withheld, gwOrder)
			u64(uint64(len(withheld)))
			for _, k := range withheld {
				str(k.Namespace)
				str(k.Name)
				u64(math.Float64bits(p.Withheld[k]))
			}
			bounded := sortedKeys(p.Bounded, func(a, b ModeKey) int {
				return cmp.Or(gwOrder(a.Gateway, b.Gateway), cmp.Compare(a.Mode, b.Mode))
			})
			u64(uint64(len(bounded)))
			for _, k := range bounded {
				str(k.Gateway.Namespace)
				str(k.Gateway.Name)
				str(k.Mode)
				u64(math.Float64bits(p.Bounded[k]))
			}
		}
	}
	return h.Sum64()
}

// sortedKeys returns m's keys sorted by order.
func sortedKeys[K comparable, V any](m map[K]V, order func(a, b K) int) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, order)
	return keys
}

// ReportedVersion is the version install id's controller reports in
// kgateway_build_info, from col's value under Key. ok is false when col does
// not tell: no OK entry, a failed scrape or replicas that disagree.
func ReportedVersion(col collect.Snapshot, id string) (v string, ok bool) {
	cv, _ := col.Get(Key)
	r, _, has := Lookup(cv, id)
	if !has || r.Err != "" {
		return "", false
	}
	return r.Metrics.BuildVersion()
}
