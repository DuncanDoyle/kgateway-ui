# Sub-phase 3.0 — Extension seams

Kind: planned
Base: 3eceda6d315a958d8099d44fc9e5348e7bd2bddc
Issues: n/a | Plan steps: implementation-plan.md Tasks 1–11; design §4.1, §15 (seam rows); D-003, D-013, D-016, D-038

## Problem / goal
A downstream distribution can add kinds, detectors, checks, an Authorizer, rule sets, routes, nav entries, finding renderers and whole pages. It cannot add a feature with its own data, API and UI without forking `app.Run`, the shell package or the chart. Sub-phase 3.0 closes the seven gaps of design §4.1 with generic, neutral seams, before 3.1 breaks the check contract. The stock UI does not change.

## Expected behaviour

| Input / action | Expected |
|---|---|
| `WithChecks("ACME-", c)` | Registered under the prefix; `KGWUI-`, a duplicate or a malformed prefix makes `Registry.Err()` non-nil and `app.Build` fails before anything starts (Task 2) |
| A downstream detector sets `Install.BaseVersion` `v2.4.7` | Rule-set selection and experimental classification use `kgateway-2.4` for that install; unset means unknown (Task 3) |
| `WithHandlers("acme", …)` with `GET /widgets` | `GET /api/ext/acme/widgets` is served; unknown paths under it get the API's JSON 404; a pattern outside `/api/ext/acme/` fails the build; `api.Visible` drops objects the Authorizer denies (gap 1, Task 4) |
| `WithCapabilities` reports `acme-widgets` `on` / `denied` | `on`: listed in `capabilities` and `capabilityStates`; `denied` with reason: only in `capabilityStates`; a core name collides → registry error; no source → `meta.json` byte-identical (gap 4, Task 5) |
| A collector publishes a value | A new fingerprint triggers a recompute; the same fingerprint does not; an expired or stopped value reads as unavailable, never as current; the confirm tick never stamps a stale publication (gap 2, Task 6). Checks read it through `Input.Collected` until 3.1 moves it to `RunContext` (gap 3 lands with D-032) |
| Chart values `rbac.extraRules`, `extraEnv`, `extraVolumes`, `extraVolumeMounts`, `extraContainers` | Rendered into the ClusterRole and Deployment; the default render stays read-only, without Secrets or `*/proxy` (gap 7, Task 7) |
| `make shell-pack` | A tarball with `dist/` JS and type declarations and no `src/`; its exports equal `src/index.ts`; the app and tests still resolve source (gap 5, Task 8) |
| `ShellExtension.contributions` | Detail tabs (with `?tab=` surviving reload), side sections, finding actions and install panels render; an empty slot renders no element; a tab colliding with a core tab is ignored with a warning (gap 6, Task 9) |
| `examples/downstream` | Uses every seam; its Go tests, chart overlay and web package (built against the packed shell) run in CI (Task 10) |

## Root cause (if known)
n/a (planned work). The gaps are listed in design §4.1.

## Seam audit (Task 1, Phase 1 design §14.4, carry-over P4)

| Refactor | Needed by a 3.0 seam? | Outcome |
|---|---|---|
| Neutral `Install` and `Detector` | Only the base version (downstream detectors cannot report the kgateway version they build on) | `Install.BaseVersion` in Task 3; the rest of the neutral shape carries on |
| Provider interface | No | carries on (standalone mode) |
| Metrics by URL | No | carries on |
| Injection in `app.Build` | No: seams are options on the existing registry | carries on |
| Kinds without discovery | No | carries on |
| Object revision and incarnation | No | carries on |
| Static install definition | No | carries on |
| Exported SPA embed | No: the shell package (gap 5) covers the web side | carries on |
| Selector scope with missing Namespace objects | No | carries on |
| D-009 §1 for file mode | No | carries on |

## Acceptance tests
- Task 2: `TestWithChecksRejectsReservedPrefix`, `TestWithChecksRejectsDuplicatePrefix`, `TestWithChecksRejectsMalformedPrefix`, `TestBuildFailsOnRegistryError`.
- Task 3: `TestKgatewayVersionUsesBaseVersion`, `TestRuleSetFromBaseVersion` (through `engine.Compute` and the routes endpoint).
- Task 4: `TestExtMountServesUnderPrefix`, `TestExtCannotShadowCoreOrSPA`, `TestVisibleAppliesAuthorizer`, `TestExtHandlersSeeOnePublication`.
- Task 5: `TestMetaListsExtensionCapability`, `TestCapabilityNameCollision`, the additive test on `meta.json`.
- Task 6: `TestPublishNotifiesOnlyOnNewFingerprint`, `TestSnapshotPrintIsOrderIndependent`, `TestExpiredValueIsUnavailable`, `TestStoppedCollectorValuesExpire`, `TestFailedValueKeepsReason`, `TestConfirmRefusesStaleCollectedValue`, `TestCollectorStartsAndStopsWithApp`.
- Task 7: `TestExtraRulesAppended`, `TestExtraEnvVolumesAndContainers`, the default-render read-only test.
- Task 8: `"the built package exports what src exports"`; the tarball holds `.d.ts` and no `src/`.
- Task 9: the six contribution tests of the plan.
- Task 10: `TestExampleUsesEverySeam`, `TestChartOverlayRendersExtraRule`, `"the Widgets tab renders on a Gateway page from the packed shell"`.

## Scope
In: `pkg/extension`, `pkg/collect` (new), `pkg/api` (ext mount, capabilities), `pkg/install`, `pkg/engine` (collected print), `pkg/health` (`Input.Collected`), `pkg/app`, `deploy/helm/kgateway-ui`, `web/packages/shell`, `web/app` and `web/tsconfig.base.json` (source condition), `examples/downstream`, `.github/workflows`, `Makefile`, README.
Out: the D-032 check contract and `RunContext` (3.1); `WithProcessSource` and `WithCommands` (3.2); any downstream-specific code or name; publishing the shell to a public registry; the other §14.4 refactors.

## Plan / design impact
None. D-038 records the seams. Approval of the plan covers the ⚠ guardrail tasks 6, 8, 9 and 10, whose file lists the plan names (ruling 2026-11-01, user approved the entire phase).

Reminders for the user (not blocking): the open item in design §25 (filing the reject-status draft), and the upstream scope-endpoint issue (carry-over P3). Filing both stays the user's call.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] standard gates pass; bench within 5 % of the 2.5 record on warm recompute and stream CPU
- [ ] Codex review of `git diff 3eceda6..HEAD` + triage (`reviews/3.0-codex.md`)
- [ ] phase-3.0-complete.md written; phase-status.md and roadmap.md updated; README upgrade note (`WithChecks` takes a prefix)
