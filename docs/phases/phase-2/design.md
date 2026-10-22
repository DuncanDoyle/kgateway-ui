# Phase 2 — Gateway & Route Explorer: Design

**Status:** Revised after the cross-model review (`reviews/design-codex.md`: 13 findings, then 3 re-review findings, all accepted) and the user's design review (2026-10-22, D-019, D-020), for approval
**Date:** 2026-10-21, revised 2026-10-22
**Base:** `2c25e35`
**Roadmap:** `docs/roadmap.md`, Phase 2
**Decisions:** D-001 … D-020 in `docs/decisions.md`
**kgateway refs:** every behaviour claim cites `file:line` at tag `v2.4.7` (`b4a2bf74`) or `v2.3.11` (`c2af2887`). Paths without a `pkg/` or `api/` prefix are under `pkg/kgateway/`. The UI supports both tags.

Choices the controller made without the user are marked **(our call)**. Choices the user made are marked **(user, 2026-10-22)**. §15 lists what is still open.

## 1. Goal and scope

Operators debug Gateway API routing by reading `status.parents[].conditions` in YAML, by admin snapshots and by Envoy config dumps. Support and field threads show four questions come up most:
- Which routes are on this Gateway, and on which listener?
- Why is this route not attached?
- Which child routes did delegation pick up, and which did it drop?
- Which route is the broken one?

Phase 2 answers these from the cluster's own objects and status. Where status cannot answer, the UI computes three views from the same rules the controller applies. It labels each computed fact and cross-checks it against what the controller reports (D-020).

**In scope**
1. **Gateway detail page** (roadmap):
   - listeners with name, port, protocol, hostname, TLS mode, certificateRef names (never Secret content) and allowedRoutes;
   - addresses;
   - Gateway and per-listener conditions with reason, message and freshness;
   - **routes per listener**, computed and cross-checked against the listener's `attachedRoutes` (§3.4, D-020 view A);
   - attached ListenerSets.
2. **Routes per Gateway, including the failed ones** (new, fits). Next to the attached routes, the page lists the routes whose parentRefs target this Gateway but were not accepted, with the controller's reason and message.
   - Only routes whose parentRef targets **this install's** Gateway or ListenerSet are listed. That includes routes in a namespace the controller does not watch, which get KGWUI-004 (`pkg/install/assign.go:155-190`).
   - Routes for other controllers, whether other kgateway installs or other Gateway API implementations, are never listed.
3. **ListenerSet detail page** (roadmap): parent Gateway, listeners, conditions (including the partial failure `Accepted=True/ListenersNotValid`), routes per listener and attached routes. It covers ListenerSet v1, XListenerSet and any ListenerSet kind a downstream registers.
4. **HTTPRoute detail page** (roadmap):
   - hostnames, and the effective hostnames per listener (computed, view A);
   - each parentRef with its status conditions;
   - rules with their matches, filters and backendRefs (with weight).
   Backend resolution comes from route conditions (`ResolvedRefs`). The UI does not re-check ReferenceGrants.
5. **Delegation** (§5, D-020 views B and C):
   - **Status:** what kgateway writes on child routes confirms a delegation.
   - **Permission:** a child's optional parentRef is a permission. When set, only the parents it names may delegate to it (`v2.4.7 utils/delegation/delegation.go:14-31`).
   - **Computed:** which children each delegation ref selects, which are permitted, which fit the parent's match, and which were dropped. It also computes the effective matcher of each child.
   - All three are shown side by side.
6. **Navigation both ways** (roadmap). Route → Gateway (up the delegation chain), Gateway → listener → routes, ListenerSet ↔ Gateway ↔ routes, child ↔ parent route. Every object gets a stable deep link. Finding links open these pages, and the YAML view stays as a tab.
7. **Server-side lists** (roadmap). Per install: Routes, Gateways and ListenerSets, indexed once per model recompute, filtered, sorted and paginated (§3).
8. **Event-driven recompute** (D-019). The model is recomputed when the cluster changes, not on a fixed 2 s ticker.
9. **`Backend` kind** (Phase 1 carry-over, design §14.5). Registered with its `Accepted` condition and an assignment rule (§4.4).
10. **Copy helpers.** Copy `namespace/name` (exists), and copy a get-as-YAML command for the object.

**Out of scope (later phases or upstream)**
- Effective policy merge and which policy wins (Phase 5; needs controller output).
- Envoy/xDS view per route, request-matching simulation and full precedence order (Phases 6 and 9).
- **Shadowed or duplicate matches.** This stays the controller's job. kgateway writes no condition for it, and its precedence sort is controller logic (`v2.4.7 translator/routeutils/sortable_route.go:22-37,120-171`). It is an upstream ask (§14.4); the UI will consume such a condition later **(user, 2026-10-22)**.
- A graph or topology view. Tables and drill-downs come first.
- Detail pages for GRPCRoute, TCPRoute and TLSRoute. They appear in lists, and view A counts them; their rows link to the YAML view.
- The controller admin server's `/snapshots/xds` (§3.8).

**Anti-goals**
- No computed fact without its label and its cross-check (D-020).
- No route precedence, no ReferenceGrant evaluation, no backend resolution, no policy merge.
- No new data source: no admin server, no Envoy admin, no Secrets.

## 2. Primary flows

Each flow starts from a deep link, a finding or a list. All URLs are under `/i/:id/` (§8.1).

1. **"Which routes are on this Gateway, and on which listener?"**
   - Open Gateways, filter by name, open `infra/edge`.
   - The Overview tab shows the Gateway's conditions, then a listener table. Each listener row shows the controller's `attachedRoutes` next to the UI's computed count, with "Agrees", or "Disagrees" and both numbers (§3.4).
   - Selecting a listener filters **Attached routes** to that listener's computed members. Each row shows the effective hostnames on that listener, labelled "Computed by the UI".
   - Routes that come through a ListenerSet show the ListenerSet they came through.
2. **"Why is this route not attached?"**
   - The same page has **Routes that failed to attach**. It lists the install's routes with a current `Accepted=False` for this Gateway. Each row shows the raw reason (`NotAllowedByListeners`, `NoMatchingParent` or `NoMatchingListenerHostname`, `v2.4.7 query/route.go:525-543`) and the UI's computed explanation, for example "Computed by the UI: listener `https` allows namespaces with `team=web`; namespace `shop` has `team=retail`".
   - It also lists routes still waiting for status, and routes in namespaces the controller does not watch (KGWUI-004), each with its own label. These open their detail page as diagnostic candidates (§7.1).
   - The route page shows each parentRef with every condition, its reason, message and freshness ("Describes generation 3; the route is at 4").
3. **"Which child routes did delegation pick up, and which did it drop?"**
   - Open the parent HTTPRoute. Each delegation ref shows as written: by name, `*`, or label.
   - **Delegation** shows, per ref, every route the UI computes as selected, labelled "Computed by the UI", with an outcome:
     - Attached;
     - Dropped: no rule fits the parent's match `/api` (PathPrefix);
     - Forbidden: its parentRefs do not name this route;
     - Rejected: it sets hostnames;
     - Cycle.
   - Next to each outcome is what the controller reports: the child's status entry for this parent, or "No status", and the parent's own `ResolvedRefs` condition. A mismatch shows as "Disagrees with the controller" (§6.2, KGWUI-012).
   - A child row opens the child. Its page shows the chain up to the Gateway and its **effective matches** per parent (view C).
4. **"Which route is the broken one?"**
   - Open Routes and filter by state, for example "Rules replaced or dropped". The filter covers `RouteRuleReplaced` and `RouteRuleDropped` on both versions (§6.1).
   - The table names each route, its parent, the failing reason and message, and its health. The filter lives in the URL, so the link can go into a support ticket.
5. **"Is this ListenerSet working?"**
   - Open it from the Gateway page or the ListenerSets list. It shows the parent Gateway, its conditions (`Accepted=True/ListenersNotValid` means some listeners failed), the listener table with computed and reported counts, and the attached routes.

## 3. Architecture

```
 informer events ─┐   metrics result ─┐   grace/staleness deadlines ─┐   safety timer (--poll-interval)
                  ▼                   ▼                              ▼
              trigger (debounce 250 ms, max latency 1 s, single flight)          D-019
                  ▼
 engine.Compute
   snapshot → install.Discover/Resolve/Assign → computed views (A, B, C) → health.Evaluate
            → overview.Build → explore.Build (per install)
   Model{Installs[i].Explore *explore.Index}   immutable, swapped atomically

 api (new handlers)  →  Model.Installs[i].Explore  →  filter → authz → sort → page
```

### 3.1 Recompute on change (D-019)

Phase 1 recomputes the whole model on a fixed ticker every `--poll-interval`, 2 s by default (`pkg/engine/engine.go:209-222`, `cmd/kgateway-ui/main.go:23`). Most of those recomputes see an unchanged cache. Phase 2 makes the recompute event-driven **(user, 2026-10-22; details our call)**:

| Trigger | Source |
|---|---|
| Object added, updated or deleted | an event handler on every informer in `pkg/cache`: dynamic kinds, Namespaces, Pods, Deployments |
| A kind's access changes (D-014) | a 1 s confirm tick compares each informer's access state with the last model's; a difference triggers |
| A metrics scrape result arrives | the collector notifies the engine; the scrape cadence (`--metrics-interval`) stays separate |
| Served kinds change | the kinds monitor notifies the engine |
| A time-driven deadline passes | the engine schedules the earliest grace expiry (design §5.2) and metrics-staleness deadline (design §5.4) it knows of |
| Safety timer | `--poll-interval`, now "maximum time between recomputes" |

- **Debounce.** A trigger starts a 250 ms quiet window, and each new trigger restarts it. The recompute runs when the window ends, or at the latest 1 s after the first trigger of the burst. **(our call; §15 Q14)**
  - 250 ms coalesces the event burst of one `kubectl apply` or Helm upgrade into one recompute.
  - 1 s bounds the latency during a continuous stream, such as a rollout updating Pods. A user then sees a change within about 1 s plus the SPA's 5 s poll.
- **Single flight.** At most one recompute runs at a time. Triggers during a recompute mark the model dirty, and one more recompute follows, still debounced.
- **`--poll-interval`** keeps its name and type. Its meaning becomes "recompute at least this often". The default rises from 2 s to 30 s. Setting it to 2 s restores Phase 1's cadence as a floor, so existing settings stay valid. **(our call)**
- **Freshness.** `collectedAt` stays on every response (design §6.6). It now means "the time the backend last confirmed this model matches its cache", not "the time of the last recompute".
  - The confirm tick sets it every second when no trigger is pending and no access state changed. Nothing changed, so the model is still exact.
  - If a recompute hangs, or the engine stops, `collectedAt` ages and the SPA's stale rule fires (data older than 2 × 5 s). D-006 holds: stuck data is never shown green.
  - Time-driven state, such as a grace period ending, still changes on time, through the scheduled deadline.
- **The SPA keeps its 5 s poll.** A request is served from the in-memory model and costs well under the list budget (§9.6), so polling stays cheap. Push (SSE) stays out of scope (design §1). Polling also renews `collectedAt`, which is how the client notices a stuck backend.
- **Immutable model.** Unchanged. Each recompute builds a new Model and swaps it atomically (`engine.go:181-183`). Between changes the Model, and so every cursor and list order, stays the same.

### 3.2 Where the index lives

- A new package `pkg/explore` builds one `Index` per install, inside `engine.Compute`, after health (`engine.go:169-180`). It is stored on `InstallModel` and is immutable once the Model is swapped in, like the overview and the health report. **(our call)**
- Handlers read only from the current Model, so a list request never touches the API server or the informers.
- `explore.Build` is a pure function of `(install.Resolved, *install.Owned, *cache.Snapshot, computed views, health.Report, decoded views)`.
- Per request, the API only filters and pages what the index holds. The one exception is counts derived from other objects under a non-`AllowAll` Authorizer, which are recounted over the visible objects (§7.4).
- Downstream builds get the index through the Model. The only new registry is the computed-rules seam (§3.5).

### 3.3 Decoded views are cached across recomputes

Today every `objects.*` accessor decodes the whole object again through `FromUnstructured` (`pkg/objects/objects.go:116-123`), so a route's status is decoded several times per compute.
- The engine keeps a decode cache keyed by `(uid, resourceVersion)`. Each entry holds the typed views of §4 for one object. Entries absent from the snapshot are pruned in `observe` (`engine.go:114-130`), as the grace map is today. **(our call)**
- An unchanged object is decoded once, not once per recompute.
- An object without a `uid` or `resourceVersion` is decoded every time, so a provider that lacks them still works (D-016 R5, design §14.4).
- The views are read-only and shared between Models.

### 3.4 Routes per listener (D-020 view A)

Route status is written per parentRef, not per listener, and listener status carries only a count (`attachedRoutes`). kgateway exposes the per-listener route list nowhere: not in status, not in metrics, not on the admin server except inside the xDS snapshot (§3.8). Per-listener attachment is a product requirement **(user, 2026-10-22)**. So the UI computes it from kgateway's rules and cross-checks it.

