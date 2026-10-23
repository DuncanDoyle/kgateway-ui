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
