Cross-model: YES (author: Claude Opus 5.5 plan subagent, reviewer: Codex, command: codex exec --sandbox read-only)
Scope: design + plan (Phase 6)
Diff: n/a (plan review at 5704f91)
Timing: on time

Reviewed HEAD `5704f913dc5695e692040ea6df58ae2c5cde39aa` against base `657378ad`, the design, prior triage, and untracked plan. No files changed; no tests run.

1. **P1 — Task 20 replaces an existing exported field.** Plan line 66 declares `Snapshot.Namespaces []string`; HEAD already has `Namespaces []*corev1.Namespace` (`pkg/cache/cache.go:73`). This either fails compilation or breaks kgwctl and existing namespace consumers. **Fix:** preserve `Namespaces`; add a separately named field such as `RequestedNamespaces`, and update the proposed engine tests.

2. **P2 — Task 35 creates an import cycle.** `cache.NewScopedReader` imports `endpoints`, whose interface imports `install.Install`; `pkg/install/assign.go:10` already imports `cache`. Result: `cache → endpoints → install → cache`. **Fix:** keep the interface in `pkg/endpoints`, but replace its install argument with a neutral identifier or descriptor that does not import `install`.

3. **P2 — Task 29 redeclares `api.ListenerDTO`.** Plan line 214 introduces a reduced struct under a name already exported at `pkg/api/explore_types.go:278`, with hostname, TLS, allowed routes, status and computed fields. Redeclaration cannot compile; replacement breaks existing responses and Go consumers. **Fix:** introduce `PathListenerDTO`, preserving the existing type.

4. **P2 — Task 27 reads a nonexistent deadline.** `ProjectInput.Model` is `*engine.InstallModel`, but line 181 requires `Model.Next`. HEAD places `Next engine.Deadline` on `engine.Model` (`pkg/engine/engine.go:156`), not `InstallModel`; the projection declares `Next time.Time`. **Fix:** explicitly pass the publication deadline and preserve its strict-expiry semantics.

5. **P2 — Task 28 contradicts the snapshot-pointer ruling.** Line 201 declares a pointer key but explicitly permits reuse through `Snapshot.Source.Equal`. These are different contracts: source equality does not establish identity of computed views, assignment, or topology index tables. **Fix:** require the exact snapshot pointer, as requested, and specify any additional computed-input identity needed for reuse.

6. **P2 — Tasks 27/36 cannot represent all referenced Service ports.** `BackendRowDTO.Endpoints` is singular; line 340 assigns the cached “per-port summary” to `Sources[0]`. A backend referenced on ports 80 and 443 needs both summaries, including independent completeness and counts (design §§9.1–9.5). **Fix:** return one summary per referenced port and source; define inventory presentation and detail merging explicitly.

7. **P2 — Tasks 43–45 omit map pagination.** The map fetches `/paths?gateway=…`, but Task 29 defaults to 50 rows and caps responses at 200. `buildGraph(rows)` and the 60-route threshold have no continuation-loading or truncation contract. A Gateway can therefore appear complete while missing paths, and never trigger its large-map safeguard. **Fix:** specify bounded continuation loading, completeness messaging, and threshold detection from whole-result counts.

8. **P2 — Task 40 collapses attachment-specific problems into rule-specific fields.** It copies share/problem/endpoints from the route-filtered projection into each written `BackendRefDTO`, although one route can have multiple attachments with different Gateway/listener failures. No reduction rule is specified. **Fix:** define a conservative aggregate and expose attachment-specific details, or nest trace projections under attachments.

9. **P2 — Task 21’s live comparison has no source counts.** Line 78 compares `ListOnce` object counts with `/api/v1/meta`’s served-kinds report. `MetaResponse.Kinds` contains descriptors, not object counts (`pkg/api/types.go:55`). **Fix:** compare against an independent LIST baseline or appropriately scoped inventory responses.

10. **P2 — Tasks 19/35 omit required boundary allowances.** New provider files will import client-go, but `internal/boundary/imports_test.go` allows those imports by exact filename. Neither task updates that allowlist. The required `make test` gate will reject them. **Fix:** add reviewed provider exceptions for the actual new files.

11. **P3 — Task 27’s negative coverage is incomplete.** Its tests cover endpoints capability **off**, but not denied/failing, missing status, or simultaneous error plus unavailable evidence. These are explicit review-focus/design requirements and protect against false green. **Fix:** add observable API cases for each, including an xDS rejection that remains error while endpoint coverage is unknown.

12. **P4 — Tasks 34/36 leave non-route visibility undefined.** Consumers carry role/field provenance, but backend counts and `Consumers[]` have no explicit authorization rule for the referring GatewayExtension. Phase 10 could expose a hidden consumer through a visible backend. **Fix:** authorize consumer identities before serialization and aggregation, with a restricted-viewer test.

13. **P4 — Task 29 leaves the #35 identity seam unspecified.** It references `RuntimeDTO` without defining its fields or any task implementing replacement-sensitive proxy identity, required by design §9.5. **Fix:** define that optional DTO contract now, retaining independent completeness per source.

The other requested rulings are reflected: numbering starts at 19; frozen `golden-v1` remains unchanged; `backendCheck` stays in health and moves to `Kgateway()`; judgement lives in `computed/kgwbackend`; `Consumer` lives in objects; “controller not observed” is a scope source; TCP/TLS serialize `"matches": []`; export adds no library. The stated Makefile targets and e2e commands exist.
## Triage
| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | `Snapshot.Namespaces` already exists | reviewer | accept | Verified at `pkg/cache/cache.go:68`; Task 20 uses `RequestedNamespaces` |
| 2 | Import cycle cache → endpoints → install | reviewer | accept | Task 35: `ScopedReader` takes an install ID string |
| 3 | `api.ListenerDTO` redeclared | reviewer | accept | Verified at `explore_types.go:278`; `PathListenerDTO` |
| 4 | `Next` lives on `engine.Model` | reviewer | accept | Verified at `engine.go:156`; passed in `ProjectInput.Next`, strict expiry |
| 5 | Memo key contradicts the ruling | reviewer | accept | Exact snapshot and computed-views pointers; no `Source.Equal` |
| 6 | One endpoints summary per backend | reviewer | accept | One per referenced port, list and detail |
| 7 | Map has no pagination contract | reviewer | accept | Sequential pages to 2,000 rows, truncation notice, threshold from the summary |
| 8 | Attachment problems collapsed per rule | reviewer | accept | Worst across attachments with attachment-named signals; `serves[].state` per attachment |
| 9 | `/meta` has no object counts | reviewer | accept | Compare with an independent dynamic LIST per kind |
| 10 | Boundary allowlist not updated | reviewer | accept | Tasks 19 and 35 add the two provider files |
| 11 | Projection negatives incomplete | reviewer | accept | denied, failing, missing status, xDS reject with unknown endpoints |
| 12 | Consumer visibility undefined | reviewer | accept | Authorised before serialisation and counts; restricted test |
| 13 | `RuntimeDTO` undefined | reviewer | accept | Defined, never set in Phase 6 |
| A1 | e2e gate misses the 2.5 pair | author | accept | Gate now lists all three CI pairs |

## Outcome
Scoped re-review: all 13 resolved; Codex: "agree: no P1/P2 left". The user approved the plan conditionally on Claude and Codex agreeing (2026-11-10), and the design items in §6, §13 and the 6.5/6.6 split explicitly.
