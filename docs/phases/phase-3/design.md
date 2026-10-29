# Phase 3 — Health & Diagnostics: Design

**Status:** Draft for cross-model review and user approval. Written without a question round, at the user's request, from the roadmap and from research. Revised after a builder check and the user's answers (2026-10-29).
**Date:** 2026-10-29
**Base:** `544dcd5`
**Roadmap:** `docs/roadmap.md`, Phase 3
**Decisions:** D-001 … D-026 in `docs/decisions.md`. This design proposes D-027 … D-036 (§22).
**kgateway refs:** behaviour claims cite `file:line` at tag `v2.4.7` (`b4a2bf74`) or `v2.3.11` (`c2af2887`), as in Phase 2. Claims about unreleased behaviour cite `main` at `643f50b` (2026-10-28) or tag `v2.5.0-rc.1` (published 2026-10-27). Paths without a `pkg/` or `api/` prefix are under `pkg/kgateway/`.

Every choice in this draft is **(our call)** unless it repeats an earlier user decision, which keeps its original marker. Decisions from the user's answers are marked **(user, 2026-10-29)**; those from the builder check are marked **(our call, 2.4 builder check)**. §25 holds the one open item.

**One home per contract.** Finding identity, groups and ranking: §5. The check contract and its state store: §16.2. Check availability and roll-up: §7.1. Authorization: §14. Budgets: §16.3. Phase 2's homes still hold: scheduling and timestamps (Phase 2 design §3.1), computed views and their cross-check (`computed-rules.md`).

## Decisions for this phase

| # | Decision | Marker | Where |
|---|---|---|---|
| 1 | kgateway 2.5 joins the support matrix at GA, in 3.4: the 2.5 rule set, an e2e row, KGWUI-024, the controller-reported `TargetNotFound` path for KGWUI-016, and `kgateway_build_info`. Until GA, a 2.5 controller runs on the 2.4 rule set with the "newer than tested" note, and the computed KGWUI-016 rule applies. 2.5 fixtures are captured in 3.1 if cheap, otherwise in 3.4 | (user, 2026-10-29) | §7.6, §8.6, §21, §23 |
| 2 | Budgets stay: 150 ms warm, 15 % stream CPU, 432 MiB dense median, retained heap 32 MiB. 3.1 does the performance work first (recompute memoization, the KRT spike). Any gate change goes back to the user with numbers. 3.2–3.4 each show they stay within the gates | (user, 2026-10-29) | §16.1, §16.3, §23 |
| 3 | The `health.Check` contract changes, breaking, to one contract: `Name`, `Inputs`, `Run(RunContext)`. `RunContext` carries an engine-owned `State *Store` that survives recomputes and clears on restart. Every cross-recompute need uses it. All core and example checks migrate, plus `examples/downstream`, with README upgrade notes. Lands in 3.1 | (user, 2026-10-29) | §4, §16.2, §23 |
| 4 | No Phase 3 check is required, so none turns the install unknown. A watch that is denied, failing or off makes its check "not available", with the reason | (our call, 2.4 builder check) | §7.1, §11, §17.3, §18.1 |
| 5 | `since`: status-derived findings keep `lastTransitionTime`; UI-derived findings use the UI's first-seen time per finding identity, held in the Store and reset on restart. Ranking and "oldest problem" use each finding's own `since`. KGWUI-001 gains a `since` | (our call, 2.4 builder check) | §5.1, §5.3, §6, §8.3 |
| 6 | RBAC toggles default on. 022 and 023 are warnings. KGWUI-001 is re-specified, not replaced; specific codes replace KGWUI-002 for their reasons. A route row is never green while its Gateway rejects config | (our call) | §7.1, §8.3, §8.4, §9.4, §11, §18.1 |
| 7 | The UI reads no logs, not even opt-in. Guided commands cover log-only diagnostics | (our call) | §2, §12 |
| 8 | Filing the upstream reject-status and scope-endpoint issues stays the user's call. It is the only open item | (our call) | §8.7, §24, §25 |
| 9 | Shell and navigation land in 3.1, before the new pages: a prominent object header, a fluid layout that uses wide screens, and **path-based** breadcrumbs carried in the URL (from user testing on a 3008 × 1692 monitor) | (user, 2026-10-29) | §17.6, §21, §23 |
| 10 | Findings carry a deterministic **suggested fix** (YAML, target, basis) where the evidence leaves exactly one correct change: 014, and 013 for a wrong group or port. No model; the first layer of the roadmap's AI-assisted diagnosis | (user, 2026-10-29) | §7.7, §13.3, §17.2 |

## 1. Intent and why

Phase 3 turns the install page into the health overview the roadmap promises: one verdict, then what to fix first, ranked, with the next step for each problem.

**The central finding of the research.** Kubernetes status often says a gateway is healthy while its data plane is broken, and an Envoy rejection of the configuration (an xDS NACK) goes unnoticed. The research read about 620 public issues, support tickets (anonymised), escalations and field threads across six sources, kgateway and its predecessor's history included:
- About 95 of them are xDS rejects. In the kgateway-era issues, status was silent or misleading in 19 of 22 such cases; in a sample of downstream-distribution issues, in all 11. The resource said `Accepted=True` and `Programmed=True` while Envoy kept its last good config, so later edits silently stopped working, or a new proxy pod started with nothing.
- About 110 more are status that is missing, stale, rolled up wrongly or misleading. The predecessor project's history alone has 25 issues where status was silent, stale or wrong while traffic was broken, or fixed.
- The decisive data sat, in most cases, in a place the operator did not look: the controller log, the proxy's admin port, or a sibling resource's status. Time to diagnose ranged from hours to months.

What already exists. Phase 1 raises KGWUI-001 from `kgateway_envoy_xds_rejects_active`, checks controller and proxy readiness, and applies status freshness rules (Phase 1 design §5.2). Phase 2 added explorer pages, delegation, and computed views with a cross-check (D-020). So the gap is not detection. It is:
1. **Trust.** The reject finding says "rejected" and nothing more: no type, no number of proxies, no hint that the Gateway's status is green. The operator reads the green status and believes it.
2. **Ranking and grouping.** One missing Service can raise 40 findings. Nothing says which one to fix first.
3. **Next step.** The Envoy message, the culprit and the process failure live in logs and admin ports the UI must not read. The UI does not say where to look.
4. **Coverage.** Restarts, OOM kills, probe failures, version skew, empty backends and status that stopped progressing raise no finding today.

## 2. Goals, non-goals and anti-goals

**Goals**
1. **One verdict and a ranked list** on the install page: what is wrong, how much it affects, since when, and the decisive next step (§6).
2. **An analyzer with stable finding codes** that covers the roadmap floor: unresolved references, missing ReferenceGrant, rules dropped or replaced, policy target not found, conflicting listeners (§7).
3. **xDS rejects per Gateway that are hard to ignore:** per xDS type, how many proxies reject, the contradiction with status named, and the commands that show Envoy's reason (§8).
4. **Process health through the provider seam** (D-016): restarts, termination reasons, crash loops, probe failures, stuck rollouts, and version skew between controller, proxies and the Gateway API CRDs (§9).
5. **Status freshness** as findings an operator can act on (§10).
6. **Backend readiness:** a Service the controller resolved, but with zero ready endpoints (§11).
7. **Stable finding identity** (Phase 2 carry-over) and server-side paging of findings (§5, §13).
8. **The KRT decision**, taken in this phase with a spike first, and the performance work done before load is added (§16).
9. **One check contract** with declared inputs and an engine-owned state store, so core and downstream checks share one engine path (§16.2).

**Non-goals**
- Effective policy and merge winners (Phase 5), the route tester and shadowing (Phase 6), certificate expiry (Phase 7), golden metrics (Phase 8), the Envoy filter chain and a comparison of the controller's snapshot with what Envoy accepted (Phase 9).
- Predicting what Envoy will reject (regex limits, TLS parameter names, reserved ports). kgateway's strict validation mode is the answer upstream.
- Extension pods (ext-auth, rate limiting): kgateway ships none by default. A downstream registers its own processes through the seam (§9.1).
- GRPCRoute, TCPRoute and TLSRoute detail pages (roadmap deferred list). Their findings link to the YAML view.

**Anti-goals**
- **False positives that erode trust.** Istio's analyzer and the predecessor project's health check both lost users this way: findings on objects that worked, a sync check that misread healthy proxies, a UI that marked working gateways failed. Every Phase 3 finding rests on evidence the controller or Kubernetes wrote. Where the UI computes something, it is labelled, has a verifier and fires only after persistence (D-020).
- **Mirroring controller logic without a verifier.** The one new computed rule (policy target lookup on 2.3 and 2.4, §7.6) sits behind the `computed.Rules` seam, is cross-checked against the controller's ancestors, and switches off on versions where the controller reports `TargetNotFound` itself. Everything else reads what the controller or Kubernetes reports.
- **Reading Secrets, logs or admin ports.** Logs and snapshots can carry secret material: a downstream issue shows an API key inside an Envoy rejection message. The UI reads no logs, not even opt-in **(our call)**. It prints the command; the operator runs it with their own rights (§12).
- **Copying one error onto every object.** One cause is one finding group, with the affected objects listed under it (§5.2). The predecessor project copied proxy-level warnings onto every route, and users could not find the culprit.

## 3. Primary flows

1. **"Is it healthy, and what do I fix first?"** Open the install. The verdict strip says healthy, unknown with a reason, or "3 problems". Below it, "Needs attention" lists problem groups by rank: "Gateway `infra/edge`: proxies reject route config. Status says Accepted on 37 routes" first, then "Controller restarted 4 times in 15 min, last OOMKilled", then "Service `shop/cart` has no ready endpoints: 6 routes". Each row links to its subject and opens its next steps.
2. **"Why does my change not take effect?"** The Gateway page's Data plane section says: "2 of 3 proxies reject RouteConfiguration since 09:12. Status still says Accepted on 37 routes. Envoy keeps serving its last accepted config." It lists the routes and policies on this Gateway that changed in the 10 minutes before the first reject, labelled as a hint. It gives three copyable commands: the controller replica's log line with Envoy's reason, the proxy log, and the proxy's reject counters.
3. **"Is the control plane OK?"** The process strip shows the controller (2/2 ready, 0 restarts in 15 min, v2.4.7) and the proxies (12 Gateways, 1 with a crash-looping pod, 1 on an older version). It links to a Processes page with one row per Gateway.
4. **"Why is this route broken?"** The route's finding says "Backend not found: Service `shop/cart` exists but has no port 8080 (ports: 80)", or "Not permitted: add a ReferenceGrant in `shop` from `HTTPRoute` in `web` to `Service`", with the grant as YAML to copy.
5. **"Why does nothing update?"** An install finding says "The controller has not written status for 14 objects in 6 minutes", and gives the log command.

## 4. Architecture

