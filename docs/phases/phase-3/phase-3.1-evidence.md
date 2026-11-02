# Phase 3.1 evidence: shell and navigation

Recorded on 2026-11-02 against the kind e2e cluster (`make e2e KGW_VERSION=v2.4.7 GWAPI_VERSION=v1.6.1`, fixtures `base.yaml` and `explorer.yaml`). The recordings are kept outside the repo. Clips are 1920×1080 in both themes (`-dark` is "Control room", `-light` is "Control room light"); the width screenshots use the width named in the file.

## Automated checks

`make e2e` passes: the Go e2e suite and 43 Playwright tests, including `web/e2e/navigation.spec.ts`.

| Check | Spec |
|---|---|
| Gateway → attached route → child route, each crumb back, reload and a copied URL in a new browser context | `navigation.spec.ts` |
| A filtered routes list and a filtered findings list: object → list crumb restores the filters | `navigation.spec.ts` |
| A 7-entry trail collapses into the "…" menu; a menu item opens its page with the shorter trail | `navigation.spec.ts` |
| The object header on the Gateway, ListenerSet, HTTPRoute and resource pages keeps its box and the trail's as data arrives | `navigation.spec.ts` |
| At 720, 1280, 1600 and 3008 px: no horizontal page scroll; the frame is `min(2400, available)` wide; core detail pages are one column | `navigation.spec.ts` |
| The candidate banner takes the trail's place; nothing else moves | `explorer.spec.ts` |

**Two columns.** Core detail pages contribute no side section (ruling R30), and the e2e build has no extension, so no page in this cluster has a second column. The e2e asserts one column at every width. The two-column grid from 1600 px is covered by the unit tests in `web/packages/shell/src/shell/layout.test.tsx`; a browser check needs a build with a contributed side section and is out of scope here.

## Clips

| File | What it shows |
|---|---|
| `findings-list-dark.webm`, `findings-list-light.webm` | The findings list at 10 a page (the page size is lowered by route interception), then Next, the Warning and Info severity chips and a sort by Code. A pass: the URL gains `cursor`, `severity` and `sort=code`, and the rows change to match each step. |
| `finding-affected-dark.webm`, `finding-affected-light.webm` | A finding detail paging its affected objects two at a time. No e2e finding has affected objects yet, so the reply is filled with 7 real route refs by route interception; a pass is the count of 7 and Next stepping through pages 1 to 3 with Previous enabled after page 1. |
| `trail-walk-dark.webm`, `trail-walk-light.webm` | Gateway infra/edge, then the attached route shop/store, then its child shop/c-host, then back by the crumbs. A pass: the trail reads install › Gateway infra/edge › HTTPRoute shop/store › HTTPRoute shop/c-host, and each crumb lands on its page with the shorter trail. |
| `trail-collapsed-dark.webm`, `trail-collapsed-light.webm` | A route opened with 7 earlier pages in `via`. A pass: the two oldest sit in the "…" menu, five crumbs follow it, and a menu item opens that page. |
| `loading-dark.webm`, `loading-light.webm` | Every API reply held for 2 s, on the Gateway page and the findings list. A pass: the trail, header and tabs show at once, the skeletons fill the body, and the data replaces them without moving the header. |
| `stale-dark.webm`, `stale-light.webm` | The findings list and the Gateway page load, then every API call is refused. A pass: after the next 5 s poll every state shows Unknown and a "Cannot load" notice appears at the top of the page. |

## Screenshots

| Files | What they show |
|---|---|
| `findings-list-*.png`, `findings-page2-*.png`, `findings-filtered-sorted-*.png` | The list on page 1 and page 2, then filtered by severity and sorted by code. |
| `finding-affected-p1-*.png` … `finding-affected-p3-*.png` | The three pages of affected objects. |
| `trail-walk-*.png` | The full trail on the child route. |
| `trail-collapsed-*.png`, `trail-collapsed-menu-*.png` | The collapsed trail, closed and with the menu open and its second item focused. |
| `loading-gateway-*.png`, `loaded-gateway-*.png`, `loading-findings-*.png` | Skeletons, then the loaded Gateway page. |
| `stale-findings-*.png`, `stale-gateway-*.png` | Every state Unknown with the failure notice. |
| `findings-{720,1280,1600,3008}-*.png`, `gateway-{720,1280,1600,3008}-*.png`, `route-trail-{720,1280,1600,3008}-*.png` | The findings list, a Gateway and a route with a trail at each width. scrollWidth equalled innerWidth in all 24; at 3008 px the content is 2400 px wide and centred. |
