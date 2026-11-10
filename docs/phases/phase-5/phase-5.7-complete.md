# Phase 5.7 — Checks report what they are waiting on (Complete)

**Completed:** 2026-11-10; merged in #34 ([`42d58fd`](https://github.com/DuncanDoyle/snellius/commit/42d58fd44c4fbfa81e25db45e7be08a87d95f14b)), CI green, released as `v0.7.0-alpha.1` (pre-release)
**Range:** 78e38f8..42d58fd (branch `duncandoyle/checkHolds`)

## Summary
A check that waits before it decides now declares the wait. Each check result carries an optional `hold` with `until` (RFC 3339, UTC), `count` in the check's own unit, and a generic `reason`. Several holds in one check give the earliest `until` and the summed `count`. `ok`, `pending` and the install roll-up are unchanged, and restricted callers get the same aggregate hold (D-013, D-068). "What was checked" shows a held check as "Deciding", with a note such as "Deciding on 2 objects, in 25s". The hold sites are 018, 023, 025/026, policy targets (016, 012), resource and gateway proxy grace, and the xDS restart hold. Closes #33 (program edge E9).

## Sub-phases (phase record only)
n/a

## Reviews
- `reviews/5.7-brief-codex.md`: 8 findings on the brief, all accepted before implementation.
- `reviews/5.7-codex.md`: 3 P3 findings on the code, all accepted and fixed (f8b214b, e55dad0), with mutation-checked tests.

## Waivers
None.

## Deliverables
- Gates: `go vet`, `go test -race ./...`, web lint, typecheck and tests; PR CI green on every job (go, web, helm, image, bench, hygiene, the kind e2e matrix, kgwctl compile).
- Live demo on a cluster: before (v0.6.0) the endpoints check showed green for about 35 s, then KGWUI-023; after, it showed "Deciding" counting down, then KGWUI-023. The API hold had `until` = first seen + 30 s, then disappeared.
- `release.yaml` marks a tag with a suffix as a GitHub pre-release.

## Notes for later phases
- A check can hold some items and raise findings on others; both show.
- Gateway detail (`DataPlaneDTO`) has no hold; add it when a Gateway page needs it.

## Deferred / open
- snellius.dev findings page: syncs with the full `v0.7.0` release, since the site documents stable releases.
- Program file E9 row: proposed to the user, not yet committed.
- Per-collector next-run time (optional in #33): a follow-up if wanted.
