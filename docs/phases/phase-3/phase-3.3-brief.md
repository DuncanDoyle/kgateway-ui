# Sub-phase 3.3 — Analyzer

Kind: planned
Base: 79a53073ff5914a5b1396dfbb11803e04a11d5c6
Issues: [#13](https://github.com/DuncanDoyle/kgateway-ui/issues/13) (E8) | Plan steps: implementation-plan.md Tasks 55–73 and 71a; design §5.2, §5.3, §7.1–§7.7, §8.9, §10, §14 rows 013–018, §16.3, §17.1, §18, §21; D-020, D-024, D-030, D-034, D-036, D-037, D-040, D-043, D-044, D-046, D-047, D-053

## Problem / goal
Today a route with a missing ReferenceGrant, a typo'd backend group or a replaced rule shows up as a generic KGWUI-002 "condition False". A policy whose target does not exist reads "not yet processed". A conflicted listener on a Gateway that says `Accepted=True` raises nothing. When status stops moving for the whole install, the user sees many unrelated 003 warnings instead of one cause. The user has to read raw conditions and guess the fix.

3.3 adds the analyzer floor:
- specific codes 013–018 that replace 002 for their reasons (D-030);
- 003 explained by kind;
- four new groups and a grouped findings view;
- suggested fixes where exactly one answer exists (D-036);
- opt-in derived signals from controller logs that keep fixed fields and never the text (D-037);
- `docs/findings.md`.

Two program edges ride along. E3: the analyzer is usable by an external Go module. E8 (#13): an exported config helper and a static metrics source for one-shot callers (D-053).

## Expected behaviour

### Freshness and the 1 KiB clip (Task 56)

| Input / action | Expected |
|---|---|
| A route with no status from this controller, after the 60 s grace | 003 warning, evidence `freshness: never-written`. Severity and grace are unchanged: info inside the grace, warning after |
| A route whose only status is the CRD default (`Pending`, "Waiting for controller", `lastTransitionTime` at the Unix epoch) | 003, `never-written`, never `pending` |
| `observedGeneration` 3, `metadata.generation` 4 | 003, `stale`, with the observed and current generations and since when |
| A current condition with reason `Pending`, or status `Unknown` | 003, `pending` |
| A policy with no ancestor from this install, and a target that exists | 003, `no-ancestor` |
| An object that matches two kinds, for example stale and `Pending` | One 003. The kind is the first that matches in the §10.1 table order **(inferred)** |
| A status message of 64 KiB with a fake key at its end | Any evidence that quotes it is cut at 1 KiB at a UTF-8 boundary, with a marker. The key appears in no API body |

### References: 013 and 014 (Tasks 57, 58)

| Input / action | Expected |
|---|---|
| A route parent `ResolvedRefs=False/BackendNotFound`; the backendRef's group `delegation.gateway.example` serves no kind `Service` | 013 error on the route: "No kind `Service` in group …. Check the group." No 002 for that condition |
| The same, and exactly one served group has a kind `Service` | The suggested fix is the ref with that group, as a fragment at `spec.rules[i].backendRefs[j]` |
| The same, and two served groups have that kind | 013 with no fix, only next steps |
| A route parent `ResolvedRefs=False` with any other reason, group served | 013 with the controller's reason and message, clipped |
| A listener `ResolvedRefs=False/InvalidCertificateRef` | 013 on the Gateway or ListenerSet, keyed by listener name, quoting the controller. It never says whether the Secret exists |
| A route parent or listener `ResolvedRefs=False/RefNotPermitted`, one cross-namespace ref of the failing field | 014 error. Suggested fix: the ReferenceGrant as a new object in the target namespace, `from` (group, kind, namespace of the referrer), `to` (group, kind, optional name), spec-defined fields only. The YAML parses and round-trips |
| The same for a listener `certificateRef` | 014 with the grant for the Secret ref |
| `RefNotPermitted` on a parent with two cross-namespace backendRefs | 014 lists both candidates with their locations, `attributed: false`, next steps, no YAML |
| `RefNotPermitted` where the ref's group serves no such kind | 014, explained as a wrong group, never as a missing grant. It carries no grant. It carries the corrected-group fix when exactly one served group has the kind **(inferred)** |
| A `RefNotPermitted` on a kgateway-specific field (policy or extension ref) | 014 quoting the controller's message, no YAML |
| A grant or fix that would name an object the caller cannot view | No fix. The finding keeps its next steps |
| Any other current `False` condition, such as `Accepted=False/NotAllowedByListeners` | 002, as today |
| One condition | Exactly one finding: its specific code or 002, never both |
| The Phase 2 goldens | Change only in the `code` and `slug` of the named 013/014 findings, listed in `additiveExceptions`. A new unlisted scalar change still fails the additive test |

### Rules replaced or dropped: 015 (Task 59)

| Input / action | Expected |
|---|---|
| 2.3: route parent `Accepted=False`, reason `RouteRuleReplaced`, message `Replaced Rule (2): …` | 015 error on the route. Evidence reads "match 2", never a rule index. No 002 |
| 2.4: `kgateway.dev/Programmed=False`, reason `RouteRuleDropped` | 015 |
| The message holds a `group/Kind/namespace/name` token of a served policy kind | The evidence links the policy. The finding joins the "Replaced by policy" group with that policy as the cause |
| The message does not parse | The message is quoted; no link; the finding is in the "Other" group |
| The linked policy is hidden from the caller | `evidenceRestricted: true`, no evidence (§14 row 015) |
| Replaced for 9:59, then 10:01 | No hint, then the #14047 hint. The 10 minutes count from the finding's `since` (the condition's `lastTransitionTime`) **(inferred)** |

### Policy target not found: 016 (Tasks 60, 61)

| Input / action | Expected |
|---|---|
| Observation on 2.3.11 and 2.4.7 (Task 60 Step 0) | `computed-rules.md` "Policy targets" records which ancestors the controller writes per shape. If it contradicts the verifiable shapes below, the work stops and the user decides |
| A TrafficPolicy whose only targetRef names a Gateway that does not exist in the policy's namespace | Computed 016 warning, labelled with the rule and rule set. None at grace + 29 s; present at grace + 31 s. No 003 for the policy |
| A targetRef whose `sectionName` names no listener or rule | 016, "section not found" |
| A targetSelector whose labels match no object of its kind in the policy's namespace | 016, "matches nothing" |
| Two targets, one missing | One 016 for the missing target. The policy's other findings are unchanged |
| A target kind that is not served or not watched | Nothing |
| Discovery scope unknown (D-024) | No 016; the rule is unknown |
| A single-target policy the rule calls missing, with a current ancestor for that target | KGWUI-012, check kind `policy-target` |
| A multi-target or selector policy | 016 labelled "not verified"; never a KGWUI-012 |
| An `acme` rule set wrapping `kgw24` without `PolicyTargeter` | Builds and serves; no computed 016 |
| A `v2.5.0-rc.1` controller on the 2.4 rule set | The "newer than tested" note and the computed 016 |
| e2e `tp-missing-target` on 2.3.11 and 2.4.7 | 016, not 003 |

### Listener conflicts: 017 (Task 62)

| Input / action | Expected |
|---|---|
| A Gateway `Accepted=True/ListenersNotValid` with listener `https` `Conflicted=True`, and an accepted listener on 443 with the same hostname | 017 error keyed by `https`. Evidence names the other listener, labelled "Computed by the UI", rule "listener port and hostname" (R95) |
| `Conflicted=True` and no other accepted listener matches | 017 with "The controller reports a conflict; no other accepted listener on port 443 has the same hostname". No KGWUI-012 |
| The other side's Gateway or ListenerSet is hidden from the caller | `evidenceRestricted: true`, no evidence |

### Status not progressing: 018 (Task 63)

| Input / action | Expected |
|---|---|
| Controller ready; 10 objects with a 003 warning; the oldest for 4:59, then 5:01 | None, then one 018 warning on the controller Deployment |
| 9 of 200 objects with status | None. 10 of 200: 018 |
| 2 of 10 objects | None (minimum 3). 3 of 30: 018 |
| 018 present | Evidence: counts per §10.1 kind, the oldest age, and "status syncs in the last 5 min: N" when `kgateway_status_syncer_status_syncs_total` is present. Next step: the controller log command grepping `status` |
| Controller not ready | 005 only, no 018 |
| 018 present | The "Status not progressing" group holds 018 and the 003 warnings of that install it explains. Every 003 warning of the install counts as explained **(inferred)** |

### Groups and the grouped view (Task 64)

| Input / action | Expected |
|---|---|
| Two routes naming the same missing backend (identity after defaults) | One "Missing backend" group, two affected routes |
| An unattributed 013 (several candidate refs) | In the "Other" group, on its own resource **(inferred)** |
| 014s with one (from namespace, to namespace, to kind) shape | One "Not permitted" group |
| A fixture with every family | Each finding is in exactly one group, checked top-down in §5.2 order. Rank class 3 orders 015, 017, 013, 014, 016, 011, then 003 after grace; 018 is class 4 |
| `GET …/findings?view=groups&severity=…&code=…&q=…` | A page of `GroupRow`, paged on the server. `q` matches restricted rows by resource identity only. A bad filter value gives 400 **(inferred)** |
| A group whose cause the caller cannot view | A restricted row: severity, code, the restricted label, no affected list |

### Controller log signals (Tasks 65–67)

| Input / action | Expected |
|---|---|
| `diagnostics.controllerLogs` off (default) | No log stream. Capability `controllerLogs` reads `off`. No Role renders |
| On, Role missing in the controller namespace | Capability `denied`. No KGWUI-008; the install state does not change |
| On, two controller replicas | One follow stream per replica, from 15 min back, reconnecting with back-off from 1 s to 60 s |
| A controller container restart | One read of the previous container's log, at most 256 KiB |
| A 1 MiB line without a marker | Dropped unparsed; never buffered beyond the line scanner |
| An `"xds error"` line whose listener name equals a name in that Gateway's proxy stats | 001 evidence gains the name and the error class |
| A well-formed name that is not in the Gateway's proxy stats | Only the class is kept |
| Proxy stats off or failing | No names are kept, only classes |
| An error class not on the fixed list | `other` |
| 10 consecutive parser failures on marked lines | Capability `failing`, "log format not recognised" |
| More than 1,000 derived events for one install | The oldest go |
| The sentinel `AKIAFAKEKEY123456` in every accepted capture position, and behind spoofed markers | Absent from every API body, from a reflect walk of the published model, and from the UI's own `slog` output |
| A reject first logged 8 min before a UI restart | After the restart, 001's `since` equals the earliest derived event |
| A Go panic in the previous container | 019 evidence: class `panic` and the top frame's function name |
| A deployer apply error | 006 or 021 evidence: time, object kind and name, error class |
| The snapshot-deferral line on 2.3 or 2.4 | "Withheld" evidence on the named Gateway's 001 or Data plane section **(inferred)** |
| A derived event that names an object the caller cannot view | Filtered as that object (§14) |

### Finding reference, SPA and wording (Tasks 68–70)

| Input / action | Expected |
|---|---|
| `docs/findings.md` | One entry per code 001–026 (cause, evidence, fix), anchors `#kgwui-0NN`. A test fails when a `health.Code*` has no anchor. The README notes that UI-derived `since` resets on restart |
| Finding detail | "Learn more" links to the code's anchor; the suggested fix sits above the next steps |
| Suggested fix | Copy and download the YAML. A fragment shows its location. Commands and YAML render as text, never HTML |
| `/i/:id/findings?view=groups` | The grouped view; filters round-trip through the URL |
| Evidence for `ref-unresolved`, `ref-not-permitted`, `rule-replaced`, `freshness`, log events | One renderer each. Log events show names and class, never text |
| 720, 1280, 1600 and 3008 px, both themes | No horizontal page scroll; hover, active and focus-visible on every new control; no layout shift as data arrives |
| Every new user-facing string | Recorded in `phase-3.3-wording.md` by the §17.5 procedure |

### One-shot callers and the exported analyzer: E3, E8 (Task 71a, D-053)

The plan's file layout already puts the analyzer under `pkg/`: checks in `pkg/health`, groups and fixes in `pkg/diagnose`, rule sets in `pkg/computed`, run through `pkg/engine`. No package moves, so E3 needs no layout decision.

| Input / action | Expected |
|---|---|
| A Go module outside this repo imports only `pkg/` packages, builds both configs with the helper, starts the cache, waits for it to settle and computes once | The same findings, groups and ids as the UI's engine over the same objects, 013–018 included |
| The same caller reads a finding with one answer | The suggested fix is reachable through exported packages, with no HTTP server **(inferred)** |
| `pkg/app` built through the helper | Every existing test and golden unchanged |
| A static metrics source over one `Collect` result taken at T | At T + max age − 1 s, `xDS rejects` uses it. At T + max age + 1 s it is stale and `xDS rejects` is unavailable, as in the UI |
| The static source asked for an install it has no result for | No metrics; `xDS rejects` unavailable, as in the UI |
| A single pass with a restart hold, a grace period or scope-verifier persistence pending | Those checks read pending. `Report.Next` says when to compute again. The helper never waits; its Go doc says so |
| `analyze -f`: a snapshot built from files, with no cluster | Not settled by the design. See Plan points |

## Budgets (§16.3)

No gate changes without the user. A broken gate stops the work, and the numbers go to the user. Starting points: `bench/3.2.txt` (reference machine, D-044) and `bench/3.2-ci.txt`. Task 71 measures with the analyzer codes and the log reader at 50 matching lines/s.

| Measure | 3.3 budget at Large | 3.2 reference | 3.2 CI |
|---|---|---|---|
| Warm recompute median, consistent / contradicting / worst | ≤ 150 ms (reference) | 34.4 / 44.4 / 45.0 ms | 104.3 / 140.6 / 144.8 ms ⚠ |
| Analyzer stage, §16.3 scope, measurement world: warm / memoized / scrape per run | ≤ 20 / 2 / 2 ms | 3.76 / 0.21 / 1.14 ms | memoized with scrape 2.92 ms (reported) |
| Stream CPU, annotation-only | ≤ 15 % | 6.5 / 8.0 % | 9.8 / 13.6 % |
| Stream CPU, spec-edit, measurement world | ≤ 25 %; edit-driven recomputes ≤ 1/s (D-047) | 13.8 % (10 min); 0.93–0.95/s | 19.4 % (60 s) |
| OS memory, median of five | ≤ 288 MiB (D-043) | 264.0 MiB (spec-edit, measurement world) | — |
| Retained heap over 2.1, measurement world | ≤ 32 MiB (40 MiB from D-054), and ≤ +24 MiB above `3.1-baseline.txt` | +30.8 MiB ⚠ | +31.0 MiB ⚠ |
| 3.2 watches' retained state | ≤ 1,600,000 B (D-046) | 1,450,136 B | — |
| Dense: retained / cold / per warm | 176 / 246 / 185 MiB | 160 / 207 / 166 MiB | 160 / 221 / 166 MiB |
| `GET …/findings` (grouped view included), `…/diagnostics`, `…/processes` | p95 < 50 ms, p99 < 100 ms; p95 < 100 ms with 20 clients | 0.59 / 0.86 / 0.30 ms; 20 clients 1.37 / 4.85 / 1.21 ms | 20 clients 82.7 / 96.6 ms ⚠ |
| Burst, idle, cold | Phase 2 gates, unchanged | p95 1.003 s; 20 idle; cold 49–55 ms | — |

Retained heap is the gate most likely to break: 1.2 MiB of headroom in the measurement world. Derived log events (up to 1,000 per install), the 016 and 018 Stores and the grouped index all add to it. Task 71 stops the work when any gate is within 2 MiB, and the numbers go to the user before more code lands.

## Carry-forwards from 3.2

| Item | Outcome in 3.3 | Task |
|---|---|---|
| `diagnostics.controllerLogs` renders its Role (`controller-logs-role.yaml`), passes `--diagnostics-controller-logs` and is in `/meta` capabilities; nothing reads logs | The reader reads logs when the toggle is on. The chart template and `TestControllerLogsRoleIsOptInAndNamespaced` already exist and are extended, not re-created | 65 |
| Retained heap headroom 1.2 MiB | Measured with the 3.3 inputs; within 2 MiB stops the work | 71 |
| R57 (Services and EndpointSlices in the measurement world), R59 (ReplicaSet watch) | Not in 3.3; 3.4 | — |
| Parked Minors (`/meta` gate, endpoints RBAC early, empty `controllerLogsNamespaces` entry, two glossary nits, S349 plural, `TestBudgetFindingsRequests` world, R74 rollout reason and the ≥ 60 s check, the 2 px reservation at 720 px) | Stay parked. A task that edits the same lines may fix one and says so in its report | — |
| E3 and E8 at the program level | Delivered by 3.3; program file and kgwctl notice at the close, with the user's go | 71a, 73 |

## Root cause (if known)
n/a (planned work).

## Acceptance tests
- Task 56: one test per freshness kind through `/health`; `TestEvidenceClipsLongStatus`.
- Task 57: route and listener cases; the typo'd group; the kgateway-specific field; two candidates, not attributed; `TestSpecificCodeReplacesConditionFalse`; the additive test with only the listed exceptions.
- Task 58: one group → fix, two → none; grants for a backendRef and a certificateRef; `TestNoFixWithoutAttribution`; no fix naming a hidden object; YAML round-trip.
- Task 59: 2.3 and 2.4 reasons; parsed and unparseable messages; the hint at 9:59 and 10:01.
- Task 60: golden fixtures with provenance, `TestRuleSetWithoutPolicyTargeterRaisesNo016`, `TestDownstreamRuleSet`.
- Task 61: missing target, missing section, empty selector, partial and all missing, the verifier, not verified, scope unknown, 29 s and 31 s, `TestNewerThanTestedUsesComputed016`.
- Task 62: both sides named; no other side; the `ListenersNotValid` Gateway.
- Task 63: threshold edges; 4:59 and 5:01; 005 instead.
- Task 64: exclusive membership over the full family table; group paging; restricted group rows; `findings-groups.json` and `…-restricted.json` goldens.
- Task 65: prefilter, reconnect, 256 KiB cap, event cap, 10 failures, toggle off, Role missing, the namespaced Role test.
- Task 66: captured lines per tag, `TestUnknownNameKeepsOnlyClass`, `TestLogSecretLeavesNoTrace`.
- Task 67: 001 `since` after an engine restart; 019 top frame; 021 deployer class; dependencies per §14.
- Task 68: every `health.Code*` has an anchor.
- Task 69: the component and page tests of the plan.
- Task 71a: a black-box test that imports only `pkg/` packages and runs the one-shot path; static source staleness at max age ± 1 s; `pkg/app` tests and goldens unchanged.
- Task 71: every §16.3 gate in `bench/3.3.txt`.
- Task 72: `TestAnalyzer` and `TestControllerLogSignals` on kind (v2.3.11, v2.4.7); `web/e2e/diagnostics.spec.ts`.

## Scope
In:
- `pkg/health`: `freshness.go`, `refs.go`, `rules.go`, `targets.go`, `conflicts.go`, evidence, 002 and 003 changes, 001/006/019/021 log evidence.
- `pkg/diagnose`: `fix.go`, the four families.
- `pkg/kinds/catalog.go`; `pkg/computed` (`targets.go`, `logs.go`, `kgw23`, `kgw24`, testdata); `pkg/ctrllogs`; `pkg/ctrlmetrics` (status syncs; the static source).
- `pkg/setup` (new, D-053) and `pkg/app`; `pkg/api` (findings, grouped view, fixes, goldens, additive exceptions).
- `internal/boundary` (`pkg/ctrllogs/stream.go`, `pkg/setup/setup.go`); `examples/downstream`; the chart Role test.
- `docs/findings.md`, `computed-rules.md`, README upgrade notes.
- `web/packages/shell` (`SuggestedFix`, `EvidenceView`, findings pages, types).
- `test/e2e` (`analyzer.yaml`, `diagnostics_test.go`, `api_test.go`), `web/e2e`, `.github/workflows/e2e.yaml` (the log row).

Out:
- 013 explanations 2 and 3, and the port fix: they need Services (3.4).
- 023, 024, the 2.5 rule set and the `TargetNotFound` status path (3.4).
- R57, R59. Proxy logs, admin ports, `/config_dump`.
- ReferenceGrant evaluation or backend resolution (Phase 2 anti-goal).
- Any fix derived from translation logic.
- A long-running use of krp `pkg/envoy/source/kube` (E4, below).
- Gate changes of any kind.
- Moving analyzer code into new packages for E3.

## Program
- **E3** (kgwctl Phase 4: full `check`, `analyze -f`). In progress in 3.3. The outcome is that an external module can import the analyzer and run it over a cache snapshot. The one-shot test of Task 71a is its acceptance test.
- **E8** (#13, kgwctl Phase 2: `check` MVP). Added to 3.3 by the user on 2026-11-04 as Task 71a (D-053). #13 closes with the commit at the 3.3 close.
- **E4** (#15, UI Phase 9). 3.3 does not consume it. The long-running backend must not use krp `pkg/envoy/source/kube` until krp#25 is fixed (krp Phase 5). `source.File` and `source.Admin` are unaffected.
- Waiting on: none.

## Review Focus
These are five input classes that no 3.3 task tests and that are most likely to bite users. Each owning task adds the test, or the reviewer checks the behaviour.

| # | Input class | Why it bites | Expected | Owner |
|---|---|---|---|---|
| 1 | Refs that rely on defaults: no `group`, `group: ""`, no `kind`, no `namespace`, or a port set only by the controller default | Most backendRefs omit group and kind. The grant's `to` and the Missing backend identity must treat omitted and explicit core Service as the same | A grant with `group: ""`, `kind: Service`. One Missing backend group for omitted and explicit core refs. A same-namespace ref is never a 014 candidate **(inferred)** | 57, 58, 64 |
| 2 | Discovery that fails in part (an aggregated API down), or a CRD installed or removed while the UI runs | "No kind in group, check the group" plus a corrected-group fix on a real group would send users to break working config | A kind whose served state is unknown gives explanation 4 (the controller's message), never explanation 1 and never a fix **(inferred)** | 57, 58 |
| 3 | Listener hostnames for 017: a wildcard `*.example.com` beside `a.example.com`, an empty hostname, and a ListenerSet in another namespace | Spec-level distinctness is easy to get wrong for wildcards and empty hostnames | A hostname conflict's other side has the exact same hostname on the same port; a wildcard and its subdomains are distinct, and empty matches only empty. A ListenerSet in another namespace is named as the other side (R94) | 62 |
| 4 | The same NACK logged by two controller replicas, and a replica replaced mid-stream | Duplicate events can double evidence or reset `since`; a stream on a deleted Pod can leak | One derived event per reject per Gateway and type, whichever replica logged it. `since` keeps the earliest. A stream ends when its Pod goes **(inferred)** | 65, 67 |
| 5 | A policy applied before its target, as GitOps sync waves do, with the target arriving during the grace or the 30 s hold | A computed 016 that flashes during every rollout teaches users to ignore it | No 016 when the target arrives before grace + 30 s; the hold restarts when a target disappears again **(inferred)** | 61 |

## Plan / design impact
- **D-053** (accepted by the user on 2026-11-04): Task 71a, the config helper and the static metrics source. It answers #13's open question: waiting stays with the caller.
- **Plan edits in this commit:** Task 71a; the Sub-phases row; the File Structure and the import allowlist gain `pkg/setup/setup.go` and `pkg/ctrlmetrics/static.go`; Task 73 reviews against D-053.
- Task 73 changes `design.md` only where the code deviates.
- Approval of the plan covers the ⚠ guardrail tasks 57, 59, 60, 61, 65 and 71a within their file lists (user, 2026-11-01; Task 71a by D-053). A task that needs more files stops and asks.

Plan points to settle in the owning task:
1. Task 61 cites `test/e2e/api_test.go:189-195`. `tpMissingTarget` is now at `api_test.go:95-101`, called at line 196.
2. Task 65 creates `role-logs.yaml` and `TestLogRoleOnlyInListedNamespaces`. 3.2 already ships `controller-logs-role.yaml` and `TestControllerLogsRoleIsOptInAndNamespaced`. Task 65 extends those.
3. Task 56 adds a 1 KiB `Clip` in `pkg/health`. `computed.ClipDisplay` already cuts at 256 B with `…[truncated]`. The task picks one marker for the user-visible text.
4. Global Constraints still say OS memory ≤ 256 MiB. D-043 made it the median of five ≤ 288 MiB. This brief uses 288.
5. E3 `analyze -f` runs over objects from files, with no cluster. The design does not say what served kinds, catalog, access states and install a file-built snapshot has. That decides 013 explanation 1 and the 016 rule. The user decides before Task 71a whether 3.3 covers it.
6. Where the 2.3/2.4 "withheld" evidence from the deferral line shows. §8.9 ties it to what 024 gives on 2.5, and 024 is 3.4.

Reminders for the user (not blocking): the open item in design §25 (filing the reject-status draft), and the upstream scope-endpoint issue (carry-over P3). Filing both stays the user's call.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] standard gates pass; the 3.3 budgets above hold (`bench/3.3.txt`, `3.3-ci.txt`), or the numbers went to the user
- [ ] kind e2e passes on v2.3.11 and v2.4.7, plus the log row (`--diagnostics-controller-logs`); `/ui-loop` on finding detail with a fix and the grouped view
- [ ] wording pass recorded in `phase-3.3-wording.md`
- [ ] the `(inferred)` outputs below confirmed by the user, or recorded in `decisions.md`, before their task
- [ ] Codex review of `git diff 79a5307..HEAD` + triage (`reviews/3.3-codex.md`)
- [ ] phase-3.3-complete.md written (bench table vs 3.2); phase-status.md and roadmap.md updated; README upgrade notes (013–018, 002 replaced for those reasons, 003 kinds, the log toggle, `pkg/setup`)
- [ ] #13 closed with the commit; E3 and E8 status for the program file and a `cross-repo` notice to kgwctl drafted, posted with the user's go

## Inferred outputs to confirm
1. An object that matches two freshness kinds takes the first in §10.1 order (Task 56).
2. `RefNotPermitted` on a ref whose group is not served: 014, the wrong-group explanation, no grant, the corrected-group fix when one group has the kind (Tasks 57, 58).
3. 015's 10-minute hint counts from the finding's `since`, the condition's `lastTransitionTime` (Task 59).
4. Every 003 warning of the install counts as explained by its 018 and joins the "Status not progressing" group (Tasks 63, 64).
5. An unattributed 013 goes to the "Other" group (Task 64).
6. A bad `view=groups` filter value gives 400 (Task 64).
7. The 2.3/2.4 "withheld" evidence shows on the Gateway's 001 or Data plane section (Task 67).
8. A one-shot caller reaches suggested fixes through exported packages, without the HTTP server (Task 71a).
9. Defaulted refs: the grant uses `group: ""`, `kind: Service`; omitted and explicit core refs share one Missing backend group; a same-namespace ref is never a 014 candidate (Review Focus 1).
10. A kind whose served state is unknown gives the controller's message, never "check the group" and never a fix (Review Focus 2).
11. Hostname match for 017: exact hostname on the same port, and empty matches only empty; a ListenerSet in another namespace is named (Review Focus 3; revised by R94).
12. One derived event per reject per Gateway and type across replicas; `since` keeps the earliest; a stream ends with its Pod (Review Focus 4).
13. No 016 when the target arrives before grace + 30 s; the hold restarts when a target disappears again (Review Focus 5).
