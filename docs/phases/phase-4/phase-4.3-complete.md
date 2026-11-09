# Phase 4.3 — Clock skew between browser and cluster (Complete)

**Completed:** 2026-11-08
**Range:** a110f37..HEAD (branch `duncandoyle/clockSkew`)

## Summary
Data age is now measured on the server's clock. Each data response's `Date` header gives the server-minus-browser offset, and `useNow` adds it, so `isStale`, "Last checked Xs ago" and every `ago` read the corrected time. A minikube clock 35 min behind the browser no longer shows fresh data as stale; a clock ahead still turns stale when polls fail.

Offset changes within 2 s are ignored, so equal clocks read exactly as in v0.5.0 and ages do not jitter. Failed requests and non-2xx answers keep the last offset. The hidden-tab hold stores server time, so a skew learnt during the hold does not move it. The backend is unchanged: Go `net/http` already sends `Date`.

## Sub-phases (phase record only)
n/a

## Reviews
- `reviews/4.3-codex.md`: 3 findings (1 P1, 1 P2, 1 P3), all accepted and fixed (3630efe, README note).

## Waivers
None.

## Deliverables
- Acceptance tests: 3 hooks tests (35 min behind, 35 min ahead with failing polls, skew learnt while hidden) and 1 Shell test (header and switcher) failed before 5c1c5fd and pass after. The equal-clock and held-skew tests guard the tolerance and the hold.
- Gates: web typecheck, lint, vitest 809/809, build; `make lint` (0 issues). No Go change, so no `go test` run was needed.

## Notes for later phases
- Skewed clocks are known to about ±0.5 s plus latency. Add a millisecond server-time header only if a limit tighter than 10 s needs it.

## Deferred / open
None.
