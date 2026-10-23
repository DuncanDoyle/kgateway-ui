# Sub-phase 2.1 — Event-driven recompute

Kind: planned
Base: 4f2fc3098d1869b79e524a6afeaa5e83ca3cb696
Issues: n/a | Plan steps: implementation-plan.md Tasks 1–10; decisions D-019, D-021

## Problem / goal
Phase 1 recomputes the model on a fixed 2 s ticker (`pkg/engine/engine.go:209-222`). Idle clusters pay a full recompute every tick, a change waits up to one interval, and `computedAt` cannot say whether an unchanged model is still current. Sub-phase 2.1 makes the Phase 1 pages event-driven with honest freshness (D-019), and first records the cluster facts the computed views depend on (D-021).

## Expected behaviour

| Input / action | Expected |
|---|---|
| Rules §10 items 1–10 run on kgateway v2.3.11 and v2.4.7 | Each answer recorded in D-021; rules §10 links it (Task 1) |
| `test/e2e/setup.sh` on either pair | Experimental Gateway API CRDs installed (TCPRoute, TLSRoute, ListenerSet); Phase 1 e2e passes (Task 1) |
| Phase 1 model at Small, Medium, Large | Baseline recompute cost measured and stored (Task 2) |
| One `kubectl apply` or Helm upgrade | One recompute after the debounce (250 ms after the last trigger, at most 1 s after the first) (Tasks 3–6) |
| Idle cluster | No recompute; the confirm tick renews `confirmedAt` once a second when the source fingerprint is unchanged (Tasks 3, 6) |
| A change whose notification has not arrived | The confirm tick sees a different fingerprint and schedules a recompute; it never confirms old data (Task 6) |
| A finding maturing at time T | A recompute at T, with no further event (Task 6) |
| `installs.json`, `bare-installs.json` | Gain optional `confirmedAt`; other fields unchanged (Task 7) |
| SPA freshness | Cards, switcher and header read `confirmedAt`, falling back to `computedAt` (Task 8) |
| `--poll-interval` | Maximum time between recomputes (safety timer), default 30 s (Task 9) |

## Root cause (if known)
n/a (planned work). The fixed ticker is `pkg/engine/engine.go:209-222`; its interval is `cmd/kgateway-ui/main.go:23`.

## Acceptance tests
- Task 1: kind observations on both tags (fixtures `test/e2e/fixtures/observe/`), recorded verbatim in D-021.
- Tasks 3–6: deterministic engine tests with a fake clock (design §9.3): debounce, single flight, confirmation against the fingerprint, deadlines, paused handler delivery.
- Task 7: API boundary tests plus the additive-only golden check against `pkg/api/testdata/golden-v1/`.
- Task 8: Vitest for each freshness consumer.
- Task 9: `pkg/app` wiring tests; budget tests at Large (`//go:build bench`).

## Scope
In: `pkg/trigger`, `pkg/cache`, `pkg/ctrlmetrics`, `pkg/kinds/monitor.go`, `pkg/engine`, `pkg/api` (timestamps), `pkg/app`, `cmd/kgateway-ui`, `web/packages/shell` (freshness), `internal/testutil`, `test/e2e/setup.sh`, `test/e2e/fixtures/observe/`, README and chart notes, `docs/`.
Out: computed views (2.3, 2.4), explorer pages and new kinds (2.2), push to the SPA, incremental index updates.

## Plan / design impact
D-021 (Task 1) may change rule sets or the `verified` marks of `computed-rules.md` §8.3 before 2.3 and 2.4. Otherwise none.

## Exit criteria
From the plan's Sub-phases table: D-021 observations recorded; Phase 1 baseline measured; §9.3 engine tests pass with a fake clock; `confirmedAt` served and read by the SPA; existing goldens differ only by `confirmedAt`; burst, idle and tick budgets met at Large; Codex review triaged.

## Files
- Task 1: `docs/phase-status.md`, this brief, `test/e2e/setup.sh`, `test/e2e/fixtures/observe/*`, `docs/decisions.md`, `docs/phases/phase-2/computed-rules.md`
- Task 2: `internal/testutil/scale.go`, `internal/testutil/fixtures.go`, `pkg/engine/bench_test.go`, `Makefile`, `docs/phases/phase-2/bench/2.1-baseline.txt`
- Tasks 3–4: `pkg/trigger/`, `pkg/cache/cache.go`, `pkg/cache/fingerprint.go`, `pkg/ctrlmetrics/scrape.go`, `pkg/kinds/monitor.go`
- Tasks 5–6: `pkg/engine/sched.go`, `pkg/engine/loop.go`, `pkg/engine/engine.go`, `internal/testutil/clock.go`
- Task 7: `pkg/api/types.go`, `pkg/api/handlers.go`, `pkg/api/testdata/golden-v1/`, `pkg/api/additive_test.go`
- Task 8: `web/packages/shell/src/api/freshness.ts` and its consumers
- Task 9: `pkg/app/app.go`, `cmd/kgateway-ui/main.go`, `pkg/engine/budget_test.go`, `README.md`, `deploy/helm/kgateway-ui/README.md`
- Task 10: `docs/phases/phase-2/phase-2.1-complete.md`, `docs/phases/phase-2/reviews/2.1-codex.md`, `docs/phase-status.md`, `docs/roadmap.md`

## Definition of done
- [ ] every task's tests fail before and pass after (reported per task)
- [ ] full test suite passes (Go, chart, web, kind e2e on v2.3.11 and v2.4.7)
- [ ] 2.1 budgets met at Large
- [ ] cross-model review of `git diff <base>..HEAD` + triage
- [ ] phase-2.1-complete.md written; phase-status.md and roadmap updated
