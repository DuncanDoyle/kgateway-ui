# kgateway-ui Helm chart

Installs the read-only kgateway UI: a Deployment, a ClusterIP Service, a ServiceAccount, a ClusterRole and its binding, and optionally an HTTPRoute.

## Security and trust model

The UI has no login. Whoever reaches it sees everything its ServiceAccount can read, which can be more than their own Kubernetes RBAC allows. Treat access to the UI as cluster-read access.

- The ClusterRole never grants Secrets. That is the UI's whole secret-handling guarantee: no other field is redacted, and Deployments and other objects are shown as stored.
- The default exposure is a port-forward to the ClusterIP Service.
- `httpRoute.enabled` is for operators who put authentication and authorisation on the route, for example a kgateway TrafficPolicy with OIDC or external auth that admits only operators.
- Per-user authorisation is on the roadmap.

## Install

```sh
# Install a released chart; --version picks the chart and the matching image
helm install ui oci://ghcr.io/duncandoyle/charts/kgateway-ui --version 0.1.0 \
  -n kgateway-ui --create-namespace
kubectl -n kgateway-ui port-forward svc/ui-kgateway-ui 8080:80
```

To install from a checkout, set the image tag to a released version. The in-repo `appVersion` is not bumped on release, so without it the templates from your checkout run an older image.

```sh
helm install ui ./deploy/helm/kgateway-ui -n kgateway-ui --create-namespace \
  --set image.tag=<version>
```

## Values

| Key | Default | Description |
|---|---|---|
| `image.repository` | `ghcr.io/duncandoyle/kgateway-ui` | Image repository. |
| `image.tag` | `""` | Image tag; defaults to the chart `appVersion`. Required for a local-path install. |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy. |
| `install.pin` | `""` | Pin the UI to one controller install: an install ID (e.g. `kgw.kgateway`) or `namespace/name`. Empty shows all installs. |
| `httpRoute.enabled` | `false` | Create an HTTPRoute on an existing Gateway. Read [Security and trust model](#security-and-trust-model) first. |
| `httpRoute.parentRefs` | `[]` | Gateway parentRefs for the route. See [Security and trust model](#security-and-trust-model). |
| `httpRoute.hostnames` | `[]` | Hostnames for the route. See [Security and trust model](#security-and-trust-model). |
| `serviceAccount.create` | `true` | Create the ServiceAccount. |
| `serviceAccount.name` | `""` | ServiceAccount name; defaults to `<release>-kgateway-ui`. |
| `resources` | 50m CPU / 128Mi requested, 512Mi memory limit | Container resources. |
| `podSecurityContext` | `runAsNonRoot`, `RuntimeDefault` seccomp | Pod security context. |
| `securityContext` | no privilege escalation, read-only root FS, all capabilities dropped | Container security context. |

## Recompute

The UI recomputes its model when a watched object, an access state, a metrics scrape or the served kinds change, debounced to at most about one recompute per second. The binary's `--poll-interval` (default 30s, not set by the chart) is only the safety timer: the longest time between recomputes. Setting it to `2s` restores the earlier fixed 2 s cadence as a floor.

## RBAC

The ClusterRole grants only `get`, `list` and `watch`:

- `gateway.networking.k8s.io`: gatewayclasses, gateways, httproutes, grpcroutes, tcproutes, tlsroutes, backendtlspolicies, listenersets.
- `gateway.networking.x-k8s.io`: xlistenersets.
- `gateway.kgateway.dev`: all resources.
- Core: namespaces, pods. `apps`: deployments.

No Secrets, no ConfigMaps and no `*/proxy` access. Pods and Deployments are only listed and watched through the controller detectors' label selectors. RBAC cannot express a label restriction, so the grant itself is cluster-wide.

Kinds added downstream through `extension.WithKinds` need matching rules in this ClusterRole.

## NetworkPolicy

In-cluster, the UI scrapes controller metrics directly from the controller pods on port 9092. If a NetworkPolicy blocks that traffic, the UI reports the signal as unavailable (KGWUI-008) instead of showing it green. Allow ingress from the UI pod to the controller pods on 9092 to get metrics-based checks.
