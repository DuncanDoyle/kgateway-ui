# Sub-phase 4.2 — User-testing follow-ups

Kind: planned (added by D-063 after user testing on 2026-11-07)
Base: 9d98451b4a472666f3d4c018f7c8d2e191fd9b4d
Issues: n/a | Plan steps: D-063 items 1–3

## Problem / goal
The user tried Snellius on a minikube cluster with kgateway 2.5.0 (2026-11-07) and raised three points.
1. After the browser tab sat in the background for about 9 minutes, the UI said "Data is stale. Last checked 9m ago", then recovered by itself a moment later.
2. The Gateways, Routes and ListenerSets lists and the install overview show "Error" or "Warning" on a row, but not why.
3. There is no About page naming the product, its version and what it is watching.

## Expected behaviour

| Input / action | Expected |
|---|---|
| Tab hidden for minutes, then shown; backend answers normally | No stale banner, no "unknown" states: the data counts as fresh until the first poll after the tab is shown has answered |
| Tab shown, and the first poll after it fails or returns data older than the stale limit | The stale banner and unknown states appear, as today |
| Tab visible, backend stops answering | Stale after two poll intervals, as today |
| Hover or keyboard focus on a row's "Error"/"Warning" in the Gateways, Routes, ListenerSets lists and the overview Gateway table | A tooltip with the deciding finding: `<code>: <message>`, plus "+N more" when the object has other findings of that severity or lower |
| Same, for a row whose state comes from an install-wide check, not a finding of its own ("unknown") | Fixed text naming the cause, e.g. a required check is not OK |
| Same, for a route marked warning because its Gateway rejects config | Fixed text saying the parent Gateway rejects configuration |
| A healthy row | No tooltip (or the plain state), no extra payload |
| A caller who cannot view every object in a finding's evidence | The tooltip shows only code and message, never Detail or evidence (Phase 2 rule 7) |
| Open the About page (from the header brand) | Large `snellius` wordmark with the symbol, "Kubernetes Gateway Management & Observability", "See the whole picture.", the UI version from `/api/v1/meta`, each watched install as edition, version and controller name (e.g. "kgateway 2.5.0"), links to the GitHub repo, the docs and the licence |
| A downstream brand is set | The About page shows the downstream product name and logo, not Snellius (D-008) |

## Root cause (if known)
1. SWR does not poll hidden tabs, while the stale check compares `collectedAt` with the wall clock (`web/packages/shell/src/api/hooks.ts:31-35`). On return, the clock has moved but no fetch has run.
2. Row DTOs carry the state only (`pkg/api/explore_types.go` `RouteRow`, `GatewayListRow`, `ListenerSetRow`; `OverviewGatewayRow`). The deciding finding is already kept per object (`pkg/explore/explore.go` `Worst`, `ix.Findings`).
3. Not built.

## Acceptance tests
- Web: a hooks or Header test that hides the tab, advances time past the stale limit, shows the tab, and asserts no stale banner until the revalidation resolves; and that a failing first poll does show it.
- API: list and overview handler tests assert the row reason (code, message, count) for an error row, a warning row, an unknown row, a lifted route and a healthy row (no reason); and that Detail never appears.
- Web: tooltip tests on each table (hover and focus) for those cases.
- Web: About page test with the default brand and with a downstream brand; version and installs from stubbed `/meta` and installs.
- Visual: `/ui-loop` screenshots of a tooltip and the About page in both themes.

## Scope
In: `web/packages/shell/src` (hooks, tables, Tooltip use, a new About page and route, header link), `pkg/api` row DTOs and builders, `pkg/explore` (a count in `Worst`), tests, README (one line on the About page), `docs/phases/phase-4/design.md` (a §12 for 4.2).
Out: changing which finding decides a state; new endpoints; a tooltip on detail pages; build metadata beyond the existing version.

## Plan / design impact
D-063 adds these items to Phase 4. Design §12 describes them. No budget change.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite and gates pass
- [ ] cross-model review of `git diff 9d98451..HEAD` + triage
- [ ] phase-4.2-complete.md written; phase-status.md updated
