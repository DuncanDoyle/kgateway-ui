# Phase 2 — Gateway & Route Explorer: Design

**Status:** Revised after the cross-model review (`reviews/design-codex.md`, 13 findings, all accepted), for user approval
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
   - Each row carries the reason and message of the condition that failed for this parent, so the table needs no extra requests.
   - It also lists routes still waiting for status and routes from namespaces the controller does not watch (KGWUI-004), each with its own label. These open their detail page as diagnostic candidates (§7.1).
   - The route page shows each parentRef with every condition, its reason, message and freshness ("Describes generation 3; the route is at 4").
3. **"Which child routes did delegation pick up?"**
   - Open the parent HTTPRoute. A rule whose backendRef is a delegation ref shows it as written: by name, `*`, or label.
   - **Child routes** lists every route whose controller status names this route as its parent. Only status makes a route a child (§4.3).
   - **Routes that name this parent but are not reported** lists routes whose `spec.parentRefs` name this route while no status from the controller does. They are diagnostic candidates, not children.
   - A delegation ref with no reporting child shows "No child route reports this parent", next to the parent's own `ResolvedRefs` condition.
   - A child row opens the child, whose page shows the chain up to the Gateway.
4. **"Which route is the broken one?"**
   - Open Routes and filter by state, for example "Rules replaced or dropped". The filter covers `RouteRuleReplaced` and `RouteRuleDropped` on both versions (§6.1).
   - The table names each route, its parent, the failing reason and message, and its health. The filter lives in the URL, so the link can go into a support ticket.
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
- `explore.Build` is a pure function of `(install.Resolved, *install.Owned, *cache.Snapshot, health.Report, decoded views)`. Per request, the API only filters and pages what the index holds. The one exception is counts derived from other objects under a non-`AllowAll` Authorizer, which are recounted over the visible objects (§7.4).
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
| By parent route | HTTPRoute ref | rows of the children with a reported edge to it (§4.3) | Child routes, delegation tree |
| Declared candidates | HTTPRoute ref | rows whose spec names it with no reported edge (§4.3) | "Named but not reported" section, KGWUI-011 |
| Parent routes | route ref | the parent HTTPRoutes of its reported edges | bounded up-chain on request (§5.2) |
| By namespace | namespace | row indexes | namespace filter |
| By backend | Backend ref | route row indexes | Backend tile, route → Backend links |
| Condition state | — | a bit set per row (§6.1) | state filter |
| Findings by object | object ref | worst error and warning finding on it | row state (§6.3), health filter |
| Gateway rows | — | `[]GatewayRow` (§7.3) | Gateways list |
| ListenerSet rows | — | `[]ListenerSetRow`, also by parent Gateway | ListenerSets list, Gateway page |
| No-parent rows | — | in-scope HTTPRoutes with no parent (§4.3) | Routes filter `no-parent` |

- The findings map replaces today's per-row scan of all findings (`pkg/overview/overview.go:166-187`), which is linear in findings per Gateway. Row state is computed from it with Phase 1's rules (§6.3).
- Up-chains are not stored. They are computed per detail request with a bounded walk (§5.2), so diamonds cannot blow up the index. **(our call)**
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
- An anchor row that disappears between requests does not break the cursor: the search finds the position where its tuple would sort.
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

Phase 1 assigns a route to an install only through a parentRef that resolves to a Gateway or ListenerSet of that install (`pkg/install/assign.go:153-189`). So a route reached **only** through HTTPRoute parents (or through none) is not owned, counted or health-checked today. A mixed route, with both a Gateway parentRef and an HTTPRoute parentRef, is owned through its Gateway, but its HTTPRoute-parent status is ignored (`assign.go:158-170`). Phase 2 extends assignment. **(our call)**

**A child relationship needs controller status.** In kgateway a child's parentRef only *permits* attachment. The parent selects first, through its backendRef; then attachment is checked, then the matcher filter runs (`v2.4.7 pkg/kgateway/query/route.go:258-268`, `pkg/kgateway/utils/delegation/delegation.go:14-31`, `pkg/kgateway/translator/httproute/delegation_helpers.go:84-102`; same order at `v2.3.11 pkg/kgateway/query/route.go:231-241`). A declaration alone therefore proves nothing. Predicting selection would mirror the controller (D-005, D-006).

