import "@xyflow/react/dist/base.css";
import type { SerializedStyles } from "@emotion/react";
import { useTheme } from "@emotion/react";
import styled from "@emotion/styled";
import {
  Controls,
  Handle,
  MiniMap,
  type Node,
  type NodeProps,
  Position,
  ReactFlow,
  type ReactFlowInstance,
} from "@xyflow/react";
import { Minus, Plus } from "lucide-react";
import { type FocusEvent, type KeyboardEvent, useContext, useEffect, useMemo, useRef, useState } from "react";
import { mapCopy } from "../../copy/map";
import { cellCopy } from "../../copy/traffic";
import { HealthBadge } from "../../health/HealthBadge";
import { StaleView } from "../../health/staleView";
import { stateLabel } from "../../health/state";
import type { HealthState } from "../../theme/tokens";
import { MapEdgeLine, type MapFlowEdge, reducedMotion, strokeWidth } from "./edges";
import { download, exportFileName, exportPNG, exportSVG } from "./export";
import { kindLabel, type MapGraph, type MapNode, throughNode } from "./graph";
import { ARROWS, type ArrowKey, nextNode } from "./keyboard";
import { changedColumns, layoutColumns, NODE_H, NODE_W, type Point } from "./layout";
import { MapCanvas } from "./map.css";

// region Styles
const Card = styled.div<{ $selected: boolean }>`
  position: relative;
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
    padding-right: 20px;
    font-size: ${(p) => p.theme.font.size.xs};
    color: ${(p) => p.theme.color.textMuted};
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .name {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
`;
/** A route card's expand toggle, in its top right corner; a pointer control, as the side panel has the same action. */
const Toggle = styled.button`
  position: absolute;
  top: 4px;
  right: 4px;
  display: inline-flex;
  padding: 2px;
  cursor: pointer;
  color: ${(p) => p.theme.color.text};
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    border-color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 10%, ${(p) => p.theme.color.surface});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 20%, ${(p) => p.theme.color.surface});
  }
`;

// region Helpers
type MapNodeData = {
  kind: string;
  name: string;
  state: HealthState;
  delegated: boolean;
  selected: boolean;
  /** Set on a route card: opens or closes it (its delegated routes, else its rules). */
  toggle?: () => void;
  open?: boolean;
  /** A collapsed route's count of hidden delegated routes. */
  hidden?: number;
};
type MapFlowNode = Node<MapNodeData, "map">;

/** Saves the map as drawn (graph, layout, theme) as an SVG or a PNG file. */
export type MapExporter = (kind: "svg" | "png") => Promise<void>;
/** PNG pixels per canvas pixel. */
const PNG_SCALE = 2;

/** A node's accessible name: kind, name and state, plus how many of its paths fail when any do. */
function nodeLabel(g: MapGraph, n: MapNode, stale: boolean): string {
  const base = mapCopy.nodeLabel(kindLabel(n), n.name, stateLabel(stale ? "unknown" : n.state));
  const failing = n.rows.filter((i) => g.rows[i]?.state === "error").length;
  return failing && !stale ? `${base}, ${mapCopy.fail(failing, n.rows.length)}` : base;
}

/** The map node a key or focus event comes from, when the event's target is the node itself. */
const nodeId = (t: EventTarget | null) =>
  t instanceof HTMLElement && t.classList.contains("react-flow__node") ? (t.dataset.id ?? undefined) : undefined;

