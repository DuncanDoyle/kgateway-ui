# Phase 1 — Foundation & Overview: Design

**Status:** Revised after five cross-model reviews (`reviews/design-codex.md` and rounds 2–5), for approval
**Date:** 2026-10-17, revised 2026-10-18
**Base:** `560c393`
**Roadmap:** `docs/roadmap.md`, Phase 1
**Decisions:** D-001 … D-013 in `docs/decisions.md`

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
- **Health checks:** status conditions (with freshness), resources not yet processed, unattached routes, routes in unwatched namespaces, controller and proxy readiness, xDS rejects, scope verification, and signal availability and age (D-006).
- **Design foundation:** the Control room visual direction, design tokens, two themes (dark default and light), the health-state primitives, and the frontend extension points (D-008, D-010, D-011).
- **Build foundation:** container image, Helm chart, CI, Apache-2.0 licence, README.
- **Extension seam:** Go functional options and registries, a frontend registry, and a test-only extension that exercises both.
  - Backend: kinds, install detectors, scope provider, scope verifier, checks, and the API `Authorizer` (§7.1). Finding code prefixes are a documented convention (`WithChecks`), not a registry; a registry is deferred to the seam milestone.
  - Frontend: brand, themes (including typography tokens), routes, navigation, and finding renderers.
  - Analyzers, API-route registration, per-kind renderers and health-card contributions are deferred to the seam milestone after Phase 2.
- **Trust model** (D-013): whoever reaches the UI may see everything it shows. It is documented prominently, and an allow-all `Authorizer` seam is in place for later per-user authorisation.

**Out of scope (later phases)**
- Gateway and route detail pages, and flows (Phase 2 and later).
- The controller admin server (`:9095`) and the Envoy admin API.
- Authentication and per-user authorisation (Phase 10; it plugs into the `Authorizer` seam, D-013).
- Live push (SSE); the UI polls.
- Publishing the npm package to a registry (seam milestone after Phase 2).
- Prometheus integration.

## 2. Primary flows

1. **"Is it healthy?"** Open the UI. With one install, it lands on that install's page; with several, it lands on the cluster page with one card per install.
   - Healthy: the strip is a slim green line, and a "What was checked" list proves that each signal was collected.
   - Unhealthy: the strip shows counts per severity, followed by the top 5 findings by severity, then "Show all".
2. **"What is wrong with X?"** Click a finding to open a read-only YAML view of the resource (status first, `managedFields` stripped), with a copy button for its `namespace/name`.
   - Every finding whose resource kind is listed in `/api/v1/meta` links to that view, and the link resolves (§7). This includes unwatched routes, ambiguous Gateways, the controller Deployment and the Gateways whose proxy is not ready.
   - A finding whose kind is not listed shows its resource as plain text, without a link.
