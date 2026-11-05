import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { type ReactNode, useContext, useMemo } from "react";
import { Link, useLocation } from "react-router-dom";
import { useNow } from "../../api/clock";
import { useFindingGroups, useFindings } from "../../api/diagHooks";
import { isStale, useListFilter } from "../../api/hooks";
import type { FindingItem, GroupRow, LinkedRef, Severity } from "../../api/types";
import { columnCopy, findingsCopy, groupsCopy, listCopy } from "../../copy/explorer";
import { ListFrame } from "../../explore/ListFrame";
import { listValues, useListPage } from "../../explore/listQuery";
import { type ObjectColumn, ObjectTable } from "../../explore/ObjectTable";
import { withVia } from "../../explore/objectPath";
import { type FilterOption, FilterTag, StateFilter } from "../../explore/StateFilter";
import { HealthBadge } from "../../health/HealthBadge";
import { HealthIcon } from "../../health/HealthIcon";
import { StaleView } from "../../health/staleView";
import { severityLabel, severityToState } from "../../health/state";
import { installPath } from "../../shell/paths";
import { Tooltip } from "../../shell/Tooltip";
import { useLinkVia } from "../../shell/trail";
import { Chip } from "../../states/controls";
import { Muted, NameCell, nameTip, OneLine } from "../explore/listParts";
import { ago } from "../install/HealthStrip";

// region Styles
/** A finding's message as one line that opens the finding; the full text is in its Tooltip. */
const FindingLink = styled(Link)`
  display: block;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  color: inherit;
  text-decoration: none;
  border-radius: ${(p) => p.theme.radius.sm};
  /* The row opens the finding too, so hovering the row underlines it. */
  &:hover,
  tr[data-link]:hover & {
    text-decoration: underline;
  }
  &:active {
    color: ${(p) => p.theme.color.accentText};
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
`;
/** Two joined chips, so the view switch never reads as one more filter. */
const ViewGroup = styled.div`
  display: inline-flex;
  margin-right: ${(p) => p.theme.space(2)};
  & > button:first-of-type {
    border-top-right-radius: 0;
    border-bottom-right-radius: 0;
  }
  & > button:last-of-type {
    margin-left: -1px;
    border-top-left-radius: 0;
    border-bottom-left-radius: 0;
  }
  & > button[aria-pressed="true"],
  & > button:focus-visible {
    position: relative;
    z-index: 1;
  }
`;
const Code = styled.span`
  font-family: ${(p) => p.theme.font.mono};
  font-size: ${(p) => p.theme.font.size.sm};
`;

// region Helpers
const NOUN = ["finding", "findings"] as const;
const SEVERITIES: Severity[] = ["error", "warning", "info"];
const severityOptions: FilterOption[] = SEVERITIES.map((s) => ({
  value: s,
  label: severityLabel[s],
  icon: <HealthIcon state={severityToState(s)} />,
}));
/** Filters set through the URL only (links from other pages), shown as removable tags. */
const TAGS = ["code", "kind", "namespace", "group"];
const OPTIONAL = ["severity", ...TAGS];

/** Params the grouped view does not take (pkg/api groupsParams); switching to it drops them, so it never 400s. */
const NOT_GROUPED = ["kind", "namespace", "group", "sort"];
/** Filters a group row carries to the flat view of its members (R98). */
const GROUP_CARRIES = ["severity", "code", "q"];

/** The flat view of group g's findings, with the grouped view's filters. */
const groupFindingsPath = (installId: string, g: { id: string }, params: URLSearchParams) => {
  const q = new URLSearchParams();
  for (const k of GROUP_CARRIES) if (params.get(k)) q.set(k, params.get(k) as string);
  q.set("group", g.id);
  return `${installPath(installId, "findings")}?${q}`;
};

/** A tag's value in words: a group/Kind filter shows its kind. */
const tagValue = (key: string, v: string) => (key === "kind" ? (v.split("/").at(-1) ?? v) : v);

