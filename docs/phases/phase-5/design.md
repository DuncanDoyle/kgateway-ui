# Phase 5 — Generic Gateway API core: Design

**Status:** Draft for cross-model review and user approval. Written without a question round, from the roadmap, D-061, D-065 and a re-verified coupling audit. Calls marked (our call) are open to change.
**Date:** 2026-11-08
**Base:** `169712c`
**Roadmap:** `docs/roadmap.md`, Phase 5
**Decisions:** D-003, D-016, D-020, D-061, D-065 in `docs/decisions.md`. This design proposes D-066 … D-071 (§16).
**Code refs:** `file:line` at the base commit.

**One home per contract.** Packs and per-install dispatch: §4. Install identity: §5. Metrics: §7. Rule sets and view A: §8. Budgets: §14. The Phase 3 homes still hold: the check contract (Phase 3 design §16.2), collectors (§4.1), the Authorizer (§14).

## Decisions for this phase

| # | Decision | Marker | Where |
|---|---|---|---|
| 1 | The base is implementation-independent Gateway API. kgateway is pack #1, on the same seam later downstream editions use | (user, 2026-11-07) | §1, §4 |
| 2 | No multi-implementation support is claimed | (user, 2026-11-07) | §2 |
| 3 | A core-only build is checked in a private compatibility CI, outside this repo. This repo ships the core-only build and a generic smoke test it runs | (user, 2026-11-08; D-065) | §12 |
| 4 | Finding codes keep `KGWUI-` for every check, core and kgateway alike. No API break | (user, 2026-11-07) | §6 |
| 5 | One default binary, with the kgateway pack built in. `extension.New()` keeps meaning "core + kgateway" | (user, 2026-11-07) | §4, §11 |
| 6 | A pack is a set of per-install implementations, selected by the detector that claimed the install. Registry-wide fields stay as the defaults for old callers | (our call) | §4.2 |
| 7 | An install no pack claims is a **class-only install**: one per GatewayClass `controllerName`, with an opaque ID derived from it | (our call) | §5 |
| 8 | A class-only install assumes all namespaces in scope, labelled "assumed", and has no scope verifier | (user, 2026-11-08) | §5.3 |
| 9 | No package moves. kgateway code stays at its paths; a new `pkg/kgateway` assembles the pack; old exported names stay as wrappers | (our call) | §11 |
| 10 | Controller metrics become a kgateway-pack collector, a clean break: they leave `engine.Config.Metrics` and `health.Input.Metrics`, and checks read them only through `RunContext.Collected`. The types and the one-shot API stay in `pkg/ctrlmetrics` | (user, 2026-11-08) | §7 |
| 11 | The three rule sets share one spec-level view A in `pkg/computed/viewa`; each set keeps only its deltas | (our call) | §8.1 |
| 12 | An install without a rule set gets no computed view: status only, no KGWUI-011/012 | (user, 2026-11-08) | §8.2 |
| 13 | Delegation is an optional rule-set capability, detected by type assertion | (our call) | §8.3 |
| 14 | The Envoy layer (proxy stats, xDS rejects, Envoy names, proxy image detection) belongs to the kgateway pack. The core assumes no Envoy | (our call, D-065) | §4.3 |
| 15 | Condition types beyond Gateway API (`kgateway.dev/Programmed`) come from the pack, per install | (our call) | §9 |
| 16 | The SPA stays one bundle. kgateway copy and the delegation tab are contributions, shown per install from server-sent features | (our call) | §10 |
| 17 | The default binary shows class-only installs of other controllers only behind a flag, off by default | (user, 2026-11-08) | §5.4 |
| 18 | No budget or gate changes. Each sub-phase shows it stays within the Phase 3 gates | (user, 2026-10-29, carried) | §14 |
| 19 | The next tag is `v0.6.0`. It breaks the Go API (metrics, `computed.Rules`), with README upgrade notes; API v1 stays additive | (our call, after the user's metrics decision) | §7, §11 |

## 1. Intent and why

Snellius is a kgateway UI, and stays one. The goal is a cleaner product, not a second market: the core speaks only Gateway API, and kgateway is the first pack on the seam (D-061). That keeps kgateway semantics out of places they do not belong before Phase 7 adds policy semantics, and gives downstream editions the same seam.

The coupling audit of 2026-11-07, re-verified at the base:
- About 60 % of `pkg/` (about 24k of 39k non-test lines) is already generic Gateway API.
- The registry seams exist (`pkg/extension/extension.go:33-68`): kinds, detectors, scope, verifier, checks, Authorizer, proxy mode, rule sets, process source, proxy stats, commands, collectors, handlers, capabilities.
- What is hard-coded is wiring, not logic. The table below lists every hotspot.

| Hotspot | Where | What is kgateway-specific |
|---|---|---|
| Registry defaults | `pkg/extension/extension.go:173-190` | `New` wires kgateway kinds, detector, scope, checks, rule sets, proxy mode, commands |
| Kinds | `pkg/kinds/upstream.go:17-41`, `:43-107` | Gateway API and `gateway.kgateway.dev` kinds in one list; experimental-only table per kgateway minor |
| Detector | `pkg/install/upstream.go:11-80` | labels `kgateway=kgateway`, image repo `kgateway`, metrics port 9092 |
| Edition branches | `pkg/install/upstream.go:82-100` | `KgatewayVersion`, `ControllerVersion` branch on `Edition == "kgateway"` |
| Controller settings | `pkg/install/settings.go:10,35,43`, `scope.go:15,219-225` | `KGW_*` env vars read for every install in `Resolve` |
| Rule-set choice | `pkg/engine/engine.go:340` | `RulesFor(res.KgatewayVersion())` |
| Metrics | `pkg/health/finding.go:163`, `pkg/engine/engine.go:51,338-389,404,412`, `pkg/engine/loop.go:74-78,222-227`, `pkg/api/details.go:331`, `pkg/setup/setup.go:162` | `Input.Metrics` is the concrete `ctrlmetrics.Result` of `kgateway_*` families; the engine reads it for every install, for the verifier, the reported version and the model's freshness |
| Condition type | `pkg/explore/rows.go:28`, `pkg/health/checks.go:217`, `pkg/health/rules.go:62` | `kgateway.dev/Programmed` |
| Proxy detection | `pkg/process/kube.go:22` | proxy Pods found by the `envoy-wrapper` image |
| Collectors | `pkg/app/app.go:255-298` | the metrics scraper and the Envoy proxy stats collector start for every build |
| Rule sets | `pkg/computed/kgw23`, `kgw24`, `kgw25` | about 2.7k non-test lines each, 8.2k in all; 24→25 differ in about 110 lines, 23→24 in about 160 |
| Delegation | `pkg/install/assign.go:257`, `pkg/api/details.go:620-760` | always on, kgateway semantics |
| Web copy | `web/packages/shell/src/shell/vocabulary.tsx:30-33`, `explore/ComputedBadge.tsx:82-85`, `shell/ObjectHeader.tsx:85`, `pages/ResourcePage.tsx:33`, `pages/AboutPage.tsx:136` | kgateway names, group and labels |
| Delegation tab | `web/packages/shell/src/pages/explore/RoutePage.tsx:15` | a core page imports `DelegationTab` |
| Delegation semantics in core | `pkg/objects/httproute.go:279,313`, `pkg/health/refs.go:79,291`, `pkg/explore/admit.go:125` | backendRefs to HTTPRoutes and `delegation.kgateway.dev` read as delegation |
| Policy and Backend semantics | `pkg/install/assign.go:329`, `pkg/health/refs.go:182`, `pkg/health/backend.go:54`, `pkg/api/details.go:635` | kgateway policy ownership, ListenerPolicy CA refs, Backend judgement |
| Experimental kinds | `pkg/kinds/upstream.go:46-107`, called at `pkg/health/checks.go:577`, `pkg/api/details.go:441`, `pkg/engine/views.go:105` | an unknown version gets kgateway's conservative classification |
| Pack findings inside a core check | `pkg/health/checks.go:328` (015), `:450` (011), `:465` and `pkg/health/targets.go:87` (016, 012) | `conditionsCheck` emits pack-owned codes |
| Nil workload | `pkg/health/checks.go:169,202` | `controllerCheck` dereferences the install's Deployment |

**Treatment of kgateway semantics inside core packages (our call, Codex review).** Three kinds:
1. **Gated per install.** Delegation interpretation (decode stays; its use in refs, admission and assignment runs only when the install's rule set is a `Delegator`), the pack-owned branches of `conditionsCheck` (run only through the install's `Delegator`, `PolicyTargeter` and `ConditionTypes`, with today's finding IDs and order), and experimental-kind advice (a new `Impl.ExperimentalOnly` hook; nil gives no experimental advice).
2. **Inert without pack kinds.** Policy ownership, ListenerPolicy CA refs and Backend judgement act only on `gateway.kgateway.dev` kinds, which only the kgateway pack registers. They stay where they are in Phase 5; the Backends (6) and Policies (7) phases move them behind pack hooks when they rework those areas. Tracked as a carry-over.
3. **Fixed in 5.1.** Every check that reads the install's workload handles a nil Deployment as "not applicable".

## 2. Goals, non-goals and anti-goals

**Goals**
1. A core that runs with no pack: installs, scope, explorer, health and the SPA work for any controller that writes Gateway API status (§4, §5).
2. kgateway as a pack: everything in the hotspot table moves behind it, with no change in what a kgateway user sees (§4, §11).
3. One spec-level view A, shared by the rule sets (§8.1).
4. Delegation as an optional capability, server and SPA (§8.3, §10).
5. What the private compatibility CI needs: a core-only build and a generic smoke test (§12).
6. No kgwctl break: every name kgwctl imports keeps working (§11).

**Non-goals**
- Support for any implementation other than kgateway.
- A pack for any other implementation.
- Running the compatibility check in this repo's CI (D-065).
- Physical package moves or a `pkg/core` tree (§11).
- Renaming finding codes or the `UpstreamPrefix` constant.
- New checks or new views.

**Anti-goals**
- **False findings on other controllers.** A class-only install never runs a check that reads kgateway semantics, metrics or Envoy stats. Where the core cannot know, it says "not available for this controller", never red.
- **Core code that names kgateway.** Enforced by a test (§13), not by review alone.
- **A plugin runtime.** Packs are Go code linked at build time (D-003). No dynamic loading.
- **A slower or bigger UI.** Dispatch runs per install or per stage, never per object (§14).

## 3. Primary flows

1. **A kgateway user upgrades to v0.6.0.** Nothing changes: same installs, IDs, findings, pages and delegation tab.
2. **The core-only build on a cluster with another controller.** One install, `example.com~2Fgateway`, labelled "Gateway API controller `example.com/gateway`". Gateways, listeners, routes and their status show. Health uses status findings (002, 003, 013, 014, 017, 023). The data plane section says "Not available for this controller".
3. **A downstream edition.** Its `main` calls `extension.New(downstream.Pack())`. Its detector claims its controller first; the kgateway pack keeps claiming plain kgateway (D-003).

## 4. Architecture

```
 extension.New(opts...) = NewCore(kgateway.Pack(), opts...)
                │
   core: kinds (Gateway API) · class-only installs · status checks · explorer · API · SPA
                │  per install: Impl = the pack whose detector claimed it ("" for class-only)
   pack kgateway: kinds · detector · scope · verifier · settings · rule sets (+delegation)
                  · proxy mode · process · proxy stats (Envoy) · metrics collector · checks
                  · condition types · commands · web contributions
```

### 4.1 Core and pack boundary per package

| Package | Core | kgateway pack | Change |
|---|---|---|---|
| `pkg/cache`, `pkg/objects`, `pkg/trigger`, `pkg/collect`, `pkg/platform`, `pkg/overview`, `pkg/diagnose` | all | — | none |
| `pkg/kinds` | Gateway API descriptors, categories, `Resolve`, `Monitor` | `gateway.kgateway.dev` descriptors, experimental-only table | split `Upstream()` into `GatewayAPI()` + `kgateway` list; `Upstream()` stays as both |
| `pkg/install` | `Install`, `Detector`, `Discover`, `Assign`, IDs, `Scope`, class-only installs | `UpstreamDetector`, `NamespaceSelectorScope`, `ReadSettings` | `Install.Impl`; settings per pack; delegation hook |
| `pkg/computed` | interfaces, `viewa` | `kgw23`, `kgw24`, `kgw25`, `rulesets` | dedupe (§8.1); `Delegator` optional |
| `pkg/health` | status, scope, refs, conflicts, endpoints, process, freshness checks | 001, 007, 010, 011, 012, 015, 016, 022, 024, 025, 026 | `Core()` + `Kgateway()`; `Upstream()` stays as both |
| `pkg/ctrlmetrics`, `pkg/ctrllogs`, `pkg/proxystats`, `pkg/scopeverify`, `pkg/proxymode` | — | all | started only by the pack |
| `pkg/process` | `Source`, `Set`, controller set from the install's workload | proxy Pods by image (`kube.go:22`) | proxy discovery per pack |
| `pkg/engine` | scheduling, per-install dispatch | — | reads `Impl` per install |
| `pkg/explore`, `pkg/api` | all | condition types, delegation endpoints | per-install features |
| `pkg/extension` | `NewCore`, options, `Pack` | — | §4.2 |
| `pkg/kgateway` (new) | — | `Pack()` assembles the above | new |
| `pkg/app`, `pkg/setup` | wiring | — | collectors from packs |

### 4.2 Packs and per-install dispatch (our call)

Today the registry has one `Scope`, one `RulesFor`, one `ProxyMode` for every install. With class-only installs next to kgateway installs, these must vary per install. A pack bundles them:

```go
// Pack is one implementation's contributions; extension.NewCore applies packs in order.
type Pack struct {
	Name      string             // "kgateway"; stamped on each install its detector claims
	Kinds     []kinds.Descriptor
	Detectors []install.Detector
	Impl      Impl               // per-install hooks, used for installs this pack claimed
	Checks    []health.Check     // run only on installs this pack claimed (§6)
	Collectors []func(CollectorEnv) Collector // built by the app with clients, options, clock and installs (§7)
	Features  []string           // per-install feature names sent to the SPA (§10)
}
```

`Impl` holds the per-install hooks that the registry holds globally today: `Scope`, `Verifier`, `Settings`, `RulesFor`, `ProxyMode`, `Process`, `ProxyStats`, `Commands`, `ConditionTypes`, `Version` (the install's implementation version for rule-set choice). The existing global `With…` options still work: they set the hook for installs of the pack that is being built (the default pack in `New`), so a downstream that calls `WithComputedRules` today keeps its behaviour.

**Old callers (our call).** Detectors added with `WithDetectors`, and hooks set by the old `With…` options or by assigning `Registry` fields, belong to the default pack: `kgateway` in `New`, `core` in `NewCore`. `ImplFor` reads those fields when the app is built. So a downstream detector keeps today's scope, rule sets, checks and process hooks, and `examples/downstream` proves it through engine output.

`Pack` lives in `pkg/extension`. `pkg/kgateway` returns plain values and does not import `extension`, so `extension.New` can import `kgateway` without a cycle.

**Revisit after the Headlamp comparison:** the pack model and where its frontend half lives (§10). A separate research task is comparing Headlamp's plugin model; it may change the shape of `Pack`, not the boundary.

### 4.3 The Envoy layer (D-065)

A controller in the private check may run a data plane that is not Envoy. So the core must not assume Envoy anywhere. Proxy stats (`:9091/stats`), the xDS reject metric, `computed.EnvoyNamer`, the `envoy-wrapper` proxy image and Envoy commands in next steps all belong to the kgateway pack. For a class-only install:
- the proxy stats and controller logs collectors do not run for it;
- KGWUI-001, 024, 025 and 026 are not registered for it, so they neither fire nor show "not available" rows;
- the Gateway page's data plane section shows one line, "Not available for this controller", from the absent `dataplane` feature (§10).

## 5. Install identity for class-only installs

### 5.1 Discovery (our call)

After the detectors run, the core groups GatewayClasses by `spec.controllerName`. Each controller name that no detected install lists in `ControllerNames` becomes one class-only install. GatewayClasses and Gateways are watched cluster-wide today, so this adds no watch and no memory.

### 5.2 Identity (our call)

| Field | Value |
|---|---|
| ID | the controller name, tilde-escaped: every byte outside `A-Z a-z 0-9 . -` becomes `~` and two hex digits. `example.com/gateway` → `example.com~2Fgateway`. The encoding is injective, so two controller names never share an ID, and the result passes `ValidateID` (`pkg/install/id.go:12`) for every name Gateway API allows (its path part permits `:`, `_`, `/` and more). It cannot collide with a workload ID `<namespace>.<name>`, which never holds `~` |
| Display | "Gateway API controller `<controllerName>`" |
| Edition, Version | empty: unknown |
| Deployment, Pods | none: controller readiness (005) and process findings (019–021) are not available; the API's optional platform fields read "not applicable" (D-016) |
| ControllerNames | the one name |
| Impl | `""` (core) |

A pinned install (`--install`) accepts this ID like any other.

### 5.3 Default scope (user, 2026-11-08)

A class-only install has no scope provider, so the core assumes all namespaces. The scope reads `{mode: all, source: "assumed"}`. The scope check says "Scope assumed: all namespaces. Snellius cannot read this controller's discovery settings". No scope verifier runs, so KGWUI-007 is not available for it. The alternative, `unknown`, would force the install's health to unknown and turn view A off; since class-only installs have no view A anyway (§8.2), the cost of "unknown" is the verdict alone. We prefer "assumed": a route outside the controller's real scope then shows as not accepted (003), which is true from the user's side.

### 5.4 The default binary (user, 2026-11-08)

Class-only installs are new install rows. In the default binary they are off unless the chart value `installs.classOnly` (flag `--class-only-installs`) is on, so a kgateway user sees no new installs. The core-only build turns it on. The value is generic and names no implementation.

## 6. Checks and finding codes

All codes keep `KGWUI-` (user, 2026-11-07). A pack's checks run only on installs the pack claimed. The engine filters by `Impl` before it runs a check; the memo key already includes the install, so this costs nothing.

| Code | Owner | Why |
|---|---|---|
| 002, 003, 004, 008, 009, 013, 014, 017, 018, 023 | core | read Gateway API status, Services, EndpointSlices, scope |
| 005, 006, 019, 020, 021 | core | read the install's process data; not available when there is none |
| 001, 024 | kgateway | `kgateway_*` metrics |
| 007 | kgateway | `kgateway_resources_managed` verifier |
| 010 | kgateway | `GatewayParameters` |
| 011, 012 | kgateway | delegation and computed cross-check |
| 015 | kgateway | kgateway condition reason |
| 016 | kgateway | kgateway policy targets |
| 022 | kgateway | kgateway-to-Gateway-API table |
| 025, 026 | kgateway | Envoy proxy stats |

Most pack codes come from whole checks, which the engine filters by `Impl`. Four come from branches of the core `conditionsCheck` (011, 012, 015, 016, see §1). Those branches stay in the check and run through the install's `Delegator`, `PolicyTargeter` and `ConditionTypes`; a class-only install has none, so they emit nothing, and kgateway findings keep their IDs and order. The restart and rollout checks keep their optional controller-log enrichment: they read the key when it is there and do not need it.

`health.Upstream()` keeps returning every check, in today's order. New `health.Core()` and `health.Kgateway()` return the two halves. The check types stay in `pkg/health`: they share unexported helpers, and moving them would export many helpers for no user gain (our call).

## 7. Controller metrics behind the collector seam (user, 2026-11-08)

Today `app.go:255-298` builds the metrics scraper for every build. The engine reads one `ctrlmetrics.Result` per install (`engine.go:338`) and passes it into every check as `health.Input.Metrics`, into the scope verifier, into the reported version (`engine.go:412`) and into the model's freshness (`Model.MetricsRead`, `Source.Metrics`, `loop.go:74`).

**The user chose a clean break.** Controller metrics leave the engine and the check input. They are one collector value of the kgateway pack, read only through `RunContext.Collected`.

### 7.1 The collector (our call)

1. The kgateway pack's collector factory, `func(CollectorEnv) Collector`, wraps `ctrlmetrics.Collector`. `CollectorEnv` carries what `app.go:251-279` uses today: the clients, the app options, the clock, the installs function and the collected store. It scrapes only installs the pack claimed.
2. It publishes one value under `ctrlmetrics.Key` (`"controller-metrics/results"`): a `ctrlmetrics.Results` with one `Result` per install ID and the max age (3 × the interval), with the fingerprint of the content and **no store expiry**. Staleness stays per entry, from each scrape's `CollectedAt` through `Result.Stale` (`scrape.go:129`), as today: staggered scrapes keep their own age, and the API keeps serving the last snapshot after it is stale, as `details.go:331` does now (our call, Codex re-review).
3. One key for all installs keeps one collector contract. A scrape of one install changes the fingerprint for all, which recomputes only the metric checks of every kgateway install: per stage, not per object.
4. The collector names that `setup.go:125` reserves move to the pack; a downstream collector still cannot take them.

### 7.2 Readers (our call)

| Reader today | After |
|---|---|
| checks 001, 024 (`health/xds.go:118-133`, `withheld.go`) | declare `Collected: [ctrlmetrics.Key]`; read their install's entry |
| the scope verifier (`scopeverify.Input.Metrics`) | the input carries the collected snapshot; the kgateway verifier reads its entry |
| the reported version (`engine.go:412`) | the kgateway `Impl.Version(install.Install, collect.Snapshot)` hook reads `BuildVersion()` from the entry |
| the gateway data-plane detail (`api/details.go:331`) | reads the entry from `Model.Collected`, the collected snapshot the computation used, so it shows the same evidence as the checks |
| the model freshness (`Model.MetricsRead`, `Source.Metrics`) | `Model.Collected` (new) holds the snapshot read once per computation (`engine.go:313`); its fingerprints feed the source key |
| the memo print (`memo.go:103`) | collected keys, as for every collector |

Removed: `engine.Config.Metrics`, `engine.Config.MetricsMaxAge`, `engine.Model.MetricsRead`, `engine.MetricsRead`, `engine.Source.Metrics`, `health.Input.Metrics`, `HasMetrics`, `MetricsMaxAge`, `InputSet.Metrics`, `scopeverify.Input.Metrics`, `HasMetrics`, `ctrlmetrics.Source`, `ctrlmetrics.Static`, `Collector.Get`. The core-only build has no metrics at all.

### 7.3 The one-shot path (E8; our call)

The metric types and the one-shot API stay in `pkg/ctrlmetrics`. No package moves (row 9); the package now belongs to the kgateway pack. Kept: `Metrics`, `Parse`, `Result`, `Scraper`, `HTTPScraper`, `PodProxyScraper`, `NewCollector(...).Collect`. New:

```go
// CollectOnce scrapes each kgateway install once, synchronously.
func CollectOnce(ctx context.Context, s Scraper, insts []install.Install, maxAge time.Duration) Results
// Results is one scrape per install ID, with the age beyond which an entry is stale.
type Results struct { ByInstall map[string]Result; MaxAge time.Duration }
// Publish stores r under Key, with no store expiry, so the next Compute reads it.
func Publish(store *collect.Store, r Results)
```

`setup.Config` gains `Collected *collect.Store`, already wired to `engine.Config.Collected`. The documented one-shot use:
1. `setup.Build`, start the cache, and wait as the `setup` Go doc says today (`setup.go:11-17`): `Settled()`, then each optional watch and list-only kind OK or failed. Take one `Snapshot()`;
2. `install.Discover(snap, reg.Detectors)`, keep the installs with `Impl == "kgateway"`;
3. `ctrlmetrics.Publish(cfg.Collected, ctrlmetrics.CollectOnce(ctx, ctrlmetrics.PodProxyScraper{Kube: kube}, insts, setup.MetricsMaxAge(interval)))`;
4. set `cfg.Engine.Snapshots` to return that same snapshot, then `engine.New(cfg.Engine).Compute()` (it takes no argument, `engine.go:275`).

Go doc on `ctrlmetrics` and the README carry this sequence. A fixture test runs it (§13).

### 7.4 Core-only

The core-only build registers no metrics collector, and no core check reads `ctrlmetrics.Key`. A behaviour test runs every core check on a class-only install twice, with and without metrics, logs and proxy stats in the collected snapshot, and asserts equal findings (memoized recomputes included). That proves the core does not read them, which a check of declared inputs cannot.

## 8. Rule sets, view A and delegation

### 8.1 Dedupe (our call)

View A is spec-level Gateway API (D-020 (A)): sectionName and port, allowedRoutes namespaces and kinds, hostname intersection. The three sets copy it with small deltas: 2.4 and 2.5 add listener isolation (`kgw24/isolation.go`, `kgw25/isolation.go`).

New package `pkg/computed/viewa` holds the shared attachment rules, parameterised by the kinds each listener protocol allows. Listener isolation is not in `viewa`: it follows which plain-HTTP listeners kgateway merges onto one port (`kgw24/isolation.go:69`), which is data-plane behaviour. Each kgateway set applies it as an annotation after `viewa`, and keeps its own delegation (views B, C), policy targets and log parser (our call, Codex review). The per-version golden fixtures in `pkg/computed/testdata/kgateway-2.{3,4,5}` stay the acceptance test: they must pass unchanged before and after the move. Expected result: about 2k fewer non-test lines.

### 8.2 View A for unknown implementations (user, 2026-11-08)

A class-only install gets no rule set, so no computed view and no KGWUI-011 or 012. This is different from a rule set that only lacks delegation (§8.3): that one keeps view A, its cross-check and 012 from attachment. The SPA shows status only: listener attached-route counts and parentRef conditions, as on a Phase 1 page. Running `viewa` for an unknown controller would claim to know its attachment rules; a disagreement would then raise a warning we cannot stand behind (anti-goal).

### 8.3 Delegation as an optional capability (our call)

`computed.Rules` loses `Delegation` from the required interface. A new optional interface carries it:

```go
// Delegator computes views B and C; a rule set without it has no delegation.
type Delegator interface {
	Delegation(in DelegationInput) DelegationResult
}
```

The engine detects it by type assertion, as for `PolicyTargeter` today (`pkg/computed/targets.go:66`). Without it: `install.Assign` skips `delegate` (`assign.go:257`), the delegation endpoint answers 404 for that install, and the install's features omit `delegation` (§10). The kgateway sets implement it, so kgateway behaviour is unchanged.

Without `Delegator`, only delegation is off: view A, `CheckAttachment` (`pkg/engine/views.go:32`) and 012 from attachment stay.

**This is a breaking Go API change for callers (our call).** An out-of-tree `Rules` implementation keeps compiling, but a caller that holds a `computed.Rules` and calls `.Delegation` must assert `computed.Delegator` first. The constructors keep returning `computed.Rules`; in-repo callers and tests change. kgwctl does not import `computed`. The README upgrade note and the `v0.6.0` release note say so.

## 9. Condition types (our call)

Gateway API's own types (`Accepted`, `Programmed`, `ResolvedRefs`) are core. A pack adds implementation types through `Impl.ConditionTypes`:

```go
type ConditionTypes struct {
	RouteOptional []string // kgateway: "kgateway.dev/Programmed"
	RouteRejected []string // condition types whose False status rejects a rule (KGWUI-015)
}
```

`explore/rows.go:28`, `health/checks.go:217` and `health/rules.go:62` read the install's set instead of the constant.

## 10. Web contributions (our call)

The SPA stays one bundle, so one image serves every install. Per install, `InstallSummary` (`pkg/api/types.go:15`) gains a feature list (additive, optional in API v1): `delegation`, `computed`, `dataplane`, `processes`. The SPA renders a feature's UI only when the install has it.

| Today | Phase 5 |
|---|---|
| `vocabulary.tsx:30-33` "No kgateway install found", kgateway labels | core copy "No Gateway API controller found"; the kgateway pack's vocabulary adds the label hint |
| `ComputedBadge.tsx:82-85` parses `kgateway-2.4` | the server sends a display label with the rule-set ID |
| `ObjectHeader.tsx:85`, `ResourcePage.tsx:33` compare `gateway.kgateway.dev` | descriptor-driven: `kinds.CategoryBackend` from `/meta` |
| `AboutPage.tsx:136` "kgateway 2.5.0" | edition and version as sent; "Gateway API controller" for class-only |
| `RoutePage.tsx:15` imports `DelegationTab` | the kgateway web pack registers it as a detail-tab contribution, shown when the install has `delegation` |
| `RoutePage.tsx:168` delegation permission copy, child and effective-match blocks | shown only with `delegation` |
| `GatewayPage.tsx:103` data plane and proxy sections | with `dataplane` / `processes`; otherwise one line, "Not available for this controller" |
| computed badges and cross-check chips | only with `computed` |

The kgateway web pack lives in the shell package as `kgatewayWebPack`, merged into the default app's `ShellExtension`. `SHELL_API_VERSION` stays 1: the change is additive.

**Revisit after the Headlamp comparison:** whether a pack's frontend half should be a separately built module rather than a build-time merge.

### 10.1 Authorizer

Unchanged. Packs add no data the Authorizer does not already filter; class-only installs go through the same `api.Visible` paths. Per-user login stays deferred (D-013, Phase 12).

**Revisit after the Headlamp comparison:** per-user auth. Headlamp works with the viewer's own credentials; that comparison may move the D-013 trust model, not this phase's seam.

## 11. Package moves and kgwctl impact

No package moves (our call). Every name kgwctl imports at `kgwctl` `980d32d` keeps compiling, except the metrics names the user chose to break (§7):

| Package | kgwctl uses | Phase 5 | Impact |
|---|---|---|---|
| `extension` | `New`, `Registry`, `WithDetectors`, `WithKinds`, `WithCollectors`, `Deps` | `New` = `NewCore(kgateway.Pack(), …)`; `Registry` gains fields | none |
| `kinds` | `Upstream`, `KgatewayGroup`, categories, `Served`, `Resolve`, `ByCategory`, `Dedupe` | `Upstream()` returns the same list in the same order | none |
| `install` | `Install`, `Resolved`, `Owned`, `UpstreamDetector`, `ProxyPodDetector`, `ParsePinned` | `Install` gains `Impl`; `Resolved` gains optional fields | none (additive) |
| `cache`, `objects`, `explore`, `process`, `app` | as today | additive | none |
| `engine`, `setup`, `health` | `engine.Compute`, `setup.Build`, `health.ValidateOrder` | `engine.Config.Metrics`, `MetricsMaxAge`, `Model.MetricsRead`, `Source.Metrics` and `health.Input.Metrics` removed; `setup.Config.Collected` added | breaks only code that set or read metrics through them |
| `ctrlmetrics` | not imported at `980d32d`; the E8 contract documents `Static`, `NewCollector`, `PodProxyScraper` | `Source`, `Static`, `Collector.Get` removed; `CollectOnce`, `Publish`, `Results`, `Key` added | to get metrics in `check`: the §7.3 sequence instead of `cfg.Engine.Metrics = ctrlmetrics.Static{…}` |
| `computed` | not imported | `Rules` loses `Delegation` from the required set | none for kgwctl |

One behaviour change reaches kgwctl: `extension.NewCore()` exists, and a kgwctl that called it would see class-only installs. kgwctl does not call it.

**Cross-project communication (user, 2026-11-08).** When the metrics move merges (5.2), a plan task updates the E8 rows of the program file to the new names and files a `cross-repo` issue in kgwctl naming E8, the new names and the exact changes (§7.3). Snellius does not wait for kgwctl. The phase close sends the usual `v0.6.0` notice for E2, E3 and E8.

## 12. The private compatibility check (D-065)

The proof runs only in a private compatibility CI, outside this repo (user, 2026-11-08). Its workflow checks out Snellius, builds the core-only image, installs a controller on kind and runs the smoke test. This repo ships the two pieces it needs and names no implementation.

### 12.1 What this repo ships (our call)

| Piece | Where | What |
|---|---|---|
| Core-only build | `cmd/snellius-core`, `make image-core` | a `main` that calls `extension.NewCore()`, with class-only installs on |
| Generic smoke test | `test/compat`, build tag `compat` | asserts through API v1 against whatever cluster the kubeconfig names. Inputs come from env: the expected controller name, a Gateway and a route |

The smoke test asserts:
1. one install, whose ID is the tilde-escaped controller name (§5.2), and whose scope reads `all`/`assumed`;
2. the Gateway and route are listed, with their status, and the install verdict is not error;
3. `/meta` lists no `gateway.kgateway.dev` kind, and the install has none of the `delegation`, `computed`, `dataplane` features;
4. no finding with a kgateway-pack code (§6);
5. the default binary, with class-only installs off, does not claim the controller.

A Playwright spec (same tag) opens the install page and the route page and checks there is no delegation tab and no error panel.

### 12.2 What public CI runs

Nothing behavioural for the compatibility check (D-065). Public CI runs `go vet -tags compat ./test/compat/...` so the test keeps compiling, and builds the core-only image. Class-only behaviour is covered in-repo by unit and API tests over hand-written fixtures with a made-up controller name (§13).

### 12.3 What the private check must cover (our call)

- A controller with a data plane that is not Envoy, so a leaked Envoy assumption shows (§4.3).
- Its Gateway API minor matched to the controller's own `go.mod`, as the kgateway matrix does.
- The caveat its results carry: a controller that shares code ancestry with kgateway may write status the way kgateway does. Such a check proves the core assumes no kgateway names, kinds, metrics or Envoy data plane; it proves less about independence from kgateway's status semantics. The private CI should include an unrelated implementation too.

The private repo exists (user, 2026-11-08); its workflows are a 5.6 task, written there. A failure there is reported by hand, never as an automated issue in this repo.

## 13. Testing

| Test | Boundary | Asserts |
|---|---|---|
| Core boundary (`internal/boundary`) | per file | files that may import pack packages (`ctrlmetrics`, `ctrllogs`, `proxystats`, `scopeverify`, `proxymode`, `computed/kgw*`, `rulesets`, `pkg/kgateway`) are listed with a reason, like today's allowlist; the list only shrinks. The compatibility surface of §7 and §11 is listed explicitly |
| Core checks ignore pack data | `engine.Compute` on a class-only install | equal findings with and without metrics, logs and proxy stats, including memoized recomputes (§7) |
| E8 one-shot path | the §7.3 sequence over a fixture with a rejecting controller | KGWUI-001 appears with the same identity as the long-running path; without `Publish`, 001 is "not available" |
| Class-only install | `install.Discover` over a snapshot with two controllers | IDs, display, assumed scope; a controller claimed by a detector gets no class-only twin |
| Pack dispatch | `engine.Compute` with kgateway and class-only installs | each install runs only its checks; findings for kgateway installs are byte-equal to the base |
| Golden API v1 | `pkg/api/testdata/golden-v1` (frozen), `testdata/golden` (current) | `golden-v1` untouched and `additive_test.go` passes; current goldens gain only optional fields; new current goldens for a class-only install |
| Rule-set goldens | `pkg/computed/testdata/kgateway-2.{3,4,5}` | pass unchanged across the `viewa` move |
| Delegation off | API with a rule set without `Delegator` | 404 on the delegation endpoint; no `delegation` feature |
| `examples/downstream` | as in Phase 3 | builds and passes with no change to its code |
| kgwctl compile check | CI job building kgwctl `main` against this branch through a `go.work`; informational (`continue-on-error`), not required | reports whether kgwctl compiles. It may fail by design until kgwctl adapts to the metrics break; it never blocks a merge here |
| e2e kgateway matrix | kind, 2.3, 2.4, 2.5 | unchanged |
| Compat test compiles | `go vet -tags compat` | §12.2 |
| SPA | Vitest | delegation tab only with the feature; neutral empty-state copy; backend kind by category |

## 14. Performance and budgets

No gate changes; those are the user's call (Phase 3 design §16.3). Expectations:
- Dispatch reads `Impl` once per install per recompute. No per-object hooks.
- Class-only discovery walks GatewayClasses, already in the snapshot.
- The core owns the shared cache; packs add no informers (D-061). The kgateway pack's collectors are the ones that exist today.
- `viewa` replaces copies; it must not add allocations. The chain benchmarks in `kgw2x/chain_bench_test.go` gate it.

Each sub-phase runs the bench gates (warm recompute ≤ 150 ms, stream CPU, retained heap ≤ 48 MiB, dense memory) and records the numbers in its complete file. Any break stops the work and goes to the user.

## 15. Sub-phases

| Sub-phase | Roadmap | Scope | Ends with |
|---|---|---|---|
| 5.1 | G1 | `Pack`, `Impl`, `NewCore`, `app.RunWith`; `Install.Impl`; class-only installs, IDs, assumed scope; nil-workload handling; settings and experimental kinds per pack; `kinds` split; `cmd/snellius-core` | kgateway goldens unchanged |
| 5.2 | G2 | metrics and proxy stats collectors from the pack; the metrics clean break and one-shot API (§7); checks split; per-install check filter; program file E8 rows and the kgwctl `cross-repo` issue | — |
| 5.3 | G3 | `viewa`; rule-set dedupe; `Delegator`; delegation off path | rule-set goldens unchanged; bench |
| 5.4 | G4 | condition types, proxy mode, process proxy discovery, commands and verifier per pack; core boundary test with the string allowlist | — |
| 5.5 | G5 | install features in API v1; web vocabulary; delegation tab as contribution; badge and backend by descriptor | Vitest, Playwright |
| 5.6 | G6 | the generic smoke test and core-only image (§12); the private compatibility CI's workflows, written outside this repo; the kgwctl compile job; docs and README | `v0.6.0` proposed |

## 16. Proposed decisions

- **D-066** Packs with per-install dispatch; `extension.New` = core + kgateway (§4.2).
- **D-067** Class-only installs: ID, display, assumed scope, off by default in the default binary (§5).
- **D-068** Controller metrics as a kgateway-pack collector, read only through `RunContext.Collected`; types and one-shot API in `pkg/ctrlmetrics` (§7; user, 2026-11-08).
- **D-069** Shared view A; no computed view for an install without a rule set (§8.1, §8.2).
- **D-070** Delegation as an optional `Delegator` (§8.3).
- **D-071** The Envoy layer belongs to the kgateway pack (§4.3).

## 17. Open questions for the user

None. Answered 2026-11-08: class-only scope (row 8), metrics (row 10), view A for other controllers (row 12) and the default binary (row 17).
