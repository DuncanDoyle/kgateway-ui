Cross-model: YES (author: Claude Opus 5.5, reviewer: Codex, command: codex exec --sandbox read-only)
Scope: design (major revision after user review: D-019, D-020)
Diff: 412c0ab..60188b2
Timing: on time

Reviewed `412c0ab..60188b2`, the earlier findings and triage, D-019/D-020, and both kgateway tags. **Found 3 P1, 8 P2, 1 P3 and 1 P4 findings.** No files edited. D-020’s mirroring override is treated as authorized.

1. **P1 — Computing delegation once per edge loses the matcher context that determines attachment.**

   **Design:** [§5.2:410](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:410), §5.3:433.

   The design computes each `(parent, ref, child)` once and walks paths only for cycles. But delegation outcomes depend on the parent’s matches **after ancestor filtering or inheritance**.

   Example: root R delegates under `/api` to P. P has matches `/api` and `/other` on a rule delegating to C, whose match is `/other/item`. R filters P down to `/api`; kgateway therefore drops C. Evaluating P→C from P’s original matches incorrectly attaches C through `/other`.

   **Evidence:** `v2.4.7:pkg/kgateway/translator/httproute/delegation_helpers.go:91–102` replaces the cloned child’s rules with filtered matches; `delegation.go:93–95` recursively translates that clone. Equivalent behavior: `v2.3.11:…/delegation_helpers.go:92–103`, `…/delegation.go:100–102`.

   **Why:** Both view B and its cross-check can be wrong on ordinary multilevel delegation. Carry bounded matcher context through the traversal, or report context-dependent outcomes as unknown.

2. **P1 — The cross-check expects stronger conditions than successful attachment establishes.**

   **Design:** [§3.4:178](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:178), §5.4:441–446.

   An attached child is expected to have `Accepted=True`, `ResolvedRefs=True` and, on 2.4, `Programmed=True`. Attachment does not establish backend validity or successful rule translation. A correctly attached child with a missing Service legitimately has `ResolvedRefs=False`. Invalid matchers can legitimately produce `Programmed=False` on 2.4 or `Accepted=False` on 2.3. View A likewise cannot interpret every `Accepted=False` as an attachment disagreement.

   **Evidence:** `v2.4.7:pkg/kgateway/translator/httproute/gateway_http_route_translator.go:214–216` reports backend errors after attachment; `pkg/kgateway/query/utils.go:45–57` sets `ResolvedRefs=False/BackendNotFound`. Rule failures are reported at `v2.4.7:…/translator/irtranslator/route.go:325–350` and `v2.3.11:…/translator/irtranslator/route.go:291–313`. TCP conflict rejection also happens later: `v2.4.7:…/translator/listener/gateway_listener_translator.go:452–466`; 2.3:449–463.

   **Why:** Correct computations will produce persistent KGWUI-012 warnings on already broken configurations. Compare only evidence attributable to the modeled stage; unrelated failure conditions must not count as disagreements.

3. **P1 — The 2.3 “port means no child status” claim is wrong, and the version exception is incomplete.**

   **Design:** [§5.4:441](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:441), §5.4:442.

   With explicit group, kind and namespace, no sectionName, and a port, a 2.3 child can receive status. Its synthesized report ref omits port, but the report lookup key also omits port. The builder then emits the spec ref, including its port.

   **Evidence:** `v2.3.11:pkg/kgateway/query/route.go:252–259`; `pkg/reports/reporter.go:369–394`; `pkg/reports/status.go:477–484,513–518`. The 2.4 lookup also omits port: `v2.4.7:pkg/reports/reporter.go:424–449`.

   Conversely, the namespace/sectionName mismatch affects **hostname-rejected children too**, not just the “attached” row: 2.3 creates their synthesized reporter before validating hostnames (`v2.3.11:…/translator/httproute/delegation.go:82–96`).

   **Why:** The rule set will flag valid port-bearing status as unexpected and expect rejection status that 2.3 cannot emit. Apply version-specific report-key behavior consistently across outcomes.

