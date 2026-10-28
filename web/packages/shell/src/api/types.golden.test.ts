import badRequestGolden from "../../../../../pkg/api/testdata/golden/bad-request.json";
import bareInstalls from "../../../../../pkg/api/testdata/golden/bare-installs.json";
import bareMeta from "../../../../../pkg/api/testdata/golden/bare-meta.json";
import bareOverview from "../../../../../pkg/api/testdata/golden/bare-overview.json";
import gatewayDetailGolden from "../../../../../pkg/api/testdata/golden/gateway-detail.json";
import gatewayDetailRestrictedGolden from "../../../../../pkg/api/testdata/golden/gateway-detail-restricted.json";
import gatewaysGolden from "../../../../../pkg/api/testdata/golden/gateways.json";
import health from "../../../../../pkg/api/testdata/golden/health.json";
import healthEvidenceRestrictedGolden from "../../../../../pkg/api/testdata/golden/health-evidence-restricted.json";
import installs from "../../../../../pkg/api/testdata/golden/installs.json";
import listenersetDetailGolden from "../../../../../pkg/api/testdata/golden/listenerset-detail.json";
import listenersetDetailAllowedGolden from "../../../../../pkg/api/testdata/golden/listenerset-detail-allowed.json";
import listenersetDetailInvalidGolden from "../../../../../pkg/api/testdata/golden/listenerset-detail-invalid.json";
import listenersetsGolden from "../../../../../pkg/api/testdata/golden/listenersets.json";
import meta from "../../../../../pkg/api/testdata/golden/meta.json";
import notFound from "../../../../../pkg/api/testdata/golden/not-found.json";
import overview from "../../../../../pkg/api/testdata/golden/overview.json";
import routeDelegationGolden from "../../../../../pkg/api/testdata/golden/route-delegation.json";
import routeDelegationCutGolden from "../../../../../pkg/api/testdata/golden/route-delegation-cut.json";
import routeDelegationRestrictedGolden from "../../../../../pkg/api/testdata/golden/route-delegation-restricted.json";
import routeDetailGolden from "../../../../../pkg/api/testdata/golden/route-detail.json";
import routeDetailCandidateGolden from "../../../../../pkg/api/testdata/golden/route-detail-candidate.json";
import routeDetailDelegationRestrictedGolden from "../../../../../pkg/api/testdata/golden/route-detail-delegation-restricted.json";
import routeDetailEffectiveGolden from "../../../../../pkg/api/testdata/golden/route-detail-effective.json";
import routeDetailRestrictedGolden from "../../../../../pkg/api/testdata/golden/route-detail-restricted.json";
import routeDetailSelectedGolden from "../../../../../pkg/api/testdata/golden/route-detail-selected.json";
import routesGolden from "../../../../../pkg/api/testdata/golden/routes.json";
import routesByBackendGolden from "../../../../../pkg/api/testdata/golden/routes-by-backend.json";
import routesByBackendRestrictedGolden from "../../../../../pkg/api/testdata/golden/routes-by-backend-restricted.json";
import routesByListenerGolden from "../../../../../pkg/api/testdata/golden/routes-by-listener.json";
import routesDroppedGolden from "../../../../../pkg/api/testdata/golden/routes-dropped.json";
import routesFilteredGolden from "../../../../../pkg/api/testdata/golden/routes-filtered.json";
import routesRestrictedGolden from "../../../../../pkg/api/testdata/golden/routes-restricted.json";
import type {
  Access,
  AllowedListeners,
  AllowedRoutes,
  AncestorPath,
  AncestorStep,
  BackendRef,
  BackendState,
  Candidate,
  CheckResult,
  Computed,
  ComputedAttachment,
  Condition,
  Count,
  DelegationContext,
  DelegationRef,
  DelegationRow,
  EffectiveContext,
  EffectiveEdge,
  EffectiveRule,
  ErrorResponse,
  Filter,
  Finding,
  Fraction,
  GatewayDetail,
  GatewayListRow,
  GatewayRow,
  GroupKind,
  HealthReport,
  InstallHeader,
  InstallSummary,
  KeyMatch,
  LabelSelector,
  LabelSelectorRequirement,
  LinkedRef,
  Listener,
  ListenerAttachment,
  ListenerSetDetail,
  ListenerSetRow,
  ListenerStatus,
  ListenerTLS,
  Match,
  MetaResponse,
  Mirror,
  OverviewResponse,
  Page,
  ParentRef,
  ParentStatus,
  ParentSummary,
  PathMatch,
  PathStep,
  Readiness,
  Ref,
  RouteAttachment,
  RouteCounts,
  RouteDetail,
  RouteRow,
  Rule,
  ScopeSummary,
  ServedKind,
  SpecParentRef,
} from "./types";

