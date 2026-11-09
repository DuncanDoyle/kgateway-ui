import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import metaGolden from "../../../../../pkg/api/testdata/golden/meta.json";
import { POLL_MS } from "../api/hooks";
import type { Finding, InstallSummary, ScopeSummary } from "../api/types";
import { defaultBrand, ShellExtensionContext } from "../shell/extension";
import { kubernetesVocabulary } from "../shell/vocabulary";
import { unhealthy } from "../test/fixtures";
import { pseudoRules, renderWithTheme, stubApi } from "../test/render";
import { ThemeProvider } from "../theme/ThemeProvider";
import { defaultThemes } from "../theme/themes";
import { ClusterLanding } from "./ClusterLanding";
import { FindingRow } from "./install/FindingRow";
import { ResourcePage } from "./ResourcePage";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// Fixture only: it takes "ns/name" for brevity; the UI itself never splits an ID.
function inst(
  nsName: string,
  state: InstallSummary["health"]["state"],
  counts: InstallSummary["health"]["counts"] = {},
  computedAt = new Date().toISOString(),
  scope: Partial<ScopeSummary> = {},
): InstallSummary {
  const [namespace = "", name = ""] = nsName.split("/");
  return {
    id: `${namespace}.${name}`,
    displayName: nsName,
    namespace,
    name,
    version: "v2.4.7",
    edition: "kgateway",
    controllerNames: [],
    readiness: { state: "ready", ready: 1, desired: 1 },
    scope: { mode: "selectors", watched: 12, total: 40, ...scope },
    health: { state, counts },
    groupRows: 0,
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

// region Cluster landing

it("cluster landing renders one card per install linking to it", async () => {
  stubApi({ "/api/v1/installs": [inst("a/one", "error", { error: 1, warning: 2 }), inst("b/two", "healthy")] });
  renderWithTheme(<ClusterLanding />);
  const card = await screen.findByRole("link", { name: /a\/one/ });
  expect(card).toHaveAttribute("href", "/i/a.one");
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
    "12 of 40 namespaces. Source: Deployment template.",
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

it("cluster landing keeps an idle install fresh when confirmedAt is recent", async () => {
  const now = Date.now();
  const old = new Date(now - 60_000).toISOString();
  stubApi({
    "/api/v1/installs": [{ ...inst("a/one", "error", { error: 1 }, old), confirmedAt: new Date(now).toISOString() }],
  });
  renderWithTheme(<ClusterLanding />);
  const card = await screen.findByRole("link", { name: /a\/one/ });
  expect(card).toHaveTextContent(/error/i);
  expect(card).not.toHaveTextContent("Data is stale");
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
  const card = screen.getByRole("link", { name: /a\/one/ });
  expect(card).toHaveTextContent("Unknown");
  expect(screen.queryAllByTestId("health-icon-healthy")).toHaveLength(0);
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
        vocabulary: kubernetesVocabulary,
        contributions: {},
        findingRenderers: {
          KGWUI: () => <span>short</span>,
          "KGWUI-00": () => <span>longest</span>,
          "KGWUI-0": () => <span>middle</span>,
        },
      }}
    >
      <ThemeProvider themes={defaultThemes}>
        <MemoryRouter>
          <FindingRow finding={f} installId="kgw.kgateway" />
        </MemoryRouter>
      </ThemeProvider>
    </ShellExtensionContext.Provider>,
  );
  expect(screen.getByText("longest")).toBeInTheDocument();
  expect(screen.queryByText("short")).toBeNull();
  expect(screen.queryByText("middle")).toBeNull();
});

it("the cluster landing is headed by the vocabulary's landing title", async () => {
  stubApi({ "/api/v1/installs": [inst("a/one", "healthy"), inst("b/two", "healthy")] });
  renderWithTheme(<ClusterLanding />);
  expect(await screen.findByRole("heading", { level: 1 })).toHaveTextContent("Installs in this cluster");
});

// region Resource page

const cartPath = "/api/v1/installs/kgw.kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/cart";
const lastApplied = "kubectl.kubernetes.io/last-applied-configuration";

function resource(path: string) {
  return renderWithTheme(
    <Routes>
      <Route path="/i/:id/r/:group/:kind/:rns/:rname" element={<ResourcePage />} />
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
  resource("/i/kgw.kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart");
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
  resource("/i/kgw.kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart");
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
  resource("/i/kgw.kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart");
  await screen.findByTestId("yaml");
  expect(screen.queryByRole("button", { name: /last-applied/ })).toBeNull();
});

it("resource page 404 says it is out of scope", async () => {
  stubApi({});
  resource("/i/kgw.kgateway/r/gateway.networking.k8s.io/Gateway/infra/foreign");
  await waitFor(() => expect(screen.getByText("Not in this install's scope")).toBeInTheDocument());
  expect(screen.getByRole("link", { name: "Back to install" })).toHaveAttribute("href", "/i/kgw.kgateway");
});

it("resource page keeps the loaded YAML when a later fetch fails", async () => {
  const routes: Record<string, unknown> = {
    [cartPath]: { kind: "HTTPRoute", metadata: { name: "cart", namespace: "shop" } },
  };
  stubApi(routes);
  resource("/i/kgw.kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart");
  await screen.findByTestId("yaml");
  routes[cartPath] = [500, { error: "boom" }];
  revalidate();
  expect(await screen.findByRole("alert")).toHaveTextContent("Cannot load resource: boom");
  expect(screen.getByTestId("yaml")).toHaveTextContent("name: cart");
});

/** Stubs fetch to serve body() for cartPath with a Collected-At header of at(). */
function stubResource(body: () => unknown, at: () => string) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url === cartPath
        ? new Response(JSON.stringify(body()), { status: 200, headers: { "Collected-At": at() } })
        : new Response(JSON.stringify({ error: "not found" }), { status: 404 }),
    ),
  );
}

