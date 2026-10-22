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
