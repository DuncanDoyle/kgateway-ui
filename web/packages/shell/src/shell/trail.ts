import { useMemo } from "react";
import { useLocation } from "react-router-dom";
import type { Ref } from "../api/types";
import { navCopy } from "../copy/explorer";

// region Types
/** One earlier page of the trail (D-035). An object path is group/kind/namespace/name, each part encoded, "-" for empty. */
export type Crumb = { kind: "list"; path: string; search: string } | { kind: "object"; path: string };

// region Helpers
/** The longest `via` value read or written; a longer one keeps its last {@link TRAIL_MAX} valid entries. */
export const VIA_MAX = 4096;
/** The most crumbs shown between the install and the current page; older ones collapse into a menu. */
export const TRAIL_MAX = 6;

const GW = "gateway.networking.k8s.io";
/** List pages that may start a trail, by path, with their crumb labels. */
const LISTS: Record<string, string> = {
  gateways: navCopy.gateways,
  routes: navCopy.routes,
  listenersets: navCopy.listenerSets,
  findings: navCopy.findings,
};
// A malformed entry keeps its text behind this marker; encoded paths never contain "?".
const TEXT = "?";
const TEXT_MAX = 120;

const GROUP = /^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$/;
const KIND = /^[A-Za-z][A-Za-z0-9]{0,62}$/;
const NAMESPACE = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
const NAME = /^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$/;

function decode(s: string): string | undefined {
  try {
    return decodeURIComponent(s);
  } catch {
    return undefined;
  }
}
const part = (s: string | undefined) => (s ? encodeURIComponent(s) : "-");
const unpart = (s: string) => (s === "-" ? "" : decode(s));

/** The crumb of an object page. */
export function objectCrumb(r: Ref): Crumb {
  return {
    kind: "object",
    path: [part(r.group), part(r.kind), part(r.namespace), part(r.name)].join("/"),
  };
}

/** The object a crumb names, or undefined for a list, malformed or invalid entry. */
export function crumbRef(c: Crumb): Ref | undefined {
  if (c.kind !== "object" || c.path.startsWith(TEXT)) return undefined;
  const segs = c.path.split("/");
  if (segs.length !== 4) return undefined;
  const [group, kind, namespace, name] = segs.map(unpart);
  if (group === undefined || kind === undefined || namespace === undefined || name === undefined) return undefined;
  if ((group && (group.length > 253 || !GROUP.test(group))) || !KIND.test(kind)) return undefined;
  if ((namespace && (namespace.length > 63 || !NAMESPACE.test(namespace))) || name.length > 253 || !NAME.test(name))
    return undefined;
  return namespace ? { group, kind, namespace, name } : { group, kind, name };
}

/** What a crumb shows: its own path only (design §17.6), or the entry's text when it is malformed. */
export function crumbLabel(c: Crumb): string {
  if (c.kind === "list") return LISTS[c.path] ?? c.path;
  if (c.path.startsWith(TEXT)) return c.path.slice(TEXT.length);
  const r = crumbRef(c);
  return r ? `${r.kind} ${r.namespace ? `${r.namespace}/` : ""}${r.name}` : c.path;
}

/** The text of a malformed entry, clipped; decoded where it can be. */
function textCrumb(raw: string): Crumb {
  const text = decode(raw) ?? raw;
  return {
    kind: "object",
    path: TEXT + (text.length > TEXT_MAX ? `${text.slice(0, TEXT_MAX)}…` : text),
  };
}

const same = (a: Crumb, b: Crumb) =>
  a.kind === b.kind && a.path === b.path && (a.kind !== "list" || b.kind !== "list" || a.search === b.search);

/** One entry: undefined drops it (another install, empty), a text crumb marks it malformed. */
function parseEntry(raw: string, installId: string, first: boolean): Crumb | undefined {
  if (!raw) return undefined;
  const q = raw.indexOf("?");
  const head = q < 0 ? raw : raw.slice(0, q);
  const segs = head.split("/");
  const install = decode(segs[0] ?? "");
  if (install === undefined || segs.length < 2) return textCrumb(raw);
  if (install !== installId) return undefined;
  if (segs.length === 2) {
    const page = segs[1] ?? "";
    const search = q < 0 ? "" : raw.slice(q + 1);
    if (!first || !Object.hasOwn(LISTS, page)) return textCrumb(raw);
    return { kind: "list", path: page, search };
  }
  if (segs.length !== 5 || q >= 0) return textCrumb(raw);
  const c: Crumb = { kind: "object", path: segs.slice(1).join("/") };
  const r = crumbRef(c);
  // Stored in the canonical form, so the same object always compares equal.
  return r ? objectCrumb(r) : textCrumb(raw);
}

