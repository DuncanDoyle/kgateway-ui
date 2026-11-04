# Sub-phase 3.2 — Wording (Task 51)

The final copy of the 3.2 health overview: finding messages and details for 001, 005, 006, 019–022, 025 and 026, check notes and names, availability reasons, next steps, the install page, the Processes page, evidence, the Gateway page's Data plane section, the route chip, API 400 bodies and the chart README (design §17.5). Each string had five options; a separate agent picked one, recorded in the Choice column ("kept" is option 1). Rulings R66 settle the picker's doubtful entries. SPA strings live in `diagnosticsCopy`, `processesCopy` and `dataPlaneCopy` in `web/packages/shell/src/copy/explorer.ts`, whose comment ids match this table. Ids run S304–S470 and continue the 3.1 range.

Option 1 is always the text before the pass. Placeholders (`%s`, `%d`, `{n}`, `{ago}` …) stay in order. Finding codes, kinds and Kubernetes reasons are unchanged.

## Glossary (from the picker)

- **Not available**: an optional check whose input is off, denied or failing. "Turned off" for a toggle.
- **Not observed**: a set the UI cannot see. **Not reported**: an empty field. Neither is "unknown".
- **Unknown**: only the health state. Check notes say "not known".
- **Programmed**: the server and SPA both say "Status says Programmed, but proxies reject its config".
- **New rejects**: a proxy counter grew since the last scrape. Never "rejecting" for one proxy.
- **Restarted recently**: restarts in the 15-minute window.
- **Expects**: the Gateway API minor kgateway is built against.
- **Last exit**: the last termination.
- **Check notes**: sentence case, one clause, no inner period or semicolon, since notes are joined with "; ".
- **Ready counts**: `{ready}/{desired} ready` everywhere.

## Finding messages and details

