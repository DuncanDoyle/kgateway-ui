package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"

	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/snellius/pkg/api"
	"github.com/DuncanDoyle/snellius/pkg/app"
	"github.com/DuncanDoyle/snellius/pkg/cli"
	"github.com/DuncanDoyle/snellius/pkg/extension"
	"github.com/DuncanDoyle/snellius/pkg/objects"
)

//go:embed all:dist
var dist embed.FS

func main() {
	ui, err := fs.Sub(dist, "dist")
	if err != nil {
		log.Fatal(err)
	}
	// The stock binary's flags; the UI is this build's own
	kubeconfig, o, err := cli.Parse(flag.CommandLine, os.Args[1:])
	if err != nil {
		os.Exit(2)
	}
	o.UI = ui
	if err := app.Run(context.Background(), kubeconfig, o, extensions(clock.RealClock{})...); err != nil {
		log.Fatal(err)
	}
}

// options are the stock binary's defaults with ui as the UI.
func options(ui fs.FS) app.Options {
	_, o, err := cli.Parse(flag.NewFlagSet("downstream", flag.ContinueOnError), nil)
	if err != nil {
		panic(err)
	}
	o.UI = ui
	return o
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
