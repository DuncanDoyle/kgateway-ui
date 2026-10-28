# Phase 2.4 — Views B and C: computed route delegation (Complete)

**Completed:** 2026-10-28
**Range:** 86c70a4..f399c89 (branch `duncandoyle/viewsBC`)

## Summary
The UI computes route delegation, shows why a child is kept or dropped, and cross-checks the result with the controller's status:
- **Delegation tab:** a parent HTTPRoute's page lists, per rule and delegation ref, the routes the ref selects, whether each permits the parent, and the outcome: attached, partly attached, dropped, not permitted, rejected for hostnames, cycle, not resolved or unknown. Each row opens its paths from a Gateway or ListenerSet, with each ancestor's match. Each row shows the child's status entry and the parent's condition.
- **Effective matches (view C):** a child route shows the matches it really serves under each parent, after the fit or the inherit merge. A dropped rule names the clause that failed.
- **Counts and flags:** the rules list shows selected and attached counts per ref; while a ref is hidden, only visible ones. The routes list flags a child that is selected but not attached (`dropped`).
- **Cross-check:** `child-entry` and `parent-resolvedrefs` compare views B and C with route status. A disagreement that lasts 30 s raises KGWUI-012, a warning that never turns an install red.
- **KGWUI-011:** gains computed triggers (a parent drops or does not permit the child) and an orphan trigger (no parent keeps a route with no parentRefs), as warnings.
- **Visibility:** an unknown discovery scope makes views B and C unknown (`scope-unknown`). A child the caller can see shows its hidden ancestors by name and delegated match only (D-024).
- **Bounds:** delegation work has install-wide caps. Past them a parent or ref reads "too large to compute". The edge cap is configurable (`--delegation-edge-cap`, chart `delegation.edgeCap`, default 10,000). The binary sets its Go memory limit to 90 % of the pod limit (D-025).

## Sub-phases (phase record only)
n/a

