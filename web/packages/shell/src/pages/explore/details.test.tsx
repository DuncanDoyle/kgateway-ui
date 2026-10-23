import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes, useLocation } from "react-router-dom";
import gatewayGolden from "../../../../../../pkg/api/testdata/golden/gateway-detail.json";
import lsGolden from "../../../../../../pkg/api/testdata/golden/listenerset-detail.json";
import lsAllowedGolden from "../../../../../../pkg/api/testdata/golden/listenerset-detail-allowed.json";
import lsInvalidGolden from "../../../../../../pkg/api/testdata/golden/listenerset-detail-invalid.json";
import listenersetsGolden from "../../../../../../pkg/api/testdata/golden/listenersets.json";
import metaGolden from "../../../../../../pkg/api/testdata/golden/meta.json";
import routesGolden from "../../../../../../pkg/api/testdata/golden/routes.json";
import type { GatewayDetail, ListenerSetDetail, ListenerSetList, RouteList } from "../../api/types";
import { TABS_H } from "../../explore/DetailTabs";
import { ROW_H } from "../../explore/ObjectTable";
import { pseudoRules, renderWithTheme } from "../../test/render";
import { controlRoomDark, controlRoomLight } from "../../theme/themes";
import { HEADER_H } from "./detailParts";
import { GatewayPage } from "./GatewayPage";
import { ListenerSetPage } from "./ListenerSetPage";

const ID = "kgw.kgateway";
const GW = "gateway.networking.k8s.io";
const API = `/api/v1/installs/${ID}`;
const GW_PATH = `/i/${ID}/gateways/infra/edge`;
const LS_PATH = (name: string) => `/i/${ID}/listenersets/${GW}/ListenerSet/infra/${name}`;
const now = () => new Date().toISOString();
const gateway = (): GatewayDetail => ({ ...(gatewayGolden.body as GatewayDetail), collectedAt: now() });
const ls = (g: { body: unknown }): ListenerSetDetail => ({ ...(g.body as ListenerSetDetail), collectedAt: now() });
const routes = (): RouteList => ({ ...(routesGolden.body as RouteList), collectedAt: now() });
const lsList = (): ListenerSetList => ({ ...(listenersetsGolden.body as ListenerSetList), collectedAt: now() });
const yamlObj = {
  kind: "Gateway",
  metadata: { name: "edge", namespace: "infra" },
  spec: { gatewayClassName: "kgateway" },
};

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
const listCalls = (list: string) => calls().filter((u) => u.pathname === `${API}/${list}`);

/** The default API: the goldens, with overrides by detail body. */
function api(over: { gateway?: GatewayDetail; ls?: ListenerSetDetail } = {}) {
  return (u: URL): Reply | undefined => {
    if (u.pathname === "/api/v1/meta") return metaGolden.body;
    if (u.pathname === `${API}/gateways/infra/edge`) return over.gateway ?? gateway();
    if (u.pathname.startsWith(`${API}/listenersets/${GW}/ListenerSet/infra/`)) return over.ls ?? ls(lsGolden);
    if (u.pathname === `${API}/routes`) return routes();
    if (u.pathname === `${API}/listenersets`) return lsList();
    if (u.pathname.startsWith(`${API}/resources/`)) return yamlObj;
    return undefined;
  };
}

function Where() {
  const loc = useLocation();
  return <output data-testid="location">{loc.pathname + loc.search}</output>;
}
const location = () => screen.getByTestId("location").textContent ?? "";

