# Finding reference

Every finding Snellius raises has a code, such as `KGWUI-001`. This page has one entry per code. "Learn more" on a finding's page opens its entry here.

Each entry says:

- **Cause**: what raises the finding, and its severity.
- **Evidence**: what the finding page shows.
- **Fix**: what you can do about it.

Some findings, or parts of their evidence, are computed by the UI, not reported by the controller. The entry says so, and the finding page labels them "Computed". The controller's status always wins: a computed finding is a hint you can check.

Severities: an error turns the install red, a warning turns it yellow, and info never changes its state on its own. A check that is still pending can make the install unknown, as the KGWUI-003 and KGWUI-008 entries say.

**Holds.** Some checks wait before they decide, such as KGWUI-023's 30 s and KGWUI-018's 5 min. While a check waits, its result carries a `hold`: how many items it is deciding on and when the first is due. "What was checked" shows such a check as "Deciding", not green. A hold never changes the install's state, and it starts again when the UI restarts.

**Since.** A finding from a status condition takes the condition's `lastTransitionTime`. For every other finding, `since` is when the UI first saw it. The UI keeps that time in memory, so it resets when the UI restarts. The [README upgrade notes](../README.md#upgrade-notes) say the same. KGWUI-001 is the one exception: with controller logs on, its `since` can come from the log.

**Next steps.** Some findings have next steps: commands filled in for your install. The UI never runs them. Each one names the permission it needs in your own RBAC, such as `pods/log`.

## KGWUI-001

`xds-rejected` · error · controller metrics

**Cause.** The proxies of a Gateway reject the config the controller sends them: the controller's `kgateway_envoy_xds_rejects_active` gauge is above 0 for the Gateway. Only that gauge raises the finding. There is one finding per Gateway, whatever the number of rejected types. When the Gateway's status is current and says `Programmed=True`, the message is "Status says Programmed, but proxies reject its config".

A controller restart drops the gauge until the proxies reconnect, so a real reject would look fixed. When a reject disappears while a controller Pod restarts or is replaced, the UI holds it for 60 s, and the install reads unknown, not healthy.

**Evidence.**

- Per rejected xDS type: the rejecting streams, the controller Pods that report them, when the UI first saw the reject, and the growth of `kgateway_envoy_xds_rejects_total` since then.
- The Gateway's proxies, ready over desired.
- From each proxy's Envoy stats: which Pods saw a reject since the last scrape, per type. For route configs and clusters it also names the Envoy resources. These are Envoy names, such as "Envoy cluster `kube_shop_cart_8080`", not Kubernetes objects.
- Routes, ListenerSets and policies on the Gateway whose generation changed in the 10 minutes before the first reject, at most 10, newest first. This is a hint, not a cause.
- With controller logs on (`diagnostics.controllerLogs`): per rejected type, the time of the earliest `"xds error"` line and an error class from a fixed list, such as `tls` or `duplicate`. It names the Envoy resources in the line only when they equal a name in the Gateway's proxy stats. A listener reject keeps only its class. The UI never keeps or shows the log text, because it can contain secrets.

Routes, ListenerSets and policies whose own status is green for this Gateway carry the `gateway-rejecting` flag. They show at least a warning, because their green status may be stale.

**Since.** When the UI first saw the reject. With controller logs on, the earliest matching log line can move it earlier. The UI reads the last 15 minutes of each controller Pod's log when it starts. So after a UI restart, `since` comes back from the log when that line is recent enough.

**Fix.**

1. Read Envoy's reason in the controller log. The next steps give the command: `kubectl logs` on the controller Pod that reported the reject, filtered for `xds error` and the Gateway's name. It needs `pods/log`.
2. Fix the object that produced the rejected config. Start with the recently changed objects.
3. The reject clears when the proxies accept the next push. A fix that changes nothing in the config sends no push, so the gauge stays set until the next one.

If the controller metrics cannot be read, the UI raises KGWUI-008 for the `xDS rejects` check instead, and the install reads unknown.

## KGWUI-002

`condition-false` · error, warning or info · status

**Cause.** A current condition on an object of this install is `False`, and no more specific code covers its reason. KGWUI-013, 014, 015 and 017 own their reasons, so one condition is always one finding. It covers GatewayClasses, Gateways, ListenerSets and their listeners, routes per parent, policies per ancestor, and Backends that routes use.