## Reviews
- **Task reviews:** Tasks 53–69 and the allocation perf pass each had a spec and quality review; Task 52 (docs only) was reviewed by the controller. Fix rounds were confirmed by scoped re-reviews or by the controller for small diffs. The perf pass was checked with a differential test: 99 outputs byte-identical.
- **Cross-model (Codex):** `reviews/2.4-codex.md`, three rounds, 11 findings: 9 accepted, 0 rejected, 2 deferred.
  1. First round (86c70a4..a316748): 3 findings. #1 (P1, a hidden non-ancestor uses the shared budget) accepted by the user and documented as design §7.4 rule 4d. #2 (P1, quadratic path copies) and #3 (P3, weak e2e asserts) fixed.
  2. Re-review (a316748..47a7cdd): #1 accepted by decision, #3 resolved, #2 still open through dependency aggregates and matcher DTOs. Both fixed, plus two vectors found during the fix (merge size, match text in findings).
  3. Final round (47a7cdd..bd07a2f, capacity review, retried once after the OpenAI filter refused it): 4 findings. F2 (hub fan-in, a regression from [`7343d12`](https://github.com/DuncanDoyle/kgateway-ui/commit/7343d12)) fixed. F1 (inherited path bytes) and F3 (failure-reason text) deferred as known limits with a P3 carry-over. F4 repeats #1.

## Wording pass
`phase-2.4-wording.md` holds the final copy (S188–S264; S129–S131 deferred from 2.3), picked per string from five options by a separate subagent. Rulings:
1. The `allows` labels (S235–S237) are dropped; no component renders them.
2. The open path list is named "Paths (`<n>`)" (S264), separate from the toggle text.
3. Fit clauses carry no backticks, since the Why cell renders text as is.
4. S258 and S259 name the parent without backticks, to match the shipped S98 they merge with.

Six strings changed for accuracy, among them S189 ("Selected but not attached") and S220 ("No paths shown. This list is not complete."). Rule and ref numbers on screen count from 1.

## Waivers
none

## Deliverables
- **Backend:**
  - `pkg/computed`: delegation inputs and roots, completeness, checks (`child-entry`, `parent-resolvedrefs`), fingerprints, interned dependency sets;
  - `kgw23` and `kgw24`: fit, select, accumulate, traverse, project and report keys, with per-version delegation fixtures;
  - `pkg/install/settings.go`: `DELEGATION_WILDCARD_NAMESPACE`;
  - `pkg/engine` delegation views; `pkg/health` KGWUI-011 computed and orphan triggers, KGWUI-012 for delegation;
  - `pkg/explore` delegation rows and ancestors; `pkg/api` delegation endpoint, effective matchers, page unit budget, new goldens;
  - `pkg/app/memlimit.go`: `app.Run` applies 90 % of `KGWUI_MEMORY_LIMIT` unless `GOMEMLIMIT` is set.
- **Chart:** `delegation.edgeCap`; `KGWUI_MEMORY_LIMIT` from `resources.limits.memory`.
- **SPA:** `DelegationTab`, `EffectiveMatches`, up-chain and candidate banner changes; `copy/explorer.ts`.
- **Tests:**
  - delegation goldens (Go and TS) and the rules matrix through the handlers;
  - Large-tier budgets with views B and C, `BenchmarkDenseSelection`, the memory gates below;
  - kind e2e `TestDelegationAgreesWithController` (20 pinned check identities) and the Delegation tab in `explorer.spec.ts`.
- **Docs:**
  - D-024, the D-021 amendment, D-025 and its amendment;
  - `computed-rules.md` §2, §3.3, §5, §8, §11; design §6.2, §7.3, §7.4 (rules 4b–4d), §9.1, §9.6;
  - `phase-2.4-wording.md`, `phase-2.4-evidence.md`, `documentation-notes.md`.
- **Gates on [`f399c89`](https://github.com/DuncanDoyle/kgateway-ui/commit/f399c89):**
  - Go: `go vet` (plain, `bench`, `e2e`) clean, `go test ./... -race` passes in 24 packages, golangci-lint reports 0 issues.
  - Web: Vitest 527/527, typecheck, lint and build pass.
  - kind e2e on v2.4.7: Go 5/5, Playwright 19/19, forced KGWUI-012 at since + 30.3 s.
  - kind e2e on v2.3.11: Go 4 plus 1 skipped by design, Playwright 19/19.
  - On both tags all 16 `child-entry` and 4 `parent-resolvedrefs` checks agree with the controller.

### Benchmarks (Large tier)
Laptop numbers (Apple M5 Max, GOMAXPROCS=4) from `bench/2.4.txt` and `bench/2.4-stream-10m.txt`; the 2.3 column is CI (EPYC 7763), so compare with care. CI record `bench/2.4-ci.txt` (PR #10, 4-vCPU EPYC 9V74): warm recompute median 104 ms (large) and 125 ms (large-contradicting), budget ≤ 150 ms; with unchanged views input 60 and 82 ms; stream CPU 10.1 % and 11.8 %, budget ≤ 15 %. The stream test edits annotations only, so its recomputes reuse the views memo; a stream of spec edits costs about the full recompute (tracked below).

| Measure | 2.3 (CI) | 2.4 consistent | 2.4 worst case | Limit |
|---|---|---|---|---|
| Warm recompute (median) | 64 ms | 35 ms | 44 ms | ≤ 150 ms |
| Allocation per recompute | 19.2 MiB | 36.2 MiB | 43.5 MiB | reported (D-022) |
| Retained heap over baseline | +12.4 MiB | +19.0 MiB | +20.8 MiB | ≤ 32 MiB |
| Stream CPU, 60 s / 10 min | 12.2 % | 6.6 % / 7.0 % | 7.4 % / 8.0 % | ≤ 15 % |
| Memory from the OS, 60 s / 10 min | 156 MiB | 177 / 185 MiB | 185 / 194 MiB | ≤ 256 MiB |
| List p95, 20 clients, restricted `sort=health` | 83 ms | 35.5 ms | — | < 100 ms |
| Delegation of the widest parent, p95 sequential / 20 clients | — | 0.34 / 0.52 ms (restricted 0.40 / 0.63 ms) | — | < 50 / < 100 ms |
| Route detail, deepest child, p95 | — | 55 µs | — | < 50 ms |
| Dense selection, Large (20 × 500) | — | 63 ms, 10,000 rows, no cap | — | reported |

`bench/2.4.txt` was recorded on 2026-10-27, before D-025 and the Codex memory fixes, so its Extreme dense row still runs at the old 50,000 edge cap. The CI record is the reference for HEAD.

Memory gates (`bench` tag; both rule sets where it applies):

| Gate | Measured | Limit |
|---|---|---|
| `TestBudgetDenseMemory`, Extreme dense at the default cap, peak heap | 389 MiB | ≤ 432 MiB |
| `TestBudgetDependencyAggregates`, fan-in | 40 MiB | ≤ 64 MiB |
| `TestBudgetHubPeak`, 9,000 roots on one hub, peak during the run | ~48 MiB | ≤ 64 MiB |
| `TestBudgetInheritedMerges`, inherit chain | 49 MiB | ≤ 64 MiB |
| `TestBudgetDeepChain`, 3,000 routes at depth 100 | 1.2 MiB | ≤ 4 MiB |
| `TestBudgetMatcherMaterialization`, one page | 25 MiB | ≤ 32 MiB |

`make bench` takes about 530 s locally (22–27 min expected on CI); the 10-minute stream run takes 21 min.

## ui-loop recordings
Listed in `phase-2.4-evidence.md`; local, not committed, run against v2.4.7 on 2026-10-28. Run dir `/var/folders/l9/kvw9vb8121720jclk572xwpw0000gp/T/ui-loop/kgateway-ui-claude/task-69/`:
- videos `artifacts/probe-{1-agreeing,2-truncated,3-unknown,4-restricted}…/video.webm`;
- screenshots in light and dark at 1280 and 720 px: agreeing, truncated (20 of 21 paths), unknown, restricted, and the edge-cap fix in `artifacts-fix/probe-3-unknown-past-the-edge-cap/`;
- e2e shots `web/test-results/explorer-delegation-partial.png` and `explorer-effective-matches.png`.

## Release note
In README "Upgrade notes":
- the Delegation tab and effective matches;
- selected and attached counts, and the `dropped` flag;
- KGWUI-011 computed and orphan triggers; KGWUI-012 for delegation;
- `scope-unknown` covers views B and C; hidden ancestors by name and match;
- the edge cap, `--delegation-edge-cap` and `delegation.edgeCap`;
- the Go memory limit at 90 % of the pod limit, `KGWUI_MEMORY_LIMIT`, and `app.Run` for downstream builds.

## Rulings made during execution
These are recorded in the plan workspace ledger. The ones that changed behaviour or the design:
- An unknown discovery scope makes views B and C unknown. A visible child shows its hidden ancestors' names and delegated matches, nothing else (user, 2026-10-26, D-024).
- Non-route dependencies still restrict: a root admitted through a hidden Namespace, or a status root with a hidden target (our call, raised with the user). Hidden ancestor routes show the same way on the up-chain and in KGWUI-011 and KGWUI-012 evidence (our call, extends D-024).
- Child-edit drops clear on both tags, observed on kind (2026-10-26). One rule replaces the split by cause: a leftover entry after any drop disagrees after 30 s, on both rule sets (D-021 amended). Forbidden leftovers are verified too. A 2.3 parent entry the controller never writes is not observable.
- Forbidden is always known; unresolved rows keep their contexts. An unknown wildcard setting keeps a `setting-unknown` row with own-namespace label children certain.
- Report keys split into accepted and uncertain; the context cap is 30,000 per install.
- The 2.4 warm recompute gate is 150 ms, per design §9.6.
- A restricted computed fact carries no rule name. A ref whose selection is hidden is one restricted zero-child row. Every row carries root-eligibility dependencies. Evidence strips ancestors only (design §7.4 rules 4c and 4d).
- A ref cut by the edge cap reads unknown, too large, at ref level, not "selects no routes" (rule 4c).
- The default edge cap is 10,000, down from 50,000, and configurable. The Go memory limit follows the pod limit (user, 2026-10-27, D-025).
- The dense gate checks peak heap only (≤ 432 MiB). The binary sets the Go memory limit to 90 % of the pod limit; an explicit `GOMEMLIMIT` wins (user, 2026-10-27, D-025 amended).
- Memory bounds from the Codex rounds (our calls unless noted): shared path prefixes; depth bound 100; a page budget in materialized units; merges cost ceil(keys/4) matcher steps; finding match text capped at 512 bytes; interned dependency sets, finished sets only.
- The compute caps stay install-wide. Hidden routes can make an unrelated visible row read "too large to compute" (user, 2026-10-28, design §7.4 rule 4d).
- Fix the merge-size vector, then run one final Codex round; anything left becomes a documented known limit and a P3 carry-over (user, 2026-10-28).
- From the final round, fix the hub fan-in regression only. Inherited path bytes and failure-reason text are known limits (user, 2026-10-28).

## Notes for later phases
- The Phase 2 cross-model review is next, after the 2.4 PR merges.
- (P3) The sustained-stream test edits annotations only, so it measures the views memo's hit path. Add a reported (not gated) stream variant with spec edits to show the full-recompute CPU on CI. The local `bench/2.4.txt` predates D-025 and the memory fixes; `bench/2.4-ci.txt` is the reference for HEAD.
- (P4) Apps that embed `Shell` must wait for fonts before rendering, as `web/app/src/main.tsx` does (`cc85f1c`), or long names can shift the layout on load.
- The compute caps are install-wide by design (rule 4d). A per-root budget would change outcomes and needs a design change.
- The API serves no `parent-resolvedrefs` check, so the e2e builds the backend in-process to list them.
- The context and matcher caps are not configurable, so an effective matcher can only be unknown through them, not through the edge cap.
- The ui-loop recorded 2.4.7 only; the 2.3 e2e covers the same pages.
- `make bench` grew to about 9 min locally; watch the CI job's timeout.

## Deferred / open
- (P3) Delegation memory: bound generated path bytes in inherit merges, and charge failure-reason text and JSON escaping in the page budget — Codex final round F1 and F3, user 2026-10-28; carried in `phase-status.md`.
- (P3) After a cap, a row reads `too-large` where it might be `not-reached`; precision only (Task 57, parked).
- (P3) The seed route sits in its own closure; the safe direction (Task 58, parked).
- (P4) The forced-disagreement e2e runs only in the v2.4.7 row (from 2.3).
- (P4) The `allows` labels are not rendered; their wording is kept in `phase-2.4-wording.md` if they are.
