# Sub-phase 1.1 — Backend

Kind: planned
Base: 8fb0fa564a6cf62b6dd98e5ae120257fd3d4b6a6
Issues: n/a | Plan steps: implementation-plan.md Tasks 1–11

## Problem / goal
Build the Go backend of the Phase 1 UI: a single binary that discovers kgateway installs, watches their resources with a shared informer cache, scrapes controller metrics, evaluates health findings (KGWUI-001…009) and serves the v1 JSON API, with the Go extension seam a downstream build imports.

## Expected behaviour
As specified in design.md §3–§5, §7 and §8, and the per-task tests of implementation-plan.md Tasks 1–11. Key observable cases:

| Input / action | Expected |
|---|---|
| Cluster objects in (fake dynamic/typed clients) | API JSON out matching design §7 |
| Stale, absent or Pending status after grace | KGWUI-003, never healthy |
| Watch or list failure after sync | that kind unavailable, install unknown (D-014) |
| Metrics older than 3 × interval or unreachable | KGWUI-008, state unknown |
| Deny-some Authorizer | resource 404; finding kept as restricted; roll-up unchanged |
| Downstream extension registers kind/detector/check | its findings and counts appear in API output |

## Root cause (if known)
n/a (new work)

## Acceptance tests
The API boundary tests of Task 10 and the extension-seam test of Task 11; unit tests per task. Task 1 records D-012 from a real kind cluster.

## Scope
In: cmd/, pkg/, internal/, Makefile, go.mod, LICENSE, docs/decisions.md (D-012).
Out: web/ (sub-phase 1.2), Dockerfile, Helm chart, CI, e2e (sub-phase 1.3).

## Plan / design impact
D-012 is recorded by Task 1. Any deviation goes to decisions.md and the user.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite passes (`go vet`, `go test ./...`)
- [ ] cross-model review of `git diff <base>..HEAD` + triage (planned: at the phase review)
- [ ] phase-1.1-complete.md written; phase-status.md updated
