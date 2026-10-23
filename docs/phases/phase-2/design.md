# Phase 2 — Gateway & Route Explorer: Design

**Status:** Approved by the user 2026-10-23, with the addendum of 2026-10-23 (support-ticket research and planning fixes). Cross-model reviews: `reviews/design-r4-codex.md` (14 findings, then re-review findings N1–N7 and M1–M3, all accepted) and `reviews/design-codex.md` (13 findings and 5 re-review findings, all accepted). The research behind §1 covers support tickets (anonymised), knowledge-base articles and field threads.
**Date:** 2026-10-21, revised 2026-10-22 and 2026-10-23
**Base:** `2c25e35`
**Roadmap:** `docs/roadmap.md`, Phase 2
**Decisions:** D-001 … D-020 in `docs/decisions.md`
**Rules:** `computed-rules.md` is the versioned specification of the computed views, their cross-check and KGWUI-012 persistence. This design refers to it and does not repeat it.
**kgateway refs:** every behaviour claim cites `file:line` at tag `v2.4.7` (`b4a2bf74`) or `v2.3.11` (`c2af2887`). Paths without a `pkg/` or `api/` prefix are under `pkg/kgateway/`. The UI supports both tags.

Choices made without the user are marked **(our call)**. Choices the user made are marked **(user, 2026-10-22)** or **(user, 2026-10-23)**. §15 lists every decided question.

**One home per contract.** Scheduling and timestamps: §3.1. Authorization: §7.4. Computed views and cross-check: `computed-rules.md` §4-9. Ownership: §4.2. Every other section refers to these.

## 1. Goal and scope

Operators debug Gateway API routing by reading `status.parents[].conditions` in YAML, by admin snapshots and by Envoy config dumps. Support tickets (anonymised), knowledge-base articles and field threads show four questions come up most:
- Which routes are on this Gateway, and on which listener?
- Why is this route not attached?
- Which child routes did delegation pick up, and which did it drop?
- Which route is the broken one?

Phase 2 answers these from the cluster's own objects and status. Where status cannot answer, the UI computes three views from the rules the controller applies: route → listener attachment (A), delegation selection (B) and effective matchers (C). Each computed fact is labelled, says what the controller confirmed, and is cross-checked (D-020).

**Staging (user, 2026-10-22).** All three views are in Phase 2, in four sub-phases (§13). Each is reviewed cross-model and is shippable alone: 2.1 the event-driven engine; 2.2 the explorer on observed status; 2.3 view A; 2.4 views B and C.

**In scope**
1. **Gateway detail page** (roadmap): listeners (name, port, protocol, hostname, TLS mode, certificateRef names, never Secret content, allowedRoutes), addresses, Gateway and per-listener conditions with freshness, routes per listener (view A, from 2.3) and attached ListenerSets.
2. **Routes per Gateway, including the failed ones** (new, fits). Next to the attached routes, the page lists routes whose parentRefs target this Gateway but were not accepted, with the controller's reason and message.
   - Only routes whose parentRef targets **this install's** Gateway or ListenerSet are listed, including routes in a namespace the controller does not watch, which get KGWUI-004 (`pkg/install/assign.go:155-190`).
   - Routes for other controllers are never listed.
3. **ListenerSet detail page** (roadmap): parent Gateway, listeners, conditions (including `Accepted=True/ListenersNotValid`), routes per listener and attached routes. It covers ListenerSet v1, XListenerSet and any ListenerSet kind a downstream registers.
4. **HTTPRoute detail page** (roadmap): hostnames and effective hostnames per listener (view A); each parentRef with its conditions and computed attachment; rules with matches, filters and backendRefs (with weight). Backend resolution comes from `ResolvedRefs`. The UI does not re-check ReferenceGrants.
5. **Delegation** (§5): the controller's status, the child's parentRef permission (`v2.4.7 utils/delegation/delegation.go:14-31`), and the computed selection and effective matchers, side by side.
6. **Navigation both ways** (roadmap): route → Gateway up the delegation chain, Gateway → listener → routes, ListenerSet ↔ Gateway ↔ routes, child ↔ parent. Every object has a stable deep link; finding links open these pages; the YAML view stays as a tab.
7. **Server-side lists** (roadmap): Routes, Gateways and ListenerSets per install, indexed once per recompute, filtered, sorted and paginated.
8. **Event-driven recompute** (D-019).
9. **`Backend` kind** (Phase 1 carry-over, design §14.5), with its `Accepted` condition and an assignment rule (§4.3).
10. **Copy helpers**: copy `namespace/name` (exists) and a get-as-YAML command.

**Out of scope**
- Effective policy merge (Phase 5); Envoy/xDS per route, request simulation and precedence (Phases 6 and 9).
- Shadowed or duplicate matches. kgateway writes no condition, and its precedence sort is controller logic (`v2.4.7 translator/routeutils/sortable_route.go:22-37,120-171`). It is an upstream ask (§14); the UI will consume such a condition later **(user, 2026-10-22)**.
- A graph view; detail pages for GRPCRoute, TCPRoute and TLSRoute (they appear in lists and view A counts them; rows open the YAML view); the controller admin server (§3.7); implementing the upstream state resource (§14).

**Anti-goals**
- No computed fact without its label, its coverage and its cross-check (D-020).
- No route precedence, no ReferenceGrant evaluation, no backend resolution, no policy merge.
- No new data source: no admin server, no Envoy admin, no Secrets.

## 2. Primary flows

All URLs are under `/i/:id/` (§8.1). The sub-phase that delivers each step is in brackets.

1. **"Which routes are on this Gateway, and on which listener?"** Open Gateways, filter, open `infra/edge`. The listener table shows the controller's `attachedRoutes` [2.2] and, next to it, the computed count with "Count agrees with the controller", or both numbers and "Disagrees" [2.3]. Selecting a listener filters Attached routes to its computed members, each with its effective hostnames on that listener, labelled "Computed by the UI" [2.3]. Routes through a ListenerSet show it.
2. **"Why is this route not attached?"** The page lists routes with a current `Accepted=False` for this Gateway, with the raw reason (`NotAllowedByListeners`, `NoMatchingParent`, `NoMatchingListenerHostname`, `v2.4.7 query/route.go:525-543`) [2.2]. Each row adds the computed explanation, for example "listener `https` allows namespaces with `team=web`; namespace `shop` has `team=retail`" [2.3]. Routes waiting for status and routes in unwatched namespaces (KGWUI-004) are listed with their own labels and open as diagnostic candidates (§7.1). The route page shows each parentRef's conditions and freshness ("Describes generation 3; the route is at 4") [2.2].
3. **"Which child routes did delegation pick up, and which did it drop?"** Open the parent HTTPRoute. Each delegation ref shows as written [2.2]. Child routes lists every child the controller reports [2.2]. The Delegation tab lists every route each ref selects, labelled "Computed by the UI", with an outcome (attached, attached under some parent matches, dropped with the failing match, forbidden, rejected for hostnames, cycle) and what the controller reports next to it [2.4]. A child page shows the chain to the Gateway [2.2] and its effective matches per parent path [2.4].
4. **"Which route is the broken one?"** Open Routes, filter by state, for example "Rules replaced or dropped", which covers `RouteRuleReplaced` and `RouteRuleDropped` on both versions (§6.1). The filter lives in the URL [2.2].
5. **"Is this ListenerSet working?"** Open it from the Gateway page or the list: parent Gateway, conditions (`Accepted=True/ListenersNotValid` means some listeners failed), listeners and attached routes [2.2], computed counts [2.3].

## 3. Architecture

```
 informer events ─┐  access transitions ─┐  metrics result ─┐  served kinds ─┐  deadlines ─┐  safety timer
                  ▼                      ▼                  ▼                ▼             ▼
            triggers (latency only)  →  scheduler (debounce, single flight)                              D-019
                  ▼
 engine.Compute(snapshot with source fingerprint F)
   install.Discover/Resolve/Assign → computed views (A, B, C) → health.Evaluate → overview.Build → explore.Build
                  ▼
 Published{Model, Source F, ConfirmedAt}   immutable, swapped atomically; confirm tick republishes ConfirmedAt only
                  ▼
 api handlers: Load Published once → filter → authorize (dependencies) → sort → page
```

### 3.1 Scheduling, publication and timestamps (D-019)

This is the only definition of when the model is recomputed and what its timestamps mean **(user, 2026-10-22, event-driven; details our call)**. Phase 1 recomputes on a fixed ticker, `--poll-interval`, 2 s by default (`pkg/engine/engine.go:209-222`, `cmd/kgateway-ui/main.go:23`).

**Triggers.**

| Trigger | Source |
|---|---|
| Object added, updated or deleted | event handler on every informer in `pkg/cache`: dynamic kinds, Namespaces, Pods, Deployments |
| Access transition (D-014) | the list/watch progress hooks and initial sync; a 1 s confirm tick also compares access states as a fallback |
| Metrics scrape result | the collector notifies the engine; the scrape cadence (`--metrics-interval`) stays separate |
| Served kinds change | the kinds monitor |
| Time-driven deadline | the earliest of: grace expiry (design §5.2), metrics staleness (design §5.4), KGWUI-012 and computed KGWUI-011 maturation (`computed-rules.md` §9.3) |
| Safety timer | `--poll-interval`, now "maximum time between recomputes" |

**Triggers give latency, not proof.** client-go updates the informer store first and calls event handlers later, from a separate listener goroutine with its own buffer (`k8s.io/client-go@v0.36.2 tools/cache/controller.go:639-644`, `shared_informer.go:961-965,1356-1360`). A counter bumped in a handler therefore lags the data, and a backlog of notifications could let an old model look current. So triggers only start recomputes. Freshness is decided by the data itself.

**Source fingerprint (our call).** `Snapshot()` already walks every store (`pkg/cache/cache.go:265-305`). In the same pass it computes, per kind, the object count and an order-independent 64-bit hash over each object's (uid, `resourceVersion`), plus every access state. The engine adds the identity of the metrics result and the served-kinds list it read. This tuple is the **source fingerprint** of the snapshot, and it describes exactly the data copied.
- Chosen over an engine-owned mirror store with its own revision: the mirror would itself be filled by the lagging handlers, so it would confirm what the handlers had delivered, not what the informers hold.
- Cost: one pass over the stores per confirm tick, with no decoding and no copying. It is benchmarked in 2.1 (≤ 5 ms per tick at Large, reported at Extreme) **(our call)**.
- A hash collision could hide a change until the next trigger or the safety timer. At 64 bits per kind this is accepted.

**Scheduler.** One goroutine owns the scheduler state. Its states are idle, pending (with a due time) and running.
- **Debounce.** A trigger makes the state pending. The due time is 250 ms after the last trigger, but at most 1 s after the first trigger of the burst **(user, 2026-10-23; §15 Q14)**. One `kubectl apply` or Helm upgrade gives one recompute; a continuous stream still recomputes about once a second.
- **Deadlines** are due at their instant, with no debounce. A deadline whose predicate is strict (metrics staleness uses `age > maxAge`, `pkg/ctrlmetrics/scrape.go:86-87`) is due at the first clock reading after the boundary; one that uses `≥` (maturation) is due at the boundary.
- **Single flight.** At most one recompute runs. Triggers during a run keep the state pending; their due time follows the same rules; one more run follows.
- After a run, the state is pending if a trigger arrived during it. Completing an old snapshot never clears a newer trigger.

**Publication.** A run computes a new immutable `Model` from a snapshot with fingerprint `F`. It then swaps in one `Published{Model, Source: F, ConfirmedAt: snapshot.TakenAt}` with an atomic pointer. Today's `Current()` returns the shared pointer (`engine.go:188-191`); it now returns the `Published` value. No field of a published value is ever changed.

