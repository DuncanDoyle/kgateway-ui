import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { POLL_MS } from "../../api/hooks";
import type { HealthReport, OverviewResponse } from "../../api/types";
import { healthy, meta, overview, processing, restricted, unhealthy, unknown } from "../../test/fixtures";
import { renderWithTheme, stubApi } from "../../test/render";
import { InstallPage } from "./InstallPage";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function Where() {
  const loc = useLocation();
  return <output data-testid="location">{loc.search}</output>;
}

function mount(health: HealthReport, path = "/i/kgw/kgateway", ov: OverviewResponse = overview()) {
  stubApi({
    "/api/v1/installs/kgw/kgateway/overview": ov,
    "/api/v1/installs/kgw/kgateway/health": health,
    "/api/v1/meta": meta,
  });
  return renderWithTheme(
    <>
      <Routes>
        <Route path="/i/:ns/:name" element={<InstallPage />} />
      </Routes>
      <Where />
    </>,
    { path },
  );
}

function GoTo({ to }: { to: string }) {
  const navigate = useNavigate();
  return (
    <button type="button" onClick={() => navigate(to)}>
      go
    </button>
  );
}

function gatewayRows() {
  return within(screen.getByRole("table", { name: "Gateways" }))
    .getAllByRole("row")
    .slice(1);
}

it("unhealthy: strip with counts, top 5 findings, show all", async () => {
  mount(unhealthy());
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("1 error, 6 warnings");
  expect(screen.getAllByTestId("finding-row")).toHaveLength(5);
  expect(screen.getByRole("link", { name: "Show all (7)" })).toHaveAttribute("href", "/i/kgw/kgateway/findings");
  const first = screen.getAllByTestId("finding-row")[0] as HTMLElement;
  expect(first).toHaveTextContent("Configuration rejected by Envoy");
  expect(first).toHaveTextContent("2 active rejects");
  expect(first).toHaveTextContent("KGWUI-001");
});

it("healthy: slim strip and what was checked", async () => {
  mount(healthy());
  expect(await screen.findByTestId("health-strip")).toHaveTextContent("All healthy");
  const checked = screen.getByRole("list", { name: "What was checked" });
  expect(within(checked).getByText("xDS rejects")).toBeInTheDocument();
});

it("what was checked shows a check's detail", async () => {
  const h = healthy();
  h.checks.push({
    name: "controller scope",
    ok: true,
    required: true,
    collectedAt: h.collectedAt,
    detail: "scope from Deployment template; no controller Pod running",
  });
  mount(h);
  const checked = await screen.findByRole("list", { name: "What was checked" });
  expect(checked).toHaveTextContent("scope from Deployment template; no controller Pod running");
});

it("unknown: never green, names the missing signal", async () => {
  mount(unknown());
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("No known problems — xDS rejects could not be checked");
  expect(within(strip).getByTestId("health-icon-unknown")).toBeInTheDocument();
  expect(screen.getByTestId("finding-row")).toHaveTextContent("connection refused");
});

it("unknown with warnings counts the warnings instead of claiming no problems", async () => {
  const h = unknown();
  h.findings.push({ ...(unhealthy().findings[1] as HealthReport["findings"][number]) });
  h.counts = { info: 1, warning: 1 };
  mount(h);
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("1 warning — xDS rejects could not be checked");
  expect(strip).not.toHaveTextContent("No known problems");
});

it("served kinds changed: the strip names the check and the finding shows its detail", async () => {
  const t = new Date().toISOString();
  mount({
    install: "kgw/kgateway",
    state: "unknown",
    counts: { info: 1 },
    collectedAt: t,
    findings: [
      {
        code: "KGWUI-008",
        slug: "signal-unavailable",
        severity: "info",
        install: "kgw/kgateway",
        message: "Served kinds changed; restart the UI to watch them",
        detail: "added TLSRoute",
      },
    ],
    checks: [
      {
        name: "served kinds",
        ok: false,
        required: true,
        collectedAt: t,
        error: "served kinds changed: added TLSRoute",
      },
    ],
  });
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("No known problems — served kinds could not be checked");
  const row = screen.getByTestId("finding-row");
  expect(row).toHaveTextContent("Served kinds changed; restart the UI to watch them");
  expect(row).toHaveTextContent("added TLSRoute");
  expect(row).toHaveTextContent("kgw/kgateway"); // no resource: the install stands in
});

// The strip and the gateway rows age into stale on the shared clock, without new data.
it("a healthy page turns stale as it ages while identical responses keep arriving", async () => {
  vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
  const base = Date.parse("2026-10-19T12:00:00Z");
  vi.setSystemTime(base);
  mount(healthy(base), "/i/kgw/kgateway", overview(base));
  // vi.waitFor polls on real timers, so it works while setInterval is faked.
  await vi.waitFor(() => expect(screen.getByTestId("health-strip")).toHaveTextContent("All healthy"));
  for (const r of gatewayRows()) expect(r).not.toHaveTextContent("Unknown");
  vi.setSystemTime(base + 2 * POLL_MS + 1000);
  act(() => {
    vi.advanceTimersByTime(1000);
  });
  expect(screen.getByTestId("health-strip")).toHaveTextContent(/Data is stale/);
  for (const r of gatewayRows()) expect(r).toHaveTextContent("Unknown");
});

