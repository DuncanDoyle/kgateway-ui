# Sub-phase 3.3 — Wording (Task 70)

The final copy of the 3.3 analyzer: the 003 kinds, finding messages and details for 013–018, the ref explanations, suggested fixes, the 015 hint, the 016 and 017 computed labels, controller log signals, the grouped view, "Learn more", the controllerLogs capability reasons and the `view=groups` 400 bodies (design §7, §8.9, §10, §17). Each string had five options; a separate agent picked one, recorded in the Choice column ("kept" is option 1). Ruling R109 lets every pick stand, the doubtful ones included. SPA strings live in `diagnosticsCopy`, `fixCopy`, `groupsCopy` and `dataPlaneCopy` in `web/packages/shell/src/copy/explorer.ts`, whose comment ids match this table. The explorer's route pending reasons (`pkg/explore/rows.go`) show S475 and the S477–S480 texts too, so they follow the same case: S475 stays lower-case (R110) and the others, with "Parent not found", take sentence case. Ids run S472–S567 and continue the 3.2 range.

Option 1 is always the text before the pass. Placeholders (`%s`, `%d`, `{n}`, `{ago}` …) stay in order. Finding codes, kinds, Kubernetes reasons (`Pending`, `Unknown`, `RouteRuleReplaced` …) and wire values are unchanged. Where a row is marked "wire value", a change also changes the API value, the TS enum and the goldens. Task reports 57–69 hold first drafts for many rows; their options are reused here.

The 3.2 glossary applies: "Not known", "Not observed", "Not available", sentence-case check notes.

## KGWUI-003: messages and details per kind

`pkg/health/checks.go` and `pkg/health/freshness.go`. The message is the finding title; the detail shows under "Detail" and ends with the experimental hint or `within` text when those apply. The details start lower-case today. The apply step should pick one case for all of them, as 3.2 did for check notes.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | S472 | `pkg/health/checks.go:277` | 003 message for every not-processed kind | none | Not yet processed by the controller | The controller has not processed this yet | Not processed by the controller yet | Waiting for the controller to process this | The controller has not handled this yet || kept |
| 2 | S473 | `pkg/health/checks.go:432` | 003 message when the route's parent does not exist | none | Not attached: parent not found | Not attached: the parent does not exist | Parent not found, so not attached | Not attached because its parent is missing | The parent of this route does not exist || kept |
| 3 | S474 | `pkg/health/checks.go:433` | 003 detail for S473 | `%s` parent | parent %s does not exist | Parent %s does not exist | The cluster has no parent %s | Parent %s is missing | No object matches parent %s || 2 (sentence case for S474–S478) |
| 4 | S475 | `pkg/health/freshness.go:29`, used at `checks.go:420` and `:470` | 003 detail, never written (CRD default only, or no entry for a parent), and no-ancestor; the `:420` form adds ` for %s` | `%s` the parent entry | no status from the controller / no status from the controller for %s | No status from the controller / No status from the controller for %s | The controller wrote no status / The controller wrote no status for %s | No controller status yet / No controller status yet for %s | The controller has not written a status / The controller has not written a status for %s || kept (R110: frozen v1 detail) |
| 5 | S476 | `pkg/health/checks.go:344` | 003 detail, never written, for one listener | `%s` listener | no status for listener %s | No status for listener %s | The controller wrote no status for listener %s | Listener %s has no status | No status entry for listener %s || 2 |
| 6 | S477 | `pkg/health/freshness.go:46` | 003 detail, stale | none | status describes an older generation | Status describes an older generation | Status is for an older generation | Status is out of date: it describes an older generation | The status predates the latest change || 2 |
| 7 | S478 | `pkg/health/freshness.go:50` | 003 detail, pending: a required condition is missing | `%s` condition type | no %s condition | No %s condition | Status has no %s condition | The %s condition is missing | The controller has not set %s yet || 2 |
| 8 | S479 | `pkg/health/freshness.go:55` | 003 detail, pending: reason `Pending` | `%s` condition type | %s is Pending | %s has reason Pending | %s: Pending | %s is still Pending | Condition %s is Pending || kept |
| 9 | S480 | `pkg/health/freshness.go:60` | 003 detail, pending: status `Unknown` | `%s` condition type | %s is Unknown | %s has status Unknown | %s: Unknown | %s is still Unknown | Condition %s is Unknown || kept |