4. **P2 — Per-ref outcomes are compared with status that aggregates across refs and matches.**

   **Design:** [§5.4:439–448](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:439).

   Suppose P delegates to C from two rules: one drops C, the other attaches it. C correctly has a P status entry. The first row nevertheless expects “no entry from P.” Similarly, multiple backend failures write the same parent `ResolvedRefs` condition; separate refs cannot each demand their own final reason.

   **Evidence:** Both tags invoke delegation per parent match: `pkg/kgateway/translator/httproute/gateway_http_route_translator.go:119–120,190–208`. Reports are keyed by parent identity and section, not rule/ref/match: `v2.4.7:pkg/reports/reporter.go:424–449`; `v2.3.11:…:369–394`. Condition writes update the same condition type: 2.4:505–512; 2.3:447–454.

   **Why:** A correct partial drop becomes a disagreement. Aggregate expected signals at the controller’s reporting granularity before comparing; retain individual outcomes as computations whose status may be inconclusive.

5. **P2 — “Agrees” overstates what the available cross-checks verify.**

   **Design:** [§3.4:184](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:184), §5.3:433, §8.4:789–790.

   Equal listener counts do not verify route membership or effective hostnames. Selecting the wrong route while preserving cardinality passes. Child status also contains no effective matcher, so a wrong inherited path or header merge can pass every proposed check.

   **Evidence:** Counts are published from `len(res.Routes)` at `v2.4.7:pkg/kgateway/translator/gateway/gateway_translator.go:110–120`. Matcher computation occurs separately at `…/httproute/delegation_helpers.go:117–140`; the same distinction exists on 2.3. Route report keys contain no matcher information at either tag.

   **Why:** D-020 authorizes a checked computation, but the healthy “Agrees” badge can imply validation of facts that were never observed. Record check coverage explicitly: “count agrees,” “status compatible,” and “effective matcher not independently verified.”

6. **P2 — Requiring current status prevents KGWUI-012 from detecting missing child status.**

   **Design:** [§6.2:522–525](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:522), §4.3:350, §9.1:877, Q12.

   A computed-attached child with no controller entry has no `observedGeneration` to satisfy the current-status requirement. An implicit child with no other status therefore never raises the KGWUI-012 promised by Q12 and the acceptance test.

   **Evidence:** At both tags, the child reporter is created only after matcher filtering: `v2.4.7:…/translator/httproute/delegation.go:49–81`; 2.3:51–83. No entry means no condition-generation evidence.

   **Why:** The principal computed-only-child failure is excluded by the finding predicate. Define separate evidence requirements for missing expected status, stale status, and contradictory current status.

7. **P2 — The 30-second persistence state lacks a dependency and expiry contract.**

   **Design:** [§6.2:523–526](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:523), §3.1:116, §9.3:898–906.

   “Any input of that check” does not identify which fields reset persistence. Treating whole-object changes as inputs lets unrelated status or annotation updates indefinitely suppress a real discrepancy. The deadline list also omits KGWUI-012 maturation; idle discrepancies depend on an incidental safety recompute rather than a scheduled persistence deadline.

   **Evidence:** Objects carry changing resourceVersions into the snapshot (`pkg/cache/cache.go:269–275`), and the proposed decode cache explicitly uses them (§3.3:142). The engine currently has generation-based grace tracking only (`pkg/engine/engine.go:110–139`).

   **Why:** Q13 cannot yet specify predictable noise or detection latency. Define stable check identity, semantic dependency fingerprints, reset/clear behavior, and a scheduled maturation deadline. Unrelated writes and metrics arrivals should not restart the window.

8. **P2 — D-019 does not cover the existing timestamp consumers.**

   **Design:** [§3.1:124–129](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:124), §7.6:751–754.

   The confirmation scheme discusses `collectedAt`, but install summaries expose `computedAt`. Cluster landing cards, the install switcher and header use that field for staleness. With recomputes 30 seconds apart, they become unknown after 10 seconds even while confirmation advances.

   **Evidence:** `pkg/api/types.go:25–26`, `pkg/api/handlers.go:172`; `web/packages/shell/src/pages/ClusterLanding.tsx:131`, `shell/InstallSwitcher.tsx:51`, `shell/Header.tsx:161`. Health’s timestamp is also stored separately in the report and served at `pkg/api/handlers.go:220–222`.

   **Why:** Updating only model/overview freshness leaves existing screens inconsistent. Specify every response timestamp and consumer. Preserve `computedAt`’s documented meaning; any additive confirmation field also needs a compatibility/golden exception.