`pkg/health`. Messages show as the finding title in the findings list, the finding page, the install page's group row (group titles reuse the top finding's message) and the Gateway page. Details show under "Detail" on the finding page.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | S304 | `pkg/health/xds.go:38` | 001 message when Gateway status is current and `Programmed=True`; pairs with S432 | none | Status says Programmed, but proxies reject its config | Status says Programmed, but proxies reject the config | Proxies reject this config, though status says Programmed | Programmed in status, but proxies reject its config | Status shows Programmed. Proxies reject its config | kept |
| 2 | S305 | `pkg/health/xds.go:39` | 001 message otherwise; also `dataPlaneCopy.rejecting` (`copy/explorer.ts:522`), the Gateway page's Data plane badge, which must stay equal | none | Proxies reject this Gateway's config | Proxies reject the Gateway's config | This Gateway's proxies reject its config | Proxies reject config for this Gateway | Proxies refuse this Gateway's config | kept |
| 3 | S306 | `pkg/health/xds.go:40-41` | 001 detail, one sentence per rejected type (`Listener`, `Cluster`, …); 1 uses the singular form | `%s` type short name, `%d` streams | %s rejected on %d streams. / %s rejected on 1 stream. | %s: rejected on %d streams. / %s: rejected on 1 stream. | %d streams reject %s. / 1 stream rejects %s. | Proxies reject %s on %d streams. / Proxies reject %s on 1 stream. | %s rejects on %d streams. / %s rejects on 1 stream. | kept |
| 4 | S307 | `pkg/health/xds.go:42` | 001 detail fallback when no per-type data exists | `%.0f` count | %.0f active rejects | %.0f rejects active now | The controller reports %.0f active rejects. | %.0f rejects reported by the controller | Active rejects: %.0f | kept |
| 5 | S308 | `pkg/health/xds.go:43` | Restart hold: `xDS rejects` check error while held; the strip shows it as "Health unknown: …". The hold keeps a reject that vanished within 60 s of a controller Pod change | none | A controller Pod restarted or was replaced; waiting up to 60 s for proxies to reconnect | A controller Pod restarted or was replaced. Waiting up to 60 s for rejects to return | A controller Pod changed. Rejects stay held for up to 60 s | A controller Pod restarted. Checking again within 60 s | Controller Pod replaced or restarted; rejects held up to 60 s | A controller Pod restarted or was replaced. Waiting up to 60 s for rejects to return |
| 6 | S309 | `pkg/health/xds.go:44` | KGWUI-008 detail and the check error when controller metrics are stale (changed in 3.2) | `%s` RFC 3339 time | controller metrics are stale (last collected at %s) | Controller metrics are stale. Last collected at %s | controller metrics are out of date (last collected at %s) | Controller metrics last arrived at %s and are stale | stale controller metrics (last collected at %s) | Controller metrics are stale. Last collected at %s |
| 7 | S310 | `pkg/health/process.go:45`, parts at `:404-411` | 005 and 006 detail for the first not-ready Pod, e.g. `0/2 ready: CrashLoopBackOff, last exit OOMKilled` | `%d/%d`, `%s` parts; parts `last exit %s`, `last exit code %d` | %d/%d ready: %s | %d of %d ready: %s | %d/%d ready. Pod state: %s | Ready: %d/%d. %s | %d/%d ready (%s) | kept |
| 8 | S311 | `pkg/health/checks.go:676` | 006 message when several Deployments carry the Gateway's label | none | More than one proxy deployment matches this Gateway | More than one proxy Deployment matches this Gateway | Several proxy Deployments match this Gateway | This Gateway matches more than one proxy Deployment | Two or more proxy Deployments match this Gateway | More than one proxy Deployment matches this Gateway |
| 9 | S312 | `pkg/health/process.go:34` | 019 message; `%s` is `Controller` or `Proxy` | `%s` | %s restarted in the last 15 minutes | %s restarted in the last 15 min | %s restarted recently | %s restarted within 15 minutes | %s restarting: restarted in the last 15 minutes | kept |
| 10 | S313 | `pkg/health/process.go:35-36`, `:272-277` | 019 detail; the count form when the restart count grew while watched (`once` / `%d times`) | `%s` Pod, `%s` count | Pod %s restarted %s in the last 15 minutes. / Pod %s restarted in the last 15 minutes. | Pod %s restarted %s in 15 minutes. / Pod %s restarted in the last 15 minutes. | %s restarted %s in the last 15 minutes. / %s restarted in the last 15 minutes. | Pod %s: restarted %s in the last 15 minutes. / Pod %s: restarted in the last 15 minutes. | Pod %s restarted %s recently. / Pod %s restarted recently. | kept |
| 11 | S314 | `pkg/health/process.go:37-38` | 019 detail suffix: the last termination | `%s` reason, `%d` code | ` Last exit: %s (exit code %d).` / ` Last exit code %d.` | ` It last exited with %s (code %d).` / ` It last exited with code %d.` | ` Last exit: %s, code %d.` / ` Last exit: code %d.` | ` Last stop: %s (exit code %d).` / ` Last stop: exit code %d.` | ` Exited with %s (exit code %d).` / ` Exited with code %d.` | kept |
| 12 | S315 | `pkg/health/process.go:39` | 019 detail suffix on the controller when the exit code is 2; the command is a next step (S371) | none | ` A Go panic exits with code 2. The stack trace is in the previous container's log.` | ` Exit code 2 is a Go panic. The previous container's log has the stack trace.` | ` Code 2 means a Go panic. See the previous container's log for the stack trace.` | ` A Go panic exits with code 2. Its stack trace is in the previous log.` | ` Exit code 2 usually means a Go panic. The previous log holds the trace.` | kept |
| 13 | S316 | `pkg/health/process.go:40` | 020 message; `%s` is `Controller` or `Proxy` | `%s` | %s probes failing | %s probes are failing | %s probe failures | %s fails its probes | %s health probes failing | kept |
| 14 | S317 | `pkg/health/process.go:41`, `:334-336` | 020 detail; `%s` is `its <type> probe` when one type is in the spec, else `a probe` | `%s` Pod, `%s` probe, `%d` count | Pod %s failed %s %d times in the last 15 minutes, from Events. | Pod %s failed %s %d times in the last 15 minutes (from Events). | Events show Pod %s failed %s %d times in the last 15 minutes. | Pod %s failed %s %d times in 15 minutes, per Events. | In the last 15 minutes, Pod %s failed %s %d times (Events). | Events show Pod %s failed %s %d times in the last 15 minutes. |
| 15 | S318 | `pkg/health/process.go:42` | 020 detail suffix when the first Event already had a count above 1; pairs with S439 | none | ` Earlier failures not counted.` | ` Earlier failures were not counted.` | ` Failures before the UI saw the Event are not counted.` | ` Older failures are not counted.` | ` The count leaves out earlier failures.` | ` Earlier failures were not counted.` |
| 16 | S319 | `pkg/health/process.go:43` | 021 message; `%s` is `Controller` or `Proxy` | `%s` | %s rollout stuck | %s rollout is stuck | %s rollout passed its deadline | %s rollout not progressing | Stuck %s rollout | kept |
| 17 | S320 | `pkg/health/process.go:44` | 021 detail | none | The rollout passed its progress deadline (ProgressDeadlineExceeded). | The rollout missed its progress deadline (ProgressDeadlineExceeded). | The Deployment stopped progressing (ProgressDeadlineExceeded). | The rollout ran past its deadline (ProgressDeadlineExceeded). | Progress deadline passed (ProgressDeadlineExceeded). | kept |
| 18 | S321 | `pkg/health/process.go:46` | 021 detail suffix: a waiting Pod | `%s` Pod, `%s` reason | ` Pod %s is waiting: %s.` | ` Pod %s waits with %s.` | ` Pod %s is stuck in %s.` | ` Pod %s: %s.` | ` Pod %s is waiting (%s).` | kept |
| 19 | S322 | `pkg/health/skew.go:33` | 022 message, proxy against controller | none | Proxy version differs from the controller | Proxy runs a different version from the controller | Proxy and controller versions differ | Proxy version does not match the controller | Proxy is on another version than the controller | kept |
| 20 | S323 | `pkg/health/skew.go:34` | 022 detail, proxy against controller | `%s` proxy, `%s` controller | Proxy %s, controller %s. kgateway does not support mixed minor versions. | Proxy runs %s, the controller %s. kgateway does not support mixed minor versions. | Proxy %s, controller %s. Mixed minor versions are not supported. | The proxy is %s and the controller is %s. kgateway needs one minor version. | Proxy %s and controller %s differ. kgateway does not support this. | kept |
| 21 | S324 | `pkg/health/skew.go:35` | 022 detail suffix when GatewayParameters sets the proxy image | `%s` namespace/name | ` GatewayParameters %s pins the proxy image, which is the likely cause.` | ` GatewayParameters %s sets the proxy image. That is the likely cause.` | ` The likely cause: GatewayParameters %s sets a fixed proxy image.` | ` GatewayParameters %s fixes the proxy image, which likely causes this.` | ` GatewayParameters %s names the proxy image; it is the likely cause.` | ` GatewayParameters %s sets the proxy image. That is the likely cause.` |
| 22 | S325 | `pkg/health/skew.go:36` | 022 message, controller replicas differ | none | Controller Pods run different versions | Controller Pods are on different versions | Controller replicas run different versions | Not all controller Pods run the same version | Mixed versions across controller Pods | kept |
| 23 | S326 | `pkg/health/skew.go:37` | 022 detail; versions joined by " and " | `%s` versions | Controller Pods run %s after the rollout ended. | Controller Pods still run %s after the rollout ended. | After the rollout, controller Pods run %s. | The rollout ended, but controller Pods run %s. | Controller Pods run %s. The rollout has ended. | Controller Pods still run %s after the rollout ended. |
| 24 | S327 | `pkg/health/skew.go:38` | 022 message, CRD bundle against the expected Gateway API minor | none | Gateway API CRDs do not match this kgateway version | Gateway API CRDs do not fit this kgateway version | Gateway API CRDs are the wrong version for this kgateway | This kgateway version expects other Gateway API CRDs | Gateway API CRD version does not match kgateway | kept |
| 25 | S328 | `pkg/health/skew.go:39` | 022 detail | `%s` CRD, `%s` bundle, `%s` kgateway, `%s` expected | %s is Gateway API %s; kgateway %s is built against %s. | %s is Gateway API %s. kgateway %s is built against %s. | %s is from Gateway API %s, but kgateway %s expects %s. | %s uses Gateway API %s, but kgateway %s needs %s. | %s is Gateway API %s. kgateway %s expects %s. | %s is Gateway API %s. kgateway %s expects %s. |
| 26 | S329 | `pkg/health/skew.go:40` | 022 message, mixed CRD versions or channels | none | Gateway API CRDs are from different versions or channels | Gateway API CRDs mix versions or channels | Gateway API CRDs come from more than one release or channel | Not all Gateway API CRDs share a version and channel | Mixed Gateway API CRD versions or channels | kept |
| 27 | S330 | `pkg/health/skew.go:41` | 022 detail, the odd CRD against the majority | `%s` CRD, `%s` its version, `%s` majority | %s is %s; most Gateway API CRDs are %s. | %s is %s. Most Gateway API CRDs are %s. | %s is %s, but most Gateway API CRDs are %s. | %s is on %s; the others are mostly %s. | %s is %s while most are %s. | %s is %s, but most Gateway API CRDs are %s. |
| 28 | S331 | `pkg/health/proxystats.go:23` | 025 message | none | Proxy is Ready but some config is still loading | Proxy is Ready, but some config is still loading | Proxy is Ready with config still warming | Proxy is Ready but has config still loading | Ready proxy is still loading some config | Proxy is Ready, but some config is still loading |
| 29 | S332 | `pkg/health/proxystats.go:24` | 025 detail; `%s` is up to three Pods (S335) | `%s` Pods | Config is still loading on %s. | Config is still warming on %s. | %s still load some config. | Listeners or clusters are still warming on %s. | Some config is still loading on %s. | kept |
| 30 | S333 | `pkg/health/proxystats.go:25` | 026 message | none | Proxy lost its connection to the controller | Proxy is not connected to the controller | Proxy is disconnected from the controller | Proxy cannot reach the controller | Proxy has no connection to the controller | kept |
| 31 | S334 | `pkg/health/proxystats.go:26` | 026 detail | `%s` Pods | %s lost its connection to the controller. It keeps serving its last config. | %s is not connected to the controller. It serves its last config. | %s lost the controller connection and serves its last config. | %s has no controller connection. It still serves the last config it got. | %s is disconnected. It keeps serving its last config. | kept |
| 32 | S335 | `pkg/health/proxystats.go:32` | Pod list past three, in 025 and 026 details | `%s` names, `%d` rest | %s and %d more | %s, and %d more | %s (+%d more) | %s and %d others | %s, plus %d more | kept |

