Cross-model: YES (author: Claude Opus 5.5, reviewer: Codex, command: codex exec --sandbox read-only)
Scope: design (Phase 6, no implementation plan yet)
Diff: n/a (design review of the untracked draft at 22709b7)
Timing: on time

I would revise the design before implementation. The main blockers are false-green health, delegated-path construction, visibility, and bounds that do not actually bound retained memory.

Reviewed the untracked [Phase 6 design](/Users/ddoyle/Development/claude/snellius-claude/docs/phases/phase-6/design.md) against HEAD `22709b7`, the requested documents, and cited code. No files changed; no implementation tests run.

1. **P1 — Path health omits Gateway-scoped contradicting signals.**  
   **Location:** §4.5, lines 120–123.  
   **Evidence:** The aggregation names backend findings and route signals. xDS rejection findings belong to the Gateway (`pkg/health/xds.go:272`), as do proxy readiness and disconnect findings (`pkg/health/checks.go`, `pkg/health/proxystats.go:162`). Gateway `Programmed=False` is another explicit D-006 signal.  
   **Why:** An accepted route with ready Service endpoints can appear healthy through a rejecting or unavailable Gateway.  
   **Fix:** Define health over the complete attachment chain, including Gateway/listener conditions and Gateway-scoped findings. Include required-check unavailability and stale/missing status, preserving errors even when another signal is unknown.

2. **P1 — View A does not supply delegated child attachments.**  
   **Location:** §4.2, line 99; §4.3.  
   **Evidence:** `pkg/engine/views.go:113` explicitly excludes children without Gateway/ListenerSet parents from view A. `computed.Delegator` computes separate views B/C; it does not add child members to view A.  
   **Why:** Deriving rows as specified omits delegated backends. Simply attaching every child rule to every ancestor attachment would instead invent paths, hostnames and matcher contexts.  
   **Fix:** Specify a bounded join of existing A/B/C results, retaining root attachment, delegation provenance, effective matcher mode, dependency sets and truncation/unknown markers. Do not introduce another delegation evaluator. Define ListenerSet-to-Gateway projection too.

