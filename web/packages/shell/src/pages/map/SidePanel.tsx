import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { X } from "lucide-react";
import { useContext, useEffect, useRef } from "react";
import type { Endpoints, LinkedRef, PathRow } from "../../api/types";
import { routeStateBackendCopy } from "../../copy/explorer";
import { mapCopy } from "../../copy/map";
import { cellCopy, signalText } from "../../copy/traffic";
import { backendPath, linkTarget, objectPath } from "../../explore/objectPath";
import { HealthBadge } from "../../health/HealthBadge";
import { HealthIcon } from "../../health/HealthIcon";
import { StaleView } from "../../health/staleView";
import { installPath } from "../../shell/paths";
import { InlineLink, TextButton } from "../../states/controls";
import type { HealthState } from "../../theme/tokens";
import { refParam } from "../explore/detailParts";
import { backendParam } from "../explore/route/refs";
import { endpointsText } from "../traffic/pathText";
import { kindLabel, type MapNode } from "./graph";
import { MAP_H } from "./map.css";

// region Styles
const Panel = styled.aside<{ stylesOverride?: SerializedStyles }>`
  height: ${MAP_H};
  box-sizing: border-box;
  overflow-y: auto;
  padding: ${(p) => p.theme.space(3)};
  background: ${(p) => p.theme.color.surface};
  color: ${(p) => p.theme.color.text};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
  p {
    margin: 0;
  }
  font-size: ${(p) => p.theme.font.size.sm};
  header {
    display: flex;
    align-items: flex-start;
    gap: ${(p) => p.theme.space(2)};
  }
  .kind {
    font-size: ${(p) => p.theme.font.size.xs};
    color: ${(p) => p.theme.color.textMuted};
  }
  h2 {
    margin: 0 0 ${(p) => p.theme.space(1)};
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
    overflow-wrap: anywhere;
  }
  h3 {
    margin: ${(p) => p.theme.space(3)} 0 ${(p) => p.theme.space(1)};
    font-size: ${(p) => p.theme.font.size.sm};
  }
  h4.route {
    margin: ${(p) => p.theme.space(2)} 0 ${(p) => p.theme.space(1)};
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
    display: grid;
    gap: ${(p) => p.theme.space(1)};
  }
  li {
    display: flex;
    align-items: flex-start;
    gap: ${(p) => p.theme.space(1.5)};
    overflow-wrap: anywhere;
  }
  .counts,
  .links,
  .actions {
    margin-top: ${(p) => p.theme.space(3)};
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: ${(p) => p.theme.space(2)};
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
  }
  ${(p) => p.stylesOverride}
`;
const Close = styled.button`
  margin-left: auto;
  flex: none;
  display: inline-flex;
  padding: 4px;
  cursor: pointer;
  color: ${(p) => p.theme.color.text};
  background: transparent;
  border: 0;
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 8%, ${(p) => p.theme.color.surface});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 16%, ${(p) => p.theme.color.surface});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
  }
`;

// region Helpers
type Problem = Exclude<HealthState, "healthy">;
const ORDER: Problem[] = ["error", "warning", "unknown"];
const countLine: Record<Problem, (n: number, of: number) => string> = {
  error: mapCopy.fail,
  warning: mapCopy.warn,
  unknown: mapCopy.unknown,
};

/** The node's detail page: a backend's own page, a listener's Gateway with it selected, else the ref's link. */
function detailLink(installId: string, gateway: LinkedRef, n: MapNode): string | undefined {
  if (n.kind === "gateway" || n.action) return undefined;
  if (n.kind === "backend" && n.ref) return backendPath(installId, n.ref);
  if (n.kind === "listener" && !n.ref) return linkTarget(installId, gateway, undefined, { listener: n.name });
  if (n.kind === "rule" && n.ref && n.rule !== undefined)
    return linkTarget(installId, n.ref, undefined, { rule: String(n.rule + 1) });
  return n.ref ? objectPath(installId, n.ref) : undefined;
}

/** The Traffic filter for the paths through the node, with gateway= fixed. */
function trafficLink(installId: string, gateway: LinkedRef, n: MapNode, rows: PathRow[]): string {
  const q = new URLSearchParams({ gateway: `${gateway.namespace ?? ""}/${gateway.name}` });
  const first = rows[n.rows[0] ?? -1];
  if (n.kind === "listener") q.set("listener", n.name);
  else if ((n.kind === "route" || n.kind === "rule") && n.ref) q.set("route", refParam(n.ref));
  else if (n.kind === "backend" && n.ref) q.set("backend", backendParam(n.ref));
  else if (n.action && first?.routes.length)
    q.set("route", refParam(first.routes[first.routes.length - 1] as LinkedRef));
  return `${installPath(installId, "traffic")}?${q}`;
}

/** A backend's endpoints per port, as the rows through it report them. */
function endpointsByPort(n: MapNode, rows: PathRow[]): [number | undefined, Endpoints][] {
  const by = new Map<number | undefined, Endpoints>();
  for (const i of n.rows) {
    const r = rows[i];
    if (r?.endpoints && !by.has(r.port)) by.set(r.port, r.endpoints);
  }
  return [...by];
}

// region Component
interface SidePanelProps {
  installId: string;
  gateway: LinkedRef;
  node: MapNode;
  /** The map's rows; node.rows indexes them. */
  rows: PathRow[];
  /** Move focus into the panel when it mounts; the map keys it by node, so another node mounts it again. */
  takeFocus?: boolean;
  onClose: () => void;
  /** Shows or hides a route's rule nodes. */
  onRules?: () => void;
  /** Shows or hides a delegating route's delegated routes. */
  onDelegated?: () => void;
  /** The map is focused on this node. */
  focused: boolean;
  /** Focuses the map on this node, or clears the focus. */
  onFocus: (on: boolean) => void;
  stylesOverride?: SerializedStyles;
}

