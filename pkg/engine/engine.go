// Package engine computes the UI's model from the cache, the controller
// metrics and the registered checks, when triggers or deadlines call for it (D-019).
package engine

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/overview"
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxymode"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
)

// Config wires the engine.
type Config struct {
	Snapshots cache.Snapshotter
	// Served is the one served-kind list given to assignment, checks, the
	// scope verifier and the overview.
	Served []kinds.Served
	// KindsChange, if set, reports how the kinds served now differ from Served.
	KindsChange func() kinds.Change
	Detectors   []install.Detector
	Scope       install.ScopeProvider
	// Verifier checks scope against the controller; its persistence should be
	// 2 × the metrics interval (design §4.4).
	Verifier scopeverify.Verifier
	Metrics  ctrlmetrics.Source
	Checks   []health.Check
	// Pinned restricts the UI to one install ID; empty shows all.
	Pinned string
	// Platform is the platform the snapshots come from; zero means platform.Kubernetes().
	Platform platform.Info
	// ProxyMode decides whether a Gateway is self-managed; nil means proxymode.GatewayParameters{}.
	ProxyMode proxymode.Resolver
	// Logger receives install rejections; nil means slog.Default().
	Logger *slog.Logger
	// MetricsMaxAge is the age beyond which a metrics result is stale
	// (3 × the metrics interval). Zero disables staleness: never use it in production.
	MetricsMaxAge time.Duration
	// Now stamps computations; nil means Clock.Now. It must agree with Clock,
	// or Run sees deadlines due that Compute does not.
	Now func() time.Time
	// OnCompute, if set, receives each model Run publishes, on Run's goroutine.
	// Confirmations do not call it.
	OnCompute func(*Model)
	// Triggers starts recomputes; nil means only deadlines, the safety timer
	// and the confirm tick do.
	Triggers *trigger.Signal
	// Clock drives Run's timers; nil means clock.RealClock{}.
	Clock clock.WithTicker
	// ConfirmEvery is the confirm tick interval; zero means ConfirmEvery.
	ConfirmEvery time.Duration
	// OnLoop, if set, is called on Run's goroutine after each loop transition (tests).
	OnLoop func(LoopEvent)
}

// InstallModel is everything the API serves about one install.
type InstallModel struct {
	Resolved install.Resolved
	Owned    *install.Owned
	Overview overview.Overview
	Health   health.Report
}

// Model is one consistent computation over a snapshot.
type Model struct {
	Snapshot   *cache.Snapshot
	Served     []kinds.Served
	Installs   []InstallModel
	Pinned     string
	ComputedAt time.Time
	// Rejected are this computation's install rejections (invalid IDs); /readyz reports them.
	Rejected []string
	// Seq orders computations; it strictly increases.
	Seq uint64
	// Next is the earliest deadline not yet due at ComputedAt: a grace expiry,
	// metrics staleness or a scope mismatch maturing.
	Next Deadline
	// MetricsRead are the metrics results this computation used, one per install, in install order.
	MetricsRead []MetricsRead
}

// MetricsRead is one metrics result as Compute read it.
type MetricsRead struct {
	ID      string
	Result  ctrlmetrics.Result
	Present bool
}

// Engine computes Models.
type Engine struct {
	cfg Config
	cur atomic.Pointer[Published]
	// computeMu serialises Compute; seq, seen and rejected are only touched under it.
	computeMu sync.Mutex
	seq       uint64
	seen      map[generation]time.Time
	rejected  map[string]bool // install errors already logged
	// beforeConfirmSwap, if set, runs in confirm between its read and its swap (tests).
	beforeConfirmSwap func()
}

// generation identifies one spec version of one object.
type generation struct {
	uid types.UID
	gen int64
}