3. **P1 — “No backends” does not establish intentional, harmless blackholing.**  
   **Location:** §10, line 248; §4.1, line 87.  
   **Evidence:** The design classifies every rule without backends as “no traffic by design.” Such a rule still matches requests; redirect/direct-response behavior depends on its filters. Gateway API also specifies error responses for invalid backend configurations. [HTTPRoute specification source](https://raw.githubusercontent.com/kubernetes-sigs/gateway-api/v1.4.0/apis/v1/httproute_types.go).  
   **Why:** An accidentally empty rule can receive a reassuring explanation despite failing requests. Weight zero likewise does not cancel unrelated route or Gateway failures.  
   **Fix:** Report the written action: redirect, direct response, no forwarding backend, or zero-weight ref. Do not infer intent or suppress existing problems. Define the all-zero denominator case explicitly.

4. **P1 — The visibility predicate exposes computed facts without authorizing their dependencies.**  
   **Location:** §13, line 274.  
   **Evidence:** Visibility requires only route and backend access. Paths also reveal Gateway, ListenerSet, listener, hostname and potentially ancestor-route facts. Existing `pkg/api/visible.go` provides `allowDeps`; computed attachments and delegation contexts carry dependency sets.  
   **Why:** A caller allowed to see a route/backend pair could learn a forbidden Gateway’s topology or a hidden ancestor’s effective configuration. The paragraph also contradicts itself: a denied backend both excludes the row and produces a restricted row.  
   **Fix:** Define separate rules for written refs, computed topology, restricted placeholders and aggregates. Authorize computed dependencies before exposing their facts. Authorize backend-detail admission and Service access on **every request, including memo hits, before any live read**. Preserve D-013’s restricted severity behavior unless explicitly superseded; visible-only counts must not imply complete, healthy coverage.

5. **P2 — The path memo’s invalidation contract does not cover its health inputs.**  
   **Location:** §4.3, line 104.  
   **Evidence:** Rows contain problems and are sorted by them, but the key is “snapshot version.” `engine.Source` separately tracks cache, collected values and staleness (`pkg/engine/loop.go:11–31`). Findings also change when holds/persistence deadlines expire without object changes. D-052 specifically guards time-dependent memo reuse.  
   **Why:** Reusing topology on unchanged Kubernetes data can preserve an obsolete healthy state or problem ordering after a reject, metric expiry or matured endpoint finding.  
   **Fix:** Separate topology memoization from health projection, or key the combined table by the complete immutable model/publication identity and applicable deadlines. State this explicitly rather than calling it the D-052 pattern.

6. **P2 — Traffic-share arithmetic lacks boundaries for mixed delegation and unresolved attribution.**  
   **Location:** §4.4, lines 112–116.  
   **Evidence:** Delegation receives no share, but the denominator for ordinary refs in a rule containing delegation is unspecified. §10 also permits several unresolved candidates, while §4.4 promises a numerical failing share.  
   **Why:** Excluding delegation weights or including them both imply controller-specific allocation. An unattributed `ResolvedRefs=False` cannot establish which weighted fraction fails.  
   **Fix:** Limit percentages to rules whose applicable semantics are established; suppress ordinary-ref percentages for mixed delegation rules unless authoritative evidence supplies them. Show failure percentages only for attributable refs. Label percentages as conditional within that rule, never Gateway-wide traffic shares, and use protocol-appropriate failure wording.

7. **P2 — Endpoint summaries lose the port identity that KGWUI-023 requires.**  
   **Location:** §9.1 and §9.4, lines 206, 227–234.  
   **Evidence:** `sliceCounts` takes a Service **port name**, handles address families, and selects the larger family (`pkg/health/endpoints.go:215–234`). `EndpointsDTO` has one backend-level ready/total pair without a port selector.  
   **Why:** A Service can be ready on one referenced port and empty on another. A Service-wide summary can contradict its path rows. Mandatory integer counts also need explicit interpretation for unavailable, failed or partial reads.  
   **Fix:** Carry backend port/protocol identity or return per-port summaries, preserving the existing dual-stack rules. Specify that only complete reads establish authoritative zero; partial/failed reads retain completeness and never become “no endpoints.” Distinguish not-applicable cases such as ExternalName and non-Service Backends.

8. **P2 — The port-chain and Service-detail data are not available from today’s cache.**  
   **Location:** §8.2, line 194; §9.3, line 219.  
   **Evidence:** `ServicePort.Target` is zero for a named targetPort, and `serviceFact` discards its name (`pkg/cache/services.go:39–46, 114–138`). Services are stored as facts, without full YAML. Reading EndpointSlices does not recover either the written targetPort name or Service YAML.  
   **Why:** The promised display cannot be implemented from the named inputs. Also, one slice lacking a port does not alone establish a broken Service-port chain.  
   **Fix:** Choose and budget an additive fact field or an authorized on-demand Service read; document additional RBAC. Define whole-Service port evidence and incomplete-read behavior. Keep the chain as written configuration plus observed slice ports, without predicting Pod resolution or kgateway translation. Clarify whether port discrepancies are descriptive facts or new checks, given §4.5’s “no new checks.”

9. **P2 — The scoped reader’s stated limits do not bound response decoding or memo memory.**  
   **Location:** §9.2, lines 211–215; §14, line 279.  
   **Evidence:** Four in-flight reads limit concurrency, not the number of completed Service entries retained for five seconds. A typed LIST can decode its entire response before the application stops at 1,000 endpoints.  
   **Why:** Many distinct Service requests can retain far more than 4,000 endpoints. A single large response can exceed the intended allocation bound before truncation. This undermines the approximately 8 MiB D-057 headroom.  
   **Fix:** Define pagination, continuation handling, response/page limits, global memo entry/byte limits, eviction, duplicate-request coalescing and bounded admission. Distinguish a complete empty result from a truncated result. Resolve the “reader in the core” wording with an injected neutral interface whose Kubernetes implementation owns client-go types; state unsupported-provider behavior and per-request authorization.

10. **P2 — `ListOnce` does not specify how it fills the same Snapshot.**  
    **Location:** §12, line 262.  
    **Evidence:** Listing served kinds fills only part of `Snapshot`. Existing snapshots also contain Namespaces, selected controller Pods/Deployments and configured optional facts, plus `Synced`, `TakenAt` and `Source` (`pkg/cache/cache.go:64–107, 619–675`). Informer recovery intentionally requires a successful watch, which cannot be reused unchanged for list-only access.  
    **Why:** A nominally successful one-shot snapshot can lack install detection, scope evidence or fingerprints, or remain unavailable despite successful LISTs.  
    **Fix:** Specify all configured inputs, selectors, transforms, sorting, fingerprints and sync semantics. Define partial-success versus fatal errors; successful LIST must establish one-shot access without watch recovery. Optional/list-only kinds must retain their existing semantics.

11. **P2 — Namespace-scoped Access needs an additive public contract and consumer semantics.**  
    **Location:** §12, lines 264–268.  
    **Evidence:** `Snapshot.Access` is currently `map[GVR]Access`; consumers directly index it by GVR. Fingerprints also encode kind-wide access (`pkg/cache/fingerprint.go`). The draft promises namespace-level access without defining its representation or aggregate meaning.  
    **Why:** Replacing the map breaks kgwctl; retaining only an aggregate cannot express one readable namespace and one denied namespace. Scoping only dynamic informers would still leave typed watches cluster-wide.  
    **Fix:** Preserve the existing field and define an additive namespace-access API, conservative aggregate rules, fingerprint inclusion, and downstream availability behavior. Specify namespace treatment for typed inputs, nil/empty lists and deduplication. Distinguish “controller not observed” from a known install outside scope—an unread Deployment does not supply its identity.

12. **P2 — Backends inventory excludes references precisely when attachment fails.**  
    **Location:** §8.1, line 179.  
    **Evidence:** Services qualify through a **path** or non-route consumer. View A members contain accepted computed attachments; rejected/unknown attachments are represented separately as outcomes.  
    **Why:** A Service referenced by a route whose listener/hostname attachment fails can disappear from Backends, weakening the inverse “how is this backend exposed?” investigation.  
    **Fix:** Build backend candidates from admitted routes’ written refs and pack consumers, independently of successful paths. Display zero confirmed exposures with the attachment failure/uncertainty instead of omitting the backend.

13. **P2 — “Same DTO” does not define an additive representation for GRPC matches.**  
    **Location:** §7.2, line 168; §11.  
    **Evidence:** `MatchDTO.Method` is an HTTP method string; GRPC method matching needs service, method and match type (`pkg/api/explore_types.go:414–431`). Existing rule serialization also defaults absent matches to HTTP `PathPrefix "/"`.  
    **Why:** Reusing that representation directly either misrepresents GRPC/TCP/TLS rules or changes existing API v1 meanings.  
    **Fix:** Specify optional, kind-specific fields and serialization while preserving HTTP response shapes and exported Go field types. Preserve exported `health.BackendState` as a compatibility wrapper when moving dispatch behind the pack. Document any intentional Go break and kgwctl migration separately; a minor tag does not itself define compatibility.

14. **P3 — The budget verification would miss the proposed retained state and expensive request cases.**  
    **Location:** §14, lines 278–280; §16, line 306.  
    **Evidence:** Engine-only benches do not populate an API-owned path memo or live-reader memo. D-039’s allocation gates concern recomputes, not HTTP requests. `scale.go` already generates all three additional route kinds at 5% each (lines 257–262); ordinary leaves mostly have one rule and one ref (lines 632–654).  
    **Why:** Existing gates can pass while request-populated state exceeds D-057. Merely “adding GRPC/TCP” does not exercise rules×refs expansion, authorization, alternate sorting or concurrent cache misses.  
    **Fix:** Define request-specific allocation gates and retained-heap measurements after warming all API caches, including multiple installs, distinct Services and overlapping generations. Add explicit multi-rule/multi-ref fixtures and concurrent cold-request cases. At 48 bytes, about 175,000 rows alone consume the entire 8 MiB headroom.

15. **P4 — The future extension contracts lack enough identity and provenance.**  
    **Location:** §9.4, lines 227–238; §11, line 257; §7.3.  
    **Evidence:** One optional `Proxy` ref does not define runtime-instance identity, port-aware cluster mapping or several mapping results. `BackendConsumers(snapshot)` returns only two refs, with no declared install context, role, port or field provenance.  
    **Why:** #35 needs to distinguish proxy replacement and ambiguous mappings; Phase 7 needs to distinguish ext-auth, rate-limit and ext-proc dependencies; Phase 11 needs runtime filter evidence alongside written configuration.  
    **Fix:** State extension requirements now: stable runtime identity with replacement detection, port-aware mapping returning multiple results/unknown, source-specific completeness, and consumer role/port/provenance scoped per install. Keep written filters intact when adding `effectiveFilters`. The collectors themselves can remain deferred.
## Triage
| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | Path health omits Gateway-scoped signals | reviewer | accept | §4.5: problem over the whole chain (Gateway, listener, route, delegation, backend, coverage); unknown never hides an error |
| 2 | View A lacks delegated children | reviewer | accept | §4.2: rows join view A with the Delegator's view C rows; view C uncertainty carries through; no new evaluator; ListenerSet projects to its Gateway (§4.1) |
| 3 | "No backends" is not intentional blackholing | reviewer | accept | §4.1: the row shows the written action; anti-goal "no inferred intent"; all-zero weights defined in §4.4 |
| 4 | Visibility ignores computed dependencies | reviewer | accept | §13: rows authorised by `allowDeps` on their dependency set, memo hits included; backend detail authorises before any read; contradiction removed; policy still needs the user |
| 5 | Memo key misses health inputs | reviewer | accept | §4.3: topology memo per snapshot, health projection per model publication plus earliest deadline (D-052) |
| 6 | Share undefined for mixed delegation and unattributed refs | reviewer | accept | §4.4: no share in mixed rules; failing share only when attributed; "% of this rule's requests"; protocol wording |
| 7 | Endpoint summaries lose port identity | reviewer | accept | §9.1, §9.2, §9.5: per Service port as 023 counts; completeness; counts absent unless established; not-applicable cases |
| 8 | Port chain data not in the cache | reviewer | accept | §9.4: `TargetName` added to the Service fact; chain is descriptive; slice-port gap only on complete reads; no Service YAML tab (§8.2) |
| 9 | Reader bounds don't bound memory | reviewer | accept | §9.3: paged LIST (250 × 4), coalescing, LRU of 32 Services, neutral interface in the core with a Kubernetes implementation |
| 10 | `ListOnce` doesn't define the Snapshot | reviewer | accept | §12.1: inputs, Access, Synced, TakenAt, Source, error semantics |
| 11 | Namespace Access needs an additive contract | reviewer | accept | §12.2: `Access` kept as aggregate; new `NamespaceAccess`; typed inputs scoped too; "controller not observed" |
| 12 | Backends inventory drops failed attachments | reviewer | accept | §8.1: candidates from written refs of admitted routes and consumers; "0 confirmed exposures" |
| 13 | GRPC matches don't fit the DTO | reviewer | accept | §7.2: kind-specific optional fields; HTTP shape unchanged; `health.BackendState` kept as a wrapper (§11) |
| 14 | Budget verification misses request state | reviewer | accept | §14: API-level bench pass after warming caches; multi-rule/ref and delegated fixtures; 2 MiB topology cap; request gates |
| 15 | Future contracts lack identity and provenance | reviewer | accept | §9.5 `RuntimeDTO` and #35 requirements; §11 consumer role, port and field; §7.3 written filters kept |
| B1 | §13 decision and its effect on the memo | author (builder check) | accept | §13 split into the seam (built regardless) and the policy (needs the user); memo stays viewer-independent |
| B2 | Share for unattributed refs | author (builder check) | accept | same as #6 |
| B3 | Hostnames per row | author (builder check) | accept | §4.1: list per row, sort by first, `q` matches any |
| B4 | Budget overshoot fallback | author (builder check) | accept | §14: shrink first, ask before a raise |
| B5 | 6.2 vs 6.3 Backend judgement order | author (builder check) | accept | §11: 6.2 consumes today's state; 6.3 moves it |
| B6 | Core group in URLs | author (builder check) | accept | §8.2: `-` in paths, `core` in params, as today |
| B7 | Endpoints for Backend resources | author (builder check) | accept | §4.5, §9.2: not applicable; state from conditions |
| B8 | Where "503" comes from | author (builder check) | accept | §4.4, §8.2: fixed phrases per signal; no status code promised |
| B9 | GatewayExtension decoding | author (builder check) | accept | §8.3: fields named from the kgateway API; re-verified against the plan's kgateway version in 6.3 |

## Scoped re-review (Codex, same command)
Codex rated 8 findings resolved and 6 partly resolved (2, 9, 10, 11, 13, 14). It found one new P2: `Source` is a fingerprint (`pkg/cache/cache.go:94`), not a string.

| # | Partial / new | Decision | Note |
|---|---|---|---|
| 2 | join bounds, matcher correlation | accept | §4.2: match from the view C context; bounded by view C's bounds and truncation |
| 9 | 250 slices can exceed 1,000 endpoints | accept | §9.3: 10 slices × 10 pages, ≤ ~1,000 endpoints, partial beyond |
| 10, new P2 | `Source` is a fingerprint | accept | §12.1: `Source` unchanged; new `Mode` field |
| 11 | consumers of namespace access | accept | §12.2: Snellius unaffected; denied namespaces give "unknown" |
| 13 | wrapper lacks selection context | accept | §11: `BackendState` keeps its signature and calls the pack's judgement directly |
| 14 | no request allocation limits | accept | §14: starting allocation targets, tuned in 6.2 |
