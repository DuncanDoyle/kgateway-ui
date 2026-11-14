import type { LinkedRef, PathRow, PathSignal } from "../../api/types";
import { listenerCopy } from "../../copy/explorer";
import { mapCopy } from "../../copy/map";
import { actionCopy } from "../../copy/traffic";
import type { HealthState } from "../../theme/tokens";
import { refParam } from "../explore/detailParts";
import { matchSummary } from "../traffic/pathText";

// region Helpers
/** The map's columns, left to right (design §6.1); the rule column holds rule nodes while a route is expanded. */
export type NodeKind = "gateway" | "listener" | "route" | "rule" | "backend";
export const COLUMNS: readonly NodeKind[] = ["gateway", "listener", "route", "rule", "backend"];

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
  /** A delegated route's parent route node; the route sits under it in the routes column. */
  delegatedUnder?: string;
  /** A rule node's rule index in its route. */
  rule?: number;
  /** A route node ends rows (it has rules to open), delegates rows, is open into its rules, or is collapsed. */
  ends?: boolean;
  delegates?: boolean;
  expanded?: boolean;
  collapsed?: boolean;
  /**
   * How many delegated routes a collapsed route hides, and their problems per hidden route: its id, and its name
   * with the hidden routes between it and this node (`mid › leaf`), so two routes of one name stay apart.
   */
  hiddenRoutes?: number;
  hiddenProblems?: { id: string; route: string; signals: PathSignal[] }[];
  rows: number[];
  /** The distinct row signals that name this node. */
  problems: PathSignal[];
  /** A route node's backends' error and warning findings, which make it at least warning (D-082). */
  backendProblems?: PathSignal[];
}

/**
 * An edge between adjacent columns. share is set only where every row on it shows a share (design §4.4) and the
 * rows are one rule's or agree on it. Never style or animate an edge as healthy unless state is "healthy".
 */
export interface MapEdge {
  id: string;
  source: string;
  target: string;
  share?: number;
  /** The worst problem state of the rows on the edge. */
  state: HealthState;
  /** Every row on the edge is an error or its share is failing. */
  broken: boolean;
  rows: number[];
}

/** A path row's state, and whether it fails (an error, or a failing share). */
export interface RowMark {
  state: HealthState;
  fails: boolean;
}

export interface MapGraph {
  nodes: MapNode[];
  edges: MapEdge[];
  /** Per input row, indexed as nodes' and edges' rows are. */
  rows: RowMark[];
  /** The rows are filtered by state, so the healthy rows on an edge may be missing: no edge is drawn broken. */
  stateFiltered?: boolean;
}

const fails = (r: Pick<PathRow, "problem" | "shareState">) => r.problem.state === "error" || r.shareState === "failing";

const RANK: Record<HealthState, number> = { healthy: 0, unknown: 1, warning: 2, error: 3 };
export const worse = (a: HealthState, b: HealthState) => (RANK[b] > RANK[a] ? b : a);
export const refLabel = (r: { namespace?: string; name: string }) =>
  r.namespace ? `${r.namespace}/${r.name}` : r.name;
const same = (a?: LinkedRef, b?: LinkedRef) => !!a && !!b && refParam(a) === refParam(b);

const signalKey = (s: PathSignal) => JSON.stringify([s.kind, s.code, s.title, s.ref && refParam(s.ref), s.state]);

/** A node's kind line: the resource kind, the listener noun, "Rule n", or "Action" for a rule's written action. */
export const kindLabel = (n: MapNode) =>
  n.action
    ? mapCopy.action
    : n.kind === "listener"
      ? listenerCopy.listener
      : n.kind === "rule"
        ? mapCopy.rule((n.rule ?? 0) + 1)
        : (n.ref?.kind ?? n.kind);

/** The node ids a row passes through. */
interface RowIds {
  gateway: string;
  listener: string;
  routes: string[];
  end?: string;
}

/** The node a signal belongs to, by its kind, listener and ref; anything unplaced goes to the row's last node. */
function signalNode(s: PathSignal, r: PathRow, ids: RowIds): string {
  const last = ids.end ?? ids.routes[ids.routes.length - 1] ?? ids.listener;
  if (s.kind === "listener" || s.listener === r.listener.name || (s.kind === "waiting" && s.code === "listener"))
    return ids.listener;
  if (same(s.ref, r.backend) && ids.end) return ids.end;
  const i = r.routes.findIndex((x) => same(s.ref, x));
  if (i >= 0) return ids.routes[i] ?? last;
  if (same(s.ref, r.listenerSet)) return ids.listener;
  if (same(s.ref, r.gateway)) return ids.gateway;
  return last;
}

// region Component
/** Options of buildGraph: a state filter on the rows, and the delegating routes collapsed. */
export interface GraphOptions {
  stateFiltered?: boolean;
  collapsed?: ReadonlySet<string>;
}

/** The ids of a row's route chain, root first; a delegated route's id is its chain from the root. */
const chainIds = (r: PathRow) =>
  r.routes.map(
    (_, k) =>
      `r:${r.routes
        .slice(0, k + 1)
        .map(refParam)
        .join("~")}`,
  );

