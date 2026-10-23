import { render, screen, waitFor } from "@testing-library/react";
import { SWRConfig } from "swr";
import {
  type CorePages,
  controlRoomDark,
  defaultPages,
  Shell,
  type ShellExtension,
  THEME_PAINT_KEY,
  type Theme,
} from "../index";
import { overview, unhealthy } from "../test/fixtures";
import { stubApi } from "../test/render";
import { reservedPathOf } from "./Shell";

// A downstream with its own brand, theme, page, nav entry and finding renderer, registered through the public shell API only.
const acmeTheme: Theme = {
  ...controlRoomDark,
  id: "acme",
  name: "Acme",
  color: { ...controlRoomDark.color, accent: "#7A1FA2", bg: "#101820" },
  font: { ...controlRoomDark.font, ui: "Acme Sans", size: { ...controlRoomDark.font.size, sm: "13px" } },
};

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

it("a downstream extension contributes brand, theme, page, nav entry and finding renderer", async () => {
  const now = Date.now();
  stubApi({
    "/api/v1/installs": [
      {
        id: "kgw.kgateway",
        displayName: "kgw/kgateway",
        namespace: "kgw",
        name: "kgateway",
        version: "9.9.9",
        edition: "acme",
        controllerNames: [],
        readiness: { state: "ready", ready: 1, desired: 1 },
        scope: { mode: "all", watched: 1, total: 1 },
        health: { state: "error", counts: { error: 1 } },
        computedAt: new Date(now).toISOString(),
      },
    ],
    "/api/v1/meta": {
      version: "dev",
      synced: true,
      platform: "Kubernetes",
      capabilities: ["scope", "workloads"],
      kinds: [],
    },
    "/api/v1/installs/kgw.kgateway/overview": overview(now),
    "/api/v1/installs/kgw.kgateway/health": {
      ...unhealthy(now),
      findings: [
        { code: "ACME-001", slug: "demo", severity: "error", install: "kgw.kgateway", message: "Downstream finding" },
      ],
    },
  });
  window.history.pushState({}, "", "/i/kgw.kgateway");
  render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <Shell
        pages={defaultPages}
        extension={{
          brand: { productName: "Acme Edge", logo: <span>A</span> },
          themes: [acmeTheme],
          defaultThemeId: "acme",
          routes: [{ path: "widgets", element: <p>widgets page</p>, scope: "install" }],
          navEntries: [{ label: "Widgets", to: "widgets", scope: "install" }],
          findingRenderers: { "ACME-": () => <span>acme detail</span> },
        }}
      />
    </SWRConfig>,
  );
  expect(await screen.findByText("Acme Edge")).toBeInTheDocument();
  expect(await screen.findByText("acme detail")).toBeInTheDocument();
  expect(screen.getByTestId("edition")).toHaveTextContent("acme");
  // The contributed tokens apply: colour, font family, type scale, and the pre-React paint record.
  expect(screen.getByTestId("version")).toHaveStyle({ backgroundColor: "#7A1FA2", fontSize: "13px" });
  expect(document.body).toHaveStyle({ fontFamily: "Acme Sans" });
  expect(JSON.parse(localStorage.getItem(THEME_PAINT_KEY) ?? "{}")).toEqual({ bg: "#101820", scheme: "dark" });
  // The nav entry links to the contributed page, which renders on navigation.
  expect(screen.getByRole("link", { name: "Widgets" })).toHaveAttribute("href", "/i/kgw.kgateway/widgets");
  window.history.pushState({}, "", "/i/kgw.kgateway/widgets");
  window.dispatchEvent(new PopStateEvent("popstate"));
  await waitFor(() => expect(screen.getByText("widgets page")).toBeInTheDocument());
});

// A build that predates the explorer slots: only the four Phase 1 pages.
const phase1Pages = {
  ClusterLanding: defaultPages.ClusterLanding,
  InstallPage: defaultPages.InstallPage,
  FindingsPage: defaultPages.FindingsPage,
  ResourcePage: defaultPages.ResourcePage,
};

function mountAt(path: string, pages: CorePages, extension: ShellExtension = {}) {
  stubApi({
    "/api/v1/installs": [],
    "/api/v1/meta": { version: "dev", synced: true, platform: "Kubernetes", capabilities: [], kinds: [] },
  });
  window.history.pushState({}, "", path);
  render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <Shell pages={pages} extension={extension} />
    </SWRConfig>,
  );
}

it("renders new pages with only the four Phase 1 slots", async () => {
  mountAt("/i/x/routes", phase1Pages);
  await waitFor(() => expect(screen.getByRole("heading", { name: "Routes", level: 1 })).toBeInTheDocument());
});

it("a downstream slot replaces the upstream page", async () => {
  mountAt("/i/x/gateways/infra/edge", { ...phase1Pages, GatewayPage: () => <p>downstream gateway page</p> });
  expect(await screen.findByText("downstream gateway page")).toBeInTheDocument();
});

it("a contribution at routes cannot replace the core page", async () => {
  mountAt("/i/x/routes", phase1Pages, {
    routes: [
      { path: "routes", element: <p>impostor</p>, scope: "install" },
      { path: "gateways/:ns/edge", element: <p>impostor</p>, scope: "install" },
      { path: "routes/*", element: <p>impostor</p>, scope: "install" },
    ],
  });
  await waitFor(() => expect(screen.getByRole("heading", { name: "Routes", level: 1 })).toBeInTheDocument());
  expect(screen.queryByText("impostor")).toBeNull();
  window.history.pushState({}, "", "/i/x/gateways/infra/edge");
  window.dispatchEvent(new PopStateEvent("popstate"));
  // The Gateway page answers instead, here with its not-found state.
  await waitFor(() => expect(screen.getByRole("heading", { name: "Not in this install's scope" })).toBeInTheDocument());
  expect(screen.queryByText("impostor")).toBeNull();
});

it("a contribution outside the reserved paths still renders", async () => {
  mountAt("/i/x/widgets", phase1Pages, {
    routes: [{ path: "widgets", element: <p>widgets page</p>, scope: "install" }],
  });
  expect(await screen.findByText("widgets page")).toBeInTheDocument();
});

it.each([
  ["install", "routes", "routes"],
  ["install", "/gateways/", "gateways"],
  ["install", "routes/*", "routes"],
  ["install", "gateways/:ns/:name", "gateways"],
  ["install", "listenersets/stats", "listenersets"],
  ["install", ":tab", undefined],
  ["install", "*", undefined],
  ["install", "x/routes", undefined],
  ["install", "routesx", undefined],
  ["top", "routes", undefined],
  ["top", "i/:id/x", undefined],
] as const)("reservedPathOf(%s %s) is %s", (scope, path, want) => {
  expect(reservedPathOf({ scope, path, element: null })).toBe(want);
});

it("a dropped contribution warns once, naming its path and the reserved path", async () => {
  const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
  mountAt("/i/x/routes", phase1Pages, {
    routes: [
      { path: "routes/stats", element: <p>impostor</p>, scope: "install" },
      { path: "widgets", element: <p>widgets</p>, scope: "install" },
    ],
  });
  await waitFor(() => expect(screen.getByRole("heading", { name: "Routes", level: 1 })).toBeInTheDocument());
  const ours = warn.mock.calls.filter((c) => String(c[0]).includes("routes/stats"));
  expect(ours).toHaveLength(1);
  expect(String(ours[0]?.[0])).toContain('"routes"');
  expect(warn.mock.calls.some((c) => String(c[0]).includes("widgets"))).toBe(false);
  warn.mockRestore();
});