## KGWUI-003: evidence

`diagnostics/EvidenceView.tsx`, on the finding page. The freshness labels name the four kinds of design §10.1.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 10 | S481 | `copy/explorer.ts:505` | Row label for the freshness kind | none | Status | Status state | Freshness | Why | Status problem || kept |
| 11 | S482 | `copy/explorer.ts:508` | Kind `never-written` | none | The controller never wrote a status | No status written yet | The controller has not written a status | Status never written | No status from the controller || 3 (doubt: "never" overstates a state that can still change) |
| 12 | S483 | `copy/explorer.ts:509` | Kind `stale` | none | Status is for an older generation | Status is out of date | Status describes an older generation | Status lags behind the spec | Old status, written for an earlier generation || kept |
| 13 | S484 | `copy/explorer.ts:510` | Kind `pending` | none | Waiting for the controller | The controller has not finished | Status is pending | Not processed yet | The controller is still working on it || kept |
| 14 | S485 | `copy/explorer.ts:511` | Kind `no-ancestor` | none | No status entry from this controller | This controller wrote no status entry | No entry for this controller in status | Status has no entry from this controller | The controller left no status entry || kept |
| 15 | S486 | `copy/explorer.ts:513` | Row label, stale only | none | Generation | Generations | Observed generation | Generation gap | Spec generation || kept |
| 16 | S487 | `copy/explorer.ts:514` | Row value, stale only | `{observed}`, `{current}` | Status has {observed}, object is at {current} | Status is for {observed}; the object is at {current} | Observed {observed}, current {current} | Status written for {observed}, object now at {current} | {observed} in status, {current} on the object || kept |

## KGWUI-013 and 014: explanations and evidence

Explanation 1 is the finding message when discovery says the ref's group does not serve its kind (`pkg/health/refs.go`). Explanation 2 keeps the controller's reason message (`reasonMessages`, unchanged). The evidence shows the refs the condition may concern.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 17 | S488 | `pkg/health/refs.go:16` | 013/014 message, explanation 1, one kind (design §7.3 text) | `%s` kind, `%s` group | No kind `%s` in group `%s`. Check the group. | `%s` is not a kind of group `%s`. Check the group. | Kind `%s` is not served in group `%s`. Check the group. | There is no `%s` in group `%s`. Is the group right? | Kind `%s` does not exist in group `%s`. Fix the group. || kept |
| 18 | S489 | `pkg/health/refs.go:18` | 013/014 message, explanation 1, several kinds | none | Some refs name a kind their group does not serve. Check the groups. | Several refs use a group that has no such kind. Check the groups. | Some refs point to kinds that are not served. Check their groups. | These refs name kinds the API server does not serve. Check the groups. | More than one ref names an unserved kind. Check the groups. || kept |
| 19 | S490 | `pkg/health/refs.go:20` | Name of the core group in S488; the group fix title S535 uses the same word | none | core | (core) | the core group | core (empty group) | v1 || kept |
| 20 | S491 | `copy/explorer.ts:515` | Evidence heading, one attributed ref | none | The ref | Failing ref | Ref at fault | This ref | The reference || kept |
| 21 | S492 | `copy/explorer.ts:516` | Evidence heading, several candidate refs | none | Refs the condition may be about | Possible refs | Candidate refs | Refs that may have failed | Which ref failed? || kept |
| 22 | S493 | `copy/explorer.ts:517` | Note under S492 | none | The condition does not say which of these refs failed. | The controller does not name the failing ref. | One of these refs failed. | The failing ref is not known. | The status does not say which ref failed. || kept |
| 23 | S494 | `copy/explorer.ts:518`, `:559` | Column label in the ref table and the fix block (`fixCopy.location`), which must stay equal | none | Location | Field | Path | Where | Field path || kept (same text in both places) |
| 24 | S495 | `copy/explorer.ts:519` | Column label in the ref table | none | Ref | Reference | Points to | Target | Ref target || kept |
| 25 | S496 | `copy/explorer.ts:520` | Badge on a ref of explanation 1 | none | No such kind in this group | Kind not served | Group does not serve this kind | Unknown kind for this group | Wrong group || kept |

