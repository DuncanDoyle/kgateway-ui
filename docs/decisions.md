# Decision log

Append-only. Newest at the bottom. Supersede, don't edit.

## D-001 — The UI is read-only (2026-10-15, roadmap)
**Context:** Gateway configuration is typically managed through GitOps. CRUD-centric gateway UIs drift from Git and compete with the source of truth.
**Decision:** The UI never mutates cluster state. At most, a later feature may output YAML for the user to apply through their own pipeline.
**Alternatives:** A CRUD UI — rejected because it conflicts with GitOps and widens the RBAC and security surface.
**Consequences:** The backend needs only get/list/watch RBAC. Write-adjacent ideas (e.g. "what-if" YAML analysis) are deferred.

## D-002 — A Go backend with an embedded SPA, in one binary, for in-cluster and local use (2026-10-15, roadmap)
**Context:** A browser-only SPA cannot reach the controller admin server or proxy metrics. The API server offers no CORS support or browser login. Search, analyzers and effective-policy views over 1000s of routes need watch caches and indexes.
**Decision:** A single Go binary serves the API and the embedded React + TypeScript SPA. In-cluster it runs as a Helm-installed pod with a read-only ServiceAccount; locally it runs with the user's kubeconfig.
**Alternatives:**
- A pure SPA, through `kubectl proxy` or a token in the browser — rejected for the reachability, auth and scale reasons above.
- In-cluster only — rejected because of the slower dev loop.
- A Headlamp plugin — rejected because it binds the UI to a Kubernetes resource-viewer model; it remains a possible second surface (deferred).
**Consequences:** We own a backend, an image and a Helm chart from Phase 1. Semantics (health rules, analyzers) live in testable Go packages that a future CLI could reuse.

## D-003 — Extension through libraries and a downstream build (2026-10-15, roadmap)
**Context:** Downstream distributions must be able to extend the UI (extra policy kinds, pages, edition detection) without forking or copying code. This repo is public; downstream code is private.
**Decision:**
- This repo publishes a Go module, which holds the registries for resource kinds, health checks, analyzers and API routes, and a frontend package, which holds the app shell plus extension points for navigation, pages, per-kind renderers and health cards.
- A downstream repo has its own `main` and frontend entry point. It imports both, registers its extras, and builds its own image.
- The UI is descriptor-driven where practical, i.e. the backend describes kinds and features and the frontend renders them generically, so that most downstream additions need only Go code.
**Alternatives:**
- Runtime plugins (discoverable extension services plus remotely loaded JS modules) — rejected because versioning, security and compatibility testing are too complex for a single known downstream.
- One codebase with an edition flag — rejected because downstream code cannot live in a public repo.
**Consequences:**
- Public package boundaries (Go `pkg/`, the npm package exports) are an API and must be versioned.
- The extension seam is validated by a throwaway downstream build after Phase 2.
- This mirrors the functional-options and plugin-registration pattern that kgateway downstreams already use.

## D-004 — Platform operator is the primary persona (2026-10-15, roadmap)
**Context:** The persona determines the landing page, whether views are scoped, and how RBAC-aware the backend must be.
**Decision:** The UI is optimised for platform operators who own the gateways and need cluster-wide views. It acts with its own read-only ServiceAccount (in-cluster) or the user's kubeconfig (local).
**Alternatives:**
- Operators and app teams from day one — rejected because it pulls OIDC and impersonation into Phase 1.
- App developer first — rejected; the operator need is broader and more urgent.
**Consequences:** In-cluster access control relies on who can reach the Service until Phase 10 adds OIDC, impersonation and namespace-scoped views.

## D-005 — Present Gateway API semantics; never re-implement translation (2026-10-15, roadmap)
**Context:** Users think in Gateway API and policy resources, not Envoy xDS. Gateway API resources and their status already describe hostnames, paths and attachments. Re-implementing kgateway translation would drift from the controller.
**Decision:**
- Gateway API resources and their status conditions are the primary data source and the vocabulary of the UI.
- A route that is Accepted and Programmed is treated as active as specified. Divergence in the data plane is a kgateway bug, which the UI surfaces.
- xDS and Envoy config are used only where Gateway API cannot answer the question (filter order, rejected config), and are always mapped back to the originating resources.
- kgateway translation logic (including policy merge semantics) is never re-implemented.
- Separately: because the repo is public, vendor and commercial edition names are not used in code, file names or repo text; they are called "downstream distributions".
**Alternatives:** Deriving all routing answers from xDS — rejected because it makes Envoy the vocabulary and hides the Gateway API model users reason in.
**Consequences:**
- Phase 5 (effective policy) must build on policy ancestor status rather than on its own merge logic.
- Phase 6 (route tester) may evaluate spec-defined Gateway API precedence, but must decide how to handle kgateway-specific behaviour (delegation, rule replacement).

## D-006 — Contradicting signals are shown, not verified (2026-10-16, roadmap)
**Context:** D-005 said a data-plane divergence "is a kgateway bug, which the UI surfaces rather than hides". Read literally, that promises the UI verifies the data plane against the spec. Doing that requires predicting the expected Envoy config, which is the translation logic D-005 rules out.
**Decision:** This narrows the wording of D-005; the rest of D-005 stands.
- A route that is Accepted and Programmed is treated as active as specified, and the UI does not verify the data plane against the spec.
- The UI shows every signal that contradicts that status and never reports green while one of them is active. These signals are xDS rejects per gateway, dropped or replaced rules, Gateway `Programmed=False`, proxy readiness, and (Phase 9) differences between the controller's xDS snapshot and the config Envoy accepted.
**Alternatives:** Verifying the data plane against the spec — rejected because it requires re-implementing translation (D-005).
**Consequences:**
- A translation that is wrong but was accepted by Envoy stays invisible to the UI.
- Health roll-ups (Phase 1 banner, Phase 3 overview) must combine all of these signals, and no single green status can mask another.

## D-007 — Metrics backends are PromQL-first behind an interface; OpenTelemetry covers logs and traces (2026-10-16, roadmap)
**Context:** Principle 6 named Prometheus. Users run a range of metrics platforms, and OpenTelemetry was suggested as a more generic term.
**Decision:**
- The UI works without any external metrics backend. When one is configured, it queries it through a backend interface.
- The first implementation of that interface targets PromQL-compatible backends (Prometheus, Thanos, Mimir, Cortex, VictoriaMetrics). This fits because kgateway and Envoy expose Prometheus-format metrics, whether those metrics reach the store by scraping or through an OpenTelemetry Collector.
- Downstreams can implement the interface for other backends.
- In Phase 8, access-log and trace exploration assumes these are exported via OpenTelemetry.
**Alternatives:**
- Naming OpenTelemetry as the metrics source — rejected because OpenTelemetry defines production and transport (SDKs, OTLP, Collector), not a query API, so the UI cannot read metrics "from OTel".
- Naming Prometheus only — rejected as too narrow.
**Consequences:** The Phase 8 design defines the metrics backend interface and its extension point.

## D-008 — The UI is rebrandable by downstreams through theme tokens (2026-10-16, roadmap)
**Context:** D-003 lets downstream distributions extend the UI. A downstream build also needs to look like its own product (logo, palette, product name, typography) without forking styles or components. The public repo cannot use proprietary component libraries.
**Decision:**
- All visual identity flows from theme tokens: colours, typography, logo, product name and status colours.
- The default theme carries the kgateway identity.
- A downstream build supplies its own token set (and may add themes) through the frontend extension package. Core components never hard-code brand values.
- The UI builds on an open-source component library, chosen in the Phase 1 design. A downstream may use its own component library for the pages it adds.
**Alternatives:** Downstream forks the stylesheet or overrides CSS selectors — rejected as copy/paste in disguise, which breaks on every upstream change.
**Consequences:**
- The token set is part of the public extension API (D-003) and is versioned with it.
- Phase 1 must deliver the token system and at least one override path, tested.

