import { css, type SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { EyeOff, Info } from "lucide-react";
import type { PathCounts, PathSummary as Summary } from "../../api/types";
import { summaryCopy } from "../../copy/traffic";
import { HealthIcon } from "../../health/HealthIcon";
import { stateLabel } from "../../health/state";
import { Shimmer } from "../../states/Shimmer";
import type { HealthState } from "../../theme/tokens";

// region Styles
const Box = styled.section<{ stylesOverride?: SerializedStyles }>`
  margin-bottom: ${(p) => p.theme.space(3)};
  .total {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(3)};
    /* Fixed, so the counts and notes arriving later move nothing. */
    min-height: 24px;
    margin-bottom: ${(p) => p.theme.space(2)};
    font-size: ${(p) => p.theme.font.size.sm};
  }
  .note {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: ${(p) => p.theme.color.textMuted};
  }
  ul {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: ${(p) => p.theme.space(2)};
    margin: 0;
    padding: 0;
    list-style: none;
    @media (max-width: 899px) {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  ${(p) => p.stylesOverride}
`;
/** One state's counts; a non-zero problem state gets its status border and tint, never a left stripe. */
const Tile = styled.li<{ $state: HealthState; $on: boolean }>`
  box-sizing: border-box;
  height: 64px;
  padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
  border-radius: ${(p) => p.theme.radius.md};
  border: 1px solid ${(p) => (p.$on ? p.theme.color.status[p.$state].fg : p.theme.color.border)};
  background: ${(p) => (p.$on ? p.theme.color.status[p.$state].stripBg : p.theme.color.surface)};
  .head {
    display: flex;
    align-items: center;
    gap: ${(p) => p.theme.space(1.5)};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    color: ${(p) => (p.$on ? p.theme.color.status[p.$state].fg : p.theme.color.textMuted)};
    white-space: nowrap;
  }
  .rest {
    margin-top: 4px;
    font-size: ${(p) => p.theme.font.size.sm};
    color: ${(p) => p.theme.color.textMuted};
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-variant-numeric: tabular-nums;
  }
`;

// region Helpers
const wide = css`width: 240px;`;
const narrow = css`width: 160px; max-width: 100%;`;
const STATES: HealthState[] = ["error", "warning", "unknown", "healthy"];

/** "37 paths" and the rest of a state's counts, the way one broken shared backend reads (design §5). */
function tileText(c: PathCounts): [string, string] {
  const [paths, ...rest] = summaryCopy.counts(c.paths, c.routes, c.gateways, c.backends).split(" · ");
  return [paths ?? "", rest.join(" · ")];
}

// region Component
interface PathSummaryProps {
  /** The whole filtered result; absent while loading. */
  summary?: Summary;
  stylesOverride?: SerializedStyles;
}

/** The counts of the whole filtered result, per state, with the coverage and visibility notes (design §5, §13). */
export function PathSummary({ summary, stylesOverride }: PathSummaryProps) {
  return (
    <Box aria-label={summaryCopy.label} data-testid="path-summary" stylesOverride={stylesOverride}>
      <div className="total">
        {summary ? (
          <span data-testid="summary-counts">
            {summaryCopy.counts(summary.paths, summary.routes, summary.gateways, summary.backends)}
          </span>
        ) : (
          <Shimmer h={14} stylesOverride={wide} />
        )}
        {summary?.hidden ? (
          <span className="note">
            <EyeOff size={14} aria-hidden="true" />
            {summaryCopy.hidden}
          </span>
        ) : null}
        {summary?.endpointsNotRead ? (
          <span className="note">
            <Info size={14} aria-hidden="true" />
            {summaryCopy.endpointsNotRead}
          </span>
        ) : null}
      </div>
      <ul>
        {STATES.map((s) => {
          const c = summary?.byState[s];
          const [paths, rest] = c ? tileText(c) : ["", ""];
          return (
            <Tile key={s} $state={s} $on={!!c?.paths && s !== "healthy"} data-testid={`summary-${s}`}>
              <div className="head">
                <HealthIcon state={s} notice />
                {stateLabel(s)}
                {c ? <span>{paths}</span> : null}
              </div>
              <div className="rest">{c ? rest : <Shimmer h={12} stylesOverride={narrow} />}</div>
            </Tile>
          );
        })}
      </ul>
    </Box>
  );
}
