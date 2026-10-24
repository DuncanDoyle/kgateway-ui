import styled from "@emotion/styled";
import { useMemo, useState } from "react";
import { useRoutes } from "../../api/exploreHooks";
import type { Attachment, LinkedRef, ParentSummary, RouteCounts, RouteRow } from "../../api/types";
import { columnCopy, gatewayCopy } from "../../copy/explorer";
import type { ObjectColumn } from "../../explore/ObjectTable";
import { HealthBadge } from "../../health/HealthBadge";
import { Chip } from "../../states/controls";
import { expectedRows, refParam, Section, SubList } from "./detailParts";
import { attachmentLabel, Muted, NameCell, OneLine } from "./listParts";

// region Styles
const Why = styled.span`
  font-size: ${(p) => p.theme.font.size.sm};
  .reason {
    margin-left: 6px;
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
    color: ${(p) => p.theme.color.textMuted};
  }
  .message {
    margin-left: 6px;
    color: ${(p) => p.theme.color.textMuted};
  }
`;

const Bar = styled.div`
  display: flex;
  flex-wrap: wrap;
  gap: ${(p) => p.theme.space(1)};
  margin-bottom: ${(p) => p.theme.space(2)};
`;

// region Helpers
const NOUN = ["route", "routes"] as const;
type Failed = Extract<Attachment, "not-accepted" | "pending" | "unwatched">;
const FAILED: { value: Failed; label: string; count: keyof RouteCounts }[] = [
  { value: "not-accepted", label: gatewayCopy.subFilter["not-accepted"], count: "notAccepted" },
  { value: "pending", label: gatewayCopy.subFilter.pending, count: "pending" },
  { value: "unwatched", label: gatewayCopy.subFilter.unwatched, count: "unwatched" },
];

const same = (a: LinkedRef | undefined, b: LinkedRef) =>
  !!a && a.group === b.group && a.kind === b.kind && a.namespace === b.namespace && a.name === b.name;

function baseColumns(installId: string): ObjectColumn<RouteRow>[] {
  return [
    { id: "health", header: columnCopy.status, width: "120px", cell: (r) => <HealthBadge state={r.health} /> },
    { id: "name", header: columnCopy.route, cell: (r) => <NameCell r={r.ref} installId={installId} /> },
    { id: "kind", header: columnCopy.kind, width: "120px", secondary: true, cell: (r) => r.ref.kind },
    {
      id: "hostnames",
      header: columnCopy.hostnames,
      width: "22%",
      secondary: true,
      cell: (r) =>
        r.hostnames.length ? <OneLine text={r.hostnames.join(", ")} /> : <Muted>{columnCopy.inherited}</Muted>,
    },
  ];
}

/** Why the route did not attach to parent: the deciding condition's reason as written, and its message, on one line. */
function whyColumn(parent: LinkedRef): ObjectColumn<RouteRow> {
  return {
    id: "why",
    header: columnCopy.why,
    width: "34%",
    cell: (r) => {
      const ps = r.parents.filter((p) => same(p.ref, parent) || same(p.via, parent));
      // A reason without a condition type is ours, not the controller's: never shown as a code.
      const parts = (p: ParentSummary) =>
        [
          attachmentLabel[p.attachment],
          p.sectionName ? `· ${p.sectionName}` : "",
          p.conditionType ? (p.reason ?? "") : "",
          p.conditionType ? (p.message ?? "") : "",
        ].filter(Boolean);
      return (
        <OneLine text={ps.map((p) => parts(p).join(" ")).join("; ")} placement="end">
          <Why>
            {ps.map((p, i) => (
              <span key={[p.ref.kind, p.ref.namespace, p.ref.name, p.sectionName, p.port].join("/")}>
                {i ? "; " : null}
                {attachmentLabel[p.attachment]}
                {p.sectionName ? <Muted> · {p.sectionName}</Muted> : null}
                {p.conditionType && p.reason ? <span className="reason">{p.reason}</span> : null}
                {p.conditionType && p.message ? <span className="message">{p.message}</span> : null}
              </span>
            ))}
          </Why>
        </OneLine>
      );
    },
  };
}
const rowRef = (r: RouteRow) => r.ref;

// region Component
interface RouteSectionsProps {
  installId: string;
  parent: LinkedRef;
  counts: RouteCounts;
}

/** Attached routes, and routes that failed to attach with Not accepted, Waiting and Unwatched sub-filters. */
export function RouteSections({ installId, parent, counts }: RouteSectionsProps) {
  const [failed, setFailed] = useState<Failed>("not-accepted");
  const p = refParam(parent);
  const attachedQuery = useMemo(() => ({ parent: p, attachment: "accepted" }), [p]);
  const failedQuery = useMemo(() => ({ parent: p, attachment: failed }), [p, failed]);
  const base = useMemo(() => baseColumns(installId), [installId]);
  const failedCols = useMemo(() => [...base, whyColumn(parent)], [base, parent]);
  return (
    <>
      <Section aria-labelledby="attached-routes">
        <h2 id="attached-routes">
          {gatewayCopy.attachedRoutes} <span className="note">{counts.accepted}</span>
        </h2>
        <SubList
          installId={installId}
          label={gatewayCopy.attachedRoutes}
          noun={NOUN}
          useList={useRoutes}
          query={attachedQuery}
          columns={base}
          rowRef={rowRef}
          emptyTitle={gatewayCopy.noRoutesAttached}
          skeletonRows={expectedRows(counts.accepted)}
          // The install-wide hidden-routes line shows once, under the failed table.
          showDenied={false}
        />
      </Section>
      <Section aria-labelledby="failed-routes">
        <h2 id="failed-routes">{gatewayCopy.notAttached}</h2>
        <Bar role="group" aria-label={gatewayCopy.notAttachedGroup}>
          {FAILED.map((f) => (
            <Chip key={f.value} type="button" aria-pressed={failed === f.value} onClick={() => setFailed(f.value)}>
              {f.label} ({counts[f.count]})
            </Chip>
          ))}
        </Bar>
        <SubList
          installId={installId}
          label={gatewayCopy.notAttached}
          noun={NOUN}
          useList={useRoutes}
          query={failedQuery}
          columns={failedCols}
          rowRef={rowRef}
          emptyTitle={gatewayCopy.noRoutesInState}
          skeletonRows={expectedRows(counts[FAILED.find((f) => f.value === failed)?.count ?? "notAccepted"])}
        />
      </Section>
    </>
  );
}
