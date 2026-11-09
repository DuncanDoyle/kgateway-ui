Cross-model: YES (author: Claude Opus 5.5, reviewer: Codex, command: codex exec --sandbox read-only)
Scope: phase 5, post-merge (c7e6c73, 20275b3, bf3b4d7)
Diff: 0e74043..bf3b4d7, plus c7e6c73 from the PR
Timing: retroactive (after the merge and v0.6.0)

- **P2 — New failures in an interrupted pass are discarded.** `test/compat/compat.go:69`: `ctx.Err() != nil` replaces *all* current errors with `last`. For example, an earlier pass reports “not synced”; the next detects the default build incorrectly claiming the controller, then its installs request times out. The real claim failure disappears. This cannot turn failure into success, but can hide its cause. The regression test meaningfully covers retaining an earlier error when the next request hangs; it does not cover preserving fresh assertion failures alongside timeout errors.

- **P2 — The UI does not list individual ReferenceGrants.** `docs/findings.md:242`: the new sentence promises a list users could inspect before creating a grant. `web/packages/shell/src/pages/install/CountTiles.tsx:72` renders only a count and kind label, without navigation. `pkg/api/handlers.go:67` exposes individual resources by known name, but no ReferenceGrant collection endpoint. The backend reads grants and the UI counts them; users still need an external command to find existing grants and compare their `from`/`to`.

- **P2 — End-to-end prerequisites omit required executables.** `CONTRIBUTING.md:11`: Go, Node, Docker and kind are insufficient for the documented `make e2e`. `test/e2e/setup.sh:61` invokes `kubectl`, and line 66 invokes `helm`; neither is installed by the target. CI explicitly installs Helm. A contributor following this prerequisite list can fail immediately after cluster creation.

- **P4 — The close record contradicts its updated completion claim.** `docs/phases/phase-5/phase-5.6-complete.md:3`: the first private compatibility run now “passed,” while line 23 still says it “has not happened.” Line 26 also describes the CI image build as future work despite line 3 confirming success. The recorded range ends at `971323c`, excluding the `c7e6c73` harness fix on which completion relies. This leaves the sub-phase’s outstanding work and reviewed scope ambiguous.

- **P4 — The asserted documentation reconciliation remains incomplete.** `docs/decisions.md:1171`: Tasks 6 and 13 correctly correspond to the version and API-field decisions, but Task 6 still declares `Impl.Version(install.Install, collect.Snapshot)` at `implementation-plan.md:135` and repeats that contract at line 139, despite its later deviation note. Design §7.2 (`design.md:232`) retains the same obsolete hook. These contradict the actual `Version`/`Reported` split and give extension authors incompatible interface guidance.
## Triage

| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | P2: a pass the deadline cuts short drops its fresh failures | Codex | Accepted, fixed | `Settle` falls back to the previous pass only when every error of the cut pass is a timeout; `Check` wraps request errors with `%w`. Test `TestSettleKeepsAFreshFailureInACutPass` failed before ("default build has not synced yet" replaced the fresh `/meta` failure), passes after; 10× under `-race` |
| 2 | P2: the UI does not list ReferenceGrants one by one | Codex | Accepted, fixed | findings.md: the UI counts them, with the `kubectl` command to compare an existing grant |
| 3 | P2: e2e prerequisites omit kubectl and helm | Codex | Accepted, fixed | CONTRIBUTING.md lists both |
| 4 | P4: the 5.6 record contradicts its completion line | Codex | Accepted, fixed | Range ends at `c7e6c73`; notes and deferred items updated |
| 5 | P4: plan Task 6 and design §7.2 still show the old `Impl.Version` hook | Codex | Accepted, fixed | Both show `Version` unchanged and `Reported` with the snapshot (D-066) |
