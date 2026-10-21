import { css } from "@emotion/react";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { POLL_MS } from "../../api/hooks";
import type { HealthReport, InstallSummary, OverviewResponse, ScopeSummary } from "../../api/types";
import { InstallSwitcher } from "../../shell/InstallSwitcher";
import {
  healthy,
  installSummary,
  meta,
  overview,
  processing,
  proxyPending,
  restricted,
  unhealthy,
  unknown,
} from "../../test/fixtures";
import { renderWithTheme, stubApi } from "../../test/render";
import { InstallPage } from "./InstallPage";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function Where() {
  const loc = useLocation();
  return <output data-testid="location">{loc.search}</output>;
}

function mount(health: HealthReport, path = "/i/kgw.kgateway", ov: OverviewResponse = overview()) {
  stubApi({
    // Finding rows name the install from the installs list.
    "/api/v1/installs": [installSummary()],
    "/api/v1/installs/kgw.kgateway/overview": ov,
    "/api/v1/installs/kgw.kgateway/health": health,
    "/api/v1/meta": meta,
  });
  return renderWithTheme(
    <>
      <Routes>
        <Route path="/i/:id" element={<InstallPage />} />
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
  expect(screen.getByRole("link", { name: "Show all 7 findings" })).toHaveAttribute("href", "/i/kgw.kgateway/findings");
  const first = screen.getAllByTestId("finding-row")[0] as HTMLElement;
  expect(first).toHaveTextContent("Configuration rejected by Envoy");
  expect(first).toHaveTextContent("2 active rejects");
  expect(first).toHaveTextContent("KGWUI-001");
});

it("healthy: slim strip and what was checked", async () => {
  mount(healthy());
  expect(await screen.findByTestId("health-strip")).toHaveTextContent("All healthy");
  const checked = screen.getByRole("list", { name: "Checks" });
  expect(within(checked).getByText("xDS rejects")).toBeInTheDocument();
});

it("each check names its state in text, not only in the icon", async () => {
  const h = healthy();
  h.checks.push({ name: "gateway ownership", ok: false, required: false, collectedAt: h.collectedAt });
  mount(h);
  const items = within(await screen.findByRole("list", { name: "Checks" })).getAllByRole("listitem");
  expect(items[0]).toHaveTextContent(/^Healthy: resource status$/);
  expect(items[2]).toHaveTextContent(/^Unknown: gateway ownership$/);
});

it("only the strip headline is a live status, not its ticking age", async () => {
  mount(healthy());
  const strip = await screen.findByTestId("health-strip");
  expect(strip).not.toHaveAttribute("role");
  expect(within(strip).getByRole("status")).toHaveTextContent(/^All healthy$/);
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
  const checked = await screen.findByRole("list", { name: "Checks" });
  expect(checked).toHaveTextContent("scope from Deployment template; no controller Pod running");
});

it("unknown: never green, names the missing signal", async () => {
  mount(unknown());
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("Health unknown: Cannot check xDS rejects");
  expect(strip).not.toHaveTextContent("No known problems");
  expect(within(strip).getByTestId("health-icon-unknown")).toBeInTheDocument();
  expect(screen.getByTestId("finding-row")).toHaveTextContent("connection refused");
});

it("unknown with warnings names the reason, then the warnings", async () => {
  const h = unknown();
  h.findings.push({ ...(unhealthy().findings[1] as HealthReport["findings"][number]) });
  h.counts = { info: 1, warning: 1 };
  mount(h);
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("Health unknown: Cannot check xDS rejects. 1 warning");
  expect(strip).not.toHaveTextContent("No known problems");
});

