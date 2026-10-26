# Phase status

## Phase
Phase: 2 — Gateway & Route Explorer
Base: 6b5dcc2b6e69854496a47ed904f2b13d715eeff2
Docs: docs/phases/phase-2/
Status: design and plan approved 2026-10-23; sub-phase 2.1 complete, merged in #7 (35f105d); sub-phase 2.2 complete, merged in #8 (f01bb0a); sub-phase 2.3 complete, merged in #9 (86c70a4); sub-phase 2.4 active (phase-2.4-brief.md); Phase 1 complete (phases/phase-1/phase-1-complete.md, merged through #6, 6cd6111)

### Queue
- [x] Q1 Phase 2 design (`design.md`, `computed-rules.md`, D-019, D-020) — approved by user 2026-10-23; reviews: reviews/design-codex.md, design-r4-codex.md
- [x] Q2 Phase 2 implementation plan (`implementation-plan.md`), planned sub-phases 2.1–2.4 (Tasks 1–70)
- [x] Q3 Cross-model review of design + plan, triage, user approval — reviews/design-codex.md, design-r4-codex.md, plan-codex.md; plan approved by user 2026-10-23 (with chart `extraArgs`, Task 50)
- [x] Q4 Sub-phase 2.1 — event-driven recompute (plan Tasks 1–10) — phases/phase-2/phase-2.1-complete.md; merged in #7 (35f105d)
- [x] Q5 Sub-phase 2.2 — explorer on observed status (Tasks 11–34) — phases/phase-2/phase-2.2-complete.md; review reviews/2.2-codex.md; merged in #8 (f01bb0a)
- [x] Q6 Sub-phase 2.3 — view A (Tasks 35–51) — phases/phase-2/phase-2.3-complete.md; review reviews/2.3-codex.md; merged in #9 (86c70a4)
- [ ] Q7 Sub-phase 2.4 — views B and C (Tasks 52–70) ← CURRENT
- [ ] Q8 Phase cross-model review + triage

### Exit gates
- [ ] tests/vet pass (observed)
- [ ] all queue items done or deferred
- [x] design+plan cross-model review + triage: docs/phases/phase-2/reviews/design-codex.md, design-r4-codex.md, plan-codex.md
- [ ] phase cross-model review + triage: docs/phases/phase-2/reviews/phase-codex.md
- [ ] decisions.md / design / plan in sync with code
- [ ] phase-2-complete.md written, roadmap updated, tracked items closed

## Sub-phase
Sub-phase: 2.4 Views B and C
Status: active
Base: 86c70a4753df7f63a24926c400476ff430eed382
Brief: docs/phases/phase-2/phase-2.4-brief.md
Branch: duncandoyle/viewsBC

## Carry-over (deferred to later phases)
- (P4) Downstream detectors cannot report their base kgateway version, so experimental classification is conservative for them — seam milestone — src: phases/phase-1/phase-1.5-complete.md
- (P3) Scope verification for experimental Gateway API kinds: switch KGWUI-007 to the controller's enabled kinds once the upstream scope endpoint exists (D-017 §4) — src: reviews/phase-codex.md#5
- (P3) REMIND USER at the start of each phase: file the upstream kgateway issue "Expose the controller's effective discovery scope through a read-only endpoint" (H9, D-009 §7). User chose to file it in a later phase (2026-10-20). Draft kept outside the repo (public repo). Before filing: confirm on a cluster that status stays after a namespace is deselected, and re-verify the file:line refs against kgateway main.
- (P3) Watch CRDs added or removed after startup without a restart — https://github.com/DuncanDoyle/kgateway-ui/issues/1
- (P4) Extension seam: finding-code prefix registry (Phase 1 uses a documented convention) — for the seam milestone after Phase 2 — src: phases/phase-1/phase-1.1-complete.md
- (P4) Extension seam: analyzers, API-route registration, per-kind renderers, health cards — for the seam milestone after Phase 2 — src: phases/phase-1/reviews/design-codex.md#16
- (P3) Per-user AuthN/AuthZ behind `api.Authorizer` (D-013) — roadmap — src: phases/phase-1/phase-1.4-complete.md
- (P4) Refactors of design §14.4, triggered by the standalone PoC; neutral `Install` and `Detector` before the seam milestone
- (P4) Optional `kind-main` e2e job: run on manual dispatch with `kgw_main_version`; chart source unverified — src: phases/phase-1/phase-1.4-complete.md

## Notes
- 2026-10-26: sub-phase 2.3 merged in #9 (86c70a4). Sub-phase 2.4 started with D-024 (unknown scope and the inherited match) and the pre-flight rulings PF1–PF26 folded into plan Tasks 52–70.
- 2026-10-25: sub-phase 2.3 closed. Gates green on 33cc71e; Codex review triaged and fixed (reviews/2.3-codex.md). User decisions D-022 (memory gates) and D-023 (restricted memberships, written-ref merge). No waivers.
- 2026-10-23: sub-phase 2.2 closed. Gates green on 0855652; Codex review triaged and fixed (reviews/2.2-codex.md). No waivers.
- 2026-10-23: user approved the plan and a chart `extraArgs` value (Task 50 Step 0).
- 2026-10-21: Phase 1 closed. All exit gates held (observed on 6cd6111; CI green on PR #6). No waivers.
- The first release tag freezes API v1, install IDs and SPA URLs; make the ghcr packages public first (phase-1-complete.md).
- The repo is public: per the public-repo hygiene rules, vendor and commercial edition names must not appear in code, file names, commit messages or PR text. Refer to them as "downstream distributions".
