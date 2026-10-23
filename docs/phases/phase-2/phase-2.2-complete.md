# Phase 2.2 — Explorer on observed status (Complete)

**Completed:** 2026-10-23
**Range:** 77fc4dc..HEAD (branch `duncandoyle/explorer`, PR #8)

## Summary
The UI gains a Gateway and route explorer built on what the controller reports:
- **Ownership (Q12):** delegated child routes belong to an install through reported status edges, found breadth-first from Gateway-attached routes. Declared candidates (a parentRef to an HTTPRoute with no status entry for it) get KGWUI-011, info and pending inside grace, a warning after. Out-of-scope routes, implicit children included, get KGWUI-004.
- **Backend:** the `gateway.kgateway.dev/Backend` kind with its `Accepted` rules; `Accepted=Unknown` reads unknown, a current `Accepted=True` reads "Configuration accepted".
- **API:** paged lists of Gateways, ListenerSets and routes, with keyset cursors, strict params, filters and the `backend=` reverse lookup. Detail endpoints for Gateways, ListenerSets and HTTPRoutes, with up-chain ancestors and links that say whether a detail page, a YAML view or nothing serves each ref. Every row, count, link and ancestor goes through the per-request Authorizer view; AllowAll makes no Authorizer calls.
- **SPA:** Gateways, Routes and ListenerSets lists, and Gateway, ListenerSet and HTTPRoute pages with conditions, listeners, parent cards (implicit parents included), rules as written, child routes, sub-tables and a YAML tab. Finding and Gateway-table links open the detail pages. Stale data turns every state unknown, while notices keep their icons. Pages retry a failed request every 5 s, except a 400.
- **Extensions:** optional `CorePages` slots for the six explorer pages; the explorer paths are reserved. `SHELL_API_VERSION` stays 1.

## Reviews
- **Pre-flight scan:** PF1–PF33, all ruled before Task 11.
- **Task reviews:** every task had a spec and quality review; fix rounds were confirmed by scoped re-reviews or by the controller for small diffs.
- **Cross-model (Codex) review of the sub-phase:** `reviews/2.2-codex.md`. Five findings: 1 P1 (malformed query encoding dropped filters), 2 P2 (spec-only parents had no link; sub-tables hid the notice after a failed poll), 1 P3 (layout tests asserted CSS values), 1 P4 (explicit `matches: []` shown as the default match). All accepted and fixed; the Codex re-review found four resolved and no new issues, and the open part of the P3 was closed in `0855652`.

## Waivers
none

## Deliverables
- `pkg/objects` typed views and `RouteView`; `pkg/engine` decode cache; `pkg/install` status-based assignment and delegation; `pkg/health` delegation, Backend and KGWUI-011 checks; `pkg/explore` index, rows, admission, queries, cursors and ancestors; `pkg/api` list and detail endpoints, link metadata, strict params, the Authorizer view, goldens and the link-agreement, file-case and additive golden tests.
- `web/packages/shell`: explorer types and hooks, `objectPath`, `LinkedName`, copy buttons, install navigation, list and detail pages, the stale view, `retryEveryPoll`, `copy/explorer.ts`.
- `test/e2e/fixtures/explorer.yaml`, `TestExplorerShowsControllerStatus`, `web/e2e/explorer.spec.ts` (walk, filters, themes and layout geometry).
- Budget tests and benchmarks for 2.2 (`make bench`, `make bench-stream`); the bench workflow covers the explorer paths.
- README upgrade notes; wording record `phase-2.2-wording.md`; evidence record `phase-2.2-evidence.md`.
- Gates on `0855652`: `go vet` (plain, `bench` and `e2e` tags), `go test ./... -race` over 20 packages, golangci-lint 0 issues; Vitest 443/443, typecheck, Biome and build; no forbidden terms in the tree. kind e2e passed locally on v2.4.7 (Go 2/2, Playwright 17/17) and on v2.3.11 (earlier in the sub-phase), and in PR #8's CI matrix.

### Benchmarks (Large tier, CI runner, `bench/2.1-ci.txt` and `bench/2.2-ci.txt`)

| Measure | 2.1 | 2.2 | Limit |
|---|---|---|---|
| Warm recompute | ~92 ms (Xeon) | ~55 ms (EPYC) | ≤ 100 ms |
| Allocation per recompute | 26.7 MB | 23.1 MiB | ≤ 32 MiB |
| Retained heap over the 2.1 baseline | — | +7.2 MiB | ≤ 32 MiB |
| Stream (1 update / 100 ms, 20 pollers) | — | 0.93 recomputes/s, 8.9 % of one core | ≤ 1/s, ≤ 15 % |
| List p95, sequential | — | ≤ 3.4 ms | < 50 ms |
| List p95, 20 clients | — | worst 73.5 ms (restricted, `sort=health`) | < 100 ms |
| Route detail, deepest child, p95 | — | 43 µs | < 50 ms |

The 2.1 and 2.2 runners differ in CPU, so compare across rows with care. The 10-minute stream ran once on a laptop: 0.94 recomputes/s, 7.2 % of one core (`bench/2.2-stream-10m.txt`).

## Release note
In README "Upgrade notes": delegated children with a status entry now count and can colour the install; KGWUI-011 for declared candidates, including the kgateway 2.3 case; the stale-entry limitation after a child edit; the KGWUI-004 detail naming the target kind; the optional page slots and reserved paths; retries every 5 s after an outage.

## Notes for later phases
- **20-client restricted list sort** uses 74 % of its budget on the CI runner. Recheck when 2.3 adds computed listeners.
- **List mix "listener"** stands in as a ListenerSet parent until 2.3 adds a listener index.
- **kgateway reason order:** a route on a section whose listener rejects its namespace gets `NoMatchingParent` when another listener admits it (design §9.5).

## Rulings made during execution
Recorded in the plan workspace ledger; the ones that changed behaviour or the design:
- `parent=<HTTPRoute>` with no `attachment` lists reported children only; declared and unwatched rows need their `attachment` value (design §5, §7.2).
- AllowAll is matched by exact type, so an embedding Authorizer is still called.
- Detail links follow the server's detail kinds; downstream ListenerSet kinds link to the ListenerSet page.
- Lists keep the previous query's rows while a new one loads, marked busy; an error on the new query never shows the old rows (plan Task 26 note).
- Stale view covers states, not notices (design §6.3).
- Table names truncate to one line with the full name in a tooltip (design §8.7).
- The Gateway detail carries its visible ListenerSet count (design §7.4 rule 5).
- A hidden parent Gateway reads "Not available" (design §8.5).

## Deferred / open
- (P3) Gateway routes column and sort from the computed count — 2.3.
- (P4) A downstream route contribution is checked for reserved paths, but there is no warning for other overlaps with core pages (router ranking decides, as in Phase 1).
- (P4) No test for retry behaviour on unmount or with two consumers of one key; the SWR source shows it correct.
