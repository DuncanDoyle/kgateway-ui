# kgateway UI — Roadmap

**Last updated:** 2026-10-16

## Goal
A read-only web UI for kgateway (Envoy data plane) that lets platform operators see at a glance whether their gateways are healthy, understand where traffic goes and which policies apply, and debug misrouted traffic in large deployments (100s of hostnames, 1000s of routes). It layers the higher-level semantics of Gateway API and kgateway policies on top of the cluster state, rather than being yet another Kubernetes resource viewer. The project is built so that downstream distributions can extend it without forking or copying code.

## Product principles
These apply to every phase. A phase design that breaks one needs a `decisions.md` entry.

1. **Semantics first, not a CR viewer.** Every page answers an operator question: is it healthy, where does this traffic go, what applies here. Raw YAML is a secondary tab, never the primary view.
2. **Read-only.** The UI never mutates the cluster and stays GitOps-friendly. At most it outputs YAML (D-001).
3. **Health is the home page.** It starts thin in Phase 1, and every later phase feeds new signals into it.
4. **Speak Gateway API; never re-implement translation.**
   - Gateway API resources and their status are the primary source, presented in the user's language.
   - A route that is Accepted and Programmed is treated as active as specified; the UI does not verify the data plane against the spec. It does show every signal that contradicts that status — xDS rejects, dropped or replaced rules, proxy readiness, and (Phase 9) differences between the config the controller sent and the config Envoy accepted — and never reports green while one of them is active (D-006).
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
3. **kgateway identity.** The default theme uses the kgateway project's logo and palette, within the terms of its artwork licence, rather than inventing a new brand.
4. **One health vocabulary.** There is a fixed set of states (healthy, warning, error, unknown), always expressed as colour + icon + text, never colour alone. It is used identically everywhere: the banner, lists, detail pages and flow diagrams.
5. **Operator-grade navigation and density.**
   - Every view, including its filters and selection, has a shareable deep link.
   - The UI can be navigated with the keyboard.
   - Dense tables stay usable at 1000s of rows.

## Phases

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
**Status:** In progress — sub-phases 2.1 (event-driven recompute) and 2.2 (explorer on observed status) complete; 2.3 next
**Docs:** `docs/phases/phase-2/`

- Drill into a Gateway: its listeners, ports, hostnames, TLS mode, addresses and attached routes.
- Drill into an HTTPRoute: its rules, matchers, filters, parentRefs, backends, conditions, delegation and ListenerSets.
- Navigate both ways, route → gateway and gateway → routes.
- Lists are indexed, filtered and paginated on the server.

### Phase 3 — Health & Diagnostics
**Status:** Not started
**Docs:** `docs/phases/phase-3/`

"At a glance": the home page becomes the health overview, ranked by severity. Each finding links to the resource it concerns. Sources:
- an analyzer with stable finding codes: unresolved references, missing ReferenceGrant, rules dropped or replaced, policies whose target is not found, conflicting listeners;
- xDS rejects (NACKs) per gateway;
- controller and proxy pod health.
- "Pod health" is "process health": restarts and readiness come from the provider (D-016).

### Phase 4 — Traffic Flow & Backends
**Status:** Not started
**Docs:** `docs/phases/phase-4/`

- Where a route actually sends traffic: Services and Backends, weights, endpoint health and endpoint discovery status.
- A scoped flow diagram centred on a single gateway or route.
- Must work with `Backend` and Envoy cluster health alone; Services and EndpointSlices are optional (D-016).

### Phase 5 — Policies & Filters
**Status:** Not started
**Docs:** `docs/phases/phase-5/`

For each feature (CORS, JWT, ext-auth, rate limiting, transformations, …):
- where it is attached;
- the direct, inherited and effective policy for a route, with provenance;
- merge and override conflicts;
- drill-down into the actual feature configuration.

**Design question:** kgateway's merge semantics are translation logic (principle 4). The effective view must be built from policy ancestor status (`Merged`, `Overridden`, …), not from our own merge implementation. If that status is insufficient, the gap is raised upstream.

