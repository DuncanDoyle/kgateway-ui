import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactNode, useState } from "react";
import { Route, Routes, useLocation, useNavigate } from "react-router-dom";
import gatewayGolden from "../../../../../pkg/api/testdata/golden/gateway-detail.json";
import listenersetsGolden from "../../../../../pkg/api/testdata/golden/listenersets.json";
import metaGolden from "../../../../../pkg/api/testdata/golden/meta.json";
import routesGolden from "../../../../../pkg/api/testdata/golden/routes.json";
import type { Finding, GatewayDetail, ListenerSetList, RouteList } from "../api/types";
import { DetailFrame } from "../pages/explore/detailParts";
import { GatewayPage } from "../pages/explore/GatewayPage";
import { FindingPage } from "../pages/findings/FindingPage";
import { FindingRow } from "../pages/install/FindingRow";
import { InstallPage } from "../pages/install/InstallPage";
import { diagnostics, installSummary, meta, overview, unhealthy } from "../test/fixtures";
import { renderWithTheme, stubApi } from "../test/render";
import { type Contributions, type DetailContext, resetContributionWarnings } from "./contributions";
import { type FindingRenderer, ShellExtensionContext, useShellExtension } from "./extension";

const ID = "kgw.kgateway";
const GW = "gateway.networking.k8s.io";
const API = `/api/v1/installs/${ID}`;
const GW_PATH = `/i/${ID}/gateways/infra/edge`;
const now = () => new Date().toISOString();

