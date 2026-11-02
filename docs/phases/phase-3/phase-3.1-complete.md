# Phase 3.1 — Foundations, performance and the KRT decision (Complete)

**Completed:** 2026-11-02
**Range:** 7f129a6..HEAD (branch `duncandoyle/phase3Seams`)

## Summary
3.1 did the performance work first, then laid the ground the rest of Phase 3 builds on. Users see the same pages, easier to read and navigate, and findings that have ids and page on the server.
- **Baseline and KRT:** the Phase 2 baseline on `544dcd5`, locally and on CI, and a reported spec-edit stream variant. The KRT spike failed two of five criteria, so the engine keeps the immutable full recompute (D-027).
- **Check contract:** one contract, `Name`, `Inputs`, `Run(RunContext)`, with an engine-owned Store per check (D-032). `app.Build` rejects a check that declares a later or missing check, and duplicate names. A test guard fails when a check reads an input it did not declare.
- **Memoization:** a check whose declared inputs, wake time and earlier outputs are unchanged is skipped. Memoized and uncached engines give byte-equal `/health`, `/overview` and `/findings`.
- **Delegation bounds:** an inherit merge past 4 KiB is `too-large`, never `dropped`; only display strings are clipped. The page budget charges reason text and JSON escaping.
- **Controller metrics:** reject counters per Pod UID and type, never negative across restarts. Dropped updates turn scope verification into one KGWUI-008.
- **Finding identity:** every finding has an `id` and a `group`. Ids hash only data a restricted row shows (D-040). UI-derived findings carry a `since` that resets on restart.
- **Groups and rank:** groups order by severity, class, affected count, oldest `since` and id. Unclaimed findings form one "Other" group per resource; KGWUI-008 is not ranked.
- **Findings API:** `GET …/findings` (filters, sort, sealed keyset cursors bound to the install) and `GET …/findings/{id}` (affected objects paged). `q` never matches hidden text.
- **Shell:** the server-paged findings page and finding page, one `ObjectHeader` on every detail page, a fluid layout to 2400 px with a side column from 1600 px (D-042), and path-based breadcrumbs resolved through `GET …/refs` and the Authorizer (D-041).
- **Test worlds:** scale tiers gain Services, EndpointSlices, proxy Pods, Warning Events and CRDs; a kind NACK fixture with captures on v2.3.11, v2.4.7 and v2.5.0-rc.1.

## Sub-phases (phase record only)
n/a

