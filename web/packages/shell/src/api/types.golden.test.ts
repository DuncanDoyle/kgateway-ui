import bareInstalls from "../../../../../pkg/api/testdata/golden/bare-installs.json";
import bareMeta from "../../../../../pkg/api/testdata/golden/bare-meta.json";
import bareOverview from "../../../../../pkg/api/testdata/golden/bare-overview.json";
import health from "../../../../../pkg/api/testdata/golden/health.json";
import installs from "../../../../../pkg/api/testdata/golden/installs.json";
import meta from "../../../../../pkg/api/testdata/golden/meta.json";
import notFound from "../../../../../pkg/api/testdata/golden/not-found.json";
import overview from "../../../../../pkg/api/testdata/golden/overview.json";
import type {
  Access,
  CheckResult,
  Count,
  ErrorResponse,
  Finding,
  GatewayRow,
  HealthReport,
  InstallHeader,
  InstallSummary,
  LabelSelector,
  LinkedRef,
  MetaResponse,
  OverviewResponse,
  Readiness,
  Ref,
  ScopeSummary,
  ServedKind,
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
const linkedRef = shape<LinkedRef>({ group: "!", kind: "!", namespace: "?", name: "!", link: "?" });
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
const metaShape = shape<MetaResponse>(
  { version: "!", pinned: "?", synced: "!", platform: "!", capabilities: "!", kinds: "!" },
  { kinds: kind },
);
const errorShape = shape<ErrorResponse>({ error: "!" });

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
  ] as const)("%s", (_, golden, s) => {
    expect(problems(s, golden.body, "body")).toEqual([]);
  });

  // Guards against a vacuous pass: the fixtures carry the optional fields the SPA branches on.
  it("covers restricted findings, omitted install fields and confirmedAt", () => {
    expect(health.body.findings.some((f) => "restricted" in f && f.restricted)).toBe(true);
    const bare = bareInstalls.body[0];
    expect(bare && !("scope" in bare) && !("namespace" in bare) && !("ready" in bare.readiness)).toBe(true);
    expect(installs.body.every((s) => "confirmedAt" in s)).toBe(true);
  });
});
