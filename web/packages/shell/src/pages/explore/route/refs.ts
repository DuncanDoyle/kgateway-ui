import type { LinkedRef, ParentRef, Ref } from "../../../api/types";

/** Same object: group, kind, namespace and name. */
export const sameRef = (a: Ref | undefined, b: Ref) =>
  !!a && a.group === b.group && a.kind === b.kind && (a.namespace ?? "") === (b.namespace ?? "") && a.name === b.name;

/** Same parentRef after defaults, including section and port. */
export const sameParentRef = (a: ParentRef, b: ParentRef) =>
  sameRef(a, b) && a.sectionName === b.sectionName && a.port === b.port;

/** A parentRef as a ref that never links: the spec's own words (design §7.4 rule 8). */
export const asText = (p: ParentRef): LinkedRef => ({
  group: p.group,
  kind: p.kind,
  namespace: p.namespace,
  name: p.name,
  link: "none",
});

/** The routes list's backend param: group/Kind/namespace/name, core for the empty group (design §7.2). */
export const backendParam = (r: Ref) => [r.group || "core", r.kind, r.namespace ?? "", r.name].join("/");

export const GATEWAY_API = "gateway.networking.k8s.io";
export const isRouteRef = (r: Pick<Ref, "group" | "kind">) => r.group === GATEWAY_API && r.kind === "HTTPRoute";
