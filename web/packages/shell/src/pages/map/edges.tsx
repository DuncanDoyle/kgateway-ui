import { BaseEdge, type Edge, EdgeLabelRenderer, type EdgeProps, getBezierPath, Position } from "@xyflow/react";
import { useSyncExternalStore } from "react";
import { HealthIcon } from "../../health/HealthIcon";
import { useShownState } from "../../health/staleView";
import type { MapEdge } from "./graph";

// region Helpers
export type MapEdgeData = Pick<MapEdge, "state" | "broken" | "share"> & { dim: boolean };
export type MapFlowEdge = Edge<MapEdgeData, "map">;

const REDUCE = "(prefers-reduced-motion: reduce)";
const query = () => (typeof matchMedia === "function" ? matchMedia(REDUCE) : undefined);

/** True while the user asks for reduced motion. */
export const reducedMotion = () => !!query()?.matches;

const subscribe = (change: () => void) => {
  const q = query();
  q?.addEventListener?.("change", change);
  return () => q?.removeEventListener?.("change", change);
};

/** Follows prefers-reduced-motion, including a change while the map is open. */
export const useReducedMotion = () => useSyncExternalStore(subscribe, reducedMotion, () => false);

/** Stroke width: 1.5 px, or 1.5–6 px by share where the share is shown (design §4.4). */
export const strokeWidth = (share?: number) => (share === undefined ? 1.5 : 1.5 + (Math.min(100, share) / 100) * 4.5);

// region Component
/**
 * An edge in its state's stroke and pattern: solid healthy, dotted warning, dash-dot unknown. A broken edge
 * (every row fails) is dashed with a filled error icon; a partly failing one is solid with a hollow error ring.
 * Only a healthy edge carries the flow overlay, and none under reduced motion. Unknown in a stale view.
 */
export function MapEdgeLine({ id, sourceX, sourceY, targetX, targetY, data, style }: EdgeProps<MapFlowEdge>) {
  const state = useShownState(data?.state ?? "unknown");
  const still = useReducedMotion();
  const broken = state === "error" && !!data?.broken;
  // A partly failing edge keeps a solid stroke but carries a hollow error ring, so it never reads as healthy.
  const partial = state === "error" && !broken;
  const [path, x, y] = getBezierPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition: Position.Right,
    targetPosition: Position.Left,
  });
  return (
    <>
      <BaseEdge id={id} path={path} style={style} className={`state-${state}${broken ? " broken" : ""}`} />
      {state === "healthy" && !still ? (
        <path d={path} className="map-edge-flow" style={{ strokeWidth: style?.strokeWidth }} />
      ) : null}
      {broken || partial ? (
        <EdgeLabelRenderer>
          <div
            className={`map-edge-mark ${broken ? "broken" : "partial"}${data?.dim ? " dim" : ""}`}
            data-edge={id}
            style={{ transform: `translate(-50%, -50%) translate(${x}px, ${y}px)` }}
          >
            {broken ? <HealthIcon state="error" size={14} /> : null}
          </div>
        </EdgeLabelRenderer>
      ) : null}
    </>
  );
}
