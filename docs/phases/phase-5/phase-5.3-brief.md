# Sub-phase 5.3 — Rule sets, view A, delegation

Kind: planned
Base: 0ca585e
Issues: n/a | Plan steps: implementation-plan.md, Sub-phase 5.3 (G3), Tasks 8–10

## Problem / goal
The three kgateway rule sets (`pkg/computed/kgw23`, `kgw24`, `kgw25`) each carry a copy of view A: `attach.go`, `allowed.go` and `hostnames.go` differ only in their package line. View A is spec-level Gateway API (D-020 (A)); listener isolation (2.4, 2.5) and delegation (views B, C) are kgateway behaviour.

`computed.Rules` requires `Delegation`, so every rule set must compute views B and C. A rule set without delegation has no way to say so: the engine, `install.Assign`, the refs check, the explorer and the API all treat a backendRef to an HTTPRoute as delegation for every install.

5.3 moves view A into one shared package, makes delegation an optional `computed.Delegator`, and turns delegation off for an install whose rule set lacks one. An install without a rule set (class-only) gets status only: no computed view and no KGWUI-011 or 012 (user, 2026-11-08). A kgateway user sees no change.

## Expected behaviour

| Input / action | Expected |
|---|---|
| Every fixture in `pkg/computed/testdata/kgateway-2.{3,4,5}`, every rule set | full view A, B and C output identical to the base; fixtures untouched |
| `BenchmarkAttachLarge`, chain budgets | allocations within 5 % of the base |
| API `golden` and `golden-v1` | unchanged |
| A rule set without `Delegator` (attachment only) | `GET …/delegation` is 404; route detail has no delegation ref state; a backendRef to an HTTPRoute is an ordinary ref in the refs check and admission; no KGWUI-011 |
| The same rule set | view A, its cross-check and a KGWUI-012 from an attachment disagreement still appear |
| A Go caller holding `computed.Rules` | asserts `computed.Delegator` to compute views B and C (README upgrade note) |
| Class-only install's Gateway and route detail | status-level attachment only; no `computed` fact anywhere |

## Root cause (if known)
The sets were ported by copy (`// Port of kgw23 …; keep in sync.`). `computed.Rules` declares `Delegation` (`pkg/computed/rules.go`), and the delegation pass in `install.Assign` (`pkg/install/delegation.go`), `delegationCondition` and `delegationRoute` (`pkg/health`) and the route-detail backendRef (`pkg/api/details.go`) do not ask whether the install's rule set delegates.

## Acceptance tests
- `pkg/computed`: goldens pass unchanged; a scratch dump of every set's full output over every fixture and three scale worlds is byte-identical to the base (evidence in the 5.3 record, not committed).
- `pkg/computed/viewa`: the view A unit tests, moved from the sets, through `viewa.Attach`.
- `pkg/api`: an attachment-only rule set: delegation 404, no delegation ref state, ordinary refs, no 011, view A and 012 present.
- `pkg/api`: a class-only install's Gateway and route detail carry no computed facts.
- `examples/downstream` unchanged.

## Scope
In: new `pkg/computed/viewa`; `pkg/computed/kgw2{3,4,5}`; `pkg/computed/rules.go`; `pkg/install` (`Resolved`, delegation pass); `pkg/engine` (views, rule set before assignment); `pkg/health` (refs, condition rewording); `pkg/api` (delegation endpoint, backendRef); tests; README upgrade note.
Out: condition types, proxy mode, process, commands and verifier per install, the core boundary test (5.4); install features and web (5.5). Sharing delegation, logs or targets between the kgateway sets (they stay per version, design §8.1).

## Plan / design impact
None expected. Any ruling where the plan leaves a choice is recorded in the 5.3 complete file.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite and gates pass, bench gates recorded
- [ ] cross-model review of `git diff 0ca585e..HEAD` + triage
- [ ] phase-5.3-complete.md written; phase-status.md updated