3. **"What does this install manage?"**
   - Count tiles: GatewayClasses, Gateways, routes per kind, policies per kind, and watched namespaces as "N of M namespaces".
   - The Gateway table, which is sortable and has a free-text filter, with a deep link per state. Sort and filter live in the URL.

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
pkg/api/                   HTTP handlers, JSON types (versioned), Authorizer seam
internal/server/           HTTP server, SPA embedding, /healthz, /readyz
web/                       frontend (see §6)
deploy/helm/kgateway-ui/   Helm chart
```

Everything a downstream needs lives under `pkg/`. `internal/` holds only wiring that a downstream replaces with its own `main`.

### 3.2 Dependencies (backend)

- **client-go** (dynamic client and informers).
- **`k8s.io/api` and `k8s.io/apimachinery`** (Namespaces, Pods, Deployments and unstructured objects).
- **No `sigs.k8s.io/gateway-api` dependency.** Gateway API objects stay unstructured and are read through the UI's own views in `pkg/objects` (parentRefs, conditions, ancestors).
- **`prometheus/common/expfmt`** and **`prometheus/client_model`** to parse metrics.
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
- The upstream detector finds controller Deployments by the chart's labels `kgateway=kgateway,app.kubernetes.io/component=controller`.
- It reads the version from `app.kubernetes.io/version`, falling back to the image tag.
- It sets edition = upstream when the image is the kgateway controller image.
- It sets `controllerNames` to the upstream default `kgateway.dev/kgateway`. The waypoint GatewayClass (`kgateway-waypoint`) uses the same controller name.
- A downstream registers its own detector, which recognises its images and env and supplies its controller names, edition label and logo.
- Each detector supplies three **label selectors**. Controller Pods are selected more loosely than the Deployment, so a chart that omits the component label on Pods still works:

  | Selector | Upstream value | Why |
  |---|---|---|
  | Controller Deployments | `kgateway=kgateway,app.kubernetes.io/component=controller` | `component=controller` is set on the Deployment only |
  | Controller Pods | `kgateway=kgateway` | On 2.3 and later, controller Pods carry `kgateway=kgateway`, `app.kubernetes.io/name`, `app.kubernetes.io/instance` and `app.kubernetes.io/component=controller` (added in kgateway [`9041e96061`](https://github.com/kgateway-dev/kgateway/commit/9041e96061)); 2.2 and earlier lack the component label |
  | Proxy Deployments | `app.kubernetes.io/managed-by=kgateway` | |

- The cache watches Pods and Deployments only through these selectors, never cluster-wide unfiltered. The Pod watch uses the controller Pod selector.
- A controller Pod belongs to the install whose Deployment's `spec.selector` matches it in the same namespace. The Pod selector alone does not tell two installs apart.

### 4.2 ScopeProvider

`Scope(install) → {namespaces predicate, controllerNames}`. v1 is `NamespaceSelectorScope`:
- **Input.** It reads `KGW_DISCOVERY_NAMESPACE_SELECTORS` from a Ready controller Pod, else any controller Pod, else the controller Deployment's Pod template when no Pod exists (D-015). The Pod comes first because it reflects what runs during a rollout; the scope check names the source when the template is used. Duplicate env entries resolve last-wins. A `valueFrom` reference is not supported: the scope becomes `unknown` with a finding.
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
  - An install whose scope is unknown competes for a Gateway only when no install with a known scope contains it. With two or more candidates in either group the Gateway is ambiguous, as above.
  - Whether kgateway allows two installs to share a `controllerName` with disjoint scopes is verified in the first plan task. The rule above is safe either way.
- **Reference resolution.** Every reference is resolved by its full identity: group, kind, namespace and name.
  - A parentRef defaults to group `gateway.networking.k8s.io`, kind `Gateway`, and the route's namespace, as the Gateway API defines.
  - A parentRef with a foreign group never resolves to a Gateway API object of the same kind and name. The same applies to a ListenerSet's parent.
- **Route** (HTTP, GRPC, TCP, TLS). A route belongs to install I when its namespace is in I's scope and at least one parentRef resolves to a Gateway (or ListenerSet) of I.
  - Its health comes only from `status.parents[]` entries that meet both conditions: the entry's `controllerName` ∈ I.controllerNames, and its `parentRef` equals a **current** `spec.parentRefs` entry that resolves to a Gateway or ListenerSet owned by I. Equality covers the whole parentRef, `sectionName` and `port` included (unset differs from set), so status for a removed port or section never satisfies the current one.
  - Status left behind for a removed parentRef is ignored. So is status for a parent owned by another install that shares the controller name.
  - A route whose namespace is **outside** the scope of the install owning its target Gateway gets finding `KGWUI-004 namespace-not-watched` (warning). It never counts towards totals or health.
  - A parent is never reported as not found while access to the parent's kind is unavailable: it may only be missing from the cache, and the cluster-access check already reports that.
  - A route whose parentRef targets a Gateway or ListenerSet that **does not exist** gets `KGWUI-003` in every install whose scope contains the route's namespace, once per missing parent (findings are deduplicated by resource and parentRef). It counts in an install's totals only when at least one other parentRef resolves to a Gateway or ListenerSet owned by that install; a route with no owned parent is a diagnostic candidate and is not counted anywhere.
- **Policy** (any kind with category `policy`).
  - A policy in the kgateway API group belongs to I when its namespace is in I's scope.
  - Any other policy (for example `BackendTLSPolicy` or a downstream policy) belongs to I when its namespace is in I's scope **and** either it has an ancestor whose `controllerName` ∈ I.controllerNames, or it has no ancestors at all (new, not yet processed). Inventory membership does not wait for status.
  - **Owned-ancestor rule.** An ancestor from I's controller that is a Gateway or ListenerSet must resolve to a Gateway or ListenerSet that I owns; if all such ancestors belong to another install, so does the policy. Any other ancestor from I's controller (a backend, `StatusSummary`) counts by `controllerName`, so the policy belongs to every install whose scope contains it.
  - Its health comes only from `status.ancestors[]` entries whose `controllerName` ∈ I.controllerNames. A policy with no such ancestor is "not yet processed" after the grace period (§5.2).
  - The synthetic ancestor `gateway.kgateway.dev/StatusSummary` is treated as a status summary, not as a Gateway. It names no Gateway, so it counts by `controllerName`. Only kgateway `main` writes it (§10).
- **Out-of-scope resources** are never counted, even if they carry old kgateway status. This handles stale status left behind when a namespace is deselected.
- **Known limit.** A policy whose only status ancestors are deleted Gateways appears in no install until kgateway rewrites its status.

### 4.4 ScopeVerifier

v1 compares, per install and per `(namespace, resource kind)`, the UI's count of namespace-scoped raw objects with the controller's `kgateway_resources_managed{namespace,parent,resource}` gauge, summed over `parent`. That is the same population: namespace-filtered and not ownership-filtered.
- The UI must weight objects the way the gauge does (verified on kgateway main `8a498c6406`, `pkg/krtcollections/metrics/metrics.go`): a Gateway counts 1; a route counts once per `spec.parentRefs` entry, so a route with no parentRefs counts 0; a ListenerSet counts 1 and is reported as `resource="XListenerSet"`; a policy counts 1 under its own kind.
- A gauge with no samples has no series, so an absent family in a successful scrape means 0, not "unavailable".
- A mismatch that persists for more than 2 scrape intervals produces `KGWUI-007 scope-mismatch` (warning), showing both numbers.
- For the verifier, an absent `resources_managed` family counts as 0. Whether that is trusted is the xDS check's concern (§5.2).
- If the metric is unavailable (renamed or unreachable), the verifier reports `KGWUI-008 signal-unavailable` (info). It does not decide health.
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
- Upstream codes use the prefix `KGWUI-`. A downstream uses its own prefix; this is a documented convention (see `WithChecks`), not enforced by a registry (§1).
- A check emitting a severity other than error, warning or info is reported as warning, with the original named in `detail`.
- `message` is plain language. The raw condition type, status and reason go in `detail`.

### 5.2 Phase 1 checks

| Code | Slug | Severity | Source |
|---|---|---|---|
| KGWUI-001 | xds-rejected | error | `kgateway_envoy_xds_rejects_active{gateway_namespace,gateway_name,type_url} > 0`, summed over controller replicas. An absent reject family in a successful scrape means no rejects, but only when the scrape has a `kgateway_resources_managed` family or the install owns no Gateways; otherwise the check is unavailable (KGWUI-008), because an absent reject family is then indistinguishable from a wrong endpoint. |
| KGWUI-002 | condition-false | error / warning / info | A **current** condition (see freshness below) is False: GatewayClass `Accepted`; Gateway and ListenerSet `Accepted`/`Programmed`; listener conditions; route parent `Accepted`/`ResolvedRefs` (error) and `kgateway.dev/Programmed`; policy ancestor `Accepted`/`Attached` (`Invalid`, `TargetNotFound`, … → error; `PartiallyValid` → warning; `Overridden` → info, since overriding is normal Gateway API precedence, not a fault) |
| KGWUI-003 | route-unattached | info while being processed, warning after the grace period | A route whose parentRef targets a Gateway or ListenerSet that does not exist (§4.3). Also any owned resource **not yet processed** (see freshness below): a missing status block, an absent required condition, a condition with reason `Pending`, stale conditions, or (for a policy) no ancestor from the install's controller. Inside the grace period the message reads "Being processed by the controller" (info); after it, "Not yet processed by the controller" (warning). |
| KGWUI-004 | namespace-not-watched | warning | §4.3 |
| KGWUI-005 | controller-not-ready | error | Controller Deployment has ready replicas < desired replicas, or 0 desired replicas (D-017 §1) |
| KGWUI-006 | proxy-not-ready | error / warning | Error when the proxy Deployment of an assigned Gateway has ready replicas < desired replicas. Warning when it has 0 desired replicas, since scaling to zero may be deliberate (D-017 §1), and when no proxy Deployment exists after the grace period. Inside the grace period a missing proxy has no finding yet, but the `gateway proxies` check is pending, so the install is unknown (D-017 §3). The finding references the Gateway, which is the link target. |
| KGWUI-007 | scope-mismatch | warning | §4.4 |
| KGWUI-008 | signal-unavailable | info (forces state `unknown`; see 5.3) | A check could not collect a current input: metrics unreachable, metrics result too old, RBAC denied, a watch failing, scope unknown, cache not synced, or served kinds changed since startup (§5.4) |
| KGWUI-009 | ambiguous-ownership | warning | §4.3 |
| KGWUI-010 | self-managed-proxy | info | An owned Gateway is self-managed (§14.3 H5) and has no proxy Deployment: the UI cannot see that proxy's readiness. It never drives the roll-up; a proxy Deployment that exists is still judged under KGWUI-006. |

`RouteRuleDropped` and `RouteRuleReplaced` reasons are reported under KGWUI-002 with their own message ("Rule replaced by a direct response"). On 2.3 the controller reports them through `Accepted=False`; on 2.4 and `main` through `kgateway.dev/Programmed`. Both are reported, because any current False is reported with its reason.

Conditions are either required or optional (below). Optional conditions report only `False`; `Unknown` and absent produce no finding.

**Status freshness.** Every status-based check applies these rules, in this order:
1. **Freshness first.** A condition whose `observedGeneration` is lower than the object's `metadata.generation` is stale. A missing `observedGeneration` (0) on an object with a generation counts as stale too: there is no evidence it describes the current spec. A stale condition describes an old spec, so a stale True is not success and a stale False is not a current error. kgateway sets `observedGeneration` on its conditions; the first plan task confirms this on 2.3 and 2.4.
2. **Required status.** Each status block has required conditions: GatewayClass `Accepted`; Gateway `Accepted` and `Programmed`; owned ListenerSet `Accepted` and `Programmed` (D-017 §2: kgateway writes some failures only on the ListenerSet); each listener of a Gateway or ListenerSet `Accepted` and `Programmed`; route parent `Accepted` and `ResolvedRefs`; policy ancestor `Accepted`. Every listener in `spec.listeners` needs its own status block, except on a ListenerSet that is currently `Accepted=False`: there only the listener blocks present are judged, because kgateway writes none for a ListenerSet it denies (reason `NotAllowed`). Kinds that kgateway watches only with experimental features on (`kinds.ExperimentalOnly`: XListenerSet, and TCPRoute and TLSRoute before v1) are judged only once they carry status from the install's controller. Without it the UI cannot tell "not processed yet" from "not watched", so it reports nothing (D-017 §4). A missing block, an absent required condition, or a condition whose reason is `Pending` means "not yet processed". A current required condition with status `Unknown` and any other reason is also "not yet processed" (KGWUI-003), not an error: only `False` maps to KGWUI-002 (kgateway writes `Accepted=False/Pending` and `Attached=False/Pending` as policy defaults before translation). Conditions that only some versions write (for example `kgateway.dev/Programmed`, `Attached`) are evaluated when present and are not required.
3. **Grace period.** The grace period is 60 s, measured from when the UI first saw the object's current (uid, `metadata.generation`). A spec change bumps the generation and restarts it; a label, annotation or finalizer update does not. Objects that already exist when the UI starts get grace from the UI's start, so after a restart stale status shows "being processed" for 60 s, then a warning. A resource that is not yet processed inside the grace period gets `KGWUI-003` at **info** ("being processed"), and the `resource status` check is not OK, so the install is **unknown** (§5.3), never healthy. A managed Gateway without a proxy Deployment gets the same grace: the `gateway proxies` check is pending until it ends (D-017 §3). After the grace period the finding is a **warning** and the check is OK again. `KGWUI-003` is reported once per resource and parentRef: a route gets one per pending or missing parent.
4. A check reports OK only on current evidence. Absent, pending or stale status never counts as healthy.

### 5.3 Roll-up

Per install:
1. Any finding with severity error → **error**.
2. Otherwise, any required check not OK → **unknown**. A check is not OK when its signal was not collected, or when resources are still being processed inside the grace period (§5.2; the check reports `pending`).
3. Otherwise, any warning → **warning**.
4. Otherwise → **healthy**.

Unknown ranks above warning and healthy, so a missing signal is never shown as green or merely yellow (D-006). The strip leads with "Health unknown" and names the reason, for example "Health unknown: xDS rejects could not be checked" for a missing signal, or that resources are being processed by the controller while changes are in grace; it never reads "No known problems". Kept data whose poll fails, or that is older than twice the poll interval, is shown as unknown on every surface (strip, Gateway rows, landing cards, header readiness, switcher).

Every check reports `{name, ok, collectedAt, error}`. The healthy view renders this list as "What was checked".

### 5.4 Failure handling

| Situation | Behaviour |
|---|---|
| API server unreachable, or cache not synced | Full-page "Connecting to cluster…" with the cause after a timeout. Numbers are never shown before the first sync. |
| A watch fails after the initial sync | `HasSynced` stays true after later list/watch failures, so it is not used as availability. client-go also retries connection-refused and 429 watch errors without reporting them to the watch error handler, and delivers errors as events inside an open stream. Each informer therefore runs over an **instrumented ListerWatcher** that records every List and Watch call outcome and sees every event the watch delivers. A **failure** (a failed List or Watch call, or a `watch.Error` event) makes the kind unavailable. The kind recovers on a **successful classic Watch call opened after the latest failure**: the reflector resumes from its last resource version (or the server answers 410 and the reflector re-lists by itself, then watches), so no event is needed, and an idle kind recovers as soon as its watch works again. In watch-list mode it recovers on the `k8s.io/initial-events-end` bookmark of a stream opened after the failure. A successful List alone does not recover: the Watch call that follows must succeed too, so a kind whose list is allowed but watch forbidden stays unavailable. Events on a watch opened before the failure never recover it. The UI never forces a re-list, so tracking adds no load while the API server is throttling. **Dead connections** are detected by client-go's HTTP/2 connection health check (`ReadIdleTimeout` 30 s plus `PingTimeout` 15 s), so within about 45 s, and the failed watch then counts as a failure. **Known limit:** right after recovery, while client-go replays the events missed during the outage, the UI can briefly show the pre-outage state; the outage itself shows unknown. This is the same trust every controller places in its informers; periodic full relists were rejected as a permanent API load for a few-second window (`reviews/design-r5-codex.md`). Lists have a 1 min request timeout. This covers every watched kind, including Namespaces, Pods and Deployments. An errored informer counts as settled for the startup sync, but the cluster-access check is not OK: KGWUI-008, and the install becomes unknown. |
| Served kinds change after startup | Served kinds are re-resolved every 60 s. When the served set (or a kind's preferred version) differs from the watched set, each install gets a required KGWUI-008 asking for a restart ("served kinds changed: …; restart the UI to watch them"). Phase 1 never re-watches live. |
| Controller metrics unreachable | KGWUI-008, and the install becomes unknown. The strip reads "Health unknown: xDS rejects could not be checked". |
| Metrics scrape bounds | Each `/metrics` body is capped at 8 MiB; hitting the cap is an error, never a truncated parse. Each pod has a timeout of min(interval / 2, 5 s). Installs are scraped concurrently, so one hung install cannot starve the others. An install pod without an IP is an error. |
| Last metrics result too old | A metrics result older than 3 × the metrics interval is unavailable (KGWUI-008), even if that scrape succeeded. Recomputing the report never makes old evidence look fresh. |
| No install found | Designed empty state explaining what the UI looks for and how to pin `--install`. |
| RBAC denies a kind | Its tile shows "No access", naming the missing verb and resource; KGWUI-008. |
| A kind's CRD is not served | The tile is hidden. Not a finding. |
| Data older than 2× the poll interval | "Checked …" turns into a stale warning. The strip **and** every Gateway row's status show unknown. The same rule applies to the cluster cards, the install switcher and the header readiness icon, using the install summary's `computedAt`. A shared 1 s clock re-evaluates every staleness check, so data ages into stale even when polls keep returning identical responses (which SWR does not re-render). No green badge survives on stale data. |

## 6. Frontend

### 6.1 Stack (D-010)

- React 19, TypeScript and Vite.
- `@emotion/react` / `@emotion/styled`, `react-router`, `lucide-react` and SWR.
- TanStack Table with virtualisation for the dense tables.
- Vitest and Testing Library for unit tests, Playwright for e2e, and Biome for linting and formatting.
- No proprietary component libraries. Components follow the team frontend conventions: one component per file, `// region` organisation, and emotion.
- **Override contract.** Every component exported from `packages/shell`, the `Shell` root included, accepts `stylesOverride`. Page-internal styled parts are not part of the contract.
- **Type declarations.** The app compiles with Vite's client type declarations (for asset imports such as the SVG logo). The shell's public entry carries its emotion theme typing, so the app and downstream builds get the typed theme without copying a declaration file.

