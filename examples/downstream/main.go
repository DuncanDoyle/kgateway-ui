package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"time"

	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/app"
	"github.com/DuncanDoyle/kgateway-ui/pkg/extension"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

//go:embed all:dist
var dist embed.FS

func main() {
	ui, err := fs.Sub(dist, "dist")
	if err != nil {
		log.Fatal(err)
	}
	if err := app.Run(context.Background(), "", options(ui), extensions(clock.RealClock{})...); err != nil {
		log.Fatal(err)
	}
}

// options are the backend settings. Both intervals must be positive; these
// are the stock binary's defaults.
func options(ui fs.FS) app.Options {
	return app.Options{UI: ui, PollInterval: 30 * time.Second, MetricsInterval: 10 * time.Second}
}

// extensions are what this build adds to the stock UI. The types are in
// acme.go; clk drives the collector, so tests can step it.
func extensions(clk clock.WithTicker) []extension.Option {
	return []extension.Option{
		// Watch, count and serve an extra policy kind
		extension.WithKinds(acmePolicy),
		// Decide per request what the caller may see
		extension.WithAuthorizer(api.AuthorizerFunc(func(r *http.Request, ref objects.Ref) bool {
			return ref.Namespace != "acme-system"
		})),
		// Replace the upstream GatewayParameters resolution
		extension.WithProxyModeResolver(acmeProxyMode{}),
		// Publish a value between recomputes, for checks to read
		extension.WithCollectors(&acmeWidgets{clock: clk}),
		// Add checks; their finding codes start with ACME-
		extension.WithChecks("ACME-", acmeWidgetCheck{}),
		// Serve endpoints under /api/ext/acme/
		extension.WithHandlers("acme", acmeHandlers),
		// Report a feature's state in /api/v1/meta
		extension.WithCapabilities(acmeCapabilities),
	}
}
