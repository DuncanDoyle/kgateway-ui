import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { dump } from "js-yaml";
import type { BackendRef, Condition, DelegationRef, Filter, Match, Rule } from "../../../api/types";
import { ConditionList } from "../../../explore/ConditionList";
import { LinkedName } from "../../../explore/LinkedName";
import { HealthBadge } from "../../../health/HealthBadge";
import { HealthIcon } from "../../../health/HealthIcon";
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
/** An explicit empty list is match-all; it shows the CRD default, never "matches nothing" (design §7.3). */
const DEFAULT_MATCH: Match = { path: { type: "PathPrefix", value: "/" } };

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
          Header {h.name} {h.type} {h.value}
        </span>
      ))}
      {m.queryParams?.map((q) => (
        <span key={`q-${q.name}`}>
          Query {q.name} {q.type} {q.value}
        </span>
      ))}
      {m.method ? <span>Method {m.method}</span> : null}
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
              Mirror to <LinkedName refValue={f.mirror.backendRef} installId={installId} />
            </span>
            {f.mirror.port ? <span>Port {f.mirror.port}</span> : null}
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
  reported: { label: "Reported by the controller", state: "healthy" },
  "not-reported": { label: "Not reported by the controller", state: "unknown" },
  "not-found": { label: "No such route", state: "error" },
  restricted: { label: "Restricted", state: "unknown" },
};

function Delegation({ b, installId }: { b: BackendRef & { delegation: DelegationRef }; installId: string }) {
  const d = b.delegation;
  const s = d.state ? delegationState[d.state] : undefined;
  return (
    <span className="line">
      <span className="muted">Delegates to</span>
      {d.mode === "name" ? (
        <span>
          Route <LinkedName refValue={b.ref} installId={installId} />
        </span>
      ) : d.mode === "wildcard" ? (
        <span>Every route in {d.namespace}</span>
      ) : (
        <span>
          Routes in {d.namespace} with label {d.label}
        </span>
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

/** The Backend's Accepted condition; the body has no Backend generation, so staleness is said in words. */
function BackendCondition({ c }: { c: Condition }) {
  return (
    <ConditionList
      conditions={[{ ...c, stale: false }]}
      generation={c.observedGeneration}
      extra={() => (c.stale ? <span className="muted">Describes an older generation of the Backend.</span> : null)}
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
            {b.port ? <span>Port {b.port}</span> : null}
            <span data-testid="weight">
              Weight {b.weight}
              {b.weightDefaulted ? " (default)" : ""}
            </span>
            {b.backend?.restricted ? (
              <span className="state">
                <HealthIcon state="unknown" size={14} />
                Restricted
              </span>
            ) : b.backend?.state ? (
              <HealthBadge state={b.backend.state} />
            ) : null}
            {/* Delegation refs never match the backend filter (design §7.2), so only real backends get it. */}
            <InlineLink to={lookup}>Other routes using this backend</InlineLink>
          </span>
          {b.backend?.accepted ? <BackendCondition c={b.backend.accepted} /> : null}
        </>
      )}
      {b.filters.length ? (
        <div className="nested">
          <h4>Filters on this backend</h4>
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
  if (!rules.length) return <p>No rules.</p>;
  return (
    <Rules stylesOverride={stylesOverride}>
      {rules.map((r) => {
        const title = `Rule ${r.index + 1}${r.name ? ` ${r.name}` : ""}`;
        const matches = r.matches.length ? r.matches : [DEFAULT_MATCH];
        return (
          <RuleBox key={r.index} aria-label={title}>
            <h3>
              Rule {r.index + 1}
              {r.name ? <span className="name">{r.name}</span> : null}
            </h3>
            <div>
              <h4>Matches</h4>
              <ul>
                {matches.map((m, i) => (
                  // biome-ignore lint/suspicious/noArrayIndexKey: positional
                  <li key={i} className="line" data-testid="match">
                    <MatchLine m={m} />
                    {r.matches.length ? null : <span className="muted">Default match</span>}
                  </li>
                ))}
              </ul>
            </div>
            {r.filters.length ? (
              <div>
                <h4>Filters</h4>
                <FilterList filters={r.filters} installId={installId} />
              </div>
            ) : null}
            <div>
              <h4>Backends</h4>
              {r.backendRefs.length ? (
                <ul>
                  {r.backendRefs.map((b, i) => (
                    // biome-ignore lint/suspicious/noArrayIndexKey: the same backend may repeat in a rule
                    <BackendItem key={i} b={b} installId={installId} />
                  ))}
                </ul>
              ) : (
                <span className="muted">No backends.</span>
              )}
            </div>
          </RuleBox>
        );
      })}
    </Rules>
  );
}
