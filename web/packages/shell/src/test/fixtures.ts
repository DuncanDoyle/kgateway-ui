import type { Category, HealthReport, InstallSummary, MetaResponse, OverviewResponse, ServedKind } from "../api/types";

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
        class: "kgateway",
        state: "error",
        listeners: 2,
        attachedRoutes: 98,
        addresses: ["34.12.8.1"],
      },
      {
        ref: { group: gw, kind: "Gateway", namespace: "prod", name: "internal" },
        class: "kgateway",
        state: "healthy",
        listeners: 1,
        attachedRoutes: 40,
        addresses: ["10.0.4.7"],
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
  resource: { group: gw, kind: "HTTPRoute", namespace: ns, name },
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
        resource: { group: gw, kind: "Gateway", namespace: "prod", name: "edge" },
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
      resource: { group: gw, kind: "HTTPRoute", namespace: "shop", name },
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
        resource: { group: gw, kind: "HTTPRoute", namespace: "payments", name: "checkout" },
      },
    ],
    counts: { error: 1 },
  };
}

/** The install summary that matches overview(). */
export function installSummary(at = Date.now()): InstallSummary {
  return {
    ...overview(at).header,
    health: { state: "error", counts: { error: 1, warning: 6 } },
    computedAt: new Date(at).toISOString(),
  };
}
