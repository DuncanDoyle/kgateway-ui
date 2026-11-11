# Sub-phase 6.4 — Route trace, all route kinds

Kind: planned
Base: e0d16a8
Issues: n/a | Plan steps: implementation-plan.md, Sub-phase 6.4, Tasks 40–42

## Problem / goal
An application owner ("customers can't reach the HR system via this URL") needs to see which rule of which route sends traffic to which backend, and why it fails (design §1, §7). Route detail exists only for HTTPRoute today (`pkg/api/details.go:469-477`). 6.4 serves route detail for GRPCRoute, TCPRoute and TLSRoute with kind-specific matches, and turns the Rules section into the route trace: the attachments it serves through, then per rule the match, the filters as written (the Phase 11 slot), a share bar, and each backend with port, weight, share, endpoints and problem, linking to backend detail.

## Expected behaviour

| Input / action | Expected |
|---|---|
| GRPCRoute detail | method match (service/method/type) and headers; weighted refs with shares |
| TCPRoute / TLSRoute detail | ports and weights; `matches: []`; no match line in the UI |
| HTTPRoute detail | existing fields byte-equal; only new optional keys |
| A rule with weights 1/3 | 25 % / 75 % |
| All weights 0 | every ref "0 %, no traffic" |
| A rule mixing delegation and ordinary refs | no share shown |
| TCP/TLS failure wording | "connections fail" |
| A ref's problem with two attachments | worst across attachments, signals naming each attachment; `serves[].state` per attachment |
| Zero-endpoint backend line | its problem, linked to backend detail |
| `effectiveFilters` | reserved, never set (Phase 11) |
| Stale data | every state unknown |
| golden-v1 | byte-equal |

## Root cause (if known)
n/a (new feature).

## Acceptance tests
The test names in plan Tasks 40–41, at the API boundary and Playwright.

## Scope
In: `pkg/api` (route detail for all kinds, trace fields), `pkg/explore` (detail links for the new kinds), the SPA route detail Rules section.
Out: the map (6.5, 6.6); no new retained state (D-057 headroom is 1.5 MiB).

## Plan / design impact
none

## Definition of done
- [ ] acceptance tests fail before, pass after
- [ ] full test suite, lint, web tests and bench pass; bench-6.4.txt
- [ ] wording pass (phase-6.4-wording.md)
- [ ] demo reel of the HR-system story
- [ ] phase-6.4-complete.md written; phase-status.md updated
