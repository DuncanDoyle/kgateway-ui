import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { dump } from "js-yaml";
import { useContext } from "react";
import type { BackendRef, Condition, DelegationRef, Filter, Rule, TraceAttachment } from "../../../api/types";
import { delegationCopy, routeCopy, traceCopy } from "../../../copy/explorer";
import { ComputedBadge } from "../../../explore/ComputedBadge";
import { ConditionList } from "../../../explore/ConditionList";
import { LinkedName } from "../../../explore/LinkedName";
import { backendPath } from "../../../explore/objectPath";
import { HealthBadge } from "../../../health/HealthBadge";
import { HealthIcon } from "../../../health/HealthIcon";
import { StaleView } from "../../../health/staleView";
import { stateLabel } from "../../../health/state";
import { YamlBlock } from "../../../resource/ResourceYaml";
import { installPath } from "../../../shell/paths";
import { InlineLink } from "../../../states/controls";
import type { HealthState } from "../../../theme/tokens";
import { tcpKind } from "../../traffic/pathText";
import { MatchLine } from "./MatchLine";
import { RefFacts, RefWhy } from "./RefTrace";
import { backendParam } from "./refs";
import { ShareBar } from "./ShareBar";

// region Styles
const Rules = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  gap: ${(p) => p.theme.space(3)};
  ${(p) => p.stylesOverride}