Severity is error, with three exceptions:

- a policy ancestor with reason `PartiallyValid` is a warning;
- a policy ancestor with reason `Overridden` is info;
- a Backend whose `Accepted=False` has no `observedGeneration`, as kgateway 2.3 writes it, is a warning.

kgateway 2.5 controllers write `TargetNotFound` for a missing policy target, and the UI shows it as KGWUI-016, not here. A controller whose version the UI cannot read runs on the 2.5 rules, so it is shown as KGWUI-016 too. On 2.3 and 2.4 the UI computes a missing target instead: see KGWUI-016.

**Evidence.** The message is the condition's reason in plain words. The detail quotes the condition as written: type, status, reason and message, and the listener, parent or ancestor it belongs to. A parent route whose backendRefs all delegate reads "A delegating rule has no accepted child route" in place of "Backend not found".

**Fix.** Read the condition's message in the detail. It is the controller's own text and usually names the field. Correct that field. The finding clears when the controller writes a new status.

## KGWUI-003

`route-unattached` · info, then warning · status

**Cause.** The controller has not processed an object, or its status describes an older version. Despite the slug, it covers every kind with status: GatewayClasses, Gateways, ListenerSets, listeners, routes per parent, and policies.

For 60 s after the UI first sees the object's current generation, the finding is info, "Being processed by the controller", and the install reads unknown. After 60 s it is a warning, "Not yet processed by the controller". A route whose parent does not exist reads "Not attached: parent not found".

**Evidence.** The freshness kind:

- `never-written`: the controller never wrote status for the object, or only the CRD's default status is there.
- `stale`: `observedGeneration` is below `metadata.generation`. The evidence quotes both, and when the UI first saw the current generation.
- `pending`: a current condition has reason `Pending` or status `Unknown`.
- `no-ancestor`: no Gateway of this install reports using the policy.

For a kind the controller watches only with its experimental Gateway API features on, the detail says those features may be off.

**Fix.**

1. Check that the object belongs to this install: its Gateway's GatewayClass names this controller, and its namespace is watched (see KGWUI-004).
2. Check that the controller is running and ready (see KGWUI-005).
3. If many objects have this finding at once, see KGWUI-018.
4. For a policy with no ancestor, check its targets. When every target is missing, KGWUI-016 replaces this finding.

## KGWUI-004

`namespace-not-watched` · warning · discovery scope

**Cause.** A route targets a Gateway, ListenerSet or HTTPRoute of this install from a namespace the controller does not watch. The controller's `discoveryNamespaceSelectors` leave the namespace out, so it never sees the route.

**Evidence.** The detail names the parent the route targets.

**Fix.** Label the route's namespace so the controller's selector matches it, or change the selector. If the route should not attach here, remove the parentRef.

## KGWUI-005

`controller-not-ready` · error · Deployments, Pods

**Cause.** The controller Deployment has fewer ready replicas than desired, or is scaled to 0.

**Evidence.** Ready over desired replicas. From the Pods: the waiting reason, such as `CrashLoopBackOff`, and how the last container ended, such as `OOMKilled`. The UI never reads the termination message, as it can hold log text.

**Fix.** Look at the controller Pods with `kubectl describe pod` and `kubectl logs`. Common causes are a crash loop, an image that cannot be pulled, and too little memory. KGWUI-019, 020 and 021 add detail on restarts, probes and rollouts.

## KGWUI-006

`proxy-not-ready` · error or warning · Deployments, Pods

**Cause.** A Gateway's proxy Deployment is not ready. The severity depends on what the UI finds:

- fewer ready replicas than desired: error;
- scaled to 0 replicas: warning, because it may be deliberate;
- no proxy Deployment 60 s after the UI first saw the Gateway: warning. Within those 60 s the install reads unknown;
- more than one Deployment matches the Gateway: warning.

A self-managed Gateway raises KGWUI-010 instead.

**Evidence.** As for KGWUI-005: ready over desired, the waiting reason and the last termination. With controller logs on, it adds the deployer's errors for the Gateway from the last 15 minutes: the error class, such as `apply` or `parameters`, the object it names, and the time. The log text is never shown.

