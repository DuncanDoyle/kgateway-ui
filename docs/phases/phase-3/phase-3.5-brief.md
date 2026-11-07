# Sub-phase 3.5 — Stabilization

Kind: stabilization
Base: 662ecc5dc4399ff0c896744d8459145facdd01a9
Issues: none (the queue in `docs/phase-status.md` is the tracker) | Items: S1–S5 from `reviews/phase-codex.md` #2–#6; S7–S9 from D-058 items 7, 10 and 11 (user, 2026-11-07). S6 closed without code (D-059).

## Problem / goal
Close the accepted Phase 3 review findings and the three D-058 revisions before the phase closes.

| Item | What the user observes today |
|---|---|
| S1 (P2) | With a rule set that maps Envoy names to objects (`computed.EnvoyNamer`), a viewer who may see a Gateway and its Pods but not a mapped Service still gets that Service's Envoy name in KGWUI-001's proxy-stat evidence. Log events already filter it. |
| S2 (P2) | Adding or removing only a list-only kind (DirectResponse, GatewayExtension, ReferenceGrant) after start raises KGWUI-008 and fails the required "served kinds" check, so a healthy install turns unknown. |
| S3 (P2) | A proxy with a KGWUI-024 error shows healthy on `/processes`, `health=error` leaves it out, the diagnostics data-plane summary has no withheld count, and the overview Gateway row's "Data plane" shows ✓. |
| S4 (P3) | The resource-status check reads Services (013's explanations) without declaring them; it stays correct only because the check also declares `Clock`. |
| S5 (P4) | KGWUI-023 attribution is quadratic in routes per Service port: 260 ms at 10,000 routes on one port, healthy Services included. |
| S7 (P3) | On 2.5, a policy whose `targetSelectors` match nothing shows KGWUI-003 "no status from the controller" instead of "the selector matches nothing". |
| S8 (P3) | KGWUI-018 shows a "Status syncs, last 5 min" count that the user does not find useful (and 2.5 does not export). |
| S9 (P3) | A controller whose version cannot be read runs on the 2.4 rules; the UI is positioned on 2.5. |

## Expected behaviour

| Input / action | Expected |
|---|---|
| S1: namer maps an Envoy cluster name in proxy-stat evidence to Service `s`; caller may view the Gateway and Pods but not `s` | the name is absent from `proxyStats[].types[].envoyResources` in `/health`, finding detail and diagnostics; a caller who may view `s` sees it |
| S1: name the namer does not map | shown as today |
| S2: served set differs from the watched set only in list-only kinds | no KGWUI-008; "served kinds" check OK, with a detail naming the change and that a restart lists them |
| S2: a judged kind changes (with or without list-only ones) | KGWUI-008 and a failed check, as today |
| S3: Gateway with a KGWUI-024 | its proxy set's health on `/processes` is the 024's severity at worst; `health=error` includes an error 024 |
| S3: diagnostics | `dataPlane.withheld` counts Gateways with a 024 error, under the same visibility rule as `rejecting` |
| S3: overview Gateway row with a 024 error and no 001 | `dataPlane: "withheld"`; 001 still wins |
| S4: Service added or its port changes, nothing else | the resource-status check reruns (its declared inputs change), also with `Clock` off |
| S5: 10,000 routes on one Service port | 023 attribution is linear: within 2× the 1,000-route time per route; results unchanged |
| S7: 2.5 rule set, policy with only `targetSelectors`, every one matching nothing, after grace + 30 s | KGWUI-016 warning, labelled computed, detail "matches nothing" / "empty selector"; it replaces 003. Named `targetRefs` keep the controller's `TargetNotFound` path only |
| S8: any version | no status-sync count in 018 evidence, the SPA, or docs |
| S9: controller version unreadable (tag and build_info) | rule set `kgateway-2.5`, note "Controller version unknown; using the 2.5 rules" |

## Root cause (if known)
- S1: `pkg/api/diagnostics.go` copies `ProxyStats.Types[].EnvoyResources` as is; `pkg/health/proxystats.go` records only Pod dependencies.
- S2: `pkg/kinds/monitor.go` diffs every served descriptor, list-only ones included; `kindsCheck` treats any difference as 008.
- S3: `processCodes` in `pkg/api/processes.go` lacks 024; `dataPlane()` and the overview row read only 001.
- S4: `conditionsCheck.Inputs()` omits the Services kind.
- S5: `pkg/health/endpoints.go` scans the route slice per backend ref, before checking readiness.
- S7: `kgw25` has no `PolicyTargeter`.
- S8, S9: user decisions (D-058 items 10, 11).

## Acceptance tests
At the API boundary (`pkg/api` handlers over an engine model) for S1, S3 and S9; `health.Evaluate` for S2, S7 and S8 (S2 through the real `kinds.Diff`, S8 through `ctrlmetrics.Parse`); the declared-input guard for S4; a benchmark plus a route-listed-once test for S5. Each fails before its change, except S5's, which is a guard.

## Scope
In: `pkg/api`, `pkg/health`, `pkg/kinds`, `pkg/computed/kgw25`, `pkg/computed/rulesets`, `pkg/ctrlmetrics`, `pkg/engine`, `web/packages/shell` (024 data-plane value, sync count removal), docs (design, plan, findings, README, decisions).
Out: live re-watch of list-only kinds; any change to 024's triggers; the upstream selector status (kgateway#14998).

## Plan / design impact
- design §7.6 (S7), §10 (S2), §13 wire shapes (S3: `withheld`, `dataPlane: "withheld"`), §10.2 (S8); computed-rules §2 (S9).
- decisions: D-058 items 7, 10, 11 already record S7–S9.
- S8 removes exported `ctrlmetrics` names (`StatusSyncsTotal`, `PodMetrics.StatusSyncs*`): README upgrade note.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite passes
- [ ] cross-model review of `git diff 662ecc5dc4399ff0c896744d8459145facdd01a9..HEAD` + triage (stabilization)
- [ ] phase-3.5-complete.md written; phase-status.md updated
