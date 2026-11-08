# Phase 5.1 — Packs, class-only installs, default scope (Complete)

**Completed:** 2026-11-08
**Range:** 9059cfe..0d95797 (branch `duncandoyle/genericCore`)

## Summary
The extension registry now holds packs: per-install implementations selected by the detector that claimed the install. kgateway is the default pack; old `With…` options set it in any order. `NewCore()` builds a registry with no pack. An install no pack claims becomes a class-only install, one per GatewayClass `controllerName`, with an escaped ID and an assumed all-namespaces scope; the default binary shows them only behind a flag (off). Settings, scope and experimental kinds resolve per install. A `snellius-core` binary and a core-only image target exist. Behaviour for kgateway installs is unchanged: the API v1 goldens and the downstream example are untouched.

## Sub-phases (phase record only)
n/a

## Reviews
- `reviews/5.1-codex.md`: 4 findings (3 P2, 1 P3), all accepted and fixed (3b7f8af, 0e733f7, ef3cb61, 4ccedee).

## Waivers
None.

## Deliverables
- Gates: build, `go vet` (default, `e2e`, `bench`), `go test -race ./...`, `TZ=Europe/Amsterdam go test ./pkg/api`, `make lint`, `make actionlint`, `helm lint`, `REQUIRE_HELM=1 go test ./deploy/...`, `make build-core`.
- `make bench` (`bench-5.1.txt`): every budget passes; retained heap +23.4 MiB over the 2.1 baseline (budget 48), warm recompute 33.0 ms (budget 150).
- A test-harness flake (`TestPhase2GoldensUnchangedOnNewContract`, present before 5.1) fixed at its root cause in 0d95797: the harness now waits for its optional watches.

## Notes for later phases
- Class-only installs still run the kgateway checks and rule sets until 5.2 (check split) and 5.3 (no computed view without a rule set); their health reads unknown until then.
- `internal/cli` holds the flags both binaries share (not in the plan).
- kgwctl uses only the preserved `ProxyPodDetector` interface; the detector wrapping does not break it (checked by Codex at kgwctl 980d32d).

## Deferred / open
- The core-only image's `docker build` was not run locally (Docker unavailable); CI builds it.
