# Walkthrough scenarios

Small, self-contained kgateway setups for walking through and reviewing the UI. Each scenario shows one thing: a structure to explore, or one problem and the finding it raises. Deploy one on its own to look at it in isolation, or several together to see how they combine.

## Conventions

- Each scenario is one file, `NN-name.yaml`, and owns one or more namespaces named `uc-NN-…`.
- Every namespace carries `scenario: "NN"`, so a scenario is removed by deleting the namespaces with its label.
- Every namespace also carries `team: a`, the label the walkthrough install's `discoveryNamespaceSelectors` selects (as in `test/e2e/setup.sh`). Scenario 17 leaves it off on purpose.
- Each scenario brings its own Gateway, unless it says it builds on another one.

## Status

The list below is the plan. Files are added scenario by scenario; the **Verified** column records what each one raised on a real cluster, with the kgateway version.

## Healthy setups

| # | Name | What it shows | Verified |
|---|---|---|---|
| 01 | Basic storefront | One Gateway with HTTP and HTTPS listeners and three simple HTTPRoutes. Everything is healthy. | – |
| 02 | ListenerSets | One Gateway plus two ListenerSets from two team namespaces. | – |
| 03 | Rich HTTPRoute | Several rules with path and header matches, and a 90/10 canary split across two Services. | – |
| 04 | Route delegation | A parent route delegates to child routes in another namespace. | – |
| 05 | Cross-namespace with ReferenceGrant | A route sends traffic to a Service in another namespace, allowed by a ReferenceGrant. | – |
| 06 | Policies | A TrafficPolicy on the Gateway and on one route, and a BackendConfigPolicy. | – |
| 07 | Backends | kgateway Backend resources (static host) next to plain Services, for the backend's "how it is exposed" view. | – |
| 08 | Shared route | One HTTPRoute attached to two Gateways. | – |
| 09 | Big Gateway | 70 routes, so the map opens on problems only and lists page. | – |
| 10 | Non-HTTP routes | TCPRoute and TLSRoute on their own listeners. | – |
| 19 | Platform and teams | `uc-19-infra` owns a Gateway that accepts routes only from namespaces labelled for access. `uc-19-team-a` and `uc-19-team-b` own their HTTPRoutes and a TrafficPolicy each. `uc-19-shared` holds common backends and the ReferenceGrants for both teams. | – |

## One problem each

| # | Name | What goes wrong | Expected finding | Verified |
|---|---|---|---|---|
| 11 | Route to a missing Service | A backendRef names a Service that does not exist. | `ref-unresolved` (KGWUI-013), error | – |
| 12 | Cross-namespace without ReferenceGrant | A route sends traffic to a Service in another namespace with no ReferenceGrant. | `ref-not-permitted` (KGWUI-014), error | – |
| 13 | Route to a missing listener | A parentRef names a listener (`sectionName`) the Gateway does not have. | `route-unattached` (KGWUI-003), info, then warning | – |
| 14 | Listener conflict | Two listeners use the same port and hostname. | `listener-conflicted` (KGWUI-017), error | – |
| 15 | Policy target missing | A TrafficPolicy targets an HTTPRoute that does not exist. | `policy-target-not-found` (KGWUI-016), error | – |
| 16 | Backend scaled to 0 | A route's Service has no ready Pods. | `backend-no-ready-endpoints` (KGWUI-023), warning after 30 s | – |
| 17 | Namespace not watched | A namespace with a route lacks the `team: a` label. | `namespace-not-watched` (KGWUI-004), warning | – |
| 18 | Proxy cannot start | The Gateway's GatewayParameters set an image that does not exist. | `proxy-not-ready` (KGWUI-006), error | – |
| 20 | Team without access | Builds on 19: a team namespace without the access label attaches a route, and the listener refuses it. | `route-unattached` (KGWUI-003) or `condition-false` (KGWUI-002) with reason `NotAllowedByListeners`, to confirm | – |

## Proxy rejections (NACK)

kgateway accepts these, and the Gateway still reports `Programmed=True`, but Envoy rejects the config. `kubectl` shows nothing wrong.

| # | Name | What goes wrong | Expected finding | Verified |
|---|---|---|---|---|
| 21 | Route rejected by the proxy | A regex path passes kgateway's check but exceeds Envoy's regex size limit, so the proxies reject this Gateway's route config. Two proxy replicas, so the UI counts the rejection per proxy. Only this Gateway is affected. | `xds-rejected` (KGWUI-001), error; on kgateway 2.5 possibly `xds-config-withheld` (KGWUI-024) as well | – |
| 22 | Cluster rejected everywhere | A BackendConfigPolicy sets a cipher suite that does not exist, so Envoy rejects the cluster. kgateway sends clusters to every proxy of the install, so every deployed scenario's Gateway gets the rejection. Deploy it alone to keep it to one Gateway. | `xds-rejected` (KGWUI-001), error, on every proxy of the install | – |

## Not covered

These findings come from the controller itself: `controller-not-ready` (005), `scope-mismatch` (007), `ambiguous-ownership` (009), `process-restarting` (019), `rollout-stuck` (021) and `version-skew` (022). Triggering them means breaking or restarting the kgateway controller, which affects every scenario at once, so no scenario stages them.
