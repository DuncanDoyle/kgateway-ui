# Sub-phase 3.1 — Wording (Task 31)

The final copy of the 3.1 findings pages, breadcrumbs and object header (Phase 2 design §8.8, Phase 3 design §17.5). Each string kept its five options; an independent picker chose one, recorded in the Choice column. Findings strings live in `findingsCopy` in `web/packages/shell/src/copy/explorer.ts`; breadcrumb, header and data-age strings in `web/packages/shell/src/copy/navigation.ts`. The comment ids there match this table. Ids run S265–S303 and continue the Phase 2 range.

Option 1 is always the current text. Placeholders such as `{n}`, `{ago}` and `{install}` stay as they are. Sorting has no strings of its own: the Severity (rank), Code and Since column headers sort the list.

## Findings list

`pages/findings/FindingsPage.tsx`, `findingsCopy`. S280 and S282 also show on the install page (`FindingRow`, `HealthStrip`) and the finding page.

| id | Where | Current | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|
| S265 | Page title and table name (same word as nav S02) | Findings | Problems | Issues | Health findings | Install findings | kept (decided earlier) |
| S266 | Search box accessible label | Filter findings | Search findings | Find a finding | Filter the findings | Search | Filter findings |
| S267 | Search placeholder; the server matches the object's namespace/name, the message and the detail | Filter by name or message | Filter by object, message or detail | Search objects and messages | Filter by object name or text | Type an object name or message text | Filter by object, message or detail |
| S268 | Column header and chip group label | Severity | Level | Priority | Impact | How bad | Severity |
| S269 | Column header and tag label (`Code: KGWUI-004`) | Code | Check | Finding code | Check code | ID | Code |
| S270 | Column header of the message column | Finding | Message | Problem | What's wrong | Issue | Message |
| S271 | Column header; detail fact; affected-objects column | Object | Resource | Affects | Kubernetes object | On | Object |
| S272 | Column header: when the finding was first seen | Since | First seen | Open since | Started | Age | Since |
| S273 | Object cell of a finding about the install itself | The install | This install | Install-wide | Whole install | The install itself | The install |
| S274 | Tag label from a link, e.g. `Namespace: shop` | Namespace | In namespace | Namespace is | Namespace filter | ns | Namespace |
| S275 | Tag label for a finding group, shows the group id | Group | Same cause | Cause group | Grouped with | Group id | Group |
| S276 | Result count next to the title | {n} findings | {n} found | {n} findings in total | {n} results | Total: {n} | {n} findings |
| S277 | Empty list, no filters | No findings in this install. | No problems found in this install. | This install has no findings. | Nothing to report in this install. | No findings yet. | No findings in this install. |
| S278 | Empty list with filters | No findings match these filters. | No findings match. | Nothing matches these filters. | No findings fit these filters. | No results. Try other filters. | No findings match these filters. |
| S279 | Error state title | Cannot load findings | Findings did not load | Could not load findings | Cannot show findings | Loading findings failed | Cannot load findings (R32) |
| S280 | Restricted finding: message cell, install page row and the finding page heading | Restricted: ask the owning team | Hidden: ask the team that owns it | You cannot view this. Ask its owners | Ask the owning team for details | Not visible to you: ask the owners | kept (decided earlier) |
| S281 | Accessible name of the muted dash for a missing value | No value | Not set | None | Empty | Not known | No value |
| S282 | Age format (`ago()`), also used by the data-age line | 5s ago, 10m ago, 3h ago, 16d ago | 5 sec ago, 10 min ago, 3 hr ago, 16 days ago | 5 seconds ago … 16 days ago | 5s, 10m, 3h, 16d | just now, 10 min ago, 3 hours ago, 16 days ago | 5s ago, 10m ago, 3h ago, 16d ago |

## Finding page

`pages/findings/FindingPage.tsx`, `findingsCopy`; tab titles in `shell/DocumentTitle.tsx`.

