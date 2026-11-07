import type { HealthState } from "../theme/tokens";
import type { LinkedRef, Page } from "./exploreTypes";
import type { CheckResult, Ref, Severity } from "./types";

/** Mirrors the diagnostics DTOs of pkg/api/diag_types.go as built (sub-phase 3.1, design §13.3). */

/**
 * One finding of GET …/findings. restricted: the caller may not view the resource; only identity is sent (no message).
 * evidenceRestricted: the detail was replaced because it quotes an object the caller may not view.
 */
export interface FindingItem {
  id: string;
  code: string;
  slug: string;
  severity: Severity;
  resource?: LinkedRef;
  message?: string;
  detail?: string;
  since?: string;
  group?: string;
  restricted?: boolean;
  evidenceRestricted?: boolean;
}
/** One command a user can run to learn more; needs names the permissions it requires, e.g. "pods/log". */
export interface NextStep {
  title: string;
  command: string;
  needs?: string[];
  note?: string;
}
/** The one change the evidence leaves (design §7.7); path is the fragment's location when create is false. */
export interface SuggestedFix {
  title: string;
  target: LinkedRef;
  create: boolean;
  path?: string;
  yaml: string;
  basis: string;
}
/** GET …/findings/{findingId}: affected pages through the visible objects of the finding's group, 50 at a time.
 * evidence and nextSteps are absent on restricted and evidence-restricted findings. */
export interface FindingDetail {
  install: string;
  finding: FindingItem;
  evidence?: Evidence | OtherEvidence;
  suggestedFix?: SuggestedFix;
  nextSteps?: NextStep[];
  affected: Page<LinkedRef>;
  collectedAt: string;
}
export type FindingList = Page<FindingItem>;
/** GET …/findings?view=groups: every group in rank order, a group listed when one member matches the filters. */
export type GroupList = Page<GroupRow>;

/** Typed evidence of a finding, tagged by kind (design §13.3, sub-phase 3.2). */
export type Evidence =
  | ProcessEvidence
  | VersionSkewEvidence
  | XDSRejectsEvidence
  | ProxyStatsEvidence
  | FreshnessEvidence
  | RefsEvidence
  | RuleReplacedEvidence
  | ListenerConflictEvidence
  | StatusProgressEvidence
  | EndpointsEvidence
  | WithheldEvidence;
/** Evidence of a kind this build does not know, e.g. a downstream's; its finding renderer shows it. */
export interface OtherEvidence {
  kind: string;
  [field: string]: unknown;
}
/** How a process last ended; never its message. */
export interface TerminationFacts {
  reason?: string;
  exitCode: number;
  at: string;
}
/** Probe failures observed in the Events window; type is absent when the Pod defines more than one probe type. */
export interface ProbeFacts {
  type?: string;
  failures: number;
  last: string;
  historyUnknown?: boolean;
}
/** Whether the install's controller log was read; not read is never "nothing found". */
export type ControllerLogs = "read" | "not-read";
/** A Go panic from the previous container's log; a trace has no time, so readAt is when the UI read it. */
export interface PanicFacts {
  class: string;
  frame?: string;
  readAt: string;
}
/** One kind of deployer error on a Gateway: its class, the object it names when visible, and when last logged. */
export interface DeployerError {
  class: string;
  object?: Ref;
  last: string;
}
/**
 * One Pod's facts behind KGWUI-005, 006 and 019–021; restarts are those observed in the window. pod is absent on a
 * 021 whose only facts come from the controller log; deployer is newest first.
 */
export interface ProcessEvidence {
  kind: "process";
  pod?: string;
  restarts?: number;
  termination?: TerminationFacts;
  waiting?: string;
  probe?: ProbeFacts;
  controllerLogs?: ControllerLogs;
  panic?: PanicFacts;
  deployer?: DeployerError[];
}
/** The Pods running one version: the first few names, and total counts them all. */
export interface VersionPods {
  version: string;
  pods: string[];
  total: number;
}
/** The versions behind KGWUI-022; expected is the Gateway API bundle minor the controller is built against. */
export interface VersionSkewEvidence {
  kind: "version-skew";
  controller?: string;
  proxies?: VersionPods[];
  replicas?: VersionPods[];
  crds?: { name: string; bundleVersion?: string; channel?: string }[];
  expected?: string;
}
/** One rejected xDS type: streams summed over the reporting controller Pods, rejects_total growth since firstSeen. */
export interface XDSTypeRejects {
  typeUrl: string;
  streams: number;
  reportedBy: string[];
  firstSeen: string;
  totalSinceFirstSeen: number;
}
/** Whether a type's update_rejected grew since the Pod's previous scrape; envoyResources are Envoy names, never objects. */
export interface ProxyTypeRejects {
  typeUrl: string;
  rejecting: boolean;
  rejectingSince?: string;
  envoyResources?: string[];
  envoyResourcesTruncated?: boolean;
}
/** One rejected type's controller log lines, whichever replica logged them: the earliest, classes, and visible Envoy names. */
export interface XDSLogEvent {
  typeUrl: string;
  since: string;
  classes: string[];
  envoyResources?: string[];
}
/**
 * KGWUI-001: rejected types, the Gateway's proxies, visible objects changed shortly before the first reject, and the
 * controller log's events, absent when the log was not read.
 */
