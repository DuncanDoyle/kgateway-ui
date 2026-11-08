import { render, screen, waitFor, within } from "@testing-library/react";
import { SWRConfig } from "swr";
import dataPlaneGolden from "../../../../../../pkg/api/testdata/golden/gateway-detail-data-plane.json";
import installsGolden from "../../../../../../pkg/api/testdata/golden/installs.json";
import metaGolden from "../../../../../../pkg/api/testdata/golden/meta.json";
import routeGolden from "../../../../../../pkg/api/testdata/golden/route-detail.json";
import type { InstallSummary } from "../../api/types";
import { dataPlaneCopy, delegationCopy, detailCopy, routeCopy } from "../../copy/explorer";
import { kgatewayWebPack } from "../../kgateway/webPack";
import type { DetailTabContribution } from "../../shell/contributions";
import type { ShellExtension } from "../../shell/extension";
import { Shell } from "../../shell/Shell";
import { testSWR } from "../../test/render";
import { defaultPages } from "../defaultPages";

const ID = "kgw.kgateway";
const GW = "gateway.networking.k8s.io";
const API = `/api/v1/installs/${ID}`;
const ROUTE = `/i/${ID}/routes/${GW}/HTTPRoute/shop/mid`;
const GATEWAY = `/i/${ID}/gateways/infra/nack`;
const ALL = ["delegation", "computed", "dataplane", "processes"];
const fresh = <T,>(g: { body: T }) => ({ ...g.body, collectedAt: new Date().toISOString() });
const rows = { install: ID, items: [], total: 0, collectedAt: new Date().toISOString() };

/** The goldens, with the install's features as given; undefined sends none, as an older server does. */
function serve(features: string[] | undefined) {
  const install = { ...(installsGolden.body[0] as InstallSummary), features };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const p = new URL(url, "http://ui").pathname;
      const body =
        p === "/api/v1/meta"
          ? metaGolden.body
          : p === "/api/v1/installs"
            ? [install]
            : p === `${API}/routes/${GW}/HTTPRoute/shop/mid`
              ? fresh(routeGolden)
              : p === `${API}/gateways/infra/nack`
                ? fresh(dataPlaneGolden)
                : p.endsWith("/routes") || p.endsWith("/listenersets") || p.endsWith("/delegation")
                  ? rows
                  : undefined;
      if (body === undefined) return new Response(JSON.stringify({ error: "not found" }), { status: 404 });
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
}

function mount(path: string, ext: ShellExtension = kgatewayWebPack) {
  window.history.pushState({}, "", path);
  return render(
    <SWRConfig value={testSWR()}>
      <Shell extension={ext} pages={defaultPages} />
    </SWRConfig>,
  );
}
const installsLoaded = () => waitFor(() => expect(screen.getByTestId("detail-header")).toBeInTheDocument());

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("route page", () => {
  it.each([
    ["every feature", ALL],
    ["an older server that sends none", undefined],
  ])(
    "with %s: the Delegation tab, permission line, child routes, effective matches and computed badges",
    async (_, f) => {
      serve(f);
      mount(ROUTE);
      expect(await screen.findByRole("tab", { name: delegationCopy.tab })).toBeInTheDocument();
      expect(screen.getByText(routeCopy.parentRoutes)).toBeInTheDocument();
      expect(screen.getByRole("heading", { name: new RegExp(`^${routeCopy.childRoutes}`) })).toBeInTheDocument();
      expect(screen.getByRole("heading", { name: /^Effective matches/ })).toBeInTheDocument();
      expect(screen.getAllByTestId("computed-badge").length).toBeGreaterThan(0);
    },
  );

  it("without delegation or computed: none of them, even on a deep link to the tab", async () => {
    serve([]);
    mount(`${ROUTE}?tab=delegation`);
    await screen.findByRole("heading", { name: new RegExp(`^${routeCopy.rules}`) });
    await installsLoaded();
    await waitFor(() => expect(screen.queryByRole("tab", { name: delegationCopy.tab })).toBeNull());
    expect(screen.getByRole("tab", { name: detailCopy.overview })).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByText(routeCopy.parentRoutes)).toBeNull();
    expect(screen.queryByRole("heading", { name: new RegExp(`^${routeCopy.childRoutes}`) })).toBeNull();
    expect(screen.queryByRole("heading", { name: /^Effective matches/ })).toBeNull();
    expect(screen.queryAllByTestId("computed-badge")).toHaveLength(0);
  });

  it("the Delegation tab comes from the kgateway web pack: the core alone has none", async () => {
    serve(ALL);
    mount(ROUTE, {});
    await screen.findByRole("heading", { name: new RegExp(`^${routeCopy.rules}`) });
    expect(screen.queryByRole("tab", { name: delegationCopy.tab })).toBeNull();
  });
});

describe("gateway page", () => {
  it("with dataplane and processes: the data plane and proxy sections", async () => {
    serve(ALL);
    mount(GATEWAY);
    expect(await screen.findByRole("region", { name: dataPlaneCopy.title })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Proxies" })).toBeInTheDocument();
    expect(screen.queryByText(dataPlaneCopy.notAvailable)).toBeNull();
  });

  it("without them: one line, Not available for this controller", async () => {
    serve([]);
    mount(GATEWAY);
    await screen.findByRole("table", { name: "Listeners" });
    const section = await screen.findByRole("region", { name: dataPlaneCopy.title });
    expect(within(section).getByText(dataPlaneCopy.notAvailable)).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Proxies" })).toBeNull();
    expect(screen.queryByText(dataPlaneCopy.rejecting)).toBeNull();
  });
});

describe("contributed detail tabs", () => {
  const tab = (o: Partial<DetailTabContribution>): DetailTabContribution => ({
    id: "extra",
    label: "Extra",
    render: () => <p>extra tab</p>,
    ...o,
  });
  const withTab = (t: DetailTabContribution): ShellExtension => ({
    contributions: { detailTabs: { [`${GW}/HTTPRoute`]: [t] } },
  });

  it("a tab with a feature shows only where the install has it", async () => {
    serve(["processes"]);
    const view = mount(ROUTE, withTab(tab({ feature: "processes" })));
    expect(await screen.findByRole("tab", { name: "Extra" })).toBeInTheDocument();
    view.unmount();
    serve(["computed"]);
    mount(ROUTE, withTab(tab({ feature: "processes" })));
    await screen.findByRole("heading", { name: new RegExp(`^${routeCopy.rules}`) });
    await installsLoaded();
    await waitFor(() => expect(screen.queryByRole("tab", { name: "Extra" })).toBeNull());
  });

  it("applies decides from the loaded detail", async () => {
    serve(ALL);
    const view = mount(
      ROUTE,
      withTab(tab({ applies: (ctx) => (ctx.detail as { childCount?: number }).childCount === 2 })),
    );
    expect(await screen.findByRole("tab", { name: "Extra" })).toBeInTheDocument();
    view.unmount();
    mount(ROUTE, withTab(tab({ applies: () => false })));
    await screen.findByRole("heading", { name: new RegExp(`^${routeCopy.rules}`) });
    expect(screen.queryByRole("tab", { name: "Extra" })).toBeNull();
  });
});
