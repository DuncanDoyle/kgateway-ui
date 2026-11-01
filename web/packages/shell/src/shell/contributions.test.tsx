import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { Route, Routes, useLocation } from "react-router-dom";
import gatewayGolden from "../../../../../pkg/api/testdata/golden/gateway-detail.json";
import listenersetsGolden from "../../../../../pkg/api/testdata/golden/listenersets.json";
import metaGolden from "../../../../../pkg/api/testdata/golden/meta.json";
import routesGolden from "../../../../../pkg/api/testdata/golden/routes.json";
import type { Finding, GatewayDetail, ListenerSetList, RouteList } from "../api/types";
import { DetailFrame } from "../pages/explore/detailParts";
import { GatewayPage } from "../pages/explore/GatewayPage";
import { FindingRow } from "../pages/install/FindingRow";
import { InstallPage } from "../pages/install/InstallPage";
import { installSummary, meta, overview, unhealthy } from "../test/fixtures";
import { renderWithTheme, stubApi } from "../test/render";
import type { Contributions } from "./contributions";
import { ShellExtensionContext, useShellExtension } from "./extension";

const ID = "kgw.kgateway";
const GW = "gateway.networking.k8s.io";
const API = `/api/v1/installs/${ID}`;
const GW_PATH = `/i/${ID}/gateways/infra/edge`;
const now = () => new Date().toISOString();

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

/** Adds contributions to the default extension, as Shell does for a downstream. */
function With({ contributions, children }: { contributions: Contributions; children: ReactNode }) {
  const base = useShellExtension();
  return <ShellExtensionContext.Provider value={{ ...base, contributions }}>{children}</ShellExtensionContext.Provider>;
}

function Where() {
  const loc = useLocation();
  return <output data-testid="location">{loc.pathname + loc.search}</output>;
}
const location = () => screen.getByTestId("location").textContent ?? "";

