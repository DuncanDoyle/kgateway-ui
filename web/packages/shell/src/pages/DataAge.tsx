import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useNow } from "../api/clock";
import { isStale } from "../api/hooks";
import { HealthIcon } from "../health/HealthIcon";
import { ago } from "./install/HealthStrip";

// region Styles
const Age = styled.span<{ stylesOverride?: SerializedStyles }>`
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
  color: ${(p) => p.theme.color.textMuted};
  font-size: ${(p) => p.theme.font.size.sm};
  ${(p) => p.stylesOverride}
`;

// region Component
interface DataAgeProps {
  collectedAt: string;
  stylesOverride?: SerializedStyles;
}

/** "Last checked 5s ago", or stale with an unknown icon (D-006). Owns the 1 s clock, so its page does not re-render on each tick. */
export function DataAge({ collectedAt, stylesOverride }: DataAgeProps) {
  const now = useNow();
  const stale = isStale(collectedAt, now);
  return (
    <Age data-testid="data-age" stylesOverride={stylesOverride}>
      {stale ? <HealthIcon state="unknown" /> : null}
      {stale ? `Data is stale. Last checked ${ago(collectedAt, now)}` : `Last checked ${ago(collectedAt, now)}`}
    </Age>
  );
}
