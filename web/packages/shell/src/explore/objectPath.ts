import type { LinkedRef } from "../api/types";
import { resourcePath } from "../pages/install/FindingRow";
import { installPath } from "../shell/paths";

// region Helpers
const GATEWAY_API = "gateway.networking.k8s.io";
const GATEWAY_API_X = "gateway.networking.x-k8s.io";

/** The detail page of a kind, from the server's closed detail set (pkg/explore DetailKinds); undefined for any other kind. */
function detailPage(r: LinkedRef): "gateways" | "routes" | "listenersets" | undefined {
  if (r.group === GATEWAY_API && r.kind === "Gateway") return "gateways";
  if (r.group === GATEWAY_API && r.kind === "HTTPRoute") return "routes";
  if ((r.group === GATEWAY_API && r.kind === "ListenerSet") || (r.group === GATEWAY_API_X && r.kind === "XListenerSet"))
    return "listenersets";
  return undefined;
}

/** Where r opens, from its server-computed link only (design §7.1): detail → its detail page, yaml → the YAML view, none → undefined. */
export function objectPath(installId: string, r: LinkedRef): string | undefined {
  if (r.link === "yaml") return resourcePath(installId, r);
  const page = r.link === "detail" ? detailPage(r) : undefined;
  if (!page) return undefined;
  const ns = encodeURIComponent(r.namespace ?? "");
  const name = encodeURIComponent(r.name);
  if (page === "gateways") return installPath(installId, `gateways/${ns}/${name}`);
  return installPath(installId, `${page}/${encodeURIComponent(r.group)}/${encodeURIComponent(r.kind)}/${ns}/${name}`);
}