function mountGateway(contributions: Contributions, path = GW_PATH) {
  // Served by path, so the sub-lists' queries need not be spelled out.
  const byPath: Record<string, unknown> = {
    "/api/v1/meta": metaGolden.body,
    [`${API}/gateways/infra/edge`]: { ...(gatewayGolden.body as GatewayDetail), collectedAt: now() },
    [`${API}/routes`]: { ...(routesGolden.body as RouteList), collectedAt: now() },
    [`${API}/listenersets`]: { ...(listenersetsGolden.body as ListenerSetList), collectedAt: now() },
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const body = byPath[new URL(url, "http://ui").pathname];
      return body === undefined
        ? new Response(JSON.stringify({ error: "not found" }), { status: 404 })
        : new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  return renderWithTheme(
    <With contributions={contributions}>
      <Routes>
        <Route path="/i/:id/gateways/:ns/:name" element={<GatewayPage />} />
      </Routes>
      <Where />
    </With>,
    { path },
  );
}

function mountInstall(contributions: Contributions) {
  stubApi({
    "/api/v1/installs": [installSummary()],
    [`${API}/overview`]: overview(),
    [`${API}/health`]: unhealthy(),
    "/api/v1/meta": meta,
  });
  return renderWithTheme(
    <With contributions={contributions}>
      <Routes>
        <Route path="/i/:id" element={<InstallPage />} />
      </Routes>
    </With>,
    { path: `/i/${ID}` },
  );
}

const widgets: Contributions = {
  detailTabs: {
    [`${GW}/Gateway`]: [
      {
        id: "widgets",
        label: "Widgets",
        render: (c) => <p>widgets of {`${c.installId} ${c.ref.namespace}/${c.ref.name}`}</p>,
      },
    ],
    [`${GW}/HTTPRoute`]: [{ id: "route-only", label: "Route only", render: () => <p>route only</p> }],
  },
};

it("a Gateway detail tab renders and its ?tab= survives reload", async () => {
  const user = userEvent.setup();
  const first = mountGateway(widgets);
  await screen.findByRole("table", { name: "Listeners" });
  const list = screen.getByRole("tablist", { name: "Gateway views" });
  expect(
    within(list)
      .getAllByRole("tab")
      .map((t) => t.textContent),
  ).toEqual(["Overview", "YAML", "Widgets"]);
  await user.click(screen.getByRole("tab", { name: "Widgets" }));
  await waitFor(() => expect(location()).toBe(`${GW_PATH}?tab=widgets`));
  expect(screen.getByRole("tabpanel")).toHaveTextContent("widgets of kgw.kgateway infra/edge");
  // Reload: the same URL opens on the contributed tab.
  first.unmount();
  mountGateway(widgets, `${GW_PATH}?tab=widgets`);
  expect(await screen.findByText("widgets of kgw.kgateway infra/edge")).toBeInTheDocument();
  expect(screen.getByRole("tab", { name: "Widgets" })).toHaveAttribute("aria-selected", "true");
});

it("side sections render after the core content, on the Overview tab only", async () => {
  mountGateway({
    sideSections: {
      [`${GW}/Gateway`]: [{ id: "notes", title: "Notes", render: (c) => <p>notes for {c.ref.name}</p> }],
    },
  });
  const section = await screen.findByRole("region", { name: "Notes" });
  expect(section).toHaveTextContent("notes for edge");
  const listeners = screen.getByRole("table", { name: "Listeners" });
  expect(listeners.compareDocumentPosition(section) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  await userEvent.setup().click(screen.getByRole("tab", { name: "YAML" }));
  expect(screen.queryByRole("region", { name: "Notes" })).not.toBeInTheDocument();
});

it("a slot with no contribution renders nothing and takes no space", async () => {
  const empty: Contributions = {
    detailTabs: { [`${GW}/Gateway`]: [] },
    sideSections: { [`${GW}/Gateway`]: [] },
    findingActions: [],
    installPanels: [],
  };
  const g = mountGateway(empty);
  await screen.findByRole("table", { name: "Listeners" });
  expect(screen.getAllByRole("tab")).toHaveLength(2);
  expect(document.querySelector("[data-slot]")).toBeNull();
  g.unmount();
  mountInstall(empty);
  await screen.findByRole("table", { name: "Gateways" });
  expect(screen.getAllByTestId("finding-row").length).toBeGreaterThan(0);
  expect(document.querySelector("[data-slot]")).toBeNull();
});

it("a contribution colliding with a core tab is ignored", async () => {
  const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
  const mine = (id: string) => ({ id, label: `Mine ${id}`, render: () => <p>mine {id}</p> });
  stubApi({ "/api/v1/meta": metaGolden.body });
  renderWithTheme(
    <With contributions={{ detailTabs: { [`${GW}/HTTPRoute`]: [mine("yaml"), mine("delegation"), mine("extra")] } }}>
      <DetailFrame
        installId={ID}
        refValue={{ group: GW, kind: "HTTPRoute", namespace: "shop", name: "web", link: "detail" }}
        noun="HTTPRoute"
        data={{ health: "healthy", collectedAt: now() }}
        extraTabs={[{ id: "delegation", label: "Delegation", content: <p>core delegation</p> }]}
      >
        <p>overview</p>
      </DetailFrame>
    </With>,
    { path: `/i/${ID}/routes/${GW}/HTTPRoute/shop/web?tab=delegation` },
  );
  expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual([
    "Overview",
    "YAML",
    "Delegation",
    "Mine extra",
  ]);
  expect(screen.getByRole("tabpanel")).toHaveTextContent("core delegation");
  expect(warn).toHaveBeenCalledWith(expect.stringContaining('"yaml"'));
  expect(warn).toHaveBeenCalledWith(expect.stringContaining('"delegation"'));
});

it("finding actions render only where applies() is true", () => {
  stubApi({ "/api/v1/installs": [installSummary()] });
  const finding = (code: string, extra: Partial<Finding> = {}): Finding => ({
    code,
    slug: "demo",
    severity: "warning",
    install: ID,
    message: `message ${code}`,
    ...extra,
  });
  renderWithTheme(
    <With
      contributions={{
        findingActions: [
          {
            id: "silence",
            label: "Silence",
            applies: (f) => f.code.startsWith("ACME-"),
            render: (f) => <button type="button">Silence {f.code}</button>,
          },
        ],
      }}
    >
      <FindingRow finding={finding("ACME-001")} installId={ID} />
      <FindingRow finding={finding("KGWUI-001")} installId={ID} />
      <FindingRow finding={finding("ACME-002", { restricted: true, message: "" })} installId={ID} />
    </With>,
  );
  const [acme, core, hidden] = screen.getAllByTestId("finding-row") as [HTMLElement, HTMLElement, HTMLElement];
  expect(within(acme).getByRole("group", { name: "Silence" })).toContainElement(
    within(acme).getByRole("button", { name: "Silence ACME-001" }),
  );
  expect(core.querySelector("[data-slot]")).toBeNull();
  // A restricted finding shows nothing beyond its label, so its actions stay hidden too.
  expect(hidden.querySelector("[data-slot]")).toBeNull();
});

it("install panels render below the Gateway table", async () => {
  mountInstall({
    installPanels: [{ id: "acme", title: "Acme panel", render: (c) => <p>panel for {c.installId}</p> }],
  });
  const table = await screen.findByRole("table", { name: "Gateways" });
  const panel = screen.getByRole("region", { name: "Acme panel" });
  expect(panel).toHaveTextContent("panel for kgw.kgateway");
  expect(table.compareDocumentPosition(panel) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});
