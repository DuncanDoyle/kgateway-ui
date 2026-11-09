# Sub-phase 5.6 — The compatibility check and close

Kind: planned
Base: bc65504
Issues: n/a | Plan steps: implementation-plan.md, Sub-phase 5.6 (G6), Tasks 16–18; design §12, §13; D-065

## Problem / goal
5.1–5.5 built the core-only binary and class-only installs, but nothing proves the seam holds against a real controller. D-065 puts that proof in a private CI outside this repo. This repo must ship what that CI runs, with no implementation names: a generic smoke test and its Playwright half. Public CI only keeps them compiling and builds the core-only image. An informational job reports whether kgwctl `main` still compiles against this tree. README gains packs, `NewCore`, class-only installs and the v0.6.0 upgrade notes.

## Expected behaviour

| Input / action | Expected |
|---|---|
| `go test -tags compat ./test/compat/...` with `COMPAT_*` set, core build healthy | passes within 3 min: one install with ID `ClassOnlyID(controller)`, scope all/assumed, verdict not error; Gateway and route listed with status, route accepted by the Gateway; no `gateway.kgateway.dev` kind; no `delegation`, `computed`, `dataplane` feature; no kgateway-pack finding code; the default build does not claim the controller |
| Same, any one of those broken | fails, naming the broken assertion |
| Same, a `COMPAT_*` variable missing or not `namespace/name` | fails at once, naming the variable |
| `compat.spec.ts` with `COMPAT_UI_URL` set | install page lists the Gateway; route page has a YAML tab, no Delegation tab; no `role=alert` on either |
| kgateway e2e Playwright run (no `COMPAT_UI_URL`) | does not load `compat.spec.ts` |
| PR CI | `go vet -tags compat ./test/compat/...`, `make image-core`; a `kgwctl compile (informational)` job with `continue-on-error: true` |

## Root cause (if known)
n/a (planned work).

## Acceptance tests
- `test/compat/check_test.go` (untagged, runs in `go test ./...`): `Check` passes against `app.Build` over a made-up controller with `NewCore()`, the default build from `New()`; each §12.1 assertion fails when its response is mutated through a proxy; the default build with class-only installs on is caught; `EnvFromOS` rejects a bad input.
- `compat.spec.ts`: run locally against the same fixture served with the real SPA (throwaway server, not committed); a missing route fails it.
- `make actionlint` (with shellcheck) on the CI changes; the kgwctl job's script run by hand against kgwctl `980d32d`.

## Scope
In: `test/compat/`, `web/e2e/compat.spec.ts`, `web/e2e/playwright.config.ts`, `.github/workflows/ci.yaml`, `README.md`, `docs/documentation-notes.md`.
Out: any behavioural compat run in this repo; naming a controller other than kgateway; the private repo's content (written there); posting the program file or kgwctl issues (at merge); the phase review and complete records.

## Plan / design impact
Deviations, recorded here and in the complete file:
- The assertions live in an untagged `test/compat/compat.go` (`Check`, `Settle`, `EnvFromOS`), so in-repo tests exercise them on every run; the `compat`-tagged `compat_test.go` only reads env and polls.
- `playwright.config.ts` ignores `compat.spec.ts` unless `COMPAT_UI_URL` is set, so the kgateway e2e never loads it.
- The kgwctl job renames kgwctl's old module path (`kgateway-ui` → `snellius`) when present, so it checks the API rather than the v0.5.0 rename kgwctl has not made yet. It also runs `go vet`.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite and gates pass
- [ ] phase cross-model review (at phase close, not in this sub-phase run)
- [ ] phase-5.6-complete.md written; phase-status.md updated