**Fix.** Check the proxy Pods with `kubectl describe pod`. When the Deployment is missing, the deployer could not create it: read the deployer errors or the controller log, and check the Gateway's GatewayParameters. When several Deployments match, remove the one the controller does not own.

## KGWUI-007

`scope-mismatch` · warning · controller metrics

**Cause.** In a namespace, the UI counts a different number of objects than the controller's `kgateway_resources_managed` gauge. The difference lasted at least two metrics intervals (20 s by default). Usually the UI and the controller disagree about which namespaces the controller watches.

**Evidence.** The namespace, the resource, and both counts.

**Fix.** Compare the controller's `discoveryNamespaceSelectors` with the scope on the install page ("N of M namespaces"), and check the namespace's labels. When the controller dropped metric updates, the gauge is wrong until the controller restarts. The UI then raises KGWUI-008 instead of this finding.

## KGWUI-008

`signal-unavailable` · info · the UI's own inputs

**Cause.** The UI could not read an input it needs. When the input is required, the install reads unknown, never healthy. The message names what failed:

- "No access to `<resource>`": the UI's ServiceAccount cannot list or watch a core kind.
- "Could not check cluster access": the cache has not synced yet.
- "Served kinds changed; restart the UI to watch them": a CRD was added or removed. A change limited to DirectResponse, GatewayExtension or ReferenceGrant, which the UI lists but never judges, is no finding: "What was checked" notes it on the served kinds check.
- "Could not check controller scope": the controller's namespace selectors could not be read, for example when they come from `valueFrom`.
- "Could not check xDS rejects": controller metrics are missing, failing or stale, or lack `kgateway_resources_managed` while the install owns Gateways.
- "Could not verify scope against the controller": scope verification could not run. This one is optional and never makes the install unknown.

**Evidence.** The detail gives the reason, such as the API error or the time of the last metrics scrape.

**Fix.**

- No access: check the UI's ClusterRole. Kinds added by a downstream build need `rbac.extraRules`.
- Served kinds changed: restart the UI.
- Metrics: the UI scrapes the controller Pods on port 9092. Check that no NetworkPolicy blocks it.

## KGWUI-009

`ambiguous-ownership` · warning · GatewayClasses, scope

**Cause.** More than one controller install claims a Gateway: their GatewayClass controller names and watched namespaces both cover it.

**Evidence.** The installs that claim it.

**Fix.** Give each controller its own GatewayClass controller name, or make their watched namespaces disjoint.

## KGWUI-010

`self-managed-proxy` · info · GatewayParameters

**Cause.** The Gateway's proxy is self-managed, so the controller does not deploy it, and the UI cannot see its readiness. This finding never changes the install's state.

**Evidence.** None beyond the Gateway.

**Fix.** Nothing to fix. Check the proxy's readiness where you run it.

## KGWUI-011

`delegated-route-not-attached` · info, then warning · status, and computed by the UI

**Cause.** A route delegated from a parent HTTPRoute is not attached. There are three triggers:

- Status: the route names a parent HTTPRoute but has no status entry for it. It is info for 60 s, then a warning. On kgateway 2.3, a child ref that omits the namespace or sets `sectionName` never gets a status entry, so it always gets this warning.
- Computed by the UI: a parent selects the route but drops it, because no rule fits the parent's matches. Or a parent selects it, but the route's parentRefs do not permit that parent. A warning.
- Computed by the UI: the route has no parentRefs, and no parent route of this install keeps it. A warning.

The UI computes delegation with the rules of the controller's version. While the install's discovery scope is unknown, it computes nothing.

**Evidence.** The parent route. A computed trigger says why the parent drops or does not permit the route.

**Fix.** Make the child's matches fit inside the parent's, or fix the child's parentRefs to permit the parent. The parent route's Delegation tab shows what each ref selects and why.

## KGWUI-012

`computed-view-disagrees` · warning · computed by the UI

**Cause.** The UI computes part of what the controller reports, with the rules of the controller's version, and checks the result against status. This finding means the two have disagreed for more than 30 s. It covers:

- route counts per listener;
- whether a route is accepted on a parent;
- delegation: a child's status entry, and a parent's `ResolvedRefs`;
- a policy target the UI computes as missing (KGWUI-016) while the controller writes a current ancestor for it.

