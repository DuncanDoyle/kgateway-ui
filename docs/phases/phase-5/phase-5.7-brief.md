# Sub-phase 5.7 — Checks report what they are waiting on (E9)

Kind: planned (after the Phase 5 close, before Phase 6; D-068)
Base: 78e38f8e
Issues: #33 (program edge E9, from kgwctl) | Plan steps: n/a
Review: reviews/5.7-brief-codex.md (all 8 findings accepted)

## Problem / goal
Some checks wait before they decide, and today the API and UI cannot tell that wait from a result.

- **Silent holds.** These checks report `ok: true` while they wait, so "What was checked" shows them green. That breaks its rule: green means checked and fine.
  - `status progress` (KGWUI-018) holds for 5 minutes.
  - `proxy config warming` and `proxy connection` (KGWUI-025, KGWUI-026) hold for 60 s.
  - `backend endpoints` (KGWUI-023) holds for 30 s.
  - The policy-target waits inside `resource status` (computed KGWUI-016 and the verifier's KGWUI-012, `TargetHold`) hold too. They can still be running after the grace period, when the check is already OK.
- **Pending checks have no due time.** These already set `pending: true` with a message, but say nothing about when they will decide:
  - `resource status` and `gateway proxies` (60 s grace);
  - `xDS rejects` (restart hold).
- After a UI restart every hold starts again, and a client sees nothing of that warm-up.

Reproduction: create a Service with no ready endpoints and a route to it. For 30 s, `GET /api/v1/installs/{id}/health` shows `backend endpoints` with `ok: true` and no finding, and the UI shows it green.

## Expected behaviour
A check that holds declares it. The install health API returns an optional `hold` object on each check result:
- `until` (RFC 3339): the earliest moment a held item is decided;
- `count`: how many items are held;
- `reason`: generic text that never names an object.

`ok` and `pending` keep their current meaning, and the install state rolls up exactly as before. A held item is the unit the check would raise its finding on, so the count matches the findings that can follow:

| Check | Held item |
|---|---|
| `status progress` (018) | the stuck objects 018 counts |
| `backend endpoints` (023) | a Service port |
| `proxy config warming`, `proxy connection` (025, 026) | a Gateway whose proxy pods are held |
| `resource status`, grace | a resource |
| `resource status`, policy targets (016, 012) | a policy target |
| `gateway proxies`, grace | a proxy |
| `xDS rejects`, restart hold | a Gateway |

When a check holds more than one kind of item, as `resource status` can with grace and policy targets, `count` is their sum and `until` the earliest due time.

| Input / action | Expected |
|---|---|
| 018: 3 of 20 objects stuck, so over the threshold; the longest stuck for 2 of 5 minutes | `status progress`: `ok: true`, `hold: {until: longest-stuck start + 5m, count: 3, reason}`; no finding; install state unchanged |
| 018: the longest-stuck object recovers during the hold | `until` moves to the next-longest object's start + 5m, as the existing rule does |
| 018: the hold ends with the objects still stuck | The finding appears; `hold` is gone |
| 023: two Service ports without ready endpoints, first seen 10 s and 20 s ago | `backend endpoints`: `hold.count: 2`, `hold.until` = the earlier due time (first seen 20 s ago + 30 s) (confirmed 2026-11-10) |
| 023: one Service with two ports, both without endpoints | `hold.count: 2` |
| One item held and one already raised in the same check | The finding for the raised item, plus `hold` for the held one |
| 025/026: a Gateway's proxy inside the 60 s condition age | `hold` on that check, count 1 |
| A policy whose target is missing, grace over, inside `TargetHold` | `resource status`: `ok: true` plus `hold` |
| `resource status` with 3 resources in grace | `ok: false`, `pending: true`, error text unchanged, plus `hold: {until: earliest grace end, count: 3}` (confirmed 2026-11-10; install state stays unknown, as today) |
| `gateway proxies` with a proxy not found yet; `xDS rejects` during the restart hold | Same: `pending` as today, plus `hold` |
| A check whose finding will clear later (process restarts, released config, data that turns stale) | No `hold`: a raised finding or an input deadline is not a wait |
| A running engine with unchanged inputs, at a hold's `until` | The engine recomputes by itself. The next API read shows the finding, or the moved hold |
| UI, "What was checked": a check with `hold` | A "deciding" state: its own icon and label, not green and not unknown, with a note such as "Deciding on 2 objects, in 25s" (relative time) (confirmed 2026-11-10) |
| UI, stale data | Every check shows unknown, holds included (D-006, unchanged) |
| A caller who may not see every object (D-013 restricted view) | The same `hold` as a full caller: aggregate count and time, generic reason. This matches how state and counts already cover every finding |

## Root cause (if known)
`RunContext.WakeAt` (`pkg/health/store.go:274`) only schedules a recompute, and its callers use it for three different things:
1. waiting before a finding;
2. the decay of a raised finding;
3. input staleness deadlines.

`CheckResult` (`pkg/health/finding.go:110`) has no field for a wait, and `Report.Next` is unserialised. Holds therefore have to be declared explicitly. They can't be inferred from `WakeAt`.

## Acceptance tests
- **Engine boundary** (`engine.Compute` over a fake clock): one test per hold site from the table, covering 018, 023, 025/026, policy targets, resource grace, gateway proxies grace and the xDS restart hold. Each asserts:
  - `ok`, `pending`, `hold.count` and `hold.until` while holding;
  - after `until`, the finding is raised and the hold is gone.
- **Aggregation**: two items with different due times give the earliest `until` and the summed count. A mixed raised-and-held check shows both.
- **Running engine**: unchanged inputs, safety recompute off, the clock advanced past `until`. The published model, read through the API, changes from hold to finding without any trigger.
- **API**:
  - the health JSON carries `hold` with an RFC 3339 `until` and no object names;
  - a check with no hold omits the field;
  - a restricted caller gets the same `hold`.
- **Decay and staleness**: a process-restart finding inside its window carries no `hold`. This is a regression guard; it already passes today.
- **Web** (CheckedList test):
  - `ok` plus `hold` renders "deciding" with the relative note;
  - `ok` without `hold` renders healthy;
  - stale data renders unknown.
- Every positive test fails before the change.

## Scope
In:
- the hold declaration in `pkg/health/store.go`;
- `CheckResult` in `pkg/health/finding.go`;
- the hold sites: `freshness.go`, `proxystats.go`, `endpoints.go`, `targets.go`, and the grace sites in `checks.go` and `xds.go`;
- the check output print in `rollup.go`, so a check that depends on another check's output sees hold changes;
- the API wiring of check results (`pkg/api/diagnostics.go`, `wireChecks`) where it copies fields;
- the web `CheckResult` type and `CheckedList`, with its copy;
- `docs/findings.md` where it describes holds;
- snellius.dev, if its docs describe "What was checked".

Out:
- Gateway detail (`DataPlaneDTO`): it projects selected fields, not check results, so it gets no hold yet.
- The per-collector next-run time: optional in #33, a follow-up if wanted.
- `skew.go`'s settle quiet, which already writes its reason to `detail`.
- Serialising `Report.Next`.
- A push channel: the SPA keeps polling every 5 s.
- New timing rules, or changing any hold length.
- Per-object hold details.

## Plan / design impact
D-068 records the scope. The API change is additive (a new optional field). Phase 3 design §5.3 ("green means checked and fine") gains the deciding state.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite and gates pass (go vet/test, web lint/typecheck/test)
- [ ] cross-model review of `git diff 78e38f8..HEAD` + triage
- [ ] phase-5.7-complete.md written; phase-status.md updated
- [ ] released in a tag; the tag commented on #33 and the program file's E9 row updated (approved like any commit)