`;
const RuleBox = styled.section`
  display: grid;
  gap: ${(p) => p.theme.space(3)};
  min-width: 0;
  padding: ${(p) => p.theme.space(3)};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.surface};
  font-size: ${(p) => p.theme.font.size.sm};
  h3 {
    margin: 0;
    font-size: ${(p) => p.theme.font.size.base};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    overflow-wrap: anywhere;
  }
  h3 .name {
    margin-left: ${(p) => p.theme.space(2)};
    font-family: ${(p) => p.theme.font.mono};
    font-weight: ${(p) => p.theme.font.weight.regular};
  }
  h4 {
    margin: 0 0 4px;
    font-size: ${(p) => p.theme.font.size.sm};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    color: ${(p) => p.theme.color.textMuted};
  }
  ul,
  ol {
    display: grid;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .code {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
    overflow-wrap: anywhere;
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
  }
  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 2px ${(p) => p.theme.space(3)};
    min-width: 0;
  }
  .backend {
    display: grid;
    gap: 6px;
    min-width: 0;
    padding-top: 6px;
    border-top: 1px solid ${(p) => p.theme.color.border};
  }
  .backend:first-of-type {
    padding-top: 0;
    border-top: 0;
  }
  .state {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .filter {
    display: grid;
    gap: 4px;
    min-width: 0;
  }
  .nested {
    padding-left: ${(p) => p.theme.space(3)};
  }
  /* The rule's filters as written, in a quiet marked area. */
  .filters {
    padding: ${(p) => p.theme.space(2)};
    border: 1px dashed ${(p) => p.theme.color.borderStrong};
    border-radius: ${(p) => p.theme.radius.sm};
  }
  .refs {
    display: grid;
    gap: ${(p) => p.theme.space(2)};
  }
`;
const Config = styled(YamlBlock)`
  max-height: 200px;
  padding: ${(p) => p.theme.space(2)};
  line-height: 1.4;
`;

// region Helpers

const pct = (f: Filter) => {
  const m = f.mirror;
  if (!m) return null;
  if (m.percent !== undefined) return `${m.percent}%`;
  if (m.fraction) return `${m.fraction.numerator}/${m.fraction.denominator}`;
  return null;
};

function FilterItem({ f, installId }: { f: Filter; installId: string }) {
  const share = pct(f);
  return (
    <li className="filter" data-testid="filter" data-type={f.type}>
      <span className="line">
        <span className="code">{f.type}</span>
        {f.mirror ? (
          <>
            <span>
              {routeCopy.mirrorTo} <LinkedName refValue={f.mirror.backendRef} installId={installId} />
            </span>
            {f.mirror.port ? (
              <span>
                {routeCopy.port} {f.mirror.port}
              </span>
            ) : null}
            {share ? <span>{share}</span> : null}
          </>
        ) : null}
        {f.extensionRef ? (
          <span>
            <span className="muted">{f.extensionRef.kind}</span>{" "}
            <LinkedName refValue={f.extensionRef} installId={installId} />
          </span>
        ) : null}
      </span>
      {f.config ? <Config>{dump(f.config, { noRefs: true, lineWidth: 140 })}</Config> : null}
    </li>
  );
}

function FilterList({ filters, installId }: { filters: Filter[]; installId: string }) {
  return (
    <ol>
      {filters.map((f, i) => (
        // Filters keep written order; a type may repeat.
        // biome-ignore lint/suspicious/noArrayIndexKey: positional
        <FilterItem key={i} f={f} installId={installId} />
      ))}
    </ol>
  );
}

const delegationState: Record<NonNullable<DelegationRef["state"]>, { label: string; state: HealthState }> = {
  reported: { label: routeCopy.delegation.reported, state: "healthy" },
  "not-reported": { label: routeCopy.delegation["not-reported"], state: "unknown" },
  "not-found": { label: routeCopy.delegation["not-found"], state: "error" },
  restricted: { label: routeCopy.delegation.restricted, state: "unknown" },
};

/** A ref's counts. While the ref is Hidden they cover visible routes only, and say so (S234). */
export function RefCounts({ d }: { d: DelegationRef }) {
  const visible = d.computed?.state === "restricted";
  return (
    <>
      {d.selected !== undefined ? (
        <span>{visible ? delegationCopy.visibleSelected(d.selected) : delegationCopy.selected(d.selected)}</span>
      ) : null}
      {d.attached !== undefined ? (
        <span>{visible ? delegationCopy.visibleAttached(d.attached) : delegationCopy.attached(d.attached)}</span>
      ) : null}
    </>
  );
}

function Delegation({ b, installId }: { b: BackendRef & { delegation: DelegationRef }; installId: string }) {
  const d = b.delegation;
  const stale = useContext(StaleView);
  const known = d.state ? delegationState[d.state] : undefined;
  // A stale page shows every state unknown, words included (design §6.3).
  const s = known && stale ? { label: stateLabel("unknown"), state: "unknown" as const } : known;
  return (
    <span className="line">
      <span className="muted">{routeCopy.delegatesTo}</span>
      {d.mode === "name" ? (
        <span>
          {routeCopy.delegateRoute} <LinkedName refValue={b.ref} installId={installId} />
        </span>
      ) : d.mode === "wildcard" ? (
        <span>{routeCopy.delegateWildcard(d.namespace)}</span>
      ) : (
        <span>{routeCopy.delegateLabel(d.namespace, d.label ?? "")}</span>
      )}
      {s ? (
        <span className="state">
          <HealthIcon state={s.state} size={14} />
          {s.label}
        </span>
      ) : null}
      {d.computed ? (
        // View B's counts of visible children (2.4); absent when the UI could not compute them.
        <span className="line" data-testid="ref-counts">
          <RefCounts d={d} />
          <ComputedBadge computed={d.computed} compact extra={delegationCopy.selectedHint} />
        </span>
      ) : null}
    </span>
  );
}

/**
 * The Backend's Accepted condition. The body has no Backend generation, so staleness is said in words. The icon
 * follows the Backend's state, so a stale or evidence-less condition is unknown; only a current True is "accepted".
 */
function BackendCondition({ c, state }: { c: Condition; state: HealthState }) {
  const noEvidence = c.observedGeneration === 0;
  const accepted = !noEvidence && !c.stale && c.status === "True";
  const note = c.stale ? routeCopy.backendStale : noEvidence ? routeCopy.backendNoEvidence : undefined;
  return (
    <ConditionList
      conditions={[{ ...c, stale: false }]}
      generation={c.observedGeneration}
      stateOf={() => state}
      headOf={accepted ? () => routeCopy.backendAccepted : undefined}
      extra={() => (note ? <span className="muted">{note}</span> : null)}
    />
  );
}

interface BackendItemProps {
  b: BackendRef;
  installId: string;
  serves: TraceAttachment[];
  tcp: boolean;
}

/** One backend line: the ref as written, then its share, endpoints, state and why (design §7.1). */
function BackendItem({ b, installId, serves, tcp }: BackendItemProps) {
  return (
    <li className="backend" data-testid={`backend-${b.ref.name}`}>
      {b.delegation ? (
        <Delegation b={{ ...b, delegation: b.delegation }} installId={installId} />
      ) : (
        <>
          <span className="line">
            <span>
              <span className="muted">{b.ref.kind}</span>{" "}
              {/* Backend detail, unless the caller may not see the backend: then its name stays text. */}
              <LinkedName
                refValue={b.ref}
                installId={installId}
                to={b.backend?.restricted ? undefined : backendPath(installId, b.ref)}
              />
            </span>
            {b.port ? (
              <span>
                {routeCopy.port} {b.port}
              </span>
            ) : null}
            <span data-testid="weight">{routeCopy.weight(b.weight, b.weightDefaulted)}</span>
            <RefFacts b={b} tcp={tcp} />
            {/* The path problem covers the Backend's own state; without one, the Backend's state shows. */}
            {b.backend?.restricted ? (
              <span className="state">
                <HealthIcon state="unknown" size={14} />
                {routeCopy.restricted}
              </span>
            ) : b.backend?.state && !b.problem ? (
              <HealthBadge state={b.backend.state} />
            ) : null}
            {/* With no backend detail to open, a restricted viewer can still find the other routes using it. */}
            {b.backend?.restricted ? (
              <InlineLink
                to={`${installPath(installId, "routes")}?${new URLSearchParams({ backend: backendParam(b.ref) })}`}
              >
                {routeCopy.otherRoutes}
              </InlineLink>
            ) : null}
          </span>
          <RefWhy b={b} serves={serves} />
          {b.backend?.accepted ? (
            <BackendCondition c={b.backend.accepted} state={b.backend.state ?? "unknown"} />
          ) : null}
        </>
      )}
      {b.filters.length ? (
        <div className="nested">
          <h4>{routeCopy.backendRefFilters}</h4>
          <FilterList filters={b.filters} installId={installId} />
        </div>
      ) : null}
    </li>
  );
}

// region Component
interface RuleListProps {
  installId: string;
  /** The route's kind: TCPRoute and TLSRoute rules have no matches or filters, and connections fail. */
  kind: string;
  rules: Rule[];
  /** The attachments the backend lines' signals index. */
  serves?: TraceAttachment[];
  stylesOverride?: SerializedStyles;
}

/**
 * The route trace (design §7.1): each spec rule's match, filters as written, share bar, and one line per backendRef
 * or delegation ref with its share, endpoints and problem.
 */
export function RuleList({ installId, kind, rules, serves = [], stylesOverride }: RuleListProps) {
  const tcp = tcpKind(kind);
  if (!rules.length) return <p>{routeCopy.noRules}</p>;
  return (
    <Rules stylesOverride={stylesOverride}>
      {rules.map((r) => {
        const title = `${routeCopy.rule(r.index + 1)}${r.name ? ` ${r.name}` : ""}`;
        return (
          <RuleBox key={r.index} aria-label={title}>
            <h3>
              {routeCopy.rule(r.index + 1)}
              {r.name ? <span className="name">{r.name}</span> : null}
            </h3>
            {/* TCPRoute and TLSRoute rules match every connection and have no filters. */}
            {tcp ? null : (
              <div>
                <h4>{routeCopy.matches}</h4>
                {/* Omitted matches arrive with the CRD default; only an explicit [] is empty (computed-rules §3.2). */}
                {/* A GRPCRoute's omitted or empty matches match every method. */}
                {r.matches.length || kind === "GRPCRoute" ? (
                  <ul>
                    {(r.matches.length ? r.matches : [{}]).map((m, i) => (
                      // biome-ignore lint/suspicious/noArrayIndexKey: positional
                      <li key={i} className="line" data-testid="match">
                        <MatchLine m={m} grpc={kind === "GRPCRoute"} />
                      </li>
                    ))}
                  </ul>
                ) : (
                  <span className="muted" data-testid="match-empty">
                    {routeCopy.emptyMatches}
                  </span>
                )}
              </div>
            )}
            {tcp ? null : (
              <div className="filters" data-testid="rule-filters">
                <h4>{routeCopy.filters}</h4>
                {r.filters.length ? (
                  <FilterList filters={r.filters} installId={installId} />
                ) : (
                  <span className="muted">{traceCopy.noFilters}</span>
                )}
              </div>
            )}
            <div className="refs">
              <h4>{routeCopy.backendRefs}</h4>
              <ShareBar refs={r.backendRefs} tcp={tcp} />
              {r.backendRefs.length ? (
                <ul>
                  {r.backendRefs.map((b, i) => (
                    // biome-ignore lint/suspicious/noArrayIndexKey: the same backend may repeat in a rule
                    <BackendItem key={i} b={b} installId={installId} serves={serves} tcp={tcp} />
                  ))}
                </ul>
              ) : (
                <span className="muted">{routeCopy.noBackendRefs}</span>
              )}
            </div>
          </RuleBox>
        );
      })}
    </Rules>
  );
}
