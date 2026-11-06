# Sub-phase 3.4 — Wording (Task 80)

30 strings, five options each, chosen by a separate picker; 2 changed (S570, S573), 28 kept.

The final copy of the 3.4 backend checks: the 023 message and detail, the endpoints evidence labels, the 024 messages, details and evidence labels, the "not available on this version" check note, the 013 Service explanations, the port-fix title and basis, the two check names and the reasons the new checks give when they cannot run (design §8.6, §11). Each string has five options; a separate agent picks one and records it in the Choice column ("kept" is option 1). SPA strings live in `diagnosticsCopy` in `web/packages/shell/src/copy/explorer.ts`, whose comment ids match this table. Ids continue the 3.3 range: S568–S578 were drafted in Tasks 79 and 85, and S579–S596 are new. S362 keeps its 3.2 id.

Option 1 is always the text before the pass. Placeholders (`%s`, `%d`, `{n}`, `{ago}` …) stay in order. Finding codes, kinds, Kubernetes reasons (`Pending`, `Unknown`, `ResolvedRefs` …), metric modes (`flip_release`, `warm_release`, `carry_expired`) and wire values are unchanged. Where a row is marked "wire value", a change also changes the API value, the TS enum and the goldens.

The 3.2 glossary applies: "Not known", "Not observed", "Not available", sentence-case check notes. Two Task 79 review notes shape the options. First, "0 of 0 ready" hides "no endpoints at all", so S570 offers forms for a total of 0. Second, the row label "Ready" (S400, shared with the process tables) and the value "N of M ready" both say "ready", so S570 offers values that drop the word.

## KGWUI-023: message, detail and evidence

`pkg/health/endpoints.go` and `diagnostics/EvidenceView.tsx`. The message is the finding title and comes from design §11. The finding is on the Service, one per Service port that a route backend uses. The evidence shows the four facts below.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | S579 | `pkg/health/endpoints.go:27` | 023 message (design §11 text) | none | Requests to this backend get 503 | This backend returns 503 to requests | Requests to this backend fail with 503 | No ready endpoints: requests get 503 | Nothing can serve this backend, so requests get 503 | kept |
| 2 | S580 | `pkg/health/endpoints.go:28` | 023 detail | `%s` namespace/name, `%d` port | Service `%s` has no ready endpoints for port %d. | Service `%s` has no ready endpoint on port %d. | No endpoint of Service `%s` is ready on port %d. | Port %d of Service `%s` has no ready endpoints. | Service `%s`, port %d: no ready endpoints. | kept |
| 3 | S568 | `copy/explorer.ts:463` | Evidence row label, the Service | none | Service | Backend Service | Service name | Backend | Service used by the route | kept |
| 4 | S569 | `copy/explorer.ts:464` | Evidence row label, the port; the value is `80` or `80 (http)` | none | Port | Service port | Backend port | Port (name) | Port used | kept |
| 5 | S570 | `copy/explorer.ts:465` | Evidence row value under the label "Ready" (S400). Total 0 means no endpoints at all | `{ready}`, `{total}` | {ready} of {total} ready | {ready} of {total} (total 0: No endpoints) | {ready} of {total} endpoints (total 0: No endpoints) | {ready} ready out of {total} (total 0: None exist) | {ready}/{total} (total 0: No endpoints at all) | 2 |
| 6 | S571 | `copy/explorer.ts:466` | Evidence row label, the number of EndpointSlices that serve the port | none | EndpointSlices | Slices | EndpointSlice count | Endpoint slices | EndpointSlices for this Service | kept |

## KGWUI-023: availability reasons

`pkg/health/endpoints.go`. Shown in "What was checked" when the check cannot run, after the state ("Off", "No permission to read its input", "Its input is failing"). The denied case shows the API error text, which is not ours.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 7 | S581 | `pkg/health/endpoints.go:29` | Check note, Services or EndpointSlices watch is off | none | Services and EndpointSlices are not watched | Services and EndpointSlices are turned off | The UI does not watch Services and EndpointSlices | The Services and EndpointSlices watches are off | Not watching Services and EndpointSlices | kept |
| 8 | S582 | `pkg/health/endpoints.go:30` | Check note, a watch has not synced | none | Services and EndpointSlices are not synced yet | The Services and EndpointSlices watches have not synced yet | Services and EndpointSlices are still loading | Waiting for Services and EndpointSlices to sync | Services and EndpointSlices have not loaded yet | kept |
| 9 | S595 | `pkg/health/endpoints.go:38` | Check name in "What was checked" | none | backend endpoints | Backend endpoints | backend readiness | ready endpoints | Service endpoints | kept (wire value: the check name is `checks[].name` in the health and diagnostics goldens and the TS `CheckResult`, so a rename changes them) |