### Phase 6 — Search & Route Tester
**Status:** Not started
**Docs:** `docs/phases/phase-6/`

- Search across 100s of hostnames and 1000s of paths.
- "Why does this request go here?": given a host, path, method and headers, show the winning listener, route and rule, and its backends.
- Show the rules that lost, and why, and shadowed or conflicting routes.

**Design question:** route precedence is defined by the Gateway API spec, so evaluating it over accepted routes is "speaking Gateway API". kgateway-specific behaviour (delegation, rule replacement) is the risk. The design decides whether the controller's xDS snapshot is used as a cross-check.

### Phase 7 — Security Posture
**Status:** Not started
**Docs:** `docs/phases/phase-7/`

- TLS, mTLS and backend TLS indicators on flows (lock icons).
- Listener TLS modes.
- Routes without authentication or authorization.
- An opt-in certificate inventory with expiry dates (opt-in because it needs Secret access, see principle 8).
- Certificates may come from files, not Secrets (D-016).

### Phase 8 — Metrics & Insights
**Status:** Not started
**Docs:** `docs/phases/phase-8/`

- Golden metrics (traffic, errors, latency) shown in context on gateway and route pages, sourced from the proxy and controller metrics endpoints.
- An optional integration with a PromQL-compatible metrics backend for longer windows, with links out to Grafana for history.
- An exploration of access logs and traces, assuming they are exported via OpenTelemetry (D-007).

**Design question:** where this UI's job stops and the job of the metrics platform starts.

### Phase 9 — Runtime Filter Chain
**Status:** Not started
**Docs:** `docs/phases/phase-9/`

- The actual Envoy HTTP filter chain and filter order per route, mapped back to the Gateway API and policy resources that produced it.
- Intended configuration (the controller's xDS snapshot) compared with the configuration Envoy accepted.
- Builds on [krp](https://github.com/DuncanDoyle/krp) as a Go library, which first requires moving its parser, model and filter packages out of `internal/`.
- Easier standalone, because the admin server (9095) and Envoy admin are local (D-016).

### Phase 10 — App-Team Views
**Status:** Not started
**Docs:** `docs/phases/phase-10/`

- OIDC login and impersonation.
- Namespace-scoped views, so an application developer sees only their own routes and their health.
- SubjectAccessReview and impersonation are one `Authorizer`, behind a `kubernetes-rbac` capability; a neutral authorisation story is a Phase 10 design question (D-016).

## Milestones outside the phase sequence
- **Extension seam proof (after Phase 2):** a throwaway downstream build, in a separate repository, registers one extra policy kind and edition detection. This validates D-003 before later phases build on it.

## Deferred / out of roadmap
- **Per-user authentication and authorisation** (D-013). OIDC login, Kubernetes SubjectAccessReview per request against the shared cache, with health roll-ups that still show which resource is wrong when the user may not see its details (D-013). It plugs into the Phase 1 `Authorizer` seam. Required before the UI reads Secrets (Phase 7 certificate details).
- **Pre-apply "what-if" analysis of pasted YAML** (like gwctl `analyze -f`). Valuable, but write-path adjacent; revisit after Phase 6.
- **A CLI sharing the analyzer library.** Possible once the Phase 3 analyzer exists as a package.
- **A Headlamp plugin.** It would be a second surface; one surface comes first.
- **A support-bundle export.** Revisit after Phase 9.
- **The agentgateway data plane.** It has its own controller, so this roadmap covers the Envoy data plane only.
- **GRPCRoute, TCPRoute and TLSRoute.** Not in Phase 2 scope (HTTPRoute first). Added when a phase design pulls them in.

## Assumptions
- Supported kgateway versions: the current OSS minor and the one before it (N, N-1). The exact matrix is set in the Phase 1 design.
- Tech stack: a Go backend and a React + TypeScript frontend, confirmed in the Phase 1 design.