/** Path of a finding's page; a restricted finding has none, as there is nothing more to show. */
export const findingPath = (installId: string, f: { id?: string; restricted?: boolean }) =>
  f.id && !f.restricted ? installPath(installId, `findings/${encodeURIComponent(f.id)}`) : undefined;

/** The ref ObjectTable names a row by (expand label); a finding about the install has none of its own. */
const rowRef = (f: FindingItem): LinkedRef => f.resource ?? { group: "", kind: "", name: f.code, link: "none" };
const rowKey = (f: FindingItem) => f.id;

// region Component
/** A finding's severity: icon, colour and text; unknown under a stale view, like every state (design §6.3). */
export function SeverityBadge({ severity }: { severity: Severity }) {
  const stale = useContext(StaleView);
  return stale ? (
    <HealthBadge state="unknown" />
  ) : (
    <HealthBadge state={severityToState(severity)} label={severityLabel[severity]} />
  );
}

/** A value the row does not have, e.g. the age of a restricted finding: a muted dash, read as "No value". */
function NoValue() {
  return <Muted aria-label={findingsCopy.noValue}>—</Muted>;
}

/** Provides StaleView from data age; owns the clock, so its children keep identity and do not re-render each second. */
function StaleWhen({ collectedAt, failed, children }: { collectedAt?: string; failed: boolean; children: ReactNode }) {
  const now = useNow();
  const stale = !!collectedAt && (failed || isStale(collectedAt, now));
  return <StaleView.Provider value={stale}>{children}</StaleView.Provider>;
}

/** "5m ago" from since; owns the clock, so the table does not re-render each second. */
function SinceCell({ since }: { since?: string }) {
  const now = useNow();
  return since ? <OneLine text={since}>{ago(since, now)}</OneLine> : <NoValue />;
}

function columns(installId: string, via?: string): ObjectColumn<FindingItem>[] {
  return [
    {
      id: "severity",
      header: findingsCopy.severity,
      width: "120px",
      // Rank orders by severity first, then cause class, blast radius and age (design §5.3).
      sortKey: "rank",
      cell: (f) => <SeverityBadge severity={f.severity} />,
    },
    { id: "code", header: findingsCopy.code, width: "120px", sortKey: "code", cell: (f) => <Code>{f.code}</Code> },
    {
      id: "finding",
      header: findingsCopy.finding,
      cell: (f) => {
        const page = findingPath(installId, f);
        const to = page ? withVia(page, via) : undefined;
        if (!to) return <Muted>{findingsCopy.restricted}</Muted>;
        const text = f.message ?? "";
        return (
          <Tooltip label={f.detail ? `${text}. ${f.detail}` : text} wrap stylesOverride={nameTip}>
            <FindingLink to={to}>{text}</FindingLink>
          </Tooltip>
        );
      },
    },
    {
      id: "object",
      header: findingsCopy.object,
      width: "26%",
      cell: (f) =>
        f.resource ? <NameCell r={f.resource} installId={installId} /> : <Muted>{findingsCopy.install}</Muted>,
    },
    {
      id: "kind",
      header: columnCopy.kind,
      width: "140px",
      secondary: true,
      cell: (f) => (f.resource ? <OneLine text={f.resource.kind} /> : <NoValue />),
    },
    {
      id: "since",
      header: findingsCopy.since,
      width: "110px",
      sortKey: "since",
      secondary: true,
      cell: (f) => <SinceCell since={f.since} />,
    },
  ];
}

