package extension

import (
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/diagnose"
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
// assumed in scope, no settings read, no version, no experimental-only kinds.
type Impl struct {
	Scope    install.ScopeProvider
	Verifier scopeverify.Verifier
	Settings func(install.Install) install.Settings
	RulesFor func(version string) computed.Selection
	// Version is the install's implementation version, the input of RulesFor.
	Version func(install.Install) string
	// ExperimentalOnly reports whether the controller watches served kind k only
	// with experimental Gateway API features on.
	ExperimentalOnly func(k kinds.Served, inst install.Install) bool
	ProxyMode        proxymode.Resolver
	Process          process.Source
	ProxyStats       proxystats.Source
	Commands         diagnose.Commands
}

// Pack is one implementation's contributions to the core. Its Name is stamped
// on each install its detectors claim (install.Install.Impl).
type Pack struct {
	Name      string
	Kinds     []kinds.Descriptor
	Detectors []install.Detector
	Impl      Impl
	Checks    []health.Check
}

// WithPack adds p: its kinds, its detectors (tried before those already
// registered) and its checks. p becomes the pack being built, so the hook
// options and Registry hook fields that follow set p's hooks. A pack name that
// is empty or already registered adds nothing and Registry.Err reports it.
func WithPack(p Pack) Option {
	return func(r *Registry) {
		if _, dup := r.impls[p.Name]; p.Name == "" || dup || p.Name == r.pack {
			r.fail("pack name %q is empty or already registered", p.Name)
			return
		}
		r.impls[r.pack] = r.currentImpl()
		r.pack = p.Name
		r.setImpl(p.Impl)
		r.Kinds = kinds.Dedupe(append(r.Kinds, p.Kinds...))
		r.Detectors = append(stamp(p.Name, p.Detectors), r.Detectors...)
		r.Checks = append(r.Checks, p.Checks...)
	}
}

// ImplFor returns the hooks of the pack that claimed i, read from the Registry
// fields for the pack being built. An install no registered pack claims, such
// as a class-only install, gets the core's hooks.
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

// currentImpl is the hooks of the pack being built.
func (r *Registry) currentImpl() Impl {
	return Impl{Scope: r.Scope, Verifier: r.Verifier, Settings: r.settings, RulesFor: r.RulesFor, Version: r.version,
		ExperimentalOnly: r.experimentalOnly, ProxyMode: r.ProxyMode, Process: r.Process, ProxyStats: r.ProxyStats, Commands: r.Commands}
}

// setImpl makes im the hooks of the pack being built.
func (r *Registry) setImpl(im Impl) {
	r.Scope, r.Verifier, r.settings, r.RulesFor, r.version = im.Scope, im.Verifier, im.Settings, im.RulesFor, im.Version
	r.experimentalOnly, r.ProxyMode, r.Process, r.ProxyStats, r.Commands = im.ExperimentalOnly, im.ProxyMode, im.Process, im.ProxyStats, im.Commands
}

// kgatewayPack is pack #1, built from pkg/kgateway.
func kgatewayPack() Pack {
	return Pack{Name: kgateway.Name, Kinds: kgateway.Kinds(), Detectors: []install.Detector{kgateway.Detector()}, Checks: kgateway.Checks(),
		Impl: Impl{Scope: kgateway.Scope(), Settings: kgateway.Settings, RulesFor: kgateway.RulesFor, Version: kgateway.Version,
			ExperimentalOnly: kgateway.ExperimentalOnly, ProxyMode: kgateway.ProxyMode(), Process: process.Kube{}, Commands: diagnose.Kubectl{}}}
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