it("resource page picks up a status change within one poll interval", async () => {
  vi.useFakeTimers({ toFake: ["Date", "setTimeout", "clearTimeout", "setInterval", "clearInterval"] });
  vi.setSystemTime(Date.parse("2026-10-21T12:00:00Z"));
  // SWR only polls a visible page; jsdom reports hidden.
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
  let reason = "Pending";
  stubResource(
    () => ({ kind: "HTTPRoute", metadata: { name: "cart", namespace: "shop" }, status: { reason } }),
    () => new Date().toISOString(),
  );
  resource("/i/kgw.kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart");
  await vi.waitFor(() => expect(screen.getByTestId("yaml")).toHaveTextContent("reason: Pending"));
  reason = "Accepted";
  await act(async () => {
    await vi.advanceTimersByTimeAsync(POLL_MS);
  });
  expect(screen.getByTestId("yaml")).toHaveTextContent("reason: Accepted");
});

it("resource page says when the data was checked, from the Collected-At header", async () => {
  stubResource(
    () => ({ kind: "HTTPRoute", metadata: { name: "cart", namespace: "shop" } }),
    () => new Date().toISOString(),
  );
  resource("/i/kgw.kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart");
  expect(await screen.findByTestId("data-age")).toHaveTextContent(/^Last checked \d+s ago$/);
});

it("resource page marks old data as stale", async () => {
  stubResource(
    () => ({ kind: "HTTPRoute", metadata: { name: "cart", namespace: "shop" } }),
    () => new Date(Date.now() - 75_000).toISOString(),
  );
  resource("/i/kgw.kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart");
  const age = await screen.findByTestId("data-age");
  expect(age).toHaveTextContent("Data is stale. Last checked 1m ago");
  expect(within(age).getByTestId("health-icon-unknown")).toBeInTheDocument();
});

it("resource page shows other errors as errors", async () => {
  stubApi({ [cartPath]: [500, { error: "boom" }] });
  resource("/i/kgw.kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart");
  expect(await screen.findByRole("alert")).toHaveTextContent("Cannot load resource");
});

const lookupName = "Routes that send traffic here";

