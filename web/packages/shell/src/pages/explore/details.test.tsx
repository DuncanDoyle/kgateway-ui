import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes, useLocation, useNavigate } from "react-router-dom";
import gatewayGolden from "../../../../../../pkg/api/testdata/golden/gateway-detail.json";
import gatewayRestrictedGolden from "../../../../../../pkg/api/testdata/golden/gateway-detail-restricted.json";
import lsGolden from "../../../../../../pkg/api/testdata/golden/listenerset-detail.json";
import lsAllowedGolden from "../../../../../../pkg/api/testdata/golden/listenerset-detail-allowed.json";
import lsInvalidGolden from "../../../../../../pkg/api/testdata/golden/listenerset-detail-invalid.json";
import listenersetsGolden from "../../../../../../pkg/api/testdata/golden/listenersets.json";
import metaGolden from "../../../../../../pkg/api/testdata/golden/meta.json";
import routesGolden from "../../../../../../pkg/api/testdata/golden/routes.json";
import routesByListenerGolden from "../../../../../../pkg/api/testdata/golden/routes-by-listener.json";
import { POLL_MS } from "../../api/hooks";
import type { GatewayDetail, ListenerSetDetail, ListenerSetList, RouteList, RouteRow } from "../../api/types";
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
  const navigate = useNavigate();
  return (
    <>
      <output data-testid="location">{loc.pathname + loc.search}</output>
      <button type="button" data-testid="back" onClick={() => navigate(-1)} />
    </>
  );
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

it("Gateway page shows the status count and the computed count side by side", async () => {
  serve(api());
  mount(GW_PATH);
  const listeners = await screen.findByRole("table", { name: "Listeners" });
  const headers = within(listeners)
    .getAllByRole("columnheader")
    .map((h) => h.textContent);
  expect(headers).toEqual(expect.arrayContaining(["Attached routes", "RoutesComputed"]));
  expect(within(rowOf(listeners, "https")).getAllByText("0")).toHaveLength(2);
  // A listener the controller has not written status for says so, with no number.
  const fresh = rowOf(listeners, "new");
  expect(within(fresh).getByText("No status")).toBeInTheDocument();
  expect(within(fresh).getByText("Not reported")).toBeInTheDocument();
  expect(within(rowOf(listeners, "http")).getByText("Any hostname")).toBeInTheDocument();
  expect(within(rowOf(listeners, "http")).getByText(/^Namespaces with team=a · /)).toBeInTheDocument();
  // Certificates are names as text: Secrets never link.
  const https = rowOf(listeners, "https");
  expect(within(https).getByText(/^Terminate · /)).toBeInTheDocument();
  expect(within(https).queryByRole("link")).toBeNull();
  expect(https).toHaveTextContent("infra/edge-cert");
  // Gateway conditions: icon and text, the reason as written.
  const accepted = screen.getByTestId("condition-Accepted");
  expect(accepted).toHaveTextContent("Accepted: True");
  expect(within(accepted).getByTestId("health-icon-healthy")).toBeInTheDocument();
  expect(screen.getByText("Attached ListenerSets (status): 1")).toBeInTheDocument();
  expect(screen.getByText("kgateway")).toBeInTheDocument();
});

/** Every health icon in the document: on a stale page all of them read unknown (design §6.3). */
const iconStates = () =>
  Array.from(document.querySelectorAll('[data-testid^="health-icon-"]')).map((e) =>
    e.getAttribute("data-testid")?.replace("health-icon-", ""),
  );
const old = () => new Date(Date.now() - 10 * 60_000).toISOString();

it("stale Gateway data turns every state on the page unknown", async () => {
  // Only the detail is stale; the sub-lists are fresh and still follow the page.
  serve(api({ gateway: { ...gateway(), collectedAt: old() } }));
  mount(GW_PATH);
  const attached = await screen.findByRole("table", { name: "Attached routes" });
  await waitFor(() => expect(attached).toHaveAttribute("aria-busy", "false"));
  await screen.findByText(/Data is stale/);
  expect(within(screen.getByTestId("detail-header")).getByText("Unknown")).toBeInTheDocument();
  // The listener's state and its computed check both read Unknown.
  expect(within(rowOf(table("Listeners"), "http")).getAllByText("Unknown")).toHaveLength(2);
  expect(iconStates().length).toBeGreaterThan(3);
  expect(new Set(iconStates())).toEqual(new Set(["unknown"]));
});

