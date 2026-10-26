# Phase 2.3 — View A: computed listener attachment (Complete)

**Completed:** 2026-10-25
**Range:** f01bb0a..HEAD (branch `duncandoyle/viewA`)

## Summary
The UI computes which routes attach to each listener and cross-checks the result with the controller's status:
- **Rule sets:** `pkg/computed/kgw23` and `kgw24` mirror kgateway v2.3.11 and v2.4.7 attachment: allowed routes (decided before the section check, rules §4.1), section and port, hostname intersection and per-listener counting. `rulesets.Select` picks one by controller version; downstream builds register their own with `extension.WithComputedRules`. The 2.4 isolation note marks counts that plain-HTTP listener isolation may not program.
- **Cross-check:** every computed count and parentRef outcome is checked against `attachedRoutes` and route status (rules §8). The result is agreement, disagreement, pending, unknown (`scope-unknown`, `listenerset-no-status`, `setting-unknown`, `namespace-labels-unavailable`) or restricted. Fingerprints cover the rules §9.2 inputs.
- **KGWUI-012:** a disagreement that lasts the 30 s window becomes a warning on the route and its parent. It never turns an install red.
- **API:** Gateway and ListenerSet details carry `computedRoutes`, effective hostnames and badges; routes take `?listener=` (with `_unknown`); `/meta` lists the filters; the Gateways routes column uses the computed own-listener count. Every per-route computed fact goes through the Authorizer by its dependency set; listener facts show to every caller who sees the listener.
- **SPA:** the `Computed` badge on listener tables, parent cards and route sections, with short compact states and a tooltip that names the rule.
- **Chart:** `extraArgs` for extra UI flags.

## Reviews
- **Task reviews:** every task had a spec and quality review. Fix rounds were confirmed by scoped re-reviews or by the controller for small diffs.
- **Cross-model (Codex):** `reviews/2.3-codex.md`, 4 findings plus 2 from the re-review. Accepted:
  - **#1 and #2 (P1):** restricted-data leaks through the isolation note and unknown memberships. Their fixes were later superseded (below).
  - **#3 (P1):** the list dropped a second written ref's check.
  - **#4 (P3):** no app-level extension test.

  Re-review R1 and R2 were accepted. Restricting a listener's count or note leaked through what moved it: a hidden route or ListenerSet entering or leaving its dependencies. Both were resolved by showing listener-level facts to every caller who sees the listener ([`4f82c36`](https://github.com/DuncanDoyle/kgateway-ui/commit/4f82c36), user, 2026-10-26). That supersedes the fixes for #1 and #2.

## Wording pass
`phase-2.3-wording.md` holds the final copy (S111–S187), picked per string from five options by a separate subagent. Rulings:
- no final period on states and notes; the tooltip joins sentences;
- no `;` inside explanation templates;
- short compact states (Agrees, Disagrees, Waiting, Unknown, Hidden, Not checked).

## Waivers
none

## Deliverables
- **Backend:**
  - `pkg/install/settings.go`;
  - `pkg/computed` (rules, attach, check, fingerprint, goldens) and `kgw23`/`kgw24` with 20 and 21 upstream-derived fixtures;
  - `rulesets`;
  - `pkg/engine` views and persistence;
  - `pkg/health` KGWUI-012;
  - `pkg/explore` listener index;
  - `pkg/api` computed DTOs, the 41-case Authorizer matrix and new goldens.
- **SPA:** `ComputedBadge` and view A in the Gateway, ListenerSet, route and Gateways pages; `copy/explorer.ts`.
- **Tests:**
  - `internal/testutil/scale` consistent world and `Tier.Contradicting`;
  - Large-tier budgets in both worlds;
  - kind e2e `TestComputedAgreesWithController` and `TestForcedDisagreementRaisesKGWUI012`;
  - `explorer.spec.ts` listener filter.
- **Docs:**
  - `computed-rules.md` §3.3, §4, §8, §9 updates;
  - design §5.2, §7.2–7.4, §8.1, §9.6;
  - D-022 (memory gates) and D-023 (restricted memberships, written-ref merge);
  - `phase-2.3-evidence.md`.
