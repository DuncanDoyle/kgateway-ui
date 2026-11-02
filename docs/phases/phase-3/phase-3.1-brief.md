# Sub-phase 3.1 — Foundations, performance and the KRT decision

Kind: planned
Base: 7f129a69f059980879339f3cd491fdb24c64bc23
Issues: n/a | Plan steps: implementation-plan.md Tasks 12–33; design §5, §8.2, §13.1–§13.3 (findings), §15, §16, §17.6, §23 (3.1 row); D-027, D-028, D-032, D-035, D-039

## Problem / goal
Phase 2 ended near its CPU and memory budgets, and every later sub-phase adds load. 3.1 does the performance work first: a Phase 2 baseline, the spec-edit stream variant, a KRT spike with a decision written down beforehand (D-027), and per-check memoization. It then lays the ground the rest of Phase 3 builds on: one check contract with declared inputs and an engine-owned Store (D-032), stable finding ids, `since`, groups and rank (D-028), and server-paged findings. The shell gets an object header, a fluid layout and path-based breadcrumbs (D-035). Users see the same pages, easier to read and navigate, and findings that have ids and page on the server.

## Expected behaviour

| Input / action | Expected |
|---|---|
| `TestBudgetSpecEditStream` at Large: one HTTPRoute path value toggled every 100 ms for 10 min (`-short`: 60 s), 20 clients polling `/routes` every 5 s. Each edit cycles to the next route, bumps its generation and leaves status stale, so KGWUI-003 findings accumulate and the KGWUI-012 cross-check has no candidates | Reports process CPU % of one core, recomputes/s, GC share and OS memory; gates nothing in 3.1 (Task 12) |
| Phase 2 baseline on `544dcd5` and on the 3.0 HEAD, locally and on CI | Recorded in `docs/phases/phase-3/bench/3.1-baseline.txt` and `bench/3.1-baseline-ci.txt`; an existing gate broken on either commit, or spec-edit CPU above 22 %, stops the work and the numbers go to the user (Task 12, §16.3) |
| The KRT spike (view A as KRT collections, throwaway branch) | Measured at Large against the current engine: warm recompute, spec-edit stream CPU, retained heap, dense memory; plus module graph, binary size, Go floor and licences. The decision rule below picks Adopt or Keep; D-027 records the numbers and the outcome; the user sees them before Task 15 (Tasks 13, 14) |
| A check implements `Name`, `Inputs`, `Run(RunContext)` and writes its Store | The value survives recomputes, is cleared on engine restart, is invisible to other checks and to other installs (Task 15) |
| A check declares `Checks: ["x"]` where `x` runs later or not at all | `app.Build` fails (`health.ValidateOrder`) (Task 15) |
| A check reads `RunContext.Collected` for a key it did not declare | Returns false (Task 16) |
| A check's undeclared inputs change (kinds dropped, metrics, verify or computed results replaced, `Now` moved before its next deadline) | Its findings and `CheckResult` are equal; a failure names the check and the input (Task 17) |
| An annotation-only edit to a kind a check does not declare | The check is skipped and its last findings, result and wake time are reused (Task 18) |
| A check's wake time passes, or it declares `Clock` | It runs (Task 18) |
| A watch fails or recovers with its objects unchanged | `cluster access` reruns and reports it (Task 18) |
| The same scripted sequence through a memoizing and a non-memoizing engine | `/health`, `/overview` and `/findings` bodies are byte-equal after every step (Task 18) |
| An inherit merge whose path would exceed 4 KiB | The child's context is `unknown` / `too-large`, never `dropped`; no KGWUI-011 or KGWUI-012 follows; only display strings are clipped, with `…[truncated]` (Task 19) |
| A page of rows with long reasons full of `"` and `<` | The page stops earlier than one with short reasons; the cursor resumes without loss (Task 19) |
| A controller replica restarts and its `rejects_total` resets | The delta per Pod UID is never negative; a vanished UID's value is forgotten (Task 21) |
| Any controller Pod reports `resources_updates_dropped_total > 0` | No KGWUI-007; one KGWUI-008 "scope verification unavailable: the controller dropped metric updates" (Task 21) |
| A finding is reworded, its severity or `since` changes, or the UI restarts | Its `id` (`^f1-[a-z2-7]{26}$`) is unchanged; ids are unique within an install (Task 22) |
| A UI-derived finding persists across recomputes, then the UI restarts | `since` stays at first-seen, then resets; status-derived findings keep `lastTransitionTime` (Task 22) |
| Findings of every class, with ties at each rank key | Groups order by severity, class, affected count (descending), oldest `since`, group id; each finding is in exactly one group; unclaimed findings form an "Other" group per resource; KGWUI-008 is not ranked (Task 23) |
| `GET …/findings` with `severity`, `code`, `kind`, `namespace`, `group`, `q`, `sort` and a cursor | A keyset page; forward and back through 3,309 KGWUI-012 at Large give the same sets; a bad filter value, sort or foreign cursor → 400 (Task 24) |
| `GET …/findings/{findingId}` | `FindingDetail` with 50 affected objects per page and `?affectedCursor=`; an unknown id → 404 (Task 24) |
| Deny-some Authorizer, `q` matching a hidden finding's message | Matches nothing; `total` is stable under every filter; restricted rows keep `id` and `group` (Task 24) |
| `/health` and `/overview` | Unchanged except each finding gains `id` and `group` (additive test) (Task 24) |
| Scale tiers at a fixed seed | Services, EndpointSlices, proxy Pods, Warning Events and CRD counts match §16.3, deterministically; the worst case sets its shares (Task 25) |
| The SPA findings page | Filters, sort and cursor round-trip through the URL; a 400 on a cursor drops it and loads page one; restricted rows show code, severity and the restricted label; skeleton rows have the final height (Task 26) |
| Any core object detail page | One `ObjectHeader`: kind icon and label, the name in the largest type, namespace muted, state and data age; the same height loading and loaded (Task 27) |
| Viewport 720, 1280, 1600, 3008 px | No horizontal page scroll; lists fill up to 2400 px; two detail columns from 1600 px, the side column stacked after main below it; prose capped at 80ch (Tasks 28, 30) |
| Gateway → route → child route, then a crumb | Each crumb returns to that page with its own trail; reload and a copied URL keep the trail; a cycle cuts the trail back; past 6 entries the oldest collapse into "…" (Task 29) |
| A `via` entry that is malformed, unresolvable, from another install or for a hidden object | Plain text, dropped, or the restricted label respectively; the page still loads; no `via` shows `<install> › <kind list> › <object>` (Task 29) |
| `GET …/findings` at Large, each filter mix, 20 concurrent clients | p95 < 50 ms, p99 < 100 ms; p95 < 100 ms concurrent (Task 32) |

