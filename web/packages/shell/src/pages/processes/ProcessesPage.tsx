import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { type ReactNode, useMemo } from "react";
import { useNow } from "../../api/clock";
import { useProcesses } from "../../api/diagHooks";
import { isStale } from "../../api/hooks";
import type { GatewayProcesses, Page, ProcessSet } from "../../api/types";
import { columnCopy, diagnosticsCopy, listCopy, processesCopy } from "../../copy/explorer";
import { DIAG_ROW_H, ProcessTable } from "../../diagnostics/ProcessTable";
import { ListFrame } from "../../explore/ListFrame";
import { useListPage } from "../../explore/listQuery";
import { type ObjectColumn, ObjectTable } from "../../explore/ObjectTable";
import { StateFilter } from "../../explore/StateFilter";
import { HealthBadge } from "../../health/HealthBadge";
import { StaleView } from "../../health/staleView";
import { Shimmer } from "../../states/Shimmer";
import { ListFilters, Muted, NameCell, OneLine, useShownPage } from "../explore/listParts";

// region Styles
/** Header plus one row: reserved from the first render, so a one-replica controller moves nothing below. */
const CONTROLLER_MIN_H = 2 * DIAG_ROW_H;

const Block = styled.section`
  margin-bottom: ${(p) => p.theme.space(5)};
  h2 {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(3)};
    margin: 0 0 ${(p) => p.theme.space(2)};
    font-size: ${(p) => p.theme.font.size.base};
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  h2 .note {
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
    font-weight: ${(p) => p.theme.font.weight.regular};
  }
  .box {
    min-height: ${CONTROLLER_MIN_H}px;
    padding: ${(p) => p.theme.space(2)} 0;
    border: 1px solid ${(p) => p.theme.color.border};
    border-radius: ${(p) => p.theme.radius.md};
    background: ${(p) => p.theme.color.surface};
  }
  /* Lines outside the table line up with its cells. */
  .box > span,
  .box > div > span {
    margin: 0 ${(p) => p.theme.space(3)};
  }
`;
const ProxiesHeading = styled.h2`
  margin: 0 0 ${(p) => p.theme.space(2)};
  font-size: ${(p) => p.theme.font.size.base};
  font-weight: ${(p) => p.theme.font.weight.semibold};
`;
const ShowOnly = styled.div`
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(2)};
  padding-left: ${(p) => p.theme.space(2)};
  border-left: 1px solid ${(p) => p.theme.color.borderStrong};
  .label {
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
  }
`;
const Mono = styled.span`
  font-family: ${(p) => p.theme.font.mono};
`;

// region Helpers
const NOUN = ["Gateway", "Gateways"] as const;
const SHOW: { key: "skew" | "restarts"; label: string }[] = [
  { key: "skew", label: processesCopy.skew },
  { key: "restarts", label: processesCopy.restarting },
];
const NO_SORT = { key: "", desc: false };
const noop = () => {};

type ProcessesPageData = Page<GatewayProcesses> & { controller: ProcessSet };

/** GET …/processes as a list page: the proxies page, with the controller set beside it. */
function useProcessesList(id: string, query: URLSearchParams) {
  const r = useProcesses(id, query);
  const data = useMemo<ProcessesPageData | undefined>(
    () => (r.data ? { ...r.data.proxies, controller: r.data.controller, collectedAt: r.data.collectedAt } : undefined),
    [r.data],
  );
  return { ...r, data };
}

/** The ready count, or "Not observed"; never a state of its own (D-016). */
function ProxyCount({ set }: { set: ProcessSet }) {
  if (!set.observed) return <Muted>{diagnosticsCopy.notObserved}</Muted>;
  if (set.ready === undefined || set.desired === undefined) return <Muted>{diagnosticsCopy.notReported}</Muted>;
  return <>{diagnosticsCopy.readyOf(set.ready, set.desired)}</>;
}

/** The visible proxies' versions; the skew flag is the server's and includes patch differences. */
function Versions({ row }: { row: GatewayProcesses }) {
  const versions = [...new Set(row.set.processes.map((p) => p.version).filter(Boolean))].join(", ");
  if (!versions && podsHidden(row.set)) return <Muted>{processesCopy.hidden}</Muted>;
  if (!versions) return <Muted>{row.set.observed ? diagnosticsCopy.notReported : null}</Muted>;
  return (
    <OneLine text={row.skew ? `${versions} ${processesCopy.differs}` : versions}>
      <Mono>{versions}</Mono>
      {row.skew ? <Muted> {processesCopy.differs}</Muted> : null}
    </OneLine>
  );
}

/** The server sent counts but no Pods: the caller may not view them (§14 rule 16). */
const podsHidden = (set: ProcessSet) =>
  !!set.restricted || (set.observed && set.processes.length === 0 && (set.desired ?? set.ready ?? 0) > 0);

function recentRestarts(set: ProcessSet): number | undefined {
  const known = set.processes.filter((p) => p.restartsRecent !== undefined);
  return known.length ? known.reduce((n, p) => n + (p.restartsRecent ?? 0), 0) : undefined;
}

