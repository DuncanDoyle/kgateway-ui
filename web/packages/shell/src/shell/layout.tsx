import styled from "@emotion/styled";
import type { ReactNode } from "react";

// region Constants
/** Page widths in px: content is fluid up to maxWidth, detail pages use two columns from twoColumnFrom; prose stays proseMax wide. */
export const layout = { maxWidth: 2400, twoColumnFrom: 1600, proseMax: "80ch" };

// region Styles
const Columns = styled.div`
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: ${(p) => p.theme.space(5)};
  align-items: start;
  @media (min-width: ${layout.twoColumnFrom}px) {
    grid-template-columns: minmax(0, 1fr) minmax(320px, 30rem);
  }
`;

// region Component
interface DetailColumnsProps {
  /** The object's structure. */
  main: ReactNode;
  /** Conditions, findings, related objects and contributed sections; omitted when null. */
  side: ReactNode;
}

/** A detail page's body: one column with the side after the main, two columns from 1600 px. */
export function DetailColumns({ main, side }: DetailColumnsProps) {
  return (
    <Columns data-testid="detail-columns">
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
