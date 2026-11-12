# Notes for the Snellius user documentation

Topics the user-facing documentation must cover when it is written. Each entry says what to explain and why. Add an entry whenever a decision creates something an operator needs to know.

## Delegation edge cap and memory (2026-10-27, D-025)

- **What it is.** The UI computes route delegation (which child routes each parent selects) up to a cap on selection edges per install. An edge is one parent ref selecting one child route. A wildcard or label ref that matches many routes makes many edges.
- **What users see past the cap.** Parents past the cap read "too large to compute". Rows already computed keep their outcomes with "Some paths not computed". Checks that report something missing (a missing child status entry, the computed KGWUI-011) are not raised anywhere in the install. View A and the controller's own status are unaffected.
- **Caps are shared.** The caps count the whole install, routes the viewer cannot see included. So a large hidden part can make a visible route read "too large to compute".
- **Depth.** A delegation path deeper than 100 routes is also "too large to compute". This bound is fixed.
- **Inherited headers.** A child that inherits its parent's matcher copies every header and query match above it. Very deep inherit chains with many headers therefore reach the matcher cap sooner and read "too large to compute" earlier.
- **Known limits.** Very long inherited path segments across deep chains, and very long header values in failure reasons, can use a lot of memory; tracked for a later phase.
- **How to raise it.** The cap is configurable (binary flag and chart value; see the chart README). Raising it lets the UI compute denser delegation, but memory grows with it: during a recompute the old and new models are both in memory. Users who raise the cap must also raise the pod's memory limit. The binary sets its Go memory target to 90 % of the pod limit unless `GOMEMLIMIT` is set, so the two move together.
- **Sizing guidance.** Give measured numbers (peak RSS per cap value on the dense benchmark, `docs/phases/phase-2/bench/`) rather than a formula. Measured on the densest benchmark (100 wildcard parents × 1,000 routes, no `GOMEMLIMIT`; `bench/2.4-dense-memory.txt`): the default 10,000 peaks at about 425–465 MiB RSS, inside the default 512Mi limit; 20,000 at about 510–565 MiB; 50,000 at about 700–760 MiB. Less dense installs use less at the same cap.

## Scalability: what the UI watches and holds (2026-11-06, D-031)

- **Cluster-wide watches.** Services and EndpointSlices are watched across the cluster, not only the ones routes reference. A Kubernetes watch can filter by namespace, label or field, never by "referenced by a route"; watching per Service would put far more load on the API server.
- **Cut down on arrival.** Each object is reduced to about 200 bytes at the informer (Service: name, type, ports; EndpointSlice: ports and ready counts). At 5,000 Services and 6,000 EndpointSlices this is about 2 MiB. Checks look only at Services a route references.
- **Turning it off.** The `diagnostics.endpoints` chart value turns both watches off; KGWUI-023 then reads "not available".
- **Sizing guidance.** Give measured numbers per scale tier from the Phase 3 bench records, not a formula.

## Other Gateway API controllers: class-only installs (2026-11-09, Phase 5 design §5)

- **What it is.** With `installs.classOnly` on (flag `--class-only-installs`), each GatewayClass `controllerName` that no pack recognises becomes an install, read from Gateway API status alone. Off by default, so a kgateway user sees no new installs.
- **What users see.** The install is named "Gateway API controller `<name>`". Its ID escapes each byte outside `A-Z a-z 0-9 . -` as `~` and two hex digits, which shows in URLs. The scope reads "assumed: all namespaces". There is no version, edition, computed badge, Delegation tab, data plane section or Processes block; the Gateway page says "Not available for this controller".
- **What it does not claim.** Snellius is built for kgateway. A class-only install shows what the controller wrote to status, judged by the core checks only. Explain which finding codes can appear (the core ones, design §6) and which never do.
- **Assumed scope.** A route outside the controller's real discovery scope shows as not accepted (KGWUI-003). Say this is what the user sees from the cluster, and that Snellius cannot read the controller's settings.
- **The core-only build.** `cmd/snellius-core` (`make build-core`, `make image-core`) is the core with no pack and class-only installs always on. It is for testing the core's independence, not a supported product build; say so if it is documented at all.

## Traffic: the path list (2026-11-12, Phase 6 design §4–§5)

- **What a path is.** One row per Gateway, listener, route (and delegated routes), rule and backendRef. Hostnames are a list on the row. A rule with no backend shows its written action (redirect, direct response, no backend); a delegation ref shows "delegate".
- **Where a problem comes from.** The worst existing signal along the whole chain: Gateway, listener, route status, delegation, backend findings, endpoints. Phase 6 adds no checks. Explain "unknown" (a check it depends on is unavailable, or a finding is still held) and why it is never shown as healthy.
- **Share.** Only within one rule, as Gateway API defines weights; never a share of Gateway traffic. Weight 0 is "no traffic". No share on rules that delegate, redirect or have no backend.
- **Counts and visibility.** The summary counts only paths the viewer may see, and says when some are hidden. A class-only install has no paths; the page says why.
- **Known over-report.** A delegated path can show an error that belongs to another rule of its root route. It never hides one.

## Backends and endpoints (2026-11-12, Phase 6 design §8–§9)

- **What is listed.** Every kgateway Backend, and every Service or other backend a route or GatewayExtension names. Unreferenced Services are not listed.
- **Endpoints.** The list counts ready endpoints per backend port from the cached facts, as KGWUI-023 does. Backend detail reads that one Service's EndpointSlices on request (at most about 1,000 endpoints; more reads "partial"). Only a complete read establishes zero. Explain "partial", "timed out", "busy, retry" and "not applicable" (ExternalName, Backend resources).
- **Port chain.** backendRef port → Service port → targetPort → slice ports. Named targetPorts are not resolved to container ports; explain why (no Pod watch).
- **Reference explanations.** When a ref does not resolve, the page shows the controller's reason and the fix (for `RefNotPermitted`, the ReferenceGrant). When the controller's status does not say which ref failed, every candidate says so; Snellius does not guess.
- **RBAC.** The live read uses the EndpointSlice `list` the `diagnostics.endpoints` value already grants; with it off, the section shows "unavailable".

## Route trace (2026-11-12, Phase 6 design §7)

- **What it shows.** Per rule of a route: its match, filters as written, a share bar and each backend with port, weight, share, endpoints and state, for all four route kinds.
- **Wording per protocol.** "Requests fail" for HTTP and gRPC, "connections fail" for TCP and TLS. No status code is promised.
- **What it does not show yet.** The effective filter chain (later phase) and per-proxy endpoints (after GA, #35).

## Gateway map (2026-11-12, Phase 6 design §6)

- **What it is.** One Gateway's paths as columns: Gateway → listeners → routes → rules → backends. It ignores the Overview tab's listener picker.
- **Reading it.** Edge colour is the worst state of its paths; a dashed edge with an error icon means every path on it fails; a solid error edge with a hollow ring means some do. Under a state filter no edge is drawn as broken. Animation is off with reduced motion.
- **Interactions.** Hover, side panel, expand into rules, collapse delegated routes, "Show only its paths", search and chips. Expand, collapse and focus live in the URL, so a link reproduces the view.
- **Large Gateways.** More than 60 routes or backends opens on problems only, with a prompt to narrow.
- **Keyboard and accessibility.** One tab stop; arrow keys along the paths, Enter for details, Escape to close. The Table button is the accessible equivalent.
- **Export.** SVG or PNG of the whole drawn graph (filters, focus and collapse applied), with a title line and legend; not the visible viewport.
