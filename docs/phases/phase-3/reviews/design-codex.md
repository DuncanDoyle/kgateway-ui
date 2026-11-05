# Review: Phase 3 design + implementation plan — Codex

Reviewer: Codex (`codex exec --sandbox read-only`)
Cross-model: yes (authored by Claude)
Scope: `git diff 544dcd5..1217683` — design.md, implementation-plan.md, roadmap (docs only; no code changed, no tests run)
Date: 2026-11-01

---

I would not approve the plan as written. The main problems are signal correctness, freshness, and several integration steps that cannot satisfy the stated gates.

Reviewed the requested documents and `544dcd5..HEAD`, and checked the relevant backend, shell, chart, downstream example and e2e code. No files changed; no tests run.

1. **P1 — Truncating inherited paths changes computation, not just presentation.**  
   **Location:** [implementation-plan.md:419](docs/phases/phase-3/implementation-plan.md:419).  
   **Evidence:** Task 19 inserts `BoundPath` into matcher merging. Today `merge` produces paths subsequently consumed by `fit`, which compares literal prefixes (`pkg/computed/kgw24/fit.go:83,156`; likewise kgw23). Appending `…[truncated]` creates a different matcher and can produce false dropped-route outcomes or disagreements.  
   **Direction:** When a computation exceeds the byte bound, propagate `unknown/too-large` and suppress dependent negative findings. Clip only display strings.

2. **P1 — The memo fingerprint omits access-state changes.**  
   **Location:** [implementation-plan.md:408](docs/phases/phase-3/implementation-plan.md:408).  
   **Evidence:** `inputPrint` includes kind object hashes but not `Snapshot.Source.Access` or `Snapshot.Synced`. The existing required `cluster access` check reads those fields (`pkg/health/checks.go:55–67`). A watch can fail while its cached objects remain unchanged.  
   **Why:** The engine can recompute yet reuse a successful check result, incorrectly retaining healthy output.  
   **Direction:** Fingerprint availability and synchronization alongside object content, including recovery transitions.

3. **P1 — Optional watches still make the install unknown through an existing required check.**  
   **Location:** [implementation-plan.md:624](docs/phases/phase-3/implementation-plan.md:624).  
   **Evidence:** Task 35 adds the new watches to `Snapshot.Access`. Existing `cacheCheck` treats *every* failed Access entry as KGWUI-008 and returns `Required: true`. Ignoring the new checks’ `Unavailable` results in `RollUp` does not remove that separate failure.  
   **Why:** Denied Events or CRD reads violate the explicit “not available, never unknown” guarantee.  
   **Direction:** Separate required core inputs from optional diagnostic inputs in the access check, with API tests for denial and recovery.

4. **P1 — Sampled cumulative counters cannot identify an active reject.**  
   **Location:** [design.md:401](docs/phases/phase-3/design.md:401), [implementation-plan.md:706](docs/phases/phase-3/implementation-plan.md:706).  
   **Evidence:** Between scrapes, `(rejected, success)` can change from `(3,8)` to `(4,9)` whether ACK or NACK happened last. RDS and cluster entries retain only `update_rejected`, so recovery is even less observable.  
   **Why:** The proposed fallback can report a recovered proxy as actively rejecting, or miss an active rejection.  
   **Direction:** Treat these as “rejects observed during this interval.” Use an authoritative current-state signal for active rejects; represent ambiguous ordering as unknown.

5. **P1 — The log-name grammar does not establish the promised secret boundary.**  
   **Location:** [design.md:440](docs/phases/phase-3/design.md:440), [implementation-plan.md:925](docs/phases/phase-3/implementation-plan.md:925).  
   **Evidence:** An alphanumeric credential satisfies `[A-Za-z0-9_.~:/-]{1,253}`. An error containing `Error adding/updating listener(s) AKIAFAKEKEY123456:` passes the proposed shape and grammar. The planned leakage test does not require placing its sentinel in an accepted extraction position.  
   **Why:** Untrusted error text can survive as a supposedly safe resource name and reach the API or MCP adapter.  
   **Direction:** Validate the structured log envelope and retained identifiers against independently known resources where possible; otherwise retain only fixed classifications. Test secrets and spoofed markers inside every accepted capture position.

