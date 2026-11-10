# Sub-phase 6.1 — Cache access for kgwctl

Kind: planned
Base: 5704f91
Issues: #18, #19 | Plan steps: implementation-plan.md, Sub-phase 6.1, Tasks 19–21

## Problem / goal
kgwctl runs the cache once per command (edge E2). Two gaps make it unusable for some users (design §12):
- A kind is usable only once a WATCH succeeds after the LIST (`pkg/cache/cache.go:86`). A user with `list` but not `watch` gets nothing, though `kubectl get` works (#18).
- Every input is cluster-wide (`cache.go:246-261`, `:344-349`). A user with namespace-scoped RBAC gets `denied` for every kind (#19).

6.1 adds `cache.ListOnce` and `Config.Namespaces`, both additive. Snellius itself keeps its cluster-wide informers and sees no change.

## Expected behaviour

| Input / action | Expected |
|---|---|
| `ListOnce` over the fixture world | same `Objects`, Services, EndpointSlices, proxy Pods and fingerprint as a synced informer cache; `Mode` "list" (informer cache: "watch") |
| `ListOnce`, one kind's LIST returns 403 | that kind `denied`, `Synced` true, no error returned |
| `ListOnce`, one kind's LIST returns 500 | that kind `failed`, `Synced` false, no error returned |
| `ListOnce` at any time | no WATCH request is made |
| `ListOnce` with a cancelled context or a nil dynamic client | an error |
| `Config.Namespaces` nil or empty | today's snapshot, byte-equal fingerprint, `NamespaceAccess` nil |
| `Config.Namespaces = [a, b]` | namespaced objects only from a and b; GatewayClass and Namespace still cluster-wide; `RequestedNamespaces` = [a, b] |
| Services denied in b only | `Access[services]` denied (aggregate), `NamespaceAccess[services]` = {a: ok, b: denied}; fingerprint differs from all-ok; a Service backend in b gets KGWUI-023 "not available", never healthy |
| Scoped, class-only installs on, controller not seen | install scope source "controller not observed" (unscoped: "assumed") |
| `Snapshot.Namespaces` (`[]*corev1.Namespace`), `Snapshot.Source`, `Snapshot.Access` | unchanged type and meaning |

## Root cause (if known)
See the problem. The informer path is the only path, and it is cluster-wide.

## Acceptance tests
Tasks 19 and 20 test names in the plan: `TestListOnceMatchesCache`, `TestListOnceAccess`, `TestListOnceErrors`, `TestListOnceTakenAt`, `TestNamespacesInformerPerNamespace`, `TestNamespaceAccessAggregate`, `TestNamespacesNilUnchanged`, `TestDeniedNamespaceIsUnknown`, `TestClassOnlyNotObservedWhenScoped`; e2e `TestListOnceLive` (independent dynamic LIST per kind).

## Scope
In: `pkg/cache`, `pkg/install/scope.go`, `pkg/engine` (tests and the scope source), `internal/boundary/imports_test.go`, README "Library use", `docs/decisions.md` (D-070 … D-077).
Out: any change to how Snellius itself starts its cache; the scoped EndpointSlice reader (6.3).

## Plan / design impact
none

## Definition of done
- [ ] acceptance tests fail before, pass after (report both)
- [ ] full test suite, lint and bench pass; e2e matrix passes
- [ ] phase-6.1-complete.md written; phase-status.md updated
- [ ] tag proposal and kgwctl comments queued for the user (not posted)
