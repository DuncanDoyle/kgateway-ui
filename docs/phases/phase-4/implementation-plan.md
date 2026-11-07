# Phase 4 — Snellius rebrand: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename the product kgateway-ui to Snellius, in its visual identity and its artifacts, without renaming anything about kgateway the gateway.

**Architecture:** A mechanical rename in six tasks, ordered so each leaves the build green: Go module first, then build and chart, web packages, visual identity, docs, and a final verification. No behaviour changes except the storage key and env var fallbacks.

**Tech Stack:** Go, React/TypeScript (npm workspaces, Vite, Vitest, Playwright), Helm, GitHub Actions, kind.

**Spec:** `docs/phases/phase-4/design.md` (D-061, D-062).

## Global Constraints

- New names: module `github.com/DuncanDoyle/snellius`; binary and `cmd/snellius`; image `ghcr.io/duncandoyle/snellius`; chart `snellius` at `deploy/helm/snellius`; npm `@snellius/shell`, workspaces `snellius-web` and `snellius-app`, source condition `@snellius/source`; env prefix `SNELLIUS_`; storage keys `snellius.theme` and `snellius.theme.paint`; `brand.productName` `snellius`.
- Unchanged: `KGWUI-` finding codes, `UpstreamPrefix`, `#kgwui-0nn` anchors, `/api/v1` and `/api/ext` routes, every Gateway API and kgateway identifier, kind cluster names `kgwui-e2e` and `kgwui-dev`, test-data provenance lines, the old storage keys and `KGWUI_MEMORY_LIMIT` where they are read as fallbacks, the migration notes that name the old artifacts, and closed historical docs (phase 1–3 records, reviews, bench `.txt`, existing decisions).
- Never a blind global replace: each occurrence of `kgateway-ui`, `kgwui` or `kgateway UI` is classified as product (rename), code or anchor (keep), or history (keep).
- Public repo: the forbidden-terms list of the public-repo hygiene check never appears.
- Comments 1–2 lines; tests assert observable behaviour.
- The GitHub repo is renamed by the user before this phase merges; links to the new repo may 404 until then.

## Review Focus

1. A user with a saved theme under `kgateway-ui.theme.paint` opens the new build: the first paint uses their theme, with no flash.
2. A downstream `ShellExtension` with its own brand still shows its own name and logo, and `document.title` follows it.
3. `KGWUI_MEMORY_LIMIT` set by hand still caps memory when `SNELLIUS_MEMORY_LIMIT` is unset.
4. A leftover `kgateway-ui` in a shipped path (image tag, chart path, Makefile target) breaks `make` or CI: the leftover check in Task 6 catches it.
5. Content about kgateway the gateway (install header, version notes, finding text) still says kgateway.

---

### Task 1: Go module, binary and env var

**Files:** `go.mod`; every `.go` import (about 1,100 in 279 files, including `examples/downstream`); `cmd/kgateway-ui/` → `cmd/snellius/`; `Makefile`; `Dockerfile` (ldflags path only here; names in Task 2); `pkg/version/version.go`; `pkg/app/memlimit.go`; `pkg/app/app.go:453`; `internal/testutil/benchgate/benchgate.go`; `.github/workflows/bench.yaml`.

**Interfaces:** Produces the module path `github.com/DuncanDoyle/snellius` for every later task.

- [ ] **Step 1: Failing test** `TestMemoryLimitEnvFallback` in `pkg/app`, keeping today's semantics (decimal bytes, 90 % target, `GOMEMLIMIT` wins): with only `KGWUI_MEMORY_LIMIT=67108864` set, the target is 90 % of it and a deprecation line is logged; with both set, `SNELLIUS_MEMORY_LIMIT` wins; with `GOMEMLIMIT` set, neither applies; with neither, the Go default stays.
- [ ] **Step 2:** Run it; it fails (no `SNELLIUS_` name).
- [ ] **Step 3:** Rewrite the module path (`go mod edit -module`, then imports with `gofmt -r` or a scripted replace limited to the import path), move `cmd/kgateway-ui` to `cmd/snellius`, rename the env vars with the fallback, the ldflags path, and the log line.
- [ ] **Step 4:** `go build ./... && go vet ./... && go test ./...` pass, and the bench and e2e build tags vet.
- [ ] **Step 5:** Commit.

### Task 2: Image, chart, CI and e2e harness

**Files:** `Dockerfile`; `deploy/helm/kgateway-ui/` → `deploy/helm/snellius/` (Chart.yaml, values.yaml, `_helpers.tpl`, templates, NOTES.txt, README.md); `deploy/helm/chart_test.go`; `.github/workflows/{ci,e2e,release}.yaml`; `test/e2e/setup.sh`; `test/e2e/api_test.go` (names only); `examples/downstream/main_test.go:259` (chart path and namespace) and `examples/downstream/chart/values-acme.yaml:2`; `Makefile` targets.

**Interfaces:** Consumes Task 1's binary name. Produces chart `snellius`, objects named `<release>-snellius`, namespace `snellius` in the harness.

- [ ] **Step 1:** Update `chart_test.go` expectations to the new names (`./snellius`, `ui-snellius`, `ui-snellius-logs`, `app.kubernetes.io/name: snellius`); run `REQUIRE_HELM=1 go test ./deploy/...`; it fails.
- [ ] **Step 2:** Rename the chart and helpers, the image repo and OCI source label, the Dockerfile output and entrypoint, the workflows' image, chart and npm paths, and the e2e harness (image `snellius:e2e`, namespace `snellius`, `deploy/ui-snellius`). Kind cluster names stay.
- [ ] **Step 3:** `helm lint deploy/helm/snellius` and `REQUIRE_HELM=1 go test ./deploy/...` pass; `docker build .` succeeds; `make actionlint` passes.
- [ ] **Step 4:** Commit.