// New returns an Engine.
func New(cfg Config) *Engine {
	if cfg.Clock == nil {
		cfg.Clock = clock.RealClock{}
	}
	if cfg.Now == nil {
		cfg.Now = cfg.Clock.Now
	}
	if cfg.ConfirmEvery <= 0 {
		cfg.ConfirmEvery = ConfirmEvery
	}
	if cfg.Platform.IsZero() {
		cfg.Platform = platform.Kubernetes()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Engine{cfg: cfg, seen: map[generation]time.Time{}, rejected: map[string]bool{}}
}

// Platform returns the platform the engine computes models for.
func (e *Engine) Platform() platform.Info { return e.cfg.Platform }

// observe records when each (uid, generation) in snap was first seen and
// forgets the ones that are gone. The grace period runs from that moment: a
// generation bump restarts it, a label or annotation update does not, and
// objects present at the first computation get grace from UI start.
func (e *Engine) observe(snap *cache.Snapshot, now time.Time) {
	current := map[generation]bool{}
	for _, objs := range snap.Objects {
		for _, u := range objs {
			k := generation{u.GetUID(), u.GetGeneration()}
			current[k] = true
			if _, ok := e.seen[k]; !ok {
				e.seen[k] = now
			}
		}
	}
	for k := range e.seen {
		if !current[k] {
			delete(e.seen, k)
		}
	}
}

// firstSeen returns when the engine first saw u's current generation; an
// object it has not observed counts as seen now. Called under computeMu.
func (e *Engine) firstSeen(now time.Time) func(u *unstructured.Unstructured) time.Time {
	return func(u *unstructured.Unstructured) time.Time {
		if t, ok := e.seen[generation{u.GetUID(), u.GetGeneration()}]; ok {
			return t
		}
		return now
	}
}

// Compute builds a new Model and publishes it. Once Run has started, only
// Run's loop computes and publishes.
func (e *Engine) Compute() *Model {
	p := e.compute()
	e.publish(p)
	return p.Model
}

// compute builds a new Model and returns it with its source, unpublished.
func (e *Engine) compute() *Published {
	e.computeMu.Lock()
	defer e.computeMu.Unlock()
	snap := e.cfg.Snapshots.Snapshot()
	now := e.cfg.Now()
	e.observe(snap, now)
	e.seq++
	served := e.cfg.Served
	m := &Model{Snapshot: snap, Served: served, Pinned: e.cfg.Pinned, ComputedAt: now, Seq: e.seq}
	var kc kinds.Change
	if e.cfg.KindsChange != nil {
		kc = e.cfg.KindsChange()
	}
	found, errs := install.Discover(snap, e.cfg.Detectors)
	for _, err := range errs {
		m.Rejected = append(m.Rejected, err.Error())
		// Logged once per distinct error, not on every recompute.
		if msg := err.Error(); !e.rejected[msg] {
			e.rejected[msg] = true
			e.cfg.Logger.Error("install rejected", "err", err)
		}
	}
	all := install.Resolve(found, e.cfg.Scope)
	// Assignment runs over all installs so ambiguity is detected even when pinned.
	asg := install.Assign(snap, served, all)
	next := deadlines{now: now}
	firstSeen := e.firstSeen(now)
	// Only an object health asks about while it is in grace gets a grace deadline.
	seen := func(u *unstructured.Unstructured) time.Time {
		t := firstSeen(u)
		if now.Sub(t) < health.Grace {
			next.add(t.Add(health.Grace), false) // in grace while now-firstSeen < Grace
		}
		return t
	}
	for _, res := range all {
		if e.cfg.Pinned != "" && res.ID() != e.cfg.Pinned {
			continue
		}
		// Read once: the verifier, health and the source fingerprint share this evidence.
		met, has := e.cfg.Metrics.Get(res.ID())
		m.MetricsRead = append(m.MetricsRead, MetricsRead{ID: res.ID(), Result: met, Present: has})
		if has && e.cfg.MetricsMaxAge > 0 {
			next.add(met.CollectedAt.Add(e.cfg.MetricsMaxAge), true) // stale when age > max
		}
		ver := e.cfg.Verifier.Verify(scopeverify.Input{Install: res, Snapshot: snap, Served: served, Metrics: met, HasMetrics: has,
			MetricsMaxAge: e.cfg.MetricsMaxAge, Now: now})
		if !ver.Matures.IsZero() {
			next.add(ver.Matures, true) // reported once now − since > persist
		}
		owned := asg.PerInstall[res.ID()]
		rep := health.Evaluate(health.Input{Install: res, Owned: owned, Assignment: asg, Snapshot: snap, Served: served, KindsChange: kc, Metrics: met,
			HasMetrics: has, MetricsMaxAge: e.cfg.MetricsMaxAge, FirstSeen: seen, Verify: ver, Now: now, Platform: e.cfg.Platform, ProxyMode: e.cfg.ProxyMode}, e.cfg.Checks)
		m.Installs = append(m.Installs, InstallModel{Resolved: res, Owned: owned, Health: rep, Overview: overview.Build(res, owned, snap, served, rep, e.cfg.Platform)})
	}
	m.Next = next.next
	p := &Published{Model: m, Source: Source{Cache: snap.Source, Metrics: encodeMetrics(m.MetricsRead), Kinds: kc.String()}, ConfirmedAt: snap.TakenAt}
	return p
}

// deadlines collects the earliest deadline not yet due at now.
type deadlines struct {
	now  time.Time
	next Deadline
}

// add considers a deadline at at; strict marks an "after at" predicate.
func (d *deadlines) add(at time.Time, strict bool) {
	if c := (Deadline{At: at, Strict: strict}); !c.Due(d.now) {
		d.next = Earliest(d.next, c)
	}
}

// Current returns the latest publication, or nil before the first.
func (e *Engine) Current() *Published { return e.cur.Load() }

// Installs returns the installs of the current model; the metrics collector scrapes these.
func (e *Engine) Installs() []install.Install {
	p := e.Current()
	if p == nil {
		return nil
	}
	out := make([]install.Install, 0, len(p.Model.Installs))
	for _, i := range p.Model.Installs {
		out = append(out, i.Resolved.Install)
	}
	return out
}
