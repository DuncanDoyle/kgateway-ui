// Package app wires the backend together. Both the upstream main and
// downstream mains use it; downstreams pass extension options.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/extension"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
)

// Options are the runtime settings.
type Options struct {
	// Listen is the HTTP address; empty means :8080 in-cluster and 127.0.0.1:8080 locally.
	Listen string
	// Pinned restricts the UI to one install: its ID, or namespace/name (D-016).
	Pinned string
	// PollInterval is the maximum time between model recomputes: the safety
	// timer. Object, access, metrics and served-kind changes recompute sooner (D-019).
	PollInterval time.Duration
	// MetricsInterval is the controller metrics scrape interval.
	MetricsInterval time.Duration
	// DelegationEdgeCap caps the selected delegation edges per install (rules
	// §5.7); zero means computed.DefaultBounds.Edges. A higher cap needs more
	// memory (D-025).
	DelegationEdgeCap int
	// KindsInterval is how often served kinds are re-resolved; zero means 60 s.
	KindsInterval time.Duration
	// UI is the single-page application served at /.
	UI fs.FS
	// ConfirmEvery is the engine's confirm tick interval; zero means
	// engine.ConfirmEvery. Tests only.
	ConfirmEvery time.Duration
	// OnLoop receives the engine's loop transitions. Tests only.
	OnLoop func(engine.LoopEvent)
	// Clock drives the engine and stamps snapshots and collected values; nil
	// means the real clock. Tests only.
	Clock clock.WithTicker
	// Diagnostics toggles the optional inputs; the zero value keeps the defaults.
	Diagnostics Diagnostics
}

// Diagnostics toggles the optional inputs (design §18.1): No* turns off a
// default-on input, ControllerLogs turns on a default-off one.
type Diagnostics struct {
	NoEvents, NoCRDs, NoProxyStats, NoEndpoints bool
	ControllerLogs                              bool
	// ControllerLogsNamespaces are the install namespaces whose controller logs are read.
	ControllerLogsNamespaces []string
}

// CoreCapabilities returns the core inputs' states for /api/v1/meta; a syncing
// or unstarted watch has none. collected is for inputs later tasks add.
func CoreCapabilities(snap *cache.Snapshot, d Diagnostics, collected collect.Snapshot) []api.Capability {
	inputs := []struct {
		name string
		off  bool
		gvr  schema.GroupVersionResource
	}{
		{"processes", false, cache.ProxyPodsGVR},
		{"events", d.NoEvents, cache.EventsGVR},
		{"crds", d.NoCRDs, cache.CRDsGVR},
	}
	var out []api.Capability
	for _, in := range inputs {
		if in.off {
			out = append(out, api.Capability{Name: in.name, State: api.CapOff})
			continue
		}
		if snap == nil {
			continue
		}
		a, running := snap.Access[in.gvr]
		switch why := health.AccessAvailability(a); {
		case !running:
		case a.OK:
			out = append(out, api.Capability{Name: in.name, State: api.CapOn})
		case why == health.AvailDenied:
			out = append(out, api.Capability{Name: in.name, State: api.CapDenied, Reason: reasonDenied})
		case why == health.AvailFailing:
			out = append(out, api.Capability{Name: in.name, State: api.CapFailing, Reason: reasonFailing})
		}
	}
	return out
}

// /api/v1/meta is not gated, so its reasons are fixed text; the raw error stays in check results.
const (
	reasonDenied  = "Access to this resource is denied"
	reasonFailing = "The watch is failing"
)

// Clients are the Kubernetes clients the app uses.
type Clients struct {
	Dynamic   dynamic.Interface
	Kube      kubernetes.Interface
	Discovery kinds.DiscoveryClient
	// Metadata reads CRD metadata; nil means no CRD watch.
	Metadata metadata.Interface
	// InCluster selects direct pod-IP metric scraping; otherwise pods/proxy is used.
	InCluster bool
	// Scraper overrides the metrics scraper InCluster selects; nil keeps it.
	Scraper ctrlmetrics.Scraper
}

