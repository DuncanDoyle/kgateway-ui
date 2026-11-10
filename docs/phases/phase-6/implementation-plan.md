# Phase 6 — Traffic Flow & Backends: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Answer "where does this traffic go, and is it getting there?" with a path list, a route trace, a Gateway map and backend pages, all on one path model, plus kgwctl's `ListOnce` and namespace-scoped cache inputs.

**Architecture:** A new core package `pkg/paths` derives a topology (attachment × rule × backendRef rows, with dependency sets) from view A, the Delegator's view C and decoded rules, memoised per install and snapshot; a health projection maps each row to its problem from existing findings, conditions and capability states, memoised per publication. `pkg/api` pages, sorts and authorises the projected rows per request. Backend detail adds a bounded on-demand EndpointSlice read behind a neutral `endpoints.ScopedReader`. Backend judgement and GatewayExtension consumers sit behind the kgateway pack.

**Tech Stack:** Go, React 19/TypeScript (Vitest, Playwright), `@xyflow/react` (new, lazy chunk), Helm, kind.

**Spec:** `docs/phases/phase-6/design.md` (proposes D-070 … D-077). Section refs below (§n) are to it.

## Global Constraints

- API v1 additive only: new fields optional, nothing changes meaning. The frozen `pkg/api/testdata/golden-v1` files stay byte-equal; current `golden` files gain only new optional keys (`additive_test.go` passes).
- Every exported Go name kgwctl imports keeps compiling with today's meaning; `health.BackendState(b) (State, *metav1.Condition)` keeps its signature. `cache.ListOnce` and `Config.Namespaces` are additive.
- No new finding codes (`KGWUI-` set unchanged). Phase 6 adds views, not checks (§4.5).
- No new informers. No Pod watch. Named targetPorts are not resolved to container ports.
- D-057 retained heap is a hard gate: +48 MiB over the 2.1 baseline (worst at 5.6: +40.0). An overshoot shrinks retained state first (drop the topology memo, or derive GRPC/TCP/TLS rules on request), then asks the user before any raise. Other bench gates unchanged.
- New request gates at Large (starting targets, tuned in 6.2): paths cold < 200 ms and < 32 MiB allocated, warm < 20 ms and < 2 MiB; backend detail < 4 MiB allocated, scoped read included.
- Public repo hygiene: none of the vendor or commercial edition terms on the public-repo-hygiene list, and no other Gateway API implementation named, in code, comments, file names, commits or PR text.
- Comments 1–2 lines. Tests assert observable behaviour at the highest practical boundary: API responses, engine output, Playwright for UI.
- All SPA copy goes through a wording pass (`docs/phases/phase-6/phase-6.N-wording.md`, the format of `phase-3/phase-3.5-wording.md`, ids from S599 on, five options each, a sub-agent picks the least machine-sounding accurate one).
- Each sub-phase ends with `make bench` (output to `docs/phases/phase-6/bench-6.N.txt`), the e2e matrix and a demo reel (1920×1080 capture, 1600×900 output, one part, < 10 MB).
- Gates: `make test`, `make lint`, `cd web && npm test && npm run lint && npm run typecheck`, `make bench`, `make e2e` for the CI pairs v2.4.7/v1.6.1, v2.3.11/v1.5.1 and v2.5.0/v1.7.0 (`.github/workflows/e2e.yaml:26-29`).

## Review Focus

1. A rule mixing a delegation ref and ordinary refs never shows a share; a rule whose weights are all 0 shows "0 %, no traffic" on every ref (Task 26).
2. With the endpoints capability off, denied or failing, a Service row is "unknown" and the summary says endpoints were not read; never "0 ready" and never healthy (Tasks 27, 29).
3. A restricted viewer: hidden rows leave the page, the counts and the filter options; cursors stay stable across pages; a memo hit is authorised again (Tasks 28, 29, 36).
4. A route whose status is older than its spec reads "waiting for the controller", never "failed", on paths, the trace and reference explanations (Tasks 27, 37).
5. A multi-port Service: a path row's ready/total equals what KGWUI-023 counts for that same port, so a row never contradicts a finding (Task 27).

---

## Sub-phase 6.1: cache access for kgwctl (§12)

### Task 19: `cache.ListOnce`

**Files:** new `pkg/cache/listonce.go`, new `pkg/cache/listonce_test.go`, `pkg/cache/cache.go` (Snapshot `Mode`; shared snapshot assembly with `Snapshot()` at `:619-675`), `internal/boundary/imports_test.go` (allow `pkg/cache/listonce.go`: "the list-only Kubernetes provider").

**Interfaces:**
```go
const ModeWatch, ModeList = "watch", "list"
type Snapshot struct{ …; Mode string } // "watch" from Cache.Snapshot, "list" from ListOnce
func ListOnce(ctx context.Context, cfg Config) (*Snapshot, error)
```
Same inputs as `Start`: served kinds, Namespaces, label-selected controller/proxy Pods and Deployments, and the optional facts `cfg` enables (Services, EndpointSlices, Events, CRDs), with the same selectors, transforms, sorting and fingerprints. `cfg.Changes` is ignored.

- [ ] **Step 1:** Failing tests over fake dynamic/kube clients loaded with the 5.x fixture world:
  - `TestListOnceMatchesCache`: `ListOnce` and a started `Cache` (after `Synced`) give equal `Objects`, `Services`, `EndpointSlices`, `ProxyPods`, and `Source.Equal` is true; `Mode` is `list` and `watch`.
  - `TestListOnceAccess`: a 403 on one kind → `Access[gvr]` denied and `Synced` true; a 500 → failed and `Synced` false; no watch call is made (fake records no `watch` verb).
  - `TestListOnceErrors`: cancelled ctx → non-nil error; a Config with nil `Dynamic` → error; a partial result never returns an error.
  - `TestListOnceTakenAt`: `TakenAt` equals `cfg.Now()` after the last LIST.
- [ ] **Step 2:** Implement, sharing the transform and sort code with the informer path (no copy of it). `go test ./pkg/cache/...` passes; `make bench` cache budgets unchanged.
- [ ] **Step 3:** Commit `feat(cache): ListOnce fills a snapshot from LISTs only (#18)`.

### Task 20: namespace-scoped inputs

**Files:** `pkg/cache/cache.go` (Config, New, Start, walk), `pkg/cache/listonce.go`, `pkg/cache/services.go`, `pkg/cache/fingerprint.go`, `pkg/engine/engine.go`, `pkg/install/scope.go`, tests in each.

