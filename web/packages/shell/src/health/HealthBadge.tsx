import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
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

// region Component
interface HealthBadgeProps {
  state: HealthState;
  label?: string;
  stylesOverride?: SerializedStyles;
}

/** Icon + colour + text for a health state. */
export function HealthBadge({ state: given, label, stylesOverride }: HealthBadgeProps) {
  const state = useShownState(given);
  return (
    <Badge state={state} stylesOverride={stylesOverride}>
      <HealthIcon state={state} />
      {label ?? stateLabel(state)}
    </Badge>
  );
}
