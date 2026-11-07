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
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"
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
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrllogs"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/extension"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
	"github.com/DuncanDoyle/kgateway-ui/pkg/setup"
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
// or unstarted watch, and proxy stats before their first round, have none.
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
	if d.NoEndpoints {
		out = append(out, api.Capability{Name: "endpoints", State: api.CapOff})
	} else if c, ok := endpointsCapability(snap); ok {
		out = append(out, c)
	}
	// Per-Gateway failures stay in the value; only a stopped or stuck collector is failing here.
	switch v, ok := collected.Get(proxystats.Key); {
	case d.NoProxyStats:
		out = append(out, api.Capability{Name: "proxyStats", State: api.CapOff})
	case !ok:
	case v.State == collect.OK:
		out = append(out, api.Capability{Name: "proxyStats", State: api.CapOn})
	case v.State == collect.Off:
		out = append(out, api.Capability{Name: "proxyStats", State: api.CapOff})
	default:
		out = append(out, api.Capability{Name: "proxyStats", State: api.CapFailing, Reason: reasonStatsFailing})
	}
	if !d.ControllerLogs {
		return append(out, api.Capability{Name: "controllerLogs", State: api.CapOff})
	}
	st, ok := ctrllogs.Availability(collected)
	if !ok {
		return out
	}
	c := api.Capability{Name: "controllerLogs", State: api.CapFailing}
	switch st {
	case ctrllogs.StateOn:
		c.State = api.CapOn
	case ctrllogs.StateDenied:
		c.State, c.Reason = api.CapDenied, reasonDenied
	case ctrllogs.StateUnrecognised:
		c.Reason = reasonLogsUnrecognised
	case ctrllogs.StateNoParser:
		c.Reason = reasonLogsNoParser
	case ctrllogs.StateNotConnected:
		c.Reason = reasonLogsNotConnected
	default:
		c.Reason = reasonLogsFailing
	}
	return append(out, c)
}

// endpointsCapability merges the Service and EndpointSlice watches: on only when
// both run, denied when either is, else failing. ok is false while either is
// missing or syncing.
func endpointsCapability(snap *cache.Snapshot) (api.Capability, bool) {
	if snap == nil {
		return api.Capability{}, false
	}
	var failing, denied bool
	for _, gvr := range []schema.GroupVersionResource{cache.ServicesGVR, cache.EndpointSlicesGVR} {
		a, running := snap.Access[gvr]
		switch why := health.AccessAvailability(a); {
		case !running:
			return api.Capability{}, false
		case a.OK:
		case why == health.AvailDenied:
			denied = true
		case why == health.AvailFailing:
			failing = true
		default:
			return api.Capability{}, false
		}
	}
	switch {
	case denied:
		return api.Capability{Name: "endpoints", State: api.CapDenied, Reason: reasonDenied}, true
	case failing:
		return api.Capability{Name: "endpoints", State: api.CapFailing, Reason: reasonFailing}, true
	}
	return api.Capability{Name: "endpoints", State: api.CapOn}, true
}

