import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes, useLocation } from "react-router-dom";
import gatewayGolden from "../../../../../../pkg/api/testdata/golden/gateway-detail.json";
import dataPlaneGolden from "../../../../../../pkg/api/testdata/golden/gateway-detail-data-plane.json";
import dataPlaneRestrictedGolden from "../../../../../../pkg/api/testdata/golden/gateway-detail-data-plane-restricted.json";
import proxiesGolden from "../../../../../../pkg/api/testdata/golden/gateway-detail-proxies.json";
import proxiesRestrictedGolden from "../../../../../../pkg/api/testdata/golden/gateway-detail-proxies-restricted.json";
import listenersetsGolden from "../../../../../../pkg/api/testdata/golden/listenersets.json";
import metaGolden from "../../../../../../pkg/api/testdata/golden/meta.json";
import processesGolden from "../../../../../../pkg/api/testdata/golden/processes.json";
import processesRestrictedGolden from "../../../../../../pkg/api/testdata/golden/processes-restricted.json";
import routesGolden from "../../../../../../pkg/api/testdata/golden/routes.json";
import type { GatewayDetail, ListenerSetList, ProcessesResponse, RouteList } from "../../api/types";
import { dataPlaneCopy, diagnosticsCopy, processesCopy } from "../../copy/explorer";
import { hrefPath, pseudoRules, renderWithTheme } from "../../test/render";
import { controlRoomDark, controlRoomLight } from "../../theme/themes";
import { GatewayPage } from "../explore/GatewayPage";
import { ProcessesPage } from "./ProcessesPage";

const ID = "kgw.kgateway";
const API = `/api/v1/installs/${ID}`;
const now = () => new Date().toISOString();
const fresh = <T extends { collectedAt: string }>(b: unknown): T => ({ ...(b as T), collectedAt: now() });
const processes = (g: { body: unknown } = processesGolden): ProcessesResponse => {
  const b = fresh<ProcessesResponse>(g.body);
  return { ...b, proxies: { ...b.proxies, collectedAt: b.collectedAt } };
};
const gateway = (g: { body: unknown }, over: Partial<GatewayDetail> = {}): GatewayDetail => ({
  ...fresh<GatewayDetail>(g.body),
  ...over,
});

type Reply = unknown | [number, unknown];
/** Stubs fetch with a handler over the parsed request URL; undefined is a 404. */
function serve(fn: (u: URL) => Reply | undefined) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const u = new URL(url, "http://ui");
      const r =
        u.pathname === "/api/v1/meta"
          ? metaGolden.body
          : u.pathname === `${API}/routes`
            ? fresh<RouteList>(routesGolden.body)
            : u.pathname === `${API}/listenersets`
              ? fresh<ListenerSetList>(listenersetsGolden.body)
              : fn(u);
      if (r === undefined) return new Response(JSON.stringify({ error: "not found" }), { status: 404 });
      const [status, body] = Array.isArray(r) && typeof r[0] === "number" ? (r as [number, unknown]) : [200, r];
      return new Response(JSON.stringify(body), { status });
    }),
  );
}
const calls = () => vi.mocked(fetch).mock.calls.map((c) => new URL(String(c[0]), "http://ui"));
const procCalls = () => calls().filter((u) => u.pathname === `${API}/processes`);

function Where() {
  const loc = useLocation();
  return <output data-testid="location">{loc.pathname + loc.search}</output>;
}
const location = () => screen.getByTestId("location").textContent ?? "";
const search = () => new URLSearchParams(location().split("?")[1] ?? "");

function mount(path: string, themeId?: string) {
  return renderWithTheme(
    <>
      <Routes>
        <Route path="/i/:id/processes" element={<ProcessesPage />} />
        <Route path="/i/:id/gateways/:ns/:name" element={<GatewayPage />} />
        <Route path="*" element={null} />
      </Routes>
      <Where />
    </>,
    { path, themeId },
  );
}
const proxiesTable = () => screen.getByRole("table", { name: processesCopy.gatewayProxies });
const rowOf = (name: string) =>
  within(proxiesTable())
    .getAllByRole("row")
    .find((r) => within(r).queryByText(name)) as HTMLElement;
