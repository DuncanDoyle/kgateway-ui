# Phase 3.5 — Stabilization (Complete)

**Completed:** 2026-11-07
**Range:** 662ecc5..HEAD (branch `duncandoyle/stab35`, not pushed)

## Summary
3.5 closes the accepted Phase 3 review findings and three revisions of D-058. The user started it while away ("Do all of those things in the background").

| Item | What changed | Commit |
|---|---|---|
| S1 (P2) | With a rule set that maps Envoy names to objects, KGWUI-001's proxy-stat names are filtered as those objects, as log names are | [`87b38f3`](https://github.com/DuncanDoyle/kgateway-ui/commit/87b38f3) |
| S2 (P2) | A served-kinds change limited to list-only kinds is a note on the OK check, not KGWUI-008 | [`7be3980`](https://github.com/DuncanDoyle/kgateway-ui/commit/7be3980) |
| S3 (P2) | A KGWUI-024 error reaches `/processes`, `diagnostics.dataPlane.withheld`, the overview's Data plane cell ("Config withheld") and the Gateway page's headline | [`ed4625b`](https://github.com/DuncanDoyle/kgateway-ui/commit/ed4625b), [`97e7e90`](https://github.com/DuncanDoyle/kgateway-ui/commit/97e7e90) |
| S4 (P3) | The resource status check declares Services | [`c7fb8a1`](https://github.com/DuncanDoyle/kgateway-ui/commit/c7fb8a1) |
| S5 (P4) | KGWUI-023 route attribution is linear: 251 → 44 ms at 10,000 routes on one Service port | [`92c928e`](https://github.com/DuncanDoyle/kgateway-ui/commit/92c928e) |
| S6 (P3) | No code: §14 rule 11 amended (D-059) | — |
| S7 (P3) | On 2.5, a selector that matches nothing is a computed KGWUI-016 warning; exit: kgateway#14998 | [`7f6336d`](https://github.com/DuncanDoyle/kgateway-ui/commit/7f6336d) |
| S8 (P3) | The status-sync count is gone from KGWUI-018, the SPA, the docs and `pkg/ctrlmetrics` | [`5a2253e`](https://github.com/DuncanDoyle/kgateway-ui/commit/5a2253e) |
| S9 (P3) | An unreadable controller version selects the 2.5 rules | [`c3034b8`](https://github.com/DuncanDoyle/kgateway-ui/commit/c3034b8) |

## Sub-phases (phase record only)
n/a

## Reviews
- **Task reviews:** none per item; 3.5 ran in one session without subagents. The cross-model review below covers the whole diff.
- **Cross-model, two Codex rounds** (Codex reviewed Claude's work):
  1. `reviews/3.5-codex.md` (`662ecc5..0a7c00c`): 1 P1 (ranked P2 at triage), 1 P2, 1 P3; all accepted and fixed in [`97e7e90`](https://github.com/DuncanDoyle/kgateway-ui/commit/97e7e90).
  2. `reviews/3.5-codex-rereview.md` (`0a7c00c..97e7e90`): all three resolved; no new findings.

## Wording pass
Done: `phase-3.5-wording.md`. 3 strings (S597, S598, S182). The picker was the author, not a separate agent; the file says so.

## Decisions
- D-058 items 7, 10 and 11 (user, 2026-11-07) define S7–S9; D-059 closes S6.
- D-060: the 3.5 as-built choices, proposed for the user's confirmation.

## Waivers
None.

## Deliverables
- Each item's test failed before its change and passed after: S1, S3 and S9 at the API; S2, S7 and S8 through `health.Evaluate` (S2 through `kinds.Diff`, S8 through `ctrlmetrics.Parse`); S4 in the declared-input guard. S5 is a benchmark plus a guard test.
- Standard gates on [`97e7e90`](https://github.com/DuncanDoyle/kgateway-ui/commit/97e7e90): `go vet ./...` with the default, `e2e` and `bench` tags; `go test -race ./...` (35 packages ok); `TZ=Europe/Amsterdam go test ./pkg/api`; `make lint` (0 issues) and `make actionlint`; web vitest (751 tests), typecheck, lint and build; `helm lint` and `REQUIRE_HELM=1 go test ./deploy/...`; the forbidden-terms check over the added lines.
- Not run for 3.5: kind e2e and `make bench`. Both run in CI on the 3.5 PR.

## Deferred / open
- D-060 awaits the user's confirmation.
- kgateway#14998 (upstream): when the controller reports a selector that matches nothing, S7's computed rule can go.
- The plan's kind e2e for a main build now expects 016 from status (S9); it is unverified, since `kind-main` is skipped in CI.
- Push, PR and merge of 3.5, and the Phase 3 close, need the user's go.