**The rule** (`v2.4.7 query/route.go:437-547`; `v2.3.11 query/route.go:397-508`; the attachment files are byte-identical at both tags). For each Gateway, and for each **allowed** ListenerSet of it (`route.go:393-410`):

1. **Candidates.** Routes with a parentRef whose (group, kind, namespace, name) names this resource. Defaults: group and kind give Gateway; namespace gives the route's own (`pkg/krtcollections/policy.go:1247-1274`). A Gateway parentRef never reaches ListenerSet listeners, and a ListenerSet parentRef never reaches Gateway listeners.
2. **Per parentRef and per listener:**
   - **allowed kinds:** protocol defaults (HTTP/HTTPS → HTTPRoute, GRPCRoute; TLS Terminate → TLSRoute, TCPRoute; other TLS → TLSRoute; TCP → TCPRoute; UDP → none; any other protocol → HTTPRoute), intersected with `allowedRoutes.kinds` when set; a nil group means `gateway.networking.k8s.io`; only the kind is compared (`route.go:157-221`);
   - **allowed namespaces:** default `Same`, meaning the namespace of the resource (for a ListenerSet listener, the ListenerSet's); `All`; or `Selector` evaluated against the Namespace object's labels. A missing Namespace matches only an empty selector (`pkg/krtcollections/namespaceselector.go:20-28`). A nil selector with `From: Selector` is a listener error;
   - `allowed = true` once kind and namespace pass on **any** listener, before the section check;
   - **section:** `port` if set must equal the listener port, and `sectionName` if set must equal the listener name (`query/query.go:170-178`) → `matched`;
   - **hostnames** (HTTPRoute, GRPCRoute, TLSRoute): the intersection rules of `query/query.go:231-296` → `hosts`, plus the effective hostnames.
3. **Outcome per parentRef:** not `allowed` → `NotAllowedByListeners`; else not `matched` → `NoMatchingParent`; else not `hosts` → `NoMatchingListenerHostname`; else accepted on each listener that passed.

The UI reproduces one quirk exactly. `allowed` is decided across all listeners before the section check. So a parentRef with `sectionName: b`, where listener `a` allows the route's namespace and `b` does not, reports `NoMatchingParent`.

**Inputs the UI needs:**
- Gateways, ListenerSets and routes (watched today);
- Namespace labels (watched today, `pkg/cache/cache.go:47-57`);
- whether a ListenerSet is allowed.

The last input is taken from the ListenerSet's own status, not computed. A ListenerSet the controller denied has `Accepted=False` (D-017 §2, `pkg/krtcollections/policy.go:607-622`). The allowedListeners rule is not reproduced, and this input is labelled. **(our call)**

**Scope.** Only routes in namespaces the controller watches are candidates. The controller cannot see others (`pkg/pluginsdk/collections/collections.go:105`). Routes of experimental-only kinds count only when the controller's experimental features are on (§3.6).

**Cross-check.** The computed count per listener uses exactly the controller's semantics (`translator/gateway/gateway_translator.go:110-121`):
- one entry per (route, parentRef) that passed on that listener, so a route with two matching parentRefs counts twice;
- top-level routes only, never delegated children;
- counted before backend resolution, delegation outcomes and listener validation.

It is compared with `status.listeners[].attachedRoutes` of the Gateway and of each allowed ListenerSet. The per-parentRef outcome is also compared with the route's `Accepted` condition and reason. Status entries are keyed without port (`pkg/reports/reporter.go:424-450`), so two parentRefs that differ only by port are compared as one.

**What the page shows:**

| State | Listener row | Route rows |
|---|---|---|
| Agrees | "12 routes (controller: 12)" | routes listed under the listener, labelled computed |
| Disagrees | "Computed 12, controller 11. The UI's computation disagrees with the controller." | routes still listed under the listener, with the disagreement banner; the controller's count is authoritative |
| Input missing | "Unknown: Namespace labels unavailable" (or "ListenerSet has no status", "rules for this controller version unknown") | routes listed under "Listener unknown" |

KGWUI-012 is raised only after a disagreement persists (§6.2). The old "All listeners" fallback exists only in the last two states. There it reads "Listener unknown" and says why. **(user, 2026-10-22; wording our call)**

**Programming-only rules are not part of view A.** Two things narrow what Envoy finally serves without changing status or counts:
- 2.4's HTTP listener isolation, which drops a hostname from a less specific listener (`v2.4.7 translator/listener/gateway_listener_translator.go:689-747`; 2.3 has none);
- listeners dropped by validation (`gateway_listener_translator.go:62`).
The UI shows status semantics. The page notes "Envoy may serve fewer hostnames on this listener" when a more specific listener shares the port on 2.4. **(our call)**

### 3.5 The computed-rules seam

The three computed views sit behind one interface in a new package `pkg/computed` **(our call)**:

```go
// Rules computes the views of D-020 for one controller version.
type Rules interface {
    // Version names the pinned rule set, e.g. "kgateway-2.4".
    Version() string
    Attachment(in AttachmentInput) AttachmentResult      // view A
    Delegation(in DelegationInput) DelegationResult      // views B and C
}
```

- It follows the `proxymode.Resolver` pattern (`pkg/engine/engine.go:44-45`). `engine.Config` takes a `RulesFor(controllerVersion) Rules` function, and downstreams register theirs through `extension.WithComputedRules`.
- Upstream ships `kgateway-2.3` and `kgateway-2.4`. They share the attachment and selection code, which is byte-identical at the two tags (p2-rules). They differ in the status they expect (§5.4) and in 2.4's programming-only listener isolation.
- **Version selection:** the controller minor, as in `kinds.ExperimentalOnly` (`pkg/kinds/upstream.go:55-94`). An unknown version uses the newest set, labelled "Controller version unknown; using the 2.4 rules". A newer minor than the newest set is labelled "Rules not yet verified for 2.5". The cross-check still runs in both cases.
- **Pinned per version:** golden fixtures per rule set (§9.2). A change to kgateway's rules is a new rule set, not an edit of an old one.
- **Inputs are neutral.** The rules take decoded views, a namespace → labels map and a settings map, never `corev1` types (§10).

### 3.6 Controller settings the computed views read

Settings are read from the controller Pod spec as `KGW_DISCOVERY_NAMESPACE_SELECTORS` is today, with D-015's fallback: a Ready Pod, any Pod, then the Deployment's Pod template (`pkg/install/scope.go:95-140`).

| Env | Default | Used by |
|---|---|---|
| `DELEGATION_WILDCARD_NAMESPACE` (no `KGW_` prefix) | `all`; an empty value also means `all` (`api/labels/delegation.go:12`) | view B: a label ref with this namespace searches every namespace |
| `KGW_ENABLE_EXPERIMENTAL_GATEWAY_API_FEATURES` | `true` (`v2.4.7 api/settings/settings.go:324`; `v2.3.11` `:306`) | view A: whether experimental-only kinds are candidates |

- `valueFrom`, or an `envFrom` without an explicit entry, gives "unknown". The computed views that depend on the setting then show "Unknown: the UI cannot read `DELEGATION_WILDCARD_NAMESPACE`", and their cross-check is skipped.
- kgateway reads the wildcard once at process start (`api/labels/delegation.go:12`). The Pod spec is what runs, and the template fallback describes the next rollout, as D-015 says.
- D-017 §4 is unchanged: the scope verifier still compares stable kinds only. Reading the experimental flag here serves the computed views alone. **(our call; §15 Q16)**
- `KGW_WEIGHTED_ROUTE_PRECEDENCE` and `KGW_VALIDATION_MODE` are not read: precedence and replacement are not computed.
- The settings are part of the install's provider data (`install.Resolved.Settings`, a map). A non-Kubernetes provider supplies them its own way.

### 3.7 What is indexed

| Index | Key | Value | Serves |
|---|---|---|---|
| Route rows | — | `[]RouteRow`, sorted by (namespace, name, group, kind) | Routes list, default order |
| By parent | Gateway or ListenerSet ref | row indexes, with the parentRef and attachment state | Gateway and ListenerSet pages |
| By listener | (resource, listener) | computed member rows, effective hostnames, cross-check state (view A) | listener filter, listener table |
| By parent route | HTTPRoute ref | rows of the children with a reported edge (§4.3) | Child routes, up-chain |
| Delegation | (parent route, rule, ref) | computed selected children with outcome and reason (view B) | Delegation table |
| Declared candidates | HTTPRoute ref | rows whose spec names it with no reported edge (§4.3) | KGWUI-011 |
| Parent routes | route ref | the parent HTTPRoutes of its reported edges | bounded up-chain on request (§5.5) |
| By namespace | namespace | row indexes | namespace filter |
| By backend | Backend ref | route row indexes | Backend tile, route → Backend links |
| Condition state | — | a bit set per row (§6.1) | state filter |
| Findings by object | object ref | worst error and warning finding on it | row state (§6.3), health filter |
| Gateway rows | — | `[]GatewayRow` (§7.3) | Gateways list |
| ListenerSet rows | — | `[]ListenerSetRow`, also by parent Gateway | ListenerSets list, Gateway page |
| No-parent rows | — | in-scope HTTPRoutes with no parent (§4.3) | Routes filter `no-parent` |

- The findings map replaces today's per-row scan of all findings (`pkg/overview/overview.go:166-187`).
- Up-chains and per-path effective matchers are not stored. They are computed per detail request with a bounded walk (§5.5), so diamonds cannot blow up the index. **(our call)**
- Hostname and name search is a scan over pre-lowercased strings in the rows. At the Large tier (§9.6), a scan is cheaper than keeping an n-gram index current. **(our call)**

### 3.8 Why the admin snapshot is not used

The controller's admin server does hold the effective config. `/snapshots/xds` has the Envoy routes per proxy, with post-delegation matchers (`admin/xds_snapshot.go`; route names at `translator/irtranslator/route.go:725-739`). Phase 2 still does not read it **(our call)**:
- **Reach.** It binds to `localhost:9095`. On 2.3 that is hard-coded (`v2.3.11 admin/server.go:87`). On 2.4 it is `KGW_ADMIN_BIND_ADDRESS`, default `localhost` (`v2.4.7 api/settings/settings.go:200`, `admin/server.go:27-30,94`). No containerPort and no Service exist. From the UI pod the only path is `pods/portforward`, a write-class RBAC verb (D-001, design §8).
- **Surface.** The same server serves `/logging`, which can change the log level, and `/debug/pprof/`. Opening it for a read-only UI widens the surface well past what the UI needs.
- **Attribution.** Route source metadata is needed to map an Envoy route back to its HTTPRoute reliably. It exists only on 2.4, behind the experimental `KGW_ENABLE_ROUTE_SOURCE_METADATA`, default off (`v2.4.7 api/settings/settings.go:332`, `translator/irtranslator/policy.go:123-160`).
- `/snapshots/krt` is an internal dump, not a contract (D-009).

The snapshot stays a candidate optional cross-check for Phase 9. Upstream asks for a safe read-only source are in §14.

### 3.9 Pagination: keyset cursors

The model changes whenever the cluster does. Page numbers (offsets) shift whenever a route is added or removed in front of the page. Keyset cursors do not.
- A cursor is opaque base64url. It holds the sort key, the direction and the last row's key tuple: (sort value, namespace, name, group, kind).
- `next` starts strictly after that tuple in the current Model; `prev` ends strictly before the first row's tuple. Both are binary searches over the sorted, filtered rows.
- Ordering is total and deterministic: every sort ends with the tie-break (namespace, name, group, kind). The same Model always gives the same order, and a new Model keeps unchanged rows in the same relative order.
- A row whose sort value changes (for example its health) moves to its new place. A user paging through may see it twice or miss it. That is the cost of a live list, and offsets have it too.
- An anchor row that disappears between requests does not break the cursor: the search finds the position where its tuple would sort.
- The response carries `total` (after filters and the Authorizer), so the UI can say "51–100 of 2,340".
- There is no "jump to page 40". Filters narrow a list faster than paging does. **(our call; §15 Q9)**

Kubernetes `limit`/`continue` pages only by key and expires on compaction, so it is not an option.

### 3.10 Per-request cost

A list request picks the narrowest index (listener, then parent, then namespace, else all rows). It tests each candidate against the remaining filters and the state bit set, applies the Authorizer, sorts if the sort is not the default, and slices one page. Each step is linear or `n log n` on in-memory structs. §9.6 sets the budgets and the benchmarks that prove them.

## 4. Data model

### 4.1 New typed views in `pkg/objects`

All views decode from unstructured objects, apply Gateway API defaults and never fail. A malformed field gives an empty value, as today (`objects.go:116-123`). No `sigs.k8s.io/gateway-api` dependency is added (design §3.2).

| View | Fields |
|---|---|
| `Listener` (spec) | `Name`, `Port` (0 = unset), `Protocol`, `Hostname`, `TLS{Mode, CertificateRefs []Ref}`, `AllowedRoutes{Namespaces{From, Selector}, Kinds []GroupKind}` |
| `ListenerStatus` (extended) | adds `SupportedKinds []GroupKind` to today's name, `attachedRoutes` and conditions (`objects.go:172-187`) |
| `GatewayView` | `Class`, `Listeners []Listener`, `SpecAddresses`, `StatusAddresses []{Type, Value}`, `AttachedListenerSets *int32`, `Conditions`, `ListenerStatus` |
| `ListenerSetView` | `Parent ParentRef` (today's `ListenerSetParent`, `objects.go:219-225`), `Listeners []Listener`, `Conditions`, `ListenerStatus` |
| `HTTPRouteView` | `Hostnames`, `ParentRefs`, `Rules []Rule`, `Parents []RouteParentStatus`, `Labels` (for the delegation label), `InheritParentMatcher bool` |
| `Rule` | `Index`, `Name`, `Matches []Match` (nil when omitted, distinct from an explicit empty list), `Filters []Filter`, `BackendRefs []BackendRef` |
| `Match` | `Path{Type, Value}`, `Headers []{Type, Name, Value}`, `QueryParams []{Type, Name, Value}`, `Method` |
| `Filter` | `Type`, `ExtensionRef *Ref`, `Config map[string]any` (the type's own block, as written) |
| `BackendRef` | `Group`, `Kind` (default `Service`), `Namespace` (default the route's), `Name`, `Port`, `Weight` (default 1), `Filters`, `Delegation *DelegationRef` |
| `DelegationRef` | `Mode` (`name`, `wildcard`, `label`), `Namespace` (default the parent's), `Name` or `Label` |
| `BackendView` | `Type` (`spec.type`), `Conditions` |
| `RouteHostnames` (GRPCRoute, TLSRoute) | `Hostnames`, for view A |

Notes:
- Header and query matches carry their Gateway API defaults (type `Exact`), so the matcher-fit equality of view C compares like with like, as kgateway's `DeepEqual` does on its typed structs.
- `InheritParentMatcher` is true for `true`, `yes` or `enabled`, case-insensitive, read from the child (`v2.4.7 utils/delegation/delegation.go:59-71`, `pkg/krtcollections/policy.go:1429`).
- `TLS.Options` is not decoded. It is free-form and stays in the YAML tab. **(our call)**
- `CertificateRefs` are refs only. They are never resolved, fetched or linked (§11).

### 4.2 Delegation refs, as kgateway reads them

A backendRef is a delegation ref when it names an HTTPRoute (`group: gateway.networking.k8s.io, kind: HTTPRoute`) or a label selector (`group: delegation.kgateway.dev, kind: label`) (`v2.4.7 translator/backendref/types.go:14-28`). Group and kind must be explicit; an omitted kind means Service. The namespace defaults to the parent route's namespace (`pkg/krtcollections/policy.go:1575-1590`). No ReferenceGrant is involved: only the child's parentRefs gate cross-namespace delegation.

| Mode | Spec | Selects (`query/route.go:315-361`; `v2.3.11` `:275`) |
|---|---|---|
| `name` | `name: x` | the HTTPRoute ns/x; missing → unresolved reference |
| `wildcard` | `name: "*"` or empty | every HTTPRoute in ns |
| `label` | `name: v` | HTTPRoutes in ns whose label `delegation.kgateway.dev/label` equals `v` (`api/labels/delegation.go:7`) |
| `label`, namespace = the wildcard setting | `name: v` | that label value in every namespace (§3.6) |

An empty result is an unresolved reference.

### 4.3 Assignment of delegated child routes

Assignment stays **status-based**. Computed views explain and cross-check; they never decide ownership, counts or install health on their own (D-020). **(our call)**

Phase 1 assigns a route to an install only through a parentRef that resolves to a Gateway or ListenerSet of that install (`pkg/install/assign.go:153-189`). So a route reached **only** through HTTPRoute parents, or through none, is not owned, counted or health-checked today. A mixed route, with both a Gateway parentRef and an HTTPRoute parentRef, is owned through its Gateway, but its HTTPRoute-parent status is ignored (`assign.go:158-170`). Phase 2 extends assignment. **(our call)**

**Reported edge.** Route C has a reported edge to HTTPRoute P in install I when all of these hold:
1. C's namespace is in I's scope, and P is owned by I;
2. C has a `status.parents` entry whose `controllerName` ∈ I.controllerNames and whose `parentRef` resolves to P after defaults;
3. **eligibility:** either a current `spec.parentRefs` entry of C equals that status parentRef after defaults (Phase 1's equality rule, design §4.3), or C's `spec.parentRefs` list is **empty**.

The empty-list fallback matches kgateway, which allows implicit attachment only when the whole list is empty (`v2.4.7 utils/delegation/delegation.go:25-31`; `v2.3.11` same file `:25-45`), and whose status builder falls back to report refs only then (`v2.4.7 pkg/reports/status.go:489-496`). A child that moved from P to Q keeps no edge to P: its old P status has no matching spec entry, as Phase 1 ignores removed refs (`pkg/health/checks.go:314`). **(our call)**

**Freshness does not decide the edge.** An eligible status entry keeps the reported edge whether it is current or stale. kgateway stamps route status with the generation of the report it was built from (`v2.4.7 pkg/reports/status.go:475-480`), so right after a child edit its entry describes the previous generation. Ownership and counts stay stable through that window. Freshness then sets the child's state with Phase 1's rules (design §5.2): a stale or `Pending` entry is "being processed" (KGWUI-003 info, check pending, row `unknown`) inside the grace period and "not yet processed" (KGWUI-003 warning) after it. **(our call)**

**Declared candidate.** A current `spec.parentRefs` entry of C resolves to an HTTPRoute P owned by I, C's namespace is in I's scope, and C has **no eligible status entry** for P at all, current or stale. C is a diagnostic candidate only:
- It is not a child. It never appears in Child routes or the up-chain, and never makes a `name` ref "reported".
- It gets KGWUI-011 (§6.2). Its detail includes view B's computed outcome for P, when one exists.
- It is not counted, unless C is owned another way.

**Out-of-scope routes are never candidates.** A route outside I's scope whose parentRef names a route of I gets KGWUI-004 only, as Phase 1's unwatched routes do (`assign.go:181-186`). It never gets KGWUI-011, never enters a computed view as a candidate, and never makes the resource-status check pending, since missing status is expected there.

**Ownership rules.**
- A breadth-first pass starts at the Gateway-attached HTTPRoutes and follows **reported edges only**, over a reverse index built once per recompute. A visited set stops cycles; diamonds give a child two parents (§5.5). Cost is linear in routes plus edges.
- A child is owned once per install, even with several parents. Its `OwnedObject.Parents` adds every parent route with a reported edge. A mixed route keeps its Gateway parents and gains its reported HTTPRoute parents.
- Owned children count in the HTTPRoute tile and in install health. This changes Phase 1's numbers on clusters that use delegation (§15 Q12).
- A route in a namespace outside I's scope whose parentRef names a route of I gets KGWUI-004, as a route to an unwatched Gateway does today.
- A parentRef to an HTTPRoute that does not exist gets KGWUI-003 (missing parent). HTTPRoute joins the "parent kinds" set (`assign.go:146-151`).
- Two installs that share a controller name each count only edges whose parent they own, as in Phase 1 (design §4.3).

**Computed-only children.** A route that view B computes as selected and attached, with no reported edge, is not owned. It is a diagnostic candidate (§7.1), and the mismatch is a cross-check disagreement (§5.4).

**HTTPRoutes with no parent.** An in-scope HTTPRoute with an empty `spec.parentRefs` and no status from I's controller is a would-be child that nothing reports. Serving contract **(our call; §15 Q10)**:
- listed under the Routes filter `no-parent` in every install whose scope contains it;
- not counted, and no status-based finding, because it may be intentional;
- view B may show it as selected and dropped under a parent (§5);
- its row opens the YAML view, which serves it (§7.1). It has a detail page only when an owned parent selects it;
- the Authorizer applies as for any row.

**Stale status after a drop.** On 2.4, a child dropped by the matcher fit has its kgateway status cleared to `parents: []` (`v2.4.7 pkg/krtcollections/policy.go:1150-1178`). Other cases still need checking on a cluster: an implicit child the parent stops selecting, and the same drop on 2.3 (§9.5 task 1).

### 4.4 The `Backend` kind

- **Descriptor:** `gateway.kgateway.dev/Backend`, resource `backends`, version `v1alpha1`, namespaced, a new category `backend`, display name "Backends". It is added to `kinds.Upstream()` (`pkg/kinds/upstream.go:17-34`). RBAC already covers it (`gateway.kgateway.dev: *`, design §8).
- **Assignment:** a Backend belongs to install I when its namespace is in I's scope and a backendRef of a route owned by I resolves to it by full identity (group, kind, namespace defaulting to the route's, name). A Backend nobody references belongs to no install. Its tile counts only referenced Backends, and the tile says "in use". **(our call)**
- **Scope verifier:** the category maps to no `resources_managed` label, so KGWUI-007 ignores it (`pkg/scopeverify/verify.go:141-151`).
- **Resource endpoint:** owned Backends join `lookup` (`pkg/api/handlers.go:255-265`). A Backend referenced by an owned route but outside I's scope is a diagnostic candidate (§7.1).
- **Health:** §6.2.
- This prepares Phase 4, which must work with `Backend` alone (D-016).

### 4.5 ListenerSet kinds stay generic

- ListenerSet v1 and XListenerSet are already registered with category `listenerset` (`pkg/kinds/upstream.go:21-22`). A downstream adds its own kind through `extension.WithKinds` with the same category.
- The detail view and view A read `spec.parentRef`, `spec.listeners[]` and `status.listeners[]` in the Gateway API ListenerSet shape. A downstream kind in this category must follow that shape. The kinds doc states this contract. **(our call)**
- kgateway reconciles ListenerSet v1 and XListenerSet both, and writes the legacy CRD's required `port` itself (`v2.4.7 proxy_syncer/status_syncer.go:664,702`; `v2.3.11` same file `:612,650`).
- URLs and API paths carry the group and kind, so two kinds with one name never collide.

## 5. Delegation (D-020 views B and C)

### 5.1 Three sources, side by side

| Source | Says | Shown as |
|---|---|---|
| Child's `spec.parentRefs` | which parents **may** delegate to it (a permission; any parent when empty) | "Allows: `shop/api`" or "Allows any parent" |
| Controller status | which parents **did** delegate to it (the reported edge, §4.3) | the child's status entry for this parent |
| View B and C (computed) | which routes each ref selects, the outcome per child and its effective matches | labelled "Computed by the UI" with the rule applied |

### 5.2 View B: child selection and outcome

For each owned parent route P reached from a Gateway, the rule set walks P's rules and delegation refs as kgateway does (`v2.4.7 query/route.go:233-292`; `v2.3.11` `:206`), then applies the flatten outcomes (`translator/httproute/gateway_http_route_translator.go:178-225`, `translator/httproute/delegation.go:26-102`; `v2.3.11 delegation.go:28`):

```
for rule in P.rules:
  for ref in rule.backendRefs where ref is delegation:
    selected = fetch(ref)                           # §4.2; empty → "No route selected" (parent BackendNotFound expected)
    for C in selected:
      if C.parentRefs not empty and none names P:   # explicit group+kind, same name, ns default C's (delegation.go:20-56)
        outcome = forbidden; continue               # silently skipped by kgateway, no status
      if C on the current path:                     # path-scoped visited set
        ref fails with cycle; all siblings dropped  # BackendMap.get returns the error (query/backendmap.go:44-54)
      kept = view C per rule of C (§5.3)
      if no rule kept:    outcome = dropped (reason: which match failed)
      elif C.hostnames:   outcome = rejected-hostnames (Accepted=False UnsupportedValue expected)
      else:               outcome = attached
```

- Child hostnames are checked only **after** the matcher fit (`delegation.go:49-91`). So a hostname-bearing child whose rules all fail the fit is `dropped`, not `rejected-hostnames`.
- **Two quirks of kgateway are reproduced as written and flagged as unverified on a cluster** (§9.5 task 1):
  - the child list of a ref is accumulated per rule, so a second delegation ref in one rule also maps to the earlier ref's children;
  - the map is kept per parent route, so the same ref in two rules ends with the later rule's list.
- **Bounds.** kgateway flattens per path, so its work grows with the number of paths. The UI computes per (parent, ref, child) edge once. It walks paths only to detect cycles, with a budget of 10,000 node visits per install per recompute. If the budget runs out, the remaining refs show "Too large to compute" and their cross-check is skipped. **(our call)**
- **Scope.** Candidates outside the install's scope are excluded, as the controller cannot see them. A `name` ref to an out-of-scope route shows "Not watched by the controller".
- **Unknown settings.** A label ref whose namespace may be the wildcard, while the wildcard setting is unknown (§3.6), shows "Unknown". Its cross-check is skipped.

### 5.3 View C: effective matcher

Per kept child rule (`v2.4.7 translator/httproute/delegation_helpers.go:65-97`; `v2.3.11` `:34`):

**Default mode** (`IsDelegatedRouteMatch`, `v2.4.7 utils/delegation/delegation.go:82-133`; `v2.3.11` `:72`). A child match fits the parent match when all of these hold:
- the parent path type is `PathPrefix`;
- the child path value starts with the parent value, as a raw string prefix, so `/api` accepts `/apiv2`. The child's path type is not checked;
- every parent header and query param appears unchanged in the child;
- a parent method is matched by the same child method.

A fitting child match is used unchanged. A rule with omitted `matches` gets the CRD default `PathPrefix /`, which fails under any parent prefix other than `/`. The UI says so: "Dropped: this rule has no matches, so it defaults to `/`, which is outside the parent's `/api`".

**Inherit mode** (`mergeParentChildRouteMatch`, `delegation_helpers.go:117-196`; `v2.3.11` `:118`). The rule is chosen by the child's annotation (§4.1):
- path: `path.Join(parent, child)`, keeping the child's path type, with a nil child path treated as Prefix `""`;
- headers and query params: the union by name, the parent winning on a conflict, sorted by name;
- method: the parent's, if set;
- an explicit `matches: []` inherits the parent match;
- nothing is dropped.

**Shown.** The child page lists, per parent edge, the parent match, the mode, and each rule's effective matches or its drop reason. All of it is labelled "Computed by the UI (inherit-parent-matcher)" or "(default mode)". A multi-level chain composes level by level, using the same bounded walk as the up-chain (§5.5).

### 5.4 Cross-check per child and per parent

Each computed outcome has an expected controller signal. The expectation depends on the rule set (§3.5):

| Computed outcome | Expected on the child | Expected on the parent |
|---|---|---|
| attached | an entry for P, defaults `Accepted=True`, `ResolvedRefs=True`, and on 2.4 `kgateway.dev/Programmed=True` (`pkg/reports/status.go:752-780`). On 2.3, a child whose parentRef omits the namespace or sets `sectionName` or `port` gets **no** entry, because 2.3 synthesises the ref (`v2.3.11 query/route.go:252-259`); the 2.3 rule set expects that. | — |
| rejected-hostnames | `Accepted=False/UnsupportedValue` (`v2.4.7 delegation.go:83-91,104-110`) | counts as no child |
| dropped | no entry from P; on 2.4 stale entries cleared (`pkg/krtcollections/policy.go:1150-1178`) | counts as no child |
| forbidden | no entry from P | counts as no child |
| cycle | — | `ResolvedRefs=False/RefNotPermitted` (`query/utils.go:38-44`) |
| ref has no attached child | — | `ResolvedRefs=False/BackendNotFound` (`delegation.go:98-100`); under STANDARD or STRICT validation the rule becomes a 500 response (`translator/irtranslator/route.go:315-372`) |

A mismatch is shown on the row as "Disagrees with the controller", with both sides. After it persists, it becomes KGWUI-012 (§6.2). The controller's status stays authoritative for ownership and health (§4.3).

### 5.5 Tree, levels and the up-chain

| Parent page shows | Source |
|---|---|
| Each delegation ref, as written (mode, namespace, name or label) | parent spec |
| **Delegation**: per ref, the computed selected children with outcome, reason and the controller's signal, paginated | `GET …/routes/{g}/{k}/{ns}/{name}/delegation` (§7.1) |
| **Child routes**: every child with a reported edge, paginated | `GET …/routes?parent=…` |
| The parent's own conditions per Gateway parent | parent status |

- **N levels.** A child page shows its own delegation refs and children. The tree has no depth limit apart from the compute budget. Each level is one paginated list, fetched when expanded, so a 100-child wildcard does not load the whole subtree.
- **Up the chain.** The route page header shows paths up to a Gateway or ListenerSet: `edge (Gateway) › api (HTTPRoute) › orders (HTTPRoute)`. Repeated diamonds can make the number of paths exponential in the depth, so the walk is bounded **(our call)**:
  - it runs per detail request, over the "parent routes" index (reported edges only), depth-first in a stable order (parents sorted by namespace, name);
  - it stops after 5 complete paths, at depth 32, or after visiting 256 route nodes, whichever comes first;
  - a path that meets a route already on it ends with a `cycle` marker;
  - if any bound stopped the walk, the response says `truncated: true`. It never claims an exact count of the remaining paths.
- **Diamonds.** kgateway tracks visited routes per path, so A→B1→C, A→B2→C is valid and C gets one status entry per parent (`v2.4.7 query/route.go:240-246`; `v2.3.11` `:213-219`).

### 5.6 What is not shown

| Not shown | Why |
|---|---|
| Which of several rules wins for a request, and shadowed matches | The controller's job; upstream ask §14.4 **(user, 2026-10-22)** |
| Ordering of flattened children | Precedence logic (`translator/routeutils/sortable_route.go:22-37,120-171`); 2.3's sort is not a strict weak order (`v2.3.11` `:27-35`), so ties are not predictable anyway |
| Anything that needs the xDS snapshot (what Envoy finally serves) | §3.8 |
| Inherited policies | Phase 5 |

## 6. Health additions

Kept minimal. Phase 1's checks and roll-up (design §5) are unchanged except where this section says so.

### 6.1 Condition state flags on route rows

Each route row carries flags from its **current** status entries for this install's parents, using Phase 1's freshness rules (design §5.2), plus three computed flags. They drive the Routes filter, and create no findings by themselves.

| Flag | Rule |
|---|---|
| `accepted` | some parent: `Accepted=True` |
| `not-accepted` | some parent: `Accepted=False` (reason kept) |
| `refs-unresolved` | some parent: `ResolvedRefs=False` |
| `not-programmed` | some parent: `kgateway.dev/Programmed=False`, or `Accepted=False` with reason `RouteRuleReplaced` or `RouteRuleDropped` |
| `pending` | some parent: no status, `Pending`, or stale |
| `declared` | a parentRef to an owned HTTPRoute with no reported edge (§4.3) |
| `no-parent` | §4.3, HTTPRoutes with no parent |
| `dropped` (computed) | view B: selected by an owned parent and dropped, forbidden or rejected |
| `disagrees` (computed) | a cross-check disagreement involves this route (§3.4, §5.4) |
| `listener-unknown` (computed) | view A could not place an accepted parentRef on a listener |

- `kgateway.dev/Programmed` exists on 2.4 (`v2.4.7 api/conditions/conditions.go:7`) and not on 2.3. On 2.3 the replaced and dropped reasons arrive through `Accepted=False` (`v2.3.11 translator/irtranslator/route.go:294-312`; 2.4 `v2.4.7` same file `:328-350`).
- Reasons are shown as raw strings and never mapped to a fixed enum, because reason sets differ across specs and versions (research, gateway-api#4760).

### 6.2 Findings

| Code | Slug | Severity | Change |
|---|---|---|---|
| KGWUI-002 | condition-false | error (unchanged) | Now also covers the status entries of reported edges (§4.3) and Backend `Accepted` (below). |
| KGWUI-003 | route-unattached | info / warning (unchanged) | Also fires for a parentRef to an HTTPRoute that does not exist. |
| KGWUI-004 | namespace-not-watched | warning (unchanged) | Also fires for a route in an unwatched namespace that names an owned route. |
| **KGWUI-011** | **delegated-route-not-attached** | info inside the grace period, warning after | **New.** An in-scope route the parent should delegate to is not attached. Two triggers, below. |
| **KGWUI-012** | **computed-view-disagrees** | warning | **New.** A computed view disagrees with what the controller reports, persistently. **(our call; §15 Q13)** |

**KGWUI-011.**
- **Status trigger:** a declared candidate (§4.3). It names an owned HTTPRoute and has no eligible status entry for it, current or stale.
- **Computed trigger:** view B computes an in-scope route as selected by an owned parent's `name` or `label` ref, and the outcome is `dropped` or `forbidden`.
- A `wildcard` ref selects every route in a namespace, so its dropped routes may be unrelated. They show in the Delegation table and get no finding. **(our call; §15 Q15)**
- `rejected-hostnames` already has KGWUI-002 from status.
- Message draft: "Parent route does not use this route".
- Detail draft: "Computed by the UI: no rule fits the parent's match `/api` (PathPrefix). No status from HTTPRoute `shop/api`." Without a computed outcome: "No status from HTTPRoute `shop/api`."
- The grace period runs from the later of the route's and the parent's current generation, since editing either changes the outcome. **(our call)**
- An eligible but stale entry is not KGWUI-011. It keeps the edge and follows Phase 1's freshness rules (KGWUI-003, §4.3).
- A route outside the install's scope never gets KGWUI-011. It gets KGWUI-004 only, and it never makes the resource-status check pending.

**KGWUI-012.**
- **Resource:** the object whose status disagrees. That is the Gateway or ListenerSet for a listener count, the child route for a child outcome, and the parent route for an expected `ResolvedRefs`.
- **Persistence:** both conditions must hold **(our call; §15 Q13)**:
  - the disagreement held on every recompute for 30 s since the last change to any input of that check;
  - the disagreeing object's status is current (`observedGeneration` = `generation`).
  Status writes lag translation, and `attachedRoutes` is not tied to any route's generation, so a check right after a change can disagree only because the status has not landed yet.
- **Message draft:** "The UI's computed view disagrees with the controller".
- **Detail draft:** "Listener `https`: computed 12 routes, controller reports 11. Rule set kgateway-2.4."
- **Roll-up:** a warning. It can turn the install yellow, never red. The controller's status stays authoritative.
- A check whose inputs are unknown (§3.4, §3.6) or out of budget (§5.2) is skipped, never reported as a disagreement.

**Backend `Accepted`.** kgateway 2.4 stamps `observedGeneration` on the condition (`v2.4.7 pkg/reports/status.go:410-415`); kgateway 2.3 does not (`v2.3.11 extensions2/pluginutils/status.go:10-40`, written by `extensions2/plugins/backend/status.go:51-71`). The rule reads the condition's own evidence, not the controller version **(our call; §15 Q11)**:

| Condition | Evidence | Finding | Backend state |
|---|---|---|---|
| `observedGeneration` ≥ the Backend's `generation` | current | `False` → KGWUI-002 error; `True` → none | from the finding; `True` → healthy |
| `observedGeneration` > 0 and lower | stale | none (Phase 1: a stale False is not a current error) | unknown |
| `observedGeneration` 0 (2.3) | none | `False` → KGWUI-002 **warning**, detail "The controller does not say which version of this Backend it checked" | unknown |
| absent | none | none (the condition is optional) | unknown |

- No-evidence states are never green (D-016 §6, design §5.2). They do not add a failed required check, so they do not turn the whole install unknown. Otherwise every 2.3 install with Backends would stay unknown forever, the outcome D-018 rejected.
- A stale or absent Backend condition gives no "not yet processed" finding. Backend status is optional, so the Phase 1 pending helper (`pkg/health/checks.go:228-255`) is not used for it.

**Experimental hint.** XListenerSet pages and rows reuse D-018's hint when the object has no status: "The controller's experimental Gateway API features may be off."

### 6.3 Row state

Every list row and detail header shows a health **state** (`error`, `unknown`, `warning`, `healthy`), never a severity. It uses Phase 1's Gateway row rules (`pkg/overview/overview.go:166-187`), applied to the row's own object ref **(our call)**:
1. any error finding on the object → `error`;
2. else any required check of the install not OK (signal missing, or resources in grace) → `unknown`;
3. else any warning finding on the object → `warning`;
4. else → `healthy`.

Two refinements:
- A Backend with no current evidence is `unknown` (§6.2).
- Info findings never set a state. Stale data turns every state `unknown` on the client (design §5.4).

**What propagates.**
- A delegated child's failures count in **install** health (the roll-up covers every finding, design §5.3) and on the **child's own row**.
- A Gateway, ListenerSet or parent route badge keeps Phase 1 semantics: only findings on that object. A Gateway with broken routes is not shown red for them. A KGWUI-012 on a Gateway makes its badge `warning`. **(our call)**
- Clusters that use delegation may change colour after upgrade, because children are now judged. The release notes say so.

## 7. API v1 additions

All additions are new paths or new optional fields. No existing path, body or install ID changes (D-016 H11).

### 7.1 Endpoints

| Endpoint | Returns |
|---|---|
| `GET /api/v1/installs/{id}/routes` | `RouteList` (§7.3) |
| `GET /api/v1/installs/{id}/gateways` | `GatewayList` |
| `GET /api/v1/installs/{id}/listenersets` | `ListenerSetList` |
| `GET /api/v1/installs/{id}/gateways/{ns}/{name}` | `GatewayDetail` |
| `GET /api/v1/installs/{id}/listenersets/{group}/{kind}/{ns}/{name}` | `ListenerSetDetail` |
| `GET /api/v1/installs/{id}/routes/{group}/{kind}/{ns}/{name}` | `RouteDetail`. Phase 2 serves HTTPRoute only; other route kinds return 404 with "no detail view for this kind". |
| `GET /api/v1/installs/{id}/routes/{group}/{kind}/{ns}/{name}/delegation` | `Page<DelegationRow>`: view B for this parent, filterable by `rule`, `ref` and `outcome` |

- Detail paths carry group and kind where kinds can share a name. Gateway has one kind, so its path is shorter. **(our call)**
- The ListenerSets list is not in the roadmap text. It is added because large installs carry hundreds of ListenerSets. **(our call)**
- `/api/v1/meta` gains an optional `views` field: the group/kinds that have a detail page (`Gateway`, every `listenerset` kind, `HTTPRoute`). It is informational. The SPA does not derive links from it. The rule set in use is not in `/meta`; each detail payload carries it as `ruleSet` (§7.3).

**Admission.** Finding links and spec links must not end in a 404 **(our call)**:

| Object | Detail endpoint | Resource (YAML) endpoint |
|---|---|---|
| Owned by the install | yes | yes (today) |
| Referenced by a current finding of the install | yes, as a diagnostic candidate | yes (today, `pkg/api/handlers.go:266-285`) |
| Named by a `parentRef` or `backendRef` of an owned object, by full identity, of a served kind | yes, as a diagnostic candidate | yes (**new**) |
| Selected by an owned parent's delegation ref in view B | yes, as a diagnostic candidate | yes (**new**) |
| A `no-parent` row of the install (§4.3) | only if the previous row applies; otherwise its row opens the YAML view | yes (**new**) |
| Anything else | 404 | 404 |

- A diagnostic candidate's detail says so in a banner: "Not part of this install. Shown because …" with the reason (a finding, the owned object that names it, or the parent that selects it). It shows its status as written, with entries from this install's controller first.
- Examples: an unwatched route (KGWUI-004), a route whose parent is missing (KGWUI-003), a declared candidate (KGWUI-011), a dropped child, and a Backend outside the scope that an owned route names.
- A ListenerSet whose parent Gateway is missing is never assigned (`pkg/install/assign.go:136-139`), and nothing owned names it. It stays out of the install, as in Phase 1. A route naming it gets KGWUI-003, which makes the route servable.
- The resource endpoint change is additive: it serves a superset of what it serves today, and every denial is still a 404 (D-013).

**Link metadata.** One function, `admit(install, caller, ref) → detail | yaml | none`, implements the admission table and the Authorizer. The detail and resource endpoints call it to answer, and every payload calls it to label each reference it renders **(our call)**:
- Every reference in a list or detail body is a `LinkedRef`: the ref plus its `link`. This covers row refs, parents, `via`, parent routes, ancestor steps, backendRefs, delegation targets and selected children, ExtensionRef filters, the ListenerSet's parent Gateway and the candidate's `by`.
- `detail`: the object is admitted and its kind has a detail view. `yaml`: admitted, no detail view (or a `no-parent` row). `none`: not admitted, denied, absent, or never served (Secret certificateRefs, Services). Denied and absent both give `none`, so the label reveals nothing extra (D-013).
- Admission does not follow chains. A candidate C is admitted through its finding, but a route D that only C names is not. D's ref on C's page is `none` and renders as text with "Not part of this install". A link that the endpoint would reject is never rendered.
- Phase 1 findings gain an optional `link` on their resource, from the same function, so finding rows need no kind lookup either. The field is additive.
- The SPA never derives a link from a kind. It renders `link` as given.

### 7.2 Query parameters

| Param | Lists | Values |
|---|---|---|
| `namespace` | all | exact namespace; repeatable (OR) |
| `kind` | routes | `group/Kind`; repeatable |
| `parent` | routes, listenersets | `group/Kind/namespace/name`. Routes: a Gateway, ListenerSet or HTTPRoute; a Gateway includes routes attached through its ListenerSets. ListenerSets: a Gateway. |
| `listener` | routes | listener name; needs a Gateway or ListenerSet `parent`. Matches view A's computed members of that listener, plus `unknown` for "Listener unknown". |
| `attachment` | routes | with `parent`: `accepted`, `not-accepted`, `pending`, `unwatched`, and for an HTTPRoute parent `declared` |
| `state` | routes | a §6.1 flag; repeatable (OR) |
| `health` | all | `error`, `unknown`, `warning`, `healthy`; repeatable |
| `q` | all | case-insensitive substring of namespace/name; for routes also hostnames |
| `sort` | all | `name` (default: namespace, then name), `kind`, `health`, and for Gateways `routes`; suffix `.asc` or `.desc` |
| `limit` | all | 1–200, default 50 |
| `cursor` | all | an opaque `next` or `prev` value from a previous response |
| `rule`, `ref`, `outcome` | delegation | rule index, ref index in the rule, and a §5.2 outcome |

**Errors.** Every error uses Phase 1's `ErrorResponse` body (`pkg/api/types.go:64-67`).
- 400: unknown param value, `limit` out of range, `listener` without a Gateway or ListenerSet `parent`, a malformed cursor, or a cursor from a different sort. The SPA drops the cursor and reloads page one.
- 404: unknown install, or a detail object that is absent, not admitted (§7.1) or denied. Denied and absent look the same (D-013).
- 503: cache not synced, as today (`handlers.go:69-77`).

### 7.3 DTOs

Field names are the wire names. `?` marks optional fields. Every time is UTC (D-017).

```jsonc
// Shared
Link            "detail" | "yaml" | "none"                           // from admit(), §7.1
LinkedRef       { group, kind, namespace?, name, link: Link }        // every rendered reference
Computed        { by: "ui", rules: string, rule: string,             // rules: "kgateway-2.4"; rule: the rule applied
                  check: "agrees"|"disagrees"|"pending"|"skipped", note? }
Condition       { type, status, reason, message, observedGeneration, lastTransitionTime, stale }
ParentStatus    { parentRef: ParentRef, target: LinkedRef, controllerName, ours, conditions: Condition[] }
                // ours: from this install's controller
ParentSummary   { ref: LinkedRef, sectionName?, port?,
                  via?: LinkedRef, viaRestricted?: true,             // via: the ListenerSet a Gateway was reached through
                  attachment: "accepted"|"not-accepted"|"pending"|"unwatched"|"declared",
                  conditionType?, reason?, message?,                 // the deciding condition for this parent
                  listeners?: { names: string[], effectiveHostnames: string[], computed: Computed } | { unknown: string } }
Page<T>         { install, items: T[], total, next?, prev?, deniedInInstall?, collectedAt }
                // deniedInInstall: objects of the listed kind(s) in the whole install that the caller
                // may not view; independent of every filter; absent under AllowAll

// Lists
RouteRow        { ref: LinkedRef, hostnames: string[], parents: ParentSummary[], parentRoutes: LinkedRef[],
                  children: number, flags: string[], health: State }
GatewayRow2     { ref: LinkedRef, class, health, listeners, attachedRoutes /*status sum*/, routes /*accepted, UI*/,
                  notAccepted, listenerSets, disagreements, addresses: string[] }
ListenerSetRow  { ref: LinkedRef, parent: LinkedRef, health, listeners, acceptedReason?, routes, disagreements }
DelegationRow   { rule, ref: number, refAsWritten: { mode, namespace, name?, label? }, child: LinkedRef,
                  allows: "any"|"this"|"other",                      // the child's parentRef permission
                  outcome: "attached"|"dropped"|"forbidden"|"rejected-hostnames"|"cycle"|"unknown",
                  reason?, computed: Computed,
                  observed: { entry?: ParentStatus, parentCondition?: Condition } }
RouteList = Page<RouteRow>; GatewayList = Page<GatewayRow2>; ListenerSetList = Page<ListenerSetRow>

// Details
ListenerDTO     { name, port?, protocol, hostname?, tls?: { mode, certificateRefs: LinkedRef[] /*link always none*/ },
                  allowedRoutes: { namespaces: { from, selector? }, kinds: GroupKind[] },
                  status?: { attachedRoutes, supportedKinds: GroupKind[], conditions: Condition[] },
                  computedRoutes?: number, computed?: Computed, unknown?: string,
                  isolationNote?: true }                             // 2.4 listener isolation, §3.4
GatewayDetail   { install, ref, generation, class, health, candidate?, ruleSet, conditions: Condition[],
                  addresses: {type?, value}[], listeners: ListenerDTO[], attachedListenerSets?,
                  routeCounts: { accepted, notAccepted, pending, unwatched }, collectedAt }
                // ListenerSets come from GET …/listenersets?parent=… (paged), not from this body
ListenerSetDetail { install, ref, generation, health, candidate?, ruleSet, parent: LinkedRef, parentFound, conditions,
                  listeners: ListenerDTO[], experimentalHint?, routeCounts, collectedAt }
RouteDetail     { install, ref, generation, health, candidate?, ruleSet, hostnames: string[],
                  annotations: { inheritParentMatcher? }, parentRefs: ParentRef[], parents: ParentStatus[],
                  rules: RuleDTO[], effective?: EffectiveDTO[], effectiveTruncated?: boolean,
                  ancestors: AncestorPath[], ancestorsTruncated: boolean,
                  childCount, declaredCount, collectedAt }
EffectiveDTO    { parent: LinkedRef, parentMatch: MatchDTO, mode: "default"|"inherit",
                  rules: { index, kept: boolean, matches: MatchDTO[], reason? }[], computed: Computed }
AncestorPath    { steps: AncestorStep[], cycle: boolean }
AncestorStep    { ref: LinkedRef } | { restricted: true }            // a restricted step carries no identity
RuleDTO         { index, name?, matches: MatchDTO[], filters: FilterDTO[], backendRefs: BackendRefDTO[] }
FilterDTO       { type, extensionRef?: LinkedRef, config? }
BackendRefDTO   { ref: LinkedRef, port?, weight, filters: FilterDTO[],
                  delegation?: { mode, namespace, name?, label?,
                                 state?: "reported"|"not-reported"|"not-found"|"restricted",
                                 selected?: number, attached?: number, computed?: Computed },
                  backend?: { state: State, accepted?: Condition } | { restricted: true } }  // kind Backend only
// candidate: { reason: "finding"|"referenced"|"selected", by?: LinkedRef } — a diagnostic candidate (§7.1)
// ruleSet (on details): the rule set used for this install, e.g. "kgateway-2.4", with a note when unverified
// Finding.resource gains an optional link: Link (additive, §7.1)
```

- `GatewayRow2` is a new type. Phase 1's overview `GatewayRow` (`pkg/overview/overview.go:73-80`) keeps every field and gains one optional field, `link: Link`, computed by the same admission function (§7.1). The overview Gateway table can then link to the detail page without a finding. **(our call)** The final Go name is the plan's choice.
- `delegation.state` is set only for `name` refs and is status-based: `reported` needs a reported edge (§4.3). `selected` and `attached` are the computed counts.
- `effective` lists at most 20 parent edges; more sets `effectiveTruncated`. **(our call)**
- A route page lists reported children through `GET …/routes?parent=gateway.networking.k8s.io/HTTPRoute/{ns}/{name}`, declared candidates with `&attachment=declared`, and computed selection through `…/delegation`.
- The Gateway page pages its ListenerSets through `GET …/listenersets?parent=…`, so no detail body is unbounded. **(our call)**
- `ParentSummary` carries the deciding condition per parent: the first current `False` among `Accepted`, `ResolvedRefs`, `kgateway.dev/Programmed`, else the pending reason ("no status", `Pending`, "stale"). Failed-route tables render without a detail request.

### 7.4 Authorizer

Every new handler goes through the `Authorizer` (`pkg/api/authz.go:12-15`). The rules, all **(our call)**:
- **Detail:** a denied object returns 404, as does an absent one.
- **Rows:** list rows are filtered per ref **before** paging. `total` counts only what the caller sees.
- **No filtered hidden counts.** A count of denied rows that match a filter would let a caller probe hostnames or states of objects it cannot view. Lists report at most `deniedInInstall`: the unfiltered number of objects of that kind in the install the caller may not view. That matches what Phase 1's count tiles already imply. The UI shows it once per list page, never per nested table.
- **Every dereferenced object is authorized.** Data that comes from another object is sent only if the caller may view that object:
  - a Backend's state and `Accepted` condition (`{restricted: true}` otherwise);
  - a `name` delegation ref's state (`restricted` otherwise);
  - a delegation row's child and its status (the row is dropped when the child is denied);
  - `via` (the ListenerSet a Gateway was reached through; `viaRestricted` otherwise);
  - an up-chain step (`{restricted: true}`, with no kind, namespace or name);
  - computed explanations that quote another object, such as namespace labels in a `NotAllowedByListeners` explanation, omit what came from an object the caller may not view;
  - derived counts: `routes`, `notAccepted`, `listenerSets`, `routeCounts`, `children`, `childCount`, `declaredCount`, `computedRoutes`, `selected` and `attached` count only visible objects. Under a non-`AllowAll` Authorizer they are recounted per request, and the listener cross-check is shown only to callers who see every member of that listener (otherwise "Cross-check hidden"). Health keeps the full result.
- **Spec refs stay.** References written in an authorized object's own spec or status (parentRefs, backendRefs, certificateRefs, status parentRefs) are part of that object. They are sent as written, as the YAML view sends them today. Status-written numbers (`attachedRoutes`, `attachedListenerSets`) are sent as written.
- **Health is not filtered.** Install health and every row's state still cover all findings (design §7.1). A row's state uses only findings on that row's own object (§6.3).
- `AllowAll` skips per-row calls and uses the precomputed counts (`authz.go:30-37`).

### 7.5 Example

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
                  "listeners": {"names": [], "effectiveHostnames": [],
                                "computed": {"by": "ui", "rules": "kgateway-2.4", "rule": "hostname-intersection",
                                             "check": "agrees", "note": "listener https has *.example.org; route has shop.example.com"}}}],
     "parentRoutes": [], "children": 0, "flags": ["not-accepted"], "health": "error"}
  ],
  "total": 1,
  "collectedAt": "2026-10-22T09:12:03Z"
}
```

kgateway writes no message on attachment failures (`translator/gateway/gateway_translator.go:80-87`), which is why the computed note matters.

### 7.6 Goldens

New golden fixtures in `pkg/api/testdata/golden`: `routes.json`, `routes-filtered.json`, `routes-by-listener.json`, `gateways.json`, `listenersets.json`, `gateway-detail.json` (one listener agreeing, one disagreeing, one unknown), `listenerset-detail.json`, `route-detail.json` (with delegation, effective matchers in both modes, a cycle, a truncated up-chain and a Backend ref), `route-delegation.json` (every outcome), `route-detail-candidate.json`, `bad-request.json` and a restricted variant (restricted Backend, ancestor step and `via`, plus `deniedInInstall`).
- The TypeScript golden test (`web/packages/shell/src/api/types.golden.test.ts`) imports each one.
- Existing goldens must not change, except additive ones:
  - `meta.json` gains the optional `views` field and the Backend kind;
  - `health.json` gains the optional `link` on finding resources;
  - `overview.json` and `bare-overview.json` gain the optional `link` on Gateway rows.

## 8. Frontend

### 8.1 Pages and URLs

| URL | Page |
|---|---|
| `/i/:id/gateways` | Gateways list |
| `/i/:id/gateways/:ns/:name` | Gateway detail (`?listener=` selects a listener) |
| `/i/:id/listenersets` | ListenerSets list |
| `/i/:id/listenersets/:group/:kind/:ns/:name` | ListenerSet detail |
| `/i/:id/routes` | Routes list |
| `/i/:id/routes/:group/:kind/:ns/:name` | HTTPRoute detail |
| `/i/:id/r/:group/:kind/:namespace/:name` | YAML view (unchanged; still served for every linkable kind) |

- Detail pages have two tabs, **Overview** and **YAML**, held in `?tab=yaml`. The YAML tab reuses the YAML view's body (`web/packages/shell/src/pages/ResourcePage.tsx:135-192`) and its polling and `Collected-At` handling.
- The header gains install-scoped nav entries: Overview, Gateways, Routes, ListenerSets, Findings.
- List filters, sort and cursor live in the query string, like the Gateway table today (`pages/install/GatewayTable.tsx:221-260`).
- Every link comes from the reference's server-computed `link` (§7.1): `detail` opens the detail page, `yaml` the YAML view, and `none` renders plain text. Routes of other kinds and `no-parent` rows arrive as `yaml`. The SPA never picks a target from the kind.

### 8.2 Extension contract

- `CorePages` (`shell/extension.ts:49-54`) gains **optional** slots: `GatewaysPage`, `GatewayPage`, `ListenerSetsPage`, `ListenerSetPage`, `RoutesPage`, `RoutePage`. A missing slot falls back to the upstream page, so a downstream app that passes today's four still compiles and gets the new pages.
- The core reserves the paths `gateways`, `listenersets` and `routes` under `/i/:id/`. The doc on `RouteContribution` (`extension.ts:14-22`) lists them, and a test proves a contribution cannot replace them.
- New exports: `objectPath(installId, linkedRef)` (the path for its `link`, or `undefined` for `none`), a `ComputedBadge` component, the list hooks and DTO types. `resourcePath` stays and keeps meaning the YAML view (`pages/install/FindingRow.tsx:76-79`).
- `Vocabulary` gains `getCommand(ref, resource)`, returning the copy text or `undefined` to hide the button. The default is `kubectl get <resource>.<group> -n <ns> <name> -o yaml`; the plural and group come from `/meta`, so two CRDs with one kind name never clash. A platform without kubectl overrides or hides it (D-016 H4).
- `SHELL_API_VERSION` stays **1**. Every change is additive, and no release has frozen the contract yet (phase-1-complete.md). **(our call)**

### 8.3 Finding links retarget

`FindingRow` (`FindingRow.tsx:34,42`) and the top findings use `objectPath` on the finding's server-computed `link`; the Gateway table row uses the row's own `link` (§7.1, §7.3). A Gateway, ListenerSet or HTTPRoute finding gets `detail`; anything else gets `yaml`, as before. The link comes from the same admission function the endpoints use, so it never 404s. A restricted finding still never links.

### 8.4 Computed facts on screen

- Every computed fact carries a `ComputedBadge`: a small outlined "Computed" chip with an icon. Its tooltip names the rule and rule set: "Computed by the UI from kgateway 2.4's attachment rules: hostname intersection".
- A cross-check result sits next to it. "Agrees" uses the healthy icon. "Disagrees with the controller" uses the warning icon and shows both values. "Not checked" says why.
- Controller-reported facts carry no badge, so the default reading is "this is what kgateway says".
- Colour is never the only signal. The chip has text, and the state has icon + text (roadmap UX §4).

### 8.5 Page content

All detail pages: a diagnostic candidate shows its banner (§7.1) above the header.

**Gateway detail.** Header: kind, name, health badge, data age, copy buttons. Then:
1. Conditions: one compact row per condition, with status icon + text, reason, message and freshness.
2. Listeners: name, port, protocol, hostname (or "Any hostname"), TLS mode, certificate names (mono, not links), allowed namespaces and kinds, controller `attachedRoutes` and computed count with the cross-check, conditions on expand. Selecting a row sets `?listener=`.
3. ListenerSets (paged, from the ListenerSets list): rows with health and accepted reason; "Attached ListenerSets (status): N" next to it.
4. Attached routes (paged), filtered by the selected listener, with effective hostnames (computed).
5. Routes that failed to attach (paged): the reason, the message and the computed explanation per row. Sub-filters: Not accepted, Waiting for status, Unwatched namespace.

**HTTPRoute detail.**
1. Up-chain breadcrumb (§5.5). A restricted step reads "A parent you cannot view". A truncated walk ends with "More paths not shown".
2. Hostnames, or "Inherits the listener's hostnames", or for a child "Inherits the parent route's hostnames". Effective hostnames per listener (computed).
3. Parents: one card per parentRef. It shows the target (linked per its `link`), section or port, the computed listeners, and every condition. Entries from other controllers are collapsed under "Status from other controllers".
4. Permission: "Allows any parent" or "Allows: …" from the child's parentRefs (§5.1).
5. Rules: one block per rule. It shows matches (path type and value, headers, query params, method), filters (type plus the config as written; ExtensionRef linked per its `link`) and backendRefs (kind, namespace/name, port, weight). A delegation ref shows its mode and its computed selected and attached counts. A Backend shows its state and `Accepted` condition, or "Restricted" when the caller may not view it.
6. Effective matches (computed, view C), per parent edge, for a child.
7. Delegation (computed, view B, paged), when the route has delegation refs.
8. Child routes (reported, paged).

**ListenerSet detail.** Parent Gateway link (or "Parent Gateway not found"), conditions, a listener table like the Gateway's with the cross-check, and attached routes.

### 8.6 States

| State | Behaviour |
|---|---|
| Loading | Skeleton rows at the final row height, and fixed-height header and tabs, so nothing shifts when data lands |
| Empty | Designed per table: "No routes are attached to this Gateway." "No routes match these filters." with a Clear filters action |
| Error after data | Keep the data, show `PageNotice` above it (`ResourcePage.tsx:174` pattern) |
| 404 | "Not in this install's scope", with a link back (Phase 1 copy) |
| 400 cursor | Drop the cursor silently and load page one |
| Stale | `DataAge` turns stale; every health badge on the page shows unknown (design §5.4) |
| Computed view unknown | The listener or delegation row says what input is missing; no number is shown as if it were known |
| Denied objects | Once per list page, from `deniedInInstall`: "N routes in this install are hidden from you". Never per filter or per nested table (§7.4). |
| Install switch | Render data only when the response's `install` matches the URL (design §6.6) |

### 8.7 Interaction, layout and accessibility

- Every link, button, tab, chip, row and menu item has hover, active and focus-visible styles, in both themes.
- Tables keep fixed column widths from the first render. Long names wrap inside their cell (`overflow-wrap: anywhere`).
- Table rows are keyboard-navigable, as in the Gateway table. Sortable headers carry `aria-sort`.
- Tabs follow the WAI-ARIA tabs pattern. Expanders carry `aria-expanded`. The up-chain is a `nav` with `aria-label="Route parents"`.
- The `ComputedBadge` tooltip is reachable by keyboard, and its text is also in the accessible name.
- Condition state is always icon + colour + text. Contrast is checked in both themes by the existing token test.
- Below 900 px, detail sections stack and tables drop secondary columns into an expandable row. No horizontal page scroll at 720 px.
- Polling every 5 s never resets scroll, focus or expanded state.

### 8.8 Wording pass

All new copy goes through the wording pass: short sentences, plain English, active voice. Per string, a sub-agent picks the least machine-sounding of five accurate options. Strings in scope:
- page and section titles, empty states, and filter and flag labels;
- attachment labels and the delegation outcomes and reasons of §5;
- the computed badge and its tooltip, the cross-check states and the "Listener unknown" reasons;
- the KGWUI-011 and KGWUI-012 messages and details;
- the Backend freshness detail, the candidate banner and the restricted placeholders;
- the denied-objects line and the copy-button labels.

Drafts in this document are placeholders.

## 9. Testing

The primary boundary stays **cluster objects in → API JSON out** (design §9). Every claimed behaviour gets a test at that boundary.

### 9.1 API boundary tests (Go)

Seed the fake dynamic client, run the real cache, install, computed, health, explore and API packages, and call the handlers.
- **Lists:** each filter alone and combined. Sort by each key both ways. Malformed cursor, cursor from another sort, `limit=0` and `limit=201` all return 400. `listener` without a Gateway or ListenerSet `parent` returns 400.
- **Cursors across recomputes:** next and prev across a page boundary; a route inserted before the anchor; the anchor row deleted; many rows tied on the sort value in descending order; paging backwards from the last page to the first and getting the same rows as forwards; a row whose sort value changes between recomputes (it moves; no row is skipped that kept its value).
- **View A, per rule:** each protocol's default kinds; `allowedRoutes.kinds` intersection, with a nil group; `Same` for a Gateway and for a ListenerSet listener; `All`; `Selector` matching, not matching, and with the Namespace missing; a nil selector (listener error); `port` and `sectionName`; each hostname-intersection case (no route hostnames, no listener hostname, exact/exact, wildcard listener, wildcard route, both wildcards, duplicates); a route with two parentRefs matching one listener (counted twice); a Gateway parentRef never reaching ListenerSet listeners; the `NoMatchingParent` quirk; a denied ListenerSet's routes not evaluated.
- **View A cross-check:** agree; a fabricated `attachedRoutes` off by one (disagrees, KGWUI-012 only after 30 s and with current status); a disagreement that clears before 30 s (no finding); Namespace access unavailable (unknown, skipped); two parentRefs differing only by port (compared as one).
- **Attachment:** a route attached through a ListenerSet shows `via`. A route with `Accepted=False` and each of the three reasons, with the computed explanation in its `ParentSummary`. A pending route. An unwatched route in `attachment=unwatched`.
- **Stale status:** a stale `Accepted=True` is not `accepted`, and a stale `False` is not `not-accepted`.
- **View B, per outcome:** name, wildcard and label refs; a label ref with the wildcard namespace, with the setting default, set, and unknown (`valueFrom`); permitted with empty parentRefs, permitted by name, forbidden (nil group or kind in the child's parentRef also forbids); dropped; rejected hostnames; a hostname-bearing child that fails the fit (dropped, not rejected); a cycle failing the whole ref and its siblings; the two flagged quirks; an out-of-scope `name` target; the visit budget running out ("Too large to compute").
- **View B cross-check, per rule set:** attached with the expected entry (2.4 with `Programmed`, 2.3 without); a 2.3 child with a namespace-less parentRef (no entry expected, agrees); dropped with no entry (agrees); computed attached but no entry on 2.4 (disagrees); parent `BackendNotFound` expected and present, and expected but absent.
- **View C:** default-mode fit for each clause (prefix type, raw prefix `/apiv2`, child path type ignored, headers, query params, method); omitted `matches` dropped under `/api`; explicit `matches: []` under inherit; inherit merge of path (`path.Join` cleaning, child type kept, nil child path), headers and query params (parent wins, sorted), and method.
- **Reported vs declared:** a child with a matching spec parentRef and status is a child. A child with a spec parentRef and no status is a declared candidate: not in Child routes, `name` ref `not-reported`, KGWUI-011 with the computed reason in its detail, not counted. A parent with no delegation refs at all, named by a child's parentRef, gets no children.
- **Stale matching entry:** an explicit child whose matching status entry is stale (its generation bumped) stays a child. It stays owned and counted, with no KGWUI-011. Inside the grace period it gets KGWUI-003 info, the check is pending, and its row is `unknown`; after the grace period it gets KGWUI-003 warning. The counts are the same before and after the edit.
- **Unwatched declared route:** a route outside the scope whose parentRef names an owned HTTPRoute gets exactly one finding, KGWUI-004. It gets no KGWUI-011, no computed outcome, the resource-status check stays OK, and install health is unchanged.
- **Implicit fallback:** a child with empty parentRefs and status is a child. A child that moves its parentRef from P to Q while old P status remains has no edge to P, and P's page does not list it. A child that gains a parentRef to a Gateway loses its implicit edge.
- **KGWUI-011 computed trigger:** a route selected by a `name` or `label` ref and dropped gets it; the same route selected only by a `wildcard` ref does not.
- **Delegation shapes:** a three-level chain, a diamond (one child, two paths, two status entries), a repeated-diamond lattice (the up-chain stops at 5 paths with `ancestorsTruncated`), and a parent with refs and no reported child.
- **Other delegation findings:** a child with hostnames and `UnsupportedValue` gets KGWUI-002. A parentRef to a missing HTTPRoute gets KGWUI-003. An HTTPRoute with no parent lands in `state=no-parent`, uncounted, with no status finding, and its YAML view resolves.
- **Phase 1 → Phase 2 behaviour change (Q12).** Each case asserts counts, findings and roll-up together, before and after the change: a mixed-role route (Gateway and HTTPRoute parents) owned once, now judged on both; a child reported by two parents counted once; two installs that share a controller name, each owning only its own edges; a removed parentRef; a namespace that leaves the scope; a stale implicit edge; a computed-only child (not owned, KGWUI-012).
- **Isolation:** a child whose status comes from another install's controller is not owned here. A child reported by parents in two installs appears in both.
- **Backend:** a referenced Backend is owned and counted; an unreferenced one is not. Each row of the §6.2 table: current `False` is an error; current `True` is healthy; stale is unknown with no finding; `observedGeneration` 0 with `False` is a warning; with `True` it is unknown, never healthy; absent is unknown. None of them adds "not yet processed" or turns the install unknown.
- **Row state:** an object with a warning finding while a required check is pending is `unknown`; an info-only object is `healthy`; a Gateway whose routes fail keeps its own state; a Gateway with KGWUI-012 is `warning`.
- **ListenerSet:** `Accepted=True/ListenersNotValid` with one failed listener; `Accepted=False` with no listener entries; an XListenerSet without status shows the experimental hint; a ListenerSet whose parent Gateway is missing stays out of the install. The Gateway page pages 200 ListenerSets.
- **Admission:** every finding resource opens its detail (or YAML) view; an object named by an owned route's parentRef or backendRef, or selected by an owned parent, is served as a candidate; an unrelated object returns 404 from both endpoints.
- **Link metadata:** for every reference in every golden payload, `link` agrees with the endpoint. `detail` → the detail endpoint returns 200; `yaml` → the resource endpoint returns 200; `none` → both return 404. One test walks every rendered link of a fixture world.
- **Second navigation step:** candidate C (admitted through KGWUI-011) names route D through a delegation backendRef, and nothing else admits D. On C's page, D's ref has `link: none` and both endpoints return 404 for D. When D is owned, the same ref has `link: detail` and resolves.
- **Authorizer:** a deny-some authorizer hides list rows, nested ListenerSets, delegation rows and children. `total` counts only visible rows. `deniedInInstall` stays the same under every filter (a hostname probe changes nothing). A denied Backend gives `{restricted: true}` inside an allowed route, with no condition message. A denied `name`-ref target gives `restricted`. A denied ancestor is `{restricted: true}` with no identity. A denied Namespace's labels never appear in a computed explanation. Derived counts drop denied objects, and a listener with a hidden member shows "Cross-check hidden". A denied detail returns 404. Health and flags are unchanged.
- **Compatibility:** every Phase 1 golden passes unchanged, except the additive changes listed in §7.6; a test asserts those diffs only add optional fields. The delegation cases above cover the numbers that do change.

### 9.2 Per-version goldens for the computed views

Each rule set (`kgateway-2.3`, `kgateway-2.4`) has its own fixture directory under `pkg/computed/testdata/<rules>/`. Each case holds an input world (Gateways, ListenerSets, routes, Namespaces, settings) and the expected outputs of views A, B and C, plus the expected controller signals for the cross-check.
- The cases cover every rule in §3.4, §5.2 and §5.3, and every version difference in §5.4.
- **Provenance:** where possible, a case's expected controller signals are copied from kgateway's own translator test outputs at that tag (`translator/gateway/testutils/outputs/…`), with the source file named in the case. They then double as a check that the UI's reading of kgateway matches kgateway's own tests.
- A new kgateway minor gets a new directory, starting as a copy, and a diff review of the rule files between the tags.
- API goldens (§7.6) are checked by Go and by TypeScript.

### 9.3 D-019 recompute tests (deterministic, fake clock)

The engine takes a clock and a trigger source. Tests drive both directly; nothing sleeps.
- A burst of 500 events within 100 ms gives exactly one recompute, 250 ms after the last event.
- A continuous stream (one event every 100 ms for 5 s) gives one recompute per 1 s window.
- A trigger during a running recompute gives exactly one more recompute after it.
- No trigger for 29 s gives no recompute; the safety timer fires at 30 s. `--poll-interval 2s` gives one every 2 s.
- A grace expiry 60 s after an object's generation was first seen triggers a recompute at that moment, and the finding turns from info to warning.
- A metrics result arrival triggers one recompute. A metrics result reaching 3 × the interval triggers one at the staleness deadline.
- An access transition (watch failure) with no object event triggers a recompute within the confirm tick.
- `collectedAt` advances each second while idle, and stops advancing while a recompute is blocked. The API then serves an ageing `collectedAt`.
- Phase 1's engine tests run unchanged against the new loop.

### 9.4 Frontend (Vitest)

- Filters, sort, `listener` and cursor round-trip through the URL; a 400 drops the cursor.
- Each state of §8.6, including stale turning every badge unknown with fake timers.
- An install switch never shows the previous install's rows.
- `objectPath` follows `link` only: `detail`, `yaml`, and `none` as plain text, for every kind; restricted findings never link.
- Every computed value renders a `ComputedBadge`; a controller value renders none.
- A disagreeing listener shows both numbers and the warning icon with text.
- `CorePages` with only the four Phase 1 slots still renders the new pages.
- A contribution at `routes` cannot replace the core page.
- `getCommand` returning `undefined` hides the copy button.
- Skeletons and loaded rows have the same height (layout shift).

### 9.5 kind e2e

New fixtures, run on kgateway 2.3.11 and 2.4.7 in the existing matrix:
- a Gateway with three listeners (HTTP, HTTPS with a wildcard hostname, TCP) and a `Selector` allowedRoutes on a labelled namespace;
- routes rejected with `NotAllowedByListeners`, `NoMatchingParent` (including the sectionName quirk) and `NoMatchingListenerHostname`;
- a route with two parentRefs to one listener;
- a ListenerSet with one invalid listener, and a denied ListenerSet;
- a delegation tree: a name child, a label child, a wildcard parent, a child forbidden by its parentRef, and a child whose header match does not fit the parent;
- a child with hostnames, and an inherit-parent-matcher child;
- a cycle;
- a static `Backend` and an invalid one;
- a replaced rule.

**Assertions.**
- On both tags, every listener's computed count equals the controller's `attachedRoutes`.
- Every view B outcome matches the child and parent status that the version's rule set expects.
- No KGWUI-012 is raised.
- A deliberately skewed fixture (editing a Namespace label while the controller is scaled to 0) raises KGWUI-012 after the persistence window, and clears when the controller returns.

**First plan task (cluster observation, like Phase 1's D-012).** On 2.3.11 and 2.4.7, record whether status is **produced**, and under which parentRef, for:
1. an implicit child (empty parentRefs) after the parent stops selecting it: is the old entry removed (§4.3)?
2. an explicit child whose parentRef omits the namespace (§5.4);
3. an explicit child whose parentRef sets `sectionName` or `port` (§5.4);
4. an implicit child with empty parentRefs selected by name, wildcard and label;
5. a hostname-bearing child whose rules all fail the parent's match (§5.2);
6. a matcher-dropped child on 2.3: is stale status cleared, as on 2.4?
7. the two flagged view B quirks (§5.2): a second delegation ref in one rule, and the same ref in two rules;
8. Backend `observedGeneration` (§6.2);
9. listener `attachedRoutes` for delegated children (expected: not counted) and for ListenerSet routes.

Normalised-identity unit tests cannot answer these. The result becomes a decision entry, and the rule sets and expectations are adjusted before the backend tasks that depend on them.

### 9.6 Scale tiers and budgets

The user set three tiers **(user, 2026-10-22)**. The shapes per tier are our proposal **(our call; §15 Q6b)**:

| Shape | Typical | Large (capacity target) | Extreme (stress) |
|---|---|---|---|
| Gateways | 10–30 (bench: 30) | 50–100 (100) | 500 |
| Listeners per Gateway | 3 | 4 | 4 |
| HTTPRoutes | 100–500 (500) | 1,000–2,000 (2,000) | 5,000 |
| parentRefs per route | 1–2, mean 1.2 | 1–2, mean 1.3 | 1–4, mean 1.5 |
| ListenerSets | 10 | 200 | 2,000 |
| Delegated children | 10 % of routes, depth ≤ 2, fan-out ≤ 10 | 10 %, depth ≤ 3, fan-out ≤ 50 | 10 %, depth ≤ 4, fan-out ≤ 100, plus a diamond lattice 8 layers deep |
| Other route kinds (GRPC, TCP, TLS) | 5 % each | 5 % each | 5 % each |
| Namespaces (labelled, some `Selector` listeners) | 20 | 100 | 500 |
| Backends | 50 | 200 | 1,000 |
| Failed parentRefs in status | 5 % | 5 % | 5 % |

**Budgets apply at Large.** Typical is the UX reference. Extreme reports numbers with no pass/fail. All budgets are **(our call)** on the CI runner class:

| Measure | Budget at Large |
|---|---|
| Phase 1 compute | measured first as the baseline; no Phase 1 benchmark exists today |
| One recompute, warm decode cache, including views A, B and C | ≤ 150 ms |
| One recompute, cold (first) | ≤ 1 s |
| Allocation per warm recompute | ≤ 32 MiB |
| Change burst: 1,000 object events within 1 s | ≤ 2 recomputes; event-to-model p95 ≤ 1.2 s |
| Sustained change stream (an event every 100 ms for 10 min) with 20 clients polling | recompute rate ≤ 1 per s; engine CPU ≤ 15 % of one core; GC share reported |
| Idle cluster (no events) for 10 min | recomputes only from the safety timer and deadlines; confirm-tick CPU reported |
| Heap of the index, the computed views and the decode cache | ≤ 32 MiB above the Phase 1 baseline; reported per kind |
| List request through the HTTP handler, per filter mix (none, parent, listener, namespace+state, `q`, `sort=health`, delegation of the widest parent) | p95 < 50 ms, p99 < 100 ms |
| The same with 20 concurrent clients | p95 < 100 ms |
| Route detail on the deepest child (bounded up-chain, effective matchers) | p95 < 50 ms |

**Method.**
- Go benchmarks run with `-benchmem`, for allocation and averages.
- Latency percentiles do not come from `ns/op`. A harness sends 10,000 requests per filter mix through `httptest`, records each duration, and reports p50, p95 and p99 with `b.ReportMetric`. The concurrent run uses 20 goroutines over the same harness.
- Burst and stream runs inject events through the trigger source with a real clock, so list latency is measured while Models swap.
- Typical and Extreme run the same suite and report numbers.

Numbers are measured and written into the sub-phase record, with the machine and Go version. Nothing is estimated. A Large budget that fails blocks the sub-phase.

### 9.7 Platform neutrality test

`explore.Build` and the computed rule sets run in a test over a snapshot with no Pods or Deployments, namespace labels given as a plain map, settings given as a plain map, and objects without `uid` or `resourceVersion`. They produce the same rows; the decode cache is bypassed.

## 10. Platform neutrality (D-016)

| Addition | How it stays neutral |
|---|---|
| `pkg/explore`, `pkg/computed` | Read objects only through `pkg/objects` and unstructured. They import no `corev1`, `appsv1` or client-go; the import boundary test covers the new files (`internal/boundary/imports_test.go`). |
| Namespace labels | Turned into a `map[string]map[string]string` in the provider-side code that already handles `corev1.Namespace` (`pkg/install`, on the allowlist). The rules never see the typed object. |
| Controller settings | Read from the Pod spec in `pkg/install` (allowlisted), exposed as `install.Resolved.Settings`. A non-Kubernetes provider supplies the same keys its own way, or none ("unknown"). |
| D-019 triggers | The engine takes an abstract trigger source. The Kubernetes provider feeds it from informer handlers; a file-mode provider feeds it from its own change stream (design §14.6). |
| Assignment changes | Stay in `pkg/install/assign.go`, already on the boundary allowlist. No new typed imports. |
| Decode cache | Keyed by `uid` and `resourceVersion`, which file mode synthesises (design §14.8). Missing keys bypass it. |
| `Backend` | A kgateway CRD, available on every platform. It is what Phase 4 builds on without Services. |
| Service backendRefs | Shown as written. Services are never watched or looked up. |
| Copy command | Platform copy through `Vocabulary.getCommand`; a platform can hide it. |
| URLs and IDs | Install IDs stay opaque and encoded (design §14.3 H1). Object paths use group, kind, namespace and name, which every provider supplies. |
| Readiness | No proxy or Pod data on the new pages. Gateway health comes from findings, which already respect capabilities. |

## 11. Security

- **No Secret reads.** The ClusterRole stays without Secrets (D-013). Listener `certificateRefs` are shown as `kind namespace/name` text. They are never fetched, resolved or linked: their `link` is always `none` (§7.1).
- **No TLS options.** `tls.options` is not decoded for the Overview tab (§4.1); the YAML tab shows the object as stored, as today.
- **No admin server.** The UI does not port-forward to, or call, the controller's admin port (§3.8). RBAC stays get/list/watch.
- **Authorizer everywhere** (§7.4). Denied and absent look the same. Every dereferenced object is authorized, including Namespace labels quoted in computed explanations. Lists never report filtered counts of denied rows; only an unfiltered per-kind count, `deniedInInstall`.
- **Admission is bounded** (§7.1). The detail and resource endpoints serve owned objects, finding-referenced objects, objects named by an owned object's spec, routes selected by an owned parent, and the install's no-parent rows. Nothing else.
- **Settings read are named, not dumped.** Only the env entries of §3.6 are read from the controller Pod. No other env value is copied into the model or the API.
- **No new reach.** No new RBAC beyond the `Backend` watch, already covered by `gateway.kgateway.dev: *`.
- **Input handling.** Query params are validated against fixed sets. Cursors are decoded with a size cap (1 KiB) and checked against the sort. Messages and annotations render as text, never as HTML.
- **Trust model unchanged** (D-013). The new pages show only what the ServiceAccount could already read through the YAML view, plus what the UI computes from it.

## 12. Cost

This phase generates no data-plane configuration and no xDS churn.

| Item | Cost |
|---|---|
| API server | One more cluster-wide watch (`backends`). No other new requests. Event handlers add no API calls. |
| Recompute | Only on change, debounced (D-019), plus the safety timer and deadlines. Each recompute covers the ownership BFS, views A, B and C, health and `explore.Build`, linear in objects and parentRefs except view B's path walk, which is budgeted (§5.2). Budgets in §9.6. |
| Idle | A 1 s confirm tick compares access states and stamps `collectedAt`. No recompute. |
| Up-chain and effective matchers | Per route detail request, at most 256 node visits and 20 parent edges (§5.5, §7.3). Not stored. |
| Decode | Once per object change, not per recompute (§3.3). |
| Memory | The index rows, the computed views, the decode cache and, briefly, the previous Model while it is replaced. Measured and reported per kind, §9.6. |
| Per request | One index lookup, a filter scan, an optional sort, one page. No API server calls. |
| Client | One list request per visible table every 5 s, at most 200 rows each. Detail pages poll one detail endpoint plus their visible tables. |

## 13. Sub-phase split (suggestion for the plan)

| Sub-phase | Content |
|---|---|
| 2.1 Backend | The cluster observation task (§9.5). The Phase 1 compute baseline benchmark. D-019's event-driven engine and its tests. Views and the decode cache. Reported-edge and Backend assignment, declared candidates, admission and links. `pkg/computed` with views A, B and C, both rule sets, the per-version goldens and the cross-checks. KGWUI-011, KGWUI-012 and the Backend rules. `pkg/explore` indexes. The API endpoints and goldens. Benchmarks at all three tiers. |
| 2.2 Frontend | Lists, detail pages, tabs, the listener table and computed badges, the delegation table, the up-chain, link retargeting, copy helpers, the `CorePages` slots, the wording pass, Vitest, ui-loop evidence. |
| 2.3 e2e and close | kind fixtures and computed-vs-controller assertions on 2.3 and 2.4, the Playwright additions, the README and chart notes (`--poll-interval`'s new meaning), and the release-notes line on changed counts. |

2.1 is large. The plan should consider splitting views A and B/C into separate tasks, or 2.1a/2.1b. Every task states its file list for approval (change-size guardrail).

## 14. Upstream asks (draft text only, not filed)

Each needs a check against kgateway `main` before filing. D-020 makes each computed view a bridge: when kgateway reports the fact, the UI switches to it and keeps its computation only as a cross-check. The user decides where the asks go (§15 Q5).

### 14.1 Resolved route → listener relationships

> **Expose which listeners each route attached to**
> Route status is per parentRef and listener status has only `attachedRoutes`. Which listeners a Gateway-wide parentRef matched, and with which effective hostnames, is computed (`pkg/kgateway/query/route.go:437-547`, `query/query.go:231-296`) but published nowhere. Tools must re-run the attachment rules to show routes per listener.
> Ask: publish the per-listener result. Options: per-listener entries in a route condition message, a `kgateway.dev/` status extension, or the state object of §14.3. Open question: which shape scales to thousands of routes per listener?

### 14.2 Delegation resolution per child

> **Report delegation outcomes, including dropped children and effective matchers**
> A delegation ref selects children; each is then checked for permission, matcher fit and hostnames (`translator/httproute/delegation.go:26-102`, `delegation_helpers.go:33-107`). Only kept children and hostname rejections get status. A child dropped by the matcher fit, or skipped by its parentRefs, gets none. The parent shows `ResolvedRefs=False/BackendNotFound` only when no child is left. The effective matcher after `inherit-parent-matcher` is visible only in xDS.
> Ask: per child and per parent ref, report selected / attached / dropped (with the failing match) / forbidden, and the effective matches. A child with all rules dropped could get `Accepted=False` with a reason such as `NoMatchingParentRule`. Open question: is a partial drop `Accepted=True` plus `kgateway.dev/Programmed=False/RouteRuleDropped`, as replaced rules are reported today?

### 14.3 A controller-written state resource outside Gateway API (user's proposal)

> **A read-only state CRD for resolved routing state**
> Gateway API status has no room for resolved relationships: routes per listener, delegation trees, effective hostnames and matchers, shadowing. Earlier approaches that put the full config in one object hit the etcd object-size limit.
> Ask: a kgateway-owned CRD, written by the controller and read by the controller itself, UIs, CLIs and monitoring. It holds summaries, not full config, and is split to stay small: one object per Gateway (listeners → route refs and effective hostnames), and one per delegating parent route (children and outcomes), each with a size cap and a truncation marker. It needs no admin port and no `pods/portforward`, and it carries `observedGeneration` per input. Open questions: the split key, whether ListenerSets get their own objects, and write rate under churn.

### 14.4 Shadowed or duplicate match conditions

> **Routes shadowed by an identical match get no condition**
> Routes are merged per virtual host and sorted stably. With equal sort keys the oldest route wins (`translator/routeutils/sortable_route.go:120-171`). The losing route stays `Accepted=True` with no signal. gateway-api#4303 tracks the spec side.
> Ask: a `kgateway.dev/` condition (or a reason on `kgateway.dev/Programmed`) on a route whose match is fully shadowed by another route on the same listener and hostname, naming the winner.

### 14.5 Hostname resolution

> **Report effective hostnames per parentRef and listener**
> Attachment uses a hostname intersection (`query/query.go:231-296`). On 2.4, plain-HTTP listener isolation then narrows hostnames further (`translator/listener/gateway_listener_translator.go:689-747`). Neither result is visible, and `NoMatchingListenerHostname` carries no message.
> Ask: publish the effective hostnames, or at least a message naming the listener hostname that did not intersect. This also records a UI limitation: the UI shows status-level hostnames, not the 2.4 isolation result.

### 14.6 Clear a child's status when it stops being delegated (only if §9.5 confirms it)

> On 2.4 a matcher-dropped child's status is cleared (`pkg/krtcollections/policy.go:1150-1178`). An implicit child the parent stops selecting, and the same drop on 2.3, may keep a stale entry, because no report is built (`pkg/reports/status.go:452-472`).
> Ask: remove kgateway-owned parent entries from routes the translation no longer reports, in every case.

### 14.7 Additions to the pending scope endpoint proposal

The "effective discovery scope" proposal (phase-status carry-over) could also report two settings, so tools stop reading Pod env:
- the effective `DELEGATION_WILDCARD_NAMESPACE` (`api/labels/delegation.go:12`);
- the experimental-features flag (`api/settings/settings.go:324`), which D-017 §4 already wants.

### 14.8 A safe read-only source for the effective config

> The admin server holds `/snapshots/xds` but binds to localhost, shares a port with `/logging` and pprof, and needs `KGW_ENABLE_ROUTE_SOURCE_METADATA` for reliable attribution (§3.8).
> Ask: a separately bindable, read-only snapshot endpoint (or listener) with route source metadata on by default.

## 15. Open questions for the user

### 15.1 Decided in review (2026-10-22)

| Former | Question | Decision (user, 2026-10-22) |
|---|---|---|
| Q2 | Hostname intersection: compute or show both sides? | Compute it, as part of view A, labelled and cross-checked. It is also recorded as a limitation and upstream ask (§14.5). |
| Q3 | Shadowed matches and inherited delegation matchers? | Shadowed matches stay the controller's job (§14.4). Inherited and default-mode effective matchers are computed (view C). |
| Q6 | Scale target? | Three tiers: Typical, Large (capacity target, budgets apply) and Extreme (stress, report only) (§9.6). |
| Q8 | Routes per listener? | A product requirement: computed (view A) and cross-checked against `attachedRoutes`. |
| — | Recompute model | Event-driven (D-019). |
| — | Mirroring boundary | Overridden for views A, B and C under D-020's rules. |

### 15.2 Still open

| # | Question | Default this design assumes | If the other answer |
|---|---|---|---|
| Q1 | Policies attached to a route or Gateway (a targetRef index, no merge order): Phase 2 or later? | **Later (Phase 5).** A list without precedence can mislead. | Add a `policies` section to detail pages and a by-target index. About one task more; the page must say "attached, not effective". |
| Q4 | A graph view? | **Later.** | A new task in 2.2 and a layout library; the tree and tables stay the primary view. |
| Q5 | Upstream asks §14: one combined issue, separate issues, or the state CRD (§14.3) as the umbrella? | **§14.3 as an umbrella proposal**, with §14.1, §14.2 and §14.5 as its motivating cases; §14.4 and §14.8 separate; §14.7 joins the scope proposal. | Separate issues are easier to triage but scatter the design argument. |
| Q6b | The shape per tier in §9.6: parentRefs per route, ListenerSets, delegation fan-out and depth. | **As proposed.** | The fixture generator and budgets change; the tiers stay. |
| Q7 | Re-run the support-ticket and knowledge-base research before plan approval? | **Yes, before plan approval.** The current research covers support and field threads only. | Approve on the current research; new pain points become Phase 3 inputs. |
| Q9 | Keyset cursors without page numbers (§3.9)? | **Yes.** | Offsets add "page N" links; pages shift as the model changes. |
| Q10 | HTTPRoutes with empty parentRefs and no status (§4.3): listed under `no-parent` in every install whose scope holds them; not counted; no status finding; YAML view (detail only when an owned parent selects them); the Authorizer applies. Accept? | **Yes.** | Drop them (silent delegation misses stay invisible), or add an info finding per route (noisy where intentional). |
| Q11 | Backend freshness (§6.2): current `False` → error; current `True` → healthy; stale → unknown, no finding; no evidence (2.3): `False` → warning, `True` → unknown; absent → unknown. No-evidence states never show green and never turn the install unknown. Accept? | **Yes.** | `False` without evidence as an error: a stale False after an edit shows red. No finding: an invalid 2.3 Backend is never flagged. |
| Q12 | Delegated children now count in tiles and install health (§4.3). Ownership stays status-based. Computed views explain and cross-check but never own: a computed-only child is a candidate plus KGWUI-012. An eligible status entry keeps the edge whether current or stale. Accept the changed numbers? | **Yes.** Children were invisible, which hid real failures. | Count computed children too: health would follow the UI's computation rather than the controller's status, which D-020 does not allow without a further amendment. Or keep Phase 1 counts: child failures never reach the health strip. |
| Q13 | KGWUI-012 "computed view disagrees": severity and persistence (§6.2). | **Warning**, after 30 s of persistent disagreement with current status. | Error: a UI rule bug turns installs red. Info: a real controller bug (for example a wrong `attachedRoutes`) stays green. A shorter window raises transient noise during status lag. |
| Q14 | D-019 debounce window and latency bound (§3.1). | **250 ms quiet, 1 s maximum, safety timer 30 s.** | A longer window lowers CPU under churn but delays the UI. A shorter safety timer adds idle recomputes. |
| Q15 | Dropped routes selected only by a `wildcard` ref: finding or not (§6.2)? | **No finding.** They are shown in the Delegation table. | A KGWUI-011 per dropped route: noisy where a namespace mixes children and unrelated routes. |
| Q16 | Read `KGW_ENABLE_EXPERIMENTAL_GATEWAY_API_FEATURES` from the controller Pod for view A (§3.6)? | **Yes**, for the computed views only; D-017 §4 is unchanged for the scope verifier. | Assume `true` (kgateway's default): a cluster with experimental features off shows persistent KGWUI-012 on listeners with experimental route kinds. |
