# Sub-phase 3.4 — Backends and kgateway 2.5

Kind: planned
Base: 8342a38ca9d81254585deae7612979fe92c1a2bb
Issues: [#17](https://github.com/DuncanDoyle/kgateway-ui/issues/17), [#20](https://github.com/DuncanDoyle/kgateway-ui/issues/20) (E2, D-056) | Plan steps: implementation-plan.md Tasks 74–87, 78a, 78b; design §7.3, §7.6, §8.6, §11, §16.3, §19, §21; D-020, D-031, D-033, D-034, D-044, D-046, D-047, D-054, D-055, D-056

## Problem / goal
A route can be Accepted with `ResolvedRefs=True` while its Service has no ready endpoints. Every request then gets a 503, and the UI shows green. A 013 on a Service ref quotes the controller, even when the Service is missing or lacks the named port. kgateway 2.5.0 is GA (2026-11-02), and the UI runs it on the 2.4 rule set with a "newer than tested" note.

3.4 adds:
- Service and EndpointSlice watches, cut down at the informer, behind the `endpoints` capability (D-031);
- KGWUI-023 and the "Empty backend" group (§11);
- 013 explanations 2 and 3, and the one-port fix (§7.3, §7.7);
- a platform-neutral analyzer test (§19);
- kgateway 2.5 support: the `kgw25` rule set with 016 from status, KGWUI-024, `kgateway_build_info`, and a 2.5 e2e row (D-033);
- two kgwctl requests (D-056): descriptors for DirectResponse, GatewayExtension and ReferenceGrant (#17), and a way to wait for the cache's goroutines (#20).

2.5.0 GA is out, so Tasks 83–86 run in this sub-phase. kgateway `v2.5.0` pins Gateway API `v1.7.0` (`go.mod` at the tag).

## Expected behaviour

### Services and EndpointSlices (Task 75)

| Input / action | Expected |
|---|---|
| `diagnostics.endpoints` on (the default), both watches list and watch | `/meta` capability `endpoints` is `on` |
| Toggle off | No Service or EndpointSlice watch starts. 023 is "not available", reason `off`. No KGWUI-008; the install is not unknown |
| RBAC denies Services or EndpointSlices | 023 is "not available", reason `denied`. No KGWUI-008; the install is not unknown |
| A watch errors after it worked, then recovers | 023 is `failing` while it errors, and runs again after recovery |
| An EndpointSlice endpoint with `conditions.ready` unset | Counts as ready |

### KGWUI-023 (Task 76)

| Input / action | Expected |
|---|---|
| An HTTPRoute backendRef to core Service `shop/cart` port 8080, weight unset, the parent `ResolvedRefs=True` from this install, and no ready endpoint for that port for 30 s | 023 warning on the Service: "Requests to this backend get 503". One finding per Service port. The routes are the group's affected list |
| The same at 29 s | No 023 |
| The same at 31 s | 023 |
| Ready drops to zero, then one endpoint turns ready at 20 s | No 023; the hold restarts at the next drop |
| Endpoints with ready unset | No 023 |
| `type: ExternalName` | No 023 |
| Weight 0 | No 023 |
| Parent `ResolvedRefs=False` | No 023 (013 covers it) |
| A delegation ref, a mirror target, a `Backend` kind, a Hostname ref | No 023 |
| Two routes to the same Service port | One 023; both routes in the group's affected list |
| Membership: the proxy's view of the cluster | Shown as evidence only, from one Ready proxy per Gateway, cached 10 s. It never decides whether 023 fires |
| `endpoints` capability not `on` | 023 "not available" with the reason; never a 023 |

### 013 with Service data, and the port fix (Task 77)

| Input / action | Expected |
|---|---|
| `ResolvedRefs=False/BackendNotFound`, core Service ref `shop/cart`, Services watched, no such Service | 013: "Service `shop/cart` does not exist." |
| The Service exists, ref port 8080, Service ports 80 and 443 | 013: "Service `shop/cart` has no port 8080 (ports: 80, 443)." |
| The same with a one-port Service (port 80) | The suggested fix is the ref with `port: 80`, at its `spec.rules[i].backendRefs[j]` path |
| A two-port Service | No fix; next steps only |
| The viewer cannot see the Service | The explanation and fix are withheld as `evidenceRestricted`, as for other dependencies |
| Services not watched (`endpoints` off, denied or failing) | Explanation 4, the controller's message, as in 3.3 |
| A group that serves no kind `Service` | Explanation 1 still wins (3.3) |

### Platform neutrality (Task 78)

| Input / action | Expected |
|---|---|
| A snapshot with no Pods, Events, Services, EndpointSlices or CRD metadata | Process findings "not observed"; 023 "not available"; every other code equal to the run on the full snapshot |

### kgwctl requests (Tasks 78a, 78b; D-056)

| Input / action | Expected |
|---|---|
| A cluster serving DirectResponse, GatewayExtension (`gateway.kgateway.dev/v1alpha1`) and ReferenceGrant | All three are listed and described through `kinds`, the cache and the API, like any other kind. API v1 kind lists gain them at the end, so existing order is unchanged |
| A cluster that does not serve one of them | That kind is absent, as for any unserved kind. No error |
| Categories | DirectResponse `policy`, GatewayExtension `parameters`, ReferenceGrant a new `referencegrant` category |
| ReferenceGrants in the cache | Listed and shown. Nothing reads them to decide resolution: 013 and 014 are unchanged (Phase 2 anti-goal) |
| `cache.Start(ctx)`, cancel `ctx`, then `Wait` | `Wait` returns once every goroutine `Start` launched has exited. A test with a goroutine-leak check passes |
| `Wait` before `Start`, or called twice | Returns at once |

### kgateway 2.5 (Tasks 83–86)

| Input / action | Expected |
|---|---|
| A 2.5 controller | Selects the `kgw25` rule set. No "newer than tested" note |
| `kgw25`: a policy whose ancestor reason is `TargetNotFound` | 016 error from status. No computed 016 on this rule set (D-020 rule 5) |
| `kgw24` and `kgw23` | Unchanged: computed 016 as in 3.3 |
| `kgateway_xds_snapshot_perclient_withheld_clients{state="warm_withheld"} > 0` | 024 error: "Proxies get no config updates, because referenced clusters are missing" |
| `bounded_publishes_total{mode=~"flip_release\|warm_release\|carry_expired"}` increased in the last 15 min (per Pod UID) | 024 warning: "Some routes fail: config was released before every cluster was ready" |
| The family absent (2.3, 2.4) | 024 "not available on this version" (`AvailNotOnVersion`) in "What was checked". The install is healthy, never unknown |
| `kgateway_build_info` present | The controller version comes from it, over the image tag; when they disagree, the evidence shows both. Absent: the image tag, as today |
| The 2.5 e2e row (`v2.5.0`, Gateway API `v1.7.0`) | The full suite passes, with 016 from `TargetNotFound` and a withheld-config case for 024 |
| The TCPRoute fixture | On `gateway.networking.k8s.io/v1` when every row serves it; otherwise one fixture per served version |

If Task 83 finds the GA family names, labels, `TargetNotFound`, `EnableStatsRoute` default or the Gateway API pin differ from the design, it stops and reports.

## Budgets (§16.3)

No gate changes without the user. A broken gate stops the work, and the numbers go to the user. Start from `bench/3.3.txt` (reference machine, D-044). Task 81 measures the Large tier with 5,000 Services and 6,000 EndpointSlices, 5 % empty in the worst case, and re-seeds the measurement world with them (R57).

| Measure | 3.4 budget at Large | 3.3 reference |
|---|---|---|
| Warm recompute median, consistent / contradicting / worst | ≤ 150 ms | 35.5 / 46.7 / 51.3 ms |
| Cold recompute median | ≤ 1 s | 52 / 56 / 62 ms |
| Retained heap over 2.1, measurement world | ≤ 40 MiB (D-054), and ≤ +24 MiB over 3.1 | +34.6 MiB ⚠ |
| 3.2 watches' retained state | ≤ 1,600,000 B (D-046) | 1,450,520 B |
| 3.4 watches' retained state (Services, EndpointSlices, the three #17 kinds) | reported | — |
| Dense: retained / cold / per warm | 176 / 246 / 185 MiB | 160 / 207 / 166 MiB |
| Analyzer stage, §16.3 scope: warm / memoized / new scrape | ≤ 20 / 2 / 2 ms | 6.00 / 0.20 / 1.28 ms |
| Stream CPU, annotation-only / spec-edit | ≤ 15 % / ≤ 25 % | 9.5 % / 13.9 % |
| Edit-driven recomputes, worst | ≤ 1/s (D-047) | 0.98/s ⚠ |
| OS memory, median of five | ≤ 288 MiB (D-043) | 209.5 MiB |
| `…/findings`, `…/diagnostics` p95, sequential / 20 clients | < 50 / < 100 ms | 0.52 / 1.31; 0.85 / 5.01 ms |

Retained heap has 5.4 MiB of headroom, and the new watches add to it. Task 81 stops the work when any gate is within 2 MiB, and the numbers go to the user before more code lands. EndpointSlices churn: their events reach a recompute only through the scheduler's debounce (D-019), so the edit-driven rate must stay ≤ 1/s with slice churn in the stream.

## Carry-forwards from 3.3

| Item | Outcome in 3.4 | Task |
|---|---|---|
| R57: Services and EndpointSlices in the measurement world | Re-seeded with the 3.4 watches | 81 |
| R96: a sync-only scrape publishes a new model (0.10/s idle) | Measured with the 3.4 inputs; a fix only if it costs a gate, else stays (user, 2026-11-05) | 81 |
| Task 77 port fix and Service explanations; 023 and 024 entries in `docs/findings.md` | In 3.4 | 76, 77, 85 |
| R59: proxy Pod ownership by UID needs a ReplicaSet watch | Not in Tasks 74–87. Goes to the phase review (user, 2026-11-06) | — |
| Program block in `phase-status.md` | Updated in this commit | 74 |

## Root cause (if known)
n/a (planned work).

## Acceptance tests
- Task 75: transform shape; unset `ready`; toggle off; denied (no KGWUI-008, install not unknown).
- Task 76: zero ready; unset ready; `ExternalName`; weight 0; `ResolvedRefs=False`; delegation and mirror refs; 29 s and 31 s; endpoints off.
- Task 77: Service missing; port missing with the list; one-port fix; two-port no fix; hidden Service → `evidenceRestricted`.
- Task 78: `TestAnalyzerWithoutPodsEventsServicesOrCRDs`.
- Task 78a: the three kinds through `/api` on a fake served-kind set; absent when not served; v1 kind-list order unchanged (goldens); 013 and 014 goldens unchanged with ReferenceGrants present.
- Task 78b: Start, cancel, `Wait`, then a goroutine-leak check; `Wait` before `Start` returns.
- Task 79: component tests per evidence state, both themes.
- Task 81: every §16.3 gate in `bench/3.4.txt`.
- Task 82: `backends.yaml` on kind v2.3.11 and v2.4.7: a backend scaled to zero gives 023 after 30 s; the wrong-port fixture gives explanation 3 and a fix; `/ui-loop` on 023.
- Tasks 83–86: GA captures; `kgw25` 016 from status; 024 per metric case; not-on-version on 2.3 and 2.4; the 2.5 e2e row.

## Scope
In:
- `pkg/cache` (`services.go`, the three kinds, `Wait`); `pkg/kinds`; `pkg/app` (toggle, capability).
- `pkg/health` (`endpoints.go`, `withheld.go`, `refs.go`, `targets.go`); `pkg/diagnose` (the "Empty backend" family, the port fix).
- `pkg/proxystats` (membership on demand); `pkg/ctrlmetrics` (per-client families, build info); `pkg/install`.
- `pkg/computed/kgw25`, rule-set tables, testdata; `pkg/engine` (neutral test).
- `pkg/api` (evidence, goldens); `internal/boundary` (`pkg/cache/services.go`).
- `deploy/helm` (ClusterRole: Services, EndpointSlices, the three kinds; the toggle; chart tests).
- `web/packages/shell` (`EvidenceView`, not-on-version state, wording).
- `test/e2e`, `web/e2e`, `.github/workflows/e2e.yaml` (2.5 row).
- `docs/findings.md`, README (upgrade notes, Supported versions).

Out:
- ReferenceGrant evaluation, or any backend resolution (Phase 2 anti-goal). Service data never decides whether a ref resolves.
- 023 for delegation refs, mirrors, `Backend` kinds, ServiceEntries or Hostname refs (Phase 4).
- #18 (list-only snapshot) and #19 (namespace-scoped listing): Phase 4 planning (D-056).
- R59, the ReplicaSet watch.
- A long-running use of xdsview `source/kube` (E4 is UI Phase 9).
- Gate changes of any kind.

## Program
- **E2** (kgwctl, available in `v0.3.0` with caveats). #17 and #20 ship in 3.4 (D-056). #18 and #19 go to Phase 4 planning; their issues get a comment saying so at the close.
- **E3, E8**: delivered in `v0.3.0` (kgwctl#3). 3.4 adds 023 and 024 to the analyzer; the contract row in the program file gains them at the close.
- **E4, E5** (#21, #15): xdsview `v0.3.0` (renamed from krp). For UI Phase 9; 3.4 does not consume them.
- Waiting on: none.
- At the close: propose tag `v0.4.0` (new exported kinds and `cache.Wait`), update the program file, and notify kgwctl with a `cross-repo` issue, all with the user's go.

## Review Focus
These are five input classes that no 3.4 task tests and that are most likely to bite users. Each owning task adds the test, or the reviewer checks the behaviour.

| # | Input class | Why it bites | Expected | Owner |
|---|---|---|---|---|
| 1 | A backendRef port that names a Service port by number, where the EndpointSlice port is named and the target port differs (`port: 80`, `targetPort: http` → 8080) | Slices carry the target port and the Service port's name, not the Service port number. Matching by number gives a false 023 on every named-port Service | The ref's port maps to the Service port, then to the slice port by name (unnamed: the only port). No 023 while a ready endpoint serves it | 75, 76 |
| 2 | A Service with several EndpointSlices, some for other address types (IPv4 and IPv6), or a slice briefly orphaned during a rollout | Counting one slice, or a slice from the wrong address family, gives a false zero | Ready counts sum over every slice labelled for the Service, per port | 75, 76 |
| 3 | A deliberate scale-to-zero (KEDA, nightly) | A warning that never clears trains users to ignore it | 023 stays a warning; it clears within one recompute after an endpoint turns ready | 76 |
| 4 | A 2.5 controller whose rule set says `TargetNotFound`, while another install in the same UI still runs 2.4 | Per-install rule sets must not leak: one install's status path must not turn off the other's computed rule | Each install uses its own rule set | 84 |
| 5 | A cluster without the ReferenceGrant CRD, or with only `v1beta1` served | A hard-coded version breaks listing on older bundles | The kind lists on whichever served version the catalog prefers; absent when not served | 78a |

## Plan / design impact
- **D-056** (user, 2026-11-06): #17 and #20 join 3.4 as Tasks 78a and 78b; #18 and #19 go to Phase 4 planning.
- **Plan edits in this commit:** Tasks 78a and 78b; the Sub-phases row; the File Structure (`pkg/kinds/upstream.go`, `pkg/cache/cache.go`); Task 87 reviews against D-056.
- Task 87 changes `design.md` only where the code deviates, and lists the three kinds where the design lists modelled kinds.
- Approval of the plan covers the ⚠ guardrail tasks 75 and 84 within their file lists (user, 2026-11-01). Tasks 78a and 78b are covered by D-056 within their file lists. A task that needs more files stops and asks.

Plan points to settle in the owning task:
1. Task 75 names `pkg/cache/services.go` as one new allowlisted file. The ClusterRole also needs `list`/`watch` on the three #17 kinds; Task 78a adds those rules.
2. Task 86 says Gateway API `v1.7.x`; the GA tag pins `v1.7.0`. Task 83 confirms.
3. Task 85 changes `pkg/install/upstream.go`. If `kgateway_build_info` disagrees with the image tag, the evidence shows both.

Reminders for the user (not blocking): the open item in design §25 (filing the reject-status draft), and the upstream scope-endpoint issue (carry-over P3). Filing both stays the user's call.

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] standard gates pass; the 3.4 budgets above hold (`bench/3.4.txt`), or the numbers went to the user
- [ ] kind e2e passes on v2.3.11, v2.4.7 and v2.5.0; `/ui-loop` on 023
- [ ] wording pass recorded in `phase-3.4-wording.md`
- [x] the `(inferred)` outputs below confirmed by the user (2026-11-06)
- [ ] Codex review of `git diff 8342a38..HEAD` + triage (`reviews/3.4-codex.md`)
- [ ] phase-3.4-complete.md written (bench table vs 3.3); phase-status.md and roadmap.md updated; README upgrade notes (023, Services and EndpointSlices read, the `endpoints` toggle, the three kinds, `cache.Wait`, 2.5 support)
- [ ] #17 and #20 closed with the commit; #18 and #19 commented; tag, program file and kgwctl notice proposed to the user

## Inferred outputs to confirm
All eight confirmed by the user on 2026-11-06:
1. `diagnostics.endpoints` is on by default (Task 75). The watches stay cluster-wide: a watch cannot filter by "referenced", and the cut-down objects cost about 2 MiB at Large.
2. Categories: DirectResponse `policy`, GatewayExtension `parameters`, ReferenceGrant a new `referencegrant` category (Task 78a).
3. `Wait` before `Start`, or a second `Wait`, returns at once (Task 78b).
4. `kgateway_build_info` wins over the image tag for the controller version; when they disagree, the evidence shows both (Task 85).
5. The 3.4 watches' retained state is reported, with no gate of its own; the total retained-heap gate covers it (Task 81).
6. A backendRef port maps to the Service port, then to the slice port by name (Review Focus 1).
7. Ready counts sum over every slice for the Service, per port (Review Focus 2).
8. R59 goes to the phase review, not 3.4.