9. **P2 — Confirmation needs an atomic publication protocol, not merely an empty trigger queue.**

   **Design:** [§3.1:122–129](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:122), D-019:340,350.

   No protocol binds a confirmation timestamp to the exact Model and source revisions it confirms. A recompute snapshots data, an event arrives during computation, and the old snapshot is published. Its completion must not clear that later event or allow the confirm tick to stamp the published model as current. Independently mutating a timestamp on the published Model also violates the stated immutability contract.

   **Evidence:** Snapshot acquisition and publication are separated at `pkg/engine/engine.go:147` and `181–183`; `Current()` returns the shared pointer at 188–191. Cache stores and access states are read independently at `pkg/cache/cache.go:265–304`.

   **Why:** This can produce fresh timestamps on obsolete data. Specify revision/epoch tracking, running versus pending state, and atomic publication of the Model plus its confirmation metadata. HTTP polling must only read that metadata—the statement that polling “renews” it contradicts hung-engine detection.

10. **P2 — Authorization covers quoted evidence more clearly than derived facts.**

    **Design:** [§7.4:709–718](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:709), §7.3:680–681, §9.1:885.

    Removing denied Namespace labels from an explanation still exposes their selector result through listener membership. An allowed child’s `effective.parentMatch` and inherited headers can expose a denied parent’s matcher. Filtering only denied child rows does not address these dependencies. The acceptance requirement that flags remain unchanged further conflicts with hiding derived facts.

    **Evidence:** Namespace membership is evaluated at `v2.4.7:pkg/kgateway/query/route.go:475–479` and 2.3:435–439. Parent matcher values are copied into children at both tags’ `…/httproute/delegation_helpers.go:117–140`. Existing finding redaction checks only the finding’s resource (`pkg/api/handlers.go:121–124`), so an allowed child finding can also carry evidence from a denied parent.

    **Why:** Authorizing the displayed route is insufficient. Define dependency-based visibility for computed facts and finding evidence, including a restricted/unchecked wire representation, rather than only redacting quoted strings.

11. **P2 — The rewrite drops the earlier KGWUI-011 pending-check behavior.**

    **Design:** [§6.2:510–519](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:510), §6.3:548–556.

    The previous design explicitly made the resource-status check pending during a declared candidate’s grace period. The revision retains info severity but removes that requirement. A declared candidate is unowned, so existing owned-route evaluation does not supply it automatically; an info finding alone leaves the install and row healthy.

    **Evidence:** `412c0ab:docs/phases/phase-2/design.md:351` requires pending. Current `pkg/health/checks.go:316` evaluates owned routes; `pkg/health/rollup.go:23–39` ignores info findings when choosing health.

    **Why:** This regresses the earlier waiting-for-status semantics without a decision. Restore it for the status trigger, and explicitly decide whether computed dropped/forbidden outcomes should have the same grace behavior.

12. **P2 — The DTOs cannot express the promised per-listener route details.**

    **Design:** [§7.3:645](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:645), §7.3:675–681, §8.5:807–808.

    `RouteDetail` has no view-A attachment results: its `parents` contain conditions and target refs, but no computed listeners or effective hostnames. `ParentSummary` has one hostname array beside several listener names, losing which hostname belongs to which listener.

    **Evidence:** kgateway computes hostname intersections separately inside each listener iteration: `v2.4.7:pkg/kgateway/query/route.go:457–521`; 2.3:417–481.

    **Why:** The HTTPRoute page cannot render its specified parent cards or effective hostnames faithfully. Add results keyed by parentRef **and listener resource/name**, including check coverage and restricted/unknown states.

13. **P3 — Acceptance tests omit the cases most likely to invalidate the new guarantees.**

    **Design:** [§9.1:864–869](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:864), §9.3:899–906, §9.5:935–939.

    Add controller-backed cases for ancestor-pruned parent matches, the same child attached and dropped through different matches, attached children with backend failures, and a 2.3 port-only child ref. Add engine interleavings around snapshot, event arrival, publication and confirmation, plus persistence expiry without further events.

    **Evidence:** The tagged translation paths in findings 1–4 demonstrate these are distinct observable cases. Also, `pkg/ctrlmetrics/scrape.go:86–87` uses strict `age > maxAge`; the proposed exact-deadline test must verify the boundary and subsequent scheduling rather than assume equality is stale.

    **Why:** Current positive goldens can encode the same mistaken expectations as the implementation. “No KGWUI-012” on uncomplicated fixtures does not establish either computation fidelity or race safety.

