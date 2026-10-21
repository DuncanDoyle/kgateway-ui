# Phase 2 — Gateway & Route Explorer: Design

**Status:** Draft for cross-model review and user approval
**Date:** 2026-10-21
**Base:** `2c25e35`
**Roadmap:** `docs/roadmap.md`, Phase 2
**Decisions:** D-001 … D-018 in `docs/decisions.md`
**kgateway refs:** every behaviour claim cites `file:line` at tag `v2.4.7` (`b4a2bf74`) or `v2.3.11` (`c2af2887`). The UI supports both.

Decisions the controller made without the user are marked **(our call)**. §15 lists the ones the user should confirm.

## 1. Goal and scope

Operators debug Gateway API routing by reading `status.parents[].conditions` in YAML, by admin snapshots and by Envoy config dumps. Support and field threads show four questions come up most:
- Which routes are on this Gateway?
- Why is this route not attached?
- Which child routes did delegation pick up?
- Which route is the broken one?

Phase 2 answers these from the cluster's own objects and status, at the scale of thousands of routes. It never evaluates routing itself (D-005, D-006).

**In scope**
1. **Gateway detail page** (roadmap):
   - listeners with name, port, protocol, hostname, TLS mode, certificateRef names (never Secret content) and allowedRoutes;
   - addresses;
   - Gateway and per-listener conditions with reason, message and freshness;
   - status `attachedRoutes` next to the UI's list of routes;
   - attached ListenerSets.
2. **Routes per Gateway, including the failed ones** (new, fits). Next to the attached routes, the page lists routes whose parentRefs target the Gateway but are not accepted, with the controller's reason and message. The source is route status, never a UI re-evaluation of allowedRoutes.
3. **ListenerSet detail page** (roadmap): parent Gateway, listeners, conditions (including the partial failure `Accepted=True/ListenersNotValid`) and attached routes. It covers ListenerSet v1, XListenerSet and any ListenerSet kind a downstream registers.
4. **HTTPRoute detail page** (roadmap):
   - hostnames ("inherits the listener's hostnames" when empty);
   - each parentRef with its status conditions;
   - rules with their matches, filters and backendRefs (with weight).
   Backend resolution comes from route conditions (`ResolvedRefs`); the UI does not re-check ReferenceGrants.
5. **Delegation, observed not mirrored** (§5). The tree is built from what kgateway writes on child routes.
6. **Navigation both ways** (roadmap). Route → Gateway (up the delegation chain), Gateway → routes, ListenerSet ↔ Gateway ↔ routes, child ↔ parent route. Every object gets a stable deep link. Finding links open these pages; the YAML view stays as a tab.
7. **Server-side lists** (roadmap). Per install: Routes, Gateways and ListenerSets, indexed once per model compute, filtered, sorted and paginated (§3).
8. **`Backend` kind** (Phase 1 carry-over, design §14.5). Registered with its `Accepted` condition and an assignment rule (§4.4).
9. **Copy helpers.** Copy `namespace/name` (exists), and copy a get-as-YAML command for the object.

**Out of scope (later phases or upstream)**
- Effective policy merge and which policy wins (Phase 5; needs controller output).
- Envoy/xDS view per route, request-matching simulation, full precedence order (Phases 6 and 9).
- Duplicate or shadowed host+path detection. kgateway writes no condition for it, and the precedence sort is controller logic (`v2.4.7 pkg/kgateway/translator/routeutils/sortable_route.go:120-171`). Upstream ask, §14.
- Inherited parent matchers on delegated children, and child rules dropped for not fitting the parent (`v2.4.7 pkg/kgateway/translator/httproute/delegation_helpers.go:33-107`). No status exists. Upstream ask, §14.
- A graph or topology view. Tables and drill-downs come first.
- Detail pages for GRPCRoute, TCPRoute and TLSRoute. They appear in lists and link to the YAML view.

**Anti-goals**
- No UI evaluation of `allowedRoutes`, hostname intersection, label or namespace selectors, ReferenceGrants or route precedence. Each is controller logic (memory rule, D-005, D-016 §7).
- No per-listener route attribution the controller did not write (§3.4).
- No new data source: no admin server, no Envoy admin, no Secrets.

## 2. Primary flows

Each flow starts from a deep link, a finding or a list. All URLs are under `/i/:id/` (§8.1).

1. **"Which routes are on this Gateway?"**
   - Open Gateways, filter by name, open `infra/edge`.
   - The Overview tab shows the Gateway's conditions, then a listener table. Each listener row shows the status `attachedRoutes` count and the number of routes that name it by `sectionName`.
   - Below, **Attached routes** is a paginated table. It holds every route of the install with a current `Accepted=True` status entry for a parentRef to this Gateway or to one of its ListenerSets. A column shows the listener it names, "All listeners" when the parentRef has no `sectionName` or `port`, or the ListenerSet it came through.
2. **"Why is this route not attached?"**
   - The same page has **Routes that failed to attach**. It lists routes with a current `Accepted=False` for this Gateway, with the raw reason (`NotAllowedByListeners`, `NoMatchingParent`, `NoMatchingListenerHostname`, `v2.4.7 pkg/kgateway/query/route.go:525-543`) and message.
   - It also lists routes still waiting for status and routes from namespaces the controller does not watch (KGWUI-004), each with its own label.
   - The route page shows each parentRef with every condition, its reason, message and freshness ("Describes generation 3; the route is at 4").
3. **"Which child routes did delegation pick up?"**
   - Open the parent HTTPRoute. A rule whose backendRef is a delegation ref shows it as written: by name, `*`, or label.
   - **Child routes** lists every route whose status names this route as its parent.
   - A delegation ref with no reporting child shows "No child route reports this parent", next to the parent's own `ResolvedRefs` condition.
   - A child row opens the child, whose page shows the chain up to the Gateway.
4. **"Which route is the broken one?"**
   - Open Routes and filter by state, for example "Rules replaced or dropped". The filter covers `RouteRuleReplaced` and `RouteRuleDropped` on both versions (§6.1).
   - The table names each route, its parent and its health. The filter lives in the URL, so the link can go into a support ticket.
5. **"Is this ListenerSet working?"**
   - Open it from the Gateway page or the ListenerSets list. It shows the parent Gateway, its conditions (`Accepted=True/ListenersNotValid` means some listeners failed), the listener table and the attached routes.

## 3. Architecture

```
 engine.Compute (every --poll-interval, default 2 s)
   snapshot → install.Discover/Resolve/Assign → health.Evaluate → overview.Build
                                              └→ explore.Build  (new, per install)
   Model{Installs[i].Explore *explore.Index}   immutable, swapped atomically

 api (new handlers)  →  Model.Installs[i].Explore  →  filter → authz → sort → page
```

### 3.1 Where the index lives