6. **P1 — A parent-level condition does not identify the ref to fix.**  
   **Location:** [implementation-plan.md:855](docs/phases/phase-3/implementation-plan.md:855).  
   **Evidence:** Tasks 57–58 turn one `ResolvedRefs` condition into a specific ref, group correction or ReferenceGrant. Existing `RouteParentStatus` contains a parent identity and conditions, with no backend-ref location (`pkg/objects/objects.go:231`). A route can contain several backend refs and extension refs.  
   **Why:** The plan can recommend a grant or edit for the wrong reference, including unnecessarily broadening access.  
   **Direction:** Define explicit attribution and ambiguity rules. Without uniquely established provenance, show candidate refs and next steps, with no suggested YAML.

7. **P1 — The retained Event facts cannot establish “three failures in 15 minutes.”**  
   **Location:** [implementation-plan.md:606](docs/phases/phase-3/implementation-plan.md:606), [implementation-plan.md:650](docs/phases/phase-3/implementation-plan.md:650).  
   **Evidence:** The projection retains Pod UID, reason, cumulative count and last timestamp. A series with 99 old failures plus one recent failure can look like 100 recent failures. Event identity and series fields are also discarded, preventing reliable delta tracking across separate Events.  
   **Why:** KGWUI-020 can raise false findings and inflate counts.  
   **Direction:** Retain Event identity and relevant series timestamps; maintain observed deltas in a bounded window. Initial history that cannot be reconstructed must remain explicitly uncertain.

8. **P2 — The newest-ReplicaSet test has no defined source for the template hash.**  
   **Location:** [design.md:514](docs/phases/phase-3/design.md:514).  
   **Evidence:** The design compares Pod `pod-template-hash` against the Deployment’s current template while refusing to watch ReplicaSets. The Deployment template does not normally contain that generated hash; Kubernetes itself locates the new ReplicaSet by comparing templates while ignoring the hash. `Rollout.NewHash` is introduced without specifying how it is obtained.  
   **Why:** The early image-pull branch of KGWUI-021 is not implementable from the prescribed comparison.  
   **Direction:** Supply verified ReplicaSet facts through the provider, or initially rely on `ProgressDeadlineExceeded`.

9. **P2 — The policy-target “verifier” lacks target-level attribution.**  
   **Location:** [design.md:289](docs/phases/phase-3/design.md:289), [implementation-plan.md:879](docs/phases/phase-3/implementation-plan.md:879).  
   **Evidence:** Existing policy ancestors carry an ancestor ref and conditions, not the target-ref index (`pkg/objects/objects.go:254`). Ancestors may be Gateways above targeted routes. With multiple targets sharing a Gateway, an ancestor cannot establish which target resolved.  
   **Why:** The proposed verifier can either miss drift or falsely claim that a missing target contradicts status. This does not yet satisfy the phase’s anti-mirroring guarantee.  
   **Direction:** Specify reporting granularity, matching rules and coverage before implementation. Unsupported cases need `not-verified`, backed by observations on both supported controller versions.

10. **P2 — The code replacements conflict with the required compatibility and e2e gates.**  
    **Location:** [implementation-plan.md:846](docs/phases/phase-3/implementation-plan.md:846).  
    **Evidence:** Replacing KGWUI-002 changes existing scalar values in `golden/health.json`, while `additive_test.go` rejects such changes. Existing e2e assertions still require KGWUI-002 for `shop/broken` and KGWUI-003 for the missing-target policy (`test/e2e/api_test.go:95,195`). Their migration is absent from the task file lists.  
    **Why:** The prescribed standard gates cannot pass as planned.  
    **Direction:** Explicitly distinguish additive wire-schema compatibility from the approved pre-release finding-code migration, record narrow golden exceptions, and migrate existing e2e expectations in the owning tasks.

11. **P2 — The new provider files violate the existing import boundary.**  
    **Location:** [implementation-plan.md:23](docs/phases/phase-3/implementation-plan.md:23), [implementation-plan.md:976](docs/phases/phase-3/implementation-plan.md:976).  
    **Evidence:** The boundary allowlist is per file: only `pkg/cache/cache.go` is currently allowed. New `cache/events.go`, `crds.go` and `services.go` need Kubernetes imports, but the plan authorizes only three other new allowlist entries.  
    **Why:** The required boundary gate fails even with otherwise correct implementations.  
    **Direction:** Add reviewed, narrow provider-file exceptions, or keep Kubernetes decoding in already allowlisted adapters.

