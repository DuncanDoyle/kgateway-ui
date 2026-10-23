import type { LinkedRef } from "../api/types";
import { resourcePath } from "../pages/install/FindingRow";
import { installPath } from "../shell/paths";

// region Helpers
const GATEWAY_API = "gateway.networking.k8s.io";

/**
 * Where r opens, from its server-computed link only (design §7.1): detail → its detail page, yaml → the YAML view,
 * none → undefined. Detail kinds are the Gateway, routes and the ListenerSet kinds, so any other detail kind is a ListenerSet.
 */
export function objectPath(installId: string, r: LinkedRef): string | undefined {
  if (r.link === "yaml") return resourcePath(installId, r);
  if (r.link !== "detail") return undefined;
  const ns = encodeURIComponent(r.namespace ?? "");
  const name = encodeURIComponent(r.name);
  if (r.group === GATEWAY_API && r.kind === "Gateway") return installPath(installId, `gateways/${ns}/${name}`);
  const page = r.kind.endsWith("Route") ? "routes" : "listenersets";
  return installPath(installId, `${page}/${encodeURIComponent(r.group)}/${encodeURIComponent(r.kind)}/${ns}/${name}`);
}