| id | Where | Current | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|
| S283 | Back link to the list | All findings | Back to findings | Findings | Back to all findings | See all findings | Back to findings |
| S284 | 404 state title (the id is unknown or the finding cleared) | Finding not found | This finding no longer exists | No finding with this id | Finding is gone | This finding is not open | Finding not found |
| S285 | Error state title | Cannot load finding | Finding did not load | Could not load this finding | Cannot show this finding | Loading the finding failed | Cannot load finding (R32) |
| S286 | Fact label for the longer explanation | Detail | Details | More detail | Explanation | Why | Detail |
| S287 | Fact label: first seen | Since | First seen | Open since | Started | Seen since | Since |
| S288 | Section heading and table label | Affected objects | Objects affected | What this affects | Affected | Objects with this problem | Affected objects |
| S289 | Empty affected-objects table | No affected objects you can view. | You cannot view any affected objects. | No affected objects to show. | None you can view. | No objects you can view are affected. | No affected objects you can view. |
| S290 | Section heading | Suggested fix | How to fix | Fix | Suggested change | Try this fix | Suggested fix |
| S291 | Section heading | Next steps | What to check | What to do next | Steps | Try these | Next steps |
| S292 | Note under a step that needs rights | Needs {permissions} | Requires {permissions} | You need {permissions} | Needs rights: {permissions} | Run with {permissions} | Needs {permissions} |
| S293 | Copy button tooltip; accessible name `Copy: {title}` | Copy / Copy: {title} | Copy command / Copy command: {title} | Copy / Copy {title} | Copy text / Copy text: {title} | Copy to clipboard / Copy {title} to clipboard | Copy / Copy {title} |
| S294 | Browser tab titles | Finding: {install} / Findings: {install} | Finding · {install} / Findings · {install} | {install}: Finding / {install}: Findings | Finding – {install} / Findings – {install} | Finding in {install} / Findings in {install} | Finding: {install} / Findings: {install} |

## Breadcrumbs

`shell/trail.ts`, `breadcrumbsCopy`; rendered by `shell/Breadcrumbs.tsx`. List crumbs reuse the nav copy.

| id | Where | Current | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|
| S295 | Trail `nav` accessible label | Breadcrumb | Breadcrumbs | Trail | You are here | Page trail | Breadcrumb |
| S296 | "…" button accessible label | Show {n} earlier pages | Show {n} more pages | {n} earlier pages | Show the {n} pages before | Show {n} hidden pages | Show {n} earlier pages |
| S297 | Menu accessible label | Earlier pages | Pages before | Previous pages | More pages | Earlier steps | Earlier pages |
| S298 | Crumb for an object the caller cannot view or that no longer exists (the server's `none` link covers both) | A page you cannot open | A page you cannot view | Page not available | Unavailable page | A page you cannot see | Page not available |

## Object header and data age

`shell/ObjectHeader.tsx` (`objectHeaderCopy`), `pages/DataAge.tsx`, `shell/DocumentTitle.tsx`.

| id | Where | Current | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|
| S299 | Muted label before the namespace value | namespace | in namespace | ns | Namespace | in | namespace |
| S300 | Data-age line, fresh data (header and list pages) | Last checked {ago} | Checked {ago} | Updated {ago} | Data from {ago} | As of {ago} | Last checked {ago} |
| S301 | Data-age line, stale data | Data is stale. Last checked {ago} | Data is out of date. Last checked {ago} | Stale. Last checked {ago} | Out of date: checked {ago} | Not current. Last checked {ago} | Data is stale. Last checked {ago} |
| S302 | Browser tab title of an object page, e.g. `Gateway infra/edge: dev` | {Kind} {namespace}/{name}: {install} | {Kind} {namespace}/{name} · {install} | {namespace}/{name} ({Kind}): {install} | {Kind} {namespace}/{name} – {install} | {Kind} {namespace}/{name} in {install} | {Kind} {namespace}/{name}: {install} |
| S303 | Hidden heading prefix, so the `h1` reads `Gateway infra/edge` | {Kind} {namespace}/ | {Kind}: {namespace}/ | {Kind} named {namespace}/ | {Kind} in {namespace}, named | {Kind} object {namespace}/ | {Kind} {namespace}/ |

## Reused unchanged

These keep their earlier wording and are not part of this pass: "Kind" (column header and tag), "Clear filters" (S21), "Previous" and "Next" (S25), "You cannot view {n} objects in this install." (S24 with the noun "object"), "Show details for {name}" (S27), the severity labels "Error", "Warning", "Info" and the stale "Unknown".


## Picker notes

- Load errors: the picker chose "Could not load findings" and "Could not load this finding"; ruling R32 overrides both (below).
- "Since" is used for both the list column and the finding-page fact.
- Search label "Filter findings" fits the new placeholder "Filter by object, message or detail", which names all three fields the server matches.
- "Message" heads the message column; the crumb "Page not available" fits both hidden and deleted objects.
- Tab titles keep the colon form for findings and object pages.
- Unchanged current text wins where it is already short and accurate.
## Rulings

- R32: S279 and S285 keep the app's existing error form, "Cannot load findings" and "Cannot load finding" (`listCopy.cannotLoad`, S26), for consistency with the explorer pages.
