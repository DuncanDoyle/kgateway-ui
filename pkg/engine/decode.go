package engine

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// decodeCache is an objects.Decoder that reuses a decoded view while an
// object's uid and resourceVersion are unchanged. It is not safe for
// concurrent use: the engine touches it only under computeMu. Views are
// shared between calls and must be treated as read-only.
type decodeCache struct {
	gateways      viewCache[objects.GatewayView]
	listenerSets  viewCache[objects.ListenerSetView]
	httpRoutes    viewCache[objects.HTTPRouteView]
	backends      viewCache[objects.BackendView]
	routeHostname viewCache[objects.RouteHostnames]
}

var _ objects.Decoder = (*decodeCache)(nil)

func newDecodeCache() *decodeCache {
	return &decodeCache{
		gateways:      viewCache[objects.GatewayView]{},
		listenerSets:  viewCache[objects.ListenerSetView]{},
		httpRoutes:    viewCache[objects.HTTPRouteView]{},
		backends:      viewCache[objects.BackendView]{},
		routeHostname: viewCache[objects.RouteHostnames]{},
	}
}

// Gateway returns the cached GatewayView for u, decoding it on a miss.
func (d *decodeCache) Gateway(u *unstructured.Unstructured) *objects.GatewayView {
	return d.gateways.get(u, objects.DecodeGateway)
}

// ListenerSet returns the cached ListenerSetView for u, decoding it on a miss.
func (d *decodeCache) ListenerSet(u *unstructured.Unstructured) *objects.ListenerSetView {
	return d.listenerSets.get(u, objects.DecodeListenerSet)
}

// HTTPRoute returns the cached HTTPRouteView for u, decoding it on a miss.
func (d *decodeCache) HTTPRoute(u *unstructured.Unstructured) *objects.HTTPRouteView {
	return d.httpRoutes.get(u, objects.DecodeHTTPRoute)
}

// Backend returns the cached BackendView for u, decoding it on a miss.
func (d *decodeCache) Backend(u *unstructured.Unstructured) *objects.BackendView {
	return d.backends.get(u, objects.DecodeBackend)
}

// RouteHostnames returns the cached RouteHostnames for u, decoding it on a miss.
func (d *decodeCache) RouteHostnames(u *unstructured.Unstructured) *objects.RouteHostnames {
	return d.routeHostname.get(u, objects.DecodeRouteHostnames)
}

// prune drops every entry whose uid is absent from live or whose
// resourceVersion differs from the live one.
func (d *decodeCache) prune(live map[types.UID]string) {
	d.gateways.prune(live)
	d.listenerSets.prune(live)
	d.httpRoutes.prune(live)
	d.backends.prune(live)
	d.routeHostname.prune(live)
}

// size reports the number of cached views (tests).
func (d *decodeCache) size() int {
	return len(d.gateways) + len(d.listenerSets) + len(d.httpRoutes) + len(d.backends) + len(d.routeHostname)
}

type cachedView[T any] struct {
	rv string
	v  *T
}

// viewCache holds one view type, one entry per uid.
type viewCache[T any] map[types.UID]cachedView[T]

// get returns the view for u. An object without uid or resourceVersion
// bypasses the cache.
func (c viewCache[T]) get(u *unstructured.Unstructured, decode func(*unstructured.Unstructured) *T) *T {
	uid, rv := u.GetUID(), u.GetResourceVersion()
	if uid == "" || rv == "" {
		return decode(u)
	}
	if e, ok := c[uid]; ok && e.rv == rv {
		return e.v
	}
	v := decode(u)
	c[uid] = cachedView[T]{rv: rv, v: v}
	return v
}

func (c viewCache[T]) prune(live map[types.UID]string) {
	for uid, e := range c {
		if rv, ok := live[uid]; !ok || rv != e.rv {
			delete(c, uid)
		}
	}
}