// Checks the backend's golden wire format (pkg/api golden_test.go) against these types.
// Spec<T> must list every key of T with its optionality, so tsc fails when types.ts drifts from a spec.
type Spec<T> = { [K in keyof T]-?: object extends Pick<T, K> ? "?" : "!" };
// Value types: each golden body must satisfy Wide<T>, so tsc checks scalars and arrays versus objects.
// JSON imports type strings as string, so Wide<T> widens unions and Enums<T> checks their values at run time.
type Wide<T> = T extends string
  ? string
  : T extends readonly (infer E)[]
    ? Wide<E>[]
    : T extends object
      ? { [K in keyof T]: Wide<T[K]> }
      : T;
type Lit<V> = V extends string ? (string extends V ? never : V) : never;
type Union<V> = Lit<NonNullable<V> extends readonly (infer E)[] ? E : NonNullable<V>>;
type EnumKey<T> = { [K in keyof T]-?: [Union<T[K]>] extends [never] ? never : K }[keyof T];
/** Every key of T typed as a string-literal union (or an array of one), with each of its members. */
type Enums<T> = { [K in EnumKey<T>]-?: Record<Union<T[K]>, 1> };
interface Shape {
  keys: Record<string, "!" | "?">;
  nested: Record<string, Shape>;
  enums: Record<string, Record<string, 1>>;
}
const shape = <T>(
  keys: Spec<T>,
  nested: Partial<Record<keyof T, Shape>> = {},
  ...[enums]: EnumKey<T> extends never ? [] : [Enums<T>]
): Shape => ({
  keys,
  nested: nested as Record<string, Shape>,
  enums: (enums ?? {}) as Record<string, Record<string, 1>>,
});

// The members of each union in types.ts; Record<U, 1> makes tsc require every member and nothing else.
const healthState = { healthy: 1, warning: 1, error: 1, unknown: 1 } as const;
const link = { link: { detail: 1, yaml: 1, none: 1 } } as const;
const category = {
  category: {
    gatewayclass: 1,
    gateway: 1,
    listenerset: 1,
    route: 1,
    policy: 1,
    workload: 1,
    parameters: 1,
    backend: 1,
  },
} as const;
const delegationRefEnums = {
  mode: { name: 1, wildcard: 1, label: 1 },
  state: { reported: 1, "not-reported": 1, "not-found": 1, restricted: 1 },
} as const;

