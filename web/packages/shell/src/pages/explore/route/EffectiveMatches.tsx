import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Lock, Minus } from "lucide-react";
import type { EffectiveContext, EffectiveEdge } from "../../../api/types";
import { computedCopy, delegationCopy, effectiveCopy, routeCopy } from "../../../copy/explorer";
import { ComputedBadge } from "../../../explore/ComputedBadge";
import { LinkedName } from "../../../explore/LinkedName";
import { HealthIcon } from "../../../health/HealthIcon";
import { Section } from "../detailParts";
import { Muted, OneLine } from "../listParts";
import { MatchLine } from "./MatchLine";

// region Styles
const Cards = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 360px), 1fr));
  gap: ${(p) => p.theme.space(3)};
  ${(p) => p.stylesOverride}
`;
export const Card = styled.article`
  display: grid;
  align-content: start;
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
    gap: 4px ${(p) => p.theme.space(2)};
    margin: 0;
    font-size: ${(p) => p.theme.font.size.base};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    overflow-wrap: anywhere;
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
    gap: 6px;
    min-width: 0;
    padding-top: ${(p) => p.theme.space(2)};
    border-top: 1px solid ${(p) => p.theme.color.border};
  }
  .label {
    color: ${(p) => p.theme.color.textMuted};
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 2px ${(p) => p.theme.space(2)};
    min-width: 0;
  }
  .rule {
    display: grid;
    grid-template-columns: max-content max-content minmax(0, 1fr);
    align-items: center;
    gap: ${(p) => p.theme.space(2)};
    min-width: 0;
  }
  .state {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .matches {
    display: grid;
    gap: 2px;
    min-width: 0;
  }
`;
/** An outlined neutral icon: a dropped rule is a normal outcome, not an error. */
export const NeutralBox = styled.span`
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: 16px;
  height: 16px;
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.sm};
  color: ${(p) => p.theme.color.textMuted};
`;
export const Placeholder = styled.span`
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: ${(p) => p.theme.color.textMuted};
`;

// region Helpers
/** A context's rules: kept with their effective matches, or dropped with the failing clause. */
function ContextRules({ c }: { c: EffectiveContext }) {
  if (c.restricted || !c.parentMatch)
    return (
      <Placeholder>
        <Lock size={14} aria-hidden="true" />
        {computedCopy.restricted}
      </Placeholder>
    );
  return (
    <>
      <span className="head">
        <span className="label">{delegationCopy.parentMatch}</span>
        <MatchLine m={c.parentMatch} />
      </span>
      <ol>
        {c.rules?.map((r) => (
          <li key={r.index} className="rule" data-testid={`effective-rule-${r.index}`}>
            <span>{routeCopy.rule(r.index + 1)}</span>
            <span className="state">
              {r.kept ? (
                <HealthIcon state="healthy" size={14} />
              ) : (
                <NeutralBox aria-hidden="true">
                  <Minus size={11} strokeWidth={3} />
                </NeutralBox>
              )}
              {r.kept ? effectiveCopy.kept : effectiveCopy.dropped}
            </span>
            {r.kept ? (
              <span className="matches">
                {r.matches.map((m, i) => (
                  // biome-ignore lint/suspicious/noArrayIndexKey: matches keep written order
                  <MatchLine key={i} m={m} />
                ))}
              </span>
            ) : r.reason ? (
              <OneLine text={r.reason} />
            ) : (
              <span />
            )}
          </li>
        ))}
      </ol>
    </>
  );
}

/** Says a cut edge is incomplete; an edge with no kept context is never shown as if complete (design §7.3). */
export function CutNote({ kept, truncated }: { kept: number; truncated?: boolean }) {
  if (kept && !truncated) return null;
  return (
    <Placeholder data-testid="cut-note">
      <HealthIcon state="unknown" size={14} notice />
      {kept ? delegationCopy.morePaths : delegationCopy.noPathsKept}
    </Placeholder>
  );
}

// region Component
interface EffectiveMatchesProps {
  installId: string;
  edges: EffectiveEdge[];
  truncated?: boolean;
  stylesOverride?: SerializedStyles;
}

/** View C: this route's rules under each parent route's match, one card per parent edge (design §8.5). */
export function EffectiveMatches({ installId, edges, truncated, stylesOverride }: EffectiveMatchesProps) {
  return (
    <Section aria-labelledby="route-effective">
      <h2 id="route-effective">
        {effectiveCopy.title} <span className="note">{edges.length}</span>
      </h2>
      <Cards stylesOverride={stylesOverride}>
        {edges.map((e) => (
          <Card
            key={`${e.parent.namespace}/${e.parent.name}`}
            data-testid="effective-edge"
            aria-label={`${effectiveCopy.from} ${e.parent.namespace}/${e.parent.name}`}
          >
            <h3>
              <Muted>{effectiveCopy.from}</Muted>
              <LinkedName refValue={e.parent} installId={installId} />
            </h3>
            <Muted>{effectiveCopy.modes[e.mode]}</Muted>
            <ComputedBadge computed={e.computed} />
            {e.computed.state === "restricted" ? null : (
              <>
                {e.contexts.map((c, i) => (
                  // biome-ignore lint/suspicious/noArrayIndexKey: contexts keep the server's order
                  <div key={i} className="ctx" data-testid="effective-context">
                    <ContextRules c={c} />
                  </div>
                ))}
                <CutNote kept={e.contexts.length} truncated={e.contextsTruncated} />
              </>
            )}
          </Card>
        ))}
      </Cards>
      {truncated ? <Muted>{effectiveCopy.moreParents}</Muted> : null}
    </Section>
  );
}
