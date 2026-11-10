# Sub-phase 5.7 — Checks report what they are waiting on (E9)

Kind: planned (after the Phase 5 close, before Phase 6; D-068)
Base: 78e38f8e
Issues: #33 (program edge E9, from kgwctl) | Plan steps: n/a

## Problem / goal
Some checks wait before they decide, and today the API and UI cannot tell that wait from a result.

- **Silent holds.** These checks report `ok: true` while they wait, so "What was checked" shows them green. That breaks its rule: green means checked and fine.
  - `status progress` (KGWUI-018) holds for 5 minutes.
  - `proxy config warming` and `proxy connection` (KGWUI-025, KGWUI-026) hold for 60 s.
  - `backend endpoints` (KGWUI-023) holds for 30 s.
- **Pending checks have no due time.** These already set `pending: true` with a message, but say nothing about when they will decide:
  - `resource status` and `gateway proxies` (60 s grace);
  - `xDS rejects` (restart hold).
- After a UI restart every hold starts again, and a client sees nothing of that warm-up.

Reproduction: create a Deployment-backed Service with no ready endpoints and a route to it. For 30 s, `GET /api/v1/installs/{id}/health` shows `backend endpoints` with `ok: true` and no finding, and the UI shows it green.

## Expected behaviour
A check that holds declares it. The API returns an optional `hold` object on each check result: `until` (RFC 3339), `count` (number of held objects) and `reason` (short text). `ok` and `pending` keep their current meaning, and the install state rolls up exactly as before.

| Input / action | Expected |
|---|---|
| 018: one Deployment stuck, held 2 of 5 minutes | `status progress`: `ok: true`, `hold: {until: start+5m, count: 1, reason}`; no finding; install state unchanged |
| 018 hold ends with the object still stuck | The finding appears; `hold` is gone |
| 023: two Services without ready endpoints, first seen 10 s and 20 s ago | `backend endpoints`: `hold.count: 2`, `hold.until` = the earlier due time (first seen 20 s ago + 30 s) (inferred) |
| One object held and one already raised in the same check | The finding for the raised object, plus `hold` for the held one |
| 025/026 inside the 60 s condition age | `hold` on that check, as for 023 |
| `resource status` with 3 resources in grace | `ok: false`, `pending: true`, error text unchanged, plus `hold: {until: earliest grace end, count: 3}` (inferred) |
| `gateway proxies` with a proxy not found yet; `xDS rejects` during the restart hold | Same: `pending` as today, plus `hold` |
| A check whose finding will clear later (process restarts, released config, data that turns stale) | No `hold`: a raised finding or an input deadline is not a wait |
| A hold starts, moves or ends with no other change | The health stream sends an update (the report counts as changed) |
| UI, "What was checked": a check with `hold` | A "deciding" state: its own icon and label, not green and not unknown, with a note such as "Deciding on 2 objects, in 25s" (relative time) (inferred) |
| UI, stale data | Every check shows unknown, holds included (D-006, unchanged) |
| API on a Gateway page | Counts only; `hold` never names an object, as `Gaps` and `Withheld` stay unserialised |

## Root cause (if known)
`RunContext.WakeAt` (`pkg/health/store.go:274`) only schedules a recompute, and its callers use it for three different things:
1. waiting before a finding;
2. the decay of a raised finding;
3. input staleness deadlines.

`CheckResult` (`pkg/health/finding.go:110`) has no field for a wait, and `Report.Next` is unserialised. Holds therefore have to be declared explicitly. They can't be inferred from `WakeAt`.

## Acceptance tests
- Engine boundary (`engine.Compute` over a fake clock), for 018 and 023:
  - holding returns `ok: true` plus `hold`, with the `until` and `count` from the table;
  - after `until`, the finding is raised and the hold is gone.
- API: the health JSON for an install carries `hold` with the RFC 3339 `until` and no object refs. A check with no hold omits the field.
- Stream: a change that only moves a hold sends a health update.
- Pending sites: `resource status` in grace carries `pending: true` and `hold`.
- Decay and staleness: a process-restart finding inside its window carries no `hold`.
- Web (CheckedList test): `ok` plus `hold` renders "deciding" with the relative note. `ok` without `hold` renders healthy. Stale data renders unknown.
- Each test fails before the change.

## Scope
In:
- the hold declaration in `pkg/health/store.go`;
- `CheckResult` in `pkg/health/finding.go`;
- the holding checks: `freshness.go`, `proxystats.go`, `endpoints.go`, and the pending sites in `checks.go` and `xds.go`;
- the report fingerprint in `rollup.go`;
- the web `CheckResult` type and `CheckedList`, with its copy;
- `docs/findings.md` where it describes holds;
- snellius.dev, if its docs describe "What was checked".

Out:
- the per-collector next-run time (optional in #33; a follow-up edge item if wanted);
- `skew.go`'s settle quiet, which already writes its reason to `detail`;
- serialising `Report.Next`;
- new timing rules, or changing any hold length;
- per-object hold details.

## Plan / design impact
D-068 records the scope. The API change is additive (new optional field). Phase 3 design §5.3 ("green means checked and fine") gains the deciding state.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite and gates pass (go vet/test, web lint/typecheck/test)
- [ ] cross-model review of `git diff 78e38f8..HEAD` + triage
- [ ] phase-5.7-complete.md written; phase-status.md updated
- [ ] released in a tag; the tag commented on #33 and the program file's E9 row updated (approved like any commit)
