Cross-model: YES (author: Claude Opus 5.5, reviewer: Codex, command: codex exec --sandbox read-only)
Scope: design
Diff: n/a (design review of docs/phases/phase-2/design.md at 99b072b)
Timing: on time

I found **1 P1, 9 P2 and 3 P3 findings**. No files were edited. References to kgateway below are from `git show` at the named tag.

1. **P1 — Declared parentRefs are presented as observed delegation.**  
   **Design:** §4.3, lines 191–199; §5.2, lines 244–254; §7.3, line 404.  
   An explicit child parentRef creates an edge even if the parent never selects that child. That edge then qualifies the child for the “picked up” tree and a named delegation ref’s `reported` state. In kgateway, the child parentRef only permits attachment: selection happens first through the parent’s backendRef, followed by attachment and matcher filtering. Evidence: `v2.4.7:pkg/kgateway/query/route.go:258–268`, `pkg/kgateway/utils/delegation/delegation.go:14–30`, and `pkg/kgateway/translator/httproute/delegation_helpers.go:84–102`; equivalent selection/attachment ordering exists at `v2.3.11:pkg/kgateway/query/route.go:231–241`.  
   **Why:** A parent that delegates nowhere can acquire “children” solely through their declarations. Keep declared relationships as diagnostic candidates, but require controller status for “reported” relationships. Computing selection to bridge that gap would violate D-005/D-006.

2. **P2 — The observed-edge fallback resurrects removed explicit parents.**  
   **Design:** §4.3, lines 193–198.  
   “No spec parentRef to P” is broader than an implicit child with **no parentRefs**. If a child changes its parent from P to Q while old P status remains, this rule reclassifies P as an observed parent. kgateway allows implicit attachment only when the entire parentRefs list is empty: `v2.4.7:pkg/kgateway/utils/delegation/delegation.go:25–30`; `v2.3.11`, same file, lines 25–45. Its status builder likewise falls back to report refs only for an empty list: `v2.4.7:pkg/reports/status.go:489–496`. Phase 1 deliberately ignores removed refs: [checks.go:314](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/health/checks.go:314).  
   **Why:** Old relationships can retain ownership, breadcrumbs and health findings in the former install. The fallback needs an explicit eligibility rule and transition tests.

3. **P2 — New detail admission rules break promised navigation.**  
   **Design:** §4.3, line 204; §5.2, line 251; §7.2, line 365; §8.3, line 475.  
   Details return 404 for unowned objects, but finding links are retargeted to details by kind. Unwatched routes and missing-parent routes are intentionally unowned; the existing resource endpoint serves them through finding references: [handlers.go:255](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/api/handlers.go:255), especially lines 266–285. “No parent” routes have neither ownership nor findings, so their new list rows also cannot open either detail or YAML under the stated rules. ListenerSets with missing Gateways are never assigned: [assign.go:136](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/install/assign.go:136).  
   **Why:** Several advertised debugging flows end in 404. Define access for diagnostic candidates and no-parent inventory, or provide a deliberate fallback.

4. **P2 — Filtered `hidden` counts expose denied object data.**  
   **Design:** §7.3, lines 378–379; §7.4, line 416; §11, line 618.  
   `hidden` counts denied rows **after filters**, including hostname substring and condition-state filters. A caller can query candidate hostname strings and observe whether the denied count changes. Phase 1 excludes denied objects from inventory counts; D-013 permits restricted finding identities, not arbitrary hostname or status probing: [handlers.go:130](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/api/handlers.go:130), `docs/decisions.md:210–214`.  
   **Why:** “Never named” does not prevent disclosure through filtered counts. Omit denied-match counts or explicitly approve a broader disclosure contract.

5. **P2 — Authorization does not cover all dereferenced data, and ancestor placeholders lack a wire shape.**  
   **Design:** §7.3, lines 399–405; §7.4, lines 415–418.  
   An allowed route can include a denied Backend’s full `accepted: Condition`, including its message. Filtering route rows does not authorize that Backend lookup. D-013’s existing behavior removes denied finding messages and evidence: [handlers.go:115](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/api/handlers.go:115). Meanwhile, `ancestors.path: Ref[]` cannot express “A parent you cannot view” without sending its identity or inventing a fake ref.  
   **Why:** Specify authorization for fetched Backend status and derived existence/state fields, plus a typed restricted ancestor representation. References already present in an authorized object’s spec are a separate case.

