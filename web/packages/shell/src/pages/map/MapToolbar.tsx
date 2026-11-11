import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Search } from "lucide-react";
import type { PathFacets } from "../../api/types";
import { mapCopy } from "../../copy/map";
import { trafficCopy } from "../../copy/traffic";
import { listValues } from "../../explore/listQuery";
import { type FilterOption, FilterTag, StateFilter } from "../../explore/StateFilter";
import { stateLabel } from "../../health/state";
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
  ${(p) => p.stylesOverride}
`;

// region Helpers
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
  stylesOverride?: SerializedStyles;
}

/**
 * The map's find box and its filter chips: Problems only, route kind and route namespace (design §6.1).
 * The chips write the URL, which drives the server query; the find box only moves the map.
 */
export function MapToolbar({
  params,
  onFilter,
  facets,
  find,
  onFind,
  onFindNext,
  found,
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
