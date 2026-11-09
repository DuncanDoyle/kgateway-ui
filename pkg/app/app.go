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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/snellius/pkg/api"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/ctrllogs"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/diagnose"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/extension"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/proxystats"
	"github.com/DuncanDoyle/snellius/pkg/setup"
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
	// ClassOnlyInstalls adds an install per GatewayClass controllerName that no
	// detector claims, known from Gateway API status alone.
	ClassOnlyInstalls bool
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
	Engine  *engine.Engine
	Handler http.Handler
	cfg     engine.Config
	cache   *cache.Cache
	kinds   *kinds.Monitor
	opts    Options
	// reg starts the extension collectors; packs runs the pack collectors; collected is their store.
	reg, packs *extension.Registry
	collectors []extension.Collector
	collected  *collect.Store
	deps       extension.Deps
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
			NoEvents: o.Diagnostics.NoEvents, NoCRDs: o.Diagnostics.NoCRDs, NoEndpoints: o.Diagnostics.NoEndpoints, Clock: o.Clock,
			ClassOnlyInstalls: o.ClassOnlyInstalls}, reg)
	if err != nil {
		return nil, err
	}
	mon, collected := sc.Kinds, sc.Collected
	ca := cache.New(sc.Cache)
	discovered := make(chan struct{})
	var once sync.Once
	crds := crdRefresher{mon: mon}
	cfg := sc.Engine
	cfg.Snapshots = ca
	cfg.ConfirmEvery, cfg.OnLoop = o.ConfirmEvery, o.OnLoop
	cfg.OnCompute = func(m *engine.Model) {
		// An unsettled cache can hold a Deployment without its Pods; scraping
		// that install fails and is not retried for a full metrics interval.
		if m.Snapshot.Synced && len(m.Installs) > 0 {
			once.Do(func() { close(discovered) })
		}
		crds.observe(m.Snapshot.CRDs)
	}
	eng := engine.New(cfg)
	env := extension.CollectorEnv{Kube: c.Kube, InCluster: c.InCluster, Scraper: c.Scraper, Interval: o.MetricsInterval,
		MaxAge: setup.MetricsMaxAge(o.MetricsInterval), Installs: eng.Installs, Current: eng.Current, Discovered: discovered,
		Collected: collected, NoProxyStats: o.Diagnostics.NoProxyStats, ControllerLogs: o.Diagnostics.ControllerLogs}
	if o.Clock != nil {
		env.Clock = o.Clock
	}
	packCols := reg.PackCollectors(env)
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
	// Each install's next steps, UI features and counted kinds come from its pack.
	commands := func(i install.Install) diagnose.Commands { return reg.ImplFor(i).Commands }
	h := api.NewHandler(eng, o.UI, reg.Authorizer, api.WithExtMounts(mounts...), api.WithCoreCapabilities(core), api.WithCapabilitySources(reg.CapabilityStates),
		api.WithCommands(reg.Commands), api.WithInstallCommands(commands), api.WithInstallFeatures(reg.FeaturesFor),
		api.WithPacks(reg.Packs()...), api.WithInstallKinds(reg.KindAppliesTo))
	return &App{Engine: eng, Handler: h, cfg: cfg, cache: ca, kinds: mon, opts: o, discovered: discovered, reg: reg,
		packs: extension.NewCore(extension.WithCollectors(packCols...)), collectors: packCols, collected: collected, deps: deps}, nil
}

// Start runs the cache, the pack and extension collectors, the model loop and
// the served-kinds monitor until ctx is done. PollInterval is the loop's
// safety timer. The controller metrics collector starts as soon as the first
// install is discovered over a settled cache, so the first metrics do not wait for a tick.
func (a *App) Start(ctx context.Context) {
	a.cache.Start(ctx)
	a.packs.RunCollectors(ctx, a.deps, a.collected)
	a.reg.RunCollectors(ctx, a.deps, a.collected)
	go a.Engine.Run(ctx, a.opts.PollInterval)
	go a.kinds.Run(ctx, a.opts.KindsInterval)
}

// Run is RunWith over extension.New(opts...): the core plus the kgateway pack.
func Run(ctx context.Context, kubeconfig string, o Options, opts ...extension.Option) error {
	return RunWith(ctx, kubeconfig, o, extension.New(opts...))
}

// RunWith builds clients from kubeconfig (or the in-cluster config when empty
// and running in a pod), then serves reg until ctx is done.
func RunWith(ctx context.Context, kubeconfig string, o Options, reg *extension.Registry) error {
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
	a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Metadata: md, InCluster: inCluster}, o, reg)
	if err != nil {
		return err
	}
	return serve(ctx, a, o.Listen, func(addr net.Addr) { slog.Info("snellius listening", "addr", addr.String(), "inCluster", inCluster) })
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