**Evidence.** What the UI computed, what the controller reports, and the rule set used. For delegation, the match the ancestor delegated under.

**Fix.** The controller's status is what counts. If the controller is right, the UI's rule set does not match this controller version; report it to the UI project with the detail. If the UI is right, the status may be stale: see KGWUI-003 and KGWUI-018.

## KGWUI-013

`ref-unresolved` · error · status, explanation computed by the UI

**Cause.** The controller cannot resolve a reference. Either a current route parent has `ResolvedRefs=False` with a reason other than `RefNotPermitted`, such as `BackendNotFound` or `InvalidKind`. Or a current listener has `ResolvedRefs=False` with reason `InvalidCertificateRef`. The finding always starts from the controller's condition. The UI never decides on its own that a ref resolves.

**Evidence.**

- The refs the condition can be about, each with its location, such as `spec.rules[2].backendRefs[0]`.
- For a route, an explanation computed by the UI. When discovery says a ref's group serves no such kind, the message reads, for example, "No kind `Service` in group `delegation.gateway.example`. Check the group." Next, when the Services watch is healthy and exactly one core Service ref can be the cause: the detail line says "Service `shop/cart` does not exist.", or "Service `shop/cart` has no port 8080 (ports: 80, 443)." These come from the Service, so they are hidden, with the evidence and the fix, when you cannot view it. A ref the UI cannot check, such as a Backend, on the same parent leaves the finding unattributed with the controller's reason. Otherwise the message is the controller's reason. Without a healthy Services watch the UI never says a Service is missing.
- For a listener, the controller's message. The UI cannot read Secrets, so it never says whether a certificate Secret exists.

**Fix.** Correct the ref the explanation names. The finding page suggests a fix, the ref with the corrected group, when both of these hold:

- the condition can be about only one ref;
- discovery is complete, and exactly one served group has a kind of that name.

When the Service exists and has exactly one port, the page suggests the ref with that port. A Service with several ports, or a ref with no port, gets no fix. If you cannot view the Service, the explanation and fix are withheld.

A route that delegates, or is a delegation child, gets no suggested fix: the condition cannot be tied to one of its refs. The fix sets only group, kind, name and namespace; keep the port and other fields. The UI never applies it.

## KGWUI-014

`ref-not-permitted` · error · status

**Cause.** A ref to another namespace is not permitted: a current route parent or listener has `ResolvedRefs=False` with reason `RefNotPermitted`. In the Gateway API, a ref to another namespace needs a ReferenceGrant in the target namespace.

**Evidence.** The cross-namespace refs the condition can be about, each with its location. For a route these are its backendRefs, mirrors and delegated HTTPRoute refs; for a listener, its certificate refs and the Gateway's CA certificate refs. When a ref's group serves no such kind, the message says so, as for KGWUI-013.

**Fix.** Create a ReferenceGrant in the target namespace that allows the referring kind and namespace. The finding page shows that grant as YAML to copy or download when both of these hold:

- the condition can be about only one cross-namespace ref;
- the ref is a route backendRef or a listener certificateRef, the fields the Gateway API spec defines.

Create it with `kubectl create -f`. The UI counts ReferenceGrants but does not match them against the ref, so if one already exists, compare its `from` and `to` with the suggested one: `kubectl get referencegrants -n <target namespace> -o yaml`.

When that one ref's group serves no such kind, the page suggests the corrected group instead, as for KGWUI-013. If discovery is incomplete, or several groups serve the kind, it suggests nothing.

There is no suggested grant for a route that delegates or is a delegation child, because a delegation cycle also reads `RefNotPermitted`. Nor is there one for a listener whose CA refs may come from a ListenerPolicy, or for a ListenerSet listener whose Gateway is not in this install or sets CA refs. For kgateway's own ref fields, such as policy and extension refs, follow the controller's message.

## KGWUI-015

`rule-replaced` · error · status

**Cause.** The controller replaced a route rule with a direct response, or dropped it, because of invalid configuration. The route's parent status has reason `RouteRuleReplaced` or `RouteRuleDropped`: on `Accepted` on kgateway 2.3, on `kgateway.dev/Programmed` on 2.4.

**Evidence.**

