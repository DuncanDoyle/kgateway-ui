package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/app"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/extension"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxymode"
)

//go:embed all:dist
var dist embed.FS

// acmeProxyMode reads the self-managed flag from acme's own parameters kind.
type acmeProxyMode struct{}

func (acmeProxyMode) Resolve(gw *unstructured.Unstructured, snap *cache.Snapshot, served []kinds.Served) proxymode.Mode {
	return proxymode.Managed
}

func main() {
	ui, err := fs.Sub(dist, "dist")
	if err != nil {
		log.Fatal(err)
	}
	if err := app.Run(context.Background(), "", options(ui), extensions()...); err != nil {
		log.Fatal(err)
	}
}

// options are the backend settings. Both intervals must be positive; these
// are the stock binary's defaults.
func options(ui fs.FS) app.Options {
	return app.Options{UI: ui, PollInterval: 30 * time.Second, MetricsInterval: 10 * time.Second}
}

// extensions are what this build adds to the stock UI.
func extensions() []extension.Option {
	return []extension.Option{
		// Watch, count and serve an extra policy kind
		extension.WithKinds(kinds.Descriptor{
			Group: "acme.example.com", Kind: "AcmePolicy", Resource: "acmepolicies",
			Versions: []string{"v1"}, Namespaced: true, Category: kinds.CategoryPolicy, DisplayName: "AcmePolicy",
		}),
		// Decide per request what the caller may see
		extension.WithAuthorizer(api.AuthorizerFunc(func(r *http.Request, ref objects.Ref) bool {
			return ref.Namespace != "acme-system"
		})),
		// Replace the upstream GatewayParameters resolution
		extension.WithProxyModeResolver(acmeProxyMode{}),
	}
}