const dataPlane = () => screen.getByRole("region", { name: dataPlaneCopy.title });
const proxiesSection = () => screen.getByRole("region", { name: processesCopy.proxies });

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Processes page", () => {
  it("Processes page filters through the URL", async () => {
    serve((u) => (u.pathname === `${API}/processes` ? processes() : undefined));
    const first = mount(`/i/${ID}/processes?health=warning,error&skew=true&q=edge&x=ignored`);
    await screen.findByText("kgateway-0");
    // Only documented params reach the API, health as repeated keys.
    expect([...(procCalls()[0]?.searchParams.entries() ?? [])]).toEqual([
      ["health", "warning"],
      ["health", "error"],
      ["q", "edge"],
      ["skew", "true"],
    ]);
    const show = screen.getByRole("group", { name: processesCopy.showOnly });
    expect(within(show).getByRole("button", { name: processesCopy.skew })).toHaveAttribute("aria-pressed", "true");
    expect(within(show).getByRole("button", { name: processesCopy.restarting })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
    expect(screen.getByRole("searchbox", { name: processesCopy.search })).toHaveValue("edge");
    // A reload restores the same view from the URL.
    const saved = location();
    first.unmount();
    vi.mocked(fetch).mockClear();
    mount(saved);
    await screen.findByText("kgateway-0");
    expect(procCalls()[0]?.searchParams.get("skew")).toBe("true");
    expect(procCalls()[0]?.searchParams.getAll("health")).toEqual(["warning", "error"]);
    // Toggling the chips writes the URL and the query; skew off removes the param, never skew=false.
    await userEvent.click(
      within(screen.getByRole("group", { name: processesCopy.showOnly })).getByRole("button", {
        name: processesCopy.restarting,
      }),
    );
    await waitFor(() => expect(search().get("restarts")).toBe("true"));
    await userEvent.click(screen.getByRole("button", { name: processesCopy.skew }));
    await waitFor(() => expect(search().has("skew")).toBe(false));
    await waitFor(() => expect(procCalls().at(-1)?.searchParams.get("restarts")).toBe("true"));
    expect(procCalls().at(-1)?.searchParams.has("skew")).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: /Unknown/ }));
    await waitFor(() => expect(search().get("health")).toBe("warning,error,unknown"));
    // Clear filters drops every filter.
    await userEvent.click(screen.getAllByRole("button", { name: "Clear filters" })[0] as HTMLElement);
    await waitFor(() => expect(location()).toBe(`/i/${ID}/processes?x=ignored`));
  });

  it("lists the controller and each Gateway's proxies with the server's state", async () => {
    serve((u) => (u.pathname === `${API}/processes` ? processes() : undefined));
    mount(`/i/${ID}/processes`);
    const ctrl = await screen.findByRole("table", { name: processesCopy.controllerTable });
    expect(within(ctrl).getByText("kgateway-0")).toBeInTheDocument();
    expect(screen.getByText("3 Gateways")).toBeInTheDocument();
    const edge = rowOf("edge");
    expect(within(edge).getByText("Warning")).toBeInTheDocument();
    expect(within(edge).getByText(diagnosticsCopy.readyOf(1, 1))).toBeInTheDocument();
    expect(within(edge).getByText("2.4.5")).toBeInTheDocument();
    expect(within(edge).getByText(processesCopy.differs)).toBeInTheDocument();
    expect(within(edge).getByText("1")).toBeInTheDocument();
    expect(hrefPath(within(edge).getByRole("link", { name: /edge/ }))).toBe(`/i/${ID}/gateways/infra/edge`);
    expect(within(rowOf("calm")).queryByText(processesCopy.differs)).toBeNull();
    // One page and no denied count: the empty footer is gone once loaded.
    expect(screen.queryByTestId("list-footer")).toBeNull();
  });

  it("not observed for a self-managed Gateway", async () => {
    serve((u) => (u.pathname === `${API}/processes` ? processes() : undefined));
    mount(`/i/${ID}/processes`);
    await screen.findByText("kgateway-0");
    const lone = rowOf("lone");
    expect(within(lone).getByText(diagnosticsCopy.notObserved)).toBeInTheDocument();
    expect(within(lone).getByText("Unknown")).toBeInTheDocument();
    expect(within(lone).queryByText("Healthy")).toBeNull();
  });

  it("a restricted caller sees counts, never hidden Pod names", async () => {
    serve((u) => (u.pathname === `${API}/processes` ? processes(processesRestrictedGolden) : undefined));
    const { container } = mount(`/i/${ID}/processes`);
    await screen.findByText("2 Gateways");
    expect(container.textContent).not.toContain("kgateway-0");
    expect(container.textContent).not.toContain("edge-0");
    const ctrl = screen.getByRole("region", { name: diagnosticsCopy.controller });
    expect(within(ctrl).getByText(diagnosticsCopy.readyOf(1, 1))).toBeInTheDocument();
    expect(within(rowOf("edge")).getByText(diagnosticsCopy.readyOf(1, 1))).toBeInTheDocument();
  });

  it("hidden versions and restarts read Hidden, not Not reported", async () => {
    serve((u) => (u.pathname === `${API}/processes` ? processes(processesRestrictedGolden) : undefined));
    mount(`/i/${ID}/processes`);
    await screen.findByText("2 Gateways");
    const edge = rowOf("edge");
    expect(within(edge).queryByText(diagnosticsCopy.notReported)).toBeNull();
    expect(within(edge).getAllByText(processesCopy.hidden).length).toBeGreaterThan(0);
  });

  it("a failed first load shows the error, not a shimmering controller", async () => {
    serve((u) => (u.pathname === `${API}/processes` ? [500, { error: "boom" }] : undefined));
    mount(`/i/${ID}/processes`);
    expect(await screen.findByText("boom")).toBeInTheDocument();
    expect(screen.queryByTestId("shimmer")).toBeNull();
    expect(screen.queryByRole("region", { name: diagnosticsCopy.controller })).toBeNull();
  });

  it("the Show only chips carry a visible label", async () => {
    serve((u) => (u.pathname === `${API}/processes` ? processes() : undefined));
    mount(`/i/${ID}/processes`);
    await screen.findByText("kgateway-0");
    const label = screen.getByText(processesCopy.showOnly);
    expect(label).toBeVisible();
    // The label sits beside its chips, outside the health group.
    expect(label.closest('[role="group"]')).toBeNull();
    expect(label.parentElement).toContainElement(screen.getByRole("button", { name: processesCopy.skew }));
  });

  it("stale data turns every state unknown", async () => {
    const old = new Date(Date.now() - 10 * 60_000).toISOString();
    serve((u) =>
      u.pathname === `${API}/processes`
        ? { ...processes(), collectedAt: old, proxies: { ...processes().proxies, collectedAt: old } }
        : undefined,
    );
    mount(`/i/${ID}/processes`);
    await screen.findByText("kgateway-0");
    expect(within(rowOf("edge")).queryByText("Warning")).toBeNull();
    expect(within(rowOf("calm")).queryByText("Healthy")).toBeNull();
    expect(screen.queryAllByText("Ready")).toHaveLength(1); // the column header only
  });

  describe.each([controlRoomDark, controlRoomLight])("in the $name theme", (t) => {
    it("the Show only chips have hover, active and focus-visible styles", async () => {
      serve((u) => (u.pathname === `${API}/processes` ? processes() : undefined));
      mount(`/i/${ID}/processes`, t.id);
      await screen.findByText("kgateway-0");
      const chip = screen.getByRole("button", { name: processesCopy.skew });
      for (const p of [":hover", ":active", ":focus-visible"]) expect(pseudoRules(chip, p).length).toBeGreaterThan(0);
    });
  });
});

