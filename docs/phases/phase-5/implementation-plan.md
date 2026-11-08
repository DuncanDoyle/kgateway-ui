# Phase 5 — Generic Gateway API core: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the core implementation-independent Gateway API, with kgateway as pack #1 on the extension seam, and no visible change for kgateway users.

**Architecture:** A `Pack` bundles one implementation's kinds, detectors, checks, collectors and per-install hooks (`Impl`). `extension.New()` stays "core + kgateway"; `extension.NewCore()` is the core alone. Installs no pack claims become class-only installs, keyed by GatewayClass `controllerName`. The engine dispatches per install. No package moves.

**Tech Stack:** Go, React/TypeScript (Vitest, Playwright), Helm, GitHub Actions, kind.

**Spec:** `docs/phases/phase-5/design.md` (D-061, D-065; proposes D-066 … D-071).

## Global Constraints

- No package moves. Every exported name kgwctl imports keeps compiling with today's meaning (design §11): `extension.New`, `kinds.Upstream`, `kinds.KgatewayGroup`, `install.UpstreamDetector`, `install.ProxyPodDetector`, `ctrlmetrics.Static`, `engine.Config.Metrics`, `health.Input.Metrics`.
- API v1 changes are additive and optional. The `golden-v1` files stay byte-equal for kgateway installs.
- Finding codes keep `KGWUI-`. No new codes.
- Per-install or per-stage hooks only; nothing per object. No new informers.
- Bench gates unchanged (warm recompute ≤ 150 ms, stream CPU, retained heap ≤ 48 MiB, dense memory, OS memory). A break stops the work and goes to the user.
- Public repo: no vendor or edition names; no implementation is named as a test target (D-065).
- Comments 1–2 lines; tests assert observable behaviour at the highest practical boundary (API or engine output), not internals.

## Review Focus

1. A kgateway install's findings, pages and API responses are identical before and after each sub-phase.
2. A class-only install never shows a kgateway-pack finding, the delegation tab, or a data plane section.
3. A controller with a non-Envoy data plane gets no Envoy-derived output anywhere.
4. A downstream that uses today's `With…` options keeps its behaviour (`examples/downstream` unchanged).
5. kgwctl `main` still compiles against this branch.

---

## Sub-phase 5.1 (G1): packs, class-only installs, default scope

### Task 1: `Pack`, `Impl` and `NewCore`

**Files:** `pkg/extension/extension.go`, new `pkg/extension/pack.go`, new `pkg/kgateway/pack.go`, `pkg/extension/extension_test.go`.

**Interfaces:**
```go
type Impl struct {
	Scope      install.ScopeProvider
	Verifier   scopeverify.Verifier
	Settings   func(install.Install) install.Settings
	RulesFor   func(version string) computed.Selection
	Version    func(install.Install) string // version used for rule-set choice
	ProxyMode  proxymode.Resolver
	Process    process.Source
	ProxyStats proxystats.Source
	Commands   diagnose.Commands
	Conditions health.ConditionTypes
}
type Pack struct { Name string; Kinds []kinds.Descriptor; Detectors []install.Detector; Impl Impl; Checks []health.Check; Collectors []func(CollectorEnv) Collector; Features []string }
func NewCore(opts ...Option) *Registry
// pkg/health: registration lists only in this task; Task 7 adds dispatch
func Core() []Check; func Kgateway() []Check // Upstream() = today's list and order
func WithPack(p Pack) Option
func (r *Registry) ImplFor(i install.Install) Impl // "" → core defaults
// pkg/kgateway, no import of extension:
func Kinds() []kinds.Descriptor; func Detector() install.Detector; func Checks() []health.Check; ...
```
`New(opts...)` = `NewCore(WithPack(kgatewayPack()), opts...)`. Detectors from `WithDetectors`, hooks from the old `With…` options and direct `Registry` field assignments belong to the default pack (`kgateway` in `New`, `core` in `NewCore`); `ImplFor` reads the fields at build time (design §4.2). `Impl` also carries `ExperimentalOnly func(served kinds.Served, inst install.Install) bool`.

