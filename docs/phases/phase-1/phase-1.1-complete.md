# Phase 1.1 — Backend (Complete)

**Completed:** 2026-10-19
**Range:** 1a848bb..41650c0 (branch `duncandoyle/phase1Backend`)

## Summary
The Go backend of the Phase 1 UI is done: one binary that discovers kgateway installs, watches their Gateway API and kgateway resources through a shared informer cache, scrapes controller metrics, evaluates health findings KGWUI-001…009 without false greens (D-006, D-014), and serves the v1 JSON API behind an `Authorizer` seam (D-013). Downstream builds extend it through `pkg/extension` (kinds, detectors, scope provider, scope verifier, checks, Authorizer). A smoke test on kind with kgateway v2.4.7 passed every expectation: a broken backend gives KGWUI-002, a route in an unwatched namespace gives KGWUI-004 and is not counted, metrics and scope verification are OK, no Secrets are served, and the state recovers when the broken route is removed.

## Reviews
- Per-task spec + quality reviews for Tasks 1–11 (subagents; Tasks 7, 8, 9 and 11 needed fix rounds, Task 9 two).
- Whole-branch review (most capable model): 6 Important + 12 Minor findings; all Important fixed or ruled, must-fix minors fixed in one wave; scoped re-review clean with 3 Minor items parked (below).
- Cross-model review of this diff happens at the phase review (planned sub-phase).

## Waivers
none

## Deliverables
- `cmd/kgateway-ui`, `internal/webui` (embedded SPA placeholder), `internal/testutil`.
- `pkg/{kinds,objects,cache,install,ctrlmetrics,health,scopeverify,overview,engine,api,extension,app,version}`.
- Fixtures: real controller `/metrics` captures with an xDS reject, v2.4.7 and v2.3.11 (`pkg/ctrlmetrics/testdata`).
- D-012 and D-015 recorded; design.md and D-013/D-014 updated to match the code.
- D-015 (user request after the final review): the scope falls back to the controller Deployment's Pod template when no controller Pod exists.
- Direct dependencies: `k8s.io/{api,apimachinery,client-go}` v0.36.2, `prometheus/common` v0.72.1, `prometheus/client_model` v0.6.3. golangci-lint v2.15.0 via `go run`.
- `go test -race ./...` passes in about 16 s; `make lint` 0 issues.

## Notes for later phases
- The frontend (1.2) consumes `/api/v1` as built; `InstallSummary.computedAt`, `Finding.restricted` and the health `install` id are the freshness and identity hooks.
- Served kinds are re-resolved every 60 s; a change asks for a restart (no live re-watching).
- Compute decodes each object view several times per cycle; fine at 5k routes (<1 s), consider caching by (uid, resourceVersion) beyond 20k.

## Deferred / open
- Live re-watching of CRDs added or removed after startup: https://github.com/DuncanDoyle/kgateway-ui/issues/1 (the UI asks for a restart meanwhile).
- Parked: an invalid hand-built downstream selector is silently skipped (`pkg/install/scope.go`); validate in `install.Resolve`.
- Parked: the served-kinds check reports fresh during a discovery outage; track the last successful refresh.
- Parked: `kinds.Diff` could be unexported; `Change` JSON tags unused; `Monitor.Run` panics on a non-positive interval.
- Deferred minors from task reviews are listed below for the phase review to triage.
- Extension seam: finding-code prefix registry joins the seam-milestone carry-over.

