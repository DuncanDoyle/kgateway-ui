import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { Route, Routes } from "react-router-dom";
import metaGolden from "../../../../../../pkg/api/testdata/golden/meta.json";
import routeGolden from "../../../../../../pkg/api/testdata/golden/route-detail.json";
import candidateGolden from "../../../../../../pkg/api/testdata/golden/route-detail-candidate.json";
import restrictedGolden from "../../../../../../pkg/api/testdata/golden/route-detail-restricted.json";
import routesGolden from "../../../../../../pkg/api/testdata/golden/routes.json";
import type { RouteDetail, RouteList, RouteRow } from "../../api/types";
import { TABS_H } from "../../explore/DetailTabs";
import { ROW_H } from "../../explore/ObjectTable";
import { pseudoRules, renderWithTheme } from "../../test/render";
import { controlRoomDark, controlRoomLight } from "../../theme/themes";
import { HEADER_H } from "./detailParts";
import { RoutePage } from "./RoutePage";

const ID = "kgw.kgateway";
const GW = "gateway.networking.k8s.io";
const API = `/api/v1/installs/${ID}`;
const MID = `/i/${ID}/routes/${GW}/HTTPRoute/shop/mid`;
const FAR = `/i/${ID}/routes/${GW}/HTTPRoute/blog/far`;
const MID_PARAM = `${GW}/HTTPRoute/shop/mid`;
const now = () => new Date().toISOString();
const detail = (g: { body: unknown }): RouteDetail =>
  structuredClone({ ...(g.body as RouteDetail), collectedAt: now() });
const row = (name: string): RouteRow =>
  structuredClone((routesGolden.body as RouteList).items.find((r) => r.ref.name === name) as RouteRow);

/** Two reported children of mid, one per page, as the list sends them under parent=mid. */
function childPage(cursor: string | null): RouteList {
  const asChild = (r: RouteRow): RouteRow => ({
    ...r,
    parents: [
      { ref: { group: GW, kind: "HTTPRoute", namespace: "shop", name: "mid", link: "detail" }, attachment: "accepted" },
    ],
  });
  return cursor
    ? { install: ID, items: [asChild(row("cart"))], total: 2, prev: "p1", collectedAt: now() }
    : { install: ID, items: [asChild(row("child"))], total: 2, next: "c2", collectedAt: now() };
}

type Reply = unknown | [number, unknown];
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
const routeCalls = () => calls().filter((u) => u.pathname === `${API}/routes`);

function api(over: { mid?: RouteDetail } = {}) {
  return (u: URL): Reply | undefined => {
    if (u.pathname === "/api/v1/meta") return metaGolden.body;
    if (u.pathname === `${API}/routes/${GW}/HTTPRoute/shop/mid`) return over.mid ?? detail(routeGolden);
    if (u.pathname === `${API}/routes/${GW}/HTTPRoute/blog/far`) return detail(candidateGolden);
    if (u.pathname === `${API}/routes`) return childPage(u.searchParams.get("cursor"));
    return undefined;
  };
}

function mount(path: string, themeId?: string) {
  return renderWithTheme(
    <Routes>
      <Route path="/i/:id/routes/:group/:kind/:ns/:name" element={<RoutePage />} />
      <Route path="*" element={<p>elsewhere</p>} />
    </Routes>,
    { path, themeId },
  );
}
const loaded = () => screen.findByRole("navigation", { name: "Route parents" });
const rule = (n: number) => screen.getByRole("region", { name: new RegExp(`^Rule ${n}\\b`) });
/** A backendRef by name: delegation refs sit in rule 1, the rest in rule 2. */
const backend = (name: string) =>
  within(rule(["child", "declared", "ghost"].includes(name) ? 1 : 2)).getByTestId(`backend-${name}`);

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("up-chain shows a restricted step and 'More paths not shown'", async () => {
  serve(api());
  const first = mount(MID);
  const nav = await loaded();
  const paths = within(nav).getAllByTestId("route-path");
  expect(paths).toHaveLength(5);
  // Root first; each step links where its link says; the route itself is the current page.
  const edge = within(paths[0] as HTMLElement).getByRole("link", { name: /infra\/edge/ });
  expect(edge).toHaveAttribute("href", `/i/${ID}/gateways/infra/edge`);
  expect(
    within(paths[0] as HTMLElement)
      .getByLabelText("shop/mid")
      .closest("[aria-current]"),
  ).toHaveAttribute("aria-current", "page");
  expect(within(paths[1] as HTMLElement).getByText("Cycle")).toBeInTheDocument();
  expect(within(nav).getByText("More paths not shown")).toBeInTheDocument();
  first.unmount();

  serve(api({ mid: detail(restrictedGolden) }));
  mount(MID);
  const nav2 = await loaded();
  const restricted = within(nav2).getAllByTestId("route-path")[3] as HTMLElement;
  expect(restricted).toHaveTextContent("A parent you cannot view");
  // A restricted step has no identity: nothing but the visible route on that path.
  expect(within(restricted).queryAllByRole("link")).toHaveLength(0);
  expect(within(nav2).queryByText("More paths not shown")).toBeNull();
});

