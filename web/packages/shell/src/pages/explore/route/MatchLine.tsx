import styled from "@emotion/styled";
import type { Match } from "../../../api/types";
import { routeCopy, traceCopy } from "../../../copy/explorer";
import { cellCopy } from "../../../copy/traffic";

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
/** One match as its written parts: path or gRPC method, headers, query parameters and HTTP method. */
function parts(m: Match): string[] {
  const g = m.grpcMethod;
  return [
    ...(m.path ? [`${m.path.type} ${m.path.value}`] : []),
    ...(g ? [`${traceCopy.grpcMethod} ${g.type ?? "Exact"} ${g.service ?? "*"}/${g.method ?? "*"}`] : []),
    ...(m.headers ?? []).map((h) => `${routeCopy.header} ${h.name} ${h.type} ${h.value}`),
    ...(m.queryParams ?? []).map((q) => `${routeCopy.query} ${q.name} ${q.type} ${q.value}`),
    ...(m.method ? [`${routeCopy.method} ${m.method}`] : []),
  ];
}

/** A match as one line of text, e.g. for a tooltip. */
export const matchText = (m: Match) => parts(m).join(" ");

// region Component
/** A route match as written, its parts side by side; a GRPCRoute match with no parts matches any method. */
export function MatchLine({ m, grpc, className }: { m: Match; grpc?: boolean; className?: string }) {
  const written = parts(m);
  return (
    <Line className={className}>
      {(written.length || !grpc ? written : [cellCopy.anyMethod]).map((p, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: parts keep written order and may repeat
        <span key={i}>{p}</span>
      ))}
    </Line>
  );
}
