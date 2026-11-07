import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import type { RowReason } from "../api/types";
import { rowReasonCopy } from "../copy/explorer";
import { Tooltip } from "../shell/Tooltip";
import type { HealthState } from "../theme/tokens";
import { HealthIcon } from "./HealthIcon";
import { useShownState } from "./staleView";
import { stateLabel } from "./state";

// region Styles
const Badge = styled.span<{ state: HealthState; stylesOverride?: SerializedStyles }>`
  display: inline-flex;
  align-items: center;
  gap: ${(p) => p.theme.space(1.5)};
  font-weight: ${(p) => p.theme.font.weight.semibold};
  font-size: ${(p) => p.theme.font.size.sm};
  white-space: nowrap;
  color: ${(p) => p.theme.color.status[p.state].fg};
  ${(p) => p.stylesOverride}
`;
const Focusable = styled.span`
  display: inline-flex;
  cursor: help;
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    background: ${(p) => p.theme.color.status.unknown.stripBg};
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
`;

// region Helpers
/** The tooltip text for a row reason. */
export function rowReasonText(r: RowReason): string {
  return r.kind === "finding" ? rowReasonCopy.finding(r.code, r.message, r.more) : rowReasonCopy[r.kind];
}

// region Component
interface HealthBadgeProps {
  state: HealthState;
  label?: string;
  /** Why the state is not healthy: shown in a tooltip on hover and keyboard focus. */
  reason?: RowReason;
  stylesOverride?: SerializedStyles;
}

/** Icon + colour + text for a health state. */
export function HealthBadge({ state: given, label, reason, stylesOverride }: HealthBadgeProps) {
  const state = useShownState(given);
  const badge = (
    <Badge state={state} stylesOverride={stylesOverride}>
      <HealthIcon state={state} />
      {/* A stale view replaces the state, so a label naming the old one goes too. */}
      {state === given ? (label ?? stateLabel(state)) : stateLabel(state)}
    </Badge>
  );
  // The reason explains the given state only, so a stale view drops it with that state.
  if (!reason || state !== given || state === "healthy") return badge;
  return (
    <Tooltip label={rowReasonText(reason)} placement="start" wrap>
      <Focusable tabIndex={0} data-testid="health-reason">
        {badge}
      </Focusable>
    </Tooltip>
  );
}
