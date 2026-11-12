# Sub-phase 6.3 — Backends

Kind: planned
Base: 8727eac
Issues: n/a | Plan steps: implementation-plan.md, Sub-phase 6.3, Tasks 33–39

## Problem / goal
An operator who sees a broken backend cannot quickly tell which routes, Gateways, listeners and hostnames expose it, what its endpoints look like, or why a reference to it fails (design §1, §8). 6.3 adds the Backends list and the backend detail page, moves Backend judgement behind the kgateway pack (Phase 5 carry-over), lists GatewayExtension consumers, reads one Service's EndpointSlices on demand, and explains unresolved references with the fix.

## Expected behaviour

| Input / action | Expected |
|---|---|
| kgateway install findings | IDs, order and KGWUI-002 evidence byte-equal to the base after the pack move |
| Class-only install with a `Backend`-shaped object | no KGWUI-002, no Backend badge; conditions only |
| Backends list | every in-scope `Backend`, every ref an admitted route writes (attached or not), every consumer backend; unreferenced Services not listed; problems first |
| Service ref whose attachment failed | listed with 0 confirmed exposures and the attachment problem |
| GatewayExtension ext-auth backend | "used by GatewayExtension X (ext-auth)" with role and field |
| Backend detail of a Service | status line in fixed phrases (no status code), exposure paths, endpoints per referenced port, port chain, reference explanations, Service facts (no YAML) |
| Detail endpoints, read complete | per address: ready, serving, terminating, Pod |
| Read over the page bound, a timeout, or the in-flight limit | "partial", "timed out", "busy, retry" — never zero endpoints |
| Endpoints capability off / no reader | cached counts, source "unavailable" |
| Named targetPort | its name shown; not resolved to a container port |
| Single-candidate `RefNotPermitted` | attributed, with the ReferenceGrant diagnose suggests |
| Several candidates | each says one of them fails; no guess |
| Status older than spec | "waiting for the controller" |
| Hidden backend (restricted viewer) | 404, zero scoped reads; hidden exposure rows and consumers absent from rows and counts |
| Existing API responses | golden-v1 byte-equal; existing goldens unchanged except new optional keys |
| Retained heap, worst world | within +48 MiB in-child, scoped-reader memo warm (headroom after 6.2: 2.7 MiB) |

## Root cause (if known)
n/a (new feature and a planned move).

## Acceptance tests
The test names in plan Tasks 33–38, at the API boundary and Playwright for the pages.

## Scope
In: `pkg/health` (backend check move), `pkg/computed` (BackendJudge), `pkg/kgateway` (consumers), `pkg/extension`, `pkg/objects` (Consumer), `pkg/cache` (TargetName, scoped reader), new `pkg/endpoints`, `pkg/api` (backends endpoints), the SPA Backends pages.
Out: route detail changes (6.4), the map (6.5, 6.6), per-proxy endpoints (#35).

## Plan / design impact
none

## Definition of done
- [ ] acceptance tests fail before, pass after
- [ ] full test suite, lint, web tests and bench pass; D-057 in-child figure recorded in bench-6.3.txt
- [ ] wording pass done (phase-6.3-wording.md)
- [ ] demo reel of the Backends pages with real data
- [ ] phase-6.3-complete.md written; phase-status.md updated
