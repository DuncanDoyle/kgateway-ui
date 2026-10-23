Cross-model: YES (author: Claude Opus 5.5, reviewer: Codex, command: codex exec --sandbox read-only)
Scope: implementation plan
Diff: n/a (plan review of docs/phases/phase-2/implementation-plan.md at 3fd76c7)
Timing: on time

I would return the plan for revision. The staging matches the approved design, but several tasks prescribe incorrect behavior or leave contracts unresolved.

I reviewed all 70 tasks against the spec, current code, and source at `v2.4.7` and `v2.3.11`. This was a static review; I did not edit files or run builds.

1. **P1 — Task 19, Step 3: `name.desc` returns ascending results.**  
   [The implementation instruction](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:850) skips sorting whenever the key is `name`, because rows are already ordered. They are ordered ascending. Consequently `name.desc` returns the wrong order, and cursor searches using descending comparisons operate on an incorrectly ordered slice. Skip sorting only for `name.asc`, or reverse the order appropriately.

2. **P1 — Tasks 37/41/59: the inputs cannot preserve kgateway’s report keys.**  
   [Task 41](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:1418) defines `KeyOf(objects.ParentRef)`, but `objects.ParentRef` has already defaulted omitted group, kind and namespace. `objects.RouteParents` also defaults status references. Both upstream tags key reports **as written**, with nil represented by empty strings: `pkg/reports/reporter.go`, V4:424–450 and V3:369–394.  
   Two refs differing by omitted versus explicit namespace can therefore collapse into one UI check despite being separate controller reports. Task 55 adds written **spec** refs too late for independently shipped 2.3, and does not supply written status refs. Preserve both forms before implementing view A; use resolved identities for object lookup and written identities for reports.

3. **P1 — Task 2: the proposed scale helper fails the mandatory boundary gate.**  
   [Its signature](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:185) requires `corev1.Namespace` in the new non-test file `internal/testutil/scale.go`. That file is absent from the per-file allowlist, and Task 2 does not modify the allowlist. [The boundary test](/Users/ddoyle/Development/claude/kgateway-ui-claude/internal/boundary/imports_test.go:26) excludes `_test.go` files, not all test-support directories. Move the typed portion into an existing allowed helper or include a reviewed test-support exception.

4. **P1 — Tasks 1/33: the e2e environment cannot create all required fixtures.**  
   [Task 1 uses the existing setup unchanged](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:164); Task 33 only specifies applying the explorer fixture. `test/e2e/setup.sh` installs `standard-install.yaml`. The required experimental ListenerSet/XListenerSet and 2.3 TCPRoute resources need additional CRDs. The pinned kgateway CRD chart contains kgateway CRDs, not those Gateway API CRDs. Specify the version-appropriate experimental CRD installation before the observations, then retain it for the e2e matrix.

5. **P2 — Task 6: migration to `Current() *Published` misses existing consumers.**  
   [The modification list](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:378) omits `Engine.Installs()`, which accesses `Current().Installs`, and the existing assertion at `engine_test.go:136`. Both require migration. Also explicitly preserve `OnCompute`: `app.Build` uses it to close `discovered`, without which the metrics collector never starts. Adding `OnPublish` does not replace that lifecycle obligation.

6. **P2 — Task 6: confirmation and public `Compute()` have an unresolved publication race.**  
   [Step 3](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:437) makes the scheduler the owner of running state, but leaves `Compute()` publicly callable and publishing under a separate lock. A direct compute can publish while an idle confirmation holds an older `p`; the confirmation can subsequently store `Published{p.Model,…}` and restore the older model. Atomic stores prevent a data race, not this lost update. Specify scheduler-exclusive publication, serialization, or a compare-and-swap that cannot overwrite a newer publication.

7. **P2 — Task 6: metrics identity does not identify exactly the evidence read.**  
   [The fingerprint definition](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:393) hashes install ID, collection timestamp and error, but neither the `Get` presence boolean nor the metric values. `ctrlmetrics.Source` promises no unique timestamp per result. Two different results at one fake-clock instant—or supplied by an extension—can compare equal. Define a result revision contract or hash the relevant result and presence. Confirmation must use the same identity calculation.