- **Gates on `33cc71e`:**
  - Go: `go vet` (plain, `bench`, `e2e`) clean, `go test ./... -race -count=1` passes in 24 packages, golangci-lint reports 0 issues.
  - Web: Vitest 494/494, typecheck, lint and build pass (last run on `4b5cc6b`; the later commits touch no web code).
  - kind e2e passed on v2.4.7 (Go 4/4, Playwright 18/18; KGWUI-012 at since + 30.8 s) and v2.3.11 (forced test skipped by design).

### Benchmarks (Large tier)
2.2 numbers are from the CI runner (`bench/2.2-ci.txt`); 2.3 numbers are from a laptop (`bench/2.3.txt`, `bench/2.3-stream-10m.txt`, darwin/arm64), so compare with care. The CI record for 2.3 comes from the PR's bench job.

| Measure | 2.2 | 2.3 consistent | 2.3 worst case | Limit |
|---|---|---|---|---|
| Warm recompute | ~55 ms | 42 ms | 49 ms | ≤ 120 ms |
| Allocation per recompute | 23.1 MiB | 29.1 MiB | 35.1 MiB | reported (D-022) |
| Retained heap over baseline | +7.2 MiB | +13.6 MiB | +15.8 MiB | ≤ 32 MiB |
| Stream CPU, 10 min | 7.2 % (laptop) | 9.2 % | 10.2 % | ≤ 15 % |
| Memory from the OS, 10 min | — | 164 MiB | 173 MiB | ≤ 256 MiB |
| List p95, 20 clients, restricted `sort=health` | 73.5 ms | 5.5 ms | — | < 100 ms |

`make bench` takes 7.4 min, and `make bench-stream` takes 20.6 min.

## ui-loop recordings
Listed in `phase-2.3-evidence.md`: the Gateway, ListenerSet and HTTPRoute pages in light and dark, at 1280 and 720 px, in agreeing, pending, unknown and disagreeing states.

## Release note
In README "Upgrade notes":
- computed counts and hostnames, labelled and cross-checked;
- the Gateways routes column counts own listeners;
- `?listener=`;
- KGWUI-012, a warning that is never red;
- `scope-unknown`;
- "Hidden" for restricted facts;
- `WithComputedRules` and the chart's `extraArgs`.

## Rulings made during execution
These are recorded in the plan workspace ledger. The ones that changed behaviour or the design:
- Unknown discovery scope makes view A unknown, `scope-unknown` (user, 2026-10-24, rules §3.3).
- Unknown reasons have a precedence, from highest to lowest:
  1. `scope-unknown`
  2. `listenerset-no-status`
  3. `setting-unknown`
  4. `namespace-labels-unavailable`
- No isolation note on an unknown count, a NoStatus ListenerSet or a dropped listener.
- The Gateway's routes count covers its own listeners only (design §7.3). `_unknown` is the reserved listener filter value.
- The wire note is the check's own note.
- Allocation per recompute is reported, not gated. The gates are CPU, retained heap and memory from the OS (user, 2026-10-25, D-022; the 256 MiB figure is our call).
- KGWUI-012 subjects are looked up once per run (CPU fix). The scale harness clears the fake clients' action log, which had inflated long-stream memory.
- Listener facts (count, check, unknown state, isolation note) show to every caller who sees the listener, hidden members included (user, 2026-10-26, D-023 amended). The list merges written-ref checks, worst first (user, 2026-10-25, D-023).

## Notes for later phases
- 2.4 copies view B and C code from `kgw23` to `kgw24` (Task 59). Listener facts are not dependency-restricted; per-route facts of views B and C are (design §7.4 rules 4 and 5).
- Memory from the OS was the closest gate before the harness fix. Watch it when views B and C land.
- `bench-stream` runs two 10-minute worlds against a 30 min timeout.

## Deferred / open
- (P4) The version regexp is duplicated between `rulesets.Select` and `kinds.controllerMinor`.
- (P4) A pre-2.3 server shifts the layout once when `/meta` is slower than the detail.
- (P4) The scale generator panics on tiers with fewer than 10 Gateways or no ListenerSets. No current tier is affected.
- (P4) CheckResult slimming stays available if 2.4 needs allocation headroom.
- (P4) The forced-disagreement e2e runs only in the v2.4.7 row.
