# Phase 6.5 — Gateway map, core (Complete)

**Completed:** 2026-11-11 (local branch `duncandoyle/phase6`, not yet pushed)
**Range:** 120db13..b246dcc

## Summary
Gateway detail has a Map tab (design §6): React Flow (`@xyflow/react` 12.12.0, lazy chunk 63.8 kB gzipped) with our own fixed-column layout (Gateway → listeners → routes → backends). Hovering a node lights every path through it; clicking opens a side panel in its own column with the node's state, "N of M paths through this node fail", problems, endpoints and links. Find, Problems-only/kind/namespace chips (in the URL, from server facets), large-Gateway handling, truncation and inconsistency notices, keyboard navigation along edges, and a table view with the same rows. Edge states never rely on colour alone (dash, dots, rings, icons).

## Sub-phases (phase record only)
n/a

## Reviews
Per-task Claude reviews and scoped re-reviews (same model family; the cross-model review is at phase close). Notable findings fixed:
- Task 44: paging could miss a path that turned bad between pages (now a consistency check with bounded restarts); edges gained a worst-row state; no cross-rule shares.
- Task 45: polls no longer reset the view; the side panel reflows the map; lit edges show the state of the shared paths; a partial-error mark distinct from broken.
- Task 46: the table keeps listeners; an unapproved test dependency was removed (queued for the user).
- A flaky web test was fixed at its root (`7174974`).

## Waivers
None.

## Deliverables
- Commits `ce06ef1` … `b246dcc` (Tasks 43–47) and `7174974`.
- Bench (`bench-6.5.txt`): D-057 settled +46.7 MiB at large-contradicting-worst (budget 48); request gates pass on rerun (one list-request p95 tail spike, see below); map chunk 63,752 B gzipped (budget 80 kB).
- e2e: Go e2e and Playwright (78 passed) on the three pairs, local binary.
- Large-Gateway check (80 routes): opens on Problems only; 16.7 ms frames while panning; culling renders 27 of 103 nodes zoomed in.
- Wording pass: `phase-6.5-wording.md` (separate Claude subagent, not cross-model).
- Demo reel (not in the repo): scratchpad `reels/6.5/summary-reel.mp4`.

## Notes for later phases
- Node state comes from signals naming the node; the panel shows failing paths through it.
- Listener waiting signals reach the Gateway node (server wire shape); an additive server field later.

## Deferred / open
- `TestBudgetListRequests` (allow-all detail p95) failed once at 65 ms (gate 50), 12 ms on rerun; no server change in 6.5. Re-checked at phase close.
- Under Problems only, a trunk edge can draw as broken because only failing rows are loaded — fixed in 6.6.
