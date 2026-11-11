# Phase 6 — Traffic Flow & Backends: Design

**Status:** Draft, revised after the builder check and the Codex design review (`reviews/design-codex.md`). Awaiting user approval. Shaped with the user in a mockup session on 2026-11-10. Calls marked (our call) are open to change.
**Date:** 2026-11-10
**Base:** `657378ad` (recorded at phase start; the design adds no code)
**Roadmap:** `docs/roadmap.md`, Phase 6
**Decisions:** D-006, D-013, D-014, D-016, D-031, D-039, D-052, D-056, D-057, D-058, D-069 in `docs/decisions.md`. This design proposes D-070 … D-077 (§15).
**Code refs:** `file:line` at `22709b7` (docs only since the base).

**One home per contract.** Path model: §4. Path list: §5. Gateway map: §6. Route trace: §7. Backends: §8. Endpoints and the scoped reader: §9. Reference explanations: §10. Packs: §11. Cache access for kgwctl: §12. Visibility: §13. Budgets: §14.

## Decisions for this phase

| # | Decision | Marker | Where |
|---|---|---|---|
| 1 | Three views on one model: a **path list** for operators at scale, a **route trace** for one application, a **Gateway map** for the overview | (user, 2026-11-10) | §3 |
| 2 | The path list opens on every Gateway of the selected install, problems first, and narrows by filter. The page you come from sets the starting filter | (user, 2026-11-10; Codex opinion) | §5 |
| 3 | The Gateway map is per Gateway, with filtering. A large Gateway opens filtered | (user, 2026-11-10; threshold our call) | §6 |
| 4 | A Backends list and a backend detail page with "how is this backend exposed" | (user, 2026-11-07 and 2026-11-10) | §8 |
| 5 | The runtime filter chain (Phase 11) and the per-proxy endpoint view (#35) are out of scope. The route trace and backend detail leave a marked slot for each | (user, 2026-11-10; D-069) | §2, §7.3, §9.5 |
| 6 | Path topology is derived from view A, the Delegator's view C and decoded rules, memoised per snapshot. Health is projected onto it per model publication. Neither is retained beyond one generation | (our call) | §4 |
| 7 | GRPCRoute, TCPRoute and TLSRoute decode their rules, weights and ports. Route detail and the route trace cover all four route kinds, with kind-specific match fields | (our call) | §4.1, §7.2 |
| 8 | Share is shown only where Gateway API defines it: within one rule, with no delegation ref, and failing shares only for attributed refs | (our call) | §4.4 |
| 9 | Reference explanations reuse the KGWUI-013/014 attribution and diagnose's suggested ReferenceGrant. ReferenceGrants are still never evaluated (D-056) | (our call) | §10 |
| 10 | Backend detail reads one Service's EndpointSlices on demand, through a bounded, neutral scoped-reader interface. It is the seam #35 reuses | (our call) | §9.3 |
| 11 | No Pod watch. The Service fact gains the targetPort name. Named targetPorts are not resolved to container ports | (our call; memory) | §9.4 |
| 12 | Backend judgement (KGWUI-002 and the Backend badge) and GatewayExtension backend refs move behind the kgateway pack, in 6.3 | (carry-over from Phase 5 §1) | §11 |
| 13 | Phase 6 adds views, not checks. Every problem shown comes from an existing finding, condition or capability state | (our call) | §4.5 |
| 14 | The Gateway map uses React Flow with our own fixed-column layout, and is as interactive as possible | (user, 2026-11-10) | §6 |
| 15 | `cache.ListOnce` (kgwctl #18) and namespace-scoped inputs (kgwctl #19), additive to the Go API | (D-056) | §12 |
| 16 | Every new endpoint authorises rows and their dependencies per request, memo hits included. Counts on the new pages count only what the viewer may see | (user, 2026-11-10) | §13 |
| 17 | No budget change. D-057 is a hard gate: a sub-phase that overshoots shrinks its retained state first, and asks the user before any raise | (user, 2026-10-29, carried; fallback our call) | §14 |

## 1. Intent and why

The question this phase answers is "where does this traffic go, and is it getting there?" The field research for this phase (about 300 support cases, private) shows the recurring failures:
- A route is green while its backend has no ready endpoints.
- A reference fails and the reason is unclear or misreported.
- Weights are not what the operator thinks.
- Ports don't line up between the route, the Service and the pods.
- Nobody can say quickly which routes and Gateways a broken backend affects.

The research also had a strong signal about per-proxy endpoint drift. That needs data Snellius cannot read today, so it moved after GA (D-069, #35).

The user named three people and moments (2026-11-10):
- **Platform operator at scale.** Manages hundreds or thousands of routes. Someone reports a problem, and the operator must see which Gateways, listeners and routes are affected. "This would be my go-to view." → path list.
- **Application owner, or onboarding.** "Customers can't reach the HR system via this URL." Needs to see which rule of which route sends traffic to which backend. → route trace.
- **Overview and demos.** "It just looks pretty"; filtering helps reading it. → Gateway map.

## 2. Goals, non-goals and anti-goals

**Goals**
1. A path list over every Gateway of an install, problems first, with server-side filter, sort and paging (§5).
2. A route trace inside route detail for all four route kinds (§7).
3. A Gateway map with filtering (§6).
4. A Backends list and a backend detail page (§8).
5. Explanations for references that don't resolve, with the fix (§10).
6. kgwctl #18 and #19 (§12).
7. The seams #35 and Phase 11 need, stated as requirements, without building them (§7.3, §9.5, §11).

**Non-goals**
- Per-proxy endpoints, Envoy cluster health or drift (#35, after GA).
- The effective filter chain (Phase 11).
- Policy semantics on a route or backend (Phase 7). A policy shows only as "uses this backend through GatewayExtension X" (§8.3).
- A whole-cluster graph (roadmap principle 5).
- New health checks (§4.5).

**Anti-goals**
- Re-deriving what kgateway sends to Envoy: cluster names, effective weights after translation, delegation weight semantics, container port resolution (principle 4).
- Showing an incomplete read as "no endpoints" (D-069 requirement 2 applies already).
- Any view that is green while a contradicting signal is active (D-006).
- Inferring intent. A rule without backends is described by what it does, never as "fine by design".

## 3. The three views

| View | Opens from | Scope | Question it answers |
|---|---|---|---|
| Path list | Install nav "Traffic"; a Gateway; a backend; a route | All Gateways of the install, narrowed by filters | What is affected, and where? |
| Route trace | Route detail, Rules section | One route | Which rule sends this traffic where? |
| Gateway map | Gateway detail, new "Map" tab | One Gateway | What does this Gateway serve? |

All three, and backend detail, read the same path model (§4). Every backend in every view links to backend detail, and every route links to route detail.

## 4. The path model

### 4.1 What a path is

A **path** is one way traffic can flow: Gateway → listener → effective hostnames → route (→ delegated route …) → rule → backendRef. There is one row per (attachment, rule, backendRef).
- A row carries the attachment's effective hostnames as a list; it does not expand per hostname. `sort=hostname` sorts by the first hostname in order; `q` matches any (our call, builder Q3).
- A ListenerSet attachment projects to its parent Gateway. The row shows the ListenerSet and the Gateway.
- A rule with no backendRef is one row whose backend is the **written action**: redirect, direct response (via `extensionRef`), or "no backend". No intent is inferred (Codex #3).
- A mirror is a row with a `mirror` flag and its percent.

Inputs, all existing:
- **Attachments** from view A: resource, listener, route, parentRef, effective hostnames and dependency set (`pkg/computed/viewa/attach.go:321`). Hostname intersection is Gateway API spec logic and already lives there.
- **Rules and backendRefs** from the decoded route. HTTPRoute keeps ref, port, weight and filters (`pkg/objects/httproute.go:87-95`). GRPCRoute, TCPRoute and TLSRoute keep only `{Ref, Mirror}` today (`pkg/objects/backend.go:44-81`). Phase 6 decodes their rules: GRPCRoute matches (method service/method/type, headers), and backendRefs with port and weight for all three. RequestMirror is also decoded for GRPCRoute. This decodes the spec; it is not translation.

Fields per row: install, Gateway, ListenerSet, listener (name, protocol, port), hostnames, route chain (root route, then delegated routes), rule index, match summary, backend ref (kind, ref, port), weight, share (§4.4), mirror, action, and **problem** (§4.5).

### 4.2 Delegation (Codex #2)

View A excludes delegated children that have no Gateway or ListenerSet parent (`pkg/engine/views.go:113`). Their paths come from the Delegator's existing result (`pkg/computed/rules.go:17-26`, `pkg/computed/delegation.go:102-114`):
- A child's rows hang under its root's attachment, through the view C rows and contexts that reach it. A row carries the route chain, the delegation provenance, and the row's dependency set.
- A child rule's match on a path is the view C context for that root attachment, not the child's rule as written. The join is bounded by view C's own bounds and truncation (`DelegationInput.Bounds`, `Row.ContextsTruncated`); a truncated row shows "more paths not computed".
- Where view C marks a row unknown, unconfirmable, not computed or truncated (`delegation.go:380-405`), the path row says so and its problem is at least "unknown". Phase 6 adds no delegation evaluator.
- Without a Delegator (a class-only install), a backendRef to an HTTPRoute is an ordinary ref of kind HTTPRoute, and there are no child rows.

### 4.3 Topology and health, memoised separately (Codex #5)

- **Topology** (attachments × rules × refs, with dependency sets) is derived from the snapshot and the computed views. It is memoised per install for one snapshot generation. A row is a compact struct of indexes into existing objects, ≤ 48 bytes.
- **Health projection** maps each row to its problem from findings, conditions and capability states. It is memoised per model publication: the engine's published model identity, which changes with collected values and staleness (`pkg/engine/loop.go:11-31`), not only with the snapshot. Like D-052, the memo also holds the earliest pending deadline (finding holds, persistence, scrape expiry) and is not reused past it.
- Sorting happens on the projected table. Paging and authorisation happen per request (§13).
- `explore.Index.ByBackend` (`pkg/explore/explore.go:168-173`) stays as is.

Cardinality: rows = Σ over attachments of Σ over rules of max(1, refs), plus delegated rows. §14 sets the gate.

### 4.4 Share (Codex #6, builder Q2)

Gateway API defines weights as proportional within one rule. Share = weight ÷ sum of the rule's weights, shown as "% of this rule's requests". It is never shown as a share of Gateway traffic.
- Share is shown only for rules with no delegation ref. In a rule that mixes delegation and ordinary refs it is "not shown", because the split there is controller semantics (principle 4).
- When every weight in the rule is 0, every ref shows "0 %, no traffic".
- A ref the route's status reports as unresolved shows its share as **failing** only when KGWUI-013/014 attributes the failure to it. Unattributed candidates show their share as written plus "may fail". The rule-level bar marks the rule as having an unresolved ref.
- Failure wording follows the protocol: "requests fail" for HTTP and gRPC, "connections fail" for TCP and TLS. No status code is promised.
- Mirror rows show their mirror percent, not a share.

### 4.5 Problems come from existing signals (Codex #1)

A row's problem is the most severe signal along its **whole chain**. Error beats warning beats unknown beats healthy; an unknown never hides an error.
1. **Gateway and listener:** Gateway `Accepted`/`Programmed`, listener conditions, and Gateway-scoped findings: xDS rejects (`pkg/health/xds.go:272`), proxy readiness and disconnects (`pkg/health/proxystats.go:162`, `pkg/health/checks.go`).
2. **Route:** the parent status for this attachment (`Accepted`, `ResolvedRefs`), stale or missing status ("waiting for the controller", D-014), and the existing route findings (dropped or replaced rules).
3. **Delegation:** the view C state on delegated rows (§4.2).
4. **Backend:** KGWUI-002 (Backend not accepted), KGWUI-013/014 when attributed to this ref, and KGWUI-023 (no ready endpoints) for this backend **port**.
5. **Coverage:** "unknown" when a check this row depends on is unavailable, for example the endpoints capability is off, denied or failing for a Service ref. Never "healthy".

Backend-resource refs (kgateway `Backend`) have no endpoints; their endpoint cell is "not applicable", and their state comes from the Backend's conditions (builder Q7).

Rows sort problem-first by default (error, warning, unknown, healthy), then by Gateway, listener, route and rule. The order is global, applied before paging.

## 5. Path list

**Page:** `/i/:id/traffic`, nav item "Traffic" after "Routes" (`web/packages/shell/src/shell/InstallNav.tsx:65-70`). The name is our call; SPA copy goes through the wording pass.

**API:** `GET /api/v1/installs/{id}/paths` with these params:
- `gateway` (ns/name, repeatable), `listener`, `route` (group/kind/ns/name, matches any route in the chain), `backend` (group/kind/ns/name, repeatable, `core` for the empty group as in `pkg/api/params.go:28`), `namespace`, `state` (error, warning, unknown, healthy), `kind` (route kind), `mirror` (include, only, exclude), `q` (text over hostnames, route and backend names);
- `sort` (`problem.asc` default; `gateway`, `route`, `backend`, `hostname`), `cursor`, `limit` (1–200, default 50), as in `pkg/api/params.go:50-128`.

**Response:** the rows, plus a **summary of the whole filtered result**: affected paths, distinct routes, Gateways and backends, per state. One broken shared backend then reads as "1 backend, 37 routes, 4 Gateways", not 37 failures. The summary also says when coverage is incomplete ("endpoints not read") so a count never implies complete coverage.

**Entry points set the filter:** nav → all Gateways; Gateway detail → `gateway=`; backend detail → `backend=`; route detail → `route=`. Every filter, the sort and the cursor live in the URL (`web/packages/shell/src/explore/listQuery.ts:92-124`). Changing a filter resets the cursor.

**Scale:** 1000s of rows render in the existing virtualised table. Filters are chips, per the team's UI rules.

## 6. Gateway map

The user wants the map "as interactive as possible" (2026-11-10).

### 6.1 Behaviour

The map is a "Map" tab on Gateway detail, with four columns: Gateway → listeners → routes → backends. Node states use the one health vocabulary (colour + icon + text). It reads the same filter params as the path list, server-side, with `gateway=` fixed. Delegated routes appear in the routes column under their root, marked as delegated.

Interactions (user, 2026-11-10):
1. **Pan, zoom, fit to view and a minimap.**
2. **Hover highlights** every path through a node and dims the rest. Edge thickness follows the share where share is shown (§4.4).
3. **Side panel on click:** the node's state, problems, endpoints and links, without leaving the map.
4. **Expand and collapse:** a route opens into its rules, and a delegating route into its children.
5. **Focus mode:** a pinned node reduces the map to its upstream and downstream paths. The selection is in the URL.
6. **Search** to locate a node, and **filter chips** (problems only, kind, namespace) that update the map live.
7. **Animated flow** on healthy edges and broken edges drawn as broken. Off under `prefers-reduced-motion`.
8. **Keyboard navigation** between connected nodes. A table toggle shows the same rows as the path list, as the accessible equivalent.
9. **Export** of the current view as PNG or SVG.

**Large Gateways open filtered (our call):** when the Gateway has more than 60 routes or 60 backends, the map opens on "Problems only" with a prompt to narrow. The threshold is a constant, tuned on the Large fixture.

### 6.2 React Flow, own layout (user, 2026-11-10)

The canvas uses React Flow (`@xyflow/react`, MIT), the first graph dependency in the web UI. It brings pan, zoom, minimap, viewport, keyboard handling and custom nodes as React components. Its styles are overridden with theme tokens, so every theme applies.

The layout is our own: fixed columns, with nodes in a column ordered by the barycentre of their connected nodes to reduce crossings. No layout engine is added (ELK alone is about 1.4 MB). Expand and collapse re-run the layout for the changed columns only.

The map loads lazily as its own chunk, so pages without it don't pay for it.

## 7. Route trace

### 7.1 Behaviour

Route detail's Rules section (`web/packages/shell/src/pages/explore/route/RuleList.tsx`) becomes the trace:
- Above the rules: the attachments (Gateway, listener, effective hostnames) the route serves through, with their state.
- Per rule: the match, the filters as written, a stacked share bar (§4.4), then one line per backend with port, weight, share, endpoint state and problem.
- The existing "routes using this backend" link becomes the backend detail link.

### 7.2 All route kinds (our call; Codex #13)

`routeDetail` returns 404 for kinds other than HTTPRoute (`pkg/api/details.go:469-477`). Phase 6 serves GRPCRoute, TCPRoute and TLSRoute details. Compatibility:
- HTTPRoute responses keep their exact shape and meanings, including the default `PathPrefix "/"` match.
- GRPCRoute matches use new optional fields (`grpcMethod {service, method, type}`, headers). `MatchDTO.Method` stays HTTP-only. TCPRoute and TLSRoute rules have no `matches`.
- `BackendRefDTO` (`pkg/api/explore_types.go:488-496`) gains optional `share`, `shareState`, `problem` and `endpoints` (§9.5). Nothing existing changes meaning.

### 7.3 The Phase 11 slot

Each rule has a marked "Filters" area that today shows the filters as written. Phase 11 adds the effective chain next to them; the written filters stay. The DTO reserves `rules[].effectiveFilters`, absent today. No UI text promises the feature.

## 8. Backends

### 8.1 What is listed (our call; Codex #12)

- Every kgateway `Backend` in the install's scope, used or not.
- Every Service or other kind written as a backendRef by an admitted route of the install, **whether or not its attachment succeeded**, and every backend a non-route consumer names (§8.3). A backend with refs but no successful path shows "0 confirmed exposures" and the attachment's failure or uncertainty.
- Unreferenced Services are not listed: at the Extreme tier that would be 25,000 rows of noise.

**Page:** `/i/:id/backends`, nav item "Backends". **API:** `GET /api/v1/installs/{id}/backends` with `kind`, `namespace`, `state`, `used` (yes, no), `gateway`, `q`, `sort` (`problem.asc` default, `name`, `used`), cursor and limit. Chips as in the mockup: Problems, Services, Backend resources, Unused, Namespace, Gateway.

Columns: backend, kind (Service, or `Backend · <spec.type>`), endpoints (ready/total for the referenced ports, or "not applicable"), used by (routes, Gateways, other consumers), problem.

### 8.2 Backend detail

The page is at `/i/:id/backends/:group/:kind/:ns/:name`, with `-` for the empty group as in `web/packages/shell/src/explore/objectPath.ts:6` (builder Q6). The API is `GET /api/v1/installs/{id}/backends/{group}/{kind}/{ns}/{name}`. Sections, in this order (the user approved the mockup):
1. **Status line** naming the impact, built from the problems (§4.5), for example "2 routes on 2 Gateways send traffic here; no endpoint is ready, so requests fail". It uses fixed phrases per signal and never promises a status code (builder Q8).
2. **How this backend is exposed:** the path list filtered by `backend=`, with Gateway, listener, hostname, route rule, share and ref state. Then the non-route consumers (§8.3).
3. **Endpoints**, per referenced port, by source (§9).
4. **Port chain** (§9.4).
5. **Reference explanations** for refs to this backend that don't resolve (§10).
6. For a Backend: conditions and YAML, as on other detail pages. For a Service: the cached facts (type and ports). The Service cache holds facts, not objects, so there is no Service YAML tab (our call; Codex #8).

A Backend detail also shows `spec.type`. The rest of the Backend spec (static hosts, AWS, DNS) is not decoded (our call).

### 8.3 Non-route consumers

The kgateway pack decodes GatewayExtension backend refs (§11). In the kgateway API these are `spec.extAuth.grpcService.backendRef`, `spec.extProc.grpcService.backendRef`, `spec.rateLimit.grpcService.backendRef` and the HTTP-service `backendRef` (kgateway `api/v1alpha1/kgateway/gateway_extensions_types.go:49-75, 138-164, 262-265`; verified against the plan's kgateway version in 6.3). The page lists each as "used by GatewayExtension X (ext-auth)". Which policies use that extension is Phase 7. Otherwise GatewayExtension stays list-only and is not judged (D-058 item 6).

## 9. Endpoints

### 9.1 Per port, from the cache

The list, the rows and the backend detail summary use the cached facts. `SliceFact` ready and total are counted **per Service port** exactly as KGWUI-023 counts them: the Service port is matched by number (preferring TCP), slice ports are matched by the Service port's name, and the larger address family is used (`pkg/health/endpoints.go:205-234`). Codex #7 found that one Service-wide number could contradict a path row; this avoids that.

### 9.2 Completeness

Every endpoint summary carries a source, a read time and a completeness: complete, partial, failed, unavailable or stale. Only a complete read establishes zero. ExternalName Services and Backend resources are "not applicable", not zero.

### 9.3 Detail: the scoped reader (our call; Codex #9)

The cached facts strip addresses, serving and terminating (D-057), and the mockup's per-address table needs them. Backend detail therefore reads that one Service's EndpointSlices on request.
- **Interface in the core, implementation in the Kubernetes provider (D-016).** `ScopedReader.Endpoints(ctx, install, service) (EndpointsResult, error)`. A provider without it reports "unavailable", and the section shows the cached counts only.
- **Read:** a namespaced LIST with `kubernetes.io/service-name=<name>`, paged with `limit=10` slices and at most 10 pages. A slice holds at most 100 endpoints by default, so a page decodes at most about 1,000 endpoints. A continuation beyond the last page, or more than 1,000 endpoints in all, marks the result **partial**.
- **Process-wide bounds:** at most 4 reads in flight, and duplicate requests coalesce. Each read times out after 2 s. The memo keeps at most 32 Services for 5 s each, least recently used first, and at most 1,000 endpoints per Service. Over a bound the section says "partial", "timed out" or "busy, retry".
- **Authorisation first:** the caller must be allowed to see the backend, on every request and on memo hits too, before any read (§13).
- **Shown per endpoint:** address, port, ready, serving, terminating, and the target Pod name when the slice names it.
- **RBAC:** list EndpointSlices, which the chart already grants for the endpoints capability. There is no watch and no retained state beyond the memo.

### 9.4 Port chain (Codex #8)

`ServicePort` keeps the numeric target only, and drops a named targetPort (`pkg/cache/services.go:39-46, 114-138`). Phase 6 adds `TargetName string` to the fact. It's additive; the names are short and few.

The chain is written configuration plus observed slice ports: backendRef port → Service port (by number, preferring TCP, as 023 does) → targetPort (a number, or a name) → slice ports observed for that port name. It describes; it does not judge. Two cases are facts on the page, not new checks:
- **"The Service has no such port"** already comes from KGWUI-013 when the controller reports it.
- **"No slice has a port named X"** shows only when the endpoints read is complete.

Named targetPorts are not resolved to container ports. That needs Pods, which the memory budget does not allow.

### 9.5 Seams for #35 (Codex #15)

The endpoints section and `BackendRefDTO.endpoints` use one shape:

```go
// EndpointsDTO is one backend port's endpoints from one source.
type EndpointsDTO struct {
    Source   string        `json:"source"`          // "kubernetes" now; "controller", "proxy" later (#35)
    Port     *PortDTO      `json:"port,omitempty"`  // the backend port this summary is for
    Runtime  *RuntimeDTO   `json:"runtime,omitempty"` // per-proxy sources only (#35)
    ReadAt   time.Time     `json:"readAt"`
    Complete string        `json:"complete"`        // complete, partial, failed, unavailable, stale, not-applicable
    Ready    *int          `json:"ready,omitempty"` // absent unless the read establishes a count
    Total    *int          `json:"total,omitempty"`
    Items    []EndpointDTO `json:"items,omitempty"` // detail only
}
```

Backend detail returns `sources: []EndpointsDTO`. Requirements recorded now for #35, so that it can add sources without an API break:
- `RuntimeDTO` identifies install, Gateway and proxy process, and changes when a proxy is replaced.
- A cluster mapping may return several results or "unknown".
- Each source has its own completeness.

The scoped reader (§9.3) is the first user of the bounded on-demand collection #35 needs (D-069 seam 5). The cluster-mapping hook (D-069 seam 2) is not built.

## 10. Reference explanations

The route's `ResolvedRefs` condition is per parent, not per ref. Today KGWUI-013/014 attributes it to one ref only when exactly one candidate remains (`pkg/health/refs.go:265-332`, `:322`). Phase 6 shows, per ref:
- **Attributed:** the finding's explanation in plain words, the route's reason (`RefNotPermitted`, `BackendNotFound`, `InvalidKind`), and the fix. For `RefNotPermitted` the fix is the ReferenceGrant diagnose already suggests (`pkg/diagnose/fix.go:32-108`).
- **Not attributed:** on each candidate, "This route reports `ResolvedRefs=False` (reason) for one of these refs". It does not guess which one.
- **Not reconciled yet** (status older than the spec): "waiting for the controller", never "failed" (D-014).

ReferenceGrants are still not evaluated (D-056), and the candidate set is narrowed only by the existing finding logic. The controller's status stays authoritative.

## 11. Packs

Phase 5 left Backend judgement "inert without pack kinds" and promised to move it (Phase 5 design §1 item 2; `docs/phase-status.md:45`). It moves in 6.3. 6.2 consumes today's `health.BackendState` and the KGWUI-002 findings unchanged (builder Q5).
- `backendCheck` (`pkg/health/backend.go:43-77`) moves from `health.Core()` (`pkg/health/checks.go:35`) to the kgateway pack's checks, with the same ID and findings.
- The badge goes behind an optional hook, `computed.BackendJudge`, modelled on `TargetStatusReporter` (`pkg/computed/targets.go:76-87`): a one-method interface on the rule set, plus a `Selection` accessor that returns nil. Without it, a Backend shows its conditions and no badge. `health.BackendState` keeps its signature and calls the kgateway pack's judgement directly, so kgwctl keeps compiling. New code goes through the hook.
- GatewayExtension backend refs come from a new pack hook `extension.Impl.BackendConsumers` (`pkg/extension/pack.go:34-53`). It returns, per install, `[]Consumer{Consumer Ref, Role string, BackendRef objects.Ref, Port int, Field string}`. The role is ext-auth, ext-proc, rate-limit or http. Nil means none. Phase 7 needs the role and field provenance (Codex #15).
- Wrapping rule sets must forward the new hook, as D-058 item 9 requires.

## 12. Cache access for kgwctl (#18, #19)

Both are additive Go API. Nothing kgwctl uses today changes type or meaning (Codex #10, #11).

### 12.1 `ListOnce` (#18)

`cache.ListOnce(ctx, Config) (*Snapshot, error)` fills the same `Snapshot` an informer cache would, from LISTs only:
- the same inputs as `Start`: every served kind, Namespaces, the label-selected controller and proxy Pods and Deployments, and the optional facts the Config enables, with the same selectors, transforms, sorting and fingerprints (`pkg/cache/cache.go:64-107, 619-675`);
- `Access` per kind: a successful LIST is `ok` with no watch attempted; a denied LIST is `denied`; any other error is `failed`;
- `Synced` is true when every required input is `ok` or `denied`; `TakenAt` is the time the last LIST returned; `Source` stays the fingerprint it is today (`pkg/cache/cache.go:94, 674`), and a new `Mode` field says "list" or "watch";
- list-only and optional kinds keep their existing meaning;
- the error return is only for a configuration error or a cancelled context. A partial result is a snapshot with per-kind `Access`.

### 12.2 Namespace-scoped inputs (#19)

`cache.Config.Namespaces []string`. Nil or empty means cluster-wide, as today. The list is deduplicated and sorted.
- Namespaced served kinds, Services, EndpointSlices and the selected Pods and Deployments get one informer (or one LIST) per namespace. Cluster-scoped kinds (GatewayClass, Namespace) are attempted cluster-wide and reported in `Access` as usual.
- `Snapshot.Access` keeps its type, `map[GVR]Access`, as the aggregate: `ok` only when every namespace is `ok`, else the worst state. A new `Snapshot.NamespaceAccess map[GVR]map[string]Access` holds the detail. Fingerprints include it.
- **Consumers:** Snellius does not set `Namespaces`, so nothing changes for it. A library user reads `NamespaceAccess` to say which namespaces it could not read. A check whose input is denied in some namespaces reports "unknown" for objects there, never healthy.
- **Install detection outside the list:** an install whose controller is not observed is reported as "controller not observed", which is different from a known install outside scope. Assignment still runs on what is visible (answer to #19's open question; our call).
- Snellius itself keeps cluster-wide inputs. The option is for library users.

The next tag after 6.1 is a minor.

## 13. Visibility (user approved 2026-11-10; Codex #4, builder Q1)

The Authorizer is allow-all today (D-013). A path exposes more than its route and backend: Gateway, ListenerSet, listener, hostnames, and delegation ancestors.

**Built regardless (the seam):**
- Each topology row carries its dependency set (§4.3).
- Every request authorises each row by `allowDeps` (`pkg/api/visible.go:88`) on its dependencies, after the viewer-independent memo and before counting and paging. Memo hits are authorised too.
- Backend detail authorises the backend before reading anything, the scoped reader included.
- With allow-all this costs one fast path (`allowsAll`), as today.

**Approved by the user, 2026-11-10 (D-077):** on the new pages, a row whose dependencies the viewer may not all see is left out, not shown as a placeholder. Summaries, counts and filter options count only visible rows, and the summary says "some paths are hidden from you" when any were. This supersedes D-013's count rule for the new pages only; existing pages keep their rules until per-user authorisation (Phase 10). D-013's restricted-finding rule (severity and identity of a hidden resource's finding) stays on the findings pages.


## 14. Budgets (Codex #14, builder Q4)

- Retained heap stays within D-057, +48 MiB over the 2.1 baseline. The worst case at 5.6 is +40.0 MiB (`docs/phases/phase-5/bench-5.6.txt`), so about 8 MiB is left. This is a hard gate.
- **New retained state:**
  - decoded rules for GRPCRoute, TCPRoute and TLSRoute;
  - the targetPort name on Service facts;
  - the topology memo (≤ 48 bytes per row);
  - the health projection memo;
  - the scoped reader's memo (≤ 32 Services × 1,000 endpoints).
- **Row gate:** at 48 bytes, 8 MiB is about 175,000 rows. The topology memo gets its own cap of 2 MiB per install generation. Above the cap, the paths endpoint pages straight from derivation without a memo and reports it in the response.
- **Measured after warming every API cache:** the bench gains an API-level pass. It requests paths, backends and backend detail for every install, many distinct Services, and overlapping generations, then measures retained heap. The engine-only bench cannot see these memos.
- **Fixtures:** the scale generator already makes GRPCRoute, TCPRoute and TLSRoute at 5 % each, mostly with one rule and one ref (`internal/testutil/scale/scale.go:257-262, 632-654`). The Large and worst fixtures gain multi-rule, multi-ref and delegated routes so rules × refs expansion is exercised.
- **Per request:** at Large, the cold and warm paths endpoint, and concurrent cold backend details, are measured for time and bytes allocated. These are new request gates, separate from D-039's recompute gates. Starting targets, tuned in 6.2: paths under 200 ms and 32 MiB allocated cold, under 20 ms and 2 MiB warm; backend detail under 4 MiB allocated, the scoped read included.
  - Tuned in 6.2 (`bench-6.2.txt`), paths cold: under 100 ms and 32 MiB allocated (measured 31 ms, 28.4 MiB).
  - Tuned in 6.2, paths warm: under 5 ms and 2 MiB allocated (measured 1.2 ms, 1.7 MiB, restricted).
- **Overshoot:** a sub-phase that overshoots shrinks first. For example, it drops the topology memo or derives GRPC, TCP and TLS rules on request. It asks the user before proposing a budget raise.
- The SPA gains `@xyflow/react` (about 50 kB gzipped), loaded lazily with the map. The other pages add no dependency.

## 15. Proposed decisions

| ID | Title |
|---|---|
| D-070 | Three views on one path model; path list scope and entry filters |
| D-071 | Topology and health memoised separately; delegation from view C; problems over the whole chain |
| D-072 | All route kinds decode rules; route detail for all four, additive DTO |
| D-073 | Share only where Gateway API defines it; failing share only when attributed |
| D-074 | Backend detail reads EndpointSlices through a bounded, neutral scoped reader |
| D-075 | Backend judgement and GatewayExtension consumers move behind the kgateway pack |
| D-076 | `cache.ListOnce` and namespace-scoped inputs, additive |
| D-077 | Visible-only rows and counts on the Phase 6 pages (user, 2026-11-10) |

## 16. Sub-phases

| # | Content | Depends on |
|---|---|---|
| 6.1 | Cache access: `ListOnce`, namespace-scoped inputs (§12). Tag after it for kgwctl | — |
| 6.2 | Path model: rule decoding for all kinds, delegation join, topology and health memos, share, problems; `paths` API with the authorisation seam; path list page; API-level bench pass (§4, §5, §13, §14) | — |
| 6.3 | Backends: pack moves, consumers, list, detail, scoped reader, targetPort name, port chain, explanations (§8–§11) | 6.2 |
| 6.4 | Route trace and route detail for all kinds (§7) | 6.2 |
| 6.5 | Gateway map: canvas, layout, pan/zoom/minimap, hover highlight, side panel, search and filters (§6.1 items 1–3, 6, 8) | 6.2 |
| 6.6 | Gateway map: expand and collapse, focus mode, animated flow, export (§6.1 items 4, 5, 7, 9) | 6.5 |

Each sub-phase runs the bench and the e2e matrix, and gets a demo reel.

## 17. Left to the builder

- Exact SPA wording, through the wording pass of earlier phases.
- The barycentre ordering details, edge styling and animation of the map.
- Fixture shapes for multi-rule, multi-ref and delegated routes.
- Whether the topology memo is per install or shared across the installs of one snapshot.

## 18. Open questions

1. §13: approved by the user (2026-11-10).
2. The upstream kgateway scope-endpoint issue (a carry-over reminder): file it now or later.
