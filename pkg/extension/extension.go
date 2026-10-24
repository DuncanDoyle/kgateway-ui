// Package extension is the public seam through which downstream distributions
// extend the UI backend without forking it (D-003). A downstream main calls
// app.Run with options from this package.
//
// # Compatibility
//
// API v1 (the /api/v1 paths and their JSON) and the SPA URLs are stable from
// the first release. This Go API (Registry and its options, and the
// install.Detector, install.Install and install.ScopeProvider types it
// carries) may change until the extension-seam milestone after Phase 2. When
// the provider seam arrives, a Kubernetes Detector keeps working as that
// provider's adapter (D-016).
package extension

import (
	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed/rulesets"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxymode"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

// Registry holds everything the backend is built from.
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
}

// Option modifies a Registry.
type Option func(*Registry)

// WithKinds adds kinds to watch, count and serve. Kinds are unique by group
// and kind: a descriptor for a registered kind replaces it in place.
func WithKinds(d ...kinds.Descriptor) Option {
	return func(r *Registry) { r.Kinds = kinds.Dedupe(append(r.Kinds, d...)) }
}

// WithDetectors adds install detectors. They are tried before the upstream
// detector, so a downstream can claim Deployments the upstream one would also match.
func WithDetectors(d ...install.Detector) Option {
	return func(r *Registry) { r.Detectors = append(append([]install.Detector{}, d...), r.Detectors...) }
}

// WithScopeProvider replaces the scope provider.
func WithScopeProvider(p install.ScopeProvider) Option { return func(r *Registry) { r.Scope = p } }

// WithScopeVerifier replaces the scope verifier.
func WithScopeVerifier(v scopeverify.Verifier) Option { return func(r *Registry) { r.Verifier = v } }

// WithChecks adds health checks. Finding codes with the KGWUI- prefix are
// reserved for upstream checks; a downstream uses its own prefix (ACME-001).
// Severities outside error, warning and info are reported as warnings.
func WithChecks(c ...health.Check) Option {
	return func(r *Registry) { r.Checks = append(r.Checks, c...) }
}

// WithAuthorizer replaces the default api.AllowAll. Every API response with
// resources, findings or counts goes through it (D-013).
func WithAuthorizer(a api.Authorizer) Option { return func(r *Registry) { r.Authorizer = a } }

// WithProxyModeResolver replaces the upstream GatewayParameters resolution.
func WithProxyModeResolver(p proxymode.Resolver) Option { return func(r *Registry) { r.ProxyMode = p } }

// WithComputedRules replaces rulesets.Select. A downstream distribution maps
// its controller to a rule set; it receives "" as the version (D-018 §3).
func WithComputedRules(f func(controllerVersion string) computed.Selection) Option {
	return func(r *Registry) { r.RulesFor = f }
}

// New returns the upstream registry with opts applied.
func New(opts ...Option) *Registry {
	r := &Registry{
		Kinds:      kinds.Upstream(),
		Detectors:  []install.Detector{install.UpstreamDetector{}},
		Scope:      install.NamespaceSelectorScope{},
		Checks:     health.Upstream(),
		Authorizer: api.AllowAll{},
		ProxyMode:  proxymode.GatewayParameters{},
		RulesFor:   rulesets.Select,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}
