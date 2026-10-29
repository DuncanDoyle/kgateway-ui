# Phase status

## Phase
Phase: 3 — Health & Diagnostics
Base: 544dcd5db3196cffd077267bca364ba8a8cd6d24
Docs: docs/phases/phase-3/
Status: designing; Phase 2 closed 2026-10-29 (phases/phase-2/phase-2-complete.md)

### Queue
- [ ] Q1 Phase 3 design (`design.md`), incl. the KRT incremental-recompute question in the roadmap and stable finding identity (phase-2 review finding 8) ← CURRENT — draft written 2026-10-29 without a question round (user's request); open questions in design §25
- [ ] Q2 Phase 3 implementation plan (`implementation-plan.md`)
- [ ] Q3 Cross-model review of design + plan, triage, user approval

### Exit gates
- [ ] tests/vet pass (observed)
- [ ] all queue items done or deferred
- [ ] design+plan cross-model review + triage: docs/phases/phase-3/reviews/
- [ ] phase cross-model review + triage: docs/phases/phase-3/reviews/phase-codex.md
- [ ] decisions.md / design / plan in sync with code
- [ ] phase-3-complete.md written, roadmap updated, tracked items closed

## Sub-phase
none

## Carry-over (deferred to later phases)
- (P3) REMIND USER at the start of each phase: file the upstream kgateway issue "Expose the controller's effective discovery scope through a read-only endpoint" (H9, D-009 §7). User chose to file it in a later phase (2026-10-20). Draft kept outside the repo (public repo). Before filing: confirm on a cluster that status stays after a namespace is deselected, and re-verify the file:line refs against kgateway main.
- (P3) Delegation memory: bound generated path bytes in inherit merges and charge failure-reason text and JSON escaping in the page budget — src: phases/phase-2/reviews/2.4-codex.md (final round) — planned for 3.1 (design §15)
- (P3) Stream CPU gate measures the views-memo hit path (annotation-only edits); add a reported spec-edit stream variant — src: phases/phase-2/phase-2.4-complete.md — planned for 3.1 (design §16.3)
- (P3) Scope verification for experimental Gateway API kinds: switch KGWUI-007 to the controller's enabled kinds once the upstream scope endpoint exists (D-017 §4) — src: phases/phase-1/reviews/phase-codex.md#5
- (P3) Watch CRDs added or removed after startup without a restart — https://github.com/DuncanDoyle/kgateway-ui/issues/1
- (P3) Per-user AuthN/AuthZ behind `api.Authorizer` (D-013) — roadmap — src: phases/phase-1/phase-1.4-complete.md
- (P4) Stable finding identity for Phase 3 diagnostics — src: phases/phase-2/reviews/phase-codex.md#8 — decided in the Phase 3 design draft §5.1 (sub-phase 3.1), pending approval
- (P4) Apps that embed `Shell` must wait for fonts before rendering, as `web/app/src/main.tsx` does, or long names shift the layout on load — src: phases/phase-2/phase-2.4-complete.md — planned for 3.2 (design §15)
- (P4) Downstream detectors cannot report their base kgateway version, so experimental classification is conservative for them — seam milestone — src: phases/phase-1/phase-1.5-complete.md
- (P4) Extension seam: finding-code prefix registry (Phase 1 uses a documented convention) — for the seam milestone after Phase 2 — src: phases/phase-1/phase-1.1-complete.md
- (P4) Extension seam: analyzers, API-route registration, per-kind renderers, health cards — for the seam milestone after Phase 2 — src: phases/phase-1/reviews/design-codex.md#16
- (P4) Refactors of design §14.4, triggered by the standalone PoC; neutral `Install` and `Detector` before the seam milestone
- (P4) Optional `kind-main` e2e job: run on manual dispatch with `kgw_main_version`; chart source unverified — src: phases/phase-1/phase-1.4-complete.md
- Smaller Phase 2 items (parked Tasks 57 and 58, test gaps, P4 polish) — src: phases/phase-2/phase-2-complete.md, Deferred / open

## Notes
- 2026-10-29: Phase 3 started at base 544dcd5. Design draft `phases/phase-3/design.md` written from the roadmap and the Phase 3 research (kept outside the repo, because the repo is public). The KRT question is framed as D-027, decided after a spike in 3.1. A second upstream draft (xDS reject status) sits next to the scope-endpoint draft outside the repo; filing both stays the user's call.
- 2026-10-29: Phase 2 closed. All exit gates held (CI on PR #11 green; merge 8b988e1 has the same tree). No waivers. Record: phases/phase-2/phase-2-complete.md.
- The first release tag freezes API v1, install IDs and SPA URLs; make the ghcr packages public first (phase-1-complete.md).
- The repo is public: per the public-repo hygiene rules, vendor and commercial edition names must not appear in code, file names, commit messages or PR text. Refer to them as "downstream distributions".
