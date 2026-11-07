# Phase 4 — Snellius rebrand: Design

**Status:** Draft for cross-model review. The user pre-approved the phase while away (2026-11-07); calls marked (our call) are open to change on their return.
**Date:** 2026-11-07
**Base:** `966be06`
**Roadmap:** `docs/roadmap.md`, Phase 4
**Decisions:** D-061 (the rename and its scope). This design proposes D-062 (§9).
**Inputs:** the branding package `snellius-branding/` (untracked, owner-supplied) and its brief `docs/snellius-rebranding-claude-prompt-v2.md` (untracked). This design overrides the brief in one point: artifacts are renamed too (D-061, user 2026-11-07).

## Decisions for this phase

| # | Decision | Marker | Where |
|---|---|---|---|
| 1 | The product is **Snellius**; tagline "See the whole picture."; descriptor "Kubernetes Gateway Management & Observability". kgateway stays the name of the observed implementation | (user, 2026-11-07) | §2 |
| 2 | Artifacts are renamed: GitHub repo, Go module, binary, image, Helm chart, npm packages | (user, 2026-11-07) | §4 |
| 3 | Finding codes keep `KGWUI-`, with their `#kgwui-0nn` anchors | (user, 2026-11-07) | §4.6 |
| 4 | A clean break, not a dual-name period: v0.5.0 ships only the new names, with a migration note. The UI is stateless and pre-1.0, so an uninstall and a reinstall is cheap | (our call) | §5 |
| 5 | Saved theme choices survive: the new storage keys read the old ones once | (our call) | §4.4 |
| 6 | Historical records (closed phase docs, reviews, bench records, decisions) keep the old name; living docs change | (our call) | §6 |

## 1. Intent and why

`kgateway-ui` describes a component, not a product. The user wants an identity in the style of CNCF tools, ready for a Gateway API core that is not tied to one implementation (Phase 5). Today the header even says "kgateway" with the kgateway icon, so the UI presents itself as the gateway it observes.

Goals:
1. Every surface a user sees says Snellius: header, browser title, favicon, README, install commands, chart notes.
2. Every artifact a user installs or imports is named Snellius.
3. Nothing about kgateway the gateway is renamed.

Non-goals: new themes or a palette redesign; renaming finding codes; claiming multi-implementation support; trademark clearance (SURF runs a supercomputer named Snellius; the README says the name is not cleared).

Anti-goals: a mechanical `kgateway-ui` → `snellius` replace that also rewrites references to kgateway, Gateway API kinds, or history.

## 2. Naming rules

| Context | Form |
|---|---|
| Product in prose, titles | Snellius |
| Wordmark in the header, `brand.productName` | snellius (lowercase, as in the logo) |
| Identifiers (module, binary, image, chart, npm scope, namespace) | `snellius` |
| The observed gateway | kgateway, unchanged |
| Env vars | `SNELLIUS_` prefix |

Sentence pattern for docs: "Snellius is a read-only UI for Kubernetes Gateway API, built for kgateway."

## 3. Visual identity

- **Header:** `brand.logo` is `snellius-symbol-dark.svg` (the header band is dark in both themes), 24 px, with the wordmark text from `brand.productName`. No full logo next to the text.
- **Assets:** copy the chosen SVGs and icons into `web/app/src/assets/snellius/`, imported as modules so the binary stays self-contained. The favicon set, apple touch icon (180) and manifest icons go to `web/app/public/`.
- **Manifest:** ship `site.webmanifest` for the icons only, without claiming offline support.
- **Palette:** the brand colours (navy, blue, cyan, teal, gold) are not health colours. No health token changes. The header stays the existing ink band. A brand accent token is out of scope (non-goal).
- **Fonts:** keep Figtree and JetBrains Mono and the `renderWhenFontsReady` contract. The wordmark renders in Figtree.
- **kgateway artwork:** `kgateway-icon-white.svg` and its attribution stay, used only where a kgateway mark is factual. Nothing uses it after this phase; it stays vendored for that use (our call).
- **Check:** the symbol must be recognisable at 24 px in both themes and at 16 px as a favicon. That is verified by screenshots (§8).

## 4. Artifact renames

### 4.1 Go
- Module `github.com/DuncanDoyle/kgateway-ui` becomes `github.com/DuncanDoyle/snellius` (all imports, `examples/downstream`, ldflags in `Makefile` and `Dockerfile`).
- `cmd/kgateway-ui` becomes `cmd/snellius`; the binary is `snellius`.
- `KGWUI_MEMORY_LIMIT` becomes `SNELLIUS_MEMORY_LIMIT`, with unchanged semantics (decimal bytes, 90 % target, `GOMEMLIMIT` wins). The old name is still read when the new one is unset, with a deprecation log line, for one minor release (our call: someone may set it by hand outside the chart). `KGWUI_BENCH_TIMING` (internal) becomes `SNELLIUS_BENCH_TIMING`.
- The listen log line says "snellius listening".
- `KGWUI-` codes and `UpstreamPrefix` are unchanged.