```
 informers (+ proxy Pods, Events, CRD metadata, Services, EndpointSlices)    metrics (per replica, per type)
                  │                                                                │
                  ▼                                                                ▼
 engine.Compute(snapshot) — unchanged scheduling (D-019)
   install → computed views (A, B, C, + policy targets) → health checks (Phase 1, 2)
           → process.Source (§9) → analyzers (§7, memoized per input fingerprint, §16.2)
           → identity, groups, rank (§5) → overview / explore / diagnostics indexes
                  ▼
 Published{Model, Source, ConfirmedAt}  →  api: diagnostics, findings, processes, detail blocks
```

- **Analyzers are checks.** They implement the `health.Check` contract and register through `extension.WithChecks`, so downstreams add their own (the analyzer seam the seam milestone asked for, Phase 1 design §6.3). The contract changes, breaking, in 3.1: every check declares its **inputs** and runs with a `RunContext` that carries an engine-owned state store **(user, 2026-10-29)**. Core, example and downstream checks all move to it (§16.2).
- **Evidence is structured.** A finding keeps today's `message` and `detail` strings, and gains a typed `evidence` object per code (§13.3). The SPA renders evidence; it never parses prose (Phase 2 review finding 8).
- **New package `pkg/process`** holds the process model and the provider seam (§9.1). **`pkg/diagnose`** holds identity, grouping, ranking and next-step templates. Both are platform neutral; the Kubernetes implementation of `process.Source` lives in one allowlisted file (§19).
- **`pkg/ctrlmetrics`** keeps every metric name in one place, as today, and now keeps per-replica and per-type samples (§8.2).

## 5. Finding identity, groups and ranking

### 5.1 Stable identity (Phase 2 carry-over, P4)

Every finding gets an `id`, stable across recomputes and UI restarts, and unique within an install **(our call)**.
- `id = "f1-" + base32(sha256(installID ‖ code ‖ resource ‖ discriminator))[:26]`, lower case. The `f1` prefix versions the scheme. Fields are joined with a separator byte that cannot occur in them.
- `resource` is the full identity (group, kind, namespace, name) after defaults. `discriminator` is the code's structured key from the table below, encoded canonically (written refs, as in `computed-rules.md` §7).
- Message, detail, severity and `since` are **not** part of the identity, so rewording, a severity change inside the grace period or a moved timestamp keep the `id`.

**What `since` means (our call, 2.4 builder check).**
- **Status-derived findings** keep the controller's `lastTransitionTime`, as today.
- **UI-derived findings** (metrics, processes, computed) use the UI's first-seen time per finding `id`. The time lives in the check's state store (§16.2), so it resets when the UI restarts. `docs/findings.md` and the README say so.
- KGWUI-001 gains a `since`: the first time the UI saw rejects for that Gateway (§8.3).
- The engine's existing `seen` map, keyed by (uid, generation), keeps driving grace periods. It does not hold finding `since` values and is unchanged.

| Code | Discriminator |
|---|---|
| 001, 005, 006, 009, 010, 018, 019, 020, 021, 024 | none: one per resource |
| 002 | the condition's owner (parentRef as written, listener name, or ancestorRef as written, or `self`) and the condition type |
| 003 | parentRef as written, or `self` |
| 004 | the targeted parent ref |
| 007 | (namespace, resource label) |
| 008 | check name and cause key |
| 011 | the parent route ref, or `orphan` |
| 012 | the check key of `computed-rules.md` §8 (kind, subject, key) |
| 013, 014 | parentRef as written, or listener name |
| 015 | parentRef as written and the match index |
| 016 | targetRef index and the ref as written, or ancestorRef as written for the controller-reported form |
| 017 | listener name |
| 022 | `proxy`, `controller`, or the CRD name |
| 023 | the Service port (number or name, as the backendRef wrote it) |

- The `id` is opaque: clients compare it, never parse it. A downstream code's discriminator is the downstream's choice; it must be stable.
- API v1 is not yet frozen (no release tag exists), so `id` is an optional field added to every finding in every response, including `/health` (§13).

### 5.2 Groups and blast radius

A **group** is one cause with the findings it explains. The home page lists groups; the findings page lists findings, with a group filter **(our call)**.

| Group | Cause (group subject) | Members | Affected objects |
|---|---|---|---|
| Data plane rejects | the Gateway | its KGWUI-001 | routes, ListenerSets and policies with a current `Accepted=True` or `Programmed=True` entry for that Gateway (§8.4) |
| Missing backend | the unresolved backend identity, after defaults | every KGWUI-013 whose route names it | those routes |
| Not permitted | (from namespace, to namespace, to kind) | every KGWUI-014 with that shape | those routes and listeners |
| Replaced by policy | the policy named in the rule message, when it parses (§7.4) | every KGWUI-015 naming it | those routes |
| Status not progressing | the controller Deployment | KGWUI-018 and the KGWUI-003 warnings it explains | those objects |
| Empty backend | the Service | its KGWUI-023 | routes that reference it |
| Process | the controller Deployment, or the Gateway for proxies | KGWUI-005/006 and 019–021 on that subject | — |
| Other | the finding's own resource | that finding | — |

- A finding belongs to exactly one group; the table is checked top-down.
- The group `id` follows §5.1 with the code `G` plus the family name and the cause identity.
- **Affected** lists are bounded: the diagnostics payload carries the first 5 per group plus `affectedTotal` (visible objects only, §14 rule 13), and the finding detail pages through the rest.

### 5.3 Ranking

Groups are ordered by these keys, in order **(our call)**:
1. **Severity** of the worst member: error, then warning, then info.
2. **Class:**
   1. data plane: 001, 024;
   2. processes: 005, 006, 019, 020, 021;
   3. traffic-affecting configuration: 015, 017, 013, 014, 023, 016, 011, and 003 after grace;
   4. consistency and install hygiene: 012, 018, 022, 007, 009, 002 and everything else.
3. **Blast radius:** affected objects, descending.
4. **Age:** the oldest `since` first, using each member finding's own `since` (§5.1) **(our call, 2.4 builder check)**. A stuck problem outranks a fresh one of the same size.
5. The group `id`, for a stable order.

Info findings are never shown in "Needs attention"; they stay on the findings page. A KGWUI-008 (signal unavailable) is not ranked: the verdict strip names it, as today (Phase 1 design §5.3).

## 6. The health overview (install home page)

The URL stays `/i/:id`. Layout A (D-011) keeps its order with two sections added. Everything above the Gateway table answers "what is wrong"; the tiles and the table stay below for inventory **(our call)**.

| Section | Content | Source |
|---|---|---|
| Verdict strip | unchanged states and wording rules (Phase 1 design §5.3). Unhealthy: "N problems", counts per severity, the oldest problem's age (the oldest finding `since`, §5.1) | `diagnostics.verdict` |
| Needs attention | up to 10 groups by rank (§5.3). Each row: state (icon, colour, text), title, subject as a link, blast radius ("37 routes"), age, code, and a "Next step" expander with the group's commands (§12). "Show all" opens the findings page grouped | `diagnostics.groups` |
| Processes | one line for the controller (ready/desired, restarts in 15 min, version) and one for proxies (Gateways with every proxy ready, with a reject, with skew, with restarts). Links to `/i/:id/processes` | `diagnostics.processes` |
| What was checked | unchanged; collapsed when unhealthy | `checks[]` |
| Count tiles, Gateway table | unchanged; the Gateway table gains a "Data plane" column: ✓, "Rejects" or "Unknown" | overview |

- Healthy installs keep the slim green strip and show "What was checked", never an empty "Needs attention" box.
- A group whose cause the caller cannot view shows its severity, code and "Restricted: ask the owning team", as findings do today (D-013).
- Stale data turns every state unknown, as today. No green badge survives on stale data.

## 7. The analyzer

### 7.1 Codes

Phase 1 and 2 use KGWUI-001 to KGWUI-012. Phase 3 adds KGWUI-013 to KGWUI-024 and re-specifies KGWUI-001 and KGWUI-003 without changing their codes. Codes are never reused.

**Specific codes replace KGWUI-002 for their reasons (our call, D-030).** KGWUI-001 is re-specified, not replaced by a "status says healthy" code (§8.3) **(our call)**. KGWUI-002 stays the fallback for any current `False` condition without a specific code. A condition yields exactly one finding: either its specific code or 002. Alert rules keyed on 002 for these reasons will see the new codes; the release note says so. API v1 has no release yet.

| Code | Slug | Trigger | Severity | Source | Pending / grace | Sub-phase |
|---|---|---|---|---|---|---|
| 001 | xds-rejected | §8 | error | metrics | restart hold (§8.3); `since` from the UI's first sight (§5.1) | 3.2 |
| 003 | route-unattached | unchanged trigger; evidence gains its kind (§10.1) | info in grace, warning after | status | grace 60 s (unchanged) | 3.3 |
| **013** | ref-unresolved | a current route parent `ResolvedRefs=False` with a reason other than `RefNotPermitted`, or a current listener `ResolvedRefs=False` with reason `InvalidCertificateRef` | error | status, plus explanation from discovery and Services | none: current status only | 3.3, 3.4 |
| **014** | ref-not-permitted | a current route parent or listener `ResolvedRefs=False` with reason `RefNotPermitted` | error | status, plus the grant shape (§7.3) | none | 3.3 |
| **015** | rule-replaced | a current `RouteRuleReplaced` or `RouteRuleDropped` reason (on `Accepted` on 2.3, `kgateway.dev/Programmed` on 2.4) | error | status | none | 3.3 |
| **016** | policy-target-not-found | §7.6 | error when the controller reports it; warning when computed | status (2.5 rule set), computed (2.3, 2.4, and 2.5 before its rule set) | computed: after the policy's grace, then 30 s persistence | 3.3; status path 3.4 |
| **017** | listener-conflicted | a current listener condition `Conflicted=True` on an owned Gateway or ListenerSet | error | status, plus the other side (§7.5) | none | 3.3 |
| **018** | status-not-progressing | §10.2 | warning | status, plus metrics evidence | matures after 5 min | 3.3 |
| **019** | process-restarting | §9.3 | warning | process | expires 15 min after the last restart | 3.2 |
| **020** | probe-failing | §9.3 | warning | process (Events) | expires 15 min after the last failure | 3.2 |
| **021** | rollout-stuck | §9.3 | warning | process | none: Deployment condition | 3.2 |
| **022** | version-skew | §9.4 | warning | process, CRD metadata | suppressed during a controller rollout and 10 min after it | 3.2 |
| **023** | backend-no-ready-endpoints | §11 | warning | Services, EndpointSlices | matures after 30 s | 3.4 |
| **024** | xds-config-withheld | §8.6 (kgateway 2.5 and later) | error or warning | metrics | none | 3.4 |

022 and 023 are warnings, so neither turns an install red on its own **(our call)**.

