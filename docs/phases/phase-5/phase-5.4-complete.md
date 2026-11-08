# Phase 5.4 — The rest of the kgateway pack (Complete)

**Completed:** 2026-11-08
**Range:** 3a96594..d81a85b (branch `duncandoyle/genericCore`)

## Summary
The route list reads each install's condition types. The scope verifier, the process source, the proxy mode and the next-step commands are per install: a class-only install runs no verifier and has empty process sets, and a pack without its own hooks gets none (only the kgateway pack gets the interval-sized default verifier). A boundary test lists the 14 core files that still import a kgateway-pack package, each with a reason; a stale entry fails it. All exported additions are additive (`api.WithInstallCommands`, new `engine.Hooks` fields, `explore.Input.Conditions`).

The analyzer allocation budget concern from 5.3 was a measurement error introduced in 5.2: the bench harness built the incoming scrape inside the measured run. The harness now builds it beforehand, as the 5.1 harness did; a guard keeps the metric checks re-running. Worst case 968,624 of 1,027,000 B (94.3 %). No budget or production code changed.

## Sub-phases (phase record only)
n/a

## Reviews
- `reviews/5.4-codex.md`: 2 findings (1 P2, 1 P3), both accepted and fixed (a04feb0). Codex confirmed the budget change is a measurement correction, not a loosened gate.

## Waivers
None.

## Deliverables
- Gates: build, `go vet` (default, `e2e`, `bench`), `go test -race ./...` (39 packages), `TZ=Europe/Amsterdam go test ./pkg/api`, `make lint`, `make actionlint`, `helm lint`, `REQUIRE_HELM=1 go test ./deploy/...`.
- `make bench` (`bench-5.4.txt`): every budget passes. Retained heap +23.3 MiB over the 2.1 baseline at Large, +39.8 MiB worst (budget 48); warm 33.4 / 50 ms; cold 48 / 65 ms.

## Notes for later phases
- A downstream using `WithPack` must supply the hooks it needs; legacy `With…` options keep default-pack ownership.

## Deferred / open
None.
