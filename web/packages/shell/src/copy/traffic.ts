/**
 * User-facing copy of the Traffic page (Phase 6, sub-phase 6.2).
 * The ids in comments (S599…) match the wording record in docs/phases/phase-6/phase-6.2-wording.md.
 */
import type { PathRow, PathSignal } from "../api/types";

type Noun = readonly [string, string];
const plural = (n: number, [one, many]: Noun) => `${n} ${n === 1 ? one : many}`;
const ref = (r?: { namespace?: string; name: string }) =>
  r ? (r.namespace ? `${r.namespace}/${r.name}` : r.name) : "";

// region Page and filters
export const trafficCopy = {
  title: "Traffic", // S599
  search: "Filter paths", // S600
  searchPlaceholder: "Filter by hostname, route or backend", // S601
  table: "Paths", // S602
  noun: ["path", "paths"] as Noun,
  state: "Path state", // S603
  kind: "Route kind", // S604
  mirror: "Mirrors", // S605
  mirrorOnly: "Mirrors only", // S606
  mirrorExclude: "Hide mirrors", // S607
  narrow: "Narrow to", // S608
  gateways: "Gateways", // S609
  namespaces: "Route namespaces", // S610
  gatewayChips: "Gateway", // S667
  namespaceChips: "Namespace", // S668
  gatewayEntry: "Paths through this Gateway", // S669
  routeEntry: "Paths through this route", // S763
  tags: { gateway: "Gateway", listener: "Listener", route: "Route", backend: "Backend", namespace: "Namespace" }, // S611
};

// region Columns and cells
export const pathColumnCopy = {
  gateway: "Gateway · listener", // S612
  route: "Route · rule", // S613
  match: "Match", // S614
  target: "Sends to", // S615
  share: "Share", // S616
  shareHint: "% of this rule's requests", // S617
  endpoints: "Endpoints", // S618
  why: "Why", // S619
  hostnames: "Hostnames", // S620
};

/** What a rule does when it names no backend, and a delegation (design §4.1). */
export const actionCopy: Record<Exclude<PathRow["action"], "backend">, string> = {
  redirect: "Redirect", // S621
  "direct-response": "Direct response", // S622
  none: "No backend", // S623
  delegate: "Delegates", // S624
};

export const cellCopy = {
  delegated: "Delegated", // S625
  viaListenerSet: (name: string) => `on ListenerSet ${name}`, // S626
  allRequests: "All requests", // S627
  matchUnknown: "Match unknown", // S628
  allConnections: "All connections", // S629
  anyMethod: "Any method", // S630
  more: (n: number) => `+${n} more`, // S631
  noHostnames: "Any hostname", // S632
  delegateTo: (target: string) => `Delegates to ${target}`, // S633
};

/** Share cells (design §4.4); failure wording follows the protocol. */
export const shareCopy = {
  percent: (p: number) => `${Number.isInteger(p) ? p : p.toFixed(1)} %`,
  noTraffic: "no traffic", // S634
  requestsFail: "requests fail", // S635
  connectionsFail: "connections fail", // S636
  mayFail: "may fail", // S637
  notShown: "Not shown", // S638
  notShownHint: "No share: this rule also delegates.", // S639
  mirror: (p: number) => `Mirror ${Number.isInteger(p) ? p : p.toFixed(1)} %`, // S640
};

/** Endpoint cells: a count only where the read established one, never "0 ready" for a read that failed. */
export const endpointCopy = {
  ready: (ready: number, total: number) => `${ready}/${total} ready`, // S641
  partial: "Partly read", // S642
  failed: "Read failed", // S643
  unavailable: "Not read", // S644
  stale: "Out of date", // S645
  notApplicable: "Not applicable", // S646
  unknown: "Unknown", // S647
  timedOut: "Read timed out", // S672
  busy: "Busy, try again", // S673
};

/** View C notes on delegated rows (design §4.2). */
export const noteCopy: Record<NonNullable<PathRow["note"]>, string> = {
  unknown: "Delegation outcome unknown", // S648
  unconfirmable: "Delegation cannot be confirmed", // S649
  "not-computed": "More paths not computed", // S650
};

/** One problem signal as a sentence; the code is a finding code, condition type or capability state. */
export function signalText(s: PathSignal): string {
  const on = s.ref ? ` (${s.ref.kind} ${ref(s.ref)})` : "";
  switch (s.kind) {
    case "gateway":
    case "route-status":
      return `${s.code} is False${on}`; // S651
    case "listener":
      return `Listener ${s.code} is False${on}`; // S652
    case "finding":
      return s.title ? `${s.title}${on}` : `Finding ${s.code}${on}`; // S653
    case "delegation":
      return `Delegation ${s.code}${on}`; // S654
    case "coverage":
      return `Endpoints not read: ${s.code}${on}`; // S655
    case "waiting":
      if (s.code === "missing") return `Not seen in the cluster yet${on}`; // S656
      if (s.code === "listener") return `Listener has no status yet${on}`; // S657
      if (s.code?.startsWith("KGWUI-")) return `Checking for ready endpoints${on}`; // S658
      if (s.code) return `${s.code} not reported yet${on}`; // S659
      return `Waiting for the controller${on}`; // S660
  }
}

/** A signal for a tooltip: a finding's title with its code. */
export function signalDetail(s: PathSignal): string {
  return s.kind === "finding" && s.title ? `${s.code}: ${signalText(s)}` : signalText(s);
}

// region Summary
export const summaryCopy: {
  label: string;
  paths: (n: number) => string;
  reach: (routes: number, gateways: number, backends: number) => string;
  counts: (paths: number, routes: number, gateways: number, backends: number) => string;
  hidden: string;
  endpointsNotRead: string;
  notComputedTitle: string;
  notComputed: string;
} = {
  label: "Path summary", // S666
  paths: (n: number) => plural(n, ["path", "paths"]), // S670
  reach: (routes: number, gateways: number, backends: number) =>
    [
      plural(routes, ["route", "routes"]),
      plural(gateways, ["Gateway", "Gateways"]),
      plural(backends, ["backend", "backends"]),
    ].join(" · "), // S671
  counts: (paths: number, routes: number, gateways: number, backends: number) =>
    `${summaryCopy.paths(paths)} · ${summaryCopy.reach(routes, gateways, backends)}`, // S661
  hidden: "Some paths are hidden from you.", // S662
  endpointsNotRead: "Endpoints were not read for some paths.", // S663
  notComputedTitle: "Paths not available", // S664
  notComputed: "This install has no computed views. That does not mean it has no traffic.", // S665
};