## D-009 — Own watches for resources; pluggable, verified scope per controller install (2026-10-17, phase 1)
**Context:**
- The kgateway controller does not watch the whole cluster. `discoveryNamespaceSelectors` (env `KGW_DISCOVERY_NAMESPACE_SELECTORS`; a JSON list of label selectors, OR'ed, where empty means all) limits every namespaced kind it watches.
- Gateways count only if their GatewayClass `controllerName` matches.
- A resource outside that scope gets no status from kgateway, and previously written status is not cleared when a namespace is deselected. So a UI that ignores scope shows false errors and stale data.
- As of kgateway main `8a498c6406` (and 2.3/2.4), the controller exposes no effective-scope API. The admin server has no settings endpoint. `/snapshots/krt` is an internal, unbounded debug dump (it includes ConfigMap data) and is localhost-bound. `kgateway_resources_managed{namespace,parent,resource}` counts the namespace-filtered resources but is a metric, not a contract.
- Label filters for Secrets, ConfigMaps and Services are proposed upstream (kgateway#14530, #14801) and not merged.
- A cluster can run multiple kgateway installs with different scopes and versions.

Investigated independently by Codex and by a Claude subagent against kgateway main `8a498c6406`, `v2.4.x` and `v2.3.x`; the findings agreed.

**Decision:**
1. **Resources come from our own Kubernetes watches, permanently.** Informers provide change notification, a local cache and server-side indexing. We will not ask the controller to serve resources or notifications.
2. **Scope is pluggable.**
   - A `ScopeProvider` answers, per controller install, which namespaces and which `controllerName`s are in scope.
   - v1 is `NamespaceSelectorScope`: it reads `KGW_DISCOVERY_NAMESPACE_SELECTORS` from the running controller Pod spec (duplicate env entries resolve last-wins), applies kgateway's semantics (OR'ed selectors, empty means all, cluster-scoped objects always pass), and tracks namespace label changes live.
   - A later `ControllerScope` asks the controller once it exposes its effective scope.
   - Downstreams can register their own provider (D-003).
3. **Scope is verified.**
   - A pluggable `ScopeVerifier` checks the UI's view against the controller.
   - v1 compares per-namespace, per-kind counts with `kgateway_resources_managed`. A mismatch is a health warning ("UI scope differs from the controller"), never a silent correction.
   - The check is soft, because metric names can change upstream.
4. **Filters on referenced objects are not mirrored** (Secret/ConfigMap/Service label selectors, ServiceEntry/WorkloadEntry exclusions). The UI never resolves references itself; it shows `ResolvedRefs` as kgateway reports it (D-005).
5. **Scope is per controller install and first-class.**
   - The backend discovers all installs. Each one is a scope: namespace selector, controller and class names, version, edition and metrics endpoint.
   - One shared cache serves all scopes.
   - The API and URLs carry the install (`/api/v1/installs/{ns}/{name}/…`).
   - The shell shows an install switcher when there is more than one install, and the home page shows a health line per install.
   - A pinned mode (`--install`, Helm value) restricts the UI to one install for environments where installs isolate teams.
   - Routes are assigned to an install by the `controllerName` of the Gateway they attach to.
6. **Unwatched-namespace diagnostic.**
   - Route parentRefs are watched cluster-wide.
   - A route in a namespace outside its target Gateway's install scope becomes a warning finding ("targets Gateway X, but its namespace is not watched by the controller").
   - Such routes never count towards totals or health.
7. **Upstream proposal.** Propose a small read-only "effective scope" endpoint in kgateway: controller identity, version, `controllerName`s, and the effective namespace scope. It must be reachable without opening the admin server's pprof and log-level handlers.

**Alternatives:**
- Watching the whole cluster unfiltered — rejected: false errors and stale status.
- Using `/snapshots/krt` as the scope source — rejected: an unstable internal format, an unbounded dump that includes ConfigMap data, and localhost-bound.
- Deriving scope from status `controllerName` — rejected: out-of-scope and not-yet-reconciled resources look identical, and stale status survives deselection.
- Using `resources_managed` as the primary scope source — rejected as primary because it is a metric, not a contract; it is kept as the verifier.
- Fetching resources from the controller via a new introspection API — rejected: it needs a streaming and notification API upstream, which is a large ask with little gain over watches.
- Mirroring all controller filters — rejected: referenced-object filters are already reflected in status.

**Consequences:**
- `NamespaceSelectorScope` duplicates a small, stable piece of controller logic. It is pinned by tests per supported kgateway version, and the verifier exposes drift at runtime.
- The backend needs cluster-wide read access to the watched kinds and to Pods (for the controller env), plus `services/proxy` on the controller metrics Service for local mode.
- Open item for the Phase 1 design, to verify on a cluster: whether two installs can share a `controllerName` with disjoint namespace selectors. If they can, install assignment needs another tie-breaker.

## D-010 — Frontend foundation: React 19, emotion and open primitives (2026-10-17, phase 1)
**Context:**
- The OSS shell must host pages contributed by downstream builds (D-003).
- A known downstream's component library is built on React 19, emotion, `@rc-component/*`, `lucide-react`, `react-router` and SWR, with Vite and Vitest.
- Proprietary libraries cannot be used in this public repo (D-008).
**Decision:**
- React 19, TypeScript and Vite.
- `@emotion/react` / `@emotion/styled`, `react-router`, `lucide-react` and SWR.
- TanStack Table with virtualisation for dense tables.
- Vitest, Testing Library, Playwright and Biome for testing, linting and formatting.
- Our own small component set, built on open primitives.
**Alternatives:**
- MUI — rejected: heavy, and its distinctive look fights rebranding; downstream pages would show two visual languages.
- Radix/shadcn + Tailwind — rejected: a second styling system next to emotion for downstream pages.
**Consequences:**
- Downstream pages built on that library slot into the shell without a second styling system or conflicting React/router versions.
- The peer versions (React, emotion, router) become part of the extension contract.

## D-011 — Install page layout A, cluster landing C, "Control room" visual direction (2026-10-17, phase 1)
**Context:** The overview must answer "is it healthy, and if not, why" at a glance, scale to many findings, and handle clusters with several installs. Options were compared as wireframes in the visual companion.
**Decision:**
- **Install page (layout A):** a full-width health strip, then the top findings, count tiles and the Gateway table.
- **Cluster landing (layout C):** one card per install, shown only when there is more than one install and the UI is not pinned.
- **Visual direction "Control room":**
  - The dark theme is the default, built on the logo ink and logo blue; a light theme keeps the ink header band.
  - Figtree for UI text; JetBrains Mono for resource names, with the namespace dimmed.
  - Health states are always icon + colour + text.
**Alternatives:**
- Layout B, the inventory with a findings rail — rejected: the rail truncates findings and does not scale to many of them.
- Layout C as the only landing page — rejected: an extra click in the common single-install case.
- The "kgateway native" light-first direction — not chosen; the user preferred an operations-console feel.
**Consequences:**
- The theme tokens must support a dark-first default.
- Additional themes (for example Solarized) arrive later through the same tokens.

## D-012 — Multi-install and metric replica behaviour, observed (2026-10-19, phase 1)
**Context:** Design §13 items 1–2; experiment on kind with kgateway v2.4.7 (Gateway API v1.6.1), two installs (`kgw-a/kgateway`, `kgw-b/kgateway`) with disjoint `discoveryNamespaceSelectors` (`team=a`, `team=b`), both on the default `controllerName` `kgateway.dev/kgateway`. The metrics capture and the observedGeneration check were repeated on v2.3.11 (Gateway API v1.5.1, one install, two replicas).
**Decision:**
- **Shared `controllerName` works.** Both `helm install`s succeeded with no cluster-scoped conflict. There is one GatewayClass `kgateway` (`spec.controllerName: kgateway.dev/kgateway`), with a single `Accepted=True` condition ("GatewayClass accepted by kgateway controller", `observedGeneration: 1`). Its `resourceVersion` stayed at `683` across a 30 s wait and across the later scale-up, so there is no flapping. Both installs ran `KGW_DISCOVERY_NAMESPACE_SELECTORS` for their own team.
- **Gateways are reconciled only by the install whose selector matches.** `kubectl get gateway -A -o wide` shows `app-a/gw` and `app-b/gw` both `PROGRAMMED True`. Each namespace got its own `gw` proxy Deployment (`1/1`). The install in `kgw-a` logged nothing about `app-b`, and the install in `kgw-b` logged nothing about `app-a` (0 matching lines each). Proxy `node_id` was `gw-...-9pfwn.app-b` on install B.
- **Controller Pod labels** (identical on v2.4.7 and v2.3.11): `app.kubernetes.io/component=controller,app.kubernetes.io/instance=kgateway,app.kubernetes.io/name=kgateway,kgateway=kgateway,pod-template-hash=...`. Deployment name is `kgateway` in each install namespace (release name `kgateway`).
- **Metric replica semantics.** With `kgw-a/kgateway` at 2 replicas, both replicas report the same `kgateway_resources_managed` series (krt runs on every replica):
  ```
  kgateway_resources_managed{namespace="app-a",parent="",resource="BackendConfigPolicy"} 1
  kgateway_resources_managed{namespace="app-a",parent="gw",resource="Gateway"} 1
  kgateway_resources_managed{namespace="app-a",parent="gw",resource="HTTPRoute"} 1
  ```
  Only the replica that serves the proxy's xDS stream reports the reject, the other has no `envoy_xds_rejects_*` series at all (the series appears only after a reject). This is the same on v2.4.7 and v2.3.11:
  ```
  kgateway_envoy_xds_rejects_active{gateway_name="gw",gateway_namespace="app-a",type_url="envoy.config.cluster.v3.Cluster"} 1
  kgateway_envoy_xds_rejects_total{gateway_name="gw",gateway_namespace="app-a",type_url="envoy.config.cluster.v3.Cluster"} 1
  ```
  So the UI sums rejects over replicas and takes the max of `resources_managed`. A Gateway with no routes or policies shows only its own `resource="Gateway"` series, which is why the captures were taken after the routes were added.
- **observedGeneration is set** (v2.4.7 and v2.3.11): `kubectl get gateway -n app-a gw -o jsonpath='{.metadata.generation} {.status.conditions[*].observedGeneration}'` printed `1 1 1 1` (Accepted, Programmed and ResolvedRefs all at generation 1). On v2.4.7 the listener conditions (Accepted, Conflicted, ResolvedRefs, Programmed) were also at 1.
- **Reject trigger.** The invalid-cipher-suites `BackendConfigPolicy` produced the reject on both versions within seconds of the proxy being connected. Captures are in `pkg/ctrlmetrics/testdata/v2.4.7.txt` and `v2.3.11.txt`.

Install assignment keeps design §4.3 (controllerName ∩ namespace scope; ambiguity finding when >1). Metrics: rejects summed over replicas; resources_managed max over replicas.
**Alternatives:** n/a (observation).
**Consequences:** No change to design §4.3. Because both installs share one GatewayClass and one `controllerName`, `controllerName` alone cannot tell installs apart, so the namespace scope must. The GatewayClass status is not per install and cannot be used for per-install health. Task 8's contract test can rely on a reject series being present only after a reject, so its absence must read as zero.

## D-013 — Trust model: whoever reaches the UI may see everything it shows (2026-10-18, phase 1)
**Context:** The UI reads the cluster with its own ServiceAccount and has no authentication in Phase 1. Anyone who reaches it sees what that ServiceAccount can read, which can exceed their own Kubernetes RBAC. Any field of any resource can hold a secret (an inline token in an AI `Backend`, a literal env var in a Deployment), so field-level redaction cannot be made reliable.
**Decision:**
- Phase 1 follows the Argo CD model: a user who can access the UI is allowed to view everything the UI can show.
- The UI never reads Secrets; the ClusterRole excludes them. This is what "never handles secret material" means; no other field is redacted.
- Exposure stays the operator's choice: port-forward to the ClusterIP Service, or the optional HTTPRoute with authentication and authorisation in front of it (for example a kgateway TrafficPolicy on that route).
- The trust model is documented prominently in the README, the chart README/NOTES and the `httpRoute` values, and later in the docs.
- `pkg/api` gets an `Authorizer` seam that decides per request which resources and findings a caller may see. Phase 1 ships an allow-all implementation.
**Alternatives:**
- Per-user authorisation now (OIDC login plus SubjectAccessReview against a shared cache, like Kiali) — deferred: identity integration differs per cluster (OIDC, cloud IAM, none), and partial visibility needs its own health roll-up design.
- Port-forward only, no HTTPRoute — rejected by the user: operators can secure the route with the gateway they already run.
- Hiding Deployments or redacting fields — rejected: secrets can sit in any field, so it suggests a guarantee the UI cannot give.
**Consequences:**
- Per-user AuthN/AuthZ becomes a roadmap item that plugs into the `Authorizer` seam rather than a retrofit through every handler.
- Finding links work for every watched kind, Deployments included.
- Health stays truthful under partial visibility: the roll-up covers all findings. A finding about a resource the user may not view still shows its severity, code and resource identity (kind, namespace, name) so the platform team knows which team to ask, but not its message or evidence (user, 2026-10-18). This reveals that the named resource exists; a stricter namespace-only variant can be revisited with the per-user design.
- Namespace scope counts and install identity are not filtered by the `Authorizer`; revisit with per-user authorisation.
- Phase 7 (TLS posture) must not read Secrets under this model; certificate details wait for per-user authorisation or another source.

## D-014 — Watch freshness trusts informers, with failure detection (2026-10-19, phase 1)
**Context:** D-006 forbids a false green, so the UI must notice when its cached view of the cluster may be outdated. Five cross-model review rounds showed that exact catch-up detection on top of client-go informers (heartbeats on bookmarks, replay checkpoints) cannot be made correct without tracking client-go internals: bookmark delivery is not guaranteed, and a resumed watch's first event does not mean its backlog is applied.
**Decision:**
- Any failed List or Watch call, or a `watch.Error` event, makes that kind unavailable, which turns the install unknown.
- The kind recovers on a successful Watch call opened after the latest failure, or on the initial-events-end bookmark in watch-list mode. No event is required, so idle kinds recover too.
- Dead connections are detected by client-go's HTTP/2 health check (about 45 s).
- Known limit: right after recovery, the UI can show the pre-outage state for the few seconds client-go needs to replay missed events. The outage itself shows as unknown.
- Known limit: a routine 410 Gone / Expired relist counts as a failure, so the kind shows unknown for the relist window.
- Known limit: a late `watch.Error` from an already-replaced stream can keep a kind unavailable until the next Watch call.
**Alternatives:**
- Periodic full relists with list-age freshness — rejected: a permanent API load of one full list per kind per minute, for a few-second window.
- Heartbeat on watch bookmarks — rejected: bookmarks are not guaranteed on idle watches.
**Consequences:**
- The UI is as fresh as any controller built on informers, including kgateway's own.
- No extra API-server load.

## D-015 — Scope falls back to the Deployment template (2026-10-19, phase 1)
**Context:** The namespace scope is read from `KGW_DISCOVERY_NAMESPACE_SELECTORS` on a controller Pod. With no Pod (Deployment scaled to 0, Pods deleted, or the Pod watch failing) the scope was unknown, and an install with an unknown scope can only claim Gateways that no other install claims.
**Decision:**
- Source order: a Ready controller Pod, then any controller Pod, then the controller Deployment's Pod template.
- The Pod comes first because it shows what is running; during a rollout the template already holds the new config.
- The scope records its source, and the scope check names it when the template is used.
- `valueFrom`, invalid JSON and a downstream provider's unknown still give an unknown scope.
**Alternatives:**
- Template first — rejected: during a rollout it describes the future, not the running controller.
- Pod only (the previous rule) — rejected by the user: a controller scaled to 0 should not lose its scope.
**Consequences:**
- A controller scaled to 0 keeps its Gateways and shows red through KGWUI-005, not an ownership change.
- Known limit: during a rollout that changes the selectors, old and new Pods carry different scopes, so the scope can switch between them and the verifier may briefly report a mismatch.

## D-016 — Platform neutrality is a design principle (2026-10-20, phase 1)
**Context:**
- The product focus stays kgateway (and later downstream distributions) installed on Kubernetes. Standalone is a separate project; UI integration comes later. This decision only keeps that future open.
- A separate project will run kgateway (controller and Envoy) on hosts without Kubernetes workloads, and the UI should later work on top of it. Two future modes:
  - **kube-lite**: a local kube-apiserver serves Gateway API and kgateway CRDs and their status, but no Pods, Deployments, Services or EndpointSlices exist. The controller and Envoy are host processes; Gateways use `GatewayParameters.spec.selfManaged`.
  - **file mode**: no apiserver. Config is a folder of YAML files, status lives in the controller's memory, objects have no UID, `resourceVersion` or `generation`, and the UI may be embedded in the standalone binary.
- An audit at `c2c52b8` found that the shared object model (`pkg/objects`, `pkg/kinds`), the `Authorizer` and the read-only stance are already neutral. Install identity, readiness, metrics targets, scope, freshness keys, `/meta`, the API v1 install paths and SPA copy assume Kubernetes workloads (design §14.2).
- The first release (sub-phase 1.4) freezes API v1 paths and SPA URLs. Anything client-visible is cheap to neutralise before it and a breaking change after it.
- `selfManaged` Gateways exist in Kubernetes mode too. Today each one gets a false KGWUI-006 "Proxy deployment not found" warning.

**Decision:**
1. Platform neutrality is a design principle with eleven constraints, R1–R11 (design §14.1). Kubernetes is the first platform, not an assumption of core code.
2. "Core" means object assignment, checks, roll-up, overview, the API handlers and the SPA. Kubernetes workload types (`corev1`, `appsv1`), discovery and client-go live in a **provider**. Today that boundary is the `cache.Snapshot` plus `pkg/install`; making it a real provider interface waits until standalone exists (design §14.4).
3. Client-visible contracts are neutralised **now**, before the first release (design §14.3):
   - opaque install IDs;
   - optional platform-specific fields meaning "not applicable";
   - a platform plus capability set in `/meta`;
   - a SPA vocabulary for platform copy.
   Internal refactors wait. The exception is self-managed proxies, a bug in Kubernetes mode today. The Go extension API is excluded from the first release's compatibility promise until the extension-seam milestone.
4. **No new hard dependencies.** No new `corev1`/`appsv1` import, Pod or Deployment assumption, or discovery-API dependency enters core code. A per-file import-boundary test enforces the imports, and review checks the semantic assumptions.
5. **Supersedes part of D-009 §1.** "Resources come from our own Kubernetes watches" becomes the rule *for the Kubernetes and kube-lite providers*. A file-mode provider may get objects and status from the controller, because file mode has no other source. D-009's other rules, D-012 and D-015 are rules of the Kubernetes provider.
6. **D-014 is the Kubernetes provider's freshness contract.** Other providers state their own. A provider that cannot confirm status matches the current spec cannot report green.
7. Mirroring controller logic stays a pluggable interim with an upstream ask, including the self-managed detection (design §14.3, H5).

**Alternatives:**
- Do nothing until standalone is real. Rejected: install IDs, API paths, DTO required fields and the extension `Detector` signature would ship in v1 and need a breaking change later.
- Build the provider interface and a neutral `Install` now. Rejected: designing it without a second implementation guesses its shape, and it is a large refactor of approved, merged scope.
- A mode string (`platform: kubernetes|standalone`) the SPA branches on. Rejected: capabilities compose (kube-lite has scope but no workloads) and downstreams can add their own.

**Consequences:**
- Phase 1 gains a small set of hedges (design §14.3). They change the API v1 install path from `{ns}/{name}` to `{id}`, which is a public interface change before its first release.
- A self-managed Gateway no longer gets a false "proxy Deployment not found" warning. The UI says it cannot see that proxy's readiness instead, and every observed failure still shows.
- Later phases design against capabilities, not Kubernetes (design §14.5). Phase 3 "pod health" becomes "process health"; Phase 4 must work with `Backend` and Envoy cluster health alone; Phase 10 authorisation is K8s-only through a capability unless a neutral story exists.
- What a standalone status source must provide is recorded as requirements for that project, not designed here (design §14.6).
- User decisions (2026-10-20): the hedges are a new planned sub-phase 1.3, and packaging, CI and e2e become 1.4. Self-managed readiness is info. `Backend` is registered in the Phase 2 design.

## D-017 — Phase 1 review fixes: health gaps, RBAC, YAML freshness (2026-10-21, phase 1)
**Context:**
- The Phase 1 cross-model review (reviews/phase-codex.md) found health false greens and two smaller contract gaps. A separate verifier confirmed each against the code and kgateway v2.3.11/v2.4.7.
- Some fixes need a choice the design did not make.

**Decision:**
1. **Workloads at zero.** A controller Deployment with 0 desired replicas is red KGWUI-005 (D-015 already says so). A managed proxy Deployment with 0 desired replicas is warning KGWUI-006: the Gateway serves no traffic, but scaling to zero may be intentional. A proxy with fewer ready than desired replicas stays an error.
2. **ListenerSet status is required.** Owned ListenerSets join design §5.2's required-status list, because kgateway writes some failures (`rejectDeniedListenerSets`) only on the ListenerSet (D-006).
3. **Grace keeps a check pending.** During a missing proxy's grace period, the required proxy check is pending, not OK, so the install cannot show green.
4. **Scope verification compares stable kinds only.** Kinds that kgateway only watches with experimental features on (pre-v1 TCPRoute and TLSRoute, XListenerSet) are left out of KGWUI-007's comparison. We do not read the experimental-features flag. When the upstream scope endpoint reports enabled kinds (D-009 §7), the verifier uses that list, so experimental Gateway API support works across kgateway and downstream distributions.
5. **Least-privilege RBAC.** The ClusterRole drops the ReferenceGrant and CRD reads. No code reads them, and discovery needs neither. This supersedes design §8's list and pre-flight ruling PF11.
6. **YAML freshness.** The YAML view polls like the other pages. The resource endpoint reports the collection time in a response header, so the v1 JSON body is unchanged.

**Alternatives:**
- Proxy at zero as error. Rejected: scaling a Gateway to zero can be deliberate.
- Read the experimental-features env from the controller Pod. Rejected: it mirrors more controller logic; the upstream endpoint is the right source.
- Add a `collectedAt` field to the resource body. Rejected: the body is the raw object, and wrapping it breaks v1.

**Consequences:**
- Stabilization sub-phase 1.5 implements these with queue items Q9–Q17.
- The upstream scope proposal should ask for enabled kinds, as the draft already does; the verifier switches to it when it exists.

## D-018 — Status-less experimental objects warn, with a hint (2026-10-21, phase 1)
**Context:**
- Sub-phase 1.5 first skipped experimental-only objects (XListenerSet, pre-v1 TCPRoute and TLSRoute) that carry no status from the controller, so installs with experimental features off stayed quiet.
- The 1.5 cross-model review (reviews/1.5-codex.md #1) showed this hides a real failure: kgateway enables experimental features by default, so a never-processed object looked healthy.

**Decision:**
1. Status-less experimental-only objects follow the normal rule: KGWUI-003 "not yet processed" warning after the grace period.
2. The finding adds a hint that the controller's experimental Gateway API features may be off.
3. Which kinds are experimental-only depends on the controller version: kgateway 2.3 watches no TCPRoute without experimental features; 2.4 watches v1 TCPRoute always. When the version is unknown, a kind counts as experimental-only if any supported version gates it.
4. D-017 §4 (scope verification on stable kinds only) is unchanged and applies on the metrics side too.

**Alternatives:**
- Info only, roll-up green. Rejected: a failure on a default install stays green.
- Health unknown while such objects exist. Rejected: installs with experimental features off stay unknown forever.

**Consequences:**
- Installs with experimental features off show a steady, explained warning for those objects until the upstream scope endpoint reports enabled kinds (D-017 §4).

## D-019 — Event-driven model recompute (2026-10-22, phase 2)
**Status:** accepted 2026-10-23 (proposed 2026-10-22 from the user's design review; approved with the Phase 2 design)
**Context:**
- Phase 1 recomputes the whole model on a fixed ticker, `--poll-interval`, default 2 s (`pkg/engine/engine.go:209-222`, `cmd/kgateway-ui/main.go:23`). Most recomputes see an unchanged cache.
- Phase 2 adds computed views and indexes per recompute (D-020, Phase 2 design §3). A fixed 2 s cost at the Large tier is waste on an idle cluster and still adds up to 2 s of latency on a busy one.
- Informers already deliver every change. Time-driven state (grace expiry, metrics staleness) and access transitions (D-014) do not arrive as object events.

**Decision:**
1. The engine recomputes on triggers:
   - informer add, update and delete events for every watched kind, including Namespaces, Pods and Deployments;
   - access transitions, from the list/watch progress hooks, with a 1 s confirm tick as a fallback;
   - metrics scrape results; the scrape cadence (`--metrics-interval`) stays separate;
   - served-kinds changes;
   - scheduled deadlines: grace expiry, metrics staleness, and the maturation of KGWUI-012 and the computed KGWUI-011;
   - a slow safety timer.
2. **Source fingerprint.** Triggers only start recomputes; they prove nothing, because client-go updates the informer store before it calls event handlers. While copying the stores, a snapshot computes a fingerprint of exactly what it copied: per kind, the object count and an order-independent hash of (uid, `resourceVersion`), plus every access state, the metrics result and the served kinds. A model records the fingerprint of its snapshot.
3. **Scheduler.** One goroutine owns the state: idle, pending (with a due time) or running. Debounce: a 250 ms trailing quiet window, bounded to 1 s from the first trigger of a burst. Deadlines run at their instant without debounce. Single flight: triggers during a run keep the state pending, and one more run follows. A run never clears a trigger that arrived during it.
4. **Publication.** A run publishes one immutable value, `{Model, Source fingerprint, ConfirmedAt}`, through an atomic pointer. `ConfirmedAt` starts as the snapshot time. Nothing in a published value is changed afterwards.
5. **Confirmation.** Every second, when nothing is pending or running, the confirm tick computes the live fingerprint of the stores. If it equals the published one, it publishes a new value with the same Model and `ConfirmedAt = now`. If not, it does not confirm and schedules a recompute, so a change whose handler has not run yet is still caught. A hung recompute stops confirmation, so the SPA's stale rule still fires (D-006).
6. **Timestamps.** `computedAt` on the install list keeps its meaning: when the model behind health was computed. A new optional `confirmedAt` carries `ConfirmedAt`, and the cluster cards, install switcher and header read it, falling back to `computedAt`. `collectedAt` on the overview, health and every Phase 2 body, and the `Collected-At` header, carry `ConfirmedAt`. Per-check `collectedAt` and finding `since` are unchanged. The Phase 2 design §3.1 lists every field and consumer.
7. **`--poll-interval`** keeps its name and type. Its meaning becomes "recompute at least this often" (the safety timer). The default rises from 2 s to 30 s. A user who set 2 s gets Phase 1's cadence as a floor, so existing settings stay valid.
8. **Polling only reads.** The SPA keeps its 5 s poll. A request loads the published value once and serves its Model and `ConfirmedAt`; it renews nothing. Push stays out of scope.

**Alternatives:**
- Keep the fixed ticker. Rejected: a constant full recompute on idle clusters, and latency bounded by the interval.
- Incremental per-object index updates. Rejected for now: much more complex than an immutable rebuild, and unnecessary at the Large tier. Revisit if the Extreme tier numbers demand it.
- No debounce (recompute per event). Rejected: a Helm upgrade would cause hundreds of recomputes.
- Replace `--poll-interval` with a new flag. Rejected: it breaks existing invocations for no gain.
- Redefine `computedAt` as the confirmation time. Rejected: it has a documented meaning, and clients may rely on it.
- Stamp the confirmation time on the published Model. Rejected: it breaks immutability and can stamp an old model as current.
- Confirm on a counter bumped by event handlers. Rejected: handlers run after the store changes, so a notification backlog would renew freshness on old data.
- An engine-owned mirror store with its own revision. Rejected: the lagging handlers fill it, so it proves only what they delivered.

**Consequences:**
- The Model stays immutable and is swapped atomically. Between changes the Model, and so list order and cursors, stays the same.
- `pkg/cache` registers event handlers and exposes a trigger source and fingerprinted snapshots. The engine takes a clock, a trigger source and a snapshot source, so tests are deterministic with a fake clock, including the interleavings of snapshot, event, publication and confirmation, and paused handler delivery.
- The confirm tick walks the stores once a second; its cost is benchmarked in sub-phase 2.1.
- `installs.json` and `bare-installs.json` gain the optional `confirmedAt`; the change is additive.
- Budgets are stated per recompute and per change burst, not per fixed interval (Phase 2 design §9.6).
- The README and chart notes describe the new meaning of `--poll-interval`.

## D-020 — Computed views, labelled and cross-checked (2026-10-22, phase 2)
**Status:** accepted 2026-10-23 (proposed 2026-10-22 from the user's design review; approved with the Phase 2 design)
**Context:**
- D-005, D-016 §7 and the memory rule keep the UI from re-implementing controller logic, except as a pluggable, verified interim with an upstream ask.
- Three questions at the core of Gateway API observability have no answer in kgateway's status:
  - which listener a route attached to, and with which effective hostnames (status is per parentRef; listener status has only `attachedRoutes`);
  - which children a delegation ref selected, and which it dropped (a child dropped by the matcher fit gets no status, `v2.4.7 pkg/kgateway/translator/httproute/delegation_helpers.go:33-107`);
  - a delegated child's effective matcher.
- The only controller source is the admin server's `/snapshots/xds`. It binds to localhost, shares a port with `/logging` and pprof, and needs `pods/portforward` (Phase 2 design §3.7).

**Decision:** the user overrides the strict boundary for three views:
- **(A) route → listener attachment:** sectionName and port, allowedRoutes namespaces (including `Selector` on Namespace labels), kinds, and hostname intersection;
- **(B) delegation child selection:** name, `*` and label, with the namespace including the `DELEGATION_WILDCARD_NAMESPACE` controller env, read from the controller Pod like the scope selector, with D-015's fallback. Also the child's parentRef permission and the matcher fit, so the UI can show children that were selected but dropped;
- **(C) the effective matcher** of delegated children, in inherit-parent-matcher and default mode.

Rules for each view:
1. Every computed fact is labelled "computed by the UI", with the rule applied and the rule set.
2. It is cross-checked against what the controller reports:
   - per-listener `attachedRoutes`, with kgateway's exact counting semantics;
   - parentRef `Accepted` reasons;
   - child status entries;
   - the parent's `ResolvedRefs` (`BackendNotFound`, `RefNotPermitted`).
   A signal checks only the stage the rules model; later failures, such as backend resolution, are never disagreements. Expected signals are aggregated at the controller's reporting granularity. A passing check says what it covered ("count agrees", "status compatible" or "not independently verified"). A persistent disagreement is shown as "computed view disagrees with controller" and raises KGWUI-012 (warning). It is never hidden. `docs/phases/phase-2/computed-rules.md` §8-9 defines the contract.
3. The computation sits behind a pluggable interface (`pkg/computed`, like `pkg/proxymode`). It is pinned and tested per kgateway minor (rule sets `kgateway-2.3` and `kgateway-2.4`), with per-version golden fixtures. The rules are specified in `docs/phases/phase-2/computed-rules.md`. Delegation is evaluated with the parent's match as its ancestors left it; where that context cannot be resolved, the result is unknown.
4. Ownership, counts and install health stay status-based. A computed view explains and cross-checks; it never owns an object.
5. Each view has an upstream ask, and the computation becomes a cross-check once kgateway reports the fact.

Shadowed-match detection stays the controller's job: an upstream ask, consumed later. Hostname intersection (view A) is also recorded as a limitation: the UI shows status-level hostnames, not 2.4's listener isolation.

**Alternatives:**
- Status only, with "All listeners" and observed children only. Rejected by the user: it leaves the most common questions unanswered.
- Read `/snapshots/xds`. Rejected for Phase 2: write-class RBAC, a debug surface, and no reliable source attribution on 2.3.
- Compute without cross-check. Rejected: a silent UI bug would look like controller truth.

**Consequences:**
- The UI reads Namespace labels (already watched), and two controller env settings (`DELEGATION_WILDCARD_NAMESPACE`, `KGW_ENABLE_EXPERIMENTAL_GATEWAY_API_FEATURES`), for the computed views only. D-017 §4 is unchanged for the scope verifier.
- A new kgateway minor needs a rule-set review before it is supported.
- The kind e2e asserts that computed and controller counts agree on 2.3 and 2.4.
- New finding KGWUI-012. KGWUI-011 gains a computed trigger.
- When an install's discovery scope is unknown, view A is unknown (`scope-unknown`), so no KGWUI-012 comes from guessed candidates (user, 2026-10-24).
- Upstream asks: resolved route → listener relationships, delegation resolution per child, a controller-written state CRD outside Gateway API, shadowed-match conditions, and hostname resolution (Phase 2 design §14).

## D-021 — Delegation and attachment status, observed on a cluster (2026-10-23, phase 2)
**Status:** accepted 2026-10-23 (observation, sub-phase 2.1 Task 1)
**Context:**
- `computed-rules.md` §10 lists ten behaviours the rule sets assume but code reading cannot settle (design §9.5).
- Run on kind with kgateway v2.4.7 / Gateway API v1.6.1 and v2.3.11 / v1.5.1, experimental CRDs, plus the legacy XListenerSet CRD from v1.4.1 (the last release that ships it). Fixtures and runner: `test/e2e/fixtures/observe/`.
- World: Gateway `infra/edge` with listeners `http` 8080 and `https` 8443 (`*.example.com`); parent HTTPRoute `shop/api` attached to it. Each item was recorded after the Gateway and `shop/api` reported their current generation and two records 5 s apart matched. Items 1 and 6 were recorded again 30 s later; nothing changed.
- "Entry" means a `status.parents` item with `controllerName: kgateway.dev/kgateway`. "Synthesized ref" is `{group: gateway.networking.k8s.io, kind: HTTPRoute, name: api, namespace: shop}`.

**Decision:** the rule sets use these facts. Both tags agree except where noted.

| # | Applied | v2.4.7 | v2.3.11 |
|---|---|---|---|
| 1 | implicit child `c1`, then the parent switches its ref to `c1b` | `c1` entry under the synthesized ref, then `parents: []` | same |
| 2 | explicit child, parentRef `{group, kind: HTTPRoute, name: api}` (no namespace) | entry under the ref as written, no namespace | `parents: []` (no entry) |
| 3 | explicit children: `sectionName: http`; `port: 8080` | both entries as written, with `sectionName` or `port` | `sectionName`: `parents: []`; `port`: entry with `port: 8080` |
| 4 | implicit children by name, `*` in `orders`, label in `shop`, label in namespace `all` | an `Accepted=True` entry under the synthesized ref for each | same |
| 5 | hostname children: `c5d` fails the fit, `c5k` fits | `c5d` no status; `c5k` `Accepted=False/UnsupportedValue`; parent `ResolvedRefs=False/BackendNotFound` | same |
| 6 | child `c6` attached, then the parent match becomes `/store` | entry, then `parents: []` | same |
| 7 | quirk 1: refs `c7a`, `c7b` in one rule | proxy has two `/q1/a` routes (both from `c7a`) and one `/q1/b` | same |
| 7 | quirk 2: `c7c` under `/q1` and `/q2`; `c7d` (`/q1/d`) only under `/q2` | `c7d` `Accepted=True`; proxy has `/q1/d`; parent `ResolvedRefs=False/BackendNotFound` | same |
| 8 | mid-level `c8p`: self-delegation (cycle) and a missing Service, observed with two rule orderings | cycle rule first → `BackendNotFound`; Service rule first → `RefNotPermitted` | same |
| 8 | top-level: working Service rule plus an unresolved delegation ref | `ResolvedRefs=False/BackendNotFound` | same |
| 9 | one rule, matches `/api` and `/other`, child `c9` `/api/nine` | one entry, `Accepted=True`; parent `ResolvedRefs=False/BackendNotFound`; one proxy route | same |
| 10 | delegated child `c10`; route `lsr` on ListenerSet `infra/ls` (`extra` 8090); route `xlsr` on XListenerSet `infra/xls` (`legacy` 8091) | Gateway `http=3 https=3`: `good`, `broken` and `api`, not `c10` (baseline before any item: 2 each); `extra=1`; `legacy=1`; both sets `Accepted=True` | same |

Verbatim excerpts (`kubectl get httproute -o json | jq '.status.parents'`):
- Item 2, v2.4.7: `"parentRef":{"group":"gateway.networking.k8s.io","kind":"HTTPRoute","name":"api"}` with `Accepted=True`. v2.3.11: `"parents":[]`.
- Item 3, v2.3.11: `c3p` `"parentRef":{"group":"gateway.networking.k8s.io","kind":"HTTPRoute","name":"api","namespace":"shop","port":8080}`, `Accepted=True`; `c3s` `"parents":[]`.
- Items 1 and 6, both tags: `{"n":"shop/c1","parents":[]}`, `{"n":"shop/c6","parents":[]}`.
- Item 8, both tags: `c8p` `ResolvedRefs=False/BackendNotFound@1` (cycle rule first); `ResolvedRefs=False/RefNotPermitted@1` (Service rule first).
- Item 10, both tags: baseline `{"listeners":[{"name":"http","attachedRoutes":2},{"name":"https","attachedRoutes":2}]}`; after item 10 `{"name":"http","attachedRoutes":3},{"name":"https","attachedRoutes":3}`; `infra/ls` `{"type":"Accepted","status":"True","reason":"Accepted","observedGeneration":1}`, `{"name":"extra","attachedRoutes":1}`; `infra/xls` `{"type":"Accepted","status":"True","reason":"Accepted","observedGeneration":1}`, `{"name":"legacy","attachedRoutes":1}`.

Verbatim proxy routes for item 7, listener 8080, identical on both tags (`run.sh xds`; the `https` listener repeats them):
```
quirk 1 (prefix /q1):
{"name":"listener~8080~*-route-1-httproute-c7a-shop-0-0-matcher-0","match":{"path_separated_prefix":"/q1/a"}}
{"name":"listener~8080~*-route-2-httproute-c7a-shop-0-0-matcher-0","match":{"path_separated_prefix":"/q1/a"}}
{"name":"listener~8080~*-route-3-httproute-c7b-shop-0-0-matcher-0","match":{"path_separated_prefix":"/q1/b"}}
quirk 2 (prefix /q):
{"name":"listener~8080~*-route-1-httproute-c7c-shop-0-0-matcher-0","match":{"path_separated_prefix":"/q1/c"}}
{"name":"listener~8080~*-route-2-httproute-c7c-shop-0-0-matcher-0","match":{"path_separated_prefix":"/q2/c"}}
{"name":"listener~8080~*-route-3-httproute-c7d-shop-0-0-matcher-0","match":{"path_separated_prefix":"/q1/d"}}
{"name":"listener~8080~*-route-5-httproute-api-shop-1-0-matcher-0","match":{"path_separated_prefix":"/q2"}}
```

**Alternatives:** n/a (observation).

**Consequences:**
- Report keys (§7) are confirmed as written on both tags, including the 2.3 port case. No rule-set change.
- Clearing is verified on both sets only when a parent edit causes the drop: deselection (item 1) and matcher drop (item 6). §2 and the §8.3 "unexpected status" row mark that case verified, so a current leftover entry there can raise KGWUI-012 on 2.3 too (sub-phase 2.4). A drop caused by a child edit was not observed, and 2.3's clearing path was not traced in code. A leftover entry after a child-edit drop stays inconclusive (`not-verified`, no `disagrees`, no KGWUI-012) until observed. Reported-edge eligibility (§4.2) is unchanged.
- The two quirks of §5.4 are verified on both tags; the flag is removed. With quirk 2 a child can attach under a rule that never names it.
- A delegation context with no kept child sets the parent's `ResolvedRefs=False/BackendNotFound` (items 5, 7, 9), as §5.4's `valid == 0` says. 2.4 goldens for those fixtures expect it.
- When delegation and backend failures meet on one key, the last rule in spec order wins (observed with two rule orderings). §8.2 already compares the reason only without non-delegation backendRefs; unchanged.
- `attachedRoutes` never counts delegated children. A ListenerSet or XListenerSet route counts only on that set's listener, not on the Gateway's. View A (2.3) uses this.
- Phase 1's base fixtures attach `good` and `broken`; `blog/posts` is outside install A's scope and was not counted.

**Amended 2026-10-26 (observation):** A kind observation on kgateway v2.4.7 (Gateway API v1.6.1) and v2.3.11 (v1.5.1) showed that the controller clears a delegated child's stale status entry for a parent when the child is edited. Results are in `.superpowers/sdd/implementation-plan/observe-child-edit.md`; fixtures `test/e2e/fixtures/observe/11-child-matcher-edit.yaml` (child match becomes `/other/six`) and `12-child-parentrefs-edit.yaml` (child adds a parentRef the parent does not satisfy).
- Per tag, on v2.4.7 and on v2.3.11: case 1 (the child's match changes, so it no longer fits) and case 2 (the child adds parentRefs that do not permit the parent) each went from one entry to `[]` before the first poll (under 0.15 s). The control, a parent edit (`06b-matcher-dropped.yaml`), cleared too, in the same time.
- Code reading agrees: `ProcessHTTPRouteStatusMarkers` (`pkg/krtcollections/policy.go` V3:1119-1137, called from `proxy_syncer.go` V3:292-293; V4:1147-1178) clears any kgateway entry that is not reported, whatever caused the drop.
- Consequence: clearing is verified on both rule sets for every cause. A current leftover entry after any drop or deselection is `disagrees` after the 30 s window, and so KGWUI-012, on both sets. The split by cause in the consequences above, and PF1's use of first-seen times to decide the cause, are removed. Case 2 was a child that became `forbidden` (its new parentRef no longer permits the parent), so a forbidden child's leftover entry is verified too (ruling 2026-10-26, Task 61 fix round). Rules §2, §8.3, §10 and §11.4, design §5.2 and §14.5, the 2.4 brief and Task 61 follow.

## D-022 — Memory is gated on CPU and retained heap, not allocation (2026-10-25, phase 2)
**Status:** accepted 2026-10-25 (user)
**Context:**
- Design §9.6 gated allocation per warm recompute at ≤ 32 MiB at the Large tier, an unexplained number of our own (all §9.6 budgets are "our call"). The user kept it on 2026-10-23 when 2.2 measured 35.7 MB on CI.
- Allocation is short-lived garbage; it costs only through garbage collection. The stream budget (engine CPU ≤ 15 % of one core) measures that cost directly, and retained heap measures what the UI holds against the chart's pod limit (`limits.memory: 512Mi`).
- 2.3 at Large: 29.2 MiB per recompute in a consistent world, 44.2 MiB in a worst case where every route status contradicts view A (3,309 KGWUI-012 and 2,574 Phase 1 KGWUI-002 findings), 35.1 MiB after the cuts below; retained heap +13.6 and +16.1 MiB over the baseline.

**Decision:**
1. Allocation per warm recompute is reported, not gated.
2. The gates are engine CPU in the sustained stream (≤ 15 % of one core), retained heap (≤ 32 MiB above the baseline) and staying well inside the pod memory limit, each in the consistent world and in the worst case. "Well inside" is memory obtained from the OS during the sustained stream ≤ 256 MiB, half the limit **(our call)**.

**Consequences:**
- Task 49 runs the sustained stream in both worlds and applies the cheap allocation cuts anyway (pre-sized finding slices, `Finding.parent` as a pointer), which bring the worst case from 44.2 to 35.1 MiB.
- Replaces the user's 2026-10-23 "keep 32 MiB" for allocation; the ledger note to revisit it at each sub-phase close now applies to CPU and retained heap.

## D-023 — View A on the wire: hidden counts and merged written refs (2026-10-25, phase 2)
**Status:** accepted 2026-10-25 (user)
**Context:**
- The 2.3 cross-model review found that a hidden candidate route moved a visible route's membership: the count's unknown state and the `listener=` filter ignored the count's dependencies.
- The review also found that the route list showed only the first of several written parentRefs that resolve to one parent. Each has its own report key and check (rules §8.2), so a contradicting second check was invisible in the list.

**Decision:**
1. A count's facts, its check and its unknown state, reach a membership and the `listener=` filter only when the count's dependencies are visible. Otherwise the membership carries only its parentRef's check (design §7.2, §7.4 rule 5).
2. The list's one `ParentSummary.computed` merges those checks: worst state first (`disagrees` > `pending` > `unknown` > `restricted` > `checked`), coverage united, `restricted` if any side is. Route detail keeps each written ref separate (design §7.3, rules §8.4).

**Consequences:**
- A caller who cannot view a count's dependencies may see a membership as checked where an unrestricted caller sees it unknown. That reveals nothing hidden.
- No wire format change: `ParentSummary` keeps its shape.

**Amended 2026-10-26 (user):**
- Decision 1 is replaced. Listener facts are shown to every caller who can see the listener: the computed count, its cross-check, its unknown state and reason, and its isolation note. The count includes members the caller cannot see, as `attachedRoutes` does. A visible route's membership takes the count's state again, so an unknown count makes it unknown for every caller, and `listener=` has no members under it. Per-route facts of hidden routes stay hidden (design §7.2, §7.4 rules 4 and 5).
- Why: the re-review (R1, R2) showed that restricting a count leaks. Moving a hidden route or ListenerSet into or out of a listener's dependencies flipped visible facts between "Hidden" and their real state. The number of routes a Gateway serves is also a property of that Gateway.
- Decision 2, the written-ref merge, stands.

## D-024 — Views B and C: unknown scope and the inherited match (2026-10-26, phase 2)
**Status:** accepted 2026-10-26 (user; sub-phase 2.4 pre-flight PF4, PF5)
**Context:**
- When an install's discovery scope is unknown, view A is unknown: its candidates are a guess (user, 2026-10-24; rules §3.3). Views B and C had no such rule. With an unknown scope every namespace counts as in scope (`pkg/install/scope.go:45-48`), so view B would select routes the controller may not watch and could raise computed KGWUI-011 and KGWUI-012 on them.
- Design §7.4 rule 4 made a delegation context, row or effective matcher `restricted` when any route on its path was hidden. D-023's amendment showed that such restriction leaks: a fact flips between "Hidden" and its real state as a hidden object enters or leaves its dependencies. A hidden ancestor gaining or losing a path to a visible child flips that child's rows the same way.

**Decision:**
1. **Unknown scope.** An unknown discovery scope makes views B and C unknown. Every delegation row, context, effective matcher and check is `unknown` with reason `scope-unknown`. No computed or orphan KGWUI-011 and no KGWUI-012 from `child-entry` or `parent-resolvedrefs` is raised while it holds. This extends the 2026-10-24 decision.
2. **Inherited match.** A delegation row, delegation context or effective matcher on a child the caller can see shows its inherited match: the match each ancestor rule on the path delegated under, and the ancestors' names, even when an ancestor is hidden. Nothing else of a hidden ancestor is sent: no other rules, filters, backends, status or detail link (`link: none`). So these facts never turn "Hidden" because of a hidden ancestor.
3. The rule covers `GET …/delegation` (rows, contexts and the `outcome` filter), `RouteDetail.effective` and the computed `dropped` flag. Per-route facts of hidden routes that are not ancestors of a visible row stay hidden (design §7.4 rule 4). Row presence and `total` still count visible children only (rule 3).
- Why (user): the child's owner needs the inherited match to manage the child; the ancestor's name is already in the child's status (design §7.4 rule 8); the delegating match is what the parent deliberately handed to the child.

**Alternatives:**
- Restrict every fact with a hidden route on its path (design §7.4 rule 4 as first written). Rejected: visible rows flip to "Hidden" as hidden ancestors come and go, and the child's owner loses what they need.
- Compute views B and C over a guessed scope. Rejected: selection outside the controller's real scope raises false warnings.

**Consequences:**
- Rules §3.3, §5.6 and §11.2, and design §6.2, §7.3, §7.4 (rule 4b) and §9.1 change with this entry.
- A context's path steps carry each ancestor's name and the match it delegated under. Retained contexts grow by one match per path step, inside the retention caps of rules §5.7.
- Non-route dependencies still restrict: a root admitted through a `Selector` over a hidden Namespace, or a status root whose target is hidden, keeps its descendants' contexts, rows and effective matchers `restricted` (rules §11.2). Those facts are decided by hidden labels or hidden objects, not handed over by an ancestor **(our call; raised with the user)**.
- The same rule holds everywhere a hidden ancestor route of a visible child appears: the route header's up-chain step (design §7.4 rule 3, ancestor routes only) and KGWUI-011 and KGWUI-012 evidence (rule 7) show its name and the matches it delegated under, with no link and nothing else. A finding whose only hidden dependencies are such ancestors is not `evidenceRestricted`. Hidden Gateways and ListenerSets on the up-chain keep `{restricted: true}` **(our call, extends D-024)**.
- The computed `dropped` flag follows decision 2 (our call).

## D-025 — Delegation edge cap: lower default, configurable, GOMEMLIMIT (2026-10-27, phase 2)
**Status:** accepted 2026-10-27 (user)
**Context:**
- The Task 68 review measured Extreme dense selection (100 wildcard parents × 1,000 routes) with realistic status: at most 32 entries per route, the Gateway API MaxItems. At the 50,000 edge cap the peak heap was 640 MiB and the peak RSS 730 MiB, past the chart's 512Mi limit. `GOMEMLIMIT=400MiB` held the heap at 393 MiB but thrashed the GC (RSS 505 MiB). An edge cap of 10,000 gave 361 and 427 MiB; Large dense (20 × 500) 179 and 215 MiB.
- The scale fixture wrote 100 status entries per route, which a real API server rejects.
- Measured again for the cap (`bench/2.4-dense-memory.txt`, Apple M5, GOMAXPROCS=4, no `GOMEMLIMIT`): peak RSS 423–465 MiB at 10,000, 509–566 MiB at 20,000, 702–760 MiB at 50,000. Below 10,000 the curve flattens: 404–412 MiB at 5,000 and 372–433 MiB at 2,000 or less. The floor is the snapshot (69 MiB live) plus the rest of the engine and model (54 MiB live), doubled by the GC's heap target while two models are live.

**Decision:**
1. The default edge cap is 10,000 selected edges per install, down from 50,000 (rules §5.7) **(user, 2026-10-27)**. It is the largest round value inside the limit; it keeps Large dense (10,000 edges) uncut and Large's 2,017 view B rows well under it.
2. The cap is configurable: the binary flag `--delegation-edge-cap` and the chart value `delegation.edgeCap`. Zero keeps the default; a negative value fails start-up. The other bounds stay unexposed **(user, 2026-10-27)**.
3. The chart sets `GOMEMLIMIT` to the container's `limits.memory` through the downward API, and leaves it unset without a memory limit **(user, 2026-10-27)**. The downward API cannot scale the value, so it is the whole limit, not a fraction of it.
- The gate `TestBudgetDenseMemory` (bench tag) holds Extreme dense at the default, with no `GOMEMLIMIT`, to peak RSS ≤ 480 MiB and peak heap ≤ 432 MiB. The 400 and 320 MiB first proposed sit below the floor at any cap, so no cap meets them **(our call; raised with the user)**.

**Consequences:**
- Past the cap, parents read "too large to compute"; rows already computed keep their outcomes with some paths not computed.
- A hit edge cap makes every route incomplete (rules §8.5), so negative checks are silent across the install: no missing child entry and no computed KGWUI-011.
- A higher cap needs a higher memory limit. The user docs say so (`docs/documentation-notes.md`).
- `GOMEMLIMIT` follows the pod limit, so raising `limits.memory` raises it too. It is a safety net, not a substitute for the cap: at 50,000 with the limit set the RSS still reached 599 MiB.
- `scale.DenseSelection` writes at most 32 status entries per route (`scale.MaxRouteParentStatuses`).
- Depth bound 100 (our call, 2.4): a path of more than 100 routes is cut there as `too-large` (rules §5.7). Each context's deps hold its path's routes, so an uncapped 3,000-route chain retained 290 MiB; at the cap it retains about 1 MiB (`TestBudgetDeepChain`). It is not exposed as a flag.

**Amended 2026-10-27 (user sign-off):**
- The gate `TestBudgetDenseMemory` checks peak heap only (≤ 432 MiB at the default cap). RSS stays measured and logged but is not gated: it had about 11 MiB of margin and could flake on CI. This replaces the RSS ≤ 480 MiB part of the gate above.
- `GOMEMLIMIT` is 90 % of the pod memory limit, not the whole limit, and the binary sets it. The chart passes the raw limit in `KGWUI_MEMORY_LIMIT` through the downward API (only with `resources.limits.memory`), and at start-up the binary calls `debug.SetMemoryLimit` with 90 % of it. An explicit `GOMEMLIMIT` wins; a missing, zero or unparsable value is ignored and logged. This replaces decision 3's chart-set `GOMEMLIMIT` and the matching consequence.

**Amended 2026-10-28 (user):**
- The gate takes the median peak heap of five child processes at the default cap, still ≤ 432 MiB. A single peak sample flakes on GC timing: the same code and allocations ranged 362–452 MiB. Twice the default and 50,000 run once and are reported only.

## D-026 — Sub-phase 2.5: expected outputs the design does not state (2026-10-28, phase 2)
**Status:** accepted 2026-10-28 (our call)

**Context:** The 2.5 brief marks three expected outputs as inferred. The process requires each to be confirmed or recorded before implementation.

**Decision:**
1. A failed or stale `/delegation` poll turns the Delegation tab's states Unknown at once, and the next good poll restores them. This is how the 2.2 detail pages behave (design §8.6).
2. A hidden ref that the edge cap cut reads the same as an uncut restricted ref: `restricted`, nothing more (design §7.4 rule 4c).
3. The TypeScript golden guard fails typecheck or the tests when a wire value's type in `types.ts` stops matching the goldens.

**Consequences:** These are the 2.5 acceptance rows for Q1, Q4 and Q7. Each reuses an existing rule, so no new behaviour is introduced.

**Alternatives:** n/a.

## D-028 — Stable finding identity, groups and ranking (2026-11-01, phase 3)
**Status:** accepted 2026-11-01 (user approved the Phase 3 design and plan)

**Context:** Phase 2 findings have no id, so the SPA cannot link, page or track a finding across recomputes (phase-2 review finding 8).

**Decision:** Every finding gets a stable `id` from its code and identity key, and a `group` when it shares a cause with others. Groups are ranked by severity, blast radius and class (design §5).

**Consequences:** `id` and `group` are optional API fields. Findings page on the server by rank.

**Alternatives:** n/a.

## D-029 — Process health through `process.Source` (2026-11-01, phase 3)
**Status:** accepted 2026-11-01 (user approved the Phase 3 design and plan)

**Context:** Phase 3 reports controller and proxy process health (D-016) without reading admin ports or Pod output.

**Decision:** Process facts come through `process.Source`, with one allowlisted Kubernetes source. Proxy Pods, Warning Events and Gateway API CRD metadata are watched. Termination messages and probe output are never read (design §9, §18). This reverses D-017 §5 for CRD metadata only, behind a toggle.

**Consequences:** New watches and RBAC rows, each with a capability that degrades to "not available".

**Alternatives:** n/a.

## D-030 — Specific codes replace KGWUI-002 for their reasons (2026-11-01, phase 3)
**Status:** accepted 2026-11-01 (user approved the Phase 3 design and plan)

**Context:** KGWUI-002 covers every `False` condition, so its message cannot say what to fix.

**Decision:** Specific codes (013, 014, 015, 016) replace 002 for their reasons; 002 stays the fallback. KGWUI-001 is re-specified rather than adding a "status says healthy" code. A row whose Gateway rejects config is never healthy (design §7.1, §8.3, §8.4).

**Consequences:** A listed exception to the additive golden test: only the `code` and `slug` of the named findings change. API v1 has no release yet.

**Alternatives:** n/a.

## D-031 — Backend readiness from Services and EndpointSlices (2026-11-01, phase 3)
**Status:** accepted 2026-11-01 (user approved the Phase 3 design and plan)

**Context:** A route can be accepted while its backend has no ready endpoints.

**Decision:** KGWUI-023 reads Services and EndpointSlices, limited to Service refs the controller resolved (design §11).

**Consequences:** New watches with transforms and an `endpoints` capability, in 3.4.

**Alternatives:** n/a.

## D-032 — The `health.Check` contract changes to `Name`, `Inputs`, `Run(RunContext)` (2026-11-01, phase 3)
**Status:** accepted 2026-10-29 (user)

**Context:** Checks recompute on every change, and downstream checks cannot declare new inputs.

**Decision:** A breaking change to `Name`, `Inputs`, `Run(RunContext)`, with an engine-owned Store per (install, check) and memoization on the declared inputs (design §16.2).

**Consequences:** Every core, example and downstream check migrates in 3.1. README upgrade notes. The last planned break of the Go extension API.

**Alternatives:** n/a.

## D-033 — kgateway 2.5 joins the support matrix at GA (2026-11-01, phase 3)
**Status:** accepted 2026-10-29 (user)

**Decision:** kgateway 2.5 is supported in 3.4, once 2.5.0 GA ships. Before GA it runs on the 2.4 rule set (design §7.6, §8.6, §23).

**Consequences:** If GA is not out at 3.4, its 2.5 tasks wait and the rest ships.

**Alternatives:** n/a.

## D-034 — No Phase 3 check is required (2026-11-01, phase 3)
**Status:** accepted 2026-11-01 (our call, approved with the design)

**Decision:** A check whose input is off, denied, failing or absent returns "not available" with a reason. It never raises KGWUI-008 and never turns the install unknown. `since` is the controller's time for status findings and the UI's first-seen time for UI-derived ones (design §5.1, §7.1).

**Consequences:** Existing required checks keep their flag.

**Alternatives:** n/a.

## D-035 — Path-based breadcrumbs, a shared object header and a fluid layout (2026-11-01, phase 3)
**Status:** accepted 2026-10-29 (user)

**Decision:** Breadcrumbs follow the path the user took, carried in the URL (`via`). Every detail page has an object header. The layout is fluid up to 2400 px, with two columns on detail pages from 1600 px (design §17.6).

**Alternatives:** n/a.

## D-036 — Deterministic suggested fixes (2026-11-01, phase 3)
**Status:** accepted 2026-10-29 (user)

**Decision:** A fix is suggested only where exactly one change follows from the spec, discovery or a watched object. It is a fragment with its location for existing objects. It is never applied and never derived from translation logic (design §7.7).

**Consequences:** AI-assisted diagnosis is a later phase (roadmap Phase 11).

**Alternatives:** n/a.

## D-037 — Envoy stats from the proxies; controller logs opt-in (2026-11-01, phase 3)
**Status:** accepted 2026-11-01 (user)

**Decision:** Envoy stats are read from each proxy's stats listener, with no new RBAC. Controller logs are opt-in and limited to the install namespace; only fixed fields are kept, and log text is never stored or shown. Proxy logs, `/config_dump` and admin ports stay out until per-user identity (Phase 10) and Phase 9 (design §8.8, §8.9, §12, §18).

**Alternatives:** n/a.

## D-038 — Extension seams for downstream features, as sub-phase 3.0 (2026-11-01, phase 3)
**Status:** accepted 2026-11-01 (user)

**Context:** A downstream build could add checks, but not a whole feature, without forking.

**Decision:** Seven seams: endpoints, collectors, open check inputs, capabilities, a built shell package, in-page slots and chart extension values (design §4.1). They ship as sub-phase 3.0. `examples/downstream` in CI is the standing proof.

**Alternatives:** n/a.

## D-039 — Dense memory gate on retained heap and bytes per recompute (2026-11-01, phase 3)
**Status:** accepted 2026-11-01 (user); D-044 adds the analyzer stage's allocations to the deterministic gates

**Context:** `TestBudgetDenseMemory` gated the median peak heap of five runs at ≤ 432 MiB (D-025). Peak heap depends on when the GC runs: on the same code, medians ranged 427–440 MiB, and the 2.4 code (`3eceda6`) failed it too. The heap retained and the bytes allocated were identical in every run, before and after 3.0.

**Decision:** The gate checks three medians at the default edge cap: the heap retained after warm recomputes (≤ 176 MiB), the bytes allocated by the cold recompute (≤ 246 MiB) and per warm recompute (≤ 185 MiB). Each is the 2.4 measurement (160, 210 and 168 MiB; the cold allocation is 224 MiB on the linux/amd64 CI runner) plus about 10 % of the larger value. The cold budget went from 231 to 246 MiB on 2026-11-02 so CI keeps the same headroom (user). Peak heap and RSS stay logged, not gated. The binary's soft memory limit (90 % of the pod limit, D-025) makes the GC work harder near the limit; it does not guarantee the pod stays under it.

**Consequences:** A regression in what the engine keeps or allocates fails the gate deterministically. A change that only moves GC timing does not. A change that holds more memory at once without allocating more is not gated; peak heap is logged for it (tracked in `phase-status.md`).

**Alternatives:** Raise the peak-heap budget; rejected, since the 2.4 code already sits at it and noise stays.

## D-027 — Keep the immutable full recompute; no KRT (2026-11-02, phase 3)
**Status:** accepted 2026-11-02 (decision rule of design §16.1, written before the spike)

**Context:** Recompute cost grows with analyzers and cluster size, and Phase 2 ended near its CPU budget. Istio's KRT collections recompute incrementally. Sub-phase 3.1 rebuilt view A on KRT in a throwaway spike and measured it at Large against the current engine (`bench/3.1-krt-spike.txt`).

**Decision:** Keep the immutable full recompute. Add per-check memoization (design §16.2). The spike failed two of five criteria:

| Criterion | Measured | Pass |
|---|---|---|
| Spec-edit stream CPU at least 40 % lower | 14.2 % against 10.9 % of one core: 30 % higher | no |
| Retained heap no more than +16 MiB | +16.5 to +19.4 MiB | no |
| One immutable published value per request | yes, only through a hand-built three-step barrier (KRT has no flush call) | yes |
| No `replace` directives | none | yes |
| Apache-2.0 compatible licences | Apache-2.0, MIT, BSD, MPL-2.0 | yes |

Other costs: binary +7.9 MB (+27 %), module graph 318 → 1,429 lines, Go floor 1.27.1, a forced Kubernetes client upgrade, and `istio.io/istio` has only pseudo-versions.

**Consequences:** Phase 3 continues with declared check inputs and memoization (Task 18), which an incremental engine could feed later. The spike rebuilt view A only, which is 3.6 % of recompute CPU, so it could not show a 40 % gain on its own. A future proposal would have to move all views and checks, at the dependency costs above.

**Alternatives:** Adopt KRT for views and analyzers in a follow-up phase; rejected by the rule.

## D-040 — Finding ids hash only data a restricted row shows (2026-11-02, phase 3)
**Status:** accepted 2026-11-02 (user, Codex 3.1 review #3); replaces ruling R24

**Context:** A restricted row shows a finding's `id`, code and resource. The hash scheme is public. KGWUI-002 hashed the condition type into its id, so a caller could hash `Accepted` and `Programmed` and learn which hidden condition failed. A low-entropy value is not hidden by hashing it.

**Decision:** An id hashes only the install, the code, the resource and data the resource's spec shows. Hidden parts of the discriminator become ordinals (`#n`): a rank in a stable order among the resource's findings of that code. This covers KGWUI-002 condition types, KGWUI-011 computed parents, KGWUI-012 subjects and status-only owners (policy ancestors, status-reported route parents; ruling R33). Design §5.1 lists the discriminators; §14 rule 11 states the rule.

**Consequences:** A restricted id reveals nothing a restricted row does not. An id can shift when a sibling finding on the same owner appears or clears, and the UI-derived `since` moves with it. Clients compare ids and never parse them.

**Alternatives:** Hash the hidden values (R24); rejected, since a guessed value can be confirmed.

## D-041 — Breadcrumbs resolve through the Authorizer (2026-11-02, phase 3)
**Status:** accepted 2026-11-02 (user, Codex 3.1 review #4); replaces ruling R31

**Context:** The breadcrumb trail lives in the URL (`via`, D-035). A crumb the page did not hold showed its URL name and linked to it, so a deleted object stayed clickable and a hidden object's name showed.

**Decision:** `GET /api/v1/installs/{id}/refs` takes 1–6 `ref` values and returns each as a `LinkedRef`, through the Authorizer. The shell resolves the last 12 object crumbs, in at most two requests (ruling R34); older crumbs get the restricted label without a link. Until the answer arrives, a crumb, the current one included, shows a neutral placeholder, never its URL name. A `none` link shows the restricted label, since hidden and absent read the same (D-013). A network failure retries; only a final answer restricts a crumb.

**Consequences:** No crumb shows a name the caller may not see. A page view costs up to two extra small requests. Very long trails lose links on their oldest crumbs.

**Alternatives:** Trust names held by the page and let the target say "not in scope" (R31); rejected, since the trail then shows names it never checked.

## D-042 — Side column from 1600 px on detail pages (2026-11-02, phase 3)
**Status:** accepted 2026-11-02 (user, Codex 3.1 review #5); replaces ruling R30

**Context:** D-035 asks for two columns on detail pages at wide widths. Under R30, core pages stayed one column, because only contributed sections went to the side.

**Decision:** From 1600 px, conditions and related objects (Gateway conditions, a ListenerSet's parent and conditions, a route's ancestry and parent conditions) sit in a side column beside the structure. Below 1600 px they sit at the top of the main column. The page switches what it renders at the breakpoint, not the CSS order, so tab order follows what is visible. The first render reads the media query, and the skeleton reserves the same split (design §17.6).

**Consequences:** Wide screens show conditions and related objects beside the structure. The browser tests check the column split at 720, 1280, 1600 and 3008 px.

**Alternatives:** CSS `order` or grid placement; rejected, since tab order would then differ from the visual order.

## D-043 — Median-of-five OS-memory gate at a measured value; opt-in chart memory limit (2026-11-03, phase 3)
**Status:** accepted 2026-11-03 (user, Task 46 interim measurement); amends D-022's and design §16.3's 256 MiB OS-memory gate; D-044 makes the 288 MiB gate a reference-machine budget, reported in CI

**Context:**
- The 3.2 measurement world broke the OS-memory gate (≤ 256 MiB). Memory from the OS follows the GC goal: a GC during a maturation compute marks about 115 MB live, so the goal reaches about 230 MB.
- One run per world is noisy: runs on the same code differed by up to 45 MiB.
- A 224 MiB default soft limit passed the stream gate, but thrashed the dense Extreme world (100 × 1000, default edge cap): warm recompute 110 → 312 ms, GC cycles 12 → 395, GC CPU 0.6 → 20.9 s. 90 % of the 512Mi limit (460 MiB) matched no limit. A fixed default would also stop a raised `limits.memory` from raising the target.
- Retained heap broke by 0.5 MiB. The world's routes carried a Service backendRef per referenced Service, seeded for 3.4, which no 3.2 check reads; they cost 1.6 MB.

**Decision:**
1. No default soft limit beyond D-025: the binary sets 90 % of the pod's memory limit, `GOMEMLIMIT` wins, and no pod limit sets none. The chart's `memoryLimit` is opt-in and renders `GOMEMLIMIT`; empty renders nothing.
2. The stream OS-memory gate reads the median of five runs per world, like the heap and dense gates, and is 288 MiB: the worst median of the spec-edit and annotation streams plus about 10 %, rounded up to 8 MiB (256.1 MiB, spec-edit in the measurement world). CPU stays gated in every run.
3. The Service backendRefs leave the 3.2 measurement world. 3.4 seeds them again with the Services and EndpointSlices.

**Consequences:** The gate keeps OS memory inside the 512Mi pod limit with headroom, but no longer at half of it. A stream test takes five times as long.

**Alternatives:** A 224 MiB default soft limit (rejected: the dense thrash above); a lower GOGC (rejected: it costs CPU at every heap size); cutting per-recompute garbage, mostly Phase 2 views and the explorer index (deferred: a larger refactor).

## D-044 — Bench gates split by what they measure (2026-11-04, phase 3)
**Status:** accepted 2026-11-04 (user); amends D-039 and D-043

**Context:** GitHub-hosted runners are shared VMs with variable CPU. On identical code, 3.1's warm recompute ranged 102.9–151.3 ms on CI. The 3.2 memoized analyzer stage with a new scrape each run is 1.14 ms locally and 2.31 ms on CI run 41718646271, against a 2 ms gate. D-039 already moved dense memory to deterministic measures for the same reason.

**Decision:** Three classes of gate.
1. **Deterministic**, gated everywhere, CI included: retained heap (over the 2.1 and 3.1 baselines), dense retained heap and cold and warm allocation (D-039), recompute counts (idle 10 min, burst, stream rate), the matcher page allocation, and the analyzer stage's allocations per run. The stage budgets are the 3.2 measurement plus about 10 %, the same on darwin/arm64 and amd64:

   | World / §16.3-scope mode | Measured | Budget |
   |---|---|---|
   | Large, warm | 173,864 B, 642 allocs | 192,000 B, 710 |
   | Large, memoized | 28,904 B, 491 | 32,000 B, 540 |
   | Large, memoized, new scrape each run | 31,480 B, 503 | 35,000 B, 555 |
   | Measurement world, warm | 5,061,760 B, 18,837 | 5,570,000 B, 20,700 |
   | Measurement world, memoized | 80,024 B, 1,471 | 88,000 B, 1,620 |
   | Measurement world, memoized, new scrape each run | 933,136 B, 5,335 | 1,027,000 B, 5,870 |

2. **Timing regression**, gated in CI: base and head run in the same job, interleaved, five runs each (`make bench-ab`, `internal/benchab`). The gate fails when head's median ns/op is more than 15 % above base's. It covers warm recompute (Large, contradicting, unchanged views, measurement world), cold recompute, the confirm tick at Large and the analyzer stage's three §16.3-scope modes. A benchmark base lacks is reported "new", not gated. The base is the PR's base commit, or the `base` input of a manual run (default `main`). Stream CPU stays reported in CI: five 60 s runs per world and variant take about 20 min a side.
3. **Reference**, absolute time budgets: warm ≤ 150 ms, cold ≤ 1 s, analyzer ≤ 20 / 2 ms, confirm tick ≤ 5 ms, burst p95 ≤ 1.2 s, endpoints p95 < 50 / p99 < 100 / 20 clients p95 < 100 ms, stream CPU ≤ 15 / 25 %, OS memory median ≤ 288 MiB. They gate on the reference machine, an Apple M5 Max at the default GOMAXPROCS (all 18 CPUs), in `make bench`. `KGWUI_BENCH_TIMING=report`, which the workflow sets, logs a miss instead of failing; unset or `reference` gates.

**Consequences:** CI fails on what changes deterministically or on a measured slowdown against the base on the same VM, not on runner speed. The §16.3 time budgets hold only as measured on the reference machine; each sub-phase's local `make bench` record carries them. The workflow runs about twice the engine benchmarks. A PR from a long-lived phase branch compares against `main`, so its A/B reports the whole phase's change.

**Alternatives:** Raise the CI time budgets (rejected: runner speed varies more than the headroom); retry flaky timing gates (rejected: it hides real regressions).

## D-045 — Install page reserves Needs attention from the summary; fallback fonts after the font cap (2026-11-04, phase 3)
**Status:** accepted 2026-11-04 (user, Codex 3.2 review #5 and #6)

**Context:**
- The install page skeleton reserved no Needs attention block, so an unhealthy install pushed Processes and the inventory down when diagnostics arrived (§17.4). A fixed reservation would show an empty box on healthy installs (§6), and the SPA derives no state (ruling R62).
- The first render waits up to 3 s for the bundled fonts. They load with `font-display: swap`, so a font that arrived after the cap still swapped in and could rewrap text.

**Decision:**
1. `InstallSummary` gains `groupRows`: the number of groups `…/diagnostics` lists for the same caller (at most 10). A group with a hidden cause counts, as its restricted row shows.
2. When the summary arrives before diagnostics, the skeleton reserves the loaded block: the heading, `groupRows` rows at the collapsed row height and the "Show all" footer. `groupRows: 0` reserves nothing. Below 720 px a collapsed row is 88 px, so both layouts keep a fixed height.
3. The 3 s font cap stays. When it fires, `renderWhenFontsReady` sets `font-fallback` on the root element, and the theme drops the leading web font from its UI and mono stacks for the session.

**Consequences:** Loading to loaded shifts nothing when the summary is cached, as it is once the header has loaded. A first visit with a slow font request stays on the fallback fonts until reload. A theme's font stacks must lead with their web font.

**Alternatives:** Reserve one row for every install (rejected: healthy installs shift, and others still move); derive the rows from the summary's finding counts (rejected: R62, and groups ≤ findings); amend §17.4 to exempt the loading swap; `font-display: optional` (rejected: a cold load may use the fallback even when the font is fast); no font cap (rejected: a hung request blocks the page).

## D-046 — The 3.2 watches' retained state is its own heap part (2026-11-04, phase 3)
**Status:** accepted 2026-11-04 (user, Codex 3.2 review #7; ruling R75); adds to design §16.3's retained-heap gate

**Context:** The retained-heap gate stops the measurement world's live cache before it measures, so the snapshot's copies count but the proxy Pod, Event and CRD informer stores, the Pod UID index and the Event delta histories do not. Keeping the whole cache would also count the Phase 2 stores and the fake clients' trackers, which no baseline counted.

**Decision:**
1. Each heap child builds a fresh measurement world for its tier and measures the HeapAlloc freed by stopping only the proxy Pod, Event and CRD watches. They run under their own context in `cache.Start`; a `bench`-tagged hook stops them and drops their state.
2. The part is gated on its median of five, deterministic class (D-044): ≤ 1,600,000 B in every world, the measurement world's median plus about 10 %. Measured on darwin/arm64:

   | World | Median | Min–max |
   |---|---|---|
   | large | 1,392,888 B | 1,392,840–1,393,000 |
   | large-contradicting | 1,392,984 B | 1,392,872–1,393,000 |
   | large-contradicting-worst (measurement) | 1,450,216 B | 1,450,136–1,450,344 |

3. The retained-heap gates (≤ 32 MiB over 2.1, ≤ +24 MiB over 3.1) and their baselines are unchanged.

**Consequences:** Growth in the new watches' state fails the heap test on its own, even while the main gate has headroom. The worst world's histories cost about 57 KB. Each heap child builds one more world.

**Alternatives:** Re-baseline the whole retained heap with the live cache kept (rejected: it counts Phase 2 stores and fake trackers); bound the stores by their caps and log them (rejected: R75 asks for a measured gate).

## D-047 — The spec-edit stream gates edit-driven recomputes; grace deadlines are logged (2026-11-04, phase 3)
**Status:** accepted 2026-11-04 (user, Codex 3.2 review #4); amends D-044's "stream rate" for the spec-edit variant

**Context:** The spec-edit stream totals 1.25–1.38 recomputes/s over 10 min, against ≤ 1/s. Each edit leaves the route's status stale, so KGWUI-003 turns from info to warning at first-seen + 60 s (`health.Grace`). D-019 runs that grace deadline without debounce. A deadline run stamps the edits since the last run, so it schedules another run 60 s later. In a 150 s run, all 47 deadline runs raised KGWUI-003 warnings, so these runs are needed.

**Decision:**
1. The stream counts each run by cause in the harness: deadline when the last published `Model.Next` is due at its start, safety 30 s after the last publication, edit-driven otherwise. The engine records no cause.
2. Edit-driven runs are gated ≤ 1/s in the spec-edit stream, 60 s and 10 min, deterministic (D-044). This uses its own flag. Deadline and total rates are logged only. KGWUI-012 assertions stay off for spec-edit.
3. The annotation stream keeps its ≤ 1/s gate on the total.

Measured on the reference machine, 5 runs per world:

| Run | Edit-driven | Deadline | Total |
|---|---|---|---|
| 60 s (`make bench`) | 0.93–0.95/s | 0 | 0.93–0.95/s |
| 10 min (`make bench-specedit`) | 0.51–0.60/s | 0.70–0.81/s | 1.25–1.38/s |

**Consequences:** The 60 s gate has 5–7 % headroom. This is structural: under a steady stream, the debounce runs once per `MaxDelay` (1 s) plus the wait for the next edit. Any change to the debounce or the edit cadence moves that number. Over 10 min, deadline runs absorb pending bursts, so the edit-driven rate falls. Grace deadlines are not bounded by a gate.

**Alternatives:** Gate the 10 min total at a measured value (rejected by the user); let a grace deadline wait for a pending burst (rejected: it amends D-019, and a warning could appear up to 1 s late); have a fake controller catch status up (rejected: it changes what the variant measures).

## D-048 — Proxy stats evidence carries flags and names, not counts (2026-11-04, phase 3)
**Status:** accepted by the user on 2026-11-04, from rulings made during 3.2 execution (rulings R48, R49, R52, R53, R55); recorded at the 3.2 close. Amends design §8.8 and §13.3

**Context:**
- §13.3 gave KGWUI-001 a per-Pod `rejectsSinceLastScrape` count. While a proxy rejects, its `update_rejected` counter grows by hundreds a minute, because the controller pushes again and again.
- A published count changes the collector's fingerprint on every scrape. Every dependent check would then rerun on every scrape, against the D-044 recompute-count gates.
- §8.8 pairs `cluster.<name>.update_rejected` with CDS. In Envoy it is the per-cluster EDS subscription counter.
- §8.8 gates 025 and 026 on a Ready proxy. A downstream stats source can report a Pod the process data does not know.
- §13.3 gives 025 and 026 evidence for one Pod, but each is one finding per Gateway.

**Decision:**
1. Per Pod and xDS type, proxy stats publish `rejecting` (the counter grew since the previous scrape) and `rejectingSince`, plus the Envoy names whose counters grew (`envoyResources`, capped, with `envoyResourcesTruncated`). Never a raw count (R49).
2. Names from `cluster.<name>.update_rejected` are listed under `ClusterLoadAssignment`. CDS rejects carry no names (R53).
3. A stats Pod the process data does not know counts as Ready when Envoy reports `server.state` `live`, which is what Envoy's `/ready` checks. Unknown readiness is not Ready (R52).
4. 025 and 026 evidence is `{kind: "proxy-stats", pods: [{pod, connected?, warmingListeners?, warmingClusters?, serverState?}]}`. A missing field was not reported.
5. On `xds-rejects` evidence, `proxies` and `recentlyChanged` are omitted when empty. A self-managed Gateway has no proxies the UI knows (R48, R55).

**Consequences:** The evidence shows "New rejects, started …", never a number. A growing counter no longer changes the fingerprint on every scrape. A Cluster reject shows `rejecting` on the Cluster type only.

**Alternatives:** The per-Pod count of §13.3, which Codex 3.2 review #3 asked for (rejected: R49, the fingerprint churn above).

## D-049 — Probe type from the whole Pod; skew rules for CRDs and ListenerSets (2026-11-04, phase 3)
**Status:** accepted by the user on 2026-11-04, from rulings made during 3.2 execution (rulings R41, R45); recorded at the 3.2 close. Amends design §9.2 and §9.4

**Context:**
- §9.2 names a probe type when "the container" defines one. The cache keeps no Event `fieldPath`, so the UI cannot tell which container's probe failed.
- §9.4 says a standard-channel Gateway CRD decides the ListenerSet hint. Gateway API v1.5 and later standard CRDs define `allowedListeners`, so the channel alone is not enough.
- §7.1 suppresses all of 022 during a controller rollout; §9.4 states it for proxies only.

**Decision:**
1. The probe type is the one type the Pod's containers define together, else "a probe" (R41).
2. The ListenerSet hint decides from channel and bundle version. Standard before v1.5 and experimental before v1.3 lack `allowedListeners`; a CRD that defines it drops the hint (R45).
3. Every 022 finding, the CRD ones included, is suppressed during a controller rollout and for 10 min after (R45, §7.1).

**Consequences:** A Pod with a sidecar that has its own probe reads "a probe". A CRD skew warning waits for the controller rollout to settle.

**Alternatives:** n/a.

## D-050 — Diagnostics are built per request; summary shapes (2026-11-04, phase 3)
**Status:** accepted by the user on 2026-11-04, from rulings made during 3.2 execution (Task 45 and 49 reviews, ruling R62); recorded at the 3.2 close. Amends design §6, §13.1, §13.3 and §20

**Context:**
- §20 precomputes the diagnostics summary per Model. Retained heap had about 4 MiB headroom at the 3.2 start (3.1 record), and 3.2 watches add to it.
- §13.3 names `ProcessSummary` and `ProxySummary` but gives no fields. The SPA must not derive health (R62).
- §6 shows the controller's restarts in 15 min. The restart check keeps its window per Pod but names one Pod per subject, so a sum over Pods undercounts.

**Decision:**
1. `GET …/diagnostics` is built per request from the published Model and the caller's view. Nothing per caller is retained.
2. `ProcessSummary` is `{state, observed, ready?, desired?, version?, restricted?}`. `state` is the worst controller finding, 005 included; unknown when not observed and none fired (R62).
3. `ProxySummary` is `{state, gateways, ready, rejecting, skew, restarting, notObserved, selfManaged}`, over visible Gateways. `selfManaged` counts Gateways with a KGWUI-010; they are also in `notObserved`. `state` is the worst known Gateway health in the row-state order; a healthy Gateway with unobserved proxies counts as unknown.
4. `ProcessSummary` has no `restartsRecent`. A recent restart shows as a 019 group and on the Processes page, where `ProcessDTO.restartsRecent` is the named Pod's count.
5. `dataPlane` is `{gateways, rejecting, unavailable?}`. `rejecting` counts Gateways with a 001 over every finding (§14 rule 18). `gateways` counts visible Gateways plus hidden ones a restricted 001 already names, so `rejecting` never exceeds it and no hidden count leaks. `withheld` waits for 024 (3.4).
6. `DataPlaneDTO` on the Gateway page adds `rejecting`, `unavailable` (the xDS rejects check is not OK, so `rejecting: false` means unknown), `evidenceRestricted` and `proxyStats {available, unavailable?, reason?}` per Gateway. `snapshot` gains `by: "controller"`.
7. `GroupRow` adds `code`, the top member's code, which restricted rows show. `Verdict` adds `counts` per severity. `ProcessSetDTO` adds `restricted`.
8. `OverviewGatewayRow` gains `dataPlane`: `ok`, `rejects` or `unknown`, computed on the server.

**Consequences:** The worst-case diagnostics request takes p95 0.86 ms sequential and 4.85 ms with 20 clients (restricted, measurement world; `bench/3.2.txt`). The controller line shows no restart count until the restart check exposes every Pod's window.

**Alternatives:** Precompute per Model, as §20 planned (not taken: retained heap headroom).

## D-051 — Next-step commands from a Go seam; the evidence view owns finding renderers (2026-11-04, phase 3)
**Status:** accepted by the user on 2026-11-04, from rulings made during 3.2 execution (rulings R39, R64); recorded at the 3.2 close. Amends design §8.5 and §17.2

**Context:**
- §8.5 takes commands from the SPA `Vocabulary`. Next steps are built on the server, per caller, so that rule 15 hides commands that name hidden objects.
- The install page no longer lists single findings, so finding renderers and actions lost their place there. Two components rendering the same renderer gave it two owners.

**Decision:**
1. Commands come from `diagnose.Commands`, default `diagnose.Kubectl`. A downstream replaces them with `extension.WithCommands`, for a platform without kubectl (R39).
2. `EvidenceView` is the only owner of `findingRenderers`. It renders the code's renderer for an evidence kind the shell does not know, or for a finding with no evidence; a restricted finding renders none (R64).
3. `findingActions` render on the finding page. `FindingRow` still renders both.

**Consequences:** A downstream that changes commands ships Go, not only an SPA build. The README upgrade notes say where renderers and actions moved.

**Alternatives:** n/a.

## D-052 — A memo with a wake time reuses only while scrape times hold (2026-11-04, phase 3)
**Status:** accepted by the user on 2026-11-04, from rulings made during 3.2 execution (Task 52, rulings R69 and R70); recorded at the 3.2 close. Amends design §16.2

**Context:**
- A memo hit returned the wake time of the run that stored it. The xDS rejects check wakes at `Metrics.CollectedAt` + max age, which the input print leaves out. Idle scrapes then kept an old wake and forced a recompute every 45 s: 29 idle recomputes in 10 min against 20.
- Fixing that reruns xDS rejects after every scrape. Its fresh findings slice then made health re-merge the report and the engine rebuild the diagnosis: the memoized analyzer stage took 3.11 ms against 2 ms (R70).

**Decision:**
1. A memo whose result set a wake time is reused only while the times of the metrics and the declared collector values are unchanged (`timesPrint`).
2. A rerun whose findings equal the memo's returns the memo's slice. The merged report and the diagnosis are reused when every check's findings slice is the same.
3. The diagnosis is built only from the inputs its memo key covers: findings, the install, Owned, the views of Owned's objects and the group ids. Next steps read the snapshot per request.

**Consequences:** Idle 10 min is back to 20 recomputes. The memoized stage with a new scrape each run is 1.14 ms on the reference machine (measurement world).

**Alternatives:** Drop the xDS rejects wake time (rejected: the declared-input guard needs it).

## D-053 — An exported config helper and a static metrics source for one-shot callers (2026-11-04, phase 3)
**Status:** accepted by the user on 2026-11-04, for program edge E8 ([#13](https://github.com/DuncanDoyle/kgateway-ui/issues/13)); planned as Task 71a in sub-phase 3.3. Adds to the plan; no design section changes

**Context:**
- kgwctl's `check` runs `engine.New(cfg).Compute()` once over a settled cache, with controller metrics from one `Collect` call (program edge E8).
- `cache.Config` and `engine.Config` are built in `pkg/app/app.go`. `pkg/app` is the UI's wiring, not a library, so a second caller copies the scope, detector, selector and verifier setup and drifts from it.
- `ctrlmetrics.Source` is exported, so a caller can wrap a `Collect` result itself, but its staleness handling can then differ from the UI's.
- #13 asks whether the helper should take the time-based checks into account (restart hold, grace periods, `Report.Next`).

**Decision:**
1. A new package under `pkg/` (`pkg/setup`) builds `cache.Config` and `engine.Config` from the Kubernetes clients, the options and an `*extension.Registry`, the way `pkg/app` does. `pkg/app` builds its own config through it, so the two cannot drift. `pkg/app` keeps its exported names.
2. `ctrlmetrics.Static` is an optional `ctrlmetrics.Source` over one `Collect` result per install. Staleness is the engine's, through `MetricsMaxAge`, the same as in the UI.
3. Time-based waiting stays with the caller. Neither the helper nor the static source waits. A single pass reads the restart hold, grace periods, scope-verifier persistence and metric staleness as pending, and `Report.Next` says when to compute again. The Go doc says so. This answers #13's open question.
4. This is an addition to the versioned `pkg/` API (D-003): new names only, no existing signature changes.

**Consequences:** kgwctl drops its copy of the wiring. `pkg/setup/setup.go` gets an import-boundary allowlist entry (D-016), since it names the client-go interfaces. Task 71a touches 6 files (⚠ guardrail, approved here). Its one-shot test also serves as the E3 acceptance test: an external module runs the analyzer over a cache snapshot.

**Alternatives:** A helper that waits for the time-based checks (rejected: a CLI picks its own timeout and output, and a library that sleeps hides latency). Leave the wiring to each caller (rejected: drift).

## D-054 — Retained heap over 2.1 raised to 40 MiB (2026-11-05, phase 3)
**Status:** accepted by the user on 2026-11-05 (sub-phase 3.3, Task 71). Changes design §16.3's retained-heap gate

**Context:**
- The gate holds the median retained heap at Large to ≤ 32 MiB above the 2.1 baseline. The 32 MiB is our own number (D-022); every sub-phase adds findings and state to it.
- 3.3 measures +34.8 MiB (3.2: +30.8). The growth is the 3.3 checks' findings and state, plus about 1 MiB of derived log events; no regression was found.
- The gates that guard the pod limit pass: OS memory during the stream 210.2 MiB (≤ 288 MiB, D-043; pod limit 512 Mi), and +10.4 MiB above the 3.1 baseline (≤ +24 MiB).

**Decision:** The retained-heap gate over the 2.1 baseline is ≤ 40 MiB. The ≤ +24 MiB gate over the 3.1 baseline and every other gate are unchanged.

**Consequences:** 5.2 MiB of headroom for 3.4. `TestBudgetHeap` gates 40 MiB.

**Alternatives:** Cut retained state to fit 32 MiB (not taken: about 0.2 MiB of savings found; the other gates already bound the pod's memory).

## D-055 — Sub-phase 3.3 as built: deviations from the design (2026-11-05, phase 3)
**Status:** accepted by the user on 2026-11-05; rulings R79, R88, R94, R95, R101, R102, R105 and R108 and the 3.3 task reviews. Amends design §5.2, §5.3, §7.5, §7.7, §8.9, §13.3, §17.2, §17.4 and the 3.3 row of §23

**Context:**
- 3.3 shipped with the choices below, made while building it. Each is verified against the code.

**Decision:**
1. §7.7, R88: `Finding.Fix` is dropped. `diagnose.Fix(f)` builds the suggested fix per request from the finding's evidence alone.
2. §5.3, Task 64: a group's class is its lowest-numbered member class, so its rank follows its most severe member.
3. §5.2, Task 64: the "Not permitted" group key is (from namespace, to namespace, to group and kind).
4. §7.5, R94, R95: rule names are "listener port and hostname" and "listener port and protocol". The condition's reason picks the other side, from listeners kgateway kept only. Other reasons list none.
5. §8.9, R101: the previous-container read asks for the last 200 lines with a 4 MiB client cap and sends no byte limit, which would cut the end of the tail.
6. §8.9, Task 68: KGWUI-001's `since` survives a UI restart only within the 15-minute log backfill.
7. §8.9, Task 72: Envoy never creates a rejected cluster or listener, so CDS and LDS reject names never appear in proxy stats. On real clusters those rejects keep only the error class. Unit tests cover the name-keeping path.
8. §13.3, R108: `GroupRow` gains `grant`, the shape of a "Not permitted" group, which has no cause object.
9. §17.2, Task 69: the web component is `SuggestedFixView`. The type `SuggestedFix` keeps its name.
10. §8.9, Tasks 65 and 66: the log parser types (`LogParser`, `LogWindow`, `DeployerLogClass`) live in `pkg/computed`.
11. §8.9, R105: "withheld" evidence expires 15 minutes after the last qualifying line.
12. §17.4, Task 69: the grouped findings view drops the flat view's filters, and browser Back does not restore them.
13. §23 (3.3 row), R79: `analyze -f` over files is not in 3.3. 3.3 delivers the analyzer over a cache snapshot, proven by the one-shot test (D-053, program edge E3).
14. §8.9, R102: the newest panic keeps a reserved slot in the 1,000-event cap. When that panic is the oldest event, the next oldest is dropped instead, so newer events never evict it.

**Consequences:** The design states the as-built behaviour. Offline snapshot semantics for `analyze -f` (served kinds, discovery, access, install) need a design before they are built. Changing the previous-container read is one constant.

**Alternatives:** n/a.

## D-056 — Two kgwctl E2 requests join sub-phase 3.4 (2026-11-06, phase 3)
**Status:** accepted 2026-11-06 (user)

**Context:** kgwctl filed four non-blocking E2 requests after `v0.3.0`: three missing kinds (#17), a list-only snapshot (#18), namespace-scoped listing (#19) and a way to wait for the cache's goroutines (#20).

**Decision:** #17 and #20 ship in 3.4 as Tasks 78a and 78b. DirectResponse, GatewayExtension and ReferenceGrant get descriptors, appended after the v1 kinds; ReferenceGrants are listed and shown, never evaluated. `pkg/cache` gains a call that returns once every goroutine `Start` launched has exited. #18 and #19 go to Phase 4 planning, because they change how the cache handles access and scope.

**Consequences:** Two more watches' worth of RBAC and memory in 3.4, measured in Task 81. A minor tag after 3.4 carries the new exported API.

**Alternatives:** None of the four in 3.4; all four in 3.4 (a design change to the cache's access model).

## D-057 — Retained heap over 2.1 raised to 48 MiB; Service and EndpointSlice facts shrunk (2026-11-06, phase 3)
**Status:** accepted 2026-11-06 (user)

**Context:** The 3.4 bench (`bench/3.4.txt`) measures +41.9 MiB retained heap over 2.1 in the measurement world, against the 40 MiB gate (D-054). Real data grew 4.4 MB (EndpointSlice facts, the snapshot's fact slices and the routes' Service backendRefs). Freed informer stores leave 6–14 MB of fragmentation that varies between runs. The Service and EndpointSlice watches hold about 960 B per object, not the design's 200 B (design §11).

**Decision:** First shrink the facts toward the design's 200 B per object: EndpointSlice ready counts as a slice indexed like the ports, not a map. Then raise the retained-heap gate to 48 MiB over 2.1. The ≤ +24 MiB over 3.1 gate is unchanged.

**Consequences:** `TestBudgetHeap` gates at 48 MiB. The 3.4 bench is re-run after the shrink. OS memory (275.5 of 288 MiB) is also tight and benefits from the shrink.

**Alternatives:** Shrink only and keep 40 MiB; raise the gate without shrinking.

## D-058 — Sub-phase 3.4 as built: deviations from the design (2026-11-06, phase 3)
**Status:** confirmed by the user (2026-11-06/07); items 7, 10 and 11 revised

**Context:** 3.4 (brief `phases/phase-3/phase-3.4-brief.md`) shipped with these differences from design and plan. Each was ruled during the work (ledger rulings in brackets).

**Decision:**
1. KGWUI-023 has no proxy membership evidence (plan Task 76) [R118]. It needs a forward Service-port-to-Envoy-cluster mapping, which mirrors controller naming. It comes back once a verified, port-aware reverse mapping exists (xdsview `origin`, UI Phase 9).
2. KGWUI-024's warning is tested on a real 2.5.0 capture: a `flip_release` before/after pair from kind with `REFERENCED` xDS scoping, a 20 ms publish budget and a CPU-capped controller (`pkg/ctrlmetrics/testdata/v2.5.0-released-a/-b.txt`). Its error (`warm_withheld`) is tested only on a fixture synthesised in the GA format [R130]. On kind a referenced cluster is missing from CDS for tens of milliseconds at most, so the gauge never outlasts a scrape. The 2.5 e2e row checks that 024 is available with no finding; the release trigger needs settings that would change the whole row, so it is not an e2e case (plan Task 86).
3. 024's availability marker is any series of a 2.5 per-client family or of `kgateway_build_info`, not the withheld and bounded families themselves: a healthy 2.5 controller exports none of those (§8.6). Each signal has its own evidence.
4. `kgateway_build_info`'s `version`, when it parses as a version, is the controller version for the install header, 022 and rule-set selection. A value that does not parse ("undefined") is ignored. Replicas that disagree keep the last agreed version [R132–R134].
5. ReferenceGrant read access returns to the ClusterRole, reversing D-017 item 5 for that kind (D-056). The CRD read stays off.
6. DirectResponse, GatewayExtension and ReferenceGrant are list-only: listed and shown, never judged, and outside `cluster access`, `Synced` and `Settled` [R124, R125]. kgateway writes status on an attached DirectResponse; judging it belongs to Phase 5 (Policies).
7. On 2.5, a `targetSelector` that matches nothing shows 003, not 016 (§7.6). kgateway calls an empty selector valid and never judges selectors. Revised (user, 2026-11-07): 003's "no status from the controller" misreads a processed policy, so 3.5 brings back the computed 016 for this one case on 2.5, labelled UI-computed (queue S7). Exit: kgateway#14998 asks the controller to report it; the UI reads that status once it ships.
8. 023 evidence counts the larger address family of a dual-stack Service, and a port number on both TCP and UDP matches the TCP port.
9. New exported API: `computed.TargetStatusReporter` and `Selection.TargetStatusReporter()` (user, 2026-11-06). A rule set that wraps `kgw25` must forward it.
10. On 2.5, KGWUI-018 has no status-sync count: the controller no longer exports `status_syncs_total` (kgateway #14519). 018 already treats the family as absent. Revised (user, 2026-11-07): the status-sync count goes from the UI on every version; it is not useful (queue S8).
11. A controller whose version cannot be read, from the tag or from build_info, still selects the 2.4 rule set, as before 3.4. Revised (user, 2026-11-07): it selects the 2.5 rule set, the version the UI is positioned on (queue S9). Cost: an older controller whose version cannot be read gets no computed 016, since 2.5 relies on the controller's `TargetNotFound`.

**Consequences:** design §7.6, §8.6 and plan Task 76 state the as-built behaviour. Items 1 and 6 are carried to the phase review.

**Alternatives:** n/a.

## D-059 — Group IDs may hash values the resource's own status serves (2026-11-07, phase 3)
**Status:** accepted (user, 2026-11-07)

**Context:** The phase review (`phases/phase-3/reviews/phase-codex.md` #1) found that KGWUI-015's group ID hashes the replacing policy's name, which §14 rule 11 does not allow for a viewer who may not see that policy. The route's own status message already names the policy, and Phase 2 rule 8 serves that status to the same viewer.

**Decision:** amend §14 rule 11: a `group` may also hash a value the grouped resource's own status already serves to the caller. No code change.

**Consequences:** queue S6 closes without code. Rule 11 now depends on Phase 2 rule 8: if route status stops reaching restricted viewers, 015's group ID must change with it.

**Alternatives:** ordinal grouping: rejected, since a per-route ordinal cannot group routes across the policy, and a cross-route ordinal encodes the policy anyway.

## D-060 — Sub-phase 3.5 as built (2026-11-07, phase 3)
**Status:** confirmed by the user (2026-11-07); the rulings were made while the user was away

**Context:** Stabilization 3.5 (brief `phases/phase-3/phase-3.5-brief.md`) closes phase-review findings #2–#6 and D-058 items 7, 10 and 11. These choices were not fixed by the review or the decisions.

**Decision:**
1. S3: `withheld` (diagnostics), the overview row's `dataPlane: "withheld"` and the Gateway page's `dataPlane.configWithheld` (headline "Config withheld") count a KGWUI-024 **error** only. Its warning (config released early) still updates proxies, so the Data plane cell keeps ✓ or Rejects; the warning shows in the Gateway's state. 001 wins over 024 in the cell.
2. S2: `kinds.Change` splits list-only kinds into their own fields; the served kinds check stays OK with a note "Kinds the UI only lists changed (…); restart the UI to list them".
3. S7: `kgw25` gets a `PolicyTargeter` that judges selectors only (as kgw24's selector rule) and leaves every named targetRef unknown.
4. S8: the status-sync count leaves the exported `pkg/ctrlmetrics` API too, not only the UI; no consumer used it (kgwctl checked).
5. S5: no gated budget; the recorded benchmark (`BenchmarkEndpointsFanIn`) shows 251 → 44 ms at 10,000 routes on one Service port.

**Consequences:** README upgrade notes list the API changes. Design §7.6, §10, §13 and §10.2 state the as-built behaviour.

**Alternatives:** n/a.


## D-061 — Two new phases: the Snellius rebrand and a generic Gateway API core (2026-11-07, roadmap)
**Status:** accepted (user, 2026-11-07)

**Context:** Phase 3 is complete. About 60% of the code is already generic Gateway API. The Policies phase will add kgateway policy semantics, which makes a later split more expensive. The product also needs a name of its own that is not the name of one implementation.

**Decision:**
1. **New Phase 4 — Snellius rebrand.** The UI product is renamed Snellius, tagline "See the whole picture.", descriptor "Kubernetes Gateway Management & Observability". Scope is the UI identity plus the artifacts: GitHub repo, Go module path, container image, Helm chart and binary names.
   - kgateway stays the name of the observed implementation in all content about it.
   - Finding codes keep the `KGWUI-` prefix, so the API does not break.
   - This supersedes one bullet of D-008: "The default theme carries the kgateway identity" becomes "The default theme carries the Snellius identity". The rest of D-008 stands: all identity flows from theme tokens, and downstreams override them. The roadmap's UX requirement 3 ("kgateway identity") changes the same way.
2. **New Phase 5 — Generic Gateway API core.** The base becomes implementation-independent Kubernetes Gateway API. kgateway is plugin #1, on the same extension seam later editions use. No multi-implementation support is claimed. A core-only build is checked in a private compatibility CI, outside this repo, as proof the seam holds (wording amended 2026-11-08). Rough sub-phases:
   - G1: a neutral install and detector, and a default scope.
   - G2: controller metrics behind the collector seam; kgateway-only checks move into a kgateway pack.
   - G3: dedupe the kgw23, kgw24 and kgw25 rule sets; extract a generic view A; delegation becomes optional.
   - G4: a kgateway pack for checks, kinds and condition types.
   - G5: web vocabulary and the delegation tab as contributions.
   - G6: the private compatibility check of a core-only build.
3. **Renumbering.** The old Phases 4–11 become 6–13. Closed records (Phase 1–3 designs, plans, complete files, reviews, and D-001 … D-060) keep the old numbers. Read them with this map:

| Old | New | Phase |
|---|---|---|
| 4 | 6 | Traffic Flow & Backends |
| 5 | 7 | Policies & Filters |
| 6 | 8 | Search & Route Tester |
| 7 | 9 | Security Posture |
| 8 | 10 | Metrics & Insights |
| 9 | 11 | Runtime Filter Chain |
| 10 | 12 | App-Team Views |
| 11 | 13 | AI-Assisted Diagnosis |

**Alternatives:**
- Keep the kgateway-only base and split later: rejected, since the Policies phase would add kgateway semantics to the core first.
- Rename the finding codes: rejected, since `KGWUI-` codes are API v1.

**Consequences:**
- The refactor costs no measurable performance if hooks run per install or per stage, not per object.
- Memory is the tight budget (+39.3 of 48 MiB retained heap, D-057), so the core must own the shared cache.
- `roadmap.md` and `phase-status.md` use the new numbers.

## D-062 — Snellius identity supersedes the kgateway default identity (2026-11-07, phase 4)
**Status:** accepted (user pre-approval, 2026-11-07)

**Context:** D-008 gave the default theme the kgateway identity, so the header presented the UI as the gateway it observes. D-061 renames the product to Snellius.

**Decision:** The default theme and brand carry the Snellius identity. D-008's mechanism is unchanged: a downstream build supplies its own brand and tokens. The kgateway icon stays vendored with its attribution for factual use.

**Alternatives:**
- Keep the kgateway identity as the default: rejected, since the product is not the gateway.

**Consequences:**
- `brand.productName` and the default brand read `snellius`.
- Nothing in the UI uses the kgateway icon after Phase 4; the file stays for factual use.

## D-063 — Phase 4 gains user-testing follow-ups (2026-11-07, phase 4)
**Status:** accepted (user, 2026-11-07)

**Context:** The user tried Snellius on minikube with kgateway 2.5.0 and asked for three changes.

**Decision:** sub-phase 4.2 (planned) adds:
1. The stale banner waits for the first poll after a hidden tab is shown again.
2. A tooltip on a row's Error or Warning in the Gateways, Routes and ListenerSets lists and the overview, showing the deciding finding's code and message and how many more there are. Row DTOs gain a small optional reason; no extra request on hover. Detail and evidence never appear.
3. An About page: product name, descriptor, tagline, UI version, watched installs, and links.

**Consequences:** the row DTOs grow by an optional field (API v1, additive). Phase 4 design §12.

**Alternatives:** fetch the reason on hover: rejected, it costs a request per hover for data the backend already has.

## D-064 — Read-only means "never changes the cluster"; guarded test requests are allowed (2026-11-08, roadmap)
**Status:** accepted (user, 2026-11-08)

**Context:** The user wants to send a test request to a Gateway from the UI while debugging, without switching tools. D-001 says the UI never mutates cluster state. Sending traffic changes no cluster object, but it is an active action: the request must come from the backend (a browser cannot set `Host` or reach in-cluster addresses), and a request can have side effects in the application behind the route.

**Decision:** D-001 is read as "the UI never changes the cluster". The UI may send test requests through a Gateway, in the Search & Route Tester phase, with these guardrails:
1. Off by default; a chart value turns it on.
2. GET, HEAD and OPTIONS only by default; other methods need a separate chart value, and only once per-user login exists.
3. Targets are limited to listeners of Gateways the UI watches; no arbitrary URLs.
4. No redirects followed; fixed timeouts and response size caps.
5. Every request is audit-logged (who, target, method, path, status).

**Alternatives:** keep the tester prediction-only: rejected by the user, the actual result is what makes it useful. Embed an OSS HTTP client (Hoppscotch, Bruno): rejected, full separate apps for a small form.

**Consequences:** RBAC stays get/list/watch; the new surface is network egress from the UI pod to Gateway addresses. Saved test suites, re-runs and load testing stay out of scope (roadmap, Phase 8).


## D-065 — The core-only proof runs in a private compatibility CI (2026-11-08, phase 5)
**Status:** accepted (user, 2026-11-08)

**Context:** D-061 asked for a CI smoke test of a core-only build, as proof the seam holds. Snellius is a kgateway UI and claims no other implementation (D-061). A public CI job against another implementation would read as a support claim.

**Decision:**
1. The proof runs only in a private compatibility CI, outside this repo. Its workflow checks out Snellius and runs a core-only smoke test on kind. Nothing for it runs in this repo's CI.
2. Public docs name no implementation as a test target.
3. The core must not assume Envoy. A controller with a non-Envoy data plane is in scope of the private check, so proxy stats, xDS rejects and Envoy names belong to the kgateway pack.
4. This repo ships what the private CI needs, with no implementation names: a core-only build and a generic smoke test it can run against any cluster (Phase 5 design §12).

**Alternatives:** a public CI job against another implementation: rejected by the user, since it reads as a support claim.

**Consequences:** Setting up the private repo is a Phase 5 G6 task; creating it needs the user's go. A failure there is reported to this repo's maintainers by hand, not by an automated issue.


## D-066 — Phase 5 as built (2026-11-09, phase 5)
**Status:** accepted; each item came from a cross-model review triage

**Context:** Phase 5 (design `phases/phase-5/design.md`) moved kgateway behind a pack. Three choices differ from the design text or were not fixed by it.

**Decision:**
1. `Impl.Version` keeps its signature; `Impl.Reported(install, snapshot)` reads the reported version, and the engine keeps the last one when a scrape fails (design §4.2, 5.2 review).
2. API v1 `features` on installs and `packs` in `/meta` are always sent, `[]` when empty, so the SPA can tell an older server (field absent) from a current one (5.5 review).
3. The compatibility smoke test reports the last complete pass when its deadline cuts one short (5.6, first private run).

**Alternatives:** n/a.

**Consequences:** Design §4.2 and plan Tasks 6 and 13 state the as-built behaviour. README upgrade notes list the v0.6.0 API changes.


## D-067 — The shared flags are a public package (2026-11-09, between phases 5 and 6)
**Status:** accepted (user, 2026-11-09)

**Context:** D-003 has a downstream build bring its own `main`. The flags both binaries share lived in `internal/cli`, which another module cannot import, so a downstream `main` would have to copy them and drift.

**Decision:** The flags move to `pkg/cli`, unchanged. `cli.Parse` still sets the embedded SPA as `Options.UI`; a build from the module cache embeds only the placeholder, so a downstream build sets `Options.UI` to its own web build.

**Alternatives:** Downstream copies the flags: rejected, every new flag would need a downstream change.

**Consequences:** `pkg/cli` is public API (D-003): new flags are additive, and a removed or renamed flag is a breaking change.

## D-068 — Checks declare their holds (2026-11-10, between phases 5 and 6)
**Status:** accepted (user, 2026-11-10; program edge E9, #33)

**Context:** Several checks wait before they decide. Some report OK while they wait, so "What was checked" shows them green, and the pending ones give no due time. kgwctl's `check --live` and the UI after a restart both need to tell "fine" from "still deciding". `WakeAt` cannot tell them: it also schedules the decay of raised findings and input staleness deadlines.

**Decision:** A check declares a hold explicitly. `CheckResult` gains an optional `hold` with `until`, `count` and `reason`. `ok` and `pending` keep their meaning, and the rollup is unchanged. Holds carry counts only, never object refs. They count toward the report fingerprint. The UI shows a third "deciding" state. The work is sub-phase 5.7, before Phase 6.

**Alternatives:**
- Infer holds from `WakeAt`: rejected, because it would mark checks with live findings as deciding.
- Set `ok: false` during a hold: rejected, because one held object would grey out the whole check.
- Derive times from cluster timestamps: rejected by kgwctl, since deletions, retries and webhooks leave none.

**Consequences:** `hold` is public API from the release that carries it. The per-collector next-run time stays out unless a later edge asks for it.

## D-069 — Proxy endpoint health moves after GA (2026-11-10, phase 6)
**Status:** accepted (user, 2026-11-10; #35)

**Context:** The roadmap asked Phase 6 to work with `Backend` objects and Envoy cluster health alone (D-016). Field reports show the proxy side is where the hardest backend problems sit: stale or diverging endpoints per replica, ejected endpoints, limits nobody set. Showing it needs data Snellius doesn't read today. Per-endpoint data is on the proxy admin port, which kgateway binds to localhost. Per-cluster counters need a cluster-to-backend mapping that D-058 deferred. Phase 6 also carries the topology graph and the backend inventory.

**Decision:** Phase 6 covers backends from Kubernetes and from kgateway `Backend` status. Envoy cluster and endpoint health per proxy, and drift between Kubernetes, the controller and the proxies, move to an opt-in proxy endpoint view after GA (#35). Phase 6 keeps its seams open: runtime identity per install, Gateway and proxy; a cluster-mapping hook that may return several results or "unknown"; endpoints modelled by source with completeness; per-proxy sections in backend detail; scoped, time-limited collectors; backend health that can take a later proxy input.

**Alternatives:** Per-cluster counters from the stats Snellius already reads, in Phase 6: rejected for now, since the mapping to backends is the hard part and it would grow Phase 6.

**Consequences:** A backend without Kubernetes endpoints shows `Backend` status only until #35. The Phase 6 design checks each seam above against #35.

## D-070 — Three views on one path model; path list scope and entry filters (2026-11-10, phase 6)
**Status:** accepted (user, 2026-11-10; Phase 6 design §3, §5)

**Context:** Phase 6 answers "where does this traffic go, and is it getting there?" for three people: an operator at scale, an application owner, and someone giving an overview or demo.

**Decision:** One path model (a row per attachment, rule and backendRef) feeds three views: a path list, a route trace in route detail, and a per-Gateway map. The path list opens on every Gateway of the install, problems first, and narrows by server-side filters. The page it opens from sets the starting filter (`gateway=`, `backend=`, `route=`), and every filter lives in the URL.

**Consequences:** All three views and backend detail agree, since they read one model. One broken shared backend reads as one backend with many routes, not many failures.

**Alternatives:** A whole-cluster graph: rejected (roadmap principle 5). A path list per Gateway only: rejected, because the operator needs every affected Gateway at once.

## D-071 — Topology and health memoised separately; delegation from view C; problems over the whole chain (2026-11-10, phase 6)
**Status:** accepted (user, 2026-11-10; Phase 6 design §4.2, §4.3, §4.5)

**Context:** A path joins view A, decoded rules and delegation. Its health changes with collected values and staleness, not only with the snapshot.

**Decision:** Topology is derived from view A, the Delegator's view C and decoded rules, and memoised per install for one snapshot generation. Health is projected onto it per model publication and not reused past its earliest pending deadline. Delegated rows come from view C, and view C's unknown or truncated states carry over. A row's problem is the most severe existing signal along its whole chain; an unknown never hides an error.

**Consequences:** Neither memo outlives one generation. Phase 6 adds no delegation evaluator and no new check.

**Alternatives:** One memo for topology and health: rejected, because health changes far more often than topology.

## D-072 — All route kinds decode rules; route detail for all four, additive DTO (2026-11-10, phase 6)
**Status:** accepted (user, 2026-11-10; Phase 6 design §4.1, §7.2)

**Context:** GRPCRoute, TCPRoute and TLSRoute keep only `{Ref, Mirror}` today, and route detail serves HTTPRoute only.

**Decision:** The three kinds decode their rules: GRPCRoute matches, and backendRefs with port and weight for all three. Route detail and the route trace serve all four kinds. New DTO fields are optional; HTTPRoute responses keep their shape and meaning, and `MatchDTO.Method` stays HTTP-only.

**Consequences:** More retained decoded rules, counted against D-057. API v1 stays additive.

**Alternatives:** HTTPRoute only in Phase 6: rejected, because paths through the other kinds would show no rule or weight.

## D-073 — Share only where Gateway API defines it; failing share only when attributed (2026-11-10, phase 6)
**Status:** accepted (user, 2026-11-10; Phase 6 design §4.4)

**Context:** Gateway API defines weights as proportional within one rule. Delegation splits and unattributed `ResolvedRefs` failures are controller semantics.

**Decision:** Share is weight over the rule's weight sum, shown as "% of this rule's requests". It is not shown in a rule with a delegation ref. A share shows as failing only when KGWUI-013/014 attributes the failure to that ref; other candidates show "may fail". Mirror rows show their percent.

**Consequences:** No share of Gateway traffic is ever shown. Failure wording follows the protocol and promises no status code.

**Alternatives:** Shares across delegation or of the whole Gateway: rejected, as they re-derive translation (principle 4).

## D-074 — Backend detail reads EndpointSlices through a bounded, neutral scoped reader (2026-11-10, phase 6)
**Status:** accepted (user, 2026-11-10; Phase 6 design §9.3)

**Context:** The cached Service and EndpointSlice facts strip addresses and serving state (D-057), and backend detail needs them per endpoint.

**Decision:** A `ScopedReader` interface in the core, implemented by the Kubernetes provider, reads one Service's EndpointSlices on request: a paged, label-selected LIST of at most 10 pages of 10 slices. At most 4 reads run at once, each times out after 2 s, and a memo keeps 32 Services for 5 s. Over a bound the result is partial, timed out or busy. The caller is authorised before any read.

**Consequences:** No watch and no retained state beyond the memo. The reader is the seam #35 reuses (D-069).

**Alternatives:** Keep addresses in the cached facts: rejected by the memory budget. A Pod watch: rejected for the same reason.

## D-075 — Backend judgement and GatewayExtension consumers move behind the kgateway pack (2026-11-10, phase 6)
**Status:** accepted (user, 2026-11-10; Phase 6 design §11)

**Context:** Phase 5 left Backend judgement in the core and promised to move it. Backend detail also needs to show GatewayExtensions that use a backend.

**Decision:** In 6.3, `backendCheck` moves to the kgateway pack's checks with the same ID and findings. The badge goes behind an optional `computed.BackendJudge` hook. `health.BackendState` keeps its signature. GatewayExtension backend refs come from a new pack hook, `extension.Impl.BackendConsumers`, with role and field. Wrapping rule sets forward the new hook (D-058 item 9).

**Consequences:** Without the pack, a Backend shows conditions and no badge. kgwctl keeps compiling.

**Alternatives:** Keep the judgement in the core: rejected, as it is kgateway-specific.

## D-076 — `cache.ListOnce` and namespace-scoped inputs, additive (2026-11-10, phase 6)
**Status:** accepted (user, 2026-11-10; Phase 6 design §12; kgwctl #18, #19)

**Context:** A kind is usable only after a WATCH succeeds, and every input is cluster-wide. A kgwctl user with `list` but not `watch`, or with namespace-scoped RBAC, gets nothing (D-056).

**Decision:** `cache.ListOnce(ctx, Config)` fills the same `Snapshot` from LISTs only. A denied LIST is `denied`, any other error `failed`, and `Mode` says "list" or "watch". The error return is for configuration errors and a cancelled context only. `Config.Namespaces` reads namespaced inputs per namespace; nil or empty means cluster-wide. `Access` holds the worst state over the namespaces, and a new `NamespaceAccess` holds the detail; fingerprints include it. A check denied in a namespace reports "unknown" there, never healthy. A scoped install whose controller is not observed says "controller not observed".

**Consequences:** Snellius itself keeps cluster-wide informers and sees no change. The next tag after 6.1 is a minor.

**Alternatives:** A separate snapshot type for list-only reads: rejected, as kgwctl would need two code paths.

## D-077 — Visible-only rows and counts on the Phase 6 pages (2026-11-10, phase 6)
**Status:** accepted (user, 2026-11-10; Phase 6 design §13)

**Context:** A path exposes its Gateway, ListenerSet, listener, hostnames and delegation ancestors, not only its route and backend. The Authorizer is allow-all today (D-013).

**Decision:** Every new endpoint authorises each row on its whole dependency set per request, memo hits included, before counting and paging. A row the viewer may not fully see is left out, not shown as a placeholder. Summaries, counts and filter options count visible rows only, and the summary says "some paths are hidden from you" when any were.

**Consequences:** This supersedes D-013's count rule on the new pages only. Existing pages keep their rules until Phase 10, and D-013's restricted-finding rule stays on the findings pages.

**Alternatives:** Placeholders for hidden rows, as D-013 counts: rejected, because a path's dependencies would leak through its placeholder.

## D-078 — Phase 6 as built: deviations from the design (2026-11-12, phase 6)
**Status:** rulings made while the user was away; pending user review

**Context:** Phase 6 ran largely while the user was away. Where the build met a question the design did not settle, a ruling was made and logged; the design (`docs/phases/phase-6/design.md`) now describes the result.

**Decision:** These rulings stand until the user reviews them:
1. A zero-ready port takes its severity from KGWUI-023 (warning), not the counts; unknown during the hold (§4.5). If wrong: a broken backend shows as a warning, as on the findings page.
2. Any weight-0 ref is "no traffic", not only when all weights are 0 (§4.4). If wrong: one test changes.
3. Delegated rows take the root's unattributed `ResolvedRefs` failure without the root's 013/014 candidates (§4.5). If wrong: a false error on a delegated path, never a hidden one.
4. Rule decoding is opt-in (`objects.DecodeRouteRules`), off the recompute hot path, to protect the heap (D-039).
5. The topology memo reuses on snapshot `Source` and views generation together, and holds no model or snapshot pointers (§4.3). If wrong: a stale topology until the next change.
6. Summaries gain `facets` and `notComputed`; a class-only install gets an explanation, not paths (§5). If wrong: those installs see no paths.
7. The path and backend lists page on the server (≤ 200 rows) instead of a virtualised table (§5). If wrong: a later virtualisation pass.
8. The scoped-reader memo has a 1 MiB byte budget (§9.3). If wrong: fewer Services kept warm for 5 s.
9. D-057 gates the settled heap after a publication, measured in-child with the engine figure, world built with GC off; Extreme is reported, not gated (§14). At phase close the settled worst world is +46.6 of 48 MiB (warm again +47.4, reported). If wrong: a real process's heap fragmentation can pass the budget.
10. The map chunk budget is 80 kB gzipped, not ~50 kB; React Flow alone is ≈ 60 kB (§6.2). If wrong: 20 kB more on the map tab only.
11. Delegated children show at rest; a delegating route collapses (`collapse`); the rule column is always reserved so backends never move (§6.1). If wrong: a wider resting map.
12. Edge state is the worst row, a lit edge the worst shared row; broken only when every row fails, never under a state filter; partial error has a hollow ring (§6.1). If wrong: one styling rule to revisit.
13. Export covers the whole drawn graph, not the viewport (§6.1).
14. Events stay cluster-wide under `Config.Namespaces` (§12.2). If wrong: a namespace-only user sees Events "denied".
15. No `@axe-core/playwright` scan (a new dependency needs approval); role, name and focus assertions stand in for it.
16. Reversed in 6.7: the Backends `used` filter and its facets count visible routes and consumers only. A Backend only hidden objects use is listed under `used=no` ("no visible consumers"); excluding it there revealed the hidden use (D-077).

**Consequences:** Nothing here changes a budget, a finding code or the meaning of API v1. Each item can be reversed on its own.

**Alternatives:** Wait for the user at each question: rejected under the away rule; only pushes, posts and approvals were queued.

## D-079 — A `backend` link opens backend detail (2026-11-13, 0.7.1)
**Status:** accepted (user, 2026-11-13)

**Context:** Phase 6 added backend detail, but `link` (design §7.1) knew only `detail`, `yaml` and `none`. A group caused by a missing or unready Service read `none`, though its backend page opened from the Backends list.

**Decision:** `link` gains `backend`: the ref is not `detail`, and backend detail serves it to this caller, absent or not. The link and the endpoint decide by the same listing (`paths.Backends` over `backendsInput`). `detail` wins over `backend`; `backend` wins over `yaml` and `none`. The resource endpoint and the reads that need an admitted object still use `admit`. A Needs attention row opens as a whole: its cause, else its one affected object, else its findings (`findings?group=`); a restricted group never opens. A findings list row keeps its two links, the resource and the finding page.

**Consequences:** Adding an enum value to API v1 `link` is additive. Clients treat an unknown value like `none`, as the SPA does. A listed kgateway Backend now opens its backend page, whose YAML tab keeps `admit`'s rule.

**Alternatives:** Derive the page from the kind in the SPA: rejected, links come from the server (§7.1).

## D-080 — Detail pages carry their row's reason (2026-11-13, 0.7.1)
**Status:** accepted (user, 2026-11-13)

**Context:** From 1600 px a ListenerSet's conditions sit in the side column, so its Error badge has no cause in the column read first. The SPA derives no state (R62), and the detail responses had no reason.

**Decision:** Gateway, ListenerSet and route detail gain an optional `reason`: the list row's reason from the same `explore.Worst.Reason`, plus `finding`, the deciding finding's id, on kind `finding`. A problem strip under the header shows it while the state is not healthy, with the first condition the condition list shows as failing (display only) and, on a ListenerSet, the parent Gateway. The header badge links to the strip. Backend detail keeps its status line as the strip.

**Consequences:** API v1 grows by an optional field on three detail bodies (additive). List rows keep D-063's four keys.

**Alternatives:** Pick a condition in the SPA as the cause: rejected, R62. Add the id to the list rows' reason: rejected, the lists have no use for it.

## D-081 — Long routes show their rules as a compact table (2026-11-13, 0.7.1)
**Status:** accepted (user, 2026-11-13)

**Context:** A route's detail page drew every rule open. With dozens of rules, finding one meant scrolling past all the others.

**Decision:** Up to five rules, the page is unchanged. Above five, each rule is one fixed-height row (number and name, match summary, filter count, backends, state) that opens in place to the full rule. A rule's state is the worst `problem.state` of its backendRefs, as the map's rule node shows it; a rule with no problem data shows none. Error and warning rules start open. Above the rows: a Problems only chip, a search box (paths, header and query names and values, methods, filter types, backends) and Expand all / Collapse all. The URL keeps `problems=1`, `q=` and `open=` (1-based rule numbers, `none` when all are closed; absent means the default). `rule=N` opens rule N and scrolls to it, on either layout; the map's rule nodes, Traffic's route cells and a backend's explanations link with it.

**Consequences:** No API change. The detail frame shares its URL params through a context, so a section never adds a second `useUrlParams`.

**Alternatives:** Collapse every route's rules: rejected, short routes read best open. Keep the open rules in local state: rejected, reload and back would lose the place.
