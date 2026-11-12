# Sub-phase 6.2 — Path model, paths API, path list

Kind: planned
Base: 6bf3a46
Issues: n/a | Plan steps: implementation-plan.md, Sub-phase 6.2, Tasks 22–32

## Problem / goal
A platform operator with hundreds or thousands of routes gets a problem report and cannot quickly see which Gateways, listeners and routes a broken backend affects (design §1). 6.2 builds the one path model all Phase 6 views read (design §4), serves it as `GET /api/v1/installs/{id}/paths` (§5) with per-row authorisation (§13), and adds the "Traffic" path list page.

## Expected behaviour

| Input / action | Expected |
|---|---|
| Open Traffic from the install nav | every Gateway of the install, problems first (error, warning, unknown, healthy), then Gateway, listener, route, rule |
| A rule with weights 90/10 and no delegation ref | shares 90 % and 10 %, "% of this rule's requests" |
| A rule mixing a delegation ref and an ordinary ref | no share shown |
| All weights 0 | every ref "0 %, no traffic" |
| An unresolved ref attributed by KGWUI-013/014 | its share "failing"; unattributed candidates "may fail" |
| A Service with no ready endpoints for the referenced port | error on that port's rows; ready/total equals KGWUI-023's evidence for the port |
| Endpoints capability off, denied or failing | Service rows "unknown", summary says endpoints were not read; never "0 ready", never healthy |
| Healthy backend behind a Gateway with an xDS reject or listener `Programmed=False` | error |
| Route status older than its spec | "waiting for the controller", not error |
| Delegated child route | its rows under the root attachment, with the route chain; view C uncertainty shown |
| A rule with no backend | the written action: redirect, direct response, or "no backend" |
| One broken backend used by 37 routes on 4 Gateways | summary: 1 backend, 37 routes, 4 Gateways |
| Restricted viewer | rows with a hidden dependency absent from rows, counts and filter options; "some paths are hidden"; memo hits authorised again |
| Unknown param, `limit` 0 or 201, bad cursor | 400 |
| Gateway / backend / route entry points | open with `gateway=` / `backend=` / `route=` set; filters, sort and cursor in the URL |
| Existing API responses | `golden-v1` byte-equal; current goldens unchanged except new optional keys |
| Retained heap, worst world | within +48 MiB, measured A/B against the base (single runs swing 39–45 MiB) |

## Root cause (if known)
n/a (new feature).

## Acceptance tests
The test names in plan Tasks 22–31, at the API boundary (goldens and handler tests) and Playwright for the page.

## Scope
In: `pkg/objects`, `internal/testutil/scale`, new `pkg/paths`, `pkg/health` (exported per-port count), `pkg/api` (paths endpoint, visibility), bench tests, the SPA Traffic page.
Out: backends pages and the scoped reader (6.3), route detail changes (6.4), the map (6.5, 6.6).

## Plan / design impact
none

## Definition of done
- [ ] acceptance tests fail before, pass after
- [ ] full test suite, lint, web tests and bench pass; heap A/B against the base recorded in bench-6.2.txt
- [ ] wording pass done (phase-6.2-wording.md)
- [ ] demo reel of the Traffic page with real data
- [ ] phase-6.2-complete.md written; phase-status.md updated
