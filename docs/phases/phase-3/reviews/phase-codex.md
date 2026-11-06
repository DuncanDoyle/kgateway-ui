# Review: Phase 3 (Health & Diagnostics) — Codex

Cross-model: YES (author: Claude Opus 5.5 subagents, reviewer: Codex, command: `codex exec --sandbox read-only`)
Scope: phase 3
Diff: 544dcd5..3d7698e
Timing: on time
Date: 2026-11-06
Note: link targets changed from local absolute paths to repo-relative ones; the text is otherwise verbatim. Each finding was verified with a reproduction test or a measurement (SDD workspace `phase-review-verify.md`).

Found **4 P1, 1 P2 and 1 P4 findings** in `544dcd5..3d7698e`.

1. **P1 — Group IDs disclose the hidden policy behind KGWUI-015.**  
   [pkg/diagnose/group.go:329](../../../../pkg/diagnose/group.go:329) takes the policy parsed from status as the group cause; line 138 hashes that identity. Findings retain this group ID when their resource or evidence is restricted.

   This is directly verifiable: `GroupID("kgw.kgateway", "Replaced by policy", TrafficPolicy shop/auth, nil)` produces `f1-vmugvx4eo3s4dvlxqwh4vvjdha`, exactly the value in the restricted-policy golden. A caller can therefore confirm guessed policy identities despite the evidence being hidden. The policy need not appear in the route’s spec. This violates §14 rule 11/D-040 across `/health` and both findings endpoints; ordinalizing the finding ID did not protect its group ID.

2. **P1 — Proxy-stat evidence bypasses authorization of mapped Envoy names.**  
   [pkg/api/diagnostics.go:153](../../../../pkg/api/diagnostics.go:153) copies `ProxyStats.Types[].EnvoyResources` unchanged. The same function filters mapped names in `LogEvents` at line 171.

   With a registered `computed.EnvoyNamer`, a viewer allowed the Gateway and Pods but denied the mapped Service still receives its Envoy resource name through proxy-stat evidence. [pkg/health/proxystats.go:250](../../../../pkg/health/proxystats.go:250) records only Pod dependencies. Thus `/health` and finding detail disclose evidence that §14 rule 12 requires filtering. This affects the existing downstream seam and the planned Phase 9 mapper integration.

3. **P1 — Optional list-only kind changes still raise KGWUI-008 and make installs unknown.**  
   [pkg/kinds/monitor.go:45](../../../../pkg/kinds/monitor.go:45) includes every served descriptor in its comparison, including `ListOnly` kinds. Any resulting difference reaches [pkg/health/checks.go:116](../../../../pkg/health/checks.go:116), which emits KGWUI-008 and a failed required check.

   Adding or removing only DirectResponse, GatewayExtension or ReferenceGrant after startup therefore makes an otherwise healthy install unknown. The 3.4 readiness fix excludes these from cache readiness and `cluster access`, but misses this second gate. D-034/D-058’s optionality ruling is incompletely implemented. Fixing the health classification does not require implementing the deferred live re-watch feature.

4. **P1 — KGWUI-024 does not reach the earlier process/data-plane summaries.**  
   [pkg/api/processes.go:29](../../../../pkg/api/processes.go:29) includes 001, 025 and 026 in process health but omits 024. Ready proxies with an active withheld-config error therefore appear healthy in `/processes`; `health=error` excludes them, and diagnostics can show a healthy proxy summary alongside the error finding.

   The Gateway data-plane block similarly reads withheld evidence only from the earlier log-derived path; [pkg/api/handlers.go:255](../../../../pkg/api/handlers.go:255) still derives its data-plane value solely from 001. The 3.4 signal detects “no config updates” without completing its integration into the 3.2 views. This contradicts the shared health vocabulary and D-050’s explicitly postponed 024 integration.

5. **P2 — The resource-status check reads Services without declaring them.**  
   [pkg/health/checks.go:226](../../../../pkg/health/checks.go:226) declares `AllInputs()` plus Catalog. Its new 013 path reads both Service access and Service facts through [pkg/health/refs.go:344](../../../../pkg/health/refs.go:344), but Services are optional watches outside `Owned`.

   `Clock: true` currently prevents stale memo reuse, masking the contract violation. Narrowing that check’s clock dependency later would silently cache Service explanations and port fixes across Service changes. The declared-input guard misses this because its Service worlds have resolved references and never exercise the new explanation branch. Declare Services and add that branch to the guard.

6. **P4 — Backend fan-in makes endpoint analysis quadratic, including for healthy Services.**  
   [pkg/health/endpoints.go:102](../../../../pkg/health/endpoints.go:102) scans the accumulated route slice for every backend reference. For 10,000 routes sharing one Service port, this performs roughly 50 million reference comparisons.

   The affected list is built before the ready-endpoint check, so healthy Services incur the same work and then discard it. Route changes invalidate this check through `Owned`. This is fragile for Phase 4’s shared-backend flows and large production fan-in despite the supplied benchmark gates passing. Use set-based accumulation or sort/deduplicate after collection.

No files edited or tests run. Test results remain author-reported. The linked program file could not be fetched; E2/E3/E8 were checked against the available local program checkout.

## Triage
| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | Group IDs disclose the hidden policy behind KGWUI-015 (P1) | reviewer | user decides | Partly confirmed, P3. The group ID and the restricted row's `cause` name the policy, but the route's own status message already names it, and Phase 2 rule 8 serves that status to the caller. Either group 015 by an ordinal, or amend §14 rule 11 |
| 2 | Proxy-stat evidence bypasses authorization of mapped Envoy names (P1) | reviewer | accept (P2) | Confirmed through the extension seam only: no shipped rule set registers an `EnvoyNamer`. Map and filter as `LogEvents` are. Stabilization 3.5 |
| 3 | List-only kind changes raise KGWUI-008 (P1) | reviewer | accept (P2) | Confirmed: `kinds/monitor.go` compares every served descriptor. A list-only change gives a note, not a failed required check. Stabilization 3.5 |
| 4 | KGWUI-024 does not reach the processes and data-plane summaries (P1) | reviewer | accept (P2) | Confirmed. Add 024 to the process codes and the data-plane value. Stabilization 3.5 |
| 5 | The resource-status check reads Services without declaring them (P2) | reviewer | accept (P3) | Confirmed, latent while the check declares `Clock`. Declare Services and add a guard world. Stabilization 3.5 |
| 6 | Backend fan-in makes 023 quadratic (P4) | reviewer | accept (P4) | Confirmed: 260 ms at 10,000 routes on one Service port, 49 ms linear. Stabilization 3.5 |
