# Sub-phase 2.3 — Wording (Task 48)

The final copy of view A on screen and the 2.3 server text (design §8.8). SPA strings live in `web/packages/shell/src/copy/explorer.ts`; the comment ids there match this table. Server ids start at S167. S129–S131 are view B reasons and get their pass in 2.4.

Badge state texts and notes carry no trailing period: the compact tooltip joins its parts as sentences and ends with one period (`joinSentences` in `explore/ComputedBadge.tsx`). Explanation clauses hold no `;`, since clauses are joined with `"; "`.

## Computed badge

| id | Text | Where |
|---|---|---|
| S111 | Computed | Badge chip (fixed, design §8.4) |
| S112 | Computed by the UI from `<rule set>`'s `<family>`: `<rule>` (e.g. "Computed by the UI from kgateway 2.4's attachment rules: hostname intersection") | Chip tooltip |
| S113 | attachment rules | Tooltip family for view A's rules |
| S114 | rules | Tooltip family for other rules |
| S115 | allowed routes | Rule `allowed-routes` |
| S116 | section name and port | Rule `section-and-port` |
| S117 | hostname intersection | Rule `hostname-intersection` |
| S118 | routes per listener | Rule `listener-count` |
| S119 | Count agrees with the controller | Coverage `count-agrees` |
| S120 | Consistent with the status | Coverage `status-compatible` |
| S121 | The controller does not report this | Coverage `not-verified` |
| S122 | Waiting for the controller's status | Check state `pending` |
| S123 | Hidden: depends on objects you cannot view | Check state `restricted` |
| S124 | The UI's computation disagrees with the controller. The controller's status is what counts | Check state `disagrees` |
| S125 | Unknown: the install's scope is not known | Reason `scope-unknown` (user, 2026-10-24) |
| S126 | Unknown: the UI cannot read namespace labels | Reason `namespace-labels-unavailable` |
| S127 | Unknown: the ListenerSet has no current status | Reason `listenerset-no-status` |
| S128 | Unknown: the UI cannot read a controller setting | Reason `setting-unknown` |
| S129–S131 | (2.4) | View B reasons `not-reached`, `context-unknown`, `too-large`; drafts kept |
| S132 | Unknown (`<reason>`) | Reason this SPA has no text for |
| S133 | Unknown: the data is stale | Stale view |
| S159 | Agrees | Compact state, any passing coverage |
| S160 | Disagrees | Compact state |
| S161 | Waiting | Compact state |
| S162 | Unknown | Compact state |
| S163 | Hidden | Compact state |
| S164 | Not checked | Compact state, nothing compared |

## View A on pages

| id | Text | Where |
|---|---|---|
| S134 | Computed routes | Listener expand label ("Computed routes: 3") |
| S135 | Envoy may serve fewer hostnames on this listener | Isolation note: icon label, tooltip and expand text |
| S136 | Routes by listener | Listener chip group `aria-label` |
| S137 | All listeners | Listener chip |
| S138 | Unknown listener | Listener chip for `_unknown` |
| S139 | Computed routes on listener `<name>` | Attached section title with a listener selected |
| S140 | Routes with an unknown listener | Section title for `_unknown` |
| S141 | No routes on this listener. | Empty listener |
| S142 | No route has an unknown listener. | Empty `_unknown` |
| S143 | The UI cannot list this listener's routes. | Listener with an unknown or hidden count |
| S144 | All routes of this `<Kind>` | Link to the Routes list with `parent=` |
| S145 | Effective hostnames | Column on a selected listener |
| S146 | Why unknown | Column for `_unknown` |
| S147 | Listeners | Parent card: computed listeners |
| S148 | Computed outcome | Parent card label |
| S149 | Accepted | Computed outcome (fixed, 2.2 term S12) |
| S150 | No listener allows this route | Outcome `NotAllowedByListeners` |
| S151 | No listener matches the section or port | Outcome `NoMatchingParent` |
| S152 | No listener hostname matches | Outcome `NoMatchingListenerHostname` |
| S153 | Routes | Computed column on the Gateways and ListenerSets lists and the listener table |
| S154 | Computed by the UI: routes on this object's own listeners, each counted once | Routes column chip tooltip |
| S155 | No count | Routes cell with an omitted count |
| S156 | Disagreements: N | Routes cell |
| S157 | UI disagrees with the controller | Route state flag `disagrees` |
| S158 | Unknown listener | Route state flag `listener-unknown` |
| S165 | This `<Kind>` has no listener named `<name>`. | `?listener=` names no listener |
| S166 | The controller does not report effective hostnames. | Parent card listener tooltip |
| S187 | No count | Listener expand with an unknown or hidden count (was "None") |

## Server text

| id | Text | Where |
|---|---|---|
| S167 | listener `<l>` has `<hostname>`, route has `<hostnames>` | Explanation, hostname fact (`pkg/api/explain.go`) |
| S168 | listener `<l>` allows routes from namespace `<ns>`, route is in `<ns>` | Explanation, `From: Same` |
| S169 | listener `<l>` allows namespaces with `<selector>`, route's namespace has `<labels>` | Explanation, selector |
| S170 | listener `<l>` allows namespaces with `<selector>`, route's namespace has no labels | Explanation, selector with no labels |
| S171 | listener `<l>` does not allow `<route kind>` (allows `<kinds>`) | Explanation, kind |
| S172 | listener `<l>` has an invalid namespace selector: `<error>` | Explanation, selector error |
| S173 | parentRef wants port `<p>`, listener `<l>` is on `<p>` | Explanation, port |
| S174 | parentRef names section `<s>`, not listener `<l>` | Explanation, section |
| S175 | listener `<l>`: `<code>` | Explanation fallback |
| S176 | The UI's computed view disagrees with the controller | KGWUI-012 message (`pkg/health/messages.go`) |
| S177 | Listener `<l>`: computed N routes, controller reports M. Names are in backticks in the text. | KGWUI-012 detail, count |
| S178 | Parent `<name>` (section `<s>`): computed `<outcome>`, controller reports `<status>`. Names are in backticks in the text. | KGWUI-012 detail, parentRef |
| S179 | Rule set `<id>`. | KGWUI-012 detail suffix |
| S180 | computed-view-disagrees | KGWUI-012 slug (fixed, design §6.2) |
| S181 | computed views | Health check name |
| S182 | Controller version unknown; using the 2.4 rules | Selection note (`pkg/computed/rulesets`; quoted in computed-rules §2) |
| S183 | Rules not yet verified for `<major.minor>` | Selection note (quoted in computed-rules §2) |
| S184 | The controller's status cannot confirm this | Inconclusive note (`pkg/computed/check.go`) |
| S185 | listener: want a listener name or "_unknown" | API 400 (`pkg/api/params.go`) |
| S186 | listener needs a Gateway or ListenerSet parent | API 400 (`pkg/explore/query.go`) |
