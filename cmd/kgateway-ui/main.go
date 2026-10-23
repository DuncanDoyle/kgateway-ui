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
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, kubeconfig, o); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(1)
	}
}
