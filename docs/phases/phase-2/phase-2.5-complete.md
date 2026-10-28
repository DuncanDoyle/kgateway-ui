# Phase 2.5 — Stabilization of the Phase 2 review findings (Complete)

**Completed:** 2026-10-28
**Range:** 29b8792..0d07532 (branch `duncandoyle/phase2Stabilize`)
**Kind:** stabilization

## Summary
Seven defects found by the Phase 2 cross-model review are fixed; each restores behaviour the design already states, with no wire format change and no new dependencies:
- **Delegation tab (Q1):** when the tab's own poll fails or goes stale, every row state, computed badge and ref summary badge reads Unknown, and returns to the real states on the next good poll. A ref collapsed after a failed poll keeps that Unknown state.
- **Orphan KGWUI-011 (Q2):** the orphan conclusion is install-local; another install's status entry on the same route no longer suppresses it.
- **`listener-unknown` (Q3):** a visible parent keeps the flag when another parent of the route is hidden.
- **Edge cap (Q4):** with an unknown discovery scope, every ref reads `scope-unknown`, including refs the edge cap cut.
- **`since` (Q5):** KGWUI-012 on any computed subject (declared, no-parent or cross-install child) keeps its own first-seen time under restricted evidence.
- **Plan (Q6):** Task 44 of the implementation plan follows the amended D-023.
- **Golden guard (Q7):** `types.golden.test.ts` fails on a scalar type, enum value or array-versus-object mismatch and names the JSON path.

## Sub-phases (phase record only)
n/a

## Reviews
- `reviews/phase-codex.md` — 8 findings: 7 accepted (fixed here, Q1–Q7), 0 rejected, 1 deferred (finding 8, stable finding identity, to Phase 3 planning).
- `reviews/2.5-codex.md` — 4 findings: 4 accepted, 0 rejected, 0 deferred. Two rounds:
  1. Round 1 (29b8792..97d02d0): confirmed Q2, Q3, Q4 and Q6. Found three gaps: the Q1 summary badge ([`6670b0a`](https://github.com/DuncanDoyle/kgateway-ui/commit/6670b0a)), the Q5 cross-install child ([`6b04ccd`](https://github.com/DuncanDoyle/kgateway-ui/commit/6b04ccd)) and inherited enum names in Q7 ([`8d771b1`](https://github.com/DuncanDoyle/kgateway-ui/commit/8d771b1), [`7c63cb8`](https://github.com/DuncanDoyle/kgateway-ui/commit/7c63cb8)).
  2. Re-review (97d02d0..7c63cb8): Q5 and Q7 resolved. One P2 on Q1 (collapsing cleared the stale state), fixed in [`0d07532`](https://github.com/DuncanDoyle/kgateway-ui/commit/0d07532) and verified by the controller, with no further Codex round.
- Concern accepted as intended: a failed ref stays Unknown while collapsed until it is reopened and gets a good poll.
- Decision D-026 recorded the three inferred outputs (Q1, Q4, Q7).

## Waivers
none

## Deliverables
| Item | Commits | Files |
|---|---|---|
| Q1 | [`45baa25`](https://github.com/DuncanDoyle/kgateway-ui/commit/45baa25), [`6670b0a`](https://github.com/DuncanDoyle/kgateway-ui/commit/6670b0a), [`0d07532`](https://github.com/DuncanDoyle/kgateway-ui/commit/0d07532) | `web/packages/shell/src/pages/explore/route/DelegationTab.tsx`, `web/packages/shell/src/pages/explore/delegation.test.tsx` |
| Q2 | [`7f6ee7b`](https://github.com/DuncanDoyle/kgateway-ui/commit/7f6ee7b) | `pkg/computed/check.go`, `pkg/api/computed_test.go` |
| Q3 | [`12be26a`](https://github.com/DuncanDoyle/kgateway-ui/commit/12be26a) | `pkg/explore/rows.go`, `pkg/explore/query.go`, `pkg/api/computed_test.go` |
| Q4 | [`197101b`](https://github.com/DuncanDoyle/kgateway-ui/commit/197101b) | `pkg/api/details.go`, `pkg/api/delegation_test.go` |
| Q5 | [`dbff6b2`](https://github.com/DuncanDoyle/kgateway-ui/commit/dbff6b2), [`6b04ccd`](https://github.com/DuncanDoyle/kgateway-ui/commit/6b04ccd) | `pkg/health/computed.go`, `pkg/api/computed_test.go` |
| Q6 | [`a5ea505`](https://github.com/DuncanDoyle/kgateway-ui/commit/a5ea505) | `docs/phases/phase-2/implementation-plan.md` (the Q6 grep matched 2 lines on the base, none after) |
| Q7 | [`97d02d0`](https://github.com/DuncanDoyle/kgateway-ui/commit/97d02d0), [`8d771b1`](https://github.com/DuncanDoyle/kgateway-ui/commit/8d771b1), [`7c63cb8`](https://github.com/DuncanDoyle/kgateway-ui/commit/7c63cb8) | `web/packages/shell/src/api/types.golden.test.ts` |

Each of Q1–Q5 and Q7 has a regression test at the boundary the brief names. No dependency was added; no golden changed.

Gates on HEAD ([`0d07532`](https://github.com/DuncanDoyle/kgateway-ui/commit/0d07532)):
- **Go:** `go vet` clean (plain, `bench`, `e2e`); `go test -race` passes in 24 packages; `make lint` reports 0.
- **Web:** 536/536 tests; typecheck, lint and build pass.
- **Kind e2e:** not re-run locally; this PR's CI runs it.

## Notes for later phases
- A failed ref stays Unknown while collapsed until it is reopened and gets a good poll (accepted as intended).
- First-seen for a computed finding comes from the finding's own subject, not from the ownership buckets (`Declared`, `NoParent`); a new kind of computed subject needs no extra bucket.
- The golden guard uses own-property checks for keys and enum values; keep that when extending it.

## Deferred / open
- Finding 8 of the phase review (P4, stable finding identity for Phase 3 diagnostics) — Phase 3 planning.
