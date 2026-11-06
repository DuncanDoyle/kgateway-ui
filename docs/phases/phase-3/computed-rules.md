# Phase 3 — Computed rules specification

**Date:** 2026-11-05
**Rule sets:** `kgateway-2.3` (tag `v2.3.11`, `c2af2887`) and `kgateway-2.4` (tag `v2.4.7`, `b4a2bf74`); `kgateway-2.5` (tag `v2.5.0`, `f858390f`) reads 016 from status
**Design:** `design.md` §7.6. Phase 2's `docs/phases/phase-2/computed-rules.md` holds views A, B and C and its principles (§1) apply here: one rule set per kgateway minor, unknown never a guess, explain never own.

`V3` is v2.3.11, `V4` is v2.4.7 and `V5` is v2.5.0. Paths are under `pkg/` unless they start with `api/`. Choices made without the user are marked **(our call)**.

## Policy targets

The rule tells the UI when a kgateway policy's `targetRefs` or `targetSelectors` resolve to nothing, so KGWUI-016 can be computed on 2.3 and 2.4 (design §7.6). A rule set offers it through the optional `computed.PolicyTargeter` interface; a rule set without it raises no computed 016.

### `kgateway-2.5`: from status, rule off

On `kgateway-2.5` the computed rule is off and 016 comes from status (design §7.6, D-020 rule 5). `kgw25` has no `PolicyTargeter`. It implements `ReportsTargetNotFound`, which `pkg/health` checks before it reads 016 from status.

- The controller writes the ancestor `gateway.kgateway.dev/StatusSummary` with `Accepted=False` and `Attached=False`, reason `TargetNotFound`, for every policy kind (`kgateway/proxy_syncer/policy_target_status.go` V5:294-307). The `Accepted` message lists the missing refs.
- A current ancestor of that shape from this install is one 016 error, quoting `Accepted`. It replaces both conditions' 002 (D-030). The policy has an ancestor, so no 003 is raised.
- A stale ancestor is "not yet processed" (003), as for any policy ancestor.
- The controller judges targetRefs to Gateway, ListenerSet, HTTPRoute, GRPCRoute, TCPRoute, TLSRoute, Service and Backend (V5:61-124), and a missing `sectionName` on the first four. It never judges selectors (V5:243-258), so on 2.5 a selector that matches nothing raises no 016.
- `kgateway-2.3` and `kgateway-2.4` keep the computed rule and read no 016 from status: a `TargetNotFound` there stays a 002. Each install uses its own set.
- A controller newer than 2.5 gets `kgateway-2.5` with the "newer than tested" note.

### Rule set behaviour

| Behaviour | `kgateway-2.3` | `kgateway-2.4` |
|---|---|---|
| Target lookup is local to the policy's namespace (GEP-713); no ReferenceGrant | `krtcollections/policy.go` V3:864-900 | `krtcollections/policy.go` V4:873-909 |
| `targetRef` fields: group, kind, name, optional `sectionName` | `api/v1alpha1/shared/shared_types.go` V3:24-52 | V4:25-53 |
| `targetSelector` fields: group, kind, `matchLabels`, optional `sectionName` | V3:54-88 | V4:55-89 |
| Gateway and ListenerSet `sectionName` is a listener name | `krtcollections/policy.go` V3:503-509, :576-579 | V4:512-518, :585-588 |
| HTTPRoute and GRPCRoute `sectionName` is a rule name; unnamed rules are never a section | `krtcollections/policy.go` V3:1387-1389, `krtcollections/grpc_route.go` V3:50-52 | V4:1446-1448, `krtcollections/grpc_route.go` V4:58-60 |
| A selector selects an object that has every `matchLabels` pair; an empty `matchLabels` or an object without labels selects nothing | `krtcollections/policy.go` V3:830, :896; istio `labels.Instance.Match` | V4:839, :905 |

The two rule sets behave the same. Each set keeps its own copy, as phase 2's §1 rule 1 requires.

### The rule

Each target gets one result, in order: every `targetRef`, then every `targetSelector`. A selector's `Index` continues the count after the last `targetRef`.