### 6.2 Structure

```
web/
  packages/shell/      the reusable package: app shell, router, theme + tokens,
                       health primitives, extension registry, API client
  app/                 the upstream entry: registers the upstream pages, theme and logo; builds the SPA
```

A downstream build has its own `app/`, which imports `packages/shell` and registers its extras. The package stays unpublished in Phase 1, and the downstream test in §9 consumes it through the workspace.

### 6.3 Frontend extension points

- **Pages and routes:** under `/i/:id/...` or at the top level.
- **Navigation entries.**
- **Health renderers:** a custom message or detail per finding code prefix. The prefix is a convention, not a registry (§1).
- **Kind display metadata.** This is mostly backend-driven through descriptors, so most downstream additions need Go only.
- **Theme sets and the brand:** logo, product name and tokens (D-008). Tokens cover colours and typography: font families, sizes and weights. Every component takes its type from these tokens, never from hard-coded values.
- **Deferred to the seam milestone after Phase 2:** per-kind renderers and health-card contributions (and, in Go, analyzers and API-route registration). Phase 1 pages need neither.

### 6.4 Pages and URLs

| URL | Page |
|---|---|
| `/` | Redirects to `/i/:id` when there is one install (or the UI is pinned); otherwise the cluster landing (layout C) |
| `/i/:id` | Install page (layout A) |
| `/i/:id/findings` | All findings, filterable by severity, code and kind (the target of "Show all") |
| `/i/:id/r/:group/:kind/:namespace/:name` | Read-only YAML view of one resource. The UI links here only for kinds listed in `/api/v1/meta`. |

