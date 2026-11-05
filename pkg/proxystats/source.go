// Package proxystats reads Envoy stats from each Gateway proxy's stats
// listener (design §8.8, D-037). A Source finds the proxies and reads their
// stats; the Collector scrapes every source on the metrics interval and
// publishes what checks read under Key.
package proxystats

import (
	"context"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Source finds a Gateway's proxies, fetches their stats and interprets them.
// Kube is the default; a downstream with other proxies or another stats
// format supplies its own through extension.WithProxyStatsSource.
type Source interface {
	// Targets are gw's proxies; false means the source knows none (not observed).
	Targets(in install.Resolved, gw objects.Ref, snap *cache.Snapshot) ([]Target, bool)
	// Fetch reads one proxy's stats. The collector bounds its time through ctx and
	// rejects a body over MaxBody; error texts must not carry response content.
	Fetch(ctx context.Context, t Target) ([]byte, error)
	// Interpret reads body into raw counters.
	Interpret(t Target, body []byte) (PodStats, error)
}

// Target is one proxy to scrape.
type Target struct {
	// ID is stable for the process's life, such as a Pod UID; Name is shown.
	ID, Name string
	Gateway  objects.Ref
	// Addr is where the source fetches from, such as "podIP:9091".
	Addr string
}

// PodStats are one proxy's raw counters as Interpret reads them; nil and
// empty fields were not reported, and no field means "rejecting now".
type PodStats struct {
	UID, Name string
	Gateway   objects.Ref
	// Connected is control_plane.connected_state.
	Connected *bool
	// WarmingListeners and WarmingClusters are Envoy's warming gauges.
	WarmingListeners, WarmingClusters *int64
	LDS, CDS                          Pair
	// RDS and Clusters are update_rejected per Envoy route config and cluster name.
	RDS, Clusters map[string]int64
	// RDSTruncated and ClustersTruncated are set when the source dropped names.
	RDSTruncated, ClustersTruncated bool
	// ServerState is "live", "draining", "pre-initializing" or "initializing".
	ServerState string
	// Uptime is server.uptime in seconds; zero when not reported.
	Uptime int64
	Err    string
}

// Pair is a type's update_rejected and update_success counters.
type Pair struct{ Rejected, Success int64 }