## KRT decision rule (design §16.1, written before the spike runs)

| Outcome | Decision |
|---|---|
| Spec-edit stream CPU at least 40 % lower, retained heap no more than +16 MiB, every request still reads one immutable published value, no `replace` directives, Apache-2.0 compatible licence | **Adopt** KRT for the views and analyzers in a follow-up phase. Phase 3 checks declare their inputs (§16.2), which KRT can feed later |
| Anything less, or inconclusive | **Keep** the immutable full recompute. Add per-check memoization (§16.2) |

Either way the result is recorded as D-027, with the numbers. Neither outcome changes the remaining Phase 3 tasks: Phase 3 ships the contract with declared inputs and memoization (Task 14).

## Root cause (if known)
n/a (planned work). The carry-overs are listed in design §15.

## Acceptance tests
- Task 12: `TestBudgetSpecEditStream` (reported, not gated).
- Task 15: `TestStoreSurvivesRecompute`, `TestStoreClearedOnEngineRestart`, `TestChecksCannotReadEachOthersStore`, `TestStoresAreIsolatedPerInstall`, `TestOutputRequiresDeclaredEarlierCheck`, `TestPhase2GoldensUnchangedOnNewContract`.
- Task 16: `TestCollectedOnlyForDeclaredKeys`; `TestExampleUsesEverySeam` with the declared collector key.
- Task 17: `TestChecksReadOnlyDeclaredInputs`.
- Task 18: `TestUnchangedInputsSkipCheck`, `TestDueDeadlineRunsCheck`, `TestStoreWritingCheckStillSkipped`, `TestMemoNeverOutlivesStaleness`, `TestSkippedCheckKeepsGraceDeadline`, `TestAccessFailureAndRecoveryRerunChecks`, `TestMemoizedResponsesEqualUncached`.
- Task 19: `TestOversizedInheritIsTooLargeNotDropped`, `TestComputedPathsAreNeverClipped`, `TestDisplayClipOnly`, `TestPageBudgetChargesReasonAndEscaping`.
- Task 21: `TestParsePerTypeAndPod`, `TestCounterDeltaPerPodUIDNeverNegative`, `TestDroppedUpdatesTurnScopeCheckUnavailable`.
- Task 22: `TestIDFormat`, `TestIDStableAcrossRecomputesAndRestart`, `TestRewordingKeepsID`, `TestDiscriminatorSeparates`, `TestIDUniqueWithinInstall`, `TestUIDerivedSinceSurvivesRecomputeAndResetsOnRestart`, `TestStatusFindingKeepsLastTransitionTime`.
- Task 23: `TestRankOrderEveryClass`, `TestGroupMembershipExclusive`, `TestOtherGroupPerResource`, `TestSignalUnavailableNotRanked`.
- Task 24: `TestFindingsPaging`, `TestFindingsFilters`, `TestFindingsFiltersRevealNothingHidden`, `TestFindingDetailAffectedPaging`, `TestHealthFindingsGainIDAndGroup`, the four new goldens, the additive test.
- Task 25: `TestScaleWorldMatchesTier`, extended.
- Task 26: the six findings-page tests of the plan.
- Task 27: the four `ObjectHeader` tests of the plan.
- Task 28: `"side column stacks under main below 1600"`, `"prose blocks cap at 80ch"`, `"no 1400px cap remains"`.
- Task 29: the eight breadcrumb tests of the plan.
- Task 30: `web/e2e/navigation.spec.ts` on kind (kgateway v2.4.7).
- Task 32: `TestBudgetFindingsRequests`; `BenchmarkAnalyzerStage/{warm,memoized}` (reported).