The install ID is opaque: the SPA encodes it in URLs and never splits it (§14.3 H1).

Filters and sorting are stored in the query string, so every view can be shared as a link. This includes the Gateway table's text filter.

### 6.5 Visual direction (D-011)

- **"Control room".**
  - The dark theme is the default. It is built on the logo ink `#151927` with the logo blue `#002DC9` as the accent, used sparingly.
  - The light theme keeps the ink header band.
  - Typefaces: Figtree for UI text, JetBrains Mono for resource names. Both are bundled locally, with no runtime font CDN.
- **Signature element.** Resource names are set in mono, with the `namespace/` prefix dimmed.
- **Health vocabulary.** Healthy ✓, Warning !, Error ✕ and Unknown ? — always icon + colour + text, at WCAG AA in both themes.
- **Layout A (install page).**
  - The header carries the logo, version chip, edition, install name, controller readiness (ready / desired replicas) and the install switcher; the edition tag shows only when it differs from the product name. The install scope is shown by a count tile on the install page ("{watched} of {total}" namespaces, with the source when it comes from the Deployment template).
  - The health strip is full width. When the install is healthy it is a slim line.
  - Below it: the top 5 findings, the count tiles (including GatewayClasses), and the Gateway table (status, name, class, listeners, attached routes, addresses) with a free-text filter on name, namespace and class.
- **Theme switcher.** A compact icon dropdown. The chosen theme is stored in `localStorage`.
- **Logo.** The default theme uses the kgateway logo from CNCF artwork, used to identify the project it is a UI for, under the Linux Foundation trademark policy.

### 6.6 Data fetching

- SWR polls the API every 5 s, using the backend's cache (not the API server).
- The backend scrapes controller metrics every 10 s.
- Every response carries `collectedAt` (the install list carries `computedAt` per install), which drives the "Checked …" label and the stale state. The stale state is computed against a shared clock that ticks every second, not only when new data arrives.
- **Install identity.** Install-scoped requests never keep the previous install's data while the next one loads. A page renders install data only when the response's install id matches the install in the URL; otherwise it shows its loading state. Switching installs never shows the old install's strip, tiles or Gateways under the new install's URL.

## 7. API (v1)

The API is read-only JSON. Every resource payload has `managedFields` removed as noise. No other field is redacted (D-013). Secrets are never served, because they are never read.

| Endpoint | Returns |
|---|---|
| `GET /api/v1/installs` | `[{id, displayName, namespace?, name?, version, edition, controllerNames, readiness: {state, ready?, desired?}, scope?: {mode, selectors, watched, total}, health: {state, counts}, computedAt}]` |
| `GET /api/v1/installs/{id}/overview` | Header data (including edition and controller readiness), counts per kind (`{kind, count, access}`, GatewayClasses included), and the Gateway list. Every response carries the install `id`. |
| `GET /api/v1/installs/{id}/health` | `{install, state, findings[], checks[], counts, collectedAt}`; `install` is the install id |
| `GET /api/v1/installs/{id}/resources/{group}/{kind}/{namespace}/{name}` | One object (rules below) |
| `GET /api/v1/meta` | UI version, pinned install, `platform`, `capabilities`, registered kinds and descriptors, plus `apps/Deployment` under the `workloads` capability: every kind the resource endpoint can serve, so the UI links exactly these |
| `GET /healthz`, `GET /readyz` | Liveness; readiness is true once the cache has synced and no install was rejected for an invalid ID (§14.3 H1) |

**Resource endpoint.** It serves an object of a watched, served kind when either:
- the install owns it (§4.3); or
- a current finding of the install references it. This covers unwatched routes (004), routes with a missing parent (003), ambiguous Gateways (009), and the controller Deployment (005). A proxy-not-ready finding (006) references its Gateway.

Anything else returns 404. The endpoint reads only from the shared cache, so it can never return a kind the UI does not watch.

### 7.1 Authorizer

`pkg/api` has an `Authorizer` seam (D-013). It decides, per request, whether the caller may see a given object, identified by group, kind, namespace and name (for example `Authorize(request, ref) bool`).
- Every handler that returns resources, findings or counts goes through it: installs, overview, health and resources.
- A denied object returns 404 from the resource endpoint, the same as an absent one. It is left out of resource lists and count tiles. Findings about it are **kept but restricted** (next bullet), never dropped.
- Namespace scope counts (`watched` / `total`) and install identity are not filtered by the `Authorizer`. Revisit with per-user authorisation.
- Phase 1 ships `AllowAll`. A downstream or a later phase registers its own through `extension.WithAuthorizer`; the default is `AllowAll`.
- The roll-up is always computed over all findings of the install, never only the visible ones: a user must see that something is wrong even when they may not see the details (D-006, D-013). A finding about a resource the caller may not view is redacted, not dropped: it keeps its severity, code and resource identity (kind, namespace, name), so a platform team can tell which team to contact; its message and evidence are removed and it carries `restricted: true`. The UI renders it without a link, as "Restricted: ask the owning team".

## 8. Packaging, RBAC, deployment

- **Binary flags:**
  - `--kubeconfig`;
  - `--install <id or namespace/name>` (pinned mode);
  - `--listen` (default `:8080`; `127.0.0.1:8080` in local mode);
  - `--poll-interval`;
  - `--metrics-interval`.
- **Image.** A multi-stage build into distroless `static:nonroot`, built for linux/amd64 and linux/arm64, published to `ghcr.io/duncandoyle/kgateway-ui`.
- **Helm chart** (`deploy/helm/kgateway-ui`, published as OCI on ghcr):
  - Deployment with 1 replica, the restricted pod security context and resource requests.
  - A ClusterIP Service.
  - A ServiceAccount, ClusterRole and ClusterRoleBinding.
  - `install.pin`.
  - `httpRoute.enabled` (default `false`), with `parentRefs` and `hostnames`. It creates an HTTPRoute on an existing Gateway.
  - **Trust model in the chart** (D-013). The chart README, `NOTES.txt` and the comment on the `httpRoute` values state it plainly: anyone who reaches the UI sees everything its ServiceAccount can read, which can exceed their own RBAC. They recommend authentication and authorisation on the route, for example a kgateway TrafficPolicy attached to it. Without the route, access is a port-forward to the ClusterIP Service.