## Reviews
- **Task reviews:** Tasks 12–32 each had a spec and quality review, with fix rounds confirmed by scoped re-reviews.
- **Cross-model, three Codex rounds** (Codex reviewed Claude's work):
  1. `reviews/3.1-codex.md` (`7f129a6..bad4c30`): 3 P1, 2 P2, 2 P3. Four accepted and fixed in [`63c8cd9`](https://github.com/DuncanDoyle/kgateway-ui/commit/63c8cd9), [`8a406bc`](https://github.com/DuncanDoyle/kgateway-ui/commit/8a406bc) and [`0fbe846`](https://github.com/DuncanDoyle/kgateway-ui/commit/0fbe846). Three contradicted rulings R24, R31 and R30; the user decided each (D-040, D-041, D-042).
  2. `reviews/3.1-codex-r2.md` (`bad4c30..9d33adb`): five resolved, two partly; five new findings (3 P1, 2 P2), all accepted and fixed.
  3. `reviews/3.1-codex-r3.md` (`9d33adb..205e164`): all round 2 items resolved; one new P2, fixed in [`94dc322`](https://github.com/DuncanDoyle/kgateway-ui/commit/94dc322).

## Wording pass
Done: `phase-3.1-wording.md`. 39 strings (S265–S303), five options each, chosen by an independent picker. R32 keeps two "Cannot load …" strings for consistency.

## Waivers
None.

## Deliverables
- Acceptance tests failed before and passed after, per the task reports (Tasks 12–32, RED/GREEN sections in the SDD workspace).
- Standard gates: vet, `go test -race ./...` (26 packages), lint, web type-check, vitest (620 tests), Helm, the downstream example: all pass.
- kind e2e, the three `e2e.yaml` rows, run locally on [`205e164`](https://github.com/DuncanDoyle/kgateway-ui/commit/205e164): all pass. Each row: Go e2e `ok`, Playwright 44/44.
- kind e2e, v2.4.7 row, on [`94dc322`](https://github.com/DuncanDoyle/kgateway-ui/commit/94dc322) (web-only fix): Go e2e ok, Playwright 44 passed.
- CI bench run: [bench.yaml run 41495360233](https://github.com/DuncanDoyle/kgateway-ui/actions/runs/41495360233) on the throwaway branch `duncandoyle/bench31` at [`205e164`](https://github.com/DuncanDoyle/kgateway-ui/commit/205e164) (later commits change web code only): all gates pass (`bench/3.1-ci.txt`).
- Evidence: `phase-3.1-evidence.md`. It was recorded before D-042, so its one-column note is out of date; the browser test now checks the split.

### Benchmarks (Large tier)
Records: `bench/3.1-baseline.txt`, `bench/3.1-baseline-ci.txt`, `bench/3.1.txt` (local, on [`4997354`](https://github.com/DuncanDoyle/kgateway-ui/commit/4997354)), `bench/3.1-krt-spike.txt`. Local runs on one 18-core arm64 machine.

| Measure | `544dcd5` | 3.0 | 3.1 | KRT spike | Budget |
|---|---|---|---|---|---|
| Warm recompute median, consistent / contradicting | 40.6 / 46.7 ms | 38.9 / 44.1 ms | 34.3 / 42.2 ms (worst 43.3) | 41.1 / 46.8 ms (current 38.3 / 44.1) | ≤ 150 ms |
| Retained heap over 2.1, consistent / contradicting | +23.2 / +24.4 MiB | +23.1 / +24.3 MiB | +23.0 / +27.8 MiB | +41.7 / +40.9 MiB | ≤ 32 MiB; ≤ +24 MiB over baseline |
| Dense: retained / cold / per warm | 160 / 210 / 168 MiB | 160 / 210 / 168 MiB | 160 / 207 / 166 MiB | 162 / 212 / 168 MiB | 176 / 246 / 185 MiB |
| Stream CPU, annotation-only, consistent / contradicting (60 s) | 6.6 / 7.5 % | 6.6 / 7.3 % | 6.9 / 9.5 % | not run | ≤ 15 % |
| Stream CPU, spec-edit, 60 s | 8.4 % | 8.4 % | 9.9 % | not run | reported |
| Stream CPU, spec-edit, 10 min | 11.0 % (1.31/s) | 11.0 % (1.30/s) | 13.2 % (1.33/s) | 14.2 % (current 10.9 %) | reported; ≤ 25 % from 3.2 |
| `GET …/findings`, worst filter mix p95, sequential / 20 clients | — | — | 0.56 / 1.21 ms (`q`, restricted) | — | < 50 / < 100 ms |
| Analyzer stage, warm and memoized, large / worst | — | — | 2.0 / 8.1–8.2 ms | — | reported; gated from 3.2 |

CI (linux/amd64, 4 CPUs):

| Measure | Phase 2 baseline CI (`544dcd5`) | 3.1 CI | Budget |
|---|---|---|---|
| Warm recompute median, consistent / contradicting / worst | 99.3 / 118.1 ms / — | 83.7 / 105.5 / 113.6 ms | ≤ 150 ms |
| Retained heap over 2.1, consistent / contradicting | +23.0 / +24.8 MiB | +23.0 / +26.8 MiB | ≤ 32 MiB |
| Dense: retained / cold / per warm | 160 / 224 / 168 MiB | 160 / 221 / 166 MiB | 176 / 246 / 185 MiB |
| Stream CPU, annotation-only, consistent / contradicting | 9.4 / 11.4 % | 7.9 / 10.6 % | ≤ 15 % |
| Stream CPU, spec-edit (60 s) | 14.0 % | 11.7 % | reported |
| Findings `q`, restricted, 20 clients p95 | — | 62 ms | < 100 ms |

Task 32 found three problems before the record:
1. `TestBudgetMatcherMaterialization` used 1,623 MiB against ≤ 32 MiB. A delegation row built its full context before charging it (Task 19). Fixed in [`00c5bc3`](https://github.com/DuncanDoyle/kgateway-ui/commit/00c5bc3).
2. CI `q` p95 was 122 ms with 20 clients. Search text is now lowered once per model. Fixed in [`ed62cc9`](https://github.com/DuncanDoyle/kgateway-ui/commit/ed62cc9).
3. CI warm recompute, worst case, was 151.3 ms against 150. The user chose to optimise with the gate unchanged. Ids are memoised and grouping is cheaper in [`5ef5434`](https://github.com/DuncanDoyle/kgateway-ui/commit/5ef5434) and [`2711d37`](https://github.com/DuncanDoyle/kgateway-ui/commit/2711d37); CI then measured 102.9 / 134.5 / 139.0 ms.

## Release note
Findings now have stable ids, groups and a ranked, server-paged findings page with a page per finding. Detail pages gain an object header, a side column on wide screens and breadcrumbs that follow your path. Downstream checks move to a new contract with declared inputs (README, Upgrade notes).

## Rulings made during execution
1. **R14:** the `544dcd5` baseline also runs the D-039 dense test from HEAD. If wrong: a re-run.
2. **R15:** spec edits bump `metadata.generation` and leave status stale. If wrong: the spec-edit stream is re-measured.
3. **R16:** Task 15 migrates every old-contract implementer at once, so the module builds. If wrong: nothing.
4. **R17:** `ValidateOrder` also rejects duplicate check names. If wrong: a downstream with two same-named checks fails `app.Build`.
5. **R18:** `CollectedAt` is a timestamp; a memo hit restamps it. Codex round 1 #1 made dependents see it. If wrong: a field-semantics change.
6. **R19:** `Snapshot.Synced` and `TakenAt` are always inputs. If wrong: checks rerun more often than needed.
7. **R20:** memoization gives no warm speed-up yet; old checks declare `Clock`. If wrong: a narrowing pass on old checks if 3.2 misses the ≤ 2 ms memoized gate.
8. **R21:** Task 18's memo deviations accepted (`Print` returns `(uint64, bool)`, access hashed from `Snapshot.Access`). If wrong: a local refactor of the memo.
9. **R22:** UI-derived `since` applies to every finding without one, 003 and 004 included. If wrong: a golden update.
10. **R23:** id keys: 008 by check name, written ports in parentRef keys, policy 002 by resolved ancestorRef. If wrong: ids change once, before release.
11. **R24:** ids hash condition types and computed parents. Superseded by D-040.
12. **R25:** "Other" is one group per resource; a group's class is its lowest member's; KGWUI-008 groups apart. If wrong: group ids change.
13. **R26:** findings sort uses Phase 2's `.asc` / `.desc` suffixes. If wrong: the sort parameter changes in API and SPA.
14. **R27:** `sort=since` orders restricted rows by their hidden `since`, like rank does. If wrong: a row's position hints at its age.
15. **R28:** extra files in Task 26 accepted. If wrong: nothing.
16. **R29:** extra files in Task 28 accepted. If wrong: nothing.
17. **R30:** core pages stay one column; the side holds contributed sections only. Superseded by D-042.
18. **R31:** crumbs the page does not hold link by URL name. Superseded by D-041.
19. **R32:** S279 and S285 keep "Cannot load …". If wrong: two strings reworded.
20. **R33:** status-only owners (policy ancestors, status route parents) are ordinals too. If wrong: more ids shift with siblings than needed.
21. **R34:** trail lookups cover the last 12 crumbs, at most two requests; reconnect refetches only while retrying. If wrong: old crumbs of long trails lose links.

## Notes for later phases
- The ≤ 2 ms memoized analyzer gate (3.2) needs narrower inputs: memoized runs 8.1 ms worst case, the same as warm, because Phase 1–2 checks declare `Clock` (R20).
- Retained heap has 4.2 MiB headroom in the contradicting world (+27.8 of 32 MiB). 3.2 watches add to it.
- Dense allocation per warm recompute rose to 175 MiB mid-3.1 (Tasks 22–24) and fell to 166 after Task 32. New families add to the same path.
- `Affected` copies whole findings per group; consider an index-based form when 3.2 families land.
- Scale world gaps: proxy Pods share one PodIP and lack a ReplicaSet owner; Events set only `LastTimestamp`; long Gateways have no status or proxy Deployment.
- The top bar crowds at 720 px: the nav sits close to the install label.
- The caller of `TotalGrowth` must skip `Err` results when §16.2 per-Pod state is wired.
- `@tanstack/react-virtual` is unused; remove it with the next dependency change.
- With `NACK=1`, `edge` rejects the Cluster too; 3.2 e2e expectations must allow KGWUI-001 there. Proxy `update_rejected` grows by hundreds a minute, so deltas must not count growth as new episodes.
- Evidence recordings (clips and screenshots) are kept outside the repo.

## Deferred / open
- (P3) Memoized analyzer gate needs narrower inputs (R20), above; in `phase-status.md`.
- (P3) Retained heap headroom 4.2 MiB in the contradicting world, above; in `phase-status.md`.
- Minors in the SDD ledger: `DocumentTitle` S294/S302 and `ObjectHeader` S303 prefixes are inline; `ObjectHeader` h1 `position: relative` unused; `defaultIcon` nested ternary; affected paging is synthetic until 3.2 families.
