# Phase 2 — Computed rules specification (D-020)

**Status:** Approved by the user 2026-10-23
**Date:** 2026-10-22
**Rule sets:** `kgateway-2.3` (tag `v2.3.11`, `c2af2887`) and `kgateway-2.4` (tag `v2.4.7`, `b4a2bf74`)
**Design:** `design.md`. This document is the only definition of the computed views (D-020 views A, B and C), their cross-check against the controller and the persistence rules of KGWUI-012. The design refers to it and does not repeat it. Scheduling is defined once in design §3.1, and authorization once in design §7.4.

`V3` is v2.3.11 and `V4` is v2.4.7. Paths are under `pkg/kgateway/` unless they start with `pkg/` or `api/`. A line range written once with "both" is the same at both tags. Choices made without the user are marked **(our call)**; the user's are marked **(user, 2026-10-22)**.

## 1. Principles

1. **One rule set per kgateway minor.** A rule set reproduces what that minor does. A change in kgateway gives a new rule set, never an edit of a shipped one. A new set starts as a copy, plus a diff review of every file cited here between the two tags.
2. **Every result says what it is.** Each result carries its value, the rule applied, the rule set, its **dependencies** (the objects whose data decided it) and its **check** (§8.4). The design's Authorizer uses the dependencies (design §7.4).
3. **Unknown, never a guess.** When an input or a context cannot be resolved, the result is `unknown` with a reason (§3.3). Its value is absent and its cross-check is skipped. A partial answer is never shown as a full one. A bound reduces certainty; it never turns a missing path into a negative conclusion (§5.5, §8.5).
4. **Explain, never own.** Results never decide ownership, counts or install health. They feed two findings only: KGWUI-011's computed trigger and KGWUI-012 (design §6.2).
5. **Staged.** View A ships in sub-phase 2.3; views B and C ship in 2.4 (design §13) **(user, 2026-10-22)**. Each section's tests (§11) gate its sub-phase.

## 2. Rule sets and version selection

| Behaviour | `kgateway-2.3` | `kgateway-2.4` |
|---|---|---|
| Attachment (view A) | same behaviour; `query/route.go` V3:397-508 | `query/route.go` V4:437-547 |
| Selection, permission, matcher fit (views B, C) | same behaviour; line numbers in §5 and §6 | same |
| Report key of a delegated child (§7) | always synthesized | the child's own parentRef, as written |
| Success conditions filled in by default | `Accepted`, `ResolvedRefs` | adds `kgateway.dev/Programmed` (`pkg/reports/status.go` V4:752-780; `api/conditions/conditions.go` absent at V3) |
| Rule replaced or dropped | `Accepted=False`, `RouteRuleReplaced`/`RouteRuleDropped` (`translator/irtranslator/route.go` V3:291-313) | `kgateway.dev/Programmed=False`, same reasons (V4:325-350) |
| TCP or TLS route conflict | `Accepted=False/Conflicted` (`translator/listener/gateway_listener_translator.go` V3:645-656, called at :463) | `kgateway.dev/Programmed=False/Conflicted` (V4:648-660, called at :466) |
| Old status of a dropped or deselected child | after a parent edit (deselection or matcher drop): cleared to `parents: []`, observed (D-021, items 1 and 6). After a child edit: not observed | after a parent edit: cleared to `parents: []` (`pkg/krtcollections/policy.go` V4:1150-1178), observed (D-021, items 1 and 6). After a child edit: not observed |
| Plain-HTTP listener isolation (programming only, §4.5) | none | `translator/listener/gateway_listener_translator.go` V4:689-747 |

**Selection.** The rule set follows the controller minor, as `kinds.ExperimentalOnly` does (`pkg/kinds/upstream.go:55-94`).
- Selection returns `Selection{Rules, Note}` from `pkg/computed/rulesets`. The factory lives there, not in `pkg/computed`, so the rule-set packages can import `pkg/computed` without an import cycle **(our call)**.
- An unknown version uses the newest set, labelled "Controller version unknown; using the 2.4 rules".
- A minor newer than the newest set uses the newest set, labelled "Rules not yet verified for 2.5".
- A controller older than 2.3 uses the 2.3 set, labelled "Rules not yet verified for <major.minor>", for example "2.2" **(our call)**.
- The cross-check runs in both cases. The label is part of every result's check note.

## 3. Inputs

### 3.1 What the rules read

- **Decoded views** of Gateways, ListenerSets and routes (design §4.1). Only routes in namespaces the controller watches are candidates; the controller cannot see others (`pkg/pluginsdk/collections/collections.go` V4:105).
- **Namespace labels** as a map from namespace to labels, with a flag that says whether Namespace access is OK. Never a `corev1` type (design §10).
- **Settings** (design §3.4): `DELEGATION_WILDCARD_NAMESPACE` and `KGW_ENABLE_EXPERIMENTAL_GATEWAY_API_FEATURES`. Each is a known value or unknown.
- **ListenerSet acceptance**, taken from the ListenerSet's own current status: `Accepted` not `False`. It is not computed. A denied ListenerSet is never evaluated by kgateway (`pkg/krtcollections/policy.go` V4:607-622) **(our call)**.
- **Controller status**, read only for the cross-check (§8) and for roots when view A is unknown (§5.1).

### 3.2 Defaults applied when decoding

- parentRef: group and kind default to Gateway; namespace to the route's own (`pkg/krtcollections/policy.go` V4:1247-1274).
- Delegation backendRef: namespace defaults to the parent route's (`pkg/krtcollections/policy.go` V4:1575-1590). Classification follows `translator/backendref/types.go` V4:12-28, V3:12-28 (the file is the same at both tags):
  - **HTTPRoute ref** (`IsHTTPRoute`): `group` is exactly `gateway.networking.k8s.io` **and** `kind` is exactly `HTTPRoute`, both set. A nil group or kind never matches, and an omitted kind means Service. Within this group, `name` `""` or `"*"` is the **wildcard** mode and any other name is the **name** mode (`query/route.go` V4:339, V3:299).
  - **Label selector** (`IsHTTPRouteDelegationLabelSelector`): `group` and `kind` both set, and `group + "/" + kind` equals `delegation.kgateway.dev/label` (`api/labels/delegation.go:7`), so group `delegation.kgateway.dev` and kind `label`. The `name` is the label value.
  - Anything else is an ordinary backend.
- Header and query matches default to type `Exact`, so equality compares like with like, as kgateway's `DeepEqual` does on typed structs.
- A rule with omitted `matches` has the CRD default `PathPrefix /`. An explicit `matches: []` stays empty.
- `inherit-parent-matcher` is true for `true`, `yes` or `enabled`, case-insensitive, read from the child (`api/annotations/delegation.go:7`; `utils/delegation/delegation.go` V4:59-71, V3:48-60; `pkg/krtcollections/policy.go` V4:1429).

### 3.3 Unknown reasons

