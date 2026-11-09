# Phase 5 — Generic Gateway API core (Complete)

**Completed:** 2026-11-09 (local; closes for good once the PR's CI passes, including the core image build)
**Range:** 169712c..HEAD (branch `duncandoyle/genericCore`)

## Summary
The core of Snellius is now implementation-independent Kubernetes Gateway API, and kgateway is pack #1 on the same extension seam later downstream editions use. The default binary still ships with the kgateway pack, and for kgateway users nothing visible changed: API v1 only gained additive fields, and every kgateway page is pixel-identical in screenshots. An install no pack claims is a class-only install, status only, behind a chart value that is off by default. A core-only build exists and is proven in a private compatibility CI outside this repo (D-065). No multi-implementation support is claimed.

## Sub-phases (phase record only)
| Sub-phase | Kind | Issues | Description |
|---|---|---|---|
| 5.1 | planned | n/a | Packs with per-install hooks, class-only installs, default scope, `snellius-core` |
| 5.2 | planned | n/a | Metrics clean break into the kgateway pack; checks per install |
| 5.3 | planned | n/a | Shared spec-level view A; `Delegator` optional; status only without a rule set |
| 5.4 | planned | n/a | Verifier, process, proxy mode and commands per install; core boundary test; bench harness correction |
| 5.5 | planned | n/a | Install features and server packs in API v1; per-install web surfaces |
| 5.6 | planned | n/a | Compatibility smoke test, informational kgwctl job, docs |

## Reviews
- `reviews/design-codex.md` (17) and `design-codex-r2.md` (5): all accepted.
- Sub-phase reviews `5.1`–`5.5-codex.md`: 4, 4, 3, 2, 4 findings, all accepted and fixed.
- `reviews/phase-codex.md`: 5 findings (1 P1), all accepted and fixed.

## Waivers
None.

## Deliverables
- Go API breaks for v0.6.0 (README upgrade notes): controller metrics leave `engine.Config.Metrics` and `health.Input.Metrics` (`ctrlmetrics.CollectOnce` and `Publish` replace `Static` and `Source`); `computed.Rules` no longer requires `Delegation` (assert `computed.Delegator`); frontend callers register `kgatewayWebPack`.
- Final bench on 971323c: every budget passes (`bench-5.6.txt`).
- kgwctl: the program-file update and the cross-repo notice are drafted, to post at merge.

## Notes for later phases
- Policy and Backend semantics still live in core packages; they move with the policy phases (6 and 7).
- Class-only installs with the flag on pick up kgateway policies and show KGWUI-003 on them.
- The core boundary test is an import check; semantic leaks need review.

## Deferred / open
- First private compatibility run, after the push.