export interface XDSRejectsEvidence {
  kind: "xds-rejects";
  types: XDSTypeRejects[];
  proxies?: { ready: number; desired: number };
  statusSaysHealthy: boolean;
  proxyStats?: { pod: string; types: ProxyTypeRejects[] }[];
  recentlyChanged?: LinkedRef[];
  controllerLogs: ControllerLogs;
  logEvents?: XDSLogEvent[];
}
/** One Ready proxy's Envoy stats behind KGWUI-025 or 026; an absent field was not reported. */
export interface ProxyPodStats {
  pod: string;
  connected?: boolean;
  warmingListeners?: number;
  warmingClusters?: number;
  serverState?: string;
}
export interface ProxyStatsEvidence {
  kind: "proxy-stats";
  pods: ProxyPodStats[];
}
/** Why KGWUI-003 judges a status not current (design §10.1); observed, current and since are set when stale. */
export interface FreshnessEvidence {
  kind: "freshness";
  freshness: "never-written" | "stale" | "pending" | "no-ancestor";
  observed?: number;
  current?: number;
  since?: string;
}
/** A ref and where its referrer writes it, e.g. spec.rules[2].backendRefs[0]; ref has the spec defaults applied. */
export interface RefLocation {
  path: string;
  ref: Ref;
}
/**
 * Why KGWUI-013 or 014 names its refs (design §7.3). candidates are the refs the condition may be about;
 * attributed is true only with exactly one.
 */
export interface RefsEvidence {
  kind: "refs";
  explanation: "kind-not-served" | "controller";
  candidates?: RefLocation[];
  attributed: boolean;
}
/**
 * Why KGWUI-015 reports a rule (design §7.4). matchIndex is the controller's match index, not a rule index,
 * and absent when the message does not parse; policy is the one served policy the message names.
 */
export interface RuleReplacedEvidence {
  kind: "rule-replaced";
  matchIndex?: number;
  reason: "RouteRuleReplaced" | "RouteRuleDropped";
  message: string;
  policy?: LinkedRef;
}
/** Another listener on the same Gateway that the rule finds; owner is its Gateway or ListenerSet. */
export interface ConflictingListener {
  owner: LinkedRef;
  name: string;
  protocol?: string;
  hostname?: string;
}
/**
 * Why KGWUI-017 reports a listener (design §7.5): the controller's Conflicted condition, and the other listeners
 * the UI computes by rule, chosen by reason. rule is absent for a reason no rule covers, such as a reserved port.
 * others holds the first few; othersTotal counts them all.
 */
export interface ListenerConflictEvidence {
  kind: "listener-conflict";
  listener: string;
  port: number;
  protocol?: string;
  hostname?: string;
  reason: string;
  message: string;
  rule?: "listener port and hostname" | "listener port and protocol";
  others?: ConflictingListener[];
  othersTotal: number;
}

/** How many stuck objects have one design §10.1 kind; an object counts under its first kind in table order. */
export interface FreshnessCount {
  freshness: FreshnessEvidence["freshness"];
  objects: number;
}
/** Why KGWUI-018 reports the controller (design §10.2): stuck objects per kind and the oldest KGWUI-003 warning's since. */
export interface StatusProgressEvidence {
  kind: "status-progress";
  kinds: FreshnessCount[];
  oldest: string;
}

/**
 * Why KGWUI-023 reports a Service port (design §11): the port as the backendRef writes it, with its Service port name,
 * and the ready and total endpoints of the larger address family across `slices` EndpointSlices.
 */
export interface EndpointsEvidence {
  kind: "endpoints";
  service: string;
  port: number;
  portName?: string;
  ready: number;
  total: number;
  slices: number;
}

/**
 * A Gateway's kgateway 2.5 per-client metrics behind KGWUI-024 (design §8.6), summed over controller Pods:
 * its withheld proxies, and the alerting bounded publishes of the last 15 minutes by mode.
 */
export interface WithheldEvidence {
  kind: "withheld";
  withheld?: number;
  releases?: ReleaseModeCount[];
  reportedBy: string[];
}
/** How many bounded publishes of one mode a Gateway had. */
export interface ReleaseModeCount {
  mode: string;
  count: number;
}

