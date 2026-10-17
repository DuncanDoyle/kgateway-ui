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

**Spec:** `docs/phases/phase-1/design.md` (decisions D-001 … D-011 in `docs/decisions.md`). Read both before starting any task.

## Global Constraints

- The repo is **PUBLIC**. The forbidden brand terms of the public-repo hygiene rule must not appear in code, comments, file names, docs, commit messages or PR text. The agent tooling's `public-repo-hygiene` skill holds the list; this repo never spells it out. Downstreams are called "downstream distributions".
  - The CI `hygiene` job reads the pattern from the GitHub repository variable `FORBIDDEN_TERMS` (set once by the repo owner).
  - Before every commit, run the same check locally: `git diff --cached | grep -inE "$FORBIDDEN_TERMS" && echo STOP || true`, with `FORBIDDEN_TERMS` exported in your shell.
- **Read-only (D-001):** no create/update/patch/delete calls to the Kubernetes API, ever. **No Secrets:** never list, watch or serve Secrets.
- Never call the controller admin server (`:9095`) or the Envoy admin API.
- **Go:**
  - Module `github.com/DuncanDoyle/kgateway-ui`, `go 1.26`.
  - **Do not import `github.com/kgateway-dev/kgateway`.**
  - Every exported identifier has a Go doc comment (https://go.dev/doc/comment).
- **Frontend:**
  - No proprietary component libraries.
  - One React component per file, named export.
  - `// region Styles` / `// region Helpers` / `// region Component` ordering.
  - emotion styled components, each accepting `stylesOverride`.
  - Text must never rely on colour alone. Health is always icon + colour + text.
- **Upstream contract** (verified on kgateway `8a498c6406`, v2.4.x and v2.3.x):

  | Item | Value |
  |---|---|
  | Controller labels | `kgateway=kgateway`, `app.kubernetes.io/component=controller` |
  | Controller image name | last path segment `kgateway` (`cr.kgateway.dev/kgateway-dev/kgateway:<tag>`) |
  | Version label | `app.kubernetes.io/version` |
  | Metrics container port | name `metrics`, default `9092` |
  | Proxy Deployment labels | `app.kubernetes.io/managed-by=kgateway`, `app.kubernetes.io/component=proxy`, `gateway.networking.k8s.io/gateway-name=<gw>` |
  | Controller name | `kgateway.dev/kgateway` (including the `kgateway-waypoint` class) |
  | Scope env | `KGW_DISCOVERY_NAMESPACE_SELECTORS`: a JSON list of `metav1.LabelSelector`, OR'ed; empty or unset means all namespaces |
  | Reject metric | `kgateway_envoy_xds_rejects_active{gateway_namespace,gateway_name,type_url}` |
  | Managed metric | `kgateway_resources_managed{namespace,parent,resource}`. Weights: Gateway 1; route 1 per `spec.parentRefs` entry; ListenerSet 1 as `resource="XListenerSet"`; policy 1 under its kind |
  | Route condition | `kgateway.dev/Programmed`; reasons `RouteRuleDropped` and `RouteRuleReplaced` |
  | Policy conditions | `Accepted` / `Attached`; reasons `Valid`, `Invalid`, `Attached`, `Merged`, `Overridden`, `Pending`, `PartiallyValid`, `TargetNotFound`; synthetic ancestor kind `StatusSummary`, group `gateway.kgateway.dev` |

- **Finding codes** are stable: `KGWUI-001` xds-rejected … `KGWUI-009` ambiguous-ownership (design §5.2).
- **Roll-up:** error > unknown > warning > healthy (design §5.3).
- **Supported versions:** kgateway v2.3.11 (Gateway API v1.5.1) and v2.4.7 (Gateway API v1.6.1).
- **AGENTS.md change-size rule:** approval of this plan covers its file count and size. Anything beyond it needs approval again.

## Review Focus

These failure modes are implied by the spec, but no other task's tests exercise them. Each has a test in the owning task.

1. **Long Gateway names (more than 63 characters).** The proxy Deployment's `gateway-name` label is truncated. The proxy must still be matched to its Gateway (by ownerReference first, then label); otherwise the UI raises a false "proxy not found". → Task 9.
2. **Cross-namespace parentRefs.** A route in `shop` targets `infra/edge`. Assignment and the unwatched-namespace check must use the **route's** namespace for scope, not the Gateway's. → Task 7.
3. **Controller in CrashLoopBackOff (0 ready Pods).** The scope must still be read from the Pod spec, and the report must show controller-not-ready (error) plus metrics unavailable. It must never be silently healthy. → Tasks 6 and 9.
4. **Stale conditions** (`observedGeneration < metadata.generation`). They describe an old spec. If they are older than the 60 s grace period they show as "not yet processed" (warning); they are never reported as a current error or a current success. → Task 9.
5. **Scale.** 5,000 HTTPRoutes across 200 namespaces must compute a full model (assignment, health and overview) in under 1 s on a laptop, so the 2 s recompute loop never stacks up. → Task 10.

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
pkg/api/{types.go,handlers.go}          HTTP JSON API
pkg/extension/extension.go              Registry + functional options
pkg/app/app.go                          Build/Run wiring (used by upstream and downstream mains)
internal/webui/{embed.go,dist/index.html}  embedded SPA (placeholder committed)
internal/testutil/{fixtures.go,clients.go} test fixtures and fake clients
web/package.json, web/tsconfig.base.json, web/biome.json
web/packages/shell/…                    reusable shell package
web/app/…                               upstream entry
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

**Interfaces:** Produces facts that Tasks 7 and 8 rely on.

- [ ] **Step 1: Create a cluster with two disjoint kgateway installs**

```bash
kind create cluster --name kgwui-exp
kubectl apply --server-side -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.6.1/standard-install.yaml
helm install kgateway-crds oci://cr.kgateway.dev/kgateway-dev/charts/kgateway-crds --version v2.4.7 -n kgw-a --create-namespace
# Install A watches team=a namespaces, install B watches team=b namespaces.
helm install kgw-a oci://cr.kgateway.dev/kgateway-dev/charts/kgateway --version v2.4.7 -n kgw-a \
  --set-json 'discoveryNamespaceSelectors=[{"matchLabels":{"team":"a"}}]'
helm install kgw-b oci://cr.kgateway.dev/kgateway-dev/charts/kgateway --version v2.4.7 -n kgw-b --create-namespace \
  --set-json 'discoveryNamespaceSelectors=[{"matchLabels":{"team":"b"}}]'
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
kubectl logs -n kgw-a deploy/kgw-a --since=2m | grep -i -E 'gatewayclass|conflict' | head
kubectl get gatewayclass kgateway -o jsonpath='{.metadata.resourceVersion}'; sleep 30; \
kubectl get gatewayclass kgateway -o jsonpath='{.metadata.resourceVersion}'   # flapping if it changes repeatedly
```

Expected (to be confirmed):
- each Gateway is programmed only by the install whose selector matches its namespace;
- the GatewayClass status is written by both installs, possibly flapping.

- [ ] **Step 3: Observe the metric replica semantics**

```bash
kubectl scale -n kgw-a deploy/kgw-a --replicas=2 && kubectl rollout status -n kgw-a deploy/kgw-a
for p in $(kubectl get pod -n kgw-a -l kgateway=kgateway -o name); do
  kubectl port-forward -n kgw-a $p 19092:9092 >/dev/null 2>&1 & PF=$!; sleep 2
  echo "== $p"; curl -s localhost:19092/metrics | grep -E '^kgateway_(resources_managed|envoy_xds_rejects_active)'
  kill $PF
done
```

Record whether both replicas report the same `kgateway_resources_managed` series (expected: yes; krt runs on every replica). Also record that `envoy_xds_rejects_active` is set by the replica serving the proxy's xDS stream, which is why the UI sums it.

- [ ] **Step 4: Record the results and clean up**

Append to `docs/decisions.md`:

```markdown
## D-012 — Multi-install and metric replica behaviour, observed (<experiment date, YYYY-MM-DD>, phase 1)
**Context:** Design §13 items 1–2; experiment on kind with kgateway v2.4.7, two installs with disjoint discoveryNamespaceSelectors.
**Decision:** <observed facts, verbatim command output excerpts>. Install assignment keeps design §4.3 (controllerName ∩ namespace scope; ambiguity finding when >1). Metrics: rejects summed over replicas; resources_managed max over replicas.
**Alternatives:** n/a (observation).
**Consequences:** <any change needed; if §4.3 must change, STOP and ask the user before Task 7>.
```

Then run `kind delete cluster --name kgwui-exp`.

- [ ] **Step 5: Commit**

```bash
git add docs && git commit -m "docs: record multi-install and metrics replica observations (D-012)"
```

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
  - `kinds.Category` with the constants `CategoryGatewayClass`, `CategoryGateway`, `CategoryListenerSet`, `CategoryRoute` and `CategoryPolicy`;
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
  - `objects.ParentRef{Group, Kind, Namespace, Name, SectionName string}`;
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
  - `objects.FindCondition(cs []metav1.Condition, typ string) *metav1.Condition`;
  - `objects.IsStale(u, c metav1.Condition) bool`.

- [ ] **Step 1: Write the failing test**

`pkg/objects/objects_test.go`:

```go
package objects

import (
	"testing"

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

func TestRefOf(t *testing.T) {
	got := RefOf(route())
	if got != (Ref{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Namespace: "shop", Name: "cart"}) || got.String() != "shop/cart" {
		t.Fatalf("got %+v %q", got, got.String())
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
}

type rawRef struct {
	Group       *string `json:"group"`
	Kind        *string `json:"kind"`
	Namespace   *string `json:"namespace"`
	Name        string  `json:"name"`
	SectionName *string `json:"sectionName"`
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
	return p
}

type routeView struct {
	Spec struct {
		ParentRefs []rawRef `json:"parentRefs"`
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

// IsStale reports whether c describes an older generation of u than the current
// spec. Controllers that do not set observedGeneration (0) are never stale.
func IsStale(u *unstructured.Unstructured, c metav1.Condition) bool {
	return c.ObservedGeneration != 0 && c.ObservedGeneration < u.GetGeneration()
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
  - `cache.Access{OK bool; Err string}` (JSON `ok,error`);
  - `cache.Snapshot{Objects map[schema.GroupVersionResource][]*unstructured.Unstructured; Access map[schema.GroupVersionResource]Access; Namespaces []*corev1.Namespace; Pods []*corev1.Pod; Deployments []*appsv1.Deployment; Synced bool; TakenAt time.Time}`;
  - `cache.Config{Dynamic dynamic.Interface; Kube kubernetes.Interface; Served []kinds.Served; PodSelectors, DeploymentSelectors []labels.Selector}`;
  - `cache.New(Config) *Cache`, `(*Cache).Start(ctx)`, `(*Cache).Snapshot() *Snapshot`;
  - `cache.Snapshotter` (interface `Snapshot() *Snapshot`).
- Produces (tests only):
  - `testutil.Clients(t, objs ...runtime.Object) (dynamic.Interface, *kubefake.Clientset)`. Kube objects go to the typed fake; `*unstructured.Unstructured` goes to the dynamic fake; fake discovery serves every `kinds.Upstream()` kind at its first version.
  - `testutil.ServedUpstream() []kinds.Served`.
  - The fixture builders listed in Step 1.

- [ ] **Step 1: Write the test fixtures** (shared by every later backend test)

`internal/testutil/fixtures.go`:

```go
// Package testutil builds fixture objects and fake clients for backend tests.
package testutil

import (
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

// TrafficPolicy builds a kgateway TrafficPolicy with status.ancestors.
func TrafficPolicy(ns, name string, ancestors ...PolicyAncestor) *unstructured.Unstructured {
	u := obj("gateway.kgateway.dev/v1alpha1", "TrafficPolicy", ns, name)
	as := []any{}
	for _, a := range ancestors {
		as = append(as, map[string]any{"ancestorRef": refMap(a.Ref), "controllerName": a.Controller, "conditions": conds(a.Conds)})
	}
	u.Object["spec"] = map[string]any{}
	u.Object["status"] = map[string]any{"ancestors": as}
	return u
}

// ControllerDeployment builds a kgateway controller Deployment with the upstream labels.
func ControllerDeployment(ns, name, image string, desired, ready int32, env ...corev1.EnvVar) *appsv1.Deployment {
	lbls := map[string]string{"kgateway": "kgateway", "app.kubernetes.io/component": "controller",
		"app.kubernetes.io/name": "kgateway", "app.kubernetes.io/instance": name, "app.kubernetes.io/version": "2.4.7"}
	sel := map[string]string{"kgateway": "kgateway", "app.kubernetes.io/name": "kgateway", "app.kubernetes.io/instance": name}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: lbls, UID: types.UID("deploy/" + ns + "/" + name), CreationTimestamp: Old},
		Spec: appsv1.DeploymentSpec{
			Replicas: &desired,
			Selector: &metav1.LabelSelector{MatchLabels: sel},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: sel}, Spec: corev1.PodSpec{Containers: []corev1.Container{{
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
		ObjectMeta: metav1.ObjectMeta{Namespace: d.Namespace, Name: name, Labels: d.Spec.Template.Labels, UID: types.UID("pod/" + d.Namespace + "/" + name)},
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
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
)

var controllerSel = labels.SelectorFromSet(labels.Set{"kgateway": "kgateway", "app.kubernetes.io/component": "controller"})

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
		PodSelectors: []labels.Selector{controllerSel}, DeploymentSelectors: []labels.Selector{controllerSel}})
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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	toolscache "k8s.io/client-go/tools/cache"

	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
)

// Access reports whether a kind could be listed.
type Access struct {
	OK  bool   `json:"ok"`
	Err string `json:"error,omitempty"`
}

// Snapshot is an immutable view of the cache at TakenAt. Callers must not mutate it.
type Snapshot struct {
	Objects     map[schema.GroupVersionResource][]*unstructured.Unstructured
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

// Cache is a set of informers whose contents are read through Snapshot.
type Cache struct {
	mu      sync.Mutex
	errs    map[toolscache.SharedIndexInformer]string
	dyn     map[schema.GroupVersionResource]toolscache.SharedIndexInformer
	ns      toolscache.SharedIndexInformer
	pods    []toolscache.SharedIndexInformer
	deploys []toolscache.SharedIndexInformer
	now     func() time.Time
}

// New creates the informers. Nothing runs until Start.
func New(cfg Config) *Cache {
	c := &Cache{errs: map[toolscache.SharedIndexInformer]string{}, dyn: map[schema.GroupVersionResource]toolscache.SharedIndexInformer{}, now: time.Now}
	df := dynamicinformer.NewDynamicSharedInformerFactory(cfg.Dynamic, 0)
	for _, s := range cfg.Served {
		inf := df.ForResource(s.GVR()).Informer()
		c.trackErrors(inf)
		c.dyn[s.GVR()] = inf
	}
	c.ns = informers.NewSharedInformerFactory(cfg.Kube, 0).Core().V1().Namespaces().Informer()
	c.trackErrors(c.ns)
	for _, sel := range cfg.PodSelectors {
		inf := selected(cfg.Kube, sel).Core().V1().Pods().Informer()
		c.trackErrors(inf)
		c.pods = append(c.pods, inf)
	}
	for _, sel := range cfg.DeploymentSelectors {
		inf := selected(cfg.Kube, sel).Apps().V1().Deployments().Informer()
		c.trackErrors(inf)
		c.deploys = append(c.deploys, inf)
	}
	return c
}

func selected(kube kubernetes.Interface, sel labels.Selector) informers.SharedInformerFactory {
	return informers.NewSharedInformerFactoryWithOptions(kube, 0, informers.WithTweakListOptions(func(o *metav1.ListOptions) {
		o.LabelSelector = sel.String()
	}))
}

// trackErrors records the last list/watch error of inf (e.g. Forbidden) so the
// snapshot can report the kind as inaccessible instead of silently empty.
func (c *Cache) trackErrors(inf toolscache.SharedIndexInformer) {
	_ = inf.SetWatchErrorHandler(func(_ *toolscache.Reflector, err error) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.errs[inf] = err.Error()
	})
}

func (c *Cache) all() []toolscache.SharedIndexInformer {
	out := []toolscache.SharedIndexInformer{c.ns}
	for _, i := range c.dyn {
		out = append(out, i)
	}
	return append(append(out, c.pods...), c.deploys...)
}

// Start runs all informers until ctx is cancelled.
func (c *Cache) Start(ctx context.Context) {
	for _, inf := range c.all() {
		go inf.Run(ctx.Done())
	}
}

func (c *Cache) access(inf toolscache.SharedIndexInformer) Access {
	c.mu.Lock()
	defer c.mu.Unlock()
	if inf.HasSynced() {
		delete(c.errs, inf) // a later successful list clears an earlier error
		return Access{OK: true}
	}
	if e, ok := c.errs[inf]; ok {
		return Access{Err: e}
	}
	return Access{}
}

// Snapshot copies the current cache contents.
func (c *Cache) Snapshot() *Snapshot {
	s := &Snapshot{Objects: map[schema.GroupVersionResource][]*unstructured.Unstructured{}, Access: map[schema.GroupVersionResource]Access{}, Synced: true, TakenAt: c.now()}
	settled := func(a Access) bool { return a.OK || a.Err != "" }
	for gvr, inf := range c.dyn {
		a := c.access(inf)
		s.Access[gvr] = a
		s.Synced = s.Synced && settled(a)
		for _, o := range inf.GetStore().List() {
			s.Objects[gvr] = append(s.Objects[gvr], o.(*unstructured.Unstructured))
		}
		sort.Slice(s.Objects[gvr], func(i, j int) bool { return key(s.Objects[gvr][i]) < key(s.Objects[gvr][j]) })
	}
	s.Synced = s.Synced && settled(c.access(c.ns))
	for _, o := range c.ns.GetStore().List() {
		s.Namespaces = append(s.Namespaces, o.(*corev1.Namespace))
	}
	seen := map[types.UID]bool{}
	for _, inf := range c.pods {
		s.Synced = s.Synced && settled(c.access(inf))
		for _, o := range inf.GetStore().List() {
			if p := o.(*corev1.Pod); !seen[p.UID] {
				seen[p.UID] = true
				s.Pods = append(s.Pods, p)
			}
		}
	}
	for _, inf := range c.deploys {
		s.Synced = s.Synced && settled(c.access(inf))
		for _, o := range inf.GetStore().List() {
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
Expected: PASS. If `SetWatchErrorHandler` has a different signature in client-go v0.36 (a context-aware variant), use `SetWatchErrorHandlerWithContext(func(_ context.Context, _ *toolscache.Reflector, err error) {...})`; the behaviour stays the same.

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
  - `install.Detector` (interface: `Name() string; ControllerSelector() labels.Selector; ProxySelector() labels.Selector; Detect(d *appsv1.Deployment) (Install, bool)`);
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
	// ControllerSelector selects controller Deployments and Pods; the cache watches only these.
	ControllerSelector() labels.Selector
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

Run: `go test ./pkg/install/... -run 'Discover|Detector|Selector|Scope'`
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
  - `install.Owned{GatewayClasses, Gateways, ListenerSets []*unstructured.Unstructured; Routes, Policies []OwnedObject}`;
  - `install.OwnedObject{Kind kinds.Served; Obj *unstructured.Unstructured}`;
  - `install.AmbiguousGateway{Gateway objects.Ref; Installs []string}`;
  - `install.UnwatchedRoute{Install string; Route objects.Ref; Gateway objects.Ref}`;
  - `install.Assignment{PerInstall map[string]*Owned; Ambiguous []AmbiguousGateway; Unwatched []UnwatchedRoute}`;
  - `install.Assign(snap *cache.Snapshot, served []kinds.Served, insts []Resolved) Assignment`.

**Rules** (design §4.3): A Gateway belongs to install I when its class's `controllerName` ∈ I.ControllerNames and the Gateway's namespace is in I's scope. If more than one install matches, the Gateway is ambiguous and assigned to none. A ListenerSet follows its parent Gateway, but only if the ListenerSet's own namespace is in scope. A route belongs to I when one of its parentRefs resolves (directly, or through a ListenerSet) to a Gateway of I **and the route's own namespace** is in I's scope; otherwise the route is recorded as Unwatched for I. A kgateway-group policy belongs to every install whose scope contains its namespace. Any other policy (e.g. BackendTLSPolicy) belongs to I only if it is in scope and has a `status.ancestors` entry with I's controller name.

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
}

// Owned is everything assigned to one install.
type Owned struct {
	GatewayClasses []*unstructured.Unstructured
	Gateways       []*unstructured.Unstructured
	ListenerSets   []*unstructured.Unstructured
	Routes         []OwnedObject
	Policies       []OwnedObject
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

type nsName struct{ ns, name string }

// Assign assigns the snapshot's resources to insts.
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
	gwOwner := map[nsName]int{}
	for _, gw := range list(kinds.CategoryGateway) {
		var cands []int
		for _, i := range classOwners[objects.GatewayClassName(gw.Obj)] {
			if inScope(insts[i], gw.Obj.GetNamespace()) {
				cands = append(cands, i)
			}
		}
		switch len(cands) {
		case 0:
		case 1:
			gwOwner[nsName{gw.Obj.GetNamespace(), gw.Obj.GetName()}] = cands[0]
			o := a.PerInstall[insts[cands[0]].ID()]
			o.Gateways = append(o.Gateways, gw.Obj)
		default:
			amb := AmbiguousGateway{Gateway: objects.RefOf(gw.Obj)}
			for _, i := range cands {
				amb.Installs = append(amb.Installs, insts[i].ID())
			}
			a.Ambiguous = append(a.Ambiguous, amb)
		}
	}

	// ListenerSets follow their parent Gateway when they are in scope themselves.
	lsGateway := map[nsName]nsName{}
	for _, ls := range list(kinds.CategoryListenerSet) {
		p := objects.ListenerSetParent(ls.Obj)
		gk := nsName{p.Namespace, p.Name}
		i, ok := gwOwner[gk]
		if !ok || !inScope(insts[i], ls.Obj.GetNamespace()) {
			continue
		}
		lsGateway[nsName{ls.Obj.GetNamespace(), ls.Obj.GetName()}] = gk
		o := a.PerInstall[insts[i].ID()]
		o.ListenerSets = append(o.ListenerSets, ls.Obj)
	}

	// Routes: the route's own namespace decides scope (Review Focus 2).
	for _, r := range list(kinds.CategoryRoute) {
		assigned := map[int]bool{}
		for _, p := range objects.RouteParentRefs(r.Obj) {
			gk := nsName{p.Namespace, p.Name}
			switch p.Kind {
			case "Gateway":
			case "ListenerSet", "XListenerSet":
				g, ok := lsGateway[gk]
				if !ok {
					continue
				}
				gk = g
			default:
				continue
			}
			i, ok := gwOwner[gk]
			if !ok || assigned[i] {
				continue
			}
			assigned[i] = true
			if inScope(insts[i], r.Obj.GetNamespace()) {
				o := a.PerInstall[insts[i].ID()]
				o.Routes = append(o.Routes, r)
			} else {
				a.Unwatched = append(a.Unwatched, UnwatchedRoute{Install: insts[i].ID(), Route: objects.RefOf(r.Obj),
					Gateway: objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: gk.ns, Name: gk.name}})
			}
		}
	}

	// Policies.
	for _, p := range list(kinds.CategoryPolicy) {
		for _, in := range insts {
			if !inScope(in, p.Obj.GetNamespace()) {
				continue
			}
			if p.Kind.Group != kinds.KgatewayGroup && !hasAncestorFrom(p.Obj, in.ControllerNames) {
				continue
			}
			o := a.PerInstall[in.ID()]
			o.Policies = append(o.Policies, p)
		}
	}
	return a
}

func hasAncestorFrom(u *unstructured.Unstructured, controllers []string) bool {
	for _, a := range objects.PolicyAncestors(u) {
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
- Create: `pkg/ctrlmetrics/testdata/v2.4.7.txt`, `pkg/ctrlmetrics/testdata/v2.3.11.txt` (real controller `/metrics` output captured in Task 1's cluster; the captured lines for both metric families are kept verbatim, plus at least 20 other lines)
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
  - `ctrlmetrics.Result{Metrics Metrics; CollectedAt time.Time; Err string}`;
  - `ctrlmetrics.Collector`, `ctrlmetrics.NewCollector(s Scraper) *Collector`, `(*Collector).Collect(ctx, in install.Install) Result`, `(*Collector).Run(ctx, every time.Duration, installs func() []install.Install)`, `(*Collector).Get(id string) (Result, bool)`;
  - `ctrlmetrics.Source` (interface: `Get(id string) (Result, bool)`).

- [ ] **Step 1: Write the failing tests**

`pkg/ctrlmetrics/parse_test.go`:

```go
package ctrlmetrics

import (
	"os"
	"strings"
	"testing"
)

const sample = `# HELP kgateway_envoy_xds_rejects_active Number of xDS responses currently rejected by envoy proxy
# TYPE kgateway_envoy_xds_rejects_active gauge
kgateway_envoy_xds_rejects_active{gateway_name="edge",gateway_namespace="infra",type_url="type.googleapis.com/envoy.config.listener.v3.Listener"} 1
kgateway_envoy_xds_rejects_active{gateway_name="edge",gateway_namespace="infra",type_url="type.googleapis.com/envoy.config.route.v3.RouteConfiguration"} 1
kgateway_envoy_xds_rejects_active{gateway_name="ok",gateway_namespace="infra",type_url="type.googleapis.com/envoy.config.listener.v3.Listener"} 0
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

// Contract test: the metric names and labels the UI depends on exist in real
// controller output for every supported version.
func TestCapturedFixturesContainContractFamilies(t *testing.T) {
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
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go get github.com/prometheus/common@latest && go test ./pkg/ctrlmetrics/...`
Expected: FAIL (`undefined: Parse`). The fixture test also fails until the files from Task 1 are added.

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
- Consumes: `install.Resolved`, `install.Owned`, `install.Assignment`, `cache.Snapshot`, `ctrlmetrics.Result`, `objects.*`.
- Produces:
  - `health.Severity` (`SeverityError`, `SeverityWarning`, `SeverityInfo`);
  - `health.State` (`StateHealthy`, `StateWarning`, `StateError`, `StateUnknown`);
  - `health.Finding{Code, Slug string; Severity Severity; Install string; Resource *objects.Ref; Message, Detail string; Since *time.Time}`;
  - `health.CheckResult{Name string; OK, Required bool; CollectedAt time.Time; Error string}`;
  - `health.Input{Install install.Resolved; Owned *install.Owned; Assignment install.Assignment; Snapshot *cache.Snapshot; Metrics ctrlmetrics.Result; HasMetrics bool; Verify scopeverify.Result; Now time.Time}`;
  - `health.Check` (interface: `Name() string; Run(in Input) ([]Finding, CheckResult)`);
  - `health.Report{State State; Findings []Finding; Checks []CheckResult; Counts map[Severity]int; CollectedAt time.Time}`;
  - `health.Evaluate(in Input, checks []Check) Report`;
  - `health.RollUp(fs []Finding, cs []CheckResult) State`;
  - `health.Upstream() []Check`;
  - `health.Grace = 60 * time.Second`;
  - the code constants `CodeXDSRejected` … `CodeAmbiguousOwnership` (`"KGWUI-001"` … `"KGWUI-009"`).
- Produces:
  - `scopeverify.Mismatch{Namespace, Resource string; UI, Controller float64; Since time.Time}`;
  - `scopeverify.Result{Available bool; Reason string; Mismatches []Mismatch}`;
  - `scopeverify.Verifier` (interface: `Name() string; Verify(in Input) Result`);
  - `scopeverify.Input{Install install.Resolved; Snapshot *cache.Snapshot; Served []kinds.Served; Metrics ctrlmetrics.Result; HasMetrics bool; Now time.Time}`;
  - `scopeverify.NewManagedCountVerifier(persist time.Duration) *ManagedCountVerifier`.

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
	metrics *ctrlmetrics.Result // nil = not collected yet
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
		Verify: scopeverify.Result{Available: true}}
	if w.metrics != nil {
		hi.Metrics, hi.HasMetrics = *w.metrics, true
	}
	return Evaluate(hi, Upstream())
}

func has(r Report, code string, sev Severity, res string) bool {
	for _, f := range r.Findings {
		if f.Code == code && f.Severity == sev && (res == "" || (f.Resource != nil && f.Resource.String() == res)) {
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
		{"002 rule replaced", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller, Conds: []metav1.Condition{
				testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("kgateway.dev/Programmed", metav1.ConditionFalse, "RouteRuleReplaced")}})
		}, CodeConditionFalse, SeverityError, "shop/cart", StateError},
		{"002 policy target not found", func(w *world) {
			w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{
				Ref: objects.ParentRef{Group: "gateway.kgateway.dev", Kind: "StatusSummary", Name: "summary"}, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "TargetNotFound")}}))
		}, CodeConditionFalse, SeverityError, "shop/tp", StateError},
		{"002 policy partially valid", func(w *world) {
			w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "PartiallyValid")}}))
		}, CodeConditionFalse, SeverityWarning, "shop/tp", StateWarning},
		{"002 policy overridden is info", func(w *world) {
			w.objs = append(w.objs, testutil.TrafficPolicy("shop", "tp", testutil.PolicyAncestor{Ref: gw, Controller: testutil.Controller,
				Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Valid"), testutil.Cond("Attached", metav1.ConditionFalse, "Overridden")}}))
		}, CodeConditionFalse, SeverityInfo, "shop/tp", StateHealthy},
		{"003 route not processed", func(w *world) {
			w.objs[2] = testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw})
		}, CodeRouteUnattached, SeverityWarning, "shop/cart", StateWarning},
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

// Review Focus 4: stale conditions describe an older spec.
func TestStaleConditionIsPendingNotError(t *testing.T) {
	w := healthyWorld()
	gw := testutil.GwRef("infra", "edge")
	route := testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.RouteParent{Ref: gw, Controller: testutil.Controller,
		Conds: []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionFalse, "NotAllowedByListeners")}})
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
	Now        time.Time
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
}

// CheckResult records whether a check could collect its input. A Required
// check that is not OK forces the state to unknown.
type CheckResult struct {
	Name        string    `json:"name"`
	OK          bool      `json:"ok"`
	Required    bool      `json:"required"`
	CollectedAt time.Time `json:"collectedAt"`
	Error       string    `json:"error,omitempty"`
}

// Input is everything a check may look at for one install.
type Input struct {
	Install    install.Resolved
	Owned      *install.Owned
	Assignment install.Assignment
	Snapshot   *cache.Snapshot
	Metrics    ctrlmetrics.Result
	HasMetrics bool
	Verify     scopeverify.Result
	Now        time.Time
}

// Check produces findings for one concern.
type Check interface {
	Name() string
	Run(in Input) ([]Finding, CheckResult)
}

// Report is the evaluated health of one install.
type Report struct {
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
	r := Report{Counts: map[Severity]int{}, CollectedAt: in.Now, Findings: []Finding{}}
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

// cacheCheck: the snapshot is synced and every served kind is readable.
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

// conditionsCheck: KGWUI-002 and KGWUI-003.
type conditionsCheck struct{}

func (conditionsCheck) Name() string { return "resource status" }
func (c conditionsCheck) Run(in Input) ([]Finding, CheckResult) {
	var fs []Finding
	id := in.Install.ID()
	add := func(u *unstructured.Unstructured, sev Severity, cond metav1.Condition, extra string) {
		fs = append(fs, Finding{Code: CodeConditionFalse, Slug: "condition-false", Severity: sev, Install: id, Resource: ref(u),
			Message: message(cond), Detail: detail(cond, extra), Since: since(cond)})
	}
	pending := func(u *unstructured.Unstructured, why string) {
		if in.Now.Sub(u.GetCreationTimestamp().Time) < Grace {
			return
		}
		fs = append(fs, Finding{Code: CodeRouteUnattached, Slug: "route-unattached", Severity: SeverityWarning, Install: id, Resource: ref(u),
			Message: "Not yet processed by the controller", Detail: why})
	}
	// falseOrPending reports cond if False and current; a stale condition is pending (Review Focus 4).
	falseOrPending := func(u *unstructured.Unstructured, cs []metav1.Condition, types []string, extra string) {
		for _, t := range types {
			cond := objects.FindCondition(cs, t)
			if cond == nil || cond.Status == metav1.ConditionTrue {
				continue
			}
			if objects.IsStale(u, *cond) {
				pending(u, "status describes an older generation")
				return
			}
			add(u, SeverityError, *cond, extra)
		}
	}
	for _, gc := range in.Owned.GatewayClasses {
		falseOrPending(gc, objects.Conditions(gc), []string{"Accepted"}, "")
	}
	for _, gw := range in.Owned.Gateways {
		falseOrPending(gw, objects.Conditions(gw), []string{"Accepted", "Programmed"}, "")
		for _, l := range objects.GatewayListeners(gw) {
			falseOrPending(gw, l.Conditions, []string{"Accepted", "ResolvedRefs", "Programmed"}, "listener "+l.Name)
			if cf := objects.FindCondition(l.Conditions, "Conflicted"); cf != nil && cf.Status == metav1.ConditionTrue {
				add(gw, SeverityError, *cf, "listener "+l.Name)
			}
		}
	}
	for _, r := range in.Owned.Routes {
		var mine []objects.RouteParentStatus
		for _, p := range objects.RouteParents(r.Obj) {
			if slices.Contains(in.Install.ControllerNames, p.ControllerName) {
				mine = append(mine, p)
			}
		}
		if len(mine) == 0 {
			pending(r.Obj, "no status from the controller")
			continue
		}
		for _, p := range mine {
			falseOrPending(r.Obj, p.Conditions, []string{"Accepted", "ResolvedRefs", "kgateway.dev/Programmed"}, "parent "+p.ParentRef.Namespace+"/"+p.ParentRef.Name)
		}
	}
	for _, p := range in.Owned.Policies {
		for _, a := range objects.PolicyAncestors(p.Obj) {
			if !slices.Contains(in.Install.ControllerNames, a.ControllerName) {
				continue
			}
			for _, cond := range a.Conditions {
				if objects.IsStale(p.Obj, cond) {
					continue
				}
				if sev, report := policySeverity(cond); report {
					add(p.Obj, sev, cond, "ancestor "+a.AncestorRef.Kind+" "+a.AncestorRef.Name)
				}
			}
		}
	}
	return fs, ok(c.Name(), true, in.Snapshot.TakenAt)
}

// policySeverity maps a policy ancestor condition to a severity (design §5.2,
// with Overridden as info; see the plan's proposed refinement).
func policySeverity(c metav1.Condition) (Severity, bool) {
	switch c.Reason {
	case "PartiallyValid":
		return SeverityWarning, true
	case "Overridden":
		return SeverityInfo, true
	case "Pending":
		return "", false
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
- Create: `pkg/overview/overview.go`, `pkg/engine/engine.go`, `pkg/api/types.go`, `pkg/api/handlers.go`
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
  - `overview.Build(res install.Resolved, owned *install.Owned, snap *cache.Snapshot, served []kinds.Served, rep health.Report) Overview`.
- Produces:
  - `engine.Config{Snapshots cache.Snapshotter; Served []kinds.Served; Detectors []install.Detector; Scope install.ScopeProvider; Verifier scopeverify.Verifier; Metrics ctrlmetrics.Source; Checks []health.Check; Pinned string; Now func() time.Time}`;
  - `engine.InstallModel{Resolved install.Resolved; Owned *install.Owned; Overview overview.Overview; Health health.Report}`;
  - `engine.Model{Snapshot *cache.Snapshot; Served []kinds.Served; Installs []InstallModel; Pinned string; ComputedAt time.Time}`;
  - `engine.New(Config) *Engine`, `(*Engine).Compute() *Model`, `(*Engine).Current() *Model`, `(*Engine).Installs() []install.Install`, `(*Engine).Run(ctx, every time.Duration)`.
- Produces:
  - `api.ModelSource` (interface `Current() *engine.Model`);
  - `api.NewHandler(src ModelSource, ui fs.FS) http.Handler`;
  - the JSON types in `pkg/api/types.go`.

- [ ] **Step 1: Write the failing boundary tests**

`pkg/api/api_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/scopeverify"
)

const img = "cr.kgateway.dev/kgateway-dev/kgateway:v2.4.7"

type fakeMetrics map[string]ctrlmetrics.Result

func (f fakeMetrics) Get(id string) (ctrlmetrics.Result, bool) { r, ok := f[id]; return r, ok }

func okMetrics(ids ...string) fakeMetrics {
	f := fakeMetrics{}
	for _, id := range ids {
		f[id] = ctrlmetrics.Result{CollectedAt: testutil.Now, Metrics: ctrlmetrics.Metrics{Rejects: map[ctrlmetrics.GatewayKey]float64{}, Managed: map[ctrlmetrics.ManagedKey]float64{}}}
	}
	return f
}

// start runs the real cache, engine and handler over fake clients seeded with objs.
func start(t *testing.T, served []kinds.Served, pinned string, m ctrlmetrics.Source, objs ...runtime.Object) (*engine.Engine, *httptest.Server, func(*corev1.Namespace)) {
	t.Helper()
	dyn, kube := testutil.Clients(t, objs...)
	det := install.UpstreamDetector{}
	c := cache.New(cache.Config{Dynamic: dyn, Kube: kube, Served: served,
		PodSelectors: []labels.Selector{det.ControllerSelector()}, DeploymentSelectors: []labels.Selector{det.ControllerSelector(), det.ProxySelector()}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Start(ctx)
	e := engine.New(engine.Config{Snapshots: c, Served: served, Detectors: []install.Detector{det}, Scope: install.NamespaceSelectorScope{},
		Verifier: scopeverify.NewManagedCountVerifier(time.Hour), Metrics: m, Checks: health.Upstream(), Pinned: pinned, Now: func() time.Time { return testutil.Now }})
	deadline := time.Now().Add(5 * time.Second)
	for !e.Compute().Snapshot.Synced {
		if time.Now().After(deadline) {
			t.Fatal("not synced")
		}
		time.Sleep(20 * time.Millisecond)
	}
	srv := httptest.NewServer(NewHandler(e, fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}}))
	t.Cleanup(srv.Close)
	update := func(n *corev1.Namespace) {
		if _, err := kube.CoreV1().Namespaces().Update(ctx, n, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	return e, srv, update
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
func world(extra ...runtime.Object) []runtime.Object {
	d := testutil.ControllerDeployment("kgw", "kgateway", img, 1, 1, testutil.Env(install.DiscoveryNamespaceSelectorsEnv, `[{"matchLabels":{"team":"a"}}]`))
	gw := testutil.GwRef("infra", "edge")
	return append([]runtime.Object{
		testutil.Namespace("kgw", nil), testutil.Namespace("infra", map[string]string{"team": "a"}),
		testutil.Namespace("shop", map[string]string{"team": "a"}), testutil.Namespace("blog", nil),
		d, testutil.ControllerPod(d, "kgateway-0", true), testutil.ProxyDeployment("infra", "edge", 1, 1),
		testutil.GatewayClass("kgateway", testutil.Controller, true), testutil.GatewayClass("other", "example.com/other", true),
		testutil.Gateway("infra", "edge", "kgateway", nil), testutil.Gateway("infra", "foreign", "other", nil),
		testutil.HTTPRoute("shop", "cart", []objects.ParentRef{gw}, testutil.AcceptedBy(gw, testutil.Controller)),
		testutil.HTTPRoute("blog", "posts", []objects.ParentRef{gw}),
	}, extra...)
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
	o := get[OverviewResponse](t, srv, "/api/v1/installs/kgw/kgateway/overview", 200)
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
	if !hasFinding(h, health.CodeNamespaceNotWatched, "blog/posts") || h.State != health.StateWarning {
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

func TestResourceEndpoint(t *testing.T) {
	_, srv, _ := start(t, testutil.ServedUpstream(), "", okMetrics("kgw/kgateway"), world()...)
	r := get[map[string]any](t, srv, "/api/v1/installs/kgw/kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/cart", 200)
	if md, _ := r["metadata"].(map[string]any); md["managedFields"] != nil || md["name"] != "cart" {
		t.Fatalf("unexpected object: %+v", r["metadata"])
	}
	get[map[string]any](t, srv, "/api/v1/installs/kgw/kgateway/resources/gateway.networking.k8s.io/HTTPRoute/blog/posts", 404) // out of scope
	get[map[string]any](t, srv, "/api/v1/installs/kgw/kgateway/resources/-/Secret/shop/x", 404)                                // never served
	get[map[string]any](t, srv, "/api/v1/installs/kgw/kgateway/resources/gateway.networking.k8s.io/GatewayClass/-/kgateway", 200)
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
	for _, k := range served {
		if k.Category == kinds.CategoryGatewayClass {
			continue
		}
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
	Now    func() time.Time
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
}

// New returns an Engine.
func New(cfg Config) *Engine {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Engine{cfg: cfg}
}

// Compute builds a new Model and makes it current.
func (e *Engine) Compute() *Model {
	snap := e.cfg.Snapshots.Snapshot()
	now := e.cfg.Now()
	m := &Model{Snapshot: snap, Served: e.cfg.Served, Pinned: e.cfg.Pinned, ComputedAt: now}
	all := install.Resolve(install.Discover(snap, e.cfg.Detectors), e.cfg.Scope)
	// Assignment runs over all installs so ambiguity is detected even when pinned.
	asg := install.Assign(snap, e.cfg.Served, all)
	for _, res := range all {
		if e.cfg.Pinned != "" && res.ID() != e.cfg.Pinned {
			continue
		}
		met, has := e.cfg.Metrics.Get(res.ID())
		ver := e.cfg.Verifier.Verify(scopeverify.Input{Install: res, Snapshot: snap, Served: e.cfg.Served, Metrics: met, HasMetrics: has, Now: now})
		owned := asg.PerInstall[res.ID()]
		rep := health.Evaluate(health.Input{Install: res, Owned: owned, Assignment: asg, Snapshot: snap, Metrics: met, HasMetrics: has, Verify: ver, Now: now}, e.cfg.Checks)
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
	Scope           overview.ScopeSummary `json:"scope"`
	Health          HealthSummary         `json:"health"`
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

// HealthResponse is GET …/health.
type HealthResponse = health.Report

// MetaResponse is GET /api/v1/meta.
type MetaResponse struct {
	Version string         `json:"version"`
	Pinned  string         `json:"pinned,omitempty"`
	Synced  bool           `json:"synced"`
	Kinds   []kinds.Served `json:"kinds"`
}

// ErrorResponse is the body of every non-2xx API response.
type ErrorResponse struct {
	Error string `json:"error"`
}
```

`pkg/api/handlers.go`:

```go
package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/version"
)

// ModelSource provides the current model.
type ModelSource interface{ Current() *engine.Model }

// NewHandler returns the HTTP handler for the API, the probes and the SPA in ui.
func NewHandler(src ModelSource, ui fs.FS) http.Handler {
	h := &handler{src: src}
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

type handler struct{ src ModelSource }

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
	resp := MetaResponse{Version: version.Version}
	if m != nil {
		resp.Pinned, resp.Synced, resp.Kinds = m.Pinned, m.Snapshot.Synced, m.Served
	}
	writeJSON(w, resp)
}

func (h *handler) installs(w http.ResponseWriter, _ *http.Request) {
	m := h.model(w)
	if m == nil {
		return
	}
	out := []InstallSummary{}
	for _, i := range m.Installs {
		hd := i.Overview.Header
		out = append(out, InstallSummary{ID: hd.ID, Namespace: hd.Namespace, Name: hd.Name, Version: hd.Version, Edition: hd.Edition,
			ControllerNames: hd.ControllerNames, Scope: hd.Scope, Health: HealthSummary{State: i.Health.State, Counts: i.Health.Counts}})
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
		writeJSON(w, OverviewResponse{Overview: i.Overview, CollectedAt: m.ComputedAt})
	}
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	if _, i := h.find(w, r); i != nil {
		writeJSON(w, i.Health)
	}
}

// resource serves one object owned by the install. Only owned (in-scope)
// objects of served kinds are reachable, so Secrets never are. "-" stands for
// the empty group or namespace.
func (h *handler) resource(w http.ResponseWriter, r *http.Request) {
	_, i := h.find(w, r)
	if i == nil {
		return
	}
	dash := func(s string) string {
		if s == "-" {
			return ""
		}
		return s
	}
	group, kind, ns, name := dash(r.PathValue("group")), r.PathValue("kind"), dash(r.PathValue("rns")), r.PathValue("rname")
	match := func(u *unstructured.Unstructured) bool {
		return u.GetKind() == kind && u.GetNamespace() == ns && u.GetName() == name && strings.HasPrefix(u.GetAPIVersion(), group+"/")
	}
	o := i.Owned
	cands := append(append(append([]*unstructured.Unstructured{}, o.GatewayClasses...), o.Gateways...), o.ListenerSets...)
	for _, x := range o.Routes {
		cands = append(cands, x.Obj)
	}
	for _, x := range o.Policies {
		cands = append(cands, x.Obj)
	}
	for _, u := range cands {
		if match(u) {
			c := u.DeepCopy()
			unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
			writeJSON(w, c.Object)
			return
		}
	}
	writeErr(w, http.StatusNotFound, "resource not found in this install's scope")
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
  - `extension.Registry{Kinds []kinds.Descriptor; Detectors []install.Detector; Scope install.ScopeProvider; Verifier scopeverify.Verifier; Checks []health.Check}`;
  - `extension.Option` (`func(*Registry)`);
  - `extension.WithKinds(...kinds.Descriptor)`, `WithDetectors(...install.Detector)` (prepended, so downstream detectors win), `WithScopeProvider(install.ScopeProvider)`, `WithScopeVerifier(scopeverify.Verifier)`, `WithChecks(...health.Check)`;
  - `extension.New(opts ...Option) *Registry` (upstream defaults plus the options).
- Produces:
  - `app.Options{Listen, Pinned string; PollInterval, MetricsInterval time.Duration; UI fs.FS}`;
  - `app.Clients{Dynamic dynamic.Interface; Kube kubernetes.Interface; Discovery kinds.DiscoveryClient; InCluster bool}`;
  - `app.App{Engine *engine.Engine; Handler http.Handler}`;
  - `app.Build(c Clients, o Options, reg *extension.Registry) (*App, error)`, `(*App).Start(ctx context.Context)`;
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
	"testing"

	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
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
}
```

`pkg/app/app_test.go`. This is the extension-seam test (design §9): a test-only downstream registers a detector, a kind and a check, and all three show up in the API output.

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

// New returns the upstream registry with opts applied.
func New(opts ...Option) *Registry {
	r := &Registry{
		Kinds:     kinds.Upstream(),
		Detectors: []install.Detector{install.UpstreamDetector{}},
		Scope:     install.NamespaceSelectorScope{},
		// A mismatch must outlast two 10 s metric scrapes (design §4.4).
		Verifier: scopeverify.NewManagedCountVerifier(20 * time.Second),
		Checks:   health.Upstream(),
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
		podSel = append(podSel, d.ControllerSelector())
		depSel = append(depSel, d.ControllerSelector(), d.ProxySelector())
	}
	ca := cache.New(cache.Config{Dynamic: c.Dynamic, Kube: c.Kube, Served: served, PodSelectors: podSel, DeploymentSelectors: depSel})
	var scraper ctrlmetrics.Scraper = ctrlmetrics.PodProxyScraper{Kube: c.Kube}
	if c.InCluster {
		scraper = ctrlmetrics.HTTPScraper{Client: &http.Client{Timeout: 5 * time.Second}}
	}
	col := ctrlmetrics.NewCollector(scraper)
	eng := engine.New(engine.Config{Snapshots: ca, Served: served, Detectors: reg.Detectors, Scope: reg.Scope, Verifier: reg.Verifier,
		Metrics: col, Checks: reg.Checks, Pinned: o.Pinned})
	return &App{Engine: eng, Handler: api.NewHandler(eng, o.UI), cache: ca, collector: col, opts: o}, nil
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

All commands run from `web/` unless stated otherwise. The frontend follows the team conventions: one component per file with a named export, `// region Styles` / `// region Helpers` / `// region Component`, emotion styled components with a `stylesOverride` prop, and no native tooltips.

### Task 12: Frontend workspace scaffold

**Files:**
- Create: `web/package.json`, `web/tsconfig.base.json`, `web/biome.json`, `web/vitest.workspace.ts`
- Create: `web/packages/shell/package.json`, `web/packages/shell/tsconfig.json`, `web/packages/shell/src/index.ts`, `web/packages/shell/src/test/setup.ts`
- Create: `web/app/package.json`, `web/app/tsconfig.json`, `web/app/vite.config.ts`, `web/app/index.html`, `web/app/src/main.tsx`
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
  - `interface Theme { id: string; name: string; mode: "dark" | "light"; color: { bg; surface; surfaceRaised; border; text; textMuted; accent; accentText; headerBg; headerText; focus: string; status: Record<HealthState, StatusTokens> }; font: { ui: string; mono: string }; radius: { sm: string; md: string }; space: (n: number) => string }`;
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
  font: { ui: string; mono: string };
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
  body { background: ${t.color.bg}; color: ${t.color.text}; font-family: ${t.font.ui}; font-size: 14px; }
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

Add the exports to `web/packages/shell/src/index.ts`:

```ts
export type { HealthState, StatusTokens, Theme } from "./theme/tokens";
export { controlRoomDark, controlRoomLight, defaultThemes } from "./theme/themes";
export { contrastRatio } from "./theme/contrast";
export { ThemeProvider, useThemeSwitch } from "./theme/ThemeProvider";
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `npm test`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web && git commit -m "feat(web): Control room theme tokens with contrast tests"
```

---

### Task 14: Health primitives, ResourceName, API types and data hooks

**Files:**
- Create: `web/packages/shell/src/health/HealthIcon.tsx`, `web/packages/shell/src/health/HealthBadge.tsx`, `web/packages/shell/src/health/state.ts`
- Create: `web/packages/shell/src/resource/ResourceName.tsx`
- Create: `web/packages/shell/src/api/types.ts`, `web/packages/shell/src/api/client.ts`, `web/packages/shell/src/api/hooks.ts`
- Create: `web/packages/shell/src/test/render.tsx` (test helper)
- Modify: `web/packages/shell/src/index.ts`
- Test: `web/packages/shell/src/health/HealthBadge.test.tsx`, `web/packages/shell/src/resource/ResourceName.test.tsx`, `web/packages/shell/src/api/hooks.test.tsx`

**Interfaces:**
- Consumes: `Theme`, `HealthState`, `ThemeProvider`.
- Produces:
  - `<HealthIcon state size?>`, `<HealthBadge state label? stylesOverride?>`;
  - `stateLabel(state): string`, `severityToState(sev): HealthState`;
  - `<ResourceName namespace? name kind? stylesOverride?>`.
- Produces (TypeScript mirrors of the Go JSON in Task 10):
  - `Ref`, `Finding`, `CheckResult`, `HealthReport`, `InstallSummary`, `Count`, `GatewayRow`, `OverviewResponse`, `MetaResponse`, `ScopeSummary`, `Severity`.
- Produces:
  - `apiFetch<T>(path): Promise<T>` (throws `ApiError{status, message}`);
  - `useInstalls()`, `useOverview(id)`, `useHealth(id)`, `useMeta()`, `useResource(id, ref)` (SWR, refresh every 5 s except `useMeta` and `useResource`);
  - `POLL_MS = 5000`;
  - `isStale(collectedAt: string, now: number): boolean` (older than 2 × `POLL_MS`).
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
import { screen, waitFor } from "@testing-library/react";
import { renderWithTheme, stubApi } from "../test/render";
import { isStale, POLL_MS } from "./hooks";
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
import styled from "@emotion/styled";
import { AlertTriangle, Check, HelpCircle, X } from "lucide-react";
import type { HealthState } from "../theme/tokens";

// region Styles
const Chip = styled.span<{ state: HealthState; size: number }>`
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: ${(p) => p.size}px;
  height: ${(p) => p.size}px;
  border-radius: ${(p) => p.theme.radius.sm};
  background: ${(p) => p.theme.color.status[p.state].iconBg};
  color: ${(p) => p.theme.color.status[p.state].iconFg};
`;

// region Helpers
const glyphs = { healthy: Check, warning: AlertTriangle, error: X, unknown: HelpCircle } as const;

// region Component
interface HealthIconProps {
  state: HealthState;
  size?: number;
}

/** The icon half of the health vocabulary; always paired with text by callers. */
export function HealthIcon({ state, size = 16 }: HealthIconProps) {
  const Glyph = glyphs[state];
  return (
    <Chip state={state} size={size} data-testid={`health-icon-${state}`} aria-hidden="true">
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
  font-weight: 600;
  font-size: 12px;
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
  font-size: 12.5px;
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
export interface Finding {
  code: string; slug: string; severity: Severity; install: string; resource?: Ref;
  message: string; detail?: string; since?: string;
}
export interface CheckResult { name: string; ok: boolean; required: boolean; collectedAt: string; error?: string }
export interface HealthReport {
  state: HealthState; findings: Finding[]; checks: CheckResult[]; counts: Partial<Record<Severity, number>>; collectedAt: string;
}
export interface ScopeSummary { mode: "all" | "selectors" | "unknown"; selectors?: unknown[]; error?: string; watched: number; total: number }
export interface InstallSummary {
  id: string; namespace: string; name: string; version: string; edition: string; controllerNames: string[];
  scope: ScopeSummary; health: { state: HealthState; counts: Partial<Record<Severity, number>> };
}
export interface Count { group: string; kind: string; category: string; displayName: string; count: number; access: { ok: boolean; error?: string } }
export interface GatewayRow { ref: Ref; class: string; state: HealthState; listeners: number; attachedRoutes: number; addresses: string[] | null }
export interface OverviewResponse {
  header: {
    id: string; namespace: string; name: string; version: string; edition: string; controllerNames: string[];
    controller: { ready: number; desired: number }; scope: ScopeSummary;
  };
  counts: Count[]; gateways: GatewayRow[]; collectedAt: string;
}
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

const live = { refreshInterval: POLL_MS, keepPreviousData: true };

/** True when data collected at collectedAt is older than two poll intervals. */
export function isStale(collectedAt: string, now: number = Date.now()): boolean {
  return now - Date.parse(collectedAt) > 2 * POLL_MS;
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

Export all of these from `index.ts`:

```ts
export { HealthIcon } from "./health/HealthIcon";
export { HealthBadge } from "./health/HealthBadge";
export { severityToState, stateLabel } from "./health/state";
export { ResourceName } from "./resource/ResourceName";
export * from "./api/types";
export { ApiError, apiFetch } from "./api/client";
export { isStale, POLL_MS, useHealth, useInstalls, useMeta, useOverview, useResource } from "./api/hooks";
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
  - `<Shell extension={ShellExtension} pages={CorePages} />`;
  - `interface CorePages { ClusterLanding: ComponentType; InstallPage: ComponentType; FindingsPage: ComponentType; ResourcePage: ComponentType }`. Task 16–17 supply the defaults: `defaultPages`.
  - `useShellExtension(): Required<ShellExtension>` (merged with defaults);
  - `useInstallId(): string` (from `/i/:ns/:name`);
  - `<PageState kind="loading" | "connecting" | "error" | "empty" title message? action?>`;
  - `<Tooltip label>`, the app tooltip (never a native `title`).

- [ ] **Step 1: Write the failing tests**

`web/packages/shell/src/shell/Shell.test.tsx`:

```tsx
import { render, screen, waitFor } from "@testing-library/react";
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
const inst = (id: string) => ({ id, namespace: id.split("/")[0], name: id.split("/")[1], version: "2.4.7", edition: "kgateway",
  controllerNames: [], scope: { mode: "all", watched: 3, total: 3 }, health: { state: "healthy", counts: {} } });

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
import styled from "@emotion/styled";
import { type ReactNode, useId, useState } from "react";

// region Styles
const Wrap = styled.span`
  position: relative;
  display: inline-flex;
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
  font-size: 12px;
  white-space: nowrap;
  pointer-events: none;
`;

// region Component
interface TooltipProps {
  label: string;
  children: ReactNode;
}

/** App tooltip shown on hover and focus. Native title tooltips are not used. */
export function Tooltip({ label, children }: TooltipProps) {
  const [open, setOpen] = useState(false);
  const id = useId();
  return (
    <Wrap
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
import { keyframes } from "@emotion/react";
import { Copy } from "lucide-react";
import type { ReactNode } from "react";
import { HealthIcon } from "../health/HealthIcon";

// region Styles
const shimmer = keyframes`from { opacity: .45 } to { opacity: .9 }`;
const Center = styled.div`
  min-height: 60vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: ${(p) => p.theme.space(3)};
  text-align: center;
  color: ${(p) => p.theme.color.textMuted};
  h2 { color: ${(p) => p.theme.color.text}; font-size: 18px; margin: 0; }
  p { max-width: 560px; margin: 0; line-height: 1.5; }
  code { font-family: ${(p) => p.theme.font.mono}; color: ${(p) => p.theme.color.text}; }
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
`;
export const Shimmer = styled.div<{ h?: number }>`
  height: ${(p) => p.h ?? 16}px;
  border-radius: ${(p) => p.theme.radius.sm};
  background: ${(p) => p.theme.color.surfaceRaised};
  animation: ${shimmer} 0.9s ease-in-out infinite alternate;
`;

// region Component
interface PageStateProps {
  kind: "loading" | "connecting" | "error" | "empty";
  title: string;
  message?: ReactNode;
  copyText?: string;
}

/** Designed full-page loading, connecting, error and empty states. */
export function PageState({ kind, title, message, copyText }: PageStateProps) {
  return (
    <Center role={kind === "error" ? "alert" : "status"}>
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
import { useInstalls } from "../api/hooks";
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
              <HealthIcon state={i.health.state} />
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
import { useInstalls } from "../api/hooks";
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
  display: inline-flex; align-items: center; gap: 8px; color: inherit; text-decoration: none; font-weight: 700; letter-spacing: -0.01em;
  img, svg { height: 24px; width: auto; }
`;
const Chip = styled.span`
  padding: 2px 8px; border-radius: ${(p) => p.theme.radius.sm}; font-size: 12px;
  background: ${(p) => p.theme.color.accent}; color: ${(p) => p.theme.color.headerText};
`;
const Spacer = styled.div` flex: 1; `;
const Nav = styled.nav`
  display: flex; gap: ${(p) => p.theme.space(1)};
  a { display: inline-flex; align-items: center; gap: 6px; padding: 6px 10px; border-radius: ${(p) => p.theme.radius.sm};
      color: ${(p) => p.theme.color.headerText}; text-decoration: none; }
  a:hover { background: ${(p) => p.theme.color.surfaceRaised}; }
`;

// region Component
/** Header: brand, install identity and version, extension navigation, switchers. */
export function Header() {
  const { brand, navEntries } = useShellExtension();
  const match = useMatch("/i/:ns/:name/*");
  const { data } = useInstalls();
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
          <ResourceName namespace={current.namespace} name={current.name} />
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
import { useMemo } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { ThemeProvider } from "../theme/ThemeProvider";
import { defaultThemes } from "../theme/themes";
import { type CorePages, defaultBrand, ShellExtensionContext, type ShellExtension } from "./extension";
import { Header } from "./Header";
import { RootRedirect } from "./RootRedirect";

// region Component
interface ShellProps {
  extension?: ShellExtension;
  pages: CorePages;
}

/** The application shell. Upstream and downstream apps render this with their own extension. */
export function Shell({ extension = {}, pages }: ShellProps) {
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
- Consumes: `useOverview`, `useHealth`, `useInstallId`, `isStale`, `HealthBadge`, `HealthIcon`, `ResourceName`, `Tooltip`, `PageState`, `Shimmer`, `useShellExtension().findingRenderers`.
- Produces:
  - `<InstallPage />`;
  - `<FindingRow finding installId />` (used again by Task 17);
  - `resourcePath(installId: string, r: Ref): string` (exported from `FindingRow.tsx`: `/i/{id}/r/{group|-}/{kind}/{ns|-}/{name}`).

Behaviour (design §2, §5.3, §6.4–6.5; D-011):

| Data | Rendering |
|---|---|
| `health.state` = healthy | A slim strip: ✓ "All healthy", "checked 3s ago". The right column lists "What was checked" from `checks`. |
| error / warning / unknown | Strip in that state's colours: "{n} errors, {m} warnings" (unknown: "No known problems — {failing required check names} could not be checked"). Below it, the top 5 findings by severity, then "Show all ({total})" linking to `/i/{id}/findings`. |
| `collectedAt` older than 2 × `POLL_MS` | The strip turns unknown, with the text "Data is stale — last checked {relative time}". |
| finding | A row with a severity badge, `ResourceName` (or the install when there is no resource), the message, and the detail on a second muted line. Clicking the row opens the YAML view; a copy button copies `namespace/name`. A registered finding renderer (by code prefix) adds content below. |
| counts | One tile per count, in served order. A tile whose `access.ok` is false shows "No access" and the error in a tooltip. Tiles share one height and stretch to fill the row. A scope tile shows "{watched} / {total} namespaces", with the selectors in a tooltip; scope `unknown` shows a warning state. |
| gateways | A TanStack table with columns Status, Gateway, Class, Listeners, Routes, Addresses. Sort is stored in the query string (`?sort=routes.desc`). Rows link to the Gateway's YAML view in Phase 1. |
| first load | Shimmer placeholders with the final layout's dimensions (nothing shifts when data arrives). |

- [ ] **Step 1: Write the fixtures and the failing tests**

`web/packages/shell/src/test/fixtures.ts`:

```ts
import type { HealthReport, OverviewResponse } from "../api/types";

export const now = new Date().toISOString();

export const overview: OverviewResponse = {
  header: { id: "kgw/kgateway", namespace: "kgw", name: "kgateway", version: "2.4.7", edition: "kgateway",
    controllerNames: ["kgateway.dev/kgateway"], controller: { ready: 2, desired: 2 },
    scope: { mode: "selectors", selectors: [{ matchLabels: { team: "a" } }], watched: 12, total: 40 } },
  counts: [
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
  state: "healthy", findings: [], counts: {}, collectedAt: now,
  checks: [
    { name: "resource status", ok: true, required: true, collectedAt: now },
    { name: "xDS rejects", ok: true, required: true, collectedAt: now },
  ],
};

export const unknown: HealthReport = {
  state: "unknown", counts: { info: 1 }, collectedAt: now,
  findings: [{ code: "KGWUI-008", slug: "signal-unavailable", severity: "info", install: "kgw/kgateway", message: "Could not check xDS rejects", detail: "connection refused" }],
  checks: [{ name: "xDS rejects", ok: false, required: true, collectedAt: now, error: "connection refused" }],
};
```

`web/packages/shell/src/pages/install/InstallPage.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react";
import { Route, Routes } from "react-router-dom";
import { healthy, overview, unhealthy, unknown } from "../../test/fixtures";
import { renderWithTheme, stubApi } from "../../test/render";
import { InstallPage } from "./InstallPage";

function mount(health: unknown) {
  stubApi({ "/api/v1/installs/kgw/kgateway/overview": overview, "/api/v1/installs/kgw/kgateway/health": health });
  return renderWithTheme(<Routes><Route path="/i/:ns/:name" element={<InstallPage />} /></Routes>, { path: "/i/kgw/kgateway" });
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

it("stale data turns the strip unknown", async () => {
  mount({ ...healthy, collectedAt: new Date(Date.now() - 60_000).toISOString() });
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent(/Data is stale/);
  expect(within(strip).getByTestId("health-icon-unknown")).toBeInTheDocument();
});

it("count tiles, no-access tile and scope tile", async () => {
  mount(healthy);
  expect(await screen.findByText("142")).toBeInTheDocument();
  expect(screen.getByText("No access")).toBeInTheDocument();
  expect(screen.getByText("12 / 40")).toBeInTheDocument();
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
import styled from "@emotion/styled";
import { Copy } from "lucide-react";
import { Link } from "react-router-dom";
import type { Finding, Ref } from "../../api/types";
import { HealthBadge } from "../../health/HealthBadge";
import { severityToState } from "../../health/state";
import { ResourceName } from "../../resource/ResourceName";
import { useShellExtension } from "../../shell/extension";
import { Tooltip } from "../../shell/Tooltip";

// region Styles
const Row = styled.div`
  display: grid; grid-template-columns: 96px minmax(180px, 280px) 1fr auto; align-items: start; gap: ${(p) => p.theme.space(3)};
  padding: ${(p) => p.theme.space(2.5)} ${(p) => p.theme.space(3)};
  border-bottom: 1px solid ${(p) => p.theme.color.border};
  &:last-of-type { border-bottom: 0; }
  a { color: inherit; text-decoration: none; }
  a:hover [data-part="name"] { text-decoration: underline; }
  .msg { color: ${(p) => p.theme.color.text}; }
  .detail { color: ${(p) => p.theme.color.textMuted}; font-size: 12px; margin-top: 2px; }
  @media (max-width: 720px) { grid-template-columns: 1fr; }
`;
const IconButton = styled.button`
  width: 28px; height: 28px; display: inline-flex; align-items: center; justify-content: center; cursor: pointer;
  background: transparent; color: ${(p) => p.theme.color.textMuted}; border: 1px solid transparent; border-radius: ${(p) => p.theme.radius.sm};
  &:hover { border-color: ${(p) => p.theme.color.border}; color: ${(p) => p.theme.color.text}; }
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
}

/** One finding: severity, resource, message, detail; click opens the resource. */
export function FindingRow({ finding: f, installId }: FindingRowProps) {
  const { findingRenderers } = useShellExtension();
  const extra = Object.entries(findingRenderers).find(([prefix]) => f.code.startsWith(prefix))?.[1];
  const name = f.resource ? <ResourceName namespace={f.resource.namespace} name={f.resource.name} /> : <span>{f.install}</span>;
  return (
    <Row data-testid="finding-row">
      <HealthBadge state={severityToState(f.severity)} label={{ error: "Error", warning: "Warning", info: "Info" }[f.severity]} />
      <div>
        {f.resource ? <Link to={resourcePath(installId, f.resource)}>{name}</Link> : name}
        {f.resource ? <div className="detail">{f.resource.kind}</div> : null}
      </div>
      <div>
        <div className="msg">{f.message}</div>
        {f.detail ? <div className="detail">{f.detail}</div> : null}
        {extra ? extra(f) : null}
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
import { isStale } from "../../api/hooks";
import { HealthIcon } from "../../health/HealthIcon";
import type { HealthState } from "../../theme/tokens";

// region Styles
const Strip = styled.div<{ state: HealthState }>`
  display: flex; align-items: center; gap: ${(p) => p.theme.space(3)}; padding: ${(p) => p.theme.space(2.5)} ${(p) => p.theme.space(3)};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.status[p.state].stripBg}; border: 1px solid ${(p) => p.theme.color.status[p.state].stripBorder};
  b { font-weight: 600; }
  .when { margin-left: auto; color: ${(p) => p.theme.color.textMuted}; font-size: 12px; }
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
    const missing = h.checks.filter((c) => c.required && !c.ok).map((c) => c.name);
    return `No known problems — ${missing.join(", ")} could not be checked`;
  }
  const parts = [];
  if (h.counts.error) parts.push(plural(h.counts.error, "error"));
  if (h.counts.warning) parts.push(plural(h.counts.warning, "warning"));
  return parts.join(", ");
}

// region Component
/** Full-width health strip; stale data is shown as unknown (D-006). */
export function HealthStrip({ health }: { health: HealthReport }) {
  const stale = isStale(health.collectedAt);
  const state: HealthState = stale ? "unknown" : health.state;
  return (
    <Strip state={state} data-testid="health-strip" role="status">
      <HealthIcon state={state} size={18} />
      <b>{stale ? `Data is stale — last checked ${ago(health.collectedAt)}` : headline(health)}</b>
      {!stale ? <span className="when">checked {ago(health.collectedAt)}</span> : null}
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
  .n { display: block; font-size: 22px; font-weight: 600; letter-spacing: -0.02em; }
  .l { color: ${(p) => p.theme.color.textMuted}; font-size: 12px; }
  .warn { display: inline-flex; gap: 6px; align-items: center; color: ${(p) => p.theme.color.status.unknown.fg}; font-weight: 600; }
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
import { Link, useSearchParams } from "react-router-dom";
import type { GatewayRow } from "../../api/types";
import { HealthBadge } from "../../health/HealthBadge";
import { ResourceName } from "../../resource/ResourceName";
import { resourcePath } from "./FindingRow";

// region Styles
const Table = styled.table`
  width: 100%; border-collapse: collapse; background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border}; border-radius: ${(p) => p.theme.radius.md};
  th { text-align: left; font-size: 10px; letter-spacing: .06em; text-transform: uppercase; font-weight: 600;
       color: ${(p) => p.theme.color.textMuted}; padding: 8px 12px; border-bottom: 1px solid ${(p) => p.theme.color.border}; }
  th button { all: unset; cursor: pointer; }
  td { padding: 8px 12px; border-bottom: 1px solid ${(p) => p.theme.color.border}; }
  tr:last-of-type td { border-bottom: 0; }
  a { color: inherit; text-decoration: none; }
`;

// region Helpers
const col = createColumnHelper<GatewayRow>();
const rank = { error: 0, unknown: 1, warning: 2, healthy: 3 } as const;

function columns(installId: string) {
  return [
    col.accessor("state", { header: "Status", cell: (c) => <HealthBadge state={c.getValue()} />, sortingFn: (a, b) => rank[a.original.state] - rank[b.original.state] }),
    col.accessor((r) => `${r.ref.namespace}/${r.ref.name}`, { id: "gateway", header: "Gateway",
      cell: (c) => <Link to={resourcePath(installId, c.row.original.ref)}><ResourceName namespace={c.row.original.ref.namespace} name={c.row.original.ref.name} /></Link> }),
    col.accessor("class", { header: "Class" }),
    col.accessor("listeners", { header: "Listeners" }),
    col.accessor("attachedRoutes", { id: "routes", header: "Routes" }),
    col.accessor((r) => (r.addresses ?? []).join(", "), { id: "addresses", header: "Addresses", enableSorting: false }),
  ];
}

// region Component
/** Gateways of the install; sort is kept in the URL (?sort=routes.desc). */
export function GatewayTable({ rows, installId }: { rows: GatewayRow[]; installId: string }) {
  const [params, setParams] = useSearchParams();
  const [id, dir] = (params.get("sort") ?? "state.asc").split(".");
  const sorting: SortingState = [{ id: id ?? "state", desc: dir === "desc" }];
  const table = useReactTable({
    data: rows, columns: columns(installId), state: { sorting },
    onSortingChange: (u) => {
      const next = typeof u === "function" ? u(sorting) : u;
      const s = next[0];
      setParams((p) => { if (s) p.set("sort", `${s.id}.${s.desc ? "desc" : "asc"}`); return p; }, { replace: true });
    },
    getCoreRowModel: getCoreRowModel(), getSortedRowModel: getSortedRowModel(),
  });
  return (
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
  );
}
```

`InstallPage.tsx`:

```tsx
import styled from "@emotion/styled";
import { useHealth, useOverview } from "../../api/hooks";
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
  h2 { font-size: 13px; font-weight: 600; margin: ${(p) => p.theme.space(5)} 0 ${(p) => p.theme.space(2)}; }
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
  const err = ov.error ?? h.error;
  if (err && err.status === 503) return <PageState kind="connecting" title="Connecting to cluster…" message={err.message} />;
  if (err && !(ov.data && h.data)) return <PageState kind="error" title="Cannot load this install" message={err.message} copyText={err.message} />;
  if (!ov.data || !h.data) {
    return (
      <Page aria-busy="true">
        <Shimmer h={44} />
        <div style={{ height: 16 }} />
        <Shimmer h={84} />
        <div style={{ height: 16 }} />
        <Shimmer h={160} />
      </Page>
    );
  }
  const healthy = h.data.state === "healthy";
  return (
    <Page>
      <HealthStrip health={h.data} />
      {healthy ? (
        <Split>
          <div>
            <CountTiles overview={ov.data} />
            <h2>Gateways</h2>
            <GatewayTable rows={ov.data.gateways} installId={id} />
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
          <GatewayTable rows={ov.data.gateways} installId={id} />
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
- **ClusterLanding:** one card per install with health badge, `ResourceName`, version chip, "{n} errors, {m} warnings · {watched} / {total} namespaces" and the counts. The whole card is a link to `/i/{id}`. Cards share one height.
- **FindingsPage:**
  - Every finding of the install, filterable with one-click chips. The severity chips are Error, Warning and Info; the kind chips are built from the kinds that appear in the findings.
  - Free-text filtering matches the message, code or resource name.
  - All filters live in the query string (`?sev=error,warning&kind=HTTPRoute&q=cart`).
  - The list is virtualised (`@tanstack/react-virtual`), so 5,000 findings scroll smoothly.
  - When filters match nothing, a centred empty state appears.
- **ResourcePage:** `kind`, `ResourceName`, a copy button for `namespace/name`, then YAML (`js-yaml` `dump`, `status` moved before `spec` so the health-relevant part comes first) in a mono `<pre>`. A 404 shows "Not in this install's scope".

- [ ] **Step 1: Write the failing tests**

`web/packages/shell/src/pages/pages.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes } from "react-router-dom";
import { overview, unhealthy } from "../test/fixtures";
import { renderWithTheme, stubApi } from "../test/render";
import { ClusterLanding } from "./ClusterLanding";
import { FindingsPage } from "./FindingsPage";
import { ResourcePage } from "./ResourcePage";

const inst = (id: string, state: string, counts = {}) => ({ id, namespace: id.split("/")[0], name: id.split("/")[1], version: "2.4.7",
  edition: "kgateway", controllerNames: [], scope: { mode: "selectors", watched: 12, total: 40 }, health: { state, counts } });

it("cluster landing renders one card per install linking to it", async () => {
  stubApi({ "/api/v1/installs": [inst("a/one", "error", { error: 1, warning: 2 }), inst("b/two", "healthy")] });
  renderWithTheme(<ClusterLanding />);
  const card = await screen.findByRole("link", { name: /a\/one/ });
  expect(card).toHaveAttribute("href", "/i/a/one");
  expect(card).toHaveTextContent("1 error, 2 warnings");
  expect(screen.getByRole("link", { name: /b\/two/ })).toHaveTextContent("Healthy");
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
    { path: "/i/kgw/kgateway/r/gateway.networking.k8s.io/HTTPRoute/blog/posts" });
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
import { useInstalls } from "../api/hooks";
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
  .top { display: flex; align-items: center; gap: 8px; justify-content: space-between; }
  .muted { color: ${(p) => p.theme.color.textMuted}; font-size: 12px; }
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
  if (error) return <PageState kind="error" title="Cannot load installs" message={error.message} copyText={error.message} />;
  if (!data) return <PageState kind="loading" title="Loading…" />;
  return (
    <Grid>
      {data.map((i) => (
        <Card key={i.id} to={`/i/${i.id}`}>
          <div className="top">
            <ResourceName namespace={i.namespace} name={i.name} />
            <HealthBadge state={i.health.state} />
          </div>
          <span className="muted">
            v{i.version} · {summary(i.health.counts) || "No problems"} · {i.scope.mode === "all" ? "all namespaces" : `${i.scope.watched} / ${i.scope.total} namespaces`}
          </span>
        </Card>
      ))}
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
  &[aria-pressed="true"] { background: ${(p) => p.theme.color.surfaceRaised}; border-color: ${(p) => p.theme.color.textMuted}; font-weight: 600; }
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
  pre { margin: 0; padding: ${(p) => p.theme.space(4)}; overflow: auto; font-family: ${(p) => p.theme.font.mono}; font-size: 12.5px; line-height: 1.5;
        background: ${(p) => p.theme.color.surface}; border: 1px solid ${(p) => p.theme.color.border}; border-radius: ${(p) => p.theme.radius.md}; }
  button { width: 28px; height: 28px; display: inline-flex; align-items: center; justify-content: center; cursor: pointer;
           background: transparent; color: ${(p) => p.theme.color.textMuted}; border: 1px solid ${(p) => p.theme.color.border}; border-radius: ${(p) => p.theme.radius.sm}; }
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

// A test-only downstream: own brand, own theme, own page, own finding renderer (design §9).
const acmeTheme: Theme = { ...controlRoomDark, id: "acme", name: "Acme", color: { ...controlRoomDark.color, accent: "#7A1FA2" } };

it("a downstream extension contributes brand, theme, page and finding renderer", async () => {
  stubApi({
    "/api/v1/installs": [{ id: "kgw/kgateway", namespace: "kgw", name: "kgateway", version: "9.9.9", edition: "acme", controllerNames: [],
      scope: { mode: "all", watched: 1, total: 1 }, health: { state: "error", counts: { error: 1 } } }],
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
httpRoute:
  enabled: false     # expose the UI through an existing Gateway; put authentication in front (see README)
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
kgateway UI is installed (read-only; it has no login of its own).

Open it with:
  kubectl -n {{ .Release.Namespace }} port-forward svc/{{ include "kgateway-ui.fullname" . }} 8080:80
  open http://localhost:8080
{{- if .Values.httpRoute.enabled }}

It is also exposed through an HTTPRoute. Put authentication in front of it,
for example a kgateway TrafficPolicy with OAuth2/OIDC on that route.
{{- end }}

Controller metrics are read directly from the controller pods on port 9092.
If a NetworkPolicy blocks that, the UI reports "signal unavailable" instead of green.
```

`README.md` (chart): the values table, the RBAC rationale (read-only; no Secrets; Pods and Deployments are read but only watched through label selectors), the NetworkPolicy note, and the authentication guidance for `httpRoute`.

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

Scenario (the fixture names are used in the assertions below):

| Object | Namespace (labels) | Expected in UI |
|---|---|---|
| Install A: kgateway, `discoveryNamespaceSelectors=[{matchLabels:{team:a}}]` | `kgw-a` | install `kgw-a/kgw-a` |
| Gateway `edge` (class `kgateway`) | `infra` (team=a) | Gateway count 1; row Healthy |
| HTTPRoute `good` → httpbin Service | `shop` (team=a) | counted, no finding |
| HTTPRoute `broken` → Service `missing` | `shop` | KGWUI-002 "Backend not found" |
| HTTPRoute `posts` → `infra/edge` | `blog` (no label) | not counted; KGWUI-004 |
| TrafficPolicy `tp-missing-target` targeting HTTPRoute `nope` | `shop` | KGWUI-002 "Policy target not found" |
| (second scenario) Install B: selector `team=b`, Gateway `edge` in `app-b` (team=b) | `kgw-b` | second install; cluster landing shows 2 cards |

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
helm install kgw-a oci://cr.kgateway.dev/kgateway-dev/charts/kgateway --version "$KGW_VERSION" -n kgw-a \
  --set-json 'discoveryNamespaceSelectors=[{"matchLabels":{"team":"a"}}]' --wait
kubectl apply -f test/e2e/fixtures/base.yaml
if [[ "${SECOND_INSTALL:-}" == 1 ]]; then
  helm install kgw-b oci://cr.kgateway.dev/kgateway-dev/charts/kgateway --version "$KGW_VERSION" -n kgw-b --create-namespace \
    --set-json 'discoveryNamespaceSelectors=[{"matchLabels":{"team":"b"}}]' --wait
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
	get(t, "/api/v1/installs/kgw-a/kgw-a/overview", &o)
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
		get(t, "/api/v1/installs/kgw-a/kgw-a/health", &h)
		ok := has(h, health.CodeConditionFalse, "shop/broken") && has(h, health.CodeNamespaceNotWatched, "blog/posts") &&
			has(h, health.CodeConditionFalse, "shop/tp-missing-target")
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
      await expect(page.getByRole("link", { name: /kgw-a\/kgw-a/ })).toBeVisible();
      await page.getByRole("link", { name: /kgw-a\/kgw-a/ }).click();
    }
    await expect(page).toHaveURL(/\/i\/kgw-a\/kgw-a$/);
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
```

Add a non-blocking job for kgateway `main` builds once that version is installable from a chart (track it in Carry-over; not required to close Phase 1).

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
2. **Install.** The Helm OCI command and the port-forward.
3. **Run locally.** `make web build && ./bin/kgateway-ui --kubeconfig …`.
4. **What it reads.** Read-only RBAC; no Secrets; controller metrics on port 9092.
5. **Scope.** It honours `discoveryNamespaceSelectors` and shows "N / M namespaces".
6. **Extending.** A `pkg/extension` options example and a shell `ShellExtension` example, using a neutral example name (`acme`).
7. **Supported versions.**
8. **Contributing.** The SDLC docs live in `docs/`.
9. **Licence.** Apache-2.0.

The README must not contain any of the forbidden terms.

- [ ] **Step 2: Draft the upstream proposal (do not file it)**

Write the issue text for `kgateway-dev/kgateway`: "Expose the controller's effective discovery scope through a read-only endpoint" (D-009 §7).
- **Problem:** external tools must mirror `discoveryNamespaceSelectors`.
- **Proposal:** a read-only endpoint that does not expose pprof or `/logging`. It returns controller identity, version, `controllerName`s, the parsed selectors, the selected namespaces, and the enabled kinds.
- **Alternatives:** admin `/snapshots/krt` (unstable, includes ConfigMap data); the metrics.
- **Open questions:** which port; how to authenticate.

Run the `sanitize-kgateway-oss-resources` skill on it, put the AI-assisted note at the top, and **hand it to the user to file**. Never post it.

- [ ] **Step 3: Close sub-phase 1.3**

Write `docs/phases/phase-1/phase-1.3-complete.md` and update `docs/phase-status.md`. Then request the Phase 1 cross-model review (SDLC "review phase 1"). Closing Phase 1 is a separate step, gated by the exit gates.

- [ ] **Step 4: Commit**

```bash
git add README.md docs && git commit -m "docs: README; close sub-phase 1.3"
```