function columns(installId: string): ObjectColumn<GatewayProcesses>[] {
  return [
    { id: "health", header: columnCopy.status, width: "120px", cell: (r) => <HealthBadge state={r.health} /> },
    { id: "name", header: processesCopy.gateway, cell: (r) => <NameCell r={r.gateway} installId={installId} /> },
    { id: "proxies", header: processesCopy.proxies, width: "130px", cell: (r) => <ProxyCount set={r.set} /> },
    {
      id: "rejects",
      header: processesCopy.rejects,
      width: "150px",
      cell: (r) => (r.rejects !== undefined ? r.rejects : <Muted aria-label={diagnosticsCopy.notReported}>—</Muted>),
    },
    { id: "version", header: processesCopy.version, width: "22%", secondary: true, cell: (r) => <Versions row={r} /> },
    {
      id: "restarts",
      header: processesCopy.restarts,
      width: "130px",
      secondary: true,
      cell: (r) =>
        recentRestarts(r.set) ??
        (podsHidden(r.set) ? (
          <Muted>{processesCopy.hidden}</Muted>
        ) : (
          <Muted aria-label={diagnosticsCopy.notReported}>—</Muted>
        )),
    },
  ];
}
const rowRef = (r: GatewayProcesses) => r.gateway;

/** Provides StaleView from data age; owns the clock, so its children do not re-render each second. */
function StaleWhen({ collectedAt, failed, children }: { collectedAt?: string; failed: boolean; children: ReactNode }) {
  const now = useNow();
  const stale = !!collectedAt && (failed || isStale(collectedAt, now));
  return <StaleView.Provider value={stale}>{children}</StaleView.Provider>;
}

// region Component
interface ProcessesPageProps {
  stylesOverride?: SerializedStyles;
}

/** The controller's processes and each Gateway's proxies, filtered and paged by the server through the URL. */
export function ProcessesPage({ stylesOverride }: ProcessesPageProps = {}) {
  const l = useListPage<GatewayProcesses, ProcessesPageData>("processes", useProcessesList);
  const page = useShownPage(l.data, !!l.error);
  const cols = useMemo(() => columns(l.id), [l.id]);
  const shown = SHOW.filter((s) => l.params.get(s.key) === "true").map((s) => s.key);
  const ctrl = l.data?.controller;
  const counts =
    // A restricted set shows its counts in the table's place.
    ctrl?.observed && !ctrl.restricted && ctrl.ready !== undefined && ctrl.desired !== undefined
      ? diagnosticsCopy.readyOf(ctrl.ready, ctrl.desired)
      : undefined;
  return (
    <StaleWhen collectedAt={l.data?.collectedAt} failed={!!l.error}>
      <ListFrame
        title={processesCopy.title}
        installId={l.id}
        noun={NOUN}
        collectedAt={l.data?.collectedAt}
        total={l.data?.total}
        error={l.error}
        loaded={!!l.data}
        lead={
          <>
            <Block aria-labelledby="proc-controller">
              <h2>
                <span id="proc-controller">{diagnosticsCopy.controller}</span>
                {counts ? <span className="note">{counts}</span> : null}
              </h2>
              <div className="box">
                {ctrl ? (
                  <ProcessTable set={ctrl} label={processesCopy.controllerTable} />
                ) : (
                  <Shimmer h={CONTROLLER_MIN_H - 16} />
                )}
              </div>
            </Block>
            {/* The filters below apply to this table only, so its heading sits above them. */}
            <ProxiesHeading id="proc-proxies">{processesCopy.gatewayProxies}</ProxiesHeading>
          </>
        }
        search={{
          label: processesCopy.search,
          placeholder: processesCopy.placeholder,
          value: l.text,
          onChange: l.setText,
        }}
        filters={
          <ListFilters params={l.params} setFilter={l.setFilter} tags={[]}>
            {/* A visible label and divider set these apart from the health chips. */}
            <ShowOnly>
              <span className="label" aria-hidden="true">
                {processesCopy.showOnly}
              </span>
              <StateFilter
                label={processesCopy.showOnly}
                options={SHOW.map((s) => ({ value: s.key, label: s.label }))}
                selected={shown}
                // An unpressed chip removes its param: the page never sends false.
                onChange={(v) =>
                  l.setFilter(Object.fromEntries(SHOW.map((s) => [s.key, v.includes(s.key) ? "true" : null])))
                }
              />
            </ShowOnly>
          </ListFilters>
        }
        onClearFilters={l.filtered ? l.clearFilters : undefined}
        stylesOverride={stylesOverride}
      >
        <ObjectTable
          label={processesCopy.gatewayProxies}
          installId={l.id}
          noun={NOUN}
          columns={cols}
          rowRef={rowRef}
          page={page}
          loading={!l.data}
          busy={l.busy}
          emptyTitle={l.filtered ? listCopy.emptyFiltered(NOUN[1]) : listCopy.empty(NOUN[1])}
          onClearFilters={l.filtered ? l.clearFilters : undefined}
          sort={NO_SORT}
          onSort={noop}
          onPage={l.setCursor}
        />
      </ListFrame>
    </StaleWhen>
  );
}
