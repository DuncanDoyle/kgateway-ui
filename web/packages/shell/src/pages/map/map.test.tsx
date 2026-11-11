import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes, useLocation } from "react-router-dom";
import gatewayGolden from "../../../../../../pkg/api/testdata/golden/gateway-detail.json";
import metaGolden from "../../../../../../pkg/api/testdata/golden/meta.json";
import pathsGolden from "../../../../../../pkg/api/testdata/golden/paths.json";
import type { GatewayDetail, PathsResponse } from "../../api/types";
import { renderWithTheme } from "../../test/render";
import { defaultThemes } from "../../theme/themes";
import { GatewayPage } from "../explore/GatewayPage";

const ID = "kgw.kgateway";
const API = `/api/v1/installs/${ID}`;
const GW_PATH = `/i/${ID}/gateways/infra/edge`;
const now = () => new Date().toISOString();
const edgePaths = (collectedAt = now()): PathsResponse => {
  const b = pathsGolden.body as unknown as PathsResponse;
  return { ...b, rows: b.rows.filter((r) => r.gateway.name === "edge"), collectedAt };
};

/** Stubs fetch by path; paths answers with body(). */
function serve(paths: () => PathsResponse) {
  const byPath: Record<string, () => unknown> = {
    "/api/v1/meta": () => metaGolden.body,
    [`${API}/gateways/infra/edge`]: () => ({ ...(gatewayGolden.body as GatewayDetail), collectedAt: now() }),
    [`${API}/paths`]: paths,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const body = byPath[new URL(url, "http://ui").pathname];
      return body === undefined
        ? new Response(JSON.stringify({ error: "not found" }), { status: 404 })
        : new Response(JSON.stringify(body()), { status: 200 });
    }),
  );
}
const pathCalls = () =>
  vi
    .mocked(fetch)
    .mock.calls.map((c) => new URL(String(c[0]), "http://ui"))
    .filter((u) => u.pathname.endsWith("/paths"));

function Where() {
  return <output data-testid="location">{useLocation().search}</output>;
}

function mount(query = "", themeId?: string) {
  return renderWithTheme(
    <>
      <Routes>
        <Route path="/i/:id/gateways/:ns/:name" element={<GatewayPage />} />
      </Routes>
      <Where />
    </>,
    { path: `${GW_PATH}${query}`, themeId },
  );
}

// React Flow measures its container and nodes; jsdom has no layout, so these report zero sizes.
beforeEach(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.stubGlobal(
    "DOMMatrixReadOnly",
    class {
      m22 = 1;
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("the Map tab opens from the tab strip and renders a fallback while the map loads", async () => {
  serve(() => edgePaths());
  mount();
  await screen.findByRole("table", { name: "Listeners" });
  await userEvent.setup().click(screen.getByRole("tab", { name: "Map" }));
  expect(screen.getByTestId("location")).toHaveTextContent("?tab=map");
  // The fallback holds the map's place until the first page and the lazy chunk have both arrived.
  expect(screen.getByTestId("map-fallback")).toHaveAttribute("aria-busy", "true");
  const map = await screen.findByTestId("gateway-map");
  expect(screen.queryByTestId("map-fallback")).toBeNull();
  // One node per distinct listener, route and backend, plus the Gateway.
  const names = within(map)
    .getAllByTestId("map-node")
    .map((n) => n.querySelector(".name")?.textContent);
  expect(names).toEqual(
    expect.arrayContaining(["infra/edge", "http", "extra", "shop/backends", "shop/api", "shop/cart-svc", "shop/be1"]),
  );
  expect(new Set(names).size).toBe(names.length);
});

it("the map reads /paths with gateway=ns/name and the tab's filters, page one of 200 rows", async () => {
  serve(() => edgePaths());
  mount("?tab=map&state=error,warning&kind=HTTPRoute&namespace=shop&q=cart&listener=http&sort=gateway.asc&cursor=c1");
  await screen.findByTestId("gateway-map");
  // The Overview's listener selector and the path list's sort and cursor are not the map's filters.
  expect([...(pathCalls()[0]?.searchParams.entries() ?? [])]).toEqual([
    ["gateway", "infra/edge"],
    ["namespace", "shop"],
    ["state", "error"],
    ["state", "warning"],
    ["kind", "HTTPRoute"],
    ["q", "cart"],
    ["limit", "200"],
  ]);
});

it("stale data turns every node state unknown", async () => {
  serve(() => edgePaths(new Date(Date.now() - 10 * 60_000).toISOString()));
  mount("?tab=map");
  const map = await screen.findByTestId("gateway-map");
  const states = within(map)
    .getAllByTestId("map-node")
    .map((n) => n.textContent ?? "");
  expect(states.length).toBeGreaterThan(1);
  for (const s of states) expect(s).toMatch(/Unknown$/);
});

it.each(defaultThemes.map((t) => [t.id, t] as const))(
  "React Flow's colours come from the %s theme's tokens",
  async (_, theme) => {
    serve(() => edgePaths());
    mount("?tab=map", theme.id);
    await screen.findByTestId("gateway-map");
    const css = [...document.querySelectorAll("style")].map((s) => s.textContent ?? "").join("");
    await waitFor(() => expect(css).toContain(`--xy-background-color:${theme.color.bg}`));
    expect(css).toContain(`--xy-minimap-background-color:${theme.color.surface}`);
    expect(css).toContain(`--xy-edge-stroke:${theme.color.textMuted}`);
  },
);