// App is a built, not yet started, backend.
type App struct {
	Engine    *engine.Engine
	Handler   http.Handler
	cfg       engine.Config
	cache     *cache.Cache
	collector *ctrlmetrics.Collector
	kinds     *kinds.Monitor
	opts      Options
	// reg starts the collectors; collected is their store, nil without collectors.
	reg       *extension.Registry
	collected *collect.Store
	deps      extension.Deps
	// discovered is closed after the first model with an install over a settled cache.
	discovered chan struct{}
}

// Build resolves the served kinds and constructs every component. Both
// intervals must be positive and the delegation edge cap not negative.
func Build(c Clients, o Options, reg *extension.Registry) (*App, error) {
	if o.PollInterval <= 0 || o.MetricsInterval <= 0 {
		return nil, fmt.Errorf("poll interval (%v) and metrics interval (%v) must be positive", o.PollInterval, o.MetricsInterval)
	}
	if o.DelegationEdgeCap < 0 {
		return nil, fmt.Errorf("delegation edge cap %d must not be negative", o.DelegationEdgeCap)
	}
	if err := reg.Err(); err != nil {
		return nil, err
	}
	if err := health.ValidateOrder(reg.Checks); err != nil {
		return nil, err
	}
	pinned, err := install.ParsePinned(o.Pinned)
	if err != nil {
		return nil, err
	}
	served, err := kinds.Resolve(c.Discovery, reg.Kinds)
	if err != nil {
		return nil, err
	}
	if o.KindsInterval <= 0 {
		o.KindsInterval = 60 * time.Second
	}
	// One coalescing signal: object, access, metrics and served-kind changes all start a recompute (D-019).
	changes := trigger.New()
	mon := kinds.NewMonitor(c.Discovery, reg.Kinds, served)
	mon.OnChange = changes
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
	ca := cache.New(cache.Config{Dynamic: c.Dynamic, Kube: c.Kube, Served: served, PodSelectors: podSel, DeploymentSelectors: depSel,
		ProxyPodSelectors: proxySel, Events: !o.Diagnostics.NoEvents, CRDs: !o.Diagnostics.NoCRDs, Metadata: c.Metadata, Changes: changes, Now: now})
	scraper := c.Scraper
	switch {
	case scraper != nil:
	case c.InCluster:
		scraper = ctrlmetrics.HTTPScraper{Client: &http.Client{Timeout: 5 * time.Second}}
	default:
		scraper = ctrlmetrics.PodProxyScraper{Kube: c.Kube}
	}
	// Metrics older than three scrape intervals are unavailable, not reused.
	maxAge := 3 * o.MetricsInterval
	col := ctrlmetrics.NewCollector(scraper, ctrlmetrics.WithMaxAge(maxAge))
	col.OnResult = changes
	verifier := reg.Verifier
	if verifier == nil {
		// A mismatch must outlast two scrapes before it is reported (design §4.4).
		verifier = scopeverify.NewManagedCountVerifier(2 * o.MetricsInterval)
	}
	discovered := make(chan struct{})
	var once sync.Once
	cfg := engine.Config{Snapshots: ca, Served: served, KindsChange: mon.Change, Detectors: reg.Detectors, Scope: reg.Scope, Verifier: verifier,
		Metrics: col, Checks: reg.Checks, Pinned: pinned, Platform: platform.Kubernetes(), MetricsMaxAge: maxAge, ProxyMode: reg.ProxyMode,
		RulesFor: reg.RulesFor, Process: reg.Process,
		Triggers: changes, ConfirmEvery: o.ConfirmEvery, OnLoop: o.OnLoop, Clock: o.Clock,
		// The other bounds stay zero: the walker gives each its default.
		DelegationBounds: computed.Bounds{Edges: o.DelegationEdgeCap},
		OnCompute: func(m *engine.Model) {
			// An unsettled cache can hold a Deployment without its Pods; scraping
			// that install fails and is not retried for a full metrics interval.
			if m.Snapshot.Synced && len(m.Installs) > 0 {
				once.Do(func() { close(discovered) })
			}
		}}
	var collected *collect.Store
	if len(reg.Collectors()) > 0 {
		collected = collect.New(changes, now)
		cfg.Collected = collected.Snapshot
	}
	eng := engine.New(cfg)
	if err := reg.CheckCapabilities(eng.Platform().Capabilities); err != nil {
		return nil, err
	}
	deps := extension.Deps{Current: eng.Current, Authorizer: reg.Authorizer, Capabilities: reg.CapabilityStates, Logger: slog.Default()}
	mounts, err := reg.Mounts(deps)
	if err != nil {
		return nil, err
	}
	core := func() []api.Capability {
		var snap *cache.Snapshot
		if p := eng.Current(); p != nil && p.Model != nil {
			snap = p.Model.Snapshot
		}
		// No core capability reads collected values yet, so none are copied per request.
		return CoreCapabilities(snap, o.Diagnostics, collect.Snapshot{})
	}
	h := api.NewHandler(eng, o.UI, reg.Authorizer, api.WithExtMounts(mounts...), api.WithCoreCapabilities(core), api.WithCapabilitySources(reg.CapabilityStates))
	return &App{Engine: eng, Handler: h, cfg: cfg, cache: ca, collector: col, kinds: mon, opts: o,
		discovered: discovered, reg: reg, collected: collected, deps: deps}, nil
}