**Reported edge.** Route C has a reported edge to HTTPRoute P in install I when all of these hold:
1. C's namespace is in I's scope, and P is owned by I;
2. C has a `status.parents` entry whose `controllerName` ∈ I.controllerNames and whose `parentRef` resolves to P after defaults;
3. **eligibility:** either a current `spec.parentRefs` entry of C equals that status parentRef after defaults (Phase 1's equality rule, design §4.3), or C's `spec.parentRefs` list is **empty**.

The empty-list fallback matches kgateway, which allows implicit attachment only when the whole list is empty (`v2.4.7 pkg/kgateway/utils/delegation/delegation.go:25-31`; `v2.3.11` same file `:25-45`), and whose status builder falls back to report refs only then (`v2.4.7 pkg/reports/status.go:489-496`). A child that moved from P to Q keeps no edge to P: its old P status has no matching spec entry, as Phase 1 ignores removed refs (`pkg/health/checks.go:314`). **(our call)**

**Freshness does not decide the edge.** An eligible status entry keeps the reported edge whether it is current or stale. kgateway stamps route status with the generation of the report it was built from (`v2.4.7 pkg/reports/status.go:475-480`), so right after a child edit its entry describes the previous generation. Ownership and counts stay stable through that window. Freshness then sets the child's state with Phase 1's rules (design §5.2): a stale or `Pending` entry is "being processed" (KGWUI-003 info, check pending, row `unknown`) inside the grace period and "not yet processed" (KGWUI-003 warning) after it. **(our call)**

**Declared candidate.** A current `spec.parentRefs` entry of C that resolves to an HTTPRoute P owned by I, where C's namespace is in I's scope and C has **no eligible status entry** for P at all, current or stale. It is a diagnostic candidate only:
- It is not a child. It never appears in Child routes or the up-chain, and never makes a `name` ref "reported".
- It gets KGWUI-011 (§6.2). It is listed on P's page under "Routes that name this parent but are not reported".
- It is not counted, unless C is owned another way.

**Out-of-scope routes are never candidates.** A route outside I's scope whose parentRef names a route of I gets KGWUI-004 only, as Phase 1's unwatched routes do (`pkg/install/assign.go:181-186`). It never gets KGWUI-011 and never makes the resource-status check pending, since missing status is expected there.

**Ownership rules.**
- A breadth-first pass starts at the Gateway-attached HTTPRoutes and follows **reported edges only**, over a reverse index built once per compute. A visited set stops cycles; diamonds give a child two parents (§5.4). Cost is linear in routes plus edges.
- A child is owned once per install, even with several parents. Its `OwnedObject.Parents` adds every parent route with a reported edge. A mixed route keeps its Gateway parents and gains its reported HTTPRoute parents.
- Owned children count in the HTTPRoute tile and in install health. This changes Phase 1's numbers on clusters that use delegation (§15 Q12).
- A route in a namespace outside I's scope whose parentRef names a route of I gets KGWUI-004, as a route to an unwatched Gateway does today.
- A parentRef to an HTTPRoute that does not exist gets KGWUI-003 (missing parent). HTTPRoute joins the "parent kinds" set (`assign.go:146-151`).
- Two installs that share a controller name each count only edges whose parent they own, as in Phase 1 (design §4.3).

**HTTPRoutes with no parent.** An in-scope HTTPRoute with an empty `spec.parentRefs` and no status from I's controller is a would-be child that nothing reports. Serving contract **(our call; §15 Q10)**:
- listed under the Routes filter `no-parent` in every install whose scope contains it;
- not counted, no finding, because it may be intentional;
- its row opens the YAML view, which serves it (§7.1); it has no detail page;
- the Authorizer applies as for any row.

**Stale reported status.** When a parent stops selecting an implicit child (empty parentRefs), kgateway may leave the child's old status entry in place. `BuildRouteStatus` returns nil for a route with no report (`v2.4.7 pkg/reports/status.go:452-472`), so the syncer likely has nothing to write (unverified). The plan's first task verifies this on both versions (§9.5). If confirmed, the UI cannot tell such an edge apart without mirroring. It then labels implicit edges "Reported by the child's status", and the upstream ask goes in (§14.3).

### 4.4 The `Backend` kind

- **Descriptor:** `gateway.kgateway.dev/Backend`, resource `backends`, version `v1alpha1`, namespaced, a new category `backend`, display name "Backends". It is added to `kinds.Upstream()` (`pkg/kinds/upstream.go:17-34`). RBAC already covers it (`gateway.kgateway.dev: *`, design §8).
- **Assignment:** a Backend belongs to install I when its namespace is in I's scope and a backendRef of a route owned by I resolves to it by full identity (group, kind, namespace defaulting to the route's, name). A Backend nobody references belongs to no install. Its tile counts only referenced Backends, and the tile says "in use". **(our call)**
- **Scope verifier:** the category maps to no `resources_managed` label, so KGWUI-007 ignores it (`pkg/scopeverify/verify.go:141-151`).
- **Resource endpoint:** owned Backends join `lookup` (`pkg/api/handlers.go:255-265`), so the YAML view and finding links resolve. A Backend referenced by an owned route but outside I's scope is a diagnostic candidate (§7.1).
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

The UI compares parentRefs after Gateway API defaults (`objects.go:64-84`). Equal identity after defaults does not prove that status exists, though.

Known 2.3 gap: the synthesised ref drops the child's own `sectionName` and `port` as well as filling in an omitted namespace (`v2.3.11 pkg/kgateway/query/route.go:252-259`). The `v2.4.7` comment says a synthesised ref that differs from the spec "would leave the child without status" (`route.go:294-299`). So on 2.3 an explicit child whose parentRef omits the namespace, or sets a section or port, may get no status at all. The UI then shows it as a declared candidate with KGWUI-011 (§4.3, §6.2). The plan's first task checks status **production** for each case on both tags (§9.5).

### 5.2 The tree the UI shows

The tree is built from §4.3's **reported edges** only. Declared candidates are shown apart from it.

| Parent page shows | Source |
|---|---|
| Each delegation ref, as written (mode, namespace, name or label) | parent spec |
| **Child routes**: every child with a reported edge to this route, paginated | `GET …/routes?parent=…` (§7.2) |
| Each child's status for this parent: `Accepted`, `ResolvedRefs`, programmed state, freshness | child status |
| **Routes that name this parent but are not reported**, paginated | `GET …/routes?parent=…&attachment=declared` |
| The parent's own conditions per Gateway parent | parent status |

| Ref state | Shown as |
|---|---|
| `name` ref, named route has a reported edge to this parent | `reported`: the child row |
| `name` ref, named route exists, no reported edge | `not-reported`: "This route does not report this parent", linked per its `link` (§7.1) |
| `name` ref, no such route | `not-found`: "Route not found" (identity lookup, like a missing Gateway in Phase 1) |
| `name` ref, the caller may not view the named route | `restricted`: no state shown (§7.4) |
| `wildcard` or `label` ref | the ref as written. Children are listed for the parent as a whole, not per ref. |
| Parent has delegation refs and no reported child | "No child route reports this parent", next to the parent's `ResolvedRefs` condition |

The parent's `ResolvedRefs=False/BackendNotFound` message names the failing ref (`v2.4.7 pkg/kgateway/translator/httproute/delegation.go:45-47,98-99`). The UI shows that message verbatim next to the refs.

**N levels.** A child page shows its own delegation refs and children. The tree has no depth limit. Each level is one paginated list, fetched when expanded, so a 500-child wildcard does not load the whole subtree.