it("stale ListenerSet data turns every state on the page unknown", async () => {
  serve(api({ ls: { ...ls(lsInvalidGolden), collectedAt: old() } }));
  mount(LS_PATH("partial"));
  await screen.findByText(/Data is stale/);
  await screen.findByRole("table", { name: "Listeners" });
  expect(within(screen.getByTestId("detail-header")).getByText("Unknown")).toBeInTheDocument();
  expect(iconStates().length).toBeGreaterThan(1);
  expect(new Set(iconStates())).toEqual(new Set(["unknown"]));
  // Like a list row, a stale listener row shows only its state; the conditions stay on expand.
  const extra = rowOf(table("Listeners"), "extra");
  expect(within(extra).getAllByText("Unknown").length).toBeGreaterThan(0);
  expect(within(extra).queryByText(/Programmed: False/)).toBeNull();
});

it("a failed poll turns states unknown, keeps the notice's error icon, and fresh data restores them", async () => {
  let polls = 0;
  const base = api();
  serve((u) => {
    if (u.pathname !== `${API}/gateways/infra/edge`) return base(u);
    return polls++ === 1 ? [500, { error: "boom" }] : gateway();
  });
  vi.useFakeTimers({ shouldAdvanceTime: true });
  mount(GW_PATH);
  const header = await screen.findByTestId("detail-header");
  await within(header).findByText("Warning");
  const accepted = () => within(screen.getByTestId("condition-Accepted"));
  expect(accepted().getByTestId("health-icon-healthy")).toBeInTheDocument();

  await act(async () => {
    await vi.advanceTimersByTimeAsync(5100);
  });
  expect(polls).toBe(2);
  const notice = await screen.findByText(/Cannot load Gateway/);
  const box = notice.closest('[role="alert"], [role="status"]') ?? (notice.parentElement as HTMLElement);
  expect(within(box as HTMLElement).getByTestId("health-icon-error")).toBeInTheDocument();
  expect(within(header).getByText("Unknown")).toBeInTheDocument();
  expect(accepted().getByTestId("health-icon-unknown")).toBeInTheDocument();

  // The test SWR config retries an error once per poll interval, so one interval brings the good response.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5100);
  });
  expect(polls).toBe(3);
  await waitFor(() => expect(screen.queryByText(/Cannot load Gateway/)).toBeNull());
  expect(within(header).getByText("Warning")).toBeInTheDocument();
  expect(accepted().getByTestId("health-icon-healthy")).toBeInTheDocument();
});

it("a sub-table keeps its rows and shows the notice when a poll fails, until a good poll", async () => {
  let polls = 0;
  const base = api();
  serve((u) => {
    if (u.pathname !== `${API}/listenersets`) return base(u);
    return polls++ === 1 ? [500, { error: "boom" }] : lsList();
  });
  vi.useFakeTimers({ shouldAdvanceTime: true });
  mount(GW_PATH);
  const t = await screen.findByRole("table", { name: "ListenerSets" });
  await within(t).findByText("partial");
  expect(screen.queryByText(/Cannot load ListenerSets/)).toBeNull();

  await act(async () => {
    await vi.advanceTimersByTimeAsync(POLL_MS + 100);
  });
  expect(polls).toBe(2);
  expect(await screen.findByText(/Cannot load ListenerSets/)).toBeInTheDocument();
  expect(within(t).getByText("partial")).toBeInTheDocument();

  await act(async () => {
    await vi.advanceTimersByTimeAsync(POLL_MS + 100);
  });
  expect(polls).toBe(3);
  await waitFor(() => expect(screen.queryByText(/Cannot load ListenerSets/)).toBeNull());
  expect(within(t).getByText("partial")).toBeInTheDocument();
});

