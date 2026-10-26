import type { HealthState } from "../theme/tokens";
import type { LabelSelector, Ref } from "./types";

/** Mirrors the explorer DTOs of pkg/api/explore_types.go as built (sub-phases 2.2 and 2.3, design §7.3). */

/** How a reference may be opened; rendered as given, never derived from a kind (design §7.1). */
export type Link = "detail" | "yaml" | "none";
/** A reference with its server-computed link; the link sits flat beside the ref fields. */
export interface LinkedRef extends Ref {
  link: Link;
}
/** A group and kind, as in allowedRoutes.kinds or supportedKinds. */
export interface GroupKind {
  group: string;
  kind: string;
}
/** A parentRef after defaults; port 0 (unset) is omitted. */
export interface ParentRef {
  group: string;
  kind: string;
  namespace: string;
  name: string;
  sectionName?: string;
  port?: number;
}
/** A spec parentRef with its target's link, so a parent without status still links (design §7.1). */
export interface SpecParentRef extends ParentRef {
  target?: LinkedRef;
}

/** One page of a list. deniedInInstall counts the install's rows of the listed kinds the caller may not view. */
export interface Page<T> {
  install: string;
  items: T[];
  total: number;
  next?: string;
  prev?: string;
  deniedInInstall?: number;
  collectedAt: string;
}

export type Attachment = "accepted" | "not-accepted" | "pending" | "unwatched" | "declared";

/** A computed fact's label (design §7.3, sub-phase 2.3). restricted carries no reason and no note. */
export interface Computed {
  by: "ui";
  rules: string;
  rule: string;
  state: "checked" | "disagrees" | "pending" | "unknown" | "restricted";
  coverage?: ("count-agrees" | "status-compatible" | "not-verified")[];
  reason?: string;
  note?: string;
}
/** One listener a parentRef attaches to; hostnames only when checked or disagrees. */
export interface ListenerAttachment {
  resource: LinkedRef;
  listener: string;
  effectiveHostnames?: string[];
  computed: Computed;
}
/** View A's outcome of one parentRef; outcome is absent when unknown or restricted. */
export interface ComputedAttachment {
  outcome?: "accepted" | "NotAllowedByListeners" | "NoMatchingParent" | "NoMatchingListenerHostname";
  listeners: ListenerAttachment[];
  explanation?: string;
  computed: Computed;
}
/** View A for one Gateway or ListenerSet parentRef of a route, by its spec index. */
export interface RouteAttachment {
  parentRef: ParentRef;
  index: number;
  target: LinkedRef;
  result: ComputedAttachment;
}

/** One parent of a route row. via is a ListenerSet parent's Gateway; viaRestricted replaces it when hidden. */
export interface ParentSummary {
  ref: LinkedRef;
  sectionName?: string;
  port?: number;
  via?: LinkedRef;
  viaRestricted?: boolean;
  attachment: Attachment;
  conditionType?: string;
  reason?: string;
  message?: string;
  computed?: ComputedAttachment;
}
/** One row of the routes list; mirror is set only under a backend filter. */
export interface RouteRow {
  ref: LinkedRef;
  hostnames: string[];
  parents: ParentSummary[];
  parentRoutes: LinkedRef[];
  children: number;
  flags: string[];
  health: HealthState;
  mirror?: boolean;
}
/** One row of the Gateways list. */
export interface GatewayListRow {
  ref: LinkedRef;
  class: string;
  health: HealthState;
  listeners: number;
  attachedRoutes: number;
  notAccepted: number;
  listenerSets: number;
  addresses: string[];
  routes?: number;
  disagreements?: number;
}
/** One row of the ListenerSets list. */
export interface ListenerSetRow {
  ref: LinkedRef;
  parent: LinkedRef;
  health: HealthState;
  listeners: number;
  acceptedReason?: string;
  routes?: number;
  disagreements?: number;
}
export type RouteList = Page<RouteRow>;
export type GatewayList = Page<GatewayListRow>;
export type ListenerSetList = Page<ListenerSetRow>;

