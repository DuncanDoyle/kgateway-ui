# Phase status

## Phase
Phase: 1 — Foundation & Overview
Base: 560c39392a8aa326d0645f24593b1b16577459b5
Docs: docs/phases/phase-1/
Status: implementing; sub-phase 1.2 (frontend) complete, merged in #3 (ce37ea3); 1.3 (platform-neutral contract, D-016) complete, merged in #4 (cc8ba71); 1.4 next

### Queue
- [x] Q1 Phase 1 design (`design.md`) — approved by user 2026-10-17
- [x] Q2 Phase 1 implementation plan (`implementation-plan.md`) — drafted 2026-10-17, revised after cross-model review 2026-10-18; planned sub-phases 1.1 backend, 1.2 frontend, 1.3 packaging/CI/e2e — approved by user 2026-10-19
- [x] Q3 Cross-model review of design + plan, triage, user approval — five Codex rounds (reviews/design-*codex.md), all findings triaged; approved 2026-10-19
- [x] Q4 Sub-phase 1.1 — backend (plan Tasks 1–11) — phases/phase-1/phase-1.1-complete.md
- [x] Q5 Sub-phase 1.2 — frontend (plan Tasks 12–18) — phases/phase-1/phase-1.2-complete.md
- [x] Q6a Platform neutrality design change (D-016, design §14) — Codex review + triage: reviews/neutrality-codex.md; approved by user 2026-10-20
- [x] Q6 Sub-phase 1.3 — platform-neutral contract (design §14.3: H1–H5, H8, H11) — phases/phase-1/phase-1.3-complete.md
- [ ] Q7 Sub-phase 1.4 — packaging, CI, e2e (plan Tasks 19–23, plus H9, H10) ← CURRENT
- [ ] Q8 Phase cross-model review + triage

### Exit gates
- [ ] tests/vet pass (observed)
- [ ] all queue items done or deferred
- [x] design+plan cross-model review + triage: docs/phases/phase-1/reviews/design-codex.md … design-r5-codex.md
- [ ] phase cross-model review + triage: docs/phases/phase-1/reviews/phase-codex.md
- [ ] decisions.md / design / plan in sync with code
- [ ] phase-1-complete.md written, roadmap updated, tracked items closed

## Sub-phase
none

## Carry-over (deferred to later phases)
- (P3) Watch CRDs added or removed after startup without a restart — https://github.com/DuncanDoyle/kgateway-ui/issues/1
- (P4) Extension seam: finding-code prefix registry (Phase 1 uses a documented convention) — for the seam milestone after Phase 2 — src: phases/phase-1/phase-1.1-complete.md
- (P4) Extension seam: analyzers, API-route registration, per-kind renderers, health cards — for the seam milestone after Phase 2 — src: phases/phase-1/reviews/design-codex.md#16

## Notes
- 2026-10-20: user approved D-016 (platform neutrality). Kubernetes stays the product focus; standalone is a separate project, and Phase 1 only adds the hedges in design §14.3.
- 2026-10-20: user confirmed D-011's dark default (no OS-preference step).
- 2026-10-18: user approved Overridden policies as info (not warning) and subagent-driven execution for Phase 1.
- The repo is public: per the public-repo hygiene rules, vendor and commercial edition names must not appear in code, file names, commit messages or PR text. Refer to them as "downstream distributions" (D-005).
- Research inputs for the Phase 1 design:
  - The controller admin server (port 9095, localhost-bound by default; the bind address is configurable from kgateway 2.4) serves `/version` and `/snapshots/xds`.
  - The controller serves metrics on 9092 and the proxies on 9091. The Envoy admin port 19000 is localhost-only.
  - Edition detection: GatewayClass `spec.controllerName` and the installed CRD groups.
  - The helm labels on the controller and proxy Deployments carry `app.kubernetes.io/version`.