it("a sub-table name is one truncated line with the full name in a keyboard tooltip", async () => {
  const long = "a-listenerset-with-a-deliberately-long-name-that-must-not-wrap-or-shift-the-table";
  const base = api();
  serve((u) => {
    if (u.pathname !== `${API}/listenersets`) return base(u);
    const l = lsList();
    return { ...l, items: l.items.map((r, i) => (i ? r : { ...r, ref: { ...r.ref, name: long } })) };
  });
  mount(GW_PATH);
  const t = await screen.findByRole("table", { name: "ListenerSets" });
  const name = await within(t).findByLabelText(new RegExp(`/${long}$`));
  const style = getComputedStyle(name);
  expect([style.whiteSpace, style.overflow, style.textOverflow]).toEqual(["nowrap", "hidden", "ellipsis"]);
  const link = name.closest("a") as HTMLElement;
  for (let i = 0; i < 80 && document.activeElement !== link; i++) await userEvent.tab();
  expect(link).toHaveFocus();
  expect(screen.getByRole("tooltip")).toHaveTextContent(name.getAttribute("aria-label") ?? "");
});

it("on a stale page the YAML tab's error keeps its error icon", async () => {
  const base = api({ gateway: { ...gateway(), collectedAt: old() } });
  serve((u) => (u.pathname.startsWith(`${API}/resources/`) ? [500, { error: "boom" }] : base(u)));
  mount(`${GW_PATH}?tab=yaml`);
  await screen.findByText(/Data is stale/);
  const title = await screen.findByText("Cannot load resource");
  expect(title).toBeInTheDocument();
  expect(screen.getByTestId("health-icon-error")).toBeInTheDocument();
});

it("a stale condition says which generation it describes", async () => {
  const g = gateway();
  g.generation = 2;
  g.conditions = [{ ...(g.conditions[0] as GatewayDetail["conditions"][number]), stale: true }];
  serve(api({ gateway: g }));
  mount(GW_PATH);
  expect(await screen.findByText("Describes generation 1; the object is at 2.")).toBeInTheDocument();
});