function mount(path: string, themeId?: string) {
  return renderWithTheme(
    <>
      <Routes>
        <Route path="/i/:id/gateways/:ns/:name" element={<GatewayPage />} />
        <Route path="/i/:id/listenersets/:group/:kind/:ns/:name" element={<ListenerSetPage />} />
        <Route path="*" element={<p>elsewhere</p>} />
      </Routes>
      <Where />
    </>,
    { path, themeId },
  );
}
const table = (name: string) => screen.getByRole("table", { name });
const rowOf = (t: HTMLElement, text: string) =>
  within(t)
    .getAllByRole("row")
    .find((r) => within(r).queryByText(text)) as HTMLElement;

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("Gateway page shows status-only listener counts", async () => {
  serve(api());
  mount(GW_PATH);
  const listeners = await screen.findByRole("table", { name: "Listeners" });
  // attachedRoutes as the controller wrote them; no computed column in 2.2.
  const headers = within(listeners)
    .getAllByRole("columnheader")
    .map((h) => h.textContent);
  expect(headers).toContain("Attached routes");
  expect(headers.join(" ")).not.toMatch(/omputed/);
  expect(within(rowOf(listeners, "http")).getByText("3")).toBeInTheDocument();
  expect(within(rowOf(listeners, "https")).getByText("0")).toBeInTheDocument();
  // A listener the controller has not written status for says so, with no number.
  const fresh = rowOf(listeners, "new");
  expect(within(fresh).getByText("No status")).toBeInTheDocument();
  expect(within(fresh).getByText("Not reported")).toBeInTheDocument();
  expect(within(rowOf(listeners, "http")).getByText("Any hostname")).toBeInTheDocument();
  expect(within(rowOf(listeners, "http")).getByText("Namespaces with team=a")).toBeInTheDocument();
  // Certificates are names as text: Secrets never link.
  const https = rowOf(listeners, "https");
  expect(within(https).getByText("Terminate")).toBeInTheDocument();
  expect(within(https).queryByRole("link")).toBeNull();
  expect(https).toHaveTextContent("infra/edge-cert");
  // Gateway conditions: icon and text, the reason as written.
  const accepted = screen.getByTestId("condition-Accepted");
  expect(accepted).toHaveTextContent("Accepted: True");
  expect(within(accepted).getByTestId("health-icon-healthy")).toBeInTheDocument();
  expect(screen.getByText("Attached ListenerSets (status): 1")).toBeInTheDocument();
  expect(screen.getByText("kgateway")).toBeInTheDocument();
});

it("a stale condition says which generation it describes", async () => {
  const g = gateway();
  g.generation = 2;
  g.conditions = [{ ...(g.conditions[0] as GatewayDetail["conditions"][number]), stale: true }];
  serve(api({ gateway: g }));
  mount(GW_PATH);
  expect(await screen.findByText("Describes generation 1; the object is at 2.")).toBeInTheDocument();
});

it("failed-route table has Not accepted, Waiting for status and Unwatched namespace sub-filters", async () => {
  serve(api());
  mount(GW_PATH);
  await screen.findByRole("table", { name: "Routes that failed to attach" });
  const parent = `${GW}/Gateway/infra/edge`;
  const attachments = () =>
    listCalls("routes")
      .filter((u) => u.searchParams.get("parent") === parent)
      .map((u) => u.searchParams.getAll("attachment"));
  // Attached routes and the first sub-filter, each with exactly one attachment value (single-valued, §7.2).
  await waitFor(() => expect(attachments()).toEqual(expect.arrayContaining([["accepted"], ["not-accepted"]])));
  const group = screen.getByRole("group", { name: "Failed routes" });
  const chips = within(group).getAllByRole("button");
  expect(chips.map((c) => c.textContent)).toEqual([
    "Not accepted (1)",
    "Waiting for status (1)",
    "Unwatched namespace (1)",
  ]);
  expect(chips[0]).toHaveAttribute("aria-pressed", "true");
  // The deciding condition's reason as written, for this Gateway's parent entry.
  const failed = table("Routes that failed to attach");
  expect(within(rowOf(failed, "rejected")).getByText("NotAllowedByListeners")).toBeInTheDocument();
  // A pending reason is ours, not the controller's: it is never shown as a code.
  expect(within(failed).queryByText("no status from the controller")).toBeNull();
  await userEvent.click(within(group).getByRole("button", { name: /Waiting for status/ }));
  await waitFor(() => expect(attachments().at(-1)).toEqual(["pending"]));
  expect(within(group).getByRole("button", { name: /Waiting for status/ })).toHaveAttribute("aria-pressed", "true");
  await userEvent.click(within(group).getByRole("button", { name: /Unwatched namespace/ }));
  await waitFor(() => expect(attachments().at(-1)).toEqual(["unwatched"]));
  // Sub-tables never send unknown params; the server is strict.
  for (const u of listCalls("routes")) expect([...u.searchParams.keys()].sort()).toEqual(["attachment", "parent"]);
});

