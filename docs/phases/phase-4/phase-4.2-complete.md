# Phase 4.2 — User-testing follow-ups (Complete)

**Completed:** 2026-11-07
**Range:** 9d98451..HEAD (branch `duncandoyle/snellius`)

## Summary
Three changes from the user's first look at Snellius on minikube (D-063):
1. A tab returning from the background no longer shows "Data is stale" while its first poll is under way. The hold ends when that poll answers, or after 10 s. A failed poll or old data still shows stale.
2. Hovering or focusing a row's Error or Warning on the Gateways, Routes and ListenerSets lists and the overview shows why, e.g. "KGWUI-002: Listener http has an invalid certificate reference (+2 more)". Rows carry an optional `reason`; no request on hover. Tooltips render outside scroll containers.
3. An About page (ⓘ button beside the theme button) shows the product, its descriptor and tagline, the UI version, the watched installs and project links. The header brand still links home. `Brand` gains optional `descriptor`, `tagline` and `links`.

## Sub-phases (phase record only)
n/a

## Reviews
- `reviews/4.2-codex.md`: 5 findings (2 P1, 2 P3, 1 P4), all accepted and fixed in e847265.

## Waivers
None.

## Deliverables
- Gates: web typecheck, lint, vitest 803/803, build; `go vet`; `go test -race ./...` (35 packages); `make lint` (0 issues).
- `make bench` under `caffeinate -s`: every budget passes (`bench-4.2.txt`). Some web test runs overlapped the bench; it passed regardless.
- Screenshots of the About page, a row tooltip and the hidden-tab case in both themes (task report, outside the repo).
- The roadmap's Traffic Flow & Backends phase (Phase 6) gains a Backends list with the inverse route lookup (user, 2026-11-07).

## Notes for later phases
- `Brand.links` lets a downstream build point About at its own docs and licence; without links it shows "Built on Snellius".

## Deferred / open
None.
