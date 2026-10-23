# Phase 2.1 — Event-driven recompute (Complete)

**Completed:** 2026-10-23
**Range:** 4f2fc30..HEAD (branch `duncandoyle/eventRecompute`, PR #7)

## Summary
The UI recomputes health when something changes instead of on a fixed 2 s ticker (D-019):
- **Triggers:** informer events, access changes, metrics results with changed content, and served-kind changes start a debounced recompute (250 ms quiet, at most 1 s); one recompute runs at a time and a change during a run causes one more.
- **Time-driven state** has its own deadlines: grace expiry for objects still in grace, metrics staleness, and scope-mismatch maturation. A 30 s safety timer (`--poll-interval`) covers a missed event.
- **Publication and confirmation:** the scheduler publishes the model and its confirmation atomically. Every second an idle model is confirmed against a fingerprint of the data it was computed from (objects, access, metrics content and freshness, served kinds), so it stays fresh, and an outdated one is never stamped current.
- **API:** install summaries keep `computedAt` and gain an optional `confirmedAt`; the SPA's staleness checks use `confirmedAt` when present.
- **Cluster observations** of kgateway 2.3 and 2.4 delegation and attachment status are recorded as D-021 and feed the computed rules for 2.3 and 2.4.
- **A startup bug fixed on the way:** the metrics collector could start before Pods were listed and show a false "no ready controller pod" error for one interval.

## Reviews
- **Pre-flight scan:** 27 findings (PF1–PF27), all ruled before Task 1.
- **Task reviews:** every task had a spec and quality review (Tasks 4 and 8 reviewed by the controller as small diffs); Tasks 1, 3, 5, 6, 7 and 9 needed fix rounds, each confirmed by a scoped re-review.
- **Cross-model (Codex) review of the sub-phase:** `reviews/2.1-codex.md` — 1 P1 (a stale model kept confirmed after metrics recovered with identical content), 1 P2 (budgets not measured on the CI runner class), 2 P3 (no test blocking a real informer handler; wall-clock polling in two helpers). All accepted and fixed; the scoped Codex re-review found them resolved and no new findings.

## Waivers
none

## Deliverables
- `pkg/trigger`; `pkg/cache` fingerprint, change notifications and `Settled`; `pkg/engine` scheduler (`sched.go`), loop and publication (`loop.go`); `pkg/ctrlmetrics` notifications and content identity; `pkg/kinds` served-kind notifications; `pkg/app` wiring; `pkg/api` `confirmedAt`; the SPA freshness helper.
- `internal/testutil/scale` (scale tiers), budget tests and benchmarks (`make bench`), `.github/workflows/bench.yaml`.
- `test/e2e/setup.sh` with the experimental Gateway API bundle and the opt-in XListenerSet CRD; `test/e2e/fixtures/observe`.
- D-021; design §3.1, §4.2 and §9 updated to the 2.1 rulings.
- Gates (HEAD before the final docs): `go vet` (incl. `e2e` and `bench` tags), `go test ./... -race` over 20 packages, `TZ=Europe/Amsterdam go test ./pkg/api`, chart tests, golangci-lint 0 issues; Vitest 285/285, typecheck, Biome and build; no forbidden terms in tree or commit messages. kind e2e passed locally on v2.4.7 with the second install and on v2.3.11 (API test and Playwright 3/3), and in PR #7's CI matrix.

### Benchmarks (warm recompute per tier)

| Tier | Phase 1 baseline (laptop) | 2.1 (laptop) | 2.1 (CI runner, 4-CPU Xeon) |
|---|---|---|---|
| Typical | ~8.1 ms, 4.4 MiB | — | ~19.5 ms, 5.7 MB |
| Large | ~36.6 ms, 20.7 MiB | ~36.5 ms | ~91.8 ms, 26.7 MB |
| Extreme | ~131 ms, 73.6 MiB | — | ~335 ms, 95.7 MB |

### Budgets at the Large tier (CI runner, `bench/2.1-ci.txt`)

| Budget | Measured | Limit |
|---|---|---|
| Event to publication, p95 | 1.067–1.073 s | ≤ 1.2 s |
| Recomputes in a 1 s burst of 1,000 updates | 1 | ≤ 2 |
| Idle 10 min | 20 recomputes (safety timer), 600 confirmations, 0 refused | as designed |
| Confirmation tick, median | 0.56–0.61 ms | ≤ 5 ms |
| Warm recompute | ~92 ms | ≤ 400 ms |

## Notes for later phases
- **Memory headroom for 2.2:** on the CI runner a warm Large recompute already allocates 26.7 MB of the 32 MiB (33.5 MB) per-recompute budget that applies from 2.2, leaving about 7 MB for the explorer indexes and computed views. 2.2 must measure this early.
- **Child-edit drops** of delegated children are unobserved; leftover status after such a drop stays `not-verified` (D-021, design §4.2).
- **Every working parent with a delegating match that keeps no child** shows `ResolvedRefs=False / BackendNotFound` (D-021 item 9); 2.2's wording pass handles it.
- **XListenerSet** exists only up to Gateway API v1.4.1; its CRD is opt-in for observations (`OBSERVE_XLISTENERSET=1`).

## Deferred / open
- A cold-compute baseline (only warm recomputes are measured).
- A test pinning that the scope-mismatch deadline clears once the mismatch resolves (correct by reading).
- The sub-phase name in the plan's Sub-phases table reads "Engine and timestamps"; this record uses "Event-driven recompute".

## Rulings made during execution
Recorded in full in the sub-phase ledger; the load-bearing ones:
- PF1 — XListenerSet observations use the CRD from Gateway API v1.4.1, opt-in.
- PF4 — scale generator in its own package `internal/testutil/scale` (import cycle).
- PF14 — the confirmation tick is handled before the run timer when both are ready.
- Task 1 — clearing after a drop is verified only for parent-edit drops; a child-edit leftover is `not-verified`; the cause is decided from first-seen times, unknown counts as a child edit.
- Task 6 — grace deadlines only for objects still in grace; publication from the scheduler's done case.
- Task 9 — metrics identity is presence, error and content, not `CollectedAt`; metrics staleness is a strict deadline; an unchanged scrape does not recompute; scope-mismatch maturation gets its own deadline.
- Codex #1 — the metrics identity also carries freshness at the comparing instant, so a stale-to-fresh recovery refuses confirmation and recomputes.
- Codex #2 — a `bench` workflow on GitHub runners records the budgets (also on PRs touching engine paths).

## Deferred minor findings
- Task 3 — an error-text change while a kind keeps failing gives no notify (the confirmation tick covers it); the access-transition test takes several seconds on reflector backoff.
- Task 6 — a confirmation can land one loop iteration before a due deadline (PF14 order); the concurrent-read test is guarded only by the race detector.
- Task 9 — the app test's 1 s real-time quiet wait (accepted under PF21).