8. **P2 — Tasks 14/15: HTTPRoute existence is missing from the missing-parent change.**  
   [Task 14 adds HTTPRoute to `parentKinds`](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:650), and Task 15 reuses the missing-parent branch. In current `assign.go`, `exists` contains only Gateways and ListenerSets. Adding the kind without populating HTTPRoute existence classifies references to existing HTTPRoutes as missing and adds KGWUI-003, including on valid delegated children. Explicitly build the existence index before this branch.

9. **P2 — Tasks 41/42: reject the non-attachment `Accepted=False` interpretation as currently specified.**  
   [Task 41](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:1428) treats a non-attachment failure as contradicting computed rejection; Task 42 deliberately tests that behavior. Rules §8.1 says later-stage failures do not establish attachment disagreement. The distinction matters when parentRefs share a report key: one ref can fail attachment while another reaches translation, whose later condition replaces the earlier condition. Both tags’ `SetCondition` replace a condition of the same type.  
   This needs an explicit spec resolution accounting for overwrite order, rather than assuming a later-stage reason disproves attachment rejection.

10. **P2 — Task 42: the persistence result cannot supply the required finding `since`.**  
    [The persistence interface](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:1458) exposes state/maturity mutations and the next deadline. `CheckResult` has no window-start field or accessor, yet `health.computedCheck` must emit `Since` equal to that start. The map is private to `engine`; `health` cannot retrieve it. Add the missing output contract. The same issue affects computed KGWUI-011.

11. **P2 — Tasks 56/57: completeness consumes a graph whose required coverage is unspecified.**  
    [Task 57](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:1824) computes closure using only `DelegationResult.Edges`. Task 56 describes traversal through kept children and selection rows for unreached **owned** parents. That does not establish that edges below a dropped, non-owned child—or beyond a context/matcher cut—exist in this graph.  
    Rules §8.5 requires selection-and-permission reachability independently of matcher fit. Explicitly produce that graph, including uncertainty below non-owned routes, or let completeness obtain the selector inputs. Otherwise incomplete negative conclusions can become KGWUI-011/012 findings.

12. **P2 — Tasks 43/44/63/64: dependency authorization is not fully integrated into filtering.**  
    [The query interfaces](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:2008) accept object visibility, while listener membership and delegation outcomes can depend on hidden objects. Redacting the resulting DTO is insufficient: filtering by `listener=https` or `outcome=dropped` can expose a hidden Namespace’s labels or ancestor match through row presence and `total`. Task 44 explicitly handles this for `listener-unknown`, but leaves the other computed predicates unresolved. Specify caller-visible predicate evaluation **before** totals and paging, and test those probes.

13. **P2 — Tasks 17/42 versus Tasks 25/45: existing golden changes exceed the plan’s stated allowance.**  
    [Task 42 says goldens remain unchanged](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:1489) because there is no disagreement. However, registering an always-OK `computedCheck` adds an entry to `health.checks`; `health.Evaluate` appends every registered check’s result. Task 17 similarly adds a backend check. Registering Backend also adds an overview count tile even when its count is zero.  
    These can be legitimate additive inventory changes, but they are not merely the optional fields enumerated by §7.6. Reconcile the approved golden delta list and specify stable handling of array additions.

14. **P2 — Several task interfaces or file lists rely on later work.**  
    Concrete examples:
    
    - Task 26 imports and tests fallback pages created only in Tasks 27–29, yet requires all web checks to pass.
    - Task 42 consumes `explore.Input.Computed`, produced in Task 43.
    - Task 53 adds fields to `DelegationInput`, declared in `rules.go` by Task 37, without listing that file for modification or specifying relocation.
    - Task 60 uses fixture `bounds:` overrides, but Task 40’s `RuleCase` has no bounds field.
    - Task 42 locates `health.Input.Computed` in `checks.go`; the real `Input` declaration is in `finding.go`.

    These conflict with the plan’s exact interface/file-list contract. Introduce the prerequisites earlier or explicitly combine the dependent changes.