- **ClusterRole: get/list/watch only.**
  - `gateway.networking.k8s.io`: gatewayclasses, gateways, httproutes, grpcroutes, tcproutes, tlsroutes, referencegrants, backendtlspolicies, listenersets.
  - `gateway.networking.x-k8s.io`: xlistenersets.
  - `gateway.kgateway.dev`: `*`.
  - `apiextensions.k8s.io`: customresourcedefinitions.
  - Core: namespaces, pods.
  - `apps`: deployments.
  - Pods and Deployments are only listed and watched through the detectors' label selectors (§4.1). RBAC cannot express a label restriction, so the ClusterRole grants the verbs, and the code keeps the watches narrow. The grant is still cluster-wide, and that is part of the trust model the chart documents.
  - **No Secrets** and no `*/proxy` access. Under D-013, excluding Secrets is the UI's whole secret-handling guarantee; Deployments and other objects are served unredacted. In-cluster, controller metrics are scraped directly over the pod network, which requires network access to the controller pods on 9092. Under a restrictive NetworkPolicy this shows up as KGWUI-008, and the chart README documents it.
- **Local mode** uses the user's kubeconfig rights. Metrics go through `pods/proxy` on each controller Pod. If that is denied, the result is KGWUI-008, not a failure.
- **CI** (GitHub Actions): `go vet`, golangci-lint, `go test`, Biome, Vitest and the build. On tags: the image, the chart and a GitHub release. A kind e2e job (§9) runs on PRs.
- **README.** The project README has a prominent "Security and trust model" section that states D-013: who sees what, that Secrets are never read, the exposure options, and that per-user authorisation is on the roadmap.

## 9. Testing

Behaviour is tested at the highest practical boundary. The primary boundary is **cluster objects in → API JSON out**.

- **Fixtures mirror upstream exactly.** Fake controller Pods carry exactly the labels upstream 2.3/2.4 Pods carry, and one test uses a Pod without the component label to prove the looser Pod selector. Fixture Pods and Deployments enter through the production label-selected watches, never around them.
- **API boundary tests (Go).** A fake dynamic client is seeded with fixture objects, the real cache, install, health and overview packages run on top of it, and the HTTP handlers are called. Required cases:
  - each check KGWUI-001 to KGWUI-010, positive and negative, at the API level;
  - status freshness, each on an isolated input that is healthy without it, asserting the state, the finding and its severity, the absence of a wrong current-error finding, and the inventory counts: a stale True, a stale False, a missing `observedGeneration`, an absent required condition, reason `Pending`, a listener without a status block, a parent status for another port, a policy with no ancestors and a new non-kgateway policy, each not yet processed (warning) after the grace period; and a resource inside the grace period, being processed (info) with the install unknown;
  - parent status for a removed parentRef, and for a parent owned by another install with the same controller name: ignored, install healthy;
  - a route with one owned parent and one missing parent: counted, and finding 003 for the missing parent;
  - a parentRef with a foreign group: does not resolve to a Gateway API object;
  - a route whose parent does not exist: finding 003 in each install whose scope holds its namespace, not counted;
  - a new non-kgateway policy without status: counted, then not yet processed after the grace period;
  - a route in an unwatched namespace: not counted, and finding 004 raised;
  - a Gateway of another controller: ignored;
  - stale kgateway status in a deselected namespace: ignored;
  - metrics unreachable, and a metrics result older than 3 × the metrics interval: state is unknown, never healthy;
  - watch failures after the initial sync, through the fake dynamic and typed clients with fake watchers that emit bookmark and error events: a failed Watch call (connection refused, 429, 403) and an error event mid-stream make the state unknown, further failed attempts keep it so, and the next successful Watch call recovers it (a resumed classic watch, or a new one after a relist);
  - at the ListerWatcher level: a successful classic Watch call after a failure recovers, with no event and no List call; a failed Watch call after a successful List keeps the kind unavailable; events on an already-open watch do not recover; a watch-list stream's initial events do not recover, its initial-events-end bookmark does;
  - grace: an object's grace runs from when its current generation was first seen; an annotation-only update does not restart it and a generation bump does;
  - a route with two missing parents, and one with a pending owned parent and a missing one: one 003 per parent;
  - a CRD not served: tile hidden, no finding;
  - two installs with overlapping selectors: finding 009;
  - a pinned install: the others are not visible;
  - namespace labels changed: scope updates without a restart;
  - every finding's resource link resolves through the resource endpoint, including the controller Deployment, an unwatched route and an ambiguous Gateway; an unrelated object returns 404;
  - a deny-some `Authorizer` registered through the extension: the denied resource returns 404 and leaves the count tiles; its finding stays in the list with the same code, severity and resource, `restricted: true` and no message or evidence; the roll-up state and severity counts are unchanged.
- **NamespaceSelectorScope.** Table tests mirroring kgateway's discovery semantics (OR, empty list, invalid selector, last-wins env) for each supported version.
- **Metrics parsing.** Fixture `/metrics` payloads captured from kgateway 2.3 and 2.4 controllers after inducing a real xDS reject (a cluster with unknown TLS cipher suites, which Envoy NACKs). The contract test fails if a capture holds no reject, and asserts the reject's Gateway identity, its labels (`gateway_namespace`, `gateway_name`, `type_url` without the `type.googleapis.com/` prefix) and its parsed value.
- **Frontend.** Vitest and Testing Library for the health primitives, the theme tokens (contrast checked in both themes) and the pages against a mocked API. Required cases include:
  - switching installs never shows the old install's strip, tiles or Gateways;
  - stale data turns the strip and the Gateway rows unknown, and a stale install summary turns the cluster card, the install switcher entry and the header readiness icon unknown;
  - with fake timers, a fresh response ages past the threshold while identical responses keep arriving, and the page and the cluster card turn stale;
  - resources being processed: the strip is unknown and says how many resources are being processed;
  - the Gateway table's text filter round-trips through the URL;
  - a finding links to the YAML view only when its kind is listed in `/api/v1/meta`;
  - the `Shell` root applies `stylesOverride`.
- **Extension seam.**
  - In Go, a test-only extension registers an extra kind, detector, check (with its own code prefix) and authorizer; its findings and counts appear in API output, and the authorizer's denials take effect.
  - In the frontend, a test-only app registers a page and a theme; the page renders in the shell, and the test asserts that the contributed tokens (colour and typography) are applied to rendered components.
- **e2e** (kind, run against kgateway 2.3 and 2.4 in a matrix; `main` as a non-blocking job):
  1. Install kgateway and fixture resources, including deliberately broken ones and a namespace outside the selector.
  2. Install the chart.
  3. Check the API. The missing-target policy assertion is version-specific: on 2.3 and 2.4 the policy has no status and shows as not yet processed (003); `TargetNotFound` (002) is asserted only in the `main` job (§10).
  4. Run a Playwright smoke test of the landing page, the install page, the findings list and the YAML view, in both themes.
  - A second scenario runs two installs. Each uses the Helm release name `kgateway` in its own namespace, so the install IDs are `kgw-a.kgateway` and `kgw-b.kgateway`.

## 10. Supported versions