it("ListenerSets are paged through the list endpoint", async () => {
  serve((u) => {
    if (u.pathname === `${API}/listenersets`)
      return {
        ...lsList(),
        next: u.searchParams.get("cursor") ? undefined : "p2",
        prev: u.searchParams.get("cursor") ? "p1" : undefined,
      };
    return api()(u);
  });
  mount(GW_PATH);
  const t = await screen.findByRole("table", { name: "ListenerSets" });
  await within(t).findByText("partial");
  expect(listCalls("listenersets")[0]?.searchParams.get("parent")).toBe(`${GW}/Gateway/infra/edge`);
  const frame = t.closest("div") as HTMLElement;
  await userEvent.click(within(frame).getByRole("button", { name: "Next page" }));
  await waitFor(() => expect(listCalls("listenersets").at(-1)?.searchParams.get("cursor")).toBe("p2"));
  // The page URL does not carry a sub-table's cursor.
  expect(location()).toBe(GW_PATH);
  expect(within(rowOf(t, "partial")).getByRole("link", { name: "infra/partial" })).toHaveAttribute(
    "href",
    `/i/${ID}/listenersets/${GW}/ListenerSet/infra/partial`,
  );
});

it("ListenerSet page says when the parent Gateway is missing", async () => {
  // NotAllowed with a parent the caller cannot view: the server sends no allowedListeners; the UI also gates on parentFound.
  const base = ls(lsGolden);
  const hidden = { ...base, parentFound: false, parent: { ...base.parent, link: "none" as const } };
  for (const body of [{ ...hidden, parentAllowedListeners: undefined }, hidden]) {
    serve(api({ ls: body }));
    const view = mount(LS_PATH("na"));
    // Hidden and absent read alike, so the page says "not available", never "not found" (design §7.3).
    expect(await screen.findByText("Not available")).toBeInTheDocument();
    expect(screen.getByText("Not available").closest("dd")).toHaveTextContent("infra/edge");
    expect(screen.queryByText(/not found/i)).toBeNull();
    expect(screen.queryByRole("link", { name: "infra/edge" })).toBeNull();
    expect(screen.getByTestId("condition-Accepted")).toHaveTextContent("NotAllowed");
    expect(screen.queryByTestId("allowed-listeners")).toBeNull();
    view.unmount();
  }
});

it("a found parent Gateway links to its page", async () => {
  serve(api({ ls: ls(lsInvalidGolden) }));
  mount(LS_PATH("partial"));
  expect(await screen.findByRole("link", { name: "infra/edge" })).toHaveAttribute(
    "href",
    `/i/${ID}/gateways/infra/edge`,
  );
  expect(screen.queryByText("Not available")).toBeNull();
});

it("NotAllowed shows the parent's allowedListeners as written, or Not set with the CRD hint", async () => {
  serve(api({ ls: ls(lsGolden) }));
  const first = mount(LS_PATH("na"));
  const block = await screen.findByTestId("allowed-listeners");
  // The block sits with the NotAllowed condition.
  expect(screen.getByTestId("condition-Accepted")).toContainElement(block);
  // It sits in the text column, not the 16 px icon column.
  expect(getComputedStyle(block).gridColumn).toBe("2");
  expect(block).toHaveTextContent("Not set: the Gateway allows no ListenerSets.");
  expect(block).toHaveTextContent("the Gateway CRD in this cluster may not have the field");
  first.unmount();
  serve(api({ ls: ls(lsAllowedGolden) }));
  mount(LS_PATH("na"));
  const set = await screen.findByTestId("allowed-listeners");
  expect(set.querySelector("pre")).toHaveTextContent("namespaces: from: Same");
  expect(set).not.toHaveTextContent("Not set");
  expect(set).not.toHaveTextContent("CRD");
});