### Task 3: Web packages

**Files:** `web/package.json`, `web/app/package.json`, `web/packages/shell/package.json`, `package-lock.json` (regenerated by `npm install`), `tsconfig.base.json`, `vite.config.ts`, every `@kgateway-ui/` import, `Makefile` web targets, `examples/downstream/web/*`.

**Interfaces:** Produces `@snellius/shell` and condition `@snellius/source`.

- [ ] **Step 1:** Rename the scope, workspaces and condition; regenerate the lockfile with `npm install` (no new dependencies).
- [ ] **Step 2:** `cd web && npm ci && npm run typecheck && npm run lint && npx vitest run && npm run build` pass; `make web` passes; the downstream example builds.
- [ ] **Step 3:** Commit.

### Task 4: Visual identity

**Files:** `web/app/src/assets/snellius/` (new: symbol dark/light and mono SVGs); `web/app/public/` (favicon.svg, favicon.ico, favicon PNGs, apple touch 180, app icons 192/512, `site.webmanifest`); `web/app/src/brand.tsx`; `web/app/src/main.tsx`; `web/app/index.html`; `web/packages/shell/src/theme/themeStorage.ts`; `web/packages/shell/src/shell/Shell.tsx` and `contributions.tsx` (console prefix); `web/packages/shell/src/diagnostics/EvidenceView.tsx` (`REPO_BLOB`) and `diagnostics/diagnostics.test.tsx:612`; `web/packages/shell/src/shell/extension.ts:86` (`defaultBrand.productName`); `internal/webui/placeholder/index.html` (title and text); tests beside each.

**Interfaces:** Consumes `@snellius/shell`. `Brand` and `ShellExtension` are unchanged.

- [ ] **Step 1: Failing tests:**
  - the default app header shows an `img` from the Snellius symbol and the text `snellius`; `document.title` is `snellius` on the landing page;
  - a downstream brand (`productName: "acme"`) still shows `acme` and its logo, and the title follows it;
  - the published shell with no `extension.brand` shows `snellius` (the `defaultBrand`), not kgateway;
  - the embedded placeholder page (`internal/webui`) is titled Snellius;
  - `themeStorage`: only the old key set → the theme is read and moved to the new key, the old key removed; only the new key → read; both → new wins;
  - a static test reads `index.html`: every `href` for icons and the manifest names a file that exists in `web/app/public/`;
  - the pre-paint harness (`theme/indexHtml.test.ts`, which runs the inline script) is extended with a full storage stub: old key only, new key only, both (new wins), and a store whose writes throw. In each case the root background and `color-scheme` before React match the saved theme.
- [ ] **Step 2:** Run them; they fail.
- [ ] **Step 3:** Copy the assets from `snellius-branding/assets/` (the refined set; SVGs optimised, transparent), switch the brand to `snelliusBrand`, update `index.html` (title, icon links, apple touch icon, manifest) keeping the pre-paint script's behaviour, add the storage migration, rename the console prefix and `REPO_BLOB`. `kgateway-icon-white.svg` and its attribution README stay.
- [ ] **Step 4:** Tests, typecheck, lint, build pass.
- [ ] **Step 5:** `/ui-loop` screenshots: header, tab and landing in dark and light at 1920 × 1080, the favicon at 16 px; compare with `snellius-branding/reference/approved-dark-whatsapp-logo.png`. Contrast of the header wordmark checked (WCAG AA).
- [ ] **Step 6:** Commit.

### Task 5: Docs and decision

**Files:** `README.md`; `docs/assets/` (new: horizontal logo light/dark SVG, social banner PNG); `docs/roadmap.md` (product name only, plus "krp" → "xdsview" in the Runtime Filter Chain phase); `docs/findings.md` (product name only); `docs/documentation-notes.md` (product name only); `docs/decisions.md` (D-062).

- [ ] **Step 1:** README: Snellius logo via `<picture>` (light/dark), tagline, the kgateway relationship sentence (design §2), install commands with the new image and chart, an "Upgrading from kgateway-ui" section with the migration (design §5), and a naming note that the name is not trademark-cleared and SURF's supercomputer uses it.
- [ ] **Step 2:** D-062 per design §9. Living docs updated; closed records untouched.
- [ ] **Step 3:** Forbidden-terms check over the added lines; commit.

### Task 6: Verification

- [ ] **Step 1:** Leftover check: `git grep -n -i -E 'kgateway-ui|kgwui|kgateway ui'` outside the unchanged list (Global Constraints) returns nothing; each remaining hit is classified in the task report.
- [ ] **Step 2:** Full gates: `go vet` (default, `e2e`, `bench` tags), `go test -race ./...`, `TZ=Europe/Amsterdam go test ./pkg/api`, `make lint`, `make actionlint`, web vitest/typecheck/lint/build, `helm lint`, `REQUIRE_HELM=1 go test ./deploy/...`, the terms check.
- [ ] **Step 3:** Kind e2e on v2.5.0/1.7.0, v2.4.7/1.6.1 and v2.3.11/1.5.1 with the new chart, on cluster `kgwui-e2e` with an explicit kubeconfig and context.
- [ ] **Step 4:** Record results in the task report; commit any fixes.