## Rulings made during execution
- - Ruling: C1 (Task 8, plan L3161) parse_test calls pointer method on non-addressable TextParser — assign to a variable first, as parse.go does — cost if wrong: none (compile fix).
- - Ruling: D1 (Task 5, L1387/L6623) testutil.Clients/ClientsWith seed via dynamic fake constructor, which pluralises Gateway to "gatewaies" — seed each object with Tracker().Create(servedGVR, obj, ns) — cost if wrong: fixtures never reach the cache (would be caught by tests).
- - Ruling: D2 (Task 2, L283) .gitignore negation order — put `internal/webui/dist/*` before `!internal/webui/dist/index.html`; verify with git check-ignore — cost if wrong: fresh checkout fails go:embed.
- - Ruling: D3 (Task 11, L7038/L7053) app tests must not rely on scheduling order: inject a stub Scraper in tests; in production, trigger one collection as soon as the engine first discovers installs (not only on the 10 s tick) — cost if wrong: first metrics up to 10 s late (cosmetic).
- - Ruling: D4 (Task 9, L6956/L4231) scope-mismatch persistence = 2 × metrics interval (passed in, not a 20 s constant), strict `>` per design §4.4 — cost if wrong: KGWUI-009 one tick early/late.
- - Ruling: G1 (Task 9) add doc comments to Name()/Run() of unexported check types — cheap, satisfies the doc rule — cost: none.
- - Ruling: R2 (Task 10, L5309) TestUnservedKindHasNoTileAndNoFinding must assert no finding — cost: none.
- - Ruling: R1 (Tasks 6–10) duplicated snapshot-by-kind builder: the first task that needs it adds one helper to internal/testutil; later tasks reuse it — cost: none.
- - Ruling: R4 (Task 9, L4598) findings sorted deterministically (severity desc, code, group/kind/ns/name, parent) — the UI list must not reorder between polls — cost: none.
- - Ruling: R13 (Task 2/11) golangci-lint run via `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.x pinned` from the Makefile with a minimal .golangci.yml (Task 2); every implementer runs gofmt — cost if wrong: lint findings surface late in CI.
- - Ruling: pinning (Task 8, L3304) pin prometheus/common to an explicit version; every Go task ends with `go mod tidy` — cost: none.
- - Ruling: Task 1 commits the two metrics captures with D-012 (testdata only) instead of leaving them untracked across tasks — an untracked file can be lost by git clean between dispatches — cost: none.
- Task 5: Ruling: Task 11's ClientsWith must reuse the unexported testutil clients() helper (D1 applies there too) — cost if wrong: extension-kind fixtures never reach the cache.
- Task 5: Ruling: routine 410 Gone/Expired stays a failure (unknown during the relist window) — the cache really missed events; document as a D-014 known limit at phase close — cost if wrong: brief unknown flickers on busy clusters
- Task 7: Ruling: R1 helper is testutil.ObjectsByGVR(t, objs...) map[GVR][]*Unstructured (not a Snapshot builder — testutil cannot import pkg/cache without a cycle); Tasks 9/10 build &cache.Snapshot{Objects: testutil.ObjectsByGVR(...)} — cost: none.
- Task 7: Ruling: an install whose scope is unknown does not compete for a Gateway that an install with a known scope contains; it competes only when no known-scope install claims it (≥2 candidates there → ambiguous as before). Refines design §4.3; design text updated at sub-phase close — cost if wrong: while B's scope is unknown, a Gateway B really owns shows under A (B is unknown anyway).
- Task 8: Ruling: plan-mandated scrape bounds are inadequate — accept review Important 1–3: cap each /metrics body at 8 MiB (hitting the cap is an error, never a truncated parse); per-pod timeout of min(every/2, 5 s); scrape installs concurrently so one hung install cannot starve others; httptest-based tests for hang, oversize, non-200; also error on empty PodIP and reject every <= 0 — cost if wrong: an 8 MiB cap could reject a legitimately huge /metrics (KGWUI-008, never a false green).
- Task 9: Ruling: StatusSummary synthetic ancestors (main only) count by controllerName — they name no Gateway — cost: none.
- Task 9: Ruling: a current required condition with Status=Unknown (non-Pending reason) counts as not yet processed (KGWUI-003), not KGWUI-002 error — §5.2 maps only False to 002 — cost: none.
- Task 10: Ruling: a visible finding naming a denied object in its detail is acceptable — D-013 already shows the identity of restricted resources; only message/evidence of findings ON denied objects are removed — cost: none.
- Task 11: Ruling: rename seam-test fixture names (acme-edge/edge-controller/WafPolicy → acme-controller/WidgetPolicy etc.) — "edge" + WAF reads like a known product line — cost: none.
- - Ruling: F1 served kinds frozen at startup — re-run kinds.Resolve periodically (every 60 s); when the served set differs from the watched set, a required KGWUI-008 per install ("served kinds changed: <added/removed>; restart the UI to watch them"); no live re-watching in Phase 1 — cost if wrong: operators must restart after CRD changes (visible, not silent).
- - Ruling: F2 absent reject family indistinguishable from wrong endpoint — xDS check is unavailable (required, KGWUI-008) when the install owns ≥1 Gateway and the scrape has no resources_managed family — cost: none.
- - Ruling: F3 Scope.matchers — Contains compiles Selectors lazily when matchers is nil; seam test with a downstream ScopeProvider returning Scope{Mode: ScopeSelectors, Selectors: ...} — cost: none.
- - Ruling: F4 proxy matching ignores the detector seam — Install carries the detector's ProxySelector; proxyFor matches with it plus the standard gateway.networking.k8s.io/gateway-name label — cost: none.
- - Ruling: F5 no finding-code prefix registry — Phase 1 keeps it a documented convention (WithChecks doc + design §1/§9 text); a registry joins the seam-milestone carry-over — cost if wrong: a downstream could emit KGWUI-* codes until the milestone.
- - Ruling: F6 KGWUI-006 references the Gateway (better link target); fix design §2/§7 text — cost: none.
- - Ruling: must-fix minors in the same wave: optional condition Unknown → no finding (only False); restrict policy severity to freshness-checked condition types; Discover backfills Install.Deployment; WithKinds/Resolve dedupe by GroupKind; unknown severities rank last and are rejected/flagged; remove "Review Focus 9"/"(A1)" comments; bound every channel receive in tests with select+timeout and drain buffered watchers; t.Parallel per subtest in TestWatchFailuresAfterSyncAtTheAPI — cost: none.
- Final review: Ruling: unrecognised downstream severity → warning with the original named in detail — cost if wrong: a downstream "critical" shows as warning until it uses a known severity.
- Final review: Ruling: duplicate kind registration replaces in place (downstream may override an upstream descriptor) — cost: none.
- Final review: Ruling: a preferred-version change counts as a served-kind change (restart prompt) — cost: one extra restart prompt on CRD upgrades.
- Final review: parked — invalid hand-built downstream selector is silently skipped (scope.go:65-72) — Ruling: downstream bug only; validate in install.Resolve → ScopeUnknown at the phase review/stabilisation — cost if wrong: a buggy downstream provider under-reports until fixed.
- Final review: parked — served-kinds check reports fresh during a discovery outage (checks.go:86) — Ruling: removed CRDs also surface via cacheCheck; add last-success time later — cost: small.
- Final review: parked — kinds.Diff could be unexported; Change json tags unused; Monitor.Run panics on every <= 0 (only app guards it) — Ruling: polish for the phase review — cost: none.

