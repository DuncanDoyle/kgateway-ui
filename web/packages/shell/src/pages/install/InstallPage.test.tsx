import { css } from "@emotion/react";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { POLL_MS } from "../../api/hooks";
import type {
  Diagnostics,
  GatewayRow,
  HealthReport,
  InstallSummary,
  OverviewResponse,
  ScopeSummary,
} from "../../api/types";
import { GROUP_ROW_H } from "../../diagnostics/GroupRow";
import { type CorePages, CorePagesContext } from "../../shell/extension";
import { InstallSwitcher } from "../../shell/InstallSwitcher";
import {
  diagnostics,
  groupRow,
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
import { pseudoRules, renderWithTheme, stubApi } from "../../test/render";
import { InstallPage } from "./InstallPage";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function Where() {
  const loc = useLocation();
  return (
    <>
      <output data-testid="location">{loc.search}</output>
      <output data-testid="pathname">{loc.pathname}</output>
    </>
  );
}

function mount(
  health: HealthReport,
  path = "/i/kgw.kgateway",
  ov: OverviewResponse = overview(),
  diag: Diagnostics = diagnostics(health),
) {
  stubApi({
    "/api/v1/installs": [installSummary()],
    "/api/v1/installs/kgw.kgateway/overview": ov,
    "/api/v1/installs/kgw.kgateway/diagnostics": diag,
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

const groupRows = () => screen.getAllByTestId("group-row");
const needsAttention = () => screen.queryByRole("region", { name: "Needs attention" });
const checksToggle = () => screen.getByRole("button", { name: "What was checked" });

it("unhealthy: strip with problems and counts, a group per cause, Show all", async () => {
  mount(unhealthy());
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("7 problems: 1 error, 6 warnings");
  expect(within(needsAttention() as HTMLElement).getAllByTestId("group-row")).toHaveLength(7);
  const first = groupRows()[0] as HTMLElement;
  expect(first).toHaveTextContent("Configuration rejected by Envoy");
  expect(first).toHaveTextContent("Error");
  expect(first).toHaveTextContent("KGWUI-001");
});

it("healthy install shows the slim strip and What was checked, no Needs attention box", async () => {
  mount(healthy());
  expect(await screen.findByTestId("health-strip")).toHaveTextContent(/^All healthy/);
  expect(needsAttention()).toBeNull();
  expect(screen.queryAllByTestId("group-row")).toHaveLength(0);
  expect(checksToggle()).toHaveAttribute("aria-expanded", "true");
  const checked = screen.getByRole("list", { name: "Checks" });
  expect(within(checked).getByText("xDS rejects")).toBeInTheDocument();
  // Processes stay on the page whatever the verdict.
  expect(screen.getByTestId("process-line-controller")).toHaveTextContent("2/2 ready");
});

it("10 groups in rank order with Show all to the grouped findings page", async () => {
  const groups = Array.from({ length: 10 }, (_, i) =>
    groupRow(i, { severity: i < 3 ? "error" : "warning", title: `Problem ${i}` }),
  );
  mount(unhealthy(), "/i/kgw.kgateway", overview(), { ...diagnostics(unhealthy(), groups), groupTotal: 14 });
  await screen.findByTestId("health-strip");
  // The server ranks; the page keeps its order.
  expect(groupRows().map((r) => within(r).getByTitle(/^Problem/).textContent)).toEqual(groups.map((g) => g.title));
  expect(within(groupRows()[0] as HTMLElement).getByRole("link", { name: "shop/route-0" })).toBeInTheDocument();
  const all = screen.getByRole("link", { name: "Show all groups (14)" });
  expect(all).toHaveAttribute("href", "/i/kgw.kgateway/findings?view=groups");
  // Unhealthy: What was checked starts collapsed and opens on demand.
  expect(checksToggle()).toHaveAttribute("aria-expanded", "false");
  expect(screen.queryByRole("list", { name: "Checks" })).toBeNull();
  await userEvent.click(checksToggle());
  expect(checksToggle()).toHaveAttribute("aria-expanded", "true");
  expect(screen.getByRole("list", { name: "Checks" })).toBeInTheDocument();
});

it("restricted group row", async () => {
  const hidden = groupRow(0, {
    title: "",
    code: "KGWUI-002",
    severity: "error",
    restricted: true,
    members: 3,
    affectedTotal: 0,
    since: new Date(Date.now() - 3_600_000).toISOString(),
    cause: {
      group: "gateway.networking.k8s.io",
      kind: "HTTPRoute",
      namespace: "payments",
      name: "checkout",
      link: "none",
    },
  });
  mount(restricted(), "/i/kgw.kgateway", overview(), diagnostics(restricted(), [hidden]));
  const row = (await screen.findAllByTestId("group-row"))[0] as HTMLElement;
  expect(row).toHaveTextContent("Error");
  expect(row).toHaveTextContent("KGWUI-002");
  expect(row).toHaveTextContent("Restricted: ask the owning team");
  // Only severity, code and the label: no cause, counts, age or steps (design §14 rule 13).
  expect(row).not.toHaveTextContent("checkout");
  expect(row).not.toHaveTextContent("payments");
  expect(row).not.toHaveTextContent("3 findings");
  expect(row).not.toHaveTextContent("ago");
  expect(within(row).queryByRole("link")).toBeNull();
  expect(within(row).queryByRole("button")).toBeNull();
});

it("Needs attention keeps its row height while loading", async () => {
  const routes: Record<string, unknown> = {
    "/api/v1/installs/kgw.kgateway/overview": overview(),
    "/api/v1/installs/kgw.kgateway/diagnostics": diagnostics(unhealthy()),
    "/api/v1/meta": meta,
  };
  stubApi(routes);
  const fetch = globalThis.fetch as ReturnType<typeof vi.fn>;
  renderWithTheme(
    <Routes>
      <Route path="/i/:id" element={<InstallPage />} />
    </Routes>,
    { path: "/i/kgw.kgateway" },
  );
  const rows = await screen.findAllByTestId("group-row");
  // Each collapsed row reserves its height from the first render.
  for (const r of rows) expect(r.querySelector(".line")).toHaveStyle({ minHeight: `${GROUP_ROW_H}px` });
  // The next poll never answers: the rows stay as they are, never a skeleton.
  fetch.mockImplementation(async () => new Promise<Response>(() => {}));
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
  act(() => {
    window.dispatchEvent(new Event("online"));
  });
  await waitFor(() => expect(fetch.mock.calls.length).toBeGreaterThan(3));
  expect(screen.getAllByTestId("group-row")).toHaveLength(rows.length);
  expect(screen.queryByRole("status", { name: "Loading this install…" })).toBeNull();
});

it("the loading skeleton reserves the Processes block", async () => {
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
  const box = await screen.findByTestId("processes-shimmer");
  const loaded = screen.getByRole("status", { name: "Loading this install…" });
  expect(loaded).toContainElement(box);
});

it("stale data turns every group unknown", async () => {
  const at = Date.now() - 60_000;
  mount(unhealthy(at), "/i/kgw.kgateway", overview(at));
  expect(await screen.findByTestId("health-strip")).toHaveTextContent(/Data is stale/);
  for (const r of groupRows()) {
    expect(within(r).getByTestId("health-icon-unknown")).toBeInTheDocument();
    expect(r).toHaveTextContent("Unknown");
    expect(r).not.toHaveTextContent(/Error|Warning/);
  }
  // No green survives on stale data: processes, checks and the Data plane column included (D-006).
  expect(screen.queryAllByTestId("health-icon-healthy")).toHaveLength(0);
});

it("Data plane column shows ✓, Rejects or Unknown", async () => {
  const ov = overview();
  const [edge, internal] = ov.gateways as [GatewayRow, GatewayRow];
  const lab = (name: string, dataPlane?: GatewayRow["dataPlane"]): GatewayRow => ({
    ...internal,
    ref: { ...internal.ref, name },
    dataPlane,
  });
  mount(healthy(), "/i/kgw.kgateway?sort=gateway.asc", {
    ...ov,
    gateways: [edge, internal, lab("lab", "unknown"), lab("old")],
  });
  const table = await screen.findByRole("table", { name: "Gateways" });
  const col = within(table)
    .getAllByRole("columnheader")
    .findIndex((h) => h.textContent === "Data plane");
  expect(col).toBeGreaterThan(-1);
  const cell = (name: string) => {
    const row = gatewayRows().find((r) => r.textContent?.includes(`prod/${name}`)) as HTMLElement;
    return within(row).getAllByRole("cell")[col] as HTMLElement;
  };
  expect(cell("edge")).toHaveTextContent(/^Rejects$/);
  expect(within(cell("edge")).getByTestId("health-icon-error")).toBeInTheDocument();
  expect(cell("internal")).toHaveTextContent(/^No rejects$/);
  expect(within(cell("internal")).getByTestId("health-icon-healthy")).toBeInTheDocument();
  expect(cell("lab")).toHaveTextContent(/^Unknown$/);
  // An older server sends no value: unknown, never a tick.
  expect(cell("old")).toHaveTextContent(/^Unknown$/);
  expect(within(cell("old")).getByTestId("health-icon-unknown")).toBeInTheDocument();
});

it("not available checks show their reason", async () => {
  const h = healthy();
  h.checks.push(
    {
      name: "probe failures",
      ok: false,
      required: false,
      collectedAt: h.collectedAt,
      unavailable: "off",
      error: "Events are turned off",
    },
    {
      name: "version skew",
      ok: false,
      required: false,
      collectedAt: h.collectedAt,
      unavailable: "denied",
    },
  );
  mount(h);
  // Not available never makes the install unknown (D-034).
  expect(await screen.findByTestId("health-strip")).toHaveTextContent(/^All healthy/);
  const items = within(screen.getByRole("list", { name: "Checks" })).getAllByRole("listitem");
  const off = items.find((li) => li.textContent?.includes("probe failures")) as HTMLElement;
  expect(off).toHaveTextContent(/^Not available: probe failures\s*Events are turned off$/);
  expect(within(off).getByTestId("check-unavailable")).toBeInTheDocument();
  expect(within(off).queryByTestId("health-icon-unknown")).toBeNull();
  // Without a message from the server, the reason names the cause.
  const denied = items.find((li) => li.textContent?.includes("version skew")) as HTMLElement;
  expect(denied).toHaveTextContent(/^Not available: version skew\s*No permission to read its input$/);
});

it("Processes shows the controller and proxies lines and links to the Processes page", async () => {
  const d = diagnostics(unhealthy());
  d.processes.proxies = { ...d.processes.proxies, state: "error", ready: 1, rejecting: 1 };
  mount(unhealthy(), "/i/kgw.kgateway", overview(), d);
  const section = await screen.findByRole("region", { name: "Processes" });
  expect(within(section).getByTestId("process-line-controller")).toHaveTextContent("Healthy");
  expect(within(section).getByTestId("process-line-controller")).toHaveTextContent("version 2.4.7");
  const proxies = within(section).getByTestId("process-line-proxies");
  expect(proxies).toHaveTextContent("Error");
  expect(proxies).toHaveTextContent("1/2 Gateways ready");
  expect(proxies).toHaveTextContent("1 rejecting");
  expect(within(section).getByRole("link", { name: "View processes" })).toHaveAttribute(
    "href",
    "/i/kgw.kgateway/processes",
  );
});

it("the strip names the oldest problem's age outside its live region", async () => {
  const d = diagnostics(unhealthy());
  d.verdict.oldestSince = new Date(Date.now() - 2 * 3_600_000).toISOString();
  mount(unhealthy(), "/i/kgw.kgateway", overview(), d);
  const strip = await screen.findByTestId("health-strip");
  expect(strip).toHaveTextContent("Oldest problem 2h ago");
  expect(within(strip).getByRole("status")).toHaveTextContent(/^7 problems: 1 error, 6 warnings$/);
});

it("a CorePages DiagnosticsSection replaces Needs attention and Processes", async () => {
  stubApi({
    "/api/v1/installs/kgw.kgateway/overview": overview(),
    "/api/v1/installs/kgw.kgateway/diagnostics": diagnostics(unhealthy()),
    "/api/v1/meta": meta,
  });
  const pages: Partial<CorePages> = {
    DiagnosticsSection: ({ diagnostics: d, installId }) => (
      <p data-testid="own-section">
        {installId}: {d.groupTotal} groups
      </p>
    ),
  };
  renderWithTheme(
    <CorePagesContext.Provider value={pages}>
      <Routes>
        <Route path="/i/:id" element={<InstallPage />} />
      </Routes>
    </CorePagesContext.Provider>,
    { path: "/i/kgw.kgateway" },
  );
  expect(await screen.findByTestId("own-section")).toHaveTextContent("kgw.kgateway: 7 groups");
  expect(needsAttention()).toBeNull();
  expect(screen.queryByRole("region", { name: "Processes" })).toBeNull();
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
  // Unknown has no groups to show; What was checked stays open with the reason.
  expect(screen.getByRole("list", { name: "Checks" })).toHaveTextContent("connection refused");
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
  // KGWUI-008 is not ranked: the strip names it and What was checked says why (design §5.3).
  expect(screen.queryAllByTestId("group-row")).toHaveLength(0);
  expect(screen.getByRole("list", { name: "Checks" })).toHaveTextContent("served kinds changed: added TLSRoute");
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
  const filter = screen.getByRole("textbox", { name: "Filter Gateways" });
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

it("Gateway table row opens the Gateway page", async () => {
  mount(healthy());
  await screen.findByRole("table", { name: "Gateways" });
  const row = gatewayRows()[0] as HTMLElement;
  expect(within(row).getByRole("link", { name: "prod/edge" })).toHaveAttribute(
    "href",
    "/i/kgw.kgateway/gateways/prod/edge",
  );
  // A click anywhere on the row opens the same page.
  await userEvent.click(within(row).getByText("34.12.8.1"));
  expect(screen.getByTestId("pathname")).toHaveTextContent("/i/kgw.kgateway/gateways/prod/edge");
});

it("a Gateway row follows its link: yaml opens the YAML view, none is plain text", async () => {
  const ov = overview();
  const [edge, internal] = ov.gateways as [GatewayRow, GatewayRow];
  mount(healthy(), "/i/kgw.kgateway", {
    ...ov,
    gateways: [
      { ...edge, link: "yaml" },
      { ...internal, link: "none" },
    ],
  });
  await screen.findByRole("table", { name: "Gateways" });
  const [yaml, none] = gatewayRows() as [HTMLElement, HTMLElement];
  expect(within(yaml).getByRole("link", { name: "prod/edge" })).toHaveAttribute(
    "href",
    "/i/kgw.kgateway/r/gateway.networking.k8s.io/Gateway/prod/edge",
  );
  expect(within(none).queryByRole("link")).toBeNull();
  expect(none).not.toHaveAttribute("data-linked");
  // Only a linked row looks clickable: pointer and hover/active backgrounds sit on [data-linked].
  expect(yaml).toHaveAttribute("data-linked");
  const css = [...document.querySelectorAll("style")].map((s) => s.textContent ?? "").join("");
  expect(css).toMatch(/tbody tr\[data-linked\]:hover/);
  expect(css).toMatch(/tbody tr\[data-linked\]:active/);
  expect(css).not.toMatch(/tbody tr:hover/);
  // A row without a link goes nowhere on click.
  await userEvent.click(within(none).getAllByRole("cell")[2] as HTMLElement);
  expect(screen.getByTestId("pathname").textContent).toBe("/i/kgw.kgateway");
});

it("a group's cause opens where its link says; link none is plain text", async () => {
  const gw = "gateway.networking.k8s.io";
  mount(
    unhealthy(),
    "/i/kgw.kgateway",
    overview(),
    diagnostics(unhealthy(), [
      groupRow(0, { cause: { group: gw, kind: "Gateway", namespace: "prod", name: "edge", link: "detail" } }),
      groupRow(1, {
        cause: { group: "gateway.kgateway.dev", kind: "Backend", namespace: "shop", name: "be1", link: "yaml" },
      }),
      groupRow(2, { cause: { group: gw, kind: "HTTPRoute", namespace: "shop", name: "cart", link: "none" } }),
    ]),
  );
  const [gateway, backend, none] = (await screen.findAllByTestId("group-row")) as [
    HTMLElement,
    HTMLElement,
    HTMLElement,
  ];
  expect(within(gateway).getByRole("link", { name: "prod/edge" })).toHaveAttribute(
    "href",
    "/i/kgw.kgateway/gateways/prod/edge",
  );
  expect(within(backend).getByRole("link", { name: "shop/be1" })).toHaveAttribute(
    "href",
    "/i/kgw.kgateway/r/gateway.kgateway.dev/Backend/shop/be1",
  );
  expect(none).toHaveTextContent("shop/cart");
  expect(within(none).queryByRole("link", { name: "shop/cart" })).toBeNull();
});

it("switching installs never shows the previous install's data", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.startsWith("/api/v1/installs/b.two/")) return new Promise<Response>(() => {}); // install B never answers
      if (url === "/api/v1/installs") return new Response(JSON.stringify([installSummary()]), { status: 200 });
      const body = url.endsWith("/overview")
        ? overview()
        : url.endsWith("/diagnostics")
          ? diagnostics(unhealthy())
          : meta;
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
  expect(screen.queryAllByTestId("group-row")).toHaveLength(0);
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
    "/api/v1/installs/kgw.kgateway/diagnostics": [404, { error: "install kgw/kgateway not found" }],
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
  const status = within(internal).getAllByRole("cell")[0] as HTMLElement;
  expect(status).toHaveTextContent("Unknown");
  expect(within(status).queryByTestId("health-icon-healthy")).toBeNull();
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

it("after a first load, a failing poll keeps the page and says so", async () => {
  const routes: Record<string, unknown> = {
    "/api/v1/installs/kgw.kgateway/overview": overview(),
    "/api/v1/installs/kgw.kgateway/diagnostics": diagnostics(healthy()),
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
  routes["/api/v1/installs/kgw.kgateway/diagnostics"] = [503, { error: "connecting to cluster: cache not synced" }];
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
    "/api/v1/installs/kgw.kgateway/diagnostics": diagnostics(healthy()),
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

const filterBox = () => screen.getByRole("textbox", { name: "Filter Gateways" });

it("switching installs does not carry the gateway filter over", async () => {
  const a = overview();
  const b = overview();
  b.header = { ...b.header, id: "b.two", displayName: "b/two", namespace: "b", name: "two" };
  stubApi({
    "/api/v1/installs/kgw.kgateway/overview": a,
    "/api/v1/installs/kgw.kgateway/diagnostics": diagnostics(healthy()),
    "/api/v1/installs/b.two/overview": b,
    "/api/v1/installs/b.two/diagnostics": diagnostics({ ...healthy(), install: "b.two" }),
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

it("Show all, View processes and the What was checked toggle carry hover, active and focus-visible styles", async () => {
  mount(unhealthy());
  await screen.findByTestId("health-strip");
  const controls = [
    screen.getByRole("link", { name: "Show all groups (7)" }),
    screen.getByRole("link", { name: "View processes" }),
    checksToggle(),
  ];
  for (const el of controls)
    for (const p of [":hover", ":active", ":focus-visible"]) expect(pseudoRules(el, p).length).toBeGreaterThan(0);
});

it("below 900 px the Gateway table keeps Status, Data plane, the name and routes in view", async () => {
  vi.stubGlobal("matchMedia", (q: string) => ({
    matches: q === "(max-width: 899px)",
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
  mount(unhealthy());
  const table = await screen.findByRole("table", { name: "Gateways" });
  expect(
    within(table)
      .getAllByRole("columnheader")
      .map((h) => h.textContent),
  ).toEqual(["Status", "Data plane", "Gateway", "Routes"]);
});
