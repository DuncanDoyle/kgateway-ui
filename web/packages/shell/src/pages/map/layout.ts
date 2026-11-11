import { COLUMNS, type MapGraph, type NodeKind } from "./graph";

// region Helpers
/** Column pitch, row pitch and a delegated route's indent under its root, in canvas pixels. */
export const COL_W = 340;
export const ROW_H = 96;
export const INDENT = 24;
/** Down-and-up barycentre sweeps per layout. */
const SWEEPS = 4;

export type Point = { x: number; y: number };
/** A node and the delegated routes under it, which move with it. */
type Unit = { id: string; kids: Unit[] };
/** A column's order, as units. */
type Column = Unit[];

/** A unit's ids, depth first. */
const flat = (u: Unit): string[] => [u.id, ...u.kids.flatMap(flat)];

/** Each node's slot in its column, scaled to (0, 1) so columns of different sizes compare. */
function slots(col: Column): Map<string, number> {
  const ids = col.flatMap(flat);
  return new Map(ids.map((id, k) => [id, (k + 0.5) / ids.length]));
}

/** The mean slot of a node set's neighbours in ref; undefined when it has none there. */
function barycentre(ids: string[], adj: Map<string, string[]>, ref: Map<string, number>): number | undefined {
  let sum = 0;
  let n = 0;
  for (const id of ids)
    for (const o of adj.get(id) ?? []) {
      const p = ref.get(o);
      if (p !== undefined) {
        sum += p;
        n++;
      }
    }
  return n ? sum / n : undefined;
}

/** Reorders items by barycentre against ref; an item with no neighbour there keeps its own slot. Stable. */
function sortBy<T>(
  items: T[],
  ids: (t: T) => string[],
  adj: Map<string, string[]>,
  ref: Map<string, number>,
  own: Map<string, number>,
): T[] {
  const keyed = items.map((t, k) => ({ t, k, b: barycentre(ids(t), adj, ref) ?? own.get(ids(t)[0] as string) ?? 0 }));
  keyed.sort((a, b) => a.b - b.b || a.k - b.k);
  return keyed.map((e) => e.t);
}

/** The columns whose node ids differ between a and b. */
export function changedColumns(a: MapGraph, b: MapGraph): Set<NodeKind> {
  const key = (g: MapGraph, kind: NodeKind) =>
    g.nodes
      .filter((n) => n.kind === kind)
      .map((n) => n.id)
      .sort()
      .join("\n");
  return new Set(COLUMNS.filter((k) => key(a, k) !== key(b, k)));
}

// region Component
/**
 * Positions in fixed columns, each ordered by the barycentre of its connected nodes over a few sweeps,
 * so edges cross less (design §6.2). Delegated routes stay right under their parent, indented a step per level.
 * The rule column takes a place only when it has nodes. Deterministic.
 * With changed, only those columns are reordered; the others keep prev's heights, or their order in g.
 * A column with a node prev lacks is reordered too, since a kept column has no place for it.
 */
export function layoutColumns(
  g: MapGraph,
  changed?: ReadonlySet<NodeKind>,
  prev?: ReadonlyMap<string, Point>,
): Map<string, Point> {
  const left = new Map<string, string[]>();
  const right = new Map<string, string[]>();
  const add = (m: Map<string, string[]>, a: string, b: string) => {
    const l = m.get(a);
    if (l) l.push(b);
    else m.set(a, [b]);
  };
  for (const e of g.edges) {
    add(right, e.source, e.target);
    add(left, e.target, e.source);
  }

  // Start from prev's order where it has one, else g's order.
  const byPrev = (a: { id: string }, b: { id: string }) =>
    (prev?.get(a.id)?.y ?? Number.POSITIVE_INFINITY) - (prev?.get(b.id)?.y ?? Number.POSITIVE_INFINITY);
  const cols: Column[] = COLUMNS.map((kind) => {
    const of = g.nodes.filter((n) => n.kind === kind);
    if (prev) of.sort((a, b) => byPrev(a, b) || 0);
    const units = new Map(of.map((n) => [n.id, { id: n.id, kids: [] as Unit[] }]));
    const top: Unit[] = [];
    for (const n of of) {
      const u = units.get(n.id) as Unit;
      const parent = n.delegatedUnder ? units.get(n.delegatedUnder) : undefined;
      if (parent) parent.kids.push(u);
      else top.push(u);
    }
    return top;
  });

  const fresh = new Set(prev ? g.nodes.filter((n) => !prev.has(n.id)).map((n) => n.kind) : []);
  const moves = (c: number) => !changed || changed.has(COLUMNS[c] as NodeKind) || fresh.has(COLUMNS[c] as NodeKind);
  // Neighbours may sit more than one column away (a route's backends, past an empty rule column), so each sweep
  // compares against the slots of every column on that side.
  const sideSlots = (from: number, to: number) => {
    const out = new Map<string, number>();
    for (let c = from; c <= to; c++) for (const [id, p] of slots(cols[c] as Column)) out.set(id, p);
    return out;
  };
  const sortUnits = (
    units: Unit[],
    adj: Map<string, string[]>,
    ref: Map<string, number>,
    own: Map<string, number>,
  ): Unit[] => sortBy(units, flat, adj, ref, own).map((u) => ({ id: u.id, kids: sortUnits(u.kids, adj, ref, own) }));
  const sweep = (c: number, ref: Map<string, number>, adj: Map<string, string[]>) => {
    const col = cols[c] as Column;
    cols[c] = sortUnits(col, adj, ref, slots(col));
  };
  for (let s = 0; s < SWEEPS; s++) {
    for (let c = 1; c < cols.length; c++) if (moves(c)) sweep(c, sideSlots(0, c - 1), left);
    for (let c = cols.length - 2; c >= 0; c--) if (moves(c)) sweep(c, sideSlots(c + 1, cols.length - 1), right);
  }

  // Columns centre on y = 0; a column that did not move keeps prev's heights.
  const out = new Map<string, Point>();
  const shown = COLUMNS.filter((k, c) => k !== "rule" || (cols[c] as Column).length > 0);
  cols.forEach((col, c) => {
    const x = shown.indexOf(COLUMNS[c] as NodeKind) * COL_W;
    const depth = new Map<string, number>();
    const walk = (u: Unit, d: number) => {
      depth.set(u.id, d);
      for (const k of u.kids) walk(k, d + 1);
    };
    for (const u of col) walk(u, 0);
    const all = col.flatMap(flat);
    all.forEach((id, k) => {
      const kept = !moves(c) ? prev?.get(id) : undefined;
      out.set(id, { x: x + (depth.get(id) ?? 0) * INDENT, y: kept?.y ?? (k - (all.length - 1) / 2) * ROW_H });
    });
  });
  return out;
}