## KGWUI-015: hint and evidence

The hint is appended to the 015 detail after the condition, once the replaced rule is old enough. It names no object, so restricted callers see it too.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 26 | S497 | `pkg/health/rules.go:22` | 015 hint (design §7.4 text) | none | If the cause was temporary, such as an identity provider outage, some versions keep the rule replaced until the controller restarts. | Some versions keep the rule replaced after a temporary failure, such as an identity provider outage, until the controller restarts. | Was the cause temporary, like an identity provider outage? Some versions keep the rule replaced until you restart the controller. | Some versions restore the rule only after a controller restart, even once a temporary cause is gone. | If a temporary outage caused this, restarting the controller may restore the rule on some versions. || kept |
| 27 | S498 | `copy/explorer.ts:521` | Evidence value for the reason | none | Replaced / Dropped | Replaced by a direct response / Removed | Swapped out / Removed | Overridden / Dropped | Replaced / Discarded || kept |
| 28 | S499 | `copy/explorer.ts:522` | Evidence row label | none | Reason | Cause | Why | Controller reason | Status reason || kept |
| 29 | S500 | `copy/explorer.ts:523-524` | Evidence row label and value | `{n}` | Match / match {n} | Match index / #{n} | Match / index {n} | Rule match / match {n} | Matcher / {n} || kept |
| 30 | S501 | `copy/explorer.ts:525` | Note beside S500 | none | The controller's match index, not a rule number. | This counts matches, not rules. | Match index from the controller. | Not the rule's index. | The controller counts matches here, not rules. || kept |
| 31 | S502 | `copy/explorer.ts:526` | Evidence row label, the named policy | none | Policy | Replaced by | Named policy | Cause policy | From policy || kept |
| 32 | S503 | `copy/explorer.ts:527` | Evidence row label, the clipped status message (015 and 017) | none | Controller message | Message | Status message | The controller says | Raw message || kept |

## KGWUI-016: messages and labels

`pkg/health/targets.go`. The detail is `<target>: <what is missing>.`, then the computed label, then "Not verified" when it applies, then the rule set (`ruleSetDetail`, unchanged). `<target>` is `targetRefs[i] Kind `name`` or `targetSelectors[j] Kind `k=v,…``, which stay as written.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 33 | S504 | `pkg/health/targets.go:29` | 016 message; equals the existing `TargetNotFound` reason message | none | Policy target not found | Policy target is missing | The policy's target does not exist | No object matches this policy target | Target not found || kept |
| 34 | S505 | `pkg/health/targets.go:35` | 016 detail, target not found | `%s` target, `%s` namespace | %s: not found in namespace %s. | %s: no such object in %s. | %s does not exist in %s. | %s: missing from namespace %s. | %s: nothing by that name in namespace %s. || kept |
| 35 | S506 | `pkg/health/targets.go:36` | 016 detail, section not found | `%s` target, `%s` section | %s: section `%s` not found. | %s has no section `%s`. | %s: no listener or rule named `%s`. | %s: section `%s` does not exist. | %s: unknown section `%s`. || kept |
| 36 | S507 | `pkg/health/targets.go:37` | 016 detail, labelled selector with no match | `%s` target, `%s` namespace | %s: matches nothing in namespace %s. | %s: no object in %s has these labels. | %s selects nothing in %s. | %s: no match in namespace %s. | %s: nothing in namespace %s carries these labels. || kept |
| 37 | S508 | `pkg/health/targets.go:38` | 016 detail, empty selector | `%s` target | %s: an empty selector matches nothing. | %s has no labels, so it selects nothing. | %s: no labels to match. | %s: a selector without labels selects nothing. | %s: an empty selector selects no object. || kept |
| 38 | S509 | `pkg/health/targets.go:33` | 016 computed label, after the detail | none | ` Computed by the UI's policy targets rule.` | ` The UI computed this with its policy targets rule.` | ` Found by the UI, not reported by the controller.` | ` UI check: policy targets rule.` | ` The controller does not report this; the UI's policy targets rule does.` || kept |
| 39 | S510 | `pkg/health/targets.go:31` | 016 not-verified label; matches `noteInconclusive` | none | Not verified: the controller's status cannot confirm this. | Not verified against the controller. | The controller's status cannot confirm this. | Unverified: status cannot confirm this. | The UI cannot check this against status. || kept |
| 40 | S511 | `pkg/health/targets.go:40`, outcomes at `:335-337` | 012 detail from the policy targets verifier; matches `parentDisagrees` | `%s` target, `%s` outcome (`not found`, `section not found`), `%s` ancestor | %s: computed %s, controller reports ancestor %s. | %s: the UI finds it %s, the controller attached it to %s. | %s is %s, yet the controller reports ancestor %s. | %s: %s here, but status names ancestor %s. | %s: computed %s, while the controller lists ancestor %s. || kept |

