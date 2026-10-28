// biome-ignore-all lint/a11y/noRedundantRoles: WebKit drops implicit table roles under display grid/block, so the rows state them.
import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { ChevronDown, ChevronRight, EyeOff, Info, Lock } from "lucide-react";
import { type ReactNode, useContext, useEffect, useId, useLayoutEffect, useMemo, useState } from "react";
import { useNow } from "../../../api/clock";
import { useDelegation } from "../../../api/exploreHooks";
import { isStale } from "../../../api/hooks";
import type {
  BackendRef,
  Condition,
  DelegationContext,
  DelegationOutcome,
  DelegationRef,
  DelegationRow,
  LinkedRef,
  Rule,
} from "../../../api/types";
import { columnCopy, computedCopy, delegationCopy, detailCopy, listCopy, routeCopy } from "../../../copy/explorer";
import { ComputedBadge, ComputedChip, computedTooltip } from "../../../explore/ComputedBadge";
import { ConditionList, conditionState } from "../../../explore/ConditionList";
import { LinkedName } from "../../../explore/LinkedName";
import { ROW_H } from "../../../explore/ObjectTable";
import { Pager } from "../../../explore/Pager";
import { type FilterOption, StateFilter } from "../../../explore/StateFilter";
import { HealthIcon } from "../../../health/HealthIcon";
import { StaleView } from "../../../health/staleView";
import { Tooltip } from "../../../shell/Tooltip";
import { PageNotice } from "../../../states/PageNotice";
import { Shimmer } from "../../../states/Shimmer";
import type { HealthState } from "../../../theme/tokens";
import { DataAge } from "../../DataAge";
import { expectedRows, Facts } from "../detailParts";
import { Muted, nameTip, OneLine } from "../listParts";
import { CutNote, NeutralBox, Placeholder } from "./EffectiveMatches";
import { MatchLine } from "./MatchLine";
import { RefCounts } from "./RuleList";

// region Styles
/** Below this width a row's secondary cells move under its name (design §8.7). */
const NARROW = "@media (max-width: 899px)";

const Wrap = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  gap: ${(p) => p.theme.space(3)};
  ${(p) => p.stylesOverride}
`;
const Bar = styled.div`
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: ${(p) => p.theme.space(2)};
  min-height: 32px;
`;
const Select = styled.select`
  height: 32px;
  padding: 0 ${(p) => p.theme.space(2)};
  font: inherit;
  font-size: ${(p) => p.theme.font.size.sm};
  color: ${(p) => p.theme.color.text};
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.sm};
  cursor: pointer;
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
`;
const Label = styled.label`
  display: inline-flex;
  align-items: center;
  gap: ${(p) => p.theme.space(1)};
  font-size: ${(p) => p.theme.font.size.sm};
  color: ${(p) => p.theme.color.textMuted};
`;
const RefBox = styled.section`
  display: grid;
  gap: ${(p) => p.theme.space(2)};
  min-width: 0;
  padding: ${(p) => p.theme.space(3)};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.surface};
  font-size: ${(p) => p.theme.font.size.sm};
  h3 {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 2px ${(p) => p.theme.space(2)};
    margin: 0;
    font-size: ${(p) => p.theme.font.size.base};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    overflow-wrap: anywhere;
  }
  h3 .what {
    font-weight: ${(p) => p.theme.font.weight.regular};
  }
  .summary {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(3)};
    min-height: 28px;
  }
  .footer {
    display: flex;
    justify-content: flex-end;
  }
`;
const Toggle = styled.button`
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 28px;
  padding: 0 10px 0 6px;
  font: inherit;
  font-size: ${(p) => p.theme.font.size.sm};
  color: ${(p) => p.theme.color.text};
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.sm};
  cursor: pointer;
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
`;
/** The square expander of one row. */
const RowToggle = styled(Toggle)`
  justify-content: center;
  width: 28px;
  padding: 0;
