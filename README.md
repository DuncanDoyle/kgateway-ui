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
- `--poll-interval`: maximum time between model recomputes, the safety timer (default 30s). The model recomputes on object, access, metrics and served-kind changes; `2s` restores the earlier fixed cadence as a floor.
- `--metrics-interval`: controller metrics scrape interval (default 10s).
- `--delegation-edge-cap`: selected delegation edges computed per install (default 10000; 0 keeps the default). Past the cap, parents read "too large to compute". A higher cap needs a higher memory limit.

`make test`, `make lint` and `make e2e` run the Go tests, the linters and the kind end-to-end suite. `make help` lists all targets.

## What it reads

- **Read-only RBAC.** The ClusterRole grants only `get`, `list` and `watch`: Gateway API kinds, all `gateway.kgateway.dev` kinds, CRDs, namespaces, pods and Deployments.
- **No Secrets, no ConfigMaps, no `*/proxy`.**
- **Controller metrics on port 9092.** In-cluster, the UI scrapes the controller pods directly. If a NetworkPolicy blocks that, the UI reports the signal as unavailable instead of green. Locally, it goes through the pods/proxy API with your kubeconfig's permissions.

## Scope

The UI honours the controller's `discoveryNamespaceSelectors`. It only counts resources in namespaces the controller watches, and the install page shows the scope as "N of M namespaces".

## Extending

Downstream builds extend the UI without forking it. The backend takes options from `pkg/extension`. This is [examples/downstream/main.go](examples/downstream/main.go), which the test suite builds:

```go
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
```

The other options are `WithDetectors`, `WithChecks`, `WithScopeProvider`, `WithScopeVerifier` and `WithComputedRules`. Kinds added with `WithKinds` need matching rules in the chart's ClusterRole.

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

## Upgrade notes

### Computed route delegation (sub-phase 2.4)

The UI now computes route delegation, using kgateway 2.3's or 2.4's own rules, and cross-checks the result with the controller's status:

- A parent HTTPRoute has a Delegation tab. Per delegation ref, it lists the routes the ref selects and whether each is attached, dropped or not permitted, and why. Each row opens its paths from a Gateway or ListenerSet.
- A child route shows its effective matches: what it really serves under each parent, after the parent's match is applied.
- The rules list shows selected and attached counts per ref. The routes list flags a child that is selected but not attached.
- KGWUI-011 also fires when the UI computes that a parent drops or does not permit a child, and for a route with no parentRefs that no parent keeps. These are warnings.
- KGWUI-012 now covers delegation: a child's status entry or a parent's `ResolvedRefs` that disagrees with the computation for over 30 seconds.
- When the install's discovery scope is unknown, delegation rows and effective matches read "Unknown" too.
- A child you can see shows its hidden ancestor routes by name and delegated match only.
- Delegation work is capped at 10,000 selected edges per install. Past the cap, parents read "too large to compute". Raise it with `--delegation-edge-cap` or the chart's `delegation.edgeCap`; a higher cap needs more memory.
- The binary sets its Go memory limit to 90 % of the pod's memory limit, unless `GOMEMLIMIT` is set. The chart passes the limit in `KGWUI_MEMORY_LIMIT`.

For downstream builds:

- `app.Run` applies the memory limit, so a downstream `main` gets it too.

### Computed listener attachment (sub-phase 2.3)

The UI now computes which routes attach to each listener, using kgateway 2.3's or 2.4's own attachment rules, and cross-checks the result with the controller's status:

- Listener tables and parent cards show a computed route count and each route's effective hostnames, labelled "Computed" and marked whether the controller agrees. The controller's status is always what counts.
- The Gateways list's routes column and sort now use the computed count of the Gateway's own listeners; routes on its ListenerSets are not included.
- The routes list takes `?listener=<name>` with a Gateway or ListenerSet `parent`.
- KGWUI-012 is a new warning: the UI's computation has disagreed with the controller for over 30 seconds. It never turns an install red.
- When the install's discovery scope is unknown, computed counts and outcomes read "Unknown" instead of guessing.
- With per-user authorisation, a computed fact about a route reads "Hidden" when it depends on an object the viewer cannot see. Its value is not shown. Listener counts always show. Like the controller's `attachedRoutes`, they include routes the viewer cannot see.

For downstream builds:

- `extension.WithComputedRules` registers the rule set used for a controller version.
- The chart gains `extraArgs`, extra flags appended to the UI binary's arguments.

### Gateway & Route Explorer (sub-phase 2.2)

Counts and colours change on clusters that use route delegation:

- A delegated child route with a controller status entry for its parent HTTPRoute now counts in the HTTPRoute tile and in install health. Its failures can turn the install yellow or red.
- A route that names an HTTPRoute parent but has no status entry for it gets KGWUI-011: info while the controller may still be processing it, a warning after that. On kgateway 2.3, a child ref that omits the namespace or sets `sectionName` gets no status entry, so it always gets this warning.
- After a child edit drops its parent, the controller clears the child's old status entry within a second. For that moment the child may still count and show KGWUI-003.
- The KGWUI-004 detail names the target's kind, so a route targeting a ListenerSet reads "targets ListenerSet `<ns/name>`".

For downstream builds:

- `CorePages` gains optional `GatewaysPage`, `GatewayPage`, `ListenerSetsPage`, `ListenerSetPage`, `RoutesPage` and `RoutePage` slots. A build that leaves them out gets the upstream pages.
- The install paths `gateways`, `listenersets` and `routes`, and everything below them, are reserved. A `routes` contribution at one of them is dropped, with a console warning.

Pages now retry a failed request every 5 seconds, so they recover within one poll after an API outage. A 400 is not retried.

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
