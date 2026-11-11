import "@xyflow/react/dist/base.css";
import type { SerializedStyles } from "@emotion/react";
import { useTheme } from "@emotion/react";
import styled from "@emotion/styled";
import {
  BaseEdge,
  Controls,
  type Edge,
  EdgeLabelRenderer,
  type EdgeProps,
  getBezierPath,
  Handle,
  MiniMap,
  type Node,
  type NodeProps,
  Position,
  ReactFlow,
  type ReactFlowInstance,
} from "@xyflow/react";
import { useEffect, useMemo, useRef, useState } from "react";
import { listenerCopy } from "../../copy/explorer";
import { mapCopy } from "../../copy/map";
import { cellCopy } from "../../copy/traffic";
import { HealthBadge } from "../../health/HealthBadge";
import { HealthIcon } from "../../health/HealthIcon";
import { useShownState } from "../../health/staleView";
import type { HealthState } from "../../theme/tokens";
import { type MapEdge, type MapGraph, type MapNode, throughNode } from "./graph";
import { layoutColumns } from "./layout";
import { MapCanvas } from "./map.css";

// region Styles
const NODE_W = 220;
const NODE_H = 72;

const Card = styled.div<{ $selected: boolean }>`
  box-sizing: border-box;
  width: ${NODE_W}px;
  height: ${NODE_H}px;
  padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
  background: ${(p) => p.theme.color.surface};
  color: ${(p) => p.theme.color.text};
  border: var(--xy-node-border);
  border-radius: ${(p) => p.theme.radius.md};
  cursor: pointer;
  /* Selected: a doubled accent border, so it shows without colour too. */
  box-shadow: ${(p) => (p.$selected ? `0 0 0 2px ${p.theme.color.accentText}` : "none")};
  &:hover {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.surface});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
  .kind {
    font-size: ${(p) => p.theme.font.size.xs};
    color: ${(p) => p.theme.color.textMuted};
  }
  .name {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
`;

// region Helpers
type MapNodeData = { kind: string; name: string; state: HealthState; delegated: boolean; selected: boolean };
type MapFlowNode = Node<MapNodeData, "map">;
type MapEdgeData = Pick<MapEdge, "state" | "broken" | "share"> & { dim: boolean };
type MapFlowEdge = Edge<MapEdgeData, "map">;

/** The card's kind line: the resource kind, the listener noun, or "Action" for a rule's written action. */
const kindLabel = (n: MapNode) =>
  n.action ? mapCopy.action : n.kind === "listener" ? listenerCopy.listener : (n.ref?.kind ?? n.kind);

/** Stroke width: 1.5 px, or 1.5–6 px by share where the share is shown (design §4.4). */
const strokeWidth = (share?: number) => (share === undefined ? 1.5 : 1.5 + (Math.min(100, share) / 100) * 4.5);

const reducedMotion = () => typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

/** A node card: kind, name and state as icon + colour + text; the state is unknown in a stale view. */
function MapNodeCard({ data }: NodeProps<MapFlowNode>) {
  return (
    // nopan: a press on a card is a click, not the start of a pan, so the click reaches the node.
    <Card className="nopan" data-testid="map-node" $selected={data.selected}>
      <Handle type="target" position={Position.Left} isConnectable={false} />
      <div className="kind">
        {data.kind}
        {data.delegated && ` · ${cellCopy.delegated}`}
      </div>
      <div className="name" title={data.name}>
        {data.name}
      </div>
      <HealthBadge state={data.state} />
      <Handle type="source" position={Position.Right} isConnectable={false} />
    </Card>
  );
}

/**
 * An edge in its state's stroke and pattern: solid healthy, dotted warning, dash-dot unknown, solid error.
 * A broken edge (every row fails) is dashed and carries an error icon at its middle. Unknown in a stale view.
 */
function MapEdgeLine({ id, sourceX, sourceY, targetX, targetY, data, style }: EdgeProps<MapFlowEdge>) {
  const state = useShownState(data?.state ?? "unknown");
  const broken = state === "error" && !!data?.broken;
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
      {broken ? (
        <EdgeLabelRenderer>
          <div
            className={`map-edge-icon${data?.dim ? " dim" : ""}`}
            style={{ transform: `translate(-50%, -50%) translate(${x}px, ${y}px)` }}
          >
            <HealthIcon state="error" size={14} />
          </div>
        </EdgeLabelRenderer>
      ) : null}
    </>
  );
}

const nodeTypes = { map: MapNodeCard };
const edgeTypes = { map: MapEdgeLine };

