import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useEffect } from "react";
import { Route, Routes, useLocation, useNavigationType } from "react-router-dom";
import gatewaysGolden from "../../../../../../pkg/api/testdata/golden/gateways.json";
import listenersetsGolden from "../../../../../../pkg/api/testdata/golden/listenersets.json";
import routesGolden from "../../../../../../pkg/api/testdata/golden/routes.json";
import routesByBackendGolden from "../../../../../../pkg/api/testdata/golden/routes-by-backend.json";
import routesRestrictedGolden from "../../../../../../pkg/api/testdata/golden/routes-restricted.json";
import type { GatewayList, ListenerSetList, RouteList, RouteRow } from "../../api/types";
import { FOOTER_H, ROW_H } from "../../explore/ObjectTable";
import { pseudoRules, renderWithTheme } from "../../test/render";
import { controlRoomDark, controlRoomLight } from "../../theme/themes";
import { GatewaysPage } from "./GatewaysPage";
import { ListenerSetsPage } from "./ListenerSetsPage";
import { RoutesPage } from "./RoutesPage";

const ID = "kgw.kgateway";
const GW = "gateway.networking.k8s.io";
const fresh = <T extends { collectedAt: string }>(b: T): T => ({ ...b, collectedAt: new Date().toISOString() });
const routes = () => fresh(routesGolden.body as RouteList);

type Reply = unknown | [number, unknown];
/** Stubs fetch with a handler over the parsed request URL; undefined is a 404. */
function serve(fn: (u: URL) => Reply | undefined) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const r = fn(new URL(url, "http://ui"));
      if (r === undefined) return new Response(JSON.stringify({ error: "not found" }), { status: 404 });
      const [status, body] = Array.isArray(r) && typeof r[0] === "number" ? (r as [number, unknown]) : [200, r];
      return new Response(JSON.stringify(body), { status });
    }),
  );
}
const calls = () => vi.mocked(fetch).mock.calls.map((c) => new URL(String(c[0]), "http://ui"));
const listCalls = (list: string) => calls().filter((u) => u.pathname === `/api/v1/installs/${ID}/${list}`);

// Every navigation's type; none is a PUSH when the page only replaces, so history keeps its length.
const navTypes: string[] = [];
function Where() {
  const loc = useLocation();
  const type = useNavigationType();
  useEffect(() => {
    navTypes.push(`${type} ${loc.key}`);
  }, [loc.key, type]);
  return <output data-testid="location">{loc.pathname + loc.search}</output>;
}
const location = () => screen.getByTestId("location").textContent ?? "";
const search = () => new URLSearchParams(location().split("?")[1] ?? "");

function mount(path: string, themeId?: string) {
  return renderWithTheme(
    <Routes>
      <Route path="/i/:id/routes" element={<RoutesPage />} />
      <Route path="/i/:id/gateways" element={<GatewaysPage />} />
      <Route path="/i/:id/listenersets" element={<ListenerSetsPage />} />
      <Route path="/i/:id/r/:group/:kind/:rns/:rname" element={<p>yaml view</p>} />
      <Route path="/i/:id/routes/:group/:kind/:ns/:name" element={<p>route detail</p>} />
      <Route path="*" element={null} />
    </Routes>,
    { path, themeId },
  ).container;
}
// The location probe sits outside the page routes so it is always there.
function mountWithLocation(path: string) {
  return renderWithTheme(
    <>
      <Routes>
        <Route path="/i/:id/routes" element={<RoutesPage />} />
        <Route path="/i/:id/gateways" element={<GatewaysPage />} />
        <Route path="/i/:id/r/:group/:kind/:rns/:rname" element={<p>yaml view</p>} />
      </Routes>
      <Where />
    </>,
    { path },
  );
}

