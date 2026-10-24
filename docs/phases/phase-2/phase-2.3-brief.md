# Sub-phase 2.3 — View A

Kind: planned
Base: f01bb0aefc3a7b4cef9a6b8f3b1bc170bd0e8131
Issues: n/a | Plan steps: implementation-plan.md Tasks 35–51; design §3.4, §3.5, §3.8, §5.2, §6.1–§6.3, §7.2–§7.6, §8.1, §8.4–§8.8, §9.1, §9.3, §9.5–§9.7, §13; computed-rules.md §1–§4, §7, §8, §9, §11.1, §11.4, §11.5; D-019, D-020, D-021

## Problem / goal
kgateway reports per listener only a number (`attachedRoutes`) and per parentRef only a condition. It never says which listener a route attached to, with which effective hostnames, or why a route failed: attachment failures carry no message (`translator/gateway/gateway_translator.go:80-87`). After 2.2 the Gateway page shows `attachedRoutes` from status and nothing per listener. Reproduction: on the kind fixture, open Gateway `infra/edge`; the listener table shows each listener's `attachedRoutes`, and nothing lists which routes make up that number or their hostnames. A route rejected with `NoMatchingListenerHostname` shows the reason and an empty message.

Sub-phase 2.3 adds view A (D-020): routes per listener with effective hostnames, labelled "computed by the UI" with the rule and rule set, and cross-checked against `attachedRoutes` and each parentRef's `Accepted`. A persistent disagreement raises KGWUI-012 (warning). Ownership, counts and install health stay status-based (D-020 rule 4). No delegation views (2.4).

## Expected behaviour

