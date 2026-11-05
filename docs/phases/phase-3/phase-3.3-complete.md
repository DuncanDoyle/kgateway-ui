# Phase 3.3 — Analyzer (Complete)

**Completed:** 2026-11-05
**Range:** 79a5307..HEAD (branch `duncandoyle/phase3Seams`)

## Summary
3.3 adds the analyzer: stable finding codes that explain why config does not work, with a suggested fix where one is safe.
- **KGWUI-003 kinds:** a status that is stale, pending, never written or has no ancestor from this install now says which. Evidence never echoes more than 1 KiB of a status message.
- **New codes:**
  - 013 unresolved ref;
  - 014 ref not permitted;
  - 015 rules replaced or dropped;
  - 016 policy target not found (computed, with a verifier);
  - 017 listener conflict;
  - 018 status not progressing.
- **Suggested fixes:** for 013 (a corrected group) and 014 (the ReferenceGrant YAML, created with `kubectl create -f`). No fix is offered when the cluster cannot tell whether a group serves the kind.
- **Groups and the grouped view:** "Missing backend", "Not permitted", "Status not progressing" and "Other". `GET …/findings?view=groups` pages groups on the server, matching filters through member findings.
- **Controller log signals (opt-in, `diagnostics.controllerLogs`):** xDS rejects, snapshot deferrals ("withheld") and panics from the controller's logs. The UI keeps fixed fields only and never stores or shows log text. Parsers are pinned per kgateway version (2.3, 2.4).
- **Docs:** `docs/findings.md`, one entry per code, reached through "Learn more" on every finding.
- **One-shot callers (E8, D-053):** `pkg/setup` builds the cache and engine config the way the UI does; `ctrlmetrics.Static` serves one metrics collection.

## Sub-phases (phase record only)
n/a

