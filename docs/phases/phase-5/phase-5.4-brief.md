# Sub-phase 5.4 — The rest of the kgateway pack

Kind: planned
Base: 3a96594
Issues: n/a | Plan steps: implementation-plan.md, Sub-phase 5.4 (G4), Tasks 11–12; the analyzer budget note in phase-5.3-complete.md

## Problem / goal
**Budget.** The analyzer stage's "memoized, new scrape each run" worst case allocates 1,024,336 B of its 1,027,000 B budget (99.7 %). It was 995,368 B at 5.1 (98807aa). It must come down before more work lands on that path, without changing behaviour or the budget.

**Pack hooks per install.** `extension.Impl` carries `Verifier`, `ProxyMode`, `Process` and `Commands`, but the engine and the API use one registry-wide value for every install. A class-only install runs the kgateway scope verifier and process source, and a second pack's hooks are ignored. The explorer still reads `kgateway.dev/Programmed` as a constant (`pkg/explore/rows.go:28`), so a class-only route with that condition is flagged "not programmed".

**Boundary.** Nothing stops a core file from importing a kgateway-pack package (design §13).

Task 7 (5.2) already moved condition types into `Impl.Conditions` for the checks (`pkg/health/checks.go:454`, `pkg/health/rules.go:72`). Task 11 here does the explorer's part and the other four hooks.

## Expected behaviour

| Input / action | Expected |
|---|---|
| `TestBudgetAnalyzerStage` | the worst "new scrape" case is below 1,027,000 B again; every other row unchanged; the metric checks still re-run on each new scrape |
| Class-only route with `kgateway.dev/Programmed=False` | no `not-programmed` flag; its parent condition is not that type |
| kgateway route with the same condition | flagged as at the base |
| Class-only install | no scope verifier runs; its process sets are empty ("not applicable"); no 005, 006, 019–021 |
| Install of a second pack | that pack's verifier, process source, proxy mode and commands |
| kgateway install | findings, pages and next steps as at the base; the default verifier still applies |
| A core file that imports a pack package | listed with a reason, or the boundary test fails; the list only shrinks |

## Root cause (if known)
Budget: the 5.2 harness (`pkg/engine/bench_test.go`, `withScrape`) builds each new scrape inside the measured run, including two `ctrlmetrics.Results.Fingerprint` calls (about 51 KB). That is the collector's work; 5.1 assigned `in.Metrics` for free. To be confirmed by an alloc profile at 98807aa and HEAD.

Hooks: `engine.compute` calls `e.cfg.Verifier`, `e.cfg.Process` and passes `e.cfg.ProxyMode`; `engine.Hooks` has no field for them. `api.handler` holds one `diagnose.Commands`. `explore.Input` has no condition types.

## Acceptance tests
- `pkg/engine` (bench tag): the analyzer budget test, with a guard that the two scrapes are timed apart.
- `pkg/api`: class-only route not flagged by `kgateway.dev/Programmed`; the kgateway route is.
- `pkg/engine`: kgateway, a second pack and a class-only install each get their own verifier, process source and proxy mode; the class-only one gets none.
- `pkg/setup`: `Build` maps each pack's `Impl` to its installs' hooks; the default verifier stays the default pack's.
- `pkg/api`, `pkg/app`: next steps use the install's pack commands.
- `internal/boundary`: the pack-import allowlist, with a self-test.

## Scope
In: `pkg/engine` (hooks, bench harness), `pkg/setup`, `pkg/explore`, `pkg/api` (commands per install), `pkg/app` (wiring), `internal/boundary`, tests.
Out: install features and web (5.5); moving the pack checks out of `pkg/health`, policy ownership and Backend judgement (carry-over); `computed.EnvoyNamer`; the budget values themselves.

## Plan / design impact
None expected. Rulings where the plan leaves a choice go in the 5.4 complete file.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite and gates pass, bench gates recorded
- [ ] cross-model review of `git diff 3a96594..HEAD` + triage
- [ ] phase-5.4-complete.md written; phase-status.md updated
