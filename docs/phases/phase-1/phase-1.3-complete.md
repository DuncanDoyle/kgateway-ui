# Phase 1.3 — Platform-neutral contract (Complete)

**Completed:** 2026-10-20
**Range:** e9664ee..381b9b0 (branch `duncandoyle/platformNeutral`; design change and plan from c2c52b8)

## Summary
The client-visible contracts are platform neutral before the first release (D-016, design §14), with Kubernetes behaviour unchanged:
- **Install IDs:** opaque and URL-safe (`/api/v1/installs/{id}`, `/i/:id`, Kubernetes `<namespace>.<name>`). Invalid provider IDs are excluded, logged and reported by `/readyz`.
- **Optional fields:** platform-specific install fields are optional, with a neutral `readiness`.
- **`/meta`:** reports the platform plus the `scope` and `workloads` capabilities.
- **Vocabulary:** platform copy lives in an SPA vocabulary that a downstream can override.
- **Self-managed Gateways:** they get info KGWUI-010 instead of the false KGWUI-006, through a pluggable resolver that mirrors kgateway. Unready proxies, `Programmed=False` and xDS rejects still show (D-006).
- **Boundary test:** a per-file import test keeps Kubernetes types out of new core code.
- **Compatibility:** the policy says API v1 is stable from the first release, while the Go extension API may change until the seam milestone.

## Reviews
- **Design change:** D-016 and design §14, cross-model (Codex): 2 P1s, 4 P2s, a P3 and a P4, all accepted; the re-check found 1 new P2, fixed (reviews/neutrality-codex.md).
- **Plan amendment:** cross-model (Codex): 1 P1 and 5 P2s, accepted; the re-check found 1 new P2, fixed (reviews/1.3-plan-codex.md).
- **Pre-flight scan:** 30 rows, 14 findings, all ruled on before Task 24.
- **Task reviews:** per-task spec and quality reviews for Tasks 24–28. Task 26's single Important was resolved by a ruling; Task 27 needed one fix round. All others came back clean.
- **Wording pass:** an options writer plus a separate picker for the readiness labels, the landing title and the `--install` hint.
- **Whole-branch review:** most capable model. 0 Critical, 0 Important; 2 should-fix minors plus 4 cleanups, fixed in one wave and confirmed by a scoped re-review.
- **Cross-model code review:** happens at the phase review (planned sub-phase).

## Waivers
none

## Deliverables
- `pkg/platform` and `pkg/proxymode`; changes in `pkg/api`, `pkg/install`, `pkg/health`, `pkg/overview`, `pkg/kinds` (GatewayParameters), `pkg/engine`, `pkg/extension` and `cmd/kgateway-ui`.
- `internal/boundary`: the import boundary test.
- SPA: `/i/:id` routes, `useCapabilities`, `useVocabulary` and `Vocabulary` exports, and guards for absent fields.
- README stub with the compatibility policy.
- Test counts: `go test ./...` passes; Vitest 267/267; typecheck, Biome and the build are clean.

## Notes for later phases
- **Sub-phase 1.4:** Task 20 must grant get/list/watch on `gatewayparameters`. Without it the install shows unknown with KGWUI-008.
- **Standalone answer (design §14.8):** file mode will synthesise Kubernetes metadata. kube-lite needs a static install definition (proposed `--install-file`), which is part of the deferred provider refactor.
- **Visual evidence:** /var/folders/l9/kvw9vb8121720jclk572xwpw0000gp/T/ui-loop/kgateway-ui-claude/task-26/ (local only).

## Deferred / open
- Refactors in design §14.4 wait for the standalone PoC: provider interface, neutral `Install` and `Detector` (before the seam milestone), metrics by URL, injection in `app.Build`, and kinds without discovery.
- `Backend` kind: Phase 2 design.
- Gaps found after the final review and not fixed:
  - The Task 28 boundary test skips `web`, `docs` and `bin` at any depth, so a core package in such a folder is never scanned.
  - A dot-import of `pkg/kinds` evades its discovery-client check.

