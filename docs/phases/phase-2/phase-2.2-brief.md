# Sub-phase 2.2 — Explorer on observed status

Kind: planned
Base: 77fc4dce94f54ed3662c0943ff93e78166243f38
Issues: n/a | Plan steps: implementation-plan.md Tasks 11–34; design §4.2–§4.4, §6, §7, §8; D-020, D-021

## Problem / goal
Phase 1 shows an install's health, but no page lists or explains its Gateways, ListenerSets and routes. A route reached only through HTTPRoute parents (a delegated child) is not owned, counted or health-checked (`pkg/install/assign.go`). Sub-phase 2.2 adds every explorer page and list, built from observed status only, and makes delegated children count (Q12). No computed views yet (2.3, 2.4).

## Expected behaviour

| Input / action | Expected |
|---|---|
| A cluster serving kgateway `Backend` | The kind is registered (category `backend`) and read; `meta` lists it after Deployment; the overview has a Backends tile, present at zero, that counts only referenced ("in use") Backends (Tasks 11, 15) |
| A delegated child with an eligible status entry from the install's controller | Owned once, with every reported parent; counted in the HTTPRoute tile and install health (Q12; Task 14) |
| A child whose spec names an owned HTTPRoute but has no eligible status entry | A declared candidate: never owned, gets KGWUI-011 (pending inside grace) (Tasks 15, 16) |
| A Backend with an `Accepted` condition | Health from the condition's own evidence; check `backend status` appended after Phase 1's checks (Task 17) |
| `GET` list endpoints for Gateways, ListenerSets, routes | Filtered, authorized, sorted, keyset-paged from one immutable index per install (Tasks 18–22, 24) |
| `GET` detail endpoints for Gateway, ListenerSet, HTTPRoute | Status, parents, rules, reported children, a bounded up-chain; `meta` gains `views` (Tasks 20, 21, 23) |
| A denied object behind a filter, Backend, `via` or ancestor step | Reveals no identity or message; `deniedInInstall` ignores filters (Task 24) |
| SPA | Gateways, ListenerSets and Routes lists; Gateway, ListenerSet and HTTPRoute pages; finding links; Backend reverse lookup (Tasks 26–30) |

**The Q12 count change.** On clusters that use delegation, the HTTPRoute tile and install health now include owned delegated children, so counts and colours can change against Phase 1. The release note and the README upgrade notes say so (Task 34).

## Root cause (if known)
n/a (planned work). Ownership through HTTPRoute parents is missing in `pkg/install/assign.go` (design §4.2).

## Acceptance tests
- Task 11: `TestUpstreamHasBackend`; guards `TestVerifierIgnoresBackend`, `TestClusterRoleCoversBackends`; the additive golden check.
- Task 14: Q12 before/after tests at the API, including `TestStaleEligibleEntryKeepsEdge` and the child-edit drop pin (PF16).
- Tasks 16–17: KGWUI-011 status trigger and Backend health, including the 2.3 implicit-ref warning pin (PF15).
- Tasks 19, 24: `TestCursorsAcrossRecomputes`, `TestDeniedInInstallIgnoresFilters`.
- Task 25: new Go goldens and the TypeScript golden check (Task 26).
- Tasks 27–30: Vitest for every page; Task 33: kind e2e on v2.3.11 and v2.4.7, Playwright, ui-loop recordings.
- Task 32: 2.2 budgets at Large.

## Scope
In: `pkg/kinds`, `pkg/objects`, `pkg/engine` (decode cache, `explore.Build`), `pkg/install`, `pkg/health`, `pkg/overview`, `pkg/explore`, `pkg/api`, `internal/testutil` (incl. `scale`), `web/packages/shell`, `test/e2e`, `web/e2e`, README, `docs/`.
Out: computed views and their rule sets (2.3, 2.4), GRPCRoute, TCPRoute and TLSRoute detail pages, push to the SPA, incremental index updates.

## Plan / design impact
None planned. Pre-flight rulings PF1–PF35 (`.superpowers/` ledger) adjust task file lists and orders; the load-bearing ones: Backend last in `kinds.Upstream()` and after Deployment in `meta` (PF1), each Phase 1 golden regenerated in the task that changes it (PF4), the retained-heap baseline in `bench/2.2-baseline-heap.txt` (PF32).