// /api/v1/meta is not gated, so its reasons are fixed text; the raw error stays in check results.
const (
	reasonDenied  = "Access to this resource is denied"
	reasonFailing = "The watch is failing"
	// reasonStatsFailing: the collector stopped or is stuck (wording pass, Task 51).
	reasonStatsFailing = "Proxy stats are not being collected"
	// Controller log reasons (design §8.9).
	reasonLogsFailing      = "Controller logs cannot be read"
	reasonLogsUnrecognised = "The controller log format is not recognised"
	reasonLogsNoParser     = "No log format is known for this kgateway version"
	reasonLogsNotConnected = "No controller log stream is connected"
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
	// reg and core start the extension and core collectors; collected is their store.
	reg, core *extension.Registry
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
	if o.KindsInterval <= 0 {
		o.KindsInterval = 60 * time.Second
	}
	sc, err := setup.Build(setup.Clients{Dynamic: c.Dynamic, Kube: c.Kube, Discovery: c.Discovery, Metadata: c.Metadata},
		setup.Options{Pinned: o.Pinned, MetricsInterval: o.MetricsInterval, DelegationEdgeCap: o.DelegationEdgeCap,
			NoEvents: o.Diagnostics.NoEvents, NoCRDs: o.Diagnostics.NoCRDs, NoEndpoints: o.Diagnostics.NoEndpoints, Clock: o.Clock}, reg)
	if err != nil {
		return nil, err
	}
	changes, mon := sc.Changes, sc.Kinds
	now := time.Now
	if o.Clock != nil {
		now = o.Clock.Now
	}
	ca := cache.New(sc.Cache)
	scraper := c.Scraper
	switch {
	case scraper != nil:
	case c.InCluster:
		scraper = ctrlmetrics.HTTPScraper{Client: ctrlmetrics.NewHTTPClient()}
	default:
		scraper = ctrlmetrics.PodProxyScraper{Kube: c.Kube}
	}
	col := ctrlmetrics.NewCollector(scraper, ctrlmetrics.WithMaxAge(sc.Engine.MetricsMaxAge))
	col.OnResult = changes
	discovered := make(chan struct{})
	var once sync.Once
	crds := crdRefresher{mon: mon}
	cfg := sc.Engine
	cfg.Snapshots, cfg.Metrics = ca, col
	cfg.ConfirmEvery, cfg.OnLoop = o.ConfirmEvery, o.OnLoop
	cfg.OnCompute = func(m *engine.Model) {
		// An unsettled cache can hold a Deployment without its Pods; scraping
		// that install fails and is not retried for a full metrics interval.
		if m.Snapshot.Synced && len(m.Installs) > 0 {
			once.Do(func() { close(discovered) })
		}
		crds.observe(m.Snapshot.CRDs)
	}
	// The proxy stats collector is core, so the engine always reads the store.
	collected := collect.New(changes, now)
	cfg.Collected = collected.Snapshot
	eng := engine.New(cfg)
	stats := proxyStatsCollector(c, o, reg, eng)
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
		return CoreCapabilities(snap, o.Diagnostics, collected.Snapshot())
	}
	h := api.NewHandler(eng, o.UI, reg.Authorizer, api.WithExtMounts(mounts...), api.WithCoreCapabilities(core), api.WithCapabilitySources(reg.CapabilityStates), api.WithCommands(reg.Commands))
	cores := []extension.Collector{stats}
	if o.Diagnostics.ControllerLogs {
		cores = append(cores, controllerLogsCollector(c, o, eng, collected))
	}
	return &App{Engine: eng, Handler: h, cfg: cfg, cache: ca, collector: col, kinds: mon, opts: o,
		discovered: discovered, reg: reg, core: extension.New(extension.WithCollectors(cores...)), collected: collected, deps: deps}, nil
}

// controllerLogsCollector reads each install's controller logs through pods/log
// with the UI's Role, and names only from the latest proxy stats (D-037).
func controllerLogsCollector(c Clients, o Options, eng *engine.Engine, collected *collect.Store) logsCollector {
	installs := func() ([]ctrllogs.Install, bool) {
		p := eng.Current()
		if p == nil || p.Model == nil || p.Model.Snapshot == nil || !p.Model.Snapshot.Synced {
			return nil, false
		}
		snap := p.Model.Snapshot
		out := make([]ctrllogs.Install, 0, len(p.Model.Installs))
		for _, im := range p.Model.Installs {
			in := ctrllogs.Install{Resolved: im.Resolved, Parser: im.Rules.LogParser()}
			if im.Owned != nil {
				for _, g := range im.Owned.Gateways {
					in.Gateways = append(in.Gateways, objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: g.GetNamespace(), Name: g.GetName()})
				}
			}
			gws := in.Gateways
			// A deployer error keeps its object only when it is a Gateway of the install or a Deployment the UI sees.
			in.Known = func(r objects.Ref) bool {
				if r.Kind == "Deployment" && (r.Group == "" || r.Group == "apps") {
					return slices.ContainsFunc(snap.Deployments, func(d *appsv1.Deployment) bool { return d.Namespace == r.Namespace && d.Name == r.Name })
				}
				return slices.ContainsFunc(gws, func(g objects.Ref) bool { return g == r })
			}
			out = append(out, in)
		}
		return out, true
	}
	// Until the first proxy stats value lands, streams wait, so the backfill meets its names.
	stats := func() (proxystats.Stats, ctrllogs.StatsState) {
		v, ok := collected.Snapshot().Get(proxystats.Key)
		if !ok {
			return nil, ctrllogs.StatsPending
		}
		if s, isStats := v.Data.(proxystats.Stats); v.State == collect.OK && isStats {
			return s, ctrllogs.StatsReady
		}
		return nil, ctrllogs.StatsUnavailable
	}
	var clk clock.WithTicker
	if o.Clock != nil {
		clk = o.Clock
	}
	return logsCollector{ctrllogs.New(ctrllogs.Config{Source: ctrllogs.Kube{Client: c.Kube}, Every: o.MetricsInterval, Clock: clk, Installs: installs, Stats: stats})}
}

