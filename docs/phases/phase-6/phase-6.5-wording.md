# Sub-phase 6.5 — Wording

5 strings so far, five options each, all pending; new SPA copy of the Gateway map (`web/packages/shell/src/copy/map.ts`, `mapCopy`). Tasks 44–46 add their strings here.

Option 1 is the drafted text. Ids run from S766, after the highest id in `phase-6.4-wording.md` (S765). The map also reuses earlier strings unchanged: S664 and S665 (no computed views), S26 "Cannot load {what}", the listener noun "Listener", S621–S623 (action names on action nodes) and S625 "Delegated" (delegated route nodes).

| # | id | File:line | Where | Placeholders | Option 1 (draft) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | S766 | `copy/map.ts:6` | Gateway detail tab label | none | Map | Path map | Traffic map | Diagram | Graph | pending |
| 2 | S767 | `copy/map.ts:7` | Map canvas label (screen readers) | none | Map of this Gateway's paths | Paths through this Gateway, as a map | Gateway path map | Map of listeners, routes and backends | This Gateway's paths | pending |
| 3 | S768 | `copy/map.ts:8` | Map placeholder label while the map loads (screen readers) | none | Loading the map | Map loading | Drawing the map | Loading paths for the map | Preparing the map | pending |
| 4 | S769 | `copy/map.ts:9` | Note above the map when the result has more than 2,000 paths | shown (2,000), total | Showing {shown} of {total} paths. Narrow the filters to see the rest. | Showing {shown} of {total} paths. Narrow the filters. | The map shows the first {shown} of {total} paths. Use the filters to see the rest. | {shown} of {total} paths shown. Add a filter to see the rest. | Too many paths to draw: showing {shown} of {total}. Narrow the filters. | pending |
| 5 | S770 | `copy/map.ts:11` | Kind line of a map node for a rule with no backend (redirect, direct response, no backend) | none | Action | Rule action | Ends in | Result | No backend ref | pending |
