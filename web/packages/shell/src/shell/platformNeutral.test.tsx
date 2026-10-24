import { render, screen, waitFor, within } from "@testing-library/react";
import { SWRConfig } from "swr";
import type { InstallHeader, MetaResponse } from "../api/types";
import { defaultPages } from "../pages/defaultPages";
import { healthy, installSummary, meta, overview, unhealthy, unknown } from "../test/fixtures";
import { stubApi, testSWR } from "../test/render";
import { installPath } from "./paths";
import { Shell } from "./Shell";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function mount(path: string) {
  window.history.pushState({}, "", path);
  return render(
    <SWRConfig value={testSWR()}>
      <Shell pages={defaultPages} />
    </SWRConfig>,
  );
}

/** An install as a non-Kubernetes provider reports it: opaque ID, no namespace, name, scope or replica counts (D-016). */
function standalone(id = "acme~edge_1", displayName = "Edge one") {
  const header: InstallHeader = {
    id,
    displayName,
    version: "2.4.7",
    edition: "kgateway",
    controllerNames: [],
    readiness: { state: "not-observed" },
  };
  return {
    summary: {
      ...header,
      health: { state: "healthy" as const, counts: {} },
      computedAt: new Date().toISOString(),
    },
    ov: { ...overview(), header },
  };
}
const noCaps: MetaResponse = {
  ...meta,
  platform: "Test",
  capabilities: [],
  kinds: meta.kinds.filter((k) => k.kind !== "Deployment"),
};

const fetched = () => vi.mocked(fetch).mock.calls.map((c) => String(c[0]));

it("H1: an opaque ID survives the redirect, a reload and a deep link, and is never split", async () => {
  const { summary, ov } = standalone();
  stubApi({
    "/api/v1/installs": [summary],
    "/api/v1/meta": noCaps,
    "/api/v1/installs/acme~edge_1/overview": ov,
    "/api/v1/installs/acme~edge_1/health": {
      ...healthy(),
      install: summary.id,
    },
  });
  const first = mount("/");
  await screen.findByTestId("install-page");
  expect(window.location.pathname).toBe("/i/acme~edge_1");
  first.unmount();
  mount("/i/acme~edge_1/findings");
  await waitFor(() => expect(document.title).toBe("Findings: Edge one"));
  const installCalls = fetched().filter((u) => u.startsWith("/api/v1/installs/"));
  expect(installCalls.length).toBeGreaterThan(0);
  for (const u of installCalls) expect(u).toMatch(/^\/api\/v1\/installs\/acme~edge_1\/(overview|health)$/);
});

it("H1: a Kubernetes ID with a dot resolves, and the old two-segment URL is not an install", async () => {
  stubApi({
    "/api/v1/installs": [installSummary()],
    "/api/v1/meta": meta,
    "/api/v1/installs/kgw.kgateway/overview": overview(),
    "/api/v1/installs/kgw.kgateway/health": healthy(),
  });
  const view = mount("/i/kgw.kgateway");
  await screen.findByTestId("install-page");
  await waitFor(() => expect(screen.getByRole("banner")).toHaveTextContent("kgw/kgateway"));
  view.unmount();
  mount("/i/kgw/kgateway");
  expect(await screen.findByText("Page not found")).toBeInTheDocument();
  expect(fetched().some((u) => u.startsWith("/api/v1/installs/kgw/"))).toBe(false);
});

it("H1: an ID containing an escape sequence is decoded exactly once", async () => {
  const { summary, ov } = standalone("a%20b", "Edge pct");
  stubApi({
    "/api/v1/installs": [summary],
    "/api/v1/meta": noCaps,
    "/api/v1/installs/a%2520b/overview": ov,
    "/api/v1/installs/a%2520b/health": { ...healthy(), install: summary.id },
  });
  mount(installPath("a%20b"));
  await screen.findByTestId("install-page");
  await waitFor(() => expect(screen.getByRole("banner")).toHaveTextContent("Edge pct"));
  await waitFor(() => expect(document.title).toBe("Edge pct"));
});

it("H1: links encode the ID", () => {
  expect(installPath("a b", "findings")).toBe("/i/a%20b/findings");
  expect(installPath("acme~edge_1")).toBe("/i/acme~edge_1");
});