### 4.2 Image and release
- Image `ghcr.io/duncandoyle/snellius`; OCI source label points at the new repo.
- Chart `oci://ghcr.io/duncandoyle/charts/snellius`.
- npm release asset `snellius-shell-<v>.tgz`.

### 4.3 Helm chart
- `deploy/helm/snellius`, chart name `snellius`, template helpers `snellius.*`, `app.kubernetes.io/name: snellius`, container name `snellius`, NOTES and chart README.
- The fullname rule stays `<release>-snellius` (the helper has no collapse rule; adding one is out of scope). Docs keep release name `ui` in namespace `snellius`, giving `ui-snellius`.

### 4.4 Web
- npm scope `@snellius/shell`; workspaces `snellius-web`, `snellius-app`; source condition `@snellius/source`.
- `brand.tsx` exports `snelliusBrand`; `<title>snellius</title>` in `index.html`.
- The published shell's `defaultBrand` (used when a consumer gives no brand) is `snellius`, without a logo, as today. The embedded placeholder page shown before `make web` says Snellius.
- Storage keys `snellius.theme` and `snellius.theme.paint`. On first read, if a new key is missing and the old key exists, copy it and remove the old one. The pre-paint script in `index.html` does the same for the paint key, so the first paint is right.
- Console warning prefix `snellius:`.
- `REPO_BLOB` points at the new repo.

### 4.5 e2e and CI
- Image tag `snellius:e2e`, chart path, namespace `snellius`, `deploy/ui-snellius`.
- Kind cluster names `kgwui-e2e` and `kgwui-dev` stay: they are local guards outside the product (our call).

### 4.6 Unchanged on purpose
`KGWUI-` codes and anchors; `/api/v1` and `/api/ext` routes (no product name); Gateway API and kgateway identifiers; test data provenance; closed historical docs.

## 5. Migration

- **Install:** `helm uninstall` the old release, then install the new chart. `spec.selector` is immutable and every object name changes, so an in-place upgrade cannot work. The UI holds no state.
- **Image or chart pins:** move to the new paths; the old paths get no further releases. `v0.4.0` stays available under the old names.
- **Go importers (kgwctl):** switch imports to `github.com/DuncanDoyle/snellius` at the first tag on the new path. Old tags still resolve under the old path through the module proxy.
- **npm consumers:** `@kgateway-ui/shell` becomes `@snellius/shell`.
- **Env var:** `KGWUI_MEMORY_LIMIT` still works for one minor release.
- **Ordering:** the GitHub repo rename must happen before the Phase 4 PR merges, so the new module path and links resolve; the user does the rename.

## 6. Docs

- README: Snellius heading with the horizontal logo (light/dark via `<picture>`), tagline, the kgateway relationship, install commands, the naming note about SURF.
- Living docs: `docs/roadmap.md` (product name), `docs/findings.md` and the chart README.
- Closed records keep the old name. A note in D-061 says so.
- `docs/decisions.md`: D-062 supersedes D-008's "default theme carries the kgateway identity" (§9). D-008's mechanism stays.
- The upgrade notes section of the README gets the migration (§5).

## 7. Testing

- Unit: the default brand renders the Snellius symbol and `snellius`; a downstream brand still overrides it; `document.title` follows the brand; the storage key migration (old key only, new key only, both); the env var fallback.
- Static: `index.html` references existing favicon and manifest files (a build-time test).
- Helm: `chart_test.go` on the new names; `helm lint`.
- Go: build and full tests on the new module path; `examples/downstream` builds.
- Kind e2e on the three kgateway versions with the new chart; Playwright unchanged except names.
- A forbidden-terms check and a check that no `kgateway-ui` remains outside the unchanged list (§4.6) and history.

## 8. Visual verification

`/ui-loop` screenshots of the header, the browser tab and the landing page in both themes at 1920 × 1080, plus the favicon at 16 px, compared against `reference/approved-dark-whatsapp-logo.png`.

## 9. Proposed decision

**D-062 — Snellius identity supersedes the kgateway default identity.** The default theme and brand carry the Snellius identity. D-008's mechanism is unchanged: a downstream build supplies its own brand and tokens. The kgateway icon stays vendored with its attribution for factual use.

## 10. Sub-phases

One sub-phase, 4.1, planned. It is a mechanical change across many files, reviewed as one diff.

## 11. Open items

None. The repo rename and the kgwctl notice are outward actions queued for the user.

## 12. Sub-phase 4.2

**Row reasons (D-063).** Rows of the Routes, Gateways and ListenerSets lists and the overview's Gateway rows gain an optional `reason` (API v1, additive), absent on a healthy row. `{kind: "finding", code, message, more}` names the deciding finding and counts the object's other findings, which are all at its severity or lower; `{kind: "unknown"}` means a required check is not OK, and `{kind: "gateway-rejecting"}` means the row is warning only because its Gateway rejects config. The reason carries code and message only, never Detail or evidence (design §7.4 rule 7), and follows the row's state, which is not filtered per caller (rule 9). The UI shows it as a tooltip on the status, on hover and keyboard focus.