15. **P2 — Sub-phase setup and close tasks are incomplete lifecycle instructions.**  
    [Tasks 10/34/51/70](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:539) provide reviews and completion records, but the setup tasks do not explicitly record each sub-phase’s base SHA and active status. Review commands retain unresolved `<base>` placeholders. Close tasks omit roadmap updates; Tasks 51/70 list README upgrade notes without a step to write them. The plan also omits design Q7’s mandatory research refresh before plan approval. The four slices are sensible, but these omissions prevent their stated close procedure from being complete.

16. **P3 — Task 6: `HasWaiters` cannot make these scheduler tests deterministic.**  
    [The proposed `StepUntil`](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:392) waits for any fake-clock waiter. The repeating confirmation ticker itself keeps a waiter registered, so this says nothing about whether the loop consumed a trigger, armed the debounce timer, or processed completion. Clock advances can overtake scheduler work. Add explicit acknowledgements/barriers for those transitions; distinguish confirmations, initial publication and recompute publications when counting runs.

17. **P3 — Task 9: the wiring test can pass with informer notifications disconnected.**  
    [The test](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:519) excludes the safety timer by setting it to an hour, but the one-second confirmation scan will still discover the changed store and schedule a recompute. It therefore does not prove trigger wiring. Hold confirmation before its first tick and require publication through the debounce path, or otherwise isolate the trigger.

18. **P3 — Task 25: the golden link test asserts invalid endpoint behavior.**  
    [Step 3](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:1008) requires a `yaml` link to imply detail 404. A no-parent route rendered with a YAML link can also qualify for detail through finding/reference admission—the spec explicitly permits this. Conversely, certificate refs have `link:none` as a presentation rule; that does not make the explorer’s route-detail URL a meaningful endpoint for them. Test that the advertised destination works and denied/absent objects remain inaccessible, rather than assuming links are a bidirectional encoding of every endpoint’s availability.

19. **P3 — Tasks 42/50: maturation tests need to exclude the safety fallback.**  
    [The forced-disagreement test](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:1654) matures after 30 seconds, matching the default safety interval. It can pass without a working maturation deadline. The fake-clock deadline test also needs an explicitly later safety instant. Additionally, the e2e’s three-second upper margin is sensitive to loaded CI scheduling. Prove the exact instant deterministically; use bounded eventual behavior and cleanup restoration in the real-cluster test.

20. **P3 — Tasks 60/65: bound cases are excluded from the required API boundary coverage.**  
    [Task 60 marks bound fixtures `ruleOnly:true`](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:1910), excluding them from Task 65. Task 64 names projection assertions, but does not specify a cluster-input fixture that reaches these bounds through the real engine. Thus the API path can incorrectly discard truncation/completeness metadata while rule tests still pass. Keep representative cut, empty-retention and hidden-context cases in the handler matrix.

21. **P4 — Tasks 56/58: retention limits apply only after potentially large temporary materialization.**  
    [Task 56 retains every context first](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:1794); Task 58 subsequently aggregates and trims them. A flatten call can contribute contexts to many child rows, so the flatten-call cap does not itself bound this intermediate storage to the retained-context budget. Specify streaming aggregation with bounded retained samples, while preserving full dependency and completeness information.

On the plan’s explicit spec-gap resolutions:

| Resolution | Review |
|---|---|
| `computed.Selection{Rules, Note}` with factory in `computed/rulesets` | **Accept.** The factory placement avoids the production import cycle. |
| Pre-2.3 controllers use 2.3 rules with an unverified note | **Accept** as a conservative fallback, without claiming support for those versions. |
| Child-less unresolved rows | **Accept.** Add an explicit test that authorization does not discard the row by checking a zero child reference. |
| `no-matches` reason | **Accept.** It explains explicit empty matches without changing matcher behavior. |
| `ClassTrigger` for computed KGWUI-011 | **Accept**, provided maturity remains separate from check state and it never creates KGWUI-012. |
| Computed rejection versus non-attachment `Accepted=False` | **Object** as specified; see finding 9. |
| Unknown settings, invalid booleans, conservative label-selection over-approximation | **Accept.** Unknown inputs must remain uncertainty, not actual selection or negative evidence. |
| Boundary exception for `pkg/install/settings.go` | **Accept.** It belongs to the existing Kubernetes provider; keep its outputs neutral. It does not cover the separate `scale.go` violation. |

