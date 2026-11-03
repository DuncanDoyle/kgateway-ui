import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import type { ReactNode } from "react";
import type { ProcessSummary, ProxySummary } from "../api/types";
import { diagnosticsCopy, findingsCopy } from "../copy/explorer";
import { HealthBadge } from "../health/HealthBadge";
import type { HealthState } from "../theme/tokens";

// region Styles
/** Height of one line, fixed from the first render so polled counts shift nothing. */
export const PROCESS_LINE_H = 36;

const Line = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  grid-template-columns: 112px minmax(0, 1fr);
  align-items: center;
  gap: ${(p) => p.theme.space(3)};
  min-height: ${PROCESS_LINE_H}px;
  .label {
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  .facts {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(3)};
    min-width: 0;
    font-size: ${(p) => p.theme.font.size.sm};
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
  }
  ${(p) => p.stylesOverride}
`;

// region Helpers
function controllerState(s: ProcessSummary): HealthState {
  if (!s.observed || s.ready === undefined || s.desired === undefined) return "unknown";
  if (s.desired > 0 && s.ready === 0) return "error";
  return s.ready >= s.desired ? "healthy" : "warning";
}

function proxiesState(s: ProxySummary): HealthState {
  if (s.gateways === 0 || s.notObserved === s.gateways) return "unknown";
  if (s.rejecting > 0) return "error";
  if (s.skew > 0 || s.restarting > 0 || s.ready < s.gateways - s.notObserved) return "warning";
  return "healthy";
}

// region Component
type ProcessLineProps = (
  | { kind: "controller"; summary: ProcessSummary }
  | { kind: "proxies"; summary: ProxySummary }
) & { stylesOverride?: SerializedStyles };

/** One line of the overview's Processes section: the controller or the Gateways' proxies (design §6). */
export function ProcessLine(props: ProcessLineProps) {
  const { stylesOverride } = props;
  let state: HealthState;
  let facts: ReactNode;
  if (props.kind === "controller") {
    const s = props.summary;
    state = controllerState(s);
    facts = !s.observed ? (
      <span className="muted">{diagnosticsCopy.notObserved}</span>
    ) : (
      <>
        {s.ready !== undefined && s.desired !== undefined ? (
          <span>{diagnosticsCopy.readyOf(s.ready, s.desired)}</span>
        ) : null}
        {s.restricted ? (
          <span className="muted">{findingsCopy.restricted}</span>
        ) : s.version ? (
          <span>{diagnosticsCopy.version(s.version)}</span>
        ) : null}
      </>
    );
  } else {
    const s = props.summary;
    state = proxiesState(s);
    facts =
      s.gateways === 0 ? (
        <span className="muted">{diagnosticsCopy.noGateways}</span>
      ) : (
        <>
          <span>{diagnosticsCopy.gatewaysReady(s.ready, s.gateways)}</span>
          {s.rejecting ? <span>{diagnosticsCopy.rejecting(s.rejecting)}</span> : null}
          {s.skew ? <span>{diagnosticsCopy.skew(s.skew)}</span> : null}
          {s.restarting ? <span>{diagnosticsCopy.restarting(s.restarting)}</span> : null}
          {s.notObserved ? <span className="muted">{diagnosticsCopy.gatewaysNotObserved(s.notObserved)}</span> : null}
        </>
      );
  }
  return (
    <Line data-testid={`process-line-${props.kind}`} stylesOverride={stylesOverride}>
      <span className="label">
        {props.kind === "controller" ? diagnosticsCopy.controller : diagnosticsCopy.proxies}
      </span>
      <span className="facts">
        <HealthBadge state={state} />
        {facts}
      </span>
    </Line>
  );
}
