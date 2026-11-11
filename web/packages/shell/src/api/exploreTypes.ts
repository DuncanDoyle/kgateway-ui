import type { HealthState } from "../theme/tokens";
import type { DataPlane, ProcessSet, SuggestedFix } from "./diagTypes";
import type { LabelSelector, Ref, RowReason } from "./types";

/** Mirrors the explorer DTOs of pkg/api/explore_types.go as built (sub-phases 2.2 and 2.3, design §7.3). */

/** How a reference may be opened; rendered as given, never derived from a kind (design §7.1). */
export type Link = "detail" | "yaml" | "none";
/** A reference with its server-computed link; the link sits flat beside the ref fields. */
export interface LinkedRef extends Ref {
  link: Link;
}
/** GET …/refs: the trail's objects with their links, in request order (design §17.6). */
export interface TrailRefs {
  install: string;
  refs: LinkedRef[];
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
  /** The rule set's display name, e.g. "kgateway 2.4"; absent from older servers. */
  label?: string;
  /** Absent on a restricted fact: the deciding rule can depend on hidden objects. */
  rule?: string;
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
  reason?: RowReason;
  mirror?: boolean;
}
/** One row of the Gateways list. */
export interface GatewayListRow {
  ref: LinkedRef;
  class: string;
  health: HealthState;
  reason?: RowReason;
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
  reason?: RowReason;
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
  reason: "finding" | "referenced" | "selected";
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
  /** ruleSet's display name; absent from older servers. */
  ruleSetLabel?: string;
  conditions: Condition[];
  addresses: string[];
  listeners: Listener[];
  attachedListenerSets?: number;
  /** The ListenerSets list's total for this caller; sizes the sub-table skeleton. */
  listenerSets?: number;
  routeCounts: RouteCounts;
  /** Sent for a Gateway the install owns, not for a candidate. */
  dataPlane?: DataPlane;
  proxies?: ProcessSet;
  collectedAt: string;
}
/**
 * The parent Gateway's spec.allowedListeners as written; crdHint is set whenever the field is absent.
 * crdField says whether the watched Gateway CRD defines the field, when its channel and bundle tell.
 */
