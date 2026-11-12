import { COLUMNS, type MapGraph } from "./graph";
import { layoutColumns, type Point } from "./layout";

// region Helpers
export type ArrowKey = "ArrowLeft" | "ArrowRight" | "ArrowUp" | "ArrowDown";
export const ARROWS: readonly string[] = ["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"];

const column = (g: MapGraph, id: string) => COLUMNS.indexOf(g.nodes.find((n) => n.id === id)?.kind ?? "gateway");

// region Component
/**
 * The node an arrow key moves to from node from (design §6.1, item 8). Left and right follow an edge to the
 * adjacent column, picking the connected node nearest in height; up and down step to the neighbour in the
 * same column. Undefined when there is none. at is the layout's positions, computed from g when omitted.
 */
export function nextNode(
  g: MapGraph,
  from: string,
  key: ArrowKey,
  at: ReadonlyMap<string, Point> = layoutColumns(g),
): string | undefined {
  const here = at.get(from);
  if (!here) return undefined;
  if (key === "ArrowUp" || key === "ArrowDown") {
    const c = column(g, from);
    const col = g.nodes
      .filter((n) => COLUMNS.indexOf(n.kind) === c && at.has(n.id))
      .sort((a, b) => (at.get(a.id) as Point).y - (at.get(b.id) as Point).y);
    const k = col.findIndex((n) => n.id === from);
    return col[key === "ArrowUp" ? k - 1 : k + 1]?.id;
  }
  const right = key === "ArrowRight";
  const linked = g.edges
    .filter((e) => (right ? e.source : e.target) === from)
    .map((e) => (right ? e.target : e.source));
  let best: string | undefined;
  let gap = Number.POSITIVE_INFINITY;
  for (const id of linked) {
    const p = at.get(id);
    const d = p ? Math.abs(p.y - here.y) : Number.POSITIVE_INFINITY;
    if (d < gap) {
      gap = d;
      best = id;
    }
  }
  return best;
}
