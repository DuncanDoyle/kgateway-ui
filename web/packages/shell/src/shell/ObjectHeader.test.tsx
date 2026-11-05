import { act, render, screen, waitFor } from "@testing-library/react";
import { SWRConfig } from "swr";
import gatewayGolden from "../../../../../pkg/api/testdata/golden/gateway-detail.json";
import installsGolden from "../../../../../pkg/api/testdata/golden/installs.json";
import lsGolden from "../../../../../pkg/api/testdata/golden/listenerset-detail.json";
import listenersetsGolden from "../../../../../pkg/api/testdata/golden/listenersets.json";
import metaGolden from "../../../../../pkg/api/testdata/golden/meta.json";
import resourceGolden from "../../../../../pkg/api/testdata/golden/resource.json";
import routeGolden from "../../../../../pkg/api/testdata/golden/route-detail.json";
import routesGolden from "../../../../../pkg/api/testdata/golden/routes.json";
import { defaultPages } from "../pages/defaultPages";
import { testSWR } from "../test/render";
import { controlRoomDark } from "../theme/themes";
import { Shell } from "./Shell";

const ID = "kgw.kgateway";
const GW = "gateway.networking.k8s.io";
const API = `/api/v1/installs/${ID}`;
const PAGES = {
  gateway: { path: `/i/${ID}/gateways/infra/edge`, title: "Gateway infra/edge: kgw/kgateway", name: "edge" },
  route: { path: `/i/${ID}/routes/${GW}/HTTPRoute/shop/mid`, title: "HTTPRoute shop/mid: kgw/kgateway", name: "mid" },
  listenerSet: {
    path: `/i/${ID}/listenersets/${GW}/ListenerSet/infra/na`,
    title: "ListenerSet infra/na: kgw/kgateway",
    name: "na",
  },
  resource: { path: `/i/${ID}/r/${GW}/HTTPRoute/shop/cart`, title: "HTTPRoute shop/cart: kgw/kgateway", name: "cart" },
};
const fresh = <T,>(g: { body: T }) => ({ ...g.body, collectedAt: new Date().toISOString() });

/** The goldens by path; `hold` delays the detail bodies until released. */
function serve(hold?: Promise<void>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const u = new URL(url, "http://ui");
      const p = u.pathname;
      let body: unknown;
      if (p === "/api/v1/meta") body = metaGolden.body;
      else if (p === "/api/v1/installs") body = installsGolden.body;
      else if (p === `${API}/routes`) body = fresh(routesGolden);
      else if (p === `${API}/listenersets`) body = fresh(listenersetsGolden);
      else if (p.startsWith(`${API}/resources/`)) body = resourceGolden.body;
      else {
        if (hold) await hold;
        if (p === `${API}/gateways/infra/edge`) body = fresh(gatewayGolden);
        else if (p === `${API}/routes/${GW}/HTTPRoute/shop/mid`) body = fresh(routeGolden);
        else if (p === `${API}/listenersets/${GW}/ListenerSet/infra/na`) body = fresh(lsGolden);
      }
      if (body === undefined) return new Response(JSON.stringify({ error: "not found" }), { status: 404 });
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
}

function mount(path: string) {
  window.history.pushState({}, "", path);
  return render(
    <SWRConfig value={testSWR()}>
      <Shell pages={defaultPages} />
    </SWRConfig>,
  );
}
const headers = () => document.querySelectorAll("[data-object-header]");
const px = (el: Element) => Number.parseFloat(getComputedStyle(el).fontSize) || 0;

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("the header has the same height loading and loaded", async () => {
  let release: (() => void) | undefined;
  serve(
    new Promise<void>((r) => {
      release = r;
    }),
  );
  mount(PAGES.gateway.path);
  // The name comes from the URL, so the header is there before the data, at its final height.
  await waitFor(() => expect(headers()).toHaveLength(1));
  expect(screen.getByTestId("detail-skeleton")).toBeInTheDocument();
  const header = headers()[0] as HTMLElement;
  const loading = getComputedStyle(header).height;
  expect(loading).toMatch(/^\d+px$/);
  await act(async () => release?.());
  await screen.findByRole("table", { name: "Listeners" });
  expect(header.querySelector('[data-testid^="health-icon-"]')).not.toBeNull();
  expect(header.querySelector('[data-testid="data-age"]')).not.toBeNull();
  expect(headers()[0]).toBe(header);
  expect(getComputedStyle(header).height).toBe(loading);
});

it("the name is the largest type on the page", async () => {
  serve();
  mount(PAGES.gateway.path);
  await screen.findByRole("table", { name: "Listeners" });
  const name = screen.getByRole("heading", { level: 1 });
  expect(name).toHaveTextContent("edge");
  const size = px(name);
  // The top of the type scale, and every other element on the page sets a smaller size, or none.
  expect(size).toBe(Math.max(...Object.values(controlRoomDark.font.size).map((v) => Number.parseFloat(v))));
  const larger = [...document.body.querySelectorAll("*")].filter(
    (el) => el !== name && !name.contains(el) && px(el) >= size,
  );
  expect(larger.map((el) => `${el.tagName} ${el.textContent?.slice(0, 40)}`)).toEqual([]);
});

it.each(Object.entries(PAGES))("every core detail page renders one ObjectHeader: %s", async (_, page) => {
  serve();
  mount(page.path);
  await waitFor(() => expect(headers()).toHaveLength(1));
  const header = headers()[0] as HTMLElement;
  expect(header.contains(screen.getByRole("heading", { level: 1 }))).toBe(true);
  expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(page.name);
  // The page's own copy action sits in the header's actions.
  expect(await screen.findByRole("button", { name: "Copy namespace/name" })).toBeInTheDocument();
  expect(header.contains(screen.getByRole("button", { name: "Copy namespace/name" }))).toBe(true);
});

it.each(Object.entries(PAGES))("DocumentTitle still names the object: %s", async (_, page) => {
  serve();
  mount(page.path);
  await waitFor(() => expect(headers()).toHaveLength(1));
  await waitFor(() => expect(document.title).toBe(page.title));
});