/**
 * A map node's state, the paths through it by state, its own problems, a backend's endpoints, and links to its
 * detail and to Traffic (design §6.1). A node's state names only its own signals, plus a route's backend problems
 * (D-082), so the path counts say what fails through it.
 */
export function SidePanel({
  installId,
  gateway,
  node,
  rows,
  takeFocus,
  onClose,
  onRules,
  onDelegated,
  focused,
  onFocus,
  stylesOverride,
}: SidePanelProps) {
  const self = useRef<HTMLElement>(null);
  useEffect(() => {
    if (takeFocus) self.current?.focus();
  }, [takeFocus]);
  const stale = useContext(StaleView);
  const total = node.rows.length;
  const counts: Record<HealthState, number> = { error: 0, warning: 0, unknown: 0, healthy: 0 };
  for (const i of node.rows) {
    const s = rows[i]?.problem.state;
    if (s) counts[stale ? "unknown" : s]++;
  }
  // The fail line shows whenever a path has a problem; a stale view shows every path unknown instead.
  const lines = ORDER.filter((s) => counts[s] > 0 || (s === "error" && !stale));
  const detail = detailLink(installId, gateway, node);
  const ports = node.kind === "backend" ? endpointsByPort(node, rows) : [];
  return (
    <Panel ref={self} tabIndex={-1} aria-label={mapCopy.panel} data-testid="map-panel" stylesOverride={stylesOverride}>
      <header>
        <div>
          <div className="kind">
            {kindLabel(node)}
            {node.delegatedUnder && ` · ${cellCopy.delegated}`}
            {node.hiddenRoutes ? ` · ${mapCopy.hiddenDelegated(node.hiddenRoutes)}` : null}
          </div>
          <h2>{node.name}</h2>
          <HealthBadge state={node.state} />
        </div>
        <Close type="button" aria-label={mapCopy.close} onClick={onClose}>
          <X size={16} aria-hidden="true" />
        </Close>
      </header>
      <div className="actions">
        {node.delegates && onDelegated ? (
          <TextButton type="button" onClick={onDelegated}>
            {node.collapsed ? mapCopy.showDelegated : mapCopy.hideDelegated}
          </TextButton>
        ) : null}
        {node.ends && onRules ? (
          <TextButton type="button" onClick={onRules}>
            {node.expanded ? mapCopy.hideRules : mapCopy.showRules}
          </TextButton>
        ) : null}
        <TextButton type="button" onClick={() => onFocus(!focused)}>
          {focused ? mapCopy.clearFocus : mapCopy.focus}
        </TextButton>
      </div>
      {total > 0 ? (
        <ul className="counts" data-testid="map-panel-counts">
          {counts.healthy === total ? (
            <li>
              <HealthIcon state="healthy" size={14} />
              {total === 1 ? mapCopy.healthyOne : mapCopy.healthy(total)}
            </li>
          ) : (
            lines.map((s) => (
              <li key={s}>
                <HealthIcon state={s} size={14} />
                {countLine[s](counts[s], total)}
              </li>
            ))
          )}
        </ul>
      ) : null}
      {stale ? null : (
        <>
          <h3>{mapCopy.problems}</h3>
          {node.problems.length || node.backendProblems?.length ? (
            <ul data-testid="map-panel-problems">
              {node.problems.map((s) => {
                const text = signalText(s);
                return (
                  <li key={`${s.kind}|${text}`}>
                    <HealthIcon state={s.state} size={14} />
                    {text}
                  </li>
                );
              })}
              {/* A backend's finding makes the route warning (D-082). */}
              {node.backendProblems?.map((s) => {
                const text = `${routeStateBackendCopy["backend-problem"]}: ${signalText(s)}`;
                return (
                  <li key={`backend|${text}`}>
                    <HealthIcon state="warning" size={14} />
                    {text}
                  </li>
                );
              })}
            </ul>
          ) : (
            <p className="muted">{mapCopy.noProblems}</p>
          )}
          {/* A collapsed route's hidden delegated routes keep their own problems, under their names. */}
          {node.hiddenProblems?.map((h) => (
            <section key={h.id} data-testid="map-panel-hidden">
              <h4 className="route">{h.route}</h4>
              <ul>
                {h.signals.map((s) => {
                  const text = signalText(s);
                  return (
                    <li key={`${s.kind}|${text}`}>
                      <HealthIcon state={s.state} size={14} />
                      {text}
                    </li>
                  );
                })}
              </ul>
            </section>
          ))}
        </>
      )}
      {ports.length ? (
        <>
          <h3>{mapCopy.endpoints}</h3>
          <ul data-testid="map-panel-endpoints">
            {ports.map(([port, e]) => (
              <li key={port ?? "-"}>
                {port !== undefined ? `${mapCopy.port(port)} ` : ""}
                {endpointsText(e)}
              </li>
            ))}
          </ul>
        </>
      ) : null}
      <ul className="links">
        {detail ? (
          <li>
            <InlineLink to={detail}>{mapCopy.open}</InlineLink>
          </li>
        ) : null}
        {total > 0 ? (
          <li>
            <InlineLink to={trafficLink(installId, gateway, node, rows)}>{mapCopy.traffic}</InlineLink>
          </li>
        ) : null}
      </ul>
    </Panel>
  );
}
