# Sub-phase 2.5 — Stabilization (Phase 2 review findings)

Kind: stabilization
Base: 29b8792191b48e4e3d83000f063be44eba8d687a
Branch: duncandoyle/phase2Stabilize
Issues: n/a | Plan steps: n/a; findings 1–7 of `reviews/phase-codex.md` (accepted for 2.5); design §4.2, §6.2, §7.4 (rules 4, 4a, 4c, 5, 6, 7, 9), §7.6, §8.6, §9.4; computed-rules §3.3, §9.3; D-023 (amended), D-024, D-025

## Problem / goal
The Phase 2 cross-model review (`reviews/phase-codex.md`) found seven defects that break behaviour the design already states. Sub-phase 2.5 fixes them, with no new features, no wire format change and no new dependencies. Finding 8 (stable finding identity) is deferred to Phase 3 planning.

1. **Delegation tab freshness.** When route-detail polling succeeds and `GET …/delegation` fails, the tab shows the error notice but its retained rows still read checked, with healthy icons.
2. **Install-local orphan.** Two installs share a controller name. Install A's wildcard parent drops route C everywhere; install B serves C through its parent Q. B's status entry on C suppresses A's orphan KGWUI-011.
3. **`listener-unknown` under a hidden parent.** A route has two accepted parentRefs that view A cannot place. Hiding parent B removes the flag that visible parent A establishes, so `parent=A&listener=_unknown` includes the route while `state=listener-unknown` excludes it.
4. **Edge cap versus unknown scope.** With an unknown discovery scope, a ref the edge cap cut reads `too-large`, while every other ref reads `scope-unknown`.
5. **`since` of a computed-only candidate.** A computed-only child (declared or no-parent) with a missing entry gets KGWUI-012. When a hidden Namespace restricts its evidence, `/health` drops `since` instead of substituting the child's first-seen time.
6. **Plan drift.** `implementation-plan.md` Task 44 (lines 1535–1548) still prescribes restricting listener counts and Gateway `routes` under hidden dependencies, which the D-023 amendment replaced.
7. **TypeScript golden guard.** `types.golden.test.ts` checks keys and nesting only, so a golden whose values contradict `types.ts` (scalar type, enum value, array versus object) passes.

## Expected behaviour
Each table states observable behaviour at the boundary where it is tested. Rows marked **(inferred; D-026)** are not stated by the design; confirm them with the user, or record them in `decisions.md`, before implementing that item.

### Q1 — Delegation tab keeps freshness (design §8.6, §9.4)
| Input / action | Expected |
|---|---|
| Route detail and `GET …/delegation` both fresh | rows show their real states, computed badges and icons (unchanged) |
| Route detail polls succeed; a `GET …/delegation` poll fails after data was shown | the tab keeps its rows and shows `PageNotice`; every row state icon and every computed badge in the tab reads Unknown; no row shows agreement or a healthy icon **(inferred: matches the 2.2 detail pages, `details.test.tsx` "a failed poll turns states unknown")** |
| `GET …/delegation` returns a body whose `collectedAt` is past the stale threshold, route detail fresh | every state and computed badge in the tab reads Unknown (§8.6 Stale) |
| The next `GET …/delegation` poll succeeds with fresh data | the notice goes and the real states return **(inferred, as on the 2.2 detail pages)** |

### Q2 — The orphan conclusion is install-local (design §6.2 orphan trigger, §4.2 last ownership rule; rules §9.3)
| Input / action | Expected |
|---|---|
| Installs A and B share a controller name. Route C (in both scopes) has empty `spec.parentRefs`; A's owned parent P selects C only by a `wildcard` ref and drops it in every context, traversal complete; C has an `Accepted` entry from the shared controller naming B's owned parent Q | A's `/health`: one KGWUI-011 on C with the orphan detail, a warning 30 s after `since`, never pending. B's `/health`: no orphan KGWUI-011 on C |
| Same, but C's entry names A's owned parent P (an eligible entry of A, §4.2) | A's `/health`: no orphan KGWUI-011 on C (unchanged) |
| Same as row 1, under a caller who may view C but not P | A's `/health` still lists the finding, not `evidenceRestricted` (D-024; unchanged) |

### Q3 — A visible parent keeps `listener-unknown` (design §6.1, §7.4 rules 4, 4a, 6)
| Input / action | Expected |
|---|---|
| Route R has two accepted parentRefs, A and B, that view A cannot place (for example both ListenerSets without a current `Accepted`). Caller may view R and the dependencies of A's unknown outcome, not those of B's | R's row carries `listener-unknown`; `state=listener-unknown` includes R and counts it in `total`; `parent=A&listener=_unknown` includes R; `parent=B&listener=_unknown` does not |
| Same, caller may view neither A's nor B's dependencies | R's row has no `listener-unknown`; `state=listener-unknown` excludes R and `total` does not count it (rule 4a) |
| Same, `AllowAll` | flag present, both filters include R (unchanged) |
| `deniedInInstall` in any of the above | identical across the three callers' views of the same install, independent of filters (rule 2; unchanged) |