const rowOf = (name: string) => screen.getAllByRole("row").find((r) => within(r).queryByText(name)) as HTMLElement;

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("filters, sort and cursor round-trip through the URL", async () => {
  serve((u) => (u.pathname.endsWith("/routes") ? { ...routes(), next: "n2", prev: "p0" } : undefined));
  const path = `/i/${ID}/routes?namespace=shop&state=declared&backend=core/Service/shop/cart&sort=health.desc&cursor=c1&x=ignored`;
  const first = mountWithLocation(path);
  await screen.findByText("rejected");
  // Only documented params reach the API, repeatable ones as repeated keys.
  const q = listCalls("routes")[0]?.searchParams;
  expect([...(q?.entries() ?? [])]).toEqual([
    ["namespace", "shop"],
    ["state", "declared"],
    ["backend", "core/Service/shop/cart"],
    ["sort", "health.desc"],
    ["cursor", "c1"],
  ]);
  expect(screen.getByRole("columnheader", { name: /Status/ })).toHaveAttribute("aria-sort", "descending");
  expect(screen.getByRole("button", { name: /No status entry for the parent route/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  expect(screen.getByText("Namespace: shop")).toBeInTheDocument();
  // A reload restores the same view from the URL.
  const saved = location();
  first.unmount();
  vi.mocked(fetch).mockClear();
  mountWithLocation(saved);
  await screen.findByText("rejected");
  expect(listCalls("routes")[0]?.searchParams.getAll("state")).toEqual(["declared"]);
  expect(screen.getByRole("columnheader", { name: /Status/ })).toHaveAttribute("aria-sort", "descending");
  // Sorting resets the page: a cursor belongs to one sort.
  await userEvent.click(screen.getByRole("button", { name: /^Route/ }));
  await waitFor(() => expect(search().get("sort")).toBe("name.asc"));
  expect(search().has("cursor")).toBe(false);
  // The pager writes the server's cursor.
  await userEvent.click(screen.getByRole("button", { name: "Next page" }));
  await waitFor(() => expect(search().get("cursor")).toBe("n2"));
  await userEvent.click(screen.getByRole("button", { name: "Previous page" }));
  await waitFor(() => expect(search().get("cursor")).toBe("p0"));
  // A filter change also drops the cursor; health is repeatable.
  await userEvent.click(screen.getByRole("button", { name: /Error/ }));
  await userEvent.click(screen.getByRole("button", { name: /Warning/ }));
  await waitFor(() => expect(search().get("health")).toBe("error,warning"));
  expect(search().has("cursor")).toBe(false);
  await waitFor(() => expect(listCalls("routes").at(-1)?.searchParams.getAll("health")).toEqual(["error", "warning"]));
  // Removing a URL-only filter.
  await userEvent.click(screen.getByRole("button", { name: "Remove filter Namespace: shop" }));
  await waitFor(() => expect(search().has("namespace")).toBe(false));
});

it("a rejected cursor is dropped from the URL and page one shows", async () => {
  serve((u) => {
    if (!u.pathname.endsWith("/routes")) return undefined;
    return u.searchParams.has("cursor") ? [400, { error: "bad cursor" }] : routes();
  });
  navTypes.length = 0;
  mountWithLocation(`/i/${ID}/routes?sort=name.desc&cursor=stale`);
  await screen.findByText("rejected");
  await waitFor(() => expect(search().has("cursor")).toBe(false));
  // Dropped by replace: history length is unchanged.
  expect(navTypes.some((t) => t.startsWith("REPLACE"))).toBe(true);
  expect(navTypes.some((t) => t.startsWith("PUSH"))).toBe(false);
  expect(search().get("sort")).toBe("name.desc");
  expect(screen.queryByTestId("page-notice")).toBeNull();
});

it("a filter change keeps the rows on screen until the new page arrives", async () => {
  let release: (() => void) | undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("health=")) await gate;
      return new Response(JSON.stringify(routes()), { status: 200 });
    }),
  );
  mount(`/i/${ID}/routes`);
  await screen.findByText("rejected");
  await userEvent.click(screen.getByRole("button", { name: /Error/ }));
  await waitFor(() => expect(vi.mocked(fetch).mock.calls.some((c) => String(c[0]).includes("health="))).toBe(true));
  expect(screen.queryAllByTestId("skeleton-row")).toHaveLength(0);
  expect(screen.getByText("rejected")).toBeInTheDocument();
  await act(async () => release?.());
});

