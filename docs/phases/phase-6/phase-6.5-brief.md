# Sub-phase 6.5 — Gateway map, core

Kind: planned
Base: 120db13
Issues: n/a | Plan steps: implementation-plan.md, Sub-phase 6.5, Tasks 43–47

## Problem / goal
The user wants an overview of what one Gateway serves that is "as interactive as possible" (2026-11-10); it is also the demo view (design §1, §6). 6.5 adds a "Map" tab on Gateway detail: four fixed columns (Gateway → listeners → routes → backends) drawn with React Flow (`@xyflow/react`, approved by the user, loaded lazily) and our own column layout, with pan/zoom/minimap, hover highlighting of every path through a node, a side panel on click, search and filter chips, keyboard navigation and a table toggle. Expand/collapse, focus mode, animated flow and export are 6.6.

## Expected behaviour

| Input / action | Expected |
|---|---|
| Open Gateway → Map | the map loads as its own lazy chunk; other pages don't load `@xyflow` |
| Gateway with its paths | one node per distinct listener, route and backend; delegated routes under their root, marked |
| Edge thickness | follows the share where shown (§4.4); broken edges drawn as broken |
| Pan, zoom, fit, minimap | work; theme tokens in light and dark |
| Hover a node | every node and edge on paths through it highlighted, the rest dimmed |
| Click a node | side panel: state, problems, endpoints, links to detail and Traffic |
| Search | focuses a node by name |
| Chips (problems only, kind, namespace) | update the server query live; in the URL |
| Gateway with > 60 routes or backends | opens on Problems only, with a prompt to narrow (from the server summary, not the loaded rows) |
| More than 2,000 matching paths | loads 2,000 in pages of 200 and says "showing 2,000 of N paths, narrow the filters" |
| Keyboard | moves between connected nodes; table toggle shows the same rows as the path list |
| Stale data | every state unknown |
| Layout | deterministic; barycentre ordering reduces crossings |

## Root cause (if known)
n/a (new feature).

## Acceptance tests
The test names in plan Tasks 43–46 (Vitest, chunk test, Playwright map.spec).

## Scope
In: `web/packages/shell` (map pages, Gateway detail tab), `web/app` build config if a manual chunk is needed, the dependency `@xyflow/react`.
Out: server changes (the map reads `/paths`), 6.6 interactions. No new retained server state (D-057 headroom 1.5 MiB).

## Plan / design impact
none

## Definition of done
- [ ] acceptance tests fail before, pass after
- [ ] web tests, lint, typecheck; `make test`; bench unchanged
- [ ] wording pass (phase-6.5-wording.md)
- [ ] demo reel of the map
- [ ] phase-6.5-complete.md written; phase-status.md updated
