package kinds

import (
	"regexp"
	"slices"
)

const (
	gwGroup  = "gateway.networking.k8s.io"
	gwxGroup = "gateway.networking.x-k8s.io"
	kgwGroup = "gateway.kgateway.dev"
)

// Upstream returns the kinds of Gateway API and kgateway that the UI understands.
// Secrets and ConfigMaps are deliberately absent (design §11).
func Upstream() []Descriptor {
	return []Descriptor{
		{Group: gwGroup, Kind: "GatewayClass", Resource: "gatewayclasses", Versions: []string{"v1"}, Category: CategoryGatewayClass, DisplayName: "GatewayClasses"},
		{Group: gwGroup, Kind: "Gateway", Resource: "gateways", Versions: []string{"v1"}, Namespaced: true, Category: CategoryGateway, DisplayName: "Gateways"},
		{Group: gwGroup, Kind: "ListenerSet", Resource: "listenersets", Versions: []string{"v1"}, Namespaced: true, Category: CategoryListenerSet, DisplayName: "ListenerSets"},
		{Group: gwxGroup, Kind: "XListenerSet", Resource: "xlistenersets", Versions: []string{"v1alpha1"}, Namespaced: true, Category: CategoryListenerSet, DisplayName: "ListenerSets (experimental)"},
		{Group: gwGroup, Kind: "HTTPRoute", Resource: "httproutes", Versions: []string{"v1"}, Namespaced: true, Category: CategoryRoute, DisplayName: "HTTPRoutes"},
		{Group: gwGroup, Kind: "GRPCRoute", Resource: "grpcroutes", Versions: []string{"v1"}, Namespaced: true, Category: CategoryRoute, DisplayName: "GRPCRoutes"},
		{Group: gwGroup, Kind: "TCPRoute", Resource: "tcproutes", Versions: []string{"v1", "v1alpha2"}, Namespaced: true, Category: CategoryRoute, DisplayName: "TCPRoutes"},
		{Group: gwGroup, Kind: "TLSRoute", Resource: "tlsroutes", Versions: []string{"v1", "v1alpha3", "v1alpha2"}, Namespaced: true, Category: CategoryRoute, DisplayName: "TLSRoutes"},
		{Group: gwGroup, Kind: "BackendTLSPolicy", Resource: "backendtlspolicies", Versions: []string{"v1", "v1alpha3"}, Namespaced: true, Category: CategoryPolicy, DisplayName: "BackendTLSPolicies"},
		{Group: kgwGroup, Kind: "TrafficPolicy", Resource: "trafficpolicies", Versions: []string{"v1alpha1"}, Namespaced: true, Category: CategoryPolicy, DisplayName: "TrafficPolicies"},
		{Group: kgwGroup, Kind: "ListenerPolicy", Resource: "listenerpolicies", Versions: []string{"v1alpha1"}, Namespaced: true, Category: CategoryPolicy, DisplayName: "ListenerPolicies"},
		{Group: kgwGroup, Kind: "HTTPListenerPolicy", Resource: "httplistenerpolicies", Versions: []string{"v1alpha1"}, Namespaced: true, Category: CategoryPolicy, DisplayName: "HTTPListenerPolicies"},
		{Group: kgwGroup, Kind: "BackendConfigPolicy", Resource: "backendconfigpolicies", Versions: []string{"v1alpha1"}, Namespaced: true, Category: CategoryPolicy, DisplayName: "BackendConfigPolicies"},
		{Group: kgwGroup, Kind: "GatewayParameters", Resource: "gatewayparameters", Versions: []string{"v1alpha1"}, Namespaced: true, Category: CategoryParameters, DisplayName: "GatewayParameters"},
	}
}

// KgatewayGroup is the API group of kgateway's own CRDs.
const KgatewayGroup = kgwGroup

// experimentalOnly lists the kinds kgateway watches only with experimental
// Gateway API features on (pluginsdk/collections/setup.go), with the versions
// each supported controller minor watches regardless. Only the controller
// knows the flag (D-017 §4); the classification depends on its version (D-018 §3).
var experimentalOnly = []struct {
	group, kind string
	stable      map[string][]string // controller minor → versions watched regardless
}{
	{gwxGroup, "XListenerSet", nil},
	{gwGroup, "TCPRoute", map[string][]string{"2.3": nil, "2.4": {"v1"}}}, // v2.3 gates every TCPRoute
	{gwGroup, "TLSRoute", map[string][]string{"2.3": {"v1"}, "2.4": {"v1"}}},
}

// supportedMinors are the kgateway minors experimentalOnly describes.
var supportedMinors = []string{"2.3", "2.4"}

var minorRE = regexp.MustCompile(`^v?(\d+)\.(\d+)(?:[.+-]|$)`)

// controllerMinor returns the supported minor of controller version v, or ""
// when v is empty, unparsable or not a supported minor.
func controllerMinor(v string) string {
	m := minorRE.FindStringSubmatch(v)
	if m == nil {
		return ""
	}
	minor := m[1] + "." + m[2]
	if !slices.Contains(supportedMinors, minor) {
		return ""
	}
	return minor
}

// ExperimentalOnly reports whether a kgateway controller at version controller
// watches group/kind at version only with experimental Gateway API features
// on. An unknown controller version counts a kind as experimental-only when
// any supported minor gates it (D-018 §3).
func ExperimentalOnly(group, kind, version, controller string) bool {
	minor := controllerMinor(controller)
	for _, e := range experimentalOnly {
		if e.group != group || e.kind != kind {
			continue
		}
		if minor != "" {
			return !slices.Contains(e.stable[minor], version)
		}
		for _, m := range supportedMinors {
			if !slices.Contains(e.stable[m], version) {
				return true
			}
		}
		return false
	}
	return false
}

// ExperimentalOnly reports whether a kgateway controller at version controller
// watches s, at its served version, only with experimental Gateway API features on.
func (s Served) ExperimentalOnly(controller string) bool {
	return ExperimentalOnly(s.Group, s.Kind, s.Version, controller)
}
