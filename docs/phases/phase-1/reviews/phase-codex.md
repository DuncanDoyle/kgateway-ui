Cross-model: YES (author: Claude Opus 5.5, reviewer: Codex, command: codex exec --sandbox read-only)
Scope: phase 1
Diff: 560c393..acee415
Timing: on time

Reviewed `560c393..acee415` against the phase records. No files changed or tests run. kgateway claims below were checked against the local source, including the supported version tags.

1. **P1 — Scaled-to-zero workloads are reported ready.** [pkg/health/checks.go:142](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/health/checks.go:142) treats `readyReplicas >= desired` as success, including `0 >= 0`. A managed proxy scaled to zero can therefore leave the install healthy; a controller scaled to zero misses the red KGWUI-005 promised by D-015. The overview repeats this predicate. The supposedly covering test at `pkg/api/api_test.go:815` uses `worldWith(0, 1)`, which still sets **desired controller replicas to one**, so it never exercises scaling to zero.

2. **P1 — ListenerSet failures never enter health.** [pkg/health/checks.go:264](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/health/checks.go:264) adds owned ListenerSets to the policy-ancestor ownership map but never evaluates their conditions or listeners. A ListenerSet with current `Accepted=False` or `Programmed=False` can coexist with an entirely healthy report. kgateway’s `rejectDeniedListenerSets` writes these failures specifically on the ListenerSet; they need not make the parent Gateway fail. This hides an observed configuration failure under D-006.

3. **P1 — A missing managed proxy passes its required check during grace.** [pkg/health/checks.go:441](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/health/checks.go:441) suppresses the missing-proxy finding during grace, then returns an OK check at line 452. Restart the UI while an existing, status-green Gateway has no proxy Deployment: its generation receives fresh grace, and the install can show **healthy for 60 seconds** despite the absent proxy. Grace should suppress premature warnings while keeping this required check pending/unknown. `TestMissingProxyGraceStartsAtFirstSeen` only asserts absence of KGWUI-006, so it permits this false green.

4. **P1 — Duplicate selector environment entries are not consistently last-wins.** [pkg/install/scope.go:127](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/install/scope.go:127) returns unknown immediately upon encountering `valueFrom`. If a later entry with the same name supplies a literal selector, Kubernetes uses that later value, but the UI remains unknown indefinitely. This contradicts the explicit D-009 last-wins contract and can also change resource assignment through the unknown-scope ownership rules. Select the final matching entry before interpreting it.

5. **P1 — Scope verification confuses served kinds with controller-enabled kinds.** [pkg/scopeverify/verify.go:169](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/scopeverify/verify.go:169) counts every served route and ListenerSet kind. kgateway does not necessarily watch that population: on v2.3.11, TCPRoute collection is empty when experimental features are disabled; legacy XListenerSets are similarly gated on both supported versions. Installing those CRDs and creating objects produces a persistent KGWUI-007 even when namespace selection agrees exactly. An unrelated controller’s experimental resources can therefore turn this install yellow. The verifier needs a comparable population, or must explicitly report that comparison unavailable.

6. **P2 — ReferenceGrant and CRD permissions are unnecessary.** [deploy/helm/kgateway-ui/templates/clusterrole.yaml:9](/Users/ddoyle/Development/claude/kgateway-ui-claude/deploy/helm/kgateway-ui/templates/clusterrole.yaml:9) grants ReferenceGrant reads, and line 18 grants cluster-wide CRD reads. Neither is read by production code. `kinds.Resolve` uses API discovery endpoints, which do not require permission to read CRD objects. These grants expand the ServiceAccount’s authority without enabling functionality. This resolves the question deliberately left open by ruling PF11: remove both grants.

7. **P2 — The YAML view does not follow the documented polling contract.** [web/packages/shell/src/api/hooks.ts:56](/Users/ddoyle/Development/claude/kgateway-ui-claude/web/packages/shell/src/api/hooks.ts:56) calls `useSWR` without the live refresh options used for health and overview. While an operator watches the YAML page, controller status changes can remain invisible indefinitely until another revalidation trigger occurs. The page also has no age indicator. Design §6.6 promises five-second polling; this matters particularly when watching a broken resource recover.

8. **P2 — The documented downstream entry point exits without serving.** [README.md:95](/Users/ddoyle/Development/claude/kgateway-ui-claude/README.md:95) passes `app.Options{UI: ui}` to `app.Run`. Both intervals remain zero, and `app.Build` rejects them at `pkg/app/app.go:76`. The example then discards that error. Supply valid intervals and handle the returned error so the advertised extension example actually starts.

9. **P3 — Most API tests do not independently pin the frozen JSON contract.** [pkg/api/api_test.go:214](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/api/api_test.go:214) decodes responses into the same production DTO types used to encode them. For example, renaming `Finding.Detail`’s JSON tag changes both sides of these tests, while frontend tests consume independent mocks. Existing raw-JSON assertions cover selected platform fields, but do not lock the complete v1 representation. Add independent wire-format assertions or contract fixtures for the released fields, including omission/null behavior, before H11 freezes them.
## Triage
Each finding was checked by a separate verifier against the code, the decisions and kgateway v2.3.11/v2.4.7; findings 1–4 and 8 were reproduced with scratch tests.

| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | Workload at 0 of 0 replicas counts as ready | reviewer | accept: Q9 (P1) | Controller at zero → red KGWUI-005 (D-015). Proxy at zero is warning KGWUI-006 (user, D-017 §1). |
| 2 | ListenerSet failures never enter health | reviewer | accept: Q10 (P1) | kgateway writes `rejectDeniedListenerSets` only on the ListenerSet. Add ListenerSet to design §5.2's required-status list (D-006). |
| 3 | Missing proxy passes its check during grace | reviewer | accept: Q11 (P1) | During grace the check is pending, not OK. The KGWUI-006 row in §5.2 says so. |
| 4 | Duplicate selector env not last-wins with `valueFrom` | reviewer | accept: Q12 (P1) | Pick the last matching entry first (D-009 §2). |
| 5 | Scope verifier counts kinds the controller may not watch | reviewer | accept, narrowed: Q13 (P2) | Real only when experimental features are off; the default is on. Compare stable kinds only; enabled kinds from the upstream endpoint later (user, D-017 §4). |
| 6 | ReferenceGrant and CRD reads unused | reviewer | accept: Q14 (P2) | Remove both; update design §8. Supersedes ruling PF11 (D-017 §5). |
| 7 | YAML view does not poll; no age | reviewer | accept polling: Q15 (P2) | Age via a response header, body unchanged (user, D-017 §6). |
| 8 | README extension example exits at once | reviewer | accept: Q16 (P2) | Set the intervals and handle the error. |
| 9 | API tests don't pin the v1 wire format | reviewer | accept: Q17 (P3) | Golden JSON fixtures before the first release (H11). |