| Input / action | Expected |
|---|---|
| Controller Pod env `KGW_ENABLE_EXPERIMENTAL_GATEWAY_API_FEATURES` | Read with D-015's order (Ready Pod, any Pod, Deployment template); absent means `true`; `valueFrom`, or `envFrom` without an explicit entry, means unknown; the last duplicate wins. Namespace labels come with an access flag (Task 36; design §3.4, Q16) |
| Controller version 2.3.x, 2.4.x, unknown, newer, older | Rule set `kgateway-2.3`, `kgateway-2.4`, or the nearest set with the note "Controller version unknown; using the 2.4 rules", "Rules not yet verified for 2.5" or "Rules not yet verified for 2.2". Every `computed.note` carries it. A downstream distribution can register its own selection through `extension.WithComputedRules` (Task 37; rules §2) |
| An owned Gateway, or an accepted ListenerSet, with candidate routes | Members per (resource, listener, route, parentRef), each listener with its own effective hostnames; per-parentRef outcome `accepted`, `NotAllowedByListeners`, `NoMatchingParent` or `NoMatchingListenerHostname`. A ref with `sectionName: b`, where listener `a` admits the namespace and `b` does not, gives `NoMatchingParent` (the `allowed` quirk) (Tasks 38–39; rules §4.1–§4.3) |
| Listener counts | One per (route, parentRef) that passed: two refs to one listener count twice; delegated children never count; a ListenerSet route counts only on the ListenerSet's listener; a Gateway ref never reaches ListenerSet listeners; a denied ListenerSet gives no members, outcomes or counts (Task 39; rules §4.4; D-021 item 10) |
| Namespace access lost with a `Selector` listener; a ListenerSet without current `Accepted`; an experimental-only route with the setting unknown | `unknown` with reason `namespace-labels-unavailable`, `listenerset-no-status` or `setting-unknown`; no number and no members; a `Same` listener on the same Gateway keeps its known count; the cross-check is skipped (Task 39; rules §3.3) |
| kgateway 2.4, plain-HTTP listener whose hostname is empty or a wildcard covering another HTTP listener's hostname on the same port | `isolationNote` set; the count is unchanged; never on 2.3 (Task 40; rules §4.5) |
| Computed count equals `attachedRoutes`; a parentRef outcome consistent with its `Accepted` | State `checked`, coverage `count-agrees` or `status-compatible`. Later-stage reasons (`RouteRuleReplaced`, `Conflicted`, `ResolvedRefs=False`, `kgateway.dev/Programmed=False`) are compatible with `accepted` (Task 41; rules §8.1, §8.4) |
| A computed rejection against a non-attachment reason; refs sharing one written key with different computed outcomes | `checked`, coverage `not-verified`, note "The controller's status cannot confirm this"; never a candidate, never KGWUI-012. Report keys are built as written with port cleared, so refs differing only by port share one check and refs differing by an omitted versus explicit namespace get two (Task 41; rules §7, §8.1) |
| Current status contradicts the computation | `pending` until 30 s after the check's fingerprint last changed, then `disagrees` and one KGWUI-012 warning on the object whose status disagrees (the Gateway or ListenerSet for a count, the route for a parentRef). It is raised by the scheduled deadline at exactly `since + 30 s`, with no other event; the finding's `since` is the window start. It clears in the same recompute once the check agrees, turns unknown or its subject goes. Annotation edits, unrelated condition writes, `resourceVersion`-only updates and metrics arrivals never reset the window; a listed field, the observed value or a rule-set switch does. Stale status is `pending` and never KGWUI-012 (Task 42; rules §8.3, §9; §11.5 rows 1–4, 7–10) |
| `GET …/health` | A `computed views` check appended after `backend status`, not required, always OK. KGWUI-012 can turn the install and the Gateway row `warning`, never `error` (Task 42; design §6.2, §6.3, §7.6) |
| `GET …/routes?parent=<Gateway or ListenerSet>&listener=<name>` | The listener's members only, with their effective hostnames; `listener=unknown` lists rows whose ref to that parent is unknown; a name absent on the parent gives an empty page; `listener` without a parent, or with an HTTPRoute parent, is 400. `state=disagrees` and `state=listener-unknown` filter rows (Tasks 43–44; design §7.2, §6.1) |
| `GET /api/v1/meta` | Advertises the `listener` filter, so the SPA sends it only when the server accepts it (strict params with capability gating, design §7.2). The field's form is not fixed by the design: an additive optional field (inferred) |
| Route list rows and `GET …/routes/{…}` | `ParentSummary.computed` holds an `Attachment` (outcome, listeners, explanation such as "listener https has *.example.org; route has shop.example.com", `Computed`); `RouteDetail.attachments` has one entry per Gateway or ListenerSet parentRef with per-listener effective hostnames; HTTPRoute parentRefs get none. Hostnames appear only in state `checked` or `disagrees` (Task 44; design §7.3, §7.5) |
| Gateway and ListenerSet detail | Each listener carries `computedRoutes`, `computed` and `isolationNote`; status `attachedRoutes` is sent unchanged next to it; `disagrees` sends both numbers; `unknown` sends a reason and no `computedRoutes`; `ruleSet` is set on every detail payload. The existing visible-ListenerSet count (`GatewayDetail.listenerSets`) is unchanged (Task 44) |
| Gateways and ListenerSets lists | The `routes` column and `sort=routes` land together and use one value: the computed count of distinct member routes, recounted per caller. `disagreements` counts checks in state `disagrees` on that object. A row whose count is omitted (rule 5) sorts after rows with a count (inferred) (Tasks 43–44, 47; design §7.3) |
| A caller who may not view a dependency (a `Selector` listener over a hidden Namespace, a hidden member) | Membership, count and check are `restricted` with no value, reason or note; `computedRoutes` and `routes` are omitted; no `listener-unknown`; `listener=` and `state=listener-unknown` return the rows and `total` they return when those facts are unknown; `deniedInInstall` is unchanged; a KGWUI-012 with hidden evidence gets `evidenceRestricted`. Health and row states are unchanged. `AllowAll` makes zero Authorizer calls (Task 44; design §7.4 rules 4–7, 9, 10) |
| SPA, every computed value | A `ComputedBadge` ("Computed" chip, tooltip naming rule and rule set, keyboard-reachable and in the accessible name), then the state as icon + text; only `count-agrees` and `status-compatible` use the healthy icon; `not-verified` is neutral; controller values carry no badge. Stale data turns badges unknown; notices keep their icons (Task 46; design §5.2, §8.4, §8.6) |
| SPA pages | Listener table: status count and computed count with its check, fixed column widths, a skeleton at the final height. Selecting a listener on the Gateway page sets `?listener=`, filters attached routes to its members with effective hostnames, survives reload, and back clears it. Failed-route rows show the explanation; parent cards list computed listeners, each with its hostnames marked "Not checked by the controller"; the Routes state filter offers `disagrees` and `listener-unknown`. Table text cells stay one line with a tooltip; links follow the server's `link` (Task 47; design §8.1, §8.5, §8.7) |
| kind e2e on v2.3.11 and v2.4.7 | For every listener of every owned Gateway and ListenerSet, `computedRoutes == attachedRoutes` with `count-agrees`; every parent outcome `status-compatible`; the quirk route `NoMatchingParent`; the two-ref route counts twice; no KGWUI-012. Forced disagreement (UI at `--poll-interval=10m`, controller at 0 replicas, a `Selector` Namespace relabelled): KGWUI-012 on the route and the Gateway no earlier than `since + 30 s` and within `since + 90 s`; it clears within 2 min of scaling back (Task 50; design §9.5) |