/** The trail in a query string's `via`. Malformed entries stay as text; entries for another install are dropped. */
export function parseVia(search: string, installId: string): Crumb[] {
  let raw = new URLSearchParams(search).get("via") ?? "";
  const cut = raw.length > VIA_MAX;
  // Over the limit: keep the tail, without the entry the cut split.
  if (cut) {
    const at = raw.indexOf(",", raw.length - VIA_MAX);
    raw = at < 0 ? "" : raw.slice(at + 1);
  }
  const out: Crumb[] = [];
  for (const e of raw.split(",")) {
    const c = parseEntry(e, installId, out.length === 0 && !cut);
    if (c && !(cut && !crumbRef(c))) out.push(c);
  }
  return cut ? out.slice(-TRAIL_MAX) : out;
}

/** The trail after visiting current: cut back to its earlier entry, else appended. A list always starts a trail. */
export function appendVia(trail: Crumb[], current: Crumb): Crumb[] {
  if (current.kind === "list") return [current];
  const i = trail.findIndex((c) => same(c, current));
  return i >= 0 ? trail.slice(0, i + 1) : [...trail, current];
}

/** Splits a trail longer than max into the oldest entries, for the "…" menu, and the max − 1 shown after it. */
export function collapse(trail: Crumb[], max = TRAIL_MAX): { hidden: Crumb[]; shown: Crumb[] } {
  if (trail.length <= max) return { hidden: [], shown: trail };
  const cut = trail.length - (max - 1);
  return { hidden: trail.slice(0, cut), shown: trail.slice(cut) };
}

/** The `via` value of a trail, at most {@link VIA_MAX} long: the oldest entries go first. Text crumbs are never written. */
export function viaParam(trail: Crumb[], installId: string): string {
  const id = encodeURIComponent(installId);
  const entries = trail
    .filter((c) => c.kind === "list" || crumbRef(c))
    .map((c) =>
      // A query string never holds a raw ",", the entry separator; one written by hand is escaped.
      c.kind === "list" ? `${id}/${c.path}${c.search ? `?${c.search.replaceAll(",", "%2C")}` : ""}` : `${id}/${c.path}`,
    );
  let size = 0;
  let from = entries.length;
  while (from > 0) {
    size += (entries[from - 1]?.length ?? 0) + (from < entries.length ? 1 : 0);
    if (size > VIA_MAX) break;
    from--;
  }
  return entries.slice(from).join(",");
}

/** The page at a path: its install and its crumb, `pass` for a page that hands its trail on (a finding). */
export interface TrailPage {
  installId: string;
  crumb?: Crumb;
  pass?: boolean;
}

/** Reads a router pathname of an install page; undefined outside /i/:id. */
export function pageOf(pathname: string, search = ""): TrailPage | undefined {
  const segs = pathname.split("/").slice(1);
  if (segs[0] !== "i" || !segs[1]) return undefined;
  const parts = segs.slice(1).map(decode);
  if (parts.some((p) => p === undefined)) return undefined;
  const [installId = "", page, ...rest] = parts as string[];
  if (page === undefined || page === "") return { installId };
  if (rest.length === 0 && Object.hasOwn(LISTS, page)) {
    const q = new URLSearchParams(search);
    q.delete("via");
    return {
      installId,
      crumb: { kind: "list", path: page, search: q.toString() },
    };
  }
  if (page === "findings" && rest.length === 1) return { installId, pass: true };
  if (page === "gateways" && rest.length === 2)
    return {
      installId,
      crumb: objectCrumb({
        group: GW,
        kind: "Gateway",
        namespace: rest[0],
        name: rest[1] ?? "",
      }),
    };
  if ((page === "routes" || page === "listenersets" || page === "r") && rest.length === 4) {
    const [group = "", kind = "", ns = "", name = ""] = page === "r" ? rest.map((s) => (s === "-" ? "" : s)) : rest;
    return {
      installId,
      crumb: objectCrumb({ group, kind, namespace: ns, name }),
    };
  }
  return { installId };
}

/** The `via` for an in-app link to installId's pages from the page at pathname; undefined when it carries none. */
export function leavingVia(pathname: string, search: string, installId: string): string | undefined {
  const p = pageOf(pathname, search);
  if (!p || p.installId !== installId) return undefined;
  const trail = parseVia(search, installId);
  const next = p.pass ? trail : p.crumb ? appendVia(trail, p.crumb) : [];
  return viaParam(next, installId) || undefined;
}

/** The `via` that links from the current page to installId's object pages carry. */
export function useLinkVia(installId: string): string | undefined {
  const { pathname, search } = useLocation();
  return useMemo(() => leavingVia(pathname, search, installId), [pathname, search, installId]);
}

/** The list page of an object's kind, for the trail without `via`; undefined for a kind without one. */
export function kindList(r: Ref): Crumb | undefined {
  if (r.group === GW && r.kind === "Gateway") return { kind: "list", path: "gateways", search: "" };
  if (r.group === GW && r.kind === "HTTPRoute") return { kind: "list", path: "routes", search: "" };
  if (r.kind.endsWith("ListenerSet")) return { kind: "list", path: "listenersets", search: "" };
  return undefined;
}
