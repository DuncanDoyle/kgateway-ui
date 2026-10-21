# Phase 1.5 — Phase review fixes (Complete)

**Completed:** 2026-10-21
**Range:** a38fe3c..HEAD (branch `duncandoyle/stabilize`; base acee415)

## Summary
Fixes for the Phase 1 cross-model review (reviews/phase-codex.md, Q9–Q17) and for the review of this sub-phase (reviews/1.5-codex.md, Q18–Q22), with decisions D-017 and D-018:
- **Health:** workloads at 0 desired replicas are not ready (controller red KGWUI-005, proxy warning KGWUI-006); owned ListenerSet status is required; a missing proxy keeps its check pending during grace; status-less experimental-only objects warn after grace with a hint that experimental features may be off.
- **Scope:** duplicate selector env entries are last-wins; `envFrom` without an explicit entry makes scope unknown; the scope verifier compares stable kinds only, classified by controller version (kgateway 2.3 gates all TCPRoute, 2.4+ watches v1).
- **RBAC:** the ClusterRole no longer reads ReferenceGrants or CRDs.
- **YAML view:** polls like the other pages and shows its data age from a `Collected-At` header; the v1 body is unchanged.
- **Wire format:** every API time is UTC, converted at the API boundary so internal clocks keep their monotonic reading; golden fixtures pin the v1 JSON independently of the Go types, and the SPA's TypeScript types are checked against them.
- **README:** the extension example is a compiled program (`examples/downstream`) that the README must match.

## Reviews
- **Cross-model (Codex) of the sub-phase diff:** 2 P1, 1 P2, 2 P3, all accepted (Q18–Q22). Scoped Codex re-review: 3 resolved; 2 remainders, one rejected and one deferred, both decided by the user (reviews/1.5-codex.md).
- **Per-group reviews (same model family):** groups A–E and the final wave each had a spec and quality review; groups A, D and the final wave needed a fix round. Group C was reviewed by the controller (small diff).
- **Wording pass:** an options writer plus a separate picker for the zero-replica, pending-proxy and experimental-hint strings.

## Waivers
none

## Deliverables
- `pkg/health`, `pkg/install`, `pkg/scopeverify`, `pkg/kinds` (experimental-only classification), `pkg/api` (UTC at the boundary, `Collected-At`), `web/packages/shell` (YAML polling and age).
- `pkg/api/testdata/golden` and the TypeScript wire check.
- `deploy/helm` ClusterRole and chart test; `examples/downstream`.
- Design §4.4, §5.2, §6.6, §7 and §8 updated; D-017, D-018.
- Gates observed on the final commit: `go vet` (also with tag `e2e`), `go test ./... -race`, `pkg/api` under `TZ=Europe/Amsterdam`, chart tests, golangci-lint 0 issues; Vitest 281/281, typecheck, Biome and build clean; no forbidden terms.
- kind e2e: could not run locally (kind node failed to boot twice; Docker at 4 CPU / 8 GB with two other clusters). The PR's CI runs the e2e matrix.

## Notes for later phases
- The upstream scope endpoint should report enabled kinds; the verifier and the experimental hint switch to it (D-017 §4, D-018).
- Q16 evidence of the YAML freshness fix: /var/folders/l9/kvw9vb8121720jclk572xwpw0000gp/T/ui-loop/kgateway-ui-claude/q15-yaml-freshness/ (local only).

## Deferred / open
- Stricter metric comparison (gauge equals stable or stable plus experimental counts), until the upstream enabled-kinds endpoint lands.
- Downstream installs and unparsable versions get the conservative experimental classification; letting a detector report its base kgateway version needs a new `Install` field (seam milestone).

## Rulings made during execution
- Group A — Ruling: experimental-only kinds listed in one place in `pkg/kinds` — reused by health and the verifier — none.
- Group A — Ruling: status-less experimental objects skipped — later overturned by the user (D-018) after the 1.5 Codex review showed a false green.
- Group A — Ruling: the reviewer's claim that experimental features are off by default was wrong; kgateway defaults them on (`api/settings/settings.go`) — verified at both tags — none.
- Group B — Ruling: the verifier skips by (namespace, gauge label) because kgateway labels ListenerSet v1 and XListenerSet alike — only loses coverage, never a false KGWUI-007 — none.
- Group D — Ruling: the collection time travels in a `Collected-At` header — keeps the v1 body — none.
- Final wave — Ruling: UTC conversion moved from the time sources to the API boundary — `.UTC()` drops Go's monotonic reading — none.
- Final wave — Ruling: `useResource` drops `isLoading`/`isValidating` — the shell package is unpublished — a downstream reading them must change.
- Final wave — Ruling: any `envFrom` may set the selector, so scope is unknown — conservative per D-009 — a false unknown on unusual prefixes.
- Wave 2 — Ruling: kgateway 2.4 and later use the 2.4 classification; below 2.4 the 2.3 rules; unknown versions conservative — cost if wrong: a missing hint on a future minor that changes gating.
- D-017 §1 — Ruling: an unready proxy (fewer ready than desired) stays an error; only 0 desired is a warning — the question to the user had misdescribed today's behaviour; corrected in D-017.

## Deferred minor findings
- README example test does not run `main` or check its error handling (reviews/1.5-codex.md R-5).
- The README sync test is exact-match on the example file; harmless but strict.
- Only the `/health` handler applies `wireHealth`; a new handler writing a health report must use it too.
- Golden TS check compares keys only; value types rest on the Go goldens.
- Experimental routes whose only status is from another controller stay unjudged (general assignment rule).
- Possible flake: `pkg/app` `TestFirstMetricsCollectionFollowsDiscovery` timed out once at 5 s under parallel load.
