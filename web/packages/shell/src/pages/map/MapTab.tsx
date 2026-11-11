import { css, type SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import type { LinkedRef, Page, PathRow, PathSummary } from "../../api/types";
import { listCopy } from "../../copy/explorer";
import { mapCopy } from "../../copy/map";
import { summaryCopy, trafficCopy } from "../../copy/traffic";
import type { ParamChanges } from "../../shell/urlParams";
import { PageState } from "../../states/PageState";
import { Shimmer } from "../../states/Shimmer";
import { PROBLEM_STATES } from "../backends/BackendsPage";
import { PathTable } from "../traffic/PathTable";
import { StaleGate } from "../traffic/StaleGate";
import { gatewayGraph, type MapNode } from "./graph";
import { MapToolbar, problemsOnly } from "./MapToolbar";
import { MapBox, MapNote } from "./map.css";
import { LARGE_GATEWAY, MAP_MAX_ROWS, useMapPaths } from "./mapData";
import { SidePanel } from "./SidePanel";

// region Styles
const fill = css`
  height: 100%;
  border-radius: 0;
`;
/** The canvas, and the side panel in its own column when open, so the canvas shrinks rather than hides under it. */
const Area = styled.div<{ $panel: boolean }>`
  display: grid;
  grid-template-columns: ${(p) => (p.$panel ? "minmax(0, 1fr) min(340px, 40%)" : "minmax(0, 1fr)")};
  gap: ${(p) => p.theme.space(2)};
`;
/** The notes line keeps one line's height, so a note appearing moves nothing below it. */
const Notes = styled(MapNote)`
  min-height: 1.5em;
  span + span::before {
    content: " ";
  }
`;

// region Helpers
// React Flow loads with the map only, as its own chunk (design §6.2); MapTab starts it on mount, beside the first page.
const load = () => import("./GatewayMap");
const GatewayMap = lazy(load);

/** Holds the map's place while its chunk or its first page loads. */
function Fallback() {
  return (
    <MapBox aria-busy="true" aria-label={mapCopy.loading} data-testid="map-fallback">
      <Shimmer stylesOverride={fill} />
    </MapBox>
  );
}

/** The nodes whose name contains text, ignoring case. */
const matches = (nodes: MapNode[], text: string) => {
  const t = text.trim().toLowerCase();
  return t ? nodes.filter((n) => n.name.toLowerCase().includes(t)) : [];
};

/** More routes or backends than LARGE_GATEWAY in the whole result. */
const isLarge = (s: PathSummary) => s.routes > LARGE_GATEWAY || s.backends > LARGE_GATEWAY;

/** The table view's fixed order: the rows as the map loaded them, problems first. */
const NO_SORT = { key: "problem", desc: false };

/** pending until the first summary; a large Gateway's counts, applied once the URL carries Problems only. */
type Opening = "pending" | "done" | { routes: number; backends: number; applied: boolean };

// region Component
interface MapTabProps {
  installId: string;
  gateway: LinkedRef;
  stylesOverride?: SerializedStyles;
}

/**
 * Gateway detail's Map tab: the paths through the Gateway under the URL's filters, drawn as a map (design §6),
 * with find, filter chips and a side panel for the selected node, or as a table of the same rows.
 * A large Gateway opens on problems only.
 */
export function MapTab({ installId, gateway, stylesOverride }: MapTabProps) {
  const { search } = useLocation();
  const navigate = useNavigate();
  const params = useMemo(() => new URLSearchParams(search), [search]);
  // A large Gateway, by the first page's summary of the whole result, opens on problems only (design §6.1).
  const [opening, setOpening] = useState<Opening>(() => (params.has("state") ? "done" : "pending"));
  const paths = useMapPaths(
    installId,
    `${gateway.namespace ?? ""}/${gateway.name}`,
    params,
    // Until the URL carries Problems only, a large Gateway's unfiltered walk stops after page one.
    opening === "pending" || (typeof opening === "object" && !opening.applied) ? isLarge : undefined,
  );
  useEffect(() => {
    void load();
  }, []);
  // The detail page owns the URL params and adopts a replace it did not write, as it does a link.
  const setFilter = useCallback(
    (changes: ParamChanges) => {
      const q = new URLSearchParams(search);
      for (const [k, v] of Object.entries(changes)) {
        if (v) q.set(k, v);
        else q.delete(k);
      }
      const s = q.toString();
      if (s !== new URLSearchParams(search).toString()) navigate({ search: s ? `?${s}` : "" }, { replace: true });
    },
    [search, navigate],
  );

  const summary = paths.summary;
  const large = opening === "pending" && !!summary && isLarge(summary);
  useEffect(() => {
    if (typeof opening === "object" && !opening.applied && params.has("state"))
      setOpening({ ...opening, applied: true });
    if (opening !== "pending" || !summary) return;
    if (large) {
      setOpening({ routes: summary.routes, backends: summary.backends, applied: false });
      setFilter({ state: PROBLEM_STATES });
    } else setOpening("done");
  }, [opening, summary, large, setFilter, params]);
  const prompt = typeof opening === "object" && problemsOnly(params) ? opening : undefined;

  const graph = useMemo(() => gatewayGraph(gateway, paths.rows), [gateway, paths.rows]);
  // ?view=table shows the map's rows as a path table, the map's accessible equivalent (design §6.1, item 8).
  const table = params.get("view") === "table";
  const page = useMemo<Page<PathRow> | undefined>(
    () =>
      paths.collectedAt
        ? { install: installId, items: paths.rows, total: paths.rows.length, collectedAt: paths.collectedAt }
        : undefined,
    [installId, paths.rows, paths.collectedAt],
  );
  const filtered = ["state", "kind", "namespace", "q"].some((k) => params.has(k));
  const [selected, setSelected] = useState<string>();
  // A selection from the map moves focus into the panel; one from the find box leaves it in the box.
  const [panelFocus, setPanelFocus] = useState(false);
  const node = selected ? graph.nodes.find((n) => n.id === selected) : undefined;
  const area = useRef<HTMLDivElement>(null);
  const onSelect = useCallback((id?: string) => {
    setSelected(id);
    setPanelFocus(!!id);
  }, []);
  // Closing returns focus to the node when it was in the panel.
  const close = () => {
    const back = area.current?.querySelector("[data-testid=map-panel]")?.contains(document.activeElement);
    const id = selected;
    setSelected(undefined);
    if (back && id)
      requestAnimationFrame(() =>
        area.current?.querySelector<HTMLElement>(`.react-flow__node[data-id="${CSS.escape(id)}"]`)?.focus(),
      );
  };
  // Find: typing centres and selects the first match; Enter moves to the next.
  const [find, setFind] = useState("");
  const [at, setAt] = useState(0);
  const [focus, setFocus] = useState<{ id: string; seq: number }>();
  const found = useMemo(() => matches(graph.nodes, find), [graph.nodes, find]);
  const goTo = (list: MapNode[], k: number) => {
    const n = list[k];
    if (!n) return;
    setSelected(n.id);
    setPanelFocus(false);
    setFocus((f) => ({ id: n.id, seq: (f?.seq ?? 0) + 1 }));
    setAt(k + 1);
  };
  const onFind = (text: string) => {
    setFind(text);
    setAt(0);
    goTo(matches(graph.nodes, text), 0);
  };
  const onFindNext = () => goTo(found, at % Math.max(found.length, 1));

  if (paths.error && !summary)
    return (
      <PageState
        kind="error"
        title={listCopy.cannotLoad(trafficCopy.noun[1])}
        message={paths.error.message}
        copyText={paths.error.message}
      />
    );
  if (summary?.notComputed)
    return <PageState kind="empty" title={summaryCopy.notComputedTitle} message={summaryCopy.notComputed} />;
  return (
    <StaleGate collectedAt={paths.collectedAt} failed={!!paths.error}>
      <MapToolbar
        params={params}
        onFilter={setFilter}
        facets={summary?.facets}
        find={find}
        onFind={onFind}
        onFindNext={onFindNext}
        found={{ at: Math.max(at, 1), of: found.length }}
        table={table}
        onTable={(on) => setFilter({ view: on ? "table" : null })}
      />
      <Notes role="status" data-testid="map-notes">
        {prompt ? <span data-testid="map-large">{mapCopy.large(prompt.routes, prompt.backends)}</span> : null}
        {paths.truncated && summary ? (
          <span data-testid="map-truncated">{mapCopy.truncated(MAP_MAX_ROWS, summary.paths)}</span>
        ) : paths.partial && summary ? (
          // "Loading the rest" only while a walk runs; a walk that ended short says what it shows.
          <span data-testid="map-partial">
            {(paths.walking ? mapCopy.partial : mapCopy.shown)(paths.rows.length, summary.paths)}
          </span>
        ) : null}
        {paths.inconsistent ? <span data-testid="map-inconsistent">{mapCopy.inconsistent}</span> : null}
      </Notes>
      {!summary || large ? (
        <Fallback />
      ) : table ? (
        <PathTable
          installId={installId}
          label={mapCopy.tableLabel}
          page={page}
          busy={false}
          sort={NO_SORT}
          onSort={() => {}}
          onPage={() => {}}
          emptyTitle={filtered ? listCopy.emptyFiltered(trafficCopy.noun[1]) : listCopy.empty(trafficCopy.noun[1])}
          sortable={false}
        />
      ) : (
        <Area
          ref={area}
          $panel={!!node}
          onKeyDown={(e) => {
            if (e.key === "Escape" && node) close();
          }}
        >
          <Suspense fallback={<Fallback />}>
            <GatewayMap
              graph={graph}
              selected={node?.id}
              onSelect={onSelect}
              focus={focus}
              view={paths.query ?? ""}
              stylesOverride={stylesOverride}
            />
          </Suspense>
          {node ? (
            <SidePanel
              key={node.id}
              installId={installId}
              gateway={gateway}
              node={node}
              rows={paths.rows}
              takeFocus={panelFocus}
              onClose={close}
            />
          ) : null}
        </Area>
      )}
    </StaleGate>
  );
}
