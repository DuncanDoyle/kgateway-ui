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
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/extension"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

// Options are the runtime settings.
type Options struct {
	// Listen is the HTTP address; empty means :8080 in-cluster and 127.0.0.1:8080 locally.
	Listen string
	// Pinned restricts the UI to one install ID ("namespace/name").
	Pinned string
	// PollInterval is the model recompute interval.
	PollInterval time.Duration
	// MetricsInterval is the controller metrics scrape interval.
	MetricsInterval time.Duration
	// UI is the single-page application served at /.
	UI fs.FS
}

// Clients are the Kubernetes clients the app uses.
type Clients struct {
	Dynamic   dynamic.Interface
	Kube      kubernetes.Interface
	Discovery kinds.DiscoveryClient
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
	opts      Options
	// discovered is closed after the first model with an install.
	discovered chan struct{}
}

// Build resolves the served kinds and constructs every component. Both
// intervals must be positive.
func Build(c Clients, o Options, reg *extension.Registry) (*App, error) {
	if o.PollInterval <= 0 || o.MetricsInterval <= 0 {
		return nil, fmt.Errorf("poll interval (%v) and metrics interval (%v) must be positive", o.PollInterval, o.MetricsInterval)
	}
	served, err := kinds.Resolve(c.Discovery, reg.Kinds)
	if err != nil {
		return nil, err
	}
	var podSel, depSel []labels.Selector
	for _, d := range reg.Detectors {
		podSel = append(podSel, d.ControllerPodSelector()) // Pods may lack the Deployment's component label (A1)
		depSel = append(depSel, d.ControllerSelector(), d.ProxySelector())
	}
	ca := cache.New(cache.Config{Dynamic: c.Dynamic, Kube: c.Kube, Served: served, PodSelectors: podSel, DeploymentSelectors: depSel})
	scraper := c.Scraper
	switch {
	case scraper != nil:
	case c.InCluster:
		scraper = ctrlmetrics.HTTPScraper{Client: &http.Client{Timeout: 5 * time.Second}}
	default:
		scraper = ctrlmetrics.PodProxyScraper{Kube: c.Kube}
	}
	col := ctrlmetrics.NewCollector(scraper)
	verifier := reg.Verifier
	if verifier == nil {
		// A mismatch must outlast two scrapes before it is reported (design §4.4).
		verifier = scopeverify.NewManagedCountVerifier(2 * o.MetricsInterval)
	}
	discovered := make(chan struct{})
	var once sync.Once
	// Metrics older than three scrape intervals are unavailable, not reused.
	cfg := engine.Config{Snapshots: ca, Served: served, Detectors: reg.Detectors, Scope: reg.Scope, Verifier: verifier,
		Metrics: col, Checks: reg.Checks, Pinned: o.Pinned, MetricsMaxAge: 3 * o.MetricsInterval,
		OnCompute: func(m *engine.Model) {
			if len(m.Installs) > 0 {
				once.Do(func() { close(discovered) })
			}
		}}
	eng := engine.New(cfg)
	return &App{Engine: eng, Handler: api.NewHandler(eng, o.UI, reg.Authorizer), cfg: cfg, cache: ca, collector: col, opts: o,
		discovered: discovered}, nil
}

// Start runs the cache, the model loop and the metrics collector until ctx
// is done. The collector starts as soon as the first install is discovered,
// so the first metrics do not wait for a collector tick.
func (a *App) Start(ctx context.Context) {
	a.cache.Start(ctx)
	go a.Engine.Run(ctx, a.opts.PollInterval)
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
	a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), InCluster: inCluster}, o, extension.New(opts...))
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
	go func() {
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