const ref = shape<Ref>({ group: "!", kind: "!", namespace: "?", name: "!" });
const linkedRef = shape<LinkedRef>({ group: "!", kind: "!", namespace: "?", name: "!", link: "!" }, {}, link);
const finding = shape<Finding>(
  {
    code: "!",
    slug: "!",
    severity: "!",
    install: "!",
    resource: "?",
    message: "!",
    detail: "?",
    since: "?",
    restricted: "?",
    evidenceRestricted: "?",
  },
  { resource: linkedRef },
  { severity: { error: 1, warning: 1, info: 1 } },
);
const check = shape<CheckResult>({
  name: "!",
  ok: "!",
  required: "!",
  collectedAt: "!",
  error: "?",
  detail: "?",
  pending: "?",
});
const healthReport = shape<HealthReport>(
  { install: "!", state: "!", findings: "!", checks: "!", counts: "!", collectedAt: "!" },
  { findings: finding, checks: check },
  { state: healthState },
);
const selector = shape<LabelSelector>(
  { matchLabels: "?", matchExpressions: "?" },
  {
    matchExpressions: shape<LabelSelectorRequirement>(
      { key: "!", operator: "!", values: "?" },
      {},
      { operator: { In: 1, NotIn: 1, Exists: 1, DoesNotExist: 1 } },
    ),
  },
);
const scope = shape<ScopeSummary>(
  { mode: "!", selectors: "?", error: "?", source: "?", watched: "!", total: "!" },
  { selectors: selector },
  { mode: { all: 1, selectors: 1, unknown: 1 } },
);
const readiness = shape<Readiness>(
  { state: "!", ready: "?", desired: "?" },
  {},
  { state: { ready: 1, "not-ready": 1, unknown: 1, "not-observed": 1 } },
);
const headerKeys = {
  id: "!",
  displayName: "!",
  namespace: "?",
  name: "?",
  version: "!",
  edition: "!",
  controllerNames: "!",
  readiness: "!",
  scope: "?",
} as const;
const header = shape<InstallHeader>(headerKeys, { readiness, scope });
const summary = shape<InstallSummary>(
  { ...headerKeys, health: "!", computedAt: "!", confirmedAt: "?" },
  {
    readiness,
    scope,
    health: shape<InstallSummary["health"]>({ state: "!", counts: "!" }, {}, { state: healthState }),
  },
);
const access = shape<Access>({ ok: "!", error: "?" });
const count = shape<Count>(
  { group: "!", kind: "!", category: "!", displayName: "!", count: "!", access: "!" },
  { access },
  category,
);
const row = shape<GatewayRow>(
  { ref: "!", class: "!", state: "!", listeners: "!", attachedRoutes: "!", addresses: "!", link: "?" },
  { ref },
  { state: healthState, ...link },
);
const overviewShape = shape<OverviewResponse>(
  { header: "!", counts: "!", gateways: "!", collectedAt: "!" },
  { header, counts: count, gateways: row },
);
const kind = shape<ServedKind>(
  {
    group: "!",
    kind: "!",
    resource: "!",
    versions: "!",
    namespaced: "!",
    category: "!",
    displayName: "!",
    version: "!",
  },
  {},
  category,
);
const groupKind = shape<NonNullable<MetaResponse["views"]>[number]>({ group: "!", kind: "!" });
const metaShape = shape<MetaResponse>(
  { version: "!", pinned: "?", synced: "!", platform: "!", capabilities: "!", kinds: "!", views: "?", filters: "?" },
  { kinds: kind, views: groupKind },
);
const errorShape = shape<ErrorResponse>({ error: "!" });