## KGWUI-017: rule names, lead-ins and evidence

`pkg/health/conflicts.go`. The detail is the condition, then a lead-in naming the other side and the computed label, or a none-text when no kept listener matches.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 41 | S512 | `pkg/health/conflicts.go:25` | Rule name for `HostnameConflict`; wire value (`rule`), also in S518 and S524 | none | listener port and hostname | port and hostname | same port and hostname | listener hostname | listener port and hostname match || kept |
| 42 | S513 | `pkg/health/conflicts.go:27` | Rule name for `ProtocolConflict`; wire value (`rule`), also in S518 and S524 | none | listener port and protocol | port and protocol | one protocol per port | listener protocol | listener port and protocol match || kept |
| 43 | S514 | `pkg/health/conflicts.go:36` | Hostname lead-in | `%s` S517 list | Same port and hostname as %s. | %s has the same port and hostname. | Matches %s on port and hostname. | Same port and hostname: %s. | %s already uses this port and hostname. || kept |
| 44 | S515 | `pkg/health/conflicts.go:38` | Protocol lead-in | `%d` port, `%s` protocols, `%s` S517 list | Port %d keeps %s: %s. | Port %d stays %s for %s. | Port %d serves %s through %s. | On port %d the controller kept %s: %s. | Port %d already has %s: %s. || 4 (doubt: names the controller; "Port %d keeps" was cryptic) |
| 45 | S516 | `pkg/health/conflicts.go:40` | Computed label after S514 or S515 | `%s` rule | ` Computed by the UI's %s rule.` | ` The UI computed this with its %s rule.` | ` Found by the UI's %s rule.` | ` UI check: %s rule.` | ` The controller does not name it; the UI's %s rule does.` || kept |
| 46 | S517 | `pkg/health/conflicts.go:184-189` | The other side in S514 and S515: one, several, or past the cap | `%s` names, `%d` rest | listener %s / listeners %s and %s / listeners %s and %d more | listener %s / listeners %s and %s / listeners %s, and %d more | listener %s / listeners %s and %s / listeners %s (+%d more) | listener %s / listeners %s and %s / listeners %s and %d others | listener %s / listeners %s and %s / listeners %s, plus %d more || kept |
| 47 | S518 | `pkg/health/conflicts.go:42` | None-text, hostname rule | `%d` port | The controller reports a conflict; no other accepted listener on port %d has the same hostname. | The controller reports a conflict, but no other accepted listener on port %d has this hostname. | No other accepted listener on port %d has the same hostname, though the controller reports a conflict. | The controller reports a conflict. The UI finds no other accepted listener on port %d with this hostname. | Conflict reported, but no accepted listener on port %d shares this hostname. || 4 (pairs with S519: says the UI computed it) |
| 48 | S519 | `pkg/health/conflicts.go:43` | None-text, protocol rule | `%d` port | The controller reports a conflict; no other accepted listener on port %d uses another protocol. | The controller reports a conflict, but every other accepted listener on port %d uses the same protocol. | No other accepted listener on port %d uses another protocol, though the controller reports a conflict. | The controller reports a conflict. The UI finds no other protocol among accepted listeners on port %d. | Conflict reported, but no accepted listener on port %d has a different protocol. || 4 (doubt: option 2 is wrong when no other listener exists) |
| 49 | S520 | `copy/explorer.ts:528-531` | Evidence labels; they follow the Gateway API field names | none | Listener / Port / Protocol / Hostname | Name / Port / Protocol / Host | Listener / Port number / Protocol / Host name | Listener name / Port / Protocol / Hostname | Listener / Listener port / Listener protocol / Listener hostname || kept |
| 50 | S521 | `copy/explorer.ts:532` | Evidence row label | none | Rule | Check | UI rule | Computed rule | Matched by || kept |
| 51 | S522 | `copy/explorer.ts:533` | Evidence row value | `{rule}` | Computed by the UI: {rule} | UI rule: {rule} | Checked by the UI ({rule}) | From the UI's {rule} rule | {rule}, computed by the UI || kept |
| 52 | S523 | `copy/explorer.ts:534` | Evidence table heading | none | Listeners the controller kept | Other listeners | Conflicting listeners | Accepted listeners | Listeners in the way || kept |
| 53 | S524 | `copy/explorer.ts:535` | Column label in S523's table | none | Owner | Parent | Defined in | Owned by | On || kept |

