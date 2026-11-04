# Phase 3.2 — Data plane, processes and the overview (Complete)

**Completed:** 2026-11-04
**Range:** 8bc1bae..HEAD (branch `duncandoyle/phase3Seams`)

## Summary
3.2 turns the install page into a ranked health overview. It shows xDS rejects per Gateway and the health of the controller and proxy processes.
- **Overview:** a verdict, up to 10 ranked groups with next steps, and Processes lines for the controller and the proxies. The skeleton reserves the loaded block from the install summary, so loading shifts nothing (D-045).
- **KGWUI-001 re-specified:** one finding per Gateway, the rejected types as evidence, a `since` and a 60 s restart hold (D-030). Rows on a rejecting Gateway are never healthy (`gateway-rejecting`).
- **New codes:** 019 process-restarting, 020 probe-failing, 021 rollout-stuck, 022 version-skew, 025 proxy-config-warming and 026 proxy-disconnected, all warnings.
- **Inputs:** watches on proxy Pods, Warning Events and Gateway API CRD metadata, and Envoy stats from each proxy. Each has a toggle and reads "not available" with a reason, never unknown (D-034).
- **Pages:** a Processes page, and Data plane and Proxies sections on the Gateway page. The overview's Gateway table gains a Data plane column.
- **API:** `GET …/diagnostics` and `GET …/processes`; `dataPlane` and `proxies` on the Gateway detail; `groupRows` on the install summary.
- **Seams:** `process.Source` (D-029), a replaceable `proxystats.Source` (D-037), `extension.WithCommands` (D-051), and `renderWhenFontsReady` for apps that embed `Shell`.

## Sub-phases (phase record only)
n/a