14. **P4 — The cost bound covers path visits, but not selected-edge cardinality.**

    **Design:** [§5.2:410](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:410), §12:1034, §13:1046–1050.

    Many wildcard or label refs can each select many routes. Materializing every `(parent, ref, child)` produces quadratic output even if cycle traversal is bounded. Pagination limits responses, not index construction.

    **Evidence:** Selection returns whole matching collections at `v2.4.7:pkg/kgateway/query/route.go:327–344`; 2.3:287–304. The design stores these selected-child rows at §3.7:238.

    **Why:** “Linear in objects and parentRefs” is not a valid general bound. Cap total selected edges and matcher expansion, with explicit truncation, and benchmark dense selection separately from diamonds.

For **scope**, keep the authorized A/B/C requirements, but split implementation into independently reviewed sub-phases: D-019 and timestamp compatibility; status-based ownership plus explorer APIs; view A; then context-aware B/C and their diagnostics. Defer policies, graph layout and implementing any upstream state CRD. Move detailed rule tables and test matrices into a versioned rules specification so the main design has one authoritative scheduling, authorization and cross-check contract.

For **§15**, Q12 and Q13 need findings 4–7 resolved before their defaults are approvable. Q14’s timing defaults are reasonable starting values, but require publication-latency and deadline semantics. Q15 should suppress wildcard-only *computed* findings without suppressing an explicit declared-parent diagnostic. Q16’s explicit setting read is preferable to assuming experimental features are enabled. Q1/Q4’s deferrals, Q9’s keyset pagination and Q11’s conservative Backend freshness remain reasonable.
## Triage
| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | Per-edge delegation loses ancestor matcher context | reviewer | accept | Carry bounded matcher context through the traversal; outcomes needing unresolvable context are "unknown". |
| 2 | Cross-check expects more than attachment establishes | reviewer | accept | Compare only evidence of the modelled stage; backend and rule-translation failures are not disagreements. |
| 3 | 2.3 port rule wrong; version exception incomplete | reviewer | accept | Apply 2.3 report-key behaviour consistently, including hostname-rejected children. |
| 4 | Per-ref outcomes vs status aggregated per parent | reviewer | accept | Aggregate expected signals at the controller's reporting granularity; per-ref outcomes may be inconclusive. |
| 5 | "Agrees" overstates the checks | reviewer | accept | Show check coverage: "count agrees", "status compatible", "matcher not independently verified". |
| 6 | Current-status requirement hides missing child status | reviewer | accept | Separate rules for missing expected status, stale status and contradicting current status. |
| 7 | 30 s persistence lacks a dependency and expiry contract | reviewer | accept | Stable check identity, semantic fingerprints, reset and clear rules, a scheduled maturation deadline. |
| 8 | D-019 misses existing timestamp consumers | reviewer | accept | Specify every timestamp and consumer; keep `computedAt`'s meaning; any new field is additive with a golden exception. |
| 9 | Confirmation needs atomic publication | reviewer | accept | Revision/epoch tracking; Model and confirmation metadata published atomically; polling only reads it. |
| 10 | Derived facts leak denied objects | reviewer | accept | Dependency-based visibility for computed facts and finding evidence, with a restricted/unchecked wire form. |
| 11 | KGWUI-011 pending behaviour dropped | reviewer | accept | Restore pending during grace for the status trigger; decide it for computed outcomes. |
| 12 | DTOs cannot carry per-listener results | reviewer | accept | Results keyed by parentRef and listener, with coverage and restricted/unknown states. |
| 13 | Tests miss the riskiest cases | reviewer | accept | Add the listed controller-backed cases, engine interleavings and deadline boundary tests. |
| 14 | Selected-edge cardinality unbounded | reviewer | accept | Cap selected edges and matcher expansion, with truncation; benchmark dense selection. |
| — | Scope and structure | reviewer | accept (user, 2026-10-22) | All of A/B/C in Phase 2, staged: 2.1 D-019 and timestamps; 2.2 explorer APIs and pages on status; 2.3 view A; 2.4 context-aware B and C. Rules move to a versioned `computed-rules.md`. |

## Re-review (scoped, Codex, diff a35dc05..0c2dc5d)

Reviewed `a35dc05..0c2dc5d`, the binding triage, both revised specifications, D-019/D-020, and relevant source at both kgateway tags. **10 findings resolved; 4 not resolved.** Found **1 new P1, 5 P2 and 1 P3**. No files edited.