- The reason and the controller's message.
- The match index from the message, as "match N". It counts matches, not rules, so the UI does not map it to a rule.
- The policy the message names, as a link, when it names exactly one served policy kind.
- After a rule has been replaced (not dropped) for 10 minutes, a hint: some versions keep the rule replaced until the controller restarts.

**Fix.** Fix what the message names, often a policy the rule uses. If the cause was temporary, such as an identity provider outage, and the rule stays replaced, restart the controller.

## KGWUI-016

`policy-target-not-found` · error from status on kgateway 2.5 · warning, computed by the UI, on 2.3 and 2.4

**Cause.** A policy targets an object that does not exist.

On kgateway 2.5 the controller reports it: a current ancestor `StatusSummary` with reason `TargetNotFound`. That is one error per policy, raised at once, in place of KGWUI-002. The controller checks targetRefs to Gateways, ListenerSets, routes, Services and Backends, and the `sectionName` of a Gateway, ListenerSet, HTTPRoute or GRPCRoute. It does not check selectors, so on 2.5 the UI computes a selector that matches nothing, or an empty one, as below: a warning labelled "Computed by the UI's policy targets rule". The UI computes no named target on 2.5.

On kgateway 2.3 and 2.4 the controller writes no status for such a policy, so the UI computes it with the rule set for the controller's version. The finding page labels it "Computed by the UI's policy targets rule". It judges five target kinds: Gateway, ListenerSet, XListenerSet, HTTPRoute and GRPCRoute. A missing target of any other kind, such as a TCPRoute or a Service, raises nothing, so no finding does not mean such a target exists. It covers three cases:

- a `targetRef` names no object of that kind in the policy's namespace;
- a `sectionName` names no listener of the Gateway or ListenerSet, or no rule of the route;
- a `targetSelector` matches no object of its kind in the policy's namespace, or is empty. A policy in the controller's global policy namespace (`KGW_GLOBAL_POLICY_NAMESPACE`) matches in every namespace. When the UI cannot read that setting, a selector raises nothing.

On 2.3 and 2.4 it is raised only after the policy's 60 s grace and after the result held for 30 s more. When every target is missing, it replaces the policy's KGWUI-003. A judged kind that the cluster does not serve, or the UI cannot read, raises nothing. While the install's discovery scope is unknown, the rule raises nothing.

**Evidence.** On 2.5, the controller's condition: its message lists the missing targets. On 2.3 and 2.4, the target as written, such as "targetRefs[0] Gateway `edge`", and what is missing. Most of these findings say "Not verified: the controller's status cannot confirm this." The UI can check only a policy with one targetRef: a missing Gateway, or a missing section of a Gateway or route. For those, a current ancestor the controller writes for that target raises KGWUI-012.

**Fix.** Correct the policy's `targetRefs` or `targetSelectors`, or create the target. Check the namespace: a targetRef always looks in the policy's own namespace.

## KGWUI-017

`listener-conflicted` · error · status, other side computed by the UI

**Cause.** The controller reports a listener conflict: a current `Conflicted=True` on a listener of a Gateway or ListenerSet of this install. The controller then also sets the listener's `Accepted` and `Programmed` to `False` with the same reason. This finding covers all three, so no KGWUI-002 repeats them.

**Evidence.**

- The controller's condition: reason and message.
- The other side, computed by the UI from the condition's reason. It looks only at the listeners on the same Gateway and its ListenerSets that the controller kept: current status, `Accepted=True`, not conflicted, with supported kinds.
  - `HostnameConflict`, the "listener port and hostname" rule: listeners with the same port and exactly the same hostname.
  - `ProtocolConflict`, the "listener port and protocol" rule: listeners with the same port and another protocol.
  - Any other reason: no other side.
- When the rule finds no other side, the detail says so, for example "The controller reports a conflict. The UI finds no other accepted listener on port 443 with this hostname".

**Fix.** Give one of the listeners another port or hostname, or align their protocols. When one side is on a ListenerSet, change the side you own.

## KGWUI-018

`status-not-progressing` · warning · status, and controller metrics

**Cause.** Status has stopped moving across the install. This points at one cause, such as a status update that fails on every reconcile, a stuck translation, or a CRD whose schema rejects the status. It is one finding per install, on the controller Deployment.