## Scope
In: `pkg/api/budget_test.go` and `Makefile` (spec-edit variant), `docs/decisions.md` (D-027), `pkg/health` (contract, Store, identity, rollup), `pkg/engine` (States, memo), `examples/downstream` (migration), `pkg/computed` (path bound, display clip), `pkg/api` (findings endpoints, page budget, goldens), `pkg/ctrlmetrics` and `pkg/scopeverify` (per Pod and type, dropped updates), `pkg/diagnose` (groups, rank, index), `internal/testutil` (scale tiers, input perturbation), `test/e2e` (NACK fixture, captures), `web/packages/shell` (findings pages, `ObjectHeader`, layout, breadcrumbs, copy), `web/e2e/navigation.spec.ts`, `.github/workflows/bench.yaml` (paths), README upgrade notes. The KRT spike lives on the local branch `duncandoyle/krtSpike` and is never merged.
Out: KRT in production code (a follow-up phase, on Adopt); new analyzers and families (3.2–3.4); `…/diagnostics` and `…/processes` (3.2); evidence kinds beyond those of the findings endpoints; moving KGWUI-012 persistence onto the Store (stays in `pkg/engine/persist.go`); gate changes of any kind.

## Plan / design impact
D-027 (2026-11-02): **Keep** the immutable full recompute; the KRT spike failed the CPU and retained-heap criteria (`bench/3.1-krt-spike.txt`). Task 14 appended D-027; on Adopt, `docs/roadmap.md` gains "Incremental recompute with KRT for views and analyzers". Task 33 changes `design.md` only where the code deviates. Approval of the plan covers the ⚠ guardrail tasks 15, 18, 19, 22, 24, 26, 27 and 28, whose file lists the plan names (ruling 2026-11-01, user approved the entire phase).