References below: **D** = [design.md](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md); **R** = [computed-rules.md](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/computed-rules.md). Line numbers refer to the reviewed HEAD.

**Findings 1–14**

| # | Result | Section/line evidence |
|---|---|---|
| 1 | **Not resolved** | R §5.1/§5.4, lines 141–148 and 193–206 correctly propagate ancestor-filtered or merged matches. However, unknown contexts can still leave a known negative outcome (§5.6:235–238), and D §6.2:354 permits a finding from “every known context.” See new finding N2. |
| 2 | **Resolved** | R §8.1:296–309 distinguishes attachment/delegation evidence from backend and rule-translation failures. §8.2:320 limits inference from shared `ResolvedRefs` conditions. |
| 3 | **Resolved** | R §7:278–290 correctly excludes port from report keys, allows port-bearing 2.3 status, and applies namespace/sectionName exceptions to attachment and hostname rejection alike. |
| 4 | **Resolved** | R §8.2:313–322 aggregates by controller report key across rules, refs, contexts and roots. Individual paths need not be independently confirmed. Truncation introduces a separate completeness problem, N2. |
| 5 | **Resolved** | R §8.4:339–347 distinguishes count agreement, status compatibility and unverified membership/matchers. D §5.2:292 explicitly leaves effective hostnames and matchers unverified. |
| 6 | **Resolved** | R §8.3:328–335 separates missing, stale and contradictory evidence. §9.3:385 defines missing-child maturation using grace and the parent’s current entry, without requiring a nonexistent child condition. |
| 7 | **Not resolved** | R §9:355–389 now specifies identity, fingerprints, clearing and deadlines. But §9.2:375 includes unrelated `ResolvedRefs` changes in `child-entry` fingerprints, contradicting the no-unrelated-reset requirement. See N4. |
| 8 | **Resolved** | D §3.1:110–122 covers existing timestamps and consumers, preserves `computedAt`, and adds optional `confirmedAt`. §7.6:564–568 records the golden exceptions. |
| 9 | **Not resolved** | D §3.1:93–106 and D-019:338–344 fix immutable publication, running/pending state and read-only polling. The revision protocol still permits confirmation during the store-update/callback gap. See N1. |
| 10 | **Not resolved** | D §7.4:519–528 establishes dependency filtering and restricted representations. R §5.1:145–146 nevertheless drops the dependencies that established root reachability. See N3. |
| 11 | **Resolved** | D §6.2:353 restores pending resource-status checks during declared-candidate grace. Line 354 explicitly decides that computed outcomes do not make checks pending. |
| 12 | **Resolved** | D §7.3:450–457 and 483–507 supplies attachments keyed by parentRef and listener, each with effective hostnames and computed state. |
| 13 | **Resolved** | R §11.2–11.4:435–473 adds the requested controller cases. D §9.3:678–688 adds publication interleavings, idle maturation and strict deadline-boundary tests. |
| 14 | **Resolved** | R §5.5:217–229 caps selected edges, contexts, matcher evaluations and retained contexts. D §9.6:730,749 adds dense-selection measurement. The correctness of checks after truncation remains N2. |

**New findings, ranked**

1. **N1 — P1: Post-update informer callbacks cannot establish the claimed confirmation invariant.**

   **Location:** D §3.1:84,91,99–104; D-019:338–341.

   The cache revision advances in an informer event handler *after* the informer store changes. Consider: published model at revision 5; store changes; callback has not yet run; scheduler remains idle with revision 5; confirmation republishes the old model with `ConfirmedAt = now`. The model does not match its cache, despite passing the prescribed equality check.

   **Evidence:** The project uses client-go `v0.36.2` (`go.mod:10`). Its `tools/cache/controller.go:639–644` updates the store before notifying the informer; `tools/cache/shared_informer.go:961–965` distributes notifications, and `:1356–1360` invokes registered handlers separately. Thus the callback counter is not synchronized with store mutation.

   **Why:** Atomic publication fixes consistency of the published tuple, but does not prove that tuple describes current source state. Callback backlog can renew freshness on obsolete data.

   Bind revisions to the data actually snapshotted—for example, an engine-owned store updated together with its revision—and test confirmation while notification delivery is paused after the informer store changes.

