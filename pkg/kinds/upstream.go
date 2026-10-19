package kinds

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
	}
}

// KgatewayGroup is the API group of kgateway's own CRDs.
const KgatewayGroup = kgwGroup
