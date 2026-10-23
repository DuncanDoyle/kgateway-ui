import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { ArrowDown, ArrowUp } from "lucide-react";
import { type MouseEvent, memo, useDeferredValue, useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import type { Finding, GatewayRow, Ref } from "../../api/types";
import { objectPath } from "../../explore/objectPath";
import { HealthBadge } from "../../health/HealthBadge";
import { ResourceName } from "../../resource/ResourceName";
import { useUrlParams, useUrlText } from "../../shell/urlParams";
import type { HealthState } from "../../theme/tokens";

// region Styles
/** Filter input height, shared with the loading skeleton. */
export const FILTER_H = 32;

const Filter = styled.input`
  box-sizing: border-box;
  display: block;
  height: ${FILTER_H}px;
  width: 300px;
  max-width: 100%;
  margin-bottom: ${(p) => p.theme.space(2)};
  padding: 0 10px;
  font: inherit;
  font-size: ${(p) => p.theme.font.size.base};
  color: ${(p) => p.theme.color.text};
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.sm};
  &::placeholder {
    color: ${(p) => p.theme.color.textMuted};
  }
  &:hover {
    border-color: ${(p) => p.theme.color.text};
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 1px;
  }
`;
const Scroll = styled.div<{ stylesOverride?: SerializedStyles }>`
  overflow-x: auto;
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.surface};
  ${(p) => p.stylesOverride}
`;
const Table = styled.table`
  width: 100%;
  border-collapse: collapse;
  th {
    text-align: left;
    white-space: nowrap;
    font-size: ${(p) => p.theme.font.size.xs};
    letter-spacing: 0.06em;
    text-transform: uppercase;
    font-weight: ${(p) => p.theme.font.weight.semibold};
    color: ${(p) => p.theme.color.textMuted};
    padding: 8px 12px;
    border-bottom: 1px solid ${(p) => p.theme.color.border};
  }
  th button {
    all: unset;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    cursor: pointer;
    padding: 2px 4px;
    margin: -2px -4px;
    border-radius: ${(p) => p.theme.radius.sm};
  }
  th button:hover {
    color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.surface});
  }
  th button:active {
    color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
  th button:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
  }
  td {
    padding: 8px 12px;
    white-space: nowrap;
    border-bottom: 1px solid ${(p) => p.theme.color.border};
    font-variant-numeric: tabular-nums;
  }
  tbody tr[data-linked] {
    cursor: pointer;
  }
  tbody tr[data-linked]:hover {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 5%, ${(p) => p.theme.color.surface});
  }
  tbody tr[data-linked]:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 8%, ${(p) => p.theme.color.surface});
  }
  tbody tr:last-of-type td {
    border-bottom: 0;
  }
  a {
    color: inherit;
    text-decoration: none;
    border-radius: ${(p) => p.theme.radius.sm};
  }
  tr:hover a [data-part="name"],
  a:hover [data-part="name"] {
    text-decoration: underline;
  }
  a:active [data-part="name"] {
    color: ${(p) => p.theme.color.accentText};
  }
  a:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
  .none {
    color: ${(p) => p.theme.color.textMuted};
  }
`;
const Empty = styled.p`
  margin: 0;
  padding: ${(p) => p.theme.space(3)};
  color: ${(p) => p.theme.color.textMuted};
`;

// region Helpers
const col = createColumnHelper<GatewayRow>();
const rank = { error: 0, unknown: 1, warning: 2, healthy: 3 } as const;
const noFindings: Finding[] = [];
const gatewayKey = (r: GatewayRow) => `${r.ref.namespace ?? ""}/${r.ref.name}`;
/** Where a row opens, from its server-computed link only; an older backend sends none, so the row is text. */
const rowPath = (installId: string, r: GatewayRow) => objectPath(installId, { ...r.ref, link: r.link ?? "none" });

/** Rows whose name, namespace or class contains q (case-insensitive). */
function filterRows(rows: GatewayRow[], q: string): GatewayRow[] {
  const needle = q.trim().toLowerCase();
  if (!needle) return rows;
  return rows.filter((r) => [r.ref.name, r.ref.namespace ?? "", r.class].some((v) => v.toLowerCase().includes(needle)));
}

const refKey = (r: Pick<Ref, "group" | "kind" | "namespace" | "name">) =>
  [r.group, r.kind, r.namespace ?? "", r.name].join("|");

/** The worst error or warning per resource among the current health findings; info does not change a row. */
function findingStates(findings: Finding[]): Map<string, HealthState> {
  const worst = new Map<string, HealthState>();
  for (const f of findings) {
    if (!f.resource || f.severity === "info") continue;
    const k = refKey(f.resource);
    const prev = worst.get(k);
    if (!prev || rank[f.severity] < rank[prev]) worst.set(k, f.severity);
  }
  return worst;
}

/**
 * The state a row shows: the worse of its overview state and its current health findings, so the table agrees
 * with the findings above it. Unknown on stale data or a missing required signal (unless it has an error), as
 * the backend's rowState (D-006).
 */
function shownState(
  r: GatewayRow,
  found: Map<string, HealthState>,
  stale: boolean,
  signalsMissing: boolean,
): HealthState {
  if (stale) return "unknown";
  const f = found.get(refKey(r.ref));
  const state = f && rank[f] < rank[r.state] ? f : r.state;
  return signalsMissing && state !== "error" ? "unknown" : state;
}

function columns(installId: string) {
  return [
    col.accessor("state", {
      header: "Status",
      cell: (c) => <HealthBadge state={c.getValue()} />,
      sortingFn: (a, b) => rank[a.original.state] - rank[b.original.state],
    }),
    col.accessor(gatewayKey, {
      id: "gateway",
      header: "Gateway",
      cell: (c) => {
        const { ref } = c.row.original;
        const to = rowPath(installId, c.row.original);
        const name = <ResourceName namespace={ref.namespace} name={ref.name} />;
        return to ? <Link to={to}>{name}</Link> : name;
      },
    }),
    col.accessor("class", { header: "Class" }),
    col.accessor("listeners", { header: "Listeners" }),
    col.accessor("attachedRoutes", { id: "routes", header: "Routes" }),
    col.accessor((r) => r.addresses.join(", "), {
      id: "addresses",
      header: "Addresses",
      enableSorting: false,
      cell: (c) => c.getValue() || <span className="none">—</span>,
    }),
  ];
}

// region Component
interface GatewayTableProps {
  rows: GatewayRow[];
  /** The install's current health findings; a Gateway's errors and warnings fold into its row. */
  findings?: Finding[];
  installId: string;
  stale: boolean;
  /** A required check is not OK: non-error rows show unknown. */
  signalsMissing: boolean;
  stylesOverride?: SerializedStyles;
}

/** Gateways of the install; the text filter and the sort are kept in the URL (?q=edge&sort=routes.desc). Memoised so the page's 1 s clock tick does not re-render it. */
export const GatewayTable = memo(function GatewayTable({
  rows,
  findings = noFindings,
  installId,
  stale,
  signalsMissing,
  stylesOverride,
}: GatewayTableProps) {
  const [params, update] = useUrlParams();
  const navigate = useNavigate();
  const [text, setText] = useUrlText(params, update, "q");
  const q = useDeferredValue(text);
  // The shown state goes into the row data: TanStack caches cell values per row, so a closure would not refresh them.
  const found = useMemo(() => findingStates(findings), [findings]);
  const visible = useMemo(
    () => filterRows(rows, q).map((r) => ({ ...r, state: shownState(r, found, stale, signalsMissing) })),
    [rows, q, found, stale, signalsMissing],
  );
  const sort = params.get("sort") ?? "state.asc";
  // A new sorting array on every render re-runs the table's auto-resets, which re-render it forever.
  const sorting = useMemo<SortingState>(() => {
    const [id, dir] = sort.split(".");
    return [{ id: id || "state", desc: dir === "desc" }];
  }, [sort]);
  const cols = useMemo(() => columns(installId), [installId]);
  const table = useReactTable({
    data: visible,
    columns: cols,
    state: { sorting },
    getRowId: gatewayKey,
    enableSortingRemoval: false,
    // No pagination or expansion: their auto-resets would only add state updates.
    autoResetAll: false,
    onSortingChange: (u) => {
      const s = (typeof u === "function" ? u(sorting) : u)[0];
      if (s) update({ sort: `${s.id}.${s.desc ? "desc" : "asc"}` });
    },
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  });
  // A click anywhere on a row opens the Gateway; the name link is the keyboard path.
  const openRow = (e: MouseEvent, r: GatewayRow) => {
    const to = rowPath(installId, r);
    if (!to || (e.target as HTMLElement).closest("a, button")) return;
    navigate(to);
  };
  return (
    <>
      <Filter
        aria-label="Filter Gateways"
        autoComplete="off"
        placeholder="Filter by name, namespace or class"
        value={text}
        onChange={(e) => setText(e.target.value)}
      />
      <Scroll stylesOverride={stylesOverride}>
        <Table aria-label="Gateways">
          <thead>
            {table.getHeaderGroups().map((hg) => (
              <tr key={hg.id}>
                {hg.headers.map((h) => {
                  const sorted = h.column.getIsSorted();
                  return (
                    <th
                      key={h.id}
                      aria-sort={sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : undefined}
                    >
                      {h.column.getCanSort() ? (
                        <button type="button" onClick={h.column.getToggleSortingHandler()}>
                          {flexRender(h.column.columnDef.header, h.getContext())}
                          {sorted === "asc" ? <ArrowUp size={12} aria-hidden="true" /> : null}
                          {sorted === "desc" ? <ArrowDown size={12} aria-hidden="true" /> : null}
                        </button>
                      ) : (
                        flexRender(h.column.columnDef.header, h.getContext())
                      )}
                    </th>
                  );
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {table.getRowModel().rows.map((r) => (
              <tr
                key={r.id}
                data-linked={rowPath(installId, r.original) ? "" : undefined}
                onClick={(e) => openRow(e, r.original)}
              >
                {r.getVisibleCells().map((c) => (
                  <td key={c.id}>{flexRender(c.column.columnDef.cell, c.getContext())}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </Table>
        {visible.length === 0 ? (
          <Empty>{rows.length === 0 ? "No Gateways in this install." : "No Gateways match the filter."}</Empty>
        ) : null}
      </Scroll>
    </>
  );
});