## KGWUI-018

`pkg/health/freshness.go` and `pkg/diagnose/nextsteps.go`. The detail counts stuck objects per 003 kind, in §10.1 order, then the syncs clause when the metric is current.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 54 | S525 | `pkg/health/freshness.go:196` | 018 message | `%d` objects | Status is not progressing on %d objects | The controller has not updated status on %d objects | Status updates stalled on %d objects | %d objects have had no status update for 5 min | Status is stuck on %d objects || kept |
| 55 | S526 | `pkg/health/freshness.go:135` | Kind words in the 018 detail; they pair with S482–S485 | none | never written / stale / pending / no ancestor | never written / out of date / pending / no entry | with no status / with old status / waiting / with no entry | never written / stale / waiting / no controller entry | no status / old status / pending / no ancestor entry || kept |
| 56 | S527 | `pkg/health/freshness.go:266-270` | 018 detail: `%d <kind>` joined by ", ", then the syncs clause | `%d` count, `%s` kind; `%d` syncs | %d %s, …; status syncs in the last 5 min: %d | %d %s, …. Status syncs in the last 5 min: %d | %d %s, …; %d status syncs in the last 5 min | %d %s, …; status syncs (5 min): %d | %d %s and …; the controller synced status %d times in 5 min || 2 (doubt: period as in S309; the semicolon also works) |
| 57 | S528 | `pkg/health/freshness.go:142` | Check name in "What was checked"; lower case like the 3.2 names | none | status progress | status updates | status progress check | stuck status | status syncing || kept |
| 58 | S529 | `pkg/diagnose/nextsteps.go:117` | Next step title on 018 | none | See status errors in the controller log | Read status sync errors in the controller log | Check the controller log for status errors | See why status is not written | Find status failures in the controller log || kept |
| 59 | S530 | `copy/explorer.ts:536-538` | Evidence table heading and column labels | none | Objects with a stuck status / Objects / Oldest | Stuck objects / Count / Oldest | Not progressing / Objects / Since | Waiting objects / Count / First | Stalled status / Objects / Oldest || kept |
| 60 | S531 | `copy/explorer.ts:539` | Evidence row label | none | Status syncs, last 5 min | Syncs (5 min) | Status writes in 5 min | Recent status syncs | Status syncs in the last 5 minutes || kept |
| 61 | S532 | `copy/explorer.ts:540` | S531's value when the syncs are not known | none | Not known | Unknown | Not reported | Not measured | No data || kept |

