import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useMemo } from "react";
import type { BackendRow, Page } from "../../api/types";
import { backendColumnCopy, backendsCopy, portEndpoints, usedByCopy } from "../../copy/backends";
import { columnCopy } from "../../copy/explorer";
import { endpointCopy, pathColumnCopy } from "../../copy/traffic";
import type { SortState } from "../../explore/listQuery";
import { type ObjectColumn, ObjectTable } from "../../explore/ObjectTable";
import { backendPath } from "../../explore/objectPath";
import { Muted, NameCell, OneLine } from "../explore/listParts";
import { StateCell, WhyCell } from "../traffic/PathCells";
import { endpointsText } from "../traffic/pathText";

// region Styles
/** The state badge and the signals in words, on one line; the words shrink first. */
const Problem = styled.span`
  display: flex;
  align-items: center;
  gap: ${(p) => p.theme.space(2)};
  min-width: 0;
  > :last-child {
    min-width: 0;
  }
`;

// region Helpers
/** The endpoints cell: each referenced port's count, or why there is none (design §8.1). */
export function endpointsCellText(r: BackendRow): { text: string; muted: boolean } {
  if (!r.endpoints.length) return { text: endpointCopy.unknown, muted: true };
  const parts = r.endpoints.map((e) => (e.port ? portEndpoints(e.port.port, endpointsText(e)) : endpointsText(e)));
  return { text: parts.join(" · "), muted: r.endpoints.every((e) => e.complete === "not-applicable") };
}

function columns(installId: string): ObjectColumn<BackendRow>[] {
  return [
    {
      id: "name",
      header: backendColumnCopy.backend,
      sortKey: "name",
      cell: (r) => <NameCell r={r.ref} installId={installId} to={backendPath(installId, r.ref)} />,
    },
    {
      id: "kind",
      header: columnCopy.kind,
      width: "15%",
      cell: (r) => <OneLine text={r.kind} />,
    },
    {
      id: "endpoints",
      header: pathColumnCopy.endpoints,
      width: "17%",
      cell: (r) => {
        const e = endpointsCellText(r);
        return <OneLine text={e.text}>{e.muted ? <Muted>{e.text}</Muted> : e.text}</OneLine>;
      },
    },
    {
      id: "used",
      header: backendColumnCopy.usedBy,
      width: "20%",
      sortKey: "used",
      secondary: true,
      cell: (r) => {
        const uses = usedByCopy.uses(r.routes, r.gateways, r.consumers);
        if (!uses) return <Muted>{usedByCopy.none}</Muted>;
        // Routes write it but no attachment the status accepts reaches it (design §8.1).
        const unconfirmed = r.routes > 0 && r.confirmed === 0;
        return (
          <OneLine text={unconfirmed ? `${uses}\n${usedByCopy.noneConfirmed}` : uses}>
            {uses}
            {unconfirmed ? <Muted> · {usedByCopy.noneConfirmed}</Muted> : null}
          </OneLine>
        );
      },
    },
    {
      id: "problem",
      header: backendColumnCopy.problem,
      width: "26%",
      sortKey: "problem",
      cell: (r) => (
        <Problem>
          <StateCell r={r} />
          <WhyCell r={r} />
        </Problem>
      ),
    },
  ];
}

const rowRef = (r: BackendRow) => r.ref;

// region Component
interface BackendTableProps {
  installId: string;
  page?: Page<BackendRow>;
  busy: boolean;
  onClearFilters?: () => void;
  sort: SortState;
  onSort: (key: string) => void;
  onPage: (cursor: string) => void;
  emptyTitle: string;
  stylesOverride?: SerializedStyles;
}

/** One row per listed backend, problems first by default (design §8.1); a row opens its backend detail. */
export function BackendTable({
  installId,
  page,
  busy,
  onClearFilters,
  sort,
  onSort,
  onPage,
  emptyTitle,
  stylesOverride,
}: BackendTableProps) {
  const cols = useMemo(() => columns(installId), [installId]);
  const rowPath = useMemo(() => (r: BackendRow) => backendPath(installId, r.ref), [installId]);
  return (
    <ObjectTable
      label={backendsCopy.title}
      installId={installId}
      noun={backendsCopy.noun}
      columns={cols}
      rowRef={rowRef}
      rowPath={rowPath}
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
  );
}