Budgets (§16.3; dense gate per D-039):

| Measure | 3.1 budget at Large |
|---|---|
| Warm recompute, whole model | ≤ 150 ms, consistent and worst case |
| Stream CPU, annotation-only | ≤ 15 % of one core |
| Stream CPU, spec-edit | reported; gated from 3.2 at ≤ 25 %; above 22 % on the baseline, the numbers go to the user first |
| Retained heap | ≤ 32 MiB, and ≤ +24 MiB above `3.1-baseline.txt` |
| Dense memory, medians of 5 at the default edge cap | retained heap ≤ 176 MiB; cold-recompute allocation ≤ 246 MiB; per warm recompute ≤ 185 MiB; peak heap and RSS logged, not gated |
| `GET …/findings`, each filter mix | p95 < 50 ms, p99 < 100 ms; p95 < 100 ms with 20 clients |
| Analyzer stage | reported (`BenchmarkAnalyzerStage`); gated from 3.2 |
| Change burst, idle, cold recompute | Phase 2 gates, unchanged |

No gate changes without the user. A broken gate stops the work, and the numbers go to the user.

**Baseline** (Task 12 Step 5, 2026-11-02). No gate broken on either commit; spec-edit CPU below 22 %, so the work goes on.

| Measure (Large) | `544dcd5` local | 3.0 local | `544dcd5` CI | Budget |
|---|---|---|---|---|
| Warm recompute median, consistent / contradicting | 40.6 / 46.7 ms | 38.9 / 44.1 ms | 99.3 / 118.1 ms | ≤ 150 ms |
| Retained heap over 2.1, consistent / contradicting | +23.2 / +24.4 MiB | +23.1 / +24.3 MiB | +23.0 / +24.8 MiB | ≤ 32 MiB |
| Dense: retained / cold / per warm | 160 / 210 / 168 MiB | 160 / 210 / 168 MiB | 160 / 224 / 168 MiB | 176 / 246 / 185 MiB |
| Stream CPU, annotation-only, consistent / contradicting (60 s) | 6.6 / 7.5 % | 6.6 / 7.3 % | 9.4 / 11.4 % | ≤ 15 % |
| Stream CPU, spec-edit, 60 s | 8.4 % | 8.4 % | 14.0 % | reported |
| Stream CPU, spec-edit, 10 min | 11.0 % (1.31 recomputes/s) | 11.0 % (1.30/s) | not run | reported; ≤ 25 % from 3.2 |

The 10-minute spec-edit run recomputes 1.3 times a second, more than the annotation stream's 1/s; it is reported, not gated. The CI cold allocation is 224 MiB on linux/amd64; its budget is 246 MiB (user, 2026-11-02; D-039).

**Memoization.** The warm-recompute delta of `BenchmarkCompute` is recorded here by Task 18.

**Captures.** The NACK observation and the 2.5 timebox outcome are recorded here by Task 20.

Reminders for the user (not blocking): the open item in design §25 (filing the reject-status draft), and the upstream scope-endpoint issue (carry-over P3). Filing both stays the user's call.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] standard gates pass; the 3.1 budgets above hold (`docs/phases/phase-3/bench/3.1.txt`, `3.1-ci.txt`)
- [x] D-027 recorded with the spike numbers and shown to the user before Task 15
- [ ] Codex review of `git diff 7f129a6..HEAD` + triage (`reviews/3.1-codex.md`)
- [ ] phase-3.1-complete.md written (benchmark table: baseline `544dcd5`, 3.0, 3.1, spike); phase-status.md and roadmap.md updated; README upgrade notes (check contract, finding `id` and UI-derived `since` that resets on restart, server-paged findings page)