- [ ] **Step 1:** Failing tests: `New()` yields the same `Kinds`, `Detectors`, `Checks` order and hooks as the base; `NewCore()` yields Gateway API kinds only, no detectors, `health.Core()` checks; `WithComputedRules` on `New` changes the kgateway pack's `RulesFor`; an install found by a `WithDetectors` detector gets the kgateway pack's `Impl` (engine output equal to the base for `examples/downstream`).
- [ ] **Step 2:** Implement; `go test ./pkg/extension/... ./examples/...` pass.
- [ ] **Step 3:** Commit.

### Task 2: `kinds` split

**Files:** `pkg/kinds/upstream.go`, `pkg/kinds/kinds_test.go`, `pkg/kgateway/kinds.go`.

**Interfaces:** `kinds.GatewayAPI() []Descriptor`; `kgateway.Kinds() []kinds.Descriptor`; `kinds.Upstream()` returns both in today's order (v1 kinds first, the post-v1 kinds last).

- [ ] **Step 1:** Test: `Upstream()` equals the base list element by element; `GatewayAPI()` has no `gateway.kgateway.dev` kind. The experimental-only table stays in `kinds` (it is keyed by kgateway minor); document it as kgateway-pack data and read it only for kgateway installs (Task 4).
- [ ] **Step 2:** Implement; commit.

### Task 3: class-only installs

**Files:** `pkg/install/install.go`, new `pkg/install/class.go`, `pkg/install/install_test.go`, `pkg/engine/engine.go`, `pkg/app/app.go` (flag and chart value), `deploy/helm/snellius/values.yaml`, `templates/deployment.yaml`, `deploy/helm/chart_test.go`.

**Interfaces:**
```go
type Install struct { …; Impl string } // pack name; "" for class-only
func ClassOnly(classes []*unstructured.Unstructured, claimed []Install) []Install
func ClassOnlyID(controllerName string) string // tilde-escape every byte outside A-Z a-z 0-9 . -
const SourceAssumed = "assumed"
```
Flag `--class-only-installs`, chart `installs.classOnly` (default false); `cmd/snellius-core` forces it on.

- [ ] **Step 1:** Failing engine-level tests over a snapshot with a kgateway controller and GatewayClasses of `kgateway.dev/kgateway` and `example.com/gateway`: with the flag on, two installs, the class-only one `example.com_gateway`, display "Gateway API controller `example.com/gateway`", scope `all`/`assumed`, no Deployment; the kgateway controller name gets no twin; with the flag off, one install. `ClassOnlyID` passes `ValidateID` for names with `:`, `_`, `~` and extra `/`; `example.com/a/b` and `example.com/a_b` get different IDs; no ID equals a workload ID `<ns>.<name>`. With the controller, proxy and process checks running on the class-only install, there is no panic and they report "not applicable" (nil Deployment, `pkg/health/checks.go:169,202`).
- [ ] **Step 2:** Implement discovery after detectors; routes assign by Gateway `controllerName` as today; nil-workload handling in every check that reads the Deployment.
- [ ] **Step 3:** API test: the install DTO for a class-only install has empty edition and version, no readiness, and the scope source "assumed"; `golden-v1` unchanged.
- [ ] **Step 4:** Commit.

### Task 4: per-install settings, scope and version

**Files:** `pkg/install/scope.go` (`Resolve`), `pkg/install/settings.go`, `pkg/install/upstream.go`, `pkg/engine/engine.go:340`, tests.

**Interfaces:** `install.ResolveWith(insts []Install, implFor func(Install) (ScopeProvider, func(Install) Settings)) []Resolved`; `Resolve` stays as today for old callers. `Install.KgatewayVersion()` stays; the engine calls `Impl.Version`.