## Known limitations
- **Implicit refs on 2.3 (PF15, user ruling).** On kgateway 2.3, a child whose parentRef omits the namespace or sets `sectionName` gets the KGWUI-011 warning: the same rule applies on every version. A test pins it.
- **Child-edit drop leftovers (PF16).** After a drop caused by a child edit, a leftover eligible status entry keeps a stale owned child, counted, with KGWUI-003 (D-021: unobserved). A Task 14 test pins it; the release note says so.

## Exit criteria
From the plan's Sub-phases table: Q12 ownership with before/after tests; KGWUI-011 status trigger; Backend kind; list and detail APIs with Authorizer and goldens (Go and TS); Gateways, ListenerSets, Routes lists and the three detail pages; wording pass; 2.2 budgets met at Large; kind e2e green on v2.3.11 and v2.4.7; ui-loop recordings; release note; Codex review triaged.

## Files
- Task 11: `docs/phase-status.md`, this brief, `docs/phases/phase-2/bench/2.2-baseline-heap.txt`, `pkg/kinds/kinds.go`, `pkg/kinds/upstream.go`, `pkg/kinds/kinds_test.go`, `pkg/scopeverify/verify_test.go`, `deploy/helm/chart_test.go`, `internal/testutil/scale/scale.go`, `pkg/cache/fingerprint_test.go`, `pkg/engine/budget_test.go`, `pkg/api/types.go`, `pkg/api/handlers.go`, `pkg/api/testdata/golden/{meta,bare-meta,overview,bare-overview}.json`, `web/packages/shell/src/api/types.ts`
- Task 12: `pkg/objects/{gateway,httproute,backend,decoder}.go`, `pkg/objects/views_test.go`, `pkg/objects/objects.go`, `internal/testutil/fixtures.go`
- Task 13: `pkg/engine/decode.go`, `pkg/engine/decode_test.go`, `pkg/engine/engine.go`
- Tasks 14–15: `pkg/install/delegation.go`, `pkg/install/delegation_test.go`, `pkg/install/assign.go`, `pkg/health/checks.go`, `pkg/overview/overview.go`, `pkg/api/handlers.go`, `pkg/api/api_test.go`, `pkg/engine/bench_test.go` (PF9)
- Tasks 16–17: `pkg/health/{delegation,backend}.go` and tests, `pkg/health/finding.go`, `pkg/health/checks.go`, `pkg/health/messages.go`
- Tasks 18–20: `pkg/explore/{explore,rows,admit,query,cursor,ancestors}.go` and tests, `pkg/engine/engine.go`
- Tasks 21–24: `pkg/api/{explore_types,link,params,lists,details,visible}.go` and tests, `pkg/api/types.go`, `pkg/api/handlers.go`
- Task 25: `pkg/api/golden_test.go`, `pkg/api/additive_test.go`, `pkg/api/testdata/golden/*` (new fixtures)
- Tasks 26–30: `web/packages/shell/src/api/`, `src/explore/`, `src/pages/explore/`, `src/shell/`, `src/pages/install/`, `src/pages/ResourcePage.tsx`
- Task 31: `web/packages/shell/src/copy/explorer.ts`, `pkg/health/messages.go`
- Task 32: `pkg/api/budget_test.go`, `pkg/engine/budget_test.go`, `pkg/engine/bench_test.go`, `internal/testutil/scale/scale.go`, `.github/workflows/bench.yaml` (PF33)
- Task 33: `test/e2e/fixtures/explorer.yaml`, `web/e2e/explorer.spec.ts`, `test/e2e/setup.sh`, `test/e2e/api_test.go`
- Task 34: `docs/phases/phase-2/phase-2.2-complete.md`, `docs/phases/phase-2/reviews/2.2-codex.md`, `README.md` (Upgrade notes), `docs/phase-status.md`, `docs/roadmap.md`, `docs/phases/phase-2/design.md`

## Definition of done
- [ ] every task's tests fail before and pass after, or are reported as guards (reported per task)
- [ ] full test suite passes (Go, chart, web, kind e2e on v2.3.11 and v2.4.7)
- [ ] 2.2 budgets met at Large, heap against `bench/2.2-baseline-heap.txt`
- [ ] cross-model review of `git diff <base>..HEAD` + triage
- [ ] phase-2.2-complete.md written; phase-status.md and roadmap updated
