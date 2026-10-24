# Sub-phase 2.2 — Wording (Task 31)

The final copy of the explorer pages and the 2.2 server text (design §8.8). SPA strings live in `web/packages/shell/src/copy/explorer.ts`; the comment ids there match this table. Kubernetes field names used as labels (Port, Hostnames, Listeners, Conditions …) are not listed.

## Shell and lists

| id | Text | Where |
|---|---|---|
| S01 | Overview | Install nav |
| S02 | Findings | Install nav |
| S03 | Install | Install nav `aria-label` |
| S04 | Copy kubectl command | Copy-command button on detail headers |
| S05 | Overview | Detail page tab |
| S06 | `<Kind>` views | Detail tab list `aria-label` |
| S07 | Filter Gateways / Filter routes / Filter ListenerSets | List search box label |
| S08 | Filter by name or namespace | Gateways and ListenerSets search placeholder |
| S09 | Filter by name, namespace or hostname | Routes search placeholder |
| S10 | Health | Health chip group |
| S11 | Route state | Route state chip group |
| S12 | Accepted | Route state chip; attachment label |
| S13 | Not accepted; Routes not accepted (column) | Route state chip, attachment label, sub-filter; Gateways column |
| S14 | References not resolved | Route state chip |
| S15 | Rules replaced or dropped | Route state chip (fixed by design §2 and e2e) |
| S16 | Waiting for status | Route state chip, attachment label, sub-filter |
| S17 | No status entry for the parent route | Route state chip and attachment label for a declared parent |
| S18 | No parent | Route state chip |
| S19 | `Key: value`, refs as `Kind ns/name`, attachments by their label (e.g. "Parent: HTTPRoute shop/api", "Attachment: Not accepted") | URL filter tags |
| S20 | Remove filter `<tag>` | Tag remove button |
| S21 | Clear filters | List toolbar and empty state |
| S22 | No `<things>` in this install. | Empty list |
| S23 | No `<things>` match these filters. | Empty filtered list |
| S24 | You cannot view N `<things>` in this install. | Denied-objects line under a list |
| S25 | Previous / Next | Pager |
| S26 | Cannot load `<things>` | List, sub-table and detail error title |
| S27 | Show details for `<ns/name>` / Hide … | Row expand toggle |
| S28 | Details | Expand column header (screen reader) |
| S29 | Attached routes | Gateways column; Gateway and ListenerSet listener tables |
| S30 | None | Empty parents, addresses, TLS |
| S31 | Inherited | Route with no hostnames |
| S32 | Namespace not watched | Attachment label |
| S33 | on Gateway `<ns/name>` | Parent cell: a ListenerSet parent's Gateway |
| S34 | on a Gateway you cannot view | Parent cell: hidden Gateway |
| S35 | Mirror | Row tag under a backend filter (fixed, user) |

## Detail pages