afterEach(() => {
  resetContributionWarnings();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

/** Adds contributions to the default extension, as Shell does for a downstream. */
function With({
  contributions,
  findingRenderers,
  children,
}: {
  contributions: Contributions;
  findingRenderers?: Record<string, FindingRenderer>;
  children: ReactNode;
}) {
  const base = useShellExtension();
  return (
    <ShellExtensionContext.Provider
      value={{ ...base, contributions, findingRenderers: findingRenderers ?? base.findingRenderers }}
    >
      {children}
    </ShellExtensionContext.Provider>
  );
}

function Where() {
  const loc = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <output data-testid="location">{loc.pathname + loc.search}</output>
      <button type="button" onClick={() => navigate(`/i/${ID}/gateways/infra/other${loc.search}`)}>
        to other
      </button>
    </>
  );
}
const location = () => screen.getByTestId("location").textContent ?? "";

function mountGateway(contributions: Contributions, path = GW_PATH) {
  // Served by path, so the sub-lists' queries need not be spelled out.
  const byPath: Record<string, unknown> = {
    "/api/v1/meta": metaGolden.body,
    [`${API}/gateways/infra/edge`]: { ...(gatewayGolden.body as GatewayDetail), collectedAt: now() },
    [`${API}/gateways/infra/other`]: { ...(gatewayGolden.body as GatewayDetail), collectedAt: now() },
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
    [`${API}/diagnostics`]: diagnostics(unhealthy()),
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
  // Contributed sections sit in the side column.
  expect(section.closest("[data-testid=detail-side]")).not.toBeNull();
  const listeners = screen.getByRole("table", { name: "Listeners" });
  expect(listeners.compareDocumentPosition(section) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  await userEvent.setup().click(screen.getByRole("tab", { name: "YAML" }));
  expect(screen.queryByRole("region", { name: "Notes" })).not.toBeInTheDocument();
});

it("the skeleton holds the side column's place only where sections are contributed", async () => {
  mountGateway({ sideSections: { [`${GW}/Gateway`]: [{ id: "notes", title: "Notes", render: () => <p>n</p> }] } });
  expect(within(screen.getByTestId("detail-skeleton")).getByTestId("detail-side")).toBeInTheDocument();
  expect(await screen.findByRole("region", { name: "Notes" })).toBeInTheDocument();
  expect(screen.getByTestId("detail-side")).toBeInTheDocument();
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
  expect(screen.getAllByTestId("group-row").length).toBeGreaterThan(0);
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
  expect(warn).toHaveBeenCalledWith(expect.stringMatching(/^snellius: .*"yaml"/));
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

const boom = (): never => {
  throw new Error("boom");
};
/** Warnings that name id; React's own error log is silenced. */
function quiet() {
  vi.spyOn(console, "error").mockImplementation(() => {});
  const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
  return (id: string) =>
    warn.mock.calls.filter((c) => String(c[0]).startsWith(`snellius: hiding contribution "${id}"`)).length;
}

it("a throwing detail tab or side section leaves the Gateway page up and its slot empty", async () => {
  const warned = quiet();
  const first = mountGateway(
    {
      detailTabs: { [`${GW}/Gateway`]: [{ id: "bad-tab", label: "Bad", render: boom }] },
      sideSections: { [`${GW}/Gateway`]: [{ id: "bad-side", title: "Bad side", render: boom }] },
    },
    `${GW_PATH}?tab=bad-tab`,
  );
  await waitFor(() => expect(screen.getByRole("tab", { name: "Bad" })).toHaveAttribute("aria-selected", "true"));
  await waitFor(() => expect(warned("detail-tab:bad-tab")).toBe(1));
  expect(screen.getByTestId("detail-header")).toHaveTextContent("edge");
  expect(screen.getByRole("tabpanel")).toBeEmptyDOMElement();
  first.unmount();
  mountGateway({ sideSections: { [`${GW}/Gateway`]: [{ id: "bad-side", title: "Bad side", render: boom }] } });
  await screen.findByRole("table", { name: "Listeners" });
  expect(screen.queryByRole("region", { name: "Bad side" })).not.toBeInTheDocument();
  expect(document.querySelector("[data-slot]")).toBeNull();
  expect(warned("side-section:bad-side")).toBe(1);
});

it("a throwing install panel leaves the install page up and its slot empty", async () => {
  const warned = quiet();
  mountInstall({ installPanels: [{ id: "bad-panel", title: "Bad panel", render: boom }] });
  await screen.findByRole("table", { name: "Gateways" });
  expect(screen.getAllByTestId("group-row").length).toBeGreaterThan(0);
  expect(screen.queryByRole("region", { name: "Bad panel" })).not.toBeInTheDocument();
  expect(warned("install-panel:bad-panel")).toBe(1);
});

it("a throwing finding action or finding renderer leaves the finding page up and its slot empty", async () => {
  const warned = quiet();
  stubApi({
    "/api/v1/installs": [installSummary()],
    [`${API}/findings/f1-edge`]: {
      install: ID,
      finding: { id: "f1-edge", code: "KGWUI-001", slug: "xds-rejected", severity: "error", message: "Rejected" },
      affected: { install: ID, items: [], total: 0, collectedAt: now() },
      collectedAt: now(),
    },
    "/api/v1/meta": meta,
  });
  renderWithTheme(
    <With
      findingRenderers={{ "KGWUI-": boom }}
      contributions={{
        findingActions: [
          { id: "bad-applies", label: "Bad applies", applies: boom, render: () => <button type="button">x</button> },
          { id: "bad-render", label: "Bad render", applies: () => true, render: boom },
        ],
      }}
    >
      <Routes>
        <Route path="/i/:id/findings/:findingId" element={<FindingPage />} />
      </Routes>
    </With>,
    { path: `/i/${ID}/findings/f1-edge` },
  );
  expect(await screen.findByRole("heading", { level: 1 })).toHaveTextContent("Rejected");
  expect(screen.queryByRole("group", { name: "Bad applies" })).not.toBeInTheDocument();
  expect(screen.queryByRole("group", { name: "Bad render" })).not.toBeInTheDocument();
  // Every applicable action threw, so the actions area takes no space.
  const area = document.querySelector('[data-slot="finding-actions"]') as HTMLElement;
  expect(area).toBeEmptyDOMElement();
  expect(getComputedStyle(area).display).toBe("none");
  // One warning per contribution, however often it renders.
  expect(warned("finding-action:bad-applies")).toBe(1);
  expect(warned("finding-action:bad-render")).toBe(1);
  expect(warned("finding-renderer:KGWUI-001")).toBe(1);
});

it("a repeated contribution id renders once", async () => {
  const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
  const panel = (t: string) => ({ id: "dup-panel", title: t, render: () => <p>{t}</p> });
  const action = (l: string) => ({ id: "dup-action", label: l, applies: () => true, render: () => <span>{l}</span> });
  const g = mountGateway({
    sideSections: {
      [`${GW}/Gateway`]: [
        { id: "dup-side", title: "Side one", render: () => null },
        { id: "dup-side", title: "Side two", render: () => null },
      ],
    },
  });
  expect(await screen.findByRole("region", { name: "Side one" })).toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Side two" })).not.toBeInTheDocument();
  g.unmount();
  const install = mountInstall({ installPanels: [panel("Panel one"), panel("Panel two")] });
  await screen.findByRole("table", { name: "Gateways" });
  expect(screen.getByRole("region", { name: "Panel one" })).toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Panel two" })).not.toBeInTheDocument();
  install.unmount();
  renderWithTheme(
    <With contributions={{ findingActions: [action("A1"), action("A2")] }}>
      <FindingRow
        finding={{ code: "KGWUI-002", slug: "demo", severity: "warning", install: ID, message: "m" }}
        installId={ID}
      />
    </With>,
  );
  expect(screen.getAllByRole("group", { name: "A1" })).toHaveLength(1);
  expect(screen.queryByRole("group", { name: "A2" })).not.toBeInTheDocument();
  for (const id of ["dup-side", "dup-panel", "dup-action"])
    expect(warn).toHaveBeenCalledWith(expect.stringMatching(new RegExp(`^snellius: .*"${id}"`)));
});

it("a contribution that threw for one object renders for the next", async () => {
  const warned = quiet();
  const only = (name: string) => (c: DetailContext) => {
    if (c.ref.name === "edge") throw new Error("boom");
    return (
      <p>
        {name} for {c.ref.name}
      </p>
    );
  };
  const user = userEvent.setup();
  mountGateway(
    {
      detailTabs: { [`${GW}/Gateway`]: [{ id: "picky-tab", label: "Picky", render: only("tab") }] },
      sideSections: { [`${GW}/Gateway`]: [{ id: "picky-side", title: "Picky side", render: only("side") }] },
    },
    `${GW_PATH}?tab=picky-tab`,
  );
  await waitFor(() => expect(warned("detail-tab:picky-tab")).toBe(1));
  expect(screen.getByRole("tabpanel")).toBeEmptyDOMElement();
  // The same DetailFrame shows the next Gateway.
  await user.click(screen.getByRole("button", { name: "to other" }));
  expect(await screen.findByText("tab for other")).toBeInTheDocument();
  await user.click(screen.getByRole("tab", { name: "Overview" }));
  expect(await screen.findByRole("region", { name: "Picky side" })).toHaveTextContent("side for other");
});

it("a finding renderer or action that threw for one finding renders for the next", async () => {
  quiet();
  stubApi({ "/api/v1/installs": [installSummary()] });
  const picky = (what: string) => (f: Finding) => {
    if (f.code === "ACME-001") throw new Error("boom");
    return <span>{`${what} ${f.code}`}</span>;
  };
  // One row whose finding changes in place, as on a poll.
  function Row() {
    const [code, setCode] = useState("ACME-001");
    return (
      <>
        <FindingRow finding={{ code, slug: "demo", severity: "warning", install: ID, message: code }} installId={ID} />
        <button type="button" onClick={() => setCode("ACME-002")}>
          next
        </button>
      </>
    );
  }
  renderWithTheme(
    <With
      findingRenderers={{ "ACME-": picky("renderer") }}
      contributions={{
        findingActions: [{ id: "picky", label: "Picky", applies: () => true, render: picky("action") }],
      }}
    >
      <Row />
    </With>,
  );
  expect(screen.queryByRole("group", { name: "Picky" })).not.toBeInTheDocument();
  await userEvent.setup().click(screen.getByRole("button", { name: "next" }));
  expect(await screen.findByText("renderer ACME-002")).toBeInTheDocument();
  expect(screen.getByRole("group", { name: "Picky" })).toHaveTextContent("action ACME-002");
});

it("a detail slot retries when the object changes under loaded data", async () => {
  quiet();
  stubApi({ "/api/v1/meta": metaGolden.body });
  const picky = (c: DetailContext) => {
    if (c.ref.name === "edge") throw new Error("boom");
    return <p>tab for {c.ref.name}</p>;
  };
  // The frame keeps its data while the object changes, so only the reset can bring the slot back.
  function Frame() {
    const [name, setName] = useState("edge");
    return (
      <>
        <DetailFrame
          installId={ID}
          refValue={{ group: GW, kind: "Gateway", namespace: "infra", name, link: "detail" }}
          noun="Gateway"
          data={{ health: "healthy", collectedAt: now() }}
        >
          <p>overview</p>
        </DetailFrame>
        <button type="button" onClick={() => setName("other")}>
          next
        </button>
      </>
    );
  }
  renderWithTheme(
    <With contributions={{ detailTabs: { [`${GW}/Gateway`]: [{ id: "picky", label: "Picky", render: picky }] } }}>
      <Frame />
    </With>,
    { path: `${GW_PATH}?tab=picky` },
  );
  expect(screen.getByRole("tabpanel")).toBeEmptyDOMElement();
  await userEvent.setup().click(screen.getByRole("button", { name: "next" }));
  expect(await screen.findByText("tab for other")).toBeInTheDocument();
});
