# Snellius — Roadmap

**Last updated:** 2026-11-12

**Program:** [kgateway-tooling program.md](https://github.com/DuncanDoyle/kgateway-tooling/blob/main/program.md)

## Goal
A read-only web UI for kgateway (Envoy data plane) that lets platform operators see at a glance whether their gateways are healthy, understand where traffic goes and which policies apply, and debug misrouted traffic in large deployments (100s of hostnames, 1000s of routes). It layers the higher-level semantics of Gateway API and kgateway policies on top of the cluster state, rather than being yet another Kubernetes resource viewer. The project is built so that downstream distributions can extend it without forking or copying code.

## Product principles
These apply to every phase. A phase design that breaks one needs a `decisions.md` entry.

1. **Semantics first, not a CR viewer.** Every page answers an operator question: is it healthy, where does this traffic go, what applies here. Raw YAML is a secondary tab, never the primary view.
2. **Read-only.** The UI never changes the cluster and stays GitOps-friendly. At most it outputs YAML (D-001). It may send test traffic through a Gateway, opt-in and guarded (D-064).
3. **Health is the home page.** It starts thin in Phase 1, and every later phase feeds new signals into it.
4. **Speak Gateway API; never re-implement translation.**
   - Gateway API resources and their status are the primary source, presented in the user's language.
   - A route that is Accepted and Programmed is treated as active as specified; the UI does not verify the data plane against the spec. It does show every signal that contradicts that status — xDS rejects, dropped or replaced rules, proxy readiness, and (Phase 11) differences between the config the controller sent and the config Envoy accepted — and never reports green while one of them is active (D-006).
   - Envoy/xDS config is evidence, not vocabulary. It is used only where Gateway API objects cannot answer the question, such as filter order or rejected config, and is always mapped back to the Gateway API objects that produced it.
   - The UI must never re-implement kgateway translation logic.
5. **Built for 1000s of routes.** From Phase 2 on, indexing, filtering and pagination happen on the server. A whole-cluster graph is never a landing page.
6. **Works without an external metrics backend**, and gets richer when one is present. The first integration targets PromQL-compatible backends (Prometheus, Thanos, Mimir, VictoriaMetrics, …), behind an interface that downstreams can implement for others (D-007).
7. **Extensible.** Resource kinds, health checks, analyzers and pages register through a registry. A downstream distribution adds to it without touching core code (D-003).
8. **Never handles secret material.** The default RBAC has no Secret read access.

## UX & visual design
**Baseline:** the frontend design and frontend conventions guidelines that the agent tooling applies to this project:
- motion, layout, themes, contrast (at least WCAG AA), components, designed empty/error/loading states, and UX rules;
- React + TypeScript code structure.

**Project requirements on top of that baseline:**
1. **Open component library.** The repo is public, so the UI builds on an open-source component library (or its own components), never a proprietary one. The choice is made in the Phase 1 design.
2. **Rebrandable by downstreams.** All colour, typography, logo and product-name decisions flow from theme tokens, and a downstream build can override them without forking styles (D-008).
3. **Snellius identity.** The default theme carries the Snellius identity (from Phase 4). kgateway keeps its name wherever the UI shows the implementation it observes (D-061, superseding this point of D-008).
4. **One health vocabulary.** There is a fixed set of states (healthy, warning, error, unknown), always expressed as colour + icon + text, never colour alone. It is used identically everywhere: the banner, lists, detail pages and flow diagrams.
5. **Operator-grade navigation and density.**
   - Every view, including its filters and selection, has a shareable deep link.
   - The UI can be navigated with the keyboard.
   - Dense tables stay usable at 1000s of rows.

## Phases
Phases 4 and 5 were inserted on 2026-11-07 (D-061); the old Phases 4–11 are now 6–13. Closed records keep the old numbers; D-061 maps them.


### Phase 1 — Foundation & Overview
**Status:** Complete (2026-10-21) — `docs/phases/phase-1/phase-1-complete.md`
**Docs:** `docs/phases/phase-1/`

A minimal UI that is deployable and operational and already provides value. It can be installed with Helm or run locally against a kubeconfig, from the same binary (D-002). An operator sees:
- the kgateway logo, version and edition, and the health of the controller;
- counts of GatewayClasses, Gateways, routes by kind and policies by kind;
- the list of Gateways with their status;
- a first health banner: green, or the resources that are not Accepted or Programmed.

It also delivers the build foundation: the Go binary with the embedded SPA, a container image, a Helm chart, CI, and the extension registry seam. It also delivers the design foundation: the chosen component library, design tokens and themes (including the downstream override), and the health states.

### Phase 2 — Gateway & Route Explorer
**Status:** Complete (2026-10-29; `docs/phases/phase-2/phase-2-complete.md`)
**Docs:** `docs/phases/phase-2/`

- Drill into a Gateway: its listeners, ports, hostnames, TLS mode, addresses and attached routes.
- Drill into an HTTPRoute: its rules, matchers, filters, parentRefs, backends, conditions, delegation and ListenerSets.
- Navigate both ways, route → gateway and gateway → routes.
- Lists are indexed, filtered and paginated on the server.

### Phase 3 — Health & Diagnostics
**Status:** Complete (2026-11-07) — `docs/phases/phase-3/phase-3-complete.md`
**Docs:** `docs/phases/phase-3/`

"At a glance": the home page becomes the health overview, ranked by severity. Each finding links to the resource it concerns. Sources:
- an analyzer with stable finding codes: unresolved references, missing ReferenceGrant, rules dropped or replaced, policies whose target is not found, conflicting listeners;
- xDS rejects (NACKs) per gateway;
- controller and proxy pod health.
- "Pod health" is "process health": restarts and readiness come from the provider (D-016).
- A deterministic **suggested fix** where the evidence leaves exactly one correct change, such as the missing ReferenceGrant as YAML. No model: this is the first layer of AI-assisted diagnosis (Phase 13).

Open design question (raised 2026-10-28, decide during Phase 3 planning): should the engine move from full recomputes to incremental ones, for example with Istio's KRT collections, which kgateway itself uses? The gain is cheaper recomputes as clusters and analyzers grow. Phase 2 ended near its CPU budget, at 104–125 ms per full recompute on CI. The costs are a heavy, unversioned dependency, harder per-request consistency, and no reuse of kgateway's own code, because Go cannot link kgateway 2.3 and 2.4 into one binary. Before deciding, spike view A as KRT collections and measure CPU and memory against the current engine on the Large tier. Decided in 3.1 (2026-11-02): keep the immutable full recompute, with per-check memoization (D-027).

### Phase 4 — Snellius rebrand
**Status:** Complete (2026-11-07, v0.5.0)
**Docs:** `docs/phases/phase-4/`

The UI product is renamed Snellius: "See the whole picture.", "Kubernetes Gateway Management & Observability" (D-061).
- The UI identity: name, logo, palette and theme tokens.
- The artifacts: GitHub repo, Go module path, container image, Helm chart and binary names.
- kgateway stays the name of the observed implementation in all content about it.
- Finding codes keep the `KGWUI-` prefix, so API v1 does not break.

### Phase 5 — Generic Gateway API core
**Status:** Complete (2026-11-09, v0.6.0) — `docs/phases/phase-5/phase-5-complete.md`
**Docs:** `docs/phases/phase-5/`

The base becomes implementation-independent Kubernetes Gateway API. kgateway is plugin #1, on the same extension seam later editions use (D-061). No multi-implementation support is claimed; a core-only build is checked in a private compatibility CI, outside this repo, as proof the seam holds.

Rough sub-phases:
1. G1: a neutral install and detector, and a default scope.
2. G2: controller metrics behind the collector seam; kgateway-only checks move into a kgateway pack.
3. G3: dedupe the kgw23, kgw24 and kgw25 rule sets; extract a generic view A; delegation becomes optional.
4. G4: a kgateway pack for checks, kinds and condition types.
5. G5: web vocabulary and the delegation tab as contributions.
6. G6: the private compatibility check of a core-only build.

Why now: about 60% of the code is already generic, and the refactor is cheaper before the Policies phase adds kgateway policy semantics. Hooks run per install or per stage, so there is no measurable performance cost. Memory is the tight budget (+39.3 of 48 MiB retained heap), so the core owns the shared cache.

### Phase 6 — Traffic Flow & Backends
**Status:** Complete (2026-11-12, local branch) — `docs/phases/phase-6/phase-6-complete.md`
**Docs:** `docs/phases/phase-6/`

- Where a route actually sends traffic: Services and Backends, weights, endpoint health and endpoint discovery status.
- A scoped flow diagram centred on a single gateway or route.
- A Backends list (kgateway `Backend` objects, Kubernetes Services and any other kind a route sends traffic to). Selecting one shows every route that routes to it, through which Gateways, listeners and hostnames: how this backend is exposed. It is the inverse of the route page's "other routes using this backend" (user, 2026-11-07).
- Must work with `Backend` status alone; Services and EndpointSlices are optional (D-016). Envoy cluster and endpoint health from the proxies moves to the proxy endpoint view after GA (D-069, #35); Phase 6 keeps the seams for it.

### Phase 7 — Policies & Filters
**Status:** Not started
**Docs:** `docs/phases/phase-7/`

For each feature (CORS, JWT, ext-auth, rate limiting, transformations, …):
- where it is attached;
- the direct, inherited and effective policy for a route, with provenance;
- merge and override conflicts;
- drill-down into the actual feature configuration.

**Design question:** kgateway's merge semantics are translation logic (principle 4). The effective view must be built from policy ancestor status (`Merged`, `Overridden`, …), not from our own merge implementation. If that status is insufficient, the gap is raised upstream.

**Design item (user, 2026-11-13): two signals per route.** Each route gets its own state and a separate traffic-path state, as the Gateway table's Data plane column sits beside its state. `gateway-rejecting` and `backend-problem` (D-082) move from the state into the path signal, and policy problems inherited from a parent join it. Phase 10 adds observed traffic (errors, 5xx) to the same signal.

### Phase 8 — Search & Route Tester
**Status:** Not started
**Docs:** `docs/phases/phase-8/`

- Search across 100s of hostnames and 1000s of paths.
- "Why does this request go here?": given a host, path, method and headers, show the winning listener, route and rule, and its backends.
- Show the rules that lost, and why, and shadowed or conflicting routes.
- **Send the request (opt-in):** send it to a Gateway listener from the UI and show the prediction next to the actual response (status, timing, Envoy's upstream headers). A mismatch links to the finding that explains it, e.g. KGWUI-023 for a 503 to a backend with no ready endpoints. Guardrails per D-064.
- **Copy as curl:** a `kubectl port-forward` plus `curl` pair that reproduces the request in a terminal or a ticket.
- Not in scope: saved test suites, re-runs or load tests. Snellius is an admin's debugging tool, not a testing platform like Postman or JMeter (user, 2026-11-08).

**Design question:** route precedence is defined by the Gateway API spec, so evaluating it over accepted routes is "speaking Gateway API". kgateway-specific behaviour (delegation, rule replacement) is the risk. The design decides whether the controller's xDS snapshot is used as a cross-check.

### Phase 9 — Security Posture
**Status:** Not started
**Docs:** `docs/phases/phase-9/`

- TLS, mTLS and backend TLS indicators on flows (lock icons).
- Listener TLS modes.
- Routes without authentication or authorization.
- An opt-in certificate inventory with expiry dates (opt-in because it needs Secret access, see principle 8).
- Certificates may come from files, not Secrets (D-016).

**Requirement to design (user, 2026-11-12): updates and CVEs.** Supply-chain pressure makes "am I on a patched version?" a core posture question.
1. Show when a newer release, patch releases first, exists for Snellius itself and for each observed platform (kgateway, and any distribution a downstream adds through an extension seam).
2. Later: list the CVEs the running versions are affected by (critical, high and maybe medium), and whether an upgrade fixes them.

**Design questions:** this is the UI's first outbound call, so it is opt-in and must work air-gapped (an offline feed or a mirror URL); which sources to trust (release feeds, OSV or GitHub advisories) and how to show their freshness; image digests versus version tags; how a downstream plugs in its own release and advisory feed.

### Phase 10 — Metrics & Insights
**Status:** Not started
**Docs:** `docs/phases/phase-10/`

- Golden metrics (traffic, errors, latency) shown in context on gateway and route pages, sourced from the proxy and controller metrics endpoints.
- An optional integration with a PromQL-compatible metrics backend for longer windows, with links out to Grafana for history.
- An exploration of access logs and traces, assuming they are exported via OpenTelemetry (D-007).

**Design question:** where this UI's job stops and the job of the metrics platform starts.

### Phase 11 — Runtime Filter Chain
**Status:** Not started
**Docs:** `docs/phases/phase-11/`

- The actual Envoy HTTP filter chain and filter order per route, mapped back to the Gateway API and policy resources that produced it.
- Intended configuration (the controller's xDS snapshot) compared with the configuration Envoy accepted.
- Builds on the `pkg/envoy` library of [xdsview](https://github.com/DuncanDoyle/xdsview) (xdsview Phase 4, v0.1.0). xdsview owns the mapping from Envoy names to Gateway API refs, and `computed.EnvoyNamer` delegates to it (program P-001, edges E4/E5).
- Easier standalone, because the admin server (9095) and Envoy admin are local (D-016).

### Phase 12 — App-Team Views
**Status:** Not started
**Docs:** `docs/phases/phase-12/`

- OIDC login and impersonation.
- Namespace-scoped views, so an application developer sees only their own routes and their health.
- SubjectAccessReview and impersonation are one `Authorizer`, behind a `kubernetes-rbac` capability; a neutral authorisation story is a Phase 12 design question (D-016).

### Phase 13 — AI-Assisted Diagnosis
**Status:** Not started
**Sequence:** after Phase 8, before Phase 9. It keeps the last number so references to Phases 9–12 stay valid.
**Docs:** `docs/phases/phase-13/`

"Explain this problem and suggest a fix", on a finding, a Gateway or a route. It builds on three layers:
1. Deterministic suggested fixes (Phase 3), which need no model.
2. The MCP server milestone, which lets any agent reason over the UI's data.
3. In this phase, an assistant inside the UI, behind a pluggable model provider. Any OpenAI-compatible endpoint works, which includes a local model, so no data has to leave the network.

The evidence from Phases 6–8 (backends, policies, the route tester) is what makes its suggestions good, which is why it comes after Phase 8.

**Constraints, from the product principles:**
- Optional and off by default, behind a provider interface like the metrics backend (principle 6, D-007).
- Read-only: suggestions are text and YAML the user applies; the UI never acts (principle 2, D-001).
- The model sees only what the viewer may see (the `Authorizer`), and never Secret material (principle 8).
- Every suggestion cites the evidence it rests on and is labelled as a suggestion. A model's guess about kgateway behaviour is not translation logic and is never presented as fact (principle 4).
- Cluster text (annotations, status messages, names) is untrusted input to the model. Prompt injection can at worst produce a bad suggestion, never an action.

**Design questions:** offline validation of suggested YAML (CRD schemas; a server-side dry-run needs write access); whether a kgateway debugging skill ships with the MCP server; how suggestions are evaluated before release.

**Idea to design (user, 2026-11-08): the agent proposes a test request.** To confirm a hypothesis ("the 503 is the empty endpoint slice"), the assistant proposes one route-tester request (Phase 8). A human reviews it and clicks Send; the agent never sends traffic itself. Same guardrails as D-064. The response goes back to the assistant as evidence. It is one request to confirm a diagnosis, not a test run.

## Milestones outside the phase sequence
- **Extension seam proof (after Phase 2):** runs as Phase 3 sub-phase 3.0 (2026-11-01). Besides kinds and detection, it adds the seams a downstream needs to ship a whole feature without forking: API endpoints, background collectors, check inputs, capabilities, a published shell package, in-page slots and chart extension values. `examples/downstream` uses every seam and runs in CI, so the proof is kept rather than thrown away. This validates D-003 before later phases build on it.
- **MCP server (after Phase 3):** API v1 exposed as MCP tools: installs, findings with their evidence and suggested fixes, and the computed views. It grows as later phases add API, such as the route tester (Phase 8). It is a thin, read-only adapter over the API. It goes through the same `Authorizer` and adds no new data access. Any agent, cloud or local, can then diagnose a kgateway install and propose fixes. Needs Phase 3.1's stable finding IDs, and API v1 released.
- **User manual (before Phase 13, user 2026-11-06):** user-facing documentation, written before Phase 13 (AI-Assisted Diagnosis), since its suggestions point users at it. It draws on `docs/documentation-notes.md`, and includes a scalability section: what the UI watches and holds in memory per kind, why some watches are cluster-wide (Services and EndpointSlices, D-031), and measured sizing per cluster size.

## Deferred / out of roadmap
- **Per-user authentication and authorisation** (D-013). OIDC login, Kubernetes SubjectAccessReview per request against the shared cache, with health roll-ups that still show which resource is wrong when the user may not see its details (D-013). It plugs into the Phase 1 `Authorizer` seam. Required before the UI reads Secrets (Phase 9 certificate details).
- **Pre-apply "what-if" analysis of pasted YAML** (like gwctl `analyze -f`). Valuable, but write-path adjacent; revisit after Phase 8.
- **A kgateway CLI** is now a planned separate project. It imports `pkg/cache` and `pkg/engine` for a one-shot load (edge E2) and the Phase 3.3 analyzer as a package (edge E3). The MCP server milestone covers part of this need.
- **A Headlamp plugin.** It would be a second surface; one surface comes first.
- **A support-bundle export.** Revisit after Phase 11.
- **Proxy endpoint view** (#35, D-069). Per-proxy endpoints, health and drift against Kubernetes and the controller, opt-in. After GA; needs an upstream kgateway option for read-only `/clusters`.
- **The agentgateway data plane.** It has its own controller, so this roadmap covers the Envoy data plane only.
- **GRPCRoute, TCPRoute and TLSRoute.** Not in Phase 2 scope (HTTPRoute first). Added when a phase design pulls them in.

## Assumptions
- Supported kgateway versions: the current OSS minor and the one before it (N, N-1). The exact matrix is set in the Phase 1 design.
- Tech stack: a Go backend and a React + TypeScript frontend, confirmed in the Phase 1 design.
