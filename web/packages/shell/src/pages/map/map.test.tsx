import { fireEvent, renderHook, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes, useLocation } from "react-router-dom";
import { SWRConfig } from "swr";
import gatewayGolden from "../../../../../../pkg/api/testdata/golden/gateway-detail.json";
import metaGolden from "../../../../../../pkg/api/testdata/golden/meta.json";
import pathsGolden from "../../../../../../pkg/api/testdata/golden/paths.json";
import type { GatewayDetail, LinkedRef, PathRow, PathSummary, PathsResponse } from "../../api/types";
import { renderWithTheme, testSWR } from "../../test/render";
import { defaultThemes } from "../../theme/themes";
import { GatewayPage } from "../explore/GatewayPage";
import { dedupeRows, useMapPaths } from "./mapData";

const ID = "kgw.kgateway";
const API = `/api/v1/installs/${ID}`;
const GW_PATH = `/i/${ID}/gateways/infra/edge`;
const now = () => new Date().toISOString();
const edgePaths = (collectedAt = now()): PathsResponse => {
  const b = pathsGolden.body as unknown as PathsResponse;
  return { ...b, rows: b.rows.filter((r) => r.gateway.name === "edge"), collectedAt };
};

/** Stubs fetch by path; paths answers with paths(url). */
function serve(paths: (u: URL) => PathsResponse) {
  const byPath: Record<string, (u: URL) => unknown> = {
    "/api/v1/meta": () => metaGolden.body,
    [`${API}/gateways/infra/edge`]: () => ({ ...(gatewayGolden.body as GatewayDetail), collectedAt: now() }),
    [`${API}/paths`]: paths,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const u = new URL(url, "http://ui");
      const body = byPath[u.pathname];
      return body === undefined
        ? new Response(JSON.stringify({ error: "not found" }), { status: 404 })
        : new Response(JSON.stringify(body(u)), { status: 200 });
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

/** Page n (from 0) of a result of total paths, 200 rows each, collected at collectedAt(n). */
function page(n: number, total: number, collectedAt = (_: number) => now()): PathsResponse {
  const b = edgePaths();
  const base = b.rows[0] as PathsResponse["rows"][number];
  const count = Math.max(0, Math.min(200, total - n * 200));
  const rows = Array.from({ length: count }, (_, k) => ({
    ...base,
    routes: [{ ...(base.routes[0] as LinkedRef), name: `r${n * 200 + k}` }],
  }));
  const next = (n + 1) * 200 < total ? `p${n + 1}` : undefined;
  return { ...b, rows, next, summary: { ...b.summary, paths: total }, collectedAt: collectedAt(n) };
}
const pageOf = (u: URL) => Number(u.searchParams.get("cursor")?.slice(1) ?? 0);

it("the map loads its pages in sequence by cursor and dates them by the oldest page", async () => {
  const t0 = Date.now();
  // Page 1 was collected earliest.
  const at = (n: number) => new Date(t0 - (n === 1 ? 3000 : 1000)).toISOString();
  serve((u) => page(pageOf(u), 450, at));
  const wrapper = ({ children }: { children: React.ReactNode }) => <SWRConfig value={testSWR()}>{children}</SWRConfig>;
  const params = new URLSearchParams();
  const { result } = renderHook(() => useMapPaths(ID, "infra/edge", params), { wrapper });
  expect(result.current).toMatchObject({ loading: true, partial: false, done: false });
  await waitFor(() => expect(result.current.done).toBe(true));
  expect(result.current).toMatchObject({ loading: false, partial: false, truncated: false, collectedAt: at(1) });
  expect(result.current.rows.map((r) => r.routes[0]?.name)).toEqual(Array.from({ length: 450 }, (_, k) => `r${k}`));
  expect(pathCalls().map((u) => u.searchParams.get("cursor"))).toEqual([null, "p1", "p2"]);
  expect(pathCalls().every((u) => u.searchParams.get("limit") === "200")).toBe(true);
});

it("a first load shows its rows as pages arrive", async () => {
  let release: () => void = () => {};
  const gate = new Promise<void>((r) => {
    release = r;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const n = pageOf(new URL(url, "http://ui"));
      if (n > 0) await gate;
      return new Response(JSON.stringify(page(n, 300)), { status: 200 });
    }),
  );
  const wrapper = ({ children }: { children: React.ReactNode }) => <SWRConfig value={testSWR()}>{children}</SWRConfig>;
  const params = new URLSearchParams();
  const { result } = renderHook(() => useMapPaths(ID, "infra/edge", params), { wrapper });
  await waitFor(() => expect(result.current).toMatchObject({ loading: false, partial: true, done: false }));
  expect(result.current.rows).toHaveLength(200);
  release();
  await waitFor(() => expect(result.current).toMatchObject({ partial: false, done: true }));
  expect(result.current.rows).toHaveLength(300);
});

/** Serves rows() as a /paths result of 200-row pages with summary(); the path count is rows().length. */
function serveResult(rows: () => PathRow[], summary: () => PathSummary, onPage?: (n: number) => void) {
  serve((u) => {
    const n = pageOf(u);
    onPage?.(n);
    const all = rows();
    const next = (n + 1) * 200 < all.length ? `p${n + 1}` : undefined;
    return { ...edgePaths(), rows: all.slice(n * 200, n * 200 + 200), next, summary: summary() };
  });
}
const named = (k: number, state: PathRow["problem"]["state"] = "healthy"): PathRow => {
  const base = edgePaths().rows[0] as PathRow;
  return { ...base, routes: [{ ...(base.routes[0] as LinkedRef), name: `r${k}` }], problem: { state, signals: [] } };
};
const hookWrapper = ({ children }: { children: React.ReactNode }) => (
  <SWRConfig value={testSWR()}>{children}</SWRConfig>
);

it("a row that turns bad between pages is not lost: the walk restarts when the result's counts change", async () => {
  // Problem-first order moves r250 ahead of the page-one anchor once it fails, so a plain walk would skip it.
  const healthy = Array.from({ length: 300 }, (_, k) => named(k));
  const failed = [named(250, "error"), ...healthy.filter((_, k) => k !== 250)];
  const base = edgePaths().summary;
  const counts = (errors: number): PathSummary => ({
    ...base,
    paths: 300,
    byState: { ...base.byState, error: { ...base.byState.error, paths: errors } },
  });
  let flipped = false;
  serveResult(
    () => (flipped ? failed : healthy),
    () => counts(flipped ? 1 : 0),
    (n) => {
      if (n === 1) flipped = true;
    },
  );
  const params = new URLSearchParams();
  const { result } = renderHook(() => useMapPaths(ID, "infra/edge", params), { wrapper: hookWrapper });
  await waitFor(() => expect(result.current.done).toBe(true));
  const names = result.current.rows.map((r) => r.routes[0]?.name);
  expect(names).toHaveLength(300);
  expect(new Set(names).size).toBe(300);
  expect(result.current.rows.find((r) => r.routes[0]?.name === "r250")?.problem.state).toBe("error");
  expect(result.current.inconsistent).toBe(false);
  // Page one, a page two whose counts differ, then a full second walk.
  expect(pathCalls().map((u) => u.searchParams.get("cursor"))).toEqual([null, "p1", null, "p1"]);
});

it("a result that keeps changing is walked three times, then shown marked inconsistent", async () => {
  const base = edgePaths().summary;
  let calls = 0;
  serveResult(
    () => Array.from({ length: 300 }, (_, k) => named(k)),
    () => ({ ...base, paths: 300 + calls++ }),
  );
  const params = new URLSearchParams();
  const { result } = renderHook(() => useMapPaths(ID, "infra/edge", params), { wrapper: hookWrapper });
  await waitFor(() => expect(result.current.inconsistent).toBe(true));
  expect(pathCalls()).toHaveLength(6);
  expect(result.current.rows.length).toBeGreaterThan(0);
});

it("the same path on two pages collapses to one row with the worse state", () => {
  const rows = dedupeRows([named(1), named(2), named(1, "error"), named(1, "warning")]);
  expect(rows.map((r) => [r.routes[0]?.name, r.problem.state])).toEqual([
    ["r1", "error"],
    ["r2", "healthy"],
  ]);
  // A different rule, port or mirror flag is another path.
  expect(
    dedupeRows([
      named(1),
      { ...named(1), rule: named(1).rule + 1 },
      { ...named(1), port: (named(1).port ?? 0) + 1 },
      { ...named(1), mirror: !named(1).mirror },
    ]),
  ).toHaveLength(4);
});

it("2,500 matching paths load 2,000 in ten requests and say so", async () => {
  serve((u) => page(pageOf(u), 2500));
  mount("?tab=map");
  await screen.findByTestId("gateway-map");
  expect(await screen.findByTestId("map-truncated")).toHaveTextContent(
    "Showing 2,000 of 2,500 paths. Narrow the filters to see the rest.",
  );
  await waitFor(() => expect(pathCalls()).toHaveLength(10));
  expect(pathCalls().map((u) => u.searchParams.get("cursor"))).toEqual([
    null,
    ...Array.from({ length: 9 }, (_, k) => `p${k + 1}`),
  ]);
  // One route node per loaded row, plus the Gateway, listener and backend.
  await waitFor(() => expect(within(screen.getByTestId("gateway-map")).getAllByTestId("map-node")).toHaveLength(2003));
});

// region Task 45: hover, side panel, find, chips, large Gateways

/** The map's node cards by name. */
const nodeNames = (map: HTMLElement) =>
  within(map)
    .getAllByTestId("map-node")
    .map((n) => n.querySelector(".name")?.textContent ?? "");
/** The React Flow wrapper of the node card named name. */
const flowNode = (map: HTMLElement, name: string) =>
  within(map)
    .getAllByTestId("map-node")
    .find((n) => n.querySelector(".name")?.textContent === name)
    ?.closest(".react-flow__node") as HTMLElement;
const refName = (r: { namespace?: string; name: string }) => (r.namespace ? `${r.namespace}/${r.name}` : r.name);

it("hovering a route lights every node on the rows through it and dims the rest", async () => {
  serve(() => edgePaths());
  mount("?tab=map");
  const map = await screen.findByTestId("gateway-map");
  const user = userEvent.setup();
  await user.hover(flowNode(map, "shop/mirror-only"));
  // The oracle: every Gateway, listener, route and backend named by a row that passes through the route.
  const through = edgePaths().rows.filter((r) => r.routes.some((x) => x.name === "mirror-only"));
  const want = new Set(
    through.flatMap((r) => [
      refName(r.gateway),
      r.listener.name,
      ...r.routes.map(refName),
      ...(r.backend ? [refName(r.backend)] : []),
    ]),
  );
  const lit = nodeNames(map).filter((n) => !flowNode(map, n).classList.contains("dim"));
  expect(new Set(lit)).toEqual(want);
  // shop/backends shares no row with it, although it shares a backend.
  expect(flowNode(map, "shop/backends")).toHaveClass("dim");
  await user.unhover(flowNode(map, "shop/mirror-only"));
  await waitFor(() => expect(map.querySelectorAll(".react-flow__node.dim")).toHaveLength(0));
});

it("a Gateway with more than 60 routes opens on problems only after its first page, with a prompt to narrow", async () => {
  // 61 routes over 450 paths: three pages unfiltered, but only the first is read.
  serve((u) => {
    const p = page(pageOf(u), u.searchParams.has("state") ? 3 : 450);
    return { ...p, summary: { ...p.summary, routes: u.searchParams.has("state") ? 3 : 61 } };
  });
  mount("?tab=map");
  await waitFor(() =>
    expect(new URLSearchParams(screen.getByTestId("location").textContent ?? "").get("state")).toBe(
      "error,warning,unknown",
    ),
  );
  expect(await screen.findByTestId("map-large")).toHaveTextContent(
    "This Gateway has 61 routes and 4 backends, so the map shows problems only. Narrow by kind or namespace, or clear Problems only to load every path.",
  );
  await screen.findByTestId("gateway-map");
  const unfiltered = pathCalls().filter((u) => !u.searchParams.has("state"));
  expect(unfiltered.map((u) => u.searchParams.get("cursor"))).toEqual([null]);
  expect(pathCalls().at(-1)?.searchParams.getAll("state")).toEqual(["error", "warning", "unknown"]);
  expect(screen.getByRole("button", { name: "Problems only" })).toHaveAttribute("aria-pressed", "true");
  // Releasing the chip shows every path and drops the prompt. The stopped first walk is not the whole result:
  // it shows as partial and is walked again in full.
  await userEvent.setup().click(screen.getByRole("button", { name: "Problems only" }));
  await waitFor(() => expect(screen.getByTestId("location")).not.toHaveTextContent("state="));
  expect(screen.queryByTestId("map-large")).toBeNull();
  // SWR's own revalidation may add a second full walk; the test SWR config does not dedupe.
  await waitFor(() =>
    expect(
      pathCalls()
        .filter((u) => !u.searchParams.has("state"))
        .map((u) => u.searchParams.get("cursor"))
        .slice(0, 4),
    ).toEqual([null, null, "p1", "p2"]),
  );
  await waitFor(() => expect(within(screen.getByTestId("gateway-map")).getAllByTestId("map-node")).toHaveLength(453));
  expect(screen.queryByTestId("map-partial")).toBeNull();
});

it("a walk still loading says how many paths it shows so far", async () => {
  let release: () => void = () => {};
  const gate = new Promise<void>((r) => {
    release = r;
  });
  serve((u) => page(pageOf(u), 300));
  const base = vi.mocked(fetch).getMockImplementation() as typeof fetch;
  vi.mocked(fetch).mockImplementation(async (input) => {
    const u = new URL(String(input), "http://ui");
    if (u.pathname.endsWith("/paths") && pageOf(u) > 0) await gate;
    return base(input);
  });
  mount("?tab=map");
  expect(await screen.findByTestId("map-partial")).toHaveTextContent("Showing 200 of 300 paths. Loading the rest.");
  release();
  await waitFor(() => expect(screen.queryByTestId("map-partial")).toBeNull());
});

it("a Gateway at the threshold opens unfiltered, and a state in the URL is kept", async () => {
  serve((u) => {
    const p = page(pageOf(u), 60);
    return { ...p, summary: { ...p.summary, routes: 60, backends: 60 } };
  });
  mount("?tab=map&state=error");
  await screen.findByTestId("gateway-map");
  expect(new URLSearchParams(screen.getByTestId("location").textContent ?? "").get("state")).toBe("error");
  expect(screen.queryByTestId("map-large")).toBeNull();
});

it("the side panel of a backend with no ready endpoints says what fails through it and links to detail and Traffic", async () => {
  const cart = (edgePaths().rows.find((r) => r.backend?.name === "cart-svc") as PathRow).backend as LinkedRef;
  const signal = {
    kind: "finding" as const,
    code: "KGWUI-023",
    title: "No ready endpoints",
    ref: cart,
    state: "warning" as const,
  };
  serve(() => {
    const b = edgePaths();
    const rows = b.rows.map((r) =>
      r.backend?.name === "cart-svc"
        ? {
            ...r,
            endpoints: { source: "slices", readAt: now(), complete: "complete" as const, ready: 0, total: 2 },
            problem: { state: "warning" as const, signals: [signal] },
          }
        : r,
    );
    return { ...b, rows };
  });
  mount("?tab=map");
  const map = await screen.findByTestId("gateway-map");
  const user = userEvent.setup();
  // d3-zoom's mousedown handler needs a real window; a click alone selects.
  fireEvent.click(within(flowNode(map, "shop/cart-svc")).getByTestId("map-node"));
  const panel = await screen.findByRole("complementary", { name: "Node details" });
  expect(within(panel).getByRole("heading", { name: "shop/cart-svc" })).toBeVisible();
  expect(panel).toHaveTextContent("Warning");
  const n = edgePaths().rows.filter((r) => r.backend?.name === "cart-svc").length;
  expect(within(panel).getByTestId("map-panel-counts")).toHaveTextContent(
    `0 of ${n} paths through this node fail${n} of ${n} paths through this node have a warning`,
  );
  expect(within(panel).getByTestId("map-panel-problems")).toHaveTextContent(
    "No ready endpoints (Service shop/cart-svc)",
  );
  const port = edgePaths().rows.find((r) => r.backend?.name === "cart-svc")?.port;
  expect(within(panel).getByTestId("map-panel-endpoints")).toHaveTextContent(`Port ${port}: 0/2 ready`);
  expect(within(panel).getByRole("link", { name: "Open details" })).toHaveAttribute(
    "href",
    `/i/${ID}/backends/-/Service/shop/cart-svc`,
  );
  expect(within(panel).getByRole("link", { name: "Show these paths in Traffic" })).toHaveAttribute(
    "href",
    `/i/${ID}/traffic?${new URLSearchParams({ gateway: "infra/edge", backend: "core/Service/shop/cart-svc" })}`,
  );
  // The panel takes focus, in its own column beside the canvas.
  expect(panel).toHaveFocus();
  expect(panel.parentElement).toBe(map.parentElement);
  // The selected node keeps its paths lit; closing the panel clears both.
  expect(flowNode(map, "shop/backends")).not.toHaveClass("dim");
  expect(flowNode(map, "shop/api")).toHaveClass("dim");
  await user.click(within(panel).getByRole("button", { name: "Close details" }));
  expect(screen.queryByTestId("map-panel")).toBeNull();
  // Focus goes back to the node.
  await waitFor(() => expect(flowNode(map, "shop/cart-svc")).toHaveFocus());
  expect(map.querySelectorAll(".react-flow__node.dim")).toHaveLength(0);
});

it("a route whose backend fails reads healthy itself, and its panel counts the failing paths through it", async () => {
  const be1 = edgePaths().rows.find((r) => r.backend?.name === "be1") as PathRow;
  const signal = {
    kind: "finding" as const,
    code: "KGWUI-002",
    title: "Backend not accepted",
    ref: be1.backend,
    state: "error" as const,
  };
  serve(() => {
    const b = edgePaths();
    return {
      ...b,
      rows: b.rows.map((r) =>
        r.backend?.name === "be1" ? { ...r, problem: { state: "error" as const, signals: [signal] } } : r,
      ),
    };
  });
  mount("?tab=map");
  const map = await screen.findByTestId("gateway-map");
  fireEvent.click(within(flowNode(map, "shop/backends")).getByTestId("map-node"));
  const panel = await screen.findByRole("complementary", { name: "Node details" });
  const through = edgePaths().rows.filter((r) => r.routes[0]?.name === "backends").length;
  expect(panel).toHaveTextContent("Healthy");
  expect(within(panel).getByTestId("map-panel-counts")).toHaveTextContent(
    `1 of ${through} paths through this node fail`,
  );
  expect(within(panel).getByText("Nothing on this node itself.")).toBeVisible();
});

it("chips update the server query live and live in the URL", async () => {
  serve(() => edgePaths());
  mount("?tab=map");
  await screen.findByTestId("gateway-map");
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Problems only" }));
  await waitFor(() =>
    expect(pathCalls().at(-1)?.searchParams.getAll("state")).toEqual(["error", "warning", "unknown"]),
  );
  await user.click(
    within(screen.getByRole("group", { name: "Route kind" })).getByRole("button", { name: "GRPCRoute" }),
  );
  await waitFor(() => expect(pathCalls().at(-1)?.searchParams.get("kind")).toBe("GRPCRoute"));
  // Namespace chips come from the summary's facets.
  await user.click(
    within(screen.getByRole("group", { name: "Route namespaces" })).getByRole("button", { name: "shop" }),
  );
  await waitFor(() => expect(pathCalls().at(-1)?.searchParams.get("namespace")).toBe("shop"));
  const url = new URLSearchParams(screen.getByTestId("location").textContent ?? "");
  expect([url.get("tab"), url.get("state"), url.get("kind"), url.get("namespace")]).toEqual([
    "map",
    "error,warning,unknown",
    "GRPCRoute",
    "shop",
  ]);
});

it("find selects the first node whose name matches, and Enter moves to the next", async () => {
  serve(() => edgePaths());
  mount("?tab=map");
  await screen.findByTestId("gateway-map");
  const user = userEvent.setup();
  const box = screen.getByRole("searchbox", { name: "Find a node" });
  await user.type(box, "SVC");
  const svc = nodeNames(screen.getByTestId("gateway-map")).filter((n) => n.toLowerCase().includes("svc"));
  expect(svc.length).toBeGreaterThan(1);
  const heading = async () =>
    within(await screen.findByRole("complementary", { name: "Node details" })).getByRole("heading", { level: 2 });
  expect(await heading()).toHaveTextContent(svc[0] as string);
  // A find keeps focus in the box, so typing goes on.
  expect(box).toHaveFocus();
  expect(screen.getByTestId("map-found")).toHaveTextContent(`1 of ${svc.length}`);
  await user.type(box, "{Enter}");
  expect(await heading()).toHaveTextContent(svc[1] as string);
  expect(screen.getByTestId("map-found")).toHaveTextContent(`2 of ${svc.length}`);
  await user.clear(box);
  await user.type(box, "nothing-here");
  expect(screen.getByTestId("map-found")).toHaveTextContent("No match");
});

it("a result that kept changing shows a quiet notice", async () => {
  let calls = 0;
  serve((u) => {
    const n = pageOf(u);
    const p = page(n, 300);
    return { ...p, summary: { ...p.summary, paths: 300 + calls++ } };
  });
  mount("?tab=map");
  expect(await screen.findByTestId("map-inconsistent")).toHaveTextContent(
    "The paths changed while loading. The map may be out of date.",
  );
  // The kept walk ended on a page with more after it, but no walk runs now: no "Loading the rest".
  await waitFor(() => expect(screen.getByTestId("map-partial")).toHaveTextContent(/^Showing 200 of [\d,]+ paths\.$/));
});

it("a node with one healthy path says so in the singular", async () => {
  serve(() => edgePaths());
  mount("?tab=map");
  const map = await screen.findByTestId("gateway-map");
  expect(edgePaths().rows.filter((r) => r.routes[0]?.name === "via-ls")).toHaveLength(1);
  fireEvent.click(within(flowNode(map, "shop/via-ls")).getByTestId("map-node"));
  expect(await screen.findByTestId("map-panel-counts")).toHaveTextContent(/^Its one path is healthy$/);
});

it("Escape closes the panel from the map and from the panel", async () => {
  serve(() => edgePaths());
  mount("?tab=map");
  const map = await screen.findByTestId("gateway-map");
  const user = userEvent.setup();
  fireEvent.click(within(flowNode(map, "shop/api")).getByTestId("map-node"));
  expect(await screen.findByTestId("map-panel")).toHaveFocus();
  await user.keyboard("{Escape}");
  expect(screen.queryByTestId("map-panel")).toBeNull();
  await waitFor(() => expect(flowNode(map, "shop/api")).toHaveFocus());
  // Again, with focus on a map node.
  fireEvent.click(within(flowNode(map, "shop/api")).getByTestId("map-node"));
  await screen.findByTestId("map-panel");
  flowNode(map, "shop/backends").focus();
  await user.keyboard("{Escape}");
  expect(screen.queryByTestId("map-panel")).toBeNull();
});

// region Task 46: keyboard navigation and table view

/** The name on a React Flow node wrapper's card. */
const nameOf = (el: Element | null) => el?.querySelector(".name")?.textContent ?? "";

it("arrow keys move focus along connections, focus lights the paths, Enter opens the panel and Escape returns", async () => {
  serve(() => edgePaths());
  mount("?tab=map");
  const map = await screen.findByTestId("gateway-map");
  const user = userEvent.setup();
  // One tab stop: the Gateway, until another node takes focus.
  const stops = [...map.querySelectorAll<HTMLElement>(".react-flow__node")].filter((n) => n.tabIndex === 0);
  expect(stops.map(nameOf)).toEqual(["infra/edge"]);
  stops[0]?.focus();
  await user.keyboard("{ArrowRight}");
  const rows = edgePaths().rows;
  const listener = nameOf(document.activeElement);
  expect(rows.map((r) => r.listener.name)).toContain(listener);
  // Up and down stay in the listeners column.
  await user.keyboard("{ArrowDown}{ArrowUp}");
  expect(new Set(rows.map((r) => r.listener.name))).toContain(nameOf(document.activeElement));
  const back = nameOf(document.activeElement);
  await user.keyboard("{ArrowRight}");
  const route = nameOf(document.activeElement);
  // The route is a root route attached to the focused listener.
  expect(rows.some((r) => r.listener.name === back && refName(r.routes[0] as LinkedRef) === route)).toBe(true);
  expect((document.activeElement as HTMLElement).tabIndex).toBe(0);
  // Keyboard focus lights the paths through the node, as hover does.
  const want = new Set(
    rows
      .filter((r) => r.routes.some((x) => refName(x) === route))
      .flatMap((r) => [
        refName(r.gateway),
        r.listener.name,
        ...r.routes.map(refName),
        ...(r.backend ? [refName(r.backend)] : []),
      ]),
  );
  const lit = nodeNames(map).filter((n) => !flowNode(map, n).classList.contains("dim"));
  // Action nodes (a rule with no backend) carry no ref name, so they are left out of the comparison.
  expect(new Set(lit.filter((n) => n.includes("/") || rows.some((r) => r.listener.name === n)))).toEqual(want);
  await user.keyboard("{Enter}");
  const panel = await screen.findByRole("complementary", { name: "Node details" });
  expect(within(panel).getByRole("heading", { level: 2 })).toHaveTextContent(route);
  expect(panel).toHaveFocus();
  await user.keyboard("{Escape}");
  expect(screen.queryByTestId("map-panel")).toBeNull();
  await waitFor(() => expect(nameOf(document.activeElement)).toBe(route));
});

it("each node's accessible name gives its kind, name, state and failing paths", async () => {
  serve(() => {
    const b = edgePaths();
    const rows = b.rows.map((r) =>
      r.backend?.name === "cart-svc" && r.routes[0]?.name === "backends"
        ? { ...r, problem: { state: "error" as const, signals: [] } }
        : r,
    );
    return { ...b, rows };
  });
  mount("?tab=map");
  const map = await screen.findByTestId("gateway-map");
  // The failing row's state lands on its backend; the Gateway itself stays healthy but counts the failing path.
  expect(flowNode(map, "shop/cart-svc")).toHaveAttribute(
    "aria-label",
    "Service shop/cart-svc, Error, 1 of 2 paths through this node fail",
  );
  expect(flowNode(map, "infra/edge")).toHaveAttribute(
    "aria-label",
    "Gateway infra/edge, Healthy, 1 of 8 paths through this node fail",
  );
  expect(flowNode(map, "shop/via-ls")).toHaveAttribute("aria-label", "HTTPRoute shop/via-ls, Healthy");
  // The map's own key help replaces React Flow's default description, which offers to delete nodes.
  expect(map).toHaveTextContent("Arrow keys move along the paths. Enter opens details. Escape closes them.");
  expect(map).not.toHaveTextContent(/delete/i);
});

it("the table toggle shows the map's rows as a path table, keeps the filters and lives in the URL", async () => {
  serve(() => edgePaths());
  mount("?tab=map&kind=HTTPRoute");
  await screen.findByTestId("gateway-map");
  const user = userEvent.setup();
  const toggle = screen.getByRole("button", { name: "Table" });
  expect(toggle).toHaveAttribute("aria-pressed", "false");
  const calls = pathCalls().length;
  await user.click(toggle);
  const table = await screen.findByRole("table", { name: "Paths through this Gateway" });
  // The same rows as the map's query: one per distinct path.
  expect(within(table).getAllByRole("row").length - 1).toBe(dedupeRows(edgePaths().rows).length);
  expect(screen.queryByTestId("gateway-map")).toBeNull();
  expect(screen.getByRole("button", { name: "Table" })).toHaveAttribute("aria-pressed", "true");
  // The toggle refetches nothing: the table draws the rows the map loaded.
  expect(pathCalls()).toHaveLength(calls);
  let url = new URLSearchParams(screen.getByTestId("location").textContent ?? "");
  expect([url.get("tab"), url.get("kind"), url.get("view")]).toEqual(["map", "HTTPRoute", "table"]);
  await user.click(screen.getByRole("button", { name: "Table" }));
  await screen.findByTestId("gateway-map");
  url = new URLSearchParams(screen.getByTestId("location").textContent ?? "");
  expect([url.get("tab"), url.get("kind"), url.get("view")]).toEqual(["map", "HTTPRoute", null]);
});
