import type { SerializedStyles } from "@emotion/react";
import { EyeOff } from "lucide-react";
import { useMemo } from "react";
import { useBackends, useGateways } from "../../api/exploreHooks";
import type { BackendRow, BackendsResponse, Page } from "../../api/types";
import { backendsCopy } from "../../copy/backends";
import { listCopy } from "../../copy/explorer";
import { trafficCopy } from "../../copy/traffic";
import { ListFrame } from "../../explore/ListFrame";
import { listValues, useListPage } from "../../explore/listQuery";
import { type FilterOption, FilterTag, StateFilter } from "../../explore/StateFilter";
import { stateLabel } from "../../health/state";
import { useVocabulary } from "../../shell/extension";
import type { HealthState } from "../../theme/tokens";
import { refText } from "../explore/listParts";
import { Narrow, narrowOptions, single } from "../traffic/narrow";
import { StaleGate } from "../traffic/StaleGate";
import { BackendTable } from "./BackendTable";

// region Helpers
/** The Problems chip: every state that is not healthy, so an unknown never hides (Phase 6 design §4.5). */
export const PROBLEM_STATES = "error,warning,unknown";
const SERVICE = "Service";
/** Chip values: problems, unused, or kind:<Kind>. */
const PROBLEMS = "problems";
const UNUSED = "unused";
const kindChip = (kind: string) => `kind:${kind}`;
/** The Gateways the Gateway chips offer: the install's first page of Gateways. */
const GATEWAY_QUERY = new URLSearchParams();

/** The response as a list page for the table; the list sends no total, so the page's own count stands in. */
const toPage = (d: BackendsResponse): Page<BackendRow> => ({
  install: d.install,
  items: d.rows,
  total: d.rows.length,
  next: d.next,
  prev: d.prev,
  collectedAt: d.collectedAt,
});

/** Which "Show" chips the URL presses. */
function shown(params: URLSearchParams): string[] {
  const out: string[] = [];
  if (listValues(params, "state").join(",") === PROBLEM_STATES) out.push(PROBLEMS);
  const kind = listValues(params, "kind")[0];
  if (kind) out.push(kindChip(kind));
  if (params.get("used") === "no") out.push(UNUSED);
  return out;
}

// region Component
interface BackendsPageProps {
  stylesOverride?: SerializedStyles;
}

/**
 * Every backend of the install a route or other object uses, and every Backend resource, problems first
 * (Phase 6 design §8.1). Filters, sort and cursor live in the URL; the server filters and pages.
 */
export function BackendsPage({ stylesOverride }: BackendsPageProps = {}) {
  const l = useListPage<BackendRow, BackendsResponse>("backends", useBackends);
  const vocab = useVocabulary();
  const page = useMemo(() => (l.data ? toPage(l.data) : undefined), [l.data]);
  const gatewaysList = useGateways(l.id, GATEWAY_QUERY);
  const gateways = listValues(l.params, "gateway");
  const namespace = listValues(l.params, "namespace").slice(0, 1);
  const gatewayOptions = narrowOptions(
    (gatewaysList.data?.items ?? []).map((g) => refText(g.ref)),
    gateways,
  );
  const namespaceOptions = narrowOptions(
    [...new Set((l.data?.rows ?? []).map((r) => r.ref.namespace ?? "").filter(Boolean))].sort(),
    namespace,
  );
  const showOptions: FilterOption[] = [
    { value: PROBLEMS, label: backendsCopy.problems },
    { value: kindChip(SERVICE), label: backendsCopy.services },
    ...vocab.backendKinds.map((k) => ({ value: kindChip(k.kind), label: k.chip })),
    { value: UNUSED, label: backendsCopy.unused },
  ];
  const pressed = shown(l.params);
  const onShow = (next: string[]) => {
    const changed = single(pressed, next) || pressed.find((v) => !next.includes(v)) || "";
    const on = next.includes(changed);
    if (changed === PROBLEMS) l.setFilter({ state: on ? PROBLEM_STATES : null });
    else if (changed === UNUSED) l.setFilter({ used: on ? "no" : null });
    else if (changed.startsWith("kind:")) l.setFilter({ kind: on ? changed.slice(5) : null });
  };
  // A state or kind set by a link that no chip shows stays visible as a tag.
  const states = listValues(l.params, "state");
  const kind = listValues(l.params, "kind")[0];
  const tags = [
    ...(states.length && !pressed.includes(PROBLEMS)
      ? [
          <FilterTag
            key="state"
            text={`${trafficCopy.state}: ${states.map((s) => stateLabel(s as HealthState)).join(", ")}`}
            onRemove={() => l.setFilter({ state: null })}
          />,
        ]
      : []),
    ...(kind && !showOptions.some((o) => o.value === kindChip(kind))
      ? [<FilterTag key="kind" text={kind} onRemove={() => l.setFilter({ kind: null })} />]
      : []),
  ];
  return (
    <ListFrame
      title={backendsCopy.title}
      installId={l.id}
      noun={backendsCopy.noun}
      collectedAt={l.data?.collectedAt}
      error={l.error}
      loaded={!!l.data}
      search={{
        label: backendsCopy.search,
        placeholder: backendsCopy.searchPlaceholder,
        value: l.text,
        onChange: l.setText,
      }}
      filters={
        <>
          <StateFilter label={backendsCopy.show} options={showOptions} selected={pressed} onChange={onShow} />
          {tags}
        </>
      }
      onClearFilters={l.filtered ? l.clearFilters : undefined}
      stylesOverride={stylesOverride}
    >
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
          label={backendsCopy.namespaces}
          options={namespaceOptions}
          selected={namespace}
          onChange={(v) => l.setFilter({ namespace: single(namespace, v) })}
        />
        {l.data?.hidden ? (
          <span className="label" data-testid="backends-hidden">
            <EyeOff size={14} aria-hidden="true" /> {backendsCopy.hidden}
          </span>
        ) : null}
      </Narrow>
      <StaleGate collectedAt={l.data?.collectedAt} failed={!!l.error}>
        <BackendTable
          installId={l.id}
          page={page}
          busy={l.busy}
          onClearFilters={l.filtered ? l.clearFilters : undefined}
          sort={l.sort}
          onSort={l.setSort}
          onPage={l.setCursor}
          emptyTitle={l.filtered ? listCopy.emptyFiltered(backendsCopy.noun[1]) : listCopy.empty(backendsCopy.noun[1])}
        />
      </StaleGate>
    </ListFrame>
  );
}
