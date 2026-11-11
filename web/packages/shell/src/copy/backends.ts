/**
 * User-facing copy of the Backends list and backend detail (Phase 6, sub-phase 6.3).
 * The ids in comments (S674…) match the wording record in docs/phases/phase-6/phase-6.3-wording.md.
 */
import type { PathSignal } from "../api/types";

type Noun = readonly [string, string];
const plural = (n: number, [one, many]: Noun) => `${n} ${n === 1 ? one : many}`;

// region List
export const backendsCopy = {
  title: "Backends", // S674
  search: "Filter backends", // S675
  searchPlaceholder: "Filter by namespace or name", // S676
  noun: ["backend", "backends"] as Noun,
  show: "Show", // S677
  problems: "Problems", // S678
  services: "Services", // S679
  unused: "Unused", // S680
  namespaces: "Backend namespaces", // S681
  hidden: "Some backends are hidden from you.", // S682
};

export const backendColumnCopy = {
  backend: "Backend", // S683
  usedBy: "Used by", // S684
  problem: "Problem", // S685
};

/** The Used by cell: visible routes, Gateways and other consumers (design §8.1). */
export const usedByCopy = {
  /** Routes count references, not senders: a route counts although no accepted path reaches the backend. */
  uses: (routes: number, gateways: number, others: number) =>
    [
      routes ? `${plural(routes, ["route", "routes"])} ${routes === 1 ? "references" : "reference"} it` : "",
      gateways ? plural(gateways, ["Gateway", "Gateways"]) : "",
      others ? plural(others, ["other", "others"]) : "",
    ]
      .filter(Boolean)
      .join(" · "), // S752
  none: "Unused", // S687
  noneConfirmed: "0 confirmed paths", // S688
};

/** One port's endpoints in the list's cell: "80: 0/3 ready". */
export const portEndpoints = (port: number, text: string) => `Port ${port}: ${text}`; // S689

/** The endpoints cell of a backend no route writes a port for. */
export const noPortCopy = "No port referenced"; // S751

// region Status line
/**
 * The detail's status line (design §8.2): who sends traffic here, then one fixed phrase per signal.
 * It never names a status code.
 */
export const statusCopy = {
  routes: (routes: number, gateways: number) =>
    `${plural(routes, ["route", "routes"])} on ${plural(gateways, ["Gateway", "Gateways"])} ${routes === 1 ? "sends" : "send"} traffic here`, // S690
  others: (n: number) => `${plural(n, ["other object", "other objects"])} ${n === 1 ? "uses" : "use"} it`, // S691
  unused: "Nothing uses this backend", // S692
  noReady: (fail: string) => `no endpoint is ready, so ${fail}`, // S693
  notAccepted: (fail: string) => `the controller does not accept it, so ${fail}`, // S694
  missing: "it is not seen in the cluster yet", // S695
  missingPort: "a referenced port is not seen on the Service yet", // S748
  checking: "its endpoints are still being checked", // S696
  waiting: "it is waiting for the controller", // S697
  notRead: "its endpoints were not read", // S698
  pathProblem: "some paths to it have a problem", // S700
  fine: "no problem found", // S701
  stale: "its state is unknown while the data is out of date", // S702
};

/**
 * The phrase for one of the backend's own signals; fail is "requests fail" or "connections fail". cached says the
 * cache holds the Service, so a missing signal is about one of its ports.
 */
export function statusPhrase(s: PathSignal, fail: string, cached = false): string {
  switch (s.kind) {
    case "finding":
      if (s.code === "KGWUI-023") return statusCopy.noReady(fail);
      if (s.code === "KGWUI-002") return statusCopy.notAccepted(fail);
      return s.title ?? s.code ?? statusCopy.pathProblem;
    case "waiting":
      if (s.code === "missing") return cached ? statusCopy.missingPort : statusCopy.missing;
      if (s.code === "KGWUI-023") return statusCopy.checking;
      return statusCopy.waiting;
    case "coverage":
      return statusCopy.notRead;
    default:
      return statusCopy.pathProblem;
  }
}

// region Detail sections
export const exposureCopy = {
  title: "How this backend is exposed", // S703
  openTraffic: "Open in Traffic", // S704
  none: "No route sends traffic here.", // S705
  others: "Other users", // S706
  usedBy: (kind: string) => `Used by ${kind}`, // S707
  role: (role: string) => `(${role})`,
  field: "Field", // S708
  table: "Paths to this backend", // S741
};

export const endpointsCopy = {
  port: (port: number, name?: string) => (name ? `Port ${port} (${name})` : `Port ${port}`), // S709
  source: { kubernetes: "From Kubernetes" } as Record<string, string>, // S710
  read: (ago: string) => `Read ${ago}`, // S711
  cachedNote: "Counts from the cache, not a fresh read.", // S712
  retry: "Try again", // S713
  address: "Address", // S714
  ready: "Ready", // S715
  serving: "Serving", // S716
  terminating: "Terminating", // S717
  pod: "Pod", // S718
  yes: "Yes", // S719
  no: "No", // S720
  noItems: "No endpoints in this read.", // S721
  partialNote: "Some endpoints were not read.", // S722
  cached: (ready: number, total: number) => `${ready}/${total} ready, cached`, // S723
  perProxy: "Per-proxy endpoints are not shown.", // S724
  noPorts: "No port of this backend is referenced.", // S725
  table: (port: string) => `Endpoints on ${port}`, // S742
};

export const portChainCopy = {
  title: "Port chain", // S726
  refPort: "Route port", // S727
  servicePort: "Service port", // S728
  targetPort: "Target port", // S729
  slicePorts: "Slice ports", // S730
  noneObserved: "none observed", // S731
  noSuchPort: (port: number) => `The Service has no port ${port}.`, // S732
  noSlicePort: (name: string) => `No slice has a port named ${name}.`, // S733
  noSlicePortUnnamed: "No slice has a port for this Service port.", // S749
  named: (name: string) => `${name} (named, not resolved)`, // S734
};

export const explanationCopy = {
  title: "Why refs fail", // S735
  candidate: (reason: string) => `This route reports ResolvedRefs=False (${reason}) for one of these refs.`, // S736
  waiting: "Waiting for the controller.", // S737
  reason: "Reason", // S738
  at: "Written at", // S743
};

export const factCopy = {
  service: "Service facts", // S746
  facts: (kind: string) => `${kind} facts`, // S750
  type: "Type", // S747
  kind: "Kind",
  serviceType: "Service type", // S739
  ports: "Ports", // S740
};