- [ ] **Step 1:** Tests: a class-only install never reads `KGW_*` env (settings unknown with reason "not read for this controller"); a kgateway install resolves exactly as at the base; the scope check message for "assumed"; a class-only install with an experimental-channel kind gets no experimental advice at the three callers (`pkg/health/checks.go:577`, `pkg/api/details.go:441`, `pkg/engine/views.go:105`), while kgateway installs keep today's.
- [ ] **Step 2:** Implement; commit.

### Task 5: `cmd/snellius-core` and the core-only image

**Files:** new `cmd/snellius-core/main.go`, `pkg/app/app.go:449` (`RunWith(ctx, reg *extension.Registry, o Options)`; `Run` calls it with `extension.New(opts...)`), `Makefile` (`build-core`, `image-core`), `Dockerfile` (build arg for the cmd path).

- [ ] **Step 1:** Test in `pkg/app`: a registry from `NewCore()` builds with class-only installs on, and `/meta` lists no `gateway.kgateway.dev` kind. (Collectors move in Task 6; their absence is tested there.)
- [ ] **Step 2:** Implement; `make build-core` and `docker build --build-arg CMD=snellius-core .` succeed. Commit.

**5.1 exit:** `go test ./...`, `go vet ./...`, web tests, `golden-v1` unchanged, bench gates recorded in `phase-5.1-complete.md`.

---

## Sub-phase 5.2 (G2): metrics and checks into the pack

### Task 6: collectors from packs

**Files:** `pkg/app/app.go:255-298`, `pkg/kgateway/collectors.go`, `pkg/app/app_test.go`.

**Files (also):** `pkg/extension/pack.go` (`CollectorEnv`), `pkg/setup/setup.go:120-128`.

**Interfaces:** `CollectorEnv{Clients; Options; Clock; Installs func() []install.Install; Collected *collect.Store; SetMetrics func(ctrlmetrics.Source)}`. The kgateway pack's factories build the metrics collector (wrapping `ctrlmetrics.Collector`, scraping only installs with `Impl == "kgateway"`), the proxy stats collector and the controller-logs collector (when enabled). `engine.Config.Metrics` is fed through `SetMetrics`; its type is unchanged. `setup.Build` builds the same factories; the reserved names stay reserved for the pack.

- [ ] **Step 1:** Test: with `New()`, the scrape set equals the base for kgateway installs; a class-only install is never scraped; with `NewCore()`, no metrics, proxy stats or controller-logs collector starts; `setup.Build` still rejects a downstream collector with a reserved name.
- [ ] **Step 2:** Implement; commit.

### Task 7: check split and per-install filter

**Files:** `pkg/health/checks.go` (`Core`, `Kgateway`, `Upstream`), `pkg/engine/engine.go`, `pkg/health/store.go`, new `internal/boundary/checks_test.go`.

**Interfaces:** `health.Core() []Check`, `health.Kgateway() []Check`, `health.Upstream()` = today's list and order. `Pack.Checks` run only on installs of that pack; core checks run on all.

- [ ] **Step 1:** Failing engine test: a class-only install with a NACK-shaped status, a `kgateway.dev/Programmed=False` route condition, a route that names a missing policy target, a backendRef to an HTTPRoute and Envoy-looking proxy Pods raises no 001, 007, 010, 011, 012, 015, 016, 022, 024, 025, 026; the kgateway install's findings (IDs and order) are byte-equal to the base. The `conditionsCheck` branches for 011, 012, 015, 016 run through the install's `Delegator`, `PolicyTargeter` and `ConditionTypes` (design §6).
- [ ] **Step 2:** Behaviour test: core checks on a class-only install give equal findings with and without metrics, controller-log events and proxy stats in the input, across a memoized recompute. Restart and rollout checks keep their optional log enrichment for kgateway installs.
- [ ] **Step 3:** Implement; bench; commit.

---

## Sub-phase 5.3 (G3): rule sets, view A, delegation

### Task 8: `pkg/computed/viewa`

