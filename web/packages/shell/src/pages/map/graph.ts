import type { LinkedRef, PathRow, PathSignal } from "../../api/types";
import { actionCopy } from "../../copy/traffic";
import type { HealthState } from "../../theme/tokens";
import { refParam } from "../explore/detailParts";

// region Helpers
/** The map's columns, left to right (design §6.1). */
export type NodeKind = "gateway" | "listener" | "route" | "backend";
export const COLUMNS: readonly NodeKind[] = ["gateway", "listener", "route", "backend"];

/**
 * One node of the map. rows indexes the path rows through it. A backend-column node with action set is the
 * written action of rules with no backend (redirect, direct response, no backend), one per route.
 */
export interface MapNode {
  id: string;
  kind: NodeKind;
  ref?: LinkedRef;
  name: string;
  action?: Exclude<PathRow["action"], "backend" | "delegate">;
  state: HealthState;
  /** A delegated route's root route node; the route sits under it in the routes column. */
  delegatedUnder?: string;
  rows: number[];
}

/** An edge between adjacent columns; share only where every row on it shows a share (design §4.4). */
export interface MapEdge {
  id: string;
  source: string;
  target: string;
  share?: number;
  /** Every row on the edge is an error or its share is failing. */
  broken: boolean;
  rows: number[];
}

export interface MapGraph {
  nodes: MapNode[];
  edges: MapEdge[];
}

const RANK: Record<HealthState, number> = { healthy: 0, unknown: 1, warning: 2, error: 3 };
export const worse = (a: HealthState, b: HealthState) => (RANK[b] > RANK[a] ? b : a);
export const refLabel = (r: { namespace?: string; name: string }) =>
  r.namespace ? `${r.namespace}/${r.name}` : r.name;
const same = (a?: LinkedRef, b?: LinkedRef) => !!a && !!b && refParam(a) === refParam(b);

/** The node ids a row passes through. */
interface RowIds {
  gateway: string;
  listener: string;
  routes: string[];
  end?: string;
}

/** The node a signal belongs to, by its kind and ref; anything unplaced goes to the row's last node. */
function signalNode(s: PathSignal, r: PathRow, ids: RowIds): string {
  const last = ids.end ?? ids.routes[ids.routes.length - 1] ?? ids.listener;
  if (s.kind === "listener" || (s.kind === "waiting" && s.code === "listener")) return ids.listener;
  if (same(s.ref, r.backend) && ids.end) return ids.end;
  const i = r.routes.findIndex((x) => same(s.ref, x));
  if (i >= 0) return ids.routes[i] ?? last;
  if (same(s.ref, r.listenerSet)) return ids.listener;
  if (same(s.ref, r.gateway)) return ids.gateway;
  return last;
}

// region Component
/**
 * One node per distinct Gateway, listener, route and backend of the rows, in order of first appearance.
 * Delegated routes join the routes column under their root, with no edge of their own from a listener.
 * A node's state is the worst of the row signals that name it, so one broken backend leaves its routes healthy.
 * expanded is the set of expanded node ids; expansion into rules and children is sub-phase 6.6, so it is unused yet.
 */
export function buildGraph(rows: PathRow[], _expanded: ReadonlySet<string> = new Set()): MapGraph {
  const nodes = new Map<string, MapNode>();
  const edges = new Map<string, MapEdge & { shares: (number | undefined)[] }>();
  const node = (id: string, i: number, init: Omit<MapNode, "id" | "rows" | "state">) => {
    let n = nodes.get(id);
    if (!n) {
      n = { id, ...init, state: "healthy", rows: [] };
      nodes.set(id, n);
    }
    if (n.rows[n.rows.length - 1] !== i) n.rows.push(i);
    return n;
  };
  const edge = (source: string, target: string, i: number, r: PathRow, share: boolean) => {
    const id = `${source}>${target}`;
    let e = edges.get(id);
    if (!e) {
      e = { id, source, target, broken: true, rows: [], shares: [] };
      edges.set(id, e);
    }
    e.rows.push(i);
    e.broken &&= r.problem.state === "error" || r.shareState === "failing";
    if (share) e.shares.push(r.shareState === "shown" ? r.share : undefined);
  };

  const roots = new Set<string>();
  rows.forEach((r, i) => {
    const gateway = `g:${refParam(r.gateway)}`;
    node(gateway, i, { kind: "gateway", ref: r.gateway, name: refLabel(r.gateway) });
    const set = r.listenerSet ? `${refParam(r.listenerSet)}|` : "";
    const listener = `l:${set}${r.listener.name}`;
    node(listener, i, { kind: "listener", ref: r.listenerSet, name: r.listener.name });
    edge(gateway, listener, i, r, false);

    const routes = r.routes.map((x) => `r:${refParam(x)}`);
    const root = routes[0];
    routes.forEach((id, k) => {
      const n = node(id, i, { kind: "route", ref: r.routes[k], name: refLabel(r.routes[k] as LinkedRef) });
      if (k > 0) n.delegatedUnder ??= root;
    });
    if (root) {
      roots.add(root);
      edge(listener, root, i, r, false);
    }

    const tail = routes[routes.length - 1];
    let end: string | undefined;
    if (r.backend) {
      end = `b:${refParam(r.backend)}`;
      node(end, i, { kind: "backend", ref: r.backend, name: refLabel(r.backend) });
    } else if (r.action !== "backend" && r.action !== "delegate" && tail) {
      end = `a:${r.action}:${tail}`;
      node(end, i, { kind: "backend", action: r.action, name: actionCopy[r.action] });
    }
    if (tail && end) edge(tail, end, i, r, true);

    // Each signal raises the state of the node it names; a row state no signal explains goes to its last node.
    const ids: RowIds = { gateway, listener, routes, end };
    let explained: HealthState = "healthy";
    for (const s of r.problem.signals) {
      const n = nodes.get(signalNode(s, r, ids));
      if (n) n.state = worse(n.state, s.state);
      explained = worse(explained, s.state);
    }
    if (RANK[r.problem.state] > RANK[explained]) {
      const n = nodes.get(signalNode({ kind: "coverage", state: r.problem.state }, r, ids));
      if (n) n.state = worse(n.state, r.problem.state);
    }
  });

  // A route that is also attached directly is a root, not delegated.
  for (const id of roots) {
    const n = nodes.get(id);
    if (n) n.delegatedUnder = undefined;
  }
  return {
    nodes: [...nodes.values()],
    edges: [...edges.values()].map(({ shares, ...e }) => {
      const shown = shares.length > 0 && shares.every((s) => s !== undefined);
      return shown ? { ...e, share: Math.max(...(shares as number[])) } : e;
    }),
  };
}
