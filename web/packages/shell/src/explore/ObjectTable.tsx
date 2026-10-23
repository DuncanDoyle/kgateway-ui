import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import {
  type ColumnDef,
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  type SortingState,
  useReactTable,
  type VisibilityState,
} from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ChevronDown, ChevronRight } from "lucide-react";
import { type KeyboardEvent, type MouseEvent, type ReactNode, useCallback, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import type { LinkedRef, Page } from "../api/types";
import { listCopy } from "../copy/explorer";
import { useMedia } from "../shell/useMedia";
import { QuietButton } from "../states/controls";
import { Shimmer } from "../states/Shimmer";
import { VisuallyHidden } from "../states/VisuallyHidden";
import type { SortState } from "./listQuery";
import { objectPath } from "./objectPath";
import { Pager } from "./Pager";

// region Styles
/** Height of a one-line row; skeleton rows use it too, so nothing shifts when data arrives. */
export const ROW_H = 44;
/** Height of the footer that holds the denied-objects line and the pager, reserved while loading. */
export const FOOTER_H = 44;
const SKELETON_ROWS = 8;
/** Below 900 px secondary columns move into an expandable row (design §8.7). */
const NARROW = "(max-width: 899px)";

const Frame = styled.div<{ stylesOverride?: SerializedStyles }>`
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.surface};
  ${(p) => p.stylesOverride}
`;
const Table = styled.table`
  width: 100%;
  table-layout: fixed;
  border-collapse: collapse;
  th {
    height: 36px;
    box-sizing: border-box;
    text-align: left;
    white-space: nowrap;
    overflow: hidden;
    font-size: ${(p) => p.theme.font.size.xs};
    letter-spacing: 0.06em;
    text-transform: uppercase;
    font-weight: ${(p) => p.theme.font.weight.semibold};
    color: ${(p) => p.theme.color.textMuted};
    padding: 0 12px;
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
    padding: 6px 12px;
    vertical-align: middle;
    overflow-wrap: anywhere;
    border-bottom: 1px solid ${(p) => p.theme.color.border};
    font-variant-numeric: tabular-nums;
  }
  /* Table rows grow to fit, so this is a minimum: a long wrapped name only adds height. */
  tbody tr[data-row],
  tbody tr[data-empty] {
    height: ${ROW_H}px;
  }
  tbody {
    transition: opacity 120ms ease-out;
  }
  tbody[data-busy] {
    opacity: 0.55;
  }
  tbody tr[data-link] {
    cursor: pointer;
  }
  tbody tr[data-row]:hover {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 5%, ${(p) => p.theme.color.surface});
  }
  tbody tr[data-link]:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 9%, ${(p) => p.theme.color.surface});
  }
  tbody tr[data-row]:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: -2px;
  }
  tbody tr:last-of-type td {
    border-bottom: 0;
  }
  /* A row click opens the row's own object, so hover underlines only its name, not other links in the row. */
  tr:hover td[data-col="name"] a [data-part="name"] {
    text-decoration: underline;
  }
  .expand {
    all: unset;
    display: inline-flex;
    padding: 4px;
    cursor: pointer;
    border-radius: ${(p) => p.theme.radius.sm};
    color: ${(p) => p.theme.color.textMuted};
  }
  .expand:hover {
    color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 8%, ${(p) => p.theme.color.surface});
  }
  .expand:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 16%, ${(p) => p.theme.color.surface});
  }
  .expand:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
  }
  dl {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 4px ${(p) => p.theme.space(3)};
    margin: 0;
    font-size: ${(p) => p.theme.font.size.sm};
  }
  dt {
    color: ${(p) => p.theme.color.textMuted};
  }
  dd {
    margin: 0;
    min-width: 0;
  }
  .none {
    color: ${(p) => p.theme.color.textMuted};
  }
`;
const Empty = styled.div`
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: ${(p) => p.theme.space(2)};
  /* No vertical padding: the empty row keeps the skeleton row's ROW_H height. */
  color: ${(p) => p.theme.color.textMuted};
`;
const Footer = styled.div`
  box-sizing: border-box;
  display: flex;
  align-items: center;
  gap: ${(p) => p.theme.space(2)};
  height: ${FOOTER_H}px;
  padding: 0 12px;
  border-top: 1px solid ${(p) => p.theme.color.border};
  font-size: ${(p) => p.theme.font.size.sm};
  color: ${(p) => p.theme.color.textMuted};
  .denied {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
`;

// region Helpers
/** One column; secondary columns move into the expandable row below 900 px. */
export interface ObjectColumn<T> {
  id: string;
  header: string;
  /** A fixed width from the first render (design §8.7), e.g. "120px" or "18%"; absent columns share the rest. */
  width?: string;
  cell: (row: T) => ReactNode;
  /** The server sort key (design §7.2); absent means the column does not sort. */
  sortKey?: string;
  secondary?: boolean;
}

const refKey = (r: LinkedRef) => [r.group, r.kind, r.namespace ?? "", r.name].join("/");
const nameOf = (r: LinkedRef) => (r.namespace ? `${r.namespace}/${r.name}` : r.name);

/** "1 route", "2 routes". */
export function count(n: number, [one, many]: readonly [string, string]): string {
  return `${n} ${n === 1 ? one : many}`;
}

// region Component
interface ObjectTableProps<T> {
  label: string;
  installId: string;
  /** Singular and plural noun of the rows, e.g. ["route", "routes"]. */
  noun: readonly [string, string];
  columns: ObjectColumn<T>[];
  rowRef: (row: T) => LinkedRef;
  page?: Page<T>;
  loading: boolean;
  /** The rows shown belong to the previous query while a new one loads: busy and dimmed, never moved. */
  busy?: boolean;
  emptyTitle: string;
  /** Set when filters apply; the empty state then offers Clear filters. */
  onClearFilters?: () => void;
  sort: SortState;
  onSort: (key: string) => void;
  onPage: (cursor: string) => void;
  /** Skeleton rows while loading; set it to the expected row count so nothing below moves. 0 reserves the empty row. */
  skeletonRows?: number;
  /** Whether the footer says how many objects the caller cannot view; one table per page should. */
  showDenied?: boolean;
  stylesOverride?: SerializedStyles;
}

/**
 * A server-sorted, server-paged list of objects. Rows open where their link says; arrow keys move between rows and
 * Enter opens one. Expanded rows are kept by object identity, so polling never collapses them.
 */
export function ObjectTable<T>({
  label,
  installId,
  noun,
  columns,
  rowRef,
  page,
  loading,
  busy = false,
  emptyTitle,
  onClearFilters,
  sort,
  onSort,
  onPage,
  skeletonRows = SKELETON_ROWS,
  showDenied = true,
  stylesOverride,
}: ObjectTableProps<T>) {
  const navigate = useNavigate();
  const narrow = useMedia(NARROW);
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set());
  const [active, setActive] = useState(0);
  const rowEls = useRef<(HTMLTableRowElement | null)[]>([]);
  const secondary = columns.filter((c) => c.secondary);
  const collapse = narrow && secondary.length > 0;
  const toggle = useCallback(
    (k: string) =>
      setExpanded((prev) => {
        const next = new Set(prev);
        if (!next.delete(k)) next.add(k);
        return next;
      }),
    [],
  );
  const defs = useMemo<ColumnDef<T>[]>(() => {
    const h = createColumnHelper<T>();
    const expand = h.display({
      id: "expand",
      cell: (c) => {
        const r = rowRef(c.row.original);
        // Read through meta, so expanding changes no column def and the button keeps its focus.
        const open = (c.table.options.meta as { expanded: ReadonlySet<string> }).expanded.has(refKey(r));
        return (
          <button
            type="button"
            className="expand"
            aria-expanded={open}
            aria-label={listCopy.toggleDetails(open, nameOf(r))}
            onClick={() => toggle(refKey(r))}
          >
            {open ? <ChevronDown size={16} aria-hidden="true" /> : <ChevronRight size={16} aria-hidden="true" />}
          </button>
        );
      },
    });
    return [
      ...(collapse ? [expand] : []),
      ...columns.map((col) => h.display({ id: col.id, header: col.header, cell: (c) => col.cell(c.row.original) })),
    ];
  }, [columns, collapse, rowRef, toggle]);
  const visibility = useMemo<VisibilityState>(
    () => Object.fromEntries(secondary.map((c) => [c.id, !collapse])),
    [secondary, collapse],
  );
  const sortedCol = columns.find((c) => c.sortKey === sort.key);
  const sorting = useMemo<SortingState>(
    () => (sortedCol ? [{ id: sortedCol.id, desc: sort.desc }] : []),
    [sortedCol, sort.desc],
  );
  const table = useReactTable({
    data: page?.items ?? [],
    columns: defs,
    state: { sorting, columnVisibility: visibility },
    meta: { expanded },
    getRowId: (r) => refKey(rowRef(r)),
    manualSorting: true,
    manualPagination: true,
    autoResetAll: false,
    getCoreRowModel: getCoreRowModel(),
  });
  const widths: Record<string, string | undefined> = { expand: "40px" };
  for (const c of columns) widths[c.id] = c.width;
  const shown = table.getVisibleLeafColumns();
  const rows = table.getRowModel().rows;
  const open = (r: T) => {
    const to = objectPath(installId, rowRef(r));
    if (to) navigate(to);
  };
  const onRowClick = (e: MouseEvent, r: T) => {
    if ((e.target as HTMLElement).closest("a, button")) return;
    open(r);
  };
  const onRowKey = (e: KeyboardEvent, i: number, r: T) => {
    if (e.target !== e.currentTarget) return;
    const to = { ArrowDown: i + 1, ArrowUp: i - 1, Home: 0, End: rows.length - 1 }[e.key];
    if (to !== undefined) {
      e.preventDefault();
      rowEls.current[Math.max(0, Math.min(rows.length - 1, to))]?.focus();
    } else if (e.key === "Enter") open(r);
  };
  const focusable = Math.min(active, Math.max(0, rows.length - 1));
  return (
    <Frame stylesOverride={stylesOverride}>
      <Table aria-label={label} aria-busy={loading || busy}>
        <colgroup>
          {shown.map((c) => (
            <col key={c.id} style={{ width: widths[c.id] }} />
          ))}
        </colgroup>
        <thead>
          <tr>
            {shown.map((c) => {
              const col = columns.find((x) => x.id === c.id);
              const s = c.getIsSorted();
              return (
                <th
                  key={c.id}
                  scope="col"
                  aria-sort={s === "asc" ? "ascending" : s === "desc" ? "descending" : undefined}
                >
                  {col?.sortKey ? (
                    <button type="button" onClick={() => col.sortKey && onSort(col.sortKey)}>
                      {col.header}
                      {s === "asc" ? <ArrowUp size={12} aria-hidden="true" /> : null}
                      {s === "desc" ? <ArrowDown size={12} aria-hidden="true" /> : null}
                    </button>
                  ) : (
                    (col?.header ?? <VisuallyHidden>{listCopy.details}</VisuallyHidden>)
                  )}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody data-busy={busy || undefined}>
          {!page && loading
            ? Array.from({ length: Math.max(1, skeletonRows) }, (_, i) => `s${i}`).map((k) => (
                <tr key={k} data-row="" data-testid="skeleton-row">
                  {shown.map((c) => (
                    <td key={c.id}>{c.id === "expand" || skeletonRows === 0 ? null : <Shimmer h={14} />}</td>
                  ))}
                </tr>
              ))
            : null}
          {page && rows.length === 0 ? (
            <tr data-empty="" data-testid="empty-row">
              <td colSpan={shown.length}>
                <Empty>
                  {emptyTitle}
                  {onClearFilters ? (
                    <QuietButton type="button" onClick={onClearFilters}>
                      {listCopy.clearFilters}
                    </QuietButton>
                  ) : null}
                </Empty>
              </td>
            </tr>
          ) : null}
          {rows.map((r, i) => {
            const k = r.id;
            const linked = !!objectPath(installId, rowRef(r.original));
            return [
              <tr
                key={k}
                ref={(el) => {
                  rowEls.current[i] = el;
                }}
                data-row=""
                data-link={linked ? "" : undefined}
                tabIndex={i === focusable ? 0 : -1}
                onFocus={() => setActive(i)}
                onClick={(e) => onRowClick(e, r.original)}
                onKeyDown={(e) => onRowKey(e, i, r.original)}
              >
                {r.getVisibleCells().map((c) => (
                  <td key={c.id} data-col={c.column.id}>
                    {flexRender(c.column.columnDef.cell, c.getContext())}
                  </td>
                ))}
              </tr>,
              collapse && expanded.has(k) ? (
                <tr key={`${k}-details`} data-testid="row-details">
                  <td colSpan={shown.length}>
                    <dl>
                      {secondary.map((c) => [
                        <dt key={`${c.id}-t`}>{c.header}</dt>,
                        <dd key={`${c.id}-d`}>{c.cell(r.original)}</dd>,
                      ])}
                    </dl>
                  </td>
                </tr>
              ) : null,
            ];
          })}
        </tbody>
      </Table>
      <Footer data-testid="list-footer">
        <span className="denied">
          {showDenied && page?.deniedInInstall ? listCopy.denied(page.deniedInInstall, noun) : null}
        </span>
        {/* Once loaded, a single page has no pager; the footer keeps its height. */}
        {!page || page.prev || page.next ? <Pager prev={page?.prev} next={page?.next} onPage={onPage} /> : null}
      </Footer>
    </Frame>
  );
}

/** The table's frame and styles, for a detail table that is not a server list (e.g. listeners). */
export { Frame as TableFrame, Table as TableStyles };