It is raised when the controller is ready and objects have KGWUI-003 warnings for 5 minutes: at least 10 objects, or at least 10 % of the objects with status and at least 3. A controller that is not ready raises KGWUI-005 instead.

**Evidence.**

- The stuck objects per freshness kind (see KGWUI-003), and the oldest one's age.

**Since.** When the UI first saw the condition. It resets when the UI restarts.

**Fix.** Read the controller log for status errors; the next step filters it for `status`. Then fix what it names, such as the CRD or the controller's RBAC for status updates.

## KGWUI-019

`process-restarting` · warning · Pods, and controller logs

**Cause.** A controller or proxy container restarted in the last 15 minutes: its restart count grew while the UI watched, or its last termination ended in the window. There is one finding for the controller Deployment, and one per Gateway for its proxies. It expires 15 minutes after the last restart.

**Evidence.** The Pod, its restarts in the window, and its last exit reason and code. For the controller, exit code 2 adds a note: a Go panic exits with code 2. With controller logs on, the UI reads the last 200 lines of the previous container's log once after a restart. If they hold a panic, the evidence adds the function name of its top frame. No other log text is kept.

**Fix.** Read the previous container's log; for a controller panic, the next step gives the `kubectl logs --previous` command. An `OOMKilled` exit needs a higher memory limit.

## KGWUI-020

`probe-failing` · warning · Events

**Cause.** One Pod of the controller, or of a Gateway's proxies, failed a probe at least 3 times in the last 15 minutes. The UI counts `Unhealthy` Warning Events. This catches proxies that flap without restarting.

**Evidence.** The Pod, the count and the last time. It names the probe type when the Pod's containers define only one. The UI counts only the increases it saw itself; earlier failures are not counted, and the evidence says so. Probe output is never read.

**Fix.** Check the probe and its target with `kubectl describe pod`. Look for a slow start, overload or a wrong probe port. With Events off (`diagnostics.events`), this check reads "not available".

## KGWUI-021

`rollout-stuck` · warning · Deployments

**Cause.** A controller or proxy Deployment has `Progressing=False` with reason `ProgressDeadlineExceeded`. When no replica is ready, KGWUI-005 or 006 covers it instead.

**Evidence.** The waiting reasons of the Deployment's Pods, such as `ImagePullBackOff` or `CrashLoopBackOff`. For a proxy with controller logs on, the deployer's errors for the Gateway, as for KGWUI-006.

**Fix.** Check the new Pods with `kubectl describe pod` and `kubectl rollout status`. Fix the image, config or resources that stop them, or roll back with `kubectl rollout undo`.

## KGWUI-022

`version-skew` · warning · Pods, CRD metadata

**Cause.** Versions do not match. One of:

- a proxy runs another minor version than the controller, read from the image tags; kgateway does not support mixed minor versions;
- controller Pods still run two versions after a rollout ended;
- the Gateway API CRDs are not the bundle version this kgateway minor expects: v1.5 for 2.3, v1.6 for 2.4, v1.7 for 2.5;
- the Gateway API CRDs carry different bundle versions or channels.

Nothing is compared during a controller rollout and for 10 minutes after it, so the deployer can update the proxies. A patch difference is not a finding. An image tag that is not a version is not compared.

**Evidence.** The controller's version, the proxy or replica versions with their Pods, and the CRDs' bundle versions and channels. When GatewayParameters sets the proxy image, the detail names it as the likely cause.

**Fix.** Upgrade the proxies with the controller: remove or update the image in GatewayParameters. Install the Gateway API CRDs of the expected version, all from one bundle and channel.

## KGWUI-023

`backend-no-ready-endpoints` · warning · Services, EndpointSlices

**Cause.** A route sends traffic to a Service port that has had no ready endpoint for 30 s. HTTP requests to it get a 503; on a TCP or TLS route, connections fail. With a weighted split, only the share sent to this Service fails. The UI checks only a rule `backendRef` to a core `Service` with a weight above 0, on a parent where the controller reports `ResolvedRefs=True` for the route's current generation. The Service must exist and not be `ExternalName`. The UI sums ready endpoints for that port over all the Service's EndpointSlices; an endpoint with `ready` unset counts as ready. One ready endpoint restarts the 30 s. Delegation refs, mirror targets, `Backend` refs and Hostname refs are never checked. There is one finding per Service port; the routes that use it are the affected objects. Scaling to zero can be deliberate, so this is a warning.