it("while a new query loads, the old rows are marked busy and dimmed", async () => {
  let release: (() => void) | undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("health=")) await gate;
      return new Response(JSON.stringify(routes()), { status: 200 });
    }),
  );
  mount(`/i/${ID}/routes`);
  await screen.findByText("rejected");
  const table = screen.getByRole("table");
  expect(table).toHaveAttribute("aria-busy", "false");
  await userEvent.click(screen.getByRole("button", { name: /Error/ }));
  await waitFor(() => expect(table).toHaveAttribute("aria-busy", "true"));
  const body = table.querySelector("tbody") as HTMLElement;
  expect(getComputedStyle(body).opacity).toBe("0.55");
  await act(async () => release?.());
  await waitFor(() => expect(table).toHaveAttribute("aria-busy", "false"));
  expect(getComputedStyle(body).opacity).not.toBe("0.55");
});

it("an error on a new query never shows the previous query's rows", async () => {
  serve((u) =>
    u.pathname.endsWith("/routes") ? (u.searchParams.has("health") ? [500, { error: "boom" }] : routes()) : undefined,
  );
  mount(`/i/${ID}/routes`);
  await screen.findByText("rejected");
  await userEvent.click(screen.getByRole("button", { name: /Error/ }));
  expect(await screen.findByRole("heading", { name: "Cannot load routes" })).toBeInTheDocument();
  expect(screen.queryByText("rejected")).toBeNull();
  expect(screen.queryByText("11 routes")).toBeNull();
  expect(screen.queryByTestId("data-age")).toBeNull();
});

it("a failed query keeps its title and filters, and Clear filters recovers", async () => {
  serve((u) =>
    u.pathname.endsWith("/routes") ? (u.searchParams.has("health") ? [500, { error: "boom" }] : routes()) : undefined,
  );
  mountWithLocation(`/i/${ID}/routes`);
  await screen.findByText("rejected");
  await userEvent.click(screen.getByRole("button", { name: /Error/ }));
  expect(await screen.findByRole("heading", { name: "Cannot load routes" })).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "Routes", level: 1 })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /Error/ })).toHaveAttribute("aria-pressed", "true");
  expect(screen.queryByTestId("page-notice")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "Clear filters" }));
  expect(await screen.findByText("rejected")).toBeInTheDocument();
  expect(search().has("health")).toBe(false);
});

