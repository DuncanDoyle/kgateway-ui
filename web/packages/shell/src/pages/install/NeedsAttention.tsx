import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useId } from "react";
import { Link } from "react-router-dom";
import type { GroupRow } from "../../api/types";
import { diagnosticsCopy } from "../../copy/explorer";
import { DiagnosticsGroupRow } from "../../diagnostics/GroupRow";
import { installPath } from "../../shell/paths";

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