## Rulings made during execution
- - PF1 T24 api_test.go:201 comment — Ruling: T24 updates it too — trivial — none.
- - PF2 T24 KGWUI-005 message loop vacuous — Ruling: assert the finding exists before checking wording — test hygiene — none.
- - PF3 T24 Platform.Name=="" default — Ruling: Kubernetes default only when the whole Platform is the zero value (IsZero), never on name — D-016 "capabilities, not mode strings" — low.
- - PF4 T24 new backend messages skip wording pass — Ruling: Task 27's wording pass also picks "Controller not ready"/"Proxy not ready"; T27 updates the Go strings and their tests — keeps one wording pass — a second small edit in Go.
- - PF5 KGWUI-005 keeps an apps/Deployment ref without workloads — Ruling: allowed; only the Kubernetes provider (which has workloads) produces Deployment-backed installs, so the combination cannot occur in Phase 1 — cost if wrong: a stray "Deployment" label in a future provider, caught when the provider seam lands.
- - PF6 T25 "ClusterRole already grants" — Ruling: reword to "Task 20 grants" — none.
- - PF7 T25 RBAC-denied GatewayParameters → install unknown vs Review Focus 12 promising KGWUI-006 — Ruling: keep T25 behaviour (denied kind → KGWUI-008, unknown, same as every other denied kind); T25 updates Review Focus 12 and design §14.3 H5 to say so — consistent with D-014 — cost: a local run with a kubeconfig lacking GatewayParameters access shows unknown (explained by KGWUI-008).
- - PF8 T26 pages.test.tsx:307 installId — Ruling: edit by hand — none.
- - PF9 T26 InstallPage.test.tsx:267 optional scope assignment — Ruling: cast-and-spread form — none.
- - PF10 T26 Header unused ResourceName import — Ruling: remove it — none.
- - PF11 T26 H3 test passes while /meta loading — Ruling: wait for a /meta-dependent element first — test hygiene — none.
- - PF12 T26 synchronous asserts in InstallPage.test.tsx:155, Shell.test.tsx:133 — Ruling: findBy/waitFor — flake risk — none.
- - PF13 T26 skeleton reads meta.capabilities unguarded — Ruling: use useCapabilities() and add a no-scope skeleton test — none.
- - PF14 T27 claim that typecheck rejects `vocabulary` key — Ruling: drop the claim — none.
- Task 26: Ruling: Header does not call decodeInstallId on route params (deviation from brief) — React Router 7 already decodes useParams/useMatch, so a second decode corrupts IDs with % escapes; only DocumentTitle (raw pathname) decodes, with a test — cost if wrong: a mis-decoded title for exotic IDs.
- Task 26: Ruling: SHELL_API_VERSION stays 1 — nothing has been released and H11 excludes the Go/extension API from the compatibility promise until the seam milestone — cost: a downstream built pre-release must rebuild.
- Task 26: Ruling: RootRedirect empty-state copy is Task 27's (vocabulary) — the brief's scope — none.
- Task 26: Ruling: the Step 9 wording pass (Important) moves to Task 27's single wording pass, which already lists the three readiness labels and now also the two backend messages (PF4) — implementers may not spawn subagents, so the controller dispatches the options writer and the separate picker during Task 27 — cost: Task 27 edits Header.tsx strings and the H3 expectation if the pick differs.
- Task 27: Ruling: the pin hint must change (Important "unrequested copy change") — H1 makes --install accept the install ID, so "--install namespace/name" alone is incomplete; use the picked wording "To always open one install, start the UI with --install <id> or --install <namespace>/<name>." — cost: one string.

## Deferred minor findings (for the phase review)
- Task 24: minor (deferred): engine `rejected` map never pruned; a rejection that recurs after disappearing is not logged again
- Task 24: minor (deferred): api_test.go failure message claims "over three computations", not guaranteed by the test
- Task 24: minor (deferred): ReadinessUnknown is defined but never produced
- Task 24: minor (deferred): Discover error text always says "Deployment"
- Task 25: minor (deferred): 19 files vs ~15 in the brief (4 SPA files + proxymode_test.go), each justified — record in the 1.3 close
- Task 25: minor (deferred): proxymode Unresolved doc says kgateway reports InvalidParameters, wrong for a missing GatewayClass
- Task 25: minor (deferred): nil ProxyMode default lives in two places (finding.go, extension.go), as the brief requires
- Task 26: minor (deferred): header shows "Controller ready" until /meta loads, then "Controller: N of M ready" (layout shift)
- Task 26: minor (deferred): install-level finding shows the raw ID until /installs loads
- Task 26: minor (deferred): skeleton reserves the scope tile by capability, CountTiles renders it by header.scope (one-tile shift possible)
- Task 27: minor (deferred): H4 override test covers only connecting and noInstallsTitle, not noInstallsHelp/pinHint/copyName
- Task 27: minor (deferred): RootRedirect joins noInstallsHelp and pinHint with a literal space
- Task 28: minor (deferred): web/docs/bin skip list applies at any depth (pkg/web would never be scanned)
- Task 28: minor (deferred): dot-import of pkg/kinds evades the discovery-client check
- Task 28: minor (deferred): discarded errors from filepath.Rel / strconv.Unquote in imports_test.go
- Task 28: minor (deferred): selector-name matching could false-positive on a local named kinds
- Final review: minor (deferred): "Proxy deployment not found" keeps Deployment wording without workloads — for the provider seam
- Final review: minor (deferred): --install namespace/name cannot pin a custom-ID install
- Final review: minor (deferred): explicit undefined in a downstream vocabulary override erases the default
