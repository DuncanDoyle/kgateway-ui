# Sub-phase 2.3 — Evidence (Task 50)

The kind e2e and the ui-loop run for view A (design §9.5). Fixture: `test/e2e/fixtures/explorer.yaml`; API checks: `TestComputedAgreesWithController` and `TestForcedDisagreementRaisesKGWUI012` in `test/e2e/api_test.go`; browser checks: `web/e2e/explorer.spec.ts`.

## e2e runs

| kgateway / Gateway API | `make e2e` |
|---|---|
| v2.4.7 / v1.6.1, `UI_EXTRA_ARGS=--poll-interval=10m` | pass: Go 4 tests, Playwright 18 tests (local, 2026-10-25) |
| v2.3.11 / v1.5.1 | pass: Go 3 tests, forced disagreement skipped as in its CI row; Playwright 18 tests (local, 2026-10-25) |

On both tags every listener's computed count equals `attachedRoutes` with `count-agrees`, every evaluated parentRef is `checked` with `status-compatible`, `shop/quirk` is `NoMatchingParent`, `infra/two-refs` counts twice on `infra/edge` `http`, and there is no KGWUI-012.

Forced disagreement (v2.4.7): with the controller at 0 replicas and `edge-https` removed from Namespace `flip`, `flip/flip` read pending, then KGWUI-012 appeared on HTTPRoute `flip/flip` and Gateway `infra/edge` in the model of `since + 30.8 s` (an earlier run: `since + 30.2 s`). It cleared after the controller came back.

After fix round 1 the Go e2e and Playwright suites passed again on a fresh v2.4.7 cluster (forced disagreement skipped: default poll interval).

## ui-loop recordings

These are local recordings on the machine that ran the loop, not committed. Run against v2.4.7 on 2026-10-25.

- Run dir, with `goal.md`, `journal.md` and the probe: `/var/folders/l9/kvw9vb8121720jclk572xwpw0000gp/T/ui-loop/kgateway-ui-claude/task-50/`
- Video: `artifacts/probe-view-A-computed-fact-b5989-pending-unknown-disagreeing/video.webm`
- Screenshots, same folder:
  - Agreeing: `1-agrees-{gateway,gateway-https,listenerset,route}-{dark,light}-{1280,720}.png` (Gateway `infra/edge`, `?listener=https`, ListenerSet `infra/extra`, HTTPRoute `store/secure`)
  - Pending: `2-route-pending-1280.png`, `2-gateway-pending-1280.png` (dark)
  - Unknown: `2-unknown-{listenerset,route}-{dark,light}-{1280,720}.png` (ListenerSet `infra/late` created while the controller was stopped, route `shop/on-late`)
  - Disagreeing: `3-disagrees-{route,gateway}-{dark,light}-{1280,720}.png`, `3-disagrees-gateways-list.png`, `3-disagrees-health.png` (Findings filtered to the two KGWUI-012 warnings)
- `artifacts.1/`, `artifacts.2/`: the first two iterations (the health shot missed the findings list).
- Fix round 1: `m6/artifacts/probe-protocol-cell-stays-on-one-line-at-720-px/listenerset-extra-{dark,light}-720.png` and `…-720-tooltip.png`.

## Findings

Fixed in round 1:
- At 720 px the ListenerSet listener table wrapped the protocol `example.com/odd` onto two lines. It now truncates with the full text in a tooltip (design §8.7); the row keeps the height of its neighbours (`listenerset-extra-light-720-tooltip.png`).

No code change:
- The HTTPRoute CRD lets two refs reach one listener only when one omits the namespace and the other writes it. `infra/two-refs` uses that shape; it gives two report keys and two checks (computed-rules §8.1).
- A route on a denied ListenerSet gets no status entry, but the UI owns it (`pkg/install/assign.go`). `TestRealCluster` now counts owned routes by that rule.

Known gaps:
- Pending is shot only at 1280 px in dark: the 30 s window allows one pass.
- While the controller is down, health reads Unknown beside pending and disagrees (Phase 1 readiness). Expected.
