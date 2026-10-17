# Phase 1 — Foundation & Overview: Design

**Status:** Draft, for review
**Date:** 2026-10-17
**Base:** `560c393`
**Roadmap:** `docs/roadmap.md`, Phase 1
**Decisions:** D-001 … D-011 in `docs/decisions.md`

## 1. Goal and scope

A minimal UI that is deployable and operational and already provides value. A platform operator opens it and sees, at a glance, whether their kgateway installation is healthy. If it is not, they see why. Then they see an inventory of what the install manages.

**In scope**
- **One Go binary** with an embedded SPA, running in-cluster (Helm) or locally (kubeconfig) (D-002).
- **Discovery of kgateway controller installs** and a scope per install (D-009):
  - an install switcher;
  - a cluster landing page shown only when there is more than one install;
  - a pinned mode.
- **Install page.** Header: logo, version, edition, controller readiness and scope. Then a health strip with findings, count tiles, and the Gateway list (layout A; D-011).
- **A read-only YAML view** of a single resource, as the link target of a finding until Phase 2 adds detail pages.
- **Health checks:** status conditions, unattached routes, routes in unwatched namespaces, controller and proxy readiness, xDS rejects, scope verification, and signal availability (D-006).
- **Design foundation:** the Control room visual direction, design tokens, two themes (dark default and light), the health-state primitives, and the frontend extension points (D-008, D-010, D-011).
- **Build foundation:** container image, Helm chart, CI, Apache-2.0 licence, README.
- **Extension seam:** Go functional options and registries, a frontend registry, and a test-only extension that exercises both.

**Out of scope (later phases)**
- Gateway and route detail pages, and flows (Phase 2 and later).
- The controller admin server (`:9095`) and the Envoy admin API.
- Authentication (Phase 10).
- Live push (SSE); the UI polls.
- Publishing the npm package to a registry (seam milestone after Phase 2).
- Prometheus integration.

## 2. Primary flows

1. **"Is it healthy?"** Open the UI. With one install, it lands on that install's page; with several, it lands on the cluster page with one card per install.
   - Healthy: the strip is a slim green line, and a "What was checked" list proves that each signal was collected.
   - Unhealthy: the strip shows counts per severity, followed by the top 5 findings by severity, then "Show all".
2. **"What is wrong with X?"** Click a finding to open a read-only YAML view of the resource (status first, `managedFields` stripped), with a copy button for its `namespace/name`.
3. **"What does this install manage?"**
   - Count tiles: Gateways, routes per kind, policies per kind, and watched namespaces as "N / M".
   - The Gateway table, which is sortable and filterable, with a deep link per state.

## 3. Architecture

```
 browser ── SPA (React 19, embedded) ──HTTP JSON──┐
                                                  ▼
 kgateway-ui binary ──────────────────────────────────────────────────────────
   api            /api/v1/...  (versioned JSON, read-only)
   overview       per-install counts, gateway list, header data
   health         registered checks → findings → roll-up per install
   install        install discovery, ScopeProvider, ScopeVerifier, assignment
   cache          client-go dynamic informers (one shared cache, cluster-wide)
   kinds          kind registry + API discovery (served CRDs only)
   ctrlmetrics    scrapes controller /metrics per replica
   extension      functional options + registries (the public seam, D-003)
 ─────────────────────────────────────────────────────────────────────────────
        │ watch (get/list/watch)                │ HTTP GET /metrics
        ▼                                       ▼
   kube-apiserver                         kgateway controller pods :9092
                                          (in-cluster: pod IP; local: pods/proxy)
```

### 3.1 Go module layout

```
cmd/kgateway-ui/           main: flags, wiring, default (upstream) registrations
pkg/extension/             Options, Registry, functional options (public API)
pkg/kinds/                 Kind descriptors, discovery of served kinds
pkg/cache/                 shared informer cache over the dynamic client
pkg/install/               Install, InstallDetector, ScopeProvider, ScopeVerifier, assignment
pkg/health/                Check, Finding, codes, roll-up
pkg/overview/              install overview model
pkg/ctrlmetrics/           controller metrics scraping + parsing
pkg/api/                   HTTP handlers, JSON types (versioned)
internal/server/           HTTP server, SPA embedding, /healthz, /readyz
web/                       frontend (see §6)
deploy/helm/kgateway-ui/   Helm chart
```

Everything a downstream needs lives under `pkg/`. `internal/` holds only wiring that a downstream replaces with its own `main`.

### 3.2 Dependencies (backend)

