import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useContext, useMemo } from "react";
import { Link } from "react-router-dom";
import { useNow } from "../../api/clock";
import { useFindings } from "../../api/diagHooks";
import { isStale, useListFilter } from "../../api/hooks";
import type { FindingRow, LinkedRef, Severity } from "../../api/types";
import { columnCopy, listCopy } from "../../copy/explorer";
import { ListFrame } from "../../explore/ListFrame";
import { listValues, useListPage } from "../../explore/listQuery";
import { type ObjectColumn, ObjectTable } from "../../explore/ObjectTable";
import { type FilterOption, FilterTag, StateFilter } from "../../explore/StateFilter";
import { HealthBadge } from "../../health/HealthBadge";
import { HealthIcon } from "../../health/HealthIcon";
import { StaleView } from "../../health/staleView";
import { severityLabel, severityToState } from "../../health/state";
import { installPath } from "../../shell/paths";
import { Tooltip } from "../../shell/Tooltip";
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
const Code = styled.span`
  font-family: ${(p) => p.theme.font.mono};
  font-size: ${(p) => p.theme.font.size.sm};
`;

// region Helpers
/** Copy of the findings pages; the wording pass (Task 31) replaces the drafts. */
export const findingsCopy = {
  title: "Findings",
  search: "Filter findings",
  placeholder: "Filter by name or message",
  severity: "Severity",
  code: "Code",
  finding: "Finding",
  object: "Object",
  since: "Since",
  install: "The install",
  restricted: "Restricted: ask the owning team",
  tags: { code: "Code", kind: "Kind", namespace: "Namespace", group: "Group" } as Record<string, string>,
  allFindings: "All findings",
  notFound: "Finding not found",
  detail: "Detail",
  firstSeen: "Since",
  affected: "Affected objects",
  noAffected: "No affected objects you can view.",
  suggestedFix: "Suggested fix",
  nextSteps: "Next steps",
  needs: (perms: string[]) => `Needs ${perms.join(", ")}`,
  copy: "Copy",
};
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

/** A tag's value in words: a group/Kind filter shows its kind. */
const tagValue = (key: string, v: string) => (key === "kind" ? (v.split("/").at(-1) ?? v) : v);

/** Path of a finding's page; a restricted finding has none, as there is nothing more to show. */
export const findingPath = (installId: string, f: { id?: string; restricted?: boolean }) =>
  f.id && !f.restricted ? installPath(installId, `findings/${encodeURIComponent(f.id)}`) : undefined;

/** The ref ObjectTable names a row by (expand label); a finding about the install has none of its own. */
const rowRef = (f: FindingRow): LinkedRef => f.resource ?? { group: "", kind: "", name: f.code, link: "none" };
const rowKey = (f: FindingRow) => f.id;

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

/** "5m ago" from since; owns the clock, so the table does not re-render each second. */
function SinceCell({ since }: { since?: string }) {
  const now = useNow();
  return since ? <OneLine text={since}>{ago(since, now)}</OneLine> : <Muted>{columnCopy.none}</Muted>;
}

function columns(installId: string): ObjectColumn<FindingRow>[] {
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
        const to = findingPath(installId, f);
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
      cell: (f) => (f.resource ? <OneLine text={f.resource.kind} /> : <Muted>{columnCopy.none}</Muted>),
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

interface FindingsPageProps {
  stylesOverride?: SerializedStyles;
}

/** Every finding of the install, filtered, sorted and paged by the server through the URL (design §17.1). */
export function FindingsPage({ stylesOverride }: FindingsPageProps = {}) {
  // Filters are offered once /meta lists them, and shown until it says otherwise, so nothing appears late.
  const support = {
    severity: useListFilter("findings", "severity"),
    code: useListFilter("findings", "code"),
    kind: useListFilter("findings", "kind"),
    namespace: useListFilter("findings", "namespace"),
    group: useListFilter("findings", "group"),
  } as Record<string, boolean | undefined>;
  const skip = OPTIONAL.filter((k) => support[k] === false);
  const l = useListPage("findings", useFindings, skip);
  const cols = useMemo(() => columns(l.id), [l.id]);
  const rowPath = useMemo(() => (f: FindingRow) => findingPath(l.id, f), [l.id]);
  const now = useNow();
  // Stale data or a failed poll turns every severity unknown, as on the other lists.
  const stale = !!l.data && (!!l.error || isStale(l.data.collectedAt, now));
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
          {support.severity !== false ? (
            <StateFilter
              label={findingsCopy.severity}
              options={severityOptions}
              selected={listValues(l.params, "severity")}
              onChange={(v) => l.setFilter({ severity: v.join(",") })}
            />
          ) : null}
          {TAGS.filter((k) => support[k] !== false).flatMap((key) =>
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
      }
      onClearFilters={l.filtered ? l.clearFilters : undefined}
      stylesOverride={stylesOverride}
    >
      <StaleView.Provider value={stale}>
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
      </StaleView.Provider>
    </ListFrame>
  );
}