### Q4 — `scope-unknown` wins over `too-large` (rules §3.3 precedence; D-024 decision 1; D-025)
| Input / action | Expected |
|---|---|
| Install with an unknown discovery scope; delegation selection exceeds the edge cap (the cap may be lowered with `--delegation-edge-cap` for the test); caller `AllowAll` | `GET …/delegation` and `RouteDetail` for every parent, including refs the cap cut: each ref's `computed` is `unknown` with reason `scope-unknown`; no ref, row or context carries `too-large` |
| Same, a caller who cannot view the selection scope of a cut ref | that ref carries the same `computed` as an uncut selection-restricted ref under unknown scope, so the response does not tell whether the cap cut it **(inferred, from rule 4c and §3.3)** |
| Known discovery scope, cap exceeded | cut refs read `unknown` with reason `too-large` (unchanged, D-025) |

### Q5 — Restricted evidence keeps a `since` for computed-only children (design §7.4 rules 7 and 9; rules §9.3 step 4)
| Input / action | Expected |
|---|---|
| Child C is a computed-only candidate (a declared candidate of owned parent P, or a no-parent route P selects) that view B computes as attached; C has no entry from P, and the missing-entry check matures. P's root is admitted through a `Selector` over Namespace N. Caller may view C, not N | `/health` lists KGWUI-012 on C with `evidenceRestricted: true`, the detail "Details depend on objects you cannot view", and `since` equal to C's current-generation first-seen time |
| Same, `AllowAll` | KGWUI-012 on C with its full detail and `since` equal to the check's window start (unchanged) |
| An owned child with the same restricted evidence | `since` equals the child's first-seen time (unchanged, `TestKGWUI012ResourceSince`) |

### Q6 — The plan matches the amended D-023 (design §7.4 rule 5)
| Input / action | Expected |
|---|---|
| Read `implementation-plan.md` Task 44 | no unmarked text prescribes omitting `computedRoutes` or a Gateway or ListenerSet row's `routes` when a dependency is hidden, nor a test for restricted counts or restricted listener-count evidence. The superseded text either matches design §7.4 rule 5 or is marked superseded with a pointer to the D-023 amendment |
| Grep the plan for `omitted when any dependency is hidden` and `TestHiddenMemberRestrictsCounts` | no hit outside a superseded marker |

### Q7 — The golden guard checks values (design §7.6)
| Input / action | Expected |
|---|---|
| Every golden in `pkg/api/testdata/golden` against the exported API types | the guard passes (unchanged) |
| A golden value whose scalar type contradicts `types.ts` (a number where a string is declared, or the reverse) | the guard fails and names the JSON path |
| A value outside a declared string-literal union (for example `Computed.state: "bogus"`) | the guard fails and names the path |
| An array where an object is declared, or an object where an array is declared | the guard fails and names the path |
| A field's type changes in `types.ts` without the guard changing | `npm run typecheck` or `npm test` fails **(inferred: the guard's existing contract for keys, extended to values)** |

## Root cause (if known)
Quoted from `reviews/phase-codex.md`; the implementation is not prescribed.
1. `DelegationTab.tsx:617` "retains and renders `l.data`, but never evaluates its `collectedAt` or supplies a stale context. … The enclosing `DetailFrame` tracks a different request."
2. `pkg/computed/check.go:1201` "treats any entry with a matching `controllerName` as disqualifying; `check.go:1144` uses that result for the orphan predicate."
3. `pkg/explore/rows.go:1180` "unions dependencies from every unknown parent into one set; `query.go:372` requires that entire union to be visible."
4. `pkg/api/details.go:600` "unconditionally labels a cut ref `too-large`. Selection runs before the scope-unknown traversal shortcut, so an unknown-scope install can still populate `TooLarge`."
5. `pkg/health/computed.go:49` "populates `ResourceSince` only through `ownedResources`, which excludes declared and no-parent candidates."
6. `implementation-plan.md:1535` and lines 1545–1548 predate the D-023 amendment.
7. `types.golden.test.ts:485`: "`Spec<T>` retains only keys and optionality."

## Acceptance tests
Each item gets a regression test that fails on the base for the reason above and passes after the fix, at the highest practical boundary. Report the failing and passing runs per item.

| Item | Boundary | Test home |
|---|---|---|
| Q1 | SPA via vitest: the route page with the Delegation tab, fetch served per path, fake timers | `web/packages/shell/src/pages/explore/delegation.test.tsx` |
| Q2 | HTTP API: two installs that share a controller name, `GET /api/v1/installs/{id}/health` for each, through the Authorizer | `pkg/api/computed_test.go` or `pkg/api/delegation_test.go` |
| Q3 | HTTP API: `GET …/routes` with `state=listener-unknown` and `parent=…&listener=_unknown`, through an Authorizer that denies one parent's dependencies, and under `AllowAll` | `pkg/api/computed_test.go` |
| Q4 | HTTP API: `GET …/routes/…/delegation` and `GET …/routes/…` on an unknown-scope install with a low edge cap, under `AllowAll` and a restricting Authorizer | `pkg/api/delegation_test.go` |
| Q5 | HTTP API: `GET …/health` through an Authorizer that hides the Selector Namespace, and under `AllowAll` | `pkg/api/computed_test.go` |
| Q6 | Docs check: the grep of the Q6 table (matches on the base, none after) and a read of Task 44 against design §7.4 rule 5 | n/a (recorded in `phase-2.5-complete.md`) |
| Q7 | vitest: the guard run against the real goldens, plus mutated copies built in the test for each mismatch class | `web/packages/shell/src/api/types.golden.test.ts` |