## Suggested fixes

Titles and bases in `pkg/diagnose/fix.go`; the block in `diagnostics/SuggestedFix.tsx`. The download command is `kubectl create -f <file>` and stays as is.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 62 | S533 | `pkg/diagnose/fix.go:32` | Grant fix title | `%s` namespace | Create a ReferenceGrant in `%s` | Add a ReferenceGrant to `%s` | Allow the ref with a ReferenceGrant in `%s` | Grant access in `%s` | New ReferenceGrant in `%s` || kept |
| 63 | S534 | `pkg/diagnose/fix.go:33` | Grant fix basis | none | Gateway API: a ref to another namespace needs a ReferenceGrant in that namespace. | Gateway API: cross-namespace refs need a ReferenceGrant. | The Gateway API needs a ReferenceGrant for refs across namespaces. | Gateway API: the target namespace must allow the ref. | A ref to another namespace works only with a ReferenceGrant there. || kept |
| 64 | S535 | `pkg/diagnose/fix.go:35` | Group fix title | `%s` group | Set the group to `%s` | Use group `%s` | Change the group to `%s` | Fix the group: `%s` | Correct the group to `%s` || kept |
| 65 | S536 | `pkg/diagnose/fix.go:37` | Group fix basis | `%s` group, `%s` kind | API discovery: only group `%s` serves kind `%s`. This sets only group, kind, name and namespace; keep the port and other fields. | API discovery: only group `%s` serves kind `%s`. Change only these fields; the port and the rest stay. | Discovery shows only group `%s` with kind `%s`. The fix sets group, kind, name and namespace and leaves the port alone. | API discovery: group `%s` alone serves kind `%s`. Update these four fields and keep every other field. | Only group `%s` serves kind `%s` here. Merge these fields at the path; keep the port. || kept |
| 66 | S537 | `copy/explorer.ts:558` | Badge on a fix that creates an object | none | New object | Creates a new object | Not yet in the cluster | A new resource | To be created || kept |
| 67 | S538 | `copy/explorer.ts:560` | Note on a fragment fix | none | Change only these fields at this location. Keep the rest. | Merge these fields here; keep the others. | Edit only these fields. | Replace only these fields at this path. | Update these fields and leave the rest as is. || kept |
| 68 | S539 | `copy/explorer.ts:561` | Heading of the basis line | none | Basis | Why | Based on | Source | Reason || kept |
| 69 | S540 | `diagnostics/SuggestedFix.tsx:172` | Heading of the YAML block; inline today, the apply step moves it to `fixCopy` | none | YAML | Fix | Change | Manifest | Suggested YAML || kept |
| 70 | S541 | `copy/explorer.ts:562` | Heading of the create command | none | Create it with | Run | To create it, run | Create with | Command || kept |
| 71 | S542 | `copy/explorer.ts:563` | Note under the create command | none | `kubectl create` fails if the name is taken, so it never replaces an object. | `kubectl create` stops if the name exists. | It uses `kubectl create`, which never overwrites. | `kubectl create` refuses a name that is taken. | `kubectl create` never replaces an object: it fails if the name is taken. || kept |
| 72 | S543 | `copy/explorer.ts:566` | Copy button's accessible name; the visible text is S293 | `{title}` | Copy YAML: {title} | Copy the YAML for {title} | Copy {title} | Copy fix: {title} | Copy YAML to clipboard: {title} || kept |
| 73 | S544 | `copy/explorer.ts:567-568` | Download button text and accessible name | `{file}` | Download / Download {file} | Save / Save {file} | Download YAML / Download {file} | Save as file / Save as {file} | Get file / Get {file} || kept |

## Grouped view

