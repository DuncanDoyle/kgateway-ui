import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { dump } from "js-yaml";
import { useContext } from "react";
import type { BackendRef, Condition, DelegationRef, Filter, Match, Rule } from "../../../api/types";
import { routeCopy } from "../../../copy/explorer";
import { ConditionList } from "../../../explore/ConditionList";
import { LinkedName } from "../../../explore/LinkedName";
import { HealthBadge } from "../../../health/HealthBadge";
import { HealthIcon } from "../../../health/HealthIcon";
import { StaleView } from "../../../health/staleView";
import { stateLabel } from "../../../health/state";
import { YamlBlock } from "../../../resource/ResourceYaml";
import { installPath } from "../../../shell/paths";
import { InlineLink } from "../../../states/controls";
import type { HealthState } from "../../../theme/tokens";
import { backendParam } from "./refs";

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
  .match {
    display: flex;
    flex-wrap: wrap;
    gap: 2px ${(p) => p.theme.space(3)};
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
`;
const Config = styled(YamlBlock)`
  max-height: 200px;
  padding: ${(p) => p.theme.space(2)};
  line-height: 1.4;
`;

// region Helpers

function MatchLine({ m }: { m: Match }) {
  return (
    <span className="match code">
      {m.path ? (
        <span>
          {m.path.type} {m.path.value}
        </span>
      ) : null}
      {m.headers?.map((h) => (
        <span key={`h-${h.name}`}>
          {routeCopy.header} {h.name} {h.type} {h.value}
        </span>
      ))}
      {m.queryParams?.map((q) => (
        <span key={`q-${q.name}`}>
          {routeCopy.query} {q.name} {q.type} {q.value}
        </span>
      ))}
      {m.method ? (
        <span>
          {routeCopy.method} {m.method}
        </span>
      ) : null}
    </span>
  );
}

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

function BackendItem({ b, installId }: { b: BackendRef; installId: string }) {
  const lookup = `${installPath(installId, "routes")}?${new URLSearchParams({ backend: backendParam(b.ref) })}`;
  return (
    <li className="backend" data-testid={`backend-${b.ref.name}`}>
      {b.delegation ? (
        <Delegation b={{ ...b, delegation: b.delegation }} installId={installId} />
      ) : (
        <>
          <span className="line">
            <span>
              <span className="muted">{b.ref.kind}</span> <LinkedName refValue={b.ref} installId={installId} />
            </span>
            {b.port ? (
              <span>
                {routeCopy.port} {b.port}
              </span>
            ) : null}
            <span data-testid="weight">{routeCopy.weight(b.weight, b.weightDefaulted)}</span>
            {b.backend?.restricted ? (
              <span className="state">
                <HealthIcon state="unknown" size={14} />
                {routeCopy.restricted}
              </span>
            ) : b.backend?.state ? (
              <HealthBadge state={b.backend.state} />
            ) : null}
            {/* Delegation refs never match the backend filter (design §7.2), so only real backends get it. */}
            <InlineLink to={lookup}>{routeCopy.otherRoutes}</InlineLink>
          </span>
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
  rules: Rule[];
  stylesOverride?: SerializedStyles;
}

/** Each spec rule as written: matches, filters with their config, and backendRefs or delegation refs. */
export function RuleList({ installId, rules, stylesOverride }: RuleListProps) {
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
            <div>
              <h4>{routeCopy.matches}</h4>
              {/* Omitted matches arrive with the CRD default; only an explicit [] is empty (computed-rules §3.2). */}
              {r.matches.length ? (
                <ul>
                  {r.matches.map((m, i) => (
                    // biome-ignore lint/suspicious/noArrayIndexKey: positional
                    <li key={i} className="line" data-testid="match">
                      <MatchLine m={m} />
                    </li>
                  ))}
                </ul>
              ) : (
                <span className="muted" data-testid="match-empty">
                  {routeCopy.emptyMatches}
                </span>
              )}
            </div>
            {r.filters.length ? (
              <div>
                <h4>{routeCopy.filters}</h4>
                <FilterList filters={r.filters} installId={installId} />
              </div>
            ) : null}
            <div>
              <h4>{routeCopy.backendRefs}</h4>
              {r.backendRefs.length ? (
                <ul>
                  {r.backendRefs.map((b, i) => (
                    // biome-ignore lint/suspicious/noArrayIndexKey: the same backend may repeat in a rule
                    <BackendItem key={i} b={b} installId={installId} />
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