/**
 * One ranked group of "Needs attention" or the grouped findings view, with its rank keys. class: 1 data plane, 2 processes, 3 traffic, 4 hygiene.
 * code and title are the top member's. restricted: the cause is hidden; title is "" and there are no affected objects or steps.
 */
export interface GroupRow {
  id: string;
  title: string;
  code: string;
  severity: Severity;
  class: number;
  cause?: LinkedRef;
  members: number;
  affected: LinkedRef[];
  affectedTotal: number;
  since?: string;
  nextSteps?: NextStep[];
  restricted?: boolean;
  /** A "Not permitted" group's shape, as it has no cause object; absent when the caller may not view it. */
  grant?: GrantShape;
}
/** The (from namespace, to namespace, to group and kind) one ReferenceGrant allows; toGroup is "" for core. */
export interface GrantShape {
  fromNamespace: string;
  toNamespace: string;
  toGroup: string;
  toKind: string;
}
/** The install's state over every finding; reason names the required check that made it unknown. */
export interface Verdict {
  state: HealthState;
  problems: number;
  counts: Partial<Record<Severity, number>>;
  oldestSince?: string;
  reason?: string;
}
/** The controller line; state is the server's (its worst controller finding). restricted keeps the counts and state only. */
export interface ProcessSummary {
  state: HealthState;
  observed: boolean;
  ready?: number;
  desired?: number;
  version?: string;
  restricted?: boolean;
}
/** Visible Gateways: with every proxy ready, rejecting, with skew, restarting, and not observed. */
export interface ProxySummary {
  /** The worst known health of these Gateways, from the server; unknown when none is known. */
  state: HealthState;
  gateways: number;
  ready: number;
  rejecting: number;
  skew: number;
  restarting: number;
  notObserved: number;
}
export interface DataPlaneSummary {
  gateways: number;
  rejecting: number;
  /** Gateways with a KGWUI-024 error: their proxies get no config updates. */
  withheld?: number;
  unavailable?: boolean;
}
/** GET …/diagnostics: groups are the first 10 of groupTotal. */
export interface Diagnostics {
  install: string;
  verdict: Verdict;
  groups: GroupRow[];
  groupTotal: number;
  processes: { controller: ProcessSummary; proxies: ProxySummary };
  dataPlane: DataPlaneSummary;
  checks: CheckResult[];
  collectedAt: string;
}
/** One replica or proxy; an absent field was not observed. */
export interface ProcessInfo {
  name: string;
  ready?: boolean;
  restarts?: number;
  restartsRecent?: number;
  lastStart?: string;
  termination?: TerminationFacts;
  waiting?: string;
  version?: string;
  probe?: ProbeFacts;
}
/** The processes behind one role; observed false means not observed, never failing. restricted: counts only. */
export interface ProcessSet {
  observed: boolean;
  desired?: number;
  ready?: number;
  processes: ProcessInfo[];
  rollout?: { progressing: boolean; reason?: string };
  restricted?: boolean;
}
/** One Gateway's proxies; skew includes patch differences, which are never a finding. */
export interface GatewayProcesses {
  gateway: LinkedRef;
  health: HealthState;
  set: ProcessSet;
  rejects?: number;
  skew?: boolean;
}
/** GET …/processes?health=…&q=…&skew=…&restarts=… */
export interface ProcessesResponse {
  install: string;
  controller: ProcessSet;
  proxies: Page<GatewayProcesses>;
  collectedAt: string;
}
/**
 * A Gateway's data plane. rejects and recentlyChanged need every evidence dependency, else evidenceRestricted.
 * unavailable: the xDS rejects check is not OK, so rejecting false means unknown.
 */
export interface DataPlane {
  rejecting: boolean;
  /** A KGWUI-024 error: the proxies get no config updates. */
  configWithheld?: boolean;
  rejects: { typeUrl: string; streams: number; firstSeen: string }[];
  statusSaysHealthy: boolean;
  /** The controller's xDS snapshot: what it built, not what proxies accepted; never a finding. */
  snapshot?: {
    by: "controller";
    listeners: number;
    routes: number;
    clusters: number;
    endpoints: number;
    secrets: number;
  };
  nextSteps: NextStep[];
  recentlyChanged: LinkedRef[];
  evidenceRestricted?: boolean;
  unavailable?: boolean;
  proxyStats: { available: boolean; unavailable?: CheckResult["unavailable"]; reason?: string };
  /** The controller log shows proxies may get no config updates (kgateway 2.3 and 2.4): the first and last such line. */
  withheld?: { message: string; first: string; last: string };
  /** Whether the install's controller log was read; when not, an absent withheld is unobserved, never "not withheld". */
  controllerLogs: ControllerLogs;
}
