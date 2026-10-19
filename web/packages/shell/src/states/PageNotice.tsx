import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import type { ApiError } from "../api/client";
import { HealthIcon } from "../health/HealthIcon";
import type { HealthState } from "../theme/tokens";

// region Styles
// Status is the icon plus a full status-tinted border and background; never a left stripe.
const Box = styled.div<{ $state: HealthState; stylesOverride?: SerializedStyles }>`
  display: flex;
  align-items: center;
  gap: ${(p) => p.theme.space(2)};
  margin-bottom: ${(p) => p.theme.space(3)};
  padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.status[p.$state].stripBg};
  border: 1px solid ${(p) => p.theme.color.status[p.$state].stripBorder};
  color: ${(p) => p.theme.color.text};
  font-size: ${(p) => p.theme.font.size.sm};
  overflow-wrap: anywhere;
  ${(p) => p.stylesOverride}
`;

// region Component
interface PageNoticeProps {
  /** The error of the latest poll; the page keeps showing the data it loaded before. */
  error: ApiError;
  /** The page's error title, e.g. "Cannot load findings". */
  title: string;
  stylesOverride?: SerializedStyles;
}

/** Inline notice over data that is still shown after a poll failed. */
export function PageNotice({ error, title, stylesOverride }: PageNoticeProps) {
  const connecting = error.status === 503;
  return (
    <Box
      $state={connecting ? "unknown" : "error"}
      role={connecting ? "status" : "alert"}
      data-testid="page-notice"
      stylesOverride={stylesOverride}
    >
      <HealthIcon state={connecting ? "unknown" : "error"} />
      {connecting ? "Waiting for the cluster" : `${title}: ${error.message}`}
    </Box>
  );
}
