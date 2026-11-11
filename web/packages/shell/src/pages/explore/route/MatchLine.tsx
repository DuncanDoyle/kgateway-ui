import styled from "@emotion/styled";
import type { GRPCMethod, KeyMatch, Match } from "../../../api/types";
import { routeCopy } from "../../../copy/explorer";
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
const headerText = (h: KeyMatch) => `${routeCopy.header} ${h.name} ${h.type} ${h.value}`;

/** A GRPCRoute match as its parts: service/method, or any method, then headers; Traffic shows the same text. */
export function grpcParts(method?: GRPCMethod, headers?: KeyMatch[]): string[] {
  const m = method?.service || method?.method ? `${method.service ?? "*"}/${method.method ?? "*"}` : cellCopy.anyMethod;
  return [m, ...(headers ?? []).map(headerText)];
}

/** One HTTPRoute match as its written parts: path, headers, query parameters and method. */
function parts(m: Match): string[] {
  return [
    ...(m.path ? [`${m.path.type} ${m.path.value}`] : []),
    ...(m.headers ?? []).map(headerText),
    ...(m.queryParams ?? []).map((q) => `${routeCopy.query} ${q.name} ${q.type} ${q.value}`),
    ...(m.method ? [`${routeCopy.method} ${m.method}`] : []),
  ];
}

/** A match as one line of text, e.g. for a tooltip. */
export const matchText = (m: Match) => parts(m).join(" ");

// region Component
/** A route match as written, its parts side by side; grpc renders a GRPCRoute match. */
export function MatchLine({ m, grpc, className }: { m: Match; grpc?: boolean; className?: string }) {
  return (
    <Line className={className}>
      {(grpc ? grpcParts(m.grpcMethod, m.headers) : parts(m)).map((p, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: parts keep written order and may repeat
        <span key={i}>{p}</span>
      ))}
    </Line>
  );
}
