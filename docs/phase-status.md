# Phase status

## Phase
Phase: 2 — Gateway & Route Explorer
Base: 6b5dcc2b6e69854496a47ed904f2b13d715eeff2
Docs: docs/phases/phase-2/
Status: design in progress; Phase 1 complete (phases/phase-1/phase-1-complete.md, merged through #6, 6cd6111)

### Queue
- [ ] Q1 Phase 2 design (`design.md`) ← CURRENT
- [ ] Q2 Phase 2 implementation plan (`implementation-plan.md`)
- [ ] Q3 Cross-model review of design + plan, triage, user approval

### Exit gates
- [ ] tests/vet pass (observed)
- [ ] all queue items done or deferred
- [ ] design+plan cross-model review + triage: docs/phases/phase-2/reviews/design-codex.md
- [ ] phase cross-model review + triage: docs/phases/phase-2/reviews/phase-codex.md
- [ ] decisions.md / design / plan in sync with code
- [ ] phase-2-complete.md written, roadmap updated, tracked items closed

## Sub-phase
none

## Carry-over (deferred to later phases)
- (P4) Downstream detectors cannot report their base kgateway version, so experimental classification is conservative for them — seam milestone — src: phases/phase-1/phase-1.5-complete.md
- (P4) Possible flake: pkg/app TestFirstMetricsCollectionFollowsDiscovery (5 s timeout under parallel load) — src: phases/phase-1/phase-1.5-complete.md
- (P3) Scope verification for experimental Gateway API kinds: switch KGWUI-007 to the controller's enabled kinds once the upstream scope endpoint exists (D-017 §4) — src: reviews/phase-codex.md#5
- (P3) REMIND USER at the start of each phase: file the upstream kgateway issue "Expose the controller's effective discovery scope through a read-only endpoint" (H9, D-009 §7). User chose to file it in a later phase (2026-10-20). Draft kept outside the repo (public repo). Before filing: confirm on a cluster that status stays after a namespace is deselected, and re-verify the file:line refs against kgateway main.
- (P3) Watch CRDs added or removed after startup without a restart — https://github.com/DuncanDoyle/kgateway-ui/issues/1
- (P4) Extension seam: finding-code prefix registry (Phase 1 uses a documented convention) — for the seam milestone after Phase 2 — src: phases/phase-1/phase-1.1-complete.md
- (P4) Extension seam: analyzers, API-route registration, per-kind renderers, health cards — for the seam milestone after Phase 2 — src: phases/phase-1/reviews/design-codex.md#16
- (P3) Per-user AuthN/AuthZ behind `api.Authorizer` (D-013) — roadmap — src: phases/phase-1/phase-1.4-complete.md
- (P3) Register `Backend` with its `Accepted` condition and an assignment rule — Phase 2 design (D-016, design §14.5)
- (P4) Refactors of design §14.4, triggered by the standalone PoC; neutral `Install` and `Detector` before the seam milestone
- (P4) Optional `kind-main` e2e job: run on manual dispatch with `kgw_main_version`; chart source unverified — src: phases/phase-1/phase-1.4-complete.md

## Notes
- 2026-10-21: Phase 1 closed. All exit gates held (observed on 6cd6111; CI green on PR #6). No waivers.
- The first release tag freezes API v1, install IDs and SPA URLs; make the ghcr packages public first (phase-1-complete.md).
- The repo is public: per the public-repo hygiene rules, vendor and commercial edition names must not appear in code, file names, commit messages or PR text. Refer to them as "downstream distributions".
