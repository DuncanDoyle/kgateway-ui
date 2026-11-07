# Phase status

## Phase
Phase: 3 — Health & Diagnostics
Base: 544dcd5db3196cffd077267bca364ba8a8cd6d24
Docs: docs/phases/phase-3/
Status: implementing (design and plan approved 2026-11-01); Phase 2 closed 2026-10-29 (phases/phase-2/phase-2-complete.md)

### Queue
- [x] Q1 Phase 3 design (`design.md`) — approved by the user 2026-11-01
- [x] Q2 Phase 3 implementation plan (`implementation-plan.md`) — 87 tasks, sub-phases 3.0–3.4 (2026-11-01)
- [x] Q3 Cross-model review of design + plan, triage, user approval — `reviews/design-codex.md`, 15 findings accepted and applied; user approved all of 3.0–3.4 on 2026-11-01 ("I approve the entire phase 3, so 3.0 to 3.4")
- [x] Q4 Sub-phase 3.0 Extension seams (Tasks 1–11) — closed 2026-11-02, phases/phase-3/phase-3.0-complete.md
- [x] Q5 Sub-phase 3.1 Foundations, performance and the KRT decision (Tasks 12–33) — closed 2026-11-02, phases/phase-3/phase-3.1-complete.md
- [x] Q6 Sub-phase 3.2 Data plane, processes and the overview (Tasks 34–54) — closed 2026-11-04, phases/phase-3/phase-3.2-complete.md
- [x] Q7 Sub-phase 3.3 Analyzer (Tasks 55–73, 71a) — closed 2026-11-05, phases/phase-3/phase-3.3-complete.md
- [x] Q8 Sub-phase 3.4 Backends and kgateway 2.5 (Tasks 74–87, 78a, 78b) — closed 2026-11-06, phases/phase-3/phase-3.4-complete.md
- [x] Q9 Phase review (Codex) — `reviews/phase-codex.md`, triaged 2026-11-06: 5 accepted, 1 for the user
- [x] Q10 Stabilization 3.5 — closed 2026-11-07 (local, not pushed), phases/phase-3/phase-3.5-complete.md
  - [x] S1 (P2) Proxy-stat Envoy names reach restricted viewers through the namer seam — 87b38f3: names now filtered as their mapped objects
  - [x] S2 (P2) A list-only kind change raises KGWUI-008 through `kinds/monitor.go` — 7be3980: `kinds.Change` splits list-only kinds; a note on the OK check
  - [x] S3 (P2) KGWUI-024 missing from the processes and data-plane summaries — ed4625b, 97e7e90: processes, diagnostics, overview cell and Gateway headline
  - [x] S4 (P3) The resource-status check reads Services without declaring them — c7fb8a1: declared; guard worlds added
  - [x] S5 (P4) 023 attribution is quadratic in backend fan-in — 92c928e: linear, 251 → 44 ms at 10,000 routes
  - [x] S7 (P3) On 2.5, a selector that matches nothing shows 003 instead of 016 — 7f6336d: kgw25 judges selectors only; exit kgateway#14998
  - [x] S8 (P3) Remove the status-sync count (`status_syncs_total`) from KGWUI-018 and the UI on every version — 5a2253e
  - [x] S9 (P3) An unreadable controller version selects the 2.5 rule set, not 2.4 — c3034b8
  - [x] S6 (P3) 015 group ID names the policy for restricted viewers — no code: §14 rule 11 amended (D-059, user 2026-11-07)
### Exit gates
- [x] tests/vet pass (observed) — 3.5 gates on 97e7e90, 2026-11-07
- [x] all queue items done or deferred
- [x] design+plan cross-model review + triage: docs/phases/phase-3/reviews/design-codex.md
- [x] phase cross-model review + triage: docs/phases/phase-3/reviews/phase-codex.md
- [ ] decisions.md / design / plan in sync with code
- [ ] phase-3-complete.md written, roadmap updated, tracked items closed

