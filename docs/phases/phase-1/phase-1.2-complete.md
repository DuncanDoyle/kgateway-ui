# Phase 1.2 — Frontend (Complete)

**Completed:** 2026-10-20
**Range:** f544471..75ff683 (branch `duncandoyle/phase1Frontend`)

## Summary
The Phase 1 web UI is done and embedded in the binary. Operators get a cluster landing (one card per install), an install page that answers "is it healthy, and if not, why" at a glance (health strip, top findings, count tiles with scope, Gateway table), a findings page with URL-backed filters, and a status-first YAML view, in "Control room" dark and light themes. Stale, failed or unprocessed data never shows as healthy on any surface (D-006). Restricted findings keep severity, code and resource and say "Restricted: ask the owning team" (D-013). A downstream build adds pages, nav entries, themes and finding renderers through the public `@kgateway-ui/shell` API, guarded by an export-surface snapshot test.

## Reviews
- Per-task spec + quality reviews for Tasks 12–18; Tasks 13, 14, 16 and 18 needed one fix round, Task 17 four (URL-backed filters under router transitions).
- Pre-flight run of the plan's code: 17 rulings before Task 12 (versions, pre-React paint, fonts, tests).
- Wording pass: 70 strings extracted, 59 with five options each, chosen by a separate agent; 29 changed under 15 consistency rules.
- Whole-branch review (most capable model): 6 Important, all fixed in one wave; its re-review found one new Important (kept data green after a failed poll), fixed in one targeted follow-up (a recorded process deviation).
- Cross-model review of this diff happens at the phase review (planned sub-phase).

## Waivers
none

## Deliverables
- `web/packages/shell` (themes, health primitives, API client and hooks, shell, pages, extension registry), `web/app`, `internal/webui` (placeholder when not built; built SPA via `make build`; source maps not embedded).
- Pinned stack: React 19, Vite 7, @vitejs/plugin-react 5, Vitest 4 (test.projects), jsdom 26, Biome, emotion, react-router 7, SWR, TanStack Table/Virtual, lucide-react, variable Figtree and JetBrains Mono.
- Vendored CNCF white kgateway icon with source commit and trademark note.
- 257 Vitest tests; `go test ./...` (with REQUIRE_SPA after `make build`) passes; `git status` clean after `make build`.

## Notes for later phases
- The theme default is dark unless stored or set by a downstream (D-011 as written); following the OS preference is a small switch if wanted.
- One `useUrlParams` instance per page by design.
- Visual evidence (Playwright clips and screenshots): /var/folders/l9/kvw9vb8121720jclk572xwpw0000gp/T/ui-loop/kgateway-ui-claude/ (local only).

## Deferred / open
- Parked: typing in a filter before a route switch commits can cancel the switch (needs scripted input; fix would use UNSAFE_ router APIs).
- Parked: during a failing poll the strip reads "Data is stale" even when the data is seconds old.
- Deferred: shared tint helper and duplicate buttons; literal breakpoints; Gateway table virtualisation; cache headers on /assets; SPA fallback for missing hashed assets; sub-path hosting.
- 1.3: render-loop guard (idle-CPU/render-count) in the Playwright e2e; Go scale-test timing budget under load before CI.

