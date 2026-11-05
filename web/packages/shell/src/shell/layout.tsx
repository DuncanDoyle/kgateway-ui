import styled from "@emotion/styled";
import type { ReactNode } from "react";
import { useMedia } from "./useMedia";

// region Constants
/** Content is fluid up to maxWidth px; detail pages go two-column from twoColumnFrom px; prose stays proseMax wide. */
export const layout = { maxWidth: 2400, twoColumnFrom: 1600, proseMax: "80ch" };

/** True from layout.twoColumnFrom px, the same query as the columns' CSS, so what renders matches the grid. */
export function useTwoColumns(): boolean {
  return useMedia(`(min-width: ${layout.twoColumnFrom}px)`);
}

// region Styles
const Columns = styled.div<{ two: boolean }>`
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: ${(p) => p.theme.space(5)};
  align-items: start;
  ${(p) =>
    p.two
      ? `@media (min-width: ${layout.twoColumnFrom}px) { grid-template-columns: minmax(0, 1fr) minmax(320px, 400px); }`
      : ""}
`;

// region Component
interface DetailColumnsProps {
  /** The object's structure. */
  main: ReactNode;
  /** Contributed sections and longer auxiliary content; with none, the page stays one column. */
  side: ReactNode;
}

/**
 * A detail page's body: one column with the side after the main, two columns from 1600 px. Pages move blocks between
 * columns by rendering them in one or the other ({@link useTwoColumns}), never by CSS order, so tab order follows the eye.
 */
export function DetailColumns({ main, side }: DetailColumnsProps) {
  return (
    <Columns two={!!side} data-testid="detail-columns">
      <div data-testid="detail-main" style={{ minWidth: 0 }}>
        {main}
      </div>
      {side ? (
        <aside data-testid="detail-side" style={{ minWidth: 0 }}>
          {side}
        </aside>
      ) : null}
    </Columns>
  );
}
