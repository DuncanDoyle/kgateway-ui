# Phase 6.2 — Path model, paths API, path list (Complete)

**Completed:** 2026-11-11 (local branch `duncandoyle/phase6`, not yet pushed)
**Range:** 6bf3a46..8727eac

## Summary
One path model now feeds every Phase 6 view (design §4). `pkg/paths` builds one row per (attachment, rule, backendRef) from view A, the Delegator's view C and decoded rules, and projects each row's problem from existing signals along the whole chain. `GET /api/v1/installs/{id}/paths` serves it with server-side filters, sort, cursor paging, a summary with facets, and per-row authorisation by dependency set (D-077). The new Traffic page lists every path problems first, with shares, endpoints, plain-word reasons and one-click Gateway and namespace chips.

## Sub-phases (phase record only)
n/a

## Reviews
Per-task Claude reviews and scoped re-reviews (same model family; the cross-model review is at phase close). Notable findings fixed:
- Task 22: rule decoding made opt-in (hot-path cost).
- Task 24: delegation rows revealed the child route without authorising it.
- Task 27: zero-ready severity now comes from KGWUI-023 (warning), never from counts; missing Service is "waiting".
- Task 28: topology memo keyed on snapshot content plus a views generation, not per publication; a real data race fixed.
- Task 29: view-less installs say so (`notComputed`).
- Task 30: D-057 measured in-child; the bimodal heap figure traced to GC timing during the world build and fixed in the method.
- Task 31: stale view, facets, finding titles; warm allocation regression (2.13 → 0.49 MiB); per-second remount that dropped keyboard focus.

## Waivers
None.

## Deliverables
- Commits `192fd03` … `8727eac` (Tasks 22–32), with `c5e28d9` gating finding titles by visibility.
- Bench (`bench-6.2.txt`): D-057 in-child warm retained +45.3 MiB at large-contradicting-worst, +29.8 at large-paths (budget 48). Path requests warm ≤ 1.74 MiB and < 2 ms; cold ≤ 32 ms and ≤ 29 MiB.
- e2e: Go e2e and Playwright pass on v2.4.7/v1.6.1, v2.3.11/v1.5.1, v2.5.0/v1.7.0 against the local binary; TestForcedDisagreementRaisesKGWUI012 needs the in-cluster image (Docker DNS blocks the image build locally).
- Wording pass: `phase-6.2-wording.md`, 73 strings, picked by a separate Claude subagent (not cross-model).
- Demo reel (not in the repo): scratchpad `reels/6.2/summary-reel.mp4`.

## Notes for later phases
- Worst-world D-057 headroom is 2.7 MiB, measured with GC off during the world build (reproducible, low-fragmentation mode). 6.3's scoped-reader memo must fit in it.
- `action: "delegate"` is a fifth path action; design §4.1 lists four — update at phase close.
- Class-only installs get no paths (`notComputed`); paths from route status for them is a user question.

## Deferred / open
- Minors in the SDD ledger (test gaps, naming, a known delegated-row over-report pinned by a test).
- Full in-cluster e2e (image build) and push: CI, after the user approves the push.
