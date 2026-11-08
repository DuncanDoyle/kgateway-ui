# Sub-phase 5.5 — Web

Kind: planned
Base: b4db2ca
Issues: n/a | Plan steps: implementation-plan.md, Sub-phase 5.5 (G5), Tasks 13–15; design §10

## Problem / goal
The SPA assumes every install is kgateway. A class-only install (5.1) gets kgateway copy, a Delegation tab slot, data plane and proxy sections, and a computed badge whose label is parsed from a kgateway rule-set ID. The server knows what each install supports but does not say so. The empty state says "No kgateway install found" in the core vocabulary, and the Backend kind is matched by the `gateway.kgateway.dev` group.

The SPA stays one bundle. kgateway copy and the Delegation tab become a web pack (`kgatewayWebPack`), merged into the default app's `ShellExtension`; surfaces show per install from server-sent features. A kgateway install looks and behaves as at the base.

## Expected behaviour

| Input / action | Expected |
|---|---|
| `GET /api/v1/installs`, kgateway install | `features: ["delegation","computed","dataplane","processes"]` |
| Same, class-only install | `features: []` |
| Same, a rule set without `computed.Delegator` | no `delegation` |
| A computed fact on the wire | `label` beside `rules`, e.g. `"kgateway 2.4"`; details carry `ruleSetLabel` beside `ruleSet` |
| `golden-v1` | untouched; `additive_test.go` passes; current goldens gain only `features`, `label` and `ruleSetLabel` |
| Core vocabulary, no installs | "No Gateway API controller found", no kgateway labels; with `kgatewayWebPack` today's label hint |
| Computed badge tooltip | the server's label; an ID without one is shown as sent |
| Backend lookup link, Backend icon | from the `/meta` kind category `backend`, not the group |
| About page, class-only install | "Gateway API controller" for the empty edition and version |
| Route page, install with `delegation` | Delegation tab (from the web pack), permission line, child routes and effective matches as today |
| Route page, install without `delegation` | none of these |
| Gateway page, install without `dataplane`/`processes` | one line, "Not available for this controller", instead of the data plane and proxy sections |
| Computed badges, install without `computed` | not shown |
| Server that sends no `features` (older) | today's behaviour: every feature on; the Delegation tab keeps its `/meta` filters check |

## Root cause (if known)
`InstallSummary` (`pkg/api/types.go:15`) has no feature list. The web reads kgateway constants: `vocabulary.tsx:30`, `ComputedBadge.tsx:82`, `ObjectHeader.tsx:85`, `ResourcePage.tsx:33`, `AboutPage.tsx:136`, `RoutePage.tsx:15`.

## Acceptance tests
- `pkg/api`: install features for kgateway, class-only and attachment-only installs; `label`/`ruleSetLabel` on a route and Gateway detail; goldens additive.
- `pkg/app`: the app's handler lists the pack's features (wiring through `Registry.FeaturesFor`).
- `pkg/extension`: `New()` gives kgateway installs the kgateway pack's features; `NewCore()` none.
- Vitest: vocabulary, badge label, backend by category, About fallback; Delegation tab, permission line, child and effective blocks only with `delegation`; computed badges only with `computed`; Gateway page line without `dataplane`/`processes`.
- Playwright (kgateway e2e) unchanged; screenshots of kgateway and class-only installs against a stubbed API.

## Scope
In: `pkg/api` (features, labels), `pkg/computed` (rule-set label), `pkg/engine`/`pkg/explore` (carry the label), `pkg/extension`, `pkg/kgateway`, `pkg/app` (wiring), `web/packages/shell`, `web/app/src/main.tsx`.
Out: install page Processes section and data plane column (the server already reads "not applicable"); a separately built web pack (design §10 revisit); policy and Backend judgement (carry-over).

## Plan / design impact
Two deviations, recorded here and in the complete file:
- `InstallSummary.Features` is always sent (`[]` when none), not `omitempty`: an absent list must mean an older server, so the SPA keeps today's behaviour there.
- The web pack file is `kgateway/webPack.tsx` (it holds JSX), and details gain `ruleSetLabel` beside `ruleSet` so the hostnames column tooltip needs no parsing either.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite and gates pass
- [ ] cross-model review of `git diff b4db2ca..HEAD` + triage
- [ ] phase-5.5-complete.md written; phase-status.md updated