Helper-level tests in `pkg/computed`, `pkg/explore` or `pkg/health` may supplement these; they do not replace them.

## Scope
In:
- Q1: `web/packages/shell/src/pages/explore/route/DelegationTab.tsx` and the shared freshness code it needs; `web/packages/shell/src/pages/explore/delegation.test.tsx`.
- Q2: `pkg/computed/check.go` and its tests; the API test.
- Q3: `pkg/explore/rows.go`, `pkg/explore/query.go` and their tests; the API test.
- Q4: `pkg/api/details.go`, and the `pkg/computed` or `pkg/api` code that decides a cut ref's reason; the API test.
- Q5: `pkg/health/computed.go` and its tests; the API test.
- Q6: `docs/phases/phase-2/implementation-plan.md`.
- Q7: `web/packages/shell/src/api/types.golden.test.ts`; `web/packages/shell/src/api/types.ts` only where the stronger guard exposes a real mismatch with the Go wire shape.
- Records: `docs/phases/phase-2/phase-2.5-complete.md`, `docs/phases/phase-2/reviews/2.5-codex.md`, `docs/phase-status.md`, and `docs/decisions.md` for any inferred row the user rules on.

Out:
- Finding 8: a stable finding identity (`pkg/health/computed.go` finding shape, `TopFindings.tsx`), deferred to Phase 3 planning.
- Wire format and goldens: no field is added, removed or retyped, so `pkg/api/testdata/golden` does not change.
- Every carry-over in `docs/phase-status.md`, including the delegation memory bounds and the spec-edit stream variant.
- Other orphan or trigger logic not needed for Q2 (the per-parent computed trigger already checks the entry's parent), other computed flags not needed for Q3, other edge-cap wording, chart values, kind e2e fixtures, refactors and new dependencies.

## Plan / design impact
- `implementation-plan.md` Task 44 text changes (Q6). No design, rules or decision change is expected: every other item restores stated behaviour.
- The inferred rows in Q1, Q4 and Q7 need user confirmation, or a `decisions.md` entry, before those items are implemented.

## Definition of done
- [ ] Q1–Q7: each has a regression test that fails before the fix and passes after, at the boundary above (report both runs; Q6 reports the grep)
- [ ] `go vet ./...`, `go vet -tags bench ./...` and `go vet -tags e2e ./...` pass
- [ ] `go test -race ./...` passes
- [ ] `make lint` passes
- [ ] in `web/`: `npm test`, `npm run typecheck`, `npm run lint` and `npm run build` pass
- [ ] cross-model review of `git diff 29b8792..HEAD` by Codex, saved to `reviews/2.5-codex.md`, with every finding triaged (required for stabilization)
- [ ] `phase-2.5-complete.md` written; `phase-status.md` updated; `phase-codex.md` triage notes point at the fixing commits

## Queue
- [x] Q1 (P1) Delegation tab rows turn Unknown when their own poll fails or goes stale — src: reviews/phase-codex.md#1 — `fix(shell): delegation tab goes unknown when its poll fails`: the rows' own poll set no stale view; only the route detail's did
- [x] Q2 (P1) Orphan KGWUI-011 ignores entries naming another install's parent — src: reviews/phase-codex.md#2 — `fix(computed): orphan KGWUI-011 is install-local`: any entry from the shared controller name counted, not only eligible entries under owned parents
- [x] Q3 (P1) A visible unknown parent keeps `listener-unknown` when another parent is hidden — src: reviews/phase-codex.md#3 — `fix(explore): listener-unknown per parent`: unknown parents' deps were unioned into one set that had to be visible whole
- [x] Q4 (P2) `scope-unknown` wins over the edge cap's `too-large` — src: reviews/phase-codex.md#4 — `fix(api): scope-unknown wins over too-large`: a cut ref was labelled too-large without checking the install's scope
- [x] Q5 (P2) Computed-only candidates keep `since` under restricted evidence — src: reviews/phase-codex.md#5 — `fix(health): computed-only candidates keep since under restricted evidence`: the first-seen lookup covered owned objects only, not declared or no-parent candidates
- [x] Q6 (P2) Implementation plan Task 44 matches the D-023 amendment — src: reviews/phase-codex.md#6 — `docs: plan Task 44 follows D-023`: Task 44 predated the D-023 amendment; the Q6 grep matched 2 lines on the base, none after
- [x] Q7 (P3) TypeScript golden guard checks value types — src: reviews/phase-codex.md#7 — `test(shell): golden guard checks wire value types`: `Spec<T>` kept only keys and optionality, never the value types
