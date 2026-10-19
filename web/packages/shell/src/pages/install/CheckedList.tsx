import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import type { CheckResult } from "../../api/types";
import { HealthIcon } from "../../health/HealthIcon";

// region Styles
const List = styled.ul<{ stylesOverride?: SerializedStyles }>`
  list-style: none;
  margin: 0;
  padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  li {
    display: grid;
    grid-template-columns: 16px 1fr;
    column-gap: 8px;
    align-items: center;
    padding: 5px 0;
  }
  .detail {
    grid-column: 2;
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
  }
  ${(p) => p.stylesOverride}
`;

// region Component
interface CheckedListProps {
  checks: CheckResult[];
  /** Stale data shows every check as unknown: an old tick is not evidence (D-006). */
  stale?: boolean;
  stylesOverride?: SerializedStyles;
}

/** Proves that green means "checked and fine", not "did not look" (design §5.3). */
export function CheckedList({ checks, stale = false, stylesOverride }: CheckedListProps) {
  return (
    <List aria-label="What was checked" stylesOverride={stylesOverride}>
      {checks.map((c) => {
        const note = c.detail ?? (c.ok ? undefined : c.error);
        return (
          <li key={c.name}>
            <HealthIcon state={c.ok && !stale ? "healthy" : "unknown"} />
            <span>{c.name}</span>
            {note ? <span className="detail">{note}</span> : null}
          </li>
        );
      })}
    </List>
  );
}