- **client-go** (dynamic client and informers).
- **`sigs.k8s.io/gateway-api`**: typed Gateway API structs, converted from unstructured objects.
- **`prometheus/common/expfmt`** to parse metrics.
- **No dependency on the kgateway Go module.**
  - kgateway policies are handled generically: `metadata`, `spec.targetRefs` / `targetSelectors`, and `status.ancestors[]`, which follows the standard Gateway API `PolicyStatus` shape.
  - This avoids coupling the UI's release cadence and dependency tree to kgateway's.
  - Kind descriptors carry what a kind needs (group, kind, plural, category, display name). Upstream kinds are registered in `cmd/`; downstream kinds are registered the same way.

## 4. Installs and scope (D-009)

### 4.1 Install discovery

An **Install** is one kgateway controller deployment. It has:
- an identity (`namespace/name` of the controller Deployment);
- its version and edition;
- its `controllerNames`;
- its `Scope`;
- its controller Pods (for metrics).

**InstallDetector** is a registered interface; there can be several.
- The upstream detector finds controller Deployments by Helm labels: `app.kubernetes.io/component=controller`, plus the kgateway chart's `app.kubernetes.io/name`.
- It reads the version from `app.kubernetes.io/version`, falling back to the image tag.
- It sets edition = upstream when the image is the kgateway controller image.
- It sets `controllerNames` to the upstream defaults: `kgateway.dev/kgateway`, plus the waypoint class's controller name when `KGW_ENABLE_WAYPOINT` is true in the Pod env.
- A downstream registers its own detector, which recognises its images and env and supplies its controller names, edition label and logo.
- Each detector also supplies the **label selectors** for its controller Pods/Deployments and its proxy Deployments (upstream: the controller labels above, and `app.kubernetes.io/managed-by=kgateway` for proxies). The cache watches Pods and Deployments only through these selectors, never cluster-wide unfiltered.

### 4.2 ScopeProvider

`Scope(install) → {namespaces predicate, controllerNames}`. v1 is `NamespaceSelectorScope`:
- **Input.** It reads `KGW_DISCOVERY_NAMESPACE_SELECTORS` from the spec of a running controller Pod (not the Deployment). Duplicate env entries resolve last-wins. A `valueFrom` reference is not supported: the scope becomes `unknown` with a finding.
- **Semantics** (mirrors kgateway):
  - The value is a JSON list of `metav1.LabelSelector`.
  - A namespace is in scope if **any** selector matches it.
  - An empty list means all namespaces.
  - An invalid selector makes the scope `unknown`; kgateway itself would fail to start.
- **Liveness.** It uses the shared Namespace informer, so label changes apply immediately.
- **Tests.** The semantics are pinned by table tests derived from kgateway's own discovery tests for each supported version (§9).

### 4.3 Assignment

- **Gateway.** A Gateway belongs to install I when its GatewayClass `controllerName` ∈ I.controllerNames **and** its namespace is in I's scope.
  - If more than one install matches, it is assigned to none and gets finding `KGWUI-009 ambiguous-ownership`.
  - Whether kgateway allows two installs to share a `controllerName` with disjoint scopes is verified in the first plan task. The rule above is safe either way.
- **Route** (HTTP, GRPC, TCP, TLS). A route belongs to install I when its namespace is in I's scope and at least one parentRef resolves to a Gateway (or ListenerSet) of I.
  - A route whose namespace is **outside** the scope of the install owning its target Gateway gets finding `KGWUI-004 namespace-not-watched` (warning). It never counts towards totals or health.
- **Policy** (any kind with category `policy`). A policy belongs to I when its namespace is in I's scope.
  - Its health comes only from `status.ancestors[]` entries whose `controllerName` ∈ I.controllerNames.
  - The synthetic ancestor `gateway.kgateway.dev/StatusSummary` is treated as a status summary, not as a Gateway.
- **Out-of-scope resources** are never counted, even if they carry old kgateway status. This handles stale status left behind when a namespace is deselected.

### 4.4 ScopeVerifier

v1 compares, per install and per `(namespace, resource kind)`, the UI's count of namespace-scoped raw objects with the controller's `kgateway_resources_managed` gauge. That is the same population: namespace-filtered and not ownership-filtered.
- A mismatch that persists for more than 2 scrape intervals produces `KGWUI-007 scope-mismatch` (warning), showing both numbers.
- If the metric is absent (renamed or unavailable), the verifier reports `KGWUI-008 signal-unavailable` (info). It does not decide health.
- The plan's first task confirms the per-replica semantics of this gauge, i.e. whether every replica reports the full set.