- A new package `pkg/explore` builds one `Index` per install, inside `engine.Compute`, after health (`pkg/engine/engine.go:169-180`). It is stored on `InstallModel` and is immutable once the Model is swapped in (`engine.go:181-183`), like the overview and the health report. **(our call)**
- Handlers read only from the current Model, so a list request never touches the API server or the informers.
- `explore.Build` is a pure function of `(install.Resolved, *install.Owned, *cache.Snapshot, health.Report, decoded views)`. The API can rebuild nothing per request; it only filters what the index holds.
- Downstream builds get the index through the Model; there is no new extension registry in Phase 2.

### 3.2 Decoded views are cached across computes

Today every `objects.*` accessor decodes the whole object again through `FromUnstructured` (`pkg/objects/objects.go:116-123`). A route's status is decoded several times per compute. At 5,000 routes recomputed every 2 s, that is the cost to avoid.
- The engine keeps a decode cache keyed by `(uid, resourceVersion)`. Each entry holds the typed views of §4 for one object. Entries absent from the snapshot are pruned in `observe` (`engine.go:114-130`), as the grace map is today. **(our call)**
- An unchanged object is decoded once, not once per compute.
- An object without a `uid` or `resourceVersion` is decoded every time, so a provider that lacks them still works (D-016 R5, design §14.4).
- The views are read-only and shared between Models.

### 3.3 What is indexed

| Index | Key | Value | Serves |
|---|---|---|---|
| Route rows | — | `[]RouteRow`, sorted by (namespace, name, group, kind) | Routes list, default order |
| By parent | Gateway or ListenerSet ref | row indexes, with the parentRef and attachment state | Gateway and ListenerSet pages |
| By parent route | HTTPRoute ref | rows of the children that report it (§5) | Child routes, delegation tree |
| By namespace | namespace | row indexes | namespace filter |
| By backend | Backend ref | route row indexes | Backend tile, route → Backend links |
| Condition state | — | a bit set per row (§6.1) | state filter |
| Health | object ref | worst finding severity | health column, health filter |
| Gateway rows | — | `[]GatewayRow` (§7.3) | Gateways list |
| ListenerSet rows | — | `[]ListenerSetRow` | ListenerSets list |
| Ancestors | route ref | up-chains to a Gateway or ListenerSet | route page breadcrumb |

- The health map replaces today's per-row scan of all findings (`pkg/overview/overview.go:166-187`), which is linear in findings per Gateway.
- Hostname and name search is a scan over pre-lowercased strings in the rows. At 5,000 rows a scan is cheaper than keeping an n-gram index current. **(our call)**

### 3.4 Routes per listener: what the status can say

Route status is written per parentRef, not per listener. kgateway matches a parentRef against every listener (kinds, namespaces, `sectionName`/`port`, hostname intersection) and reports one result per parentRef (`v2.4.7 pkg/kgateway/query/route.go:447-543`). Listener status carries only a count (`attachedRoutes`).

So the UI attributes a route to a listener only when its parentRef says so:
- with `sectionName`, to that listener;
- with `port`, to "listeners on port N";
- with neither, to "All listeners".
It never decides which listeners a Gateway-wide parentRef matched. That decision is the controller's (anti-goal, §1). **(our call)**

The listener table shows the status count and the number of routes that name the listener, side by side, without a mismatch finding. The two counts measure different things.

### 3.5 Pagination: keyset cursors

The model is recomputed every 2 s. Page numbers (offsets) shift whenever a route is added or removed in front of the page. Keyset cursors do not.
- A cursor is opaque base64url. It holds the sort key, the direction and the last row's key tuple: (sort value, namespace, name, group, kind).
- `next` starts strictly after that tuple in the current Model; `prev` ends strictly before the first row's tuple. Both are binary searches over the sorted, filtered rows.
- Ordering is total and deterministic: every sort ends with the tie-break (namespace, name, group, kind). The same Model always gives the same order, and a new Model keeps unchanged rows in the same relative order.
- A row whose sort value changes (for example its health) moves to its new place. A user paging through may see it twice or miss it. That is the cost of a live list, and it is the same with offsets.
- The response carries `total` (after filters and the Authorizer), so the UI can say "51–100 of 2,340".
- There is no "jump to page 40". Filters narrow a list faster than paging does at this size. **(our call; §15 Q9)**

Cursors are preferred over Kubernetes `limit`/`continue`, which pages only by key and expires on compaction. The research covered this.

### 3.6 Per-request cost

A list request: pick the narrowest index (parent, then namespace, else all rows), test each candidate against the remaining filters and the state bit set, apply the Authorizer, sort when the sort is not the default, and slice one page. At 5,000 rows each step is linear or `n log n` on in-memory structs. §9.6 sets the target and the benchmark that proves it.

## 4. Data model

### 4.1 New typed views in `pkg/objects`

All views decode from unstructured objects, apply Gateway API defaults and never fail. A malformed field gives an empty value, as today (`objects.go:116-123`). No `sigs.k8s.io/gateway-api` dependency is added (design §3.2).