12. **P2 — The promised replaceable stats source is hardcoded in the plan.**  
    **Location:** [design.md:894](docs/phases/phase-3/design.md:894), [implementation-plan.md:690](docs/phases/phase-3/implementation-plan.md:690).  
    **Evidence:** The design places the stat list on the rule-set seam and promises downstream formats can plug in. Task 41 instead fixes one `StatsFilter`, Pod transport and port 9091; only name mapping is replaceable. `WithProcessSource` cannot provide an alternative stats transport.  
    **Why:** A downstream or standalone provider cannot supply this feature through the advertised seams without bypassing core behavior.  
    **Direction:** Define replaceable transport/target discovery and stat interpretation, and exercise them in the downstream proof.

13. **P3 — Memoization tests primarily prove skipping, not correct API behavior.**  
    **Location:** [implementation-plan.md:412](docs/phases/phase-3/implementation-plan.md:412).  
    **Evidence:** The principal tests assert check invocation counters and deadlines. The declared-input guard uses the Phase 2 fixture world and fresh Stores, leaving later process, collector and state-dependent branches largely unexercised.  
    **Why:** Incorrect memo keys can pass while the API serves stale findings.  
    **Direction:** Compare memoized and uncached API responses across access failures, recovery, collector expiry, process changes and deadlines. Include `/health` and `/overview` authorization tests after adding typed evidence: today `visibleHealth` clears only message, detail and since.

14. **P4 — Collector publication has no enforceable immutability or freshness contract.**  
    **Location:** [implementation-plan.md:212](docs/phases/phase-3/implementation-plan.md:212).  
    **Evidence:** `Data any` and a map snapshot have no stated ownership/copying rules. `At` is not part of the confirmation identity, and no collector-wide expiry contract is defined.  
    **Why:** A downstream can accidentally mutate already published data, or leave old successful data continuously confirmed after collection stops—both undermine D-019 and later metrics/MCP consumers.  
    **Direction:** Define ownership transfer or copying, per-value availability and expiry, and lifecycle cleanup before shipping this seam.

15. **P4 — A planned breaking extension change lands after the “last planned break.”**  
    **Location:** [implementation-plan.md:869](docs/phases/phase-3/implementation-plan.md:869).  
    **Evidence:** Task 60 adds `PolicyTargets` to `computed.Rules` in 3.3. Downstream implementations supplied through `WithComputedRules` must then change, despite the stability statement that 3.1 is the last planned break ([design.md:114](docs/phases/phase-3/design.md:114)).  
    **Direction:** Establish the complete contract in 3.1, or make policy-target support an optional interface. Add a downstream custom-rule implementation to the standing proof.

The **87 tasks are reasonable as a programme of separate milestones, but too broad for one phase approval**. Keep the explicitly requested 3.0 proof, then foundations and the core diagnostics release. Move log parsing, backend readiness and 2.5 support into separately approved milestones. They add distinct security, data-model and release dependencies.

The fixed budgets are **unproven, not demonstrably impossible**. Dense heap has about 16 MiB headroom and retained heap about 7–9 MiB; per-check memoization does not remove the existing views/index rebuild costs. Measure representative new watches, evidence, groups and Stores before building all the pages.

On controller mirroring: the policy-target verifier needs substantial definition before it earns that description. Envoy naming is appropriately deferred while unverified; copying `kgw24` into `kgw25` should additionally require an explicit semantic review and observations before claiming 2.5 support.

---

## Triage

Author: Claude. Every code claim behind findings 1–3 was checked against the source before accepting (`pkg/health/checks.go:54-67`, `pkg/computed/kgw24/fit.go:83,149`, plan Task 18–19).

