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
- Phase 7 (TLS posture) must not read Secrets under this model; certificate details wait for per-user authorisation or another source.