Routes whose rule backendRefs name the Service carry the `backend-problem` flag and show at least a warning, like any route to a backend with an error or warning finding (D-082).

**Evidence.** The Service, the port, its ready and total endpoints, and how many EndpointSlices the Service has. A dual-stack Service lists its Pods once per address family, so the counts are those of the larger family.

**Since.** When the port's endpoints first read zero. It resets when the UI restarts.

**Fix.** Check the Service's Pods with `kubectl get endpointslices -l kubernetes.io/service-name=<name>` and `kubectl describe pod`: they may be scaled to zero, not Ready, or not matched by the Service's selector. With Services and EndpointSlices off (`diagnostics.endpoints`), denied or failing, this check reads "not available".

## KGWUI-024

`xds-config-withheld` · error or warning · controller metrics · kgateway 2.5 and later

**Cause.** kgateway 2.5 holds back a proxy's config while its routes name a cluster that is not in CDS yet. Two signals, one finding each per Gateway:
- **Error**, "Proxies get no config updates, because referenced clusters are missing": `kgateway_xds_snapshot_perclient_withheld_clients{state="warm_withheld"}` is above 0. A withheld proxy gets no updates at all, certificate rotations included, until the clusters appear or the warm withhold bound releases it.
- **Warning**, "Some routes fail: config was released before every cluster was ready": `kgateway_xds_snapshot_perclient_bounded_publishes_total` grew for mode `flip_release`, `warm_release` or `carry_expired` in the last 15 minutes. The UI tracks the counter per controller Pod; a Pod's first scrape and a counter reset after a restart are not an increase.

**Evidence.** Per signal, summed over replicas: the error lists the withheld proxies and the controller Pods reporting them; the warning lists the releases per mode in the last 15 minutes and the Pods behind them. The error also shows as "Config withheld" in the overview's Data plane column and makes the Gateway's proxies an error on the Processes page.

**Since.** When the UI first saw the finding. It resets when the UI restarts.

**Fix.** Find the route or backend that names the missing cluster: check the Gateway's routes for a backend that was just added or renamed, and the controller log for snapshot deferrals. A short spike after a controller restart can be translation lag; a lasting one is not.

On kgateway 2.3 and 2.4 the metrics do not exist, so "What was checked" shows this check as "not available on this version". The install stays healthy. Presence of the metrics decides availability, not the version string: the 2.5 per-client families or `kgateway_build_info`. When `kgateway_build_info` reports a version, the UI uses it over the Deployment's version label or image tag for the rule set, the install header and KGWUI-022, and "What was checked" names both when they differ. A value that is not a version, such as `undefined` from a build without version flags, is ignored.

On kgateway 2.3 and 2.4 with controller logs on, the Gateway's Data plane section shows "Proxies may not be getting config updates" when the controller keeps logging a snapshot deferral for the Gateway. That is evidence on the Gateway page, not a finding.

## KGWUI-025

`proxy-config-warming` · warning · proxy stats

**Cause.** A Ready proxy has had listeners or clusters warming for 60 s: Envoy's `listener_manager.total_listeners_warming` or `cluster_manager.warming_clusters` is above 0. Part of its config is still loading. Warming can be slow for a good reason, so this is a warning.

**Evidence.** The proxy Pods, with their warming listeners and clusters.

**Fix.** Read the proxy log with `kubectl logs` for a cluster or listener that cannot finish loading, such as a cluster with no endpoints or a missing certificate. The UI reads proxy stats on port 9091; when it cannot reach that port, this check reads "not available".

## KGWUI-026

`proxy-disconnected` · warning · proxy stats

**Cause.** A Ready proxy has not been connected to the controller for 60 s: Envoy's `control_plane.connected_state` is 0. The proxy keeps serving its last config, but gets no updates.

**Evidence.** The proxy Pods, with their connection state and Envoy's server state.

**Fix.** Check that the controller is ready (see KGWUI-005) and that the proxy can reach the controller's xDS Service. Read the proxy log for connection errors. After a controller restart, the proxy usually reconnects on its own.