it("hostnames or inherited text for a child", async () => {
  serve(api());
  const first = mount(MID);
  await loaded();
  expect(screen.getByText("mid.example.com")).toBeInTheDocument();
  first.unmount();

  const child = detail(routeGolden);
  child.hostnames = [];
  serve(api({ mid: child }));
  const second = mount(MID);
  await loaded();
  expect(screen.getByText("Inherits the parent route's hostnames")).toBeInTheDocument();
  second.unmount();

  const top = detail(routeGolden);
  top.hostnames = [];
  top.parentRefs = top.parentRefs.filter((p) => p.kind === "Gateway");
  top.parents = top.parents.filter((p) => p.target.kind === "Gateway");
  serve(api({ mid: top }));
  mount(MID);
  await loaded();
  expect(screen.getByText("Inherits the listener's hostnames")).toBeInTheDocument();
});

it("permission line: Allows any parent / Allows: shop/api", async () => {
  serve(api());
  const first = mount(MID);
  await loaded();
  expect(
    screen.getByText("Allows: shop/loop, shop/top1, shop/top2, shop/top3, shop/top4, shop/top5"),
  ).toBeInTheDocument();
  first.unmount();

  const open = detail(routeGolden);
  open.parentRefs = [];
  serve(api({ mid: open }));
  const second = mount(MID);
  await loaded();
  expect(screen.getByText("Allows any parent")).toBeInTheDocument();
  second.unmount();

  // Only non-route parentRefs: no parent route may delegate here.
  const gwOnly = detail(routeGolden);
  gwOnly.parentRefs = gwOnly.parentRefs.filter((p) => p.kind === "Gateway");
  serve(api({ mid: gwOnly }));
  mount(MID);
  await loaded();
  expect(screen.getByText("Allows no parent route")).toBeInTheDocument();
});

it("child routes are paged and fetched when expanded", async () => {
  let release: (() => void) | undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  const reply = api();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const u = new URL(url, "http://ui");
      if (u.pathname === `${API}/routes`) await gate;
      return new Response(JSON.stringify(reply(u)), { status: 200 });
    }),
  );
  mount(MID);
  await loaded();
  // Nothing fetched until the section opens.
  expect(routeCalls()).toHaveLength(0);
  const toggle = screen.getByRole("button", { name: "Show child routes" });
  expect(toggle).toHaveAttribute("aria-expanded", "false");
  fireEvent.click(toggle);
  expect(toggle).toHaveAttribute("aria-expanded", "true");
  const t = await screen.findByRole("table", { name: "Child routes" });
  // childCount 2; declared candidates are never children, so declaredCount does not count.
  expect(within(t).getAllByTestId("skeleton-row")).toHaveLength(2);
  expect(getComputedStyle(within(t).getAllByTestId("skeleton-row")[0] as HTMLElement).height).toBe(`${ROW_H}px`);
  await waitFor(() => expect(routeCalls()).toHaveLength(1));
  const q = routeCalls()[0] as URL;
  expect(q.searchParams.get("parent")).toBe(MID_PARAM);
  expect(q.searchParams.has("attachment")).toBe(false);
  await act(async () => release?.());
  expect(await within(t).findByLabelText("shop/child")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Next page" }));
  expect(await within(t).findByLabelText("shop/cart")).toBeInTheDocument();
  expect(routeCalls().at(-1)?.searchParams.get("cursor")).toBe("c2");
});

it("a route with no children says so and fetches nothing", async () => {
  const none = detail(routeGolden);
  // A declared candidate alone is not a child.
  none.childCount = 0;
  none.declaredCount = 1;
  serve(api({ mid: none }));
  mount(MID);
  await loaded();
  expect(screen.getByText("No child routes.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Show child routes" })).toBeNull();
  expect(routeCalls()).toHaveLength(0);
});

it("other controllers' entries are collapsed", async () => {
  serve(api());
  mount(FAR);
  await loaded();
  expect(screen.getByTestId("candidate-banner")).toHaveTextContent("HTTPRoute shop/mid refers to it.");
  const card = screen.getByRole("article", { name: "Parent infra/foreign" });
  // The foreign Gateway is not admitted: its name is text.
  expect(within(card).queryByRole("link")).toBeNull();
  expect(within(card).getByTestId("condition-Accepted")).toHaveTextContent("Accepted: True");
  expect(within(card).queryByText("example.com/other")).toBeNull();
  const more = within(card).getByRole("button", { name: "Other controllers (1)" });
  expect(more).toHaveAttribute("aria-expanded", "false");
  fireEvent.click(more);
  expect(more).toHaveAttribute("aria-expanded", "true");
  expect(within(card).getByText("example.com/other")).toBeInTheDocument();
  // An empty up-chain says so.
  expect(screen.getByText("No reported path to a Gateway.")).toBeInTheDocument();
});