**Interfaces:**
```go
type Config struct{ …; Namespaces []string } // nil or empty: cluster-wide
type Snapshot struct{ …; RequestedNamespaces []string; NamespaceAccess map[schema.GroupVersionResource]map[string]Access } // Namespaces []*corev1.Namespace (cache.go:68) is unchanged
const install.SourceNotObserved = "controller not observed"
```
Deduplicated and sorted at `New`/`ListOnce`. Namespaced served kinds, Services, EndpointSlices and selected Pods/Deployments get one informer (or LIST) per namespace; GatewayClass and Namespace are cluster-wide. `Access[gvr]` is the aggregate: ok only when every namespace is ok, else the worst (failed > denied > syncing). Fingerprints include `NamespaceAccess`.

- [ ] **Step 1:** Failing tests: `TestNamespacesInformerPerNamespace` (two namespaces, objects outside them absent, cluster-scoped kinds present); `TestNamespaceAccessAggregate` (denied in one namespace → aggregate denied, detail lists each; fingerprint differs from the all-ok case); `TestNamespacesNilUnchanged` (nil gives today's snapshot, `NamespaceAccess` nil, fingerprint equal to the base); the same two cases through `ListOnce`.
- [ ] **Step 2:** Engine tests: `TestDeniedNamespaceIsUnknown` (Services denied in namespace `b` only: a route's Service backend in `b` gets KGWUI-023 "not available", never healthy; one in `a` is judged as today); `TestClassOnlyNotObservedWhenScoped`: with `Snapshot.RequestedNamespaces` set and class-only installs on, a GatewayClass whose controller is not seen gives an install with scope source `controller not observed`; without `RequestedNamespaces` it stays `assumed`.
- [ ] **Step 3:** Implement; `make test`, `make bench`. Commit `feat(cache): namespace-scoped inputs (#19)`.

### Task 21: 6.1 close

- [ ] **Step 1:** Append D-070 … D-077 (§15) to `docs/decisions.md` as approved; README "Library use" gains `ListOnce` and `Namespaces` with a 5-line example each.
- [ ] **Step 2:** Bench → `bench-6.1.txt`; e2e matrix; a new e2e Go test `TestListOnceLive` (tag `e2e`, `test/e2e/api_test.go`) asserts `ListOnce` over the kind cluster has, per served kind, the same object count as an independent LIST of that kind through the dynamic client in the test.
- [ ] **Step 3:** Demo reel: a terminal recording of `TestListOnceLive -v` and the namespace-scoped test output.
- [ ] **Step 4:** Propose the next minor tag to the user; after it, comment on kgwctl #18 and #19 (ask before the first post) and update the program file's E2 row. Commit `docs: close sub-phase 6.1`.

---

## Sub-phase 6.2: path model, paths API, path list (§4, §5, §13, §14)

### Task 22: decode GRPCRoute, TCPRoute and TLSRoute rules

**Files:** `pkg/objects/backend.go` (`RouteView`, `DecodeRoute`), new `pkg/objects/grpcroute.go`, `pkg/objects/route_test.go`.

**Interfaces:**
```go
type RouteView struct{ …; Rules []Rule } // additive; Targets unchanged
type GRPCMethodMatch struct{ Type, Service, Method string } // Type defaults to "Exact"
type GRPCMatch struct{ Method *GRPCMethodMatch; Headers []KeyMatch }
type Rule struct{ …; GRPCMatches []GRPCMatch } // set for GRPCRoute only
```
GRPCRoute: matches, filters (RequestMirror included), backendRefs with port, weight and backendRef filters. TCPRoute/TLSRoute: backendRefs with port and weight, no matches. Weight default 1 with `WeightDefaulted`, as `decodeBackendRef`.

- [ ] **Step 1:** Failing tests: `TestDecodeGRPCRules` (method service/method/type, header matches, two weighted refs, a mirror); `TestDecodeTCPTLSRules` (ports, weights, `Matches` nil); `TestRouteBackendTargetsUnchanged` (Targets equal to today's for every fixture).
- [ ] **Step 2:** Implement; `pkg/computed` goldens unchanged; `make bench` (retained heap recorded). Commit `feat(objects): decode rules of every route kind`.

### Task 23: scale fixtures with multi-rule, multi-ref and delegated paths

**Files:** `internal/testutil/scale/scale.go` (`rules`, `:632-654`; `Worst()` at `:75`), `internal/testutil/scale/scale_test.go`.

**Interfaces:** `Tier.MultiRulePct, MultiRefPct int` (0 in Typical; set in `Large.Worst()` and `Extreme`); a multi-rule route has 3 rules, a multi-ref rule 3 weighted refs, one in ten with a weight-0 ref and one in twenty mixing a delegation ref with an ordinary ref.

- [ ] **Step 1:** Test `TestWorstPathShapes`: the worst Large world has routes with ≥ 3 rules, rules with ≥ 3 refs, mixed delegation rules, and GRPC/TCP/TLS routes with weights; the row count (Σ attachments × Σ rules × max(1, refs) + delegated) is logged. Engine goldens over `Large` (non-worst) unchanged.
- [ ] **Step 2:** Implement; commit `test(scale): multi-rule, multi-ref and delegated routes`.

### Task 24: `pkg/paths` topology

**Files:** new `pkg/paths/topology.go`, `pkg/paths/topology_test.go`, `internal/boundary/imports_test.go` (`pkg/paths` is core: no `pkg/kgateway`, `computed/kgw*`).

**Interfaces:**
```go
type Action uint8 // ActionBackend, ActionRedirect, ActionDirectResponse, ActionNone
type Row struct { /* indexes only, ≤ 48 bytes */ }
type Input struct {
	Install  install.Resolved
	Owned    *install.Owned
	Index    *explore.Index
	Views    *computed.Views     // view A; Delegation set only with a Delegator
	Decode   objects.Decoder
	Delegates bool               // the selection has a Delegator
}
type Topology struct{ Rows []Row; Refs []objects.Ref /* dependency table */ ; … }
func Build(in Input) *Topology
func (t *Topology) Bytes() int // retained size estimate for the 2 MiB cap
// accessors used by projection and the API
func (t *Topology) Gateway(r Row) objects.Ref; ListenerSet(r Row) *objects.Ref; Listener(r Row) Listener
func (t *Topology) Hostnames(r Row) []string; Chain(r Row) []objects.Ref; Rule(r Row) *objects.Rule
func (t *Topology) Backend(r Row) (objects.BackendRef, bool); Mirror(r Row) (objects.MirrorFilter, bool)
func (t *Topology) Action(r Row) Action; Deps(r Row) explore.DepSet
```
One row per (view A attachment, rule, backendRef); a rule with no backendRef is one row with its written action (RequestRedirect → redirect; an `extensionRef` filter to a DirectResponse → direct response; else none); a mirror is a row with `Mirror`. A ListenerSet attachment carries the ListenerSet and projects to its parent Gateway. `Deps` index `Topology.Refs`.

- [ ] **Step 1:** Failing tests over small fixtures: `TestRowsPerAttachmentRuleRef` (2 attachments × 2 rules × {2, 0} refs → 6 rows, the 0-ref rule shows its action); `TestListenerSetProjects`; `TestMirrorRow`; `TestHostnamesAsList` (one row, all effective hostnames); `TestAllRouteKinds` (GRPC/TCP/TLS rows with port and weight); `TestRowSize` (`unsafe.Sizeof(Row{}) <= 48`); `TestDepsCoverChain` (deps contain Gateway, ListenerSet, every route of the chain, backend).
- [ ] **Step 2:** Implement; commit `feat(paths): topology rows from view A and decoded rules`.

### Task 25: delegated rows from view C (§4.2)

**Files:** `pkg/paths/delegation.go`, `pkg/paths/delegation_test.go`.

**Interfaces:** `Row` gains a delegation state; `func (t *Topology) Delegation(r Row) DelegState` with `DelegState{Provenance []objects.Ref; Matches []objects.Match; Unknown computed.UnknownReason; Unconfirmable, NotComputed, Truncated bool}`.

- [ ] **Step 1:** Failing tests using the kgw25 Delegator on the delegation fixtures: a child with no Gateway parent gets rows under its root's attachment, with the chain root → child; its match is the view C context, not its written rule; `ContextsTruncated` gives a row with `Truncated` ("more paths not computed"); an unknown/unconfirmable view C row marks the path row; with `Delegates=false`, a backendRef to an HTTPRoute is an ordinary ref row of kind HTTPRoute and there are no child rows.
- [ ] **Step 2:** Implement through `Views.Delegation.Rows` contexts only (no new evaluator); commit `feat(paths): delegated paths from view C`.

### Task 26: share (§4.4)

**Files:** `pkg/paths/share.go`, `pkg/paths/share_test.go`.

**Interfaces:**
```go
type ShareState string // "shown", "not-shown", "no-traffic", "failing", "may-fail", "mirror"
func Shares(rule *objects.Rule) []Share // index-aligned with rule.BackendRefs
type Share struct{ Percent float64; State ShareState }
func (s Share) WithAttribution(attributed, candidate bool) Share // failing / may-fail
```

- [ ] **Step 1:** Failing tests: weights 1/3 → 25 %/75 %; defaulted weights equal shares; all weights 0 → every ref `no-traffic` 0 %; a rule with a delegation ref and an ordinary ref → every ref `not-shown`; `WithAttribution(true, true)` → `failing`; `(false, true)` → `may-fail` keeping the written percent; a mirror row → `mirror` with its percent.
- [ ] **Step 2:** Implement; commit `feat(paths): share within a rule`.

### Task 27: health projection (§4.5, §9.1, §9.2)

**Files:** `pkg/paths/project.go`, `pkg/paths/project_test.go`, `pkg/health/endpoints.go` (export the per-port count), `pkg/health/endpoints_test.go`.

**Interfaces:**
```go
// pkg/health — the KGWUI-023 count, shared; 023's findings unchanged
func PortEndpoints(svc *cache.ServiceFact, slices []*cache.SliceFact, port int32) (ev *EndpointsEvidence, anyReady bool)
// pkg/paths
type Signal struct{ Kind string; Code string; Ref objects.Ref; State health.State } // Kind: gateway, listener, route-status, waiting, finding, delegation, endpoints, coverage
type Problem struct{ State health.State; Signals []Signal }
type Endpoints struct{ Complete string; Ready, Total *int; ReadAt time.Time; Port int32; PortName string }
type ProjectInput struct{ Model *engine.InstallModel; Next engine.Deadline /* engine.Model.Next of the publication */; Snapshot *cache.Snapshot; Now time.Time; EndpointsCap string /* on, off, denied, failing */ }
type Projection struct{ Problems []Problem; Shares []Share; Endpoints []*Endpoints; Order []int32 /* problem.asc */ ; Next engine.Deadline }
func Project(t *Topology, in ProjectInput) *Projection
```
Problem = worst along the chain: error > warning > unknown > healthy; unknown never hides an error. Sources: Gateway `Accepted`/`Programmed`, listener conditions, Gateway findings (xDS rejects, proxy readiness/disconnects); the parent status for this attachment (`Accepted`, `ResolvedRefs`), stale/missing status → `waiting` (D-014); route findings; view C state; KGWUI-002, 013/014 only when attributed to this ref, 023 for this backend port; coverage unknown when the endpoints capability is not `on`. Backend-resource refs: endpoints `not-applicable`, state from the Backend's conditions. `Order`: problem, then Gateway, listener, route, rule. `Next`: copied from `ProjectInput.Next` (the publication's `engine.Model.Next`, `pkg/engine/engine.go:156`), with its strict-expiry meaning: the projection is not reused at or after it.

- [ ] **Step 1:** `TestPortEndpointsMatches023`: over the 3.4 endpoint fixtures, `PortEndpoints` equals the evidence of every 023 finding; `make test` shows 023 findings unchanged.
- [ ] **Step 2:** Failing projection tests (engine output over fixtures): healthy route + zero-ready backend port → error; listener `Programmed=False` + healthy backend → error; a two-port Service with one port ready → only the other port's rows error, each row's ready/total equals 023's evidence for that port; endpoints capability off, denied, and failing → each `unknown` with a `coverage` signal, `Ready` nil; a route with no status for this parent → `waiting`; an xDS reject on the Gateway while endpoints are unknown → error, not unknown; ExternalName Service → `not-applicable`; a ResolvedRefs failure attributed to ref B → only B's share `failing`; unattributed → both `may-fail`; observedGeneration < generation → `waiting`, not error; a Backend `Accepted=False` → error from 002; global order puts every error before any warning.
- [ ] **Step 3:** Implement; commit `feat(paths): health projection from existing signals`.

### Task 28: memos and query

**Files:** new `pkg/paths/query.go`, `pkg/paths/cursor.go`, `pkg/paths/memo.go`, tests.

**Interfaces:**
```go
type Query struct{ Gateways []objects.Ref; Listener string; Route *objects.Ref; Backends []objects.Ref; Namespace string; States []health.State; Kind string; Mirror string /* include, only, exclude */; Q string; Sort string /* problem.asc, gateway, route, backend, hostname */; Desc bool; Cursor string; Limit int }
type Page struct{ Rows []int32; Next, Prev string; Summary Summary }
type Summary struct{ Paths, Routes, Gateways, Backends int; ByState map[health.State]Counts; EndpointsNotRead, Hidden, Unmemoised bool }
type Counts struct{ Paths, Routes, Gateways, Backends int }
func Run(t *Topology, p *Projection, q Query, visible func(explore.DepSet) bool) (Page, error) // explore.ErrBadQuery / ErrBadCursor on bad input
type Memo struct{ … }               // process-wide, one entry per install
func (m *Memo) Topology(key TopoKey, build func() *Topology) (*Topology, bool) // false: over the 2 MiB cap, not kept
func (m *Memo) Projection(pub *engine.Published, install string, t *Topology, project func() *Projection) *Projection
type TopoKey struct{ Install string; Snapshot *cache.Snapshot; Views *engine.InstallModel } // reuse only on the exact snapshot and computed-views pointers; no Source.Equal fallback
```
`visible` runs before counting and paging; filter options and the summary count only visible rows; `Hidden` set when any row was dropped. `route=` matches any route in the chain. Cursor format as `pkg/explore/cursor.go` (sort key + row identity, not a position). A projection entry is not reused past its `Next`.

- [ ] **Step 1:** Failing tests: each filter; `q` matches any hostname; `sort=hostname` by first hostname; paging 3 × limit 2 visits every row once with a stable cursor under an unchanged projection; a visibility func hiding one Gateway removes its rows from rows, counts and options and sets `Hidden`; one broken shared backend over 37 routes on 4 Gateways summarises as 1 backend, 37 routes, 4 Gateways; topology over 2 MiB → `Unmemoised` and rows equal to the memoised path; a projection is rebuilt after `Next` and after a new publication with the same snapshot.
- [ ] **Step 2:** Implement; commit `feat(paths): query, cursor and memos`.

### Task 29: `GET /api/v1/installs/{id}/paths`

**Files:** `pkg/api/handlers.go` (route), new `pkg/api/paths.go`, `pkg/api/paths_types.go`, `pkg/api/params.go` (`listPaths`), `pkg/api/visible.go` (`allowTable`), new `pkg/api/paths_test.go`, `pkg/api/testdata/golden/paths*.json`, `web/packages/shell/src/api/exploreTypes.ts`.

**Interfaces:**
```go
type PathListenerDTO struct{ Name, Protocol string; Port int32 } // api.ListenerDTO (explore_types.go:278) is unchanged
type PathRowDTO struct {
	Gateway LinkedRef; ListenerSet *LinkedRef; Listener PathListenerDTO; Hostnames []string
	Routes []LinkedRef /* root first */; Delegated bool; Rule int; Matches []MatchDTO
	Action string /* backend, redirect, direct-response, none */; Backend *LinkedRef; Port int32
	Weight int32; Share *float64; ShareState string; Mirror bool; MirrorPercent *float64
	Problem ProblemDTO; Endpoints *EndpointsDTO; Note string /* "not-computed", "unknown", "unconfirmable" */
}
type ProblemDTO struct{ State health.State; Signals []SignalDTO }
type SignalDTO struct{ Kind string; Code string `json:",omitempty"`; Ref *LinkedRef `json:",omitempty"`; Reason string `json:",omitempty"` }
type PathsResponse struct{ Install string; Rows []PathRowDTO; Next, Prev string; Summary PathSummaryDTO; CollectedAt time.Time }
type PathSummaryDTO struct{ Paths, Routes, Gateways, Backends int; ByState map[string]PathCountsDTO; EndpointsNotRead, Hidden, Unmemoised bool }
type EndpointsDTO // exactly §9.5: Source, Port *PortDTO, Runtime *RuntimeDTO, ReadAt, Complete, Ready *int, Total *int, Items []EndpointDTO
type PortDTO struct{ Port int32; Name string `json:",omitempty"` }
type RuntimeDTO struct{ Install string; Gateway LinkedRef; Proxy string /* process name */; Instance string /* changes when the proxy is replaced */ } // defined for #35, never set in Phase 6
func (v *view) allowTable(t *paths.Topology, d explore.DepSet) bool // memoised per topology; allowsAll fast path
```
JSON names camelCase as the DTO names. Params per §5: `gateway` (ns/name, repeatable), `listener`, `route` (group/kind/ns/name), `backend` (repeatable, `core` for the empty group), `namespace`, `state`, `kind`, `mirror`, `q`, `sort`, `cursor`, `limit` (1–200, default 50).

- [ ] **Step 1:** Failing API tests over the harness: golden `paths.json` and `paths-restricted.json`; unknown param → 400; `limit=0` and `limit=201` → 400; bad cursor → 400; restricted authorizer: rows whose Gateway is hidden are absent and `summary.hidden` is true, counts exclude them, and a second identical request (memo hit) calls the Authorizer again; class-only install → no delegated rows; `golden-v1` byte-equal; `additive_test.go` passes.
- [ ] **Step 2:** Implement; TS types and a `types.golden.test.ts` case. Commit `feat(api): paths endpoint`.

### Task 30: API-level bench pass and request gates

**Files:** `pkg/api/budget_test.go`, `pkg/api/bench_test.go`, `pkg/engine/budget_test.go` (`TestBudgetHeap` reads the API pass).

**Interfaces:** `TestBudgetPathsRequests` (Large: cold and warm, time and bytes allocated, per §14 targets); `TestBudgetHeapAPIWarm` (after warming paths for every install, at Large worst and Extreme: retained heap delta, added to the engine figure and checked against +48 MiB); `BenchmarkPathsLarge`, `BenchmarkPathsLargeRestricted`.

- [ ] **Step 1:** Write the tests; run `make bench`. Record the numbers in `bench-6.2.txt`.
- [ ] **Step 2:** On a heap overshoot: drop the topology memo (rows derived per request) or derive GRPC/TCP/TLS rules on request, re-run; if still over, stop and ask the user. Tune the request targets once, record them in the design §14. Commit `test(bench): API-level path budgets`.

### Task 31: path list page

**Files:** `web/packages/shell/src/shell/Shell.tsx` (`["traffic", TrafficPage]`), `shell/InstallNav.tsx` (entry after Routes), new `pages/traffic/TrafficPage.tsx`, `pages/traffic/PathTable.tsx`, `pages/traffic/PathSummary.tsx`, `explore/listQuery.ts` (paths params), `api/exploreHooks.ts` (`usePaths(installId, query)`), `copy/traffic.ts`, `pages/defaultPages.ts`, tests, new `web/e2e/traffic.spec.ts`, `test/e2e/fixtures/traffic.yaml`.

**Interfaces:** `TrafficPage` reads every filter, sort and cursor from the URL; a filter change resets the cursor. Entry links: Gateway detail → `?gateway=ns/name`, route detail → `?route=…`. Chips for state, kind, namespace, mirror; virtualised table (existing `ObjectTable`); summary line with "some paths are hidden" and "endpoints not read" notes.

- [ ] **Step 1:** Vitest: URL round-trip of every param; changing a chip drops `cursor`; summary renders counts and both notes; problem rows render state with icon and text (not colour alone).
- [ ] **Step 2:** Fixture `traffic.yaml`: a weighted two-ref HTTPRoute, a GRPCRoute, a TCPRoute, a Service scaled to 0, a cross-namespace ref without a grant, a rule with a redirect only. Playwright `traffic.spec.ts`: nav → Traffic lists problems first; the zero-endpoint backend row is an error; the `gateway=` chip narrows; reload keeps filters; the redirect rule shows its action.
- [ ] **Step 3:** Implement; hover and active styles on chips and rows; light and dark theme checked. Commit `feat(web): Traffic path list`.

### Task 32: 6.2 close

- [ ] **Step 1:** Wording pass → `phase-6.2-wording.md`; apply choices.
- [ ] **Step 2:** `make bench` → `bench-6.2.txt` (D-057 figure stated); e2e matrix green.
- [ ] **Step 3:** Demo reel of Task 31's flow on the kind cluster with the traffic fixture. Commit `docs: close sub-phase 6.2`.

---

## Sub-phase 6.3: backends (§8–§11)

### Task 33: Backend judgement behind the pack

**Files:** `pkg/health/checks.go` (`backendCheck` from `Core()` to `Kgateway()`; `Upstream()` order unchanged), `pkg/health/backend.go`, new `pkg/computed/kgwbackend/judge.go`, `pkg/computed/targets.go` (hook), `pkg/computed/kgw23|kgw24|kgw25/rules.go`, wrapping rule sets (`pkg/computed/rulesets`), `pkg/api/details.go:671`, `internal/boundary/imports_test.go` (allow `pkg/health/backend.go` → `computed/kgwbackend`, reason "§11 compatibility surface"), README upgrade note.

**Interfaces:**
```go
// pkg/computed
type BackendVerdict string // "accepted", "rejected", "unknown"
type BackendJudge interface{ JudgeBackend(b *unstructured.Unstructured) (BackendVerdict, *metav1.Condition) }
func (s Selection) BackendJudge() BackendJudge // nil without it
// pkg/computed/kgwbackend — today's backendAccepted logic
func Judge(b *unstructured.Unstructured) (computed.BackendVerdict, *metav1.Condition)
// pkg/health — unchanged signature, now calls kgwbackend.Judge
func BackendState(b *unstructured.Unstructured) (State, *metav1.Condition)
```

- [ ] **Step 1:** Failing tests: kgateway install findings (IDs, order, KGWUI-002 evidence) byte-equal to the base; a class-only install with a `Backend`-shaped object raises no 002 and its detail has conditions and no badge; a wrapping rule set forwards `BackendJudge` (as D-058 item 9); `health.BackendState` returns the same values as before on the 3.x fixtures.
- [ ] **Step 2:** Implement; paths projection (Task 27) reads 002 unchanged. Commit `refactor(pack): Backend judgement behind the kgateway pack`.

### Task 34: GatewayExtension consumers

**Files:** `pkg/extension/pack.go` (`Impl.BackendConsumers`), new `pkg/objects/consumer.go`, new `pkg/kgateway/consumers.go`, `pkg/kgateway/consumers_test.go`, `pkg/extension/extension.go` (wire, forward in `ImplFor`).

**Interfaces:**
```go
type Consumer struct{ Consumer Ref; Role string; BackendRef Ref; Port int; Field string } // Role: ext-auth, ext-proc, rate-limit, http
Impl.BackendConsumers func(inst install.Install, owned *install.Owned) []objects.Consumer // nil: none
func kgateway.BackendConsumers(inst install.Install, owned *install.Owned) []objects.Consumer // reads owned.Listed GatewayExtensions
```

- [ ] **Step 1:** Re-verify the field paths against every kgateway minor Snellius supports (2.3, 2.4, 2.5; latest patch tags), for example `gh api "repos/kgateway-dev/kgateway/contents/api/v1alpha1/kgateway/gateway_extensions_types.go?ref=v2.4.7" --jq .content | base64 -d | grep -n backendRef`. Expected per design: `spec.extAuth.grpcService.backendRef`, `spec.extProc.grpcService.backendRef`, `spec.rateLimit.grpcService.backendRef`, and the HTTP service `backendRef`. Record the tags and any difference in the commit message; a version-specific path goes in a per-version table in `consumers.go`.
- [ ] **Step 2:** Failing tests: one GatewayExtension per role gives one Consumer with `Field` the exact JSON path and the default namespace and Service kind applied; a missing backendRef gives none; a class-only install has none; `NewCore()` registry has nil `BackendConsumers`.
- [ ] **Step 3:** Implement; commit `feat(pack): GatewayExtension backend consumers`.

### Task 35: targetPort name and the scoped reader

**Files:** `pkg/cache/services.go` (`ServicePort.TargetName`), new `pkg/endpoints/reader.go`, `pkg/endpoints/bounded.go`, `pkg/endpoints/bounded_test.go`, new `pkg/cache/scoped.go`, `pkg/cache/scoped_test.go`, `internal/boundary/imports_test.go` (allow `pkg/cache/scoped.go`: "the scoped EndpointSlice reader"), `pkg/app/app.go` (wire when the endpoints capability is on), `pkg/api/handlers.go` (`WithScopedReader`).

**Interfaces:**
```go
type ServicePort struct{ …; TargetName string } // set when targetPort is a name; Target stays 0 then
// pkg/endpoints (core, neutral)
type Endpoint struct{ Address string; Port int32; PortName string; Ready, Serving, Terminating bool; Pod string }
type Result struct{ Items []Endpoint; Complete string /* complete, partial, failed, unavailable, timed-out, busy */; ReadAt time.Time }
type ScopedReader interface{ Endpoints(ctx context.Context, installID string, svc objects.Ref) (Result, error) } // no pkg/install import: cache → endpoints → install → cache would cycle
type Bounds struct{ InFlight, MemoServices, MaxEndpoints, PageSize, MaxPages int; Timeout, TTL time.Duration }
var DefaultBounds = Bounds{InFlight: 4, MemoServices: 32, MaxEndpoints: 1000, PageSize: 10, MaxPages: 10, Timeout: 2 * time.Second, TTL: 5 * time.Second}
func Bounded(r ScopedReader, b Bounds, clock clock.Clock) ScopedReader // coalesces duplicates, LRU memo
// pkg/cache (Kubernetes provider)
func NewScopedReader(kube kubernetes.Interface) endpoints.ScopedReader // namespaced LIST, label kubernetes.io/service-name
func api.WithScopedReader(r endpoints.ScopedReader) HandlerOption // nil: "unavailable"
```

- [ ] **Step 1:** Failing tests: `TestServiceFactTargetName` (named and numeric targetPort); `TestScopedReaderPages` (fake client: 3 pages of 10 → complete; a continue token past page 10 → partial; > 1,000 endpoints → partial, truncated to 1,000); `TestBoundedCoalesces` (10 concurrent calls for one Service → 1 read); `TestBoundedBusy` (5th distinct read while 4 in flight → `busy`); `TestBoundedTimeout` (2 s → `timed-out`); `TestBoundedLRU` (33rd Service evicts the least recent; an entry older than 5 s is re-read).
- [ ] **Step 2:** Implement; `make bench` (targetPort name retained cost recorded). Commit `feat(endpoints): bounded scoped EndpointSlice reader`.

### Task 36: backends API

**Files:** `pkg/api/handlers.go`, new `pkg/api/backends.go`, `pkg/api/backends_types.go`, `pkg/api/params.go` (`listBackends`), `pkg/paths/backends.go` (backend aggregation over the projection), tests, goldens, `web/packages/shell/src/api/exploreTypes.ts`.

**Interfaces:**
```go
// GET /api/v1/installs/{id}/backends  params: kind, namespace, state, used (yes, no), gateway, q, sort (problem.asc, name, used), cursor, limit
// GET /api/v1/installs/{id}/backends/{group}/{kind}/{ns}/{name}   ("-" is the empty group)
type BackendRowDTO struct{ Ref LinkedRef; Kind string /* "Service" or "Backend · <spec.type>" */; Endpoints []EndpointsDTO /* one per referenced port */; Routes, Gateways, Consumers, Confirmed int; Problem ProblemDTO }
type BackendsResponse struct{ Install string; Rows []BackendRowDTO; Next, Prev string; Hidden bool; CollectedAt time.Time }
type BackendDetail struct {
	Install string; Ref LinkedRef; Kind string; Type string `json:",omitempty"`; Problem ProblemDTO; Status []SignalDTO
	Exposure PathsResponse /* backend= filter, first page */; Consumers []ConsumerDTO
	Sources []EndpointsDTO; PortChain []PortChainDTO; Explanations []RefExplanationDTO
	Service *ServiceFactDTO; Conditions []Condition; CollectedAt time.Time
}
type ConsumerDTO struct{ Consumer LinkedRef; Role, Field string; Port int }
type PortChainDTO struct{ RefPort int32; ServicePort *int32; ServicePortName, TargetPort, TargetName string; SlicePorts []int32; NoSuchPort, NoSlicePort bool }
type ServiceFactDTO struct{ Type string; Ports []PortDTO }
```
Listed: every in-scope `Backend` (used or not), every ref an admitted route of the install writes (attached or not), every consumer backend. Unreferenced Services are not listed. Detail authorises the backend before any read, the scoped reader included, memo hits included; a hidden backend → 404 as other details. Consumer identities (the GatewayExtension) are authorised before serialisation and before `consumers` counts; a hidden consumer is omitted and counted nowhere. `Sources` has one `kubernetes` entry per referenced Service port, each with its own completeness and counts; the scoped read's endpoints are split by port name into each entry's `Items` when complete or partial. The list's endpoints cell shows each referenced port ("80: 0/3 · 443: 3/3"). `NoSlicePort` only when the read is complete.

- [ ] **Step 1:** Failing API tests: list golden; `used=no` lists an unused Backend; a Service ref whose attachment failed lists with `confirmed` 0 and the attachment problem; a GatewayExtension consumer appears with role `ext-auth`; restricted viewer: hidden backend → 404 with zero scoped reads (counting fake), hidden path rows absent from `exposure`; a hidden GatewayExtension is absent from `consumers` and from the list's `consumers` count; reader nil → source `unavailable`, cached counts shown; ExternalName → `not-applicable`; port chain for a named targetPort shows `targetName` and slice ports; `golden-v1` byte-equal.
- [ ] **Step 2:** Bench additions: `TestBudgetBackendDetail` (concurrent cold details at Large, < 4 MiB each), backend detail in `TestBudgetHeapAPIWarm` with ≥ 32 distinct Services and overlapping generations.
- [ ] **Step 3:** Implement; commit `feat(api): backends list and detail`.

### Task 37: reference explanations (§10)

**Files:** `pkg/api/explain.go`, `pkg/api/backends.go`, `pkg/api/explain_test.go`.

**Interfaces:**
```go
type RefExplanationDTO struct{ Route LinkedRef; Rule, Ref int; Kind string /* attributed, candidate, waiting */; Reason string; Explanation string; Fix *diagnose.SuggestedFix `json:",omitempty"` }
func refExplanations(m *engine.InstallModel, route objects.Ref, ref objects.Ref) []RefExplanationDTO
```
Attributed: the 013/014 finding's `RefsEvidence.Explanation`, the route's reason, and `diagnose.Fix` (the ReferenceGrant for `RefNotPermitted`). Candidate: each unattributed candidate gets one entry quoting the route's `ResolvedRefs=False` reason, no guess. Stale status: `waiting`. ReferenceGrants are not evaluated.

- [ ] **Step 1:** Failing tests: a single-candidate `RefNotPermitted` → attributed with the grant YAML equal to `diagnose.Fix`; two candidates → two `candidate` entries with the same reason; observedGeneration behind → `waiting`, no fix; restricted route → entry absent.
- [ ] **Step 2:** Implement; commit `feat(api): reference explanations`.

### Task 38: Backends pages

**Files:** `Shell.tsx` (`["backends", BackendsPage]`, `["backends/:group/:kind/:ns/:name", BackendPage]`), `InstallNav.tsx` (entry "Backends"), new `pages/backends/BackendsPage.tsx`, `BackendPage.tsx`, `EndpointsSection.tsx`, `PortChain.tsx`, `Explanations.tsx`, `explore/objectPath.ts` (backend detail path, `-` for the empty group), `pages/explore/route/RuleList.tsx` (`BackendItem` link → backend detail), `kgateway/webPack.ts` (Backend `spec.type` label), `copy/backends.ts`, tests, new `web/e2e/backends.spec.ts`.

- [ ] **Step 1:** Vitest: chips Problems, Services, Backend resources, Unused, Namespace, Gateway map to params; status line uses fixed phrases per signal and no status code; "partial", "timed out", "busy, retry" states render; a source `unavailable` shows cached counts only; no YAML tab for a Service.
- [ ] **Step 2:** Playwright: Backends → the zero-endpoint Service is first; its detail status line names routes and Gateways; "How this backend is exposed" links to Traffic with `backend=`; the cross-namespace ref shows the suggested ReferenceGrant; a route's backend links to backend detail.
- [ ] **Step 3:** Implement; commit `feat(web): Backends list and detail`.

### Task 39: 6.3 close

- [ ] **Step 1:** Wording pass → `phase-6.3-wording.md`.
- [ ] **Step 2:** `make bench` → `bench-6.3.txt`; e2e matrix (the kgateway e2e Backend and GatewayExtension fixtures in `backends.yaml` extended with an ext-auth extension).
- [ ] **Step 3:** Demo reel: Backends list → detail → exposure → Traffic. Commit `docs: close sub-phase 6.3`.

---

## Sub-phase 6.4: route trace, all route kinds (§7)

### Task 40: route detail for every route kind

**Files:** `pkg/api/details.go` (`routeDetail` `:469-477`), `pkg/api/explore_types.go` (`MatchDTO`, `RuleDTO`, `BackendRefDTO`, `RouteDetail`), `pkg/api/details_test.go`, goldens, `web/packages/shell/src/api/exploreTypes.ts`, `pkg/explore` (`DetailKinds`, `isHTTPRoute` callers that gate links).

**Interfaces:**
```go
type GRPCMethodDTO struct{ Type, Service, Method string } // json grpcMethod
type MatchDTO struct{ …; GRPCMethod *GRPCMethodDTO `json:"grpcMethod,omitempty"` } // Method stays HTTP-only
type RuleDTO struct{ …; EffectiveFilters []FilterDTO `json:"effectiveFilters,omitempty"` } // reserved for Phase 11, never set
type BackendRefDTO struct{ …; Share *float64 `json:"share,omitempty"`; ShareState string `json:"shareState,omitempty"`; Problem *ProblemDTO `json:"problem,omitempty"`; Endpoints *EndpointsDTO `json:"endpoints,omitempty"` }
type TraceAttachmentDTO struct{ Gateway LinkedRef; ListenerSet *LinkedRef; Listener PathListenerDTO; Hostnames []string; State health.State }
type RouteDetail struct{ …; Serves []TraceAttachmentDTO `json:"serves,omitempty"` }
```
GRPCRoute, TCPRoute and TLSRoute details from `RouteView.Rules`. TCP/TLS rules send `"matches": []` (the field has no `omitempty`) and the SPA shows no match line for those kinds; HTTPRoute keeps the `PathPrefix "/"` default. Share/problem/endpoints come from the paths projection for the route (`route=` filter), not recomputed. A ref's `problem` is the worst across the route's attachments, and its signals name the attachment (Gateway, listener) each came from; `serves[].state` shows each attachment's own state. Delegation stays HTTPRoute-only.

- [ ] **Step 1:** Failing tests: GRPCRoute detail golden (method match, headers, two weighted refs with shares 25/75); TCPRoute and TLSRoute detail goldens (ports, weights, no matches); HTTPRoute detail: existing fields byte-equal to today's except the new optional keys; `effectiveFilters` absent everywhere; GRPCRoute links in lists now open detail (`link: detail`); `golden-v1` byte-equal.
- [ ] **Step 2:** Implement; commit `feat(api): route detail for GRPCRoute, TCPRoute and TLSRoute`.

### Task 41: route trace in the SPA

**Files:** `pages/explore/RoutePage.tsx`, `pages/explore/route/RuleList.tsx`, new `pages/explore/route/ShareBar.tsx`, `pages/explore/route/Serves.tsx`, `pages/explore/route/MatchLine.tsx` (gRPC method), `copy/explorer.ts`, tests, `web/e2e/traffic.spec.ts`.

**Interfaces:** `Serves` above the rules (Gateway, listener, hostnames, state); per rule: match, a "Filters" area (filters as written, a marked slot with no text about a future feature), `ShareBar` (stacked, labels "% of this rule's requests"; `not-shown`, `no-traffic`, `failing`, `may-fail` states with text and icon), then one line per backend with port, weight, share, endpoint state and problem; failure wording "requests fail" for HTTP/gRPC and "connections fail" for TCP/TLS; route detail links to Traffic with `route=`.

- [ ] **Step 1:** Vitest: share bar for 1/3, all-zero, mixed-delegation rules; TCPRoute rule wording "connections fail"; a GRPC match line shows service/method.
- [ ] **Step 2:** Playwright: open the GRPCRoute and TCPRoute from the routes list; the weighted HTTPRoute shows 25 %/75 %; the zero-endpoint backend line shows its problem and links to backend detail.
- [ ] **Step 3:** Implement; commit `feat(web): route trace`.

### Task 42: 6.4 close

- [ ] **Step 1:** Wording pass → `phase-6.4-wording.md`.
- [ ] **Step 2:** `make bench` → `bench-6.4.txt`; e2e matrix (both TCPRoute fixtures, v1 and v1alpha2).
- [ ] **Step 3:** Demo reel: the HR-system story (§1): route → rule → backend → why it fails. Commit `docs: close sub-phase 6.4`.

---

## Sub-phase 6.5: Gateway map, core (§6.1 items 1–3, 6, 8; §6.2)

### Task 43: React Flow, lazy chunk, Map tab

**Files:** `web/packages/shell/package.json` (`@xyflow/react` in `dependencies`, exact current minor), `pages/explore/GatewayPage.tsx` (tab "Map"), new `pages/map/MapTab.tsx` (`React.lazy` boundary), `pages/map/GatewayMap.tsx`, `pages/map/map.css.ts` (theme-token overrides of React Flow's styles), `web/app/vite.config.ts` if a manual chunk is needed, new `web/packages/shell/src/pages/map/chunk.test.ts`.

- [ ] **Step 1:** Test `chunk.test.ts` over the built `web/app/dist`: no entry or non-map chunk contains `@xyflow`; the map chunk ≤ 60 kB gzipped. Vitest: the tab renders a fallback while loading; the map reads `/paths?gateway=ns/name` with the tab's filters.
- [ ] **Step 2:** Implement with React Flow's pan, zoom, fit view and `MiniMap`; light and dark themes from tokens. Commit `feat(web): Gateway map tab with React Flow`.

### Task 44: graph and layout

**Files:** new `pages/map/graph.ts`, `pages/map/layout.ts`, `pages/map/layout.test.ts`.

**Interfaces:**
```ts
// The map loads paths in pages of 200 up to MAP_MAX_ROWS (2,000), sequentially; beyond that it shows "showing 2,000 of N paths, narrow the filters". The large-Gateway check uses the server summary (summary.routes, summary.backends), not the loaded rows.
const MAP_MAX_ROWS = 2000;
type NodeKind = "gateway" | "listener" | "route" | "backend";
interface MapNode { id: string; kind: NodeKind; ref?: LinkedRef; state: HealthState; delegatedUnder?: string; rows: number[] }
interface MapEdge { id: string; source: string; target: string; share?: number; broken: boolean; rows: number[] }
function buildGraph(rows: PathRow[], expanded: ReadonlySet<string>): { nodes: MapNode[]; edges: MapEdge[] };
function layoutColumns(g: { nodes: MapNode[]; edges: MapEdge[] }, changed?: ReadonlySet<NodeKind>): Map<string, { x: number; y: number }>;
const LARGE_GATEWAY = 60;
```
Four fixed columns; order within a column by the barycentre of connected nodes, a few sweeps; deterministic for equal input. `changed` re-runs only those columns.

- [ ] **Step 1:** Failing tests: one node per distinct listener/route/backend; delegated routes sit under their root with `delegatedUnder`; edge `share` only where the row's `shareState` is `shown`; layout is deterministic; on a crossing fixture, barycentre ordering has fewer crossings than input order; with `changed={"route"}` gateway and listener positions are unchanged.
- [ ] **Step 2:** Implement; commit `feat(web): map graph and column layout`.

### Task 45: hover, side panel, search, filters, large Gateways

**Files:** `pages/map/GatewayMap.tsx`, new `pages/map/SidePanel.tsx`, `pages/map/MapToolbar.tsx`, `copy/map.ts`, tests, new `web/e2e/map.spec.ts`.

**Interfaces:** hover highlights every edge and node on paths through the hovered node (by shared `rows`) and dims the rest; click opens `SidePanel` (state, problems, endpoints, links to detail and Traffic); search box focuses a node by name; chips Problems only, kind, namespace update the server query live; a Gateway with > `LARGE_GATEWAY` routes or backends opens on Problems only with a prompt to narrow; edge thickness follows share where shown.

- [ ] **Step 1:** Vitest: hover set equals the nodes on shared rows; large Gateway (summary of 61 routes, first page only loaded) opens with `state=error,warning,unknown` in the URL; 2,500 matching paths load 2,000 and show the truncation notice; side panel content for a backend with no ready endpoints.
- [ ] **Step 2:** Playwright `map.spec.ts`: open Gateway → Map; hover a route dims unrelated nodes; click the broken backend opens the panel with a link to backend detail; the Problems chip reduces nodes.
- [ ] **Step 3:** Implement; commit `feat(web): map hover, side panel, search and filters`.

### Task 46: keyboard navigation and table toggle

**Files:** `pages/map/GatewayMap.tsx`, new `pages/map/keyboard.ts`, `pages/map/keyboard.test.ts`, `pages/map/MapTab.tsx` (toggle reuses `PathTable` from Task 31).

**Interfaces:** `nextNode(g, from: string, key: "ArrowLeft" | "ArrowRight" | "ArrowUp" | "ArrowDown"): string | undefined` — left/right move to a connected node in the adjacent column, up/down within the column; Enter opens the side panel. The table toggle shows the same rows as the path list.

- [ ] **Step 1:** Tests: arrow keys follow connections only; the table toggle shows the same row count as the map's query; Playwright: keyboard-only reach of a backend panel; axe has no serious violations on the tab.
- [ ] **Step 2:** Implement; commit `feat(web): map keyboard navigation and table view`.

### Task 47: 6.5 close

- [ ] **Step 1:** Wording pass → `phase-6.5-wording.md`.
- [ ] **Step 2:** `make bench` → `bench-6.5.txt`; e2e matrix; Large fixture check of the 60 threshold (tune the constant if Large Gateways render slowly; record the reason).
- [ ] **Step 3:** Demo reel: map overview, hover, panel, filters. Commit `docs: close sub-phase 6.5`.

---

## Sub-phase 6.6: Gateway map, the rest (§6.1 items 4, 5, 7, 9)

### Task 48: expand/collapse and focus mode

**Files:** `pages/map/graph.ts`, `pages/map/GatewayMap.tsx`, `pages/map/focus.ts`, tests, `web/e2e/map.spec.ts`.

**Interfaces:** URL params `expand` (repeatable node ids) and `focus` (one node id); `focusSubgraph(g, id): { nodes; edges }` keeps the node's upstream and downstream paths only. Expand opens a route into rule nodes and a delegating route into its children; layout re-runs for changed columns only.

- [ ] **Step 1:** Tests: expanding a two-rule route adds two rule nodes and keeps other positions; focus on a backend keeps exactly the Gateways, listeners and routes on its rows; reload restores `expand` and `focus`.
- [ ] **Step 2:** Implement; commit `feat(web): map expand, collapse and focus`.

### Task 49: animated flow and export

**Files:** `pages/map/edges.tsx`, new `pages/map/export.ts`, `pages/map/export.test.ts`, `pages/map/MapToolbar.tsx`.

**Interfaces:** healthy edges animate, broken edges draw dashed with a break mark; no animation under `prefers-reduced-motion`. `exportSVG(g, positions, theme): string` builds the SVG from our own layout and node data; `exportPNG(svg: string, scale: number): Promise<Blob>` rasterises it on a canvas. No new dependency.

- [ ] **Step 1:** Tests: reduced motion → no animated edge class; `exportSVG` output contains one element per visible node and edge, with the theme colours; Playwright downloads a PNG and an SVG of non-zero size.
- [ ] **Step 2:** Implement; commit `feat(web): map flow animation and export`.

### Task 50: 6.6 close and phase close

- [ ] **Step 1:** Wording pass → `phase-6.6-wording.md`.
- [ ] **Step 2:** `make bench` → `bench-6.6.txt` (final D-057 figure); e2e matrix; demo reel of the whole phase (Traffic → map → backend → route trace).
- [ ] **Step 3:** README (Traffic, Backends, map, `ListOnce`, `Namespaces`); `docs/documentation-notes.md` topics; design §14 targets updated to the tuned values. Commit `docs: close Phase 6`.
