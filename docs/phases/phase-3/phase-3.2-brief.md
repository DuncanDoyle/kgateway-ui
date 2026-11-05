# Sub-phase 3.2 — Data plane, processes and the overview

Kind: planned
Base: 8bc1bae82607a427dae805928717b482a3ceeb7a
Issues: n/a | Plan steps: implementation-plan.md Tasks 34–54; design §6, §8.1–§8.5, §8.8, §9, §13, §14 rules 13–16, §16.3, §17.1–§17.4, §18.1 (events, CRDs, proxy stats); D-016, D-029, D-030, D-034, D-037, D-039, D-040

## Problem / goal
The install page lists findings, but it does not say what to fix first. A Gateway whose proxies reject config can still show green status, and the UI does not see processes that restart, flap or run mixed versions. 3.2 adds the ranked health overview: a verdict, up to 10 groups by rank with next steps, and a Processes line. KGWUI-001 becomes one finding per Gateway with evidence, `since` and a restart hold (D-030). New watches (proxy Pods, Warning Events, Gateway API CRD metadata) and proxy Envoy stats feed 019–022, 025 and 026 through `pkg/process` (D-029) and a replaceable stats source (D-037). Every new input has a toggle and degrades to "not available" with a reason, never to unknown (D-034). Users get the overview, a Processes page and Data plane and Proxies sections on the Gateway page.

## Expected behaviour