2. **N2 — P2: Truncated contexts can become false negative conclusions and findings.**

   **Location:** R §1:14; §5.5:226; §5.6:235–238; §8.2:319–320; D §6.2:354.

   R first says unknown results have no value and skip cross-checks. Later, a row with unknown paths retains its known outcome. The child-entry aggregation does not define an incomplete expectation, while KGWUI-011 explicitly permits `dropped`/`forbidden` in “every known context.”

   Example: the processed contexts drop C, but an unprocessed context attaches C. A bounded computation can therefore display a dropped outcome, raise KGWUI-011, or expect no entry and dispute legitimate status.

   **Controller evidence:** Every successful parent-match traversal can create the child reporter: `v2.4.7:pkg/kgateway/translator/httproute/delegation.go:49–95`; equivalent `v2.3.11:…/delegation.go:51–103`.

   **Why:** Bounds must reduce certainty, not change the inferred controller result. Define completeness per report-key aggregate. An observed attached context can establish expected entry presence; absence or “dropped everywhere” requires complete traversal. Gate negative KGWUI-011/012 conclusions accordingly.

3. **N3 — P2: View B/C dependency sets omit the facts that made a route a root.**

   **Location:** R §4.6:132–134; §5.1:145–146; §5.4:206; D §7.4:522.

   Root eligibility depends on view A, but a context’s dependencies are defined only as routes on its path. They do not inherit the Gateway/ListenerSet and Namespace dependencies that decided attachment.

   An authorized root and child behind a denied Namespace selector can therefore expose whether a context exists, plus its delegation outcome, even though the caller cannot view a deciding input.

   **Controller evidence:** Namespace selection gates attachment at `v2.4.7:pkg/kgateway/query/route.go:464–478` and `v2.3.11:…/route.go:424–438`.

   **Why:** The general authorization rule is correct, but its algorithmic dependency sets are incomplete. Carry root-eligibility dependencies into every descendant result; define dependencies for the status-root fallback too.

4. **N4 — P2: Persistence fingerprints still reset on evidence unrelated to the check.**

   **Location:** R §8.1:303; §9.2:369–378.

   `child-entry` and `parent-resolvedrefs` share a fingerprint definition that includes both `Accepted` and `ResolvedRefs`. Yet the cross-check explicitly treats a child’s backend `ResolvedRefs` failure as unrelated to attachment.

   A persistent child-entry contradiction can restart its 30-second window whenever backend resolution toggles, even though entry presence and hostname acceptance remain unchanged.

   **Controller evidence:** Backend failures write `ResolvedRefs` after delegation processing at `v2.4.7:pkg/kgateway/translator/httproute/gateway_http_route_translator.go:190–215`.

   **Why:** This defeats the accepted promise that unrelated writes cannot suppress a real discrepancy. Define signal fingerprints separately for each check kind, including only evidence used by that comparison.

5. **N5 — P2: Effective-matcher computation has conflicting execution and budget contracts.**

   **Location:** D §3.5:186; §7.3:486–511; §12:790; R §5.5:217–224; §5.4/§6:198–206,274.

   D says per-path effective matchers are computed per detail request and are not stored. R computes them during traversal, uses them to produce descendant outcomes and cross-check expectations, and caps work **per install per recompute**.

   The design does not specify whether requests replay the bounded traversal, reconstruct only retained contexts, or run a separate walk. Those choices can produce different truncation and different facts from the ones checked in the published model.

   **Controller evidence:** Matcher filtering directly controls recursive translation: `v2.4.7:pkg/kgateway/translator/httproute/delegation_helpers.go:91–102` and `delegation.go:93–95`; corresponding 2.3 lines are `92–103` and `100–103`.

   **Why:** The stated “one home” split leaves this contract contradictory. Specify in R which context/matcher results the immutable model retains and how detail responses project them, including one authoritative budget and truncation policy.

6. **N6 — P2: Delegation pagination lacks a unique row cursor.**

   **Location:** D §3.6:191–193; §7.1:392; §7.3:466–470.

   The universal cursor ends with `(sort value, namespace, name, group, kind)`. A delegation response can contain several rows for the same child under different rules or refs. Those rows share the prescribed cursor tuple.

   If a page ends between them, `next`, defined as strictly after the tuple, skips the remaining rows.

   **Evidence:** R §11.2:437 explicitly requires separate dropped and attached rows for the same child under two rules.

   **Why:** The 2.4 endpoint cannot reliably expose all outcomes. Define delegation ordering and cursors using a unique row identity, including rule and ref indexes, and test a page boundary between duplicate child identities.

