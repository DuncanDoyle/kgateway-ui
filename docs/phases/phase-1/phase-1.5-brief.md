# Sub-phase 1.5 — Phase review fixes

Kind: stabilization
Base: acee415bfd5025c19839689bfdc88fb1cf80c856
Issues: n/a (queue Q9–Q17) | Plan steps: n/a — findings of reviews/phase-codex.md, decisions D-017

## Problem / goal
The Phase 1 cross-model review found health false greens and contract gaps before the first release. A verifier reproduced findings 1–4 and 8. Fix them without new features.

## Expected behaviour

| Input / action | Expected |
|---|---|
| Controller Deployment with 0 desired replicas | Install red; error KGWUI-005 (Q9) |
| Managed proxy Deployment with 0 desired replicas | Warning KGWUI-006; install not green (Q9) |
| Owned ListenerSet with current `Accepted=False` or `Programmed=False` | A status finding on the ListenerSet, same severity rules as other required kinds; install not green (Q10) |
| Managed Gateway, status green, no proxy Deployment, within grace | No KGWUI-006 yet; the proxy check is pending; install not green (Q11) |
| Same, after grace | Warning KGWUI-006 as today (Q11) |
| Controller env: `KGW_DISCOVERY_NAMESPACE_SELECTORS` via `valueFrom`, then a later literal entry of the same name | Scope from the literal (last wins) (Q12) |
| Same name, last entry uses `valueFrom` | Scope unknown, as today (Q12) |
| Install with matching namespace selection; pre-v1 TCPRoute/TLSRoute or XListenerSet objects exist | No KGWUI-007 from those kinds (Q13) |
| `helm template` | ClusterRole has no `referencegrants` or `customresourcedefinitions` rule; every kind the backend reads is still granted (Q14) |
| YAML view open while the object's status changes | Updates within one poll interval (5 s); the page shows when the data was collected (Q15) |
| `GET /api/v1/installs/{id}/resources/...` | JSON body unchanged; a response header carries the collection time (Q15) |
| README extension example | Starts and serves; errors are returned (Q16) |
| v1 JSON of `/installs`, `/meta`, `/overview`, `/health`, findings | Pinned by golden fixtures independent of the Go DTO types, incl. omitted fields (Q17) |

## Root cause (if known)
- Q9: `pkg/health/checks.go:142` treats `ready >= desired` as ready, so 0 ≥ 0 passes.
- Q10: `checks.go:264` maps ListenerSets for ownership but never checks their conditions.
- Q11: `checks.go:441-452` returns OK while grace suppresses the finding.
- Q12: `pkg/install/scope.go:127` returns unknown on the first `valueFrom` entry.
- Q13: `pkg/scopeverify/verify.go:169` counts every served route kind.
- Q14: design §8 listed unused grants.
- Q15: `web/packages/shell/src/api/hooks.ts:56` has no refresh interval.
- Q16: `README.md:95` leaves intervals at zero and drops the error.
- Q17: `pkg/api/api_test.go` decodes into production DTOs.

## Acceptance tests
At the HTTP API boundary (`pkg/api` tests through the real handlers and engine) for Q9–Q13 and Q15's header; chart tests for Q14; Vitest for Q15's polling and age display; a compile-and-run test or example test for Q16; golden JSON fixtures for Q17. Each must fail before its fix.

## Scope
In: pkg/health, pkg/install, pkg/scopeverify, pkg/api, pkg/overview (if it repeats the readiness predicate), deploy/helm, web/packages/shell (YAML page and hooks), README.md, design.md text for §4.4, §5.2, §6.6, §8.
Out: the upstream scope endpoint; reading the experimental-features flag; Phase 2 detail pages; the deferred minors in the 1.x complete records.

## Plan / design impact
D-017. Design §4.4 (stable kinds), §5.2 (ListenerSet required; zero replicas; pending during grace), §6.6 (YAML polling and age), §8 (ClusterRole) change to match.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite passes (Go, chart, web, kind e2e)
- [ ] cross-model review of `git diff <base>..HEAD` + triage (stabilization)
- [ ] phase-1.5-complete.md written; phase-status.md updated