1. **Scope unknown.** When the install's discovery scope is unknown, every result is `unknown` (D-024).
2. **Known kinds.** The rule judges `Gateway`, `ListenerSet`, `HTTPRoute` and `GRPCRoute` in `gateway.networking.k8s.io`, and `XListenerSet` in `gateway.networking.x-k8s.io`. Any other kind is `unknown`, so a Service or Backend target raises nothing **(our call)**.
3. **Served and watched.** A known kind that the API server does not serve, or that the UI does not watch, is `unknown`.
4. **targetRef.** `not-found` when no object with that name is in the policy's namespace. With a `sectionName`, `section-not-found` when the object has no listener (Gateway, ListenerSet) or no named rule (HTTPRoute, GRPCRoute) with that name. Otherwise `found`.
5. **targetSelector.** `matches-nothing` when `matchLabels` is empty. `found` when an object of its kind in the policy's namespace has every label. Otherwise it depends on the global policy namespace (below): `unknown` when the UI cannot read the setting, `found` when the policy is in that namespace and an object in any namespace has every label, and `matches-nothing` in every other case. A selector's `sectionName` is not judged: the UI does not know which objects matched, so it never calls a section missing **(our call)**.
6. **Verifiable.** A result is verifiable only when the policy has exactly one target, that target is a `targetRef` to a Gateway, HTTPRoute or GRPCRoute, and its outcome is not `unknown`. Every other shape is "not verified" (design §7.6). A ListenerSet target is not verifiable: its ancestor was not observed **(our call)**.

**The global policy namespace.** A policy in the controller's global policy namespace (`KGW_GLOBAL_POLICY_NAMESPACE`, `api/settings/settings.go` V3:293-295, V4:303-305) also selects labelled objects in every namespace (`krtcollections/policy.go` V3:909-913, V4:918-922). Its `targetRefs` stay local. The input carries the setting and whether the UI could read it (`TargetInput.GlobalPolicyNamespace`, `GlobalPolicyNamespaceKnown`); unset means no global namespace. The rule set judges it, so a minor that changes it gets a new rule set, not an input change.

### Observed ancestors (Task 60 Step 0)

Observed 2026-11-05 on kind with kgateway v2.3.11 (Gateway API v1.5.1) and v2.4.7 (Gateway API v1.6.1), using `test/e2e/fixtures/observe/policy-targets.yaml`. Every shape below is a TrafficPolicy in namespace `pt`. HTTPRoutes `r1` and `r2` attach to Gateway `gw`; `r3` attaches to `gw` and `gw2`. Both tags wrote the same ancestors for every shape.

| Shape | Policy | Ancestors written | Tied to one target |
|---|---|---|---|
| One Gateway | `one-gateway` → Gateway `gw` | Gateway `gw` | yes: the target itself |
| One Gateway listener | `gateway-listener` → `gw`, section `alt` | Gateway `gw` (no `sectionName`) | yes: the target |
| One HTTPRoute | `one-route` → `r1` | Gateway `gw` | yes: the route's Gateway |
| A route section | `route-section` → `r1`, section `rule-a` | Gateway `gw` | yes: the route's Gateway |
| One route on two Gateways | `route-two-gateways` → `r3` | Gateways `gw` and `gw2` | yes: each is a Gateway of the route |
| Two routes on one Gateway | `two-routes` → `r1`, `r2` | Gateway `gw`, once | no: shared by both targets |
| A label selector | `selector` → HTTPRoutes with `pick: "yes"` (`r1`, `r2`) | Gateway `gw`, once | no |
| One found, one missing | `one-found-one-missing` → `r1`, `nope` | Gateway `gw` (from `r1`) | no |
| Missing Gateway | `missing-gateway` → Gateway `nope` | none; `status` absent | — |
| Missing listener | `missing-listener` → `gw`, section `nope` | none; `status` absent | — |
| Missing route section | `missing-route-section` → `r1`, section `nope` | none; `status` absent | — |
| Selector that matches nothing | `selector-none` → `pick: "never"` | none; `status` absent | — |
| Target deleted after attaching | `target-deleted` → HTTPRoute `gone` on `gw2`; `gone` deleted by hand once the ancestor appears | `gw2`, then `status.ancestors: []` within 15 s | — |

What this settles:
- An ancestor is always a Gateway: the target for a Gateway target, the route's Gateways for a route target. It never names the targetRef or the section it came from.
- A policy with one Gateway or route target has ancestors that belong to that target alone. A current ancestor for such a target, when the rule calls it missing, is a disagreement (KGWUI-012, kind `policy-target`).
- With several targets or a selector, one ancestor can come from any target, so no ancestor can be tied to the missing one. These shapes are "not verified".
- A target that the controller cannot find gets no ancestor, and a deleted target loses its ancestor. Neither leaves a stale ancestor that would raise a false KGWUI-012.

Both observations agree with the verifiable shapes of design §7.6 and the plan's Task 60.