| id | Text | Where |
|---|---|---|
| S36 | `<Type>`: `<Status>` | Condition line |
| S37 | Describes generation N; the object is at M. | Stale condition |
| S38 | Stale conditions show the unknown icon; the line keeps the written status | Condition lists, listener state |
| S39 | No conditions reported. | Empty conditions |
| S40 | The controller has not written status for this listener. | Listener expand, no status |
| S41 | No status | Listener status cell |
| S42 | Not set | Listener port |
| S43 | Any hostname | Listener hostname |
| S44 | Not reported | Listener attached routes, no status |
| S45 | Same namespace / All namespaces / Namespaces with `<selector>` | Listener allowed routes |
| S46 | any labels (inline), Any labels (alone) | Empty namespace selector |
| S47 | Default kinds for the protocol | Listener allowed kinds unset |
| S48 | Show conditions for listener `<name>` / Hide … | Listener expand button |
| S49 | Attached ListenerSets (status): N | Gateway ListenerSets heading |
| S50 | No ListenerSets name this Gateway. | Gateway ListenerSets empty |
| S51 | Attached routes · No routes attached. | Attached routes section |
| S52 | Routes not attached · Not attached | Failed routes section heading and chip group |
| S53 | Not accepted (N) · Waiting for status (N) · Namespace not watched (N) | Failed routes sub-filters |
| S54 | Why | Failed routes column |
| S55 | No routes in this state. | Failed routes empty |
| S56 | Not available | ListenerSet parent Gateway absent or hidden |
| S57 | Parent Gateway's `allowedListeners` | Allowed-listeners block title |
| S58 | Not set: the Gateway allows no ListenerSets. | allowedListeners absent |
| S59 | If you set `allowedListeners` and it is missing here, the Gateway CRD in this cluster may not have the field. The API server drops fields its CRD does not define. If so, install a Gateway CRD from the same Gateway API channel and version as the ListenerSet CRD. | CRD hint |
| S60 | The controller watches `<Kind> <version>` only with its experimental Gateway API features on. They may be off here. | ListenerSet with no status, experimental kind (`experimentalHint`) |
| S61 | Route parents | Up-chain nav label |
| S62 | A parent you cannot view | Restricted up-chain step |
| S63 | Cycle | Up-chain cycle marker |
| S64 | More paths not shown | Truncated up-chain |
| S65 | No reported path to a Gateway or ListenerSet. | Empty up-chain |
| S66 | Inherits the listener's hostnames / Inherits the parent route's hostnames | Route hostnames |
| S67 | Parent routes | Permission line label |
| S68 | Allows any parent route | Empty parentRefs |
| S69 | Allows: `<ns/name, …>` | parentRefs naming routes |
| S70 | Allows no parent route | parentRefs naming no route |
| S71 | No status from this install's controllers. | Parent card without own entry |
| S72 | Other controllers (N) | Collapsed other entries |
| S73 | A backend may be missing, or a delegation ref may select no route. | Under `ResolvedRefs=False BackendNotFound` on a route with a delegation ref |
| S74 | No parentRefs. | Route with no parentRefs |
| S75 | No rules. | Route with no rules |
| S76 | Backend refs | Rule sub-heading |
| S77 | No backend refs. | Rule with no backendRefs |
| S78 | Filters on this backend ref | backendRef filters |
| S79 | ~~Default match~~ | Removed (2.2 review): omitted matches arrive with the default filled in, so the label had no case |
| S80 | Mirror to `<ns/name>` | Mirror filter |
| S81 | Weight N (default) | Defaulted weight |
| S82 | Restricted | Hidden Backend or delegated route |
| S83 | Other routes using this backend | backendRef reverse lookup (fixed, user) |
| S84 | Configuration accepted | Backend with a current `Accepted=True` (fixed, user; never "working") |
| S85 | Describes an older generation of the Backend. | Stale Backend condition; icon unknown |
| S86 | `Accepted: <Status>` + "The controller does not say which version it checked." | Backend condition with no `observedGeneration`; icon follows the Backend state |
| S87 | Delegates to | Delegation ref |
| S88 | Route `<ns/name>` · Every route in `<ns>` · Routes in `<ns>` with label `<v>` | Delegation ref mode |
| S89 | Reported by the controller | Delegation state |
| S90 | Not reported by the controller | Delegation state |
| S91 | No such route | Delegation state |
| S92 | Show child routes / Hide child routes | Child routes toggle |
| S93 | No reported child routes. | No children |
| S94 | Not part of this install. Shown because … | Candidate banner |
| S95 | a finding of this install names it. · `<Kind> <ns/name>` refers to it. · an object of this install refers to it. | Candidate banner reasons |
| S96 | Routes that send traffic here | Backend YAML view (fixed, user) |

## Server text

| id | Text | Where |
|---|---|---|
| S97 | No status for the named parent route | KGWUI-011 message (`pkg/health/messages.go`) |
| S98 | No status entry for parent HTTPRoute `<ns/name>` | KGWUI-011 detail |
| S99 | the controller does not report which Backend version it checked | KGWUI-002 Backend no-evidence detail, in parentheses (`pkg/health/backend.go`) |
| S100 | A delegating rule has no accepted child route | KGWUI-002 message on a delegation-only parent (D-021 item 9) |
| S101 | A delegating rule selects no child route the controller accepted | That finding's detail prefix |
| S102 | Details depend on objects you cannot view | Restricted finding evidence (`pkg/api/explore_types.go`) |
| S103 | targets `<Kind> <ns/name>` | KGWUI-004 detail; Gateway text unchanged from Phase 1 |
| S104 | Namespace not watched by the controller | KGWUI-004 message (Phase 1, unchanged) |
| S105 | Pending reasons stay out of the UI | Route row `reason` without a condition type |
| S106 | ; the controller watches `<Kind> <version>` only when its experimental Gateway API features are on, and they may be off here | "Not yet processed" finding clause (D-018, unchanged) |
| S107 | no detail view for this kind | Detail 404 API error |
| S108 | Implicit parent | Card for a status entry when parentRefs is empty (label `Implicit parent <ns/name>`) |
| S109 | Not in parentRefs | Card for a status entry no spec parentRef names (label `Parent <ns/name>, not in parentRefs`) |
| S110 | Empty match list | Rule with an explicit `matches: []` (computed-rules §3.2) |
