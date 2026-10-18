# Phase 1 — Foundation & Overview: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A deployable, read-only kgateway UI that shows each controller install's health, with reasons, and an inventory, as one Go binary with an embedded React SPA, installable with Helm or runnable locally.

**Architecture:**
- **Backend.** A Go backend keeps a shared informer cache of Gateway API and kgateway kinds. It discovers kgateway controller installs, resolves each install's scope (mirroring `discoveryNamespaceSelectors`, verified against the controller's own metric), and assigns resources to installs. It runs registered health checks and serves a versioned JSON API.
- **Frontend.** A React SPA built from a reusable shell package renders the install page (layout A) and the cluster landing (layout C) in the "Control room" theme.
- **Extension.** Everything a downstream needs is registered through `pkg/extension` and the shell's extension object.

**Tech Stack:**
- Backend: Go 1.26, client-go v0.36, `sigs.k8s.io/gateway-api` v1.6.2 (types only), `prometheus/common/expfmt`.
- Frontend: React 19, TypeScript, Vite, emotion, react-router, SWR, TanStack Table/Virtual, lucide-react, js-yaml, Vitest, Testing Library, Playwright, Biome.
- Packaging and test infrastructure: Helm 3, kind.

**Spec:** `docs/phases/phase-1/design.md` (decisions D-001 … D-013 in `docs/decisions.md`). Read both before starting any task.

**Revision:** this plan was revised after the cross-model design review (`docs/phases/phase-1/reviews/design-codex.md`, triage rows 1–15, 17, A1–A3). Finding numbers in comments ("finding 5") refer to that file. Row 16 is deferred to Carry-over. A second and a third review (`reviews/design-r2-codex.md`, `reviews/design-r3-codex.md`) are cited as "round 2, finding N" and "round 3, finding N".

## Global Constraints

- The repo is **PUBLIC**. The forbidden brand terms of the public-repo hygiene rule must not appear in code, comments, file names, docs, commit messages or PR text. The agent tooling's `public-repo-hygiene` skill holds the list; this repo never spells it out. Downstreams are called "downstream distributions".
  - The CI `hygiene` job reads the pattern from the GitHub repository variable `FORBIDDEN_TERMS` (set once by the repo owner).
  - Before every commit, run the same check locally: `git diff --cached | grep -inE "$FORBIDDEN_TERMS" && echo STOP || true`, with `FORBIDDEN_TERMS` exported in your shell.
- **Read-only (D-001):** no create/update/patch/delete calls to the Kubernetes API, ever. **No Secrets:** never list, watch or serve Secrets.
- **Trust model (D-013):** whoever reaches the UI may see everything it shows; there is no login in Phase 1 and no field redaction. Every API handler that returns resources, findings or counts goes through the `api.Authorizer` seam (Phase 1 ships `AllowAll`). A denied object returns 404 and is dropped from resource lists and count tiles. A finding on it is redacted, not dropped: it keeps code, severity and resource ref, carries `restricted: true`, and loses its message and evidence. The health roll-up and severity counts always cover every finding.
- Never call the controller admin server (`:9095`) or the Envoy admin API.
- **Go:**
  - Module `github.com/DuncanDoyle/kgateway-ui`, `go 1.26`.
  - **Do not import `github.com/kgateway-dev/kgateway`.**
  - Every exported identifier has a Go doc comment (https://go.dev/doc/comment).
- **Frontend:**
  - No proprietary component libraries.
  - One React component per file, named export.
  - `// region Styles` / `// region Helpers` / `// region Component` ordering.
  - emotion styled components. Every exported component accepts `stylesOverride`; page-internal styled parts are not part of the contract (a downstream replaces a whole page through `CorePages`).
  - Typography comes from theme tokens (`theme.font.size.*`, `theme.font.weight.*`); components never hard-code font sizes or weights (D-008).
  - Text must never rely on colour alone. Health is always icon + colour + text.
- **Upstream contract** (verified on kgateway `8a498c6406`, v2.4.x and v2.3.x):

  | Item | Value |
  |---|---|
  | Controller Deployment labels | `kgateway=kgateway`, `app.kubernetes.io/component=controller` (plus `app.kubernetes.io/name`, `app.kubernetes.io/instance`, `app.kubernetes.io/version`). This is `ControllerSelector()`. |
  | Controller Pod labels | `kgateway=kgateway`, `app.kubernetes.io/name`, `app.kubernetes.io/instance`; `app.kubernetes.io/component=controller` is added to the Pod template from v2.3.0 ([`9041e96061`](https://github.com/kgateway-dev/kgateway/commit/9041e96061)) and is absent on v2.2 and earlier charts. The cache watches Pods with `ControllerPodSelector()` = `kgateway=kgateway` only, and `Discover` attributes Pods to an install through the controller Deployment's `spec.selector` in the same namespace (A1). |
  | Controller Deployment name | `kgateway.fullname`: the release name when it contains `kgateway`, else `<release>-kgateway` (so release `kgateway` → Deployment `kgateway`) |
  | Controller image name | last path segment `kgateway` (`cr.kgateway.dev/kgateway-dev/kgateway:<tag>`) |
  | Version label | `app.kubernetes.io/version` |
  | Metrics container port | name `metrics`, default `9092` |
  | Proxy Deployment labels | `app.kubernetes.io/managed-by=kgateway`, `app.kubernetes.io/component=proxy`, `gateway.networking.k8s.io/gateway-name=<gw>`. For Gateway names over 63 characters the label value is truncated and hashed, so the proxy is matched by ownerReference first (Review Focus 1). |
  | Controller name | `kgateway.dev/kgateway` (including the `kgateway-waypoint` class) |
  | Scope env | `KGW_DISCOVERY_NAMESPACE_SELECTORS`: a JSON list of `metav1.LabelSelector`, OR'ed; empty or unset means all namespaces |
  | Reject metric | `kgateway_envoy_xds_rejects_active{gateway_namespace,gateway_name,type_url}`; `type_url` has the `type.googleapis.com/` prefix stripped (e.g. `envoy.config.listener.v3.Listener`). The series exists only after a first reject and then stays at 0. |
  | Managed metric | `kgateway_resources_managed{namespace,parent,resource}`. Weights: Gateway 1; route 1 per `spec.parentRefs` entry; ListenerSet 1 as `resource="XListenerSet"`; policy 1 under its kind |
  | Condition freshness | kgateway sets `observedGeneration` on its conditions (Task 1 Step 3 confirms on 2.3 and 2.4). A missing observedGeneration counts as stale (round 2, finding 3). |
  | Route conditions | Required: `Accepted`, `ResolvedRefs`. Optional, evaluated when present: `kgateway.dev/Programmed` (v2.4.x and main) with reasons `RouteRuleDropped` and `RouteRuleReplaced`. v2.3.x reports dropped and replaced rules as `Accepted=False` with the same reasons and the rule in the message (A2); any current `Accepted=False` is reported with its reason message. |
  | Policy conditions | Required: `Accepted`. Optional, evaluated when present: `Attached`. Reasons `Valid`, `Invalid`, `Attached`, `Merged`, `Overridden`, `Pending`, `PartiallyValid`. `ancestorRefConditionsWithDefaults` (`pkg/reports/policy.go` on v2.3.11 and v2.4.7) writes `Accepted=False/Pending` and `Attached=False/Pending` before translation; the UI treats reason `Pending` on any block as "not yet processed" (round 2, finding 2). **Main only** (not on v2.3.x or v2.4.x): reason `TargetNotFound` and the synthetic ancestor kind `StatusSummary`, group `gateway.kgateway.dev`. On v2.3.x and v2.4.x a policy whose target is missing has no status, which the UI reports as "not yet processed" (KGWUI-003) after the grace period (finding 7). |

- **Finding codes** are stable: `KGWUI-001` xds-rejected … `KGWUI-009` ambiguous-ownership (design §5.2).
- **Roll-up:** error > unknown > warning > healthy (design §5.3).
- **Supported versions:** kgateway v2.3.11 (Gateway API v1.5.1) and v2.4.7 (Gateway API v1.6.1).
- **AGENTS.md change-size rule:** approval of this plan covers its file count and size. Anything beyond it needs approval again.

## Review Focus

These failure modes are implied by the spec, but no other task's tests exercise them. Each has a test in the owning task.

1. **Long Gateway names (more than 63 characters).** The proxy Deployment's `gateway-name` label is truncated. The proxy must still be matched to its Gateway (by ownerReference first, then label); otherwise the UI raises a false "proxy not found". → Task 9.
2. **Cross-namespace parentRefs.** A route in `shop` targets `infra/edge`. Assignment and the unwatched-namespace check must use the **route's** namespace for scope, not the Gateway's. → Task 7.
3. **Controller in CrashLoopBackOff (0 ready Pods).** The scope must still be read from the Pod spec, and the report must show controller-not-ready (error) plus metrics unavailable. It must never be silently healthy. → Tasks 6 and 9.
4. **Stale or missing status.** A condition with `observedGeneration < metadata.generation` describes an old spec, whether it is `True` or `False`; a missing observedGeneration (0) counts as stale. Freshness is checked before status. A stale condition, a missing required condition, a condition with reason `Pending`, a configured listener without a status block, a route parent with no status entry for its exact parentRef (port and section included), and a policy with no ancestor from the install's controller all mean "not yet processed". Inside the 60 s grace period, measured from when the engine first saw the object's current generation, that is KGWUI-003 at info and the install is unknown; after it, a warning. KGWUI-003 is one per (resource, parentRef). None of them is ever reported as a current error or a current success (finding 1; round 2, findings 2–5; round 3, findings 3 and 5). → Task 9 (unit) and Task 10 (API, isolated cases).
5. **Scale.** 5,000 HTTPRoutes across 200 namespaces must compute a full model (assignment, health and overview) in under 1 s on a laptop, so the 2 s recompute loop never stacks up. → Task 10.
6. **Watch failures after the initial sync.** `HasSynced()` stays true after later list/watch failures, and client-go retries connection-refused and 429 watch errors without calling the watch error handler. Every informer therefore runs over an instrumented ListerWatcher that records each List and Watch call outcome. A kind is unavailable from its last failed call until its next successful Watch call, so a successful list alone never restores it. The cluster-access check then forces "unknown". Namespaces, Pods and Deployments are covered too (finding 2; round 3, finding 1). → Task 5 (fake-client watch reactors) and Task 10 (API).
7. **Old evidence.** A metrics result older than 3 × the metrics interval is unavailable (KGWUI-008), however often the model is recomputed. In the frontend, stale data turns the health strip, every gateway row, the cluster cards, the install switcher and the header readiness icon to unknown, re-evaluated every second by a shared clock even when polls return identical data (finding 3; round 2, finding 6; round 3, finding 4). → Tasks 8, 9, 10, 15, 16 and 17.
8. **Install switch.** While install B loads, the page never shows install A's strip, findings or gateways (finding 4). → Task 16.
9. **Partial visibility.** A deny-some `Authorizer` registered through `extension.WithAuthorizer` makes the denied object 404 and drops it from the count tiles. Its finding stays in the list with the same code, severity and resource ref, `restricted: true` and no message or evidence; the roll-up state is unchanged (D-013). → Task 11.

## Sub-phases

This plan is too big for one reviewable change, so it is split into planned sub-phases. Each sub-phase ends green (tests pass) and is committed. The phase cross-model review covers all three.

| Sub-phase | Tasks | Outcome |
|---|---|---|
| **1.1 Backend** | 1–11 | `go run ./cmd/kgateway-ui --kubeconfig …` serves the full JSON API against a real cluster |
| **1.2 Frontend** | 12–18 | The SPA, embedded in the binary, renders all pages |
| **1.3 Packaging, CI, e2e** | 19–23 | Image, Helm chart, CI, kind e2e on 2.3 and 2.4; README; upstream proposal draft |

## File map

```
go.mod, go.sum, Makefile, LICENSE, README.md, .gitignore, biome.json (web)
cmd/kgateway-ui/main.go                 flags → app.Run
pkg/version/version.go                  build version
pkg/kinds/{kinds.go,upstream.go}        descriptors, served-version resolution
pkg/objects/objects.go                  typed views over unstructured Gateway API objects
pkg/cache/cache.go                      shared informers → Snapshot
pkg/install/{install.go,upstream.go}    Install, Detector, Discover
pkg/install/scope.go                    Scope, ScopeProvider, NamespaceSelectorScope
pkg/install/assign.go                   Assign: resources → installs
pkg/ctrlmetrics/{parse.go,scrape.go}    metrics parsing, scrapers, Collector
pkg/scopeverify/verify.go               Verifier, ManagedCountVerifier
pkg/health/{finding.go,messages.go,checks.go,rollup.go}
pkg/overview/overview.go                header, counts, gateway rows
pkg/engine/engine.go                    periodic model computation
pkg/api/{types.go,handlers.go,authz.go} HTTP JSON API and the Authorizer seam (D-013)
pkg/extension/extension.go              Registry + functional options
pkg/app/app.go                          Build/Run wiring (used by upstream and downstream mains)
internal/webui/{embed.go,dist/index.html}  embedded SPA (placeholder committed)
internal/testutil/{fixtures.go,clients.go} test fixtures and fake clients
web/package.json, web/tsconfig.base.json, web/biome.json
web/packages/shell/…                    reusable shell package
web/app/…                               upstream entry (src/vite-env.d.ts declares Vite asset imports)
web/e2e/…                               Playwright specs
deploy/helm/kgateway-ui/…               chart
deploy/helm/chart_test.go               helm template assertions
test/e2e/{setup.sh,fixtures/*.yaml,api_test.go}
.github/workflows/{ci.yaml,e2e.yaml,release.yaml}
Dockerfile, .dockerignore
```

---

# Sub-phase 1.1 — Backend

### Task 1: Cluster experiment — shared controllerName and metric replica semantics

This resolves design §13 items 1 and 2 before any assignment or metrics code is written. **No product code.**

**Files:**
- Modify: `docs/decisions.md` (append D-012 with the observations)
- Modify: `docs/phases/phase-1/design.md` §13 (mark items 1 and 2 resolved, linking D-012)
- Create: `pkg/ctrlmetrics/testdata/v2.4.7.txt`, `pkg/ctrlmetrics/testdata/v2.3.11.txt` (captured controller `/metrics` output with an induced xDS reject; committed with Task 8)

**Interfaces:** Produces facts that Tasks 7 and 8 rely on, and the captured metrics fixtures for Task 8.

- [ ] **Step 1: Create a cluster with two disjoint kgateway installs**

```bash
kind create cluster --name kgwui-exp
kubectl apply --server-side -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.6.1/standard-install.yaml
helm install kgateway-crds oci://cr.kgateway.dev/kgateway-dev/charts/kgateway-crds --version v2.4.7 -n kgw-a --create-namespace
# Install A watches team=a namespaces, install B watches team=b namespaces.
# Release name "kgateway" in each namespace: kgateway.fullname collapses to the
# release name, so the controller Deployments are kgw-a/kgateway and kgw-b/kgateway.
helm install kgateway oci://cr.kgateway.dev/kgateway-dev/charts/kgateway --version v2.4.7 -n kgw-a \
  --set-json 'discoveryNamespaceSelectors=[{"matchLabels":{"team":"a"}}]'
helm install kgateway oci://cr.kgateway.dev/kgateway-dev/charts/kgateway --version v2.4.7 -n kgw-b --create-namespace \
  --set-json 'discoveryNamespaceSelectors=[{"matchLabels":{"team":"b"}}]'
kubectl get deploy -n kgw-a kgateway && kubectl get deploy -n kgw-b kgateway   # confirm the rendered names before continuing
kubectl create ns app-a && kubectl label ns app-a team=a
kubectl create ns app-b && kubectl label ns app-b team=b
for ns in app-a app-b; do cat <<EOF | kubectl apply -f -
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata: {name: gw, namespace: $ns}
spec:
  gatewayClassName: kgateway
  listeners: [{name: http, port: 8080, protocol: HTTP}]
EOF
done
```

If the second `helm install` fails on a cluster-scoped name conflict, record the exact error. That is itself the answer to item 1 ("two installs cannot coexist without changes"), and the experiment stops there.

- [ ] **Step 2: Observe ownership**

```bash
kubectl get gatewayclass kgateway -o yaml | yq '.status'          # who writes it; does it flap?
kubectl get gateway -A -o wide                                     # both Programmed?
kubectl get deploy -n app-a -l app.kubernetes.io/component=proxy   # proxy created by A only?
kubectl get deploy -n app-b -l app.kubernetes.io/component=proxy
kubectl logs -n kgw-a deploy/kgateway --since=2m | grep -i -E 'gatewayclass|conflict' | head
kubectl get gatewayclass kgateway -o jsonpath='{.metadata.resourceVersion}'; sleep 30; \
kubectl get gatewayclass kgateway -o jsonpath='{.metadata.resourceVersion}'   # flapping if it changes repeatedly
```

Expected (to be confirmed):
- each Gateway is programmed only by the install whose selector matches its namespace;
- the GatewayClass status is written by both installs, possibly flapping.

- [ ] **Step 3: Observe the metric replica semantics**

```bash
kubectl scale -n kgw-a deploy/kgateway --replicas=2 && kubectl rollout status -n kgw-a deploy/kgateway
kubectl get pod -n kgw-a -l kgateway=kgateway --show-labels   # record the Pod labels for the contract table
for p in $(kubectl get pod -n kgw-a -l kgateway=kgateway -o name); do
  kubectl port-forward -n kgw-a $p 19092:9092 >/dev/null 2>&1 & PF=$!; sleep 2
  echo "== $p"; curl -s localhost:19092/metrics | grep -E '^kgateway_(resources_managed|envoy_xds_rejects_active)'
  kill $PF
done
# Freshness evidence (round 2, finding 3): kgateway must set observedGeneration on its conditions.
kubectl get gateway -n app-a gw -o jsonpath='{.metadata.generation} {.status.conditions[*].observedGeneration}{"\n"}'
```

Record whether both replicas report the same `kgateway_resources_managed` series (expected: yes; krt runs on every replica). Also record that `envoy_xds_rejects_active` is set by the replica serving the proxy's xDS stream, which is why the UI sums it. Record the observedGeneration output: every condition must carry the Gateway's generation. If any is 0 or missing, STOP and ask the user, because the UI treats a missing observedGeneration as stale (design §5.2).

- [ ] **Step 4: Induce an xDS reject and capture the metrics fixtures**

Task 8's contract test needs a real reject in each capture (round 2, finding 11). Under the default `validation.level: standard`, kgateway passes unknown TLS cipher suites through to the cluster config and Envoy NACKs it (upstream fixture `pkg/kgateway/translator/gateway/testutils/inputs/route-replacement/backendconfigpolicy/invalid-cipher-suites.yaml`, present on v2.3.11 and v2.4.7):

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: v1
kind: Service
metadata: {name: example-svc, namespace: app-a}
spec: {ports: [{port: 80, targetPort: 8080}], selector: {app: example}}
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata: {name: reject-me, namespace: app-a}
spec:
  parentRefs: [{name: gw}]
  rules: [{backendRefs: [{name: example-svc, port: 80}]}]
---
apiVersion: gateway.kgateway.dev/v1alpha1
kind: BackendConfigPolicy
metadata: {name: invalid-ciphers, namespace: app-a}
spec:
  targetRefs: [{group: "", kind: Service, name: example-svc}]
  tls:
    insecureSkipVerify: false
    parameters: {minVersion: "1.2", maxVersion: "1.3", cipherSuites: ["BOGUS_CIPHER_SUITE_1"], ecdhCurves: ["invalid-curve-p256"]}
EOF
# Save the /metrics of the replica that reports the reject (it serves the proxy's xDS stream).
mkdir -p pkg/ctrlmetrics/testdata
for i in $(seq 1 30); do
  for p in $(kubectl get pod -n kgw-a -l kgateway=kgateway -o name); do
    kubectl port-forward -n kgw-a $p 19092:9092 >/dev/null 2>&1 & PF=$!; sleep 2
    curl -s localhost:19092/metrics > /tmp/kgw-metrics.txt; kill $PF
    if grep -E '^kgateway_envoy_xds_rejects_active\{.*gateway_name="gw".*gateway_namespace="app-a".*\} 1$' /tmp/kgw-metrics.txt; then
      cp /tmp/kgw-metrics.txt pkg/ctrlmetrics/testdata/v2.4.7.txt; break 2
    fi
  done
  sleep 2
done
test -s pkg/ctrlmetrics/testdata/v2.4.7.txt || echo "NO REJECT CAPTURED"
```

If no reject appears within a minute, STOP and ask the user for another reject trigger; do not commit a capture without one. Record the reject line (labels and value) in D-012.

For v2.3.11, repeat Step 1 with only install A at `--version v2.3.11` (Gateway API v1.5.1) on a fresh cluster, run the observedGeneration check from Step 3, then this step, saving to `pkg/ctrlmetrics/testdata/v2.3.11.txt`.

- [ ] **Step 5: Record the results and clean up**

Append to `docs/decisions.md`:

```markdown
## D-012 — Multi-install and metric replica behaviour, observed (<experiment date, YYYY-MM-DD>, phase 1)
**Context:** Design §13 items 1–2; experiment on kind with kgateway v2.4.7, two installs with disjoint discoveryNamespaceSelectors.
**Decision:** <observed facts, verbatim command output excerpts>. Install assignment keeps design §4.3 (controllerName ∩ namespace scope; ambiguity finding when >1). Metrics: rejects summed over replicas; resources_managed max over replicas.
**Alternatives:** n/a (observation).
**Consequences:** <any change needed; if §4.3 must change, STOP and ask the user before Task 7>.
```

Then run `kind delete cluster --name kgwui-exp`.

- [ ] **Step 6: Commit**

```bash
git add docs && git commit -m "docs: record multi-install and metrics replica observations (D-012)"
```

Leave the two `testdata` captures uncommitted; Task 8 commits them with the code that reads them.

---

### Task 2: Go module, server skeleton, embedded SPA placeholder, LICENSE

**Files:**
- Create: `go.mod`, `Makefile`, `LICENSE` (Apache-2.0 text from https://www.apache.org/licenses/LICENSE-2.0.txt), `.gitignore`
- Create: `pkg/version/version.go`
- Create: `internal/webui/embed.go`, `internal/webui/dist/index.html`
- Test: `internal/webui/embed_test.go`

**Interfaces:**
- Produces: `version.Version string`; `webui.FS() fs.FS` (the root of `dist`).

- [ ] **Step 1: Initialise the module**

```bash
go mod init github.com/DuncanDoyle/kgateway-ui
go mod edit -go=1.26
printf '.superpowers/\nbin/\nweb/node_modules/\nweb/**/node_modules/\nweb/**/dist/\n!internal/webui/dist/index.html\ninternal/webui/dist/*\n' > .gitignore
```

- [ ] **Step 2: Write the failing test**

`internal/webui/embed_test.go`:

```go
package webui

import (
	"io/fs"
	"testing"
)

// The binary must always embed an index.html, even before the frontend is built,
// so `go build` works on a fresh checkout.
func TestFSServesIndex(t *testing.T) {
	b, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Fatalf("index.html not embedded: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("index.html is empty")
	}
}
```

- [ ] **Step 3: Run it and confirm it fails**

Run: `go test ./internal/webui/...`
Expected: FAIL (`undefined: FS`)

- [ ] **Step 4: Implement**

`internal/webui/dist/index.html`:

```html
<!doctype html><html><head><meta charset="utf-8"><title>kgateway UI</title></head>
<body><p>Frontend not built. Run <code>make web</code>.</p></body></html>
```

`internal/webui/embed.go`:

```go
// Package webui embeds the built single-page application into the binary.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the embedded SPA rooted at the dist directory.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // dist is embedded at compile time; this cannot fail at runtime
	}
	return sub
}
```

`pkg/version/version.go`:

```go
// Package version holds the build version of the binary.
package version

// Version is set at build time with -ldflags "-X github.com/DuncanDoyle/kgateway-ui/pkg/version.Version=<v>".
var Version = "dev"
```

`Makefile`:

```makefile
VERSION ?= dev
LDFLAGS := -s -w -X github.com/DuncanDoyle/kgateway-ui/pkg/version.Version=$(VERSION)

.PHONY: test lint build web run
test:
	go test ./...
lint:
	go vet ./...
	golangci-lint run ./...
web:
	cd web && npm ci && npm run build
	rm -rf internal/webui/dist && cp -r web/app/dist internal/webui/dist
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/kgateway-ui ./cmd/kgateway-ui
run:
	go run ./cmd/kgateway-ui --kubeconfig $${KUBECONFIG:-$$HOME/.kube/config}
```

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `go test ./internal/webui/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go.mod Makefile LICENSE .gitignore pkg/version internal/webui
git commit -m "chore: go module, embedded SPA placeholder, licence"
```

---

### Task 3: Kind descriptors and served-version resolution

**Files:**
- Create: `pkg/kinds/kinds.go`, `pkg/kinds/upstream.go`
- Test: `pkg/kinds/kinds_test.go`

**Interfaces:**
- Produces:
  - `kinds.Category` with the constants `CategoryGatewayClass`, `CategoryGateway`, `CategoryListenerSet`, `CategoryRoute`, `CategoryPolicy` and `CategoryWorkload` (Deployments, read through typed informers and never counted; used by `/api/v1/meta` in Task 10);
  - `kinds.Descriptor{Group, Kind, Resource string; Versions []string; Namespaced bool; Category Category; DisplayName string}`;
  - `kinds.Served{Descriptor; Version string}` with `GVR() schema.GroupVersionResource`;
  - `kinds.DiscoveryClient` (interface: `ServerResourcesForGroupVersion(string) (*metav1.APIResourceList, error)`);
  - `kinds.Resolve(dc DiscoveryClient, descs []Descriptor) ([]Served, error)`;
  - `kinds.Upstream() []Descriptor`;
  - `kinds.ByCategory(served []Served, c Category) []Served`.

- [ ] **Step 1: Write the failing test**

`pkg/kinds/kinds_test.go`:

```go
package kinds

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakediscovery "k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"
)

func discovery(lists ...*metav1.APIResourceList) *fakediscovery.FakeDiscovery {
	return &fakediscovery.FakeDiscovery{Fake: &clienttesting.Fake{Resources: lists}}
}

func list(gv string, resources ...string) *metav1.APIResourceList {
	l := &metav1.APIResourceList{GroupVersion: gv}
	for _, r := range resources {
		l.APIResources = append(l.APIResources, metav1.APIResource{Name: r})
	}
	return l
}

func TestResolvePicksFirstServedVersionAndSkipsMissing(t *testing.T) {
	descs := []Descriptor{
		{Group: "gateway.networking.k8s.io", Kind: "TLSRoute", Resource: "tlsroutes", Versions: []string{"v1", "v1alpha2"}, Namespaced: true, Category: CategoryRoute},
		{Group: "gateway.kgateway.dev", Kind: "HTTPListenerPolicy", Resource: "httplistenerpolicies", Versions: []string{"v1alpha1"}, Namespaced: true, Category: CategoryPolicy},
		{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Resource: "httproutes", Versions: []string{"v1"}, Namespaced: true, Category: CategoryRoute},
	}
	dc := discovery(
		list("gateway.networking.k8s.io/v1", "httproutes", "gateways"),
		list("gateway.networking.k8s.io/v1alpha2", "tlsroutes"),
		// gateway.kgateway.dev/v1alpha1 is served but without httplistenerpolicies (removed on main)
		list("gateway.kgateway.dev/v1alpha1", "trafficpolicies"),
	)
	got, err := Resolve(dc, descs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 served kinds, got %d: %+v", len(got), got)
	}
	if got[0].Kind != "TLSRoute" || got[0].Version != "v1alpha2" {
		t.Errorf("TLSRoute should fall back to v1alpha2, got %+v", got[0])
	}
	if got[1].Kind != "HTTPRoute" || got[1].GVR().Resource != "httproutes" {
		t.Errorf("unexpected %+v", got[1])
	}
}

func TestUpstreamDescriptorsAreComplete(t *testing.T) {
	want := map[string]Category{
		"GatewayClass": CategoryGatewayClass, "Gateway": CategoryGateway, "ListenerSet": CategoryListenerSet,
		"XListenerSet": CategoryListenerSet, "HTTPRoute": CategoryRoute, "GRPCRoute": CategoryRoute,
		"TCPRoute": CategoryRoute, "TLSRoute": CategoryRoute, "BackendTLSPolicy": CategoryPolicy,
		"TrafficPolicy": CategoryPolicy, "ListenerPolicy": CategoryPolicy, "HTTPListenerPolicy": CategoryPolicy,
		"BackendConfigPolicy": CategoryPolicy,
	}
	got := map[string]Category{}
	for _, d := range Upstream() {
		got[d.Kind] = d.Category
		if d.Resource == "secrets" {
			t.Fatal("Secrets must never be registered")
		}
	}
	for k, c := range want {
		if got[k] != c {
			t.Errorf("%s: want category %q, got %q", k, c, got[k])
		}
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go get k8s.io/client-go@v0.36.2 k8s.io/apimachinery@v0.36.2 && go test ./pkg/kinds/...`
Expected: FAIL (`undefined: Descriptor`)

- [ ] **Step 3: Implement**

`pkg/kinds/kinds.go`:

```go
// Package kinds describes the Kubernetes resource kinds the UI understands and
// resolves which of them the cluster actually serves.
package kinds

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Category groups kinds by the role they play in the UI.
type Category string

// Categories understood by the UI.
const (
	CategoryGatewayClass Category = "gatewayclass"
	CategoryGateway      Category = "gateway"
	CategoryListenerSet  Category = "listenerset"
	CategoryRoute        Category = "route"
	CategoryPolicy       Category = "policy"
	// CategoryWorkload marks kinds read through typed informers (Deployments).
	// They are never watched dynamically or counted; findings link to them.
	CategoryWorkload     Category = "workload"
)

// Descriptor describes a kind the UI can watch. Versions are in preference order.
type Descriptor struct {
	Group       string   `json:"group"`
	Kind        string   `json:"kind"`
	Resource    string   `json:"resource"`
	Versions    []string `json:"versions"`
	Namespaced  bool     `json:"namespaced"`
	Category    Category `json:"category"`
	DisplayName string   `json:"displayName"`
}

// Served is a Descriptor resolved to the version the cluster serves.
type Served struct {
	Descriptor
	Version string `json:"version"`
}

// GVR returns the group/version/resource used to watch this kind.
func (s Served) GVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: s.Group, Version: s.Version, Resource: s.Resource}
}

// DiscoveryClient is the subset of the discovery API that Resolve needs.
type DiscoveryClient interface {
	ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error)
}

// Resolve returns, in input order, the descriptors whose resource is served in
// one of their versions, resolved to the first such version. Kinds that are not
// served (CRD not installed, or removed in this kgateway version) are omitted.
func Resolve(dc DiscoveryClient, descs []Descriptor) ([]Served, error) {
	cache := map[string]*metav1.APIResourceList{}
	get := func(gv string) (*metav1.APIResourceList, error) {
		if l, ok := cache[gv]; ok {
			return l, nil
		}
		l, err := dc.ServerResourcesForGroupVersion(gv)
		if apierrors.IsNotFound(err) {
			l, err = &metav1.APIResourceList{}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("discover %s: %w", gv, err)
		}
		cache[gv] = l
		return l, nil
	}
	var out []Served
	for _, d := range descs {
		for _, v := range d.Versions {
			l, err := get(schema.GroupVersion{Group: d.Group, Version: v}.String())
			if err != nil {
				return nil, err
			}
			if has(l, d.Resource) {
				out = append(out, Served{Descriptor: d, Version: v})
				break
			}
		}
	}
	return out, nil
}

func has(l *metav1.APIResourceList, resource string) bool {
	for _, r := range l.APIResources {
		if r.Name == resource {
			return true
		}
	}
	return false
}

// ByCategory returns the served kinds in category c.
func ByCategory(served []Served, c Category) []Served {
	var out []Served
	for _, s := range served {
		if s.Category == c {
			out = append(out, s)
		}
	}
	return out
}
```

`pkg/kinds/upstream.go`:

```go
package kinds

const (
	gwGroup  = "gateway.networking.k8s.io"
	gwxGroup = "gateway.networking.x-k8s.io"
	kgwGroup = "gateway.kgateway.dev"
)

// Upstream returns the kinds of Gateway API and kgateway that the UI understands.
// Secrets and ConfigMaps are deliberately absent (design §11).
func Upstream() []Descriptor {
	return []Descriptor{
		{Group: gwGroup, Kind: "GatewayClass", Resource: "gatewayclasses", Versions: []string{"v1"}, Category: CategoryGatewayClass, DisplayName: "GatewayClasses"},
		{Group: gwGroup, Kind: "Gateway", Resource: "gateways", Versions: []string{"v1"}, Namespaced: true, Category: CategoryGateway, DisplayName: "Gateways"},
		{Group: gwGroup, Kind: "ListenerSet", Resource: "listenersets", Versions: []string{"v1"}, Namespaced: true, Category: CategoryListenerSet, DisplayName: "ListenerSets"},
		{Group: gwxGroup, Kind: "XListenerSet", Resource: "xlistenersets", Versions: []string{"v1alpha1"}, Namespaced: true, Category: CategoryListenerSet, DisplayName: "ListenerSets (experimental)"},
		{Group: gwGroup, Kind: "HTTPRoute", Resource: "httproutes", Versions: []string{"v1"}, Namespaced: true, Category: CategoryRoute, DisplayName: "HTTPRoutes"},
		{Group: gwGroup, Kind: "GRPCRoute", Resource: "grpcroutes", Versions: []string{"v1"}, Namespaced: true, Category: CategoryRoute, DisplayName: "GRPCRoutes"},
		{Group: gwGroup, Kind: "TCPRoute", Resource: "tcproutes", Versions: []string{"v1", "v1alpha2"}, Namespaced: true, Category: CategoryRoute, DisplayName: "TCPRoutes"},
		{Group: gwGroup, Kind: "TLSRoute", Resource: "tlsroutes", Versions: []string{"v1", "v1alpha3", "v1alpha2"}, Namespaced: true, Category: CategoryRoute, DisplayName: "TLSRoutes"},
		{Group: gwGroup, Kind: "BackendTLSPolicy", Resource: "backendtlspolicies", Versions: []string{"v1", "v1alpha3"}, Namespaced: true, Category: CategoryPolicy, DisplayName: "BackendTLSPolicies"},
		{Group: kgwGroup, Kind: "TrafficPolicy", Resource: "trafficpolicies", Versions: []string{"v1alpha1"}, Namespaced: true, Category: CategoryPolicy, DisplayName: "TrafficPolicies"},
		{Group: kgwGroup, Kind: "ListenerPolicy", Resource: "listenerpolicies", Versions: []string{"v1alpha1"}, Namespaced: true, Category: CategoryPolicy, DisplayName: "ListenerPolicies"},
		{Group: kgwGroup, Kind: "HTTPListenerPolicy", Resource: "httplistenerpolicies", Versions: []string{"v1alpha1"}, Namespaced: true, Category: CategoryPolicy, DisplayName: "HTTPListenerPolicies"},
		{Group: kgwGroup, Kind: "BackendConfigPolicy", Resource: "backendconfigpolicies", Versions: []string{"v1alpha1"}, Namespaced: true, Category: CategoryPolicy, DisplayName: "BackendConfigPolicies"},
	}
}

// KgatewayGroup is the API group of kgateway's own CRDs.
const KgatewayGroup = kgwGroup
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./pkg/kinds/...`
Expected: PASS. If the fake discovery client returns a non-NotFound error for missing group versions, the test fails on the kgateway group; in that case, wrap it with an `apierrors.IsNotFound(err) || strings.Contains(err.Error(), "not found")` check and add a comment explaining that the fake client's error type differs from the real one.

- [ ] **Step 5: Commit**

```bash
git add pkg/kinds go.mod go.sum && git commit -m "feat(kinds): kind descriptors and served-version resolution"
```

---

### Task 4: Typed views over Gateway API objects

**Files:**
- Create: `pkg/objects/objects.go`
- Test: `pkg/objects/objects_test.go`

**Interfaces:**
- Produces:
  - `objects.Ref{Group, Kind, Namespace, Name string}` (JSON `group,kind,namespace,name`) with `String()` returning `"ns/name"` (or `"name"` for cluster-scoped objects);
  - `objects.RefOf(u *unstructured.Unstructured) Ref`;
  - `objects.ParentRef{Group, Kind, Namespace, Name, SectionName string; Port int32}` (`Port` 0 means unset; Gateway API ports start at 1). Parent-status matching compares the whole struct, so a different port or section is a different parent (round 2, finding 5). `Ref() Ref` returns the referenced object's full group/kind/namespace/name identity, with section and port dropped (used for assignment, finding 6);
  - `objects.RouteParentRefs(u) []ParentRef` (defaults applied: group `gateway.networking.k8s.io`, kind `Gateway`, namespace = route namespace);
  - `objects.RouteParentStatus{ParentRef ParentRef; ControllerName string; Conditions []metav1.Condition}`;
  - `objects.RouteParents(u) []RouteParentStatus`;
  - `objects.AncestorStatus{AncestorRef ParentRef; ControllerName string; Conditions []metav1.Condition}`;
  - `objects.PolicyAncestors(u) []AncestorStatus`;
  - `objects.Conditions(u) []metav1.Condition` (`status.conditions`);
  - `objects.ListenerStatus{Name string; AttachedRoutes int32; Conditions []metav1.Condition}`;
  - `objects.GatewayListeners(u) []ListenerStatus`;
  - `objects.GatewayClassName(u) string`;
  - `objects.ControllerName(gatewayClass) string`;
  - `objects.ListenerSetParent(u) ParentRef`;
  - `objects.GatewayAddresses(u) []string`;
  - `objects.GatewaySpecListeners(u) []string` (the names in `spec.listeners`, so a configured listener without a status block is detected; round 2, finding 4);
  - `objects.FindCondition(cs []metav1.Condition, typ string) *metav1.Condition`;
  - `objects.IsStale(u, c metav1.Condition) bool` (`observedGeneration` lower than `metadata.generation`; a missing `observedGeneration` (0) on an object with a generation counts as stale, round 2 finding 3).

- [ ] **Step 1: Write the failing test**

`pkg/objects/objects_test.go`:

```go
package objects

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func route() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "HTTPRoute",
		"metadata": map[string]any{"name": "cart", "namespace": "shop", "generation": int64(3)},
		"spec": map[string]any{"parentRefs": []any{
			map[string]any{"name": "edge", "namespace": "infra"},
			map[string]any{"name": "ls1", "kind": "ListenerSet", "sectionName": "https"},
		}},
		"status": map[string]any{"parents": []any{map[string]any{
			"parentRef":      map[string]any{"name": "edge", "namespace": "infra"},
			"controllerName": "kgateway.dev/kgateway",
			"conditions": []any{map[string]any{"type": "Accepted", "status": "True", "reason": "Accepted",
				"observedGeneration": int64(2), "lastTransitionTime": "2026-10-17T09:00:00Z", "message": ""}},
		}}},
	}}
}

func TestRouteParentRefsAppliesDefaults(t *testing.T) {
	got := RouteParentRefs(route())
	want := []ParentRef{
		{Group: "gateway.networking.k8s.io", Kind: "Gateway", Namespace: "infra", Name: "edge"},
		{Group: "gateway.networking.k8s.io", Kind: "ListenerSet", Namespace: "shop", Name: "ls1", SectionName: "https"},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRouteParentsAndStaleness(t *testing.T) {
	r := route()
	ps := RouteParents(r)
	if len(ps) != 1 || ps[0].ControllerName != "kgateway.dev/kgateway" || ps[0].ParentRef.Kind != "Gateway" {
		t.Fatalf("unexpected %+v", ps)
	}
	c := FindCondition(ps[0].Conditions, "Accepted")
	if c == nil || !IsStale(r, *c) {
		t.Fatal("observedGeneration 2 < generation 3 must be stale")
	}
}

// Round 2, finding 3: a condition without observedGeneration gives no evidence
// that it describes the current spec, so it is stale.
func TestMissingObservedGenerationIsStale(t *testing.T) {
	r := route()
	c := metav1.Condition{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 0}
	if !IsStale(r, c) {
		t.Fatal("observedGeneration 0 on an object at generation 3 must be stale")
	}
	c.ObservedGeneration = 3
	if IsStale(r, c) {
		t.Fatal("observedGeneration equal to generation is current")
	}
}

// Round 2, finding 5: the port is part of the parent identity for status
// matching, and unset differs from set; Ref() drops it.
func TestParentRefKeepsPort(t *testing.T) {
	u := route()
	u.Object["spec"] = map[string]any{"parentRefs": []any{
		map[string]any{"name": "edge", "namespace": "infra", "port": int64(443)},
		map[string]any{"name": "edge", "namespace": "infra"},
	}}
	got := RouteParentRefs(u)
	if got[0].Port != 443 || got[1].Port != 0 || got[0] == got[1] {
		t.Fatalf("port must be kept and distinguish parents: %+v", got)
	}
	if got[0].Ref() != got[1].Ref() {
		t.Fatal("Ref() identifies the Gateway, without port")
	}
}

func TestGatewaySpecListeners(t *testing.T) {
	gw := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "Gateway",
		"metadata":   map[string]any{"name": "edge", "namespace": "infra"},
		"spec":       map[string]any{"listeners": []any{map[string]any{"name": "http"}, map[string]any{"name": "https"}}},
	}}
	if got := GatewaySpecListeners(gw); len(got) != 2 || got[0] != "http" || got[1] != "https" {
		t.Fatalf("spec listeners: %v", got)
	}
}

func TestRefOf(t *testing.T) {
	got := RefOf(route())
	if got != (Ref{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Namespace: "shop", Name: "cart"}) || got.String() != "shop/cart" {
		t.Fatalf("got %+v %q", got, got.String())
	}
}

// Assignment resolves parents by their full identity (finding 6): the group
// is part of the key, and the section name is not.
func TestParentRefIdentityKeepsGroup(t *testing.T) {
	got := RouteParentRefs(route())[1].Ref()
	if got != (Ref{Group: "gateway.networking.k8s.io", Kind: "ListenerSet", Namespace: "shop", Name: "ls1"}) {
		t.Fatalf("got %+v", got)
	}
	foreign := ParentRef{Group: "example.com", Kind: "Gateway", Namespace: "infra", Name: "edge"}
	if foreign.Ref() == RouteParentRefs(route())[0].Ref() {
		t.Fatal("a foreign-group Gateway must not share an identity with a Gateway API Gateway")
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./pkg/objects/...`
Expected: FAIL (`undefined: RouteParentRefs`)

- [ ] **Step 3: Implement**

`pkg/objects/objects.go`:

```go
// Package objects provides typed, read-only views over unstructured Gateway API
// and kgateway objects. It decodes only the fields the UI needs and applies the
// Gateway API defaults, so callers never handle raw maps.
package objects

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GatewayAPIGroup is the API group of the Gateway API.
const GatewayAPIGroup = "gateway.networking.k8s.io"

// Ref identifies an object.
type Ref struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// String returns "namespace/name", or "name" for cluster-scoped objects.
func (r Ref) String() string {
	if r.Namespace == "" {
		return r.Name
	}
	return r.Namespace + "/" + r.Name
}

// RefOf returns the Ref of u.
func RefOf(u *unstructured.Unstructured) Ref {
	gv, _ := schema.ParseGroupVersion(u.GetAPIVersion())
	return Ref{Group: gv.Group, Kind: u.GetKind(), Namespace: u.GetNamespace(), Name: u.GetName()}
}

// ParentRef is a Gateway API parent or ancestor reference with defaults applied.
type ParentRef struct {
	Group       string `json:"group"`
	Kind        string `json:"kind"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	SectionName string `json:"sectionName,omitempty"`
	// Port is the parentRef port; 0 means unset (Gateway API ports start at 1).
	Port int32 `json:"port,omitempty"`
}

// Ref returns the identity of the referenced object. Section name and port
// are dropped: they select a listener, not a different object.
func (p ParentRef) Ref() Ref {
	return Ref{Group: p.Group, Kind: p.Kind, Namespace: p.Namespace, Name: p.Name}
}

type rawRef struct {
	Group       *string `json:"group"`
	Kind        *string `json:"kind"`
	Namespace   *string `json:"namespace"`
	Name        string  `json:"name"`
	SectionName *string `json:"sectionName"`
	Port        *int32  `json:"port"`
}

// resolve applies the Gateway API defaults: group gateway.networking.k8s.io,
// kind Gateway, and the referencing object's namespace.
func (r rawRef) resolve(defaultNS string) ParentRef {
	p := ParentRef{Group: GatewayAPIGroup, Kind: "Gateway", Namespace: defaultNS, Name: r.Name}
	if r.Group != nil {
		p.Group = *r.Group
	}
	if r.Kind != nil {
		p.Kind = *r.Kind
	}
	if r.Namespace != nil && *r.Namespace != "" {
		p.Namespace = *r.Namespace
	}
	if r.SectionName != nil {
		p.SectionName = *r.SectionName
	}
	if r.Port != nil {
		p.Port = *r.Port
	}
	return p
}

type routeView struct {
	Spec struct {
		ParentRefs []rawRef `json:"parentRefs"`
		Listeners  []struct {
			Name string `json:"name"`
		} `json:"listeners"`
	} `json:"spec"`
	Status struct {
		Parents []struct {
			ParentRef      rawRef             `json:"parentRef"`
			ControllerName string             `json:"controllerName"`
			Conditions     []metav1.Condition `json:"conditions"`
		} `json:"parents"`
		Ancestors []struct {
			AncestorRef    rawRef             `json:"ancestorRef"`
			ControllerName string             `json:"controllerName"`
			Conditions     []metav1.Condition `json:"conditions"`
		} `json:"ancestors"`
		Conditions []metav1.Condition `json:"conditions"`
		Listeners  []struct {
			Name           string             `json:"name"`
			AttachedRoutes int32              `json:"attachedRoutes"`
			Conditions     []metav1.Condition `json:"conditions"`
		} `json:"listeners"`
		Addresses []struct {
			Value string `json:"value"`
		} `json:"addresses"`
	} `json:"status"`
}

// view decodes the subset of fields used by this package. Unknown fields are
// ignored; a malformed object yields an empty view rather than an error,
// because the UI must keep rendering the rest of the cluster.
func view(u *unstructured.Unstructured) routeView {
	var v routeView
	_ = runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &v)
	return v
}

// RouteParentRefs returns spec.parentRefs with defaults applied.
func RouteParentRefs(u *unstructured.Unstructured) []ParentRef {
	v := view(u)
	out := make([]ParentRef, 0, len(v.Spec.ParentRefs))
	for _, r := range v.Spec.ParentRefs {
		out = append(out, r.resolve(u.GetNamespace()))
	}
	return out
}

// RouteParentStatus is one entry of a route's status.parents.
type RouteParentStatus struct {
	ParentRef      ParentRef
	ControllerName string
	Conditions     []metav1.Condition
}

// RouteParents returns status.parents with defaults applied to each parentRef.
func RouteParents(u *unstructured.Unstructured) []RouteParentStatus {
	v := view(u)
	out := make([]RouteParentStatus, 0, len(v.Status.Parents))
	for _, p := range v.Status.Parents {
		out = append(out, RouteParentStatus{ParentRef: p.ParentRef.resolve(u.GetNamespace()), ControllerName: p.ControllerName, Conditions: p.Conditions})
	}
	return out
}

// AncestorStatus is one entry of a policy's status.ancestors.
type AncestorStatus struct {
	AncestorRef    ParentRef
	ControllerName string
	Conditions     []metav1.Condition
}

// PolicyAncestors returns status.ancestors with defaults applied.
func PolicyAncestors(u *unstructured.Unstructured) []AncestorStatus {
	v := view(u)
	out := make([]AncestorStatus, 0, len(v.Status.Ancestors))
	for _, a := range v.Status.Ancestors {
		out = append(out, AncestorStatus{AncestorRef: a.AncestorRef.resolve(u.GetNamespace()), ControllerName: a.ControllerName, Conditions: a.Conditions})
	}
	return out
}

// Conditions returns status.conditions.
func Conditions(u *unstructured.Unstructured) []metav1.Condition { return view(u).Status.Conditions }

// ListenerStatus is one entry of a Gateway's status.listeners.
type ListenerStatus struct {
	Name           string
	AttachedRoutes int32
	Conditions     []metav1.Condition
}

// GatewayListeners returns status.listeners.
func GatewayListeners(u *unstructured.Unstructured) []ListenerStatus {
	v := view(u)
	out := make([]ListenerStatus, 0, len(v.Status.Listeners))
	for _, l := range v.Status.Listeners {
		out = append(out, ListenerStatus{Name: l.Name, AttachedRoutes: l.AttachedRoutes, Conditions: l.Conditions})
	}
	return out
}

// GatewayAddresses returns the values of status.addresses.
func GatewayAddresses(u *unstructured.Unstructured) []string {
	var out []string
	for _, a := range view(u).Status.Addresses {
		out = append(out, a.Value)
	}
	return out
}

// GatewaySpecListeners returns the names in a Gateway's spec.listeners.
func GatewaySpecListeners(u *unstructured.Unstructured) []string {
	var out []string
	for _, l := range view(u).Spec.Listeners {
		out = append(out, l.Name)
	}
	return out
}

// GatewayClassName returns spec.gatewayClassName of a Gateway.
func GatewayClassName(u *unstructured.Unstructured) string {
	s, _, _ := unstructured.NestedString(u.Object, "spec", "gatewayClassName")
	return s
}

// ControllerName returns spec.controllerName of a GatewayClass.
func ControllerName(gatewayClass *unstructured.Unstructured) string {
	s, _, _ := unstructured.NestedString(gatewayClass.Object, "spec", "controllerName")
	return s
}

// ListenerSetParent returns spec.parentRef of a ListenerSet with defaults applied.
func ListenerSetParent(u *unstructured.Unstructured) ParentRef {
	m, _, _ := unstructured.NestedMap(u.Object, "spec", "parentRef")
	var r rawRef
	_ = runtime.DefaultUnstructuredConverter.FromUnstructured(m, &r)
	return r.resolve(u.GetNamespace())
}

// FindCondition returns the condition of type typ, or nil.
func FindCondition(cs []metav1.Condition, typ string) *metav1.Condition {
	for i := range cs {
		if cs[i].Type == typ {
			return &cs[i]
		}
	}
	return nil
}

// IsStale reports whether c may describe an older generation of u than the
// current spec. A condition without observedGeneration (0) gives no evidence
// that it is current, so it counts as stale (round 2, finding 3); kgateway
// sets observedGeneration on its conditions (Task 1 confirms it on 2.3/2.4).
func IsStale(u *unstructured.Unstructured, c metav1.Condition) bool {
	return c.ObservedGeneration < u.GetGeneration()
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./pkg/objects/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/objects go.mod go.sum && git commit -m "feat(objects): typed views over Gateway API objects"
```

---

### Task 5: Shared informer cache, test fixtures and fake clients

**Files:**
- Create: `pkg/cache/cache.go`
- Create: `internal/testutil/fixtures.go`, `internal/testutil/clients.go`
- Test: `pkg/cache/cache_test.go`

**Interfaces:**
- Consumes: `kinds.Served`, `kinds.Upstream()`.
- Produces:
  - `cache.Access{OK bool; Err string}` (JSON `ok,error`). Every informer is built over an instrumented `ListerWatcher` that records the outcome of each List and Watch call its reflector makes, including the connection-refused and 429 watch retries client-go never reports to the watch error handler. `OK` means synced and no failed call since the last successful Watch call; a kind is unavailable from its last failed call until its next successful Watch call (round 3, finding 1). `HasSynced()` alone is not enough, because it stays true after later failures (finding 2). There is no extra API traffic;
  - `cache.ListTimeout = time.Minute` (every informer List call is bounded; watches use client-go's own 5–10 minute watch timeout, and a silently stalled watch stream is a documented limit);
  - `cache.NamespacesGVR`, `cache.PodsGVR`, `cache.DeploymentsGVR` (`schema.GroupVersionResource` keys under which the typed informers report their `Access`);
  - `cache.Snapshot{Objects map[schema.GroupVersionResource][]*unstructured.Unstructured; Access map[schema.GroupVersionResource]Access; Namespaces []*corev1.Namespace; Pods []*corev1.Pod; Deployments []*appsv1.Deployment; Synced bool; TakenAt time.Time}`. `Access` holds every served kind plus the three typed GVRs;
  - `cache.Config{Dynamic dynamic.Interface; Kube kubernetes.Interface; Served []kinds.Served; PodSelectors, DeploymentSelectors []labels.Selector}`;
  - `cache.New(Config) *Cache`, `(*Cache).Start(ctx)`, `(*Cache).Snapshot() *Snapshot`;
  - `cache.Snapshotter` (interface `Snapshot() *Snapshot`).
- Produces (tests only):
  - `testutil.Clients(t, objs ...runtime.Object) (dynamic.Interface, *kubefake.Clientset)`. Kube objects go to the typed fake; `*unstructured.Unstructured` goes to the dynamic fake; fake discovery serves every `kinds.Upstream()` kind at its first version.
  - `testutil.ServedUpstream() []kinds.Served`.
  - The fixture builders listed in Step 1, including `testutil.Policy(apiVersion, kind, ns, name string, ancestors ...PolicyAncestor)`, `testutil.BackendTLSPolicy(ns, name string, ancestors ...PolicyAncestor)` and `testutil.AddSpecListener(gw *unstructured.Unstructured, name string)` (a configured listener with no status block). Parent refs keep `Port`. `ControllerDeployment`'s Pod template carries the upstream v2.3/v2.4 Pod labels exactly (`kgateway`, `app.kubernetes.io/name`, `app.kubernetes.io/instance`, `app.kubernetes.io/component=controller`), so fixture Pods enter through the production watch (finding 14).

- [ ] **Step 1: Write the test fixtures** (shared by every later backend test)

`internal/testutil/fixtures.go`:

```go
// Package testutil builds fixture objects and fake clients for backend tests.
package testutil

import (
	"maps"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Now is the fixed clock used by fixtures and by tests that compare times.
var Now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

// Old is a creation time well past every grace period.
var Old = metav1.NewTime(Now.Add(-time.Hour))

// Controller is the upstream kgateway controller name.
const Controller = "kgateway.dev/kgateway"

// Cond builds a condition observed at generation 1.
func Cond(typ string, status metav1.ConditionStatus, reason string) metav1.Condition {
	return metav1.Condition{Type: typ, Status: status, Reason: reason, ObservedGeneration: 1, LastTransitionTime: Old}
}

func conds(cs []metav1.Condition) []any {
	out := make([]any, 0, len(cs))
	for _, c := range cs {
		out = append(out, map[string]any{"type": c.Type, "status": string(c.Status), "reason": c.Reason,
			"message": c.Message, "observedGeneration": c.ObservedGeneration, "lastTransitionTime": c.LastTransitionTime.UTC().Format(time.RFC3339)})
	}
	return out
}

func obj(apiVersion, kind, ns, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{"apiVersion": apiVersion, "kind": kind}}
	u.SetNamespace(ns)
	u.SetName(name)
	u.SetGeneration(1)
	u.SetUID(types.UID(kind + "/" + ns + "/" + name))
	u.SetCreationTimestamp(Old)
	return u
}

// Namespace builds a namespace with labels.
func Namespace(name string, lbls map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: lbls, UID: types.UID("ns/" + name)}}
}

// GatewayClass builds a GatewayClass with an Accepted condition.
func GatewayClass(name, controller string, accepted bool) *unstructured.Unstructured {
	u := obj("gateway.networking.k8s.io/v1", "GatewayClass", "", name)
	st := metav1.ConditionTrue
	if !accepted {
		st = metav1.ConditionFalse
	}
	u.Object["spec"] = map[string]any{"controllerName": controller}
	u.Object["status"] = map[string]any{"conditions": conds([]metav1.Condition{Cond("Accepted", st, "Accepted")})}
	return u
}

// Listener is a listener status entry for Gateway fixtures.
type Listener struct {
	Name     string
	Attached int32
	Conds    []metav1.Condition
}

// Gateway builds a Gateway. With no conditions given it is Accepted and Programmed.
func Gateway(ns, name, class string, cs []metav1.Condition, listeners ...Listener) *unstructured.Unstructured {
	u := obj("gateway.networking.k8s.io/v1", "Gateway", ns, name)
	if cs == nil {
		cs = []metav1.Condition{Cond("Accepted", metav1.ConditionTrue, "Accepted"), Cond("Programmed", metav1.ConditionTrue, "Programmed")}
	}
	if len(listeners) == 0 {
		listeners = []Listener{{Name: "http", Conds: []metav1.Condition{Cond("Accepted", metav1.ConditionTrue, "Accepted"), Cond("Programmed", metav1.ConditionTrue, "Programmed")}}}
	}
	ls := []any{}
	specLs := []any{}
	for _, l := range listeners {
		ls = append(ls, map[string]any{"name": l.Name, "attachedRoutes": int64(l.Attached), "conditions": conds(l.Conds), "supportedKinds": []any{}})
		specLs = append(specLs, map[string]any{"name": l.Name, "port": int64(8080), "protocol": "HTTP"})
	}
	u.Object["spec"] = map[string]any{"gatewayClassName": class, "listeners": specLs}
	u.Object["status"] = map[string]any{"conditions": conds(cs), "listeners": ls, "addresses": []any{map[string]any{"type": "IPAddress", "value": "10.0.0.1"}}}
	return u
}

// AddSpecListener adds a listener to gw's spec without a status block, as when
// the controller has not processed it yet.
func AddSpecListener(gw *unstructured.Unstructured, name string) {
	spec := gw.Object["spec"].(map[string]any)
	spec["listeners"] = append(spec["listeners"].([]any), map[string]any{"name": name, "port": int64(8443), "protocol": "HTTP"})
}

// RouteParent is a status.parents entry for route fixtures.
type RouteParent struct {
	Ref        objects.ParentRef
	Controller string
	Conds      []metav1.Condition
}

func refMap(p objects.ParentRef) map[string]any {
	m := map[string]any{"name": p.Name}
	if p.Group != "" {
		m["group"] = p.Group
	}
	if p.Kind != "" {
		m["kind"] = p.Kind
	}
	if p.Namespace != "" {
		m["namespace"] = p.Namespace
	}
	if p.SectionName != "" {
		m["sectionName"] = p.SectionName
	}
	if p.Port != 0 {
		m["port"] = int64(p.Port)
	}
	return m
}

// GwRef is a parentRef to a Gateway.
func GwRef(ns, name string) objects.ParentRef {
	return objects.ParentRef{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: ns, Name: name}
}

// HTTPRoute builds an HTTPRoute with the given parentRefs and status.parents.
func HTTPRoute(ns, name string, parents []objects.ParentRef, status ...RouteParent) *unstructured.Unstructured {
	u := obj("gateway.networking.k8s.io/v1", "HTTPRoute", ns, name)
	prs := []any{}
	for _, p := range parents {
		prs = append(prs, refMap(p))
	}
	st := []any{}
	for _, s := range status {
		st = append(st, map[string]any{"parentRef": refMap(s.Ref), "controllerName": s.Controller, "conditions": conds(s.Conds)})
	}
	u.Object["spec"] = map[string]any{"parentRefs": prs}
	u.Object["status"] = map[string]any{"parents": st}
	return u
}

// AcceptedBy is the status.parents entry of a healthy route attached to p by controller.
func AcceptedBy(p objects.ParentRef, controller string) RouteParent {
	return RouteParent{Ref: p, Controller: controller, Conds: []metav1.Condition{
		Cond("Accepted", metav1.ConditionTrue, "Accepted"), Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs"),
		Cond("kgateway.dev/Programmed", metav1.ConditionTrue, "Programmed")}}
}

// PolicyAncestor is a status.ancestors entry for policy fixtures.
type PolicyAncestor struct {
	Ref        objects.ParentRef
	Controller string
	Conds      []metav1.Condition
}

// Policy builds a policy of any kind with status.ancestors.
func Policy(apiVersion, kind, ns, name string, ancestors ...PolicyAncestor) *unstructured.Unstructured {
	u := obj(apiVersion, kind, ns, name)
	as := []any{}
	for _, a := range ancestors {
		as = append(as, map[string]any{"ancestorRef": refMap(a.Ref), "controllerName": a.Controller, "conditions": conds(a.Conds)})
	}
	u.Object["spec"] = map[string]any{}
	u.Object["status"] = map[string]any{"ancestors": as}
	return u
}

// TrafficPolicy builds a kgateway TrafficPolicy with status.ancestors.
func TrafficPolicy(ns, name string, ancestors ...PolicyAncestor) *unstructured.Unstructured {
	return Policy("gateway.kgateway.dev/v1alpha1", "TrafficPolicy", ns, name, ancestors...)
}

// BackendTLSPolicy builds a Gateway API BackendTLSPolicy with status.ancestors.
func BackendTLSPolicy(ns, name string, ancestors ...PolicyAncestor) *unstructured.Unstructured {
	return Policy("gateway.networking.k8s.io/v1", "BackendTLSPolicy", ns, name, ancestors...)
}

// ControllerDeployment builds a kgateway controller Deployment with the upstream
// labels. The Pod template carries the upstream v2.3/v2.4 Pod labels exactly:
// the selector labels plus app.kubernetes.io/component=controller.
func ControllerDeployment(ns, name, image string, desired, ready int32, env ...corev1.EnvVar) *appsv1.Deployment {
	lbls := map[string]string{"kgateway": "kgateway", "app.kubernetes.io/component": "controller",
		"app.kubernetes.io/name": "kgateway", "app.kubernetes.io/instance": name, "app.kubernetes.io/version": "2.4.7"}
	sel := map[string]string{"kgateway": "kgateway", "app.kubernetes.io/name": "kgateway", "app.kubernetes.io/instance": name}
	podLbls := map[string]string{"kgateway": "kgateway", "app.kubernetes.io/name": "kgateway", "app.kubernetes.io/instance": name,
		"app.kubernetes.io/component": "controller"}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: lbls, UID: types.UID("deploy/" + ns + "/" + name), CreationTimestamp: Old},
		Spec: appsv1.DeploymentSpec{
			Replicas: &desired,
			Selector: &metav1.LabelSelector{MatchLabels: sel},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: podLbls}, Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: "kgateway", Image: image, Env: env,
				Ports: []corev1.ContainerPort{{Name: "metrics", ContainerPort: 9092}},
			}}}},
		},
		Status: appsv1.DeploymentStatus{Replicas: desired, ReadyReplicas: ready},
	}
}

// ControllerPod builds a Pod of d. ready controls its Ready condition and phase.
func ControllerPod(d *appsv1.Deployment, name string, ready bool) *corev1.Pod {
	phase, st := corev1.PodRunning, corev1.ConditionTrue
	if !ready {
		phase, st = corev1.PodPending, corev1.ConditionFalse
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: d.Namespace, Name: name, Labels: maps.Clone(d.Spec.Template.Labels), UID: types.UID("pod/" + d.Namespace + "/" + name)},
		Spec:       d.Spec.Template.Spec,
		Status:     corev1.PodStatus{Phase: phase, PodIP: "10.1.0.5", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: st}}},
	}
}

// ProxyDeployment builds the proxy Deployment kgateway creates for a Gateway.
func ProxyDeployment(gwNS, gwName string, desired, ready int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: gwNS, Name: gwName, UID: types.UID("proxy/" + gwNS + "/" + gwName),
			Labels: map[string]string{"app.kubernetes.io/managed-by": "kgateway", "app.kubernetes.io/component": "proxy",
				"gateway.networking.k8s.io/gateway-name": gwName},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "gateway.networking.k8s.io/v1", Kind: "Gateway", Name: gwName, UID: types.UID("Gateway/" + gwNS + "/" + gwName)}}},
		Spec:   appsv1.DeploymentSpec{Replicas: &desired},
		Status: appsv1.DeploymentStatus{Replicas: desired, ReadyReplicas: ready},
	}
}

// Env is a shorthand for a literal env var.
func Env(name, value string) corev1.EnvVar { return corev1.EnvVar{Name: name, Value: value} }
```

`internal/testutil/clients.go`:

```go
package testutil

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	fakediscovery "k8s.io/client-go/discovery/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
)

// ServedUpstream resolves every upstream descriptor to its first version.
func ServedUpstream() []kinds.Served {
	var out []kinds.Served
	for _, d := range kinds.Upstream() {
		out = append(out, kinds.Served{Descriptor: d, Version: d.Versions[0]})
	}
	return out
}

// Clients returns fake dynamic and typed clients seeded with objs. Unstructured
// objects go to the dynamic client; everything else goes to the typed client.
// Fake discovery serves every upstream kind at its first version.
func Clients(t *testing.T, objs ...runtime.Object) (dynamic.Interface, *kubefake.Clientset) {
	t.Helper()
	listKinds := map[schema.GroupVersionResource]string{}
	byGV := map[string]*metav1.APIResourceList{}
	for _, s := range ServedUpstream() {
		listKinds[s.GVR()] = s.Kind + "List"
		gv := schema.GroupVersion{Group: s.Group, Version: s.Version}.String()
		if byGV[gv] == nil {
			byGV[gv] = &metav1.APIResourceList{GroupVersion: gv}
		}
		byGV[gv].APIResources = append(byGV[gv].APIResources, metav1.APIResource{Name: s.Resource, Kind: s.Kind, Namespaced: s.Namespaced})
	}
	var dyn, typed []runtime.Object
	for _, o := range objs {
		if _, ok := o.(*unstructured.Unstructured); ok {
			dyn = append(dyn, o)
		} else {
			typed = append(typed, o)
		}
	}
	kube := kubefake.NewSimpleClientset(typed...)
	fd := kube.Discovery().(*fakediscovery.FakeDiscovery)
	for _, l := range byGV {
		fd.Resources = append(fd.Resources, l)
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, dyn...), kube
}
```

- [ ] **Step 2: Write the failing cache tests**

`pkg/cache/cache_test.go`:

```go
package cache

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
)

// The production selectors (Task 6): Deployments by kgateway + component,
// Pods by kgateway only (A1).
var (
	deploySel = labels.SelectorFromSet(labels.Set{"kgateway": "kgateway", "app.kubernetes.io/component": "controller"})
	podSel    = labels.SelectorFromSet(labels.Set{"kgateway": "kgateway"})
)

func waitSynced(t *testing.T, c *Cache) *Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := c.Snapshot(); s.Synced {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("cache did not sync")
	return nil
}

func TestSnapshotContainsWatchedObjectsOnly(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	other := testutil.ControllerDeployment("apps", "web", "nginx", 1, 1)
	other.Labels = map[string]string{"app": "web"} // must not be cached: label-selected watches only
	dyn, kube := testutil.Clients(t,
		testutil.Namespace("shop", nil),
		d, testutil.ControllerPod(d, "kgateway-1", true), other,
		testutil.Gateway("shop", "edge", "kgateway", nil),
	)
	c := New(Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream(),
		PodSelectors: []labels.Selector{podSel}, DeploymentSelectors: []labels.Selector{deploySel}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	s := waitSynced(t, c)

	gw := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
	if len(s.Objects[gw]) != 1 || !s.Access[gw].OK {
		t.Fatalf("gateways: %d objects, access %+v", len(s.Objects[gw]), s.Access[gw])
	}
	if len(s.Deployments) != 1 || s.Deployments[0].Name != "kgateway" {
		t.Fatalf("only the label-selected controller Deployment may be cached, got %d", len(s.Deployments))
	}
	if len(s.Pods) != 1 || len(s.Namespaces) != 1 {
		t.Fatalf("pods=%d namespaces=%d", len(s.Pods), len(s.Namespaces))
	}
	for _, gvr := range []schema.GroupVersionResource{NamespacesGVR, PodsGVR, DeploymentsGVR} {
		if !s.Access[gvr].OK {
			t.Errorf("%s must be reported in Access: %+v", gvr.Resource, s.Access[gvr])
		}
	}
}

// waitFor polls cond until it holds or 15 s pass (reflector backoff starts at 0.8 s).
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Round 3, finding 1: the informers' own List and Watch calls decide access.
// The watch fails after the sync while list stays allowed; the kind is
// unavailable from that failed call until a Watch call succeeds again. The
// failures enter through the fake clients, as production sees them.
func TestWatchFailureAfterSyncLastsUntilAWatchSucceeds(t *testing.T) {
	hr := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	refused := fmt.Errorf("dial tcp 10.96.0.1:443: connect: %w", syscall.ECONNREFUSED)
	cases := []struct {
		name  string
		gvr   schema.GroupVersionResource
		typed bool // inject into the typed clientset instead of the dynamic client
		err   error
		want  string // substring of the recorded error
	}{
		{"httproutes connection refused (retried silently by client-go)", hr, false, refused, "connection refused"},
		{"httproutes throttled (retried silently by client-go)", hr, false, apierrors.NewTooManyRequests("slow down", 1), "slow down"},
		{"httproutes forbidden", hr, false, apierrors.NewForbidden(schema.GroupResource{Group: hr.Group, Resource: hr.Resource}, "", nil), "forbidden"},
		{"namespaces throttled", NamespacesGVR, true, apierrors.NewTooManyRequests("slow down", 1), "slow down"},
		{"pods connection refused", PodsGVR, true, refused, "connection refused"},
		{"deployments forbidden", DeploymentsGVR, true, apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "", nil), "forbidden"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dyn, kube := testutil.Clients(t, testutil.Namespace("shop", nil))
			var fail atomic.Bool
			var failed atomic.Int32
			watchers := make(chan *watch.FakeWatcher, 64)
			react := func(clienttesting.Action) (bool, watch.Interface, error) {
				if fail.Load() {
					failed.Add(1)
					return true, nil, tc.err
				}
				w := watch.NewFake()
				watchers <- w
				return true, w, nil
			}
			if tc.typed {
				kube.PrependWatchReactor(tc.gvr.Resource, react)
			} else {
				dyn.(*dynamicfake.FakeDynamicClient).PrependWatchReactor(tc.gvr.Resource, react)
			}
			c := New(Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream(),
				PodSelectors: []labels.Selector{podSel}, DeploymentSelectors: []labels.Selector{deploySel}})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.Start(ctx)
			waitSynced(t, c)

			fail.Store(true)
			(<-watchers).Stop() // the established watch ends; the reflector's next Watch call fails
			waitFor(t, "the failed watch to be recorded", func() bool { return !c.Snapshot().Access[tc.gvr].OK })
			if a := c.Snapshot().Access[tc.gvr]; !strings.Contains(a.Err, tc.want) {
				t.Fatalf("recorded error %q, want it to contain %q", a.Err, tc.want)
			}
			for gvr, a := range c.Snapshot().Access {
				if gvr != tc.gvr && !a.OK {
					t.Errorf("%s must stay readable, got %+v", gvr.Resource, a)
				}
			}
			// Further failed watches (and, for 403, successful relists) keep it unavailable.
			n := failed.Load()
			waitFor(t, "another failed watch", func() bool { return failed.Load() > n })
			if c.Snapshot().Access[tc.gvr].OK {
				t.Fatal("only a successful Watch call may clear the error")
			}
			fail.Store(false)
			waitFor(t, "recovery after a successful watch", func() bool { return c.Snapshot().Access[tc.gvr].OK })
		})
	}
}

func TestForbiddenNamespacesAreReported(t *testing.T) {
	dyn, kube := testutil.Clients(t, testutil.Namespace("shop", nil))
	kube.PrependReactor("list", "namespaces", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "", nil)
	})
	c := New(Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream()})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	s := waitSynced(t, c)
	if s.Access[NamespacesGVR].OK || s.Access[NamespacesGVR].Err == "" {
		t.Fatalf("namespaces must be reported as not accessible, got %+v", s.Access[NamespacesGVR])
	}
}

func TestForbiddenKindIsReportedAndDoesNotBlockSync(t *testing.T) {
	dyn, kube := testutil.Clients(t, testutil.Namespace("shop", nil))
	dyn.(*dynamicfake.FakeDynamicClient).PrependReactor("list", "httproutes", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: "httproutes"}, "", nil)
	})
	c := New(Config{Dynamic: dyn, Kube: kube, Served: testutil.ServedUpstream()})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	s := waitSynced(t, c)
	hr := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	if s.Access[hr].OK || s.Access[hr].Err == "" {
		t.Fatalf("httproutes must be reported as not accessible, got %+v", s.Access[hr])
	}
}
```

- [ ] **Step 3: Run them and confirm they fail**

Run: `go test ./pkg/cache/...`
Expected: FAIL (`undefined: New`)

- [ ] **Step 4: Implement**

`pkg/cache/cache.go`:

```go
// Package cache keeps a shared, read-only informer cache of the kinds the UI
// watches and exposes it as immutable snapshots.
package cache

import (
	"context"
	"sort"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	toolscache "k8s.io/client-go/tools/cache"

	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
)

// Access reports whether a kind can currently be read.
type Access struct {
	OK  bool   `json:"ok"`
	Err string `json:"error,omitempty"`
}

// ListTimeout bounds every List call an informer makes, so a hung list cannot
// keep the last good evidence alive. Watches rely on client-go's own watch
// timeout, which re-establishes them every 5–10 minutes.
const ListTimeout = time.Minute

// GVRs under which the typed informers report their Access.
var (
	NamespacesGVR  = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	PodsGVR        = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	DeploymentsGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
)

// Snapshot is an immutable view of the cache at TakenAt. Callers must not mutate it.
type Snapshot struct {
	Objects map[schema.GroupVersionResource][]*unstructured.Unstructured
	// Access holds every served kind plus NamespacesGVR, PodsGVR and DeploymentsGVR.
	Access      map[schema.GroupVersionResource]Access
	Namespaces  []*corev1.Namespace
	Pods        []*corev1.Pod
	Deployments []*appsv1.Deployment
	// Synced is true once every informer has synced or failed with an error.
	Synced  bool
	TakenAt time.Time
}

// Snapshotter returns the current snapshot.
type Snapshotter interface{ Snapshot() *Snapshot }

// Config configures a Cache. Pods and Deployments are only watched through
// the given label selectors, never cluster-wide (design §4.1).
type Config struct {
	Dynamic             dynamic.Interface
	Kube                kubernetes.Interface
	Served              []kinds.Served
	PodSelectors        []labels.Selector
	DeploymentSelectors []labels.Selector
}

// calls records the outcome of the List and Watch calls one informer's
// reflector makes. A failed call marks the kind unavailable; only a later
// successful Watch call clears it, because that is when the informer is
// receiving current events again (round 3, finding 1).
type calls struct {
	mu  sync.Mutex
	err string
}

func (c *calls) record(err error, isWatch bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case err != nil:
		c.err = err.Error()
	case isWatch:
		c.err = ""
	}
}

func (c *calls) last() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// tracked is an informer with the record of its own List and Watch calls.
type tracked struct {
	inf   toolscache.SharedIndexInformer
	calls *calls
}

// Cache is a set of informers whose contents are read through Snapshot.
type Cache struct {
	now     func() time.Time
	dyn     map[schema.GroupVersionResource]tracked
	ns      tracked
	pods    []tracked
	deploys []tracked
}

// instrumented builds an informer whose ListerWatcher records every List and
// Watch call outcome, including the connection-refused and 429 watch retries
// that client-go never reports to the watch error handler. client tells the
// reflector whether the client supports watch-list semantics (fakes do not).
func instrumented(client any, example runtime.Object, list toolscache.ListWithContextFunc, watchFn toolscache.WatchFuncWithContext) tracked {
	cs := &calls{}
	lw := toolscache.ToListWatcherWithWatchListSemantics(&toolscache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) {
			ctx, cancel := context.WithTimeout(ctx, ListTimeout)
			defer cancel()
			obj, err := list(ctx, o)
			cs.record(err, false)
			return obj, err
		},
		WatchFuncWithContext: func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) {
			w, err := watchFn(ctx, o)
			cs.record(err, true)
			return w, err
		},
	}, client)
	return tracked{inf: toolscache.NewSharedIndexInformer(lw, example, 0, toolscache.Indexers{}), calls: cs}
}

// New creates the informers. Nothing runs until Start.
func New(cfg Config) *Cache {
	c := &Cache{now: time.Now, dyn: map[schema.GroupVersionResource]tracked{}}
	for _, s := range cfg.Served {
		ri := cfg.Dynamic.Resource(s.GVR())
		c.dyn[s.GVR()] = instrumented(cfg.Dynamic, &unstructured.Unstructured{},
			func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) { return ri.List(ctx, o) },
			func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) { return ri.Watch(ctx, o) })
	}
	nss := cfg.Kube.CoreV1().Namespaces()
	c.ns = instrumented(cfg.Kube, &corev1.Namespace{},
		func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) { return nss.List(ctx, o) },
		func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) { return nss.Watch(ctx, o) })
	pods, deploys := cfg.Kube.CoreV1().Pods(""), cfg.Kube.AppsV1().Deployments("")
	for _, sel := range cfg.PodSelectors {
		c.pods = append(c.pods, instrumented(cfg.Kube, &corev1.Pod{},
			func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) { o.LabelSelector = sel.String(); return pods.List(ctx, o) },
			func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) { o.LabelSelector = sel.String(); return pods.Watch(ctx, o) }))
	}
	for _, sel := range cfg.DeploymentSelectors {
		c.deploys = append(c.deploys, instrumented(cfg.Kube, &appsv1.Deployment{},
			func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) { o.LabelSelector = sel.String(); return deploys.List(ctx, o) },
			func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) { o.LabelSelector = sel.String(); return deploys.Watch(ctx, o) }))
	}
	return c
}

func (c *Cache) all() []tracked {
	out := []tracked{c.ns}
	for _, t := range c.dyn {
		out = append(out, t)
	}
	return append(append(out, c.pods...), c.deploys...)
}

// Start runs all informers until ctx is cancelled.
func (c *Cache) Start(ctx context.Context) {
	for _, t := range c.all() {
		go t.inf.RunWithContext(ctx)
	}
}

// access reports whether t can currently be read. HasSynced records only the
// initial sync and stays true after later failures (finding 2), so the last
// failed List or Watch call wins over it until a Watch call succeeds.
func access(t tracked) Access {
	if e := t.calls.last(); e != "" {
		return Access{Err: e}
	}
	if t.inf.HasSynced() {
		return Access{OK: true}
	}
	return Access{}
}

// accessAll merges several informers of one kind: the first that is not OK wins.
func accessAll(ts []tracked) Access {
	for _, t := range ts {
		if a := access(t); !a.OK {
			return a
		}
	}
	return Access{OK: true}
}

// Snapshot copies the current cache contents.
func (c *Cache) Snapshot() *Snapshot {
	s := &Snapshot{Objects: map[schema.GroupVersionResource][]*unstructured.Unstructured{}, Access: map[schema.GroupVersionResource]Access{}, Synced: true, TakenAt: c.now()}
	settled := func(a Access) bool { return a.OK || a.Err != "" }
	for gvr, t := range c.dyn {
		a := access(t)
		s.Access[gvr] = a
		s.Synced = s.Synced && settled(a)
		for _, o := range t.inf.GetStore().List() {
			s.Objects[gvr] = append(s.Objects[gvr], o.(*unstructured.Unstructured))
		}
		sort.Slice(s.Objects[gvr], func(i, j int) bool { return key(s.Objects[gvr][i]) < key(s.Objects[gvr][j]) })
	}
	s.Access[NamespacesGVR] = access(c.ns)
	s.Access[PodsGVR] = accessAll(c.pods)
	s.Access[DeploymentsGVR] = accessAll(c.deploys)
	s.Synced = s.Synced && settled(s.Access[NamespacesGVR])
	for _, o := range c.ns.inf.GetStore().List() {
		s.Namespaces = append(s.Namespaces, o.(*corev1.Namespace))
	}
	seen := map[types.UID]bool{}
	for _, t := range c.pods {
		s.Synced = s.Synced && settled(access(t))
		for _, o := range t.inf.GetStore().List() {
			if p := o.(*corev1.Pod); !seen[p.UID] {
				seen[p.UID] = true
				s.Pods = append(s.Pods, p)
			}
		}
	}
	for _, t := range c.deploys {
		s.Synced = s.Synced && settled(access(t))
		for _, o := range t.inf.GetStore().List() {
			if d := o.(*appsv1.Deployment); !seen[d.UID] {
				seen[d.UID] = true
				s.Deployments = append(s.Deployments, d)
			}
		}
	}
	return s
}

func key(u *unstructured.Unstructured) string { return u.GetNamespace() + "/" + u.GetName() }
```

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `go test ./pkg/cache/... -race`
Expected: PASS. `ToListWatcherWithWatchListSemantics`, `ListWatch.ListWithContextFunc`/`WatchFuncWithContext` and `SharedIndexInformer.RunWithContext` exist in client-go v0.36.2 (`tools/cache/listwatch.go`, `shared_informer.go`).

**Known limit (round 3, finding 2).** A watch stream that stalls silently, with the TCP connection open and no events or errors, is not detected until client-go's watch timeout (5–10 minutes) ends it and the next Watch call reports the outcome. Lists are bounded by `ListTimeout`.

- [ ] **Step 6: Commit**

```bash
git add pkg/cache internal/testutil go.mod go.sum && git commit -m "feat(cache): label-scoped shared informer cache with access reporting"
```

---

### Task 6: Install discovery and namespace scope

**Files:**
- Create: `pkg/install/install.go`, `pkg/install/upstream.go`, `pkg/install/scope.go`
- Test: `pkg/install/install_test.go`, `pkg/install/scope_test.go`

**Interfaces:**
- Consumes: `cache.Snapshot`, the `testutil` fixtures.
- Produces:
  - `install.Install{Namespace, Name, Version, Edition, Container string; ControllerNames []string; Deployment *appsv1.Deployment; Pods []*corev1.Pod; MetricsPort int32}` with `ID() string` (`"ns/name"`);
  - `install.Detector` (interface: `Name() string; ControllerSelector() labels.Selector; ControllerPodSelector() labels.Selector; ProxySelector() labels.Selector; Detect(d *appsv1.Deployment) (Install, bool)`). `ControllerSelector` selects controller Deployments; `ControllerPodSelector` is the cache's Pod watch (A1); Pods are attributed to an install through the Deployment's `spec.selector` in the same namespace;
  - `install.UpstreamDetector{}`;
  - `install.Discover(snap *cache.Snapshot, detectors []Detector) []Install` (sorted by ID; first matching detector wins);
  - `install.ScopeMode` (`ScopeAll`, `ScopeSelectors`, `ScopeUnknown`);
  - `install.Scope{Mode ScopeMode; Selectors []metav1.LabelSelector; Err string}` with `Contains(nsName string, ns *corev1.Namespace) bool`;
  - `install.ScopeProvider` (interface: `Name() string; Scope(inst Install) Scope`);
  - `install.NamespaceSelectorScope{}`;
  - `install.ParseSelectors(raw string) Scope`;
  - `install.Resolved{Install; Scope Scope}`;
  - `install.Resolve(insts []Install, p ScopeProvider) []Resolved`;
  - `install.DiscoveryNamespaceSelectorsEnv = "KGW_DISCOVERY_NAMESPACE_SELECTORS"`.

- [ ] **Step 1: Write the failing tests**

`pkg/install/install_test.go`:

```go
package install

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
)

func TestDiscoverUpstreamInstall(t *testing.T) {
	d := testutil.ControllerDeployment("kgateway-system", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 2, 2)
	p1, p2 := testutil.ControllerPod(d, "kgateway-a", true), testutil.ControllerPod(d, "kgateway-b", true)
	mirrored := testutil.ControllerDeployment("kgw-b", "kgw-b", "registry.local:5000/mirror/kgateway@sha256:abc", 1, 1)
	unknown := testutil.ControllerDeployment("x", "other", "example.com/another-controller:1.0", 1, 1)
	snap := &cache.Snapshot{Deployments: []*appsv1.Deployment{unknown, mirrored, d}, Pods: testutil.PodList(p1, p2)}

	got := Discover(snap, []Detector{UpstreamDetector{}})
	if len(got) != 2 {
		t.Fatalf("want 2 installs (upstream image and its mirror), got %d", len(got))
	}
	in := got[0] // sorted by ID: kgateway-system/kgateway < kgw-b/kgw-b
	if in.ID() != "kgateway-system/kgateway" || in.Version != "2.4.7" || in.Edition != "kgateway" ||
		in.MetricsPort != 9092 || len(in.Pods) != 2 || in.ControllerNames[0] != "kgateway.dev/kgateway" {
		t.Fatalf("unexpected install %+v", in)
	}
}

func TestDetectorOrderLetsDownstreamWin(t *testing.T) {
	d := testutil.ControllerDeployment("ns", "c", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	got := Discover(&cache.Snapshot{Deployments: []*appsv1.Deployment{d}}, []Detector{fixedDetector{}, UpstreamDetector{}})
	if len(got) != 1 || got[0].Edition != "fixed" {
		t.Fatalf("first matching detector must win, got %+v", got)
	}
}

type fixedDetector struct{ UpstreamDetector }

func (fixedDetector) Detect(d *appsv1.Deployment) (Install, bool) {
	in, ok := UpstreamDetector{}.Detect(d)
	in.Edition = "fixed"
	return in, ok
}

// A1: the Pod watch must match the upstream Pod labels of every supported chart
// (v2.3+ adds app.kubernetes.io/component=controller to Pods; older charts and
// custom templates do not), and Pods are attributed through the Deployment's selector.
func TestControllerPodSelectorMatchesUpstreamPods(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 2, 2)
	current := testutil.ControllerPod(d, "kgateway-a", true)
	older := testutil.ControllerPod(d, "kgateway-b", true)
	delete(older.Labels, "app.kubernetes.io/component")
	det := UpstreamDetector{}
	for _, p := range []*corev1.Pod{current, older} {
		if !det.ControllerPodSelector().Matches(labels.Set(p.Labels)) {
			t.Fatalf("pod %s with labels %v must be watched", p.Name, p.Labels)
		}
	}
	if det.ControllerSelector().Matches(labels.Set(older.Labels)) {
		t.Fatal("the Deployment selector requires the component label; it must not be used for Pods")
	}
	other := testutil.ControllerPod(testutil.ControllerDeployment("kgw", "other", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1), "other-a", true)
	got := Discover(&cache.Snapshot{Deployments: []*appsv1.Deployment{d}, Pods: testutil.PodList(current, older, other)}, []Detector{det})
	if len(got) != 1 || len(got[0].Pods) != 2 {
		t.Fatalf("both pods of kgw/kgateway, and only those, must be attributed to it: %+v", got)
	}
}
```

Add a helper to `internal/testutil/fixtures.go` (this extends Task 5's file):

```go
// PodList converts pods for use in a Snapshot.
func PodList(ps ...*corev1.Pod) []*corev1.Pod { return ps }
```

`pkg/install/scope_test.go`:

```go
package install

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
)

func ns(name string, l map[string]string) *corev1.Namespace { return testutil.Namespace(name, l) }

func TestParseSelectorsMirrorsKgatewaySemantics(t *testing.T) {
	a := ns("a", map[string]string{"team": "a"})
	b := ns("b", map[string]string{"team": "b", "env": "prod"})
	c := ns("c", nil)
	cases := []struct {
		name, raw string
		mode      ScopeMode
		in        []*corev1.Namespace
		out       []*corev1.Namespace
	}{
		{"unset means all", "", ScopeAll, []*corev1.Namespace{a, b, c}, nil},
		{"empty list means all", "[]", ScopeAll, []*corev1.Namespace{a, b, c}, nil},
		{"empty selector matches all", "[{}]", ScopeSelectors, []*corev1.Namespace{a, b, c}, nil},
		{"selectors are OR'ed", `[{"matchLabels":{"team":"a"}},{"matchLabels":{"env":"prod"}}]`, ScopeSelectors, []*corev1.Namespace{a, b}, []*corev1.Namespace{c}},
		{"matchExpressions", `[{"matchExpressions":[{"key":"team","operator":"In","values":["b"]}]}]`, ScopeSelectors, []*corev1.Namespace{b}, []*corev1.Namespace{a, c}},
		{"invalid JSON is unknown", `[{`, ScopeUnknown, nil, nil},
		{"invalid operator is unknown", `[{"matchExpressions":[{"key":"team","operator":"Bogus"}]}]`, ScopeUnknown, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := ParseSelectors(tc.raw)
			if s.Mode != tc.mode {
				t.Fatalf("mode: want %s got %s (%s)", tc.mode, s.Mode, s.Err)
			}
			for _, n := range tc.in {
				if !s.Contains(n.Name, n) {
					t.Errorf("%s should be in scope", n.Name)
				}
			}
			for _, n := range tc.out {
				if s.Contains(n.Name, n) {
					t.Errorf("%s should be out of scope", n.Name)
				}
			}
		})
	}
}

func TestScopeAllContainsNamespaceMissingFromCache(t *testing.T) {
	// Objects can arrive before their Namespace is cached; "all" must not drop them.
	if !ParseSelectors("[]").Contains("late", nil) {
		t.Fatal("all-namespaces scope must contain a namespace not yet cached")
	}
	if ParseSelectors(`[{"matchLabels":{"team":"a"}}]`).Contains("late", nil) {
		t.Fatal("a selector scope cannot match a namespace whose labels are unknown")
	}
}

func TestNamespaceSelectorScopeReadsPodEnv(t *testing.T) {
	img := "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7"
	sel := `[{"matchLabels":{"team":"a"}}]`
	cases := []struct {
		name string
		env  []corev1.EnvVar
		mode ScopeMode
	}{
		{"literal value", []corev1.EnvVar{testutil.Env(DiscoveryNamespaceSelectorsEnv, sel)}, ScopeSelectors},
		{"duplicate env: last wins", []corev1.EnvVar{testutil.Env(DiscoveryNamespaceSelectorsEnv, sel), testutil.Env(DiscoveryNamespaceSelectorsEnv, "[]")}, ScopeAll},
		{"valueFrom is unknown", []corev1.EnvVar{{Name: DiscoveryNamespaceSelectorsEnv, ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{Key: "k"}}}}, ScopeUnknown},
		{"absent means all", nil, ScopeAll},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, 1, tc.env...)
			in, _ := UpstreamDetector{}.Detect(d)
			in.Pods = []*corev1.Pod{testutil.ControllerPod(d, "p", true)}
			if got := (NamespaceSelectorScope{}).Scope(in).Mode; got != tc.mode {
				t.Fatalf("want %s got %s", tc.mode, got)
			}
		})
	}
}

// Review Focus 3: a crash-looping controller (no ready Pod) still has a Pod spec;
// the scope is read from it rather than becoming unknown.
func TestScopeFromNotReadyPod(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 0,
		testutil.Env(DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"a"}}]`))
	in, _ := UpstreamDetector{}.Detect(d)
	in.Pods = []*corev1.Pod{testutil.ControllerPod(d, "p", false)}
	if got := (NamespaceSelectorScope{}).Scope(in).Mode; got != ScopeSelectors {
		t.Fatalf("want selectors from the not-ready pod's spec, got %s", got)
	}
	in.Pods = nil
	if got := (NamespaceSelectorScope{}).Scope(in).Mode; got != ScopeUnknown {
		t.Fatalf("no pods at all must be unknown, got %s", got)
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./pkg/install/...`
Expected: FAIL (`undefined: Discover`)

- [ ] **Step 3: Implement**

`pkg/install/install.go`:

```go
// Package install discovers kgateway controller installs, resolves the
// namespace scope each one watches, and assigns resources to installs.
package install

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
)

// Install is one kgateway controller deployment and what the UI knows about it.
type Install struct {
	Namespace       string
	Name            string
	Version         string
	Edition         string
	ControllerNames []string
	// Container is the name of the controller container (the source of the env-based settings).
	Container   string
	Deployment  *appsv1.Deployment
	Pods        []*corev1.Pod
	MetricsPort int32
}

// ID returns "namespace/name" of the controller Deployment.
func (i Install) ID() string { return i.Namespace + "/" + i.Name }

// Detector recognises the controller Deployments of one distribution.
// Detectors are tried in order; the first that matches wins.
type Detector interface {
	Name() string
	// ControllerSelector selects controller Deployments; the cache watches only these.
	ControllerSelector() labels.Selector
	// ControllerPodSelector selects the controller Pods the cache watches. It may
	// be broader than the Pod labels of one chart version: Discover attributes
	// Pods to an install through the Deployment's spec.selector.
	ControllerPodSelector() labels.Selector
	// ProxySelector selects the gateway proxy Deployments of this distribution.
	ProxySelector() labels.Selector
	// Detect returns the Install for d, or false if d is not this distribution's controller.
	Detect(d *appsv1.Deployment) (Install, bool)
}

// Discover returns one Install per controller Deployment in snap, sorted by ID.
func Discover(snap *cache.Snapshot, detectors []Detector) []Install {
	var out []Install
	for _, d := range snap.Deployments {
		for _, det := range detectors {
			if !det.ControllerSelector().Matches(labels.Set(d.Labels)) {
				continue
			}
			in, ok := det.Detect(d)
			if !ok {
				continue
			}
			in.Pods = podsOf(d, snap.Pods)
			out = append(out, in)
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

func podsOf(d *appsv1.Deployment, pods []*corev1.Pod) []*corev1.Pod {
	sel, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		return nil
	}
	var out []*corev1.Pod
	for _, p := range pods {
		if p.Namespace == d.Namespace && sel.Matches(labels.Set(p.Labels)) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
```

`pkg/install/upstream.go`:

```go
package install

import (
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// UpstreamControllerName is the GatewayClass controllerName of upstream kgateway,
// used by both the "kgateway" and "kgateway-waypoint" classes.
const UpstreamControllerName = "kgateway.dev/kgateway"

// UpstreamDetector recognises upstream kgateway controllers: Helm labels
// kgateway=kgateway and app.kubernetes.io/component=controller, and an image
// whose repository name is "kgateway" (mirrors keep that name).
type UpstreamDetector struct{}

// Name implements Detector.
func (UpstreamDetector) Name() string { return "kgateway" }

// ControllerSelector implements Detector.
func (UpstreamDetector) ControllerSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"kgateway": "kgateway", "app.kubernetes.io/component": "controller"})
}

// ControllerPodSelector implements Detector. Controller Pods carry the
// component label only from chart v2.3.0 on, so the watch uses kgateway=kgateway (A1).
func (UpstreamDetector) ControllerPodSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"kgateway": "kgateway"})
}

// ProxySelector implements Detector.
func (UpstreamDetector) ProxySelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"app.kubernetes.io/managed-by": "kgateway", "app.kubernetes.io/component": "proxy"})
}

// Detect implements Detector.
func (UpstreamDetector) Detect(d *appsv1.Deployment) (Install, bool) {
	for _, c := range d.Spec.Template.Spec.Containers {
		repo, tag := splitImage(c.Image)
		if repo != "kgateway" {
			continue
		}
		in := Install{Namespace: d.Namespace, Name: d.Name, Edition: "kgateway", Container: c.Name,
			ControllerNames: []string{UpstreamControllerName}, Deployment: d, MetricsPort: 9092}
		in.Version = d.Labels["app.kubernetes.io/version"]
		if in.Version == "" {
			in.Version = strings.TrimPrefix(tag, "v")
		}
		for _, p := range c.Ports {
			if p.Name == "metrics" {
				in.MetricsPort = p.ContainerPort
			}
		}
		return in, true
	}
	return Install{}, false
}

// splitImage returns the last path segment of the repository and the tag
// ("" for digest-only references). "reg:5000/a/kgateway:v1" → ("kgateway", "v1").
func splitImage(image string) (repo, tag string) {
	if i := strings.Index(image, "@"); i >= 0 {
		image = image[:i]
	}
	last := image[strings.LastIndex(image, "/")+1:]
	if i := strings.Index(last, ":"); i >= 0 {
		return last[:i], last[i+1:]
	}
	return last, ""
}
```

`pkg/install/scope.go`:

```go
package install

import (
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// DiscoveryNamespaceSelectorsEnv is the controller env var rendered from the
// Helm value discoveryNamespaceSelectors.
const DiscoveryNamespaceSelectorsEnv = "KGW_DISCOVERY_NAMESPACE_SELECTORS"

// ScopeMode says how a Scope decides namespace membership.
type ScopeMode string

// Scope modes.
const (
	ScopeAll       ScopeMode = "all"
	ScopeSelectors ScopeMode = "selectors"
	// ScopeUnknown means the scope could not be determined. Contains then
	// answers as if all namespaces were in scope so the UI still shows data,
	// and the health report is forced to "unknown" (design §5.3).
	ScopeUnknown ScopeMode = "unknown"
)

// Scope is the set of namespaces a controller install watches.
type Scope struct {
	Mode      ScopeMode              `json:"mode"`
	Selectors []metav1.LabelSelector `json:"selectors,omitempty"`
	Err       string                 `json:"error,omitempty"`
	matchers  []labels.Selector
}

// Contains reports whether namespace nsName (with object ns, which may be nil
// if the namespace is not cached yet) is in scope.
func (s Scope) Contains(nsName string, ns *corev1.Namespace) bool {
	switch s.Mode {
	case ScopeAll, ScopeUnknown:
		return true
	case ScopeSelectors:
		if ns == nil {
			return false
		}
		for _, m := range s.matchers {
			if m.Matches(labels.Set(ns.Labels)) {
				return true
			}
		}
	}
	return false
}

// ScopeProvider resolves the scope of an install (D-009). Downstreams may register their own.
type ScopeProvider interface {
	Name() string
	Scope(inst Install) Scope
}

// NamespaceSelectorScope mirrors kgateway's discoveryNamespaceSelectors: a JSON
// list of label selectors read from the controller Pod env, OR'ed, where an
// empty or absent list means all namespaces.
type NamespaceSelectorScope struct{}

// Name implements ScopeProvider.
func (NamespaceSelectorScope) Name() string { return "namespace-selectors" }

// Scope implements ScopeProvider. It reads the env of a controller Pod, not
// the Deployment, so a rollout in progress reflects what actually runs. Ready
// pods are preferred, but any pod's spec is used if none is ready.
func (NamespaceSelectorScope) Scope(inst Install) Scope {
	pod := pickPod(inst.Pods)
	if pod == nil {
		return Scope{Mode: ScopeUnknown, Err: "no controller pod found"}
	}
	var c *corev1.Container
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == inst.Container {
			c = &pod.Spec.Containers[i]
		}
	}
	if c == nil {
		return Scope{Mode: ScopeUnknown, Err: fmt.Sprintf("container %q not found in pod %s", inst.Container, pod.Name)}
	}
	var val string
	found := false
	for _, e := range c.Env { // duplicate entries: the last one wins, as in Kubernetes
		if e.Name != DiscoveryNamespaceSelectorsEnv {
			continue
		}
		if e.ValueFrom != nil {
			return Scope{Mode: ScopeUnknown, Err: DiscoveryNamespaceSelectorsEnv + " uses valueFrom, which the UI cannot resolve"}
		}
		val, found = e.Value, true
	}
	if !found {
		return Scope{Mode: ScopeAll}
	}
	return ParseSelectors(val)
}

func pickPod(pods []*corev1.Pod) *corev1.Pod {
	for _, p := range pods {
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				return p
			}
		}
	}
	if len(pods) > 0 {
		return pods[0]
	}
	return nil
}

// ParseSelectors parses the value of KGW_DISCOVERY_NAMESPACE_SELECTORS.
func ParseSelectors(raw string) Scope {
	if strings.TrimSpace(raw) == "" {
		return Scope{Mode: ScopeAll}
	}
	var sels []metav1.LabelSelector
	if err := json.Unmarshal([]byte(raw), &sels); err != nil {
		return Scope{Mode: ScopeUnknown, Err: "invalid " + DiscoveryNamespaceSelectorsEnv + ": " + err.Error()}
	}
	if len(sels) == 0 {
		return Scope{Mode: ScopeAll}
	}
	s := Scope{Mode: ScopeSelectors, Selectors: sels}
	for i := range sels {
		m, err := metav1.LabelSelectorAsSelector(&sels[i])
		if err != nil {
			return Scope{Mode: ScopeUnknown, Err: "invalid selector: " + err.Error()}
		}
		s.matchers = append(s.matchers, m)
	}
	return s
}

// Resolved is an Install with its resolved Scope.
type Resolved struct {
	Install
	Scope Scope
}

// Resolve applies p to every install.
func Resolve(insts []Install, p ScopeProvider) []Resolved {
	out := make([]Resolved, 0, len(insts))
	for _, in := range insts {
		out = append(out, Resolved{Install: in, Scope: p.Scope(in)})
	}
	return out
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./pkg/install/... -run 'Discover|Detector|Selector|Scope|PodSelector'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/install internal/testutil && git commit -m "feat(install): controller install discovery and namespace scope"
```

---

### Task 7: Assigning resources to installs

**Files:**
- Create: `pkg/install/assign.go`
- Test: `pkg/install/assign_test.go`

**Interfaces:**
- Consumes: `cache.Snapshot`, `kinds.Served`, `kinds.KgatewayGroup`, `objects.*`, `install.Resolved`.
- Produces:
  - `install.Owned{GatewayClasses, Gateways, ListenerSets []*unstructured.Unstructured; Routes, Policies []OwnedObject; Unattached []UnattachedRoute}`;
  - `install.OwnedObject{Kind kinds.Served; Obj *unstructured.Unstructured; Parents []objects.ParentRef}`. For a route, `Parents` are its current `spec.parentRefs` that resolve to a Gateway or ListenerSet of this install; only their status entries are evaluated (finding 5). Empty for policies;
  - `install.UnattachedRoute{Route OwnedObject; Parent objects.ParentRef}` (a diagnostic candidate for KGWUI-003, not counted; finding 9);
  - `install.AmbiguousGateway{Gateway objects.Ref; Installs []string}`;
  - `install.UnwatchedRoute{Install string; Route objects.Ref; Gateway objects.Ref}`;
  - `install.Assignment{PerInstall map[string]*Owned; Ambiguous []AmbiguousGateway; Unwatched []UnwatchedRoute}`;
  - `install.Assign(snap *cache.Snapshot, served []kinds.Served, insts []Resolved) Assignment`.

**Rules** (design §4.3): Every reference is resolved by its full group/kind/namespace/name identity (`objects.ParentRef.Ref()`); a parentRef in another API group never matches a Gateway API object (finding 6).
- **Gateways.** A Gateway belongs to install I when its class's `controllerName` ∈ I.ControllerNames and the Gateway's namespace is in I's scope. If more than one install matches, the Gateway is ambiguous and assigned to none.
- **ListenerSets.** A ListenerSet follows its parent Gateway, but only if the ListenerSet's own namespace is in scope.
- **Routes.** A route belongs to I when one of its parentRefs resolves (directly, or through a ListenerSet) to a Gateway of I **and the route's own namespace** is in I's scope; its `Parents` are exactly those parentRefs. If the route's namespace is out of scope, it is recorded as Unwatched for I.
- **Unattached routes.** A parentRef of a served Gateway or ListenerSet kind whose target does not exist makes the route Unattached in every install whose scope contains the route's namespace (finding 9). A route can be owned through one parent and unattached through another.
- **Policies.** A kgateway-group policy belongs to every install whose scope contains its namespace. Any other policy (e.g. BackendTLSPolicy) belongs to I when it is in scope and either has a `status.ancestors` entry with I's controller name or has no ancestors at all yet: it is new and not yet processed (finding 10).

- [ ] **Step 1: Write the failing tests**

`pkg/install/assign_test.go`:

```go
package install

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// snapshot builds a Snapshot from fixtures, grouping unstructured objects by
// the served kind that matches their apiVersion/kind.
func snapshot(nss []*corev1.Namespace, objs ...*unstructured.Unstructured) *cache.Snapshot {
	s := &cache.Snapshot{Objects: map[schema.GroupVersionResource][]*unstructured.Unstructured{}, Namespaces: nss, Synced: true}
	for _, o := range objs {
		gv, _ := schema.ParseGroupVersion(o.GetAPIVersion())
		for _, k := range testutil.ServedUpstream() {
			if k.Group == gv.Group && k.Kind == o.GetKind() {
				s.Objects[k.GVR()] = append(s.Objects[k.GVR()], o)
			}
		}
	}
	return s
}

func resolved(id string, scopeJSON string) Resolved {
	ns, name := splitID(id)
	return Resolved{Install: Install{Namespace: ns, Name: name, ControllerNames: []string{testutil.Controller}}, Scope: ParseSelectors(scopeJSON)}
}

func splitID(id string) (string, string) {
	for i := range id {
		if id[i] == '/' {
			return id[:i], id[i+1:]
		}
	}
	return "", id
}

func names(objs []OwnedObject) []string {
	var out []string
	for _, o := range objs {
		out = append(out, o.Obj.GetNamespace()+"/"+o.Obj.GetName())
	}
	return out
}

var teamA = `[{"matchLabels":{"team":"a"}}]`

func TestAssignGatewaysRoutesAndPolicies(t *testing.T) {
	nss := []*corev1.Namespace{
		testutil.Namespace("infra", map[string]string{"team": "a"}),
		testutil.Namespace("shop", map[string]string{"team": "a"}),
		testutil.Namespace("blog", nil),
	}
	snap := snapshot(nss,
		testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.GatewayClass("istio", "istio.io/gateway-controller", true),
		testutil.Gateway("infra", "edge", "kgateway", nil),
		testutil.Gateway("infra", "mesh", "istio", nil),
		// Review Focus 2: cross-namespace parentRef; the route's own namespace (shop) decides scope.
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{testutil.GwRef("infra", "edge")}),
		testutil.HTTPRoute("blog", "posts", []objects.ParentRef{testutil.GwRef("infra", "edge")}),
		testutil.HTTPRoute("shop", "istio-only", []objects.ParentRef{testutil.GwRef("infra", "mesh")}),
		testutil.TrafficPolicy("shop", "tp"),
		testutil.TrafficPolicy("blog", "tp-out"),
	)
	a := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw/kgateway", teamA)})
	o := a.PerInstall["kgw/kgateway"]
	if len(o.Gateways) != 1 || o.Gateways[0].GetName() != "edge" {
		t.Fatalf("gateways: %d", len(o.Gateways))
	}
	if got := names(o.Routes); len(got) != 1 || got[0] != "shop/cart" {
		t.Fatalf("routes: %v (istio-only must be ignored, blog/posts is out of scope)", got)
	}
	if len(a.Unwatched) != 1 || a.Unwatched[0].Route.String() != "blog/posts" || a.Unwatched[0].Gateway.String() != "infra/edge" {
		t.Fatalf("unwatched: %+v", a.Unwatched)
	}
	if got := names(o.Policies); len(got) != 1 || got[0] != "shop/tp" {
		t.Fatalf("policies: %v", got)
	}
	if len(o.GatewayClasses) != 1 || o.GatewayClasses[0].GetName() != "kgateway" {
		t.Fatalf("classes: %d", len(o.GatewayClasses))
	}
}

func TestAssignRouteViaListenerSet(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("infra", nil), testutil.Namespace("shop", nil)}
	ls := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "ListenerSet",
		"metadata": map[string]any{"name": "ls", "namespace": "shop"},
		"spec":     map[string]any{"parentRef": map[string]any{"name": "edge", "namespace": "infra"}},
	}}
	snap := snapshot(nss,
		testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.Gateway("infra", "edge", "kgateway", nil), ls,
		testutil.HTTPRoute("shop", "r", []objects.ParentRef{{Group: objects.GatewayAPIGroup, Kind: "ListenerSet", Namespace: "shop", Name: "ls"}}),
	)
	o := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw/kgateway", "[]")}).PerInstall["kgw/kgateway"]
	if len(o.ListenerSets) != 1 || len(o.Routes) != 1 {
		t.Fatalf("listenersets=%d routes=%d", len(o.ListenerSets), len(o.Routes))
	}
}

func TestAssignAmbiguousGateway(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("shared", map[string]string{"team": "a", "env": "x"})}
	snap := snapshot(nss, testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.Gateway("shared", "gw", "kgateway", nil))
	a := Assign(snap, testutil.ServedUpstream(), []Resolved{
		resolved("kgw-a/a", teamA), resolved("kgw-b/b", `[{"matchLabels":{"env":"x"}}]`),
	})
	if len(a.Ambiguous) != 1 || len(a.Ambiguous[0].Installs) != 2 {
		t.Fatalf("want 1 ambiguous gateway claimed by 2 installs, got %+v", a.Ambiguous)
	}
	if len(a.PerInstall["kgw-a/a"].Gateways)+len(a.PerInstall["kgw-b/b"].Gateways) != 0 {
		t.Fatal("an ambiguous gateway must not be assigned to any install")
	}
}

func TestDisjointInstallsSplitCleanly(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("app-a", map[string]string{"team": "a"}), testutil.Namespace("app-b", map[string]string{"team": "b"})}
	snap := snapshot(nss, testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.Gateway("app-a", "gw", "kgateway", nil), testutil.Gateway("app-b", "gw", "kgateway", nil))
	a := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw-a/a", teamA), resolved("kgw-b/b", `[{"matchLabels":{"team":"b"}}]`)})
	if len(a.PerInstall["kgw-a/a"].Gateways) != 1 || len(a.PerInstall["kgw-b/b"].Gateways) != 1 || len(a.Ambiguous) != 0 {
		t.Fatalf("unexpected split: a=%d b=%d ambiguous=%d", len(a.PerInstall["kgw-a/a"].Gateways), len(a.PerInstall["kgw-b/b"].Gateways), len(a.Ambiguous))
	}
}

func TestStaleStatusOutOfScopeIsIgnored(t *testing.T) {
	// A namespace deselected after kgateway wrote status: the old status must not make the route count.
	nss := []*corev1.Namespace{testutil.Namespace("infra", map[string]string{"team": "a"}), testutil.Namespace("old", nil)}
	stale := testutil.HTTPRoute("old", "r", []objects.ParentRef{testutil.GwRef("infra", "edge")}, testutil.AcceptedBy(testutil.GwRef("infra", "edge"), testutil.Controller))
	snap := snapshot(nss, testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.Gateway("infra", "edge", "kgateway", nil), stale)
	a := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw/kgateway", teamA)})
	if len(a.PerInstall["kgw/kgateway"].Routes) != 0 {
		t.Fatal("out-of-scope route with stale kgateway status must not be assigned")
	}
}

// Finding 6: a parentRef or ListenerSet parent in another API group never
// resolves to a Gateway API Gateway with the same namespace/name.
func TestForeignGroupReferencesDoNotResolve(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("infra", nil), testutil.Namespace("shop", nil)}
	foreignGw := objects.ParentRef{Group: "example.com", Kind: "Gateway", Namespace: "infra", Name: "edge"}
	ls := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "ListenerSet",
		"metadata": map[string]any{"name": "ls", "namespace": "shop"},
		"spec":     map[string]any{"parentRef": map[string]any{"group": "example.com", "kind": "Gateway", "name": "edge", "namespace": "infra"}},
	}}
	snap := snapshot(nss,
		testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.Gateway("infra", "edge", "kgateway", nil), ls,
		testutil.HTTPRoute("shop", "foreign", []objects.ParentRef{foreignGw}),
	)
	o := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw/kgateway", "[]")}).PerInstall["kgw/kgateway"]
	if len(o.Routes) != 0 || len(o.ListenerSets) != 0 || len(o.Unattached) != 0 {
		t.Fatalf("foreign-group references must not resolve: routes=%v listenersets=%d unattached=%d", names(o.Routes), len(o.ListenerSets), len(o.Unattached))
	}
}

// Finding 5: a route attached to Gateways of two installs carries, per install,
// only the parentRefs that resolve to that install's Gateways.
func TestRouteParentsAreScopedToTheInstall(t *testing.T) {
	nss := []*corev1.Namespace{
		testutil.Namespace("infra-a", map[string]string{"team": "a"}),
		testutil.Namespace("infra-b", map[string]string{"env": "x"}),
		testutil.Namespace("shop", map[string]string{"team": "a", "env": "x"}),
	}
	ga, gb := testutil.GwRef("infra-a", "edge"), testutil.GwRef("infra-b", "edge")
	snap := snapshot(nss, testutil.GatewayClass("kgateway", testutil.Controller, true),
		testutil.Gateway("infra-a", "edge", "kgateway", nil), testutil.Gateway("infra-b", "edge", "kgateway", nil),
		testutil.HTTPRoute("shop", "both", []objects.ParentRef{ga, gb}))
	a := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw-a/a", teamA), resolved("kgw-b/b", `[{"matchLabels":{"env":"x"}}]`)})
	for id, want := range map[string]objects.ParentRef{"kgw-a/a": ga, "kgw-b/b": gb} {
		rs := a.PerInstall[id].Routes
		if len(rs) != 1 || len(rs[0].Parents) != 1 || rs[0].Parents[0] != want {
			t.Fatalf("%s: want only parent %+v, got %+v", id, want, rs)
		}
	}
}

// Finding 9: a route whose parent Gateway does not exist is kept as a
// diagnostic candidate in every install whose scope contains the route.
func TestRouteToMissingGatewayIsUnattached(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("infra", map[string]string{"team": "a"}), testutil.Namespace("shop", map[string]string{"team": "a"}),
		testutil.Namespace("blog", nil)}
	gone := testutil.GwRef("infra", "gone")
	snap := snapshot(nss, testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.Gateway("infra", "edge", "kgateway", nil),
		testutil.HTTPRoute("shop", "orphan", []objects.ParentRef{gone}),
		testutil.HTTPRoute("shop", "half", []objects.ParentRef{testutil.GwRef("infra", "edge"), gone}),
		testutil.HTTPRoute("blog", "elsewhere", []objects.ParentRef{gone}))
	o := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw/kgateway", teamA)}).PerInstall["kgw/kgateway"]
	if got := names(o.Routes); len(got) != 1 || got[0] != "shop/half" {
		t.Fatalf("only shop/half is owned (through infra/edge): %v", got)
	}
	var un []string
	for _, u := range o.Unattached {
		if u.Parent != gone {
			t.Fatalf("unexpected parent %+v", u.Parent)
		}
		un = append(un, u.Route.Obj.GetNamespace()+"/"+u.Route.Obj.GetName())
	}
	if len(un) != 2 || un[0] != "shop/orphan" || un[1] != "shop/half" {
		t.Fatalf("unattached: %v (blog/elsewhere is out of scope)", un)
	}
}

// Finding 10: a non-kgateway policy in scope belongs to the install while it has
// no ancestors yet, and stops belonging to it once another controller owns it.
func TestGenericPolicyWithoutStatusIsOwned(t *testing.T) {
	nss := []*corev1.Namespace{testutil.Namespace("shop", nil)}
	other := testutil.PolicyAncestor{Ref: testutil.GwRef("shop", "mesh"), Controller: "istio.io/gateway-controller"}
	mine := testutil.PolicyAncestor{Ref: testutil.GwRef("shop", "edge"), Controller: testutil.Controller}
	snap := snapshot(nss,
		testutil.BackendTLSPolicy("shop", "new"),
		testutil.BackendTLSPolicy("shop", "istio", other),
		testutil.BackendTLSPolicy("shop", "ours", mine))
	o := Assign(snap, testutil.ServedUpstream(), []Resolved{resolved("kgw/kgateway", "[]")}).PerInstall["kgw/kgateway"]
	if got := names(o.Policies); len(got) != 2 || got[0] != "shop/new" || got[1] != "shop/ours" {
		t.Fatalf("policies: %v", got)
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./pkg/install/... -run Assign`
Expected: FAIL (`undefined: Assign`)

- [ ] **Step 3: Implement**

`pkg/install/assign.go`:

```go
package install

import (
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// OwnedObject is a route or policy together with its kind.
type OwnedObject struct {
	Kind kinds.Served
	Obj  *unstructured.Unstructured
	// Parents are the route's current spec.parentRefs that resolve to a Gateway
	// or ListenerSet of this install. Only their status entries are evaluated
	// (finding 5). Empty for policies.
	Parents []objects.ParentRef
}

// UnattachedRoute is a route in an install's scope whose parentRef names a
// Gateway or ListenerSet that does not exist. It is a diagnostic candidate
// for KGWUI-003 and is not counted as owned (finding 9).
type UnattachedRoute struct {
	Route  OwnedObject
	Parent objects.ParentRef
}

// Owned is everything assigned to one install.
type Owned struct {
	GatewayClasses []*unstructured.Unstructured
	Gateways       []*unstructured.Unstructured
	ListenerSets   []*unstructured.Unstructured
	Routes         []OwnedObject
	Policies       []OwnedObject
	Unattached     []UnattachedRoute
}

// AmbiguousGateway is a Gateway claimed by more than one install (finding KGWUI-009).
type AmbiguousGateway struct {
	Gateway  objects.Ref
	Installs []string
}

// UnwatchedRoute is a route that targets a Gateway of Install from a namespace
// that Install does not watch (finding KGWUI-004).
type UnwatchedRoute struct {
	Install string
	Route   objects.Ref
	Gateway objects.Ref
}

// Assignment maps resources to installs (design §4.3).
type Assignment struct {
	PerInstall map[string]*Owned
	Ambiguous  []AmbiguousGateway
	Unwatched  []UnwatchedRoute
}

// Assign assigns the snapshot's resources to insts. References are resolved
// by full group/kind/namespace/name identity (finding 6).
func Assign(snap *cache.Snapshot, served []kinds.Served, insts []Resolved) Assignment {
	a := Assignment{PerInstall: map[string]*Owned{}}
	for _, in := range insts {
		a.PerInstall[in.ID()] = &Owned{}
	}
	nsByName := map[string]*corev1.Namespace{}
	for _, n := range snap.Namespaces {
		nsByName[n.Name] = n
	}
	inScope := func(in Resolved, ns string) bool { return in.Scope.Contains(ns, nsByName[ns]) }
	list := func(c kinds.Category) []OwnedObject {
		var out []OwnedObject
		for _, k := range kinds.ByCategory(served, c) {
			for _, o := range snap.Objects[k.GVR()] {
				out = append(out, OwnedObject{Kind: k, Obj: o})
			}
		}
		return out
	}

	// GatewayClass name → installs whose controller owns it.
	classOwners := map[string][]int{}
	for _, gc := range list(kinds.CategoryGatewayClass) {
		cn := objects.ControllerName(gc.Obj)
		for i, in := range insts {
			if slices.Contains(in.ControllerNames, cn) {
				classOwners[gc.Obj.GetName()] = append(classOwners[gc.Obj.GetName()], i)
				a.PerInstall[in.ID()].GatewayClasses = append(a.PerInstall[in.ID()].GatewayClasses, gc.Obj)
			}
		}
	}

	// Gateways: class controllerName ∩ namespace scope.
	gwOwner := map[objects.Ref]int{}
	exists := map[objects.Ref]bool{} // every Gateway and ListenerSet in the snapshot
	for _, gw := range list(kinds.CategoryGateway) {
		gref := objects.RefOf(gw.Obj)
		exists[gref] = true
		var cands []int
		for _, i := range classOwners[objects.GatewayClassName(gw.Obj)] {
			if inScope(insts[i], gw.Obj.GetNamespace()) {
				cands = append(cands, i)
			}
		}
		switch len(cands) {
		case 0:
		case 1:
			gwOwner[gref] = cands[0]
			o := a.PerInstall[insts[cands[0]].ID()]
			o.Gateways = append(o.Gateways, gw.Obj)
		default:
			amb := AmbiguousGateway{Gateway: gref}
			for _, i := range cands {
				amb.Installs = append(amb.Installs, insts[i].ID())
			}
			a.Ambiguous = append(a.Ambiguous, amb)
		}
	}

	// ListenerSets follow their parent Gateway when they are in scope themselves.
	lsGateway := map[objects.Ref]objects.Ref{}
	for _, ls := range list(kinds.CategoryListenerSet) {
		lref := objects.RefOf(ls.Obj)
		exists[lref] = true
		gref := objects.ListenerSetParent(ls.Obj).Ref()
		i, ok := gwOwner[gref]
		if !ok || !inScope(insts[i], ls.Obj.GetNamespace()) {
			continue
		}
		lsGateway[lref] = gref
		o := a.PerInstall[insts[i].ID()]
		o.ListenerSets = append(o.ListenerSets, ls.Obj)
	}

	// parentKinds are the served group/kinds a route parentRef can name; only
	// for these can the UI tell that the target does not exist.
	parentKinds := map[[2]string]bool{}
	for _, k := range append(kinds.ByCategory(served, kinds.CategoryGateway), kinds.ByCategory(served, kinds.CategoryListenerSet)...) {
		parentKinds[[2]string{k.Group, k.Kind}] = true
	}

	// Routes: the route's own namespace decides scope (Review Focus 2).
	for _, r := range list(kinds.CategoryRoute) {
		parents := map[int][]objects.ParentRef{} // install index → parentRefs resolving to its Gateways
		gateway := map[int]objects.Ref{}         // install index → first Gateway reached
		var order []int
		for _, p := range objects.RouteParentRefs(r.Obj) {
			pref := p.Ref()
			gref := pref
			if g, ok := lsGateway[pref]; ok {
				gref = g
			}
			if i, ok := gwOwner[gref]; ok {
				if _, seen := parents[i]; !seen {
					order = append(order, i)
					gateway[i] = gref
				}
				parents[i] = append(parents[i], p)
				continue
			}
			if parentKinds[[2]string{pref.Group, pref.Kind}] && !exists[pref] {
				for _, in := range insts {
					if inScope(in, r.Obj.GetNamespace()) {
						o := a.PerInstall[in.ID()]
						o.Unattached = append(o.Unattached, UnattachedRoute{Route: OwnedObject{Kind: r.Kind, Obj: r.Obj}, Parent: p})
					}
				}
			}
		}
		for _, i := range order {
			if inScope(insts[i], r.Obj.GetNamespace()) {
				o := a.PerInstall[insts[i].ID()]
				o.Routes = append(o.Routes, OwnedObject{Kind: r.Kind, Obj: r.Obj, Parents: parents[i]})
			} else {
				a.Unwatched = append(a.Unwatched, UnwatchedRoute{Install: insts[i].ID(), Route: objects.RefOf(r.Obj), Gateway: gateway[i]})
			}
		}
	}

	// Policies.
	for _, p := range list(kinds.CategoryPolicy) {
		for _, in := range insts {
			if !inScope(in, p.Obj.GetNamespace()) {
				continue
			}
			if p.Kind.Group != kinds.KgatewayGroup && !claimable(p.Obj, in.ControllerNames) {
				continue
			}
			o := a.PerInstall[in.ID()]
			o.Policies = append(o.Policies, p)
		}
	}
	return a
}

// claimable reports whether a policy outside the kgateway group belongs to an
// install with controllers: it has an ancestor from one of them, or no
// ancestors at all yet, so it is new and not yet processed (finding 10).
func claimable(u *unstructured.Unstructured, controllers []string) bool {
	as := objects.PolicyAncestors(u)
	if len(as) == 0 {
		return true
	}
	for _, a := range as {
		if slices.Contains(controllers, a.ControllerName) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./pkg/install/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/install && git commit -m "feat(install): assign gateways, routes and policies to installs"
```

---

### Task 8: Controller metrics: parsing, scraping and collection

**Files:**
- Create: `pkg/ctrlmetrics/parse.go`, `pkg/ctrlmetrics/scrape.go`
- Create: `pkg/ctrlmetrics/testdata/v2.4.7.txt`, `pkg/ctrlmetrics/testdata/v2.3.11.txt` (real controller `/metrics` output with an induced reject, captured in Task 1 Step 4; the captured lines for both metric families are kept verbatim, plus at least 20 other lines)
- Test: `pkg/ctrlmetrics/parse_test.go`, `pkg/ctrlmetrics/scrape_test.go`

**Interfaces:**
- Consumes: `install.Install` (Pods, MetricsPort).
- Produces:
  - `ctrlmetrics.XDSRejectsActive = "kgateway_envoy_xds_rejects_active"`, `ctrlmetrics.ResourcesManaged = "kgateway_resources_managed"`;
  - `ctrlmetrics.GatewayKey{Namespace, Name string}`, `ctrlmetrics.ManagedKey{Namespace, Resource string}`;
  - `ctrlmetrics.Metrics{Rejects map[GatewayKey]float64; Managed map[ManagedKey]float64; ManagedPresent bool}`;
  - `ctrlmetrics.Parse(r io.Reader) (Metrics, error)`;
  - `ctrlmetrics.Aggregate(replicas []Metrics) Metrics` (rejects summed; managed max per key);
  - `ctrlmetrics.Scraper` (interface: `Scrape(ctx context.Context, pod *corev1.Pod, port int32) (io.ReadCloser, error)`);
  - `ctrlmetrics.HTTPScraper{Client *http.Client}`, `ctrlmetrics.PodProxyScraper{Kube kubernetes.Interface}`;
  - `ctrlmetrics.Result{Metrics Metrics; CollectedAt time.Time; Err string}` with `Stale(now time.Time, maxAge time.Duration) bool` (collected more than `maxAge` before `now`; a zero `maxAge` never makes a result stale; finding 3);
  - `ctrlmetrics.Collector`, `ctrlmetrics.NewCollector(s Scraper) *Collector`, `(*Collector).Collect(ctx, in install.Install) Result`, `(*Collector).Run(ctx, every time.Duration, installs func() []install.Install)`, `(*Collector).Get(id string) (Result, bool)`;
  - `ctrlmetrics.Source` (interface: `Get(id string) (Result, bool)`).

- [ ] **Step 1: Write the failing tests**

`pkg/ctrlmetrics/parse_test.go`:

```go
package ctrlmetrics

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// type_url values carry no "type.googleapis.com/" prefix: the controller strips
// it (envoy_error.go on v2.3.11, v2.4.7 and main).
const sample = `# HELP kgateway_envoy_xds_rejects_active Number of xDS responses currently rejected by envoy proxy
# TYPE kgateway_envoy_xds_rejects_active gauge
kgateway_envoy_xds_rejects_active{gateway_name="edge",gateway_namespace="infra",type_url="envoy.config.listener.v3.Listener"} 1
kgateway_envoy_xds_rejects_active{gateway_name="edge",gateway_namespace="infra",type_url="envoy.config.route.v3.RouteConfiguration"} 1
kgateway_envoy_xds_rejects_active{gateway_name="ok",gateway_namespace="infra",type_url="envoy.config.listener.v3.Listener"} 0
# HELP kgateway_resources_managed Current number of resources managed
# TYPE kgateway_resources_managed gauge
kgateway_resources_managed{namespace="shop",parent="edge",resource="HTTPRoute"} 3
kgateway_resources_managed{namespace="shop",parent="internal",resource="HTTPRoute"} 2
kgateway_resources_managed{namespace="shop",parent="",resource="TrafficPolicy"} 1
`

func TestParseSumsByKey(t *testing.T) {
	m, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if m.Rejects[GatewayKey{"infra", "edge"}] != 2 || m.Rejects[GatewayKey{"infra", "ok"}] != 0 {
		t.Fatalf("rejects: %+v", m.Rejects)
	}
	if !m.ManagedPresent || m.Managed[ManagedKey{"shop", "HTTPRoute"}] != 5 || m.Managed[ManagedKey{"shop", "TrafficPolicy"}] != 1 {
		t.Fatalf("managed: %+v", m.Managed)
	}
}

func TestAbsentFamiliesMeanZeroNotError(t *testing.T) {
	m, err := Parse(strings.NewReader("# TYPE go_goroutines gauge\ngo_goroutines 12\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Rejects) != 0 || m.ManagedPresent {
		t.Fatalf("absent families must parse as empty: %+v", m)
	}
}

func TestAggregateSumsRejectsAndMaxesManaged(t *testing.T) {
	a := Metrics{Rejects: map[GatewayKey]float64{{"infra", "edge"}: 1}, Managed: map[ManagedKey]float64{{"shop", "HTTPRoute"}: 5}, ManagedPresent: true}
	b := Metrics{Rejects: map[GatewayKey]float64{{"infra", "edge"}: 1}, Managed: map[ManagedKey]float64{{"shop", "HTTPRoute"}: 5}, ManagedPresent: true}
	got := Aggregate([]Metrics{a, b})
	if got.Rejects[GatewayKey{"infra", "edge"}] != 2 || got.Managed[ManagedKey{"shop", "HTTPRoute"}] != 5 {
		t.Fatalf("got %+v", got)
	}
}

// labelNames returns the sorted label names of every series of family name.
func labelNames(t *testing.T, text, name string) [][]string {
	t.Helper()
	fams, err := expfmt.NewTextParser(model.UTF8Validation).TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for _, s := range fams[name].GetMetric() {
		var ls []string
		for _, l := range s.GetLabel() {
			ls = append(ls, l.GetName())
		}
		sort.Strings(ls)
		out = append(out, ls)
	}
	return out
}

// Contract test (finding 15): the metric names, labels and values the UI
// depends on, checked against real controller output for every supported version.
// The synthetic sample above is not evidence of the upstream contract.
func TestCapturedFixturesHonourTheContract(t *testing.T) {
	for _, f := range []string{"testdata/v2.4.7.txt", "testdata/v2.3.11.txt"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		m, err := Parse(strings.NewReader(string(b)))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !m.ManagedPresent {
			t.Errorf("%s: %s missing", f, ResourcesManaged)
		}
		for _, ls := range labelNames(t, string(b), ResourcesManaged) {
			if strings.Join(ls, ",") != "namespace,parent,resource" {
				t.Errorf("%s: %s labels %v, want namespace,parent,resource", f, ResourcesManaged, ls)
			}
		}
		gateways := 0.0
		for k, v := range m.Managed {
			if k.Resource == "Gateway" && k.Namespace != "" {
				gateways += v
			}
		}
		if gateways < 1 {
			t.Errorf("%s: the captured install manages a Gateway, but no resource=\"Gateway\" series counts it: %+v", f, m.Managed)
		}
		// Each capture holds the reject induced in Task 1 Step 4 (round 2, finding 11):
		// the CDS NACK of app-a/gw. A capture without it fails, so the test never passes vacuously.
		rejects := labelNames(t, string(b), XDSRejectsActive)
		if len(rejects) == 0 {
			t.Fatalf("%s: no %s series; recapture with the induced reject (Task 1 Step 4)", f, XDSRejectsActive)
		}
		for _, ls := range rejects {
			if strings.Join(ls, ",") != "gateway_name,gateway_namespace,type_url" {
				t.Errorf("%s: %s labels %v", f, XDSRejectsActive, ls)
			}
		}
		if got := m.Rejects[GatewayKey{Namespace: "app-a", Name: "gw"}]; got != 1 {
			t.Errorf("%s: parsed rejects for app-a/gw = %v, want 1", f, got)
		}
		if !strings.Contains(string(b), `type_url="envoy.config.cluster.v3.Cluster"`) {
			t.Errorf("%s: the cipher-suite reject is a CDS NACK; type_url must be envoy.config.cluster.v3.Cluster without the type.googleapis.com/ prefix", f)
		}
	}
}
```

`pkg/ctrlmetrics/scrape_test.go`:

```go
package ctrlmetrics

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
)

type fakeScraper map[string]string // pod name → body; missing → error

func (f fakeScraper) Scrape(_ context.Context, p *corev1.Pod, _ int32) (io.ReadCloser, error) {
	b, ok := f[p.Name]
	if !ok {
		return nil, errors.New("connection refused")
	}
	return io.NopCloser(strings.NewReader(b)), nil
}

func inst(pods ...*corev1.Pod) install.Install {
	return install.Install{Namespace: "kgw", Name: "kgateway", Pods: pods, MetricsPort: 9092}
}

func TestCollectSumsOverReadyReplicas(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "kgateway:v2.4.7", 2, 2)
	p1, p2 := testutil.ControllerPod(d, "a", true), testutil.ControllerPod(d, "b", true)
	c := NewCollector(fakeScraper{"a": sample, "b": sample})
	r := c.Collect(context.Background(), inst(p1, p2))
	if r.Err != "" || r.Metrics.Rejects[GatewayKey{"infra", "edge"}] != 4 {
		t.Fatalf("got %+v", r)
	}
}

func TestCollectFailsWhenAnyReadyReplicaFails(t *testing.T) {
	// A missing replica may hold the rejects: an incomplete sum must not look like "no rejects".
	d := testutil.ControllerDeployment("kgw", "kgateway", "kgateway:v2.4.7", 2, 2)
	r := NewCollector(fakeScraper{"a": sample}).Collect(context.Background(), inst(testutil.ControllerPod(d, "a", true), testutil.ControllerPod(d, "b", true)))
	if r.Err == "" {
		t.Fatal("partial scrape must be an error")
	}
}

func TestCollectNoReadyPodsIsError(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "kgateway:v2.4.7", 1, 0)
	if r := NewCollector(fakeScraper{}).Collect(context.Background(), inst(testutil.ControllerPod(d, "a", false))); r.Err == "" {
		t.Fatal("no ready controller pod must be an error")
	}
}

// Finding 3: a result keeps its CollectedAt, so recomputing the model cannot make it look fresh.
func TestResultStaleness(t *testing.T) {
	r := Result{CollectedAt: testutil.Now}
	if r.Stale(testutil.Now.Add(30*time.Second), 30*time.Second) {
		t.Fatal("exactly maxAge old is not stale")
	}
	if !r.Stale(testutil.Now.Add(31*time.Second), 30*time.Second) {
		t.Fatal("older than maxAge must be stale")
	}
	if r.Stale(testutil.Now.Add(time.Hour), 0) {
		t.Fatal("a zero maxAge never makes a result stale")
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go get github.com/prometheus/common@latest && go test ./pkg/ctrlmetrics/...`
Expected: FAIL (`undefined: Parse`). The fixture test also fails until the files from Task 1 are added. Commit them with this task.

- [ ] **Step 3: Implement**

`pkg/ctrlmetrics/parse.go`:

```go
// Package ctrlmetrics scrapes and interprets the kgateway controller's
// Prometheus metrics. All metric names the UI depends on are declared here.
package ctrlmetrics

import (
	"io"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// Metric families the UI depends on (verified on kgateway v2.3.x, v2.4.x and main).
const (
	XDSRejectsActive = "kgateway_envoy_xds_rejects_active"
	ResourcesManaged = "kgateway_resources_managed"
)

// GatewayKey identifies a Gateway in metric labels.
type GatewayKey struct{ Namespace, Name string }

// ManagedKey identifies a resources_managed series after summing over "parent".
type ManagedKey struct{ Namespace, Resource string }

// Metrics is the interpreted subset of one or more controller scrapes.
type Metrics struct {
	// Rejects is the number of active xDS rejects per Gateway, summed over type_url.
	Rejects map[GatewayKey]float64
	// Managed is kgateway_resources_managed summed over the parent label.
	Managed map[ManagedKey]float64
	// ManagedPresent is true if the family appeared in the scrape. An absent
	// family with resources in scope suggests a rename upstream.
	ManagedPresent bool
}

// Parse interprets a text-format /metrics payload. A gauge that has never been
// set has no series, so an absent family means zero, not an error.
func Parse(r io.Reader) (Metrics, error) {
	m := Metrics{Rejects: map[GatewayKey]float64{}, Managed: map[ManagedKey]float64{}}
	p := expfmt.NewTextParser(model.UTF8Validation)
	fams, err := p.TextToMetricFamilies(r)
	if err != nil {
		return m, err
	}
	if f, ok := fams[XDSRejectsActive]; ok {
		for _, s := range f.GetMetric() {
			l := lbls(s)
			m.Rejects[GatewayKey{l["gateway_namespace"], l["gateway_name"]}] += s.GetGauge().GetValue()
		}
	}
	if f, ok := fams[ResourcesManaged]; ok {
		m.ManagedPresent = true
		for _, s := range f.GetMetric() {
			l := lbls(s)
			m.Managed[ManagedKey{l["namespace"], l["resource"]}] += s.GetGauge().GetValue()
		}
	}
	return m, nil
}

func lbls(s *dto.Metric) map[string]string {
	out := map[string]string{}
	for _, l := range s.GetLabel() {
		out[l.GetName()] = l.GetValue()
	}
	return out
}

// Aggregate combines per-replica scrapes. Rejects are summed, because each
// proxy's xDS stream is served by one replica. Managed counts are maxed,
// because every replica computes the full set (D-012).
func Aggregate(replicas []Metrics) Metrics {
	out := Metrics{Rejects: map[GatewayKey]float64{}, Managed: map[ManagedKey]float64{}}
	for _, r := range replicas {
		for k, v := range r.Rejects {
			out.Rejects[k] += v
		}
		for k, v := range r.Managed {
			out.Managed[k] = max(out.Managed[k], v)
		}
		out.ManagedPresent = out.ManagedPresent || r.ManagedPresent
	}
	return out
}
```

If `expfmt.NewTextParser` does not exist in the resolved `prometheus/common` version, use `var p expfmt.TextParser`. Keep a comment noting the version difference.

`pkg/ctrlmetrics/scrape.go`:

```go
package ctrlmetrics

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
)

// Scraper fetches the /metrics payload of one controller pod.
type Scraper interface {
	Scrape(ctx context.Context, pod *corev1.Pod, port int32) (io.ReadCloser, error)
}

// HTTPScraper scrapes the pod IP directly (in-cluster mode).
type HTTPScraper struct{ Client *http.Client }

// Scrape implements Scraper.
func (s HTTPScraper) Scrape(ctx context.Context, pod *corev1.Pod, port int32) (io.ReadCloser, error) {
	url := "http://" + net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(int(port))) + "/metrics"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/plain;version=0.0.4")
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

// PodProxyScraper scrapes through the API server's pods/proxy subresource
// (local mode; uses the user's kubeconfig rights).
type PodProxyScraper struct{ Kube kubernetes.Interface }

// Scrape implements Scraper.
func (s PodProxyScraper) Scrape(ctx context.Context, pod *corev1.Pod, port int32) (io.ReadCloser, error) {
	return s.Kube.CoreV1().Pods(pod.Namespace).ProxyGet("http", pod.Name, strconv.Itoa(int(port)), "/metrics", nil).Stream(ctx)
}

// Result is the outcome of collecting one install's metrics.
type Result struct {
	Metrics     Metrics
	CollectedAt time.Time
	// Err is non-empty when the metrics could not be collected completely.
	Err string
}

// Stale reports whether r was collected more than maxAge before now. A zero
// maxAge never makes a result stale. Consumers use 3 × the metrics interval,
// so a stalled collector cannot keep an old zero-reject result green (finding 3).
func (r Result) Stale(now time.Time, maxAge time.Duration) bool {
	return maxAge > 0 && now.Sub(r.CollectedAt) > maxAge
}

// Source returns the latest Result per install ID.
type Source interface {
	Get(id string) (Result, bool)
}

// Collector periodically scrapes every install's ready controller pods.
type Collector struct {
	scraper Scraper
	now     func() time.Time
	mu      sync.RWMutex
	results map[string]Result
}

// NewCollector returns a Collector using s.
func NewCollector(s Scraper) *Collector {
	return &Collector{scraper: s, now: time.Now, results: map[string]Result{}}
}

// Collect scrapes all ready pods of in and aggregates them. It fails if any
// ready pod cannot be scraped: a partial sum of rejects must not look like zero.
func (c *Collector) Collect(ctx context.Context, in install.Install) Result {
	var per []Metrics
	for _, p := range in.Pods {
		if !ready(p) {
			continue
		}
		body, err := c.scraper.Scrape(ctx, p, in.MetricsPort)
		if err != nil {
			return Result{CollectedAt: c.now(), Err: fmt.Sprintf("pod %s: %v", p.Name, err)}
		}
		m, err := Parse(body)
		body.Close()
		if err != nil {
			return Result{CollectedAt: c.now(), Err: fmt.Sprintf("pod %s: parse: %v", p.Name, err)}
		}
		per = append(per, m)
	}
	if len(per) == 0 {
		return Result{CollectedAt: c.now(), Err: "no ready controller pod to scrape"}
	}
	return Result{Metrics: Aggregate(per), CollectedAt: c.now()}
}

func ready(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// Run collects every interval until ctx is done.
func (c *Collector) Run(ctx context.Context, every time.Duration, installs func() []install.Install) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		for _, in := range installs() {
			sctx, cancel := context.WithTimeout(ctx, every)
			r := c.Collect(sctx, in)
			cancel()
			c.mu.Lock()
			c.results[in.ID()] = r
			c.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Get implements Source.
func (c *Collector) Get(id string) (Result, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.results[id]
	return r, ok
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./pkg/ctrlmetrics/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/ctrlmetrics go.mod go.sum && git commit -m "feat(ctrlmetrics): scrape and interpret controller metrics"
```

---

### Task 9: Health: findings, checks, scope verifier, roll-up

**Files:**
- Create: `pkg/health/finding.go`, `pkg/health/messages.go`, `pkg/health/checks.go`, `pkg/health/rollup.go`
- Create: `pkg/scopeverify/verify.go`
- Test: `pkg/health/checks_test.go`, `pkg/health/rollup_test.go`, `pkg/scopeverify/verify_test.go`

**Interfaces:**
- Consumes: `install.Resolved`, `install.Owned` (including `OwnedObject.Parents` and `Unattached`), `install.Assignment`, `cache.Snapshot`, `ctrlmetrics.Result` (including `Stale`), `objects.*`.
- Produces:
  - `health.Severity` (`SeverityError`, `SeverityWarning`, `SeverityInfo`);
  - `health.State` (`StateHealthy`, `StateWarning`, `StateError`, `StateUnknown`);
  - `health.Finding{Code, Slug string; Severity Severity; Install string; Resource *objects.Ref; Message, Detail string; Since *time.Time; Restricted bool}` (`Restricted` is set only by the API's `Authorizer` redaction, D-013; JSON `restricted,omitempty`);
  - `health.CheckResult{Name string; OK, Required bool; CollectedAt time.Time; Error string; Pending bool}` (`Pending`: not OK only because resources are being processed inside the grace period; JSON `pending,omitempty`);
  - `health.Input{Install install.Resolved; Owned *install.Owned; Assignment install.Assignment; Snapshot *cache.Snapshot; Metrics ctrlmetrics.Result; HasMetrics bool; MetricsMaxAge time.Duration; FirstSeen func(u *unstructured.Unstructured) time.Time; Verify scopeverify.Result; Now time.Time}` (`FirstSeen` comes from the engine; nil means the creation time, which the unit tests use);
  - `health.Check` (interface: `Name() string; Run(in Input) ([]Finding, CheckResult)`);
  - `health.Report{Install string; State State; Findings []Finding; Checks []CheckResult; Counts map[Severity]int; CollectedAt time.Time}` (`Install` is the install ID, so the frontend can reject a response for another install; finding 4);
  - `health.Evaluate(in Input, checks []Check) Report`;
  - `health.RollUp(fs []Finding, cs []CheckResult) State`;
  - `health.Upstream() []Check`;
  - `health.Grace = 60 * time.Second`;
  - the code constants `CodeXDSRejected` … `CodeAmbiguousOwnership` (`"KGWUI-001"` … `"KGWUI-009"`).
- Produces:
  - `scopeverify.Mismatch{Namespace, Resource string; UI, Controller float64; Since time.Time}`;
  - `scopeverify.Result{Available bool; Reason string; Mismatches []Mismatch}`;
  - `scopeverify.Verifier` (interface: `Name() string; Verify(in Input) Result`);
  - `scopeverify.Input{Install install.Resolved; Snapshot *cache.Snapshot; Served []kinds.Served; Metrics ctrlmetrics.Result; HasMetrics bool; MetricsMaxAge time.Duration; Now time.Time}`;
  - `scopeverify.NewManagedCountVerifier(persist time.Duration) *ManagedCountVerifier`.

**Condition rules (finding 1, D-006; round 2, findings 2–5).** Freshness is decided before status. For each status block, a missing *required* condition, a present condition with reason `Pending`, or *any* condition (`True` or `False`) with `observedGeneration < metadata.generation` (a missing observedGeneration, 0, counts as lower) makes the resource "not yet processed". Only current conditions are reported. The grace period runs from when the engine first saw the object's current (uid, generation): a generation bump restarts it, a label or annotation update does not, and objects already present at UI start get grace from that start (round 3, finding 3). Inside it, the resource gets KGWUI-003 at **info** ("Being processed by the controller") and the `resource status` check is not OK with `Pending: true`, so the install is unknown, never healthy. After it, the finding is a **warning** and the check is OK again. Required and optional types (optional ones are evaluated only when present):

| Status block | Required | Optional |
|---|---|---|
| GatewayClass | `Accepted` | — |
| Gateway | `Accepted`, `Programmed` | — |
| Gateway listener (one block per `spec.listeners` name; a configured listener with no status block is not processed) | `Accepted`, `Programmed` | `ResolvedRefs` (plus `Conflicted=True` is an error) |
| Route parent (one per `OwnedObject.Parents` entry, matched to the `status.parents` entry with the identical parentRef, section name and port included, and the install's controller name; finding 5) | `Accepted`, `ResolvedRefs` | `kgateway.dev/Programmed` |
| Policy ancestor (entries with the install's controller name) | `Accepted` | `Attached` |

A route parent without a matching status entry, a policy with no ancestor from the install's controller, and an `Unattached` route are "not yet processed" too. KGWUI-003 is deduplicated by (resource, parentRef): one per route parent (owned and pending, or missing), and one per non-route resource (round 3, finding 5).

**Proposed refinement of design §5.2 (needs user approval with this plan):** the policy reason `Overridden` (on `Attached`) maps to **info**, not warning. Overriding is the normal outcome of hierarchical policy; at scale it would flood the warning list. `PartiallyValid` stays a warning. If the user rejects this, change the single entry in `policySeverity` and its test row.

- [ ] **Step 1: Write the failing tests**

`pkg/health/rollup_test.go`:

```go
package health

import "testing"

func TestRollUpOrder(t *testing.T) {
	warn := Finding{Severity: SeverityWarning}
	errF := Finding{Severity: SeverityError}
	info := Finding{Severity: SeverityInfo}
	missing := CheckResult{Name: "metrics", Required: true, OK: false}
	optional := CheckResult{Name: "verify", Required: false, OK: false}
	cases := []struct {
		name string
		fs   []Finding
		cs   []CheckResult
		want State
	}{
		{"nothing is healthy", nil, nil, StateHealthy},
		{"info only is healthy", []Finding{info}, nil, StateHealthy},
		{"warning", []Finding{warn}, nil, StateWarning},
		{"unknown beats warning", []Finding{warn}, []CheckResult{missing}, StateUnknown},
		{"error beats unknown", []Finding{errF}, []CheckResult{missing}, StateError},
		{"optional check failing is not unknown", nil, []CheckResult{optional}, StateHealthy},
	}
	for _, tc := range cases {
		if got := RollUp(tc.fs, tc.cs); got != tc.want {
			t.Errorf("%s: want %s got %s", tc.name, tc.want, got)
		}
	}
}
```

`pkg/health/checks_test.go`. Each case builds an Input, runs `Evaluate(in, Upstream())`, and asserts that a finding with the given code and severity is present for the given resource (or that it is absent):

```go
package health

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

const img = "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7"

type world struct {
	nss     []*corev1.Namespace
	objs    []*unstructured.Unstructured
	deploys []*appsv1.Deployment
	ctrl    *appsv1.Deployment
	pods    []*corev1.Pod
	scope   string
	metrics *ctrlmetrics.Result                          // nil = not collected yet
	access  map[schema.GroupVersionResource]cache.Access // overrides on top of "everything readable"
}

func healthyWorld() *world {
	d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, 1)
	gw := testutil.GwRef("infra", "edge")
	return &world{
		nss:     []*corev1.Namespace{testutil.Namespace("infra", nil), testutil.Namespace("shop", nil)},
		ctrl:    d,
		deploys: []*appsv1.Deployment{d, testutil.ProxyDeployment("infra", "edge", 1, 1)},
		pods:    []*corev1.Pod{testutil.ControllerPod(d, "p", true)},
		scope:   "[]",
		objs: []*unstructured.Unstructured{
			testutil.GatewayClass("kgateway", testutil.Controller, true),
			testutil.Gateway("infra", "edge", "kgateway", nil),
			testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		},
		metrics: &ctrlmetrics.Result{CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{}, Managed: map[ctrlmetrics.ManagedKey]float64{}}},
	}
}

func (w *world) report(t *testing.T) Report {
	t.Helper()
	snap := &cache.Snapshot{Objects: map[schema.GroupVersionResource][]*unstructured.Unstructured{}, Access: map[schema.GroupVersionResource]cache.Access{},
		Namespaces: w.nss, Deployments: w.deploys, Pods: w.pods, Synced: true, TakenAt: testutil.Now}
	served := testutil.ServedUpstream()
	for _, k := range served {
		snap.Access[k.GVR()] = cache.Access{OK: true}
	}
	for _, gvr := range []schema.GroupVersionResource{cache.NamespacesGVR, cache.PodsGVR, cache.DeploymentsGVR} {
		snap.Access[gvr] = cache.Access{OK: true}
	}
	for gvr, a := range w.access {
		snap.Access[gvr] = a
	}
	for _, o := range w.objs {
		gv, _ := schema.ParseGroupVersion(o.GetAPIVersion())
		for _, k := range served {
			if k.Group == gv.Group && k.Kind == o.GetKind() {
				snap.Objects[k.GVR()] = append(snap.Objects[k.GVR()], o)
			}
		}
	}
	in, _ := install.UpstreamDetector{}.Detect(w.ctrl)
	in.Pods = w.pods
	res := install.Resolved{Install: in, Scope: install.ParseSelectors(w.scope)}
	a := install.Assign(snap, served, []install.Resolved{res})
	hi := Input{Install: res, Owned: a.PerInstall[res.ID()], Assignment: a, Snapshot: snap, Now: testutil.Now,
		MetricsMaxAge: 30 * time.Second, Verify: scopeverify.Result{Available: true}}
	if w.metrics != nil {
		hi.Metrics, hi.HasMetrics = *w.metrics, true
	}
	return Evaluate(hi, Upstream())
}

// has reports whether r has a finding with code on res; an empty sev or res matches any.
func has(r Report, code string, sev Severity, res string) bool {
	for _, f := range r.Findings {
		if f.Code == code && (sev == "" || f.Severity == sev) && (res == "" || (f.Resource != nil && f.Resource.String() == res)) {
			return true
		}
	}
	return false
}

func TestHealthyWorldIsHealthy(t *testing.T) {
	r := healthyWorld().report(t)
	if r.State != StateHealthy || len(r.Findings) != 0 {
		t.Fatalf("state=%s findings=%+v", r.State, r.Findings)
	}
	for _, c := range r.Checks {
		if c.Required && !c.OK {
			t.Fatalf("check %s not OK: %s", c.Name, c.Error)
		}
	}
}

func TestChecks(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	cases := []struct {
		name   string
		mutate func(w *world)
		code   string
		sev    Severity
		res    string
		state  State
	}{
		{"001 xds rejected", func(w *world) {
			w.metrics.Metrics.Rejects[ctrlmetrics.GatewayKey{Namespace: "infra", Name: "edge"}] = 1
		}, CodeXDSRejected, SeverityError, "infra/edge", StateError},
		{"002 gateway not programmed", func(w *world) {
			w.objs[1] = testutil.Gateway("infra", "edge", "kgateway", []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionFalse, "AddressNotAssigned")})
		}, CodeConditionFalse, SeverityError, "infra/edge", StateError},
		{"002 route backend not found", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
		}, CodeConditionFalse, SeverityError, "shop/cart", StateError},
		{"002 rule replaced (2.4, optional condition present)", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs"),
				testutil.Cond("kgateway.dev/Programmed", metav1.ConditionFalse, "RouteRuleReplaced")}})
		}, CodeConditionFalse, SeverityError, "shop/cart", StateError},
		{"002 rule replaced on 2.3 (Accepted=False)", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionFalse, "RouteRuleReplaced"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}})
		}, CodeConditionFalse, SeverityError, "shop/cart", StateError},
		// Main only: v2.3.x and v2.4.x write no status for a missing target (see "003 policy without ancestors").
		{"002 policy target not found (main)", func(w *world) {
			w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{
				Ref: objects.ParentRef{Group: "gateway.kgateway.dev", Kind: "StatusSummary", Name: "summary"}, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "TargetNotFound")}}))
		}, CodeConditionFalse, SeverityError, "shop/tp", StateError},
		{"002 policy partially valid", func(w *world) {
			w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "PartiallyValid")}}))
		}, CodeConditionFalse, SeverityWarning, "shop/tp", StateWarning},
		{"002 generic policy rejected", func(w *world) {
			w.objs = append(w.objs, testutil.BackendTLSPolicy("shop", "btls", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "Invalid")}}))
		}, CodeConditionFalse, SeverityError, "shop/btls", StateError},
		{"002 policy overridden is info", func(w *world) {
			w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Valid"), testutil.Cond("Attached", metav1.ConditionFalse, "Overridden")}}))
		}, CodeConditionFalse, SeverityInfo, "shop/tp", StateHealthy},
		{"003 route not processed", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw})
		}, CodeRouteUnattached, SeverityWarning, "shop/cart", StateWarning},
		{"003 required condition absent", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")}})
		}, CodeRouteUnattached, SeverityWarning, "shop/cart", StateWarning},
		{"003 policy without ancestors", func(w *world) {
			w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp"))
		}, CodeRouteUnattached, SeverityWarning, "shop/tp", StateWarning},
		{"003 generic policy without ancestors", func(w *world) {
			w.objs = append(w.objs, testutil.BackendTLSPolicy("shop", "btls"))
		}, CodeRouteUnattached, SeverityWarning, "shop/btls", StateWarning},
		{"003 parent gateway does not exist", func(w *world) {
			w.objs = append(w.objs, testutil.HTTPRoute("shop", "orphan", []objects.ParentRef{testutil.GwRef("infra", "gone")}))
		}, CodeRouteUnattached, SeverityWarning, "shop/orphan", StateWarning},
		{"004 namespace not watched", func(w *world) {
			w.scope = `[{"matchLabels":{"team":"a"}}]`
			w.nss = []*corev1.Namespace{testutil.Namespace("infra", map[string]string{"team": "a"}), testutil.Namespace("shop", nil)}
		}, CodeNamespaceNotWatched, SeverityWarning, "shop/cart", StateWarning},
		{"005 controller not ready", func(w *world) {
			w.ctrl.Status.ReadyReplicas = 0
		}, CodeControllerNotReady, SeverityError, "kgw/kgateway", StateError},
		{"006 proxy not ready", func(w *world) {
			w.deploys[1] = testutil.ProxyDeployment("infra", "edge", 2, 1)
		}, CodeProxyNotReady, SeverityError, "infra/edge", StateError},
		{"008 metrics unavailable forces unknown", func(w *world) {
			w.metrics = &ctrlmetrics.Result{CollectedAt: testutil.Now, Err: "connection refused"}
		}, CodeSignalUnavailable, SeverityInfo, "", StateUnknown},
		{"008 metrics not collected yet forces unknown", func(w *world) { w.metrics = nil }, CodeSignalUnavailable, SeverityInfo, "", StateUnknown},
		{"008 stale metrics force unknown", func(w *world) {
			w.metrics.CollectedAt = testutil.Now.Add(-5 * time.Minute) // older than MetricsMaxAge (30 s)
		}, CodeSignalUnavailable, SeverityInfo, "", StateUnknown},
		{"008 namespaces not readable forces unknown", func(w *world) {
			w.access = map[schema.GroupVersionResource]cache.Access{cache.NamespacesGVR: {Err: "connection refused"}}
		}, CodeSignalUnavailable, SeverityInfo, "", StateUnknown},
		{"008 scope unknown forces unknown", func(w *world) { w.scope = "[{" }, CodeSignalUnavailable, SeverityInfo, "", StateUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := healthyWorld()
			tc.mutate(w)
			r := w.report(t)
			if !has(r, tc.code, tc.sev, tc.res) {
				t.Fatalf("missing %s/%s on %q; findings: %+v", tc.code, tc.sev, tc.res, r.Findings)
			}
			if r.State != tc.state {
				t.Fatalf("state: want %s got %s", tc.state, r.State)
			}
		})
	}
}

// Review Focus 3: a crash-looping controller is an error and metrics are unknown; never healthy.
func TestCrashLoopingController(t *testing.T) {
	w := healthyWorld()
	w.ctrl.Status.ReadyReplicas = 0
	w.pods = []*corev1.Pod{testutil.ControllerPod(w.ctrl, "p", false)}
	w.metrics = &ctrlmetrics.Result{CollectedAt: testutil.Now, Err: "no ready controller pod to scrape"}
	r := w.report(t)
	if r.State != StateError || !has(r, CodeControllerNotReady, SeverityError, "") || !has(r, CodeSignalUnavailable, SeverityInfo, "") {
		t.Fatalf("state=%s findings=%+v", r.State, r.Findings)
	}
}

// Review Focus 4: stale conditions describe an older spec, whether False or True.
func TestStaleConditionIsPendingNotError(t *testing.T) {
	w := healthyWorld()
	gw := testutil.GwRef("infra", "edge")
	route := testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}})
	route.SetGeneration(2) // conditions observed generation 1
	w.objs[2] = route
	r := w.report(t)
	if has(r, CodeConditionFalse, SeverityError, "shop/cart") {
		t.Fatal("a stale False condition must not be reported as a current error")
	}
	if !has(r, CodeRouteUnattached, SeverityWarning, "shop/cart") {
		t.Fatalf("a route stale for longer than the grace period must be 'not yet processed': %+v", r.Findings)
	}
}

// Finding 1: a stale True condition must not count as a current success.
func TestStaleTrueIsPendingNotHealthy(t *testing.T) {
	w := healthyWorld()
	gw := testutil.GwRef("infra", "edge")
	route := testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller))
	route.SetGeneration(2) // AcceptedBy's conditions observed generation 1
	w.objs[2] = route
	r := w.report(t)
	if r.State == StateHealthy || !has(r, CodeRouteUnattached, SeverityWarning, "shop/cart") {
		t.Fatalf("stale True must be 'not yet processed', got state=%s findings=%+v", r.State, r.Findings)
	}
	n := 0
	for _, f := range r.Findings {
		if f.Code == CodeRouteUnattached {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("one parent gets at most one KGWUI-003 finding, got %d", n)
	}
}

// Round 2, finding 2: inside the grace period an unprocessed resource is
// "being processed" (info), and the install is unknown, never healthy.
func TestResourceInGraceIsBeingProcessedAndNotHealthy(t *testing.T) {
	w := healthyWorld()
	young := testutil.HTTPRoute("shop", "young", []objects.ParentRef{testutil.GwRef("infra", "edge")})
	young.SetCreationTimestamp(metav1.NewTime(testutil.Now.Add(-10 * time.Second)))
	w.objs = append(w.objs, young)
	r := w.report(t)
	if !has(r, CodeRouteUnattached, SeverityInfo, "shop/young") || has(r, CodeRouteUnattached, SeverityWarning, "shop/young") {
		t.Fatalf("a route inside the grace period is 'being processed' at info: %+v", r.Findings)
	}
	if r.State != StateUnknown {
		t.Fatalf("state: want unknown while a resource is being processed, got %s", r.State)
	}
	for _, c := range r.Checks {
		if c.Name == "resource status" && (c.OK || !c.Pending || c.Error != "1 resource is being processed by the controller") {
			t.Fatalf("resource status check: %+v", c)
		}
	}
}

// Round 3, finding 5: KGWUI-003 is per (resource, parentRef). Two missing
// parents give two findings, and a pending owned parent does not hide a missing one.
func TestNotProcessedIsPerParent(t *testing.T) {
	w := healthyWorld()
	gw := testutil.GwRef("infra", "edge")
	w.objs = append(w.objs,
		testutil.HTTPRoute("shop", "two-missing", []objects.ParentRef{testutil.GwRef("infra", "gone-1"), testutil.GwRef("infra", "gone-2")}),
		testutil.HTTPRoute("shop", "mixed", []objects.ParentRef{gw, testutil.GwRef("infra", "gone-3")})) // no status for edge either
	r := w.report(t)
	per := map[string]int{}
	for _, f := range r.Findings {
		if f.Code == CodeRouteUnattached && f.Resource != nil {
			per[f.Resource.String()]++
		}
	}
	if per["shop/two-missing"] != 2 || per["shop/mixed"] != 2 {
		t.Fatalf("want one KGWUI-003 per parent: %v; findings %+v", per, r.Findings)
	}
}

// Round 2, findings 2–5: Pending, a missing listener block, a status entry for
// another port, and observedGeneration 0 are all "not yet processed", never healthy.
func TestUnprocessedEvidenceIsNeverHealthy(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	gw443 := gw
	gw443.Port = 443
	cases := []struct {
		name   string
		mutate func(w *world)
		res    string
	}{
		{"policy Accepted=False/Pending", func(w *world) {
			w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "Pending"), testutil.Cond("Attached", metav1.ConditionFalse, "Pending")}}))
		}, "shop/tp"},
		{"gateway Programmed=Unknown/Pending", func(w *world) {
			w.objs[1] = testutil.Gateway("infra", "edge", "kgateway", []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionUnknown, "Pending")})
		}, "infra/edge"},
		{"listener without a status block", func(w *world) {
			testutil.AddSpecListener(w.objs[1], "https")
		}, "infra/edge"},
		{"status only for another port", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw443}, testutil.AcceptedBy(gw, testutil.Controller))
		}, "shop/cart"},
		{"observedGeneration 0", func(w *world) {
			p := testutil.AcceptedBy(gw, testutil.Controller)
			for i := range p.Conds {
				p.Conds[i].ObservedGeneration = 0
			}
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, p)
		}, "shop/cart"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := healthyWorld()
			tc.mutate(w)
			r := w.report(t)
			if !has(r, CodeRouteUnattached, SeverityWarning, tc.res) || has(r, CodeConditionFalse, "", tc.res) || r.State != StateWarning {
				t.Fatalf("want only 'not yet processed' (warning) on %s: state=%s findings=%+v", tc.res, r.State, r.Findings)
			}
		})
	}
}

// Finding 5: only status entries for current parentRefs of this install count.
// An entry left behind for a removed parentRef is ignored.
func TestStatusForRemovedParentIsIgnored(t *testing.T) {
	w := healthyWorld()
	gw, old := testutil.GwRef("infra", "edge"), testutil.GwRef("infra", "old")
	w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller),
		testutil.RouteParent{Ref: old, Controller: testutil.Controller, Conds: []metav1.Condition{
			testutil.Cond("Accepted", metav1.ConditionFalse, "NoMatchingParent"), testutil.Cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}})
	if r := w.report(t); r.State != StateHealthy || len(r.Findings) != 0 {
		t.Fatalf("status for a removed parentRef must be ignored: state=%s findings=%+v", r.State, r.Findings)
	}
}

// Review Focus 1: a Gateway name over 63 characters has a truncated proxy label;
// the proxy is matched through its ownerReference.
func TestLongGatewayNameMatchesProxyByOwner(t *testing.T) {
	w := healthyWorld()
	long := "edge-a-very-long-gateway-name-that-exceeds-the-sixty-three-character-label-limit"
	w.objs[1] = testutil.Gateway("infra", long, "kgateway", nil)
	p := testutil.ProxyDeployment("infra", long, 1, 1)
	p.Labels["gateway.networking.k8s.io/gateway-name"] = long[:52] + "-0123456789"
	p.Name = long[:40]
	w.deploys[1] = p
	w.objs[2] = testutil.HTTPRoute("shop", "cart", nil)
	r := w.report(t)
	for _, f := range r.Findings {
		if f.Code == CodeProxyNotReady {
			t.Fatalf("proxy must be found via ownerReference: %+v", f)
		}
	}
}
```

`pkg/scopeverify/verify_test.go`:

```go
package scopeverify

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

func input(now time.Time, managed map[ctrlmetrics.ManagedKey]float64, present bool) Input {
	gw := testutil.GwRef("shop", "edge")
	objs := []*unstructured.Unstructured{
		testutil.Gateway("shop", "edge", "any-class", nil), // raw count: class is not considered
		testutil.HTTPRoute("shop", "two-parents", []objects.ParentRef{gw, testutil.GwRef("shop", "other")}),
		testutil.HTTPRoute("shop", "no-parents", nil),
		testutil.TrafficPolicy("shop", "tp"),
		testutil.HTTPRoute("blog", "out-of-scope", []objects.ParentRef{gw}),
	}
	snap := &cache.Snapshot{Objects: map[schema.GroupVersionResource][]*unstructured.Unstructured{}, Synced: true,
		Namespaces: []*corev1.Namespace{testutil.Namespace("shop", map[string]string{"team": "a"}), testutil.Namespace("blog", nil)}}
	for _, o := range objs {
		gv, _ := schema.ParseGroupVersion(o.GetAPIVersion())
		for _, k := range testutil.ServedUpstream() {
			if k.Group == gv.Group && k.Kind == o.GetKind() {
				snap.Objects[k.GVR()] = append(snap.Objects[k.GVR()], o)
			}
		}
	}
	return Input{Install: install.Resolved{Install: install.Install{Namespace: "kgw", Name: "kgateway"}, Scope: install.ParseSelectors(`[{"matchLabels":{"team":"a"}}]`)},
		Snapshot: snap, Served: testutil.ServedUpstream(), HasMetrics: true, Now: now,
		Metrics: ctrlmetrics.Result{CollectedAt: now, Metrics: ctrlmetrics.Metrics{Managed: managed, ManagedPresent: present}}}
}

// Weights mirror the gauge: Gateway 1, route 1 per parentRef, policy 1.
var matching = map[ctrlmetrics.ManagedKey]float64{{Namespace: "shop", Resource: "Gateway"}: 1, {Namespace: "shop", Resource: "HTTPRoute"}: 2, {Namespace: "shop", Resource: "TrafficPolicy"}: 1}

func TestMatchingCountsHaveNoMismatch(t *testing.T) {
	r := NewManagedCountVerifier(20 * time.Second).Verify(input(testutil.Now, matching, true))
	if !r.Available || len(r.Mismatches) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestMismatchMustPersist(t *testing.T) {
	v := NewManagedCountVerifier(20 * time.Second)
	off := map[ctrlmetrics.ManagedKey]float64{{Namespace: "shop", Resource: "Gateway"}: 1, {Namespace: "shop", Resource: "HTTPRoute"}: 3, {Namespace: "shop", Resource: "TrafficPolicy"}: 1}
	if r := v.Verify(input(testutil.Now, off, true)); len(r.Mismatches) != 0 {
		t.Fatal("a fresh mismatch must not be reported yet")
	}
	r := v.Verify(input(testutil.Now.Add(25*time.Second), off, true))
	if len(r.Mismatches) != 1 || r.Mismatches[0].UI != 2 || r.Mismatches[0].Controller != 3 {
		t.Fatalf("persistent mismatch not reported: %+v", r)
	}
	if r := v.Verify(input(testutil.Now.Add(30*time.Second), matching, true)); len(r.Mismatches) != 0 {
		t.Fatal("mismatch must clear when counts agree")
	}
}

func TestAbsentFamilyWithObjectsIsUnavailable(t *testing.T) {
	r := NewManagedCountVerifier(20 * time.Second).Verify(input(testutil.Now, nil, false))
	if r.Available || r.Reason == "" {
		t.Fatalf("%+v", r)
	}
}

// Finding 3: counts from an old scrape must not verify the current scope.
func TestStaleMetricsAreUnavailable(t *testing.T) {
	in := input(testutil.Now, matching, true)
	in.Metrics.CollectedAt = testutil.Now.Add(-5 * time.Minute)
	in.MetricsMaxAge = 30 * time.Second
	if r := NewManagedCountVerifier(20 * time.Second).Verify(in); r.Available {
		t.Fatalf("stale metrics must make the verifier unavailable: %+v", r)
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./pkg/health/... ./pkg/scopeverify/...`
Expected: FAIL (undefined symbols)

- [ ] **Step 3: Implement the scope verifier**

`pkg/scopeverify/verify.go`:

```go
// Package scopeverify checks that the UI's view of an install's scope matches
// what the controller itself reports (D-009 §3).
package scopeverify

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Input is what a Verifier needs for one install.
type Input struct {
	Install    install.Resolved
	Snapshot   *cache.Snapshot
	Served     []kinds.Served
	Metrics    ctrlmetrics.Result
	HasMetrics bool
	// MetricsMaxAge is the age beyond which Metrics is stale; zero means no limit.
	MetricsMaxAge time.Duration
	Now           time.Time
}

// Mismatch is a (namespace, resource) whose counts have disagreed since Since.
type Mismatch struct {
	Namespace  string    `json:"namespace"`
	Resource   string    `json:"resource"`
	UI         float64   `json:"ui"`
	Controller float64   `json:"controller"`
	Since      time.Time `json:"since"`
}

// Result is a verification outcome. Available=false means the verifier could
// not run; it never decides health on its own (design §4.4).
type Result struct {
	Available  bool
	Reason     string
	Mismatches []Mismatch
}

// Verifier compares the UI's scope with the controller's. Downstreams may register their own.
type Verifier interface {
	Name() string
	Verify(in Input) Result
}

// ManagedCountVerifier compares per-(namespace, resource) counts with
// kgateway_resources_managed, weighting objects exactly as the gauge does.
type ManagedCountVerifier struct {
	persist time.Duration
	mu      sync.Mutex
	first   map[string]time.Time // install|ns|resource → first seen mismatching
}

// NewManagedCountVerifier reports mismatches that persist for at least persist.
func NewManagedCountVerifier(persist time.Duration) *ManagedCountVerifier {
	return &ManagedCountVerifier{persist: persist, first: map[string]time.Time{}}
}

// Name implements Verifier.
func (*ManagedCountVerifier) Name() string { return "resources-managed" }

// Verify implements Verifier.
func (v *ManagedCountVerifier) Verify(in Input) Result {
	if !in.HasMetrics || in.Metrics.Err != "" {
		return Result{Reason: "controller metrics unavailable"}
	}
	if in.Metrics.Stale(in.Now, in.MetricsMaxAge) {
		return Result{Reason: "controller metrics are stale"}
	}
	ui := uiCounts(in)
	if !in.Metrics.Metrics.ManagedPresent {
		if len(ui) > 0 {
			return Result{Reason: ctrlmetrics.ResourcesManaged + " not found in controller metrics"}
		}
		return Result{Available: true}
	}
	keys := map[ctrlmetrics.ManagedKey]bool{}
	for k := range ui {
		keys[k] = true
	}
	for k := range in.Metrics.Metrics.Managed {
		if comparable(k.Resource, in.Served) {
			keys[k] = true
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	res := Result{Available: true}
	for k := range keys {
		id := fmt.Sprintf("%s|%s|%s", in.Install.ID(), k.Namespace, k.Resource)
		u, c := ui[k], in.Metrics.Metrics.Managed[k]
		if u == c {
			delete(v.first, id)
			continue
		}
		since, ok := v.first[id]
		if !ok {
			v.first[id] = in.Now
			continue
		}
		if in.Now.Sub(since) >= v.persist {
			res.Mismatches = append(res.Mismatches, Mismatch{Namespace: k.Namespace, Resource: k.Resource, UI: u, Controller: c, Since: since})
		}
	}
	sort.Slice(res.Mismatches, func(i, j int) bool {
		return res.Mismatches[i].Namespace+res.Mismatches[i].Resource < res.Mismatches[j].Namespace+res.Mismatches[j].Resource
	})
	return res
}

// resourceLabel maps a served kind to the gauge's "resource" label; "" means
// the gauge does not count this kind.
func resourceLabel(k kinds.Served) string {
	switch {
	case k.Category == kinds.CategoryGateway, k.Category == kinds.CategoryRoute:
		return k.Kind
	case k.Category == kinds.CategoryListenerSet:
		return "XListenerSet" // both ListenerSet flavours are reported under this value
	case k.Category == kinds.CategoryPolicy && k.Group == kinds.KgatewayGroup:
		return k.Kind
	}
	return ""
}

func comparable(resource string, served []kinds.Served) bool {
	for _, k := range served {
		if resourceLabel(k) == resource {
			return true
		}
	}
	return false
}

func uiCounts(in Input) map[ctrlmetrics.ManagedKey]float64 {
	nsByName := map[string]bool{}
	for _, n := range in.Snapshot.Namespaces {
		nsByName[n.Name] = in.Install.Scope.Contains(n.Name, n)
	}
	out := map[ctrlmetrics.ManagedKey]float64{}
	for _, k := range in.Served {
		label := resourceLabel(k)
		if label == "" {
			continue
		}
		for _, o := range in.Snapshot.Objects[k.GVR()] {
			if !nsByName[o.GetNamespace()] {
				continue
			}
			w := 1.0
			if k.Category == kinds.CategoryRoute {
				w = float64(len(objects.RouteParentRefs(o)))
			}
			if w > 0 {
				out[ctrlmetrics.ManagedKey{Namespace: o.GetNamespace(), Resource: label}] += w
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Implement findings, messages and the roll-up**

`pkg/health/finding.go`:

```go
// Package health turns cluster state and controller signals into findings and
// a health state per install (design §5).
package health

import (
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

// Severity of a finding.
type Severity string

// Severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// State is the rolled-up health of an install.
type State string

// States, from worst to best: error, unknown, warning, healthy.
const (
	StateError   State = "error"
	StateUnknown State = "unknown"
	StateWarning State = "warning"
	StateHealthy State = "healthy"
)

// Stable finding codes. Never reuse a code (design §5.1).
const (
	CodeXDSRejected         = "KGWUI-001"
	CodeConditionFalse      = "KGWUI-002"
	CodeRouteUnattached     = "KGWUI-003"
	CodeNamespaceNotWatched = "KGWUI-004"
	CodeControllerNotReady  = "KGWUI-005"
	CodeProxyNotReady       = "KGWUI-006"
	CodeScopeMismatch       = "KGWUI-007"
	CodeSignalUnavailable   = "KGWUI-008"
	CodeAmbiguousOwnership  = "KGWUI-009"
)

// Grace is how long a resource may lack current status before it is reported
// as not yet processed.
const Grace = 60 * time.Second

// Finding is one problem or notable fact about an install.
type Finding struct {
	Code     string       `json:"code"`
	Slug     string       `json:"slug"`
	Severity Severity     `json:"severity"`
	Install  string       `json:"install"`
	Resource *objects.Ref `json:"resource,omitempty"`
	Message  string       `json:"message"`
	Detail   string       `json:"detail,omitempty"`
	Since    *time.Time   `json:"since,omitempty"`
	// Restricted marks a finding on a resource the caller may not view. The
	// API keeps its code, severity and resource and empties Message, Detail
	// and Since (D-013). Checks never set it.
	Restricted bool `json:"restricted,omitempty"`
}

// CheckResult records whether a check could collect current input. A Required
// check that is not OK forces the state to unknown.
type CheckResult struct {
	Name        string    `json:"name"`
	OK          bool      `json:"ok"`
	Required    bool      `json:"required"`
	CollectedAt time.Time `json:"collectedAt"`
	Error       string    `json:"error,omitempty"`
	// Pending marks a check that is not OK only because resources are still
	// being processed inside the grace period (round 2, finding 2).
	Pending bool `json:"pending,omitempty"`
}

// Input is everything a check may look at for one install.
type Input struct {
	Install    install.Resolved
	Owned      *install.Owned
	Assignment install.Assignment
	Snapshot   *cache.Snapshot
	Metrics    ctrlmetrics.Result
	HasMetrics bool
	// MetricsMaxAge is the age beyond which Metrics is stale (3 × the metrics
	// interval); zero means no limit.
	MetricsMaxAge time.Duration
	// FirstSeen returns when the engine first saw u's current generation; the
	// grace period runs from it (round 3, finding 3). Nil means u's creation time.
	FirstSeen func(u *unstructured.Unstructured) time.Time
	Verify    scopeverify.Result
	Now       time.Time
}

// firstSeen returns when the grace period for u's current generation started.
func (in Input) firstSeen(u *unstructured.Unstructured) time.Time {
	if in.FirstSeen != nil {
		return in.FirstSeen(u)
	}
	return u.GetCreationTimestamp().Time
}

// Check produces findings for one concern.
type Check interface {
	Name() string
	Run(in Input) ([]Finding, CheckResult)
}

// Report is the evaluated health of one install.
type Report struct {
	// Install is the install ID, so a client can reject a response for another install.
	Install     string           `json:"install"`
	State       State            `json:"state"`
	Findings    []Finding        `json:"findings"`
	Checks      []CheckResult    `json:"checks"`
	Counts      map[Severity]int `json:"counts"`
	CollectedAt time.Time        `json:"collectedAt"`
}
```

`pkg/health/rollup.go`:

```go
package health

import "sort"

var sevRank = map[Severity]int{SeverityError: 0, SeverityWarning: 1, SeverityInfo: 2}

// RollUp computes the state: error > unknown > warning > healthy (design §5.3).
func RollUp(fs []Finding, cs []CheckResult) State {
	for _, f := range fs {
		if f.Severity == SeverityError {
			return StateError
		}
	}
	for _, c := range cs {
		if c.Required && !c.OK {
			return StateUnknown
		}
	}
	for _, f := range fs {
		if f.Severity == SeverityWarning {
			return StateWarning
		}
	}
	return StateHealthy
}

// Evaluate runs checks and assembles a Report with findings sorted by
// severity, then code, then resource.
func Evaluate(in Input, checks []Check) Report {
	r := Report{Install: in.Install.ID(), Counts: map[Severity]int{}, CollectedAt: in.Now, Findings: []Finding{}}
	for _, c := range checks {
		fs, cr := c.Run(in)
		r.Findings = append(r.Findings, fs...)
		r.Checks = append(r.Checks, cr)
	}
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if sevRank[a.Severity] != sevRank[b.Severity] {
			return sevRank[a.Severity] < sevRank[b.Severity]
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return refKey(a) < refKey(b)
	})
	for _, f := range r.Findings {
		r.Counts[f.Severity]++
	}
	r.State = RollUp(r.Findings, r.Checks)
	return r
}

func refKey(f Finding) string {
	if f.Resource == nil {
		return ""
	}
	return f.Resource.Kind + "/" + f.Resource.String()
}
```

`pkg/health/messages.go`:

```go
package health

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// reasonMessages turns condition reasons into operator language.
var reasonMessages = map[string]string{
	"BackendNotFound":            "Backend not found",
	"RefNotPermitted":            "Reference not permitted (missing ReferenceGrant)",
	"InvalidKind":                "Unsupported backend kind",
	"NoMatchingParent":           "No matching listener on the parent",
	"NotAllowedByListeners":      "Not allowed by the Gateway's listeners",
	"NoMatchingListenerHostname": "No listener hostname matches",
	"UnsupportedValue":           "Unsupported value in the route",
	"RouteRuleDropped":           "Rule dropped because of invalid configuration",
	"RouteRuleReplaced":          "Rule replaced by a direct response",
	"Invalid":                    "Invalid configuration",
	"TargetNotFound":             "Policy target not found",
	"PartiallyValid":             "Partially valid; some settings were ignored",
	"Overridden":                 "Overridden by another policy",
	"AddressNotAssigned":         "No address assigned",
	"InvalidParameters":          "Invalid GatewayClass parameters",
	"PortUnavailable":            "Listener port unavailable",
	"HostnameConflict":           "Listener hostname conflicts with another listener",
	"ProtocolConflict":           "Listener protocol conflicts with another listener",
	"InvalidCertificateRef":      "Invalid certificate reference",
}

// message returns operator language for c, falling back to "<Type> is <Status> (<Reason>)".
func message(c metav1.Condition) string {
	if m, ok := reasonMessages[c.Reason]; ok {
		return m
	}
	return fmt.Sprintf("%s is %s (%s)", c.Type, c.Status, c.Reason)
}

// detail returns the raw condition for the secondary detail field.
func detail(c metav1.Condition, extra string) string {
	d := fmt.Sprintf("%s=%s, reason %s", c.Type, c.Status, c.Reason)
	if c.Message != "" {
		d += ": " + c.Message
	}
	if extra != "" {
		d += " (" + extra + ")"
	}
	return d
}
```

- [ ] **Step 5: Implement the checks**

`pkg/health/checks.go`:

```go
package health

import (
	"fmt"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Upstream returns the Phase 1 checks (design §5.2).
func Upstream() []Check {
	return []Check{cacheCheck{}, scopeCheck{}, controllerCheck{}, conditionsCheck{}, proxyCheck{}, xdsCheck{}, unwatchedCheck{}, ambiguityCheck{}, verifyCheck{}}
}

func ref(u *unstructured.Unstructured) *objects.Ref { r := objects.RefOf(u); return &r }

func since(c metav1.Condition) *time.Time {
	if c.LastTransitionTime.IsZero() {
		return nil
	}
	t := c.LastTransitionTime.Time
	return &t
}

func ok(name string, required bool, at time.Time) CheckResult {
	return CheckResult{Name: name, OK: true, Required: required, CollectedAt: at}
}

func unavailable(in Input, name, why string) ([]Finding, CheckResult) {
	return []Finding{{Code: CodeSignalUnavailable, Slug: "signal-unavailable", Severity: SeverityInfo, Install: in.Install.ID(),
			Message: "Could not check " + name, Detail: why}},
		CheckResult{Name: name, Required: true, CollectedAt: in.Now, Error: why}
}

// cacheCheck: the snapshot is synced and every served kind, plus Namespaces,
// Pods and Deployments, is currently readable (finding 2).
type cacheCheck struct{}

func (cacheCheck) Name() string { return "cluster access" }
func (c cacheCheck) Run(in Input) ([]Finding, CheckResult) {
	if !in.Snapshot.Synced {
		return unavailable(in, c.Name(), "cache not synced")
	}
	var fs []Finding
	for gvr, a := range in.Snapshot.Access {
		if !a.OK {
			fs = append(fs, Finding{Code: CodeSignalUnavailable, Slug: "signal-unavailable", Severity: SeverityInfo, Install: in.Install.ID(),
				Message: "No access to " + gvr.Resource, Detail: a.Err})
		}
	}
	if len(fs) > 0 {
		return fs, CheckResult{Name: c.Name(), Required: true, CollectedAt: in.Snapshot.TakenAt, Error: fmt.Sprintf("%d kinds not readable", len(fs))}
	}
	return nil, ok(c.Name(), true, in.Snapshot.TakenAt)
}

// scopeCheck: the install's scope could be determined.
type scopeCheck struct{}

func (scopeCheck) Name() string { return "controller scope" }
func (c scopeCheck) Run(in Input) ([]Finding, CheckResult) {
	if in.Install.Scope.Mode == install.ScopeUnknown {
		return unavailable(in, c.Name(), in.Install.Scope.Err)
	}
	return nil, ok(c.Name(), true, in.Now)
}

// controllerCheck: KGWUI-005.
type controllerCheck struct{}

func (controllerCheck) Name() string { return "controller pods" }
func (c controllerCheck) Run(in Input) ([]Finding, CheckResult) {
	d := in.Install.Deployment
	if f, bad := notReady(d, CodeControllerNotReady, "controller-not-ready", "Controller", in.Install.ID()); bad {
		f.Resource = &objects.Ref{Group: "apps", Kind: "Deployment", Namespace: d.Namespace, Name: d.Name}
		return []Finding{f}, ok(c.Name(), true, in.Now)
	}
	return nil, ok(c.Name(), true, in.Now)
}

func notReady(d *appsv1.Deployment, code, slug, what, inst string) (Finding, bool) {
	want := int32(1)
	if d.Spec.Replicas != nil {
		want = *d.Spec.Replicas
	}
	if d.Status.ReadyReplicas >= want {
		return Finding{}, false
	}
	return Finding{Code: code, Slug: slug, Severity: SeverityError, Install: inst,
		Message: fmt.Sprintf("%s not ready (%d of %d replicas)", what, d.Status.ReadyReplicas, want)}, true
}

// Required and optional condition types per status block (see the condition
// rules in the plan). Optional types are evaluated only when present.
var (
	classRequired    = []string{"Accepted"}
	gatewayRequired  = []string{"Accepted", "Programmed"}
	listenerRequired = []string{"Accepted", "Programmed"}
	listenerOptional = []string{"ResolvedRefs"}
	routeRequired    = []string{"Accepted", "ResolvedRefs"}
	routeOptional    = []string{"kgateway.dev/Programmed"}
	policyRequired   = []string{"Accepted"}
	policyOptional   = []string{"Attached"}
)

// conditionsCheck: KGWUI-002 and KGWUI-003. Freshness is decided before
// status (finding 1, D-006): a missing status block, a missing required
// condition, a condition with reason Pending, or any stale condition makes the
// resource "not yet processed"; only current conditions are reported as errors
// or count as success. Inside the grace period such a resource is "being
// processed" (info) and the check is not OK, so the install is unknown, never
// healthy (round 2, finding 2).
type conditionsCheck struct{}

func (conditionsCheck) Name() string { return "resource status" }
func (c conditionsCheck) Run(in Input) ([]Finding, CheckResult) {
	var fs []Finding
	id := in.Install.ID()
	add := func(u *unstructured.Unstructured, sev Severity, cond metav1.Condition, extra string) {
		fs = append(fs, Finding{Code: CodeConditionFalse, Slug: "condition-false", Severity: sev, Install: id, Resource: ref(u),
			Message: message(cond), Detail: detail(cond, extra), Since: since(cond)})
	}
	// One KGWUI-003 per (resource, parentRef); parent is the zero value for
	// everything but route parents (round 3, finding 5).
	type pendingKey struct {
		res    objects.Ref
		parent objects.ParentRef
	}
	reported := map[pendingKey]bool{}
	inGrace := map[objects.Ref]bool{} // resources being processed
	// notProcessed reports u (for parent) once: info while u's current
	// generation was first seen less than Grace ago, warning after.
	notProcessed := func(u *unstructured.Unstructured, parent objects.ParentRef, msg, why string) {
		r := objects.RefOf(u)
		k := pendingKey{r, parent}
		if reported[k] {
			return
		}
		reported[k] = true
		sev := SeverityWarning
		if in.Now.Sub(in.firstSeen(u)) < Grace {
			sev, msg = SeverityInfo, "Being processed by the controller"
			inGrace[r] = true
		}
		fs = append(fs, Finding{Code: CodeRouteUnattached, Slug: "route-unattached", Severity: sev, Install: id, Resource: &r,
			Message: msg, Detail: why})
	}
	// pendingFor reports a block of u that is not processed; parent is set for route parents.
	pendingFor := func(u *unstructured.Unstructured, parent objects.ParentRef, why string) {
		notProcessed(u, parent, "Not yet processed by the controller", why)
	}
	pending := func(u *unstructured.Unstructured, why string) { pendingFor(u, objects.ParentRef{}, why) }
	// current reports false when the block is pending: a required condition is
	// missing, a present condition has reason Pending, or a present condition
	// may describe an older generation (including observedGeneration 0).
	current := func(u *unstructured.Unstructured, parent objects.ParentRef, cs []metav1.Condition, required, optional []string, extra string) bool {
		for _, t := range required {
			if objects.FindCondition(cs, t) == nil {
				pendingFor(u, parent, fmt.Sprintf("no %s condition%s", t, within(extra)))
				return false
			}
		}
		for _, t := range append(slices.Clone(required), optional...) {
			cond := objects.FindCondition(cs, t)
			if cond == nil {
				continue
			}
			if cond.Reason == "Pending" {
				pendingFor(u, parent, fmt.Sprintf("%s is Pending%s", t, within(extra)))
				return false
			}
			if objects.IsStale(u, *cond) {
				pendingFor(u, parent, "status describes an older generation"+within(extra))
				return false
			}
		}
		return true
	}
	// evaluate reports every present condition of a current block that is not True.
	evaluate := func(u *unstructured.Unstructured, parent objects.ParentRef, cs []metav1.Condition, required, optional []string, extra string) {
		if !current(u, parent, cs, required, optional, extra) {
			return
		}
		for _, t := range append(slices.Clone(required), optional...) {
			if cond := objects.FindCondition(cs, t); cond != nil && cond.Status != metav1.ConditionTrue {
				add(u, SeverityError, *cond, extra)
			}
		}
	}
	for _, gc := range in.Owned.GatewayClasses {
		evaluate(gc, objects.ParentRef{}, objects.Conditions(gc), classRequired, nil, "")
	}
	for _, gw := range in.Owned.Gateways {
		evaluate(gw, objects.ParentRef{}, objects.Conditions(gw), gatewayRequired, nil, "")
		byName := map[string]objects.ListenerStatus{}
		for _, l := range objects.GatewayListeners(gw) {
			byName[l.Name] = l
		}
		// Every configured listener needs a status block (round 2, finding 4).
		for _, name := range objects.GatewaySpecListeners(gw) {
			l, ok := byName[name]
			if !ok {
				pending(gw, "no status for listener "+name)
				continue
			}
			evaluate(gw, objects.ParentRef{}, l.Conditions, listenerRequired, listenerOptional, "listener "+l.Name)
			if cf := objects.FindCondition(l.Conditions, "Conflicted"); cf != nil && cf.Status == metav1.ConditionTrue && !objects.IsStale(gw, *cf) {
				add(gw, SeverityError, *cf, "listener "+l.Name)
			}
		}
	}
	// Routes: one status block per current parentRef of this install, matched on
	// the full parentRef including section name and port (finding 5, round 2 finding 5).
	for _, r := range in.Owned.Routes {
		status := objects.RouteParents(r.Obj)
		for _, p := range r.Parents {
			extra := "parent " + p.Kind + " " + p.Namespace + "/" + p.Name
			if p.Port != 0 {
				extra += fmt.Sprintf(" port %d", p.Port)
			}
			var st *objects.RouteParentStatus
			for i := range status {
				if status[i].ParentRef == p && slices.Contains(in.Install.ControllerNames, status[i].ControllerName) {
					st = &status[i]
					break
				}
			}
			if st == nil {
				pendingFor(r.Obj, p, "no status from the controller for "+extra)
				continue
			}
			evaluate(r.Obj, p, st.Conditions, routeRequired, routeOptional, extra)
		}
	}
	// Routes whose parent does not exist (finding 9).
	for _, u := range in.Owned.Unattached {
		notProcessed(u.Route.Obj, u.Parent, "Not attached: parent not found",
			fmt.Sprintf("parent %s %s/%s does not exist", u.Parent.Kind, u.Parent.Namespace, u.Parent.Name))
	}
	// Policies: ancestors written by this install's controller. None at all is
	// pending; on v2.3/v2.4 that includes a policy whose target is missing (finding 7).
	for _, p := range in.Owned.Policies {
		var mine []objects.AncestorStatus
		for _, a := range objects.PolicyAncestors(p.Obj) {
			if slices.Contains(in.Install.ControllerNames, a.ControllerName) {
				mine = append(mine, a)
			}
		}
		if len(mine) == 0 {
			pending(p.Obj, "no status from the controller")
			continue
		}
		for _, a := range mine {
			extra := "ancestor " + a.AncestorRef.Kind + " " + a.AncestorRef.Name
			if !current(p.Obj, objects.ParentRef{}, a.Conditions, policyRequired, policyOptional, extra) {
				continue
			}
			for _, cond := range a.Conditions {
				if sev, report := policySeverity(cond); report {
					add(p.Obj, sev, cond, extra)
				}
			}
		}
	}
	if n := len(inGrace); n > 0 {
		noun := "resources are"
		if n == 1 {
			noun = "resource is"
		}
		return fs, CheckResult{Name: c.Name(), Required: true, Pending: true, CollectedAt: in.Snapshot.TakenAt,
			Error: fmt.Sprintf("%d %s being processed by the controller", n, noun)}
	}
	return fs, ok(c.Name(), true, in.Snapshot.TakenAt)
}

// within formats extra as a trailing " (extra)", or "" when empty.
func within(extra string) string {
	if extra == "" {
		return ""
	}
	return " (" + extra + ")"
}

// policySeverity maps a current policy ancestor condition to a severity
// (design §5.2, with Overridden as info; see the plan's proposed refinement).
// Reason Pending never reaches it: current() treats it as not processed.
func policySeverity(c metav1.Condition) (Severity, bool) {
	switch c.Reason {
	case "PartiallyValid":
		return SeverityWarning, true
	case "Overridden":
		return SeverityInfo, true
	}
	if c.Status == metav1.ConditionFalse {
		return SeverityError, true
	}
	return "", false
}

// proxyCheck: KGWUI-006. Proxies are matched by ownerReference first, then by
// the gateway-name label (truncated for names over 63 characters; Review Focus 1).
type proxyCheck struct{}

func (proxyCheck) Name() string { return "gateway proxies" }
func (c proxyCheck) Run(in Input) ([]Finding, CheckResult) {
	var fs []Finding
	for _, gw := range in.Owned.Gateways {
		d := proxyFor(gw, in.Snapshot.Deployments)
		if d == nil {
			if in.Now.Sub(gw.GetCreationTimestamp().Time) >= Grace {
				fs = append(fs, Finding{Code: CodeProxyNotReady, Slug: "proxy-not-ready", Severity: SeverityWarning, Install: in.Install.ID(),
					Resource: ref(gw), Message: "Proxy deployment not found"})
			}
			continue
		}
		if f, bad := notReady(d, CodeProxyNotReady, "proxy-not-ready", "Proxy", in.Install.ID()); bad {
			f.Resource = ref(gw)
			fs = append(fs, f)
		}
	}
	return fs, ok(c.Name(), true, in.Snapshot.TakenAt)
}

func proxyFor(gw *unstructured.Unstructured, ds []*appsv1.Deployment) *appsv1.Deployment {
	for _, d := range ds {
		if d.Namespace != gw.GetNamespace() || d.Labels["app.kubernetes.io/component"] != "proxy" {
			continue
		}
		for _, o := range d.OwnerReferences {
			if o.Kind == "Gateway" && o.Name == gw.GetName() {
				return d
			}
		}
	}
	for _, d := range ds {
		if d.Namespace == gw.GetNamespace() && d.Labels["app.kubernetes.io/component"] == "proxy" &&
			d.Labels["gateway.networking.k8s.io/gateway-name"] == gw.GetName() {
			return d
		}
	}
	return nil
}

// xdsCheck: KGWUI-001.
type xdsCheck struct{}

func (xdsCheck) Name() string { return "xDS rejects" }
func (c xdsCheck) Run(in Input) ([]Finding, CheckResult) {
	if !in.HasMetrics {
		return unavailable(in, c.Name(), "controller metrics not collected yet")
	}
	if in.Metrics.Err != "" {
		return unavailable(in, c.Name(), in.Metrics.Err)
	}
	// Recomputing the model must not make an old zero-reject result look fresh (finding 3).
	if in.Metrics.Stale(in.Now, in.MetricsMaxAge) {
		return unavailable(in, c.Name(), fmt.Sprintf("controller metrics are stale (last collected %s ago)", in.Now.Sub(in.Metrics.CollectedAt).Round(time.Second)))
	}
	var fs []Finding
	for _, gw := range in.Owned.Gateways {
		if n := in.Metrics.Metrics.Rejects[ctrlmetrics.GatewayKey{Namespace: gw.GetNamespace(), Name: gw.GetName()}]; n > 0 {
			fs = append(fs, Finding{Code: CodeXDSRejected, Slug: "xds-rejected", Severity: SeverityError, Install: in.Install.ID(),
				Resource: ref(gw), Message: "Configuration rejected by Envoy", Detail: fmt.Sprintf("%.0f active rejects", n)})
		}
	}
	return fs, ok(c.Name(), true, in.Metrics.CollectedAt)
}

// unwatchedCheck: KGWUI-004.
type unwatchedCheck struct{}

func (unwatchedCheck) Name() string { return "watched namespaces" }
func (c unwatchedCheck) Run(in Input) ([]Finding, CheckResult) {
	var fs []Finding
	for _, u := range in.Assignment.Unwatched {
		if u.Install != in.Install.ID() {
			continue
		}
		r := u.Route
		fs = append(fs, Finding{Code: CodeNamespaceNotWatched, Slug: "namespace-not-watched", Severity: SeverityWarning, Install: u.Install,
			Resource: &r, Message: "Namespace not watched by the controller", Detail: "targets Gateway " + u.Gateway.String()})
	}
	return fs, ok(c.Name(), false, in.Now)
}

// ambiguityCheck: KGWUI-009.
type ambiguityCheck struct{}

func (ambiguityCheck) Name() string { return "gateway ownership" }
func (c ambiguityCheck) Run(in Input) ([]Finding, CheckResult) {
	var fs []Finding
	for _, a := range in.Assignment.Ambiguous {
		if !slices.Contains(a.Installs, in.Install.ID()) {
			continue
		}
		g := a.Gateway
		fs = append(fs, Finding{Code: CodeAmbiguousOwnership, Slug: "ambiguous-ownership", Severity: SeverityWarning, Install: in.Install.ID(),
			Resource: &g, Message: "Claimed by more than one controller install", Detail: fmt.Sprint(a.Installs)})
	}
	return fs, ok(c.Name(), false, in.Now)
}

// verifyCheck: KGWUI-007, and KGWUI-008 (info, not required) when it cannot run.
type verifyCheck struct{}

func (verifyCheck) Name() string { return "scope matches controller" }
func (c verifyCheck) Run(in Input) ([]Finding, CheckResult) {
	if !in.Verify.Available {
		return []Finding{{Code: CodeSignalUnavailable, Slug: "signal-unavailable", Severity: SeverityInfo, Install: in.Install.ID(),
			Message: "Could not verify scope against the controller", Detail: in.Verify.Reason}}, CheckResult{Name: c.Name(), CollectedAt: in.Now, Error: in.Verify.Reason}
	}
	var fs []Finding
	for _, m := range in.Verify.Mismatches {
		s := m.Since
		fs = append(fs, Finding{Code: CodeScopeMismatch, Slug: "scope-mismatch", Severity: SeverityWarning, Install: in.Install.ID(),
			Message: "UI scope differs from the controller", Since: &s,
			Detail: fmt.Sprintf("namespace %s, %s: UI counts %.0f, controller counts %.0f", m.Namespace, m.Resource, m.UI, m.Controller)})
	}
	return fs, ok(c.Name(), false, in.Now)
}
```

- [ ] **Step 6: Run the tests and confirm they pass**

Run: `go test ./pkg/health/... ./pkg/scopeverify/...`
Expected: PASS. In `TestLongGatewayNameMatchesProxyByOwner`, the `ProxyDeployment` ownerReference already uses the full name; that is the behaviour under test.

- [ ] **Step 7: Commit**

```bash
git add pkg/health pkg/scopeverify && git commit -m "feat(health): findings, checks, roll-up and scope verification"
```

---

### Task 10: Engine, overview and HTTP API (API boundary tests)

**Files:**
- Create: `pkg/overview/overview.go`, `pkg/engine/engine.go`, `pkg/api/types.go`, `pkg/api/handlers.go`, `pkg/api/authz.go`
- Test: `pkg/engine/engine_test.go` (scale), `pkg/api/api_test.go` (boundary)

**Interfaces:**
- Consumes: everything from Tasks 3–9.
- Produces:
  - `overview.ReadyCount{Ready, Desired int32}`;
  - `overview.ScopeSummary{Mode string; Selectors []metav1.LabelSelector; Error string; Watched, Total int}`;
  - `overview.Header{ID, Namespace, Name, Version, Edition string; ControllerNames []string; Controller ReadyCount; Scope ScopeSummary}`;
  - `overview.Count{Group, Kind, Category, DisplayName string; Count int; Access cache.Access}`;
  - `overview.GatewayRow{Ref objects.Ref; Class string; State health.State; Listeners int; AttachedRoutes int32; Addresses []string}`;
  - `overview.Overview{Header Header; Counts []Count; Gateways []GatewayRow}`;
  - `overview.Build(res install.Resolved, owned *install.Owned, snap *cache.Snapshot, served []kinds.Served, rep health.Report) Overview`. It is pure, so the API can rebuild it over an `Authorizer`-filtered owned set. The GatewayClass category gets a tile like every other served kind (finding 12).
- Produces:
  - `engine.Config{Snapshots cache.Snapshotter; Served []kinds.Served; Detectors []install.Detector; Scope install.ScopeProvider; Verifier scopeverify.Verifier; Metrics ctrlmetrics.Source; Checks []health.Check; Pinned string; MetricsMaxAge time.Duration; Now func() time.Time}` (`MetricsMaxAge` is passed to `health.Input` and `scopeverify.Input`; `app` sets it to 3 × the metrics interval);
  - `engine.InstallModel{Resolved install.Resolved; Owned *install.Owned; Overview overview.Overview; Health health.Report}`;
  - `engine.Model{Snapshot *cache.Snapshot; Served []kinds.Served; Installs []InstallModel; Pinned string; ComputedAt time.Time}`;
  - `engine.New(Config) *Engine`, `(*Engine).Compute() *Model`, `(*Engine).Current() *Model`, `(*Engine).Installs() []install.Install`, `(*Engine).Run(ctx, every time.Duration)`. The engine remembers when it first saw each object's (uid, generation) and passes that as `health.Input.FirstSeen`; objects present at the first computation get the UI start time, and entries for objects that are gone are pruned (round 3, finding 3).
- Produces:
  - `api.ModelSource` (interface `Current() *engine.Model`);
  - `api.Authorizer` (interface `Authorize(r *http.Request, ref objects.Ref) bool`), `api.AllowAll{}` (the Phase 1 default) and `api.AuthorizerFunc` (`func(*http.Request, objects.Ref) bool` adapter) in `pkg/api/authz.go` (D-013);
  - `api.NewHandler(src ModelSource, ui fs.FS, authz Authorizer) http.Handler` (a nil `authz` means `AllowAll`);
  - the JSON types in `pkg/api/types.go`: `InstallSummary` gains `Controller overview.ReadyCount` (finding 12) and `ComputedAt time.Time` (the model's computation time, so the cluster cards and the install switcher can apply the stale rule; round 2, finding 6); `HealthResponse` stays an alias of `health.Report`, which carries `install` (finding 4); `MetaResponse.Kinds` lists the served kinds plus `apps/Deployment` (category `workload`), the kinds the resource endpoint can serve (finding 11).

**Resource endpoint (finding 11, D-013).** `GET …/resources/{group}/{kind}/{ns}/{name}` serves an object when the `Authorizer` allows it and it is either owned by the install or referenced by a current finding of the install (in Phase 1: unattached and unprocessed routes for 003, unwatched routes for 004, the controller Deployment for 005, Gateways for 006, ambiguous Gateways for 009). Only `metadata.managedFields` is stripped. Everything else returns 404 with the same message, so a denied object is indistinguishable from an absent one.

**Authorizer (D-013).** Every handler that returns resources, findings or counts goes through it. A denied object returns 404 on the resource endpoint and is left out of the overview's count tiles and gateway rows. A finding on a denied resource stays in the list with its code, severity and resource ref, but `restricted: true` and an empty message, detail and since. The roll-up state and the severity counts cover every finding. Under `AllowAll` the handlers return the precomputed model unchanged.

- [ ] **Step 1: Write the failing boundary tests**

`pkg/api/api_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/overview"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

const img = "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7"

type fakeMetrics map[string]ctrlmetrics.Result

func (f fakeMetrics) Get(id string) (ctrlmetrics.Result, bool) { r, ok := f[id]; return r, ok }

// okMetrics are fresh, reject-free metrics. ManagedPresent lets the scope
// verifier run; with the default 1 h persistence no mismatch is reported.
func okMetrics(ids ...string) fakeMetrics {
	f := fakeMetrics{}
	for _, id := range ids {
		f[id] = ctrlmetrics.Result{CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{}, Managed: map[ctrlmetrics.ManagedKey]float64{}, ManagedPresent: true}}
	}
	return f
}

// setup configures startWith. Zero values are the defaults.
type setup struct {
	served    []kinds.Served     // nil: every upstream kind
	pinned    string             // pinned install ID
	metrics   ctrlmetrics.Source // nil: okMetrics("kgw/kgateway")
	immediate bool               // report scope mismatches on the second computation (persistence 0, not 1 h)
	authz     Authorizer         // nil: AllowAll
	// prepare runs on the fake clients before the cache starts (failure injection).
	prepare func(dyn *dynamicfake.FakeDynamicClient, kube *kubefake.Clientset)
	// late objects are created after the engine has first seen the rest, so their
	// generation is first seen at testutil.Now: inside the grace period.
	late []*unstructured.Unstructured
}

// harness is a running cache, engine and handler over fake clients, with the
// clients exposed so a test can inject failures after the initial sync.
type harness struct {
	ctx   context.Context
	cache *cache.Cache
	e     *engine.Engine
	srv   *httptest.Server
	dyn   *dynamicfake.FakeDynamicClient
	kube  *kubefake.Clientset
}

// clock is the engine clock of a harness.
type clock struct{ t atomic.Pointer[time.Time] }

func (c *clock) set(t time.Time) { c.t.Store(&t) }
func (c *clock) now() time.Time  { return *c.t.Load() }

// newHarness runs the real cache, engine and handler over fake clients seeded
// with objs. The engine first computes at testutil.Now minus Grace and 1 s, so
// the seeded objects' generations are past the grace period at testutil.Now,
// the time every API response is computed at (D-006: unprocessed means warning).
func newHarness(t *testing.T, s setup, objs ...runtime.Object) *harness {
	t.Helper()
	if s.served == nil {
		s.served = testutil.ServedUpstream()
	}
	if s.metrics == nil {
		s.metrics = okMetrics("kgw/kgateway")
	}
	persist := time.Hour
	if s.immediate {
		persist = 0
	}
	dyn, kube := testutil.Clients(t, objs...)
	fd := dyn.(*dynamicfake.FakeDynamicClient)
	if s.prepare != nil {
		s.prepare(fd, kube)
	}
	det := install.UpstreamDetector{}
	c := cache.New(cache.Config{Dynamic: dyn, Kube: kube, Served: s.served,
		PodSelectors: []labels.Selector{det.ControllerPodSelector()}, DeploymentSelectors: []labels.Selector{det.ControllerSelector(), det.ProxySelector()}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Start(ctx)
	clk := &clock{}
	clk.set(testutil.Now.Add(-health.Grace - time.Second))
	e := engine.New(engine.Config{Snapshots: c, Served: s.served, Detectors: []install.Detector{det}, Scope: install.NamespaceSelectorScope{},
		Verifier: scopeverify.NewManagedCountVerifier(persist), Metrics: s.metrics, Checks: health.Upstream(), Pinned: s.pinned,
		MetricsMaxAge: 30 * time.Second, Now: clk.now})
	deadline := time.Now().Add(5 * time.Second)
	for !e.Compute().Snapshot.Synced {
		if time.Now().After(deadline) {
			t.Fatal("not synced")
		}
		time.Sleep(20 * time.Millisecond)
	}
	clk.set(testutil.Now)
	for _, u := range s.late {
		createAndWait(ctx, t, fd, c, s.served, u)
	}
	e.Compute()
	srv := httptest.NewServer(NewHandler(e, fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}}, s.authz))
	t.Cleanup(srv.Close)
	return &harness{ctx: ctx, cache: c, e: e, srv: srv, dyn: fd, kube: kube}
}

// createAndWait creates u through the fake dynamic client and waits until the cache holds it.
func createAndWait(ctx context.Context, t *testing.T, dyn *dynamicfake.FakeDynamicClient, c *cache.Cache, served []kinds.Served, u *unstructured.Unstructured) {
	t.Helper()
	gv, _ := schema.ParseGroupVersion(u.GetAPIVersion())
	for _, k := range served {
		if k.Group != gv.Group || k.Kind != u.GetKind() {
			continue
		}
		if _, err := dyn.Resource(k.GVR()).Namespace(u.GetNamespace()).Create(ctx, u, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			for _, o := range c.Snapshot().Objects[k.GVR()] {
				if o.GetNamespace() == u.GetNamespace() && o.GetName() == u.GetName() {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s %s/%s never reached the cache", u.GetKind(), u.GetNamespace(), u.GetName())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	t.Fatalf("kind %s is not served", u.GetKind())
}

// startWith is newHarness for tests that only need the engine, the server and namespace updates.
func startWith(t *testing.T, s setup, objs ...runtime.Object) (*engine.Engine, *httptest.Server, func(*corev1.Namespace)) {
	t.Helper()
	h := newHarness(t, s, objs...)
	update := func(n *corev1.Namespace) {
		if _, err := h.kube.CoreV1().Namespaces().Update(h.ctx, n, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	return h.e, h.srv, update
}

// start is startWith with the common settings.
func start(t *testing.T, served []kinds.Served, pinned string, m ctrlmetrics.Source, objs ...runtime.Object) (*engine.Engine, *httptest.Server, func(*corev1.Namespace)) {
	t.Helper()
	return startWith(t, setup{served: served, pinned: pinned, metrics: m}, objs...)
}

func get[T any](t *testing.T, srv *httptest.Server, path string, wantStatus int) T {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s: status %d, want %d", path, resp.StatusCode, wantStatus)
	}
	var v T
	if wantStatus == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
			t.Fatal(err)
		}
	}
	return v
}

// world: install kgw/kgateway scoped to team=a; infra and shop are watched, blog is not.
func world(extra ...runtime.Object) []runtime.Object { return worldWith(1, 1, extra...) }

// worldWith is world with the number of ready controller and proxy replicas (of 1).
func worldWith(ctrlReady, proxyReady int32, extra ...runtime.Object) []runtime.Object {
	d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, ctrlReady, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"a"}}]`))
	gw := testutil.GwRef("infra", "edge")
	return append([]runtime.Object{
		testutil.Namespace("kgw", nil), testutil.Namespace("infra", map[string]string{"team": "a"}),
		testutil.Namespace("shop", map[string]string{"team": "a"}), testutil.Namespace("blog", nil),
		d, testutil.ControllerPod(d, "kgateway-0", ctrlReady > 0), testutil.ProxyDeployment("infra", "edge", 1, proxyReady),
		testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.GatewayClass("other", "example.com/other", true),
		testutil.Gateway("infra", "edge", "kgateway", nil), testutil.Gateway("infra", "foreign", "other", nil),
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.HTTPRoute("blog", "posts", []objects.ParentRef{gw}),
	}, extra...)
}

// stale bumps u's generation past its fixture conditions (observed generation 1).
func stale(u *unstructured.Unstructured) *unstructured.Unstructured {
	u.SetGeneration(2)
	return u
}

func count(o OverviewResponse, kind string) (int, bool) {
	for _, c := range o.Counts {
		if c.Kind == kind {
			return c.Count, true
		}
	}
	return 0, false
}

func hasFinding(h HealthResponse, code, res string) bool {
	for _, f := range h.Findings {
		if f.Code == code && (res == "" || (f.Resource != nil && f.Resource.String() == res)) {
			return true
		}
	}
	return false
}

func TestOverviewCountsOnlyWhatTheInstallManages(t *testing.T) {
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw/kgateway"), world()...)
	insts := get[[]InstallSummary](t, srv, "/api/v1/installs", 200)
	if len(insts) != 1 || insts[0].ID != "kgw/kgateway" || insts[0].Version != "2.4.7" {
		t.Fatalf("installs: %+v", insts)
	}
	if insts[0].Controller != (overview.ReadyCount{Ready: 1, Desired: 1}) {
		t.Errorf("controller readiness: %+v", insts[0].Controller)
	}
	if !insts[0].ComputedAt.Equal(testutil.Now) {
		t.Errorf("installs must carry the model's computation time, got %s", insts[0].ComputedAt)
	}
	o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw/kgateway/overview", 200)
	if o.Header.ID != "kgw/kgateway" {
		t.Errorf("overview must carry the install id, got %q", o.Header.ID)
	}
	if n, ok := count(o, "GatewayClass"); !ok || n != 1 {
		t.Errorf("GatewayClasses: want a tile counting 1 (class 'other' excluded), got %d (tile %v)", n, ok)
	}
	if n, _ := count(o, "Gateway"); n != 1 {
		t.Errorf("Gateways: want 1 (foreign class excluded), got %d", n)
	}
	if n, _ := count(o, "HTTPRoute"); n != 1 {
		t.Errorf("HTTPRoutes: want 1 (blog/posts is out of scope), got %d", n)
	}
	if o.Header.Scope.Watched != 2 || o.Header.Scope.Total != 4 {
		t.Errorf("scope watched/total: %+v", o.Header.Scope)
	}
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw/kgateway/health", 200)
	if !hasFinding(h, health.CodeNamespaceNotWatched, "blog/posts") || h.State != health.StateWarning || h.Install != "kgw/kgateway" {
		t.Fatalf("health: %+v", h)
	}
}

func TestMetricsUnreachableIsNeverHealthy(t *testing.T) {
	m := fakeMetrics{"kgw/kgateway": {CollectedAt: testutil.Now, Err: "dial tcp: connection refused"}}
	_, srv, _ := start(t, testutil.ServedUpstream(), "", m, world()[:len(world())-1]...) // without blog/posts
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw/kgateway/health", 200)
	if h.State != health.StateUnknown || !hasFinding(h, health.CodeSignalUnavailable, "") {
		t.Fatalf("want unknown with signal-unavailable, got %s %+v", h.State, h.Findings)
	}
}

func TestUnservedKindHasNoTileAndNoFinding(t *testing.T) {
	var served []kinds.Served
	for _, s := range testutil.ServedUpstream() {
		if s.Kind != "TCPRoute" {
			served = append(served, s)
		}
	}
	_, srv, _ := start(t, served, "", okMetrics("kgw/kgateway"), world()...)
	o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw/kgateway/overview", 200)
	if _, ok := count(o, "TCPRoute"); ok {
		t.Fatal("an unserved kind must not have a tile")
	}
}

func TestOverlappingInstallsAndPinning(t *testing.T) {
	d2 := testutil.ControllerDeployment("kgw2", "kgateway2", img, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, "[]"))
	objs := world(testutil.Namespace("kgw2", nil), d2, testutil.ControllerPod(d2, "kgateway2-0", true))
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw/kgateway", "kgw2/kgateway2"), objs...)
	h := get[HealthResponse](t, srv, "/api/v1/installs/kgw2/kgateway2/health", 200)
	if !hasFinding(h, health.CodeAmbiguousOwnership, "infra/edge") {
		t.Fatalf("overlapping scopes must report ambiguous ownership: %+v", h.Findings)
	}
	_, pinned, _ := start(t, testutil.ServedUpstream(), "kgw/kgateway", okMetrics("kgw/kgateway", "kgw2/kgateway2"), objs...)
	if insts := get[[]InstallSummary](t, pinned, "/api/v1/installs", 200); len(insts) != 1 || insts[0].ID != "kgw/kgateway" {
		t.Fatalf("pinned: %+v", insts)
	}
	get[HealthResponse](t, pinned, "/api/v1/installs/kgw2/kgateway2/health", 404)
}

func TestNamespaceLabelChangeUpdatesScopeWithoutRestart(t *testing.T) {
	e, srv, update := start(t, testutil.ServedUpstream(), "", okMetrics("kgw/kgateway"), world()...)
	update(testutil.Namespace("blog", map[string]string{"team": "a"}))
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.Compute()
		o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw/kgateway/overview", 200)
		if n, _ := count(o, "HTTPRoute"); n == 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("blog/posts did not come into scope after labelling the namespace")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Finding 11: owned objects and objects referenced by a current finding are
// served (only managedFields stripped); anything else is 404.
func TestResourceEndpoint(t *testing.T) {
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw/kgateway"), world()...)
	const base = "/api/v1/installs/kgw/kgateway/resources/"
	r := get[map[string]any](t, srv, base+"gateway.networking.k8s.io/HTTPRoute/shop/cart", 200)
	if md, _ := r["metadata"].(map[string]any); md["managedFields"] != nil || md["name"] != "cart" {
		t.Fatalf("unexpected object: %+v", r["metadata"])
	}
	get[map[string]any](t, srv, base+"gateway.networking.k8s.io/HTTPRoute/blog/posts", 200)   // unwatched, referenced by KGWUI-004
	get[map[string]any](t, srv, base+"gateway.networking.k8s.io/Gateway/infra/foreign", 404)  // another controller's Gateway
	get[map[string]any](t, srv, base+"-/Secret/shop/x", 404)                                  // never served
	get[map[string]any](t, srv, base+"apps/Deployment/kgw/kgateway", 404)                     // ready controller: no finding references it
	get[map[string]any](t, srv, base+"gateway.networking.k8s.io/GatewayClass/-/kgateway", 200)
	meta := get[MetaResponse](t, srv, "/api/v1/meta", 200)
	found := false
	for _, k := range meta.Kinds {
		found = found || (k.Group == "apps" && k.Kind == "Deployment")
	}
	if !found {
		t.Fatal("/api/v1/meta must list apps/Deployment, which finding links can reach")
	}
}

// Finding 11: the controller Deployment referenced by KGWUI-005 is served.
func TestResourceEndpointServesReferencedDeployment(t *testing.T) {
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw/kgateway"), worldWith(0, 1)...)
	r := get[map[string]any](t, srv, "/api/v1/installs/kgw/kgateway/resources/apps/Deployment/kgw/kgateway", 200)
	md, _ := r["metadata"].(map[string]any)
	if r["kind"] != "Deployment" || r["apiVersion"] != "apps/v1" || md["name"] != "kgateway" || md["managedFields"] != nil {
		t.Fatalf("unexpected object: kind=%v apiVersion=%v metadata=%+v", r["kind"], r["apiVersion"], md)
	}
}

// Finding 15: every finding code at the API boundary, once where it must
// appear and once where it must not (design §9). Findings 1, 3 and 9 are
// covered here too: stale True, absent conditions, policies without
// ancestors, stale metrics and routes to a missing Gateway.
func TestEveryFindingCodeAtTheAPI(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	rejects := fakeMetrics{"kgw/kgateway": {CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{
		Rejects: map[ctrlmetrics.GatewayKey]float64{{Namespace: "infra", Name: "edge"}: 1}, Managed: map[ctrlmetrics.ManagedKey]float64{}, ManagedPresent: true}}}
	mismatch := fakeMetrics{"kgw/kgateway": {CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{
		Rejects: map[ctrlmetrics.GatewayKey]float64{}, Managed: map[ctrlmetrics.ManagedKey]float64{{Namespace: "shop", Resource: "HTTPRoute"}: 5}, ManagedPresent: true}}}
	old := fakeMetrics{"kgw/kgateway": {CollectedAt: testutil.Now.Add(-10 * time.Minute), Metrics: ctrlmetrics.Metrics{
		Rejects: map[ctrlmetrics.GatewayKey]float64{}, Managed: map[ctrlmetrics.ManagedKey]float64{}, ManagedPresent: true}}}
	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	partial := testutil.HTTPRoute("shop", "partial", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted")}})
	d2 := testutil.ControllerDeployment("kgw2", "kgateway2", img, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, "[]"))
	overlap := world(testutil.Namespace("kgw2", nil), d2, testutil.ControllerPod(d2, "kgateway2-0", true))

	cases := []struct {
		name      string
		s         setup
		objs      []runtime.Object
		recompute bool // compute once more, for findings that must persist (007)
		code, res string
		present   bool
		state     health.State // "" = not checked
	}{
		{"001 rejects", setup{metrics: rejects}, world(), false, health.CodeXDSRejected, "infra/edge", true, health.StateError},
		{"001 no rejects", setup{}, world(), false, health.CodeXDSRejected, "infra/edge", false, ""},
		{"002 backend not found", setup{}, world(broken), false, health.CodeConditionFalse, "shop/broken", true, health.StateError},
		{"002 accepted route", setup{}, world(), false, health.CodeConditionFalse, "shop/cart", false, ""},
		{"003 route without status", setup{}, world(testutil.HTTPRoute("shop", "new", []objects.ParentRef{gw})), false, health.CodeRouteUnattached, "shop/new", true, ""},
		{"003 stale True", setup{}, world(stale(testutil.HTTPRoute("shop", "old", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)))), false, health.CodeRouteUnattached, "shop/old", true, ""},
		{"003 required condition absent", setup{}, world(partial), false, health.CodeRouteUnattached, "shop/partial", true, ""},
		{"003 policy without ancestors", setup{}, world(testutil.TrafficPolicy("shop", "tp-new")), false, health.CodeRouteUnattached, "shop/tp-new", true, ""},
		{"003 parent gateway missing", setup{}, world(testutil.HTTPRoute("shop", "orphan", []objects.ParentRef{testutil.GwRef("infra", "gone")})), false, health.CodeRouteUnattached, "shop/orphan", true, ""},
		{"003 being processed inside grace", setup{late: []*unstructured.Unstructured{testutil.HTTPRoute("shop", "young", []objects.ParentRef{gw})}}, world(), false, health.CodeRouteUnattached, "shop/young", true, health.StateUnknown},
		{"003 accepted route", setup{}, world(), false, health.CodeRouteUnattached, "shop/cart", false, ""},
		{"004 unwatched namespace", setup{}, world(), false, health.CodeNamespaceNotWatched, "blog/posts", true, health.StateWarning},
		{"004 watched namespace", setup{}, world(), false, health.CodeNamespaceNotWatched, "shop/cart", false, ""},
		{"005 controller not ready", setup{}, worldWith(0, 1), false, health.CodeControllerNotReady, "kgw/kgateway", true, health.StateError},
		{"005 controller ready", setup{}, world(), false, health.CodeControllerNotReady, "", false, ""},
		{"006 proxy not ready", setup{}, worldWith(1, 0), false, health.CodeProxyNotReady, "infra/edge", true, health.StateError},
		{"006 proxy ready", setup{}, world(), false, health.CodeProxyNotReady, "", false, ""},
		{"007 persistent mismatch", setup{metrics: mismatch, immediate: true}, world(), true, health.CodeScopeMismatch, "", true, ""},
		{"007 fresh mismatch", setup{metrics: mismatch}, world(), true, health.CodeScopeMismatch, "", false, ""},
		{"008 metrics unreachable", setup{metrics: fakeMetrics{"kgw/kgateway": {CollectedAt: testutil.Now, Err: "connection refused"}}}, world(), false, health.CodeSignalUnavailable, "", true, health.StateUnknown},
		{"008 stale metrics", setup{metrics: old}, world(), false, health.CodeSignalUnavailable, "", true, health.StateUnknown},
		{"008 every signal available", setup{}, world(), false, health.CodeSignalUnavailable, "", false, ""},
		{"009 overlapping installs", setup{metrics: okMetrics("kgw/kgateway", "kgw2/kgateway2")}, overlap, false, health.CodeAmbiguousOwnership, "infra/edge", true, ""},
		{"009 single install", setup{}, world(), false, health.CodeAmbiguousOwnership, "", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, srv, _ := startWith(t, tc.s, tc.objs...)
			if tc.recompute {
				e.Compute()
			}
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw/kgateway/health", 200)
			if hasFinding(h, tc.code, tc.res) != tc.present {
				t.Fatalf("%s on %q: want present=%v; findings: %+v", tc.code, tc.res, tc.present, h.Findings)
			}
			if tc.state != "" && h.State != tc.state {
				t.Fatalf("state: want %s, got %s", tc.state, h.State)
			}
		})
	}
}

// isolated is world without the unwatched blog/posts route, so its baseline is healthy.
func isolated(extra ...runtime.Object) []runtime.Object {
	objs := world()
	return append(objs[:len(objs)-1:len(objs)-1], extra...)
}

// hasSev is hasFinding with a severity; an empty sev matches any.
func hasSev(h HealthResponse, code string, sev health.Severity, res string) bool {
	for _, f := range h.Findings {
		if f.Code == code && (sev == "" || f.Severity == sev) && (res == "" || (f.Resource != nil && f.Resource.String() == res)) {
			return true
		}
	}
	return false
}

type expect struct {
	code string
	sev  health.Severity
	res  string
}

// Round 2, finding 10: status rules at the HTTP boundary on an isolated world.
// Each case asserts the state, the expected finding and severity, the absence
// of wrong findings, and the inventory counts.
func TestStatusRulesAtTheAPI(t *testing.T) {
	gw := testutil.GwRef("infra", "edge")
	gw443 := gw
	gw443.Port = 443
	cond := testutil.Cond
	noGen := testutil.AcceptedBy(gw, testutil.Controller)
	for i := range noGen.Conds {
		noGen.Conds[i].ObservedGeneration = 0
	}
	edge2 := testutil.Gateway("infra", "edge2", "kgateway", nil)
	testutil.AddSpecListener(edge2, "https")
	// Foreign install: kgw2 watches team=b and owns infra-b/edge; shop/both is attached to both installs' Gateways.
	gwB := testutil.GwRef("infra-b", "edge")
	d2 := testutil.ControllerDeployment("kgw2", "kgateway2", img, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"b"}}]`))
	twoInstalls := isolated(testutil.Namespace("kgw2", nil), testutil.Namespace("infra-b", map[string]string{"team": "b"}), d2,
		testutil.ControllerPod(d2, "kgateway2-0", true), testutil.Gateway("infra-b", "edge", "kgateway", nil), testutil.ProxyDeployment("infra-b", "edge", 1, 1),
		testutil.HTTPRoute("shop", "both", []objects.ParentRef{gw, gwB}, testutil.AcceptedBy(gw, testutil.Controller),
			testutil.RouteParent{Ref: gwB, Controller: testutil.Controller, Conds: []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners"), cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}}))

	cases := []struct {
		name    string
		s       setup
		objs    []runtime.Object
		state   health.State
		want    *expect
		forbid  []expect
		counts  map[string]int
	}{
		{"baseline", setup{}, isolated(), health.StateHealthy, nil, nil, map[string]int{"GatewayClass": 1, "Gateway": 1, "HTTPRoute": 1}},
		{"stale True", setup{}, isolated(stale(testutil.HTTPRoute("shop", "old", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)))),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/old"}, []expect{{health.CodeConditionFalse, "", "shop/old"}}, map[string]int{"HTTPRoute": 2}},
		{"stale False", setup{}, isolated(stale(testutil.HTTPRoute("shop", "oldfail", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
			Conds: []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners"), cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}}))),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/oldfail"}, []expect{{health.CodeConditionFalse, "", "shop/oldfail"}}, map[string]int{"HTTPRoute": 2}},
		{"required condition absent", setup{}, isolated(testutil.HTTPRoute("shop", "partial", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
			Conds: []metav1.Condition{cond("Accepted", metav1.ConditionTrue, "Accepted")}})),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/partial"}, []expect{{health.CodeConditionFalse, "", "shop/partial"}}, map[string]int{"HTTPRoute": 2}},
		{"observedGeneration 0", setup{}, isolated(testutil.HTTPRoute("shop", "nogen", []objects.ParentRef{gw}, noGen)),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/nogen"}, nil, map[string]int{"HTTPRoute": 2}},
		{"policy Pending", setup{}, isolated(testutil.TrafficPolicy("shop", "tp-pending", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
			Conds: []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "Pending"), cond("Attached", metav1.ConditionFalse, "Pending")}})),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/tp-pending"}, []expect{{health.CodeConditionFalse, "", "shop/tp-pending"}}, map[string]int{"TrafficPolicy": 1}},
		{"policy without ancestors", setup{}, isolated(testutil.TrafficPolicy("shop", "tp-new")),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/tp-new"}, nil, map[string]int{"TrafficPolicy": 1}},
		{"new generic policy", setup{}, isolated(testutil.BackendTLSPolicy("shop", "btls-new")),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/btls-new"}, nil, map[string]int{"BackendTLSPolicy": 1}},
		{"being processed (first seen inside the grace period)", setup{late: []*unstructured.Unstructured{testutil.HTTPRoute("shop", "young", []objects.ParentRef{gw})}}, isolated(),
			health.StateUnknown, &expect{health.CodeRouteUnattached, health.SeverityInfo, "shop/young"}, []expect{{health.CodeRouteUnattached, health.SeverityWarning, "shop/young"}}, map[string]int{"HTTPRoute": 2}},
		{"status for a removed parent", setup{}, isolated(testutil.HTTPRoute("shop", "moved", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller),
			testutil.RouteParent{Ref: testutil.GwRef("infra", "old"), Controller: testutil.Controller, Conds: []metav1.Condition{cond("Accepted", metav1.ConditionFalse, "NoMatchingParent"), cond("ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")}})),
			health.StateHealthy, nil, []expect{{health.CodeConditionFalse, "", "shop/moved"}, {health.CodeRouteUnattached, "", "shop/moved"}}, map[string]int{"HTTPRoute": 2}},
		{"status only for another port", setup{}, isolated(testutil.HTTPRoute("shop", "ported", []objects.ParentRef{gw443}, testutil.AcceptedBy(gw, testutil.Controller))),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/ported"}, []expect{{health.CodeConditionFalse, "", "shop/ported"}}, map[string]int{"HTTPRoute": 2}},
		{"foreign-group parentRef", setup{}, isolated(testutil.HTTPRoute("shop", "foreign", []objects.ParentRef{{Group: "example.com", Kind: "Gateway", Namespace: "infra", Name: "edge"}})),
			health.StateHealthy, nil, []expect{{health.CodeRouteUnattached, "", "shop/foreign"}, {health.CodeConditionFalse, "", "shop/foreign"}}, map[string]int{"HTTPRoute": 1}},
		{"listener without a status block", setup{}, isolated(edge2, testutil.ProxyDeployment("infra", "edge2", 1, 1)),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "infra/edge2"}, nil, map[string]int{"Gateway": 2}},
		// Round 2, finding 8: a route owned through one parent counts; its missing parent still raises KGWUI-003.
		{"one parent owned, one missing", setup{}, isolated(testutil.HTTPRoute("shop", "half", []objects.ParentRef{gw, testutil.GwRef("infra", "gone")}, testutil.AcceptedBy(gw, testutil.Controller))),
			health.StateWarning, &expect{health.CodeRouteUnattached, health.SeverityWarning, "shop/half"}, []expect{{health.CodeConditionFalse, "", "shop/half"}}, map[string]int{"HTTPRoute": 2}},
		{"parent status of another install", setup{metrics: okMetrics("kgw/kgateway", "kgw2/kgateway2")}, twoInstalls,
			health.StateHealthy, nil, []expect{{health.CodeConditionFalse, "", "shop/both"}, {health.CodeRouteUnattached, "", "shop/both"}}, map[string]int{"HTTPRoute": 2, "Gateway": 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv, _ := startWith(t, tc.s, tc.objs...)
			h := get[HealthResponse](t, srv, "/api/v1/installs/kgw/kgateway/health", 200)
			if h.State != tc.state {
				t.Fatalf("state: want %s, got %s; findings: %+v", tc.state, h.State, h.Findings)
			}
			if tc.want != nil && !hasSev(h, tc.want.code, tc.want.sev, tc.want.res) {
				t.Fatalf("missing %s/%s on %s; findings: %+v", tc.want.code, tc.want.sev, tc.want.res, h.Findings)
			}
			for _, f := range tc.forbid {
				if hasSev(h, f.code, f.sev, f.res) {
					t.Fatalf("unexpected %s/%s on %s; findings: %+v", f.code, f.sev, f.res, h.Findings)
				}
			}
			o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw/kgateway/overview", 200)
			for kind, n := range tc.counts {
				if got, _ := count(o, kind); got != n {
					t.Errorf("%s count: want %d, got %d", kind, n, got)
				}
			}
		})
	}
}

// Round 3, finding 1 (and round 2, finding 10): a watch that fails after the
// initial sync, injected into the fake dynamic and typed clients while list
// stays allowed, makes the install unknown at the API. It recovers only after
// a Watch call succeeds again.
func TestWatchFailuresAfterSyncAtTheAPI(t *testing.T) {
	refused := fmt.Errorf("dial tcp 10.96.0.1:443: connect: %w", syscall.ECONNREFUSED)
	cases := []struct {
		name     string
		resource string
		typed    bool
		err      error
	}{
		{"httproutes connection refused", "httproutes", false, refused},
		{"httproutes throttled", "httproutes", false, apierrors.NewTooManyRequests("slow down", 1)},
		{"httproutes forbidden", "httproutes", false, apierrors.NewForbidden(schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: "httproutes"}, "", nil)},
		{"namespaces forbidden", "namespaces", true, apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "", nil)},
		{"pods throttled", "pods", true, apierrors.NewTooManyRequests("slow down", 1)},
		{"deployments connection refused", "deployments", true, refused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var fail atomic.Bool
			var failed atomic.Int32
			watchers := make(chan *watch.FakeWatcher, 64)
			react := func(clienttesting.Action) (bool, watch.Interface, error) {
				if fail.Load() {
					failed.Add(1)
					return true, nil, tc.err
				}
				w := watch.NewFake()
				watchers <- w
				return true, w, nil
			}
			hn := newHarness(t, setup{prepare: func(dyn *dynamicfake.FakeDynamicClient, kube *kubefake.Clientset) {
				if tc.typed {
					kube.PrependWatchReactor(tc.resource, react)
				} else {
					dyn.PrependWatchReactor(tc.resource, react)
				}
			}}, isolated()...)
			state := func() health.State {
				hn.e.Compute()
				return get[HealthResponse](t, hn.srv, "/api/v1/installs/kgw/kgateway/health", 200).State
			}
			if s := state(); s != health.StateHealthy {
				t.Fatalf("baseline must be healthy, got %s", s)
			}
			fail.Store(true)
			(<-watchers).Stop() // the established watch ends; the next Watch call fails
			waitUntil(t, "the install to become unknown", func() bool { return state() == health.StateUnknown })
			h := get[HealthResponse](t, hn.srv, "/api/v1/installs/kgw/kgateway/health", 200)
			if !hasSev(h, health.CodeSignalUnavailable, health.SeverityInfo, "") {
				t.Fatalf("KGWUI-008 missing: %+v", h.Findings)
			}
			n := failed.Load()
			waitUntil(t, "another failed watch", func() bool { return failed.Load() > n })
			if s := state(); s != health.StateUnknown {
				t.Fatalf("without a successful watch the install stays unknown, got %s", s)
			}
			fail.Store(false)
			waitUntil(t, "recovery after a successful watch", func() bool { return state() == health.StateHealthy })
		})
	}
}

// waitUntil polls cond until it holds or 15 s pass (reflector backoff starts at 0.8 s).
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestSPAFallbackAndProbes(t *testing.T) {
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw/kgateway"), world()...)
	for _, p := range []string{"/", "/i/kgw/kgateway", "/i/kgw/kgateway/findings"} {
		resp, _ := http.Get(srv.URL + p)
		if resp.StatusCode != 200 {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
		resp.Body.Close()
	}
	for _, p := range []string{"/healthz", "/readyz"} {
		resp, _ := http.Get(srv.URL + p)
		if resp.StatusCode != 200 {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
		resp.Body.Close()
	}
	get[map[string]any](t, srv, "/api/v1/nope", 404)
}
```

`pkg/engine/engine_test.go` (Review Focus 5):

```go
package engine

import (
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

type staticSnap struct{ s *cache.Snapshot }

func (s staticSnap) Snapshot() *cache.Snapshot { return s.s }

type noMetrics struct{}

func (noMetrics) Get(string) (ctrlmetrics.Result, bool) {
	return ctrlmetrics.Result{CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{}}}, true
}

// Round 3, finding 3: grace runs from when the engine first saw the current
// (uid, generation). An annotation-only update does not restart it; a
// generation bump does; objects present at the first computation get grace
// from UI start even if they were created long before.
func TestGraceRunsFromFirstSeenGeneration(t *testing.T) {
	served := testutil.ServedUpstream()
	gvr := func(kind string) schema.GroupVersionResource {
		for _, s := range served {
			if s.Kind == kind {
				return s.GVR()
			}
		}
		t.Fatal(kind)
		return schema.GroupVersionResource{}
	}
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	route := testutil.HTTPRoute("shop", "r", []objects.ParentRef{testutil.GwRef("infra", "edge")}) // created an hour ago, no status
	snap := &cache.Snapshot{Objects: map[schema.GroupVersionResource][]*unstructured.Unstructured{
		gvr("GatewayClass"): {testutil.GatewayClass("kgateway", testutil.Controller, true)},
		gvr("Gateway"):      {testutil.Gateway("infra", "edge", "kgateway", nil)},
		gvr("HTTPRoute"):    {route},
	}, Access: map[schema.GroupVersionResource]cache.Access{}, Namespaces: []*corev1.Namespace{testutil.Namespace("infra", nil), testutil.Namespace("shop", nil)},
		Deployments: []*appsv1.Deployment{d, testutil.ProxyDeployment("infra", "edge", 1, 1)}, Pods: []*corev1.Pod{testutil.ControllerPod(d, "p", true)}, Synced: true}
	now := testutil.Now
	e := New(Config{Snapshots: staticSnap{snap}, Served: served, Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour), Metrics: noMetrics{},
		Checks: health.Upstream(), Now: func() time.Time { return now }})
	severity := func() health.Severity {
		t.Helper()
		for _, f := range e.Compute().Installs[0].Health.Findings {
			if f.Code == health.CodeRouteUnattached && f.Resource != nil && f.Resource.String() == "shop/r" {
				return f.Severity
			}
		}
		t.Fatal("no KGWUI-003 on shop/r")
		return ""
	}
	if got := severity(); got != health.SeverityInfo {
		t.Fatalf("present at UI start: grace runs from start, want info, got %s", got)
	}
	now = now.Add(health.Grace + time.Second)
	if got := severity(); got != health.SeverityWarning {
		t.Fatalf("after grace: want warning, got %s", got)
	}
	route.SetAnnotations(map[string]string{"touched": "yes"}) // metadata-only update: generation unchanged
	route.SetResourceVersion("2")
	if got := severity(); got != health.SeverityWarning {
		t.Fatalf("an annotation update must not restart grace, got %s", got)
	}
	route.SetGeneration(2) // spec change
	if got := severity(); got != health.SeverityInfo {
		t.Fatalf("a generation bump must restart grace, got %s", got)
	}
	now = now.Add(health.Grace + time.Second)
	if got := severity(); got != health.SeverityWarning {
		t.Fatalf("after grace for generation 2: want warning, got %s", got)
	}
}

func TestComputeAt5000RoutesUnderOneSecond(t *testing.T) {
	served := testutil.ServedUpstream()
	gvr := func(kind string) schema.GroupVersionResource {
		for _, s := range served {
			if s.Kind == kind {
				return s.GVR()
			}
		}
		t.Fatal(kind)
		return schema.GroupVersionResource{}
	}
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	snap := &cache.Snapshot{Objects: map[schema.GroupVersionResource][]*unstructured.Unstructured{}, Access: map[schema.GroupVersionResource]cache.Access{},
		Deployments: []*appsv1.Deployment{d}, Pods: []*corev1.Pod{testutil.ControllerPod(d, "p", true)}, Synced: true, TakenAt: testutil.Now}
	snap.Objects[gvr("GatewayClass")] = []*unstructured.Unstructured{testutil.GatewayClass("kgateway", testutil.Controller, true)}
	for n := 0; n < 200; n++ {
		ns := fmt.Sprintf("ns-%03d", n)
		snap.Namespaces = append(snap.Namespaces, testutil.Namespace(ns, nil))
		snap.Objects[gvr("Gateway")] = append(snap.Objects[gvr("Gateway")], testutil.Gateway(ns, "gw", "kgateway", nil))
		gw := testutil.GwRef(ns, "gw")
		for r := 0; r < 25; r++ {
			snap.Objects[gvr("HTTPRoute")] = append(snap.Objects[gvr("HTTPRoute")],
				testutil.HTTPRoute(ns, fmt.Sprintf("r-%02d", r), []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)))
		}
	}
	e := New(Config{Snapshots: staticSnap{snap}, Served: served, Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope: install.NamespaceSelectorScope{}, Verifier: scopeverify.NewManagedCountVerifier(time.Hour), Metrics: noMetrics{},
		Checks: health.Upstream(), Now: func() time.Time { return testutil.Now }})
	start := time.Now()
	m := e.Compute()
	if el := time.Since(start); el > time.Second {
		t.Fatalf("Compute took %s for 5000 routes", el)
	}
	if len(m.Installs) != 1 || len(m.Installs[0].Owned.Routes) != 5000 {
		t.Fatalf("unexpected model: %d installs", len(m.Installs))
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./pkg/api/... ./pkg/engine/...`
Expected: FAIL (undefined packages and symbols)

- [ ] **Step 3: Implement the overview**

`pkg/overview/overview.go`:

```go
// Package overview builds the install page's header, count tiles and gateway list.
package overview

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// ReadyCount is ready versus desired replicas.
type ReadyCount struct {
	Ready   int32 `json:"ready"`
	Desired int32 `json:"desired"`
}

// ScopeSummary describes an install's namespace scope.
type ScopeSummary struct {
	Mode      string                 `json:"mode"`
	Selectors []metav1.LabelSelector `json:"selectors,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Watched   int                    `json:"watched"`
	Total     int                    `json:"total"`
}

// Header is the install page header.
type Header struct {
	ID              string       `json:"id"`
	Namespace       string       `json:"namespace"`
	Name            string       `json:"name"`
	Version         string       `json:"version"`
	Edition         string       `json:"edition"`
	ControllerNames []string     `json:"controllerNames"`
	Controller      ReadyCount   `json:"controller"`
	Scope           ScopeSummary `json:"scope"`
}

// Count is one count tile.
type Count struct {
	Group       string       `json:"group"`
	Kind        string       `json:"kind"`
	Category    string       `json:"category"`
	DisplayName string       `json:"displayName"`
	Count       int          `json:"count"`
	Access      cache.Access `json:"access"`
}

// GatewayRow is one row of the gateway table.
type GatewayRow struct {
	Ref            objects.Ref  `json:"ref"`
	Class          string       `json:"class"`
	State          health.State `json:"state"`
	Listeners      int          `json:"listeners"`
	AttachedRoutes int32        `json:"attachedRoutes"`
	Addresses      []string     `json:"addresses"`
}

// Overview is the install page model.
type Overview struct {
	Header   Header       `json:"header"`
	Counts   []Count      `json:"counts"`
	Gateways []GatewayRow `json:"gateways"`
}

// Build assembles the overview of one install.
func Build(res install.Resolved, owned *install.Owned, snap *cache.Snapshot, served []kinds.Served, rep health.Report) Overview {
	o := Overview{Header: header(res, snap), Counts: []Count{}, Gateways: []GatewayRow{}}
	byKind := map[string]int{}
	for _, c := range owned.GatewayClasses {
		byKind[c.GetKind()]++
	}
	for _, g := range owned.Gateways {
		byKind[g.GetKind()]++
	}
	for _, l := range owned.ListenerSets {
		byKind[l.GetKind()]++
	}
	for _, r := range owned.Routes {
		byKind[r.Kind.Kind]++
	}
	for _, p := range owned.Policies {
		byKind[p.Kind.Kind]++
	}
	// One tile per served kind, GatewayClasses included (finding 12).
	for _, k := range served {
		o.Counts = append(o.Counts, Count{Group: k.Group, Kind: k.Kind, Category: string(k.Category), DisplayName: k.DisplayName,
			Count: byKind[k.Kind], Access: snap.Access[k.GVR()]})
	}
	for _, g := range owned.Gateways {
		row := GatewayRow{Ref: objects.RefOf(g), Class: objects.GatewayClassName(g), Addresses: objects.GatewayAddresses(g), State: rowState(g, rep)}
		for _, l := range objects.GatewayListeners(g) {
			row.Listeners++
			row.AttachedRoutes += l.AttachedRoutes
		}
		o.Gateways = append(o.Gateways, row)
	}
	return o
}

func header(res install.Resolved, snap *cache.Snapshot) Header {
	h := Header{ID: res.ID(), Namespace: res.Namespace, Name: res.Name, Version: res.Version, Edition: res.Edition, ControllerNames: res.ControllerNames,
		Scope: ScopeSummary{Mode: string(res.Scope.Mode), Selectors: res.Scope.Selectors, Error: res.Scope.Err, Total: len(snap.Namespaces)}}
	if d := res.Deployment; d != nil {
		h.Controller = ReadyCount{Ready: d.Status.ReadyReplicas, Desired: 1}
		if d.Spec.Replicas != nil {
			h.Controller.Desired = *d.Spec.Replicas
		}
	}
	for _, n := range snap.Namespaces {
		if res.Scope.Contains(n.Name, n) {
			h.Scope.Watched++
		}
	}
	return h
}

// rowState is the worst finding on the gateway; if a required signal is
// missing and there is no error, the row is unknown, never green (D-006).
func rowState(g *unstructured.Unstructured, rep health.Report) health.State {
	ref := objects.RefOf(g)
	worst := health.StateHealthy
	for _, f := range rep.Findings {
		if f.Resource == nil || *f.Resource != ref {
			continue
		}
		switch f.Severity {
		case health.SeverityError:
			return health.StateError
		case health.SeverityWarning:
			worst = health.StateWarning
		}
	}
	for _, c := range rep.Checks {
		if c.Required && !c.OK {
			return health.StateUnknown
		}
	}
	return worst
}
```

- [ ] **Step 4: Implement the engine**

`pkg/engine/engine.go`:

```go
// Package engine periodically computes the UI's model from the cache, the
// controller metrics and the registered checks.
package engine

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/overview"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

// Config wires the engine.
type Config struct {
	Snapshots cache.Snapshotter
	Served    []kinds.Served
	Detectors []install.Detector
	Scope     install.ScopeProvider
	Verifier  scopeverify.Verifier
	Metrics   ctrlmetrics.Source
	Checks    []health.Check
	// Pinned restricts the UI to one install ID ("ns/name"); empty shows all.
	Pinned string
	// MetricsMaxAge is the age beyond which a metrics result is stale
	// (3 × the metrics interval); zero means no limit (finding 3).
	MetricsMaxAge time.Duration
	Now           func() time.Time
}

// InstallModel is everything the API serves about one install.
type InstallModel struct {
	Resolved install.Resolved
	Owned    *install.Owned
	Overview overview.Overview
	Health   health.Report
}

// Model is one consistent computation over a snapshot.
type Model struct {
	Snapshot   *cache.Snapshot
	Served     []kinds.Served
	Installs   []InstallModel
	Pinned     string
	ComputedAt time.Time
}

// Engine computes Models.
type Engine struct {
	cfg Config
	mu  sync.RWMutex
	cur *Model
	// computeMu serialises Compute; seen is only touched under it.
	computeMu sync.Mutex
	seen      map[generation]time.Time
}

// generation identifies one spec version of one object.
type generation struct {
	uid types.UID
	gen int64
}

// New returns an Engine.
func New(cfg Config) *Engine {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Engine{cfg: cfg, seen: map[generation]time.Time{}}
}

// observe records when each (uid, generation) in snap was first seen and
// forgets the ones that are gone. The grace period runs from that moment: a
// generation bump restarts it, a label or annotation update does not, and
// objects present at the first computation get grace from UI start (round 3, finding 3).
func (e *Engine) observe(snap *cache.Snapshot, now time.Time) {
	current := map[generation]bool{}
	for _, objs := range snap.Objects {
		for _, u := range objs {
			k := generation{u.GetUID(), u.GetGeneration()}
			current[k] = true
			if _, ok := e.seen[k]; !ok {
				e.seen[k] = now
			}
		}
	}
	for k := range e.seen {
		if !current[k] {
			delete(e.seen, k)
		}
	}
}

// firstSeen returns when the engine first saw u's current generation.
// It is called during Compute, under computeMu.
func (e *Engine) firstSeen(u *unstructured.Unstructured) time.Time {
	if t, ok := e.seen[generation{u.GetUID(), u.GetGeneration()}]; ok {
		return t
	}
	return e.cfg.Now()
}

// Compute builds a new Model and makes it current.
func (e *Engine) Compute() *Model {
	e.computeMu.Lock()
	defer e.computeMu.Unlock()
	snap := e.cfg.Snapshots.Snapshot()
	now := e.cfg.Now()
	e.observe(snap, now)
	m := &Model{Snapshot: snap, Served: e.cfg.Served, Pinned: e.cfg.Pinned, ComputedAt: now}
	all := install.Resolve(install.Discover(snap, e.cfg.Detectors), e.cfg.Scope)
	// Assignment runs over all installs so ambiguity is detected even when pinned.
	asg := install.Assign(snap, e.cfg.Served, all)
	for _, res := range all {
		if e.cfg.Pinned != "" && res.ID() != e.cfg.Pinned {
			continue
		}
		met, has := e.cfg.Metrics.Get(res.ID())
		ver := e.cfg.Verifier.Verify(scopeverify.Input{Install: res, Snapshot: snap, Served: e.cfg.Served, Metrics: met, HasMetrics: has,
			MetricsMaxAge: e.cfg.MetricsMaxAge, Now: now})
		owned := asg.PerInstall[res.ID()]
		rep := health.Evaluate(health.Input{Install: res, Owned: owned, Assignment: asg, Snapshot: snap, Metrics: met, HasMetrics: has,
			MetricsMaxAge: e.cfg.MetricsMaxAge, FirstSeen: e.firstSeen, Verify: ver, Now: now}, e.cfg.Checks)
		m.Installs = append(m.Installs, InstallModel{Resolved: res, Owned: owned, Health: rep, Overview: overview.Build(res, owned, snap, e.cfg.Served, rep)})
	}
	e.mu.Lock()
	e.cur = m
	e.mu.Unlock()
	return m
}

// Current returns the latest Model, or nil before the first Compute.
func (e *Engine) Current() *Model {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cur
}

// Installs returns the installs of the current model (used by the metrics collector).
func (e *Engine) Installs() []install.Install {
	m := e.Current()
	if m == nil {
		return nil
	}
	out := make([]install.Install, 0, len(m.Installs))
	for _, i := range m.Installs {
		out = append(out, i.Resolved.Install)
	}
	return out
}

// Run recomputes every interval until ctx is done.
func (e *Engine) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		e.Compute()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
```

- [ ] **Step 5: Implement the API**

`pkg/api/types.go`:

```go
// Package api serves the read-only JSON API (design §7), the probes and the SPA.
package api

import (
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/overview"
)

// InstallSummary is one entry of GET /api/v1/installs.
type InstallSummary struct {
	ID              string                `json:"id"`
	Namespace       string                `json:"namespace"`
	Name            string                `json:"name"`
	Version         string                `json:"version"`
	Edition         string                `json:"edition"`
	ControllerNames []string              `json:"controllerNames"`
	Controller      overview.ReadyCount   `json:"controller"`
	Scope           overview.ScopeSummary `json:"scope"`
	Health          HealthSummary         `json:"health"`
	// ComputedAt is when the model behind Health was computed (round 2, finding 6).
	ComputedAt time.Time `json:"computedAt"`
}

// HealthSummary is the state and per-severity counts.
type HealthSummary struct {
	State  health.State            `json:"state"`
	Counts map[health.Severity]int `json:"counts"`
}

// OverviewResponse is GET …/overview.
type OverviewResponse struct {
	overview.Overview
	CollectedAt time.Time `json:"collectedAt"`
}

// HealthResponse is GET …/health. It carries the install ID (finding 4).
type HealthResponse = health.Report

// MetaResponse is GET /api/v1/meta.
type MetaResponse struct {
	Version string `json:"version"`
	Pinned  string `json:"pinned,omitempty"`
	Synced  bool   `json:"synced"`
	// Kinds are the kinds the resource endpoint can serve: the served kinds
	// plus apps/Deployment. The frontend links only these (finding 11).
	Kinds []kinds.Served `json:"kinds"`
}

// deploymentKind is listed in /api/v1/meta because finding links reach
// controller Deployments (D-013). It is read through typed informers, not counted.
var deploymentKind = kinds.Served{Descriptor: kinds.Descriptor{Group: "apps", Kind: "Deployment", Resource: "deployments",
	Versions: []string{"v1"}, Namespaced: true, Category: kinds.CategoryWorkload, DisplayName: "Deployments"}, Version: "v1"}

// ErrorResponse is the body of every non-2xx API response.
type ErrorResponse struct {
	Error string `json:"error"`
}
```

`pkg/api/authz.go`:

```go
package api

import (
	"net/http"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// Authorizer decides, per request, which objects the caller may see (D-013).
// Findings are filtered by applying it to each finding's resource. Phase 1
// ships AllowAll; per-user authorisation plugs in here later.
type Authorizer interface {
	// Authorize reports whether the caller of r may view the object ref.
	Authorize(r *http.Request, ref objects.Ref) bool
}

// AllowAll lets every caller see everything the UI can read: the Phase 1
// trust model, in which whoever reaches the UI may view all it shows.
type AllowAll struct{}

// Authorize implements Authorizer.
func (AllowAll) Authorize(*http.Request, objects.Ref) bool { return true }

// AuthorizerFunc adapts a function to Authorizer.
type AuthorizerFunc func(r *http.Request, ref objects.Ref) bool

// Authorize implements Authorizer.
func (f AuthorizerFunc) Authorize(r *http.Request, ref objects.Ref) bool { return f(r, ref) }
```

`pkg/api/handlers.go`:

```go
package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/overview"
	"github.com/DuncanDoyle/kgateway-ui/pkg/version"
)

// ModelSource provides the current model.
type ModelSource interface{ Current() *engine.Model }

// NewHandler returns the HTTP handler for the API, the probes and the SPA in ui.
// Every response with resources, findings or counts goes through authz; a nil
// authz means AllowAll (D-013).
func NewHandler(src ModelSource, ui fs.FS, authz Authorizer) http.Handler {
	if authz == nil {
		authz = AllowAll{}
	}
	h := &handler{src: src, authz: authz}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.HandleFunc("GET /api/v1/meta", h.meta)
	mux.HandleFunc("GET /api/v1/installs", h.installs)
	mux.HandleFunc("GET /api/v1/installs/{ns}/{name}/overview", h.overview)
	mux.HandleFunc("GET /api/v1/installs/{ns}/{name}/health", h.health)
	mux.HandleFunc("GET /api/v1/installs/{ns}/{name}/resources/{group}/{kind}/{rns}/{rname}", h.resource)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) { writeErr(w, http.StatusNotFound, "not found") })
	mux.Handle("/", spa(ui))
	return mux
}

type handler struct {
	src   ModelSource
	authz Authorizer
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}

// model returns the current model, or writes 503 if none is ready.
func (h *handler) model(w http.ResponseWriter) *engine.Model {
	m := h.src.Current()
	if m == nil || !m.Snapshot.Synced {
		writeErr(w, http.StatusServiceUnavailable, "connecting to cluster: cache not synced")
		return nil
	}
	return m
}

func (h *handler) readyz(w http.ResponseWriter, _ *http.Request) {
	if m := h.src.Current(); m == nil || !m.Snapshot.Synced {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *handler) meta(w http.ResponseWriter, _ *http.Request) {
	m := h.src.Current()
	resp := MetaResponse{Version: version.Version, Kinds: []kinds.Served{deploymentKind}}
	if m != nil {
		resp.Pinned, resp.Synced = m.Pinned, m.Snapshot.Synced
		resp.Kinds = append(slices.Clone(m.Served), deploymentKind)
	}
	writeJSON(w, resp)
}

// visibleHealth returns the install's report as the caller of r may see it
// (D-013). A finding on a denied resource keeps its code, severity and ref but
// is restricted: message, detail and since are emptied. State and counts still
// cover every finding, so partial visibility never makes an install look healthier.
func (h *handler) visibleHealth(r *http.Request, i *engine.InstallModel) health.Report {
	if _, all := h.authz.(AllowAll); all {
		return i.Health
	}
	rep := i.Health
	rep.Findings = make([]health.Finding, 0, len(i.Health.Findings))
	for _, f := range i.Health.Findings {
		if f.Resource != nil && !h.authz.Authorize(r, *f.Resource) {
			f = health.Finding{Code: f.Code, Slug: f.Slug, Severity: f.Severity, Install: f.Install, Resource: f.Resource, Restricted: true}
		}
		rep.Findings = append(rep.Findings, f)
	}
	return rep
}

// visibleOverview rebuilds the overview over the owned objects the caller of r
// may see: denied objects leave the count tiles and the gateway rows.
func (h *handler) visibleOverview(r *http.Request, m *engine.Model, i *engine.InstallModel, rep health.Report) overview.Overview {
	if _, all := h.authz.(AllowAll); all {
		return i.Overview
	}
	can := func(u *unstructured.Unstructured) bool { return h.authz.Authorize(r, objects.RefOf(u)) }
	keep := func(us []*unstructured.Unstructured) []*unstructured.Unstructured {
		out := []*unstructured.Unstructured{}
		for _, u := range us {
			if can(u) {
				out = append(out, u)
			}
		}
		return out
	}
	keepObjs := func(os []install.OwnedObject) []install.OwnedObject {
		out := []install.OwnedObject{}
		for _, o := range os {
			if can(o.Obj) {
				out = append(out, o)
			}
		}
		return out
	}
	o := i.Owned
	owned := &install.Owned{GatewayClasses: keep(o.GatewayClasses), Gateways: keep(o.Gateways), ListenerSets: keep(o.ListenerSets),
		Routes: keepObjs(o.Routes), Policies: keepObjs(o.Policies), Unattached: o.Unattached}
	return overview.Build(i.Resolved, owned, m.Snapshot, m.Served, rep)
}

func (h *handler) installs(w http.ResponseWriter, r *http.Request) {
	m := h.model(w)
	if m == nil {
		return
	}
	out := []InstallSummary{}
	for k := range m.Installs {
		i := &m.Installs[k]
		hd, rep := i.Overview.Header, h.visibleHealth(r, i)
		out = append(out, InstallSummary{ID: hd.ID, Namespace: hd.Namespace, Name: hd.Name, Version: hd.Version, Edition: hd.Edition,
			ControllerNames: hd.ControllerNames, Controller: hd.Controller, Scope: hd.Scope, Health: HealthSummary{State: rep.State, Counts: rep.Counts},
			ComputedAt: m.ComputedAt})
	}
	writeJSON(w, out)
}

func (h *handler) find(w http.ResponseWriter, r *http.Request) (*engine.Model, *engine.InstallModel) {
	m := h.model(w)
	if m == nil {
		return nil, nil
	}
	id := r.PathValue("ns") + "/" + r.PathValue("name")
	for i := range m.Installs {
		if m.Installs[i].Resolved.ID() == id {
			return m, &m.Installs[i]
		}
	}
	writeErr(w, http.StatusNotFound, "install "+id+" not found")
	return nil, nil
}

func (h *handler) overview(w http.ResponseWriter, r *http.Request) {
	if m, i := h.find(w, r); i != nil {
		writeJSON(w, OverviewResponse{Overview: h.visibleOverview(r, m, i, h.visibleHealth(r, i)), CollectedAt: m.ComputedAt})
	}
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	if _, i := h.find(w, r); i != nil {
		writeJSON(w, h.visibleHealth(r, i))
	}
}

// resource serves one object of a served kind, or a Deployment, when the
// caller may view it and it is owned by the install or referenced by a current
// finding of the install (finding 11). Only
// metadata.managedFields is stripped (D-013). Secrets are never watched, so
// they are never reachable. "-" stands for the empty group or namespace.
func (h *handler) resource(w http.ResponseWriter, r *http.Request) {
	m, i := h.find(w, r)
	if i == nil {
		return
	}
	dash := func(s string) string {
		if s == "-" {
			return ""
		}
		return s
	}
	want := objects.Ref{Group: dash(r.PathValue("group")), Kind: r.PathValue("kind"), Namespace: dash(r.PathValue("rns")), Name: r.PathValue("rname")}
	if h.authz.Authorize(r, want) {
		if obj := lookup(m, i, want); obj != nil {
			writeJSON(w, obj)
			return
		}
	}
	// Denied and absent objects get the same answer.
	writeErr(w, http.StatusNotFound, "resource not found in this install's scope")
}

// lookup returns want as a JSON object, or nil if the install may not serve it.
func lookup(m *engine.Model, i *engine.InstallModel, want objects.Ref) map[string]any {
	o := i.Owned
	owned := append(append(append([]*unstructured.Unstructured{}, o.GatewayClasses...), o.Gateways...), o.ListenerSets...)
	for _, x := range slices.Concat(o.Routes, o.Policies) {
		owned = append(owned, x.Obj)
	}
	for _, u := range owned {
		if objects.RefOf(u) == want {
			return clean(u.Object)
		}
	}
	referenced := slices.ContainsFunc(i.Health.Findings, func(f health.Finding) bool { return f.Resource != nil && *f.Resource == want })
	if !referenced {
		return nil
	}
	if want.Group == deploymentKind.Group && want.Kind == deploymentKind.Kind {
		for _, d := range m.Snapshot.Deployments {
			if d.Namespace == want.Namespace && d.Name == want.Name {
				return deploymentObject(d)
			}
		}
		return nil
	}
	for _, k := range m.Served {
		if k.Group != want.Group || k.Kind != want.Kind {
			continue
		}
		for _, u := range m.Snapshot.Objects[k.GVR()] {
			if objects.RefOf(u) == want {
				return clean(u.Object)
			}
		}
	}
	return nil
}

// clean returns a copy of obj without metadata.managedFields.
func clean(obj map[string]any) map[string]any {
	c := (&unstructured.Unstructured{Object: obj}).DeepCopy()
	unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
	return c.Object
}

// deploymentObject returns d as served by the resource endpoint. Typed
// informers drop apiVersion and kind, so they are set here.
func deploymentObject(d *appsv1.Deployment) map[string]any {
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(d.DeepCopy())
	if err != nil {
		return nil
	}
	obj["apiVersion"], obj["kind"] = "apps/v1", "Deployment"
	return clean(obj)
}

// spa serves static files and falls back to index.html for client-side routes.
func spa(ui fs.FS) http.Handler {
	files := http.FileServerFS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := ui.Open(p); err == nil {
				f.Close()
				files.ServeHTTP(w, r)
				return
			}
		}
		b, err := fs.ReadFile(ui, "index.html")
		if err != nil {
			http.Error(w, "UI not built", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})
}
```

- [ ] **Step 6: Run the tests and confirm they pass**

Run: `go test ./pkg/... -race`
Expected: PASS, with the scale test well under 1 s.

- [ ] **Step 7: Commit**

```bash
git add pkg/overview pkg/engine pkg/api && git commit -m "feat(api): engine, overview and read-only JSON API"
```

---

### Task 11: Extension registry, application wiring, `main`, and the Go extension-seam test

**Files:**
- Create: `pkg/extension/extension.go`, `pkg/app/app.go`, `cmd/kgateway-ui/main.go`
- Modify: `internal/testutil/clients.go` (add `ClientsWith`)
- Test: `pkg/app/app_test.go` (extension seam), `pkg/extension/extension_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2–10.
- Produces:
  - `extension.Registry{Kinds []kinds.Descriptor; Detectors []install.Detector; Scope install.ScopeProvider; Verifier scopeverify.Verifier; Checks []health.Check; Authorizer api.Authorizer}`;
  - `extension.Option` (`func(*Registry)`);
  - `extension.WithKinds(...kinds.Descriptor)`, `WithDetectors(...install.Detector)` (prepended, so downstream detectors win), `WithScopeProvider(install.ScopeProvider)`, `WithScopeVerifier(scopeverify.Verifier)`, `WithChecks(...health.Check)`, `WithAuthorizer(api.Authorizer)` (replaces the default `api.AllowAll{}`; D-013);
  - `extension.New(opts ...Option) *Registry` (upstream defaults plus the options).
- Produces:
  - `app.Options{Listen, Pinned string; PollInterval, MetricsInterval time.Duration; UI fs.FS}`;
  - `app.Clients{Dynamic dynamic.Interface; Kube kubernetes.Interface; Discovery kinds.DiscoveryClient; InCluster bool}`;
  - `app.App{Engine *engine.Engine; Handler http.Handler}`;
  - `app.Build(c Clients, o Options, reg *extension.Registry) (*App, error)`, `(*App).Start(ctx context.Context)`. `Build` watches Pods with each detector's `ControllerPodSelector()`, sets `engine.Config.MetricsMaxAge` to 3 × `MetricsInterval`, and passes `reg.Authorizer` to `api.NewHandler`;
  - `app.Run(ctx context.Context, kubeconfig string, o Options, opts ...extension.Option) error`.
- Produces (tests only): `testutil.ClientsWith(t, extra []kinds.Descriptor, objs ...runtime.Object)`.

- [ ] **Step 1: Extend the test clients**

Replace the body of `Clients` in `internal/testutil/clients.go` with a call to the new function, and add it:

```go
// Clients returns fake clients serving the upstream kinds (see ClientsWith).
func Clients(t *testing.T, objs ...runtime.Object) (dynamic.Interface, *kubefake.Clientset) {
	return ClientsWith(t, nil, objs...)
}

// ClientsWith is Clients with extra kinds served by fake discovery, for
// downstream-extension tests.
func ClientsWith(t *testing.T, extra []kinds.Descriptor, objs ...runtime.Object) (dynamic.Interface, *kubefake.Clientset) {
	t.Helper()
	all := ServedUpstream()
	for _, d := range extra {
		all = append(all, kinds.Served{Descriptor: d, Version: d.Versions[0]})
	}
	listKinds := map[schema.GroupVersionResource]string{}
	byGV := map[string]*metav1.APIResourceList{}
	for _, s := range all {
		listKinds[s.GVR()] = s.Kind + "List"
		gv := schema.GroupVersion{Group: s.Group, Version: s.Version}.String()
		if byGV[gv] == nil {
			byGV[gv] = &metav1.APIResourceList{GroupVersion: gv}
		}
		byGV[gv].APIResources = append(byGV[gv].APIResources, metav1.APIResource{Name: s.Resource, Kind: s.Kind, Namespaced: s.Namespaced})
	}
	var dyn, typed []runtime.Object
	for _, o := range objs {
		if _, ok := o.(*unstructured.Unstructured); ok {
			dyn = append(dyn, o)
		} else {
			typed = append(typed, o)
		}
	}
	kube := kubefake.NewSimpleClientset(typed...)
	fd := kube.Discovery().(*fakediscovery.FakeDiscovery)
	for _, l := range byGV {
		fd.Resources = append(fd.Resources, l)
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, dyn...), kube
}
```

- [ ] **Step 2: Write the failing tests**

`pkg/extension/extension_test.go`:

```go
package extension

import (
	"net/http"
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

type otherDetector struct{ install.UpstreamDetector }

func (otherDetector) Name() string { return "other" }

func TestNewAppliesOptionsOnTopOfUpstream(t *testing.T) {
	r := New(WithKinds(kinds.Descriptor{Kind: "Extra"}), WithDetectors(otherDetector{}))
	if r.Kinds[len(r.Kinds)-1].Kind != "Extra" || len(r.Kinds) != len(kinds.Upstream())+1 {
		t.Fatal("extra kind must be appended to the upstream kinds")
	}
	if r.Detectors[0].Name() != "other" || r.Detectors[1].Name() != "kgateway" {
		t.Fatal("downstream detectors must be tried before the upstream detector")
	}
	if r.Scope == nil || r.Verifier == nil || len(r.Checks) == 0 {
		t.Fatal("upstream defaults missing")
	}
	if _, ok := r.Authorizer.(api.AllowAll); !ok {
		t.Fatalf("the default authorizer must be AllowAll (D-013), got %T", r.Authorizer)
	}
	deny := api.AuthorizerFunc(func(*http.Request, objects.Ref) bool { return false })
	if _, ok := New(WithAuthorizer(deny)).Authorizer.(api.AuthorizerFunc); !ok {
		t.Fatal("WithAuthorizer must replace the default")
	}
}
```

`pkg/app/app_test.go`. This is the extension-seam test (design §9): a test-only downstream registers a detector, a kind and a check, and all three show up in the API output. A second test registers a deny-some `Authorizer` (D-013, Review Focus 9).

```go
package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/extension"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

const acmeController = "acme.example.com/edge"

type acmeDetector struct{}

func (acmeDetector) Name() string                        { return "acme" }
func (acmeDetector) ControllerSelector() labels.Selector { return labels.SelectorFromSet(labels.Set{"app": "acme-edge"}) }
func (acmeDetector) ProxySelector() labels.Selector      { return labels.SelectorFromSet(labels.Set{"app": "acme-proxy"}) }

// ControllerPodSelector matches the Pods of the fixture Deployment, whose
// template comes from testutil.ControllerDeployment.
func (acmeDetector) ControllerPodSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"kgateway": "kgateway"})
}
func (acmeDetector) Detect(d *appsv1.Deployment) (install.Install, bool) {
	return install.Install{Namespace: d.Namespace, Name: d.Name, Edition: "acme", Version: "9.9.9", Container: "kgateway",
		ControllerNames: []string{acmeController}, Deployment: d, MetricsPort: 9092}, true
}

type acmeCheck struct{}

func (acmeCheck) Name() string { return "acme demo" }
func (acmeCheck) Run(in health.Input) ([]health.Finding, health.CheckResult) {
	return []health.Finding{{Code: "ACME-001", Slug: "demo", Severity: health.SeverityInfo, Install: in.Install.ID(), Message: "Downstream check ran"}},
		health.CheckResult{Name: "acme demo", OK: true, CollectedAt: in.Now}
}

var wafKind = kinds.Descriptor{Group: "policy.acme.example.com", Kind: "WafPolicy", Resource: "wafpolicies", Versions: []string{"v1"},
	Namespaced: true, Category: kinds.CategoryPolicy, DisplayName: "WAF policies"}

func TestDownstreamExtensionSeam(t *testing.T) {
	d := testutil.ControllerDeployment("acme-system", "edge", "example.com/acme/edge-controller:9.9.9", 1, 1)
	d.Labels = map[string]string{"app": "acme-edge"}
	waf := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "policy.acme.example.com/v1", "kind": "WafPolicy",
		"metadata": map[string]any{"name": "waf", "namespace": "shop"},
		"status": map[string]any{"ancestors": []any{map[string]any{
			"ancestorRef": map[string]any{"name": "edge", "namespace": "infra"}, "controllerName": acmeController, "conditions": []any{}}}},
	}}
	gw := testutil.GwRef("infra", "edge")
	dyn, kube := testutil.ClientsWith(t, []kinds.Descriptor{wafKind},
		testutil.Namespace("infra", nil), testutil.Namespace("shop", nil), d, testutil.ControllerPod(d, "edge-0", true),
		testutil.GatewayClass("acme", acmeController, true), testutil.Gateway("infra", "edge", "acme", nil),
		testutil.HTTPRoute("shop", "r", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, acmeController)), waf)

	reg := extension.New(extension.WithDetectors(acmeDetector{}), extension.WithKinds(wafKind), extension.WithChecks(acmeCheck{}))
	a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery()}, Options{PollInterval: 50 * time.Millisecond, MetricsInterval: time.Hour,
		UI: fstest.MapFS{"index.html": {Data: []byte("x")}}}, reg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.Start(ctx)
	srv := httptest.NewServer(a.Handler)
	defer srv.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		var insts []api.InstallSummary
		if resp, err := http.Get(srv.URL + "/api/v1/installs"); err == nil {
			if resp.StatusCode == 200 {
				_ = json.NewDecoder(resp.Body).Decode(&insts)
			}
			resp.Body.Close()
		}
		if len(insts) == 1 {
			if insts[0].Edition != "acme" {
				t.Fatalf("downstream detector not used: %+v", insts[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("install never appeared")
		}
		time.Sleep(50 * time.Millisecond)
	}
	var o api.OverviewResponse
	resp, _ := http.Get(srv.URL + "/api/v1/installs/acme-system/edge/overview")
	_ = json.NewDecoder(resp.Body).Decode(&o)
	resp.Body.Close()
	found := false
	for _, c := range o.Counts {
		if c.Kind == "WafPolicy" && c.Count == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("downstream kind not counted: %+v", o.Counts)
	}
	var h api.HealthResponse
	resp, _ = http.Get(srv.URL + "/api/v1/installs/acme-system/edge/health")
	_ = json.NewDecoder(resp.Body).Decode(&h)
	resp.Body.Close()
	for _, f := range h.Findings {
		if f.Code == "ACME-001" {
			return
		}
	}
	t.Fatalf("downstream check finding missing: %+v", h.Findings)
}

// getJSON decodes GET path into v and returns the status code.
func getJSON(t *testing.T, srv *httptest.Server, path string, v any) int {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK && v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode
}

// D-013 / Review Focus 9: a deny-some Authorizer registered through the
// extension hides the denied resource (404) and restricts its finding, while
// the roll-up still covers it.
func TestAuthorizerSeamRestrictsDeniedResources(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7", 1, 1)
	gw := testutil.GwRef("infra", "edge")
	broken := testutil.HTTPRoute("shop", "broken", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("ResolvedRefs", metav1.ConditionFalse, "BackendNotFound")}})
	dyn, kube := testutil.Clients(t,
		testutil.Namespace("kgw", nil), testutil.Namespace("infra", nil), testutil.Namespace("shop", nil),
		d, testutil.ControllerPod(d, "kgateway-0", true), testutil.ProxyDeployment("infra", "edge", 1, 1),
		testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.Gateway("infra", "edge", "kgateway", nil),
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)), broken)
	denied := objects.Ref{Group: objects.GatewayAPIGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "broken"}
	deny := api.AuthorizerFunc(func(_ *http.Request, ref objects.Ref) bool { return ref != denied })

	reg := extension.New(extension.WithAuthorizer(deny))
	a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery()}, Options{PollInterval: 50 * time.Millisecond, MetricsInterval: time.Hour,
		UI: fstest.MapFS{"index.html": {Data: []byte("x")}}}, reg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.Start(ctx)
	srv := httptest.NewServer(a.Handler)
	defer srv.Close()

	// Wait for the first model: the broken route's KGWUI-002 makes the state error.
	var h api.HealthResponse
	deadline := time.Now().Add(5 * time.Second)
	for {
		h = api.HealthResponse{}
		if getJSON(t, srv, "/api/v1/installs/kgw/kgateway/health", &h) == http.StatusOK && len(h.Findings) > 0 && h.State == health.StateError {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("health never settled: %+v", h)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var restricted *health.Finding
	for i := range h.Findings {
		if f := h.Findings[i]; f.Resource != nil && *f.Resource == denied {
			restricted = &h.Findings[i]
		}
	}
	if restricted == nil || !restricted.Restricted || restricted.Code != health.CodeConditionFalse || restricted.Severity != health.SeverityError ||
		restricted.Message != "" || restricted.Detail != "" || restricted.Since != nil {
		t.Fatalf("the denied route's finding must stay listed, restricted, with code/severity/ref and no message or evidence: %+v", restricted)
	}
	if h.State != health.StateError || h.Counts[health.SeverityError] < 1 {
		t.Fatalf("the roll-up must still cover the restricted finding: state=%s counts=%v", h.State, h.Counts)
	}
	if code := getJSON(t, srv, "/api/v1/installs/kgw/kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/broken", nil); code != http.StatusNotFound {
		t.Fatalf("a denied resource must 404, got %d", code)
	}
	if code := getJSON(t, srv, "/api/v1/installs/kgw/kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/cart", nil); code != http.StatusOK {
		t.Fatalf("an allowed resource must be served, got %d", code)
	}
	var o api.OverviewResponse
	getJSON(t, srv, "/api/v1/installs/kgw/kgateway/overview", &o)
	for _, c := range o.Counts {
		if c.Kind == "HTTPRoute" && c.Count != 1 {
			t.Fatalf("the denied route must leave the HTTPRoute count: %d", c.Count)
		}
	}
}
```

- [ ] **Step 3: Run them and confirm they fail**

Run: `go test ./pkg/extension/... ./pkg/app/...`
Expected: FAIL (undefined packages)

- [ ] **Step 4: Implement the registry**

`pkg/extension/extension.go`:

```go
// Package extension is the public seam through which downstream distributions
// extend the UI backend without forking it (D-003). A downstream main calls
// app.Run with options from this package.
package extension

import (
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

// Registry holds everything the backend is built from.
type Registry struct {
	Kinds     []kinds.Descriptor
	Detectors []install.Detector
	Scope     install.ScopeProvider
	Verifier  scopeverify.Verifier
	Checks    []health.Check
	// Authorizer decides per request what the caller may see (D-013).
	Authorizer api.Authorizer
}

// Option modifies a Registry.
type Option func(*Registry)

// WithKinds adds kinds to watch, count and serve.
func WithKinds(d ...kinds.Descriptor) Option {
	return func(r *Registry) { r.Kinds = append(r.Kinds, d...) }
}

// WithDetectors adds install detectors. They are tried before the upstream
// detector, so a downstream can claim Deployments the upstream one would also match.
func WithDetectors(d ...install.Detector) Option {
	return func(r *Registry) { r.Detectors = append(append([]install.Detector{}, d...), r.Detectors...) }
}

// WithScopeProvider replaces the scope provider.
func WithScopeProvider(p install.ScopeProvider) Option { return func(r *Registry) { r.Scope = p } }

// WithScopeVerifier replaces the scope verifier.
func WithScopeVerifier(v scopeverify.Verifier) Option { return func(r *Registry) { r.Verifier = v } }

// WithChecks adds health checks. Downstream finding codes must use their own prefix.
func WithChecks(c ...health.Check) Option {
	return func(r *Registry) { r.Checks = append(r.Checks, c...) }
}

// WithAuthorizer replaces the default api.AllowAll. Every API response with
// resources, findings or counts goes through it (D-013).
func WithAuthorizer(a api.Authorizer) Option { return func(r *Registry) { r.Authorizer = a } }

// New returns the upstream registry with opts applied.
func New(opts ...Option) *Registry {
	r := &Registry{
		Kinds:     kinds.Upstream(),
		Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope:     install.NamespaceSelectorScope{},
		// A mismatch must outlast two 10 s metric scrapes (design §4.4).
		Verifier:   scopeverify.NewManagedCountVerifier(20 * time.Second),
		Checks:     health.Upstream(),
		Authorizer: api.AllowAll{},
	}
	for _, o := range opts {
		o(r)
	}
	return r
}
```

- [ ] **Step 5: Implement the wiring and `main`**

`pkg/app/app.go`:

```go
// Package app wires the backend together. Both the upstream main and
// downstream mains use it; downstreams pass extension options.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/extension"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
)

// Options are the runtime settings.
type Options struct {
	Listen          string
	Pinned          string
	PollInterval    time.Duration
	MetricsInterval time.Duration
	UI              fs.FS
}

// Clients are the Kubernetes clients the app uses.
type Clients struct {
	Dynamic   dynamic.Interface
	Kube      kubernetes.Interface
	Discovery kinds.DiscoveryClient
	// InCluster selects direct pod-IP metric scraping; otherwise pods/proxy is used.
	InCluster bool
}

// App is a built, not yet started, backend.
type App struct {
	Engine    *engine.Engine
	Handler   http.Handler
	cache     *cache.Cache
	collector *ctrlmetrics.Collector
	opts      Options
}

// Build resolves the served kinds and constructs every component.
func Build(c Clients, o Options, reg *extension.Registry) (*App, error) {
	served, err := kinds.Resolve(c.Discovery, reg.Kinds)
	if err != nil {
		return nil, err
	}
	var podSel, depSel []labels.Selector
	for _, d := range reg.Detectors {
		podSel = append(podSel, d.ControllerPodSelector()) // A1: Pods may lack the Deployment's component label
		depSel = append(depSel, d.ControllerSelector(), d.ProxySelector())
	}
	ca := cache.New(cache.Config{Dynamic: c.Dynamic, Kube: c.Kube, Served: served, PodSelectors: podSel, DeploymentSelectors: depSel})
	var scraper ctrlmetrics.Scraper = ctrlmetrics.PodProxyScraper{Kube: c.Kube}
	if c.InCluster {
		scraper = ctrlmetrics.HTTPScraper{Client: &http.Client{Timeout: 5 * time.Second}}
	}
	col := ctrlmetrics.NewCollector(scraper)
	// Metrics older than three scrape intervals are unavailable, not reused (finding 3).
	eng := engine.New(engine.Config{Snapshots: ca, Served: served, Detectors: reg.Detectors, Scope: reg.Scope, Verifier: reg.Verifier,
		Metrics: col, Checks: reg.Checks, Pinned: o.Pinned, MetricsMaxAge: 3 * o.MetricsInterval})
	return &App{Engine: eng, Handler: api.NewHandler(eng, o.UI, reg.Authorizer), cache: ca, collector: col, opts: o}, nil
}

// Start runs the cache, the engine loop and the metrics collector until ctx is done.
func (a *App) Start(ctx context.Context) {
	a.cache.Start(ctx)
	go a.Engine.Run(ctx, a.opts.PollInterval)
	go a.collector.Run(ctx, a.opts.MetricsInterval, a.Engine.Installs)
}

// Run builds clients from kubeconfig (or the in-cluster config when empty and
// running in a pod), then serves until ctx is done.
func Run(ctx context.Context, kubeconfig string, o Options, opts ...extension.Option) error {
	cfg, inCluster, err := restConfig(kubeconfig)
	if err != nil {
		return err
	}
	if o.Listen == "" {
		o.Listen = "127.0.0.1:8080"
		if inCluster {
			o.Listen = ":8080"
		}
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}
	a, err := Build(Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), InCluster: inCluster}, o, extension.New(opts...))
	if err != nil {
		return err
	}
	a.Start(ctx)
	srv := &http.Server{Addr: o.Listen, Handler: a.Handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	slog.Info("kgateway UI listening", "addr", o.Listen, "inCluster", inCluster)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

func restConfig(kubeconfig string) (*rest.Config, bool, error) {
	if kubeconfig == "" {
		if c, err := rest.InClusterConfig(); err == nil {
			return c, true, nil
		}
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	c, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
	return c, false, err
}
```

`cmd/kgateway-ui/main.go`:

```go
// Command kgateway-ui serves the read-only kgateway UI.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/internal/webui"
	"github.com/DuncanDoyle/kgateway-ui/pkg/app"
)

func main() {
	var kubeconfig string
	o := app.Options{UI: webui.FS()}
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to a kubeconfig; empty uses the in-cluster config")
	flag.StringVar(&o.Pinned, "install", "", "pin the UI to one controller install (namespace/name)")
	flag.StringVar(&o.Listen, "listen", "", "listen address (default :8080 in-cluster, 127.0.0.1:8080 locally)")
	flag.DurationVar(&o.PollInterval, "poll-interval", 2*time.Second, "model recompute interval")
	flag.DurationVar(&o.MetricsInterval, "metrics-interval", 10*time.Second, "controller metrics scrape interval")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, kubeconfig, o); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 6: Run the full backend suite, vet and lint**

Run: `go vet ./... && go test ./... -race && golangci-lint run ./...`
Expected: all PASS, no lint findings.

- [ ] **Step 7: Smoke test against a real cluster**

Run: `make run` against a cluster with kgateway installed (for example the e2e kind cluster from Task 22, or any dev cluster). Then run `curl -s localhost:8080/api/v1/installs | jq .`
Expected: the install is listed with its version and a health state. Record the output in the sub-phase notes.

- [ ] **Step 8: Commit, and close sub-phase 1.1**

```bash
git add pkg/extension pkg/app cmd internal/testutil && git commit -m "feat(app): extension registry, wiring and main"
```

Update `docs/phase-status.md`: mark sub-phase 1.1 complete and write `docs/phases/phase-1/phase-1.1-complete.md` from the template.

---

# Sub-phase 1.2 — Frontend

All commands run from `web/` unless stated otherwise. The frontend follows the team conventions: one component per file with a named export, `// region Styles` / `// region Helpers` / `// region Component`, emotion styled components, a `stylesOverride` prop on every exported component, typography from theme tokens, and no native tooltips.

### Task 12: Frontend workspace scaffold

**Files:**
- Create: `web/package.json`, `web/tsconfig.base.json`, `web/biome.json`, `web/vitest.workspace.ts`
- Create: `web/packages/shell/package.json`, `web/packages/shell/tsconfig.json`, `web/packages/shell/src/index.ts`, `web/packages/shell/src/test/setup.ts`
- Create: `web/app/package.json`, `web/app/tsconfig.json`, `web/app/vite.config.ts`, `web/app/index.html`, `web/app/src/main.tsx`, `web/app/src/vite-env.d.ts`
- Test: `web/packages/shell/src/index.test.ts`

**Interfaces:**
- Produces: the npm workspace `@kgateway-ui/shell`. Its `main` is `src/index.ts`, consumed as TypeScript source by `app` and, later, by downstream apps. It is not published in Phase 1.
- Produces: `npm run build` → `web/app/dist`; `npm test`; `npm run lint`.

- [ ] **Step 1: Create the workspace files**

`web/package.json`:

```json
{
  "name": "kgateway-ui-web",
  "private": true,
  "workspaces": ["packages/*", "app"],
  "scripts": {
    "build": "npm run build -w app",
    "dev": "npm run dev -w app",
    "test": "vitest run",
    "lint": "biome check .",
    "typecheck": "tsc -p packages/shell && tsc -p app"
  }
}
```

`web/tsconfig.base.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022", "lib": ["ES2022", "DOM", "DOM.Iterable"], "module": "ESNext", "moduleResolution": "Bundler",
    "jsx": "react-jsx", "jsxImportSource": "@emotion/react", "strict": true, "noUncheckedIndexedAccess": true,
    "noEmit": true, "skipLibCheck": true, "resolveJsonModule": true, "isolatedModules": true, "types": ["vitest/globals"]
  }
}
```

`web/biome.json`:

```json
{
  "$schema": "https://biomejs.dev/schemas/2.0.0/schema.json",
  "files": { "includes": ["**", "!**/dist", "!**/node_modules"] },
  "formatter": { "indentStyle": "space", "indentWidth": 2, "lineWidth": 120 },
  "linter": { "enabled": true, "rules": { "recommended": true } }
}
```

`web/vitest.workspace.ts`:

```ts
export default ["packages/shell"];
```

`web/packages/shell/package.json`:

```json
{
  "name": "@kgateway-ui/shell",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "main": "src/index.ts",
  "types": "src/index.ts",
  "peerDependencies": {
    "@emotion/react": "^11", "@emotion/styled": "^11", "react": ">=19", "react-dom": ">=19",
    "react-router-dom": ">=7", "swr": "^2", "lucide-react": "*"
  }
}
```

`web/packages/shell/tsconfig.json`:

```json
{ "extends": "../../tsconfig.base.json", "include": ["src"] }
```

`web/packages/shell/src/test/setup.ts`:

```ts
import "@testing-library/jest-dom/vitest";
```

Add a `vitest.config.ts` in `web/packages/shell`:

```ts
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react({ jsxImportSource: "@emotion/react" })],
  test: { environment: "jsdom", globals: true, setupFiles: ["src/test/setup.ts"] },
});
```

`web/app/package.json`:

```json
{
  "name": "kgateway-ui-app",
  "private": true,
  "type": "module",
  "scripts": { "dev": "vite", "build": "tsc -p . && vite build" },
  "dependencies": { "@kgateway-ui/shell": "*" }
}
```

`web/app/tsconfig.json`:

```json
{ "extends": "../tsconfig.base.json", "include": ["src", "vite.config.ts"] }
```

`web/app/src/vite-env.d.ts` (finding 8: declares Vite's asset imports, such as the SVG logo in Task 15, for the app's typecheck; `src` is already included):

```ts
/// <reference types="vite/client" />
```

`web/app/vite.config.ts`:

```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// In dev, the Go backend runs on :8080 (make run); the Vite dev server proxies the API to it.
export default defineConfig({
  plugins: [react({ jsxImportSource: "@emotion/react" })],
  server: { proxy: { "/api": "http://127.0.0.1:8080" } },
  build: { outDir: "dist", sourcemap: true },
});
```

`web/app/index.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>kgateway</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

`web/app/src/main.tsx` (replaced in Task 15):

```tsx
import { createRoot } from "react-dom/client";

createRoot(document.getElementById("root") as HTMLElement).render(<p>kgateway UI</p>);
```

- [ ] **Step 2: Install the dependencies**

```bash
cd web
npm install -w @kgateway-ui/shell -D react@^19 react-dom@^19 @emotion/react@^11 @emotion/styled@^11 react-router-dom@^7 swr@^2 lucide-react \
  @tanstack/react-table@^8 @tanstack/react-virtual@^3 js-yaml@^4 @fontsource/figtree @fontsource/jetbrains-mono
npm install -w kgateway-ui-app react@^19 react-dom@^19 @emotion/react@^11 @emotion/styled@^11 react-router-dom@^7 swr@^2 lucide-react \
  @tanstack/react-table@^8 @tanstack/react-virtual@^3 js-yaml@^4 @fontsource/figtree @fontsource/jetbrains-mono
npm install -D typescript@^5 vite@^7 @vitejs/plugin-react vitest@^3 jsdom @testing-library/react@^16 @testing-library/jest-dom \
  @testing-library/user-event @biomejs/biome@^2 @types/react @types/react-dom @types/js-yaml @playwright/test
```

- [ ] **Step 3: Write the failing smoke test**

`web/packages/shell/src/index.test.ts`:

```ts
import * as shell from "./index";

it("exports the shell API", () => {
  expect(shell.SHELL_API_VERSION).toBe(1);
});
```

- [ ] **Step 4: Run it and confirm it fails**

Run: `npm test`
Expected: FAIL (`SHELL_API_VERSION` undefined)

- [ ] **Step 5: Implement**

`web/packages/shell/src/index.ts`:

```ts
/** Version of the shell's extension contract; bumped on breaking changes (D-003). */
export const SHELL_API_VERSION = 1;
```

- [ ] **Step 6: Run the tests, lint, typecheck and build**

Run: `npm test && npm run lint && npm run typecheck && npm run build`
Expected: PASS, and `web/app/dist/index.html` exists.

- [ ] **Step 7: Commit**

```bash
git add web && git commit -m "chore(web): npm workspace with shell package and app"
```

---

### Task 13: Theme tokens, Control room themes, fonts, contrast tests

**Files:**
- Create: `web/packages/shell/src/theme/tokens.ts`, `web/packages/shell/src/theme/themes.ts`, `web/packages/shell/src/theme/contrast.ts`, `web/packages/shell/src/theme/ThemeProvider.tsx`, `web/packages/shell/src/theme/emotion.d.ts`
- Modify: `web/packages/shell/src/index.ts`
- Test: `web/packages/shell/src/theme/themes.test.ts`, `web/packages/shell/src/theme/ThemeProvider.test.tsx`

**Interfaces:**
- Produces:
  - `type HealthState = "healthy" | "warning" | "error" | "unknown"`;
  - `interface StatusTokens { fg: string; iconBg: string; iconFg: string; stripBg: string; stripBorder: string }`;
  - `interface Theme { id: string; name: string; mode: "dark" | "light"; color: { bg; surface; surfaceRaised; border; text; textMuted; accent; accentText; headerBg; headerText; focus: string; status: Record<HealthState, StatusTokens> }; font: { ui: string; mono: string; size: FontSizes; weight: FontWeights }; radius: { sm: string; md: string }; space: (n: number) => string }`;
  - `interface FontSizes { xs; sm; code; md; base; lg; xl: string }` and `interface FontWeights { regular; semibold; bold: number }`: the type scale every component uses instead of literal sizes and weights (finding 17, D-008);
  - the Emotion theme typing (`theme/emotion.d.ts`) is referenced from the shell's `index.ts`, so the app and downstream apps that compile the shell source get it (finding 8);
  - `controlRoomDark: Theme`, `controlRoomLight: Theme`, `defaultThemes: Theme[]`;
  - `contrastRatio(a: string, b: string): number`;
  - `<ThemeProvider themes={Theme[]} defaultThemeId?: string>`;
  - `useThemeSwitch(): { themes: Theme[]; current: Theme; setTheme(id: string): void }`.

- [ ] **Step 1: Write the failing tests**

`web/packages/shell/src/theme/themes.test.ts`:

```ts
import { contrastRatio } from "./contrast";
import { controlRoomDark, controlRoomLight } from "./themes";
import type { HealthState } from "./tokens";

const states: HealthState[] = ["healthy", "warning", "error", "unknown"];

it("computes WCAG contrast", () => {
  expect(contrastRatio("#000000", "#ffffff")).toBeCloseTo(21, 0);
  expect(contrastRatio("#777777", "#ffffff")).toBeCloseTo(4.48, 1);
});

describe.each([controlRoomDark, controlRoomLight])("$name theme meets WCAG AA", (t) => {
  const c = t.color;
  it.each([
    ["text on bg", c.text, c.bg],
    ["text on surface", c.text, c.surface],
    ["muted on bg", c.textMuted, c.bg],
    ["muted on surface", c.textMuted, c.surface],
    ["header text", c.headerText, c.headerBg],
    ["accent text on bg", c.accentText, c.bg],
  ])("%s ≥ 4.5", (_, fg, bg) => {
    expect(contrastRatio(fg, bg)).toBeGreaterThanOrEqual(4.5);
  });
  it.each(states)("%s status text ≥ 4.5 on bg, surface and its strip", (s) => {
    const st = c.status[s];
    expect(contrastRatio(st.fg, c.bg)).toBeGreaterThanOrEqual(4.5);
    expect(contrastRatio(st.fg, c.surface)).toBeGreaterThanOrEqual(4.5);
    expect(contrastRatio(c.text, st.stripBg)).toBeGreaterThanOrEqual(4.5);
  });
  it.each(states)("%s status icon glyph ≥ 3 (non-text graphic)", (s) => {
    expect(contrastRatio(c.status[s].iconFg, c.status[s].iconBg)).toBeGreaterThanOrEqual(3);
  });
});

it("dark is the default and first theme", async () => {
  const { defaultThemes } = await import("./themes");
  expect(defaultThemes[0]?.id).toBe("control-room-dark");
});

it.each([controlRoomDark, controlRoomLight])("$name defines the full type scale", (t) => {
  expect(Object.keys(t.font.size).sort()).toEqual(["base", "code", "lg", "md", "sm", "xl", "xs"]);
  expect(Object.keys(t.font.weight).sort()).toEqual(["bold", "regular", "semibold"]);
});
```

`web/packages/shell/src/theme/ThemeProvider.test.tsx`:

```tsx
import { act, render, screen } from "@testing-library/react";
import { ThemeProvider, useThemeSwitch } from "./ThemeProvider";
import { defaultThemes } from "./themes";

function Probe() {
  const { current, setTheme } = useThemeSwitch();
  return (
    <button type="button" onClick={() => setTheme("control-room-light")}>
      {current.id}
    </button>
  );
}

it("defaults to dark, switches, and remembers the choice", () => {
  localStorage.clear();
  const { unmount } = render(
    <ThemeProvider themes={defaultThemes}>
      <Probe />
    </ThemeProvider>,
  );
  expect(screen.getByRole("button")).toHaveTextContent("control-room-dark");
  act(() => screen.getByRole("button").click());
  expect(screen.getByRole("button")).toHaveTextContent("control-room-light");
  unmount();
  render(
    <ThemeProvider themes={defaultThemes}>
      <Probe />
    </ThemeProvider>,
  );
  expect(screen.getByRole("button")).toHaveTextContent("control-room-light");
});

it("survives localStorage throwing", () => {
  const spy = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
    throw new Error("blocked");
  });
  render(
    <ThemeProvider themes={defaultThemes}>
      <Probe />
    </ThemeProvider>,
  );
  expect(screen.getByRole("button")).toHaveTextContent("control-room-dark");
  spy.mockRestore();
});
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `npm test`
Expected: FAIL (modules not found)

- [ ] **Step 3: Implement**

`web/packages/shell/src/theme/tokens.ts`:

```ts
/** The four health states, always rendered as icon + colour + text. */
export type HealthState = "healthy" | "warning" | "error" | "unknown";

/** Colours for one health state. */
export interface StatusTokens {
  /** Status text colour (AA on bg and surface). */
  fg: string;
  /** Background of the status icon chip. */
  iconBg: string;
  /** Glyph colour on iconBg (≥ 3:1). */
  iconFg: string;
  /** Background and border of the health strip in this state. */
  stripBg: string;
  stripBorder: string;
}

/** Type scale. Components never use literal font sizes (D-008). */
export interface FontSizes {
  /** Table headers and overlines. */
  xs: string;
  /** Secondary text, badges, chips, tooltips. */
  sm: string;
  /** Mono resource names and YAML. */
  code: string;
  /** Section headings. */
  md: string;
  /** Body text. */
  base: string;
  /** Page-state titles. */
  lg: string;
  /** Count tile numbers. */
  xl: string;
}

/** Font weights. Components never use literal weights (D-008). */
export interface FontWeights {
  regular: number;
  semibold: number;
  bold: number;
}

/** A complete theme. All visual identity flows from these tokens (D-008). */
export interface Theme {
  id: string;
  name: string;
  mode: "dark" | "light";
  color: {
    bg: string;
    surface: string;
    surfaceRaised: string;
    border: string;
    text: string;
    textMuted: string;
    accent: string;
    accentText: string;
    headerBg: string;
    headerText: string;
    focus: string;
    status: Record<HealthState, StatusTokens>;
  };
  font: { ui: string; mono: string; size: FontSizes; weight: FontWeights };
  radius: { sm: string; md: string };
  space: (n: number) => string;
}
```

`web/packages/shell/src/theme/themes.ts`:

```ts
import type { Theme } from "./tokens";

const font = {
  ui: "'Figtree Variable', Figtree, system-ui, sans-serif",
  mono: "'JetBrains Mono Variable', 'JetBrains Mono', ui-monospace, monospace",
  size: { xs: "10px", sm: "12px", code: "12.5px", md: "13px", base: "14px", lg: "18px", xl: "22px" },
  weight: { regular: 400, semibold: 600, bold: 700 },
};
const radius = { sm: "4px", md: "8px" };
const space = (n: number) => `${n * 4}px`;

/** "Control room" dark: logo ink #151927 and logo blue #002DC9 (D-011). The default theme. */
export const controlRoomDark: Theme = {
  id: "control-room-dark",
  name: "Control room",
  mode: "dark",
  color: {
    bg: "#0F1220",
    surface: "#151927",
    surfaceRaised: "#1B2033",
    border: "#232A44",
    text: "#DCE1F2",
    textMuted: "#8B93B3",
    accent: "#002DC9",
    accentText: "#9FB4FF",
    headerBg: "#151927",
    headerText: "#DCE1F2",
    focus: "#9FB4FF",
    status: {
      healthy: { fg: "#7ED6A8", iconBg: "#7ED6A8", iconFg: "#0F1220", stripBg: "#11261C", stripBorder: "#245A3E" },
      warning: { fg: "#F2C46B", iconBg: "#F2C46B", iconFg: "#0F1220", stripBg: "#2A2212", stripBorder: "#5C4A1F" },
      error: { fg: "#FF8C84", iconBg: "#FF8C84", iconFg: "#0F1220", stripBg: "#2A1418", stripBorder: "#5A2A30" },
      unknown: { fg: "#A3AAC4", iconBg: "#A3AAC4", iconFg: "#0F1220", stripBg: "#1B2033", stripBorder: "#343B57" },
    },
  },
  font,
  radius,
  space,
};

/** "Control room" light: light body, with the ink header band kept as the brand. */
export const controlRoomLight: Theme = {
  id: "control-room-light",
  name: "Control room light",
  mode: "light",
  color: {
    bg: "#FFFFFF",
    surface: "#F5F7FC",
    surfaceRaised: "#FFFFFF",
    border: "#DDE2EE",
    text: "#151927",
    textMuted: "#5A6280",
    accent: "#002DC9",
    accentText: "#002DC9",
    headerBg: "#151927",
    headerText: "#FFFFFF",
    focus: "#002DC9",
    status: {
      healthy: { fg: "#24693F", iconBg: "#2F855A", iconFg: "#FFFFFF", stripBg: "#EAF6EF", stripBorder: "#B9DEC8" },
      warning: { fg: "#7A4F0D", iconBg: "#9A6514", iconFg: "#FFFFFF", stripBg: "#FFF6E0", stripBorder: "#EBD49A" },
      error: { fg: "#A52A23", iconBg: "#C2372F", iconFg: "#FFFFFF", stripBg: "#FDEEEE", stripBorder: "#F0C2BF" },
      unknown: { fg: "#565B6B", iconBg: "#6B7080", iconFg: "#FFFFFF", stripBg: "#F0F2F7", stripBorder: "#D5DAE6" },
    },
  },
  font,
  radius,
  space,
};

/** Upstream themes; the first is the default. */
export const defaultThemes: Theme[] = [controlRoomDark, controlRoomLight];
```

If a contrast test fails for a token, adjust that single hex value (darker for the light theme, lighter for the dark one) until it passes. Never relax the test.

`web/packages/shell/src/theme/contrast.ts`:

```ts
// region Helpers
function channel(c: number): number {
  const s = c / 255;
  return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
}

function luminance(hex: string): number {
  const h = hex.replace("#", "");
  const [r, g, b] = [0, 2, 4].map((i) => Number.parseInt(h.slice(i, i + 2), 16));
  return 0.2126 * channel(r ?? 0) + 0.7152 * channel(g ?? 0) + 0.0722 * channel(b ?? 0);
}

/** WCAG 2.x contrast ratio between two #RRGGBB colours. */
export function contrastRatio(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}
```

`web/packages/shell/src/theme/emotion.d.ts`:

```ts
import "@emotion/react";
import type { Theme as KgTheme } from "./tokens";

declare module "@emotion/react" {
  export interface Theme extends KgTheme {}
}
```

`web/packages/shell/src/theme/ThemeProvider.tsx`:

```tsx
import { Global, ThemeProvider as EmotionThemeProvider, css } from "@emotion/react";
import "@fontsource/figtree";
import "@fontsource/jetbrains-mono";
import { createContext, type ReactNode, useContext, useMemo, useState } from "react";
import type { Theme } from "./tokens";

// region Helpers
const STORAGE_KEY = "kgateway-ui.theme";

// Browser storage is a per-viewer convenience only; it may be blocked.
function readStored(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
}

function store(id: string) {
  try {
    localStorage.setItem(STORAGE_KEY, id);
  } catch {
    // ignore: the choice just won't persist
  }
}

interface ThemeSwitch {
  themes: Theme[];
  current: Theme;
  setTheme: (id: string) => void;
}

const ThemeSwitchContext = createContext<ThemeSwitch | null>(null);

/** Returns the available themes, the current one and a setter. */
export function useThemeSwitch(): ThemeSwitch {
  const ctx = useContext(ThemeSwitchContext);
  if (!ctx) throw new Error("useThemeSwitch must be used inside ThemeProvider");
  return ctx;
}

const globalStyles = (t: Theme) => css`
  html, body, #root { height: 100%; margin: 0; }
  body { background: ${t.color.bg}; color: ${t.color.text}; font-family: ${t.font.ui}; font-size: ${t.font.size.base}; font-weight: ${t.font.weight.regular}; }
  *, *::before, *::after { box-sizing: border-box; }
  :focus-visible { outline: 2px solid ${t.color.focus}; outline-offset: 2px; }
  * { scrollbar-width: thin; scrollbar-color: ${t.color.border} transparent; }
  @media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
`;

// region Component
interface ThemeProviderProps {
  themes: Theme[];
  defaultThemeId?: string;
  children: ReactNode;
}

/** Provides the emotion theme, global styles and the theme switch. */
export function ThemeProvider({ themes, defaultThemeId, children }: ThemeProviderProps) {
  const initial = readStored() ?? defaultThemeId ?? themes[0]?.id;
  const [id, setId] = useState(initial);
  const current = themes.find((t) => t.id === id) ?? (themes[0] as Theme);
  const value = useMemo<ThemeSwitch>(
    () => ({
      themes,
      current,
      setTheme: (next) => {
        setId(next);
        store(next);
      },
    }),
    [themes, current],
  );
  return (
    <ThemeSwitchContext.Provider value={value}>
      <EmotionThemeProvider theme={current}>
        <Global styles={globalStyles(current)} />
        {children}
      </EmotionThemeProvider>
    </ThemeSwitchContext.Provider>
  );
}
```

Add the exports to `web/packages/shell/src/index.ts`, with the reference directive as the file's first line (triple-slash directives must precede every statement). A `.d.ts` file cannot be imported as a value, so the reference is what carries the Emotion theme typing to every program that compiles the shell source (finding 8):

```ts
/// <reference path="./theme/emotion.d.ts" />
export type { FontSizes, FontWeights, HealthState, StatusTokens, Theme } from "./theme/tokens";
export { controlRoomDark, controlRoomLight, defaultThemes } from "./theme/themes";
export { contrastRatio } from "./theme/contrast";
export { ThemeProvider, useThemeSwitch } from "./theme/ThemeProvider";
```

- [ ] **Step 4: Run the tests and the typecheck and confirm they pass**

Run: `npm test && npm run typecheck`
Expected: PASS. From Task 15 on, the app's typecheck compiles the shell through its import of `@kgateway-ui/shell`, so it fails on theme properties such as `p.theme.font` if the `emotion.d.ts` reference is missing.

- [ ] **Step 5: Commit**

```bash
git add web && git commit -m "feat(web): Control room theme tokens with contrast tests"
```

---

### Task 14: Health primitives, ResourceName, API types and data hooks

**Files:**
- Create: `web/packages/shell/src/health/HealthIcon.tsx`, `web/packages/shell/src/health/HealthBadge.tsx`, `web/packages/shell/src/health/state.ts`
- Create: `web/packages/shell/src/resource/ResourceName.tsx`
- Create: `web/packages/shell/src/api/types.ts`, `web/packages/shell/src/api/client.ts`, `web/packages/shell/src/api/hooks.ts`, `web/packages/shell/src/api/clock.ts`
- Create: `web/packages/shell/src/test/render.tsx` (test helper)
- Modify: `web/packages/shell/src/index.ts`
- Test: `web/packages/shell/src/health/HealthBadge.test.tsx`, `web/packages/shell/src/resource/ResourceName.test.tsx`, `web/packages/shell/src/api/hooks.test.tsx`

**Interfaces:**
- Consumes: `Theme`, `HealthState`, `ThemeProvider`.
- Produces:
  - `<HealthIcon state size? stylesOverride?>`, `<HealthBadge state label? stylesOverride?>`;
  - `stateLabel(state): string`, `severityToState(sev): HealthState`;
  - `<ResourceName namespace? name kind? stylesOverride?>`.
- Produces (TypeScript mirrors of the Go JSON in Task 10):
  - `Ref`, `Finding`, `CheckResult`, `HealthReport`, `InstallSummary`, `Count`, `GatewayRow`, `OverviewResponse`, `MetaResponse`, `ScopeSummary`, `Severity`.
- Produces:
  - `apiFetch<T>(path): Promise<T>` (throws `ApiError{status, message}`);
  - `useInstalls()`, `useOverview(id)`, `useHealth(id)`, `useMeta()`, `useResource(id, ref)` (SWR, refresh every 5 s except `useMeta` and `useResource`). None keeps the previous key's data: on an install switch the hooks return no data until the new install's response arrives (finding 4);
  - `POLL_MS = 5000`;
  - `isStale(collectedAt: string, now: number): boolean` (older than 2 × `POLL_MS`);
  - `isLinkable(meta: MetaResponse | undefined, r: Ref): boolean` (true when `/api/v1/meta` lists `r`'s group and kind; the frontend links only those; finding 11);
  - `useNow(): number` in `api/clock.ts`: the current time at 1 s resolution from one shared interval. Every staleness check renders with it, so data ages into the stale state even when SWR returns identical data and does not re-render (round 3, finding 4).
- The TypeScript mirrors carry `Finding.restricted`, `HealthReport.install`, `CheckResult.pending`, `InstallSummary.controller`, `InstallSummary.computedAt` and the `apps/Deployment` entry in `MetaResponse.kinds` (Task 10).
- Produces (tests only): `renderWithTheme(ui, opts?)`.

- [ ] **Step 1: Write the test helper and the failing tests**

`web/packages/shell/src/test/render.tsx`:

```tsx
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter } from "react-router-dom";
import { SWRConfig } from "swr";
import { ThemeProvider } from "../theme/ThemeProvider";
import { defaultThemes } from "../theme/themes";

/** Renders ui with the default themes, a fresh SWR cache and a router at `path`. */
export function renderWithTheme(ui: ReactElement, opts: { path?: string } = {}) {
  return render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <ThemeProvider themes={defaultThemes}>
        <MemoryRouter initialEntries={[opts.path ?? "/"]}>{ui}</MemoryRouter>
      </ThemeProvider>
    </SWRConfig>,
  );
}

/** Stubs fetch with a map of path → JSON body (status 200) or [status, body]. */
export function stubApi(routes: Record<string, unknown>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const r = routes[url];
      if (r === undefined) return new Response(JSON.stringify({ error: "not found" }), { status: 404 });
      const [status, body] = Array.isArray(r) && typeof r[0] === "number" ? (r as [number, unknown]) : [200, r];
      return new Response(JSON.stringify(body), { status });
    }),
  );
}
```

`web/packages/shell/src/health/HealthBadge.test.tsx`:

```tsx
import { screen } from "@testing-library/react";
import { renderWithTheme } from "../test/render";
import { HealthBadge } from "./HealthBadge";

it.each([
  ["healthy", "Healthy"],
  ["warning", "Warning"],
  ["error", "Error"],
  ["unknown", "Unknown"],
] as const)("%s renders an icon and text, never colour alone", (state, text) => {
  renderWithTheme(<HealthBadge state={state} />);
  expect(screen.getByText(text)).toBeInTheDocument();
  expect(screen.getByTestId(`health-icon-${state}`)).toBeInTheDocument();
});
```

`web/packages/shell/src/resource/ResourceName.test.tsx`:

```tsx
import { screen } from "@testing-library/react";
import { renderWithTheme } from "../test/render";
import { ResourceName } from "./ResourceName";

it("renders the namespace dimmed and the name in mono", () => {
  renderWithTheme(<ResourceName namespace="shop" name="cart" />);
  expect(screen.getByText("shop/")).toHaveAttribute("data-part", "namespace");
  expect(screen.getByText("cart")).toHaveAttribute("data-part", "name");
});

it("renders cluster-scoped names without a namespace", () => {
  renderWithTheme(<ResourceName name="kgateway" />);
  expect(screen.queryByText("/", { exact: false })).toBeNull();
});
```

`web/packages/shell/src/api/hooks.test.tsx`:

```tsx
import { act, screen, waitFor } from "@testing-library/react";
import { renderWithTheme, stubApi } from "../test/render";
import { useNow } from "./clock";
import { isLinkable, isStale, POLL_MS } from "./hooks";
import { useInstalls } from "./hooks";

function Probe() {
  const { data, error } = useInstalls();
  if (error) return <p>error {error.status}</p>;
  return <p>{data ? `n=${data.length}` : "loading"}</p>;
}

it("loads installs", async () => {
  stubApi({ "/api/v1/installs": [{ id: "kgw/kgateway" }] });
  renderWithTheme(<Probe />);
  await waitFor(() => expect(screen.getByText("n=1")).toBeInTheDocument());
});

it("surfaces API errors with their status", async () => {
  stubApi({ "/api/v1/installs": [503, { error: "connecting to cluster: cache not synced" }] });
  renderWithTheme(<Probe />);
  await waitFor(() => expect(screen.getByText("error 503")).toBeInTheDocument());
});

it("marks data older than two poll intervals as stale", () => {
  const now = Date.parse("2026-10-17T12:00:00Z");
  expect(isStale(new Date(now - POLL_MS).toISOString(), now)).toBe(false);
  expect(isStale(new Date(now - 2 * POLL_MS - 1).toISOString(), now)).toBe(true);
});

it("useNow ticks every second from one shared clock", async () => {
  vi.useFakeTimers();
  vi.setSystemTime(Date.parse("2026-10-17T12:00:00Z"));
  function Clock() {
    return <p>{new Date(useNow()).toISOString()}</p>;
  }
  renderWithTheme(<><Clock /><Clock /></>);
  expect(screen.getAllByText("2026-10-17T12:00:00.000Z")).toHaveLength(2);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(3000);
  });
  expect(screen.getAllByText("2026-10-17T12:00:03.000Z")).toHaveLength(2);
  vi.useRealTimers();
});

it("links only kinds that /api/v1/meta lists", () => {
  const meta = { version: "dev", synced: true, kinds: [{ group: "apps", kind: "Deployment", category: "workload", displayName: "Deployments" }] };
  expect(isLinkable(meta, { group: "apps", kind: "Deployment", namespace: "kgw", name: "kgateway" })).toBe(true);
  expect(isLinkable(meta, { group: "", kind: "Secret", namespace: "kgw", name: "x" })).toBe(false);
  expect(isLinkable(undefined, { group: "apps", kind: "Deployment", name: "kgateway" })).toBe(false);
});
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `npm test`
Expected: FAIL (modules not found)

- [ ] **Step 3: Implement the health primitives**

`web/packages/shell/src/health/state.ts`:

```ts
import type { HealthState } from "../theme/tokens";

/** Display text per state. */
export function stateLabel(s: HealthState): string {
  return { healthy: "Healthy", warning: "Warning", error: "Error", unknown: "Unknown" }[s];
}

/** Maps a finding severity to the state used to render it. Info renders as unknown-coloured. */
export function severityToState(sev: "error" | "warning" | "info"): HealthState {
  return sev === "error" ? "error" : sev === "warning" ? "warning" : "unknown";
}
```

`web/packages/shell/src/health/HealthIcon.tsx`:

```tsx
import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { AlertTriangle, Check, HelpCircle, X } from "lucide-react";
import type { HealthState } from "../theme/tokens";

// region Styles
const Chip = styled.span<{ state: HealthState; size: number; stylesOverride?: SerializedStyles }>`
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: ${(p) => p.size}px;
  height: ${(p) => p.size}px;
  border-radius: ${(p) => p.theme.radius.sm};
  background: ${(p) => p.theme.color.status[p.state].iconBg};
  color: ${(p) => p.theme.color.status[p.state].iconFg};
  ${(p) => p.stylesOverride}
`;

// region Helpers
const glyphs = { healthy: Check, warning: AlertTriangle, error: X, unknown: HelpCircle } as const;

// region Component
interface HealthIconProps {
  state: HealthState;
  size?: number;
  stylesOverride?: SerializedStyles;
}

/** The icon half of the health vocabulary; always paired with text by callers. */
export function HealthIcon({ state, size = 16, stylesOverride }: HealthIconProps) {
  const Glyph = glyphs[state];
  return (
    <Chip state={state} size={size} stylesOverride={stylesOverride} data-testid={`health-icon-${state}`} aria-hidden="true">
      <Glyph size={size - 5} strokeWidth={3} />
    </Chip>
  );
}
```

`web/packages/shell/src/health/HealthBadge.tsx`:

```tsx
import styled from "@emotion/styled";
import type { SerializedStyles } from "@emotion/react";
import type { HealthState } from "../theme/tokens";
import { HealthIcon } from "./HealthIcon";
import { stateLabel } from "./state";

// region Styles
const Badge = styled.span<{ state: HealthState; stylesOverride?: SerializedStyles }>`
  display: inline-flex;
  align-items: center;
  gap: ${(p) => p.theme.space(1.5)};
  font-weight: ${(p) => p.theme.font.weight.semibold};
  font-size: ${(p) => p.theme.font.size.sm};
  white-space: nowrap;
  color: ${(p) => p.theme.color.status[p.state].fg};
  ${(p) => p.stylesOverride}
`;

// region Component
interface HealthBadgeProps {
  state: HealthState;
  label?: string;
  stylesOverride?: SerializedStyles;
}

/** Icon + colour + text for a health state. */
export function HealthBadge({ state, label, stylesOverride }: HealthBadgeProps) {
  return (
    <Badge state={state} stylesOverride={stylesOverride}>
      <HealthIcon state={state} />
      {label ?? stateLabel(state)}
    </Badge>
  );
}
```

`web/packages/shell/src/resource/ResourceName.tsx`:

```tsx
import styled from "@emotion/styled";
import type { SerializedStyles } from "@emotion/react";

// region Styles
const Name = styled.span<{ stylesOverride?: SerializedStyles }>`
  font-family: ${(p) => p.theme.font.mono};
  font-size: ${(p) => p.theme.font.size.code};
  white-space: nowrap;
  [data-part="namespace"] { opacity: 0.55; }
  ${(p) => p.stylesOverride}
`;

// region Component
interface ResourceNameProps {
  namespace?: string;
  name: string;
  stylesOverride?: SerializedStyles;
}

/** The signature element: mono resource name with the namespace dimmed (D-011). */
export function ResourceName({ namespace, name, stylesOverride }: ResourceNameProps) {
  return (
    <Name stylesOverride={stylesOverride}>
      {namespace ? <span data-part="namespace">{namespace}/</span> : null}
      <span data-part="name">{name}</span>
    </Name>
  );
}
```

- [ ] **Step 4: Implement the API layer**

`web/packages/shell/src/api/types.ts`:

```ts
import type { HealthState } from "../theme/tokens";

/** Mirrors pkg/api and pkg/health JSON (Task 10). */
export type Severity = "error" | "warning" | "info";
export interface Ref { group: string; kind: string; namespace?: string; name: string }
/** restricted: the caller may not view the resource; message and detail are empty (D-013). */
export interface Finding {
  code: string; slug: string; severity: Severity; install: string; resource?: Ref;
  message: string; detail?: string; since?: string; restricted?: boolean;
}
/** pending: not OK only because resources are still being processed (round 2, finding 2). */
export interface CheckResult { name: string; ok: boolean; required: boolean; collectedAt: string; error?: string; pending?: boolean }
/** install is the install ID; a response for another install is never rendered (finding 4). */
export interface HealthReport {
  install: string; state: HealthState; findings: Finding[]; checks: CheckResult[]; counts: Partial<Record<Severity, number>>; collectedAt: string;
}
export interface ScopeSummary { mode: "all" | "selectors" | "unknown"; selectors?: unknown[]; error?: string; watched: number; total: number }
export interface ReadyCount { ready: number; desired: number }
/** computedAt: when the backend computed this health; stale summaries render as unknown (round 2, finding 6). */
export interface InstallSummary {
  id: string; namespace: string; name: string; version: string; edition: string; controllerNames: string[];
  controller: ReadyCount; scope: ScopeSummary; health: { state: HealthState; counts: Partial<Record<Severity, number>> }; computedAt: string;
}
export interface Count { group: string; kind: string; category: string; displayName: string; count: number; access: { ok: boolean; error?: string } }
export interface GatewayRow { ref: Ref; class: string; state: HealthState; listeners: number; attachedRoutes: number; addresses: string[] | null }
export interface OverviewResponse {
  header: {
    id: string; namespace: string; name: string; version: string; edition: string; controllerNames: string[];
    controller: ReadyCount; scope: ScopeSummary;
  };
  counts: Count[]; gateways: GatewayRow[]; collectedAt: string;
}
/** kinds: what the resource endpoint can serve (served kinds plus apps/Deployment). */
export interface MetaResponse { version: string; pinned?: string; synced: boolean; kinds: { group: string; kind: string; category: string; displayName: string }[] }
```

`web/packages/shell/src/api/client.ts`:

```ts
/** Error thrown for non-2xx API responses. */
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

/** Fetches JSON from the backend API. */
export async function apiFetch<T>(path: string): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, { headers: { Accept: "application/json" } });
  } catch (e) {
    throw new ApiError(0, `Cannot reach the UI backend: ${(e as Error).message}`);
  }
  if (!res.ok) {
    let msg = res.statusText;
    try {
      msg = ((await res.json()) as { error?: string }).error ?? msg;
    } catch {
      // body was not JSON
    }
    throw new ApiError(res.status, msg);
  }
  return (await res.json()) as T;
}
```

`web/packages/shell/src/api/hooks.ts`:

```ts
import useSWR from "swr";
import { ApiError, apiFetch } from "./client";
import type { HealthReport, InstallSummary, MetaResponse, OverviewResponse, Ref } from "./types";

/** Poll interval for live data (design §6.6). */
export const POLL_MS = 5000;

// No keepPreviousData: on an install switch it would show the old install's data under the new URL (finding 4).
const live = { refreshInterval: POLL_MS };

/** True when data collected at collectedAt is older than two poll intervals. */
export function isStale(collectedAt: string, now: number = Date.now()): boolean {
  return now - Date.parse(collectedAt) > 2 * POLL_MS;
}

/** True when /api/v1/meta lists r's kind, so its YAML view can resolve (finding 11). */
export function isLinkable(meta: MetaResponse | undefined, r: Ref): boolean {
  return !!meta?.kinds.some((k) => k.group === r.group && k.kind === r.kind);
}

/** GET /api/v1/installs. */
export function useInstalls() {
  return useSWR<InstallSummary[], ApiError>("/api/v1/installs", apiFetch, live);
}

/** GET /api/v1/meta. */
export function useMeta() {
  return useSWR<MetaResponse, ApiError>("/api/v1/meta", apiFetch);
}

/** GET /api/v1/installs/{id}/overview. */
export function useOverview(id: string) {
  return useSWR<OverviewResponse, ApiError>(`/api/v1/installs/${id}/overview`, apiFetch, live);
}

/** GET /api/v1/installs/{id}/health. */
export function useHealth(id: string) {
  return useSWR<HealthReport, ApiError>(`/api/v1/installs/${id}/health`, apiFetch, live);
}

/** GET one resource; "-" stands for an empty group or namespace. */
export function useResource(id: string, r: Ref) {
  const path = `/api/v1/installs/${id}/resources/${r.group || "-"}/${r.kind}/${r.namespace || "-"}/${r.name}`;
  return useSWR<Record<string, unknown>, ApiError>(path, apiFetch);
}
```

`web/packages/shell/src/api/clock.ts`:

```ts
import { useSyncExternalStore } from "react";

// region Helpers
// One interval for the whole app, started by the first subscriber and stopped by the last.
const listeners = new Set<() => void>();
let timer: ReturnType<typeof setInterval> | undefined;

function subscribe(onTick: () => void): () => void {
  listeners.add(onTick);
  timer ??= setInterval(() => {
    for (const l of listeners) l();
  }, 1000);
  return () => {
    listeners.delete(onTick);
    if (listeners.size === 0 && timer !== undefined) {
      clearInterval(timer);
      timer = undefined;
    }
  };
}

// Whole seconds, so the snapshot is stable between ticks (useSyncExternalStore requires it).
function snapshot(): number {
  return Math.floor(Date.now() / 1000) * 1000;
}

// region Component
/** The current time at 1 s resolution. Renders with it re-run every second, so staleness is re-evaluated even when data is unchanged. */
export function useNow(): number {
  return useSyncExternalStore(subscribe, snapshot, snapshot);
}
```

Export all of these from `index.ts`:

```ts
export { HealthIcon } from "./health/HealthIcon";
export { HealthBadge } from "./health/HealthBadge";
export { severityToState, stateLabel } from "./health/state";
export { ResourceName } from "./resource/ResourceName";
export * from "./api/types";
export { ApiError, apiFetch } from "./api/client";
export { isLinkable, isStale, POLL_MS, useHealth, useInstalls, useMeta, useOverview, useResource } from "./api/hooks";
export { useNow } from "./api/clock";
```

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `npm test && npm run lint && npm run typecheck`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add web && git commit -m "feat(web): health primitives, resource name and API hooks"
```

---

### Task 15: App shell — extension registry, routing, header, switchers, page states

**Files:**
- Create: `web/packages/shell/src/shell/extension.ts`, `web/packages/shell/src/shell/Shell.tsx`, `web/packages/shell/src/shell/Header.tsx`, `web/packages/shell/src/shell/InstallSwitcher.tsx`, `web/packages/shell/src/shell/ThemeSwitcher.tsx`, `web/packages/shell/src/shell/Tooltip.tsx`, `web/packages/shell/src/shell/RootRedirect.tsx`
- Create: `web/packages/shell/src/states/PageState.tsx`
- Modify: `web/packages/shell/src/index.ts`, `web/app/src/main.tsx`
- Create: `web/app/src/brand.tsx`, `web/app/src/assets/kgateway-icon.svg` (the colour icon from the CNCF artwork repo `projects/kgateway/icon/color/kgateway-icon-color.svg`, unmodified)
- Test: `web/packages/shell/src/shell/Shell.test.tsx`

**Interfaces:**
- Consumes: Tasks 13 and 14.
- Produces:
  - `interface Brand { productName: string; logo: ReactNode }`;
  - `interface RouteContribution { path: string; element: ReactNode; scope: "install" | "top" }`;
  - `interface NavEntry { label: string; to: string; icon?: ReactNode; scope: "install" | "top" }`;
  - `type FindingRenderer = (f: Finding) => ReactNode`;
  - `interface ShellExtension { brand?: Brand; themes?: Theme[]; defaultThemeId?: string; routes?: RouteContribution[]; navEntries?: NavEntry[]; findingRenderers?: Record<string, FindingRenderer> }` (finding renderers are keyed by code prefix, e.g. `"ACME-"`);
  - `<Shell extension={ShellExtension} pages={CorePages} stylesOverride? />` (round 2, finding 9: the override contract covers every exported component, `Shell` included);
  - `interface CorePages { ClusterLanding: ComponentType; InstallPage: ComponentType; FindingsPage: ComponentType; ResourcePage: ComponentType }`. Task 16–17 supply the defaults: `defaultPages`.
  - `useShellExtension(): Required<ShellExtension>` (merged with defaults);
  - `useInstallId(): string` (from `/i/:ns/:name`);
  - `<PageState kind="loading" | "connecting" | "error" | "empty" title message? copyText? stylesOverride?>` and `<Shimmer h? stylesOverride?>`;
  - `<Tooltip label stylesOverride?>`, the app tooltip (never a native `title`);
  - the header shows, for the current install, the version, the edition and the controller readiness ("Controller {ready}/{desired} ready" with a health icon) from `InstallSummary` (finding 12);
  - the install switcher and the header readiness icon render a summary whose `computedAt` is older than 2 × `POLL_MS` as unknown (round 2, finding 6).

- [ ] **Step 1: Write the failing tests**

`web/packages/shell/src/shell/Shell.test.tsx`:

```tsx
import { css } from "@emotion/react";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SWRConfig } from "swr";
import { stubApi } from "../test/render";
import { Shell } from "./Shell";
import type { CorePages } from "./extension";

const pages: CorePages = {
  ClusterLanding: () => <p>cluster landing</p>,
  InstallPage: () => <p>install page</p>,
  FindingsPage: () => <p>findings page</p>,
  ResourcePage: () => <p>resource page</p>,
};
const inst = (id: string, controller = { ready: 2, desired: 2 }, computedAt = new Date().toISOString()) => ({ id, namespace: id.split("/")[0],
  name: id.split("/")[1], version: "2.4.7", edition: "kgateway", controllerNames: [], controller, scope: { mode: "all", watched: 3, total: 3 },
  health: { state: "healthy", counts: {} }, computedAt });

function mount(path: string, ext = {}) {
  window.history.pushState({}, "", path);
  return render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <Shell extension={ext} pages={pages} />
    </SWRConfig>,
  );
}

it("redirects / to the only install", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": { version: "dev", synced: true, kinds: [] } });
  mount("/");
  await waitFor(() => expect(screen.getByText("install page")).toBeInTheDocument());
  expect(window.location.pathname).toBe("/i/kgw/kgateway");
});

it("shows the cluster landing and an install switcher when there are several installs", async () => {
  stubApi({ "/api/v1/installs": [inst("a/one"), inst("b/two")], "/api/v1/meta": { version: "dev", synced: true, kinds: [] } });
  mount("/");
  await waitFor(() => expect(screen.getByText("cluster landing")).toBeInTheDocument());
  mount("/i/a/one");
  await userEvent.click(await screen.findByRole("button", { name: /switch install/i }));
  expect(screen.getByRole("menuitem", { name: /b\/two/ })).toBeInTheDocument();
});

it("shows a designed empty state when no install is found", async () => {
  stubApi({ "/api/v1/installs": [], "/api/v1/meta": { version: "dev", synced: true, kinds: [] } });
  mount("/");
  await waitFor(() => expect(screen.getByText(/No kgateway installation found/)).toBeInTheDocument());
  expect(screen.getByText(/--install/)).toBeInTheDocument();
});

it("shows 'Connecting to cluster' while the backend cache is not synced", async () => {
  stubApi({ "/api/v1/installs": [503, { error: "connecting to cluster: cache not synced" }], "/api/v1/meta": { version: "dev", synced: false, kinds: [] } });
  mount("/");
  await waitFor(() => expect(screen.getByText(/Connecting to cluster/)).toBeInTheDocument());
});

it("renders extension brand and routes", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": { version: "dev", synced: true, kinds: [] } });
  mount("/i/kgw/kgateway/waf", {
    brand: { productName: "Acme Edge", logo: <span>A</span> },
    routes: [{ path: "waf", element: <p>waf page</p>, scope: "install" }],
    navEntries: [{ label: "WAF", to: "waf", scope: "install" }],
  });
  await waitFor(() => expect(screen.getByText("waf page")).toBeInTheDocument());
  expect(screen.getByText("Acme Edge")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "WAF" })).toHaveAttribute("href", "/i/kgw/kgateway/waf");
});

it("shows the install's edition and controller readiness in the header", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway", { ready: 1, desired: 2 })], "/api/v1/meta": { version: "dev", synced: true, kinds: [] } });
  mount("/i/kgw/kgateway");
  expect(await screen.findByTestId("edition")).toHaveTextContent("kgateway");
  const ready = screen.getByText("Controller 1/2 ready");
  expect(within(ready).getByTestId("health-icon-error")).toBeInTheDocument();
});

it("renders a stale install summary as unknown in the switcher and the header", async () => {
  const old = new Date(Date.now() - 60_000).toISOString();
  stubApi({ "/api/v1/installs": [inst("a/one", { ready: 2, desired: 2 }, old), inst("b/two", { ready: 2, desired: 2 }, old)],
    "/api/v1/meta": { version: "dev", synced: true, kinds: [] } });
  mount("/i/a/one");
  const ready = await screen.findByText("Controller 2/2 ready");
  expect(within(ready).getByTestId("health-icon-unknown")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: /switch install/i }));
  const item = screen.getByRole("menuitem", { name: /b\/two/ });
  expect(within(item).getByTestId("health-icon-unknown")).toBeInTheDocument();
  expect(within(item).queryByTestId("health-icon-healthy")).toBeNull();
});

it("applies stylesOverride to the shell root", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": { version: "dev", synced: true, kinds: [] } });
  window.history.pushState({}, "", "/i/kgw/kgateway");
  render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <Shell pages={pages} stylesOverride={css`outline: 3px solid rgb(1, 2, 3);`} />
    </SWRConfig>,
  );
  expect(await screen.findByTestId("shell-root")).toHaveStyle({ outline: "3px solid rgb(1, 2, 3)" });
});

it("switches theme from the theme menu", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": { version: "dev", synced: true, kinds: [] } });
  mount("/i/kgw/kgateway");
  await userEvent.click(await screen.findByRole("button", { name: /theme/i }));
  await userEvent.click(screen.getByRole("menuitemradio", { name: /Control room light/ }));
  expect(screen.getByRole("button", { name: /theme/i })).toBeInTheDocument();
});
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `npm test`
Expected: FAIL

- [ ] **Step 3: Implement the extension types and the Tooltip**

`web/packages/shell/src/shell/extension.ts`:

```ts
import { type ComponentType, createContext, type ReactNode, useContext } from "react";
import { useParams } from "react-router-dom";
import type { Finding } from "../api/types";
import { defaultThemes } from "../theme/themes";
import type { Theme } from "../theme/tokens";

/** Product identity shown in the header (D-008). */
export interface Brand {
  productName: string;
  logo: ReactNode;
}

/** A page contributed by an extension. Install-scoped paths are relative to /i/:ns/:name/. */
export interface RouteContribution {
  path: string;
  element: ReactNode;
  scope: "install" | "top";
}

/** A navigation entry contributed by an extension. */
export interface NavEntry {
  label: string;
  to: string;
  icon?: ReactNode;
  scope: "install" | "top";
}

/** Renders extra detail for findings whose code starts with a registered prefix. */
export type FindingRenderer = (f: Finding) => ReactNode;

/** Everything a downstream app can contribute to the shell (D-003). */
export interface ShellExtension {
  brand?: Brand;
  themes?: Theme[];
  defaultThemeId?: string;
  routes?: RouteContribution[];
  navEntries?: NavEntry[];
  findingRenderers?: Record<string, FindingRenderer>;
}

/** The core pages; the upstream implementations are exported as defaultPages. */
export interface CorePages {
  ClusterLanding: ComponentType;
  InstallPage: ComponentType;
  FindingsPage: ComponentType;
  ResourcePage: ComponentType;
}

export const defaultBrand: Brand = { productName: "kgateway", logo: null };

export const ShellExtensionContext = createContext<Required<ShellExtension>>({
  brand: defaultBrand,
  themes: defaultThemes,
  defaultThemeId: defaultThemes[0]?.id ?? "",
  routes: [],
  navEntries: [],
  findingRenderers: {},
});

/** The merged extension. */
export function useShellExtension(): Required<ShellExtension> {
  return useContext(ShellExtensionContext);
}

/** The install ID ("ns/name") of the current /i/:ns/:name route. */
export function useInstallId(): string {
  const { ns, name } = useParams();
  return `${ns}/${name}`;
}
```

`web/packages/shell/src/shell/Tooltip.tsx`:

```tsx
import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { type ReactNode, useId, useState } from "react";

// region Styles
const Wrap = styled.span<{ stylesOverride?: SerializedStyles }>`
  position: relative;
  display: inline-flex;
  ${(p) => p.stylesOverride}
`;
const Tip = styled.span`
  position: absolute;
  top: calc(100% + 6px);
  left: 50%;
  transform: translateX(-50%);
  z-index: 20;
  padding: 4px 8px;
  border-radius: ${(p) => p.theme.radius.sm};
  background: ${(p) => p.theme.color.surfaceRaised};
  border: 1px solid ${(p) => p.theme.color.border};
  color: ${(p) => p.theme.color.text};
  font-size: ${(p) => p.theme.font.size.sm};
  white-space: nowrap;
  pointer-events: none;
`;

// region Component
interface TooltipProps {
  label: string;
  children: ReactNode;
  stylesOverride?: SerializedStyles;
}

/** App tooltip shown on hover and focus. Native title tooltips are not used. */
export function Tooltip({ label, children, stylesOverride }: TooltipProps) {
  const [open, setOpen] = useState(false);
  const id = useId();
  return (
    <Wrap
      stylesOverride={stylesOverride}
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
      onFocus={() => setOpen(true)}
      onBlur={() => setOpen(false)}
      aria-describedby={open ? id : undefined}
    >
      {children}
      {open ? (
        <Tip role="tooltip" id={id}>
          {label}
        </Tip>
      ) : null}
    </Wrap>
  );
}
```

- [ ] **Step 4: Implement the page states**

`web/packages/shell/src/states/PageState.tsx`:

```tsx
import styled from "@emotion/styled";
import { keyframes, type SerializedStyles } from "@emotion/react";
import { Copy } from "lucide-react";
import type { ReactNode } from "react";
import { HealthIcon } from "../health/HealthIcon";

// region Styles
const shimmer = keyframes`from { opacity: .45 } to { opacity: .9 }`;
const Center = styled.div<{ stylesOverride?: SerializedStyles }>`
  min-height: 60vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: ${(p) => p.theme.space(3)};
  text-align: center;
  color: ${(p) => p.theme.color.textMuted};
  h2 { color: ${(p) => p.theme.color.text}; font-size: ${(p) => p.theme.font.size.lg}; font-weight: ${(p) => p.theme.font.weight.semibold}; margin: 0; }
  p { max-width: 560px; margin: 0; line-height: 1.5; }
  code { font-family: ${(p) => p.theme.font.mono}; color: ${(p) => p.theme.color.text}; }
  ${(p) => p.stylesOverride}
`;
const Spinner = styled.div`
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 3px solid ${(p) => p.theme.color.border};
  border-top-color: ${(p) => p.theme.color.accentText};
  animation: ${keyframes`to { transform: rotate(360deg) }`} 0.9s linear infinite;
`;
const CopyButton = styled.button`
  display: inline-flex; align-items: center; gap: 6px; padding: 6px 10px; cursor: pointer;
  border-radius: ${(p) => p.theme.radius.sm}; border: 1px solid ${(p) => p.theme.color.border};
  background: ${(p) => p.theme.color.surface}; color: ${(p) => p.theme.color.text}; font: inherit;
  &:hover { border-color: ${(p) => p.theme.color.textMuted}; }
  &:active { background: ${(p) => p.theme.color.surfaceRaised}; }
`;
/** Loading placeholder with the final layout's height. */
export const Shimmer = styled.div<{ h?: number; stylesOverride?: SerializedStyles }>`
  height: ${(p) => p.h ?? 16}px;
  border-radius: ${(p) => p.theme.radius.sm};
  background: ${(p) => p.theme.color.surfaceRaised};
  animation: ${shimmer} 0.9s ease-in-out infinite alternate;
  ${(p) => p.stylesOverride}
`;

// region Component
interface PageStateProps {
  kind: "loading" | "connecting" | "error" | "empty";
  title: string;
  message?: ReactNode;
  copyText?: string;
  stylesOverride?: SerializedStyles;
}

/** Designed full-page loading, connecting, error and empty states. */
export function PageState({ kind, title, message, copyText, stylesOverride }: PageStateProps) {
  return (
    <Center role={kind === "error" ? "alert" : "status"} stylesOverride={stylesOverride}>
      {kind === "loading" || kind === "connecting" ? <Spinner aria-hidden="true" /> : null}
      {kind === "error" ? <HealthIcon state="error" size={28} /> : null}
      {kind === "empty" ? <HealthIcon state="unknown" size={28} /> : null}
      <h2>{title}</h2>
      {message ? <p>{message}</p> : null}
      {copyText ? (
        <CopyButton type="button" onClick={() => navigator.clipboard?.writeText(copyText)}>
          <Copy size={14} /> Copy error
        </CopyButton>
      ) : null}
    </Center>
  );
}
```

- [ ] **Step 5: Implement the header, the switchers, the redirect and the shell**

`web/packages/shell/src/shell/InstallSwitcher.tsx`:

```tsx
import styled from "@emotion/styled";
import { ChevronsUpDown } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useNow } from "../api/clock";
import { isStale, useInstalls } from "../api/hooks";
import { HealthIcon } from "../health/HealthIcon";
import { ResourceName } from "../resource/ResourceName";
import { Tooltip } from "./Tooltip";

// region Styles
export const MenuButton = styled.button`
  display: inline-flex; align-items: center; gap: 8px; height: 32px; padding: 0 10px; cursor: pointer; font: inherit;
  color: ${(p) => p.theme.color.headerText}; background: transparent;
  border: 1px solid ${(p) => p.theme.color.border}; border-radius: ${(p) => p.theme.radius.sm};
`;
export const Menu = styled.div`
  position: absolute; top: calc(100% + 6px); right: 0; z-index: 30; min-width: 260px; padding: 4px;
  background: ${(p) => p.theme.color.surfaceRaised}; border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md}; box-shadow: 0 8px 24px rgba(0,0,0,.35);
  [role^="menuitem"] {
    display: flex; align-items: center; gap: 8px; width: 100%; padding: 8px; cursor: pointer; font: inherit; text-align: left;
    background: transparent; border: 0; border-radius: ${(p) => p.theme.radius.sm}; color: ${(p) => p.theme.color.text};
  }
  [role^="menuitem"]:hover { background: ${(p) => p.theme.color.surface}; }
`;
const Anchor = styled.div` position: relative; `;

// region Component
interface InstallSwitcherProps {
  currentId: string;
}

/** Switches between controller installs; hidden when there is only one (D-009 §5). */
export function InstallSwitcher({ currentId }: InstallSwitcherProps) {
  const { data } = useInstalls();
  const now = useNow();
  const [open, setOpen] = useState(false);
  const navigate = useNavigate();
  if (!data || data.length < 2) return null;
  return (
    <Anchor>
      <Tooltip label="Switch controller install">
        <MenuButton type="button" aria-label="Switch install" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)}>
          <ChevronsUpDown size={14} />
        </MenuButton>
      </Tooltip>
      {open ? (
        <Menu role="menu">
          {data.map((i) => (
            <button key={i.id} type="button" role="menuitem" aria-current={i.id === currentId}
              onClick={() => { setOpen(false); navigate(`/i/${i.id}`); }}>
              <HealthIcon state={isStale(i.computedAt, now) ? "unknown" : i.health.state} />
              <ResourceName namespace={i.namespace} name={i.name} />
            </button>
          ))}
        </Menu>
      ) : null}
    </Anchor>
  );
}
```

`web/packages/shell/src/shell/ThemeSwitcher.tsx`:

```tsx
import { Palette } from "lucide-react";
import { useState } from "react";
import styled from "@emotion/styled";
import { useThemeSwitch } from "../theme/ThemeProvider";
import { Menu, MenuButton } from "./InstallSwitcher";
import { Tooltip } from "./Tooltip";

// region Styles
const Anchor = styled.div` position: relative; `;

// region Component
/** Compact icon dropdown listing every theme (team rule: not a light/dark toggle). */
export function ThemeSwitcher() {
  const { themes, current, setTheme } = useThemeSwitch();
  const [open, setOpen] = useState(false);
  return (
    <Anchor>
      <Tooltip label={`Theme: ${current.name}`}>
        <MenuButton type="button" aria-label="Theme" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)}>
          <Palette size={14} />
        </MenuButton>
      </Tooltip>
      {open ? (
        <Menu role="menu">
          {themes.map((t) => (
            <button key={t.id} type="button" role="menuitemradio" aria-checked={t.id === current.id}
              onClick={() => { setTheme(t.id); setOpen(false); }}>
              {t.name}
            </button>
          ))}
        </Menu>
      ) : null}
    </Anchor>
  );
}
```

`web/packages/shell/src/shell/Header.tsx`:

```tsx
import styled from "@emotion/styled";
import { Link, useMatch } from "react-router-dom";
import { useNow } from "../api/clock";
import { isStale, useInstalls } from "../api/hooks";
import type { ReadyCount } from "../api/types";
import { HealthIcon } from "../health/HealthIcon";
import { ResourceName } from "../resource/ResourceName";
import { useShellExtension } from "./extension";
import { InstallSwitcher } from "./InstallSwitcher";
import { ThemeSwitcher } from "./ThemeSwitcher";

// region Styles
const Bar = styled.header`
  display: flex; align-items: center; gap: ${(p) => p.theme.space(3)}; height: 52px; padding: 0 ${(p) => p.theme.space(4)};
  background: ${(p) => p.theme.color.headerBg}; color: ${(p) => p.theme.color.headerText};
  border-bottom: 1px solid ${(p) => p.theme.color.border};
`;
const Brand = styled(Link)`
  display: inline-flex; align-items: center; gap: 8px; color: inherit; text-decoration: none; font-weight: ${(p) => p.theme.font.weight.bold}; letter-spacing: -0.01em;
  img, svg { height: 24px; width: auto; }
  &:hover { opacity: 0.85; }
  &:active { opacity: 0.7; }
`;
const Chip = styled.span`
  padding: 2px 8px; border-radius: ${(p) => p.theme.radius.sm}; font-size: ${(p) => p.theme.font.size.sm};
  background: ${(p) => p.theme.color.accent}; color: ${(p) => p.theme.color.headerText};
`;
const Tag = styled.span`
  padding: 1px 7px; border-radius: ${(p) => p.theme.radius.sm}; font-size: ${(p) => p.theme.font.size.sm};
  border: 1px solid ${(p) => p.theme.color.border}; color: ${(p) => p.theme.color.headerText};
`;
const Ready = styled.span`
  display: inline-flex; align-items: center; gap: 6px; font-size: ${(p) => p.theme.font.size.sm}; color: ${(p) => p.theme.color.headerText};
`;
const Spacer = styled.div` flex: 1; `;
const Nav = styled.nav`
  display: flex; gap: ${(p) => p.theme.space(1)};
  a { display: inline-flex; align-items: center; gap: 6px; padding: 6px 10px; border-radius: ${(p) => p.theme.radius.sm};
      color: ${(p) => p.theme.color.headerText}; text-decoration: none; }
  a:hover { background: ${(p) => p.theme.color.surfaceRaised}; }
`;

// region Helpers
/** Matches KGWUI-005: fewer ready replicas than desired is an error. */
function readiness(c: ReadyCount): "healthy" | "error" {
  return c.ready >= c.desired ? "healthy" : "error";
}

// region Component
/** Header: brand, install identity, version, edition and controller readiness, extension navigation, switchers. */
export function Header() {
  const { brand, navEntries } = useShellExtension();
  const match = useMatch("/i/:ns/:name/*");
  const { data } = useInstalls();
  const now = useNow();
  const id = match ? `${match.params.ns}/${match.params.name}` : "";
  const current = data?.find((i) => i.id === id);
  return (
    <Bar>
      <Brand to="/">
        {brand.logo}
        <span>{brand.productName}</span>
      </Brand>
      {current ? (
        <>
          <Chip>v{current.version}</Chip>
          <Tag data-testid="edition">{current.edition}</Tag>
          <ResourceName namespace={current.namespace} name={current.name} />
          <Ready>
            <HealthIcon state={isStale(current.computedAt, now) ? "unknown" : readiness(current.controller)} />
            Controller {current.controller.ready}/{current.controller.desired} ready
          </Ready>
        </>
      ) : null}
      <Nav aria-label="Extensions">
        {navEntries
          .filter((n) => n.scope === "top" || id)
          .map((n) => (
            <Link key={n.to} to={n.scope === "install" ? `/i/${id}/${n.to}` : `/${n.to}`}>
              {n.icon}
              {n.label}
            </Link>
          ))}
      </Nav>
      <Spacer />
      {id ? <InstallSwitcher currentId={id} /> : null}
      <ThemeSwitcher />
    </Bar>
  );
}
```

`web/packages/shell/src/shell/RootRedirect.tsx`:

```tsx
import { Navigate } from "react-router-dom";
import { useInstalls, useMeta } from "../api/hooks";
import { PageState } from "../states/PageState";
import type { CorePages } from "./extension";

// region Component
/** "/" → the single (or pinned) install, else the cluster landing (D-011). */
export function RootRedirect({ ClusterLanding }: Pick<CorePages, "ClusterLanding">) {
  const { data, error } = useInstalls();
  const { data: meta } = useMeta();
  if (error?.status === 503) return <PageState kind="connecting" title="Connecting to cluster…" message={error.message} />;
  if (error) return <PageState kind="error" title="Cannot load installs" message={error.message} copyText={error.message} />;
  if (!data) return <PageState kind="loading" title="Loading…" />;
  if (data.length === 0) {
    return (
      <PageState kind="empty" title="No kgateway installation found"
        message={<>The UI looks for controller Deployments labelled <code>kgateway=kgateway</code> and{" "}
          <code>app.kubernetes.io/component=controller</code>.{meta?.pinned ? <> It is pinned to <code>{meta.pinned}</code> with <code>--install</code>, which was not found.</> : <> To show one specific install, start the UI with <code>--install namespace/name</code>.</>}</>} />
    );
  }
  if (data.length === 1) return <Navigate to={`/i/${data[0]?.id}`} replace />;
  return <ClusterLanding />;
}
```

`web/packages/shell/src/shell/Shell.tsx`:

```tsx
import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useMemo } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { ThemeProvider } from "../theme/ThemeProvider";
import { defaultThemes } from "../theme/themes";
import { type CorePages, defaultBrand, ShellExtensionContext, type ShellExtension } from "./extension";
import { Header } from "./Header";
import { RootRedirect } from "./RootRedirect";

// region Styles
const Root = styled.div<{ stylesOverride?: SerializedStyles }>`
  min-height: 100%;
  ${(p) => p.stylesOverride}
`;

// region Component
interface ShellProps {
  extension?: ShellExtension;
  pages: CorePages;
  stylesOverride?: SerializedStyles;
}

/** The application shell. Upstream and downstream apps render this with their own extension. */
export function Shell({ extension = {}, pages, stylesOverride }: ShellProps) {
  const ext = useMemo<Required<ShellExtension>>(() => ({
    brand: extension.brand ?? defaultBrand,
    themes: extension.themes ?? defaultThemes,
    defaultThemeId: extension.defaultThemeId ?? (extension.themes ?? defaultThemes)[0]?.id ?? "",
    routes: extension.routes ?? [],
    navEntries: extension.navEntries ?? [],
    findingRenderers: extension.findingRenderers ?? {},
  }), [extension]);
  const { ClusterLanding, InstallPage, FindingsPage, ResourcePage } = pages;
  return (
    <ShellExtensionContext.Provider value={ext}>
      <ThemeProvider themes={ext.themes} defaultThemeId={ext.defaultThemeId}>
        <BrowserRouter>
          <Root data-testid="shell-root" stylesOverride={stylesOverride}>
            <Header />
            <main>
              <Routes>
                <Route path="/" element={<RootRedirect ClusterLanding={ClusterLanding} />} />
                <Route path="/i/:ns/:name" element={<InstallPage />} />
                <Route path="/i/:ns/:name/findings" element={<FindingsPage />} />
                <Route path="/i/:ns/:name/r/:group/:kind/:rns/:rname" element={<ResourcePage />} />
                {ext.routes.map((r) => (
                  <Route key={r.path} path={r.scope === "install" ? `/i/:ns/:name/${r.path}` : `/${r.path}`} element={r.element} />
                ))}
              </Routes>
            </main>
          </Root>
        </BrowserRouter>
      </ThemeProvider>
    </ShellExtensionContext.Provider>
  );
}
```

The version chip uses `headerText` on `accent`. Add that pair to the `it.each` list in `themes.test.ts` (`["header text on accent chip", c.headerText, c.accent]`) so its contrast is tested.

Export from `index.ts`:

```ts
export type { Brand, CorePages, FindingRenderer, NavEntry, RouteContribution, ShellExtension } from "./shell/extension";
export { useInstallId, useShellExtension } from "./shell/extension";
export { Shell } from "./shell/Shell";
export { Tooltip } from "./shell/Tooltip";
export { PageState, Shimmer } from "./states/PageState";
```

- [ ] **Step 6: Wire the upstream app**

`web/app/src/brand.tsx`:

```tsx
import type { Brand } from "@kgateway-ui/shell";
import logoUrl from "./assets/kgateway-icon.svg";

/** The kgateway project identity: CNCF artwork logo, used to identify the project this UI is for. */
export const kgatewayBrand: Brand = {
  productName: "kgateway",
  logo: <img src={logoUrl} alt="" width={24} height={24} />,
};
```

`web/app/src/main.tsx`:

```tsx
import { defaultPages, Shell } from "@kgateway-ui/shell";
import { createRoot } from "react-dom/client";
import { kgatewayBrand } from "./brand";

createRoot(document.getElementById("root") as HTMLElement).render(
  <Shell extension={{ brand: kgatewayBrand }} pages={defaultPages} />,
);
```

`defaultPages` is exported in Task 17. Until then, `npm run build` fails on that import, and that is expected. Verify this task with the tests only.

- [ ] **Step 7: Run the tests and confirm they pass**

Run: `npm test && npm run lint`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add web && git commit -m "feat(web): shell with extension registry, routing, header and switchers"
```

---

### Task 16: Install page (layout A)

**Files:**
- Create: `web/packages/shell/src/pages/install/InstallPage.tsx`, `HealthStrip.tsx`, `FindingRow.tsx`, `TopFindings.tsx`, `CheckedList.tsx`, `CountTiles.tsx`, `GatewayTable.tsx`, all in `web/packages/shell/src/pages/install/`
- Create: `web/packages/shell/src/test/fixtures.ts`
- Test: `web/packages/shell/src/pages/install/InstallPage.test.tsx`

**Interfaces:**
- Consumes: `useOverview`, `useHealth`, `useMeta`, `useInstallId`, `isStale`, `isLinkable`, `HealthBadge`, `HealthIcon`, `ResourceName`, `Tooltip`, `PageState`, `Shimmer`, `useShellExtension().findingRenderers`.
- Produces:
  - `<InstallPage />`;
  - `<FindingRow finding installId stylesOverride? />` (used again by Task 17);
  - `resourcePath(installId: string, r: Ref): string` (exported from `FindingRow.tsx`: `/i/{id}/r/{group|-}/{kind}/{ns|-}/{name}`);
  - `<HealthStrip health stale now />` and `<GatewayTable rows installId stale />` (page-internal; `stale` and `now` come from `InstallPage`, which reads `useNow()`).

Behaviour (design §2, §5.3, §6.4–6.5; D-011):

| Data | Rendering |
|---|---|
| `health.state` = healthy | A slim strip: ✓ "All healthy", "checked 3s ago". The right column lists "What was checked" from `checks`. |
| error / warning / unknown | Strip in that state's colours: "{n} errors, {m} warnings" (unknown: "No known problems — {failing required check names} could not be checked"; a failing check with `pending: true` contributes its own text instead, e.g. "No known problems — 2 resources are being processed by the controller"; round 2, finding 2). Below it, the top 5 findings by severity, then "Show all ({total})" linking to `/i/{id}/findings`. |
| health or overview `collectedAt` older than 2 × `POLL_MS` | The strip **and every gateway row** turn unknown; the strip reads "Data is stale — last checked {relative time}" (finding 3). |
| finding | A row with a severity badge, `ResourceName` (or the install when there is no resource), the message, and the detail on a second muted line. The resource is a link to its YAML view only when `/api/v1/meta` lists its kind (finding 11); a copy button copies `namespace/name`. A registered finding renderer (by code prefix) adds content below. |
| restricted finding (`restricted: true`, D-013) | Severity badge and `ResourceName` as usual, the text "Restricted: ask the owning team" instead of message and detail, and no link. |
| counts | One tile per count, in served order (the GatewayClass tile first; finding 12). A tile whose `access.ok` is false shows "No access" and the error in a tooltip. Tiles share one height and stretch to fill the row. A scope tile shows "{watched} / {total} namespaces", with the selectors in a tooltip; scope `unknown` shows a warning state. |
| gateways | A text filter (`aria-label="Filter gateways"`) above a TanStack table with columns Status, Gateway, Class, Listeners, Routes, Addresses. The filter matches name, namespace and class and lives in the query string with the sort (`?q=edge&sort=routes.desc`; finding 12). Rows link to the Gateway's YAML view in Phase 1. |
| first load, or an install switch | Shimmer placeholders with the final layout's dimensions (nothing shifts when data arrives). The page renders data only when `overview.header.id` and `health.install` both equal the route's install, so install A's data never appears under install B's URL (finding 4). |

- [ ] **Step 1: Write the fixtures and the failing tests**

`web/packages/shell/src/test/fixtures.ts`:

```ts
import type { HealthReport, MetaResponse, OverviewResponse } from "../api/types";

export const now = new Date().toISOString();

/** Kinds the resource endpoint serves in these fixtures. */
export const meta: MetaResponse = { version: "dev", synced: true, kinds: [
  { group: "gateway.networking.k8s.io", kind: "GatewayClass", category: "gatewayclass", displayName: "GatewayClasses" },
  { group: "gateway.networking.k8s.io", kind: "Gateway", category: "gateway", displayName: "Gateways" },
  { group: "gateway.networking.k8s.io", kind: "HTTPRoute", category: "route", displayName: "HTTPRoutes" },
  { group: "apps", kind: "Deployment", category: "workload", displayName: "Deployments" },
] };

export const overview: OverviewResponse = {
  header: { id: "kgw/kgateway", namespace: "kgw", name: "kgateway", version: "2.4.7", edition: "kgateway",
    controllerNames: ["kgateway.dev/kgateway"], controller: { ready: 2, desired: 2 },
    scope: { mode: "selectors", selectors: [{ matchLabels: { team: "a" } }], watched: 12, total: 40 } },
  counts: [
    { group: "gateway.networking.k8s.io", kind: "GatewayClass", category: "gatewayclass", displayName: "GatewayClasses", count: 1, access: { ok: true } },
    { group: "gateway.networking.k8s.io", kind: "Gateway", category: "gateway", displayName: "Gateways", count: 3, access: { ok: true } },
    { group: "gateway.networking.k8s.io", kind: "HTTPRoute", category: "route", displayName: "HTTPRoutes", count: 142, access: { ok: true } },
    { group: "gateway.kgateway.dev", kind: "TrafficPolicy", category: "policy", displayName: "TrafficPolicies", count: 0, access: { ok: false, error: "forbidden" } },
  ],
  gateways: [
    { ref: { group: "gateway.networking.k8s.io", kind: "Gateway", namespace: "prod", name: "edge" }, class: "kgateway", state: "error", listeners: 2, attachedRoutes: 98, addresses: ["34.12.8.1"] },
    { ref: { group: "gateway.networking.k8s.io", kind: "Gateway", namespace: "prod", name: "internal" }, class: "kgateway", state: "healthy", listeners: 1, attachedRoutes: 40, addresses: ["10.0.4.7"] },
  ],
  collectedAt: now,
};

const f = (code: string, severity: "error" | "warning" | "info", ns: string, name: string, message: string) => ({
  code, slug: code, severity, install: "kgw/kgateway", message,
  resource: { group: "gateway.networking.k8s.io", kind: "HTTPRoute", namespace: ns, name },
});

export const unhealthy: HealthReport = {
  install: "kgw/kgateway",
  state: "error",
  findings: [
    { ...f("KGWUI-001", "error", "prod", "edge", "Configuration rejected by Envoy"), resource: { group: "gateway.networking.k8s.io", kind: "Gateway", namespace: "prod", name: "edge" } },
    ...Array.from({ length: 6 }, (_, i) => f("KGWUI-002", "warning", "shop", `route-${i}`, "Backend not found")),
  ],
  checks: [{ name: "xDS rejects", ok: true, required: true, collectedAt: now }],
  counts: { error: 1, warning: 6 },
  collectedAt: now,
};

export const healthy: HealthReport = {
  install: "kgw/kgateway", state: "healthy", findings: [], counts: {}, collectedAt: now,
  checks: [
    { name: "resource status", ok: true, required: true, collectedAt: now },
    { name: "xDS rejects", ok: true, required: true, collectedAt: now },
  ],
};

export const unknown: HealthReport = {
  install: "kgw/kgateway", state: "unknown", counts: { info: 1 }, collectedAt: now,
  findings: [{ code: "KGWUI-008", slug: "signal-unavailable", severity: "info", install: "kgw/kgateway", message: "Could not check xDS rejects", detail: "connection refused" }],
  checks: [{ name: "xDS rejects", ok: false, required: true, collectedAt: now, error: "connection refused" }],
};

/** Resources inside the grace period: unknown, "being processed" (round 2, finding 2). */
export const processing: HealthReport = {
  install: "kgw/kgateway", state: "unknown", counts: { info: 2 }, collectedAt: now,
  findings: ["new-a", "new-b"].map((name) => ({ code: "KGWUI-003", slug: "route-unattached", severity: "info" as const, install: "kgw/kgateway",
    message: "Being processed by the controller", resource: { group: "gateway.networking.k8s.io", kind: "HTTPRoute", namespace: "shop", name } })),
  checks: [{ name: "resource status", ok: false, required: true, pending: true, collectedAt: now, error: "2 resources are being processed by the controller" }],
};

/** A finding on a resource the caller may not view (D-013). */
export const restricted: HealthReport = {
  ...unhealthy,
  findings: [{ code: "KGWUI-002", slug: "condition-false", severity: "error", install: "kgw/kgateway", message: "", restricted: true,
    resource: { group: "gateway.networking.k8s.io", kind: "HTTPRoute", namespace: "payments", name: "checkout" } }],
  counts: { error: 1 },
};
```

`web/packages/shell/src/pages/install/InstallPage.test.tsx`:

```tsx
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes, useNavigate } from "react-router-dom";
import { POLL_MS } from "../../api/hooks";
import { healthy, meta, overview, processing, restricted, unhealthy, unknown } from "../../test/fixtures";
import { renderWithTheme, stubApi } from "../../test/render";
import { InstallPage } from "./InstallPage";

function mount(health: unknown, path = "/i/kgw/kgateway", ov: unknown = overview) {
  stubApi({ "/api/v1/installs/kgw/kgateway/overview": ov, "/api/v1/installs/kgw/kgateway/health": health, "/api/v1/meta": meta });
  return renderWithTheme(<Routes><Route path="/i/:ns/:name" element={<InstallPage />} /></Routes>, { path });
}

function GoTo({ to }: { to: string }) {
  const navigate = useNavigate();
  return <button type="button" onClick={() => navigate(to)}>go</button>;
}

it("unhealthy: strip with counts, top 5 findings, show all", async () => {
  mount(unhealthy);
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("1 error, 6 warnings");
  expect(screen.getAllByTestId("finding-row")).toHaveLength(5);
  expect(screen.getByRole("link", { name: "Show all (7)" })).toHaveAttribute("href", "/i/kgw/kgateway/findings");
  expect(screen.getAllByTestId("finding-row")[0]).toHaveTextContent("Configuration rejected by Envoy");
});

it("healthy: slim strip and what was checked", async () => {
  mount(healthy);
  expect(await screen.findByTestId("health-strip")).toHaveTextContent("All healthy");
  const checked = screen.getByRole("list", { name: "What was checked" });
  expect(within(checked).getByText("xDS rejects")).toBeInTheDocument();
});

it("unknown: never green, names the missing signal", async () => {
  mount(unknown);
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("xDS rejects could not be checked");
  expect(within(strip).getByTestId("health-icon-unknown")).toBeInTheDocument();
});

// Round 3, finding 4: the strip and the gateway rows age into stale without new data.
it("a healthy page turns stale as it ages while identical responses keep arriving", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  const fresh = new Date().toISOString();
  mount({ ...healthy, collectedAt: fresh }, "/i/kgw/kgateway", { ...overview, collectedAt: fresh });
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("All healthy");
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2 * POLL_MS + 2000);
  });
  expect(strip).toHaveTextContent(/Data is stale/);
  const rows = within(screen.getByRole("table", { name: "Gateways" })).getAllByRole("row").slice(1);
  for (const r of rows) expect(r).toHaveTextContent("Unknown");
  vi.useRealTimers();
});

it("being processed: unknown, never green, and says what is happening", async () => {
  mount(processing);
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("No known problems — 2 resources are being processed by the controller");
  expect(within(strip).getByTestId("health-icon-unknown")).toBeInTheDocument();
  expect(strip).not.toHaveTextContent("could not be checked");
});

it("stale data turns the strip and every gateway row unknown", async () => {
  mount({ ...healthy, collectedAt: new Date(Date.now() - 60_000).toISOString() });
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent(/Data is stale/);
  expect(within(strip).getByTestId("health-icon-unknown")).toBeInTheDocument();
  const rows = within(screen.getByRole("table", { name: "Gateways" })).getAllByRole("row").slice(1);
  for (const r of rows) {
    expect(r).toHaveTextContent("Unknown");
    expect(within(r).queryByTestId("health-icon-healthy")).toBeNull();
    expect(within(r).queryByTestId("health-icon-error")).toBeNull();
  }
});

it("count tiles, GatewayClass tile, no-access tile and scope tile", async () => {
  mount(healthy);
  expect(await screen.findByText("142")).toBeInTheDocument();
  expect(screen.getByText("GatewayClasses")).toBeInTheDocument();
  expect(screen.getByText("No access")).toBeInTheDocument();
  expect(screen.getByText("12 / 40")).toBeInTheDocument();
});

it("gateway filter matches name, namespace and class and lives in the URL", async () => {
  mount(healthy, "/i/kgw/kgateway?q=internal");
  const table = await screen.findByRole("table", { name: "Gateways" });
  expect(within(table).getAllByRole("row")).toHaveLength(2); // header + internal
  const filter = screen.getByRole("textbox", { name: "Filter gateways" });
  expect(filter).toHaveValue("internal");
  await userEvent.clear(filter);
  await userEvent.type(filter, "prod");
  expect(within(table).getAllByRole("row")).toHaveLength(3); // namespace matches both
  await userEvent.clear(filter);
  await userEvent.type(filter, "nomatch");
  expect(within(table).getAllByRole("row")).toHaveLength(1);
});

it("links only finding resources whose kind /api/v1/meta lists", async () => {
  stubApi({ "/api/v1/installs/kgw/kgateway/overview": overview, "/api/v1/installs/kgw/kgateway/health": unhealthy,
    "/api/v1/meta": { ...meta, kinds: meta.kinds.filter((k) => k.kind !== "HTTPRoute") } });
  renderWithTheme(<Routes><Route path="/i/:ns/:name" element={<InstallPage />} /></Routes>, { path: "/i/kgw/kgateway" });
  const rows = await screen.findAllByTestId("finding-row");
  await waitFor(() => expect(within(rows[0] as HTMLElement).getByRole("link")).toHaveAttribute("href", "/i/kgw/kgateway/r/gateway.networking.k8s.io/Gateway/prod/edge"));
  expect(within(rows[1] as HTMLElement).queryByRole("link")).toBeNull(); // HTTPRoute is not listed
});

it("a restricted finding shows its resource and severity, a plain note and no link", async () => {
  mount(restricted);
  const row = await screen.findByTestId("finding-row");
  expect(row).toHaveTextContent("payments/checkout");
  expect(row).toHaveTextContent("Error");
  expect(row).toHaveTextContent("Restricted: ask the owning team");
  expect(within(row).queryByRole("link")).toBeNull();
});

it("switching installs never shows the previous install's data", async () => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.startsWith("/api/v1/installs/b/two/")) return new Promise<Response>(() => {}); // install B never answers
    const body = url.endsWith("/overview") ? overview : url.endsWith("/health") ? unhealthy : meta;
    return new Response(JSON.stringify(body), { status: 200 });
  }));
  renderWithTheme(<><Routes><Route path="/i/:ns/:name" element={<InstallPage />} /></Routes><GoTo to="/i/b/two" /></>, { path: "/i/kgw/kgateway" });
  expect(await screen.findByTestId("health-strip")).toHaveTextContent("1 error");
  await userEvent.click(screen.getByRole("button", { name: "go" }));
  await waitFor(() => expect(screen.queryByTestId("health-strip")).toBeNull());
  expect(screen.queryByRole("table", { name: "Gateways" })).toBeNull();
  expect(screen.queryAllByTestId("finding-row")).toHaveLength(0);
});

it("never renders a response that belongs to another install", async () => {
  mount(healthy, "/i/kgw/kgateway", { ...overview, header: { ...overview.header, id: "other/install" } });
  await waitFor(() => expect(screen.getByRole("status", { busy: true })).toBeInTheDocument());
  expect(screen.queryByTestId("health-strip")).toBeNull();
});

it("gateway table lists gateways with status text", async () => {
  mount(healthy);
  const table = await screen.findByRole("table", { name: "Gateways" });
  const rows = within(table).getAllByRole("row");
  expect(rows).toHaveLength(3); // header + 2
  expect(rows[1]).toHaveTextContent("Error");
  expect(rows[1]).toHaveTextContent("edge");
});

it("API error shows a designed error state", async () => {
  stubApi({ "/api/v1/installs/kgw/kgateway/overview": [404, { error: "install kgw/kgateway not found" }],
    "/api/v1/installs/kgw/kgateway/health": [404, { error: "install kgw/kgateway not found" }] });
  renderWithTheme(<Routes><Route path="/i/:ns/:name" element={<InstallPage />} /></Routes>, { path: "/i/kgw/kgateway" });
  await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("install kgw/kgateway not found"));
});
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `npm test`
Expected: FAIL

- [ ] **Step 3: Implement the components**

`FindingRow.tsx`:

```tsx
import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Copy } from "lucide-react";
import { Link } from "react-router-dom";
import { isLinkable, useMeta } from "../../api/hooks";
import type { Finding, Ref } from "../../api/types";
import { HealthBadge } from "../../health/HealthBadge";
import { severityToState } from "../../health/state";
import { ResourceName } from "../../resource/ResourceName";
import { useShellExtension } from "../../shell/extension";
import { Tooltip } from "../../shell/Tooltip";

// region Styles
const Row = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: grid; grid-template-columns: 96px minmax(180px, 280px) 1fr auto; align-items: start; gap: ${(p) => p.theme.space(3)};
  padding: ${(p) => p.theme.space(2.5)} ${(p) => p.theme.space(3)};
  border-bottom: 1px solid ${(p) => p.theme.color.border};
  &:last-of-type { border-bottom: 0; }
  a { color: inherit; text-decoration: none; }
  a:hover [data-part="name"] { text-decoration: underline; }
  .msg { color: ${(p) => p.theme.color.text}; }
  .detail { color: ${(p) => p.theme.color.textMuted}; font-size: ${(p) => p.theme.font.size.sm}; margin-top: 2px; }
  @media (max-width: 720px) { grid-template-columns: 1fr; }
  ${(p) => p.stylesOverride}
`;
const IconButton = styled.button`
  width: 28px; height: 28px; display: inline-flex; align-items: center; justify-content: center; cursor: pointer;
  background: transparent; color: ${(p) => p.theme.color.textMuted}; border: 1px solid transparent; border-radius: ${(p) => p.theme.radius.sm};
  &:hover { border-color: ${(p) => p.theme.color.border}; color: ${(p) => p.theme.color.text}; }
  &:active { background: ${(p) => p.theme.color.surfaceRaised}; }
`;

// region Helpers
/** Path of the read-only YAML view of r. */
export function resourcePath(installId: string, r: Ref): string {
  return `/i/${installId}/r/${r.group || "-"}/${r.kind}/${r.namespace || "-"}/${r.name}`;
}

// region Component
interface FindingRowProps {
  finding: Finding;
  installId: string;
  stylesOverride?: SerializedStyles;
}

/** One finding: severity, resource, message, detail. The resource links to its YAML view when the backend serves its kind. */
export function FindingRow({ finding: f, installId, stylesOverride }: FindingRowProps) {
  const { findingRenderers } = useShellExtension();
  const { data: meta } = useMeta();
  const extra = Object.entries(findingRenderers).find(([prefix]) => f.code.startsWith(prefix))?.[1];
  const name = f.resource ? <ResourceName namespace={f.resource.namespace} name={f.resource.name} /> : <span>{f.install}</span>;
  // A restricted finding (D-013) never links: its resource would 404 for this caller.
  const linked = !!f.resource && !f.restricted && isLinkable(meta, f.resource);
  return (
    <Row data-testid="finding-row" stylesOverride={stylesOverride}>
      <HealthBadge state={severityToState(f.severity)} label={{ error: "Error", warning: "Warning", info: "Info" }[f.severity]} />
      <div>
        {linked && f.resource ? <Link to={resourcePath(installId, f.resource)}>{name}</Link> : name}
        {f.resource ? <div className="detail">{f.resource.kind}</div> : null}
      </div>
      <div>
        {f.restricted ? (
          <div className="detail">Restricted: ask the owning team</div>
        ) : (
          <>
            <div className="msg">{f.message}</div>
            {f.detail ? <div className="detail">{f.detail}</div> : null}
            {extra ? extra(f) : null}
          </>
        )}
      </div>
      {f.resource ? (
        <Tooltip label="Copy namespace/name">
          <IconButton type="button" aria-label="Copy resource name"
            onClick={() => navigator.clipboard?.writeText(f.resource?.namespace ? `${f.resource.namespace}/${f.resource.name}` : (f.resource?.name ?? ""))}>
            <Copy size={14} />
          </IconButton>
        </Tooltip>
      ) : <span />}
    </Row>
  );
}
```

`HealthStrip.tsx`:

```tsx
import styled from "@emotion/styled";
import type { HealthReport } from "../../api/types";
import { HealthIcon } from "../../health/HealthIcon";
import type { HealthState } from "../../theme/tokens";

// region Styles
const Strip = styled.div<{ state: HealthState }>`
  display: flex; align-items: center; gap: ${(p) => p.theme.space(3)}; padding: ${(p) => p.theme.space(2.5)} ${(p) => p.theme.space(3)};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.status[p.state].stripBg}; border: 1px solid ${(p) => p.theme.color.status[p.state].stripBorder};
  b { font-weight: ${(p) => p.theme.font.weight.semibold}; }
  .when { margin-left: auto; color: ${(p) => p.theme.color.textMuted}; font-size: ${(p) => p.theme.font.size.sm}; }
`;

// region Helpers
/** Relative time, e.g. "5s ago" (team rule: times are relative). */
export function ago(iso: string, now = Date.now()): string {
  const s = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.round(s / 60)}m ago`;
  return `${Math.round(s / 3600)}h ago`;
}

function plural(n: number, w: string) {
  return `${n} ${w}${n === 1 ? "" : "s"}`;
}

function headline(h: HealthReport): string {
  if (h.state === "healthy") return "All healthy";
  if (h.state === "unknown") {
    const failing = h.checks.filter((c) => c.required && !c.ok);
    const missing = failing.filter((c) => !c.pending).map((c) => c.name);
    // A pending check was collected; its error says how many resources are being processed.
    const parts = failing.filter((c) => c.pending).map((c) => c.error ?? `${c.name} is being processed`);
    if (missing.length) parts.unshift(`${missing.join(", ")} could not be checked`);
    return `No known problems — ${parts.join("; ")}`;
  }
  const parts = [];
  if (h.counts.error) parts.push(plural(h.counts.error, "error"));
  if (h.counts.warning) parts.push(plural(h.counts.warning, "warning"));
  return parts.join(", ");
}

// region Component
/** Full-width health strip; stale data is shown as unknown (D-006). InstallPage decides staleness for the whole page. */
export function HealthStrip({ health, stale, now }: { health: HealthReport; stale: boolean; now: number }) {
  const state: HealthState = stale ? "unknown" : health.state;
  return (
    <Strip state={state} data-testid="health-strip" role="status">
      <HealthIcon state={state} size={18} />
      <b>{stale ? `Data is stale — last checked ${ago(health.collectedAt, now)}` : headline(health)}</b>
      {!stale ? <span className="when">checked {ago(health.collectedAt, now)}</span> : null}
    </Strip>
  );
}
```

`TopFindings.tsx`:

```tsx
import styled from "@emotion/styled";
import { Link } from "react-router-dom";
import type { Finding } from "../../api/types";
import { FindingRow } from "./FindingRow";

// region Styles
const Box = styled.section`
  margin-top: ${(p) => p.theme.space(2)}; background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border}; border-radius: ${(p) => p.theme.radius.md};
  .more { display: block; padding: ${(p) => p.theme.space(2.5)} ${(p) => p.theme.space(3)}; color: ${(p) => p.theme.color.accentText}; text-decoration: none; }
`;

// region Component
/** The top 5 findings (already sorted by severity by the backend) and a link to all. */
export function TopFindings({ findings, installId }: { findings: Finding[]; installId: string }) {
  if (findings.length === 0) return null;
  return (
    <Box aria-label="Findings">
      {findings.slice(0, 5).map((f, i) => (
        <FindingRow key={`${f.code}-${f.resource?.namespace}-${f.resource?.name}-${i}`} finding={f} installId={installId} />
      ))}
      <Link className="more" to={`/i/${installId}/findings`}>Show all ({findings.length})</Link>
    </Box>
  );
}
```

`CheckedList.tsx`:

```tsx
import styled from "@emotion/styled";
import type { CheckResult } from "../../api/types";
import { HealthIcon } from "../../health/HealthIcon";

// region Styles
const List = styled.ul`
  list-style: none; margin: 0; padding: ${(p) => p.theme.space(3)}; background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border}; border-radius: ${(p) => p.theme.radius.md};
  li { display: flex; gap: 8px; align-items: center; padding: 4px 0; }
`;

// region Component
/** Proves that green means "checked and fine", not "did not look" (design §5.3). */
export function CheckedList({ checks }: { checks: CheckResult[] }) {
  return (
    <List aria-label="What was checked">
      {checks.map((c) => (
        <li key={c.name}>
          <HealthIcon state={c.ok ? "healthy" : "unknown"} />
          {c.name}
        </li>
      ))}
    </List>
  );
}
```

`CountTiles.tsx`:

```tsx
import styled from "@emotion/styled";
import type { OverviewResponse } from "../../api/types";
import { HealthIcon } from "../../health/HealthIcon";
import { Tooltip } from "../../shell/Tooltip";

// region Styles
const Grid = styled.div`
  display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: ${(p) => p.theme.space(2)};
  margin: ${(p) => p.theme.space(4)} 0;
`;
const Tile = styled.div`
  height: 100%; padding: ${(p) => p.theme.space(3)}; background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border}; border-radius: ${(p) => p.theme.radius.md};
  .n { display: block; font-size: ${(p) => p.theme.font.size.xl}; font-weight: ${(p) => p.theme.font.weight.semibold}; letter-spacing: -0.02em; }
  .l { color: ${(p) => p.theme.color.textMuted}; font-size: ${(p) => p.theme.font.size.sm}; }
  .warn { display: inline-flex; gap: 6px; align-items: center; color: ${(p) => p.theme.color.status.unknown.fg}; font-weight: ${(p) => p.theme.font.weight.semibold}; }
`;

// region Component
/** Count tiles per kind plus the scope tile. */
export function CountTiles({ overview }: { overview: OverviewResponse }) {
  const sc = overview.header.scope;
  const scopeLabel = sc.mode === "all" ? "All namespaces" : sc.mode === "unknown" ? `Scope unknown: ${sc.error ?? ""}` : JSON.stringify(sc.selectors);
  return (
    <Grid>
      {overview.counts.map((c) => (
        <Tile key={`${c.group}/${c.kind}`}>
          {c.access.ok ? (
            <span className="n">{c.count}</span>
          ) : (
            <Tooltip label={c.access.error ?? "not readable"}>
              <span className="warn"><HealthIcon state="unknown" />No access</span>
            </Tooltip>
          )}
          <span className="l">{c.displayName}</span>
        </Tile>
      ))}
      <Tile>
        <Tooltip label={scopeLabel}>
          <span className="n">{sc.mode === "unknown" ? <HealthIcon state="warning" size={20} /> : `${sc.watched} / ${sc.total}`}</span>
        </Tooltip>
        <span className="l">Namespaces watched</span>
      </Tile>
    </Grid>
  );
}
```

`GatewayTable.tsx`:

```tsx
import styled from "@emotion/styled";
import { createColumnHelper, flexRender, getCoreRowModel, getSortedRowModel, type SortingState, useReactTable } from "@tanstack/react-table";
import { useMemo } from "react";
import { Link, useSearchParams } from "react-router-dom";
import type { GatewayRow } from "../../api/types";
import { HealthBadge } from "../../health/HealthBadge";
import { ResourceName } from "../../resource/ResourceName";
import { resourcePath } from "./FindingRow";

// region Styles
const Filter = styled.input`
  height: 32px; min-width: 260px; margin-bottom: ${(p) => p.theme.space(2)}; padding: 0 10px; font: inherit;
  color: ${(p) => p.theme.color.text}; background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border}; border-radius: ${(p) => p.theme.radius.sm};
  &:hover { border-color: ${(p) => p.theme.color.textMuted}; }
  &:active { border-color: ${(p) => p.theme.color.text}; }
`;
const Table = styled.table`
  width: 100%; border-collapse: collapse; background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border}; border-radius: ${(p) => p.theme.radius.md};
  th { text-align: left; font-size: ${(p) => p.theme.font.size.xs}; letter-spacing: .06em; text-transform: uppercase; font-weight: ${(p) => p.theme.font.weight.semibold};
       color: ${(p) => p.theme.color.textMuted}; padding: 8px 12px; border-bottom: 1px solid ${(p) => p.theme.color.border}; }
  th button { all: unset; cursor: pointer; }
  th button:hover { color: ${(p) => p.theme.color.text}; }
  td { padding: 8px 12px; border-bottom: 1px solid ${(p) => p.theme.color.border}; }
  tr:last-of-type td { border-bottom: 0; }
  a { color: inherit; text-decoration: none; }
  a:hover [data-part="name"] { text-decoration: underline; }
`;

// region Helpers
const col = createColumnHelper<GatewayRow>();
const rank = { error: 0, unknown: 1, warning: 2, healthy: 3 } as const;

/** Rows whose name, namespace or class contains q (case-insensitive). */
function filterRows(rows: GatewayRow[], q: string): GatewayRow[] {
  const needle = q.trim().toLowerCase();
  if (!needle) return rows;
  return rows.filter((r) => [r.ref.name, r.ref.namespace ?? "", r.class].some((v) => v.toLowerCase().includes(needle)));
}

// Stale data renders every row as unknown: an old green badge is not evidence (finding 3).
function columns(installId: string, stale: boolean) {
  return [
    col.accessor("state", { header: "Status", cell: (c) => <HealthBadge state={stale ? "unknown" : c.getValue()} />, sortingFn: (a, b) => rank[a.original.state] - rank[b.original.state] }),
    col.accessor((r) => `${r.ref.namespace}/${r.ref.name}`, { id: "gateway", header: "Gateway",
      cell: (c) => <Link to={resourcePath(installId, c.row.original.ref)}><ResourceName namespace={c.row.original.ref.namespace} name={c.row.original.ref.name} /></Link> }),
    col.accessor("class", { header: "Class" }),
    col.accessor("listeners", { header: "Listeners" }),
    col.accessor("attachedRoutes", { id: "routes", header: "Routes" }),
    col.accessor((r) => (r.addresses ?? []).join(", "), { id: "addresses", header: "Addresses", enableSorting: false }),
  ];
}

// region Component
/** Gateways of the install; the text filter and the sort are kept in the URL (?q=edge&sort=routes.desc). */
export function GatewayTable({ rows, installId, stale }: { rows: GatewayRow[]; installId: string; stale: boolean }) {
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const visible = useMemo(() => filterRows(rows, q), [rows, q]);
  const [id, dir] = (params.get("sort") ?? "state.asc").split(".");
  const sorting: SortingState = [{ id: id ?? "state", desc: dir === "desc" }];
  const table = useReactTable({
    data: visible, columns: columns(installId, stale), state: { sorting },
    onSortingChange: (u) => {
      const next = typeof u === "function" ? u(sorting) : u;
      const s = next[0];
      setParams((p) => { if (s) p.set("sort", `${s.id}.${s.desc ? "desc" : "asc"}`); return p; }, { replace: true });
    },
    getCoreRowModel: getCoreRowModel(), getSortedRowModel: getSortedRowModel(),
  });
  return (
    <>
      <Filter aria-label="Filter gateways" autoComplete="off" placeholder="Filter by name, namespace or class" value={q}
        onChange={(e) => setParams((p) => { if (e.target.value) p.set("q", e.target.value); else p.delete("q"); return p; }, { replace: true })} />
      <Table aria-label="Gateways">
        <thead>
          {table.getHeaderGroups().map((hg) => (
            <tr key={hg.id}>
              {hg.headers.map((h) => (
                <th key={h.id} aria-sort={h.column.getIsSorted() === "asc" ? "ascending" : h.column.getIsSorted() === "desc" ? "descending" : "none"}>
                  {h.column.getCanSort() ? (
                    <button type="button" onClick={h.column.getToggleSortingHandler()}>{flexRender(h.column.columnDef.header, h.getContext())}</button>
                  ) : flexRender(h.column.columnDef.header, h.getContext())}
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody>
          {table.getRowModel().rows.map((r) => (
            <tr key={r.id}>{r.getVisibleCells().map((c) => <td key={c.id}>{flexRender(c.column.columnDef.cell, c.getContext())}</td>)}</tr>
          ))}
        </tbody>
      </Table>
    </>
  );
}
```

`InstallPage.tsx`:

```tsx
import styled from "@emotion/styled";
import { useNow } from "../../api/clock";
import { isStale, useHealth, useOverview } from "../../api/hooks";
import { useInstallId } from "../../shell/extension";
import { PageState, Shimmer } from "../../states/PageState";
import { CheckedList } from "./CheckedList";
import { CountTiles } from "./CountTiles";
import { GatewayTable } from "./GatewayTable";
import { HealthStrip } from "./HealthStrip";
import { TopFindings } from "./TopFindings";

// region Styles
const Page = styled.div`
  max-width: 1400px; margin: 0 auto; padding: ${(p) => p.theme.space(4)};
  h2 { font-size: ${(p) => p.theme.font.size.md}; font-weight: ${(p) => p.theme.font.weight.semibold}; margin: ${(p) => p.theme.space(5)} 0 ${(p) => p.theme.space(2)}; }
`;
const Split = styled.div`
  display: grid; grid-template-columns: 1fr 280px; gap: ${(p) => p.theme.space(4)}; align-items: start;
  @media (max-width: 900px) { grid-template-columns: 1fr; }
`;

// region Component
/** The install page (layout A, D-011): health strip, top findings, tiles, gateways. */
export function InstallPage() {
  const id = useInstallId();
  const ov = useOverview(id);
  const h = useHealth(id);
  const now = useNow();
  const err = ov.error ?? h.error;
  if (err && err.status === 503) return <PageState kind="connecting" title="Connecting to cluster…" message={err.message} />;
  if (err && !(ov.data && h.data)) return <PageState kind="error" title="Cannot load this install" message={err.message} copyText={err.message} />;
  // Render only data that belongs to this route's install (finding 4).
  if (!ov.data || !h.data || ov.data.header.id !== id || h.data.install !== id) {
    return (
      <Page role="status" aria-busy="true" aria-label="Loading install">
        <Shimmer h={44} />
        <div style={{ height: 16 }} />
        <Shimmer h={84} />
        <div style={{ height: 16 }} />
        <Shimmer h={160} />
      </Page>
    );
  }
  // Stale health or overview turns the strip and the gateway rows unknown (finding 3).
  const stale = isStale(h.data.collectedAt, now) || isStale(ov.data.collectedAt, now);
  const healthy = h.data.state === "healthy";
  return (
    <Page>
      <HealthStrip health={h.data} stale={stale} now={now} />
      {healthy ? (
        <Split>
          <div>
            <CountTiles overview={ov.data} />
            <h2>Gateways</h2>
            <GatewayTable rows={ov.data.gateways} installId={id} stale={stale} />
          </div>
          <div>
            <h2>What was checked</h2>
            <CheckedList checks={h.data.checks} />
          </div>
        </Split>
      ) : (
        <>
          <TopFindings findings={h.data.findings} installId={id} />
          <CountTiles overview={ov.data} />
          <h2>Gateways</h2>
          <GatewayTable rows={ov.data.gateways} installId={id} stale={stale} />
        </>
      )}
    </Page>
  );
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `npm test && npm run lint`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web && git commit -m "feat(web): install page with health strip, findings, tiles and gateway table"
```

---

### Task 17: Cluster landing, findings page, YAML view, `defaultPages`

**Files:**
- Create: `web/packages/shell/src/pages/ClusterLanding.tsx`, `web/packages/shell/src/pages/FindingsPage.tsx`, `web/packages/shell/src/pages/ResourcePage.tsx`, `web/packages/shell/src/pages/defaultPages.ts`
- Modify: `web/packages/shell/src/index.ts`
- Test: `web/packages/shell/src/pages/pages.test.tsx`

**Interfaces:**
- Consumes: Tasks 14–16.
- Produces: `defaultPages: CorePages`.

Behaviour:
- **ClusterLanding:** one card per install with health badge, `ResourceName`, version chip, "{n} errors, {m} warnings · {watched} / {total} namespaces" and the counts. The whole card is a link to `/i/{id}`. Cards share one height. A summary whose `computedAt` is older than 2 × `POLL_MS` shows an unknown badge and "Data is stale" instead of its counts (round 2, finding 6).
- **FindingsPage:**
  - Every finding of the install, filterable with one-click chips. The severity chips are Error, Warning and Info; the kind chips are built from the kinds that appear in the findings.
  - Free-text filtering matches the message, code or resource name.
  - All filters live in the query string (`?sev=error,warning&kind=HTTPRoute&q=cart`).
  - The list is virtualised (`@tanstack/react-virtual`), so 5,000 findings scroll smoothly.
  - When filters match nothing, a centred empty state appears.
- **ResourcePage:** `kind`, `ResourceName`, a copy button for `namespace/name`, then YAML (`js-yaml` `dump`, `status` moved before `spec` so the health-relevant part comes first) in a mono `<pre>`. A 404 shows "Not in this install's scope"; the backend answers 404 alike for objects it may not serve and objects the caller may not view (D-013).
- All font sizes and weights come from `theme.font.size` and `theme.font.weight` (finding 17).

- [ ] **Step 1: Write the failing tests**

`web/packages/shell/src/pages/pages.test.tsx`:

```tsx
import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes } from "react-router-dom";
import { POLL_MS } from "../api/hooks";
import { overview, unhealthy } from "../test/fixtures";
import { renderWithTheme, stubApi } from "../test/render";
import { ClusterLanding } from "./ClusterLanding";
import { FindingsPage } from "./FindingsPage";
import { ResourcePage } from "./ResourcePage";

const inst = (id: string, state: string, counts = {}, computedAt = new Date().toISOString()) => ({ id, namespace: id.split("/")[0], name: id.split("/")[1],
  version: "2.4.7", edition: "kgateway", controllerNames: [], controller: { ready: 1, desired: 1 }, scope: { mode: "selectors", watched: 12, total: 40 },
  health: { state, counts }, computedAt });

it("cluster landing renders one card per install linking to it", async () => {
  stubApi({ "/api/v1/installs": [inst("a/one", "error", { error: 1, warning: 2 }), inst("b/two", "healthy")] });
  renderWithTheme(<ClusterLanding />);
  const card = await screen.findByRole("link", { name: /a\/one/ });
  expect(card).toHaveAttribute("href", "/i/a/one");
  expect(card).toHaveTextContent("1 error, 2 warnings");
  expect(screen.getByRole("link", { name: /b\/two/ })).toHaveTextContent("Healthy");
});

it("cluster landing never shows an old healthy summary as healthy", async () => {
  stubApi({ "/api/v1/installs": [inst("a/one", "healthy", {}, new Date(Date.now() - 60_000).toISOString())] });
  renderWithTheme(<ClusterLanding />);
  const card = await screen.findByRole("link", { name: /a\/one/ });
  expect(card).toHaveTextContent("Unknown");
  expect(card).toHaveTextContent("Data is stale");
  expect(card).not.toHaveTextContent("Healthy");
});

// Round 3, finding 4: identical successful polls do not re-render through SWR,
// so only the shared clock turns an ageing summary stale.
it("a fresh summary turns unknown as it ages while identical responses keep arriving", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  stubApi({ "/api/v1/installs": [inst("a/one", "healthy", {}, new Date().toISOString())] }); // the same body on every poll
  renderWithTheme(<ClusterLanding />);
  const card = await screen.findByRole("link", { name: /a\/one/ });
  expect(card).toHaveTextContent("Healthy");
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2 * POLL_MS + 2000);
  });
  expect(card).toHaveTextContent("Unknown");
  expect(card).toHaveTextContent("Data is stale");
  vi.useRealTimers();
});

function findings(path: string) {
  stubApi({ "/api/v1/installs/kgw/kgateway/health": unhealthy, "/api/v1/installs/kgw/kgateway/overview": overview });
  return renderWithTheme(<Routes><Route path="/i/:ns/:name/findings" element={<FindingsPage />} /></Routes>, { path });
}

it("findings page filters by severity chip and keeps it in the URL", async () => {
  findings("/i/kgw/kgateway/findings");
  await screen.findAllByTestId("finding-row");
  await userEvent.click(screen.getByRole("button", { name: "Error" }));
  await waitFor(() => expect(screen.getAllByTestId("finding-row")).toHaveLength(1));
  expect(screen.getByRole("button", { name: "Error" })).toHaveAttribute("aria-pressed", "true");
});

it("findings page applies filters from the URL and shows an empty state", async () => {
  findings("/i/kgw/kgateway/findings?q=nothing-matches-this");
  await waitFor(() => expect(screen.getByText("No findings match these filters")).toBeInTheDocument());
});

it("resource page shows YAML with status first and a copy button", async () => {
  stubApi({ "/api/v1/installs/kgw/kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/cart": {
    apiVersion: "gateway.networking.k8s.io/v1", kind: "HTTPRoute", metadata: { name: "cart", namespace: "shop" },
    spec: { parentRefs: [{ name: "edge" }] }, status: { parents: [] } } });
  renderWithTheme(<Routes><Route path="/i/:ns/:name/r/:group/:kind/:rns/:rname" element={<ResourcePage />} /></Routes>,
    { path: "/i/kgw/kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart" });
  const pre = await screen.findByTestId("yaml");
  expect(pre.textContent?.indexOf("status:")).toBeLessThan(pre.textContent?.indexOf("spec:") ?? 0);
  expect(screen.getByRole("button", { name: "Copy resource name" })).toBeInTheDocument();
});

it("resource page 404 says it is out of scope", async () => {
  stubApi({});
  renderWithTheme(<Routes><Route path="/i/:ns/:name/r/:group/:kind/:rns/:rname" element={<ResourcePage />} /></Routes>,
    { path: "/i/kgw/kgateway/r/gateway.networking.k8s.io/Gateway/infra/foreign" });
  await waitFor(() => expect(screen.getByText("Not in this install's scope")).toBeInTheDocument());
});
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `npm test`
Expected: FAIL

- [ ] **Step 3: Implement**

`ClusterLanding.tsx`:

```tsx
import styled from "@emotion/styled";
import { Link } from "react-router-dom";
import { useNow } from "../api/clock";
import { isStale, useInstalls } from "../api/hooks";
import { HealthBadge } from "../health/HealthBadge";
import { ResourceName } from "../resource/ResourceName";
import { PageState } from "../states/PageState";

// region Styles
const Grid = styled.div`
  max-width: 1400px; margin: 0 auto; padding: ${(p) => p.theme.space(4)};
  display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: ${(p) => p.theme.space(3)};
`;
const Card = styled(Link)`
  display: flex; flex-direction: column; gap: ${(p) => p.theme.space(2)}; height: 100%; padding: ${(p) => p.theme.space(4)};
  color: inherit; text-decoration: none; user-select: none; background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border}; border-radius: ${(p) => p.theme.radius.md};
  &:hover { background: ${(p) => p.theme.color.surfaceRaised}; }
  &:active { border-color: ${(p) => p.theme.color.textMuted}; }
  .top { display: flex; align-items: center; gap: 8px; justify-content: space-between; }
  .muted { color: ${(p) => p.theme.color.textMuted}; font-size: ${(p) => p.theme.font.size.sm}; }
`;

// region Helpers
function summary(c: { error?: number; warning?: number }): string {
  const p = [];
  if (c.error) p.push(`${c.error} error${c.error === 1 ? "" : "s"}`);
  if (c.warning) p.push(`${c.warning} warning${c.warning === 1 ? "" : "s"}`);
  return p.join(", ");
}

// region Component
/** One card per controller install (layout C); only shown with more than one install. */
export function ClusterLanding() {
  const { data, error } = useInstalls();
  const now = useNow();
  if (error) return <PageState kind="error" title="Cannot load installs" message={error.message} copyText={error.message} />;
  if (!data) return <PageState kind="loading" title="Loading…" />;
  return (
    <Grid>
      {data.map((i) => {
        // An old summary is not evidence: unknown, never its last state (round 2, finding 6).
        const stale = isStale(i.computedAt, now);
        return (
          <Card key={i.id} to={`/i/${i.id}`}>
            <div className="top">
              <ResourceName namespace={i.namespace} name={i.name} />
              <HealthBadge state={stale ? "unknown" : i.health.state} />
            </div>
            <span className="muted">
              v{i.version} · {stale ? "Data is stale" : summary(i.health.counts) || "No problems"} ·{" "}
              {i.scope.mode === "all" ? "all namespaces" : `${i.scope.watched} / ${i.scope.total} namespaces`}
            </span>
          </Card>
        );
      })}
    </Grid>
  );
}
```

`FindingsPage.tsx`:

```tsx
import styled from "@emotion/styled";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useMemo, useRef } from "react";
import { useSearchParams } from "react-router-dom";
import { useHealth } from "../api/hooks";
import type { Severity } from "../api/types";
import { useInstallId } from "../shell/extension";
import { PageState } from "../states/PageState";
import { FindingRow } from "./install/FindingRow";

// region Styles
const Page = styled.div` max-width: 1400px; margin: 0 auto; padding: ${(p) => p.theme.space(4)}; `;
const Bar = styled.div`
  display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin-bottom: ${(p) => p.theme.space(3)};
  input { height: 32px; min-width: 260px; padding: 0 10px; font: inherit; color: ${(p) => p.theme.color.text};
          background: ${(p) => p.theme.color.surface}; border: 1px solid ${(p) => p.theme.color.border}; border-radius: ${(p) => p.theme.radius.sm}; }
`;
const Chip = styled.button`
  height: 32px; padding: 0 12px; cursor: pointer; font: inherit; border-radius: 16px;
  color: ${(p) => p.theme.color.text}; background: ${(p) => p.theme.color.surface}; border: 1px solid ${(p) => p.theme.color.border};
  &:hover { border-color: ${(p) => p.theme.color.textMuted}; }
  &[aria-pressed="true"] { background: ${(p) => p.theme.color.surfaceRaised}; border-color: ${(p) => p.theme.color.textMuted}; font-weight: ${(p) => p.theme.font.weight.semibold}; }
`;
const Scroller = styled.div`
  height: calc(100vh - 200px); overflow: auto; background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border}; border-radius: ${(p) => p.theme.radius.md};
`;

// region Helpers
const SEVERITIES: { id: Severity; label: string }[] = [{ id: "error", label: "Error" }, { id: "warning", label: "Warning" }, { id: "info", label: "Info" }];

function toggle(list: string[], v: string) {
  return list.includes(v) ? list.filter((x) => x !== v) : [...list, v];
}

// region Component
/** All findings with URL-backed chip filters and a virtualised list. */
export function FindingsPage() {
  const id = useInstallId();
  const { data, error } = useHealth(id);
  const [params, setParams] = useSearchParams();
  const sev = params.get("sev")?.split(",").filter(Boolean) ?? [];
  const kinds = params.get("kind")?.split(",").filter(Boolean) ?? [];
  const q = params.get("q") ?? "";
  const set = (k: string, v: string) => setParams((p) => { if (v) p.set(k, v); else p.delete(k); return p; }, { replace: true });
  const all = data?.findings ?? [];
  const kindOptions = useMemo(() => [...new Set(all.map((f) => f.resource?.kind).filter(Boolean))] as string[], [all]);
  const rows = useMemo(() => all.filter((f) =>
    (sev.length === 0 || sev.includes(f.severity)) &&
    (kinds.length === 0 || (f.resource && kinds.includes(f.resource.kind))) &&
    (!q || `${f.code} ${f.message} ${f.resource?.namespace}/${f.resource?.name}`.toLowerCase().includes(q.toLowerCase()))), [all, sev, kinds, q]);
  const scroller = useRef<HTMLDivElement>(null);
  const v = useVirtualizer({ count: rows.length, getScrollElement: () => scroller.current, estimateSize: () => 64, overscan: 10, initialRect: { width: 1000, height: 800 } });
  if (error) return <PageState kind="error" title="Cannot load findings" message={error.message} copyText={error.message} />;
  if (!data) return <PageState kind="loading" title="Loading findings…" />;
  return (
    <Page>
      <Bar>
        {SEVERITIES.map((s) => (
          <Chip key={s.id} type="button" aria-pressed={sev.includes(s.id)} onClick={() => set("sev", toggle(sev, s.id).join(","))}>{s.label}</Chip>
        ))}
        {kindOptions.map((k) => (
          <Chip key={k} type="button" aria-pressed={kinds.includes(k)} onClick={() => set("kind", toggle(kinds, k).join(","))}>{k}</Chip>
        ))}
        <input aria-label="Filter findings" autoComplete="off" placeholder="Filter by name, message or code" value={q} onChange={(e) => set("q", e.target.value)} />
      </Bar>
      {rows.length === 0 ? (
        <PageState kind="empty" title="No findings match these filters" />
      ) : (
        <Scroller ref={scroller}>
          <div style={{ height: v.getTotalSize(), position: "relative" }}>
            {v.getVirtualItems().map((it) => {
              const f = rows[it.index];
              return f ? (
                <div key={it.key} ref={v.measureElement} data-index={it.index} style={{ position: "absolute", top: 0, left: 0, right: 0, transform: `translateY(${it.start}px)` }}>
                  <FindingRow finding={f} installId={id} />
                </div>
              ) : null;
            })}
          </div>
        </Scroller>
      )}
    </Page>
  );
}
```

`ResourcePage.tsx`:

```tsx
import styled from "@emotion/styled";
import { dump } from "js-yaml";
import { Copy } from "lucide-react";
import { useParams } from "react-router-dom";
import { useResource } from "../api/hooks";
import { ResourceName } from "../resource/ResourceName";
import { useInstallId } from "../shell/extension";
import { Tooltip } from "../shell/Tooltip";
import { PageState } from "../states/PageState";

// region Styles
const Page = styled.div`
  max-width: 1400px; margin: 0 auto; padding: ${(p) => p.theme.space(4)};
  header { display: flex; align-items: center; gap: 12px; margin-bottom: ${(p) => p.theme.space(3)}; }
  .kind { color: ${(p) => p.theme.color.textMuted}; }
  pre { margin: 0; padding: ${(p) => p.theme.space(4)}; overflow: auto; font-family: ${(p) => p.theme.font.mono}; font-size: ${(p) => p.theme.font.size.code}; line-height: 1.5;
        background: ${(p) => p.theme.color.surface}; border: 1px solid ${(p) => p.theme.color.border}; border-radius: ${(p) => p.theme.radius.md}; }
  button { width: 28px; height: 28px; display: inline-flex; align-items: center; justify-content: center; cursor: pointer;
           background: transparent; color: ${(p) => p.theme.color.textMuted}; border: 1px solid ${(p) => p.theme.color.border}; border-radius: ${(p) => p.theme.radius.sm}; }
  button:hover { color: ${(p) => p.theme.color.text}; border-color: ${(p) => p.theme.color.textMuted}; }
  button:active { background: ${(p) => p.theme.color.surfaceRaised}; }
`;

// region Helpers
/** Orders top-level keys so status comes before spec: health first (design §2 flow 2). */
function statusFirst(o: Record<string, unknown>): Record<string, unknown> {
  const { apiVersion, kind, metadata, status, spec, ...rest } = o;
  return { apiVersion, kind, metadata, status, spec, ...rest };
}

// region Component
/** Read-only YAML view of one resource; Phase 2 replaces it with detail pages. */
export function ResourcePage() {
  const id = useInstallId();
  const { group = "-", kind = "", rns = "-", rname = "" } = useParams();
  const ref = { group: group === "-" ? "" : group, kind, namespace: rns === "-" ? undefined : rns, name: rname };
  const { data, error } = useResource(id, ref);
  if (error?.status === 404) return <PageState kind="empty" title="Not in this install's scope" message={`${kind} ${ref.namespace ? `${ref.namespace}/` : ""}${rname}`} />;
  if (error) return <PageState kind="error" title="Cannot load resource" message={error.message} copyText={error.message} />;
  if (!data) return <PageState kind="loading" title="Loading…" />;
  const full = ref.namespace ? `${ref.namespace}/${rname}` : rname;
  return (
    <Page>
      <header>
        <span className="kind">{kind}</span>
        <ResourceName namespace={ref.namespace} name={rname} />
        <Tooltip label="Copy namespace/name">
          <button type="button" aria-label="Copy resource name" onClick={() => navigator.clipboard?.writeText(full)}>
            <Copy size={14} />
          </button>
        </Tooltip>
      </header>
      <pre data-testid="yaml">{dump(statusFirst(data), { noRefs: true, lineWidth: 140 })}</pre>
    </Page>
  );
}
```

`defaultPages.ts`:

```ts
import type { CorePages } from "../shell/extension";
import { ClusterLanding } from "./ClusterLanding";
import { FindingsPage } from "./FindingsPage";
import { InstallPage } from "./install/InstallPage";
import { ResourcePage } from "./ResourcePage";

/** The upstream page implementations. */
export const defaultPages: CorePages = { ClusterLanding, InstallPage, FindingsPage, ResourcePage };
```

Add to `index.ts`:

```ts
export { defaultPages } from "./pages/defaultPages";
export { FindingRow, resourcePath } from "./pages/install/FindingRow";
```

- [ ] **Step 4: Run the tests, then build**

Run: `npm test && npm run lint && npm run typecheck && npm run build`
Expected: PASS, and `web/app/dist` is built (the Task 15 `defaultPages` import now resolves).

- [ ] **Step 5: Commit**

```bash
git add web && git commit -m "feat(web): cluster landing, findings page and YAML view"
```

---

### Task 18: Frontend extension-seam test and SPA embedding

**Files:**
- Create: `web/packages/shell/src/shell/extension.test.tsx`
- Modify: `Makefile` (the `web` target already copies `dist`), `.gitignore` (already keeps only the placeholder)
- Test: `internal/webui/embed_test.go` (extended)

**Interfaces:** Consumes the shell API. Produces nothing new.

- [ ] **Step 1: Write the frontend seam test**

`web/packages/shell/src/shell/extension.test.tsx`:

```tsx
import { render, screen, waitFor } from "@testing-library/react";
import { SWRConfig } from "swr";
import { defaultPages } from "../pages/defaultPages";
import { unhealthy, overview } from "../test/fixtures";
import { stubApi } from "../test/render";
import { controlRoomDark } from "../theme/themes";
import type { Theme } from "../theme/tokens";
import { Shell } from "./Shell";

// A test-only downstream: own brand, own theme (colour and typography), own page, own finding renderer (design §9).
const acmeTheme: Theme = {
  ...controlRoomDark, id: "acme", name: "Acme",
  color: { ...controlRoomDark.color, accent: "#7A1FA2" },
  font: { ...controlRoomDark.font, ui: "Acme Sans", size: { ...controlRoomDark.font.size, sm: "13px" } },
};

it("a downstream extension contributes brand, theme, page and finding renderer", async () => {
  stubApi({
    "/api/v1/installs": [{ id: "kgw/kgateway", namespace: "kgw", name: "kgateway", version: "9.9.9", edition: "acme", controllerNames: [],
      controller: { ready: 1, desired: 1 }, scope: { mode: "all", watched: 1, total: 1 }, health: { state: "error", counts: { error: 1 } },
      computedAt: new Date().toISOString() }],
    "/api/v1/meta": { version: "dev", synced: true, kinds: [] },
    "/api/v1/installs/kgw/kgateway/overview": overview,
    "/api/v1/installs/kgw/kgateway/health": { ...unhealthy, findings: [{ code: "ACME-001", slug: "demo", severity: "error", install: "kgw/kgateway", message: "Downstream" }] },
  });
  window.history.pushState({}, "", "/i/kgw/kgateway");
  render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <Shell pages={defaultPages} extension={{
        brand: { productName: "Acme Edge", logo: <span>A</span> },
        themes: [acmeTheme], defaultThemeId: "acme",
        routes: [{ path: "waf", element: <p>waf page</p>, scope: "install" }],
        findingRenderers: { "ACME-": () => <span>acme detail</span> },
      }} />
    </SWRConfig>,
  );
  expect(await screen.findByText("Acme Edge")).toBeInTheDocument();
  await waitFor(() => expect(screen.getByText("acme detail")).toBeInTheDocument());
  // The contributed tokens actually apply (finding 15): colour, font family and type scale.
  expect(screen.getByText("v9.9.9")).toHaveStyle({ backgroundColor: "#7A1FA2" });
  expect(document.body).toHaveStyle({ fontFamily: "Acme Sans" });
  expect(screen.getAllByText("Error")[0]).toHaveStyle({ fontSize: "13px" });
  window.history.pushState({}, "", "/i/kgw/kgateway/waf");
  window.dispatchEvent(new PopStateEvent("popstate"));
  await waitFor(() => expect(screen.getByText("waf page")).toBeInTheDocument());
});
```

- [ ] **Step 2: Run it**

Run: `npm test`
Expected: PASS (the shell already supports all of this). If it fails, fix the shell, not the test.

- [ ] **Step 3: Embed the built SPA and verify from Go**

Add to `internal/webui/embed_test.go`:

```go
// After `make web`, the embedded index.html is the real SPA, not the placeholder.
func TestEmbeddedSPAWhenBuilt(t *testing.T) {
	if os.Getenv("REQUIRE_SPA") == "" {
		t.Skip("set REQUIRE_SPA=1 after `make web` (CI does)")
	}
	b, err := fs.ReadFile(FS(), "index.html")
	if err != nil || !strings.Contains(string(b), `<div id="root">`) {
		t.Fatalf("built SPA not embedded: %v", err)
	}
}
```

Add `"os"` and `"strings"` to its imports.

Run from the repo root: `make web && REQUIRE_SPA=1 go test ./internal/webui/... && make build && ./bin/kgateway-ui --kubeconfig ~/.kube/config`. Open http://127.0.0.1:8080 and check the install page visually in both themes. Take one screenshot per theme and attach them to the sub-phase complete record.
Expected: tests PASS; the page renders.

- [ ] **Step 4: Commit and close sub-phase 1.2**

```bash
git add web internal/webui && git commit -m "test(web): downstream extension seam; embed built SPA"
```

Update `docs/phase-status.md` and write `docs/phases/phase-1/phase-1.2-complete.md`.

---

# Sub-phase 1.3 — Packaging, CI, e2e

### Task 19: Container image

**Files:**
- Create: `Dockerfile`, `.dockerignore`

**Interfaces:**
- Produces: the image `ghcr.io/duncandoyle/kgateway-ui:<version>`, which runs `/kgateway-ui` as UID 65532 and listens on `:8080` in-cluster.

- [ ] **Step 1: Write the Dockerfile**

```dockerfile
# syntax=docker/dockerfile:1
# Frontend: built once on the build platform; output is architecture-independent.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
COPY web/packages/shell/package.json packages/shell/
COPY web/app/package.json app/
RUN npm ci
COPY web/ ./
RUN npm run build

# Backend: cross-compiled for the target platform with the SPA embedded.
FROM --platform=$BUILDPLATFORM golang:1.26 AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN rm -rf internal/webui/dist
COPY --from=web /src/web/app/dist internal/webui/dist
ARG TARGETOS TARGETARCH VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/DuncanDoyle/kgateway-ui/pkg/version.Version=${VERSION}" -o /out/kgateway-ui ./cmd/kgateway-ui

FROM gcr.io/distroless/static:nonroot
COPY --from=go /out/kgateway-ui /kgateway-ui
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/kgateway-ui"]
```

`.dockerignore`:

```
.git
.superpowers
bin
web/node_modules
web/**/node_modules
web/**/dist
docs
```

- [ ] **Step 2: Build it and check it**

Run: `docker buildx build --platform linux/amd64,linux/arm64 -t kgateway-ui:dev --build-arg VERSION=dev .`, then `docker build -t kgateway-ui:dev . && docker run --rm kgateway-ui:dev --help`.
Expected: both platforms build; `--help` prints the flags from Task 11.

- [ ] **Step 3: Commit**

```bash
git add Dockerfile .dockerignore && git commit -m "build: multi-arch distroless image with embedded SPA"
```

---

### Task 20: Helm chart with RBAC assertions

**Files:**
- Create: `deploy/helm/kgateway-ui/Chart.yaml`, `values.yaml`, `templates/_helpers.tpl`, `templates/deployment.yaml`, `templates/service.yaml`, `templates/serviceaccount.yaml`, `templates/clusterrole.yaml`, `templates/clusterrolebinding.yaml`, `templates/httproute.yaml`, `templates/NOTES.txt`, `README.md`
- Test: `deploy/helm/chart_test.go`

**Interfaces:**
- Produces these values: `image.repository`, `image.tag`, `install.pin`, `httpRoute.enabled`, `httpRoute.parentRefs`, `httpRoute.hostnames`, `resources`, `podSecurityContext`, `securityContext`, `serviceAccount.create`, `serviceAccount.name`.

- [ ] **Step 1: Write the failing chart test**

`deploy/helm/chart_test.go`:

```go
// Package helm tests the rendered chart. It needs the helm CLI on PATH.
package helm

import (
	"bytes"
	"os/exec"
	"slices"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

func render(t *testing.T, args ...string) []map[string]any {
	t.Helper()
	out, err := exec.Command("helm", append([]string{"template", "ui", "./kgateway-ui", "-n", "kgateway-ui"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	var docs []map[string]any
	for _, d := range bytes.Split(out, []byte("\n---")) {
		var m map[string]any
		if err := yaml.Unmarshal(d, &m); err == nil && m != nil {
			docs = append(docs, m)
		}
	}
	return docs
}

func kind(docs []map[string]any, k string) []map[string]any {
	var out []map[string]any
	for _, d := range docs {
		if d["kind"] == k {
			out = append(out, d)
		}
	}
	return out
}

func TestClusterRoleIsReadOnlyWithoutSecretsOrProxy(t *testing.T) {
	crs := kind(render(t), "ClusterRole")
	if len(crs) != 1 {
		t.Fatalf("want 1 ClusterRole, got %d", len(crs))
	}
	b, _ := yaml.Marshal(crs[0])
	var cr rbacv1.ClusterRole
	if err := yaml.Unmarshal(b, &cr); err != nil {
		t.Fatal(err)
	}
	for _, r := range cr.Rules {
		for _, v := range r.Verbs {
			if !slices.Contains([]string{"get", "list", "watch"}, v) {
				t.Errorf("verb %q is not read-only (D-001)", v)
			}
		}
		for _, res := range r.Resources {
			if res == "secrets" || res == "configmaps" || strings.Contains(res, "/proxy") || res == "*" && slices.Contains(r.APIGroups, "") {
				t.Errorf("resource %q must not be granted", res)
			}
		}
	}
}

func TestHTTPRouteIsOptIn(t *testing.T) {
	if n := len(kind(render(t), "HTTPRoute")); n != 0 {
		t.Fatalf("HTTPRoute must be off by default, got %d", n)
	}
	docs := render(t, "--set", "httpRoute.enabled=true", "--set", "httpRoute.parentRefs[0].name=edge",
		"--set", "httpRoute.parentRefs[0].namespace=infra", "--set", "httpRoute.hostnames[0]=ui.example.com")
	if n := len(kind(docs, "HTTPRoute")); n != 1 {
		t.Fatalf("want HTTPRoute when enabled, got %d", n)
	}
}

func TestPinnedInstallIsPassedAsFlag(t *testing.T) {
	b, _ := yaml.Marshal(kind(render(t, "--set", "install.pin=kgw/kgateway"), "Deployment")[0])
	if !strings.Contains(string(b), "--install=kgw/kgateway") {
		t.Fatalf("pin flag missing:\n%s", b)
	}
}

func TestPodIsHardened(t *testing.T) {
	b, _ := yaml.Marshal(kind(render(t), "Deployment")[0])
	for _, want := range []string{"runAsNonRoot: true", "readOnlyRootFilesystem: true", "allowPrivilegeEscalation: false", "/readyz", "/healthz"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("deployment lacks %q", want)
		}
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./deploy/helm/...`
Expected: FAIL (the chart does not exist)

- [ ] **Step 3: Write the chart**

`Chart.yaml`:

```yaml
apiVersion: v2
name: kgateway-ui
description: Read-only web UI for kgateway
type: application
version: 0.1.0
appVersion: "0.1.0"
```

`values.yaml`:

```yaml
image:
  repository: ghcr.io/duncandoyle/kgateway-ui
  tag: ""            # defaults to the chart appVersion
  pullPolicy: IfNotPresent
install:
  pin: ""            # "namespace/name" of one kgateway controller; empty shows all installs
# Trust model (D-013): the UI has no login. Anyone who reaches it sees everything
# its ServiceAccount can read (every watched kind, Deployments included, never
# Secrets), which can be more than their own Kubernetes RBAC allows.
httpRoute:
  # Expose the UI through an existing Gateway. Only enable this with
  # authentication and authorisation on the route, for example a kgateway
  # TrafficPolicy with OIDC or external auth that admits only operators.
  enabled: false
  parentRefs: []
  hostnames: []
serviceAccount:
  create: true
  name: ""
resources:
  requests: { cpu: 50m, memory: 128Mi }
  limits: { memory: 512Mi }
podSecurityContext:
  runAsNonRoot: true
  seccompProfile: { type: RuntimeDefault }
securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities: { drop: [ALL] }
```

`templates/_helpers.tpl`:

```yaml
{{- define "kgateway-ui.name" -}}kgateway-ui{{- end }}
{{- define "kgateway-ui.fullname" -}}{{ .Release.Name }}-kgateway-ui{{- end }}
{{- define "kgateway-ui.labels" -}}
app.kubernetes.io/name: kgateway-ui
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}
{{- define "kgateway-ui.selector" -}}
app.kubernetes.io/name: kgateway-ui
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
{{- define "kgateway-ui.sa" -}}{{ default (include "kgateway-ui.fullname" .) .Values.serviceAccount.name }}{{- end }}
```

`templates/deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "kgateway-ui.fullname" . }}
  labels: {{- include "kgateway-ui.labels" . | nindent 4 }}
spec:
  replicas: 1
  selector:
    matchLabels: {{- include "kgateway-ui.selector" . | nindent 6 }}
  template:
    metadata:
      labels: {{- include "kgateway-ui.selector" . | nindent 8 }}
    spec:
      serviceAccountName: {{ include "kgateway-ui.sa" . }}
      securityContext: {{- toYaml .Values.podSecurityContext | nindent 8 }}
      containers:
        - name: kgateway-ui
          image: "{{ .Values.image.repository }}:{{ default .Chart.AppVersion .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          args:
            - --listen=:8080
            {{- with .Values.install.pin }}
            - --install={{ . }}
            {{- end }}
          ports:
            - { name: http, containerPort: 8080 }
          readinessProbe: { httpGet: { path: /readyz, port: http }, periodSeconds: 5 }
          livenessProbe: { httpGet: { path: /healthz, port: http }, periodSeconds: 10 }
          resources: {{- toYaml .Values.resources | nindent 12 }}
          securityContext: {{- toYaml .Values.securityContext | nindent 12 }}
```

`templates/service.yaml`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: {{ include "kgateway-ui.fullname" . }}
  labels: {{- include "kgateway-ui.labels" . | nindent 4 }}
spec:
  type: ClusterIP
  selector: {{- include "kgateway-ui.selector" . | nindent 4 }}
  ports:
    - { name: http, port: 80, targetPort: http }
```

`templates/serviceaccount.yaml`:

```yaml
{{- if .Values.serviceAccount.create }}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ include "kgateway-ui.sa" . }}
  labels: {{- include "kgateway-ui.labels" . | nindent 4 }}
{{- end }}
```

`templates/clusterrole.yaml` (read-only, no Secrets, no `*/proxy`; design §8):

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ include "kgateway-ui.fullname" . }}
  labels: {{- include "kgateway-ui.labels" . | nindent 4 }}
rules:
  - apiGroups: [gateway.networking.k8s.io]
    resources: [gatewayclasses, gateways, httproutes, grpcroutes, tcproutes, tlsroutes, referencegrants, backendtlspolicies, listenersets]
    verbs: [get, list, watch]
  - apiGroups: [gateway.networking.x-k8s.io]
    resources: [xlistenersets]
    verbs: [get, list, watch]
  - apiGroups: [gateway.kgateway.dev]
    resources: ["*"]
    verbs: [get, list, watch]
  - apiGroups: [apiextensions.k8s.io]
    resources: [customresourcedefinitions]
    verbs: [get, list, watch]
  - apiGroups: [""]
    resources: [namespaces, pods]
    verbs: [get, list, watch]
  - apiGroups: [apps]
    resources: [deployments]
    verbs: [get, list, watch]
```

`templates/clusterrolebinding.yaml`:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ include "kgateway-ui.fullname" . }}
  labels: {{- include "kgateway-ui.labels" . | nindent 4 }}
roleRef: { apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: {{ include "kgateway-ui.fullname" . }} }
subjects:
  - { kind: ServiceAccount, name: {{ include "kgateway-ui.sa" . }}, namespace: {{ .Release.Namespace }} }
```

`templates/httproute.yaml`:

```yaml
{{- if .Values.httpRoute.enabled }}
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: {{ include "kgateway-ui.fullname" . }}
  labels: {{- include "kgateway-ui.labels" . | nindent 4 }}
spec:
  parentRefs: {{- toYaml .Values.httpRoute.parentRefs | nindent 4 }}
  {{- with .Values.httpRoute.hostnames }}
  hostnames: {{- toYaml . | nindent 4 }}
  {{- end }}
  rules:
    - backendRefs:
        - { name: {{ include "kgateway-ui.fullname" . }}, port: 80 }
{{- end }}
```

`templates/NOTES.txt`:

```
kgateway UI is installed. It is read-only and has no login of its own.

Trust model: anyone who can reach the UI sees everything it shows, which is
everything its ServiceAccount can read (never Secrets). That can be more than
their own Kubernetes RBAC allows. Treat access to the UI as cluster-read access.

Open it with:
  kubectl -n {{ .Release.Namespace }} port-forward svc/{{ include "kgateway-ui.fullname" . }} 8080:80
  open http://localhost:8080
{{- if .Values.httpRoute.enabled }}

It is also exposed through an HTTPRoute. Put authentication and authorisation
on that route, for example a kgateway TrafficPolicy with OIDC or external auth
that admits only the people who may read the cluster.
{{- end }}

Controller metrics are read directly from the controller pods on port 9092.
If a NetworkPolicy blocks that, the UI reports "signal unavailable" instead of green.
```

`README.md` (chart):
- A **Security and trust model** section at the top (D-013): the UI has no login; whoever reaches it sees everything its ServiceAccount can read, which can exceed their own RBAC; the ClusterRole never grants Secrets and no other field is redacted; port-forward to the ClusterIP Service is the default; `httpRoute.enabled` is for operators who put authentication and authorisation on the route (for example a kgateway TrafficPolicy); per-user authorisation is on the roadmap.
- The values table, with the `httpRoute` rows pointing at that section.
- The RBAC rationale (read-only; no Secrets; Pods and Deployments are read but only watched through label selectors).
- The NetworkPolicy note.

- [ ] **Step 4: Run the tests and lint the chart**

Run: `helm lint deploy/helm/kgateway-ui && go test ./deploy/helm/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add deploy && git commit -m "deploy: helm chart with read-only RBAC and optional HTTPRoute"
```

---

### Task 21: CI and release workflows

**Files:**
- Create: `.github/workflows/ci.yaml`, `.github/workflows/release.yaml`, `.golangci.yaml`

**Interfaces:** Produces the required checks `go`, `web`, `helm` and `hygiene` on every PR. On a `v*` tag it publishes the image, the OCI chart and a GitHub release.

- [ ] **Step 1: Write the workflows**

`.github/workflows/ci.yaml`:

```yaml
name: ci
on:
  pull_request:
  push: { branches: [main] }
permissions: { contents: read }
jobs:
  hygiene:
    # Public repo: forbidden brand terms must never land (Global Constraints).
    runs-on: ubuntu-latest
    env: { FORBIDDEN_TERMS: "${{ vars.FORBIDDEN_TERMS }}" }
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - run: |
          # The pattern lives in a repository variable so the term list itself never lands in this public repo.
          if [ -z "$FORBIDDEN_TERMS" ]; then echo "::error::repository variable FORBIDDEN_TERMS is not set"; exit 1; fi
          if git grep -inE "$FORBIDDEN_TERMS" -- . ':!web/package-lock.json' ':!go.sum'; then
            echo "::error::forbidden term found"; exit 1; fi
  web:
    runs-on: ubuntu-latest
    defaults: { run: { working-directory: web } }
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: 22, cache: npm, cache-dependency-path: web/package-lock.json }
      - run: npm ci
      - run: npm run lint
      - run: npm run typecheck
      - run: npm test
      - run: npm run build
      - uses: actions/upload-artifact@v4
        with: { name: spa, path: web/app/dist }
  go:
    runs-on: ubuntu-latest
    needs: web
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: actions/download-artifact@v4
        with: { name: spa, path: internal/webui/dist }
      - run: go vet ./...
      - uses: golangci/golangci-lint-action@v8
      - run: REQUIRE_SPA=1 go test -race ./...
  helm:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: azure/setup-helm@v4
      - run: helm lint deploy/helm/kgateway-ui
      - run: go test ./deploy/helm/...
```

The hygiene job greps the whole tree, including `docs/`, to apply D-005's naming rule to the docs too. Lock files are excluded because they contain third-party package names.

`.github/workflows/release.yaml`:

```yaml
name: release
on:
  push: { tags: ["v*"] }
permissions: { contents: write, packages: write }
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-qemu-action@v3
      - uses: docker/setup-buildx-action@v3
      - uses: docker/login-action@v3
        with: { registry: ghcr.io, username: "${{ github.actor }}", password: "${{ secrets.GITHUB_TOKEN }}" }
      - name: image
        uses: docker/build-push-action@v6
        with:
          push: true
          platforms: linux/amd64,linux/arm64
          build-args: VERSION=${{ github.ref_name }}
          tags: ghcr.io/duncandoyle/kgateway-ui:${{ github.ref_name }}
      - uses: azure/setup-helm@v4
      - name: chart
        run: |
          v="${GITHUB_REF_NAME#v}"
          helm package deploy/helm/kgateway-ui --version "$v" --app-version "$GITHUB_REF_NAME"
          echo "${{ secrets.GITHUB_TOKEN }}" | helm registry login ghcr.io -u "${{ github.actor }}" --password-stdin
          helm push "kgateway-ui-$v.tgz" oci://ghcr.io/duncandoyle/charts
      - run: gh release create "$GITHUB_REF_NAME" --generate-notes
        env: { GH_TOKEN: "${{ github.token }}" }
```

`.golangci.yaml`:

```yaml
version: "2"
linters:
  enable: [errcheck, govet, staticcheck, unused, revive, gosec]
  settings:
    revive:
      rules:
        - name: exported   # every exported identifier needs a doc comment (Go doc standard)
```

- [ ] **Step 2: Verify locally**

Run: `golangci-lint run ./...` and `act -j hygiene` (if `act` is installed; otherwise rely on the PR run).
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add .github .golangci.yaml && git commit -m "ci: lint, test, build, hygiene and release workflows"
```

---

### Task 22: kind e2e against kgateway 2.3 and 2.4

**Files:**
- Create: `test/e2e/setup.sh`, `test/e2e/fixtures/base.yaml`, `test/e2e/fixtures/second-install.yaml`, `test/e2e/api_test.go` (build tag `e2e`)
- Create: `web/e2e/playwright.config.ts`, `web/e2e/smoke.spec.ts`
- Create: `.github/workflows/e2e.yaml`

**Interfaces:**
- Consumes: the image (Task 19), the chart (Task 20) and the API (Task 10).
- Produces: `make e2e KGW_VERSION=v2.4.7 GWAPI_VERSION=v1.6.1`.

Each kgateway install uses the Helm release name `kgateway` in its own namespace, so `kgateway.fullname` renders the controller Deployment as `kgateway` and the install IDs are `kgw-a/kgateway` and `kgw-b/kgateway` (finding 13). The policy-target capability is versioned (finding 7): only kgateway `main` writes `TargetNotFound`; on v2.3.x and v2.4.x the policy gets no status and is reported as not yet processed.

Scenario (the fixture names are used in the assertions below):

| Object | Namespace (labels) | Expected in UI |
|---|---|---|
| Install A: kgateway (release `kgateway`), `discoveryNamespaceSelectors=[{matchLabels:{team:a}}]` | `kgw-a` | install `kgw-a/kgateway` |
| Gateway `edge` (class `kgateway`) | `infra` (team=a) | Gateway count 1; row Healthy |
| HTTPRoute `good` → httpbin Service | `shop` (team=a) | counted, no finding |
| HTTPRoute `broken` → Service `missing` | `shop` | KGWUI-002 "Backend not found" |
| HTTPRoute `posts` → `infra/edge` | `blog` (no label) | not counted; KGWUI-004 |
| TrafficPolicy `tp-missing-target` targeting HTTPRoute `nope` | `shop` | v2.3.x and v2.4.x: KGWUI-003 "Not yet processed by the controller" (no status). `main` (non-blocking job only): KGWUI-002 "Policy target not found" |
| (second scenario) Install B: release `kgateway`, selector `team=b`, Gateway `edge` in `app-b` (team=b) | `kgw-b` | second install `kgw-b/kgateway`; cluster landing shows 2 cards |

- [ ] **Step 1: Write the setup script**

`test/e2e/setup.sh`:

```bash
#!/usr/bin/env bash
# Creates a kind cluster with kgateway, fixtures and the UI. Usage:
#   KGW_VERSION=v2.4.7 GWAPI_VERSION=v1.6.1 [SECOND_INSTALL=1] test/e2e/setup.sh
set -euo pipefail
: "${KGW_VERSION:?}" "${GWAPI_VERSION:?}"
CLUSTER=kgwui-e2e
kind create cluster --name "$CLUSTER" --wait 120s
kubectl apply --server-side -f "https://github.com/kubernetes-sigs/gateway-api/releases/download/${GWAPI_VERSION}/standard-install.yaml"
helm install kgateway-crds oci://cr.kgateway.dev/kgateway-dev/charts/kgateway-crds --version "$KGW_VERSION" -n kgw-a --create-namespace
# Release "kgateway" in each namespace: the controller Deployment is <ns>/kgateway (kgateway.fullname).
helm install kgateway oci://cr.kgateway.dev/kgateway-dev/charts/kgateway --version "$KGW_VERSION" -n kgw-a \
  --set-json 'discoveryNamespaceSelectors=[{"matchLabels":{"team":"a"}}]' --wait
kubectl get deploy -n kgw-a kgateway   # fail fast if the rendered name ever changes
kubectl apply -f test/e2e/fixtures/base.yaml
if [[ "${SECOND_INSTALL:-}" == 1 ]]; then
  helm install kgateway oci://cr.kgateway.dev/kgateway-dev/charts/kgateway --version "$KGW_VERSION" -n kgw-b --create-namespace \
    --set-json 'discoveryNamespaceSelectors=[{"matchLabels":{"team":"b"}}]' --wait
  kubectl get deploy -n kgw-b kgateway
  kubectl apply -f test/e2e/fixtures/second-install.yaml
fi
kubectl wait -n infra --for=condition=Programmed gateway/edge --timeout=180s
docker build -t kgateway-ui:e2e --build-arg VERSION=e2e .
kind load docker-image kgateway-ui:e2e --name "$CLUSTER"
helm install ui deploy/helm/kgateway-ui -n kgateway-ui --create-namespace \
  --set image.repository=kgateway-ui --set image.tag=e2e --set image.pullPolicy=Never --wait
kubectl -n kgateway-ui port-forward svc/ui-kgateway-ui 18080:80 >/dev/null 2>&1 &
echo "UI at http://127.0.0.1:18080"
```

- [ ] **Step 2: Write the fixtures**

`test/e2e/fixtures/base.yaml`:

```yaml
apiVersion: v1
kind: Namespace
metadata: { name: infra, labels: { team: a } }
---
apiVersion: v1
kind: Namespace
metadata: { name: shop, labels: { team: a } }
---
apiVersion: v1
kind: Namespace
metadata: { name: blog }
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata: { name: edge, namespace: infra }
spec:
  gatewayClassName: kgateway
  listeners:
    - name: http
      port: 8080
      protocol: HTTP
      allowedRoutes: { namespaces: { from: All } }
---
apiVersion: apps/v1
kind: Deployment
metadata: { name: httpbin, namespace: shop }
spec:
  selector: { matchLabels: { app: httpbin } }
  template:
    metadata: { labels: { app: httpbin } }
    spec: { containers: [{ name: httpbin, image: mccutchen/go-httpbin:v2.15.0, ports: [{ containerPort: 8080 }] }] }
---
apiVersion: v1
kind: Service
metadata: { name: httpbin, namespace: shop }
spec: { selector: { app: httpbin }, ports: [{ port: 8000, targetPort: 8080 }] }
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata: { name: good, namespace: shop }
spec:
  parentRefs: [{ name: edge, namespace: infra }]
  rules: [{ backendRefs: [{ name: httpbin, port: 8000 }] }]
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata: { name: broken, namespace: shop }
spec:
  parentRefs: [{ name: edge, namespace: infra }]
  rules: [{ matches: [{ path: { type: PathPrefix, value: /broken } }], backendRefs: [{ name: missing, port: 8000 }] }]
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata: { name: posts, namespace: blog }
spec:
  parentRefs: [{ name: edge, namespace: infra }]
  rules: [{ backendRefs: [{ name: httpbin, port: 8000 }] }]
---
apiVersion: gateway.kgateway.dev/v1alpha1
kind: TrafficPolicy
metadata: { name: tp-missing-target, namespace: shop }
spec:
  targetRefs: [{ group: gateway.networking.k8s.io, kind: HTTPRoute, name: nope }]
  timeouts: { request: 10s }
```

The `timeouts` field must be valid for both kgateway 2.3 and 2.4. Verify it with `kubectl explain trafficpolicy.spec` on each version, and use whichever TrafficPolicy field both versions accept.

`test/e2e/fixtures/second-install.yaml`:

```yaml
apiVersion: v1
kind: Namespace
metadata: { name: app-b, labels: { team: b } }
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata: { name: edge, namespace: app-b }
spec:
  gatewayClassName: kgateway
  listeners: [{ name: http, port: 8080, protocol: HTTP }]
```

- [ ] **Step 3: Write the API assertions**

`test/e2e/api_test.go`:

```go
//go:build e2e

// Package e2e checks the deployed UI's API against a real kgateway install.
package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
)

var base = "http://127.0.0.1:18080"

func get(t *testing.T, path string, v any) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		resp, err := http.Get(base + path)
		if err == nil && resp.StatusCode == 200 {
			defer resp.Body.Close()
			if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
				t.Fatal(err)
			}
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s never succeeded: %v", path, err)
		}
		time.Sleep(2 * time.Second)
	}
}

func has(h api.HealthResponse, code, res string) bool {
	for _, f := range h.Findings {
		if f.Code == code && f.Resource != nil && f.Resource.String() == res {
			return true
		}
	}
	return false
}

// tpMissingTarget is the finding expected for tp-missing-target. Only kgateway
// main reports TargetNotFound; v2.3.x and v2.4.x write no status for a policy
// whose target is missing, so it is "not yet processed" (finding 7).
func tpMissingTarget() string {
	if os.Getenv("KGW_MAIN") == "1" {
		return health.CodeConditionFalse
	}
	return health.CodeRouteUnattached
}

func TestRealCluster(t *testing.T) {
	var insts []api.InstallSummary
	get(t, "/api/v1/installs", &insts)
	want := 1
	if os.Getenv("SECOND_INSTALL") == "1" {
		want = 2
	}
	if len(insts) != want {
		t.Fatalf("installs: want %d got %+v", want, insts)
	}
	var o api.OverviewResponse
	get(t, "/api/v1/installs/kgw-a/kgateway/overview", &o)
	counts := map[string]int{}
	for _, c := range o.Counts {
		counts[c.Kind] = c.Count
	}
	if counts["Gateway"] != 1 || counts["HTTPRoute"] != 2 {
		t.Fatalf("counts: %+v (posts in unlabelled blog must not count)", counts)
	}
	// Findings settle once kgateway has written status and metrics were scraped.
	deadline := time.Now().Add(2 * time.Minute)
	for {
		var h api.HealthResponse
		get(t, "/api/v1/installs/kgw-a/kgateway/health", &h)
		ok := has(h, health.CodeConditionFalse, "shop/broken") && has(h, health.CodeNamespaceNotWatched, "blog/posts") &&
			has(h, tpMissingTarget(), "shop/tp-missing-target")
		xds := false
		for _, c := range h.Checks {
			if c.Name == "xDS rejects" && c.OK {
				xds = true // in-cluster metrics scraping works
			}
		}
		if ok && xds && h.State == health.StateError {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("health never settled: state=%s findings=%+v checks=%+v", h.State, h.Findings, h.Checks)
		}
		time.Sleep(3 * time.Second)
	}
}
```

- [ ] **Step 4: Write the Playwright smoke test**

`web/e2e/playwright.config.ts`:

```ts
import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: ".",
  use: { baseURL: process.env.UI_URL ?? "http://127.0.0.1:18080", video: "retain-on-failure", screenshot: "only-on-failure" },
  timeout: 120_000,
});
```

`web/e2e/smoke.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

for (const theme of ["Control room", "Control room light"]) {
  test(`install page, findings and YAML view (${theme})`, async ({ page }) => {
    await page.goto("/");
    if (process.env.SECOND_INSTALL === "1") {
      await expect(page.getByRole("link", { name: /kgw-a\/kgateway/ })).toBeVisible();
      await page.getByRole("link", { name: /kgw-a\/kgateway/ }).click();
    }
    await expect(page).toHaveURL(/\/i\/kgw-a\/kgateway$/);
    await page.getByRole("button", { name: "Theme" }).click();
    await page.getByRole("menuitemradio", { name: theme, exact: true }).click();
    await expect(page.getByTestId("health-strip")).toContainText(/error/);
    await expect(page.getByRole("table", { name: "Gateways" })).toContainText("edge");
    await page.getByRole("link", { name: /Show all/ }).click();
    await expect(page.getByText("Namespace not watched by the controller")).toBeVisible();
    await page.getByRole("link", { name: /broken/ }).first().click();
    await expect(page.getByTestId("yaml")).toContainText("BackendNotFound");
    await page.screenshot({ path: `test-results/smoke-${theme.replace(/ /g, "-")}.png`, fullPage: true });
  });
}
```

- [ ] **Step 5: Write the workflow**

`.github/workflows/e2e.yaml`:

```yaml
name: e2e
on:
  pull_request:
  push: { branches: [main] }
permissions: { contents: read }
jobs:
  kind:
    strategy:
      fail-fast: false
      matrix:
        include:
          - { kgw: v2.4.7, gwapi: v1.6.1, second: "0" }
          - { kgw: v2.3.11, gwapi: v1.5.1, second: "0" }
          - { kgw: v2.4.7, gwapi: v1.6.1, second: "1" }
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: actions/setup-node@v4
        with: { node-version: 22, cache: npm, cache-dependency-path: web/package-lock.json }
      - uses: helm/kind-action@v1
        with: { install_only: true }
      - uses: azure/setup-helm@v4
      - run: KGW_VERSION=${{ matrix.kgw }} GWAPI_VERSION=${{ matrix.gwapi }} SECOND_INSTALL=${{ matrix.second }} bash test/e2e/setup.sh
      - run: SECOND_INSTALL=${{ matrix.second }} go test -tags e2e ./test/e2e/... -v
      - run: cd web && npm ci && npx playwright install --with-deps chromium && SECOND_INSTALL=${{ matrix.second }} npx playwright test -c e2e/playwright.config.ts
      - if: always()
        uses: actions/upload-artifact@v4
        with: { name: "e2e-${{ matrix.kgw }}-${{ matrix.second }}", path: web/test-results }
      - if: failure()
        run: kubectl logs -n kgateway-ui deploy/ui-kgateway-ui --tail=200; kubectl get gateways,httproutes -A -o wide
  # Non-blocking: kgateway main is the only line that reports TargetNotFound (finding 7).
  # It runs once the repository variable KGW_MAIN_VERSION names an installable main-build
  # chart version; main's go.mod pins Gateway API v1.6.2.
  kind-main:
    if: ${{ vars.KGW_MAIN_VERSION != '' }}
    continue-on-error: true
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: helm/kind-action@v1
        with: { install_only: true }
      - uses: azure/setup-helm@v4
      - run: KGW_VERSION=${{ vars.KGW_MAIN_VERSION }} GWAPI_VERSION=v1.6.2 bash test/e2e/setup.sh
      - run: KGW_MAIN=1 go test -tags e2e ./test/e2e/... -v
```

Until `KGW_MAIN_VERSION` is set, the `kind-main` job is skipped; track setting it in Carry-over (not required to close Phase 1).

- [ ] **Step 6: Run locally**

Run: `KGW_VERSION=v2.4.7 GWAPI_VERSION=v1.6.1 bash test/e2e/setup.sh && go test -tags e2e ./test/e2e/... -v && (cd web && npx playwright test -c e2e/playwright.config.ts)`
Expected: PASS, with screenshots in `web/test-results/`.

- [ ] **Step 7: Commit**

```bash
git add test web/e2e .github/workflows/e2e.yaml && git commit -m "test(e2e): kind matrix on kgateway 2.3/2.4 with API and Playwright checks"
```

---

### Task 23: README, upstream proposal draft, and closing Phase 1

**Files:**
- Create: `README.md`
- Modify: `docs/phase-status.md`, `docs/roadmap.md`

- [ ] **Step 1: Write the README**

Its sections:
1. **What it is.** A read-only kgateway UI; health at a glance, why, and the inventory.
2. **Security and trust model** (D-013), placed before Install so nobody exposes the UI without reading it:
   - The UI has no login in this release. Whoever can reach it sees everything it shows: everything its ServiceAccount can read across the cluster, Deployments included. That can be more than the viewer's own Kubernetes RBAC allows (the Argo CD model).
   - It never reads Secrets; the ClusterRole excludes them. No other field is redacted, because a secret can sit in any field (an inline token, a literal env var).
   - Default exposure is `kubectl port-forward` to the ClusterIP Service. The optional `httpRoute` (off by default) is for operators who put authentication and authorisation on that route, for example a kgateway TrafficPolicy with OIDC or external auth.
   - Downstream distributions and future releases plug per-user authorisation into the `api.Authorizer` seam (`extension.WithAuthorizer`); a finding on a resource the viewer may not see is shown as restricted, and the health state still counts it. Per-user AuthN/AuthZ is on the roadmap.
3. **Install.** The Helm OCI command and the port-forward, linking back to section 2 for `httpRoute`.
4. **Run locally.** `make web build && ./bin/kgateway-ui --kubeconfig …`.
5. **What it reads.** Read-only RBAC; no Secrets; controller metrics on port 9092.
6. **Scope.** It honours `discoveryNamespaceSelectors` and shows "N / M namespaces".
7. **Extending.** A `pkg/extension` options example (including `WithAuthorizer`) and a shell `ShellExtension` example, using a neutral example name (`acme`).
8. **Supported versions.** v2.3.11 and v2.4.7; note that missing policy targets are reported as "not yet processed" there, and as "Policy target not found" only on kgateway `main`.
9. **Contributing.** The SDLC docs live in `docs/`.
10. **Licence.** Apache-2.0.

The README must not contain any of the forbidden terms.

- [ ] **Step 2: Draft the upstream proposal (do not file it)**

Write the issue text for `kgateway-dev/kgateway`: "Expose the controller's effective discovery scope through a read-only endpoint" (D-009 §7).
- **Problem:** external tools must mirror `discoveryNamespaceSelectors`.
- **Proposal:** a read-only endpoint that does not expose pprof or `/logging`. It returns controller identity, version, `controllerName`s, the parsed selectors, the selected namespaces, and the enabled kinds.
- **Alternatives:** admin `/snapshots/krt` (unstable, includes ConfigMap data); the metrics.
- **Open questions:** which port; how to authenticate.

Run the `sanitize-kgateway-oss-resources` skill on it, put the AI-assisted note at the top, and **hand it to the user to file**. Never post it.

- [ ] **Step 3: Close sub-phase 1.3**

Write `docs/phases/phase-1/phase-1.3-complete.md` and update `docs/phase-status.md`, with these Carry-over items: (1) design-review finding 16, extension seams for analyzers, API routes, per-kind renderers and health cards, for the seam milestone after Phase 2; (2) set the `KGW_MAIN_VERSION` repository variable so the non-blocking `kind-main` e2e job runs; (3) per-user AuthN/AuthZ behind `api.Authorizer` (D-013). Then request the Phase 1 cross-model review (SDLC "review phase 1"). Closing Phase 1 is a separate step, gated by the exit gates.

- [ ] **Step 4: Commit**

```bash
git add README.md docs && git commit -m "docs: README; close sub-phase 1.3"
```
