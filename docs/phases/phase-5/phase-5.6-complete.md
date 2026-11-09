# Phase 5.6 — The compatibility check and close (Complete)

**Completed:** 2026-11-09 (local; the core image build and the first compatibility run need the branch pushed)
**Range:** bc65504..971323c (branch `duncandoyle/genericCore`)

## Summary
A `compat`-tagged smoke test (`test/compat`) and a Playwright spec check what a core-only build must do against any Gateway API controller: the default build claims nothing, the core build shows a status-only install with present and empty `packs` and `features`, and no process surfaces. Public CI only vets that test and builds the core-only image; behavioural runs happen in a private compatibility CI outside this repo (D-065). An informational CI job builds kgwctl `main` against this tree and never blocks a merge. The README documents packs, `NewCore`, class-only installs behind the chart value, and the v0.6.0 upgrade notes for Go and frontend callers.

## Sub-phases (phase record only)
n/a

## Reviews
- Covered by the phase review, `reviews/phase-codex.md`: 5 findings (1 P1, 2 P2, 2 P3), all accepted and fixed (f658d5e, bb35bc3, 0bed633, 1f7723e).

## Waivers
None.

## Deliverables
- Gates: build, `go vet` (default, `e2e`, `bench`, `compat`), `go test -race ./...` (40 packages), `TZ=Europe/Amsterdam go test ./pkg/api`, `make lint`, `make actionlint`, `helm lint`, `REQUIRE_HELM=1 go test ./deploy/...`, web and downstream example web gates.
- `make bench` on 971323c (`bench-5.6.txt`): every budget passes. Retained heap +23.4 MiB over the 2.1 baseline at Large, +40.0 MiB worst (budget 48); warm 33.5 / 44.5 / 49.1 ms (budget 150); cold 47 / 52 / 66 ms.

## Notes for later phases
- The first private compatibility run has not happened; expect setup fixes there.

## Deferred / open
- `make image-core` not built locally (Docker unavailable); CI builds it once pushed.
