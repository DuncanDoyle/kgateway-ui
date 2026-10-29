# Phase 2 — Gateway & Route Explorer (Complete)

**Completed:** 2026-10-29
**Range:** [`6b5dcc2`](https://github.com/DuncanDoyle/kgateway-ui/commit/6b5dcc2)..[`8b988e1`](https://github.com/DuncanDoyle/kgateway-ui/commit/8b988e1)

## Summary
An operator can now explore Gateways and routes, not just count them:
- **Lists:** Gateways, ListenerSets and routes, paged, filtered and sorted on the server. Filters include health, listener, `backend=` and delegation flags such as `dropped`.
- **Detail pages:** a Gateway shows its listeners, ports, hostnames, TLS mode, addresses and attached routes. A ListenerSet shows its listeners and parent. An HTTPRoute shows its rules, matchers, filters, parentRefs, backends, conditions and child routes. Every page has a YAML tab, and links go both ways: route to gateway and gateway to routes.
- **Delegation:** delegated child routes count when the controller reports them. A parent route's Delegation tab lists, per rule and ref, the routes it selects and why each is kept or dropped. A child route shows the matches it really serves under each parent.
- **Computed and cross-checked:** the UI computes listener attachment (view A) and delegation (views B and C) with rules that mirror kgateway 2.3 and 2.4. Each computed fact is labelled and checked against the controller's status. A disagreement that lasts 30 s raises KGWUI-012, a warning that never turns an install red.
- **New findings:** KGWUI-011 for declared, dropped, not-permitted and orphan child routes; KGWUI-012 for disagreements.
- **Access:** every row, count, link and computed fact goes through the per-request Authorizer. Hidden facts read "Hidden".
- **Freshness:** health recomputes when something changes, not on a 2 s ticker. Summaries carry a `confirmedAt` time.
- **Bounds:** delegation work has install-wide caps; past them a parent reads "too large to compute". The edge cap is configurable, and the binary sets its Go memory limit to 90 % of the pod limit.

## Sub-phases (phase record only)
| Sub-phase | Kind | Issues | Description |
|---|---|---|---|
| 2.1 | planned | #7 ([`35f105d`](https://github.com/DuncanDoyle/kgateway-ui/commit/35f105d)) | Event-driven recompute, atomic publication and `confirmedAt` (D-019, D-021) |
| 2.2 | planned | #8 ([`f01bb0a`](https://github.com/DuncanDoyle/kgateway-ui/commit/f01bb0a)) | Explorer lists and detail pages on observed status, Backend kind, KGWUI-011 |
| 2.3 | planned | #9 ([`86c70a4`](https://github.com/DuncanDoyle/kgateway-ui/commit/86c70a4)) | View A: computed listener attachment, cross-check and KGWUI-012 (D-022, D-023) |
| 2.4 | planned | #10 ([`b829c5d`](https://github.com/DuncanDoyle/kgateway-ui/commit/b829c5d)) | Views B and C: computed delegation, Delegation tab, effective matches, edge cap (D-024, D-025) |
| 2.5 | stabilization | #11 ([`8b988e1`](https://github.com/DuncanDoyle/kgateway-ui/commit/8b988e1)) | Fixes for the 7 accepted phase review findings (D-026) |

## Reviews
- `reviews/design-codex.md` — 18 findings in three rounds: 18 accepted, 0 rejected, 0 deferred.
- `reviews/design-r4-codex.md` — 26 findings in five rounds: 26 accepted, 0 rejected, 0 deferred. The scope split into 2.1–2.4 was accepted by the user (2026-10-22).
- `reviews/plan-codex.md` — 25 findings in three rounds: 25 accepted, 0 rejected, 0 deferred. Finding 7 needed a second fix (F7). Of the 8 spec-gap resolutions, 7 were accepted and 1 objected to (finding 9).
- `reviews/2.1-codex.md` — 4 findings: 4 accepted, 0 rejected, 0 deferred.
- `reviews/2.2-codex.md` — 5 findings: 5 accepted, 0 rejected, 0 deferred.
- `reviews/2.3-codex.md` — 8 findings: 8 accepted, 0 rejected, 0 deferred. The fixes for #1 and #2 were superseded by [`4f82c36`](https://github.com/DuncanDoyle/kgateway-ui/commit/4f82c36) (user, 2026-10-26).
- `reviews/2.4-codex.md` — 11 findings: 9 accepted, 0 rejected, 2 deferred (F1 and F3, P3 carry-over).
- `reviews/phase-codex.md` — 8 findings: 7 accepted (fixed in 2.5), 0 rejected, 1 deferred (finding 8, to Phase 3 planning).
- `reviews/2.5-codex.md` — 4 findings: 4 accepted, 0 rejected, 0 deferred.
- Every sub-phase also had per-task spec and quality reviews; see the sub-phase records.

## Waivers
none

## Deliverables
- **Engine:** `pkg/trigger`, the scheduler and atomic publication in `pkg/engine`, cache fingerprints, the decode cache, views and their memo.
- **Ownership and health:** status-based assignment and delegation in `pkg/install`; KGWUI-011 and KGWUI-012 and the Backend checks in `pkg/health`.
- **Computed rules:** `pkg/computed` (attachment, delegation, checks, fingerprints) with `kgw23`, `kgw24` and `rulesets`; downstream builds register their own with `extension.WithComputedRules`.
- **Explorer:** `pkg/explore` (indexes, rows, queries, cursors, ancestors) and the list, detail and delegation endpoints in `pkg/api`, with goldens and the Authorizer matrix.
- **SPA:** the Gateways, Routes and ListenerSets lists; the Gateway, ListenerSet and HTTPRoute pages; `ComputedBadge`, `DelegationTab` and `EffectiveMatches`; optional `CorePages` slots. `SHELL_API_VERSION` stays 1.
- **Chart and binary:** `extraArgs`, `delegation.edgeCap` and `--delegation-edge-cap`, `KGWUI_MEMORY_LIMIT` and `pkg/app/memlimit.go`.
- **Tests:** scale tiers in `internal/testutil/scale`, budget tests and benchmarks (`make bench`, `make bench-stream`), the `bench` workflow, records in `bench/`, and kind e2e for the explorer, view A and delegation on kgateway 2.3 and 2.4.
- **Docs:** `design.md`, `computed-rules.md`, decisions D-019 to D-026, wording and evidence records per sub-phase, README upgrade notes, `docs/documentation-notes.md`.
- No new runtime dependencies.

## Release note
In README "Upgrade notes", one section per sub-phase (2.2, 2.3, 2.4). In short:
- explorer lists and detail pages, with delegated children that count when reported;
- computed listener counts, delegation and effective matches, labelled and cross-checked;
- KGWUI-011 (declared, dropped, not permitted, orphan) and KGWUI-012 (disagreement, never red);
- "Hidden" for restricted facts and `scope-unknown` for an unknown discovery scope;
- the edge cap (`--delegation-edge-cap`, `delegation.edgeCap`) and the Go memory limit at 90 % of the pod limit;
- `extension.WithComputedRules`, `CorePages` slots and the chart's `extraArgs`.

Sub-phase 2.5 fixes behaviour the design already stated; it needs no upgrade note.

## Exit gates
- [x] **tests/vet pass (observed):** CI on PR #11 passed on [`ebbc70b`](https://github.com/DuncanDoyle/kgateway-ui/commit/ebbc70b): go, web, helm, hygiene, image, bench, and kind e2e on v2.3.11, v2.4.7 and v2.4.7 with a second install. The merge [`8b988e1`](https://github.com/DuncanDoyle/kgateway-ui/commit/8b988e1) has the same tree, so this is main.
- [x] **All queue items done:** Q1–Q9 in `phase-status.md`, each with its record.
- [x] **Design+plan cross-model review + triage:** `reviews/design-codex.md`, `design-r4-codex.md`, `plan-codex.md`; plan approved by the user 2026-10-23.
- [x] **Phase cross-model review + triage:** `reviews/phase-codex.md`; 7 findings fixed in 2.5 and confirmed by `reviews/2.5-codex.md`, 1 deferred.
- [x] **decisions.md / design / plan in sync with code:** D-019 to D-026 recorded. The one drift the phase review found (plan Task 44) was fixed in 2.5 (Q6, [`a5ea505`](https://github.com/DuncanDoyle/kgateway-ui/commit/a5ea505)). The last gate change ([`ebbc70b`](https://github.com/DuncanDoyle/kgateway-ui/commit/ebbc70b)) updated D-025 and the design with the test.
- [x] **This record written, roadmap updated, tracked items closed:** this file; `docs/roadmap.md`; `docs/phase-status.md` reset for Phase 3.

## Notes for later phases
- **Open design question for Phase 3 planning:** move the engine to incremental recomputes, for example with KRT collections. The roadmap holds the question and the spike to run first.
- **Headroom is thin.** Phase 3 adds analyzers to the same recompute, so measure early:
  - warm recompute on CI: 104–125 ms against 150 ms (`bench/2.4-ci.txt`; 100–120 ms in PR #11's bench job);
  - stream CPU: about 10–12 % against 15 %, through the views memo, since the stream edits annotations only;
  - dense memory gate: median peak heap 416 MiB against 432 MiB (PR #11's bench job);
  - retained heap: +23 to +25 MiB over the 2.1 baseline against 32 MiB (PR #11's bench job).
- **User docs:** `docs/documentation-notes.md` is the backlog of topics the user documentation must cover, starting with the edge cap and memory sizing.
- **Compute caps are install-wide** (design §7.4 rule 4d). A per-root budget would change outcomes and needs a design change.
- **Next milestone:** the extension seam proof runs after Phase 2 (roadmap).
- **Bench time:** the CI `bench` job takes about 12 min; watch its timeout as gates grow.
- **Plan as history:** `implementation-plan.md` is the historical plan; where it differs from the code, the decisions and sub-phase records win.

## Deferred / open
Carried in `docs/phase-status.md`:
- (P3) Delegation memory: bound generated path bytes in inherit merges, and charge failure-reason text and JSON escaping in the page budget — 2.4 Codex final round F1 and F3, known limits.
- (P3) Stream CPU gate measures the views-memo hit path; add a reported spec-edit stream variant — 2.4.
- (P3) Scope verification for experimental Gateway API kinds waits on the upstream scope endpoint (D-017 §4).
- (P3) Reminder: file the upstream scope-endpoint issue (H9, D-009 §7) — the user chose a later phase.
- (P3) #1 Watch CRDs added or removed after startup without a restart.
- (P3) Per-user AuthN/AuthZ behind `api.Authorizer` (D-013) — roadmap.
- (P4) Stable finding identity for Phase 3 diagnostics — phase review finding 8.
- (P4) Apps that embed `Shell` must wait for fonts before rendering, as `web/app/src/main.tsx` does, or long names shift the layout — 2.4.
- (P4) Downstream detectors cannot report their base kgateway version — seam milestone.
- (P4) Extension seams: finding-code prefix registry; analyzers, API routes, renderers, health cards — seam milestone.
- (P4) Design §14.4 refactors before the seam milestone.
- (P4) Optional `kind-main` e2e job.

Phase 2 items kept in the sub-phase records:
- (P3) Parked: after a cap, a row reads `too-large` where it might be `not-reached` (Task 57); the seed route sits in its own closure (Task 58) — 2.4.
- (P3) The 20-client list p95 runs 20 in-process clients; if it flakes, move them out of process or tie the count to GOMAXPROCS — 2.3.
- (P4) The forced-disagreement e2e runs only in the v2.4.7 row — 2.3.
- (P4) The version regexp is duplicated in `rulesets.Select` and `kinds` — 2.3.
- (P4) A pre-2.3 server shifts the layout once when `/meta` is slower than the detail — 2.3.
- (P4) The scale generator panics on tiers with fewer than 10 Gateways or no ListenerSets — 2.3.
- (P4) CheckResult slimming stays available if allocation headroom is needed — 2.3.
- (P4) No overlap warning between downstream routes and core pages, beyond reserved paths — 2.2.
- (P4) No test for retry on unmount or with two consumers of one key — 2.2.
- (P4) No test that the scope-mismatch deadline clears once the mismatch resolves — 2.1.
- (P4) The `allows` labels are not rendered; their wording stays in `phase-2.4-wording.md` — 2.4.