it("served kinds changed: the strip names the check and the finding shows its detail", async () => {
  const t = new Date().toISOString();
  mount({
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
  expect(strip).toHaveTextContent("Health unknown: Cannot check served kinds");
  const row = screen.getByTestId("finding-row");
  expect(row).toHaveTextContent("Served kinds changed; restart the UI to watch them");
  expect(row).toHaveTextContent("added TLSRoute");
  // No resource: the install stands in, named once the installs list loads.
  await waitFor(() => expect(row).toHaveTextContent("kgw/kgateway"));
});

// The strip and the gateway rows age into stale on the shared clock, without new data.
it("a healthy page turns stale as it ages while identical responses keep arriving", async () => {
  vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
  const base = Date.parse("2026-10-19T12:00:00Z");
  vi.setSystemTime(base);
  mount(healthy(base), "/i/kgw.kgateway", overview(base));
  // vi.waitFor polls on real timers, so it works while setInterval is faked.
  await vi.waitFor(() => expect(screen.getByTestId("health-strip")).toHaveTextContent("All healthy"));
  for (const r of gatewayRows()) expect(r).not.toHaveTextContent("Unknown");
  const live = within(screen.getByTestId("health-strip")).getByRole("status");
  vi.setSystemTime(base + 2 * POLL_MS + 1000);
  act(() => {
    vi.advanceTimersByTime(1000);
  });
  expect(screen.getByTestId("health-strip")).toHaveTextContent(/Data is stale/);
  // One live region, updated in place, so screen readers announce the change.
  expect(within(screen.getByTestId("health-strip")).getByRole("status")).toBe(live);
  expect(live).toHaveTextContent(/^Data is stale$/);
  for (const r of gatewayRows()) expect(r).toHaveTextContent("Unknown");
});

it("a fresh strip says when it was checked", async () => {
  mount(healthy());
  expect(await screen.findByTestId("health-strip")).toHaveTextContent(/All healthy\s*Last checked \d+s ago$/);
});

it("a pending check without a message says it is still reading", async () => {
  const h = processing();
  h.checks = [{ name: "resource status", ok: false, required: true, pending: true, collectedAt: h.collectedAt }];
  mount(h);
  expect(await screen.findByTestId("health-strip")).toHaveTextContent("Health unknown: Still reading resource status");
});

it("unknown without a failing required check says some checks did not finish", async () => {
  mount({ ...unknown(), checks: [] });
  expect(await screen.findByTestId("health-strip")).toHaveTextContent("Health unknown: Some checks did not finish");
});

it("tile tooltips name the access problem and the scope source", async () => {
  const ov = overview();
  const denied = ov.counts[3];
  if (denied) denied.access = { ok: false };
  mount(healthy(), "/i/kgw.kgateway", ov);
  const noAccess = await screen.findByText("No access");
  await userEvent.hover(noAccess);
  expect(screen.getByRole("tooltip")).toHaveTextContent(/^No permission to read this$/);
  await userEvent.unhover(noAccess);
  await userEvent.hover(within(screen.getByTestId("scope-tile")).getByText("12 of 40"));
  expect(screen.getByRole("tooltip")).toHaveTextContent(/^Namespaces matching team=a\. Source: pod kgateway-abc\.$/);
});

it("a selector without labels matches any label", async () => {
  const ov = overview();
  ov.header.scope = { ...(ov.header.scope as ScopeSummary), selectors: [{}], source: "deployment template" };
  mount(healthy(), "/i/kgw.kgateway", ov);
  await userEvent.hover(within(await screen.findByTestId("scope-tile")).getByText("12 of 40"));
  expect(screen.getByRole("tooltip")).toHaveTextContent(
    /^Namespaces matching any label\. Source: Deployment template\.$/,
  );
});

it("an install without Gateways says so", async () => {
  mount(healthy(), "/i/kgw.kgateway", { ...overview(), gateways: [] });
  expect(await screen.findByText("No Gateways in this install.")).toBeInTheDocument();
});

it("being processed: unknown, never green, and says what is happening", async () => {
  mount(processing());
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("Health unknown: 2 resources are being processed by the controller");
  expect(strip).not.toHaveTextContent("No known problems");
  expect(within(strip).getByTestId("health-icon-unknown")).toBeInTheDocument();
  expect(strip).not.toHaveTextContent("Cannot check");
});

it("a missing proxy inside the grace period: unknown, and says the proxy is not found yet", async () => {
  mount(proxyPending());
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("Health unknown: 1 proxy not found yet");
  expect(strip).not.toHaveTextContent("Cannot check");
  expect(within(strip).getByTestId("health-icon-unknown")).toBeInTheDocument();
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
  mount(healthy(), "/i/kgw.kgateway", overview(Date.now() - 75_000));
  // The age shown is that of the older response.
  expect(await screen.findByTestId("health-strip")).toHaveTextContent("Data is stale. Last checked 1m ago");
});

it("count tiles, GatewayClass tile first, no-access tile and scope tile", async () => {
  mount(healthy());
  expect(await screen.findByText("142")).toBeInTheDocument();
  const tiles = screen.getAllByTestId("count-tile");
  expect(tiles[0]).toHaveTextContent("GatewayClasses");
  expect(screen.getByText("No access")).toBeInTheDocument();
  const scope = screen.getByTestId("scope-tile");
  expect(scope).toHaveTextContent("12 of 40");
  expect(scope).toHaveTextContent("Namespaces watched");
  expect(scope).not.toHaveTextContent("Deployment template");
});

it("scope tile says when the scope comes from the Deployment template", async () => {
  const ov = overview();
  ov.header.scope = { ...(ov.header.scope as ScopeSummary), source: "deployment template" };
  mount(healthy(), "/i/kgw.kgateway", ov);
  expect(await screen.findByTestId("scope-tile")).toHaveTextContent("Source: Deployment template");
});

it("scope unknown shows an unknown scope tile", async () => {
  const ov = overview();
  ov.header.scope = { mode: "unknown", error: "uses valueFrom", watched: 0, total: 40 };
  mount(healthy(), "/i/kgw.kgateway", ov);
  const scope = await screen.findByTestId("scope-tile");
  expect(scope).toHaveTextContent("Unknown");
  expect(within(scope).getByTestId("health-icon-unknown")).toBeInTheDocument();
  expect(scope).not.toHaveTextContent("0 of 40");
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
  mount(healthy(), "/i/kgw.kgateway?q=internal", ov);
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
  mount(healthy(), "/i/kgw.kgateway?sort=routes.asc");
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
    "/i/kgw.kgateway/r/gateway.networking.k8s.io/Gateway/prod/edge",
  );
});

it("links only finding resources whose kind /api/v1/meta lists", async () => {
  stubApi({
    "/api/v1/installs/kgw.kgateway/overview": overview(),
    "/api/v1/installs/kgw.kgateway/health": unhealthy(),
    "/api/v1/meta": { ...meta, kinds: meta.kinds.filter((k) => k.kind !== "HTTPRoute") },
  });
  renderWithTheme(
    <Routes>
      <Route path="/i/:id" element={<InstallPage />} />
    </Routes>,
    { path: "/i/kgw.kgateway" },
  );
  const rows = await screen.findAllByTestId("finding-row");
  await waitFor(() =>
    expect(within(rows[0] as HTMLElement).getByRole("link")).toHaveAttribute(
      "href",
      "/i/kgw.kgateway/r/gateway.networking.k8s.io/Gateway/prod/edge",
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
  expect(row).toHaveTextContent("Restricted: ask the owning team");
  expect(within(row).queryByRole("link")).toBeNull();
});

it("switching installs never shows the previous install's data", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.startsWith("/api/v1/installs/b.two/")) return new Promise<Response>(() => {}); // install B never answers
      if (url === "/api/v1/installs") return new Response(JSON.stringify([installSummary()]), { status: 200 });
      const body = url.endsWith("/overview") ? overview() : url.endsWith("/health") ? unhealthy() : meta;
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  renderWithTheme(
    <>
      <Routes>
        <Route path="/i/:id" element={<InstallPage />} />
      </Routes>
      <GoTo to="/i/b.two" />
    </>,
    { path: "/i/kgw.kgateway" },
  );
  expect(await screen.findByTestId("health-strip")).toHaveTextContent("1 error");
  await userEvent.click(screen.getByRole("button", { name: "go" }));
  await waitFor(() => expect(screen.queryByTestId("health-strip")).toBeNull());
  expect(screen.getByRole("status", { name: "Loading this install…" })).toHaveAttribute("aria-busy", "true");
  expect(screen.queryByRole("table", { name: "Gateways" })).toBeNull();
  expect(screen.queryAllByTestId("finding-row")).toHaveLength(0);
});

it("never renders a response that belongs to another install", async () => {
  const ov = overview();
  mount(healthy(), "/i/kgw.kgateway", { ...ov, header: { ...ov.header, id: "other.install" } });
  await waitFor(() => expect(screen.getByRole("status", { name: "Loading this install…" })).toBeInTheDocument());
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
      <Route path="/i/:id" element={<InstallPage />} />
    </Routes>,
    { path: "/i/kgw.kgateway" },
  );
  // meta lists 4 countable kinds (Deployment and GatewayParameters are not counted) + the scope tile.
  await waitFor(() => expect(screen.getAllByTestId("tile-shimmer")).toHaveLength(5));
});