Note on S328: the four verbs must keep their order (CRD, bundle, kgateway, expected).

## Check notes in "What was checked"

`CheckResult.Error` or `Detail`. `CheckedList` shows them raw next to the check name. A pending required check's error is also sentence-cased into the strip (`HealthStrip.tsx:57`). Some start lower-case today. The apply step should pick one case for all of them.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 33 | S336 | `pkg/health/process.go:47` | `probe failures` not available, `off` | none | Events are turned off | Events are off | Warning Events are turned off | The events toggle is off | Not watching Events | kept |
| 34 | S337 | `pkg/health/process.go:48` | `probe failures` not available while the Events watch syncs | none | the Events watch has not synced yet | The Events watch has not synced yet | Still reading Events | Events are not loaded yet | Waiting for the Events watch to sync | The Events watch has not synced yet |
| 35 | S338 | `pkg/health/process.go:49` | `probe failures` detail at the 10,000 cap | none | More than 10,000 Events; the oldest were dropped | Over 10,000 Events. The oldest were dropped | More than 10,000 Events. Only the newest are kept | Event cap of 10,000 reached; oldest dropped | The oldest Events were dropped at 10,000 | More than 10,000 Events, so some were dropped (R66) |
| 36 | S339 | `pkg/health/process.go:50` | Restarts, probes and rollouts detail when proxy Pods are not watched | none | Proxy Pods are not watched; proxies were not checked | Proxy Pods are not watched, so proxies were not checked | Proxies were not checked: their Pods are not watched | Proxy Pods are not watched. Only the controller was checked | Not watching proxy Pods; proxies not checked | Proxy Pods are not watched, so proxies were not checked |
| 37 | S340 | `pkg/health/skew.go:42` | `version skew` note during a rollout and the 10-minute settle | none | A controller rollout is running or ended less than 10 minutes ago; versions were not compared | A controller rollout is running or just ended. Versions were not compared | Versions are compared 10 minutes after a controller rollout ends | A controller rollout is in progress or under 10 minutes old; versions not compared | Waiting for the controller rollout to settle (10 minutes) before comparing versions | Versions are compared 10 minutes after a controller rollout ends |
| 38 | S341 | `pkg/health/skew.go:43` | `version skew` note | none | Some images have no version tag; those Pods were not compared | Some images have no version tag. Those Pods were not compared | Pods whose image has no version tag were not compared | Some Pods were skipped: their image has no version tag | Images without a version tag were left out | Pods whose image has no version tag were not compared |
| 39 | S342 | `pkg/health/skew.go:44` | `version skew` note | `%s` kgateway version | No Gateway API version on record for kgateway %s; CRDs were not compared with it | The UI does not know which Gateway API version kgateway %s needs. CRDs were not compared | No known Gateway API version for kgateway %s, so CRDs were not compared | kgateway %s has no known Gateway API version; CRDs not compared | CRDs were not compared: no Gateway API version is on record for kgateway %s | No known Gateway API version for kgateway %s, so CRDs were not compared |
| 40 | S343 | `pkg/health/skew.go:45` | `version skew` note | none | Controller version unknown; CRDs were not compared with the Gateway API version it expects | Controller version unknown, so CRDs were not compared | The controller version is unknown. CRDs were not compared with its Gateway API version | CRDs were not compared: the controller version is unknown | No controller version; CRDs not compared | The controller version is not known, so CRDs were not compared (R66) |
| 41 | S344 | `pkg/health/skew.go:46` | `version skew` note when 021 is active on the controller | none | The controller rollout is stuck (KGWUI-021); versions were not compared | The controller rollout is stuck (KGWUI-021). Versions were not compared | Versions were not compared: the controller rollout is stuck (KGWUI-021) | Controller rollout stuck (KGWUI-021); versions not compared | The stuck controller rollout (KGWUI-021) blocks the version check | Versions were not compared: the controller rollout is stuck (KGWUI-021) |
| 42 | S345 | `pkg/health/skew.go:47` | `version skew` note with CRDs off | none | Gateway API CRDs are not watched; CRD versions were not checked | Gateway API CRDs are not watched, so their versions were not checked | CRD versions were not checked: CRDs are not watched | Gateway API CRDs are off; CRD versions not checked | Not watching Gateway API CRDs. Their versions were not checked | Gateway API CRDs are not watched, so their versions were not checked |
| 43 | S346 | `pkg/health/skew.go:48` | `version skew` note while the CRD watch syncs | none | the CRD watch has not synced yet | The CRD watch has not synced yet | Still reading Gateway API CRDs | CRDs are not loaded yet | Waiting for the CRD watch to sync | The CRD watch has not synced yet |
| 44 | S347 | `pkg/health/proxystats.go:27` | `proxy connection` and `proxy config warming`, no stats value yet | none | proxy stats not collected yet | Proxy stats are not collected yet | No proxy stats yet | Waiting for the first proxy stats | Proxy stats have not arrived yet | Proxy stats are not collected yet |
| 45 | S348 | `pkg/health/proxystats.go:28` | Same checks, failing with no reason given | none | proxy stats are not available | Proxy stats are not available | Proxy stats cannot be read | No proxy stats available | Proxy stats failed to load | Proxy stats are not available |
| 46 | S349 | `pkg/health/proxystats.go:29` | Same checks, Gateways with no stats | `%d` gaps, `%d` Gateways | No proxy stats for %d of %d Gateways. | %d of %d Gateways have no proxy stats. | Proxy stats missing for %d of %d Gateways. | No stats from proxies of %d of %d Gateways. | %d of %d Gateways sent no proxy stats. | %d of %d Gateways have no proxy stats. |
| 47 | S350 | `pkg/health/proxystats.go:30-31` | Same checks, failed Pod scrapes | `%d` | 1 proxy failed its stats scrape. / %d proxies failed their stats scrape. | 1 proxy stats scrape failed. / %d proxy stats scrapes failed. | Could not read stats from 1 proxy. / Could not read stats from %d proxies. | 1 proxy did not return stats. / %d proxies did not return stats. | Stats scrape failed on 1 proxy. / Stats scrape failed on %d proxies. | Could not read stats from 1 proxy. / Could not read stats from %d proxies. |
| 48 | S351 | `pkg/proxystats/collector.go:34` | `OffReason`: the 025/026 not-available note with the toggle off | none | proxy stats are turned off | Proxy stats are turned off | Proxy stats are off | The proxy stats toggle is off | Not reading proxy stats | Proxy stats are turned off |

