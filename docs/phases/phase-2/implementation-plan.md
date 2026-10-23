# Phase 2 — Gateway & Route Explorer: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Event-driven recompute with honest freshness, then Gateway, ListenerSet and HTTPRoute explorer pages and lists, first on observed status (2.1, 2.2) and then with the labelled, cross-checked computed views A, B and C (2.3, 2.4).

**Architecture:**
- **Engine.** Informer events, access transitions, metrics results, served-kind changes, deadlines and a safety timer trigger a debounced, single-flight recompute. Each run publishes one immutable `Published{Model, Source, ConfirmedAt}` through an atomic pointer; a 1 s confirm tick renews `ConfirmedAt` only while the live source fingerprint equals the published one (D-019).
- **Explorer.** `pkg/explore` builds one immutable `Index` per install inside the recompute: route, Gateway and ListenerSet rows, parent and delegation edges from status, state flags, findings per object and the admission table. Handlers filter, authorize, sort and page it with keyset cursors.
- **Computed views (2.3, 2.4).** `pkg/computed` holds pinned rule sets per kgateway minor behind the `Rules` seam; their results feed the index, the DTOs and KGWUI-011/012, never ownership or health (D-020).

**Tech Stack:** Go 1.26, client-go v0.36, `k8s.io/utils/clock` (already in the module graph, promoted to a direct dependency for the fake clock), React 19, TypeScript, emotion, react-router, SWR, TanStack Table, Vitest, Testing Library, Playwright, kind.

**Spec:** `docs/phases/phase-2/design.md`, `docs/phases/phase-2/computed-rules.md`, `docs/decisions.md` D-017 … D-020 (approved 2026-10-23, with the addendum of commit `172a5a4`). Section numbers below (§3.1, §7.4, rules §9.3) refer to these. Phase 1's plan (`docs/phases/phase-1/implementation-plan.md`) holds the project conventions this plan does not repeat.

## Global Constraints

- **API v1 is additive only:** "All additions are new paths or new optional fields. No existing path, body or install ID changes (D-016 H11)." Existing goldens change only by the optional fields of design §7.6.
- **Import boundary (D-016):** no new `k8s.io/api/core/v1`, `k8s.io/api/apps/v1`, `k8s.io/client-go` or discovery-client use in core code; `pkg/explore`, `pkg/computed` and `pkg/trigger` stay outside the allowlist of `internal/boundary/imports_test.go`.
- **Public repo:** vendor and commercial edition names never appear in code, comments, file names, docs, commit messages or PR text. Before each commit: `git diff --cached | grep -inE "$FORBIDDEN_TERMS" && echo STOP || true`.
- **Read-only:** no writes to the Kubernetes API, no Secrets, no controller admin server, no Envoy admin (design §11).
- **Times on the wire are UTC (D-017).** Internal clocks keep their zone and monotonic reading; convert with `wireTime` at the API boundary.
- **Wording pass:** every new user-facing string: "short sentences, plain English, active voice; per string, a sub-agent picks the least machine-sounding of five accurate options" (design §8.8). Design drafts are placeholders.
- **UI quality:** every interactive element has hover, active and focus-visible styles in both themes; state is icon + colour + text; skeletons at the final row height so loading causes no layout shift; no horizontal page scroll at 720 px (design §8.7).
- **Deterministic tests:** engine and scheduler tests use a fake clock and channel receives bounded by `testutil.Recv`; no `time.Sleep` in new tests. Only the `bench`-tagged budget runs use a real clock (design §9.6).
- **Cross-model review per sub-phase (D-020, design §13):** each sub-phase's diff is reviewed by Codex read-only before it closes; every finding is triaged.
- **Detail pages are Gateway, ListenerSet kinds and HTTPRoute only.** GRPCRoute, TCPRoute and TLSRoute stay list rows plus the YAML view in Phase 2 (design §1, §15).
- **Supported versions:** kgateway v2.3.11 (`c2af2887`, Gateway API v1.5.1) and v2.4.7 (`b4a2bf74`, Gateway API v1.6.1).
- **Scale tiers (design §9.6):** Typical 30 Gateways / 3 listeners / 500 HTTPRoutes / 10 ListenerSets / 20 namespaces / 50 Backends; Large 100 / 4 / 2,000 / 200 / 100 / 200; Extreme 500 / 4 / 5,000 / 2,000 / 500 / 1,000; delegated children 10 % (depth ≤ 2, 3, 4; fan-out ≤ 10, 50, 100; Extreme adds an 8-layer diamond lattice); parentRefs mean 1.2, 1.3, 1.5; parent matches per delegating rule 1, 2, 4; 5 % each GRPC, TCP and TLS routes; labelled namespaces behind some `Selector` listeners; 5 % failed parentRefs.
- **Large-tier budgets (block the sub-phase when missed):** confirm tick ≤ 5 ms; burst of 1,000 events within 1 s ≤ 2 recomputes with event-to-publication p95 ≤ 1.2 s; warm recompute ≤ 100 ms (2.2), ≤ 120 ms (2.3), ≤ 150 ms (2.4); cold recompute ≤ 1 s; allocation per warm recompute ≤ 32 MiB; heap of index, computed views and decode cache ≤ 32 MiB above baseline; sustained stream (one event per 100 ms, 10 min, 20 polling clients) ≤ 1 recompute per s and engine CPU ≤ 15 % of one core; list request p95 < 50 ms, p99 < 100 ms, p95 < 100 ms with 20 concurrent clients; route detail on the deepest child p95 < 50 ms.
- **Change-size rule (AGENTS.md §6):** approval of this plan covers each task's file list. A task that needs more files stops and asks.

## Review Focus

1. **A change whose notification has not arrived yet.** The informer store moved but its handler is blocked: the confirm tick must refuse to renew `ConfirmedAt`, schedule a run, and the run must publish the change with no handler having run. → Task 6, `TestConfirmTickCatchesChangeBeforeHandler`.
2. **An idle cluster for minutes.** No recompute for 30 s while `confirmedAt` advances each second: cluster cards, switcher and header must stay fresh, and fall back to `computedAt` only when `confirmedAt` is absent. → Task 8, `confirmedAt keeps an idle install fresh`.
3. **A delegated child just after an edit.** Its matching entry describes the previous generation: it stays a child, owned and counted, with KGWUI-003 info (check pending) inside grace, never KGWUI-011, and the tiles do not flicker. → Task 14, `TestStaleEligibleEntryKeepsEdge`.
4. **Paging while the model changes.** A row inserted before the anchor, the anchor deleted, ties under a descending sort, and backward paging: no crash, no duplicate inside one direction, `prev` returns the same set as forward. → Task 19, `TestCursorsAcrossRecomputes`.
5. **Probing hidden objects through filters.** `deniedInInstall` must not change under any `q`, `namespace`, `state` or `parent` filter, and a denied Backend, `via` or ancestor step reveals no identity or message. → Task 24, `TestDeniedInInstallIgnoresFilters`.

## Sub-phases

Each sub-phase starts with a brief (`docs/phases/phase-2/phase-2.N-brief.md`, per the `agentic-engineering-sdlc` skill), ends green and committed, gets its own cross-model review, its own Large-tier benchmarks, and ships alone (design §13).

| Sub-phase | Goal | Tasks | Exit criteria |
|---|---|---|---|
| **2.1 Engine and timestamps** | Phase 1 pages, event-driven, with honest freshness (D-019) | 1–10 | D-021 observations recorded; Phase 1 baseline measured; §9.3 engine tests pass with a fake clock; `confirmedAt` served and read by the SPA; existing goldens differ only by `confirmedAt`; burst, idle and tick budgets met at Large; Codex review triaged |
| **2.2 Explorer on observed status** | Every explorer page and list, from status only | 11–34 | Q12 ownership with before/after tests; KGWUI-011 status trigger; Backend kind; list and detail APIs with Authorizer and goldens (Go and TS); Gateways, ListenerSets, Routes lists and the three detail pages; wording pass; 2.2 budgets met at Large; kind e2e green on v2.3.11 and v2.4.7; ui-loop recordings; release note; Codex review triaged |
| **2.3 View A** | Routes per listener with effective hostnames, labelled and cross-checked | part 2 | design §13; rules §11.1, §11.4 (view A), §11.5 |
| **2.4 Views B and C** | Delegation selection, outcomes and effective matchers | part 2 | design §13; rules §11.2–§11.4; dense-selection benchmark |

## File Structure

All of Phase 2. `(2.N)` is the sub-phase that creates or first changes the file.

```
pkg/trigger/trigger.go              Signal: coalescing "something changed" notifications (2.1)
pkg/cache/cache.go                  event handlers, access-transition notify, clock, Snapshot.Source (2.1)
pkg/cache/fingerprint.go            Fingerprint, KindPrint, Cache.Fingerprint (2.1)
pkg/ctrlmetrics/scrape.go           Collector.OnResult notify (2.1)
pkg/kinds/monitor.go                Monitor.OnChange notify (2.1)
pkg/kinds/kinds.go                  CategoryBackend, ListenerSet shape contract in the package doc (2.2)
pkg/kinds/upstream.go               Backend descriptor (2.2)
pkg/engine/engine.go                Published, Source, Deadline, Compute publishes, Current (2.1); decode cache, explore.Build call (2.2); RulesFor, persistence map (2.3)
pkg/engine/sched.go                 scheduler state machine: debounce, deadlines, single flight (2.1)
pkg/engine/loop.go                  Run: scheduler goroutine, confirm tick, safety timer (2.1)
pkg/engine/decode.go                decode cache keyed by (uid, resourceVersion) (2.2)
pkg/engine/bench_test.go            Compute and fingerprint benchmarks per tier (2.1)
pkg/engine/budget_test.go           //go:build bench — burst, idle, stream budgets (2.1, 2.2)
pkg/objects/objects.go              ListenerStatus.SupportedKinds (2.2)
pkg/objects/gateway.go              Listener, GatewayView, ListenerSetView decoders (2.2)
pkg/objects/httproute.go            HTTPRouteView, Rule, Match, Filter, BackendRef, DelegationRef, RouteHostnames (2.2)
pkg/objects/backend.go              BackendView (2.2)
pkg/objects/decoder.go              Decoder interface, Direct (2.2)
pkg/install/assign.go               HTTPRoute parent kinds, Backends, unwatched owned-route parents (2.2)
pkg/install/delegation.go           reported edges, ownership BFS, declared candidates, no-parent rows (2.2)
pkg/install/settings.go             controller settings read from the Pod spec (2.3)
pkg/health/finding.go               KGWUI-011 (2.2), KGWUI-012 (2.3) codes; Finding.Evidence (2.2)
pkg/health/checks.go                HTTPRoute-parent entries in "resource status" (2.2)
pkg/health/delegation.go            KGWUI-011 status trigger (2.2); computed and orphan triggers (2.4)
pkg/health/backend.go               Backend Accepted rule (2.2)
pkg/overview/overview.go            Backend tile counts in-use Backends (2.2)
pkg/explore/explore.go              Index, Input, Build (2.2); view A members (2.3); delegation rows (2.4)
pkg/explore/rows.go                 RouteRow, GatewayRow, ListenerSetRow, flags, row state (2.2)
pkg/explore/admit.go                admission table and detail kinds (2.2); "selected" reason (2.4)
pkg/explore/query.go                filters, sorts, Page (2.2); listener filter (2.3); delegation query (2.4)
pkg/explore/cursor.go               keyset cursor encode/decode (2.2)
pkg/explore/ancestors.go            bounded up-chain walk (2.2)
pkg/computed/rules.go               Rules seam, RulesFor, rule-set selection (2.3)
pkg/computed/kgw23/, kgw24/         rule sets kgateway-2.3, kgateway-2.4 (2.3, 2.4)
pkg/computed/testdata/<rules>/      per-version fixtures (2.3, 2.4)
pkg/api/types.go                    InstallSummary.ConfirmedAt (2.1); MetaResponse.Views, HealthResponse (2.2)
pkg/api/handlers.go                 Published once per request (2.1); routes and lookup superset (2.2)
pkg/api/explore_types.go            LinkedRef, Page, rows, details, ParentSummary DTOs (2.2); Computed, Attachment (2.3); DelegationRow, EffectiveDTO (2.4)
pkg/api/link.go                     link(): admission + Authorizer (2.2)
pkg/api/params.go                   query-parameter parsing and 400s (2.2)
pkg/api/lists.go                    routes, gateways, listenersets handlers (2.2)
pkg/api/details.go                  gateway, listenerset, route detail handlers (2.2); delegation endpoint (2.4)
pkg/api/visible.go                  dependency checks, derived recounts, evidenceRestricted (2.2, extended 2.3, 2.4)
pkg/api/budget_test.go              //go:build bench — list and detail latency (2.2)
pkg/api/testdata/golden/            new fixtures per sub-phase (design §7.6)
pkg/api/testdata/golden-v1/         frozen Phase 1 goldens for the additive-only test (2.1)
pkg/extension/extension.go          WithComputedRules (2.3)
pkg/app/app.go                      trigger wiring, safety timer (2.1)
cmd/kgateway-ui/main.go             --poll-interval default 30s (2.1)
internal/testutil/scale.go          ScaleWorld per tier (2.1); dense selection (2.4)
internal/testutil/fixtures.go       Backend, delegation, rule fixtures (2.2)
internal/testutil/clock.go          fake clock helpers (2.1)
web/packages/shell/src/api/types.ts           InstallSummary.confirmedAt (2.1); re-exports explore types (2.2)
web/packages/shell/src/api/exploreTypes.ts    explorer DTOs (2.2, 2.3, 2.4)
web/packages/shell/src/api/exploreHooks.ts    list and detail hooks (2.2)
web/packages/shell/src/api/freshness.ts       confirmedOrComputed (2.1)
web/packages/shell/src/explore/objectPath.ts  objectPath, linkTarget (2.2)
web/packages/shell/src/explore/LinkedName.tsx renders a LinkedRef by its link (2.2)
web/packages/shell/src/explore/ObjectTable.tsx  paged table with URL state (2.2)
web/packages/shell/src/explore/Pager.tsx, ConditionList.tsx, CandidateBanner.tsx, DetailTabs.tsx, CopyCommandButton.tsx, StateFilter.tsx (2.2)
web/packages/shell/src/explore/ComputedBadge.tsx  (2.3)
web/packages/shell/src/copy/explorer.ts       UI copy chosen by the wording pass (2.2, 2.3, 2.4)
web/packages/shell/src/pages/explore/{GatewaysPage,ListenerSetsPage,RoutesPage,GatewayPage,ListenerSetPage,RoutePage}.tsx (2.2)
web/packages/shell/src/pages/explore/gateway/ListenerTable.tsx (2.2; computed columns 2.3)
web/packages/shell/src/pages/explore/route/{UpChain,ParentCard,RuleList,ChildRoutes}.tsx (2.2); DelegationTab.tsx, EffectiveMatches.tsx (2.4)
web/packages/shell/src/shell/{extension.ts,Shell.tsx,vocabulary.tsx,InstallNav.tsx,Header.tsx,InstallSwitcher.tsx} (2.1, 2.2)
web/packages/shell/src/pages/{ClusterLanding.tsx,defaultPages.ts} and pages/install/{FindingRow,TopFindings,GatewayTable}.tsx (2.1, 2.2)
web/e2e/explorer.spec.ts            Playwright over the kind fixtures (2.2)
test/e2e/fixtures/explorer.yaml     2.2 fixtures; view A (2.3) and delegation (2.4) additions
test/e2e/api_test.go                kind assertions per sub-phase
docs/decisions.md                   D-021 observations (2.1)
docs/phases/phase-2/bench/          raw benchmark output per sub-phase
docs/phases/phase-2/phase-2.N-{brief,complete}.md, reviews/2.N-codex.md
README.md, deploy/helm/kgateway-ui/README.md   --poll-interval meaning (2.1); upgrade note on counts (2.2)
Makefile                            bench target (2.1)
```