it("an accepted ListenerSet shows no allowed-listeners block", async () => {
  serve(api({ ls: ls(lsInvalidGolden) }));
  mount(LS_PATH("partial"));
  const listeners = await screen.findByRole("table", { name: "Listeners" });
  expect(screen.queryByTestId("allowed-listeners")).toBeNull();
  // The accepted set's invalid listener shows its condition on expand, reason as written.
  const row = rowOf(listeners, "extra");
  expect(within(row).getByText("Programmed: False")).toBeInTheDocument();
  await userEvent.click(within(row).getByRole("button", { name: "Show conditions for listener extra" }));
  const details = screen.getByTestId("listener-extra-details");
  expect(within(details).getByTestId("condition-Programmed")).toHaveTextContent("PortUnavailable");
  expect(
    within(within(details).getByTestId("condition-Programmed")).getByTestId("health-icon-error"),
  ).toBeInTheDocument();
});

it("an accepted ListenerSet never shows the block, even when the body carries allowedListeners", async () => {
  serve(api({ ls: { ...ls(lsInvalidGolden), parentAllowedListeners: { set: false, crdHint: true } } }));
  mount(LS_PATH("partial"));
  await screen.findByRole("table", { name: "Listeners" });
  expect(screen.getByTestId("condition-Accepted")).toHaveTextContent("Accepted: True");
  expect(screen.queryByTestId("allowed-listeners")).toBeNull();
});

it("candidate banner above the header", async () => {
  const g = {
    ...gateway(),
    candidate: {
      reason: "referenced" as const,
      by: { group: GW, kind: "HTTPRoute", namespace: "shop", name: "mid", link: "detail" as const },
    },
  };
  serve(api({ gateway: g }));
  mount(GW_PATH);
  const banner = await screen.findByTestId("candidate-banner");
  expect(banner).toHaveTextContent("Not part of this install. Shown because HTTPRoute shop/mid refers to it.");
  expect(within(banner).getByRole("link", { name: "shop/mid" })).toHaveAttribute(
    "href",
    `/i/${ID}/routes/${GW}/HTTPRoute/shop/mid`,
  );
  const header = screen.getByTestId("detail-header");
  expect(banner.compareDocumentPosition(header) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});

it("an owned object shows no candidate banner", async () => {
  serve(api());
  mount(GW_PATH);
  await screen.findByRole("table", { name: "Listeners" });
  expect(screen.queryByTestId("candidate-banner")).toBeNull();
});

it("polling keeps scroll, focus and expanded rows", async () => {
  // The second response changes one listener's count, so a reset keyed on the listeners would show.
  let polls = 0;
  const base = api();
  serve((u) => {
    if (u.pathname !== `${API}/gateways/infra/edge`) return base(u);
    const g = gateway();
    if (polls++ > 0)
      g.listeners = g.listeners.map((l) =>
        l.name === "http" && l.status ? { ...l, status: { ...l.status, attachedRoutes: 4 } } : l,
      );
    return g;
  });
  vi.useFakeTimers({ shouldAdvanceTime: true });
  const scrollTo = vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  mount(GW_PATH);
  const listeners = await screen.findByRole("table", { name: "Listeners" });
  const toggle = within(rowOf(listeners, "http")).getByRole("button", { name: "Show conditions for listener http" });
  fireEvent.click(toggle);
  toggle.focus();
  const heading = screen.getByRole("heading", { level: 1 });
  const before = vi.mocked(fetch).mock.calls.length;
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5100);
  });
  // A poll ran, and the page kept its nodes: nothing remounted, so scroll, focus and expansion stay.
  expect(vi.mocked(fetch).mock.calls.length).toBeGreaterThan(before);
  expect(await within(rowOf(listeners, "http")).findByText("4")).toBeInTheDocument();
  expect(screen.getByRole("heading", { level: 1 })).toBe(heading);
  expect(toggle).toHaveFocus();
  expect(toggle).toHaveAttribute("aria-expanded", "true");
  expect(screen.getByTestId("listener-http-details")).toBeInTheDocument();
  expect(scrollTo).not.toHaveBeenCalled();
});

