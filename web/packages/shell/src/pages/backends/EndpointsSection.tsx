import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useNow } from "../../api/clock";
import type { Endpoints } from "../../api/types";
import { endpointsCopy, portChainCopy } from "../../copy/backends";
import { pathColumnCopy } from "../../copy/traffic";
import { QuietButton } from "../../states/controls";
import { Section } from "../explore/detailParts";
import { ago } from "../install/HealthStrip";
import { endpointsText } from "../traffic/pathText";

// region Styles
const Sources = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  gap: ${(p) => p.theme.space(3)};
  ${(p) => p.stylesOverride}
`;
const Source = styled.div`
  padding: ${(p) => p.theme.space(3)};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.surface};
  h3 {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(3)};
    margin: 0 0 ${(p) => p.theme.space(2)};
    font-size: ${(p) => p.theme.font.size.sm};
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
    font-weight: ${(p) => p.theme.font.weight.regular};
  }
  .summary {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(3)};
    min-height: 28px;
    font-size: ${(p) => p.theme.font.size.sm};
  }
  table {
    width: 100%;
    margin-top: ${(p) => p.theme.space(2)};
    border-collapse: collapse;
    font-size: ${(p) => p.theme.font.size.sm};
    font-variant-numeric: tabular-nums;
  }
  th {
    text-align: left;
    font-size: ${(p) => p.theme.font.size.xs};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: ${(p) => p.theme.color.textMuted};
  }
  th,
  td {
    padding: 4px 8px 4px 0;
    border-bottom: 1px solid ${(p) => p.theme.color.border};
    overflow-wrap: anywhere;
  }
  tr:last-child td {
    border-bottom: 0;
  }
  .mono {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
  }
`;
/** The slot #35's per-proxy view takes: quiet, and it promises nothing. */
const ProxySlot = styled.p`
  margin: 0;
  padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
  border: 1px dashed ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  font-size: ${(p) => p.theme.font.size.sm};
  color: ${(p) => p.theme.color.textMuted};
`;

// region Helpers
/** A source's summary: the read's count, or the cached count when the read established none, or why there is none. */
export function sourceSummary(e: Endpoints): { text: string; cached: boolean } {
  const counted = e.ready !== undefined && e.total !== undefined;
  if (e.complete === "complete" || e.complete === "not-applicable") return { text: endpointsText(e), cached: false };
  // Partial, unavailable, timed out or busy: a count here comes from the cache, never from this read.
  if (counted) {
    const cached = endpointsCopy.cached(e.ready ?? 0, e.total ?? 0);
    // Unavailable means no read at all: the cached counts are all there is.
    return { text: e.complete === "unavailable" ? cached : `${cached} · ${endpointsText(e)}`, cached: true };
  }
  return { text: endpointsText(e), cached: false };
}

const yesNo = (v: boolean) => (v ? endpointsCopy.yes : endpointsCopy.no);

// region Component
/** When the source was read; not-applicable reads nothing, so it has no time. Owns the clock tick. */
function ReadAge({ readAt }: { readAt: string }) {
  const now = useNow();
  return <span className="muted">{endpointsCopy.read(ago(readAt, now))}</span>;
}

interface EndpointsSectionProps {
  sources: Endpoints[];
  /** Referenced ports the Service lacks, from the port chain: there is nothing to read for them. */
  noSuchPorts?: readonly number[];
  /** Reads again, e.g. after "busy, retry". */
  onRetry: () => void;
  stylesOverride?: SerializedStyles;
}

/** Endpoints per referenced port, by source (Phase 6 design §9); "0" only where a complete read established it. */
export function EndpointsSection({ sources, noSuchPorts = [], onRetry, stylesOverride }: EndpointsSectionProps) {
  return (
    <Section aria-labelledby="backend-endpoints">
      <h2 id="backend-endpoints">{pathColumnCopy.endpoints}</h2>
      <Sources stylesOverride={stylesOverride}>
        {sources.length ? null : <span>{endpointsCopy.noPorts}</span>}
        {sources.map((e, i) => {
          const port = e.port ? endpointsCopy.port(e.port.port, e.port.name) : "";
          const lacks = !!e.port && noSuchPorts.includes(e.port.port);
          const s = lacks ? { text: portChainCopy.noSuchPort(e.port?.port ?? 0), cached: false } : sourceSummary(e);
          // Not applicable reads nothing, and an unavailable source without counts was not read: no read time.
          const timed =
            e.complete !== "not-applicable" && !(e.complete === "unavailable" && e.ready === undefined) && !lacks;
          const items = e.complete === "complete" || e.complete === "partial" ? (e.items ?? []) : undefined;
          return (
            <Source key={`${e.source}/${e.port?.port ?? i}`} data-testid="endpoints-source" data-complete={e.complete}>
              <h3>
                {port}
                <span className="muted">{endpointsCopy.source[e.source] ?? e.source}</span>
                {timed ? <ReadAge readAt={e.readAt} /> : null}
              </h3>
              <div className="summary">
                <span data-testid="endpoints-summary">{s.text}</span>
                {s.cached ? <span className="muted">{endpointsCopy.cachedNote}</span> : null}
                {e.complete === "partial" ? <span className="muted">{endpointsCopy.partialNote}</span> : null}
                {e.cachedReady !== undefined ? (
                  <span data-testid="endpoints-disagree">{endpointsCopy.disagree(e.cachedReady)}</span>
                ) : null}
                {e.complete === "busy" || e.complete === "timed-out" ? (
                  <QuietButton type="button" onClick={onRetry}>
                    {endpointsCopy.retry}
                  </QuietButton>
                ) : null}
              </div>
              {items?.length ? (
                <table aria-label={endpointsCopy.table(port)}>
                  <thead>
                    <tr>
                      <th scope="col">{endpointsCopy.address}</th>
                      <th scope="col">{endpointsCopy.ready}</th>
                      <th scope="col">{endpointsCopy.serving}</th>
                      <th scope="col">{endpointsCopy.terminating}</th>
                      <th scope="col">{endpointsCopy.pod}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {items.map((x) => (
                      <tr key={`${x.address}:${x.port ?? ""}`}>
                        <td className="mono">{x.port ? `${x.address}:${x.port}` : x.address}</td>
                        <td>{yesNo(x.ready)}</td>
                        <td>{yesNo(x.serving)}</td>
                        <td>{yesNo(x.terminating)}</td>
                        <td className="mono">{x.pod ?? ""}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : items && e.complete === "complete" ? (
                <span className="muted">{endpointsCopy.noItems}</span>
              ) : null}
            </Source>
          );
        })}
        <ProxySlot data-testid="per-proxy-slot">{endpointsCopy.perProxy}</ProxySlot>
      </Sources>
    </Section>
  );
}
