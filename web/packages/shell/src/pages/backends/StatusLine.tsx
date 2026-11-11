import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useContext } from "react";
import type { BackendDetail } from "../../api/types";
import { statusCopy, statusPhrase } from "../../copy/backends";
import { shareCopy } from "../../copy/traffic";
import { HealthIcon } from "../../health/HealthIcon";
import { StaleView } from "../../health/staleView";
import { stateLabel } from "../../health/state";
import type { HealthState } from "../../theme/tokens";

// region Styles
/** A full border and tint in the state's colour, never a left stripe; icon, label and text carry the state too. */
const Line = styled.p<{ $state: HealthState; stylesOverride?: SerializedStyles }>`
  display: flex;
  align-items: flex-start;
  gap: ${(p) => p.theme.space(2)};
  margin: 0 0 ${(p) => p.theme.space(5)};
  padding: ${(p) => p.theme.space(3)};
  border-radius: ${(p) => p.theme.radius.md};
  border: 1px solid ${(p) => p.theme.color.status[p.$state].fg};
  background: ${(p) => p.theme.color.status[p.$state].stripBg};
  .state {
    flex: none;
    font-weight: ${(p) => p.theme.font.weight.semibold};
    color: ${(p) => p.theme.color.status[p.$state].fg};
  }
  ${(p) => p.stylesOverride}
`;

// region Helpers
const upper = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

/**
 * The status line's sentence (Phase 6 design §8.2): who sends traffic here, then one fixed phrase per own signal.
 * Failure wording follows the protocol, and no status code is named. Stale data drops every claim of state.
 */
export function statusText(d: BackendDetail, stale: boolean): string {
  const s = d.exposure.summary;
  const others = d.consumers.length;
  const uses = [s.routes ? statusCopy.routes(s.routes, s.gateways) : "", others ? statusCopy.others(others) : ""]
    .filter(Boolean)
    .join(" and ");
  const rows = d.exposure.rows;
  const tcp =
    rows.length > 0 && rows.every((r) => r.routes[0]?.kind === "TCPRoute" || r.routes[0]?.kind === "TLSRoute");
  const fail = tcp ? shareCopy.connectionsFail : shareCopy.requestsFail;
  let phrases: string[];
  if (stale) phrases = [statusCopy.stale];
  else {
    phrases = [...new Set(d.status.map((x) => statusPhrase(x, fail, !!d.service)))];
    // A worse state than its own signals comes from its paths, e.g. a route's unresolved ref.
    const own = d.status.some((x) => x.state === d.problem.state);
    if (d.problem.state !== "healthy" && !own) phrases.push(statusCopy.pathProblem);
    if (!phrases.length) phrases = [statusCopy.fine];
  }
  return `${upper(uses || statusCopy.unused)}; ${phrases.join("; ")}.`;
}

// region Component
/** The backend's impact in one sentence, with its state as icon, colour and text. */
export function StatusLine({ detail, stylesOverride }: { detail: BackendDetail; stylesOverride?: SerializedStyles }) {
  const stale = useContext(StaleView);
  const state = stale ? "unknown" : detail.problem.state;
  return (
    <Line $state={state} data-testid="status-line" data-state={state} stylesOverride={stylesOverride}>
      <HealthIcon state={state} />
      <span className="state">{stateLabel(state)}</span>
      <span>{statusText(detail, stale)}</span>
    </Line>
  );
}
