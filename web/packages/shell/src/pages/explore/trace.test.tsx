import { screen, within } from "@testing-library/react";
import { Route, Routes } from "react-router-dom";
import metaGolden from "../../../../../../pkg/api/testdata/golden/meta.json";
import routeGolden from "../../../../../../pkg/api/testdata/golden/route-detail.json";
import grpcGolden from "../../../../../../pkg/api/testdata/golden/route-detail-grpc.json";
import tcpGolden from "../../../../../../pkg/api/testdata/golden/route-detail-tcp.json";
import type { BackendRef, RouteDetail } from "../../api/types";
import { traceCopy } from "../../copy/explorer";
import { hrefPath, renderWithTheme } from "../../test/render";
import { RoutePage } from "./RoutePage";

// The route trace on route detail (Phase 6 design §7) over the API's golden responses for each route kind.
const ID = "kgw.kgateway";
const GW = "gateway.networking.k8s.io";
const API = `/api/v1/installs/${ID}`;
const detail = (g: { body: unknown }, collectedAt = new Date().toISOString()): RouteDetail =>
  structuredClone({ ...(g.body as RouteDetail), collectedAt });

function serve(routes: Record<string, RouteDetail>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const u = new URL(url, "http://ui");
      if (u.pathname === "/api/v1/meta") return new Response(JSON.stringify(metaGolden.body));
      const d = routes[u.pathname.replace(`${API}/routes/${GW}/`, "")];
      if (d) return new Response(JSON.stringify(d));
      // Child-route and other list reads: empty.
      if (u.pathname === `${API}/routes`) return new Response(JSON.stringify({ install: ID, items: [], total: 0 }));
      return new Response(JSON.stringify({ error: "not found" }), { status: 404 });
    }),
  );
}

async function open(kind: string, ns: string, name: string, d: RouteDetail) {
  serve({ [`${kind}/${ns}/${name}`]: d });
  renderWithTheme(
    <Routes>
      <Route path="/i/:id/routes/:group/:kind/:ns/:name" element={<RoutePage />} />
    </Routes>,
    { path: `/i/${ID}/routes/${GW}/${kind}/${ns}/${name}` },
  );
  return screen.findByRole("region", { name: /^Rule 1\b/ });
}

const line = (rule: HTMLElement, name: string) => within(rule).getByTestId(`backend-${name}`);
const refs = (d: RouteDetail, rule = 0) => d.rules[rule]?.backendRefs as BackendRef[];

it("a GRPCRoute: the match line shows service/method, weights 1/3 show 25 % / 75 %, and the noun follows the kind", async () => {
  const rule = await open("GRPCRoute", "shop", "grpc", detail(grpcGolden));
  expect(screen.getByTestId("detail-header")).toHaveTextContent("GRPCRoute");
  expect(within(rule).getByTestId("match")).toHaveTextContent("gRPC method Exact cart.Cart/Get");
  expect(within(rule).getByTestId("match")).toHaveTextContent("Header x-env Exact prod");
  const bar = within(rule).getByTestId("share-bar");
  expect(bar).toHaveTextContent("% of this rule's requests");
  expect(
    within(bar)
      .getAllByTestId("share-segment")
      .map((s) => s.textContent),
  ).toEqual(["25 %", "75 %"]);
  expect(within(line(rule, "cart-svc")).getByTestId("share")).toHaveTextContent("Share 25 %");
  expect(within(line(rule, "other-svc")).getByTestId("share")).toHaveTextContent("Share 75 %");
  // The filters as written sit in their own marked area, even when there are none.
  expect(within(rule).getByTestId("rule-filters")).toHaveTextContent(traceCopy.noFilters);
});

it("serves lists each attachment with its state, and a backend's signals name the attachment they came from", async () => {
  const rule = await open("GRPCRoute", "shop", "grpc", detail(grpcGolden));
  const serves = screen.getAllByTestId("serves");
  expect(serves.map((s) => s.textContent)).toEqual([
    expect.stringMatching(/^Healthy.*infra\/edge.*Listener http :80.*Any hostname$/),
    expect.stringMatching(/^Error.*infra\/other.*Listener web :8080.*Any hostname$/),
  ]);
  const why = within(line(rule, "cart-svc")).getByTestId("problem");
  expect(why).toHaveTextContent("On infra/other, listener web: Listener Programmed is False");
  expect(within(line(rule, "cart-svc")).getByTestId("path-state")).toHaveTextContent("Error");
  // A signal of the backend itself has no attachment: it comes first, unprefixed.
  const own = within(line(rule, "other-svc")).getAllByRole("listitem");
  expect(own[0]).toHaveTextContent(/^Not seen in the cluster yet/);
  expect(hrefPath(within(line(rule, "cart-svc")).getByRole("link", { name: /shop\/cart-svc/ }))).toBe(
    `/i/${ID}/backends/-/Service/shop/cart-svc`,
  );
});

