Cross-model: YES (author: Claude Opus 5.5, reviewer: Codex, command: codex exec --sandbox read-only)
Scope: design (re-review of the metrics clean break and the user's answers)
Diff: ce43017..e2440f3 (docs/phases/phase-5, docs/phase-status.md)
Timing: on time

Found five **P2 design/plan gaps**; no P1 findings.

1. **P2 — The documented E8 sequence cannot compile.** [design.md:254](/Users/ddoyle/Development/claude/snellius-claude/docs/phases/phase-5/design.md:254) calls `engine.New(cfg.Engine).Compute(snap)`. Current `Compute()` takes no arguments ([engine.go:275](/Users/ddoyle/Development/claude/snellius-claude/pkg/engine/engine.go:275)) and reads `Config.Snapshots` ([engine.go:285](/Users/ddoyle/Development/claude/snellius-claude/pkg/engine/engine.go:285)). No signature change is planned. Document assigning a snapshot provider to `cfg.Engine.Snapshots`, then calling `Compute()`; ideally use the same fixed snapshot used for discovery and scraping.

2. **P2 — The one-shot wait omits required input readiness.** [design.md:251](/Users/ddoyle/Development/claude/snellius-claude/docs/phases/phase-5/design.md:251) proceeds immediately after `Settled()`. Existing setup documentation explicitly requires also waiting for optional watches and list-only kinds to become OK or report an error ([setup.go:11](/Users/ddoyle/Development/claude/snellius-claude/pkg/setup/setup.go:11)). Following the new sequence can produce timing-dependent health results. Preserve that wait contract in the E8 instructions and fixture.

3. **P2 — Non-check readers lack the proposed collected-snapshot plumbing.** [design.md:230](/Users/ddoyle/Development/claude/snellius-claude/docs/phases/phase-5/design.md:230) requires `Impl.Version` to read the metrics entry, but its planned signature accepts only `install.Install` ([implementation-plan.md:47](/Users/ddoyle/Development/claude/snellius-claude/docs/phases/phase-5/implementation-plan.md:47)). The adjacent API reader requires a “published collected snapshot,” yet neither `Model` nor `Published` currently retains one ([engine.go:112](/Users/ddoyle/Development/claude/snellius-claude/pkg/engine/engine.go:112), [loop.go:41](/Users/ddoyle/Development/claude/snellius-claude/pkg/engine/loop.go:41)). Task 6 needs explicit snapshot arguments and publication storage so these readers use the same evidence as the checks, without independently reading the live store.

4. **P2 — Publication-relative expiry changes scrape-age semantics.** [design.md:246](/Users/ddoyle/Development/claude/snellius-claude/docs/phases/phase-5/design.md:246) specifies expiry *after publication*, replacing `Result.Stale`. Current staleness is measured from each result’s `CollectedAt` ([scrape.go:139](/Users/ddoyle/Development/claude/snellius-claude/pkg/ctrlmetrics/scrape.go:139)); `collect.Store.Publish` instead sets expiry from the store’s current time ([collect.go:87](/Users/ddoyle/Development/claude/snellius-claude/pkg/collect/collect.go:87)). With multiple installs, earlier scrapes can already be old when `CollectOnce` finishes, then receive another full freshness window. Define expiry from scrape timestamps and cover staggered results.

5. **P2 — Dropping expired data conflicts with unchanged API detail.** [design.md:220](/Users/ddoyle/Development/claude/snellius-claude/docs/phases/phase-5/design.md:220) replaces staleness with store expiry, while [implementation-plan.md:138](/Users/ddoyle/Development/claude/snellius-claude/docs/phases/phase-5/implementation-plan.md:138) requires unchanged gateway data-plane detail. Expired collector snapshots discard `Data` ([collect.go:157](/Users/ddoyle/Development/claude/snellius-claude/pkg/collect/collect.go:157)); today the API serves the last complete metrics snapshot without checking its age ([details.go:331](/Users/ddoyle/Development/claude/snellius-claude/pkg/api/details.go:331)). Specify how that last snapshot survives expiry, or explicitly approve and test the changed API behavior.

Rows **8, 12 and 17** are consistently reflected: assumed scope without verification, no computed view without rules, and class-only installs off by default. The metrics API has a stated home, the kgwctl compile job is informational, and program/issue updates are scheduled after the move merges.

Docs-only review of `ce43017..HEAD`. No files edited; no tests run.

## Triage
| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | E8 sequence calls `Compute(snap)` | reviewer | accept | Verified `engine.go:275`. Sequence sets `cfg.Engine.Snapshots` to the same snapshot, then `Compute()` (design §7.3) |
| 2 | One-shot wait omits optional watches and list-only kinds | reviewer | accept | Verified `setup.go:11-17`. The full wait is step 1 and part of the fixture test |
| 3 | `Impl.Version` and the API reader lack the collected snapshot | reviewer | accept | `Impl.Version(install, collect.Snapshot)`; new `Model.Collected` read by the API (design §7.2, Task 6) |
| 4 | Store expiry changes scrape-age semantics | reviewer | accept | Verified `collect.go:87`. No store expiry; staleness per entry from `CollectedAt` via `Result.Stale`; `Results` carries `MaxAge` |
| 5 | Expired values drop data the API serves | reviewer | accept | Verified `collect.go:157`. Fixed by 4: the last snapshot stays and the API keeps showing it; tested in Task 6 Step 2 |