it("each backendRef links to Other routes using this backend (routes?backend=…)", async () => {
  serve(api());
  mount(MID);
  await loaded();
  const lookup = (name: string) =>
    new URL(
      within(backend(name)).getByRole("link", { name: "Other routes using this backend" }).getAttribute("href") ?? "",
      "http://ui",
    );
  expect(lookup("cart-svc").pathname).toBe(`/i/${ID}/routes`);
  expect(lookup("cart-svc").searchParams.get("backend")).toBe("core/Service/shop/cart-svc");
  expect(lookup("be1").searchParams.get("backend")).toBe("gateway.kgateway.dev/Backend/shop/be1");
  // A Backend links to its YAML view and shows its state.
  expect(within(backend("be1")).getByRole("link", { name: /shop\/be1/ })).toHaveAttribute(
    "href",
    `/i/${ID}/r/gateway.kgateway.dev/Backend/shop/be1`,
  );
  expect(within(backend("be1")).getByTestId("condition-Accepted")).toHaveTextContent("Accepted: True");
  // Services have no page: their names are text.
  expect(within(backend("cart-svc")).queryAllByRole("link")).toHaveLength(1);
  // Delegation refs never match the backend filter, so they get no lookup.
  const deleg = rule(1);
  expect(within(deleg).queryByRole("link", { name: "Other routes using this backend" })).toBeNull();
});

it("delegation refs show their mode and status state", async () => {
  serve(api());
  mount(MID);
  await loaded();
  const r = rule(1);
  expect(within(backend("child")).getByRole("link", { name: /shop\/child/ })).toHaveAttribute(
    "href",
    `/i/${ID}/routes/${GW}/HTTPRoute/shop/child`,
  );
  expect(backend("child")).toHaveTextContent("Reported by the controller");
  expect(backend("declared")).toHaveTextContent("Not reported by the controller");
  expect(backend("ghost")).toHaveTextContent("No such route");
  expect(within(backend("ghost")).queryByRole("link")).toBeNull();
  expect(r).toHaveTextContent("Every route in shop");
  expect(r).toHaveTextContent("Routes in shop with label team");
});

it("absent weight shows 1 marked as defaulted; absent port shows nothing", async () => {
  serve(api());
  mount(MID);
  await loaded();
  const weight = (name: string) => within(backend(name)).getByTestId("weight").textContent;
  expect(weight("cart-svc")).toBe("Weight 1 (default)");
  expect(weight("w1")).toBe("Weight 1");
  expect(weight("w0")).toBe("Weight 0");
  expect(within(backend("w1")).getByText("Port 8080")).toBeInTheDocument();
  expect(within(backend("w0")).queryByText(/Port/)).toBeNull();
});

it("mirror filters show their target and percentage; filters keep written order", async () => {
  serve(api());
  mount(MID);
  await loaded();
  const filters = within(rule(2)).getAllByTestId("filter");
  // The rule's own filters, then w1's; each in written order.
  expect(filters.map((f) => f.dataset.type)).toEqual([
    "RequestHeaderModifier",
    "RequestMirror",
    "RequestMirror",
    "ExtensionRef",
    "ResponseHeaderModifier",
    "RequestMirror",
  ]);
  expect(filters[1]).toHaveTextContent("shop/mirror-svc");
  expect(filters[1]).toHaveTextContent("Port 80");
  expect(filters[1]).toHaveTextContent("10%");
  expect(filters[2]).toHaveTextContent("shop/mirror-frac");
  expect(filters[2]).toHaveTextContent("1/100");
  expect(filters[3]).toHaveTextContent("shop/tp");
  // Mirror targets get no reverse-lookup link: only backendRefs do.
  for (const f of filters)
    expect(within(f).queryByRole("link", { name: "Other routes using this backend" })).toBeNull();
  // Config as written, as YAML.
  expect(filters[0]).toHaveTextContent("x-a");
  expect(within(backend("w1")).getAllByTestId("filter")).toHaveLength(2);
});

