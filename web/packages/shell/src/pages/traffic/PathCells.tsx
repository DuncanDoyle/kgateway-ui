import styled from "@emotion/styled";
import { useContext } from "react";
import type { LinkedRef, PathRow } from "../../api/types";
import { columnCopy, routeCopy } from "../../copy/explorer";
import { actionCopy, cellCopy, noteCopy, shareCopy } from "../../copy/traffic";
import { backendPath, linkTarget } from "../../explore/objectPath";
import { HealthIcon } from "../../health/HealthIcon";
import { StaleView, useShownState } from "../../health/staleView";
import { stateLabel } from "../../health/state";
import { Tooltip } from "../../shell/Tooltip";
import type { HealthState } from "../../theme/tokens";
import { Muted, NameCell, nameTip, OneLine, refText } from "../explore/listParts";
import { shareText, signalDetails, signalLines } from "./pathText";

// region Styles
const State = styled.span<{ $state: HealthState }>`
  display: inline-flex;
  align-items: center;
  gap: ${(p) => p.theme.space(1.5)};
  font-weight: ${(p) => p.theme.font.weight.semibold};
  font-size: ${(p) => p.theme.font.size.sm};
  white-space: nowrap;
  color: ${(p) => p.theme.color.status[p.$state].fg};
  border-radius: ${(p) => p.theme.radius.sm};
  &[tabindex] {
    cursor: help;
  }
  &[tabindex]:hover {
    background: ${(p) => p.theme.color.status.unknown.stripBg};
  }
  &[tabindex]:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
`;
/** Two parts on one line; the first shrinks with an ellipsis, the second keeps its width. */
const Pair = styled.span`
  display: flex;
  align-items: center;
  gap: ${(p) => p.theme.space(1)};
  min-width: 0;
  .second {
    flex: none;
    font-size: ${(p) => p.theme.font.size.sm};
    color: ${(p) => p.theme.color.textMuted};
    white-space: nowrap;
  }
`;
const Tag = styled.span`
  flex: none;
  padding: 0 6px;
  border-radius: ${(p) => p.theme.radius.sm};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  font-size: ${(p) => p.theme.font.size.xs};
  color: ${(p) => p.theme.color.textMuted};
  white-space: nowrap;
`;
export const Code = styled.span`
  font-family: ${(p) => p.theme.font.mono};
  font-size: ${(p) => p.theme.font.size.code};
`;
const Flag = styled.span<{ $state: HealthState }>`
  display: inline-flex;
  align-items: center;
  gap: 4px;
  white-space: nowrap;
  font-size: ${(p) => p.theme.font.size.sm};
  color: ${(p) => p.theme.color.status[p.$state].fg};
`;

// region Component
/** The row's state with icon, colour and text; its signals in a tooltip. Under a stale view it shows unknown. */
export function StateCell({ r }: { r: Pick<PathRow, "problem"> }) {
  const state = useShownState(r.problem.state);
  const lines = state === r.problem.state ? signalDetails(r) : [];
  const badge = (
    <State $state={state} tabIndex={lines.length ? 0 : undefined} data-testid="path-state">
      <HealthIcon state={state} />
      {stateLabel(state)}
    </State>
  );
  return lines.length ? (
    <Tooltip label={lines.join("\n")} placement="start" wrap>
      {badge}
    </Tooltip>
  ) : (
    badge
  );
}

export function GatewayCell({ r, installId }: { r: PathRow; installId: string }) {
  const listener = r.listener.port ? `${r.listener.name} :${r.listener.port}` : r.listener.name;
  const tip = [refText(r.gateway), r.listenerSet ? cellCopy.viaListenerSet(refText(r.listenerSet)) : "", listener]
    .filter(Boolean)
    .join(" · ");
  return (
    <Tooltip label={tip} stylesOverride={nameTip}>
      <Pair>
        <NameCell r={r.gateway} installId={installId} />
        <span className="second">{listener}</span>
      </Pair>
    </Tooltip>
  );
}

export function RouteCell({ r, installId }: { r: PathRow; installId: string }) {
  const last = r.routes[r.routes.length - 1] as LinkedRef;
  const chain = r.routes.map(refText).join(" → ");
  const rule = routeCopy.rule(r.rule + 1);
  return (
    <Tooltip label={`${chain} · ${rule}`} stylesOverride={nameTip}>
      <Pair>
        <NameCell
          r={last}
          installId={installId}
          to={linkTarget(installId, last, undefined, { rule: String(r.rule + 1) })}
        />
        {r.delegated ? <Tag>{cellCopy.delegated}</Tag> : null}
        <span className="second">{rule}</span>
      </Pair>
    </Tooltip>
  );
}

export function TargetCell({ r, installId }: { r: PathRow; installId: string }) {
  if (r.backend) {
    const port = r.port ? `:${r.port}` : "";
    return (
      <Tooltip label={`${r.backend.kind} ${refText(r.backend)}${port}`} stylesOverride={nameTip}>
        <Pair>
          <NameCell r={r.backend} installId={installId} to={backendPath(installId, r.backend)} />
          {port ? <span className="second">{port}</span> : null}
          {r.mirror ? <Tag>{columnCopy.mirror}</Tag> : null}
        </Pair>
      </Tooltip>
    );
  }
  if (r.action === "delegate" && r.delegation) {
    const d = r.delegation;
    return <OneLine text={cellCopy.delegateTo(`${d.namespace}/${d.name ?? d.label ?? "*"}`)} />;
  }
  return <Muted data-testid="path-action">{actionCopy[r.action as keyof typeof actionCopy]}</Muted>;
}

export function ShareCell({ r }: { r: PathRow }) {
  const s = shareText(r);
  // A flag is a state, so a stale view drops it with every other state.
  const flag = useContext(StaleView) ? undefined : s.flag;
  const body = (
    <Pair>
      <span>{r.shareState === "not-shown" ? <Muted>{s.text}</Muted> : s.text}</span>
      {flag ? (
        <Flag $state={flag.state}>
          <HealthIcon state={flag.state} size={14} notice />
          {flag.text}
        </Flag>
      ) : null}
    </Pair>
  );
  return r.shareState === "not-shown" && r.action === "backend" ? (
    <OneLine text={shareCopy.notShownHint}>{body}</OneLine>
  ) : (
    body
  );
}

/** The signals behind the state in plain words, codes in the tooltip; blank under a stale view, as the state is. */
export function WhyCell({ r }: { r: Pick<PathRow, "problem" | "note"> }) {
  if (useContext(StaleView)) return null;
  const note = r.note ? [noteCopy[r.note]] : [];
  const lines = [...signalLines(r), ...note];
  return lines.length ? (
    <OneLine text={[...signalDetails(r), ...note].join("\n")} placement="end">
      {lines.join(" · ")}
    </OneLine>
  ) : null;
}