export interface AllowedListeners {
  set: boolean;
  value?: Record<string, unknown>;
  crdHint: boolean;
  crdField?: "defined" | "missing";
  crdChannel?: string;
  crdBundleVersion?: string;
}
export interface ListenerSetDetail {
  install: string;
  ref: LinkedRef;
  generation: number;
  health: HealthState;
  candidate?: Candidate;
  ruleSet?: string;
  /** ruleSet's display name; absent from older servers. */
  ruleSetLabel?: string;
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
/** One up-chain step: a ref, or restricted with no identity. A hidden ancestor route has link none and the matches it delegated under (D-024). */
export interface AncestorStep {
  ref?: LinkedRef;
  matches?: Match[];
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
  /** ruleSet's display name; absent from older servers. */
  ruleSetLabel?: string;
  hostnames: string[];
  annotations: { inheritParentMatcher?: boolean };
  parentRefs: SpecParentRef[];
  parents: ParentStatus[];
  attachments?: RouteAttachment[];
  rules: Rule[];
  effective?: EffectiveEdge[];
  effectiveTruncated?: boolean;
  ancestors: AncestorPath[];
  ancestorsTruncated: boolean;
  childCount: number;
  declaredCount: number;
  collectedAt: string;
}

// Views B and C (2.4). Task 66 renders them.
/** One rule under a parent match: kept with its effective matches, or dropped with the failing clause. */
export interface EffectiveRule {
  index: number;
  kept: boolean;
  matches: Match[];
  reason?: string;
}
/** A route's rules under one parent match, or only restricted. */
export interface EffectiveContext {
  restricted?: boolean;
  parentMatch?: Match;
  rules?: EffectiveRule[];
}
/** View C for one parent edge; contexts is [] when none was kept. */
export interface EffectiveEdge {
  parent: LinkedRef;
  mode: "default" | "inherit";
  contextsTruncated?: boolean;
  contexts: EffectiveContext[];
  computed: Computed;
}
/** A route on a context's path and the match it delegated under. */
export interface PathStep {
  ref: LinkedRef;
  match: Match;
}
/** One retained context of a delegation row, or only restricted. */
export interface DelegationContext {
  restricted?: boolean;
  parentMatch?: Match;
  path?: PathStep[];
  outcome?: DelegationOutcome;
  reason?: string;
}
export type DelegationOutcome =
  | "attached"
  | "partial"
  | "dropped"
  | "forbidden"
  | "rejected-hostnames"
  | "cycle"
  | "unresolved"
  | "unknown";
/** One view B row of GET …/delegation; child is absent when the ref selected no route. */
export interface DelegationRow {
  rule: number;
  ref: number;
  refAsWritten: DelegationRef;
  child?: LinkedRef;
  allows?: "any" | "this" | "other";
  outcome?: DelegationOutcome;
  reason?: string;
  contexts: DelegationContext[];
  contextsTruncated?: boolean;
  computed: Computed;
  observed: { entry?: ParentStatus; parentCondition?: Condition };
}

/** Mirrors the path DTOs of pkg/api/paths_types.go (sub-phase 6.2, design §4, §5, §9.5). */

/** A path row's listener; protocol and port are absent when the spec has no such listener. */
export interface PathListener {
  name: string;
  protocol?: string;
  port?: number;
}
/** A GRPCRouteMatch as written. */
export interface GRPCMatch {
  method?: { type?: string; service?: string; method?: string };
  headers?: KeyMatch[];
}
/** One existing signal on a path's chain; code is a finding code, condition type, capability state or view C reason. */
export interface PathSignal {
  kind: "gateway" | "listener" | "route-status" | "waiting" | "finding" | "delegation" | "coverage";
  code?: string;
  /** A finding signal's message, as the findings API shows it. */
  title?: string;
  ref?: LinkedRef;
  state: HealthState;
}
/** A path's worst state and its signals that are not healthy. */
export interface PathProblem {
  state: HealthState;
  signals: PathSignal[];
}
/** A backend port and its Service port name, when known. */
export interface BackendPort {
  port: number;
  name?: string;
}
/** The proxy process a per-proxy endpoints source reads (#35); never set yet. */
export interface EndpointsRuntime {
  install: string;
  gateway: LinkedRef;
  proxy: string;
  instance: string;
}
/** One endpoint of a backend detail read. */
export interface Endpoint {
  address: string;
  port?: number;
  ready: boolean;
  serving: boolean;
  terminating: boolean;
  pod?: string;
}
/** One backend port's endpoints from one source; ready and total only when the read establishes a count. */
export interface Endpoints {
  source: string;
  port?: BackendPort;
  runtime?: EndpointsRuntime;
  /** Meaningless when complete is "not-applicable": nothing was read. */
  readAt: string;
  /** A backend detail's scoped read may also be "timed-out" or "busy". */
  complete: "complete" | "partial" | "failed" | "unavailable" | "stale" | "not-applicable" | "timed-out" | "busy";
  ready?: number;
  total?: number;
  items?: Endpoint[];
}
/**
 * One path: Gateway → listener → hostnames → route chain → rule → backend.
 * matches applies to HTTPRoute rows, grpcMatches to GRPCRoute rows; TCPRoute and TLSRoute rows have neither.
 * matches is null on a delegated row whose view C match is unknown ("match unknown").
 */
export interface PathRow {
  gateway: LinkedRef;
  listenerSet?: LinkedRef;
  listener: PathListener;
  hostnames: string[];
  routes: LinkedRef[];
  delegated: boolean;
  rule: number;
  matches: Match[] | null;
  grpcMatches?: GRPCMatch[];
  action: "backend" | "redirect" | "direct-response" | "none" | "delegate";
  delegation?: DelegationRef;
  backend?: LinkedRef;
  port?: number;
  weight: number;
  share?: number;
  shareState: "shown" | "not-shown" | "no-traffic" | "failing" | "may-fail" | "mirror";
  mirror: boolean;
  mirrorPercent?: number;
  problem: PathProblem;
  endpoints?: Endpoints;
  note?: "unknown" | "unconfirmable" | "not-computed";
}
/** Counts of filtered visible paths and the distinct routes, Gateways and backends they reach. */
export interface PathCounts {
  paths: number;
  routes: number;
  gateways: number;
  backends: number;
}
/** The whole filtered result the caller may see; hidden says some paths were left out. */
export interface PathSummary extends PathCounts {
  byState: Record<HealthState, PathCounts>;
  /** Why no path was computed: "no-attachments" when the install has no computed views. */
  notComputed?: "no-attachments";
  endpointsNotRead?: boolean;
  hidden?: boolean;
  unmemoised?: boolean;
  /** The Gateways and route namespaces of the visible rows, each counted with every filter but its own; absent from older servers. */
  facets?: PathFacets;
}
/** A Gateway and the paths through it. */
export interface GatewayFacet {
  ref: LinkedRef;
  paths: number;
}
/** A route namespace and the paths with a route in it. */
export interface NamespaceFacet {
  name: string;
  paths: number;
}
/** The gateway and namespace filter values, sorted by name. */
export interface PathFacets {
  gateways: GatewayFacet[];
  namespaces: NamespaceFacet[];
}
/** GET …/paths. */
export interface PathsResponse {
  install: string;
  rows: PathRow[];
  next?: string;
  prev?: string;
  summary: PathSummary;
  collectedAt: string;
}

/** Mirrors the backend DTOs of pkg/api/backends_types.go (sub-phase 6.3, design §8, §9). */

/** One backend of the Backends list; counts cover only what the caller may see. */
export interface BackendRow {
  ref: LinkedRef;
  /** "Service", "Backend · <spec.type>" or the ref's kind. */
  kind: string;
  /** One entry per referenced Service port, or one not-applicable entry for a Backend. */
  endpoints: Endpoints[];
  routes: number;
  gateways: number;
  consumers: number;
  /** Paths on an attachment the route's status accepts. */
  confirmed: number;
  problem: PathProblem;
}
/** GET …/backends; hidden says a backend, path, route or consumer was left out. */
export interface BackendsResponse {
  install: string;
  rows: BackendRow[];
  next?: string;
  prev?: string;
  hidden?: boolean;
  /** The Gateways and namespaces of the visible backends, each counted with every filter but its own. */
  facets?: BackendFacets;
  collectedAt: string;
}
/** A Gateway and the backends it has a visible path to. */
export interface BackendGatewayFacet {
  ref: LinkedRef;
  backends: number;
}
/** A namespace and the backends in it. */
export interface BackendNamespaceFacet {
  name: string;
  backends: number;
}
/** The gateway and namespace filter values of the Backends list, sorted by name. */
export interface BackendFacets {
  gateways: BackendGatewayFacet[];
  namespaces: BackendNamespaceFacet[];
}
/** A non-route object that uses a backend, such as a GatewayExtension. */
export interface BackendConsumer {
  consumer: LinkedRef;
  role: string;
  field: string;
  port?: number;
}
/** One referenced port's chain as written and observed; named targetPorts stay unresolved. */
export interface PortChain {
  refPort: number;
  servicePort?: number;
  servicePortName?: string;
  targetPort?: string;
  targetName?: string;
  slicePorts: number[];
  noSuchPort?: boolean;
  /** Set only when a complete read found no slice port for the name. */
  noSlicePort?: boolean;
}
/** What the cache keeps of a Service. */
export interface ServiceFact {
  type: string;
  ports: BackendPort[];
}
/** One route ref to the backend that does not resolve; ref is -1 for a rule filter's mirror, and path tells them apart. */
export interface RefExplanation {
  route: LinkedRef;
  rule: number;
  ref: number;
  /** Where the route writes the ref, e.g. spec.rules[0].backendRefs[0]. */
  path: string;
  kind: "attributed" | "candidate" | "waiting";
  reason?: string;
  explanation?: string;
  fix?: SuggestedFix;
}
/** GET …/backends/{group}/{kind}/{ns}/{name}; status is the backend's own signals. */
export interface BackendDetail {
  install: string;
  ref: LinkedRef;
  kind: string;
  type?: string;
  problem: PathProblem;
  status: PathSignal[];
  exposure: PathsResponse;
  consumers: BackendConsumer[];
  sources: Endpoints[];
  portChain: PortChain[];
  explanations: RefExplanation[];
  service?: ServiceFact;
  conditions?: Condition[];
  collectedAt: string;
}