/** A node card: kind, name and state as icon + colour + text; the state is unknown in a stale view. */
function MapNodeCard({ data }: NodeProps<MapFlowNode>) {
  return (
    // nopan: a press on a card is a click, not the start of a pan, so the click reaches the node.
    <Card className="nopan" data-testid="map-node" $selected={data.selected}>
      <Handle type="target" position={Position.Left} isConnectable={false} />
      <div className="kind">
        {data.kind}
        {data.delegated && ` · ${cellCopy.delegated}`}
        {data.hidden ? ` · ${mapCopy.hiddenDelegated(data.hidden)}` : null}
      </div>
      <div className="name" title={data.name}>
        {data.name}
      </div>
      <HealthBadge state={data.state} />
      {data.toggle ? (
        <Toggle
          type="button"
          className="nopan nodrag"
          data-testid="map-toggle"
          // Out of the tab order: the map is one tab stop, and the side panel offers the same action by keyboard.
          tabIndex={-1}
          aria-label={(data.open ? mapCopy.collapseNode : mapCopy.expandNode)(data.name)}
          aria-expanded={data.open}
          onClick={(e) => {
            e.stopPropagation();
            data.toggle?.();
          }}
        >
          {data.open ? <Minus size={12} aria-hidden="true" /> : <Plus size={12} aria-hidden="true" />}
        </Toggle>
      ) : null}
      <Handle type="source" position={Position.Right} isConnectable={false} />
    </Card>
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
  centre?: { id: string; seq: number };
  /** The query the graph's rows answer, and the focus; a new one redraws the canvas and fits it, a poll keeps the view. */
  view: string;
  /** Opens or closes a route node: shows or hides its delegated routes, or, for a route that delegates none, its rules. */
  onToggle?: (id: string) => void;
  /** Receives this map's exporter while it is drawn, and undefined once it is gone; keep it stable. */
  onExporter?: (exporter?: MapExporter) => void;
  stylesOverride?: SerializedStyles;
}

/**
 * The Gateway map canvas: React Flow with pan, zoom, fit to view and a minimap, styled from theme tokens (design §6).
 * Hovering or focusing a node lights every path through it and dims the rest; a click, Enter or Space selects it.
 * Arrow keys move focus between connected nodes; one node at a time is in the tab order.
 */
export function GatewayMap({
  graph,
  selected,
  onSelect,
  centre,
  view,
  onToggle,
  onExporter,
  stylesOverride,
}: GatewayMapProps) {
  const theme = useTheme();
  const stale = useContext(StaleView);
  const [hovered, setHovered] = useState<string>();
  // focused lights its paths while a node has focus; last keeps the node Tab returns to.
  const [focused, setFocused] = useState<string>();
  const [last, setLast] = useState<string>();
  // Whether the last input was a pointer press rather than a key.
  const pointer = useRef(false);
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const press = () => {
      pointer.current = true;
    };
    const key = () => {
      pointer.current = false;
    };
    // React Flow redraws a node that leaves and re-enters the visible area as a new element; focus follows it.
    // This relies on Chromium firing focusout when a focused element is removed; Firefox does not.
    const lost = (e: Event) => {
      const id = nodeId(e.target);
      const t = e.target as HTMLElement;
      if (!id || (e as globalThis.FocusEvent).relatedTarget || !box.current?.contains(t)) return;
      requestAnimationFrame(() => {
        if (t.isConnected || (document.activeElement && document.activeElement !== document.body)) return;
        box.current?.querySelector<HTMLElement>(`.react-flow__node[data-id="${CSS.escape(id)}"]`)?.focus();
      });
    };
    document.addEventListener("pointerdown", press, true);
    document.addEventListener("keydown", key, true);
    document.addEventListener("focusout", lost, true);
    return () => {
      document.removeEventListener("pointerdown", press, true);
      document.removeEventListener("keydown", key, true);
      document.removeEventListener("focusout", lost, true);
    };
  }, []);
  const flow = useRef<ReactFlowInstance<MapFlowNode, MapFlowEdge>>(undefined);
  // Within one view, a new graph (a poll, an expand or a collapse) re-lays only the columns whose nodes changed.
  const laid = useRef<{ view: string; graph: MapGraph; at: Map<string, Point> }>(undefined);
  const at = useMemo(() => {
    const p = laid.current;
    const next =
      p && p.view === view ? layoutColumns(graph, changedColumns(p.graph, graph), p.at) : layoutColumns(graph);
    laid.current = { view, graph, at: next };
    return next;
  }, [graph, view]);
  // Export draws the graph on the map, at rest (no hover), in the layout and theme on screen.
  useEffect(() => {
    if (!onExporter) return;
    onExporter(async (kind) => {
      const now = new Date();
      const svg = exportSVG(graph, at, theme, { at: now, stale });
      const gateway = graph.nodes.find((n) => n.kind === "gateway")?.name ?? "";
      const blob = kind === "svg" ? new Blob([svg], { type: "image/svg+xml" }) : await exportPNG(svg, PNG_SCALE);
      download(blob, exportFileName(gateway, kind, now));
    });
    return () => onExporter(undefined);
  }, [onExporter, graph, at, theme, stale]);
  const active = hovered ?? focused ?? selected;
  const has = (id?: string) => !!id && graph.nodes.some((n) => n.id === id);
  const tabStop = has(last) ? last : has(selected) ? selected : graph.nodes.find((n) => n.kind === "gateway")?.id;
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
            toggle: n.kind === "route" && onToggle ? () => onToggle(n.id) : undefined,
            open: n.delegates ? !n.collapsed : !!n.expanded,
            hidden: n.hiddenRoutes,
          },
          position: at.get(n.id) ?? { x: 0, y: 0 },
          // A fixed size: a new node object keeps its dimensions, so a hover never hides the map while it re-measures.
          width: NODE_W,
          height: NODE_H,
          className: lit ? (lit.nodes.has(n.id) ? "lit" : "dim") : undefined,
          ariaLabel: nodeLabel(graph, n, stale),
          domAttributes: { tabIndex: n.id === tabStop ? 0 : -1 },
        }),
      ),
    [graph, at, lit, selected, stale, tabStop, onToggle],
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
  const centred = useRef(0);
  // The containment pan runs once per selection, so a poll's redraw never moves a node the user panned away.
  const checked = useRef<string>(undefined);
  useEffect(() => {
    const f = flow.current;
    const rect = box.current?.getBoundingClientRect();
    if (!f || !rect?.width) return;
    const centreOn = (id: string, zoom: number) => {
      const p = at.get(id);
      if (!p) return;
      const x = rect.width / 2 - (p.x + NODE_W / 2) * zoom;
      const y = rect.height / 2 - (p.y + NODE_H / 2) * zoom;
      void f.setViewport({ x, y, zoom }, { duration: reducedMotion() ? 0 : 300 });
    };
    const v = f.getViewport();
    if (centre && centre.seq !== centred.current) {
      centred.current = centre.seq;
      centreOn(centre.id, Math.max(v.zoom, 0.8));
      return;
    }
    if (selected === checked.current) return;
    checked.current = selected;
    const p = selected ? at.get(selected) : undefined;
    if (!selected || !p) return;
    const left = p.x * v.zoom + v.x;
    const top = p.y * v.zoom + v.y;
    if (left < 0 || top < 0 || left + NODE_W * v.zoom > rect.width || top + NODE_H * v.zoom > rect.height)
      centreOn(selected, v.zoom);
  }, [centre, selected, at]);
  // Moves focus to a node; one that onlyRenderVisibleElements left out is centred first, then focused once drawn.
  // The node's own focus handler records it as the tab stop, so last changes only once focus has moved.
  const moveTo = (id: string) => {
    const find = () => box.current?.querySelector<HTMLElement>(`.react-flow__node[data-id="${CSS.escape(id)}"]`);
    const el = find();
    if (el) return el.focus();
    const f = flow.current;
    const p = at.get(id);
    if (!f || !p) return;
    void f.setCenter(p.x + NODE_W / 2, p.y + NODE_H / 2, { zoom: f.getViewport().zoom, duration: 0 });
    let tries = 5;
    const retry = () => {
      const n = find();
      if (n) n.focus();
      else if (tries-- > 0) requestAnimationFrame(retry);
    };
    requestAnimationFrame(retry);
  };
  const onKeyDown = (e: KeyboardEvent) => {
    const id = nodeId(e.target);
    if (!id) return;
    if (ARROWS.includes(e.key)) {
      e.preventDefault();
      const to = nextNode(graph, id, e.key as ArrowKey, at);
      if (to) moveTo(to);
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      onSelect(id);
    }
  };
  return (
    <MapCanvas
      ref={box}
      data-testid="gateway-map"
      stylesOverride={stylesOverride}
      onKeyDown={onKeyDown}
      onFocus={(e: FocusEvent) => {
        const id = nodeId(e.target);
        // Only keyboard focus lights paths; focus that follows a pointer press leaves the view as it is.
        setFocused(id && !pointer.current ? id : undefined);
        if (id) setLast(id);
      }}
      onBlur={(e: FocusEvent) => {
        if (!nodeId(e.relatedTarget)) setFocused(undefined);
      }}
    >
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
        edgesFocusable={false}
        ariaLabelConfig={{
          "node.a11yDescription.default": mapCopy.keys,
          "node.a11yDescription.keyboardDisabled": mapCopy.keys,
          "edge.a11yDescription.default": "",
        }}
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
        {/* The panel narrows the canvas, so the minimap steps aside while it is open. */}
        {selected ? null : <MiniMap pannable zoomable />}
        <Controls showInteractive={false} />
      </ReactFlow>
    </MapCanvas>
  );
}

export default GatewayMap;