// region Component
interface GatewayMapProps {
  graph: MapGraph;
  /** The selected node's id; its paths stay lit while nothing is hovered. */
  selected?: string;
  onSelect: (id?: string) => void;
  /** A node to centre on; a new seq centres again. */
  focus?: { id: string; seq: number };
  /** The query the graph's rows answer; a new one redraws the canvas and fits it, a poll keeps the view. */
  view: string;
  stylesOverride?: SerializedStyles;
}

/**
 * The Gateway map canvas: React Flow with pan, zoom, fit to view and a minimap, styled from theme tokens (design §6).
 * Hovering a node lights every path through it and dims the rest; a click selects it.
 */
export function GatewayMap({ graph, selected, onSelect, focus, view, stylesOverride }: GatewayMapProps) {
  const theme = useTheme();
  const [hovered, setHovered] = useState<string>();
  const flow = useRef<ReactFlowInstance<MapFlowNode, MapFlowEdge>>(undefined);
  const at = useMemo(() => layoutColumns(graph), [graph]);
  const active = hovered ?? selected;
  const lit = useMemo(() => (active ? throughNode(graph, active) : undefined), [graph, active]);
  const nodes = useMemo(
    () =>
      graph.nodes.map(
        (n): MapFlowNode => ({
          id: n.id,
          type: "map",
          data: {
            kind: kindLabel(n),
            name: n.name,
            state: n.state,
            delegated: !!n.delegatedUnder,
            selected: n.id === selected,
          },
          position: at.get(n.id) ?? { x: 0, y: 0 },
          // A fixed size: a new node object keeps its dimensions, so a hover never hides the map while it re-measures.
          width: NODE_W,
          height: NODE_H,
          className: lit ? (lit.nodes.has(n.id) ? "lit" : "dim") : undefined,
        }),
      ),
    [graph, at, lit, selected],
  );
  const edges = useMemo(
    () =>
      graph.edges.map((e): MapFlowEdge => {
        // Under hover or selection a lit edge shows its state on the active node's paths; at rest, its worst row.
        const on = lit?.edges.get(e.id);
        return {
          id: e.id,
          type: "map",
          source: e.source,
          target: e.target,
          data: { state: on?.state ?? e.state, broken: on?.broken ?? e.broken, share: e.share, dim: !!lit && !on },
          style: { strokeWidth: strokeWidth(e.share) },
          className: lit ? (on ? "lit" : "dim") : undefined,
        };
      }),
    [graph, lit],
  );
  // A find request centres its node once; otherwise a selected node outside the canvas, which the side panel
  // narrows, is panned back into view at the same zoom. Both measure the canvas itself, not React Flow's last size.
  const box = useRef<HTMLDivElement>(null);
  const centred = useRef(0);
  useEffect(() => {
    const f = flow.current;
    const rect = box.current?.getBoundingClientRect();
    if (!f || !rect?.width) return;
    const centre = (id: string, zoom: number) => {
      const p = at.get(id);
      if (!p) return;
      const x = rect.width / 2 - (p.x + NODE_W / 2) * zoom;
      const y = rect.height / 2 - (p.y + NODE_H / 2) * zoom;
      void f.setViewport({ x, y, zoom }, { duration: reducedMotion() ? 0 : 300 });
    };
    const v = f.getViewport();
    if (focus && focus.seq !== centred.current) {
      centred.current = focus.seq;
      centre(focus.id, Math.max(v.zoom, 0.8));
      return;
    }
    const p = selected ? at.get(selected) : undefined;
    if (!selected || !p) return;
    const left = p.x * v.zoom + v.x;
    const top = p.y * v.zoom + v.y;
    if (left < 0 || top < 0 || left + NODE_W * v.zoom > rect.width || top + NODE_H * v.zoom > rect.height)
      centre(selected, v.zoom);
  }, [focus, selected, at]);
  return (
    <MapCanvas ref={box} data-testid="gateway-map" stylesOverride={stylesOverride}>
      <ReactFlow
        key={view}
        aria-label={mapCopy.canvas}
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        colorMode={theme.mode}
        nodesDraggable={false}
        nodesConnectable={false}
        onInit={(i) => {
          flow.current = i;
        }}
        onNodeMouseEnter={(_, n) => setHovered(n.id)}
        onNodeMouseLeave={() => setHovered(undefined)}
        onNodeClick={(_, n) => onSelect(n.id)}
        onPaneClick={() => onSelect(undefined)}
        fitView
        onlyRenderVisibleElements
        minZoom={0.1}
      >
        <MiniMap pannable zoomable />
        <Controls showInteractive={false} />
      </ReactFlow>
    </MapCanvas>
  );
}

export default GatewayMap;
