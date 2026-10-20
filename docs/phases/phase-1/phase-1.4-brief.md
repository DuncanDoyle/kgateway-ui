# Sub-phase 1.4 — Packaging, CI, e2e

Kind: planned
Base: cab3faeebe0d917f553aa5ec06e2c949cbf3af37
Issues: n/a | Plan steps: implementation-plan.md Tasks 19–23 (sub-phase 1.4), design §14.3 H9 and H10

## Problem / goal
The UI builds and tests locally but can't be installed or released. Ship a container image and a Helm chart with least-privilege RBAC, CI and release workflows, and a kind e2e against kgateway 2.3 and 2.4 that exercises the 1.3 contract (opaque install IDs, `readiness`, `capabilities`, KGWUI-010). Finish with the README, the roadmap notes (H10) and a draft upstream proposal (H9, not filed).

## Expected behaviour
As specified in the per-task steps of Tasks 19–23. Key observable cases:

| Input / action | Expected |
|---|---|
| `docker buildx build --platform linux/amd64,linux/arm64 .` | Both platforms build; the image runs as UID 65532 and `--help` prints the flags |
| `helm template` with default values | Deployment, Service, ServiceAccount, ClusterRole and binding; read-only verbs only (get, list, watch); `gatewayparameters` readable; no Secret access |
| `httpRoute.enabled=true` | An HTTPRoute with the given parentRefs and hostnames |
| `install.pin=kgw/kgateway` | The container gets `--install kgw/kgateway` |
| Pull request | CI runs Go vet/test/lint, the web gates, chart tests and the image build |
| Tag push | Release workflow publishes the multi-arch image and chart |
| kind e2e, kgateway 2.3 and 2.4, two installs | `/api/v1/installs` lists both by opaque ID; overview, findings and objects work per ID; the SPA smoke test loads `/i/<id>` with reload and deep link |
| README | States the trust model and that Kubernetes is today's only provider |

## Root cause (if known)
n/a (new work).

## Acceptance tests
Go chart tests (`deploy/helm/chart_test.go`) rendering the chart; the e2e Go API tests (build tag `e2e`) and the Playwright smoke test against kind; workflow files checked with actionlint where available.

## Scope
In: Dockerfile, .dockerignore, deploy/helm, .github/workflows, .golangci.yaml, test/e2e, web/e2e, README.md, docs/roadmap.md, the upstream proposal draft under docs.
Out: product code changes beyond what packaging needs; filing the upstream proposal; the provider interface (design §14.4); Backend kind (Phase 2).

## Plan / design impact
none expected. Task 20's ClusterRole must include `gatewayparameters` (added by Task 25 in 1.3).

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite passes (go test, vet, golangci; Vitest, Biome, typecheck, build; chart tests; e2e on kind)
- [ ] cross-model review of `git diff <base>..HEAD` + triage (planned: at the phase review)
- [ ] phase-1.4-complete.md written; phase-status.md updated
