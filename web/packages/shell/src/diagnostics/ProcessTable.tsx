import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useNow } from "../api/clock";
import type { ProcessInfo, ProcessSet } from "../api/types";
import { diagnosticsCopy, findingsCopy } from "../copy/explorer";
import { HealthBadge } from "../health/HealthBadge";
import { ago } from "../pages/install/HealthStrip";

// region Styles
/** Height of one table row, fixed so polled values never move the rows below. */
export const DIAG_ROW_H = 36;

/** A compact table for diagnostics facts; scrolls sideways on narrow screens instead of the page. */
export const DiagTable = styled.div<{ stylesOverride?: SerializedStyles }>`
  overflow-x: auto;
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: ${(p) => p.theme.font.size.sm};
  }
  th {
    text-align: left;
    font-size: ${(p) => p.theme.font.size.xs};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    color: ${(p) => p.theme.color.textMuted};
    padding: ${(p) => p.theme.space(1.5)} ${(p) => p.theme.space(3)};
    border-bottom: 1px solid ${(p) => p.theme.color.border};
    white-space: nowrap;
  }
  td {
    height: ${DIAG_ROW_H}px;
    padding: 0 ${(p) => p.theme.space(3)};
    border-bottom: 1px solid ${(p) => p.theme.color.border};
    vertical-align: middle;
    overflow-wrap: anywhere;
  }
  tr:last-of-type td {
    border-bottom: 0;
  }
  .mono {
    font-family: ${(p) => p.theme.font.mono};
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
  }
  ${(p) => p.stylesOverride}
`;
const Lines = styled.div`
  display: grid;
  gap: ${(p) => p.theme.space(1)};
  font-size: ${(p) => p.theme.font.size.sm};
  .muted {
    color: ${(p) => p.theme.color.textMuted};
  }
`;

// region Helpers
function Readiness({ ready }: { ready?: boolean }) {
  if (ready === undefined) return <span className="muted">{diagnosticsCopy.notReported}</span>;
  return ready ? (
    <HealthBadge state="healthy" label={diagnosticsCopy.ready} />
  ) : (
    <HealthBadge state="error" label={diagnosticsCopy.notReady} />
  );
}

function Ended({ p, now }: { p: ProcessInfo; now: number }) {
  if (!p.termination) return null;
  return (
    <>
      <span>{diagnosticsCopy.termination(p.termination.reason, p.termination.exitCode)}</span>{" "}
      <span className="muted">{ago(p.termination.at, now)}</span>
    </>
  );
}

// region Component
interface ProcessTableProps {
  set: ProcessSet;
  /** The table's accessible name, e.g. "Proxies of infra/edge". */
  label: string;
  stylesOverride?: SerializedStyles;
}

/** One role's processes. Not observed is never failing; a restricted set shows its counts only (design §14 rule 16). */
export function ProcessTable({ set, label, stylesOverride }: ProcessTableProps) {
  const now = useNow();
  if (!set.observed) return <HealthBadge state="unknown" label={diagnosticsCopy.notObserved} />;
  const counts =
    set.ready !== undefined && set.desired !== undefined ? diagnosticsCopy.readyOf(set.ready, set.desired) : undefined;
  if (set.restricted)
    return (
      <Lines>
        {counts ? <span>{counts}</span> : null}
        <span className="muted">{findingsCopy.restricted}</span>
      </Lines>
    );
  const probes = set.processes.some((p) => p.probe);
  const stuck = set.rollout && !set.rollout.progressing && set.rollout.reason;
  return (
    <Lines>
      {stuck ? <HealthBadge state="warning" label={diagnosticsCopy.rolloutStuck(stuck)} /> : null}
      {set.processes.length === 0 ? (
        <span className="muted">{diagnosticsCopy.noProcesses}</span>
      ) : (
        <DiagTable stylesOverride={stylesOverride}>
          <table aria-label={label}>
            <thead>
              <tr>
                <th scope="col">{diagnosticsCopy.name}</th>
                <th scope="col">{diagnosticsCopy.ready}</th>
                <th scope="col">{diagnosticsCopy.restartsInWindow}</th>
                <th scope="col">{diagnosticsCopy.lastStart}</th>
                <th scope="col">{diagnosticsCopy.lastEnded}</th>
                <th scope="col">{diagnosticsCopy.versionHeader}</th>
                {probes ? <th scope="col">{diagnosticsCopy.probe}</th> : null}
              </tr>
            </thead>
            <tbody>
              {set.processes.map((p) => (
                <tr key={p.name}>
                  <td className="mono">{p.name}</td>
                  <td>
                    {p.waiting ? <HealthBadge state="warning" label={p.waiting} /> : <Readiness ready={p.ready} />}
                  </td>
                  <td>{diagnosticsCopy.restarts(p.restarts, p.restartsRecent)}</td>
                  <td className="muted">{p.lastStart ? ago(p.lastStart, now) : null}</td>
                  <td>
                    <Ended p={p} now={now} />
                  </td>
                  <td className="mono">{p.version}</td>
                  {probes ? (
                    <td>{p.probe ? diagnosticsCopy.probeFailures(p.probe.type, p.probe.failures) : null}</td>
                  ) : null}
                </tr>
              ))}
            </tbody>
          </table>
        </DiagTable>
      )}
    </Lines>
  );
}
