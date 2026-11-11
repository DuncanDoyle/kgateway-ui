import type { SerializedStyles } from "@emotion/react";
import { useCallback, useMemo } from "react";
import type { LinkedRef, Page, PathRow } from "../../api/types";
import { columnCopy } from "../../copy/explorer";
import { cellCopy, endpointCopy, noteCopy, pathColumnCopy, trafficCopy } from "../../copy/traffic";
import type { SortState } from "../../explore/listQuery";
import { type ObjectColumn, ObjectTable } from "../../explore/ObjectTable";
import { Muted, OneLine } from "../explore/listParts";
import { Code, GatewayCell, RouteCell, ShareCell, StateCell, TargetCell, WhyCell } from "./PathCells";
import { baseKey, endpointText, keyRows, matchSummary } from "./pathText";

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
      cell: (r) => <WhyCell r={r} />,
    },
  ];
}

// region Component
interface PathTableProps {
  installId: string;
  /** The table's accessible name; Traffic's by default. */
  label?: string;
  page?: Page<PathRow>;
  busy: boolean;
  onClearFilters?: () => void;
  sort: SortState;
  onSort: (key: string) => void;
  onPage: (cursor: string) => void;
  emptyTitle: string;
  /** Column ids left out, e.g. the backend's own column on its detail page. */
  omit?: readonly string[];
  /** False for a fixed order, as on backend detail: no header sorts, and an empty footer collapses. */
  sortable?: boolean;
  stylesOverride?: SerializedStyles;
}

/** One row per path, problems first by default: Gateway → listener → route → rule → backend (design §4.1). */
export function PathTable({
  installId,
  label = trafficCopy.table,
  page,
  busy,
  onClearFilters,
  sort,
  onSort,
  onPage,
  emptyTitle,
  omit,
  sortable = true,
  stylesOverride,
}: PathTableProps) {
  const omitKey = omit?.join(",") ?? "";
  const cols = useMemo(
    () =>
      columns(installId)
        .filter((c) => !omitKey.split(",").includes(c.id))
        .map((c) => (sortable ? c : { ...c, sortKey: undefined })),
    [installId, omitKey, sortable],
  );
  const keys = useMemo(() => keyRows(page?.items ?? []), [page]);
  // Stable between data changes: a new rowKey rebuilds the table's cell renderers, which remounts every cell.
  const rowKey = useCallback((r: PathRow) => keys.get(r) ?? baseKey(r), [keys]);
  return (
    <ObjectTable
      label={label}
      installId={installId}
      noun={trafficCopy.noun}
      columns={cols}
      rowRef={rowRef}
      rowKey={rowKey}
      page={page}
      loading={!page}
      busy={busy}
      emptyTitle={emptyTitle}
      onClearFilters={onClearFilters}
      sort={sort}
      onSort={onSort}
      onPage={onPage}
      showDenied={false}
      collapseEmptyFooter={!sortable}
      stylesOverride={stylesOverride}
    />
  );
}

/** A row opens the route whose rule it is on. */
const rowRef = (r: PathRow) => r.routes[r.routes.length - 1] as LinkedRef;
