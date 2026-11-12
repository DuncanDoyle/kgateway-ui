import type { LinkedRef, Ref } from "../api/types";
import { installPath } from "../shell/paths";

/** Path of the read-only YAML view of r. */
export function resourcePath(installId: string, r: Ref): string {
  return installPath(installId, `r/${r.group || "-"}/${r.kind}/${r.namespace || "-"}/${r.name}`);
}

/** Path of the backend detail page of r (Phase 6 design §8.2), "-" for the empty group. */
export function backendPath(installId: string, r: Ref): string {
  const seg = encodeURIComponent;
  return installPath(
    installId,
    `backends/${seg(r.group || "-")}/${seg(r.kind)}/${seg(r.namespace ?? "")}/${seg(r.name)}`,
  );
}

// region Helpers
const GATEWAY_API = "gateway.networking.k8s.io";
const ROUTE_KINDS = new Set(["HTTPRoute", "GRPCRoute", "TCPRoute", "TLSRoute"]);

/** Where r opens, from its server-computed link only (design §7.1): detail → its detail page, yaml → the YAML view, none → undefined. */
export function objectPath(installId: string, r: LinkedRef): string | undefined {
  if (r.link === "yaml") return resourcePath(installId, r);
  if (r.link !== "detail") return undefined;
  const ns = encodeURIComponent(r.namespace ?? "");
  const name = encodeURIComponent(r.name);
  if (r.group === GATEWAY_API && r.kind === "Gateway") return installPath(installId, `gateways/${ns}/${name}`);
  // The server sends detail only for its DetailKinds, so any kind besides Gateway and a route kind is a ListenerSet kind.
  const page = r.group === GATEWAY_API && ROUTE_KINDS.has(r.kind) ? "routes" : "listenersets";
  return installPath(installId, `${page}/${encodeURIComponent(r.group)}/${encodeURIComponent(r.kind)}/${ns}/${name}`);
}

/** path with `via` and params added to its query; params never replace a query the path already has. */
export function withVia(path: string, via?: string, params?: Record<string, string>): string {
  const q = new URLSearchParams(params);
  if (via) q.set("via", via);
  const s = q.toString();
  return s ? `${path}${path.includes("?") ? "&" : "?"}${s}` : path;
}

/** Where r opens (as {@link objectPath}), carrying the trail `via` (D-035) and params, e.g. a selected listener. */
export function linkTarget(installId: string, r: LinkedRef, via?: string, params?: Record<string, string>) {
  const to = objectPath(installId, r);
  return to ? withVia(to, via, params) : undefined;
}
