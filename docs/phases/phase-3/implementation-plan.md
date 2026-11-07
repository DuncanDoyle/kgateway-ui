# Phase 3 — Health & Diagnostics: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extension seams first (3.0), then the performance work, the KRT decision and the new check contract (3.1), then the health overview with xDS rejects, processes and proxy Envoy stats (3.2), the analyzer with suggested fixes and opt-in log signals (3.3), and backend readiness plus kgateway 2.5 (3.4).

**Architecture:**
- **Seams (3.0).** `pkg/extension` gains endpoints under `/api/ext/{name}/`, collectors that publish into a `pkg/collect` store, capabilities with states, and the finding-code prefix registry. The shell ships as a built package with in-page contribution slots; the chart gains extension values. `examples/downstream` uses every seam in CI.
- **Check contract (3.1, D-032).** `health.Check` becomes `Name`, `Inputs`, `Run(RunContext)`. The engine owns one `health.Store` per (install, check), memoizes each check on the fingerprint of its declared inputs and its next wake time, and assigns every finding a stable `id` and, where the check leaves it unset, a first-seen `since`.
- **Diagnosis (3.1–3.4).** `pkg/diagnose` groups findings by cause, ranks groups (§5.3), and builds next steps and suggested fixes. `pkg/process` is the process seam (D-016) with one allowlisted Kubernetes source. `pkg/proxystats` and `pkg/ctrllogs` are core collectors on the 3.0 seam.
- **API.** New paths `…/findings`, `…/findings/{findingId}`, `…/diagnostics`, `…/processes`; every other change is an optional field.

**Tech Stack:** Go 1.26, client-go v0.36 (plus `k8s.io/client-go/metadata` for CRD metadata), React 19, TypeScript, emotion, react-router, SWR, TanStack Table, Vitest, Testing Library, Playwright, kind, Helm.

**Spec:** `docs/phases/phase-3/design.md` (binding). Section numbers below (§5.1, §16.3) refer to it. Phase 2's plan holds the conventions this plan does not repeat (fake clocks, `testutil.Recv`, goldens, the additive test). D-001 … D-026 hold. D-028 … D-038 are appended to `docs/decisions.md` when the user approves design and plan (queue item Q3); D-027 is appended in Task 14, after the spike. The design and plan review is `reviews/design-codex.md`; its triage table is binding.

## Global Constraints

- **Public repo.** The forbidden terms of the repository secret `FORBIDDEN_TERMS` (product and commercial-edition names of the sponsoring vendor) never appear in code, comments, file names, docs, commits or PR text. Downstreams are "downstream distributions"; examples use `acme`. No customer names, internal links or ticket ids. Before each commit: `git diff --cached | grep -inE "$FORBIDDEN_TERMS" && echo STOP || true`.
- **Read-only (D-001).** No writes to the cluster. No Secrets, ConfigMaps, `*/proxy` or `pods/portforward` in the chart. `pods/log` only in a Role in the install namespace, and only when `diagnostics.controllerLogs` is on (§8.9, §18.1). Never read Envoy or controller admin ports, termination messages, probe output, proxy logs or Event messages (§18.2; see Task 35 for the probe-type note).
- **Don't mirror controller logic (principle 4).** Computed rules live behind `computed.Rules` with a verifier (D-020). Log parsers, Envoy name mapping and the Gateway API version table sit on the rule-set seam, pinned per kgateway minor; a shipped rule set is never edited for a new minor.
- **API v1 additive only.** New paths or optional fields. Existing goldens change only by `id`, `group`, `evidence` and the fields §13.1 lists; `pkg/api/additive_test.go` asserts it. **One listed exception (§7.1, D-030):** the pre-release code migration changes the `code` and `slug` values of named findings (002 → 013, 014, 015; 003 → 016). Each owning task (57, 59, 61) adds its findings to `additiveExceptions` in `pkg/api/additive_test.go` by golden file and resource, and moves the matching `test/e2e/api_test.go` expectations in the same commit.
- **Import boundary (D-016).** The allowlist is per file. New `k8s.io/api/core/v1`, `apps/v1` or `client-go` use only in these files, each added to `internal/boundary/imports_test.go` with its reason by the task that creates it:
  - `pkg/cache/events.go` — "Warning Events on controller and proxy Pods" (Task 35)
  - `pkg/cache/crds.go` — "Gateway API CRD metadata through the metadata client" (Task 35)
  - `pkg/cache/services.go` — "Services and EndpointSlices" (Task 75)
  - `pkg/process/kube.go` — "process facts from Pods, Deployments and Events" (Task 37)
  - `pkg/proxystats/kube.go` — "the Kubernetes proxy stats source: proxy Pod targets and transport" (Task 41)
  - `pkg/ctrllogs/stream.go` — "follows controller Pod logs" (Task 65)
  - `pkg/setup/setup.go` — "builds cache and engine config from the Kubernetes clients" (Task 71a, D-053)
  `pkg/collect`, `pkg/diagnose`, `pkg/extension`, the rest of `pkg/proxystats` and the new `pkg/health` files stay outside the allowlist.
- **Availability (D-034).** No Phase 3 check sets `Required: true`. A check whose input is off, denied, failing or absent on this version returns `health.NotAvailable(...)`; it never raises KGWUI-008 and never turns the install unknown. The existing required checks keep their flag, `xDS rejects` included.
- **Go style.** Go Doc Comments on every exported name. Code comments 1–2 lines, grouped, describing the code as it is (no history). Match the surrounding idiom.
- **Deterministic tests.** Fake clock and `testutil.Recv`; no `time.Sleep` in new unit tests; holds and maturations are proven by stepping the clock to `deadline − 1 s` and `deadline + 1 s`. Only `bench`-tagged budget tests use a real clock.
- **Behavioural boundary (AGENTS.md §7).** Every code ships with an API boundary test, cluster objects and metrics in, API JSON out, positive and negative, plus isolation (a second install) where the code reads per-install data. Helper tests supplement, never replace.
- **Budgets (§16.3; user, 2026-10-29).** Warm recompute ≤ 150 ms; stream CPU (annotation-only) ≤ 15 % of one core; dense memory medians of 5: retained heap ≤ 176 MiB, allocated by the cold recompute ≤ 246 MiB and per warm recompute ≤ 185 MiB (D-039); retained heap ≤ 48 MiB (D-057; 40 MiB in 3.3, D-054; 32 MiB before 3.3) and ≤ +24 MiB above `bench/3.1-baseline.txt`; analyzer stage ≤ 20 ms warm and ≤ 2 ms memoized (from 3.2); spec-edit stream ≤ 25 % (from 3.2); OS memory during the stream: median of five ≤ 288 MiB (from 3.2, D-043); `…/diagnostics`, `…/findings`, `…/processes` p95 < 50 ms, p99 < 100 ms, p95 < 100 ms with 20 clients; burst, idle and cold gates unchanged. **No gate changes without the user. A broken gate stops the work; the numbers go to the user.**
- **Frontend quality.** Every interactive element has hover, active and focus-visible styles in both themes; state is icon + colour + text at WCAG AA, checked by `web/packages/shell/src/theme/themes.test.ts`; skeletons and slots keep their final size from the first render (no layout shift); no horizontal page scroll at 720, 1280, 1600 and 3008 px. `SHELL_API_VERSION` stays 1.
- **Wording pass (§17.5).** Every new user-facing string: short sentences, plain English, active voice. Per string, a separate sub-agent receives five accurate options and picks the one that sounds least machine-written and is most accurate. Drafts in the design are placeholders. Choices are recorded in `phase-3.N-wording.md`.
- **e2e.** Only the kind cluster `kgwui-e2e`, created by `test/e2e/setup.sh` with its own kubeconfig `$E2E_DIR/kubeconfig`. Manual commands pass `--kubeconfig "$E2E_DIR/kubeconfig" --context kind-kgwui-e2e` (Helm: `--kube-context kind-kgwui-e2e`). Never touch another cluster or the global context.
- **Git.** New commits only; never rewrite history. Trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Stage files by name. Never commit `TODO.md`, `resume_claude_session`, `.claude/` or `docs/phases/phase-2/design-feedback_do-not-store-in-git.md`. No pushes, PRs or posts without the user's go-ahead.
- **Housekeeping.** Don't delete temp files mid-task (overwrite; remove in one pass at the end). Stop only processes you started, by PID.
- **Change-size rule (AGENTS.md §6).** Approval of this plan covers each task's file list. Tasks marked **⚠ guardrail** touch more than 5 files or are expected above 400 lines and need the user's explicit go-ahead when they start. Any task that needs more files than listed stops and asks.

## Review Focus

Five input classes the design's test list (§21) does not cover, most likely to bite users; each has a test in its owning task.

1. **Two installs in one cluster.** Stores, proxy Pods, Events, proxy stats, log events and finding ids must never cross installs. → Task 15 `TestStoresAreIsolatedPerInstall`; Task 37 `TestProxyPodsBelongToOneInstall`; Task 41 `TestProxyStatsPerInstall`.
2. **Counter resets and Pod churn.** A restarted controller replica or proxy resets `rejects_total` and Envoy counters; a delta must never go negative or read as a new reject, and a Pod UID that disappears must leave no state behind. Proxy stats are evidence of rejects observed since the last scrape, never an active-reject trigger (review finding 4). → Task 21 `TestCounterDeltaPerPodUIDNeverNegative`; Task 42 `TestProxyStatsResetIsNotAReject`.
3. **Gateway names over 63 characters.** The `gateway-name` label is truncated; proxy Pods, proxy stats and 019–022, 025, 026 must still map to the right Gateway, and two Gateways sharing a truncated prefix must not merge. → Task 37 `TestLongGatewayNameProxyPods`.
4. **Pre-release, custom and digest image tags.** `v2.5.0-rc.1`, `v2.4.7-acme.1`, `latest`, `@sha256:…`, a controller on 2.4.7 and proxies on an RC. Skew compares parsed major.minor only; anything unparseable is "version unknown" with no finding; rule-set selection keeps the "newer than tested" note. → Task 39 `TestSkewUnparseableAndPrereleaseTags`; Task 61 `TestNewerThanTestedUsesComputed016`.
5. **Events that race their Pod, and aggregated Events.** An Event can arrive before the informer knows its Pod, and `count` jumps on aggregation and resets when the Event expires. 020 counts observed deltas per Event UID in a 15-minute window; a count present when the UI first sees an Event is "unknown", never counted; a series is counted once (review finding 7). → Task 35 `TestEventBeforePodIsKeptOnNextUpdate`, `TestFirstSeenCountIsUnknown`; Task 38 `TestAggregatedEventCountIsAWindowNotATotal`.

## Sub-phases

Each sub-phase starts with a brief (`docs/phases/phase-3/phase-3.N-brief.md`, agentic-engineering-sdlc format) in its first task, ends green and committed, gets its own Codex review of its diff, its own Large-tier benchmarks, and ships alone (§23).

| Sub-phase | Goal | Tasks | Exit criteria |
|---|---|---|---|
| **3.0 Extension seams** | A downstream adds a whole feature without forking (§4.1, D-038) | 1–11 | Seven seams and the two §15 seam carry-overs; `examples/downstream` uses each, tested in CI (Go, chart overlay, web package from the tarball); README extension docs and stability rule; stock UI unchanged (goldens, bench numbers unchanged); Codex review triaged |
| **3.1 Foundations, performance and the KRT decision** | Same pages, easier to read; findings with ids, paged on the server | 12–33 | Baseline on `544dcd5` recorded (local and CI); spec-edit variant reported; KRT spike and D-027; D-032 contract, Store and memoization; every check migrated; README upgrade notes; delegation memory carry-over; captures and per-Pod metrics; ids, `since`, groups and rank; findings endpoints and pages; scale tiers; ObjectHeader, fluid layout, breadcrumbs; wording pass; 3.1 budgets; Codex review triaged |
| **3.2 Data plane, processes and the overview** | The ranked health overview with xDS rejects and process health | 34–54 | Watches and toggles; `pkg/process`; 001 re-specified; 019–022, 025, 026; proxy stats; diagnostics and processes endpoints; install page, Processes page, Gateway sections; RBAC and README fix; `renderWhenFontsReady`; wording pass; all §16.3 gates incl. analyzer stage and spec-edit; kind e2e on 2.3.11 and 2.4.7; Codex review triaged |
| **3.3 Analyzer** | The analyzer floor and freshness, with suggested fixes | 55–73, 71a | 003 kinds; 013 (no Service data), 014, 015, 016 computed with rules section and verifier, 017, 018; four groups and the grouped view; fixes for 013 and 014; opt-in log signals; `docs/findings.md`; wording pass; the one-shot config helper and static metrics source (D-053); budgets with the log reader; kind e2e; Codex review triaged |
| **3.4 Backends and kgateway 2.5** | Empty backends; 2.5 supported at GA | 74–87, 78a, 78b | Service and EndpointSlice watches; 023; 013 Service explanations and port fix; platform-neutral test; wording pass; budgets; kind e2e. After 2.5.0 GA (D-033): `kgw25`, 016 from status, 024, `kgateway_build_info`, the 2.5 e2e row with the TCPRoute fixture on v1; Codex review triaged. If GA is not out, Tasks 83–86 wait and the rest ships. kgwctl #17 (three kinds) and #20 (`cache.Wait`) by D-056 |

**Lifecycle.** A sub-phase's first task records it in `docs/phase-status.md` under `## Sub-phase` (`3.N <name>`, `Status: active`, `Base: <git rev-parse HEAD>` before its first commit). Its review diffs from that base: `BASE=$(awk '/^## Sub-phase/{f=1} f && /^Base:/{print $2; exit}' docs/phase-status.md)`. Its close task writes the README upgrade notes, updates `docs/roadmap.md` and resets `## Sub-phase`. Each brief reminds the user of the open item in §25 and the scope-endpoint issue (carry-over P3); filing stays the user's call.

**Step convention.** A code task runs: the failing tests; the run command, which must fail for the stated reason; the implementation; the same run plus the task's gates, which must pass; the commit with the task's message and the trailer. Steps list only what decides something. Go tasks gate on `go vet ./... && go test ./... -race`; web tasks on `cd web && npm test && npm run typecheck && npm run lint`.

**Standard gates** (each close task, pasted into the record): `go vet ./... && go vet -tags e2e ./...`; `go test ./... -race`; `TZ=Europe/Amsterdam go test ./pkg/api`; `make lint`; `cd web && npm test && npm run typecheck && npm run lint && npm run build`; `helm lint deploy/helm/kgateway-ui && REQUIRE_HELM=1 go test ./deploy/...`; the forbidden-terms check; `make bench`; the kind e2e matrix. Expected: every command exits 0; `go test` prints `ok` for every package; the forbidden-terms check prints nothing.

## File Structure

`(3.N)` is the sub-phase that creates or first changes the file.

```
pkg/extension/extension.go          WithChecks(prefix, …), Err, WithHandlers, WithCollectors, WithCapabilities (3.0); WithProcessSource, WithCommands (3.2)
pkg/extension/deps.go               Deps, Router, Collector (3.0)
pkg/collect/collect.go              Store, Value, Snapshot: collector values with fingerprints (3.0)
pkg/api/ext.go                      ExtMount, WriteJSON, WriteError, Visible (3.0)
pkg/api/capability.go               Capability, CapabilityState, core capability source (3.0; core states 3.2)
pkg/api/handlers.go                 NewHandler options, /api/ext mount, meta capabilityStates (3.0); findings routes (3.1); diagnostics, processes (3.2)
pkg/api/findings.go, diag_types.go  findings list and detail, DTOs of §13.3 (3.1; grows per sub-phase)
pkg/api/diagnostics.go, processes.go  (3.2)
pkg/install/install.go, upstream.go Install.BaseVersion (3.0); ProxyPodDetector (3.2)
pkg/health/finding.go               Check contract, RunContext, InputSet, Finding.ID/Key/Group/Facts (3.1)
pkg/health/store.go                 Store, Load, States (3.1)
pkg/health/identity.go              FindingID, GroupID (3.1)
pkg/health/rollup.go                Evaluate with Runtime: ids, since, wake times, memo (3.1)
pkg/health/avail.go                 Availability, NotAvailable (3.2)
pkg/health/evidence.go              typed evidence per kind (3.2, 3.3, 3.4)
pkg/health/xds.go                   KGWUI-001 re-specified (3.2)
pkg/health/process.go               019, 020, 021, 005/006 detail (3.2)
pkg/health/skew.go                  022 (3.2)
pkg/health/proxystats.go            025, 026 (3.2)
pkg/health/refs.go                  013, 014 (3.3; Service explanations 3.4)
pkg/health/rules.go                 015 (3.3)
pkg/health/targets.go               016 (3.3; status path 3.4)
pkg/health/conflicts.go             017 (3.3)
pkg/health/freshness.go             003 kinds, 018 (3.3)
pkg/health/endpoints.go             023 (3.4)
pkg/health/withheld.go              024 (3.4)
pkg/diagnose/group.go, rank.go      families, groups, rank, Diagnosis (3.1; families 3.2–3.4)
pkg/diagnose/nextsteps.go           Commands, Kubectl, Quote, next steps per family (3.2)
pkg/diagnose/fix.go                 suggested fixes (3.3, 3.4)
pkg/diagnose/index.go               precomputed rank order and keyset cursors (3.1)
pkg/process/process.go              Source, Set, Process, Rollout, Termination, ProbeFailures (3.2)
pkg/process/kube.go                 Kubernetes source (3.2; allowlisted)
pkg/proxystats/{source,collector,parse,stats}.go, kube.go   replaceable proxy stats source (3.2; kube.go allowlisted)
pkg/ctrllogs/{collector,events}.go, stream.go          controller log reader (3.3; stream.go allowlisted)
pkg/computed/logs.go, envoynames.go LogParser and EnvoyNamer seams (3.2, 3.3)
pkg/computed/kgw23/, kgw24/         PolicyTargeter and log parsers as optional interfaces (3.3)
pkg/computed/kgw25/                 copy of kgw24 plus TargetNotFound (3.4)
pkg/computed/rulesets/gatewayapi.go expected Gateway API bundle per minor (3.2; 2.5 entry 3.4)
pkg/computed/bound.go               MaxPathBytes, ClipDisplay (3.1)
pkg/ctrlmetrics/parse.go, scrape.go per Pod and type; rejects_total, updates_dropped (3.1); snapshot_resources (3.2); status_syncs (3.3, removed in 3.5); per-client and build_info (3.4)
pkg/ctrlmetrics/static.go           static Source over one Collect result, for one-shot callers (3.3, D-053)
pkg/setup/setup.go                  cache.Config and engine.Config as pkg/app builds them (3.3, D-053; allowlisted)
pkg/scopeverify/verify.go           dropped updates → KGWUI-008 (3.1)
pkg/cache/cache.go, events.go, crds.go, services.go   proxy Pods, Events, CRD metadata (3.2); Services, EndpointSlices, `Wait` (3.4); new files allowlisted
pkg/engine/engine.go, memo.go       States per install, memo, Diagnosis in InstallModel (3.1); process data (3.2)
pkg/explore/rows.go, query.go       gateway-rejecting flag and filter (3.2)
pkg/kinds/catalog.go                served group/kinds from discovery for 013 (3.3)
pkg/kinds/upstream.go               DirectResponse, GatewayExtension, ReferenceGrant descriptors (3.4, D-056)
pkg/app/app.go, cmd/kgateway-ui/main.go   seams wiring (3.0); diagnostics toggles and collectors (3.2, 3.3, 3.4); config through pkg/setup (3.3)
internal/testutil/scale/scale.go    Phase 3 tier fields and worst case (3.1)
internal/boundary/imports_test.go   seven allowlist entries (3.2, 3.3, 3.4; Global Constraints)
examples/downstream/                every seam (3.0); new contract (3.1); web/ and chart/ overlays (3.0)
deploy/helm/kgateway-ui/            extension values (3.0); diagnostics toggles and rules (3.2, 3.3, 3.4)
web/packages/shell/package.json, tsconfig.build.json   built package (3.0)
web/packages/shell/src/shell/{extension.ts,contributions.tsx}  contributions (3.0)
web/packages/shell/src/shell/{ObjectHeader.tsx,layout.ts,breadcrumbs.ts,Breadcrumbs.tsx,renderWhenFontsReady.ts}  (3.1, 3.2)
web/packages/shell/src/api/{diagTypes.ts,diagHooks.ts}   (3.1; grows per sub-phase)
web/packages/shell/src/diagnostics/{NextSteps,SuggestedFix,EvidenceView,GroupRow,ProcessLine,ProcessTable}.tsx  (3.2, 3.3)
web/packages/shell/src/pages/findings/{FindingsPage,FindingPage}.tsx  (3.1; grouped view 3.3)
web/packages/shell/src/pages/install/{NeedsAttention,ProcessesSection}.tsx, pages/processes/ProcessesPage.tsx, pages/explore/gateway/{DataPlaneSection,ProxiesSection}.tsx  (3.2)
web/packages/shell/src/copy/diagnostics.ts, copy/navigation.ts   wording-pass strings (3.1–3.4)
web/e2e/navigation.spec.ts, diagnostics.spec.ts   (3.1, 3.2)
test/e2e/fixtures/{nack,diagnostics,analyzer,backends}.yaml, test/e2e/diagnostics_test.go   (3.1–3.4)
docs/findings.md                    one entry per code (3.3, 3.4)
docs/phases/phase-3/computed-rules.md   policy targets (3.3)
docs/phases/phase-3/bench/          baseline, spike, per-sub-phase output
docs/phases/phase-3/phase-3.N-{brief,complete,wording}.md, reviews/3.N-codex.md
```