## Rulings made during execution
- - Ruling: T14 API types follow preflight.md §1 (CheckResult.detail, ScopeSummary.source + typed selectors, full MetaResponse kinds, GatewayRow.addresses non-null); fix the meta literals in T14 L153 and T16 L39-44 — cost: none.
- - Ruling: T12 pins @vitejs/plugin-react ^5 and jsdom ^26 (fit vite ^7 and Node 22.17) — cost: none.
- - Ruling: T12 uses vitest ^4.1.11 (critical advisory in ^3) with `test.projects` instead of vitest.workspace.ts — cost: config shape differs from the plan.
- - Ruling: T12 index.html sets the page background from the stored theme or prefers-color-scheme in an inline script before React loads — cost: none.
- - Ruling: T12 shell package.json declares every package its source imports (TanStack, js-yaml, fontsource) as dependencies/peers — cost: none.
- - Ruling: all tasks run `biome check --write` before committing; T12 excludes vendored SVG assets from Biome — cost: none.
- - Ruling: T13 bundles variable fonts (@fontsource-variable/figtree, @fontsource-variable/jetbrains-mono) so 400–700 weights exist — cost: slightly larger bundle.
- - Ruling: T14 ResourceName exposes an accessible name "namespace/name" without the visual spacing artefact (e.g. aria-label on the wrapper); the plan's tests that query by "a/one" stay — cost: none.
- - Ruling: T15 logo: the single-fill colour logo is 1.85:1 on the ink header; use the CNCF artwork's white/reverse variant for dark backgrounds if it exists, and record the source path, commit SHA and licence/trademark note next to the file — cost if wrong: brand-guideline mismatch, fixable by swapping one file.
- - Ruling: T15/T16/T17 every interactive element gets hover and active styles (MenuButton, menu items, nav links, sort buttons, "Show all", findings chips) — cost: none.
- - Ruling: T15 theme-switch test must assert the token change it claims — cost: none.
- - Ruling: T16 stale test restores timers in afterEach/finally and drives time via fake timers + setSystemTime; fixtures build timestamps relative to the test clock, never at import time — cost: none.
- - Ruling: T16 skeleton matches the healthy layout's dimensions (no layout shift); an unknown install never reads "No known problems" — wording goes through the wording pass — cost: none.
- - Ruling: T16 replace array-index keys with stable keys — cost: none.
- - Ruling: T17 virtualised findings list: test setup supplies element sizes (getBoundingClientRect/ResizeObserver stubs) so rows render in jsdom; keep virtualisation — cost: none.
- - Ruling: T17 findings search ignores missing resource/namespace fields (never matches the string "undefined") — cost: none.
- - Ruling: T18 never commits the built SPA: the tracked placeholder index.html stays; built assets are produced by `make build`/the image build; the REQUIRE_SPA embed test runs against a fresh build only — cost: none.
- Task 13: Ruling: light focus #4F6BFF accepted (3:1+ on bg/surface/header; #002DC9 was 1.85:1 on the header) — cost: focus ring less on-brand.
- Task 13: Ruling: theme precedence follows D-011 literally — stored id → defaultThemeId → dark; no OS step in the provider or index.html (pending the user's answer; OS-following is a small switch) — cost if wrong: light-OS users first see dark.
- Task 13: Ruling: provider writes a paint record {bg, scheme} under a themeStorage key on every resolution; index.html reads only that (validated hex + scheme), else dark — fixes downstream theme flash (D-008) without theme ids — cost: first visit after a token change paints the previous colour once.
- Task 13: Ruling: add color.borderStrong (≥3:1 on bg, surface, surfaceRaised; tested) for control boundaries and the scrollbar thumb; border is documented decorative — cost: none.
- Task 14: Ruling: ResourceName dims the namespace with theme.color.textMuted (contrast-tested ≥4.5:1 on bg and surface, both themes), not opacity — plan-mandated 0.55 opacity failed AA in light — cost: none.
- Task 14: Ruling: isStale fails closed on an empty/unparsable collectedAt; HealthReport.checks typed `CheckResult[] | null` (Go sends null when no check ran) and normalised to [] in useHealth; apiFetch wraps a non-JSON 2xx body as ApiError; HealthIcon's styled size prop must not leak to the DOM — cost: none.
- Task 15: Ruling: install scope is shown by Task 16's scope tile ("{watched} / {total} namespaces", with source when from the template), not in the header; amend design §6.5 at close — cost: one less header item.
- Task 15: Ruling: the edition tag renders only when edition differs from brand.productName (upstream "kgateway kgateway" adds nothing; downstream editions stay visible) — apply in the final fix wave — cost: brief's edition test adjusts.
- Task 16: Ruling: an unknown install's strip headline leads with "Health unknown" plus the reason (e.g. "Health unknown — served kinds could not be checked"), never "No known problems"; warnings counts follow — apply in this task's fix round or the final wave — cost: none.
- Task 16: Ruling: fixtures are builder functions (time-relative); Tasks 17/18 call them — cost: none.
- Task 16: Ruling: frontend renders non-error Gateway rows unknown when a required health check is not ok (mirrors backend rowState; closes the poll-window disagreement); empty headline falls back to the state label; memoise GatewayTable/CountTiles so the 1 s clock tick doesn't re-render rows — cost: none.
- Task 17: Ruling: text filters hold local state and mirror to the URL with replace; param updates build from a ref of the latest params; apply the same fix to Task 16's GatewayTable ?q= (same root cause); add Playwright assertions that a chip + text change keeps both params and aria-pressed — cost: none.
- Task 17: Ruling: replace the pending/reconcile machinery with a simpler model — local state is the source of truth; it is mirrored to the URL with replace; the URL is adopted into local state only on a navigation we did not make (location.key changed and location.search differs from the string we last wrote). No pending list — cost: none; simpler to maintain.
- Task 17: parked — typing in the old page's filter before a route transition commits can REPLACE the pending entry with old-path?q= (the switch is lost; URL matches the screen) — Ruling: not a regression (same with useSearchParams); needs scripted input to hit; a fix would lean on UNSAFE_ router APIs; add a one-line comment at the mirror in the final wave — cost if wrong: a user occasionally re-clicks a switch.
- Task 18: Ruling: embed layout — internal/webui/dist/ is fully git-ignored except a tracked .gitkeep; the placeholder moves to tracked internal/webui/placeholder/index.html (embedded); FS() serves dist when dist/index.html exists, else the placeholder; make web overwrites dist freely — cost: none.
- Task 18: Ruling: the seam test imports public symbols from the package index only (test helpers stay relative); export THEME_PAINT_KEY from the index (downstream index.html needs it to avoid a flash) — cost: one more public export.
- - Ruling: F1 apply the T15 edition ruling (tag only when edition && edition !== brand.productName).
- - Ruling: F2 resolve defaultThemeId once (same as ThemeProvider) for the context; document extension edges (relative paths, reserved core segments `findings`/`r/...`); useInstallId returns "" outside install routes, documented.
- - Ruling: F3 export-surface snapshot test (sorted runtime + type exports); drop `rendererFor` from the public index (document longest-prefix on findingRenderers); keep CopyNameButton and resourcePath.
- - Ruling: F4 findings page shows data age ("Last checked {ago}" / stale per the wording rules) using isStale/ago.
- - Ruling: F5 restricted findings read per design §7.1, worded by the wording pass ("Restricted: ask the owning team" unless decisions.md says otherwise).
- - Ruling: F6 add the one-line known-limit comment at the urlParams mirror; design §5.3/§6.5 amendments are done by the controller at close.
- - Ruling: also fix in this wave — header shimmer on /installs error; theme tooltip lingering after a mouse pick; one shared scopeText helper for card and tile (honouring source and error); after a first load, errors keep the loaded data with a stale/error notice on every page (InstallPage behaviour); a11y: hidden state text for CheckedList and switcher items, an h1 on InstallPage, per-route document.title, tabIndex=0 on the findings scroller, role="status" on the strip headline only; Gateway rows fold current health findings for their ref into the shown state; InstallPage accepts stylesOverride; peer ranges ^19/^7; sourcemap "hidden" and *.map not embedded; placeholder index.html dark background; healthy install with info findings shows an "N info findings" link; apply every decision in scratchpad/wording/decisions.md.
- Final review (1.2): parked — strip says "Data is stale" during a failing poll even when data is seconds old; the notice above names the failure — Ruling: optional "Last check failed" wording later — cost: slightly imprecise label.

## Deferred minor findings (for the phase review)
- Task 12: minor (deferred): matchMedia outside try/catch; lucide-react ^1 instead of brief's *
- Task 13: minor (deferred): non-#RRGGBB downstream bg paints dark before React; stored id missing from a later themes prop falls back to themes[0]
- Task 14: minor (deferred): aria-label on a standalone span is not honoured by every screen reader (fine inside links)
- Task 15: minor (deferred, triage first): tooltip lingers after a mouse theme pick; header shimmer pulses forever when /installs errors (use !error); context defaultThemeId disagrees with ThemeProvider's dark fallback; extension contract edges undocumented (leading slashes, reserved core paths, useInstallId outside install routes)
- Task 15: minor (deferred): extension themes/top-scope routes and menu keyboard paths untested; Copy error button gives no feedback; 860 px literal breakpoint
- Task 16: minor (deferred): no automated guard for the TanStack render loop (add an idle-CPU/render-count check to the Playwright e2e in 1.3); row click navigates on text selection/modifier keys; duplicated copy button/severity labels/plural helper; Gateway links skip isLinkable
- Task 16: minor (deferred): strip role="status" dropped (avoids 5 s screen-reader announcements; confirm in final review); no render-count test for memoisation
- Task 17: minor (deferred): spinners instead of skeletons on resource/findings pages (one shift per visit); kind chips keyed by kind only; errors replace loaded cards (consistent with other pages); exports rendererFor/CopyNameButton widen the D-003 surface — confirm at final review; "No problems" vs "All healthy" → wording pass
