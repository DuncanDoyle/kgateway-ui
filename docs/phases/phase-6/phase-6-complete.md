# Phase 6 — Traffic Flow & Backends (Complete)

**Completed:** 2026-11-12 (local branch `duncandoyle/phase6`, not yet pushed)
**Range:** 657378ad..ec258bb

## Summary
Operators see where traffic goes and what receives it. One path model (Gateway, listener, route, rule, backend ref) feeds three views: a Traffic list with problems first, a Backends list with a detail page ("how this backend is exposed", endpoint readiness), and a Gateway map (hover lights paths, click opens a side panel, expand, focus, export). Route detail traces every route kind. States are never healthier than their signals, and restricted viewers learn nothing about hidden use (D-077). Library users get `cache.ListOnce` and namespace-scoped inputs for kgwctl. API v1 changes are additive; golden-v1 is byte-equal.

## Sub-phases (phase record only)
| Sub-phase | Kind | Closed | Description | Record |
|---|---|---|---|---|
| 6.1 | planned | 2026-11-10 | Cache access for kgwctl: `ListOnce`, namespace scoping | `phase-6.1-complete.md` |
| 6.2 | planned | 2026-11-11 | Path model, paths API, path list | `phase-6.2-complete.md` |
| 6.3 | planned | 2026-11-11 | Backends list and detail, scoped endpoint reader | `phase-6.3-complete.md` |
| 6.4 | planned | 2026-11-11 | Route trace for all route kinds | `phase-6.4-complete.md` |
| 6.5 | planned | 2026-11-11 | Gateway map, core | `phase-6.5-complete.md` |
| 6.6 | planned | 2026-11-12 | Gateway map: expand, focus, flow, export; phase close | `phase-6.6-complete.md` |
| 6.7 | stabilization | 2026-11-12 | Phase review fixes (`reviews/phase-codex.md` #1–#9, three re-review rounds) | `phase-6.7-complete.md` |

## Reviews
- `reviews/design-codex.md` — 24 findings (15 reviewer, 9 builder check): 24 accepted.
- `reviews/plan-codex.md` — 14 findings (13 reviewer, 1 author): 14 accepted.
- `reviews/phase-codex.md` — 9 findings (8 P1, 1 P2): 9 accepted, fixed in 6.7.
- `reviews/6.7-codex.md` — 5 fixed, 4 partial, 7 new: all accepted and fixed.
- `reviews/6.7-codex-r2.md` — 2 new accepted and fixed, 1 tracked as a baseline.
- `reviews/6.7-codex-r3.md` — both fixed, none new.
- Claude task reviews ran per task. Wording picks ran in separate subagents (not cross-model).

## Waivers
None.

## Deliverables
- `pkg/paths` (path model, projection, backends), the paths and backends APIs, `endpoints.ScopedReader`, the kgateway pack's Backend judgement and consumers.
- Web: Traffic, Backends, route trace and the Map tab (`@xyflow/react` 12.12.0, lazy chunk 66.3 kB gzipped of 80 kB).
- Decisions D-070 … D-078; design synced to the build (status: approved, as built).
- Bench: D-057 settled +46.9 of 48 MiB; cold `/paths` 59.8 of 100 ms.
- e2e after 6.7 on `ec258bb`, three pairs: Go 10 pass, forced-disagreement test passes, Playwright 85 passed (16 NACK skips), `map.spec.ts` 19 of 19.
- Reel of the whole phase, 7.5 MB, kept outside the repo. README "Traffic and backends"; `documentation-notes.md` topics.
- Dependencies added: `@xyflow/react` only (approved).

## Notes for later phases
- Phase 7 reuses the path model for policy attachment. Phase 11 fills the reserved rule Filters area and the proxy endpoint view (D-069).
- `used=no` means "no visible consumers" (D-078 item 16).
- The map chunk has 13.7 kB of headroom; D-057 has 1.1 MiB.
- Release: tag and push, program-file update and kgwctl comments are the user's.

## Deferred / open
- Pre-existing, before Phase 6: the Findings API lists KGWUI-002 on a Backend that only hidden routes use, which reveals hidden use (D-077). Queued for the user to schedule.
- Baselines to watch: cold `/paths` 59.8 of 100 ms; D-057 46.9 of 48 MiB. Storing row hashes in the Topology would cut the cold cost for +8 B/row of heap.
- UX: selecting a backend at fit view re-centres it and the side panel clips route names.
- UX: a node reads "Healthy" beside "1 of 1 paths through this node fail".
- Queued for the user:
  - KGWUI-023's title "Requests to this backend get 503" names a status code. A finding-copy decision.
  - A ref whose problem is a KGWUI-023 warning draws a plain blue share segment. Tint it, or leave share states to 013/014?
  - Class-only installs get no paths in Phase 6 (`summary.notComputed`). Derive them from route status?
  - Add `@axe-core/playwright` (e2e-only devDependency) for automated accessibility checks?
  - The map reserves a rule column, so the resting map is wider.
  - Export covers the whole drawn graph, not the viewport.
  - Review the D-078 deviations (status: pending user review).
- snellius.dev sync for the user-visible changes (program P-006).
