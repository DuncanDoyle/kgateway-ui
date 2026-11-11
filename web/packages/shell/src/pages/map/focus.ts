import type { HealthState } from "../../theme/tokens";
import { type MapGraph, worse } from "./graph";

// region Component
/**
 * The map reduced to node id's upstream and downstream paths: the nodes and edges that share a row with it, each
 * keeping only those rows (design §6.1, focus mode). Edge states follow the kept rows. An id not on the map keeps it whole.
 */
export function focusSubgraph(g: MapGraph, id: string): MapGraph {
  const self = g.nodes.find((n) => n.id === id);
  if (!self) return g;
  const on = new Set(self.rows);
  const keep = (rows: number[]) => rows.filter((i) => on.has(i));
  const nodes = g.nodes.flatMap((n) => {
    const rows = keep(n.rows);
    return n.id === id || rows.length ? [{ ...n, rows }] : [];
  });
  const edges = g.edges.flatMap((e) => {
    const rows = keep(e.rows);
    if (!rows.length) return [];
    const marks = rows.map((i) => g.rows[i]).filter((m) => m !== undefined);
    const state = marks.reduce<HealthState>((s, m) => worse(s, m.state), "healthy");
    return [{ ...e, rows, state, broken: !g.stateFiltered && marks.every((m) => m.fails) }];
  });
  return { ...g, nodes, edges };
}
