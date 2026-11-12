# Phase 6.6 — Gateway map, the rest; phase close (Complete)

**Completed:** 2026-11-12 (local branch `duncandoyle/phase6`, not yet pushed)
**Range:** b246dcc..c227d8d (the e2e matrix and the reel ran afterwards, on `ec258bb`, after 6.7)

## Summary
The Gateway map gains its remaining interactions (design §6.1). A route expands into its rules; a delegating route shows its children at rest and collapses on request. Focus mode reduces the map to one node's upstream and downstream paths. `expand`, `collapse` and `focus` live in the URL. Healthy edges animate (never under `prefers-reduced-motion`; only healthy edges move). The current view exports as SVG or PNG, with no new dependency. Under a state filter such as Problems only, an edge is never drawn broken just because the loaded rows fail. The phase close adds the final bench, README sections, documentation notes, the design sync and D-078.

## Sub-phases (phase record only)
n/a

## Reviews
Per-task Claude reviews (same model family; the cross-model review is `reviews/phase-codex.md`, fixed in 6.7). Notable findings fixed:
- Task 48: a collapsed parent merged child rule keys (wrong share); Find could not reach collapsed delegated routes; the backend column shifted on expand. Stale-id pruning read "unfiltered" from the URL and could delete user-set ids; it now reads the loaded query.
- Task 49: export failures were silent; buttons were active with no exporter; a sample-writer test asserted nothing.

## Waivers
None.

## Deliverables
- Commits `d509e22` … `9d159d9` (Tasks 48–49), `94155aa`, `c2a25a2`, `c227d8d` (Task 50 part 1).
- Bench (`bench-6.6.txt`): D-057 settled +46.6 of 48 MiB at large-contradicting-worst (warm again +47.4, reported); list p95 12.4 ms (gate 50); paths cold 31.2 ms, warm 1.22 ms; map chunk 66,309 B gzipped (budget 80 kB).
- e2e on `ec258bb`, three pairs (v2.3.11 / v1.5.1, v2.4.7 / v1.6.1, v2.5.0 / v1.7.0), local binary: Go 10 pass, 2–3 skip (the NACK tests); forced-disagreement test passes against the built image; Playwright 85 passed, 16 skipped (NACK world); `map.spec.ts` 19 of 19, including expand, focus, export and animation.
- Wording: `phase-6.6-wording.md` (22 strings, five options each, picked by a separate subagent).
- Design synced to the build (design §4–§14); D-078 records the rulings made while the user was away; README "Traffic and backends"; `documentation-notes.md` topics.
- Demo reel: 7.5 MB, 1600×900, 111.8 s, one part, kept outside the repo.

## Notes for later phases
- Export covers the whole drawn graph (filters, focus and collapse applied), not the viewport.
- The rule column's pitch is always reserved, so backends never move on expand. The resting map is wider.
- Incremental and reload route order can differ after an expand (minor).

## Deferred / open
- UX, from the e2e review: selecting a backend at fit view re-centres it and the side panel clips route names.
- UX: a node reads "Healthy" beside "1 of 1 paths through this node fail". It is correct (node state against path state) but reads as a contradiction.
- Expand ids under a collapsed parent stay until the next unfiltered prune (minor).
- The e2e run needed `fs.inotify.max_user_instances` raised to 512 in the colima VM; it resets on a colima restart.
