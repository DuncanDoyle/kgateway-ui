# Sub-phase 4.3 — Clock skew between browser and cluster

Kind: stabilization
Base: a110f372df8bfbab71bcf1feeb4259a63872befb
Issues: n/a (user report, 2026-11-08) | Plan steps: n/a

## Problem / goal
On 2026-11-08 the minikube VM clock ran 35 min behind the Mac after the Mac slept. Fresh data then showed "Data is stale. Last checked 35m ago" and states turned unknown. A cluster clock ahead of the browser hides real staleness the same way.

Reproduction: serve `/api/v1/installs` with `computedAt` and the HTTP `Date` header both 35 min behind the browser clock; the header and the data age show stale and unknown.

## Expected behaviour
Data age is measured on the server's clock: age = (browser now + offset) − collectedAt, where offset is the server-minus-browser clock difference taken from the response `Date` header when it arrives.

| Input / action | Expected |
|---|---|
| Server clock 35 min behind the browser, data fresh | Not stale; "Last checked 0s ago"; states keep their value |
| Server clock 35 min ahead, then polls fail | Stale after two poll intervals, as with equal clocks |
| Equal clocks (difference within the `Date` header's 1 s resolution plus latency) | Unchanged from v0.5.0 |
| Tab hidden, then shown (D-063 hold) | Unchanged: the clock holds until the first poll settles, or 10 s |

## Root cause (if known)
`isStale` (`web/packages/shell/src/api/hooks.ts:31`) and `ago` compare the server's `collectedAt` with `useNow()` (`web/packages/shell/src/api/clock.ts`), which reads the browser clock.

## Acceptance tests
- Web (hooks test, `DataAge` over `useInstalls`): a `Date` header and `computedAt` 35 min behind show "Last checked 0s ago", not stale.
- Web (hooks test): a `Date` header and `computedAt` 35 min ahead, then failing polls, show stale after two intervals.
- Web (Shell test, header and switcher): an install summary confirmed 35 min ago on a server clock 35 min behind is not unknown.
- Existing hidden-tab and failing-poll tests keep passing.

## Scope
In: `web/packages/shell/src/api/clock.ts` (offset), `client.ts` (read `Date`), tests, README upgrade note.
Out: backend changes (Go `net/http` already sends `Date`); an explicit millisecond server-time header; round-trip latency estimation.

## Plan / design impact
None. The stale rule (2 × poll interval, design §6.3) is unchanged; only the clock it reads is corrected.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite and gates pass
- [ ] cross-model review of `git diff a110f37..HEAD` + triage
- [ ] phase-4.3-complete.md written; phase-status.md updated