## Reviews
- **Task reviews:** Tasks 35–53 each had a spec and quality review, with fix rounds confirmed by scoped re-reviews.
- **Cross-model, two Codex rounds** (Codex reviewed Claude's work):
  1. `reviews/3.2-codex.md` (`8bc1bae..8e7f177`): 7 findings (2 P1, 4 P2, 1 P3). 6 accepted and fixed; #4 with revised scope (D-047). #3 rejected per R49 (D-048). The user decided #4–#7 (D-045, D-046, D-047).
  2. `reviews/3.2-codex-rereview.md` (`0720984~1..11f8c45`): the six accepted findings resolved; one new P3, accepted and fixed in [`e4416b8`](https://github.com/DuncanDoyle/kgateway-ui/commit/e4416b8).

## Wording pass
Done: `phase-3.2-wording.md`. 167 strings (S304–S470), five options each, chosen by a separate picker; 40 changed. R66 settles the picker's doubtful entries.

## Waivers
None.

## Deliverables
- Acceptance tests failed before and passed after, per the task reports (Tasks 35–54, in the SDD workspace).
- Standard gates on [`e4416b8`](https://github.com/DuncanDoyle/kgateway-ui/commit/e4416b8), observed at the close: `go vet ./...` and `-tags e2e`, `go test ./... -race` (32 packages ok, the downstream example included), `TZ=Europe/Amsterdam go test ./pkg/api`, `make lint` (0 issues), web vitest (701 tests), typecheck, lint and build, `helm lint` and `REQUIRE_HELM=1 go test ./deploy/...`: all pass.
- `make bench` on [`ce9983b`](https://github.com/DuncanDoyle/kgateway-ui/commit/ce9983b), reference mode: all gates pass. The spec-edit stream reran with the D-047 gate of [`11f8c45`](https://github.com/DuncanDoyle/kgateway-ui/commit/11f8c45). Later commits change tests only. Record: `bench/3.2.txt`.
- kind e2e, `NACK=1 make e2e` on v2.4.7 and v2.3.11, last ran in Task 53 on [`9e5f8f0`](https://github.com/DuncanDoyle/kgateway-ui/commit/9e5f8f0): Go ok (6 tests, 1 skip as designed), Playwright 50 passed.
- The Task 54 fixes ran on kind in CI ([run 41870029468](https://github.com/DuncanDoyle/kgateway-ui/actions/runs/41870029468), [`fd4fe3c`](https://github.com/DuncanDoyle/kgateway-ui/commit/fd4fe3c), throwaway branch `duncandoyle/bench32`, `NACK=1`): all three rows (v2.4.7 with `--poll-interval=10m`, v2.3.11, v2.4.7 with `SECOND_INSTALL=1`) passed 5 Go tests and 54 Playwright tests each. A local run was not possible: the Docker VM hit its inotify limit (128) with two other sessions' clusters up.
- `web/e2e/needs-attention.spec.ts` stubs the API with `page.route`. It ran 4/4 three times on the Vite dev server and once on the production bundle. At 720 px with web fonts, a collapsed row's content is 70 px in its 88 px row: 2 px headroom by the spec's measure.
- CI bench: [run 41804175589](https://github.com/DuncanDoyle/kgateway-ui/actions/runs/41804175589) on [`27e2b62`](https://github.com/DuncanDoyle/kgateway-ui/commit/27e2b62), throwaway branch `duncandoyle/bench32`: success. A/A [run 41810366248](https://github.com/DuncanDoyle/kgateway-ui/actions/runs/41810366248): 13 benchmarks within −2.1 % to +3.5 %. After the Task 54 fixes, [run 41870035617](https://github.com/DuncanDoyle/kgateway-ui/actions/runs/41870035617) on [`fd4fe3c`](https://github.com/DuncanDoyle/kgateway-ui/commit/fd4fe3c) passed with the D-046 and D-047 gates; the timing regression gate against `27e2b62` stayed within −1.7 % to +3.6 %. Record: `bench/3.2-ci.txt`.
- Evidence: `phase-3.2-evidence.md`.

### Benchmarks (Large tier)
Records: `bench/3.1.txt` (on [`4997354`](https://github.com/DuncanDoyle/kgateway-ui/commit/4997354)) and `bench/3.2.txt`. Local runs on one Apple M5 Max, 18 CPUs, the D-044 reference machine. The measurement world is Large, contradicting, worst case, with every 3.2 input. 3.1 ran each stream once; 3.2 runs five per world and reports range and median.

The gates changed during 3.2:
1. D-043: OS memory is the median of five, ≤ 288 MiB (was ≤ 256 MiB, one run).
2. D-044: time budgets gate on the reference machine only. CI gates deterministic measures, and timing against the base in the same job.
3. D-046: the new watches' retained state is its own heap part, ≤ 1,600,000 B.
4. D-047: the spec-edit stream gates edit-driven recomputes ≤ 1/s; deadline runs are logged.

| Measure | 3.1 | 3.2 | Budget |
|---|---|---|---|
| Warm recompute median, consistent / contradicting / worst | 34.3 / 42.2 / 43.3 ms | 34.4 / 44.4 / 45.0 ms | ≤ 150 ms |
| Cold recompute median, consistent / contradicting / worst | 50 / 52 / 55 ms | 49 / 52 / 55 ms | ≤ 1 s |
| Retained heap over 2.1, consistent / contradicting / measurement world | +23.0 / +27.8 MiB / — | +23.3 / +27.2 / +30.8 MiB | ≤ 32 MiB; ≤ +24 MiB over 3.1 |
| 3.2 watches' retained state, consistent / contradicting / measurement world | — | 1,392,952 / 1,392,984 / 1,450,136 B | ≤ 1,600,000 B |
| Dense: retained / cold / per warm | 160 / 207 / 166 MiB | 160 / 207 / 166 MiB | 176 / 246 / 185 MiB |
| Analyzer stage, §16.3 scope (R35), Large: warm / memoized / new scrape each run | — | 0.08 / 0.03 / 0.03 ms | ≤ 20 / 2 / 2 ms |
| Analyzer stage, §16.3 scope, measurement world: warm / memoized / new scrape each run | — | 3.76 / 0.21 / 1.14 ms | ≤ 20 / 2 / 2 ms |
| Analyzer stage, whole, warm / memoized, Large / worst | 2.0 / 8.1–8.2 ms (one figure per world) | 1.87 / 1.85 ms; 11.51 / 10.3 ms | reported |
| Stream CPU, annotation-only, consistent / contradicting (60 s) | 6.9 / 9.5 % | 6.1–6.7 (6.5) / 7.3–8.0 (8.0) % | ≤ 15 % |
| Stream CPU, spec-edit 60 s, Large / measurement world | 9.9 % / — | 8.3–8.6 (8.5) / 9.7–11.5 (9.9) % | ≤ 25 % |
| Stream CPU, spec-edit 10 min, Large / measurement world | 13.2 % (1.33/s) / — | 11.2–12.5 (11.5) / 12.8–14.1 (13.8) % | ≤ 25 % |
| Spec-edit recomputes, 60 s: edit-driven | 0.93/s (total) | 0.93–0.95/s | ≤ 1/s |
| Spec-edit recomputes, 10 min: edit-driven / deadline / total | — / — / 1.33/s | 0.51–0.60 / 0.70–0.81 / 1.25–1.38/s | ≤ 1/s edit-driven; rest logged |
| OS memory, annotation, consistent / contradicting | 175.3 / 196.2 MiB | median 159.5 / 171.5 MiB | median ≤ 288 MiB |
| OS memory, spec-edit 60 s, Large / measurement world | 200.0 MiB / — | median 175.8 / 264.0 MiB | median ≤ 288 MiB |
| OS memory, spec-edit 10 min, Large / measurement world | 204.3 MiB / — | median 187.5 / 244.1 MiB | reported |
| Idle 10 min recomputes | 20 | 20 | 20, all on the safety timer |
| Burst p95, event to publication | 1.004 s | 1.003 s | ≤ 1.2 s |
| `GET …/findings`, worst mix p95, sequential / 20 clients | 0.56 / 1.21 ms (`q`, restricted) | 0.59 / 1.37 ms (`q`, restricted) | < 50 / < 100 ms |
| `GET …/diagnostics` p95, sequential / 20 clients (restricted) | — | 0.86 / 4.85 ms | < 50 / < 100 ms |
| `GET …/processes` p95, sequential / 20 clients (restricted) | — | 0.30 / 1.21 ms | < 50 / < 100 ms |

CI (linux/amd64, 4 CPUs). Since D-044, CI time budgets are reported only, and runner speed varies run to run:

| Measure | 3.1 CI | 3.2 CI (`27e2b62`) | Budget |
|---|---|---|---|
| Warm recompute median, consistent / contradicting / worst | 83.7 / 105.5 / 113.6 ms | 104.3 / 140.6 / 144.8 ms | reported (≤ 150 ms reference) |
| Retained heap over 2.1, consistent / contradicting / measurement world | +23.0 / +26.8 MiB / — | +23.1 / +26.7 / +31.0 MiB | ≤ 32 MiB, gated |
| Dense: retained / cold / per warm | 160 / 221 / 166 MiB | 160 / 221 / 166 MiB | 176 / 246 / 185 MiB, gated |
| Stream CPU, annotation-only, consistent / contradicting | 7.9 / 10.6 % | 9.1–9.8 / 13.1–13.6 % | reported |
| Stream CPU, spec-edit 60 s, Large / measurement world | 11.7 % / — | 13.9–14.7 / 18.6–19.4 % | reported |
| Analyzer stage, memoized, new scrape each run, measurement world | — | 2.92 ms | reported (≤ 2 ms reference) |
| Findings `q`, restricted, 20 clients p95 | 62 ms | 82.7 ms | reported |
| Diagnostics, restricted, 20 clients p95 | — | 96.6 ms | reported |
| Timing A/B against `main`: Compute large / contradicting / hit, confirm tick | — | −1.5 / +10.1 / +1.2 / −4.3 % | ≤ +15 % |

CI ran before Tasks 53 and 54, so it has neither the D-046 part nor the D-047 gate.

## Release note
The install page now ranks what to fix first and shows the health of the controller and proxy processes. It reports xDS rejects per Gateway with the proxies' own evidence, and never shows a route green while its Gateway rejects config. A new Processes page and Gateway page sections show restarts, probe failures, stuck rollouts and version skew (README, Upgrade notes).

## Rulings made during execution
1. **R35:** the analyzer-stage gate measures §16.3's scope (identity, groups, rank, Phase 3 checks). If wrong: a re-measure.
2. **R36:** new 3.2 checks declare narrow inputs and use `WakeAt`; `Clock` only when `Run` reads the time beyond deadlines. If wrong: more reruns.
3. **R37:** e2e tests the restart hold by a controller Pod delete; 019 by a container restart. If wrong: one more e2e step.
4. **R38:** e2e expects 006 for an unpullable image on a new Gateway; 021 by a rollout with a short progress deadline. If wrong: one more e2e step.
5. **R39:** next-step commands through Go `extension.WithCommands`. Recorded as D-051.
6. **R40:** `/meta` capability reasons are fixed text; raw API errors only in check results. If wrong: two strings.
7. **R41:** probe type is the union across a Pod's containers. Recorded as D-049.
8. **R42:** 019 and 020 declare proxy Pod access; unobserved proxy Pods keep controller findings and name the gap. If wrong: a check result shape.
9. **R43:** 021 is skipped only when the Deployment has no ready replica. If wrong: one more 021.
10. **R44:** a terminating Pod raises no restart. If wrong: a missed 019.
11. **R45:** the ListenerSet hint decides from channel and bundle; CRD 022 is suppressed during a controller rollout. Recorded as D-049.
12. **R46:** a task's file-list overrun is ratified at its review when every extra is needed. If wrong: a stricter process.
13. **R47:** restart hold when rejects vanish within 60 s of a controller Pod change. If wrong: the hold window.
14. **R48:** `proxies` on xds-rejects evidence is omitted for self-managed Gateways. Recorded as D-048.
15. **R49:** proxy stats publish `Rejects{Seen, Since}` and names, never raw counts. Recorded as D-048.
16. **R50:** proxy Pod ownership is checked by ReplicaSet name and template hash; a full UID match needs a ReplicaSet watch. If wrong: a forged owner in the proxy namespace.
17. **R51:** the unbounded `pods/proxy` read in `pkg/ctrlmetrics` is fixed now, in its own commit. If wrong: nothing.
18. **R52:** a stats Pod the process data does not know is Ready when Envoy reports `live`. Recorded as D-048.
19. **R53:** per-cluster `update_rejected` names sit under `ClusterLoadAssignment`. Recorded as D-048.
20. **R54:** 025 and 026 `since` is the condition's onset. If wrong: a `since` value.
21. **R55:** `recentlyChanged` and `proxies` stay omitted when empty. Recorded as D-048.
22. **R56:** `diagnose.Build` copies on write at the first changed group id. If wrong: one slice copy.
23. **R57:** the 3.4 brief re-seeds Services and EndpointSlices in the measurement world. Deferred to 3.4.
24. **R58:** a flaky gate test is fixed at its root inside Task 46. If wrong: one dispatch.
25. **R59:** R50 stays for 3.2; the ReplicaSet watch moves to 3.4. Deferred to 3.4.
26. **R60:** three chart Minors parked. See Deferred.
27. **R61:** Task 48's 13 files accepted. If wrong: nothing.
28. **R62:** the server computes the process summaries' `state`. Recorded as D-050.
29. **R63:** Task 49's file overrun accepted for review. If wrong: review size.
30. **R64:** `EvidenceView` owns `findingRenderers`. Recorded as D-051.
31. **R65:** Task 50 fixes the nav and wrong states; the `/meta` gate Minor is parked. See Deferred.
32. **R66:** the picker's doubtful wording picks settled. If wrong: a few strings.
33. **R67:** the D-043 288 MiB gate replaces the brief's 256 MiB. User decision.
34. **R68:** the CI bench timeout fits the five-run streams. If wrong: longer CI runs.
35. **R69:** idle recomputes are a product bug, fixed at its root. Recorded as D-052.
36. **R70:** the memoized stage is also measured with the production metrics max age and a scrape per run. See D-044, D-052.
37. **R71:** the hold test removes the rejecting objects after the Pod delete. If wrong: a missed hold on a pure restart.
38. **R72:** during the hold the install may read error; the test asserts never healthy. If wrong: nothing new.
39. **R73:** the defects Task 53's e2e found are fixed in Task 53. If wrong: nothing.
40. **R74:** the DTO-versus-model rollout reason and the loose ≥ 60 s check are parked. See Deferred.
41. **R75:** the watches part budget is the measured median plus about 10 %. Recorded as D-046.
42. **R76:** the re-review's layout P3 gets a browser bounds test. Fixed in [`e4416b8`](https://github.com/DuncanDoyle/kgateway-ui/commit/e4416b8).

## Notes for later phases
- Retained heap has 1.2 MiB headroom in the measurement world (+30.8 of 32 MiB locally, +31.0 on CI). 3.3 and 3.4 add to it.
- The spec-edit 60 s edit-driven gate has 5–7 % headroom, by structure: the debounce runs once per second plus the wait for the next edit (D-047).
- On CI, several reported budgets sit close: warm worst 144.8 ms, diagnostics restricted 20-client p95 96.6 ms, memoized new-scrape 2.92 ms.
- `diagnostics.controllerLogs` creates its Role, but no code reads logs until the 3.3 reader (Task 67).
- `ProcessSummary` has no restart count until the restart check exposes every Pod's window (D-050).
- Program edges: E3 needs the 3.3 analyzer exported as a package under `pkg/`. E8 (kgwctl check) is proposed: it would need an exported static metrics source and the cache and engine config helper; it arrives as a cross-repo issue if the user approves.
- PR #14 put the program-block edits of `roadmap.md` and `phase-status.md` on `main`. Merge `main` into this branch before the Phase 3 PR.
- Evidence recordings are kept outside the repo.

## Deferred / open
- (P3) Retained heap headroom 1.2 MiB in the measurement world, above; in `phase-status.md`.
- (P3) R57: re-seed Services and EndpointSlices in the measurement world with the 3.4 watches — 3.4 brief.
- (P3) R59: proxy Pod ownership by UID needs a ReplicaSet watch; R50's name check stays until then — 3.4.
- Minors parked at the task reviews:
  - `/meta` gate: no exact gate for `gateway-rejecting` and Processes against a 3.1 server (R65; SPA and server ship together).
  - The endpoints RBAC is granted in 3.2, read from 3.4 (R60).
  - An empty `controllerLogsNamespaces` entry renders the Role in the release namespace (R60).
  - `xdsHoldError` and `xdsStaleError` have an internal period, against the glossary (Task 51 review).
  - One "Pod" capital in the chart README's NetworkPolicy paragraph (Task 51 review).
  - S349 reads "1 of 2 Gateways have no proxy stats" for a count of one.
  - `TestBudgetFindingsRequests`: its world has no 001, 020, 025 or 026 findings (`bench/3.2.txt`).
  - R74: the rollout reason is filtered in the DTO; `process.Rollout.Reason` still holds every Progressing reason.
  - R74: the ≥ 60 s check in `testControllerDown` measures from a `collectedAt` that can lag the scale-down.
- The Needs attention reservation has 2 px headroom at 720 px. A theme with larger font metrics can grow a row past it.
