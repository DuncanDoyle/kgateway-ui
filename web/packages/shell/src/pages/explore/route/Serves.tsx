import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import type { TraceAttachment } from "../../../api/types";
import { traceCopy } from "../../../copy/explorer";
import { cellCopy } from "../../../copy/traffic";
import { LinkedName } from "../../../explore/LinkedName";
import { HealthBadge } from "../../../health/HealthBadge";
import { Muted, refText } from "../listParts";

// region Styles
const List = styled.ul<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  gap: 6px;
  margin: 0;
  padding: 0;
  list-style: none;
  font-size: ${(p) => p.theme.font.size.sm};
  li {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 2px ${(p) => p.theme.space(3)};
    min-width: 0;
    padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
    border: 1px solid ${(p) => p.theme.color.border};
    border-radius: ${(p) => p.theme.radius.md};
    background: ${(p) => p.theme.color.surface};
    overflow-wrap: anywhere;
  }
  .state {
    flex: none;
    min-width: 92px;
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
  }
  ${(p) => p.stylesOverride}
`;

// region Helpers
/** The listener's text, e.g. "Listener http :80". */
export const listenerText = (a: TraceAttachment) => traceCopy.listener(a.listener.name, a.listener.port);

/** One attachment in words, for the signals it gave a backend line. */
export const attachmentText = (a: TraceAttachment) => traceCopy.onAttachment(refText(a.gateway), a.listener.name);

const key = (a: TraceAttachment) =>
  [refText(a.gateway), a.listenerSet ? refText(a.listenerSet) : "", a.listener.name, a.hostnames.join(",")].join("|");

// region Component
interface ServesProps {
  installId: string;
  serves: TraceAttachment[];
  /** False for a TCPRoute, which matches no hostnames. */
  hostnames?: boolean;
  stylesOverride?: SerializedStyles;
}

/** The Gateways, listeners and effective hostnames a route serves through, each with its own state (design §7.1). */
export function Serves({ installId, serves, hostnames = true, stylesOverride }: ServesProps) {
  if (!serves.length) return <Muted>{traceCopy.servesNone}</Muted>;
  return (
    <List stylesOverride={stylesOverride}>
      {serves.map((a) => (
        <li key={key(a)} data-testid="serves">
          <span className="state">
            <HealthBadge state={a.state} />
          </span>
          <span>
            <span className="muted">{a.gateway.kind}</span> <LinkedName refValue={a.gateway} installId={installId} />
          </span>
          {a.listenerSet ? <span className="muted">{cellCopy.viaListenerSet(refText(a.listenerSet))}</span> : null}
          <span>
            {listenerText(a)}
            {a.listener.protocol ? <span className="muted"> {a.listener.protocol}</span> : null}
          </span>
          {hostnames ? (
            <span data-testid="serves-hostnames">
              {a.hostnames.length ? a.hostnames.join(", ") : <span className="muted">{cellCopy.noHostnames}</span>}
            </span>
          ) : null}
        </li>
      ))}
    </List>
  );
}
