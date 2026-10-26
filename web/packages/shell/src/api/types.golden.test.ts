import badRequestGolden from "../../../../../pkg/api/testdata/golden/bad-request.json";
import bareInstalls from "../../../../../pkg/api/testdata/golden/bare-installs.json";
import bareMeta from "../../../../../pkg/api/testdata/golden/bare-meta.json";
import bareOverview from "../../../../../pkg/api/testdata/golden/bare-overview.json";
import gatewayDetailGolden from "../../../../../pkg/api/testdata/golden/gateway-detail.json";
import gatewayDetailRestrictedGolden from "../../../../../pkg/api/testdata/golden/gateway-detail-restricted.json";
import gatewaysGolden from "../../../../../pkg/api/testdata/golden/gateways.json";
import health from "../../../../../pkg/api/testdata/golden/health.json";
import installs from "../../../../../pkg/api/testdata/golden/installs.json";
import listenersetDetailGolden from "../../../../../pkg/api/testdata/golden/listenerset-detail.json";
import listenersetDetailAllowedGolden from "../../../../../pkg/api/testdata/golden/listenerset-detail-allowed.json";
import listenersetDetailInvalidGolden from "../../../../../pkg/api/testdata/golden/listenerset-detail-invalid.json";
import listenersetsGolden from "../../../../../pkg/api/testdata/golden/listenersets.json";
import meta from "../../../../../pkg/api/testdata/golden/meta.json";
import notFound from "../../../../../pkg/api/testdata/golden/not-found.json";
import overview from "../../../../../pkg/api/testdata/golden/overview.json";
import routeDetailGolden from "../../../../../pkg/api/testdata/golden/route-detail.json";
import routeDetailCandidateGolden from "../../../../../pkg/api/testdata/golden/route-detail-candidate.json";
import routeDetailRestrictedGolden from "../../../../../pkg/api/testdata/golden/route-detail-restricted.json";
import routesGolden from "../../../../../pkg/api/testdata/golden/routes.json";
import routesByBackendGolden from "../../../../../pkg/api/testdata/golden/routes-by-backend.json";
import routesByBackendRestrictedGolden from "../../../../../pkg/api/testdata/golden/routes-by-backend-restricted.json";
import routesByListenerGolden from "../../../../../pkg/api/testdata/golden/routes-by-listener.json";
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
  DelegationRef,
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
interface Shape {
  keys: Record<string, "!" | "?">;
  nested: Record<string, Shape>;
}
const shape = <T>(keys: Spec<T>, nested: Partial<Record<keyof T, Shape>> = {}): Shape => ({
  keys,
  nested: nested as Record<string, Shape>,
});

const ref = shape<Ref>({ group: "!", kind: "!", namespace: "?", name: "!" });
const linkedRef = shape<LinkedRef>({ group: "!", kind: "!", namespace: "?", name: "!", link: "!" });
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
);
const selector = shape<LabelSelector>({ matchLabels: "?", matchExpressions: "?" });
const scope = shape<ScopeSummary>(
  { mode: "!", selectors: "?", error: "?", source: "?", watched: "!", total: "!" },
  { selectors: selector },
);
const readiness = shape<Readiness>({ state: "!", ready: "?", desired: "?" });
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
  { readiness, scope },
);
const access = shape<Access>({ ok: "!", error: "?" });
const count = shape<Count>(
  { group: "!", kind: "!", category: "!", displayName: "!", count: "!", access: "!" },
  { access },
);
const row = shape<GatewayRow>(
  { ref: "!", class: "!", state: "!", listeners: "!", attachedRoutes: "!", addresses: "!", link: "?" },
  { ref },
);
const overviewShape = shape<OverviewResponse>(
  { header: "!", counts: "!", gateways: "!", collectedAt: "!" },
  { header, counts: count, gateways: row },
);
const kind = shape<ServedKind>({
  group: "!",
  kind: "!",
  resource: "!",
  versions: "!",
  namespaced: "!",
  category: "!",
  displayName: "!",
  version: "!",
});
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
const computed = shape<Computed>({ by: "!", rules: "!", rule: "!", state: "!", coverage: "?", reason: "?", note: "?" });
const computedAttachment = shape<ComputedAttachment>(
  { outcome: "?", listeners: "!", explanation: "?", computed: "!" },
  {
    listeners: shape<ListenerAttachment>(
      { resource: "!", listener: "!", effectiveHostnames: "?", computed: "!" },
      { resource: linkedRef, computed },
    ),
    computed,
  },
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
);
const listenerSetRow = shape<ListenerSetRow>(
  { ref: "!", parent: "!", health: "!", listeners: "!", acceptedReason: "?", routes: "?", disagreements: "?" },
  { ref: linkedRef, parent: linkedRef },
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
const candidate = shape<Candidate>({ reason: "!", by: "?" }, { by: linkedRef });
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
    delegation: shape<DelegationRef>({ mode: "!", namespace: "!", name: "?", label: "?", state: "?" }),
    backend: shape<BackendState>({ state: "?", accepted: "?", restricted: "?" }, { accepted: condition }),
  },
);
const rule = shape<Rule>(
  { index: "!", name: "?", matches: "!", filters: "!", backendRefs: "!" },
  { matches: match, filters: filter, backendRefs: backendRef },
);
const ancestor = shape<AncestorPath>(
  { steps: "!", cycle: "!" },
  { steps: shape<AncestorStep>({ ref: "?", restricted: "?" }, { ref: linkedRef }) },
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
    ancestors: ancestor,
  },
);

/** problems lists keys the types do not declare and required keys the wire omits, recursing into nested shapes. */
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
  }
  return out;
}

describe("types.ts matches the golden v1 wire format", () => {
  it.each([
    ["meta", meta, metaShape],
    ["bare-meta", bareMeta, metaShape],
    ["installs", installs, summary],
    ["bare-installs", bareInstalls, summary],
    ["overview", overview, overviewShape],
    ["bare-overview", bareOverview, overviewShape],
    ["health", health, healthReport],
    ["not-found", notFound, errorShape],
    ["routes", routesGolden, page(routeRow)],
    ["routes-filtered", routesFilteredGolden, page(routeRow)],
    ["routes-restricted", routesRestrictedGolden, page(routeRow)],
    ["routes-by-backend", routesByBackendGolden, page(routeRow)],
    ["routes-by-backend-restricted", routesByBackendRestrictedGolden, page(routeRow)],
    ["routes-by-listener", routesByListenerGolden, page(routeRow)],
    ["gateways", gatewaysGolden, page(gatewayListRow)],
    ["listenersets", listenersetsGolden, page(listenerSetRow)],
    ["bad-request", badRequestGolden, errorShape],
    ["gateway-detail", gatewayDetailGolden, gatewayDetail],
    ["gateway-detail-restricted", gatewayDetailRestrictedGolden, gatewayDetail],
    ["listenerset-detail", listenersetDetailGolden, listenerSetDetail],
    ["listenerset-detail-allowed", listenersetDetailAllowedGolden, listenerSetDetail],
    ["listenerset-detail-invalid", listenersetDetailInvalidGolden, listenerSetDetail],
    ["route-detail", routeDetailGolden, routeDetail],
    ["route-detail-restricted", routeDetailRestrictedGolden, routeDetail],
    ["route-detail-candidate", routeDetailCandidateGolden, routeDetail],
  ] as const)("%s", (_, golden, s) => {
    expect(problems(s, golden.body, "body")).toEqual([]);
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
    expect(routeDetailRestrictedGolden.body.ancestors.some((a) => a.steps.some((s) => "restricted" in s))).toBe(true);
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
});