it("the loading skeleton reserves no scope tile where the platform has no scope", async () => {
  const noScope = { ...meta, capabilities: ["workloads"] };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url === "/api/v1/meta" ? new Response(JSON.stringify(noScope), { status: 200 }) : new Promise<Response>(() => {}),
    ),
  );
  renderWithTheme(
    <Routes>
      <Route path="/i/:id" element={<InstallPage />} />
    </Routes>,
    { path: "/i/kgw.kgateway" },
  );
  // 4 countable kinds and no scope tile.
  await waitFor(() => expect(screen.getAllByTestId("tile-shimmer")).toHaveLength(4));
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
    "/api/v1/installs/kgw.kgateway/overview": [404, { error: "install kgw/kgateway not found" }],
    "/api/v1/installs/kgw.kgateway/health": [404, { error: "install kgw/kgateway not found" }],
  });
  renderWithTheme(
    <Routes>
      <Route path="/i/:id" element={<InstallPage />} />
    </Routes>,
    { path: "/i/kgw.kgateway" },
  );
  await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("install kgw/kgateway not found"));
});

it.each([
  ["error", "Error"],
  ["warning", "Warning"],
] as const)("a %s install without counts falls back to the state label", async (state, label) => {
  mount({ ...unhealthy(), state, counts: {} });
  expect(await screen.findByTestId("health-strip")).toHaveTextContent(label);
});

