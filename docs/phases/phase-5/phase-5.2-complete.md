# Phase 5.2 — Metrics and checks into the pack (Complete)

**Completed:** 2026-11-08
**Range:** 98807aa..99884f0 (branch `duncandoyle/genericCore`)

## Summary
Controller metrics now belong to the kgateway pack. Checks read them only through the pack collector (`RunContext.Collected`); `engine.Config.Metrics`, `health.Input.Metrics`, `ctrlmetrics.Static` and `ctrlmetrics.Source` are gone (user, 2026-11-08). One-shot callers use `ctrlmetrics.CollectOnce` and `Publish`. Checks are split per install: an install runs only its own pack's checks, and condition types come from the pack. Behaviour for kgateway installs is unchanged: the API goldens and the downstream example are untouched. The README has the Go API upgrade note.

## Sub-phases (phase record only)
n/a

## Reviews
- `reviews/5.2-codex.md`: 4 findings (1 P1, 2 P2, 1 P3), all accepted and fixed (33c79f1, 9538de0, 28871db). The P1: a model whose metrics had gone stale could be confirmed as current for one tick.

## Waivers
None.

## Deliverables
- Gates: `go vet` (default, `e2e`, `bench`), `go test -race ./...` (38 packages), `TZ=Europe/Amsterdam go test ./pkg/api`, `make lint`, `make actionlint`, `helm lint`, `REQUIRE_HELM=1 go test ./deploy/...`.
- `make bench` after the fixes: every budget passes; retained heap +23.3 MiB over the 2.1 baseline at Large and +39.3 MiB worst (budget 48); warm recompute 33 ms at Large, 48.9 ms worst (budget 150). One earlier run measured +45.3 MiB worst; reruns gave +39.2 to +40.0.
- Task 6a (program file and kgwctl notice) is drafted, not posted: `scratchpad/p52-6a/`. kgwctl `main` still imports `kgateway-ui v0.3.0`; the notice covers the module rename and E8 together, and its snippet builds against this branch.

## Notes for later phases
- Deviation: `Impl.Version` keeps its signature; `Impl.Reported(install, snapshot)` reads the reported version, and the engine keeps the last one when a scrape fails (design §4.2).
- Task 7 pulled in condition types per install and "no rule set for a pack without one" from Tasks 9–11.
- Class-only installs with the flag on pick up kgateway policies and show KGWUI-003 on them (carry-over to the policy phases).

## Deferred / open
- Post Task 6a when Phase 5 merges to main.