**Up the chain.** The route page header shows paths up to a Gateway or ListenerSet: `edge (Gateway) › api (HTTPRoute) › orders (HTTPRoute)`. Repeated diamonds can make the number of paths exponential in the depth, so the walk is bounded **(our call)**:
- It runs per detail request, over the "parent routes" index (reported edges only), depth-first in a stable order (parents sorted by namespace, name).
- It stops after 5 complete paths, at depth 32, or after visiting 256 route nodes, whichever comes first.
- A path that meets a route already on it ends with a `cycle` marker.
- If any bound stopped the walk, the response says `truncated: true`. It never claims an exact count of the remaining paths.
- Cost per request is at most 256 node visits, whatever the graph shape.

### 5.3 What is not shown, and why

| Not shown | Why |
|---|---|
| Which routes a `wildcard` or `label` ref selects | Evaluating selectors predicts the controller. `DELEGATION_WILDCARD_NAMESPACE` is controller env and invisible to the UI (`api/labels/delegation.go:12`). |
| Which ref selected a reported child | Child status names the parent route, not the backendRef. Attribution needs the selector. |
| Child rules dropped for not fitting the parent's match | Dropped with no status (`delegation_helpers.go:33-107`). Upstream ask, §14.1. |
| The effective matcher with `inherit-parent-matcher` | It is a merge the controller does (`delegation_helpers.go:117-142`, annotation `api/annotations/delegation.go:7`). The annotation itself is shown on the child. |
| Inherited policies | Phase 5. |

A child whose rules were all dropped is not reported at all (`delegation.go:49,81`). If nothing else reports the parent, the parent gets `BackendNotFound` (`delegation.go:98-99`). The UI shows both facts. It never claims to know the reason.

### 5.4 Cycles and diamonds

- kgateway tracks visited routes per path, so a diamond (A→B1→C, A→B2→C) is valid (`v2.4.7 pkg/kgateway/query/route.go:240-246`).
- A cycle sets `ResolvedRefs=False/RefNotPermitted` with a "cyclic reference" message on the parent (`route.go:271-276`, `pkg/kgateway/query/utils.go:38-44`; `v2.3.11` `route.go:244-248`, `utils.go:37`).
- The UI's own walks keep a visited set. The ownership pass (§4.3) visits each route once per install, so shared descendants cost nothing extra. The up-chain walk (§5.2) is bounded and marks "Cycle" when a path meets a route again. The condition text comes from the parent's status.

### 5.5 Child hostnames

A child with `spec.hostnames` gets `Accepted=False/UnsupportedValue` (`v2.4.7 pkg/kgateway/translator/httproute/delegation.go:83-91,104-110`; `v2.3.11 …/delegation.go:94`). Children use the parent's hostnames (`v2.4.7 delegation.go:51-53`). The child page says "Inherits the parent route's hostnames", and the condition shows as a KGWUI-002 error once the child is owned (§4.3).

The hostname check runs **after** the matcher filter (`v2.4.7 delegation.go:49-83`; `v2.3.11 delegation.go:51-90`). A hostname-bearing child whose rules were all filtered out gets no report at all, so it never reaches `UnsupportedValue`. It shows as a declared candidate (if it names the parent) or as a no-parent row. §9.5 tests this on both tags.

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
| `declared` | a parentRef to an owned HTTPRoute with no reported edge (§4.3) |
| `no-parent` | §4.3, HTTPRoutes with no parent |

