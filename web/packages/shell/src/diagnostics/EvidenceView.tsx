import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { ExternalLink } from "lucide-react";
import { Fragment, type ReactNode } from "react";
import { useNow } from "../api/clock";
import { useMeta } from "../api/hooks";
import type {
  ControllerLogs,
  EndpointsEvidence,
  Evidence,
  Finding,
  FreshnessEvidence,
  ListenerConflictEvidence,
  OtherEvidence,
  ProcessEvidence,
  ProxyStatsEvidence,
  Ref,
  RefsEvidence,
  RuleReplacedEvidence,
  StatusProgressEvidence,
  VersionPods,
  VersionSkewEvidence,
  WithheldEvidence,
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

const DocLink = styled.a<{ stylesOverride?: SerializedStyles }>`
  display: inline-flex;
  align-items: center;
  gap: ${(p) => p.theme.space(1)};
  font-size: ${(p) => p.theme.font.size.sm};
  color: ${(p) => p.theme.color.accentText};
  text-decoration: underline;
  text-underline-offset: 2px;
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    color: ${(p) => p.theme.color.text};
    text-decoration-thickness: 2px;
  }
  &:active {
    color: ${(p) => p.theme.color.textMuted};
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
  ${(p) => p.stylesOverride}
`;

// region Helpers
/** Where this repository's files are published; docs/findings.md is read at a release tag or on main. */
const REPO_BLOB = "https://github.com/DuncanDoyle/kgateway-ui/blob";
/** A version /meta reports for a tagged release build; dev and other builds link main. */
const RELEASE_TAG = /^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/;

/**
 * The findings reference entry for code, e.g. "#kgwui-001": at the release tag of version, else on main, or at base
 * when a downstream sets one. Undefined for a code the reference does not cover, such as a downstream prefix.
 */
export function findingDocUrl(code: string, version?: string, base?: string) {
  if (!/^KGWUI-\d{3}$/.test(code)) return undefined;
  const doc = base ?? `${REPO_BLOB}/${version && RELEASE_TAG.test(version) ? version : "main"}/docs/findings.md`;
  return `${doc}#${code.toLowerCase()}`;
}

/** The type's message name, e.g. "Cluster" for envoy.config.cluster.v3.Cluster. */
export const shortType = (typeUrl: string) => typeUrl.split(/[./]/).pop() || typeUrl;

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
                              ? diagnosticsCopy.rejectingSince(ago(t.rejectingSince, now))
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
      <LogState state={e.controllerLogs} />
      {e.logEvents?.length ? (
        <div>
          <h3>{diagnosticsCopy.logEvents}</h3>
          <DiagTable>
            <table aria-label={diagnosticsCopy.logEvents}>
              <thead>
                <tr>
                  <th scope="col">{diagnosticsCopy.type}</th>
                  <th scope="col">{diagnosticsCopy.classes}</th>
                  <th scope="col">{diagnosticsCopy.envoyResources}</th>
                  <th scope="col">{diagnosticsCopy.firstSeen}</th>
                </tr>
              </thead>
              <tbody>
                {/* Names and classes only: a log line's text is never sent or shown (design §8.9). */}
                {e.logEvents.map((l) => (
                  <tr key={l.typeUrl}>
                    <td className="mono" title={l.typeUrl}>
                      {shortType(l.typeUrl)}
                    </td>
                    <td className="mono">{l.classes.join(", ")}</td>
                    <td className="mono">{l.envoyResources?.join(", ")}</td>
                    <td className="muted">{ago(l.since, now)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </DiagTable>
        </div>
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

/** Whether the controller log was read; not read is never "nothing found" (D-006). */
function LogState({ state }: { state?: ControllerLogs }) {
  if (!state) return null;
  return (
    <Facts>
      <dt>{diagnosticsCopy.controllerLog}</dt>
      <dd className={state === "read" ? undefined : "muted"}>{diagnosticsCopy.logState[state] ?? state}</dd>
    </Facts>
  );
}

/** "Gateway infra/edge"; group adds a non-core group, as a ref's group can be its fault (013). */
const refText = (r: Ref, group = false) =>
  `${group && r.group ? `${r.group}/` : ""}${r.kind} ${r.namespace ? `${r.namespace}/` : ""}${r.name}`;

function Process({ e, now }: { e: ProcessEvidence; now: number }) {
  return (
    <>
      <ProcessFacts e={e} now={now} />
      {e.deployer?.length ? (
        <div>
          <h3>{diagnosticsCopy.deployerErrors}</h3>
          <DiagTable>
            <table aria-label={diagnosticsCopy.deployerErrors}>
              <thead>
                <tr>
                  <th scope="col">{diagnosticsCopy.errorClass}</th>
                  <th scope="col">{diagnosticsCopy.objects}</th>
                  <th scope="col">{diagnosticsCopy.lastSeen}</th>
                </tr>
              </thead>
              <tbody>
                {e.deployer.map((d) => (
                  <tr key={`${d.class}|${d.object ? refText(d.object) : ""}`}>
                    <td className="mono">{d.class}</td>
                    <td className="mono">{d.object ? refText(d.object) : null}</td>
                    <td className="muted">{ago(d.last, now)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </DiagTable>
        </div>
      ) : null}
    </>
  );
}

function ProcessFacts({ e, now }: { e: ProcessEvidence; now: number }) {
  return (
    <Facts>
      {/* A 021 from the controller log alone has no Pod. */}
      {e.pod ? (
        <>
          <dt>{diagnosticsCopy.pod}</dt>
          <dd className="mono">{e.pod}</dd>
        </>
      ) : null}
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
      {e.panic ? (
        <>
          <dt>{diagnosticsCopy.panic}</dt>
          <dd>
            <span className="mono">{e.panic.class}</span>
            <div className="muted">{diagnosticsCopy.panicReadAt(ago(e.panic.readAt, now))}</div>
          </dd>
          {e.panic.frame ? (
            <>
              <dt>{diagnosticsCopy.topFrame}</dt>
              <dd className="mono">{e.panic.frame}</dd>
            </>
          ) : null}
        </>
      ) : null}
      {e.controllerLogs ? (
        <>
          <dt>{diagnosticsCopy.controllerLog}</dt>
          <dd className={e.controllerLogs === "read" ? undefined : "muted"}>
            {diagnosticsCopy.logState[e.controllerLogs] ?? e.controllerLogs}
          </dd>
        </>
      ) : null}
    </Facts>
  );
}

function Refs({ e }: { e: RefsEvidence }) {
  const title = e.attributed ? diagnosticsCopy.refsAttributed : diagnosticsCopy.refsCandidates;
  return (
    <>
      {e.explanation === "kind-not-served" ? (
        <Facts>
          <dt>{diagnosticsCopy.reason}</dt>
          <dd>{diagnosticsCopy.kindNotServed}</dd>
        </Facts>
      ) : null}
      {e.candidates?.length ? (
        <div>
          <h3>{title}</h3>
          {e.attributed ? null : <div className="muted">{diagnosticsCopy.refsNotAttributed}</div>}
          <DiagTable>
            <table aria-label={title}>
              <thead>
                <tr>
                  <th scope="col">{diagnosticsCopy.location}</th>
                  <th scope="col">{diagnosticsCopy.ref}</th>
                </tr>
              </thead>
              <tbody>
                {e.candidates.map((c) => (
                  <tr key={c.path}>
                    <td className="mono">{c.path}</td>
                    <td className="mono">{refText(c.ref, true)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </DiagTable>
        </div>
      ) : null}
    </>
  );
}

function RuleReplaced({ e, installId }: { e: RuleReplacedEvidence; installId: string }) {
  return (
    <Facts>
      <dt>{diagnosticsCopy.reason}</dt>
      <dd>{diagnosticsCopy.ruleReason[e.reason] ?? e.reason}</dd>
      {e.matchIndex !== undefined ? (
        <>
          <dt>{diagnosticsCopy.match}</dt>
          <dd>
            <span className="mono">{diagnosticsCopy.matchIndex(e.matchIndex)}</span>
            <div className="muted">{diagnosticsCopy.matchNote}</div>
          </dd>
        </>
      ) : null}
      {e.policy ? (
        <>
          <dt>{diagnosticsCopy.policy}</dt>
          <dd>
            {e.policy.kind} <LinkedName refValue={e.policy} installId={installId} />
          </dd>
        </>
      ) : null}
      <dt>{diagnosticsCopy.controllerMessage}</dt>
      <dd className="mono">{e.message}</dd>
    </Facts>
  );
}

function Freshness({ e, now }: { e: FreshnessEvidence; now: number }) {
  return (
    <Facts>
      <dt>{diagnosticsCopy.status}</dt>
      <dd>{diagnosticsCopy.freshness[e.freshness] ?? e.freshness}</dd>
      {e.observed !== undefined && e.current !== undefined ? (
        <>
          <dt>{diagnosticsCopy.generation}</dt>
          <dd>{diagnosticsCopy.generations(e.observed, e.current)}</dd>
        </>
      ) : null}
      {e.since ? (
        <>
          <dt>{diagnosticsCopy.since}</dt>
          <dd>{ago(e.since, now)}</dd>
        </>
      ) : null}
    </Facts>
  );
}

function ListenerConflict({ e, installId }: { e: ListenerConflictEvidence; installId: string }) {
  const others = e.others ?? [];
  const rest = e.othersTotal - others.length;
  return (
    <>
      <Facts>
        <dt>{diagnosticsCopy.listener}</dt>
        <dd className="mono">{e.listener}</dd>
        <dt>{diagnosticsCopy.port}</dt>
        <dd>{e.port}</dd>
        {e.protocol ? (
          <>
            <dt>{diagnosticsCopy.protocol}</dt>
            <dd className="mono">{e.protocol}</dd>
          </>
        ) : null}
        {e.hostname ? (
          <>
            <dt>{diagnosticsCopy.hostname}</dt>
            <dd className="mono">{e.hostname}</dd>
          </>
        ) : null}
        <dt>{diagnosticsCopy.reason}</dt>
        <dd className="mono">{e.reason}</dd>
        {e.rule ? (
          <>
            <dt>{diagnosticsCopy.rule}</dt>
            <dd>{diagnosticsCopy.computedRule(e.rule)}</dd>
          </>
        ) : null}
      </Facts>
      {others.length ? (
        <div>
          <h3>{diagnosticsCopy.keptListeners}</h3>
          <DiagTable>
            <table aria-label={diagnosticsCopy.keptListeners}>
              <thead>
                <tr>
                  <th scope="col">{diagnosticsCopy.owner}</th>
                  <th scope="col">{diagnosticsCopy.listener}</th>
                  <th scope="col">{diagnosticsCopy.protocol}</th>
                  <th scope="col">{diagnosticsCopy.hostname}</th>
                </tr>
              </thead>
              <tbody>
                {others.map((o) => (
                  <tr key={`${o.owner.kind}/${o.owner.namespace ?? ""}/${o.owner.name}/${o.name}`}>
                    <td>
                      {o.owner.kind} <LinkedName refValue={o.owner} installId={installId} />
                    </td>
                    <td className="mono">{o.name}</td>
                    <td className="mono">{reported(o.protocol)}</td>
                    <td className="mono">{reported(o.hostname)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </DiagTable>
          {rest > 0 ? <div className="muted">{diagnosticsCopy.more(rest)}</div> : null}
        </div>
      ) : null}
    </>
  );
}

function StatusProgress({ e, now }: { e: StatusProgressEvidence; now: number }) {
  return (
    <>
      <div>
        <h3>{diagnosticsCopy.stuckObjects}</h3>
        <DiagTable>
          <table aria-label={diagnosticsCopy.stuckObjects}>
            <thead>
              <tr>
                <th scope="col">{diagnosticsCopy.status}</th>
                <th scope="col">{diagnosticsCopy.objects}</th>
              </tr>
            </thead>
            <tbody>
              {e.kinds.map((k) => (
                <tr key={k.freshness}>
                  <td>{diagnosticsCopy.freshness[k.freshness] ?? k.freshness}</td>
                  <td>{k.objects}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </DiagTable>
      </div>
      <Facts>
        <dt>{diagnosticsCopy.oldest}</dt>
        <dd>{ago(e.oldest, now)}</dd>
        <dt>{diagnosticsCopy.syncs}</dt>
        {/* Absent means unknown, never zero (D-006). */}
        <dd className={e.syncs === undefined ? "muted" : undefined}>{e.syncs ?? diagnosticsCopy.notKnown}</dd>
      </Facts>
    </>
  );
}

function Endpoints({ e, finding, installId }: { e: EndpointsEvidence; finding?: Finding; installId: string }) {
  // The finding's resource is the Service; its server-computed link decides whether the name opens.
  const svc = finding?.resource?.kind === "Service" ? finding.resource : undefined;
  return (
    <Facts>
      <dt>{diagnosticsCopy.service}</dt>
      <dd>{svc ? <LinkedName refValue={svc} installId={installId} /> : <span className="mono">{e.service}</span>}</dd>
      <dt>{diagnosticsCopy.servicePort}</dt>
      <dd className="mono">{e.portName ? `${e.port} (${e.portName})` : e.port}</dd>
      <dt>{diagnosticsCopy.ready}</dt>
      <dd>{diagnosticsCopy.readyEndpoints(e.ready, e.total)}</dd>
      <dt>{diagnosticsCopy.endpointSlices}</dt>
      <dd>{e.slices}</dd>
    </Facts>
  );
}

function Withheld({ e }: { e: WithheldEvidence }) {
  return (
    <>
      <Facts>
        {e.withheld ? (
          <>
            <dt>{diagnosticsCopy.withheldProxies}</dt>
            <dd>{e.withheld}</dd>
          </>
        ) : null}
        <dt>{diagnosticsCopy.reportedBy}</dt>
        <dd className="mono">{e.reportedBy.join(", ")}</dd>
      </Facts>
      {e.releases?.length ? (
        <DiagTable>
          <table aria-label={diagnosticsCopy.releasedRecently}>
            <thead>
              <tr>
                <th scope="col">{diagnosticsCopy.releaseMode}</th>
                <th scope="col">{diagnosticsCopy.releaseCount}</th>
              </tr>
            </thead>
            <tbody>
              {e.releases.map((r) => {
                // A mode this build has no label for shows as the metric writes it.
                const label = diagnosticsCopy.releaseModes[r.mode];
                return (
                  <tr key={r.mode}>
                    <td>
                      {label ? <span>{label} </span> : null}
                      <span className={label ? "mono muted" : "mono"}>{r.mode}</span>
                    </td>
                    <td>{r.count}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </DiagTable>
      ) : null}
    </>
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
interface LearnMoreProps {
  code: string;
  stylesOverride?: SerializedStyles;
}

/** "Learn more": the code's entry in docs/findings.md, in a new tab; nothing for a code the reference does not cover. */
export function LearnMore({ code, stylesOverride }: LearnMoreProps) {
  const { data: meta } = useMeta();
  const { findingsDocUrl } = useShellExtension();
  const href = findingDocUrl(code, meta?.version, findingsDocUrl);
  if (!href) return null;
  return (
    <DocLink
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={diagnosticsCopy.learnMoreLabel(code)}
      stylesOverride={stylesOverride}
    >
      {diagnosticsCopy.learnMore}
      <ExternalLink size={12} aria-hidden="true" />
    </DocLink>
  );
}

interface EvidenceViewProps {
  /** Absent when the finding has none: its code's renderer still applies (the 3.0 renderer contract). */
  evidence?: Evidence | OtherEvidence;
  /** The finding the evidence belongs to; an unknown kind goes to the renderer registered for its code. */
  finding?: Finding;
  stylesOverride?: SerializedStyles;
}

/** A finding's typed evidence, one view per kind (design §17.2); other kinds use the finding renderers. */
export function EvidenceView({ evidence, finding, stylesOverride }: EvidenceViewProps) {
  const installId = useInstallId();
  const now = useNow();
  const { findingRenderers } = useShellExtension();
  const e = evidence as Evidence | undefined;
  let body: ReactNode;
  switch (e?.kind) {
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
    case "refs":
      body = <Refs e={e} />;
      break;
    case "rule-replaced":
      body = <RuleReplaced e={e} installId={installId} />;
      break;
    case "freshness":
      body = <Freshness e={e} now={now} />;
      break;
    case "listener-conflict":
      body = <ListenerConflict e={e} installId={installId} />;
      break;
    case "status-progress":
      body = <StatusProgress e={e} now={now} />;
      break;
    case "endpoints":
      body = <Endpoints e={e} finding={finding} installId={installId} />;
      break;
    case "withheld":
      body = <Withheld e={e} />;
      break;
    default: {
      // A restricted finding shows only its identity, so no renderer runs for it.
      const render = finding && !finding.restricted ? rendererFor(findingRenderers, finding.code) : undefined;
      if (!finding || !render) return null;
      const f = { ...finding, evidence };
      return <Slot id={`finding-renderer:${f.code}`} resetKey={findingKey(f)} render={() => render(f)} />;
    }
  }
  return (
    <Frame data-kind={e?.kind} stylesOverride={stylesOverride}>
      {body}
    </Frame>
  );
}