7. **N7 — P3: The timestamp test requires an invariant initial publication violates.**

   **Location:** D §3.1:99,114; §9.3:688.

   Publication initializes `ConfirmedAt` to snapshot time, but the test requires `confirmedAt ≥ computedAt`. Existing computation records `ComputedAt` after taking the snapshot (`pkg/engine/engine.go:147–151`), so a newly published model normally has `confirmedAt < computedAt` until confirmation advances.

   **Why:** The 2.1 test either fails correct publication or encourages changing the preserved timestamp meaning. Test the timestamps’ individual meanings and their transition after an idle confirmation tick.

**Scope and independent shipping**

The accepted scope is preserved: A/B/C remain in Phase 2, while policies, graph layout and implementing an upstream state CRD remain deferred. The sub-phases are sensible **cumulative releases**; depending on an earlier shipped sub-phase does not undermine independent shipping.

- **2.1:** Has a complete user-visible outcome, but N1 blocks its freshness guarantee; N7 needs correction.
- **2.2:** No dependency on a future computed view is required by §13. Its status-only explorer is independently shippable.
- **2.3:** Has a coherent release boundary. N4 affects its shared persistence contract.
- **2.4:** Has a distinct release outcome, but N2–N6 need resolution before its diagnostics and API are reliable.

The document split improves ownership of the contracts. The remaining authoritative-home conflict is effective-context computation in N5; the other issues are gaps within the designated contracts.
| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| N1 | Confirmation can renew freshness during the store-update/callback gap | reviewer | accept | Bind the revision to the data actually snapshotted (engine-owned revision updated with the snapshot, or a content fingerprint); test with notification delivery paused. |
| N2 | Truncated contexts become false negatives | reviewer | accept | Completeness per report-key aggregate; an observed attached context can establish presence; "dropped everywhere" and absence need complete traversal; negative KGWUI-011/012 gated on completeness. |
| N3 | Dependency sets omit root eligibility | reviewer | accept | Root-eligibility dependencies (Gateway, ListenerSet, Namespace) carry into every descendant result, incl. the status-root fallback. |
| N4 | Fingerprints reset on unrelated evidence | reviewer | accept | One fingerprint definition per check kind, with only the evidence that comparison uses. |
| N5 | Effective-matcher execution and budget conflict | reviewer | accept | computed-rules.md is the one home: what the model retains, how details project it, one budget and truncation policy. |
| N6 | Delegation cursor not unique | reviewer | accept | Row identity includes rule and ref indexes; test a page boundary between rows of the same child. |
| N7 | Timestamp test contradicts initial publication | reviewer | accept | Test each timestamp's meaning and the transition after an idle confirmation tick. |

## Re-review 2 (scoped, Codex, diff f0ddf62..0c1e429)

Reviewed `f0ddf62..HEAD` across the three requested files, against the re-review and its triage. **N1–N7 are resolved as originally reported. Three new P2 findings remain.** No files edited.

D = [design.md](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md), R = [computed-rules.md](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/computed-rules.md). Lines refer to HEAD.

| Finding | Result | Section/line evidence |
|---|---|---|
| N1 | **Resolved** | D §3.1:91–106 replaces callback counters with fingerprints of the actual snapshot and live stores. §9.3:685–687 tests paused notification delivery and access changes. This addresses client-go v0.36.2’s store-before-callback ordering (`tools/cache/controller.go:639–649`, `shared_informer.go:1356–1360`). D-019:338–341 agrees. |
| N2 | **Resolved** | R §5.5:219–224 prevents incomplete negative row outcomes; §8.5:364–379 defines completeness and gates absence conclusions. D §6.2:359–362 gates KGWUI-011/012 accordingly. A separate reason-comparison problem appears below. |
| N3 | **Resolved** | R §5.6:229–232 propagates view-A root dependencies and the status-root target into descendant contexts, aggregates and findings. D §7.4:527 matches. |
| N4 | **Resolved** | R §9.2:399–409 separates fingerprints by check kind and explicitly excludes child `ResolvedRefs` from `child-entry` and parent `Accepted` from `parent-resolvedrefs`. |
| N5 | **Resolved** | R §5.7:234–251 makes recompute, retention and projection authoritative. D §3.5:191, §7.3:516 and §12:796 consistently make requests project retained results. |
| N6 | **Resolved** | D §3.6:196 includes rule/ref indexes in delegation cursors and defines their fixed ordering; §7.1:437 excludes delegation sorting. §9.1:655 tests boundaries between rows sharing a child. |
| N7 | **Resolved** | D §3.1:127 and §9.3:693 distinguish snapshot-time confirmation from compute time and test the subsequent idle confirmation transition. |

