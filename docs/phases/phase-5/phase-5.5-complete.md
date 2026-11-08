# Phase 5.5 — Web (Complete)

**Completed:** 2026-11-08
**Range:** b4db2ca..627feca (branch `duncandoyle/genericCore`)

## Summary
API v1 sends each install's `features` and `/meta` lists the server's `packs` (both always sent, additive). The SPA stays one bundle: kgateway copy, the delegation tab and the Envoy and process surfaces are contributions shown per install from those server-sent values, never from edition strings. The default build is unchanged for kgateway users, including "No kgateway install found"; every kgateway page is 0 px different from the base in screenshots. The core-only binary shows neutral copy. A class-only install shows status only: no delegation tab, no Processes block or nav entry, no Data plane column, and no kgateway kind tiles. Rule-set labels read "kgateway 2.4".

## Sub-phases (phase record only)
n/a

## Reviews
- `reviews/5.5-codex.md`: 4 findings (3 P2, 1 P4), all accepted and fixed (d8a68ed..f703dff); the coordinator's leftover (Processes nav entry) fixed in 627feca.

## Waivers
None.

## Deliverables
- Gates: web `npm ci`, typecheck, lint, vitest, build, `make web`; Go build, `go vet` (default, `e2e`, `bench`), `go test -race ./...`, `TZ=Europe/Amsterdam go test ./pkg/api`, `make lint`, `make actionlint`, `helm lint`, `REQUIRE_HELM=1 go test ./deploy/...`.
- `make bench` on Task 15 (labels touch recompute): every budget passes, numbers as 5.4. The review fixes changed no engine code.
- Goldens: only additive fields (`features`, `label`, `ruleSetLabel`); `golden-v1` untouched.
- Screenshots (both themes, 1920 × 1080, stubbed API): kgateway and class-only pages, outside the repo.

## Notes for later phases
- An absent `features` or `packs` list means an older server; the frontend keeps them optional.

## Deferred / open
None.
