import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useMemo } from "react";
import { useRoutes } from "../../api/exploreHooks";
import type { Attachment, ParentSummary, RouteRow } from "../../api/types";
import { ListFrame } from "../../explore/ListFrame";
import { listValues, useListPage } from "../../explore/listQuery";
import { type ObjectColumn, ObjectTable } from "../../explore/ObjectTable";
import { type FilterOption, StateFilter } from "../../explore/StateFilter";
import { HealthBadge } from "../../health/HealthBadge";
import { ResourceName } from "../../resource/ResourceName";
import { ListFilters, Muted, NameCell, useShownPage, wrapName } from "./listParts";

// region Styles
const Tag = styled.span`
  display: inline-block;
  margin-left: ${(p) => p.theme.space(1)};
  padding: 0 6px;
  border-radius: ${(p) => p.theme.radius.sm};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  font-size: ${(p) => p.theme.font.size.xs};
  color: ${(p) => p.theme.color.textMuted};
`;
const Parents = styled.ul`
  margin: 0;
  padding: 0;
  list-style: none;
  font-size: ${(p) => p.theme.font.size.sm};
  li + li {
    margin-top: 2px;
  }
`;

// region Helpers
const NOUN = ["route", "routes"] as const;

/** How a parent took the route, in words; the raw condition reason is never shown (design §8.8 owns the copy). */
const attachmentLabel: Record<Attachment, string> = {
  accepted: "Accepted",
  "not-accepted": "Not accepted",
  pending: "Waiting for status",
  unwatched: "Namespace not watched",
  declared: "Parent route has no status",
};

/** The 2.2 route state flags (design §6.1); the wording pass (Task 31) finalises the labels. */
const stateOptions: FilterOption[] = [
  { value: "accepted", label: "Accepted" },
  { value: "not-accepted", label: "Not accepted" },
  { value: "refs-unresolved", label: "References not resolved" },
  { value: "not-programmed", label: "Rules replaced or dropped" },
  { value: "pending", label: "Waiting for status" },
  { value: "declared", label: "Parent route has no status" },
  { value: "no-parent", label: "No parent" },
];

function ParentLine({ p, installId }: { p: ParentSummary; installId: string }) {
  return (
    <li>
      <NameCell r={p.ref} installId={installId} /> · <Muted>{attachmentLabel[p.attachment]}</Muted>
      {p.via ? (
        <Muted>
          {" "}
          through <ResourceName namespace={p.via.namespace} name={p.via.name} stylesOverride={wrapName} />
        </Muted>
      ) : null}
      {p.viaRestricted ? <Muted> through a Gateway you cannot view</Muted> : null}
    </li>
  );
}

function columns(installId: string): ObjectColumn<RouteRow>[] {
  return [
    {
      id: "health",
      header: "Status",
      width: "120px",
      sortKey: "health",
      cell: (r) => <HealthBadge state={r.health} />,
    },
    {
      id: "name",
      header: "Route",
      sortKey: "name",
      cell: (r) => (
        <>
          <NameCell r={r.ref} installId={installId} />
          {r.mirror ? <Tag>Mirror</Tag> : null}
        </>
      ),
    },
    { id: "kind", header: "Kind", width: "120px", sortKey: "kind", secondary: true, cell: (r) => r.ref.kind },
    {
      id: "parents",
      header: "Parents",
      cell: (r) =>
        r.parents.length ? (
          <Parents>
            {r.parents.map((p) => (
              <ParentLine
                key={[p.ref.kind, p.ref.namespace, p.ref.name, p.sectionName, p.port].join("/")}
                p={p}
                installId={installId}
              />
            ))}
          </Parents>
        ) : (
          <Muted>None</Muted>
        ),
    },
    {
      id: "hostnames",
      header: "Hostnames",
      width: "18%",
      secondary: true,
      cell: (r) => (r.hostnames.length ? r.hostnames.join(", ") : <Muted>Inherited</Muted>),
    },
    { id: "children", header: "Child routes", width: "110px", secondary: true, cell: (r) => r.children },
  ];
}

// region Component
interface RoutesPageProps {
  stylesOverride?: SerializedStyles;
}

/** The install's routes, filtered, sorted and paged by the server through the URL. */
export function RoutesPage({ stylesOverride }: RoutesPageProps = {}) {
  const l = useListPage("routes", useRoutes);
  const page = useShownPage(l.data, !!l.error);
  const cols = useMemo(() => columns(l.id), [l.id]);
  return (
    <ListFrame
      title="Routes"
      installId={l.id}
      noun={NOUN}
      collectedAt={l.data?.collectedAt}
      total={l.data?.total}
      error={l.error}
      loaded={!!l.data}
      search={{
        label: "Filter routes",
        placeholder: "Filter by name, namespace or hostname",
        value: l.text,
        onChange: l.setText,
      }}
      filters={
        <ListFilters
          params={l.params}
          setFilter={l.setFilter}
          tags={["namespace", "kind", "parent", "attachment", "backend"]}
        >
          <StateFilter
            label="Route state"
            options={stateOptions}
            selected={listValues(l.params, "state")}
            onChange={(v) => l.setFilter({ state: v.join(",") })}
          />
        </ListFilters>
      }
      onClearFilters={l.filtered ? l.clearFilters : undefined}
      stylesOverride={stylesOverride}
    >
      <ObjectTable
        label="Routes"
        installId={l.id}
        noun={NOUN}
        columns={cols}
        rowRef={rowRef}
        page={page}
        loading={!l.data}
        emptyTitle={l.filtered ? "No routes match these filters." : "No routes in this install."}
        onClearFilters={l.filtered ? l.clearFilters : undefined}
        sort={l.sort}
        onSort={l.setSort}
        onPage={l.setCursor}
      />
    </ListFrame>
  );
}

const rowRef = (r: RouteRow) => r.ref;