it("failed-route table has Not accepted, Waiting for status and Namespace not watched sub-filters", async () => {
  serve(api());
  mount(GW_PATH);
  await screen.findByRole("table", { name: "Routes not attached" });
  const parent = `${GW}/Gateway/infra/edge`;
  const attachments = () =>
    listCalls("routes")
      .filter((u) => u.searchParams.get("parent") === parent)
      .map((u) => u.searchParams.getAll("attachment"));
  // Attached routes and the first sub-filter, each with exactly one attachment value (single-valued, §7.2).
  await waitFor(() => expect(attachments()).toEqual(expect.arrayContaining([["accepted"], ["not-accepted"]])));
  const group = screen.getByRole("group", { name: "Not attached" });
  const chips = within(group).getAllByRole("button");
  expect(chips.map((c) => c.textContent)).toEqual([
    "Not accepted (1)",
    "Waiting for status (1)",
    "Namespace not watched (1)",
  ]);
  expect(chips[0]).toHaveAttribute("aria-pressed", "true");
  // The deciding condition's reason as written, for this Gateway's parent entry.
  const failed = table("Routes not attached");
  expect(within(rowOf(failed, "rejected")).getByText("UnsupportedValue")).toBeInTheDocument();
  // The Why cell is one line; its full text is in the Tooltip, on keyboard focus.
  const why = within(rowOf(failed, "rejected")).getByText("UnsupportedValue").closest("[tabindex]") as HTMLElement;
  expect([getComputedStyle(why).whiteSpace, getComputedStyle(why).textOverflow]).toEqual(["nowrap", "ellipsis"]);
  for (let i = 0; i < 160 && document.activeElement !== why; i++) await userEvent.tab();
  expect(why).toHaveFocus();
  expect(screen.getByRole("tooltip")).toHaveTextContent("UnsupportedValue unsupported filter type");
  why.blur();
  // A pending reason is ours, not the controller's: it is never shown as a code.
  expect(within(failed).queryByText("no status from the controller")).toBeNull();
  await userEvent.click(within(group).getByRole("button", { name: /Waiting for status/ }));
  await waitFor(() => expect(attachments().at(-1)).toEqual(["pending"]));
  expect(within(group).getByRole("button", { name: /Waiting for status/ })).toHaveAttribute("aria-pressed", "true");
  await userEvent.click(within(group).getByRole("button", { name: /Namespace not watched/ }));
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
  expect(within(row).getByText(/Programmed: False/)).toBeInTheDocument();
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
      within(screen.getByRole("group", { name: "Not attached" })).getAllByRole("button")[0] as HTMLElement,
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

it("below 900 px listener names keep their words: hostname moves out and long names truncate", async () => {
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
  expect(headers).not.toContain("Hostname");
  const name = within(rowOf(listeners, "https")).getByText("https");
  expect(getComputedStyle(name).whiteSpace).toBe("nowrap");
  expect(getComputedStyle(name).textOverflow).toBe("ellipsis");
  // The full name is reachable by keyboard: focus shows the app tooltip.
  expect(name).toHaveAttribute("tabindex", "0");
  fireEvent.keyDown(document, { key: "Tab" });
  act(() => name.focus());
  expect(await screen.findByRole("tooltip")).toHaveTextContent("https");
  await userEvent.click(within(rowOf(listeners, "http")).getByRole("button", { name: /Show conditions/ }));
  const details = screen.getByTestId("listener-http-details");
  expect(within(details).getByText("Hostname")).toBeInTheDocument();
  expect(details).toHaveTextContent("Any hostname");
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
  // ListenerSets: the detail's visible count; attached: routeCounts.accepted; failed (Not accepted, 0): one blank row.
  expect(skeletons("ListenerSets")).toBe(gateway().listenerSets);
  expect(skeletons("ListenerSets")).toBe(2);
  expect(skeletons("Attached routes")).toBe(5);
  expect(skeletons("Routes not attached")).toBe(1);
  const blank = within(table("Routes not attached")).getByTestId("skeleton-row");
  expect(blank.querySelector("[data-testid=shimmer]")).toBeNull();
  expect(getComputedStyle(blank).height).toBe(`${ROW_H}px`);
  await act(async () => release?.());
  await within(table("Attached routes")).findByText("cart");
  expect(loaded("Attached routes")).toBe(5);
  expect(loaded("ListenerSets")).toBe(2);
  // The empty state takes the blank row's place at the same height.
  const empty = await within(table("Routes not attached")).findByTestId("empty-row");
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
  expect(table("Routes not attached").parentElement).toHaveTextContent("You cannot view 2 routes");
  expect(screen.queryByRole("button", { name: "Next page" })).toBeNull();
});

// region View A (2.3)
const EDGE = `${GW}/Gateway/infra/edge`;
const edgeRef = { group: GW, kind: "Gateway", namespace: "infra", name: "edge", link: "detail" as const };
const byListener = (): RouteList =>
  structuredClone({ ...(routesByListenerGolden.body as RouteList), collectedAt: now() });
/** byListener's rows with effective hostnames on listener name of edge. */
function members(name: string, hosts: Record<string, string[]>): RouteList {
  const b = byListener();
  for (const r of b.items)
    for (const p of r.parents)
      for (const l of p.computed?.listeners ?? []) {
        l.listener = name;
        if (hosts[r.ref.name]) l.effectiveHostnames = hosts[r.ref.name];
      }
  return b;
}
const unplaced = (): RouteList => {
  const r = structuredClone(byListener().items[0] as RouteRow);
  r.ref = { ...r.ref, name: "stream", kind: "TCPRoute" };
  r.flags = ["accepted", "listener-unknown"];
  r.parents = [
    {
      ref: edgeRef,
      attachment: "accepted",
      computed: {
        listeners: [],
        computed: {
          by: "ui",
          rules: "kgateway-2.4",
          rule: "allowed-routes",
          state: "unknown",
          reason: "setting-unknown",
        },
      },
    },
  ];
  return { install: ID, items: [r], total: 1, collectedAt: now() };
};
/** The default API, with routes?listener= answered from the view A goldens. */
function viewA(over: { gateway?: GatewayDetail } = {}) {
  const base = api(over);
  return (u: URL): Reply | undefined => {
    const l = u.searchParams.get("listener");
    if (u.pathname === `${API}/routes` && l === "_unknown") return unplaced();
    if (u.pathname === `${API}/routes` && l)
      return members(l, { api: ["api.example.com"], cart: ["cart.example.com"] });
    return base(u);
  };
}
const listenerGroup = () => screen.getByRole("group", { name: "Routes by listener" });
const pick = (name: string | RegExp) => userEvent.click(within(listenerGroup()).getByRole("button", { name }));

it("listener table shows the status count and the computed count with its check", async () => {
  serve(api());
  mount(GW_PATH);
  const listeners = await screen.findByRole("table", { name: "Listeners" });
  const http = rowOf(listeners, "http");
  // Status count and computed count: both 6, the computed one with its badge and check.
  expect(within(http).getAllByText("6")).toHaveLength(2);
  const badge = within(http).getByTestId("computed-badge");
  expect(within(badge).getByText("Agrees")).toBeInTheDocument();
  expect(within(badge).getByTestId("health-icon-healthy")).toBeInTheDocument();
  // The column header carries the Computed chip, so cells show the number and a short state.
  const header = within(listeners).getByRole("columnheader", { name: /^Routes\s*Computed$/ });
  expect(within(header).getByText("Computed")).toBeInTheDocument();
  expect(within(http).queryByText("Computed")).toBeNull();
  // Pending: the number with its waiting state.
  expect(within(rowOf(listeners, "https")).getByText("Waiting")).toBeInTheDocument();
  // The full sentence is in the expanded row.
  await userEvent.click(within(http).getByRole("button", { name: /Show conditions/ }));
  expect(screen.getByTestId("listener-http-details")).toHaveTextContent("Count agrees with the controller");
});

it("a disagreeing listener shows both numbers and the warning icon with text", async () => {
  serve(api());
  mount(GW_PATH);
  const drift = rowOf(await screen.findByRole("table", { name: "Listeners" }), "drift");
  expect(within(drift).getByText("7")).toBeInTheDocument();
  expect(within(drift).getByText("6")).toBeInTheDocument();
  const badge = within(drift).getByTestId("computed-badge");
  expect(within(badge).getByTestId("health-icon-warning")).toBeInTheDocument();
  expect(within(badge).getByText("Disagrees")).toBeInTheDocument();
});

it("unknown listeners show no number; a hidden Namespace hides no count", async () => {
  serve(api({ gateway: { ...(gatewayRestrictedGolden.body as GatewayDetail), collectedAt: now() } }));
  mount(GW_PATH);
  const listeners = await screen.findByRole("table", { name: "Listeners" });
  const cell = (name: string) => within(rowOf(listeners, name)).getByTestId("computed-cell");
  // tcp: unknown with its reason, no number. http, a Selector over a hidden Namespace, keeps its count.
  expect(cell("tcp")).toHaveTextContent("Unknown");
  expect(cell("tcp")).not.toHaveTextContent(/\d/);
  expect(cell("http")).toHaveTextContent("6");
  expect(cell("http")).not.toHaveTextContent("Hidden");
  // The expanded row never reads an unknown count as "None".
  await userEvent.click(within(rowOf(listeners, "tcp")).getByRole("button", { name: /Show conditions/ }));
  const details = screen.getByTestId("listener-tcp-details");
  expect(details).toHaveTextContent("Computed routes: No count");
  expect(details).not.toHaveTextContent("None");
});

it("controller values carry no badge", async () => {
  serve(api());
  mount(GW_PATH);
  const listeners = await screen.findByRole("table", { name: "Listeners" });
  for (const name of ["http", "https", "new", "drift", "tcp"]) {
    const row = rowOf(listeners, name);
    // One badge per row, in the computed cell; status, name, port and the attached count have none.
    expect(within(row).getAllByTestId("computed-badge")).toHaveLength(1);
    expect(within(within(row).getByTestId("computed-cell")).getByTestId("computed-badge")).toBeInTheDocument();
  }
  expect(within(screen.getByTestId("detail-header")).queryByTestId("computed-badge")).toBeNull();
  expect(screen.getByRole("region", { name: "Conditions" }).querySelector("[data-testid=computed-badge]")).toBeNull();
  // The failed-route table's reason and message are the controller's: no badge without an explanation.
  const failed = await screen.findByRole("table", { name: "Routes not attached" });
  await within(failed).findByText("rejected");
  expect(within(rowOf(failed, "rejected")).queryByTestId("computed-badge")).toBeNull();
});

it("a shared plain-HTTP port shows the isolation note", async () => {
  const g = gateway();
  g.listeners = g.listeners.map((l) => (l.name === "http" ? { ...l, isolationNote: true } : l));
  serve(api({ gateway: g }));
  mount(GW_PATH);
  const listeners = await screen.findByRole("table", { name: "Listeners" });
  const note = within(rowOf(listeners, "http")).getByLabelText("Envoy may serve fewer hostnames on this listener");
  expect(note).toHaveAttribute("tabindex", "0");
  expect(within(rowOf(listeners, "drift")).queryByLabelText(/fewer hostnames/)).toBeNull();
  await userEvent.click(within(rowOf(listeners, "http")).getByRole("button", { name: /Show conditions/ }));
  expect(screen.getByTestId("listener-http-details")).toHaveTextContent(
    "Envoy may serve fewer hostnames on this listener",
  );
});

it("selecting a listener sets ?listener= and filters attached routes to its members", async () => {
  serve(viewA());
  mount(GW_PATH);
  await screen.findByRole("table", { name: "Attached routes" });
  await pick("drift");
  await waitFor(() => expect(location()).toBe(`${GW_PATH}?listener=drift`));
  const t = await screen.findByRole("table", { name: "Computed routes on listener drift" });
  await within(t).findByText("api");
  // The request names the parent and the listener, and nothing else (strict params, §7.2).
  const q = listCalls("routes").at(-1) as URL;
  expect([...q.searchParams.entries()]).toEqual([
    ["parent", EDGE],
    ["listener", "drift"],
  ]);
  // Effective hostnames per row, each computed.
  expect(within(rowOf(t, "api")).getByText("api.example.com")).toBeInTheDocument();
  expect(within(rowOf(t, "cart")).getByText("cart.example.com")).toBeInTheDocument();
  expect(within(rowOf(t, "api")).getByTestId("computed-badge")).toBeInTheDocument();
  expect(within(listenerGroup()).getByRole("button", { name: "drift" })).toHaveAttribute("aria-pressed", "true");
  // Back clears it.
  await userEvent.click(screen.getByTestId("back"));
  await waitFor(() => expect(location()).toBe(GW_PATH));
  expect(await screen.findByRole("table", { name: "Attached routes" })).toBeInTheDocument();
  expect(within(listenerGroup()).getByRole("button", { name: "All listeners" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
});

it("?listener= survives reload", async () => {
  serve(viewA());
  mount(`${GW_PATH}?listener=drift`);
  const t = await screen.findByRole("table", { name: "Computed routes on listener drift" });
  await within(t).findByText("api");
  // The unfiltered attached-routes query is never sent for a selected listener.
  for (const u of listCalls("routes").filter((x) => x.searchParams.get("attachment") === "accepted"))
    expect(u.searchParams.get("listener")).not.toBeNull();
  expect(listCalls("routes").some((u) => u.searchParams.get("listener") === "drift")).toBe(true);
});

it("listener=_unknown lists the routes the UI could not place", async () => {
  serve(viewA());
  mount(GW_PATH);
  await screen.findByRole("table", { name: "Attached routes" });
  await pick("Unknown listener");
  await waitFor(() => expect(location()).toBe(`${GW_PATH}?listener=_unknown`));
  const t = await screen.findByRole("table", { name: "Routes with an unknown listener" });
  await within(t).findByText("stream");
  expect(within(rowOf(t, "stream")).getByText("Unknown: the UI cannot read a controller setting")).toBeInTheDocument();
  expect(listCalls("routes").at(-1)?.searchParams.get("listener")).toBe("_unknown");
});

it("an unknown-count listener shows the unknown state and its reason, never an empty table", async () => {
  serve(viewA());
  mount(`${GW_PATH}?listener=tcp`);
  const box = await screen.findByTestId("listener-routes-unknown");
  expect(box).toHaveTextContent("Unknown: the UI cannot read a controller setting");
  expect(screen.queryByRole("table", { name: "Computed routes on listener tcp" })).toBeNull();
  expect(within(box).getByRole("link", { name: /All routes of this Gateway/ })).toHaveAttribute(
    "href",
    `/i/${ID}/routes?parent=${encodeURIComponent(EDGE)}`,
  );
  // No member query for a listener whose count is unknown.
  expect(listCalls("routes").some((u) => u.searchParams.get("listener") === "tcp")).toBe(false);
});

it("an older server without the listener filter gets no selector and no listener param", async () => {
  const base = viewA();
  serve((u) => (u.pathname === "/api/v1/meta" ? { ...metaGolden.body, filters: undefined } : base(u)));
  mount(`${GW_PATH}?listener=drift`);
  const t = await screen.findByRole("table", { name: "Attached routes" });
  await within(t).findByText("cart");
  expect(screen.queryByRole("group", { name: "Routes by listener" })).toBeNull();
  for (const u of listCalls("routes")) expect(u.searchParams.has("listener")).toBe(false);
});

it("failed-route rows show the computed explanation", async () => {
  const explanation = "listener https has *.example.org, route has shop.example.com";
  const base = api();
  serve((u) => {
    const b = base(u);
    if (u.pathname !== `${API}/routes`) return b;
    const list = structuredClone(b as RouteList);
    for (const r of list.items)
      if (r.ref.name === "rejected")
        for (const p of r.parents)
          if (p.computed) p.computed = { ...p.computed, outcome: "NoMatchingListenerHostname", explanation };
    return list;
  });
  mount(GW_PATH);
  const failed = await screen.findByRole("table", { name: "Routes not attached" });
  await within(failed).findByText("rejected");
  const row = rowOf(failed, "rejected");
  expect(within(row).getByText(explanation)).toBeInTheDocument();
  expect(within(row).getByTestId("computed-badge")).toBeInTheDocument();
});

it("ListenerSet page shows computed counts per listener", async () => {
  const l = ls(lsInvalidGolden);
  l.listeners = l.listeners.map((x, i) => ({
    ...x,
    computedRoutes: i + 2,
    computed: { by: "ui", rules: "kgateway-2.4", rule: "listener-count", state: "checked", coverage: ["count-agrees"] },
  }));
  serve(api({ ls: l }));
  mount(LS_PATH("partial"));
  const listeners = await screen.findByRole("table", { name: "Listeners" });
  const first = l.listeners[0]?.name ?? "";
  const cell = within(rowOf(listeners, first)).getByTestId("computed-cell");
  expect(cell).toHaveTextContent("2");
  expect(within(cell).getByText("Agrees")).toBeInTheDocument();
  // The listener selector is the Gateway page's (design §8.1).
  expect(screen.queryByRole("group", { name: "Routes by listener" })).toBeNull();
});

describe.each([controlRoomLight, controlRoomDark])("view A interactive styles in $id", (theme) => {
  it("listener chips and the isolation note have hover, active and focus-visible styles", async () => {
    const g = gateway();
    g.listeners = g.listeners.map((l) => (l.name === "http" ? { ...l, isolationNote: true } : l));
    serve(viewA({ gateway: g }));
    mount(`${GW_PATH}?listener=tcp`, theme.id);
    const box = await screen.findByTestId("listener-routes-unknown");
    const state = within(screen.getByTestId("listener-http")).getByText("Agrees").closest("[tabindex]") as HTMLElement;
    const els = [
      within(listenerGroup()).getByRole("button", { name: "http" }),
      within(box).getByRole("link", { name: /All routes/ }),
      // A short computed state without its own chip is the tooltip's trigger.
      state,
    ];
    for (const el of els)
      for (const pseudo of [":hover", ":active", ":focus-visible"])
        expect(pseudoRules(el, pseudo).join("").length, `${el.textContent} ${pseudo}`).toBeGreaterThan(0);
    const note = screen.getByLabelText("Envoy may serve fewer hostnames on this listener");
    expect(pseudoRules(note, ":focus-visible").join("").length).toBeGreaterThan(0);
  });
});

// region View A fix round 1
it("the listener chip bar keeps its place while /meta loads, so the route sections do not move", async () => {
  let release: (() => void) | undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  const base = viewA();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const u = new URL(url, "http://ui");
      if (u.pathname === "/api/v1/meta") await gate;
      return new Response(JSON.stringify(base(u)), { status: 200 });
    }),
  );
  mount(GW_PATH);
  await screen.findByRole("table", { name: "Attached routes" });
  // Before /meta: the bar's slot is reserved at its final height, ahead of the attached table.
  const slot = screen.getByTestId("listener-bar");
  expect(getComputedStyle(slot).minHeight).toBe("32px");
  expect(within(slot).queryAllByRole("button")).toHaveLength(0);
  await act(async () => release?.());
  const bar = await screen.findByRole("group", { name: "Routes by listener" });
  expect(bar).toHaveAttribute("data-testid", "listener-bar");
  expect(getComputedStyle(bar).minHeight).toBe("32px");
  expect(bar.compareDocumentPosition(table("Attached routes")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});

/** A row on edge's listener http directly and on ListenerSet ls1's listener also named http. */
function sharedName(): RouteList {
  const b = byListener();
  const r = structuredClone(b.items[0] as RouteRow);
  const ls1 = { group: GW, kind: "ListenerSet", namespace: "infra", name: "ls1", link: "detail" as const };
  const checked = {
    by: "ui" as const,
    rules: "kgateway-2.4",
    rule: "hostname-intersection",
    state: "checked" as const,
  };
  r.parents = [
    {
      ref: ls1,
      via: edgeRef,
      attachment: "accepted",
      computed: {
        outcome: "accepted",
        computed: checked,
        listeners: [
          {
            resource: ls1,
            listener: "http",
            effectiveHostnames: ["ls.example.com"],
            computed: { ...checked, state: "disagrees" },
          },
        ],
      },
    },
    {
      ref: edgeRef,
      attachment: "accepted",
      computed: {
        outcome: "accepted",
        computed: checked,
        listeners: [
          {
            resource: edgeRef,
            listener: "http",
            effectiveHostnames: ["gw.example.com"],
            computed: { ...checked, coverage: ["count-agrees"] },
          },
        ],
      },
    },
  ];
  return { ...b, items: [r], total: 1 };
}

it("effective hostnames come from the parent's own listener, not a ListenerSet listener of the same name", async () => {
  const base = viewA();
  serve((u) => (u.pathname === `${API}/routes` && u.searchParams.get("listener") === "http" ? sharedName() : base(u)));
  mount(`${GW_PATH}?listener=http`);
  const t = await screen.findByRole("table", { name: "Computed routes on listener http" });
  const r = rowOf(
    t,
    await within(t)
      .findByText("api")
      .then((e) => e.textContent ?? ""),
  );
  expect(r).toHaveTextContent("gw.example.com");
  expect(r).not.toHaveTextContent("ls.example.com");
  expect(within(r).getByText("Agrees")).toBeInTheDocument();
  expect(within(r).queryByText("Disagrees")).toBeNull();
});

it("Why not placed takes the parent's unknown entry, not the first entry with a result", async () => {
  const base = viewA();
  serve((u) => {
    if (u.pathname !== `${API}/routes` || u.searchParams.get("listener") !== "_unknown") return base(u);
    const b = unplaced();
    const r = b.items[0] as RouteRow;
    const ls1 = { group: GW, kind: "ListenerSet", namespace: "infra", name: "ls1", link: "detail" as const };
    r.parents.unshift({
      ref: ls1,
      via: edgeRef,
      attachment: "accepted",
      computed: {
        outcome: "accepted",
        listeners: [],
        computed: { by: "ui", rules: "kgateway-2.4", rule: "allowed-routes", state: "checked" },
      },
    });
    return b;
  });
  mount(`${GW_PATH}?listener=_unknown`);
  const t = await screen.findByRole("table", { name: "Routes with an unknown listener" });
  await within(t).findByText("stream");
  expect(rowOf(t, "stream")).toHaveTextContent("Unknown: the UI cannot read a controller setting");
});

it("a ?listener= name the Gateway does not have says so and sends no request", async () => {
  serve(viewA());
  mount(`${GW_PATH}?listener=nope`);
  const box = await screen.findByTestId("listener-routes-unknown");
  expect(box).toHaveTextContent("This Gateway has no listener named nope.");
  expect(listCalls("routes").some((u) => u.searchParams.get("listener") === "nope")).toBe(false);
});