**Counts and colours.** Status counts and ownership do not change. KGWUI-012 is new and can turn an install yellow, never red. `sort=routes` on the Gateways list changes its basis from every listed route (2.2's `GatewayRouteCount`) to the computed member count. The README upgrade notes and the release note say so (Task 51).

## View A observations (D-021)
- **Item 10** (both tags): `attachedRoutes` never counts the delegated child `c10`; route `lsr` counts 1 on ListenerSet listener `extra` and route `xlsr` 1 on XListenerSet listener `legacy`, not on the Gateway's listeners. Confirms rules §4.4 and §4.1 (a Gateway ref never reaches ListenerSet listeners). No rule change.
- **Item 3** (both tags, as rules §7 states): explicit children with `sectionName` or `port` get entries as written on 2.4; on 2.3 the `sectionName` child gets none and the `port` child an entry with its port. It confirms report keys, used in 2.4; in 2.3 its manifest seeds the `sectionName` quirk fixture (Task 50).
- No observation contradicts rules §2 or §4, so the Task 35 gate passes.

**Provenance gap.** No kgateway translator output carries `NotAllowedByListeners`, `NoMatchingParent` or `NoMatchingListenerHostname` at either tag. Those fixtures cite `computed-rules §<n>` plus `e2e`, and the kind e2e confirms them (Task 50).

## Root cause (if known)
n/a (planned work). Per-listener attachment is computed inside kgateway (`query/route.go` V4:437-547, V3:397-508; `query/query.go:231-296`) and published nowhere (design §3.4, §14.1).

## Acceptance tests
- Task 36: `TestReadExperimental`, `TestSettingsSourceOrder`, `TestSettingBool`, `TestNamespaceLabels`; existing scope tests unchanged.
- Task 37: `TestSelect`, `TestWithComputedRulesReachesTheEngine`; the boundary walk covers `pkg/computed`.
- Task 38: `TestAllowedKindsDefaults`, `TestAllowedKindsIntersection`, `TestNamespacesFrom`, `TestIntersect`.
- Task 39: `TestAttachMembersPerKind`, `TestAttachNamespaces`, `TestAttachNamespaceLabelsUnavailable`, `TestAttachSectionAndPort`, `TestAttachAllowedQuirk`, `TestAttachHostnamesPerListener`, `TestAttachTwoRefsOneListener`, `TestAttachGatewayRefNeverReachesListenerSet`, `TestAttachListenerSetNoStatus`, `TestAttachExperimental`, `TestAttachDeps`, `TestAttachFacts`, `TestAttachDeterministic` (rules §11.1).
- Task 40: `TestIsolationNote`, `TestRuleSetGoldens` over `pkg/computed/testdata/kgateway-2.{3,4}/` (one fixture per §11.1 row, plus the kgateway-provenance fixtures; a case without `provenance` fails).
- Task 41: `TestListenerCountAgreesAndDisagrees`, `TestListenerCountStaleIsPending`, `TestRouteAcceptedContradicts`, `TestRouteAcceptedLaterStagesCompatible`, `TestRefsDifferingByPortShareOneCheck`, `TestOmittedVsExplicitNamespaceTwoChecks`, `TestRejectionVsLaterStageReasonInconclusive`, `TestSharedKeyDifferentOutcomesInconclusive`, `TestUnknownIsNeverACandidate`, `TestNoteCarriesSelection`, `TestAttachmentFingerprints` (rules §11.4 view A rows); a `KeyOf` case asserting a written port leaves `Port` and `PortSet` cleared.
- Task 42 (fake clock): `TestDisagreementClearsBefore30s`, `TestMaturesAtScheduledDeadline`, `TestUnrelatedWritesDoNotReset`, `TestFindingSinceIsWindowStart`, `TestListedChangeResets`, `TestRuleSetSwitchResets`, `TestSubjectDeletedPrunes`, `TestTurnsUnknownClearsSameRun`, `TestKGWUI012IsWarning`, `TestNoFindingForUnknownStaleCompatible`, `TestComputedWithoutPodsOrUIDs` (design §9.7), `TestAttachmentInputScope`; `health.json` regenerated, `TestGoldenChangesAreAdditive` passes.
- Task 43: `TestByListenerMembers`, `TestListenerFilter`, `TestComputedFlags`, `TestDepSetsInterned`, `TestGatewayRowComputedCounts`, `TestBuildDeterministicWithViews`, `TestComputedPredicatesUseVisibleDeps`; a Gateways `sort=routes` case ordering by the computed count.
- Task 44: `TestRouteListCarriesAttachment`, `TestRouteDetailAttachments`, `TestGatewayDetailListenerStates`, `TestListenerParam`, `TestMemberCoverage`, `TestSelectorOverHiddenNamespaceIsRestricted`, `TestListenerFilterCannotProbe`, `TestHiddenMemberRestrictsCounts`, `TestKGWUI012EvidenceRestricted`, `TestNoteOnUnknownVersion`; a `/meta` case asserting the `listener` filter is advertised.
- Task 45: `TestViewAMatrixThroughAPI` (every non-`ruleOnly` fixture through the handlers); goldens `routes-by-listener.json`, `gateway-detail-restricted.json` and the regenerated 2.2 files; `TestGoldenChangesAreAdditive`, `TestGoldenLinksAgreeWithEndpoints`.
- Task 46: Vitest for `ComputedBadge` (coverage texts, restricted placeholder, keyboard tooltip, selection note, stale turns unknown); the TypeScript golden check of the Task 45 fixtures.
- Task 47: Vitest for listener tables, `?listener=`, `listener=unknown`, explanations, the isolation note, parent cards, unknown and restricted with no number, no badge on controller values, the Routes state filter, ListenerSet counts, and the Gateways routes column.
- Task 49: 2.3 budgets at Large (`bench/2.3.txt`).
- Task 50: `TestExtraArgsAppended`; `TestComputedAgreesWithController`; `TestForcedDisagreementRaisesKGWUI012`; Playwright on `infra/edge` with `?listener=https`, layout checked by bounding box; ui-loop recordings.

## Scope
In: `pkg/install` (settings), `pkg/computed` (seam, `kgw23`, `kgw24`, `rulesets`, check, fingerprint, fixtures), `pkg/engine` (`RulesFor`, views, persistence), `pkg/extension`, `pkg/app`, `pkg/health` (KGWUI-012, `computed views`), `pkg/explore` (listener index, computed flags, Gateway row counts and the routes sort), `pkg/api` (computed DTOs, `listener` param and its `/meta` advertisement, explanation, Authorizer rules 4–7), `internal/testutil` (`rulecase`, `GatewayWithListeners`), `internal/boundary`, `web/packages/shell` (`ComputedBadge`, types, listener tables, Gateway, ListenerSet, Route and Routes pages, Gateways routes column, `copy/explorer.ts`), the chart's `extraArgs`, `test/e2e`, `web/e2e`, `.github/workflows/e2e.yaml`, README, `docs/`.
Out: views B and C, the delegation endpoint and tab, effective matchers, `child-entry` and `parent-resolvedrefs` checks, KGWUI-011's computed and orphan triggers, the wildcard setting (all 2.4); reading `/snapshots/xds` (§3.7); computing 2.4's listener isolation into the count (note only); changes to ownership, status counts or health roll-up; push to the SPA; incremental index updates.

## Plan / design impact
Rulings made at setup (controller, 2026-10-24), each widening a task's file list:
- **`/meta` advertises list filters added after v1** as an additive `filters` object: list name → filter names, for example `{"routes": ["listener"]}` **(our call)**. Tasks 44–45 add it to `pkg/api/types.go` and the meta goldens; Tasks 46–47 gate the SPA control on it in `src/api/hooks.ts`. Design §7.2 records the form.
- **`sort=routes` on Gateways** switches to the computed member count with the routes column, in one task (Task 43 server, Task 47 adds `GatewaysPage.tsx`). Rows whose count is restricted sort after rows with a count **(inferred)**.
- **`?listener=` stays in the URL** on the Gateway page, as design §8.1's page table says. It selects a listener, not a sub-table filter, so it is the one exception to the page-local rule; design §8.1 says so.
- **Wording record:** `phase-2.3-wording.md`, as in 2.2 (Task 48).
- Task 35 Step 2 has nothing left to change (computed-rules §10 already reads "Answered (D-021)"); D-021 item 3 serves 2.3 only as the `sectionName` fixture manifest (Task 50).

## Known limitations
- **Implicit refs on 2.3 (PF15, user ruling).** On kgateway 2.3, a child whose parentRef omits the namespace or sets `sectionName` keeps the KGWUI-011 warning. View A covers top-level routes only, so 2.3 does not change it; the 2.2 pin stays green.
- **Listener isolation (rules §4.5).** On 2.4, view A shows status semantics. The note says Envoy may serve fewer hostnames; the hostnames themselves are not narrowed.
- **Membership is not verified.** `count-agrees` checks the number, not which routes or hostnames; a parentRef that matched several listeners stays `not-verified` for which listener (rules §8.4).

## Budgets (Task 49)
- Warm `Compute` ≤ 120 ms (median of 20), cold ≤ 1 s, allocation per warm recompute ≤ 32 MiB (user, 2026-10-23; revisit at this close; 2.2 CI: 23.1 MiB), heap of index, computed views and decode cache ≤ 32 MiB above `bench/2.2-baseline-heap.txt` (2.2 CI: +7.2 MiB), sustained stream as Task 32, `persistence.step` time reported.
- A real `parent=<gateway>&listener=<name>` list mix replaces 2.2's ListenerSet-parent stand-in (`pkg/api/budget_test.go:143`).
- Recheck the 20-client restricted `sort=health` p95 (2.2 CI: 73.5 ms of 100 ms) with computed listeners in the index.

## Exit criteria
From the plan's Sub-phases table: D-021 view A items applied; `pkg/computed` seam and both rule sets with per-version fixtures; experimental setting read; `listener-count` and `route-accepted` checks; KGWUI-012 persistence with scheduled maturation; listener and attachment DTOs, `listener` filter, Authorizer rules 4–7 for view A; goldens (Go and TS); `ComputedBadge` and pages; rules §11.1, §11.4 (view A rows), §11.5 rows 1–4, 7–10; wording pass; 2.3 budgets at Large; kind e2e: computed equals `attachedRoutes` on both tags and a forced disagreement; Codex review triaged.

## Files
- Task 35: this brief, `docs/phase-status.md`; only if D-021 required it: `computed-rules.md` (§2, §4, §10, §11.1), `docs/decisions.md`
- Task 36: `pkg/install/settings.go` and test, `pkg/install/scope.go`, `internal/boundary/imports_test.go`
- Task 37: `pkg/computed/{rules,attach,delegation}.go`, `pkg/computed/kgw23/rules.go`, `pkg/computed/kgw24/rules.go`, `pkg/computed/rulesets/rulesets.go` and test, `pkg/engine/engine.go`, `pkg/extension/extension.go` and test, `pkg/app/app.go`
- Task 38: `pkg/computed/kgw23/{allowed,hostnames}.go` and tests
- Task 39: `pkg/computed/kgw23/attach.go` and test, `pkg/computed/kgw23/rules.go`
- Task 40: `pkg/computed/kgw24/{allowed,hostnames,attach,isolation}.go` and tests, `pkg/computed/kgw24/rules.go`, `pkg/computed/golden_test.go`, `internal/testutil/rulecase.go`, `pkg/computed/testdata/kgateway-2.3/*.yaml`, `pkg/computed/testdata/kgateway-2.4/*.yaml`
- Task 41: `pkg/computed/{check,fingerprint}.go` and tests
- Task 42: `pkg/engine/{views,persist}.go` and tests, `pkg/health/computed.go` and test, `pkg/computed/rules.go`, `pkg/engine/engine.go`, `pkg/health/{finding,checks,messages}.go`, `internal/testutil/fixtures.go`, `pkg/api/testdata/golden/health.json`
- Task 43: `pkg/explore/{explore,rows,query}.go`, `pkg/explore/{explore,query}_test.go`, `pkg/engine/engine.go`
- Task 44: `pkg/api/explain.go`, `pkg/api/computed_test.go`, `pkg/api/{explore_types,params,lists,details,visible,handlers}.go`
- Task 45: `pkg/api/golden_test.go`, `pkg/api/computed_test.go`, `pkg/api/testdata/golden/{routes-by-listener,gateway-detail-restricted}.json`, regenerated `gateway-detail`, `routes`, `gateways`, `listenersets`, `listenerset-detail`, `route-detail`
- Task 46: `web/packages/shell/src/explore/ComputedBadge.tsx` and test, `src/api/exploreTypes.ts`, `src/api/types.golden.test.ts`, `src/index.ts`, `src/copy/explorer.ts`
- Task 47: `src/pages/explore/gateway/ListenerTable.tsx`, `src/pages/explore/{GatewayPage,ListenerSetPage,RoutesPage}.tsx`, `src/pages/explore/route/ParentCard.tsx`, `src/pages/explore/{details,route,lists}.test.tsx`
- Task 48: `src/copy/explorer.ts`, `pkg/api/explain.go`, `pkg/health/messages.go`, `pkg/computed/rulesets/rulesets.go`, the components of Tasks 46–47
- Task 49: `pkg/engine/budget_test.go`, `pkg/engine/bench_test.go`, `pkg/api/budget_test.go`, `docs/phases/phase-2/bench/2.3.txt`
- Task 50: `deploy/helm/kgateway-ui/{values.yaml,templates/deployment.yaml,README.md}`, `deploy/helm/chart_test.go`, `test/e2e/setup.sh`, `.github/workflows/e2e.yaml`, `test/e2e/fixtures/explorer.yaml`, `test/e2e/api_test.go`, `web/e2e/explorer.spec.ts`
- Task 51: `docs/phases/phase-2/phase-2.3-complete.md`, `docs/phases/phase-2/reviews/2.3-codex.md`, `README.md` (Upgrade notes), `docs/phase-status.md`, `docs/roadmap.md`, `design.md` and `computed-rules.md` only where the code deviates

## Notes
- Every new UI and server string goes through the five-option picker (design §8.8); choices are recorded in a 2.3 wording record next to `phase-2.2-wording.md`, with copy in `copy/explorer.ts`.
- Each rule-set fixture's expectation is written from its provenance, never generated; `kgw24` starts as a copy of `kgw23` and `kgw23` is never edited for 2.4.
- Public repo: no vendor or commercial edition names; say "downstream distributions".

## Definition of done
- [ ] every task's tests fail before and pass after, or are reported as guards (reported per task)
- [ ] full test suite passes (Go with `-race`, chart, web, kind e2e on v2.3.11 and v2.4.7, including the forced-disagreement row)
- [ ] 2.3 budgets met at Large, heap against `bench/2.2-baseline-heap.txt`; the allocation budget revisited with the user
- [ ] cross-model review of `git diff <base>..HEAD` + triage
- [ ] phase-2.3-complete.md written (with the release note on KGWUI-012 and the routes sort); phase-status.md and roadmap updated
