// Command kgateway-ui serves the read-only kgateway UI.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/webui"
	"github.com/DuncanDoyle/kgateway-ui/pkg/app"
)

func main() {
	var kubeconfig string
	o := app.Options{UI: webui.FS()}
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to a kubeconfig; empty: in-cluster config, else default kubeconfig loading rules")
	flag.StringVar(&o.Pinned, "install", "", "pin the UI to one controller install (install ID or namespace/name)")
	flag.StringVar(&o.Listen, "listen", "", "listen address (default :8080 in-cluster, 127.0.0.1:8080 locally)")
	flag.DurationVar(&o.PollInterval, "poll-interval", 30*time.Second, "maximum time between model recomputes (safety timer)")
	flag.DurationVar(&o.MetricsInterval, "metrics-interval", 10*time.Second, "controller metrics scrape interval")
	flag.IntVar(&o.DelegationEdgeCap, "delegation-edge-cap", 0, "selected delegation edges computed per install; 0: the default (10000). A higher cap needs more memory")
	var events, crds, proxyStats, endpoints bool
	flag.BoolVar(&events, "diagnostics-events", true, "watch Warning Events on controller and proxy Pods")
	flag.BoolVar(&crds, "diagnostics-crds", true, "watch Gateway API CRD metadata")
	flag.BoolVar(&proxyStats, "diagnostics-proxy-stats", true, "read proxy Envoy stats")
	flag.BoolVar(&endpoints, "diagnostics-endpoints", true, "watch Services and EndpointSlices")
	flag.BoolVar(&o.Diagnostics.ControllerLogs, "diagnostics-controller-logs", false, "read controller logs for derived signals")
	flag.Parse()
	o.Diagnostics.NoEvents, o.Diagnostics.NoCRDs, o.Diagnostics.NoProxyStats, o.Diagnostics.NoEndpoints = !events, !crds, !proxyStats, !endpoints
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, kubeconfig, o); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(1)
	}
}