// Explorer DTOs (sub-phase 2.2).
const pageKeys = {
  install: "!",
  items: "!",
  total: "!",
  next: "?",
  prev: "?",
  deniedInInstall: "?",
  collectedAt: "!",
} as const satisfies Spec<Page<unknown>>;
const page = (items: Shape) => shape<Page<unknown>>(pageKeys, { items });
// View A (sub-phase 2.3).
const computed = shape<Computed>(
  { by: "!", rules: "!", rule: "?", state: "!", coverage: "?", reason: "?", note: "?" },
  {},
  {
    by: { ui: 1 },
    state: { checked: 1, disagrees: 1, pending: 1, unknown: 1, restricted: 1 },
    coverage: { "count-agrees": 1, "status-compatible": 1, "not-verified": 1 },
  },
);
const computedAttachment = shape<ComputedAttachment>(
  { outcome: "?", listeners: "!", explanation: "?", computed: "!" },
  {
    listeners: shape<ListenerAttachment>(
      { resource: "!", listener: "!", effectiveHostnames: "?", computed: "!" },
      { resource: linkedRef, computed },
    ),
    computed,
  },
  { outcome: { accepted: 1, NotAllowedByListeners: 1, NoMatchingParent: 1, NoMatchingListenerHostname: 1 } },
);
const parentSummary = shape<ParentSummary>(
  {
    ref: "!",
    sectionName: "?",
    port: "?",
    via: "?",
    viaRestricted: "?",
    attachment: "!",
    conditionType: "?",
    reason: "?",
    message: "?",
    computed: "?",
  },
  { ref: linkedRef, via: linkedRef, computed: computedAttachment },
  { attachment: { accepted: 1, "not-accepted": 1, pending: 1, unwatched: 1, declared: 1 } },
);
const routeRow = shape<RouteRow>(
  {
    ref: "!",
    hostnames: "!",
    parents: "!",
    parentRoutes: "!",
    children: "!",
    flags: "!",
    health: "!",
    mirror: "?",
  },
  { ref: linkedRef, parents: parentSummary, parentRoutes: linkedRef },
  { health: healthState },
);
const gatewayListRow = shape<GatewayListRow>(
  {
    ref: "!",
    class: "!",
    health: "!",
    listeners: "!",
    attachedRoutes: "!",
    notAccepted: "!",
    listenerSets: "!",
    addresses: "!",
    routes: "?",
    disagreements: "?",
  },
  { ref: linkedRef },
  { health: healthState },
);
const listenerSetRow = shape<ListenerSetRow>(
  { ref: "!", parent: "!", health: "!", listeners: "!", acceptedReason: "?", routes: "?", disagreements: "?" },
  { ref: linkedRef, parent: linkedRef },
  { health: healthState },
);
const condition = shape<Condition>({
  type: "!",
  status: "!",
  reason: "!",
  message: "!",
  observedGeneration: "!",
  lastTransitionTime: "!",
  stale: "!",
});
const gk = shape<GroupKind>({ group: "!", kind: "!" });
const parentRef = shape<ParentRef>({
  group: "!",
  kind: "!",
  namespace: "!",
  name: "!",
  sectionName: "?",
  port: "?",
});
const specParentRef = shape<SpecParentRef>(
  { group: "!", kind: "!", namespace: "!", name: "!", sectionName: "?", port: "?", target: "?" },
  { target: linkedRef },
);
const parentStatus = shape<ParentStatus>(
  { parentRef: "!", target: "!", controllerName: "!", ours: "!", conditions: "!" },
  { parentRef, target: linkedRef, conditions: condition },
);
const tls = shape<ListenerTLS>({ mode: "!", certificateRefs: "!" }, { certificateRefs: linkedRef });
const allowedRoutes = shape<AllowedRoutes>(
  { namespaces: "!", kinds: "!" },
  { namespaces: shape<AllowedRoutes["namespaces"]>({ from: "!", selector: "?" }, { selector }), kinds: gk },
);
const listenerStatus = shape<ListenerStatus>(
  { attachedRoutes: "!", supportedKinds: "!", conditions: "!" },
  { supportedKinds: gk, conditions: condition },
);
const listener = shape<Listener>(
  {
    name: "!",
    port: "?",
    protocol: "!",
    hostname: "?",
    tls: "?",
    allowedRoutes: "!",
    status: "?",
    computedRoutes: "?",
    computed: "?",
    isolationNote: "?",
  },
  { tls, allowedRoutes, status: listenerStatus, computed },
);
const routeCounts = shape<RouteCounts>({ accepted: "!", notAccepted: "!", pending: "!", unwatched: "!" });
const candidate = shape<Candidate>(
  { reason: "!", by: "?" },
  { by: linkedRef },
  { reason: { finding: 1, referenced: 1, selected: 1 } },
);
const gatewayDetail = shape<GatewayDetail>(
  {
    install: "!",
    ref: "!",
    generation: "!",
    class: "!",
    health: "!",
    candidate: "?",
    ruleSet: "?",
    conditions: "!",
    addresses: "!",
    listeners: "!",
    attachedListenerSets: "?",
    listenerSets: "?",
    routeCounts: "!",
    collectedAt: "!",
  },
  { ref: linkedRef, candidate, conditions: condition, listeners: listener, routeCounts },
  { health: healthState },
);
// value is the Gateway's field as written, so its keys are not checked.
const allowedListeners = shape<AllowedListeners>({ set: "!", value: "?", crdHint: "!" });
const listenerSetDetail = shape<ListenerSetDetail>(
  {
    install: "!",
    ref: "!",
    generation: "!",
    health: "!",
    candidate: "?",
    ruleSet: "?",
    parent: "!",
    parentFound: "!",
    conditions: "!",
    listeners: "!",
    experimentalHint: "?",
    routeCounts: "!",
    parentAllowedListeners: "?",
    collectedAt: "!",
  },
  {
    ref: linkedRef,
    candidate,
    parent: linkedRef,
    conditions: condition,
    listeners: listener,
    routeCounts,
    parentAllowedListeners: allowedListeners,
  },
  { health: healthState },
);
const keyMatch = shape<KeyMatch>({ type: "!", name: "!", value: "!" });
const match = shape<Match>(
  { path: "?", headers: "?", queryParams: "?", method: "?" },
  { path: shape<PathMatch>({ type: "!", value: "!" }), headers: keyMatch, queryParams: keyMatch },
);
const mirror = shape<Mirror>(
  { backendRef: "!", port: "?", percent: "?", fraction: "?" },
  { backendRef: linkedRef, fraction: shape<Fraction>({ numerator: "!", denominator: "!" }) },
);
// config is the filter as written, so its keys are not checked.
const filter = shape<Filter>(
  { type: "!", extensionRef: "?", mirror: "?", config: "?" },
  { extensionRef: linkedRef, mirror },
);
const backendRef = shape<BackendRef>(
  { ref: "!", port: "?", weight: "!", weightDefaulted: "?", filters: "!", delegation: "?", backend: "?" },
  {
    ref: linkedRef,
    filters: filter,
    delegation: shape<DelegationRef>(
      { mode: "!", namespace: "!", name: "?", label: "?", state: "?", selected: "?", attached: "?", computed: "?" },
      { computed },
      delegationRefEnums,
    ),
    backend: shape<BackendState>(
      { state: "?", accepted: "?", restricted: "?" },
      { accepted: condition },
      { state: healthState },
    ),
  },
);
const rule = shape<Rule>(
  { index: "!", name: "?", matches: "!", filters: "!", backendRefs: "!" },
  { matches: match, filters: filter, backendRefs: backendRef },
);
const ancestor = shape<AncestorPath>(
  { steps: "!", cycle: "!" },
  { steps: shape<AncestorStep>({ ref: "?", matches: "?", restricted: "?" }, { ref: linkedRef, matches: match }) },
);
// Views B and C (sub-phase 2.4).
const effectiveEdge = shape<EffectiveEdge>(
  { parent: "!", mode: "!", contextsTruncated: "?", contexts: "!", computed: "!" },
  {
    parent: linkedRef,
    contexts: shape<EffectiveContext>(
      { restricted: "?", parentMatch: "?", rules: "?" },
      {
        parentMatch: match,
        rules: shape<EffectiveRule>({ index: "!", kept: "!", matches: "!", reason: "?" }, { matches: match }),
      },
    ),
    computed,
  },
  { mode: { default: 1, inherit: 1 } },
);
const outcomes = {
  attached: 1,
  partial: 1,
  dropped: 1,
  forbidden: 1,
  "rejected-hostnames": 1,
  cycle: 1,
  unresolved: 1,
  unknown: 1,
} as const;
const delegationRow = shape<DelegationRow>(
  {
    rule: "!",
    ref: "!",
    refAsWritten: "!",
    child: "?",
    allows: "?",
    outcome: "?",
    reason: "?",
    contexts: "!",
    contextsTruncated: "?",
    computed: "!",
    observed: "!",
  },
  {
    refAsWritten: shape<DelegationRef>(
      {
        mode: "!",
        namespace: "!",
        name: "?",
        label: "?",
        state: "?",
        selected: "?",
        attached: "?",
        computed: "?",
      },
      {},
      delegationRefEnums,
    ),
    child: linkedRef,
    contexts: shape<DelegationContext>(
      { restricted: "?", parentMatch: "?", path: "?", outcome: "?", reason: "?" },
      { parentMatch: match, path: shape<PathStep>({ ref: "!", match: "!" }, { ref: linkedRef, match }) },
      { outcome: outcomes },
    ),
    computed,
    observed: shape<DelegationRow["observed"]>(
      { entry: "?", parentCondition: "?" },
      { entry: parentStatus, parentCondition: condition },
    ),
  },
  { allows: { any: 1, this: 1, other: 1 }, outcome: outcomes },
);
const routeDetail = shape<RouteDetail>(
  {
    install: "!",
    ref: "!",
    generation: "!",
    health: "!",
    candidate: "?",
    ruleSet: "?",
    hostnames: "!",
    annotations: "!",
    parentRefs: "!",
    parents: "!",
    attachments: "?",
    rules: "!",
    effective: "?",
    effectiveTruncated: "?",
    ancestors: "!",
    ancestorsTruncated: "!",
    childCount: "!",
    declaredCount: "!",
    collectedAt: "!",
  },
  {
    ref: linkedRef,
    candidate,
    annotations: shape<RouteDetail["annotations"]>({ inheritParentMatcher: "?" }),
    parentRefs: specParentRef,
    parents: parentStatus,
    attachments: shape<RouteAttachment>(
      { parentRef: "!", index: "!", target: "!", result: "!" },
      { parentRef, target: linkedRef, result: computedAttachment },
    ),
    rules: rule,
    effective: effectiveEdge,
    ancestors: ancestor,
  },
  { health: healthState },
);

