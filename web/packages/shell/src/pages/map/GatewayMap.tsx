import "@xyflow/react/dist/base.css";
import type { SerializedStyles } from "@emotion/react";
import { useTheme } from "@emotion/react";
import styled from "@emotion/styled";
import { Controls, type Edge, Handle, MiniMap, type Node, type NodeProps, Position, ReactFlow } from "@xyflow/react";
import { useMemo } from "react";
import type { LinkedRef, PathRow } from "../../api/types";
import { listenerCopy } from "../../copy/explorer";
import { mapCopy } from "../../copy/map";
import { cellCopy } from "../../copy/traffic";
import { HealthBadge } from "../../health/HealthBadge";
import type { HealthState } from "../../theme/tokens";
import { buildGraph, type MapNode, refLabel } from "./graph";
import { layoutColumns } from "./layout";
import { MapCanvas } from "./map.css";

// region Styles
const NODE_W = 220;

const Card = styled.div`
  box-sizing: border-box;
  width: ${NODE_W}px;
  padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
  background: ${(p) => p.theme.color.surface};
  color: ${(p) => p.theme.color.text};
  border: var(--xy-node-border);
  border-radius: ${(p) => p.theme.radius.md};
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
type MapNodeData = { kind: string; name: string; state: HealthState; delegated: boolean };
type MapFlowNode = Node<MapNodeData, "map">;

/** The card's kind line: the resource kind, the listener noun, or "Action" for a rule's written action. */
const kindLabel = (n: MapNode) =>
  n.action ? mapCopy.action : n.kind === "listener" ? listenerCopy.listener : (n.ref?.kind ?? n.kind);

/** The path rows as React Flow nodes and edges, in the fixed-column layout (design §6.2). */
function toFlow(gateway: LinkedRef, rows: PathRow[]): { nodes: MapFlowNode[]; edges: Edge[] } {
  const g = buildGraph(rows, new Set());
  // A Gateway with no paths still shows itself; its state is not known from paths.
  if (!g.nodes.some((n) => n.kind === "gateway"))
    g.nodes.unshift({ id: "g", kind: "gateway", ref: gateway, name: refLabel(gateway), state: "unknown", rows: [] });
  const at = layoutColumns(g);
  return {
    nodes: g.nodes.map(
      (n): MapFlowNode => ({
        id: n.id,
        type: "map",
        data: { kind: kindLabel(n), name: n.name, state: n.state, delegated: !!n.delegatedUnder },
        position: at.get(n.id) ?? { x: 0, y: 0 },
      }),
    ),
    edges: g.edges.map((e) => ({ id: e.id, source: e.source, target: e.target })),
  };
}

/** A node card: kind, name and state as icon + colour + text; the state is unknown in a stale view. */
function MapNodeCard({ data }: NodeProps<MapFlowNode>) {
  return (
    <Card data-testid="map-node">
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
const nodeTypes = { map: MapNodeCard };

// region Component
interface GatewayMapProps {
  gateway: LinkedRef;
  rows: PathRow[];
  stylesOverride?: SerializedStyles;
}

/** The Gateway map canvas: React Flow with pan, zoom, fit to view and a minimap, styled from theme tokens (design §6). */
export function GatewayMap({ gateway, rows, stylesOverride }: GatewayMapProps) {
  const theme = useTheme();
  const { nodes, edges } = useMemo(() => toFlow(gateway, rows), [gateway, rows]);
  return (
    <MapCanvas data-testid="gateway-map" stylesOverride={stylesOverride}>
      <ReactFlow
        aria-label={mapCopy.canvas}
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        colorMode={theme.mode}
        nodesDraggable={false}
        nodesConnectable={false}
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