6. **P2 — Row health conflates finding severity with health state.**  
   **Design:** §3.3, line 114; §7.2, line 357; §7.3, line 383.  
   The index stores “worst finding severity,” while DTOs promise `health: State` and filters accept `info` but omit `unknown`. Existing states are `error`, `unknown`, `warning`, `healthy`; `info` is a finding severity: [finding.go:20](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/health/finding.go:20). Required unavailable checks take precedence over warnings: [rollup.go:22](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/health/rollup.go:22).  
   **Why:** A severity-only map cannot preserve pending/unavailable health semantics. Define row state computation and expose `unknown` consistently.

7. **P2 — Gateway health propagation is promised but not designed.**  
   **Design:** §7.4, line 418; §6.2, line 326.  
   The claim that a hidden child can turn Gateway health red “as in Phase 1” is incorrect. Phase 1’s Gateway row considers findings whose resource is that Gateway, plus required-check availability; it does not aggregate route findings: [overview.go:164](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/overview/overview.go:164). The proposed map remains keyed by each finding’s object ref.  
   **Why:** Q12 makes delegated failures affect install health, but does not establish their effect on ancestor Gateway badges. Specify propagation, including multiple parents, or narrow the claim.

8. **P2 — Backend freshness defaults can produce green without current evidence.**  
   **Design:** §6.2, lines 320–322; §15 Q11, line 685.  
   The tagged generation distinction is correct: `v2.4.7:pkg/reports/status.go:410–415` stamps generation; `v2.3.11:pkg/kgateway/extensions2/pluginutils/status.go:10–37` does not. However, the proposed fallback silently accepts generation-less `True`, and applies the fallback whenever the controller version is unknown—even if the condition supplies usable generation evidence.  
   **Why:** D-016 §6 and Phase 1 §5.2 require current evidence before green. Q11 asks only how to display `False`; it omits the consequential `True`/absent behavior. Prefer available condition evidence before version inference and explicitly decide the no-evidence state.

9. **P2 — Ancestor indexing has no bound compatible with the linear-cost claim.**  
   **Design:** §3.3, line 117; §5.2, line 260; §12, line 630.  
   The immutable index stores up-chains, while the UI promises five paths and a count of the remainder. Repeated diamonds can have exponentially many paths despite linearly many objects and edges. They are valid kgateway inputs: `v2.4.7:pkg/kgateway/query/route.go:240–246`; `v2.3.11:pkg/kgateway/query/route.go:213–219`. Current computation rebuilds the model wholesale: [engine.go:143](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/engine/engine.go:143).  
   **Why:** Limiting rendered paths does not bound construction or exact remainder counting. Define bounded traversal/storage and truncation semantics; benchmark shared descendants, not just trees.

10. **P2 — Two detail payloads bypass the stated pagination architecture.**  
    **Design:** §7.3, lines 394–396; §8.4, line 480; §2.2, line 64.  
    `GatewayDetail.listenerSets[]` returns every nested ListenerSet despite the 2,000-object target and a separate paginated endpoint. Conversely, the paginated failed-route rows lack the promised controller **message**: `ParentSummary` carries only `reason`, and `RouteRow` carries no conditions.  
    **Why:** One table remains unbounded; another requires extra detail requests to render its specified content. Page nested ListenerSets and include the necessary parent-specific failure evidence in route rows.

11. **P3 — Compatibility tests do not validate Q12’s actual behavioral change.**  
    **Design:** §4.3, line 189; §9.1, lines 531–538; §15 Q12.  
    “No child route is owned” is too broad: a route with both Gateway and HTTPRoute parentRefs is already owned through its Gateway ref, though its delegated status is ignored: [assign.go:158](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/install/assign.go:158). Unchanged Phase 1 goldens will not test changed counts and health on delegation fixtures.  
    **Why:** Add before/after cases for mixed-role routes, deduplication across parents, shared-controller installs, removed refs, scope changes and stale observed edges. Assert counts, findings and roll-up together.