// logsCollector runs the controller log collector on the collector seam.
type logsCollector struct{ c *ctrllogs.Collector }

func (logsCollector) Name() string { return ctrllogs.CollectorName }

func (lc logsCollector) Start(ctx context.Context, d extension.Deps) error {
	return lc.c.Run(ctx, d.Publish, d.Fail)
}

// proxyStatsCollector is the core proxy stats collector over reg's Source, or
// proxystats.Kube: the Pod IP in-cluster, pods/proxy with the user's rights locally (D-037).
func proxyStatsCollector(c Clients, o Options, reg *extension.Registry, eng *engine.Engine) coreCollector {
	src := reg.ProxyStats
	if src == nil {
		k := proxystats.Kube{Client: proxystats.NewHTTPClient()}
		if !c.InCluster {
			k.Kube = c.Kube
		}
		src = k
	}
	installs := func() ([]proxystats.Install, *cache.Snapshot) {
		p := eng.Current()
		// An unsettled cache can lack proxy Pods; the round waits for a synced one.
		if p == nil || p.Model == nil || p.Model.Snapshot == nil || !p.Model.Snapshot.Synced {
			return nil, nil
		}
		out := make([]proxystats.Install, 0, len(p.Model.Installs))
		for _, im := range p.Model.Installs {
			in := proxystats.Install{Resolved: im.Resolved}
			if im.Owned != nil {
				for _, g := range im.Owned.Gateways {
					in.Gateways = append(in.Gateways, objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: g.GetNamespace(), Name: g.GetName()})
				}
			}
			out = append(out, in)
		}
		return out, p.Model.Snapshot
	}
	var clk clock.WithTicker
	if o.Clock != nil {
		clk = o.Clock
	}
	return coreCollector{proxystats.New(proxystats.Config{Source: src, Every: o.MetricsInterval, Clock: clk, Installs: installs, Off: o.Diagnostics.NoProxyStats})}
}

// coreCollector runs the proxy stats collector on the collector seam.
type coreCollector struct{ c *proxystats.Collector }

func (coreCollector) Name() string { return proxystats.CollectorName }

func (cc coreCollector) Start(ctx context.Context, d extension.Deps) error {
	return cc.c.Run(ctx, d.Publish, d.Fail)
}

// Start runs the cache, the core and extension collectors, the model loop, the
// served-kinds monitor and the metrics collector until ctx is done. PollInterval is the loop's safety timer. The collector starts as soon as the first install is discovered
// over a settled cache, so the first metrics do not wait for a collector tick.
func (a *App) Start(ctx context.Context) {
	a.cache.Start(ctx)
	a.core.RunCollectors(ctx, a.deps, a.collected)
	a.reg.RunCollectors(ctx, a.deps, a.collected)
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

// crdRefresher re-lists the Catalog when the Gateway API CRD set changes, so a
// CRD installed while the UI runs is served from the next compute, not the next
// interval. Other groups wait for the interval. observe runs on the engine loop.
type crdRefresher struct {
	mon        *kinds.Monitor
	seen       bool
	key        string
	refreshing atomic.Bool
}

func (r *crdRefresher) observe(crds []cache.CRDMeta) {
	names := make([]string, 0, len(crds))
	for _, c := range crds {
		names = append(names, c.Name)
	}
	key := strings.Join(names, ",")
	if !r.seen {
		r.seen, r.key = true, key
		return
	}
	// A refresh in flight may predate this change; the key stays old so the next compute retries.
	if key == r.key || !r.refreshing.CompareAndSwap(false, true) {
		return
	}
	r.key = key
	go func() {
		defer r.refreshing.Store(false)
		if err := r.mon.Refresh(); err != nil {
			slog.Warn("re-listing served kinds after a CRD change failed", "err", err)
		}
	}()
}