**Files:** new `pkg/computed/viewa/*.go`; `pkg/computed/kgw23`, `kgw24`, `kgw25` (`attach.go`, `allowed.go`, `hostnames.go`, `isolation.go`, `keys.go`).

**Interfaces:** `viewa.Attach(in computed.AttachmentInput, o viewa.Options) computed.AttachmentResult` with `Options{AllowedKinds func(protocol string) []objects.GroupKind}`. Listener isolation stays in `kgw24`/`kgw25`, applied as an annotation after `viewa` (design §8.1).

- [ ] **Step 1:** Run `pkg/computed` goldens for 2.3, 2.4, 2.5 and the chain benchmarks; record as the baseline.
- [ ] **Step 2:** Move the shared attachment rules into `viewa`; each set calls it with its options.
- [ ] **Step 3:** Goldens pass unchanged; chain benchmarks within 5 % of baseline allocations; non-test lines in `kgw2x` drop. Commit.

### Task 9: `Delegator`

**Files:** `pkg/computed/rules.go`, `pkg/computed/kgw2{3,4,5}/rules.go` and the tests that call `New().Delegation` (`traverse_test.go`), `pkg/install/assign.go:257`, `pkg/objects/httproute.go:279,313` callers, `pkg/health/refs.go:79,291`, `pkg/explore/admit.go:125`, `pkg/engine/views.go`, `pkg/api/details.go`, `pkg/api/handlers.go:65`, `README.md` (upgrade note: callers assert `computed.Delegator`).

**Interfaces:** `computed.Rules{Version; Attachment}`; `computed.Delegator{Delegation}` optional.

- [ ] **Step 1:** Failing API test with an attachment-only rule set (no `Delegator`): `GET …/delegation` is 404; route detail has no delegation ref state; refs and admission treat a backendRef to an HTTPRoute as an ordinary ref; no 011; view A, its cross-check and a 012 from an attachment disagreement still appear. kgateway goldens unchanged.
- [ ] **Step 2:** Implement; commit.

### Task 10: no computed view without a rule set

**Files:** `pkg/engine/views.go`, `pkg/api/details.go`, tests.

- [ ] **Step 1:** Test: a class-only install's Gateway and route detail carry status-level attachment only, and no `computed` badge.
- [ ] **Step 2:** Implement; bench (warm recompute, retained heap); commit.

---

## Sub-phase 5.4 (G4): the rest of the kgateway pack

### Task 11: condition types, proxy mode, process, commands, verifier per install

**Files:** `pkg/health/checks.go:217`, `pkg/health/rules.go:62`, `pkg/explore/rows.go:28`, `pkg/process/kube.go:22`, `pkg/engine/engine.go`, `pkg/kgateway/impl.go`.

**Interfaces:** `health.ConditionTypes{RouteOptional, RouteRejected []string}` on `Input` and on the explore builder; `process.Source` from `Impl`; class-only installs get a process source that reports no controller and no proxies ("not applicable").

- [ ] **Step 1:** Tests: a class-only route with a `kgateway.dev/Programmed=False` condition is not judged by it; a kgateway route is, as at the base; a class-only Gateway's process set is "not applicable" and raises no 005, 006, 019–021.
- [ ] **Step 2:** Implement; commit.

### Task 12: core string boundary

**Files:** `internal/boundary/imports_test.go`.

- [ ] **Step 1:** Test, per file like today's allowlist: a file in a core package (design §4.1) that imports `pkg/kgateway`, `ctrlmetrics`, `ctrllogs`, `proxystats`, `scopeverify`, `proxymode`, `computed/kgw*` or `computed/rulesets` must be listed with a reason (wiring, the §7 and §11 compatibility surface, or the pack checks still housed in `pkg/health`). The list only shrinks.
- [ ] **Step 2:** Fix what it finds; commit.

---

## Sub-phase 5.5 (G5): web

### Task 13: install features in API v1