`pages/findings/FindingsPage.tsx` and `diagnostics/GroupRow.tsx`. The group row's title is its top member's message, so the family names are not shown.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 74 | S545 | `copy/explorer.ts:573` | Label of the view switch | none | View | Show | Group by | Layout | Display || kept |
| 75 | S546 | `copy/explorer.ts:574-575` | The two view chips | none | Findings / Groups | All findings / By cause | List / Grouped | Each finding / Grouped by cause | Findings / Causes || kept |
| 76 | S547 | `copy/explorer.ts:576` | Column label, the group title | none | Cause | Group | Problem | Issue | What is wrong || kept |
| 77 | S548 | `copy/explorer.ts:577` | Column label, the cause object | none | Cause object | Caused by | Source object | Root object | Object || kept |
| 78 | S549 | `copy/explorer.ts:578-579` | Column labels, affected objects and member count | none | Affected / Findings | Impact / Count | Objects hit / Members | Affected objects / Finding count | Affects / Findings || kept |
| 79 | S550 | `copy/explorer.ts:581` | Noun in the list count and empty states | `{n}` | group / groups | cause / causes | grouped problem / grouped problems | finding group / finding groups | problem / problems || kept |
| 80 | S551 | `copy/explorer.ts:545-546` | "Not permitted" group's cause cell and home page group row; the group shows in parentheses for a non-core kind | `{from}`, `{kind}`, `{group}`, `{to}` | From {from} to {kind} ({group}) in {to} | {from} to {kind} ({group}) in {to} | Refs from {from} to {kind} ({group}) in {to} | {from} refs to {kind} ({group}) in {to} | {from} → {kind} ({group}) in {to} || kept |

## Controller log signals

`diagnostics/EvidenceView.tsx` for 001, 006, 019 and 021, and `pages/explore/gateway/DataPlaneSection.tsx` for the Gateway page. Classes and frames are API values and show as is.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 81 | S552 | `copy/explorer.ts:541`, `:623` | Row label on evidence and the Data plane section, which must stay equal | none | Controller log | Controller logs | Log | Controller log reading | Log signals || kept (same text in both places) |
| 82 | S553 | `copy/explorer.ts:542` | S552's value; "Not read" is never "nothing found" (D-006) | none | Read / Not read | Checked / Not checked | Read / Not read by the UI | On / Off | Yes / No || kept |
| 83 | S554 | `copy/explorer.ts:543`, `:547` | 019 evidence row labels | none | Panic / Top frame | Crash / Function | Panic / Frame | Panic class / Where | Panic / Top of stack || kept |
| 84 | S555 | `copy/explorer.ts:544` | Muted line under the panic class | `{ago}` | Read from the log {ago}. The trace has no time. | Log read {ago}. Traces carry no time. | Seen in the log {ago}. The panic itself has no time. | Found when the log was read, {ago}. | Log read {ago}. Panic time not known. || kept |
| 85 | S556 | `copy/explorer.ts:548-550` | 006 and 021 table heading and column labels | none | Deployer errors / Error class / Last seen | Deploy errors / Kind / Last | Deployer problems / Class / Latest | Deployer errors / Error / Last logged | Errors from the deployer / Class / Last seen || kept |
| 86 | S557 | `copy/explorer.ts:551-552` | 001 table heading and column label | none | Controller log events / Error classes | Log events / Classes | Rejects in the log / Error kinds | Logged rejects / Classes | Log entries / Error types || kept |
| 87 | S558 | `pkg/health/xds.go:45`, `copy/explorer.ts:622` | Withheld badge in the Data plane section (R105); the server value and the SPA copy must stay equal | none | Proxies may not be getting config updates | The controller is holding back config for this Gateway | Config updates to these proxies may be held back | Proxies may be stuck on old config | Config updates may not reach these proxies || kept (same text in both places) |
| 88 | S559 | `copy/explorer.ts:623` | Line under S558 | `{first}`, `{last}` | First logged {first}, last {last} | Since {first}, last {last} | Logged from {first} to {last} | First {first}, latest {last} | Seen {first} to {last} || kept |

## Learn more