/** problems lists keys the types do not declare, required keys the wire omits and values outside a union, recursing. */
function problems(s: Shape, v: unknown, path: string): string[] {
  if (Array.isArray(v)) return v.flatMap((e, i) => problems(s, e, `${path}[${i}]`));
  if (v === null || typeof v !== "object") return [`${path}: want an object, got ${JSON.stringify(v)}`];
  const o = v as Record<string, unknown>;
  const out = Object.keys(o)
    .filter((k) => !(k in s.keys))
    .map((k) => `${path}.${k}: not in types.ts`);
  for (const [k, opt] of Object.entries(s.keys)) {
    if (!(k in o)) {
      if (opt === "!") out.push(`${path}.${k}: required in types.ts, absent on the wire`);
      continue;
    }
    const n = s.nested[k];
    if (n && o[k] !== null) out.push(...problems(n, o[k], `${path}.${k}`));
    const e = s.enums[k];
    if (e)
      for (const [i, x] of (Array.isArray(o[k]) ? o[k] : [o[k]]).entries())
        if (typeof x !== "string" || !(x in e))
          out.push(`${path}.${k}${Array.isArray(o[k]) ? `[${i}]` : ""}: ${JSON.stringify(x)} not in types.ts`);
  }
  return out;
}

describe("types.ts matches the golden v1 wire format", () => {
  // satisfies Wide<T> is the compile-time half of the guard.
  it.each([
    ["meta", meta.body satisfies Wide<MetaResponse>, metaShape],
    ["bare-meta", bareMeta.body satisfies Wide<MetaResponse>, metaShape],
    ["installs", installs.body satisfies Wide<InstallSummary[]>, summary],
    ["bare-installs", bareInstalls.body satisfies Wide<InstallSummary[]>, summary],
    ["overview", overview.body satisfies Wide<OverviewResponse>, overviewShape],
    ["bare-overview", bareOverview.body satisfies Wide<OverviewResponse>, overviewShape],
    ["health", health.body satisfies Wide<HealthReport>, healthReport],
    ["not-found", notFound.body satisfies Wide<ErrorResponse>, errorShape],
    ["routes", routesGolden.body satisfies Wide<Page<RouteRow>>, page(routeRow)],
    ["routes-filtered", routesFilteredGolden.body satisfies Wide<Page<RouteRow>>, page(routeRow)],
    ["routes-restricted", routesRestrictedGolden.body satisfies Wide<Page<RouteRow>>, page(routeRow)],
    ["routes-by-backend", routesByBackendGolden.body satisfies Wide<Page<RouteRow>>, page(routeRow)],
    [
      "routes-by-backend-restricted",
      routesByBackendRestrictedGolden.body satisfies Wide<Page<RouteRow>>,
      page(routeRow),
    ],
    ["routes-by-listener", routesByListenerGolden.body satisfies Wide<Page<RouteRow>>, page(routeRow)],
    ["gateways", gatewaysGolden.body satisfies Wide<Page<GatewayListRow>>, page(gatewayListRow)],
    ["listenersets", listenersetsGolden.body satisfies Wide<Page<ListenerSetRow>>, page(listenerSetRow)],
    ["bad-request", badRequestGolden.body satisfies Wide<ErrorResponse>, errorShape],
    ["gateway-detail", gatewayDetailGolden.body satisfies Wide<GatewayDetail>, gatewayDetail],
    ["gateway-detail-restricted", gatewayDetailRestrictedGolden.body satisfies Wide<GatewayDetail>, gatewayDetail],
    ["listenerset-detail", listenersetDetailGolden.body satisfies Wide<ListenerSetDetail>, listenerSetDetail],
    [
      "listenerset-detail-allowed",
      listenersetDetailAllowedGolden.body satisfies Wide<ListenerSetDetail>,
      listenerSetDetail,
    ],
    [
      "listenerset-detail-invalid",
      listenersetDetailInvalidGolden.body satisfies Wide<ListenerSetDetail>,
      listenerSetDetail,
    ],
    ["route-detail", routeDetailGolden.body satisfies Wide<RouteDetail>, routeDetail],
    ["route-detail-restricted", routeDetailRestrictedGolden.body satisfies Wide<RouteDetail>, routeDetail],
    ["route-detail-candidate", routeDetailCandidateGolden.body satisfies Wide<RouteDetail>, routeDetail],
    ["route-delegation", routeDelegationGolden.body satisfies Wide<Page<DelegationRow>>, page(delegationRow)],
    ["route-delegation-cut", routeDelegationCutGolden.body satisfies Wide<Page<DelegationRow>>, page(delegationRow)],
    [
      "route-delegation-restricted",
      routeDelegationRestrictedGolden.body satisfies Wide<Page<DelegationRow>>,
      page(delegationRow),
    ],
    ["routes-dropped", routesDroppedGolden.body satisfies Wide<Page<RouteRow>>, page(routeRow)],
    ["route-detail-effective", routeDetailEffectiveGolden.body satisfies Wide<RouteDetail>, routeDetail],
    ["route-detail-selected", routeDetailSelectedGolden.body satisfies Wide<RouteDetail>, routeDetail],
    [
      "route-detail-delegation-restricted",
      routeDetailDelegationRestrictedGolden.body satisfies Wide<RouteDetail>,
      routeDetail,
    ],
    ["health-evidence-restricted", healthEvidenceRestrictedGolden.body satisfies Wide<HealthReport>, healthReport],
  ] as const)("%s", (_, body, s) => {
    expect(problems(s, body, "body")).toEqual([]);
  });

  // Each mismatch class in a mutated copy fails the guard and names the JSON path.
  it("rejects a value outside a declared union and names its path", () => {
    const body = structuredClone(routeDelegationGolden.body);
    (body.items[0] as { computed: { state: string } }).computed.state = "bogus";
    expect(problems(page(delegationRow), body, "body")).toEqual([
      'body.items[0].computed.state: "bogus" not in types.ts',
    ]);
  });

  // Scalars and arrays versus objects fail at compile time: tsc names the property, and an unused directive fails too.
  it("rejects a scalar or an array versus object mismatch at compile time", () => {
    const d = routeDetailGolden.body;
    // @ts-expect-error a string where a number is declared
    const str = { ...d, generation: "1" } satisfies Wide<RouteDetail>;
    // @ts-expect-error a number where a string is declared
    const num = { ...d, install: 1 } satisfies Wide<RouteDetail>;
    // @ts-expect-error an object where an array is declared
    const obj = { ...d, rules: {} } satisfies Wide<RouteDetail>;
    // @ts-expect-error an array where an object is declared
    const arr = { ...d, ref: [] } satisfies Wide<RouteDetail>;
    expect([str, num, obj, arr]).toHaveLength(4);
  });

  // Guards against a vacuous pass: the fixtures carry the optional fields the SPA branches on.
  it("covers restricted findings, omitted install fields and confirmedAt", () => {
    expect(health.body.findings.some((f) => "restricted" in f && f.restricted)).toBe(true);
    const bare = bareInstalls.body[0];
    expect(bare && !("scope" in bare) && !("namespace" in bare) && !("ready" in bare.readiness)).toBe(true);
    expect(installs.body.every((s) => "confirmedAt" in s)).toBe(true);
    expect(meta.body.views?.length).toBeGreaterThan(0);
  });

  // The explorer fixtures carry each link value and the optional fields the pages branch on.
  it("covers every link, restricted placeholders, candidates and paging", () => {
    const all = JSON.stringify([
      routesGolden,
      routesRestrictedGolden,
      gatewayDetailGolden,
      routeDetailGolden,
      routeDetailRestrictedGolden,
    ]);
    for (const l of ["detail", "yaml", "none"]) expect(all).toContain(`"link":"${l}"`);
    expect(routesRestrictedGolden.body.deniedInInstall).toBeGreaterThan(0);
    expect(routesRestrictedGolden.body.items.some((r) => r.parents.some((p) => "viaRestricted" in p))).toBe(true);
    expect(routesFilteredGolden.body.next).toBeTruthy();
    expect(routesByBackendGolden.body.items.some((r) => "mirror" in r)).toBe(true);
    // A hidden ancestor route keeps its name with link none (D-024).
    expect(
      routeDetailRestrictedGolden.body.ancestors.some((a) =>
        a.steps.some((s) => "ref" in s && s.ref?.kind === "HTTPRoute" && s.ref.link === "none"),
      ),
    ).toBe(true);
    expect(routeDetailCandidateGolden.body.candidate.by).toBeDefined();
    expect(routeDetailGolden.body.parentRefs.every((p) => "target" in p)).toBe(true);
    expect(listenersetDetailAllowedGolden.body.parentAllowedListeners.set).toBe(true);
  });

  // View A: every computed label the fixtures carry uses a state and coverage ComputedBadge renders.
  it("covers view A labels: known states and coverage, restricted with no reason or note", () => {
    const labels: Computed[] = [];
    const walk = (v: unknown): void => {
      if (Array.isArray(v)) v.forEach(walk);
      else if (v && typeof v === "object") {
        for (const [k, x] of Object.entries(v)) {
          if (k === "computed" && x && typeof x === "object" && "by" in x) labels.push(x as Computed);
          walk(x);
        }
      }
    };
    walk([
      routesByListenerGolden.body,
      routesRestrictedGolden.body,
      gatewayDetailRestrictedGolden.body,
      gatewayDetailGolden.body,
      routeDetailGolden.body,
    ]);
    const states: Computed["state"][] = ["checked", "disagrees", "pending", "unknown", "restricted"];
    const coverage: NonNullable<Computed["coverage"]>[number][] = ["count-agrees", "status-compatible", "not-verified"];
    expect(new Set(labels.map((c) => c.state))).toEqual(new Set(states));
    expect(labels.some((c) => c.state === "unknown" && c.reason)).toBe(true);
    for (const c of labels) {
      expect(c.by).toBe("ui");
      expect(states).toContain(c.state);
      for (const v of c.coverage ?? []) expect(coverage).toContain(v);
      if (c.state === "restricted") expect([c.reason, c.note]).toEqual([undefined, undefined]);
    }
    expect(meta.body.filters?.routes).toContain("listener");
  });

  // Views B and C: the fixtures carry the shapes the Delegation tab and effective matches branch on.
  it("covers 2.4: outcomes, truncation, hidden ancestors, restricted rows and evidence", () => {
    const outcomes = new Set(
      [...routeDelegationGolden.body.items, ...routeDelegationCutGolden.body.items].map((r) => r.outcome),
    );
    for (const o of [
      "attached",
      "partial",
      "dropped",
      "forbidden",
      "rejected-hostnames",
      "cycle",
      "unresolved",
      "unknown",
    ])
      expect(outcomes).toContain(o);
    expect(routeDelegationCutGolden.body.items.some((r) => r.contextsTruncated && r.contexts.length === 0)).toBe(true);
    const edges = routeDetailEffectiveGolden.body.effective;
    expect(edges.every((e) => e.mode === "inherit")).toBe(true);
    expect(edges.some((e) => e.contextsTruncated && e.contexts.length > 0)).toBe(true);
    expect(edges.some((e) => e.contextsTruncated && e.contexts.length === 0)).toBe(true);
    expect(routeDetailGolden.body.effective.every((e) => e.mode === "default")).toBe(true);
    // A hidden ancestor route: name with link none and its delegated match (D-024).
    const rows = routeDelegationRestrictedGolden.body.items;
    expect(rows.some((r) => r.contexts.some((c) => c.path?.some((p) => p.ref.link === "none")))).toBe(true);
    expect(rows.some((r) => !("child" in r) && r.computed.state === "restricted" && r.contexts.length === 0)).toBe(
      true,
    );
    const detail = routeDetailDelegationRestrictedGolden.body;
    expect(detail.effective.some((e) => e.parent.link === "none" && e.computed.state === "checked")).toBe(true);
    expect(
      detail.effective.some((e) => e.computed.state === "restricted" && e.contexts.some((c) => "restricted" in c)),
    ).toBe(true);
    expect(detail.ancestors.some((a) => a.steps.some((s) => "restricted" in s))).toBe(true);
    expect(detail.ancestors.some((a) => a.steps.some((s) => "matches" in s && s.ref?.link === "none"))).toBe(true);
    expect(routesDroppedGolden.body.items.every((r) => r.flags.includes("dropped"))).toBe(true);
    expect(routeDetailSelectedGolden.body.candidate.reason).toBe("selected");
    const findings = healthEvidenceRestrictedGolden.body.findings;
    for (const code of ["KGWUI-011", "KGWUI-012"])
      expect(findings.some((f) => f.code === code && "evidenceRestricted" in f && f.evidenceRestricted)).toBe(true);
    expect(
      findings.some((f) => f.code === "KGWUI-012" && !("evidenceRestricted" in f) && f.detail?.includes("shop/hp")),
    ).toBe(true);
    expect(meta.body.filters?.delegation).toEqual(["rule", "ref", "outcome"]);
  });
});