| Input / action | Expected |
|---|---|
| Default toggles (events, CRDs, proxy stats on) | Proxy Pods, Warning Events on Pods and Gateway API CRD metadata are watched. Events of Pods outside every install are dropped at the informer. No Event message is kept (Task 35) |
| An Event arrives before the UI knows its Pod | It is dropped. Its next update after the Pod arrives sets a baseline, and later increments count (Task 35) |
| An Event first seen with count 50 | Contributes 0 and marks the Pod's history unknown; 020 then adds "earlier failures not counted" (Tasks 35, 38) |
| An aggregated Event or a series | Counted as observed deltas in a 15-minute window, by series count only, once (Tasks 35, 38) |
| More than 10,000 kept Events | The oldest go; the events check notes the cap (Task 35) |
| A toggle off | No watch starts. Dependent checks are not available with reason `off`; the capability reads `off` (Tasks 35, 36) |
| Events or CRDs watch denied, then allowed | No KGWUI-008. `cluster access` stays OK and the install state does not change. Dependent checks read `denied`, then run again after recovery (Tasks 35, 36) |
| A check is not available | Roll-up ignores it. "What was checked" shows it as not available with the reason. `cluster access` and `xDS rejects` keep `Required: true` (Task 36) |
| `/meta` | `capabilities` gains `processes`, `events`, `crds`, `proxyStats`, each only while its input runs; `capabilityStates` show `off`, `denied` or `failing` (Task 36) |
| Two installs' proxies in one namespace | Each Gateway's processes come from its own install only (Task 37) |
| Two 70-character Gateway names sharing a 63-character prefix | Each maps to its own Deployment and Pods; they never merge (Task 37) |
| A self-managed Gateway | Proxies "not observed", never failing (Task 37) |
| A Pod whose termination message holds `secret-value` | The value is absent from the model and every API body (Task 37) |
| A container restart while watched, or a termination in the last 15 min | 019 on the controller Deployment, or on the Gateway for proxies. Present at last restart + 15 min − 1 s, gone at + 1 s (Task 38) |
| Controller last exit code 2 | 019 adds the Go panic note and a `kubectl logs --previous` next step (Tasks 38, 44) |
| No ready controller or proxy replica | 005 or 006 with detail such as "0/2 ready: CrashLoopBackOff, last exit OOMKilled"; no 021 (Task 38) |
| 2 observed probe failures on a Pod in 15 min, then 3 | None, then 020 with the probe type, count and time. One probe type in the spec names it; two read "a probe" (Task 38) |
| Deployment `Progressing=False` with `ProgressDeadlineExceeded` | 021, with Pod waiting reasons as evidence. An image pull failure beside a ready old Pod without that reason is evidence only (Task 38) |
| Events off | 020 not available (`off`); the install is not unknown (Task 38) |
| Proxy 2.3.x, controller 2.4.x | 022 warning. A patch difference is evidence only. A pinned image in GatewayParameters is named as the likely cause (Task 39) |
| A controller rollout | 022 suppressed during it and 10 min after: none at 9:59, present at 10:01 (Task 39) |
| Tags `v2.5.0-rc.1` vs `v2.5.0`, `v2.4.7-acme.1`, `latest`, `@sha256:…` | No finding; parses as 2.4; "version unknown" with no finding for the last two (Task 39) |
| Two running controller versions after a settled rollout | 022 warning. "Settled" uses the same 10-minute window as proxies **(inferred)** (Task 39) |
| A Gateway API CRD bundle minor other than the expected one (2.3 → v1.5, 2.4 → v1.6, 2.5 → v1.7), or mixed bundle versions or channels | 022 warning. CRDs off: not available (Task 39) |
| A standard-channel Gateway CRD next to a ListenerSet CRD | The ListenerSet hint says which, not "may" (Task 39) |
| `rejects_active` > 0 for a Gateway on two controller replicas | One 001 per Gateway. Evidence per type: summed streams, the reporting replicas, first seen, `rejects_total` growth since first seen; proxies ready / desired (Task 40) |
| Gateway status current and `Programmed=True` while it rejects | The status-says-healthy message. Stale status: "Proxies reject this Gateway's config" (Task 40) |
| A controller Pod gets a new UID, or a higher restart count, and the reject disappears | `xDS rejects` pending and the install unknown at +59 s; healthy at +61 s. A reject back at +30 s keeps its `since` (Task 40) |
| A Gateway rejecting on the second install | Does not hold the first install (Task 40) |
| Controller metrics unavailable; proxy stats show rejects | No 001; `xDS rejects` unavailable as today (KGWUI-008, required) (Tasks 40, 42) |
| A controller scrape with an error | Its result is skipped for `rejects_total` growth: no growth from it, stored totals kept **(inferred)** (Task 40) |
| Proxy stats scrape | `GET /stats?usedonly&filter=…` on port 9091 every 10 s, at most 16 Pods at once per install. A refused port, a timeout or a body over 1 MiB → `failing`. A stat outside the filter is ignored (Task 41) |
| One proxy of a Gateway fails its scrape, the other succeeds | The failing Pod carries its error; the Gateway's stats stay available from the other **(inferred)** (Task 41) |
| A downstream `acme` stats Source with its own targets and JSON format | Feeds 026 through `/health` (Task 41) |
| Two installs | Proxy stats never cross installs (Task 41) |
| `connected_state` 0, or warming listeners or clusters above 0, on a Ready proxy | None at 59 s; 026 or 025 at 61 s, one per Gateway, Pods as evidence. A not-Ready proxy: none (Task 42) |
| Counters `(3,8)` → `(4,9)` on one Pod | Evidence "1 reject observed since the last scrape", never "rejecting now"; RDS and cluster Envoy names with a positive delta listed (Task 42) |
| `update_rejected` grows by hundreds a minute while the controller re-pushes | Shown as rejects observed since the last scrape. Never counted as new episodes; the finding's `since` and count do not change **(inferred)** (Task 42) |
| A proxy with a new UID and counters at zero | Neither a reject nor a recovery (Task 42) |
| A route, ListenerSet or policy with `True` status for a Gateway with an active 001 | Flag `gateway-rejecting`, state at least warning; an own error stays error; no finding of its own. A route on another install's Gateway is not flagged (Task 43) |
| Route list `state=gateway-rejecting` | Returns exactly the flagged rows (Task 43) |
| Data plane and Process groups | Data plane: cause the Gateway, affected the objects with current `Accepted=True` or `Programmed=True` for it. Process: cause the controller Deployment or the Gateway, members 005, 006, 019–021 (Task 43) |
| A group whose cause the caller cannot view | Severity, code and the restricted label, no affected list. Its id hashes only data a restricted row shows (D-040) (Tasks 43, 45) |
| Next steps of a data-plane group | The three §8.5 commands, naming the replica that reported the reject, values shell-quoted, with `pods/log` and `pods/portforward` needs and the secret-care note. A command that returns `""` is hidden (Task 44) |
| Objects changed before the first reject | Generation first seen 9:59 before is in, 10:01 before is out; 12 changes → the 10 newest (Task 44) |
| `GET …/diagnostics` | Verdict (state, problems, `oldestSince`), top 10 groups with the first 5 affected and `affectedTotal`, `groupTotal`, process and data-plane summaries, `checks`, `collectedAt` (Task 45) |
| `GET …/processes?health=…&q=…&skew=…&restarts=…` | Controller set and a page of Gateway proxy sets matching the filters. A bad filter value → 400 **(inferred)** (Task 45) |
| A restricted finding on `/health` and `/overview` | No `evidence`, no suggested fix, no next steps. A visible finding with a hidden dependency gets `evidenceRestricted` and no evidence (Task 45) |
| A hidden recently-changed object, a step naming a hidden object, a hidden Pod of a visible Gateway | Left out without a count; step left out; the set shows counts only (rules 14–16) (Task 45) |
| Existing goldens | Change only by optional fields; the additive test passes (Task 45) |
| Gateway detail with rejects, without, and with stats off | `dataPlane` (rejects, status contradiction, `snapshot` labelled as the controller's build, next steps, recently changed) and `proxies`; stats off reads not available (Task 46) |
| Default chart render | `events` and CRD rules present; each toggle off drops its rule and passes its flag. No Secrets, ConfigMaps, `*/proxy`, `pods/portforward` or `pods/log`. The READMEs list what the UI reads and that it reaches proxy Pods on 9091 (Task 47) |
| An app that embeds `Shell` with `renderWhenFontsReady` | Renders after fonts load, and after a font failure; long names do not shift the layout (Task 48) |
| Copy a next-step command | Copied; "Copied" is announced through an `aria-live` region; commands render as text, never HTML (Task 48) |
| Healthy install page | Slim green strip and "What was checked"; no Needs attention box (Task 49) |
| Unhealthy install page | Needs attention with up to 10 groups in rank order and "Show all" to the grouped findings page; rows keep their height while loading; stale data turns every group unknown; Data plane column shows ✓, "Rejects" or "Unknown"; not-available checks show their reason (Task 49) |
| Processes page and Gateway sections | Filters round-trip through the URL; self-managed reads "not observed"; the Proxies section hides Pod names when restricted (Task 50) |
| 720, 1280, 1600 and 3008 px, both themes | No horizontal page scroll; hover, active and focus-visible on every new control. At 720 px the top-bar nav no longer crowds the install label **(inferred)** (Tasks 48–50, 53) |
| kind, `NACK=1`, v2.3.11 and v2.4.7 | 001 on `infra/nack` with the stream count, the status-says-healthy message, the reporting replica in the log command, the rejecting proxy Pod and the Envoy resource from stats. `infra/edge` also rejects the Cluster, so 001 on `edge` is expected too **(inferred)**. `setup.sh` waits for `infra/nack` `Programmed` (Task 53) |
| kind: controller scaled to 0; unpullable proxy image; controller Pod replaced | 026 after 60 s; 021; the install unknown, not healthy, during the hold (Task 53) |

## Budgets (§16.3; dense gate per D-039)

No gate changes without the user. A broken gate stops the work, and the numbers go to the user. Starting points are 3.1 at Large (`bench/3.1.txt` local arm64, `bench/3.1-ci.txt` linux/amd64).

| Measure | 3.2 budget at Large | 3.1 local | 3.1 CI |
|---|---|---|---|
| Warm recompute median, consistent / contradicting / worst | ≤ 150 ms | 34.3 / 42.2 / 43.3 ms | 83.7 / 105.5 / 113.6 ms |
| Analyzer stage, warm, large / worst | ≤ 20 ms (gated from 3.2) | 2.0 / 8.1–8.2 ms | 5.0–5.2 / 25.0–25.8 ms ⚠ |
| Analyzer stage, memoized, large / worst | ≤ 2 ms (gated from 3.2) | 2.0 / 8.1–8.3 ms ⚠ | 5.0–5.2 / 25.1–26.2 ms ⚠ |
| Stream CPU, annotation-only, consistent / contradicting | ≤ 15 % of one core | 6.9 / 9.5 % | 7.9 / 10.6 % |
| Stream CPU, spec-edit | ≤ 25 % (gated from 3.2) | 13.2 % (10 min); 9.9 % (60 s) | 11.7 % (60 s) |
| OS memory during the stream, annotation / spec-edit | ≤ 256 MiB | 175 / 196 MiB; 204 MiB (10 min) | 173 / 181 MiB; 185 MiB |
| Retained heap over 2.1, consistent / contradicting | ≤ 32 MiB, and ≤ +24 MiB above `3.1-baseline.txt` (+23.2 / +24.4) | +23.0 / +27.8 MiB ⚠ | +23.0 / +26.8 MiB ⚠ |
| Dense: retained / cold / per warm | 176 / 246 / 185 MiB | 160 / 207 / 166 MiB | 160 / 221 / 166 MiB |
| `GET …/findings`, worst mix (`q`, restricted), p95 / p99; 20 clients p95 | < 50 / < 100 ms; < 100 ms | 0.56 / 0.61 ms; 1.21 ms | 1.0 / 1.25 ms; 62 ms |
| `GET …/diagnostics`, `GET …/processes` | p95 < 50 ms, p99 < 100 ms; p95 < 100 ms with 20 clients | new | new |
| Burst, idle, cold recompute | Phase 2 gates, unchanged (burst p95 ≤ 1.2 s, ≤ 2 recomputes; cold median ≤ 1 s) | p95 1.00 s, 1 recompute; 20 idle recomputes; cold 50–55 ms | p95 1.03 s, 1 recompute; 20 idle recomputes; cold 116–133 ms |

Two gates are tight before any 3.2 code:
1. **Memoized analyzer stage.** It fails today: memoized equals warm, because Phase 1–2 checks declare `Clock` through `AllInputs` (ruling R20). 3.2's new checks declare narrow inputs and use `WakeAt` for their windows, not `Clock`. 3.2 decides, with numbers, whether to narrow the old checks. The CI warm worst case (25 ms) also exceeds the 20 ms warm gate. The benchmark includes the Phase 1–2 checks, while §16.3 scopes the stage to identity, groups, rank and the Phase 3 checks. Both points go to the user before Task 46's interim measurement.
2. **Retained heap.** Headroom is 4.2 MiB locally and 5.2 MiB on CI in the contradicting world. The new watches, process data, proxy stats and check Stores all add to it. Task 46 Step 3 stops the work when any gate is within 2 MiB.

The proxy stats scrape (2 proxies per Gateway, in-process stub) runs inside the stream CPU and retained-heap gates.

## Carry-forwards from 3.1

| Item | Outcome in 3.2 | Task |
|---|---|---|
| NACK fixture: `edge` also rejects the Cluster on 2.3 and 2.4 (clusters reach every proxy) | e2e expectations allow 001 on `edge`, or the fixture's second reject moves to a Listener reject on `nack` only. `setup.sh` waits for `infra/nack` `Programmed` | 53 |
| Proxy `update_rejected` grows by hundreds a minute while the controller re-pushes | Proxy stats deltas read as rejects observed since the last scrape, never as new episodes. On 2.5, `rejects_total` counts episodes | 42 |
| `ctrlmetrics.TotalGrowth` forgets Pods absent from an empty result | Callers skip results with `Err` | 40 |
| Diagnose families' `Affected` copies whole findings per group | An index-based form is considered before the Data plane and Process families land. Group ids on restricted rows keep D-040 when a family's cause is not the finding's own resource | 43 |
| Scale world: proxy Pods share one PodIP and a missing ReplicaSet owner; Events set only `LastTimestamp`; long-named Gateways lack status and a proxy Deployment | Fixed where the 3.2 budgets depend on them: distinct Pod IPs for the stats stub, Event counts and times for the deltas, long names that map to a proxy Deployment | 46, 52 |
| Top bar crowds at 720 px | The nav and install label do not touch at 720 px | 49–50 |
| `@tanstack/react-virtual` unused in the shell and app packages | Removed with the next dependency change | 48 |

## Root cause (if known)
n/a (planned work).

## Acceptance tests
- Task 35: `TestEventsTransformKeepsOnlyInstallPods`, `TestEventBeforePodIsKeptOnNextUpdate`, `TestFirstSeenCountIsUnknown`, `TestSeriesCountedOnce`, `TestEventTransformKeepsNoMessage`, `TestEventsCap`, `TestCRDMetadataKeepsGatewayGroups`, `TestToggleOffStartsNoWatch`, `TestDeniedWatchReportsAccess`, `TestDeniedOptionalWatchNeverUnknown`.
- Task 36: `TestNotAvailableNeverUnknown`, `TestRequiredChecksKeepTheirFlag`, `TestMetaCapabilityStatesFromToggles`.
- Task 37: `TestProxyPodsBelongToOneInstall`, `TestLongGatewayNameProxyPods`, `TestSelfManagedIsNotObserved`, `TestTerminationMessageNeverRead`, `TestNilFieldsMeanNotObserved`.
- Task 38: the boundary tests of the plan through `/health`, with `TestAggregatedEventCountIsAWindowNotATotal`.
- Task 39: the skew tests of the plan, with `TestSkewUnparseableAndPrereleaseTags`.
- Task 40: per-type evidence over two replicas, the status-says-healthy message, `TestRestartHoldKeepsInstallUnknown`, second-install isolation; plus a scrape with `Err` adding no growth.
- Task 41: parsing the Task 20 captures, `TestDownstreamProxyStatsSource`, the `failing` cases, the concurrency bound, `TestProxyStatsPerInstall`.
- Task 42: 025 and 026 at 59 s and 61 s, not-Ready, per-Pod deltas, `TestNoRejectFindingFromProxyStatsAlone`, `TestProxyStatsResetIsNotAReject`.
- Task 43: `TestRowNeverHealthyWhileGatewayRejects`, the list filter, group affected lists, isolation.
- Task 44: `TestCommandsQuoteValues`, `TestStepsHiddenWhenCommandEmpty`, `TestRecentlyChangedWindowAndCap`.
- Task 45: `TestTypedEvidenceRestrictedOnHealthAndOverview`, `TestGroupsAndNextStepsHideHiddenObjects`, `TestDiagnosticsTopTen`, `TestProcessesFilters`, `TestProcessSetCountsOnlyWhenPodHidden`, the dependency rows for 001, 019–022, 025, 026, the new goldens and the additive test.
- Task 46: Gateway detail with rejects, without, with stats off, restricted Pods; the interim measurement (`bench/3.2-interim.txt`).
- Task 47: `TestEventsRuleFollowsToggle`, `TestCRDRuleFollowsToggle`, `TestTogglesPassedAsFlags`, `TestClusterRoleIsReadOnlyWithoutSecretsOrProxy`.
- Tasks 48–50: the component, install page and Processes page tests of the plan.
- Task 52: `TestBudgetAnalyzerStage`, the spec-edit and annotation streams with OS memory, `…/diagnostics` and `…/processes` latency, `TestBudgetHeap`, `TestBudgetDenseMemory`, every other gate.
- Task 53: `TestDataPlaneDiagnostics` on kind (v2.3.11, v2.4.7); `web/e2e/diagnostics.spec.ts`.

## Scope
In: `pkg/cache` (proxy Pods, Events, CRD metadata), `pkg/health` (availability, 001, 019–022, 025, 026, evidence, messages), `pkg/process`, `pkg/proxystats`, `pkg/computed` (Gateway API version table, `EnvoyNamer`, nil in `kgw23` and `kgw24`), `pkg/install` (`ProxyPodDetector`), `pkg/extension` (`WithProcessSource`, `WithProxyStatsSource`, `WithCommands`), `pkg/engine` (process data), `pkg/explore` (`gateway-rejecting`), `pkg/diagnose` (families, next steps), `pkg/ctrlmetrics` (snapshot resources per Gateway), `pkg/api` (diagnostics, processes, Gateway detail, capabilities, goldens), `pkg/app`, `cmd/kgateway-ui` (flags), `internal/boundary`, `internal/testutil` (scale world fixes), the chart and both READMEs, `examples/downstream`, `web/packages/shell` (components, install page, Processes page, Gateway sections, `renderWhenFontsReady`), `web/app/src/main.tsx`, `test/e2e` and `web/e2e`, `.github/workflows` (bench paths, `NACK=1`).
Out: the analyzer codes 003 kinds, 013–018 and the code migration (3.3); controller log signals and `pods/log` (3.3); Services, EndpointSlices, 023 and membership stats (3.4); 024 and the 2.5 row (3.4, after GA); a verified Envoy namer without per-version fixtures; ReplicaSet watches; proxy logs, admin ports and `/config_dump`; gate changes of any kind.

## Plan / design impact
Task 54 changes `design.md` only where the code deviates. Likely candidates: §8.5 says commands come from the SPA `Vocabulary`, the plan uses a Go `Commands` seam; §16.3's analyzer-stage scope against what the benchmark measures. Approval of the plan covers the ⚠ guardrail tasks 35, 37, 38, 41, 45, 48, 49 and 50 within their file lists (ruling 2026-11-01, user approved the entire phase). A task that needs more files stops and asks.

Plan points to settle in the owning task:
1. Task 40 declares `Clock` for `xDS rejects`, as the check does today. The hold needs only `WakeAt`; narrowing it helps the memoized gate.
2. Task 53 expects 019 from `kubectl delete pod` of the controller. A replacement Pod starts with 0 restarts and no termination, so §9.3 gives the hold, not 019 **(inferred)**. A container restart gives 019.
3. Task 53 expects 021 from an unpullable image. §9.3 needs `ProgressDeadlineExceeded` beside a ready old Pod, and the default deadline is 600 s. A new Gateway with the bad image gives 006 instead **(inferred)**.
4. Plan line references drifted with 3.1: the `xDS rejects` body is at `checks.go:666-704`, 005 at `137-158`, 006 at `579-664`, `CheckResult` at `finding.go:98-111`, `visibleHealth` at `handlers.go:156`. `InputSet.Process` exists already.

Reminders for the user (not blocking): the open item in design §25 (filing the reject-status draft), and the upstream scope-endpoint issue (carry-over P3). Filing both stays the user's call.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] standard gates pass; the 3.2 budgets above hold (`bench/3.2.txt`, `3.2-ci.txt`), or the numbers went to the user
- [ ] kind e2e passes on v2.3.11 and v2.4.7 with `NACK=1`; `/ui-loop` on the install page, Processes and the Gateway sections
- [ ] wording pass recorded in `phase-3.2-wording.md`
- [ ] Codex review of `git diff 8bc1bae..HEAD` + triage (`reviews/3.2-codex.md`)
- [ ] phase-3.2-complete.md written (bench table vs 3.1); phase-status.md and roadmap.md updated; README upgrade notes (001 re-specified, new codes, new watches and toggles, `since` resets on restart)
