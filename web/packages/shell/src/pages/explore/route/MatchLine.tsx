import styled from "@emotion/styled";
import type { Match } from "../../../api/types";
import { routeCopy } from "../../../copy/explorer";

// region Styles
const Line = styled.span`
  display: inline-flex;
  flex-wrap: wrap;
  gap: 2px ${(p) => p.theme.space(3)};
  min-width: 0;
  font-family: ${(p) => p.theme.font.mono};
  font-size: ${(p) => p.theme.font.size.code};
  overflow-wrap: anywhere;
`;

// region Helpers
/** One match as its written parts: path, headers, query parameters and method. */
function parts(m: Match): string[] {
  return [
    ...(m.path ? [`${m.path.type} ${m.path.value}`] : []),
    ...(m.headers ?? []).map((h) => `${routeCopy.header} ${h.name} ${h.type} ${h.value}`),
    ...(m.queryParams ?? []).map((q) => `${routeCopy.query} ${q.name} ${q.type} ${q.value}`),
    ...(m.method ? [`${routeCopy.method} ${m.method}`] : []),
  ];
}

/** A match as one line of text, e.g. for a tooltip. */
export const matchText = (m: Match) => parts(m).join(" ");

// region Component
/** A route match as written, its parts side by side. */
export function MatchLine({ m, className }: { m: Match; className?: string }) {
  return (
    <Line className={className}>
      {parts(m).map((p, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: parts keep written order and may repeat
        <span key={i}>{p}</span>
      ))}
    </Line>
  );
}
