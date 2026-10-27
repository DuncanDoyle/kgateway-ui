# Sub-phase 2.4 — Wording (Task 67)

The final copy of views B and C on screen and the 2.4 server text (design §8.8). SPA strings live in `web/packages/shell/src/copy/explorer.ts`; the comment ids there match this table. SPA ids run S188–S237 (S200 retired) plus S264; S129–S131 are the view B reasons deferred from 2.3. Server ids run S238–S263.

Badge state texts, row reasons and notes carry no trailing period: the compact tooltip joins its parts with `". "` (`explore/ComputedBadge.tsx`) and `joinNotes` joins notes the same way (`pkg/api/explain.go`). Fit clauses hold no `;`, since a partial row joins its reason and clause with `"; "`. Rule and ref numbers on screen count from 1; the wire keeps 0-based indexes.

## Routes list and candidate banner

| id | Text | Where |
|---|---|---|
| S188 | selects it through a delegation ref. | Candidate banner tail after S94 and the parent's name |
| S189 | Selected but not attached | Routes list flag `dropped` (covers dropped, forbidden and rejected-hostnames) |

## Computed badge

| id | Text | Where |
|---|---|---|
| S190 | delegation | Rule `delegation`, ends S112 with family S114 |
| S191 | effective matches | Rule `effective-matchers` |
| S129 | Unknown: the parent route has no path from a Gateway or ListenerSet | Reason `not-reached` |
| S130 | Unknown: part of the path above is unknown | Reason `context-unknown` |
| S131 | Unknown: too large to compute | Reason `too-large` |
| S233 | The reason without "Unknown: ", capitalised (e.g. "Too large to compute") | Delegation tab Why cell of an unknown row; the badge keeps the full S129–S131 text |

## Delegation tab

