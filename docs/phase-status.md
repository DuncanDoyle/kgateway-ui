# Phase status

## Phase
Phase: 1 — Foundation & Overview
Base: 560c39392a8aa326d0645f24593b1b16577459b5
Docs: docs/phases/phase-1/
Status: design in progress (started 2026-10-16)

### Queue
- [ ] Q1 Phase 1 design (`design.md`) — drafted 2026-10-17, awaiting user review ← CURRENT
- [ ] Q2 Phase 1 implementation plan (`implementation-plan.md`)
- [ ] Q3 Cross-model review of design + plan, triage, user approval

### Exit gates
- [ ] tests/vet pass (observed)
- [ ] all queue items done or deferred
- [ ] design+plan cross-model review + triage: docs/phases/phase-1/reviews/design-codex.md
- [ ] phase cross-model review + triage: docs/phases/phase-1/reviews/phase-codex.md
- [ ] decisions.md / design / plan in sync with code
- [ ] phase-1-complete.md written, roadmap updated, tracked items closed

## Sub-phase
none

## Carry-over (deferred to later phases)
n/a

## Notes
- The repo is public: per the public-repo hygiene rules, vendor and commercial edition names must not appear in code, file names, commit messages or PR text. Refer to them as "downstream distributions" (D-005).
- Research inputs for the Phase 1 design:
  - The controller admin server (port 9095, localhost-bound by default; the bind address is configurable from kgateway 2.4) serves `/version` and `/snapshots/xds`.
  - The controller serves metrics on 9092 and the proxies on 9091. The Envoy admin port 19000 is localhost-only.
  - Edition detection: GatewayClass `spec.controllerName` and the installed CRD groups.
  - The helm labels on the controller and proxy Deployments carry `app.kubernetes.io/version`.