// Start runs the cache, the extension collectors, the model loop, the
// served-kinds monitor and the metrics collector until ctx is done. PollInterval is the loop's safety timer. The collector starts as soon as the first install is discovered
// over a settled cache, so the first metrics do not wait for a collector tick.
func (a *App) Start(ctx context.Context) {
	a.cache.Start(ctx)
	if a.collected != nil {
		a.reg.RunCollectors(ctx, a.deps, a.collected)
	}
	go a.Engine.Run(ctx, a.opts.PollInterval)
	go a.kinds.Run(ctx, a.opts.KindsInterval)
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-a.discovered:
		}
		if err := a.collector.Run(ctx, a.opts.MetricsInterval, a.Engine.Installs); err != nil {
			slog.Error("metrics collector stopped", "err", err)
		}
	}()
}

// Run builds clients from kubeconfig (or the in-cluster config when empty and
// running in a pod), then serves until ctx is done.
func Run(ctx context.Context, kubeconfig string, o Options, opts ...extension.Option) error {
	ApplyMemoryLimit()
	cfg, inCluster, err := restConfig(kubeconfig)
	if err != nil {
		return err
	}
	if o.Listen == "" {
		o.Listen = "127.0.0.1:8080"
		if inCluster {
			o.Listen = ":8080"
		}
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}
	md, err := metadata.NewForConfig(cfg)
	if err != nil {
		return err
	}
	a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Metadata: md, InCluster: inCluster}, o, extension.New(opts...))
	if err != nil {
		return err
	}
	return serve(ctx, a, o.Listen, func(addr net.Addr) { slog.Info("kgateway UI listening", "addr", addr.String(), "inCluster", inCluster) })
}

// serve listens on addr, starts a and serves its handler until ctx is done,
// then drains in-flight requests for up to 5 s. Everything it started is
// cancelled when it returns; onListen, if set, receives the bound address.
func serve(ctx context.Context, a *App, addr string, onListen func(net.Addr)) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	a.Start(ctx)
	srv := &http.Server{Handler: a.Handler, ReadHeaderTimeout: 10 * time.Second}
	drained := make(chan struct{})
	go func() { //nolint:gosec // shutdown must outlive the cancelled serve context
		defer close(drained)
		<-ctx.Done()
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		_ = srv.Shutdown(sctx)
	}()
	if onListen != nil {
		onListen(ln.Addr())
	}
	err = srv.Serve(ln)
	cancel()
	<-drained
	if !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// restConfig prefers the in-cluster config when kubeconfig is empty, then
// falls back to the default loading rules.
func restConfig(kubeconfig string) (*rest.Config, bool, error) {
	if kubeconfig == "" {
		if c, err := rest.InClusterConfig(); err == nil {
			return c, true, nil
		}
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	c, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
	return c, false, err
}
