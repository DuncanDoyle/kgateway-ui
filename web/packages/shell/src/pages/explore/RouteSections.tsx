import styled from "@emotion/styled";
import { useMemo, useState } from "react";
import { useRoutes } from "../../api/exploreHooks";
import type { Attachment, LinkedRef, Listener, ParentSummary, RouteCounts, RouteRow } from "../../api/types";
import { columnCopy, gatewayCopy, listenerCopy, viewACopy } from "../../copy/explorer";
import { ComputedBadge, ComputedChip, computedTooltip } from "../../explore/ComputedBadge";
import { FOOTER_H, type ObjectColumn, ROW_H } from "../../explore/ObjectTable";
import { HealthBadge } from "../../health/HealthBadge";
import { installPath } from "../../shell/paths";
import { Chip, InlineLink } from "../../states/controls";
import { Shimmer } from "../../states/Shimmer";
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

/** Text, then a compact badge; the text shrinks first. */
const WithBadge = styled.span`
  display: flex;
  align-items: center;
  gap: ${(p) => p.theme.space(2)};
  min-width: 0;
  & > :first-child {
    flex: 1 1 auto;
    min-width: 0;
  }
  /* The badge keeps its size up to half the cell; its state text is cut inside it. */
  & > :last-child {
    flex: 0 0 auto;
    max-width: 50%;
  }
`;
/** A listener whose members are not known: its state and a way out, never an empty table (design §8.6). */
const NotKnown = styled.div`
  display: grid;
  gap: ${(p) => p.theme.space(2)};
  justify-items: start;
  padding: ${(p) => p.theme.space(3)};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.surface};
  font-size: ${(p) => p.theme.font.size.sm};
`;

/** Height of a table header row (ObjectTable th). */
const HEAD_H = 36;