it("being processed: unknown, never green, and says what is happening", async () => {
  mount(processing());
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("No known problems — 2 resources are being processed by the controller");
  expect(within(strip).getByTestId("health-icon-unknown")).toBeInTheDocument();
  expect(strip).not.toHaveTextContent("could not be checked");
});

it("stale data turns the strip and every gateway row unknown", async () => {
  mount(healthy(Date.now() - 60_000));
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent(/Data is stale/);
  expect(within(strip).getByTestId("health-icon-unknown")).toBeInTheDocument();
  // No green survives on stale data, the "What was checked" list included (D-006).
  expect(screen.queryAllByTestId("health-icon-healthy")).toHaveLength(0);
  for (const r of gatewayRows()) {
    expect(r).toHaveTextContent("Unknown");
    expect(within(r).queryByTestId("health-icon-healthy")).toBeNull();
    expect(within(r).queryByTestId("health-icon-error")).toBeNull();
  }
});

it("stale overview alone also turns the page unknown", async () => {
  mount(healthy(), "/i/kgw/kgateway", overview(Date.now() - 75_000));
  // The age shown is that of the older response.
  expect(await screen.findByTestId("health-strip")).toHaveTextContent("Data is stale — last checked 1m ago");
});

it("count tiles, GatewayClass tile first, no-access tile and scope tile", async () => {
  mount(healthy());
  expect(await screen.findByText("142")).toBeInTheDocument();
  const tiles = screen.getAllByTestId("count-tile");
  expect(tiles[0]).toHaveTextContent("GatewayClasses");
  expect(screen.getByText("No access")).toBeInTheDocument();
  const scope = screen.getByTestId("scope-tile");
  expect(scope).toHaveTextContent("12 / 40");
  expect(scope).toHaveTextContent("Namespaces watched");
  expect(scope).not.toHaveTextContent("Deployment template");
});

it("scope tile says when the scope comes from the Deployment template", async () => {
  const ov = overview();
  ov.header.scope.source = "deployment template";
  mount(healthy(), "/i/kgw/kgateway", ov);
  expect(await screen.findByTestId("scope-tile")).toHaveTextContent("From the Deployment template");
});

it("scope unknown shows an unknown scope tile", async () => {
  const ov = overview();
  ov.header.scope = { mode: "unknown", error: "uses valueFrom", watched: 0, total: 40 };
  mount(healthy(), "/i/kgw/kgateway", ov);
  const scope = await screen.findByTestId("scope-tile");
  expect(scope).toHaveTextContent("Unknown");
  expect(within(scope).getByTestId("health-icon-unknown")).toBeInTheDocument();
  expect(scope).not.toHaveTextContent("0 / 40");
});

it("gateway filter matches name, namespace and class and lives in the URL", async () => {
  const ov = overview();
  ov.gateways.push({
    ref: { group: "gateway.networking.k8s.io", kind: "Gateway", namespace: "dev", name: "lab" },
    class: "waypoint",
    state: "healthy",
    listeners: 1,
    attachedRoutes: 0,
    addresses: [],
  });
  mount(healthy(), "/i/kgw/kgateway?q=internal", ov);
  const table = await screen.findByRole("table", { name: "Gateways" });
  expect(within(table).getAllByRole("row")).toHaveLength(2); // header + internal
  const filter = screen.getByRole("textbox", { name: "Filter gateways" });
  expect(filter).toHaveValue("internal");
  await userEvent.clear(filter);
  await userEvent.type(filter, "prod");
  expect(within(table).getAllByRole("row")).toHaveLength(3); // namespace matches both
  expect(screen.getByTestId("location")).toHaveTextContent("?q=prod");
  await userEvent.clear(filter);
  await userEvent.type(filter, "waypoint");
  expect(within(table).getAllByRole("row")).toHaveLength(2); // class match
  await userEvent.clear(filter);
  await userEvent.type(filter, "nomatch");
  expect(within(table).getAllByRole("row")).toHaveLength(1);
  await userEvent.clear(filter);
  expect(screen.getByTestId("location")).toBeEmptyDOMElement();
});

it("gateway sort lives in the URL", async () => {
  mount(healthy(), "/i/kgw/kgateway?sort=routes.asc");
  await screen.findByRole("table", { name: "Gateways" });
  expect(gatewayRows()[0]).toHaveTextContent("internal");
  await userEvent.click(screen.getByRole("button", { name: "Routes" }));
  expect(screen.getByTestId("location")).toHaveTextContent("sort=routes.desc");
  expect(gatewayRows()[0]).toHaveTextContent("edge");
});

