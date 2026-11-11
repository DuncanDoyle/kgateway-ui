import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { type ReactNode, useMemo } from "react";
import { useNow } from "../../api/clock";
import { usePaths } from "../../api/exploreHooks";
import { isStale } from "../../api/hooks";
import type { Page, PathRow, PathsResponse } from "../../api/types";
import { listCopy } from "../../copy/explorer";
import { summaryCopy, trafficCopy } from "../../copy/traffic";
import { ListFrame } from "../../explore/ListFrame";
import { listValues, useListPage } from "../../explore/listQuery";
import { type FilterOption, FilterTag, StateFilter } from "../../explore/StateFilter";
import { HealthIcon } from "../../health/HealthIcon";
import { StaleView } from "../../health/staleView";
import { stateLabel } from "../../health/state";
import { PageState } from "../../states/PageState";
import type { HealthState } from "../../theme/tokens";
import { refText } from "../explore/listParts";
import { PathSummary } from "./PathSummary";
import { PathTable } from "./PathTable";

// region Styles
/** One line of chips taken from the shown rows; it scrolls sideways, so its height never changes. */
const Narrow = styled.div`
  display: flex;
  align-items: center;
  gap: ${(p) => p.theme.space(2)};
  height: 40px;
  margin: -${(p) => p.theme.space(1)} 0 ${(p) => p.theme.space(2)};
  overflow-x: auto;
  overflow-y: hidden;
  scrollbar-width: thin;
  white-space: nowrap;
  .label {
    flex: none;
    margin-left: ${(p) => p.theme.space(1)};
    font-size: ${(p) => p.theme.font.size.sm};
    color: ${(p) => p.theme.color.textMuted};
  }
  > div {
    flex-wrap: nowrap;
  }
`;

// region Helpers
const STATES: HealthState[] = ["error", "warning", "unknown", "healthy"];
const stateOptions: FilterOption[] = STATES.map((s) => ({
  value: s,
  label: stateLabel(s),
  icon: <HealthIcon state={s} />,
}));
const kindOptions: FilterOption[] = ["HTTPRoute", "GRPCRoute", "TCPRoute", "TLSRoute"].map((k) => ({
  value: k,
  label: k,
}));
const mirrorOptions: FilterOption[] = [
  { value: "only", label: trafficCopy.mirrorOnly },
  { value: "exclude", label: trafficCopy.mirrorExclude },
];
/** The most chips per group in the Narrow to line; selected values always show. */
const NARROW_MAX = 50;

/** The newly pressed value of a single-valued chip group, or "" when the pressed one was released. */
const single = (prev: string[], next: string[]) => next.find((v) => !prev.includes(v)) ?? "";

/** A ref param in words: "Kind ns/name" for group/kind/ns/name; anything else as written. */
function refTag(v: string): string {
  const p = v.split("/");
  return p.length === 4 && p[1] && p[3] ? `${p[1]} ${p[2] ? `${p[2]}/` : ""}${p[3]}` : v;
}

/** The response as a list page for the table; total is the filtered result's path count. */
const toPage = (d: PathsResponse): Page<PathRow> => ({
  install: d.install,
  items: d.rows,
  total: d.summary.paths,
  next: d.next,
  prev: d.prev,
  collectedAt: d.collectedAt,
});

/** The server's facet values in its name order, the selected ones first so they can always be released. */
function narrowOptions(values: string[], selected: string[]): FilterOption[] {
  const rest = values.filter((v) => !selected.includes(v));
  return [...selected, ...rest].slice(0, Math.max(NARROW_MAX, selected.length)).map((v) => ({ value: v, label: v }));
}

// region Component
/** Provides the stale view from the app clock; only this re-renders on a tick, not the page or its table. */
function StaleGate({ collectedAt, failed, children }: { collectedAt?: string; failed: boolean; children: ReactNode }) {
  const now = useNow();
  const stale = !!collectedAt && (failed || isStale(collectedAt, now));
  return <StaleView.Provider value={stale}>{children}</StaleView.Provider>;
}

interface TrafficPageProps {
  stylesOverride?: SerializedStyles;
}

/**
 * Every path of the install, problems first, filtered, sorted and paged by the server through the URL (design §5).
 * Entry points set the starting filter: gateway=, route= or backend=.
 */
