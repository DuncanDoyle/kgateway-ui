# Phase 6.4 — Route trace, all route kinds (Complete)

**Completed:** 2026-11-11 (local branch `duncandoyle/phase6`, not yet pushed)
**Range:** e0d16a8..120db13

## Summary
Route detail now serves every route kind (HTTPRoute, GRPCRoute, TCPRoute, TLSRoute) and its Rules section is a trace (design §7). It shows where the route is reached through, then per rule the match (gRPC methods, "Any method", no match line for TCP/TLS), the filters as written, a share bar of the rule's requests or connections, and each backend with port, weight, share, endpoints and problem, linking to backend detail. Failing and may-fail parts carry a pattern, not colour alone.

## Sub-phases (phase record only)
n/a

## Reviews
Per-task Claude reviews and scoped re-reviews (same model family; the cross-model review is at phase close). Notable findings fixed:
- Task 40: delegated path rows lacked root-eligibility deps (pre-existing since Task 25), so `/paths`, `/backends` and backend detail could reveal a hidden Namespace's labels; fixed in `pkg/paths` for every hop. Route detail now filters its rows in one pass.
- Task 41: "Any method" for a GRPCRoute rule without matches; patterned failing segments; breadcrumbs for every route kind; the sibling-routes lookup kept for restricted backends; connection wording for TCP/TLS.

## Waivers
None.

## Deliverables
- Commits `3651b89` … `120db13` (Tasks 40–42).
- Bench (`bench-6.4.txt`): D-057 settled +46.5 MiB at large-contradicting-worst, +31.0 at large-paths (budget 48); paths, route-detail and backend-detail request gates pass.
- e2e: Go e2e and Playwright (66 passed) on v2.4.7/v1.6.1 (TCPRoute v1), v2.3.11/v1.5.1 (v1alpha2), v2.5.0/v1.7.0 (v1), local binary; TestForcedDisagreementRaisesKGWUI012 needs the in-cluster image.
- Wording pass: `phase-6.4-wording.md` (separate Claude subagent, not cross-model).
- Demo reel (not in the repo): scratchpad `reels/6.4/summary-reel.mp4`; the HR route with a failing backend was applied for the reel only.

## Notes for later phases
- The route header's state is the existing `RowState` (unknown while an install-wide required check is not OK), which can differ from the trace's per-row states. Known divergence, kept.
- A ref whose problem is a KGWUI-023 warning draws a plain share segment; share states stay limited to 013/014 (design §4.4). Question for the user, with the "503" finding title.
- D-057 headroom 1.5 MiB; the map (6.5, 6.6) is client-side.

## Deferred / open
- A web test (`delegation.test.tsx`) failed once in a full run; root-cause investigation running before phase close.
- Minors in the SDD ledger.
