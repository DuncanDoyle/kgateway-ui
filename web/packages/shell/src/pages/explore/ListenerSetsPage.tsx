import type { SerializedStyles } from "@emotion/react";
import { useMemo } from "react";
import { useListenerSets } from "../../api/exploreHooks";
import type { ListenerSetRow } from "../../api/types";
import { ListFrame } from "../../explore/ListFrame";
import { useListPage } from "../../explore/listQuery";
import { type ObjectColumn, ObjectTable } from "../../explore/ObjectTable";
import { HealthBadge } from "../../health/HealthBadge";
import { ListFilters, NameCell, useShownPage } from "./listParts";

// region Helpers
const NOUN = ["ListenerSet", "ListenerSets"] as const;

function columns(installId: string): ObjectColumn<ListenerSetRow>[] {
  return [
    {
      id: "health",
      header: "Status",
      width: "120px",
      sortKey: "health",
      cell: (r) => <HealthBadge state={r.health} />,
    },
    { id: "name", header: "ListenerSet", sortKey: "name", cell: (r) => <NameCell r={r.ref} installId={installId} /> },
    { id: "kind", header: "Kind", width: "140px", sortKey: "kind", secondary: true, cell: (r) => r.ref.kind },
    { id: "parent", header: "Parent Gateway", cell: (r) => <NameCell r={r.parent} installId={installId} /> },
    { id: "listeners", header: "Listeners", width: "100px", cell: (r) => r.listeners },
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
      title="ListenerSets"
      installId={l.id}
      noun={NOUN}
      collectedAt={l.data?.collectedAt}
      total={l.data?.total}
      error={l.error}
      loaded={!!l.data}
      search={{
        label: "Filter ListenerSets",
        placeholder: "Filter by name or namespace",
        value: l.text,
        onChange: l.setText,
      }}
      filters={<ListFilters params={l.params} setFilter={l.setFilter} tags={["namespace", "parent"]} />}
      onClearFilters={l.filtered ? l.clearFilters : undefined}
      stylesOverride={stylesOverride}
    >
      <ObjectTable
        label="ListenerSets"
        installId={l.id}
        noun={NOUN}
        columns={cols}
        rowRef={rowRef}
        page={page}
        loading={!l.data}
        emptyTitle={l.filtered ? "No ListenerSets match these filters." : "No ListenerSets in this install."}
        onClearFilters={l.filtered ? l.clearFilters : undefined}
        sort={l.sort}
        onSort={l.setSort}
        onPage={l.setCursor}
      />
    </ListFrame>
  );
}