it("H2: an install without scope, namespace, name or replica counts renders everywhere", async () => {
  const one = standalone();
  const two = standalone("acme~edge_2", "Edge two");
  stubApi({
    "/api/v1/installs": [one.summary, two.summary],
    "/api/v1/meta": noCaps,
    "/api/v1/installs/acme~edge_1/overview": one.ov,
    "/api/v1/installs/acme~edge_1/health": {
      ...unknown(),
      install: one.summary.id,
      findings: unknown().findings.map((f) => ({
        ...f,
        install: one.summary.id,
      })),
    },
  });
  const landing = mount("/");
  const cards = await screen.findAllByTestId("install-card");
  expect(cards[0]).toHaveTextContent("Edge one");
  expect(cards[0]).toHaveAttribute("href", "/i/acme~edge_1");
  expect(cards[0]).not.toHaveTextContent(/namespaces/i);
  landing.unmount();
  mount("/i/acme~edge_1");
  await screen.findByTestId("install-page");
  expect(screen.queryByTestId("scope-tile")).toBeNull();
  expect(screen.queryByTestId("controller-readiness")).toBeNull();
  expect(await screen.findByRole("button", { name: "Switch install: Edge one" })).toBeInTheDocument();
  // The install-level finding names the install by its display name.
  await waitFor(() => expect(screen.getByTestId("finding-row")).toHaveTextContent("Edge one"));
});

it("the header readiness wording does not change when /meta arrives", async () => {
  stubApi({
    "/api/v1/installs": [installSummary()],
    "/api/v1/meta": meta,
    "/api/v1/installs/kgw.kgateway/overview": overview(),
    "/api/v1/installs/kgw.kgateway/health": healthy(),
  });
  // Hold /meta pending, so the header renders before capabilities are known.
  const answer = fetch;
  let release: () => void = () => {};
  const held = new Promise<void>((res) => (release = res));
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url === "/api/v1/meta") await held;
      return answer(url);
    }),
  );
  mount("/i/kgw.kgateway");
  const ready = await screen.findByTestId("controller-readiness");
  const before = ready.textContent;
  expect(before).toBe("Controller: 2 of 2 ready");
  release();
  await waitFor(() => expect(fetched()).toContain("/api/v1/meta"));
  await screen.findByRole("banner");
  expect(screen.getByTestId("controller-readiness").textContent).toBe(before);
});

it("H3: without the workloads capability there is no replica wording and no Deployment link", async () => {
  const s = {
    ...installSummary(),
    readiness: { state: "not-ready" as const },
  };
  const h = {
    ...unhealthy(),
    findings: [
      {
        code: "KGWUI-005",
        slug: "controller-not-ready",
        severity: "error" as const,
        install: "kgw.kgateway",
        // What the backend sends without workloads (Task 24): no replica counts.
        message: "Controller not ready",
        resource: {
          group: "apps",
          kind: "Deployment",
          namespace: "kgw",
          name: "kgateway",
          // Without workloads the backend serves no Deployment, so it sends no link.
          link: "none" as const,
        },
      },
      {
        code: "KGWUI-001",
        slug: "gateway-not-programmed",
        severity: "error" as const,
        install: "kgw.kgateway",
        message: "Gateway not programmed",
        resource: {
          group: "gateway.networking.k8s.io",
          kind: "Gateway",
          namespace: "prod",
          name: "edge",
          link: "detail" as const,
        },
      },
    ],
  };
  stubApi({
    "/api/v1/installs": [s],
    "/api/v1/meta": {
      ...meta,
      capabilities: ["scope"],
      kinds: meta.kinds.filter((k) => k.kind !== "Deployment"),
    },
    "/api/v1/installs/kgw.kgateway/overview": overview(),
    "/api/v1/installs/kgw.kgateway/health": h,
  });
  mount("/i/kgw.kgateway");
  // The Gateway finding links by its server-computed link; the Deployment's is none.
  const rows = await screen.findAllByTestId("finding-row");
  const gateway = rows.find((r) => r.textContent?.includes("Gateway not programmed")) as HTMLElement;
  await within(gateway).findByRole("link");
  const deployment = rows.find((r) => r.textContent?.includes("Controller not ready")) as HTMLElement;
  expect(within(deployment).queryByRole("link")).toBeNull();
  const ready = await screen.findByTestId("controller-readiness");
  expect(ready).toHaveTextContent("Controller: not ready");
  // No replica wording anywhere on the page, the header and finding rows included.
  expect(document.body).not.toHaveTextContent(/replica|of 2 ready/i);
});
