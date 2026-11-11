# Sub-phase 6.4 — Wording

11 strings, five options each; all new SPA copy of the route trace on route detail (`web/packages/shell/src/copy/explorer.ts`, `traceCopy`) and the route's Traffic entry (`copy/traffic.ts`).

Option 1 is provisional and is what the code uses today; the pick happens in the 6.4 close (Task 42). Placeholders stay in order. Ids run from S753, after the highest id in `phase-6.3-wording.md` (S752). The trace also reuses earlier strings unchanged: S617 "% of this rule's requests", S616 "Share", S618 "Endpoints", S626, S630, S632, S634–S639 and the signal sentences S651–S660.

| # | id | File:line | Where | Placeholders | Option 1 (draft) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | S753 | `copy/explorer.ts:655` | Section title above the rules: the attachments the route serves through | none | Serves through | Reached through | Attached to | Entry points | Where it serves | 1 (provisional) |
| 2 | S754 | `copy/explorer.ts:656` | Serves section: no visible path reaches the route | none | No path you can view reaches this route. | No visible path reaches this route. | Nothing you can view sends traffic here. | No path to this route is visible to you. | This route has no path you can see. | 1 (provisional) |
| 3 | S755 | `copy/explorer.ts:657` | Serves line: the listener | `{name}`, `{port}` (optional) | Listener {name} :{port} | Listener {name} on :{port} | {name} :{port} | Listener {name}, port {port} | Port {port} ({name}) | 1 (provisional) |
| 4 | S756 | `copy/explorer.ts:658` | Match line label before a GRPCRoute method match | none | gRPC method | Method | gRPC | Service/method | RPC | 1 (provisional) |
| 5 | S757 | `copy/explorer.ts:659` | Filters area of a rule with no filters | none | No filters. | None. | No filters on this rule. | This rule has no filters. | No filters written. | 1 (provisional) |
| 6 | S758 | `copy/explorer.ts:660` | Share bar flag: the share whose ref fails (KGWUI-013/014) | `{pct}`; requests or connections by route kind | {pct} of requests fail | {pct} of requests will fail | {pct} fail | Failing: {pct} of requests | {pct} of requests cannot be sent | 1 (provisional) |
| 7 | S759 | `copy/explorer.ts:661` | Share bar flag: the share of unattributed candidates | `{pct}` | {pct} may fail | {pct} might fail | Possibly failing: {pct} | {pct} at risk | {pct} could fail | 1 (provisional) |
| 8 | S760 | `copy/explorer.ts:662` | Share bar note: every weight in the rule is 0 | none | No traffic: every weight is 0 | No traffic: all weights are 0 | Every weight is 0, so no traffic | All weights 0: nothing is sent | No traffic (weights all 0) | 1 (provisional) |
| 9 | S761 | `copy/explorer.ts:663` | Share bar label (screen readers) | `{parts}` = "name share" list | Share of this rule's requests: {parts} | Split of this rule's requests: {parts} | This rule sends {parts} | Requests split: {parts} | Rule share: {parts} | 1 (provisional) |
| 10 | S762 | `copy/explorer.ts:664` | Backend line: the attachment a group of signals came from | `{gateway}`, `{listener}` | On {gateway}, listener {listener}: | Through {gateway}, listener {listener}: | Via {gateway} / {listener}: | {gateway}, listener {listener}: | From {gateway} ({listener}): | 1 (provisional) |
| 11 | S763 | `copy/traffic.ts:30` | Route detail link to Traffic with route= | none | Paths through this route | Traffic for this route | Show this route's paths | Open in Traffic | This route's paths | 1 (provisional, matches S669) |
