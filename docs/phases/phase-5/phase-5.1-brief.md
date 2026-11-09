# Sub-phase 5.1 — Packs, class-only installs, default scope

Kind: planned
Base: 9059cfe
Issues: n/a | Plan steps: implementation-plan.md, Sub-phase 5.1 (G1), Tasks 1–5

## Problem / goal
The core only works with kgateway. Every install comes from a kgateway detector, and the registry holds one scope provider, one rule-set selector and one settings reader for every install (`pkg/extension/extension.go:173-190`, `pkg/engine/engine.go:340`, `pkg/install/scope.go:219-225`). A cluster whose Gateway API controller is not kgateway shows "No kgateway install found", even though its GatewayClasses, Gateways and routes carry status the core could show.

5.1 adds the seam: packs with per-install hooks, a core registry with no pack, class-only installs for controllers no pack claims, and a core-only binary. A kgateway user sees no change.

## Expected behaviour

| Input / action | Expected |
|---|---|
| `extension.New()` | the same kinds, detectors, checks (same order) and hooks as at the base |
| `extension.NewCore()` | Gateway API kinds only (no `gateway.kgateway.dev` kind), no detectors, `health.Core()` checks |
| `New(WithComputedRules(f))` | the kgateway pack's rule-set choice uses `f` |
| An install found by a `WithDetectors` detector in `New` | gets the kgateway pack's hooks; engine output equals the base |
| `kinds.Upstream()` | the base list, element by element; `kinds.GatewayAPI()` has no `gateway.kgateway.dev` kind |
| Snapshot with a kgateway controller and GatewayClasses of `kgateway.dev/kgateway` and `example.com/gateway`, class-only installs on | two installs; the second has ID `example.com~2Fgateway`, display "Gateway API controller example.com/gateway", scope `all` from "assumed", no Deployment |
| Same, class-only installs off (the default binary's default) | one install, as at the base |
| The kgateway controller name | never gets a class-only twin |
| `ClassOnlyID` for names with `:`, `_`, `~` and extra `/` | passes `ValidateID`; `example.com/a/b` and `example.com/a_b` differ; never equals a `<namespace>.<name>` workload ID |
| Controller, proxy and process checks on a class-only install | no panic; they report "not available for this controller", no finding |
| `GET /api/v1/installs` for a class-only install | empty edition and version, readiness not observed, scope source "assumed"; `golden-v1` unchanged |
| Settings of a class-only install | never read from `KGW_*` env; unknown with reason "not read for this controller" |
| Scope check on a class-only install | OK, with "Scope assumed: all namespaces. Snellius cannot read this controller's discovery settings" |
| An experimental-channel kind on a class-only install | no experimental advice in health, the ListenerSet page or view A; kgateway installs keep today's |
| A registry from `NewCore()` built by `pkg/app` | builds with class-only installs on; `/meta` lists no `gateway.kgateway.dev` kind |
| `make build-core` | builds `cmd/snellius-core` |

## Root cause (if known)
Wiring, not logic: `extension.New` hard-wires the kgateway kinds, detector, scope, checks and rule sets (`pkg/extension/extension.go:173-190`); `install.Discover` only returns detector matches (`pkg/install/install.go:89`); `install.Resolve` reads `KGW_*` env for every install (`pkg/install/scope.go:219`); experimental advice reads the kgateway table for every install (`pkg/health/checks.go:577`, `pkg/api/details.go:441`, `pkg/engine/views.go:105`); `controllerCheck` dereferences the install's Deployment (`pkg/health/checks.go:169,202`).

## Acceptance tests
- `pkg/extension`: `New` against the base lists and hooks; `NewCore` kinds, detectors, checks; `WithComputedRules` reaching the kgateway pack's hooks; a `WithDetectors` install resolving to the kgateway pack.
- `pkg/kinds`: `Upstream()` against the base list; `GatewayAPI()` has no kgateway kind.
- `pkg/install`: `ClassOnly` and `ClassOnlyID` cases above; `ResolveWith` scope and settings for class-only and kgateway installs.
- `pkg/engine`: `Compute` over the two-controller snapshot with the flag on and off; every check on a class-only install without panics; experimental advice per install.
- `pkg/api`: install DTO of a class-only install; `golden-v1` untouched.
- `pkg/app`: a `NewCore()` registry builds and `/meta` has no kgateway kind.
- `deploy/helm`: `installs.classOnly` renders `--class-only-installs`.

## Scope
In: `pkg/extension`, new `pkg/kgateway`, `pkg/kinds`, `pkg/install`, `pkg/engine`, `pkg/health` (registration lists, nil workload, assumed scope), `pkg/setup`, `pkg/api` (experimental advice per install), `pkg/app`, `cmd/snellius`, new `cmd/snellius-core`, `Makefile`, `Dockerfile`, Helm chart and its tests.
Out: the metrics collector move and the per-install check filter (5.2); `viewa`, `Delegator` and "no computed view without a rule set" (5.3); condition types, proxy mode, process, commands and verifier per install (5.4); web (5.5); the smoke test (5.6).

## Plan / design impact
None expected. Any ruling where the plan leaves a choice is recorded in the 5.1 complete file.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite and gates pass, bench gates recorded
- [ ] cross-model review of `git diff 9059cfe..HEAD` + triage
- [ ] phase-5.1-complete.md written; phase-status.md updated
