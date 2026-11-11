import type { SerializedStyles } from "@emotion/react";
import { useMemo } from "react";
import type { LinkedRef, Page, PathRow } from "../../api/types";
import { columnCopy } from "../../copy/explorer";
import { cellCopy, endpointCopy, noteCopy, pathColumnCopy, trafficCopy } from "../../copy/traffic";
import type { SortState } from "../../explore/listQuery";
import { type ObjectColumn, ObjectTable } from "../../explore/ObjectTable";
import { StaleView } from "../../health/staleView";
import { Muted, OneLine } from "../explore/listParts";
import { Code, GatewayCell, RouteCell, ShareCell, StateCell, TargetCell } from "./PathCells";
import { baseKey, endpointText, keyRows, matchSummary, signalLines } from "./pathText";

// region Helpers
function columns(installId: string): ObjectColumn<PathRow>[] {
  return [
    {
      id: "state",
      header: columnCopy.status,
      width: "112px",
      sortKey: "problem",
      cell: (r) => <StateCell r={r} />,
    },
    {
      id: "gateway",
      header: pathColumnCopy.gateway,
      width: "13%",
      sortKey: "gateway",
      cell: (r) => <GatewayCell r={r} installId={installId} />,
    },
    {
      id: "hostnames",
      header: pathColumnCopy.hostnames,
      width: "11%",
      sortKey: "hostname",
      secondary: true,
      cell: (r) =>
        r.hostnames.length ? <OneLine text={r.hostnames.join(", ")} /> : <Muted>{cellCopy.noHostnames}</Muted>,
    },
    {
      id: "name",
      header: pathColumnCopy.route,
      sortKey: "route",
      cell: (r) => <RouteCell r={r} installId={installId} />,
    },
    {
      id: "match",
      header: pathColumnCopy.match,
      width: "13%",
      secondary: true,
      cell: (r) => {
        const m = matchSummary(r);
        return (
          <OneLine text={r.note ? `${m.full}\n${noteCopy[r.note]}` : m.full}>
            {m.unknown ? <Muted>{m.text}</Muted> : <Code>{m.text}</Code>}
          </OneLine>
        );
      },
    },
    {
      id: "target",
      header: pathColumnCopy.target,
      width: "14%",
      sortKey: "backend",
      cell: (r) => <TargetCell r={r} installId={installId} />,
    },
    {
      id: "share",
      header: pathColumnCopy.share,
      headerExtra: <Muted>{pathColumnCopy.shareHint}</Muted>,
      width: "12%",
      cell: (r) => <ShareCell r={r} />,
    },
    {
      id: "endpoints",
      header: pathColumnCopy.endpoints,
      width: "120px",
      secondary: true,
      cell: (r) => {
        const t = endpointText(r);
        return t ? (
          <OneLine text={t} />
        ) : (
          <OneLine text={endpointCopy.notApplicable}>
            <Muted>{endpointCopy.notApplicable}</Muted>
          </OneLine>
        );
      },
    },
    {
      id: "why",
      header: pathColumnCopy.why,
      width: "11%",
      secondary: true,
      cell: (r) => {
        const lines = signalLines(r);
        const all = r.note ? [...lines, noteCopy[r.note]] : lines;
        return all.length ? <OneLine text={all.join("\n")} placement="end" /> : null;
      },
    },
  ];
}

// region Component
interface PathTableProps {
  installId: string;
  page?: Page<PathRow>;
  /** Every state shows unknown: the data is stale or its last poll failed. */
  stale: boolean;
  busy: boolean;
  onClearFilters?: () => void;
  sort: SortState;
  onSort: (key: string) => void;
  onPage: (cursor: string) => void;
  emptyTitle: string;
  stylesOverride?: SerializedStyles;
}

/** One row per path, problems first by default: Gateway → listener → route → rule → backend (design §4.1). */
export function PathTable({
  installId,
  page,
  stale,
  busy,
  onClearFilters,
  sort,
  onSort,
  onPage,
  emptyTitle,
  stylesOverride,
}: PathTableProps) {
  const cols = useMemo(() => columns(installId), [installId]);
  const keys = useMemo(() => keyRows(page?.items ?? []), [page]);
  return (
    <StaleView.Provider value={stale}>
      <ObjectTable
        label={trafficCopy.table}
        installId={installId}
        noun={trafficCopy.noun}
        columns={cols}
        rowRef={rowRef}
        rowKey={(r) => keys.get(r) ?? baseKey(r)}
        page={page}
        loading={!page}
        busy={busy}
        emptyTitle={emptyTitle}
        onClearFilters={onClearFilters}
        sort={sort}
        onSort={onSort}
        onPage={onPage}
        showDenied={false}
        stylesOverride={stylesOverride}
      />
    </StaleView.Provider>
  );
}

/** A row opens the route whose rule it is on. */
const rowRef = (r: PathRow) => r.routes[r.routes.length - 1] as LinkedRef;