---

# Sub-phase 2.1 — Engine and timestamps

### Task 1: Cluster observations for the computed rules (D-021)

Answers rules §10 items 1–10 on both tags before any rule depends on them (design §9.5). **No product code.**

**Files:**
- Modify: `docs/decisions.md` (append D-021)
- Modify: `docs/phases/phase-2/computed-rules.md` §10 (mark each item answered, link D-021)
- Create: `test/e2e/fixtures/observe/*.yaml` (one manifest per item, reused by 2.3/2.4 e2e)

**Interfaces:** Produces facts for 2.3 and 2.4 (rule sets, the `verified` mark of rules §8.3) and the observation manifests.

- [ ] **Step 1: Write one manifest per item** under `test/e2e/fixtures/observe/` (Gateway `infra/edge` with listeners `http` 8080 and `https` 8443 hostname `*.example.com`; parent HTTPRoute `shop/api` with delegation backendRefs; children as each item needs).
- [ ] **Step 2: Run each item on v2.4.7, then v2.3.11** (`KGW_VERSION=v2.4.7 GWAPI_VERSION=v1.6.1 bash test/e2e/setup.sh`, then the pair `v2.3.11`/`v1.5.1`). For each: `kubectl apply`, wait for `observedGeneration` to match, record `kubectl get httproute -A -o json | jq '[.items[] | {n:.metadata.name, gen:.metadata.generation, parents:.status.parents}]'` and the Gateway's `status.listeners[].attachedRoutes`. Item 1 and 6 need a second apply that changes the selection; record status before and after.
- [ ] **Step 3: Append D-021** to `docs/decisions.md` in the existing format: Context (rules §10), Decision (per item: tag, observed parentRef of the entry, entry present or cleared, `attachedRoutes`, verbatim excerpt), Alternatives "n/a (observation)", Consequences (rule-set or `verified` changes for 2.3/2.4). If an observation contradicts design §4.2 (reported-edge eligibility) STOP and ask the user before Task 14.
- [ ] **Step 4: Commit**

```bash
git add docs test/e2e/fixtures/observe
git commit -m "docs: record delegation and attachment status observations (D-021)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 2: Scale world and the Phase 1 baseline

Runs on Phase 1 code, before Task 3 touches the engine.

**Files:**
- Create: `internal/testutil/scale.go`, `internal/testutil/scale_test.go`, `pkg/engine/bench_test.go`, `docs/phases/phase-2/bench/2.1-baseline.txt`
- Modify: `Makefile` (target `bench`)

**Interfaces:**
- Produces:
  - `type Tier struct { Name string; Gateways, ListenersPerGateway, HTTPRoutes int; ParentRefsMean float64; ListenerSets int; DelegatedPct float64; MaxDepth, MaxFanOut, ParentMatches int; Namespaces, Backends int; LatticeDepth int }`
  - `var Typical, Large, Extreme Tier` (values from Global Constraints)
  - `func ScaleWorld(t Tier, seed int64) (objs []*unstructured.Unstructured, nss []*corev1.Namespace)` — deterministic per seed; every route carries `AcceptedBy` status for its parents except the 5 % failed parentRefs (`Accepted=False/NotAllowedByListeners`); delegated children carry a reported status entry for their parent route; Backends are referenced by backendRefs.
  - `func ScaleSnapshot(t Tier, seed int64) *cache.Snapshot`

- [ ] **Step 1: Write the failing shape test**

```go
func TestScaleWorldMatchesTier(t *testing.T) {
	objs, nss := ScaleWorld(Large, 1)
	c := countKinds(objs) // test helper: kind → count
	if c["Gateway"] != 100 || c["ListenerSet"] != 200 || c["Backend"] != 200 || len(nss) != 100 { t.Fatalf("%v", c) }
	if got := c["HTTPRoute"]; got != 2000 { t.Fatalf("HTTPRoutes %d", got) }
	for _, k := range []string{"GRPCRoute", "TCPRoute", "TLSRoute"} { if c[k] != 100 { t.Fatalf("%s %d, want 5%% of 2000", k, c[k]) } }
	if d := delegatedChildren(objs); d < 190 || d > 210 { t.Fatalf("delegated %d", d) }
	if maxDepth(objs) > 3 || maxFanOut(objs) > 50 { t.Fatal("delegation shape") }
	a, _ := ScaleWorld(Large, 1); b, _ := ScaleWorld(Large, 1)
	if !reflect.DeepEqual(a, b) { t.Fatal("not deterministic") }
}
```

- [ ] **Step 2: Run** `go test ./internal/testutil -run TestScaleWorldMatchesTier` → FAIL (`undefined: ScaleWorld`).
- [ ] **Step 3: Implement `ScaleWorld`** with `math/rand/v2` seeded by `seed`; names `ns-%03d`, `gw-%03d`, `r-%05d`; parentRef count per route drawn so the mean matches `ParentRefsMean`; one in four namespaces labelled `team=web` with one `Selector` listener per Gateway selecting it.
- [ ] **Step 4: Run** the test → PASS.
- [ ] **Step 5: Write `BenchmarkCompute/{typical,large,extreme}`** in `pkg/engine/bench_test.go` over `ScaleSnapshot` with `b.ReportAllocs()`, one `Compute` per iteration, plus `TestScaleComputeSanity` asserting one install and 2,000 owned HTTPRoutes at Large minus the delegated-only children (Phase 1 does not own them).
- [ ] **Step 6: Add the Makefile target** `bench: go test -tags bench -run 'Budget' -bench . -benchmem -count 5 ./pkg/engine ./pkg/cache ./pkg/api`.
- [ ] **Step 7: Record the baseline:** `go test -run '^$' -bench BenchmarkCompute -benchmem -count 5 ./pkg/engine | tee docs/phases/phase-2/bench/2.1-baseline.txt`, prefixed with `go version`, CPU model and commit.
- [ ] **Step 8: Commit** `test(engine): scale world per tier and Phase 1 compute baseline` (with the trailer line).

### Task 3: Trigger signal, cache notifications and the source fingerprint

**Files:**
- Create: `pkg/trigger/trigger.go`, `pkg/trigger/trigger_test.go`, `pkg/cache/fingerprint.go`, `pkg/cache/fingerprint_test.go`
- Modify: `pkg/cache/cache.go:46-57` (Snapshot.Source), `:62-70` (Config.Now, Config.Changes), `:81-136` (progress notifies on transitions), `:187-193` (event handler per informer), `:196-197` (clock), `:242-247` (notify on sync), `:265-305` (fingerprint in the same pass)
- Test: `pkg/cache/cache_test.go`

**Interfaces:**
- Produces:
  - `package trigger`: `type Signal struct{...}`; `func New() *Signal`; `func (s *Signal) Notify()` (nil-safe, never blocks; buffered channel of 1); `func (s *Signal) C() <-chan struct{}`
  - `type KindPrint struct { Count int; Hash uint64 }`
  - `type Fingerprint struct { Kinds map[string]KindPrint; Access map[string]Access }` — keys are `GVR.String()`, including `NamespacesGVR`, `PodsGVR`, `DeploymentsGVR`
  - `func (f Fingerprint) Equal(o Fingerprint) bool`
  - `type Fingerprinter interface { Fingerprint() Fingerprint }`; `func (c *Cache) Fingerprint() Fingerprint` (walks the stores, no copy, no decode)
  - `Snapshot.Source Fingerprint` — computed from exactly the objects copied
  - `Config.Now func() time.Time` (nil: `time.Now`); `Config.Changes *trigger.Signal` (nil: no notifications)

- [ ] **Step 1: Write the failing tests**

```go
func TestSignalCoalesces(t *testing.T) { // pkg/trigger
	s := New(); s.Notify(); s.Notify(); s.Notify()
	testutil.Recv(t, s.C(), "first")
	select { case <-s.C(): t.Fatal("three notifies must deliver once"); default: }
	var nilSig *Signal; nilSig.Notify() // must not panic
}

func TestFingerprintTracksStoreNotHandlers(t *testing.T) { // pkg/cache
	// Real cache over fake clients; Changes is a Signal the test never drains.
	c, dyn := startCache(t, testutil.HTTPRoute("shop", "a", nil))
	before := c.Fingerprint()
	if !before.Equal(c.Snapshot().Source) { t.Fatal("live and snapshot fingerprints differ on an unchanged store") }
	updateAndWaitInStore(t, dyn, c, "shop", "a") // waits on the store, not on a handler
	after := c.Fingerprint()
	if after.Equal(before) { t.Fatal("resourceVersion change not reflected") }
	if after.Kinds[httpRoutesGVR.String()].Count != 1 { t.Fatal("count") }
}

func TestFingerprintIsOrderIndependent(t *testing.T) {
	a := printOf([]obj{{"u1", "5"}, {"u2", "7"}}); b := printOf([]obj{{"u2", "7"}, {"u1", "5"}})
	if a != b { t.Fatal("order changed the hash") }
	if printOf([]obj{{"u1", "6"}, {"u2", "7"}}) == a { t.Fatal("rv change not detected") }
}

func TestAccessTransitionNotifies(t *testing.T) {
	// prepare a fake watch reactor that fails once after sync (pattern: api_test TestWatchFailuresAfterSyncAtTheAPI)
	sig := trigger.New(); c := startCacheWith(t, sig, failWatchOnceAfterSync)
	testutil.Drain(sig.C()); injectWatchError(t)
	testutil.Recv(t, sig.C(), "notify on failure")
	if c.Fingerprint().Access[httpRoutesGVR.String()].OK { t.Fatal("access must be failed in the fingerprint") }
}