function groupColumns(installId: string, params: URLSearchParams): ObjectColumn<GroupRow>[] {
  return [
    {
      id: "severity",
      header: findingsCopy.severity,
      width: "120px",
      cell: (g) => <SeverityBadge severity={g.severity} />,
    },
    // The top member's code (R98).
    { id: "code", header: findingsCopy.code, width: "120px", cell: (g) => <Code>{g.code}</Code> },
    {
      id: "title",
      header: groupsCopy.title,
      cell: (g) => {
        const to = groupFindingsPath(installId, g, params);
        if (g.restricted)
          return (
            <FindingLink to={to}>
              <Muted>{findingsCopy.restricted}</Muted>
            </FindingLink>
          );
        return (
          <Tooltip label={g.title} wrap stylesOverride={nameTip}>
            <FindingLink to={to}>{g.title}</FindingLink>
          </Tooltip>
        );
      },
    },
    {
      id: "cause",
      header: groupsCopy.cause,
      width: "24%",
      cell: (g) => (!g.restricted && g.cause ? <NameCell r={g.cause} installId={installId} /> : <NoValue />),
    },
    {
      id: "affected",
      header: groupsCopy.affected,
      width: "100px",
      secondary: true,
      cell: (g) => (g.restricted ? <NoValue /> : g.affectedTotal),
    },
    { id: "members", header: groupsCopy.members, width: "100px", secondary: true, cell: (g) => g.members },
    {
      id: "since",
      header: findingsCopy.since,
      width: "110px",
      secondary: true,
      cell: (g) => <SinceCell since={g.since} />,
    },
  ];
}
const groupRef = (g: GroupRow): LinkedRef =>
  !g.restricted && g.cause ? g.cause : { group: "", kind: "", name: g.title || g.code, link: "none" };
const groupKey = (g: GroupRow) => g.id;
const NO_SORT = { key: "", desc: false };

/** The view switch: flat findings or their groups, held in the URL as view=groups (design §17.1). */
function ViewSwitch({ grouped, onChange }: { grouped: boolean; onChange: (grouped: boolean) => void }) {
  return (
    <ViewGroup role="group" aria-label={groupsCopy.view}>
      <Chip type="button" aria-pressed={!grouped} onClick={() => grouped && onChange(false)}>
        {groupsCopy.flat}
      </Chip>
      <Chip type="button" aria-pressed={grouped} onClick={() => grouped || onChange(true)}>
        {groupsCopy.groups}
      </Chip>
    </ViewGroup>
  );
}

/** Which optional filters /meta offers; undefined until it answers, so nothing appears late. */
function useSupport() {
  return {
    severity: useListFilter("findings", "severity"),
    code: useListFilter("findings", "code"),
    kind: useListFilter("findings", "kind"),
    namespace: useListFilter("findings", "namespace"),
    group: useListFilter("findings", "group"),
  } as Record<string, boolean | undefined>;
}

type ListPage = ReturnType<typeof useListPage<FindingItem>>;

/** The severity chips and the URL-only filter tags of keys. */
function Filters({
  l,
  support,
  keys,
}: {
  l: Pick<ListPage, "params" | "setFilter">;
  support: Record<string, boolean | undefined>;
  keys: string[];
}) {
  return (
    <>
      {support.severity !== false ? (
        <StateFilter
          label={findingsCopy.severity}
          options={severityOptions}
          selected={listValues(l.params, "severity")}
          onChange={(v) => l.setFilter({ severity: v.join(",") })}
        />
      ) : null}
      {keys
        .filter((k) => support[k] !== false)
        .flatMap((key) =>
          listValues(l.params, key).map((v) => {
            const rest = listValues(l.params, key).filter((x) => x !== v);
            return (
              <FilterTag
                key={`${key}=${v}`}
                text={`${findingsCopy.tags[key]}: ${tagValue(key, v)}`}
                onRemove={() => l.setFilter({ [key]: rest.join(",") })}
              />
            );
          }),
        )}
    </>
  );
}

