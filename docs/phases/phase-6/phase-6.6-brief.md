# Sub-phase 6.6 — Gateway map, the rest; phase close

Kind: planned
Base: b246dcc
Issues: n/a | Plan steps: implementation-plan.md, Sub-phase 6.6, Tasks 48–50

## Problem / goal
The map's remaining interactions (design §6.1 items 4, 5, 7, 9): expand a route into its rules and a delegating route into its children; focus mode that reduces the map to one node's upstream and downstream paths, kept in the URL; animated flow on healthy edges; export of the current view as SVG or PNG. Then the Phase 6 close: final bench, e2e, a reel of the whole phase, README and docs.

## Expected behaviour

| Input / action | Expected |
|---|---|
| Expand a two-rule route | two rule nodes appear; other columns keep their positions |
| Expand a delegating route | its children appear under it (nested chains under their own parent) |
| Focus on a backend | only the Gateways, listeners and routes on its rows remain |
| Reload | `expand` and `focus` restored from the URL |
| Healthy edge | animated flow; never animated unless its state is healthy |
| Broken edge | dashed with a break mark |
| `prefers-reduced-motion` | no animation |
| Problems-only filter | an edge is not drawn "broken" only because the loaded (filtered) rows all fail |
| Export SVG / PNG | one element per visible node and edge, theme colours; non-empty files; no new dependency |
| Stale data | every state unknown |

## Root cause (if known)
n/a (new feature).

## Acceptance tests
The test names in plan Tasks 48–49; the phase close in Task 50.

## Scope
In: `web/packages/shell/src/pages/map/*`, README, docs.
Out: server changes; new dependencies (none approved beyond `@xyflow/react`).

## Plan / design impact
none

## Definition of done
- [ ] acceptance tests fail before, pass after
- [ ] web tests, lint, typecheck, build (map chunk ≤ 80 kB); `make test`; final bench
- [ ] wording pass (phase-6.6-wording.md)
- [ ] reel of the whole phase
- [ ] phase-6.6-complete.md; Phase 6 cross-model review; phase-6-complete.md
