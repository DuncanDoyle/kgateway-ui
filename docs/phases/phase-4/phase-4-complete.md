# Phase 4 — Snellius rebrand (Complete)

**Completed:** 2026-11-07 (local; closes for good once the PR's CI runs kind e2e and the image build)
**Range:** 966be06..HEAD (branch `duncandoyle/snellius`)

## Summary
The UI is now Snellius. The header shows the Snellius symbol and the `snellius` wordmark, and the browser title and favicon follow. The README carries the logo, the tagline "See the whole picture." and an upgrade path from kgateway-ui. Every artifact has the new name: the Go module `github.com/DuncanDoyle/snellius`, the binary, the image `ghcr.io/duncandoyle/snellius`, the chart `snellius` and the npm package `@snellius/shell`. kgateway keeps its name wherever the UI shows the gateway it observes; install pages now show a `kgateway` edition tag. Finding codes keep `KGWUI-`. Saved themes and `KGWUI_MEMORY_LIMIT` carry over.

## Sub-phases (phase record only)
| Sub-phase | Kind | Issues | Description |
|---|---|---|---|
| 4.1 | planned | n/a | Plan Tasks 1–6: module, chart and CI, web packages, visual identity, docs, verification |

## Reviews
- `reviews/design-codex.md`: 6 findings, 6 accepted.
- Task review of Tasks 1–3 (Claude, same model family as the author; a task gate, not the cross-model review): pass, 2 minor notes.
- `reviews/phase-codex.md`: 3 findings plus 1 from the author, all accepted and fixed.

## Waivers
None. The user pre-approved the phase while away (2026-11-07); their final look is still open.

## Deliverables
- Gates on the final code: `go vet` (default, `e2e`, `bench`), `go test -race ./...`, `TZ=Europe/Amsterdam go test ./pkg/api`, `make lint`, `make actionlint`, web `npm ci`, typecheck, lint, vitest (772 tests), build, `make web`, `helm lint`, `REQUIRE_HELM=1 go test ./deploy/...`, and the terms check. All pass.
- Visual checks (Task 4): screenshots at 1920 × 1080 in dark and light of the landing page, header and install header; title `snellius` in both themes; wordmark contrast 13.41:1 (dark) and 17.50:1 (light); the symbol is recognisable at 24 px and the favicon at 16 px; shape and colours match `reference/approved-dark-whatsapp-logo.png`, whose beams are brighter and wider. The API was stubbed; no cluster was used.
- Not run locally: `docker build` and kind e2e (Docker unavailable). CI runs both on the PR.

## Notes for later phases
- The repo rename on GitHub must happen before this phase merges, so the new module path and links resolve.
- Old tags (v0.4.0 and before) stay under the old module path, image and chart.
- `.webmanifest` is served without a dedicated MIME type; browsers accept it.

## Deferred / open
- kgwctl must switch its imports to `github.com/DuncanDoyle/snellius` (cross-repo notice, with the user's go).
- The SVGs are vector approximations of the approved raster reference; a designer pass could brighten the beams.