`;
const GRID = "28px minmax(0, 1.1fr) 168px minmax(0, 2fr) 168px 104px";
/** The rows as an ARIA table, so each cell is tied to its column header. */
const Rows = styled.table`
  display: grid;
  width: 100%;
  border-spacing: 0;
  thead,
  tbody {
    display: block;
  }
  th,
  td {
    display: block;
    padding: 0;
    text-align: left;
    font-weight: inherit;
  }
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.sm};
  background: ${(p) => p.theme.color.bg};
  .cols {
    display: grid;
    grid-template-columns: ${GRID};
    align-items: center;
    gap: ${(p) => p.theme.space(2)};
    height: 32px;
    padding: 0 ${(p) => p.theme.space(2)};
    font-size: ${(p) => p.theme.font.size.xs};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    color: ${(p) => p.theme.color.textMuted};
    border-bottom: 1px solid ${(p) => p.theme.color.border};
    /* Narrow rows stack their cells, so only the Computed chip stays in the header. */
    ${NARROW} {
      display: flex;
      justify-content: flex-end;
      /* Hidden from sight only: the headers still name each cell's column. */
      & > th:not(:last-child) {
        position: absolute;
        width: 1px;
        height: 1px;
        overflow: hidden;
        clip: rect(0 0 0 0);
        white-space: nowrap;
      }
    }
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }
  .cols > th {
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .item + .item {
    border-top: 1px solid ${(p) => p.theme.color.border};
  }
`;
const RowLine = styled.tr`
  display: grid;
  grid-template-columns: ${GRID};
  grid-template-areas: "toggle child outcome reason signal badge";
  align-items: center;
  gap: ${(p) => p.theme.space(2)};
  min-height: ${ROW_H}px;
  padding: 0 ${(p) => p.theme.space(2)};
  & > * {
    min-width: 0;
  }
  ${NARROW} {
    grid-template-columns: 28px minmax(0, 1fr) auto;
    grid-template-areas: "toggle child outcome" ". reason reason" ". signal badge";
    row-gap: 2px;
    padding: 6px ${(p) => p.theme.space(2)};
  }
  .toggle {
    grid-area: toggle;
  }
  .child {
    grid-area: child;
  }
  .outcome {
    grid-area: outcome;
  }
  .reason {
    grid-area: reason;
    display: flex;
    align-items: center;
    gap: ${(p) => p.theme.space(2)};
  }
  .reason > :first-child {
    flex: 0 1 auto;
    min-width: 0;
  }
  .signal {
    grid-area: signal;
  }
  .badge {
    grid-area: badge;
  }
`;
const State = styled.span`
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  white-space: nowrap;
  span {
    overflow: hidden;
    text-overflow: ellipsis;
  }
`;
const Note = styled.span`
  display: inline-flex;
  flex: none;
  align-items: center;
  gap: 4px;
  font-size: ${(p) => p.theme.font.size.xs};
  color: ${(p) => p.theme.color.textMuted};
  white-space: nowrap;
`;
const Panel = styled.div`
  display: grid;
  gap: ${(p) => p.theme.space(2)};
  padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)} ${(p) => p.theme.space(3)} 44px;
  ${NARROW} {
    padding-left: ${(p) => p.theme.space(3)};
  }
  ol {
    display: grid;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .ctx {
    display: grid;
    gap: 4px;
    min-width: 0;
  }
  .steps {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px 6px;
  }
  .steps > li {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 6px;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .sep,
  .eye {
    color: ${(p) => p.theme.color.textMuted};
  }
  .why {
    color: ${(p) => p.theme.color.textMuted};
    overflow-wrap: anywhere;
  }
`;

// region Helpers
/** The column chip's rule: every row of the list is computed by view B's delegation rule. */
const CHIP = { by: "ui", rules: "kgateway-2.4", rule: "delegation", state: "checked" } as const;
const OUTCOMES: DelegationOutcome[] = [
  "attached",
  "partial",
  "dropped",
  "forbidden",
  "rejected-hostnames",
  "cycle",
  "unresolved",
  "unknown",
];
/** Each outcome's icon state; the text always sits beside it. */
const outcomeState: Record<DelegationOutcome, HealthState> = {
  attached: "healthy",
  partial: "warning",
  dropped: "error",
  forbidden: "error",
  "rejected-hostnames": "error",
  cycle: "error",
  unresolved: "warning",
  unknown: "unknown",
};
const outcomeOptions: FilterOption[] = OUTCOMES.map((o) => ({
  value: o,
  label: delegationCopy.outcomes[o],
  icon: <HealthIcon state={outcomeState[o]} size={14} notice />,
}));

/** One delegation ref of the route: rule index, backendRef index within the rule and the ref. */
interface RefAt {
  rule: number;
  ref: number;
  b: BackendRef & { delegation: DelegationRef };
}
function delegationRefs(rules: Rule[]): RefAt[] {
  return rules.flatMap((r) =>
    r.backendRefs.flatMap((b, i) =>
      b.delegation ? [{ rule: r.index, ref: i, b: { ...b, delegation: b.delegation } }] : [],
    ),
  );
}

function OutcomeLabel({ outcome }: { outcome: DelegationOutcome }) {
  return (
    <State data-testid="outcome">
      <span data-testid={`outcome-icon-${outcome}`} style={{ display: "inline-flex" }}>
        <HealthIcon state={outcomeState[outcome]} size={16} />
      </span>
      <span>{delegationCopy.outcomes[outcome]}</span>
    </State>
  );
}

function HiddenLabel() {
  return (
    <State data-testid="outcome">
      <NeutralBox aria-hidden="true">
        <Lock size={11} strokeWidth={3} />
      </NeutralBox>
      <span>{computedCopy.short.restricted}</span>
    </State>
  );
}

/** What a ref delegates to, as written. */
function Target({ at, installId }: { at: RefAt; installId: string }) {
  const d = at.b.delegation;
  if (d.mode === "name")
    return (
      <span className="what">
        {routeCopy.delegateRoute} <LinkedName refValue={at.b.ref} installId={installId} />
      </span>
    );
  return (
    <span className="what">
      {d.mode === "wildcard"
        ? routeCopy.delegateWildcard(d.namespace)
        : routeCopy.delegateLabel(d.namespace, d.label ?? "")}
    </span>
  );
}

/** The row's reason: the server's text, or for unknown the missing input. */
function reasonText(r: DelegationRow): string {
  // A restricted row carries no reason (design §7.4 rule 4); never show one even if sent.
  if (r.computed.state === "restricted") return "";
  if (r.reason) return r.reason;
  if (r.outcome === "unknown" && r.computed.reason) {
    const t = computedCopy.unknownReasons[r.computed.reason];
    if (!t) return computedCopy.unknownOther(r.computed.reason);
    // The outcome cell already says "Unknown", so the Why cell drops the badge's prefix (S233).
    const f = t.replace(/^Unknown: /, "");
    return f.charAt(0).toUpperCase() + f.slice(1);
  }
  return "";
}

/** The child's status entry under this parent: its Accepted condition, or the first one. */
function entryCondition(r: DelegationRow): Condition | undefined {
  const cs = r.observed.entry?.conditions ?? [];
  return cs.find((c) => c.type === "Accepted") ?? cs[0];
}

function Signal({ r }: { r: DelegationRow }) {
  const c = entryCondition(r);
  if (!r.observed.entry) return <Muted>{delegationCopy.noEntry}</Muted>;
  if (!c) return <Muted>{detailCopy.noConditions}</Muted>;
  const text = delegationCopy.entry(c.type, c.status);
  return (
    <Tooltip label={c.reason ? `${text} (${c.reason})` : text} wrap stylesOverride={nameTip}>
      <State tabIndex={0}>
        <HealthIcon state={conditionState(c)} size={16} />
        <span>{text}</span>
      </State>
    </Tooltip>
  );
}

function ContextItem({ c, installId }: { c: DelegationContext; installId: string }) {
  if (c.restricted)
    return (
      <li className="ctx" data-testid="delegation-context">
        <Placeholder>
          <Lock size={14} aria-hidden="true" />
          {computedCopy.restricted}
        </Placeholder>
      </li>
    );
  return (
    <li className="ctx" data-testid="delegation-context">
      <ol className="steps">
        {c.path?.map((s, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: a path may repeat a route (cycle)
          <li key={i} data-testid="path-step">
            {i > 0 ? (
              <span className="sep" aria-hidden="true">
                ›
              </span>
            ) : null}
            {s.ref.link === "none" ? (
              // A hidden ancestor: its name and the match it delegated under, nothing else (D-024).
              <EyeOff size={14} className="eye" role="img" aria-label={delegationCopy.hiddenRoute} />
            ) : null}
            <LinkedName refValue={s.ref} installId={installId} />
            <MatchLine m={s.match} />
          </li>
        ))}
      </ol>
      {c.outcome ? (
        <span className="steps">
          <OutcomeLabel outcome={c.outcome} />
          {c.reason ? <span className="why">{c.reason}</span> : null}
        </span>
      ) : null}
    </li>
  );
}

function RowItem({ r, installId }: { r: DelegationRow; installId: string }) {
  const [open, setOpen] = useState(false);
  const panelId = useId();
  const restricted = r.computed.state === "restricted";
  const reason = reasonText(r);
  const note = restricted ? undefined : r.computed.note;
  const expandable = !restricted || r.contexts.length > 0;
  return (
    <tbody className="item" role="rowgroup" data-testid="delegation-row">
      <RowLine role="row">
        <td className="toggle" role="cell">
          {expandable ? (
            <RowToggle
              type="button"
              aria-expanded={open}
              aria-controls={open ? panelId : undefined}
              aria-label={delegationCopy.togglePaths(open, r.contexts.length)}
              onClick={() => setOpen((o) => !o)}
            >
              {open ? <ChevronDown size={14} aria-hidden="true" /> : <ChevronRight size={14} aria-hidden="true" />}
            </RowToggle>
          ) : null}
        </td>
        <td className="child" role="cell">
          {r.child ? (
            <Tooltip label={`${r.child.namespace}/${r.child.name}`} stylesOverride={nameTip}>
              <LinkedName refValue={r.child} installId={installId} truncate />
            </Tooltip>
          ) : restricted ? (
            <Placeholder>
              <Lock size={14} aria-hidden="true" />
              {delegationCopy.notShown}
            </Placeholder>
          ) : (
            <Muted>{delegationCopy.noChild}</Muted>
          )}
        </td>
        <td className="outcome" role="cell">
          {r.outcome ? <OutcomeLabel outcome={r.outcome} /> : <HiddenLabel />}
        </td>
        <td className="reason" role="cell">
          {reason ? <OneLine text={reason} /> : null}
          {note ? (
            <Note data-testid="row-note">
              <Info size={12} aria-hidden="true" />
              {note}
            </Note>
          ) : null}
        </td>
        <td className="signal" role="cell">
          {restricted ? null : <Signal r={r} />}
        </td>
        <td className="badge" role="cell">
          <ComputedBadge computed={r.computed} compact chip={false} />
        </td>
      </RowLine>
      {open ? (
        <tr role="row">
          <td colSpan={6} role="cell">
            <Panel id={panelId}>
              {r.contexts.length ? (
                <ol aria-label={delegationCopy.pathList(r.contexts.length)}>
                  {r.contexts.map((c, i) => (
                    // biome-ignore lint/suspicious/noArrayIndexKey: contexts keep the server's order
                    <ContextItem key={i} c={c} installId={installId} />
                  ))}
                </ol>
              ) : null}
              {restricted ? null : <CutNote kept={r.contexts.length} truncated={r.contextsTruncated} />}
              {restricted ? null : (
                <Facts>
                  <dt>{delegationCopy.childStatus}</dt>
                  <dd>
                    {r.observed.entry ? (
                      <ConditionList
                        conditions={r.observed.entry.conditions}
                        generation={entryCondition(r)?.observedGeneration ?? 0}
                      />
                    ) : (
                      <Muted>{delegationCopy.noEntry}</Muted>
                    )}
                  </dd>
                  <dt>{delegationCopy.parentCondition}</dt>
                  <dd>
                    {r.observed.parentCondition ? (
                      <ConditionList
                        conditions={[r.observed.parentCondition]}
                        generation={r.observed.parentCondition.observedGeneration}
                      />
                    ) : (
                      <Muted>{delegationCopy.noCondition}</Muted>
                    )}
                  </dd>
                </Facts>
              )}
            </Panel>
          </td>
        </tr>
      ) : null}
    </tbody>
  );
}

const rowKey = (r: DelegationRow, i: number) =>
  r.child ? `${r.child.group}/${r.child.kind}/${r.child.namespace}/${r.child.name}` : `none-${i}`;

/** A ref with no rows: a ref the edge cap cut reads as its unknown reason, not empty (rules §5.7). */
function emptyText(at: RefAt, filtered: boolean): string {
  if (filtered) return delegationCopy.noRowsFiltered;
  const c = at.b.delegation.computed;
  if (c?.state === "restricted") return delegationCopy.notShown;
  if (c?.state === "unknown" && c.reason)
    return computedCopy.unknownReasons[c.reason] ?? computedCopy.unknownOther(c.reason);
  return delegationCopy.noRows;
}

/** One ref's rows, one paged list; its cursor is local, and a rejected one reloads page one (design §8.6). */
function RefRows({
  installId,
  route,
  at,
  outcomes,
  id,
  onStale,
}: {
  installId: string;
  route: LinkedRef;
  at: RefAt;
  outcomes: string[];
  id: string;
  /** Tells the section whether this poll failed or went stale, so its summary badge follows it too. */
  onStale: (stale: boolean) => void;
}) {
  const base = useMemo(() => {
    const q = new URLSearchParams({ rule: String(at.rule), ref: String(at.ref) });
    for (const o of outcomes) q.append("outcome", o);
    return q.toString();
  }, [at.rule, at.ref, outcomes]);
  const [cursor, setCursor] = useState<{ key: string; c: string }>();
  const c = cursor?.key === base ? cursor.c : undefined;
  const q = useMemo(() => {
    const out = new URLSearchParams(base);
    if (c) out.set("cursor", c);
    return out;
  }, [base, c]);
  const l = useDelegation(installId, route, q);
  useEffect(() => {
    if (l.cursorRejected) setCursor(undefined);
  }, [l.cursorRejected]);
  const expected = expectedRows(Math.max(1, at.b.delegation.selected ?? 1));
  const now = useNow();
  // The rows follow their own poll, not the route detail: a failed or stale poll turns their states unknown (§8.6).
  const old = !!l.data && !l.error && isStale(l.data.collectedAt, now);
  // A stale route detail above keeps the rows stale too.
  const own = !!l.data && (!!l.error || old);
  const stale = useContext(StaleView) || own;
  // Only this poll's own state goes up, before paint, so the summary never shows agreement over a failed poll.
  useLayoutEffect(() => onStale(own), [onStale, own]);
  useLayoutEffect(() => () => onStale(false), [onStale]);
  let body: ReactNode;
  if (!l.data && l.error) body = null;
  else if (!l.data)
    body = (
      <div aria-busy="true" style={{ display: "grid", gap: 1 }}>
        {Array.from({ length: expected }, (_, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: placeholders
          <Shimmer key={i} h={ROW_H} />
        ))}
      </div>
    );
  else if (!l.data.items.length) body = <Muted>{emptyText(at, outcomes.length > 0)}</Muted>;
  else
    body = (
      <Rows
        role="table"
        aria-label={delegationCopy.refTitle(at.rule + 1, at.ref + 1)}
        aria-busy={l.isLaggy || undefined}
      >
        <thead role="rowgroup">
          <tr className="cols" role="row" data-testid="delegation-cols">
            <th scope="col" role="columnheader">
              {/* The expanders' column needs a name for screen readers, not a visible label. */}
              <span className="sr">{delegationCopy.pathsColumn}</span>
            </th>
            <th scope="col" role="columnheader">
              {columnCopy.route}
            </th>
            <th scope="col" role="columnheader">
              {delegationCopy.outcomeGroup}
            </th>
            <th scope="col" role="columnheader">
              {columnCopy.why}
            </th>
            <th scope="col" role="columnheader">
              {delegationCopy.childStatus}
            </th>
            <th scope="col" role="columnheader">
              <ComputedChip label={computedTooltip(l.data.items.find((r) => r.computed.rule)?.computed ?? CHIP)} />
            </th>
          </tr>
        </thead>
        {/* One tbody per row: its line and, when open, its paths. */}
        {l.data.items.map((r, i) => (
          <RowItem key={rowKey(r, i)} r={r} installId={installId} />
        ))}
      </Rows>
    );
  return (
    <StaleView.Provider value={stale}>
      <div id={id}>
        {l.error ? <PageNotice error={l.error} title={listCopy.cannotLoad(delegationCopy.tab)} /> : null}
        {old && l.data ? <DataAge collectedAt={l.data.collectedAt} /> : null}
        {body}
        {l.data?.next || l.data?.prev ? (
          <div className="footer">
            <Pager prev={l.data.prev} next={l.data.next} onPage={(next) => setCursor({ key: base, c: next })} />
          </div>
        ) : null}
      </div>
    </StaleView.Provider>
  );
}

function RefSection({
  installId,
  route,
  at,
  outcomes,
}: {
  installId: string;
  route: LinkedRef;
  at: RefAt;
  outcomes: string[];
}) {
  const [open, setOpen] = useState(false);
  // The whole section follows the ref's own /delegation poll while it is open (§8.6).
  const [rowsStale, setRowsStale] = useState(false);
  const stale = useContext(StaleView) || rowsStale;
  const bodyId = useId();
  const d = at.b.delegation;
  const title = delegationCopy.refTitle(at.rule + 1, at.ref + 1);
  return (
    <StaleView.Provider value={stale}>
      <RefBox aria-label={title} data-testid="delegation-ref">
        <h3>
          {title}
          <Target at={at} installId={installId} />
        </h3>
        <div className="summary">
          <Toggle
            type="button"
            aria-expanded={open}
            aria-controls={open ? bodyId : undefined}
            onClick={() => setOpen((o) => !o)}
          >
            {open ? <ChevronDown size={14} aria-hidden="true" /> : <ChevronRight size={14} aria-hidden="true" />}
            {delegationCopy.toggleRows(open)}
          </Toggle>
          <RefCounts d={d} />
          {d.computed ? <ComputedBadge computed={d.computed} compact extra={delegationCopy.selectedHint} /> : null}
        </div>
        {open ? (
          <RefRows installId={installId} route={route} at={at} outcomes={outcomes} id={bodyId} onStale={setRowsStale} />
        ) : null}
      </RefBox>
    </StaleView.Provider>
  );
}

// region Component
interface DelegationTabProps {
  installId: string;
  route: LinkedRef;
  rules: Rule[];
  stylesOverride?: SerializedStyles;
}

/**
 * View B for a parent route: per delegation ref, the computed rows with outcome, contexts and the controller's
 * signal (design §5.3). Filters are page-local; only ?tab=delegation is in the URL (design §8.1).
 */
export function DelegationTab({ installId, route, rules, stylesOverride }: DelegationTabProps) {
  const refs = useMemo(() => delegationRefs(rules), [rules]);
  const [rule, setRule] = useState("");
  const [ref, setRef] = useState("");
  const [outcomes, setOutcomes] = useState<string[]>([]);
  const ruleIds = [...new Set(refs.map((r) => r.rule))];
  const inRule = refs.filter((r) => rule === "" || r.rule === Number(rule));
  const refIds = [...new Set(inRule.map((r) => r.ref))].sort((a, b) => a - b);
  const shown = inRule.filter((r) => ref === "" || r.ref === Number(ref));
  // The tab exists only for a route with delegation refs.
  if (!refs.length) return null;
  return (
    <Wrap stylesOverride={stylesOverride}>
      <Bar>
        <Label>
          {delegationCopy.ruleFilter}
          <Select
            value={rule}
            onChange={(e) => {
              setRule(e.target.value);
              setRef("");
            }}
          >
            <option value="">{delegationCopy.allRules}</option>
            {ruleIds.map((i) => (
              <option key={i} value={String(i)}>
                {routeCopy.rule(i + 1)}
              </option>
            ))}
          </Select>
        </Label>
        <Label>
          {delegationCopy.refFilter}
          <Select value={ref} onChange={(e) => setRef(e.target.value)}>
            <option value="">{delegationCopy.allRefs}</option>
            {refIds.map((i) => (
              <option key={i} value={String(i)}>
                {`${delegationCopy.refFilter} ${i + 1}`}
              </option>
            ))}
          </Select>
        </Label>
        <StateFilter
          label={delegationCopy.outcomeGroup}
          options={outcomeOptions}
          selected={outcomes}
          onChange={setOutcomes}
        />
      </Bar>
      {shown.map((at) => (
        <RefSection key={`${at.rule}/${at.ref}`} installId={installId} route={route} at={at} outcomes={outcomes} />
      ))}
    </Wrap>
  );
}