/**
 * One node per distinct Gateway, listener, route and backend of the rows, in order of first appearance.
 * A delegated route shows under its own parent, with no listener edge, unless a route above it is collapsed; its id
 * is its chain from the root, so a route delegated from two parents shows under each. A route in expanded that ends a
 * row opens into one node per rule between it and the backends. A node's state is the worst of the row signals
 * that name it; a hidden route's signals go to its collapsed parent, listed under the hidden route's name. A route
 * that sends to a backend with an error or warning finding is at least warning (D-082).
 */
export function buildGraph(
  rows: PathRow[],
  expanded: ReadonlySet<string> = new Set(),
  opts: GraphOptions = {},
): MapGraph {
  const collapsed = opts.collapsed ?? new Set<string>();
  const nodes = new Map<string, MapNode>();
  const edges = new Map<string, MapEdge & { shares: (number | undefined)[]; rules: Set<string> }>();
  const hidden = new Map<string, Set<string>>();
  const hiddenProblems = new Map<string, Map<string, { id: string; route: string; signals: PathSignal[] }>>();
  const node = (id: string, i: number, init: Omit<MapNode, "id" | "rows" | "state" | "problems">) => {
    let n = nodes.get(id);
    if (!n) {
      n = { id, ...init, state: "healthy", rows: [], problems: [] };
      nodes.set(id, n);
    }
    if (n.rows[n.rows.length - 1] !== i) n.rows.push(i);
    return n;
  };
  // rule is the row's last route and rule, so a share never mixes rules of different routes on one edge.
  const edge = (source: string, target: string, i: number, r: PathRow, rule?: string) => {
    const id = `${source}>${target}`;
    let e = edges.get(id);
    if (!e) {
      e = { id, source, target, state: "healthy", broken: true, rows: [], shares: [], rules: new Set() };
      edges.set(id, e);
    }
    e.rows.push(i);
    e.state = worse(e.state, r.problem.state);
    e.broken &&= fails(r);
    if (rule) {
      e.shares.push(r.shareState === "shown" ? r.share : undefined);
      e.rules.add(rule);
    }
  };

  rows.forEach((r, i) => {
    const gateway = `g:${refParam(r.gateway)}`;
    node(gateway, i, { kind: "gateway", ref: r.gateway, name: refLabel(r.gateway) });
    const set = r.listenerSet ? `${refParam(r.listenerSet)}|` : "";
    const listener = `l:${set}${r.listener.name}`;
    node(listener, i, { kind: "listener", ref: r.listenerSet, name: r.listener.name });
    edge(gateway, listener, i, r);

    // A route shows unless a route above it is collapsed; a hidden one stands for its collapsed parent.
    const chain = chainIds(r);
    let shown = Math.min(1, chain.length);
    while (shown < chain.length && !collapsed.has(chain[shown - 1] as string)) shown++;
    const routes = chain.map((_, k) => chain[Math.min(k, shown - 1)] as string);
    for (let k = 0; k < shown; k++) {
      const id = chain[k] as string;
      const n = node(id, i, { kind: "route", ref: r.routes[k], name: refLabel(r.routes[k] as LinkedRef) });
      if (k > 0) n.delegatedUnder = chain[k - 1];
      if (k < chain.length - 1) n.delegates = true;
      else n.ends = true;
      if (expanded.has(id) && n.ends) n.expanded = true;
    }
    const tail = chain[shown - 1];
    if (tail && shown < chain.length) {
      const set = hidden.get(tail) ?? new Set();
      for (const id of chain.slice(shown)) set.add(id);
      hidden.set(tail, set);
    }
    const root = chain[0];
    if (root) edge(listener, root, i, r);

    let end: string | undefined;
    if (r.backend) {
      end = `b:${refParam(r.backend)}`;
      node(end, i, { kind: "backend", ref: r.backend, name: refLabel(r.backend) });
    } else if (r.action !== "backend" && r.action !== "delegate" && tail) {
      end = `a:${r.action}:${tail}`;
      node(end, i, { kind: "backend", action: r.action, name: actionCopy[r.action] });
    }
    // An expanded route that ends the row opens into its rules; r.rule indexes the last route's rules.
    const rule = `${chain[chain.length - 1]}#${r.rule}`;
    let from = tail;
    if (tail && shown === chain.length && expanded.has(tail)) {
      const id = `u:${rule}`;
      node(id, i, { kind: "rule", ref: r.routes[shown - 1], rule: r.rule, name: matchSummary(r).text });
      edge(tail, id, i, r);
      from = id;
    }
    if (from && end) edge(from, end, i, r, rule);

    // Each signal raises the state of the node it names; a row state no signal explains goes to its last node.
    const ids: RowIds = { gateway, listener, routes, end };
    // Lists s on collapsed node n under hidden route k, by the hidden routes' names.
    const hide = (n: MapNode, k: number, s: PathSignal) => {
      const by = hiddenProblems.get(n.id) ?? new Map();
      hiddenProblems.set(n.id, by);
      const id = chain[k] as string;
      const route = r.routes
        .slice(shown, k + 1)
        .map((x) => refLabel(x))
        .join(" › ");
      const list = by.get(id) ?? { id, route, signals: [] };
      by.set(id, list);
      const key = signalKey(s);
      if (!list.signals.some((p: PathSignal) => signalKey(p) === key)) list.signals.push(s);
    };
    let explained: HealthState = "healthy";
    for (const s of r.problem.signals) {
      const n = nodes.get(signalNode(s, r, ids));
      const k = r.routes.findIndex((x) => same(s.ref, x));
      if (n && k >= shown) {
        // A hidden route's signal raises its collapsed parent and is listed under the hidden route.
        n.state = worse(n.state, s.state);
        hide(n, k, s);
      } else if (n) {
        n.state = worse(n.state, s.state);
        const k = signalKey(s);
        if (!n.problems.some((p) => signalKey(p) === k)) n.problems.push(s);
      }
      // A route whose backend has an error or warning finding is at least warning, as on its page (D-082).
      // A hidden route's goes to its collapsed parent, listed under the hidden route.
      const route = nodes.get(routes[routes.length - 1] ?? "");
      if (route && !r.mirror && s.kind === "finding" && same(s.ref, r.backend) && RANK[s.state] >= RANK.warning) {
        route.state = worse(route.state, "warning");
        const list = route.backendProblems ?? [];
        if (shown < chain.length) hide(route, chain.length - 1, s);
        else if (!list.some((p) => signalKey(p) === signalKey(s))) route.backendProblems = [...list, s];
      }
      explained = worse(explained, s.state);
    }
    if (RANK[r.problem.state] > RANK[explained]) {
      const n = nodes.get(signalNode({ kind: "coverage", state: r.problem.state }, r, ids));
      if (n) n.state = worse(n.state, r.problem.state);
    }
  });

  const marks = rows.map((r) => ({ state: r.problem.state, fails: fails(r) }));
  for (const n of nodes.values()) {
    // No signal names a rule, and its rows are exactly that rule's paths, so its state is their worst.
    if (n.kind === "rule") n.state = n.rows.reduce<HealthState>((s, i) => worse(s, marks[i]?.state ?? s), "healthy");
    if (n.delegates && collapsed.has(n.id)) n.collapsed = true;
    const h = hidden.get(n.id);
    if (h) n.hiddenRoutes = h.size;
    const p = hiddenProblems.get(n.id);
    if (p) n.hiddenProblems = [...p.values()];
  }
  const stateFiltered = !!opts.stateFiltered;
  return {
    nodes: [...nodes.values()],
    rows: marks,
    // Share is per rule: an edge over several rules shows it only when they agree.
    edges: [...edges.values()].map(({ shares, rules, ...e }) => {
      const shown = shares.length > 0 && shares.every((s) => s !== undefined);
      const one = rules.size === 1 || shares.every((s) => s === shares[0]);
      const out = { ...e, broken: e.broken && !stateFiltered };
      return shown && one ? { ...out, share: Math.max(...(shares as number[])) } : out;
    }),
    ...(stateFiltered ? { stateFiltered } : {}),
  };
}