## Deferred minor findings (for the phase review)
- Task 1: minor (deferred): decisions.md D-012 closing sentence detached from bullet list; elided node_id adds little
- Task 2: minor (deferred): redundant web/**/node_modules/ gitignore line (plan-mandated); minimal golangci default linter set
- Task 3: minor (deferred): Upstream completeness test checks only presence+category, not group/resource/namespaced/version order; no test for a NotFound group-version (unserved) or non-NotFound discovery error; go directive became go 1.26.0 (forced by k8s deps)
- Task 4: minor (deferred): test comments cite internal review rounds (objects_test.go:315,329,364); view() swallows FromUnstructured errors (fails safe, untested); routeView misnamed; no tests for PolicyAncestors/GatewayListeners/GatewayAddresses/Conditions/GatewayClassName/ControllerName/ListenerSetParent (ListenerSetParent without parentRef returns empty name)
- Task 5: minor (deferred, triage first at final review): watch.Filter goroutine leak on Stop + a late watch.Error from an old stream can leave a healthy kind unavailable until the next Watch call (cache.go:164)
- Task 5: minor (deferred, triage first): unbounded `<-watchers` wait (cache_test.go:267, plan-mandated)
- Task 5: minor (deferred, triage first): no test for watch-list end bookmark on a stream opened before a later failure (openedAt guard untested)
- Task 5: minor (deferred): watch-list fallback on servers rejecting sendInitialEvents records a failure → brief unknown at startup/relist; Pods/Deployments/Namespaces unsorted in Snapshot; Start offers no wait for informer exit
- Task 6: minor (deferred, triage first): Scope.matchers unexported — a downstream ScopeProvider building Scope{Mode: ScopeSelectors, Selectors: ...} matches nothing (scope.go:235); compile lazily or provide a constructor
- Task 6: minor (deferred): Detect/splitImage/metrics-port override lack table tests
- Task 7: minor (deferred): lowercase comment starts after prefix removal (assign_test.go:134,155,173,199); no XListenerSet-parent, ambiguous-ListenerSet-parent or two-install Unwatched tests; duplicate missing parentRef yields two Unattached entries (Task 9 dedupes); route via out-of-scope ListenerSet is silently dropped (design follow-up)
- Task 8: minor (deferred): Run never prunes results of vanished installs; Result maps shared with Get callers (document read-only); 200 with no recognised families → Task 9 must check ManagedPresent
- Task 8: minor (deferred): Run writes podTimeout without a lock (safe while Run is the only Collect caller); TestRunConcurrentWithGet does not discriminate concurrent vs serial install fan-out
- Task 9: minor (deferred): no health-level test that an unavailable optional verifier leaves an install healthy; deleted-Gateway policy shows nowhere (record as known limit at close); KGWUI-006 warning vs design table error (brief-mandated)
- Task 9: minor (deferred, triage first): an optional condition with Status=Unknown is still reported as KGWUI-002 error (false red); likely should yield no finding
- Task 9: minor (deferred): unserved Gateway-like ancestor kind (e.g. XListenerSet when only ListenerSet is served) counts by controllerName; owned-ancestor rule duplicated in assign.go and checks.go from two inputs (engine must pass the same served list); TestAccessFindingNamesGroup selects by index; a policy whose only status is from another install's Gateway but which also targets ours (no status yet) is not reported here
- Task 10: minor (deferred, triage first): two unbounded `<-watchers` receives in api_test.go:640,660 (and a vacuous step in the deployments case due to two buffered watches); 47 s watch-failure test could use t.Parallel per subtest
- Task 10: minor (deferred): Scope.Watched/Total namespace counts and install identities bypass the Authorizer (per-user design question); scale test under -race should skip the timing assertion; overlong doc line handlers.go:1021
- Task 11: minor (deferred): App.cfg and Persist() exist for tests; swallowed InClusterConfig error; --kubeconfig help text
- Task 11: minor (deferred): serve cancels informer/engine/collector goroutines but cannot wait for their exit
