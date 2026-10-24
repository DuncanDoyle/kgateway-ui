import type { SerializedStyles } from "@emotion/react";
import { useMemo } from "react";
import { useListenerSets } from "../../api/exploreHooks";
import type { ListenerSetRow } from "../../api/types";
import { columnCopy, listCopy, viewACopy } from "../../copy/explorer";
import { ListFrame } from "../../explore/ListFrame";
import { useListPage } from "../../explore/listQuery";
import { type ObjectColumn, ObjectTable } from "../../explore/ObjectTable";
import { HealthBadge } from "../../health/HealthBadge";
import { ListFilters, NameCell, RoutesCell, routesHeaderExtra, useShownPage } from "./listParts";

// region Helpers
const NOUN = ["ListenerSet", "ListenerSets"] as const;

function columns(installId: string): ObjectColumn<ListenerSetRow>[] {
  return [
    {
      id: "health",
      header: columnCopy.status,
      width: "120px",
      sortKey: "health",
      cell: (r) => <HealthBadge state={r.health} />,
    },
    {
      id: "name",
      header: columnCopy.listenerSet,
      sortKey: "name",
      cell: (r) => <NameCell r={r.ref} installId={installId} />,
    },
    { id: "kind", header: columnCopy.kind, width: "140px", sortKey: "kind", secondary: true, cell: (r) => r.ref.kind },
    { id: "parent", header: columnCopy.parentGateway, cell: (r) => <NameCell r={r.parent} installId={installId} /> },
    { id: "listeners", header: columnCopy.listeners, width: "100px", cell: (r) => r.listeners },
    {
      // The server sorts ListenerSets by name, kind and health only (design §7.2).
      id: "routes",
      header: viewACopy.routesColumn,
      headerExtra: routesHeaderExtra,
      width: "150px",
      cell: (r) => <RoutesCell routes={r.routes} disagreements={r.disagreements} />,
    },
  ];
}
const rowRef = (r: ListenerSetRow) => r.ref;

// region Component
interface ListenerSetsPageProps {
  stylesOverride?: SerializedStyles;
}

/** The install's ListenerSets of every ListenerSet kind, filtered, sorted and paged by the server through the URL. */
export function ListenerSetsPage({ stylesOverride }: ListenerSetsPageProps = {}) {
  const l = useListPage("listenersets", useListenerSets);
  const page = useShownPage(l.data, !!l.error);
  const cols = useMemo(() => columns(l.id), [l.id]);
  return (
    <ListFrame
      title={listCopy.listenerSets.title}
      installId={l.id}
      noun={NOUN}
      collectedAt={l.data?.collectedAt}
      total={l.data?.total}
      error={l.error}
      loaded={!!l.data}
      search={{
        label: listCopy.listenerSets.search,
        placeholder: listCopy.searchByName,
        value: l.text,
        onChange: l.setText,
      }}
      filters={<ListFilters params={l.params} setFilter={l.setFilter} tags={["namespace", "parent"]} />}
      onClearFilters={l.filtered ? l.clearFilters : undefined}
      stylesOverride={stylesOverride}
    >
      <ObjectTable
        label={listCopy.listenerSets.title}
        installId={l.id}
        noun={NOUN}
        columns={cols}
        rowRef={rowRef}
        page={page}
        loading={!l.data}
        busy={l.busy}
        emptyTitle={l.filtered ? listCopy.emptyFiltered(NOUN[1]) : listCopy.empty(NOUN[1])}
        onClearFilters={l.filtered ? l.clearFilters : undefined}
        sort={l.sort}
        onSort={l.setSort}
        onPage={l.setCursor}
      />
    </ListFrame>
  );
}
