import { css, type SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import type { LinkedRef, Page, PathRow, PathSummary } from "../../api/types";
import { listCopy } from "../../copy/explorer";
import { mapCopy } from "../../copy/map";
import { summaryCopy, trafficCopy } from "../../copy/traffic";
import type { ParamChanges } from "../../shell/urlParams";
import { QuietButton } from "../../states/controls";
import { PageState } from "../../states/PageState";
import { Shimmer } from "../../states/Shimmer";
import { PROBLEM_STATES } from "../backends/BackendsPage";
import { PathTable } from "../traffic/PathTable";
import { StaleGate } from "../traffic/StaleGate";
import { focusSubgraph } from "./focus";
import { gatewayGraph, kindLabel, type MapNode } from "./graph";
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
  button {
    height: auto;
    padding: 0 4px;
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

  // ?expand= opens routes into rules, ?collapse= hides a route's delegated routes (both repeatable), and ?focus=
  // keeps one node's paths (design §6.1).
  const expandParam = params.getAll("expand").join("\n");
  const collapseParam = params.getAll("collapse").join("\n");
  const expanded = useMemo(() => new Set(expandParam ? expandParam.split("\n") : []), [expandParam]);
  const collapsed = useMemo(() => new Set(collapseParam ? collapseParam.split("\n") : []), [collapseParam]);
  const focusId = params.get("focus") ?? undefined;
  // Under a state filter the healthy rows are not loaded, so no edge can be known to be broken. The rows answer
  // paths.query, which lags the URL until a new filter's rows arrive.
  const stateFiltered = new URLSearchParams(paths.query?.split("?")[1] ?? "").has("state");
  const full = useMemo(
    () => gatewayGraph(gateway, paths.rows, expanded, { stateFiltered, collapsed }),
    [gateway, paths.rows, expanded, stateFiltered, collapsed],
  );
  // Every route, none collapsed: what find searches, and what expand and collapse ids are checked against.
  const open = useMemo(
    () => gatewayGraph(gateway, paths.rows, expanded, { stateFiltered }),
    [gateway, paths.rows, expanded, stateFiltered],
  );
  const graph = useMemo(() => (focusId ? focusSubgraph(full, focusId) : full), [full, focusId]);
  const focusNode = focusId ? graph.nodes.find((n) => n.id === focusId) : undefined;
  // Expand, collapse and focus are views the user steps through, so each change is a history entry.
  const setView = useCallback(
    (change: (q: URLSearchParams) => void, replace = false) => {
      const q = new URLSearchParams(search);
      change(q);
      const s = q.toString();
      if (s !== new URLSearchParams(search).toString()) navigate({ search: s ? `?${s}` : "" }, { replace });
    },
    [search, navigate],
  );
  // A route that delegates opens and closes its delegated routes; one that delegates none, its rules.
  // Collapsing a route drops the expand and collapse ids of the routes under it.
  const onToggle = useCallback(
    (id: string) => {
      const n = full.nodes.find((x) => x.id === id);
      const key = n?.delegates ? "collapse" : "expand";
      setView((q) => {
        const all = q.getAll(key);
        const adding = !all.includes(id);
        q.delete(key);
        for (const x of all) if (x !== id) q.append(key, x);
        if (adding) q.append(key, id);
        if (adding && key === "collapse")
          for (const k of ["expand", "collapse"]) {
            const keep = q.getAll(k).filter((x) => !x.startsWith(`${id}~`));
            q.delete(k);
            for (const x of keep) q.append(k, x);
          }
      });
    },
    [full, setView],
  );
  const onRules = (id: string) =>
    setView((q) => {
      const all = q.getAll("expand");
      q.delete("expand");
      for (const x of all) if (x !== id) q.append("expand", x);
      if (!all.includes(id)) q.append("expand", id);
    });
  // Once every row of an unfiltered result has arrived, ids of routes no longer on it are dropped.
  const unfiltered = !["state", "kind", "namespace", "q"].some((k) => params.has(k));
  const stale = useMemo(() => {
    if (!paths.done || !unfiltered) return [];
    const ids = new Set(open.nodes.map((n) => n.id));
    return [...expanded, ...collapsed].filter((id) => !ids.has(id));
  }, [paths.done, unfiltered, open, expanded, collapsed]);
  useEffect(() => {
    if (!stale.length) return;
    setView((q) => {
      for (const k of ["expand", "collapse"]) {
        const keep = q.getAll(k).filter((x) => !stale.includes(x));
        q.delete(k);
        for (const x of keep) q.append(k, x);
      }
    }, true);
  }, [stale, setView]);
  const setFocusNode = (id?: string) => setView((q) => (id ? q.set("focus", id) : q.delete("focus")));
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
  // Clearing the focus from the notes line moves keyboard focus to the map's tab stop once it is drawn again.
  const clearFocus = () => {
    setFocusNode(undefined);
    let tries = 10;
    const next = () => {
      const el = area.current?.querySelector<HTMLElement>('.react-flow__node[tabindex="0"]');
      if (el) el.focus();
      else if (tries-- > 0) requestAnimationFrame(next);
    };
    requestAnimationFrame(next);
  };
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
  const [centre, setCentre] = useState<{ id: string; seq: number }>();
  // Find searches every route, including those a collapsed parent hides, within the focus when one is set.
  const findable = useMemo(() => (focusId ? focusSubgraph(open, focusId) : open).nodes, [open, focusId]);
  const found = useMemo(() => matches(findable, find), [findable, find]);
  const goTo = (list: MapNode[], k: number) => {
    const n = list[k];
    if (!n) return;
    // A hidden route is shown first: every route above it leaves collapse.
    if (n.kind === "route" && !graph.nodes.some((x) => x.id === n.id)) {
      const parts = n.id.slice(2).split("~");
      const above = parts.slice(1).map((_, i) => `r:${parts.slice(0, i + 1).join("~")}`);
      setView((q) => {
        const keep = q.getAll("collapse").filter((x) => !above.includes(x));
        q.delete("collapse");
        for (const x of keep) q.append("collapse", x);
      });
    }
    setSelected(n.id);
    setPanelFocus(false);
    setCentre((f) => ({ id: n.id, seq: (f?.seq ?? 0) + 1 }));
    setAt(k + 1);
  };
  const onFind = (text: string) => {
    setFind(text);
    setAt(0);
    goTo(matches(findable, text), 0);
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
        {focusId ? (
          <span data-testid="map-focus">
            {focusNode ? `${mapCopy.focused(kindLabel(focusNode), focusNode.name)} ` : `${mapCopy.focusGone} `}
            <QuietButton type="button" onClick={clearFocus}>
              {mapCopy.clearFocus}
            </QuietButton>
          </span>
        ) : null}
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
            // Escape closes the panel first, then leaves focus mode.
            if (e.key === "Escape" && node) close();
            else if (e.key === "Escape" && focusId) clearFocus();
          }}
        >
          <Suspense fallback={<Fallback />}>
            <GatewayMap
              graph={graph}
              selected={node?.id}
              onSelect={onSelect}
              centre={centre}
              view={`${paths.query ?? ""}|${focusId ?? ""}`}
              onToggle={onToggle}
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
              onRules={() => onRules(node.id)}
              onDelegated={() => onToggle(node.id)}
              focused={node.id === focusId}
              onFocus={(on) => setFocusNode(on ? node.id : undefined)}
            />
          ) : null}
        </Area>
      )}
    </StaleGate>
  );
}
