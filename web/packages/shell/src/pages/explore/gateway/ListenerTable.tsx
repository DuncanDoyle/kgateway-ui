import { css, type SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { ChevronDown, ChevronRight } from "lucide-react";
import { Fragment, type ReactNode, useState } from "react";
import type { AllowedRoutes, LabelSelector, Listener } from "../../../api/types";
import { ConditionList, conditionState } from "../../../explore/ConditionList";
import { TableFrame, TableStyles } from "../../../explore/ObjectTable";
import { HealthIcon } from "../../../health/HealthIcon";
import { stateLabel } from "../../../health/state";
import { ResourceName } from "../../../resource/ResourceName";
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
`;

// region Helpers
const NARROW = "(max-width: 899px)";
const RANK: Record<HealthState, number> = { healthy: 0, unknown: 1, warning: 2, error: 3 };

function selectorText(s: LabelSelector): string {
  const labels = Object.entries(s.matchLabels ?? {}).map(([k, v]) => `${k}=${v}`);
  const exprs = (s.matchExpressions ?? []).map(
    (e) => `${e.key} ${e.operator}${e.values ? ` (${e.values.join(", ")})` : ""}`,
  );
  return [...labels, ...exprs].join(", ") || "Any labels";
}

function namespacesText(a: AllowedRoutes): string {
  const { from, selector } = a.namespaces;
  if (from === "Same") return "Same namespace";
  if (from === "All") return "All namespaces";
  if (from === "Selector") return `Namespaces with ${selector ? selectorText(selector) : "any labels"}`;
  return from;
}

/** The listener's worst condition state, or unknown when the controller wrote no status for it. */
function listenerState(l: Listener): { state: HealthState; failing: string[] } {
  const conds = l.status?.conditions ?? [];
  if (!l.status || !conds.length) return { state: "unknown", failing: [] };
  const state = conds.map(conditionState).reduce((a, b) => (RANK[b] > RANK[a] ? b : a), "healthy" as HealthState);
  return { state, failing: conds.filter((c) => conditionState(c) === "error").map((c) => `${c.type}: ${c.status}`) };
}

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
    <Muted>None</Muted>
  );

const allowed = (l: Listener): ReactNode => (
  <>
    {namespacesText(l.allowedRoutes)}
    <div>
      {l.allowedRoutes.kinds.length ? (
        l.allowedRoutes.kinds.map((k) => k.kind).join(", ")
      ) : (
        <Muted>Kinds for the protocol</Muted>
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
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());
  const toggle = (name: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (!next.delete(name)) next.add(name);
      return next;
    });
  const cols = narrow ? 7 : 9;
  return (
    <TableFrame stylesOverride={css`${tableExtra}${stylesOverride}`}>
      <TableStyles aria-label="Listeners">
        <colgroup>
          <col style={{ width: "40px" }} />
          <col style={{ width: "130px" }} />
          <col />
          <col style={{ width: "72px" }} />
          <col style={{ width: "96px" }} />
          <col style={{ width: narrow ? "24%" : "15%" }} />
          {narrow ? null : <col style={{ width: "15%" }} />}
          {narrow ? null : <col style={{ width: "18%" }} />}
          <col style={{ width: "130px" }} />
        </colgroup>
        <thead>
          <tr>
            <th scope="col">
              <VisuallyHidden>Details</VisuallyHidden>
            </th>
            <th scope="col">Status</th>
            <th scope="col">Listener</th>
            <th scope="col">Port</th>
            <th scope="col">Protocol</th>
            <th scope="col">Hostname</th>
            {narrow ? null : <th scope="col">TLS</th>}
            {narrow ? null : <th scope="col">Allowed routes</th>}
            <th scope="col">Attached routes</th>
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
                      aria-label={`${isOpen ? "Hide" : "Show"} conditions for listener ${l.name}`}
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
                      {l.status ? stateLabel(s.state) : "No status"}
                    </State>
                    {s.failing.length ? <Muted className="failing">{s.failing.join(", ")}</Muted> : null}
                  </td>
                  <td>{l.name}</td>
                  <td>{l.port ?? <Muted>Not set</Muted>}</td>
                  <td>{l.protocol}</td>
                  <td>{l.hostname ?? <Muted>Any hostname</Muted>}</td>
                  {narrow ? null : <td>{tls(l)}</td>}
                  {narrow ? null : <td>{allowed(l)}</td>}
                  <td>{l.status ? l.status.attachedRoutes : <Muted>Not reported</Muted>}</td>
                </tr>
                {isOpen ? (
                  <tr data-testid={`listener-${l.name}-details`}>
                    <td colSpan={cols}>
                      {narrow ? (
                        <dl className="spec">
                          <dt>TLS</dt>
                          <dd>{tls(l)}</dd>
                          <dt>Allowed routes</dt>
                          <dd>{allowed(l)}</dd>
                        </dl>
                      ) : null}
                      <ConditionList
                        conditions={l.status?.conditions ?? []}
                        generation={generation}
                        empty={
                          l.status
                            ? "No conditions reported."
                            : "The controller has not written status for this listener."
                        }
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
