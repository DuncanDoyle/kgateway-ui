// Package setup builds the cache and engine configuration the UI runs on, for
// callers that run the engine themselves, such as a one-shot CLI (D-053).
// pkg/app builds its own configuration here, so the two cannot drift.
//
// Nothing here waits. The time-based checks (restart hold, grace periods,
// scope-verifier persistence) read as pending in a single pass. A fresh
// metrics result reads OK, and Next marks when it goes stale. Controller
// metrics reach the engine only through Config.Collected; the ctrlmetrics
// package documents the one-shot sequence (CollectOnce, then Publish).
// [engine.Model.Next] and each [health.Report.Next] say when computing again
// can change the result. Waiting, and for how long, is the caller's.
//
// Before a single pass, wait with [cache.Cache.Settled], not by receiving on
// Changes. Settled covers the required informers only. Then wait until each
// optional watch in Snapshot().Access ([cache.ProxyPodsGVR], [cache.CRDsGVR],
// [cache.EventsGVR], [cache.ServicesGVR], [cache.EndpointSlicesGVR], when present)
// and each list-only served kind ([kinds.Descriptor.ListOnly]) is OK or has an Err.
// Otherwise a pass can read them as not yet read, and the output varies between runs.
//
// To stop, cancel the context passed to [cache.Cache.Start], then call
// [cache.Cache.Wait], which returns once every goroutine Start launched has exited.
//
// Controller logs and proxy stats come from long-running collectors that
// pkg/app runs and this package does not. A caller without them gets those
// inputs as not read, never as healthy (D-006): controllerLogs reads as not
// read, and the checks over proxy stats or controller metrics report them not collected.
package setup

import (
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/extension"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/platform"
	"github.com/DuncanDoyle/snellius/pkg/scopeverify"
	"github.com/DuncanDoyle/snellius/pkg/trigger"
)

// Clients are the Kubernetes clients the cache and kind discovery read.
type Clients struct {
	Dynamic   dynamic.Interface
	Kube      kubernetes.Interface
	Discovery kinds.DiscoveryClient
	// Metadata reads CRD metadata; nil means no CRD watch.
	Metadata metadata.Interface
}

// Options are the settings both configs derive from.
type Options struct {
	// Pinned restricts the model to one install: its ID, or namespace/name (D-016).
	Pinned string
	// MetricsInterval is the controller metrics collection interval; it must be
	// positive. Metrics staleness and scope-verifier persistence derive from it.
	MetricsInterval time.Duration
	// DelegationEdgeCap caps the selected delegation edges per install; zero
	// means computed.DefaultBounds.Edges. It must not be negative.
	DelegationEdgeCap int
	// NoEvents and NoCRDs turn off the Warning Event and CRD metadata watches.
	NoEvents, NoCRDs bool
	// NoEndpoints turns off the Service and EndpointSlice watches; the zero value keeps them on.
	NoEndpoints bool
	// ClassOnlyInstalls adds an install per GatewayClass controllerName no
	// detector claims (engine.Config.ClassOnly).
	ClassOnlyInstalls bool
	// Clock drives the engine and stamps snapshots; nil means the real clock.
	Clock clock.WithTicker
}

// Config is what Build returns. The caller creates the cache from Cache and
// sets Engine.Snapshots to it.
type Config struct {
	Cache  cache.Config
	Engine engine.Config
	// Collected is the store Engine.Collected reads. pkg/app's collectors
	// publish to it; a one-shot caller publishes controller metrics with ctrlmetrics.Publish.
	Collected *collect.Store
	// Kinds re-resolves the served kinds and the Catalog; Build has refreshed it
	// once. A long-running caller runs it; a one-shot caller can ignore it.
	Kinds *kinds.Monitor
	// Changes is the one coalescing recompute signal the cache, the kinds
	// monitor and the engine share (D-019). A one-shot caller waits with
	// Cache.Settled and the optional and list-only Access entries, not on Changes.
	Changes *trigger.Signal
}

// noRuleSet is the rule-set choice of a pack without one, such as the core's
// for a class-only install: no computed views.
func noRuleSet(string) computed.Selection { return computed.Selection{} }

// MetricsMaxAge is the age beyond which a metrics result is stale: three
// collection intervals, so a stalled collector cannot keep an old result green.
func MetricsMaxAge(interval time.Duration) time.Duration { return 3 * interval }