- `kgateway.dev/Programmed` exists on `v2.4.7` (`api/conditions/conditions.go:7`) and not on `v2.3.11`. On 2.3 the replaced and dropped reasons arrive through `Accepted=False` (design §10). The reason names are the same on both (`pkg/pluginsdk/reporter/types.go:22-27` at both tags).
- Reasons are shown as raw strings and never mapped to a fixed enum, because reason sets differ across specs and versions (research, gateway-api#4760).

### 6.2 Findings

| Code | Slug | Severity | Change |
|---|---|---|---|
| KGWUI-002 | condition-false | error (unchanged) | Now also covers the status entries of reported edges (§4.3) and Backend `Accepted` (below). |
| KGWUI-003 | route-unattached | info / warning (unchanged) | Also fires for a parentRef to an HTTPRoute that does not exist. |
| KGWUI-004 | namespace-not-watched | warning (unchanged) | Also fires for a route in an unwatched namespace that names an owned route. |
| **KGWUI-011** | **delegated-route-not-reported** | info inside the grace period, warning after | **New.** A declared candidate (§4.3): an in-scope route's parentRef names an owned HTTPRoute, and it has no eligible status entry for it from this install's controller, current or stale. |

**KGWUI-011** replaces KGWUI-003 for this case. "Not yet processed" would mislead here: the controller has processed the parent, it just did not report the child. The finding references the candidate route, which makes it servable (§7.1). The grace period runs from the later of the candidate's and the parent's current generation, since editing either can change the outcome. **(our call)**
- An eligible but stale entry is not KGWUI-011. It keeps the edge and follows Phase 1's freshness rules (KGWUI-003 "being processed" or "not yet processed", §4.3).
- A route outside the install's scope never gets KGWUI-011. It gets KGWUI-004 only, and it never makes the resource-status check pending.
- Message draft: "Parent route does not list this route"
- Detail draft: "No status from HTTPRoute `shop/api`. The parent may not select this route, or none of its rules fit the parent's match."
- Inside the grace period: "Waiting for the parent route to report this route" (info), and the resource-status check is pending, as for KGWUI-003.
- The hint names possible causes in words. It never computes which one applies (§5.3).

**Backend `Accepted`.** kgateway 2.4 stamps `observedGeneration` on the condition (`v2.4.7 pkg/reports/status.go:410-415`); kgateway 2.3 does not (`v2.3.11 pkg/kgateway/extensions2/pluginutils/status.go:10-40`, written by `pkg/kgateway/extensions2/plugins/backend/status.go:51-71`). The rule reads the condition's own evidence, not the controller version **(our call; §15 Q11)**:

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
- A Gateway, ListenerSet or parent route badge keeps Phase 1 semantics: only findings on that object. A Gateway with broken routes is not shown red for them. Its page shows the failed routes in their own table. **(our call)**
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

- Detail paths carry group and kind where kinds can share a name. Gateway has one kind, so its path is shorter. **(our call)**

**Admission.** Finding links and spec links must not end in a 404 **(our call)**:

| Object | Detail endpoint | Resource (YAML) endpoint |
|---|---|---|
| Owned by the install | yes | yes (today) |
| Referenced by a current finding of the install | yes, as a diagnostic candidate | yes (today, `pkg/api/handlers.go:266-285`) |
| Named by a `parentRef` or `backendRef` of an owned object, by full identity, of a served kind | yes, as a diagnostic candidate | yes (**new**) |
| A `no-parent` row of the install (§4.3) | no; its row opens the YAML view | yes (**new**) |
| Anything else | 404 | 404 |

- A diagnostic candidate's detail says so in a banner: "Not part of this install. Shown because …" with the reason (a finding, or the owned object that names it). It shows its status as written, with entries from this install's controller first.
- Examples: an unwatched route (KGWUI-004), a route whose parent is missing (KGWUI-003), a declared candidate (KGWUI-011), and a Backend outside the scope that an owned route names.
- A ListenerSet whose parent Gateway is missing is never assigned (`pkg/install/assign.go:136-139`), and nothing owned names it. It stays out of the install, as in Phase 1. A route naming it gets KGWUI-003, which makes the route servable.
- The resource endpoint change is additive: it serves a superset of what it serves today, and every denial is still a 404 (D-013).
- The ListenerSets list is not in the roadmap text. It is added because 2,000 ListenerSets need a server-side list too. **(our call)**
- `/api/v1/meta` gains an optional `views` field: the group/kinds that have a detail page (`Gateway`, every `listenerset` kind, `HTTPRoute`). It is informational. The SPA does not derive links from it.

**Link metadata.** One function, `admit(install, caller, ref) → detail | yaml | none`, implements the admission table and the Authorizer. The detail and resource endpoints call it to answer, and every payload calls it to label each reference it renders **(our call)**:
- Every reference in a list or detail body is a `LinkedRef`: the ref plus its `link`. This covers row refs, parents, `via`, parent routes, ancestor steps, backendRefs, delegation `name` targets, ExtensionRef filters, the ListenerSet's parent Gateway and the candidate's `by`.
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
| `section` | routes | listener name; needs `parent`. Matches parentRefs with that `sectionName`. |
| `attachment` | routes | with `parent`: `accepted`, `not-accepted`, `pending`, `unwatched`, and for an HTTPRoute parent `declared` |
| `state` | routes | a §6.1 flag; repeatable (OR) |
| `health` | all | `error`, `unknown`, `warning`, `healthy`; repeatable |
| `q` | all | case-insensitive substring of namespace/name; for routes also hostnames |
| `sort` | all | `name` (default: namespace, then name), `kind`, `health`, and for Gateways `routes`; suffix `.asc` or `.desc` |
| `limit` | all | 1–200, default 50 |
| `cursor` | all | an opaque `next` or `prev` value from a previous response |

**Errors.** Every error uses Phase 1's `ErrorResponse` body (`pkg/api/types.go:64-67`).
- 400: unknown param value, `limit` out of range, `section` without `parent`, a malformed cursor, or a cursor from a different sort. The SPA drops the cursor and reloads page one.
- 404: unknown install, or a detail object that is absent, not admitted (§7.1) or denied. Denied and absent look the same (D-013).
- 503: cache not synced, as today (`handlers.go:69-77`).

### 7.3 DTOs

Field names are the wire names. `?` marks optional fields. Every time is UTC (D-017).

```jsonc
// Shared
Link            "detail" | "yaml" | "none"                           // from admit(), §7.1
LinkedRef       { group, kind, namespace?, name, link: Link }        // every rendered reference
Condition       { type, status, reason, message, observedGeneration, lastTransitionTime, stale }
ParentStatus    { parentRef: ParentRef, target: LinkedRef, controllerName, ours, conditions: Condition[] }
                // ours: from this install's controller
ParentSummary   { ref: LinkedRef, sectionName?, port?,
                  via?: LinkedRef, viaRestricted?: true,             // via: the ListenerSet a Gateway was reached through
                  attachment: "accepted"|"not-accepted"|"pending"|"unwatched"|"declared",
                  conditionType?, reason?, message? }                // the deciding condition for this parent
Page<T>         { install, items: T[], total, next?, prev?, deniedInInstall?, collectedAt }
                // deniedInInstall: objects of the listed kind(s) in the whole install that the caller
                // may not view; independent of every filter; absent under AllowAll

// Lists
RouteRow        { ref: LinkedRef, hostnames: string[], parents: ParentSummary[], parentRoutes: LinkedRef[],
                  children: number, flags: string[], health: State }
GatewayRow2     { ref: LinkedRef, class, health, listeners, attachedRoutes /*status sum*/, routes /*accepted, UI*/,
                  notAccepted, listenerSets, addresses: string[] }
ListenerSetRow  { ref: LinkedRef, parent: LinkedRef, health, listeners, acceptedReason?, routes }
RouteList = Page<RouteRow>; GatewayList = Page<GatewayRow2>; ListenerSetList = Page<ListenerSetRow>

// Details
ListenerDTO     { name, port?, protocol, hostname?, tls?: { mode, certificateRefs: LinkedRef[] /*link always none*/ },
                  allowedRoutes: { namespaces: { from, selector? }, kinds: GroupKind[] },
                  status?: { attachedRoutes, supportedKinds: GroupKind[], conditions: Condition[] },
                  namedBy: number }                                  // routes naming it by sectionName
GatewayDetail   { install, ref, generation, class, health, candidate?, conditions: Condition[],
                  addresses: {type?, value}[], listeners: ListenerDTO[], attachedListenerSets?,
                  routeCounts: { accepted, notAccepted, pending, unwatched }, collectedAt }
                // ListenerSets come from GET …/listenersets?parent=… (paged), not from this body
ListenerSetDetail { install, ref, generation, health, candidate?, parent: LinkedRef, parentFound, conditions,
                  listeners: ListenerDTO[], experimentalHint?, routeCounts, collectedAt }
RouteDetail     { install, ref, generation, health, candidate?, hostnames: string[],
                  annotations: { inheritParentMatcher? }, parentRefs: ParentRef[], parents: ParentStatus[],
                  rules: RuleDTO[], ancestors: AncestorPath[], ancestorsTruncated: boolean,
                  childCount, declaredCount, collectedAt }
AncestorPath    { steps: AncestorStep[], cycle: boolean }
AncestorStep    { ref: LinkedRef } | { restricted: true }            // a restricted step carries no identity
RuleDTO         { index, name?, matches: MatchDTO[], filters: FilterDTO[], backendRefs: BackendRefDTO[] }
FilterDTO       { type, extensionRef?: LinkedRef, config? }
BackendRefDTO   { ref: LinkedRef, port?, weight, filters: FilterDTO[],
                  delegation?: { mode, namespace, name?, label?,
                                 state?: "reported"|"not-reported"|"not-found"|"restricted" },
                  backend?: { state: State, accepted?: Condition } | { restricted: true } }  // kind Backend only
// candidate: { reason: "finding"|"referenced", by?: LinkedRef } — set for a diagnostic candidate (§7.1)
// Finding.resource gains an optional link: Link (additive, §7.1)
```

- `GatewayRow2` is a new type. Phase 1's overview `GatewayRow` stays exactly as it is (`pkg/overview/overview.go:73-80`). The final Go name is the plan's choice.
- `delegation.state` is set only for `name` refs (§5.2).
- A route page lists children through `GET …/routes?parent=gateway.networking.k8s.io/HTTPRoute/{ns}/{name}`, and declared candidates with `&attachment=declared`. The same paged list code serves Gateways, ListenerSets and parents.
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
  - `via` (the ListenerSet a Gateway was reached through; `viaRestricted` otherwise);
  - an up-chain step (`{restricted: true}`, with no kind, namespace or name);
  - derived counts: `routes`, `notAccepted`, `listenerSets`, `routeCounts`, `namedBy`, `children`, `childCount` and `declaredCount` count only visible objects. Under a non-`AllowAll` Authorizer they are recounted per request.
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
                  "reason": "NoMatchingListenerHostname", "message": "no hostname matches the listener"}],
     "parentRoutes": [], "children": 0, "flags": ["not-accepted"], "health": "error"}
  ],
  "total": 1,
  "collectedAt": "2026-10-21T09:12:03Z"
}
```

### 7.6 Goldens

New golden fixtures in `pkg/api/testdata/golden`: `routes.json`, `routes-filtered.json`, `gateways.json`, `listenersets.json`, `gateway-detail.json`, `listenerset-detail.json`, `route-detail.json` (with delegation, a cycle, a truncated up-chain and a Backend ref), `route-detail-candidate.json`, `bad-request.json` and a restricted variant (restricted Backend, ancestor step and `via`, plus `deniedInInstall`). The TypeScript golden test (`web/packages/shell/src/api/types.golden.test.ts`) imports each one. Existing goldens must not change, except two additive ones: `meta.json` gains the optional `views` field and the Backend kind, and `health.json` gains the optional `link` on finding resources.

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
- Every link comes from the reference's server-computed `link` (§7.1): `detail` opens the detail page, `yaml` the YAML view, and `none` renders plain text. Routes of other kinds and `no-parent` rows arrive as `yaml`. The SPA never picks a target from the kind.

### 8.2 Extension contract

- `CorePages` (`shell/extension.ts:49-54`) gains **optional** slots: `GatewaysPage`, `GatewayPage`, `ListenerSetsPage`, `ListenerSetPage`, `RoutesPage`, `RoutePage`. A missing slot falls back to the upstream page, so a downstream app that passes today's four still compiles and gets the new pages.
- The core reserves the paths `gateways`, `listenersets` and `routes` under `/i/:id/`. The doc on `RouteContribution` (`extension.ts:14-22`) lists them, and a test proves a contribution cannot replace them.
- New exports: `objectPath(installId, linkedRef)` (the path for its `link`, or `undefined` for `none`), the list hooks and DTO types. `resourcePath` stays and keeps meaning the YAML view (`pages/install/FindingRow.tsx:76-79`).
- `Vocabulary` gains `getCommand(ref, resource)`, returning the copy text or `undefined` to hide the button. The default is `kubectl get <resource>.<group> -n <ns> <name> -o yaml`; the plural and group come from `/meta`, so two CRDs with one kind name never clash. A platform without kubectl overrides or hides it (D-016 H4).
- `SHELL_API_VERSION` stays **1**. Every change is additive, and no release has frozen the contract yet (phase-1-complete.md). **(our call)**

### 8.3 Finding links retarget

`FindingRow` (`FindingRow.tsx:34,42`), the Gateway table row (`GatewayTable.tsx:192,265`) and the top findings use `objectPath` on the finding's server-computed `link` (§7.1). A Gateway, ListenerSet or HTTPRoute finding gets `detail`; anything else `yaml` as before. The link comes from the same admission function the endpoints use, so it never 404s. A restricted finding still never links.

### 8.4 Page content

All detail pages: a diagnostic candidate shows its banner (§7.1) above the header.

**Gateway detail.** Header: kind, name, health badge, data age, copy buttons. Then:
1. Conditions: one compact row per condition, with status icon + text, reason, message and freshness.
2. Listeners: name, port, protocol, hostname (or "Any hostname"), TLS mode, certificate names (mono, not links), allowed namespaces and kinds, status `attachedRoutes`, "named by N routes", conditions on expand.
3. ListenerSets (paged, from the ListenerSets list): rows with health and accepted reason; "Attached ListenerSets (status): N" next to it.
4. Attached routes (paged), with a listener column (§3.4).
5. Routes that failed to attach (paged): reason and message per row. Sub-filters: Not accepted, Waiting for status, Unwatched namespace.

**HTTPRoute detail.**
1. Up-chain breadcrumb (§5.2). A restricted step reads "A parent you cannot view". A truncated walk ends with "More paths not shown".
2. Hostnames, or "Inherits the listener's hostnames", or for a child "Inherits the parent route's hostnames".
3. Parents: one card per parentRef. It shows the target (linked per its `link`), section or port, and every condition. Entries from other controllers are collapsed under "Status from other controllers".
4. Rules: one block per rule. It shows matches (path type and value, headers, query params, method), filters (type plus the config as written; ExtensionRef linked per its `link`) and backendRefs (kind, namespace/name, port, weight). A delegation ref shows its mode, and a Backend shows its state and `Accepted` condition, or "Restricted" when the caller may not view it.
5. Child routes (paged), when the route has delegation refs or children.
6. Routes that name this parent but are not reported (paged), when there are any.

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
| Denied objects | Once per list page, from `deniedInInstall`: "N routes in this install are hidden from you". Never per filter or per nested table (§7.4). |
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

All new copy goes through the wording pass: short sentences, plain English, active voice. Per string, a sub-agent picks the least machine-sounding of five accurate options. Strings in scope: page and section titles, empty states, filter and flag labels, attachment labels, the KGWUI-011 message and detail, the delegation states of §5.2, the Backend freshness detail, the candidate banner, the restricted placeholders, the denied-objects line and the copy-button labels. Drafts in this document are placeholders.

## 9. Testing

The primary boundary stays **cluster objects in → API JSON out** (design §9). Every claimed behaviour gets a test at that boundary.

### 9.1 API boundary tests (Go)

Seed the fake dynamic client, run the real cache, install, health, explore and API packages, and call the handlers.
- **Lists:** each filter alone and combined. Sort by each key both ways. Malformed cursor, cursor from another sort, `limit=0` and `limit=201` all return 400. `section` without `parent` returns 400.
- **Cursors across recomputes:** next and prev across a page boundary; a route inserted before the anchor; the anchor row deleted; many rows tied on the sort value in descending order; paging backwards from the last page to the first and getting the same rows as forwards; a row whose sort value changes between computes (it moves; no row is skipped that kept its value).
- **Attachment:** a route with `sectionName`, `port`, and neither. A route attached through a ListenerSet shows `via`. A route with `Accepted=False` and each of the three reasons, with the reason and message in its `ParentSummary`. A pending route. An unwatched route in `attachment=unwatched`.
- **Stale status:** a stale `Accepted=True` is not `accepted`, and a stale `False` is not `not-accepted`.
- **Reported vs declared:** a child with a matching spec parentRef and status is a child. A child with a spec parentRef and no status is a declared candidate: not in Child routes, `name` ref `not-reported`, KGWUI-011, not counted. A parent with no delegation refs at all, named by a child's parentRef, gets no children.
- **Stale matching entry:** an explicit child whose matching status entry is stale (its generation bumped) stays a child. It stays owned and counted, with no KGWUI-011. Inside the grace period it gets KGWUI-003 info, the check is pending, and its row is `unknown`; after the grace period it gets KGWUI-003 warning. The counts are the same before and after the edit.
- **Unwatched declared route:** a route outside the scope whose parentRef names an owned HTTPRoute gets exactly one finding, KGWUI-004. It gets no KGWUI-011, the resource-status check stays OK, and install health is unchanged.
- **Implicit fallback:** a child with empty parentRefs and status is a child. A child that moves its parentRef from P to Q while old P status remains has no edge to P, and P's page does not list it. A child that gains a parentRef to a Gateway loses its implicit edge.
- **Delegation shapes:** a wildcard parent, a label parent, a three-level chain, a diamond (one child, two paths), a repeated-diamond lattice (the up-chain stops at 5 paths with `ancestorsTruncated`), a cycle (`RefNotPermitted` shown, `cycle: true`), and a parent with refs and no reported child.
- **Delegation findings:** a declared candidate gets KGWUI-011 (info in grace, warning after). A child with hostnames and `UnsupportedValue` gets KGWUI-002. A route in an unwatched namespace that names an owned route gets KGWUI-004. A parentRef to a missing HTTPRoute gets KGWUI-003. An HTTPRoute with no parent lands in `state=no-parent`, uncounted, with no finding, and its YAML view resolves.
- **Phase 1 → Phase 2 behaviour change (Q12).** Each case asserts counts, findings and roll-up together, before and after the change: a mixed-role route (Gateway and HTTPRoute parents) owned once, now judged on both; a child reported by two parents counted once; two installs that share a controller name, each owning only its own edges; a removed parentRef; a namespace that leaves the scope; a stale implicit edge.
- **Isolation:** a child whose status comes from another install's controller is not owned here. A child reported by parents in two installs appears in both.
- **Backend:** a referenced Backend is owned and counted; an unreferenced one is not. Each row of the §6.2 table: current `False` is an error; current `True` is healthy; stale is unknown with no finding; `observedGeneration` 0 with `False` is a warning; with `True` it is unknown, never healthy; absent is unknown. None of them adds "not yet processed" or turns the install unknown.
- **Row state:** an object with a warning finding while a required check is pending is `unknown`; an info-only object is `healthy`; a Gateway whose routes fail keeps its own state.
- **ListenerSet:** `Accepted=True/ListenersNotValid` with one failed listener; `Accepted=False` with no listener entries; an XListenerSet without status shows the experimental hint; a ListenerSet whose parent Gateway is missing stays out of the install. The Gateway page pages 2,000 ListenerSets.
- **Admission:** every finding resource opens its detail (or YAML) view; an object named by an owned route's parentRef or backendRef is served as a candidate; an unrelated object returns 404 from both endpoints.
- **Link metadata:** for every reference in every golden payload, `link` agrees with the endpoint. `detail` → the detail endpoint returns 200; `yaml` → the resource endpoint returns 200; `none` → both return 404. One test walks every rendered link of a fixture world.
- **Second navigation step:** candidate C (admitted through KGWUI-011) names route D through a delegation backendRef, and nothing else admits D. On C's page, D's ref has `link: none` and both endpoints return 404 for D. When D is owned, the same ref has `link: detail` and resolves.
- **Authorizer:** a deny-some authorizer hides list rows, nested ListenerSets and children. `total` counts only visible rows. `deniedInInstall` stays the same under every filter (a hostname probe changes nothing). A denied Backend gives `{restricted: true}` inside an allowed route, with no condition message. A denied `name`-ref target gives `restricted`. A denied ancestor is `{restricted: true}` with no identity. Derived counts drop denied objects. A denied detail returns 404. Health and flags are unchanged.
- **Compatibility:** every Phase 1 golden passes unchanged, except `meta.json` (§7.6). The delegation cases above cover the numbers that do change.

### 9.2 Goldens

§7.6, checked by Go and by TypeScript.

### 9.3 Frontend (Vitest)

- Filters, sort and cursor round-trip through the URL; a 400 drops the cursor.
- Each state of §8.5, including stale turning every badge unknown with fake timers.
- An install switch never shows the previous install's rows.
- `objectPath` follows `link` only: `detail`, `yaml`, and `none` as plain text, for every kind; restricted findings never link.
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

**First plan task (cluster observation, like Phase 1's D-012).** On 2.3.11 and 2.4.7, record whether status is **produced**, and under which parentRef, for:
1. an implicit child (empty parentRefs) after the parent stops selecting it: is the old entry removed (§4.3)?
2. an explicit child whose parentRef omits the namespace (§5.1);
3. an explicit child whose parentRef sets `sectionName` or `port` (§5.1);
4. an implicit child with empty parentRefs selected by name, wildcard and label;
5. a hostname-bearing child whose rules are all filtered out by the parent's match (§5.5);
6. Backend `observedGeneration` (§6.2);
7. whether listener `attachedRoutes` counts delegated children or ListenerSet routes.
Normalised-identity unit tests cannot answer these. The result becomes a decision entry, and the design is adjusted before the backend tasks that depend on it. The kind e2e then asserts the observed behaviour per tag.

### 9.6 Benchmarks at the scale target

**Graph shape** (the scale target, §15 Q6). A fixture generator builds one install with:
- 500 Gateways with 4 listeners each, 2,000 ListenerSets (4 per Gateway on average);
- 5,000 HTTPRoutes with 20,000 parentRefs (4 per route on average, up to 16);
- 500 delegated children in trees up to 4 deep, with fan-out up to 100 (one wildcard parent);
- shared descendants: 50 children reported by 2–4 parents, and one repeated-diamond lattice 8 layers deep (256 paths to its leaf);
- 1,000 Backends, half referenced;
- realistic status, including 5 % failed parentRefs.

**Absolute budgets** at that shape, on the CI runner class, all **(our call)**:

| Measure | Budget |
|---|---|
| Phase 1 compute | measured first as the baseline; no Phase 1 benchmark exists today |
| Full `Compute`, warm decode cache | ≤ 400 ms per compute (20 % of the 2 s interval); the index adds ≤ 10 % to the baseline |
| Full `Compute`, cold (first compute) | ≤ 2 s |
| Allocation per warm compute | ≤ 64 MiB |
| Sustained run: 10 min of computes every 2 s, with 20 clients polling lists | no compute overruns its tick; GC CPU share reported |
| Heap of the index plus the decode cache | ≤ 64 MiB above the Phase 1 baseline; reported per kind |
| List request through the HTTP handler, per filter mix (none, parent, namespace+state, `q`, `sort=health`, children of the wildcard parent) | p95 < 100 ms, p99 < 200 ms |
| The same with 20 concurrent clients | p95 < 100 ms |
| Route detail on the lattice leaf (bounded up-chain) | p95 < 50 ms |

**Method.** Go benchmarks run with `-benchmem` for allocation and averages. Latency percentiles do not come from `ns/op`. A harness sends 10,000 requests per filter mix through `httptest`, records each duration, and reports p50, p95 and p99 with `b.ReportMetric`. The concurrent run uses 20 goroutines over the same harness. Each run alternates computes in the background, so list latency is measured while the Model swaps.

Numbers are measured and written into the sub-phase record, with the machine and Go version. Nothing is estimated. A budget that fails blocks the sub-phase.

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

- **No Secret reads.** The ClusterRole stays without Secrets (D-013). Listener `certificateRefs` are shown as `kind namespace/name` text. They are never fetched, resolved or linked: their `link` is always `none` (§7.1).
- **No TLS options.** `tls.options` is not decoded for the Overview tab (§4.1); the YAML tab shows the object as stored, as today.
- **Authorizer everywhere** (§7.4). Denied and absent look the same. Every dereferenced object is authorized. Lists never report filtered counts of denied rows; only an unfiltered per-kind count, `deniedInInstall`.
- **Admission is bounded** (§7.1). The detail and resource endpoints serve owned objects, finding-referenced objects, objects named by an owned object's spec, and the install's no-parent rows. Nothing else.
- **No new reach.** No admin server, no Envoy admin, no new RBAC beyond the `Backend` watch already covered by `gateway.kgateway.dev: *`.
- **Input handling.** Query params are validated against fixed sets. Cursors are decoded with a size cap (1 KiB) and checked against the sort. Messages and annotations render as text, never as HTML.
- **Trust model unchanged** (D-013). The new pages show only what the ServiceAccount could already read through the YAML view.

## 12. Cost

This phase generates no data-plane configuration and no xDS churn.

| Item | Cost |
|---|---|
| API server | One more cluster-wide watch (`backends`). No other new requests. |
| Compute (every 2 s) | Ownership BFS over reported edges, health additions, `explore.Build`. Linear in objects and parentRefs. Budgets and measurements in §9.6. |
| Up-chain | Per route detail request, at most 256 node visits (§5.2). Not stored. |
| Decode | Once per object change, not per compute (§3.2). |
| Memory | The index rows, the decode cache and, briefly, the previous Model while it is replaced. Measured and reported per kind, §9.6. |
| Per request | One index lookup, a filter scan, optional sort, one page. No API server calls. |
| Client | One list request per visible table every 5 s, at most 200 rows each. Detail pages poll one detail endpoint plus their visible tables. |

## 13. Sub-phase split (suggestion for the plan)

| Sub-phase | Content |
|---|---|
| 2.1 Backend | Cluster observation task (§9.5). Phase 1 compute baseline benchmark. Views and decode cache. Reported-edge and Backend assignment, declared candidates, admission. KGWUI-011 and the Backend rules. `pkg/explore` indexes. The API endpoints and goldens. Benchmarks at the scale target, with numbers. |
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

> A child route with no parentRefs keeps its status entry for a parent HTTPRoute after the parent stops selecting it, because no report is built for the child (`pkg/reports/status.go:452-472`). Tools that read status see a stale parent.
> Ask: remove kgateway-owned parent entries from routes the translation no longer reports.

### 14.4 Add delegation settings to the scope endpoint proposal

The pending "effective discovery scope" proposal (phase-status carry-over) could also report the effective `DELEGATION_WILDCARD_NAMESPACE` (`api/labels/delegation.go:12`), so tools can label cluster-wide delegation correctly.

## 15. Open questions for the user

| # | Question | Default this design assumes | If the other answer |
|---|---|---|---|
| Q1 | Policies attached to a route or Gateway (a targetRef index, no merge order): Phase 2 or later? | **Later (Phase 5).** A list without precedence can mislead. | Add a `policies` section to detail pages and a by-target index. About one task more; the page must say "attached, not effective". |
| Q2 | Hostname intersection (listener ∩ route), per Gateway API matching rules: compute it, or show both sides? | **Show both sides only.** kgateway computes it (`v2.4.7 pkg/kgateway/query/query.go:231`) and reports the failure as `NoMatchingListenerHostname`. | An `effectiveHostnames` field per parentRef: a second implementation to pin per version. It crosses the no-mirroring boundary (D-005, D-016 §7), so it needs a decision amendment, not just a UX choice. |
| Q3 | Keep shadowed-match detection and inherited delegation matchers out (upstream asks)? | **Yes, out.** | Each needs a mirror of controller logic behind a pluggable interface, verified per version, with the upstream ask filed. Choosing it needs a decision amendment to D-005 / D-016 §7. |
| Q4 | A graph view? | **Later.** | A new task in 2.2 and a layout library; the tree and tables stay the primary view. |
| Q5 | Upstream asks §14.1–14.2: add to the pending scope proposal, or file separate issues? | **Separate issues.** They are unrelated to scope. §14.4 joins the scope proposal. | One combined issue is easier to track but harder to triage. |
| Q6 | Scale target, as counts **and** graph shape (§9.6): 5,000 HTTPRoutes with 20,000 parentRefs (up to 16 per route), 500 Gateways with 4 listeners, 2,000 ListenerSets, 500 delegated children up to 4 deep with fan-out up to 100, shared descendants and one 8-layer diamond lattice. Budgets: warm compute ≤ 400 ms of the 2 s interval, ≤ 64 MiB allocated per compute, ≤ 64 MiB extra heap, list p95 < 100 ms. Right order of magnitude? | **Yes.** | Benchmarks, the fixture generator and the budgets change. A 10× target likely needs an index that updates incrementally instead of per compute. |
| Q7 | Re-run the support-ticket and knowledge-base research before plan approval? | **Yes, before plan approval.** The current research covers support and field threads only. | Approve on the current research; new pain points become Phase 3 inputs. |
| Q8 | Routes per listener: only by `sectionName`/`port`, with "All listeners" otherwise (§3.4)? | **Yes.** | Per-listener attribution means evaluating allowedRoutes and hostnames: a mirror plus an upstream ask for per-listener status. It needs a decision amendment to the no-mirroring boundary. |
| Q9 | Keyset cursors without page numbers (§3.5)? | **Yes.** | Offsets add "page N" links; pages shift as the model changes every 2 s. |
| Q10 | HTTPRoutes with empty parentRefs and no status (§4.3). Serving contract: listed under the `no-parent` filter in every install whose scope holds them; not counted; no finding; the row opens the YAML view, which serves them; no detail page; the Authorizer applies. Accept? | **Yes.** | Either drop them (silent delegation misses stay invisible) or add an info finding per route (noisy where they are intentional, and it makes them findings-admitted for detail pages). |
| Q11 | Backend freshness (§6.2). The rule reads the condition's own `observedGeneration`. Current `False` → error; current `True` → healthy; stale → unknown, no finding; no evidence (2.3, `observedGeneration` 0): `False` → warning, `True` → unknown; absent → unknown. No-evidence states never show green and never turn the install unknown. Accept? | **Yes.** | `False` without evidence as an error: a stale False after an edit shows red. No finding: an invalid 2.3 Backend is never flagged. Treating a no-evidence `True` as healthy breaks the "never green without evidence" rule (D-016 §6). Making no-evidence states turn the install unknown keeps 2.3 installs with Backends unknown forever. |
| Q12 | Delegated children now count in tiles and install health (§4.3). This rests on three rules above. Only **reported** edges make a child; declared refs from in-scope routes stay candidates with KGWUI-011, and out-of-scope ones get KGWUI-004 only. An eligible status entry keeps the edge whether current or stale, so counts stay stable across edits, and freshness only sets the child's pending or unknown state (§4.3). A parent or Gateway badge keeps Phase 1 semantics, while failures show on the child's row and in install health (§6.3). Accept the changed numbers? | **Yes.** Children were invisible, which hid real failures. | Keep counts Phase 1-style and show children only on route pages. Their failures then never reach the health strip. If the declared-vs-reported or row-state rule changes, this default must be re-decided. |
