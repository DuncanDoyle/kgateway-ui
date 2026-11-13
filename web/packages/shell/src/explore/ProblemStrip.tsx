import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import type { Condition, DetailReason } from "../api/types";
import { problemCopy } from "../copy/explorer";
import { rowReasonText } from "../health/HealthBadge";
import { HealthIcon } from "../health/HealthIcon";
import { stateLabel } from "../health/state";
import { findingPath } from "../pages/findings/FindingsPage";
import type { HealthState } from "../theme/tokens";
import { conditionState } from "./ConditionList";

// region Styles
/** The strip's element id; the header's state badge links to it. */
export const PROBLEM_ID = "problem";

/** A full border and tint in the state's colour, never a left stripe; the icon and label carry the state too. */
const Strip = styled.section<{ $state: HealthState; stylesOverride?: SerializedStyles }>`
  display: grid;
  /* Section's spacing, so the sections below keep their rhythm. */
  margin-bottom: ${(p) => p.theme.space(5)};
  grid-template-columns: 16px 1fr;
  gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(2)};
  padding: ${(p) => p.theme.space(3)};
  border-radius: ${(p) => p.theme.radius.md};
  border: 1px solid ${(p) => p.theme.color.status[p.$state].stripBorder};
  background: ${(p) => p.theme.color.status[p.$state].stripBg};
  color: ${(p) => p.theme.color.text};
  overflow-wrap: anywhere;
  & > :not(:first-child) {
    grid-column: 2;
    min-width: 0;
  }
  .state {
    font-weight: ${(p) => p.theme.font.weight.semibold};
    color: ${(p) => p.theme.color.status[p.$state].fg};
  }
  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0 ${(p) => p.theme.space(2)};
  }
  .type {
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  .reason {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
    color: ${(p) => p.theme.color.textMuted};
  }
  .label {
    color: ${(p) => p.theme.color.textMuted};
  }
  /* Body text colour, not accent: it keeps AA on every status tint. */
  .jump {
    padding: 0 4px;
    margin: 0 -4px;
    border-radius: ${(p) => p.theme.radius.sm};
    color: ${(p) => p.theme.color.text};
    font-size: ${(p) => p.theme.font.size.sm};
    text-decoration: underline;
  }
  .jump:hover {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 8%, ${(p) => p.theme.color.status[p.$state].stripBg});
  }
  .jump:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.status[p.$state].stripBg});
  }
  .jump:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
  ${(p) => p.stylesOverride}
`;

// region Helpers
/** The first condition the condition list shows as failing; display only, the state comes from the server. */
export const firstFailing = (cs: readonly Condition[]) => cs.find((c) => conditionState(c) === "error");

// region Component
export interface ProblemStripProps {
  installId: string;
  /** The page's state as the server sent it; never healthy here. */
  state: HealthState;
  reason?: DetailReason;
  /** The first failing condition, linked to conditionsId. */
  condition?: Condition;
  conditionsId?: string;
  /** Where the fix goes when it is not this object, e.g. a ListenerSet's parent Gateway. */
  parent?: { label: string; name: ReactNode };
  stylesOverride?: SerializedStyles;
}

/** Why a detail page is not healthy, under its header: the deciding finding and the first failing condition. */
export function ProblemStrip({
  installId,
  state,
  reason,
  condition,
  conditionsId,
  parent,
  stylesOverride,
}: ProblemStripProps) {
  const finding =
    reason?.kind === "finding" && reason.finding ? findingPath(installId, { id: reason.finding }) : undefined;
  return (
    <Strip
      id={PROBLEM_ID}
      tabIndex={-1}
      $state={state}
      data-testid="problem-strip"
      data-state={state}
      stylesOverride={stylesOverride}
    >
      <HealthIcon state={state} />
      <span className="state">{stateLabel(state)}</span>
      {reason ? (
        <span className="line" data-testid="problem-reason">
          <span>{rowReasonText(reason)}</span>
          {finding ? (
            <Link className="jump" to={finding}>
              {problemCopy.openFinding}
            </Link>
          ) : null}
        </span>
      ) : null}
      {condition ? (
        <span className="line" data-testid="problem-condition">
          <span className="type">{`${condition.type}: ${condition.status}`}</span>
          {condition.reason ? <span className="reason">{condition.reason}</span> : null}
          {condition.message ? <span>{condition.message}</span> : null}
          {conditionsId ? (
            <a className="jump" href={`#${conditionsId}`}>
              {problemCopy.seeConditions}
            </a>
          ) : null}
        </span>
      ) : null}
      {parent ? (
        <span className="line" data-testid="problem-parent">
          <span className="label">{parent.label}</span>
          {parent.name}
        </span>
      ) : null}
    </Strip>
  );
}