`diagnostics/EvidenceView.tsx:649-652`, on the finding page. The link opens the code's entry in `docs/findings.md`; the doc's headings do not show in the UI.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 89 | S560 | `copy/explorer.ts:502` | Link text | none | Learn more | About this finding | Read more | What this means | Finding reference || kept |
| 90 | S561 | `copy/explorer.ts:503` | Link's accessible name; it starts with the visible text | `{code}` | Learn more about {code} (opens in a new tab) | {code} in the finding reference, new tab | Read about {code} (new tab) | Learn more about {code} | Open the {code} reference in a new tab || kept |

## Availability: controllerLogs

`capabilityStates` in `pkg/app/app.go`. `/api/v1/meta` reasons, read by extensions; the core SPA does not render them. Denied reuses `reasonDenied`.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 91 | S562 | `pkg/app/app.go:163` | A stream cannot be read, or the reader is stuck | none | Controller logs cannot be read | Controller logs are not being read | Reading controller logs failed | The controller log stream is failing | The UI cannot follow the controller logs || kept |
| 92 | S563 | `pkg/app/app.go:164` | Lines do not match the known format | none | Log format not recognised | The controller log format is not recognised | Controller log lines could not be parsed | Unknown controller log format | Log lines no longer match the known format || 2 |
| 93 | S564 | `pkg/app/app.go:165` | No parser for the install's rule set | none | No log format is known for this kgateway version | This kgateway version's logs are not supported | The UI cannot read logs of this kgateway version | No log parser for this kgateway version | Controller logs are not read for this version || kept |
| 94 | S565 | `pkg/app/app.go:166` | No stream connected yet | none | No controller log stream is connected | Controller logs are not connected yet | No controller Pod log is being read | Waiting for a controller log stream | The UI is not connected to any controller log || kept |

## API error bodies (400)

`pkg/api/findings.go`. Shown in the SPA's error states and to API callers. They follow `severity %q: want error, warning or info`.

| # | id | File:line | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|
| 95 | S566 | `pkg/api/findings.go:75` | `%q` | view %q: want groups | view %q: only groups is supported | unknown view %q; use groups | view %q: must be groups | view %q is not a view; use groups || kept |
| 96 | S567 | `pkg/api/findings.go:80` | `%q` | parameter %q: the groups view takes severity, code, q, limit and cursor | parameter %q is not allowed with view=groups | the groups view does not take %q | parameter %q: view=groups takes only severity, code, q, limit and cursor | unknown parameter %q for view=groups; use severity, code, q, limit or cursor || kept |

## Reused unchanged

Not part of this pass: the reason messages in `pkg/health/messages.go` (`reasonMessages`), which give the 013, 014, 015 and 017 messages for explanation 2; the condition detail format (`detail`); `ruleSetDetail`; the experimental hint; "Since" (S272), "Object" (S271), "Copy" (S293); the restricted-detail text; the diagnose family names ("Missing backend", "Not permitted", "Replaced by policy", "Status not progressing"), which feed group ids and are not shown; the log classes, frames and Envoy names (API values); the `kubectl create -f <file>` command; the `targetRefs[i]` and `targetSelectors[j]` target forms; and the `docs/findings.md` headings, which the UI does not show.

## Picker notes

- S475 (R110): kept lower-case, because its `for %s` form is a frozen detail in the v1 `/health` golden and an additive exception is a gate change. So the 003 details mix cases: S475 is lower-case, S474 and S476–S478 are sentence case. A later contract change can align it.
- S474–S478: sentence case for every 003 detail, as the 3.2 check-notes rule. The `:420` form keeps ` for %s` after it.
- S482: "has not written" matches S475's "No status from the controller". It also covers the CRD-default case.
- S515: "On port %d the controller kept %s: %s." says who kept the protocol. Placeholders stay in order.
- S518 and S519: both use option 4, so both say the UI found no match. S519 option 2 reads wrong with no other listener.
- S527: the period follows S309. Keep the semicolon if the detail reads better as one line.
- S494, S552 and S558: one text each, used in both places.
- S512 and S513: API values, kept.
- Glossary additions: **Computed by the UI** labels every UI-derived result (S509, S516, S522). **Accepted listener** is a listener the controller kept. **Not known** is S532's value, never "Unknown".

