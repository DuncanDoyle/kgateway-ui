# Phase 6.7 — Phase review fixes (Complete)

**Completed:** 2026-11-12 (local branch `duncandoyle/phase6`, not yet pushed)
**Range:** c227d8d..9c40d6c

## Summary
Fixes the nine findings of the Phase 6 cross-model review (`reviews/phase-codex.md`), then three rounds of findings from Codex re-reviews. States are never healthier than their signals: a ref failure no longer hides a programming failure, delegated hops follow view C's child-entry checks, an unused rejected Backend is an error, and a complete endpoint read that contradicts the cache makes the detail unknown and says why. Restricted viewers learn nothing about hidden use: Backends with no visible use read "No visible users" and take the judge's verdict only. On the map, listener signals light the listener, dedupe keys on a server row id, and a walk restarts when the query's own result changes or the server rejects its cursor. TCP and TLS rules keep the Filters area.

## Sub-phases (phase record only)
n/a

## Reviews
- Claude task reviews: part A (2 Important, fixed in one round), part B (none).
- Codex: `reviews/6.7-codex.md` (5 fixed, 4 partial, 7 new, all accepted), `reviews/6.7-codex-r2.md` (2 new, accepted; 1 tracked), `reviews/6.7-codex-r3.md` (both fixed, none new).

## Waivers
None.

## Deliverables
- Commits `6c0c015` … `9c40d6c`.
- API, additive: `SignalDTO.listener`, `PathRowDTO.id`, paths `revision` (a digest of the query's visible ordered result, titles included). golden-v1 byte-equal.
- Paths cursors break ties by rule, then row id; a cursor from an older server returns 400, and both the Traffic list and the map restart on it.
- Gates: `make test`, `make lint` (0 issues), web 976/976, lint, typecheck, build.
- Bench: D-057 settled +46.9 MiB (budget 48); cold `/paths` 59.8 ms on the large world (was 37, budget 100); warm p95 ≤ 2.05 ms.
- Wording: S822 "the controller does not accept it" and S823 rows in `phase-6.3-wording.md`; "No visible users", "Nothing you can see uses this backend." and the cache-disagreement sentence picked by a separate subagent from five options each.

## Notes for later phases
- Baselines to watch (Codex r2 #3): cold `/paths` 59.8 of 100 ms, D-057 46.9 of 48 MiB. Storing row hashes in the Topology would cut the cold cost for +8 B/row of heap.
- The compatible child-rejection test injects its class; no real kgateway 2.5 world produces that class with traffic rows (comment in `project_test.go`).

## Deferred / open
- Pre-existing (before Phase 6): the Findings API lists KGWUI-002 on a Backend that only hidden routes use, which reveals hidden use (D-077). Queued for the user to schedule.