---

# Sub-phase 3.0 — Extension seams

No change for users of the stock UI: every seam is empty by default, and goldens stay byte-identical except `/meta`'s optional `capabilityStates` (absent while no source reports one). 3.0 has no user-facing copy, so it has no wording pass; slots with no contribution render nothing.

### Task 1: 3.0 brief and seam audit

**Files:** Create `docs/phases/phase-3/phase-3.0-brief.md`; Modify `docs/phase-status.md`.

- [ ] **Step 1: Record the sub-phase** (Lifecycle) and write the brief: goal, Tasks 1–11, exit criteria, the seven gaps of §4.1 as the expected-behaviour table, the reminder of §25.
- [ ] **Step 2: Audit the Phase 1 design §14.4 refactors** (neutral `Install` and `Detector`, carry-over P4) against the seams. Record per item: needed by a §4.1 seam (do it in the named task) or carried on. Expected outcome: only `Install.BaseVersion` (Task 3) is needed; the rest carries on.
- [ ] **Step 3: Commit** `docs: brief for sub-phase 3.0 (extension seams)`.

### Task 2: Finding-code prefix registry

**Files:** Modify `pkg/extension/extension.go:26-43` (Registry gains `prefixes map[string]bool`, `errs []error`), `:65-70` (WithChecks), `pkg/app/app.go:84-88` (fail on `reg.Err()`); Test `pkg/extension/extension_test.go`, `pkg/app/app_test.go`.

**Interfaces:**
- `func WithChecks(prefix string, c ...health.Check) Option` — `prefix` matches `^[A-Z][A-Z0-9]{1,15}-$`.
- `func (r *Registry) Err() error` — joined registration errors; `app.Build` returns it before anything starts.
- Upstream checks are registered under `KGWUI-` by `New`; a second `KGWUI-` registration, a duplicate prefix or an invalid prefix is an error.

- [ ] **Step 1: Failing tests** `TestWithChecksRejectsReservedPrefix` (`WithChecks("KGWUI-", c)` → `Err()` names the prefix), `TestWithChecksRejectsDuplicatePrefix` (two `ACME-` registrations), `TestWithChecksRejectsMalformedPrefix` (`acme`, `ACME`, `A-`), `TestBuildFailsOnRegistryError` (`app.Build` returns the error; no informer starts).
- [ ] **Step 2: Run** `go test ./pkg/extension ./pkg/app -run 'WithChecks|RegistryError'` → FAIL (signature mismatch).
- [ ] **Step 3: Implement.** README "Extending" mentions the prefix argument in Task 10.
- [ ] **Step 4: Commit** `feat(extension): finding-code prefix registry`.

### Task 3: Base kgateway version for downstream detectors

**Files:** Modify `pkg/install/install.go:17-36` (field), `pkg/install/upstream.go:75-82` (`KgatewayVersion`); Test `pkg/install/install_test.go`, `pkg/computed/rulesets/rulesets_test.go`.

**Interfaces:** `Install.BaseVersion string` — the kgateway version a downstream build is based on, set by its detector; empty means unknown. `KgatewayVersion()` returns `Version` for edition `kgateway`, else `BaseVersion`.

- [ ] **Step 1: Failing tests** `TestKgatewayVersionUsesBaseVersion` (edition `acme`, `BaseVersion` `v2.4.7` → `v2.4.7`; unset → `""`), `TestRuleSetFromBaseVersion` (rule-set selection and experimental classification pick `kgateway-2.4` for that install, through `engine.Compute` and `/api/v1/installs/{id}/routes`).
- [ ] **Step 2: Implement, run, commit** `feat(install): optional base kgateway version for downstream detectors`.

### Task 4: Extension endpoints (`WithHandlers`)

**Files:** Create `pkg/extension/deps.go`, `pkg/api/ext.go`, `pkg/api/ext_test.go`; Modify `pkg/extension/extension.go`, `pkg/api/handlers.go:35-56`, `pkg/app/app.go:155-158`.

**Interfaces:**
- `package extension`:
  - `type Deps struct { Current func() *engine.Published; Authorizer api.Authorizer; Capabilities func() []api.Capability; Publish func(key string, value any, fingerprint uint64); Logger *slog.Logger }` — `Publish` is nil for handlers (Task 6 sets it for collectors).
  - `type Router interface { Handle(pattern string, h http.Handler) }` — `pattern` is `"[METHOD ]/path"` relative to `/api/ext/{name}`.
  - `func WithHandlers(name string, f func(r Router, d Deps)) Option` — `name` matches `^[a-z][a-z0-9-]{0,62}$`, unique; else `Err()`.