## Check names

Shown in "What was checked" and in the strip's "Cannot check …". They follow the existing lower-case names (`cluster access`, `xDS rejects`). `pkg/api` keeps copies (`diagnostics.go:23` `xdsCheckName`, `details.go:263` `proxyStatsCheckName`), so a rename changes both.

| # | id | File:line | Current | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|
| 49 | S352 | `pkg/health/process.go:16` | process restarts | restarts | container restarts | Pod restarts | recent restarts | kept |
| 50 | S353 | `pkg/health/process.go:17` | probe failures | health probes | probes | failing probes | Pod probes | kept |
| 51 | S354 | `pkg/health/process.go:18` | stuck rollouts | rollouts | rollout progress | stalled rollouts | Deployment rollouts | kept |
| 52 | S355 | `pkg/health/skew.go:20` | version skew | versions | version match | mixed versions | version consistency | kept |
| 53 | S356 | `pkg/health/proxystats.go:52` | proxy connection | proxy connections | proxy to controller connection | proxy xDS connection | proxies connected | kept |
| 54 | S357 | `pkg/health/proxystats.go:50` | proxy config warming | proxy config loading | proxy warming | config warming | proxy config loaded | kept |

## Availability

`unavailableReason` is the fallback note when a not-available check has no error text (`CheckedList.tsx:78-80`), and the Gateway page's proxy-stats fact (`DataPlaneSection.tsx:74-78`). The `/meta` reasons are fixed text in `capabilityStates`, read by extensions through `useCapabilityState`; the core SPA does not render them.