/** A status condition; stale marks one whose observedGeneration is below the object's generation. */
export interface Condition {
  type: string;
  status: string;
  reason: string;
  message: string;
  observedGeneration: number;
  lastTransitionTime: string;
  stale: boolean;
}
/** One status.parents entry of a route; ours marks one written by the install's controllers. */
export interface ParentStatus {
  parentRef: ParentRef;
  target: LinkedRef;
  controllerName: string;
  ours: boolean;
  conditions: Condition[];
}
/** Certificate refs always link none: Secrets are never read. */
export interface ListenerTLS {
  mode: string;
  certificateRefs: LinkedRef[];
}
export interface AllowedRoutes {
  namespaces: { from: string; selector?: LabelSelector };
  kinds: GroupKind[];
}
export interface ListenerStatus {
  attachedRoutes: number;
  supportedKinds: GroupKind[];
  conditions: Condition[];
}
/** One spec listener with its status block, when there is one. */
export interface Listener {
  name: string;
  port?: number;
  protocol: string;
  hostname?: string;
  tls?: ListenerTLS;
  allowedRoutes: AllowedRoutes;
  status?: ListenerStatus;
  computedRoutes?: number;
  computed?: Computed;
  isolationNote?: boolean;
}
/** Visible routes under a parent, per attachment. */
export interface RouteCounts {
  accepted: number;
  notAccepted: number;
  pending: number;
  unwatched: number;
}
/** Why an object the install does not own has a detail page; by is the owned object that names it. */
export interface Candidate {
  reason: "finding" | "referenced";
  by?: LinkedRef;
}
export interface GatewayDetail {
  install: string;
  ref: LinkedRef;
  generation: number;
  class: string;
  health: HealthState;
  candidate?: Candidate;
  ruleSet?: string;
  conditions: Condition[];
  addresses: string[];
  listeners: Listener[];
  attachedListenerSets?: number;
  /** The ListenerSets list's total for this caller; sizes the sub-table skeleton. */
  listenerSets?: number;
  routeCounts: RouteCounts;
  collectedAt: string;
}
/** The parent Gateway's spec.allowedListeners as written; crdHint is set whenever the field is absent. */
export interface AllowedListeners {
  set: boolean;
  value?: Record<string, unknown>;
  crdHint: boolean;
}
export interface ListenerSetDetail {
  install: string;
  ref: LinkedRef;
  generation: number;
  health: HealthState;
  candidate?: Candidate;
  ruleSet?: string;
  parent: LinkedRef;
  parentFound: boolean;
  conditions: Condition[];
  listeners: Listener[];
  experimentalHint?: string;
  routeCounts: RouteCounts;
  parentAllowedListeners?: AllowedListeners;
  collectedAt: string;
}
export interface PathMatch {
  type: string;
  value: string;
}
export interface KeyMatch {
  type: string;
  name: string;
  value: string;
}
export interface Match {
  path?: PathMatch;
  headers?: KeyMatch[];
  queryParams?: KeyMatch[];
  method?: string;
}
export interface Fraction {
  numerator: number;
  denominator: number;
}
export interface Mirror {
  backendRef: LinkedRef;
  port?: number;
  percent?: number;
  fraction?: Fraction;
}
/** One filter in written order; config is the filter as written. */
export interface Filter {
  type: string;
  extensionRef?: LinkedRef;
  mirror?: Mirror;
  config?: Record<string, unknown>;
}
/** A delegation ref; selected and attached count visible children of view B's rows (2.4), absent when unknown. */
export interface DelegationRef {
  mode: "name" | "wildcard" | "label";
  namespace: string;
  name?: string;
  label?: string;
  state?: "reported" | "not-reported" | "not-found" | "restricted";
  selected?: number;
  attached?: number;
  computed?: Computed;
}
/** A Backend's state and Accepted condition, or only restricted when the caller may not view it. */
export interface BackendState {
  state?: HealthState;
  accepted?: Condition;
  restricted?: boolean;
}
export interface BackendRef {
  ref: LinkedRef;
  port?: number;
  weight: number;
  weightDefaulted?: boolean;
  filters: Filter[];
  delegation?: DelegationRef;
  backend?: BackendState;
}
/** One spec rule. An explicit empty matches list is match-all, never "matches nothing". */
export interface Rule {
  index: number;
  name?: string;
  matches: Match[];
  filters: Filter[];
  backendRefs: BackendRef[];
}
/** One up-chain step: a ref, or restricted with no identity. */
export interface AncestorStep {
  ref?: LinkedRef;
  restricted?: boolean;
}
/** One up-chain path, root first. */
export interface AncestorPath {
  steps: AncestorStep[];
  cycle: boolean;
}
export interface RouteDetail {
  install: string;
  ref: LinkedRef;
  generation: number;
  health: HealthState;
  candidate?: Candidate;
  ruleSet?: string;
  hostnames: string[];
  annotations: { inheritParentMatcher?: boolean };
  parentRefs: SpecParentRef[];
  parents: ParentStatus[];
  attachments?: RouteAttachment[];
  rules: Rule[];
  ancestors: AncestorPath[];
  ancestorsTruncated: boolean;
  childCount: number;
  declaredCount: number;
  collectedAt: string;
}
