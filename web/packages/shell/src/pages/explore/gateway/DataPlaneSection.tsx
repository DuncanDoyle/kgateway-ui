import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useNow } from "../../../api/clock";
import type { DataPlane } from "../../../api/types";
import { dataPlaneCopy, diagnosticsCopy } from "../../../copy/explorer";
import { shortType } from "../../../diagnostics/EvidenceView";
import { NextSteps } from "../../../diagnostics/NextSteps";
import { DiagTable } from "../../../diagnostics/ProcessTable";
import { LinkedName } from "../../../explore/LinkedName";
import { HealthBadge } from "../../../health/HealthBadge";
import { layout } from "../../../shell/layout";
import { ago } from "../../install/HealthStrip";
import { Facts, Section } from "../detailParts";

// region Styles
const Body = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  gap: ${(p) => p.theme.space(4)};
  h3 {
    margin: 0 0 ${(p) => p.theme.space(1)};
    font-size: ${(p) => p.theme.font.size.sm};
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
    max-width: ${layout.proseMax};
  }
  .mono {
    font-family: ${(p) => p.theme.font.mono};
  }
  ul {
    margin: ${(p) => p.theme.space(1)} 0 0;
    padding-left: ${(p) => p.theme.space(4)};
    font-size: ${(p) => p.theme.font.size.sm};
  }
  ${(p) => p.stylesOverride}
`;

/** The snapshot counts on one wrapping line, label then number. */
const Counts = styled.dl`
  display: flex;
  flex-wrap: wrap;
  gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(4)};
  margin: ${(p) => p.theme.space(1)} 0 0;
  font-size: ${(p) => p.theme.font.size.sm};
  div {
    display: flex;
    gap: ${(p) => p.theme.space(1)};
  }
  dt {
    color: ${(p) => p.theme.color.textMuted};
  }
  dd {
    margin: 0;
    font-variant-numeric: tabular-nums;
  }
`;

// region Helpers
/** The headline from the server's flags only: rejecting, not readable, or no rejects (R62, D-006). */
function Headline({ dp }: { dp: DataPlane }) {
  if (dp.rejecting)
    return (
      <HealthBadge
        state="error"
        label={dp.statusSaysHealthy ? diagnosticsCopy.statusSaysHealthy : dataPlaneCopy.rejecting}
      />
    );
  if (dp.unavailable) return <HealthBadge state="unknown" label={dataPlaneCopy.unavailable} />;
  return <HealthBadge state="healthy" label={diagnosticsCopy.noRejects} />;
}

function proxyStatsText(ps: DataPlane["proxyStats"]): string {
  if (ps.available) return dataPlaneCopy.proxyStatsOn;
  return (
    ps.reason ?? (ps.unavailable && diagnosticsCopy.unavailableReason[ps.unavailable]) ?? diagnosticsCopy.notAvailable
  );
}

const refKey = (r: { group: string; kind: string; namespace?: string; name: string }) =>
  `${r.group}/${r.kind}/${r.namespace ?? ""}/${r.name}`;

// region Component
interface DataPlaneSectionProps {
  installId: string;
  dataPlane: DataPlane;
  stylesOverride?: SerializedStyles;
}

/** A Gateway's data plane: what its proxies reject, what to run next, and what changed before (design §8.3–8.5). */
export function DataPlaneSection({ installId, dataPlane: dp, stylesOverride }: DataPlaneSectionProps) {
  const now = useNow();
  const snap = dp.snapshot;
  return (
    <Section aria-labelledby="gw-dataplane">
      <h2 id="gw-dataplane">{dataPlaneCopy.title}</h2>
      <Body stylesOverride={stylesOverride}>
        <div>
          <Headline dp={dp} />
          {dp.evidenceRestricted ? <div className="muted">{dataPlaneCopy.evidenceRestricted}</div> : null}
        </div>
        {dp.rejects.length ? (
          <DiagTable>
            <table aria-label={diagnosticsCopy.rejectedTypes}>
              <thead>
                <tr>
                  <th scope="col">{diagnosticsCopy.type}</th>
                  <th scope="col">{diagnosticsCopy.streams}</th>
                  <th scope="col">{diagnosticsCopy.firstSeen}</th>
                </tr>
              </thead>
              <tbody>
                {dp.rejects.map((t) => (
                  <tr key={t.typeUrl}>
                    <td className="mono" title={t.typeUrl}>
                      {shortType(t.typeUrl)}
                    </td>
                    <td>{t.streams}</td>
                    <td className="muted">{ago(t.firstSeen, now)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </DiagTable>
        ) : null}
        {dp.nextSteps.length ? (
          <div>
            <h3>{dataPlaneCopy.nextSteps}</h3>
            <NextSteps steps={dp.nextSteps} />
          </div>
        ) : null}
        {dp.recentlyChanged.length ? (
          <div>
            <h3>{diagnosticsCopy.recentlyChanged}</h3>
            <div className="muted">{diagnosticsCopy.recentlyChangedNote}</div>
            <ul>
              {dp.recentlyChanged.map((r) => (
                <li key={refKey(r)}>
                  {r.kind} <LinkedName refValue={r} installId={installId} />
                </li>
              ))}
            </ul>
          </div>
        ) : null}
        <Facts>
          <dt>{dataPlaneCopy.proxyStats}</dt>
          <dd>{proxyStatsText(dp.proxyStats)}</dd>
        </Facts>
        {snap ? (
          <div>
            <h3>{dataPlaneCopy.snapshot}</h3>
            <div className="muted">{dataPlaneCopy.snapshotNote}</div>
            <Counts>
              {(Object.keys(dataPlaneCopy.snapshotFacts) as (keyof typeof dataPlaneCopy.snapshotFacts)[]).map((k) => (
                <div key={k}>
                  <dt>{dataPlaneCopy.snapshotFacts[k]}</dt>
                  <dd>{snap[k]}</dd>
                </div>
              ))}
            </Counts>
          </div>
        ) : null}
      </Body>
    </Section>
  );
}