it("the pager hides after load when the page has no cursors, and the footer keeps its height", async () => {
  serve((u) => (u.pathname.endsWith("/gateways") ? fresh(gatewaysGolden.body as GatewayList) : undefined));
  mount(`/i/${ID}/gateways`);
  await screen.findByText("edge");
  expect(screen.queryByRole("button", { name: "Previous page" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Next page" })).toBeNull();
  expect(getComputedStyle(screen.getByTestId("list-footer")).height).toBe(`${FOOTER_H}px`);
});

it("row hover underlines only the row's own name, not other links", async () => {
  serve((u) => (u.pathname.endsWith("/listenersets") ? fresh(listenersetsGolden.body as ListenerSetList) : undefined));
  mount(`/i/${ID}/listenersets`);
  await screen.findByText("partial");
  const css = [...document.querySelectorAll("style")].map((s) => s.textContent ?? "").join("");
  const underlines = [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)].filter(
    ([, sel = "", body = ""]) => sel.includes("tr:hover") && body.includes("underline"),
  );
  expect(underlines.length).toBeGreaterThan(0);
  for (const [, sel = ""] of underlines) expect(sel).toContain('td[data-col="name"]');
  expect(rowOf("partial").querySelector('td[data-col="parent"] a')).not.toBeNull();
});

it("a long route name is one truncated line with the full name in a keyboard tooltip", async () => {
  const long = "a-route-with-a-deliberately-long-name-that-must-not-wrap-or-shift-the-table";
  const body = routes();
  const first = body.items[0] as RouteRow;
  body.items = [{ ...first, ref: { ...first.ref, name: long } }];
  const full = `${first.ref.namespace}/${long}`;
  serve((u) => (u.pathname.endsWith("/routes") ? body : undefined));
  mount(`/i/${ID}/routes`);
  const name = await screen.findByLabelText(full);
  const style = getComputedStyle(name);
  expect([style.whiteSpace, style.overflow, style.textOverflow]).toEqual(["nowrap", "hidden", "ellipsis"]);
  const link = name.closest("a") as HTMLElement;
  for (let i = 0; i < 50 && document.activeElement !== link; i++) await userEvent.tab();
  expect(link).toHaveFocus();
  expect(screen.getByRole("tooltip")).toHaveTextContent(full);
  expect(link).toHaveAttribute("aria-describedby", screen.getByRole("tooltip").id);
});

it("text search reaches the API as q", async () => {
  serve((u) => (u.pathname.endsWith("/gateways") ? fresh(gatewaysGolden.body as GatewayList) : undefined));
  mountWithLocation(`/i/${ID}/gateways`);
  await screen.findByText("edge");
  await userEvent.type(screen.getByRole("searchbox", { name: "Filter Gateways" }), "ed");
  await waitFor(() => expect(listCalls("gateways").at(-1)?.searchParams.get("q")).toBe("ed"));
  expect(search().get("q")).toBe("ed");
  // Gateways take no route-only params.
  expect(listCalls("gateways").every((u) => !u.searchParams.has("state"))).toBe(true);
});

it("backend filter shows a Mirror label on mirror rows", async () => {
  serve((u) => (u.pathname.endsWith("/routes") ? fresh(routesByBackendGolden.body as RouteList) : undefined));
  mount(`/i/${ID}/routes?backend=core/Service/shop/mirror-svc`);
  await screen.findByText("mirror-only");
  expect(within(rowOf("mirror-only")).getByText("Mirror")).toBeInTheDocument();
  expect(within(rowOf("backends")).queryByText("Mirror")).toBeNull();
});

const yamlRow = (name: string, kind: string, flags: string[] = []): RouteRow => ({
  ref: { group: GW, kind, namespace: "shop", name, link: "yaml" },
  hostnames: [],
  parents: [],
  parentRoutes: [],
  children: 0,
  flags,
  health: "healthy",
});

it("GRPCRoute rows open the YAML view", async () => {
  serve((u) => (u.pathname.endsWith("/routes") ? { ...routes(), items: [yamlRow("grpc", "GRPCRoute")] } : undefined));
  mount(`/i/${ID}/routes`);
  await userEvent.click(within(await screen.findByRole("table")).getByText("GRPCRoute"));
  expect(await screen.findByText("yaml view")).toBeInTheDocument();
});

it("rows link by their link field", async () => {
  const none = {
    ...yamlRow("hidden", "HTTPRoute"),
    ref: { ...yamlRow("hidden", "HTTPRoute").ref, link: "none" as const },
  };
  serve((u) =>
    u.pathname.endsWith("/routes")
      ? {
          ...routes(),
          items: [routes().items[2], yamlRow("grpc", "GRPCRoute"), yamlRow("orphan", "HTTPRoute", ["no-parent"]), none],
        }
      : undefined,
  );
  mount(`/i/${ID}/routes`);
  await screen.findByText("orphan");
  const href = (name: string) =>
    within(rowOf(name))
      .queryByRole("link", { name: `shop/${name}` })
      ?.getAttribute("href");
  expect(href("api")).toBe(`/i/${ID}/routes/${GW}/HTTPRoute/shop/api`);
  expect(href("grpc")).toBe(`/i/${ID}/r/${GW}/GRPCRoute/shop/grpc`);
  expect(href("orphan")).toBe(`/i/${ID}/r/${GW}/HTTPRoute/shop/orphan`);
  expect(href("hidden")).toBeUndefined();
  // Keyboard: arrows move between rows, Enter opens the focused row.
  const api = rowOf("api");
  api.focus();
  fireEvent.keyDown(api, { key: "ArrowDown" });
  expect(rowOf("grpc")).toHaveFocus();
  fireEvent.keyDown(rowOf("grpc"), { key: "ArrowDown" });
  fireEvent.keyDown(rowOf("orphan"), { key: "Enter" });
  expect(await screen.findByText("yaml view")).toBeInTheDocument();
});

describe("shows each list state", () => {
  it("loading skeleton rows have the loaded row height, and the footer is reserved", async () => {
    let release: (() => void) | undefined;
    const gate = new Promise<void>((r) => {
      release = r;
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        await gate;
        return new Response(JSON.stringify(routesRestricted()), { status: 200 });
      }),
    );
    mount(`/i/${ID}/routes`);
    const skeleton = screen.getAllByTestId("skeleton-row");
    expect(skeleton.length).toBeGreaterThan(0);
    expect(screen.getByRole("table")).toHaveAttribute("aria-busy", "true");
    const footer = screen.getByTestId("list-footer");
    const footerH = getComputedStyle(footer).height;
    const skeletonH = getComputedStyle(skeleton[0] as HTMLElement).height;
    expect(skeletonH).toBe(`${ROW_H}px`);
    // The pager is there, disabled, before the data arrives.
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
    await act(async () => release?.());
    await screen.findByText("rejected");
    expect(getComputedStyle(rowOf("rejected")).height).toBe(skeletonH);
    expect(getComputedStyle(screen.getByTestId("list-footer")).height).toBe(footerH);
    // The denied line is said once per page.
    expect(screen.getAllByText("You cannot view 2 routes in this install.")).toHaveLength(1);
  });

  it("empty with Clear filters", async () => {
    serve((u) => (u.pathname.endsWith("/routes") ? { ...routes(), items: [], total: 0 } : undefined));
    mountWithLocation(`/i/${ID}/routes?state=pending&q=zzz&sort=kind.asc`);
    expect(await screen.findByText("No routes match these filters.")).toBeInTheDocument();
    const clear = within(screen.getByRole("table")).getByRole("button", { name: "Clear filters" });
    await userEvent.click(clear);
    await waitFor(() => expect(location()).toBe(`/i/${ID}/routes?sort=kind.asc`));
  });

  it("empty without filters offers no Clear filters", async () => {
    serve((u) => (u.pathname.endsWith("/routes") ? { ...routes(), items: [], total: 0 } : undefined));
    mount(`/i/${ID}/routes`);
    expect(await screen.findByText("No routes in this install.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Clear filters" })).toBeNull();
  });

  it("a failed poll of the same query keeps the rows under a PageNotice", async () => {
    let fail = false;
    serve((u) => (u.pathname.endsWith("/routes") ? (fail ? [500, { error: "boom" }] : routes()) : undefined));
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mount(`/i/${ID}/routes`);
    await screen.findByText("rejected");
    fail = true;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5100);
    });
    expect(await screen.findByTestId("page-notice")).toHaveTextContent("Cannot load routes: boom");
    expect(screen.getByText("rejected")).toBeInTheDocument();
    // Unconfirmed data is not evidence: the badges turn unknown, as on the install page.
    expect(within(rowOf("rejected")).getByText("Unknown")).toBeInTheDocument();
  });

  it("404 says the install is out of scope, with a link back", async () => {
    serve(() => undefined);
    mount(`/i/${ID}/listenersets`);
    expect(await screen.findByText("Not in this install's scope")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to install" })).toHaveAttribute("href", `/i/${ID}`);
  });

  it("stale data turns every badge unknown", async () => {
    serve((u) =>
      u.pathname.endsWith("/gateways")
        ? { ...(gatewaysGolden.body as GatewayList), collectedAt: "2020-01-01T00:00:00Z" }
        : undefined,
    );
    mount(`/i/${ID}/gateways`);
    await screen.findByText("edge");
    expect(within(rowOf("edge")).getByText("Unknown")).toBeInTheDocument();
    expect(within(rowOf("edge")).queryByText("Healthy")).toBeNull();
    expect(screen.getByTestId("data-age")).toHaveTextContent("stale");
  });
});

