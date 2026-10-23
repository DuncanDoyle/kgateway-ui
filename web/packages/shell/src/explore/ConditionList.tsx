import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import type { ReactNode } from "react";
import type { Condition } from "../api/types";
import { detailCopy } from "../copy/explorer";
import { HealthIcon } from "../health/HealthIcon";
import type { HealthState } from "../theme/tokens";

// region Styles
const List = styled.ul<{ stylesOverride?: SerializedStyles }>`
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: ${(p) => p.theme.space(2)};
  li {
    display: grid;
    grid-template-columns: 16px 1fr;
    gap: 2px ${(p) => p.theme.space(2)};
    align-items: start;
    min-width: 0;
  }
  li > :not(:first-child) {
    grid-column: 2;
    min-width: 0;
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0 ${(p) => p.theme.space(2)};
    overflow-wrap: anywhere;
  }
  .type {
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  .reason {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
    color: ${(p) => p.theme.color.textMuted};
  }
  .message,
  .fresh {
    font-size: ${(p) => p.theme.font.size.sm};
    color: ${(p) => p.theme.color.textMuted};
    overflow-wrap: anywhere;
  }
  .fresh {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  ${(p) => p.stylesOverride}
`;
const None = styled.p`
  margin: 0;
  color: ${(p) => p.theme.color.textMuted};
`;

// region Helpers
/** Gateway API condition types where True is the problem. */
const NEGATIVE = new Set(["Conflicted", "OverlappingTLSConfig"]);

/** The state a condition shows: True is good unless the type is a problem flag; Unknown or stale is unknown. */
export function conditionState(c: Pick<Condition, "type" | "status"> & { stale?: boolean }): HealthState {
  if (c.stale || (c.status !== "True" && c.status !== "False")) return "unknown";
  return (c.status === "True") !== NEGATIVE.has(c.type) ? "healthy" : "error";
}

// region Component
interface ConditionListProps {
  conditions: Condition[];
  /** The object's metadata.generation, for the freshness line of a stale condition. */
  generation: number;
  /** Shown when there are no conditions. */
  empty?: ReactNode;
  /** Extra content under one condition, e.g. the parent's allowedListeners next to NotAllowed. */
  extra?: (c: Condition) => ReactNode;
  /** The icon state per condition, when it follows something other than the condition (e.g. a Backend's state). */
  stateOf?: (c: Condition) => HealthState;
  /** The head text per condition; defaults to "<Type>: <Status>". */
  headOf?: (c: Condition) => ReactNode;
  stylesOverride?: SerializedStyles;
}

/** Status conditions as the controller wrote them: icon and status text, reason as written, message and freshness. */
export function ConditionList({
  conditions,
  generation,
  empty,
  extra,
  stateOf = conditionState,
  headOf,
  stylesOverride,
}: ConditionListProps) {
  if (!conditions.length) return <None>{empty ?? detailCopy.noConditions}</None>;
  return (
    <List stylesOverride={stylesOverride}>
      {conditions.map((c) => (
        <li key={c.type} data-testid={`condition-${c.type}`}>
          <HealthIcon state={stateOf(c)} />
          <span className="head">
            <span className="type">{headOf ? headOf(c) : `${c.type}: ${c.status}`}</span>
            {c.reason ? <span className="reason">{c.reason}</span> : null}
          </span>
          {c.message ? <span className="message">{c.message}</span> : null}
          {c.stale ? (
            <span className="fresh">
              <HealthIcon state="unknown" size={14} />
              {detailCopy.stale(c.observedGeneration, generation)}
            </span>
          ) : null}
          {extra?.(c)}
        </li>
      ))}
    </List>
  );
}