describe("Gateway page sections", () => {
  const GW = (name: string) => `/i/${ID}/gateways/infra/${name}`;

  it("Data plane section lists rejects, the status contradiction, commands and recently changed objects", async () => {
    serve((u) =>
      u.pathname === `${API}/gateways/infra/nack`
        ? gateway(dataPlaneGolden, {
            dataPlane: {
              ...(dataPlaneGolden.body.dataPlane as GatewayDetail["dataPlane"] & object),
              statusSaysHealthy: true,
            },
          })
        : undefined,
    );
    mount(GW("nack"));
    await screen.findByRole("region", { name: dataPlaneCopy.title });
    const dp = dataPlane();
    // The contradiction is the headline: status is green, proxies reject.
    expect(within(dp).getByText(diagnosticsCopy.statusSaysHealthy)).toBeInTheDocument();
    const types = within(dp).getByRole("table", { name: diagnosticsCopy.rejectedTypes });
    expect(within(types).getByText("Cluster")).toBeInTheDocument();
    expect(within(types).getByText("RouteConfiguration")).toBeInTheDocument();
    expect(within(types).getAllByText("2")).toHaveLength(2);
    // Commands as text, with the permission each needs and the secret-care note.
    expect(within(dp).getByText(/kubectl logs -n kgw kgateway-0 --since=1h/)).toBeInTheDocument();
    expect(within(dp).getByText(/kubectl port-forward -n infra deploy\/nack 19000:19000/)).toBeInTheDocument();
    expect(within(dp).getAllByText(/Needs pods\/log/).length).toBeGreaterThan(0);
    expect(within(dp).getAllByText(/Share them with care/).length).toBeGreaterThan(0);
    // Recently changed objects link to their pages, with the hint that they are not a cause.
    expect(within(dp).getByText(diagnosticsCopy.recentlyChangedNote)).toBeInTheDocument();
    expect(hrefPath(within(dp).getByRole("link", { name: /fresh-n/ }))).toBe(
      `/i/${ID}/routes/gateway.networking.k8s.io/HTTPRoute/shop/fresh-n`,
    );
    // The snapshot is the controller's build, never a verdict.
    expect(within(dp).getByText(dataPlaneCopy.snapshotNote)).toBeInTheDocument();
  });

  it("Data plane without the contradiction says proxies reject; hidden evidence leaves no names", async () => {
    serve((u) => (u.pathname === `${API}/gateways/infra/nack` ? gateway(dataPlaneRestrictedGolden) : undefined));
    const { container } = mount(GW("nack"));
    await screen.findByRole("region", { name: dataPlaneCopy.title });
    const dp = dataPlane();
    expect(within(dp).getByText(dataPlaneCopy.rejecting)).toBeInTheDocument();
    expect(within(dp).queryByText(diagnosticsCopy.statusSaysHealthy)).toBeNull();
    expect(within(dp).getByText(dataPlaneCopy.evidenceRestricted)).toBeInTheDocument();
    expect(within(dp).queryByRole("table", { name: diagnosticsCopy.rejectedTypes })).toBeNull();
    expect(container.textContent).not.toContain("fresh-n");
    expect(container.textContent).not.toContain("kgateway-0");
    // Steps name only visible objects, so the visible replica's step stays.
    expect(within(dp).getByText(/kgateway-1/)).toBeInTheDocument();
  });

  it("no rejects reads healthy only while the check runs; unavailable reads unknown", async () => {
    const base = gatewayGolden.body.dataPlane as GatewayDetail["dataPlane"] & object;
    let unavailable = false;
    serve((u) =>
      u.pathname === `${API}/gateways/infra/edge`
        ? gateway(gatewayGolden, { dataPlane: { ...base, unavailable } })
        : undefined,
    );
    const first = mount(GW("edge"));
    await screen.findByRole("region", { name: dataPlaneCopy.title });
    expect(within(dataPlane()).getByText(diagnosticsCopy.noRejects)).toBeInTheDocument();
    // Proxy stats show the server's reason when they do not run.
    expect(within(dataPlane()).getByText("Proxy stats are not collected yet")).toBeInTheDocument();
    first.unmount();
    unavailable = true;
    mount(GW("edge"));
    await screen.findByRole("region", { name: dataPlaneCopy.title });
    expect(within(dataPlane()).queryByText(diagnosticsCopy.noRejects)).toBeNull();
    expect(within(dataPlane()).getByText(dataPlaneCopy.unavailable)).toBeInTheDocument();
  });

  it("not observed for a self-managed Gateway on its page", async () => {
    const base = gatewayGolden.body.dataPlane as GatewayDetail["dataPlane"] & object;
    serve((u) =>
      u.pathname === `${API}/gateways/infra/edge`
        ? gateway(gatewayGolden, {
            dataPlane: { ...base, proxyStats: { available: false, unavailable: "not-observed" } },
          })
        : undefined,
    );
    mount(GW("edge"));
    await screen.findByRole("region", { name: processesCopy.proxies });
    expect(within(proxiesSection()).getByText(diagnosticsCopy.notObserved)).toBeInTheDocument();
    expect(within(proxiesSection()).queryByText("Healthy")).toBeNull();
    expect(
      within(dataPlane()).getByText(diagnosticsCopy.unavailableReason["not-observed"] as string),
    ).toBeInTheDocument();
  });

  it("Proxies section hides Pod names when restricted", async () => {
    let golden: { body: unknown } = proxiesGolden;
    serve((u) => (u.pathname === `${API}/gateways/infra/edge` ? gateway(golden) : undefined));
    const first = mount(GW("edge"));
    await screen.findByRole("region", { name: processesCopy.proxies });
    expect(within(proxiesSection()).getByText("edge-0")).toBeInTheDocument();
    expect(within(proxiesSection()).getByText(diagnosticsCopy.readyOf(1, 1))).toBeInTheDocument();
    first.unmount();
    golden = proxiesRestrictedGolden;
    const { container } = mount(GW("edge"));
    await screen.findByRole("region", { name: processesCopy.proxies });
    expect(container.textContent).not.toContain("edge-0");
    expect(within(proxiesSection()).getByText(diagnosticsCopy.readyOf(1, 1))).toBeInTheDocument();
    expect(within(proxiesSection()).getByText(diagnosticsCopy.noProcesses)).toBeInTheDocument();
  });
});