const routesRestricted = () => fresh(routesRestrictedGolden.body as RouteList);

it("Routes filter 'Rules replaced or dropped' maps to state=not-programmed", async () => {
  serve((u) => (u.pathname.endsWith("/routes") ? routes() : undefined));
  mountWithLocation(`/i/${ID}/routes`);
  await screen.findByText("rejected");
  await userEvent.click(screen.getByRole("button", { name: "Rules replaced or dropped" }));
  await waitFor(() => expect(search().get("state")).toBe("not-programmed"));
  await waitFor(() => expect(listCalls("routes").at(-1)?.searchParams.getAll("state")).toEqual(["not-programmed"]));
});

it("renders the ListenerSets list with parent links", async () => {
  serve((u) => (u.pathname.endsWith("/listenersets") ? fresh(listenersetsGolden.body as ListenerSetList) : undefined));
  mount(`/i/${ID}/listenersets?sort=kind.desc`);
  await screen.findByText("partial");
  expect(within(rowOf("partial")).getByRole("link", { name: "infra/edge" })).toHaveAttribute(
    "href",
    `/i/${ID}/gateways/infra/edge`,
  );
  expect(listCalls("listenersets")[0]?.searchParams.get("sort")).toBe("kind.desc");
  expect(screen.getByRole("columnheader", { name: /Kind/ })).toHaveAttribute("aria-sort", "descending");
});