New findings, ranked:

1. **P2 — Incomplete parent aggregates can still demand an unjustified failure reason.**

   **Location:** R §8.5, [lines 366–376](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/computed-rules.md:366), interacting with §8.2:333.

   The new table permits an incomplete aggregate with a known parent failure to be “compared as usual.” However, §8.2 also compares the reason when all failures share one reason. Finding one failure proves `ResolvedRefs=False`; it does not prove that omitted contexts cannot introduce another reason.

   Example: the retained work finds an unresolved ref (`BackendNotFound`); omitted work encounters a cycle (`RefNotPermitted`). With no ordinary backendRefs or ExtensionRef filters, comparing against the retained reason can dispute correct controller status.

   **Tagged source evidence:** inspected through `git show` in the supplied kgateway checkout: `v2.4.7:pkg/kgateway/query/utils.go:38–50` assigns those distinct reasons; `pkg/kgateway/translator/httproute/gateway_http_route_translator.go:190–208` writes failures through the same reporter; `pkg/reports/reporter.go:505–512` updates the same condition type. Corresponding 2.3 behavior is at `v2.3.11:pkg/kgateway/query/utils.go:37–49` and `pkg/reports/reporter.go:447–454`.

   **Why:** A stable truncation can produce a false KGWUI-012. Incomplete positive evidence should establish only the false status; reason comparison needs complete failure coverage.

2. **P2 — Splitting the fingerprints removes a relevant annotation from the parent check.**

   **Location:** R §9.2, [line 406](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/computed-rules.md:406), and the exclusion at line 409.

   Before this diff, the shared fingerprint included the child’s inheritance annotation. The new `parent-resolvedrefs` definition omits it and explicitly excludes unlisted annotations. That annotation decides whether the selected child is dropped or attached, which can change the parent’s expected delegation failure.

   For example, changing inheritance can change the expectation from `False/BackendNotFound` to no delegation failure. An observed `False/RefNotPermitted` contradicts both expectations, so the candidate remains active; without the annotation in its fingerprint, the old persistence window carries over instead of restarting.

   **Tagged source evidence:** `v2.4.7:pkg/kgateway/translator/httproute/delegation_helpers.go:58–102` uses inheritance to decide whether rules survive; `pkg/kgateway/translator/httproute/delegation.go:98–99` reports unresolved when no valid children remain. Equivalent behavior appears at `v2.3.11:…/delegation_helpers.go:59–103` and `…/delegation.go:105–106`.

   **Why:** This contradicts §9.2’s semantic-input contract and can mature a warning prematurely after a relevant edit.

3. **P2 — Effective-context truncation now has no defined wire representation.**

   **Location:** R §5.7, [lines 243–247](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/computed-rules.md:243); D §7.3, [lines 494–497](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/design.md:494) and 516.

   The new retention policy truncates contexts per row and per install using `contextsTruncated`. But `EffectiveDTO` has no such field. Meanwhile, R now assigns `effectiveTruncated` specifically to exceeding 20 parent edges. D says effective details project the retained contexts and their truncation flags, without defining where that context flag goes.

   A route with one parent and 21 contexts therefore returns only 20 contexts with no defined indication that another exists. At the install retention cap, an effective edge can have no retained contexts at all.

   **Why:** The route detail cannot faithfully expose the new retention policy and can present partial matcher results as complete. Define context truncation on `EffectiveDTO`, including how multiple rows belonging to one parent edge combine.

No new P1, P3 or P4 findings.
| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| M1 | Incomplete parent aggregates compare a failure reason | reviewer | accept | Incomplete positive evidence establishes only `ResolvedRefs=False`; the reason is compared only with complete failure coverage. |
| M2 | Parent fingerprint omits the inheritance annotation | reviewer | accept | `parent-resolvedrefs` includes every child input that changes the parent's expected delegation failure, incl. the inheritance annotation. |
| M3 | No wire form for context truncation | reviewer | accept | `EffectiveDTO` gains `contextsTruncated`, defined per parent edge across rows, incl. the install-cap case. |

## Re-review 3 (scoped, Codex, diff 226e974..c62211a)

M1, M2 and M3 resolved; no new findings at P1–P4.
