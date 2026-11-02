# Phase 3.0 — Extension seams (Complete)

**Completed:** 2026-11-02
**Range:** 3eceda6..HEAD (branch `duncandoyle/phase3Seams`)

## Summary
A downstream distribution can now add a whole feature, with its own data, API and UI, without forking `app.Run`, the shell package or the chart (design §4.1, D-038). The stock UI does not change.
- **Finding-code prefixes:** `WithChecks(prefix, …)` registers checks under a prefix such as `ACME-`. `KGWUI-`, a duplicate or a malformed prefix makes `app.Build` fail.
- **Base version:** `Install.BaseVersion` lets a downstream detector pick the kgateway rule set.
- **Endpoints:** `WithHandlers(name, …)` serves under `/api/ext/{name}/`; patterns outside it fail the build. `api.Visible` applies the Authorizer; `WriteJSON` and `WriteError` match the API's shapes.
- **Capabilities:** extension capabilities report on, off, denied or failing in `/meta` (`capabilityStates`); with none, `meta.json` is unchanged.
- **Collectors:** background collectors publish into `pkg/collect`. A new fingerprint triggers a recompute. Expired, failed or stopped values read as unavailable, never as current, and the confirm tick never stamps a stale value.
- **Chart values:** `rbac.extraRules`, `extraEnv`, `extraVolumes`, `extraVolumeMounts`, `extraContainers`. The default render is byte-identical.
- **Shell package:** `make shell-pack` builds a tarball with JS and type declarations only; the dev source condition is stripped.
- **In-page slots:** detail tabs, side sections, finding actions and install panels, each behind an error boundary that resets on navigation.
- **Proof:** `examples/downstream` uses every seam, and its Go tests, chart overlay and web package run in CI.

## Sub-phases (phase record only)
n/a

## Reviews
- **Task reviews:** Tasks 2–10 each had a spec and quality review, with fix rounds confirmed by scoped re-reviews. A cleanup batch handled selected deferred minors.
- **Cross-model:** `reviews/3.0-codex.md`: 4 findings (2 P2, 2 P3), all accepted and fixed in `b129432`. `reviews/3.0-gate-codex.md`: the dense gate change, 2 findings; the P2 was fixed, and the P3 was partly fixed, with the rest tracked.

## Wording pass
n/a: 3.0 adds no core UI text (ruling R11).

## Waivers
None.

## Deliverables
- Acceptance tests failed before and passed after, per the task reports (Tasks 2–10, RED/GREEN sections in the SDD workspace).
- Standard gates: vet, `go test -race ./...`, lint, web type-check and tests, Helm, the downstream example: all pass.
- kind e2e, the three `e2e.yaml` rows, run locally on 2026-11-02: all pass. Each row: Go e2e `ok`, Playwright 19 passed. `TestForcedDisagreementRaisesKGWUI012` runs only on the `--poll-interval=10m` row, as in CI.

### Benchmarks (Large tier)
Record: `bench/3.0.txt`.

| Measure | 3.0 | Budget |
|---|---|---|
| Warm recompute | 8–17 % faster than the 2.4 record | ≤ 150 ms |
| Stream CPU, annotation-only | 6.4–7.6 % | ≤ 15 % |
| Dense memory, retained heap (median of 5) | 160 MiB (2.4: 160) | ≤ 176 MiB |
| Dense memory, allocated by the cold recompute | 210 MiB (2.4: 210) | ≤ 231 MiB |
| Dense memory, allocated per warm recompute | 168 MiB (2.4: 168) | ≤ 185 MiB |
| Dense memory, peak heap (logged) | median 421–440 MiB, same on the 2.4 code | not gated (D-039) |

The dense gate changed during the close (user, 2026-11-01; D-039). The old peak-heap gate failed on the 2.4 code as well; the new measures are identical in every run.

## Release note
Downstream builds can add endpoints, background collectors, capabilities, in-page UI slots and chart extensions without forking. `extension.WithChecks` now takes a finding-code prefix first (README, Upgrade notes).

## Rulings made during execution
1. **R1, R2:** `Deps.Capabilities` arrives in Task 5. `Deps.Publish` takes a TTL, and `Deps.Fail` reports failure.
2. **R3:** an unset `BaseVersion` keeps the 2.4 rules with the "version unknown" note.
3. **R4, R6, R8, R13:** small edits outside planned file lists; `app.Options.Clock` is a test option.
4. **R5:** a fourth value state, `unavailable`.
5. **R7:** the shell's dev condition is `@kgateway-ui/source`, stripped from the tarball.
6. **R9, R10:** finding actions hidden on restricted findings; the `delegation` tab id is reserved.
7. **R11:** no wording pass.
8. **R12:** the example's web package installs without a lock file (npm 10.9.2 crash).

## Notes for later phases
- 3.1 (Task 16) moves the example's `Input.Collected` read to the D-032 `RunContext` inputs.
- The dense gate (D-039) is deterministic, so 3.1–3.4 can compare against 160/210/168 MiB exactly.

## Deferred / open
- (P3) Dense peak live memory is logged, not gated (`reviews/3.0-gate-codex.md#2`, in `phase-status.md`).
- Minors in the SDD ledger: `api.Visible` memo is per call; one test uses a sleeping `eventually`; the README tsx block is not drift-tested; CI packs the shell twice.
