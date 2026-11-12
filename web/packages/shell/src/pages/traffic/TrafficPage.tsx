import type { SerializedStyles } from "@emotion/react";
import { useMemo } from "react";
import { usePaths } from "../../api/exploreHooks";
import type { Page, PathRow, PathsResponse } from "../../api/types";
import { listCopy } from "../../copy/explorer";
import { summaryCopy, trafficCopy } from "../../copy/traffic";
import { ListFrame } from "../../explore/ListFrame";
import { listValues, useListPage } from "../../explore/listQuery";
import { type FilterOption, FilterTag, StateFilter } from "../../explore/StateFilter";
import { HealthIcon } from "../../health/HealthIcon";
import { stateLabel } from "../../health/state";
import { PageState } from "../../states/PageState";
import type { HealthState } from "../../theme/tokens";
import { refText } from "../explore/listParts";
import { Narrow, narrowOptions, single } from "./narrow";
import { PathSummary } from "./PathSummary";
import { PathTable } from "./PathTable";
import { StaleGate } from "./StaleGate";

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

// region Component

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
