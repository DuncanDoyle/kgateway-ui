# kgateway UI

A read-only web UI for kgateway. It shows the health of each controller install at a glance, why an install is unhealthy, and an inventory of what it manages: GatewayClasses, Gateways, routes and policies.

Kubernetes is the only provider today. The API and the SPA are built so other platforms can be added later.

## Security and trust model

Read this before you expose the UI.

- **No login in this release.** Whoever can reach the UI sees everything it shows. That is everything its ServiceAccount can read across the cluster, Deployments included. It can be more than the viewer's own Kubernetes RBAC allows, as in Argo CD.
- **No Secrets.** The UI never reads Secrets, and its ClusterRole excludes them. No other field is redacted, because a secret can sit in any field, such as an inline token or a literal env var.
- **Port-forward by default.** The chart creates a ClusterIP Service only. The optional `httpRoute` (off by default) is for operators who put authentication and authorisation on that route, for example a kgateway TrafficPolicy with OIDC or external auth.
- **Per-user authorisation plugs into a seam.** Downstream builds and future releases add it through `api.Authorizer` (`extension.WithAuthorizer`). A finding on a resource the viewer may not see is shown as restricted, and the health state still counts it. Per-user authentication and authorisation is on the [roadmap](docs/roadmap.md).

## Install

```sh
# Install the chart from ghcr (pick a released version)
helm install ui oci://ghcr.io/duncandoyle/charts/kgateway-ui --version 0.1.0 \
  -n kgateway-ui --create-namespace

# Open the UI on http://localhost:8080
kubectl -n kgateway-ui port-forward svc/ui-kgateway-ui 8080:80
```

The image is `ghcr.io/duncandoyle/kgateway-ui`. Installing from a checkout needs `--set image.tag=<version>`; see the [chart README](deploy/helm/kgateway-ui/README.md#install). To pin the UI to one install, set `install.pin`. To expose it through a Gateway, read [Security and trust model](#security-and-trust-model) first, then set `httpRoute.enabled`, `httpRoute.parentRefs` and `httpRoute.hostnames`. The [chart README](deploy/helm/kgateway-ui/README.md) lists every value.

## Run locally

```sh
# Build the SPA and the binary with the SPA embedded
make build

# Serve on http://127.0.0.1:8080 against your current cluster
./bin/kgateway-ui --kubeconfig ~/.kube/config
```

Flags:

- `--kubeconfig`: path to a kubeconfig; empty uses in-cluster config, else the default loading rules.
- `--install`: pin the UI to one install (install ID or `namespace/name`).
- `--listen`: listen address (default `:8080` in-cluster, `127.0.0.1:8080` locally).
- `--poll-interval`: model recompute interval (default 2s).
- `--metrics-interval`: controller metrics scrape interval (default 10s).

`make test`, `make lint` and `make e2e` run the Go tests, the linters and the kind end-to-end suite. `make help` lists all targets.

## What it reads

- **Read-only RBAC.** The ClusterRole grants only `get`, `list` and `watch`: Gateway API kinds, all `gateway.kgateway.dev` kinds, CRDs, namespaces, pods and Deployments.
- **No Secrets, no ConfigMaps, no `*/proxy`.**
- **Controller metrics on port 9092.** In-cluster, the UI scrapes the controller pods directly. If a NetworkPolicy blocks that, the UI reports the signal as unavailable instead of green. Locally, it goes through the pods/proxy API with your kubeconfig's permissions.

## Scope

The UI honours the controller's `discoveryNamespaceSelectors`. It only counts resources in namespaces the controller watches, and the install page shows the scope as "N of M namespaces".

## Extending

Downstream builds extend the UI without forking it. The backend takes options from `pkg/extension`:

```go
package main

import (
	"context"
	"embed"
	"io/fs"
	"net/http"

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
	ui, _ := fs.Sub(dist, "dist")
	_ = app.Run(context.Background(), "", app.Options{UI: ui},
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
	)
}
```

The other options are `WithDetectors`, `WithChecks`, `WithScopeProvider` and `WithScopeVerifier`. Kinds added with `WithKinds` need matching rules in the chart's ClusterRole.

The frontend renders `Shell` from `@kgateway-ui/shell` with a `ShellExtension`:

```tsx
import { defaultPages, Shell, type ShellExtension } from "@kgateway-ui/shell";
import { createRoot } from "react-dom/client";

const acme: ShellExtension = {
  // Product identity in the header
  brand: { productName: "acme", logo: null },
  // Render extra detail for findings from acme's own checks
  findingRenderers: { "ACME-": (f) => <span>{f.message}</span> },
  // Platform copy; entries left out keep the Kubernetes defaults
  vocabulary: { landingTitle: "acme installs" },
};

createRoot(document.getElementById("root") as HTMLElement).render(<Shell extension={acme} pages={defaultPages} />);
```

A `ShellExtension` can also add `themes`, `defaultThemeId`, `routes` and `navEntries`.

## Compatibility

- **API v1** (`/api/v1/...` paths and JSON) and the **SPA URLs** (`/i/{id}/...`) are stable from the first release. Install IDs are opaque: clients must not split or parse them.
- **The Go extension API** (`pkg/extension`, and the `install.Detector`, `install.Install` and `install.ScopeProvider` types it carries) may change until the extension-seam milestone after Phase 2. When the provider seam arrives, a Kubernetes `Detector` keeps working as that provider's adapter.

## Supported versions

kgateway v2.3.11 and v2.4.7, tested on kind in CI. On these versions a policy whose target is missing gets no status, so the UI reports it as "not yet processed". Only kgateway `main` sets `TargetNotFound`, which the UI shows as "Policy target not found".

## Contributing

The design, implementation plans, decisions and roadmap live in [`docs/`](docs/). Start with [`docs/roadmap.md`](docs/roadmap.md) and [`docs/phase-status.md`](docs/phase-status.md).

CI's `hygiene` job reads its term list from the repository secret `FORBIDDEN_TERMS`. Same-repo PRs and pushes fail without it; fork PRs, which get no secrets, skip the check with a warning.

### Releasing

Push a semver tag (`vX.Y.Z`). The release workflow publishes the image `ghcr.io/duncandoyle/kgateway-ui:X.Y.Z` and the chart `oci://ghcr.io/duncandoyle/charts/kgateway-ui` version `X.Y.Z`, then creates the GitHub release.

ghcr creates both packages as private on their first push. After the first tag, make them public once: open each package's settings (`kgateway-ui` and `charts/kgateway-ui`) and set its visibility to public. Then check that an anonymous pull works:

```sh
# Run logged out of ghcr.io
docker logout ghcr.io; helm registry logout ghcr.io
docker pull ghcr.io/duncandoyle/kgateway-ui:X.Y.Z
helm show chart oci://ghcr.io/duncandoyle/charts/kgateway-ui --version X.Y.Z
```

## Licence

Apache-2.0.