- `package api`:
  - `type ExtMount struct { Name, Pattern string; Handler http.Handler }`
  - `type HandlerOption func(*handlerConfig)`; `func WithExtMounts(m ...ExtMount) HandlerOption`
  - `func NewHandler(src ModelSource, ui fs.FS, authz Authorizer, opts ...HandlerOption) http.Handler` (existing callers unchanged)
  - `func WriteJSON(w http.ResponseWriter, v any)`, `func WriteError(w http.ResponseWriter, code int, msg string)` (exported wrappers of today's helpers)
  - `func Visible[T any](r *http.Request, a Authorizer, items []T, ref func(T) objects.Ref) (visible []T, hidden int)` — skips Authorizer calls for `AllowAll` (Phase 2 rule 10).

- [ ] **Step 1: Failing tests** in `pkg/api/ext_test.go`: `TestExtMountServesUnderPrefix` (`GET /api/ext/acme/hello` → 200 from the extension; `GET /api/ext/acme/missing` → the API's JSON 404), `TestExtCannotShadowCoreOrSPA` (a pattern resolving outside `/api/ext/acme/` is rejected at build), `TestVisibleAppliesAuthorizer` (deny-some Authorizer: hidden objects dropped, `hidden` counted; AllowAll makes no calls), `TestExtHandlersSeeOnePublication`.
- [ ] **Step 2: Run** `go test ./pkg/api -run Ext` → FAIL.
- [ ] **Step 3: Implement** the mount: `mux.Handle(method+" /api/ext/"+name+path, h)` registered before the `/api/` 404 catch-all.
- [ ] **Step 4: Commit** `feat(extension): endpoints under /api/ext/{name}`.

### Task 5: Capabilities with states (`WithCapabilities`)

**Files:** Create `pkg/api/capability.go`; Modify `pkg/api/types.go:46-63`, `pkg/api/handlers.go:112-140`, `pkg/extension/extension.go`, `pkg/app/app.go`; Test `pkg/api/api_test.go`, `pkg/api/additive_test.go`.

**Interfaces:**
- `type CapabilityState string` with `CapOn = "on"`, `CapOff = "off"`, `CapDenied = "denied"`, `CapFailing = "failing"`.
- `type Capability struct { Name string \`json:"name"\`; State CapabilityState \`json:"state"\`; Reason string \`json:"reason,omitempty"\` }`
- `MetaResponse.CapabilityStates []Capability \`json:"capabilityStates,omitempty"\`` — sorted by name; a capability with state `on` is also appended to `capabilities` (today's string list), so v1 clients keep working.
- `func WithCapabilitySources(f ...func() []Capability) HandlerOption`; `extension.WithCapabilities(f func() []api.Capability) Option`.
- A downstream name that collides with a core one (`processes`, `events`, `crds`, `endpoints`, `proxyStats`, `controllerLogs`, platform capabilities) is a registry error.

- [ ] **Step 1: Failing tests** `TestMetaListsExtensionCapability` (`acme-widgets` `on` → in both lists; `denied` with reason → only in `capabilityStates`), `TestCapabilityNameCollision`, and the additive test proving `meta.json` is unchanged with no source.
- [ ] **Step 2: Implement, run, commit** `feat(extension): capabilities with on, off, denied and failing states`.

### Task 6: Collectors (`WithCollectors`) and the collector store

**Files:** Create `pkg/collect/collect.go`, `pkg/collect/collect_test.go`; Modify `pkg/extension/extension.go`, `pkg/engine/engine.go:31-77` (`Config.Collected`), `:338` (`Source.Collected`), `pkg/engine/loop.go:21-35` (Source), `pkg/health/finding.go:104-146` (`Input.Collected`), `pkg/app/app.go:166-180`; Test `pkg/engine/loop_test.go`, `pkg/app/app_test.go`. **⚠ guardrail** (8 files).

**Interfaces:**
- `package collect`: `type State string` (`ok`, `failing`, `off`); `type Value struct { Data any; Fingerprint uint64; At, Expires time.Time; State State; Reason string }`; `type Store struct{...}`; `func New(changes *trigger.Signal, now func() time.Time) *Store`; `func (s *Store) Publish(key string, data any, fingerprint uint64, ttl time.Duration)` (notifies only when the fingerprint changes); `func (s *Store) Fail(key string, st State, reason string)`; `func (s *Store) Stop(collector string)` (expires every value of a stopped collector); `type Snapshot struct { Values map[string]Value; Print uint64 }`; `func (s *Store) Snapshot() Snapshot`; `func (s Snapshot) Get(key string) (Value, bool)` (an expired or non-`ok` value returns its state, never stale data as current).
- **Ownership (review finding 14):** `Publish` transfers ownership of `data`; the collector never mutates it afterwards, and readers treat it as read-only. The doc comment says so; `Snapshot` copies the map, never the values.
- `Snapshot.Print` covers each value's fingerprint, state and whether it has expired, so an expiry is a change for the confirm tick; the engine adds the earliest expiry as a deadline.
- Keys are `<collector name>/<key>`; `Deps.Publish` prefixes the collector's name.
- `extension.Collector interface { Name() string; Start(ctx context.Context, d Deps) error }`; `func WithCollectors(c ...Collector) Option` (unique names).
- `engine.Config.Collected func() collect.Snapshot`; `engine.Source.Collected uint64` — the confirm tick refuses to renew `ConfirmedAt` when the live print differs from the published one (D-019).
- `health.Input.Collected collect.Snapshot` — the read path until Task 16 moves it to `RunContext.Collected` (§4.1 gap 3 lands with the contract in 3.1).
- `app.Start` starts each collector in its own goroutine after the cache starts; an error return is logged once and the collector is not restarted.

- [ ] **Step 1: Failing tests** `TestPublishNotifiesOnlyOnNewFingerprint`, `TestSnapshotPrintIsOrderIndependent`, `TestExpiredValueIsUnavailable` (fake clock at `Expires + 1 s`: `Get` returns state unavailable and the print changed), `TestStoppedCollectorValuesExpire`, `TestFailedValueKeepsReason`, `TestConfirmRefusesStaleCollectedValue` (publish after a computation: the confirm tick sees a different print, schedules a run, and the run publishes the new value), `TestCollectorStartsAndStopsWithApp`.
- [ ] **Step 2: Run** `go test ./pkg/collect ./pkg/engine ./pkg/app -run 'Publish|Collected|Collector'` → FAIL.
- [ ] **Step 3: Implement. Run, commit** `feat(extension): collectors feeding a store that triggers recomputes`.

### Task 7: Chart extension values

**Files:** Modify `deploy/helm/kgateway-ui/values.yaml`, `templates/clusterrole.yaml`, `templates/deployment.yaml`, `deploy/helm/kgateway-ui/README.md`; Test `deploy/helm/chart_test.go`.

**Interfaces:** `rbac.extraRules: []` (appended to the ClusterRole), `extraEnv: []`, `extraVolumes: []`, `extraVolumeMounts: []` (UI container), `extraContainers: []`. `serviceAccount.name` already exists (`_helpers.tpl:13-16`); the README documents it as the target for add-on bindings.

- [ ] **Step 1: Failing tests** `TestExtraRulesAppended`, `TestExtraEnvVolumesAndContainers`, and `TestClusterRoleIsReadOnlyWithoutSecretsOrProxy` extended: it checks the **default** render only; `extraRules` is the operator's choice and the README says so.
- [ ] **Step 2: Implement, run** `REQUIRE_HELM=1 go test ./deploy/...` and `helm lint deploy/helm/kgateway-ui` → PASS; **commit** `feat(chart): extension values for rules, env, volumes and containers`.

### Task 8: The shell as a built package

**Files:** Modify `web/packages/shell/package.json`, `web/app/vite.config.ts`, `web/tsconfig.base.json`, `.github/workflows/release.yaml`, `.github/workflows/ci.yaml`, `Makefile`; Create `web/packages/shell/tsconfig.build.json`. **⚠ guardrail** (7 files).

**Interfaces:**
- `package.json`: `"exports": { ".": { "types": "./dist/index.d.ts", "source": "./src/index.ts", "default": "./dist/index.js" } }`, `"files": ["dist"]`, script `"build": "tsc -p tsconfig.build.json"` (JSX `react-jsx`, declarations on). `private` stays true, so nothing is published to a registry (§4.1 gap 5, our call).
- The app and Vitest resolve the `source` condition (`resolve.conditions` in Vite, `customConditions` in `tsconfig.base.json`), so development needs no build.
- `make shell-pack` → `web/packages/shell/kgateway-ui-shell-<version>.tgz`; the release job sets the version to the tag and runs `gh release upload "$GITHUB_REF_NAME" <tgz>`.

- [ ] **Step 1: Write** `web/packages/shell/src/index.test.ts` case `"the built package exports what src exports"`: after `npm run build -w @kgateway-ui/shell`, `Object.keys(await import("../dist/index.js"))` equals `Object.keys(await import("./index"))`.
- [ ] **Step 2: Implement; run** `cd web && npm run build -w @kgateway-ui/shell && npm test && npm run typecheck && npm run build` → PASS; `make shell-pack && tar tzf web/packages/shell/*.tgz | grep -c 'dist/.*\.d\.ts'` → a count above 0, and no `src/` entries.
- [ ] **Step 3: Commit** `build(shell): built package with type declarations, attached to releases`.

### Task 9: Shell contributions (in-page slots)

**Files:** Create `web/packages/shell/src/shell/contributions.tsx`, `src/shell/contributions.test.tsx`; Modify `src/shell/extension.ts:38-66`, `src/pages/explore/detailParts.tsx`, `src/pages/install/InstallPage.tsx`, `src/pages/install/FindingRow.tsx`, `src/index.ts`, `src/api/types.ts` (`MetaResponse.capabilityStates`), `src/api/hooks.ts` (`useCapabilityState`). **⚠ guardrail** (9 files).

**Interfaces:**
```ts
export interface DetailContext { installId: string; ref: Ref }
export interface DetailTabContribution { id: string; label: string; render: (ctx: DetailContext) => ReactNode }
export interface SideSectionContribution { id: string; title: string; render: (ctx: DetailContext) => ReactNode }
export interface FindingActionContribution { id: string; label: string; applies: (f: Finding) => boolean; render: (f: Finding) => ReactNode }
export interface InstallPanelContribution { id: string; title: string; render: (ctx: { installId: string }) => ReactNode }
export interface Contributions {
  detailTabs?: Record<string, DetailTabContribution[]>;      // key "group/Kind"
  sideSections?: Record<string, SideSectionContribution[]>;  // key "group/Kind"
  findingActions?: FindingActionContribution[];
  installPanels?: InstallPanelContribution[];
}
// ShellExtension.contributions?: Contributions
export function useCapabilityState(name: string): Capability | undefined
```
- A tab id collides with a core tab (`yaml`, `delegation`) → ignored with a console warning. Side sections render after the core side content; until Task 28 adds the side column, they stack at the end of the main column.

- [ ] **Step 1: Failing tests** `"a Gateway detail tab renders and its ?tab= survives reload"`, `"a slot with no contribution renders nothing and takes no space"` (no wrapper element in the DOM), `"a contribution colliding with a core tab is ignored"`, `"finding actions render only where applies() is true"`, `"install panels render below the Gateway table"`, `"useCapabilityState reads capabilityStates"`.
- [ ] **Step 2: Implement; run** the web gates → PASS. Contribution tabs use the DetailTabs styles, so hover, active and focus-visible come with them.
- [ ] **Step 3: Commit** `feat(shell): detail tabs, side sections, finding actions and install panels`.

### Task 10: `examples/downstream` uses every seam

**Files:** Modify `examples/downstream/main.go`, `examples/downstream/main_test.go`, `README.md` (Extending: the Go block equals `main.go`; the prefix argument; endpoints, collectors, capabilities, contributions, chart values); Create `examples/downstream/acme.go` (collector, check, handlers), `examples/downstream/chart/values-acme.yaml`, `examples/downstream/web/{package.json,tsconfig.json,acme.tsx,acme.test.tsx,vitest.config.ts}`; Modify `.github/workflows/ci.yaml` (job step `make example-web`), `Makefile` (`example-web`: `make shell-pack`, `npm --prefix examples/downstream/web install <tgz>`, `tsc --noEmit`, `vitest run`). **⚠ guardrail** (13 files).

**Interfaces:**
- `acmeWidgets` collector publishes key `widgets` every 5 s with a count; `acmeWidgetCheck` (code prefix `ACME-`) reads `Input.Collected` and raises `ACME-001` (warning) when the count is 0; capability `acme-widgets` `on`; handlers `GET /api/ext/acme/widgets` (list filtered by `api.Visible`); a `gateway.networking.k8s.io/Gateway` detail tab "Widgets".
- `values-acme.yaml`: `rbac.extraRules` granting `get, list, watch` on `acme.example.com/acmepolicies`.
- `main.go` stays the README block, so `TestReadmeShowsThisExample` keeps them equal; `acme.go` holds the types the block names.

- [ ] **Step 1: Failing tests** in `main_test.go`: `TestExampleUsesEverySeam` — build the app over fake clients, publish 0 widgets: one recompute later `/health` holds `ACME-001`; `/api/ext/acme/widgets` returns 200 and hides `acme-system` objects; `/api/v1/meta` lists `acme-widgets`; a second `WithChecks("ACME-", …)` fails `app.Build`. `TestChartOverlayRendersExtraRule` (`helm template` with `chart/values-acme.yaml` contains the rule; skipped without Helm unless `REQUIRE_HELM=1`). In `acme.test.tsx`: `"the Widgets tab renders on a Gateway page from the packed shell"`.
- [ ] **Step 2: Run** `go test ./examples/... && make example-web` → FAIL, then implement → PASS.
- [ ] **Step 3: Commit** `feat(examples): the downstream example uses every extension seam`.

### Task 11: Close sub-phase 3.0

**Files:** Create `docs/phases/phase-3/phase-3.0-complete.md`, `docs/phases/phase-3/reviews/3.0-codex.md`, `docs/phases/phase-3/bench/3.0.txt`; Modify `pkg/extension/extension.go` (package doc "Compatibility": after 3.0 the Go extension API changes only with a README upgrade note and a release-note entry; 3.1's contract change is the last planned break), `README.md` (Upgrade notes: `WithChecks` takes a prefix), `docs/phase-status.md`, `docs/roadmap.md`.

- [ ] **Step 1: Run the standard gates** and `make bench | tee docs/phases/phase-3/bench/3.0.txt`. Expected: every budget test passes; warm recompute and stream CPU within 5 % of the 2.5 record (3.0 adds no work to the stock path). A larger change: STOP and report.
- [ ] **Step 2: Codex review**

```bash
codex exec --sandbox read-only -o docs/phases/phase-3/reviews/3.0-codex.md "Review sub-phase 3.0 of the kgateway UI (diff $BASE..HEAD) against docs/phases/phase-3/design.md §4.1, §15 (seam rows) and D-003, D-013, D-016. Focus: no seam names a downstream or hints at absent features; /api/ext cannot shadow core paths; Visible applies the Authorizer like core handlers; collector publishes cannot let the confirm tick stamp stale data; capability states; prefix registry; the built shell package matches src; examples/downstream exercises each seam in CI. Test output: <paste>. Rank findings P1-P4 with file:line." < /dev/null
```

- [ ] **Step 3: Triage** every finding (accept, reject with reason, defer); fix accepted ones with tests; rerun the gates.
- [ ] **Step 4: Write `phase-3.0-complete.md`** (Summary, Reviews, Waivers, Deliverables with gate outputs, bench table vs the 2.5 record) and update `docs/phase-status.md` (`## Sub-phase` none; next 3.1) and `docs/roadmap.md` (the seam milestone shipped as 3.0).
- [ ] **Step 5: Commit** `docs: close sub-phase 3.0`.

---

# Sub-phase 3.1 — Foundations, performance and the KRT decision

Order (§16.1, user 2026-10-29): baselines and the spike first, then the performance work, then features.

### Task 12: 3.1 brief, the spec-edit stream variant and the Phase 2 baseline

**Files:** Create `docs/phases/phase-3/phase-3.1-brief.md`, `docs/phases/phase-3/bench/3.1-baseline.txt`, `bench/3.1-baseline-ci.txt`; Modify `pkg/api/budget_test.go` (spec-edit variant), `docs/phase-status.md`, `Makefile` (`bench-specedit`).

**Interfaces:** `func TestBudgetSpecEditStream(t *testing.T)` (`//go:build bench`): the `TestBudgetSustainedStream` harness at Large with one HTTPRoute **spec** change (a path value toggled) every 100 ms for 10 min (`-short`: 60 s) and 20 clients polling `/routes` every 5 s. Reports engine CPU % of one core, recomputes/s, GC share and OS memory; it gates nothing in 3.1.

- [ ] **Step 1: Write the brief**, including the KRT decision rule of §16.1 verbatim, so it is committed before the spike runs. Commit `docs: brief for sub-phase 3.1 with the KRT decision rule`.
- [ ] **Step 2: Write `TestBudgetSpecEditStream`**; commit it alone (`test(api): spec-edit stream variant (reported)`), so it can be applied to Phase 2 code.
- [ ] **Step 3: Measure on `544dcd5`** in a worktree: `git worktree add ../kgwui-baseline 544dcd5 && cd ../kgwui-baseline && git cherry-pick --no-commit <Step 2 sha>`, then `make bench` and `go test -tags bench -v -run SpecEdit -timeout 20m ./pkg/api` (full 10 min). Prefix the output with `go version`, CPU model and the commit; write it to `bench/3.1-baseline.txt` in the main checkout. Repeat on the 3.0 HEAD into the same file under a second header.
- [ ] **Step 4: CI baseline.** With the user's go-ahead, push a throwaway branch `duncandoyle/baseline3` (544dcd5 plus the test commit) and run `gh workflow run bench.yaml --ref duncandoyle/baseline3`; save the artifact as `bench/3.1-baseline-ci.txt`. The branch is never merged.
- [ ] **Step 5: Gate check.** Any existing gate broken on either commit, or spec-edit CPU above 22 %: **STOP, give the user the numbers** (§16.3). Otherwise record them in the brief.
- [ ] **Step 6: Commit** `docs: Phase 2 baseline for Phase 3 (local and CI)`.

### Task 13: KRT spike (throwaway)

**Files (throwaway branch `duncandoyle/krtSpike`, never merged):** a view A implementation over `istio.io/istio/pkg/kube/krt` collections next to `pkg/engine/views.go`, and the bench harness wired to it. **Files (phase branch):** Create `docs/phases/phase-3/bench/3.1-krt-spike.txt`.

- [ ] **Step 1: Build view A as KRT collections** fed from the same informers; each request still reads one immutable published value (a KRT snapshot copied into `Published` per recompute), or the spike records why that is impossible.
- [ ] **Step 2: Measure at Large, against the current engine on the same machine:** warm recompute (median of 20), spec-edit stream CPU (10 min), retained heap (`TestBudgetHeap`), dense memory (`TestBudgetDenseMemory`, median of 5).
- [ ] **Step 3: Record the dependency:** `go mod graph | wc -l` before and after; new modules; `replace` directives needed (`grep -c '^replace' go.mod`); binary size (`go build -o /dev/null -ldflags=-s` sizes before and after, via a temp output file); the Go version floor (`go list -m -json istio.io/istio | jq -r .GoVersion`); the licence of each new module (`go-licenses report` or each module's LICENSE).
- [ ] **Step 4: Apply the decision rule** in the output file: spec-edit CPU at least 40 % lower, retained heap no more than +16 MiB, one immutable published value per request, no `replace` directives, Apache-2.0 compatible licences → **Adopt**; anything less or inconclusive → **Keep**.
- [ ] **Step 5: Commit** (phase branch, the output file only) `docs: KRT spike measurements`. The spike branch stays local.

### Task 14: Record D-027

**Files:** Modify `docs/decisions.md` (append D-027), `docs/phases/phase-3/phase-3.1-brief.md`; on **Adopt** also `docs/roadmap.md` (a follow-up item "Incremental recompute with KRT for views and analyzers").

- [ ] **Step 1: Append D-027** in the existing format: Context (§16.1), the rule, the numbers from Task 13 and the outcome, Alternatives, Consequences. **Keep:** Phase 3 continues with per-check memoization (Task 18). **Adopt:** the views and analyzers move to KRT in a follow-up phase; Phase 3 still ships the contract with declared inputs and memoization, which KRT can feed later. Neither branch changes the remaining Phase 3 tasks.
- [ ] **Step 2: STOP and show the user** the numbers and the outcome before Task 15.
- [ ] **Step 3: Commit** `docs: D-027 KRT decision with the spike numbers`.

### Task 15: The check contract, the Store and engine-owned state (D-032)

**Files:** Create `pkg/health/store.go`, `pkg/health/store_test.go`; Modify `pkg/health/finding.go:104-171` (Input stays; RunContext, InputSet, Check), `pkg/health/rollup.go:47-76` (Evaluate), `pkg/health/checks.go` (12 checks: `Inputs`, `Run(ctx RunContext)`), `pkg/health/computed.go`, `pkg/health/backend.go`, `pkg/engine/engine.go:113-131,256-330` (States per install), `pkg/engine/engine_test.go`. **⚠ guardrail** (≥ 9 files, about 400 lines changed mostly mechanically).

**Interfaces:**
```go
// InputSet names what a check reads; the engine fingerprints exactly these.
type InputSet struct {
	Kinds       []objects.GroupKind // includes core Pod and Namespace, apps Deployment
	Owned       bool                // the install's assignment: every served kind and Namespaces
	Metrics     bool                // the install's controller metrics result
	Verify      bool                // the scope verifier result
	Computed    bool                // the install's computed views
	KindsChange bool
	Process     bool                // process data (3.2)
	Clock       bool                // reads Now beyond WakeAt deadlines: never memoized
	Checks      []string            // other checks whose output Run reads
	Collected   []string            // collector keys, "<collector>/<key>" (Task 16)
}
func AllInputs() InputSet // Owned, Metrics, Verify, Computed, KindsChange, Clock

type RunContext struct {
	Input
	State *Store // engine-owned per (install, check); survives recomputes, cleared on restart
	// unexported: wake sink, declared input set, outputs of earlier checks
}
func (c RunContext) WakeAt(at time.Time)                      // output may change at at
func (c RunContext) Retain(findingID string)                  // keep a since entry for an id not returned this run
func (c RunContext) Output(check string) (CheckOutput, bool)  // declared Checks only
type CheckOutput struct { Findings []Finding; Result CheckResult }

type Check interface { Name() string; Inputs() InputSet; Run(ctx RunContext) ([]Finding, CheckResult) }

type Store struct{ /* map[string]any */ }
func NewStore() *Store
func (s *Store) Get(key string) (any, bool); func (s *Store) Set(key string, v any); func (s *Store) Delete(key string); func (s *Store) Len() int
func Load[T any](s *Store, key string) (T, bool)
type States struct{ /* check name → *Store, plus memo entries (Task 18) */ }
func NewStates() *States
type Runtime struct { States *States; Print func(InputSet) uint64 } // nil States: fresh Stores (tests); nil Print: no memo
func Evaluate(in Input, checks []Check, rt Runtime) Report        // Report gains Next time.Time `json:"-"`
```
- Checks run in registration order; a declared `Checks` entry must name an earlier check, else `app.Build` fails (`health.ValidateOrder(checks) error`).
- `Evaluate` wraps `in.FirstSeen` per check so a grace read inside the grace period calls `WakeAt(t + Grace)`; the engine's own `seen` wrapper no longer adds grace deadlines and takes `Report.Next` instead.
- Engine: `states map[string]*health.States` keyed by install ID, dropped with a vanished install, under `computeMu`.
- Phase 1/2 checks declare `AllInputs()` except where narrower is exact: `cluster access` (all kinds' Access), `served kinds` (`KindsChange`), `controller scope` (none: `Install` is always an input), `xDS rejects` (`Owned`, `Metrics`, `Clock`).

- [ ] **Step 1: Failing tests** `TestStoreSurvivesRecompute`, `TestStoreClearedOnEngineRestart` (new `engine.New` over the same cache), `TestChecksCannotReadEachOthersStore`, `TestStoresAreIsolatedPerInstall` (Review Focus 1: two installs, one check writing its install ID; each Store holds only its own), `TestOutputRequiresDeclaredEarlierCheck`, `TestPhase2GoldensUnchangedOnNewContract` (every `pkg/api/testdata/golden/*.json` byte-identical).
- [ ] **Step 2: Run** `go test ./pkg/health ./pkg/engine ./pkg/api` → FAIL (undefined).
- [ ] **Step 3: Implement and migrate** every core check. **Step 4:** gates → PASS. **Step 5: Commit** `feat(health)!: one check contract with declared inputs and an engine-owned Store (D-032)`.

### Task 16: Collector inputs, the downstream migration and upgrade notes

**Files:** Modify `pkg/health/finding.go` (`RunContext.Collected`; remove `Input.Collected` from Task 6), `pkg/engine/engine.go` (pass the snapshot), `examples/downstream/acme.go`, `examples/downstream/main.go` (if the block changes), `README.md` (Upgrade notes "Check contract (sub-phase 3.1)": new signature, declaring inputs, moving package-level state to the Store, prefix argument).

**Interfaces:** `func (c RunContext) Collected(key string) (collect.Value, bool)` — returns false for a key not in `Inputs().Collected`.

- [ ] **Step 1: Failing tests** `TestCollectedOnlyForDeclaredKeys`, and `TestExampleUsesEverySeam` (Task 10) now with the check declaring `Collected: []string{"acme-widgets/widgets"}`.
- [ ] **Step 2: Implement, run** `go test ./pkg/health ./examples/...` → PASS; **commit** `feat(health): collector keys as declared check inputs; migrate the downstream example`.

### Task 17: The declared-input guard

**Files:** Create `pkg/health/inputs_test.go`, `internal/testutil/inputs.go` (`PerturbUndeclared(in health.Input, set health.InputSet, seed int64) health.Input`).

- [ ] **Step 1: Write `TestChecksReadOnlyDeclaredInputs`:** for every check in `health.Upstream()` and the example check, over the Phase 2 API fixture world: run once; perturb every undeclared input (drop undeclared kinds from the snapshot, replace undeclared metrics, verify and computed results, move `Now` within `[now, Report.Next)` when `Clock` is false); run again with a fresh Store; findings and `CheckResult` must be equal. A failure names the check and the input.
- [ ] **Step 2: Run** → PASS, or fix the check's `Inputs`. **Commit** `test(health): checks read only their declared inputs`.

### Task 18: Per-check memoization

**Files:** Create `pkg/engine/memo.go`, `pkg/engine/memo_test.go`; Modify `pkg/health/store.go` (memo entries in `States`), `pkg/health/rollup.go` (skip path), `pkg/engine/engine.go` (Print), `pkg/api/memo_test.go`. **⚠ guardrail** (6 files).

**Interfaces:**
- `func (e *Engine) inputPrint(id string, set health.InputSet, ctx printCtx) uint64` — wrapping sum of: `Snapshot.Synced`; `Source.Cache.Kinds[gvr].Hash` **and** `Source.Cache.Access[gvr]` (OK and error text) per declared kind, so a watch that fails or recovers with unchanged objects changes the print (review finding 2) (all served kinds and Namespaces for `Owned`); the install's metrics identity (as `encodeMetrics`); a hash of the verifier `Result`; the views memo prints; `kc.String()`; the process hash (3.2); the prints of declared checks' outputs this run; the declared collected values' fingerprints.
- A check is skipped when `Print` equals its memo, `Clock` is false and its memo's wake time is zero or after now. A skipped check's findings, result and wake time are reused, and its wake time is re-added to the engine's deadlines.
- **KGWUI-012 persistence stays in `pkg/engine/persist.go` (our call):** it runs once over every install's views before health and feeds the index; moving it into a Store would split one step across installs.

- [ ] **Step 1: Failing tests** `TestUnchangedInputsSkipCheck` (counter check: second compute with an annotation-only edit to an undeclared kind → not run), `TestDueDeadlineRunsCheck` (fake clock at `wake + 1 ns` → run), `TestStoreWritingCheckStillSkipped`, `TestMemoNeverOutlivesStaleness` (`xDS rejects` with `Clock`: always runs; a narrow check with `Metrics` and `WakeAt(collected + maxAge)` turns unavailable exactly then), `TestSkippedCheckKeepsGraceDeadline`, `TestAccessFailureAndRecoveryRerunChecks` (objects unchanged, a kind's watch fails: `cluster access` reruns and reports it; it recovers: reruns again).
- [ ] **Step 1b: API equivalence (review finding 13).** `TestMemoizedResponsesEqualUncached` in `pkg/api/memo_test.go`: two engines over the same scripted sequence, one with `Runtime.Print` nil; after each step, `/health`, `/overview`, `/findings` and later `/diagnostics` bodies are byte-equal. Steps: an access failure and recovery, a collector value expiring, a process change (3.2 extends the script), a deadline passing, an annotation-only edit.
- [ ] **Step 2: Implement, run** `go test ./pkg/engine ./pkg/health ./pkg/api -race` → PASS; `go test -run '^$' -bench BenchmarkCompute -count 5 ./pkg/engine` and record the warm delta in the brief. **Commit** `perf(engine): memoize checks on their declared inputs`.

### Task 19: Delegation memory carry-over

**Files:** Create `pkg/computed/bound.go`, `pkg/computed/bound_test.go`; Modify `pkg/computed/kgw23/fit.go:149-166`, `pkg/computed/kgw24/fit.go:149-166` (merge reports `too-large`), `pkg/computed/delegation.go` (reason `too-large` on a context), `pkg/api/details.go:48` and the page-budget accounting, `pkg/api/explain.go` (display clip); Test `pkg/api/delegation_test.go`. **⚠ guardrail** (7 files).

**Interfaces (review finding 1):** `const MaxPathBytes = 4096`. A path used in computation is never cut: `merge` returns `ok=false` when the joined path would exceed `MaxPathBytes`, and the traversal marks that context `unknown` with reason `too-large`, which the completeness rules already treat as incomplete, so no negative finding (KGWUI-011, KGWUI-012) follows. `func ClipDisplay(p string) string` cuts only display strings at a UTF-8 boundary with `…[truncated]`. The 100,000-unit response budget charges reason text and JSON-escaped bytes at 1 unit per 32 bytes after escaping.

- [ ] **Step 1: Failing tests** `TestOversizedInheritIsTooLargeNotDropped` (an 8-layer inherit chain of 1 KiB segments: the child's context is `unknown`/`too-large` on both rule sets, never `dropped`; no KGWUI-011 or KGWUI-012 through `/health`), `TestComputedPathsAreNeverClipped` (a 4,095-byte path still fits literally), `TestDisplayClipOnly` (the API shows the clipped string with the marker), `TestPageBudgetChargesReasonAndEscaping` (a page of rows with 2 KiB reasons full of `"` and `<` stops earlier than one with short reasons; the cursor resumes without loss).
- [ ] **Step 2: Implement, run, commit** `fix(computed): oversized inherited paths are too large to compute; charge reason text and escaping in the page budget`.

### Task 20: Captures: the NACK fixture, controller metrics, proxy stats; 2.5 if cheap

**Files:** Create `test/e2e/fixtures/nack.yaml`, `pkg/ctrlmetrics/testdata/{v2.3.11,v2.4.7}-rejects-{a,b}.txt`, `…-restart.txt`, `pkg/proxystats/testdata/{v2.3.11,v2.4.7}-stats-{ok,rejecting}.txt`; optional `pkg/ctrlmetrics/testdata/v2.5.0-rc.1-perclient.txt`; Modify `test/e2e/setup.sh` (`NACK=1` applies the fixture), `docs/phases/phase-3/phase-3.1-brief.md`.

No NACK fixture exists in `test/e2e` today; this task creates it (design §21).

- [ ] **Step 1: Find two reliable rejects** on both tags, one per xDS type: candidates are an HTTPRoute `RegularExpression` path above Envoy's regex program-size limit (RouteConfiguration) and a cluster-level value Envoy rejects (Cluster). Keep the first pair that, on both tags, gives `kgateway_envoy_xds_rejects_active > 0` with the Gateway still `Programmed=True`. Record the observation in the brief.
- [ ] **Step 2: Capture** with the controller scaled to 2 replicas: `/metrics` from each replica (`kubectl --kubeconfig "$E2E_DIR/kubeconfig" --context kind-kgwui-e2e get --raw /api/v1/namespaces/kgw-a/pods/<pod>:9092/proxy/metrics`), then again after `kubectl delete pod` of one replica; proxy `/stats?usedonly` through the stats listener before and during the reject. Strip nothing but timestamps; check every file with the forbidden-terms grep.
- [ ] **Step 3: 2.5 timebox (1 h).** Run `KGW_VERSION=v2.5.0-rc.1 GWAPI_VERSION=v1.7.0-rc.3 EXPLORER=0 NACK=1 bash test/e2e/setup.sh` and capture `/metrics` with the per-client families and `kgateway_build_info`. If setup fails or the timebox runs out, record "2.5 captures in 3.4" in the brief.
- [ ] **Step 4: Commit** `test: NACK fixture and metrics and proxy-stats captures on 2.3.11 and 2.4.7`.

### Task 21: Controller metrics per Pod and per type

**Files:** Modify `pkg/ctrlmetrics/parse.go:13-82`, `pkg/ctrlmetrics/scrape.go:80-100,211-254`, `pkg/scopeverify/verify.go`; Test `pkg/ctrlmetrics/parse_test.go`, `scrape_test.go`, `pkg/scopeverify/verify_test.go`, `pkg/api/api_test.go`.

**Interfaces:**
- Constants `XDSRejectsTotal = "kgateway_envoy_xds_rejects_total"`, `UpdatesDropped = "kgateway_resources_updates_dropped_total"`.
- `type TypeKey struct { Gateway GatewayKey; TypeURL string }`
- `type PodMetrics struct { UID, Name string; Active, Total map[TypeKey]float64; Dropped float64; DroppedPresent bool }`
- `Metrics.PerPod []PodMetrics` (sorted by name); `Metrics.Rejects` stays the per-Gateway sum; `Result.SameContent` compares `PerPod`.
- `scopeverify`: any Pod's `Dropped > 0` → KGWUI-008 "scope verification unavailable: the controller dropped metric updates" instead of comparing (§8.1).

- [ ] **Step 1: Failing tests** `TestParsePerTypeAndPod` (Task 20 captures: two types, two Pods, summed streams equal today's `Rejects`), `TestCounterDeltaPerPodUIDNeverNegative` (Review Focus 2: the restart capture gives a new UID and a reset counter; growth per UID is never negative, a vanished UID's value is forgotten), `TestDroppedUpdatesTurnScopeCheckUnavailable` (through `/health`: no KGWUI-007, one KGWUI-008 with that text).
- [ ] **Step 2: Implement, run, commit** `feat(ctrlmetrics): rejects per Pod and type, rejects_total and dropped updates`.

### Task 22: Finding identity and `since`

**Files:** Create `pkg/health/identity.go`, `pkg/health/identity_test.go`; Modify `pkg/health/finding.go:63-84` (fields), `pkg/health/rollup.go` (assign ids, fill `since`), `pkg/health/checks.go`, `computed.go`, `delegation.go`, `backend.go` (set `Key`), `docs/findings.md` is not yet written (the README note on `since` lands in Task 33). **⚠ guardrail** (7 files).

**Interfaces:**
- `Finding.ID string \`json:"id,omitempty"\``, `Finding.Key []string \`json:"-"\`` (the §5.1 discriminator, written refs encoded as in `computed-rules.md` §7), `Finding.Group string \`json:"group,omitempty"\``.
- `func FindingID(installID, code string, resource *objects.Ref, key []string) string` — `"f1-" + lower(base32.StdEncoding.WithPadding(NoPadding)(sha256(installID ‖ code ‖ group ‖ kind ‖ namespace ‖ name ‖ key…)))[:26]`, fields joined by `0x00`; the resource after defaults (core group `""`, namespace `""` for cluster-scoped, all empty when nil).
- `func GroupID(installID, family string, cause *objects.Ref, causeKey []string) string` = `FindingID(installID, "G", cause, append([]string{family}, causeKey...))`.
- Keys for existing codes: 002 (owner as written or `self`, condition type), 003 (parentRef as written or `self`), 004 (targeted parent ref), 007 (namespace, resource label), 008 (check name, cause key: the GVR or reason), 011 (parent route ref or `orphan`), 012 (kind, subject, key); none for 001, 005, 006, 009, 010.
- `since`: a finding returned with `Since == nil` gets the first-seen time of its `id` from its check's Store (key `"\x00since/" + id`); ids not returned and not `Retain`ed are pruned. Status-derived findings keep `lastTransitionTime`.

- [ ] **Step 1: Failing tests** `TestIDFormat` (`^f1-[a-z2-7]{26}$`), `TestIDStableAcrossRecomputesAndRestart`, `TestRewordingKeepsID` (message, detail, severity, since changed), `TestDiscriminatorSeparates` (two KGWUI-012 checks and two KGWUI-002 conditions on one resource), `TestIDUniqueWithinInstall` (Phase 2 fixture world: no duplicates), `TestUIDerivedSinceSurvivesRecomputeAndResetsOnRestart`, `TestStatusFindingKeepsLastTransitionTime`.
- [ ] **Step 2: Implement, run, commit** `feat(health): stable finding ids and first-seen since (D-028)`.

### Task 23: Groups and rank

**Files:** Create `pkg/diagnose/group.go`, `pkg/diagnose/rank.go`, `pkg/diagnose/group_test.go`; Modify `pkg/engine/engine.go:79-91` (`InstallModel.Diagnosis`).

**Interfaces:**
```go
type Class int // 1 data plane, 2 processes, 3 traffic configuration, 4 consistency and hygiene
func ClassOf(f health.Finding) Class // §5.3; 003 is 3 after grace (warning), else 4
type Family interface {
	Name() string
	// Claim returns the cause of f in this family, or ok false.
	Claim(f health.Finding, in Input) (cause *objects.Ref, causeKey []string, ok bool)
	Affected(cause *objects.Ref, causeKey []string, members []health.Finding, in Input) []objects.Ref
}
type Input struct { Install install.Resolved; Owned *install.Owned; Snapshot *cache.Snapshot; Served []kinds.Served; Views objects.Decoder }
type Group struct { ID, Family string; Cause *objects.Ref; Members []int; Severity health.Severity; Class Class; Affected []objects.Ref; Since *time.Time }
type Diagnosis struct { Groups []Group /* rank order */; Rank []int /* finding indexes in rank order */ }
func Build(rep *health.Report, in Input, families []Family) *Diagnosis // sets rep.Findings[i].Group
func Families() []Family // 3.1: the catch-all "Other" only; 3.2–3.4 insert theirs in §5.2 table order
```
- Families are tried top-down; the first claim wins; unclaimed findings form "Other" groups on their own resource.
- Rank: worst member severity; class; `len(Affected)` descending; oldest member `since` first (nil last); group id. Finding rank: its group's rank, then severity, `since`, id. Info-only groups are never in "Needs attention"; KGWUI-008 groups are not ranked (they sort last and are excluded from diagnostics groups).

- [ ] **Step 1: Failing tests** `TestRankOrderEveryClass` (a fixture with one finding per class and ties at each key), `TestGroupMembershipExclusive`, `TestOtherGroupPerResource`, `TestSignalUnavailableNotRanked`.
- [ ] **Step 2: Implement, run, commit** `feat(diagnose): finding groups and rank`.

### Task 24: Findings endpoints

**Files:** Create `pkg/api/findings.go`, `pkg/api/diag_types.go`, `pkg/api/findings_test.go`, `pkg/diagnose/index.go`; Modify `pkg/api/handlers.go` (routes; `/health` findings gain `id`, `group`, `evidence`), `pkg/api/types.go` (`addedFilters["findings"]`), `pkg/api/golden_test.go`, `pkg/api/additive_test.go`; Create goldens `findings.json`, `findings-restricted.json`, `finding-detail-other.json`, `finding-detail-restricted.json`. **⚠ guardrail** (≥ 10 files).

**Interfaces:**
- `GET /api/v1/installs/{id}/findings` → `Page[FindingRow]`; filters `severity` (repeatable), `code` (repeatable), `kind` (`group/Kind`), `namespace`, `group` (group id), `q`; `sort=rank|since|code` (default `rank`; `-` prefix for descending except `rank`); Phase 2 keyset cursors (`pkg/explore/cursor.go`); unknown filter values, a bad sort or a foreign cursor → 400.
- `GET /api/v1/installs/{id}/findings/{findingId}` → `FindingDetail{install, finding, evidence?, suggestedFix?, nextSteps?, affected: Page[LinkedRef], collectedAt}`; `?affectedCursor=`; 50 affected per page; unknown id → 404.
- DTOs as §13.3 (`FindingRow`, `FindingDetail`, `NextStep`, `SuggestedFix`, `GroupRow`), wire names camelCase, times UTC.
- `diagnose.Index`: per install, precomputed `rank`, `since` and `code` orders over finding indexes, so a page is a keyset slice.
- Authorizer (§14): rule 11 (`id`, `group` on restricted rows), 12 (`evidenceRestricted`), 17 (`q` matches a restricted finding's resource identity only; `total` counts restricted findings), 18 (ranks over every finding).

- [ ] **Step 1: Failing tests** `TestFindingsPaging` (forward and back through 3,309 KGWUI-012 at Large give the same sets), `TestFindingsFilters` (each filter, combined), `TestFindingsFiltersRevealNothingHidden` (deny-some: `q` on a hidden message matches nothing, `total` stable under every filter), `TestFindingDetailAffectedPaging`, `TestHealthFindingsGainIDAndGroup`, goldens, and the additive test (`/health`, `/overview` goldens differ only by `id` and `group`).
- [ ] **Step 2: Implement, run** `go test ./pkg/api ./pkg/diagnose` → PASS; **commit** `feat(api): server-paged findings and finding detail`.

### Task 25: Scale tiers for Phase 3

**Files:** Modify `internal/testutil/scale/scale.go` (fields, worst case), `internal/testutil/scale/live.go`, `internal/testutil/fixtures.go` (typed proxy Pods, Events, Services, EndpointSlices, CRD metadata), `internal/testutil/scale/scale_test.go`.

**Interfaces:** `Tier` gains `Services, ReferencedServices, EndpointSlices, ProxyPodsPerGateway, WarningEvents, GatewayAPICRDs int` and `Worst bool`; values: Typical 600/300/700/2/50/10, Large 5,000/2,000/6,000/2/1,000/10, Extreme 25,000/10,000/30,000/3/10,000/10. `Tier.Worst()` sets every Gateway rejecting (metrics capture per Gateway), 5 % of referenced Services with no ready endpoint, every proxy Pod restarted once. Functions `ScaleProxyPods(t, seed)`, `ScaleEvents(t, seed)`, `ScaleServices(t, seed)`, `ScaleEndpointSlices(t, seed)`, `ScaleCRDs(t)`, `ScaleMetrics(t, seed) []byte` (in `fixtures.go`, which is allowlisted).

- [ ] **Step 1: Extend `TestScaleWorldMatchesTier`** with the counts above, determinism per seed, and the worst-case shares.
- [ ] **Step 2: Implement, run, commit** `test(scale): Phase 3 tier shapes and the worst case`.

### Task 26: SPA findings page on server paging, and finding detail

**Files:** Create `web/packages/shell/src/api/diagTypes.ts`, `src/api/diagHooks.ts`, `src/api/diagHooks.test.tsx`, `src/pages/findings/FindingsPage.tsx`, `src/pages/findings/FindingPage.tsx`, `src/pages/findings/findings.test.tsx`; Modify `src/pages/FindingsPage.tsx` (removed; re-export), `src/shell/extension.ts` (`CorePages.FindingPage?`), `src/shell/Shell.tsx` (route `findings/:findingId`), `src/pages/install/FindingRow.tsx` (links to the finding page), `src/api/types.golden.test.ts`, `src/index.ts`. **⚠ guardrail** (11 files).

**Interfaces:** `useFindings(id: string, query: URLSearchParams)`, `useFindingDetail(id: string, findingId: string, affectedCursor?: string)` (SWR, `POLL_MS`, previous rows kept while a new query loads, marked busy); `supportsFilter("findings", …)` gates filters on `/meta`.

- [ ] **Step 1: Failing tests** `"filters, sort and cursor round-trip through the URL"`, `"a 400 on a cursor drops it and loads page one"`, `"restricted rows show code, severity and the restricted label"`, `"finding detail pages the affected objects"`, `"stale data turns every state unknown"`, `"skeleton rows have the final row height"`.
- [ ] **Step 2: Implement** with `ObjectTable`; hover, active, focus-visible on rows and filters in both themes. **Commit** `feat(shell): findings page on server paging; finding detail page`.

### Task 27: `ObjectHeader`

**Files:** Create `web/packages/shell/src/shell/ObjectHeader.tsx`, `src/shell/ObjectHeader.test.tsx`; Modify `src/pages/explore/detailParts.tsx`, `GatewayPage.tsx`, `ListenerSetPage.tsx`, `RoutePage.tsx`, `src/pages/ResourcePage.tsx`, `src/index.ts`. **⚠ guardrail** (8 files).

**Interfaces:** `export function ObjectHeader(props: { kind: { group: string; kind: string; label: string; icon?: ReactNode }; name: string; namespace?: string; state?: HealthState; collectedAt?: string; actions?: ReactNode })` — icon and kind label, the name in the page's largest type, the namespace muted, state (icon, colour, text) and data age; a fixed height from the first render (state and age reserve their space while loading).

- [ ] **Step 1: Failing tests** `"the header has the same height loading and loaded"` (computed style `height` equal), `"the name is the largest type on the page"`, `"every core detail page renders one ObjectHeader"`, `"DocumentTitle still names the object"`.
- [ ] **Step 2: Implement, run, commit** `feat(shell): shared object header on detail pages (D-035)`.

### Task 28: Fluid layout and two columns

**Files:** Create `web/packages/shell/src/shell/layout.ts`; Modify `src/explore/ListFrame.tsx:16-18`, `src/pages/install/InstallPage.tsx:21`, `src/pages/ClusterLanding.tsx:22`, `src/pages/findings/FindingsPage.tsx`, `src/pages/ResourcePage.tsx:19`, `src/pages/explore/detailParts.tsx:34`, `src/shell/contributions.tsx`. **⚠ guardrail** (8 files).

**Interfaces:** `export const layout = { maxWidth: 2400, twoColumnFrom: 1600, proseMax: "80ch" }`; `export function DetailColumns(props: { main: ReactNode; side: ReactNode })` — one column below 1600 px with the side stacked after the main in DOM order; two from 1600 px. Side holds conditions, findings, related objects and contributed side sections. Type sizes do not scale with the viewport.

- [ ] **Step 1: Failing tests** (jsdom with `matchMedia` stubs) `"side column stacks under main below 1600"`, `"prose blocks cap at 80ch"`, `"no 1400px cap remains"` (grep in the test over `src/**/*.tsx`).
- [ ] **Step 2: Implement, run, commit** `feat(shell): fluid layout up to 2400 px and two-column detail pages`.

### Task 29: Path-based breadcrumbs (`via`)

**Files:** Create `web/packages/shell/src/shell/breadcrumbs.ts`, `breadcrumbs.test.ts`, `src/shell/Breadcrumbs.tsx`, `Breadcrumbs.test.tsx`; Modify `src/explore/objectPath.ts` (`linkTarget` appends `via`), `src/explore/LinkedName.tsx`, `src/shell/ObjectHeader.tsx` (trail above the header).

**Interfaces:**
```ts
export type Crumb = { kind: "list"; path: string; search: string } | { kind: "object"; path: string } // path: group/kind/namespace/name
export function parseVia(search: string): Crumb[]          // drops malformed entries, keeps their text for display
export function appendVia(trail: Crumb[], current: Crumb): Crumb[] // cut back to an existing entry; else append
export function collapse(trail: Crumb[], max = 6): { hidden: Crumb[]; shown: Crumb[] }
export function viaParam(trail: Crumb[]): string           // the `via` query value
```
- Only the first entry may be a list (with its filters). Every crumb renders only what its own path says; a crumb for a hidden object shows the restricted label (lookup through the same `LinkedRef` link rules); an unresolvable entry is plain text; no `via` shows `<install> › <kind list> › <object>`. "Back to install" links in error states stay.

- [ ] **Step 1: Failing tests** `"append"`, `"cut back on a cycle"` (route ↔ Gateway three times keeps two entries), `"collapse past 6 into an ellipsis menu"`, `"hierarchy without via"`, `"malformed or unresolvable entry is text and the page loads"`, `"a via entry for another install is dropped"`, `"a 4 KiB via is cut to the last 6 valid entries"`, `"a crumb for a hidden object shows the restricted label"`. The ellipsis menu has hover, active, focus-visible and keyboard support.
- [ ] **Step 2: Implement, run, commit** `feat(shell): path-based breadcrumbs carried in the URL (D-035)`.

### Task 30: Playwright for shell and navigation; ui-loop evidence

**Files:** Create `web/e2e/navigation.spec.ts`, `docs/phases/phase-3/phase-3.1-evidence.md`.

- [ ] **Step 1: Write the spec** (§21): Gateway → attached route → child route, each crumb back to the earlier page; reload and a copied URL keep the trail; a filtered list → object → list crumb restores filters; the object header on each core detail page with no layout shift (bounding boxes equal before and after data); at 720, 1280, 1600 and 3008 px: `document.scrollingElement.scrollWidth <= innerWidth`, two columns from 1600, content width up to 2400.
- [ ] **Step 2: Run** `make e2e KGW_VERSION=v2.4.7 GWAPI_VERSION=v1.6.1` → PASS.
- [ ] **Step 3: Run `/ui-loop`** on the findings pages, a detail page and the breadcrumbs: both themes, the four widths, loading and stale states. Store recordings outside the repo; list paths in the evidence file.
- [ ] **Step 4: Commit** `test(e2e): navigation, header and layout`.

### Task 31: Wording pass for 3.1

**Files:** Create `web/packages/shell/src/copy/navigation.ts`, `docs/phases/phase-3/phase-3.1-wording.md`; Modify the components of Tasks 26–29, `web/packages/shell/src/copy/explorer.ts` (moved strings).

- [ ] **Step 1: List every 3.1 string** with its draft: findings page title, filter and sort labels, empty and error states, finding detail labels, "Affected objects", the restricted crumb, the ellipsis menu label, header labels and the data-age line.
- [ ] **Step 2: Per string,** write five accurate options; a separate sub-agent picks the least machine-sounding accurate one; record the choices.
- [ ] **Step 3: Apply;** update quoting tests. **Run** web gates → PASS. **Commit** `copy: wording pass for findings and navigation`.

### Task 32: 3.1 budgets

**Files:** Modify `pkg/engine/budget_test.go`, `pkg/api/budget_test.go` (findings mixes), `pkg/engine/bench_test.go` (`BenchmarkAnalyzerStage`), `.github/workflows/bench.yaml` (paths `pkg/collect/**`, `pkg/diagnose/**`); Create `docs/phases/phase-3/bench/3.1.txt`, `3.1-ci.txt`.

- [ ] **Step 1: Extend:** `TestBudgetFindingsRequests` (mixes: none, `severity=error`, `code=KGWUI-012`, `q`, `group`, `sort=since`; restricted; 20 clients): p95 < 50 ms, p99 < 100 ms, p95 < 100 ms concurrent. `BenchmarkAnalyzerStage/{warm,memoized}` reported (gated from 3.2). Warm recompute ≤ 150 ms (consistent and worst case); stream CPU ≤ 15 %; spec-edit reported; retained heap ≤ 32 MiB; dense median ≤ 432 MiB.
- [ ] **Step 2: Run** `make bench | tee docs/phases/phase-3/bench/3.1.txt` → PASS, or **STOP with the numbers**. CI run saved as `3.1-ci.txt`.
- [ ] **Step 3: Commit** `test: 3.1 budgets with memoization and paged findings`.

### Task 33: Close sub-phase 3.1

**Files:** Create `phase-3.1-complete.md`, `reviews/3.1-codex.md`; Modify `README.md` (Upgrade notes: the check contract; finding `id` and UI-derived `since` that resets on restart; the findings page pages on the server), `docs/phase-status.md`, `docs/roadmap.md`, `docs/phases/phase-3/design.md` (only where code deviates, e.g. identity in `pkg/health`, Task 22).

- [ ] **Step 1: Standard gates.**
- [ ] **Step 2: Codex review** (template of Task 11) against §5, §13.1–§13.3 (findings), §14 rules 11, 12, 17, 18, §15, §16, §17.6 and D-027, D-028, D-032, D-035. Focus: memo never serves output past a deadline or staleness; Store isolation; declared-input guard coverage; id stability and uniqueness; cursor correctness; restricted filters; breadcrumbs trust nothing from `via`; layout at four widths.
- [ ] **Step 3: Triage; fix; rerun. Step 4: Write the record** (benchmark table: baseline `544dcd5`, 3.0, 3.1, spike) and update status and roadmap. **Step 5: Commit** `docs: close sub-phase 3.1`.

---

# Sub-phase 3.2 — Data plane, processes and the overview

### Task 34: 3.2 brief

**Files:** Create `phase-3.2-brief.md`; Modify `docs/phase-status.md`.

- [ ] **Step 1:** Record the sub-phase; write the brief with an expected-behaviour table from §6, §8.1–§8.5, §8.8, §9, §13, §14 rules 13–16, §17.1–§17.4, §18.1 rows for events, CRDs and proxy stats. **Commit** `docs: brief for sub-phase 3.2`.

### Task 35: Watches: proxy Pods, Events and CRD metadata

**Files:** Create `pkg/cache/events.go`, `pkg/cache/crds.go`, `pkg/cache/events_test.go`; Modify `pkg/cache/cache.go:51-90` (Snapshot, Config), `pkg/health/checks.go:49-70` (`cacheCheck` scope), `pkg/app/app.go` (selectors, toggles), `cmd/kgateway-ui/main.go` (flags), `internal/boundary/imports_test.go` (two entries), `pkg/api/api_test.go`. **⚠ guardrail** (9 files).

**Interfaces:**
- `Config.ProxyPodSelectors []labels.Selector`; `Config.Events bool`; `Config.CRDs bool`; `Config.Metadata metadata.Interface`.
- `Snapshot.ProxyPods []*corev1.Pod`; `Snapshot.Events []EventFact`; `Snapshot.CRDs []CRDMeta`; Access keys `ProxyPodsGVR` (`pods`, selector-scoped name `pods#proxy`), `EventsGVR`, `CRDsGVR`.
- `type EventFact struct { UID, PodUID types.UID; Namespace, Reason string; Count int32; First, Last time.Time; SeriesCount int32; SeriesLast time.Time }` — kept reasons `Unhealthy`, `BackOff`, `Failed`, `FailedScheduling`, `FailedMount`. No message field.
- `type EventDeltas` (in `events.go`): per Event UID the last count seen and a bounded list of `(at, delta)` observations within 15 min; an Event first seen with a count above 1 records that count as the baseline, uncounted ("unknown"); a series uses `SeriesCount`, never `Count` and `SeriesCount` together (review finding 7). `Snapshot.EventDeltas map[types.UID][]Observation` keyed by Pod UID, plus `Snapshot.EventHistoryUnknown map[types.UID]bool`.
- `type CRDMeta struct { Name, Group, BundleVersion, Channel string }` — groups `gateway.networking.k8s.io` and `gateway.networking.x-k8s.io` only, filtered client-side.
- The Events informer uses field selector `type=Warning,involvedObject.kind=Pod` and a transform that keeps an Event only when its `involvedObject.uid` is a currently known controller or proxy Pod UID (a set the Pod informers maintain); at most 10,000 kept, oldest dropped, and `Snapshot.EventsCapped bool` set.
- **Probe type (ruling A2):** Event messages stay unread. The probe type comes from the Pod spec in `pkg/process/kube.go` (Task 37): exactly one probe type defined on the container → that type; otherwise "a probe".
- **Required vs optional access (review finding 3):** `cacheCheck` (`cluster access`, required) iterates only the core GVRs: served kinds, `NamespacesGVR`, `PodsGVR` (controller selector) and `DeploymentsGVR`. `ProxyPodsGVR`, `EventsGVR`, `CRDsGVR` (and in 3.4 Services and EndpointSlices) are optional: a denial or failure reaches only the dependent checks' `NotAvailable` (Task 36), never KGWUI-008.
- `app.Options.Diagnostics struct { NoEvents, NoCRDs, NoProxyStats, NoEndpoints bool; ControllerLogs bool; ControllerLogsNamespaces []string }` (zero value keeps the defaults); flags `--diagnostics-events`, `--diagnostics-crds`, `--diagnostics-proxy-stats`, `--diagnostics-endpoints` (default true), `--diagnostics-controller-logs` (default false).

- [ ] **Step 1: Failing tests** `TestEventsTransformKeepsOnlyInstallPods`, `TestEventBeforePodIsKeptOnNextUpdate` (Review Focus 5: Event before Pod dropped; the Event's next update after the Pod arrives is kept as a baseline, and later increments count), `TestFirstSeenCountIsUnknown` (an Event first seen with count 50 contributes 0 and marks history unknown), `TestSeriesCountedOnce`, `TestEventTransformKeepsNoMessage` (a message holding `secret-value` is absent from the cache objects), `TestEventsCap`, `TestCRDMetadataKeepsGatewayGroups`, `TestToggleOffStartsNoWatch` (fake client records no `events` watch), `TestDeniedWatchReportsAccess`, and through the API `TestDeniedOptionalWatchNeverUnknown` (Events and CRDs denied: no KGWUI-008, `cluster access` OK, install state unchanged; after recovery the dependent checks run again).
- [ ] **Step 2: Implement, run, commit** `feat(cache): proxy Pods, Warning Events and Gateway API CRD metadata`.

### Task 36: Availability and core capabilities

**Files:** Create `pkg/health/avail.go`, `pkg/health/avail_test.go`; Modify `pkg/health/finding.go:90-102` (CheckResult), `pkg/api/capability.go` (core source), `pkg/app/app.go`.

**Interfaces:**
- `type Availability string` with `AvailOff = "off"`, `AvailDenied = "denied"`, `AvailFailing = "failing"`, `AvailNotOnVersion = "not-on-version"`, `AvailNotObserved = "not-observed"`.
- `CheckResult.Unavailable Availability \`json:"unavailable,omitempty"\``.
- `func NotAvailable(name string, why Availability, detail string, at time.Time) CheckResult` — `OK: false`, `Required: false`, no finding.
- `func CoreCapabilities(snap *cache.Snapshot, d app.Diagnostics, collected collect.Snapshot) []api.Capability` (in `pkg/app`): `processes` (proxy Pods watch), `events`, `crds`, then `proxyStats`, `controllerLogs`, `endpoints` as later tasks add them; `off` from the toggle, `denied` from a 403 in Access, `failing` otherwise.
- `RollUp` ignores results with `Unavailable` set; "What was checked" shows them as not available with the reason. With Task 35's `cacheCheck` scope, an optional watch's failure surfaces only here.

- [ ] **Step 1: Failing tests** `TestNotAvailableNeverUnknown` (through `/health`: state stays healthy), `TestRequiredChecksKeepTheirFlag`, `TestMetaCapabilityStatesFromToggles` (off, denied, failing).
- [ ] **Step 2: Implement, run, commit** `feat(health): not-available checks with a reason; core capability states`.

### Task 37: `pkg/process` and the Kubernetes source

**Files:** Create `pkg/process/process.go`, `pkg/process/kube.go`, `pkg/process/kube_test.go`; Modify `pkg/install/install.go` (`ProxyPodDetector`), `pkg/install/upstream.go`, `pkg/extension/extension.go` (`WithProcessSource`), `pkg/engine/engine.go` (process data per install, hash), `pkg/health/finding.go` (`Input.Process`), `internal/boundary/imports_test.go` (`pkg/process/kube.go`: "process facts from Pods, Deployments and Events"). **⚠ guardrail** (9 files).

**Interfaces:**
- `process.Source`, `Set`, `Process` exactly as §9.1, plus `type Rollout struct { Progressing bool; Reason string; SettledAt *time.Time }` (no template hash; review finding 8), `type Termination struct { Reason string; ExitCode int32; At time.Time }`, `type ProbeFailures struct { Type string; Count int; Last time.Time; HistoryUnknown bool }` (observed deltas in 15 min from `Snapshot.EventDeltas`; `Type` from the Pod spec: the one defined probe type, else `""` shown as "a probe").
- `type ProxyPodDetector interface { ProxyPodSelector() labels.Selector }` (optional on a Detector); upstream: `kgateway=kube-gateway,app.kubernetes.io/component=proxy`.
- `process.Kube{}` reads controller Pods, proxy Pods (a Pod belongs to a proxy Deployment when the Deployment's `spec.selector` matches it in the same namespace), Deployments and `Snapshot.Events`. `Version` from the image tag of the container whose image's last path segment is `envoy-wrapper` (proxies) or the controller container; termination message never read.
- `health.Input.Process *ProcessData` with `type ProcessData struct { Controller process.Set; Proxies map[objects.Ref]process.Set; Print uint64 }`.

- [ ] **Step 1: Failing tests** `TestProxyPodsBelongToOneInstall` (Review Focus 1: two installs' proxies in one namespace), `TestLongGatewayNameProxyPods` (Review Focus 3: two 70-character Gateway names sharing a 63-character prefix map to their own Deployments and Pods), `TestSelfManagedIsNotObserved`, `TestTerminationMessageNeverRead` (a Pod with a message containing `secret-value`: absent from the model and every API body), `TestNilFieldsMeanNotObserved`.
- [ ] **Step 2: Implement, run** `go test ./pkg/process ./internal/boundary` → PASS; **commit** `feat(process): process model and the Kubernetes source (D-029)`.

### Task 38: KGWUI-019, 020, 021 and the 005/006 detail

**Files:** Create `pkg/health/process.go`, `pkg/health/process_test.go`, `pkg/health/evidence.go`; Modify `pkg/health/finding.go` (codes, `Finding.Facts`), `pkg/health/checks.go:118-160,527-580` (005/006 detail), `pkg/api/process_test.go` (boundary). **⚠ guardrail** (6 files).

**Interfaces:**
- Codes `CodeProcessRestarting = "KGWUI-019"` (slug `process-restarting`), `CodeProbeFailing = "KGWUI-020"` (`probe-failing`), `CodeRolloutStuck = "KGWUI-021"` (`rollout-stuck`).
- `type Evidence interface{ EvidenceKind() string }`; `Finding.Facts Evidence \`json:"evidence,omitempty"\`` (the existing `Finding.Evidence []objects.Ref` stays the dependency set of §14 rule 12); `ProcessEvidence{Restarts, Termination, Waiting, Probe}` kind `process`.
- 019: restart count grew while watched (previous count per process ID in the Store) or last termination finished within 15 min; one per subject (controller Deployment, or the Gateway for proxies); exit code 2 on the controller adds the panic note and `kubectl logs --previous`; `WakeAt(lastRestart + 15 min)`.
- 020: ≥ 3 observed probe failures (deltas of `Unhealthy` Events, Task 35) on one Pod in 15 min; uncounted history adds the note "earlier failures not counted"; `WakeAt(oldest observation + 15 min)`.
- 021: `Progressing=False/ProgressDeadlineExceeded` only. Waiting reasons of the Deployment's Pods (`ImagePullBackOff`, `ErrImagePull`, `InvalidImageName`, `CreateContainerConfigError`, `CrashLoopBackOff`) are evidence. No template-hash comparison (review finding 8). No ready replica → 005/006 instead.
- 005/006 detail: "0/2 ready: CrashLoopBackOff, last exit OOMKilled" form.

- [ ] **Step 1: Failing tests** (boundary: Pods, Deployments, Events in → `/health` JSON out): restart in window then expiry at 15 min + 1 s; OOMKilled; exit code 2; CrashLoopBackOff detail; probe failures 2 (none) and 3 observed; an Event first seen with count 50 (none); aggregated Event counts as a window (`TestAggregatedEventCountIsAWindowNotATotal`, Review Focus 5); the probe type from a one-probe container and "a probe" from a two-probe one; `ProgressDeadlineExceeded`; an image pull failure beside a ready old Pod is evidence only, no 021 without `ProgressDeadlineExceeded`; no ready replica gives 005/006 and no 021; events off → 020 not available (`off`), install not unknown.
- [ ] **Step 2: Implement, run, commit** `feat(health): process restarts, probe failures and stuck rollouts`.

### Task 39: KGWUI-022 version skew

**Files:** Create `pkg/health/skew.go`, `pkg/health/skew_test.go`, `pkg/computed/rulesets/gatewayapi.go`; Modify `pkg/api/explain.go` (ListenerSet hint with the channel), `pkg/health/finding.go` (code).

**Interfaces:**
- `CodeVersionSkew = "KGWUI-022"` (`version-skew`, warning); Key `proxy`, `controller` or the CRD name.
- `func ExpectedGatewayAPI(kgatewayMinor string) (bundle string, ok bool)` — `2.3 → v1.5`, `2.4 → v1.6`, `2.5 → v1.7` (the 2.5 row re-read from the GA tag in Task 84); pluggable through the rule-set selection.
- Proxy vs controller: major.minor differ → finding; patch differ → evidence only; suppressed while the controller rolls out and 10 min after (`SettledAt` in the Store, `WakeAt(settled + 10 min)`); unparseable tag → "version unknown", no finding; a proxy Deployment whose image is pinned in GatewayParameters is named as the likely cause. Controller replicas: two running versions after a settled rollout. CRDs: an installed bundle minor that differs from the expected; differing bundle versions or channels across the Gateway API CRDs.
- `VersionSkewEvidence` kind `version-skew`.
- ListenerSet hint: a standard-channel Gateway CRD next to a ListenerSet CRD replaces "may" with the definite wording (§9.4).

- [ ] **Step 1: Failing tests** minor skew; patch skew evidence only; digest tag; rollout suppression at 9:59 and 10:01; CRD bundle and channel mismatch; `TestSkewUnparseableAndPrereleaseTags` (Review Focus 4: `v2.5.0-rc.1` vs `v2.5.0` no finding, `v2.4.7-acme.1` parses as 2.4, `latest` and `@sha256:` unknown); CRDs off → not available.
- [ ] **Step 2: Implement, run, commit** `feat(health): version skew between controller, proxies and Gateway API CRDs`.

### Task 40: KGWUI-001 re-specified

**Files:** Create `pkg/health/xds.go`, `pkg/health/xds_test.go`; Modify `pkg/health/checks.go:603-636` (remove the old check body), `pkg/health/evidence.go`, `pkg/api/xds_test.go` (boundary).

**Interfaces:**
- Check name stays `xDS rejects`, `Required: true`, inputs `Owned`, `Metrics`, `Process`, `Clock`, `Collected: ["proxy-stats/stats"]` (Task 42 fills it).
- `XDSRejectsEvidence` (kind `xds-rejects`) per §13.3: `types[{typeUrl, streams, reportedBy, firstSeen, totalSinceFirstSeen}]`, `proxies{ready, desired}`, `statusSaysHealthy`, `recentlyChanged` (Task 44), `proxyStats?` (Task 42), `logEvents?` (Task 67).
- The active state is the controller's `rejects_active` gauge only. With controller metrics unavailable the check is unavailable as today (KGWUI-008, required), whatever proxy stats show (review finding 4).
- Message: Gateway status current and `Programmed=True` → the status-says-healthy draft; else "Proxies reject this Gateway's config".
- **Restart hold (algorithm fixed by §8.3):** the Store keeps the previous controller Pod UIDs and restart counts, and the previous set of rejecting Gateways. A Gateway whose rejects disappear while some controller Pod has a new UID or a higher restart count is held until `now + 60 s`: no 001 finding for it, the check result is `Pending` with an error naming the restart, `WakeAt(holdEnd)`, and `Retain(id)` keeps its first-seen time. A reject that returns during the hold continues with that time. The hold clears at its end or when the reject returns.
- `Finding.Evidence` (deps): controller Pods in `reportedBy`, proxy Pods in `proxyStats`.

- [ ] **Step 1: Failing tests** per-type evidence summed over two replicas (Task 20 captures); the status-says-healthy message on `Programmed=True` and its absence when status is stale; `TestRestartHoldKeepsInstallUnknown` (new UID: unknown at +59 s, healthy at +61 s; higher restart count; reject back at +30 s keeps `since`); a rejecting Gateway on the second install does not hold the first.
- [ ] **Step 2: Implement, run, commit** `feat(health): xDS rejects per Gateway with evidence, since and the restart hold (D-030)`.

### Task 41: Proxy Envoy stats collector

**Files:** Create `pkg/proxystats/source.go`, `pkg/proxystats/collector.go`, `pkg/proxystats/parse.go`, `pkg/proxystats/stats.go`, `pkg/proxystats/kube.go`, `pkg/proxystats/proxystats_test.go`, `pkg/computed/envoynames.go`; Modify `pkg/extension/extension.go` (`WithProxyStatsSource`), `pkg/app/app.go` (register as a core collector), `internal/boundary/imports_test.go` (`pkg/proxystats/kube.go`), `examples/downstream/acme.go`, `examples/downstream/main_test.go`. **⚠ guardrail** (12 files, likely > 400 lines).

**Interfaces:**
- **Replaceable source (review finding 12):**
```go
// Source finds a Gateway's proxies, fetches their stats and interprets them.
type Source interface {
	Targets(in install.Resolved, gw objects.Ref, snap *cache.Snapshot) ([]Target, bool) // false: not observed
	Fetch(ctx context.Context, t Target) ([]byte, error)                               // bounded by the caller
	Interpret(t Target, body []byte) (PodStats, error)
}
type Target struct { ID, Name string; Gateway objects.Ref; Addr string }
```
  `proxystats.Kube` (in `kube.go`) is the default: proxy Pods from `Snapshot.ProxyPods`, Pod IP or `pods/proxy` transport, port 9091, `StatsFilter`, Envoy text format. `extension.WithProxyStatsSource(s proxystats.Source) Option` replaces it. The collector owns the interval, bounds, concurrency and publishing for any Source.
- `const StatsFilter` — the one regex of the §8.8 table (`control_plane.connected_state`, `cluster_manager.warming_clusters`, `listener_manager.total_listeners_warming`, `listener_manager.lds.update_(rejected|success)`, `cluster_manager.cds.update_(rejected|success)`, `http\..*\.rds\..*\.update_rejected`, `cluster\..*\.update_rejected`, `server.state`, `server.uptime`).
- Request `GET http://<podIP>:9091/stats?usedonly&filter=<StatsFilter>` in-cluster, `pods/proxy` with the user's rights locally; every metrics interval (10 s); body ≤ 1 MiB (over → `failing`); timeout `min(interval/2, 5 s)`; at most 16 Pods at once per install.
- `type PodStats struct { UID, Name string; Gateway objects.Ref; Connected *bool; WarmingListeners, WarmingClusters *int64; LDS, CDS Pair; RDS, Clusters map[string]int64 /* update_rejected per Envoy name */; ServerState string; Uptime int64; Err string }`; `type Pair struct{ Rejected, Success int64 }`. Raw counters only; Task 42 turns them into "rejects observed since the last scrape" per Pod UID. No field claims an active reject (review finding 4).
- Publishes key `proxy-stats/stats` with `map[installID][]PodStats` and a content fingerprint.
- `computed.EnvoyNamer interface { Object(envoyName string) (objects.Ref, bool) }` on `computed.Selection`; nil means names are shown as Envoy names only. Ship nil in `kgw23`/`kgw24`; Task 53 records the naming observed on both tags, and a verified namer ships only with per-version fixtures.

- [ ] **Step 1: Failing tests** parse the Task 20 captures; `TestDownstreamProxyStatsSource` in `examples/downstream/main_test.go` (an `acme` Source with its own targets and a JSON stats format feeds 026 through `/health`); a refused listener, a timeout and a 1 MiB + 1 byte body → `failing`; a self-managed Gateway → `not-observed`; at most 16 concurrent scrapes (blocking stub); `TestProxyStatsPerInstall` (Review Focus 1); a stat name outside the filter is ignored.
- [ ] **Step 2: Implement, run, commit** `feat(proxystats): replaceable proxy stats source; Envoy stats from each proxy's stats listener (D-037)`.

### Task 42: KGWUI-025, 026 and per-Pod 001 evidence

**Files:** Create `pkg/health/proxystats.go`, `pkg/health/proxystats_test.go`; Modify `pkg/health/xds.go`, `pkg/health/evidence.go`, `pkg/health/finding.go` (codes).

**Interfaces:**
- `CodeProxyConfigWarming = "KGWUI-025"` (`proxy-config-warming`), `CodeProxyDisconnected = "KGWUI-026"` (`proxy-disconnected`), warnings, one per Gateway, Pods as evidence; mature after 60 s of the condition on a Ready proxy (start kept in the Store, `WakeAt(start + 60 s)`).
- `ProxyStatsEvidence` kind `proxy-stats`.
- 001 evidence per Pod (as built: `rejecting` and `rejectingSince` per type, never a count, D-048): `rejectsSinceLastScrape` per type, the delta of `update_rejected` against the previous value per Pod UID in the Store (a new UID or a lower value is a reset, read as 0); RDS and cluster names with a positive delta listed. Proxy stats never raise 001 and never mark it active; a disagreement with the controller gauge is evidence only (review finding 4).

- [ ] **Step 1: Failing tests** 025 and 026 at 59 s (none) and 61 s; a not-Ready proxy (none); rejects observed since the last scrape per Pod (`(3,8)` → `(4,9)` reads as 1 observed reject, never as "rejecting now"); RDS and cluster names; `TestNoRejectFindingFromProxyStatsAlone` (controller metrics unavailable, proxy stats show rejects: no 001, the check unavailable as today); `TestProxyStatsResetIsNotAReject` (Review Focus 2: a new UID with counters at zero is neither a reject nor a recovery).
- [ ] **Step 2: Implement, run, commit** `feat(health): proxy config warming, lost controller connection, observed rejects per Pod as evidence`.

### Task 43: `gateway-rejecting`, the data-plane and process groups

**Files:** Modify `pkg/explore/rows.go` (flag and state), `pkg/explore/query.go` (`state=gateway-rejecting`), `pkg/diagnose/group.go` (families "Data plane rejects" and "Process"); Test `pkg/explore/explore_test.go`, `pkg/api/lists_test.go`.

**Interfaces:** row flag `gateway-rejecting` on a route, ListenerSet or policy whose current status is `True` for a Gateway with an active 001; its state is at least `warning`, an own error stays `error`; no finding of its own. Data plane family: cause the Gateway; affected the objects with a current `Accepted=True` or `Programmed=True` entry for it. Process family: cause the controller Deployment, or the Gateway for proxies; members 005, 006, 019–021.

- [ ] **Step 1: Failing tests** `TestRowNeverHealthyWhileGatewayRejects`, the list filter, group affected lists, isolation (a route on another install's Gateway is not flagged).
- [ ] **Step 2: Implement, run, commit** `feat(explore): rows of a rejecting Gateway are never healthy`.

### Task 44: Next steps and recently changed objects

**Files:** Create `pkg/diagnose/nextsteps.go`, `pkg/diagnose/nextsteps_test.go`; Modify `pkg/extension/extension.go` (`WithCommands`), `pkg/health/xds.go` (`recentlyChanged`).

**Interfaces:**
```go
type Commands interface { // "" hides a step; a platform without kubectl overrides it
	ControllerLog(pod objects.Ref, since time.Duration, grep ...string) string
	ProxyLog(proxy objects.Ref, since time.Duration, grep string) string
	ProxyCounters(proxy objects.Ref) string // port-forward plus curl
	PreviousLog(pod objects.Ref) string
}
type Kubectl struct{}           // the §8.5 commands
func Quote(s string) string     // POSIX single quotes when s holds anything outside [A-Za-z0-9_./:=-]
type NextStep struct { Title, Command string; Needs []string; Note string }
func Steps(g Group, findings []health.Finding, in Input, c Commands) []NextStep
```
- Data plane: the three §8.5 commands, naming the controller replica that reported the reject; `Needs` `pods/log` and `pods/portforward`; the secret-care note. Process: `PreviousLog` for 019 with exit code 2.
- `recentlyChanged`: routes, ListenerSets and policies on this Gateway whose current generation was first seen in the 10 min before the first reject, newest first, at most 10, from `Input.FirstSeen`.

- [ ] **Step 1: Failing tests** `TestCommandsQuoteValues`, `TestStepsHiddenWhenCommandEmpty`, `TestRecentlyChangedWindowAndCap` (9:59 before in, 10:01 out; 12 changes → 10).
- [ ] **Step 2: Implement, run, commit** `feat(diagnose): next-step commands and recently changed objects`.

### Task 45: Diagnostics and processes endpoints

**Files:** Create `pkg/api/diagnostics.go`, `pkg/api/processes.go`, `pkg/api/diagnostics_test.go`; Modify `pkg/api/diag_types.go`, `pkg/api/handlers.go` (incl. `visibleHealth:145`, which clears only message, detail and since today), `pkg/api/findings.go` (evidence and next steps in detail), `pkg/api/golden_test.go`; goldens `diagnostics.json`, `diagnostics-restricted.json`, `processes.json`, `processes-restricted.json`, `finding-detail-xds-rejects.json`, `…-process.json`, `…-version-skew.json`, `…-proxy-stats.json` (+ restricted). **⚠ guardrail** (≥ 12 files).

**Interfaces:**
- `GET …/diagnostics` → `Diagnostics` (§13.3): verdict (state, problems, `oldestSince`, reason), top 10 groups (`GroupRow` with first 5 affected and `affectedTotal`), `groupTotal`, process summary, data-plane summary, `checks`, `collectedAt`. Built per request (D-050).
- `GET …/processes` → `{controller: ProcessSetDTO, proxies: Page<GatewayProcesses>}`; filters `health`, `q`, `skew`, `restarts`.
- Authorizer rules 13 (restricted group rows; visible-only affected and `affectedTotal`; install-wide members), 14 (recently changed each authorized, hidden ones left out without a count), 15 (next steps name only visible objects), 16 (process facts need the Deployment or Gateway; Pod names and per-Pod facts also need the Pod).

- [ ] **Step 1: Failing tests** `TestTypedEvidenceRestrictedOnHealthAndOverview` (review finding 13: a restricted finding on `/health` and `/overview` carries no `evidence`, no suggested fix and no next steps; a visible finding with a hidden dependency gets `evidenceRestricted` and no `evidence`), `TestGroupsAndNextStepsHideHiddenObjects`, `TestDiagnosticsTopTen`, `TestProcessesFilters`, `TestProcessSetCountsOnlyWhenPodHidden`, the evidence dependency table rows for 001, 019–022, 025, 026, goldens and the additive test.
- [ ] **Step 2: Implement, run, commit** `feat(api): diagnostics and processes endpoints`.

### Task 46: Gateway detail: data plane and proxies

**Files:** Modify `pkg/api/explore_types.go:268` (`GatewayDetail.DataPlane`, `Proxies`), `pkg/api/details.go`, `pkg/ctrlmetrics/parse.go` (`kgateway_xds_snapshot_resources` per Gateway), `pkg/api/details_test.go`, goldens `gateway-detail.json`, `gateway-detail-restricted.json`.

**Interfaces:** `DataPlaneDTO` (§13.3) with `snapshot` labelled as the controller's build, never a finding; `Proxies ProcessSetDTO`. Route rows gain the `gateway-rejecting` flag on the wire.

- [ ] **Step 1: Failing tests** gateway detail with rejects, without, with stats off; restricted Pods. **Step 2: Implement, run, commit** `feat(api): Gateway data plane and proxies sections`.
- [ ] **Step 3: Interim measurement before the 3.2 pages (review, budgets row).** At Large in the worst case, with the proxy Pod, Event and CRD watches, process data, proxy stats (stub, 2 per Gateway), 001 and 019–022, 025, 026 evidence, the 3.2 groups and every check Store: run `TestBudgetWarmRecompute`, `TestBudgetHeap`, `TestBudgetDenseMemory` (median of 5), `TestBudgetAnalyzerStage` and the spec-edit stream (`-short`). Record in `bench/3.2-interim.txt` with the headroom per gate. Any gate broken, or retained heap within 2 MiB of a gate: **STOP and give the user the numbers** before Task 48.

### Task 47: RBAC, chart toggles and the README fix

**Files:** Modify `deploy/helm/kgateway-ui/values.yaml` (`diagnostics.events`, `crds`, `proxyStats`, `endpoints` true; `controllerLogs` false; `controllerLogsNamespaces: [kgateway-system]`), `templates/clusterrole.yaml` (events, CRDs under their toggles), `templates/deployment.yaml` (flags), `deploy/helm/kgateway-ui/README.md` (each toggle and what it disables; the UI Pod reaches proxy Pods on 9091), `README.md:52` (the real read list), `deploy/helm/chart_test.go`.

- [ ] **Step 1: Failing tests** `TestEventsRuleFollowsToggle`, `TestCRDRuleFollowsToggle`, `TestTogglesPassedAsFlags`, `TestClusterRoleIsReadOnlyWithoutSecretsOrProxy` (unchanged default: no secrets, configmaps, `*/proxy`, `pods/portforward`, `pods/log`).
- [ ] **Step 2: Implement, run** `REQUIRE_HELM=1 go test ./deploy/...` → PASS; **commit** `feat(chart): diagnostics toggles and their RBAC; README lists what the UI reads`.

### Task 48: SPA types, components and `renderWhenFontsReady`

**Files:** Modify `web/packages/shell/src/api/diagTypes.ts`, `diagHooks.ts` (`useDiagnostics`, `useProcesses`), `src/index.ts`, `web/app/src/main.tsx`; Create `src/diagnostics/{NextSteps,EvidenceView,GroupRow,ProcessLine,ProcessTable}.tsx`, `src/diagnostics/diagnostics.test.tsx`, `src/shell/renderWhenFontsReady.ts`. **⚠ guardrail** (10 files).

**Interfaces:** `export function renderWhenFontsReady(root: HTMLElement, element: ReactElement): Promise<void>`; `NextSteps({ steps })` (numbered, code block, copy button announcing "Copied" via an `aria-live` region, permission line, note; commands as text); `EvidenceView({ evidence })` dispatching on `kind` (`xds-rejects`, `process`, `version-skew`, `proxy-stats`; unknown kinds fall through to `findingRenderers`); `GroupRow` (state, title, cause link, blast radius, age, code chip, `aria-expanded` next-step expander); `ProcessLine`, `ProcessTable`.

- [ ] **Step 1: Failing tests** `"NextSteps copies the command and announces it"`, `"commands render as text, never HTML"`, one per evidence kind, `"an unknown kind uses the finding renderer"`, `"renderWhenFontsReady renders after fonts load and after a font failure"`.
- [ ] **Step 2: Implement** with hover, active, focus-visible in both themes. **Commit** `feat(shell): diagnostics components; render after fonts`.

### Task 49: Install page: verdict, Needs attention, Processes

**Files:** Create `web/packages/shell/src/pages/install/NeedsAttention.tsx`, `ProcessesSection.tsx`; Modify `src/pages/install/InstallPage.tsx`, `HealthStrip.tsx`, `GatewayTable.tsx` (Data plane column), `src/shell/extension.ts` (`CorePages.DiagnosticsSection?`), `src/pages/install/InstallPage.test.tsx`. **⚠ guardrail** (7 files).

- [ ] **Step 1: Failing tests** `"healthy install shows the slim strip and What was checked, no Needs attention box"`, `"10 groups in rank order with Show all to the grouped findings page"`, `"restricted group row"`, `"Needs attention keeps its row height while loading"`, `"stale data turns every group unknown"`, `"Data plane column shows ✓, Rejects or Unknown"`, `"not available checks show their reason"`.
- [ ] **Step 2: Implement, run, commit** `feat(shell): health overview on the install page`.

### Task 50: Processes page and Gateway sections

**Files:** Create `web/packages/shell/src/pages/processes/ProcessesPage.tsx`, `src/pages/explore/gateway/{DataPlaneSection,ProxiesSection}.tsx`, `src/pages/processes/processes.test.tsx`; Modify `src/pages/explore/GatewayPage.tsx`, `src/shell/InstallNav.tsx` (Processes), `src/shell/extension.ts` (`ProcessesPage?`), `src/pages/explore/RoutesPage.tsx` (`gateway-rejecting` filter). **⚠ guardrail** (8 files).

- [ ] **Step 1: Failing tests** `"Processes page filters through the URL"`, `"not observed for a self-managed Gateway"`, `"Data plane section lists rejects, the status contradiction, commands and recently changed objects"`, `"Proxies section hides Pod names when restricted"`.
- [ ] **Step 2: Implement, run, commit** `feat(shell): Processes page; Gateway data plane and proxies`.

### Task 51: Wording pass for 3.2

**Files:** Modify `pkg/health/messages.go` (001, 019–022, 025, 026 messages and detail; the restart hold; check names), `pkg/diagnose/nextsteps.go` (step titles and notes), `web/packages/shell/src/copy/diagnostics.ts`, the components of Tasks 48–50, goldens; Create `phase-3.2-wording.md`.

- [ ] **Step 1: List every 3.2 string:** titles and messages per code, the status-says-healthy message, next-step titles, notes and the secret-care note, the port-forward note, "not available", "not observed", each availability reason, check names shown in "What was checked", group titles, section and column labels, empty states.
- [ ] **Step 2: Five options per string; a separate sub-agent picks.** **Step 3: Apply; regenerate goldens; run** `go test ./pkg/... && cd web && npm test` → PASS. **Commit** `copy: wording pass for the health overview`.

### Task 52: 3.2 budgets

**Files:** Modify `pkg/engine/budget_test.go`, `pkg/api/budget_test.go`, `.github/workflows/bench.yaml` (paths `pkg/process/**`, `pkg/proxystats/**`); Create `bench/3.2.txt`, `3.2-ci.txt`.

- [ ] **Step 1: Extend** with the worst-case world, proxy stats (2 per Gateway, served by an in-process stub) and process data: `TestBudgetAnalyzerStage` ≤ 20 ms warm, ≤ 2 ms memoized; spec-edit stream ≤ 25 %; OS memory ≤ 256 MiB (as built: median of five ≤ 288 MiB, D-043); `…/diagnostics` and `…/processes` latency; retained heap ≤ +24 MiB above the 3.1 baseline and ≤ 32 MiB; every other gate.
- [ ] **Step 2: Run** `make bench | tee docs/phases/phase-3/bench/3.2.txt` → PASS, or **STOP with the numbers**. **Commit** `test: 3.2 budgets with processes and proxy stats`.

### Task 53: kind e2e, Playwright and ui-loop for 3.2

**Files:** Create `test/e2e/fixtures/diagnostics.yaml` (GatewayParameters with an unpullable image), `test/e2e/diagnostics_test.go`, `web/e2e/diagnostics.spec.ts`, `phase-3.2-evidence.md`; Modify `test/e2e/setup.sh`, `.github/workflows/e2e.yaml` (`NACK=1`).

- [ ] **Step 1: Write `TestDataPlaneDiagnostics`** on the NACK fixture: 001 with the stream count, the status-says-healthy message, the controller replica in the log command, the rejecting proxy Pod and the Envoy resource from stats; proxy with its controller scaled to 0 → 026 after 60 s; unpullable image → 021; `kubectl delete pod` of the controller → 019, and the install unknown (not healthy) during the hold. Record the Envoy cluster and route-config names seen on both tags for the namer (Task 41).
- [ ] **Step 2: Playwright:** install page → a group → next steps copy; Processes page; Gateway data plane; both themes, hover and focus screenshots.
- [ ] **Step 3: Run** both `make e2e` pairs → PASS. **Step 4: `/ui-loop`** on the install page, Processes and the Gateway sections (both themes, four widths, loading, stale). **Commit** `test(e2e): data plane and process health on kgateway 2.3 and 2.4`.

### Task 54: Close sub-phase 3.2

**Files:** Create `phase-3.2-complete.md`, `reviews/3.2-codex.md`; Modify `README.md` (Upgrade notes: 001 re-specified; new codes; new watches and toggles; `since` resets on restart), `docs/phase-status.md`, `docs/roadmap.md`, `design.md` (deviations).

- [ ] **Step 1: Standard gates.** **Step 2: Codex review** against §6, §8.1–§8.5, §8.8, §9, §13, §14 rules 13–16, §16.3, §17, §18 and D-029, D-030, D-034, D-037. Focus: the restart hold; per-Pod deltas; no message, probe output or log text read; proxy stats bounds; not-available never unknown; group and next-step authorization; no layout shift. **Step 3: Triage; fix; rerun. Step 4: Record** (bench table vs 3.1). **Step 5: Commit** `docs: close sub-phase 3.2`.

---

# Sub-phase 3.3 — Analyzer

### Task 55: 3.3 brief

- [ ] Record the sub-phase; brief from §7.1–§7.7, §8.9, §10, §14 table rows 013–018, §21. **Commit** `docs: brief for sub-phase 3.3`.

### Task 56: KGWUI-003 kinds and the 1 KiB clip

**Files:** Create `pkg/health/freshness.go`, `pkg/health/freshness_test.go`; Modify `pkg/health/checks.go` (003 sets `FreshnessEvidence`), `pkg/health/evidence.go`.

**Interfaces:** `func Clip(s string) string` — a message over 1 KiB is cut at a UTF-8 boundary with `…[cut]`. `FreshnessEvidence{Freshness: "never-written"|"stale"|"pending"|"no-ancestor", Observed, Current *int64, Since *time.Time}` with the rules of §10.1 (`never-written` includes the CRD default `Pending`/"Waiting for controller" at the Unix epoch).

- [ ] **Step 1: Failing tests** one per kind through `/health`; `TestEvidenceClipsLongStatus` (a 64 KiB message with a fake key at its end: the key appears in no API body). **Step 2: Implement, run, commit** `feat(health): freshness kinds for KGWUI-003; clip long status text`.

### Task 57: KGWUI-013 and 014 from status

**Files:** Create `pkg/health/refs.go`, `pkg/health/refs_test.go`, `pkg/kinds/catalog.go`; Modify `pkg/kinds/monitor.go` (publish the catalog), `pkg/health/checks.go` (002 skips conditions a specific code owns), `pkg/health/finding.go` (codes, `Input.Catalog`), `pkg/api/additive_test.go` (exceptions), `pkg/api/testdata/golden/health.json` and other goldens carrying these conditions, `test/e2e/api_test.go` (002 expectations for these reasons move to 013/014). **⚠ guardrail** (≥ 9 files).

**Interfaces:**
- `CodeRefUnresolved = "KGWUI-013"` (`ref-unresolved`), `CodeRefNotPermitted = "KGWUI-014"` (`ref-not-permitted`), errors; Key parentRef as written or listener name.
- `kinds.Catalog` — every served (group, kind) from discovery: `func (c Catalog) Served(group, kind string) bool`, `func (c Catalog) GroupsWithKind(kind string) []string`.
- 013 explanation, first match wins: (1) group/kind not served; (4) the controller's reason and message, clipped. (2) and (3) arrive in Task 77. Listener `InvalidCertificateRef` quotes the controller.
- 014: route parent or listener `ResolvedRefs=False/RefNotPermitted`; explanation (1) runs first, so a wrong group is explained as a wrong group.
- **Attribution (review finding 6):** the condition names a parent or listener, not a ref. Evidence carries `candidates []RefLocation` (`{path: "spec.rules[2].backendRefs[0]", ref}`): for 014 every cross-namespace ref of the failing field on that route or listener; for 013 the refs the UI's own explanation singles out (kind not served, missing Service, missing port), else every ref of the failing field. `attributed bool` is true only with exactly one candidate.
- One condition, one finding: 013/014 replace 002 for those reasons (D-030).

- [ ] **Step 1: Failing tests** route and listener cases; a typo'd group; a kgateway-specific field (no YAML); a route with two cross-namespace backendRefs (two candidates, not attributed); `TestSpecificCodeReplacesConditionFalse` (no 002 for those conditions; other `False` conditions still 002); the additive test passes with the listed exceptions only (a new unlisted scalar change still fails).
- [ ] **Step 2: Implement, run, commit** `feat(health): unresolved and not-permitted references`.

### Task 58: Suggested fixes for 013 and 014

**Files:** Create `pkg/diagnose/fix.go`, `pkg/diagnose/fix_test.go`; Modify `pkg/health/evidence.go` (`Finding.Fix *SuggestedFix`), `pkg/api/findings.go` (serve and authorize), `pkg/api/findings_test.go`.

**Interfaces:** `type SuggestedFix struct { Title string; Target objects.Ref; Create bool; Path string; YAML string; Basis string; Names []objects.Ref }`. A fix is built only for an attributed finding (Task 57, exactly one candidate); otherwise the detail lists the candidates and next steps, with no YAML (review finding 6). 014: the ReferenceGrant (`from`: group, kind, namespace of the referrer; `to`: group, kind, optional name) as a new object in the target namespace, spec-defined fields only. 013 (1): the ref with the one served group that has that kind, as a fragment at `spec.rules[i].backendRefs[j]`. More than one candidate → no fix. A fix naming a hidden object is dropped (§14 rule 15).

- [ ] **Step 1: Failing tests** one group → fix, two → none; grant for backendRef and certificateRef; `TestNoFixWithoutAttribution` (two cross-namespace refs on the failing parent: candidates listed, no YAML); no fix when the grant names a hidden object; YAML parses and round-trips. **Step 2: Implement, run, commit** `feat(diagnose): deterministic suggested fixes (D-036)`.

### Task 59: KGWUI-015 rules replaced or dropped

**Files:** Create `pkg/health/rules.go`, `pkg/health/rules_test.go`; Modify `pkg/health/checks.go` (skip those reasons in 002), `pkg/diagnose/group.go` ("Replaced by policy"), `pkg/api/additive_test.go` and affected goldens (exceptions for 002 → 015), `test/e2e/api_test.go` (expectations for replaced-rule routes). **⚠ guardrail** (≥ 6 files).

**Interfaces:** `CodeRuleReplaced = "KGWUI-015"` (`rule-replaced`, error); Key parentRef and match index; `RuleReplacedEvidence{MatchIndex, Reason, Message, Policy *LinkedRef}`; message parse `^(Replaced|Dropped) Rule \((\d+)\): (.*)$`; a `group/Kind/namespace/name` token of a served policy kind links it; after 10 min of replacement, the #14047 hint.

- [ ] **Step 1: Failing tests** 2.3 `Accepted=False` and 2.4 `kgateway.dev/Programmed=False`; parsed policy and unparseable message; hint at 9:59 and 10:01. **Step 2: Implement, run, commit** `feat(health): rules replaced or dropped`.

### Task 60: Policy targets rule (computed)

**Files:** Create `docs/phases/phase-3/computed-rules.md` (section "Policy targets", pinned per rule set), `pkg/computed/targets.go`, `pkg/computed/kgw23/targets.go`, `pkg/computed/kgw24/targets.go`, `pkg/computed/testdata/kgateway-2.{3,4}/policy-targets.yaml`, `test/e2e/fixtures/observe/policy-targets.yaml`; Modify `pkg/computed/golden_test.go`, `examples/downstream/acme.go`, `examples/downstream/main_test.go`. `pkg/computed/rules.go` is **not** changed. **⚠ guardrail** (10 files).

**Interfaces:** `type TargetInput struct { Policy objects.Ref; TargetRefs []objects.TargetRef; Selectors []objects.TargetSelector; Lookup func(objects.GroupKind, ns, name string) (*unstructured.Unstructured, bool); Labelled func(objects.GroupKind, ns string, sel map[string]string) bool; Served func(objects.GroupKind) bool; ScopeKnown bool }`; `type TargetResult struct { Index int; Ref objects.TargetRef; Outcome string /* found|not-found|section-not-found|matches-nothing|unknown */ }`; `type PolicyTargeter interface { PolicyTargets(in TargetInput) []TargetResult }` — an **optional** interface a rule set implements, detected by type assertion on `computed.Rules`, so `computed.Rules` does not change after 3.1 (review finding 15); a rule set without it raises no computed 016. Local refs in the policy's namespace (GEP-713); unknown kinds raise nothing; scope unknown → every result `unknown` (D-024).
- **Verification granularity (review finding 9):** `TargetResult.Verifiable bool` — true only for a policy with exactly one target whose expected ancestor is that target (a Gateway) or the target's Gateway (a route). Every other shape is `not-verified`.

- [ ] **Step 0: Observe first.** On 2.3.11 and 2.4.7 (kind `kgwui-e2e`), apply `observe/policy-targets.yaml`: a TrafficPolicy targeting one Gateway, one HTTPRoute, a route section, two routes on one Gateway, and a label selector. Record per shape which ancestors the controller writes and whether any ancestor can be tied to one target. Write the result into the `computed-rules.md` section before any code; if it contradicts the verifiable shapes above, STOP and ask the user.
- [ ] **Step 1: Fixtures with provenance**, written from the rules and the observations, never generated. **Step 2: Failing golden tests** plus `TestRuleSetWithoutPolicyTargeterRaisesNo016` and `TestDownstreamRuleSet` (an `acme` rule set wrapping `kgw24` via `WithComputedRules`, without `PolicyTargeter`, builds and serves). **Implement; run; commit** `feat(computed): policy target lookup for kgateway 2.3 and 2.4 as an optional rule-set interface`.

### Task 61: KGWUI-016 and its verifier

**Files:** Create `pkg/health/targets.go`, `pkg/health/targets_test.go`; Modify `pkg/computed/check.go` (kind `policy-target`), `pkg/health/checks.go` (016 replaces 003 when every target is missing), `pkg/api/computed_test.go`, `pkg/api/additive_test.go` and affected goldens (exceptions for 003 → 016), `test/e2e/api_test.go:189-195` (`tpMissingTarget` expects 016 on 2.3 and 2.4). **⚠ guardrail** (≥ 7 files).

**Interfaces:** `CodePolicyTargetNotFound = "KGWUI-016"` (`policy-target-not-found`); computed: warning, labelled with rule and rule set, after the policy's grace then 30 s held (Store; `WakeAt`); Key targetRef index and ref as written. Verifier, for `Verifiable` results only: a current ancestor for a target the rule called missing → KGWUI-012 kind `policy-target`. Other results show 016 labelled "not verified" and raise no KGWUI-012.

- [ ] **Step 1: Failing tests** missing target, missing section, empty selector, partial targets (016 per missing target, other findings unchanged), all missing (no 003), the verifier raising 012 for a single-target policy, a multi-target policy marked not verified with no 012, scope unknown, 29 s (none) and 31 s; `TestNewerThanTestedUsesComputed016` (Review Focus 4: a `v2.5.0-rc.1` controller on the 2.4 rule set shows the note and the computed 016).
- [ ] **Step 2: Implement, run, commit** `feat(health): policy target not found, computed and verified`.

### Task 62: KGWUI-017 listener conflicts

**Files:** Create `pkg/health/conflicts.go`, `pkg/health/conflicts_test.go`; Modify `pkg/health/evidence.go`.

**Interfaces:** `CodeListenerConflicted = "KGWUI-017"` (`listener-conflicted`, error), Key listener name; trigger `Conflicted=True` on an owned Gateway or ListenerSet listener; evidence lists other listeners on the same Gateway (ListenerSets included) that kgateway kept, on the same port, matched by the condition's reason (`HostnameConflict`: exact hostname, rule "listener port and hostname"; `ProtocolConflict`: different protocol, rule "listener port and protocol"; R94, R95), labelled "Computed by the UI"; none found → the "no other listener overlaps" text, no disagreement. Deps: each other listener's owner.

- [ ] **Step 1: Failing tests** both sides named; no other side; `Accepted=True/ListenersNotValid` Gateway with a conflicted listener now raises 017. **Step 2: Implement, run, commit** `feat(health): conflicted listeners with both sides named`.

### Task 63: KGWUI-018 status not progressing

**Files:** Modify `pkg/health/freshness.go`, `pkg/ctrlmetrics/parse.go` (`kgateway_status_syncer_status_syncs_total` per Pod; removed in 3.5), `pkg/diagnose/group.go` ("Status not progressing"); Test `pkg/health/freshness_test.go`.

**Interfaces:** `CodeStatusNotProgressing = "KGWUI-018"` (warning) on the controller Deployment: controller ready, ≥ 1 owned object with a 003 warning for 5 min, and ≥ 10 objects or ≥ 10 % of objects with status (minimum 3) in that state; start in the Store, `WakeAt(start + 5 min)`; evidence counts per kind and oldest age (the status-sync count is removed, D-058 item 10, 3.5); next step the log command grepping `status`. Not-ready controller → 005 only.

- [ ] **Step 1: Failing tests** threshold edges (9 and 10; 2, 3 at 10 %), maturation at 4:59 and 5:01, 005 instead. **Step 2: Implement, run, commit** `feat(health): status not progressing`.

### Task 64: 3.3 groups and the grouped findings view

**Files:** Modify `pkg/diagnose/group.go` (families "Missing backend", "Not permitted", "Replaced by policy", "Status not progressing" in §5.2 order), `pkg/api/findings.go` (`?view=groups` → `Page<GroupRow>` with `severity`, `code`, `q` filters), goldens `findings-groups.json`, `…-restricted.json`.

The design names `?view=groups` for the page (§17.1) but no API for paging groups; this plan serves it from the findings endpoint.

- [ ] **Step 1: Failing tests** exclusive membership across the full family table; group paging; restricted group rows. **Step 2: Implement, run, commit** `feat(api): grouped findings view`.

### Task 65: Controller log reader (opt-in)

**Files:** Create `pkg/ctrllogs/collector.go`, `pkg/ctrllogs/stream.go`, `pkg/ctrllogs/events.go`, `pkg/ctrllogs/collector_test.go`; Modify `pkg/app/app.go`, `internal/boundary/imports_test.go` (`pkg/ctrllogs/stream.go`: "follows controller Pod logs"), `deploy/helm/kgateway-ui/templates/role-logs.yaml` (new), `deploy/helm/chart_test.go`. **⚠ guardrail** (8 files).

**Interfaces:**
- One follow stream per controller replica (`sinceSeconds=900`), reconnecting with exponential back-off (1 s to 60 s); after a restart, one read of the previous container capped at 256 KiB. A substring prefilter on `computed.LogParser.Markers()` runs first; a line without a marker is dropped unread and never buffered beyond the line scanner.
- `type Event struct { Time time.Time; Marker string; Gateway objects.Ref; TypeURL string; Names []string; Class string; Object *objects.Ref }`; at most 1,000 per install, oldest dropped; published as `controller-logs/events`.
- Names: a parser's extracted name is kept only if it equals an Envoy resource name in that Gateway's latest proxy stats (`proxy-stats/stats`, a declared input); otherwise only the class is kept. With proxy stats off or failing, no names are kept (review finding 5). Unknown error class → `other`; 10 consecutive parser failures on marked lines → capability `controllerLogs` `failing`, "log format not recognised".
- Chart: `role-logs.yaml` renders, only with `diagnostics.controllerLogs: true`, one Role with `get` on `pods/log` and a RoleBinding per entry of `diagnostics.controllerLogsNamespaces`. The design names "the install namespace" but the chart cannot discover installs; this value is the plan's answer.

- [ ] **Step 1: Failing tests** prefilter (a 1 MiB unmarked line is never parsed); reconnect with a fake stream; the 256 KiB cap; the event cap; 10 failures; toggle off (no Role, capability `off`); Role missing (`denied`); `TestLogRoleOnlyInListedNamespaces`.
- [ ] **Step 2: Implement, run, commit** `feat(ctrllogs): opt-in controller log reader keeping fixed fields (D-037)`.

### Task 66: Log parsers pinned per version

**Files:** Create `pkg/computed/logs.go`, `pkg/computed/kgw23/logs.go`, `pkg/computed/kgw24/logs.go`, `pkg/computed/testdata/kgateway-2.{3,4}/logs/*.txt`, `pkg/ctrllogs/leak_test.go`; Modify `pkg/ctrllogs/collector.go` (name check against proxy stats). The log parsers are optional rule-set interfaces like `PolicyTargeter` (Task 60).

**Interfaces:** `type LogParser interface { Markers() []string; Parse(line []byte) (ctrllogs-neutral fields, ok bool, err error) }` returning only the §8.9 fields: `"xds error"` (time, Gateway, type URL; names only from Envoy's fixed NACK shapes such as `Error adding/updating listener(s) <name>:`; class from a fixed list), the snapshot-deferral line (exact text pinned from `v2.3.11` and `v2.4.7` source in this task and cited `file:line`), a Go panic (class `panic`, top frame function, previous container only), deployer apply errors (time, object kind and name, class).

- [ ] **Step 1: Captured lines** from the NACK fixture on both tags (Task 20 cluster), plus synthetic lines for each class. **Step 2: Failing tests** a name equal to one in the Gateway's proxy stats and the class kept; unknown class → `other`; `TestUnknownNameKeepsOnlyClass` (a well-formed name not in proxy stats is dropped); `TestLogSecretLeavesNoTrace` (review finding 5: the sentinel `AKIAFAKEKEY123456` placed in **every** accepted capture position of every marker — the listener, route and cluster name slots, the Gateway and type fields, the object name of a deployer error, the panic frame — and behind spoofed markers (a marker inside a quoted value, two markers on one line, a marker in a JSON key): absent from every API body, from a reflect walk of the published model, and from the UI's own `slog` output captured in the test). **Step 3: Implement, run, commit** `feat(computed): controller log parsers for kgateway 2.3 and 2.4`.

### Task 67: Derived log evidence

**Files:** Modify `pkg/health/xds.go` (`logEvents`; `since` from the earliest derived event, surviving a UI restart), `pkg/health/process.go` (019 panic top frame; 006 and 021 deployer errors), `pkg/health/evidence.go` (deferral as "withheld" evidence on 2.3/2.4), `pkg/api/findings_test.go`.

- [ ] **Step 1: Failing tests** 001 with log events and `since` equal to the earliest event after an engine restart; 019 top frame; 021 deployer class; deps follow §14 (names mapped to objects filtered as those objects). **Step 2: Implement, run, commit** `feat(health): derived log signals as evidence`.

### Task 68: `docs/findings.md` and "Learn more"

**Files:** Create `docs/findings.md`; Modify `pkg/api/diag_types.go` (`FindingRow.learnMore` absent; the SPA builds the anchor from the code), `web/packages/shell/src/diagnostics/EvidenceView.tsx`.

- [ ] **Step 1: One entry per code 001–026** (cause, evidence, fix; anchors `#kgwui-0NN`); the README's note that UI-derived `since` resets on restart; a test that every `health.Code*` has an anchor. **Commit** `docs: finding reference`.

### Task 69: SPA for 3.3

**Files:** Create `web/packages/shell/src/diagnostics/SuggestedFix.tsx`; Modify `EvidenceView.tsx` (`ref-unresolved`, `ref-not-permitted`, `rule-replaced`, `freshness`, log events), `src/pages/findings/FindingsPage.tsx` (grouped view), `FindingPage.tsx` (fix above next steps, Learn more), `src/api/diagTypes.ts`, tests.

- [ ] **Step 1: Failing tests** `"SuggestedFix copies and downloads the YAML"`, `"fragment shows its location"`, `"grouped view round-trips ?view=groups"`, one per new evidence kind, `"log events show names and class, never text"`. **Step 2: Implement** (hover, active, focus-visible; both themes). **Commit** `feat(shell): analyzer evidence, suggested fixes and grouped findings`.

### Task 70: Wording pass for 3.3

- [ ] Strings: messages per code 003 kinds, 013–018; explanation texts; fix titles and basis lines; the 015 hint; log-signal labels; grouped-view labels; capability reasons. Same procedure; record in `phase-3.3-wording.md`; apply; goldens; **commit** `copy: wording pass for the analyzer`.

### Task 71a: One-shot config helper and static metrics source (E8, D-053)

Added by the user on 2026-11-04 for program edge E8 ([#13](https://github.com/DuncanDoyle/kgateway-ui/issues/13)); decision D-053. It runs before Task 71, so the budgets measure `pkg/app` built through the helper. Its one-shot test is also the acceptance test of edge E3.

**Files:** Create `pkg/setup/setup.go`, `pkg/setup/setup_test.go`, `pkg/ctrlmetrics/static.go`, `pkg/ctrlmetrics/static_test.go`; Modify `pkg/app/app.go` (build its config through `pkg/setup`), `internal/boundary/imports_test.go` (`pkg/setup/setup.go`: "builds cache and engine config from the Kubernetes clients"). **⚠ guardrail** (6 files; approved by D-053).

**Interfaces:**
- `pkg/setup` builds `cache.Config` and `engine.Config` from the Kubernetes clients, the options and an `*extension.Registry` the way `pkg/app` does: served kinds, detectors and their Pod, Deployment and proxy Pod selectors, scope, the scope verifier and its persistence, rule sets, the process source, checks, the metrics max age (3 × the metrics interval) and the diagnostics toggles. `pkg/app` uses it, so the two cannot drift. `pkg/app` keeps its exported names.
- `ctrlmetrics.Static` is a `ctrlmetrics.Source` over one `Collect` result per install. Staleness is the engine's, through `MetricsMaxAge`, as in the UI.
- Neither waits. Time-based checks (restart hold, grace periods, scope-verifier persistence, metric staleness) are the caller's: a single pass reads them as pending, and `Report.Next` says when to compute again. The Go doc of both says so.

- [ ] **Step 1: Failing tests** a black-box test (package `setup_test`) that imports only `pkg/` packages, builds both configs over fake clients, computes once and gets the same findings and ids as `pkg/app` over the same objects, 013–018 included; `ctrlmetrics.Static` at max age − 1 s (used) and + 1 s (stale, `xDS rejects` unavailable); an install without a result; existing `pkg/app` tests and goldens unchanged.
- [ ] **Step 2: Implement, run, commit** `feat(setup): exported cache and engine config for one-shot callers (D-053)`.

### Task 71: 3.3 budgets

- [ ] Extend the worst-case world with the analyzer codes and the log reader at 50 matching lines/s (stub stream); every §16.3 gate; `make bench | tee bench/3.3.txt` → PASS or **STOP with the numbers**; path filter `pkg/ctrllogs/**`. **Commit** `test: 3.3 budgets with the analyzer and log reader`.

### Task 72: kind e2e for 3.3

**Files:** Create `test/e2e/fixtures/analyzer.yaml`; Modify `test/e2e/diagnostics_test.go`, `.github/workflows/e2e.yaml` (one row with `UI_EXTRA_ARGS=--diagnostics-controller-logs` and the chart value), `web/e2e/diagnostics.spec.ts`.

- [ ] **Step 1: Fixtures:** a missing ReferenceGrant (014), a wrong Service port (013, controller message), a policy with a missing target (016 computed), a conflicted ListenerSet listener (017).
- [ ] **Step 2: `TestAnalyzer`** asserts each finding and fix; `TestControllerLogSignals` (logs on): derived names that match proxy stats and the class on 001, and no response body of any endpoint contains the Envoy message substring. **Step 3: Run** both pairs → PASS; `/ui-loop` on finding detail with a fix. **Commit** `test(e2e): analyzer and log signals on kgateway 2.3 and 2.4`.

### Task 73: Close sub-phase 3.3

- [ ] Standard gates; Codex review against §7, §8.9, §10, §14, §16.3 and D-030, D-036, D-037, D-053 (focus: one finding per condition; fixes only with one answer; no log text anywhere; computed 016 verifier; grouping exclusivity; `pkg/setup` and `pkg/app` cannot drift); triage; record (bench vs 3.2); README upgrade notes (013–018, 002 replaced for those reasons, the log toggle, `pkg/setup`); status and roadmap; close #13 with the commit; draft the E3 and E8 program-file update and the kgwctl `cross-repo` notice, posted with the user's go. **Commit** `docs: close sub-phase 3.3`.

---

# Sub-phase 3.4 — Backends and kgateway 2.5

### Task 74: 3.4 brief

- [ ] Record the sub-phase; brief from §7.6, §8.6, §11, §21 (023, 024, the 2.5 row); note whether 2.5.0 GA is out. **Commit** `docs: brief for sub-phase 3.4`.

### Task 75: Service and EndpointSlice watches

**Files:** Create `pkg/cache/services.go`, `pkg/cache/services_test.go`; Modify `pkg/cache/cache.go` (optional, not in `cluster access`, Task 35), `pkg/app/app.go` (toggle, capability `endpoints`), `deploy/helm/kgateway-ui/templates/clusterrole.yaml`, `deploy/helm/chart_test.go`, `internal/boundary/imports_test.go` (`pkg/cache/services.go`). **⚠ guardrail** (7 files).

**Interfaces:** `type ServiceFact struct { Namespace, Name, Type string; Ports []ServicePort }` (`ServicePort{Name string; Port, Target int32}`); `type SliceFact struct { Namespace, Service string; Ports []SlicePort; Ready []int /* ready endpoints per entry of Ports, ready unset counts as ready (compact form, D-057) */ }`; `Snapshot.Services`, `Snapshot.EndpointSlices`; transforms keep only these fields (about 200 bytes each). Capability `endpoints` `on` only when both watches run.

- [ ] **Step 1: Failing tests** transform shape; unset `ready` counts; toggle off (no watch, no rule); denied (no KGWUI-008, install not unknown). **Step 2: Implement, run, commit** `feat(cache): Services and EndpointSlices, cut down at the informer (D-031)`.

### Task 76: KGWUI-023 and the empty-backend group

**Files:** Create `pkg/health/endpoints.go`, `pkg/health/endpoints_test.go`; Modify `pkg/diagnose/group.go` ("Empty backend"), `pkg/proxystats/collector.go` (membership on demand), `pkg/api/findings.go` (evidence).

**Interfaces:** `CodeBackendNoReadyEndpoints = "KGWUI-023"` (warning) on the Service, Key the port as the backendRef wrote it; rule of §11 (core Service, weight > 0 with unset = 1, a current `ResolvedRefs=True` from this install on that parent, not `ExternalName`, no ready endpoint for that port), held 30 s per Service port in the Store; `EndpointsEvidence{Service, Port, Ready, Total, Slices}`. Membership on demand was removed in 3.4 (D-058): it needs a forward Service-to-cluster name mapping.

- [ ] **Step 1: Failing tests** zero ready; unset ready; `ExternalName`; weight 0; `ResolvedRefs=False`; delegation and mirror refs (none); 29 s and 31 s; endpoints off → not available, install not unknown. **Step 2: Implement, run, commit** `feat(health): backends with no ready endpoints`.

### Task 77: 013 Service explanations and the port fix

**Files:** Modify `pkg/health/refs.go` (explanations 2 and 3), `pkg/diagnose/fix.go` (one-port Service), `pkg/health/refs_test.go`, `pkg/diagnose/fix_test.go`.

- [ ] **Step 1: Failing tests** Service missing; port missing with the port list; a one-port Service gives the corrected port at its path; a two-port Service gives none; the Service as an evidence dependency (hidden Service → `evidenceRestricted`). **Step 2: Implement, run, commit** `feat(health): Service explanations and port fix for unresolved references`.

### Task 78: Platform-neutral analyzer test

**Files:** Create `pkg/engine/neutral_test.go`.

- [ ] **Step 1: `TestAnalyzerWithoutPodsEventsServicesOrCRDs`** (§19): a snapshot with none of them: process findings "not observed", 023 "not available", every other code equal to the full-snapshot run. **Commit** `test(engine): analyzer stage on a platform without Pods, Events, Services or CRDs`.

### Task 78a: DirectResponse, GatewayExtension and ReferenceGrant (#17, D-056)

**Files:** Modify `pkg/kinds/upstream.go` (three descriptors, appended last), `pkg/kinds/kinds.go` (category `referencegrant`), `deploy/helm/kgateway-ui/templates/clusterrole.yaml`, `deploy/helm/chart_test.go`, `pkg/api` goldens; web nav only if a new category needs a label.

- [ ] **Step 1: Failing tests** the three kinds listed and described through `/api` when served; absent when not served; v1 kind-list order unchanged; ReferenceGrant on whichever version is served; 013 and 014 goldens unchanged with ReferenceGrants present. **Step 2: Implement, run, commit** `feat(kinds): DirectResponse, GatewayExtension and ReferenceGrant (#17)`.

### Task 78b: `cache.Wait` (#20, D-056)

**Files:** Modify `pkg/cache/cache.go`, `pkg/cache/cache_test.go`; README (one-shot use).

- [ ] **Step 1: Failing tests** Start, cancel, `Wait`, then a goroutine-leak check; `Wait` before `Start` and a second `Wait` return at once. **Step 2: Implement, run, commit** `feat(cache): Wait for the goroutines Start launched (#20)`.

### Task 79: SPA for 3.4

- [ ] `EvidenceView` gains `endpoints` and the 024 messages; "not available on this version" state; tests per state; hover, active, focus in both themes. **Commit** `feat(shell): backend readiness evidence`.

### Task 80: Wording pass for 3.4

- [ ] Strings for 023, 024 (both messages), endpoints evidence labels, "not available on this version", the 013 Service explanations and the port-fix title. Same procedure; `phase-3.4-wording.md`; **commit** `copy: wording pass for backends`.

### Task 81: 3.4 budgets

- [ ] Large tier with 5,000 Services and 6,000 EndpointSlices and the worst case (5 % empty); every §16.3 gate; `make bench | tee bench/3.4.txt` → PASS or **STOP with the numbers**. **Commit** `test: 3.4 budgets with Services and EndpointSlices`.

### Task 82: kind e2e for 3.4 (2.3.11 and 2.4.7)

**Files:** Create `test/e2e/fixtures/backends.yaml`; Modify `test/e2e/diagnostics_test.go`, `web/e2e/diagnostics.spec.ts`.

- [ ] A backend scaled to zero (023 after 30 s); the wrong-port fixture now gives explanation 3 and a fix. Both pairs → PASS; `/ui-loop` on 023. **Commit** `test(e2e): backend readiness`. If 2.5.0 GA is not out: run Task 87 for this part now; Tasks 83–86 and a second Task 87 run after GA.

### Task 83: 2.5.0 GA re-checks and captures

**Files:** Create `pkg/ctrlmetrics/testdata/v2.5.0-*.txt`, `pkg/proxystats/testdata/v2.5.0-*.txt`; Modify `phase-3.4-brief.md`.

- [ ] **Step 1:** Confirm the GA tag exists (`gh api repos/kgateway-dev/kgateway/releases/tags/v2.5.0`). Re-read from the tag: the per-client family names and labels (§8.6), `kgateway_build_info`, `TargetNotFound` (#14709), `EnableStatsRoute` default, the Gateway API pin in `go.mod`. Any change from the design: STOP and report.
- [ ] **Step 2:** Capture metrics and stats on the NACK fixture and a withheld-config case; re-check any 3.1 RC capture against GA. **Commit** `test: kgateway 2.5.0 captures`.

### Task 84: The `kgateway-2.5` rule set

**Files:** Create `pkg/computed/kgw25/` (copy of `kgw24`), `pkg/computed/testdata/kgateway-2.5/`; Modify `pkg/computed/rulesets/rulesets.go` (2.5 selects `kgw25`), `pkg/computed/rulesets/gatewayapi.go` (2.5 row from the GA `go.mod`), `pkg/health/targets.go` (status path: ancestor reason `TargetNotFound` → 016 error; computed off for named targets on this set, D-020 rule 5; selectors stay computed, D-058 item 7, 3.5). **⚠ guardrail** (copied package, > 400 lines).

- [ ] **Step 1: Failing tests** 016 from status on `kgw25`; no computed 016 for a named target there (3.5: a selector that matches nothing is computed); `kgw24` unchanged; version table. **Step 2: Implement, run, commit** `feat(computed): kgateway 2.5 rule set; policy targets from status (D-033)`.

### Task 85: KGWUI-024 and `kgateway_build_info`

**Files:** Create `pkg/health/withheld.go`, `pkg/health/withheld_test.go`; Modify `pkg/ctrlmetrics/parse.go` (per-client families; build info), `pkg/install/upstream.go` (controller version from `kgateway_build_info` when present), `pkg/api/findings_test.go`.

**Interfaces:** `CodeConfigWithheld = "KGWUI-024"`: `withheld_clients{state="warm_withheld"} > 0` → error; an increase of `bounded_publishes_total{mode=~"flip_release|warm_release|carry_expired"}` in 15 min (Store per Pod UID) → warning; family absent → `NotAvailable(…, AvailNotOnVersion, …)`, install never unknown.

- [ ] **Step 1: Failing tests** each metric case; family absent on 2.3 and 2.4 gives "not available on this version" and a healthy install. **Step 2: Implement, run, commit** `feat(health): withheld configuration on kgateway 2.5`.

### Task 86: The 2.5 e2e row and the TCPRoute fixture

**Files:** Modify `test/e2e/fixtures/explorer.yaml:99` (TCPRoute), `test/e2e/setup.sh`, `.github/workflows/e2e.yaml` (row `{ kgw: v2.5.0, gwapi: v1.7.x }`), `README.md` (Supported versions), `test/e2e/diagnostics_test.go`.

- [ ] **Step 1: Check served versions** on each bundle in the matrix: `kubectl --kubeconfig "$E2E_DIR/kubeconfig" --context kind-kgwui-e2e get crd tcproutes.gateway.networking.k8s.io -o jsonpath='{.spec.versions[*].name}'`. If `v1` is served on every row, move the TCPRoute to `gateway.networking.k8s.io/v1`; otherwise split it into `explorer-tcproute-v1alpha2.yaml` and `explorer-tcproute-v1.yaml`, and `setup.sh` applies the one the cluster serves.
- [ ] **Step 2:** The 2.5 row runs the full suite with 016 from `TargetNotFound` and a withheld-config case for 024. All rows → PASS. **Commit** `test(e2e): kgateway 2.5 row; TCPRoute fixture on the served version`.

### Task 87: Close sub-phase 3.4

- [ ] Standard gates; Codex review against §7.6, §8.6, §11, §19, §21 and D-031, D-033, D-056 (focus: 023 stays within resolved Service refs; Service data never decides resolution; not-on-version never unknown; `kgw25` is a copy plus the status path); triage; record (bench vs 3.3; whether the 2.5 part shipped); README upgrade notes (023, Services and EndpointSlices read, the `endpoints` toggle; 2.5 support at GA); status (next: the phase review and stabilization) and roadmap. **Commit** `docs: close sub-phase 3.4`.

---

## Design coverage

| Design section | Tasks |
|---|---|
| §4.1 seams 1–7, rules | 4, 6, 16 (gap 3), 5, 8, 9, 7; proof 10; stability 11 |
| §5.1 identity, `since` | 22; 001 `since` 40; log `since` 67 |
| §5.2 groups | 23; 43; 64; 76 |
| §5.3 rank | 23 |
| §6 overview | 45, 49 |
| §7.1 codes, availability | 36; each code's task; 002 replaced 57, 59 |
| §7.3 013, 014 | 57, 58, 77 |
| §7.4 015 | 59 |
| §7.5 017 | 62 |
| §7.6 016 | 60, 61, 84 |
| §7.7 suggested fixes | 58, 77, 69 |
| §8.1–§8.2 metrics | 21, 46, 63, 85 |
| §8.3 001 | 40, 42, 67 |
| §8.4 `gateway-rejecting` | 43, 50 |
| §8.5 next steps | 44, 48 |
| §8.6 024 | 85 |
| §8.7 upstream proposal | user's call; reminder in each brief |
| §8.8 proxy stats | 41, 42, 76 (membership), 47 |
| §8.9 log signals | 65, 66, 67 |
| §9.1–§9.2 process seam, Events | 35, 37 |
| §9.3 019–021 | 38 |
| §9.4 022, CRDs | 39 |
| §10 freshness, 018 | 56, 63 |
| §11 023 | 75, 76 |
| §12 commands for what the UI cannot read | 44 |
| §13 API | 24, 45, 46, 64 |
| §14 Authorizer rules 11–18 | 24 (11, 12, 17, 18), 45 (13–16), 58 (15 for fixes) |
| §15 carry-overs | P4 identity 22; delegation memory 19; spec-edit 12; fonts 48; KRT 13–14; prefix registry 2; base version 3; refactors audit 1 |
| §16.1 KRT | 13, 14 |
| §16.2 contract, memo | 15, 16, 17, 18 |
| §16.3 budgets, scale tiers | 12, 25, 32, 52, 71, 81 |
| §17.1–§17.4 pages, components, states | 26, 48, 49, 50, 69, 79 |
| §17.5 wording | 31, 51, 70, 80 |
| §17.6 shell and navigation | 27, 28, 29, 30 |
| §18 RBAC, chart, security | 7, 47, 65, 75; never-read tests 37, 56, 66 |
| §19 neutrality | 37, 78 |
| §21 testing | each code's task; e2e 53, 72, 82, 86 |
| §22 decisions | D-027 in 14; D-028–D-038 at approval (Q3) |
| `reviews/design-codex.md` triage | 1 → 19; 2, 13 → 18, 45; 3 → 35; 4 → 40–42; 5 → 65–66; 6 → 57–58; 7, A2 → 35, 37, 38; 8 → 37–38; 9, 15 → 60–61; 10 → 57, 59, 61; 11 → Global Constraints, 35, 41, 75; 12 → 41; 14 → 6; budgets → 46 Step 3; A1 → 20 |