export function TrafficPage({ stylesOverride }: TrafficPageProps = {}) {
  const l = useListPage<PathRow, PathsResponse>("paths", usePaths);
  const page = useMemo(() => (l.data ? toPage(l.data) : undefined), [l.data]);
  const summary = l.data?.summary;
  const gateways = listValues(l.params, "gateway");
  const namespace = listValues(l.params, "namespace").slice(0, 1);
  const facets = summary?.facets;
  const gatewayOptions = narrowOptions(
    (facets?.gateways ?? []).map((g) => refText(g.ref)),
    gateways,
  );
  const namespaceOptions = narrowOptions(
    (facets?.namespaces ?? []).map((n) => n.name),
    namespace,
  );
  // The line keeps its place while loading and goes when there is nothing to narrow; its filters are then tags.
  const narrow = !summary || (!summary.notComputed && summary.paths > 0);
  const tagKeys = narrow
    ? (["listener", "route", "backend"] as const)
    : (["gateway", "namespace", "listener", "route", "backend"] as const);
  const tags = tagKeys.flatMap((key) =>
    listValues(l.params, key).map((v) => (
      <FilterTag
        key={`${key}=${v}`}
        text={`${trafficCopy.tags[key]}: ${refTag(v)}`}
        onRemove={() =>
          l.setFilter({
            [key]: listValues(l.params, key)
              .filter((x) => x !== v)
              .join(","),
          })
        }
      />
    )),
  );
  return (
    <ListFrame
      title={trafficCopy.title}
      installId={l.id}
      noun={trafficCopy.noun}
      collectedAt={l.data?.collectedAt}
      total={summary?.paths}
      error={l.error}
      loaded={!!l.data}
      search={{
        label: trafficCopy.search,
        placeholder: trafficCopy.searchPlaceholder,
        value: l.text,
        onChange: l.setText,
      }}
      filters={
        <>
          <StateFilter
            label={trafficCopy.state}
            options={stateOptions}
            selected={listValues(l.params, "state")}
            onChange={(v) => l.setFilter({ state: v.join(",") })}
          />
          <StateFilter
            label={trafficCopy.kind}
            options={kindOptions}
            selected={listValues(l.params, "kind").slice(0, 1)}
            onChange={(v) => l.setFilter({ kind: single(listValues(l.params, "kind"), v) })}
          />
          <StateFilter
            label={trafficCopy.mirror}
            options={mirrorOptions}
            selected={listValues(l.params, "mirror")}
            onChange={(v) => l.setFilter({ mirror: single(listValues(l.params, "mirror"), v) })}
          />
          {tags}
        </>
      }
      onClearFilters={l.filtered ? l.clearFilters : undefined}
      stylesOverride={stylesOverride}
    >
      {narrow ? (
        <Narrow data-testid="narrow-to">
          <span className="label">{trafficCopy.narrow}</span>
          <span className="label" aria-hidden="true">
            {trafficCopy.gatewayChips}
          </span>
          <StateFilter
            label={trafficCopy.gateways}
            options={gatewayOptions}
            selected={gateways}
            onChange={(v) => l.setFilter({ gateway: v.join(",") })}
          />
          <span className="label" aria-hidden="true">
            {trafficCopy.namespaceChips}
          </span>
          <StateFilter
            label={trafficCopy.namespaces}
            options={namespaceOptions}
            selected={namespace}
            onChange={(v) => l.setFilter({ namespace: single(namespace, v) })}
          />
        </Narrow>
      ) : null}
      {summary?.notComputed ? (
        <PageState kind="empty" title={summaryCopy.notComputedTitle} message={summaryCopy.notComputed} />
      ) : (
        <StaleGate collectedAt={l.data?.collectedAt} failed={!!l.error}>
          <PathSummary summary={summary} />
          <PathTable
            installId={l.id}
            page={page}
            busy={l.busy}
            onClearFilters={l.filtered ? l.clearFilters : undefined}
            sort={l.sort}
            onSort={l.setSort}
            onPage={l.setCursor}
            emptyTitle={l.filtered ? listCopy.emptyFiltered(trafficCopy.noun[1]) : listCopy.empty(trafficCopy.noun[1])}
          />
        </StaleGate>
      )}
    </ListFrame>
  );
}
