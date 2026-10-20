# Sub-phase 1.3 — Platform-neutral contract

Kind: planned
Base: 6188284eb2abc0abb88a9d65717bf720cae9ec2c
Issues: n/a | Plan steps: implementation-plan.md Tasks 24–28

## Problem / goal
Neutralise the client-visible contracts before the first release (sub-phase 1.4) so a later non-Kubernetes kgateway can be supported without breaking API v1 or SPA URLs (D-016, design §14). Fix the false "proxy Deployment not found" warning on self-managed Gateways, which happens in Kubernetes mode today. Kubernetes behaviour stays as it is otherwise.

## Expected behaviour
As specified in design §14.3 (hedges H1–H5, H8, H11 and the acceptance table) and the per-task tests of Tasks 24–28. Key observable cases:

| Input / action | Expected |
|---|---|
| Kubernetes install `kgw/kgateway` | ID `kgw.kgateway`; `/api/v1/installs/kgw.kgateway/…` and `/i/kgw.kgateway` work, including reload and deep links |
| `--install kgw/kgateway` or `--install kgw.kgateway` | Both pin the same install; an invalid value fails at startup |
| Provider ID with `/`, `%`, or equal to `.` / `..` | Install excluded, error logged, `/readyz` 503 naming detector and ID |
| Install without scope, namespace/name, readiness not observed | API and SPA render; no scope tile, no replica text, no crash |
| `/meta` without `workloads` | No Deployment links or replica wording; `apps/Deployment` absent from kinds |
| Self-managed Gateway (class or Gateway parameters), no proxy Deployment | Info KGWUI-010; roll-up unaffected |
| Self-managed Gateway with unready proxy Deployment, `Programmed=False` or xDS reject | Same findings as a managed Gateway |
| Referenced GatewayParameters missing | KGWUI-006 as today |
| Downstream vocabulary override | Overridden strings show, others fall back |
| New core file importing `k8s.io/api/core/v1` | Import boundary test fails |

## Root cause (if known)
n/a (new work); the self-managed false warning comes from `proxyCheck` requiring a proxy Deployment (`pkg/health/checks.go:419-461`).

## Acceptance tests
Go tests at the HTTP API and health-check boundary; Vitest + Testing Library against a mocked API; Playwright evidence for Tasks 26–27 against the local kind cluster.

## Scope
In: pkg/ (api, install, health, overview, kinds, extension, engine, app, new platform and proxymode packages), cmd/kgateway-ui, web/packages/shell, design.md text that the tasks update, a README stub.
Out: Dockerfile, Helm, CI, e2e (sub-phase 1.4); the provider interface and neutral Install (design §14.4); Backend kind (Phase 2).

## Plan / design impact
D-016 accepted 2026-10-20; design §14; plan Tasks 24–28 reviewed by Codex (reviews/1.3-plan-codex.md).

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite passes (go test, vet, golangci if present; Vitest, Biome, typecheck, build)
- [ ] cross-model review of `git diff <base>..HEAD` + triage (planned: at the phase review)
- [ ] phase-1.3-complete.md written; phase-status.md updated