it("parent cells say how each parent took the route, in words", async () => {
  serve((u) => (u.pathname.endsWith("/routes") ? routes() : undefined));
  mount(`/i/${ID}/routes`);
  await screen.findByText("rejected");
  expect(within(rowOf("rejected")).getByText("Not accepted")).toBeInTheDocument();
  // The raw condition reason is never shown as a code.
  expect(within(rowOf("rejected")).queryByText("UnsupportedValue")).toBeNull();
  expect(within(rowOf("via-ls")).getByText(/on Gateway/)).toHaveTextContent("on Gateway infra/edge");
});

describe("below 900 px", () => {
  beforeEach(() => {
    vi.stubGlobal("matchMedia", (q: string) => ({
      matches: q.includes("max-width"),
      addEventListener: () => {},
      removeEventListener: () => {},
    }));
  });

  it("moves secondary columns into an expandable row", async () => {
    serve((u) => (u.pathname.endsWith("/gateways") ? fresh(gatewaysGolden.body as GatewayList) : undefined));
    mount(`/i/${ID}/gateways`);
    await screen.findByText("edge");
    expect(screen.queryByRole("columnheader", { name: /Addresses/ })).toBeNull();
    const toggle = within(rowOf("edge")).getByRole("button", { name: "Show details for infra/edge" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    const details = screen.getByTestId("row-details");
    expect(within(details).getByText("Addresses")).toBeInTheDocument();
    expect(within(details).getByText("10.0.0.1")).toBeInTheDocument();
  });
});

describe.each([controlRoomDark, controlRoomLight])("in the $name theme", (t) => {
  it("every interactive element has hover, active and focus-visible styles", async () => {
    vi.stubGlobal("matchMedia", (q: string) => ({
      matches: q.includes("max-width"),
      addEventListener: () => {},
      removeEventListener: () => {},
    }));
    serve((u) => (u.pathname.endsWith("/routes") ? { ...routes(), next: "n" } : undefined));
    mount(`/i/${ID}/routes?state=pending`, t.id);
    await screen.findByText("rejected");
    const els = {
      row: rowOf("rejected"),
      sort: screen.getByRole("button", { name: /^Route/ }),
      pager: screen.getByRole("button", { name: "Next page" }),
      filter: screen.getByRole("button", { name: "Rules replaced or dropped" }),
      search: screen.getByRole("searchbox", { name: "Filter routes" }),
      clear: screen.getByRole("button", { name: "Clear filters" }),
      expand: within(rowOf("rejected")).getByRole("button", { name: /Show details/ }),
    };
    for (const [name, el] of Object.entries(els)) {
      // The rule may sit on the element or on a styled ancestor (table, bar).
      const rules = (p: string) => {
        let out: string[] = [];
        for (let n: Element | null = el; n && out.length === 0; n = n.parentElement) out = pseudoRules(n, p);
        return out.join();
      };
      expect(rules(":hover"), `${name} :hover`).toContain(t.color.text);
      // A text box has no pressed state.
      if (name !== "search") expect(rules(":active"), `${name} :active`).toContain(t.color.text);
      expect(rules(":focus-visible"), `${name} :focus-visible`).toContain(t.color.focus);
    }
  });
});

it("URL filter tags show refs as Kind ns/name and attachments in words", async () => {
  serve((u) => (u.pathname.endsWith("/routes") ? routes() : undefined));
  const p = new URLSearchParams({
    parent: "gateway.networking.k8s.io/HTTPRoute/shop/api",
    attachment: "not-accepted",
    backend: "gateway.kgateway.dev/Backend/shop/be1",
    kind: "GRPCRoute",
  });
  mount(`/i/${ID}/routes?${p}`);
  await screen.findByText("rejected");
  expect(screen.getByText("Parent: HTTPRoute shop/api")).toBeInTheDocument();
  expect(screen.getByText("Attachment: Not accepted")).toBeInTheDocument();
  expect(screen.getByText("Backend: Backend shop/be1")).toBeInTheDocument();
  expect(screen.getByText("Kind: GRPCRoute")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Remove filter Parent: HTTPRoute shop/api" })).toBeInTheDocument();
});

it("a tag value that is not a full ref stays as written", async () => {
  serve((u) => (u.pathname.endsWith("/routes") ? routes() : undefined));
  mount(`/i/${ID}/routes?backend=shop/be1`);
  await screen.findByText("rejected");
  expect(screen.getByText("Backend: shop/be1")).toBeInTheDocument();
});

// region View A (2.3)
const meta = { capabilities: [], kinds: [], filters: { routes: ["listener"] } };

it("Routes state filter offers disagrees and listener-unknown", async () => {
  serve((u) => (u.pathname === "/api/v1/meta" ? meta : u.pathname.endsWith("/routes") ? routes() : undefined));
  mountWithLocation(`/i/${ID}/routes`);
  await screen.findByText("rejected");
  const group = screen.getByRole("group", { name: "Route state" });
  await userEvent.click(await within(group).findByRole("button", { name: "UI disagrees with the controller" }));
  await waitFor(() => expect(search().get("state")).toBe("disagrees"));
  await userEvent.click(within(group).getByRole("button", { name: "Listener unknown" }));
  await waitFor(() =>
    expect(listCalls("routes").at(-1)?.searchParams.getAll("state")).toEqual(["disagrees", "listener-unknown"]),
  );
});

it("an older server is not offered the 2.3 state values", async () => {
  serve((u) =>
    u.pathname === "/api/v1/meta"
      ? { ...meta, filters: undefined }
      : u.pathname.endsWith("/routes")
        ? routes()
        : undefined,
  );
  mount(`/i/${ID}/routes`);
  await screen.findByText("rejected");
  const group = screen.getByRole("group", { name: "Route state" });
  expect(within(group).queryByRole("button", { name: "Listener unknown" })).toBeNull();
});

it("Gateways list has a sortable routes column with view A's count", async () => {
  const gws = () => {
    const b = fresh(gatewaysGolden.body as GatewayList);
    const first = b.items[0];
    if (!first) throw new Error("golden has no Gateway");
    return {
      ...b,
      items: [
        { ...first, disagreements: 1 },
        { ...first, ref: { ...first.ref, name: "hidden" }, routes: undefined },
      ],
    };
  };
  serve((u) => (u.pathname.endsWith("/gateways") ? gws() : undefined));
  mountWithLocation(`/i/${ID}/gateways`);
  await screen.findByText("hidden");
  const header = screen.getByRole("columnheader", { name: /^Routes\s*Computed$/ });
  // A computed column: the header carries the Computed chip; controller columns do not.
  expect(within(header).getByText("Computed")).toBeInTheDocument();
  expect(within(screen.getByRole("columnheader", { name: /Attached routes/ })).queryByText("Computed")).toBeNull();
  const edge = within(rowOf("edge")).getByTestId("routes-cell");
  expect(edge).toHaveTextContent("6");
  expect(edge).toHaveTextContent("1 disagrees");
  expect(within(edge).getByTestId("health-icon-warning")).toBeInTheDocument();
  // No count: no number shown as if known.
  expect(within(rowOf("hidden")).getByTestId("routes-cell")).toHaveTextContent("No count");
  await userEvent.click(within(header).getByRole("button", { name: /Routes/ }));
  await waitFor(() => expect(search().get("sort")).toBe("routes.asc"));
  await waitFor(() => expect(listCalls("gateways").at(-1)?.searchParams.get("sort")).toBe("routes.asc"));
  expect(screen.getByRole("columnheader", { name: /^Routes\s*Computed$/ })).toHaveAttribute("aria-sort", "ascending");
});

it("ListenerSets list shows view A's routes count", async () => {
  serve((u) => (u.pathname.endsWith("/listenersets") ? fresh(listenersetsGolden.body as ListenerSetList) : undefined));
  mount(`/i/${ID}/listenersets`);
  await screen.findByText("partial");
  expect(within(rowOf("ls1")).getByTestId("routes-cell")).toHaveTextContent("1");
  expect(within(rowOf("partial")).getByTestId("routes-cell")).toHaveTextContent("0");
});
