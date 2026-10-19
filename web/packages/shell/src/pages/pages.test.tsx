import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { POLL_MS } from "../api/hooks";
import type { Finding, HealthReport, InstallSummary } from "../api/types";
import { defaultBrand, ShellExtensionContext } from "../shell/extension";
import { meta, overview, restricted, unhealthy } from "../test/fixtures";
import { renderWithTheme, stubApi } from "../test/render";
import { ThemeProvider } from "../theme/ThemeProvider";
import { defaultThemes } from "../theme/themes";
import { ClusterLanding } from "./ClusterLanding";
import { FindingsPage } from "./FindingsPage";
import { FindingRow } from "./install/FindingRow";
import { ResourcePage } from "./ResourcePage";

// jsdom has no layout: give elements a size so the virtualiser renders rows.
beforeEach(() => {
  vi.spyOn(Element.prototype, "clientHeight", "get").mockReturnValue(800);
  vi.spyOn(Element.prototype, "clientWidth", "get").mockReturnValue(1000);
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(64);
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(1000);
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function inst(
  id: string,
  state: InstallSummary["health"]["state"],
  counts: InstallSummary["health"]["counts"] = {},
  computedAt = new Date().toISOString(),
  scope: Partial<InstallSummary["scope"]> = {},
): InstallSummary {
  const [namespace = "", name = ""] = id.split("/");
  return {
    id,
    namespace,
    name,
    version: "v2.4.7",
    edition: "kgateway",
    controllerNames: [],
    controller: { ready: 1, desired: 1 },
    scope: { mode: "selectors", watched: 12, total: 40, ...scope },
    health: { state, counts },
    computedAt,
  };
}

/** Revalidates every SWR key now: SWR refetches on reconnect of a visible page (jsdom reports hidden). */
function revalidate() {
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
  act(() => {
    window.dispatchEvent(new Event("online"));
  });
}

function Where() {
  const loc = useLocation();
  return <output data-testid="location">{loc.search}</output>;
}

// region Cluster landing

it("cluster landing renders one card per install linking to it", async () => {
  stubApi({ "/api/v1/installs": [inst("a/one", "error", { error: 1, warning: 2 }), inst("b/two", "healthy")] });
  renderWithTheme(<ClusterLanding />);
  const card = await screen.findByRole("link", { name: /a\/one/ });
  expect(card).toHaveAttribute("href", "/i/a/one");
  expect(card).toHaveTextContent("1 error, 2 warnings");
  expect(card).toHaveTextContent("12 of 40 namespaces");
  // The version label already carries a "v"; it is shown once.
  expect(within(card).getByText("v2.4.7")).toBeInTheDocument();
  const two = screen.getByRole("link", { name: /b\/two/ });
  expect(two).toHaveTextContent("Healthy");
  expect(two).toHaveTextContent("All healthy");
});

it("cluster landing says all namespaces for an install that watches everything", async () => {
  stubApi({ "/api/v1/installs": [inst("a/one", "healthy", {}, undefined, { mode: "all" })] });
  renderWithTheme(<ClusterLanding />);
  expect(await screen.findByRole("link", { name: /a\/one/ })).toHaveTextContent("All namespaces");
});

it("cluster landing names a scope error and a scope read from the Deployment template", async () => {
  stubApi({
    "/api/v1/installs": [
      inst("a/one", "healthy", {}, undefined, { mode: "unknown", error: "uses valueFrom" }),
      inst("b/two", "healthy", {}, undefined, { source: "deployment template" }),
    ],
  });
  renderWithTheme(<ClusterLanding />);
  expect(await screen.findByRole("link", { name: /a\/one/ })).toHaveTextContent("Scope unknown: uses valueFrom");
  expect(screen.getByRole("link", { name: /b\/two/ })).toHaveTextContent(
    "12 of 40 namespaces. Source: Deployment template",
  );
});

it("cluster landing never shows an old healthy summary as healthy", async () => {
  stubApi({ "/api/v1/installs": [inst("a/one", "healthy", {}, new Date(Date.now() - 60_000).toISOString())] });
  renderWithTheme(<ClusterLanding />);
  const card = await screen.findByRole("link", { name: /a\/one/ });
  expect(card).toHaveTextContent("Unknown");
  expect(card).toHaveTextContent("Data is stale");
  expect(card).not.toHaveTextContent("Healthy");
  expect(within(card).queryByTestId("health-icon-healthy")).toBeNull();
});

// Identical successful polls do not re-render through SWR, so only the shared clock turns an ageing summary stale.
it("a fresh summary turns unknown as it ages while identical responses keep arriving", async () => {
  vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
  const base = Date.parse("2026-10-19T12:00:00Z");
  vi.setSystemTime(base);
  stubApi({ "/api/v1/installs": [inst("a/one", "healthy", {}, new Date(base).toISOString())] });
  renderWithTheme(<ClusterLanding />);
  await vi.waitFor(() => expect(screen.getByRole("link", { name: /a\/one/ })).toHaveTextContent("Healthy"));
  vi.setSystemTime(base + 2 * POLL_MS + 1000);
  act(() => {
    vi.advanceTimersByTime(1000);
  });
  const card = screen.getByRole("link", { name: /a\/one/ });
  expect(card).toHaveTextContent("Unknown");
  expect(card).toHaveTextContent("Data is stale");
});

it("cluster landing shows an error state when installs cannot load", async () => {
  stubApi({ "/api/v1/installs": [500, { error: "boom" }] });
  renderWithTheme(<ClusterLanding />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Cannot load installs");
});

it("cluster landing keeps the loaded cards when a later poll fails", async () => {
  const routes: Record<string, unknown> = { "/api/v1/installs": [inst("a/one", "healthy")] };
  stubApi(routes);
  renderWithTheme(<ClusterLanding />);
  await screen.findByRole("link", { name: /a\/one/ });
  routes["/api/v1/installs"] = [500, { error: "boom" }];
  revalidate();
  expect(await screen.findByRole("alert")).toHaveTextContent("Cannot load installs: boom");
  expect(screen.getByRole("link", { name: /a\/one/ })).toBeInTheDocument();
});

// region Findings page

function findings(path: string, health: HealthReport = unhealthy()) {
  stubApi({
    "/api/v1/installs/kgw/kgateway/health": health,
    "/api/v1/installs/kgw/kgateway/overview": overview(),
    "/api/v1/meta": meta,
  });
  return renderWithTheme(
    <>
      <Routes>
        <Route path="/i/:ns/:name/findings" element={<FindingsPage />} />
      </Routes>
      <Where />
    </>,
    { path },
  );
}

it("findings page filters by severity chip and keeps it in the URL", async () => {
  findings("/i/kgw/kgateway/findings");
  await waitFor(() => expect(screen.getAllByTestId("finding-row")).toHaveLength(7));
  await userEvent.click(screen.getByRole("button", { name: "Error" }));
  await waitFor(() => expect(screen.getAllByTestId("finding-row")).toHaveLength(1));
  expect(screen.getByRole("button", { name: "Error" })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByTestId("location")).toHaveTextContent("?sev=error");
  await userEvent.click(screen.getByRole("button", { name: "Error" }));
  await waitFor(() => expect(screen.getAllByTestId("finding-row")).toHaveLength(7));
  expect(screen.getByTestId("location")).toBeEmptyDOMElement();
});

it("findings page builds kind chips from the findings and applies them from the URL", async () => {
  findings("/i/kgw/kgateway/findings?kind=Gateway");
  await waitFor(() => expect(screen.getAllByTestId("finding-row")).toHaveLength(1));
  expect(screen.getByRole("button", { name: "Gateway" })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByRole("button", { name: "HTTPRoute" })).toHaveAttribute("aria-pressed", "false");
});

it("findings page applies filters from the URL and shows an empty state", async () => {
  findings("/i/kgw/kgateway/findings?q=nothing-matches-this");
  await waitFor(() => expect(screen.getByText("No findings match these filters")).toBeInTheDocument());
  expect(screen.getByRole("textbox", { name: "Filter findings" })).toHaveValue("nothing-matches-this");
});

it("findings page says No findings when the install has none", async () => {
  const h = unhealthy();
  h.findings = [];
  findings("/i/kgw/kgateway/findings", h);
  await waitFor(() => expect(screen.getByText("No findings")).toBeInTheDocument());
  expect(screen.queryByText("No findings match these filters")).toBeNull();
});

it.each([
  ["message", "rejected by envoy", 1],
  ["code", "kgwui-002", 6],
  ["namespace/name", "shop/route-3", 1],
])("findings free text matches the %s", async (_what, q, n) => {
  findings(`/i/kgw/kgateway/findings?q=${encodeURIComponent(q)}`);
  await waitFor(() => expect(screen.getAllByTestId("finding-row")).toHaveLength(n));
});

it("findings free text never matches 'undefined' on a finding without a resource", async () => {
  const h = unhealthy();
  h.findings.push({
    code: "KGWUI-008",
    slug: "signal-unavailable",
    severity: "info",
    install: "kgw/kgateway",
    message: "Could not check xDS rejects",
  });
  findings("/i/kgw/kgateway/findings?q=undefined", h);
  await waitFor(() => expect(screen.getByText("No findings match these filters")).toBeInTheDocument());
});

it("findings page renders restricted findings without message or link", async () => {
  findings("/i/kgw/kgateway/findings", restricted());
  const row = await screen.findByTestId("finding-row");
  expect(row).toHaveTextContent("Restricted: ask the owning team");
  expect(row).toHaveTextContent("KGWUI-002");
  expect(within(row).queryByRole("link")).toBeNull();
});

it("findings page says when the data was checked", async () => {
  findings("/i/kgw/kgateway/findings");
  expect(await screen.findByTestId("data-age")).toHaveTextContent(/^Last checked \d+s ago$/);
});

it("findings page marks old data as stale", async () => {
  findings("/i/kgw/kgateway/findings", unhealthy(Date.now() - 75_000));
  const age = await screen.findByTestId("data-age");
  expect(age).toHaveTextContent("Data is stale. Last checked 1m ago");
  expect(within(age).getByTestId("health-icon-unknown")).toBeInTheDocument();
});

it("findings page keeps the loaded findings when a later poll fails", async () => {
  const routes: Record<string, unknown> = { "/api/v1/installs/kgw/kgateway/health": unhealthy(), "/api/v1/meta": meta };
  stubApi(routes);
  renderWithTheme(
    <Routes>
      <Route path="/i/:ns/:name/findings" element={<FindingsPage />} />
    </Routes>,
    { path: "/i/kgw/kgateway/findings" },
  );
  await waitFor(() => expect(screen.getAllByTestId("finding-row")).toHaveLength(7));
  routes["/api/v1/installs/kgw/kgateway/health"] = [500, { error: "boom" }];
  revalidate();
  expect(await screen.findByRole("alert")).toHaveTextContent("Cannot load findings: boom");
  expect(screen.getAllByTestId("finding-row")).toHaveLength(7);
});

it("findings list is keyboard scrollable", async () => {
  findings("/i/kgw/kgateway/findings");
  expect(await screen.findByRole("region", { name: "Findings" })).toHaveAttribute("tabindex", "0");
});

it("findings page virtualises a long list", async () => {
  const h = unhealthy();
  h.findings = Array.from({ length: 5000 }, (_, i) => ({
    code: "KGWUI-002",
    slug: "condition-false",
    severity: "warning" as const,
    install: "kgw/kgateway",
    message: "Backend not found",
    resource: { group: "gateway.networking.k8s.io", kind: "HTTPRoute", namespace: "shop", name: `route-${i}` },
  }));
  findings("/i/kgw/kgateway/findings", h);
  const rows = await screen.findAllByTestId("finding-row");
  expect(rows.length).toBeGreaterThan(0);
  expect(rows.length).toBeLessThan(100);
});

it("findings page never shows another install's findings", async () => {
  const h = unhealthy();
  h.install = "other/install";
  findings("/i/kgw/kgateway/findings", h);
  await waitFor(() => expect(screen.getByText("Loading findings…")).toBeInTheDocument());
  expect(screen.queryAllByTestId("finding-row")).toHaveLength(0);
});

// region Finding renderers

it("the longest matching finding renderer prefix wins", () => {
  const f: Finding = { ...(unhealthy().findings[0] as Finding) };
  render(
    <ShellExtensionContext.Provider
      value={{
        brand: defaultBrand,
        themes: defaultThemes,
        defaultThemeId: "",
        routes: [],
        navEntries: [],
        findingRenderers: {
          KGWUI: () => <span>short</span>,
          "KGWUI-00": () => <span>longest</span>,
          "KGWUI-0": () => <span>middle</span>,
        },
      }}
    >
      <ThemeProvider themes={defaultThemes}>
        <MemoryRouter>
          <FindingRow finding={f} installId="kgw/kgateway" />
        </MemoryRouter>
      </ThemeProvider>
    </ShellExtensionContext.Provider>,
  );
  expect(screen.getByText("longest")).toBeInTheDocument();
  expect(screen.queryByText("short")).toBeNull();
  expect(screen.queryByText("middle")).toBeNull();
});

// region Resource page

const cartPath = "/api/v1/installs/kgw/kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/cart";
const lastApplied = "kubectl.kubernetes.io/last-applied-configuration";

function resource(path: string) {
  return renderWithTheme(
    <Routes>
      <Route path="/i/:ns/:name/r/:group/:kind/:rns/:rname" element={<ResourcePage />} />
    </Routes>,
    { path },
  );
}

it("resource page shows YAML with status first and a copy button", async () => {
  stubApi({
    [cartPath]: {
      apiVersion: "gateway.networking.k8s.io/v1",
      kind: "HTTPRoute",
      metadata: { name: "cart", namespace: "shop" },
      spec: { parentRefs: [{ name: "edge" }] },
      status: { parents: [] },
    },
  });
  resource("/i/kgw/kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart");
  const pre = await screen.findByTestId("yaml");
  const text = pre.textContent ?? "";
  expect(text.indexOf("status:")).toBeGreaterThan(-1);
  expect(text.indexOf("status:")).toBeLessThan(text.indexOf("spec:"));
  // Status leads the document.
  expect(text.startsWith("status:")).toBe(true);
  expect(screen.getByRole("button", { name: "Copy namespace/name" })).toBeInTheDocument();
  expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("shop/cart");
});

it("resource page hides the last-applied annotation until asked", async () => {
  stubApi({
    [cartPath]: {
      kind: "HTTPRoute",
      metadata: { name: "cart", namespace: "shop", annotations: { [lastApplied]: '{"big":"blob"}', team: "a" } },
      spec: {},
    },
  });
  resource("/i/kgw/kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart");
  const pre = await screen.findByTestId("yaml");
  expect(pre).not.toHaveTextContent(lastApplied);
  expect(pre).toHaveTextContent("team: a");
  const toggle = screen.getByRole("button", { name: "Show last-applied config" });
  expect(toggle).toHaveAttribute("aria-expanded", "false");
  await userEvent.click(toggle);
  expect(screen.getByTestId("yaml")).toHaveTextContent(lastApplied);
  expect(screen.getByRole("button", { name: "Hide last-applied config" })).toHaveAttribute("aria-expanded", "true");
});

it("resource page without the annotation offers no toggle", async () => {
  stubApi({ [cartPath]: { kind: "HTTPRoute", metadata: { name: "cart", namespace: "shop" } } });
  resource("/i/kgw/kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart");
  await screen.findByTestId("yaml");
  expect(screen.queryByRole("button", { name: /last-applied/ })).toBeNull();
});

it("resource page 404 says it is out of scope", async () => {
  stubApi({});
  resource("/i/kgw/kgateway/r/gateway.networking.k8s.io/Gateway/infra/foreign");
  await waitFor(() => expect(screen.getByText("Not in this install's scope")).toBeInTheDocument());
  expect(screen.getByRole("link", { name: "Back to install" })).toHaveAttribute("href", "/i/kgw/kgateway");
});

it("resource page keeps the loaded YAML when a later fetch fails", async () => {
  const routes: Record<string, unknown> = {
    [cartPath]: { kind: "HTTPRoute", metadata: { name: "cart", namespace: "shop" } },
  };
  stubApi(routes);
  resource("/i/kgw/kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart");
  await screen.findByTestId("yaml");
  routes[cartPath] = [500, { error: "boom" }];
  revalidate();
  expect(await screen.findByRole("alert")).toHaveTextContent("Cannot load resource: boom");
  expect(screen.getByTestId("yaml")).toHaveTextContent("name: cart");
});

it("resource page shows other errors as errors", async () => {
  stubApi({ [cartPath]: [500, { error: "boom" }] });
  resource("/i/kgw/kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart");
  expect(await screen.findByRole("alert")).toHaveTextContent("Cannot load resource");
});
