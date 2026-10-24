import { css, type SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { ChevronDown, ChevronRight, Info } from "lucide-react";
import { Fragment, type ReactNode, useContext, useState } from "react";
import type { AllowedRoutes, LabelSelector, Listener } from "../../../api/types";
import { columnCopy, detailCopy, listCopy, listenerCopy, viewACopy } from "../../../copy/explorer";
import { ComputedBadge } from "../../../explore/ComputedBadge";
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
import { Muted, OneLine, refText, wrapName } from "../listParts";

// region Styles
const State = styled.span`
  display: inline-flex;
  align-items: center;
  gap: 6px;
`;
/** Count, isolation note and compact badge on one line; the badge shrinks first. */
const Counted = styled.span`
  display: flex;
  align-items: center;
  gap: ${(p) => p.theme.space(2)};
  min-width: 0;
  .count {
    flex: none;
  }
`;
const NoteIcon = styled.span`
  display: inline-flex;
  flex: none;
  padding: 2px;
  cursor: help;
  border-radius: ${(p) => p.theme.radius.sm};
  color: ${(p) => p.theme.color.textMuted};
  &:hover {
    color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 8%, ${(p) => p.theme.color.surface});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 16%, ${(p) => p.theme.color.surface});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
  }
`;
const tableExtra = css`
  .failing {
    font-size: 0.85em;
  }
  dl.spec {
    margin-bottom: 12px;
  }
  .computed-details {
    display: grid;
    gap: 4px;
    margin-bottom: 12px;
    font-size: 0.875em;
  }
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

/** A cell's text, for its Tooltip and one-line form. */
const tlsText = (l: Listener) =>
  l.tls ? [l.tls.mode, ...l.tls.certificateRefs.map((c) => refText(c))].join(" · ") : columnCopy.none;
const allowedText = (l: Listener) =>
  `${namespacesText(l.allowedRoutes)} · ${
    l.allowedRoutes.kinds.length ? l.allowedRoutes.kinds.map((k) => k.kind).join(", ") : listenerCopy.defaultKinds
  }`;

/** The listener's state, and its failing conditions unless stale (then they stay on expand), on one line. */
function StateCell({ l, stale }: { l: Listener; stale: boolean }) {
  const s = listenerState(l);
  const label = l.status ? stateLabel(stale ? "unknown" : s.state) : listenerCopy.noStatus;
  const failing = s.failing.length && !stale ? s.failing.join(", ") : "";
  return (
    <OneLine text={failing ? `${label} · ${failing}` : label}>
      <State>
        <HealthIcon state={s.state} />
        {label}
      </State>
      {failing ? <Muted className="failing"> · {failing}</Muted> : null}
    </OneLine>
  );
}

/** A number is shown only when view A knows it: never for unknown or restricted (design §8.6). */
const knownCount = (l: Listener) =>
  l.computed && l.computedRoutes !== undefined && l.computed.state !== "unknown" && l.computed.state !== "restricted"
    ? l.computedRoutes
    : undefined;

/** The computed count with its check; the isolation note is an icon whose text is its label and tooltip. */
function ComputedCell({ l }: { l: Listener }) {
  if (!l.computed) return <Muted>{columnCopy.none}</Muted>;
  const n = knownCount(l);
  return (
    <Counted data-testid="computed-cell">
      {n !== undefined ? <span className="count">{n}</span> : null}
      {l.isolationNote ? (
        <Tooltip label={viewACopy.isolationNote} wrap placement="end">
          <NoteIcon tabIndex={0} role="img" aria-label={viewACopy.isolationNote}>
            <Info size={14} aria-hidden="true" />
          </NoteIcon>
        </Tooltip>
      ) : null}
      <ComputedBadge computed={l.computed} compact />
    </Counted>
  );
}

// region Component
interface ListenerTableProps {
  listeners: Listener[];
  /** The object's generation, for stale listener conditions. */
  generation: number;
  stylesOverride?: SerializedStyles;
}

/**
 * Spec listeners with their status: the controller's attached count, and from 2.3 view A's computed count with its check.
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
  // A server before 2.3 sends no computed facts, so the column is left out.
  const computed = listeners.some((l) => l.computed);
  const cols = (narrow ? 6 : 9) + (computed ? 1 : 0);
  return (
    <TableFrame stylesOverride={css`${tableExtra}${stylesOverride}`}>
      <TableStyles aria-label={listenerCopy.table}>
        <colgroup>
          <col style={{ width: "40px" }} />
          <col style={{ width: "120px" }} />
          <col />
          <col style={{ width: "64px" }} />
          <col style={{ width: "88px" }} />
          {narrow ? null : <col style={{ width: computed ? "10%" : "15%" }} />}
          {narrow ? null : <col style={{ width: computed ? "10%" : "15%" }} />}
          {narrow ? null : <col style={{ width: computed ? "12%" : "18%" }} />}
          <col style={{ width: computed ? "116px" : "130px" }} />
          {computed ? <col style={{ width: narrow ? "172px" : "25%" }} /> : null}
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
            {computed ? <th scope="col">{viewACopy.computedRoutes}</th> : null}
          </tr>
        </thead>
        <tbody>
          {listeners.map((l) => {
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
                    <StateCell l={l} stale={stale} />
                  </td>
                  <td>
                    <OneLine text={l.name} />
                  </td>
                  <td>{l.port ?? <Muted>{listenerCopy.notSet}</Muted>}</td>
                  <td>{l.protocol}</td>
                  {narrow ? null : <td>{l.hostname ? <OneLine text={l.hostname} /> : hostname(l)}</td>}
                  {narrow ? null : (
                    <td>
                      <OneLine text={tlsText(l)} />
                    </td>
                  )}
                  {narrow ? null : (
                    <td>
                      <OneLine text={allowedText(l)} />
                    </td>
                  )}
                  <td>{l.status ? l.status.attachedRoutes : <Muted>{listenerCopy.notReported}</Muted>}</td>
                  {computed ? (
                    <td>
                      <ComputedCell l={l} />
                    </td>
                  ) : null}
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
                      {l.computed ? (
                        <div className="computed-details">
                          <span>
                            {viewACopy.computedRoutes}: {knownCount(l) ?? <Muted>{columnCopy.none}</Muted>}
                          </span>
                          <ComputedBadge computed={l.computed} />
                          {l.isolationNote ? <Muted>{viewACopy.isolationNote}</Muted> : null}
                        </div>
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
