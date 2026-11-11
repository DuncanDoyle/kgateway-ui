import { COLUMNS, type MapGraph, type NodeKind } from "./graph";

// region Helpers
/** Column pitch, row pitch and a delegated route's indent under its root, in canvas pixels. */
export const COL_W = 340;
export const ROW_H = 96;
export const INDENT = 24;
/** Down-and-up barycentre sweeps per layout. */
const SWEEPS = 4;

export type Point = { x: number; y: number };
/** A column's order: units of one node, or a root route followed by its delegated routes. */
type Column = string[][];

/** Each node's slot in its column, scaled to (0, 1) so columns of different sizes compare. */
function slots(col: Column): Map<string, number> {
  const ids = col.flat();
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

// region Component
/**
 * Positions in four fixed columns, each ordered by the barycentre of its connected nodes over a few sweeps,
 * so edges cross less (design §6.2). Delegated routes stay right under their root, indented. Deterministic.
 * With changed, only those columns are reordered; the others keep prev's positions, or their order in g.
 * A column with a node prev lacks is reordered too, since a kept column has no place for it.
 */
export function layoutColumns(
  g: MapGraph,
  changed?: ReadonlySet<NodeKind>,
  prev?: ReadonlyMap<string, Point>,
): Map<string, Point> {
  const ids = new Set(g.nodes.map((n) => n.id));
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
    const units = new Map<string, string[]>();
    for (const n of of) if (!n.delegatedUnder || !ids.has(n.delegatedUnder)) units.set(n.id, [n.id]);
    for (const n of of) if (n.delegatedUnder) units.get(n.delegatedUnder)?.push(n.id);
    return [...units.values()];
  });

  const fresh = new Set(prev ? g.nodes.filter((n) => !prev.has(n.id)).map((n) => n.kind) : []);
  const moves = (c: number) => !changed || changed.has(COLUMNS[c] as NodeKind) || fresh.has(COLUMNS[c] as NodeKind);
  const sweep = (c: number, r: number, adj: Map<string, string[]>) => {
    const col = cols[c] as Column;
    const ref = slots(cols[r] as Column);
    const own = slots(col);
    cols[c] = sortBy(col, (u) => u, adj, ref, own).map((u) =>
      u.length > 1 ? [u[0] as string, ...sortBy(u.slice(1), (id) => [id], adj, ref, own)] : u,
    );
  };
  for (let s = 0; s < SWEEPS; s++) {
    for (let c = 1; c < cols.length; c++) if (moves(c)) sweep(c, c - 1, left);
    for (let c = cols.length - 2; c >= 0; c--) if (moves(c)) sweep(c, c + 1, right);
  }

  // Columns centre on y = 0; a column that did not move keeps prev's positions.
  const out = new Map<string, Point>();
  const under = new Map(g.nodes.map((n) => [n.id, n.delegatedUnder]));
  cols.forEach((col, c) => {
    const flat = col.flat();
    flat.forEach((id, k) => {
      const kept = !moves(c) ? prev?.get(id) : undefined;
      const indent = under.get(id) && ids.has(under.get(id) as string) ? INDENT : 0;
      out.set(id, kept ?? { x: c * COL_W + indent, y: (k - (flat.length - 1) / 2) * ROW_H });
    });
  });
  return out;
}
