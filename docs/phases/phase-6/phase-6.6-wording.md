# Sub-phase 6.6 — Wording

12 strings so far, five options each; new SPA copy of the Gateway map (`web/packages/shell/src/copy/map.ts`, `mapCopy`). Tasks 48–49 add their strings here.

Option 1 is the drafted text. Choices are pending the wording pass. Ids run from S800, after the highest id in `phase-6.5-wording.md` (S799). Task 48 also reuses S625 "Delegated" (delegated route cards), S778 "Node details" and the 6.5 side panel strings unchanged.

| # | id | File:line | Where | Placeholders | Option 1 (draft) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | S800 | `copy/map.ts:43` | Kind line of a rule node, shown when a route is expanded | n (1-based rule number) | Rule {n} | Rule #{n} | Rule {n} of the route | Match rule {n} | Route rule {n} | pending |
| 2 | S801 | `copy/map.ts:44` | Side panel button that opens a route into one node per rule | none | Show rules | Expand rules | Show its rules | Open rules | Rules | pending |
| 3 | S802 | `copy/map.ts:45` | Side panel button that closes a route's rule nodes | none | Hide rules | Collapse rules | Hide its rules | Close rules | Fewer details | pending |
| 4 | S803 | `copy/map.ts:46` | Route card toggle label (screen readers), closed: shows its delegated routes, or its rules for a route that delegates none | name | Expand {name} | Open {name} | Show more of {name} | Expand route {name} | Show what {name} contains | pending |
| 5 | S804 | `copy/map.ts:47` | Route card toggle label (screen readers), open: hides its delegated routes, or its rules | name | Collapse {name} | Close {name} | Show less of {name} | Collapse route {name} | Hide what {name} contains | pending |
| 6 | S805 | `copy/map.ts:48` | Side panel button that reduces the map to the node's paths | none | Focus on this | Show only its paths | Focus | Isolate | Only this node's paths | pending |
| 7 | S806 | `copy/map.ts:49` | Button that leaves focus mode (notes line and side panel) | none | Clear focus | Show the whole map | Leave focus | Show all paths | Exit focus | pending |
| 8 | S807 | `copy/map.ts:50` | Note above the map in focus mode | kind, name | Showing only the paths through {kind} {name}. | Focused on {kind} {name}. | Only paths through {kind} {name} are shown. | The map shows the paths through {kind} {name}. | Showing paths through {kind} {name} only. | pending |
| 9 | S808 | `copy/map.ts:51` | Side panel button that hides a route's delegated routes | none | Hide delegated routes | Collapse delegated routes | Hide child routes | Collapse | Hide delegation | pending |
| 10 | S809 | `copy/map.ts:52` | Side panel button that shows a collapsed route's delegated routes | none | Show delegated routes | Expand delegated routes | Show child routes | Expand | Show delegation | pending |
| 11 | S810 | `copy/map.ts:53` | Kind line of a collapsed route card and its side panel: how many delegated routes it hides | n | +{n} delegated | {n} delegated hidden | +{n} hidden | {n} more delegated | Hides {n} delegated | pending |
| 12 | S811 | `copy/map.ts:54` | Note above the map when the focused node is not on the map | none | The focused node is not on the map. | The focused node is gone from the map. | Nothing to focus on: that node is not on the map. | The node in focus is not on this map. | Focus node not found. | pending |
