package extension

import (
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/diagnose"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kgateway"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/process"
	"github.com/DuncanDoyle/snellius/pkg/proxymode"
	"github.com/DuncanDoyle/snellius/pkg/proxystats"
	"github.com/DuncanDoyle/snellius/pkg/scopeverify"
)

// Impl is one pack's per-install hooks, used for the installs its detectors
// claim (Phase 5 design §4.2). A nil hook is the core's: all namespaces
// assumed in scope, no settings read, no version, no experimental-only kinds,
// no scope verification, no processes, every proxy managed and the API's
// default commands.
type Impl struct {
	Scope    install.ScopeProvider
	Verifier scopeverify.Verifier
	Settings func(install.Install) install.Settings
	RulesFor func(version string) computed.Selection
	// Version is the install's implementation version, the input of RulesFor.
	Version func(install.Install) string
	// Reported reads the version the controller reports from the collected
	// values; false keeps the last one (engine.Hooks.Reported).
	Reported func(install.Install, collect.Snapshot) (string, bool)
	// ExperimentalOnly reports whether the controller watches served kind k only
	// with experimental Gateway API features on.
	ExperimentalOnly func(k kinds.Served, inst install.Install) bool
	ProxyMode        proxymode.Resolver
	Process          process.Source
	ProxyStats       proxystats.Source
	Commands         diagnose.Commands
	// Conditions are the implementation's condition types beyond Gateway API's.
	Conditions health.ConditionTypes
}

// Pack is one implementation's contributions to the core. Its Name is stamped
// on each install its detectors claim (install.Install.Impl).
type Pack struct {
	Name      string
	Kinds     []kinds.Descriptor
	Detectors []install.Detector
	Impl      Impl
	Checks    []health.Check
	// Collectors build the pack's collectors when the app is built. Their names
	// are reserved: a WithCollectors collector cannot take them.
	Collectors []func(CollectorEnv) Collector
	// Features are the UI features the pack's installs support (api.Feature*);
	// the API drops computed and delegation where the rule set lacks them.
	Features []string
}

// CollectorEnv is what the app builds a pack's collectors with (Phase 5 design
// §7.1). A factory only builds, so it may get a zero CollectorEnv to tell its
// name; the collector starts in Start.
type CollectorEnv struct {
	// Kube reads Pods, their logs and, outside the cluster, pods/proxy.
	Kube kubernetes.Interface
	// InCluster reads Pod IPs directly; otherwise through pods/proxy.
	InCluster bool
	// Scraper replaces the controller metrics scraper InCluster selects; nil keeps it.
	Scraper ctrlmetrics.Scraper
	// Interval is the collection interval; MaxAge, 3 × Interval, is when a value is stale.
	Interval, MaxAge time.Duration
	// Clock drives the collectors; nil means the real clock.
	Clock clock.WithTicker
	// Installs and Current return the published model's installs and the model.
	Installs func() []install.Install
	Current  func() *engine.Published
	// Discovered is closed after the first model with an install over a settled cache.
	Discovered <-chan struct{}
	// Collected is the store the collectors publish to; one may read another's values.
	Collected *collect.Store
	// NoProxyStats turns proxy stats off; ControllerLogs turns log reading on.
	NoProxyStats, ControllerLogs bool
	// Pack is the pack's name and Impl its hooks, read when the app is built.
	Pack string
	Impl Impl
}

// WithPack adds p: its kinds, its detectors (tried before those already
// registered) and its checks. The first pack added becomes the default pack
// (kgateway in New): the old hook options, WithDetectors and the Registry hook
// fields set its hooks, whatever their order. A later pack keeps p.Impl. A pack
// name that is empty or already registered adds nothing and Registry.Err reports it.
func WithPack(p Pack) Option {
	return func(r *Registry) {
		if _, dup := r.impls[p.Name]; p.Name == "" || dup || p.Name == r.pack {
			r.fail("pack name %q is empty or already registered", p.Name)
			return
		}
		if r.pack == "" {
			r.impls[""] = r.currentImpl() // the core's hooks, before the default pack takes the fields
			r.pack = p.Name
			r.setImpl(p.Impl)
		} else {
			r.impls[p.Name] = p.Impl
		}
		r.Kinds = kinds.Dedupe(append(r.Kinds, p.Kinds...))
		r.Detectors = append(stamp(p.Name, p.Detectors), r.Detectors...)
		r.Checks = append(r.Checks, p.Checks...)
		r.features[p.Name] = slices.Clone(p.Features)
		for _, c := range p.Checks {
			r.checkPack[c.Name()] = p.Name
		}
		for _, f := range p.Collectors {
			r.packCollectors = append(r.packCollectors, packCollector{pack: p.Name, build: f})
		}
	}
}

// packCollector is one pack's collector factory.
type packCollector struct {
	pack  string
	build func(CollectorEnv) Collector
}

