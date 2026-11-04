import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useId } from "react";
import { Link } from "react-router-dom";
import type { GroupRow } from "../../api/types";
import { diagnosticsCopy } from "../../copy/explorer";
import { DiagnosticsGroupRow, GROUP_ROW_H, GROUP_ROW_H_NARROW, GROUP_ROW_NARROW } from "../../diagnostics/GroupRow";
import { installPath } from "../../shell/paths";
import { Shimmer } from "../../states/Shimmer";

// region Styles
/** Height of the "Show all" footer, fixed like the rows above it. */
export const SHOW_ALL_H = 40;

const Box = styled.div<{ stylesOverride?: SerializedStyles }>`
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  overflow: hidden;
  .more {
    box-sizing: border-box;
    display: flex;
    align-items: center;
    height: ${SHOW_ALL_H}px;
    padding: 0 ${(p) => p.theme.space(3)};
    border-top: 1px solid ${(p) => p.theme.color.border};
    color: ${(p) => p.theme.color.accentText};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    text-decoration: none;
  }
  .more:hover {
    text-decoration: underline;
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 5%, ${(p) => p.theme.color.surface});
  }
  .more:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 12%, ${(p) => p.theme.color.surface});
  }
  .more:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: -2px;
  }
  ${(p) => p.stylesOverride}
`;
// Rows and footer of the loaded block: a row is GROUP_ROW_H (narrow: GROUP_ROW_H_NARROW) plus a 1px divider.
const SkeletonBox = styled(Box)`
  .row {
    display: flex;
    align-items: center;
    height: ${GROUP_ROW_H}px;
    padding: 0 ${(p) => p.theme.space(3)};
  }
  .row + .row {
    box-sizing: content-box;
    border-top: 1px solid ${(p) => p.theme.color.border};
  }
  .row > * {
    flex: 1;
  }
  .foot {
    display: flex;
    align-items: center;
    height: ${SHOW_ALL_H}px;
    padding: 0 ${(p) => p.theme.space(3)};
    border-top: 1px solid ${(p) => p.theme.color.border};
  }
  .foot > * {
    width: 120px;
  }
  @media (max-width: ${GROUP_ROW_NARROW}px) {
    .row {
      height: ${GROUP_ROW_H_NARROW}px;
    }
  }
`;

// region Component
interface NeedsAttentionProps {
  /** Up to 10 groups, ranked by the server (design §5.3); the order is kept. */
  groups: GroupRow[];
  groupTotal: number;
  installId: string;
  stylesOverride?: SerializedStyles;
}

/** The ranked groups and a link to the grouped findings page; renders nothing without groups (design §6). */
export function NeedsAttention({ groups, groupTotal, installId, stylesOverride }: NeedsAttentionProps) {
  const id = useId();
  if (groups.length === 0) return null;
  return (
    <section aria-labelledby={id}>
      <h2 id={id}>{diagnosticsCopy.needsAttention}</h2>
      <Box stylesOverride={stylesOverride}>
        {groups.map((g) => (
          <DiagnosticsGroupRow key={g.id} group={g} installId={installId} />
        ))}
        <Link className="more" to={`${installPath(installId, "findings")}?view=groups`}>
          {diagnosticsCopy.showAllGroups(Math.max(groupTotal, groups.length))}
        </Link>
      </Box>
    </section>
  );
}

/** The loading placeholder for rows groups: the loaded block's heading, rows and footer; nothing for 0 (design §17.4). */
export function NeedsAttentionSkeleton({ rows }: { rows: number }) {
  if (rows <= 0) return null;
  return (
    <section data-testid="needs-attention-shimmer">
      <h2>{diagnosticsCopy.needsAttention}</h2>
      <SkeletonBox>
        {Array.from({ length: rows }, (_, i) => `row-${i}`).map((k) => (
          <div key={k} className="row" data-testid="group-row-shimmer">
            <Shimmer h={32} />
          </div>
        ))}
        <div className="foot" data-testid="show-all-shimmer">
          <Shimmer h={12} />
        </div>
      </SkeletonBox>
    </section>
  );
}
