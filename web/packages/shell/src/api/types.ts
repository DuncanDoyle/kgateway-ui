import type { HealthState } from "../theme/tokens";
import type { Evidence, OtherEvidence } from "./diagTypes";
import type { Link, LinkedRef } from "./exploreTypes";

export * from "./diagTypes";
export * from "./exploreTypes";

/** Mirrors pkg/api, pkg/overview, pkg/health JSON as built (sub-phase 1.1). */
export type Severity = "error" | "warning" | "info";
export type Category =
  | "gatewayclass"
  | "gateway"
  | "listenerset"
  | "route"
  | "policy"
  | "workload"
  | "parameters"
  | "backend"
  | "referencegrant";
export interface Ref {
  group: string;
  kind: string;
  namespace?: string;
  name: string;
}
/**
 * restricted: the caller may not view the resource; message is "" and detail/since are absent (D-013).
 * evidenceRestricted: the detail was replaced because it quotes an object the caller may not view.
 * id opens the finding's page; group is its cause's group (both from sub-phase 3.1).
 * evidence is the typed evidence (sub-phase 3.2); absent when restricted or evidence-restricted.
 */
export interface Finding {
  id?: string;
  group?: string;
  code: string;
  slug: string;
  severity: Severity;
  install: string;
  resource?: LinkedRef;
  message: string;
  detail?: string;
  since?: string;
  restricted?: boolean;
  evidenceRestricted?: boolean;
  evidence?: Evidence | OtherEvidence;
}
/** detail: context for an OK check (e.g. scope source); pending: not OK only because of the grace period. */
export interface CheckResult {
  name: string;
  ok: boolean;
  required: boolean;
  collectedAt: string;
  error?: string;
  detail?: string;
  pending?: boolean;
  /** Why an optional input is missing; such a check never makes the install unknown (D-034). */
  unavailable?: "off" | "denied" | "failing" | "not-on-version" | "not-observed";
  /** The check is still deciding on count items, first at until; reason names no object (D-068). */
  hold?: CheckHold;
}
export interface CheckHold {
  until: string;
  count: number;
  reason: string;
}
/** Health as sent on the wire: checks is null when no check ran. */
export type RawHealthReport = Omit<HealthReport, "checks"> & { checks: CheckResult[] | null };
/** install is the install ID; a response for another install is never rendered. */
export interface HealthReport {
  install: string;
  state: HealthState;
  findings: Finding[];
  checks: CheckResult[];
  counts: Partial<Record<Severity, number>>;
  collectedAt: string;
}
export interface LabelSelectorRequirement {
  key: string;
  operator: "In" | "NotIn" | "Exists" | "DoesNotExist";
  values?: string[];
}
export interface LabelSelector {
  matchLabels?: Record<string, string>;
  matchExpressions?: LabelSelectorRequirement[];
}
/** source: where the scope was read from, e.g. "pod kgateway-abc" or "deployment template" (D-015). */
export interface ScopeSummary {
  mode: "all" | "selectors" | "unknown";
  selectors?: LabelSelector[];
  error?: string;
  source?: string;
  watched: number;
  total: number;
}
/** Controller readiness (D-016). not-observed: the platform cannot see it, which is not a failure. */
export interface Readiness {
  state: "ready" | "not-ready" | "unknown" | "not-observed";
  ready?: number;
  desired?: number;
}
export interface InstallHeader {
  /** Opaque: encode it in URLs, never split or parse it (D-016). */
  id: string;
  displayName: string;
  namespace?: string;
  name?: string;
  version: string;
  edition: string;
  controllerNames: string[];
  readiness: Readiness;
  /** Absent where the platform has no namespace scope. */
  scope?: ScopeSummary;
}
/**
 * computedAt: when the backend computed this health. confirmedAt: when the backend last
 * confirmed it matches the cluster; absent from older backends. Stale summaries render as unknown.
 */
export interface InstallSummary extends InstallHeader {
  health: { state: HealthState; counts: Partial<Record<Severity, number>> };
  /** The number of groups diagnostics lists: the install page reserves that many Needs attention rows while they load. */
  groupRows: number;
  computedAt: string;
  confirmedAt?: string;
  /** The install's UI features, e.g. "delegation"; absent from older servers, which have them all (design §10). */
  features?: string[];
}
export interface Access {
  ok: boolean;
  error?: string;
}
export interface Count {
  group: string;
  kind: string;
  category: Category;
  displayName: string;
  count: number;
  access: Access;
}
/**
 * Why a list or overview row is not healthy (D-063); healthy rows have none. A finding is the row's deciding
 * finding, code and message only, and more counts the object's other findings. The other kinds carry nothing else.
 */
export type RowReason =
  | { kind: "finding"; code: string; message: string; more?: number }
  | { kind: "unknown" }
  | { kind: "gateway-rejecting" };
export interface GatewayRow {
  ref: Ref;
  class: string;
  state: HealthState;
  reason?: RowReason;
  listeners: number;
  attachedRoutes: number;
  addresses: string[];
  link?: Link;
  /** The Data plane column, from the server: unknown while the xDS rejects check is not OK (design §6). */
  dataPlane?: "ok" | "rejects" | "withheld" | "unknown";
}
export interface OverviewResponse {
  header: InstallHeader;
  counts: Count[];
  gateways: GatewayRow[];
  collectedAt: string;
}
export interface ServedKind {
  group: string;
  kind: string;
  resource: string;
  versions: string[];
  namespaced: boolean;
  category: Category;
  displayName: string;
  version: string;
}
/** kinds: what the resource endpoint can serve (apps/Deployment only with workloads). capabilities: branch on these, never on platform (D-016). */
export interface MetaResponse {
  version: string;
  pinned?: string;
  synced: boolean;
  platform: string;
  capabilities: string[];
  kinds: ServedKind[];
  /** The group/kinds with a detail view; informational, links come from LinkedRef.link. */
  views?: { group: string; kind: string }[];
  /** The implementation packs the server runs, e.g. "kgateway"; absent from older servers, which run them all. */
  packs?: string[];
  /** Filters added after v1, per list (2.3: routes has listener). */
  filters?: Record<string, string[]>;
  /** Extension capabilities with their state, by name; omitted when there are none. */
  capabilityStates?: Capability[];
}
/** One extension capability; reason says why it is not on. Only "on" ones are also in MetaResponse.capabilities. */
export interface Capability {
  name: string;
  state: "on" | "off" | "denied" | "failing";
  reason?: string;
}
export interface ErrorResponse {
  error: string;
}