## 5. Health

### 5.1 Finding

```json
{
  "code": "KGWUI-002",
  "slug": "condition-false",
  "severity": "error",              // error | warning | info
  "install": "kgateway-system/kgateway",
  "resource": {"group": "gateway.networking.k8s.io", "kind": "HTTPRoute", "namespace": "shop", "name": "cart"},
  "message": "Backend not found",   // operator language
  "detail": "ResolvedRefs=False, reason BackendNotFound, parent prod/edge",
  "since": "2026-10-17T09:12:03Z"
}
```

- Codes are stable and never reused.
- Upstream codes use the prefix `KGWUI-`. A downstream registers its codes under its own prefix.
- `message` is plain language. The raw condition type, status and reason go in `detail`.

### 5.2 Phase 1 checks

| Code | Slug | Severity | Source |
|---|---|---|---|
| KGWUI-001 | xds-rejected | error | `kgateway_envoy_xds_rejects_active{gateway,namespace,type_url} > 0`, summed over controller replicas |
| KGWUI-002 | condition-false | error / warning | GatewayClass `Accepted`; Gateway `Accepted`/`Programmed`; listener conditions; route parent `Accepted`/`ResolvedRefs` (error) and `kgateway.dev/Programmed`; policy ancestor `Accepted`/`Attached` (`Invalid`, `TargetNotFound`, … → error; `PartiallyValid`, `Overridden` → warning) |
| KGWUI-003 | route-unattached | warning | A route that is in scope but whose parentRefs resolve to no Gateway of the install, or that has no parent status from the install's controller |
| KGWUI-004 | namespace-not-watched | warning | §4.3 |
| KGWUI-005 | controller-not-ready | error | Controller Deployment has ready replicas < desired replicas |
| KGWUI-006 | proxy-not-ready | error | The proxy Deployment of an assigned Gateway has ready replicas < desired replicas |
| KGWUI-007 | scope-mismatch | warning | §4.4 |
| KGWUI-008 | signal-unavailable | info (forces state `unknown`; see 5.3) | A check could not collect its input: metrics unreachable, RBAC denied, scope unknown, or cache not synced |
| KGWUI-009 | ambiguous-ownership | warning | §4.3 |

`RouteRuleDropped` and `RouteRuleReplaced` reasons are reported under KGWUI-002 with their own message ("Rule replaced by a direct response").

### 5.3 Roll-up

Per install:
1. Any finding with severity error → **error**.
2. Otherwise, any required signal not collected → **unknown**.
3. Otherwise, any warning → **warning**.
4. Otherwise → **healthy**.

Unknown ranks above warning and healthy, so a missing signal is never shown as green or merely yellow (D-006).

Every check reports `{name, ok, collectedAt, error}`. The healthy view renders this list as "What was checked".

### 5.4 Failure handling

| Situation | Behaviour |
|---|---|
| API server unreachable, or cache not synced | Full-page "Connecting to cluster…" with the cause after a timeout. Numbers are never shown before the first sync. |
| Controller metrics unreachable | KGWUI-008, and the install becomes unknown. The strip reads "No known problems — xDS rejects could not be checked". |
| No install found | Designed empty state explaining what the UI looks for and how to pin `--install`. |
| RBAC denies a kind | Its tile shows "No access", naming the missing verb and resource; KGWUI-008. |
| A kind's CRD is not served | The tile is hidden. Not a finding. |
| Data older than 2× the poll interval | "Checked …" turns into a stale warning and the strip shows unknown. |

## 6. Frontend

### 6.1 Stack (D-010)

- React 19, TypeScript and Vite.
- `@emotion/react` / `@emotion/styled`, `react-router`, `lucide-react` and SWR.
- TanStack Table with virtualisation for the dense tables.
- Vitest and Testing Library for unit tests, Playwright for e2e, and Biome for linting and formatting.
- No proprietary component libraries. Components follow the team frontend conventions: one component per file, `// region` organisation, and emotion with `stylesOverride`.

### 6.2 Structure

```
web/
  packages/shell/      the reusable package: app shell, router, theme + tokens,
                       health primitives, extension registry, API client
  app/                 the upstream entry: registers the upstream pages, theme and logo; builds the SPA
```

A downstream build has its own `app/`, which imports `packages/shell` and registers its extras. The package stays unpublished in Phase 1, and the downstream test in §9 consumes it through the workspace.

### 6.3 Frontend extension points

