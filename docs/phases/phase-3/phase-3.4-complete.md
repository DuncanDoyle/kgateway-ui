# Phase 3.4 — Backends and kgateway 2.5 (Complete)

**Completed:** 2026-11-06
**Range:** 8342a38..HEAD (branch `duncandoyle/backends`)

## Summary
3.4 finds backends that serve nothing and adds kgateway 2.5.
- **KGWUI-023:** a route's core Service backend with no ready endpoint on the named port for 30 s. Only refs the controller resolved count. The finding sums ready endpoints over every EndpointSlice of the Service, per port.
- **Services and EndpointSlices:** two cluster-wide watches behind `diagnostics.endpoints` (on), cut down to about 250 B per object (D-057). Off, denied or failing gives 023 "not available".
- **KGWUI-013:** explains a missing Service or a missing port, and suggests the port when the Service has exactly one. KGWUI-014 keeps the grant explanation.
- **kgateway 2.5:** its own rule set (`kgw25`). 016 comes from the controller's `TargetNotFound`, and the computed 016 is off there. The controller version comes from `kgateway_build_info`.
- **KGWUI-024:** proxies withheld from config updates (error), or config released before every cluster was ready (warning). On 2.3 and 2.4 it reads "Not available on this version".
- **Platform neutrality:** a provider that observes no Pods reports the process checks as "Not observed".
- **kgwctl requests (D-056):**
  - #17: DirectResponse, GatewayExtension and ReferenceGrant are listed and shown, never judged.
  - #20: `cache.Wait` returns once every goroutine `Start` launched has exited.

## Sub-phases (phase record only)
n/a