12. **P3 — Version fixtures need to test status production, not merely normalized identity.**  
    **Design:** §5.1; §5.5, line 282; §9.1, line 533; §9.5, lines 574–578.  
    On 2.3, synthesized refs omit child-specified section/port as well as replacing an omitted namespace: `v2.3.11:pkg/kgateway/query/route.go:252–259`. On 2.4, the matching ref is retained as written: `v2.4.7:pkg/kgateway/query/route.go:294–310`. Also, hostname rejection occurs **after matcher filtering**, so a hostname-bearing child whose rules were all filtered receives no child report: `v2.4.7:pkg/kgateway/translator/httproute/delegation.go:49–83`; 2.3 lines 51–90.  
    **Why:** Identity-normalization tests cannot establish that status exists. Cover omitted namespace, section/port, empty parentRefs and filtered-out hostname-bearing children at both tags.

13. **P3 — Performance and cursor acceptance coverage cannot substantiate the stated guarantees.**  
    **Design:** §9.1, line 528; §9.6, lines 583–594.  
    Cursor tests cover insertion before an anchor, but omit anchor deletion, descending ties, reverse traversal and sort-value movement across recomputes. Performance gates specify p95 without a latency-sampling method; ordinary Go benchmark results report averages. The relative compute-overhead target also supplies no absolute budget against the two-second recompute interval.  
    **Why:** Add those cursor cases, explicit concurrency/latency measurement, and an absolute sustained-compute/allocation budget under polling. Otherwise a passing benchmark can coexist with permanently stale UI data.

For §15, **Q12 needs the declared-versus-observed and health decisions above before a default is safe; Q10 needs a serving contract; Q11 needs the `True`/absent freshness decision; Q6 needs graph-shape and compute budgets**. Q2/Q3/Q8’s alternatives are changes to the accepted no-mirroring boundary, so selecting them requires a decision amendment, not just a UX preference. Keeping policies, topology and precedence out is consistent with the roadmap; the Backend registration is an explicit Phase 1 carry-over.
## Triage
| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | Declared parentRefs presented as observed delegation | reviewer | accept | "Reported" requires controller status; declared refs are diagnostic candidates only. |
| 2 | Observed-edge fallback resurrects removed parents | reviewer | accept | Fallback only when the child's parentRefs list is empty, matching kgateway; transition tests. |
| 3 | Detail admission rules break navigation | reviewer | accept | Our call: details serve owned objects plus diagnostic candidates (referenced by a current finding or an owned object's parentRef/backendRef); no-parent rows open YAML. |
| 4 | Filtered `hidden` counts leak denied data | reviewer | accept | Our call: drop filtered hidden counts; only the unfiltered install-wide denied count, as Phase 1 does. |
| 5 | Dereferenced Backend status and ancestor placeholders | reviewer | accept | Authorize every dereferenced object; a typed restricted ancestor with no identity. |
| 6 | Row health conflates severity and state | reviewer | accept | Row state uses the Phase 1 roll-up rules; `unknown` everywhere, no `info` state. |
| 7 | Gateway health propagation not designed | reviewer | accept | Our call: narrow the claim; Gateway badges keep Phase 1 semantics; delegated failures count in install health and on the route rows. |
| 8 | Backend freshness can go green without evidence | reviewer | accept | Prefer the condition's own generation; no evidence → unknown, never green (D-016 §6). Q11 restated. |
| 9 | Ancestor paths unbounded on diamonds | reviewer | accept | Bounded traversal with truncation semantics; benchmark shared descendants. |
| 10 | Two detail payloads bypass pagination | reviewer | accept | Page nested ListenerSets; route rows carry the parent-specific reason and message. |
| 11 | Q12 compatibility tests too weak | reviewer | accept | Before/after cases for mixed-role routes, dedup, shared controllers, removed refs, scope changes, stale edges. |
| 12 | Version fixtures must test status production | reviewer | accept | Cluster observation on both tags for omitted namespace, section/port, empty parentRefs, filtered hostname-bearing children. |
| 13 | Cursor and performance acceptance too weak | reviewer | accept | More cursor cases, a latency-sampling method, an absolute compute and allocation budget. |