// Mirrors the backend's rowState: a missing required signal makes every non-error row unknown (D-006).
it("a failing required check turns non-error gateway rows unknown", async () => {
  mount(unknown());
  await screen.findByTestId("health-strip");
  const [edge, internal] = gatewayRows() as [HTMLElement, HTMLElement];
  expect(edge).toHaveTextContent("Error"); // an error finding is still an error
  expect(internal).toHaveTextContent("Unknown");
  expect(within(internal).queryByTestId("health-icon-healthy")).toBeNull();
});

it("a failing optional check leaves gateway rows as reported", async () => {
  const h = healthy();
  h.checks.push({ name: "gateway ownership", ok: false, required: false, collectedAt: h.collectedAt });
  mount(h);
  await screen.findByTestId("health-strip");
  expect(gatewayRows()[1]).toHaveTextContent("Healthy");
});

it("a healthy install with info findings links to them", async () => {
  const h = healthy();
  h.findings = unknown().findings.concat(processing().findings);
  h.counts = { info: 3 };
  mount(h);
  expect(await screen.findByRole("link", { name: "3 info findings" })).toHaveAttribute(
    "href",
    "/i/kgw.kgateway/findings",
  );
});

it("a healthy install without findings shows no findings link", async () => {
  mount(healthy());
  await screen.findByTestId("health-strip");
  expect(screen.queryByRole("link", { name: /info finding/ })).toBeNull();
});

it("gateway rows take the worst of their row state and their current health findings", async () => {
  const h = healthy();
  const gw = "gateway.networking.k8s.io";
  h.state = "error";
  h.counts = { error: 1, warning: 1 };
  h.findings = [
    {
      code: "KGWUI-002",
      slug: "condition-false",
      severity: "warning",
      install: "kgw.kgateway",
      message: "Listener conflicted",
      resource: { group: gw, kind: "Gateway", namespace: "prod", name: "internal" },
    },
    // A finding on another kind with the same name does not count for the Gateway.
    {
      code: "KGWUI-002",
      slug: "condition-false",
      severity: "error",
      install: "kgw.kgateway",
      message: "Backend not found",
      resource: { group: gw, kind: "HTTPRoute", namespace: "prod", name: "internal" },
    },
  ];
  mount(h);
  await screen.findByTestId("health-strip");
  const [edge, internal] = gatewayRows() as [HTMLElement, HTMLElement];
  expect(edge).toHaveTextContent("Error"); // already an error on the overview
  expect(internal).toHaveTextContent("Warning");
});

it("after a first load, a failing poll keeps the page and says so", async () => {
  const routes: Record<string, unknown> = {
    "/api/v1/installs/kgw.kgateway/overview": overview(),
    "/api/v1/installs/kgw.kgateway/health": healthy(),
    "/api/v1/meta": meta,
  };
  stubApi(routes);
  renderWithTheme(
    <Routes>
      <Route path="/i/:id" element={<InstallPage />} />
    </Routes>,
    { path: "/i/kgw.kgateway" },
  );
  await screen.findByTestId("health-strip");
  routes["/api/v1/installs/kgw.kgateway/health"] = [503, { error: "connecting to cluster: cache not synced" }];
  // SWR refetches on reconnect of a visible page (jsdom reports hidden).
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
  act(() => {
    window.dispatchEvent(new Event("online"));
  });
  expect(await screen.findByTestId("page-notice")).toHaveTextContent(
    /^Waiting for the cluster: connecting to cluster: cache not synced$/,
  );
  expect(screen.getByTestId("health-strip")).toBeInTheDocument();
  expect(screen.getByRole("table", { name: "Gateways" })).toBeInTheDocument();
  // Unconfirmed data is not evidence: no green survives while a poll fails (D-006).
  expect(screen.queryAllByTestId("health-icon-healthy")).toHaveLength(0);
  for (const r of gatewayRows()) expect(r).toHaveTextContent("Unknown");
});