- **Pages and routes:** under `/i/:ns/:name/...` or at the top level.
- **Navigation entries.**
- **Health renderers:** a custom message or detail per finding code prefix.
- **Kind display metadata.** This is mostly backend-driven through descriptors, so most downstream additions need Go only.
- **Theme sets and the brand:** logo, product name and tokens (D-008).

### 6.4 Pages and URLs

| URL | Page |
|---|---|
| `/` | Redirects to `/i/:ns/:name` when there is one install (or the UI is pinned); otherwise the cluster landing (layout C) |
| `/i/:ns/:name` | Install page (layout A) |
| `/i/:ns/:name/findings` | All findings, filterable by severity, code and kind (the target of "Show all") |
| `/i/:ns/:name/r/:group/:kind/:namespace/:name` | Read-only YAML view of one resource |

Filters and sorting are stored in the query string, so every view can be shared as a link.

### 6.5 Visual direction (D-011)

- **"Control room".**
  - The dark theme is the default. It is built on the logo ink `#151927` with the logo blue `#002DC9` as the accent, used sparingly.
  - The light theme keeps the ink header band.
  - Typefaces: Figtree for UI text, JetBrains Mono for resource names. Both are bundled locally, with no runtime font CDN.
- **Signature element.** Resource names are set in mono, with the `namespace/` prefix dimmed.
- **Health vocabulary.** Healthy ✓, Warning !, Error ✕ and Unknown ? — always icon + colour + text, at WCAG AA in both themes.
- **Layout A (install page).**
  - The header carries the logo, version chip, install name, controller readiness, scope and the install switcher.
  - The health strip is full width. When the install is healthy it is a slim line.
  - Below it: the top 5 findings, the count tiles, and the Gateway table (status, name, class, listeners, attached routes, addresses).
- **Theme switcher.** A compact icon dropdown. The chosen theme is stored in `localStorage`.
- **Logo.** The default theme uses the kgateway logo from CNCF artwork, used to identify the project it is a UI for, under the Linux Foundation trademark policy.

### 6.6 Data fetching

- SWR polls the API every 5 s, using the backend's cache (not the API server).
- The backend scrapes controller metrics every 10 s.
- Every response carries `collectedAt`, which drives the "Checked …" label and the stale state.

## 7. API (v1)

The API is read-only JSON. Every resource payload has `managedFields` removed, and Secrets are never served.

| Endpoint | Returns |
|---|---|
| `GET /api/v1/installs` | `[{id, namespace, name, version, edition, controllerNames, scope: {mode, selectors, watched, total}, health: {state, counts}}]` |
| `GET /api/v1/installs/{ns}/{name}/overview` | Header data, counts per kind (`{kind, count, access}`), and the Gateway list |
| `GET /api/v1/installs/{ns}/{name}/health` | `{state, findings[], checks[], collectedAt}` |
| `GET /api/v1/installs/{ns}/{name}/resources/{group}/{kind}/{namespace}/{name}` | One object (in scope only) |
| `GET /api/v1/meta` | UI version, pinned install, registered kinds and descriptors |
| `GET /healthz`, `GET /readyz` | Liveness; readiness is true once the cache has synced |

## 8. Packaging, RBAC, deployment

- **Binary flags:**
  - `--kubeconfig`;
  - `--install <ns>/<name>` (pinned mode);
  - `--listen` (default `:8080`; `127.0.0.1:8080` in local mode);
  - `--poll-interval`;
  - `--metrics-interval`.
- **Image.** A multi-stage build into distroless `static:nonroot`, built for linux/amd64 and linux/arm64, published to `ghcr.io/duncandoyle/kgateway-ui`.
- **Helm chart** (`deploy/helm/kgateway-ui`, published as OCI on ghcr):
  - Deployment with 1 replica, the restricted pod security context and resource requests.
  - A ClusterIP Service.
  - A ServiceAccount, ClusterRole and ClusterRoleBinding.
  - `install.pin`.
  - `httpRoute.enabled` (default `false`), with `parentRef` and `hostnames`. It creates an HTTPRoute on an existing Gateway. The chart notes recommend putting authentication in front of the UI.
- **ClusterRole: get/list/watch only.**
  - `gateway.networking.k8s.io`: gatewayclasses, gateways, httproutes, grpcroutes, tcproutes, tlsroutes, referencegrants, backendtlspolicies, listenersets.
  - `gateway.networking.x-k8s.io`: xlistenersets.
  - `gateway.kgateway.dev`: `*`.
  - `apiextensions.k8s.io`: customresourcedefinitions.
  - Core: namespaces, pods.
  - `apps`: deployments.
  - Pods and Deployments are only listed and watched through the detectors' label selectors (§4.1). RBAC cannot express a label restriction, so the ClusterRole grants the verbs, and the code keeps the watches narrow.
  - **No Secrets** and no `*/proxy` access. In-cluster, controller metrics are scraped directly over the pod network, which requires network access to the controller pods on 9092. Under a restrictive NetworkPolicy this shows up as KGWUI-008, and the chart README documents it.
