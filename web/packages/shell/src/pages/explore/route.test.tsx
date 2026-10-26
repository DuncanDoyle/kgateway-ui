import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes } from "react-router-dom";
import metaGolden from "../../../../../../pkg/api/testdata/golden/meta.json";
import routeGolden from "../../../../../../pkg/api/testdata/golden/route-detail.json";
import candidateGolden from "../../../../../../pkg/api/testdata/golden/route-detail-candidate.json";
import restrictedGolden from "../../../../../../pkg/api/testdata/golden/route-detail-restricted.json";
import routesGolden from "../../../../../../pkg/api/testdata/golden/routes.json";
import type { Condition, RouteDetail, RouteList, RouteRow } from "../../api/types";
import { routeCopy } from "../../copy/explorer";
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
  // A hidden parent route keeps its name with no link (D-024); the walk stops there.
  const hidden = within(nav2).getAllByTestId("route-path")[3] as HTMLElement;
  expect(hidden).toHaveTextContent("shop/top1");
  expect(within(hidden).queryAllByRole("link")).toHaveLength(0);
  expect(within(nav2).getByText("More paths not shown")).toBeInTheDocument();
});

it("hostnames or inherited text for a child", async () => {
  serve(api());
  const first = mount(MID);
  await loaded();
  // The route's own hostnames; the parent card's effective hostnames repeat it.
  expect(screen.getAllByText("mid.example.com").length).toBeGreaterThan(0);
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

it("permission line: Allows any parent route / Allows: shop/api", async () => {
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
  expect(screen.getByText("Allows any parent route")).toBeInTheDocument();
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
  expect(screen.getByText("No reported child routes.")).toBeInTheDocument();
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
  expect(screen.getByText("No reported path to a Gateway or ListenerSet.")).toBeInTheDocument();
});

it("a parent without a status entry links from its spec target; a hidden one is text", async () => {
  // A declared candidate: spec parentRefs, no status entries. top1–4 are hidden in this golden.
  const declared = detail(restrictedGolden);
  declared.parents = [];
  serve(api({ mid: declared }));
  mount(MID);
  await loaded();
  const edge = screen.getByRole("article", { name: "Parent infra/edge" });
  expect(within(edge).getByRole("link", { name: "infra/edge" })).toHaveAttribute(
    "href",
    `/i/${ID}/gateways/infra/edge`,
  );
  expect(within(edge).getByText("No status from this install's controllers.")).toBeInTheDocument();
  const hidden = screen.getByRole("article", { name: "Parent shop/top1" });
  expect(within(hidden).queryByRole("link")).toBeNull();
  expect(hidden).toHaveTextContent(/^HTTPRouteshop\/top1No status from this install's controllers\.$/);
});

it("an implicit child shows a card for each status entry its parentRefs do not name", async () => {
  const child = detail(routeGolden);
  const cond = (type: string, status: string, reason: string): Condition => ({
    type,
    status,
    reason,
    message: `${type} ${reason}`,
    observedGeneration: child.generation,
    lastTransitionTime: "2026-10-23T09:00:00Z",
    stale: false,
  });
  const parentRef = { group: GW, kind: "HTTPRoute", namespace: "shop", name: "api" };
  child.parentRefs = [];
  child.parents = [
    {
      parentRef,
      target: { ...parentRef, link: "detail" },
      controllerName: "kgateway.dev/kgateway",
      ours: true,
      conditions: [cond("Accepted", "False", "UnsupportedValue"), cond("ResolvedRefs", "True", "ResolvedRefs")],
    },
  ];
  serve(api({ mid: child }));
  const first = mount(MID);
  await loaded();
  const card = screen.getByRole("article", { name: "Implicit parent shop/api" });
  expect(within(card).getByText("Implicit parent")).toBeInTheDocument();
  expect(within(card).getByTestId("condition-Accepted")).toHaveTextContent("Accepted: False");
  expect(within(card).getByTestId("condition-Accepted")).toHaveTextContent("UnsupportedValue");
  // The link follows the entry's ref.
  expect(within(card).getByRole("link", { name: "shop/api" })).toHaveAttribute(
    "href",
    `/i/${ID}/routes/${GW}/HTTPRoute/shop/api`,
  );
  expect(screen.queryByText("No parentRefs.")).toBeNull();
  first.unmount();

  // With parentRefs, an entry none of them names gets its own card too.
  const left = structuredClone(child);
  left.parentRefs = [{ group: GW, kind: "Gateway", namespace: "infra", name: "edge" }];
  serve(api({ mid: left }));
  mount(MID);
  await loaded();
  expect(screen.getByRole("article", { name: "Parent infra/edge" })).toBeInTheDocument();
  const extra = screen.getByRole("article", { name: "Parent shop/api, not in parentRefs" });
  expect(within(extra).getByText("Not in parentRefs")).toBeInTheDocument();
  expect(within(extra).getByTestId("condition-Accepted")).toHaveTextContent("UnsupportedValue");
});

it("stale route data turns every state on the page unknown", async () => {
  serve(api({ mid: { ...detail(routeGolden), collectedAt: new Date(Date.now() - 10 * 60_000).toISOString() } }));
  mount(MID);
  await loaded();
  await screen.findByText(/Data is stale/);
  expect(within(screen.getByTestId("detail-header")).getByText("Unknown")).toBeInTheDocument();
  const states = Array.from(document.querySelectorAll('[data-testid^="health-icon-"]')).map((e) =>
    e.getAttribute("data-testid"),
  );
  expect(states.length).toBeGreaterThan(2);
  expect(new Set(states)).toEqual(new Set(["health-icon-unknown"]));
  // Delegation states follow the stale view: unknown text with the unknown icon.
  expect(backend("child")).toHaveTextContent("Unknown");
  expect(backend("child")).not.toHaveTextContent("Reported by the controller");
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
  // A current Accepted=True reads "Configuration accepted", never "working".
  expect(within(backend("be1")).getByTestId("condition-Accepted")).toHaveTextContent("Configuration accepted");
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

it("matches render as written; an explicit empty list is not the default match", async () => {
  const d = detail(routeGolden);
  (d.rules[0] as RouteDetail["rules"][number]).matches = [];
  serve(api({ mid: d }));
  mount(MID);
  await loaded();
  // Explicit matches: [] reaches the SPA empty and keeps its own line (computed-rules §3.2, §6).
  expect(within(rule(1)).queryByTestId("match")).toBeNull();
  expect(within(rule(1)).getByTestId("match-empty")).toHaveTextContent(routeCopy.emptyMatches);
  expect(rule(1)).not.toHaveTextContent("PathPrefix /");
  // Omitted matches arrive with the CRD default filled in, shown as written.
  expect(within(rule(2)).getByTestId("match")).toHaveTextContent("PathPrefix /");
  expect(within(rule(2)).queryByTestId("match-empty")).toBeNull();
  const m = within(rule(3)).getByTestId("match");
  expect(m).toHaveTextContent("PathPrefix /");
  expect(m).toHaveTextContent("Header x-env Exact prod");
  expect(m).toHaveTextContent("Query q RegularExpression .*");
  expect(m).toHaveTextContent("Method GET");
  expect(within(rule(3)).queryByTestId("match-empty")).toBeNull();
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
    "A backend may be missing, or a delegation ref may select no route.",
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

/** mid's detail with Backend be1's state and Accepted condition replaced. */
function withBe1(state: "healthy" | "unknown", accepted: Partial<Condition>) {
  const d = detail(routeGolden);
  for (const r of d.rules)
    for (const b of r.backendRefs)
      if (b.ref.name === "be1" && b.backend?.accepted)
        b.backend = { state, accepted: { ...b.backend.accepted, ...accepted } };
  return d;
}

it("an evidence-less Backend condition shows the unknown icon and says the version is not reported", async () => {
  serve(api({ mid: withBe1("unknown", { observedGeneration: 0 }) }));
  mount(MID);
  await loaded();
  const c = within(backend("be1")).getByTestId("condition-Accepted");
  expect(c).toHaveTextContent("Accepted: True");
  expect(c).toHaveTextContent("The controller does not say which version it checked.");
  expect(c).not.toHaveTextContent("Configuration accepted");
  expect(within(c).getByTestId("health-icon-unknown")).toBeInTheDocument();
  expect(within(c).queryByTestId("health-icon-healthy")).toBeNull();
});

it("a stale Backend condition shows the unknown icon, not Configuration accepted", async () => {
  serve(api({ mid: withBe1("unknown", { stale: true }) }));
  mount(MID);
  await loaded();
  const c = within(backend("be1")).getByTestId("condition-Accepted");
  expect(c).toHaveTextContent("Accepted: True");
  expect(c).toHaveTextContent("Describes an older generation of the Backend.");
  expect(within(c).queryByTestId("health-icon-healthy")).toBeNull();
});

// region View A (2.3)
it("parent card lists computed listeners, one line each, with effective hostnames not checked by the controller", async () => {
  const d = detail(routeGolden);
  const a = d.attachments?.[0];
  if (!a) throw new Error("golden has no attachment");
  const first = a.result.listeners[0];
  if (!first) throw new Error("golden has no listener");
  // Server-shaped: a parentRef that matches two listeners adds not-verified to each member (explain.go memberCoverage).
  const coverage: ("count-agrees" | "status-compatible" | "not-verified")[] = [
    "count-agrees",
    "status-compatible",
    "not-verified",
  ];
  first.computed = { ...first.computed, coverage };
  a.result.listeners.push({
    ...structuredClone(first),
    listener: "https",
    effectiveHostnames: ["mid.example.com", "www.example.com"],
  });
  serve(api({ mid: d }));
  mount(MID);
  await loaded();
  const card = screen.getByRole("article", { name: "Parent infra/edge" });
  // One Computed chip marks the listeners block; each listener line has no chip of its own.
  const block = within(card).getByTestId("computed-listeners");
  expect(within(block).getAllByText("Computed")).toHaveLength(1);
  const http = within(card).getByTestId("computed-listener-http");
  const https = within(card).getByTestId("computed-listener-https");
  expect(http).toHaveTextContent("mid.example.com");
  expect(http).not.toHaveTextContent("www.example.com");
  expect(https).toHaveTextContent("mid.example.com, www.example.com");
  for (const l of [http, https]) {
    // One short state per line; the coverage detail lives in the tooltip.
    expect(within(l).getByText("Agrees")).toBeInTheDocument();
    expect(l).not.toHaveTextContent("Count agrees with the controller");
  }
  // "The controller does not report this" never shows twice: not on the card at all, and once in a listener's tooltip.
  expect(within(card).queryAllByText("The controller does not report this")).toHaveLength(0);
  const state = within(http).getByText("Agrees").closest("[tabindex]") as HTMLElement;
  for (let i = 0; i < 120 && document.activeElement !== state; i++) await userEvent.tab();
  expect(state).toHaveFocus();
  const tipText = screen.getByRole("tooltip").textContent ?? "";
  expect(tipText.split("The controller does not report this")).toHaveLength(2);
  expect(tipText).toContain("The controller does not report effective hostnames.");
  // The listener links to the Gateway page with that listener selected.
  expect(within(http).getByRole("link", { name: "http" })).toHaveAttribute(
    "href",
    `/i/${ID}/gateways/infra/edge?listener=http`,
  );
  // The parentRef's computed outcome, with its own badge.
  expect(within(card).getByTestId("computed-outcome")).toHaveTextContent("Accepted");
});

it("a parent card's unknown or restricted listener shows no hostnames", async () => {
  const d = detail(routeGolden);
  const a = d.attachments?.[0];
  if (!a) throw new Error("golden has no attachment");
  a.result = {
    listeners: [],
    computed: { by: "ui", rules: "kgateway-2.4", rule: "hostname-intersection", state: "restricted" },
  };
  serve(api({ mid: d }));
  mount(MID);
  await loaded();
  const card = screen.getByRole("article", { name: "Parent infra/edge" });
  const outcome = within(card).getByTestId("computed-outcome");
  expect(outcome).toHaveTextContent("Hidden: depends on objects you cannot view");
  expect(within(card).queryByTestId("computed-listeners")).toBeNull();
});

it("a failed parentRef shows its computed explanation", async () => {
  const d = detail(routeGolden);
  const a = d.attachments?.[0];
  if (!a) throw new Error("golden has no attachment");
  a.result = {
    outcome: "NoMatchingListenerHostname",
    listeners: [],
    explanation: "listener https has *.example.org, route has shop.example.com",
    computed: {
      by: "ui",
      rules: "kgateway-2.4",
      rule: "hostname-intersection",
      state: "checked",
      coverage: ["status-compatible"],
    },
  };
  serve(api({ mid: d }));
  mount(MID);
  await loaded();
  const outcome = within(screen.getByRole("article", { name: "Parent infra/edge" })).getByTestId("computed-outcome");
  expect(outcome).toHaveTextContent("listener https has *.example.org, route has shop.example.com");
  expect(within(outcome).getByTestId("computed-badge")).toHaveTextContent("Consistent with the status");
});