**Availability and roll-up (our call, 2.4 builder check).** Phase 1 design §5.3 says a required check that is not OK makes the install unknown. No Phase 3 check is required: each sets `Required: false`, so none can turn the install unknown. Existing required checks keep their flag; the re-specified KGWUI-001 keeps the existing `xDS rejects` check's flag, including its restart hold (§8.3). Today's `unavailable` helper (`pkg/health/checks.go:42-46`) always sets `Required: true` and raises KGWUI-008; Phase 3 checks use a variant that does neither.
- A check whose input is missing returns an unavailable `CheckResult` with its reason: `denied` (RBAC refused the watch), `failing` (the watch or scrape errors), or `off` (its chart toggle is off). "What was checked" shows it as "not available", in the wording it uses for unreachable metrics today.
- A missing metric family on an older version is `not-on-version`, shown as "not available on this version" (§8.6).
- A not-available Phase 3 check raises no KGWUI-008 and never turns the verdict unknown. Its findings are simply absent, and "What was checked" says why.

Each code has a short docs entry (cause, evidence, fix) in `docs/findings.md`, written in 3.3; a finding's "Learn more" links to it **(our call)**. Messages and next steps go through the wording pass (§17.5).

### 7.2 Why this set, and what was cut

The research ranked 18 candidates by frequency and pain across sources. The floor and the top ten are in, as the table shows; Phase 2 already covers route attachment and delegation (KGWUI-003, 011, 012).