| Reason | When | What is unknown |
|---|---|---|
| `namespace-labels-unavailable` | Namespace access is not OK and a listener uses `From: Selector` | that listener's count, its members and their parentRef outcomes |
| `listenerset-no-status` | a ListenerSet has no current `Accepted` | that ListenerSet's listeners |
| `setting-unknown` | a setting comes from `valueFrom`, or from `envFrom` with no explicit entry | experimental flag: every route of an experimental-only kind, and the listeners it could count on. Wildcard: label refs whose namespace could be the wildcard |
| `not-reached` | an owned parent route is not reached from any root (§5.1) | its contexts, so its delegation outcomes; selection and permission stay known |
| `context-unknown` | an ancestor on the path is unknown or was cut by a bound | everything below that point |
| `too-large` | a compute bound of §5.7 stopped the computation | everything not yet computed, in the deterministic order of §5.7 |

**Uncertainty is never evidence (our call).** An unknown setting, a boolean setting that does not parse, and the over-approximation used for label selection under an unknown wildcard setting all stay uncertainty. They never select a route, never make a route a candidate, and never count as negative evidence. They feed only the uncertain points of §8.5.

Missing objects are rules, not unknowns. A missing Namespace matches only an empty selector (§4.2). A missing `name` target is an unresolved reference (§5.2).

## 4. View A — route → listener attachment

### 4.1 Algorithm

For each Gateway, and for each **allowed** ListenerSet of it (`query/route.go` V4:393-410, V3:353-370), kgateway runs `processRoute` on every candidate route. Candidates are routes with a parentRef whose (group, kind, namespace, name) names the resource. A Gateway parentRef never reaches ListenerSet listeners, and a ListenerSet parentRef never reaches Gateway listeners.

```
viewA(resource, route):                               # processRoute: V4 query/route.go:437-547, V3:397-508
  for ref in route.parentRefs where names(ref, resource):    # query/query.go:201-229, both
    allowed = matched = hosts = false
    for l in resource.listeners:
      kinds, nsOK, err = allowedRoutes(resource, l)    # V4 query/route.go:157-221, V3:130-194
      if err: listenerError(l); continue               # From=Selector with a nil selector
      if route.kind not in kinds: continue             # Kind compared, group ignored
      if not nsOK(route.namespace): continue
      allowed = true                                   # set BEFORE the section check
      if ref.port and ref.port != l.port: continue     # query/query.go:170-178, both
      if ref.sectionName and ref.sectionName != l.name: continue
      matched = true
      ok, eff = intersect(l.hostname, route.hostnames) # HTTPRoute, GRPCRoute, TLSRoute; query/query.go:231-296, both
      if not ok: continue
      hosts = true
      member(resource, l) += (route, ref, eff)          # one entry per (route, parentRef)
    outcome(route, ref) = not allowed → NotAllowedByListeners      # V4 query/route.go:526-543, V3:486-503
                          not matched → NoMatchingParent
                          not hosts   → NoMatchingListenerHostname
                          else        → accepted on the listeners that passed
```

**The `allowed` quirk is reproduced exactly.** `allowed` is decided across all listeners before the section check. A parentRef with `sectionName: b`, where listener `a` allows the route's namespace and `b` does not, gives `NoMatchingParent`.

### 4.2 Allowed routes

- **Kinds.** Protocol defaults: HTTP and HTTPS → HTTPRoute, GRPCRoute; TLS with mode Terminate → TLSRoute, TCPRoute; any other TLS mode → TLSRoute; TCP → TCPRoute; UDP → none; any other protocol → HTTPRoute (`query/route.go` V4:198-221). A set `allowedRoutes.kinds` is intersected with those defaults; a nil group means `gateway.networking.k8s.io` (V4:163-175). An explicit empty `kinds` list allows no kinds; an entry with an empty group is ignored, and the requested order and duplicates are kept (`query/route.go` V4:163-174, V3:136-147; `filterSupportedRouteKinds` V4:223-231, V3:196-204).
- **Namespaces.** The default `Same` means the namespace of the resource; for a ListenerSet listener that is the ListenerSet's. `All` allows every namespace. `Selector` is evaluated against the Namespace object's labels. A missing Namespace is admitted only by an empty selector, via `sel.Empty()` (`pkg/krtcollections/namespaceselector.go` V4:20-28). An unknown `From` acts as `Same` (`query/route.go` V4:178-192, V3:151-165).
- **Experimental kinds.** A route of an experimental-only kind (D-018) is a candidate only when the experimental setting is true.

### 4.3 Hostname intersection