/** The graph of a Gateway's rows; a Gateway with no paths still shows itself, its state not known from paths. */
export function gatewayGraph(
  gateway: LinkedRef,
  rows: PathRow[],
  expanded: ReadonlySet<string> = new Set(),
  opts: GraphOptions = {},
): MapGraph {
  const g = buildGraph(rows, expanded, opts);
  if (!g.nodes.some((n) => n.kind === "gateway"))
    g.nodes.unshift({
      id: `g:${refParam(gateway)}`,
      kind: "gateway",
      ref: gateway,
      name: refLabel(gateway),
      state: "unknown",
      rows: [],
      problems: [],
    });
  return g;
}

/** A lit edge's state on the active node's paths: the worst of the rows it shares with the node. */
export interface LitEdge {
  state: HealthState;
  broken: boolean;
}

/**
 * The nodes and edges on paths through node id: those that share a row with it (design §6.1, hover).
 * Each edge carries its state over the shared rows only, so a healthy path's trunk does not read as failing.
 * Under a state filter no lit edge is broken, as in buildGraph.
 */
export function throughNode(g: MapGraph, id: string): { nodes: Set<string>; edges: Map<string, LitEdge> } {
  const rows = new Set(g.nodes.find((n) => n.id === id)?.rows);
  const shares = (r: number[]) => r.some((i) => rows.has(i));
  const edges = new Map<string, LitEdge>();
  for (const e of g.edges) {
    const on = e.rows.filter((i) => rows.has(i)).map((i) => g.rows[i] as RowMark);
    if (on.length)
      edges.set(e.id, {
        state: on.reduce<HealthState>((s, r) => worse(s, r.state), "healthy"),
        broken: !g.stateFiltered && on.every((r) => r.fails),
      });
  }
  return { nodes: new Set([id, ...g.nodes.filter((n) => shares(n.rows)).map((n) => n.id)]), edges };
}