**Confirmation.** The confirm tick runs every second on the scheduler goroutine. When the state is idle and not running, it computes the live source fingerprint. If it equals `Published.Source`, it swaps in a new `Published` with the same Model and Source and `ConfirmedAt = now`. If it differs, it does not confirm and makes the state pending, so a change whose notification has not arrived is still picked up. So:
- an idle, exact model keeps a fresh `ConfirmedAt`;
- a hung or slow recompute stops confirmation, `ConfirmedAt` ages, and the SPA's stale rule fires after 2 × 5 s. D-006 holds: stuck data is never shown green;
- a model built from an old snapshot, or one whose store has moved on before its handler ran, is never stamped as current.

**Polling only reads.** Each request loads `Published` once and serves the Model and `ConfirmedAt` from that one value. A request never renews anything. The SPA keeps its 5 s poll; push (SSE) stays out of scope.

**`--poll-interval`** keeps its name and type. It becomes the safety timer. The default rises from 2 s to 30 s. Setting 2 s restores Phase 1's cadence as a floor **(our call)**.

**Timestamps.** Every timestamp served, and its consumers:

| Field | Served by | Meaning in Phase 2 | Consumers |
|---|---|---|---|
| `computedAt` | `GET /api/v1/installs`, per install (`pkg/api/types.go:25-26`, `handlers.go:172`) | **unchanged**: when the model behind health was computed (`Model.ComputedAt`) | read only as a fallback (below) |
| `confirmedAt` (**new, optional**) | `GET /api/v1/installs`, per install | `Published.ConfirmedAt` | `ClusterLanding.tsx:131`, `InstallSwitcher.tsx:51`, `Header.tsx:161` switch to `confirmedAt ?? computedAt` |
| `collectedAt` | overview (`handlers.go:194`) and every new list and detail body | `Published.ConfirmedAt` (D-019 §4) | `InstallPage.tsx:123-126`, `HealthStrip.tsx:79`, `DataAge` |
| `collectedAt` | health report (`pkg/health/rollup.go:48`, served at `handlers.go:220-222`) | `Published.ConfirmedAt`, set on the handler's copy in `wireHealth`; the stored report is unchanged | `InstallPage.tsx:123`, `FindingsPage.tsx:237`, `HealthStrip.tsx:79` |
| `checks[].collectedAt` | health | unchanged: the time of each check's own evidence | "What was checked" |
| `findings[].since` | health | unchanged | finding rows |
| `Collected-At` header | resource endpoint (`handlers.go:245`) | `Published.ConfirmedAt` | `ResourcePage.tsx:140,182` |

At publication `confirmedAt` is the snapshot time, so it is slightly **earlier** than `computedAt`, which is taken after the snapshot (`engine.go:147-151`). The first idle confirm tick moves it past `computedAt`. Without `confirmedAt`, the cluster cards, switcher and header would turn unknown 10 s after each recompute on an idle cluster. A frontend served by an older backend falls back to `computedAt`. `installs.json` and `bare-installs.json` gain the optional field (§7.6).

### 3.2 Where the index lives

- A new package `pkg/explore` builds one `Index` per install inside `engine.Compute`, after health (`engine.go:169-180`), and stores it on `InstallModel`. It is immutable once published **(our call)**.
- `explore.Build` is a pure function of `(install.Resolved, *install.Owned, *cache.Snapshot, computed views, health.Report, decoded views)`.
- Handlers read only the published Model. Per request they filter and page what the index holds; under a non-`AllowAll` Authorizer they also recount derived counts and check dependencies (§7.4).
- Downstreams get the index through the Model. The only new registry is the computed-rules seam (§3.4).

### 3.3 Decoded views are cached across recomputes

Today every `objects.*` accessor decodes the whole object again (`pkg/objects/objects.go:116-123`).
- The engine keeps a decode cache keyed by `(uid, resourceVersion)`, pruned in `observe` (`engine.go:114-130`) like the grace map **(our call)**. An unchanged object is decoded once.
- An object without a `uid` or `resourceVersion` is decoded every time, so a provider that lacks them still works (D-016 R5).
- The views are read-only and shared between Models.

### 3.4 Computed views: the seam and its inputs

Per-listener attachment and delegation outcomes are published nowhere by kgateway: not in status, not in metrics, not on the admin server except inside the xDS snapshot (§3.7). They are product requirements **(user, 2026-10-22)**, so the UI computes them under D-020. `computed-rules.md` defines the algorithms, the cross-check and persistence.

```go
// Rules computes the views of D-020 for one controller version.
type Rules interface {
    // Version names the pinned rule set, e.g. "kgateway-2.4".
    Version() string
    Attachment(in AttachmentInput) AttachmentResult // view A (sub-phase 2.3)
    Delegation(in DelegationInput) DelegationResult // views B and C (sub-phase 2.4)
}
```

- In a new package `pkg/computed`, following the `proxymode.Resolver` pattern (`pkg/engine/engine.go:44-45`). `engine.Config` takes `RulesFor(controllerVersion) Rules`; downstreams register theirs through `extension.WithComputedRules` **(our call)**.
- Upstream ships `kgateway-2.3` and `kgateway-2.4`. Version selection and their differences: `computed-rules.md` §2.
- Inputs are neutral: decoded views, a namespace → labels map, a settings map (§10).
- Every result carries its dependencies (for §7.4) and its check state and coverage (`computed-rules.md` §8.4).

**Settings read for the computed views.** From the controller Pod spec, as `KGW_DISCOVERY_NAMESPACE_SELECTORS` is today, with D-015's fallback: a Ready Pod, any Pod, then the Deployment's Pod template (`pkg/install/scope.go:95-140`).

| Env | Default | Used by |
|---|---|---|
| `DELEGATION_WILDCARD_NAMESPACE` (no `KGW_` prefix) | `all`; empty also means `all` (`api/labels/delegation.go:12`) | view B label refs |
| `KGW_ENABLE_EXPERIMENTAL_GATEWAY_API_FEATURES` | `true` (`v2.4.7 api/settings/settings.go:324`; `v2.3.11` `:306`) | view A: whether experimental-only kinds are candidates |

- The setting is read explicitly, never assumed **(user, 2026-10-23; §15 Q16)**. `valueFrom`, or an `envFrom` without an explicit entry, gives "unknown", and the views that depend on it say so.
- kgateway reads the wildcard once at start (`api/labels/delegation.go:12`). The Pod spec is what runs; the template fallback describes the next rollout (D-015).
- D-017 §4 is unchanged: the scope verifier compares stable kinds only. The experimental flag serves the computed views alone.
- Only these entries are read. The values live in `install.Resolved.Settings`; a non-Kubernetes provider supplies them its own way.

### 3.5 What is indexed

| Index | Key | Value | Serves | From |
|---|---|---|---|---|
| Route rows | — | `[]RouteRow`, sorted by (namespace, name, group, kind). Includes every route the install lists: owned routes, attached or not; unwatched routes that target the install (KGWUI-004, `attachment=unwatched`); declared candidates (`attachment=declared`); and no-parent rows | Routes list | 2.2 |
| By parent | Gateway or ListenerSet ref | row indexes, parentRef and status attachment | Gateway and ListenerSet pages | 2.2 |
| By parent route | HTTPRoute ref | children with a reported edge (§4.2) | Child routes | 2.2 |
| Declared candidates | HTTPRoute ref | rows that name it with no reported edge | KGWUI-011 | 2.2 |
| Parent routes | route ref | parents of its reported edges | up-chain (§5.3) | 2.2 |
| By namespace | namespace | row indexes | namespace filter | 2.2 |
| ByBackend | backend identity (group, kind, namespace, name) after defaults, for Services and Backends alike, from rule backendRefs and `RequestMirror` filters | row indexes, with "mirror" marked | `backend` filter, Backend links | 2.2 |
| Condition state | — | a bit set per row (§6.1) | state filter | 2.2 |
| Findings by object | object ref | worst error and warning finding | row state, health filter | 2.2 |
| Gateway, ListenerSet, no-parent rows | — | rows; ListenerSets also by parent Gateway | lists | 2.2 |
| By listener | (resource, listener) | computed members with effective hostnames, count, check | listener filter and table | 2.3 |
| Delegation | (parent route, rule, ref) | view B rows with contexts | Delegation tab | 2.4 |

- The findings map replaces today's per-row scan (`pkg/overview/overview.go:166-187`).
- Up-chains follow reported edges and are walked per detail request with a bound, not stored (§5.3) **(our call)**. Delegation contexts and effective matchers are computed in the recompute and retained as `computed-rules.md` §5.7 says; detail responses only project them.
- Name and hostname search is a scan over pre-lowercased strings **(our call)**.

### 3.6 Pagination: keyset cursors

- A cursor is opaque base64url: the sort key, the direction and the last row's unique tuple: (sort value, namespace, name, group, kind) for object lists, and (rule, ref, namespace, name, group, kind) for delegation rows, whose only order is by rule, ref, then child. One child can have several rows, so the rule and ref indexes are part of the key. `next` starts strictly after it in the current Model; `prev` ends strictly before the first row. Both are binary searches.
- Every sort ends with the tie-break (namespace, name, group, kind), so the same Model gives the same order.
- A row whose sort value changes moves; a pager may see it twice or miss it, as with offsets. A vanished anchor still works: the search finds where its tuple would sort.
- Responses carry `total` (after filters and the Authorizer). There is no "jump to page N" **(user, 2026-10-23; §15 Q9)**. Kubernetes `limit`/`continue` expires on compaction, so it is not an option.

### 3.7 Why the admin snapshot is not used

`/snapshots/xds` holds the effective Envoy routes (`admin/xds_snapshot.go`). Phase 2 does not read it **(our call)**:
- it binds to `localhost:9095` (hard-coded on 2.3, `v2.3.11 admin/server.go:87`; `KGW_ADMIN_BIND_ADDRESS`, default `localhost`, on 2.4, `v2.4.7 api/settings/settings.go:200`), with no containerPort or Service, so the only path is `pods/portforward`, a write-class verb (D-001);
- the same server serves `/logging`, which can change the log level, and `/debug/pprof/`;
- mapping an Envoy route to its HTTPRoute needs `KGW_ENABLE_ROUTE_SOURCE_METADATA`, 2.4 only, default off (`v2.4.7 api/settings/settings.go:332`);
- `/snapshots/krt` is an internal dump, not a contract (D-009).

It stays a candidate optional cross-check for Phase 9 (§14).

### 3.8 Per-request cost

A list request picks the narrowest index (listener, parent, namespace, else all rows), tests each candidate against the remaining filters and the state bits, authorizes, sorts if needed and slices one page. §9.6 sets the budgets.

## 4. Data model

### 4.1 New typed views in `pkg/objects`

All views decode from unstructured objects, apply Gateway API defaults and never fail: a malformed field gives an empty value (`objects.go:116-123`). No `sigs.k8s.io/gateway-api` dependency is added.

