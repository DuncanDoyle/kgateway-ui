import type { LinkedRef } from "../api/types";
import { resourcePath } from "../pages/install/FindingRow";
import { installPath } from "../shell/paths";

// region Helpers
const GATEWAY_API = "gateway.networking.k8s.io";

/** Where r opens, from its server-computed link only (design §7.1): detail → its detail page, yaml → the YAML view, none → undefined. */
export function objectPath(installId: string, r: LinkedRef): string | undefined {
  if (r.link === "yaml") return resourcePath(installId, r);
  if (r.link !== "detail") return undefined;
  const ns = encodeURIComponent(r.namespace ?? "");
  const name = encodeURIComponent(r.name);
  if (r.group === GATEWAY_API && r.kind === "Gateway") return installPath(installId, `gateways/${ns}/${name}`);
  // The server sends detail only for its DetailKinds, so any kind besides Gateway and HTTPRoute is a ListenerSet kind.
  const page = r.group === GATEWAY_API && r.kind === "HTTPRoute" ? "routes" : "listenersets";
  return installPath(installId, `${page}/${encodeURIComponent(r.group)}/${encodeURIComponent(r.kind)}/${ns}/${name}`);
}