/** The grouped view: one row per cause, opening the flat view of its members with the same filters (R98). */
function GroupedFindings({ stylesOverride }: FindingsPageProps) {
  const support = useSupport();
  const skip = [...NOT_GROUPED, ...OPTIONAL.filter((k) => support[k] === false)];
  const l = useListPage<GroupRow>("findings", useFindingGroups, skip);
  const cols = useMemo(() => groupColumns(l.id, l.params), [l.id, l.params]);
  const rowPath = useMemo(() => (g: GroupRow) => groupFindingsPath(l.id, g, l.params), [l.id, l.params]);
  const noun = groupsCopy.noun;
  return (
    <ListFrame
      title={findingsCopy.title}
      installId={l.id}
      noun={noun}
      collectedAt={l.data?.collectedAt}
      total={l.data?.total}
      error={l.error}
      loaded={!!l.data}
      search={{ label: findingsCopy.search, placeholder: findingsCopy.placeholder, value: l.text, onChange: l.setText }}
      filters={
        <>
          <ViewSwitch grouped onChange={() => l.setFilter({ view: null })} />
          <Filters l={l} support={support} keys={["code"]} />
        </>
      }
      onClearFilters={l.filtered ? l.clearFilters : undefined}
      stylesOverride={stylesOverride}
    >
      <StaleWhen collectedAt={l.data?.collectedAt} failed={!!l.error}>
        <ObjectTable
          label={findingsCopy.title}
          installId={l.id}
          noun={noun}
          columns={cols}
          rowRef={groupRef}
          rowKey={groupKey}
          rowPath={rowPath}
          page={l.data}
          loading={!l.data}
          busy={l.busy}
          emptyTitle={l.filtered ? listCopy.emptyFiltered(noun[1]) : listCopy.empty(noun[1])}
          onClearFilters={l.filtered ? l.clearFilters : undefined}
          sort={NO_SORT}
          onSort={() => {}}
          onPage={l.setCursor}
        />
      </StaleWhen>
    </ListFrame>
  );
}

interface FindingsPageProps {
  stylesOverride?: SerializedStyles;
}

/** Every finding of the install, flat or grouped, filtered, sorted and paged by the server through the URL (design §17.1). */
export function FindingsPage({ stylesOverride }: FindingsPageProps = {}) {
  // Each view owns the URL params while it shows (one useUrlParams per page), so the switch remounts it.
  const grouped = new URLSearchParams(useLocation().search).get("view") === "groups";
  return grouped ? (
    <GroupedFindings stylesOverride={stylesOverride} />
  ) : (
    <FlatFindings stylesOverride={stylesOverride} />
  );
}

function FlatFindings({ stylesOverride }: FindingsPageProps) {
  const support = useSupport();
  const skip = OPTIONAL.filter((k) => support[k] === false);
  const l = useListPage("findings", useFindings, skip);
  const via = useLinkVia(l.id);
  const cols = useMemo(() => columns(l.id, via), [l.id, via]);
  const rowPath = useMemo(() => (f: FindingItem) => findingPath(l.id, f), [l.id]);
  return (
    <ListFrame
      title={findingsCopy.title}
      installId={l.id}
      noun={NOUN}
      collectedAt={l.data?.collectedAt}
      total={l.data?.total}
      error={l.error}
      loaded={!!l.data}
      search={{ label: findingsCopy.search, placeholder: findingsCopy.placeholder, value: l.text, onChange: l.setText }}
      filters={
        <>
          <ViewSwitch
            grouped={false}
            onChange={() => l.setFilter({ view: "groups", ...Object.fromEntries(NOT_GROUPED.map((k) => [k, null])) })}
          />
          <Filters l={l} support={support} keys={TAGS} />
        </>
      }
      onClearFilters={l.filtered ? l.clearFilters : undefined}
      stylesOverride={stylesOverride}
    >
      {/* Stale data or a failed poll turns every severity unknown, as on the other lists. */}
      <StaleWhen collectedAt={l.data?.collectedAt} failed={!!l.error}>
        <ObjectTable
          label={findingsCopy.title}
          installId={l.id}
          noun={NOUN}
          columns={cols}
          rowRef={rowRef}
          rowKey={rowKey}
          rowPath={rowPath}
          page={l.data}
          loading={!l.data}
          busy={l.busy}
          emptyTitle={l.filtered ? listCopy.emptyFiltered(NOUN[1]) : listCopy.empty(NOUN[1])}
          onClearFilters={l.filtered ? l.clearFilters : undefined}
          sort={l.sort}
          // Rank, the default, sorts one way only: choosing it clears the sort.
          onSort={(k) => (k === "rank" ? l.setFilter({ sort: null }) : l.setSort(k))}
          onPage={l.setCursor}
        />
      </StaleWhen>
    </ListFrame>
  );
}
