package proxystats

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

const (
	// MaxBody bounds one stats body; a larger one fails the Pod, never a truncated parse.
	MaxBody = 1 << 20
	// maxConcurrent is how many Pods of one install are scraped at once.
	maxConcurrent = 16
	// maxFetchTimeout caps one fetch; the timeout is min(interval/2, 5 s).
	maxFetchTimeout = 5 * time.Second
	// maxErrLen caps a Source's error text.
	maxErrLen = 120
	// firstRoundRetry is how soon a round without a model is retried.
	firstRoundRetry = time.Second
)

// OffReason is the reason published when proxy stats are turned off.
const OffReason = "Proxy stats are turned off"

// errTimedOut is a Pod's error when its fetch or the round ran out of time.
const errTimedOut = "timed out"

// cut shortens s to at most n bytes on a rune boundary.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// Install is one install to scrape and the Gateways it owns.
type Install struct {
	Resolved install.Resolved
	Gateways []objects.Ref
}

// Config configures a Collector.
type Config struct {
	Source Source
	// Every is the scrape interval, the metrics interval.
	Every time.Duration
	// Clock drives the ticker and stamps Rejects.Since; nil means the real clock.
	Clock clock.WithTicker
	// Installs returns the installs and the snapshot they were resolved from;
	// a nil snapshot (no model yet) skips the round.
	Installs func() ([]Install, *cache.Snapshot)
	// Off publishes collect.Off once and scrapes nothing.
	Off bool
}

// Collector scrapes each install's proxies and turns their counters into
// rejects observed since the previous scrape per Pod UID (Review Focus 2).
type Collector struct {
	cfg Config
	// mu serialises rounds; pods holds the last counters per install and Pod UID.
	mu   sync.Mutex
	pods map[podKey]*podState
}

type podKey struct{ install, uid string }

// podState is one Pod's counters at its last successful scrape.
type podState struct {
	uptime             int64
	lds, cds           int64
	rds, clusters      map[string]int64
	ldsSince, cdsSince time.Time
	// rdsCut and clustersCut are set when names were dropped from that list.
	rdsCut, clustersCut bool
}

// New returns a Collector for cfg.
func New(cfg Config) *Collector {
	if cfg.Clock == nil {
		cfg.Clock = clock.RealClock{}
	}
	return &Collector{cfg: cfg, pods: map[podKey]*podState{}}
}

// Run scrapes now and every interval until ctx is done, publishing under
// ValueKey with a TTL of three intervals; Off publishes collect.Off once.
func (c *Collector) Run(ctx context.Context, publish func(key string, v any, fp uint64, ttl time.Duration), fail func(key string, st collect.State, reason string)) error {
	if c.cfg.Off {
		fail(ValueKey, collect.Off, OffReason)
		<-ctx.Done()
		return nil
	}
	if c.cfg.Every <= 0 || c.cfg.Source == nil || c.cfg.Installs == nil {
		return errors.New("proxystats: the collector needs a positive interval, a Source and Installs")
	}
	t := c.cfg.Clock.NewTicker(c.cfg.Every)
	defer t.Stop()
	for {
		st, fp, ok := c.Round(ctx)
		var retry <-chan time.Time
		switch {
		case ok && ctx.Err() == nil:
			publish(ValueKey, st, fp, 3*c.cfg.Every)
		case !ok:
			// No synced model yet: try again soon rather than a whole interval later.
			retry = c.cfg.Clock.After(min(c.cfg.Every, firstRoundRetry))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C():
		case <-retry:
		}
	}
}

// Tracked is how many Pods have counters held; a vanished Pod is dropped on the next round.
func (c *Collector) Tracked() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pods)
}

// scrapes are one install's targets per Gateway and their results.
type scrapes struct {
	gateways []Gateway
	targets  [][]Target
	results  [][]scraped
}

type scraped struct {
	stats PodStats
	err   string
}

