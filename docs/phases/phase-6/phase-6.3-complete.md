# Phase 6.3 — Backends (Complete)

**Completed:** 2026-11-11 (local branch `duncandoyle/phase6`, not yet pushed)
**Range:** 8727eac..e0d16a8

## Summary
Operators can now see every backend and how it is exposed (design §8–§11). The Backends list shows in-scope Backends, every backend a route writes (attached or not) and GatewayExtension consumers, problems first, with per-port endpoints and facet chips. Backend detail names the impact in a plain status line, lists the exposure paths and non-route consumers, shows endpoints per port with addresses from an on-demand, byte-budgeted EndpointSlice read, the port chain, and why references fail with the suggested ReferenceGrant. Backend judgement moved behind the kgateway pack.

## Sub-phases (phase record only)
n/a

## Reviews
Per-task Claude reviews and scoped re-reviews (same model family; the cross-model review is at phase close). Notable findings fixed:
- Task 35: the reader memo's 1 MiB was a density measurement, now a byte budget; a race made tests flaky.
- Task 36: the paths memo kept the previous engine model alive after every publication (from Tasks 28/29, ≈12–38 MB); it now holds sequence numbers. D-057 is gated on the settled heap after a publication.
- Task 37: explanation text and reference paths now come from one source with the health checks.
- Task 38: a missing referenced port no longer reads "Not read"; chips come from server facets.

## Waivers
None.

## Deliverables
- Commits `a9b351a` … `e0d16a8` (Tasks 33–39).
- Bench (`bench-6.3.txt`): D-057 settled +46.5 MiB at large-contradicting-worst (warm +47.3 reported), +30.9 at large-paths; backend detail ≤ 1.83 MiB median per request; paths warm ≤ 1.744 MiB.
- e2e: Go e2e and Playwright (60 passed) on v2.4.7/v1.6.1, v2.3.11/v1.5.1, v2.5.0/v1.7.0 against the local binary; TestForcedDisagreementRaisesKGWUI012 needs the in-cluster image.
- Wording pass: `phase-6.3-wording.md` (picked by a separate Claude subagent, not cross-model).
- Demo reel (not in the repo): scratchpad `reels/6.3/summary-reel.mp4`.

## Notes for later phases
- D-057 headroom at the worst world is 1.5 MiB. 6.4–6.6 should add no retained state; the map is client-side.
- KGWUI-023's finding title names a status code ("… get 503"); it shows in Why/Problem columns. A finding-copy question for the user.

## Deferred / open
- Minors in the SDD ledger.
- Full in-cluster e2e (image build) and push: CI, after the user approves the push.