| View | Fields |
|---|---|
| `Listener` | `Name`, `Port` (0 = unset), `Protocol`, `Hostname`, `TLS{Mode, CertificateRefs []Ref}`, `AllowedRoutes{Namespaces{From, Selector}, Kinds []GroupKind}` |
| `ListenerStatus` (extended) | adds `SupportedKinds` to today's name, `attachedRoutes` and conditions (`objects.go:172-187`) |
| `GatewayView` | `Class`, `Listeners`, `SpecAddresses`, `StatusAddresses`, `AttachedListenerSets *int32`, `Conditions`, `ListenerStatus` |
| `ListenerSetView` | `Parent` (today's `ListenerSetParent`, `objects.go:219-225`), `Listeners`, `Conditions`, `ListenerStatus` |
| `HTTPRouteView` | `Hostnames`, `ParentRefs`, `Rules`, `Parents []RouteParentStatus`, `Labels`, `InheritParentMatcher` |
| `Rule` | `Index`, `Name`, `Matches` (nil when omitted, distinct from an explicit empty list), `Filters`, `BackendRefs` |
| `Match` | `Path{Type, Value}`, `Headers`, `QueryParams` (type defaults to `Exact`), `Method` |
| `Filter` | `Type`, `ExtensionRef *Ref`, `Config map[string]any` as written |
| `BackendRef` | `Group`, `Kind` (default `Service`), `Namespace` (default the route's), `Name`, `Port`, `Weight` (default 1), `Filters`, `Delegation *DelegationRef` |
| `DelegationRef` | `Mode` (`name`, `wildcard`, `label`), `Namespace` (default the parent's), `Name` or `Label`. Classification as kgateway does it, identical at both tags: `computed-rules.md` §3.2 |
| `BackendView` | `Type`, `Conditions` |
| `RouteHostnames` (GRPCRoute, TLSRoute) | `Hostnames`, for view A |

`TLS.Options` is not decoded; it stays in the YAML tab **(our call)**. `CertificateRefs` are refs only, never resolved (§11).

### 4.2 Assignment of delegated child routes

Assignment stays **status-based**. Computed views explain and cross-check; they never decide ownership, counts or install health (D-020). This ships in 2.2 **(user, 2026-10-23; §15 Q12)**.

Phase 1 assigns a route only through a parentRef that resolves to a Gateway or ListenerSet of the install (`pkg/install/assign.go:153-189`). A route reached only through HTTPRoute parents is not owned, counted or health-checked; a mixed route is owned through its Gateway, but its HTTPRoute-parent status is ignored (`assign.go:158-170`).

**Reported edge.** Route C has a reported edge to HTTPRoute P in install I when:
1. C's namespace is in I's scope, and P is owned by I;
2. C has a `status.parents` entry whose `controllerName` ∈ I.controllerNames and whose `parentRef` resolves to P after defaults;
3. **eligibility:** a current `spec.parentRefs` entry of C equals that status parentRef after defaults (Phase 1's equality rule), or C's `spec.parentRefs` list is **empty**.

The empty-list fallback matches kgateway, which allows implicit attachment only for an empty list (`v2.4.7 utils/delegation/delegation.go:25-31`; `v2.3.11` same file `:25-45`) and falls back to report refs only then (`v2.4.7 pkg/reports/status.go:489-496`). A child that moved from P to Q keeps no edge to P, as Phase 1 ignores removed refs (`pkg/health/checks.go:314`).

**Freshness does not decide the edge.** An eligible entry keeps the edge whether current or stale. kgateway stamps route status with the report's generation (`v2.4.7 pkg/reports/status.go:475-480`), so right after a child edit its entry describes the previous generation. Ownership and counts stay stable; freshness sets the child's state with Phase 1's rules: KGWUI-003 info inside the grace period (check pending, row `unknown`), warning after it.

**Declared candidate.** A current `spec.parentRefs` entry of C resolves to an HTTPRoute P owned by I, C's namespace is in I's scope, and C has **no eligible status entry** for P, current or stale. C is a diagnostic candidate only: never a child, never in Child routes or the up-chain, never makes a `name` ref "reported", not counted unless owned another way. It gets KGWUI-011 (§6.2).

**Out-of-scope routes are never candidates.** A route outside I's scope whose parentRef names a route of I gets KGWUI-004 only (`assign.go:181-186`). It never gets KGWUI-011, never enters a computed view, and never makes the resource-status check pending.

**Ownership rules.**
- A breadth-first pass starts at Gateway-attached HTTPRoutes and follows **reported edges only**, over a reverse index built once per recompute. A visited set stops cycles. Cost is linear in routes plus edges.
- A child is owned once per install, with every reported parent in `OwnedObject.Parents`. A mixed route keeps its Gateway parents and gains its reported HTTPRoute parents.
- Owned children count in the HTTPRoute tile and in install health. This changes Phase 1's numbers on clusters that use delegation (§15 Q12).
- A parentRef to an HTTPRoute that does not exist gets KGWUI-003. HTTPRoute joins the parent kinds (`assign.go:146-151`).
- Two installs that share a controller name each count only edges whose parent they own.

**Computed-only children** (2.4). A route view B computes as attached with no reported edge is not owned. It is a diagnostic candidate, and the missing entry is checked under `computed-rules.md` §8.3 (missing expected status).

**HTTPRoutes with no parent.** An in-scope HTTPRoute with empty `spec.parentRefs` and no status from I's controller **(user, 2026-10-23; §15 Q10)**: listed under `state=no-parent` in every install whose scope holds it; not counted; no status finding; shown by view B when selected; its row opens the YAML view (detail only when an owned parent selects it); the Authorizer applies.

**Stale status after a drop.** On 2.4 a matcher-dropped child's status is cleared (`v2.4.7 pkg/krtcollections/policy.go:1150-1178`). The other cases are observed on a cluster first (`computed-rules.md` §10).

### 4.3 The `Backend` kind

- **Descriptor:** `gateway.kgateway.dev/Backend`, resource `backends`, version `v1alpha1`, namespaced, category `backend`, added to `kinds.Upstream()` (`pkg/kinds/upstream.go:17-34`). RBAC already covers it.
- **Assignment:** a Backend belongs to install I when its namespace is in I's scope and a backendRef of a route owned by I names it by full identity. An unreferenced Backend belongs to no install; the tile counts only referenced ones ("in use") **(our call)**.
- **Scope verifier:** no `resources_managed` label, so KGWUI-007 ignores it (`pkg/scopeverify/verify.go:141-151`).
- **Resource endpoint:** owned Backends join `lookup` (`pkg/api/handlers.go:255-265`); a referenced Backend outside the scope is a diagnostic candidate (§7.1).
- Health: §6.2. This prepares Phase 4, which must work with `Backend` alone (D-016).

### 4.4 ListenerSet kinds stay generic

- ListenerSet v1 and XListenerSet are registered with category `listenerset` (`pkg/kinds/upstream.go:21-22`). A downstream adds its kind through `extension.WithKinds` with that category and must follow the Gateway API ListenerSet shape (`spec.parentRef`, `spec.listeners[]`, `status.listeners[]`). The kinds doc states this **(our call)**.
- kgateway reconciles both kinds (`v2.4.7 proxy_syncer/status_syncer.go:664,702`; `v2.3.11` `:612,650`).
- URLs carry group and kind, so two kinds with one name never collide.

## 5. Delegation on screen

### 5.1 Three sources, side by side

| Source | Says | Shown as | From |
|---|---|---|---|
| Child's `spec.parentRefs` | which parents **may** delegate to it | "Allows: `shop/api`" or "Allows any parent" | 2.2 |
| Controller status | which parents **did** (the reported edge, §4.2) | the child's status entry for this parent | 2.2 |
| Views B and C | which routes each ref selects, the outcome per path, the effective matches | "Computed by the UI", with rule, coverage and check | 2.4 |

### 5.2 Cross-check on screen

The contract is `computed-rules.md` §8 and §9; this section only says how it is shown.
- A computed fact shows its coverage: "Count agrees with the controller", "Status agrees" or "Not checked by the controller" (`computed-rules.md` §8.4). Effective matchers and effective hostnames always read "Not checked by the controller".
- `pending` reads "Waiting for the controller". `unknown` names the missing input. `restricted` reads "Hidden: depends on objects you cannot view" (§7.4).
- `disagrees` shows both values and "The UI's computation disagrees with the controller. The controller's status is what counts." A mature disagreement is KGWUI-012 (§6.2).
- A delegation row whose path the aggregated status cannot confirm says "Status cannot confirm this path".

### 5.3 Tree, levels and the up-chain

| Parent page shows | Source | From |
|---|---|---|
| Each delegation ref as written | parent spec | 2.2 |
| Child routes: every child with a reported edge, paged | `GET …/routes?parent=…` | 2.2 |
| Delegation: per ref, the computed rows with outcome, contexts and the controller's signal, paged | `GET …/routes/{g}/{k}/{ns}/{name}/delegation` | 2.4 |

- **N levels.** Each level is one paged list, fetched when expanded, so a 100-child wildcard does not load the subtree.
- **Up the chain.** The route header shows paths to a Gateway or ListenerSet: `edge (Gateway) › api (HTTPRoute) › orders (HTTPRoute)`. The walk runs per detail request over reported edges only, depth-first with parents sorted by (namespace, name). It stops after 5 complete paths, at depth 32, or after 256 route nodes, whichever comes first. A path that meets a route already on it ends with a `cycle` marker. A bounded walk sets `truncated: true` and never claims a remaining count **(our call)**.
- **Diamonds.** kgateway tracks visited routes per path, so A→B1→C and A→B2→C are valid and C gets one entry per parent (`v2.4.7 query/route.go:240-246`; `v2.3.11` `:213-219`).

### 5.4 What is not shown

| Not shown | Why |
|---|---|
| Which rule wins for a request; shadowed matches | the controller's job; upstream ask (§14) **(user, 2026-10-22)** |
| Order of flattened children | precedence logic; 2.3's sort is not a strict weak order (`v2.3.11 translator/routeutils/sortable_route.go:27-35`) |
| What Envoy finally serves | needs the xDS snapshot (§3.7); 2.4's listener isolation gets a note only (`computed-rules.md` §4.5) |
| Inherited policies | Phase 5 |

## 6. Health additions

Phase 1's checks and roll-up (design §5) are unchanged except where this section says so.

### 6.1 Condition state flags on route rows

Each row carries flags from its **current** status entries for this install's parents (Phase 1 freshness rules), plus computed flags. They drive the Routes filter and create no findings.

| Flag | Rule | From |
|---|---|---|
| `accepted` / `not-accepted` | some parent: `Accepted=True` / `Accepted=False` (reason kept) | 2.2 |
| `refs-unresolved` | some parent: `ResolvedRefs=False` | 2.2 |
| `not-programmed` | some parent: `kgateway.dev/Programmed=False`, or `Accepted=False` with `RouteRuleReplaced` or `RouteRuleDropped` | 2.2 |
| `pending` | some parent: no status, `Pending`, or stale | 2.2 |
| `declared` | a parentRef to an owned HTTPRoute with no reported edge | 2.2 |
| `no-parent` | §4.2 | 2.2 |
| `disagrees` | the row's object has KGWUI-012 (a finding, so not dependency-filtered) | 2.3 |
| `listener-unknown` (computed) | view A could not place an accepted parentRef | 2.3 |
| `dropped` (computed) | view B: selected by an owned parent and dropped, forbidden or rejected | 2.4 |

- `kgateway.dev/Programmed` exists on 2.4 (`v2.4.7 api/conditions/conditions.go:7`), not on 2.3. On 2.3 the replaced and dropped reasons arrive through `Accepted=False` (`v2.3.11 translator/irtranslator/route.go:294-312`; `v2.4.7` `:328-350`).
- Reasons are raw strings, never a fixed enum (gateway-api#4760).
- Computed flags follow their dependencies (§7.4). A caller who may not view one sees no such flag, and a filter on it does not match that row for that caller.

### 6.2 Findings

| Code | Slug | Severity | Change | From |
|---|---|---|---|---|
| KGWUI-002 | condition-false | error (unchanged) | also covers reported-edge entries and Backend `Accepted` | 2.2 |
| KGWUI-003 | route-unattached | info / warning (unchanged) | also a parentRef to a missing HTTPRoute | 2.2 |
| KGWUI-004 | namespace-not-watched | warning (unchanged) | also an unwatched route naming an owned route. The detail names the parent's kind when it is not a Gateway ("targets HTTPRoute shop/api", "targets ListenerSet …"); the Gateway text stays "targets Gateway …" (`pkg/health/checks.go:619`), so Phase 1 goldens do not change **(our call)** | 2.2 |
| **KGWUI-011** | **delegated-route-not-attached** | info inside the grace period, warning after | **new**; two triggers | 2.2, 2.4 |
| **KGWUI-012** | **computed-view-disagrees** | warning | **new** **(user, 2026-10-23; §15 Q13)** | 2.3, 2.4 |

**KGWUI-011.** One finding per (route, parent HTTPRoute), except the orphan trigger, which gives one per route. Message draft: "Parent route does not use this route".
- **Status trigger** (2.2): a declared candidate (§4.2). The grace period runs from the later of the route's and the parent's current generation first-seen time **(our call)**. **Inside it the finding is info and the `resource status` required check is pending, so the install is unknown, as for any resource being processed** (Phase 1 design §5.2; restored from the earlier design). After it, the finding is a warning and the check is OK again. This holds for every ref mode: an explicit declared parent always gets the diagnostic, even when only a wildcard ref selects the route (§15 Q15). Detail draft: "No status from HTTPRoute `shop/api`", plus the computed reason when 2.4 has one.
- **Computed trigger** (2.4): view B computes an in-scope route, with no eligible entry, as selected by an owned parent's `name` or `label` ref with outcome `dropped` or `forbidden` in every context, and the traversal for that (route, parent) is **complete** (`computed-rules.md` §8.5). An incomplete traversal gives no computed finding. It is a warning once mature (`computed-rules.md` §9.3, 30 s). **It never makes a check pending**: a computed outcome waits on no controller output, and health stays status-based (D-020) **(our call)**. 
- **Orphan trigger** (2.4) **(user, 2026-10-23; §15 Q15)**: a route gets the computed finding only when it is an **orphan in this install**. All of these must hold: its `spec.parentRefs` list is empty, so it can work only as a delegated child; it is selected **only by `wildcard` refs** of this install's owned parents (any `name` or `label` ref that selects it gives the per-parent trigger above instead, and no orphan finding); **every** selecting outcome, in every context, is `dropped` or `forbidden` (a `cycle` or `unresolved` outcome does not qualify); and the traversal for every selecting parent is complete (`computed-rules.md` §8.5). The conclusion is install-local: a parent of another install may still select and serve the route, because an empty list permits any parent (`utils/delegation/delegation.go:25-27`, both tags). One finding per route, not per parent; a warning once mature, never pending. A route with its own parentRefs that a wildcard selects and drops gets no finding and stays in the Delegation tab with its reason. Detail draft (wording-pass candidate): "No parent route in this install keeps this route, and it has no parentRefs of its own."
- **Two clocks.** KGWUI-011's status-trigger grace runs 60 s from the later of the route's and the parent's current-generation first-seen time. KGWUI-012's `graceEnd` for a missing child entry (`computed-rules.md` §9.3) runs 60 s from the latest first-seen time of the child, the parent and every route on a contributing path, and is combined with the 30 s persistence window. They are different clocks and can end at different times **(our call)**.
- `rejected-hostnames` already has KGWUI-002 from status. An eligible but stale entry is not KGWUI-011; it follows Phase 1's freshness rules. An out-of-scope route gets KGWUI-004 only.

**KGWUI-012.** Raised when a check matures under `computed-rules.md` §8.3 and §9.3. The resource is the object whose status disagrees. Message draft: "The UI's computed view disagrees with the controller". Detail draft: "Listener `https`: computed 12 routes, controller reports 11. Rule set kgateway-2.4." A warning: it can turn the install yellow, never red. Unknown, skipped, stale, not-verified and incomplete checks never raise it. A conclusion that needs absence (no entry expected, no delegation failure) needs a complete traversal (`computed-rules.md` §8.5).

**Backend `Accepted`** (2.2). kgateway 2.4 stamps `observedGeneration` (`v2.4.7 pkg/reports/status.go:410-415`); 2.3 does not (`v2.3.11 extensions2/pluginutils/status.go:10-40`). The rule reads the condition's own evidence, not the version **(user, 2026-10-23; §15 Q11)**:

| Condition | Evidence | Finding | State |
|---|---|---|---|
| `observedGeneration` ≥ `generation` | current | `False` → KGWUI-002 error; `True` → none | from the finding; `True` → healthy |
| `observedGeneration` > 0 and lower | stale | none | unknown |
| `observedGeneration` 0 (2.3) | none | `False` → KGWUI-002 **warning**, "The controller does not say which version of this Backend it checked" | unknown |
| absent | none | none (optional condition) | unknown |

No-evidence states are never green (D-016 §6) and add no failed required check, so a 2.3 install with Backends does not stay unknown forever (D-018). The pending helper (`pkg/health/checks.go:228-255`) is not used for Backends.

**Experimental hint.** XListenerSet pages reuse D-018's hint when the object has no status.

### 6.3 Row state

Every row and detail header shows a **state** (`error`, `unknown`, `warning`, `healthy`), never a severity, with Phase 1's Gateway row rules applied to the row's own object (`pkg/overview/overview.go:166-187`) **(our call)**: any error finding → `error`; else any required check not OK → `unknown`; else any warning finding → `warning`; else `healthy`. A Backend without current evidence is `unknown`. Info findings never set a row's state by their own severity. A KGWUI-011 info inside its grace period, though, makes the install-wide `resource status` required check pending (§6.2), and Phase 1's rule is install-wide: while any required check is not OK, **every** row of the install is `unknown`. Stale data turns every state `unknown` on the client.

**Propagation.** A child's failures count in install health and on the child's row. A Gateway, ListenerSet or parent route badge counts only findings on that object; a KGWUI-012 on a Gateway makes it `warning` **(our call)**. Clusters that use delegation may change colour after upgrade; the release notes say so.

## 7. API v1 additions

All additions are new paths or new optional fields. No existing path, body or install ID changes (D-016 H11).

### 7.1 Endpoints and admission

| Endpoint | Returns | From |
|---|---|---|
| `GET /api/v1/installs/{id}/routes` | `RouteList` | 2.2 |
| `GET /api/v1/installs/{id}/gateways` | `GatewayList` | 2.2 |
| `GET /api/v1/installs/{id}/listenersets` | `ListenerSetList` | 2.2 |
| `GET /api/v1/installs/{id}/gateways/{ns}/{name}` | `GatewayDetail` | 2.2 |
| `GET /api/v1/installs/{id}/listenersets/{group}/{kind}/{ns}/{name}` | `ListenerSetDetail` | 2.2 |
| `GET /api/v1/installs/{id}/routes/{group}/{kind}/{ns}/{name}` | `RouteDetail`; HTTPRoute only, other kinds 404 "no detail view for this kind" | 2.2 |
| `GET /api/v1/installs/{id}/routes/{group}/{kind}/{ns}/{name}/delegation` | `Page<DelegationRow>`, filterable by `rule`, `ref`, `outcome` | 2.4 |

- Gateway has one kind, so its path is shorter **(our call)**. The ListenerSets list is added because large installs carry hundreds **(our call)**.
- `/api/v1/meta` gains an optional `views` field: the group/kinds with a detail page. It is informational; the SPA never derives links from it. Each detail payload carries `ruleSet`.

**Admission.** Finding links and spec links must not end in a 404 **(our call)**:

| Object | Detail | YAML |
|---|---|---|
| Owned by the install | yes | yes (today) |
| Referenced by a current finding of the install | candidate | yes (today, `pkg/api/handlers.go:266-285`) |
| Named by a `parentRef` or `backendRef` of an owned object, by full identity, of a served kind | candidate | yes (**new**) |
| Selected by an owned parent's delegation ref in view B (2.4) | candidate | yes (**new**) |
| A `no-parent` row of the install | only if a row above applies | yes (**new**) |
| Anything else | 404 | 404 |

- A candidate's page says "Not part of this install. Shown because …" with the reason, and lists this controller's status entries first.
- A ListenerSet whose parent Gateway is missing is never assigned (`pkg/install/assign.go:136-139`); a route naming it gets KGWUI-003, which makes the route servable.
- The resource endpoint serves a superset of today; every denial is still a 404 (D-013).

**Link metadata.** One function, `admit(install, caller, ref) → detail | yaml | none`, implements admission and the Authorizer. Both endpoints call it, and every payload labels each reference with it **(our call)**:
- every rendered reference is a `LinkedRef` (rows, parents, `via`, parent routes, ancestor steps, backendRefs, delegation targets, ExtensionRef filters, the ListenerSet's parent, a candidate's `by`);
- `detail`: admitted with a detail view; `yaml`: admitted without one, or a `no-parent` row; `none`: not admitted, denied, absent or never served (Secret certificateRefs, Services). Denied and absent both give `none` (D-013);
- admission does not follow chains: a route D named only by candidate C is `none` on C's page;
- Phase 1 findings gain an optional `link` on their resource, and overview Gateway rows an optional `link` (§7.6). The SPA renders `link` as given and never derives one from a kind.

### 7.2 Query parameters

| Param | Lists | Values |
|---|---|---|
| `namespace` | all | exact; repeatable (OR) |
| `kind` | routes | `group/Kind`; repeatable |
| `parent` | routes, listenersets | `group/Kind/namespace/name`; routes: a Gateway (including routes through its ListenerSets), ListenerSet or HTTPRoute; listenersets: a Gateway |
| `listener` (2.3) | routes | needs a Gateway or ListenerSet `parent`; view A's computed members, or `unknown` |
| `attachment` | routes | with `parent`: `accepted`, `not-accepted`, `pending`, `unwatched`, and for an HTTPRoute parent `declared` |
| `state` | routes | a §6.1 flag; repeatable (OR) |
| `health` | all | `error`, `unknown`, `warning`, `healthy`; repeatable |
| `q` | all | case-insensitive substring of namespace/name; routes also hostnames |
| `backend` (2.2) | routes | `group/Kind/namespace/name` of a backend, `core` for the empty group (`core/Service/shop/cart`); repeatable (OR). Matches routes whose rules' `backendRefs`, or whose `RequestMirror` filters, name it by full identity after defaults. Delegation refs never match. **(user, 2026-10-23)** |
| `sort` | all | `name` (default), `kind`, `health`, Gateways also `routes`; suffix `.asc` or `.desc` |
| `limit`, `cursor` | all | 1–200, default 50; an opaque `next` or `prev` value |
| `rule`, `ref`, `outcome` (2.4) | delegation | rule index, ref index, a row outcome; no `sort` (fixed order, §3.6) |

**Errors** use Phase 1's `ErrorResponse` (`pkg/api/types.go:64-67`). 400: unknown value, `limit` out of range, `listener` without a suitable `parent`, a malformed cursor or one from another sort (the SPA drops it and reloads page one). 404: unknown install, or an absent, unadmitted or denied object. 503: cache not synced (`handlers.go:69-77`).

### 7.3 DTOs

Field names are wire names. `?` marks optional fields. Times are UTC (D-017).

```jsonc
// Shared
Link            "detail" | "yaml" | "none"                              // from admit(), §7.1
LinkedRef       { group, kind, namespace?, name, link: Link }
Computed        { by: "ui", rules: string, rule: string,                // rules: "kgateway-2.4"
                  state: "checked"|"disagrees"|"pending"|"unknown"|"restricted",
                  coverage?: ("count-agrees"|"status-compatible"|"not-verified")[],
                  reason?, note? }                                      // computed-rules.md §8.4; restricted: no reason, no note
Condition       { type, status, reason, message, observedGeneration, lastTransitionTime, stale }
ParentStatus    { parentRef: ParentRef, target: LinkedRef, controllerName, ours, conditions: Condition[] }
ListenerAttachment { resource: LinkedRef, listener: string,             // one per (parentRef, listener), view A
                  effectiveHostnames?: string[], computed: Computed }   // hostnames absent unless state is checked or disagrees
Attachment      { outcome?: "accepted"|"NotAllowedByListeners"|"NoMatchingParent"|"NoMatchingListenerHostname",
                  listeners: ListenerAttachment[], explanation?, computed: Computed }   // outcome absent when unknown or restricted
ParentSummary   { ref: LinkedRef, sectionName?, port?, via?: LinkedRef, viaRestricted?: true,
                  attachment: "accepted"|"not-accepted"|"pending"|"unwatched"|"declared",  // from status
                  conditionType?, reason?, message?,                    // the deciding condition
                  computed?: Attachment }                               // 2.3
Page<T>         { install, items: T[], total, next?, prev?, deniedInInstall?, collectedAt }

// Lists
RouteRow        { ref: LinkedRef, hostnames: string[], parents: ParentSummary[], parentRoutes: LinkedRef[],
                  children: number, flags: string[], health: State }
GatewayRow2     { ref: LinkedRef, class, health, listeners, attachedRoutes /*status sum*/, routes?, notAccepted,
                  listenerSets, disagreements?, addresses: string[] }   // routes, disagreements: 2.3
ListenerSetRow  { ref: LinkedRef, parent: LinkedRef, health, listeners, acceptedReason?, routes?, disagreements? }
DelegationRow   { rule, ref: number, refAsWritten: { mode, namespace, name?, label? }, child: LinkedRef,
                  allows: "any"|"this"|"other",
                  outcome: "attached"|"partial"|"dropped"|"forbidden"|"rejected-hostnames"|"cycle"|"unresolved"|"unknown",
                  reason?, contexts: DelegationContext[], contextsTruncated?: true, computed: Computed,
                  observed: { entry?: ParentStatus, parentCondition?: Condition } }
DelegationContext { parentMatch: MatchDTO, path: AncestorStep[], outcome, reason? } | { restricted: true }

// Details
ListenerDTO     { name, port?, protocol, hostname?, tls?: { mode, certificateRefs: LinkedRef[] /*link none*/ },
                  allowedRoutes: { namespaces: { from, selector? }, kinds: GroupKind[] },
                  status?: { attachedRoutes, supportedKinds, conditions: Condition[] },
                  computedRoutes?: number, computed?: Computed, isolationNote?: true }   // 2.3
GatewayDetail   { install, ref, generation, class, health, candidate?, ruleSet?, conditions, addresses,
                  listeners: ListenerDTO[], attachedListenerSets?,
                  routeCounts: { accepted, notAccepted, pending, unwatched }, collectedAt }
ListenerSetDetail { install, ref, generation, health, candidate?, ruleSet?, parent: LinkedRef, parentFound,
                  conditions, listeners: ListenerDTO[], experimentalHint?, routeCounts,
                  parentAllowedListeners?: { set: boolean, value?: object, crdHint: boolean },   // 2.2; value as written
                  collectedAt }
RouteDetail     { install, ref, generation, health, candidate?, ruleSet?, hostnames,
                  annotations: { inheritParentMatcher? }, parentRefs: ParentRef[], parents: ParentStatus[],
                  attachments?: { parentRef: ParentRef, index, target: LinkedRef, result: Attachment }[],  // 2.3
                  rules: RuleDTO[], effective?: EffectiveDTO[], effectiveTruncated?: boolean,             // 2.4
                  ancestors: AncestorPath[], ancestorsTruncated: boolean,
                  childCount, declaredCount, collectedAt }
EffectiveDTO    { parent: LinkedRef, mode: "default"|"inherit", contextsTruncated?: true,   // per parent edge, computed-rules.md §5.7
                  contexts: ({ parentMatch: MatchDTO, rules: { index, kept, matches: MatchDTO[], reason? }[] }
                             | { restricted: true })[],
                  computed: Computed }                                  // coverage always not-verified
AncestorPath    { steps: AncestorStep[], cycle: boolean }
AncestorStep    { ref: LinkedRef } | { restricted: true }               // no identity when restricted
RuleDTO         { index, name?, matches: MatchDTO[], filters: FilterDTO[], backendRefs: BackendRefDTO[] }
FilterDTO       { type, extensionRef?: LinkedRef, config? }
BackendRefDTO   { ref: LinkedRef, port?, weight, filters: FilterDTO[],
                  delegation?: { mode, namespace, name?, label?,
                                 state?: "reported"|"not-reported"|"not-found"|"restricted",
                                 selected?: number, attached?: number, computed?: Computed },
                  backend?: { state: State, accepted?: Condition } | { restricted: true } }
// candidate: { reason: "finding"|"referenced"|"selected", by?: LinkedRef }
// Finding gains optional link on its resource and optional evidenceRestricted (§7.4)
// InstallSummary gains optional confirmedAt (§3.1)
```

- `attachments` has one entry per parentRef to a Gateway or ListenerSet. Each listener of each parentRef has its own `effectiveHostnames`, so the parent cards render without a second request.
- `ParentSummary.computed` carries the same `Attachment` for list rows. Failed-route tables render reason, message and explanation without a detail request.
- `GatewayRow2` is new. Phase 1's overview `GatewayRow` (`pkg/overview/overview.go:73-80`) gains only an optional `link`. The final Go name is the plan's choice.
- `delegation.state` is status-based and set only for `name` refs: `reported` needs a reported edge. `selected` and `attached` are computed counts (2.4).
- `effective` and `DelegationRow.contexts` project what the model retained, with the limits and truncation flags of `computed-rules.md` §5.7. A detail request never re-runs the traversal. An `EffectiveDTO` covers one parent edge, so it merges the contexts of every delegation row with that parent and child; its `contextsTruncated` is set when any of them was cut, and an edge with no kept contexts is sent as `contexts: []` with the flag, never as complete.
- The Gateway page pages its ListenerSets through `GET …/listenersets?parent=…`, so no detail body is unbounded.
- `ParentSummary` picks the deciding condition: the first current `False` among `Accepted`, `ResolvedRefs`, `kgateway.dev/Programmed`, else the pending reason.

### 7.4 Authorizer

This is the only definition of what a caller may see. Every new handler goes through the `Authorizer` (`pkg/api/authz.go:12-15`). All rules are **(our call)**.

1. **Objects.** A denied detail object returns 404, as an absent one does. List rows are filtered per ref before paging; `total` counts only visible rows.
2. **No filtered hidden counts.** Lists report at most `deniedInInstall`: the unfiltered number of objects of the listed kinds in the install that the caller may not view. It is independent of every filter, so a hostname probe changes nothing, and it is shown once per list page.
2a. **The `backend` filter cannot probe.** It matches backendRefs as written in the specs of routes the caller may view, which are already part of those objects (rule 8). It never looks up the backend: the response is the same whether the backend exists, is absent or is denied, and carries nothing about the backend's state. `total` counts visible routes only, and `deniedInInstall` stays unfiltered (rule 2) **(our call)**.
3. **Dereferenced objects.** Data that comes from another object is sent only if the caller may view that object: a Backend's state and condition (`{restricted: true}`), a `name` ref's state (`restricted`), a delegation row's child (the row is dropped), `via` (`viaRestricted`), an ancestor step (`{restricted: true}`, no identity).
4. **Computed facts are dependency-based.** Every computed result carries its dependency set: the objects whose data decided it (`computed-rules.md` §4.6, §5.6). For a listener membership that includes the route's Namespace when the listener uses `Selector`. For a delegation context or an effective matcher it includes every route on the path, because the parent's match and inherited headers come from them, plus the facts that made the root a root: the Gateways, ListenerSets and Namespaces behind its view A outcome, or for a status root the entry's target (`computed-rules.md` §5.1). A fact is sent only if the caller may view **every** dependency. Otherwise its `Computed.state` is `restricted`, and its value, reason and note are omitted: no hostnames, no outcome, no match, no partial value. Quoted strings are never redacted piecemeal.
5. **Derived counts** (`routes`, `notAccepted`, `listenerSets`, `routeCounts`, `children`, `childCount`, `declaredCount`, `computedRoutes`, `selected`, `attached`) count only visible objects and are recounted per request. A computed count whose dependencies are not all visible, for example a listener with a hidden member or a hidden Namespace behind a `Selector`, is `restricted` and its cross-check is not shown.
6. **Computed flags** (`listener-unknown`, `dropped`) follow rule 4 per caller.
7. **Finding evidence.** Phase 1 redacts a finding only by its resource (`pkg/api/handlers.go:121-124`). Phase 2 findings carry their evidence's dependency set. When the caller may view the resource but not every dependency, the finding is listed with its code, severity and resource, its detail is replaced by "Details depend on objects you cannot view", and `evidenceRestricted: true` is set (additive). This covers KGWUI-011 and KGWUI-012 details that quote a parent's match, a hidden route or a Namespace's labels.
8. **Spec refs stay.** References written in an authorized object's own spec or status are part of it and are sent as written, as the YAML view does. Status-written numbers (`attachedRoutes`, `attachedListenerSets`) are sent as written.
9. **Health is not filtered.** Install health, row states and the `disagrees` flag cover all findings (design §7.1, D-013). Only evidence is restricted (rule 7).
10. `AllowAll` skips per-row and dependency calls and uses precomputed values (`authz.go:30-37`). Dependency sets are stored as indexes into a per-install ref table, so the check is a bounded loop.

### 7.5 Example (2.3)

```http
GET /api/v1/installs/kgw.kgateway/routes?parent=gateway.networking.k8s.io/Gateway/infra/edge&attachment=not-accepted&limit=2
```
```json
{
  "install": "kgw.kgateway",
  "items": [
    {"ref": {"group": "gateway.networking.k8s.io", "kind": "HTTPRoute", "namespace": "shop", "name": "cart", "link": "detail"},
     "hostnames": ["shop.example.com"],
     "parents": [{"ref": {"group": "gateway.networking.k8s.io", "kind": "Gateway", "namespace": "infra", "name": "edge", "link": "detail"},
                  "sectionName": "https", "attachment": "not-accepted", "conditionType": "Accepted",
                  "reason": "NoMatchingListenerHostname", "message": "",
                  "computed": {"outcome": "NoMatchingListenerHostname", "listeners": [],
                               "explanation": "listener https has *.example.org; route has shop.example.com",
                               "computed": {"by": "ui", "rules": "kgateway-2.4", "rule": "hostname-intersection",
                                            "state": "checked", "coverage": ["status-compatible"]}}}],
     "parentRoutes": [], "children": 0, "flags": ["not-accepted"], "health": "error"}
  ],
  "total": 1,
  "collectedAt": "2026-10-22T09:12:03Z"
}
```

kgateway writes no message on attachment failures (`translator/gateway/gateway_translator.go:80-87`), which is why the explanation matters.

### 7.6 Goldens

New fixtures in `pkg/api/testdata/golden`, added per sub-phase:
- 2.2: `routes.json`, `routes-filtered.json`, `gateways.json`, `listenersets.json`, `gateway-detail.json`, `listenerset-detail.json` (with a `NotAllowed` ListenerSet whose parent has no `allowedListeners`, and one whose parent sets it), `route-detail.json` (delegation refs, reported children, a cycle in the up-chain, a truncated up-chain, a Backend ref), `route-detail-candidate.json`, `bad-request.json`, and a restricted variant (Backend, ancestor step, `via`, `deniedInInstall`).
- 2.3: `routes-by-listener.json`; `gateway-detail.json` gains one listener per check state; a restricted variant with a `Selector` listener over a hidden Namespace.
- 2.4: `route-delegation.json` (every outcome, `partial`, contexts), `route-detail.json` gains effective matchers in both modes, an edge with `contextsTruncated` and an edge with `contexts: []` at the install cap; a restricted variant with a hidden ancestor and `evidenceRestricted`.

The TypeScript golden test (`web/packages/shell/src/api/types.golden.test.ts`) imports each one. Existing goldens change only by optional fields, and a test asserts the diffs add nothing else:
- `installs.json`, `bare-installs.json`: `confirmedAt` (2.1);
- `meta.json`: `views` and the Backend kind (2.2);
- `health.json`: `link` on finding resources (2.2); `evidenceRestricted` appears only in restricted fixtures;
- `overview.json`, `bare-overview.json`: `link` on Gateway rows (2.2).

**Go shapes (our call).** `HealthResponse` stops being an alias of `health.Report` (`pkg/api/types.go:42`) and becomes a struct that embeds the report and shadows `Findings` with a type that adds `link`. `OverviewResponse` shadows the embedded `overview.Overview.Gateways` (`pkg/overview/overview.go:86`) with rows that add `link`. `encoding/json` serialises the shallower field, so both changes are additive on the wire; the stored report and overview stay unchanged.

**Reverse lookup goldens (2.2).** `routes-by-backend.json` (a Service, a Backend, a mirror target, two backends OR-ed) and a restricted variant: the same query for a denied backend and for an absent one return identical bodies.

## 8. Frontend

### 8.1 Pages and URLs

| URL | Page |
|---|---|
| `/i/:id/gateways`, `/i/:id/gateways/:ns/:name` | Gateways list, Gateway detail (`?listener=` selects a listener, 2.3) |
| `/i/:id/listenersets`, `/i/:id/listenersets/:group/:kind/:ns/:name` | ListenerSets list, detail |
| `/i/:id/routes`, `/i/:id/routes/:group/:kind/:ns/:name` | Routes list, HTTPRoute detail |
| `/i/:id/r/:group/:kind/:namespace/:name` | YAML view (unchanged) |

- Detail pages have **Overview** and **YAML** tabs (`?tab=yaml`); the YAML tab reuses the YAML view's body (`web/packages/shell/src/pages/ResourcePage.tsx:135-192`).
- The header gains install-scoped nav: Overview, Gateways, Routes, ListenerSets, Findings.
- Filters, sort and cursor live in the query string, as in the Gateway table (`pages/install/GatewayTable.tsx:221-260`).
- Every link comes from the reference's `link` (§7.1).

### 8.2 Extension contract

- `CorePages` (`shell/extension.ts:49-54`) gains **optional** slots: `GatewaysPage`, `GatewayPage`, `ListenerSetsPage`, `ListenerSetPage`, `RoutesPage`, `RoutePage`. A missing slot falls back to the upstream page.
- The core reserves `gateways`, `listenersets` and `routes` under `/i/:id/`; the `RouteContribution` doc lists them (`extension.ts:14-22`).
- New exports: `objectPath(installId, linkedRef)`, `ComputedBadge`, list hooks and DTO types. `resourcePath` keeps meaning the YAML view (`pages/install/FindingRow.tsx:76-79`).
- `Vocabulary` gains `getCommand(ref, resource)`; the default is `kubectl get <resource>.<group> -n <ns> <name> -o yaml`, plural and group from `/meta`. A platform without kubectl overrides or hides it (D-016 H4).
- `SHELL_API_VERSION` stays **1**: every change is additive and no release has frozen the contract (phase-1-complete.md) **(our call)**.
- The cluster cards, switcher and header read `confirmedAt ?? computedAt` (§3.1).

### 8.3 Finding links retarget

`FindingRow` (`pages/install/FindingRow.tsx:121`) and the top findings use `objectPath` on the finding's `link`; the Gateway table's name cell and row click (`pages/install/GatewayTable.tsx:192,265`) use the row's `link`. A restricted finding never links.

### 8.4 Computed facts on screen

- Every computed fact carries a `ComputedBadge`: an outlined "Computed" chip with an icon. Its tooltip names the rule and rule set: "Computed by the UI from kgateway 2.4's attachment rules: hostname intersection".
- Next to it sits the coverage or check state of §5.2, as icon + text. Only `count-agrees` and `status-compatible` use the healthy icon, and their text says what was compared. `not-verified` uses a neutral icon.
- Controller-reported facts carry no badge, so the default reading is "this is what kgateway says".
- Colour is never the only signal (roadmap UX §4).

### 8.5 Page content

A diagnostic candidate shows its banner above the header.

**Gateway detail.** Header: kind, name, health, data age, copy buttons. Then: conditions (icon + text, reason, message, freshness); listeners (name, port, protocol, hostname or "Any hostname", TLS mode, certificate names as text, allowed namespaces and kinds, `attachedRoutes`, and from 2.3 the computed count with its check; conditions on expand); ListenerSets (paged) with "Attached ListenerSets (status): N"; attached routes (paged; from 2.3 filtered by the selected listener, with effective hostnames); routes that failed to attach (paged; reason, message, and from 2.3 the explanation), with sub-filters Not accepted, Waiting for status, Unwatched namespace.

**HTTPRoute detail.** Up-chain breadcrumb ("A parent you cannot view" for a restricted step; "More paths not shown" when truncated); hostnames, or "Inherits the listener's hostnames", or for a child "Inherits the parent route's hostnames"; one card per parentRef with target, section or port, every condition and, from 2.3, the computed listeners each with its own effective hostnames; other controllers' entries collapsed; permission ("Allows any parent" or "Allows: …"); rules (matches, filters with config as written, backendRefs with weight; a delegation ref shows its mode and, from 2.4, its selected and attached counts; a Backend shows its state or "Restricted"); from 2.4, effective matches per parent path and the Delegation tab; child routes (reported, paged).

**ListenerSet detail.** Parent Gateway link (or "Parent Gateway not found"), conditions, the listener table, attached routes. **Allowed listeners (2.2, user, 2026-10-23).** When the ListenerSet is not accepted with reason `NotAllowed`, the page shows the parent Gateway's `spec.allowedListeners` as written next to that condition (or "Not set: the Gateway allows no ListenerSets"). When the field is absent, a hint adds: "If you set `allowedListeners` and it is missing here, the Gateway CRD in this cluster may not have the field. The API server drops fields its CRD does not define. Install a Gateway CRD from the same Gateway API channel and version as the ListenerSet CRD." **Detection (our call):** the UI does not read CRDs (D-017 §5 dropped that RBAC), so it shows the hint whenever the field is absent on the parent Gateway. It cannot tell an unset field from one the CRD lacks: both read as absent. Served versions do not settle it either, because the standard and experimental channels serve the same Gateway version. The hint is therefore worded as a possibility, never a diagnosis.

**Reverse lookup (2.2, user, 2026-10-23).** "Routes that send traffic here" opens the Routes list with `backend=…`. It appears on the YAML view of an admitted Backend, and on every backendRef of a route page ("Other routes using this backend"). Services are not watched, so they have no YAML view (§11); for a Service the entry point is the route's backendRef **(our call)**. Mirror targets are listed with a "Mirror" label.

### 8.6 States

| State | Behaviour |
|---|---|
| Loading | skeleton rows at the final row height; fixed-height header and tabs |
| Empty | per table, with a Clear filters action where filters apply |
| Error after data | keep the data, show `PageNotice` (`ResourcePage.tsx:174` pattern) |
| 404 | "Not in this install's scope", with a link back |
| 400 cursor | drop the cursor and load page one |
| Stale | `DataAge` turns stale; every badge shows unknown |
| Computed unknown, pending, restricted | the row says which; no number is shown as if known |
| Denied objects | once per list page, from `deniedInInstall` |
| Install switch | render only when the response's `install` matches the URL |

### 8.7 Interaction, layout and accessibility

- Every interactive element has hover, active and focus-visible styles in both themes. Tables keep fixed column widths from the first render, wrap long names, are keyboard-navigable and carry `aria-sort`.
- Tabs follow the WAI-ARIA tabs pattern; expanders carry `aria-expanded`; the up-chain is a `nav` with `aria-label="Route parents"`. The `ComputedBadge` tooltip is reachable by keyboard and in the accessible name.
- State is always icon + colour + text; contrast is checked in both themes by the existing token test.
- Below 900 px sections stack and tables move secondary columns into an expandable row; no horizontal page scroll at 720 px.
- Polling never resets scroll, focus or expanded state.

### 8.8 Wording pass

All new copy goes through the wording pass: short sentences, plain English, active voice; per string, a sub-agent picks the least machine-sounding of five accurate options. It covers titles, empty states, filter and flag labels, attachment labels, delegation outcomes and reasons, the badge, coverage and check states, unknown reasons, KGWUI-011 (including the orphan detail) and KGWUI-012 text, the Backend detail (a current `Accepted=True` reads "Configuration accepted", never "working") **(user, 2026-10-23)**, the allowed-listeners hint, the candidate banner, restricted placeholders, the denied-objects line and copy buttons. Drafts here are placeholders.

## 9. Testing

The boundary stays **cluster objects in → API JSON out** (design §9). The rule-level matrix of the computed views is `computed-rules.md` §11; this section lists the API, engine, frontend and cluster tests, and which matrix rows each sub-phase must pass.

### 9.1 API boundary tests (Go)

Seed the fake dynamic client, run the real cache, install, computed, health, explore and API packages, and call the handlers.
- **Lists (2.2):** each filter alone and combined; each sort both ways; malformed cursor, cursor from another sort, `limit=0` and `limit=201` → 400; `listener` without a suitable `parent` → 400.
- **Delegation cursor (2.4):** a page that ends between two rows of the same child under different rules or refs; `next` returns the remaining rows, and backward paging returns the same set.
- **Cursors across recomputes (2.2):** a page boundary both ways; insertion before the anchor; anchor deleted; descending ties; backward paging equals forward; a row whose sort value changes.
- **Attachment from status (2.2):** `via`; each of the three reasons with reason and message in `ParentSummary`; pending; unwatched. A stale `Accepted=True` is not `accepted`; a stale `False` is not `not-accepted`.
- **Reported vs declared (2.2):** a child with matching spec parentRef and status is a child. One with a spec parentRef and no status is a declared candidate: not in Child routes, `name` ref `not-reported`, KGWUI-011 info **with the `resource status` check pending and the install unknown inside the grace period**, warning and check OK after it, not counted. A parent with no delegation refs gets no children.
- **Stale matching entry (2.2):** a child whose matching entry is stale stays a child, owned and counted, with no KGWUI-011; KGWUI-003 info with the check pending inside grace, warning after. Counts are equal before and after the edit.
- **Unwatched declared route (2.2):** exactly one finding, KGWUI-004; no KGWUI-011; the check stays OK; install health unchanged.
- **Implicit fallback (2.2):** empty parentRefs with status is a child; a move from P to Q leaves no edge to P; gaining a Gateway parentRef removes the implicit edge.
- **Delegation shapes (2.2):** a three-level chain, a diamond, a repeated-diamond lattice (up-chain stops at 5 paths with `ancestorsTruncated`), a parent with refs and no reported child.
- **Other findings (2.2):** a child with hostnames and `UnsupportedValue` gets KGWUI-002; a parentRef to a missing HTTPRoute gets KGWUI-003; a no-parent route lands in `state=no-parent`, uncounted, and its YAML view resolves.
- **Phase 1 → Phase 2 behaviour change, Q12 (2.2).** Counts, findings and roll-up together, before and after: a mixed-role route owned once and judged on both; a child of two parents counted once; two installs sharing a controller name; a removed parentRef; a namespace leaving the scope; a stale implicit edge.
- **Isolation (2.2):** a child reported by another install's controller is not owned here; a child reported by parents in two installs appears in both.
- **Backend (2.2):** each row of the §6.2 table; none adds "not yet processed" or turns the install unknown.
- **Row state (2.2):** a warning plus a pending required check is `unknown`; an object with only info findings is `healthy` when every required check is OK; a KGWUI-011 info inside grace makes the required check pending, so every row of the install, including unrelated Gateways, is `unknown` until grace ends; a Gateway with failing routes keeps its own state; a Gateway with KGWUI-012 is `warning` (2.3).
- **ListenerSet (2.2):** `Accepted=True/ListenersNotValid`; `Accepted=False` with no listener entries; an XListenerSet hint; a missing parent Gateway; 200 ListenerSets paged on the Gateway page.
- **Admission and links (2.2):** every finding resource opens its view; spec-named objects are candidates; an unrelated object is 404 on both endpoints. For every reference in every golden, `link` agrees with the endpoints. The second step: D named only by candidate C is `none` and 404; owned, it is `detail`.
- **Authorizer (2.2, extended in 2.3 and 2.4):** rows, nested ListenerSets, delegation rows and children hidden; `total` visible only; `deniedInInstall` unchanged under every filter; a denied Backend is `{restricted: true}` with no message; a denied ancestor has no identity; derived counts drop denied objects. 2.3: a `Selector` listener over a hidden Namespace makes membership, count and check `restricted`, and the row carries no `listener-unknown`. 2.4: a hidden ancestor makes the child's effective contexts and delegation contexts `restricted`; a KGWUI-012 on a visible child whose evidence names a hidden parent has `evidenceRestricted` and no detail. Health and row states are unchanged in every case.
- **Computed views through the API (2.3, 2.4):** every row of `computed-rules.md` §11 that is not marked rule-only is also asserted through the handlers, with its `Computed` state, coverage and the KGWUI-012 or KGWUI-011 outcome.
- **KGWUI-011 orphan trigger (2.4):** an orphan dropped by every selecting wildcard parent with a complete traversal gets a warning; the same with one selecting parent incomplete gets none; a route with a Gateway parentRef that a wildcard selects and drops gets none, and its Delegation row is shown; an orphan another parent attaches gets none; a complete `cycle` outcome gets none; a route also selected by a `label` ref gets only the per-parent finding (`computed-rules.md` §11.5).
- **Reverse lookup (2.2):** `backend=` for a Service, a Backend and a mirror target; repeated values OR-ed; a delegation ref never matches; a malformed value is 400; a denied backend and an absent one give identical responses; hidden routes are not counted.
- **ListenerSet allowed listeners (2.2):** a `NotAllowed` ListenerSet whose parent Gateway sets `allowedListeners` shows it with no hint; one whose parent lacks the field shows the hint; an accepted ListenerSet shows neither.
- **KGWUI-004 detail (2.2):** a route naming an owned HTTPRoute or ListenerSet from an unwatched namespace says "targets HTTPRoute …" or "targets ListenerSet …"; the Gateway case is byte-identical to Phase 1.
- **Values easy to get wrong in detail views (2.2) (user, 2026-10-23).** Fixtures and goldens cover: a `RequestMirror` filter with its backendRef (and a percentage or fraction where set); weights, including an absent weight (shown as the default 1, marked as defaulted) next to an explicit 1 and an explicit 0; ports, including an absent port next to an explicit one (a missing port is never shown as 0); and filter order, preserved exactly as written across several filters of different types in one rule and in one backendRef.
- **Compatibility:** every Phase 1 golden passes, except the additive changes of §7.6.

### 9.2 Per-version goldens for the computed views

Fixture directories per rule set under `pkg/computed/testdata/<rules>/`, case list and provenance in `computed-rules.md` §11. A new kgateway minor gets a new directory, starting as a copy, plus a diff review of the cited files.

### 9.3 D-019 engine tests (deterministic, fake clock; 2.1)

The engine takes a clock, a trigger source and a snapshot source that can pause handler delivery. Tests drive them directly; nothing sleeps.
- **Debounce:** 500 events within 100 ms give one recompute, 250 ms after the last; a stream of one event per 100 ms for 5 s gives one per 1 s window.
- **Single flight:** a trigger during a run gives exactly one more run.
- **Safety timer:** no trigger for 29 s, no recompute; one at 30 s; `--poll-interval 2s` gives one every 2 s.
- **Interleavings:**
  - snapshot taken; an object changes during the run; the run publishes the old fingerprint; the trigger keeps the state pending; the confirm tick sees a different live fingerprint and does not confirm; the next run publishes the change;
  - **notification delivery paused:** the informer store changes while its handler is blocked; the confirm tick sees the new fingerprint, does not confirm, and makes the state pending; the next run includes the change, with no handler having run; when the handler finally runs, it only triggers one more recompute with an equal fingerprint;
  - an access transition with no object event changes the fingerprint and blocks confirmation;
  - a run blocked indefinitely: `ConfirmedAt` stops, the API serves it ageing, `computedAt` stays at the last completed run;
  - two requests during a swap each read one consistent `Published` (Model, Source and `ConfirmedAt` from the same value).
- **Confirmation:** idle, `ConfirmedAt` advances each second; `computedAt` does not.
- **Deadlines:** a grace expiry 60 s after first-seen triggers a recompute at that instant and the finding turns from info to warning. Metrics staleness with strict `age > maxAge`: a recompute at exactly `collectedAt + maxAge` does not mark it stale; the scheduler then runs at the next reading after the boundary, and that run does. A KGWUI-012 maturation deadline raises the finding with no other event (2.3).
- **Metrics:** a result arrival triggers one recompute.
- **Timestamps, per meaning:** `computedAt` equals the run's compute time and never moves without a run; at publication `confirmedAt` equals the snapshot's `TakenAt` (≤ `computedAt`); after the first idle confirm tick `confirmedAt` equals the tick time (> `computedAt`) while `computedAt` is unchanged; while pending or running `confirmedAt` is frozen. The overview, health and resource header carry `ConfirmedAt`; `checks[].collectedAt` and `since` keep their own values.
- Phase 1's engine tests run unchanged against the new loop.

### 9.4 Frontend (Vitest)

- Filters, sort, `listener` and cursor round-trip through the URL; a 400 drops the cursor.
- Each state of §8.6, including stale turning badges unknown with fake timers. With `confirmedAt` advancing and `computedAt` 30 s old, the cluster card, switcher and header stay fresh; with only `computedAt`, they use it.
- An install switch never shows the previous install's rows.
- `objectPath` follows `link` only; restricted findings never link.
- Every computed value renders a `ComputedBadge` with its coverage text; `not-verified` never renders the healthy icon; `restricted` renders the placeholder and no value; a controller value renders no badge.
- A disagreeing listener shows both numbers and the warning icon with text.
- `CorePages` with only the four Phase 1 slots renders the new pages; a contribution at `routes` cannot replace the core page; `getCommand` returning `undefined` hides the button.
- Skeletons and loaded rows have the same height.

### 9.5 kind e2e

Fixtures run on kgateway 2.3.11 and 2.4.7 in the existing matrix, added per sub-phase:
- 2.2: a Gateway with three listeners (HTTP, HTTPS with a wildcard hostname, TCP) and a `Selector` allowedRoutes; routes rejected with each of the three reasons; a ListenerSet with one invalid listener and a denied one; a delegation tree (name child, label child, wildcard parent, forbidden child, header-mismatch child); a child with hostnames; an inherit-parent-matcher child; a cycle; a static and an invalid `Backend`; a replaced rule.
- 2.3: the `sectionName` quirk; two parentRefs to one listener.
- 2.4: the controller-backed cases of `computed-rules.md` §11, marked **e2e**: an ancestor-pruned parent match; one child attached through one parent match and dropped through another; an attached child with a missing Service backend; a 2.3 child whose parentRef sets only a port.

**Assertions.**
- 2.2: the status-based pages show what the controller wrote.
- 2.3: on both tags, every listener's computed count equals `attachedRoutes`; every parentRef outcome is `status-compatible`; no KGWUI-012.
- 2.4: every child-entry and parent check is compatible under its rule set; no KGWUI-012. The backend-failure child is `status-compatible`, not a disagreement. The 2.3 port-only child has an entry carrying its port.
- A skewed fixture (a Namespace label edited while the controller is scaled to 0) raises KGWUI-012 at the scheduled maturation and clears when the controller returns (2.3).

**First task of 2.1 (cluster observation, like Phase 1's D-012).** Record the observations of `computed-rules.md` §10 on both tags. Normalised-identity unit tests cannot answer them. The result becomes a decision entry before the tasks that depend on it.

### 9.6 Scale tiers and budgets

The user set three tiers **(user, 2026-10-22)**. The user accepted the proposed shapes **(user, 2026-10-23; §15 Q6b)**:

| Shape | Typical | Large (capacity target) | Extreme (stress) |
|---|---|---|---|
| Gateways | 10–30 (bench: 30) | 50–100 (100) | 500 |
| Listeners per Gateway | 3 | 4 | 4 |
| HTTPRoutes | 100–500 (500) | 1,000–2,000 (2,000) | 5,000 |
| parentRefs per route | mean 1.2 | mean 1.3 | 1–4, mean 1.5 |
| ListenerSets | 10 | 200 | 2,000 |
| Delegated children | 10 %, depth ≤ 2, fan-out ≤ 10 | 10 %, depth ≤ 3, fan-out ≤ 50 | 10 %, depth ≤ 4, fan-out ≤ 100, plus a diamond lattice 8 layers deep |
| Parent matches per delegating rule | 1 | 2 | 4 |
| Dense selection (separate run) | — | 20 wildcard parents over a namespace of 500 routes | 100 wildcard parents over a namespace of 1,000 routes (hits the edge cap) |
| Namespaces / Backends | 20 / 50 | 100 / 200 | 500 / 1,000 |

Every tier also has 5 % each of GRPC, TCP and TLS routes, labelled namespaces behind some `Selector` listeners, and 5 % failed parentRefs.

**Budgets apply at Large**; Typical is the UX reference; Extreme reports numbers only. All budgets are **(our call)** on the CI runner class:

| Measure | Budget at Large | Sub-phase |
|---|---|---|
| Phase 1 compute | measured first, on Phase 1 code, as the baseline | 2.1 |
| Change burst: 1,000 events within 1 s | ≤ 2 recomputes; event-to-publication p95 ≤ 1.2 s | 2.1 |
| Idle 10 min | recomputes only from the safety timer and deadlines; confirm-tick CPU reported | 2.1 |
| One warm recompute | ≤ 100 ms with the explorer (2.2), ≤ 120 ms with view A (2.3), ≤ 150 ms with views B and C (2.4) | 2.2–2.4 |
| One cold recompute | ≤ 1 s | 2.2–2.4 |
| Allocation per warm recompute | ≤ 32 MiB | 2.2–2.4 |
| Heap of index, computed views and decode cache | ≤ 32 MiB above the baseline, per kind | 2.2–2.4 |
| Sustained stream (one event per 100 ms for 10 min) with 20 polling clients | ≤ 1 recompute per s; engine CPU ≤ 15 % of one core; GC share reported | 2.2–2.4 |
| List request per filter mix (none, parent, listener, namespace+state, `q`, `sort=health`, delegation of the widest parent) | p95 < 50 ms, p99 < 100 ms; p95 < 100 ms with 20 concurrent clients | 2.2–2.4 |
| Route detail on the deepest child | p95 < 50 ms | 2.2, 2.4 |
| Dense selection recompute | reported; the cut is deterministic | 2.4 |

**Method.** Go benchmarks with `-benchmem`. Percentiles come from a harness that sends 10,000 requests per filter mix through `httptest` and reports p50, p95 and p99 with `b.ReportMetric` (20 goroutines for the concurrent run). Burst and stream runs use a real clock, so latency is measured while Models swap. Numbers go into the sub-phase record with machine and Go version. A failing Large budget blocks the sub-phase.

### 9.7 Platform neutrality test

`explore.Build` and the rule sets run over a snapshot with no Pods or Deployments, namespace labels and settings as plain maps, and objects without `uid` or `resourceVersion`. They give the same rows; the decode cache is bypassed.

## 10. Platform neutrality (D-016)

| Addition | How it stays neutral |
|---|---|
| `pkg/explore`, `pkg/computed` | read objects only through `pkg/objects` and unstructured; no `corev1`, `appsv1` or client-go; covered by `internal/boundary/imports_test.go` |
| Namespace labels | a `map[string]map[string]string` built in `pkg/install` (allowlisted) |
| Controller settings | read from the Pod spec in `pkg/install`; `install.Resolved.Settings`; another provider supplies the keys or none ("unknown") |
| D-019 triggers and fingerprint | an abstract trigger source and a snapshot fingerprint; a file-mode provider feeds it from its own change stream (design §14.6) |
| Assignment | stays in `pkg/install/assign.go` |
| Decode cache | keyed by `uid` and `resourceVersion`, synthesised in file mode (design §14.8); missing keys bypass it |
| `Backend`, Service backendRefs | a kgateway CRD on every platform; Services shown as written, never watched |
| Copy command, URLs, readiness | `Vocabulary.getCommand`; opaque install IDs; no proxy or Pod data on the new pages |

## 11. Security

- **No Secret reads** (D-013). `certificateRefs` are text with `link: none`.
- **No TLS options** decoded (§4.1). **No admin server** (§3.7); RBAC stays get/list/watch, plus the `Backend` watch already covered by `gateway.kgateway.dev: *`.
- **Authorizer everywhere** (§7.4): denied and absent look the same; dependency-based visibility for computed facts and finding evidence; no filtered counts of denied rows.
- **Admission is bounded** (§7.1).
- **Settings are named, not dumped** (§3.4).
- **Input handling.** Params validated against fixed sets; cursors capped at 1 KiB and checked against the sort; messages and annotations render as text.
- **Trust model unchanged** (D-013): the pages show only what the ServiceAccount could read through the YAML view, plus what the UI computes from it.

## 12. Cost

This phase generates no data-plane configuration and no xDS churn.

| Item | Cost |
|---|---|
| API server | one more cluster-wide watch (`backends`); event handlers add no API calls |
| Recompute | only on change, debounced, plus the safety timer and deadlines. Ownership BFS, view A and `explore.Build` are linear in objects and parentRefs × listeners. Views B and C are capped by the selected-edge, context and matcher bounds of `computed-rules.md` §5.7. |
| Idle | a 1 s confirm tick hashes the stores, compares the source fingerprint and republishes `ConfirmedAt`; no recompute |
| Persistence state | one map entry per candidate check, pruned with its subject |
| Up-chain | per detail request, at most 256 node visits (§5.3) |
| Retained contexts and effective matchers | in the Model, capped by `computed-rules.md` §5.7 |
| Decode | once per object change (§3.3) |
| Memory | index rows, computed views, decode cache and, briefly, the previous Model; measured per kind (§9.6) |
| Per request | one atomic load, one index lookup, a filter scan, dependency checks under non-`AllowAll`, an optional sort, one page |
| Client | one list request per visible table every 5 s, at most 200 rows each |

## 13. Sub-phases

The user decided all of views A, B and C are in Phase 2, staged **(user, 2026-10-22)**. Each sub-phase has its own brief, a cross-model review of its diff (D-020 does not lower that bar), its own benchmarks at Large, and ships alone. Every task states its file list for approval (change-size guardrail).

| Sub-phase | Content | Ships |
|---|---|---|
| **2.1 Engine and timestamps** | The cluster observation task (§9.5, `computed-rules.md` §10). The Phase 1 baseline benchmark, on Phase 1 code. D-019: source fingerprint, scheduler, debounce, single flight, publication, confirmation, deadlines (§3.1). `confirmedAt` and the SPA's three consumers. §9.3 tests. README and chart notes on `--poll-interval`. | Same pages as Phase 1, event-driven, with honest freshness |
| **2.2 Explorer on observed status** | Q12's status-based ownership: reported edges, declared candidates, KGWUI-011 status trigger with pending in grace, no-parent rows, KGWUI-003/004 extensions. The `Backend` kind and rules. Typed views and the decode cache. `pkg/explore` indexes. Admission and link metadata. List and detail APIs, the Authorizer, goldens. Frontend: Gateways, ListenerSets and Routes lists; Gateway, ListenerSet and HTTPRoute pages with status, parent cards, rules, reported children and the up-chain; finding retargeting; copy helpers; `CorePages` slots; wording pass. Release note on changed counts. | Every page, with listener `attachedRoutes` from status and no computed facts |
| **2.3 View A** | `pkg/computed` seam, both rule sets for view A, the experimental setting read, per-listener and per-parentRef DTOs, the listener filter, the explanation, the cross-check for `listener-count` and `route-accepted`, the KGWUI-012 persistence map and maturation deadline, coverage badges, dependency-based authorization for view A. Matrix `computed-rules.md` §11.1, §11.4 (view A rows), §11.5. kind e2e count assertions. | Routes per listener with effective hostnames, labelled and cross-checked |
| **2.4 Views B and C** | Context-aware traversal, bounds and truncation, the wildcard setting read, the Delegation endpoint and tab, effective matchers per path, the 2.3 and 2.4 report keys, `child-entry` and `parent-resolvedrefs` checks, KGWUI-011's computed trigger, dependency-based authorization for contexts and evidence. Matrix §11.2-§11.4. The dense-selection benchmark. kind e2e for the controller-backed cases. | Delegation selection, outcomes and effective matchers |

2.3 and 2.4 depend on 2.2's DTOs and pages, and 2.4 on 2.3's roots. 2.2 does not depend on any computed view.

## 14. Upstream asks (draft text only, not filed)

Each needs a check against kgateway `main` before filing; they are filed as §15 Q5 decided **(user, 2026-10-23)**. D-020 makes each computed view a bridge: when kgateway reports the fact, the UI switches to it and keeps its computation only as a cross-check.

| # | Ask | Motivation |
|---|---|---|
| 14.1 | Publish which listeners each route attached to, with effective hostnames | per-listener results are computed (`query/route.go:437-547`, `query/query.go:231-296`) but published nowhere; `NoMatchingListenerHostname` carries no message. On 2.4 listener isolation narrows hostnames further. |
| 14.2 | Report delegation outcomes per child: selected, attached, dropped with the failing match, forbidden, and the effective matches | a dropped or forbidden child gets no status; the parent shows `BackendNotFound` only when no child is left; effective matchers are visible only in xDS. A fully dropped child could get `Accepted=False` with a reason such as `NoMatchingParentRule`. |
| 14.3 | A controller-written, read-only state resource outside Gateway API (user's proposal) | Gateway API status has no room for resolved relationships. Split per Gateway and per delegating parent, size-capped with a truncation marker, `observedGeneration` per input, no admin port. Umbrella for 14.1, 14.2 and 14.1's hostnames. |
| 14.4 | A condition on a route whose match is fully shadowed on the same listener and hostname, naming the winner | equal sort keys pick the oldest route silently (`translator/routeutils/sortable_route.go:120-171`); gateway-api#4303 tracks the spec side |
| 14.5 | Clear kgateway-owned parent entries from routes translation no longer reports, in every case (only if §9.5 confirms the gap) | 2.4 clears a matcher-dropped child (`pkg/krtcollections/policy.go:1150-1178`); other cases may keep stale entries (`pkg/reports/status.go:452-472`) |
| 14.6 | Add the effective `DELEGATION_WILDCARD_NAMESPACE` and the experimental-features flag to the pending scope endpoint proposal | tools stop reading Pod env; D-017 §4 already wants the flag |
| 14.7 | A separately bindable, read-only snapshot endpoint with route source metadata on by default | the admin server binds to localhost, shares a port with `/logging` and pprof, and needs `KGW_ENABLE_ROUTE_SOURCE_METADATA` (§3.7) |
| 14.8 | A status signal when the data plane rejects a route's config **(user, 2026-10-23)** | Envoy can reject (NACK) a configuration update that includes a route, while the route stays `Accepted=True` and later edits stop taking effect. Nothing in route or Gateway status says so. Ask: a condition per route (or per Gateway, naming the routes) when the proxy rejected the last config that contained it, with the rejection message, cleared when a later version is accepted. Open question: per route or per Gateway at scale. |

## 15. Questions for the user

### 15.1 Decided in review

| Former | Question | Decision |
|---|---|---|
| Q2 | Hostname intersection: compute or show both sides? | Compute it in view A, labelled and cross-checked; also a limitation and upstream ask (§14.1) **(user, 2026-10-22)** |
| Q3 | Shadowed matches and inherited matchers? | Shadowed matches stay the controller's job (§14.4); effective matchers in both modes are computed (view C) **(user, 2026-10-22)** |
| Q6 | Scale target? | Three tiers; budgets apply at Large (§9.6) **(user, 2026-10-22)** |
| Q8 | Routes per listener? | A product requirement: view A, cross-checked against `attachedRoutes` **(user, 2026-10-22)** |
| — | Recompute model | Event-driven (D-019) **(user, 2026-10-22)** |
| — | Mirroring boundary | Overridden for views A, B and C under D-020's rules **(user, 2026-10-22)** |
| — | GRPCRoute detail page | No: GRPCRoute stays a list row plus the YAML view in Phase 2 **(user, 2026-10-23)** |
| — | Research addendum | From the support-ticket research: the ListenerSet page shows the parent's `allowedListeners` with a CRD hint; Backend `Accepted=True` reads "Configuration accepted"; a reverse lookup from a backend to its routes; an upstream ask for data-plane rejection (§14.8); goldens for values easy to get wrong **(user, 2026-10-23)** |
| — | Scope and staging | All of views A, B and C in Phase 2, in four sub-phases, each reviewed cross-model and shippable: 2.1 D-019 and timestamps, 2.2 explorer on status, 2.3 view A, 2.4 context-aware B and C (§13). Rules move to `computed-rules.md`. **(user, 2026-10-22)** |
| Q1 | Policies attached to a route or Gateway? | Later (Phase 5): a list without precedence can mislead **(user, 2026-10-23)** |
| Q4 | A graph view? | Later **(user, 2026-10-23)** |
| Q5 | How to file the upstream asks (§14)? | §14.3 as an umbrella proposal, with 14.1 and 14.2 as its motivating cases; 14.4 and 14.7 separate; 14.6 joins the pending scope endpoint proposal **(user, 2026-10-23)** |
| Q6b | Shapes per tier (§9.6), including parent matches and dense selection | As proposed **(user, 2026-10-23)** |
| Q7 | Re-run the research before plan approval? | Yes: re-run the support-ticket and knowledge-base research before the plan is approved **(user, 2026-10-23)** |
| Q9 | Keyset cursors without page numbers (§3.6)? | Yes **(user, 2026-10-23)** |
| Q10 | No-parent HTTPRoutes (§4.2) | Listed under `no-parent` in every install whose scope holds them; not counted; no status finding; YAML view; the Authorizer applies **(user, 2026-10-23)** |
| Q11 | Backend freshness (§6.2) | Current `False` error; current `True` healthy; stale unknown with no finding; no evidence: `False` warning, `True` unknown; absent unknown. Never green without evidence, never turns the install unknown **(user, 2026-10-23)** |
| Q12 | Delegated children in tiles and health (§4.2) | They count, status-based. Computed views never own; a computed-only child is a candidate checked as "missing expected status"; per-ref outcomes are aggregated per report key; an eligible entry keeps the edge whether current or stale **(user, 2026-10-23)** |
| Q13 | KGWUI-012 severity and persistence | Warning. Contradicting current status: 30 s after the check's fingerprint last changed. Missing expected child status: after both the 60 s grace period and 30 s, while the parent's status is current. Stale: never. Scheduled deadline; unrelated writes never reset (`computed-rules.md` §9) **(user, 2026-10-23)** |
| Q14 | D-019 timing (§3.1) | 250 ms quiet, 1 s maximum, safety timer 30 s, confirm tick 1 s; event-to-publication p95 ≤ 1.2 s at Large; deadlines at their instant **(user, 2026-10-23)** |
| Q15 | Routes selected only by a `wildcard` ref and dropped | A computed KGWUI-011 **only for an orphan**: a route with an empty `spec.parentRefs` list, selected only by wildcard refs, dropped or forbidden by every selecting ref of every owned parent in every context, with a complete traversal for every selecting parent. Then no parent route of this install keeps it; the conclusion is install-local. A route with its own parentRefs that a wildcard selects and drops gets no finding and stays in the Delegation tab with its reason. `name` and `label` refs keep their trigger (§6.2) **(user, 2026-10-23)** |
| Q16 | `KGW_ENABLE_EXPERIMENTAL_GATEWAY_API_FEATURES` for view A (§3.4) | Read it explicitly from the controller Pod, for the computed views only; unreadable means "unknown"; D-017 §4 unchanged **(user, 2026-10-23)** |

### 15.2 Still open

None. Every question above is decided. The plan starts after the user approves this design.