| id | Text | Where |
|---|---|---|
| S192 | Delegation | Tab (fixed: design §5.3, §8.5, `?tab=delegation`) |
| S193 | Outcome | Outcome filter group and column header |
| S194 | Rule | Rule filter label |
| S195 | Ref | Ref filter label |
| S196 | All rules | Rule filter option |
| S197 | All refs | Ref filter option |
| S198 | Rule `<n>`, ref `<m>` | Ref section title and table name (the parent's rule and ref, from 1) |
| S199 | Show rows / Hide rows | Ref section toggle |
| S201 | This ref selects no routes. | Unfiltered ref with no rows |
| S202 | No rows match these filters. | Filtered ref with no rows |
| S203 | Attached | Outcome `attached` |
| S204 | Partly attached | Outcome `partial` |
| S205 | Dropped | Outcome `dropped` (route outcome only; view C uses S229) |
| S206 | Not permitted | Outcome `forbidden` |
| S207 | Rejected: has hostnames | Outcome `rejected-hostnames` |
| S208 | Cycle | Outcome `cycle` |
| S209 | Not resolved | Outcome `unresolved` |
| S210 | Unknown | Outcome `unknown` (fixed: 2.3 S162) |
| S211 | You cannot view this ref's full selection | Route cell of a selection-restricted row |
| S212 | No route | Route cell of an unresolved row |
| S213 | `<Type>: <Status>` (e.g. "Accepted: True") | Child's entry cell (fixed: 2.2 condition line) |
| S214 | No status entry | Child's entry cell and fact with no entry |
| S232 | No condition reported | "Parent's condition" fact with no condition |
| S215 | Child's status entry | Column header and fact label |
| S216 | Parent's condition | Fact label |
| S231 | Paths | Expander column name (screen readers only) |
| S217 | Show paths (`<n>`) / Hide paths (`<n>`) | Row expander button name; `aria-expanded` carries the state |
| S264 | Paths (`<n>`) | Open path list name |
| S219 | More paths not shown. | `contextsTruncated` note (Delegation tab and view C) |
| S220 | No paths shown. This list is not complete. | Row or edge with no kept path |
| S221 | A route you cannot view | Label on a hidden ancestor (path step and up-chain) |
| S222 | Selected: `<n>` | Ref count (rules list and ref section) |
| S223 | Attached: `<n>` | Ref count |
| S234 | Visible selected: `<n>` · Visible attached: `<n>` | Ref counts while the ref's badge is Hidden |
| S224 | Selected can include routes from an earlier ref in this rule | Extra line in the ref badge's tooltip |

## Effective matches (view C)

| id | Text | Where |
|---|---|---|
| S225 | Effective matches | Section title |
| S226 | From | Card lead before the parent route, and its `aria-label` |
| S218 | Parent match | Label of the match the parent delegated under |
| S227 | Matches as written, inside the parent's match | Mode `default` |
| S228 | Rule matches extend the parent's match | Mode `inherit` |
| S229 | Fits / Does not fit | A child rule under a parent match |
| S230 | More parent routes not shown. | Edge list cut |

## Server text: row reasons and notes

`pkg/api/explain.go`. Shown in the Why cell and on the path lines.

| id | Text | Where |
|---|---|---|
| S238 | Attached on some paths only | Reason `partial`, then `"; "` and the first failing fit clause |
| S239 | The route's parentRefs do not permit this parent | Reason `forbidden` |
| S240 | A delegated route may not set hostnames | Reason `rejected-hostnames` |
| S241 | A delegation loop fails this ref | Reason `cycle` |
| S242 | No such route | Reason `unresolved`, missing target (same words as S91) |
| S243 | Namespace not watched | Reason `unresolved`, unwatched target (same words as S32) |
| S244 | No computed path attaches it. Others not computed | Reason of an `unknown` row with a dropped path |
| S245 | Some paths not computed | Note `SomeNotComputed` |
| S246 | Status cannot confirm this path | Note `Unconfirmable` (fixed: design §5.2, rules §8.6) |
| S247 | the route's rule `<n>`: `<clause>` | Fit prefix; `<n>` is the child's rule, from 1 (`f.Rule + 1`) |
| S248 | the parent's path match is `<type>`, not PathPrefix | Fit clause |
| S249 | the parent has no path match | Fit clause |
| S250 | path `<child>` is outside the parent's `<parent>` | Fit clause |
| S251 | no matches, so it defaults to /, which is outside the parent's `<parent>` | Fit clause |
| S252 | the parent needs `<noun>` `<value>`, the rule has `<value>` | Fit clause |
| S253 | the parent needs `<noun>` `<value>`, the rule has none | Fit clause |
| S254 | an empty match list fits no parent match | Fit clause |
| S255 | header / query parameter / method | Fit nouns in S252–S253 |

Fit clauses carry no backticks; in this table they mark placeholders only. View C shows the clause without the S247 prefix, since its card already names the rule.

## Server text: check note

| id | Text | Where |
|---|---|---|
| S256 | Some paths not computed | Note `Incomplete` (`pkg/computed/check.go`), joined with the rule-set note |

## Server text: findings

`pkg/health/messages.go` and `pkg/health/computed.go`. S258 and S259 name the parent without backticks, like S98 (ruling 4). S261 and S262 put names and matches in backticks.

| id | Text | Where |
|---|---|---|
| S257 | A parent route does not attach this route | KGWUI-011 message, computed and orphan triggers |
| S258 | The UI computes that HTTPRoute `<parent>` drops this route: no rule fits the parent's matches. | KGWUI-011 computed detail, dropped; the name has no backticks in the text (ruling 4) |
| S259 | The UI computes that HTTPRoute `<parent>` selects this route, but the route's parentRefs do not permit that parent. | KGWUI-011 computed detail, forbidden; the name has no backticks in the text (ruling 4) |
| S260 | The UI computes that no parent route in this install keeps this route. The route has no parentRefs. | KGWUI-011 orphan detail |
| S261 | Status entry for parent HTTPRoute `<parent>` at `<match>`: computed `<x>`, controller reports `<y>`. | KGWUI-012 detail, `child-entry` |
| S262 | ResolvedRefs under parent `<key>` at `<match>`: computed `<x>`, controller reports `<y>`. | KGWUI-012 detail, `parent-resolvedrefs` |
| S263 | an entry / no entry / Accepted=`<status>` | Values in S261 (`entryText`) |

In S261 and S262, `<match>` is the match the ancestor delegated under, worded as the SPA's match line ("PathPrefix /api Header x-env Exact prod"). Distinct matches are joined with " or ", at most three, then " or more". With no match known, " at …" is left out: for a Gateway or ListenerSet key in S262, or when no context was kept. S179 " Rule set …" and the note follow.

## FIXED strings

| id | Text | Source |
|---|---|---|
| S192 | Delegation | Design §5.3 and §8.5, brief row 51, URL `?tab=delegation` |
| S210 | Unknown | 2.3 compact state S162 and Phase 1 `stateLabel` |
| S213 | `<Type>: <Status>` | 2.2 condition line format (`ConditionList`, S12 family) |
| S246 | Status cannot confirm this path | Design §5.2 and rules §8.6 quote it; brief row 51 |

## Rulings

1. **`allows` labels (S235–S237) are dropped for 2.4.** No component renders the row's `allows`, and the one case that matters (forbidden) already shows as S206 with S239's reason. They are removed from the copy and from the Task 67 step in the plan. If they are rendered later, use "Allows any parent route", "Allows this parent" and "Allows other parents only".
2. **The open path list is named "Paths (`<n>`)" (S264)**, a new string, not S217. `aria-expanded` on the S217 button carries the open state, so the list name stays out of the toggle text.
3. **Fit clauses S248–S254 carry no backticks.** The Why cell and path lines render text as is, so backticks showed literally. The KGWUI-012 details S261–S262 keep theirs, because the findings view renders them.
4. **S258 and S259 name the parent without backticks.** The pick assumed the shipped S98 quotes the name in backticks; it does not ("No status entry for parent HTTPRoute shop/api"). A computed finding on the same (route, parent) is merged into the S98 finding, so its detail must quote the name the same way. S260–S262 are never merged with S98: the orphan has no parent, and KGWUI-012 is its own finding.

## Accuracy fixes

| id | Was | Why it changed |
|---|---|---|
| S189 | Dropped by a parent route | Wrong for forbidden and rejected-hostnames |
| S211 | Routes not shown | Implied hidden routes exist; the same row also stands for "selects nothing" (design §7.4 rule 4c) |
| S220 | No paths kept. More paths exist than the UI computed. | Claimed more paths exist, which is not known |
| S241 | The route is already on this path | A cycle fails the ref and its siblings, so the row's own route need not be on the loop |
| S244 | Dropped on the paths computed; other paths not computed | Held a `;` inside a `"; "`-joined cell and read as the Dropped outcome |
| S247 | rule %d (0-based) | Printed the child's rule from 0 next to the parent's 1-based "Rule N, ref M" |