| # | Rank | Verdict | Action |
|---|---|---|---|
| 1 | P1 | accept | Design §15 and plan Task 19: never cut a path used in computation. A merge over the 4 KiB bound yields "too large to compute" for that child (unknown, no negative finding); only display strings are clipped. |
| 2 | P1 | accept | Task 18: the input fingerprint includes `Snapshot.Synced` and the `Access` state of every declared kind; tests for a watch failing and recovering with unchanged objects. |
| 3 | P1 | accept | Task 35 (and design §7.1): `cacheCheck` reports only required core kinds; optional diagnostic watches report through their own checks' `NotAvailable`. API tests for denial and recovery. |
| 4 | P1 | accept | Design §8.8 and Task 41: proxy stats count "rejects observed since the last scrape" per Pod and type, as evidence only. KGWUI-001's active state stays the controller gauge; the "001 fires from proxy stats" fallback is removed. |
| 5 | P1 | accept | Design §8.9 and Task 66: a name extracted from a log line is kept only if it equals an Envoy resource name the UI already knows from that Gateway's proxy stats; otherwise only the error class is kept. The leak test puts the sentinel in every accepted capture position and in spoofed markers. |
| 6 | P1 | accept | Design §7.7 and Tasks 57–58: a fix needs one attributable ref. 013 group and port fixes are attributable by the UI's own check (discovery, Service ports). 014 gets a fix only when the route has exactly one cross-namespace ref of the failing field; otherwise the candidates are listed, with no YAML. |
| 7 | P1 | accept | Design §9.2–9.3 and Tasks 36, 40: keep Event identity and series timestamps; count observed deltas in a bounded 15-minute window; history from before the UI started is "unknown", never counted. |
| 8 | P2 | accept | Design §9.3 and Task 40: KGWUI-021 relies on `ProgressDeadlineExceeded`; image-pull and crash reasons of the Deployment's Pods are evidence. No template-hash comparison. |
| 9 | P2 | accept | Design §7.6 and Task 60: specify the verifier's granularity. Single-target policies with a Gateway or route ancestor are verifiable; anything else is "not verified". Observations on 2.3.11 and 2.4.7 go into `computed-rules.md` first. |
| 10 | P2 | accept | Tasks 56–61 own the migration: a narrow, listed golden exception for the pre-release code changes (002/003 → specific codes), and the `test/e2e/api_test.go` expectations move with them. |
| 11 | P2 | accept | Plan Global Constraints: list every new file needing a Kubernetes import (`pkg/cache/events.go`, `crds.go`, `services.go`, …) for the per-file allowlist, each with its reason. |
| 12 | P2 | accept | Design §8.8, §19 and Task 41: proxy stats sit behind a `proxystats.Source` interface (targets, transport, stat interpretation), replaceable through the extension registry and exercised in `examples/downstream`. |
| 13 | P3 | accept | Task 18: compare memoized and uncached API responses across access failure and recovery, collector expiry, process changes and deadlines. Tasks 47–48: authorization tests for typed evidence on `/health` and `/overview` (`visibleHealth` clears only message, detail and since). |
| 14 | P4 | accept | Design §4.1 and Task 4: published collector values are owned by the store (callers hand over, never mutate); each value has an availability state and an expiry; a stopped collector's values expire. |
| 15 | P4 | accept | Task 60: policy-target support is an optional interface on the rule set (type assertion), so `computed.Rules` does not break after 3.1; `examples/downstream` gets a custom rule set. |
| scope | — | to user | Codex: 87 tasks are reasonable as milestones, too broad for one approval. Proposal for the user: approve 3.0–3.2 now; 3.3 (incl. log signals) and 3.4 (backends, 2.5) each get their own approval before they start. |
| budgets | — | accept | Plan already measures the baseline first (Task 12). Added: measure representative new watches, evidence, groups and Stores at Large before the 3.2 pages are built. |
| A1 | author | accept | Plan agent note: no NACK fixture exists although §21 says "existing"; Task 20 creates `test/e2e/fixtures/nack.yaml`. Design §21 wording fixed. |
| A2 | author | accept (ruling) | Probe type: Event messages stay unread (§18.2). The probe type comes from the Pod spec when only one probe type is defined, otherwise "a probe". |
| A3 | author | accept | `diagnostics.controllerLogsNamespaces` chart value, because the chart cannot discover installs. |
| A4–A11 | author | accept | Plan agent notes 4–11 (temporary `Input.Collected` in 3.0, `capabilityStates`, identity in `pkg/health`, `WithHandlers(name, f)`, grouped view from the findings endpoint, existing `serviceAccount.name`, baseline on both 544dcd5 and the 3.0 head, served-version check before moving the TCPRoute fixture) stand as written in the plan. |

Applied 2026-11-01 to design.md and implementation-plan.md. Where a task number above differs, the fix sits in the task that holds the work: finding 7 → Tasks 35, 38; finding 8 → Tasks 37, 38; finding 13 → Task 45; finding 14 → Task 6. Residual risk: log-derived names depend on proxy stats, so with `diagnostics.proxyStats` off, log signals keep only the error class.