**Files:** `pkg/api/types.go:15`, `pkg/api/handlers.go:227`, `web/packages/shell/src/api/types.ts`, current goldens under `pkg/api/testdata/golden`.

**Interfaces:** `InstallSummary.Features []string` (`omitempty`), from the pack's `Features` and the rule set (`delegation` only with a `Delegator`); the rule-set label as `computed.label`.

- [ ] **Step 1:** Test: kgateway installs list `delegation`, `computed`, `dataplane`, `processes`; class-only lists none; current goldens gain only the optional fields; frozen `golden-v1` is untouched and `additive_test.go` passes.
- [ ] **Step 2:** Implement; commit.

### Task 14: vocabulary, badge, backend kind, About

**Files:** `web/packages/shell/src/shell/vocabulary.tsx`, `explore/ComputedBadge.tsx`, `shell/ObjectHeader.tsx`, `pages/ResourcePage.tsx`, `pages/AboutPage.tsx`, new `web/packages/shell/src/kgateway/webPack.ts`, `web/app/src/main.tsx`.

- [ ] **Step 1:** Vitest: core vocabulary has no "kgateway"; with `kgatewayWebPack` the empty state shows today's label hint; the badge shows the server label; a Backend is detected by category.
- [ ] **Step 2:** Implement; commit.

### Task 15: feature-gated surfaces and the delegation tab

**Files:** `web/packages/shell/src/pages/explore/RoutePage.tsx:15,168`, `pages/explore/route/DelegationTab.tsx`, `pages/explore/GatewayPage.tsx:103`, `explore/ComputedBadge.tsx`, `kgateway/webPack.ts`, tests, `web/e2e`.

- [ ] **Step 1:** Vitest: the tab, delegation permission copy and child/effective-match blocks appear only with `delegation`; computed badges only with `computed`; the Gateway page shows "Not available for this controller" without `dataplane`/`processes`; Playwright (kgateway e2e) unchanged.
- [ ] **Step 2:** Implement; commit.

---

## Sub-phase 5.6 (G6): the compatibility check and close

### Task 16: generic smoke test

**Files:** new `test/compat/compat_test.go` (tag `compat`), `web/e2e/compat.spec.ts`, `.github/workflows/e2e.yaml` (one step on one row).

**Interfaces:** env `COMPAT_CONTROLLER`, `COMPAT_GATEWAY` (`ns/name`), `COMPAT_ROUTE` (`ns/name`), `COMPAT_UI_URL`, `COMPAT_DEFAULT_UI_URL`.

**Files (corrected):** `.github/workflows/ci.yaml` (vet and core image build), not the e2e matrix.

- [ ] **Step 1:** Write the assertions of design §12.1. Public CI runs only `go vet -tags compat ./test/compat/...` and `make image-core`; no behavioural compat run in this repo (D-065).
- [ ] **Step 2:** Commit.

### Task 17: the private compatibility CI (outside this repo)

The private repo exists (user, 2026-11-08). Its workflows are written there, never here, and this plan does not name it or its targets.

- [ ] **Step 1:** In the private repo: a workflow that checks out Snellius at a ref, builds `image-core`, sets up kind, installs each controller the user chose with its matching Gateway API minor, and runs `test/compat` and the compat Playwright spec.
- [ ] **Step 2:** Results go to the user by hand; never an issue in this repo.

### Task 18: kgwctl compile job, docs, close

**Files:** `.github/workflows/ci.yaml` (job building kgwctl `main` through a generated `go.work`), `README.md` (packs, `NewCore`, class-only installs, upgrade notes), `docs/documentation-notes.md`.

- [ ] **Step 1:** CI job green. A one-shot fixture test (`setup.Build`, `ctrlmetrics.Static`, `engine.Compute`) gives the same findings as at the base (E8).
- [ ] **Step 2:** Propose `v0.6.0`; at close, update the program file and send the `cross-repo` issue to kgwctl (E2, E3, E8).