it("a TCPRoute shows no match line or filters, and a failing ref says connections fail", async () => {
  const d = detail(tcpGolden);
  const cart = refs(d).find((b) => b.ref.name === "cart-svc") as BackendRef;
  cart.shareState = "failing";
  const rule = await open("TCPRoute", "shop", "tcp", d);
  expect(screen.getByTestId("detail-header")).toHaveTextContent("TCPRoute");
  // A TCPRoute matches no hostnames: none in the summary or on its attachments.
  expect(screen.queryByText("Hostnames")).toBeNull();
  expect(screen.queryByTestId("serves-hostnames")).toBeNull();
  expect(within(rule).queryByTestId("match")).toBeNull();
  expect(within(rule).queryByTestId("match-empty")).toBeNull();
  expect(within(rule).queryByText("Matches")).toBeNull();
  expect(within(rule).queryByTestId("rule-filters")).toBeNull();
  expect(within(rule).getByTestId("share-failing")).toHaveTextContent("100 % of connections fail");
  expect(within(line(rule, "cart-svc")).getByTestId("share")).toHaveTextContent("Share 100 % connections fail");
  expect(rule).not.toHaveTextContent("requests fail");
  // Weight 0 beside a weighted ref: 0 % with no traffic, and no part of the bar.
  expect(within(line(rule, "other-svc")).getByTestId("share")).toHaveTextContent("Share 0 % no traffic");
  expect(within(rule).getAllByTestId("share-segment")).toHaveLength(1);
});

it("all weights 0: the bar says no traffic and every ref shows 0 %, no traffic", async () => {
  const d = detail(tcpGolden);
  for (const b of refs(d)) Object.assign(b, { weight: 0, share: 0, shareState: "no-traffic" });
  const rule = await open("TCPRoute", "shop", "tcp", d);
  const bar = within(rule).getByTestId("share-bar");
  expect(bar).toHaveAttribute("data-state", "no-traffic");
  expect(bar).toHaveTextContent(traceCopy.noTraffic);
  expect(within(bar).queryAllByTestId("share-segment")).toHaveLength(0);
  for (const name of ["cart-svc", "other-svc"])
    expect(within(line(rule, name)).getByTestId("share")).toHaveTextContent("Share 0 % no traffic");
});

it("a rule mixing delegation and ordinary refs shows no bar and no share", async () => {
  const d = detail(routeGolden);
  const plain = refs(d, 2)[0] as BackendRef;
  refs(d).push({ ...plain, share: undefined, shareState: "not-shown" });
  const rule = await open("HTTPRoute", "shop", "mid", d);
  const bar = within(rule).getByTestId("share-bar");
  expect(bar).toHaveAttribute("data-state", "not-shown");
  expect(bar).toHaveTextContent("No share: this rule also delegates.");
  expect(within(bar).queryByRole("img")).toBeNull();
  expect(within(line(rule, "cart-svc")).getByTestId("share")).toHaveTextContent("Share Not shown");
});

it("may-fail refs flag their share; a rule of delegation refs only has no bar", async () => {
  await open("HTTPRoute", "shop", "mid", detail(routeGolden));
  const second = screen.getByRole("region", { name: /^Rule 2\b/ });
  expect(within(second).getByTestId("share-may-fail")).toHaveTextContent("100 % may fail");
  expect(
    within(second)
      .getAllByTestId("share-segment")
      .map((s) => s.getAttribute("data-state")),
  ).toEqual(["warning", "warning", "warning"]);
  // Rule 1 only delegates.
  expect(within(screen.getByRole("region", { name: /^Rule 1\b/ })).queryByTestId("share-bar")).toBeNull();
});

it("stale route data shows every trace state unknown and no flags", async () => {
  const d = detail(grpcGolden, new Date(Date.now() - 10 * 60_000).toISOString());
  (refs(d)[0] as BackendRef).shareState = "failing";
  const rule = await open("GRPCRoute", "shop", "grpc", d);
  await screen.findByText(/Data is stale/);
  for (const s of screen.getAllByTestId("serves")) expect(s).toHaveTextContent(/^Unknown/);
  expect(within(line(rule, "cart-svc")).getByTestId("path-state")).toHaveTextContent("Unknown");
  expect(within(rule).queryByTestId("problem")).toBeNull();
  expect(within(rule).queryByTestId("share-failing")).toBeNull();
  expect(within(line(rule, "cart-svc")).getByTestId("share")).not.toHaveTextContent("requests fail");
  expect(
    within(rule)
      .getAllByTestId("share-segment")
      .map((s) => s.getAttribute("data-state")),
  ).toEqual(["unknown", "unknown"]);
});
