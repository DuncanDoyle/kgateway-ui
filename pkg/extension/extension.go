// Package extension is the public seam through which downstream distributions
// extend the UI backend without forking it (D-003). A downstream main calls
// app.Run with options from this package.
//
// # Compatibility
//
// API v1 (the /api/v1 paths and their JSON) and the SPA URLs are stable from
// the first release. This Go API (this package and the types it carries)
// changes only with a README upgrade note and a release-note entry. The 3.1
// check-contract change is the last planned break; later additions are
// optional interfaces, detected by type assertion. When the provider seam
// arrives, a Kubernetes Detector keeps working as that provider's adapter (D-016).
package extension

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/DuncanDoyle/snellius/pkg/api"
	"github.com/DuncanDoyle/snellius/pkg/collect"
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/diagnose"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/process"
	"github.com/DuncanDoyle/snellius/pkg/proxymode"
	"github.com/DuncanDoyle/snellius/pkg/proxystats"
	"github.com/DuncanDoyle/snellius/pkg/scopeverify"
)

// Registry holds everything the backend is built from. Its hook fields (Scope,
// Verifier, ProxyMode, RulesFor, Process, ProxyStats, Commands) are the hooks
// of the default pack: kgateway in New, the core in NewCore (see WithPack).
type Registry struct {
	Kinds     []kinds.Descriptor
	Detectors []install.Detector
	Scope     install.ScopeProvider
	// Verifier checks scope against the controller. Nil means the upstream
	// verifier, which the app sizes from the metrics interval.
	Verifier scopeverify.Verifier
	Checks   []health.Check
	// Authorizer decides per request what the caller may see (D-013).
	Authorizer api.Authorizer
	// ProxyMode decides whether a Gateway's proxy is self-managed. It mirrors
	// controller logic, so a downstream with its own parameters kind replaces it.
	ProxyMode proxymode.Resolver
	// RulesFor selects the computed rule set for a controller's kgateway
	// version, "" when unknown (D-020).
	RulesFor func(controllerVersion string) computed.Selection
	// Process reports each install's controller and proxy processes (D-016).
	Process process.Source
	// ProxyStats finds proxies and reads their stats (design §8.8); nil means
	// proxystats.Kube over the app's clients.
	ProxyStats proxystats.Source
	// Commands fills in the next-step commands (design §8.5).
	Commands diagnose.Commands

	// pack names the default pack; "" is the core. settings, version, reported
	// and experimentalOnly are its hooks that have no exported field.
	pack             string
	settings         func(install.Install) install.Settings
	version          func(install.Install) string
	reported         func(install.Install, collect.Snapshot) (string, bool)
	experimentalOnly func(kinds.Served, install.Install) bool
	// impls are the hooks of the other packs, and of the core once a default pack is set, by name.
	impls map[string]Impl

	// handlers are the WithHandlers registrations, in order.
	handlers []extHandlers
	// collectors are the WithCollectors registrations, in order.
	collectors []Collector
	// packCollectors are the packs' collector factories, in order.
	packCollectors []packCollector
	// capSources are the WithCapabilities registrations, in order.
	capSources []func() []api.Capability
	// prefixes holds the finding-code prefixes registered so far.
	prefixes map[string]bool
	// errs collects registration errors; New does not fail, Err reports them.
	errs []error
}

// UpstreamPrefix is the finding-code prefix reserved for upstream checks.
const UpstreamPrefix = "KGWUI-"

var prefixPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,15}-$`)

// Err returns the registration errors joined, or nil. app.Build returns it
// before it starts anything.
func (r *Registry) Err() error { return errors.Join(r.errs...) }

// fail records a registration error.
func (r *Registry) fail(format string, args ...any) {
	r.errs = append(r.errs, fmt.Errorf(format, args...))
}

// Option modifies a Registry.
type Option func(*Registry)

// WithKinds adds kinds to watch, count and serve. Kinds are unique by group
// and kind: a descriptor for a registered kind replaces it in place.
func WithKinds(d ...kinds.Descriptor) Option {
	return func(r *Registry) { r.Kinds = kinds.Dedupe(append(r.Kinds, d...)) }
}

// WithDetectors adds install detectors for the default pack. They are tried
// before the detectors already registered, so a downstream can claim
// Deployments the upstream one would also match.
func WithDetectors(d ...install.Detector) Option {
	return func(r *Registry) { r.Detectors = append(stamp(r.pack, d), r.Detectors...) }
}

// WithScopeProvider replaces the scope provider.
func WithScopeProvider(p install.ScopeProvider) Option { return func(r *Registry) { r.Scope = p } }

// WithScopeVerifier replaces the scope verifier.
func WithScopeVerifier(v scopeverify.Verifier) Option { return func(r *Registry) { r.Verifier = v } }

// WithChecks adds health checks; by convention their finding codes start with
// prefix, such as "ACME-". A reserved or repeated prefix adds nothing and Registry.Err reports it.
func WithChecks(prefix string, c ...health.Check) Option {
	return func(r *Registry) {
		switch {
		case !prefixPattern.MatchString(prefix):
			r.fail("finding-code prefix %q must match %s", prefix, prefixPattern)
		case r.prefixes[prefix]:
			r.fail("finding-code prefix %q is already registered", prefix)
		default:
			r.prefixes[prefix] = true
			r.Checks = append(r.Checks, c...)
		}
	}
}

// WithCapabilities adds a source of capability states for /api/v1/meta. A
// capability that is on is also listed in capabilities. Names must not
// collide with core, platform or other extension capabilities; app.Build calls
// each source once to check, so a source must report a stable set of names.
// Sources run on every /api/v1/meta request: keep them fast and safe for concurrent use.
func WithCapabilities(f func() []api.Capability) Option {
	return func(r *Registry) { r.capSources = append(r.capSources, f) }
}

// CapabilityStates returns what every source reports now, in registration order.
func (r *Registry) CapabilityStates() []api.Capability {
	var all []api.Capability
	for _, f := range r.capSources {
		all = append(all, f()...)
	}
	return all
}

// CheckCapabilities reports a capability name that collides with a core one or
// the platform's platformNames, or another source.
func (r *Registry) CheckCapabilities(platformNames []string) error {
	return api.CheckCapabilities(platformNames, r.capSources...)
}

// WithAuthorizer replaces the default api.AllowAll. Every API response with
// resources, findings or counts goes through it (D-013).
func WithAuthorizer(a api.Authorizer) Option { return func(r *Registry) { r.Authorizer = a } }

// WithProxyModeResolver replaces the upstream GatewayParameters resolution.
func WithProxyModeResolver(p proxymode.Resolver) Option { return func(r *Registry) { r.ProxyMode = p } }

// WithComputedRules sets the rule-set choice of the default pack (in New,
// the kgateway pack's rulesets.Select). A downstream distribution maps its
// controller to a rule set; a build that is not the kgateway edition receives
// its BaseVersion, "" when unknown (D-018 §3).
func WithComputedRules(f func(controllerVersion string) computed.Selection) Option {
	return func(r *Registry) { r.RulesFor = f }
}

// WithProcessSource replaces process.Kube, for example to add extension Pods
// or to report processes that do not run in Pods.
func WithProcessSource(s process.Source) Option { return func(r *Registry) { r.Process = s } }

// WithProxyStatsSource replaces proxystats.Kube, for example for proxies that
// do not run in Pods or another stats format. The core collector keeps the
// interval, bounds, concurrency and publishing.
func WithProxyStatsSource(s proxystats.Source) Option { return func(r *Registry) { r.ProxyStats = s } }

// WithCommands replaces diagnose.Kubectl, for a platform without kubectl or
// with its own commands. A method that returns "" hides that step.
func WithCommands(c diagnose.Commands) Option { return func(r *Registry) { r.Commands = c } }

// New returns the core plus the kgateway pack, with opts applied. The kinds
// and checks keep the order of kinds.Upstream and health.Upstream.
func New(opts ...Option) *Registry {
	return NewCore(append([]Option{WithPack(kgatewayPack()), upstreamOrder}, opts...)...)
}

// upstreamOrder puts the kinds and checks in the base order, which API v1 kind
// lists keep; the set is the same core-then-pack set.
func upstreamOrder(r *Registry) { r.Kinds, r.Checks = kinds.Upstream(), health.Upstream() }

// NewCore returns the core alone, with opts applied: Gateway API kinds, no
// detectors, the core checks and no pack hooks.
func NewCore(opts ...Option) *Registry {
	r := &Registry{
		Kinds:      kinds.GatewayAPI(),
		Checks:     health.Core(),
		Authorizer: api.AllowAll{},
		Commands:   diagnose.Kubectl{},
		impls:      map[string]Impl{},
		prefixes:   map[string]bool{UpstreamPrefix: true},
	}
	for _, o := range opts {
		o(r)
	}
	return r
}
