# Sub-phase 2.4 — Evidence (Task 69)

The kind e2e and the ui-loop run for views B and C (design §9.5). Fixture: `test/e2e/fixtures/explorer.yaml` (route `shop/tree` and its children, after the view A objects); API checks: `TestDelegationAgreesWithController` in `test/e2e/api_test.go`; browser check: the Delegation tab test in `web/e2e/explorer.spec.ts`.

## e2e runs

| kgateway / Gateway API | `make e2e` |
|---|---|
| v2.4.7 / v1.6.1, `UI_EXTRA_ARGS=--poll-interval=10m` | pass: Go 5 tests, Playwright 19 tests (local, 2026-10-28) |
| v2.3.11 / v1.5.1 | pass: Go 4 tests, forced disagreement skipped as in its CI row; Playwright 19 tests (local, 2026-10-28) |

On both tags every `child-entry` (16) and `parent-resolvedrefs` (4) check is `checked` under the tag's rule set, `status-compatible` or `not-verified`, and there is no KGWUI-012. The API serves no `parent-resolvedrefs` check, so the test runs the backend in-process against the cluster (`app.Build`) to list every check; rows and health come from the deployed UI.

| Case | Both tags |
|---|---|
| `shop/t-pruned` (ancestor-pruned, rules §5.1) | `dropped`, no entry expected, none written |
| `shop/t-partial` (one rule, `/tree/a` and `/tree/b`) | `partial`, one `Accepted=True` entry; effective matcher kept under `/tree/a`, dropped under `/tree/b` |
| `shop/t-nobe` (missing Service) | `attached`, `status-compatible`; entry `ResolvedRefs=False/BackendNotFound` |
| `shop/t-port` (parentRef with only a port) | `attached`; entry carries `port: 8080` |
| `shop/t-mid`, `shop/tree`, `shop/store` | `parent-resolvedrefs` `False/BackendNotFound`, reason compared and equal |
| `shop/c-cycle` | `parent-resolvedrefs` `False`, reason not compared (Service backendRef); observed `RefNotPermitted` |
| 2.2 tree under `shop/store` | forbidden, header-mismatch, cycle and hostname children compatible |
| `shop/c-nons` | 2.4: entry as written, `status-compatible`; 2.3: no entry, `not-verified` |

Forced disagreement (v2.4.7): KGWUI-012 on `flip/flip` and `infra/edge` in the model of `since + 30.1 s`.

## ui-loop recordings

Local recordings, not committed. Run against v2.4.7 on 2026-10-28.

- Run dir, with `goal.md`, `journal.md` and the probe: `/var/folders/l9/kvw9vb8121720jclk572xwpw0000gp/T/ui-loop/kgateway-ui-claude/task-69/`
- Agreeing and truncated ran on the deployed UI; truncated added `many.yaml` from the run dir (21 parent matches to one inheriting child). Unknown and restricted ran the same backend in-process (`server/` in the run dir): edge cap 14, and an Authorizer that denies Namespace `shop`.
- Videos (1920×1080 canvas): `artifacts/probe-{1-agreeing…,2-truncated…,3-unknown…,4-restricted…}/video.webm`
- Screenshots, in each test's folder, `{dark,light}-{1280,720}`:
  - Agreeing: `1-agree-delegation-*.png` (`shop/tree`, `shop/t-partial` row open), `1-agree-effective-*.png`
  - Truncated: `2-truncated-delegation-*.png` (20 of 21 paths), `2-truncated-effective-*.png`
  - Unknown: `3-unknown-delegation-*.png` (`shop/t-mid` → `shop/t-pruned`), `3-unknown-cut-ref-*.png`, `3-unknown-effective-*.png`
  - Restricted: `4-restricted-delegation-*.png`, `4-restricted-effective-*.png`
- Task e2e shots: `web/test-results/explorer-delegation-partial.png`, `explorer-effective-matches.png`.

## Findings

Open:
- A ref cut by the edge cap reads "This ref selects no routes." with no badge (`3-unknown-cut-ref-*.png`). The API sends no row and no ref `computed` for it, so nothing says "too large". Rules §5.7 and D-025 say it reads too large to compute.

No code change:
- An effective matcher is not `unknown` under the edge cap: a cut ref opens no edge. Unknown effective matchers need the context or matcher caps, which are not configurable.

Known gaps:
- 2.3 was not recorded in the ui-loop; its e2e covers the same pages.
