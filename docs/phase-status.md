# Phase status

## Phase
Phase: 6 — Traffic Flow & Backends
Base: 657378ad
Docs: docs/phases/phase-6/
Status: implementing (design and plan approved 2026-11-10; plan approved on condition that Claude and Codex agree, which they did). Branch `duncandoyle/phase6`, worktree `../snellius-phase6`. Field research done (private); proxy endpoint view after GA (D-069, #35)

### Queue
Tasks 19–50 of `phases/phase-6/implementation-plan.md`, one sub-phase at a time:
- [ ] 6.1 cache access for kgwctl (Tasks 19–21) ← CURRENT
- [ ] 6.2 path model, paths API, path list (Tasks 22–32)
- [ ] 6.3 backends (Tasks 33–39)
- [ ] 6.4 route trace, all route kinds (Tasks 40–42)
- [ ] 6.5 Gateway map, core (Tasks 43–47)
- [ ] 6.6 Gateway map, the rest; phase close (Tasks 48–50)

### Exit gates
- [ ] tests/vet pass (observed)
- [ ] all queue items done or deferred
- [x] design+plan cross-model review + triage (reviews/design-codex.md, reviews/plan-codex.md)
- [ ] phase cross-model review + triage
- [ ] decisions.md / design / plan in sync with code
- [ ] snellius.dev matches the user-visible changes (program P-006; `npm run sync:findings` for finding codes)
- [ ] phase-6-complete.md written, roadmap updated, tracked items closed

## Sub-phase
Sub-phase: 6.1 — Cache access for kgwctl (planned)
Base: 5704f91
Brief: phases/phase-6/phase-6.1-brief.md

## Program
Program file: https://github.com/DuncanDoyle/kgateway-tooling/blob/main/program.md
Waiting on: none (program file read 2026-11-10; Phase 6 needs no open edge. It answers kgwctl's E2 requests #18 and #19)
Consumers: E2 (kgwctl Phase 1), E3 and E8 (kgwctl Phases 5 and 2): delivered in `v0.3.0` (2026-11-05, kgwctl#3), updated in `v0.4.0` (kgwctl#4), `v0.5.0` (module `github.com/DuncanDoyle/snellius`, kgwctl#5) and `v0.6.0` (2026-11-09, generic core, kgwctl#7). `v0.6.0` breaks E8 on purpose: controller metrics reach the engine only through `setup.Config.Collected` (`ctrlmetrics.CollectOnce` + `Publish`), kgwctl#6. The program file is updated (kgateway-tooling `ca7f2dd`). kgwctl's E2 requests #18 and #19 are for this phase (D-056). E7 (kgwctl Phase 6, policy model under `pkg/`) is UI Phase 7. An informational kgwctl compile job reports on every PR, never blocks.
Open `cross-repo` issues (2026-11-10): here #15, #21 (xdsview E4/E5, for Phase 11), #18, #19 (kgwctl E2, Phase 6), in kgwctl #1–#7 (notices from xdsview and this repo, consumer side).
Provided to this repo: E4 and E5 from xdsview `v0.3.0` (renamed from krp; #15, #21) are for UI Phase 11.

## Carry-over (deferred to later phases)
- (P3) REMIND USER at the start of each phase: file the upstream kgateway issue "Expose the controller's effective discovery scope through a read-only endpoint" (H9, D-009 §7). User chose to file it in a later phase (2026-10-20). Draft kept outside the repo (public repo). Before filing: confirm on a cluster that status stays after a namespace is deselected, and re-verify the file:line refs against kgateway main.
- (P3) Scope verification for experimental Gateway API kinds: switch KGWUI-007 to the controller's enabled kinds once the upstream scope endpoint exists (D-017 §4) — src: phases/phase-1/reviews/phase-codex.md#5
- (P3) Watch CRDs added or removed after startup without a restart — https://github.com/DuncanDoyle/kgateway-ui/issues/1
- (P3) Per-user AuthN/AuthZ behind `api.Authorizer` (D-013) — roadmap — src: phases/phase-1/phase-1.4-complete.md
- (P3) kgateway#14998 (upstream): once the controller reports a selector that matches nothing, the 3.5 S7 computed rule can go — src: phases/phase-3/phase-3.5-complete.md
- (P3) R59: proxy Pod ownership by UID needs a ReplicaSet watch; the name check (R50) stays until then — src: phases/phase-3/phase-3.2-complete.md
- (P3) D-058 item 1: 023 proxy membership evidence needs a port-aware reverse mapping (xdsview `origin`, Phase 11) — src: phases/phase-3/phase-3.4-complete.md
- (P3) D-058 item 6: DirectResponse status is not judged (Phase 7) — src: phases/phase-3/phase-3.4-complete.md
- (P3) Dense memory: peak live memory at the default edge cap is logged, not gated (D-039). Retained heap is +39.3 of 48 MiB (D-057); Phase 5 must keep the shared cache in the core — src: phases/phase-3/reviews/3.0-gate-codex.md#2
- (P3) A live `warm_withheld` capture for KGWUI-024's error; on kind the gauge never outlasts a scrape — src: phases/phase-3/phase-3.4-complete.md
- (P3) README Install: say that installing the chart needs rights to create a ClusterRole and ClusterRoleBinding (usually cluster-admin), since Kubernetes only lets you bind permissions you hold. Next release (2026-11-09)
- (P4) Watch upstream kgateway#14815 (open, checked 2026-11-10 at Phase 6 start): GatewayExtension gains an `Accepted` condition with observedGeneration. No phase waits on it. At each phase start, check whether it merged; if so, read the condition when present (an error finding for an in-use extension, as KGWUI-002 does for Backends), and show "not available" when absent
- (P4) Neutral `Install` and `Detector` (design §14.4 refactors) — Phase 5 G1 (D-061)
- (P4) kgateway policy ownership, ListenerPolicy CA refs and Backend judgement stay in core packages, inert without kgateway kinds; move behind pack hooks in Phases 6 and 7 — src: phases/phase-5/design.md §4.1, reviews/design-codex.md#4
- (P4) Optional `kind-main` e2e job: run on manual dispatch with `kgw_main_version`; chart source unverified. Its 016-from-status expectation (3.5 S9) is unverified too — src: phases/phase-1/phase-1.4-complete.md, phases/phase-3/phase-3.5-complete.md
- (P4) 2.5 controller log captures; 2.5 TLSRoute SNI conflict handling in view A — src: phases/phase-3/phase-3.4-complete.md
- Smaller Phase 2 and Phase 3 items (parked tasks, test gaps, layout at 720 px, P4 polish) — src: phases/phase-2/phase-2-complete.md and phases/phase-3/phase-3-complete.md, Deferred / open

## Notes
- [x] Q 5.7 (planned, before Phase 6): checks report what they are waiting on (E9, #33, D-068) — closed 2026-11-10, merged in #34, pre-release `v0.7.0-alpha.1`, phases/phase-5/phase-5.7-complete.md. snellius.dev syncs at `v0.7.0`
- 2026-11-09: Phase 5 closed (phases/phase-5/phase-5-complete.md), merged in #31 and released as `v0.6.0`. Phase 6 is next; read the program file and set Base when it starts.
- [x] Q 4.3 (Phase 4 stabilization, v0.5.1): data age reads the server's clock, so cluster clock skew no longer fakes or hides staleness — closed 2026-11-08, phases/phase-4/phase-4.3-complete.md
- 2026-11-07: Phase 3 closed (phases/phase-3/phase-3-complete.md). D-061 adds Phase 4 (Snellius rebrand) and Phase 5 (Generic Gateway API core); the old Phases 4–11 are now 6–13. Phase 4 is pre-approved; set Base when it starts, then write its design.
- The first release tag freezes API v1, install IDs and SPA URLs; make the ghcr packages public first (phase-1-complete.md).
- The repo is public: per the public-repo hygiene rules, vendor and commercial edition names must not appear in code, file names, commit messages or PR text. Refer to them as "downstream distributions".
