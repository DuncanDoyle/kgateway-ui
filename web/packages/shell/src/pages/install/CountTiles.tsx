import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { memo } from "react";
import type { OverviewResponse } from "../../api/types";
import { HealthIcon } from "../../health/HealthIcon";
import { Tooltip } from "../../shell/Tooltip";
import { DEPLOYMENT_TEMPLATE, scopeText } from "../scope";

// region Styles
/** Fixed tile height, shared with the loading skeleton so nothing shifts. */
export const TILE_H = 84;

/** The tile grid; the loading skeleton reuses it with shimmer tiles. */
export const TileGrid = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
  grid-auto-rows: ${TILE_H}px;
  gap: ${(p) => p.theme.space(2)};
  margin: ${(p) => p.theme.space(4)} 0 0;
  ${(p) => p.stylesOverride}
`;
const Tile = styled.div`
  box-sizing: border-box;
  height: ${TILE_H}px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  padding: ${(p) => p.theme.space(3)};
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  .n {
    display: inline-flex;
    gap: 6px;
    align-items: center;
    font-size: ${(p) => p.theme.font.size.xl};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    letter-spacing: -0.02em;
    line-height: 1.2;
    border-radius: ${(p) => p.theme.radius.sm};
    font-variant-numeric: tabular-nums;
  }
  .n:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
  .l,
  .s {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
  }
  .warn {
    color: ${(p) => p.theme.color.status.unknown.fg};
    font-size: ${(p) => p.theme.font.size.base};
  }
`;

// region Component
interface CountTilesProps {
  overview: OverviewResponse;
  stylesOverride?: SerializedStyles;
}

/** Count tiles per served kind, in served order, plus the scope tile. Memoised against the page's 1 s clock tick. */
export const CountTiles = memo(function CountTiles({ overview, stylesOverride }: CountTilesProps) {
  const sc = overview.header.scope;
  return (
    <TileGrid stylesOverride={stylesOverride}>
      {overview.counts.map((c) => (
        <Tile key={`${c.group}/${c.kind}`} data-testid="count-tile">
          {c.access.ok ? (
            <span className="n">{c.count}</span>
          ) : (
            <Tooltip label={c.access.error ?? "No permission to read this"}>
              {/* biome-ignore lint/a11y/noNoninteractiveTabindex: focusable so keyboard users can read the tooltip */}
              <span className="n warn" tabIndex={0}>
                <HealthIcon state="unknown" />
                No access
              </span>
            </Tooltip>
          )}
          <span className="l">{c.displayName}</span>
        </Tile>
      ))}
      <Tile data-testid="scope-tile">
        <Tooltip label={scopeText(sc, "detail")} placement="end">
          {/* biome-ignore lint/a11y/noNoninteractiveTabindex: focusable so keyboard users can read the tooltip */}
          <span className={sc.mode === "unknown" ? "n warn" : "n"} tabIndex={0}>
            {sc.mode === "unknown" ? (
              <>
                <HealthIcon state="unknown" />
                Unknown
              </>
            ) : (
              `${sc.watched} of ${sc.total}`
            )}
          </span>
        </Tooltip>
        <span className="l">Namespaces watched</span>
        {sc.source === DEPLOYMENT_TEMPLATE ? <span className="s">Source: Deployment template</span> : null}
      </Tile>
    </TileGrid>
  );
});
