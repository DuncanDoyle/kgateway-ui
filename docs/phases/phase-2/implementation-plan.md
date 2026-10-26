# Phase 2 — Gateway & Route Explorer: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Event-driven recompute with honest freshness, then Gateway, ListenerSet and HTTPRoute explorer pages and lists, first on observed status (2.1, 2.2) and then with the labelled, cross-checked computed views A, B and C (2.3, 2.4).

**Architecture:**
- **Engine.** Informer events, access transitions, metrics results, served-kind changes, deadlines and a safety timer trigger a debounced, single-flight recompute. Each run publishes one immutable `Published{Model, Source, ConfirmedAt}` through an atomic pointer; a 1 s confirm tick renews `ConfirmedAt` only while the live source fingerprint equals the published one (D-019).
- **Explorer.** `pkg/explore` builds one immutable `Index` per install inside the recompute: route, Gateway and ListenerSet rows, parent and delegation edges from status, state flags, findings per object and the admission table. Handlers filter, authorize, sort and page it with keyset cursors.
- **Computed views (2.3, 2.4).** `pkg/computed` holds pinned rule sets per kgateway minor behind the `Rules` seam; their results feed the index, the DTOs and KGWUI-011/012, never ownership or health (D-020).

**Tech Stack:** Go 1.26, client-go v0.36, `k8s.io/utils/clock` (already in the module graph, promoted to a direct dependency for the fake clock), React 19, TypeScript, emotion, react-router, SWR, TanStack Table, Vitest, Testing Library, Playwright, kind.

**Spec:** `docs/phases/phase-2/design.md`, `docs/phases/phase-2/computed-rules.md`, `docs/decisions.md` D-017 … D-020 (approved 2026-10-23, with the addendum of commit `172a5a4`). Section numbers below (§3.1, §7.4, rules §9.3) refer to these. Phase 1's plan (`docs/phases/phase-1/implementation-plan.md`) holds the project conventions this plan does not repeat. Design Q7's research refresh ran on 2026-10-23; its results are the addendum `172a5a4`. The spec updates from the plan review are commit `8b381c5`; the review and its binding triage are `reviews/plan-codex.md`.

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
- **Rule sets (rules §1):** one per kgateway minor; `kgw24` starts as a copy of `kgw23`; a shipped set is never edited for a new minor. A fixture's expectation is written from its provenance, never generated.
- **Supported versions:** kgateway v2.3.11 (`c2af2887`, Gateway API v1.5.1) and v2.4.7 (`b4a2bf74`, Gateway API v1.6.1).
- **Scale tiers (design §9.6):** Typical 30 Gateways / 3 listeners / 500 HTTPRoutes / 10 ListenerSets / 20 namespaces / 50 Backends; Large 100 / 4 / 2,000 / 200 / 100 / 200; Extreme 500 / 4 / 5,000 / 2,000 / 500 / 1,000; delegated children 10 % (depth ≤ 2, 3, 4; fan-out ≤ 10, 50, 100; Extreme adds an 8-layer diamond lattice); parentRefs mean 1.2, 1.3, 1.5; parent matches per delegating rule 1, 2, 4; 5 % each GRPC, TCP and TLS routes; labelled namespaces behind some `Selector` listeners; 5 % failed parentRefs.
- **Large-tier budgets (block the sub-phase when missed):** confirm tick ≤ 5 ms; burst of 1,000 events within 1 s ≤ 2 recomputes with event-to-publication p95 ≤ 1.2 s; warm recompute ≤ 100 ms (2.2), ≤ 120 ms (2.3), ≤ 150 ms (2.4); cold recompute ≤ 1 s; allocation per warm recompute reported, not gated (D-022); heap of index, computed views and decode cache ≤ 32 MiB above baseline; sustained stream (one event per 100 ms, 10 min, 20 polling clients) ≤ 1 recompute per s and engine CPU ≤ 15 % of one core; list request p95 < 50 ms, p99 < 100 ms, p95 < 100 ms with 20 concurrent clients; route detail on the deepest child p95 < 50 ms.
- **Change-size rule (AGENTS.md §6):** approval of this plan covers each task's file list. A task that needs more files stops and asks.

## Review Focus

1. **A change whose notification has not arrived yet.** The informer store moved but its handler is blocked: the confirm tick must refuse to renew `ConfirmedAt`, schedule a run, and the run must publish the change with no handler having run. → Task 6, `TestConfirmTickCatchesChangeBeforeHandler`.
2. **An idle cluster for minutes.** No recompute for 30 s while `confirmedAt` advances each second: cluster cards, switcher and header must stay fresh, and fall back to `computedAt` only when `confirmedAt` is absent. → Task 8, `confirmedAt keeps an idle install fresh`.
3. **A delegated child just after an edit.** Its matching entry describes the previous generation: it stays a child, owned and counted, with KGWUI-003 info (check pending) inside grace, never KGWUI-011, and the tiles do not flicker. → Task 14, `TestStaleEligibleEntryKeepsEdge`.
4. **Paging while the model changes.** A row inserted before the anchor, the anchor deleted, ties under a descending sort, and backward paging: no crash, no duplicate inside one direction, `prev` returns the same set as forward. → Task 19, `TestCursorsAcrossRecomputes`.
5. **Probing hidden objects through filters.** `deniedInInstall` must not change under any `q`, `namespace`, `state` or `parent` filter, and a denied Backend, `via` or ancestor step reveals no identity or message. → Task 24, `TestDeniedInInstallIgnoresFilters`.

## Sub-phases

Each sub-phase starts with a brief (`docs/phases/phase-2/phase-2.N-brief.md`, per the `agentic-engineering-sdlc` skill, written in its first task), ends green and committed, gets its own cross-model review, its own Large-tier benchmarks, and ships alone (design §13).

| Sub-phase | Goal | Tasks | Exit criteria |
|---|---|---|---|
| **2.1 Engine and timestamps** | Phase 1 pages, event-driven, with honest freshness (D-019) | 1–10 | D-021 observations recorded; Phase 1 baseline measured; §9.3 engine tests pass with a fake clock; `confirmedAt` served and read by the SPA; existing goldens differ only by `confirmedAt`; burst, idle and tick budgets met at Large; Codex review triaged |
| **2.2 Explorer on observed status** | Every explorer page and list, from status only | 11–34 | Q12 ownership with before/after tests; KGWUI-011 status trigger; Backend kind; list and detail APIs with Authorizer and goldens (Go and TS); Gateways, ListenerSets, Routes lists and the three detail pages; wording pass; 2.2 budgets met at Large; kind e2e green on v2.3.11 and v2.4.7; ui-loop recordings; release note; Codex review triaged |
| **2.3 View A** | Routes per listener with effective hostnames, labelled and cross-checked | 35–51 | D-021 view A items applied; `pkg/computed` seam and both rule sets with per-version fixtures; experimental setting read; `listener-count` and `route-accepted` checks; KGWUI-012 persistence with scheduled maturation; listener and attachment DTOs, `listener` filter, Authorizer rules 4–7 for view A; goldens (Go and TS); `ComputedBadge` and pages; rules §11.1, §11.4 (view A rows), §11.5 rows 1–4, 7–10; wording pass; 2.3 budgets at Large; kind e2e: computed equals `attachedRoutes` on both tags and a forced disagreement; Codex review triaged |
| **2.4 Views B and C** | Delegation selection, outcomes and effective matchers | 52–70 | D-021 delegation items applied; wildcard setting read; traversal with bounds, completeness, retention and projection; report keys per version; `child-entry` and `parent-resolvedrefs` checks; KGWUI-011 computed and orphan triggers; delegation endpoint and tab, effective matchers; Authorizer for contexts and evidence; goldens; rules §11.2–§11.5; wording pass; 2.4 budgets and the dense-selection benchmark; kind e2e for the controller-backed cases; Codex review triaged |

**Lifecycle.** A sub-phase's first task records it in `docs/phase-status.md` under `## Sub-phase` (`2.N <name>`, `Status: active`, `Base: <git rev-parse HEAD>` taken before that task's first commit). Its review command diffs from that base: `BASE=$(awk '/^## Sub-phase/{f=1} f && /^Base:/{print $2; exit}' docs/phase-status.md)`. Its close task writes the README upgrade notes, updates `docs/roadmap.md` and resets `## Sub-phase`.

**Step convention.** A code task runs: the failing tests; the run command, which must fail for the stated reason; the implementation; the same run plus the task's gates, which must pass; the commit with the task's message and the trailer line. Steps list only what decides something. Web tasks gate on `cd web && npm test && npm run typecheck && npm run lint`.

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
pkg/engine/views.go                 computed-view inputs from the snapshot: view A (2.3); roots and delegation (2.4)
pkg/engine/persist.go               KGWUI-012 and computed KGWUI-011 persistence map and maturation (2.3, 2.4)
pkg/engine/bench_test.go            Compute and fingerprint benchmarks per tier (2.1)
pkg/engine/budget_test.go           //go:build bench — burst, idle, stream budgets (2.1, 2.2)
pkg/objects/objects.go              ListenerStatus.SupportedKinds (2.2)
pkg/objects/gateway.go              Listener, GatewayView, ListenerSetView decoders (2.2)
pkg/objects/httproute.go            HTTPRouteView, Rule, Match, Filter, BackendRef, DelegationRef, RouteHostnames (2.2)
pkg/objects/backend.go              BackendView (2.2)
pkg/objects/decoder.go              Decoder interface, Direct (2.2)
pkg/install/assign.go               HTTPRoute parent kinds, Backends, unwatched owned-route parents (2.2)
pkg/install/delegation.go           reported edges, ownership BFS, declared candidates, no-parent rows (2.2)
pkg/install/settings.go             controller settings read from the Pod spec; namespace label map (2.3, 2.4; boundary allowlist entry)
pkg/health/finding.go               KGWUI-011 (2.2), KGWUI-012 (2.3) codes; Finding.Evidence (2.2)
pkg/health/computed.go              KGWUI-012 check (2.3)
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
pkg/computed/rules.go               Rules seam, Selection, shared result types, Views (2.3)
pkg/computed/attach.go              view A input and result types (2.3)
pkg/computed/check.go, fingerprint.go  cross-check, evidence classes, fingerprints (2.3, 2.4)
pkg/computed/delegation.go          view B and C input and result types (stub 2.3, filled 2.4)
pkg/computed/complete.go            completeness by reach closure (2.4)
pkg/computed/rulesets/rulesets.go   upstream RulesFor: version selection (2.3)
pkg/computed/kgw23/, kgw24/         rule sets kgateway-2.3, kgateway-2.4; kgw24 starts as a copy (2.3, 2.4)
pkg/computed/golden_test.go, testdata/kgateway-2.3/, testdata/kgateway-2.4/   per-version fixtures with provenance (2.3, 2.4)
pkg/api/types.go                    InstallSummary.ConfirmedAt (2.1); MetaResponse.Views, HealthResponse (2.2)
pkg/api/handlers.go                 Published once per request (2.1); routes and lookup superset (2.2)
pkg/api/explore_types.go            LinkedRef, Page, rows, details, ParentSummary DTOs (2.2); Computed, Attachment (2.3); DelegationRow, EffectiveDTO (2.4)
pkg/api/link.go                     link(): admission + Authorizer (2.2)
pkg/api/params.go                   query-parameter parsing and 400s (2.2)
pkg/api/lists.go                    routes, gateways, listenersets handlers (2.2)
pkg/api/details.go                  gateway, listenerset, route detail handlers (2.2); delegation endpoint (2.4)
pkg/api/visible.go                  dependency checks, derived recounts, evidenceRestricted (2.2, extended 2.3, 2.4)
pkg/api/explain.go                  view A explanation text (2.3)
pkg/api/budget_test.go              //go:build bench — list and detail latency (2.2)
pkg/api/testdata/golden/            new fixtures per sub-phase (design §7.6)
pkg/api/testdata/golden-v1/         frozen Phase 1 goldens for the additive-only test (2.1)
pkg/extension/extension.go          WithComputedRules (2.3)
pkg/app/app.go                      trigger wiring, safety timer (2.1)
cmd/kgateway-ui/main.go             --poll-interval default 30s (2.1)
internal/testutil/scale/scale.go    ScaleWorld per tier (2.1); dense selection (2.4)
internal/testutil/fixtures.go       Backend, delegation, rule fixtures (2.2); Gateways with listener specs (2.3)
internal/testutil/rulecase.go       rule-case fixture loader shared by pkg/computed and pkg/api (2.3)
internal/boundary/imports_test.go   allowlist entry for pkg/install/settings.go (2.3)
internal/testutil/clock.go          fake clock helpers (2.1)
web/packages/shell/src/api/types.ts           InstallSummary.confirmedAt (2.1); re-exports explore types (2.2)
web/packages/shell/src/api/exploreTypes.ts    explorer DTOs (2.2, 2.3, 2.4)
web/packages/shell/src/api/exploreHooks.ts    list and detail hooks (2.2); useDelegation (2.4)
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
web/e2e/explorer.spec.ts            Playwright over the kind fixtures (2.2; extended 2.3, 2.4)
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
- Create: `docs/phases/phase-2/phase-2.1-brief.md`
- Modify: `docs/phase-status.md` (sub-phase 2.1 active, base SHA), `test/e2e/setup.sh` (experimental CRDs)
- Modify: `docs/decisions.md` (append D-021)
- Modify: `docs/phases/phase-2/computed-rules.md` §10 (mark each item answered, link D-021)
- Create: `test/e2e/fixtures/observe/*.yaml` (one manifest per item, reused by 2.3/2.4 e2e)

**Interfaces:** Produces facts for 2.3 and 2.4 (rule sets, the `verified` mark of rules §8.3) and the observation manifests.

- [ ] **Step 1: Record the sub-phase and write `docs/phases/phase-2/phase-2.1-brief.md`** (Lifecycle above; agentic-engineering-sdlc format): goal, Tasks 1–10, exit criteria from the Sub-phases table, file list.
- [ ] **Step 2: Install the experimental Gateway API CRDs** in `test/e2e/setup.sh`: the `experimental-install.yaml` bundle of `$GWAPI_VERSION` replaces `standard-install.yaml`, so XListenerSet, ListenerSet, TCPRoute and TLSRoute exist for the observations and for every later e2e. After setup, `kubectl get crd xlistenersets.gateway.networking.x-k8s.io tcproutes.gateway.networking.k8s.io tlsroutes.gateway.networking.k8s.io` succeeds on both pairs, and the Phase 1 e2e suite still passes.
- [ ] **Step 3: Write one manifest per item** under `test/e2e/fixtures/observe/` (Gateway `infra/edge` with listeners `http` 8080 and `https` 8443 hostname `*.example.com`; parent HTTPRoute `shop/api` with delegation backendRefs; children as each item needs).
- [ ] **Step 4: Run each item on v2.4.7, then v2.3.11** (`KGW_VERSION=v2.4.7 GWAPI_VERSION=v1.6.1 bash test/e2e/setup.sh`, then the pair `v2.3.11`/`v1.5.1`). For each: `kubectl apply`, wait for `observedGeneration` to match, record `kubectl get httproute -A -o json | jq '[.items[] | {n:.metadata.name, gen:.metadata.generation, parents:.status.parents}]'` and the Gateway's `status.listeners[].attachedRoutes`. Item 1 and 6 need a second apply that changes the selection; record status before and after.
- [ ] **Step 5: Append D-021** to `docs/decisions.md` in the existing format: Context (rules §10), Decision (per item: tag, observed parentRef of the entry, entry present or cleared, `attachedRoutes`, verbatim excerpt), Alternatives "n/a (observation)", Consequences (rule-set or `verified` changes for 2.3/2.4). If an observation contradicts design §4.2 (reported-edge eligibility) STOP and ask the user before Task 14.
- [ ] **Step 6: Commit**

```bash
git add docs test/e2e/setup.sh test/e2e/fixtures/observe
git commit -m "docs: record delegation and attachment status observations (D-021)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 2: Scale world and the Phase 1 baseline

Runs on Phase 1 code, before Task 3 touches the engine.

**Files:**
- Create: `internal/testutil/scale.go`, `internal/testutil/scale_test.go`, `pkg/engine/bench_test.go`, `docs/phases/phase-2/bench/2.1-baseline.txt`
- Modify: `Makefile` (target `bench`), `internal/testutil/fixtures.go` (`ScaleNamespaces`; the file is already on the boundary allowlist, so `scale.go` never imports `k8s.io/api`)

**Interfaces:**
- Produces:
  - `type Tier struct { Name string; Gateways, ListenersPerGateway, HTTPRoutes int; ParentRefsMean float64; ListenerSets int; DelegatedPct float64; MaxDepth, MaxFanOut, ParentMatches int; Namespaces, Backends int; LatticeDepth int }`
  - `var Typical, Large, Extreme Tier` (values from Global Constraints)
  - `func ScaleWorld(t Tier, seed int64) []*unstructured.Unstructured` (in `scale.go`) and `func ScaleNamespaces(t Tier, seed int64) []*corev1.Namespace` (in `fixtures.go`) — deterministic per seed; every route carries `AcceptedBy` status for its parents except the 5 % failed parentRefs (`Accepted=False/NotAllowedByListeners`); delegated children carry a reported status entry for their parent route; Backends are referenced by backendRefs.
  - `func ScaleSnapshot(t Tier, seed int64) *cache.Snapshot`

- [ ] **Step 1: Write the failing shape test**

```go
func TestScaleWorldMatchesTier(t *testing.T) {
	objs, nss := ScaleWorld(Large, 1), ScaleNamespaces(Large, 1)
	c := countKinds(objs) // test helper: kind → count
	if c["Gateway"] != 100 || c["ListenerSet"] != 200 || c["Backend"] != 200 || len(nss) != 100 { t.Fatalf("%v", c) }
	if got := c["HTTPRoute"]; got != 2000 { t.Fatalf("HTTPRoutes %d", got) }
	for _, k := range []string{"GRPCRoute", "TCPRoute", "TLSRoute"} { if c[k] != 100 { t.Fatalf("%s %d, want 5%% of 2000", k, c[k]) } }
	if d := delegatedChildren(objs); d < 190 || d > 210 { t.Fatalf("delegated %d", d) }
	if maxDepth(objs) > 3 || maxFanOut(objs) > 50 { t.Fatal("delegation shape") }
	a, b := ScaleWorld(Large, 1), ScaleWorld(Large, 1)
	if !reflect.DeepEqual(a, b) { t.Fatal("not deterministic") }
}
```

- [ ] **Step 2: Run** `go test ./internal/testutil -run TestScaleWorldMatchesTier` → FAIL (`undefined: ScaleWorld`).
- [ ] **Step 3: Implement `ScaleWorld`** with `math/rand/v2` seeded by `seed`; names `ns-%03d`, `gw-%03d`, `r-%05d`; parentRef count per route drawn so the mean matches `ParentRefsMean`; one in four namespaces labelled `team=web` with one `Selector` listener per Gateway selecting it.
- [ ] **Step 4: Run** the test and `go test ./internal/boundary` → PASS.
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
- [ ] **Step 4: Commit** `feat(engine): metrics and served-kind triggers (D-019)`.

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
- [ ] **Step 4: Commit** `feat(engine): scheduler with debounce, deadlines and single flight (D-019)`.

### Task 6: Engine loop, publication, confirmation and deadlines

**Files:**
- Create: `pkg/engine/loop.go`, `pkg/engine/loop_test.go`, `internal/testutil/clock.go`
- Modify: `pkg/engine/engine.go:25-55` (Config), `:65-74` (Model.Next, Model.Seq), `:76-91` (atomic pointer), `:143-185` (Compute publishes through `publish` and collects deadlines), `:187-192` (Current), `:194-205` (`Installs()` reads `Current().Model.Installs`), `:207-222` (Run replaced by loop.go)
- Modify: `pkg/engine/engine_test.go:136` and `:226-249` (read `Current().Model`; `TestRunCallsOnComputeWithEachModel` keeps asserting `OnCompute`), `pkg/app/app_test.go` (one `Current()` call), `go.mod` (`k8s.io/utils` direct)

**Interfaces:**
- Consumes: `sched`, `Deadline` (Task 5); `cache.Fingerprinter`, `Snapshot.Source` (Task 3); `trigger.Signal`.
- Produces:
  - `type Source struct { Cache cache.Fingerprint; Metrics uint64; Kinds string }`; `func (s Source) Equal(o Source) bool`
  - `type Published struct { Model *Model; Source Source; ConfirmedAt time.Time }` — never mutated after publication
  - `func (e *Engine) Current() *Published` (nil before the first publication)
  - `Model.Seq uint64` — assigned under `computeMu`, strictly increasing
  - `func (e *Engine) Compute() *Model` — computes and publishes `Published{m, src, snap.TakenAt}` through `publish`. Once `Run` starts, only the loop goroutine calls it; `app` never does.
  - `func (e *Engine) publish(next *Published)` — a compare-and-swap loop that never replaces a value with a higher `Model.Seq`
  - `func (e *Engine) confirm(prev *Published, now time.Time) bool` — `CompareAndSwap(prev, &Published{prev.Model, prev.Source, now})`; false when anything was published after `prev` was read, so a confirmation never restores an older model
  - `Engine.Installs()` reads `Current().Model.Installs` (nil-safe); `Config.OnCompute` stays and is called after each recompute publication, never after a confirmation (`app.Build` closes `discovered` with it, which starts the metrics collector)
  - `func (e *Engine) Run(ctx context.Context, safety time.Duration)` — the scheduler goroutine
  - `Model.Next Deadline` — earliest future grace expiry (`firstSeen + health.Grace`, non-strict) and metrics staleness (`CollectedAt + MetricsMaxAge`, strict); 2.3 adds KGWUI-012 maturation through `func (d *deadlines) add(at time.Time, strict bool)`
  - `Config.Triggers *trigger.Signal`; `Config.Clock clock.WithTicker` (nil: `clock.RealClock{}`); `Config.ConfirmEvery time.Duration` (zero: `ConfirmEvery`)
  - `type LoopEventKind int` (`EvTriggered`, `EvArmed`, `EvRunStarted`, `EvPublished`, `EvConfirmed`, `EvConfirmRefused`); `type LoopEvent struct { Kind LoopEventKind; At time.Time; Published *Published }`; `Config.OnLoop func(LoopEvent)` — test hook, called synchronously on the loop goroutine after each transition. Tests wait for the event a transition produces before they advance the fake clock, and count runs by `EvPublished`, never by confirmations.
  - Tests set `Config.Now` to the fake clock's `Now` and `cache.Config.Now` likewise, so snapshot, compute and confirmation times share one clock.
  - `internal/testutil`: `func FakeClock() *clocktesting.FakeClock` at `Now`; `loop_test.go`: `func awaitThenStep(t *testing.T, ev <-chan LoopEvent, want LoopEventKind, c *clocktesting.FakeClock, d time.Duration)` — receives up to `want` (bounded by `testutil.Recv`), then steps
  - `type MetricsRead struct { ID string; Result ctrlmetrics.Result; Present bool }`; `Model.MetricsRead []MetricsRead` — `Compute` calls `Metrics.Get` exactly once per install and hands those captured values to the verifier and health, never a second read
  - `func encodeMetrics(reads []MetricsRead) uint64` — FNV-64a over, per read in install order: the ID, `Present`, and for a present result `CollectedAt.UnixNano`, `Err`, `ManagedPresent` and every `Rejects` and `Managed` entry in sorted key order. `Source.Metrics` is `encodeMetrics(m.MetricsRead)`, the evidence the model used; the confirm tick reads the live results for the same IDs once and applies the same encoding. Kinds identity: `KindsChange().String()`.

- [ ] **Step 1: Write the failing tests** (fake clock; a `fakeSource` implementing `cache.Snapshotter` and `cache.Fingerprinter` whose store can change without notifying, and whose `Snapshot` can block on a channel)

```go
func TestOneRecomputePerBurst(t *testing.T)            // 500 Notify within 100 ms → exactly one EvPublished, at last+250 ms
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
	// step 1 s: EvConfirmRefused, ConfirmedAt unchanged, state pending.
	// awaitThenStep(EvArmed, 250 ms): EvPublished with F2 although Notify never fired.
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
func TestConfirmNeverRestoresOlderModel(t *testing.T)  // confirm reads p1; a direct Compute publishes p2 before the swap (test seam between read and CAS) → EvConfirmRefused, Current().Model is p2's
func TestMetricsIdentityCoversContent(t *testing.T)    // same CollectedAt, different Rejects → different encoding; absent vs present empty result → different; equal results → equal
func TestMetricsFingerprintIsEvidenceRead(t *testing.T) // a Source that returns A on its first Get and B after: health saw A, Source.Metrics == encodeMetrics(A); the next confirm tick reads B → EvConfirmRefused and one run that uses B
func TestOnComputeOnlyOnRecompute(t *testing.T)        // OnCompute once per EvPublished, never on EvConfirmed
```

- [ ] **Step 2: Run** `go test ./pkg/engine -race -run 'Burst|Stream|Trigger|Safety|Change|Confirm|Access|Blocked|Concurrent|Deadline|Metrics|OnCompute'` → FAIL.
- [ ] **Step 3: Implement.** `Compute` keeps `computeMu`; publication is `atomic.Pointer[Published]`. `Run` owns one `sched`; it selects on `ctx.Done()`, `Triggers.C()`, a timer reset to `sched.wake()`, a `ConfirmEvery` ticker and a `done` channel from the run goroutine. A run executes `Compute` in its own goroutine so the loop keeps receiving triggers; after it, `setDeadline(m.Next)` and `setSafety(now + safety)`. The confirm tick runs only when `sched.idle()`: it builds the live `Source` (`Fingerprint()` when the snapshotter implements `cache.Fingerprinter`, else `Snapshot().Source`) and either calls `confirm(p, now)` or `sched.trigger(now)`. `Run` computes immediately once at start, as Phase 1 does.
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
- Produces: wire field `confirmedAt` on each install; `func handler.load(w) *engine.Published` (503 when not synced); `func additive(t *testing.T, old, cur any, path string)` (test helper: every key and value of `old` exists in `cur`; a `"<snapshot time>"` string in `old` matches any UTC RFC 3339 value; an array in `cur` keeps `old`'s elements at their indexes, in their order, and may append only the elements listed for its path. Paths are JSON paths inside the fixture document, including the `body` wrapper, independent of the fixture name; appended elements are identified by a key field: `var appended = map[string]struct{ Key string; Values []string }{"body.checks": {"name", []string{"backend status", "computed views"}}, "body.counts": {"kind", []string{"Backend"}}, "body.kinds": {"kind", []string{"Backend"}}}`, in that order (design §7.6). This covers `meta`, `bare-meta`, `overview`, `bare-overview` and `health`.)

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
func TestAdditiveHelper(t *testing.T)   // unit: an appended Backend kind in body.kinds and a computed views check in body.checks pass; a reordered element, an appended unlisted element, or a removed key fails
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
- [ ] **Step 4: Commit** `feat(shell): freshness reads confirmedAt with a computedAt fallback (D-019)`.

### Task 9: Wiring, the safety-timer flag and the 2.1 budgets

**Files:**
- Modify: `pkg/app/app.go:39-40` (doc: maximum time between recomputes; test-only `Config.ConfirmEvery` and `Config.OnLoop` passed to the engine; `OnCompute` still closes `discovered`), `:96` (`cache.Config.Changes`), `:105` (`col.OnResult`), `:90` (`mon.OnChange`), `:114-121` (`engine.Config.Triggers`), `:131` (`Run(ctx, PollInterval)` as the safety timer)
- Modify: `cmd/kgateway-ui/main.go:23` (default `30*time.Second`, help "maximum time between model recomputes (safety timer)")
- Modify: `README.md:44`, `deploy/helm/kgateway-ui/README.md` (a "Recompute" note: event-driven, `--poll-interval` is the safety timer, 2s restores Phase 1's cadence as a floor)
- Create: `pkg/engine/budget_test.go` (`//go:build bench`)
- Test: `pkg/app/app_test.go`

