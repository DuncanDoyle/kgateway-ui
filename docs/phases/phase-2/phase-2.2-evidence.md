# Sub-phase 2.2 — Evidence (Task 33)

The kind e2e and the ui-loop run for the explorer (design §9.5). Fixture: `test/e2e/fixtures/explorer.yaml`; API checks: `TestExplorerShowsControllerStatus` in `test/e2e/api_test.go`; browser checks: `web/e2e/explorer.spec.ts`.

## e2e runs

| kgateway / Gateway API | `make e2e` |
|---|---|
| v2.4.7 / v1.6.1 | pass: Go 2 tests, Playwright 7 tests (local, 2026-10-23; rerun after fix round 1) |
| v2.3.11 / v1.5.1 | pass: Go 2 tests, Playwright 7 tests (local, 2026-10-23; rerun after fix round 1) |

The tags differ only where expected:
- `shop/c-nons` (child ref without a namespace): 2.4 writes an entry, so it is a child; 2.3 writes none, so it is a declared candidate with KGWUI-011 (PF15).
- `shop/replaced`: 2.4 reports `kgateway.dev/Programmed=False/RouteRuleReplaced`, 2.3 `Accepted=False/RouteRuleReplaced`. Both match `state=not-programmed`.
- `shop/aws-be`: `Accepted=False/Invalid` on both; `observedGeneration` 1 on 2.4, 0 on 2.3.

## ui-loop recordings

These are local recordings on the machine that ran the loop, not committed. Run against v2.4.7 on 2026-10-23.

- Run dir, with `goal.md` and `journal.md`: `/var/folders/l9/kvw9vb8121720jclk572xwpw0000gp/T/ui-loop/kgateway-ui-claude/task-33/`
- Video: `artifacts/probe-explorer-pages-on-li-12324-es-widths-loading-and-stale/video.webm`
- Screenshots, same folder: `{dark,light}-{1280,720}-{gateways,routes,listenersets,gateway-edge,listenerset-extra,httproute-store}.png`, `loading-list.png`, `loading-detail.png`, `stale-list.png`, `stale-detail.png`. Re-taken after fix round 1.
- `artifacts/implicit-child.png`: the implicit parent card on `shop/c-ihost`.
- `artifacts-iteration-1/`: the first run, before the fixes.

## Findings

Fixed in round 1:
- Stale data or a failed poll turns every state on a detail page unknown: header, conditions, listeners, Backends and sub-tables (`stale-detail.png`), as the lists do (design §6.3).
- A status entry no spec parentRef names gets its own card: "Implicit parent" when parentRefs is empty, else "Not in parentRefs". Fixture `shop/c-ihost` shows `Accepted=False/UnsupportedValue` on it (`implicit-child.png`).
- Below 900 px the listener table moves Hostname into the expanded row and truncates names with a tooltip; names no longer wrap mid-word (`dark-720-listenerset-extra.png`). A long protocol value can still wrap.

No code change:
- kgateway checks every listener's allowedRoutes before the section name. A route on a listener that does not admit its namespace, while another listener does, gets `NoMatchingParent`, not `NotAllowedByListeners` (`v2.4.7 pkg/kgateway/query/route.go:446-540`). The fixture's `NotAllowedByListeners` case uses a namespace no listener admits.
