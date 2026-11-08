Cross-model: YES (author: Claude Opus 5.5, reviewer: Codex, command: codex exec --sandbox read-only)
Scope: phase 5
Diff: 169712c..dbaaeaf
Timing: on time

Found five issues in the whole-phase seams:

1. **P1 — Route freshness still uses kgateway’s condition types.** [pkg/explore/rows.go:304](/Users/ddoyle/Development/claude/snellius-claude/pkg/explore/rows.go:304) calls `health.RoutePending`, which still uses the fixed kgateway optional-condition list ([checks.go:573](/Users/ddoyle/Development/claude/snellius-claude/pkg/health/checks.go:573)). The later flag logic uses the install’s conditions. Consequently, a class-only route with current Accepted/ResolvedRefs but stale `kgateway.dev/Programmed` becomes pending in the explorer despite that condition being irrelevant to its health checks. Conversely, a pack’s own stale optional condition escapes this freshness gate. Thread the per-install condition set through freshness evaluation and test both cases.

2. **P2 — Invalid or duplicate pack collectors disappear silently.** [pkg/app/app.go:290](/Users/ddoyle/Development/claude/snellius-claude/pkg/app/app.go:290) registers pack collectors through a second registry without checking its `Err()`. `WithCollectors` rejects invalid names and drops duplicates. Meanwhile, [setup.go:136](/Users/ddoyle/Development/claude/snellius-claude/pkg/setup/setup.go:136) checks collisions only between pack collectors and legacy collectors. Two packs sharing a collector name therefore pass `Build`, but only the first collector starts. Validate all pack collector names and collisions before returning the app.

3. **P2 — Existing downstream shells lose delegation without a documented migration.** [Shell.tsx:61](/Users/ddoyle/Development/claude/snellius-claude/web/packages/shell/src/shell/Shell.tsx:61) defaults to no web packs, while the formerly built-in Delegation tab now exists only through `kgatewayWebPack`. The unchanged [downstream example](/Users/ddoyle/Development/claude/snellius-claude/examples/downstream/web/acme.tsx:28) and README frontend example register no pack. Their kgateway routes consequently lose the tab, and `?tab=delegation` falls back to Overview. The default app registers it correctly, but downstream compatibility is incomplete. Update the example and document the frontend migration.

4. **P3 — The smoke test can pass before the default server discovers anything.** [test/compat/compat.go:145](/Users/ddoyle/Development/claude/snellius-claude/test/compat/compat.go:145) accepts an empty install response as proof that the default build does not claim the controller. It never checks that server’s readiness or synchronization. A default build incorrectly running with class-only discovery enabled can therefore pass during startup; `Settle` stops at that first success. Require a synced default server before asserting absence, and cover an initially empty response that later claims the controller.

5. **P3 — Compatibility assertions omit the later web activation contracts.** [test/compat/compat.go:124](/Users/ddoyle/Development/claude/snellius-claude/test/compat/compat.go:124) checks kinds but not `meta.packs`; its feature blacklist omits `processes`. It also accepts omitted lists, although the SPA interprets omission as an older server. The Playwright checks would not catch a leaked Processes block or navigation entry. Assert present, empty `packs` and `features` arrays and verify the unsupported process surfaces stay absent.

The recorded `Version`/`Reported` split, Task 7 pull-forward, always-sent features, and `/meta` packs are reflected in the implementation. README and the external kgwctl drafts cover both deliberate Go breaks. Frozen v1 goldens are untouched. The 14-file boundary allowlist remains honest **as an import check**; it cannot catch semantic leakage such as finding 1. The positioning grep found no prohibited target or private-repository names.

No default kgateway regression was identified by static inspection. No files were edited or tests run; gate and benchmark results remain author-reported, with the local core-image build unverified.

## Triage

| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | P1: route freshness uses kgateway's condition types | Codex | Accepted, fixed | `f658d5e`: the explorer judges freshness with the install's `ConditionTypes` (`health.RoutePendingFor`). Test `TestRouteFreshnessUsesInstallConditions`: class-only route with a stale `kgateway.dev/Programmed` accepted, a pack's own stale optional condition pending, kgateway unchanged; goldens unchanged. The 001 member lookup (`rows.go`) keeps kgateway's types: it is kgateway-pack only |
| 2 | P2: invalid or shared pack collector names vanish | Codex | Accepted, fixed | `bb35bc3`: `WithPack` checks each collector name and cross-pack collisions; `Registry.Err` fails `Build`. Test `TestBuildRejectsBadPackCollectors` (shared, invalid, a kgateway collector's name) |
| 3 | P2: downstream shells lose delegation without a migration | Codex | Accepted, fixed | `0bed633`: `examples/downstream/web` and the README frontend example register `kgatewayWebPack`; v0.6.0 notes gain a frontend section. Test: Delegation tab and `?tab=delegation` on a kgateway route in the example |
| 4 | P3: smoke test passes before the default build discovers anything | Codex | Accepted, fixed | `1f7723e`: the default build's install list counts only once its `/meta` says synced. Test `TestCheckWaitsForASyncedDefaultBuild` (empty first, claims later). The private workflow already waits for `/readyz`; its README updated |
| 5 | P3: compat omits packs, absent lists and process surfaces | Codex | Accepted, fixed | `1f7723e`: `packs` and `features` must be present and empty; Playwright checks the Processes block and nav entry are absent (locators confirmed against a kgateway install). Mutation cases added to `TestCheckCatchesEachBreak` |
