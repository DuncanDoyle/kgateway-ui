# Phase status

## Phase
Phase: 4 — Snellius rebrand
Base:
Docs: docs/phases/phase-4/
Status: not started (pre-approved by the user 2026-11-07); Phase 3 closed 2026-11-07 (phases/phase-3/phase-3-complete.md)

### Queue
(empty; filled from the Phase 4 design and plan)

### Exit gates
- [ ] tests/vet pass (observed)
- [ ] all queue items done or deferred
- [ ] design+plan cross-model review + triage: docs/phases/phase-4/reviews/design-codex.md
- [ ] phase cross-model review + triage: docs/phases/phase-4/reviews/phase-codex.md
- [ ] decisions.md / design / plan in sync with code
- [ ] phase-4-complete.md written, roadmap updated, tracked items closed

## Sub-phase
none

## Program
Program file: https://github.com/DuncanDoyle/kgateway-tooling/blob/main/program.md
Waiting on: none
Consumers: E2 (kgwctl Phase 1), E3 and E8 (kgwctl Phases 5 and 2): delivered in `v0.3.0` (2026-11-05, kgwctl#3) and updated in `v0.4.0` (2026-11-07, kgwctl#4). kgwctl imports this module, so the Phase 4 module path rename needs a cross-repo notice. kgwctl's E2 requests: #17 and #20 shipped in `v0.4.0` and closed; #18 and #19 commented as moving to the Traffic Flow & Backends phase (Phase 6; Phase 4 before D-061) (D-056). E7 (kgwctl Phase 6, policy model under `pkg/`) is UI Phase 7.
Provided to this repo: E4 and E5 from xdsview `v0.3.0` (renamed from krp; #15, #21) are for UI Phase 11.

## Carry-over (deferred to later phases)
- (P3) REMIND USER at the start of each phase: file the upstream kgateway issue "Expose the controller's effective discovery scope through a read-only endpoint" (H9, D-009 §7). User chose to file it in a later phase (2026-10-20). Draft kept outside the repo (public repo). Before filing: confirm on a cluster that status stays after a namespace is deselected, and re-verify the file:line refs against kgateway main.
- (P3) D-060 (3.5 as built) awaits the user's confirmation — src: phases/phase-3/phase-3.5-complete.md
- (P3) Push, PR and merge of `duncandoyle/stab35` (3.5 and the Phase 3 close) need the user's go
- (P3) Scope verification for experimental Gateway API kinds: switch KGWUI-007 to the controller's enabled kinds once the upstream scope endpoint exists (D-017 §4) — src: phases/phase-1/reviews/phase-codex.md#5
- (P3) Watch CRDs added or removed after startup without a restart — https://github.com/DuncanDoyle/kgateway-ui/issues/1
- (P3) Per-user AuthN/AuthZ behind `api.Authorizer` (D-013) — roadmap — src: phases/phase-1/phase-1.4-complete.md
- (P3) kgateway#14998 (upstream): once the controller reports a selector that matches nothing, the 3.5 S7 computed rule can go — src: phases/phase-3/phase-3.5-complete.md
- (P3) R59: proxy Pod ownership by UID needs a ReplicaSet watch; the name check (R50) stays until then — src: phases/phase-3/phase-3.2-complete.md
- (P3) D-058 item 1: 023 proxy membership evidence needs a port-aware reverse mapping (xdsview `origin`, Phase 11) — src: phases/phase-3/phase-3.4-complete.md
- (P3) D-058 item 6: DirectResponse status is not judged (Phase 7) — src: phases/phase-3/phase-3.4-complete.md
- (P3) Dense memory: peak live memory at the default edge cap is logged, not gated (D-039). Retained heap is +39.3 of 48 MiB (D-057); Phase 5 must keep the shared cache in the core — src: phases/phase-3/reviews/3.0-gate-codex.md#2
- (P3) A live `warm_withheld` capture for KGWUI-024's error; on kind the gauge never outlasts a scrape — src: phases/phase-3/phase-3.4-complete.md
- (P4) Neutral `Install` and `Detector` (design §14.4 refactors) — Phase 5 G1 (D-061)
- (P4) Optional `kind-main` e2e job: run on manual dispatch with `kgw_main_version`; chart source unverified. Its 016-from-status expectation (3.5 S9) is unverified too — src: phases/phase-1/phase-1.4-complete.md, phases/phase-3/phase-3.5-complete.md
- (P4) 2.5 controller log captures; 2.5 TLSRoute SNI conflict handling in view A — src: phases/phase-3/phase-3.4-complete.md
- Smaller Phase 2 and Phase 3 items (parked tasks, test gaps, layout at 720 px, P4 polish) — src: phases/phase-2/phase-2-complete.md and phases/phase-3/phase-3-complete.md, Deferred / open

## Notes
- 2026-11-07: Phase 3 closed (phases/phase-3/phase-3-complete.md). D-061 adds Phase 4 (Snellius rebrand) and Phase 5 (Generic Gateway API core); the old Phases 4–11 are now 6–13. Phase 4 is pre-approved; set Base when it starts, then write its design.
- The first release tag freezes API v1, install IDs and SPA URLs; make the ghcr packages public first (phase-1-complete.md).
- The repo is public: per the public-repo hygiene rules, vendor and commercial edition names must not appear in code, file names, commit messages or PR text. Refer to them as "downstream distributions".
