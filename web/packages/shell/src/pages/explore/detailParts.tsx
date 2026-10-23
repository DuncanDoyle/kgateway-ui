import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { type ReactNode, useEffect, useMemo, useState } from "react";
import type { ApiError } from "../../api/client";
import type { Candidate, LinkedRef, Page } from "../../api/types";
import { CandidateBanner } from "../../explore/CandidateBanner";
import { CopyCommandButton } from "../../explore/CopyCommandButton";
import { DetailTabs } from "../../explore/DetailTabs";
import { type ObjectColumn, ObjectTable } from "../../explore/ObjectTable";
import { HealthBadge } from "../../health/HealthBadge";
import { CopyNameButton } from "../../resource/CopyNameButton";
import { ResourceName } from "../../resource/ResourceName";
import { ResourceYaml } from "../../resource/ResourceYaml";
import { useVocabulary } from "../../shell/extension";
import { installPath } from "../../shell/paths";
import { useUrlParams } from "../../shell/urlParams";
import { BackLink } from "../../states/controls";
import { PageNotice } from "../../states/PageNotice";
import { PageState } from "../../states/PageState";
import { Shimmer } from "../../states/Shimmer";
import type { HealthState } from "../../theme/tokens";
import { DataAge } from "../DataAge";
import { useShownPage } from "./listParts";

// region Styles
/** Height of the header row, fixed from the first render: the name comes from the URL, the rest fills in. */
export const HEADER_H = 40;

const Frame = styled.div<{ stylesOverride?: SerializedStyles }>`
  max-width: 1400px;
  margin: 0 auto;
  padding: ${(p) => p.theme.space(4)};
  header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: ${(p) => p.theme.space(2)};
    min-height: ${HEADER_H}px;
    margin-bottom: ${(p) => p.theme.space(2)};
  }
  h1 {
    display: inline-flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: ${(p) => p.theme.space(2)};
    min-width: 0;
    margin: 0;
    font-size: ${(p) => p.theme.font.size.lg};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    overflow-wrap: anywhere;
  }
  .kind {
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.base};
    font-weight: ${(p) => p.theme.font.weight.regular};
  }
  .spacer {
    flex: 1;
  }
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
const Skeleton = styled.div`
  display: grid;
  gap: ${(p) => p.theme.space(3)};
`;

// region Helpers
const TABS = [
  { id: "overview", label: "Overview" },
  { id: "yaml", label: "YAML" },
];

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
  /** The Overview tab; rendered only once data has loaded. */
  children: ReactNode;
  stylesOverride?: SerializedStyles;
}

/** A detail page: candidate banner, header, Overview and YAML tabs (`?tab=yaml`), and the detail states (design §8.6). */
export function DetailFrame({ installId, refValue, noun, data, error, children, stylesOverride }: DetailFrameProps) {
  const vocab = useVocabulary();
  const [params, update] = useUrlParams();
  const tab = params.get("tab") === "yaml" ? "yaml" : "overview";
  const errorTitle = `Cannot load ${noun}`;
  // A hidden object 404s as an absent one does (D-013).
  if (error?.status === 404 && !data)
    return (
      <PageState
        kind="empty"
        title="Not in this install's scope"
        message={
          <>
            {refValue.kind} <ResourceName namespace={refValue.namespace} name={refValue.name} />
            <br />
            <BackLink to={installPath(installId)}>Back to install</BackLink>
          </>
        }
      />
    );
  const overview = data ? (
    children
  ) : error?.status === 503 ? (
    <PageState kind="connecting" title={vocab.connecting} message={error.message} />
  ) : error ? (
    <PageState kind="error" title={errorTitle} message={error.message} copyText={error.message} />
  ) : (
    <Skeleton aria-busy="true" data-testid="detail-skeleton">
      <Shimmer h={20} />
      <Shimmer h={120} />
      <Shimmer h={200} />
    </Skeleton>
  );
  return (
    <Frame stylesOverride={stylesOverride}>
      {data && error ? <PageNotice error={error} title={errorTitle} /> : null}
      {data?.candidate ? <CandidateBanner candidate={data.candidate} installId={installId} /> : null}
      <header data-testid="detail-header">
        <h1>
          <span className="kind">{refValue.kind}</span>
          <ResourceName namespace={refValue.namespace} name={refValue.name} />
        </h1>
        <CopyNameButton resource={refValue} />
        <CopyCommandButton refValue={refValue} />
        {data ? <HealthBadge state={data.health} /> : null}
        <span className="spacer" />
        {data ? <DataAge collectedAt={data.collectedAt} /> : null}
      </header>
      <DetailTabs
        label={`${refValue.kind} views`}
        tabs={TABS}
        selected={tab}
        onSelect={(t) => update({ tab: t === "yaml" ? t : null })}
      >
        {tab === "yaml" ? <ResourceYaml installId={installId} refValue={refValue} /> : overview}
      </DetailTabs>
    </Frame>
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
}

const NO_SORT = { key: "name", desc: false };

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
  return (
    <>
      {l.error && !l.data ? <PageNotice error={l.error} title={`Cannot load ${noun[1]}`} /> : null}
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
        sort={NO_SORT}
        onSort={() => {}}
        onPage={(next) => setCursor({ key, c: next })}
      />
    </>
  );
}
