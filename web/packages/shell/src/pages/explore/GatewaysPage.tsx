import type { SerializedStyles } from "@emotion/react";
import { useMemo } from "react";
import { useGateways } from "../../api/exploreHooks";
import type { GatewayListRow } from "../../api/types";
import { ListFrame } from "../../explore/ListFrame";
import { useListPage } from "../../explore/listQuery";
import { type ObjectColumn, ObjectTable } from "../../explore/ObjectTable";
import { HealthBadge } from "../../health/HealthBadge";
import { ListFilters, Muted, NameCell, useShownPage } from "./listParts";

// region Helpers
const NOUN = ["Gateway", "Gateways"] as const;

// The server's "routes" sort counts visible routes, not the status sum shown here, so that column does not sort.
function columns(installId: string): ObjectColumn<GatewayListRow>[] {
  return [
    {
      id: "health",
      header: "Status",
      width: "120px",
      sortKey: "health",
      cell: (r) => <HealthBadge state={r.health} />,
    },
    { id: "name", header: "Gateway", sortKey: "name", cell: (r) => <NameCell r={r.ref} installId={installId} /> },
    { id: "class", header: "Class", width: "14%", secondary: true, cell: (r) => r.class },
    { id: "listeners", header: "Listeners", width: "100px", cell: (r) => r.listeners },
    { id: "attachedRoutes", header: "Attached routes", width: "140px", cell: (r) => r.attachedRoutes },
    { id: "notAccepted", header: "Not accepted", width: "120px", secondary: true, cell: (r) => r.notAccepted },
    { id: "listenerSets", header: "ListenerSets", width: "120px", secondary: true, cell: (r) => r.listenerSets },
    {
      id: "addresses",
      header: "Addresses",
      width: "18%",
      secondary: true,
      cell: (r) => (r.addresses.length ? r.addresses.join(", ") : <Muted>None</Muted>),
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
      title="Gateways"
      installId={l.id}
      noun={NOUN}
      collectedAt={l.data?.collectedAt}
      total={l.data?.total}
      error={l.error}
      loaded={!!l.data}
      search={{
        label: "Filter gateways",
        placeholder: "Filter by name or namespace",
        value: l.text,
        onChange: l.setText,
      }}
      filters={<ListFilters params={l.params} setFilter={l.setFilter} tags={["namespace"]} />}
      onClearFilters={l.filtered ? l.clearFilters : undefined}
      stylesOverride={stylesOverride}
    >
      <ObjectTable
        label="Gateways"
        installId={l.id}
        noun={NOUN}
        columns={cols}
        rowRef={rowRef}
        page={page}
        loading={!l.data}
        busy={l.busy}
        emptyTitle={l.filtered ? "No Gateways match these filters." : "No Gateways in this install."}
        onClearFilters={l.filtered ? l.clearFilters : undefined}
        sort={l.sort}
        onSort={l.setSort}
        onPage={l.setCursor}
      />
    </ListFrame>
  );
}