it("a Backend YAML view links to Routes filtered by backend=gateway.kgateway.dev/Backend/ns/name", async () => {
  stubApi({
    "/api/v1/meta": metaGolden.body,
    "/api/v1/installs/kgw.kgateway/resources/gateway.kgateway.dev/Backend/shop/be1": {
      apiVersion: "gateway.kgateway.dev/v1alpha1",
      kind: "Backend",
      metadata: { name: "be1", namespace: "shop" },
      spec: { type: "Static" },
    },
  });
  resource("/i/kgw.kgateway/r/gateway.kgateway.dev/Backend/shop/be1");
  const link = await screen.findByRole("link", { name: lookupName });
  const url = new URL(link.getAttribute("href") ?? "", "http://ui");
  expect(url.pathname).toBe("/i/kgw.kgateway/routes");
  expect(url.searchParams.getAll("backend")).toEqual(["gateway.kgateway.dev/Backend/shop/be1"]);
  // PF26: the link has hover, active and focus-visible styles.
  for (const pseudo of [":hover", ":active", ":focus-visible"]) expect(pseudoRules(link, pseudo).join("")).not.toBe("");
});

const servedKind = (group: string, kind: string, category: string) => ({
  group,
  kind,
  category,
  version: "v1",
  versions: ["v1"],
  resource: `${kind.toLowerCase()}s`,
  displayName: `${kind}s`,
  namespaced: true,
});

it("the backend lookup and icon follow the /meta kind category, not the group", async () => {
  // A downstream kind served as a backend gets both; a kgateway Backend served under another category gets neither.
  stubApi({
    "/api/v1/meta": {
      ...metaGolden.body,
      kinds: [servedKind("example.com", "Pool", "backend"), servedKind("gateway.kgateway.dev", "Backend", "policy")],
    },
    "/api/v1/installs/kgw.kgateway/resources/example.com/Pool/shop/p1": { kind: "Pool", metadata: { name: "p1" } },
    "/api/v1/installs/kgw.kgateway/resources/gateway.kgateway.dev/Backend/shop/be1": {
      kind: "Backend",
      metadata: { name: "be1" },
    },
  });
  const pool = resource("/i/kgw.kgateway/r/example.com/Pool/shop/p1");
  const link = await screen.findByRole("link", { name: lookupName });
  expect(new URL(link.getAttribute("href") ?? "", "http://ui").searchParams.getAll("backend")).toEqual([
    "example.com/Pool/shop/p1",
  ]);
  expect(document.querySelector("[data-object-header] .lucide-server")).not.toBeNull();
  pool.unmount();
  resource("/i/kgw.kgateway/r/gateway.kgateway.dev/Backend/shop/be1");
  await screen.findByText(/spec|be1/);
  await waitFor(() => expect(screen.getAllByText("be1").length).toBeGreaterThan(0));
  expect(screen.queryByRole("link", { name: lookupName })).toBeNull();
  expect(document.querySelector("[data-object-header] .lucide-server")).toBeNull();
});

it("a Backend outside the install's scope offers no reverse lookup", async () => {
  stubApi({});
  resource("/i/kgw.kgateway/r/gateway.kgateway.dev/Backend/shop/gone");
  expect(await screen.findByText("Not in this install's scope")).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: lookupName })).toBeNull();
});

it("a Service has no YAML view entry point", async () => {
  // Services are not watched, so the backend never serves one; even if it did, the view offers no lookup.
  stubApi({
    "/api/v1/installs/kgw.kgateway/resources/-/Service/shop/cart": {
      kind: "Service",
      metadata: { name: "cart", namespace: "shop" },
    },
  });
  resource("/i/kgw.kgateway/r/-/Service/shop/cart");
  await screen.findByTestId("yaml");
  expect(screen.queryByRole("link", { name: lookupName })).toBeNull();
});

it("a YAML view of another kind offers no reverse lookup", async () => {
  stubApi({ [cartPath]: { kind: "HTTPRoute", metadata: { name: "cart", namespace: "shop" } } });
  resource("/i/kgw.kgateway/r/gateway.networking.k8s.io/HTTPRoute/shop/cart");
  await screen.findByTestId("yaml");
  expect(screen.queryByRole("link", { name: lookupName })).toBeNull();
});
