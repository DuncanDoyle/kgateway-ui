import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { type ReactNode, useEffect, useId, useMemo, useState } from "react";
import type { ApiError } from "../../api/client";
import { useNow } from "../../api/clock";
import { isStale } from "../../api/hooks";
import type { Candidate, LinkedRef, Page } from "../../api/types";
import { detailCopy, listCopy } from "../../copy/explorer";
import { CandidateBanner } from "../../explore/CandidateBanner";
import { CopyCommandButton } from "../../explore/CopyCommandButton";
import { DetailTabs } from "../../explore/DetailTabs";
import { type ObjectColumn, ObjectTable } from "../../explore/ObjectTable";
import { StaleView } from "../../health/staleView";
import { CopyNameButton } from "../../resource/CopyNameButton";
import { ResourceName } from "../../resource/ResourceName";
import { ResourceYaml } from "../../resource/ResourceYaml";
import { TrailRefsProvider, useReportTrailRefs, useTrailRefsCollector } from "../../shell/Breadcrumbs";
import { Slot, useDetailTabContributions, useSideSectionContributions } from "../../shell/contributions";
import { useVocabulary } from "../../shell/extension";
import { DetailColumns, layout } from "../../shell/layout";
import { OBJECT_HEADER_H, ObjectHeader } from "../../shell/ObjectHeader";
import { installPath } from "../../shell/paths";
import { useUrlParams } from "../../shell/urlParams";
import { BackLink } from "../../states/controls";
import { PageNotice } from "../../states/PageNotice";
import { PageState } from "../../states/PageState";
import { Shimmer } from "../../states/Shimmer";
import type { HealthState } from "../../theme/tokens";
import { useShownPage } from "./listParts";

// region Styles
/** Height of the detail header, fixed from the first render: the name comes from the URL, the rest fills in. */
export const HEADER_H = OBJECT_HEADER_H;

const Frame = styled.div<{ stylesOverride?: SerializedStyles }>`
  max-width: ${layout.maxWidth}px;
  margin: 0 auto;
  padding: ${(p) => p.theme.space(4)};
  ${(p) => p.stylesOverride}
`;
export const Section = styled.section`
  margin-bottom: ${(p) => p.theme.space(5)};
  h2 {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(3)};
    margin: 0 0 ${(p) => p.theme.space(2)};
    font-size: ${(p) => p.theme.font.size.base};
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  h2 .note {
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
    font-weight: ${(p) => p.theme.font.weight.regular};
  }
`;
/** Label and value pairs under a detail header. */
export const Facts = styled.dl`
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 4px ${(p) => p.theme.space(3)};
  margin: 0;
  font-size: ${(p) => p.theme.font.size.sm};
  dt {
    color: ${(p) => p.theme.color.textMuted};
  }
  dd {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }
`;
const Stack = styled.div`
  display: grid;
  gap: ${(p) => p.theme.space(3)};
`;

// region Helpers
const TABS = [
  { id: "overview", label: detailCopy.overview },
  { id: "yaml", label: detailCopy.yaml },
];
// Page tab ids are reserved even where a page omits its tab, so a contribution never takes one over.
const CORE_TAB_IDS = [...TABS.map((t) => t.id), "delegation"];

/** A page's own tab after Overview and YAML, e.g. the route's Delegation tab (`?tab=delegation`). */
export interface ExtraTab {
  id: string;
  label: string;
  /** Rendered only once data has loaded. */
  content: ReactNode;
}

/** group/Kind/namespace/name, as the list endpoints take a parent (design §7.2). */
export const refParam = (r: LinkedRef) => [r.group, r.kind, r.namespace ?? "", r.name].join("/");

