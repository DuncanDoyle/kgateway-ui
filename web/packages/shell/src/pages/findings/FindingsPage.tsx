import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { type ReactNode, useContext, useMemo } from "react";
import { Link } from "react-router-dom";
import { useNow } from "../../api/clock";
import { useFindings } from "../../api/diagHooks";
import { isStale, useListFilter } from "../../api/hooks";
import type { FindingItem, LinkedRef, Severity } from "../../api/types";
import { columnCopy, findingsCopy, listCopy } from "../../copy/explorer";
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