| View | Fields |
|---|---|
| `Listener` (spec) | `Name`, `Port` (0 = unset), `Protocol`, `Hostname`, `TLS{Mode, CertificateRefs []Ref}`, `AllowedRoutes{Namespaces{From, Selector}, Kinds []GroupKind}` |
| `ListenerStatus` (extended) | adds `SupportedKinds []GroupKind` to today's name, `attachedRoutes` and conditions (`objects.go:172-187`) |
| `GatewayView` | `Class`, `Listeners []Listener`, `SpecAddresses`, `StatusAddresses []{Type, Value}`, `AttachedListenerSets *int32`, `Conditions`, `ListenerStatus` |
| `ListenerSetView` | `Parent ParentRef` (today's `ListenerSetParent`, `objects.go:219-225`), `Listeners []Listener`, `Conditions`, `ListenerStatus` |
| `HTTPRouteView` | `Hostnames`, `ParentRefs`, `Rules []Rule`, `Parents []RouteParentStatus` |
| `Rule` | `Index`, `Name`, `Matches []Match`, `Filters []Filter`, `BackendRefs []BackendRef` |
| `Match` | `Path{Type, Value}`, `Headers []{Type, Name, Value}`, `QueryParams []{Type, Name, Value}`, `Method` |
| `Filter` | `Type`, `ExtensionRef *Ref`, `Config map[string]any` (the type's own block, as written) |
| `BackendRef` | `Group`, `Kind` (default `Service`), `Namespace` (default the route's), `Name`, `Port`, `Weight` (default 1), `Filters`, `Delegation *DelegationRef` |
| `DelegationRef` | `Mode` (`name`, `wildcard`, `label`), `Namespace`, `Name` or `Label` |
| `BackendView` | `Type` (`spec.type`), `Conditions` |

Notes:
- `TLS.Options` is not decoded. It is free-form and stays in the YAML tab. **(our call)**
- `CertificateRefs` are refs only. They are never resolved, fetched or linked (§11).
- Generic routes (GRPCRoute, TCPRoute, TLSRoute) keep today's parentRef and parent-status views. Their rows show hostnames when the kind has them.

### 4.2 Delegation refs, as kgateway reads them

A backendRef is a delegation ref when it names an HTTPRoute (`group: gateway.networking.k8s.io, kind: HTTPRoute`) or a label selector (`group: delegation.kgateway.dev, kind: label`) (`v2.4.7 pkg/kgateway/translator/backendref/types.go:14-28`). The view records the mode as written:

| Mode | Spec | kgateway behaviour |
|---|---|---|
| `name` | `name: <route>` | One route in the ref's namespace (default the parent's); missing → unresolved reference (`v2.4.7 pkg/kgateway/query/route.go:346-351`) |
| `wildcard` | `name: "*"` or empty | Every HTTPRoute in the ref's namespace (`route.go:339-344`) |
| `label` | `name: <value>` | Routes labelled `delegation.kgateway.dev/label=<value>` (`v2.4.7 api/labels/delegation.go:7`). Namespace `all` (env `DELEGATION_WILDCARD_NAMESPACE`, `api/labels/delegation.go:12`) searches every namespace (`route.go:327-337`) |

The view only classifies the ref. It never looks up which routes a wildcard or label selects (§5.3). Both behaviours are the same on `v2.3.11` (`pkg/kgateway/query/route.go:287-313`).

### 4.3 Assignment of delegated child routes

Phase 1 assigns a route to an install only when a parentRef resolves to a Gateway or ListenerSet of that install (`pkg/install/assign.go:153-189`). A child route names an HTTPRoute, or nothing, so today **no child route is owned, counted or health-checked**. Phase 2 extends assignment. **(our call)**

A route C is a child of HTTPRoute P in install I when C's namespace is in I's scope, P is owned by I, and either:
- **explicit:** a current `spec.parentRefs` entry of C resolves to P (group `gateway.networking.k8s.io`, kind `HTTPRoute`, namespace default C's); or
- **observed:** C has a `status.parents` entry whose `controllerName` ∈ I.controllerNames and whose `parentRef` resolves to P.

Rules:
- The walk is a breadth-first pass from the Gateway-attached HTTPRoutes over a reverse index of both edge sources. A visited set stops cycles, and diamonds give a child two parents (§5.4). Cost is linear in routes plus edges.
- An explicit child keeps Phase 1's equality rule for its status: an entry counts only when its parentRef equals a current spec parentRef after defaults (design §4.3).
- An observed child (no spec parentRef to P) uses the status entry as its parent. There is no spec entry to compare it with.
- A child is owned once per install, even with several parents. Its `OwnedObject.Parents` lists every parent route.
- A child in a namespace outside I's scope, whose explicit parentRef names a route of I, gets KGWUI-004, as a route to an unwatched Gateway does today.
- An explicit parentRef to an HTTPRoute that does not exist gets KGWUI-003 (missing parent). HTTPRoute joins the "parent kinds" set (`assign.go:146-151`).
- Children count in the HTTPRoute tile and in health. This changes Phase 1's numbers on clusters that use delegation: children were invisible before.

**HTTPRoutes with no parent.** An in-scope HTTPRoute with no `spec.parentRefs` and no status from I's controller is a would-be child that nothing reports. It is listed under the Routes filter "No parent" in every install whose scope contains it. It is not counted and has no finding, because it may be intentional. **(our call; §15 Q10)**

**Stale observed status.** When a parent stops selecting a child, kgateway may leave the child's old status entry in place. `BuildRouteStatus` returns nil for a route with no report (`v2.4.7 pkg/reports/status.go:452-472`), so the syncer likely has nothing to write (unverified). The plan's first task verifies this on a cluster for both versions. If confirmed, an observed edge whose parent no longer has a delegation ref that could select it cannot be told apart without mirroring. The UI then marks observed-only edges as "Reported by the child's status" and adds the upstream ask (§14.3).

### 4.4 The `Backend` kind

- **Descriptor:** `gateway.kgateway.dev/Backend`, resource `backends`, version `v1alpha1`, namespaced, a new category `backend`, display name "Backends". It is added to `kinds.Upstream()` (`pkg/kinds/upstream.go:17-34`). RBAC already covers it (`gateway.kgateway.dev: *`, design §8).
- **Assignment:** a Backend belongs to install I when its namespace is in I's scope and a backendRef of a route owned by I resolves to it by full identity (group, kind, namespace defaulting to the route's, name). A Backend nobody references belongs to no install. Its tile counts only referenced Backends, and the tile says "in use". **(our call)**
- **Scope verifier:** the category maps to no `resources_managed` label, so KGWUI-007 ignores it (`pkg/scopeverify/verify.go:141-151`).
- **Resource endpoint:** owned Backends join `lookup` (`pkg/api/handlers.go:255-265`), so the YAML view and finding links resolve.
- **Health:** §6.2.
- This prepares Phase 4, which must work with `Backend` alone (D-016).

### 4.5 ListenerSet kinds stay generic

- ListenerSet v1 and XListenerSet are already registered with category `listenerset` (`pkg/kinds/upstream.go:21-22`). A downstream adds its own kind through `extension.WithKinds` with the same category.
- The detail view reads `spec.parentRef`, `spec.listeners[]` and `status.listeners[]` in the Gateway API ListenerSet shape. A downstream kind in this category must follow that shape. The kinds doc states this contract. **(our call)**
- kgateway reconciles ListenerSet v1 and XListenerSet both, and writes the legacy CRD's required `port` itself (`v2.4.7 pkg/kgateway/proxy_syncer/status_syncer.go:664,702`; `v2.3.11` same file `:612,650`).
- URLs and API paths carry the group and kind, so two kinds with one name never collide.

## 5. Delegation

### 5.1 Source of truth

kgateway writes a child's status under a parentRef that names the **parent HTTPRoute**, not the Gateway:
- On `v2.4.7` it uses the child's own spec parentRef when the child names the parent, and synthesises a fully qualified one otherwise (`pkg/kgateway/query/route.go:294-310`).
- On `v2.3.11` it always synthesises the fully qualified ref (`pkg/kgateway/query/route.go:252-259`).
- The status builder uses the spec parentRefs, or the report's refs when the spec has none (`v2.4.7 pkg/reports/status.go:489-496`).

The UI compares parentRefs after Gateway API defaults (`objects.go:64-84`), so both versions resolve to the same parent.

Known 2.3 gap: the `v2.4.7` comment says a synthesised ref that differs from the spec "would leave the child without status" (`route.go:294-299`). On 2.3 a child whose parentRef omits the namespace may therefore get no status. The UI shows that as "No status from the parent route" (§6.2). The plan's first task confirms it on 2.3.11.

### 5.2 The tree the UI shows

The tree is built from §4.3's edges only.

| Parent page shows | Source |
|---|---|
| Each delegation ref, as written (mode, namespace, name or label) | parent spec |
| **Child routes**: every child with an edge to this route, paginated | `GET …/routes?parent=…` (§7.2) |
| Each child's status for this parent: `Accepted`, `ResolvedRefs`, programmed state, freshness | child status |
| The parent's own conditions per Gateway parent | parent status |

| Ref state | Shown as |
|---|---|
| `name` ref, named route exists and has an edge to this parent | the child row |
| `name` ref, named route exists, no edge | "This route does not report this parent" on that ref, with a link to the route |
| `name` ref, no such route | "Route not found" on that ref (identity lookup, like a missing Gateway in Phase 1) |
| `wildcard` or `label` ref | the ref as written. Children are listed for the parent as a whole, not per ref. |
| Parent has delegation refs and no child reports it | "No child route reports this parent", next to the parent's `ResolvedRefs` condition |

The parent's `ResolvedRefs=False/BackendNotFound` message names the failing ref (`v2.4.7 pkg/kgateway/translator/httproute/delegation.go:45-47,98-99`). The UI shows that message verbatim next to the refs.

**N levels.** A child page shows its own delegation refs and children. The tree has no depth limit. Each level is one paginated list, fetched when expanded, so a 500-child wildcard does not load the whole subtree.

**Up the chain.** The route page header shows each path up to a Gateway or ListenerSet: `edge (Gateway) › api (HTTPRoute) › orders (HTTPRoute)`. With several paths (diamonds), up to 5 are shown and the rest are counted. **(our call)**

### 5.3 What is not shown, and why

| Not shown | Why |
|---|---|
| Which routes a `wildcard` or `label` ref selects | Evaluating selectors predicts the controller. `DELEGATION_WILDCARD_NAMESPACE` is controller env and invisible to the UI (`api/labels/delegation.go:12`). |
| Which ref selected an observed child | Child status names the parent route, not the backendRef. Attribution needs the selector. |
| Child rules dropped for not fitting the parent's match | Dropped with no status (`delegation_helpers.go:33-107`). Upstream ask, §14.1. |
| The effective matcher with `inherit-parent-matcher` | It is a merge the controller does (`delegation_helpers.go:117-142`, annotation `api/annotations/delegation.go:7`). The annotation itself is shown on the child. |
| Inherited policies | Phase 5. |

A child whose rules were all dropped is not reported at all (`delegation.go:49,81`). If nothing else reports the parent, the parent gets `BackendNotFound` (`delegation.go:98-99`). The UI shows both facts. It never claims to know the reason.

### 5.4 Cycles and diamonds

- kgateway tracks visited routes per path, so a diamond (A→B1→C, A→B2→C) is valid (`v2.4.7 pkg/kgateway/query/route.go:240-246`).
- A cycle sets `ResolvedRefs=False/RefNotPermitted` with a "cyclic reference" message on the parent (`route.go:271-276`, `pkg/kgateway/query/utils.go:38-44`; `v2.3.11` `route.go:244-248`, `utils.go:37`).
- The UI's own walks (ownership in §4.3, the up-chain in §5.2) keep a visited set. A walk that meets a route again stops and marks "Cycle" in the chain. The condition text comes from the parent's status.

### 5.5 Child hostnames

A child with `spec.hostnames` gets `Accepted=False/UnsupportedValue` (`v2.4.7 pkg/kgateway/translator/httproute/delegation.go:83-91,104-110`; `v2.3.11 …/delegation.go:94`). Children use the parent's hostnames (`v2.4.7 delegation.go:51-53`). The child page says "Inherits the parent route's hostnames", and the condition shows as a KGWUI-002 error once children are owned (§4.3).

## 6. Health additions

Kept minimal. Phase 1's checks and roll-up (design §5) are unchanged except where this section says so.

### 6.1 Condition state flags on route rows

Each route row carries flags from its **current** status entries for this install's parents, using Phase 1's freshness rules (design §5.2). These drive the Routes filter; they create no findings by themselves.

| Flag | Rule |
|---|---|
| `accepted` | some parent: `Accepted=True` |
| `not-accepted` | some parent: `Accepted=False` (reason kept) |
| `refs-unresolved` | some parent: `ResolvedRefs=False` |
| `not-programmed` | some parent: `kgateway.dev/Programmed=False`, or `Accepted=False` with reason `RouteRuleReplaced` or `RouteRuleDropped` |
| `pending` | some parent: no status, `Pending`, or stale |
| `no-parent` | §4.3, HTTPRoutes with no parent |

- `kgateway.dev/Programmed` exists on `v2.4.7` (`api/conditions/conditions.go:7`) and not on `v2.3.11`. On 2.3 the replaced and dropped reasons arrive through `Accepted=False` (design §10). The reason names are the same on both (`pkg/pluginsdk/reporter/types.go:22-27` at both tags).
- Reasons are shown as raw strings and never mapped to a fixed enum, because reason sets differ across specs and versions (research, gateway-api#4760).

### 6.2 Findings

| Code | Slug | Severity | Change |
|---|---|---|---|
| KGWUI-002 | condition-false | error (unchanged) | Now also covers child routes' parent entries (§4.3) and Backend `Accepted` (below). |
| KGWUI-003 | route-unattached | info / warning (unchanged) | Also fires for an explicit parentRef to an HTTPRoute that does not exist. |
| KGWUI-004 | namespace-not-watched | warning (unchanged) | Also fires for a child in an unwatched namespace that names an owned route. |
| **KGWUI-011** | **delegated-route-not-reported** | info inside the grace period, warning after | **New.** An explicit child (§4.3) has no current status entry for a parent HTTPRoute of this install. |

**KGWUI-011** replaces KGWUI-003 for this case. "Not yet processed" would mislead here: the controller has processed the parent, it just did not report the child.
- Message draft: "Parent route does not list this route"
- Detail draft: "No status from HTTPRoute `shop/api`. The parent may not select this route, or none of its rules fit the parent's match."
- Inside the grace period: "Waiting for the parent route to report this route" (info), and the resource-status check is pending, as for KGWUI-003.
- The hint names possible causes in words. It never computes which one applies (§5.3).

**Backend `Accepted`.**
- On kgateway 2.4 the condition carries `observedGeneration` (`v2.4.7 pkg/reports/status.go:410-415`). It is **optional**: `False` (reason `Invalid`) is an error under KGWUI-002, absent gives no finding, and stale follows Phase 1's rule.
- On kgateway 2.3 the condition has no `observedGeneration` (`v2.3.11 pkg/kgateway/extensions2/pluginutils/status.go:10-40`, written by `pkg/kgateway/extensions2/plugins/backend/status.go:51-71`). Phase 1's rule would call every 2.3 Backend stale, giving a false "not yet processed" warning on each one. So on 2.3, and when the controller version is unknown, a Backend `Accepted=False` is a **warning** whose detail says the controller does not report which spec version it describes. `True` and absent give no finding. **(our call; §15 Q11)**
- The version gate lives next to `kinds.ExperimentalOnly` (`pkg/kinds/upstream.go:39-94`) as a status-capability table, not in the check.

**Experimental hint.** XListenerSet pages and rows reuse D-018's hint when the object has no status: "The controller's experimental Gateway API features may be off."

**Roll-up.** Unchanged (design §5.3). New findings flow into the same strip, findings page and install health. Clusters that use delegation may change colour after upgrade, because children are now judged. The release notes say so.

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

- Detail paths carry group and kind where kinds can share a name. Gateway has one kind, so its path is shorter. **(our call)**
- The ListenerSets list is not in the roadmap text. It is added because 2,000 ListenerSets need a server-side list too. **(our call)**
- `/api/v1/meta` gains an optional `views` field: the group/kinds that have a detail page (`Gateway`, every `listenerset` kind, `HTTPRoute`). The SPA links detail pages only for these, as it links YAML only for `kinds` today (`web/packages/shell/src/api/hooks.ts:19-22`).

### 7.2 Query parameters

| Param | Lists | Values |
|---|---|---|
| `namespace` | all | exact namespace; repeatable (OR) |
| `kind` | routes | `group/Kind`; repeatable |
| `parent` | routes | `group/Kind/namespace/name` of a Gateway, ListenerSet or HTTPRoute. A Gateway includes routes attached through its ListenerSets. |
| `section` | routes | listener name; needs `parent`. Matches parentRefs with that `sectionName`. |
| `attachment` | routes | with `parent`: `accepted`, `not-accepted`, `pending`, `unwatched` |
| `state` | routes | a §6.1 flag; repeatable (OR) |
| `health` | all | `error`, `warning`, `info`, `healthy`; repeatable |
| `q` | all | case-insensitive substring of namespace/name; for routes also hostnames |
| `sort` | all | `name` (default: namespace, then name), `kind`, `health`, and for Gateways `routes`; suffix `.asc` or `.desc` |
| `limit` | all | 1–200, default 50 |
| `cursor` | all | an opaque `next` or `prev` value from a previous response |

**Errors.** Every error uses Phase 1's `ErrorResponse` body (`pkg/api/types.go:64-67`).
- 400: unknown param value, `limit` out of range, `section` without `parent`, a malformed cursor, or a cursor from a different sort. The SPA drops the cursor and reloads page one.
- 404: unknown install, or a detail object that is absent, not owned, or denied. Denied and absent look the same (D-013).
- 503: cache not synced, as today (`handlers.go:69-77`).

### 7.3 DTOs

Field names are the wire names. `?` marks optional fields. Every time is UTC (D-017).

```jsonc
// Shared
Condition       { type, status, reason, message, observedGeneration, lastTransitionTime, stale }
ParentStatus    { parentRef: ParentRef, controllerName, ours, conditions: Condition[] }  // ours: from this install's controller
ParentSummary   { ref: Ref, sectionName?, port?, via?: Ref,          // via: the ListenerSet a Gateway was reached through
                  attachment: "accepted"|"not-accepted"|"pending"|"unwatched", reason? }
Page<T>         { install, items: T[], total, hidden, next?, prev?, collectedAt }
                // hidden: rows matching the filters that the caller may not view

// Lists
RouteRow        { ref: Ref, hostnames: string[], parents: ParentSummary[], parentRoutes: Ref[],
                  children: number, flags: string[], health: State, detail: boolean }
GatewayRow2     { ref, class, health, listeners, attachedRoutes /*status sum*/, routes /*accepted, UI*/,
                  notAccepted, listenerSets, addresses: string[] }
ListenerSetRow  { ref, parent: Ref, health, listeners, acceptedReason?, routes }
RouteList = Page<RouteRow>; GatewayList = Page<GatewayRow2>; ListenerSetList = Page<ListenerSetRow>

// Details
ListenerDTO     { name, port?, protocol, hostname?, tls?: { mode, certificateRefs: Ref[] },
                  allowedRoutes: { namespaces: { from, selector? }, kinds: GroupKind[] },
                  status?: { attachedRoutes, supportedKinds: GroupKind[], conditions: Condition[] },
                  namedBy: number }                                  // routes naming it by sectionName
GatewayDetail   { install, ref, generation, class, conditions: Condition[], addresses: {type?, value}[],
                  listeners: ListenerDTO[], attachedListenerSets?, listenerSets: ListenerSetRow[],
                  hiddenListenerSets, routeCounts: { accepted, notAccepted, pending, unwatched }, collectedAt }
ListenerSetDetail { install, ref, generation, parent: Ref, parentFound, conditions, listeners: ListenerDTO[],
                  experimentalHint?, routeCounts, collectedAt }
RouteDetail     { install, ref, generation, hostnames: string[], annotations: { inheritParentMatcher? },
                  parentRefs: ParentRef[], parents: ParentStatus[], rules: RuleDTO[],
                  ancestors: { path: Ref[], cycle: boolean }[], moreAncestors, childCount, collectedAt }
RuleDTO         { index, name?, matches: MatchDTO[], filters: FilterDTO[], backendRefs: BackendRefDTO[] }
BackendRefDTO   { ref: Ref, port?, weight, filters: FilterDTO[],
                  delegation?: { mode, namespace, name?, label?, state?: "reported"|"not-reported"|"not-found" },
                  backend?: { accepted?: Condition } }               // for kind Backend only
```

- `GatewayRow2` is a new type. Phase 1's overview `GatewayRow` stays exactly as it is (`pkg/overview/overview.go:73-80`). The final Go name is the plan's choice.
- `delegation.state` is set only for `name` refs (§5.2).
- A route page lists children through `GET …/routes?parent=gateway.networking.k8s.io/HTTPRoute/{ns}/{name}`, so the same paged list code serves Gateways, ListenerSets and parents.

### 7.4 Authorizer

Every new handler goes through the `Authorizer` (`pkg/api/authz.go:12-15`):
- A denied detail object returns 404.
- List rows and nested rows (ListenerSets on a Gateway, children, routes) are filtered per ref **before** paging. `total` counts what the caller sees; `hidden` counts what was removed. Identities of hidden rows are not sent. **(our call)**
- An ancestor the caller may not view stays in the up-chain as "A parent you cannot view", so the chain keeps its length.
- Health and flags still cover every object. A hidden row can still turn a Gateway's health red, as in Phase 1 (design §7.1).
- `AllowAll` skips per-row calls (`authz.go:30-37`).

### 7.5 Example

```http
GET /api/v1/installs/kgw.kgateway/routes?parent=gateway.networking.k8s.io/Gateway/infra/edge&attachment=not-accepted&limit=2
```
```json
{
  "install": "kgw.kgateway",
  "items": [
    {"ref": {"group": "gateway.networking.k8s.io", "kind": "HTTPRoute", "namespace": "shop", "name": "cart"},
     "hostnames": ["shop.example.com"],
     "parents": [{"ref": {"group": "gateway.networking.k8s.io", "kind": "Gateway", "namespace": "infra", "name": "edge"},
                  "sectionName": "https", "attachment": "not-accepted", "reason": "NoMatchingListenerHostname"}],
     "parentRoutes": [], "children": 0, "flags": ["not-accepted"], "health": "error", "detail": true}
  ],
  "total": 1, "hidden": 0,
  "collectedAt": "2026-10-21T09:12:03Z"
}
```

### 7.6 Goldens

New golden fixtures in `pkg/api/testdata/golden`: `routes.json`, `routes-filtered.json`, `gateways.json`, `listenersets.json`, `gateway-detail.json`, `listenerset-detail.json`, `route-detail.json` (with delegation, a cycle and a Backend ref), `bad-request.json` and a restricted variant. The TypeScript golden test (`web/packages/shell/src/api/types.golden.test.ts`) imports each one. Existing goldens must not change, except `meta.json` gaining the optional `views` field and the Backend kind.

## 8. Frontend

### 8.1 Pages and URLs

| URL | Page |
|---|---|
| `/i/:id/gateways` | Gateways list |
| `/i/:id/gateways/:ns/:name` | Gateway detail |
| `/i/:id/listenersets` | ListenerSets list |
| `/i/:id/listenersets/:group/:kind/:ns/:name` | ListenerSet detail |
| `/i/:id/routes` | Routes list |
| `/i/:id/routes/:group/:kind/:ns/:name` | HTTPRoute detail |
| `/i/:id/r/:group/:kind/:namespace/:name` | YAML view (unchanged; still served for every linkable kind) |

- Detail pages have two tabs, **Overview** and **YAML**, held in `?tab=yaml`. The YAML tab reuses the YAML view's body (`web/packages/shell/src/pages/ResourcePage.tsx:135-192`) and its polling and `Collected-At` handling.
- The header gains install-scoped nav entries: Overview, Gateways, Routes, ListenerSets, Findings.
- List filters, sort and cursor live in the query string, like the Gateway table today (`pages/install/GatewayTable.tsx:221-260`).
- Routes of other kinds link to the YAML view. The link choice comes from `/meta` `views` (§7.1).

### 8.2 Extension contract

- `CorePages` (`shell/extension.ts:49-54`) gains **optional** slots: `GatewaysPage`, `GatewayPage`, `ListenerSetsPage`, `ListenerSetPage`, `RoutesPage`, `RoutePage`. A missing slot falls back to the upstream page, so a downstream app that passes today's four still compiles and gets the new pages.
- The core reserves the paths `gateways`, `listenersets` and `routes` under `/i/:id/`. The doc on `RouteContribution` (`extension.ts:14-22`) lists them, and a test proves a contribution cannot replace them.
- New exports: `objectPath(installId, ref, meta)` (detail page when listed in `views`, else the YAML view), the list hooks and DTO types. `resourcePath` stays and keeps meaning the YAML view (`pages/install/FindingRow.tsx:76-79`).
- `Vocabulary` gains `getCommand(ref, resource)`, returning the copy text or `undefined` to hide the button. The default is `kubectl get <resource>.<group> -n <ns> <name> -o yaml`; the plural and group come from `/meta`, so two CRDs with one kind name never clash. A platform without kubectl overrides or hides it (D-016 H4).
- `SHELL_API_VERSION` stays **1**. Every change is additive, and no release has frozen the contract yet (phase-1-complete.md). **(our call)**

### 8.3 Finding links retarget

`FindingRow` (`FindingRow.tsx:34,42`), the Gateway table row (`GatewayTable.tsx:192,265`) and the top findings use `objectPath`. A Gateway, ListenerSet or HTTPRoute finding opens its detail page; anything else opens the YAML view as before. A restricted finding still never links.

### 8.4 Page content

**Gateway detail.** Header: kind, name, health badge, data age, copy buttons. Then:
1. Conditions: one compact row per condition, with status icon + text, reason, message and freshness.
2. Listeners: name, port, protocol, hostname (or "Any hostname"), TLS mode, certificate names (mono, not links), allowed namespaces and kinds, status `attachedRoutes`, "named by N routes", conditions on expand.
3. ListenerSets: rows with health and accepted reason; "Attached ListenerSets (status): N" next to it.
4. Attached routes (paged), with a listener column (§3.4).
5. Routes that failed to attach (paged): reason and message per row. Sub-filters: Not accepted, Waiting for status, Unwatched namespace.

**HTTPRoute detail.**
1. Up-chain breadcrumb (§5.2).
2. Hostnames, or "Inherits the listener's hostnames", or for a child "Inherits the parent route's hostnames".
3. Parents: one card per parentRef. It shows the target link, section or port, and every condition. Entries from other controllers are collapsed under "Status from other controllers".
4. Rules: one block per rule. It shows matches (path type and value, headers, query params, method), filters (type plus the config as written; ExtensionRef as a link when linkable) and backendRefs (kind, namespace/name, port, weight). A delegation ref shows its mode, and a Backend shows its `Accepted` state.
5. Child routes (paged), when the route has delegation refs or children.

**ListenerSet detail.** Parent Gateway link (or "Parent Gateway not found"), conditions, a listener table like the Gateway's, and attached routes.

### 8.5 States

| State | Behaviour |
|---|---|
| Loading | Skeleton rows at the final row height, and fixed-height header and tabs, so nothing shifts when data lands |
| Empty | Designed per table: "No routes are attached to this Gateway." "No routes match these filters." with a Clear filters action |
| Error after data | Keep the data, show `PageNotice` above it (`ResourcePage.tsx:174` pattern) |
| 404 | "Not in this install's scope", with a link back (Phase 1 copy) |
| 400 cursor | Drop the cursor silently and load page one |
| Stale | `DataAge` turns stale; every health badge on the page shows unknown (design §5.4) |
| Hidden rows | A muted line under the table: "N more you cannot view" |
| Install switch | Render data only when the response's `install` matches the URL (design §6.6) |

### 8.6 Interaction, layout and accessibility

- Every link, button, tab, chip, row and menu item has hover, active and focus-visible styles, in both themes.
- Tables keep fixed column widths from the first render. Long names wrap inside their cell (`overflow-wrap: anywhere`).
- Table rows are keyboard-navigable, as in the Gateway table. Sortable headers carry `aria-sort`.
- Tabs follow the WAI-ARIA tabs pattern. Expanders carry `aria-expanded`. The up-chain is a `nav` with `aria-label="Route parents"`.
- Condition state is always icon + colour + text. Contrast is checked in both themes by the existing token test.
- Below 900 px, detail sections stack and tables drop secondary columns into an expandable row. No horizontal page scroll at 720 px.
- Polling every 5 s never resets scroll, focus or expanded state.

### 8.7 Wording pass

All new copy goes through the wording pass: short sentences, plain English, active voice. Per string, a sub-agent picks the least machine-sounding of five accurate options. Strings in scope: page and section titles, empty states, filter and flag labels, attachment labels, the KGWUI-011 message and detail, the delegation states of §5.2, the Backend freshness detail, the hidden-rows line and the copy-button labels. Drafts in this document are placeholders.

## 9. Testing

The primary boundary stays **cluster objects in → API JSON out** (design §9). Every claimed behaviour gets a test at that boundary.

### 9.1 API boundary tests (Go)

Seed the fake dynamic client, run the real cache, install, health, explore and API packages, and call the handlers.
- **Lists:** each filter alone and combined. Sort by each key both ways. Cursor next and prev across a page boundary. A route added in front of the cursor between two computes does not shift the next page. Malformed cursor, cursor from another sort, `limit=0` and `limit=201` all return 400. `section` without `parent` returns 400.
- **Attachment:** a route with `sectionName`, `port`, and neither. A route attached through a ListenerSet shows `via`. A route with `Accepted=False` and each of the three reasons. A pending route. An unwatched route in `attachment=unwatched`.
- **Stale status:** a stale `Accepted=True` is not `accepted`, and a stale `False` is not `not-accepted`.
- **Delegation:** a name child (explicit parentRef), a name child without parentRefs (observed), a wildcard parent, a label parent, a three-level chain, a diamond (one child, two paths), a cycle (`RefNotPermitted` shown, the walk stops, `cycle: true`), and a parent with refs and no reporting child.
- **Delegation findings:** an explicit child with no status for its parent gets KGWUI-011 (info in grace, warning after). A child with hostnames and `UnsupportedValue` gets KGWUI-002. A child in an unwatched namespace gets KGWUI-004. An explicit parentRef to a missing HTTPRoute gets KGWUI-003. An HTTPRoute with no parent lands in `state=no-parent`, uncounted, with no finding.
- **Version shapes:** the 2.3 synthesised child parentRef and the 2.4 as-written one resolve to the same parent.
- **Isolation:** a child whose status comes from another install's controller is not owned here. A child owned through two parents in two installs appears in both.
- **Backend:** a referenced Backend is owned and counted; an unreferenced one is not. `Accepted=False` with `observedGeneration` on a 2.4 install is an error. The same without it on a 2.3 install is a warning. On 2.3, no false "not yet processed".
- **ListenerSet:** `Accepted=True/ListenersNotValid` with one failed listener; `Accepted=False` with no listener entries; an XListenerSet without status shows the experimental hint; a ListenerSet whose parent Gateway is missing.
- **Authorizer:** a deny-some authorizer hides list rows, nested ListenerSets and children; `total` and `hidden` add up; a denied detail returns 404; a denied ancestor shows as a placeholder; health and flags are unchanged.
- **Compatibility:** every Phase 1 golden passes unchanged, except `meta.json` (§7.6).
- **Links:** every finding's `objectPath` target resolves through its endpoint.

### 9.2 Goldens

§7.6, checked by Go and by TypeScript.

### 9.3 Frontend (Vitest)

- Filters, sort and cursor round-trip through the URL; a 400 drops the cursor.
- Each state of §8.5, including stale turning every badge unknown with fake timers.
- An install switch never shows the previous install's rows.
- `objectPath` picks detail or YAML from `views`; restricted findings never link.
- `CorePages` with only the four Phase 1 slots still renders the new pages.
- A contribution at `routes` cannot replace the core page.
- `getCommand` returning `undefined` hides the copy button.
- Skeletons and loaded rows have the same height (layout shift).

### 9.4 Playwright and ui-loop evidence

Each frontend task runs through the ui-loop with video and screenshots in both themes and at 1440 px and 900 px. Every flow of §2 is recorded against the kind fixture. The e2e smoke test adds the Gateway page, the HTTPRoute page with a delegation tree, a filtered Routes list deep link and the YAML tab.

### 9.5 kind e2e

New fixtures, run on kgateway 2.3.11 and 2.4.7 in the existing matrix:
- a Gateway with three listeners;
- routes rejected with `NotAllowedByListeners`, `NoMatchingParent` and `NoMatchingListenerHostname`;
- a ListenerSet with one invalid listener;
- a delegation tree: a name child, a label child and a wildcard parent;
- a child whose header match does not fit the parent (dropped, no status);
- a child with hostnames;
- a cycle;
- a static `Backend` and an invalid one;
- a replaced rule.
The API assertions are version-aware where §5.1 and §6.2 say the versions differ.

**First plan task (cluster observation, like Phase 1's D-012).** On both versions, record:
1. whether a child's status entry is removed when the parent stops selecting it (§4.3);
2. whether a 2.3 child with a namespace-less parentRef gets status (§5.1);
3. Backend `observedGeneration` (§6.2);
4. whether listener `attachedRoutes` counts delegated children or ListenerSet routes.
The result becomes a decision entry. The design is adjusted before the backend tasks that depend on it.

### 9.6 Benchmarks at the scale target

A fixture generator builds one install with 500 Gateways (4 listeners each), 2,000 ListenerSets, 5,000 HTTPRoutes with 20,000 parentRefs, 500 delegated children in trees up to 4 deep, 1,000 Backends and realistic status. The Go benchmarks run with `-benchmem`:

| Benchmark | Target |
|---|---|
| `BenchmarkCompute` (whole model, warm decode cache) | the index adds ≤ 10 % to Phase 1's compute at the same size |
| `BenchmarkCompute` (cold, first compute) | reported, no target |
| `BenchmarkExploreBuild` | reported, as a share of compute |
| `BenchmarkRoutesList` per filter mix (none, parent, namespace+state, `q`, `sort=health`) | p95 < 100 ms per request through the HTTP handler |
| The same with `b.RunParallel` (20 concurrent clients) | p95 < 100 ms |
| Heap delta of the index plus the decode cache | reported per object kind |

Numbers are measured and written into the sub-phase record, with the machine and Go version. Nothing is estimated. A target that fails blocks the sub-phase.

### 9.7 Platform neutrality test

`explore.Build` runs in a test over a snapshot with no Namespaces, Pods or Deployments, and objects without `uid` or `resourceVersion`. It produces the same rows; the decode cache is bypassed.

## 10. Platform neutrality (D-016)

| Addition | How it stays neutral |
|---|---|
| `pkg/explore` | Reads objects only through `pkg/objects` and unstructured. It imports no `corev1`, `appsv1` or client-go; the import boundary test covers the new files (`internal/boundary/imports_test.go`). |
| Assignment changes | Stay in `pkg/install/assign.go`, already on the boundary allowlist. No new typed imports. |
| Decode cache | Keyed by `uid` and `resourceVersion`, which file mode synthesises (design §14.8). Missing keys bypass it. |
| `Backend` | A kgateway CRD, available on every platform. It is what Phase 4 builds on without Services. |
| Service backendRefs | Shown as written. Services are never watched or looked up. |
| Copy command | Platform copy through `Vocabulary.getCommand`; a platform can hide it. |
| URLs and IDs | Install IDs stay opaque and encoded (design §14.3 H1). Object paths use group, kind, namespace and name, which every provider supplies. |
| Namespace filter | Namespaces are part of object identity. kube-lite has them, and file mode synthesises them (design §14.8). |
| Readiness | No proxy or Pod data on the new pages. Gateway health comes from findings, which already respect capabilities. |

## 11. Security

- **No Secret reads.** The ClusterRole stays without Secrets (D-013). Listener `certificateRefs` are shown as `kind namespace/name` text. They are never fetched, resolved or linked; `isLinkable` already refuses kinds outside `/meta` (`hooks.ts:19-22`).
- **No TLS options.** `tls.options` is not decoded for the Overview tab (§4.1); the YAML tab shows the object as stored, as today.
- **Authorizer everywhere** (§7.4). Denied and absent look the same. Hidden rows are counted, never named.
- **No new reach.** No admin server, no Envoy admin, no new RBAC beyond the `Backend` watch already covered by `gateway.kgateway.dev: *`.
- **Input handling.** Query params are validated against fixed sets. Cursors are decoded with a size cap (1 KiB) and checked against the sort. Messages and annotations render as text, never as HTML.
- **Trust model unchanged** (D-013). The new pages show only what the ServiceAccount could already read through the YAML view.

## 12. Cost

This phase generates no data-plane configuration and no xDS churn.

| Item | Cost |
|---|---|
| API server | One more cluster-wide watch (`backends`). No other new requests. |
| Compute (every 2 s) | Ownership BFS over routes and edges, health additions, `explore.Build`. Linear in objects and parentRefs. Measured, §9.6. |
| Decode | Once per object change, not per compute (§3.2). |
| Memory | The index rows, the decode cache and, briefly, the previous Model while it is replaced. Measured and reported per kind, §9.6. |
| Per request | One index lookup, a filter scan, optional sort, one page. No API server calls. |
| Client | One list request per visible table every 5 s, at most 200 rows each. Detail pages poll one detail endpoint plus their visible tables. |

## 13. Sub-phase split (suggestion for the plan)

| Sub-phase | Content |
|---|---|
| 2.1 Backend | Cluster observation task (§9.5). Views and decode cache. Child and Backend assignment. KGWUI-011 and the Backend rules. `pkg/explore` indexes. The API endpoints and goldens. Benchmarks at the scale target, with numbers. |
| 2.2 Frontend | Lists, detail pages, tabs, the up-chain, link retargeting, copy helpers, the `CorePages` slots, the wording pass, Vitest, ui-loop evidence. |
| 2.3 e2e and close | kind fixtures and assertions on 2.3 and 2.4, the Playwright additions, the README and chart notes, the release-notes line on changed counts. |

2.1 touches more than 5 files and 400 lines, as does 2.2. The plan states each task's file list for approval (change-size guardrail).

## 14. Upstream asks (draft text only, not filed)

Each needs a check against kgateway `main` before filing. The user decides where they go (§15 Q5).

### 14.1 Report dropped delegated rules and children

> **Delegated child rules dropped without status**
> When a parent HTTPRoute delegates, child rules whose matches do not fit the parent's match are dropped. A child with no remaining rules is dropped too (`pkg/kgateway/translator/httproute/delegation_helpers.go:33-107`). Neither case writes a condition: the child reporter is created only for kept children (`delegation.go:81`). The child shows no status for the parent. The parent shows `ResolvedRefs=False/BackendNotFound` only when no child is left (`delegation.go:98-99`). Operators cannot see why a child is not routing.
> Ask: write a condition on the child under the parent's parentRef when rules are dropped, naming the dropped rule indexes. A child with all rules dropped could get `Accepted=False` with a reason such as `NoMatchingParentRule`. Open question: is a partial drop `Accepted=True` plus `kgateway.dev/Programmed=False/RouteRuleDropped`, matching how replaced rules are reported today?

### 14.2 Report shadowed or duplicate matches

> **Routes shadowed by an identical match get no condition**
> Routes are merged per virtual host and sorted stably; with equal sort keys the oldest route wins (`pkg/kgateway/translator/routeutils/sortable_route.go:120-171`). The losing route stays `Accepted=True` with no signal. gateway-api#4303 tracks the spec side.
> Ask: a `kgateway.dev/` condition (or a reason on `kgateway.dev/Programmed`) on a route whose match is fully shadowed by another route on the same listener and hostname, naming the winner.

### 14.3 Clear a child's status when it stops being delegated (only if §9.5 confirms it)

> A child route keeps its status entry for a parent HTTPRoute after the parent stops selecting it, because no report is built for the child (`pkg/reports/status.go:452-472`). Tools that read status see a stale parent.
> Ask: remove kgateway-owned parent entries from routes the translation no longer reports.

### 14.4 Add delegation settings to the scope endpoint proposal

The pending "effective discovery scope" proposal (phase-status carry-over) could also report the effective `DELEGATION_WILDCARD_NAMESPACE` (`api/labels/delegation.go:12`), so tools can label cluster-wide delegation correctly.

## 15. Open questions for the user

| # | Question | Default this design assumes | If the other answer |
|---|---|---|---|
| Q1 | Policies attached to a route or Gateway (a targetRef index, no merge order): Phase 2 or later? | **Later (Phase 5).** A list without precedence can mislead. | Add a `policies` section to detail pages and a by-target index. About one task more; the page must say "attached, not effective". |
| Q2 | Hostname intersection (listener ∩ route), per Gateway API matching rules: compute it, or show both sides? | **Show both sides only.** kgateway computes it (`v2.4.7 pkg/kgateway/query/query.go:231`) and reports the failure as `NoMatchingListenerHostname`. | An `effectiveHostnames` field per parentRef. It is spec semantics, but it is still a second implementation to pin with tests per version. |
| Q3 | Keep shadowed-match detection and inherited delegation matchers out (upstream asks)? | **Yes, out.** | Each needs a mirror of controller logic behind a pluggable interface, verified per version, with the upstream ask filed. |
| Q4 | A graph view? | **Later.** | A new task in 2.2 and a layout library; the tree and tables stay the primary view. |
| Q5 | Upstream asks §14.1–14.2: add to the pending scope proposal, or file separate issues? | **Separate issues.** They are unrelated to scope. §14.4 joins the scope proposal. | One combined issue is easier to track but harder to triage. |
| Q6 | Scale target: 5,000 HTTPRoutes, 500 Gateways, 2,000 ListenerSets, 20,000 parentRefs per install. Right order of magnitude? | **Yes.** | Benchmarks and the fixture generator change. A 10× target may need an index that updates incrementally instead of per compute. |
| Q7 | Re-run the support-ticket and knowledge-base research before plan approval? | **Yes, before plan approval.** The current research covers support and field threads only. | Approve on the current research; new pain points become Phase 3 inputs. |
| Q8 | Routes per listener: only by `sectionName`/`port`, with "All listeners" otherwise (§3.4)? | **Yes.** | Per-listener attribution means evaluating allowedRoutes and hostnames: a mirror plus an upstream ask for per-listener status. |
| Q9 | Keyset cursors without page numbers (§3.5)? | **Yes.** | Offsets add "page N" links; pages shift as the model changes every 2 s. |
| Q10 | List HTTPRoutes with no parent under a "No parent" filter, uncounted, no finding (§4.3)? | **Yes.** | Either drop them (silent delegation misses stay invisible) or add an info finding per route (noisy where they are intentional). |
| Q11 | Backend `Accepted=False` on 2.3 (no `observedGeneration`) as a warning (§6.2)? | **Yes, warning.** | Error: a stale False after an edit shows red. No finding: an invalid Backend on 2.3 is never flagged. |
| Q12 | Delegated children now count in tiles and health (§4.3). Accept the changed numbers? | **Yes.** Children were invisible, which hid real failures. | Keep counts Phase 1-style and show children only on route pages; their failures then never reach the health strip. |