it("tabs follow the WAI-ARIA pattern and ?tab=yaml survives reload", async () => {
  serve(api());
  const first = mount(GW_PATH);
  await screen.findByRole("table", { name: "Listeners" });
  const list = screen.getByRole("tablist", { name: "Gateway views" });
  const [overview, yaml] = within(list).getAllByRole("tab") as [HTMLElement, HTMLElement];
  expect(overview).toHaveAttribute("aria-selected", "true");
  expect(overview).toHaveAttribute("tabindex", "0");
  expect(yaml).toHaveAttribute("tabindex", "-1");
  expect(screen.getByRole("tabpanel")).toHaveAttribute("aria-labelledby", overview.id);
  overview.focus();
  fireEvent.keyDown(overview, { key: "ArrowRight" });
  expect(yaml).toHaveFocus();
  expect(yaml).toHaveAttribute("aria-selected", "true");
  await waitFor(() => expect(location()).toBe(`${GW_PATH}?tab=yaml`));
  expect(await screen.findByTestId("yaml")).toHaveTextContent("gatewayClassName: kgateway");
  expect(screen.getByRole("tabpanel")).toHaveAttribute("aria-labelledby", yaml.id);
  // Reload: the same URL opens on the YAML tab.
  first.unmount();
  mount(`${GW_PATH}?tab=yaml`);
  expect(await screen.findByTestId("yaml")).toBeInTheDocument();
  expect(screen.getByRole("tab", { name: "YAML" })).toHaveAttribute("aria-selected", "true");
  // Home goes back to Overview and drops the param.
  fireEvent.keyDown(screen.getByRole("tab", { name: "YAML" }), { key: "Home" });
  await waitFor(() => expect(location()).toBe(GW_PATH));
  expect(screen.getByRole("tab", { name: "Overview" })).toHaveFocus();
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
  mount(GW_PATH);
  // The name comes from the URL, so the header is there before the data.
  expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("infra/edge");
  expect(screen.getByTestId("detail-skeleton")).toBeInTheDocument();
  const header = () => getComputedStyle(screen.getByTestId("detail-header")).minHeight;
  const tabs = () => getComputedStyle(screen.getByTestId("detail-tabs")).height;
  expect(header()).toBe(`${HEADER_H}px`);
  expect(tabs()).toBe(`${TABS_H}px`);
  await act(async () => release?.());
  await screen.findByRole("table", { name: "Listeners" });
  expect(header()).toBe(`${HEADER_H}px`);
  expect(tabs()).toBe(`${TABS_H}px`);
});

