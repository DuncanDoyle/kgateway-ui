# Sub-phase 6.6 — Wording

8 strings so far, five options each; new SPA copy of the Gateway map (`web/packages/shell/src/copy/map.ts`, `mapCopy`). Tasks 48–49 add their strings here.

Option 1 is the drafted text. Choices are pending the wording pass. Ids run from S800, after the highest id in `phase-6.5-wording.md` (S799). Task 48 also reuses S625 "Delegated" (delegated route cards), S778 "Node details" and the 6.5 side panel strings unchanged.

| # | id | File:line | Where | Placeholders | Option 1 (draft) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | S800 | `copy/map.ts:43` | Kind line of a rule node, shown when a route is expanded | n (1-based rule number) | Rule {n} | Rule #{n} | Rule {n} of the route | Match rule {n} | Route rule {n} | pending |
| 2 | S801 | `copy/map.ts:44` | Side panel button that opens a route into its rules and delegated routes | none | Expand | Show rules | Open up | Show inside | Expand route | pending |
| 3 | S802 | `copy/map.ts:45` | Side panel button that closes an expanded route | none | Collapse | Hide rules | Close up | Hide inside | Collapse route | pending |
| 4 | S803 | `copy/map.ts:46` | Route card toggle label (screen readers), collapsed | name | Expand {name} | Show the rules of {name} | Open {name} | Expand route {name} | Show what {name} contains | pending |
| 5 | S804 | `copy/map.ts:47` | Route card toggle label (screen readers), expanded | name | Collapse {name} | Hide the rules of {name} | Close {name} | Collapse route {name} | Hide what {name} contains | pending |
| 6 | S805 | `copy/map.ts:48` | Side panel button that reduces the map to the node's paths | none | Focus on this | Show only its paths | Focus | Isolate | Only this node's paths | pending |
| 7 | S806 | `copy/map.ts:49` | Button that leaves focus mode (notes line and side panel) | none | Clear focus | Show the whole map | Leave focus | Show all paths | Exit focus | pending |
| 8 | S807 | `copy/map.ts:50` | Note above the map in focus mode | kind, name | Showing only the paths through {kind} {name}. | Focused on {kind} {name}. | Only paths through {kind} {name} are shown. | The map shows the paths through {kind} {name}. | Showing paths through {kind} {name} only. | pending |
