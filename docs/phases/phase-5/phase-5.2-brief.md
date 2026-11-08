# Sub-phase 5.2 — Metrics and checks into the pack

Kind: planned
Base: 98807aa
Issues: n/a | Plan steps: implementation-plan.md, Sub-phase 5.2 (G2), Tasks 6, 6a, 7

## Problem / goal
After 5.1 a class-only install exists, but the kgateway layer still reaches it. `pkg/app` starts the controller metrics scraper, the proxy stats collector and the controller-logs collector for every build and every install (`pkg/app/app.go:255-298`). The engine reads one `ctrlmetrics.Result` per install and hands it to every check, the scope verifier, the reported version and the model's freshness (`pkg/engine/engine.go:338-389`, `pkg/engine/loop.go:74`). Every registered check runs on every install, so a class-only install runs the kgateway checks (001, 007, 022, 024, 025, 026) and the kgateway branches of `conditionsCheck` (015 on `kgateway.dev/Programmed`).

5.2 makes controller metrics a kgateway-pack collector value, read only through `RunContext.Collected` (the user's clean break, design §7), gives a one-shot caller `ctrlmetrics.CollectOnce` and `Publish`, and runs a pack's checks only on the installs that pack claimed. A kgateway user sees no change.

## Expected behaviour

| Input / action | Expected |
|---|---|
| `New()` app over a kgateway install | the scrape set equals the base |
| `New()` app with class-only installs on | a class-only install is never scraped for metrics, proxy stats or logs |
| `NewCore()` app | no metrics, proxy stats or controller-logs collector starts |
| `setup.Build` with a downstream collector named like a pack collector | rejected, as at the base |
| A controller that rejects config | KGWUI-001 with the same id, evidence and `since` as at the base |
| A metrics entry older than its max age | 001 and 024 "not available" with today's stale wording; the Gateway data-plane detail still shows the last snapshot |
| Two installs scraped at different times | each entry goes stale on its own age |
| A count mismatch between the UI and `kgateway_resources_managed` | 007, as at the base |
| `kgateway_build_info` | still sets the reported version and the rule set |
| One-shot: design §7.3 sequence (`CollectOnce` + `Publish`) | the same KGWUI-001 as the long-running path; without `Publish`, 001 is "not available" |
| Class-only install with a NACK-shaped status, `kgateway.dev/Programmed=False`, a missing policy target, a backendRef to an HTTPRoute and Envoy-looking proxy Pods | no 001, 007, 010, 011, 012, 015, 016, 022, 024, 025, 026 |
| kgateway install next to it | findings (IDs and order) byte-equal to the base |
| Core checks on a class-only install, with and without metrics, logs and proxy stats in the collected snapshot | equal findings, memoized recomputes included |
| API `golden` and `golden-v1` | unchanged |

## Root cause (if known)
Wiring: `app.Build` builds the collectors unconditionally (`pkg/app/app.go:255-298`); metrics travel outside the collector seam (`engine.Config.Metrics`, `health.Input.Metrics`, `scopeverify.Input.Metrics`); `health.Evaluate` runs one check list for every install (`pkg/engine/engine.go:421`); `conditionsCheck` reads kgateway condition types as constants (`pkg/health/checks.go:217`, `pkg/health/rules.go:62`).

## Acceptance tests
- `pkg/app`: scrape set for kgateway installs; class-only never scraped; `NewCore()` starts no pack collector.
- `pkg/setup`: reserved collector names; the E8 one-shot sequence over a rejecting controller, with and without `Publish`.
- `pkg/engine` and `pkg/api`: 001 identity, staleness per entry, 007, reported version, data-plane detail; `golden` and `golden-v1` unchanged.
- `pkg/engine`: the class-only check filter and the core-checks-ignore-pack-data test (design §7.4).
- `examples/downstream` unchanged.

## Scope
In: `pkg/ctrlmetrics`, `pkg/collect` (if needed), `pkg/engine`, `pkg/health`, `pkg/scopeverify`, `pkg/api` (data-plane detail), `pkg/extension` (`CollectorEnv`, pack collectors, per-install checks), `pkg/kgateway`, `pkg/app`, `pkg/setup`, test helpers under `internal/testutil`, README upgrade note. Task 6a's program-file diff and kgwctl issue body are written outside the repo; nothing is posted.
Out: `viewa`, `Delegator`, no computed view without a rule set (5.3); proxy mode, process, commands and verifier per install, and condition types in `pkg/explore` (5.4); web (5.5).

## Plan / design impact
None expected. Any ruling where the plan leaves a choice is recorded in the 5.2 complete file.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite and gates pass, bench gates recorded
- [ ] cross-model review of `git diff 98807aa..HEAD` + triage
- [ ] phase-5.2-complete.md written; phase-status.md updated