## Sub-phase
Sub-phase: none (3.5 closed 2026-11-07 on `duncandoyle/stab35`, not pushed; the Phase 3 close is next, with the user's go)
Kind: n/a
Status: n/a
Base: n/a
Brief: n/a

## Program
Program file: https://github.com/DuncanDoyle/kgateway-tooling/blob/main/program.md
Waiting on: none
Consumers: E2 (kgwctl Phase 1), E3 and E8 (kgwctl Phases 5 and 2): delivered in `v0.3.0` (2026-11-05, kgwctl#3) and updated in `v0.4.0` (2026-11-07, kgwctl#4). kgwctl's E2 requests: #17 and #20 shipped in `v0.4.0` and closed; #18 and #19 commented as moving to the Traffic Flow & Backends phase (currently Phase 4) (D-056). E7 (kgwctl Phase 6, policy model under `pkg/`) is UI Phase 5.
Provided to this repo: E4 and E5 from xdsview `v0.3.0` (renamed from krp; #15, #21) are for UI Phase 9; 3.4 does not consume them.

## Carry-over (deferred to later phases)
- (P3) REMIND USER at the start of each phase: file the upstream kgateway issue "Expose the controller's effective discovery scope through a read-only endpoint" (H9, D-009 §7). User chose to file it in a later phase (2026-10-20). Draft kept outside the repo (public repo). Before filing: confirm on a cluster that status stays after a namespace is deselected, and re-verify the file:line refs against kgateway main.
- (P3) Delegation memory: bound generated path bytes in inherit merges and charge failure-reason text and JSON escaping in the page budget — src: phases/phase-2/reviews/2.4-codex.md (final round) — done in 3.1 (phases/phase-3/phase-3.1-complete.md)
- (P3) Stream CPU gate measures the views-memo hit path (annotation-only edits); add a reported spec-edit stream variant — src: phases/phase-2/phase-2.4-complete.md — done in 3.1 (phases/phase-3/phase-3.1-complete.md)
- (P3) Scope verification for experimental Gateway API kinds: switch KGWUI-007 to the controller's enabled kinds once the upstream scope endpoint exists (D-017 §4) — src: phases/phase-1/reviews/phase-codex.md#5
- (P3) Watch CRDs added or removed after startup without a restart — https://github.com/DuncanDoyle/kgateway-ui/issues/1
- (P3) Per-user AuthN/AuthZ behind `api.Authorizer` (D-013) — roadmap — src: phases/phase-1/phase-1.4-complete.md
- (P4) Stable finding identity for Phase 3 diagnostics — src: phases/phase-2/reviews/phase-codex.md#8 — done in 3.1 (D-028, D-040)
- (P4) Apps that embed `Shell` must wait for fonts before rendering, as `web/app/src/main.tsx` does, or long names shift the layout on load — src: phases/phase-2/phase-2.4-complete.md — done in 3.2 (`renderWhenFontsReady`, D-045)
- (P4) Downstream detectors cannot report their base kgateway version, so experimental classification is conservative for them — seam milestone — src: phases/phase-1/phase-1.5-complete.md
- (P4) Extension seam: finding-code prefix registry (Phase 1 uses a documented convention) — for the seam milestone after Phase 2 — src: phases/phase-1/phase-1.1-complete.md
- (P4) Extension seam: analyzers, API-route registration, per-kind renderers, health cards — for the seam milestone after Phase 2 — src: phases/phase-1/reviews/design-codex.md#16
- (P4) Refactors of design §14.4, triggered by the standalone PoC; neutral `Install` and `Detector` before the seam milestone
- (P4) Optional `kind-main` e2e job: run on manual dispatch with `kgw_main_version`; chart source unverified — src: phases/phase-1/phase-1.4-complete.md
- (P3) Dense memory: peak live memory at the default edge cap is logged, not gated (D-039); a regression that holds more at once without allocating more passes. Candidate: a child with the production soft limit, gating peak RSS against the pod limit — src: phases/phase-3/reviews/3.0-gate-codex.md#2
- (P3) The 3.2 memoized analyzer gate (≤ 2 ms) needs narrower inputs: memoized runs 8.1 ms at the worst case, because Phase 1–2 checks declare `Clock` (ruling R20) — src: phases/phase-3/phase-3.1-complete.md — done in 3.2 (§16.3 scope, R35; measurement world 0.21 ms, 1.14 ms with a scrape per run, D-052)
- (P3) Retained heap headroom is 1.2 MiB in the 3.2 measurement world (+30.8 of 32 MiB); 3.3 and 3.4 add to it — src: phases/phase-3/phase-3.2-complete.md — gate 40 MiB in 3.3 (D-054), 48 MiB in 3.4 after shrinking the endpoint facts (D-057); 3.4 measures +39.3
- (P3) R96: a sync-only scrape publishes a new model (0.10/s idle) with no finding change — src: phases/phase-3/phase-3.3-complete.md — measured in 3.4 (0.10/s, costs no gate); stays
- (P3) R57: re-seed Services and EndpointSlices in the measurement world with the 3.4 watches — src: phases/phase-3/phase-3.2-complete.md — done in 3.4 (bench/3.4.txt)
- (P3) R59: proxy Pod ownership by UID needs a ReplicaSet watch; the name check (R50) stays until then — src: phases/phase-3/phase-3.2-complete.md — not in 3.4's tasks; for the phase review
- (P3) D-058 items 1 and 6: 023 proxy membership evidence (needs a port-aware reverse mapping, Phase 9); DirectResponse status not judged (Phase 5) — src: phases/phase-3/phase-3.4-complete.md
- (P3) A live `warm_withheld` capture for KGWUI-024's error; on kind the gauge never outlasts a scrape — src: phases/phase-3/phase-3.4-complete.md
- (P4) 2.5 controller log captures; 2.5 TLSRoute SNI conflict handling in view A; an unreadable controller version selects the 2.4 rule set — src: phases/phase-3/phase-3.4-complete.md
- Smaller Phase 2 items (parked Tasks 57 and 58, test gaps, P4 polish) — src: phases/phase-2/phase-2-complete.md, Deferred / open

## Notes
- 2026-11-06: Sub-phase 3.4 closed (phases/phase-3/phase-3.4-complete.md). Two Codex rounds triaged (3 P1, all reproduced and fixed; the re-review clean). The user decided D-056 and D-057 and the export of `computed.TargetStatusReporter`; D-058 records the deviations (item 6 confirmed, the rest pending). Standard gates pass on 8fa399b; `make bench` passes on 31e97db under `caffeinate -s`; kind e2e passes on all three pairs at 3d7698e. The unpushed branch was rewritten to drop a forbidden word from one test fixture. Next: land on `main` and tag `v0.4.0` (user), then the phase review.
- 2026-11-06: Sub-phase 3.4 started at base 8342a38 (3.0–3.3 merged in PR #22, tagged `v0.3.0`). Branch `duncandoyle/backends`. kgateway 2.5.0 GA is out (2026-11-02), so Tasks 83–86 run now. The user added kgwctl #17 and #20 (D-056). The brief's `(inferred)` outputs go to the user before their tasks.
- 2026-11-05: Sub-phase 3.3 closed (phases/phase-3/phase-3.3-complete.md). Three Codex rounds triaged (6 + 2 findings, all fixed; the last round clean). The user raised the retained-heap gate to 40 MiB (D-054) and confirmed the 3.3 rulings; D-055 records the deviations (all confirmed). Standard gates and `make bench` pass on c97c3eb; kind e2e passes on both pairs. Next: land on `main` and tag for kgwctl (#16), then 3.4.
- 2026-11-04: Sub-phase 3.3 started at base 79a5307. Brief phases/phase-3/phase-3.3-brief.md; its `(inferred)` outputs and six plan points go to the user before their tasks. The user added E8 to 3.3 (Task 71a, D-053). Retained heap headroom is 1.2 MiB in the measurement world.
- 2026-11-04: Sub-phase 3.2 closed (phases/phase-3/phase-3.2-complete.md). Codex review and re-review triaged; the user decided D-045, D-046 and D-047, and D-048 … D-052 record the design deviations. Standard gates pass on e4416b8; `make bench` passes on ce9983b. CI on fd4fe3c (bench32): kind e2e passed on all three rows with NACK=1 and the bench passed with the D-046 and D-047 gates. 3.3 is next; the user starts it.
- 2026-11-02: Sub-phase 3.2 started (user). Brief phases/phase-3/phase-3.2-brief.md. Two gates are tight at the start: the memoized analyzer stage fails today (R20), and the CI warm worst case is 25 ms against 20; retained heap headroom is 4.2 MiB (local) in the contradicting world.
- 2026-11-02: Sub-phase 3.1 closed (phases/phase-3/phase-3.1-complete.md). D-027 keeps the full recompute. Three Codex rounds triaged; the user decided D-040 (finding ids), D-041 (breadcrumbs through `/refs`) and D-042 (side column from 1600 px), replacing R24, R31 and R30. Gates pass locally; kind e2e passed on all three rows at 205e164.
- 2026-11-02: Sub-phase 3.1 started. Baseline recorded (bench/3.1-baseline.txt, -ci.txt): every gate passes on 544dcd5 and 3.0; spec-edit 11.0 % local (10 min), 14.0 % CI (60 s). Throwaway branch `duncandoyle/baseline3` pushed for the CI run (user go), never merged. The user raised the D-039 cold-allocation budget to 246 MiB (CI measures 224) and approved Task 15 after seeing D-027.
- 2026-11-02: Sub-phase 3.0 closed (phases/phase-3/phase-3.0-complete.md). Both blocked gates cleared: kind e2e passed on all three e2e.yaml rows locally; the dense gate changed per D-039. 3.1 Task 12 needs the user's go for a throwaway CI push.
- 2026-11-01: Close blocked (3.0): (1) kind e2e not run — Docker (colima) is not running locally; needs the user's go to start it or to push for CI. (2) `TestBudgetDenseMemory` exceeds 432 MiB intermittently on HEAD and on the base alike (HEAD 447 under load; 10 interleaved rounds HEAD 6/10 and base 9/10 pass; base peaked at 440). Not a 3.0 regression: warm recompute bytes and mallocs equal at both commits, retained heap equal, pprof diff −0.03 %. The gate's margin needs the user's decision.
- 2026-11-01: The user approved changing the dense gate: D-039 replaces median peak heap with medians of retained heap ≤ 176 MiB, cold-recompute bytes ≤ 231 MiB and bytes per warm recompute ≤ 185 MiB (HEAD and base both 160, 210 and 168 MiB, identical in all runs; peak median 427–440, logged). Colima started for the kind e2e. All other gates pass; Codex review `reviews/3.0-codex.md` triaged, 4 findings fixed in b129432. Bench record `phases/phase-3/bench/3.0.txt`.
- 2026-11-01: The user approved the Phase 3 design and plan, all sub-phases 3.0–3.4, in one approval. D-028 … D-038 appended to `decisions.md`. Sub-phase 3.0 started on branch `duncandoyle/phase3Seams` (stacked on `duncandoyle/phase3Design`).
- 2026-11-01: The extension seam milestone runs as sub-phase 3.0, before 3.1 (user). The seam carry-overs below move into 3.0 (design §15). Design review not yet done; no code before the user approves design and plan.
- 2026-10-29: Phase 3 started at base 544dcd5. Design draft `phases/phase-3/design.md` written from the roadmap and the Phase 3 research (kept outside the repo, because the repo is public). The KRT question is framed as D-027, decided after a spike in 3.1. A second upstream draft (xDS reject status) sits next to the scope-endpoint draft outside the repo; filing both stays the user's call.
- 2026-10-29: Phase 2 closed. All exit gates held (CI on PR #11 green; merge 8b988e1 has the same tree). No waivers. Record: phases/phase-2/phase-2-complete.md.
- The first release tag freezes API v1, install IDs and SPA URLs; make the ghcr packages public first (phase-1-complete.md).
- The repo is public: per the public-repo hygiene rules, vendor and commercial edition names must not appear in code, file names, commit messages or PR text. Refer to them as "downstream distributions".
