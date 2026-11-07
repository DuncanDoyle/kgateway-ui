# Phase 3 — Health & Diagnostics (Complete)

**Completed:** 2026-11-07
**Range:** [`544dcd5`](https://github.com/DuncanDoyle/kgateway-ui/commit/544dcd5db3196cffd077267bca364ba8a8cd6d24)..[`4184c20`](https://github.com/DuncanDoyle/kgateway-ui/commit/4184c2078f762b4f894be79a3e13c9917376974a) (3.0–3.4 on `main`; 3.5 on `duncandoyle/stab35`, not pushed)

## Summary
The install page is now a ranked health overview. Each finding has a stable code and id, links to its resource, carries evidence, and suggests a fix when exactly one change is right. The UI reads xDS rejects per Gateway, controller and proxy process health, Envoy stats, Warning Events and backend endpoints, on kgateway 2.3, 2.4 and 2.5. A downstream distribution can add a whole feature (data, API, UI, chart values) without forking.

## Sub-phases (phase record only)
| Sub-phase | Kind | Issues | Description |
|---|---|---|---|
| 3.0 Extension seams | planned | — | Finding-code prefixes, base version, endpoints, capabilities, collectors, chart values, shell package and in-page slots; `examples/downstream` proves each in CI (D-038) |
| 3.1 Foundations, performance and the KRT decision | planned | — | Baseline, no KRT (D-027), one check contract with memoization (D-032), finding ids (D-040), breadcrumbs, delegation bounds |
| 3.2 Data plane, processes and the overview | planned | — | Ranked overview, KGWUI-001 per Gateway, codes 019–022, 025, 026, Processes page, proxy stats and Events |
| 3.3 Analyzer | planned | kgwctl#16 | Codes 013–018, KGWUI-003 kinds, suggested fixes, controller log signals; `pkg/setup` and the exported analyzer for kgwctl (E3, E8) |
| 3.4 Backends and kgateway 2.5 | planned | kgwctl#17, #20 | KGWUI-023 from Services and EndpointSlices, KGWUI-024, the `kgw25` rule set, version from `kgateway_build_info` |
| 3.5 Stabilization | stabilization | — | Phase review findings S1–S9 and the D-058 revisions (items 7, 10, 11) |

## Decisions
D-027 to D-060, all dated in Phase 3. All are accepted except D-060, which awaits the user's confirmation.

## Reviews
- `reviews/design-codex.md` — design and plan, 15 findings: 15 accepted, 0 rejected, 0 deferred (plus author items A1–A11)
- `reviews/phase-codex.md` — phase diff `544dcd5..3d7698e`, 6 findings: 5 accepted (fixed in 3.5), 1 rejected by amending §14 rule 11 (D-059, user)
- Sub-phase rounds, each triaged in its record: `3.0-codex.md`, `3.0-gate-codex.md`, `3.1-codex.md`, `-r2`, `-r3`, `3.2-codex.md`, `-rereview`, `3.3-codex.md`, `-rereview`, `-rereview2`, `3.4-codex.md`, `-rereview`, `3.5-codex.md`, `-rereview`. Every last round is clean.

## Waivers
None.

## Gates
- 3.5 standard gates on [`97e7e90`](https://github.com/DuncanDoyle/kgateway-ui/commit/97e7e908cbb26f2105b84b7d892f904e50b1b865) (`phase-3.5-complete.md`). Later commits change docs only (`git diff --stat 97e7e90..4184c20`).
- At the close, on [`4184c20`](https://github.com/DuncanDoyle/kgateway-ui/commit/4184c2078f762b4f894be79a3e13c9917376974a): `go vet ./...` and `go test ./...` pass (35 packages ok), 2026-11-07.

## Deliverables
See each sub-phase record. Main packages: `pkg/extension` seams, `pkg/collect`, `pkg/health` (check contract, identity), `pkg/ctrlmetrics`, proxy stats, process health, `pkg/computed/kgw25`, `pkg/setup`, `examples/downstream`, and the shell package (`make shell-pack`). No new Go module dependencies.

## Notes for later phases
- Memory is the tight budget: retained heap is +39.3 of 48 MiB in the measurement world (D-057).
- Releases `v0.3.0` and `v0.4.0` carry the program edges E2, E3 and E8 for kgwctl.
- The first release tag freezes API v1, install IDs and SPA URLs.

## Deferred / open
- D-060 awaits the user's confirmation.
- Push, PR and merge of 3.5 need the user's go.
- kgateway#14998 (upstream): once the controller reports a selector that matches nothing, S7's computed rule can go.
- `kind-main` e2e is skipped in CI; its expectation of 016 from status (S9) is unverified.
- R59: proxy Pod ownership by UID needs a ReplicaSet watch; the name check stays. The phase review did not take it up.
- D-058 item 1: KGWUI-023 has no proxy membership evidence; it needs xdsview `origin` (Phase 11 after D-061; Phase 9 before).
- D-058 item 6: DirectResponse status is not judged (Phase 7 after D-061; Phase 5 before).
- KGWUI-024's error (`warm_withheld`) is tested on a synthesised fixture only.
- 2.5 controller log captures, and 2.5's TLSRoute SNI conflict handling in view A.
- Dense peak live memory is logged, not gated (`reviews/3.0-gate-codex.md#2`).
- 018 runs outside the gated analyzer-stage scope.
- §8.9: CDS- and LDS-rejected names never appear in proxy stats, so those rejects keep only the error class.
- The kind e2e runs at the default `ALL` xDS scoping; `REFERENCED` runs only in the capture.
- Layout at 720 px: the top nav clips its first item; the Needs attention reservation has 2 px headroom.
- Minors parked in sub-phase records 3.0–3.4 (SDD ledger items, R60, R65, R74, S349 plural, the "EndpointSlices" label spacing).

Resolved during the phase and dropped: R57 (3.4), R96 (S8 removed the status-sync count), the unreadable-version rule set (S9), the memoized analyzer gate (3.2), the delegation memory bounds and spec-edit stream (3.1), stable finding identity (3.1), font loading for `Shell` embedders (3.2), and the 3.0 seam carry-overs (prefix registry, base version, analyzers and API routes).
