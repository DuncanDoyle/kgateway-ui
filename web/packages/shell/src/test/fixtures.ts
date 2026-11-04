import type {
  Category,
  Diagnostics,
  GroupRow,
  HealthReport,
  InstallSummary,
  MetaResponse,
  OverviewResponse,
  ServedKind,
} from "../api/types";

// Builders take the collection time, so timestamps follow the test clock and are never fixed at import time.

/** A served kind with the wire fields the UI does not read filled in. */
export function kind(group: string, k: string, category: Category, displayName: string): ServedKind {
  return {
    group,
    kind: k,
    category,
    displayName,
    resource: `${k.toLowerCase()}s`,
    versions: ["v1"],
    namespaced: k !== "GatewayClass",
    version: "v1",
  };
}

/** Kinds the resource endpoint serves in these fixtures. */
export const meta: MetaResponse = {
  version: "dev",
  synced: true,
  platform: "Kubernetes",
  capabilities: ["scope", "workloads"],
  kinds: [
    kind("gateway.networking.k8s.io", "GatewayClass", "gatewayclass", "GatewayClasses"),
    kind("gateway.networking.k8s.io", "Gateway", "gateway", "Gateways"),
    kind("gateway.networking.k8s.io", "HTTPRoute", "route", "HTTPRoutes"),
    kind("gateway.kgateway.dev", "TrafficPolicy", "policy", "TrafficPolicies"),
    kind("gateway.kgateway.dev", "GatewayParameters", "parameters", "GatewayParameters"),
    kind("apps", "Deployment", "workload", "Deployments"),
  ],
};

const gw = "gateway.networking.k8s.io";

export function overview(at = Date.now()): OverviewResponse {
  return {
    header: {
      id: "kgw.kgateway",
      displayName: "kgw/kgateway",
      namespace: "kgw",
      name: "kgateway",
      version: "2.4.7",
      edition: "kgateway",
      controllerNames: ["kgateway.dev/kgateway"],
      readiness: { state: "ready", ready: 2, desired: 2 },
      scope: {
        mode: "selectors",
        selectors: [{ matchLabels: { team: "a" } }],
        source: "pod kgateway-abc",
        watched: 12,
        total: 40,
      },
    },
    counts: [
      {
        group: gw,
        kind: "GatewayClass",
        category: "gatewayclass",
        displayName: "GatewayClasses",
        count: 1,
        access: { ok: true },
      },
      { group: gw, kind: "Gateway", category: "gateway", displayName: "Gateways", count: 3, access: { ok: true } },
      { group: gw, kind: "HTTPRoute", category: "route", displayName: "HTTPRoutes", count: 142, access: { ok: true } },
      {
        group: "gateway.kgateway.dev",
        kind: "TrafficPolicy",
        category: "policy",
        displayName: "TrafficPolicies",
        count: 0,
        access: { ok: false, error: "forbidden" },
      },
    ],
    gateways: [
      {
        ref: { group: gw, kind: "Gateway", namespace: "prod", name: "edge" },
        link: "detail",
        class: "kgateway",
        state: "error",
        listeners: 2,
        attachedRoutes: 98,
        addresses: ["34.12.8.1"],
        dataPlane: "rejects",
      },
      {
        ref: { group: gw, kind: "Gateway", namespace: "prod", name: "internal" },
        link: "detail",
        class: "kgateway",
        state: "healthy",
        listeners: 1,
        attachedRoutes: 40,
        addresses: ["10.0.4.7"],
        dataPlane: "ok",
      },
    ],
    collectedAt: new Date(at).toISOString(),
  };
}

const f = (code: string, severity: "error" | "warning" | "info", ns: string, name: string, message: string) => ({
  code,
  slug: code,
  severity,
  install: "kgw.kgateway",
  message,
  resource: { group: gw, kind: "HTTPRoute", namespace: ns, name, link: "yaml" as const },
});

export function unhealthy(at = Date.now()): HealthReport {
  const t = new Date(at).toISOString();
  return {
    install: "kgw.kgateway",
    state: "error",
    findings: [
      {
        ...f("KGWUI-001", "error", "prod", "edge", "Configuration rejected by Envoy"),
        detail: "2 active rejects",
        resource: { group: gw, kind: "Gateway", namespace: "prod", name: "edge", link: "yaml" },
      },
      ...Array.from({ length: 6 }, (_, i) => f("KGWUI-002", "warning", "shop", `route-${i}`, "Backend not found")),
    ],
    checks: [{ name: "xDS rejects", ok: true, required: true, collectedAt: t }],
    counts: { error: 1, warning: 6 },
    collectedAt: t,
  };
}