**Interfaces:** Consumes Tasks 3–6. Produces `make bench` output for the 2.1 record.

- [ ] **Step 1: Write the failing wiring test** `TestAppRecomputesOnObjectChange`: build the app over fake clients with `PollInterval: time.Hour` and `ConfirmEvery: time.Hour`, so neither the safety timer nor a confirmation scan can explain a run; create an HTTPRoute; receive `OnLoop` events: `EvTriggered`, `EvArmed`, `EvRunStarted`, then `EvPublished` whose snapshot holds the route, and no `EvConfirmed` or `EvConfirmRefused`.
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
- Modify: `docs/phase-status.md`, `docs/roadmap.md`, `docs/phases/phase-2/design.md` (only where the code deviates)

- [ ] **Step 1: Run the gates** and paste the outputs into the record: `go vet ./... && go vet -tags e2e ./...`, `go test ./... -race`, `TZ=Europe/Amsterdam go test ./pkg/api`, `make lint`, `cd web && npm test && npm run typecheck && npm run lint && npm run build`, the forbidden-terms check, and the kind e2e matrix (CI or `make e2e` for both pairs).
- [ ] **Step 2: Request the cross-model review**

```bash
codex exec --sandbox read-only -o docs/phases/phase-2/reviews/2.1-codex.md "Review sub-phase 2.1 of the kgateway UI (diff $BASE..HEAD) against docs/phases/phase-2/design.md §3.1, §9.3, §9.6 and D-019. Focus: confirmation can never stamp an old model as current; single flight; deadlines strict vs non-strict; atomic publication; additive-only goldens; SPA fallback. Test output: <paste>. Rank findings P1–P4 with file:line." < /dev/null
```

- [ ] **Step 3: Triage** every finding in the review file (accept, reject with reason, defer to Carry-over); fix accepted ones with tests; rerun Step 1.
- [ ] **Step 4: Write `phase-2.1-complete.md`** in the Phase 1 record format (Summary, Reviews, Waivers, Deliverables with gate outputs, benchmark table: baseline vs 2.1 per tier) and update `docs/phase-status.md` (queue, `## Sub-phase` none, next 2.2) and `docs/roadmap.md` (2.1 shipped).
- [ ] **Step 5: Commit** `docs: close sub-phase 2.1`.

---

# Sub-phase 2.2 — Explorer on observed status

### Task 11: The `Backend` kind and the ListenerSet kind contract

**Files:**
- Create: `docs/phases/phase-2/phase-2.2-brief.md`; Modify: `docs/phase-status.md`
- Modify: `pkg/kinds/kinds.go:1-3` (package doc: a downstream ListenerSet kind registers with `CategoryListenerSet` and follows `spec.parentRef`, `spec.listeners[]`, `status.listeners[]`), `:17-29` (`CategoryBackend Category = "backend"`), `pkg/kinds/upstream.go:17-34` (Backend descriptor)
- Modify: `web/packages/shell/src/api/types.ts:5` (`"backend"` in `Category`)
- Test: `pkg/kinds/kinds_test.go`, `pkg/scopeverify/verify_test.go`, `deploy/helm/chart_test.go`

