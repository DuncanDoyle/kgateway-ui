import { css } from "@emotion/react";
import type { Theme } from "../theme/tokens";

// The header is the ink band in every theme, so its tints derive from headerText over headerBg.
// themes.test.ts checks contrast at these exact percentages.
export const HEADER_HOVER_PCT = 12;
export const HEADER_PRESS_PCT = 20;
/** Dimmed text on the band, e.g. a resource namespace; body textMuted is tuned for the page background. */
export const HEADER_MUTED_PCT = 75;

/** CSS colour: headerText mixed into headerBg at pct percent. */
export function headerTint(theme: Theme, pct: number): string {
  return `color-mix(in srgb, ${theme.color.headerText} ${pct}%, ${theme.color.headerBg})`;
}

/** ResourceName override for names shown on the header band. */
export const headerResourceName = (theme: Theme) => css`
  [data-part="namespace"] {
    color: ${headerTint(theme, HEADER_MUTED_PCT)};
  }
`;
