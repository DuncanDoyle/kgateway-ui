# Sub-phase 6.7 — Phase review fixes

Kind: stabilization
Base: c227d8d
Issues: n/a | Plan steps: reviews/phase-codex.md findings 1–9

## Problem / goal
The cross-model phase review found states shown healthier than their signals (D-006), a visibility leak (D-077), map paging and dedupe that can lose paths, and a missing Phase 11 slot. Fix each, behaviour first; no new features.

## Expected behaviour

| # | Input / action | Expected |
|---|---|---|
| 1 | Restricted viewer; a visible Backend used only by hidden objects | listed under `used=no` ("no visible consumers"); facets count it the same way; nothing reveals that hidden objects use it |
| 2 | Route parent with `ResolvedRefs=False` and `kgateway.dev/Programmed=False` | every row of that attachment carries the programming failure; ref attribution doesn't clear it; root-route KGWUI-015 counts |
| 3 | Delegated child with missing, stale or rejected controller status | its rows are waiting/unknown or error, never healthy, even with ready endpoints |
| 4 | In-scope Backend, unused, current `Accepted=False` | error in the Backends list and detail |
| 5 | Cached endpoints ready, complete scoped read shows none ready | detail state at least unknown, with a short explanation of the disagreement; no new finding |
| 6 | Healthy Gateway; one listener's `Programmed` missing or stale | that listener node is waiting/unknown on the map; the Gateway node isn't blamed for it |
| 7 | Two delegation refs in one rule; two inherited contexts through one route chain | the map and table keep every distinct `/paths` row |
| 8 | One route recovers and another fails between map pages, counts unchanged | the map restarts the walk; the failing row is present |
| 9 | TCPRoute / TLSRoute rule in the route trace | the reserved Filters area is present, as on HTTP rules |

## Root cause (if known)
See reviews/phase-codex.md (file:line per finding).

## Acceptance tests
One test per row at the highest practical boundary (pkg/paths or the API for 1–5 and the server half of 6–8; Vitest for the map and trace parts of 6–9), each shown failing before the fix. A finding that cannot be reproduced is re-triaged as rejected with the evidence.

## Scope
In: pkg/paths, pkg/api, web/packages/shell/src/pages/{map,explore/route}, their tests; additive API fields (listener identity on signals, a stable row id on PathRowDTO, a projection revision on the paths response).
Out: new features, new dependencies, gate changes.

## Plan / design impact
Design §4.5, §5 and §6 gain one line each for the additive fields; D-078 item for `used=no` is reversed.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite, lint, web tests and bench pass
- [ ] cross-model review of `git diff c227d8d..HEAD` + triage
- [ ] phase-6.7-complete.md written; phase-status.md updated