Cut, with the reason **(our call)**:
- **Policy attached but not applied** (about 35 items): needs the Envoy config dump or merge logic. Phase 5 and 9, and upstream status.
- **Envoy-rejectable value lint** (regex limits, TLS names, reserved ports, all-zero weights; about 18): predicts Envoy and strict-validation behaviour per version, which is mirroring with a high false-positive risk.
- **Unreported listener conflicts**, for example two TLSRoutes with one hostname on a passthrough listener (#14911): predicting them mirrors a controller bug. The fix belongs upstream; when it lands, KGWUI-017 shows it.
- **Controller configuration lint** (env enums, feature flags whose defaults moved): defaults change per version; it belongs in the scope/settings endpoint ask (D-009 §7).
- **Proxy out of sync on 2.3 and 2.4** (about 40, the most painful class): the evidence is in the admin server and logs only. On 2.5 the per-client metrics make part of it visible (KGWUI-024). Below 2.5 the UI gives guided commands (§12).
- **`kgateway_xds_snapshot_resources`** as a finding: it exists on 2.3 and 2.4 (`proxy_syncer/metrics.go:65-71`), but it describes the controller's per-client snapshot on one replica, not what a proxy accepted. It is shown as evidence on the Gateway page, never as a finding.

### 7.3 Unresolved and not-permitted references (013, 014)

KGWUI-013 and 014 always start from the controller's condition. The UI adds an explanation; it never decides that a reference resolves **(our call)**. Phase 2's anti-goal stands: no ReferenceGrant evaluation, no backend resolution.

**Explanation for 013,** first match wins:
1. The ref's group and kind are not served by the API server (discovery): "No kind `Service` in group `delegation.gateway.example`. Check the group." A typo in the group reads as `BackendNotFound` or `RefNotPermitted` today.
2. The kind is a core Service, Services are watched (§11), and the Service does not exist: "Service `shop/cart` does not exist."
3. The Service exists and has no port the ref names: "Service `shop/cart` has no port 8080 (ports: 80, 443)."
4. Otherwise: the controller's reason and message as written.

Listener `InvalidCertificateRef` quotes the controller. The UI cannot read Secrets, so it never says whether one exists.

**Explanation for 014,** for the fields the Gateway API spec defines (route `backendRefs`, listener `certificateRefs`): the ReferenceGrant the spec requires, as YAML to copy, with `from` (group, kind, namespace of the referrer) and `to` (group, kind, optional name). Rule 1 above runs first, so a wrong group is never explained as a missing grant. For kgateway-specific fields (policy and extension refs) the `from` identity is controller logic; the UI quotes the message and adds no YAML.

### 7.4 Rules replaced or dropped (015)

- On 2.3 the reasons arrive through `Accepted=False`; on 2.4 through `kgateway.dev/Programmed=False` (Phase 1 design §10). Both raise 015.
- The message format is `Replaced Rule (%d): …` or `Dropped Rule (%d): …`, and the number is the **match index** (`v2.4.7 translator/irtranslator/route.go:332,349`, `in.MatchIndex`), not the rule index. The evidence names it "match N" and does not map it to a rule **(our call)**.
- When the text after the colon contains a `group/Kind/namespace/name` token of a served policy kind, the evidence links that policy and the finding joins its "Replaced by policy" group (§5.2). Parsing is best effort; a parse failure quotes the message.
- A rule replaced for more than 10 minutes adds a hint: "If the cause was temporary, such as an identity provider outage, some versions keep the rule replaced until the controller restarts" (#14047).

### 7.5 Listener conflicts (017)

- The trigger is the controller's `Conflicted=True` on a listener. Today Phase 1 reports only `False` conditions, so a conflicted listener on a Gateway that says `Accepted=True` with reason `ListenersNotValid` raises nothing.
- **Both sides named.** The evidence lists the other listeners on the same Gateway, including its ListenerSets, with the same port and an overlapping hostname, labelled "Computed by the UI" with the rule "listener port and hostname overlap". Overlap is a spec-level comparison of two listener specs, not translation. If the UI finds no other side, the evidence says "The controller reports a conflict; no other listener on port 443 overlaps" and raises no disagreement.

### 7.6 Policy target not found (016)

| kgateway | Source | Behaviour |
|---|---|---|
| 2.5 GA and later, on the 2.5 rule set (3.4) | controller: ancestor reason `TargetNotFound` (#14709) | error, from status |
| 2.3, 2.4, and a 2.5 controller before the 2.5 rule set ships | computed: a targetRef or targetSelector resolves to nothing | warning, labelled, verified |

Until 2.5.0 GA ships and 3.4 adds its rule set, a 2.5 controller runs on the 2.4 rule set with the existing "newer than tested" note, and the computed rule applies there too **(user, 2026-10-29)**.

**Computed rule (2.3, 2.4).** A new method on the `computed.Rules` seam, specified in a new `computed-rules.md` section, pinned per rule set:
- A `targetRef` names a target by group, kind and name in the **policy's own namespace** (local refs, GEP-713). It is "not found" when that kind is served and watched, and no such object is in the cache. A `sectionName` that names no listener of the Gateway or ListenerSet, or no rule of the HTTPRoute, is "section not found". Any other target kind is unknown and raises nothing.
- A `targetSelector` is "matches nothing" when no object of its kind in the policy's namespace carries the labels.
- **When it fires.** After the policy's grace period (Phase 1 design §5.2), and after the result held for 30 s (`computed-rules.md` §9.3; the hold may move onto the check's state store, §16.2). If every target is missing, 016 replaces the policy's KGWUI-003 "not yet processed" finding. If only some targets are missing, 016 is raised per missing target, and the policy's other findings are unchanged.
- **Verifier.** If the controller writes a current ancestor for a target the rule called missing, the check disagrees and raises KGWUI-012 with check kind `policy-target`.
- An unknown discovery scope makes the rule unknown, as in D-024.
- On a rule set whose controller reports `TargetNotFound` (the 2.5 rule set, 3.4), the computed rule is off and the status source wins (D-020 rule 5).

### 7.7 Suggested fixes (user, 2026-10-29)

Where the evidence leaves exactly one correct change, the finding carries a **suggested fix**: the YAML to apply, the object it changes, and the rule it rests on. Elsewhere the finding keeps its next steps (§8.5). This is the deterministic first layer of the roadmap's AI-assisted diagnosis (Phase 11); it uses no model.

| Code | Suggested fix | Basis |
|---|---|---|
| 014 | the ReferenceGrant the spec requires, as a new object in the target namespace | Gateway API spec, for the spec-defined fields of §7.3 only |
| 013, explanation 1 | the ref with the corrected group, when exactly one served group has a kind of that name | API discovery |
| 013, explanation 3 | the ref with the corrected port, when the Service has exactly one port | the Service object |

Rules **(our call)**:
- A fix is only suggested when exactly one answer follows from the spec, discovery or a watched object. More than one candidate, or any kgateway-specific behaviour, gives no fix, only next steps. The UI never derives a fix from translation logic (principle 4).
- The UI never applies a fix (D-001). The user copies the YAML, or downloads it as a file.
- A fix for an existing object is the changed fragment with its location (`spec.rules[2].backendRefs[0]`), not the whole object, so it cannot overwrite fields the user did not see.
- A fix names only objects the caller may view (§14 rule 15). If it would need a hidden name, there is no fix.
- The grant YAML of §7.3 moves from the evidence into the suggested fix, so it has one home.
- The set of codes with a fix grows per code in later phases; each new entry needs its basis row.

## 8. xDS rejects per Gateway

### 8.1 What kgateway exports (verified)

`kgateway_envoy_xds_rejects_total` (counter) and `kgateway_envoy_xds_rejects_active` (gauge), labels `gateway_namespace`, `gateway_name` and `type_url` with the `type.googleapis.com/` prefix stripped (`pkg/kgateway/setup/envoy_error.go` at v2.3.11 and v2.4.7; unchanged on `main`). The UI already scrapes them on `:9092` (Phase 1 design §5.2; D-012).

How they behave, read from that file:
1. The callback runs synchronously under a mutex. **There is no channel on this path, so no NACK is dropped** (our reading of the code, both tags). The lossy "NACK channel full" behaviour reported in the research belongs to a downstream detector, not to upstream kgateway.
2. `rejects_active` counts rejecting **xDS streams** per (Gateway, type). With three proxy replicas all rejecting routes, it is 3, summed over controller replicas.
3. An entry clears only when the same stream ACKs that type, or the stream closes. A fix that produces no new push leaves it set; the next push clears it.
4. Only the controller replica that serves the stream reports it (D-012). A controller restart drops it until the proxy reconnects and rejects again.
5. A proxy whose node role does not split into three parts is ignored.
6. Envoy's reason is logged once per (stream, type) episode, as `"xds error"` with `gateway_name`, `gateway_ns`, `resource` and `error`. It is not in any metric.

Separately, `kgateway_resources_updates_dropped_total` (both tags, `pkg/krtcollections/metrics/metrics.go:89`) **is** a dropping channel, for the `kgateway_resources_*` metrics. Once it is above zero, `resources_managed` is invalid until the controller restarts (kgateway `main` metrics doc). KGWUI-007's verifier then reports KGWUI-008 "scope verification unavailable: the controller dropped metric updates" instead of comparing counts **(our call)**.

### 8.2 Scraping changes

- Keep samples per controller Pod and per `type_url`, not only the per-Gateway sum (`pkg/ctrlmetrics/parse.go:45-50` sums today).
- Parse `rejects_total` and `resources_updates_dropped_total` as well. A counter delta is taken per Pod UID, so a restarted replica's reset is not read as a negative. The previous value per Pod UID lives in the check's state store (§16.2).
- On 2.5 and later, parse the per-client publication families of §8.6 when present. An absent family means "not available on this version", never "unavailable".
- Metric names stay in `pkg/ctrlmetrics`, the one place a rename is fixed (Phase 1 design §12).

### 8.3 KGWUI-001, re-specified

KGWUI-001 keeps its code, severity (error) and resource (the Gateway). One finding per Gateway; the types are evidence **(our call)**.

**`since`.** The first time the UI saw rejects for that Gateway, kept in the check's state store and reset on a UI restart (§5.1) **(our call, 2.4 builder check)**.

**Evidence.** Per `type_url`: rejecting streams (summed), the controller Pods that report them, the UI's first-seen time, and `rejects_total` growth since first seen (from the per-Pod-UID values in the store). Also the proxy replicas the UI sees for that Gateway (ready / desired), so the row reads "2 of 3 proxies reject RouteConfiguration".

**The contradiction with status.** When the Gateway's own status is current and `Programmed=True`, the message says so: "Status says Programmed, but proxies reject its config" (wording pass). Otherwise: "Proxies reject this Gateway's config". The finding's group lists the objects whose current status says `Accepted=True` or `Programmed=True` for this Gateway, so the operator sees which green statuses may be stale (§8.4).

**Restart hold.** A reject that disappears while a controller Pod of the install has a new UID, or a higher restart count, since the previous scrape is held for 60 s, with the `xDS rejects` check pending, so the install shows unknown, not healthy. That check is required today and stays required (§7.1). The previous UIDs, restart counts and the hold deadline live in the check's state store (§16.2). If the proxies reconnect and reject again, the finding continues with its first-seen time. Without this, every controller restart briefly clears a real reject.

**Why one per Gateway.** A finding per type would split one incident into up to five rows. A finding per affected route repeats the predecessor project's mistake of copying one error onto every object (§2).

### 8.4 Contradicting status is never green

D-006 says the UI never reports green while a contradicting signal is active. Phase 3 applies it to rows and detail pages: a route row is never green while its Gateway rejects config, and shows at least warning **(our call)**:
- A route, ListenerSet or policy whose current status is `True` for a Gateway with an active KGWUI-001 carries the flag `gateway-rejecting` and is never shown `healthy`: its state is at least `warning` ("Its Gateway's proxies reject config"), even if its own conditions are all `True`. An error of its own still shows `error`.
- The flag creates no finding of its own. The row state rule of Phase 2 design §6.3 gains this one input.
- The route list gains `state=gateway-rejecting` as a filter.

### 8.5 The decisive next step

The Gateway page's Data plane section and the finding's next steps carry these commands, filled in for this install. The UI generates them; it never runs them **(our call)**. Drafts for the wording pass:

1. **See Envoy's reason** (needs `pods/log` in your own RBAC). The UI names the controller replica that reported the reject:
   ```sh
   kubectl logs -n kgateway-system kgateway-7c9f6b5d8-x2k4q --since=1h | grep 'xds error' | grep 'edge'
   ```
2. **See it on the proxy:**
   ```sh
   kubectl logs -n infra deploy/edge --since=1h | grep -i 'rejected'
   ```
3. **Check the proxy's counters** (needs `pods/portforward`):
   ```sh
   kubectl port-forward -n infra deploy/edge 19000:19000
   curl -s localhost:19000/stats | grep -E 'update_(rejected|success)'
   ```
4. **What changed:** routes, ListenerSets and policies on this Gateway whose generation changed in the 10 minutes before the first reject, newest first, at most 10. Labelled "Changed shortly before the first reject. A hint, not a cause." The research shows operators bisect by recent change; the UI has the first-seen times already.

The copy also says: "Log lines and admin output can contain configuration values, including secrets. Share them with care." Commands come from `Vocabulary` (Phase 2 design §8.2), so a platform without kubectl can override or hide them.

### 8.6 Withheld configuration on kgateway 2.5 (024)

kgateway `main` and `v2.5.0-rc.1` export per-client publication metrics with labels `gateway` and `namespace` (`main` metrics doc, "Per-client xDS publication metrics"). Where present **(our call)**:
- `kgateway_xds_snapshot_perclient_withheld_clients{state="warm_withheld"} > 0` → KGWUI-024 **error**: "Proxies get no config updates, because referenced clusters are missing". The doc says a withheld client gets no updates at all, certificate rotations included.
- An increase of `kgateway_xds_snapshot_perclient_bounded_publishes_total{mode=~"flip_release|warm_release|carry_expired"}` in the last 15 min → KGWUI-024 **warning**: "Some routes fail: config was released before every cluster was ready". The doc calls these the modes to alert on.
- The family's presence decides availability, not the version string. On 2.3 and 2.4 the check reports "not available on this version" in "What was checked" and never turns the install unknown (the check is not required, §7.1).
- kgateway 2.5 joins the support matrix at GA, and 024 ships in 3.4 once 2.5.0 GA is out **(user, 2026-10-29)**. The family names and labels are re-checked against the GA tag before 3.4 ships.

### 8.7 Upstream proposal

A draft issue sits outside the repo, because the repo is public. It asks kgateway to set a Gateway condition when proxies NACK, with the xDS type, the number of rejecting proxies and a bounded, sanitised reason, cleared on ACK, and debounced like status writes. Smaller asks ride along: a connected-proxies gauge per Gateway, and a first-seen time for the reject. It cites #14736, #14911, #14453 and #14264, and Phase 2 design §14.8. Filing stays the user's call **(our call)**; it is the open item in §25. When kgateway writes such a condition, the UI reads it first and keeps the metric as a cross-check (D-020 rule 5).

## 9. Process health

### 9.1 The provider seam (D-016)

The roadmap says pod health is process health: the facts come from a provider, so a standalone kgateway can supply them without Pods **(D-016)**. New package `pkg/process`:

```go
// Source reports the processes of one install: its controller replicas and
// the proxies of each Gateway it owns.
type Source interface {
    Controller(in install.Resolved, snap *cache.Snapshot) Set
    Proxies(in install.Resolved, gw objects.Ref, snap *cache.Snapshot) Set
}

// Set is the processes behind one role. Observed false means the provider
// cannot see them (a self-managed proxy, a static install): "not observed",
// never "failing" (D-016 R3).
type Set struct {
    Observed  bool
    Desired   *int32          // nil when not applicable
    Rollout   *Rollout        // nil when the provider has no rollout notion
    Processes []Process
}

type Process struct {
    ID          string        // stable within the provider; a Pod UID in Kubernetes
    Name        string
    Ready       *bool
    Restarts    *int32
    LastStart   *time.Time
    Termination *Termination  // last termination: reason, exit code, time; never the message
    Waiting     string        // e.g. CrashLoopBackOff, ImagePullBackOff; empty when running
    Version     string        // parsed from the image tag; empty when unknown
    Probe       *ProbeFailures // nil when the provider has no probe events
}
```

- A `nil` field means "not observed", which is never a failure.
- The Kubernetes implementation (`pkg/process/kube.go`, allowlisted in the boundary test) reads controller Pods (watched today), **proxy Pods** (new watch), Deployments (watched today) and **Events** (new watch, §9.2).
- Proxy Pods are selected by a new optional detector method, `ProxyPodSelector()`. The upstream value is `kgateway=kube-gateway,app.kubernetes.io/component=proxy`, which the proxy pod template sets on both tags (`v2.4.7 pkg/kgateway/helm/envoy/templates/deployment.yaml:37-45`). The Deployment's `app.kubernetes.io/managed-by` label is not on the Pods. A Pod belongs to a proxy Deployment when the Deployment's `spec.selector` matches it in the same namespace, as for controller Pods (Phase 1 design §4.1).
- A downstream registers its own source with `extension.WithProcessSource`, for example to add extension pods. The default is the Kubernetes source.
- The termination **message** is never read into the model: with `FallbackToLogsOnError` it holds a log tail, which can carry secrets **(our call)**.

### 9.2 Events

Probe failures, back-off and image pulls appear only as Events.
- One cluster-wide watch with field selector `type=Warning,involvedObject.kind=Pod`. Events whose involved Pod is not a controller or proxy Pod of an install are dropped at the informer by a transform, which keeps only (Pod UID, reason, count, last time).
- Kept reasons: `Unhealthy` (probe failure), `BackOff`, `Failed`, `FailedScheduling`, `FailedMount`.
- Events expire after an hour by default and can be aggregated, so event-based facts read "in the last 15 minutes, from Events" and never claim a total.
- Retained events are capped at 10,000 per UI. Past the cap the oldest go, and the `events` check notes it.
- RBAC and the chart toggle: §18.

### 9.3 Findings 019, 020, 021

KGWUI-005 (controller not ready) and 006 (proxy not ready) stay the error findings. Their detail gains the waiting reason and last termination, for example "0/2 ready: CrashLoopBackOff, last exit OOMKilled". The new codes cover what keeps a process ready but unstable **(our call)**.

**KGWUI-019 process-restarting (warning).** A controller or proxy container started again within the last 15 min: its restart count grew while the UI watched, or its last termination finished within 15 min.
- The evidence carries the restarts in the window, the last termination reason and exit code, and the time.
- For the controller, exit code 2 adds "A Go panic exits with code 2. The stack trace is in the previous container's log", with `kubectl logs --previous`.
- One finding per subject: the controller Deployment, or the Gateway for its proxies.
- Restart growth is read against the restart count per process ID kept in the check's state store (§16.2).
- A deadline expires it 15 min after the last restart (D-019).

**KGWUI-020 probe-failing (warning).** At least 3 readiness, liveness or startup probe failures on one Pod in the last 15 min, from `Unhealthy` Events. It catches proxies that flap without restarting. Probe output is not served, only the probe type, the count and the time.

**KGWUI-021 rollout-stuck (warning).** A controller or proxy Deployment has `Progressing=False` with reason `ProgressDeadlineExceeded`, or a Pod of its newest ReplicaSet waits with `ImagePullBackOff`, `ErrImagePull`, `InvalidImageName` or `CreateContainerConfigError` for more than 2 min while older Pods still serve. When no replica is ready, 005 or 006 already makes it an error.
- The newest ReplicaSet's Pods are told apart by `pod-template-hash` against the Deployment's current template. ReplicaSets are not watched.

### 9.4 Version skew (022)

**Proxy vs controller.** The proxy image tag is the kgateway version (the envoy-wrapper image is tagged with it). The proxy container is the one running that image.
- A proxy Pod whose version's major.minor differs from the controller's raises a **warning** **(our call)**: "Proxy 2.3.6, controller 2.4.0. kgateway does not support mixed minor versions" (#14471, #14384).
- A patch difference is evidence on the Gateway page, not a finding.
- The skew is suppressed while the controller Deployment rolls out, and for 10 min after, so the deployer can update the proxies. The end of the last rollout is kept in the check's state store (§16.2).
- A tag that does not parse (a digest, a custom tag) gives "version unknown", no finding. A Deployment whose image is pinned in GatewayParameters is named as the likely cause.

**Controller replicas.** Two running versions after a controller rollout settles raises a warning.

**Gateway API CRDs.** The UI reads CRD **metadata only**, through a metadata informer, and keeps the CRDs of the groups `gateway.networking.k8s.io` and `gateway.networking.x-k8s.io` (RBAC cannot filter by group, so the filter is client-side). It reads the annotations `gateway.networking.k8s.io/bundle-version` and `gateway.networking.k8s.io/channel`.
- The expected bundle version per kgateway minor is a pinned, pluggable table next to the rule sets: 2.3 → v1.5, 2.4 → v1.6, 2.5 → v1.7, from each tag's `go.mod` (`v1.5.1`, `v1.6.1`, `v1.7.0-rc.3`). The 2.5 entry is re-read from the GA tag in 3.4. It is release metadata, not translation. The upstream ask is machine-readable compatibility metadata (#13683; #13940 is closed, and no chart annotation was found up to `v2.5.0-rc.1`).
- A warning when an installed CRD's bundle minor differs from the expected one. A warning when the Gateway API CRDs carry different bundle versions or channels.
- The channel also sharpens Phase 2's ListenerSet hint (Phase 2 design §8.5). Today it is worded as a possibility, because the UI could not tell an unset `allowedListeners` from a CRD that lacks it. A standard-channel Gateway CRD next to a ListenerSet CRD now says which.

## 10. Status freshness

### 10.1 KGWUI-003, explained

The trigger, severity and grace stay as in Phase 1 design §5.2. The evidence gains a `kind`, and each kind gets its own message in the wording pass:

| Kind | Rule | Message draft |
|---|---|---|
| `never-written` | no status block from this controller, or the CRD default: reason `Pending` with message "Waiting for controller" and `lastTransitionTime` at the Unix epoch | "The controller has never written status for this object" |
| `stale` | `observedGeneration` below `metadata.generation` (or 0) | "Status describes generation 3; the object is at 4 since 09:12" |
| `pending` | a current condition with reason `Pending` or status `Unknown` | "The controller has not finished processing this object" |
| `no-ancestor` | a policy with no ancestor from this install | "No Gateway reports using this policy" (016 replaces it when the target is missing, §7.6) |

### 10.2 KGWUI-018 status-not-progressing

One finding per install, on the controller Deployment, when status as a whole stops moving. It groups the many KGWUI-003 warnings that a single cause explains, such as a status patch failing every reconcile, a stuck translation, or a CRD whose schema rejects the status (§5.2).
- **Trigger:** the controller is ready, at least one owned object has had a KGWUI-003 warning for 5 min, and at least 10 objects, or 10 % of objects with status (minimum 3), are in that state. Matures through a deadline; the start of the condition is kept in the check's state store (§16.2).
- **Evidence:** counts per §10.1 kind, the oldest age, and, when the family is present, the increase of `kgateway_status_syncer_status_syncs_total` over the window (2.3 and 2.4, `proxy_syncer/metrics.go:25-27`), quoted as "status syncs in the last 5 min: 0".
- **Next step:** the controller log command, grepping for `status`.
- A not-ready controller raises KGWUI-005 instead.

## 11. Backend readiness (023)

**Rule.** For each owned route, each rule `backendRef` that names a core `Service`, has weight above 0 (unset counts as 1), and sits on a parent where the route has a current `ResolvedRefs=True` from this install:
- the Service exists and is not `ExternalName`;
- the EndpointSlices labelled `kubernetes.io/service-name=<name>` in its namespace have no endpoint with `conditions.ready` true or unset (an unset value counts as ready, as the EndpointSlice API says) for the port the backendRef names;
- then KGWUI-023 is raised on the Service once the result held for 30 s **(our call)**. The hold's start per Service port is kept in the check's state store (§16.2).

**Why this is not mirroring.** The controller already resolved the ref (`ResolvedRefs=True`); the UI reports a Kubernetes fact about the Service it names. Delegation refs, mirror targets, `Backend` kinds, ServiceEntries and Hostname refs are out: their endpoint semantics are controller logic, and Phase 4 owns them.

**Severity: warning (our call).** Scaling to zero can be deliberate, and false positives cost trust. The message says "Requests to this backend get 503". One finding per Service port; the routes are the group's affected list.

**Data.** Two new cluster-wide watches with transforms that keep only what the rule needs:
- Service: namespace, name, type, ports (name, port, target);
- EndpointSlice: namespace, service-name label, ports (name, port), and per slice the count of ready endpoints per port.

At about 200 bytes per kept object this stays small at Large (§16.3). The same Service data feeds 013's explanation (§7.3).

**Capability.** `/meta` reports `endpoints` when both watches run. Without it, 023 returns an unavailable `CheckResult` with its reason: `off` (chart toggle), `denied` (RBAC), or `failing` (the watch errors, or a provider without Services). It says so in "What was checked" and never turns the install unknown (§7.1). This keeps D-016's rule that Services and EndpointSlices are optional.

## 12. Checks the UI cannot run

These checks need access a read-only UI does not have, by design. The UI reads no logs, not even opt-in; guided commands cover every log-only diagnostic **(our call)**. The UI shows what it knows and the command that gets the rest. Commands are filled in with the install's namespace, Deployment and Pod names, and go through `Vocabulary`.

| Question | Where the answer is | Why the UI does not read it | What the UI shows |
|---|---|---|---|
| Envoy's reason for a reject | controller log (`"xds error"`), proxy log | `pods/log`; logs can carry secrets | KGWUI-001 evidence and the commands of §8.5 |
| Which resource Envoy rejected | proxy admin `:19000` `/config_dump`, `/stats` | `pods/portforward` is write-class (D-001); output can carry secrets | the reject type, the recently changed objects, and the port-forward command |
| Endpoints a proxy really holds; health-check flags | proxy admin `/clusters`, `/config_dump?include_eds` | as above | KGWUI-023 for the Kubernetes side, plus the command |
| Proxy is Ready but has partial config | proxy admin stats `cluster_manager.warming_clusters`, `listener_manager.total_listeners_warming` (#14363) | as above | proxy readiness, KGWUI-024 on 2.5, and the command |
| Deferred or withheld snapshot on 2.3, 2.4 | controller log (`deferring snapshot`), admin `:9095/snapshots/xds` | `:9095` binds to localhost (`v2.3.11 admin/server.go:87`; `v2.4.7` `KGW_ADMIN_BIND_ADDRESS`), so only port-forward reaches it; it also serves `/logging` and pprof (Phase 2 design §3.7) | the command; KGWUI-024 on 2.5 |
| A controller panic's stack trace | previous container log | `pods/log` | KGWUI-019 with exit code 2 and `kubectl logs --previous` |
| Deployer errors (proxy Service apply rejected, #14893) | controller log | `pods/log` | KGWUI-006 or 021 when they show on the proxy; the log command |
| Controller memory profile | `:9095/debug/pprof` | as above | KGWUI-019 OOMKilled; the command |

Each command carries what it needs from the operator's own RBAC ("needs `pods/log`"). No command writes to the cluster. Port-forward is the one command that opens a connection; the copy says so.

## 13. API additions

All additions are new paths or new optional fields. No existing path, body or install ID changes. API v1 is not released, so its first release includes them.

### 13.1 Endpoints

| Endpoint | Returns | Sub-phase |
|---|---|---|
| `GET /api/v1/installs/{id}/diagnostics` | `Diagnostics`: the verdict, the top 10 groups, the process summary, the data-plane summary, `checks`, `collectedAt` | 3.2 |
| `GET /api/v1/installs/{id}/findings` | `Page<FindingRow>`; filters `severity`, `code`, `kind`, `namespace`, `group`, `q`; sort `rank` (default), `since`, `code`; Phase 2 cursors (§3.6 there) | 3.1 |
| `GET /api/v1/installs/{id}/findings/{findingId}` | `FindingDetail`: the finding, typed evidence, next steps, and the first 50 affected objects with a cursor (`?affectedCursor=`) | 3.1 (evidence grows per sub-phase) |
| `GET /api/v1/installs/{id}/processes` | `{controller: ProcessSetDTO, proxies: Page<GatewayProcesses>}`; filters `health`, `q`, `skew`, `restarts` | 3.2 |

Existing payloads gain:
- every finding, everywhere: optional `id`, `group` and `evidence`;
- `GatewayDetail`: optional `dataPlane` (§8) and `proxies`;
- route rows: the flag `gateway-rejecting` (§8.4);
- `/meta` `capabilities`: `processes`, `events`, `crds`, `endpoints`, each present only when its watch runs; and `filters.findings`, listing the findings filters.

`/health` keeps returning every finding, for compatibility. The SPA moves to the paged endpoint, because a worst-case Large install carries thousands of findings (D-022 measured 3,309 KGWUI-012 and 2,574 KGWUI-002 in one model).

### 13.2 Ranking on the wire

`GroupRow` carries its rank keys (`severity`, `class`, `affectedTotal`, `since`) so a client can explain the order. The server sorts; clients never re-rank.

### 13.3 DTOs

```jsonc
FindingRow     { id, code, slug, severity, resource: LinkedRef, message?, detail?, since?, group?,
                 restricted?, evidenceRestricted? }
FindingDetail  { install, finding: FindingRow, evidence?: Evidence, suggestedFix?: SuggestedFix,
                 nextSteps?: NextStep[], affected: Page<LinkedRef>, collectedAt }
Evidence       // a tagged union on "kind"; one shape per code. Examples:
  { kind: "xds-rejects", types: { typeUrl, streams, reportedBy: string[], firstSeen, totalSinceFirstSeen }[],
    proxies: { ready, desired }, statusSaysHealthy: boolean, recentlyChanged: LinkedRef[] }
  { kind: "ref-unresolved", ref: BackendRefDTO, reason, message, explanation?: string, computed?: Computed }
  { kind: "ref-not-permitted", ref }                              // the grant is the suggested fix (§7.7)
  { kind: "rule-replaced", matchIndex, reason, message, policy?: LinkedRef }
  { kind: "process", restarts?, termination?: { reason, exitCode, at }, waiting?, probe?: { type, failures, last } }
  { kind: "version-skew", controller, proxies: { version, pods }[], crds?: { name, bundleVersion, channel }[], expected? }
  { kind: "endpoints", service: LinkedRef, port, ready: 0, total, slices }
  { kind: "freshness", freshness: "never-written"|"stale"|"pending"|"no-ancestor", observed?, current?, since? }
NextStep       { title, command, needs?: string[], note? }       // needs: e.g. ["pods/log"]
SuggestedFix   { title, target: LinkedRef, create: boolean, path?: string, yaml, basis }
                                                                // path: the fragment's location when create is false
GroupRow       { id, title, severity, class, cause: LinkedRef, members: number, affected: LinkedRef[],
                 affectedTotal, since, nextSteps?: NextStep[], restricted? }
Diagnostics    { install, verdict: { state, problems, oldestSince?, reason? }, groups: GroupRow[], groupTotal,
                 processes: { controller: ProcessSummary, proxies: ProxySummary },
                 dataPlane: { gateways, rejecting, withheld?, unavailable? }, checks: CheckResult[], collectedAt }
ProcessSetDTO  { observed, desired?, ready?, processes: ProcessDTO[], rollout?: { progressing, reason? } }
ProcessDTO     { name, ready?, restarts?, restartsRecent?, lastStart?, termination?, waiting?, version?, probe? }
GatewayProcesses { gateway: LinkedRef, health, set: ProcessSetDTO, rejects?: number, skew?: boolean }
DataPlaneDTO   { rejects: { typeUrl, streams, firstSeen }[], statusSaysHealthy, snapshot?: { listeners, routes,
                 clusters, endpoints, secrets }, nextSteps: NextStep[], recentlyChanged: LinkedRef[] }
```

- Wire names follow Phase 2's conventions. Times are UTC.
- `snapshot` is `kgateway_xds_snapshot_resources`, labelled "From the controller's metrics: what it built, not what proxies accepted" (§7.2).
- Goldens: `diagnostics.json`, `findings.json`, `finding-detail-*.json` (one per evidence kind), `processes.json`, and restricted variants of each. Existing goldens change only by optional fields; a test asserts the diffs add nothing else (Phase 2 design §7.6).

## 14. Authorizer

Phase 2 design §7.4 rules 1–10 hold. Phase 3 adds these **(our call)**:

11. **Finding identity.** `id` and `group` are shown on restricted findings too. They are hashes of data a restricted finding already shows (code, resource identity, D-013) or of spec fields of that resource, and reveal nothing else.
12. **Evidence dependencies.** Every finding carries its evidence's dependency set in `Finding.Evidence` (it exists, `pkg/health/finding.go:76-78`). A caller who may view the resource but not every dependency gets `evidenceRestricted: true` and no evidence (Phase 2 rule 7). Dependencies per code:

| Code | Resource | Evidence dependencies |
|---|---|---|
| 001 | Gateway | controller Pods named in `reportedBy`; each `recentlyChanged` object is filtered on its own (rule 14) |
| 013 | route or Gateway/ListenerSet | the Service, when the explanation quotes its existence or ports |
| 014 | route or Gateway/ListenerSet | none: the grant is built from the ref as written (rule 8) |
| 015 | route | the policy, when linked; otherwise none (the message is the route's own status) |
| 016 | policy | none: the target is named in the policy's spec. An absent target is never hidden |
| 017 | Gateway or ListenerSet | every other listener's Gateway or ListenerSet named |
| 018 | controller Deployment | none: counts only |
| 019, 020, 021 | controller Deployment, or Gateway | the Pods named |
| 022 | Gateway, controller Deployment, or CRD | the Pods named; CRDs are cluster-scoped refs |
| 023 | Service | none beyond the Service: slices are read as part of it |
| 024 | Gateway | none |

13. **Groups and affected lists.** A group row needs its cause to be visible; otherwise it is a restricted row (severity, code, "Restricted: ask the owning team") with no affected list. Affected lists hold visible objects only, and `affectedTotal` counts visible objects only (Phase 2 rule 5). The member count is install-wide, like `deniedInInstall` (rule 2).
14. **Recently changed objects** (§8.5) are each authorized on their own; hidden ones are left out without a count.
15. **Next steps** name only objects the caller may view. A command that would need a hidden name is left out.
16. **Processes.** Controller process facts need the controller Deployment. A Gateway's proxy facts need the Gateway; Pod names and per-Pod facts also need the Pod. Otherwise the set shows only its counts (ready/desired, which the Gateway's own proxy finding already reveals).
17. **Findings filters.** `q` matches a restricted finding's resource identity only, never its hidden message or evidence. Other filters use code, severity, kind and namespace, which restricted findings show. `total` counts restricted findings, since they are listed (D-013).
18. **Health is not filtered** (rule 9). The verdict, counts and ranks are computed over every finding.

## 15. Phase 2 carry-overs

| Carry-over | Decision | Where |
|---|---|---|
| (P4) Stable finding identity | decided: §5.1 | 3.1 |
| (P3) Delegation memory: bound generated path bytes in inherit merges; charge failure-reason text and JSON escaping in the page budget | do it. Inherit merges cap a generated path at 4 KiB, cut with a marker; the 100,000-unit response budget charges reason text and escaped bytes at 1 unit per 32 bytes after escaping **(our call)** | 3.1 |
| (P3) Spec-edit stream variant | measure it first on Phase 2 code, then gate it (§16.3) | 3.1 |
| (P4) `Shell` font wait | the shell exports `renderWhenFontsReady(root, element)`, which `web/app/src/main.tsx` uses; the extension docs require it for apps that embed `Shell` **(our call)** | 3.2 |
| KRT incremental recompute | decided this phase: §16 and D-027 | 3.1 |
| (P3) Upstream scope-endpoint issue | stays the user's call **(our call)**. Reminder: the draft lives outside the repo; Phase 3 adds a second draft (§8.7). §25 asks how to file them | — |
| (P3) Experimental-kind scope verification; #1 watch CRDs added after start | unchanged; still waiting on the scope endpoint. The CRD metadata watch (§9.4) does not re-watch kinds live | — |
| (P3) Per-user AuthN/AuthZ | unchanged (roadmap) | — |

## 16. Engine: the KRT decision and budgets

### 16.1 The KRT decision (D-027)

The question (roadmap, raised 2026-10-28): move the engine from full recomputes to incremental ones, for example with Istio's KRT collections, which kgateway uses?
- **For it:** recompute cost grows with analyzers and cluster size, and Phase 2 ended near its CPU budget.
- **Against it:** a heavy, unversioned dependency; per-request consistency is harder (today each request reads one immutable Model, D-019); and no reuse of kgateway's own code, because Go cannot link kgateway 2.3 and 2.4 into one binary.

**Phase 3 takes the decision in sub-phase 3.1, after a spike, before any analyzer lands** **(our call)**. 3.1 schedules the performance work first: recompute memoization (§16.2) and the KRT spike come before 3.2–3.4 add load **(user, 2026-10-29)**.

The spike:
- view A rebuilt as KRT collections, on a throwaway branch that is never merged;
- measured with the existing bench harness at Large against the current engine: warm recompute, the spec-edit stream, retained heap, and dense memory;
- also records the dependency's module graph, binary size change, Go version floor and licence.

The decision rule is written down before the spike runs:

| Outcome | Decision |
|---|---|
| Spec-edit stream CPU at least 40 % lower, retained heap no more than +16 MiB, every request still reads one immutable published value, no `replace` directives, Apache-2.0 compatible licence | **Adopt** KRT for the views and analyzers in a follow-up phase. Phase 3 checks declare their inputs (§16.2), which KRT can feed later |
| Anything less, or inconclusive | **Keep** the immutable full recompute. Add per-check memoization (§16.2) |

Either way the result is recorded as D-027, with the numbers.

### 16.2 The check contract and per-check memoization

**One contract, breaking (user, 2026-10-29).** Today a check is `Name()` and `Run(in Input)` (`pkg/health/finding.go:168-171`). In 3.1 every check implements:

```go
// Check is one health check or analyzer. The engine calls Run on every
// recompute whose declared inputs changed, and reuses the last result otherwise.
type Check interface {
    Name() string
    // Inputs names what Run reads: kinds, metric families, process data,
    // other checks' outputs. Undeclared reads are a bug the tests catch.
    Inputs() InputSet
    Run(ctx RunContext) ([]Finding, CheckResult)
}

// RunContext is today's Input plus the check's own state.
type RunContext struct {
    Input
    // State is owned by the engine and keyed by check name. It survives
    // recomputes and is cleared when the UI restarts.
    State *Store
}
```

- **Why one contract.** It is discoverable, the engine has a single path, and declared inputs let the engine skip or memoise unchanged checks, which also suits KRT (§16.1). The README already says the Go extension API may change until the extension-seam milestone after Phase 2.
- **Every cross-recompute need uses the Store:** the KGWUI-001 restart hold and first-seen time (§8.3), per-Pod-UID counter values and `rejects_total` growth (§8.2), 019's restart growth (§9.3), 022's rollout suppression (§9.4), 023's hold (§11), 018's maturation (§10.2), and UI-derived `since` values (§5.1). Whether KGWUI-012's persistence windows (`pkg/engine/persist.go`) move onto it is left to the plan, where it simplifies things.
- A check reads and writes only its own Store. Stored values never reach the API except as the findings and `since` the check returns.
- **Migration.** All core checks, the example checks and `examples/downstream` move to the new contract in 3.1. The README gains upgrade notes for downstream builds: the new signature, how to declare inputs, and how to move state held in package variables onto the Store.

**Memoization.** The engine reuses a check's previous result when the fingerprints of its declared inputs are unchanged. The fingerprints are the per-kind hashes D-019 already computes (`Snapshot()` source fingerprint), plus the metrics result identity, a process-data hash and the fingerprints of any checks it reads. It is the views-memo pattern Phase 2 used for annotation-only edits. Time-driven checks (019, 020, 024, every maturation and hold) also key on their next deadline, so a skipped run never misses a deadline. A check that writes its Store is still skipped when its inputs and deadline are unchanged.

### 16.3 Budgets

Phase 2 ended with thin headroom: warm recompute 104–125 ms against 150 ms on CI; stream CPU 10–12 % against 15 % (annotation-only, through the views memo); dense peak heap 416 MiB against 432 MiB; retained heap +23 to +25 MiB against 32 MiB. The gates stay: 150 ms warm, 15 % stream CPU, 432 MiB dense median, retained heap 32 MiB **(user, 2026-10-29)**. 3.2, 3.3 and 3.4 each show, with numbers, that they stay within them. Phase 3 adds per-stage gates **(our call)**:

| Measure | Budget at Large | When |
|---|---|---|
| Phase 2 baseline: warm recompute, both stream variants, retained heap, dense memory | measured on `544dcd5` first, on CI and locally, recorded in `bench/3.1-baseline.txt` | 3.1 |
| Warm recompute, whole model | ≤ 150 ms, unchanged, consistent and worst case | 3.1–3.4 |
| Analyzer stage alone (identity, groups, rank, every Phase 3 check) | ≤ 20 ms warm; ≤ 2 ms when memoized inputs are unchanged | 3.2–3.4 |
| Stream CPU, annotation-only | ≤ 15 % of one core, unchanged | 3.1–3.4 |
| Stream CPU, spec-edit (one route spec change per 100 ms for 10 min, 20 polling clients) | reported in 3.1 on Phase 2 code; gated from 3.2 at ≤ 25 % of one core. If the baseline already exceeds 22 %, the numbers go to the user before a gate is set | 3.1–3.4 |
| Retained heap | the Phase 2 gate of 32 MiB, unchanged; and ≤ +24 MiB above the 3.1 baseline, including the new watches (proxy Pods, Events, CRD metadata, Services, EndpointSlices), finding identities and check Stores | 3.1–3.4 |
| Dense memory | median peak heap ≤ 432 MiB, unchanged (D-025) | 3.1–3.4 |
| OS memory during the stream | ≤ 256 MiB (D-022) | 3.2–3.4 |
| `GET …/diagnostics`, `GET …/findings` (each filter mix), `GET …/processes` | p95 < 50 ms, p99 < 100 ms; p95 < 100 ms with 20 concurrent clients | 3.1–3.4 |
| Change burst, idle, cold recompute | Phase 2 gates, unchanged | 3.1–3.4 |

No gate changes without the user. If the 3.1 baseline, or any later sub-phase, breaks a gate, the work stops and the numbers go to the user, who decides **(user, 2026-10-29)**.

**Scale tiers grow** (`internal/testutil/scale`):

| Shape | Typical | Large | Extreme |
|---|---|---|---|
| Services (referenced / total) | 300 / 600 | 2,000 / 5,000 | 10,000 / 25,000 |
| EndpointSlices | 700 | 6,000 | 30,000 |
| Proxy Pods | 2 per Gateway | 2 per Gateway | 3 per Gateway |
| Warning Events on watched Pods | 50 | 1,000 | 10,000 (the cap) |
| Gateway API CRDs (metadata) | 10 | 10 | 10 |
| Worst case for findings | — | every Gateway rejecting, 5 % of Services empty, every proxy restarted once | — |

## 17. Frontend

### 17.1 Pages and URLs

| URL | Page | Sub-phase |
|---|---|---|
| `/i/:id` | install page with "Needs attention" and "Processes" (§6) | 3.2 |
| `/i/:id/findings` | server-paged, grouped or flat (`?view=groups`), filters in the URL | 3.1, 3.3 |
| `/i/:id/findings/:findingId` | finding detail: evidence, affected objects, next steps, "Learn more" | 3.1 |
| `/i/:id/processes` | controller and per-Gateway proxy processes | 3.2 |
| `/i/:id/gateways/:ns/:name` | gains the Data plane and Proxies sections | 3.2 |

The install nav gains Processes. `CorePages` gains optional `DiagnosticsSection`, `FindingPage` and `ProcessesPage` slots (Phase 2 design §8.2 pattern). `SHELL_API_VERSION` stays 1.

### 17.2 Components

- `NextSteps`: numbered steps, each a code block with a copy button, the needed permission and a note. Commands render as text, never as HTML.
- `SuggestedFix`: the target object as a link, the fragment's location, the YAML with copy and download buttons, and the basis in one line. Shown above the next steps (§7.7).
- `EvidenceView`: one renderer per evidence `kind`. A downstream renders its own kinds through the finding-renderer extension point (Phase 1 design §6.3).
- `GroupRow`: state, title, cause link, blast radius, age, code chip and the next-step expander.
- `ProcessLine`, `ProcessTable`.

### 17.3 States

Phase 2's state table (Phase 2 design §8.6) applies. Added:
- "Not available on this version" for a check whose metric family is absent (024);
- "Not observed" for a process set the provider cannot see;
- "Not available" for a check whose watch is denied, failing or off, with the reason; a denial names the verb and resource (§7.1).

### 17.4 Interaction and accessibility

Phase 2 design §8.7 applies.
- Every new interactive element (group rows, expanders, copy buttons, filters) has hover, active and focus-visible styles in both themes.
- Expanders carry `aria-expanded`. Copy buttons announce "Copied" to screen readers.
- State is always icon, colour and text, at WCAG AA, checked by the existing token test.
- The "Needs attention" list keeps a fixed row height from the first render, so polling and late data do not shift the layout.

### 17.5 Wording pass

All new copy goes through the wording pass (Phase 2 design §8.8): titles and messages per code, freshness kinds, the status-says-healthy message, next-step titles and notes, the restart hold, "not available on this version", "not observed", the secret-care note, the breadcrumb and header labels (§17.6), and the empty states. Drafts in this design are placeholders.

### 17.6 Shell and navigation (user, 2026-10-29)

User testing of the Phase 2 explorer on a 3008 × 1692 monitor found three problems. On a detail page, the object's identity is a small line of text at the top and easy to miss. Content is capped at 1400 px (`ListFrame`), so a wide screen shows mostly blank margins. A drill-down has no way back except the browser's back button. These changes land in 3.1, so the Phase 3 pages are built on them rather than retrofitted.

**Object header.** Every object detail page opens with one shared `ObjectHeader`:
- the kind's icon and label, then the name in the largest type on the page, then the namespace, muted;
- the object's health state (icon, colour and text) and its data age;
- a fixed height from the first render, so late data moves nothing.

Extension pages for downstream kinds use the same component, exported from the shell. The browser tab title already names the object (`DocumentTitle`) and stays.

**Layout on wide screens.**
- Lists and tables are fluid: they fill the viewport minus the gutters, up to 2400 px, instead of stopping at 1400 px.
- Prose (messages, descriptions, next steps) keeps a readable line length of about 80 characters wherever it sits.
- At 1600 px and wider, detail pages use two columns. The main column holds the object's structure (listeners, rules, attached routes). The side column holds conditions, findings and related objects. Below 1600 px the side column stacks under the main one, in the same order.
- Type sizes do not grow with the viewport; browser zoom stays the way to enlarge text **(our call)**.
- The Phase 2 widths stay correct: 720 px, 1280 px and the narrow-table breakpoint keep their layouts.

**Path-based breadcrumbs (user, 2026-10-29).** The trail shows how the user got here, not the object hierarchy. A route reached from Gateway `infra/edge` shows `<install> › Gateway infra/edge › HTTPRoute shop/store`; each crumb links back to that page with its own trail.
- The trail lives in the URL, as a `via` query parameter listing the earlier object pages. It survives reloads, back and forward, and shared links. Each entry is the object path the page already uses (`group/kind/namespace/name`), so no new identifier is introduced.
- Following an in-app link from an object page appends the current page to the trail. Linking to a page already in the trail cuts the trail back to that entry, so cycles (delegation, route ↔ Gateway) never grow it.
- The trail keeps at most 6 entries. When it would grow past that, the oldest entries collapse into a "…" crumb that opens a short menu of them.
- A page with no `via` (typed URL, a link from outside, a list page) shows the plain hierarchy instead: `<install> › <kind list> › <object>`.
- A `via` entry that no longer resolves (deleted object, malformed value) is shown as plain text, not a link, and the page itself still loads. A `via` entry is never trusted for content: each crumb shows only what its own path says.
- Authorization (§14): a crumb for an object the caller may not view shows the restricted label, never its name. The current Authorizer rule for names applies unchanged.
- The first entry may be a list page (an object list or the findings page) with its filters, which already live in the URL (Phase 2 design §8.7). Its crumb returns to that list with those filters. Every later entry is an object page.

The "back to install" links in the error and not-found states stay as they are.

## 18. RBAC, chart and security

### 18.1 ClusterRole delta

All additions are get/list/watch only. Each has a chart toggle under `diagnostics.*`, default on **(our call)**. Turning one off drops both the rule and the watch. The dependent checks then report "not available" with reason `off`; a toggle that is on but denied by RBAC or failing reports `denied` or `failing`. None of them turns the install unknown (§7.1).

| Rule added | For | Toggle | Sub-phase |
|---|---|---|---|
| `""` `events` | probe failures, back-off, image pulls (§9.2) | `diagnostics.events` | 3.2 |
| `apiextensions.k8s.io` `customresourcedefinitions` (read through the metadata client) | CRD bundle version and channel (§9.4) | `diagnostics.crds` | 3.2 |
| `""` `services` | 013 explanations, 023 | `diagnostics.endpoints` | 3.4 |
| `discovery.k8s.io` `endpointslices` | 023 | `diagnostics.endpoints` | 3.4 |

- `pods` (already granted) gains a second label-selected watch for proxy Pods. No new verb.
- Still **no Secrets, no ConfigMaps, no `pods/log`, no `*/proxy`, no `pods/portforward`.**
- D-017 §5 dropped CRD reads for least privilege; D-029 reverses that for metadata only, with a reason and a toggle.
- The README's "Read-only RBAC" line lists CRDs today, while the chart has no CRD rule. 3.2 fixes the README with the real list.
- The chart README documents each toggle and what it disables.

### 18.2 Security

- **Never read:** Secrets, logs, admin ports, termination messages, probe output, Event messages. Only Event reasons, counts and times are kept.
- **Commands are text.** No command is run, sent or stored. Values are shell-quoted. Names come from objects the caller may view (§14 rule 15).
- **Trust model unchanged** (D-013). The README states the new kinds the ServiceAccount reads.
- **Raw payloads.** Evidence never echoes a whole status message longer than 1 KiB; it is cut with a marker. This guards against a controller that writes very large status (a lesson from the predecessor project).

## 19. Platform neutrality (D-016)

| Addition | How it stays neutral |
|---|---|
| `pkg/process` | a neutral model with optional fields. The Kubernetes source is one allowlisted file; a standalone provider supplies its own or reports "not observed" |
| Proxy Pod selector | an optional detector method; no new `corev1` import outside the allowlist |
| Events, Services, EndpointSlices, CRD metadata | read in `pkg/cache` (allowlisted) and handed to checks as neutral projections; each behind a capability |
| Analyzers, identity, groups, rank | `pkg/diagnose` and `pkg/health`: objects through `pkg/objects` only, covered by `internal/boundary/imports_test.go` |
| Next-step commands | through `Vocabulary`; a platform without kubectl overrides or hides them |
| Version skew | versions come from `Process.Version`, whatever the provider parses |

A test runs the analyzer stage over a snapshot with no Pods, Events, Services or CRDs: process findings are "not observed", 023 is "not available", and every other code behaves the same.

## 20. Cost

This phase generates no data-plane configuration and no xDS churn.

| Item | Cost |
|---|---|
| API server | five more watches: proxy Pods (label-selected), Events (field-selected), CRD metadata, Services and EndpointSlices (cluster-wide). Events and EndpointSlices have the highest churn; both are cut down by transforms at the informer, and EndpointSlice events trigger a recompute only through the scheduler's debounce (D-019) |
| Recompute | analyzers are linear in findings and objects; memoized per input (§16.2) |
| Metrics | the same scrape; more samples kept, per Pod and type |
| Memory | projections of the new kinds, and finding identities; gated (§16.3) |
| Per request | the diagnostics summary is precomputed per Model; findings paging uses keyset cursors over a precomputed rank order |

## 21. Testing

The boundary stays **cluster objects in → API JSON out** (Phase 1 design §9).

**API boundary tests (Go), per code, positive and negative:**
- **013:** each explanation branch (kind not served, Service missing, port missing, controller message), and a certificate ref. Suggested fixes (§7.7): one served group with that kind gives the corrected group, two give none; a one-port Service gives the corrected port, a two-port Service gives none.
- **014:** the grant as the suggested fix for a backendRef and a certificateRef; no fix when the grant would name an object the caller may not view; a wrong group explained as a wrong group, not a missing grant; a kgateway-specific field with no YAML.
- **015:** 2.3 `Accepted=False` and 2.4 `kgateway.dev/Programmed=False`; a parsed policy and an unparseable message; the 10-minute hint.
- **016:** controller-reported; computed missing target, missing section and empty selector; partial targets; the verifier raising KGWUI-012; scope unknown; a rule set with the controller reason, where the computed rule is off.
- **017:** a conflict with both sides named, and a conflict with no other side found.
- **018:** the threshold edges; maturation by deadline; a controller that is not ready gives 005, not 018.
- **019–021:** restart in the window, then expiry by deadline; OOMKilled; exit code 2; CrashLoopBackOff; probe failures from Events; `ProgressDeadlineExceeded`; image pull failure on the new ReplicaSet beside a ready old one.
- **022:** minor skew; patch skew (evidence only); a digest tag; suppression during a rollout; CRD bundle and channel mismatch.
- **023:** zero ready endpoints; unset `ready`; `ExternalName`; weight 0; `ResolvedRefs=False` (no 023); delegation and mirror refs (no 023); maturation.
- **024:** each metric case; the family absent on 2.3 and 2.4 gives "not available", with the install not unknown.

**KGWUI-001 re-specification:**
- per-type evidence and stream counts, summed over two replicas;
- the status-says-healthy message;
- `gateway-rejecting` row flags, and that such a row is never healthy;
- the restart hold, with a new Pod UID and with a higher restart count;
- `resources_updates_dropped_total > 0` turning KGWUI-007 into KGWUI-008.

**Check contract and Store:**
- every core, example and `examples/downstream` check runs on the new contract, with unchanged findings on the Phase 2 goldens;
- a Store value survives a recompute and is cleared on engine restart; checks cannot read each other's Store;
- a check whose declared inputs did not change is skipped, and one with a due deadline is not;
- a check that reads an undeclared input fails a test.

**Availability and `since`:**
- each new check with its watch off, denied and failing gives "not available" with that reason, and the install is not unknown;
- the existing required checks keep their flag;
- a status-derived finding carries `lastTransitionTime`; a UI-derived finding keeps its first-seen time across recomputes and resets on restart;
- the verdict's oldest problem and the age rank use each finding's own `since`.

**Identity, groups and rank:**
- the same input gives the same ids across recomputes and process restarts;
- rewording keeps the id;
- each discriminator separates what it must (two 012 checks on one resource);
- rank order on a fixture with every class;
- group membership is exclusive.

**Authorizer matrix:** every rule in §14 with a deny-some authorizer: restricted groups, filtered affected lists, hidden recently changed objects, next steps without hidden names, process sets without Pod details, and `q` not matching hidden messages. Health and ranks are unchanged.

**Metrics fixtures:** new captures from kgateway 2.3.11 and 2.4.7 with two rejecting types, two controller replicas and one controller restart. 2.5 captures for the per-client families and `kgateway_build_info`: in 3.1 from the release candidate if cheap, otherwise in 3.4 from 2.5.0 GA. A capture taken before GA is re-checked against GA in 3.4.

**Frontend (Vitest):**
- the install page with no groups, 10 groups and restricted groups;
- `NextSteps` copy and the permission notes;
- evidence renderers per kind;
- the stale state turning every group unknown;
- findings paging, grouping and filters through the URL;
- `renderWhenFontsReady`;
- breadcrumbs (§17.6): append, cut back on a cycle, collapse past 6 entries, the hierarchy fallback without `via`, an unresolvable or malformed entry, and a restricted crumb.

**Playwright (shell and navigation, §17.6):**
- Gateway → attached route → child route, then each crumb back, landing on the earlier page; the trail survives a reload and a copied URL;
- a list with filters → object → the list crumb restores the filters;
- the object header on every core detail page, with no layout shift as data arrives;
- layouts at 720, 1280, 1600 and 3008 px wide: no horizontal page scroll, two columns from 1600 px, content using the width up to 2400 px.

**kind e2e on 2.3.11 and 2.4.7:**
- the existing NACK fixture now asserts the status-says-healthy message, the stream count and the command text;
- a proxy with an unpullable image (021) through GatewayParameters;
- a backend scaled to zero (023);
- a missing ReferenceGrant (014);
- a wrong Service port (013);
- a policy with a missing target (016, computed);
- a conflicted ListenerSet listener (017);
- a killed controller Pod (019, and the restart hold on KGWUI-001).

**kind e2e on 2.5.0 (3.4, after GA):** a new matrix row running the suite above on the 2.5 rule set, with 016 from the controller's `TargetNotFound` and a withheld-config case for 024. Before GA, a 2.5 controller on the 2.4 rule set shows the "newer than tested" note and the computed 016 (an API boundary test).

**Benchmarks:** §16.3, in the bench workflow, in every sub-phase.

## 22. Proposed decisions

| # | Decision | Section |
|---|---|---|
| D-027 | The KRT decision rule and its outcome, recorded after the 3.1 spike | §16.1 |
| D-028 | Stable finding identity, groups and ranking | §5 |
| D-029 | Process health through `process.Source`; proxy Pods, Events and CRD metadata watched; termination messages and probe output never read | §9, §18 |
| D-030 | Specific codes replace KGWUI-002 for their reasons; KGWUI-001 re-specified instead of a new "status says healthy" code; a row whose Gateway rejects config is never healthy | §7.1, §8.3, §8.4 |
| D-031 | Backend readiness from Services and EndpointSlices, limited to Service refs the controller resolved | §11 |
| D-032 | The `health.Check` contract changes, breaking, to `Name`, `Inputs`, `Run(RunContext)`, with an engine-owned Store per check **(user, 2026-10-29)** | §16.2 |
| D-033 | kgateway 2.5 joins the support matrix at GA, in 3.4; before GA it runs on the 2.4 rule set **(user, 2026-10-29)** | §7.6, §8.6, §23 |
| D-034 | No Phase 3 check is required; unavailable checks give a reason and never turn the install unknown. `since` is the controller's time for status findings and the UI's first-seen time for UI-derived ones **(our call, 2.4 builder check)** | §5.1, §7.1 |
| D-035 | Path-based breadcrumbs carried in the URL (`via`), a shared object header, and a fluid layout up to 2400 px with two columns on detail pages from 1600 px **(user, 2026-10-29)** | §17.6 |
| D-036 | Deterministic suggested fixes: only where exactly one change follows from the spec, discovery or a watched object; a fragment with its location for existing objects; never applied, never from translation logic **(user, 2026-10-29)** | §7.7 |

The "Decisions for this phase" table near the top lists every decision from the builder check and the user's answers, with its marker.

## 23. Sub-phases

Each sub-phase has its own brief, a cross-model review of its diff, its benchmarks at Large, and ships alone. Every task lists its files for approval, under the change-size guardrail.

| Sub-phase | Content | Ships |
|---|---|---|
| **3.1 Foundations, performance and the KRT decision** | Performance work first: Phase 2 baselines, including the spec-edit stream variant; per-check memoization; the KRT spike and D-027. The new `health.Check` contract and the Store (D-032): every core, example and `examples/downstream` check migrated; README upgrade notes. Delegation memory carry-over. Finding `id`, `group` and rank fields; `since` per §5.1. `GET …/findings` and `GET …/findings/{id}`; the SPA findings page moves to server paging. Metrics: per Pod and per type, `rejects_total`, `resources_updates_dropped_total`. Scale tier extension. 2.5 fixtures if cheap. Shell and navigation (§17.6, D-035): `ObjectHeader`, the fluid and two-column layout, path-based breadcrumbs | Same pages, easier to read and navigate; findings have ids and page on the server |
| **3.2 Data plane, processes and the overview** | KGWUI-001 re-specified: evidence, `since`, restart hold, `gateway-rejecting`, next steps. `pkg/process` and the Kubernetes source; proxy Pod, Event and CRD metadata watches; 019, 020, 021, 022; the processes endpoint and page; the diagnostics endpoint; the install page's "Needs attention" and "Processes"; the Gateway Data plane and Proxies sections. RBAC (events, CRDs) and toggles; the README RBAC fix; `renderWhenFontsReady`. Wording pass | The health overview, ranked, with xDS rejects and process health |
| **3.3 Analyzer** | 013 (without Service data), 014, 015, 016 with the new `computed-rules.md` section and verifier, 017, 018, KGWUI-003 evidence kinds; groups for missing backend, not permitted, replaced by policy and status not progressing; findings grouped view; suggested fixes for 013 and 014 (§7.7); `docs/findings.md`. Wording pass | The analyzer floor and freshness findings, with suggested fixes |
| **3.4 Backends and kgateway 2.5** | Service and EndpointSlice watches with transforms; 023; 013's Service explanations; the `endpoints` capability and toggle. Once 2.5.0 GA ships (D-033): the 2.5 rule set, a 2.5 e2e row, 024, the controller-reported `TargetNotFound` path for 016, `kgateway_build_info` for the controller version, and the 2.5 fixtures if 3.1 did not capture them | Empty backends; kgateway 2.5 supported |

A stabilization sub-phase follows the phase review, as in Phases 1 and 2. Every sub-phase builds on 3.1's check contract, Store, identity and paging; 3.4's 013 explanation depends on 3.3's 013. Each of 3.2, 3.3 and 3.4 shows its benchmarks within the §16.3 gates before it ships. The order puts the research's top-ranked signals (rejects, processes) first, after the performance work. If 2.5.0 GA is not out when the rest of 3.4 is ready, the 2.5 items wait for it and the rest ships.

## 24. Upstream asks (draft text outside the repo; not filed)

| # | Ask | Motivation |
|---|---|---|
| 24.1 | A Gateway condition (or Event) when proxies NACK: xDS type, number of rejecting proxies, a bounded and sanitised reason, cleared on ACK (draft in the drafts folder) | §8; Phase 2 design §14.8 |
| 24.2 | A connected-proxies gauge per Gateway, and the reject's first-seen time | §8.3: "Programmed with no proxy connected" (#10613) and the restart hold |
| 24.3 | Machine-readable Gateway API compatibility metadata in the chart or the controller | §9.4; #13683 |
| 24.4 | Report unreported listener conflicts, such as duplicate SNI across TLSRoutes | §7.2; #14911 |
| 24.5 | A "config complete" signal for proxy readiness | §12; #14363 |

The scope-endpoint proposal (D-009 §7) stays the user's to file; 24.2 and 24.3 could join it. §25 holds the filing question.

## 25. Open item for the user

1. **Filing upstream.** File the reject-status draft (24.1) on its own, with the scope-endpoint issue, or under the Phase 2 umbrella proposal (Phase 2 design §14.3)? Filing both stays your call **(our call)**. Nothing in Phase 3 waits on it.
