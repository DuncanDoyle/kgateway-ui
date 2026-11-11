import { css, type SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Keyboard, Search, Table2 } from "lucide-react";
import type { PathFacets } from "../../api/types";
import { mapCopy } from "../../copy/map";
import { trafficCopy } from "../../copy/traffic";
import { listValues } from "../../explore/listQuery";
import { type FilterOption, FilterTag, StateFilter } from "../../explore/StateFilter";
import { stateLabel } from "../../health/state";
import { CHIP_PRESSED_ACTIVE_PCT, CHIP_PRESSED_HOVER_PCT, CHIP_PRESSED_PCT } from "../../shell/constants";
import { Tooltip } from "../../shell/Tooltip";
import type { ParamChanges } from "../../shell/urlParams";
import { SearchBox } from "../../states/controls";
import type { HealthState } from "../../theme/tokens";
import { PROBLEM_STATES } from "../backends/BackendsPage";
import { Narrow, narrowOptions, single } from "../traffic/narrow";

// region Styles
const Bar = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: ${(p) => p.theme.space(2)};
  margin-bottom: ${(p) => p.theme.space(2)};
  .found {
    display: inline-block;
    min-width: 56px;
    font-size: ${(p) => p.theme.font.size.sm};
    color: ${(p) => p.theme.color.textMuted};
  }
  .find {
    display: inline-flex;
    align-items: center;
    gap: ${(p) => p.theme.space(2)};
  }
  /* Map-only controls keep their space in the table view, so the chips and the toggle never move. */
  .off {
    visibility: hidden;
  }
  .end {
    display: inline-flex;
    align-items: center;
    gap: ${(p) => p.theme.space(2)};
    margin-left: auto;
  }
  ${(p) => p.stylesOverride}
`;
/** The table toggle: as tall as the chips and find box; pressed shows the table, with the chips' selected fill. */
const Toggle = styled.button`
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 12px;
  cursor: pointer;
  font: inherit;
  font-size: ${(p) => p.theme.font.size.sm};
  color: ${(p) => p.theme.color.text};
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    border-color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.surface});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
  &[aria-pressed="true"] {
    border-color: ${(p) => p.theme.color.accentText};
    box-shadow: inset 0 0 0 1px ${(p) => p.theme.color.accentText};
    background: color-mix(in srgb, ${(p) => p.theme.color.accentText} ${CHIP_PRESSED_PCT}%, ${(p) => p.theme.color.surface});
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  &[aria-pressed="true"]:hover {
    background: color-mix(in srgb, ${(p) => p.theme.color.accentText} ${CHIP_PRESSED_HOVER_PCT}%, ${(p) => p.theme.color.surface});
  }
  &[aria-pressed="true"]:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.accentText} ${CHIP_PRESSED_ACTIVE_PCT}%, ${(p) => p.theme.color.surface});
  }
`;
/** The keyboard hint: an icon whose tooltip lists the map's keys. */
const Hint = styled.span`
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  box-sizing: border-box;
  border-radius: ${(p) => p.theme.radius.sm};
  color: ${(p) => p.theme.color.textMuted};
  cursor: help;
  &:hover {
    color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.surface});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
`;

// region Helpers
const hidden = css`
  visibility: hidden;
`;
const PROBLEMS = "problems";
const KINDS: FilterOption[] = ["HTTPRoute", "GRPCRoute", "TCPRoute", "TLSRoute"].map((k) => ({ value: k, label: k }));

/** True when the URL's state filter is exactly the Problems set, in any order. */
export function problemsOnly(params: URLSearchParams): boolean {
  const states = new Set(listValues(params, "state"));
  const problems = PROBLEM_STATES.split(",");
  return states.size === problems.length && problems.every((s) => states.has(s));
}

// region Component
interface MapToolbarProps {
  params: URLSearchParams;
  onFilter: (changes: ParamChanges) => void;
  facets?: PathFacets;
  find: string;
  onFind: (text: string) => void;
  /** Enter in the find box: focus the next match. */
  onFindNext: () => void;
  /** The focused match and the match count, once there is text. */
  found?: { at: number; of: number };
  /** The rows show as a table instead of the map. */
  table: boolean;
  onTable: (table: boolean) => void;
  stylesOverride?: SerializedStyles;
}

/**
 * The map's find box, its filter chips (Problems only, route kind and route namespace) and the table toggle
 * (design §6.1). The chips write the URL, which drives the server query; the find box only moves the map.
 */
export function MapToolbar({
  params,
  onFilter,
  facets,
  find,
  onFind,
  onFindNext,
  found,
  table,
  onTable,
  stylesOverride,
}: MapToolbarProps) {
  const problems = problemsOnly(params);
  const states = listValues(params, "state");
  const kind = listValues(params, "kind").slice(0, 1);
  const namespace = listValues(params, "namespace").slice(0, 1);
  const namespaceOptions = narrowOptions(
    (facets?.namespaces ?? []).map((n) => n.name),
    namespace,
  );
  return (
    <>
      <Bar stylesOverride={stylesOverride}>
        {/* Find only moves the map, so the table view hides it. */}
        <span className={table ? "find off" : "find"} data-testid="map-find">
          <form
            onSubmit={(e) => {
              e.preventDefault();
              onFindNext();
            }}
          >
            <SearchBox>
              <Search size={14} aria-hidden="true" />
              <input
                type="search"
                aria-label={mapCopy.find}
                placeholder={mapCopy.findPlaceholder}
                value={find}
                onChange={(e) => onFind(e.target.value)}
              />
            </SearchBox>
          </form>
          <span className="found" role="status" data-testid="map-found">
            {find && found ? (found.of ? mapCopy.found(found.at, found.of) : mapCopy.notFound) : ""}
          </span>
        </span>
        <StateFilter
          label={mapCopy.problemsOnly}
          options={[{ value: PROBLEMS, label: mapCopy.problemsOnly }]}
          selected={problems ? [PROBLEMS] : []}
          onChange={(v) => onFilter({ state: v.length ? PROBLEM_STATES : null })}
        />
        <StateFilter
          label={trafficCopy.kind}
          options={KINDS}
          selected={kind}
          onChange={(v) => onFilter({ kind: single(kind, v) })}
        />
        {/* A state set by a link that the chip does not show stays visible as a tag. */}
        {states.length && !problems ? (
          <FilterTag
            text={`${trafficCopy.state}: ${states.map((s) => stateLabel(s as HealthState)).join(", ")}`}
            onRemove={() => onFilter({ state: null })}
          />
        ) : null}
        <span className="end">
          <Tooltip label={mapCopy.keys} placement="end" wrap stylesOverride={table ? hidden : undefined}>
            <Hint tabIndex={0} role="note" aria-label={mapCopy.keyboard} data-testid="map-keys">
              <Keyboard size={16} aria-hidden="true" />
            </Hint>
          </Tooltip>
          <Tooltip label={mapCopy.tableTip} placement="end">
            <Toggle type="button" aria-pressed={table} onClick={() => onTable(!table)}>
              <Table2 size={14} aria-hidden="true" />
              {mapCopy.table}
            </Toggle>
          </Tooltip>
        </span>
      </Bar>
      <Narrow data-testid="map-namespaces">
        <span className="label" aria-hidden="true">
          {trafficCopy.namespaceChips}
        </span>
        <StateFilter
          label={trafficCopy.namespaces}
          options={namespaceOptions}
          selected={namespace}
          onChange={(v) => onFilter({ namespace: single(namespace, v) })}
        />
      </Narrow>
    </>
  );
}