## KGWUI-013: Service explanations and the port fix

`pkg/health/refs.go` and `pkg/diagnose/fix.go`. Explanations 2 and 3 are written when a backendRef names a Service the cache can check. The message is followed by the controller's condition text. The fix shows only when the Service has exactly one port. `%s` is `namespace/name` in the explanations and the bare name in the basis.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 10 | S583 | `pkg/health/refs.go:25` | 013 detail start, explanation 2: the Service is missing | `%s` namespace/name | Service `%s` does not exist. | The cluster has no Service `%s`. | Service `%s` not found. | No Service named `%s` exists. | Service `%s` is missing. | kept |
| 11 | S584 | `pkg/health/refs.go:27` | 013 detail start, explanation 3: the Service has no such port | `%s` namespace/name, `%d` port, `%s` ports | Service `%s` has no port %d (ports: %s). | Service `%s` does not have port %d. Its ports: %s. | Port %d is not a port of Service `%s` (ports: %s). | The route uses port %d, but Service `%s` has ports %s. | Service `%s` has no port %d; it has %s. | kept |
| 12 | S585 | `pkg/diagnose/fix.go:39` | Fix title, the Service's only port | `%d` port | Set the port to `%d` | Use port `%d` | Change the port to `%d` | Set port `%d` | Point the ref at port `%d` | kept |
| 13 | S586 | `pkg/diagnose/fix.go:41` | Fix basis, under the title | `%s` Service name | Service `%s` has exactly one port. This sets only group, kind, name, namespace and port; keep the other fields. | Service `%s` has one port. The fix sets group, kind, name, namespace and port only; keep the other fields. | `%s` has exactly one port, so the fix names it. It replaces group, kind, name, namespace and port; keep the other fields. | The only port of Service `%s` is used. Only group, kind, name, namespace and port change; keep the rest. | Service `%s` has a single port. Replace these five fields and keep the others: group, kind, name, namespace, port. | kept |

In S586 option 3, `%s` is the Service name without the "Service" word, so it reads "`web` has exactly one port".

## KGWUI-024: messages, details and evidence

`pkg/health/withheld.go` and `diagnostics/EvidenceView.tsx`. There are two findings. The error (S587, S588) says the controller holds config back for some proxies. The warning (S589, S590) says a release happened before every cluster was ready, in the last 15 minutes. The messages come from design §8.6. The release modes in S590 print as the wire names `flip_release`, `warm_release` and `carry_expired`; the SPA labels (S576–S578) show in the evidence table only.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 14 | S587 | `pkg/health/withheld.go:35` | 024 error message (design §8.6 text) | none | Proxies get no config updates, because referenced clusters are missing | Proxies get no config updates: referenced clusters are missing | Config updates are held back, because referenced clusters are missing | The controller holds config back from proxies, because clusters are missing | Proxies get no new config. Clusters they refer to are missing. | kept |
| 15 | S588 | `pkg/health/withheld.go:36-37` | 024 error detail; the second form is for exactly one proxy | `%d` count | The controller withholds config from %d of this Gateway's proxies. / The controller withholds config from 1 of this Gateway's proxies. | The controller holds config back from %d proxies of this Gateway. / ... from 1 proxy of this Gateway. | %d proxies of this Gateway get no config from the controller. / 1 proxy of this Gateway gets no config from the controller. | The controller withholds config from %d proxies of this Gateway. / ... from 1 proxy of this Gateway. | %d of this Gateway's proxies are not getting config. / 1 of this Gateway's proxies is not getting config. | kept |
| 16 | S589 | `pkg/health/withheld.go:38` | 024 warning message (design §8.6 text) | none | Some routes fail: config was released before every cluster was ready | Some routes fail: the controller released config before every cluster was ready | Config was released before every cluster was ready, so some routes fail | Some routes fail because config went out before its clusters were ready | Routes may fail: config was released early, before every cluster was ready | kept |
| 17 | S590 | `pkg/health/withheld.go:39-40` | 024 warning detail; the second form is for one release. The `(%s)` lists the modes, each as `mode count` | `%d` total, `%s` modes | %d releases in the last 15 minutes (%s). / 1 release in the last 15 minutes (%s). | %d releases in the last 15 minutes: %s. / 1 release in the last 15 minutes: %s. | In the last 15 minutes: %d releases (%s). / In the last 15 minutes: 1 release (%s). | The controller released config %d times in the last 15 minutes (%s). / ... 1 time in the last 15 minutes (%s). | %d releases since 15 minutes ago (%s). / 1 release since 15 minutes ago (%s). | kept |
| 18 | S572 | `copy/explorer.ts:468` | Evidence row label, count of withheld proxies | none | Withheld proxies | Proxies without config | Proxies held back | Withheld from | Proxies getting no config | kept |
| 19 | S573 | `copy/explorer.ts:469` | Evidence table label (its `aria-label`), the release table | none | Released in the last 15 min | Releases in the last 15 minutes | Recent releases | Releases, last 15 minutes | Config released in the last 15 min | 2 |
| 20 | S574 | `copy/explorer.ts:470` | Column label, release table | none | Mode | Kind of release | Release mode | What was released | Case | kept |
| 21 | S575 | `copy/explorer.ts:471` | Column label, release table | none | Releases | Count | Times | Number of releases | Total | kept |
| 22 | S576 | `copy/explorer.ts:473` | Mode label for `flip_release` (wire value stays) | none | Held route change released | Route change released early | Held route change let through | Route change released | Routes released before clusters were ready | kept |
| 23 | S577 | `copy/explorer.ts:474` | Mode label for `warm_release` (wire value stays) | none | Withheld proxy released | Withheld proxy got config | Held proxy released early | Warming proxy released | Proxy released before clusters were ready | kept |
| 24 | S578 | `copy/explorer.ts:475` | Mode label for `carry_expired` (wire value stays) | none | Missing cluster dropped | Missing cluster removed | Cluster dropped after the wait ended | Waiting for the cluster timed out | Cluster dropped after it stayed missing | kept |