**Interfaces:** Produces `kinds.CategoryBackend`; descriptor `{Group: "gateway.kgateway.dev", Kind: "Backend", Resource: "backends", Versions: ["v1alpha1"], Namespaced: true, Category: CategoryBackend, DisplayName: "Backends"}`. Appended last to `kinds.Upstream()`, so `meta.kinds` gains it at the end (Task 7's `appended`).

- [ ] **Step 1: Record the sub-phase and write `docs/phases/phase-2/phase-2.2-brief.md`** (Lifecycle above; agentic-engineering-sdlc format): goal, Tasks 11–34, exit criteria, file list, the Q12 count change.
- [ ] **Step 2: Write the failing tests** `TestUpstreamHasBackend` (descriptor as above), `TestVerifierIgnoresBackend` (a Backend in scope does not change KGWUI-007's comparison) and `TestClusterRoleCoversBackends` (rendered ClusterRole grants get/list/watch on `gateway.kgateway.dev` `*` or `backends`).
- [ ] **Step 3: Run** `go test ./pkg/kinds ./pkg/scopeverify ./deploy/helm -run 'Backend'` → FAIL.
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
  - `type WrittenRef struct { Group, Kind, Namespace, Name, SectionName string; Port *int32 }` — a parentRef as written, omitted fields as empty strings (design §4.1, computed-rules §7); `func WrittenParentRefs(u) []WrittenRef` for any route kind, index-aligned with `RouteParentRefs`; `HTTPRouteView.WrittenRefs []WrittenRef`; `RouteParentStatus.Written WrittenRef` (the status parentRef as written, `json:"-"`). Report keys and persistence identities use the written form; lookup and ownership use the resolved one.
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
func TestWrittenAndResolvedRefs(t *testing.T)  // spec and status refs with omitted group, kind and namespace: Written has "", the resolved ref has the defaults; an explicit namespace equal to the default stays distinct in Written
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
- [ ] **Step 4: Commit** `feat(engine): decode cache keyed by uid and resourceVersion`.

### Task 14: Reported edges and status-based ownership of delegated children (Q12)

**Files:**
- Create: `pkg/install/delegation.go`, `pkg/install/delegation_test.go`
- Modify: `pkg/install/assign.go:14-22` (`OwnedObject.RouteParents`), `:32-40` (Owned fields of Task 15 declared here as empty), `:99-135` (`exists` also holds every HTTPRoute in the snapshot, built before the missing-parent branch at `:172`), `:146-151` (HTTPRoute joins the parent kinds), `:153-189` (Gateway-attached routes seed the BFS), end of `Assign` (call `delegate`)
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
func TestExistingHTTPRouteParentIsNotMissing(t *testing.T) // a child naming an existing HTTPRoute gets no KGWUI-003; a missing one does
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

- [ ] **Step 2: Run** `go test ./pkg/install ./pkg/api ./pkg/health` → PASS.
- [ ] **Step 3: Commit** `feat(install): declared, no-parent and Backend candidates`.

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
- Modify: `pkg/health/checks.go:23-25` (register `backendCheck`, named `backend status`, not required, appended after Phase 1's checks; design §7.6)

**Interfaces:**
- Consumes: `Owned.Backends` (Task 15), `objects.DecodeBackend` (Task 12).
- Produces: `func BackendState(b *unstructured.Unstructured) (health.State, *metav1.Condition)` — used by the explorer for `backend.state`.

- [ ] **Step 1: Write the failing table test** `TestBackendAcceptedRules` with the four rows of design §6.2: current `False` → KGWUI-002 error; current `True` → no finding, `healthy`; `observedGeneration` > 0 and lower → no finding, `unknown`; `observedGeneration` 0 → `False` gives KGWUI-002 warning with detail "The controller does not say which version of this Backend it checked" (draft), `True` gives `unknown`; absent → no finding, `unknown`. Every row: no failed required check and no "not yet processed" finding.
- [ ] **Step 2: Run** `go test ./pkg/health` → PASS.
- [ ] **Step 3: Commit** `feat(health): Backend Accepted from the condition's own evidence (Q11)`.

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
func TestSortsBothWaysWithTieBreak(t *testing.T) // every sort ends with (ns, name, group, kind); name.desc returns z before a, and its cursors page in that order
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
- [ ] **Step 3: Implement.** Candidate set from the narrowest index (parent, then backend, then namespace, else all rows), filtered, `visible` applied, sorted with `slices.SortStableFunc` unless the sort is `name.asc` (rows are already in that order; `name.desc` needs the sort or a reversal), then the cursor position by `sort.Search` over the full sorted slice.
- [ ] **Step 4: Commit** `feat(explore): filters, sorts and keyset cursors`.

### Task 20: Bounded up-chain walk

**Files:**
- Create: `pkg/explore/ancestors.go`, `pkg/explore/ancestors_test.go`

**Interfaces:**
- Produces: `type AncestorStep struct { Ref objects.Ref; Restricted bool }`; `type AncestorPath struct { Steps []AncestorStep; Cycle bool }`; `func (ix *Index) Ancestors(route objects.Ref, visible func(objects.Ref) bool) (paths []AncestorPath, truncated bool)`; `const MaxPaths, MaxDepth, MaxNodes = 5, 32, 256`.

- [ ] **Step 1: Write the failing tests** `TestAncestorsChain` (child → parent → Gateway: one path, root first), `TestAncestorsDiamond` (two paths, parents sorted by namespace then name), `TestAncestorsCycleMarker` (a path meeting a route already on it ends with `Cycle: true`), `TestAncestorsLatticeTruncates` (8-layer repeated diamond: 5 paths, `truncated`, never a remaining count), `TestAncestorsNodeBound` (256 node visits stop the walk), `TestAncestorsRestrictedStep` (`visible` false → `Restricted: true`, zero `Ref`).
- [ ] **Step 2: Implement** depth-first over `ParentRoutes` (reported edges only), then the route's Gateway or ListenerSet parents from `ParentLink.Target`; stop at whichever bound comes first.
- [ ] **Step 3: Commit** `feat(explore): bounded up-chain over reported edges`.

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
- [ ] **Step 3: Run** `go test ./pkg/api` → PASS except `TestGoldenWireFormat` for `health`, `overview`, `bare-overview` (regenerated in Task 25; `TestGoldenChangesAreAdditive` must still pass after regeneration).
- [ ] **Step 4: Commit** `feat(api): link metadata from one admission function`.

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
- [ ] **Step 4: Commit** `feat(api): routes, gateways and listenersets lists`.

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
  - `type MatchDTO struct { Path *PathMatchDTO \`json:"path,omitempty"\`; Headers []KeyMatchDTO \`json:"headers,omitempty"\`; QueryParams []KeyMatchDTO \`json:"queryParams,omitempty"\`; Method string \`json:"method,omitempty"\` }` (design §7.3 names it without fields; `objects.Match` after defaults; reused by 2.4's contexts and effective matches)
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

- [ ] **Step 2: Run** `go test ./pkg/api` → PASS (goldens aside).
- [ ] **Step 3: Commit** `feat(api): Gateway, ListenerSet and HTTPRoute detail endpoints`.

### Task 24: Authorizer rules for 2.2's facts

**Files:**
- Create: `pkg/api/visible.go`, `pkg/api/visible_test.go`
- Modify: `pkg/api/lists.go`, `pkg/api/details.go`, `pkg/api/handlers.go:110-128` (evidence restriction)

**Interfaces:**
- Produces: `type view struct{...}` per request with `can(ref objects.Ref) bool` (memoised), `deniedInInstall(kinds []objects.GroupKind) int`, `recount(ids []explore.RowID) int`; rules 1, 2, 3, 5, 7, 8, 9, 10 of design §7.4 for 2.2's data. 2.3/2.4 add rules 4 and 6 through `func (v *view) allowDeps(ix *explore.Index, d explore.DepSet) bool` (Task 44): dependency sets are indexes into a per-install ref table (design §7.4 rule 10).

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

- [ ] **Step 2: Implement.** Replacement detail text comes from `copy` constants finalised in Task 31.
- [ ] **Step 3: Run** `go test ./pkg/api -race` → PASS.
- [ ] **Step 4: Commit** `feat(api): Authorizer rules for explorer lists and details`.

### Task 25: Goldens for 2.2, Go and TypeScript

**Files:**
- Modify: `pkg/api/golden_test.go:31-78` (cases; a delegation world: three-level chain, diamond, cycle, lattice, Backend ref, ListenerSet with an invalid listener, declared child), `pkg/api/additive_test.go` (new files are not in `golden-v1`)
- Create: `pkg/api/testdata/golden/{routes,routes-filtered,gateways,listenersets,gateway-detail,listenerset-detail,route-detail,route-detail-candidate,bad-request,route-detail-restricted,routes-restricted,routes-by-backend,routes-by-backend-restricted}.json` — `listenerset-detail.json` holds a `NotAllowed` ListenerSet whose parent lacks `allowedListeners` and one whose parent sets it; `route-detail.json` holds a mirror filter (percent and fraction), absent vs explicit 1 and 0 weights, absent vs explicit port, and several filters of different types in one rule and one backendRef; `routes-by-backend.json` queries a Service, a Backend, a mirror target and two backends OR-ed; the restricted variant holds the same query for a denied and an absent backend, with identical bodies
- Regenerate: `meta.json` (`views`, Backend kind), `health.json` (`link`, the appended `backend status` check), `overview.json`, `bare-overview.json` (`link`, the appended Backends tile)
- Modify: `web/packages/shell/src/api/types.golden.test.ts` (import and shape-check each new fixture)

- [ ] **Step 1: Add the cases and run** `go test ./pkg/api -run Golden` → FAIL (files missing).
- [ ] **Step 2: Regenerate** with `-update`; read every new file; check `TestGoldenChangesAreAdditive` passes over the regenerated Phase 1 files.
- [ ] **Step 3: Add `TestGoldenLinksAgreeWithEndpoints`:** walk every `link` in every golden body; `detail` → the detail endpoint returns 200; `yaml` → the resource endpoint returns 200; `none` on a denied or absent object → the resource endpoint and any detail endpoint for its kind return 404. A `none` on a never-served kind (Secret, Service) asserts only the resource endpoint's 404, and a `yaml` link asserts nothing about the detail endpoint.
- [ ] **Step 4: Run** `go test ./pkg/api` → PASS. Commit `test(api): golden wire format for the explorer`. The TypeScript shape checks of these fixtures are in Task 26.

### Task 26: Frontend contract: types, hooks, links, slots and navigation

**Files:**
- Create: `web/packages/shell/src/api/exploreTypes.ts`, `src/api/exploreHooks.ts`, `src/api/exploreHooks.test.tsx`, `src/explore/objectPath.ts`, `src/explore/objectPath.test.ts`, `src/explore/LinkedName.tsx`, `src/explore/LinkedName.test.tsx`, `src/explore/CopyCommandButton.tsx`, `src/shell/InstallNav.tsx`
- Modify: `src/api/types.ts` (re-export), `src/api/types.golden.test.ts` (import and shape-check every Task 25 fixture), `src/shell/vocabulary.tsx:5-43` (`getCommand`), `src/shell/Header.tsx` (InstallNav), `src/index.ts` (exports; `SHELL_API_VERSION` stays 1)
- Test: `src/index.test.ts`

**Interfaces:**
- Consumes: the DTOs of Tasks 21–23.
- Produces:
  - TS types mirroring each Go DTO (`Link`, `LinkedRef`, `Page<T>`, `RouteRow`, `GatewayListRow`, `ListenerSetRow`, `ParentSummary`, `GatewayDetail`, `ListenerSetDetail`, `RouteDetail`, …)
  - `export function objectPath(installId: string, r: LinkedRef): string | undefined` — `detail` → `/i/:id/gateways/:ns/:name`, `/i/:id/listenersets/:group/:kind/:ns/:name` or `/i/:id/routes/:group/:kind/:ns/:name` by the ref's kind; `yaml` → `resourcePath`; `none` → `undefined`
  - `useRoutes(id, query: URLSearchParams)`, `useGateways`, `useListenerSets`, `useGatewayDetail(id, ns, name)`, `useListenerSetDetail(id, ref)`, `useRouteDetail(id, ref)` — SWR, 5 s poll, no `keepPreviousData`; an `ApiError` 400 on a cursor reports `cursorRejected: true`
    - Lists keep previous data while a new query loads, marked busy; an error on the new query never shows the old rows (Task 27 ruling).
  - `export function LinkedName({ refValue, installId }: { refValue: LinkedRef; installId: string })`
  - `Vocabulary.getCommand: (ref: Ref, resource: string) => string | undefined` — default `kubectl get <resource>.<group> -n <ns> <name> -o yaml` (no `.<group>` for the core group, no `-n` when cluster-scoped)

- [ ] **Step 1: Write the failing tests**

```ts
test("objectPath follows link only", () => {
  expect(objectPath("a.b", { group: gw, kind: "HTTPRoute", namespace: "shop", name: "x", link: "detail" })).toBe("/i/a.b/routes/gateway.networking.k8s.io/HTTPRoute/shop/x");
  expect(objectPath("a.b", { ...ref, kind: "GRPCRoute", link: "yaml" })).toBe("/i/a.b/r/gateway.networking.k8s.io/GRPCRoute/shop/x");
  expect(objectPath("a.b", { ...ref, link: "none" })).toBeUndefined();
});
it("getCommand returning undefined hides the copy button", ...);
it("never shows the previous install's rows on a switch", ...);       // response.install must match the URL
it("a 400 on a cursor drops it and loads page one", ...);
// types.golden.test.ts: every Task 25 fixture parses against its Spec<T>; tsc fails when a DTO drifts.
```

- [ ] **Step 2: Run** `cd web && npx vitest run packages/shell/src/explore packages/shell/src/shell packages/shell/src/api` → FAIL.
- [ ] **Step 3: Implement.** Install nav: Overview, Gateways, Routes, ListenerSets, Findings; `aria-current="page"` on the active entry; hover, active, focus-visible styles.
- [ ] **Step 4: Commit** `feat(shell): explorer types, hooks, links and install navigation`.

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

- [ ] **Step 2: Implement** with TanStack Table (manual sorting and pagination: the server sorts).
- [ ] **Step 3: Commit** `feat(shell): Gateways, ListenerSets and Routes lists`.

### Task 28: Gateway and ListenerSet detail pages

**Files:**
- Create: `web/packages/shell/src/pages/explore/{GatewayPage,ListenerSetPage}.tsx`, `src/pages/explore/gateway/ListenerTable.tsx`, `src/explore/{ConditionList,CandidateBanner,DetailTabs}.tsx`, `src/pages/explore/details.test.tsx`

**Interfaces:**
- Consumes: `useGatewayDetail`, `useListenerSetDetail`, `useRoutes` (`parent=`, `attachment=`), `useListenerSets` (`parent=`), `LinkedName`, `CopyCommandButton`, `CopyNameButton`.
- Produces: `DetailTabs` (WAI-ARIA tabs, `?tab=yaml` reuses the YAML view body from `ResourcePage.tsx`), `ConditionList` (icon + text, reason, message, freshness "Describes generation N; the object is at M"), `CandidateBanner` ("Not part of this install. Shown because …").

- [ ] **Step 1: Write the failing tests** `"Gateway page shows status-only listener counts"` (attachedRoutes from status; no computed column in 2.2), `"failed-route table has Not accepted, Waiting for status and Unwatched namespace sub-filters"`, `"ListenerSets are paged through the list endpoint"`, `"ListenerSet page says when the parent Gateway is missing"`, `"NotAllowed shows the parent's allowedListeners as written, or Not set with the CRD hint"`, `"an accepted ListenerSet shows no allowed-listeners block"`, `"candidate banner above the header"`, `"polling keeps scroll, focus and expanded rows"`.
- [ ] **Step 2: Commit** `feat(shell): Gateway and ListenerSet detail pages`.

### Task 29: HTTPRoute detail page

**Files:**
- Create: `web/packages/shell/src/pages/explore/RoutePage.tsx`, `src/pages/explore/route/{UpChain,ParentCard,RuleList,ChildRoutes}.tsx`, `src/pages/explore/route.test.tsx`

**Interfaces:**
- Consumes: `useRouteDetail`, `useRoutes(parent=<route>)` for child routes.
- Produces: `UpChain` as `nav aria-label="Route parents"`; `ParentCard` per parentRef (target, section or port, every condition, other controllers' entries collapsed); `RuleList` (matches, filters with config as written, backendRefs with weight, delegation refs with mode and status state, Backend state or "Restricted").

- [ ] **Step 1: Write the failing tests** `"up-chain shows a restricted step and 'More paths not shown'"`, `"hostnames or inherited text for a child"`, `"permission line: Allows any parent / Allows: shop/api"`, `"child routes are paged and fetched when expanded"`, `"other controllers' entries are collapsed"`, `"each backendRef links to Other routes using this backend (routes?backend=…)"`, `"absent weight shows 1 marked as defaulted; absent port shows nothing"`, `"mirror filters show their target and percentage; filters keep written order"`.
- [ ] **Step 2: Commit** `feat(shell): HTTPRoute detail page`.

### Task 30: Page slots, finding links and the Backend reverse lookup

Comes after Tasks 27–29, whose pages the slots fall back to.

**Files:**
- Modify: `web/packages/shell/src/shell/extension.ts:14-22` (reserved paths doc), `:48-54` (optional slots), `src/shell/Shell.tsx:50-71` (routes, slot fallback), `src/pages/defaultPages.ts`, `src/shell/extension.test.tsx`
- Modify: `web/packages/shell/src/pages/install/FindingRow.tsx:76-79` (keep `resourcePath`), `:121` (`objectPath(installId, f.resource)`), `src/pages/install/TopFindings.tsx`, `src/pages/install/GatewayTable.tsx:192,265` (row `link`), `src/pages/ResourcePage.tsx:135-192` ("Routes that send traffic here" on an admitted Backend's YAML view)
- Test: `src/pages/install/InstallPage.test.tsx`, `src/pages/pages.test.tsx`

**Interfaces:** Produces `CorePages`' optional `GatewaysPage`, `GatewayPage`, `ListenerSetsPage`, `ListenerSetPage`, `RoutesPage`, `RoutePage`.

- [ ] **Step 1: Write the failing tests** `"renders new pages with only the four Phase 1 slots"` (`/i/x/routes` renders the upstream RoutesPage), `"a contribution at routes cannot replace the core page"`, `"finding on an HTTPRoute opens its detail page"`, `"restricted finding never links"`, `"Gateway table row opens the Gateway page"`, `"a finding with link yaml opens the YAML view"`, `"a Backend YAML view links to Routes filtered by backend=gateway.kgateway.dev/Backend/ns/name"`, `"a Service has no YAML view entry point"`.
- [ ] **Step 2: Commit** `feat(shell): page slots; finding and Gateway links open detail pages`.

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
- Modify: `README.md` ("Upgrade notes": counts and colours change on clusters that use delegation, design §4.2 and §6.3), `docs/phase-status.md`, `docs/roadmap.md`, `docs/phases/phase-2/design.md` (only where the code deviates)

- [ ] **Step 1: Write the release note** in README "Upgrade notes": delegated child routes with a controller status entry now count in the HTTPRoute tile and in install health; a delegated child's failures can turn the install yellow or red; routes declaring an HTTPRoute parent without status get KGWUI-011.
- [ ] **Step 2: Run the gates** of Task 10 plus `make bench`; paste outputs into the record.
- [ ] **Step 3: Request the cross-model review**

```bash
codex exec --sandbox read-only -o docs/phases/phase-2/reviews/2.2-codex.md "Review sub-phase 2.2 of the kgateway UI (diff $BASE..HEAD) against docs/phases/phase-2/design.md §4.2, §4.3, §6, §7, §8 and D-016/D-019. Focus: Q12 ownership and counts, KGWUI-011 pending in grace, Backend freshness, admission and links, Authorizer rules 1-3, 5, 7-10, cursors, additive goldens, SPA states and accessibility. Test and bench output: <paste>. Rank findings P1–P4 with file:line." < /dev/null
```

- [ ] **Step 4: Triage** every finding; fix accepted ones with tests; rerun the gates.
- [ ] **Step 5: Write `phase-2.2-complete.md`** (Summary, Reviews, wording-pass choices, Waivers, Deliverables, benchmark table vs 2.1, ui-loop recording paths, release note) and update `docs/phase-status.md` (`## Sub-phase` none, next: 2.3) and `docs/roadmap.md` (2.2 shipped).
- [ ] **Step 6: Commit** `docs: close sub-phase 2.2`.

---

# Sub-phase 2.3 — View A

Every 2.3 task reads `computed-rules.md` §1–§4, §8 and §9 for the behaviour it implements; the plan pins names, signatures, values and tests, not the algorithms the rules already write out.

### Task 35: 2.3 brief and view A observations (D-021)

**Files:**
- Create: `docs/phases/phase-2/phase-2.3-brief.md`; Modify: `docs/phase-status.md` (sub-phase 2.3 active, base SHA)
- Modify, only when D-021 requires it: `docs/phases/phase-2/computed-rules.md` (§2, §4, §10, §11.1), `docs/decisions.md` (D-021 Consequences)

**Interfaces:** Consumes D-021 (Task 1). Produces the view A rules Tasks 36–50 implement. **No product code.**

- [ ] **Step 1: Map the observations.** For D-021 items 3 and 10 (the view A items; item 3 returns in 2.4), write in the brief what each tag showed and which rules section it confirms. Expected per rules §4.4: delegated children are not in `attachedRoutes`; ListenerSet routes count only on the ListenerSet's listener status.
- [ ] **Step 2: Gate.** If an observation contradicts rules §2 or §4, STOP and ask the user with the proposed rules text; the approved rules change only with approval. Otherwise mark §10 item 10 "confirmed by D-021".
- [ ] **Step 3: Record the sub-phase** (Lifecycle) **and write the brief** (agentic-engineering-sdlc format): goal, Tasks 35–51, exit criteria from the Sub-phases table, file list, and the provenance gap: no kgateway translator output carries `NotAllowedByListeners`, `NoMatchingParent` or `NoMatchingListenerHostname` at either tag, so those fixtures cite the code and the kind e2e confirms them (Task 50).
- [ ] **Step 4: Commit** `docs: 2.3 brief and view A observations (D-021)`.

### Task 36: Experimental setting and namespace labels

**Files:**
- Create: `pkg/install/settings.go`, `pkg/install/settings_test.go`
- Modify: `pkg/install/scope.go:94-140` (extract the Pod-or-template and container lookup into `controllerContainer`, used by `Scope` and `ReadSettings`; `Scope`'s messages unchanged), `:182-194` (`Resolved.Settings`, filled by `Resolve`), `internal/boundary/imports_test.go:26-39` (allowlist `pkg/install/settings.go`: "controller settings from Pod env; Namespace labels")

**Interfaces:**
- Produces:
  - `type Setting struct { Value string; Known bool; Reason, Source string }` — `Reason` says why it is unknown; `Source` as `Scope.Source`
  - `func (s Setting) Bool() (v, known bool)` — `strconv.ParseBool`; an unparsable value is not known (our call)
  - `type Settings struct { Experimental Setting }` (Task 53 adds `WildcardNamespace`)
  - `const ExperimentalEnv = "KGW_ENABLE_EXPERIMENTAL_GATEWAY_API_FEATURES"`
  - `func ReadSettings(inst Install) Settings` — D-015 order (Ready Pod, any Pod, Deployment template); absent → `"true"`; `valueFrom` → unknown; no entry but `envFrom` → unknown; an explicit entry wins over `envFrom`; the last duplicate wins
  - `Resolved.Settings Settings`
  - `func NamespaceLabels(snap *cache.Snapshot) (labels map[string]map[string]string, ok bool)` — `ok` is `snap.Access[cache.NamespacesGVR].OK`

- [ ] **Step 1: Write the failing tests**

```go
func TestReadExperimental(t *testing.T) {
	cases := []struct {
		name    string
		env     []corev1.EnvVar
		envFrom bool
		want    Setting // Reason compared with strings.Contains
	}{
		{"absent", nil, false, Setting{Value: "true", Known: true}},
		{"explicit false", []corev1.EnvVar{testutil.Env(ExperimentalEnv, "false")}, false, Setting{Value: "false", Known: true}},
		{"last duplicate wins", []corev1.EnvVar{testutil.Env(ExperimentalEnv, "false"), testutil.Env(ExperimentalEnv, "true")}, false, Setting{Value: "true", Known: true}},
		{"valueFrom", []corev1.EnvVar{{Name: ExperimentalEnv, ValueFrom: &corev1.EnvVarSource{}}}, false, Setting{Reason: "valueFrom"}},
		{"envFrom without entry", nil, true, Setting{Reason: "envFrom"}},
		{"entry beats envFrom", []corev1.EnvVar{testutil.Env(ExperimentalEnv, "false")}, true, Setting{Value: "false", Known: true}},
	}
	// each case: install with one Ready Pod; Source == "pod <name>"
}
func TestSettingsSourceOrder(t *testing.T) // Ready Pod beats a non-ready Pod beats the template (Source "deployment template"); no workload → unknown
func TestSettingBool(t *testing.T)         // "true", "False", "1" known; "maybe" → known false
func TestNamespaceLabels(t *testing.T)     // access OK → labels by name, an unlabelled Namespace present with an empty map; access failed → ok false
```

- [ ] **Step 2: Run** `go test ./pkg/install -run 'Experimental|SourceOrder|SettingBool|NamespaceLabels'` → FAIL (undefined).
- [ ] **Step 3: Run** `go test ./pkg/install ./internal/boundary` → PASS; the existing scope tests pass unchanged.
- [ ] **Step 4: Commit** `feat(install): read the experimental-features setting and namespace labels (Q16)`.

### Task 37: The `pkg/computed` seam and rule-set selection

**Files:**
- Create: `pkg/computed/rules.go`, `pkg/computed/attach.go`, `pkg/computed/delegation.go`, `pkg/computed/kgw23/rules.go`, `pkg/computed/kgw24/rules.go`, `pkg/computed/rulesets/rulesets.go`, `pkg/computed/rulesets/rulesets_test.go`
- Modify: `pkg/engine/engine.go:25-55` (`Config.RulesFor`), `pkg/extension/extension.go:25-39,76-88` (`Registry.RulesFor`, `WithComputedRules`), `pkg/app/app.go` (passes `Registry.RulesFor` to the engine), `pkg/extension/extension_test.go`

**Interfaces:**
- Produces (`pkg/computed`, shared by both rule sets and every later task):
  - `type Rules interface { Version() string; Attachment(in AttachmentInput) AttachmentResult; Delegation(in DelegationInput) DelegationResult }` (design §3.4)
  - `type Selection struct { Rules Rules; Note string }` — the label of rules §2; it goes into every result's note
  - `type UnknownReason string`: `UnknownNamespaceLabels = "namespace-labels-unavailable"`, `UnknownListenerSetStatus = "listenerset-no-status"`, `UnknownSetting = "setting-unknown"`, `UnknownNotReached = "not-reached"`, `UnknownContext = "context-unknown"`, `UnknownTooLarge = "too-large"` (rules §3.3)
  - `type State string`: `StateChecked`, `StateDisagrees`, `StatePending`, `StateUnknown`, `StateRestricted` (wire values of rules §8.4)
  - `type Coverage string`: `CoverageCount = "count-agrees"`, `CoverageStatus = "status-compatible"`, `CoverageNone = "not-verified"`
  - `attach.go` (view A, rules §4.6):
    - `type Acceptance int` (`Accepted`, `Denied`, `NoStatus`); `type Resource struct { Ref objects.Ref; Parent objects.Ref; Generation int64; Listeners []objects.Listener; ListenerStatus []objects.ListenerStatus; Acceptance Acceptance }` (a Gateway is `Accepted`; `Parent` is a ListenerSet's Gateway, zero for a Gateway)
    - `type Route struct { Ref objects.Ref; Generation int64; Hostnames []string; ParentRefs []objects.ParentRef; Written []objects.WrittenRef; Parents []objects.RouteParentStatus; ExperimentalOnly bool }` — `Written` is index-aligned with `ParentRefs`; each status entry carries its own `Written` (Task 12)
    - `type AttachmentInput struct { Resources []Resource; Routes []Route; NamespaceLabels map[string]map[string]string; NamespacesOK bool; Experimental install.Setting; ControllerNames []string }`
    - `type Outcome string`: `OutcomeAccepted = "accepted"`, `OutcomeNotAllowed = "NotAllowedByListeners"`, `OutcomeNoParent = "NoMatchingParent"`, `OutcomeNoHostname = "NoMatchingListenerHostname"`
    - `type FactCode string` (`FactKind = "kind"`, `FactNamespace = "namespace"`, `FactSelectorError = "selector-error"`, `FactPort = "port"`, `FactSection = "section"`, `FactHostname = "hostname"`); `type Fact struct { Listener string; Code FactCode; Want, Got string }`
    - `type Member struct { Resource objects.Ref; Listener string; Route objects.Ref; ParentRef int; Hostnames []string; Deps []objects.Ref }`
    - `type RefOutcome struct { Route objects.Ref; ParentRef int; Resource objects.Ref; Outcome Outcome; Listeners []string; Facts []Fact; Unknown UnknownReason; Rule string; Deps []objects.Ref }` — `Rule` is `allowed-routes`, `section-and-port` or `hostname-intersection` (the stage that decided it)
    - `type ListenerCount struct { Resource objects.Ref; Listener string; Count int; Unknown UnknownReason; Error string; IsolationNote bool; Deps []objects.Ref }` — rule `listener-count`
    - `type AttachmentResult struct { Members []Member; Outcomes []RefOutcome; Counts []ListenerCount }`
  - `delegation.go`: `type DelegationInput struct{}`, `type DelegationResult struct{}` — fields arrive in Tasks 53–59 (additive); both sets return the zero value and the engine does not call `Delegation` before Task 62
  - `kgw23.New() computed.Rules` (`Version() == "kgateway-2.3"`), `kgw24.New()` (`"kgateway-2.4"`); `Attachment` returns the zero value until Task 39
  - `package rulesets`: `func Select(controllerVersion string) computed.Selection` (its own package: it imports both sets, which import `computed`)
  - `engine.Config.RulesFor func(controllerVersion string) computed.Selection` (nil → `rulesets.Select`); `extension.WithComputedRules(f func(string) computed.Selection) Option`

- [ ] **Step 1: Write the failing tests**

```go
func TestSelect(t *testing.T) { // rules §2
	for v, want := range map[string]struct{ set, note string }{
		"2.3.11": {"kgateway-2.3", ""}, "v2.3.0": {"kgateway-2.3", ""}, "2.4.7": {"kgateway-2.4", ""},
		"":       {"kgateway-2.4", "Controller version unknown; using the 2.4 rules"},
		"banana": {"kgateway-2.4", "Controller version unknown; using the 2.4 rules"},
		"2.5.0":  {"kgateway-2.4", "Rules not yet verified for 2.5"},
		"3.0.1":  {"kgateway-2.4", "Rules not yet verified for 3.0"},
		"2.2.9":  {"kgateway-2.3", "Rules not yet verified for 2.2"}, // older than the oldest set (our call)
	} {
		s := Select(v)
		if s.Rules.Version() != want.set || s.Note != want.note { t.Errorf("%q: %s %q", v, s.Rules.Version(), s.Note) }
	}
}
func TestWithComputedRulesReachesTheEngine(t *testing.T) // extension_test.go: a downstream func set by WithComputedRules is Registry.RulesFor; default is rulesets.Select
```

- [ ] **Step 2: Run** `go test ./pkg/computed/... ./pkg/extension -run 'Select|WithComputedRules'` → FAIL (undefined).
- [ ] **Step 3: Implement.** The engine passes `Resolved.KgatewayVersion()`, so a downstream distribution gets the unknown-version note.
- [ ] **Step 4: Run** `go test ./pkg/computed/... ./pkg/extension ./pkg/engine ./internal/boundary` → PASS (the boundary walk covers `pkg/computed`).
- [ ] **Step 5: Commit** `feat(computed): rules seam and rule-set selection (D-020)`.

### Task 38: Allowed routes and hostname intersection (`kgateway-2.3`)

**Files:**
- Create: `pkg/computed/kgw23/allowed.go`, `pkg/computed/kgw23/hostnames.go`, `pkg/computed/kgw23/allowed_test.go`, `pkg/computed/kgw23/hostnames_test.go`

**Interfaces:**
- Produces (package-private; copied to `kgw24` in Task 40):
  - `func allowedKinds(l objects.Listener) []objects.GroupKind` — rules §4.2 protocol defaults, intersected with `allowedRoutes.kinds` (nil group = `gateway.networking.k8s.io`)
  - `type nsVerdict struct { OK, SelectorError, UsesSelector bool; Unknown computed.UnknownReason }`; `func namespaces(l objects.Listener, resourceNS, routeNS string, labels map[string]map[string]string, labelsOK bool) nsVerdict`
  - `func intersect(listener string, route []string) (ok bool, eff []string)` — rules §4.3

- [ ] **Step 1: Write the failing tests**

```go
func TestAllowedKindsDefaults(t *testing.T) // HTTP, HTTPS → [HTTPRoute GRPCRoute]; TLS Terminate → [TLSRoute TCPRoute]; TLS Passthrough → [TLSRoute]; TCP → [TCPRoute]; UDP → []; "FOO" → [HTTPRoute]
func TestAllowedKindsIntersection(t *testing.T) // HTTPS with kinds [{nil, GRPCRoute}] → [GRPCRoute]; [{gateway.networking.k8s.io, HTTPRoute}] → [HTTPRoute]; [{example.com, HTTPRoute}] → []; [{nil, TCPRoute}] on HTTP → []
func TestNamespacesFrom(t *testing.T) {
	// Same on a Gateway listener → the Gateway's namespace; Same on a ListenerSet listener → the ListenerSet's, not the Gateway's;
	// All → any; Selector team=web: shop{team=web} OK, shop{team=retail} not, missing Namespace not; empty selector + missing Namespace → OK;
	// From Selector with a nil selector → SelectorError; Selector with labelsOK false → Unknown namespace-labels-unavailable
}
func TestIntersect(t *testing.T) {
	for _, c := range []struct { l string; r, want []string; ok bool }{
		{"", nil, nil, true},
		{"a.example.com", nil, []string{"a.example.com"}, true},
		{"", []string{"x.com"}, []string{"x.com"}, true},
		{"a.example.com", []string{"a.example.com"}, []string{"a.example.com"}, true},
		{"a.example.com", []string{"b.example.com"}, nil, false},
		{"*.example.com", []string{"a.b.example.com"}, []string{"a.b.example.com"}, true}, // any depth
		{"*.example.com", []string{"example.com"}, nil, false},
		{"a.example.com", []string{"*.example.com"}, []string{"a.example.com"}, true},
		{"*.example.com", []string{"*.a.example.com"}, []string{"*.a.example.com"}, true}, // the more specific
		{"*.a.example.com", []string{"*.example.com"}, []string{"*.a.example.com"}, true},
		{"A.example.com", []string{"a.example.com"}, nil, false},                         // case-sensitive
		{"*.example.com", []string{"a.example.com", "a.example.com", "b.example.com"}, []string{"a.example.com", "b.example.com"}, true},
	} { /* assert ok and eff */ }
}
```

- [ ] **Step 2: Run** `go test ./pkg/computed/kgw23 -run 'AllowedKinds|NamespacesFrom|Intersect'` → FAIL.
- [ ] **Step 3: Implement** with `metav1.LabelSelectorAsSelector` for `Selector`.
- [ ] **Step 4: Commit** `feat(computed): allowed routes and hostname intersection for kgateway-2.3`.

### Task 39: View A attachment and listener counts (`kgateway-2.3`)

**Files:**
- Create: `pkg/computed/kgw23/attach.go`, `pkg/computed/kgw23/attach_test.go`
- Modify: `pkg/computed/kgw23/rules.go` (`Attachment`)

**Interfaces:**
- Consumes: Task 37 types; Task 38 helpers.
- Produces: `func (Rules) Attachment(in computed.AttachmentInput) computed.AttachmentResult` — rules §4.1 per Gateway and per `Accepted` ListenerSet (the `allowed` quirk exactly as written), §4.4 counts, §4.6 dependencies, one `Fact` per listener that failed for a non-accepted outcome. Sorted: Members by (resource, listener, route, parentRef); Outcomes by (route, parentRef); Counts by resource, then listener order as written. Namespace deps are `objects.Ref{Kind: "Namespace", Name: ns}`.
- Members follow the listener order as written, as Counts do, not listener name.
- Unknown rules (rules §3.3; the precise reach is our call): a `Denied` ListenerSet gives no members, outcomes or counts; a `NoStatus` ListenerSet makes its counts and the refs naming it `listenerset-no-status`. An experimental-only route with the setting false is not a candidate; with the setting unknown its outcomes are `setting-unknown`, and so is every count of a listener it names whose allowed kinds include its kind. A listener whose namespace verdict is unknown has an unknown count and no members; a ref is unknown when any kind-matching listener of its resource has an unknown verdict.

- [ ] **Step 1: Write the failing tests** (rules §11.1, rule level; test-local builders `gw(ns, name string, ls ...objects.Listener) computed.Resource`, `route(kind, ns, name string, refs ...objects.ParentRef) computed.Route`)

```go
func TestAttachMembersPerKind(t *testing.T)        // HTTP listener holds an HTTPRoute and a GRPCRoute, not a TCPRoute; TLS Passthrough holds only a TLSRoute
func TestAttachNamespaces(t *testing.T)            // Same (Gateway and ListenerSet), All, Selector match/no match/missing; nil selector → ListenerCount.Error, no members, NotAllowedByListeners when no other listener allows
func TestAttachNamespaceLabelsUnavailable(t *testing.T) // Selector listener: count Unknown namespace-labels-unavailable; ref Unknown; a Same listener on the same Gateway keeps its known count
func TestAttachSectionAndPort(t *testing.T)        // port mismatch, sectionName mismatch → NoMatchingParent; port 0 matches any port
func TestAttachAllowedQuirk(t *testing.T) {
	// listener a: Same (allows shop); listener b: Selector team=web; namespace shop has team=retail; the ref sets sectionName b
	// → Outcome NoMatchingParent (not NotAllowedByListeners); Facts name b's namespace rule and the section
}
func TestAttachHostnamesPerListener(t *testing.T)  // one ref, listeners a "*.example.com" and b "api.example.com", route [api.example.com x.example.com] → a: both, b: [api.example.com]
func TestAttachTwoRefsOneListener(t *testing.T)    // count 2, two Members
func TestAttachGatewayRefNeverReachesListenerSet(t *testing.T) // and the reverse; a Denied ListenerSet → no Members, Outcomes or Counts for it
func TestAttachListenerSetNoStatus(t *testing.T)
func TestAttachExperimental(t *testing.T)          // TCPRoute ExperimentalOnly: setting true → member; false → no outcome, not counted; unknown → outcome and TCP listener count Unknown setting-unknown
func TestAttachDeps(t *testing.T)                  // member: route, resource, + Namespace only under Selector; count: resource, every candidate route, every Selector-evaluated Namespace
func TestAttachFacts(t *testing.T)                 // NotAllowedByListeners: kind fact {https, kind, "HTTPRoute, GRPCRoute", "TCPRoute"}; namespace fact {https, namespace, "team=web", "team=retail"}; NoMatchingListenerHostname: {https, hostname, "*.example.org", "shop.example.com"}
func TestAttachDeterministic(t *testing.T)         // shuffled input order → reflect.DeepEqual results
```

- [ ] **Step 2: Run** `go test ./pkg/computed/kgw23 -run Attach` → FAIL.
- [ ] **Step 3: Commit** `feat(computed): view A attachment and counts for kgateway-2.3`.

### Task 40: `kgateway-2.4`, the isolation note and per-version fixtures

**Files:**
- Create: `pkg/computed/kgw24/{allowed,hostnames,attach}.go` and their tests (copies of `kgw23`, package renamed), `pkg/computed/kgw24/isolation.go`, `pkg/computed/kgw24/isolation_test.go`, `pkg/computed/golden_test.go`, `internal/testutil/rulecase.go`, `pkg/computed/testdata/kgateway-2.3/*.yaml`, `pkg/computed/testdata/kgateway-2.4/*.yaml`
- Modify: `pkg/computed/kgw24/rules.go`

**Interfaces:**
- Consumes: Tasks 37–39.
- Produces:
  - `kgw24.Rules.Attachment` — `kgw23`'s plus `ListenerCount.IsolationNote` (rules §4.5): an `HTTP` listener whose hostname is empty or a wildcard covering the hostname of another `HTTP` listener on the same port; the count never changes
  - `type RuleCase struct { Provenance string; RuleOnly bool; ControllerVersion string; Settings map[string]string; NamespacesOK bool; Namespaces map[string]map[string]string; Bounds map[string]int; Objects []*unstructured.Unstructured; Expect map[string]any }` — `Bounds` overrides `computed.Bounds` fields by name (used from Task 60); `func LoadRuleCase(t *testing.T, path string) RuleCase` — one YAML file per case; `provenance` names `translator/gateway/testutils/outputs/<file>@<tag>` when the expected controller signals are copied from kgateway, else `computed-rules §<n>` plus `e2e` when the kind e2e confirms it; `ruleOnly: true` keeps a case out of the API matrix (Task 45)
  - `func TestRuleSetGoldens(t *testing.T)` — per directory: `rulesets.Select(c.ControllerVersion).Rules.Version()` equals the directory name; outputs equal `expect`; a case without `provenance` fails. Expectations are written from the source, never generated.

- [ ] **Step 1: Write the failing tests**

```go
func TestIsolationNote(t *testing.T) {
	// kgw24, port 8080 HTTP: a (no hostname), b "api.example.com", c "*.example.com"; port 8443 HTTPS d (no hostname)
	// → note on a and c, not b, never d; counts equal kgw23's for the same input. kgw23: no note anywhere.
}
```
  and `TestRuleSetGoldens` (fails: no testdata).
- [ ] **Step 2: Run** `go test ./pkg/computed/... -run 'Isolation|RuleSetGoldens'` → FAIL.
- [ ] **Step 3: Copy and implement:** `cp pkg/computed/kgw23/{allowed,hostnames,attach}{,_test}.go pkg/computed/kgw24/`, rename the package, add `isolation.go`.
- [ ] **Step 4: Write the fixtures**, one per §11.1 row in both directories, plus these with kgateway provenance (present at both tags unless noted; expected counts are the outputs' `attachedRoutes`): `listener-sets/basic.yaml`, `listener-sets/no-allowed-lis.yaml`, `listener-sets/accepted-ls-rejected-listener.yaml`, `multiple-listeners-http-routing-proxy.yaml`, `multiple-listeners-https-routing-proxy.yaml`, `grpc-routing/https-listener.yaml`, `discovery-namespace-selector/base_select_infra.yaml`, and in `kgateway-2.4` only `listener-sets/shared-port-policy-isolation.yaml@v2.4.7` (note set, count unchanged; its 2.3 copy has no note).
- [ ] **Step 5: Run** `go test ./pkg/computed/...` → PASS.
- [ ] **Step 6: Commit** `feat(computed): kgateway-2.4 rule set and per-version view A fixtures`.

### Task 41: Cross-check for view A: `listener-count` and `route-accepted`

**Files:**
- Create: `pkg/computed/check.go`, `pkg/computed/fingerprint.go`, `pkg/computed/check_test.go`, `pkg/computed/fingerprint_test.go`

**Interfaces:**
- Consumes: Tasks 37–40.
- Produces:
  - `type CheckKind string`: `KindListenerCount = "listener-count"`, `KindRouteAccepted = "route-accepted"` (Task 61 adds `KindChildEntry = "child-entry"`, `KindParentResolvedRefs = "parent-resolvedrefs"`; Task 62 `KindDelegatedNotAttached = "delegated-not-attached"`, `KindOrphan = "orphan-not-attached"`)
  - `type Identity struct { Install, RuleSet string; Kind CheckKind; Subject string }`; `func Subject(parts ...string) string` (NUL-joined; the parts of rules §9.1)
  - `type ReportKey struct { Group, Kind, Namespace, Name, SectionName string }`; `func KeyOf(w objects.WrittenRef) ReportKey` (as written, no port; rules §7)
  - `type Class string`: `ClassContradicting`, `ClassStale`, `ClassMissing`, `ClassUnexpected`, `ClassCompatible`, `ClassNotObservable`, `ClassInconclusive`, `ClassUnknown` (rules §8.1, §8.3)
  - `type CheckResult struct { ID Identity; Class Class; Verified bool; Coverage []Coverage; Fingerprint uint64; On objects.Ref; Listener string; Key ReportKey; Expected, Observed string; Deps, Contributors []objects.Ref; ParentCurrent bool; Note string; State State; Mature bool; Since time.Time }` — `On` is the object whose status disagrees (rules §9.3 step 4); `State`, `Mature` and `Since` (the window start, rules §9.3 step 4) are set by the engine (Task 42); `Contributors` and `ParentCurrent` serve `child-entry` (Task 61)
  - `func (c CheckResult) Candidate() bool` — contradicting, missing, or unexpected with `Verified`
  - `func CheckAttachment(install string, sel Selection, in AttachmentInput, r AttachmentResult) []CheckResult` — rules §8.1–§8.4 for the two kinds: refs sharing a written `ReportKey` with the same computed outcome give one `route-accepted`; a stale signal (`observedGeneration` ≠ generation) is `ClassStale`; a missing top-level entry is `ClassStale` with `Observed` `"none"` (Phase 1's KGWUI-003 owns it, rules §8.3); unknown results are `ClassUnknown`; `Accepted=False` with a non-attachment reason is compatible with `accepted`; against a computed rejection it is inconclusive, and so is any written key shared by refs with different computed outcomes (`ClassInconclusive`: state `checked`, coverage `not-verified`, note "The controller's status cannot confirm this", never a candidate, never KGWUI-012; rules §8.1); `ResolvedRefs` and `kgateway.dev/Programmed` are never read; `Note` is `sel.Note`
  - `fingerprint.go`: `func listenerCountPrint(...) uint64`, `func routeAcceptedPrint(...) uint64` — FNV-64a over a canonical encoding of exactly the rules §9.2 inputs and evidence; a non-attachment reason hashes as `"other"`

- [ ] **Step 1: Write the failing tests**

```go
func TestListenerCountAgreesAndDisagrees(t *testing.T) // §11.4 row 1: equal → Compatible [count-agrees]; off by one, current → Contradicting, On the Gateway, Expected "2", Observed "1"
func TestListenerCountStaleIsPending(t *testing.T)     // listener conditions' observedGeneration < generation → ClassStale, !Candidate()
func TestRouteAcceptedContradicts(t *testing.T)        // §11.4 row 2: computed accepted vs current Accepted=False/NoMatchingParent → Contradicting, On the route
func TestRouteAcceptedLaterStagesCompatible(t *testing.T) // §11.4 rows 4–5, top-level: 2.3 Accepted=False/RouteRuleReplaced, 2.3 Accepted=False/Conflicted, 2.4 kgateway.dev/Programmed=False, ResolvedRefs=False/BackendNotFound → Compatible [status-compatible]
func TestRefsDifferingByPortShareOneCheck(t *testing.T) // §11.1 row 7: one route-accepted; both members rejected → expects Accepted=False with an attachment reason
func TestOmittedVsExplicitNamespaceTwoChecks(t *testing.T) // §11.4: refs differing only by an omitted vs explicit namespace → two keys, two checks
func TestRejectionVsLaterStageReasonInconclusive(t *testing.T) // §11.4: computed NoMatchingParent, observed current Accepted=False/RouteRuleReplaced → ClassInconclusive, not-verified, !Candidate()
func TestSharedKeyDifferentOutcomesInconclusive(t *testing.T)   // §11.4: one ref accepted, one rejected under one written key → ClassInconclusive
func TestUnknownIsNeverACandidate(t *testing.T)
func TestNoteCarriesSelection(t *testing.T)            // "Controller version unknown; using the 2.4 rules" on every result
func TestAttachmentFingerprints(t *testing.T) {
	// listener-count changes with: listener port, protocol, hostname, allowedRoutes; ListenerSet acceptance; a candidate route's kind,
	// namespace, hostnames or parentRefs to the resource; a Selector namespace's labels; the experimental setting; attachedRoutes; listener observedGeneration.
	// Unchanged by: an annotation, resourceVersion, managedFields, an unrelated condition, lastTransitionTime, a non-Selector namespace's labels.
	// route-accepted: reason RouteRuleReplaced → Conflicted unchanged ("other"); NoMatchingParent → NotAllowedByListeners changed.
}
```

- [ ] **Step 2: Run** `go test ./pkg/computed -run 'ListenerCount|RouteAccepted|RefsDiffering|OmittedVsExplicit|Inconclusive|Unknown|NoteCarries|AttachmentFingerprints'` → FAIL.
- [ ] **Step 3: Commit** `feat(computed): view A cross-check, evidence classes and fingerprints`.

### Task 42: Persistence, maturation deadlines and KGWUI-012

**Files:**
- Create: `pkg/engine/views.go`, `pkg/engine/views_test.go`, `pkg/engine/persist.go`, `pkg/engine/persist_test.go`, `pkg/health/computed.go`, `pkg/health/computed_test.go`
- Modify: `pkg/computed/rules.go` (`Views`), `pkg/engine/engine.go` (`InstallModel.Computed`, the persistence field, `Compute`: views → persistence → health; Task 43 hands `Views` to `explore.Build`), `pkg/health/finding.go` (`CodeComputedDisagrees`; `Input.Computed` on the `Input` at `:93`), `pkg/health/checks.go:23-25` (register `computedCheck`, named `computed views`, not required, appended after `backend status`), `pkg/health/messages.go` (drafts), `internal/testutil/fixtures.go` (`GatewayWithListeners`; the default Gateway listener admits all namespaces), `pkg/objects/backend.go` (`RouteView.WrittenRefs`, so report keys reuse the cached decode), `internal/testutil/computedtest` (`NoViews`, for the frozen v1 wire worlds and the idle budget until Task 49)

**Interfaces:**
- Consumes: Tasks 36–41; `deadlines.add` (Task 6).
- Regenerate: `pkg/api/testdata/golden/health.json` (the appended `computed views` check, design §7.6; nothing else changes)
- Produces:
  - `type Views struct { RuleSet, Note string; Attachment AttachmentResult; Checks []CheckResult }` (Task 62 adds `Delegation`)
  - `func attachmentInput(res install.Resolved, owned *install.Owned, snap *cache.Snapshot, served []kinds.Served, dec objects.Decoder) computed.AttachmentInput` — owned Gateways and owned ListenerSets (acceptance from the current `Accepted`; `Resource.Parent` from `objects.ListenerSetParent(u).Ref()`, which the 2.4 isolation note needs, rules §4.5); in-scope routes of route kinds with a parentRef naming one of them, with `Written` from `objects.WrittenParentRefs`; `ExperimentalOnly` from `Served.ExperimentalOnly(res.KgatewayVersion())`; labels from `install.NamespaceLabels`
  - `InstallModel.Computed *computed.Views`
  - `const Window = 30 * time.Second`
  - `type persistence struct{...}` (map `computed.Identity → {print uint64; since time.Time}`, under `computeMu`); `func (p *persistence) step(now time.Time, checks []computed.CheckResult, graceEnd func(computed.CheckResult) time.Time) (next time.Time)` — one call per `Compute` over every install's checks; rules §9.3 steps 1–8: sets `State`, `Mature` and `Since` in place, deletes every entry not a candidate this run, returns the earliest future maturation (zero when none), which `Compute` passes to `deadlines.add(next, false)`. A non-candidate's state: stale → `pending`; compatible, not observable → `checked`; unknown → `unknown`.
  - `health.CodeComputedDisagrees = "KGWUI-012"`, slug `computed-view-disagrees`; `health.Input.Computed *computed.Views`; `computedCheck` (name "computed views", not required, always OK) emits one KGWUI-012 warning per check in state `disagrees`: resource `On`, `Evidence` = `Deps`, message "The UI's computed view disagrees with the controller", detail `disagreeDetail(c, ruleSet)` (drafts, Task 48), `Since` = `c.Since`
  - `func GatewayWithListeners(ns, name, class string, spec []objects.Listener, status ...Listener) *unstructured.Unstructured` in testutil — listener specs as given (port, protocol, hostname, TLS mode, allowedRoutes)

- [ ] **Step 1: Write the failing tests** (fake clock; worlds from testutil; rules §11.5 rows owned by 2.3)

```go
func TestDisagreementClearsBefore30s(t *testing.T)      // row 1: off by one at T, fixed at T+29s → no KGWUI-012 ever, entry gone
func TestMaturesAtScheduledDeadline(t *testing.T) {    // row 2; design §9.3
	// Run loop with a 10 min safety interval, so only the deadline can explain the run; no events after T.
	// At T+30s−1ns Current() has no KGWUI-012. awaitThenStep to T+30s: exactly one publication, KGWUI-012 warning on the Gateway,
	// detail "Listener `http`: computed 2 routes, controller reports 1. Rule set kgateway-2.4." (draft)
}
func TestUnrelatedWritesDoNotReset(t *testing.T)       // row 3: at T+10s an annotation edit, an unrelated condition write, a resourceVersion-only update, a metrics arrival → matures at T+30s
// row 4, route-accepted half: a non-attachment reason never opens a route-accepted window (rules §8.1), so TestAttachmentFingerprints (Task 41) pins it.
func TestFindingSinceIsWindowStart(t *testing.T)       // §11.5: KGWUI-012's since equals the check's window start, also after a reset
func TestListedChangeResets(t *testing.T)              // row 7: listener hostname edit at T+20s (still off) → T+50s; attachedRoutes change → reset
func TestRuleSetSwitchResets(t *testing.T)             // row 8: controller 2.3.11 → 2.4.7 at T+20s → matures at T+50s
func TestSubjectDeletedPrunes(t *testing.T)            // row 9: finding gone the same run, map empty
func TestTurnsUnknownClearsSameRun(t *testing.T)       // row 10: Namespace access lost → state unknown, finding gone that run
func TestKGWUI012IsWarning(t *testing.T)               // health: install warning, never error; the Gateway row warning (design §9.1 row state)
func TestNoFindingForUnknownStaleCompatible(t *testing.T)
func TestComputedWithoutPodsOrUIDs(t *testing.T)       // design §9.7: no Pods or Deployments → Experimental unknown, experimental-only routes setting-unknown, every other result equal; objects without uid or resourceVersion give the same Views
func TestAttachmentInputScope(t *testing.T)            // views_test.go: an out-of-scope route and another install's Gateway are absent; a ListenerSet with stale Accepted → NoStatus; TCPRoute ExperimentalOnly per controller version
```

- [ ] **Step 2: Run** `go test ./pkg/engine ./pkg/health -race -run 'Disagreement|Matures|Unrelated|SinceIsWindowStart|ListedChange|RuleSetSwitch|SubjectDeleted|TurnsUnknown|KGWUI012|NoFinding|ComputedWithout|AttachmentInput'` → FAIL.
- [ ] **Step 3: Run** `go test ./pkg/engine ./pkg/health ./pkg/api -race` → PASS after regenerating `health.json`; `TestGoldenChangesAreAdditive` passes.
- [ ] **Step 4: Commit** `feat(engine): KGWUI-012 persistence with scheduled maturation (D-020)`.

### Task 43: Explorer index for view A

**Files:**
- Modify: `pkg/explore/explore.go`, `pkg/explore/rows.go`, `pkg/explore/query.go`, `pkg/explore/explore_test.go`, `pkg/explore/query_test.go`, `pkg/engine/engine.go` (`Views` into `explore.Input`)

**Interfaces:**
- Consumes: `computed.Views` (Task 42).
- Produces:
  - `Input.Computed *computed.Views`
  - `type DepSet []int32`; `Index.Refs []objects.Ref` (interned per install, design §7.4 rule 10); `func (ix *Index) Ref(i int32) objects.Ref`
  - `type ListenerKey struct { Resource objects.Ref; Listener string }`
  - `type Member struct { Row RowID; ParentRef int; Hostnames []string; Deps DepSet }`; `Index.ByListener map[ListenerKey][]Member`
  - `type ListenerFacts struct { Count int; Known bool; Unknown computed.UnknownReason; Error string; IsolationNote bool; Check *computed.CheckResult; Deps DepSet }`; `Index.Listeners map[ListenerKey]ListenerFacts`
  - `type ListenerHit struct { Listener string; Hostnames []string; Deps DepSet }`; `type RefFacts struct { Out *computed.RefOutcome; Listeners []ListenerHit; Check *computed.CheckResult; Deps DepSet }` (`Out` and `Check` are shared with the views, Task 43 fix round 1); `RouteRow.Computed []RefFacts`; `RouteRow.UnknownDeps DepSet` (dependencies of `listener-unknown`)
  - `FlagDisagrees` (wire `disagrees`: the row's object has KGWUI-012), `FlagListenerUnknown` (wire `listener-unknown`: a status-accepted parentRef whose outcome is unknown or has no listener)
  - `GatewayRow.Members []RowID`, `GatewayRow.Disagreements int` (checks in state `disagrees` on the Gateway); the same on `ListenerSetRow`
  - `Index.RuleSet, Index.RuleNote string`
  - `Query.Listener string` — needs `Parent` of a Gateway or ListenerSet kind, else `ErrBadQuery`; a name selects `ByListener[{parent, name}]`; `_unknown` (never a listener name) selects rows whose ref to that parent is unknown; a Gateway's listener never matches ListenerSet routes; the listener index is the narrowest (design §3.8)
  - Computed predicates are caller-visible (design §7.4 rule 4a): `listener=<name>`, `listener=_unknown` and `state=listener-unknown` match a row only when `visible` holds for every ref of the deciding fact's `DepSet`, checked before `Total`, sorting and paging. `state=disagrees` is a finding and is not filtered (rule 9).

- [ ] **Step 1: Write the failing tests**

```go
func TestByListenerMembers(t *testing.T) // hostnames per listener; a route with two refs on one listener: one row on the page, two Members
func TestListenerFilter(t *testing.T)    // listener=https → members; listener=_unknown → unknown rows; no parent or an HTTPRoute parent → ErrBadQuery; a name absent on the parent → empty page
func TestComputedFlags(t *testing.T)     // disagrees from the findings map; listener-unknown only on a status-accepted ref with an unknown outcome
func TestDepSetsInterned(t *testing.T)   // equal refs share one index; Ref(i) round-trips
func TestGatewayRowComputedCounts(t *testing.T) // Members distinct; Disagreements counts disagreeing checks on the Gateway
func TestBuildDeterministicWithViews(t *testing.T)
func TestComputedPredicatesUseVisibleDeps(t *testing.T) // a member whose Selector Namespace is hidden: not matched by listener=https, not counted in Total, matched under AllowAll
```

- [ ] **Step 2: Run** `go test ./pkg/explore -run 'ByListener|ListenerFilter|ComputedFlags|DepSets|ComputedCounts|WithViews|VisibleDeps'` → FAIL.
- [ ] **Step 3: Run** `go test ./pkg/explore ./internal/boundary` → PASS.
- [ ] **Step 4: Commit** `feat(explore): view A members, listener index and computed flags`.

### Task 44: API: computed DTOs, the `listener` filter and Authorizer rules 4–7 for view A

**Files:**
- Create: `pkg/api/explain.go`, `pkg/api/computed_test.go`
- Modify: `pkg/api/explore_types.go`, `pkg/api/params.go` (`listener`), `pkg/api/lists.go`, `pkg/api/details.go`, `pkg/api/visible.go`, `pkg/api/handlers.go:110-128` (KGWUI-012 evidence)

**Interfaces:**
- Consumes: Task 43; `view.can` (Task 24).
- Produces (JSON per design §7.3):
  - `type Computed struct { By, Rules, Rule, State string; Coverage []string \`json:"coverage,omitempty"\`; Reason string \`json:"reason,omitempty"\`; Note string \`json:"note,omitempty"\` }` — `By` is always `"ui"`; `Reason` is the unknown reason; `restricted` carries no reason and no note
  - `type ListenerAttachment struct { Resource LinkedRef; Listener string; EffectiveHostnames []string \`json:"effectiveHostnames,omitempty"\`; Computed Computed }` — hostnames only in state `checked` or `disagrees`
  - `type Attachment struct { Outcome string \`json:"outcome,omitempty"\`; Listeners []ListenerAttachment \`json:"listeners"\`; Explanation string \`json:"explanation,omitempty"\`; Computed Computed }`
  - `ParentSummary.Computed *Attachment`; `type RouteAttachment struct { ParentRef objects.ParentRef; Index int; Target LinkedRef; Result Attachment }`; `RouteDetail.Attachments []RouteAttachment` (one per Gateway or ListenerSet parentRef)
  - `ListenerDTO.ComputedRoutes *int`, `.Computed *Computed`, `.IsolationNote bool \`json:"isolationNote,omitempty"\``; `GatewayListRow.Routes *int`, `.Disagreements int \`json:"disagreements,omitempty"\``; the same two on `ListenerSetRow`
  - `ruleSet` on every detail payload is `Views.RuleSet`; every `Computed.Rules` is the same value; `Computed.Note` is the check's own `CheckResult.Note` when a check exists (it includes `Views.Note`, and carries the rules §8.1 inconclusive sentence), the ref check's note for a membership, and `Views.Note` when there is no check
  - `func explain(fs []computed.Fact) string` — drafts as design §7.5 ("listener https has *.example.org; route has shop.example.com"), finalised in Task 48
  - `func memberCoverage(listener, ref computed.CheckResult, matched int) []computed.Coverage` — rules §8.4: the listener's `count-agrees`, the ref's `status-compatible`, and `not-verified` when the ref matched more than one listener; a membership is `disagrees` or `pending` when either check is
  - `func (v *view) allowDeps(ix *explore.Index, d explore.DepSet) bool` — memoised per ref index; under `AllowAll` true without calls
  - Rule 4: a hidden dependency makes the state `restricted` and drops outcome, listeners, hostnames, explanation, reason and note. Rule 5: `computedRoutes` and `routes` are omitted when any dependency is hidden, else recounted. Rule 6: `listener-unknown` is dropped and does not match a `state` filter for a caller who cannot view its `UnknownDeps`. Rule 7: KGWUI-012 whose evidence is hidden gets `evidenceRestricted`.

- [ ] **Step 1: Write the failing tests**

```go
func TestRouteListCarriesAttachment(t *testing.T)  // design §7.5 body: outcome, listeners [], explanation, rule hostname-intersection, rules kgateway-2.4, state checked, coverage [status-compatible]
func TestRouteDetailAttachments(t *testing.T)      // per listener effectiveHostnames; HTTPRoute parentRefs have no entry
func TestGatewayDetailListenerStates(t *testing.T) // checked [count-agrees]; disagrees with computedRoutes and status attachedRoutes both sent; pending; unknown (reason, no computedRoutes); isolationNote
func TestListenerParam(t *testing.T)               // 400 without parent and with an HTTPRoute parent; listener=_unknown 200
func TestMemberCoverage(t *testing.T)              // one listener → [count-agrees status-compatible]; two → + not-verified
func TestSelectorOverHiddenNamespaceIsRestricted(t *testing.T) // design §9.1 (2.3): membership, count, check restricted with no value; no listener-unknown; state=listener-unknown does not match; health and row states unchanged
func TestListenerFilterCannotProbe(t *testing.T)   // design §9.1: listener=https over a hidden Selector Namespace returns the rows and total it returns when those facts are unknown; deniedInInstall unchanged
func TestHiddenMemberRestrictsCounts(t *testing.T) // computedRoutes and Gateway row routes omitted; AllowAll: zero Authorizer calls
func TestKGWUI012EvidenceRestricted(t *testing.T)  // visible Gateway, hidden candidate route → detail replaced, evidenceRestricted true
func TestNoteOnUnknownVersion(t *testing.T)        // an install without a kgateway version: every computed.note is the unknown-version note
```

- [ ] **Step 2: Run** `go test ./pkg/api -run 'Attachment|ListenerStates|ListenerParam|MemberCoverage|HiddenNamespace|CannotProbe|HiddenMember|KGWUI012Evidence|NoteOnUnknown'` → FAIL.
- [ ] **Step 3: Run** `go test ./pkg/api -race` → PASS except goldens (Task 45).
- [ ] **Step 4: Commit** `feat(api): view A on routes, listeners and parent cards, with dependency-based authorization`.

### Task 45: Goldens for 2.3 and the matrix through the handlers

**Files:**
- Modify: `pkg/api/golden_test.go` (view A world: one listener per check state), `pkg/api/computed_test.go` (`TestViewAMatrixThroughAPI`)
- Create: `pkg/api/testdata/golden/{routes-by-listener,gateway-detail-restricted}.json`
- Regenerate: `gateway-detail.json`, `routes.json`, `gateways.json`, `listenersets.json`, `listenerset-detail.json`, `route-detail.json`

- [ ] **Step 1: Write `TestViewAMatrixThroughAPI`:** for every `pkg/computed/testdata/*/*.yaml` case without `ruleOnly`, seed the fake cluster from `LoadRuleCase` (controller Deployment with that version and settings), call the handlers, and assert each listener's `computedRoutes`, `computed.state` and coverage, each `ParentSummary.computed.outcome`, and the KGWUI-012 outcome equal the case's `expect` (design §9.1 "Computed views through the API").
- [ ] **Step 2: Add the golden cases and run** `go test ./pkg/api -run 'Golden|ViewAMatrix'` → FAIL.
- [ ] **Step 3: Regenerate** with `-update`; read every changed file: the 2.2 files change only by the 2.3 optional fields and the added listeners; `TestGoldenChangesAreAdditive` and `TestGoldenLinksAgreeWithEndpoints` pass.
- [ ] **Step 4: Run** `go test ./pkg/api` → PASS.
- [ ] **Step 5: Commit** `test(api): view A goldens and the rules matrix through the handlers`.

### Task 46: `ComputedBadge` and the 2.3 frontend types

**Files:**
- Create: `web/packages/shell/src/explore/ComputedBadge.tsx`, `src/explore/ComputedBadge.test.tsx`
- Modify: `src/api/exploreTypes.ts` (`Computed`, `Attachment`, `ListenerAttachment`, `RouteAttachment`, the listener and row fields), `src/api/types.golden.test.ts` (Task 45 fixtures), `src/index.ts` (export `ComputedBadge`), `src/copy/explorer.ts` (2.3 drafts)

**Interfaces:**
- Consumes: Task 44 DTOs.
- Produces: `export function ComputedBadge({ computed }: { computed: Computed })` — an outlined "Computed" chip with an icon; its tooltip names rule and rule set ("Computed by the UI from kgateway 2.4's attachment rules: hostname intersection", draft), keyboard-reachable and in the accessible name; then the state as icon + text per design §5.2 and §8.4: `count-agrees` and `status-compatible` use the healthy icon and say what was compared; `not-verified` a neutral icon; `pending` "Waiting for the controller"; `unknown` the reason's text; `restricted` "Hidden: depends on objects you cannot view"; `disagrees` the warning icon and "The UI's computation disagrees with the controller. The controller's status is what counts."; then `note`.

- [ ] **Step 1: Write the failing tests**

```ts
it("renders each coverage with its own text", ...);           // count-agrees and status-compatible: healthy icon; not-verified: neutral icon, never healthy
it("restricted renders the placeholder and no value", ...);
it("tooltip is reachable by keyboard and part of the accessible name", ...);
it("shows the selection note", ...);                           // "Controller version unknown; using the 2.4 rules"
it("stale data turns the badge unknown", ...);                 // fake timers past 2 × 5 s
// types.golden.test.ts: routes-by-listener and gateway-detail-restricted parse against their Spec<T>
```

- [ ] **Step 2: Run** `cd web && npx vitest run packages/shell/src/explore/ComputedBadge.test.tsx packages/shell/src/api/types.golden.test.ts` → FAIL.
- [ ] **Step 3: Commit** `feat(shell): ComputedBadge and view A types`.

### Task 47: Pages: listener counts, `?listener=`, explanations and computed listeners

**Files:**
- Modify: `web/packages/shell/src/pages/explore/gateway/ListenerTable.tsx`, `src/pages/explore/GatewayPage.tsx`, `src/pages/explore/ListenerSetPage.tsx`, `src/pages/explore/route/ParentCard.tsx`, `src/pages/explore/RoutesPage.tsx`
- Test: `src/pages/explore/details.test.tsx`, `src/pages/explore/route.test.tsx`, `src/pages/explore/lists.test.tsx`

**Interfaces:** Consumes `ComputedBadge`, `useRoutes` with `listener=`.

- [ ] **Step 1: Write the failing tests**

```ts
it("listener table shows the status count and the computed count with its check", ...);   // "Count agrees with the controller"
it("a disagreeing listener shows both numbers and the warning icon with text", ...);
it("selecting a listener sets ?listener= and filters attached routes to its members", ...); // effective hostnames per row; survives reload; back clears it
it("listener=_unknown lists the routes the UI could not place", ...);
it("an unknown-count listener shows the unknown state and its reason, never an empty table", ...);
it("failed-route rows show the computed explanation", ...);
it("a shared plain-HTTP port shows the isolation note", ...);
it("parent card lists computed listeners, each with its own effective hostnames marked Not checked by the controller", ...);
it("unknown and restricted listeners show no number", ...);
it("controller values carry no badge", ...);
it("Routes state filter offers disagrees and listener-unknown", ...);
it("ListenerSet page shows computed counts per listener", ...);
```

- [ ] **Step 2: Run** `cd web && npx vitest run packages/shell/src/pages/explore` → FAIL.
- [ ] **Step 3: Implement.** Fixed column widths from the first render; the computed column's skeleton has the final height.
- [ ] **Step 4: Commit** `feat(shell): routes per listener with effective hostnames`.

### Task 48: Wording pass for 2.3

**Files:** Modify `web/packages/shell/src/copy/explorer.ts`, `pkg/api/explain.go`, `pkg/health/messages.go`, `pkg/computed/rulesets/rulesets.go` (selection notes), the components of Tasks 46–47.

- [ ] **Step 1: List every 2.3 string** with its draft: badge label and tooltip per rule (`allowed-routes`, `section-and-port`, `hostname-intersection`, `listener-count`), the three coverage texts, `pending`, `restricted`, `disagrees`, the three 2.3 unknown reasons, each explanation fact, the isolation note ("Envoy may serve fewer hostnames on this listener"), the two selection notes, KGWUI-012 message and detail, the listener filter and "unknown" option, the `disagrees` and `listener-unknown` flag labels.
- [ ] **Step 2: Pick per string** as in Task 31 (five accurate options, a separate sub-agent picks the least machine-sounding); record the choices for the 2.3 record.
- [ ] **Step 3: Apply** and update the test expectations that quote them; regenerate goldens if a server string changed.
- [ ] **Step 4: Run** `go test ./pkg/... && cd web && npm test` → PASS.
- [ ] **Step 5: Commit** `copy: wording pass for view A`.

### Task 49: 2.3 budgets at Large

**Files:** Modify `pkg/engine/budget_test.go`, `pkg/engine/bench_test.go`, `pkg/api/budget_test.go`

- [ ] **Step 1: Extend the budget tests:** warm `Compute` ≤ 120 ms (median of 20), cold ≤ 1 s, allocation ≤ 32 MiB; heap of index, computed views and decode cache ≤ 32 MiB above the 2.1 baseline, per kind; the sustained stream of Task 32; list mixes gain `parent=<gateway>&listener=<name>`; `persistence.step` time reported.
- [ ] **Step 2: Run** `make bench | tee docs/phases/phase-2/bench/2.3.txt` → PASS, or STOP and report the miss.
- [ ] **Step 3: Commit** `test: Large-tier budgets with view A`.

### Task 50: kind e2e for 2.3: computed agrees, and a forced disagreement

**Files:** Modify `deploy/helm/kgateway-ui/values.yaml`, `deploy/helm/kgateway-ui/templates/deployment.yaml`, `deploy/helm/kgateway-ui/README.md`, `deploy/helm/chart_test.go`, `test/e2e/setup.sh`, `.github/workflows/e2e.yaml`, `test/e2e/fixtures/explorer.yaml`, `test/e2e/api_test.go`, `web/e2e/explorer.spec.ts`

- [ ] **Step 0: Chart `extraArgs`** (user, 2026-10-23): `values.yaml` gains `extraArgs: []` (a list of strings appended after the chart's own args, documented in the chart README); `TestExtraArgsAppended` renders `--set extraArgs={--poll-interval=10m}` and asserts the container args end with `--poll-interval=10m`, and with defaults asserts the args are unchanged from today. `setup.sh` accepts `UI_EXTRA_ARGS` and passes it as `extraArgs` to `helm install`; the 2.3 forced-disagreement matrix row sets `UI_EXTRA_ARGS=--poll-interval=10m`.
- [ ] **Step 1: Add fixtures** (design §9.5, 2.3): the `sectionName` quirk route (D-021 item 3's manifest from `test/e2e/fixtures/observe/` where it fits), a route with two parentRefs to one listener, a `Selector` listener over labelled namespaces, a ListenerSet with routes and a denied ListenerSet with a route, an HTTPS `*.example.com` listener with matching and non-matching routes.
- [ ] **Step 2: Write `TestComputedAgreesWithController`:** on each tag, for every listener of every owned Gateway and ListenerSet, `computedRoutes == status.attachedRoutes` and coverage holds `count-agrees`; every `ParentSummary.computed` is `checked` with `status-compatible`; the quirk route is `NoMatchingParent`; the two-ref route counts twice; health holds no KGWUI-012.
- [ ] **Step 3: Write `TestForcedDisagreementRaisesKGWUI012`:** it runs only when the UI was installed with `--poll-interval=10m` (Step 0; the test skips with a clear message otherwise, and the e2e workflow runs it in the 2.4.7 row with `UI_EXTRA_ARGS` set), so the safety timer cannot explain a run and no rollout or port-forward restart is needed. Scale the controller to 0; relabel the `Selector` namespace so the computed outcome flips; poll every second until the route's check reads `pending`; KGWUI-012 appears on the route and on the Gateway no earlier than its `since` + 30 s and within `since` + 90 s; scale back to 1; KGWUI-012 clears within 2 min. `t.Cleanup` restores the label and the replica count. The exact instant is proven by Task 42's fake-clock test, not here.
- [ ] **Step 4: Extend `explorer.spec.ts`:** open `infra/edge`, select listener `https`, the URL holds `?listener=https`, rows show effective hostnames and the badge.
- [ ] **Step 5: Run** `make e2e KGW_VERSION=v2.4.7 GWAPI_VERSION=v1.6.1` and `make e2e KGW_VERSION=v2.3.11 GWAPI_VERSION=v1.5.1` → PASS (or the CI matrix).
- [ ] **Step 6: Run the `/ui-loop` skill** on the Gateway, ListenerSet and HTTPRoute pages with computed facts: light and dark, 1280 px and 720 px, pending, unknown and disagreeing states; list the recording paths in the 2.3 record.
- [ ] **Step 7: Commit** `test(e2e): view A agrees with the controller on kgateway 2.3 and 2.4`.

### Task 51: Close sub-phase 2.3

**Files:**
- Create: `docs/phases/phase-2/phase-2.3-complete.md`, `docs/phases/phase-2/reviews/2.3-codex.md`
- Modify: `README.md` (Upgrade notes: KGWUI-012 and the computed listener columns), `docs/phase-status.md`, `docs/roadmap.md`, `docs/phases/phase-2/design.md` and `computed-rules.md` (only where the code deviates)

- [ ] **Step 1: Run the gates** of Task 10 plus `make bench`; paste outputs into the record.
- [ ] **Step 2: Request the cross-model review**

```bash
codex exec --sandbox read-only -o docs/phases/phase-2/reviews/2.3-codex.md "Review sub-phase 2.3 of the kgateway UI (diff $BASE..HEAD) against docs/phases/phase-2/computed-rules.md §1-4, §8, §9, §11.1, §11.4, §11.5 and design §3.4, §5.2, §7.3-7.4, §8.4. Focus: fidelity to kgateway's attachment rules incl. the allowed quirk and counting; evidence classes and coverage; fingerprints read exactly the listed fields; maturation at the exact instant; restricted facts never leak a value; fixture provenance. Test, e2e and bench output: <paste>. Rank findings P1-P4 with file:line." < /dev/null
```

- [ ] **Step 3: Triage** every finding; fix accepted ones with tests; rerun the gates.
- [ ] **Step 4: Write `phase-2.3-complete.md`** (Summary, Reviews, wording-pass choices, Waivers, Deliverables, benchmark table vs 2.2, ui-loop recordings, release note) and update `docs/phase-status.md` (`## Sub-phase` none, next: 2.4) and `docs/roadmap.md` (2.3 shipped).
- [ ] **Step 5: Write the README upgrade notes:** KGWUI-012 is a new warning that never turns an install red; listener tables and parent cards show computed counts and hostnames, labelled and cross-checked.
- [ ] **Step 6: Commit** `docs: close sub-phase 2.3`.

---

# Sub-phase 2.4 — Views B and C

Every 2.4 task reads `computed-rules.md` §3, §5–§9 for the behaviour it implements. View B and C code is written in `kgw23` and copied to `kgw24` in Task 59; only the report keys differ (rules §2, §7). D-024 (user, 2026-10-26) and the pre-flight rulings PF1–PF26 (`.superpowers/sdd/implementation-plan/preflight-2.4.md`) are folded into the tasks below.

### Task 52: 2.4 brief and delegation observations (D-021)

**Files:**
- Create: `docs/phases/phase-2/phase-2.4-brief.md`; Modify: `docs/phase-status.md` (sub-phase 2.4 active, base SHA)
- Modify: `docs/phases/phase-2/computed-rules.md` (§3.3, §5.6, §11.2), `docs/phases/phase-2/design.md` (§6.2, §7.3, §7.4, §9.1), `docs/decisions.md` (D-024), `.superpowers/sdd/implementation-plan/preflight-2.4.md`

**Interfaces:** Consumes D-021 items 1–9. Produces the facts later tasks pin: a leftover entry's clearing is verified on both sets for any cause of the drop or deselection, parent edit or child edit (items 1, 6 and the 2026-10-26 amendment; rules §2, §8.3; PF1); the §5.4 quirks carry no note (item 7; PF10); the §7 report keys (items 2–4), the §8.2 reason rule (item 8), one entry per child (item 9), clearing for implicit children (item 1), `dropped` vs `rejected-hostnames` (item 5). **No product code.**

- [ ] **Step 1: Map** each item to the rules section it confirms or changes, in the brief.
- [ ] **Step 2: Gate.** A contradiction of rules §5–§8 → STOP and ask the user with the proposed text. Otherwise nothing to mark: §10 already reads "Answered (D-021)". Apply the setup rulings: rules §11.2's quirk row reads "as written" (PF10); D-024 (user, 2026-10-26) amends rules §3.3, §5.6, §11.2 and design §6.2, §7.3, §7.4, §9.1.
- [ ] **Step 3: Record the sub-phase** (Lifecycle) **and write the brief:** goal, Tasks 52–70, exit criteria, file list, the decided values above.
- [ ] **Step 4: Commit** `docs: start sub-phase 2.4 (brief, D-024, plan rulings)`.

### Task 53: Wildcard setting, roots and delegation inputs

**Files:**
- Modify: `pkg/computed/delegation.go`, `pkg/install/settings.go`, `pkg/install/settings_test.go`, `pkg/engine/views.go`, `pkg/engine/views_test.go`

**Interfaces:**
- Consumes: `ReadSettings` (Task 36); `AttachmentResult` (Task 39).
- Produces:
  - `const WildcardEnv = "DELEGATION_WILDCARD_NAMESPACE"`; `Settings.WildcardNamespace Setting` — absent → `"all"`; `""` → `"all"` (rules §5.2); `valueFrom` and `envFrom` as Task 36
  - `pkg/computed/delegation.go`: `type DelegRoute struct { Ref objects.Ref; Generation int64; View *objects.HTTPRouteView; Owned, TopLevel bool }`; `type Root struct { Route objects.Ref; FromStatus, Unknown bool; Deps []objects.Ref; Keys, UncertainKeys []ReportKey }` (`Keys` and `UncertainKeys` added in Task 57's fix round: the report keys a root's delegation failures are written under, see below); `type Bounds struct { Edges, Contexts, Matchers, ContextsPerRow, ContextsPerInstall, EffectiveEdges, ContextsPerEdge int }`; `var DefaultBounds = Bounds{Edges: 50_000, Contexts: 20_000, Matchers: 200_000, ContextsPerRow: 20, ContextsPerInstall: 30_000, EffectiveEdges: 20, ContextsPerEdge: 20}` (`ContextsPerInstall` 30,000: our call, 2.4; D-022 retained heap) (rules §5.7)
  - `DelegationInput` gains `Routes []DelegRoute` (every in-scope HTTPRoute, sorted by (namespace, name)), `Unwatched map[objects.Ref]bool` (HTTPRoutes outside the scope), `Roots []Root`, `Wildcard install.Setting`, `ControllerNames []string`, `Bounds Bounds`, `ScopeUnknown bool` (the install's discovery scope is unknown: views B and C are `scope-unknown`, D-024)
  - `func delegationInput(res install.Resolved, owned *install.Owned, snap *cache.Snapshot, dec objects.Decoder, a computed.AttachmentResult) computed.DelegationInput` — roots per rules §5.1: an owned top-level HTTPRoute with an `accepted` outcome (deps: the union of its outcomes' deps, rules §5.6); all outcomes unknown and a current `Accepted=True` entry → `FromStatus` (deps: the route and the entry's target); all unknown without one → `Unknown`. `Keys`: `KeyOf(WrittenRefs[o.ParentRef])` of each accepted, known outcome, or the status entry's key for a status root; `UncertainKeys`: the keys of unknown outcomes not in `Keys`. Rejected parentRefs and other controllers' Gateways (no outcome) get no key (Task 57 fix round)

- [ ] **Step 1: Write the failing tests** `TestReadWildcard` (absent → all, `""` → all, `team-a`, valueFrom → unknown), `TestRootsFromViewA` (accepted root with deps; rejected-only → no root; unknown + current `Accepted=True` → status root; unknown without → `Unknown`), `TestDelegationInputScope` (out-of-scope HTTPRoutes only in `Unwatched`; `Owned` and `TopLevel` set; an unknown scope sets `ScopeUnknown`).
- [ ] **Step 2: Run** `go test ./pkg/install ./pkg/engine -run 'Wildcard|RootsFromViewA|DelegationInputScope'` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the same command → PASS (PF24).
- [ ] **Step 5: Commit** `feat(engine): delegation inputs, roots and the wildcard setting`.

### Task 54: View C: default fit and inherit merge

**Files:**
- Create: `pkg/computed/kgw23/fit.go`, `pkg/computed/kgw23/fit_test.go`
- Modify: `pkg/computed/delegation.go`

**Interfaces:**
- Produces:
  - `type FitClause string`: `ClausePathType = "path-type"`, `ClausePathPrefix = "path-prefix"`, `ClauseHeader = "header"`, `ClauseQuery = "query"`, `ClauseMethod = "method"`, `ClauseDefaultSlash = "default-slash"` (omitted `matches` under a parent prefix other than `/`), `ClauseNoMatches = "no-matches"` (explicit `matches: []` in default mode; our call, rules §6 names no clause)
  - `type FitFailure struct { Rule, Match int; Clause FitClause; Parent, Child string }`; `type EffectiveRule struct { Index int; Kept bool; Matches []objects.Match; Reason *FitFailure }`
  - `kgw23`: `func fits(parent, child objects.Match) (bool, computed.FitClause)`; `func merge(parent, child objects.Match) objects.Match`; `func viewC(child *objects.HTTPRouteView, parent objects.Match) (rules []computed.EffectiveRule, kept bool)` — rules §6; omitted `matches` become `PathPrefix /` (rules §3.2)

- [ ] **Step 1: Write the failing tests** (rules §11.2 rows "Default fit" and "Inherit")

```go
func TestFitClauses(t *testing.T) {
	// parent Exact /api or RegularExpression → path-type; Prefix /api vs child Prefix /apiv2 → ok (raw prefix);
	// vs child Exact /api/x → ok (child type not checked); vs /other → path-prefix;
	// parent header x=1: child without → header, child x=1 → ok, child x=1 RegularExpression → header; query params alike;
	// parent method GET: child POST or none → method, child GET → ok
}
func TestOmittedMatchesUnderPrefix(t *testing.T) // nil matches under /api → dropped, default-slash, Parent "/api"; under / → kept
func TestMerge(t *testing.T) {
	// /api + Prefix / → Prefix /api; /api + Exact /x → Exact /api/x; /api + nil path → Prefix /api (a hand-built Match: decoded matches never have a nil path, PF23); parent Exact /api + child Prefix /x → Prefix /api/x (type not checked);
	// headers a=1,b=2 + b=3,c=4 → a=1,b=2,c=4 sorted by name; query alike; method: parent GET wins, parent none keeps child POST
}
func TestExplicitEmptyMatches(t *testing.T)      // inherit → [parentMatch]; default → dropped, no-matches
func TestViewCKeepsChildIffAnyRuleKept(t *testing.T)
```

- [ ] **Step 2: Run** `go test ./pkg/computed/kgw23 -run 'Fit|Omitted|Merge|ExplicitEmpty|ViewC'` → FAIL.
- [ ] **Step 3: Implement** (`path.Join` for the merged path; a nil child path counts as Prefix `""`).
- [ ] **Step 4: Run** the same command → PASS (PF24).
- [ ] **Step 5: Commit** `feat(computed): view C fit and inherit merge`.

### Task 55: View B selection and permission

**Files:**
- Create: `pkg/computed/kgw23/select.go`, `pkg/computed/kgw23/select_test.go`
- Modify: `pkg/computed/delegation.go` (`Allows`)

**Interfaces:**
- Produces:
  - `type Allows string`: `AllowsAny = "any"`, `AllowsThis = "this"`, `AllowsOther = "other"`
  - `kgw23`: `func newSelector(routes []computed.DelegRoute, unwatched map[objects.Ref]bool, wildcard install.Setting) *selector` (indexes by namespace and by (namespace, label value), built once); `func (s *selector) fetch(ref objects.DelegationRef) (children, possible []objects.Ref, unresolved computed.Unresolved, unknown computed.UnknownReason)` — rules §5.2; children sorted by (namespace, name); `unresolved` is empty or a code, `computed.UnresolvedMissing` or `computed.UnresolvedUnwatched` (type `Unresolved` in `delegation.go`); `pkg/api/explain.go` renders "Selects no routes" and "Not watched by the controller" (PF9); with the wildcard unknown, every `label` ref is `setting-unknown` with no `children`, and `possible` lists every route carrying that label value in any namespace: uncertain points for completeness, never a selection (rules §3.3)
  - `func permits(child *objects.HTTPRouteView, childNS string, parent objects.Ref) (bool, computed.Allows)` — rules §5.3, on `child.WrittenRefs` (Task 12), where an empty group or kind never matches

- [ ] **Step 1: Write the failing tests** (rules §11.2 rows 1–2)

```go
func TestFetchModes(t *testing.T)             // name → ns/x; missing → UnresolvedMissing; out of scope → UnresolvedUnwatched; "*" and "" → every HTTPRoute in ns; label → only delegation.kgateway.dev/label == v
func TestLabelWildcardNamespace(t *testing.T) // default: ref ns "all" selects the label everywhere; set "team-a": ns team-a selects everywhere, ns all only in "all"; "" → all; valueFrom → setting-unknown, no children, every labelled route in `possible`
func TestPermits(t *testing.T)                // empty list → any; explicit group+kind+name, ns nil → child's own ns → this; nil group or kind → other; sectionName and port ignored; another parent → other
```

- [ ] **Step 2: Run** `go test ./pkg/computed/kgw23 -run 'Fetch|LabelWildcard|Permits'` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the same command → PASS (PF24).
- [ ] **Step 5: Commit** `feat(computed): view B selection and permission`.

### Task 56: Row accumulator: streaming aggregation and retention

Retention is streaming (computed-rules §5.7): each context is folded into its row as it is produced, and only samples within the caps are kept.

**Files:**
- Create: `pkg/computed/kgw23/accum.go`, `pkg/computed/kgw23/accum_test.go`
- Modify: `pkg/computed/delegation.go`

**Interfaces:**
- Consumes: `FitFailure`, `EffectiveRule` (Task 54); `Allows` (Task 55); `ReportKey` (Task 41); `Bounds` (Task 53).
- Produces (`pkg/computed/delegation.go`):
  - `type RowOutcome string`: `attached`, `partial`, `dropped`, `forbidden`, `rejected-hostnames`, `cycle`, `unresolved`, `unknown`
  - `type RowKey struct { Parent objects.Ref; Rule, Ref int; Child objects.Ref }` — a ref that selects nothing gives one row with a zero `Child` and outcome `unresolved` (computed-rules §5.5)
  - `type PathStep struct { Route objects.Ref; Match objects.Match }` — a route on the path and the match it delegated under (D-024); `type Context struct { ParentMatch objects.Match; Path []PathStep; Reporter ReportKey; Outcome RowOutcome; Unknown UnknownReason; Reason *FitFailure; Rules []EffectiveRule; Deps []objects.Ref }`
  - `type Row struct { Key RowKey; RefAsWritten objects.DelegationRef; Allows Allows; Outcome RowOutcome; Unknown UnknownReason; SomeNotComputed, Unconfirmable bool; Reason *FitFailure; Unresolved Unresolved; Contexts []Context; ContextsTruncated bool; Deps []objects.Ref; Reporters []ReportKey; ChildKey ReportKey; WritesEntry bool }` — `Deps` and `Reporters` cover every context, retained or not; `Unresolved` holds the unresolved code; there is no quirk field, since both quirks are verified (D-021 item 7, PF10)
  - `type RowMeta struct { RefAsWritten objects.DelegationRef; Allows Allows; Unresolved Unresolved; ScopeUnknown bool }`; `Row.ContextDigest uint64` — order-independent wrapping sum of a 64-bit hash of every contributing context's incoming match, path, reporter and outcome, retained or not; `Row.KeyDigests map[ReportKey]uint64` — the same streaming digest partitioned by each context's `Reporter`. Task 61's fingerprints read them for the per-context inputs of computed-rules §9.2
  - `kgw23`: `func newAccumulator(b computed.Bounds) *accumulator`; `func (a *accumulator) open(key computed.RowKey, meta computed.RowMeta)` — called once per row before any `add` (an `unresolved` zero-child row gets `open`, then one `add` per context, like any row); `func (a *accumulator) add(key computed.RowKey, c computed.Context)` — always updates the row's outcome counters, `Deps`, `Reporters`, `ContextDigest` and `KeyDigests[c.Reporter]`; keeps `c` only while the row holds fewer than `ContextsPerRow` and the install fewer than `ContextsPerInstall`, else sets `ContextsTruncated`; `func (a *accumulator) rows(incomplete func(objects.Ref) bool) []computed.Row` — final outcomes per computed-rules §5.5 (all known and equal → that outcome; `attached` and `dropped` → `partial`; `attached` with unknown → `attached` and `SomeNotComputed`; no `attached` with unknown or an incomplete child → `unknown`, never `dropped`; `forbidden` always known), in `RowKey` order

- [ ] **Step 1: Write the failing tests**

```go
func TestAggregateOutcomes(t *testing.T)               // the rules above, incl. "Dropped on the paths computed; other paths not computed"
func TestContextsPerRowCap(t *testing.T)               // 21 contexts → 20 kept, ContextsTruncated, outcome from all 21
func TestInstallContextCap(t *testing.T)               // small ContextsPerInstall: later rows keep outcomes, no contexts, truncated
func TestRetentionIsStreaming(t *testing.T)            // 10,000 adds with ContextsPerInstall 100: live retained contexts never exceed 100 at any add (test counter)
func TestDepsAndReportersSurviveRetention(t *testing.T) // a context dropped by a cap still adds its Deps and Reporter
func TestUnretainedContextChangesDigest(t *testing.T)  // change only the incoming match of the 21st (unretained) context, same outcome, deps and reporter → ContextDigest and that reporter's KeyDigests entry differ; other keys' entries do not
func TestRowMetaCarried(t *testing.T)                 // RefAsWritten, Allows and Unresolved from open appear on the emitted Row
func TestTwoRowsOfOneChildAreSeparate(t *testing.T)    // rows (0,0,C) and (1,0,C) both present with their own outcomes
func TestUnresolvedRowHasZeroChild(t *testing.T)
```

- [ ] **Step 2: Run** `go test ./pkg/computed/kgw23 -run 'Aggregate|ContextsPerRow|InstallContext|Streaming|Survive|ChangesDigest|RowMeta|TwoRows|ZeroChild'` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the same command → PASS (PF24).
- [ ] **Step 5: Commit** `feat(computed): streaming row accumulator with bounded retention`.

### Task 57: Context-aware traversal, compute bounds and the reach graph

**Files:**
- Create: `pkg/computed/kgw23/traverse.go`, `pkg/computed/kgw23/traverse_test.go`, `pkg/computed/kgw23/keys.go`, `pkg/computed/kgw23/keys_test.go` (moved from Task 59, PF3)
- Modify: `pkg/computed/delegation.go`, `pkg/computed/kgw23/rules.go` (`Delegation`)

**Interfaces:**
- Consumes: Tasks 53–56.
- Produces:
  - `type ParentSignal struct { Parent objects.Ref; Key ReportKey; Reasons []string; Deps []objects.Ref }` — `BackendNotFound`, `RefNotPermitted` in first-seen order
  - `type Edge struct { Parent, Child objects.Ref }`; `type Uncertain struct { Route objects.Ref; Rule, Ref int; Reason UnknownReason }` (a cut or unknown (parent, ref); an unknown root with `Rule == -1`; a route an uncertain selection might reach, with `Rule == -2`)
  - `DelegationResult` gains `Rows []Row; Signals []ParentSignal; Reach []Edge; Uncertain []Uncertain; Cap string` (`""`, `"edges"`, `"contexts"` or `"matchers"`, the first cap hit). No `ClearsDroppedStatus`: clearing is verified for every cause and both sets in Task 61 (PF1, D-021 amendment)
  - `kgw23.Rules.Delegation` — computed-rules §5.1 roots and contexts; §5.4 `selectTree`, `childrenFor`, `flatten` as written (both quirks, path-scoped cycle check, errors never cleared, hostnames after the fit; memoising `selectTree` per parent allowed), each row opened with its `RowMeta` (the ref as written, `Allows` from `permits`, the unresolved code, `ScopeUnknown`) before its first context, and each context handed to the accumulator as it is produced, including one context per evaluation of an unresolved ref on its zero-child row; each context's `Path` lists every route from the root with the match it delegated under (D-024); with `ScopeUnknown`, every row is `unknown`/`scope-unknown` and every root an uncertain point (D-024); the matcher cap is charged `len(child matches)` before each `viewC` call, and a call that would pass it is not made (PF15); the three compute caps of §5.7 in the fixed order (roots by (namespace, name), rules and refs by index, children by (namespace, name)), everything after a cap `unknown`/`too-large`; owned parents no root reaches get their selection and permission rows with `unknown`/`not-reached`; context deps per §5.6
  - `kgw23`: `func childKey(child *objects.HTTPRouteView, childNS string, parent objects.Ref) (key computed.ReportKey, writes bool)` per Task 59's definition (moved here, PF3): the traversal sets `Row.ChildKey`, `Row.WritesEntry` and each child `Context.Reporter` from it
  - The reach graph (computed-rules §8.5): `Reach` holds (route → permitted selected child) for **every** in-scope HTTPRoute with delegation refs, owned or not, kept, dropped or unreached, from selection and permission alone, so edges below a dropped non-owned route and below a context or matcher cut are present; it counts against the edge cap. An uncertain selection adds no edge: its parent, and each route it might select (`possible`, Task 55), become `Uncertain`.

- [ ] **Step 1: Write the failing tests** (computed-rules §11.2)

```go
func TestAncestorPrunedParentMatch(t *testing.T) // R /api → P {/api, /other} → C /other/item: P→C has one context, ParentMatch /api, C dropped, path-prefix
func TestOneChildTwoMatchesOneRule(t *testing.T)  // P {/api, /v2} → C /api/x: contexts attached (/api) and dropped (/v2); row partial
func TestOneChildTwoRules(t *testing.T)           // rule 0 drops C, rule 1 attaches: rows (0,0,C) dropped and (1,0,C) attached
func TestHostnameChild(t *testing.T)              // fits → rejected-hostnames; every rule fails the fit → dropped
func TestCycle(t *testing.T)                      // A→B→A: every child row of B's ref is cycle; B's signal RefNotPermitted
func TestDiamond(t *testing.T)                    // A→B1→C, A→B2→C: C contexts under B1 and B2 with reporters for each parent
func TestQuirks(t *testing.T)                     // second ref of a rule carries the first ref's children; the same ref key in two rules: the later overwrites; no quirk field or note (D-021 item 7, PF10)
func TestNotReached(t *testing.T)                 // owned parent off every root path: rows with child and allows, outcome unknown, not-reached
func TestThreeLevelMergedContext(t *testing.T)    // inherit child: the grandchild's ParentMatch is the child's merged match
func TestUnresolvedRef(t *testing.T)              // name of a missing route: one row, zero Child, unresolved, Note "Selects no routes"; signal BackendNotFound
func TestComputeCaps(t *testing.T)                // Bounds{Edges: 3}, {Contexts: 2}, {Matchers: 4}: Cap names the cap; later rows unknown too-large; Uncertain lists the cut (parent, ref); two runs DeepEqual
func TestContextDeps(t *testing.T)                // root eligibility deps + every route on the path
func TestReachGraphIgnoresFit(t *testing.T)       // edges below a dropped non-owned route, below an unreached parent and below a context cut are in Reach
func TestUncertainSelectionAddsNoEdges(t *testing.T) // label ref under an unknown wildcard: no Reach edge from it; the parent and every possible route are Uncertain
func TestPathCarriesDelegatingMatches(t *testing.T) // D-024: each Path step holds the route and the match it delegated under; the last equals ParentMatch
func TestScopeUnknownRowsUnknown(t *testing.T)      // D-024: ScopeUnknown → every row unknown, scope-unknown; every root Uncertain
func TestChildKeys(t *testing.T)                    // the kgw23 half of Task 59's cases (moved, PF3)
```

- [ ] **Step 2: Run** `go test ./pkg/computed/kgw23 -run 'Ancestor|OneChild|Hostname|Cycle|Diamond|Quirks|NotReached|ThreeLevel|Unresolved|ComputeCaps|ContextDeps|Reach|UncertainSelection|PathCarries|ScopeUnknown|ChildKeys'` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the same command → PASS (PF24).
- [ ] **Step 5: Commit** `feat(computed): context-aware delegation traversal, compute bounds and reach graph`.

### Task 58: Completeness, final outcomes and effective projection

**Files:**
- Create: `pkg/computed/complete.go`, `pkg/computed/complete_test.go`, `pkg/computed/kgw23/project.go`, `pkg/computed/kgw23/project_test.go`
- Modify: `pkg/computed/delegation.go`, `pkg/computed/kgw23/rules.go`, `internal/testutil/scale/scale.go` and `internal/testutil/scale/scale_test.go` (`DenseSelection`; a view-B-consistent world, PF2, PF16)

**Interfaces:**
- Consumes: Tasks 56–57.
- Produces:
  - `type Completeness struct{...}`; `func NewCompleteness(r DelegationResult) Completeness`; `func (c Completeness) Incomplete(route objects.Ref) bool` — computed-rules §8.5: closure from every `Uncertain` route over `Reach`, linear in edges; `Cap == "edges"` makes every route incomplete
  - `DelegationResult.Complete Completeness` — `Delegation` runs traversal, then `NewCompleteness`, then `accumulator.rows(Complete.Incomplete)`, then projection
  - `type EffectiveEdge struct { Parent objects.Ref; Mode string; Contexts []Context; ContextsTruncated bool; Deps []objects.Ref }` (`Mode` `"default"` or `"inherit"`); `type ChildEffective struct { Edges []EffectiveEdge; Truncated bool }`; `DelegationResult.Effective map[objects.Ref]ChildEffective` — per child, parent edges sorted by (namespace, name), at most `EffectiveEdges`; contexts per edge the union of the retained contexts of every row with that (parent, child), in traversal order, at most `ContextsPerEdge`; an edge with none kept at the install cap has `Contexts` non-nil empty and `ContextsTruncated` (computed-rules §5.7)
  - `scale.World` writes the status view B expects: each child carries one match per incoming match of each delegator, so every context attaches (today a child fits only its first delegator, `scale.go:512-518`, and the `-alt` matches fit no child, `scale.go:497-503`); under `Contradict`, child entries are removed and parent `ResolvedRefs` is `False/BackendNotFound` (PF2)
  - `func scale.DenseSelection(parents, routes int) []*unstructured.Unstructured` — `parents` wildcard parents over one namespace of `routes` HTTPRoutes, each route matching under every parent (design §9.6)

- [ ] **Step 1: Write the failing tests** (computed-rules §11.2 bound and identity rows; §11.5 reach row)

```go
func TestClosureFollowsReach(t *testing.T)           // cut at P's ref: C (dropped by the fit elsewhere) and C's child G incomplete; an unrelated route complete
func TestDroppedNonOwnedRouteInClosure(t *testing.T) // §11.5: a dropped non-owned route between an uncertain point and a negative aggregate → incomplete
func TestEdgeCapMakesEverythingIncomplete(t *testing.T)
func TestUnknownRootIsUncertain(t *testing.T)        // its descendants incomplete
func TestUnknownWildcardLabelRef(t *testing.T)       // §11.2: label ref with the setting unknown above C → C incomplete
func TestClosureIsLinear(t *testing.T)               // 50,000 edges: each visited once (test counter)
func TestCutAfterDropBeforeAttach(t *testing.T)      // row unknown, not dropped
func TestCutBeforeAnyContextAttachedComputed(t *testing.T) // row attached, SomeNotComputed ("Some paths not computed")
func TestDenseSelectionCut(t *testing.T)             // in kgw23, decoded with objects.DecodeHTTPRoute (a kgw23 test cannot import pkg/engine, PF16); DenseSelection(100, 1000) with DefaultBounds: Cap "edges" at 50,000 rows; the rest unknown too-large; two runs DeepEqual
func TestEffectiveProjection(t *testing.T)           // 21 contexts on one edge → 20 + flag; an edge from two rows, one truncated → flag; an edge at the install cap → Contexts [] + flag; 21 parent edges → 20 sorted, Truncated
func TestEffectiveEqualsTraversal(t *testing.T)      // each edge's EffectiveRule values are the ones its row's contexts used (reflect.DeepEqual)
func TestWorldChildrenFitEveryDelegator(t *testing.T) // internal/testutil/scale: every child match fits each delegator's incoming matches; Contradict flips entries and parent ResolvedRefs (PF2)
```

- [ ] **Step 2: Run** `go test ./pkg/computed/... -run 'Closure|DroppedNonOwned|EdgeCap|UnknownRoot|UnknownWildcard|Cut|Dense|EffectiveProjection|EffectiveEquals'` and `go test ./internal/testutil/scale -run WorldChildrenFit` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the same command → PASS (PF24).
- [ ] **Step 5: Commit** `feat(computed): completeness, final delegation outcomes and effective projection`.

### Task 59: Report keys per version and `kgateway-2.4` views B and C

**Files:**
- Create: `pkg/computed/kgw24/{fit,select,accum,traverse,project,keys}.go` and their tests (copies of `kgw23`; `keys.go` differs; `kgw23/keys.go` is Task 57's, PF3)
- Modify: `pkg/computed/kgw24/rules.go`

**Interfaces:**
- Produces: `func childKey(child *objects.HTTPRouteView, childNS string, parent objects.Ref) (key computed.ReportKey, writes bool)` per rule set (rules §7): `kgw23` synthesizes `{gateway.networking.k8s.io, HTTPRoute, parent.Namespace, parent.Name}` and writes only for an empty list or a spec ref with explicit group and kind, an explicit namespace equal to the parent's and no sectionName (a port is allowed); `kgw24` uses the permitting ref as written (`HTTPRouteView.WrittenRefs`, Task 12; omitted as empty) and always writes. `kgw23`'s `childKey` and its wiring land in Task 57; this task adds `kgw24`'s. There is no `ClearsDroppedStatus` (PF1).

- [ ] **Step 1: Write the failing tests** (rules §11.3; the `kgw24` half here, the `kgw23` half in Task 57)

```go
func TestChildKeys(t *testing.T) {
	// empty parentRefs → both: synthesized, writes
	// explicit full ref → 2.3 synthesized, writes; 2.4 as written
	// only a port → 2.3 synthesized, writes (the entry carries the port); 2.4 as written
	// namespace omitted, attached and hostname-rejected → 2.3 writes false; 2.4 Namespace "" as written, writes
	// sectionName set → 2.3 writes false; 2.4 as written
	// two refs differing only by port → one key
}
```

- [ ] **Step 2: Run** `go test ./pkg/computed/... -run ChildKeys` → FAIL.
- [ ] **Step 3: Copy** `fit`, `select`, `accum`, `traverse`, `project`, `keys` and their tests into `kgw24`, and change `kgw24/keys.go` to the 2.4 key.
- [ ] **Step 4: Run** `go test ./pkg/computed/...` → PASS.
- [ ] **Step 5: Commit** `feat(computed): report keys per version; kgateway-2.4 views B and C`.

### Task 60: Per-version fixtures for views B and C

**Files:**
- Create: `pkg/computed/testdata/kgateway-2.3/delegation-*.yaml`, `pkg/computed/testdata/kgateway-2.4/delegation-*.yaml`
- Modify: `pkg/computed/golden_test.go` (compare `rows`, `signals`, `effective`)

- [ ] **Step 1: Extend `TestRuleSetGoldens`** to compare delegation outputs; run → FAIL (no fixtures).
- [ ] **Step 2: Write the fixtures.** With kgateway provenance at both tags (expected outcomes: a child whose routes appear in the output's `Routes` is attached; the output's route status gives the child-entry and parent signals): `delegation/{basic,basic_invalid_hostname,basic_parentref_match,basic_parentref_mismatch,child_rule_matcher,cyclic1,cyclic2,discard_invalid_child_matches,inherit_parentref,invalid_child_valid_standalone,label_based,multiple_children,multiple_parents,multi_level_multiple_parents,nested_absolute_relative,relative_paths,unresolved_ref}.yaml`; in `kgateway-2.4` only `delegation/basic_parentref_match_same_namespace.yaml@v2.4.7`. Every other §11.2 and §11.3 row gets a case citing its rules section, marked `e2e` where design §9.5 runs it; the bound cases use `bounds:` (Task 40's field). A `scope-unknown` case pins D-024. Three of them stay in the API matrix (not `ruleOnly`): a cut after a dropping context, a row with no contexts kept at the install cap, and a context with a hidden dependency.
- [ ] **Step 3: Run** `go test ./pkg/computed/...` → PASS. A mismatch with a kgateway output is a rules bug: STOP and report it, never edit the expectation to pass.
- [ ] **Step 4: Commit** `test(computed): per-version delegation fixtures with provenance`.

### Task 61: Cross-check `child-entry` and `parent-resolvedrefs`

**Files:**
- Modify: `pkg/computed/check.go`, `pkg/computed/fingerprint.go`, `pkg/computed/check_test.go`, `pkg/computed/fingerprint_test.go`

**Interfaces:**
- Consumes: Tasks 56–59; `DelegationResult.Complete` (Task 58).
- Produces:
  - `KindChildEntry`, `KindParentResolvedRefs`
  - `func CheckDelegation(install string, sel Selection, in DelegationInput, r *DelegationResult) []CheckResult` (completeness from `r.Complete`; no first-seen lookup: clearing is verified for every cause, and `graceEnd` is the engine's, Task 62; PF1) — rules §8.1–§8.5 for both kinds: aggregation per report key over every row, context and root (a root's contexts and signals carry only its `Root.Keys`; under a `DelegationResult.UncertainKeys` entry no negative expectation is made, Task 57 fix round); a negative expectation on an incomplete subject is `ClassUnknown`, note "Not all paths computed"; a 2.3 row with `WritesEntry` false is `ClassNotObservable` (`not-verified`); a current entry nobody expects, including one under P's delegation key when P no longer selects the child (deselection, D-021 item 1), is `ClassUnexpected`: `Verified` for every cause, parent edit or child edit (observed 2026-10-26, D-021 amendment), with no first-seen lookup, a `forbidden` child's included (D-021 amendment, case 2; rules §8.3, PF1); with `in.ScopeUnknown` every check is `ClassUnknown`, `scope-unknown` (D-024); the parent's reason is compared only when complete, single, and P has no non-delegation backendRef and no `ExtensionRef` filter; `Contributors` = child, P and every route on a contributing path; `ParentCurrent` = P's entry under its reporter key is current; sets `Row.Unconfirmable` when the aggregated signal cannot confirm the row's own outcome ("Status cannot confirm this path")
  - `childEntryPrint`, `parentResolvedRefsPrint` — exactly the rules §9.2 fields; the per-context inputs (incoming match, path) come from the contributing rows' digests, so unretained contexts count: `child-entry` reads `ContextDigest`; `parent-resolvedrefs` for (P, key) reads only `KeyDigests[key]` of P's rows

- [ ] **Step 1: Write the failing tests** (rules §11.2 expectations, §11.4)

```go
func TestChildEntryAggregation(t *testing.T)          // partial row → entry expected, status-compatible; two rules → one entry expected; diamond → two checks, one per parent key
func TestAncestorPrunedExpectsNoEntry(t *testing.T)   // complete → no entry expected; a current entry left after a parent edit: Unexpected, Verified, on both sets
func TestAttachedChildMissingBackend(t *testing.T)    // §11.4 "attached child with a missing Service backend": ResolvedRefs=False/BackendNotFound → Compatible, status-compatible
func TestAttachedChildReplacedRule(t *testing.T)      // §11.4 "attached child with a replaced rule": 2.4 Programmed=False, 2.3 Accepted=False/RouteRuleReplaced → Compatible
func TestRejectedHostnamesExpectsUnsupportedValue(t *testing.T)
func TestParentWithOneUnresolvedRef(t *testing.T)     // §11.4 "parent with two delegation refs, one unresolved": expects False/BackendNotFound, reason compared
func TestTruncatedParentAggregate(t *testing.T)       // §11.4 "truncated parent aggregate": retained BackendNotFound, cut cycle, observed False/RefNotPermitted → Compatible, reason not compared
func TestParentMissingServiceBackend(t *testing.T)    // §11.4 "parent with a missing Service backend": delegation fine, observed False → Compatible
func TestComputedAttachedNoEntry(t *testing.T)        // §11.4 "computed-attached child, no entry, P current": ClassMissing, Contributors child+P+path, ParentCurrent per P's entry
func TestStaleChildEntryIsStale(t *testing.T)         // §11.4 "stale child entry contradicting"
func TestForbiddenEntryLeftNotVerified(t *testing.T)  // §11.4 "forbidden child with a current entry left"
func TestLeftEntryAnyCause(t *testing.T)             // §11.4 "matcher-dropped child (parent edit or child edit)", and the unknown first-seen row: Unexpected, Verified, candidate, both sets (PF1)
func TestDeselectedEntryLeft(t *testing.T)           // D-021 item 1: P stops selecting C after a parent edit; C\'s current entry → Unexpected, Verified (PF1)
func TestScopeUnknownChecksUnknown(t *testing.T)     // D-024: every child-entry and parent-resolvedrefs check unknown, scope-unknown
func TestNoEntryIncompleteIsUnknown(t *testing.T)     // §11.2 cut row; unknown wildcard label ref → negative checks unknown
func Test23ChildWithoutEntryNotVerified(t *testing.T) // §11.3: namespace omitted, sectionName set
func TestUnconfirmableRow(t *testing.T)               // a dropped row whose child key expects an entry from another context
func TestDelegationFingerprints(t *testing.T) {
	// child-entry changes when only an unretained context's incoming match changes (via ContextDigest).
	// Two roots reach P→C, so P has two reporter keys K1 and K2: changing an unretained context under K1 changes the parent-resolvedrefs print for (P, K1) only; (P, K2) keeps its print and its window.
	// child-entry unchanged by: the child's ResolvedRefs or Programmed, an annotation other than inherit-parent-matcher;
	// changed by: the child's matches, hostnames, parentRefs or inherit annotation, a context's incoming match or path, the ref, the wildcard setting, completeness, entry presence, Accepted/UnsupportedValue.
	// parent-resolvedrefs unchanged by: P's Accepted or Programmed; changed by: a selected child's inherit annotation, matches or labels used by a label ref, P's backendRef kinds, ExtensionRef filters.
}
```

- [ ] **Step 2: Run** `go test ./pkg/computed -run 'ChildEntry|AncestorPruned|AttachedChild|RejectedHostnames|Parent|Truncated|ComputedAttached|StaleChild|ForbiddenEntry|NoEntry|23Child|Unconfirmable|DelegationFingerprints|LeftEntry|Deselected|ScopeUnknown'` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the same command → PASS (PF24).
- [ ] **Step 5: Commit** `feat(computed): child-entry and parent-resolvedrefs cross-checks with completeness`.

### Task 62: KGWUI-011 computed and orphan triggers; delegation in the engine

**Files:**
- Modify: `pkg/computed/check.go`, `pkg/computed/check_test.go`, `pkg/computed/rules.go` (`Views.Delegation`), `pkg/engine/engine.go` (`Delegation` and its checks per install; the cap hit logged once per change), `pkg/engine/persist.go` (`graceEnd`), `pkg/engine/persist_test.go`, `pkg/health/delegation.go`, `pkg/health/delegation_test.go`, `pkg/health/messages.go`; added by PF7 and PF2: `pkg/engine/views.go` (`computeViews` runs `Delegation` and `CheckDelegation`), `pkg/health/checks.go` (the merge with the status trigger, `checks.go:328-336`), `pkg/health/computed.go` and `computed_test.go` (KGWUI-012 detail for the two new kinds), `pkg/engine/bench_test.go` (`TestScaleComputeSanity`: no delegation check is a candidate in the consistent world)

**Interfaces:**
- Consumes: Tasks 42, 56–61; `declaredFinding` (Task 16).
- Produces:
  - `KindDelegatedNotAttached` per (route, parent) and `KindOrphan` per route, from `CheckDelegation`, with `Class` `ClassTrigger = "trigger"` (our call: rules §8.3 has no class for them): `Candidate()` includes it, `persistence.step` matures it at `since + Window`, and its `State` stays `checked`, so it never makes a check pending: `persistence.visit` tracks its window but leaves `State` alone (today it sets every candidate `pending`, `pkg/engine/persist.go:59-69`; PF12). Predicates: design §6.2 and rules §9.3, exactly (computed trigger: in scope, no eligible entry, selected by the parent's `name` or `label` ref, every context `dropped` or `forbidden`, complete; orphan: empty `spec.parentRefs`, no eligible entry, selected only by `wildcard` refs of owned parents, every selecting row in every context `dropped` or `forbidden`, every selecting parent complete)
  - `Views.Delegation DelegationResult`; the engine calls `CheckDelegation(id, sel, in, &res)`; it reads first-seen times only in `graceEnd` (PF1)
  - `engine.Config.DelegationBounds computed.Bounds` (zero: `computed.DefaultBounds`) — set from a fixture's `bounds` by the API matrix (Task 65)
  - both KGWUI-011 kinds take `Since` from the persistence step (rules §9.3 step 4)
  - the install's computed summary (rules §5.7): the engine logs `DelegationResult.Cap` once per distinct value, as it logs install rejections (`engine.go:225-230`, PF21); no wire field exists for it
  - engine `graceEnd(c)` = the latest `firstSeen` of `c.Contributors` + `health.Grace` for `child-entry`, zero otherwise
  - `func computedTriggerFindings(in Input) []Finding` in `delegation.go` — one KGWUI-011 warning per mature `delegated-not-attached`, merged into the status-trigger finding of the same (route, parent) by appending the computed reason, in `conditionsCheck`; `Evidence` leaves out the routes on the contributing paths, which the detail may quote by name and delegated match (design §7.4 rule 7, **(our call, extends D-024)**); the merged finding takes the higher severity, and the `resource status` pending effect follows the status trigger's own grace clock **(inferred, PF13)**; neither trigger fires under `ScopeUnknown` (D-024); one per mature orphan, detail "No parent route in this install keeps this route, and it has no parentRefs of its own." (draft); `Evidence` = `Deps`

- [ ] **Step 1: Write the failing tests** (rules §11.4 "computed-attached child, no entry" with P current and with P stale, "matcher-dropped child (parent edit or child edit)"; §11.5 rows 4–6, 11–13, 18–24; rows cited by case name, PF11)

```go
func TestChildEntryMaturesAtLaterOfGraceAndWindow(t *testing.T) // row 11 both orders; §11.4 "computed-attached child, no entry, P current": KGWUI-012 on the child at max(since+30s, graceEnd)
func TestChildEntryWithStaleParentStaysPending(t *testing.T)    // §11.4 "the same with P stale"
func TestMatcherDroppedEntryLeftDisagrees(t *testing.T)         // §11.4 "matcher-dropped child (parent edit or child edit)", both sets: KGWUI-012 on the child after 30 s
func TestChildResolvedRefsToggleNoReset(t *testing.T)           // §11.5 row 4
func TestParentAcceptedChangeNoReset(t *testing.T)              // row 5
func TestInheritAnnotationResets(t *testing.T)                  // row 6: False/BackendNotFound → no failure, observed False/RefNotPermitted; the old window does not carry over
func TestComputedKGWUI011(t *testing.T)                         // row 12, safety interval 10 min: warning at exactly 30 s by the deadline; before it no finding and every required check unchanged
func TestOrphanWarningAfter30s(t *testing.T)                    // row 18: one per route
func TestOrphanIncompleteNoFinding(t *testing.T)                // row 19: a selecting parent cut by a bound; one in a reach closure
func TestRouteWithParentRefsNoOrphan(t *testing.T)              // row 20
func TestOrphanAttachedByAnotherParent(t *testing.T)            // row 21
func TestNewSelectingParentResetsOrphan(t *testing.T)           // row 22
func TestOrphanCycleNoFinding(t *testing.T)                     // row 23
func TestOrphanAlsoLabelSelected(t *testing.T)                  // row 24: only the per-parent finding for the label parent
func TestKGWUI011SinceIsWindowStart(t *testing.T)               // §11.5: computed and orphan findings carry their window start
func TestKGWUI011OnePerRouteParent(t *testing.T)                // status and computed triggers on one pair → one finding with the computed reason
func TestScopeUnknownNoDelegationFindings(t *testing.T) // D-024: no computed or orphan KGWUI-011 and no KGWUI-012 from views B and C
func TestMergedTriggerSeverity(t *testing.T)            // PF13: status info in grace + mature computed → warning; the resource status check follows the status grace
```

- [ ] **Step 2: Run** `go test ./pkg/computed ./pkg/engine ./pkg/health -race -run 'ChildEntryMatures|StaleParent|MatcherDropped|ToggleNoReset|ParentAccepted|InheritAnnotation|ComputedKGWUI011|Orphan|RouteWithParentRefs|NewSelecting|SinceIsWindowStart|OnePerRouteParent|ScopeUnknownNoDelegation|MergedTrigger|ScaleComputeSanity'` → FAIL.
- [ ] **Step 3: Implement** (PF24).
- [ ] **Step 4: Run** `go test ./pkg/... -race` → PASS (goldens aside).
- [ ] **Step 5: Commit** `feat(health): KGWUI-011 computed and orphan triggers; delegation checks in the engine (Q15)`.

### Task 63: Explorer index for views B and C

**Files:**
- Modify: `pkg/explore/explore.go`, `pkg/explore/rows.go`, `pkg/explore/admit.go`, `pkg/explore/query.go`, `pkg/explore/cursor.go`, `pkg/explore/explore_test.go`, `pkg/explore/query_test.go`; `pkg/explore/ancestors.go` and `ancestors_test.go` (a hidden ancestor route step keeps its ref for the API to send with `link: none`, **(our call, extends D-024)**)

**Interfaces:**
- Consumes: `Views.Delegation` (Task 62); `DepSet` (Task 43).
- Produces:
  - `type DelegRow struct { computed.Row; Deps DepSet; ContextDeps []DepSet; Check *computed.CheckResult }` — `Deps` and `ContextDeps` leave out the routes on each context's path, which never restrict (D-024); `Check` is the row's `child-entry` check, joined by (child, parent, `Row.ChildKey`) (PF14); `Index.DelegRows []DelegRow`; `Index.Delegation map[objects.Ref][]int` (parent → rows sorted by (rule, ref, namespace, name, group, kind))
  - `type EffectiveEntry struct { computed.ChildEffective; EdgeDeps []DepSet; ContextDeps [][]DepSet }`; `Index.Effective map[objects.Ref]EffectiveEntry`
  - `type RefKey struct { Parent objects.Ref; Rule, Ref int }`; `Index.RefRows map[RefKey][]int` (for `selected` and `attached` recounts)
  - `FlagDropped` (wire `dropped`: a row selected by an owned parent is `dropped`, `forbidden` or `rejected-hostnames`); `RouteRow.DroppedDeps DepSet`
  - `ReasonSelected CandidateReason = "selected"` (the package's `Reason…` names, `pkg/explore/admit.go:17-18`, PF20) with `By` the first selecting parent; a no-parent row selected by an owned parent gets a detail link
  - `Index.DelegationCap string`
  - `type DelegationQuery struct { Rule, Ref *int; Outcomes []computed.RowOutcome; Limit int; Cursor string }`; `func (ix *Index) QueryDelegation(parent objects.Ref, q DelegationQuery, visible func(objects.Ref) bool) (Page, error)` — `Page.IDs` index `DelegRows`; cursor JSON `{"s":"delegation","d":"asc","b":before,"r":[rule,ref],"t":[ns,name,group,kind]}` (design §3.6), same 1 KiB cap; an object-list cursor is `ErrBadCursor`; an `outcome` filter and `state=dropped` match a row only when `visible` holds for every ref of its `Deps`, before `Total` and paging (design §7.4 rule 4a); a zero-`Child` row is never dropped for child visibility

- [ ] **Step 1: Write the failing tests**

```go
func TestDelegationRowsOrder(t *testing.T)
func TestDelegationCursorBetweenRowsOfOneChild(t *testing.T) // design §9.1: a page ends between (0,0,C) and (1,0,C); next returns the rest; prev returns the same set
func TestDelegationCursorAnchorGone(t *testing.T)           // anchor row deleted in the next Model: next starts where its tuple sorts
func TestDelegationFilters(t *testing.T)                    // rule, ref, outcome (OR); an object-list cursor → ErrBadCursor
func TestFlagDropped(t *testing.T)
func TestSelectedAdmission(t *testing.T)                    // selected no-parent route: "selected", By, detail; unselected no-parent: yaml
func TestRefRows(t *testing.T)
func TestOutcomeFilterUsesVisibleDeps(t *testing.T) // a dropped row under a root behind a hidden Selector Namespace: not matched by outcome=dropped, not in Total; listed unfiltered as restricted. Under a hidden ancestor route it matches (D-024)
func TestFlags(t *testing.T)                        // existing: now accepts `dropped` (explore_test.go:343-345, PF19)
func TestRowCheckJoin(t *testing.T)                 // PF14: each row's Check is its child-entry check by ChildKey
func TestUnresolvedRowSurvivesAuthorization(t *testing.T)
```

- [ ] **Step 2: Run** `go test ./pkg/explore -run 'Delegation|FlagDropped|SelectedAdmission|RefRows|OutcomeFilter|UnresolvedRow|Flags|RowCheckJoin'` → FAIL.
- [ ] **Step 3: Implement** (PF24).
- [ ] **Step 4: Run** `go test ./pkg/explore ./internal/boundary` → PASS.
- [ ] **Step 5: Commit** `feat(explore): delegation rows, effective edges and the selected candidate`.

### Task 64: API: the delegation endpoint, effective matchers, and authorization of contexts and evidence

**Files:**
- Create: `pkg/api/delegation_test.go`
- Modify: `pkg/api/explore_types.go`, `pkg/api/details.go` (endpoint, `effective`, ref counts), `pkg/api/params.go` (`rule`, `ref`, `outcome`), `pkg/api/visible.go`, `pkg/api/link.go` (`selected`), `pkg/api/handlers.go` (route registration), `pkg/api/explain.go` (outcome, fit-clause and unresolved text), `pkg/api/types.go` (`/meta` filters) (PF8)

**Interfaces:**
- Consumes: Task 63; `allowDeps` (Task 44).
- Produces (JSON per design §7.3):
  - `type DelegationRow struct { Rule, Ref int; RefAsWritten DelegationRefDTO; Child *LinkedRef \`json:"child,omitempty"\`; Allows string; Outcome, Reason string \`json:",omitempty"\`; Contexts []DelegationContext; ContextsTruncated bool \`json:"contextsTruncated,omitempty"\`; Computed Computed; Observed DelegationObserved }` — `Child` absent only on an `unresolved` row with no selection
  - `type DelegationContext struct { Restricted bool \`json:"restricted,omitempty"\`; ParentMatch *MatchDTO; Path []PathStepDTO; Outcome, Reason string }`; `type PathStepDTO struct { Ref LinkedRef; Match MatchDTO }`: each ancestor's name and the match it delegated under; a hidden ancestor has `link: none` and nothing else (D-024); `type DelegationObserved struct { Entry *ParentStatus; ParentCondition *Condition }`
  - `type EffectiveDTO struct { Parent LinkedRef; Mode string; ContextsTruncated bool \`json:"contextsTruncated,omitempty"\`; Contexts []EffectiveContext \`json:"contexts"\`; Computed Computed }` — `contexts: []` is sent, never omitted; coverage always `["not-verified"]`
  - `type EffectiveContext struct { Restricted bool; ParentMatch *MatchDTO; Rules []EffectiveRuleDTO }`; `type EffectiveRuleDTO struct { Index int; Kept bool; Matches []MatchDTO; Reason string }`
  - `RouteDetail.Effective []EffectiveDTO`, `.EffectiveTruncated bool`; `BackendRefDTO.Delegation` gains `Selected, Attached *int` and `Computed *Computed` (its state merges the ref's rows worst-first, D-023 decision 2; a row's `computed` is its `child-entry` check plus the row's note; **inferred**, PF14); `Candidate.Reason` `"selected"`
  - `GET /api/v1/installs/{id}/routes/{group}/{kind}/{ns}/{name}/delegation` → `Page[DelegationRow]`; `rule` and `ref` integers, `outcome` repeatable; `sort` → 400; `/meta` `filters` gains `"delegation": ["rule", "ref", "outcome"]` (PF8)
  - Rules (design §7.4): a row whose child is hidden is dropped (3); routes on a context's path never restrict it: a hidden ancestor is sent by name (`link: none`) with the match it delegated under, and nothing else (4b, D-024); a context with another hidden dependency (a root's Gateway, ListenerSet or `Selector` Namespace, or a status root's target) is `{restricted: true}`, and a row or effective edge aggregating one is `restricted` with no outcome or reason (4); `selected` and `attached` count visible children only (5); `dropped` follows its deps per caller (6); KGWUI-011 and KGWUI-012 with hidden evidence get `evidenceRestricted`, except when the only hidden dependencies are ancestor routes, whose names and delegated matches the detail may quote (7); an up-chain step for a hidden ancestor route is `{ref (link none), matches}` from the child's view B contexts, a hidden Gateway or ListenerSet step stays `{restricted: true}` (3) **(our call, extends D-024)**

- [ ] **Step 1: Write the failing tests**

```go
func TestDelegationEndpoint(t *testing.T)     // every outcome incl. partial and unresolved; contexts with parentMatch and path; observed entry and parent condition; an unconfirmable row's note
func TestDelegationParams(t *testing.T)       // non-integer rule/ref, unknown outcome, sort → 400; cursor paging; unknown route → 404
func TestEffectiveOnRoutePage(t *testing.T)   // both modes; equals the traversal (§11.2); coverage not-verified; contexts [] with contextsTruncated at the install cap; effectiveTruncated at 21 edges
func TestHiddenRootNamespace(t *testing.T)    // §11.2: root behind a Selector listener, Namespace denied → every descendant context, row and effective edge restricted
func TestStatusRootTargetDenied(t *testing.T) // §11.2
func TestHiddenAncestorEvidence(t *testing.T) // design §9.1 (2.4): KGWUI-012 on a visible child whose only hidden evidence is its parent route → detail kept, quoting the parent's name and delegated match, no evidenceRestricted; with a root behind a hidden Selector Namespace → evidenceRestricted, no detail (**(our call, extends D-024)**)
func TestUpChainHiddenAncestorRoute(t *testing.T) // a hidden ancestor route step: name, link none, delegated matches; a hidden Gateway step stays restricted (**(our call, extends D-024)**)
func TestDelegationCountsRecounted(t *testing.T)
func TestDroppedRouteWithGatewayParentShown(t *testing.T) // §11.5 row 20: Delegation row with its reason; no KGWUI-011
func TestHealthUnchangedUnderDelegationRestriction(t *testing.T)
func TestOutcomeFilterCannotProbe(t *testing.T) // design §9.1: outcome=dropped under a root behind a hidden Selector Namespace and state=dropped give the rows and total of the unknown case
func TestHiddenAncestorShowsInheritedMatch(t *testing.T) // D-024: rows, contexts and effective edges under a hidden ancestor route show its name (link none) and delegating match, nothing else; total counts visible children
func TestMetaAdvertisesDelegation(t *testing.T)       // PF8: filters.delegation
```

- [ ] **Step 2: Run** `go test ./pkg/api -run 'Delegation|Effective|HiddenRoot|StatusRoot|HiddenAncestor|DroppedRoute|CannotProbe|InheritedMatch|MetaAdvertises|UpChainHidden'` → FAIL.
- [ ] **Step 3: Implement** (PF24).
- [ ] **Step 4: Run** `go test ./pkg/api -race` → PASS except goldens (Task 65).
- [ ] **Step 5: Commit** `feat(api): delegation endpoint and effective matchers with dependency-based authorization`.

### Task 65: Goldens for 2.4 and the matrix through the handlers

**Files:**
- Create: `pkg/api/testdata/golden/{route-delegation,route-detail-delegation-restricted,health-evidence-restricted}.json` (the second: a hidden ancestor shown by name and match only, in the Delegation rows, effective matchers and the up-chain, and a root behind a hidden `Selector` Namespace restricted, D-024; the last: KGWUI-011 and KGWUI-012 with `evidenceRestricted` from a hidden root Namespace, and one quoting a hidden parent route without it, design §7.6)
- Regenerate: `route-detail.json` (effective in both modes, an edge with `contextsTruncated`, an edge with `contexts: []` at the install cap), `routes.json` (`dropped` flag), `route-detail-candidate.json` (`selected`), `meta.json` and `bare-meta.json` (`filters.delegation`, PF8)
- Modify: `pkg/api/golden_test.go`, `pkg/api/computed_test.go` (`TestDelegationMatrixThroughAPI`), `pkg/api/api_test.go` (`consistentWithViews`, the Q12 worlds), `pkg/api/details_test.go` (`delegationWorld`)

- [ ] **Step 0: Make the 2.2 API worlds consistent with views B and C, and widen `consistentWithViews` to all three views** (Task 62 review). Task 62 limited the guard to view A, because these worlds keep reported children under parents that never select or keep them, and those leftovers are correct D-021 disagreements:
  - `delegationWorld` (`details_test.go`): `shop/loop` has no rules, so mid's wildcard drops it; give it a match under `/api`. `shop/mid` delegates to the unresolved `ghost` and to label `team`, which selects nothing; it must report `ResolvedRefs=False/BackendNotFound` (D-021 item 8), or drop those refs.
  - `TestQ12CountsBeforeAndAfter` (`api_test.go`): `shop/api` and `infra-b/pb` have no delegation refs, yet `leaving/child`, `two`, `moved`, `both` and `stale` report under them. Give each parent refs to its children, and give each child a fitting match.
  - `TestEveryEligibleEntryIsJudged` and `TestKGWUI004DetailNamesParentKind` (`api_test.go`): `shop/api` must delegate to `shop/child`, and the child needs a fitting match.
  - Then widen `consistentWithViews` back to every check kind. Run `go test ./pkg/api` → PASS before Step 1. Any golden these fixture edits move is regenerated in Step 3.
- [ ] **Step 1: Write `TestDelegationMatrixThroughAPI`** as Task 45's, over the delegation fixtures without `ruleOnly`, including the three bound cases run with their `bounds` through `engine.Config.DelegationBounds`: row outcomes, `contextsTruncated`, `effectiveTruncated`, `contexts: []`, `computed.state` and coverage, child-entry and parent checks, KGWUI-011 and KGWUI-012 outcomes.
- [ ] **Step 2: Add the golden cases and run** `go test ./pkg/api -run 'Golden|DelegationMatrix'` → FAIL.
- [ ] **Step 3: Regenerate** with `-update`; read every changed file; additive and link-agreement tests pass.
- [ ] **Step 4: Commit** `test(api): delegation goldens and the rules matrix through the handlers`.

### Task 66: Frontend: Delegation tab, effective matches and ref counts

**Files:**
- Create: `web/packages/shell/src/pages/explore/route/DelegationTab.tsx`, `src/pages/explore/route/EffectiveMatches.tsx`, `src/pages/explore/delegation.test.tsx`
- Modify: `src/api/exploreTypes.ts`, `src/api/exploreHooks.ts` (`useDelegation(id: string, ref: LinkedRef, query: URLSearchParams)`), `src/api/types.golden.test.ts`, `src/pages/explore/RoutePage.tsx`, `src/pages/explore/route/RuleList.tsx`, `src/explore/CandidateBanner.tsx`, `src/pages/explore/route/UpChain.tsx` (a hidden ancestor route step as name and matches, no link, **(our call, extends D-024)**), `src/api/hooks.ts` and `src/pages/explore/RoutesPage.tsx` (the tab and the `dropped` option only when `/meta` advertises `filters.delegation`), `src/copy/explorer.ts` (drafts), `src/index.ts` (`useDelegation` export, design §8.2) (PF8)

- [ ] **Step 1: Write the failing tests**

```ts
it("Delegation tab lists rows per ref, paged, each with outcome and the controller's signal", ...);
it("every outcome has its icon and text", ...);    // partial "Attached under some parent matches"; dropped with the failing match; unresolved "Selects no routes"; unknown with its reason
it("contexts expand with aria-expanded; a restricted context shows the placeholder", ...);
it("contextsTruncated says more paths exist", ...);
it("a row the status cannot confirm says so", ...);
it("outcome, rule and ref filters stay page-local and only ?tab=delegation is in the URL; a 400 cursor reloads page one", ...); // design §8.1, PF26
it("effective matches per parent path in both modes, marked with the not-verified coverage copy", ...); // assert copy constants, PF22
it("a hidden ancestor shows its name without a link, and its delegating match", ...); // D-024
it("the up-chain shows a hidden ancestor route by name without a link; a hidden Gateway stays 'A parent you cannot view'", ...); // extends D-024
it("the tab and the dropped filter appear only when /meta advertises filters.delegation", ...); // PF8
it("an edge with no kept contexts never looks complete", ...);
it("a delegation ref shows selected and attached counts with a ComputedBadge", ...);
it("a selected route's candidate banner names its parent", ...);
// types.golden.test.ts: the Task 65 fixtures parse against their Spec<T>
```

- [ ] **Step 2: Run** `cd web && npx vitest run packages/shell/src/pages/explore packages/shell/src/api` → FAIL.
- [ ] **Step 3: Implement.** Each ref is one paged list fetched when expanded (design §5.3).
- [ ] **Step 4: Run** `cd web && npm test && npm run typecheck && npm run lint` → PASS (PF24).
- [ ] **Step 5: Commit** `feat(shell): Delegation tab and effective matches`.

### Task 67: Wording pass for 2.4

**Files:** Modify `web/packages/shell/src/copy/explorer.ts`, `pkg/health/messages.go`, `pkg/api/explain.go`, the components of Task 66, `pkg/computed/check.go` (check notes), the goldens the copy touches; Create `docs/phases/phase-2/phase-2.4-wording.md` (PF9).

- [ ] **Step 1: List every 2.4 string** with its draft: the eight outcomes and their reasons, each fit clause, the three remaining unknown reasons, "Some paths not computed", "Dropped on the paths computed; other paths not computed", "Not all paths computed", "Status cannot confirm this path", "Selects no routes" and "Not watched by the controller" (rendered in `explain.go` from the unresolved codes), allows labels, mode labels, truncation notes, the `dropped` flag label, KGWUI-011 computed and orphan details, the "selected" banner reason.
- [ ] **Step 1b: KGWUI-012 detail:** a `child-entry` or `parent-resolvedrefs` detail quotes the ancestor's delegated match next to its name (design §7.4 rule 7; carried from Task 64).
- [ ] **Step 2: Pick per string** as in Task 31; record the choices in `phase-2.4-wording.md`.
- [ ] **Step 3: Apply;** update quoting tests and goldens.
- [ ] **Step 4: Run** `go test ./pkg/... && cd web && npm test` → PASS.
- [ ] **Step 5: Commit** `copy: wording pass for delegation`.

### Task 68: 2.4 budgets and the dense-selection benchmark

**Files:** Modify `pkg/engine/budget_test.go`, `pkg/engine/bench_test.go`, `pkg/api/budget_test.go`, `.github/workflows/bench.yaml` (the path filter gains `pkg/computed/**`, PF17); Create `docs/phases/phase-2/bench/2.4.txt`, `2.4-ci.txt`, `2.4-stream-10m.txt` (PF6)

- [ ] **Step 1: Extend the budget tests:** warm `Compute` ≤ 150 ms and cold ≤ 1 s; allocation per recompute reported, not gated (D-022); heap with retained contexts ≤ 32 MiB above `bench/2.2-baseline-heap.txt`, per kind; the sustained stream with engine CPU ≤ 15 % and memory from the OS ≤ 256 MiB; each in the consistent world and the worst case (`Tier.Contradicting`, which also contradicts child entries and parent `ResolvedRefs` after Task 58); the consistent world raises no KGWUI-012 and holds no window (PF6). The list mix "delegation of the widest parent" moves from 2.2's `routes?parent=<HTTPRoute>` stand-in (`pkg/api/budget_test.go:168`) to the delegation endpoint (p95 < 50 ms, p99 < 100 ms, 20 clients p95 < 100 ms); route detail on the deepest child p95 < 50 ms; route detail on the widest delegating parent (the most view B rows, a restricted caller included) p95 < 50 ms (Task 64 fix round 3).
- [ ] **Step 2: Add `BenchmarkDenseSelection/{large,extreme}`** over `DenseSelection(20, 500)` and `DenseSelection(100, 1000)`; report time, allocation and the cap hit; two runs give the same `Cap` and the same last computed row.
- [ ] **Step 3: Run** `make bench | tee docs/phases/phase-2/bench/2.4.txt` → PASS, or STOP and report the miss. Save the CI run as `bench/2.4-ci.txt` and the 10-minute stream as `bench/2.4-stream-10m.txt`.
- [ ] **Step 4: Commit** `test: Large-tier budgets with views B and C; dense selection`.

### Task 69: kind e2e for 2.4

**Files:** Modify `test/e2e/fixtures/explorer.yaml`, `test/e2e/api_test.go`, `web/e2e/explorer.spec.ts`; Create `docs/phases/phase-2/phase-2.4-evidence.md` (PF25)

- [ ] **Step 1: Add fixtures** (design §9.5, 2.4): the ancestor-pruned parent match; one child attached through one parent match and dropped through another; an attached child with a missing Service backend; a child whose parentRef sets only a port. Reuse D-021's manifests from `test/e2e/fixtures/observe/` where they fit.
- [ ] **Step 2: Write `TestDelegationAgreesWithController`:** on each tag, every `child-entry` and `parent-resolvedrefs` check is `checked` under its rule set; health holds no KGWUI-012; the backend-failure child is `status-compatible`; the pruned child is `dropped` with no entry expected; the two-match child is `partial` with an entry; on 2.3 the port-only child has an entry carrying its port. The 2.2 fixture's delegation tree (forbidden, header-mismatch, cycle and hostname children) gets compatible or `not-verified` checks too, and `TestComputedAgreesWithController` and `TestForcedDisagreementRaisesKGWUI012` stay green; a mismatch is a rules bug: STOP (PF18).
- [ ] **Step 3: Extend `explorer.spec.ts`:** open the parent route's Delegation tab, expand a `partial` row, open the child, see its effective matches.
- [ ] **Step 4: Run** both `make e2e` pairs → PASS.
- [ ] **Step 5: Run the `/ui-loop` skill** on the Delegation tab and effective matches: both themes, 1280 px and 720 px, restricted, truncated and unknown states; list the recording paths in `phase-2.4-evidence.md`.
- [ ] **Step 6: Commit** `test(e2e): delegation agrees with the controller on kgateway 2.3 and 2.4`.

### Task 70: Close sub-phase 2.4

**Files:**
- Create: `docs/phases/phase-2/phase-2.4-complete.md`, `docs/phases/phase-2/reviews/2.4-codex.md`
- Modify: `README.md` (Upgrade notes: KGWUI-011 computed and orphan triggers, the Delegation tab), `docs/phase-status.md`, `docs/roadmap.md`, `docs/phases/phase-2/design.md` and `computed-rules.md` (only where the code deviates)

- [ ] **Step 1: Run the gates** of Task 10 plus `make bench`; paste outputs into the record.
- [ ] **Step 2: Request the cross-model review**

```bash
codex exec --sandbox read-only -o docs/phases/phase-2/reviews/2.4-codex.md "Review sub-phase 2.4 of the kgateway UI (diff $BASE..HEAD) against docs/phases/phase-2/computed-rules.md §3, §5-9, §11.2-11.5 and design §5, §6.2, §7.1-7.4. Focus: traversal fidelity incl. ancestor context, both quirks and path-scoped cycles; bounds never turn a missing path into a negative; completeness and the orphan rule; report keys per version; aggregation per report key; fingerprints; restricted contexts and D-024's inherited match (a hidden ancestor sends only its name and delegating match); delegation cursors; fixture provenance. Test, e2e and bench output: <paste>. Rank findings P1-P4 with file:line." < /dev/null
```

- [ ] **Step 3: Triage** every finding; fix accepted ones with tests; rerun the gates.
- [ ] **Step 4: Write `phase-2.4-complete.md`** (Summary, Reviews, wording-pass choices, Waivers, Deliverables, benchmark table vs 2.3 with the dense-selection numbers, ui-loop recordings, release note) and update `docs/phase-status.md` (`## Sub-phase` none; next: the Phase 2 cross-model review and exit gates) and `docs/roadmap.md` (2.4 shipped).
- [ ] **Step 5: Write the README upgrade notes:** KGWUI-011 now also fires when a parent's ref drops a route on every path, and for orphans; the Delegation tab and effective matches are computed, labelled and cross-checked.
- [ ] **Step 6: Commit** `docs: close sub-phase 2.4`.

---

## Rules §11 coverage

Each matrix row has a rule-level test in the named task, a fixture in Task 40 or 60, and, unless the fixture is `ruleOnly`, an API assertion in Task 45 or 65.

| Rules section | Rows | Tasks |
|---|---|---|
| §11.1 | kinds, namespaces, `NoMatchingParent` quirk, hostnames, two refs, Gateway vs ListenerSet refs, no status, experimental | 38, 39; e2e rows 50 |
| §11.1 | Namespace access unavailable | 39, 44 |
| §11.1 | refs differing only by port | 41 |
| §11.4 | inconclusive cases (non-attachment reason, shared key), omitted vs explicit namespace | 41 |
| §11.1 | 2.4 isolation note | 40 |
| §11.2 | selection modes, wildcard setting, permission | 55 |
| §11.2 | ancestor-pruned, two matches, two rules, hostname child, cycle, diamond, quirks, not reached, three levels | 57; checks 61; e2e rows 69 |
| §11.2 | two rows of one child | 56, 63 |
| §11.2 | context caps | 56 |
| §11.2 | dense selection, effective edges, cuts, effective equals traversal | 58; API 64, 65; benchmark 68 |
| §11.2 | label ref with an unknown wildcard | 55, 57, 58, 61 |
| §11.2 | root behind a hidden Namespace, status root denied, hidden ancestor route (D-024) | 64 |
| §11.2 | unknown discovery scope (D-024) | 53, 57, 61, 62 |
| §11.2 | default fit, inherit | 54 |
| §11.3 | all rows | 59; port-only e2e 69 |
| §11.4 | listener count, `NoMatchingParent`, TCP conflict, top-level replaced rule | 41, 42 |
| §11.4 | child and parent rows; effective matcher | 61, 62; 64; e2e 69 |
| §11.5 | rows 1–3, 4 (route-accepted, via 41's fingerprint test), 7–10 | 42 |
| §11.5 | rows 4 (child), 5, 6, 11–13, 18–24 | 62; row 20's Delegation row 64 |
| §11.5 | finding `since` is the window start | 42, 62 |
| §11.5 | dropped non-owned route in the reach closure | 57, 58 |
| §11.5 | unresolved row with no child | 56, 57, 63 |
| §11.5 | explicit `matches: []` (`no-matches`) | 54 |
| §11.5 | unparsable boolean setting | 36 |
