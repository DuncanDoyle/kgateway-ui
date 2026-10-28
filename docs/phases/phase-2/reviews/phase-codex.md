Cross-model: YES (author: Claude Opus 5.5, reviewer: Codex, command: codex exec --sandbox read-only)
Scope: phase 2
Diff: 6b5dcc2..77e4363
Timing: on time

Found **8 new findings** in `6b5dcc2..77e4363`.

1. **P1 — Delegation rows remain “checked” after their own poll fails.**  
   [DelegationTab.tsx:617](/Users/ddoyle/Development/claude/kgateway-ui-claude/web/packages/shell/src/pages/explore/route/DelegationTab.tsx:617) retains and renders `l.data`, but never evaluates its `collectedAt` or supplies a stale context. If route-detail polling succeeds while `/delegation` fails, the notice appears while retained rows still show agreement and healthy status icons. The enclosing `DetailFrame` tracks a different request. This breaks the freshness behavior established in 2.2 and design §8.6.

2. **P1 — Another install’s status suppresses an install-local orphan warning.**  
   [check.go:1201](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/computed/check.go:1201) treats any entry with a matching `controllerName` as disqualifying; [check.go:1144](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/computed/check.go:1144) uses that result for the orphan predicate. Let install A’s wildcard parent drop C everywhere, while install B serves C through Q. If both installs share the controller name, Q’s entry suppresses A’s KGWUI-011. Design §6.2 and rules §9.3 explicitly make this conclusion install-local; the entry must belong to an eligible parent owned by A.

3. **P1 — A hidden parent suppresses an independently visible `listener-unknown` fact.**  
   [rows.go:1180](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/explore/rows.go:1180) unions dependencies from every unknown parent into one set; [query.go:372](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/explore/query.go:372) requires that entire union to be visible. With two accepted-but-unknown parentRefs, denying parent B removes the flag established by visible parent A. Consequently, `parent=A&listener=_unknown` can include the route while `state=listener-unknown` excludes it. Preserve separate dependency sets and accept any visible supporting fact, as `dropped` already does.

4. **P2 — The edge-cap fallback overrides unknown-scope precedence.**  
   [details.go:600](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/api/details.go:600) unconditionally labels a cut ref `too-large`. Selection runs before the scope-unknown traversal shortcut, so an unknown-scope install can still populate `TooLarge`. Earlier refs then show `scope-unknown`, while cut refs show `too-large`, including in the SPA. Rules §3.3 requires `scope-unknown` to win over every other reason.

5. **P2 — Computed-only candidates lose the required restricted-evidence timestamp.**  
   [computed.go:49](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/health/computed.go:49) populates `ResourceSince` only through `ownedResources`, which excludes declared and no-parent candidates. A computed-attached child missing status can therefore receive KGWUI-012 without `ResourceSince`. When a hidden Namespace restricts its evidence, `/health` removes `since` instead of substituting the child’s first-seen time. This misses design §7.4 rule 9 precisely for the computed-only children introduced in 2.4.

6. **P2 — The implementation plan still prescribes superseded visibility behavior.**  
   [implementation-plan.md:1535](/Users/ddoyle/Development/claude/kgateway-ui-claude/docs/phases/phase-2/implementation-plan.md:1535) says hidden dependencies suppress listener counts and Gateway `routes`; lines 1545–1548 prescribe tests for restricted counts and listener-count evidence. D-023’s amendment, the current design, and the code require those listener facts to remain visible. These instructions could reintroduce the behavior deliberately removed during 2.3. This is documentation drift, not a repeat of the triaged authorization findings.

7. **P3 — The TypeScript golden guard checks keys, not wire value types.**  
   [types.golden.test.ts:485](/Users/ddoyle/Development/claude/kgateway-ui-claude/web/packages/shell/src/api/types.golden.test.ts:485) verifies required keys and nested shapes, but never validates scalar types, enum values, or array-versus-object distinctions against the declared TypeScript types. `Spec<T>` retains only keys and optionality. Thus this guard can pass incompatible DTO value types despite its contract-checking role. Add value-level validation for the exported API shapes.

8. **P4 — Phase 2 discards stable check identity before exposing findings.**  
   [computed.go:45](/Users/ddoyle/Development/claude/kgateway-ui-claude/pkg/health/computed.go:45) converts a check with a structured identity into a finding without retaining that identity. Multiple KGWUI-012 findings can share code and resource; the SPA distinguishes them using mutable message/detail text ([TopFindings.tsx:43](/Users/ddoyle/Development/claude/kgateway-ui-claude/web/packages/shell/src/pages/install/TopFindings.tsx:43)). Phase 3 diagnostics and downstream renderers will otherwise need to parse prose or use occurrence indexes. An optional stable finding ID would preserve API v1 compatibility.

No additional API v1 path/body-shape break or forwarding defect in `WithComputedRules`, `CorePages` fallback, or `app.Run` was identified. Files were unchanged; findings are source-traced, and the supplied gates were not rerun.

## Triage
| # | Finding | Source | Decision | Note |
|---|---|---|---|---|
| 1 | P1 Delegation tab keeps showing checked rows after its own poll fails | reviewer | accept | Fixed in 2.5: [`45baa25`](https://github.com/DuncanDoyle/kgateway-ui/commit/45baa25), [`6670b0a`](https://github.com/DuncanDoyle/kgateway-ui/commit/6670b0a), [`0d07532`](https://github.com/DuncanDoyle/kgateway-ui/commit/0d07532) |
| 2 | P1 Another install's status suppresses an install-local orphan KGWUI-011 | reviewer | accept | Fixed in 2.5: [`7f6ee7b`](https://github.com/DuncanDoyle/kgateway-ui/commit/7f6ee7b) |
| 3 | P1 A hidden parent suppresses a visible `listener-unknown` flag | reviewer | accept | Fixed in 2.5: [`12be26a`](https://github.com/DuncanDoyle/kgateway-ui/commit/12be26a) |
| 4 | P2 Edge-cap `too-large` overrides `scope-unknown` | reviewer | accept | Fixed in 2.5: [`197101b`](https://github.com/DuncanDoyle/kgateway-ui/commit/197101b) |
| 5 | P2 Computed-only candidates lose `ResourceSince` under restricted evidence | reviewer | accept | Fixed in 2.5: [`dbff6b2`](https://github.com/DuncanDoyle/kgateway-ui/commit/dbff6b2), [`6b04ccd`](https://github.com/DuncanDoyle/kgateway-ui/commit/6b04ccd) |
| 6 | P2 Implementation plan still prescribes restricted listener counts | reviewer | accept | Fixed in 2.5: [`a5ea505`](https://github.com/DuncanDoyle/kgateway-ui/commit/a5ea505) |
| 7 | P3 TS golden guard checks keys, not value types | reviewer | accept | Fixed in 2.5: [`97d02d0`](https://github.com/DuncanDoyle/kgateway-ui/commit/97d02d0), [`8d771b1`](https://github.com/DuncanDoyle/kgateway-ui/commit/8d771b1), [`7c63cb8`](https://github.com/DuncanDoyle/kgateway-ui/commit/7c63cb8) |
| 8 | P4 Findings drop the check's stable identity | reviewer | defer: Phase 3 planning | An optional stable finding ID keeps API v1 compatible; Phase 3 diagnostics need it |