// region Component
interface DetailFrameProps {
  installId: string;
  /** The object from the URL, so the header renders before the data. */
  refValue: LinkedRef;
  noun: string;
  data?: { health: HealthState; candidate?: Candidate; collectedAt: string };
  error?: ApiError;
  /** The Overview tab's main column; rendered only once data has loaded. */
  children: ReactNode;
  /** The Overview tab's side column, for longer auxiliary content. Contributed sections follow it; with neither, one column. */
  side?: ReactNode;
  /** Tabs after YAML; a ?tab= naming none of them shows Overview. */
  extraTabs?: ExtraTab[];
  /** Refs the page's data links, for the trail above the header. */
  trailRefs?: readonly LinkedRef[];
  stylesOverride?: SerializedStyles;
}

/** A detail page: candidate banner, header, Overview and YAML tabs (`?tab=yaml`), and the detail states (design §8.6). */
export function DetailFrame({
  installId,
  refValue,
  noun,
  data,
  error,
  children,
  side,
  extraTabs,
  trailRefs,
  stylesOverride,
}: DetailFrameProps) {
  const vocab = useVocabulary();
  const [params, update] = useUrlParams();
  const coreIds = useMemo(() => [...CORE_TAB_IDS, ...(extraTabs ?? []).map((t) => t.id)], [extraTabs]);
  const contributed = useDetailTabContributions(refValue, coreIds);
  const sections = useSideSectionContributions(refValue);
  const tabs = useMemo(
    () => [...TABS, ...[...(extraTabs ?? []), ...contributed].map(({ id, label }) => ({ id, label }))],
    [extraTabs, contributed],
  );
  const asked = params.get("tab");
  const tab = tabs.find((t) => t.id === asked)?.id ?? "overview";
  const extra = extraTabs?.find((t) => t.id === tab);
  const own = contributed.find((t) => t.id === tab);
  const ctx = { installId, ref: refValue };
  const sideId = useId();
  const objectKey = `${installId}|${refParam(refValue)}`;
  const errorTitle = listCopy.cannotLoad(noun);
  const now = useNow();
  // Stale data or a failed poll turns every state on the page unknown, as on the lists (design §6.3).
  const stale = !!data && (!!error || isStale(data.collectedAt, now));
  // The trail resolves crumbs through refs the page holds and refs its sub-tables report.
  const by = data?.candidate?.by;
  const heldRefs = useMemo(() => [...(trailRefs ?? []), ...(by ? [by] : [])], [trailRefs, by]);
  const [pageRefs, reportRefs] = useTrailRefsCollector(heldRefs);
  // A hidden object 404s as an absent one does (D-013).
  if (error?.status === 404 && !data)
    return (
      <PageState
        kind="empty"
        title={listCopy.notInScope}
        message={
          <>
            {refValue.kind} <ResourceName namespace={refValue.namespace} name={refValue.name} />
            <br />
            <BackLink to={installPath(installId)}>{listCopy.backToInstall}</BackLink>
          </>
        }
      />
    );
  const overview = data ? (
    extra ? (
      extra.content
    ) : own ? (
      <Slot key={own.id} id={`detail-tab:${own.id}`} resetKey={objectKey} render={() => own.render(ctx)} />
    ) : (
      <DetailColumns
        main={children}
        side={
          side || sections.length ? (
            <>
              {side}
              {sections.map((s) => (
                <Slot
                  key={s.id}
                  id={`side-section:${s.id}`}
                  resetKey={objectKey}
                  render={() => (
                    <Section aria-labelledby={`${sideId}-${s.id}`} data-slot="side-section">
                      <h2 id={`${sideId}-${s.id}`}>{s.title}</h2>
                      {s.render(ctx)}
                    </Section>
                  )}
                />
              ))}
            </>
          ) : null
        }
      />
    )
  ) : error?.status === 503 ? (
    <PageState kind="connecting" title={vocab.connecting} message={error.message} />
  ) : error ? (
    <PageState kind="error" title={errorTitle} message={error.message} copyText={error.message} />
  ) : (
    <div aria-busy="true" data-testid="detail-skeleton">
      <DetailColumns
        main={
          <Stack>
            <Shimmer h={20} />
            <Shimmer h={200} />
          </Stack>
        }
        // Only a page with contributed sections has a side column to hold a place for.
        side={sections.length && tab === "overview" ? <Shimmer h={120} /> : null}
      />
    </div>
  );
  return (
    <StaleView.Provider value={stale}>
      <TrailRefsProvider value={reportRefs}>
        <Frame stylesOverride={stylesOverride}>
          {data && error ? <PageNotice error={error} title={errorTitle} /> : null}
          {data?.candidate ? <CandidateBanner candidate={data.candidate} installId={installId} /> : null}
          <ObjectHeader
            kind={{ group: refValue.group, kind: refValue.kind, label: refValue.kind }}
            name={refValue.name}
            namespace={refValue.namespace}
            state={data?.health}
            collectedAt={data?.collectedAt}
            trailRefs={pageRefs}
            actions={
              <>
                <CopyNameButton resource={refValue} />
                <CopyCommandButton refValue={refValue} />
              </>
            }
          />
          <DetailTabs
            label={detailCopy.views(refValue.kind)}
            tabs={tabs}
            selected={tab}
            onSelect={(t) => update({ tab: t === "overview" ? null : t })}
          >
            {tab === "yaml" ? <ResourceYaml installId={installId} refValue={refValue} /> : overview}
          </DetailTabs>
        </Frame>
      </TrailRefsProvider>
    </StaleView.Provider>
  );
}

