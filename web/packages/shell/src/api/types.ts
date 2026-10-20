import type { HealthState } from "../theme/tokens";

/** Mirrors pkg/api, pkg/overview, pkg/health JSON as built (sub-phase 1.1). */
export type Severity = "error" | "warning" | "info";
export type Category = "gatewayclass" | "gateway" | "listenerset" | "route" | "policy" | "workload" | "parameters";
export interface Ref {
  group: string;
  kind: string;
  namespace?: string;
  name: string;
}
/** restricted: the caller may not view the resource; message is "" and detail/since are absent (D-013). */
export interface Finding {
  code: string;
  slug: string;
  severity: Severity;
  install: string;
  resource?: Ref;
  message: string;
  detail?: string;
  since?: string;
  restricted?: boolean;
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
/** computedAt: when the backend computed this health; stale summaries render as unknown. */
export interface InstallSummary extends InstallHeader {
  health: { state: HealthState; counts: Partial<Record<Severity, number>> };
  computedAt: string;
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
export interface GatewayRow {
  ref: Ref;
  class: string;
  state: HealthState;
  listeners: number;
  attachedRoutes: number;
  addresses: string[];
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
}
export interface ErrorResponse {
  error: string;
}