it("matches render as written, and an empty list as the default match", async () => {
  const d = detail(routeGolden);
  (d.rules[0] as RouteDetail["rules"][number]).matches = [];
  serve(api({ mid: d }));
  mount(MID);
  await loaded();
  // An explicit empty list is match-all: shown as the CRD default, never as "matches nothing".
  expect(within(rule(1)).getByTestId("match")).toHaveTextContent("PathPrefix /");
  expect(within(rule(1)).getByText("Default match")).toBeInTheDocument();
  expect(rule(1)).not.toHaveTextContent(/nothing/i);
  const m = within(rule(3)).getByTestId("match");
  expect(m).toHaveTextContent("PathPrefix /");
  expect(m).toHaveTextContent("Header x-env Exact prod");
  expect(m).toHaveTextContent("Query q RegularExpression .*");
  expect(m).toHaveTextContent("Method GET");
  expect(within(rule(3)).queryByText("Default match")).toBeNull();
});

it("restricted objects show no names or data", async () => {
  serve(api({ mid: detail(restrictedGolden) }));
  mount(MID);
  await loaded();
  // A hidden Backend: its spec ref as text, "Restricted", no condition.
  const be1 = backend("be1");
  expect(be1).toHaveTextContent("Restricted");
  expect(within(be1).queryByTestId("condition-Accepted")).toBeNull();
  expect(within(be1).queryByRole("link", { name: /shop\/be1/ })).toBeNull();
  // The backend filter cannot probe (§7.4 rule 2a), so a hidden Backend keeps its lookup link.
  const look = within(be1).getByRole("link", { name: "Other routes using this backend" });
  expect(new URL(look.getAttribute("href") ?? "", "http://ui").searchParams.get("backend")).toBe(
    "gateway.kgateway.dev/Backend/shop/be1",
  );
  // A hidden child of a name ref: written name as text, state Restricted.
  expect(within(backend("child")).queryByRole("link", { name: /shop\/child/ })).toBeNull();
  expect(backend("child")).toHaveTextContent("Restricted");
  // A hidden parent route: its card names the parentRef as written, without a link.
  const card = screen.getByRole("article", { name: "Parent shop/top1" });
  expect(within(card).queryByRole("link")).toBeNull();
});

it("a BackendNotFound next to a delegation ref gets a hedged note", async () => {
  const d = detail(routeGolden);
  const p = d.parents[0] as RouteDetail["parents"][number];
  p.conditions = p.conditions.map((c) =>
    c.type === "ResolvedRefs" ? { ...c, status: "False", reason: "BackendNotFound", message: "missing" } : c,
  );
  serve(api({ mid: d }));
  mount(MID);
  await loaded();
  const card = screen.getByRole("article", { name: "Parent infra/edge" });
  expect(within(card).getByTestId("condition-ResolvedRefs")).toHaveTextContent(
    "This can mean a missing backend or a delegation ref that selects no route.",
  );
});

it("the header and tabs keep their size from loading to loaded", async () => {
  let release: (() => void) | undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  const reply = api();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      await gate;
      return new Response(JSON.stringify(reply(new URL(url, "http://ui"))), { status: 200 });
    }),
  );
  mount(MID);
  expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("shop/mid");
  expect(screen.getByTestId("detail-skeleton")).toBeInTheDocument();
  const header = () => getComputedStyle(screen.getByTestId("detail-header")).minHeight;
  const tabs = () => getComputedStyle(screen.getByTestId("detail-tabs")).height;
  expect(header()).toBe(`${HEADER_H}px`);
  expect(tabs()).toBe(`${TABS_H}px`);
  await act(async () => release?.());
  await loaded();
  expect(header()).toBe(`${HEADER_H}px`);
  expect(tabs()).toBe(`${TABS_H}px`);
});

describe.each([controlRoomLight, controlRoomDark])("interactive styles in $id", (theme) => {
  it("up-chain links, the lookup link and the expanders have hover, active and focus-visible styles", async () => {
    serve(api());
    mount(FAR, theme.id);
    await loaded();
    const more = screen.getByRole("button", { name: "Other controllers (1)" });
    more.focus();
    expect(more).toHaveFocus();
    const els: HTMLElement[] = [more];
    serve(api());
    const r = mount(MID, theme.id);
    const nav = await within(r.container).findByRole("navigation", { name: "Route parents" });
    const step = within(nav).getAllByRole("link")[0] as HTMLElement;
    step.focus();
    expect(step).toHaveFocus();
    els.push(
      step,
      within(r.container).getByRole("button", { name: "Show child routes" }),
      within(r.container).getAllByRole("link", { name: "Other routes using this backend" })[0] as HTMLElement,
    );
    for (const el of els)
      for (const pseudo of [":hover", ":active", ":focus-visible"])
        expect(
          pseudoRules(el, pseudo).join("").length,
          `${el.textContent || el.getAttribute("aria-label")} ${pseudo}`,
        ).toBeGreaterThan(0);
  });
});