- kgateway **2.3.x** and **2.4.x** in CI; `main` is tracked as a non-blocking e2e job.
- Kinds are discovered per cluster, so kinds that exist only in some versions (for example `HTTPListenerPolicy` on 2.3 and 2.4) appear and disappear without code changes. The served set is resolved at startup and re-resolved every 60 s; a change (including a preferred-version change) raises a required KGWUI-008 per install asking for a restart (§5.4).
- **Status capabilities differ per version** (verified on the v2.3.x and v2.4.x release refs):
  - The policy reason `TargetNotFound` and the `StatusSummary` ancestor exist only on `main`. On 2.3 and 2.4 a policy whose target is missing gets no status at all, so the UI reports it as not yet processed (§5.2). It never claims a missing target it cannot see.
  - Dropped and replaced route rules appear as `Accepted=False` on 2.3 and as `kgateway.dev/Programmed=False` on 2.4 and `main`. Both are reported under KGWUI-002.

## 11. Security

**Trust model (D-013).** Phase 1 follows the Argo CD model: whoever can reach the UI may see everything it shows.
- The UI reads the cluster with its own ServiceAccount, so a viewer can see more than their own Kubernetes RBAC allows. This is stated, not hidden.
- **Secrets are never read.** The ClusterRole excludes them, and this is the whole meaning of "never handles secret material".
- **No field redaction.** Any field of any resource can hold a secret (an inline token in a policy, a literal env var in a Deployment), so redaction cannot be made reliable. Objects are served as stored, minus `managedFields`. Deployments are served like any other watched kind.
- **Exposure is the operator's choice.** Either a port-forward to the ClusterIP Service, or the optional HTTPRoute with authentication and authorisation in front of it (for example a kgateway TrafficPolicy on that route). The route stays off by default.
- **Documented prominently** in the README ("Security and trust model"), the chart README, `NOTES.txt` and the `httpRoute` values comment (§8).
- **Seam for later.** Every API handler goes through the `Authorizer` (§7.1). Phase 1 ships `AllowAll`. Per-user authentication and authorisation is a roadmap item that plugs in there, not a retrofit through every handler.

**Other properties.**
- The UI is read-only (D-001). The ClusterRole grants get/list/watch only.
- It never calls the controller admin server (`:9095`) or the Envoy admin API.
- The pod runs as non-root on a read-only root filesystem, with all capabilities dropped.

## 12. Cost

This phase generates no data-plane configuration. The cost falls on the control plane and the API server:
- **Watches.** One cluster-wide watch per registered served kind: about 20 kinds, plus Namespaces, and label-selected watches on Pods (controller only) and Deployments (controller and proxies). That is comparable to a single extra controller's list/watch load.
- **Availability tracking** adds no requests: it records the outcome of the informers' own List and Watch calls and the events they receive.
- **Memory.** The memory footprint scales with the number of Gateway API and kgateway objects in the cluster. Secrets and ConfigMaps are not cached.
- **Metrics scraping.** One `GET /metrics` per controller replica every 10 s, with an 8 MiB body cap and a per-pod timeout of min(interval / 2, 5 s) (§5.4).
- **No new data-plane objects or xDS churn.** The metric names the UI depends on are listed in one place (`pkg/ctrlmetrics`), so a rename upstream is a one-line change and shows up as KGWUI-008 until it is made.

## 13. Open items

1. **Shared `controllerName` across installs.** Can two kgateway installs share a `controllerName` with disjoint scopes, and how do they behave? Resolved (D-012): yes. Both installs ran with one GatewayClass and one `controllerName`, each reconciled only the Gateways in its own namespaces, and the GatewayClass did not flap. §4.3 is unchanged.
2. **Replica semantics of the controller metrics.** Is `kgateway_envoy_xds_rejects_active` per replica (and summed), and does every replica report the full `kgateway_resources_managed`? Resolved (D-012): yes. Only the replica serving the proxy's xDS stream reports the reject, and every replica reports the full `kgateway_resources_managed`, so rejects are summed and `resources_managed` takes the max.
3. **Upstream proposal** for an effective-scope endpoint (D-009 §7). It is filed during Phase 1, but it does not block the phase.

## 14. Platform neutrality (D-016)

Kubernetes is the product focus; this section only keeps a later standalone integration possible. The UI will later run on kgateway without Kubernetes workloads: **kube-lite** (local apiserver with CRDs and status, but no Pods, Deployments or Services; host-process controller and Envoy; `selfManaged` Gateways) and **file mode** (no apiserver, YAML files, status in controller memory, possibly embedded in the standalone binary). This section maps each constraint to today's code and says what changes, and when. Pointers are against `c2c52b8`.

### 14.1 Constraints

| # | Constraint | Our reading |
|---|---|---|
| R1 | The data source is pluggable; core logic needs no `corev1`/`appsv1` objects and no live apiserver. | Agree. The interface waits for a second implementation (§14.4); no new coupling meanwhile (H8). |
| R2 | An install is a controller instance: opaque ID, version/edition, controller names, metrics endpoints, readiness. Statically configured installs must be possible. | Agree. Opaque ID now (H1). A neutral `Install` comes later, as a separate provider seam; today's `Detector` stays as the Kubernetes provider's adapter. The extension API is excluded from the first release's compatibility promise until the seam milestone (H11). |
| R3 | Readiness comes from a provider signal. A self-managed Gateway gets no "proxy deployment not found". "Not observed" is distinct from "failing". | Agree, and it is a Kubernetes bug today (H5). Refinement: "not observed" is **info** and does not drive the roll-up to unknown. Otherwise every standalone install is unknown forever. D-006 still holds, because an unobserved signal is not a contradicting one. |
| R4 | Metrics are addressed by URL; no "no ready controller pod" dead end. | Agree. Part of the neutral `Install` (§14.4). |
| R5 | Status may come from the controller. Missing UID, `generation` or `observedGeneration` must not break freshness or keys. | Agree, with a refinement. Freshness is a provider contract: either `generation`/`observedGeneration`, or the provider confirms that status matches the current spec revision. A provider that does neither cannot reach green, because status kept after a file edit could be green for the old spec (§5.2). Keys and grace need a provider revision or incarnation, not just ref-based keys (§14.4). |
| R6 | Namespaces and scope are optional; a single implicit namespace works; nothing is dropped silently without Namespace objects. | Partly. kube-lite always has Namespace objects, because the apiserver creates `default`. "Scope not applicable" is H2. Selector scope with a missing Namespace object still drops objects (`pkg/install/scope.go:50`). That rule only applies where selectors exist, so it is deferred (§14.4). |
| R7 | Capabilities, not mode strings; opaque IDs in API and SPA URLs; a vocabulary for platform copy. | Agree (H1–H4). Only capabilities the SPA reads are defined, because unread capabilities are untested. |
| R8 | Read-only; no server-side-apply assumptions. | Already holds: no write calls in non-test code; `managedFields` is only stripped (`pkg/api/handlers.go:252`). |
| R9 | `pkg/...` is embeddable; no globals or flags in core; providers injected; the SPA embed is reusable. | Mostly holds: no `init()`, no `os.Getenv`, flags only in `cmd/`. The SPA embed is in `internal/webui`, but `Options.UI` already takes any `fs.FS`, and exporting the embed later is additive (§14.4). `app.Build` cannot take a `Snapshotter` or metrics `Source` yet (§14.4). |
| R10 | AuthZ stays a seam. | Already holds: `Authorize(r *http.Request, ref objects.Ref) bool` (`pkg/api/authz.go:12`). |
| R11 | Register `Backend` and `GatewayParameters`. | `GatewayParameters` now (H5 needs it). `Backend` in the Phase 2 design: a kind with no assignment rule is watched and counted but never attributed to an install (`ByCategory`), and attribution needs route references. |

### 14.2 Where Kubernetes leaks in today