- **Local mode** uses the user's kubeconfig rights. Metrics go through `pods/proxy` on each controller Pod. If that is denied, the result is KGWUI-008, not a failure.
- **CI** (GitHub Actions): `go vet`, golangci-lint, `go test`, Biome, Vitest and the build. On tags: the image, the chart and a GitHub release. A kind e2e job (§9) runs on PRs.

## 9. Testing

Behaviour is tested at the highest practical boundary. The primary boundary is **cluster objects in → API JSON out**.

- **API boundary tests (Go).** A fake dynamic client is seeded with fixture objects, the real cache, install, health and overview packages run on top of it, and the HTTP handlers are called. Required cases:
  - each check KGWUI-001 to KGWUI-009, positive and negative;
  - a route in an unwatched namespace: not counted, and finding 004 raised;
  - a Gateway of another controller: ignored;
  - stale kgateway status in a deselected namespace: ignored;
  - metrics unreachable: state is unknown, never healthy;
  - a CRD not served: tile hidden, no finding;
  - two installs with overlapping selectors: finding 009;
  - a pinned install: the others are not visible;
  - namespace labels changed: scope updates without a restart.
- **NamespaceSelectorScope.** Table tests mirroring kgateway's discovery semantics (OR, empty list, invalid selector, last-wins env) for each supported version.
- **Metrics parsing.** Fixture `/metrics` payloads captured from kgateway 2.3 and 2.4 controllers.
- **Frontend.** Vitest and Testing Library for the health primitives, the theme tokens (contrast checked in both themes) and the pages against a mocked API.
- **Extension seam.**
  - In Go, a test-only extension registers an extra kind, detector, check and code prefix; its findings and counts appear in API output.
  - In the frontend, a test-only app registers a page and a theme; the page renders in the shell and the tokens apply.
- **e2e** (kind, run against kgateway 2.3 and 2.4 in a matrix):
  1. Install kgateway and fixture resources, including deliberately broken ones and a namespace outside the selector.
  2. Install the chart.
  3. Check the API.
  4. Run a Playwright smoke test of the landing page, the install page, the findings list and the YAML view, in both themes.
  - A second scenario runs two installs.

## 10. Supported versions

- kgateway **2.3.x** and **2.4.x** in CI; `main` is tracked as a non-blocking e2e job.
- Kinds are discovered per cluster, so kinds that exist only in some versions (for example `HTTPListenerPolicy` on 2.3 and 2.4) appear and disappear without code changes.

## 11. Security

- The UI is read-only (D-001). It never reads Secrets, and resource payloads have `managedFields` removed.
- It never calls the controller admin server (`:9095`) or the Envoy admin API.
- It has no authentication of its own in Phase 1 (D-004). Access control is the Service reachability plus whatever the operator puts in front of an optional HTTPRoute.
- The pod runs as non-root on a read-only root filesystem, with all capabilities dropped.

## 12. Cost

This phase generates no data-plane configuration. The cost falls on the control plane and the API server:
- **Watches.** One cluster-wide watch per registered served kind: about 20 kinds, plus Namespaces, and label-selected watches on Pods (controller only) and Deployments (controller and proxies). That is comparable to a single extra controller's list/watch load.
- **Memory.** The memory footprint scales with the number of Gateway API and kgateway objects in the cluster. Secrets and ConfigMaps are not cached.
- **Metrics scraping.** One `GET /metrics` per controller replica every 10 s.
- **No new data-plane objects or xDS churn.** The metric names the UI depends on are listed in one place (`pkg/ctrlmetrics`), so a rename upstream is a one-line change and shows up as KGWUI-008 until it is made.

## 13. Open items

1. **Shared `controllerName` across installs.** Can two kgateway installs share a `controllerName` with disjoint scopes, and how do they behave? This is the first plan task (kind experiment). §4.3 is safe either way.
2. **Replica semantics of the controller metrics.** Is `kgateway_envoy_xds_rejects_active` per replica (and summed), and does every replica report the full `kgateway_resources_managed`? This is the first plan task, together with item 1.
3. **Upstream proposal** for an effective-scope endpoint (D-009 §7). It is filed during Phase 1, but it does not block the phase.