## KGWUI-024: availability, version note and check name

`pkg/health/withheld.go`, `pkg/health/process.go` and `copy/explorer.ts`. S362 is the state label for a check whose metric family is absent (design §8.6); it keeps its 3.2 id and shows with the check name in "What was checked". S591–S593 are check notes. The version notes show on the finding when the controller's reported version and its Deployment's tag disagree, and the UI uses the first. S594 is a process-check note changed on this branch.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 25 | S362 | `copy/explorer.ts:508` | Check state `not-on-version` (wire value `not-on-version` stays) | none | Not available on this version | Not available before kgateway 2.5 | This controller does not report it | Not on this version | The controller version has no such metric | kept |
| 26 | S596 | `pkg/health/withheld.go:98` | Check name in "What was checked" | none | withheld config | Withheld config | config withholding | withheld proxies | proxy config release | kept (wire value: the check name is `checks[].name` in the health and diagnostics goldens and the TS `CheckResult`, so a rename changes them) |
| 27 | S591 | `pkg/health/withheld.go:41` | Check note, the controller's metrics have not been scraped yet | none | Controller metrics not collected yet | Controller metrics are not collected yet | The UI has not collected controller metrics yet | Waiting for the first controller metrics | No controller metrics yet | kept |
| 28 | S592 | `pkg/health/withheld.go:42` | Version note, the reported and Deployment versions differ | `%s` reported, `%s` Deployment, `%s` used | The controller reports version %s, but its Deployment says %s. The UI uses %s. | The controller reports version %s; its Deployment says %s. The UI uses %s. | Version %s from the controller differs from %s on its Deployment. The UI uses %s. | The controller says it is %s. Its Deployment says %s. The UI uses %s. | Reported version %s, Deployment version %s. The UI uses %s. | kept |
| 29 | S593 | `pkg/health/withheld.go:43` | Version note, the Deployment has no version | `%s` reported, `%s` used | The controller reports version %s; its Deployment has no version. The UI uses %s. | The controller reports version %s, but its Deployment has none. The UI uses %s. | The controller reports version %s. Its Deployment names no version, so the UI uses %s. | Version %s comes from the controller. The Deployment has no version. The UI uses %s. | The controller says it is %s. Its Deployment has no version, so the UI uses %s. | kept |
| 30 | S594 | `pkg/health/process.go:54` | Check note, the provider reports no processes (state "Not observed") | none | The platform does not report processes | The platform does not report any processes | Processes are not observed on this platform | This platform gives no process data | The platform lists no controller or proxy processes | kept |

## Reused unchanged

Not part of this pass: the 013 explanation 1 text (S488, S489), the evidence headings and badges of S491–S496, the reason messages in `pkg/health/messages.go`, the finding titles and details of 019–022 and 025–026, and the check names, state labels and notes of 3.2 and 3.3 (S360, S361 and S363 included). Process-check constants that only changed alignment in `pkg/health/process.go` are not new strings.
