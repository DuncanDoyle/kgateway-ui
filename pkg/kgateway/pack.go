// Package kgateway holds the parts of the kgateway pack: its kinds, detector,
// checks and per-install hooks. pkg/extension assembles them into the pack
// that extension.New adds to the core; this package does not import it.
package kgateway

import (
	"github.com/DuncanDoyle/snellius/pkg/computed"
	"github.com/DuncanDoyle/snellius/pkg/computed/rulesets"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/proxymode"
)

// Name is the pack name, stamped on each install its detector claims.
const Name = "kgateway"

// Detector returns the upstream kgateway controller detector.
func Detector() install.Detector { return install.UpstreamDetector{} }

// Checks returns the kgateway checks.
func Checks() []health.Check { return health.Kgateway() }

// Scope returns the scope provider that reads kgateway's discoveryNamespaceSelectors.
func Scope() install.ScopeProvider { return install.NamespaceSelectorScope{} }

// Settings reads the controller's KGW_* settings.
func Settings(i install.Install) install.Settings { return install.ReadSettings(i) }

// RulesFor selects the computed rule set for a kgateway version.
func RulesFor(version string) computed.Selection { return rulesets.Select(version) }

// Version is the kgateway version that picks the rule set.
func Version(i install.Install) string { return i.KgatewayVersion() }

// ExperimentalOnly reports whether the install's controller watches k only
// with experimental Gateway API features on, from the table in pkg/kinds.
func ExperimentalOnly(k kinds.Served, i install.Install) bool {
	return k.ExperimentalOnly(i.KgatewayVersion())
}

// ProxyMode returns the GatewayParameters resolution of self-managed proxies.
func ProxyMode() proxymode.Resolver { return proxymode.GatewayParameters{} }