## Reviews
- **Task reviews:** Tasks 56–72 and 71a each had a spec and quality review, with fix rounds confirmed by scoped re-reviews (task files in the SDD workspace).
- **Cross-model, three Codex rounds** (Codex reviewed Claude's work):
  1. `reviews/3.3-codex.md` (`79a5307..0a196dc`): 6 findings (5 P1, 1 P2), all accepted and fixed in [`b7d56ea`](https://github.com/DuncanDoyle/kgateway-ui/commit/b7d56ea), [`445da5d`](https://github.com/DuncanDoyle/kgateway-ui/commit/445da5d), [`3a4e6c1`](https://github.com/DuncanDoyle/kgateway-ui/commit/3a4e6c1), [`7630a14`](https://github.com/DuncanDoyle/kgateway-ui/commit/7630a14), [`2c1cd2c`](https://github.com/DuncanDoyle/kgateway-ui/commit/2c1cd2c) and [`f70d082`](https://github.com/DuncanDoyle/kgateway-ui/commit/f70d082).
  2. `reviews/3.3-codex-rereview.md` (`0a196dc..f70d082`): all six resolved; one new P1 (018 history unbounded while stuck objects rotate), fixed in [`c97c3eb`](https://github.com/DuncanDoyle/kgateway-ui/commit/c97c3eb), and one P3 (re-run the bench), done.
  3. `reviews/3.3-codex-rereview2.md` (`f70d082..c97c3eb`): the P1 resolved; no new findings.

## Wording pass
Done: `phase-3.3-wording.md`. 96 strings (S472–S567), five options each, chosen by a separate picker; 11 changed. R109 lets every pick stand. S475 stays lower-case because a frozen v1 golden pins it (R110, user 2026-11-05).

## Decisions
- D-053: `pkg/setup` and `ctrlmetrics.Static` for one-shot callers (user, 2026-11-04).
- D-054: retained heap over 2.1 raised to ≤ 40 MiB (user, 2026-11-05).
- D-055: the as-built deviations from the design, confirmed by the user on 2026-11-05; item 14 (R102: the newest panic keeps a reserved slot in the capped event store), added at the close, confirmed the same day.
- The user confirmed rulings R77, R79, R82, R84, R94/R95 and R101 on 2026-11-05, and kept the defaults for R96 (sync-only publishes, revisit in 3.4), 018 outside the analyzer-stage budget, and S475 (R110).

## Waivers
None.

## Deliverables
- Acceptance tests failed before and passed after, per the task reports (Tasks 56–72, 71a) and `close-33-fix-report.md`.
- Standard gates on [`c97c3eb`](https://github.com/DuncanDoyle/kgateway-ui/commit/c97c3eb), observed at the close:
  - `go vet ./...` with the default, `e2e` and `bench` tags;
  - `go test -race ./...` (34 packages ok);
  - `TZ=Europe/Amsterdam go test ./pkg/api`;
  - `make lint` (0 issues) and `make actionlint`;
  - web vitest (735 tests), typecheck, lint and build;
  - `helm lint deploy/helm/kgateway-ui` and `REQUIRE_HELM=1 go test ./deploy/...`.
- `make bench` on [`c97c3eb`](https://github.com/DuncanDoyle/kgateway-ui/commit/c97c3eb), reference mode: all gates pass. Record: `bench/3.3.txt`.
- kind e2e, `NACK=1 CONTROLLER_LOGS=1 make e2e` on [`60e4411`](https://github.com/DuncanDoyle/kgateway-ui/commit/60e4411) (same code as `c97c3eb`): v2.4.7 / GW API v1.6.1 and v2.3.11 / v1.5.1 both pass, Go ok and Playwright 60 passed each.
- Real-cluster walkthrough of the analyzer and log signals in Task 72 (ui-loop run dir outside the repo).

### Benchmarks (Large tier)
Record: `bench/3.3.txt`, one Apple M5 Max, 18 CPUs (the D-044 reference machine). The measurement world is Large, contradicting, worst case, with every 3.2 and 3.3 input and the log reader at 50 lines/s.

| Measure | 3.2 | 3.3 | Budget |
|---|---|---|---|
| Warm recompute median, consistent / contradicting / worst | 34.4 / 44.4 / 45.0 ms | 35.5 / 46.7 / 51.3 ms | ≤ 150 ms |
| Cold recompute median, consistent / contradicting / worst | 49 / 52 / 55 ms | 52 / 56 / 62 ms | ≤ 1 s |
| Retained heap over 2.1, consistent / contradicting / measurement world | +23.3 / +27.2 / +30.8 MiB | +23.4 / +27.4 / +34.6 MiB | ≤ 40 MiB (D-054); ≤ +24 MiB over 3.1 (+10.2) |
| 3.2 watches' retained state, measurement world | 1,450,136 B | 1,450,520 B | ≤ 1,600,000 B |
| Dense: retained / cold / per warm | 160 / 207 / 166 MiB | 160 / 207 / 166 MiB | 176 / 246 / 185 MiB |
| Analyzer stage, §16.3 scope, measurement world: warm / memoized / new scrape each run | 3.76 / 0.21 / 1.14 ms | 6.00 / 0.20 / 1.28 ms | ≤ 20 / 2 / 2 ms |
| Analyzer stage with 018, measurement world: warm / memoized | — | 6.47 / 3.93 ms | reported |
| Stream CPU, annotation-only, consistent / contradicting (median of 5) | 6.5 / 8.0 % | 6.6 / 9.5 % | ≤ 15 % |
| Spec-edit stream CPU, consistent / worst | — | 10.0 / 13.9 % | ≤ 25 % |
| Edit-driven recomputes, worst | ≤ 1/s | 0.98/s | ≤ 1/s (D-047) |
| OS memory during the stream, median of 5, worst | — | 209.5 MiB | ≤ 288 MiB (D-043) |
| `…/findings` p95 sequential / 20 clients, worst mix (`view=groups&q`) | — | 0.52 / 1.31 ms | < 50 / < 100 ms |
| `…/diagnostics` p95 sequential / 20 clients, measurement world | — | 0.85 / 5.01 ms | < 50 / < 100 ms |

## Deferred / open
- R96: a scrape that changes only the status-sync count publishes a new model (0.10/s idle) with no finding change. Revisit in 3.4.
- 018 runs outside the gated analyzer-stage scope; inside it, the memoized allocation row would exceed its budget.
- §8.9: CDS- and LDS-rejected names never appear in proxy stats, so on real clusters those rejects keep only the error class (D-055). The keep-name path is unit-tested only.
- The top nav clips its first item at 720 px; this predates 3.3.
- Carry to 3.4: R57, R59; Task 77 port fixes and Service explanations; the 023/024 docs entries.
- Program: E3 and E8 delivered in this branch; the module tag kgwctl needs (#16) follows landing on `main` (user: after the 3.3 close).
