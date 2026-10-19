# Sub-phase 1.2 — Frontend

Kind: planned
Base: 1f21b21b3e7f08b51e445fb9effb4326d6afaa2c
Issues: n/a | Plan steps: implementation-plan.md Tasks 12–18

## Problem / goal
Build the Phase 1 web UI on the merged backend API: an install page that answers "is it healthy, and if not, why" at a glance (layout A), a cluster landing for several installs (layout C), a findings list, a status-first YAML view, light and dark "Control room" themes, and the frontend extension seam a downstream build imports.

## Expected behaviour
As specified in design.md §2 and §6 and the per-task tests of implementation-plan.md Tasks 12–18. Key observable cases:

| Input / action | Expected |
|---|---|
| Healthy install | Green strip, "what was checked" list, count tiles, Gateway table |
| Install with findings | Strip shows the worst state and counts; top findings link to the resource view |
| Stale data (computedAt too old) or wrong install id | Strip and Gateway rows turn unknown; never the previous install's data |
| Restricted finding | Severity, code and resource shown; no link; "restricted" text |
| More than one install, not pinned | Cluster landing with one card per install |
| Theme switch | Light and dark both pass the contrast tests; no colour flash before React loads |
| Downstream app registers a page, nav entry and theme | They render in the shell and the tokens apply |

## Root cause (if known)
n/a (new work)

## Acceptance tests
Vitest + Testing Library per task against a mocked API; the frontend seam test (Task 18); Playwright evidence for the visual tasks against a local kind cluster with the 1.1 backend.

## Scope
In: web/ (packages/shell, app), internal/webui embedding (Task 18), Makefile web targets.
Out: Dockerfile, Helm chart, CI, e2e (sub-phase 1.3); backend changes except what Task 18 needs to embed the SPA.

## Plan / design impact
The backend API drifted from the plan through 1.1 rulings (restricted findings, computedAt, health install id, scope.source, CheckResult.detail, served-kinds finding); the frontend follows the API as built.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite passes (Vitest, Biome, typecheck, build; go test)
- [ ] cross-model review of `git diff <base>..HEAD` + triage (planned: at the phase review)
- [ ] phase-1.2-complete.md written; phase-status.md updated
