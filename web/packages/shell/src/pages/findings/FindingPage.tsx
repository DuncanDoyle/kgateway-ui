import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { useNow } from "../../api/clock";
import { useFindingDetail } from "../../api/diagHooks";
import { isStale } from "../../api/hooks";
import type { LinkedRef, NextStep } from "../../api/types";
import { columnCopy, findingsCopy, listCopy } from "../../copy/explorer";
import { EvidenceView, LearnMore } from "../../diagnostics/EvidenceView";
import { NextSteps } from "../../diagnostics/NextSteps";
import { SuggestedFixView, withCode } from "../../diagnostics/SuggestedFix";
import { LinkedName } from "../../explore/LinkedName";
import { type ObjectColumn, ObjectTable } from "../../explore/ObjectTable";
import { StaleView } from "../../health/staleView";
import { useInstallId, useVocabulary } from "../../shell/extension";
import { layout } from "../../shell/layout";
import { installPath } from "../../shell/paths";
import { BackLink } from "../../states/controls";
import { PageNotice } from "../../states/PageNotice";
import { PageState } from "../../states/PageState";
import { Shimmer } from "../../states/Shimmer";
import { DataAge } from "../DataAge";
import { expectedRows, Facts, Section } from "../explore/detailParts";
import { NameCell } from "../explore/listParts";
import { FindingContributions } from "../install/FindingRow";
import { ago } from "../install/HealthStrip";
import { SeverityBadge } from "./FindingsPage";

// region Styles
/** Height of the title row, fixed from the first render so the message arriving moves nothing. */
const TITLE_H = 32;

const Frame = styled.div<{ stylesOverride?: SerializedStyles }>`
  max-width: ${layout.maxWidth}px;
  margin: 0 auto;
  padding: ${(p) => p.theme.space(4)};
  header {
    margin-bottom: ${(p) => p.theme.space(4)};
  }
  .back {
    display: inline-block;
    margin-bottom: ${(p) => p.theme.space(2)};
  }
  h1 {
    min-height: ${TITLE_H}px;
    margin: 0;
    font-size: ${(p) => p.theme.font.size.lg};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    overflow-wrap: anywhere;
    /* Prose keeps a readable line length on wide screens (design §17.6). */
    max-width: ${layout.proseMax};
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
    min-height: 28px;
    margin-top: ${(p) => p.theme.space(1)};
  }
  .code {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.sm};
    color: ${(p) => p.theme.color.textMuted};
  }
  .spacer {
    flex: 1;
  }
  /* The finding's facts, its evidence and contributed actions, spaced as separate blocks. */
  .details {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: ${(p) => p.theme.space(4)};
  }
  .prose {
    max-width: ${layout.proseMax};
  }
  ${(p) => p.stylesOverride}
`;
const Skeleton = styled.div`
  display: grid;
  gap: ${(p) => p.theme.space(3)};
`;

// region Helpers
const AFFECTED_NOUN = ["object", "objects"] as const;
const NO_SORT = { key: "name", desc: false };
const affectedColumns = (installId: string): ObjectColumn<LinkedRef>[] => [
  { id: "name", header: findingsCopy.object, cell: (r) => <NameCell r={r} installId={installId} /> },
  { id: "kind", header: columnCopy.kind, width: "180px", cell: (r) => r.kind },
];
const self = (r: LinkedRef) => r;

// region Component
function StepsSection({ steps }: { steps: NextStep[] }) {
  return (
    <Section aria-labelledby="finding-steps">
      <h2 id="finding-steps">{findingsCopy.nextSteps}</h2>
      <NextSteps steps={steps} />
    </Section>
  );
}

interface FindingPageProps {
  stylesOverride?: SerializedStyles;
}