const Bar = styled.div`
  min-height: 32px;
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
/** The row's parent entries for parent, direct or through a ListenerSet. */
const under = (r: RouteRow, parent: LinkedRef) => r.parents.filter((p) => same(p.ref, parent) || same(p.via, parent));

/** The value of listener= that selects rows view A could not place (design §7.2); no listener name can be it. */
export const UNPLACED = "_unknown";
const hidden = (l?: Listener) => l?.computed?.state === "unknown" || l?.computed?.state === "restricted";

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
          p.computed?.explanation ? `· ${p.computed.explanation}` : "",
        ].filter(Boolean);
      // The explanation is computed, so it carries a badge; reason and message are the controller's.
      const explained = ps.find((p) => p.computed?.explanation)?.computed;
      const line = (
        <OneLine text={ps.map((p) => parts(p).join(" ")).join("; ")} placement="end">
          <Why>
            {ps.map((p, i) => (
              <span key={[p.ref.kind, p.ref.namespace, p.ref.name, p.sectionName, p.port].join("/")}>
                {i ? "; " : null}
                {attachmentLabel[p.attachment]}
                {p.sectionName ? <Muted> · {p.sectionName}</Muted> : null}
                {p.conditionType && p.reason ? <span className="reason">{p.reason}</span> : null}
                {p.conditionType && p.message ? <span className="message">{p.message}</span> : null}
                {p.computed?.explanation ? (
                  <>
                    <Muted className="message">·</Muted>
                    <span className="message">{p.computed.explanation}</span>
                  </>
                ) : null}
              </span>
            ))}
          </Why>
        </OneLine>
      );
      return explained ? (
        <WithBadge>
          {line}
          <ComputedBadge computed={explained.computed} compact />
        </WithBadge>
      ) : (
        line
      );
    },
  };
}

/** Each row's effective hostnames on the parent's own listener, computed per (parentRef, listener). */
function hostnamesColumn(parent: LinkedRef, listener: string, ruleSet: string): ObjectColumn<RouteRow> {
  const tip = computedTooltip({ by: "ui", rules: ruleSet, rule: "hostname-intersection", state: "checked" });
  return {
    id: "effective",
    header: viewACopy.effectiveHostnames,
    headerExtra: <ComputedChip label={tip} />,
    width: "34%",
    cell: (r) => {
      // A ListenerSet under this Gateway may have a listener of the same name: only the parent's own count here.
      const ls = under(r, parent)
        .flatMap((p) => p.computed?.listeners ?? [])
        .filter((l) => l.listener === listener && same(l.resource, parent));
      const first = ls[0];
      if (!first) return null;
      const hosts = [...new Set(ls.flatMap((l) => l.effectiveHostnames ?? []))];
      const known = first.computed.state === "checked" || first.computed.state === "disagrees";
      // Hostnames are sent only for checked or disagrees; an empty list there means any hostname.
      const text = !known ? "" : hosts.length ? hosts.join(", ") : listenerCopy.anyHostname;
      return (
        <WithBadge>
          {text ? <OneLine text={text} /> : null}
          <ComputedBadge computed={first.computed} compact chip={false} />
        </WithBadge>
      );
    },
  };
}

/** Why view A could not place the row's ref to parent: the unknown reason, as a computed fact. */
function notPlacedColumn(parent: LinkedRef): ObjectColumn<RouteRow> {
  return {
    id: "notPlaced",
    header: viewACopy.whyNotPlaced,
    width: "40%",
    cell: (r) => {
      // The ref the UI could not place; another ref of the row (e.g. through a ListenerSet) may have a result.
      const ps = under(r, parent);
      const c = (ps.find((p) => p.computed?.computed.state === "unknown") ?? ps.find((p) => p.computed))?.computed
        ?.computed;
      return c ? <ComputedBadge computed={c} compact="long" /> : null;
    },
  };
}
const rowRef = (r: RouteRow) => r.ref;

// region Component
/** The Gateway page's listener selection (design §8.1); absent where the page or the server has none. */
export interface ListenerPick {
  listeners: Listener[];
  /** A listener name, UNPLACED, or undefined for every attached route. */
  selected?: string;
  onSelect: (name?: string) => void;
  /** False while /meta loads: the bar's place is kept, so nothing moves when the chips arrive. */
  ready: boolean;
  /** The detail's rule set, for the computed columns' header chips. */
  ruleSet: string;
}

interface RouteSectionsProps {
  installId: string;
  parent: LinkedRef;
  counts: RouteCounts;
  pick?: ListenerPick;
}

/** Attached routes, optionally per listener, and routes that failed to attach with Not accepted, Waiting and Unwatched sub-filters. */
export function RouteSections({ installId, parent, counts, pick }: RouteSectionsProps) {
  const [failed, setFailed] = useState<Failed>("not-accepted");
  const p = refParam(parent);
  const sel = pick?.selected;
  const listener = pick?.listeners.find((l) => l.name === sel);
  // Only a server that accepts listener= gets it; an unknown name has nothing to ask for.
  const absent = !!pick?.ready && !!sel && sel !== UNPLACED && !listener;
  // A listener selects view A's members; without one the section lists status-accepted routes.
  const attachedQuery = useMemo(
    (): Record<string, string> => (sel ? { parent: p, listener: sel } : { parent: p, attachment: "accepted" }),
    [p, sel],
  );
  const failedQuery = useMemo(() => ({ parent: p, attachment: failed }), [p, failed]);
  const base = useMemo(() => baseColumns(installId), [installId]);
  const failedCols = useMemo(() => [...base, whyColumn(parent)], [base, parent]);
  const attachedCols = useMemo(
    () =>
      !sel
        ? base
        : sel === UNPLACED
          ? [...base, notPlacedColumn(parent)]
          : [...base, hostnamesColumn(parent, sel, pick?.ruleSet ?? "")],
    [base, parent, sel, pick?.ruleSet],
  );
  const title = !sel
    ? gatewayCopy.attachedRoutes
    : sel === UNPLACED
      ? viewACopy.notPlacedTitle
      : viewACopy.routesOnListener(sel);
  const known = listener?.computed && !hidden(listener) ? listener.computedRoutes : undefined;
  return (
    <>
      <Section aria-labelledby="attached-routes">
        <h2 id="attached-routes">
          {title} {sel ? null : <span className="note">{counts.accepted}</span>}
        </h2>
        {pick?.ready ? (
          <Bar role="group" aria-label={viewACopy.listenerGroup} data-testid="listener-bar">
            <Chip type="button" aria-pressed={!sel} onClick={() => pick.onSelect(undefined)}>
              {viewACopy.allListeners}
            </Chip>
            {pick.listeners.map((l) => (
              <Chip key={l.name} type="button" aria-pressed={sel === l.name} onClick={() => pick.onSelect(l.name)}>
                {l.name}
              </Chip>
            ))}
            <Chip type="button" aria-pressed={sel === UNPLACED} onClick={() => pick.onSelect(UNPLACED)}>
              {viewACopy.notPlaced}
            </Chip>
          </Bar>
        ) : pick ? (
          // /meta is loading: keep the bar's place.
          <Bar aria-hidden="true" data-testid="listener-bar" />
        ) : null}
        {pick && !pick.ready && sel ? (
          // A selected listener waits for /meta, at the height its table will have.
          <Shimmer h={expectedRows(sel === UNPLACED ? 1 : (known ?? 1)) * ROW_H + HEAD_H + FOOTER_H + 2} />
        ) : absent || (listener?.computed && hidden(listener)) ? (
          <NotKnown data-testid="listener-routes-unknown">
            {absent ? (
              <span>{viewACopy.noSuchListener(parent.kind, sel)}</span>
            ) : listener?.computed ? (
              <>
                <ComputedBadge computed={listener.computed} />
                <span>{viewACopy.cannotList}</span>
              </>
            ) : null}
            <InlineLink to={`${installPath(installId, "routes")}?${new URLSearchParams({ parent: p })}`}>
              {viewACopy.allRoutesOf(parent.kind)}
            </InlineLink>
          </NotKnown>
        ) : (
          <SubList
            installId={installId}
            label={title}
            noun={NOUN}
            useList={useRoutes}
            query={attachedQuery}
            columns={attachedCols}
            rowRef={rowRef}
            emptyTitle={
              !sel
                ? gatewayCopy.noRoutesAttached
                : sel === UNPLACED
                  ? viewACopy.noRoutesNotPlaced
                  : viewACopy.noRoutesOnListener
            }
            skeletonRows={expectedRows(sel ? (known ?? 1) : counts.accepted)}
            // The install-wide hidden-routes line shows once, under the failed table.
            showDenied={false}
          />
        )}
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