type ListHook<T> = (
  id: string,
  query: URLSearchParams,
) => { data?: Page<T>; error?: ApiError; cursorRejected: boolean; isLaggy: boolean };

interface SubListProps<T> {
  installId: string;
  label: string;
  noun: readonly [string, string];
  useList: ListHook<T>;
  /** The fixed query, e.g. parent and attachment; a change starts again at page one. */
  query: Record<string, string>;
  columns: ObjectColumn<T>[];
  rowRef: (row: T) => LinkedRef;
  emptyTitle: string;
  /** The expected row count, so the skeleton has the loaded table's height. */
  skeletonRows: number;
  showDenied?: boolean;
}

const NO_SORT = { key: "name", desc: false };
/** The server's default page size (design §7.2): a sub-table never shows more rows than this. */
export const PAGE_LIMIT = 50;
/** Skeleton rows for an expected count: at most one page. */
export const expectedRows = (n: number) => Math.min(Math.max(0, n), PAGE_LIMIT);

/** A paged list under a detail page, through the list endpoint; its cursor is local, so the page URL stays short. */
export function SubList<T extends { health: HealthState }>({
  installId,
  label,
  noun,
  useList,
  query,
  columns,
  rowRef,
  emptyTitle,
  skeletonRows,
  showDenied,
}: SubListProps<T>) {
  const key = new URLSearchParams(query).toString();
  const [cursor, setCursor] = useState<{ key: string; c: string }>();
  const c = cursor?.key === key ? cursor.c : undefined;
  const q = useMemo(() => {
    const out = new URLSearchParams(key);
    if (c) out.set("cursor", c);
    return out;
  }, [key, c]);
  const l = useList(installId, q);
  useEffect(() => {
    if (l.cursorRejected) setCursor(undefined);
  }, [l.cursorRejected]);
  const page = useShownPage(l.data, !!l.error);
  useReportTrailRefs(useMemo(() => page?.items.map(rowRef), [page, rowRef]));
  return (
    <>
      {l.error ? <PageNotice error={l.error} title={listCopy.cannotLoad(noun[1])} /> : null}
      <ObjectTable
        label={label}
        installId={installId}
        noun={noun}
        columns={columns}
        rowRef={rowRef}
        page={page}
        loading={!l.data && !l.error}
        busy={l.isLaggy}
        emptyTitle={emptyTitle}
        skeletonRows={skeletonRows}
        showDenied={showDenied}
        sort={NO_SORT}
        onSort={() => {}}
        onPage={(next) => setCursor({ key, c: next })}
      />
    </>
  );
}
