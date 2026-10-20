// Package engine periodically computes the UI's model from the cache, the
// controller metrics and the registered checks.
package engine

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/overview"
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
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
	// Logger receives install rejections; nil means slog.Default().
	Logger *slog.Logger
	// MetricsMaxAge is the age beyond which a metrics result is stale
	// (3 × the metrics interval). Zero disables staleness: never use it in production.
	MetricsMaxAge time.Duration
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// OnCompute, if set, receives each model Run computes, on Run's goroutine.
	OnCompute func(*Model)
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
}

// Engine computes Models.
type Engine struct {
	cfg Config
	mu  sync.RWMutex
	cur *Model
	// computeMu serialises Compute; seen is only touched under it.
	computeMu sync.Mutex
	seen      map[generation]time.Time
	rejected  map[string]bool // install errors already logged; only touched under computeMu
}

// generation identifies one spec version of one object.
type generation struct {
	uid types.UID
	gen int64
}

// New returns an Engine.
func New(cfg Config) *Engine {
	if cfg.Now == nil {
		cfg.Now = time.Now
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

// Compute builds a new Model and makes it current.
func (e *Engine) Compute() *Model {
	e.computeMu.Lock()
	defer e.computeMu.Unlock()
	snap := e.cfg.Snapshots.Snapshot()
	now := e.cfg.Now()
	e.observe(snap, now)
	served := e.cfg.Served
	m := &Model{Snapshot: snap, Served: served, Pinned: e.cfg.Pinned, ComputedAt: now}
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
	seen := e.firstSeen(now)
	for _, res := range all {
		if e.cfg.Pinned != "" && res.ID() != e.cfg.Pinned {
			continue
		}
		met, has := e.cfg.Metrics.Get(res.ID())
		ver := e.cfg.Verifier.Verify(scopeverify.Input{Install: res, Snapshot: snap, Served: served, Metrics: met, HasMetrics: has,
			MetricsMaxAge: e.cfg.MetricsMaxAge, Now: now})
		owned := asg.PerInstall[res.ID()]
		rep := health.Evaluate(health.Input{Install: res, Owned: owned, Assignment: asg, Snapshot: snap, Served: served, KindsChange: kc, Metrics: met,
			HasMetrics: has, MetricsMaxAge: e.cfg.MetricsMaxAge, FirstSeen: seen, Verify: ver, Now: now, Platform: e.cfg.Platform}, e.cfg.Checks)
		m.Installs = append(m.Installs, InstallModel{Resolved: res, Owned: owned, Health: rep, Overview: overview.Build(res, owned, snap, served, rep, e.cfg.Platform)})
	}
	e.mu.Lock()
	e.cur = m
	e.mu.Unlock()
	return m
}

// Current returns the latest Model, or nil before the first Compute.
func (e *Engine) Current() *Model {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cur
}

// Installs returns the installs of the current model; the metrics collector scrapes these.
func (e *Engine) Installs() []install.Install {
	m := e.Current()
	if m == nil {
		return nil
	}
	out := make([]install.Install, 0, len(m.Installs))
	for _, i := range m.Installs {
		out = append(out, i.Resolved.Install)
	}
	return out
}

// Run computes immediately and then every interval until ctx is done,
// passing each model to Config.OnCompute.
func (e *Engine) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if m := e.Compute(); e.cfg.OnCompute != nil {
			e.cfg.OnCompute(m)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
