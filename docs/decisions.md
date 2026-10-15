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
