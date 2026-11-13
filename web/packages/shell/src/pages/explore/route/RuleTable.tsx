import { css, type SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { ChevronRight, Search } from "lucide-react";
import { useContext, useId, useMemo } from "react";
import type { BackendRef, Match, Rule, TraceAttachment } from "../../../api/types";
import { listCopy, routeCopy } from "../../../copy/explorer";
import { StateFilter } from "../../../explore/StateFilter";
import { HealthBadge } from "../../../health/HealthBadge";
import { Tooltip } from "../../../shell/Tooltip";
import type { ParamChanges } from "../../../shell/urlParams";
import { SearchBox, TextButton } from "../../../states/controls";
import type { HealthState } from "../../../theme/tokens";
import { tcpKind } from "../../traffic/pathText";
import { DetailParams } from "../detailParts";
import { Muted, refText } from "../listParts";
import { grpcParts } from "./MatchLine";
import { linkedRule, RuleBody, RuleBox, ruleAnchor, ruleTitle, useScrollToRule } from "./RuleList";

// region Styles
/** Height of a closed rule row; fixed, so rows never move as they render. */
export const RULE_ROW_H = 40;

const Bar = styled.div`
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: ${(p) => p.theme.space(2)};
  margin-bottom: ${(p) => p.theme.space(3)};
  .count {
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
  }
  .end {
    margin-left: auto;
  }
`;
const Rows = styled.ul<{ $tcp: boolean; stylesOverride?: SerializedStyles }>`
  display: grid;
  gap: ${(p) => p.theme.space(1)};
  margin: 0;
  padding: 0;
  list-style: none;
  > li {
    min-width: 0;
    border: 1px solid ${(p) => p.theme.color.border};
    border-radius: ${(p) => p.theme.radius.md};
    background: ${(p) => p.theme.color.surface};
  }
  .row {
    display: grid;
    grid-template-columns: ${(p) =>
      p.$tcp
        ? "16px minmax(4rem, 12rem) minmax(0, 6rem) minmax(0, 1fr) minmax(0, 7rem)"
        : "16px minmax(4rem, 12rem) minmax(0, 3fr) minmax(0, 6rem) minmax(0, 2fr) minmax(0, 7rem)"};
    align-items: center;
    gap: ${(p) => p.theme.space(3)};
    box-sizing: border-box;
    width: 100%;
    height: ${RULE_ROW_H}px;
    padding: 0 ${(p) => p.theme.space(3)};
    cursor: pointer;
    font: inherit;
    font-size: ${(p) => p.theme.font.size.sm};
    text-align: left;
    color: ${(p) => p.theme.color.text};
    background: transparent;
    border: 0;
    border-radius: inherit;
  }
  .row:hover {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.surface});
  }
  .row:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
  .row:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: -2px;
  }
  .row[aria-expanded="true"] {
    border-radius: ${(p) => p.theme.radius.md} ${(p) => p.theme.radius.md} 0 0;
  }
  .row[aria-expanded="true"] .chevron {
    transform: rotate(90deg);
  }
  .chevron {
    color: ${(p) => p.theme.color.textMuted};
  }
  .cell {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .title {
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  .name {
    margin-left: ${(p) => p.theme.space(2)};
    font-family: ${(p) => p.theme.font.mono};
    font-weight: ${(p) => p.theme.font.weight.regular};
  }
  .code {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
  }
  ${(p) => p.stylesOverride}
`;
// The open rule hangs under its row inside the same box.
const Body = styled(RuleBox)`
  border: 0;
  border-top: 1px solid ${(p) => p.theme.color.border};
  border-radius: 0 0 ${(p) => p.theme.radius.md} ${(p) => p.theme.radius.md};
`;
// The tooltip's wrapper is the grid cell, so its text can shrink to an ellipsis.
const cellTip = css`
  display: flex;
  min-width: 0;
`;

// region Helpers

// Outside a detail page the table has no URL to keep its state in, and shows the defaults.
const NO_PARAMS: [URLSearchParams, (c: ParamChanges) => void] = [new URLSearchParams(), () => {}];

const RANK: Record<HealthState, number> = { healthy: 0, unknown: 1, warning: 2, error: 3 };

/** The worst problem state of the rule's backend lines, from the server's per-ref problem; none without problem data. */
export function ruleState(r: Rule): HealthState | undefined {
  let worst: HealthState | undefined;
  for (const b of r.backendRefs) {
    const s = b.problem?.state;
    if (s && (!worst || RANK[s] > RANK[worst])) worst = s;
  }
  return worst;
}

/** One match as a short line: method, path, then header and query counts; a GRPCRoute match as service/method. */
function matchSummary(m: Match, grpc: boolean): string {
  const head = grpc ? [grpcParts(m.grpcMethod)[0]] : [m.method, m.path ? `${m.path.type} ${m.path.value}` : undefined];
  const counts = [
    m.headers?.length ? routeCopy.moreHeaders(m.headers.length) : undefined,
    !grpc && m.queryParams?.length ? routeCopy.moreQuery(m.queryParams.length) : undefined,
  ];
  return [...head, ...counts].filter(Boolean).join(" ");
}

function matchesSummary(r: Rule, kind: string): string {
  const grpc = kind === "GRPCRoute";
  // As in the full view: an omitted GRPCRoute match list matches every method, an explicit HTTP [] is empty.
  if (!r.matches.length) return grpc ? matchSummary({}, true) : routeCopy.emptyMatches;
  return r.matches.map((m) => matchSummary(m, grpc)).join(", ");
}

function refSummary(b: BackendRef, weights: boolean): string {
  const d = b.delegation;
  if (d?.mode === "wildcard") return routeCopy.delegateWildcard(d.namespace);
  if (d?.mode === "label") return routeCopy.delegateLabel(d.namespace, d.label ?? "");
  if (d) return `${routeCopy.delegateRoute} ${refText(b.ref)}`;
  return weights ? `${b.ref.name} (${b.weight})` : b.ref.name;
}

/** The backends by name, with weights when there is more than one, or the delegation target. */
function backendsSummary(r: Rule): string {
  if (!r.backendRefs.length) return routeCopy.noBackendRefs;
  return r.backendRefs.map((b) => refSummary(b, r.backendRefs.length > 1)).join(", ");
}

/** The text the search box matches: paths, header and query names and values, methods, filter types and backends. */
function searchText(r: Rule): string {
  const out: (string | undefined)[] = [r.name];
  for (const m of r.matches) {
    out.push(m.path?.value, m.method, m.grpcMethod?.service, m.grpcMethod?.method);
    for (const h of [...(m.headers ?? []), ...(m.queryParams ?? [])]) out.push(h.name, h.value);
  }
  for (const f of r.filters) out.push(f.type);
  for (const b of r.backendRefs) {
    out.push(refText(b.ref), b.delegation?.label, ...b.filters.map((f) => f.type));
  }
  return out.filter(Boolean).join("\n").toLowerCase();
}

/** Open rule numbers from the URL: ?open= lists them ("none" for none), else the default; ?rule= adds one. */
function openRules(params: URLSearchParams, rules: Rule[], linked?: number): Set<number> {
  const written = params.get("open");
  const out = new Set(
    // Error and warning rules, and a route's only rule, start open; the rest start closed.
    written === null
      ? rules
          .filter((r) => rules.length === 1 || ["error", "warning"].includes(ruleState(r) ?? ""))
          .map((r) => r.index + 1)
      : written.split(",").map(Number).filter(Number.isInteger),
  );
  if (linked !== undefined) out.add(linked);
  return out;
}

/** The ?open= value for a set of rule numbers; ?rule= folds into it, so a closed linked rule stays closed. */
const openChange = (open: Set<number>): ParamChanges => ({
  open: open.size ? [...open].sort((a, b) => a - b).join(",") : "none",
  rule: null,
});

// region Component
interface RuleTableProps {
  installId: string;
  kind: string;
  rules: Rule[];
  serves?: TraceAttachment[];
  stylesOverride?: SerializedStyles;
}

/**
 * A route's rules as compact rows that open in place to the full rule (D-081). Problems only, the search text
 * and the open rules live in the URL: ?problems=1, ?q= and ?open=; ?rule=n opens rule n and scrolls to it.
 */
export function RuleTable({ installId, kind, rules, serves = [], stylesOverride }: RuleTableProps) {
  const tcp = tcpKind(kind);
  const id = useId();
  const [params, update] = useContext(DetailParams) ?? NO_PARAMS;
  const problems = params.get("problems") === "1";
  const q = params.get("q") ?? "";
  const linked = linkedRule(params, rules);
  useScrollToRule(linked);
  const open = openRules(params, rules, linked);
  const texts = useMemo(() => new Map(rules.map((r) => [r.index, searchText(r)])), [rules]);
  const needle = q.trim().toLowerCase();
  const shown = rules.filter((r) => {
    const s = ruleState(r);
    if (problems && (!s || s === "healthy")) return false;
    return !needle || (texts.get(r.index) ?? "").includes(needle);
  });
  const allOpen = shown.length > 0 && shown.every((r) => open.has(r.index + 1));
  const toggle = (n: number) => {
    const next = new Set(open);
    if (!next.delete(n)) next.add(n);
    update(openChange(next));
  };
  const toggleAll = () => {
    const next = new Set(open);
    for (const r of shown) {
      if (allOpen) next.delete(r.index + 1);
      else next.add(r.index + 1);
    }
    update(openChange(next));
  };
  return (
    <>
      <Bar>
        <StateFilter
          label={routeCopy.problemsOnly}
          options={[{ value: "problems", label: routeCopy.problemsOnly }]}
          selected={problems ? ["problems"] : []}
          onChange={(v) => update({ problems: v.length ? "1" : null })}
        />
        <SearchBox>
          <Search size={14} aria-hidden="true" />
          <input
            type="search"
            aria-label={routeCopy.searchRules}
            autoComplete="off"
            placeholder={routeCopy.searchRules}
            value={q}
            onChange={(e) => update({ q: e.target.value })}
          />
        </SearchBox>
        {problems || needle ? (
          <span className="count" data-testid="rule-count">
            {routeCopy.shownRules(shown.length, rules.length)}
          </span>
        ) : null}
        <TextButton type="button" className="end" onClick={toggleAll}>
          {allOpen ? routeCopy.collapseAll : routeCopy.expandAll}
        </TextButton>
      </Bar>
      {shown.length ? (
        <Rows $tcp={tcp} stylesOverride={stylesOverride}>
          {shown.map((r) => {
            const n = r.index + 1;
            const isOpen = open.has(n);
            const state = ruleState(r);
            const match = matchesSummary(r, kind);
            const backends = backendsSummary(r);
            return (
              <li key={r.index} id={ruleAnchor(n)} data-testid="rule-row">
                <button
                  type="button"
                  className="row"
                  aria-expanded={isOpen}
                  aria-controls={`${id}-${n}`}
                  onClick={() => toggle(n)}
                >
                  <ChevronRight className="chevron" size={16} aria-hidden="true" />
                  <span className="cell title">
                    {routeCopy.rule(n)}
                    {r.name ? <span className="name">{r.name}</span> : null}
                  </span>
                  {/* TCPRoute and TLSRoute rules match every connection, so they have no match column. */}
                  {tcp ? null : (
                    <Tooltip label={match} wrap stylesOverride={cellTip}>
                      <span className="cell code">{match}</span>
                    </Tooltip>
                  )}
                  <span className="cell muted">{routeCopy.filterCount(r.filters.length)}</span>
                  <Tooltip label={backends} wrap stylesOverride={cellTip}>
                    <span className="cell">{backends}</span>
                  </Tooltip>
                  <span className="cell">{state ? <HealthBadge state={state} /> : null}</span>
                </button>
                {isOpen ? (
                  <Body id={`${id}-${n}`} aria-label={ruleTitle(r)}>
                    <RuleBody installId={installId} kind={kind} rule={r} serves={serves} />
                  </Body>
                ) : null}
              </li>
            );
          })}
        </Rows>
      ) : (
        <Muted data-testid="rule-empty">{needle ? routeCopy.noRuleMatch : listCopy.emptyFiltered("rules")}</Muted>
      )}
    </>
  );
}