| Area | Today | Seam that exists |
|---|---|---|
| Data input | `cache.Snapshot` carries typed `Namespaces`, `Pods`, `Deployments` (`pkg/cache/cache.go:47-57`) | `cache.Snapshotter` (`cache.go:60`) is the engine's only input |
| Wiring | `app.Build` always builds `cache.New`, `kinds.Resolve` and `ctrlmetrics.NewCollector` (`pkg/app/app.go:73-118`) | `engine.Config` accepts any `Snapshotter` and `Source` |
| Install | ID = Deployment `ns/name` (`pkg/install/install.go:34`); `Install` carries Deployment and Pods (`:17-31`); `Detector.Detect(*appsv1.Deployment)` (`:49`) | `extension.Registry` Detectors |
| Readiness | Controller: replicas (`pkg/health/checks.go:131-141`). Proxy: Deployment lookup, KGWUI-006 warning after grace (`:419-461`) | `extension.Registry` Checks (append-only) |
| Metrics | Pod IP or `pods/proxy` (`pkg/ctrlmetrics/scrape.go:22-73, 113-162`) | `ctrlmetrics.Source` (`:91`) |
| Scope | Pod env or Deployment template (`pkg/install/scope.go:99-140`) | `ScopeProvider`, `ScopeVerifier` |
| Freshness keys | Grace keyed on `{uid, generation}` (`pkg/engine/engine.go:76-120`); Pod/Deployment dedupe by UID (`pkg/cache/cache.go:285-302`). Empty UIDs collide. | — |
| API | Paths `/installs/{ns}/{name}` (`pkg/api/handlers.go:39-41,167`); required `namespace`, `name`, `controller{ready,desired}`, `scope{selectors,watched,total}`; `/meta` hard-codes `apps/Deployment` (`pkg/api/types.go:54`) | Handlers depend on `engine.Model` |
| SPA | Routes `/i/:ns/:name` and the ID rebuilt by joining params in four places (`shell/Shell.tsx:54-60`, `shell/extension.ts:70`, `shell/Header.tsx:110`, `shell/DocumentTitle.tsx:6`); IDs not URL-encoded; `scope` and `controller` are dereferenced unguarded (`pages/install/CountTiles.tsx:80`, `pages/ClusterLanding.tsx:140`); "Waiting for the cluster" in six files; the RootRedirect empty state explains Deployment labels | `ShellExtension`, `CorePages` |
| Kinds | Served versions via the discovery API (`pkg/kinds/kinds.go:76-107`); no `Backend`/`GatewayParameters` (`pkg/kinds/upstream.go:11-27`) | `extension.WithKinds` |

### 14.3 Hedges for Phase 1

Each hedge is small and lands before the first release, which freezes API v1 and the SPA URLs. H5 is also a bug fix in Kubernetes mode.

| # | Behaviour | Constraint |
|---|---|---|
| H1 | **Opaque install ID.** One path segment everywhere: `GET /api/v1/installs/{id}/…` and SPA `/i/:id/…`. By contract an ID uses only URL-unreserved characters (`A-Z a-z 0-9 . _ ~ -`), and an ID outside that set, or equal to `.` or `..` (which URL normalisation would remove), is invalid. Installs are discovered at runtime, so an install with an invalid provider ID is excluded, an error naming the detector and the raw ID is logged, and `/readyz` reports not ready with that error, so a broken downstream detector is loud. The Kubernetes detector cannot produce an invalid ID. An invalid `--install` value fails at startup. The SPA still encodes it and never parses it. The Kubernetes ID is `<namespace>.<name>`: namespace names cannot contain dots and Deployment names are DNS subdomains, so it is unique and in the set. Clients must not rely on that structure. `--install` accepts the ID or `namespace/name`. | R2, R7 |
| H2 | **Platform fields are optional; absent means not applicable.** The install DTO gets a neutral `readiness: {state, ready?, desired?}`, where state is ready, not-ready, unknown or not-observed. `namespace`, `name` and `scope` become optional, plus a `displayName`. The SPA renders the scope tile and replica text only when present and never crashes on absence. | R2, R3, R6 |
| H3 | **`/meta` reports `platform` and `capabilities`.** Platform is a display string such as "Kubernetes". Capabilities are a string set; Phase 1 defines only the ones the SPA reads: `scope` (namespace scope tile and filters) and `workloads` (Deployment links and replica wording, in the UI and in finding messages). `apps/Deployment` stays in `kinds` only under `workloads`. A `useCapabilities()` hook in the shell. | R7 |
| H4 | **SPA vocabulary.** One module holds the platform copy: "Waiting for the cluster", the cluster landing title, the no-install empty state (how an install is found, the `--install` hint) and "Copy namespace/name". The Kubernetes vocabulary is the default; `ShellExtension` gets an optional `vocabulary` override. Strings go through the wording pass. | R7 |
| H5 | **Self-managed proxies.** Register `GatewayParameters` (`gateway.kgateway.dev`). The UI follows kgateway's own resolution: class parameters, then Gateway parameters, where a selfManaged layer wins and a later `kube` layer does not undo it (`pkg/deployer/merge.go:12-28`, `pkg/kgateway/deployer/gateway_parameters.go:224-306`, kgateway `556b4ce`). Outcomes: **self-managed** when every referenced GatewayParameters resolves and the merge is selfManaged; **unresolved** when a referenced one is missing or unreadable, which keeps today's behaviour (kgateway also sets `Accepted=False`, reason `InvalidParameters`); otherwise **managed**. When RBAC denies GatewayParameters, it is like any other denied kind: KGWUI-008 and the install unknown, never KGWUI-010. Only one thing changes: for a self-managed Gateway, the "no proxy Deployment found" case becomes info finding KGWUI-010 "Self-managed proxy: the UI cannot see its readiness". A proxy Deployment that exists but is not ready is still KGWUI-006, and `Programmed=False`, xDS rejects and every other check are unchanged (D-006). The resolution sits behind a pluggable interface, because it mirrors controller logic; the upstream proposal asks the controller to report it (§14.6). | R3, R11 |
| H8 | **Import boundary test.** A Go test fails when a non-test file outside today's per-file allowlist imports `k8s.io/api/core/v1`, `apps/v1`, `k8s.io/client-go` or `pkg/kinds`' discovery client. The allowlist names files, not packages, and only shrinks; a file that must be added needs a reviewed reason. The test cannot see semantic assumptions, such as a new Pod-shaped field, so the review checklist asks for them. | R1 |
| H9 | **Upstream proposal** (plan Task 23) adds two asks: `controllerName` in the admin `/version` response, and connected-proxy state per Gateway. `Programmed` semantics for self-managed Gateways are left to the standalone project's planned issue, so only one is filed (§14.8). Edition is not an upstream concern: a downstream build reports it. No Kubernetes Pod introspection required. | R2, R3 |
| H10 | **Docs.** The README states that Kubernetes is today's only provider. The roadmap entries for Phases 3, 4, 7, 9 and 10 carry the notes in §14.5. | — |
| H11 | **Compatibility policy.** The README and `pkg/extension` doc state that API v1 (paths and JSON) is stable from the first release. The Go extension API (`Detector`, `Install`, `ScopeProvider`, `Registry`) may change until the extension-seam milestone. When the provider seam arrives, the Kubernetes `Detector` keeps working as that provider's adapter. | R2, R9 |

Acceptance, at the API and SPA boundary:

| Hedge | Case | Expected |
|---|---|---|
| H1 | Install ID `ns.name`, and a test provider ID with `~` and `_` | Navigation, reload, deep links and every API call resolve the install. The ID is never split. |
| H1 | Provider ID containing `/` or `%`, or equal to `.` or `..` | The install is left out of `/installs`, an error naming the detector and the raw ID is logged, and `/readyz` returns 503 with that error. An invalid `--install` value fails at startup. |
| H2 | Install with no `scope`, no `namespace`/`name`, `readiness.state=not-observed` | `/installs` and the overview header render; no scope tile, no replica text, no crash. |
| H3 | `capabilities` without `workloads` | No Deployment links and no replica wording, in the UI or in finding messages; `apps/Deployment` absent from `kinds`. |
| H4 | Downstream `vocabulary` override | Overridden strings show; others fall back to the defaults. |
| H5 | Class selfManaged, Gateway without parameters, no proxy Deployment | KGWUI-010 info; the roll-up is unaffected. |
| H5 | Class selfManaged, Gateway parameters `kube` | Self-managed (KGWUI-010). |
| H5 | Class parameters `kube`, Gateway parameters selfManaged | Self-managed (KGWUI-010). |
| H5 | Class selfManaged, Gateway references missing GatewayParameters | Unresolved: KGWUI-006 as today, plus the controller's `Accepted=False`. |
| H5 | Self-managed Gateway whose matching proxy Deployment exists and is not ready | KGWUI-006, not KGWUI-010. |
| H5 | Self-managed Gateway with `Programmed=False` or an active xDS reject | Same findings and roll-up as a managed Gateway. |
| H8 | A new core file importing `k8s.io/api/core/v1` | The test fails. |

Placement: a new planned sub-phase before packaging (§14.7 Q1). H1–H4 touch about 25 files across the API and SPA, so the plan amendment goes through review and approval first.

### 14.4 Refactors that wait until standalone is real

| Refactor | Trigger | Shape |
|---|---|---|
| Provider interface | The kube-lite PoC needs installs | A `Provider` yields objects, installs and access; the engine stops seeing typed objects. The Kubernetes provider owns informers, discovery, Pod/Deployment detection and D-014. |
| Neutral `Install` and `Detector` | Same trigger, and **before the extension-seam milestone** (after Phase 2), so the seam proof uses the final signature | `Install{ID, DisplayName, Version, Edition, ControllerNames, MetricsTargets []URL, Readiness}`; a static install from flags or config; `Detector` takes provider data, not `*appsv1.Deployment`. |
| Metrics by URL | Same | `Scraper.Scrape(ctx, url)`; the Kubernetes provider turns Pods into URLs (or `pods/proxy`). |
| Injection in `app.Build` | Embedding | `Build` takes a `Provider` and optional `Source`; `Run` keeps the kubeconfig and in-cluster path. |
| Kinds without discovery | File mode | The provider reports served kinds; the discovery client is a Kubernetes detail. |
| Object revision and incarnation | File mode | Met by Kubernetes metadata the standalone binary synthesises (§14.8): `generation` per spec change, a new `uid` per create, `resourceVersion` as the store revision. Today's `{uid, generation}` keys and freshness logic then work unchanged; the provider must reject objects without them. |
| Static install definition | kube-lite | An install read from a definition file (proposed `--install-file`): ID, controller names, version source, metrics and admin URLs. Controller liveness probes those URLs. |
| Exported SPA embed | An embedding binary | Export the embed as a public package; additive. |
| Selector scope with missing Namespace objects | A provider with selectors but no Namespace objects | Report "membership unknown" instead of out-of-scope. |
| D-009 §1 for file mode | File-mode provider | Objects and status come from the controller (D-016 §5). |

### 14.5 Notes for later phases

- **Phase 2:** register `Backend` with its `Accepted` condition and an assignment rule.
- **Phase 3:** "pod health" is "process health"; restarts and readiness come from the provider.
- **Phase 4:** must work with `Backend` and Envoy cluster health alone; Services and EndpointSlices are optional.
- **Phase 7:** certificates may come from files, not Secrets.
- **Phase 9:** easier standalone, because the admin server (9095) and Envoy admin are local.
- **Phase 10:** SubjectAccessReview and impersonation are one `Authorizer`, behind a `kubernetes-rbac` capability. A neutral authorisation story is a Phase 10 design question.

### 14.6 What a standalone status source must provide

These are requirements for the standalone project, not a design.
- **Objects:** each Gateway API and kgateway object with spec, status and conditions, plus a stable key (group, kind, namespace, name). A UID and a generation are optional.
- **List and change notification:** a full list plus ordered changes (watch, stream or polling with a revision), with a way to tell "source down" from "no changes".
- **Freshness:** `generation`/`observedGeneration` with Kubernetes semantics (generation changes on spec edits only), a `uid` that changes on delete and recreate, and `resourceVersion` as a monotonic revision. Agreed with the standalone project (§14.8).
- **Install identity:** controller names, version, edition and an ID that is stable across restarts.
- **Liveness:** controller liveness, and per Gateway the proxies connected over xDS and whether they accept the config.
- **Metrics:** URLs for the controller's Prometheus endpoint (today `localhost:9092`, hard-coded in kgateway) and Envoy admin.
- **Scope:** whether namespace scoping exists and, if so, the effective namespaces.

### 14.7 Decisions (user, 2026-10-20)

1. **Placement:** a new planned sub-phase 1.3 "platform-neutral contract" (H1–H5, H8, H11). Packaging, CI and e2e become sub-phase 1.4, which also carries H9 and H10.
2. **Self-managed severity:** info (KGWUI-010).
3. **`Backend`:** registered in the Phase 2 design.

### 14.8 Standalone project's answer (2026-10-20)

The standalone project checked §14.6 against its design. Its roadmap: kube-lite PoC with the stock controller, then a single binary that imports kgateway as a Go module, as downstream builds do.

| Topic | Agreed |
|---|---|
| Objects, list and watch | kube-lite: apiserver objects. File mode: Kubernetes list/watch semantics with a monotonic revision; unreachable shows as a transport error or missed heartbeat. |
| Freshness | File mode synthesises Kubernetes metadata (`generation`, `uid`, `resourceVersion`) instead of new fields. Accepted: it keeps D-014-style freshness unchanged. |
| Install identity | kube-lite: a definition file the UI reads by flag; no fake Deployment, because it would show 0/1 replicas. File mode: an identity endpoint owned by the binary, which also carries edition. |
| Controller liveness | kube-lite: the UI probes the metrics or admin URL. File mode: the identity endpoint. |
| Proxy readiness | kube-lite: not observable (stock controller, `Programmed=True` regardless of Envoy), so KGWUI-010 is right. File mode: per-Gateway connected proxies and ACK/NACK through xDS callbacks. |
| Metrics | kube-lite: one controller per host on 9092. File mode: set in code. A configurable port upstream is nice to have. |
| Scope | Always "all"; Namespace objects exist (apiserver-created, or synthesised in file mode). |
| Embedding | The single binary will likely embed `pkg/...` and serve the SPA from its own `fs.FS`. `pkg/...` stays free of globals and in-cluster assumptions (R9, H8). |
| Authorisation | No identity story yet; localhost-bound. A later one plugs into `Authorizer`. |

Effect here: H9's asks shrink (no edition, `Programmed` semantics filed by the standalone project), and §14.4's revision row is met by synthesised metadata. No Phase 1 task changes.