// Round scrapes every install once and returns the value, its fingerprint
// and true; false when there is no model yet.
func (c *Collector) Round(ctx context.Context) (Stats, uint64, bool) {
	ins, snap := c.cfg.Installs()
	if snap == nil {
		return nil, 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	timeout := min(c.cfg.Every/2, maxFetchTimeout)
	// The round ends before the next tick, so it always publishes inside the TTL.
	rctx, cancel := context.WithTimeout(ctx, c.cfg.Every*9/10)
	defer cancel()
	all := make([]scrapes, len(ins))
	var wg sync.WaitGroup
	for i, in := range ins {
		wg.Add(1)
		go func() {
			defer wg.Done()
			all[i] = c.scrapeInstall(rctx, in, snap, timeout)
		}()
	}
	wg.Wait()
	now := c.cfg.Clock.Now()
	st := make(Stats, len(ins))
	seen := map[podKey]bool{}
	for i, in := range ins {
		id, s := in.Resolved.ID(), all[i]
		for gi := range s.gateways {
			for ti, t := range s.targets[gi] {
				key := podKey{install: id, uid: t.ID}
				seen[key] = true
				s.gateways[gi].Pods = append(s.gateways[gi].Pods, c.fold(key, t, s.results[gi][ti], now))
			}
		}
		st[id] = s.gateways
	}
	for k := range c.pods {
		if !seen[k] {
			delete(c.pods, k)
		}
	}
	return st, fingerprint(st), true
}

// scrapeInstall resolves in's targets per Gateway, in Ref order, and fetches
// them, at most maxConcurrent at once; Pods not reached before ctx ends time
// out. A Pod listed twice is scraped once.
func (c *Collector) scrapeInstall(ctx context.Context, in Install, snap *cache.Snapshot, timeout time.Duration) scrapes {
	gws := slices.Clone(in.Gateways)
	slices.SortFunc(gws, func(a, b objects.Ref) int {
		return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Group, b.Group), cmp.Compare(a.Kind, b.Kind))
	})
	gws = slices.Compact(gws)
	s := scrapes{gateways: make([]Gateway, len(gws)), targets: make([][]Target, len(gws)), results: make([][]scraped, len(gws))}
	ids := map[string]bool{}
	for i, gw := range gws {
		ts, ok := c.cfg.Source.Targets(in.Resolved, gw, snap)
		ts = slices.DeleteFunc(slices.Clone(ts), func(t Target) bool {
			dup := ids[t.ID]
			ids[t.ID] = true
			return dup
		})
		slices.SortFunc(ts, func(a, b Target) int { return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID)) })
		s.gateways[i] = Gateway{Gateway: gw, Observed: ok, Pods: make([]Pod, 0, len(ts))}
		s.targets[i], s.results[i] = ts, make([]scraped, len(ts))
	}
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for gi, ts := range s.targets {
		for ti, t := range ts {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				s.results[gi][ti] = scraped{err: errTimedOut}
				continue
			}
			wg.Add(1)
			go func() {
				defer func() { <-sem; wg.Done() }()
				s.results[gi][ti] = c.scrape(ctx, t, timeout)
			}()
		}
	}
	wg.Wait()
	return s
}

// scrape fetches and interprets one target under timeout. Errors are fixed
// text or the Source's own text, cut to maxErrLen; never response content.
func (c *Collector) scrape(ctx context.Context, t Target, timeout time.Duration) scraped {
	fctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	b, err := c.cfg.Source.Fetch(fctx, t)
	switch {
	case err != nil && errors.Is(fctx.Err(), context.DeadlineExceeded):
		return scraped{err: errTimedOut}
	case err != nil:
		return scraped{err: cut(err.Error(), maxErrLen)}
	case len(b) > MaxBody:
		return scraped{err: "response over 1 MiB"}
	}
	ps, err := c.cfg.Source.Interpret(t, b)
	if err != nil {
		return scraped{err: "unreadable stats"}
	}
	return scraped{stats: ps}
}

