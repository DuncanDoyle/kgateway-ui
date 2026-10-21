# Phase 1.4 — Packaging, CI, e2e (Complete)

**Completed:** 2026-10-20
**Range:** 3309349..3e5139d (branch `duncandoyle/packaging`; base cab3fae)

## Summary
kgateway UI can now be built, installed, tested on a real cluster and released:
- **Image:** multi-arch (amd64, arm64) distroless image with the SPA embedded, UID 65532, OCI labels linking it to the repo.
- **Helm chart:** read-only ClusterRole (get, list, watch; no Secrets), hardened pod, optional HTTPRoute, `install.pin`. Chart tests assert that every kind the backend reads, including `gatewayparameters`, is granted.
- **CI:** Go vet/test/lint, web gates, chart tests, actionlint, a multi-arch image build without push, and a forbidden-terms check over tree, file names and commit messages that reads its list from a secret and fails closed except on fork PRs.
- **Release:** semver tags publish the image and the OCI chart to ghcr and create a GitHub release, each job with least privilege.
- **e2e:** kind matrix on kgateway v2.3.11 and v2.4.7, plus a second install on 2.4.7. The Go API test covers opaque install IDs, `readiness`, `/meta` capabilities, a resource by ID and KGWUI-010 on a self-managed Gateway. Playwright covers the overview, a deep link, reload and both themes. `make e2e` / `make e2e-down` run it locally.
- **Docs:** README with the trust model, install, extending and compatibility; roadmap notes for D-016 (H10); upstream proposal drafted for the user (H9), not committed.

## Reviews
- **Pre-flight scan:** 23 findings and 17 clean checks, all ruled on before Task 19.
- **Task reviews:** spec and quality reviews for Tasks 19–23. No Critical or Important findings, except Task 21's hygiene job (2 Important, fixed in one round with an amended ruling). Tasks 20, 21 and 22 each had one fix round for minors.
- **Whole-branch review:** most capable model, same model family (not cross-model). 0 Critical, 3 Important (term list printed in public logs via `vars`; ghcr packages private on first push; local chart install pulls a missing image), 9 Minor. Fixed in one wave and confirmed by a scoped re-review.
- **Cross-model code review:** happens at the phase review (planned sub-phase).

## Waivers
none

## Deliverables
- `Dockerfile`, `.dockerignore`.
- `deploy/helm/kgateway-ui` and `deploy/helm/chart_test.go`.
- `.github/workflows/ci.yaml`, `release.yaml`, `e2e.yaml`; `.golangci.yml` linters.
- `test/e2e` (setup script, fixtures, API test with tag `e2e`), `web/e2e` (Playwright).
- Makefile targets `e2e`, `e2e-down`, `actionlint`.
- `README.md`, `docs/roadmap.md` notes, design wording fixes (`parentRefs`, "N of M namespaces").
- Gates observed on 3e5139d: `go build`, `go vet` (also with tag `e2e`), `go test ./... -race`, `REQUIRE_HELM=1` chart tests, golangci-lint 0 issues, actionlint, `helm lint`; Vitest 267/267, typecheck, Biome and build clean; no forbidden terms in tree or commit messages. kind e2e on v2.4.7 with the second install passed after the last e2e change (API test, Playwright 3/3); v2.3.11 and v2.4.7 single-install passed earlier in the task.

## Notes for later phases
- **Before the first CI run:** create the repository secret `FORBIDDEN_TERMS` and delete the variable of the same name; until then every same-repo run fails the hygiene job by design.
- **First release:** make the ghcr image and chart packages public, then check an anonymous pull (README, Releasing).
- **Upstream proposal:** the draft is with the user. Two claims come from D-009 research and should be re-checked before filing: stale status after namespace deselection, and tools reading the selector env from the Pod spec.
- The workflows have only been checked locally (actionlint, local runs). The first PR run is their real test.

## Deferred / open
- Design-review finding 16: extension seams for analyzers, API routes, per-kind renderers and health cards (seam milestone after Phase 2).
- The optional `kind-main` e2e job runs on manual dispatch with a `kgw_main_version` input; its chart source is unverified.
- Per-user AuthN/AuthZ behind `api.Authorizer` (D-013).
- Register `Backend` with its `Accepted` condition and an assignment rule in the Phase 2 design (D-016, design §14.5).
- The refactors of design §14.4, triggered by the standalone PoC; the neutral `Install` and `Detector` land before the extension-seam milestone.

## Rulings made during execution
- PF1 source maps — Ruling: the image and the CI artifact drop `*.map` — the embed test rejects them — none.
- PF2 lint config — Ruling: edit the existing `.golangci.yml` — a second file is ignored — none.
- PF3 gosec findings — Ruling: exclude gosec on tests; `nolint` with a reason on the intentional Background shutdown context — none.
- PF4–PF6 actions — Ruling: golangci v2.15.0 pinned; current action majors; tokens via `env` only — none.
- PF7 hygiene — Ruling: also scan commit messages; first warn-only when the list is empty, then amended after Task 21's review to fail closed except on fork PRs — warn-only would let a term land on main — a run fails if the secret is removed.
- PF8 — Ruling: cite the public-repo note, not D-005 — none.
- PF9 — Ruling: PRs build the multi-arch image without pushing — none.
- PF10 — Ruling: chart test asserts every read kind is granted, derived from the kind registry — none.
- PF11 — Ruling: keep the `referencegrants` and CRD read grants (design §8) — slight read-only over-grant; raise at the phase review.
- PF12–PF15 — Ruling: `install.pin` accepts ID or namespace/name; commit go.mod; chart tests skip without helm unless `REQUIRE_HELM=1`; add `make e2e` — none.
- PF16 — Ruling: e2e adds `/meta`, `readiness`, a resource by ID, KGWUI-010 via a self-managed fixture, deep link and reload; second install on 2.4 only (brief corrected) — none.
- PF17–PF20 — Ruling: assert the theme changes; `kind-main` optional; idempotent setup with bounded polling; ignore Playwright output — none.
- PF21 — Ruling: the upstream draft is not committed (public repo) and goes to the user — none.
- PF22, PF23 — Ruling: no duplicate carry-over; multi-arch check uses a docker-container builder — none.
- Task 22 — Ruling: the controller reran the 2.4.7 + second-install e2e after the fix round changed API assertions — the implementer had not rerun kind — none.
- Final review I1 — Ruling: the term list moves to a secret — GitHub prints step `env` values and does not mask variables — none.

## Deferred minor findings (for the phase review)
- Task 19: base images pinned by tag, not digest.
- Task 20: no chart icon.
- Task 21: release runs no tests and has no tag-on-main check; after a force-push with an unreachable `before`, only the head commit is scanned; an invalid regex in the term list could leak through the perl error (masked now that it is a secret).
- Task 22: the edge row's Healthy state is not asserted.
- Final review: binary reports `v0.1.0` while image and chart use `0.1.0`; hygiene does not scan file content of intermediate PR commits; actions pinned by tag, not SHA; no separate shellcheck step in CI.
- Re-review: if e2e becomes a required check, docs-only PRs wait on a skipped check; Dependabot PRs get no secrets and would fail hygiene.