// ChecksFor returns the checks that run on installs of pack impl, in
// registration order: the core's and that pack's. WithChecks adds checks to
// the default pack, so they run on its installs.
func (r *Registry) ChecksFor(impl string) []health.Check {
	if _, known := r.impls[impl]; !known && impl != r.pack {
		impl = "" // an install no registered pack claims gets the core's checks
	}
	out := make([]health.Check, 0, len(r.Checks))
	for _, c := range r.Checks {
		if p := r.checkPack[c.Name()]; p == "" || p == impl {
			out = append(out, c)
		}
	}
	return out
}

// PackCollectors builds every pack's collectors with env, in registration
// order, each with its pack's name and hooks.
func (r *Registry) PackCollectors(env CollectorEnv) []Collector {
	out := make([]Collector, 0, len(r.packCollectors))
	for _, pc := range r.packCollectors {
		e := env
		e.Pack, e.Impl = pc.pack, r.ImplFor(install.Install{Impl: pc.pack})
		out = append(out, pc.build(e))
	}
	return out
}

// ImplFor returns the hooks of the pack that claimed i, read from the Registry
// fields for the default pack. An install no registered pack claims, such as a
// class-only install, gets the core's hooks.
func (r *Registry) ImplFor(i install.Install) Impl {
	switch im, ok := r.impls[i.Impl]; {
	case i.Impl == r.pack:
		return r.currentImpl()
	case ok:
		return im
	case r.pack == "":
		return r.currentImpl() // no pack yet: the fields are the core's
	}
	return r.impls[""]
}

// FeaturesFor returns the UI features of the pack that claimed i; none for the core.
func (r *Registry) FeaturesFor(i install.Install) []string { return slices.Clone(r.features[i.Impl]) }

// currentImpl is the default pack's hooks, held in the Registry fields.
func (r *Registry) currentImpl() Impl {
	return Impl{Scope: r.Scope, Verifier: r.Verifier, Settings: r.settings, RulesFor: r.RulesFor, Version: r.version, Reported: r.reported,
		ExperimentalOnly: r.experimentalOnly, ProxyMode: r.ProxyMode, Process: r.Process, ProxyStats: r.ProxyStats, Commands: r.Commands,
		Conditions: r.conditions}
}

// setImpl makes im the default pack's hooks.
func (r *Registry) setImpl(im Impl) {
	r.Scope, r.Verifier, r.settings, r.RulesFor, r.version, r.reported = im.Scope, im.Verifier, im.Settings, im.RulesFor, im.Version, im.Reported
	r.experimentalOnly, r.ProxyMode, r.Process, r.ProxyStats, r.Commands = im.ExperimentalOnly, im.ProxyMode, im.Process, im.ProxyStats, im.Commands
	r.conditions = im.Conditions
}

// kgatewayPack is pack #1, built from pkg/kgateway.
func kgatewayPack() Pack {
	return Pack{Name: kgateway.Name, Kinds: kgateway.Kinds(), Detectors: []install.Detector{kgateway.Detector()}, Checks: kgateway.Checks(),
		Impl: Impl{Scope: kgateway.Scope(), Settings: kgateway.Settings, RulesFor: kgateway.RulesFor, Version: kgateway.Version, Reported: kgateway.Reported,
			ExperimentalOnly: kgateway.ExperimentalOnly, ProxyMode: kgateway.ProxyMode(), Process: process.Kube{}, Commands: diagnose.Kubectl{},
			Conditions: kgateway.Conditions()},
		Collectors: []func(CollectorEnv) Collector{metricsCollector, proxyStatsCollector, controllerLogsCollector}, Features: kgateway.Features()}
}

// stamp wraps ds so the installs they claim carry pack, unless a detector sets
// Impl itself. The core's detectors ("") are not wrapped.
func stamp(pack string, ds []install.Detector) []install.Detector {
	out := make([]install.Detector, 0, len(ds))
	for _, d := range ds {
		switch pd, ok := d.(install.ProxyPodDetector); {
		case pack == "":
			out = append(out, d)
		case ok:
			out = append(out, packProxyPodDetector{packDetector{d, pack}, pd})
		default:
			out = append(out, packDetector{d, pack})
		}
	}
	return out
}

// packDetector stamps its pack's name on the installs its Detector claims.
type packDetector struct {
	install.Detector
	pack string
}

// Detect implements install.Detector.
func (d packDetector) Detect(dep *appsv1.Deployment) (install.Install, bool) {
	in, ok := d.Detector.Detect(dep)
	if ok && in.Impl == "" {
		in.Impl = d.pack
	}
	return in, ok
}

// packProxyPodDetector is a packDetector that keeps its detector's proxy Pod selector.
type packProxyPodDetector struct {
	packDetector
	pp install.ProxyPodDetector
}

// ProxyPodSelector implements install.ProxyPodDetector.
func (d packProxyPodDetector) ProxyPodSelector() labels.Selector { return d.pp.ProxyPodSelector() }
