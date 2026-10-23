import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { AlertTriangle, Check, HelpCircle, X } from "lucide-react";
import type { HealthState } from "../theme/tokens";
import { useShownState } from "./staleView";

// region Styles
const Chip = styled.span<{ $state: HealthState; $size: number; $stylesOverride?: SerializedStyles }>`
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: ${(p) => p.$size}px;
  height: ${(p) => p.$size}px;
  border-radius: ${(p) => p.theme.radius.sm};
  background: ${(p) => p.theme.color.status[p.$state].iconBg};
  color: ${(p) => p.theme.color.status[p.$state].iconFg};
  ${(p) => p.$stylesOverride}
`;

// region Helpers
const glyphs = { healthy: Check, warning: AlertTriangle, error: X, unknown: HelpCircle } as const;

// region Component
interface HealthIconProps {
  state: HealthState;
  size?: number;
  /** A notice's icon, not an object's state: it ignores the stale view (design §6.3 covers states only). */
  notice?: boolean;
  stylesOverride?: SerializedStyles;
}

/** The icon half of the health vocabulary; always paired with text by callers. */
export function HealthIcon({ state: given, size = 16, notice, stylesOverride }: HealthIconProps) {
  const shown = useShownState(given);
  const state = notice ? given : shown;
  const Glyph = glyphs[state];
  return (
    <Chip
      $state={state}
      $size={size}
      $stylesOverride={stylesOverride}
      data-testid={`health-icon-${state}`}
      aria-hidden="true"
    >
      <Glyph size={size - 5} strokeWidth={3} />
    </Chip>
  );
}