I found no substantial product scope creep: mirror/defaulted-weight fields, reverse lookup, allowed-listeners hints and the wording passes implement approved requirements. The plan’s main proportion problem is its repeated FAIL/PASS/commit transcript, while the contracts above remain undecided. Shortening that repetition would help; the tasks that refer directly to complete algorithms in `computed-rules.md` do not need those algorithms copied into the plan.
## Triage
| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | `name.desc` returns ascending | reviewer | accept | Skip the sort only for `name.asc`. |
| 2 | Inputs lose written report keys | reviewer | accept | Keep written spec and status parentRefs alongside resolved identities from 2.2 (typed views); reports keyed as written. |
| 3 | Scale helper breaks the boundary gate | reviewer | accept | Typed Namespace construction moves to an allowlisted test-support file with a reviewed entry, or into `_test.go`. |
| 4 | e2e lacks experimental CRDs | reviewer | accept | Version-appropriate experimental Gateway API CRDs installed before the observations and kept for the e2e matrix. |
| 5 | `Current()` migration misses consumers | reviewer | accept | Migrate `Engine.Installs()` and `engine_test.go:136`; keep `OnCompute` for `app.Build`. |
| 6 | Public `Compute()` and confirmation race | reviewer | accept | Scheduler-exclusive publication; confirmation publishes by compare-and-swap that never overwrites a newer model. |
| 7 | Metrics identity incomplete | reviewer | accept | Hash presence and the result content used by checks, the same function for snapshot and confirmation. |
| 8 | HTTPRoute existence missing | reviewer | accept | Build the HTTPRoute existence index before the missing-parent branch. |
| 9 | Non-attachment `Accepted=False` as contradiction | reviewer | accept | Our call: same-type conditions overwrite, so a non-attachment `Accepted=False` against a computed rejection, or any key shared by parentRefs with different computed outcomes, is inconclusive, never KGWUI-012. Recorded in computed-rules §8. |
| 10 | Persistence cannot give `since` | reviewer | accept | `CheckResult` carries the window start; health reads it through the computed input. |
| 11 | Completeness graph coverage unspecified | reviewer | accept | Task 56 produces the selection-and-permission reach graph incl. uncertainty below non-owned routes, independent of matcher fit. |
| 12 | Computed filters can probe | reviewer | accept | Computed predicates evaluated caller-visibly before totals and paging; probe tests for `listener=` and `outcome=`. |
| 13 | Golden deltas exceed the allowance | reviewer | accept | Enumerate the additive inventory changes (new checks, Backend tile) in design §7.6 and the plan; stable ordering. |
| 14 | Interfaces rely on later tasks | reviewer | accept | Reorder or merge the listed tasks; fix file lists. |
| 15 | Lifecycle steps incomplete | reviewer | accept | Setup tasks record base SHA and status; review commands use real SHAs; close tasks update roadmap and README notes. Q7 research ran 2026-10-23 (design addendum 172a5a4). |
| 16 | Fake-clock waiters not deterministic | reviewer | accept | Explicit scheduler acknowledgements per transition. |
| 17 | Wiring test passes without informers | reviewer | accept | Hold confirmation; require publication through the debounce path. |
| 18 | Golden link test asserts invalid behaviour | reviewer | accept | Test that advertised destinations work and denied/absent stay inaccessible. |
| 19 | Maturation tests depend on the safety timer | reviewer | accept | Safety instant later than maturation in fake-clock tests; bounded eventual checks in e2e. |
| 20 | Bound cases excluded from API matrix | reviewer | accept | Representative cut, empty-retention and hidden-context cases in the handler matrix. |
| 21 | Retention after large materialization | reviewer | accept | Streaming aggregation with bounded samples, keeping dependency and completeness data. |
| — | Spec-gap resolutions | reviewer | accept 7, object 1 | The seven accepted go into computed-rules.md; the eighth is #9. |
