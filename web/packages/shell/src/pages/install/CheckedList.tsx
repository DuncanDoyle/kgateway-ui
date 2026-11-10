import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { ChevronDown, ChevronRight, CircleSlash, Clock } from "lucide-react";
import { useId, useState } from "react";
import { useNow } from "../../api/clock";
import type { CheckResult } from "../../api/types";
import { diagnosticsCopy } from "../../copy/explorer";
import { HealthIcon } from "../../health/HealthIcon";
import { stateLabel } from "../../health/state";
import { VisuallyHidden } from "../../states/VisuallyHidden";
import { inTime } from "./HealthStrip";

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
  .na,
  .deciding {
    display: inline-flex;
    color: ${(p) => p.theme.color.textMuted};
  }
  ${(p) => p.stylesOverride}
`;
const Toggle = styled.button`
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin: 0 0 0 -6px;
  padding: 2px 6px;
  cursor: pointer;
  font: inherit;
  font-size: ${(p) => p.theme.font.size.md};
  font-weight: ${(p) => p.theme.font.weight.semibold};
  line-height: 20px;
  color: ${(p) => p.theme.color.text};
  background: transparent;
  border: 0;
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.bg});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.bg});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
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
  const now = useNow();
  return (
    <List aria-label="Checks" stylesOverride={stylesOverride}>
      {checks.map((c) => {
        // A not-available check is neither fine nor unknown: its optional input is missing (D-034).
        const na = !c.ok && c.unavailable;
        // A held check is still deciding: neither green nor unknown, unless the data is stale (D-068).
        const hold = !na && !stale ? c.hold : undefined;
        const note = na
          ? (c.error ?? c.detail ?? diagnosticsCopy.unavailableReason[c.unavailable ?? ""])
          : (c.detail ?? (c.ok || hold ? undefined : c.error));
        const state = c.ok && !stale ? "healthy" : "unknown";
        const label = na ? diagnosticsCopy.notAvailable : hold ? diagnosticsCopy.deciding : stateLabel(state);
        return (
          <li key={c.name}>
            {na ? (
              <span className="na" data-testid="check-unavailable" aria-hidden="true">
                <CircleSlash size={16} />
              </span>
            ) : hold ? (
              <span className="deciding" data-testid="check-deciding" aria-hidden="true">
                <Clock size={16} />
              </span>
            ) : (
              <HealthIcon state={state} />
            )}
            <span>
              <VisuallyHidden>{label}: </VisuallyHidden>
              {c.name}
            </span>
            {note ? <span className="detail">{note}</span> : null}
            {hold ? (
              <span className="detail" title={hold.reason}>
                {diagnosticsCopy.decidingOn(hold.count, inTime(hold.until, now))}
              </span>
            ) : null}
          </li>
        );
      })}
    </List>
  );
}

interface CheckedSectionProps extends CheckedListProps {
  /** Starts collapsed, as when Needs attention is shown; the user's choice wins after that. */
  collapsed: boolean;
}

/** "What was checked" with a toggle; collapsed while there are problems to look at first (design §6). */
export function CheckedSection({ collapsed, ...list }: CheckedSectionProps) {
  const [chosen, setChosen] = useState<boolean>();
  const open = chosen ?? !collapsed;
  const id = useId();
  return (
    <section aria-labelledby={`${id}-title`}>
      <h2>
        <Toggle
          type="button"
          id={`${id}-title`}
          aria-expanded={open}
          aria-controls={id}
          onClick={() => setChosen(!open)}
        >
          {open ? <ChevronDown size={16} aria-hidden="true" /> : <ChevronRight size={16} aria-hidden="true" />}
          {diagnosticsCopy.whatWasChecked}
        </Toggle>
      </h2>
      <div id={id} hidden={!open}>
        {open ? <CheckedList {...list} /> : null}
      </div>
    </section>
  );
}
