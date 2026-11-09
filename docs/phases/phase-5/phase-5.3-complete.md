# Phase 5.3 — Rule sets, view A, delegation (Complete)

**Completed:** 2026-11-08
**Range:** 0ca585e..ecacaa2 (branch `duncandoyle/genericCore`)

## Summary
The three kgateway rule sets share one spec-level view A in `pkg/computed/viewa` (allowed routes, sectionName and port, hostname intersection); listener isolation stays in the kgateway sets. Non-test code shrinks by 1,061 lines. Delegation is an optional rule-set capability (`computed.Delegator`); without it, a backendRef to an HTTPRoute is an ordinary backend reference everywhere. An install without a rule set gets status only, with no computed view and no KGWUI-011/012 (user, 2026-11-08). kgateway output is unchanged: a dump of 972 rule-set outputs is byte-identical before and after, and the goldens and fixtures are untouched. The README has the `Delegator` upgrade note.

## Sub-phases (phase record only)
n/a

## Reviews
- `reviews/5.3-codex.md`: 3 findings (1 P2, 1 P3, 1 P4), all accepted and fixed (b98dd1f, 9657b82, e1016dd). Codex independently confirmed the 972-output identity and found no kgateway regression.

## Waivers
None.

## Deliverables
- Gates: build, `go vet` (default, `e2e`, `bench`), `go test -race ./...` (39 packages), `TZ=Europe/Amsterdam go test ./pkg/api`, `make lint`, `make actionlint`, `helm lint`, `REQUIRE_HELM=1 go test ./deploy/...`.
- Bench: every budget passes. Retained heap +23.4 MiB over the 2.1 baseline at Large, +44.6 MiB worst (budget 48); warm recompute 33 ms, worst 50.5 ms; cold 48 / 65 ms.

## Notes for later phases
- Tight budget: the analyzer "memoized, new scrape" worst case is 1,024,336 of 1,027,000 B (99.7 %). It rose in 5.2 (5.1: 995,368 B), not in 5.3. To be reduced in 5.4 before more work lands on that path.
- The worst-tier retained heap varies between about +39 and +45 MiB across runs (budget 48).
- `viewa` compares full GroupKind; the kgateway sets keep their kind-only matching explicitly.
- TDD lapse on the P4 fix: implemented before its tests; both tests fail under mutation.

## Deferred / open
None.
