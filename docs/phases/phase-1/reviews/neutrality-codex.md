# Review: platform neutrality proposal (D-016, design §14)
Cross-model: YES (author: Claude Opus 5.5, reviewer: Codex)
Scope: design change
Diff: c2c52b8..5db5856
Timing: on time


1. **P1 — H5’s “either reference resolves” rule misclassifies resolution errors.** [design.md:500](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/design.md:500). Upstream `gateway_parameters.go:224–241` requires the Gateway reference to exist, then resolves class defaults before merging. A missing class reference also errors (`:299–306`). Thus class selfManaged + missing Gateway parameters, or Gateway selfManaged + missing class parameters, is **an error**, not successful self-managed resolution. Require a resolution-error/unknown outcome; do not suppress diagnostics through the surviving reference.

2. **P1 — H5 can suppress an observed proxy failure.** [design.md:500](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/design.md:500). It replaces KGWUI-006 wholesale with info, but that code covers both missing Deployment and an existing unready Deployment (`pkg/health/checks.go:431–443`). SelfManaged describes provisioning, not readiness; it does not prove an observed failure irrelevant. Suppress only the false “Deployment not found” expectation. Preserve attributable proxy failures, `Programmed=False`, and xDS rejects, or this violates D-006.

3. **P2 — The Detector deferral conflicts with freezing the extension surface.** [design.md:514](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/design.md:514). D-016 says the first release freezes extensions, yet defers replacing public `Detector.Detect(*appsv1.Deployment)` until standalone/the post-Phase-2 milestone. Downstreams already register it through `WithDetectors`. Proving a replacement later does not prevent breaking existing implementations. Preserve a Kubernetes-specific Detector adapter and add a separate provider seam later, or explicitly exclude this interface from the first-release compatibility promise.

4. **P2 — H8 does not enforce the stated dependency constraint.** [design.md:503](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/design.md:503). Its package allowlist permits new Kubernetes imports throughout `health`, `api`, and `install`, including assignment—the very code D-016 defines as core. It also cannot detect new Pod assumptions or discovery-interface dependencies without those imports; `kinds.DiscoveryClient` already demonstrates that pattern. Freeze existing dependency edges/files rather than exempting entire packages, and describe semantic assumptions as review requirements.

5. **P2 — “No generations” needs an alternative freshness contract, not merely skipped checks.** [design.md:467](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/design.md:467). R5 allows green after skipping generation checks, while §14.6 requires status tied to the current file revision (`:535`) and §5.2 requires current evidence. A healthy condition retained after a file edit could otherwise remain green until reconciliation. Require provider-confirmed revision correspondence; distinguish unsupported generation metadata from temporarily unconfirmed status.

6. **P3 — The hedges lack observable acceptance cases.** [design.md:492](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/design.md:492). Add cases for H5’s merge/error combinations and active contradicting signals; H1 IDs containing reserved URL characters through navigation, reload and API calls; H2 missing fields on both install summaries and overview headers; and H6 distinct empty-UID objects plus generation changes. Without these, Kubernetes-only fixtures can pass while the promised neutral contracts remain unusable.

7. **P4 — H6 prevents collisions but cannot identify file spec revisions.** [design.md:501](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-1/design.md:501). `{ref, UID="", generation=0}` remains unchanged across file edits and delete/recreate between snapshots. `engine.observe` therefore retains the old grace timestamp. Reserve a provider revision/incarnation concept for the deferred model; do not present H6 as completing file-mode grace semantics.

The **selfManaged precedence claim itself is correct** at upstream `556b4ce548`: defaults → class parameters → Gateway parameters; selfManaged sets the marker and clears `Kube`, and a subsequent kube override cannot undo it (`merge.go:12–28`). The missing-reference errors are the qualification H5 omits.

The **§14.2 code pointers substantively check out** across backend and SPA; some pinpoint lines are approximate—scope rendering is around `CountTiles.tsx:88` and `ClusterLanding.tsx:137`. No material audit-location error found.

**Scope judgment:** defer H7’s public embed move until embedding is needed: `Options.UI fs.FS` already accepts supplied assets, and exporting an embed later is additive. H6 is cheap defensive work, but empty persisted UIDs are not a normal Kubernetes production bug. The cheap pre-release omission is an explicit compatibility policy for the existing Detector interface and a URL-safe install-ID contract. No files edited.
## Triage
| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | H5 misclassifies parameter resolution errors | reviewer | accept | H5 now has three outcomes: self-managed, unresolved and managed. Unresolved keeps today's behaviour. Acceptance cases added. |
| 2 | H5 could hide an observed proxy failure | reviewer | accept | Only "no proxy Deployment found" changes. An unready Deployment stays KGWUI-006, and `Programmed=False` and rejects are unchanged. |
| 3 | Detector deferral conflicts with freezing the extension surface | reviewer | accept | New H11: the Go extension API is outside the first-release promise until the seam milestone. Today's `Detector` becomes the Kubernetes provider's adapter. |
| 4 | H8 allowlist by package does not enforce the constraint | reviewer | accept | The allowlist is now per file and covers the discovery client. Semantic assumptions are a review checklist item. |
| 5 | Skipping generation checks allows a stale green | reviewer | accept | R5 row, D-016 §6 and §14.6: a provider must confirm status matches the current spec revision, or it cannot reach green. |
| 6 | No observable acceptance cases | reviewer | accept | Acceptance table added to §14.3. |
| 7 | H6 cannot see file revisions | reviewer | accept | H6 moved to §14.4 as a provider revision and incarnation concept, also listed in §14.6. |
| — | H7 can wait; `Options.UI` already takes an `fs.FS` | reviewer | accept | H7 moved to §14.4. |
| — | H6 is not a Kubernetes production bug | reviewer | accept | D-016 no longer calls it one. |
| — | Precedence claim and §14.2 pointers correct | reviewer | n/a | Line refs are approximate in two SPA files; acceptable for a proposal. |