// fold turns one scrape into its published Pod and updates the Pod's counters.
// A failed scrape keeps the previous counters, so the next one compares to them.
func (c *Collector) fold(key podKey, t Target, r scraped, now time.Time) Pod {
	p := Pod{UID: t.ID, Name: t.Name}
	if r.err != "" {
		p.Err = r.err
		return p
	}
	ps := r.stats
	p.Connected, p.WarmingListeners, p.WarmingClusters, p.ServerState = ps.Connected, ps.WarmingListeners, ps.WarmingClusters, ps.ServerState
	prev := c.pods[key]
	// A new UID or a lower uptime is a restart: its counters start a new baseline.
	reset := prev == nil || (ps.Uptime > 0 && ps.Uptime < prev.uptime)
	if reset {
		prev = &podState{}
	}
	cur := &podState{uptime: ps.Uptime, lds: ps.LDS.Rejected, cds: ps.CDS.Rejected}
	p.LDS, cur.ldsSince = observe(reset, prev.lds, cur.lds, prev.ldsSince, now)
	p.CDS, cur.cdsSince = observe(reset, prev.cds, cur.cds, prev.cdsSince, now)
	cur.rds, p.RDS, cur.rdsCut = names(reset, prev.rds, prev.rdsCut, ps.RDS)
	cur.clusters, p.Clusters, cur.clustersCut = names(reset, prev.clusters, prev.clustersCut, ps.Clusters)
	cur.rdsCut, cur.clustersCut = cur.rdsCut || ps.RDSTruncated, cur.clustersCut || ps.ClustersTruncated
	p.RDSTruncated, p.ClustersTruncated = cur.rdsCut, cur.clustersCut
	p.Names = allNames(ps.RDS, ps.Clusters)
	c.pods[key] = cur
	return p
}

// allNames lists the valid route config and cluster names a scrape parsed,
// sorted, at most maxPodNames: its own cap, apart from the reject lists' maxNames.
func allNames(rds, clusters map[string]int64) []string {
	out := make([]string, 0, len(rds)+len(clusters))
	for _, m := range []map[string]int64{rds, clusters} {
		for n := range m {
			if validName(n) {
				out = append(out, n)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	out = slices.Compact(out)
	return out[:min(len(out), maxPodNames)]
}

// observe reports growth from old to cur as rejects seen, keeping since while
// growth continues. A lower value is a reset and reads as none.
func observe(reset bool, old, cur int64, since, now time.Time) (Rejects, time.Time) {
	if reset || cur <= old {
		return Rejects{}, time.Time{}
	}
	if since.IsZero() {
		since = now
	}
	return Rejects{Seen: true, Since: since}, since
}

// names keeps at most maxNames valid names of cur, grown ones first, then
// those already kept, then the rest, and lists the grown ones sorted. A name
// absent from old counted 0, unless old was truncated and its baseline is unknown.
func names(reset bool, old map[string]int64, oldCut bool, cur map[string]int64) (kept map[string]int64, grew []string, truncated bool) {
	rank := func(n string) int {
		o, had := old[n]
		switch {
		case reset:
			return 2
		case cur[n] > o && (had || !oldCut):
			return 0
		case had:
			return 1
		}
		return 2
	}
	all := make([]string, 0, len(cur))
	for n := range cur {
		if validName(n) {
			all = append(all, n)
		}
	}
	slices.SortFunc(all, func(a, b string) int { return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a, b)) })
	truncated = len(all) > maxNames || len(all) < len(cur)
	all = all[:min(len(all), maxNames)]
	if len(all) == 0 {
		return nil, nil, truncated
	}
	kept = make(map[string]int64, len(all))
	for _, n := range all {
		kept[n] = cur[n]
		if rank(n) == 0 {
			grew = append(grew, n)
		}
	}
	slices.Sort(grew)
	return kept, grew, truncated
}