func TestEventsNotifyAndSyncNotifies(t *testing.T) {
	sig := trigger.New(); c, dyn := startCacheWith(t, sig, nil)
	testutil.Recv(t, sig.C(), "sync notify"); testutil.Drain(sig.C())
	createAndWaitInStore(t, dyn, c, testutil.HTTPRoute("shop", "b", nil))
	testutil.Recv(t, sig.C(), "add notify")
}
```

- [ ] **Step 2: Run** `go test ./pkg/trigger ./pkg/cache -run 'Signal|Fingerprint|Notif'` → FAIL (undefined).
- [ ] **Step 3: Implement.** The per-object hash and the combine are fixed so every provider agrees:

```go
// objHash mixes FNV-64a of uid, a NUL, and resourceVersion through splitmix64.
// Kind hashes are the wrapping sum of objHash: order-independent.
func objHash(uid types.UID, rv string) uint64 {
	h := fnv.New64a(); _, _ = h.Write([]byte(uid)); _, _ = h.Write([]byte{0}); _, _ = h.Write([]byte(rv))
	x := h.Sum64() + 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
```

  `progress.failure`/`recover` call `onChange` only when `err` changes between empty and non-empty. Each informer gets `AddEventHandler(ResourceEventHandlerFuncs{Add, Update, Delete: notify})`. `Start` runs one goroutine per informer that waits for `HasSynced` and notifies once.
- [ ] **Step 4: Run** the tests → PASS; `go test ./pkg/cache ./pkg/api` → PASS (Phase 1 behaviour unchanged).
- [ ] **Step 5: Benchmark** `BenchmarkFingerprint/{large,extreme}` in `pkg/cache/fingerprint_test.go` over a cache seeded from `testutil.ScaleWorld`; report ns/op.
- [ ] **Step 6: Commit** `feat(cache): change notifications and source fingerprint (D-019)`.

### Task 4: Metrics and served-kind notifications

**Files:**
- Modify: `pkg/ctrlmetrics/scrape.go:96-105` (field), `:167-193` (notify after each store), `pkg/kinds/monitor.go:67-99` (field, notify on a changed `Change`)
- Test: `pkg/ctrlmetrics/scrape_test.go`, `pkg/kinds/monitor_test.go`

**Interfaces:**
- Consumes: `trigger.Signal` (Task 3).
- Produces: `Collector.OnResult *trigger.Signal`; `Monitor.OnChange *trigger.Signal` (notified only when `Refresh` yields a `Change` whose `String()` differs from the previous one).

- [ ] **Step 1: Write the failing tests** `TestCollectorNotifiesOnEachResult` (one `Run` tick with a stub scraper → one receive on `OnResult.C()`) and `TestMonitorNotifiesOnlyOnChange` (two `Refresh` calls with the same discovery answer → one receive; a third with a removed kind → a second receive).
- [ ] **Step 2: Run** `go test ./pkg/ctrlmetrics ./pkg/kinds -run Notif` → FAIL.
- [ ] **Step 3: Implement** the two fields and notifies.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(engine): metrics and served-kind triggers (D-019)`.

### Task 5: Scheduler state machine

Pure: no goroutine, no clock, no timers. Task 6 drives it.

**Files:**
- Create: `pkg/engine/sched.go`, `pkg/engine/sched_test.go`

**Interfaces:**
- Produces:
  - `const Quiet = 250 * time.Millisecond; const MaxDelay = time.Second; const ConfirmEvery = time.Second`
  - `type Deadline struct { At time.Time; Strict bool }`; `func (d Deadline) Due(now time.Time) bool` (Strict: `now.After(At)`; else `!now.Before(At)`); `func (d Deadline) IsZero() bool`; `func Earliest(ds ...Deadline) Deadline`
  - `type sched struct{...}` with `trigger(now time.Time)`, `setDeadline(d Deadline)` (replaces the previous deadline), `setSafety(at time.Time)`, `wake() (time.Time, bool)` (next instant the loop must check; a strict deadline wakes at `At.Add(time.Nanosecond)`), `start(now time.Time) bool` (true when idle or pending and due; moves to running and clears pending), `finish()` (running → pending if triggered during the run, else idle), `idle() bool`

- [ ] **Step 1: Write the failing tests**

```go
func TestDebounceBurst(t *testing.T) {
	var s sched; t0 := testutil.Now
	for i := range 500 { s.trigger(t0.Add(time.Duration(i) * 200 * time.Microsecond)) } // 500 events in 100 ms
	last := t0.Add(499 * 200 * time.Microsecond)
	if at, _ := s.wake(); !at.Equal(last.Add(Quiet)) { t.Fatalf("due %v, want 250 ms after the last event", at) }
	if s.start(last.Add(Quiet - time.Nanosecond)) { t.Fatal("started early") }
	if !s.start(last.Add(Quiet)) { t.Fatal("not started when due") }
}

func TestDebounceStreamCapsAtOneSecond(t *testing.T) {
	var s sched; runs := 0; t0 := testutil.Now
	for i := 0; i <= 50; i++ { // one event per 100 ms for 5 s
		now := t0.Add(time.Duration(i) * 100 * time.Millisecond)
		if s.start(now) { runs++; s.finish() }
		s.trigger(now)
	}
	if runs != 5 { t.Fatalf("runs %d, want one per 1 s window", runs) }
}

func TestSingleFlightKeepsTriggerDuringRun(t *testing.T) {
	var s sched; s.trigger(testutil.Now); s.start(testutil.Now.Add(Quiet))
	s.trigger(testutil.Now.Add(Quiet + time.Millisecond)) // during the run
	if s.start(testutil.Now.Add(10 * time.Second)) { t.Fatal("second run while running") }
	s.finish()
	if s.idle() { t.Fatal("a trigger during the run must leave the state pending") }
}

func TestDeadlinesHaveNoDebounce(t *testing.T) {
	var s sched; at := testutil.Now.Add(60 * time.Second)
	s.setDeadline(Deadline{At: at})
	if !s.start(at) { t.Fatal("non-strict deadline is due at its instant") }
	s.finish(); s.setDeadline(Deadline{At: at.Add(time.Minute), Strict: true})
	if s.start(at.Add(time.Minute)) { t.Fatal("strict deadline must wait for the first reading after it") }
	if w, _ := s.wake(); !w.Equal(at.Add(time.Minute + time.Nanosecond)) { t.Fatal("strict wake") }
}

func TestSafetyTimer(t *testing.T) {
	var s sched; s.setSafety(testutil.Now.Add(30 * time.Second))
	if s.start(testutil.Now.Add(29 * time.Second)) { t.Fatal("early") }
	if !s.start(testutil.Now.Add(30 * time.Second)) { t.Fatal("safety timer did not fire") }
}
```

- [ ] **Step 2: Run** `go test ./pkg/engine -run 'Debounce|SingleFlight|Deadlines|Safety'` → FAIL.
- [ ] **Step 3: Implement `sched`.** Pending due time = `min(last+Quiet, first+MaxDelay)`; a run's `start` is allowed when that time, the deadline or the safety instant is due.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(engine): scheduler with debounce, deadlines and single flight (D-019)`.

### Task 6: Engine loop, publication, confirmation and deadlines

**Files:**
- Create: `pkg/engine/loop.go`, `pkg/engine/loop_test.go`, `internal/testutil/clock.go`
- Modify: `pkg/engine/engine.go:25-55` (Config), `:65-74` (Model.Next), `:76-91` (atomic pointer), `:143-185` (Compute publishes and collects deadlines), `:187-192` (Current), `:207-222` (Run replaced by loop.go)
- Modify: `pkg/engine/engine_test.go:226-249` (`TestRunCallsOnComputeWithEachModel` reads `Current().Model`), `pkg/app/app_test.go` (one `Current()` call), `go.mod` (`k8s.io/utils` direct)

**Interfaces:**
- Consumes: `sched`, `Deadline` (Task 5); `cache.Fingerprinter`, `Snapshot.Source` (Task 3); `trigger.Signal`.
- Produces:
  - `type Source struct { Cache cache.Fingerprint; Metrics uint64; Kinds string }`; `func (s Source) Equal(o Source) bool`
  - `type Published struct { Model *Model; Source Source; ConfirmedAt time.Time }` — never mutated after publication
  - `func (e *Engine) Current() *Published` (nil before the first publication)
  - `func (e *Engine) Compute() *Model` — computes and publishes `Published{m, src, snap.TakenAt}`
  - `func (e *Engine) Run(ctx context.Context, safety time.Duration)` — the scheduler goroutine
  - `Model.Next Deadline` — earliest future grace expiry (`firstSeen + health.Grace`, non-strict) and metrics staleness (`CollectedAt + MetricsMaxAge`, strict); 2.3 adds KGWUI-012 maturation through `func (d *deadlines) add(at time.Time, strict bool)`
  - `Config.Triggers *trigger.Signal`; `Config.Clock clock.WithTicker` (nil: `clock.RealClock{}`); `Config.OnPublish func(*Published)` (test hook, called on the loop goroutine after each swap)
  - Tests set `Config.Now` to the fake clock's `Now` and `cache.Config.Now` likewise, so snapshot, compute and confirmation times share one clock.
  - `internal/testutil`: `func FakeClock() *clocktesting.FakeClock` at `Now`; `func StepUntil(t *testing.T, c *clocktesting.FakeClock, d time.Duration)` steps once waiters are registered (`HasWaiters`), bounded by `RecvTimeout`
- Metrics identity: FNV-64a over `(install ID, CollectedAt.UnixNano, Err)` of every `Metrics.Get` the run made, in install order. Kinds identity: `KindsChange().String()`.

- [ ] **Step 1: Write the failing tests** (fake clock; a `fakeSource` implementing `cache.Snapshotter` and `cache.Fingerprinter` whose store can change without notifying, and whose `Snapshot` can block on a channel)

```go
func TestOneRecomputePerBurst(t *testing.T)            // 500 Notify within 100 ms → exactly one OnPublish, at last+250 ms
func TestStreamRecomputesOncePerSecond(t *testing.T)   // Notify every 100 ms for 5 s → 5 publications
func TestTriggerDuringRunGivesOneMoreRun(t *testing.T) // block Snapshot, Notify ×3, unblock → exactly 2 runs
func TestSafetyTimerOnly(t *testing.T) {               // no triggers: none at 29 s, one at 30 s; safety 2s → one per 2 s
}
func TestChangeDuringRunPublishesOldThenNew(t *testing.T) {
	// snapshot taken (fingerprint F1), store moves to F2 during the run, Notify.
	// First publication: Source.Cache == F1. Confirm tick before the second run: ConfirmedAt unchanged.
	// Second publication: Source.Cache == F2.
}
func TestConfirmTickCatchesChangeBeforeHandler(t *testing.T) {
	// Review Focus 1. Idle, published F1. src.setPrint(F2) without Notify (handler paused).
	// StepUntil 1 s: no confirmation (ConfirmedAt unchanged), state pending.
	// StepUntil 250 ms: a run publishes F2 although Notify never fired.
	// Later Notify (handler resumes): one more run, publishing an equal Source.
}
func TestAccessTransitionBlocksConfirmation(t *testing.T) // only Access changes in the fingerprint → no confirm, one run
func TestBlockedRunAgesConfirmedAt(t *testing.T) {
	// Run blocks forever; step 10 s: Current().ConfirmedAt and Model.ComputedAt stay at the last completed run.
}
func TestConcurrentReadsSeeOnePublished(t *testing.T) {
	// 8 readers loop on Current() while publications and confirmations swap; each read's
	// Model, Source and ConfirmedAt satisfy p.Source.Cache.Equal(p.Model.Snapshot.Source) and !p.ConfirmedAt.Before(p.Model.Snapshot.TakenAt). Run under -race.
}
func TestConfirmAdvancesOnlyConfirmedAt(t *testing.T) {
	// idle: each 1 s step → ConfirmedAt == clock.Now(), Model pointer and ComputedAt unchanged.
	// at publication ConfirmedAt == snapshot TakenAt <= ComputedAt; after the first tick ConfirmedAt > ComputedAt.
}
func TestGraceDeadlineTurnsFindingToWarning(t *testing.T) {
	// a route without status first seen at T: no trigger; at T+60 s exactly one run; KGWUI-003 info → warning.
}
func TestStrictMetricsDeadline(t *testing.T) {
	// MetricsMaxAge 30 s, CollectedAt C: a run at exactly C+30 s keeps metrics current;
	// the scheduler runs at the next reading after the boundary and that run raises KGWUI-008 stale.
}
func TestMetricsResultTriggersOneRecompute(t *testing.T) // Collector.OnResult shares Config.Triggers → one run
```

- [ ] **Step 2: Run** `go test ./pkg/engine -race -run 'Burst|Stream|Trigger|Safety|Change|Confirm|Access|Blocked|Concurrent|Deadline|Metrics'` → FAIL.
- [ ] **Step 3: Implement.** `Compute` keeps `computeMu`; publication is `atomic.Pointer[Published]`. `Run` owns one `sched`; it selects on `ctx.Done()`, `Triggers.C()`, a timer reset to `sched.wake()`, a `ConfirmEvery` ticker and a `done` channel from the run goroutine. A run executes `Compute` in its own goroutine so the loop keeps receiving triggers; after it, `setDeadline(m.Next)` and `setSafety(now + safety)`. The confirm tick runs only when `sched.idle()`: it builds the live `Source` (`Fingerprint()` when the snapshotter implements `cache.Fingerprinter`, else `Snapshot().Source`) and either swaps `Published{p.Model, p.Source, now}` or calls `sched.trigger(now)`. `Run` computes immediately once at start, as Phase 1 does.
- [ ] **Step 4: Run** the new tests and `go test ./pkg/engine ./pkg/app -race` → PASS (Phase 1 engine tests unchanged apart from `Current().Model`).
- [ ] **Step 5: Commit** `feat(engine): event-driven recompute with atomic publication and confirmation (D-019)`.

### Task 7: API timestamps and the additive-only golden check

**Files:**
- Modify: `pkg/api/types.go:12-27` (`ConfirmedAt *time.Time \`json:"confirmedAt,omitempty"\``), `pkg/api/handlers.go:25-29` (`ModelSource.Current() *engine.Published`), `:69-80`, `:82-94`, `:96-108`, `:161-175` (one `Current()` per request), `:192-196` and `:220-224` (`collectedAt` = `ConfirmedAt`), `:202-218` (`wireHealth(rep, confirmedAt)` sets `CollectedAt` on the copy), `:230-252` (`Collected-At` = `ConfirmedAt`)
- Modify: `pkg/api/api_test.go:108-171` (harness passes `clk.now` as `cache.Config.Now`), `:875-879` (`staticModel.Current`)
- Create: `pkg/api/testdata/golden-v1/*.json` (copy of today's `testdata/golden/*.json`, never regenerated), `pkg/api/additive_test.go`
- Regenerate: `pkg/api/testdata/golden/{installs,bare-installs}.json`

**Interfaces:**
- Consumes: `engine.Published` (Task 6).
- Produces: wire field `confirmedAt` on each install; `func handler.load(w) *engine.Published` (503 when not synced); `func additive(t *testing.T, old, cur any, path string)` (test helper: every key and value of `old` exists in `cur`; a `"<snapshot time>"` string in `old` matches any UTC RFC 3339 value).

- [ ] **Step 1: Write the failing tests**

```go
func TestGoldenChangesAreAdditive(t *testing.T) {
	for _, name := range []string{"meta", "installs", "overview", "health", "resource", "not-found", "bare-meta", "bare-installs", "bare-overview"} {
		additive(t, readGolden(t, "golden-v1", name), readGolden(t, "golden", name), name)
	}
}
func TestTimestampsPerMeaningAtTheAPI(t *testing.T) {
	// Engine with a fake clock: Compute at T0 (snapshot T0), then a confirm at T0+5 s.
	// installs: computedAt == T0, confirmedAt == T0+5 s; overview, health collectedAt and the
	// Collected-At header == T0+5 s; health checks[].collectedAt and findings[].since keep their own values.
}
func TestRequestReadsOnePublished(t *testing.T) // a ModelSource whose Current() counts calls: each handler calls it once
```

- [ ] **Step 2: Run** `go test ./pkg/api -run 'Additive|TimestampsPerMeaning|OnePublished'` → FAIL.
- [ ] **Step 3: Implement** the handler changes; regenerate goldens with `go test ./pkg/api -run Golden -update`. The only diff in `git diff pkg/api/testdata/golden` is `confirmedAt` in the two installs files and `"<snapshot time>"` turning into the pinned time.
- [ ] **Step 4: Run** `go test ./pkg/api` and `TZ=Europe/Amsterdam go test ./pkg/api` → PASS.
- [ ] **Step 5: Commit** `feat(api): confirmedAt and confirmation-time collectedAt (D-019)`.

### Task 8: SPA timestamp consumers

**Files:**
- Create: `web/packages/shell/src/api/freshness.ts`, `web/packages/shell/src/api/freshness.test.ts`
- Modify: `web/packages/shell/src/api/types.ts:82-86` (`confirmedAt?: string`), `src/pages/ClusterLanding.tsx:131`, `src/shell/InstallSwitcher.tsx:51`, `src/shell/Header.tsx:161`, `src/api/types.golden.test.ts` (InstallSummary spec gains `confirmedAt: "?"`), `src/test/fixtures.ts:233-240` (`installSummary(at, { confirmedAt?, computedAt? })`)
- Test: `src/pages/pages.test.tsx`, `src/shell/Shell.test.tsx`

**Interfaces:**
- Produces: `export function confirmedOrComputed(s: Pick<InstallSummary, "confirmedAt" | "computedAt">): string`, exported from `index.ts`.

- [ ] **Step 1: Write the failing tests**

```ts
it("confirmedAt keeps an idle install fresh", () => {
  vi.useFakeTimers({ now: T });
  stubApi({ "/api/v1/installs": [installSummary(T, { computedAt: iso(T - 30_000), confirmedAt: iso(T) })] });
  renderWithTheme(<Shell pages={defaultPages} />, { path: "/" });
  // cluster card, switcher entry and header readiness icon show the health state, not "unknown"
  expect(await screen.findByTestId("install-card-kgw.kgateway")).toHaveTextContent(/error/i);
});
it("falls back to computedAt from an older backend", () => {
  // no confirmedAt, computedAt 30 s old → all three show "unknown"
});
test("confirmedOrComputed prefers confirmedAt", () => {
  expect(confirmedOrComputed({ computedAt: "a", confirmedAt: "b" })).toBe("b");
  expect(confirmedOrComputed({ computedAt: "a" })).toBe("a");
});
```

- [ ] **Step 2: Run** `cd web && npx vitest run packages/shell/src/api/freshness.test.ts packages/shell/src/pages/pages.test.tsx` → FAIL.
- [ ] **Step 3: Implement**; the three consumers call `isStale(confirmedOrComputed(i), now)`.
- [ ] **Step 4: Run** `cd web && npm test && npm run typecheck && npm run lint` → PASS.
- [ ] **Step 5: Commit** `feat(shell): freshness reads confirmedAt with a computedAt fallback (D-019)`.

### Task 9: Wiring, the safety-timer flag and the 2.1 budgets

**Files:**
- Modify: `pkg/app/app.go:39-40` (doc: maximum time between recomputes), `:96` (`cache.Config.Changes`), `:105` (`col.OnResult`), `:90` (`mon.OnChange`), `:114-121` (`engine.Config.Triggers`), `:131` (`Run(ctx, PollInterval)` as the safety timer)
- Modify: `cmd/kgateway-ui/main.go:23` (default `30*time.Second`, help "maximum time between model recomputes (safety timer)")
- Modify: `README.md:44`, `deploy/helm/kgateway-ui/README.md` (a "Recompute" note: event-driven, `--poll-interval` is the safety timer, 2s restores Phase 1's cadence as a floor)
- Create: `pkg/engine/budget_test.go` (`//go:build bench`)
- Test: `pkg/app/app_test.go`

**Interfaces:** Consumes Tasks 3–6. Produces `make bench` output for the 2.1 record.

- [ ] **Step 1: Write the failing wiring test** `TestAppRecomputesOnObjectChange`: build the app over fake clients with `PollInterval: time.Hour`; create an HTTPRoute; `testutil.Recv` on a channel fed by `OnPublish` until a published snapshot contains it (no safety timer can explain it).
- [ ] **Step 2: Run** `go test ./pkg/app -run RecomputesOnObjectChange` → FAIL (times out: ticker is an hour).
- [ ] **Step 3: Implement** the wiring and the flag default.
- [ ] **Step 4: Run** `go test ./... -race` → PASS.
- [ ] **Step 5: Write the budget tests** (real cache over fake clients seeded with `ScaleWorld(Large, 1)`):

```go
func TestBudgetBurst(t *testing.T) {
	// 1,000 updates within 1 s; record each update's time and the first publication whose
	// Snapshot holds its resourceVersion. Assert: publications during the burst window + 2 s ≤ 2;
	// p95 event-to-publication ≤ 1.2 s. Log p50/p95/p99.
}
func TestBudgetIdleTenMinutes(t *testing.T) {
	// fake clock; 600 one-second steps with no change: publications only at 30 s multiples (20)
	// and at Model.Next deadlines; 600 confirmations.
}
func TestBudgetConfirmTick(t *testing.T) { // Fingerprint at Large: median of 50 ≤ 5 ms; Extreme reported
}
```

- [ ] **Step 6: Run** `make bench | tee docs/phases/phase-2/bench/2.1.txt` → budget tests PASS; the file records machine and Go version.
- [ ] **Step 7: Commit** `feat(app): wire recompute triggers; --poll-interval becomes the safety timer (D-019)`.

### Task 10: Close sub-phase 2.1

**Files:**
- Create: `docs/phases/phase-2/phase-2.1-complete.md`, `docs/phases/phase-2/reviews/2.1-codex.md`
- Modify: `docs/phase-status.md`, `docs/phases/phase-2/design.md` (only where the code deviates)

- [ ] **Step 1: Run the gates** and paste the outputs into the record: `go vet ./... && go vet -tags e2e ./...`, `go test ./... -race`, `TZ=Europe/Amsterdam go test ./pkg/api`, `make lint`, `cd web && npm test && npm run typecheck && npm run lint && npm run build`, the forbidden-terms check, and the kind e2e matrix (CI or `make e2e` for both pairs).
- [ ] **Step 2: Request the cross-model review**

```bash
codex exec --sandbox read-only -o docs/phases/phase-2/reviews/2.1-codex.md "Review sub-phase 2.1 of the kgateway UI (diff <base>..HEAD) against docs/phases/phase-2/design.md §3.1, §9.3, §9.6 and D-019. Focus: confirmation can never stamp an old model as current; single flight; deadlines strict vs non-strict; atomic publication; additive-only goldens; SPA fallback. Test output: <paste>. Rank findings P1–P4 with file:line." < /dev/null
```

- [ ] **Step 3: Triage** every finding in the review file (accept, reject with reason, defer to Carry-over); fix accepted ones with tests; rerun Step 1.
- [ ] **Step 4: Write `phase-2.1-complete.md`** in the Phase 1 record format (Summary, Reviews, Waivers, Deliverables with gate outputs, benchmark table: baseline vs 2.1 per tier) and update `docs/phase-status.md` (queue, sub-phase none, next 2.2).
- [ ] **Step 5: Commit** `docs: close sub-phase 2.1`.

---

# Sub-phase 2.2 — Explorer on observed status

### Task 11: The `Backend` kind and the ListenerSet kind contract

**Files:**
- Modify: `pkg/kinds/kinds.go:1-3` (package doc: a downstream ListenerSet kind registers with `CategoryListenerSet` and follows `spec.parentRef`, `spec.listeners[]`, `status.listeners[]`), `:17-29` (`CategoryBackend Category = "backend"`), `pkg/kinds/upstream.go:17-34` (Backend descriptor)
- Modify: `web/packages/shell/src/api/types.ts:5` (`"backend"` in `Category`)
- Test: `pkg/kinds/kinds_test.go`, `pkg/scopeverify/verify_test.go`, `deploy/helm/chart_test.go`

**Interfaces:** Produces `kinds.CategoryBackend`; descriptor `{Group: "gateway.kgateway.dev", Kind: "Backend", Resource: "backends", Versions: ["v1alpha1"], Namespaced: true, Category: CategoryBackend, DisplayName: "Backends"}`.

- [ ] **Step 1: Write the failing tests** `TestUpstreamHasBackend` (descriptor as above), `TestVerifierIgnoresBackend` (a Backend in scope does not change KGWUI-007's comparison) and `TestClusterRoleCoversBackends` (rendered ClusterRole grants get/list/watch on `gateway.kgateway.dev` `*` or `backends`).
- [ ] **Step 2: Run** `go test ./pkg/kinds ./pkg/scopeverify ./deploy/helm -run 'Backend'` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** → PASS. `meta.json` gains the Backend kind in Task 25.
- [ ] **Step 5: Commit** `feat(kinds): register the Backend kind`.

### Task 12: Typed views

**Files:**
- Create: `pkg/objects/gateway.go`, `pkg/objects/httproute.go`, `pkg/objects/backend.go`, `pkg/objects/decoder.go`, `pkg/objects/views_test.go`
- Modify: `pkg/objects/objects.go:172-187` (`ListenerStatus.SupportedKinds []GroupKind`), `internal/testutil/fixtures.go` (builders below)

**Interfaces:**
- Produces (all decoders never fail; a malformed field yields its zero value; design §4.1):
  - `type GroupKind struct { Group string \`json:"group"\`; Kind string \`json:"kind"\` }`
  - `type Listener struct { Name string; Port int32; Protocol, Hostname string; TLS *ListenerTLS; AllowedRoutes AllowedRoutes }`; `type ListenerTLS struct { Mode string; CertificateRefs []Ref }`; `type AllowedRoutes struct { Namespaces RouteNamespaces; Kinds []GroupKind }`; `type RouteNamespaces struct { From string; Selector *metav1.LabelSelector }` (From defaults to `Same`)
  - `type GatewayView struct { Class string; Listeners []Listener; SpecAddresses, StatusAddresses []string; AttachedListenerSets *int32; AllowedListeners map[string]any; Conditions []metav1.Condition; ListenerStatus []ListenerStatus }` — `AllowedListeners` is `spec.allowedListeners` as written, nil when absent
  - `type ListenerSetView struct { Parent ParentRef; Listeners []Listener; Conditions []metav1.Condition; ListenerStatus []ListenerStatus }`
  - `type HTTPRouteView struct { Hostnames []string; ParentRefs []ParentRef; Rules []Rule; Parents []RouteParentStatus; Labels map[string]string; InheritParentMatcher bool }`
  - `type Rule struct { Index int; Name string; Matches []Match; Filters []Filter; BackendRefs []BackendRef }` — `Matches` nil when omitted, empty when `matches: []`
  - `type Match struct { Path *PathMatch; Headers, QueryParams []KeyMatch; Method string }`; `type PathMatch struct { Type, Value string }`; `type KeyMatch struct { Type, Name, Value string }` (Type defaults to `Exact`)
  - `type Filter struct { Type string; ExtensionRef *Ref; Mirror *MirrorFilter; Config map[string]any }` — slices keep the order as written
  - `type MirrorFilter struct { Backend Ref; Port int32; Percent *int32; Fraction *Fraction }`; `type Fraction struct { Numerator, Denominator int32 }` (Denominator defaults to 100) — `RequestMirror` filters on rules and on backendRefs
  - `type BackendRef struct { Ref Ref; Port int32; Weight int32; WeightDefaulted bool; Filters []Filter; Delegation *DelegationRef }` — Kind defaults to `Service`, Namespace to the route's; an absent weight is 1 with `WeightDefaulted`; an absent port stays 0 and is never sent as 0
  - `type DelegationMode string` (`DelegationName`, `DelegationWildcard`, `DelegationLabel`); `type DelegationRef struct { Mode DelegationMode; Namespace, Name, Label string }` — Namespace defaults to the parent's; classification exactly as computed-rules §3.2: `name`/`wildcard` only when group is `gateway.networking.k8s.io` and kind `HTTPRoute`, both set (name `""` or `"*"` is wildcard); `label` only when group `delegation.kgateway.dev` and kind `label`, both set, with `name` as the label value; anything else is an ordinary backend
  - `func BackendTargets(v *HTTPRouteView) []BackendTarget`; `type BackendTarget struct { Ref Ref; Mirror bool }` — every rule backendRef and every `RequestMirror` target after defaults; delegation refs excluded
  - `type BackendView struct { Type string; Conditions []metav1.Condition }`; `type RouteHostnames struct { Hostnames []string }`
  - `func DecodeGateway(u) *GatewayView`, `DecodeListenerSet`, `DecodeHTTPRoute`, `DecodeBackend`, `DecodeRouteHostnames` (all `u *unstructured.Unstructured`)
  - `type Decoder interface { Gateway(u) *GatewayView; ListenerSet(u) *ListenerSetView; HTTPRoute(u) *HTTPRouteView; Backend(u) *BackendView; RouteHostnames(u) *RouteHostnames }`; `type Direct struct{}` (decodes every call)
  - testutil: `Rule(matches ...map[string]any) map[string]any`, `DelegateTo(ns, name string) map[string]any`, `DelegateWildcard(ns string)`, `DelegateLabel(ns, label string)`, `WithRules(u, rules ...map[string]any)`, `WithHostnames(u, h ...string)`, `Backend(ns, name string, conds []metav1.Condition)`, `HTTPRouteRef(ns, name string) objects.ParentRef`

- [ ] **Step 1: Write the failing tests** (table-driven over unstructured inputs):

```go
func TestDecodeHTTPRouteDefaults(t *testing.T) {
	v := DecodeHTTPRoute(route(`{"rules":[{"backendRefs":[{"name":"svc","port":80}]},{"matches":[]}]}`))
	if v.Rules[0].Matches != nil || v.Rules[1].Matches == nil || len(v.Rules[1].Matches) != 0 { t.Fatal("omitted vs empty matches") }
	b := v.Rules[0].BackendRefs[0]
	if b.Ref.Kind != "Service" || b.Ref.Namespace != "shop" || b.Weight != 1 || b.Delegation != nil { t.Fatalf("%+v", b) }
}
func TestDecodeDelegationModes(t *testing.T)     // name; "*" and "" wildcard; label; nil group or kind is never a delegation ref; namespace defaults to the parent's
func TestDecodeMirrorWeightPortOrder(t *testing.T) // RequestMirror with percent and with fraction; absent weight (1, defaulted) vs explicit 1 vs explicit 0; absent port 0 vs explicit; three filters of different types keep their order, in a rule and in a backendRef
func TestBackendTargets(t *testing.T)              // Service, Backend and mirror target listed, mirror marked; delegation refs absent
func TestAllowedListenersAsWritten(t *testing.T)   // absent → nil; set → the map as written
func TestDecodeHeaderTypeDefaultsExact(t *testing.T)
func TestInheritParentMatcherValues(t *testing.T) // true/yes/enabled any case → true; "on", "" → false
func TestDecodeListenerDefaults(t *testing.T)     // Port 0 when unset; From "Same"; TLS refs only, never resolved
func TestMalformedFieldsGiveZeroValues(t *testing.T) // listeners: "x" → empty view, no panic
func TestDirectDecodesEveryCall(t *testing.T)     // two calls → two distinct pointers
```

- [ ] **Step 2: Run** `go test ./pkg/objects -run 'Decode|Inherit|Malformed|Direct'` → FAIL.
- [ ] **Step 3: Implement** with `runtime.DefaultUnstructuredConverter.FromUnstructured` into raw structs (pointer fields for defaults), as `objects.go:116-123` does.
- [ ] **Step 4: Run** `go test ./pkg/objects` → PASS.
- [ ] **Step 5: Commit** `feat(objects): typed views for Gateways, ListenerSets, HTTPRoutes and Backends`.

### Task 13: Decode cache

**Files:**
- Create: `pkg/engine/decode.go`, `pkg/engine/decode_test.go`
- Modify: `pkg/engine/engine.go:76-91` (field), `:110-130` (`observe` prunes the cache)

**Interfaces:**
- Consumes: `objects.Decoder`, `Decode*` (Task 12).
- Produces: `type decodeCache struct{...}` implementing `objects.Decoder`, keyed by `(uid, resourceVersion)` per view type; `func (d *decodeCache) prune(live map[types.UID]string)`; objects without uid or resourceVersion bypass it.

- [ ] **Step 1: Write the failing tests** `TestDecodeCacheReusesUnchangedObjects` (same uid/rv twice → same pointer), `TestDecodeCacheMissesOnNewResourceVersion`, `TestDecodeCacheBypassWithoutKeys` (no uid → two distinct pointers), `TestDecodeCachePrunedWithObserve` (object removed from the snapshot → entry gone after `Compute`).
- [ ] **Step 2: Run** `go test ./pkg/engine -run DecodeCache` → FAIL.
- [ ] **Step 3: Implement**; the cache is only touched under `computeMu`.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(engine): decode cache keyed by uid and resourceVersion`.

### Task 14: Reported edges and status-based ownership of delegated children (Q12)

**Files:**
- Create: `pkg/install/delegation.go`, `pkg/install/delegation_test.go`
- Modify: `pkg/install/assign.go:14-22` (`OwnedObject.RouteParents`), `:32-40` (Owned fields of Task 15 declared here as empty), `:146-151` (HTTPRoute joins the parent kinds), `:153-189` (Gateway-attached routes seed the BFS), end of `Assign` (call `delegate`)
- Test: `pkg/api/api_test.go` (new tests below)

**Interfaces:**
- Produces:
  - `OwnedObject.RouteParents []objects.ParentRef` — reported HTTPRoute parents (status parentRef after defaults)
  - `func delegate(snap *cache.Snapshot, served []kinds.Served, insts []Resolved, a *Assignment, inScope func(Resolved, string) bool)` — BFS from Gateway-attached HTTPRoutes over reported edges only; visited set per install; linear in routes plus edges
  - `func ReportedEdge(child *unstructured.Unstructured, parent objects.Ref, controllers []string) (entry *objects.RouteParentStatus, ok bool)` — design §4.2 conditions 2 and 3 (scope and ownership checked by the caller); eligibility: a current spec parentRef equal after defaults, or an empty spec list
  - `func (o *Owned) Route(ref objects.Ref) (*OwnedObject, bool)`

- [ ] **Step 1: Write the failing tests** (unit in `pkg/install`, boundary in `pkg/api`):

```go
func TestReportedEdgeNeedsEligibility(t *testing.T) {
	p := testutil.HTTPRouteRef("shop", "api")
	child := testutil.HTTPRoute("shop", "c", []objects.ParentRef{p}, testutil.AcceptedBy(p, testutil.Controller))
	if _, ok := ReportedEdge(child, p.Ref(), []string{testutil.Controller}); !ok { t.Fatal("matching spec + status is an edge") }
	moved := testutil.HTTPRoute("shop", "c", []objects.ParentRef{testutil.HTTPRouteRef("shop", "other")}, testutil.AcceptedBy(p, testutil.Controller))
	if _, ok := ReportedEdge(moved, p.Ref(), []string{testutil.Controller}); ok { t.Fatal("a removed ref keeps no edge") }
	implicit := testutil.HTTPRoute("shop", "c", nil, testutil.AcceptedBy(p, testutil.Controller))
	if _, ok := ReportedEdge(implicit, p.Ref(), []string{testutil.Controller}); !ok { t.Fatal("empty list falls back to status") }
}
func TestStaleEligibleEntryKeepsEdge(t *testing.T) { // Review Focus 3, at the API
	// child generation 2, entry observedGeneration 1 (testutil stale()): child is in Owned, the HTTPRoute tile
	// count is equal before and after the edit, KGWUI-003 info with "resource status" pending inside grace,
	// warning after; no KGWUI-011.
}
func TestDelegationShapesOwnership(t *testing.T)  // three-level chain, diamond (C owned once, two RouteParents), cycle stops
func TestImplicitEdgeRules(t *testing.T)          // empty parentRefs + status → child; gaining a Gateway parentRef removes the implicit edge
func TestChildReportedByAnotherInstall(t *testing.T) // not owned here; a child reported by parents in two installs is owned by both
func TestQ12CountsBeforeAndAfter(t *testing.T) {
	// One world, asserted against the Phase 1 numbers recorded inline: a mixed-role route owned once and judged on
	// its Gateway and its HTTPRoute parent; a child of two parents counted once; two installs sharing a controller
	// name; a removed parentRef; a namespace leaving the scope; a stale implicit edge. Assert tile counts, findings and roll-up.
}
```

- [ ] **Step 2: Run** `go test ./pkg/install ./pkg/api -run 'ReportedEdge|StaleEligible|DelegationShapes|ImplicitEdge|AnotherInstall|Q12'` → FAIL.
- [ ] **Step 3: Implement.** A reverse index `parentRef → []child` is built once from `status.parents` of every HTTPRoute whose `controllerName` is one of the install's; the BFS queue starts with owned HTTPRoutes and appends a child when `ReportedEdge` holds, the child is in scope, and it is not visited. A mixed route keeps its Gateway `Parents` and gains `RouteParents`.
- [ ] **Step 4: Run** → PASS; `go test ./...` → PASS except goldens touched by Q12 (none: the golden world has no delegation).
- [ ] **Step 5: Commit** `feat(install): own delegated children through reported edges (Q12)`.

### Task 15: Candidates: declared, no-parent, missing HTTPRoute parents, unwatched, Backends

**Files:**
- Modify: `pkg/install/delegation.go`, `pkg/install/assign.go:24-61` (types), `:172-187` (missing HTTPRoute parent → `Unattached`; out-of-scope route naming an owned route → `Unwatched`), `pkg/health/checks.go:604-622` (detail "targets <Kind> ns/name"; Gateway text unchanged), `pkg/overview/overview.go:98-110` (count `Owned.Backends`), `pkg/api/handlers.go:156` (keep visible Backends)
- Test: `pkg/install/delegation_test.go`, `pkg/api/api_test.go`

**Interfaces:**
- Produces:
  - `type DeclaredCandidate struct { Route OwnedObject; Parent objects.ParentRef }` — in scope, a current spec parentRef to an owned HTTPRoute, no eligible entry current or stale
  - `Owned.Declared []DeclaredCandidate`; `Owned.NoParent []OwnedObject` (in scope, empty `spec.parentRefs`, no status entry from the install's controllers); `Owned.Backends []*unstructured.Unstructured` (in scope and named by full identity by a backendRef of an owned route); `Owned.ReferencedBackends []objects.Ref` (all such refs, in scope or not, for admission)
  - `UnwatchedRoute.Gateway` now holds any owned parent (Gateway, ListenerSet or HTTPRoute)

- [ ] **Step 1: Write the failing tests**

```go
func TestDeclaredCandidateIsNotAChild(t *testing.T) // in Owned.Declared, not in Routes or RouteParents; not counted
func TestUnwatchedDeclaredRouteGetsOnlyKGWUI004(t *testing.T) {
	// API: exactly one finding (KGWUI-004), no KGWUI-011, "resource status" OK, install health unchanged.
}
func TestMissingHTTPRouteParentIsKGWUI003(t *testing.T)
func TestKGWUI004DetailNamesParentKind(t *testing.T) // "targets HTTPRoute shop/api", "targets ListenerSet …"; the Gateway case is byte-identical to Phase 1
func TestNoParentRowsInEveryInstallScope(t *testing.T) // two installs whose scopes hold the route: both list it, neither counts it
func TestBackendAssignment(t *testing.T) {
	// referenced in scope → Owned.Backends and the tile ("in use"); unreferenced → no install;
	// referenced out of scope → only in ReferencedBackends.
}
```

- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./pkg/install ./pkg/api ./pkg/health` → PASS.
- [ ] **Step 5: Commit** `feat(install): declared, no-parent and Backend candidates`.

### Task 16: Health for reported edges and KGWUI-011's status trigger

**Files:**
- Create: `pkg/health/delegation.go`, `pkg/health/delegation_test.go`
- Modify: `pkg/health/finding.go:41-53` (`CodeDelegatedNotAttached = "KGWUI-011"`), `:59-75` (`Evidence []objects.Ref \`json:"-"\``), `pkg/health/checks.go:187-392` (declared candidates inside `conditionsCheck.Run`, which owns the pending result), `:308-326` (evaluate the reported-edge entries of `RouteParents` like Gateway parents), `pkg/health/messages.go` (draft strings, finalised in Task 31)

**Interfaces:**
- Consumes: `Owned.Declared`, `OwnedObject.RouteParents` (Tasks 14, 15).
- Produces: `Finding.Evidence` (the objects whose data a finding's detail quotes; KGWUI-011: the parent route); `func graceStart(in Input, us ...*unstructured.Unstructured) time.Time` (latest first-seen); `func declaredFinding(in Input, d install.DeclaredCandidate) Finding` in `delegation.go`. 2.4 adds the computed and orphan triggers to `delegation.go`.

- [ ] **Step 1: Write the failing tests**

```go
func TestKGWUI011InGraceIsInfoAndPending(t *testing.T) {
	// declared candidate C of P, both first seen at Now-10s: KGWUI-011 info on C, Evidence [P],
	// "resource status" check Pending, report State unknown.
}
func TestKGWUI011AfterGraceIsWarningAndCheckOK(t *testing.T) // grace from the later of C's and P's first-seen
func TestReportedEdgeEntryFalseIsKGWUI002(t *testing.T)       // a child with hostnames and Accepted=False/UnsupportedValue
func TestStaleMatchingEntryIsKGWUI003NotKGWUI011(t *testing.T)
func TestWildcardOnlyParentStillGetsDiagnostic(t *testing.T)   // explicit declared parent always gets KGWUI-011 (§15 Q15)
```

- [ ] **Step 2: Run** `go test ./pkg/health -run 'KGWUI011|ReportedEdge|StaleMatching|WildcardOnly'` → FAIL.
- [ ] **Step 3: Implement.** KGWUI-011 is one finding per (route, parent HTTPRoute). Its grace clock is its own (design §6.2 "Two clocks"): 60 s from the later of the route's and the parent's current-generation first-seen time. KGWUI-012's `graceEnd` (rules §9.3) is a different clock; do not share a helper with it beyond `graceStart`. A declared candidate inside grace adds its route to `inGrace`, so `conditionsCheck` returns its existing pending result and roll-up is unchanged.
- [ ] **Step 4: Run** `go test ./pkg/health ./pkg/api` → PASS.
- [ ] **Step 5: Commit** `feat(health): reported-edge status and KGWUI-011 status trigger`.

### Task 17: Backend `Accepted` from the condition's own evidence (Q11)

**Files:**
- Create: `pkg/health/backend.go`, `pkg/health/backend_test.go`
- Modify: `pkg/health/checks.go:23-25` (register `backendCheck`, not required)

**Interfaces:**
- Consumes: `Owned.Backends` (Task 15), `objects.DecodeBackend` (Task 12).
- Produces: `func BackendState(b *unstructured.Unstructured) (health.State, *metav1.Condition)` — used by the explorer for `backend.state`.

- [ ] **Step 1: Write the failing table test** `TestBackendAcceptedRules` with the four rows of design §6.2: current `False` → KGWUI-002 error; current `True` → no finding, `healthy`; `observedGeneration` > 0 and lower → no finding, `unknown`; `observedGeneration` 0 → `False` gives KGWUI-002 warning with detail "The controller does not say which version of this Backend it checked" (draft), `True` gives `unknown`; absent → no finding, `unknown`. Every row: no failed required check and no "not yet processed" finding.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./pkg/health` → PASS.
- [ ] **Step 5: Commit** `feat(health): Backend Accepted from the condition's own evidence (Q11)`.

### Task 18: `explore.Build`: rows, flags, states and admission

**Files:**
- Create: `pkg/explore/explore.go`, `pkg/explore/rows.go`, `pkg/explore/admit.go`, `pkg/explore/explore_test.go`
- Modify: `pkg/engine/engine.go:57-63` (`InstallModel.Index *explore.Index`), `:169-180` (`explore.Build` after health, with the decode cache)

**Interfaces:**
- Consumes: Tasks 12–17.
- Produces:
  - `type Input struct { Install install.Resolved; Owned *install.Owned; Assignment install.Assignment; Snapshot *cache.Snapshot; Served []kinds.Served; Health health.Report; Views objects.Decoder }` (2.3 adds `Computed`)
  - `func Build(in Input) *Index` — pure; same input gives the same rows in the same order
  - `type RowID int32`
  - `type Flag uint16` with `FlagAccepted, FlagNotAccepted, FlagRefsUnresolved, FlagNotProgrammed, FlagPending, FlagDeclared, FlagNoParent` (2.3 adds `FlagDisagrees`, `FlagListenerUnknown`; 2.4 `FlagDropped`); `func ParseFlag(s string) (Flag, bool)`; `func (f Flag) Names() []string` (wire names of design §6.1)
  - `type Attachment string` (`"accepted"`, `"not-accepted"`, `"pending"`, `"unwatched"`, `"declared"`)
  - `type ParentLink struct { Target objects.Ref; Ref objects.ParentRef; Via *objects.Ref; Attachment Attachment; Cond *metav1.Condition; Pending string }` — the deciding condition per design §7.3 (first current `False` among `Accepted`, `ResolvedRefs`, `kgateway.dev/Programmed`, else the pending reason)
  - `type RouteRow struct { Ref objects.Ref; Kind kinds.Served; Obj *unstructured.Unstructured; Hostnames []string; Parents []ParentLink; ParentRoutes []objects.Ref; Children int; Flags Flag; State health.State; Owned bool; search string }` — rows are every route the install lists (design §3.5): owned routes, attached or not (including unattached ones with a missing parent); unwatched routes that target the install (`attachment=unwatched`); declared candidates (`attachment=declared`); and no-parent rows
  - `type BackendUse struct { Row RowID; Mirror bool }` — `Mirror` when the row names the backend only through `RequestMirror`
  - `type GatewayRow struct { Ref objects.Ref; Obj *unstructured.Unstructured; View *objects.GatewayView; State health.State; AttachedRoutes int32; Routes, NotAccepted []RowID; ListenerSets []int }` (`ListenerSets` indexes `Index.ListenerSets`)
  - `type ListenerSetRow struct { Ref objects.Ref; Obj *unstructured.Unstructured; View *objects.ListenerSetView; Parent objects.Ref; ParentFound bool; State health.State; AcceptedReason string; Routes []RowID }`
  - `type Index struct { Routes []RouteRow; Gateways []GatewayRow; ListenerSets []ListenerSetRow; ByParent, ByParentRoute, Declared map[objects.Ref][]RowID; ByBackend map[objects.Ref][]BackendUse; ByNamespace map[string][]RowID; ParentRoutes map[objects.Ref][]objects.Ref; NoParent []RowID; Findings map[objects.Ref]Worst; admitted map[objects.Ref]Admission }` (`ByParent` keys Gateways and ListenerSets; a Gateway key also covers routes through its ListenerSets; `ByBackend` keys backend identity after defaults, Services and Backends alike, from `objects.BackendTargets`)
  - `type Worst struct { Error, Warning *health.Finding }`
  - `func RowState(ref objects.Ref, rep health.Report) health.State` — any error finding → `error`; else any required check not OK → `unknown`; else any warning → `warning`; else `healthy`; info never sets a state (generalises `pkg/overview/overview.go:166-187`)
  - `type CandidateReason string` (`"finding"`, `"referenced"`; 2.4 adds `"selected"`); `type Admission struct { Owned bool; Reason CandidateReason; By *objects.Ref; NoParent bool }`
  - `func (ix *Index) Admission(ref objects.Ref) (Admission, bool)`; `func DetailKinds(served []kinds.Served) []objects.GroupKind` (Gateway, every `CategoryListenerSet` kind, HTTPRoute)

- [ ] **Step 1: Write the failing tests**

```go
func TestRowsCoverEveryRouteThatTargetsTheInstall(t *testing.T) // owned, declared, unattached, unwatched, no-parent; other controllers' routes absent
func TestAttachmentFromCurrentStatusOnly(t *testing.T) {
	// stale Accepted=True → pending, not accepted; stale Accepted=False → pending, not not-accepted;
	// each of NotAllowedByListeners, NoMatchingParent, NoMatchingListenerHostname keeps reason and message; via set for ListenerSet routes.
}
func TestFlags(t *testing.T) // one row per flag of §6.1 that 2.2 owns; RouteRuleReplaced under Accepted=False sets FlagNotProgrammed (2.3 shape)
func TestRowStateRules(t *testing.T) // warning + pending required check → unknown; info only with every required check OK → healthy; a Gateway with failing routes keeps its own state
func TestKGWUI011InGraceMakesEveryRowUnknown(t *testing.T) // install-wide rule (design §6.3): an unrelated Gateway row is unknown until grace ends
func TestByBackendIndex(t *testing.T) // Service, Backend, mirror target (Mirror true), a route naming one backend both ways (Mirror false); delegation refs never indexed
func TestAdmissionTable(t *testing.T) {
	// owned → Owned; finding resource → "finding"; parentRef/backendRef target of an owned object of a served kind → "referenced";
	// no-parent row → NoParent; D named only by candidate C → absent (admission does not follow chains).
}
func TestBuildIsDeterministic(t *testing.T) // two Builds over shuffled snapshot order → reflect.DeepEqual
func TestBuildWithoutPodsOrUIDs(t *testing.T) // design §9.7: no Pods/Deployments, no uid/rv → same rows as with them
```

- [ ] **Step 2: Run** `go test ./pkg/explore` → FAIL.
- [ ] **Step 3: Implement.** Pre-lowercase `ns/name` and hostnames into `search` once per row.
- [ ] **Step 4: Run** `go test ./pkg/explore ./pkg/engine ./internal/boundary` → PASS (boundary: `pkg/explore` imports no forbidden package).
- [ ] **Step 5: Commit** `feat(explore): per-install index of routes, Gateways and ListenerSets`.

### Task 19: Queries, sorting and keyset cursors

**Files:**
- Create: `pkg/explore/query.go`, `pkg/explore/cursor.go`, `pkg/explore/query_test.go`

**Interfaces:**
- Consumes: `Index` (Task 18).
- Produces:
  - `type Sort struct { Key string; Desc bool }` — keys `name` (default), `kind`, `health`, and for Gateways `routes`
  - `type Query struct { Namespaces []string; Kinds []objects.GroupKind; Parent *objects.Ref; Attachment Attachment; Backends []objects.Ref; States Flag; Health []health.State; Q string; Sort Sort; Limit int; Cursor string }` (2.3 adds `Listener string`) — `Backends` OR-ed through `ByBackend`; it reads specs only, never the backend object
  - `type Page struct { IDs []int; Total int; Next, Prev string }`
  - `func (ix *Index) QueryRoutes(q Query, visible func(objects.Ref) bool) (Page, error)`; `QueryGateways`, `QueryListenerSets` (same shape; `Parent` on ListenerSets is a Gateway)
  - `var ErrBadCursor, ErrBadQuery = errors.New(...)`
  - Cursor: base64url (no padding) of JSON `{"s":sortKey,"d":"asc"|"desc","b":before,"v":sortValue,"t":[ns,name,group,kind]}`; at most 1 KiB; a cursor whose `s`/`d` differ from the query is `ErrBadCursor`

- [ ] **Step 1: Write the failing tests**

```go
func TestEachFilterAloneAndCombined(t *testing.T) // includes backend: one value, two OR-ed, a mirror target, a delegation ref target (no match)
func TestSortsBothWaysWithTieBreak(t *testing.T) // every sort ends with (ns, name, group, kind)
func TestCursorsAcrossRecomputes(t *testing.T) { // Review Focus 4
	// page 1 (limit 2) on Model A; Model B inserts a row before the anchor → next starts strictly after the anchor;
	// Model C deletes the anchor → next starts where its tuple would sort; descending ties; prev from page 2 == page 1;
	// a row whose sort value changes may repeat across pages but never within one page.
}
func TestBadCursors(t *testing.T) // malformed, 1 KiB + 1, from another sort or direction → ErrBadCursor
func TestTotalCountsVisibleRowsOnly(t *testing.T)
func TestNarrowestIndexIsUsed(t *testing.T) // parent → ByParent; namespace → ByNamespace; asserts candidates scanned via a test counter
```

- [ ] **Step 2: Run** `go test ./pkg/explore -run 'Filter|Sorts|Cursor|Total|Narrowest'` → FAIL.
- [ ] **Step 3: Implement.** Candidate set from the narrowest index (parent, then backend, then namespace, else all rows), filtered, `visible` applied, sorted with `slices.SortStableFunc` unless the key is `name` (rows are already in that order), then the cursor position by `sort.Search` over the full sorted slice.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(explore): filters, sorts and keyset cursors`.

### Task 20: Bounded up-chain walk

**Files:**
- Create: `pkg/explore/ancestors.go`, `pkg/explore/ancestors_test.go`

**Interfaces:**
- Produces: `type AncestorStep struct { Ref objects.Ref; Restricted bool }`; `type AncestorPath struct { Steps []AncestorStep; Cycle bool }`; `func (ix *Index) Ancestors(route objects.Ref, visible func(objects.Ref) bool) (paths []AncestorPath, truncated bool)`; `const MaxPaths, MaxDepth, MaxNodes = 5, 32, 256`.

- [ ] **Step 1: Write the failing tests** `TestAncestorsChain` (child → parent → Gateway: one path, root first), `TestAncestorsDiamond` (two paths, parents sorted by namespace then name), `TestAncestorsCycleMarker` (a path meeting a route already on it ends with `Cycle: true`), `TestAncestorsLatticeTruncates` (8-layer repeated diamond: 5 paths, `truncated`, never a remaining count), `TestAncestorsNodeBound` (256 node visits stop the walk), `TestAncestorsRestrictedStep` (`visible` false → `Restricted: true`, zero `Ref`).
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** depth-first over `ParentRoutes` (reported edges only), then the route's Gateway or ListenerSet parents from `ParentLink.Target`; stop at whichever bound comes first.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(explore): bounded up-chain over reported edges`.

### Task 21: DTOs, links and the resource endpoint superset

**Files:**
- Create: `pkg/api/explore_types.go`, `pkg/api/link.go`, `pkg/api/link_test.go`
- Modify: `pkg/api/types.go:41-42` (`HealthResponse` becomes a struct), `:44-57` (`Views []objects.GroupKind \`json:"views,omitempty"\``), `pkg/api/handlers.go:110-128` (findings carry `resource.link`), `:132-159` (Backends kept in the visible Owned), `:254-289` (`lookup` serves every admitted object), `pkg/overview/overview.go:72-80` (no change; the API wraps rows), `pkg/api/api_test.go` (finding helpers read `LinkedRef`)

**Interfaces:**
- Consumes: `explore.Index.Admission`, `explore.DetailKinds`.
- Produces:
  - `type Link string` (`LinkDetail = "detail"`, `LinkYAML = "yaml"`, `LinkNone = "none"`)
  - `type LinkedRef struct { objects.Ref; Link Link \`json:"link"\` }`
  - `func (h *handler) link(r *http.Request, i *engine.InstallModel, ref objects.Ref) Link` — `none` when denied, absent, never served (Secrets, Services) or not admitted; `detail` when admitted and its kind is in `DetailKinds`; else `yaml`
  - `func (h *handler) linked(r *http.Request, i *engine.InstallModel, ref objects.Ref) LinkedRef`
  - `type FindingDTO struct { health.Finding; Resource *LinkedRef \`json:"resource,omitempty"\`; EvidenceRestricted bool \`json:"evidenceRestricted,omitempty"\` }`; `type HealthResponse struct { health.Report; Findings []FindingDTO \`json:"findings"\` }`
  - `type OverviewGatewayRow struct { overview.GatewayRow; Link Link \`json:"link,omitempty"\` }`; `OverviewResponse.Gateways []OverviewGatewayRow \`json:"gateways"\`` shadows the embedded field

- [ ] **Step 1: Write the failing tests** `TestEveryFindingResourceOpensItsView` (for each finding in the golden world, `link` ≠ `none` and the resource endpoint returns 200), `TestSpecNamedObjectsAreCandidates` (a backendRef'd Backend out of scope → YAML 200), `TestUnrelatedObjectIs404OnBothEndpoints`, `TestSecondStepIsNone` (D named only by candidate C: `none` and 404; once owned: `detail`).
- [ ] **Step 2: Run** `go test ./pkg/api -run 'EveryFinding|SpecNamed|Unrelated|SecondStep'` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./pkg/api` → PASS except `TestGoldenWireFormat` for `health`, `overview`, `bare-overview` (regenerated in Task 25; `TestGoldenChangesAreAdditive` must still pass after regeneration).
- [ ] **Step 5: Commit** `feat(api): link metadata from one admission function`.

### Task 22: List endpoints

**Files:**
- Create: `pkg/api/params.go`, `pkg/api/lists.go`, `pkg/api/lists_test.go`
- Modify: `pkg/api/handlers.go:39-48` (routes), `pkg/api/explore_types.go`

**Interfaces:**
- Consumes: `explore.Query*` (Task 19), `link` (Task 21).
- Produces:
  - `type Page[T any] struct { Install string \`json:"install"\`; Items []T \`json:"items"\`; Total int \`json:"total"\`; Next string \`json:"next,omitempty"\`; Prev string \`json:"prev,omitempty"\`; DeniedInInstall int \`json:"deniedInInstall,omitempty"\`; CollectedAt time.Time \`json:"collectedAt"\` }`
  - `type ParentSummary struct { Ref LinkedRef; SectionName string; Port int32; Via *LinkedRef; ViaRestricted bool; Attachment string; ConditionType, Reason, Message string }` (2.3 adds `Computed *Attachment`) — JSON names per design §7.3
  - `type RouteRow struct { Ref LinkedRef; Hostnames []string; Parents []ParentSummary; ParentRoutes []LinkedRef; Children int; Flags []string; Health health.State; Mirror bool \`json:"mirror,omitempty"\` }` — `mirror` only under a `backend` filter, when every matched use is a mirror (our call: the design's "Mirror" label needs a wire flag; additive)
  - `type GatewayListRow struct { Ref LinkedRef; Class string; Health health.State; Listeners int; AttachedRoutes int32; NotAccepted, ListenerSets int; Addresses []string }` (2.3 adds `Routes`, `Disagreements`) — the design's `GatewayRow2`
  - `type ListenerSetRow struct { Ref LinkedRef; Parent LinkedRef; Health health.State; Listeners int; AcceptedReason string }` (2.3 adds `Routes`, `Disagreements`)
  - `func parseQuery(r *http.Request, list listKind) (explore.Query, error)` — 400 bodies use `ErrorResponse`; `backend` is `group/Kind/namespace/name` with `core` for the empty group (`core/Service/shop/cart`), repeatable
  - Paths: `GET /api/v1/installs/{id}/routes`, `…/gateways`, `…/listenersets`

- [ ] **Step 1: Write the failing tests**

```go
func TestListParamsAndErrors(t *testing.T) {
	// 400: limit=0, limit=201, unknown state, unknown sort, attachment without parent, malformed cursor, cursor from another sort.
	// 404: unknown install. 503: not synced. Default limit 50.
}
func TestRoutesFilteredByParentIncludeListenerSetRoutes(t *testing.T)
func TestAttachmentSubFilters(t *testing.T)      // not-accepted, pending, unwatched, declared (HTTPRoute parent only)
func TestBackendFilter(t *testing.T)             // core/Service, a Backend, a mirror target (mirror:true), two values OR-ed; malformed value (3 segments, empty name) → 400
func TestCollectedAtIsConfirmedAt(t *testing.T)
func TestListenerSetsOfOneGatewayPaged(t *testing.T) // 200 ListenerSets: four pages of 50, total 200
```

- [ ] **Step 2: Run** `go test ./pkg/api -run 'ListParams|FilteredByParent|AttachmentSub|CollectedAtIs|ListenerSetsOfOne'` → FAIL.
- [ ] **Step 3: Implement**; under `AllowAll` no per-row Authorizer calls.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(api): routes, gateways and listenersets lists`.

### Task 23: Detail endpoints and `/meta` views

**Files:**
- Create: `pkg/api/details.go`, `pkg/api/details_test.go`
- Modify: `pkg/api/handlers.go:39-48`, `:96-108` (`views`), `pkg/api/explore_types.go`

**Interfaces:**
- Consumes: Tasks 18–22.
- Produces (JSON names per design §7.3; 2.3/2.4 add the optional fields marked there):
  - `type Condition struct { Type, Status, Reason, Message string; ObservedGeneration int64; LastTransitionTime time.Time; Stale bool }`
  - `type ParentStatus struct { ParentRef objects.ParentRef; Target LinkedRef; ControllerName string; Ours bool; Conditions []Condition }`
  - `type ListenerDTO struct { Name string; Port int32; Protocol, Hostname string; TLS *ListenerTLSDTO; AllowedRoutes AllowedRoutesDTO; Status *ListenerStatusDTO }` (2.3 adds `ComputedRoutes`, `Computed`, `IsolationNote`)
  - `type RouteCounts struct { Accepted, NotAccepted, Pending, Unwatched int }`
  - `type Candidate struct { Reason string; By *LinkedRef }`
  - `type GatewayDetail struct { Install string; Ref LinkedRef; Generation int64; Class string; Health health.State; Candidate *Candidate; RuleSet string; Conditions []Condition; Addresses []string; Listeners []ListenerDTO; AttachedListenerSets *int32; RouteCounts RouteCounts; CollectedAt time.Time }`
  - `type ListenerSetDetail struct { …; Parent LinkedRef; ParentFound bool; ExperimentalHint string; ParentAllowedListeners *AllowedListenersDTO \`json:"parentAllowedListeners,omitempty"\`; … }`; `type AllowedListenersDTO struct { Set bool \`json:"set"\`; Value map[string]any \`json:"value,omitempty"\`; CRDHint bool \`json:"crdHint"\` }` — sent only when the ListenerSet's current `Accepted=False` has reason `NotAllowed` and the parent exists; `crdHint` whenever the field is absent (the UI reads no CRDs, so the hint is a possibility, design §8.5)
  - `type RouteDetail struct { Install string; Ref LinkedRef; Generation int64; Health health.State; Candidate *Candidate; RuleSet string; Hostnames []string; Annotations struct{ InheritParentMatcher *bool }; ParentRefs []objects.ParentRef; Parents []ParentStatus; Rules []RuleDTO; Ancestors []AncestorPathDTO; AncestorsTruncated bool; ChildCount, DeclaredCount int; CollectedAt time.Time }` (2.3 adds `Attachments`; 2.4 `Effective`, `EffectiveTruncated`)
  - `type RuleDTO`, `FilterDTO`, `BackendRefDTO` (with `Delegation *DelegationRefDTO{Mode, Namespace, Name, Label, State}` — `State` `reported`/`not-reported`/`not-found`/`restricted`, set only for `name` refs; `Backend *BackendStateDTO`; `Weight` plus `WeightDefaulted bool \`json:"weightDefaulted,omitempty"\``; `Port` omitted when absent; `FilterDTO.Mirror *{backendRef LinkedRef, port?, percent?, fraction?}`; filters in written order), `AncestorPathDTO`, `AncestorStepDTO` per design §7.3
  - Paths: `GET …/gateways/{ns}/{name}`, `…/listenersets/{group}/{kind}/{ns}/{name}`, `…/routes/{group}/{kind}/{ns}/{name}` (non-HTTPRoute kinds: 404 "no detail view for this kind")

- [ ] **Step 1: Write the failing tests**

```go
func TestRouteDetailDelegation(t *testing.T) {
	// parent with name, wildcard and label refs as written; a reported child counted in childCount;
	// a declared child in declaredCount and its name ref state "not-reported"; up-chain root first; a cycle path; a truncated lattice.
}
func TestRouteDetailFreshness(t *testing.T) // parents[].conditions[].stale on a stale entry; other controllers' entries have ours=false
func TestGatewayDetailListeners(t *testing.T) // hostname, TLS mode, certificateRefs link "none", allowedRoutes, status attachedRoutes and supportedKinds
func TestListenerSetDetail(t *testing.T) // Accepted=True/ListenersNotValid; Accepted=False without listener entries; XListenerSet hint; missing parent: parentFound false
func TestParentAllowedListeners(t *testing.T) // NotAllowed + parent sets allowedListeners → value, no hint; NotAllowed + field absent → set false, crdHint; accepted → absent
func TestRouteDetailValuesEasyToGetWrong(t *testing.T) // mirror filter with percent/fraction; absent weight 1 weightDefaulted vs explicit 1 and 0; absent port omitted; filter order preserved
func TestCandidateDetail(t *testing.T)   // candidate.reason "referenced" with by; this controller's entries listed first
func TestNonHTTPRouteDetailIs404(t *testing.T)
func TestMetaViews(t *testing.T)         // Gateway, ListenerSet, XListenerSet, HTTPRoute
```

- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./pkg/api` → PASS (goldens aside).
- [ ] **Step 5: Commit** `feat(api): Gateway, ListenerSet and HTTPRoute detail endpoints`.

### Task 24: Authorizer rules for 2.2's facts

**Files:**
- Create: `pkg/api/visible.go`, `pkg/api/visible_test.go`
- Modify: `pkg/api/lists.go`, `pkg/api/details.go`, `pkg/api/handlers.go:110-128` (evidence restriction)

**Interfaces:**
- Produces: `type view struct{...}` per request with `can(ref objects.Ref) bool` (memoised), `deniedInInstall(kinds []objects.GroupKind) int`, `recount(ids []explore.RowID) int`; rules 1, 2, 3, 5, 7, 8, 9, 10 of design §7.4 for 2.2's data. 2.3/2.4 add rules 4 and 6 through `func (v *view) allowAll(deps []objects.Ref) bool`.

- [ ] **Step 1: Write the failing tests** (deny-some `AuthorizerFunc`, the pattern of `TestAuthorizerRestrictsDeniedObjects`; rules 1, 2, 2a, 3, 5, 7–10):

```go
func TestDeniedInInstallIgnoresFilters(t *testing.T) { // Review Focus 5
	// deniedInInstall equal for: no filter, q=<hidden name>, namespace=<hidden ns>, state=declared, parent=<gateway>.
}
func TestDeniedObjectsLeaveRowsAndCounts(t *testing.T)   // total, children, childCount, declaredCount, routeCounts, notAccepted, listenerSets count visible only
func TestBackendFilterCannotProbe(t *testing.T)     // rule 2a: backend= for a denied backend and for an absent one give byte-identical bodies; hidden routes not in total; deniedInInstall unchanged
func TestDereferencedDataIsRestricted(t *testing.T)     // Backend → {restricted:true}, no message; name ref state "restricted"; via → viaRestricted; ancestor step without identity
func TestDeniedDetailIs404(t *testing.T)
func TestEvidenceRestrictedFinding(t *testing.T)       // KGWUI-011 on a visible child of a hidden parent: detail replaced, evidenceRestricted true
func TestHealthUnchangedUnderRestriction(t *testing.T) // install state, row states unchanged
func TestAllowAllMakesNoAuthorizerCalls(t *testing.T)  // a counting AllowAll wrapper is recognised by allowsAll → 0 calls
```

- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement.** Replacement detail text comes from `copy` constants finalised in Task 31.
- [ ] **Step 4: Run** `go test ./pkg/api -race` → PASS.
- [ ] **Step 5: Commit** `feat(api): Authorizer rules for explorer lists and details`.

### Task 25: Goldens for 2.2, Go and TypeScript

**Files:**
- Modify: `pkg/api/golden_test.go:31-78` (cases; a delegation world: three-level chain, diamond, cycle, lattice, Backend ref, ListenerSet with an invalid listener, declared child), `pkg/api/additive_test.go` (new files are not in `golden-v1`)
- Create: `pkg/api/testdata/golden/{routes,routes-filtered,gateways,listenersets,gateway-detail,listenerset-detail,route-detail,route-detail-candidate,bad-request,route-detail-restricted,routes-restricted,routes-by-backend,routes-by-backend-restricted}.json` — `listenerset-detail.json` holds a `NotAllowed` ListenerSet whose parent lacks `allowedListeners` and one whose parent sets it; `route-detail.json` holds a mirror filter (percent and fraction), absent vs explicit 1 and 0 weights, absent vs explicit port, and several filters of different types in one rule and one backendRef; `routes-by-backend.json` queries a Service, a Backend, a mirror target and two backends OR-ed; the restricted variant holds the same query for a denied and an absent backend, with identical bodies
- Regenerate: `meta.json` (`views`, Backend kind), `health.json` (`link`), `overview.json`, `bare-overview.json` (`link`)
- Modify: `web/packages/shell/src/api/types.golden.test.ts` (import and shape-check each new fixture)

- [ ] **Step 1: Add the cases and run** `go test ./pkg/api -run Golden` → FAIL (files missing).
- [ ] **Step 2: Regenerate** with `-update`; read every new file; check `TestGoldenChangesAreAdditive` passes over the regenerated Phase 1 files.
- [ ] **Step 3: Add `TestGoldenLinksAgreeWithEndpoints`:** walk every `link` in every golden body; `detail` → the detail endpoint returns 200; `yaml` → the resource endpoint returns 200 and the detail endpoint 404 or no detail kind; `none` → both 404 (or never served).
- [ ] **Step 4: Run** `go test ./pkg/api` → PASS. Commit `test(api): golden wire format for the explorer`. The TypeScript shape checks of these fixtures are in Task 26.

### Task 26: Frontend contract: types, hooks, links, slots and navigation

**Files:**
- Create: `web/packages/shell/src/api/exploreTypes.ts`, `src/api/exploreHooks.ts`, `src/api/exploreHooks.test.tsx`, `src/explore/objectPath.ts`, `src/explore/objectPath.test.ts`, `src/explore/LinkedName.tsx`, `src/explore/LinkedName.test.tsx`, `src/explore/CopyCommandButton.tsx`, `src/shell/InstallNav.tsx`
- Modify: `src/api/types.ts` (re-export), `src/api/types.golden.test.ts` (import and shape-check every Task 25 fixture), `src/shell/extension.ts:14-22` (reserved paths doc), `:48-54` (optional slots), `src/shell/Shell.tsx:50-71` (routes, slot fallback), `src/shell/vocabulary.tsx:5-43` (`getCommand`), `src/shell/Header.tsx` (InstallNav), `src/pages/defaultPages.ts`, `src/index.ts` (exports; `SHELL_API_VERSION` stays 1)
- Test: `src/shell/extension.test.tsx`, `src/index.test.ts`

**Interfaces:**
- Consumes: the DTOs of Tasks 21–23.
- Produces:
  - TS types mirroring each Go DTO (`Link`, `LinkedRef`, `Page<T>`, `RouteRow`, `GatewayListRow`, `ListenerSetRow`, `ParentSummary`, `GatewayDetail`, `ListenerSetDetail`, `RouteDetail`, …)
  - `export function objectPath(installId: string, r: LinkedRef): string | undefined` — `detail` → `/i/:id/gateways/:ns/:name`, `/i/:id/listenersets/:group/:kind/:ns/:name` or `/i/:id/routes/:group/:kind/:ns/:name` by the ref's kind; `yaml` → `resourcePath`; `none` → `undefined`
  - `useRoutes(id, query: URLSearchParams)`, `useGateways`, `useListenerSets`, `useGatewayDetail(id, ns, name)`, `useListenerSetDetail(id, ref)`, `useRouteDetail(id, ref)` — SWR, 5 s poll, no `keepPreviousData`; an `ApiError` 400 on a cursor reports `cursorRejected: true`
  - `export function LinkedName({ refValue, installId }: { refValue: LinkedRef; installId: string })`
  - `Vocabulary.getCommand: (ref: Ref, resource: string) => string | undefined` — default `kubectl get <resource>.<group> -n <ns> <name> -o yaml` (no `.<group>` for the core group, no `-n` when cluster-scoped)
  - `CorePages` gains optional `GatewaysPage`, `GatewayPage`, `ListenerSetsPage`, `ListenerSetPage`, `RoutesPage`, `RoutePage`

- [ ] **Step 1: Write the failing tests**

```ts
test("objectPath follows link only", () => {
  expect(objectPath("a.b", { group: gw, kind: "HTTPRoute", namespace: "shop", name: "x", link: "detail" })).toBe("/i/a.b/routes/gateway.networking.k8s.io/HTTPRoute/shop/x");
  expect(objectPath("a.b", { ...ref, kind: "GRPCRoute", link: "yaml" })).toBe("/i/a.b/r/gateway.networking.k8s.io/GRPCRoute/shop/x");
  expect(objectPath("a.b", { ...ref, link: "none" })).toBeUndefined();
});
it("renders new pages with only the four Phase 1 slots", ...);       // /i/x/routes renders the upstream RoutesPage
it("a contribution at routes cannot replace the core page", ...);
it("getCommand returning undefined hides the copy button", ...);
it("never shows the previous install's rows on a switch", ...);       // response.install must match the URL
it("a 400 on a cursor drops it and loads page one", ...);
// types.golden.test.ts: every Task 25 fixture parses against its Spec<T>; tsc fails when a DTO drifts.
```

- [ ] **Step 2: Run** `cd web && npx vitest run packages/shell/src/explore packages/shell/src/shell packages/shell/src/api` → FAIL.
- [ ] **Step 3: Implement.** Install nav: Overview, Gateways, Routes, ListenerSets, Findings; `aria-current="page"` on the active entry; hover, active, focus-visible styles.
- [ ] **Step 4: Run** `cd web && npm test && npm run typecheck && npm run lint` → PASS.
- [ ] **Step 5: Commit** `feat(shell): explorer types, hooks, links, page slots and install navigation`.

### Task 27: List pages

**Files:**
- Create: `web/packages/shell/src/explore/ObjectTable.tsx`, `src/explore/Pager.tsx`, `src/explore/StateFilter.tsx`, `src/pages/explore/{GatewaysPage,ListenerSetsPage,RoutesPage}.tsx`, `src/pages/explore/lists.test.tsx`

**Interfaces:**
- Consumes: Task 26 hooks, `useUrlParams` (`src/shell/urlParams.ts`).
- Produces: `ObjectTable<T>({ columns, page, loading, emptyTitle, onClearFilters })` with fixed column widths, `aria-sort`, keyboard row navigation, skeleton rows at the final height, `deniedInInstall` line once per page, and secondary columns collapsing into an expandable row below 900 px.

- [ ] **Step 1: Write the failing tests**

```ts
it("filters, sort and cursor round-trip through the URL", ...);  // ?namespace=shop&state=declared&backend=core/Service/shop/cart&sort=health.desc&cursor=… survives reload
it("backend filter shows a Mirror label on mirror rows", ...);
it("GRPCRoute rows open the YAML view", ...);
it("shows each list state", ...);   // loading skeleton height == row height; empty with Clear filters; error after data keeps rows + PageNotice; 404; stale badges unknown
it("Routes filter 'Rules replaced or dropped' maps to state=not-programmed", ...);
it("rows link by their link field", ...); // a GRPCRoute row opens the YAML view; a no-parent row opens YAML
```

- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** with TanStack Table (manual sorting and pagination: the server sorts).
- [ ] **Step 4: Run** `cd web && npm test && npm run typecheck && npm run lint` → PASS.
- [ ] **Step 5: Commit** `feat(shell): Gateways, ListenerSets and Routes lists`.

### Task 28: Gateway and ListenerSet detail pages

**Files:**
- Create: `web/packages/shell/src/pages/explore/{GatewayPage,ListenerSetPage}.tsx`, `src/pages/explore/gateway/ListenerTable.tsx`, `src/explore/{ConditionList,CandidateBanner,DetailTabs}.tsx`, `src/pages/explore/details.test.tsx`

**Interfaces:**
- Consumes: `useGatewayDetail`, `useListenerSetDetail`, `useRoutes` (`parent=`, `attachment=`), `useListenerSets` (`parent=`), `LinkedName`, `CopyCommandButton`, `CopyNameButton`.
- Produces: `DetailTabs` (WAI-ARIA tabs, `?tab=yaml` reuses the YAML view body from `ResourcePage.tsx`), `ConditionList` (icon + text, reason, message, freshness "Describes generation N; the object is at M"), `CandidateBanner` ("Not part of this install. Shown because …").

- [ ] **Step 1: Write the failing tests** `"Gateway page shows status-only listener counts"` (attachedRoutes from status; no computed column in 2.2), `"failed-route table has Not accepted, Waiting for status and Unwatched namespace sub-filters"`, `"ListenerSets are paged through the list endpoint"`, `"ListenerSet page says when the parent Gateway is missing"`, `"NotAllowed shows the parent's allowedListeners as written, or Not set with the CRD hint"`, `"an accepted ListenerSet shows no allowed-listeners block"`, `"candidate banner above the header"`, `"polling keeps scroll, focus and expanded rows"`.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the web gates → PASS.
- [ ] **Step 5: Commit** `feat(shell): Gateway and ListenerSet detail pages`.

### Task 29: HTTPRoute detail page

**Files:**
- Create: `web/packages/shell/src/pages/explore/RoutePage.tsx`, `src/pages/explore/route/{UpChain,ParentCard,RuleList,ChildRoutes}.tsx`, `src/pages/explore/route.test.tsx`

**Interfaces:**
- Consumes: `useRouteDetail`, `useRoutes(parent=<route>)` for child routes.
- Produces: `UpChain` as `nav aria-label="Route parents"`; `ParentCard` per parentRef (target, section or port, every condition, other controllers' entries collapsed); `RuleList` (matches, filters with config as written, backendRefs with weight, delegation refs with mode and status state, Backend state or "Restricted").

- [ ] **Step 1: Write the failing tests** `"up-chain shows a restricted step and 'More paths not shown'"`, `"hostnames or inherited text for a child"`, `"permission line: Allows any parent / Allows: shop/api"`, `"child routes are paged and fetched when expanded"`, `"other controllers' entries are collapsed"`, `"each backendRef links to Other routes using this backend (routes?backend=…)"`, `"absent weight shows 1 marked as defaulted; absent port shows nothing"`, `"mirror filters show their target and percentage; filters keep written order"`.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the web gates → PASS.
- [ ] **Step 5: Commit** `feat(shell): HTTPRoute detail page`.

### Task 30: Finding links retarget and the Backend reverse lookup

**Files:**
- Modify: `web/packages/shell/src/pages/install/FindingRow.tsx:76-79` (keep `resourcePath`), `:121` (`objectPath(installId, f.resource)`), `src/pages/install/TopFindings.tsx`, `src/pages/install/GatewayTable.tsx:192,265` (row `link`), `src/pages/ResourcePage.tsx:135-192` ("Routes that send traffic here" on an admitted Backend's YAML view)
- Test: `src/pages/install/InstallPage.test.tsx`, `src/pages/pages.test.tsx`

- [ ] **Step 1: Write the failing tests** `"finding on an HTTPRoute opens its detail page"`, `"restricted finding never links"`, `"Gateway table row opens the Gateway page"`, `"a finding with link yaml opens the YAML view"`, `"a Backend YAML view links to Routes filtered by backend=gateway.kgateway.dev/Backend/ns/name"`, `"a Service has no YAML view entry point"`.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the web gates → PASS.
- [ ] **Step 5: Commit** `feat(shell): finding and Gateway links open detail pages`.

### Task 31: Wording pass for 2.2

**Files:**
- Create: `web/packages/shell/src/copy/explorer.ts`
- Modify: `pkg/health/messages.go` (KGWUI-011 message and detail, Backend no-evidence detail, restricted evidence text), the components of Tasks 26–30 (import strings from `copy/explorer.ts`)

- [ ] **Step 1: List every 2.2 string** (titles, nav labels, empty states, filter and flag labels, attachment labels, permission lines, candidate banner reasons, restricted placeholders, denied-objects line, copy buttons, KGWUI-011 message/detail, Backend detail including current `Accepted=True` as "Configuration accepted" (never "working"; user, 2026-10-23), the allowed-listeners "Not set" line and CRD hint (worded as a possibility), "Routes that send traffic here", "Other routes using this backend", the Mirror label, the defaulted-weight mark, evidence-restricted detail) with its design draft.
- [ ] **Step 2: For each string,** write five accurate options and have a separate sub-agent pick the least machine-sounding accurate one; record the choices in the 2.2 record.
- [ ] **Step 3: Apply** the chosen strings; update test expectations that quote them.
- [ ] **Step 4: Run** `go test ./pkg/health ./pkg/api && cd web && npm test` → PASS (regenerate goldens if a message changed; additive test still passes).
- [ ] **Step 5: Commit** `copy: wording pass for the explorer`.

### Task 32: 2.2 budgets at Large

**Files:**
- Create: `pkg/api/budget_test.go` (`//go:build bench`)
- Modify: `pkg/engine/budget_test.go`, `pkg/engine/bench_test.go`, `internal/testutil/scale.go` (status for 5 % failed parentRefs and the deepest delegated child)

- [ ] **Step 1: Write the budget tests**

```go
func TestBudgetWarmRecompute(t *testing.T) // Large: warm Compute ≤ 100 ms (median of 20), allocation ≤ 32 MiB; cold ≤ 1 s
func TestBudgetHeap(t *testing.T)          // runtime.ReadMemStats after GC: index + decode cache ≤ 32 MiB above the 2.1 baseline, per kind reported
func TestBudgetSustainedStream(t *testing.T) // real cache; one update per 100 ms for 10 min (-short: 60 s); 20 goroutines polling /routes every 5 s; ≤ 1 recompute/s, engine CPU ≤ 15 % of one core (rusage delta), GC share logged
func TestBudgetListRequests(t *testing.T) {
	// 10,000 httptest requests per mix: none, parent, namespace+state, q, sort=health; p95 < 50 ms, p99 < 100 ms;
	// 20 goroutines: p95 < 100 ms. Route detail on the deepest child: p95 < 50 ms. b.ReportMetric-style logs.
}
```

- [ ] **Step 2: Run** `make bench | tee docs/phases/phase-2/bench/2.2.txt` → PASS, or STOP and report the miss (a failing Large budget blocks the sub-phase).
- [ ] **Step 3: Commit** `test: Large-tier budgets for the explorer`.

### Task 33: kind e2e, Playwright and ui-loop evidence for 2.2

**Files:**
- Create: `test/e2e/fixtures/explorer.yaml`, `web/e2e/explorer.spec.ts`
- Modify: `test/e2e/setup.sh` (apply the fixture), `test/e2e/api_test.go` (subtests)

- [ ] **Step 1: Write the fixture** (design §9.5, 2.2): Gateway `infra/edge` with HTTP, HTTPS `*.example.com` and TCP listeners and a `Selector` allowedRoutes; routes rejected with each of the three reasons; a ListenerSet with one invalid listener and one denied; a delegation tree (name child, label child, wildcard parent, forbidden child, header-mismatch child); a child with hostnames; an inherit-parent-matcher child; a cycle; a static and an invalid `Backend`; a replaced rule; a `RequestMirror` filter to a Service; a ListenerSet rejected `NotAllowed` by a parent without `allowedListeners`.
- [ ] **Step 2: Write `TestExplorerShowsControllerStatus`** in `test/e2e/api_test.go`: for each object, the list and detail bodies carry exactly the controller's conditions, reasons and `attachedRoutes`; reported children appear under Child routes; the forbidden and header-mismatch children have no reported edge; `routes?backend=` for the mirror target lists the mirroring route with `mirror: true`; the `NotAllowed` ListenerSet detail carries `parentAllowedListeners`.
- [ ] **Step 3: Write `explorer.spec.ts`:** open Gateways → `infra/edge` → a failed route → its parent → back via the up-chain; filter Routes by "Rules replaced or dropped"; check both themes for hover/active/focus styles via `page.hover` and `page.focus` screenshots.
- [ ] **Step 4: Run** `make e2e KGW_VERSION=v2.4.7 GWAPI_VERSION=v1.6.1` and `make e2e KGW_VERSION=v2.3.11 GWAPI_VERSION=v1.5.1` → PASS (or the CI matrix).
- [ ] **Step 5: Run the `/ui-loop` skill** on the three detail pages and three lists against the kind cluster: light and dark, 1280 px and 720 px, loading and stale states. Store the recordings outside the repo and list their paths in the 2.2 record.
- [ ] **Step 6: Commit** `test(e2e): explorer fixtures and assertions on kgateway 2.3 and 2.4`.

### Task 34: Close sub-phase 2.2

**Files:**
- Create: `docs/phases/phase-2/phase-2.2-complete.md`, `docs/phases/phase-2/reviews/2.2-codex.md`
- Modify: `README.md` ("Upgrade notes": counts and colours change on clusters that use delegation, design §4.2 and §6.3), `docs/phase-status.md`, `docs/phases/phase-2/design.md` (only where the code deviates)

- [ ] **Step 1: Write the release note** in README "Upgrade notes": delegated child routes with a controller status entry now count in the HTTPRoute tile and in install health; a delegated child's failures can turn the install yellow or red; routes declaring an HTTPRoute parent without status get KGWUI-011.
- [ ] **Step 2: Run the gates** of Task 10 Step 1 plus `make bench`; paste outputs into the record.
- [ ] **Step 3: Request the cross-model review**

```bash
codex exec --sandbox read-only -o docs/phases/phase-2/reviews/2.2-codex.md "Review sub-phase 2.2 of the kgateway UI (diff <base>..HEAD) against docs/phases/phase-2/design.md §4.2, §4.3, §6, §7, §8 and D-016/D-019. Focus: Q12 ownership and counts, KGWUI-011 pending in grace, Backend freshness, admission and links, Authorizer rules 1-3, 5, 7-10, cursors, additive goldens, SPA states and accessibility. Test and bench output: <paste>. Rank findings P1–P4 with file:line." < /dev/null
```

- [ ] **Step 4: Triage** every finding; fix accepted ones with tests; rerun the gates.
- [ ] **Step 5: Write `phase-2.2-complete.md`** (Summary, Reviews, wording-pass choices, Waivers, Deliverables, benchmark table vs 2.1, ui-loop recording paths, release note) and update `docs/phase-status.md` (next: 2.3).
- [ ] **Step 6: Commit** `docs: close sub-phase 2.2`.

---

Part 2 (sub-phases 2.3 and 2.4) follows.
