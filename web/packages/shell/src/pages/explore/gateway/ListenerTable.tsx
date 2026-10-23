import { css, type SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { ChevronDown, ChevronRight } from "lucide-react";
import { Fragment, type ReactNode, useContext, useState } from "react";
import type { AllowedRoutes, LabelSelector, Listener } from "../../../api/types";
import { columnCopy, detailCopy, listCopy, listenerCopy } from "../../../copy/explorer";
import { ConditionList, conditionState } from "../../../explore/ConditionList";
import { TableFrame, TableStyles } from "../../../explore/ObjectTable";
import { HealthIcon } from "../../../health/HealthIcon";
import { StaleView } from "../../../health/staleView";
import { stateLabel } from "../../../health/state";
import { ResourceName } from "../../../resource/ResourceName";
import { Tooltip } from "../../../shell/Tooltip";
import { useMedia } from "../../../shell/useMedia";
import { VisuallyHidden } from "../../../states/VisuallyHidden";
import type { HealthState } from "../../../theme/tokens";
import { Muted, wrapName } from "../listParts";

// region Styles
const State = styled.span`
  display: inline-flex;
  align-items: center;
  gap: 6px;
`;
const tableExtra = css`
  .failing {
    display: block;
    font-size: 0.85em;
  }
  dl.spec {
    margin-bottom: 12px;
  }
  /* A listener name never breaks mid-word: it truncates, with the full name as a tooltip. */
  .name {
    display: block;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
`;

const nameTip = css`
  display: flex;
  max-width: 100%;
  min-width: 0;
`;

// region Helpers
const NARROW = "(max-width: 899px)";
const RANK: Record<HealthState, number> = { healthy: 0, unknown: 1, warning: 2, error: 3 };

function selectorText(s: LabelSelector): string {
  const labels = Object.entries(s.matchLabels ?? {}).map(([k, v]) => `${k}=${v}`);
  const exprs = (s.matchExpressions ?? []).map(
    (e) => `${e.key} ${e.operator}${e.values ? ` (${e.values.join(", ")})` : ""}`,
  );
  return [...labels, ...exprs].join(", ") || listenerCopy.anyLabels;
}

function namespacesText(a: AllowedRoutes): string {
  const { from, selector } = a.namespaces;
  if (from === "Same") return listenerCopy.sameNamespace;
  if (from === "All") return listenerCopy.allNamespaces;
  if (from === "Selector")
    return listenerCopy.namespacesWith(selector ? selectorText(selector) : listenerCopy.anyLabelsInline);
  return from;
}

/** The listener's worst condition state, or unknown when the controller wrote no status for it. */
function listenerState(l: Listener): { state: HealthState; failing: string[] } {
  const conds = l.status?.conditions ?? [];
  if (!l.status || !conds.length) return { state: "unknown", failing: [] };
  const state = conds.map(conditionState).reduce((a, b) => (RANK[b] > RANK[a] ? b : a), "healthy" as HealthState);
  return { state, failing: conds.filter((c) => conditionState(c) === "error").map((c) => `${c.type}: ${c.status}`) };
}

const hostname = (l: Listener): ReactNode => l.hostname ?? <Muted>{listenerCopy.anyHostname}</Muted>;

const tls = (l: Listener): ReactNode =>
  l.tls ? (
    <>
      {l.tls.mode}
      {/* Certificates are names only: Secrets are never read. */}
      {l.tls.certificateRefs.map((c) => (
        <div key={`${c.namespace}/${c.name}`}>
          <ResourceName namespace={c.namespace} name={c.name} stylesOverride={wrapName} />
        </div>
      ))}
    </>
  ) : (
    <Muted>{columnCopy.none}</Muted>
  );

const allowed = (l: Listener): ReactNode => (
  <>
    {namespacesText(l.allowedRoutes)}
    <div>
      {l.allowedRoutes.kinds.length ? (
        l.allowedRoutes.kinds.map((k) => k.kind).join(", ")
      ) : (
        <Muted>{listenerCopy.defaultKinds}</Muted>
      )}
    </div>
  </>
);

// region Component
interface ListenerTableProps {
  listeners: Listener[];
  /** The object's generation, for stale listener conditions. */
  generation: number;
  stylesOverride?: SerializedStyles;
}

/**
 * Spec listeners with their status. Attached routes is the controller's status count (2.2 shows no computed count).
 * Conditions open per row; expanded rows are kept by listener name, so polling never collapses them.
 */
export function ListenerTable({ listeners, generation, stylesOverride }: ListenerTableProps) {
  const narrow = useMedia(NARROW);
  const stale = useContext(StaleView);
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());
  const toggle = (name: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (!next.delete(name)) next.add(name);
      return next;
    });
  // Below 900 px hostname, TLS and allowed routes move into the expanded row, as list secondary columns do.
  const cols = narrow ? 6 : 9;
  return (
    <TableFrame stylesOverride={css`${tableExtra}${stylesOverride}`}>
      <TableStyles aria-label={listenerCopy.table}>
        <colgroup>
          <col style={{ width: "40px" }} />
          <col style={{ width: "130px" }} />
          <col />
          <col style={{ width: "72px" }} />
          <col style={{ width: "96px" }} />
          {narrow ? null : <col style={{ width: "15%" }} />}
          {narrow ? null : <col style={{ width: "15%" }} />}
          {narrow ? null : <col style={{ width: "18%" }} />}
          <col style={{ width: "130px" }} />
        </colgroup>
        <thead>
          <tr>
            <th scope="col">
              <VisuallyHidden>{listCopy.details}</VisuallyHidden>
            </th>
            <th scope="col">{columnCopy.status}</th>
            <th scope="col">{listenerCopy.listener}</th>
            <th scope="col">{listenerCopy.port}</th>
            <th scope="col">{listenerCopy.protocol}</th>
            {narrow ? null : <th scope="col">{listenerCopy.hostname}</th>}
            {narrow ? null : <th scope="col">{listenerCopy.tls}</th>}
            {narrow ? null : <th scope="col">{listenerCopy.allowedRoutes}</th>}
            <th scope="col">{columnCopy.attachedRoutes}</th>
          </tr>
        </thead>
        <tbody>
          {listeners.map((l) => {
            const s = listenerState(l);
            const isOpen = open.has(l.name);
            return (
              <Fragment key={l.name}>
                <tr data-row="" data-testid={`listener-${l.name}`}>
                  <td>
                    <button
                      type="button"
                      className="expand"
                      aria-expanded={isOpen}
                      aria-label={listenerCopy.toggle(isOpen, l.name)}
                      onClick={() => toggle(l.name)}
                    >
                      {isOpen ? (
                        <ChevronDown size={16} aria-hidden="true" />
                      ) : (
                        <ChevronRight size={16} aria-hidden="true" />
                      )}
                    </button>
                  </td>
                  <td>
                    <State>
                      <HealthIcon state={s.state} />
                      {l.status ? stateLabel(stale ? "unknown" : s.state) : listenerCopy.noStatus}
                    </State>
                    {/* Stale: the state only, as list rows show it; the conditions stay on expand. */}
                    {s.failing.length && !stale ? <Muted className="failing">{s.failing.join(", ")}</Muted> : null}
                  </td>
                  <td>
                    {/* Truncated names: the full name is in an app tooltip, reachable by keyboard when narrow. */}
                    <Tooltip label={l.name} stylesOverride={nameTip}>
                      <span className="name" tabIndex={narrow ? 0 : undefined}>
                        {l.name}
                      </span>
                    </Tooltip>
                  </td>
                  <td>{l.port ?? <Muted>{listenerCopy.notSet}</Muted>}</td>
                  <td>{l.protocol}</td>
                  {narrow ? null : <td>{hostname(l)}</td>}
                  {narrow ? null : <td>{tls(l)}</td>}
                  {narrow ? null : <td>{allowed(l)}</td>}
                  <td>{l.status ? l.status.attachedRoutes : <Muted>{listenerCopy.notReported}</Muted>}</td>
                </tr>
                {isOpen ? (
                  <tr data-testid={`listener-${l.name}-details`}>
                    <td colSpan={cols}>
                      {narrow ? (
                        <dl className="spec">
                          <dt>{listenerCopy.hostname}</dt>
                          <dd>{hostname(l)}</dd>
                          <dt>{listenerCopy.tls}</dt>
                          <dd>{tls(l)}</dd>
                          <dt>{listenerCopy.allowedRoutes}</dt>
                          <dd>{allowed(l)}</dd>
                        </dl>
                      ) : null}
                      <ConditionList
                        conditions={l.status?.conditions ?? []}
                        generation={generation}
                        empty={l.status ? detailCopy.noConditions : listenerCopy.noStatusWritten}
                      />
                    </td>
                  </tr>
                ) : null}
              </Fragment>
            );
          })}
        </tbody>
      </TableStyles>
    </TableFrame>
  );
}