// Build validates o and reg, resolves the served kinds and lists the Catalog
// once, and returns the cache and engine configuration pkg/app runs on.
// Engine.Snapshots is the caller's to set.
func Build(c Clients, o Options, reg *extension.Registry) (Config, error) {
	if o.MetricsInterval <= 0 {
		return Config{}, fmt.Errorf("metrics interval (%v) must be positive", o.MetricsInterval)
	}
	if o.DelegationEdgeCap < 0 {
		return Config{}, fmt.Errorf("delegation edge cap %d must not be negative", o.DelegationEdgeCap)
	}
	if err := reg.Err(); err != nil {
		return Config{}, err
	}
	if err := health.ValidateOrder(reg.Checks); err != nil {
		return Config{}, err
	}
	pinned, err := install.ParsePinned(o.Pinned)
	if err != nil {
		return Config{}, err
	}
	served, err := kinds.Resolve(c.Discovery, reg.Kinds)
	if err != nil {
		return Config{}, err
	}
	// Pack collectors publish under their names; an extension must not take them.
	for _, pc := range reg.PackCollectors(extension.CollectorEnv{}) {
		if slices.ContainsFunc(reg.Collectors(), func(c extension.Collector) bool { return c.Name() == pc.Name() }) {
			return Config{}, fmt.Errorf("collector name %q is reserved for a pack collector", pc.Name())
		}
	}
	plat := platform.Kubernetes()
	if err := reg.CheckCapabilities(plat.Capabilities); err != nil {
		return Config{}, err
	}
	changes := trigger.New()
	mon := kinds.NewMonitor(c.Discovery, reg.Kinds, served)
	mon.OnChange = changes
	// The first refresh lists the Catalog, so 013 and 014 explain refs from the first compute.
	if err := mon.Refresh(); err != nil {
		slog.Warn("listing served kinds failed; refs are explained from the controller's message", "err", err)
	}
	var podSel, depSel, proxySel []labels.Selector
	for _, d := range reg.Detectors {
		podSel = append(podSel, d.ControllerPodSelector()) // Pods may lack the Deployment's component label
		depSel = append(depSel, d.ControllerSelector(), d.ProxySelector())
		if pd, ok := d.(install.ProxyPodDetector); ok {
			proxySel = append(proxySel, pd.ProxyPodSelector())
		}
	}
	now := time.Now
	if o.Clock != nil {
		now = o.Clock.Now
	}
	collected := collect.New(changes, now)
	// Each pack's checks are listed once; HooksFor runs several times per install and compute.
	var mu sync.Mutex
	checksFor := map[string][]health.Check{}
	checks := func(impl string) []health.Check {
		mu.Lock()
		defer mu.Unlock()
		cs, ok := checksFor[impl]
		if !ok {
			cs = reg.ChecksFor(impl)
			checksFor[impl] = cs
		}
		return cs
	}
	verifier := reg.Verifier
	if verifier == nil {
		// A mismatch must outlast two collections before it is reported (design §4.4).
		verifier = scopeverify.NewManagedCountVerifier(2 * o.MetricsInterval)
	}
	// The default verifier is the default pack's; the core and other packs keep their own.
	verifierFor := func(i install.Install, im extension.Impl) scopeverify.Verifier {
		if im.Verifier == nil && i.Impl != "" && i.Impl == reg.DefaultPack() {
			return verifier
		}
		return im.Verifier
	}
	return Config{
		Cache: cache.Config{Dynamic: c.Dynamic, Kube: c.Kube, Served: served, PodSelectors: podSel, DeploymentSelectors: depSel,
			ProxyPodSelectors: proxySel, Events: !o.NoEvents, CRDs: !o.NoCRDs, Endpoints: !o.NoEndpoints, Metadata: c.Metadata, Changes: changes, Now: now},
		Engine: engine.Config{Served: served, KindsChange: mon.Change, Catalog: mon.Catalog, Detectors: reg.Detectors, Scope: reg.Scope,
			Verifier: verifier, Checks: reg.Checks, Pinned: pinned, Platform: plat,
			ProxyMode: reg.ProxyMode, RulesFor: reg.RulesFor, Process: reg.Process, Collected: collected.Snapshot,
			HooksFor: func(i install.Install) engine.Hooks {
				im := reg.ImplFor(i)
				rules := im.RulesFor
				if rules == nil {
					rules = noRuleSet
				}
				return engine.Hooks{Scope: im.Scope, Settings: im.Settings, RulesFor: rules, Version: im.Version, Reported: im.Reported,
					ExperimentalOnly: im.ExperimentalOnly, Checks: checks(i.Impl), Conditions: im.Conditions,
					Verifier: verifierFor(i, im), Process: im.Process, ProxyMode: im.ProxyMode}
			},
			Triggers: changes, Clock: o.Clock, ClassOnly: o.ClassOnlyInstalls,
			// The other bounds stay zero: the walker gives each its default.
			DelegationBounds: computed.Bounds{Edges: o.DelegationEdgeCap}},
		Kinds:     mon,
		Changes:   changes,
		Collected: collected,
	}, nil
}