export function healthy(at = Date.now()): HealthReport {
  const t = new Date(at).toISOString();
  return {
    install: "kgw.kgateway",
    state: "healthy",
    findings: [],
    counts: {},
    collectedAt: t,
    checks: [
      { name: "resource status", ok: true, required: true, collectedAt: t },
      { name: "xDS rejects", ok: true, required: true, collectedAt: t },
    ],
  };
}

export function unknown(at = Date.now()): HealthReport {
  const t = new Date(at).toISOString();
  return {
    install: "kgw.kgateway",
    state: "unknown",
    counts: { info: 1 },
    collectedAt: t,
    findings: [
      {
        code: "KGWUI-008",
        slug: "signal-unavailable",
        severity: "info",
        install: "kgw.kgateway",
        message: "Could not check xDS rejects",
        detail: "connection refused",
      },
    ],
    checks: [{ name: "xDS rejects", ok: false, required: true, collectedAt: t, error: "connection refused" }],
  };
}

/** Resources inside the grace period: unknown, "being processed" (design §5.3). */
export function processing(at = Date.now()): HealthReport {
  const t = new Date(at).toISOString();
  return {
    install: "kgw.kgateway",
    state: "unknown",
    counts: { info: 2 },
    collectedAt: t,
    findings: ["new-a", "new-b"].map((name) => ({
      code: "KGWUI-003",
      slug: "route-unattached",
      severity: "info" as const,
      install: "kgw.kgateway",
      message: "Being processed by the controller",
      resource: { group: gw, kind: "HTTPRoute", namespace: "shop", name, link: "yaml" },
    })),
    checks: [
      {
        name: "resource status",
        ok: false,
        required: true,
        pending: true,
        collectedAt: t,
        error: "2 resources are being processed by the controller",
      },
    ],
  };
}

/** A managed Gateway whose proxy is missing inside the grace period: unknown, no finding yet (D-017 §3). */
export function proxyPending(at = Date.now()): HealthReport {
  const t = new Date(at).toISOString();
  return {
    install: "kgw.kgateway",
    state: "unknown",
    counts: {},
    collectedAt: t,
    findings: [],
    checks: [
      { name: "resource status", ok: true, required: true, collectedAt: t },
      {
        name: "gateway proxies",
        ok: false,
        required: true,
        pending: true,
        collectedAt: t,
        error: "1 proxy not found yet",
      },
    ],
  };
}

/** A finding on a resource the caller may not view (D-013). */
export function restricted(at = Date.now()): HealthReport {
  return {
    ...unhealthy(at),
    findings: [
      {
        code: "KGWUI-002",
        slug: "condition-false",
        severity: "error",
        install: "kgw.kgateway",
        message: "",
        restricted: true,
        resource: { group: gw, kind: "HTTPRoute", namespace: "payments", name: "checkout", link: "none" },
      },
    ],
    counts: { error: 1 },
  };
}

/** The install summary that matches overview(). */
export function installSummary(
  at = Date.now(),
  times: { confirmedAt?: string; computedAt?: string } = {},
  groupRows = 0,
): InstallSummary {
  return {
    ...overview(at).header,
    health: { state: "error", counts: { error: 1, warning: 6 } },
    groupRows,
    computedAt: times.computedAt ?? new Date(at).toISOString(),
    ...(times.confirmedAt ? { confirmedAt: times.confirmedAt } : {}),
  };
}

/** A ranked group on a route; i names it and orders it. */
export function groupRow(i: number, extra: Partial<GroupRow> = {}): GroupRow {
  return {
    id: `g-${i}`,
    title: `Problem ${i}`,
    code: "KGWUI-013",
    severity: "warning",
    class: 3,
    cause: { group: gw, kind: "HTTPRoute", namespace: "shop", name: `route-${i}`, link: "detail" },
    members: 1,
    affected: [],
    affectedTotal: 0,
    ...extra,
  };
}

/**
 * The diagnostics that match a health fixture: its verdict and checks, with one group per error or warning
 * finding unless groups are given. The processes read healthy.
 */
export function diagnostics(h: HealthReport, groups?: GroupRow[]): Diagnostics {
  const gs =
    groups ??
    h.findings
      .filter((x) => x.severity !== "info")
      .map((x, i) =>
        groupRow(i, {
          title: x.message,
          code: x.code,
          severity: x.severity,
          cause: x.resource,
          restricted: x.restricted,
        }),
      );
  return {
    install: h.install,
    verdict: { state: h.state, counts: h.counts, problems: (h.counts.error ?? 0) + (h.counts.warning ?? 0) },
    groups: gs,
    groupTotal: gs.length,
    processes: {
      controller: { state: "healthy", observed: true, ready: 2, desired: 2, version: "2.4.7" },
      proxies: { state: "healthy", gateways: 2, ready: 2, rejecting: 0, skew: 0, restarting: 0, notObserved: 0 },
    },
    dataPlane: { gateways: 2, rejecting: 0 },
    checks: h.checks,
    collectedAt: h.collectedAt,
  };
}
