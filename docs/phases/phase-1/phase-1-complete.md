# Phase 1 — Foundation & Overview (Complete)

**Completed:** 2026-10-21
**Range:** 560c393..6cd6111

## Summary
A read-only kgateway UI that installs with Helm or runs locally from one binary. An operator opens it and sees, per controller install:
- the version, edition, controller readiness and namespace scope;
- a health roll-up with findings that say what is wrong and link to the resource's YAML;
- counts of GatewayClasses, Gateways, routes and policies, and the list of Gateways with their status.

Health covers status conditions with freshness, resources not yet processed, unattached routes, controller and proxy readiness (including self-managed and zero-replica proxies), xDS rejects, ListenerSets, and scope verification against controller metrics. With several installs, a cluster landing page lists them.

It also delivers the foundations: the Go backend with the embedded React SPA, the extension seam (Go options and a shell extension API), themes and design tokens, a platform-neutral client contract (D-016), a multi-arch image, a least-privilege Helm chart, CI and release workflows, and a kind e2e on kgateway 2.3 and 2.4.

## Sub-phases (phase record only)
| Sub-phase | Kind | Issues | Description |
|---|---|---|---|
| 1.1 | planned | — | Backend: kinds, cache, install discovery and scope, assignment, metrics, health, engine, HTTP API, extension registry (#2) |
| 1.2 | planned | — | Frontend: shell, themes, install page, cluster landing, findings, YAML view, SPA embedding (#3) |
| 1.3 | planned | — | Platform-neutral contract: opaque install IDs, optional fields, capabilities, vocabulary, self-managed proxies, import boundary (D-016) (#4) |
| 1.4 | planned | — | Packaging, CI, e2e: image, Helm chart, workflows, kind e2e, README (#5) |
| 1.5 | stabilization | — | Fixes for the phase review and its own review (Q9–Q22, D-017, D-018) (#6) |

## Reviews
- `reviews/design-codex.md` … `design-r5-codex.md` — five design and plan rounds; all findings triaged, one rejected and one deferred in round 1, the rest accepted.
- `reviews/neutrality-codex.md` — D-016 design change: 8 findings, all accepted (incl. the re-review's P2).
- `reviews/1.3-plan-codex.md` — plan amendment for 1.3: 8 findings, all accepted.
- `reviews/phase-codex.md` — phase review: 9 findings (5 P1, 3 P2, 1 P3), all accepted → sub-phase 1.5.
- `reviews/1.5-codex.md` — 1.5 review: 5 findings accepted; re-review remainders: 1 rejected, 1 deferred (both decided by the user).
- Every sub-phase also had per-task spec and quality reviews and a whole-branch review (same model family); see the sub-phase records.

## Waivers
none

## Deliverables
- Backend: `cmd/kgateway-ui`, `pkg/{api,app,cache,ctrlmetrics,engine,extension,health,install,kinds,objects,overview,platform,proxymode,scopeverify}`, `internal/{boundary,webui}`.
- Frontend: `web/packages/shell` (extension API, `SHELL_API_VERSION` 1) and `web/app`.
- Packaging: `Dockerfile`, `deploy/helm/kgateway-ui`, `.github/workflows/{ci,release,e2e}.yaml`, `test/e2e`, `web/e2e`, `examples/downstream`.
- Contract tests: golden v1 JSON in `pkg/api/testdata/golden`, checked by Go and TypeScript.
- Decisions D-001…D-018. Licence Apache-2.0.
- Gates observed on 6cd6111: `go vet` (also with tag `e2e`), `go test ./... -race`, chart tests, golangci-lint 0 issues, Vitest 281/281, typecheck, Biome and build. CI on PR #6 (same tree): go, web, helm, image, hygiene and kind e2e on v2.3.11, v2.4.7 and v2.4.7 with a second install, all pass.

## Notes for later phases
- The first release (a `vX.Y.Z` tag) freezes API v1, install IDs and SPA URLs (D-016 H11). Before it, make the ghcr image and chart packages public and check an anonymous pull (README, Releasing).
- CI's forbidden-terms check reads the repository secret `FORBIDDEN_TERMS`; fork and Dependabot PRs get no secrets.
- File the upstream scope proposal (reminder in phase-status.md); its enabled-kinds field lets scope verification and the experimental hint stop guessing (D-017 §4, D-018).
- `docs/phases/phase-1/implementation-plan.md` is the historical plan; where it differs from the code, the decisions and the sub-phase records win.

## Deferred / open
- #1 Watch CRDs added or removed after startup without a restart — P3, later phase.
- The carry-over list in `docs/phase-status.md`: extension seams (finding-code registry, analyzers, API routes, renderers, health cards), per-user AuthN/AuthZ, `Backend` kind for Phase 2, the design §14.4 refactors, the optional `kind-main` job, downstream base-version reporting, and one possible test flake.
- Deferred minor findings are listed in each `phase-1.<M>-complete.md`.
