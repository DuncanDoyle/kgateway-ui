import { act, screen, waitFor } from "@testing-library/react";
import { renderWithTheme, stubApi } from "../test/render";
import {
  useGatewayDetail,
  useGateways,
  useListenerSetDetail,
  useListenerSets,
  useRouteDetail,
  useRoutes,
} from "./exploreHooks";
import { POLL_MS } from "./hooks";
import type { RouteList, RouteRow } from "./types";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const gw = "gateway.networking.k8s.io";
const row = (name: string): RouteRow => ({
  ref: { group: gw, kind: "HTTPRoute", namespace: "shop", name, link: "detail" },
  hostnames: [],
  parents: [],
  parentRoutes: [],
  children: 0,
  flags: [],
  health: "healthy",
});
const list = (install: string, ...names: string[]): RouteList => ({
  install,
  items: names.map(row),
  total: names.length,
  collectedAt: "2026-10-17T12:00:00Z",
});

function RoutesProbe({ id, query = "" }: { id: string; query?: string }) {
  const { data, error, cursorRejected } = useRoutes(id, new URLSearchParams(query));
  const rows = data?.items.map((r) => r.ref.name).join(",");
  return (
    <p>
      {error && !cursorRejected ? `error ${error.status}` : data ? `rows ${rows}` : "loading"}
      {cursorRejected ? " cursor rejected" : ""}
    </p>
  );
}

const fetched = () => vi.mocked(fetch).mock.calls.map((c) => String(c[0]));

it("loads a list with its query", async () => {
  stubApi({ "/api/v1/installs/a.one/routes?namespace=shop&sort=name.desc": list("a.one", "cart", "api") });
  renderWithTheme(<RoutesProbe id="a.one" query="namespace=shop&sort=name.desc" />);
  expect(await screen.findByText("rows cart,api")).toBeInTheDocument();
});

it("never shows the previous install's rows on a switch", async () => {
  let release: (() => void) | undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/b.two")) await gate;
      const body = url.includes("/a.one") ? list("a.one", "old") : list("b.two", "new");
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  const { rerender } = renderWithTheme(<RoutesProbe id="a.one" />);
  expect(await screen.findByText("rows old")).toBeInTheDocument();
  rerender(<RoutesProbe id="b.two" />);
  expect(screen.queryByText("rows old")).toBeNull();
  expect(screen.getByText("loading")).toBeInTheDocument();
  release?.();
  expect(await screen.findByText("rows new")).toBeInTheDocument();
});

it("never renders a response for another install", async () => {
  stubApi({ "/api/v1/installs/b.two/routes": list("a.one", "old") });
  renderWithTheme(<RoutesProbe id="b.two" />);
  await waitFor(() => expect(fetched()).toContain("/api/v1/installs/b.two/routes"));
  // Let the response settle; it must stay hidden.
  await new Promise((r) => setTimeout(r, 20));
  expect(screen.getByText("loading")).toBeInTheDocument();
});

it("a 400 on a cursor drops it and loads page one", async () => {
  stubApi({
    "/api/v1/installs/a.one/routes?sort=name&cursor=stale": [400, { error: "bad query: cursor" }],
    "/api/v1/installs/a.one/routes?sort=name": list("a.one", "first"),
  });
  renderWithTheme(<RoutesProbe id="a.one" query="sort=name&cursor=stale" />);
  expect(await screen.findByText("rows first cursor rejected")).toBeInTheDocument();
});

it("a rejected cursor stops polling while page one keeps polling", async () => {
  vi.useFakeTimers();
  const stale = "/api/v1/installs/a.one/routes?sort=name&cursor=stale";
  const pageOne = "/api/v1/installs/a.one/routes?sort=name";
  stubApi({ [stale]: [400, { error: "bad query: cursor" }], [pageOne]: list("a.one", "first") });
  renderWithTheme(<RoutesProbe id="a.one" query="sort=name&cursor=stale" />);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(100);
  });
  expect(screen.getByText("rows first cursor rejected")).toBeInTheDocument();
  const count = (u: string) => fetched().filter((f) => f === u).length;
  const [before, firstBefore] = [count(stale), count(pageOne)];
  await act(async () => {
    await vi.advanceTimersByTimeAsync(4 * POLL_MS);
  });
  expect(count(stale)).toBe(before);
  expect(count(pageOne)).toBeGreaterThan(firstBefore);
});

it("a 400 without a cursor is an error, not a rejected cursor", async () => {
  stubApi({ "/api/v1/installs/a.one/routes?limit=0": [400, { error: "bad query" }] });
  renderWithTheme(<RoutesProbe id="a.one" query="limit=0" />);
  expect(await screen.findByText("error 400")).toBeInTheDocument();
});

function OtherProbes({ id }: { id: string }) {
  const gws = useGateways(id, new URLSearchParams());
  const lss = useListenerSets(id, new URLSearchParams("parent=x"));
  const g = useGatewayDetail(id, "infra", "edge");
  const ls = useListenerSetDetail(id, { group: gw, kind: "ListenerSet", namespace: "infra", name: "ls1" });
  const r = useRouteDetail(id, { group: gw, kind: "HTTPRoute", namespace: "shop", name: "api" });
  const ready = [gws.data, lss.data, g.data, ls.data, r.data].every(Boolean);
  return <p>{ready ? "all loaded" : "loading"}</p>;
}

it("detail and other list hooks fetch their paths and check the install", async () => {
  const detail = { install: "a.one" };
  stubApi({
    "/api/v1/installs/a.one/gateways": list("a.one"),
    "/api/v1/installs/a.one/listenersets?parent=x": list("a.one"),
    "/api/v1/installs/a.one/gateways/infra/edge": detail,
    [`/api/v1/installs/a.one/listenersets/${gw}/ListenerSet/infra/ls1`]: detail,
    [`/api/v1/installs/a.one/routes/${gw}/HTTPRoute/shop/api`]: detail,
  });
  renderWithTheme(<OtherProbes id="a.one" />);
  expect(await screen.findByText("all loaded")).toBeInTheDocument();
});