/** One finding: what it says, where, its suggested fix and next steps, and the objects its cause affects (design §17.1). */
export function FindingPage({ stylesOverride }: FindingPageProps = {}) {
  const id = useInstallId();
  const { findingId = "" } = useParams();
  const vocab = useVocabulary();
  // The affected cursor is local, as on a detail page's sub-lists, so the page URL stays short.
  const [cursor, setCursor] = useState<{ finding: string; c: string }>();
  const c = cursor?.finding === findingId ? cursor.c : undefined;
  const { data, error, cursorRejected, isLaggy } = useFindingDetail(id, findingId, c);
  useEffect(() => {
    if (cursorRejected) setCursor(undefined);
  }, [cursorRejected]);
  const now = useNow();
  const back = <BackLink to={installPath(id, "findings")}>{findingsCopy.allFindings}</BackLink>;
  const errorTitle = listCopy.cannotLoad("finding");
  // A finding resolved while open 404s: it reads as gone, not as a load failure, even with data cached.
  if (error?.status === 404) return <PageState kind="empty" title={findingsCopy.notFound} message={back} />;
  const f = data?.finding;
  // The wire finding as a Finding, for the renderers and actions a downstream registers.
  const finding = f && data ? { ...f, install: id, message: f.message ?? "", evidence: data.evidence } : undefined;
  // Stale data or a failed poll turns every state on the page unknown (design §6.3).
  const stale = !!data && (!!error || isStale(data.collectedAt, now));
  const body = data ? null : error?.status === 503 ? (
    <PageState kind="connecting" title={vocab.connecting} message={error.message} />
  ) : error && !cursorRejected ? (
    <PageState kind="error" title={errorTitle} message={error.message} copyText={error.message} />
  ) : (
    <Skeleton aria-busy="true" data-testid="detail-skeleton">
      <Shimmer h={60} />
      <Shimmer h={200} />
    </Skeleton>
  );
  return (
    <StaleView.Provider value={stale}>
      <Frame data-testid="page-frame" stylesOverride={stylesOverride}>
        {data && error && !cursorRejected ? <PageNotice error={error} title={errorTitle} /> : null}
        <header data-testid="detail-header">
          <span className="back">{back}</span>
          <h1>{f ? f.restricted ? findingsCopy.restricted : f.message : <Shimmer h={24} />}</h1>
          <div className="meta">
            {f ? <SeverityBadge severity={f.severity} /> : null}
            {f ? <span className="code">{f.code}</span> : null}
            {f ? <LearnMore code={f.code} /> : null}
            <span className="spacer" />
            {data ? <DataAge collectedAt={data.collectedAt} /> : null}
          </div>
        </header>
        {body}
        {data && f ? (
          <>
            <Section aria-label={listCopy.details} className="details">
              <Facts>
                {f.resource ? (
                  <>
                    <dt>{findingsCopy.object}</dt>
                    <dd>
                      {f.resource.kind} <LinkedName refValue={f.resource} installId={id} />
                    </dd>
                  </>
                ) : null}
                {f.since ? (
                  <>
                    <dt>{findingsCopy.firstSeen}</dt>
                    <dd>{ago(f.since, now)}</dd>
                  </>
                ) : null}
                {f.detail ? (
                  <>
                    <dt>{findingsCopy.detail}</dt>
                    <dd className="prose">{withCode(f.detail)}</dd>
                  </>
                ) : null}
              </Facts>
              {/* EvidenceView owns the finding renderers (unknown or no evidence); actions follow (design §4.1 gap 6). */}
              {finding ? <EvidenceView evidence={data.evidence} finding={finding} /> : null}
              {finding ? <FindingContributions finding={finding} /> : null}
            </Section>
            {data.suggestedFix ? <SuggestedFixView fix={data.suggestedFix} installId={id} /> : null}
            {data.nextSteps?.length ? <StepsSection steps={data.nextSteps} /> : null}
            {/* A restricted finding has no affected objects to show (design §14 rule 13). */}
            {f.restricted ? null : (
              <Section aria-labelledby="finding-affected">
                <h2 id="finding-affected">
                  {findingsCopy.affected} <span className="note">{data.affected.total}</span>
                </h2>
                <ObjectTable
                  label={findingsCopy.affected}
                  installId={id}
                  noun={AFFECTED_NOUN}
                  columns={affectedColumns(id)}
                  rowRef={self}
                  page={data.affected}
                  loading={false}
                  busy={isLaggy}
                  emptyTitle={findingsCopy.noAffected}
                  skeletonRows={expectedRows(data.affected.total)}
                  showDenied={false}
                  sort={NO_SORT}
                  onSort={() => {}}
                  onPage={(next) => setCursor({ finding: findingId, c: next })}
                />
              </Section>
            )}
          </>
        ) : null}
      </Frame>
    </StaleView.Provider>
  );
}
