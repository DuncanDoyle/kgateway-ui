# Phase 6.1 — Cache access for kgwctl (Complete)

**Completed:** 2026-11-10 (local branch `duncandoyle/phase6`, not yet pushed)
**Range:** 5704f91..6bf3a46

## Summary
Library users get two additive ways to read the cluster (design §12, D-076). `cache.ListOnce` fills the same `Snapshot` from LISTs only, so a user with `list` but not `watch` works (#18). `Config.Namespaces` limits namespaced inputs to a list, with the aggregate `Access` kept and per-namespace detail in `NamespaceAccess` (#19). In a scoped snapshot, a check never judges a namespace it did not read: KGWUI-023 is "not available" there, and no Service-missing explanation is given. Snellius itself is unchanged.

## Sub-phases (phase record only)
n/a

## Reviews
Per-task reviews (Claude subagents, same model family; the cross-model review is at phase close):
- Task 19 `ListOnce`: approved, 4 minors (2 fixed in Task 20, 2 deferred).
- Task 20 namespace scoping: 1 Important (unrequested namespaces read as readable), fixed in `2856030`.
- Task 21 close: wording fixes in `6bf3a46`.

## Waivers
None.

## Deliverables
- Commits: `39220d3` ListOnce, `2790e49` + `2856030` namespace scoping, `3bcdb5e` + `6bf3a46` D-070 … D-077, README examples, `TestListOnceLive`, `bench-6.1.txt`.
- Gates: `make test`, `make lint`, `make bench` pass. `TestListOnceLive` passes on kind for v2.4.7/v1.6.1, v2.3.11/v1.5.1 and v2.5.0/v1.7.0.
- Demo reel (terminal output, not in the repo): scratchpad `reels/6.1/summary-reel.mp4`.

## Notes for later phases
- Worst-world retained heap swings 39–45 MiB over the 2.1 baseline on unchanged code (A/B against the base, two runs each). 6.2 measures its additions with A/B runs, not one run.
- Events stay cluster-wide under `Config.Namespaces`; a namespace-only user sees Events as denied.

## Deferred / open
- Full local e2e matrix (UI image, Go e2e suite, Playwright) did not run: Docker Desktop's resolver returns no A records, so `npm ci` fails in the image build. CI runs the matrix when the branch is pushed.
- Tag proposal, kgwctl #18/#19 comments and the program file E2 row: queued for the user.
- Minors: unpaged consistent LIST in `ListOnce` (API-server cost on large clusters); `scopeCheck` has no text for "controller not observed"; partial-endpoints detail labels syncing namespaces with the aggregate reason.
- Decision Alternatives lines in D-070 … D-077 that the design does not state are the implementer's wording; the user should check them.