From `query/query.go` V4:231-296 (both). Matching is a case-sensitive string suffix match.
- A route with no hostnames attaches. Its effective hostname is the listener's, or none.
- A listener with no hostname gives the route's hostnames as written (`query/query.go` V4:248-250); duplicates are removed only when intersecting.
- Otherwise, per pair: two exact names must be equal (result: the listener's); a `*.x` listener matches an exact route name ending in `.x` at any depth (result: the route's); an exact listener matches a `*.x` route when it ends in `.x` (result: the listener's); two wildcards give the more specific. Duplicates are removed.
- Attachment fails only when the result is empty.
- The result is the **effective hostnames per (parentRef, listener)**. Each listener has its own list (`query/route.go` V4:457-521, V3:417-481).

### 4.4 Listener count

`attachedRoutes` is `len(listenerResult.Routes)` for each listener of the Gateway and of each allowed ListenerSet (`translator/gateway/gateway_translator.go` V4:110-121, same in V3). So the computed count is:
- one entry per (route, parentRef) that passed on that listener; a route with two matching parentRefs counts twice;
- top-level routes only, never delegated children;
- counted before backend resolution, delegation outcomes, rule translation and listener validation.

### 4.5 Programming-only rules (not part of view A)

Two things narrow what Envoy serves without changing status or counts:
- 2.4's plain-HTTP listener isolation drops a hostname from a less specific listener on the same port (`translator/listener/gateway_listener_translator.go` V4:689-747; V3 has none);
- listeners that fail validation are dropped later (`TranslateListeners`, V4:62). Their routes still count and keep `Accepted=True`.

View A shows status semantics. On 2.4, a plain-HTTP listener carries the note "Envoy may serve fewer hostnames on this listener" when another plain-HTTP listener on the same port of the same Gateway (its own listeners and those of its allowed ListenerSets, `appendHttpListener` V4:135-173) has a hostname inside this listener's and ranks above it in kgateway's precedence: empty < wildcard < exact, a wildcard with more labels first, equal hostnames never (`translator/listener/gateway_listener_translator.go` V4:700-747). So an empty-hostname listener is noted when any other has a hostname, and a `*.x` listener when another is an exact name ending in `.x` or a longer wildcard under `.x`. The count never changes **(our call)**.
- Only listeners kgateway merges take part, so a listener whose current status is `Accepted=False` neither gives nor gets the note: kgateway drops rejected and conflicted listeners before merging (V4:62-63; `translator/listener/validation.go` V4:166, 518-546).
- A ListenerSet without a current `Accepted` (§3.3), or whose parent Gateway is not known, neither gives nor gets the note: its listeners' counts are unknown **(our call)**.

### 4.6 Output

| Result | Key | Value | Dependencies |
|---|---|---|---|
| Listener membership | (resource, listener, route, parentRef index) | effective hostnames | route, resource, the route's Namespace when the listener uses `Selector` |
| ParentRef outcome | (route, parentRef index) | accepted with listeners, or one of three reasons | route, resource; Namespaces evaluated by `Selector` on any listener of the resource |
| Listener count | (resource, listener) | number | resource, every candidate route, every Namespace evaluated by `Selector` |
| Explanation | per non-accepted outcome | the listener facts that failed, e.g. "listener `https` allows namespaces with `team=web`; namespace `shop` has `team=retail`" | as the outcome |

## 5. View B — delegation selection, permission and fit with ancestor context

### 5.1 Roots and contexts

kgateway flattens delegation **once per parent match and delegation ref**, with the parent's match as it stands **after its ancestors filtered or merged it** (`translator/httproute/gateway_http_route_translator.go` V4:119,190-210, V3:119,195-207). A kept child is cloned with its rules replaced by the fitting matches (`translator/httproute/delegation_helpers.go` V4:91-102, V3:92-103), and that clone is translated recursively (`translator/httproute/delegation.go` V4:93-95, V3:100-103). So an outcome depends on the path, not just the edge.

Example: root R delegates under `/api` to P. P has matches `/api` and `/other` on a rule that delegates to C, whose only match is `/other/item`. R's fit reduces P to `/api`, so C is dropped. Evaluating P → C from P's own matches would wrongly attach C through `/other`. The rules carry the context to get this right.

- **Root.** An owned top-level HTTPRoute that view A accepts on at least one listener. When view A is unknown for all its parentRefs, a current `Accepted=True` from status makes it a root, labelled "root from status". Otherwise it is not a root. Roots do not depend on the listener: view B never reads hostnames, only the child's own `spec.hostnames`.
- **Context.** One `HTTPRouteMatch` (the incoming parent match), the path of routes from the root, the report key of the parent's reporter (§7), and the dependency set (§5.6): the root's eligibility dependencies plus every route on the path.
- **Root contexts.** Each match of each root rule that has a delegation ref, with the CRD default when `matches` is omitted.
- **Child contexts.** Each effective match (view C) of each kept child rule that has a delegation ref.

### 5.2 Selection

| Mode | Spec | Selects (`query/route.go` fetchRoutesByRef V4:315-361, V3:275-321) |
|---|---|---|
| `name` | `name: x` | the HTTPRoute ns/x; missing → unresolved reference |
| `wildcard` | `name: "*"` or empty | every HTTPRoute in ns |
| `label` | group `delegation.kgateway.dev`, kind `label`, `name: v` | HTTPRoutes in ns whose label `delegation.kgateway.dev/label` is exactly `v` (`api/labels/delegation.go:7`) |
| `label`, namespace equals the wildcard setting | `name: v` | that label value in every namespace. The setting defaults to `all`; an empty value also means `all` (`api/labels/delegation.go:12`) |

An empty result is an unresolved reference. No ReferenceGrant is involved. Out-of-scope routes are never selected; a `name` ref to one shows "Not watched by the controller".

### 5.3 Permission

`ChildRouteCanAttachToParentRef` (`utils/delegation/delegation.go` V4:20-34, V3:20-47):
- a child with an empty `parentRefs` list may be selected by any parent;
- otherwise it needs a parentRef with **explicit** `group: gateway.networking.k8s.io` and `kind: HTTPRoute` (nil does not match), the parent's name, and a namespace equal to the parent's (nil means the child's own);
- sectionName and port are ignored.

A forbidden child is skipped silently, with no status, before the cycle check (`query/route.go` V4:266-269, V3:240-242).

### 5.4 Algorithm

```
selectTree(P, path):                                   # getDelegatedChildren: V4 query/route.go:233-292, V3:206-269
  items, errs = {}, {}
  for rule in P.rules:
    acc = []                                           # quirk 1: accumulated per RULE, not per ref
    for ref in delegationRefs(rule):
      routes, err = fetch(ref)                         # §5.2
      if err: errs[key(ref)] = err; continue
      for c in routes:
        if not permits(c, P): row(P, ref, c, forbidden); continue
        if c in path: errs[key(ref)] = cycle; continue # path-scoped, so diamonds are valid
        acc += c
      items[key(ref)] = copy(acc)                      # quirk 2: keyed per parent ROUTE; a later rule overwrites
  return items, errs                                   # errors are never cleared (query/backendmap.go V4:39-54, both)

childrenFor(P, ref, path):                             # BackendMap.get
  items, errs = selectTree(P, path)
  if key(ref) in errs: return error errs[key(ref)]     # one cycle fails the whole ref and its siblings
  if key(ref) not in items: return error unresolved
  return items[key(ref)]

flatten(P, i, ref, ctx):                               # V4 translator/httproute/delegation.go:26-102, V3:28-109
  kids, err = childrenFor(P, ref, ctx.path)
  if err: parentSignal(ctx.reporterKey, err); rows(P, i, ref, ctx, err); return
  valid = 0
  for c in kids:
    eff = viewC(c, ctx.match)                          # §6; ctx.match is already pruned by the ancestors
    if eff.keptRules is empty: row(P, i, ref, c, ctx, dropped, eff.reason); continue
    if c.hostnames: row(..., rejected-hostnames); childSignal(c, key(c, P), rejected); continue   # V4 :83-91, V3 :90-98
    row(..., attached, eff); childSignal(c, key(c, P), attached); valid += 1
    for (k, rule) in eff.keptRules:
      for m in rule.matches:                           # the effective matches
        for ref2 in delegationRefs(rule):
          if not budget.take(): stop(too-large)        # §5.7
          flatten(c, k, ref2, {match: m, path: ctx.path + c, reporterKey: key(c, P), deps: ctx.deps + c})   # deps start at the root's eligibility set, §5.6
  if valid == 0: parentSignal(ctx.reporterKey, unresolved)        # V4 :98-100, V3 :105-107
```

- `parentSignal` maps an error as `ProcessBackendError` does: a cycle gives `ResolvedRefs=False/RefNotPermitted`; an unresolved reference gives `ResolvedRefs=False/BackendNotFound` (`query/utils.go` V4:38-51, V3:37-50). Under STANDARD or STRICT validation the parent rule becomes a 500 response (`translator/irtranslator/route.go` V4:315-372).
- Child hostnames are checked **after** the fit. A hostname-bearing child whose rules all fail the fit is `dropped`, not `rejected-hostnames`.
- Memoising `selectTree` per parent and filtering the path afterwards is allowed. The results must equal the definition above.
- **The two quirks are reproduced as written.** Both were observed on 2.3.11 and 2.4.7 (D-021, item 7).

### 5.5 Outcomes and rows

A delegation ref that selects nothing (an unresolved reference, §5.2) yields one row with outcome `unresolved` and **no child** **(our call)**.

Per context, a row (parent, rule index, ref index, child) gets one outcome: `attached`, `dropped` (with the failing match), `forbidden`, `rejected-hostnames`, `cycle` (the ref failed), `unresolved` (the ref selected nothing usable) or `unknown` (with a reason). The rule and ref indexes are part of the row's identity, because one child can be selected by several rules and refs of one parent.

The row aggregates its contexts **(our call)**. A bound reduces certainty; it never changes a conclusion:
- every context known and the same → that outcome;
- `attached` in some contexts and `dropped` in others → `partial`, "Attached under some parent matches";
- `attached` in a known context while others are unknown → `attached`, with "Some paths not computed". A positive outcome stays true whatever the other paths do;
- no `attached` context and some contexts unknown, or the row's aggregate incomplete (§8.5) → `unknown`, "Dropped on the paths computed; other paths not computed". Never `dropped`;
- `forbidden` does not depend on context and is always known.

### 5.6 Dependencies

Every result carries the set of objects whose data decided it; the design's Authorizer filters on it (design §7.4).
- **Root eligibility.** A view A root depends on the union of the dependencies of all its parentRef outcomes (§4.6): the route, each Gateway or ListenerSet, and each Namespace a `Selector` evaluated. A status root depends on the route and the target of the `Accepted=True` entry it used.
- **Context.** A context depends on its root's eligibility set plus every route on its path. Each child context inherits its parent context's set and adds the child.
- **Row and effective matcher.** The union of the sets of its contexts. A context the caller may not view is projected as `{restricted: true}`; an outcome that aggregates over a hidden context is `restricted` as a whole, so the presence of a hidden path is never revealed.
- **Aggregate checks** (§8.2) and the findings they raise: the union over every context in the aggregate.

### 5.7 Budget, retention and projection

This is the one policy for how much is computed, kept and served **(our call)**. All of view B and C is computed in the recompute; the Model keeps what this table allows; a detail or delegation response projects the kept results and never re-runs the traversal.

| Stage | Bound | Cap | On reaching it |
|---|---|---|---|
| Compute | selected edges: (parent, rule, ref, child) rows, forbidden included | 50,000 per install | the rest is `unknown`, `too-large` |
| Compute | contexts: `flatten` calls | 20,000 per install | the rest is `unknown`, `too-large` |
| Compute | matcher evaluations: (child match, context) fit or merge steps | 200,000 per install | the rest is `unknown`, `too-large` |
| Retain | contexts kept per row, each with its incoming match, path, outcome and effective rules | first 20 in traversal order | `contextsTruncated` on the row; the outcome still aggregates all contexts |
| Retain | contexts kept per install | 100,000 | later rows keep their outcome with no contexts and `contextsTruncated` |
| Project | rows per delegation page | the page `limit` (≤ 200) | cursor (design §3.6) |
| Project | parent edges in a route's `effective` | 20, sorted by parent (namespace, name) | `effectiveTruncated` |
| Project | contexts per parent edge in `effective` | the union of the retained contexts of every row with that (parent, child), in traversal order, at most 20 | the edge's `contextsTruncated`: true when any of those rows is truncated (per-row or install cap) or the union exceeds 20. At the install cap an edge can have no contexts; it is then sent with `contexts: []` and `contextsTruncated: true`, never as complete |

- Work runs in a fixed order: roots by (namespace, name), rules and refs by index, children by (namespace, name). A cut is deterministic, so the same Model gives the same cut. Which cap was hit is **logged** once per change of the summary; it is not served on the API **(our call)**.
- **Retention is streaming (our call).** Each `flatten` result is folded into its row's aggregate as it is produced: the outcome aggregate, the dependency set, the completeness inputs and the report-key aggregates are updated in full, and the context itself is kept only while the row's and the install's retention caps allow. Nothing is materialised first and trimmed later, so memory is bounded by the retention caps plus the aggregates. Dependency and completeness data are never dropped by retention; only context samples are.
- An effective matcher shown on a page is exactly the one the traversal used for the outcome and the cross-check in the same Model.
- Retained contexts are counted in the heap budget (design §9.6).
- View A has no cap. It is linear in parentRefs × listeners.
- The design's benchmarks include a dense-selection shape, separate from the diamond lattice (design §9.6).

## 6. View C — effective matcher

Per kept child rule, given the incoming parent match of the context (`translator/httproute/delegation_helpers.go` V4:33-107, V3:34-108):

```
viewC(child, parentMatch):
  kept = []
  for rule in child.rules:
    valid = []
    if inherit(child) and rule.matches == []: valid += parentMatch       # explicit empty list only
    for m in rule.matches:
      if inherit(child): valid += merge(parentMatch, m)                  # V4 :117-196, V3 :118-196
      elif fits(parentMatch, m): valid += m                              # used unchanged
    if valid: kept += rule with matches = valid
    else: reason(rule) = the clause of fits() that failed
  return kept                                                            # child kept iff kept is not empty
```

**Default mode, `fits`** (`IsDelegatedRouteMatch`, `utils/delegation/delegation.go` V4:82-133, V3:72-123):
- the parent path type must be `PathPrefix`; with an Exact or Regex parent every child match fails;
- the child path value must start with the parent value as a raw string, so `/api` accepts `/apiv2`; the child's path type is not checked;
- every parent header and query param must appear in the child, equal in name, type and value;
- a parent method must be matched by the same child method.

A rule with omitted `matches` has the default `PathPrefix /`, which fails under any parent prefix but `/`. The reason reads "This rule has no matches, so it defaults to `/`, which is outside the parent's `/api`".

An explicit `matches: []` in default mode has no match to fit, so the rule is dropped with reason `no-matches`: "This rule has an empty match list, so nothing fits the parent's match" **(our call)**.

**Inherit mode, `merge`** (`mergeParentChildRouteMatch`, `delegation_helpers.go` V4:117-142, V3:118-146). The parent path type is not checked.
- Path: `path.Join(parent, child)`, keeping the child's path type. A nil child path counts as Prefix `""`. `path.Join` cleans the result, so `/api` + `/` gives `/api`.
- Headers and query params: the union by name, the parent winning on a conflict, sorted by name (V4:146-196, V3:147-196).
- Method: the parent's, if set.
- Nothing is dropped.

The **context carries the result down.** A grandchild's parent match is one of the child's effective matches, never the child's spec match. Each effective match depends on every route on the path.

## 7. Report keys per version

kgateway keys a status report on (group, kind, namespace, name, sectionName) **as written**, with nil as empty and **no port** (`getParentRefKey`, `pkg/reports/reporter.go` V4:424-450, V3:369-394). The status builder walks the route's `spec.parentRefs` (or, when that list is empty, the report's own refs with the namespace filled in) and writes an entry for each spec ref whose key has a report. The entry carries the spec ref as written, port included (`pkg/reports/status.go` V4:489-496,531-543, V3:437-442,471-483).

| Route | Reporter ref | Entry written |
|---|---|---|
| Top-level route, both | the parentRef as written | for every parentRef the translation reported |
| Child with empty `parentRefs`, both | synthesized `{gateway.networking.k8s.io, HTTPRoute, P.namespace, P.name}` | yes, under the synthesized ref |
| Child with an explicit permitting ref, 2.4 | the child's matching ref as written (`childParentRef`, `query/route.go` V4:300-310; reporter at `delegation.go` V4:81) | yes, under that ref |
| Child with an explicit permitting ref, 2.3 | always synthesized (`query/route.go` V3:252-259; reporter at `delegation.go` V3:83-88) | **only** when some spec ref has the same key: explicit group and kind, an **explicit** namespace equal to P's, and no sectionName. A port is allowed; the entry then carries it. |

**Written versus resolved (our call).** A report key is built from the parentRef **as written**, with empty strings for omitted fields (`pkg/reports/reporter.go` V4:424-450, V3:369-394). Two refs that differ only by an omitted versus an explicit namespace are therefore two keys, and two checks. The resolved identity (defaults applied) is used only to find the target object. The typed views keep both forms for spec and status refs from sub-phase 2.2 on (design §4.1).

Consequences, applied to **every** outcome that writes child status, `attached` and `rejected-hostnames` alike:
- On 2.3, a child whose permitting ref omits the namespace or sets `sectionName` gets **no entry at all**. The 2.3 rule set expects none, and the check is `not-verified` (nothing can be observed), never a disagreement.
- On 2.3, a child whose ref differs from the synthesized one only by a port **does** get an entry, written with its port.
- Two spec refs that differ only by port share one key. Both get the same entry content. The check compares them as one.

## 8. Cross-check contract

### 8.1 Which signal checks which stage

A controller signal checks a computed result only when the signal is written by the stage the rules model. Failures of later stages are never disagreements.

**Overwrites make some cases inconclusive (our call).** `SetCondition` replaces a condition of the same type at both tags (`pkg/reports/reporter.go` V4:505-512, V3:447-454), so the last writer wins. Two cases are therefore **inconclusive**, never contradicting and never KGWUI-012:
- a computed attachment rejection facing an observed `Accepted=False` with a non-attachment reason: a later stage may have overwritten the attachment reason;
- any report key shared by parentRefs with **different** computed outcomes: which write survived depends on order.
The check shows `checked` with coverage `not-verified` and the note "The controller's status cannot confirm this".

| Signal | Written by | Checks | Never a disagreement |
|---|---|---|---|
| Listener `attachedRoutes` | attachment (`gateway_translator.go` V4:110-121, both) | view A listener count | — (it is counted before every later stage) |
| Route `Accepted=False` with `NotAllowedByListeners`, `NoMatchingParent` or `NoMatchingListenerHostname` | attachment (`gateway_translator.go` V4:80-87, both) | view A parentRef outcome | — |
| Route `Accepted=False` with any other reason | later stages: rule replacement on 2.3, TCP/TLS conflict on 2.3 (§2) | nothing | compatible with a computed `accepted` |
| `ResolvedRefs=False` on a top-level route or a child | backend resolution (`gateway_http_route_translator.go` V4:214-216; `query/utils.go` V4:45-57) | nothing on its own | compatible with every computed outcome |
| `kgateway.dev/Programmed=False` | rule translation, conflicts (2.4) | nothing | compatible |
| Child entry for P, present or absent | delegation flatten (§5.4) | view B child outcome, aggregated (§8.2) | — |
| Child `Accepted=False/UnsupportedValue` | hostname validation (`delegation.go` V4:83-91, V3:90-98) | view B `rejected-hostnames` | — |
| Parent `ResolvedRefs=False/BackendNotFound` or `RefNotPermitted` | delegation failures and backend failures share it | view B parent signal, aggregated (§8.2) | see §8.2 |
| Effective matchers | nowhere | nothing | always `not-verified` |
| Effective hostnames | nowhere | nothing | always `not-verified` |

### 8.2 Aggregation at the controller's reporting granularity

kgateway merges every write for one report key into one entry, and a later write of a condition type replaces an earlier one (`SetCondition`, `pkg/reports/reporter.go` V4:505-512, V3:447-454). Per-rule, per-ref and per-match outcomes are therefore not separately visible. Expected signals are aggregated per report key before they are compared:

| Check | Key | Expected |
|---|---|---|
| `listener-count` | (resource, listener) | the computed count (§4.4) |
| `route-accepted` | (route, report key of the parentRef, as written) | refs sharing a key with the **same** computed outcome are merged; refs sharing a key with different outcomes are inconclusive (§8.1). A rejection expects `Accepted=False` with **the computed** attachment reason; another attachment reason contradicts; an observed non-attachment reason is inconclusive. An acceptance expects no `False` with an attachment reason |
| `child-entry` | (child, report key under P, §7) | an entry iff **any** context, over every rule and ref of P and every root, gives `attached` or `rejected-hostnames`. With a rejection: `Accepted=False/UnsupportedValue`. With an attachment: no `UnsupportedValue`. No entry when every context gives `dropped`, `forbidden`, `cycle` or `unresolved`. |
| `parent-resolvedrefs` | (P, P's reporter key) | `False` when any context with that key records a delegation failure. The reason is compared only when the aggregate is **complete** (§8.5), all its failures share one reason, **and** P has no non-delegation backendRef and no `ExtensionRef` filter. An incomplete aggregate with a known failure establishes only `ResolvedRefs=False`, because an omitted context could add another reason (`query/utils.go` V4:38-50, V3:37-49), and writes to one key replace each other (`pkg/reports/reporter.go` V4:505-512, V3:447-454); otherwise only the status is compared. A computed "no delegation failure" against an observed `False` contradicts only under the same condition; otherwise it is compatible. |

Individual rows keep their own outcomes. A row whose outcome the aggregated signal cannot confirm says "Status cannot confirm this path". It is never a disagreement on its own. Every negative expectation in this table (no entry, no delegation failure) also needs a complete aggregate (§8.5).

### 8.3 Evidence classes

"Current" means the condition's `observedGeneration` equals the generation of the object that carries it. A current signal proves the controller processed that object's spec, not every other object the check depends on. The persistence window (§9) covers that gap.

| Class | When | Check state | KGWUI-012 |
|---|---|---|---|
| Contradicting current status | the signal exists, is current, and contradicts the expectation | `pending` until mature, then `disagrees` | when mature (§9.3) |
| Stale status | the signal exists and is stale | `pending`, "Waiting for the controller" | never. Phase 1's KGWUI-003 already reports status that stays stale. |
| Trigger | a computed or orphan KGWUI-011 predicate holds (§9.3) | the check state is unaffected; maturity is tracked separately | never; it raises only KGWUI-011 |
| Missing expected status | an entry is expected and this controller wrote none | `pending` until mature, then `disagrees` | `child-entry` only, when mature (§9.3). A missing entry on an owned top-level route, and a missing listener entry on an owned Gateway or ListenerSet (`pkg/health/checks.go:264`), is Phase 1's KGWUI-003, and the check stays `pending`. |
| Unexpected status | no entry is expected and a current one exists | `disagrees` where the rule set marks the clearing as verified (both sets: a drop or deselection caused by a parent edit, D-021); otherwise, including a drop caused by a child edit, `not-verified`, "The controller may keep old status". Cause: a child edit when the child's current generation was first seen after the parent's current generation, or when either time is unknown; otherwise a parent edit (our call) | where verified, when mature |
| Compatible | the signal is consistent with the expectation | `checked`, with coverage (§8.4) | none |
| Not observable | no signal can show it | `checked` with coverage `not-verified` | none |

### 8.4 Coverage labels

A cross-check that passes says what it verified. "Agrees" is never shown for a fact the controller does not report **(our call)**.

| Coverage | Means | Shown as (draft) |
|---|---|---|
| `count-agrees` | the listener's count equals `attachedRoutes`; membership and effective hostnames are not verified | "Count agrees with the controller" |
| `status-compatible` | the parentRef's or child's status is consistent with the outcome | "Status agrees" |
| `not-verified` | nothing the controller writes covers this fact | "Not checked by the controller" |

A result may carry several labels. A route's listener membership carries the listener's `count-agrees` and the parentRef's `status-compatible`, and stays `not-verified` for which listener it is on when its parentRef matched several. An effective matcher is always `not-verified`.

Check states on the wire are `checked`, `disagrees`, `pending`, `unknown` and `restricted`. `restricted` is set at serve time when the caller may not view a dependency (design §7.4).

### 8.5 Completeness

An aggregate (§8.2) is **complete** when every context that could contribute to it was evaluated. Positive evidence needs no completeness; negative evidence does.

- **Uncertain points.** A cut by a compute bound (the (parent, ref) not flattened), a context or selection that is `unknown` (§3.3), and an owned top-level route whose root eligibility is unknown.
- **Reach graph (the input).** The traversal produces, besides its outcomes, a graph of (route → permitted selected child) edges for **every** HTTPRoute in scope that has delegation refs, owned or not, kept or dropped, reached or not, computed from selection (§5.2) and permission (§5.3) alone, never from the matcher fit or the context. Edges below a non-owned or dropped route, and below a context or matcher cut, are therefore present. A selection that is itself uncertain (§3.3) marks its parent as an uncertain point instead of adding edges. Every route that uncertain selection might select (for a label ref under an unknown wildcard setting: every route with that label value, in any namespace) is itself an uncertain point, so it and everything it reaches are incomplete (our call). This graph is bounded by the selected-edge cap.
- **Reach closure.** From each uncertain point, follow the reach graph: (parent → permitted selected child), transitively. This over-approximates every route the missing work could reach. It is linear in selected edges. If the edge cap itself was hit, every aggregate in the install is incomplete.
- An aggregate whose child (for `child-entry`), parent P (for `parent-resolvedrefs`) or route (for KGWUI-011) is in a reach closure is **incomplete**. For the orphan trigger the aggregate is the route across **every** owned parent of this install that selects it; it is incomplete when the route, or any selecting parent, is in a reach closure. Completeness, and the conclusion it supports, are **install-local**: a parent of another install may still select the route, since an empty `parentRefs` list permits any parent (`utils/delegation/delegation.go` V4:25-27, V3:25-27).

| Expectation | Needs completeness | If incomplete |
|---|---|---|
| Child entry present (some context attached or rejected) | no | compared as usual |
| No child entry (every context dropped, forbidden, cycle or unresolved) | yes | `unknown`, "Not all paths computed"; no KGWUI-012 |
| Parent `ResolvedRefs=False` (some context failed) | no, for the status | the status is compared; the reason is not |
| The parent's failure reason | yes | not compared |
| No delegation failure on the parent | yes | `unknown`; no KGWUI-012 |
| KGWUI-011 computed trigger, `name` or `label` ref (dropped or forbidden everywhere under that parent) | yes | no finding |
| KGWUI-011 orphan trigger (empty `parentRefs`; selected only by `wildcard` refs; every selecting outcome `dropped` or `forbidden`) | yes, for every selecting parent of this install | no finding |
| Listener count, parentRef outcome (view A) | no traversal; unknown inputs only (§3.3) | — |

## 9. Persistence of KGWUI-012 and the computed KGWUI-011

### 9.1 Check identity

A check's identity is (install, rule set, check kind, subject):

| Kind | Subject |
|---|---|
| `listener-count` | resource ref, listener name |
| `route-accepted` | route ref, report key |
| `child-entry` | child ref, parent ref, report key |
| `parent-resolvedrefs` | parent ref, reporter key |
| `delegated-not-attached` (KGWUI-011 computed, `name` or `label`) | route ref, parent ref |
| `orphan-not-attached` (KGWUI-011 orphan) | route ref (covers every selecting parent) |

A rule set change gives a new identity.

### 9.2 Semantic fingerprint

Each check kind has its own fingerprint: a hash over exactly the decoded inputs its expectation read, plus exactly the observed evidence its comparison uses (§8.1). Nothing else resets the window.

| Kind | Inputs | Observed evidence |
|---|---|---|
| `listener-count` | the listener's spec (name, port, protocol, hostname, TLS mode, allowedRoutes); for a ListenerSet, its acceptance; each candidate route's kind, namespace, hostnames and parentRefs to this resource; candidate namespaces' labels when the listener uses `Selector`; the experimental setting | `attachedRoutes`; the listener conditions' `observedGeneration` |
| `route-accepted` | the route's kind, namespace, hostnames and parentRefs to the resource; the resource's listener specs (as for `listener-count`); namespace labels when `Selector` is used | for the key: `Accepted` status, reason and `observedGeneration`, but the reason only when it is an attachment reason (another reason is compatible and is hashed as "other") |
| `child-entry` | per contributing context: its incoming match and path; the child's matches, hostnames, parentRefs and inherit annotation; the ref; the wildcard setting; the aggregate's completeness | for the key: entry presence; `Accepted` status and `observedGeneration`, with the reason only when it is `UnsupportedValue`. **Not** `ResolvedRefs` or `Programmed`. |
| `parent-resolvedrefs` | per context with P's reporter key: its incoming match and path; every input of each selected child that can change whether it is kept, and so whether P records a failure: its rules' matches, hostnames, parentRefs, labels used by label refs, and the `inherit-parent-matcher` annotation; the ref; the wildcard setting; P's backendRef kinds and `ExtensionRef` filters; the aggregate's completeness | for P's key: `ResolvedRefs` status, its reason when §8.2 compares it, and `observedGeneration`. **Not** `Accepted` or `Programmed`. |
| `delegated-not-attached` | as `child-entry` | none |
| `orphan-not-attached` | the route's `parentRefs` (empty or not); the set of owned parents and refs that select it; for each, the `child-entry` inputs of its contexts; completeness of every selecting parent | none |

An input whose change can only remove the check or make it `unknown` (ListenerSet acceptance; the experimental setting for `route-accepted`) need not be hashed: §9.3 clears the window.

Not in any fingerprint: `resourceVersion`, `managedFields`, labels and annotations not listed, conditions not listed for that kind, `lastTransitionTime`, metrics, Pods and Deployments. A child's backend `ResolvedRefs` toggling never restarts its `child-entry` window. An unrelated status write, annotation edit or metrics arrival never restarts any window.

### 9.3 State, maturation and clearing

The engine keeps a map from identity to (fingerprint, `since`) under its compute lock, carried across recomputes like Phase 1's grace map (`pkg/engine/engine.go:83,110-130`). Each recompute:
1. For a check in a candidate class (contradicting, missing expected, unexpected where verified): if its identity is new or its fingerprint changed, `since` = now.
2. **Contradicting current or unexpected status** is mature when `now − since ≥ 30 s`.
3. **Missing expected child status** is mature when `now ≥ max(since + 30 s, graceEnd)` **and** P's own entry under its reporter key is current. `graceEnd` is 60 s (Phase 1's grace period) after the latest first-seen time of the current generation of the child, P and every route on a contributing path. If P's entry is not current, the check stays `pending`; P has Phase 1's KGWUI-003.
4. **Window start.** Every check result carries its window start, `since`, as an output of the persistence step. The finding it raises uses that value as its `since`. The same holds for both KGWUI-011 kinds **(our call)**.
5. A mature check raises KGWUI-012 on the object whose status disagrees: the Gateway or ListenerSet for a count, the route for a parentRef, the child for a child entry, P for its `ResolvedRefs`.
6. **Clearing.** When the check is no longer in a candidate class (it agrees, turns unknown or is skipped), its entry is deleted and the finding goes on that same recompute.
7. Entries whose subject no longer exists are pruned. A UI restart loses the map, so windows restart, as grace does in Phase 1.
8. **Deadline.** Each recompute reports the earliest future maturation instant to the scheduler (design §3.1). The predicate becomes true at exactly that instant, so a recompute then raises the finding with no further event.

**Computed KGWUI-011** (design §6.2) uses the same map with kind `delegated-not-attached`, and the orphan trigger with kind `orphan-not-attached`. The orphan predicate is, all of: the route is in scope, has an empty `spec.parentRefs` list and no eligible status entry; it is selected by at least one owned parent of this install, and **only by `wildcard` refs** (if any `name` or `label` ref selects it, the per-parent trigger applies and there is no orphan finding); **every** selecting row, in every context, has outcome `dropped` or `forbidden` (`cycle`, `unresolved`, `unknown`, `attached`, `partial` and `rejected-hostnames` all disqualify); and its orphan aggregate is complete (§8.5). The finding says no parent route of this install keeps the route; it claims nothing about other installs. Both kinds are mature 30 s after `since`. They never make a check pending, because they wait on no controller output **(our call)**.

## 10. Observations that may change these rules

The first task of sub-phase 2.1 records, on 2.3.11 and 2.4.7, whether status is produced, and under which parentRef (design §9.5):
1. an implicit child (empty `parentRefs`) after the parent stops selecting it: is the old entry removed? **Answered (D-021): yes, on both.**
2. an explicit child whose parentRef omits the namespace (§7); **answered (D-021): no entry on 2.3, entry as written on 2.4.**
3. an explicit child whose parentRef sets `sectionName`, and one that sets only `port` (§7); **answered (D-021): as §7 states, on both.**
4. an implicit child selected by name, wildcard and label; **answered (D-021): synthesized entry for each, on both.**
5. a hostname-bearing child whose rules all fail the fit (§5.4); **answered (D-021): no entry, on both.**
6. a matcher-dropped child on 2.3: is old status cleared, as on 2.4? **Answered (D-021): yes, when a parent edit causes the drop; a child-edit drop is not observed.**
7. the two quirks of §5.4: a second delegation ref in one rule, and the same ref in two rules; **answered (D-021): both reproduce, on both.**
8. a parent with both a Service backendRef and a delegation ref: which `ResolvedRefs` reason wins (§8.2)? **Answered (D-021): the last rule in spec order.**
9. a child kept under one parent match and dropped under another: one entry (§8.2)? **Answered (D-021): one entry, `Accepted=True`.**
10. listener `attachedRoutes` for delegated children (expected: not counted) and for ListenerSet routes. **Answered (D-021): children not counted; a ListenerSet route counts only on its ListenerSet listener.**

Each result becomes a decision entry. A rule set, and the `verified` mark of §8.3, change before the sub-phase that depends on them.

## 11. Test matrix

Each case is a fixture world (Gateways, ListenerSets, routes, Namespaces, settings, controller status) with the expected outputs of the views and the cross-check. Fixtures live in `pkg/computed/testdata/<rule set>/`. Where possible the expected controller signals are copied from kgateway's own translator test outputs at that tag (`translator/gateway/testutils/outputs/…`), with the source file named. The cases that need a real controller also run in the kind e2e (design §9.5); they are marked **e2e**.

### 11.1 View A (sub-phase 2.3)

| Case | Expect |
|---|---|
| Each protocol's default kinds; `allowedRoutes.kinds` intersection with a nil group | members per kind |
| `Same` on a Gateway and on a ListenerSet listener; `All`; `Selector` matching, not matching, Namespace missing; nil selector | membership; listener error |
| Namespace access unavailable with a `Selector` listener | `unknown`, `namespace-labels-unavailable`; check skipped |
| `port`; `sectionName`; the `NoMatchingParent` quirk **e2e** | outcomes |
| Every hostname case of §4.3, with per-listener effective hostnames on a parentRef that matches two listeners | separate lists per listener |
| Two parentRefs of one route matching one listener **e2e** | counted twice |
| Two parentRefs that differ only by port | one `route-accepted` check |
| A Gateway parentRef never reaching ListenerSet listeners; a denied ListenerSet's routes not evaluated **e2e** | no members |
| ListenerSet without status | `listenerset-no-status` |
| Experimental kind with the setting true, false and unknown | candidate, not a candidate, `unknown` |
| 2.4 isolation note on a shared plain-HTTP port | note, count unchanged |

### 11.2 View B and C (sub-phase 2.4)

| Case | Expect |
|---|---|
| Name, wildcard, label refs; a label ref with the wildcard namespace under the default, a set value and `valueFrom` | selection; `setting-unknown` |
| Permission: empty list, by name, forbidden, nil group or kind in the child's ref | permitted, forbidden |
| **Ancestor-pruned parent match:** R `/api` → P {`/api`, `/other`} → C `/other/item` **e2e** | C `dropped` under the only context; no child entry expected |
| **One child, two matches:** P {`/api`, `/v2`} → C `/api/x`, in one rule **e2e** | row `partial`; `child-entry` expects an entry; `status-compatible` |
| **One child, two rules:** P's rule 1 drops C, rule 2 attaches it | rows `dropped` and `attached`; one entry expected |
| Hostname-bearing child that fits; one that does not | `rejected-hostnames`; `dropped` |
| Cycle: the ref fails, its siblings drop, parent `RefNotPermitted` | as stated |
| Diamond A→B1→C, A→B2→C | two child entries, one per parent |
| The two quirks of §5.4 | as written, flagged |
| An owned parent not reached from a root | `not-reached`; selection still shown |
| Dense selection: 100 wildcard parents over a namespace of 1,000 routes | `too-large` after the edge cap, deterministic cut |
| Contexts per row above 20; retained contexts above 100,000 | `contextsTruncated`; outcomes unchanged |
| One parent edge with 21 contexts; an edge from two rows, one truncated; an edge with no contexts kept at the install cap | `effective` edge has 20 contexts and `contextsTruncated`; flag set; `contexts: []` with `contextsTruncated` |
| A cut after a context that drops C, before one that would attach it | row `unknown`, not `dropped`; `child-entry` incomplete, no KGWUI-012 on an existing entry; no computed KGWUI-011 |
| A cut before any context reaches C, and an attached context already computed | row `attached`, "Some paths not computed"; entry expected |
| A label ref with an unknown wildcard setting above C | C in the reach closure; negative checks `unknown` |
| Root behind a `Selector` listener; caller denied that Namespace | every descendant context, row and effective matcher `restricted` |
| Status root; caller denied the entry's target Gateway | descendants `restricted` |
| Two rows of one child under rules 0 and 1 | separate identities; both listed |
| An effective matcher on the route page | equal to the one the traversal used in the same Model |
| Default fit: each clause, raw prefix `/apiv2`, child path type ignored, omitted `matches` under `/api` | kept or the failing clause |
| Inherit: `path.Join` cleaning, child type kept, nil child path, header and query union with the parent winning, method, explicit `matches: []` | effective matches |
| A three-level chain where the grandchild's context is the child's merged match | grandchild evaluated against the merged match |

### 11.3 Report keys (sub-phase 2.4)

| Case | 2.3 | 2.4 |
|---|---|---|
| Child with empty `parentRefs` | entry, synthesized ref | entry, synthesized ref |
| Explicit, full ref | entry | entry as written |
| Explicit ref with only a **port** **e2e** | entry, written with the port | entry as written |
| Explicit ref omitting the namespace, attached and hostname-rejected | no entry; `not-verified` | entry as written |
| Explicit ref with `sectionName` | no entry; `not-verified` | entry as written |

### 11.4 Cross-check (sub-phases 2.3 and 2.4)

| Case | Expect |
|---|---|
| Listener count equal; off by one with current status | `count-agrees`; `disagrees` after 30 s |
| Computed rejected (`NoMatchingParent`); observed `Accepted=False/RouteRuleReplaced`, current | inconclusive: `not-verified`; no KGWUI-012 |
| Two parentRefs share a written key, one computed accepted and one rejected | inconclusive; no KGWUI-012 |
| Two parentRefs differing only by an omitted versus explicit namespace | two keys, two checks |
| Computed accepted; observed `Accepted=False/NoMatchingParent`, current | `disagrees` after 30 s |
| **Attached child with a missing Service backend** (`ResolvedRefs=False/BackendNotFound`) **e2e** | `status-compatible`, no KGWUI-012 |
| Attached child with a replaced rule: 2.4 `Programmed=False`, 2.3 `Accepted=False/RouteRuleReplaced` | compatible |
| TCP conflict on both versions | compatible |
| Parent with two delegation refs, one unresolved | `parent-resolvedrefs` expects `False`; reason compared |
| Truncated parent aggregate: the retained work finds `BackendNotFound`, the omitted work holds a cycle; observed `False/RefNotPermitted` | status compared and compatible; reason not compared; no KGWUI-012 |
| Parent with a missing Service backend and all delegation refs fine | observed `False` compatible |
| Computed-attached child, no entry, P current | `pending` until `max(since + 30 s, graceEnd)`, then KGWUI-012 on the child |
| The same with P stale | stays `pending`; no KGWUI-012 |
| Stale child entry contradicting | `pending`; never KGWUI-012 |
| Matcher-dropped child (parent edit) with a current entry left, both sets | `disagrees` after 30 s |
| Matcher-dropped child (child edit) with a current entry left | `not-verified`; no KGWUI-012 |
| Drop with an unknown first-seen time for child or parent, current entry left | treated as a child edit: `not-verified`; no KGWUI-012 |
| Forbidden child with a current entry left | `not-verified` |
| Effective matcher | always `not-verified` |

### 11.5 Persistence and deadlines (sub-phase 2.3; extended in 2.4)

| Case | Expect |
|---|---|
| Disagreement clears at 29 s | no finding |
| No events after the disagreement starts | the scheduled recompute at exactly `since + 30 s` raises it; at `since + 30 s − 1 ns` it is absent |
| An annotation edit, an unrelated condition write, a `resourceVersion`-only update, a metrics arrival during the window | no reset |
| A child's `ResolvedRefs` toggles during a `child-entry` window; a child's `Accepted` reason changes between non-attachment reasons during a `route-accepted` window | no reset |
| A parent's `Accepted` changes during a `parent-resolvedrefs` window | no reset |
| A selected child's `inherit-parent-matcher` annotation changes while `parent-resolvedrefs` still contradicts (expectation `False/BackendNotFound` → no failure, observed `False/RefNotPermitted`) | reset; the old window does not carry over |
| A change to a listed field, or to the observed value | reset |
| Rule set switch | reset |
| Subject deleted | entry pruned, finding gone |
| Disagreement turns unknown (Namespace access lost) | finding cleared the same recompute |
| Missing expected entry: `graceEnd` later than `since + 30 s`, and the reverse | matures at the later instant |
| Computed KGWUI-011: matures at 30 s; never makes a check pending | as stated |
| A KGWUI-012 and a KGWUI-011 raised after their windows | each finding's `since` equals its window start |
| A dropped non-owned route between an uncertain point and a negative aggregate | the aggregate is in the reach closure; no negative finding |
| A delegation ref that selects nothing | one `unresolved` row with no child |
| Explicit `matches: []` in default mode | rule dropped with `no-matches` |
| An unparsable boolean setting | uncertainty only; no route selected or excluded on it |
| **Orphan** selected only by wildcard refs, dropped by every selecting parent, traversal complete | KGWUI-011 warning after 30 s, one per route |
| The same orphan with one selecting parent incomplete (cut by a bound, or in a reach closure) | no finding |
| A route with a Gateway parentRef, selected by a wildcard and dropped | no finding; Delegation row shown with its reason |
| An orphan dropped by one parent's wildcard and attached by another parent | no finding |
| A new parent starts selecting an orphan | the orphan window resets |
| An orphan whose only selecting wildcard ref fails with a complete `cycle` outcome | no orphan finding |
| An orphan selected by a wildcard ref of one parent and a `label` ref of another, dropped by both | only the per-parent KGWUI-011 for the `label` parent; no orphan finding |