| # | id | File:line | Where | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|
| 55 | S358 | `copy/explorer.ts:486` | Screen-reader prefix on a not-available check; proxy-stats fact fallback | Not available | Unavailable | Not checked | Could not check | Skipped | kept |
| 56 | S359 | `copy/explorer.ts:488` | Reason `off` | Turned off | Off | Turned off in the chart | Its input is turned off | Toggle is off | kept |
| 57 | S360 | `copy/explorer.ts:489` | Reason `denied` | No permission to read its input | No permission to read its data | Access denied | RBAC denies its input | The UI may not read its input | kept |
| 58 | S361 | `copy/explorer.ts:490` | Reason `failing` | Its input is failing | Its data source is failing | Its input cannot be read | Reading its input fails | Input failing | kept |
| 59 | S362 | `copy/explorer.ts:491` | Reason `not-on-version` | Not available on this version | Not available on this kgateway version | This version does not expose it | Needs a newer kgateway | Not on this controller version | kept |
| 60 | S363 | `copy/explorer.ts:492`, `:466` | Reason `not-observed`; also a process set the UI cannot see (`ProcessLine`, `ProcessTable`, Processes page Proxies cell) | Not observed | Not visible to the UI | Nothing to observe | Not seen by the UI | Not watched | kept |
| 61 | S364 | `copy/explorer.ts:471` | Proxies line: Gateways the UI cannot see (`{n}`) | {n} not observed | {n} not visible | {n} not seen by the UI | {n} unobserved | {n} with no proxies seen | kept |
| 62 | S365 | `copy/explorer.ts:460` | A field the source left empty (versions, Pod readiness, stats cells, the muted dash's name) | Not reported | Not given | No value | Empty | Unknown | kept |
| 63 | S366 | `pkg/app/app.go:133` | `/meta` reason, `denied` | Access to this resource is denied | RBAC denies access to this input | The UI may not read this input | Access denied | Permission denied for this input | kept |
| 64 | S367 | `pkg/app/app.go:134` | `/meta` reason, `failing` | The watch is failing | This watch is failing | The watch returns errors | The watch cannot read this input | Watch failing | kept |
| 65 | S368 | `pkg/app/app.go:136` | `/meta` reason, proxy stats collector stopped or stuck (placeholder today) | Proxy stats are not being collected | Proxy stats collection has stopped | The proxy stats collector is failing | Proxy stats are not arriving | Proxy stats cannot be collected | kept |

Note on S365 option 3: "Unknown" is a health state. The picker should treat it as inaccurate.

## Next steps

Step titles and the note: `pkg/diagnose/nextsteps.go`. Rendered by `diagnostics/NextSteps.tsx` on the finding page, the group row and the Gateway page. "Next steps" (S291) and "Needs {permissions}" (S292) are reused.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 66 | S369 | `nextsteps.go:113` | 001 step 1: the controller log of the reporting replica | none | See Envoy's reason | Read the reject reason | See why Envoy rejected it | Find the reject in the controller log | See the reason in the controller log | See the reason in the controller log |
| 67 | S370 | `nextsteps.go:114` | 001 step 2: the proxy's log | none | See it on the proxy | Check the proxy's log | See the reject in the proxy log | Read the proxy log | Find it in the proxy log | See the reject in the proxy log |
| 68 | S371 | `nextsteps.go:115` | 001 step 3: port-forward and read update counters | none | Check the proxy's counters | Read the proxy's update counters | Check the proxy's reject counters | Read the proxy's stats | See the proxy's update counts | Read the proxy's update counters |
| 69 | S372 | `nextsteps.go:116` | 019 step on exit code 2: the previous container's log | none | Read the panic's stack trace | Read the stack trace | See the panic's stack trace | Read the previous log | Get the panic's stack trace | kept |
| 70 | S373 | `nextsteps.go:117` | The secret-care note, on every step | none | Log lines and admin output can contain configuration values, including secrets. Share them with care. | Logs and admin output can include config values, even secrets. Share them with care. | This output can contain secrets. Share it with care. | Logs and admin output may hold config values and secrets. Take care when you share them. | Output can include configuration and secrets. Check it before you share it. | kept |
| 71 | S374 | new, `nextsteps.go` (step 3 only) | The port-forward note design §8.5 and §12 ask for ("the copy says so"). No text exists today | none | (none) | This command opens a connection to the proxy's admin port. | Port-forward opens a connection to the proxy. Stop it when done. | This opens a local tunnel to the proxy's admin port. | The only step that opens a connection: a port-forward to the proxy. | This command opens a connection to the proxy's admin port. |
| 72 | S375 | `copy/explorer.ts:418` | Group row expander; it opens every step of the group | none | Next step | Next steps | Show next steps | What to do | Steps | Next steps |
| 73 | S376 | `copy/explorer.ts:416` | `aria-live` announcement after a copy | none | Copied | Command copied | Copied to clipboard | Copied the command | Done | kept |
| 74 | S377 | `copy/explorer.ts:417` | `aria-live` announcement when the clipboard refuses | none | Could not copy | Copy failed | Cannot copy | Could not copy the command | Copy did not work | kept |

## Install page

`pages/install/*`. The restricted group label reuses S280.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 75 | S378 | `copy/explorer.ts:481` | Section heading over the ranked groups | none | Needs attention | Fix first | Top problems | What to fix | Problems by rank | kept |
| 76 | S379 | `copy/explorer.ts:482` | Link to the grouped findings page | `{n}` | Show all groups ({n}) | All groups ({n}) | See all {n} groups | Show all ({n}) | View all groups ({n}) | kept |
| 77 | S380 | `copy/explorer.ts:419` | Group meta: objects whose green status may be stale | `{n}` | {n} affected | {n} objects affected | Affects {n} | {n} affected objects | {n} objects | kept |
| 78 | S381 | `copy/explorer.ts:420` | Group meta: member findings | `{n}` | {n} finding / {n} findings | {n} problem / {n} problems | {n} in group | {n} grouped finding / {n} grouped findings | {n} related / {n} related | kept |
| 79 | S382 | `pkg/api/diagnostics.go:26` | Group title when the caller cannot view the top finding's resource (the cause is visible) | none | Details hidden | Hidden: you cannot view its object | Title hidden | Restricted: ask the owning team | You cannot view this finding | kept |
| 80 | S383 | `HealthStrip.tsx:67`, `copy/explorer.ts:497` | Strip headline with problems, e.g. `3 problems: 2 errors, 1 warning` | `{n}`, `{counts}` | {n} problem / {n} problems: {counts} | {n} problems ({counts}) | {n} problems. {counts} | {counts} in {n} problems | {n} to fix: {counts} | kept |
| 81 | S384 | `copy/explorer.ts:498` | Strip, age of the oldest open problem | `{ago}` | Oldest problem {ago} | Oldest since {ago} | First problem {ago} | Oldest open {ago} | Problems since {ago} | kept |
| 82 | S385 | `copy/explorer.ts:485` | Toggle heading over the checks; the list's accessible name stays "Checks" | none | What was checked | Checks | Checks run | What the UI checked | Checked | kept |
| 83 | S386 | `copy/explorer.ts:483` | Section heading, controller and proxies lines | none | Processes | Running processes | Controller and proxies | Pods | Process health | kept |
| 84 | S387 | `copy/explorer.ts:484` | Link to the Processes page | none | View processes | All processes | See processes | Open processes | Show processes | kept |
| 85 | S388 | `copy/explorer.ts:494`, `:521` | Gateways table column; Gateway page section heading | none | Data plane | Proxies | Config rejects | Proxy config | Envoy config | kept |
| 86 | S389 | `copy/explorer.ts:495` | Data plane cell and Gateway page badge, no rejects | none | No rejects | Accepted | None rejected | OK | No rejects seen | kept |
| 87 | S390 | `copy/explorer.ts:496` | Data plane cell, rejects | none | Rejects | Rejecting | Config rejected | Rejects config | Has rejects | kept |

## Processes: lines and tables

`diagnostics/ProcessLine.tsx`, `diagnostics/ProcessTable.tsx`, also on the Processes page and the Gateway page's Proxies section.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 88 | S391 | `copy/explorer.ts:462` | Line label and Processes page heading | none | Controller | Control plane | Controller Pods | kgateway controller | Controller set | kept |
| 89 | S392 | `copy/explorer.ts:427`, `:512` | Line label, evidence fact, Processes column, Gateway page heading | none | Proxies | Gateway proxies | Proxy Pods | Data plane proxies | Envoy proxies | kept |
| 90 | S393 | `copy/explorer.ts:463`, `:428` | Ready counts: `2/3 ready` (lines, tables, headings) and `2 of 3 ready` (001 evidence). One form should win | `{ready}`, `{desired}` | {ready}/{desired} ready / {ready} of {desired} ready | {ready}/{desired} ready (both) | {ready} of {desired} ready (both) | Ready {ready}/{desired} (both) | {ready}/{desired} (both) | {ready}/{desired} ready, also for `proxiesReady` (R66) |
| 91 | S394 | `copy/explorer.ts:464` | Controller line fact | `{v}` | version {v} | {v} | Version {v} | running {v} | on {v} | kept |
| 92 | S395 | `copy/explorer.ts:467` | Proxies line with no Gateways | none | No Gateways you can view | No Gateways to show | You can view no Gateways | No visible Gateways | No Gateways | kept |
| 93 | S396 | `copy/explorer.ts:468` | Proxies line fact | `{ready}`, `{total}` | {ready}/{total} Gateways ready | {ready} of {total} Gateways ready | Gateways ready: {ready}/{total} | {ready}/{total} Gateways have ready proxies | {ready}/{total} ready | kept |
| 94 | S397 | `copy/explorer.ts:434` | Proxies line fact: Gateways with active 001 | `{n}` | {n} rejecting | {n} rejecting config | {n} with rejects | {n} reject config | Rejects on {n} | kept |
| 95 | S398 | `copy/explorer.ts:469` | Proxies line fact | `{n}` | {n} with version skew | {n} on another version | {n} with mixed versions | {n} version mismatch | {n} out of version | kept |
| 96 | S399 | `copy/explorer.ts:470` | Proxies line fact | `{n}` | {n} restarting | {n} restarted recently | {n} with restarts | {n} restarted | {n} restarting now | {n} restarted recently |
| 97 | S400 | `copy/explorer.ts:473-474` | Ready column badges | none | Ready / Not ready | Ready / Not ready yet | Yes / No | Ready / Unready | Ready / Not Ready | kept |
| 98 | S401 | `copy/explorer.ts:475-476` | Restarts cell: total, with recent count in the window | `{total}`, `{recent}` | {total} ({recent} recent) / {recent} recent | {total}, {recent} recent | {total} ({recent} in 15 min) / {recent} in 15 min | {total} (+{recent}) / +{recent} | {total} total, {recent} recent / {recent} recent | kept |
| 99 | S402 | `copy/explorer.ts:440` | Column header and evidence fact: restart count | none | Restarts | Restart count | Restarts (15 min) | Recent restarts | Restarted | kept |
| 100 | S403 | `copy/explorer.ts:477` | Column header | none | Last start | Started | Last started | Up since | Start time | kept |
| 101 | S404 | `copy/explorer.ts:441` | Column header and evidence fact | none | Last ended | Last exit | Last stopped | Ended | Last termination | Last exit |
| 102 | S405 | `copy/explorer.ts:442` | Last-ended cell; `Exited` when no reason | `{reason}`, `{exitCode}` | {reason}, exit code {exitCode} / Exited, exit code {exitCode} | {reason} (exit code {exitCode}) / Exit code {exitCode} | {reason}, code {exitCode} / Exited, code {exitCode} | {reason}: exit {exitCode} / Exit {exitCode} | {reason} with code {exitCode} / Exited with code {exitCode} | kept |
| 103 | S406 | `copy/explorer.ts:465`, `:514` | Column header | none | Version | Image version | Running version | Tag | Image tag | kept |
| 104 | S407 | `copy/explorer.ts:478` | Warning badge over a table | `{reason}` | Rollout stuck: {reason} | Rollout is stuck: {reason} | Stuck rollout ({reason}) | Rollout not progressing: {reason} | Rollout stuck ({reason}) | kept |
| 105 | S408 | `copy/explorer.ts:479` | Empty table | none | No processes you can view. | No processes to show. | You can view no processes. | No Pods you can view. | No visible processes. | kept |
| 106 | S409 | `copy/explorer.ts:444` | Column header and evidence fact | none | Probe | Probe failures | Failing probe | Health probe | Probes | kept |
| 107 | S410 | `copy/explorer.ts:445-446` | Probe cell and evidence | `{type}`, `{n}` | {type} probe failed {n} times / A probe failed once | {type} probe: {n} failures / A probe: 1 failure | {type} probe failed {n}× / A probe failed 1× | Failed {type} probe {n} times / Failed a probe once | {n} {type} probe failures / 1 probe failure | kept |
| 108 | S411 | `copy/explorer.ts:443` | Evidence fact: waiting reason | none | Waiting | Waiting reason | State | Stuck on | Waiting on | kept |

## Processes page

`pages/processes/ProcessesPage.tsx`, `processesCopy`.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 109 | S412 | `copy/explorer.ts:503`, `:16` | Page title and nav entry | none | Processes | Pods | Runtime | Controller and proxies | Process health | kept |
| 110 | S413 | `copy/explorer.ts:504` | Search accessible label | none | Filter Gateways | Filter proxies | Search Gateways | Filter by Gateway | Find a Gateway | kept |
| 111 | S414 | `copy/explorer.ts:505` | Search placeholder; the server matches the Gateway's namespace/name | none | Filter by Gateway name | Filter by Gateway name or namespace | Search Gateway names | Gateway name | Type a Gateway name | Filter by Gateway name or namespace |
| 112 | S415 | `copy/explorer.ts:506` | Chip group label | none | Show only | Only | Limit to | Filter | Only show | kept |
| 113 | S416 | `copy/explorer.ts:507` | Chip | none | Version skew | Mixed versions | Version mismatch | Other version | Skewed | kept |
| 114 | S417 | `copy/explorer.ts:508` | Chip | none | Restarting | Restarted recently | Recent restarts | With restarts | Restarts | Restarted recently |
| 115 | S418 | `copy/explorer.ts:509` | Controller table accessible name | none | Controller processes | Controller Pods | Controller | Controller replicas | Controller processes table | kept |
| 116 | S419 | `copy/explorer.ts:510` | Heading and table name | none | Gateway proxies | Proxies by Gateway | Proxies | Proxy sets | Gateway proxy Pods | kept |
| 117 | S420 | `copy/explorer.ts:511` | Column header | none | Gateway | Name | Gateway name | Owner | For Gateway | kept |
| 118 | S421 | `copy/explorer.ts:513` | Column: the 001 stream count | none | Rejecting streams | Streams rejecting | Rejects | Rejected streams | Streams with rejects | kept |
| 119 | S422 | `copy/explorer.ts:515` | Column: restarts in the window | none | Recent restarts | Restarts (15 min) | Restarts | Recent | Restarted | kept |
| 120 | S423 | `copy/explorer.ts:516` | Muted suffix after the proxy versions | none | differs from controller | not the controller's version | controller differs | ≠ controller | other than controller | kept |
| 121 | S424 | `copy/explorer.ts:517` | Version and restarts cells when Pods are hidden | none | Hidden | Restricted | Not shown | You cannot view these | Hidden from you | kept |
| 122 | S425 | `copy/explorer.ts:518` | Gateway page Proxies table accessible name | `{gw}` | Proxies of {gw} | {gw} proxies | Proxies for {gw} | Proxy Pods of {gw} | Proxies: {gw} | kept |

## Evidence

`diagnostics/EvidenceView.tsx`, on the finding page and the Gateway page.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 123 | S426 | `copy/explorer.ts:422` | 001 table name (also on the Gateway page) | none | Rejected config types | Rejected types | Config types rejected | Rejects by type | Rejected xDS types | kept |
| 124 | S427 | `copy/explorer.ts:423` | Column header | none | Type | Config type | xDS type | Resource type | Kind | kept |
| 125 | S428 | `copy/explorer.ts:424` | Column header | none | Streams | Rejecting streams | Proxy streams | Connections | Stream count | kept |
| 126 | S429 | `copy/explorer.ts:425` | Column header | none | First seen | Since | Rejecting since | Seen since | First reject | kept |
| 127 | S430 | `copy/explorer.ts:426` | Column: controller replicas that report the reject | none | Reported by | Controller Pod | Seen by | Reporting replica | Reported by replica | kept |
| 128 | S431 | `copy/explorer.ts:430` | Per-proxy table name | none | Rejects seen on each proxy | Rejects by proxy | Rejects each proxy saw | Proxy reject counters | Rejects per proxy Pod | kept |
| 129 | S432 | `copy/explorer.ts:429` | Warning badge in 001 evidence and the Gateway page headline; pairs with S304 (server says "Programmed") | none | Status says healthy, but proxies reject its config. | Status says Programmed, but proxies reject its config. | Status says Programmed, but proxies reject its config | Status shows healthy, but proxies reject its config. | Programmed in status, but proxies reject its config | Status says Programmed, but proxies reject its config |
| 130 | S433 | `copy/explorer.ts:431` | Column header and fact | none | Pod | Proxy Pod | Proxy | Name | Pod name | kept |
| 131 | S434 | `copy/explorer.ts:435`, `:432` | Per-proxy column and its badge: counter grew in the last scrape (`Rejecting`, or `Rejecting since {ago}` over consecutive scrapes). Design: never "rejecting now" | `{ago}` after "since" | Rejecting / Rejecting since {ago} | Rejects since last scrape / Rejects seen since {ago} | New rejects / New rejects since {ago} | Rejects observed / Rejects observed since {ago} | Counter grew / Growing since {ago} | New rejects / New rejects, started {ago} (R66) |
| 132 | S435 | `copy/explorer.ts:436` | Per-proxy cell, no growth | none | Not rejecting | No new rejects | None since last scrape | No rejects observed | Counter steady | No new rejects |
| 133 | S436 | `copy/explorer.ts:433` | Column: Envoy route config or cluster names | none | Envoy resources | Envoy names | Envoy config names | Rejected Envoy resources | Envoy resource names | kept |
| 134 | S437 | `copy/explorer.ts:437` | Muted suffix when the Envoy names were cut | none | and more | and others | … | (more not shown) | plus more | kept |
| 135 | S438 | `copy/explorer.ts:438` | Heading over objects changed in the 10 minutes before the first reject | none | Changed shortly before the first reject | Changed just before the first reject | Changed in the 10 minutes before the first reject | Recent changes before the reject | Changed before rejects began | kept |
| 136 | S439 | `copy/explorer.ts:439` | Note under S438 | none | A hint, not a cause. | A hint, not a proven cause. | Possible causes only. | These may not be the cause. | A lead, not a cause. | kept |
| 137 | S440 | `copy/explorer.ts:447` | 020 evidence note; pairs with S318 | none | Earlier failures were not counted. | Earlier failures not counted. | Failures before the UI saw the Event are not counted. | Older failures are not counted. | The count leaves out earlier failures. | kept |
| 138 | S441 | `copy/explorer.ts:448` | 022 fact | none | Controller version | Controller | Controller runs | Controller image | Controller is on | kept |
| 139 | S442 | `copy/explorer.ts:449` | 022 fact: the Gateway API minor the controller is built against | none | Gateway API expected | Expected Gateway API | Built for Gateway API | Gateway API it expects | Needs Gateway API | kept |
| 140 | S443 | `copy/explorer.ts:450` | 022 heading | none | Proxy versions | Proxies by version | Versions on proxies | Proxy images | Proxy version | kept |
| 141 | S444 | `copy/explorer.ts:451` | 022 heading | none | Controller replica versions | Controller Pod versions | Controller versions | Versions on controller Pods | Controller replicas by version | kept |
| 142 | S445 | `copy/explorer.ts:452` | 022 heading | none | Gateway API CRDs | CRD versions | Gateway API CRD versions | Installed Gateway API CRDs | CRDs | kept |
| 143 | S446 | `copy/explorer.ts:453` | Pod list suffix in 022 evidence | `{n}` | and {n} more | +{n} more | and {n} others | {n} more | plus {n} more | kept |
| 144 | S447 | `copy/explorer.ts:454`, `:535` | 025/026 table name; Gateway page fact label | none | Proxy stats | Envoy stats | Proxy counters | Stats from proxies | Proxy metrics | kept |
| 145 | S448 | `copy/explorer.ts:455-456` | 025/026 column and its badges | none | Connected / Not connected | Connected / Disconnected | Controller link / Lost | Connected to controller / Not connected | Yes / No | kept |
| 146 | S449 | `copy/explorer.ts:457-458` | 025/026 columns | none | Warming listeners / Warming clusters | Listeners loading / Clusters loading | Listeners warming / Clusters warming | Pending listeners / Pending clusters | Loading listeners / Loading clusters | kept |
| 147 | S450 | `copy/explorer.ts:459` | 025/026 column: Envoy `server.state` | none | Server state | Envoy state | Proxy state | State | Envoy server state | kept |

## Gateway page: Data plane

`pages/explore/gateway/DataPlaneSection.tsx`, `dataPlaneCopy`. The heading is S388, the error badge S305, the healthy badge S389, the next-steps heading S291.

| # | id | File:line | Where | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 148 | S451 | `copy/explorer.ts:523` | Unknown badge: the `xDS rejects` check is not available (controller metrics) | none | Rejects cannot be read right now | Cannot read rejects right now | Rejects are unknown: controller metrics are missing | Cannot tell if proxies reject config | Reject data is not available | kept |
| 149 | S452 | `copy/explorer.ts:524` | Note when evidence names objects the caller cannot view | none | Details hidden: they name objects you cannot view. | Some details are hidden. They name objects you cannot view. | Details hidden: they mention objects you cannot view. | Details are hidden because they name objects you cannot view. | Hidden: these details name objects you cannot view. | kept |
| 150 | S453 | `copy/explorer.ts:526` | Heading over the controller's snapshot counts | none | Built by the controller | Controller snapshot | What the controller built | Sent by the controller | Controller's config | kept |
| 151 | S454 | `copy/explorer.ts:527` | Note under S453 | none | From the controller's metrics: what it built, not what proxies accepted. | From controller metrics. This is what it built, not what proxies accepted. | What the controller built. Proxies may not have accepted it. | Controller metrics show what it built, not what proxies took. | Built, not accepted: from the controller's metrics. | From controller metrics. This is what it built, not what proxies accepted. |
| 152 | S455 | `copy/explorer.ts:528-534` | Snapshot count labels | none | Listeners, Routes, Clusters, Endpoints, Secrets | Listeners, Route configs, Clusters, Endpoints, Secrets | Listeners, RDS, CDS, EDS, SDS | Listeners, Routes, Clusters, Endpoint sets, Secrets | Listener, Route, Cluster, Endpoint, Secret counts | kept |
| 153 | S456 | `copy/explorer.ts:536` | Proxy stats fact when available | none | Collected | Available | Read every 10 s | On | Being collected | kept |

## Routes

| # | id | File:line | Where | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|
| 154 | S457 | `copy/explorer.ts:83` | Route state chip: its Gateway has an active 001 | Gateway rejects config | Gateway's proxies reject config | On a rejecting Gateway | Gateway config rejected | Proxies reject config | kept |

## API error bodies (400)

Shown in the SPA's error states and to API callers.

| # | id | File:line | Placeholders | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|
| 155 | S458 | `pkg/api/diagnostics.go:226` | none | diagnostics takes no parameters | diagnostics accepts no query parameters | no query parameters allowed on diagnostics | diagnostics: unexpected query parameter | the diagnostics route takes no parameters | kept |
| 156 | S459 | `pkg/api/processes.go:333` | `%q` | health %q: want healthy, warning, error or unknown | health %q: must be healthy, warning, error or unknown | bad health %q: use healthy, warning, error or unknown | health %q is not healthy, warning, error or unknown | health %q: expected healthy, warning, error or unknown | kept |
| 157 | S460 | `pkg/api/processes.go:346` | `%s`, `%q` | %s %q: want true or false | %s %q: must be true or false | bad %s %q: use true or false | %s %q is not true or false | %s %q: expected true or false | kept |
| 158 | S461 | `pkg/api/findings.go:333` | tag (`affected`, `processes`) | not a cursor of the {tag} list | not a cursor for the {tag} list | cursor does not belong to the {tag} list | invalid {tag} cursor | this cursor is not from the {tag} list | kept |

## Chart README and values

`deploy/helm/kgateway-ui/README.md` (values table and RBAC), `values.yaml` comments, root `README.md`. Docs, not UI, but the brief includes them.

| # | id | File:line | Where | Option 1 (current) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|
| 159 | S462 | `README.md:39` (chart) | `diagnostics.events` row | Watch Warning Events for controller and proxy Pods. Off drops the `events` rule; checks that need it report "not available". | Watches Warning Events on controller and proxy Pods. Off drops the `events` rule, and checks that need it show "not available". | Warning Events on controller and proxy Pods. Off removes the `events` rule; probe checks read "not available". | Watch Warning Events on controller and proxy Pods. When off, the `events` rule is dropped and dependent checks are not available. | Reads Warning Events on controller and proxy Pods. Turn off to drop the `events` rule; dependent checks then read "not available". | kept |
| 160 | S463 | `README.md:40` | `diagnostics.crds` row | Watch Gateway API CRD metadata (bundle version and channel). Off drops the `customresourcedefinitions` rule. | Watches Gateway API CRD metadata (bundle version and channel). Off drops the `customresourcedefinitions` rule; the CRD check reads "not available". | Gateway API CRD metadata: bundle version and channel. Off removes the `customresourcedefinitions` rule. | Watch the bundle version and channel of Gateway API CRDs. Off drops the `customresourcedefinitions` rule. | Reads Gateway API CRD metadata (version, channel). Turn off to drop the `customresourcedefinitions` rule. | kept |
| 161 | S464 | `README.md:41` | `diagnostics.proxyStats` row | Read Envoy stats from each proxy Pod on port 9091. Needs no RBAC; the UI Pod must reach proxy Pods on 9091. Off skips the reads. | Reads Envoy stats from each proxy Pod on port 9091. Needs no RBAC, but the UI Pod must reach proxy Pods on 9091. Off stops the reads. | Envoy stats from proxy Pods on port 9091. No RBAC needed; allow the UI Pod to reach 9091. Off skips them. | Read each proxy Pod's Envoy stats on port 9091. This needs network access to 9091, not RBAC. Off skips the reads. | Scrapes Envoy stats on port 9091 of each proxy Pod. No RBAC; the UI Pod needs network access to 9091. Turn off to stop. | kept |
| 162 | S465 | `README.md:42` | `diagnostics.endpoints` row (names a phase number) | Grant `services` and `endpointslices`. The UI starts reading them in 3.4. Off drops both rules. | Grants `services` and `endpointslices`. No check reads them yet. Off drops both rules. | Grant `services` and `endpointslices` for a later release. Off drops both rules. | Grants `services` and `endpointslices`, reserved for endpoint checks. Off drops both rules. | Adds `services` and `endpointslices` rules. Not read yet. Off drops them. | Grants `services` and `endpointslices`. No check reads them yet. Off drops both rules. |
| 163 | S466 | `README.md:43` | `diagnostics.controllerLogs` row | Read controller logs for derived signals. Adds a `pods/log` `get` Role in each namespace below. The UI keeps fixed fields and never stores or shows log text. | Reads controller logs for derived signals. Adds a `pods/log` `get` Role in each namespace below. The UI keeps fixed fields only and never stores or shows log text. | Derive signals from controller logs. Adds a `pods/log` `get` Role per namespace below. Log text is never stored or shown. | Read controller logs for extra signals. Creates a `pods/log` `get` Role in each listed namespace. Only fixed fields are kept; log text never is. | Controller log signals. Adds a `pods/log` `get` Role in each namespace below. The UI never stores or shows log text. | kept |
| 164 | S467 | `README.md:44` | `diagnostics.controllerLogsNamespaces` row | Namespaces that get the `pods/log` Role. Only used when `diagnostics.controllerLogs` is on. | Namespaces that get the `pods/log` Role, when `diagnostics.controllerLogs` is on. | The namespaces for the `pods/log` Role. Used only with `diagnostics.controllerLogs`. | Where the `pods/log` Role is created. Needs `diagnostics.controllerLogs`. | Namespaces with a `pods/log` Role. Ignored unless `diagnostics.controllerLogs` is on. | kept |
| 165 | S468 | `values.yaml:13-21` | Comment block and inline comments of `diagnostics` (inline `endpoints` comment also names 3.4) | Data sources for the health checks. Each toggle drops its RBAC rule and its watch; the checks that need it then report "not available". | Inputs for the health checks. Turning one off drops its RBAC rule and watch; its checks then read "not available". | Health-check inputs. Off drops the RBAC rule and the watch, and dependent checks show "not available". | What the health checks read. Each toggle controls one RBAC rule and watch. Off makes dependent checks "not available". | Health check data sources. A toggle that is off removes its rule and watch; its checks read "not available". | Option 1 for the block; the inline `endpoints` comment reads "no check reads them yet", as S465 (R66) |
| 166 | S469 | `README.md:75` (chart) | RBAC paragraph | No Secrets, no ConfigMaps, no `*/proxy` and no `pods/portforward` access. `pods/log` is granted only with `diagnostics.controllerLogs`, in the listed namespaces. | No access to Secrets, ConfigMaps, `*/proxy` or `pods/portforward`. `pods/log` is granted only with `diagnostics.controllerLogs`, in the listed namespaces. | Never Secrets, ConfigMaps, `*/proxy` or `pods/portforward`. `pods/log` only with `diagnostics.controllerLogs`, in the listed namespaces. | The UI gets no Secrets, ConfigMaps, `*/proxy` or `pods/portforward`. It gets `pods/log` only with `diagnostics.controllerLogs`, in the listed namespaces. | No Secrets, ConfigMaps, `*/proxy` or `pods/portforward`. `diagnostics.controllerLogs` adds `pods/log`, only in the listed namespaces. | kept |
| 167 | S470 | `README.md:81` (chart); root `README.md:52-53` | NetworkPolicy sentence; root "Read-only RBAC" bullet | Proxy stats need the UI pod to reach proxy pods on 9091. / root bullet as is | Proxy stats need ingress from the UI Pod to proxy Pods on 9091. | For proxy stats, allow the UI Pod to reach proxy Pods on 9091. | Allow ingress from the UI Pod to proxy Pods on 9091 to get proxy stats. | Proxy stats need the UI Pod to reach port 9091 on proxy Pods. | Proxy stats need the UI Pod to reach port 9091 on proxy Pods. |
| 168 | S471 | `pkg/health/proxystats.go:27` | 026 detail for two or more Pods (Task 53 review round 1) | `%s` Pods | %s lost their connection to the controller. They keep serving their last config. | %s are not connected to the controller. They serve their last config. | %s lost their controller connection and keep serving their last config. | %s have no connection to the controller. Each keeps serving its last config. | %s are disconnected from the controller. They still serve their last config. | %s are disconnected from the controller. They still serve their last config. |

## Reused unchanged

Not part of this pass: "Next steps" (S291), "Needs {permissions}" (S292), "Copy" / "Copy {title}" (S293), "Restricted: ask the owning team" (S280), "Since" (S272), the health labels "Healthy", "Warning", "Error", "Unknown", "Status" (column), "Name", "Clear filters", the list empty states (`listCopy.empty`, `listCopy.emptyFiltered` with the noun "Gateway"), "Last checked {ago}" (S300), and "All healthy" (Phase 1). The `xDS rejects` check name and the 008 messages other than S309 predate 3.2.

## Rulings

- R66: S308 keeps the pick. S338 reads "More than 10,000 Events, so some were dropped". S343 keeps "not known". S434 and S435 keep the picks; the badge renders as "New rejects, started {ago}", since "since 5m ago" reads wrong. S468's inline comment uses the S465 wording. S393 also changes `proxiesReady`, so 001 evidence reads `2/3 ready`.

## Picker notes

- The secret-care note stays. The counters step also says it opens a connection (S374), as design §8.5 and §12 ask.
- Group titles reuse the finding messages; "Details hidden" (S382) stays apart from S280.
- `1 of 2 Gateways have no proxy stats.` keeps one template for every count.