it("the install page has an h1 and accepts stylesOverride", async () => {
  stubApi({
    "/api/v1/installs/kgw.kgateway/overview": overview(),
    "/api/v1/installs/kgw.kgateway/health": healthy(),
    "/api/v1/meta": meta,
  });
  renderWithTheme(
    <Routes>
      <Route
        path="/i/:id"
        element={
          <InstallPage
            stylesOverride={css`
              outline: 3px solid rgb(1, 2, 3);
            `}
          />
        }
      />
    </Routes>,
    { path: "/i/kgw.kgateway" },
  );
  const h1 = await screen.findByRole("heading", { level: 1 });
  expect(h1).toHaveTextContent("kgw/kgateway");
  expect(screen.getByTestId("install-page")).toHaveStyle({ outline: "3px solid rgb(1, 2, 3)" });
});

// region Switching installs

function summary(ov: OverviewResponse): InstallSummary {
  return { ...ov.header, health: { state: "healthy", counts: {} }, computedAt: new Date().toISOString() };
}

function Path() {
  const loc = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <output data-testid="path">{loc.pathname + loc.search}</output>
      <button type="button" onClick={() => navigate(-1)}>
        back
      </button>
    </>
  );
}

async function switchTo(id: string) {
  await userEvent.click(screen.getByRole("button", { name: /^Switch install/ }));
  const item = within(screen.getByRole("menu", { name: "Installs" }))
    .getAllByRole("menuitem")
    .find((el) => el.textContent?.replace(/\s/g, "").includes(id));
  if (!item) throw new Error(`no menu item for ${id}`);
  await userEvent.click(item);
}

const filterBox = () => screen.getByRole("textbox", { name: "Filter gateways" });

it("switching installs does not carry the gateway filter over", async () => {
  const a = overview();
  const b = overview();
  b.header = { ...b.header, id: "b.two", displayName: "b/two", namespace: "b", name: "two" };
  stubApi({
    "/api/v1/installs/kgw.kgateway/overview": a,
    "/api/v1/installs/kgw.kgateway/health": healthy(),
    "/api/v1/installs/b.two/overview": b,
    "/api/v1/installs/b.two/health": { ...healthy(), install: "b.two" },
    "/api/v1/meta": meta,
  });
  renderWithTheme(
    <>
      <InstallSwitcher installs={[summary(a), summary(b)]} currentId="" />
      <Routes>
        <Route path="/i/:id" element={<InstallPage />} />
      </Routes>
      <Path />
    </>,
    { path: "/i/kgw.kgateway?q=internal" },
  );
  await screen.findByRole("table", { name: "Gateways" });
  expect(filterBox()).toHaveValue("internal");
  // Visit B, then go back: both installs are cached, so the table stays mounted from here on.
  await switchTo("b/two");
  await waitFor(() => expect(screen.getByTestId("path")).toHaveTextContent(/^\/i\/b\.two$/));
  await screen.findByRole("table", { name: "Gateways" });
  await userEvent.click(screen.getByRole("button", { name: "back" }));
  await waitFor(() => expect(filterBox()).toHaveValue("internal"));
  expect(gatewayRows()).toHaveLength(1);
  await switchTo("b/two");
  await waitFor(() => expect(screen.getByTestId("path")).toHaveTextContent(/^\/i\/b\.two/));
  expect(screen.getByTestId("path")).toHaveTextContent(/^\/i\/b\.two$/);
  expect(filterBox()).toHaveValue("");
  expect(gatewayRows()).toHaveLength(2);
  // Typing on A, then switching, leaves B clean too.
  await switchTo("kgw/kgateway");
  await waitFor(() => expect(screen.getByTestId("path")).toHaveTextContent(/^\/i\/kgw\.kgateway$/));
  await userEvent.type(filterBox(), "edge");
  expect(screen.getByTestId("path")).toHaveTextContent(/^\/i\/kgw\.kgateway\?q=edge$/);
  await switchTo("b/two");
  await waitFor(() => expect(screen.getByTestId("path")).toHaveTextContent(/^\/i\/b\.two/));
  expect(screen.getByTestId("path")).toHaveTextContent(/^\/i\/b\.two$/);
  expect(filterBox()).toHaveValue("");
});
