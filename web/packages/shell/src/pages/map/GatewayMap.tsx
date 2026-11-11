import "@xyflow/react/dist/base.css";
import type { SerializedStyles } from "@emotion/react";
import { useTheme } from "@emotion/react";
import styled from "@emotion/styled";
import { Controls, type Edge, Handle, MiniMap, type Node, type NodeProps, Position, ReactFlow } from "@xyflow/react";
import { useMemo } from "react";
import type { LinkedRef, PathRow } from "../../api/types";
import { listenerCopy } from "../../copy/explorer";
import { mapCopy } from "../../copy/map";
import { HealthBadge } from "../../health/HealthBadge";
import type { HealthState } from "../../theme/tokens";
import { refParam } from "../explore/detailParts";
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
type Column = "gateway" | "listener" | "route" | "backend";
type MapNodeData = { kind: string; name: string; state: HealthState };
type MapFlowNode = Node<MapNodeData, "map">;

const COLUMNS: Column[] = ["gateway", "listener", "route", "backend"];
const COL_W = NODE_W + 120;
const ROW_H = 96;
const RANK: Record<HealthState, number> = { healthy: 0, unknown: 1, warning: 2, error: 3 };
const worse = (a: HealthState, b: HealthState) => (RANK[b] > RANK[a] ? b : a);
const name = (r: { namespace?: string; name: string }) => (r.namespace ? `${r.namespace}/${r.name}` : r.name);

/**
 * One node per distinct listener, route and backend in four columns, in order of first appearance.
 * A placeholder for the column layout of plan Task 44 (graph.ts, layout.ts).
 */
function toFlow(gateway: LinkedRef, rows: PathRow[]): { nodes: MapFlowNode[]; edges: Edge[] } {
  const cols = new Map<Column, Map<string, MapNodeData>>(COLUMNS.map((c) => [c, new Map()]));
  const edges = new Map<string, Edge>();
  const put = (col: Column, id: string, data: MapNodeData) => {
    const at = cols.get(col);
    const had = at?.get(id);
    at?.set(id, had ? { ...had, state: worse(had.state, data.state) } : data);
  };
  put("gateway", "g", { kind: gateway.kind, name: name(gateway), state: "healthy" });
  for (const r of rows) {
    const state = r.problem.state;
    const chain: string[] = ["g"];
    put("gateway", "g", { kind: gateway.kind, name: name(gateway), state });
    const l = `l:${r.listener.name}`;
    put("listener", l, { kind: listenerCopy.listener, name: r.listener.name, state });
    chain.push(l);
    const route = r.routes[0];
    if (route) {
      const id = `r:${refParam(route)}`;
      put("route", id, { kind: route.kind, name: name(route), state });
      chain.push(id);
    }
    if (r.backend) {
      const id = `b:${refParam(r.backend)}`;
      put("backend", id, { kind: r.backend.kind, name: name(r.backend), state });
      chain.push(id);
    }
    for (let i = 1; i < chain.length; i++) {
      const source = chain[i - 1] as string;
      const target = chain[i] as string;
      edges.set(`${source}>${target}`, { id: `${source}>${target}`, source, target });
    }
  }
  const nodes = COLUMNS.flatMap((col, x) =>
    [...(cols.get(col) ?? new Map<string, MapNodeData>())].map(
      ([id, data], y): MapFlowNode => ({ id, type: "map", data, position: { x: x * COL_W, y: y * ROW_H } }),
    ),
  );
  return { nodes, edges: [...edges.values()] };
}

/** A node card: kind, name and state as icon + colour + text; the state is unknown in a stale view. */
function MapNodeCard({ data }: NodeProps<MapFlowNode>) {
  return (
    <Card data-testid="map-node">
      <Handle type="target" position={Position.Left} isConnectable={false} />
      <div className="kind">{data.kind}</div>
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
        minZoom={0.1}
      >
        <MiniMap pannable zoomable />
        <Controls showInteractive={false} />
      </ReactFlow>
    </MapCanvas>
  );
}

export default GatewayMap;
