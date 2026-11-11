import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import type { RefExplanation } from "../../api/types";
import { explanationCopy } from "../../copy/backends";
import { routeCopy } from "../../copy/explorer";
import { SuggestedFixView } from "../../diagnostics/SuggestedFix";
import { LinkedName } from "../../explore/LinkedName";
import { Section } from "../explore/detailParts";

// region Styles
const List = styled.ul<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  gap: ${(p) => p.theme.space(3)};
  margin: 0;
  padding: 0;
  list-style: none;
  ${(p) => p.stylesOverride}
`;
const Item = styled.li`
  padding: ${(p) => p.theme.space(3)};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.surface};
  font-size: ${(p) => p.theme.font.size.sm};
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(3)};
    margin-bottom: ${(p) => p.theme.space(2)};
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
  }
  .mono {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
  }
  p {
    margin: 0 0 ${(p) => p.theme.space(2)};
  }
  section {
    margin: ${(p) => p.theme.space(3)} 0 0;
  }
`;

// region Component
interface ExplanationsProps {
  installId: string;
  explanations: RefExplanation[];
  stylesOverride?: SerializedStyles;
}

/**
 * Why each route ref to this backend does not resolve (Phase 6 design §10). Attributed refs get the explanation and
 * the fix; candidates quote the route's reason without guessing which ref it means; stale status is waiting.
 */
export function Explanations({ installId, explanations, stylesOverride }: ExplanationsProps) {
  return (
    <Section aria-labelledby="backend-explanations">
      <h2 id="backend-explanations">{explanationCopy.title}</h2>
      <List stylesOverride={stylesOverride}>
        {explanations.map((e, i) => (
          <Item
            key={`${e.route.kind}/${e.route.namespace}/${e.route.name}/${e.path}`}
            data-testid="explanation"
            data-kind={e.kind}
          >
            <div className="head">
              <span>
                <span className="muted">{e.route.kind}</span> <LinkedName refValue={e.route} installId={installId} />
              </span>
              <span>{routeCopy.rule(e.rule + 1)}</span>
              <span className="muted">
                {explanationCopy.at} <span className="mono">{e.path}</span>
              </span>
            </div>
            {e.kind === "waiting" ? (
              <p>{explanationCopy.waiting}</p>
            ) : e.kind === "candidate" ? (
              <p>{explanationCopy.candidate(e.reason ?? "")}</p>
            ) : (
              <>
                {e.explanation ? <p>{e.explanation}</p> : null}
                {e.reason ? (
                  <p>
                    <span className="muted">{explanationCopy.reason}</span> <span className="mono">{e.reason}</span>
                  </p>
                ) : null}
                {e.fix ? <SuggestedFixView fix={e.fix} installId={installId} headingId={`backend-fix-${i}`} /> : null}
              </>
            )}
          </Item>
        ))}
      </List>
    </Section>
  );
}