it("a detail 404 says the object is out of scope", async () => {
  serve((u) => (u.pathname === "/api/v1/meta" ? metaGolden.body : undefined));
  mount(GW_PATH);
  expect(await screen.findByText("Not in this install's scope")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Back to install" })).toHaveAttribute("href", `/i/${ID}`);
});

it("the header offers copy buttons", async () => {
  serve(api());
  mount(GW_PATH);
  await screen.findByRole("table", { name: "Listeners" });
  const header = screen.getByTestId("detail-header");
  expect(within(header).getByRole("button", { name: "Copy namespace/name" })).toBeInTheDocument();
  expect(await within(header).findByRole("button", { name: /kubectl|command/i })).toBeInTheDocument();
});

describe.each([controlRoomLight, controlRoomDark])("interactive styles in $id", (theme) => {
  it("tabs, expanders, sub-filter chips and copy buttons have hover, active and focus-visible styles", async () => {
    serve(api());
    mount(GW_PATH, theme.id);
    const listeners = await screen.findByRole("table", { name: "Listeners" });
    const els = [
      screen.getByRole("tab", { name: "Overview" }),
      within(rowOf(listeners, "http")).getByRole("button", { name: /Show conditions/ }),
      within(screen.getByRole("group", { name: "Failed routes" })).getAllByRole("button")[0] as HTMLElement,
      screen.getByRole("button", { name: "Copy namespace/name" }),
    ];
    for (const el of els)
      for (const pseudo of [":hover", ":active", ":focus-visible"])
        expect(
          pseudoRules(el, pseudo).join("").length,
          `${el.textContent || el.getAttribute("aria-label")} ${pseudo}`,
        ).toBeGreaterThan(0);
  });
});

it("below 900 px the listener table moves TLS and allowed routes into the expanded row", async () => {
  vi.stubGlobal("matchMedia", (q: string) => ({
    matches: q.includes("max-width: 899px"),
    media: q,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
  serve(api());
  mount(GW_PATH);
  const listeners = await screen.findByRole("table", { name: "Listeners" });
  const headers = within(listeners)
    .getAllByRole("columnheader")
    .map((h) => h.textContent);
  expect(headers).not.toContain("TLS");
  await userEvent.click(within(rowOf(listeners, "https")).getByRole("button", { name: /Show conditions/ }));
  const details = screen.getByTestId("listener-https-details");
  expect(within(details).getByText("TLS")).toBeInTheDocument();
  expect(details).toHaveTextContent("Terminate");
});

it("sub-table skeletons have the loaded row count, so the sections below keep their place", async () => {
  let release: (() => void) | undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  const reply = api();
  const g = { ...gateway(), routeCounts: { accepted: 5, notAccepted: 0, pending: 1, unwatched: 1 } };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const u = new URL(url, "http://ui");
      if (u.pathname === `${API}/gateways/infra/edge`) return new Response(JSON.stringify(g), { status: 200 });
      // Lists wait, so their skeletons can be counted.
      await gate;
      const a = u.searchParams.get("attachment");
      const body = reply(u) as RouteList;
      if (u.pathname === `${API}/routes`) {
        // The visible count matches routeCounts: none not accepted.
        const items =
          a === "not-accepted"
            ? []
            : body.items.filter((r) =>
                r.parents.some((p) => p.attachment === a && [p.ref.name, p.via?.name].includes("edge")),
              );
        return new Response(JSON.stringify({ ...body, items, total: items.length }), { status: 200 });
      }
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  mount(GW_PATH);
  await screen.findByRole("table", { name: "Listeners" });
  const skeletons = (name: string) => within(table(name)).queryAllByTestId("skeleton-row").length;
  const loaded = (name: string) =>
    within(table(name))
      .queryAllByRole("row")
      .filter((r) => r.closest("tbody") && r.dataset.row !== undefined).length;
  // ListenerSets: the status count; attached: routeCounts.accepted; failed (Not accepted, 0): one blank row.
  expect(skeletons("ListenerSets")).toBe(1);
  expect(skeletons("Attached routes")).toBe(5);
  expect(skeletons("Routes that failed to attach")).toBe(1);
  const blank = within(table("Routes that failed to attach")).getByTestId("skeleton-row");
  expect(blank.querySelector("[data-testid=shimmer]")).toBeNull();
  expect(getComputedStyle(blank).height).toBe(`${ROW_H}px`);
  await act(async () => release?.());
  await within(table("Attached routes")).findByText("cart");
  expect(loaded("Attached routes")).toBe(5);
  expect(loaded("ListenerSets")).toBe(2);
  // The empty state takes the blank row's place at the same height.
  const empty = await within(table("Routes that failed to attach")).findByTestId("empty-row");
  expect(getComputedStyle(empty).height).toBe(`${ROW_H}px`);
});

it("the hidden-routes line shows once for the route sections, and single pages have no pager", async () => {
  const base = api();
  serve((u) => {
    const b = base(u);
    return u.pathname === `${API}/routes` ? { ...(b as RouteList), deniedInInstall: 2 } : b;
  });
  mount(GW_PATH);
  await within(await screen.findByRole("table", { name: "Attached routes" })).findByText("cart");
  expect(screen.getAllByText("You cannot view 2 routes in this install.")).toHaveLength(1);
  expect(table("Routes that failed to attach").parentElement).toHaveTextContent("You cannot view 2 routes");
  expect(screen.queryByRole("button", { name: "Next page" })).toBeNull();
});