it("gateway names link to their YAML view", async () => {
  mount(healthy());
  await screen.findByRole("table", { name: "Gateways" });
  expect(within(gatewayRows()[0] as HTMLElement).getByRole("link", { name: "prod/edge" })).toHaveAttribute(
    "href",
    "/i/kgw/kgateway/r/gateway.networking.k8s.io/Gateway/prod/edge",
  );
});

it("links only finding resources whose kind /api/v1/meta lists", async () => {
  stubApi({
    "/api/v1/installs/kgw/kgateway/overview": overview(),
    "/api/v1/installs/kgw/kgateway/health": unhealthy(),
    "/api/v1/meta": { ...meta, kinds: meta.kinds.filter((k) => k.kind !== "HTTPRoute") },
  });
  renderWithTheme(
    <Routes>
      <Route path="/i/:ns/:name" element={<InstallPage />} />
    </Routes>,
    { path: "/i/kgw/kgateway" },
  );
  const rows = await screen.findAllByTestId("finding-row");
  await waitFor(() =>
    expect(within(rows[0] as HTMLElement).getByRole("link")).toHaveAttribute(
      "href",
      "/i/kgw/kgateway/r/gateway.networking.k8s.io/Gateway/prod/edge",
    ),
  );
  expect(within(rows[1] as HTMLElement).queryByRole("link")).toBeNull(); // HTTPRoute is not listed
});

it("a restricted finding shows its resource, severity and code, a short note and no link", async () => {
  mount(restricted());
  const row = await screen.findByTestId("finding-row");
  expect(row).toHaveTextContent("payments/checkout");
  expect(row).toHaveTextContent("Error");
  expect(row).toHaveTextContent("KGWUI-002");
  expect(row).toHaveTextContent("Restricted");
  expect(within(row).queryByRole("link")).toBeNull();
});

it("switching installs never shows the previous install's data", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.startsWith("/api/v1/installs/b/two/")) return new Promise<Response>(() => {}); // install B never answers
      const body = url.endsWith("/overview") ? overview() : url.endsWith("/health") ? unhealthy() : meta;
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  renderWithTheme(
    <>
      <Routes>
        <Route path="/i/:ns/:name" element={<InstallPage />} />
      </Routes>
      <GoTo to="/i/b/two" />
    </>,
    { path: "/i/kgw/kgateway" },
  );
  expect(await screen.findByTestId("health-strip")).toHaveTextContent("1 error");
  await userEvent.click(screen.getByRole("button", { name: "go" }));
  await waitFor(() => expect(screen.queryByTestId("health-strip")).toBeNull());
  expect(screen.getByRole("status", { name: "Loading install" })).toHaveAttribute("aria-busy", "true");
  expect(screen.queryByRole("table", { name: "Gateways" })).toBeNull();
  expect(screen.queryAllByTestId("finding-row")).toHaveLength(0);
});

it("never renders a response that belongs to another install", async () => {
  const ov = overview();
  mount(healthy(), "/i/kgw/kgateway", { ...ov, header: { ...ov.header, id: "other/install" } });
  await waitFor(() => expect(screen.getByRole("status", { name: "Loading install" })).toBeInTheDocument());
  expect(screen.queryByTestId("health-strip")).toBeNull();
});

it("the loading skeleton reserves one tile per served kind plus the scope tile", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url === "/api/v1/meta" ? new Response(JSON.stringify(meta), { status: 200 }) : new Promise<Response>(() => {}),
    ),
  );
  renderWithTheme(
    <Routes>
      <Route path="/i/:ns/:name" element={<InstallPage />} />
    </Routes>,
    { path: "/i/kgw/kgateway" },
  );
  // meta lists 4 countable kinds (Deployment is not counted) + the scope tile.
  await waitFor(() => expect(screen.getAllByTestId("tile-shimmer")).toHaveLength(5));
});

it("gateway table lists gateways with status text", async () => {
  mount(healthy());
  const table = await screen.findByRole("table", { name: "Gateways" });
  const rows = within(table).getAllByRole("row");
  expect(rows).toHaveLength(3); // header + 2
  expect(rows[1]).toHaveTextContent("Error");
  expect(rows[1]).toHaveTextContent("edge");
});

it("API error shows a designed error state", async () => {
  stubApi({
    "/api/v1/installs/kgw/kgateway/overview": [404, { error: "install kgw/kgateway not found" }],
    "/api/v1/installs/kgw/kgateway/health": [404, { error: "install kgw/kgateway not found" }],
  });
  renderWithTheme(
    <Routes>
      <Route path="/i/:ns/:name" element={<InstallPage />} />
    </Routes>,
    { path: "/i/kgw/kgateway" },
  );
  await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("install kgw/kgateway not found"));
});