## Reviews
- **Task reviews:** Tasks 75–86, 78a, 78b and the 81b follow-up each had a spec and quality review, with fix rounds confirmed by scoped re-reviews (task files in the SDD workspace).
- **Cross-model, two Codex rounds** (Codex reviewed Claude's work):
  1. `reviews/3.4-codex.md` (`8342a38..8fa399b`): 3 P1, all reproduced and fixed in [`83ef10d`](https://github.com/DuncanDoyle/kgateway-ui/commit/83ef10d), [`72f7d5f`](https://github.com/DuncanDoyle/kgateway-ui/commit/72f7d5f) and [`5e88ef9`](https://github.com/DuncanDoyle/kgateway-ui/commit/5e88ef9).
  2. `reviews/3.4-codex-rereview.md` (`8fa399b..5e88ef9`): all three resolved; no new findings.
- **History rewrite:** a test fixture in [`0f104a6`](https://github.com/DuncanDoyle/kgateway-ui/commit/0f104a6) used a forbidden word, later renamed. The unpushed branch was rewritten with a tree filter on that one file; the final tree is unchanged.

## Wording pass
Done: `phase-3.4-wording.md`. 30 strings (S568–S596 and S362), five options each, chosen by a separate picker; 2 changed (S570, S573). S595 and S596 are check names and wire values, kept.

## Decisions
- D-056: kgwctl #17 and #20 in 3.4; #18 and #19 to Phase 4 planning (user, 2026-11-06).
- D-057: Service and EndpointSlice facts shrunk, then the retained-heap gate raised to 48 MiB (user, 2026-11-06).
- D-058: the as-built deviations, eleven items. Proposed; the user confirmed item 6 and asked to revise item 2, which the REFERENCED-mode capture did. The rest await the user.
- The user confirmed the brief's eight inferred outputs (2026-11-06), and the export of `computed.TargetStatusReporter`.

## Waivers
None.

## Deliverables
- Acceptance tests failed before and passed after, per the task reports and `close-34-fix-report.md`.
- Standard gates on [`8fa399b`](https://github.com/DuncanDoyle/kgateway-ui/commit/8fa399b):
  - `go vet ./...` with the default, `e2e` and `bench` tags;
  - `go test -race ./...` (35 packages ok);
  - `TZ=Europe/Amsterdam go test ./pkg/api`;
  - `make lint` (0 issues) and `make actionlint`;
  - web vitest (750 tests), typecheck, lint and build;
  - `helm lint deploy/helm/kgateway-ui` and `REQUIRE_HELM=1 go test ./deploy/...`;
  - the forbidden-terms check over the added lines.
- The Codex fixes ([`83ef10d`](https://github.com/DuncanDoyle/kgateway-ui/commit/83ef10d) … [`5e88ef9`](https://github.com/DuncanDoyle/kgateway-ui/commit/5e88ef9)) passed vet, race tests and lint.
- `make bench` on [`31e97db`](https://github.com/DuncanDoyle/kgateway-ui/commit/31e97db) (the final code), reference mode, under `caffeinate -s` with no sleep in the run: all gates pass. Record: `bench/3.4.txt`. An earlier run was void: the Mac slept with the lid closed (`caffeinate -i` does not stop that).
- kind e2e on [`3d7698e`](https://github.com/DuncanDoyle/kgateway-ui/commit/3d7698e) (the final code, Codex fixes included): v2.5.0 / v1.7.0, v2.4.7 / v1.6.1 and v2.3.11 / v1.5.1 all pass, Go ok and Playwright 64 passed each, no reruns.
- A real 2.5.0 `flip_release` capture under `REFERENCED` xDS scoping ([`acd3e7a`](https://github.com/DuncanDoyle/kgateway-ui/commit/acd3e7a)); the 024 warning is tested on it.
- `/ui-loop` on the 023 finding page, on kind v2.4.7, in both themes.

### Benchmarks (Large tier)
Record: `bench/3.4.txt`, one Apple M5 Max, 18 CPUs (the D-044 reference machine). The measurement world adds 5,000 Services and 6,000 EndpointSlices (5 % empty) and the three list-only kinds (R57).

| Measure | 3.3 | 3.4 | Budget |
|---|---|---|---|
| Warm recompute median, consistent / contradicting / worst | 35.5 / 46.7 / 51.3 ms | 37.7 / 44.6 / 49.2 ms | ≤ 150 ms |
| Cold recompute median, consistent / contradicting / worst | 52 / 56 / 62 ms | 49 / 53 / 65 ms | ≤ 1 s |
| Retained heap over 2.1, consistent / contradicting / measurement world | +23.4 / +27.4 / +34.6 MiB | +23.3 / +27.3 / +39.3 MiB | ≤ 48 MiB (D-057); ≤ +24 MiB over 3.1 (+14.9) |
| 3.2 watches' retained state | 1,450,520 B | 1,396,112 B | ≤ 1,600,000 B |
| 3.4 watches' retained state (Services, EndpointSlices, list-only kinds) | — | 3,071,736 B | reported |
| Dense: retained / cold / per warm | 160 / 207 / 166 MiB | 160 / 208 / 166 MiB | 176 / 246 / 185 MiB |
| Analyzer stage, §16.3 scope, measurement world: warm / memoized / new scrape | 6.00 / 0.20 / 1.28 ms | 5.51 / 0.19 / 1.17 ms | ≤ 20 / 2 / 2 ms |
| Analyzer allocations, Large warm §16.3 scope | 185,112 B | 147,016 B | ≤ 192,000 B |
| Stream CPU, annotation-only, worst run | 9.7 % | 8.6 % | ≤ 15 % |
| Spec-edit stream CPU, worst | 13.9 % | 13.3 % | ≤ 25 % |
| Edit-driven recomputes, worst | 0.98/s | 0.98/s | ≤ 1/s (D-047) |
| OS memory during the stream, median of 5, worst | 209.5 MiB | 250.4 MiB | ≤ 288 MiB (D-043) |
| `…/findings` p95 sequential / 20 clients, worst mix | 0.52 / 1.31 ms | 0.71 / 5.92 ms | < 50 / < 100 ms |
| `…/diagnostics` p95 sequential / 20 clients, measurement world | 0.85 / 5.01 ms | 0.84 / 3.84 ms | < 50 / < 100 ms |

## Deferred / open
- D-058 items 1–5 and 7–11 await the user's confirmation.
- 023 has no proxy membership evidence; it needs a verified, port-aware reverse mapping (xdsview `origin`, Phase 9).
- The 024 error (`warm_withheld`) is tested on a synthesised fixture only; on kind the gauge never outlasts a scrape.
- DirectResponse status is not judged (Phase 5).
- R59 (proxy Pod ownership by UID needs a ReplicaSet watch): to the phase review.
- A controller whose version cannot be read selects the 2.4 rule set.
- 2.5 controller log captures; 2.5's TLSRoute SNI conflict handling in view A.
- Cosmetic: the "EndpointSlices" evidence label touches its value.
- The kind e2e runs 3.4 at its default `ALL` xDS scoping; `REFERENCED` scoping runs only in the capture.
- Program: the contract table and edges for `v0.4.0`, a release tag, a kgwctl notice, and comments on #18 and #19, all with the user's go.
