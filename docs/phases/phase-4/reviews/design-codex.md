Cross-model: YES (author: Claude Opus 5.5, reviewer: Codex, command: codex exec --sandbox read-only)
Scope: design
Diff: n/a (design review)
Timing: on time

1. **P1 — The memory-limit test specifies incompatible behavior.** [implementation-plan.md:38](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-4/implementation-plan.md:38) expects `KGWUI_MEMORY_LIMIT=64Mi` to produce a 64 MiB limit. Currently, [memlimit.go:25](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/app/memlimit.go:25) accepts decimal bytes and applies **90%** as Go’s target; existing tests explicitly reject `512Mi`. Implementing the proposed expectation would change memory semantics during a rename. Test fallback using decimal bytes, preserve the 90% calculation, and retain `GOMEMLIMIT` precedence.

2. **P2 — The published shell’s default product identity is missed.** Task 4 changes the app’s `brand.tsx`, but [extension.ts:86](/Users/ddoyle/Development/claude/kgateway-ui-claude/web/packages/shell/src/shell/extension.ts:86) independently defines `defaultBrand.productName = "kgateway"`. [Shell.tsx:54](/Users/ddoyle/Development/claude/kgateway-ui-claude/web/packages/shell/src/shell/Shell.tsx:54) uses it whenever consumers omit `extension.brand`. Those consumers would still display kgateway as the product. The leftover check also misses this bare `kgateway` string. Explicitly update and test the shell default; leaving it also carries implementation identity into Phase 5’s generic core.

3. **P2 — The downstream chart consumer is absent from the chart-move step.** [implementation-plan.md:46](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-4/implementation-plan.md:46) lists the chart consumers to update but omits [examples/downstream/main_test.go:259](/Users/ddoyle/Development/claude/kgateway-ui-claude/examples/downstream/main_test.go:259), which executes Helm against `../../deploy/helm/kgateway-ui`. Task 1 only promises import rewrites there. Moving the chart without updating this string breaks `TestChartOverlayRendersExtraRule`, which CI requires. Also update the command in `examples/downstream/chart/values-acme.yaml:2`.

4. **P2 — Task 4 targets a nonexistent evidence component.** [implementation-plan.md:67](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-4/implementation-plan.md:67) names `src/explore/EvidenceView.tsx`. The shipped repository URL actually lives in [diagnostics/EvidenceView.tsx:85](/Users/ddoyle/Development/claude/kgateway-ui-claude/web/packages/shell/src/diagnostics/EvidenceView.tsx:85), with matching expectations in `diagnostics/diagnostics.test.tsx:612`. Correct the task’s file list so the findings-document links and their tests are updated together.

5. **P2 — The embedded fallback page is another omitted product surface.** [internal/webui/placeholder/index.html:1](/Users/ddoyle/Development/claude/kgateway-ui-claude/internal/webui/placeholder/index.html:1) still titles itself “kgateway UI.” [embed.go:19](/Users/ddoyle/Development/claude/kgateway-ui-claude/internal/webui/embed.go:19) serves it when the SPA has not been built, including a fresh-checkout source run. Neither the visual-identity task nor the docs task includes it, despite the design requiring every user-facing surface to say Snellius. Add it explicitly to Task 4.

6. **P3 — The proposed pre-paint check asserts source text rather than migration behavior.** [implementation-plan.md:75](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-4/implementation-plan.md:75) checks that the script names both storage keys. That can pass while an old light-theme preference initially paints dark. Existing [indexHtml.test.ts:8](/Users/ddoyle/Development/claude/kgateway-ui-claude/web/packages/shell/src/theme/indexHtml.test.ts:8) already executes the inline script, but its storage stub only understands the current key and has no write/remove methods. Extend that behavioral harness with old-only, new-only and conflicting records, checking the root background and color scheme before React runs. Include migration writes throwing: persistence failure should not prevent a readable saved theme from painting.

No files edited or tests run; this was a static review of the docs-only change.

## Triage
| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | Memory-limit test changes semantics | reviewer | accept | Verified `memlimit.go:25,30`. The test uses decimal bytes and the 90 % target; design §4.1 states unchanged semantics |
| 2 | Shell `defaultBrand` still says kgateway | reviewer | accept | Verified `extension.ts:86`. Added to Task 4 and design §4.4, with a test |
| 3 | Downstream example runs Helm on the old chart path | reviewer | accept | Verified `main_test.go:259`, `values-acme.yaml:2`. Added to Task 2 |
| 4 | Wrong `EvidenceView.tsx` path | reviewer | accept | Corrected to `diagnostics/` with its test |
| 5 | Embedded placeholder page says "kgateway UI" | reviewer | accept | Verified `internal/webui/placeholder/index.html:1`. Added to Task 4 and design §4.4 |
| 6 | Pre-paint test asserts source text | reviewer | accept | Task 4 now extends the behavioural harness, including throwing writes |
