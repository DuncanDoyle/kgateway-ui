# Sub-phase 2.2 — Evidence (Task 33)

The kind e2e and the ui-loop run for the explorer (design §9.5). Fixture: `test/e2e/fixtures/explorer.yaml`; API checks: `TestExplorerShowsControllerStatus` in `test/e2e/api_test.go`; browser checks: `web/e2e/explorer.spec.ts`.

## e2e runs

| kgateway / Gateway API | `make e2e` |
|---|---|
| v2.4.7 / v1.6.1 | pass: Go 2 tests, Playwright 7 tests (local, 2026-10-23) |
| v2.3.11 / v1.5.1 | pass: Go 2 tests, Playwright 7 tests (local, 2026-10-23) |

The tags differ only where expected:
- `shop/c-nons` (child ref without a namespace): 2.4 writes an entry, so it is a child; 2.3 writes none, so it is a declared candidate with KGWUI-011 (PF15).
- `shop/replaced`: 2.4 reports `kgateway.dev/Programmed=False/RouteRuleReplaced`, 2.3 `Accepted=False/RouteRuleReplaced`. Both match `state=not-programmed`.
- `shop/aws-be`: `Accepted=False/Invalid` on both; `observedGeneration` 1 on 2.4, 0 on 2.3.

## ui-loop recordings

These are local recordings on the machine that ran the loop, not committed. Run against v2.4.7 on 2026-10-23.

- Run dir, with `goal.md` and `journal.md`: `/var/folders/l9/kvw9vb8121720jclk572xwpw0000gp/T/ui-loop/kgateway-ui-claude/task-33/`
- Video: `artifacts/probe-explorer-pages-on-li-12324-es-widths-loading-and-stale/video.webm`
- Screenshots, same folder: `{dark,light}-{1280,720}-{gateways,routes,listenersets,gateway-edge,listenerset-extra,httproute-store}.png`, `loading-list.png`, `loading-detail.png`, `stale-list.png`, `stale-detail.png`

## Findings

- Stale data turns every list row Unknown. Detail headers keep the server state next to "Data is stale" (`stale-detail.png`); design §6.3 says unknown.
- At 720 px the listener table squeezes the Listener column, so names wrap mid-word (`dark-720-listenerset-extra.png`).
- Found while writing `explorer.spec.ts`: a child with empty `parentRefs` shows "No parentRefs." and no parent card, so its status entry under the synthesized ref is not on the page. A child with hostnames gets `Accepted=False/UnsupportedValue` there on both tags. The fixture gives `shop/c-host` an explicit parentRef so the walk can show the reason.
- kgateway checks every listener's allowedRoutes before the section name. A route on a listener that does not admit its namespace, while another listener does, gets `NoMatchingParent`, not `NotAllowedByListeners` (`v2.4.7 pkg/kgateway/query/route.go:446-540`). The fixture's `NotAllowedByListeners` case uses a namespace no listener admits.
