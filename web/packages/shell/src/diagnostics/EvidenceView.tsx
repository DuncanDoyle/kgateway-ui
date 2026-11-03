import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Fragment, type ReactNode } from "react";
import { useNow } from "../api/clock";
import type {
  Evidence,
  Finding,
  OtherEvidence,
  ProcessEvidence,
  ProxyStatsEvidence,
  VersionPods,
  VersionSkewEvidence,
  XDSRejectsEvidence,
} from "../api/types";
import { diagnosticsCopy } from "../copy/explorer";
import { LinkedName } from "../explore/LinkedName";
import { HealthBadge } from "../health/HealthBadge";
import { Facts } from "../pages/explore/detailParts";
import { rendererFor } from "../pages/install/FindingRow";
import { ago } from "../pages/install/HealthStrip";
import { findingKey, Slot } from "../shell/contributions";
import { useInstallId, useShellExtension } from "../shell/extension";
import { DiagTable } from "./ProcessTable";

// region Styles
const Frame = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  gap: ${(p) => p.theme.space(3)};
  h3 {
    margin: 0 0 ${(p) => p.theme.space(1)};
    font-size: ${(p) => p.theme.font.size.sm};
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  .mono {
    font-family: ${(p) => p.theme.font.mono};
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
  }
  ul {
    margin: 0;
    padding-left: ${(p) => p.theme.space(4)};
    font-size: ${(p) => p.theme.font.size.sm};
  }
  ${(p) => p.stylesOverride}
`;

// region Helpers
/** The type's message name, e.g. "Cluster" for envoy.config.cluster.v3.Cluster. */
const shortType = (typeUrl: string) => typeUrl.split(/[./]/).pop() || typeUrl;

/** Pod names with the count of those not listed. */
const podList = (v: VersionPods) => {
  const rest = v.total - v.pods.length;
  return rest > 0 ? `${v.pods.join(", ")} ${diagnosticsCopy.more(rest)}` : v.pods.join(", ");
};

function XDSRejects({ e, installId, now }: { e: XDSRejectsEvidence; installId: string; now: number }) {
  return (
    <>
      {e.statusSaysHealthy ? <HealthBadge state="warning" label={diagnosticsCopy.statusSaysHealthy} /> : null}
      {e.proxies ? (
        <Facts>
          <dt>{diagnosticsCopy.proxies}</dt>
          <dd>{diagnosticsCopy.proxiesReady(e.proxies.ready, e.proxies.desired)}</dd>
        </Facts>
      ) : null}
      <DiagTable>
        <table aria-label={diagnosticsCopy.rejectedTypes}>
          <thead>
            <tr>
              <th scope="col">{diagnosticsCopy.type}</th>
              <th scope="col">{diagnosticsCopy.streams}</th>
              <th scope="col">{diagnosticsCopy.firstSeen}</th>
              <th scope="col">{diagnosticsCopy.reportedBy}</th>
            </tr>
          </thead>
          <tbody>
            {e.types.map((t) => (
              <tr key={t.typeUrl}>
                <td className="mono" title={t.typeUrl}>
                  {shortType(t.typeUrl)}
                </td>
                <td>{t.streams}</td>
                <td className="muted">{ago(t.firstSeen, now)}</td>
                <td className="mono">{t.reportedBy.join(", ")}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </DiagTable>
      {e.proxyStats?.length ? (
        <DiagTable>
          <table aria-label={diagnosticsCopy.proxyRejects}>
            <thead>
              <tr>
                <th scope="col">{diagnosticsCopy.pod}</th>
                <th scope="col">{diagnosticsCopy.type}</th>
                <th scope="col">{diagnosticsCopy.rejectingNow}</th>
                <th scope="col">{diagnosticsCopy.envoyResources}</th>
              </tr>
            </thead>
            <tbody>
              {e.proxyStats.flatMap((p) =>
                p.types.map((t) => (
                  <tr key={`${p.pod}|${t.typeUrl}`}>
                    <td className="mono">{p.pod}</td>
                    <td className="mono" title={t.typeUrl}>
                      {shortType(t.typeUrl)}
                    </td>
                    <td>
                      {t.rejecting ? (
                        <HealthBadge
                          state="error"
                          label={
                            t.rejectingSince
                              ? `${diagnosticsCopy.rejectingSince} ${ago(t.rejectingSince, now)}`
                              : diagnosticsCopy.rejectingNow
                          }
                        />
                      ) : (
                        <span className="muted">{diagnosticsCopy.notRejecting}</span>
                      )}
                    </td>
                    <td className="mono">
                      {t.envoyResources?.join(", ")}
                      {t.envoyResourcesTruncated ? <span className="muted"> {diagnosticsCopy.truncated}</span> : null}
                    </td>
                  </tr>
                )),
              )}
            </tbody>
          </table>
        </DiagTable>
      ) : null}
      {e.recentlyChanged?.length ? (
        <div>
          <h3>{diagnosticsCopy.recentlyChanged}</h3>
          <div className="muted">{diagnosticsCopy.recentlyChangedNote}</div>
          <ul>
            {e.recentlyChanged.map((r) => (
              <li key={`${r.group}/${r.kind}/${r.namespace ?? ""}/${r.name}`}>
                {r.kind} <LinkedName refValue={r} installId={installId} />
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </>
  );
}

function Process({ e, now }: { e: ProcessEvidence; now: number }) {
  return (
    <Facts>
      <dt>{diagnosticsCopy.pod}</dt>
      <dd className="mono">{e.pod}</dd>
      {e.restarts !== undefined ? (
        <>
          <dt>{diagnosticsCopy.restartsInWindow}</dt>
          <dd>{e.restarts}</dd>
        </>
      ) : null}
      {e.termination ? (
        <>
          <dt>{diagnosticsCopy.lastEnded}</dt>
          <dd>
            <span>{diagnosticsCopy.termination(e.termination.reason, e.termination.exitCode)}</span>{" "}
            <span className="muted">{ago(e.termination.at, now)}</span>
          </dd>
        </>
      ) : null}
      {e.waiting ? (
        <>
          <dt>{diagnosticsCopy.waiting}</dt>
          <dd className="mono">{e.waiting}</dd>
        </>
      ) : null}
      {e.probe ? (
        <>
          <dt>{diagnosticsCopy.probe}</dt>
          <dd>
            <span>{diagnosticsCopy.probeFailures(e.probe.type, e.probe.failures)}</span>{" "}
            <span className="muted">{ago(e.probe.last, now)}</span>
            {e.probe.historyUnknown ? <div className="muted">{diagnosticsCopy.historyUnknown}</div> : null}
          </dd>
        </>
      ) : null}
    </Facts>
  );
}

function Versions({ title, list }: { title: string; list?: VersionPods[] }) {
  if (!list?.length) return null;
  return (
    <div>
      <h3>{title}</h3>
      <Facts>
        {list.map((v) => (
          <Fragment key={v.version}>
            <dt className="mono">{v.version || diagnosticsCopy.notReported}</dt>
            <dd className="mono">{podList(v)}</dd>
          </Fragment>
        ))}
      </Facts>
    </div>
  );
}

function VersionSkew({ e }: { e: VersionSkewEvidence }) {
  return (
    <>
      {e.controller || e.expected ? (
        <Facts>
          {e.controller ? (
            <>
              <dt>{diagnosticsCopy.controllerVersion}</dt>
              <dd className="mono">{e.controller}</dd>
            </>
          ) : null}
          {e.expected ? (
            <>
              <dt>{diagnosticsCopy.expected}</dt>
              <dd className="mono">{e.expected}</dd>
            </>
          ) : null}
        </Facts>
      ) : null}
      <Versions title={diagnosticsCopy.proxyVersions} list={e.proxies} />
      <Versions title={diagnosticsCopy.replicaVersions} list={e.replicas} />
      {e.crds?.length ? (
        <div>
          <h3>{diagnosticsCopy.crds}</h3>
          <Facts>
            {e.crds.map((c) => (
              <Fragment key={c.name}>
                <dt className="mono">{c.name}</dt>
                <dd className="mono">
                  {`${c.bundleVersion ?? diagnosticsCopy.notReported}${c.channel ? ` (${c.channel})` : ""}`}
                </dd>
              </Fragment>
            ))}
          </Facts>
        </div>
      ) : null}
    </>
  );
}

const reported = (v: number | string | undefined) =>
  v === undefined || v === "" ? <span className="muted">{diagnosticsCopy.notReported}</span> : v;

function ProxyStats({ e }: { e: ProxyStatsEvidence }) {
  return (
    <DiagTable>
      <table aria-label={diagnosticsCopy.proxyStats}>
        <thead>
          <tr>
            <th scope="col">{diagnosticsCopy.pod}</th>
            <th scope="col">{diagnosticsCopy.connected}</th>
            <th scope="col">{diagnosticsCopy.warmingListeners}</th>
            <th scope="col">{diagnosticsCopy.warmingClusters}</th>
            <th scope="col">{diagnosticsCopy.serverState}</th>
          </tr>
        </thead>
        <tbody>
          {e.pods.map((p) => (
            <tr key={p.pod}>
              <td className="mono">{p.pod}</td>
              <td>
                {p.connected === undefined ? (
                  reported(undefined)
                ) : p.connected ? (
                  <HealthBadge state="healthy" label={diagnosticsCopy.connected} />
                ) : (
                  <HealthBadge state="error" label={diagnosticsCopy.disconnected} />
                )}
              </td>
              <td>{reported(p.warmingListeners)}</td>
              <td>{reported(p.warmingClusters)}</td>
              <td className="mono">{reported(p.serverState)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </DiagTable>
  );
}

// region Component
interface EvidenceViewProps {
  evidence: Evidence | OtherEvidence;
  /** The finding the evidence belongs to; an unknown kind goes to the renderer registered for its code. */
  finding?: Finding;
  stylesOverride?: SerializedStyles;
}

/** A finding's typed evidence, one view per kind (design §17.2); other kinds use the finding renderers. */
export function EvidenceView({ evidence, finding, stylesOverride }: EvidenceViewProps) {
  const installId = useInstallId();
  const now = useNow();
  const { findingRenderers } = useShellExtension();
  const e = evidence as Evidence;
  let body: ReactNode;
  switch (e.kind) {
    case "xds-rejects":
      body = <XDSRejects e={e} installId={installId} now={now} />;
      break;
    case "process":
      body = <Process e={e} now={now} />;
      break;
    case "version-skew":
      body = <VersionSkew e={e} />;
      break;
    case "proxy-stats":
      body = <ProxyStats e={e} />;
      break;
    default: {
      const render = finding ? rendererFor(findingRenderers, finding.code) : undefined;
      if (!finding || !render) return null;
      const f = { ...finding, evidence };
      return <Slot id={`finding-renderer:${f.code}`} resetKey={findingKey(f)} render={() => render(f)} />;
    }
  }
  return (
    <Frame data-kind={e.kind} stylesOverride={stylesOverride}>
      {body}
    </Frame>
  );
}
