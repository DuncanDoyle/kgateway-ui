import type { SerializedStyles } from "@emotion/react";
import { useMemo } from "react";
import { useGateways } from "../../api/exploreHooks";
import type { GatewayListRow } from "../../api/types";
import { columnCopy, listCopy } from "../../copy/explorer";
import { ListFrame } from "../../explore/ListFrame";
import { useListPage } from "../../explore/listQuery";
import { type ObjectColumn, ObjectTable } from "../../explore/ObjectTable";
import { HealthBadge } from "../../health/HealthBadge";
import { ListFilters, Muted, NameCell, OneLine, useShownPage } from "./listParts";

// region Helpers
const NOUN = ["Gateway", "Gateways"] as const;

// The server's "routes" sort counts visible routes, not the status sum shown here, so that column does not sort.
function columns(installId: string): ObjectColumn<GatewayListRow>[] {
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
      header: columnCopy.gateway,
      sortKey: "name",
      cell: (r) => <NameCell r={r.ref} installId={installId} />,
    },
    { id: "class", header: columnCopy.class, width: "14%", secondary: true, cell: (r) => <OneLine text={r.class} /> },
    { id: "listeners", header: columnCopy.listeners, width: "100px", cell: (r) => r.listeners },
    { id: "attachedRoutes", header: columnCopy.attachedRoutes, width: "140px", cell: (r) => r.attachedRoutes },
    { id: "notAccepted", header: columnCopy.notAccepted, width: "120px", secondary: true, cell: (r) => r.notAccepted },
    {
      id: "listenerSets",
      header: columnCopy.listenerSets,
      width: "120px",
      secondary: true,
      cell: (r) => r.listenerSets,
    },
    {
      id: "addresses",
      header: columnCopy.addresses,
      width: "18%",
      secondary: true,
      cell: (r) => (r.addresses.length ? <OneLine text={r.addresses.join(", ")} /> : <Muted>{columnCopy.none}</Muted>),
    },
  ];
}
const rowRef = (r: GatewayListRow) => r.ref;

// region Component
interface GatewaysPageProps {
  stylesOverride?: SerializedStyles;
}

/** The install's Gateways, filtered, sorted and paged by the server through the URL. */
export function GatewaysPage({ stylesOverride }: GatewaysPageProps = {}) {
  const l = useListPage("gateways", useGateways);
  const page = useShownPage(l.data, !!l.error);
  const cols = useMemo(() => columns(l.id), [l.id]);
  return (
    <ListFrame
      title={listCopy.gateways.title}
      installId={l.id}
      noun={NOUN}
      collectedAt={l.data?.collectedAt}
      total={l.data?.total}
      error={l.error}
      loaded={!!l.data}
      search={{
        label: listCopy.gateways.search,
        placeholder: listCopy.searchByName,
        value: l.text,
        onChange: l.setText,
      }}
      filters={<ListFilters params={l.params} setFilter={l.setFilter} tags={["namespace"]} />}
      onClearFilters={l.filtered ? l.clearFilters : undefined}
      stylesOverride={stylesOverride}
    >
      <ObjectTable
        label={listCopy.gateways.title}
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
