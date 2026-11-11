import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { Route, Routes, useLocation } from "react-router-dom";
import backendGolden from "../../../../../../pkg/api/testdata/golden/backend-detail-backend.json";
import explainedGolden from "../../../../../../pkg/api/testdata/golden/backend-detail-explained.json";
import serviceGolden from "../../../../../../pkg/api/testdata/golden/backend-detail-service.json";
import backendsGolden from "../../../../../../pkg/api/testdata/golden/backends.json";
import gatewaysGolden from "../../../../../../pkg/api/testdata/golden/gateways.json";
import type { BackendDetail, BackendsResponse, Endpoints, PathSignal } from "../../api/types";
import { kgatewayWebPack } from "../../kgateway/webPack";
import { ShellExtensionContext, useShellExtension } from "../../shell/extension";
import { renderWithTheme } from "../../test/render";
import { BackendPage } from "./BackendPage";
import { BackendsPage } from "./BackendsPage";

const ID = "kgw.kgateway";
const API = `/api/v1/installs/${ID}`;
const now = () => new Date().toISOString();
const list = (): BackendsResponse => ({ ...(backendsGolden.body as unknown as BackendsResponse), collectedAt: now() });
const detail = (g: { body: unknown }): BackendDetail => {
  const d = structuredClone(g.body) as BackendDetail;
  return { ...d, collectedAt: now(), exposure: { ...d.exposure, collectedAt: now() } };
};

/** Stubs fetch by pathname; anything else is a 404. */
function serve(routes: Record<string, (u: URL) => unknown>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const u = new URL(url, "http://ui");
      const f = routes[u.pathname];
      return f ? new Response(JSON.stringify(f(u)), { status: 200 }) : new Response("{}", { status: 404 });
    }),
  );
}
const calls = (suffix: string) =>
  vi
    .mocked(fetch)
    .mock.calls.map((c) => new URL(String(c[0]), "http://ui"))
    .filter((u) => u.pathname.endsWith(suffix));

/** The kgateway pack's vocabulary, as the app merges it, so the Backend resources chip shows. */
function WithPack({ children }: { children: ReactNode }) {
  const base = useShellExtension();
  return (
    <ShellExtensionContext.Provider
      value={{ ...base, vocabulary: { ...base.vocabulary, ...kgatewayWebPack.vocabulary } }}
    >
      {children}
    </ShellExtensionContext.Provider>
  );
}

function Where() {
  const loc = useLocation();
  return <output data-testid="location">{`${loc.pathname}${loc.search}`}</output>;
}
const where = () => new URL(screen.getByTestId("location").textContent ?? "", "http://ui");

function mount(path: string) {
  return renderWithTheme(
    <WithPack>
      <Routes>
        <Route path="/i/:id/backends" element={<BackendsPage />} />
        <Route path="/i/:id/backends/:group/:kind/:ns/:name" element={<BackendPage />} />
        <Route path="*" element={null} />
      </Routes>
      <Where />
    </WithPack>,
    { path },
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// region List
it("the Problems, Services, Backend resources, Unused, Namespace and Gateway chips map to the list params", async () => {
  serve({
    [`${API}/backends`]: () => list(),
    [`${API}/gateways`]: () => ({ ...gatewaysGolden.body, collectedAt: now() }),
  });
  mount(`/i/${ID}/backends`);
  const table = await screen.findByRole("table", { name: "Backends" });
  await within(table).findAllByLabelText("shop/cart-svc");
  const last = () => calls("/backends").at(-1)?.searchParams;
  const chip = (name: string) => screen.getByRole("button", { name });
  await userEvent.click(chip("Problems"));
  await waitFor(() => expect(last()?.getAll("state")).toEqual(["error", "warning", "unknown"]));
  await userEvent.click(chip("Services"));
  await waitFor(() => expect(last()?.get("kind")).toBe("Service"));
  // Kind holds one value: Backend resources replaces Services.
  await userEvent.click(chip("Backend resources"));
  await waitFor(() => expect(last()?.getAll("kind")).toEqual(["Backend"]));
  expect(chip("Services")).toHaveAttribute("aria-pressed", "false");
  await userEvent.click(chip("Unused"));
  await waitFor(() => expect(last()?.get("used")).toBe("no"));
  // The namespace chips come from the rows, the Gateway chips from the install's Gateways.
  await userEvent.click(chip("shop"));
  await waitFor(() => expect(last()?.get("namespace")).toBe("shop"));
  await userEvent.click(await screen.findByRole("button", { name: "infra/edge" }));
  await waitFor(() => expect(last()?.getAll("gateway")).toEqual(["infra/edge"]));
  // Releasing a chip drops its param; the URL holds every filter for a reload.
  await userEvent.click(chip("Problems"));
  await waitFor(() => expect(last()?.has("state")).toBe(false));
  expect(Object.fromEntries(where().searchParams)).toEqual({
    kind: "Backend",
    used: "no",
    namespace: "shop",
    gateway: "infra/edge",
  });
});

it("rows show kind, endpoints per port, users and the problem; a row opens backend detail", async () => {
  serve({
    [`${API}/backends`]: () => list(),
    [`${API}/gateways`]: () => ({ ...gatewaysGolden.body, collectedAt: now() }),
  });
  mount(`/i/${ID}/backends`);
  const table = await screen.findByRole("table", { name: "Backends" });
  const row = (name: string) =>
    within(table)
      .getAllByRole("row")
      .find((r) => within(r).queryByLabelText(name)) as HTMLElement;
  await within(table).findAllByLabelText("shop/cart-svc");
  expect(row("shop/cart-svc")).toHaveTextContent("80: 1/1 ready");
  expect(row("shop/be1")).toHaveTextContent("Backend · Static");
  expect(row("shop/be1")).toHaveTextContent("Not applicable");
  expect(row("shop/be-unused")).toHaveTextContent("Not used");
  // Routes write it, but no accepted attachment reaches it.
  expect(row("shop/lonely-svc")).toHaveTextContent("0 confirmed exposures");
  expect(row("shop/authz")).toHaveTextContent("2 others");
  expect(within(row("shop/lonely-svc")).getByTestId("path-state")).toHaveTextContent("Error");
  const link = within(row("shop/cart-svc")).getByRole("link", { name: /cart-svc/ });
  expect(link.getAttribute("href")?.split("?")[0]).toBe(`/i/${ID}/backends/-/Service/shop/cart-svc`);
});

// region Detail
function serveDetail(d: BackendDetail, path = `${API}/backends/-/Service/shop/named-svc`) {
  serve({ [path]: () => d });
}
const statusLine = () => screen.findByTestId("status-line");

it("the status line names routes and Gateways and the impact in fixed phrases, never a status code", async () => {
  const d = detail(serviceGolden);
  d.problem = { state: "error", signals: [] };
  d.status = [{ kind: "finding", code: "KGWUI-023", state: "error" } as PathSignal];
  d.exposure.summary = { ...d.exposure.summary, routes: 2, gateways: 2 };
  serveDetail(d);
  mount(`/i/${ID}/backends/-/Service/shop/named-svc`);
  const line = await statusLine();
  expect(line).toHaveTextContent("2 routes on 2 Gateways send traffic here; no endpoint is ready, so requests fail.");
  expect(line).toHaveTextContent("Error");
  expect(line.textContent).not.toMatch(/\b[45]\d\d\b/);
});

it("the status line has a phrase per signal: waiting, not read, not accepted, unused", async () => {
  const d = detail(serviceGolden);
  d.status = [
    { kind: "waiting", code: "missing", state: "unknown" },
    { kind: "coverage", code: "off", state: "unknown" },
  ] as PathSignal[];
  d.exposure.summary = { ...d.exposure.summary, routes: 1, gateways: 1 };
  d.problem = { state: "unknown", signals: [] };
  d.service = undefined;
  d.consumers = [
    {
      consumer: {
        group: "gateway.kgateway.dev",
        kind: "GatewayExtension",
        namespace: "shop",
        name: "authz",
        link: "yaml",
      },
      role: "ext-auth",
      field: "spec.extAuth.grpcService.backendRef",
    },
  ];
  serveDetail(d);
  mount(`/i/${ID}/backends/-/Service/shop/named-svc`);
  expect(await statusLine()).toHaveTextContent(
    "1 route on 1 Gateway sends traffic here and 1 other object uses it; it is not seen in the cluster yet; its endpoints were not read.",
  );
  // The consumer is listed under the paths with its role and field.
  expect(screen.getByTestId("consumer")).toHaveTextContent(
    "Used by GatewayExtension shop/authz (ext-auth) Field spec.extAuth.grpcService.backendRef",
  );
});

it("a missing port of a cached Service and a worse state from its paths each get their phrase", async () => {
  const d = detail(serviceGolden);
  d.status = [{ kind: "waiting", code: "missing", state: "unknown" } as PathSignal];
  d.problem = { state: "error", signals: [] };
  serveDetail(d);
  mount(`/i/${ID}/backends/-/Service/shop/named-svc`);
  expect(await statusLine()).toHaveTextContent(
    "1 route on 1 Gateway sends traffic here; a referenced port is not seen on the Service yet; some paths to it have a problem.",
  );
});

it("a Backend's status line says it is not accepted, and its page keeps conditions and the YAML tab", async () => {
  const d = detail(backendGolden);
  d.status = [{ kind: "finding", code: "KGWUI-002", state: "error" } as PathSignal];
  d.problem = { state: "error", signals: [] };
  d.exposure.summary = { ...d.exposure.summary, routes: 0, gateways: 0 };
  d.consumers = [];
  serveDetail(d, `${API}/backends/gateway.kgateway.dev/Backend/shop/be-used`);
  mount(`/i/${ID}/backends/gateway.kgateway.dev/Backend/shop/be-used`);
  expect(await statusLine()).toHaveTextContent(
    "Nothing uses this backend; the controller does not accept it, so requests fail.",
  );
  expect(screen.getByRole("tab", { name: "YAML" })).toBeInTheDocument();
  expect(screen.getByText("Backend type")).toBeInTheDocument();
  expect(screen.getByTestId("condition-Accepted")).toBeInTheDocument();
  // A Backend reads nothing: not applicable, with no read time.
  expect(screen.getByTestId("endpoints-summary")).toHaveTextContent("Not applicable");
  expect(screen.queryByText(/^Read /)).toBeNull();
});

it("a Service has no YAML tab; its facts, port chain and per-proxy slot show", async () => {
  serveDetail(detail(serviceGolden));
  mount(`/i/${ID}/backends/-/Service/shop/named-svc`);
  await statusLine();
  expect(screen.getByRole("tab", { name: "Overview" })).toBeInTheDocument();
  expect(screen.queryByRole("tab", { name: "YAML" })).toBeNull();
  expect(screen.getByText("Service facts")).toBeInTheDocument();
  expect(screen.getByText("ClusterIP")).toBeInTheDocument();
  const chain = screen.getByTestId("port-chain");
  expect(chain).toHaveTextContent("Route port80");
  expect(chain).toHaveTextContent("Service port80 (web)");
  expect(chain).toHaveTextContent("Target porthttp-web (named, not resolved)");
  expect(chain).toHaveTextContent("Slice ports8080");
  expect(screen.getByTestId("per-proxy-slot")).toHaveTextContent(
    "Endpoints as each proxy sees them are not shown here.",
  );
});

it("an unavailable source shows the cached counts only, never a table or a zero", async () => {
  serveDetail(detail(serviceGolden));
  mount(`/i/${ID}/backends/-/Service/shop/named-svc`);
  await statusLine();
  const src = screen.getByTestId("endpoints-source");
  expect(within(src).getByTestId("endpoints-summary")).toHaveTextContent(/^1\/2 ready, cached$/);
  expect(within(src).queryByRole("table")).toBeNull();
});

it.each([
  ["partial", "Partly read"],
  ["timed-out", "Timed out"],
  ["busy", "Busy, retry"],
  ["failed", "Read failed"],
] as const)("a %s read says %s and never 0 endpoints", async (complete, text) => {
  const d = detail(serviceGolden);
  d.sources = [{ ...(d.sources[0] as Endpoints), complete, ready: undefined, total: undefined }];
  serveDetail(d);
  mount(`/i/${ID}/backends/-/Service/shop/named-svc`);
  await statusLine();
  const summary = screen.getByTestId("endpoints-summary");
  expect(summary).toHaveTextContent(text);
  expect(summary.textContent).not.toMatch(/\b0\b/);
  const retry = screen.queryByRole("button", { name: "Retry" });
  if (complete === "busy" || complete === "timed-out") {
    const before = calls("/named-svc").length;
    await userEvent.click(retry as HTMLElement);
    await waitFor(() => expect(calls("/named-svc").length).toBeGreaterThan(before));
  } else expect(retry).toBeNull();
});

it("a complete read lists each endpoint with ready, serving, terminating and its Pod", async () => {
  const d = detail(serviceGolden);
  d.sources = [
    {
      ...(d.sources[0] as Endpoints),
      complete: "complete",
      ready: 1,
      total: 2,
      items: [
        { address: "10.0.0.1", port: 8080, ready: true, serving: true, terminating: false, pod: "web-1" },
        { address: "10.0.0.2", port: 8080, ready: false, serving: false, terminating: true, pod: "web-2" },
      ],
    },
  ];
  serveDetail(d);
  mount(`/i/${ID}/backends/-/Service/shop/named-svc`);
  await statusLine();
  expect(screen.getByTestId("endpoints-summary")).toHaveTextContent(/^1\/2 ready$/);
  const rows = within(screen.getByRole("table", { name: "Endpoints on Port 80 (web)" })).getAllByRole("row");
  expect(rows[1]).toHaveTextContent("10.0.0.1:8080YesYesNoweb-1");
  expect(rows[2]).toHaveTextContent("10.0.0.2:8080NoNoYesweb-2");
});

it("the cross-namespace ref shows its explanation, reason and the suggested ReferenceGrant", async () => {
  serveDetail(detail(explainedGolden), `${API}/backends/-/Service/store/cart`);
  mount(`/i/${ID}/backends/-/Service/store/cart`);
  await statusLine();
  const e = screen.getByTestId("explanation");
  expect(e).toHaveTextContent("The controller does not permit this ref to another namespace.");
  expect(e).toHaveTextContent("RefNotPermitted");
  expect(e).toHaveTextContent("spec.rules[0].backendRefs[0]");
  expect(within(e).getByRole("heading", { name: "Suggested fix" })).toBeInTheDocument();
  expect(e).toHaveTextContent("kind: ReferenceGrant");
});

it("a candidate quotes the route's reason without guessing; waiting never says failed", async () => {
  const d = detail(explainedGolden);
  const base = d.explanations[0] as BackendDetail["explanations"][number];
  d.explanations = [
    { ...base, kind: "candidate", explanation: undefined, fix: undefined },
    { ...base, kind: "waiting", path: "spec.rules[1].backendRefs[0]", rule: 1, reason: undefined, fix: undefined },
  ];
  serveDetail(d, `${API}/backends/-/Service/store/cart`);
  mount(`/i/${ID}/backends/-/Service/store/cart`);
  await statusLine();
  const [candidate, waiting] = screen.getAllByTestId("explanation");
  expect(candidate).toHaveTextContent("This route reports ResolvedRefs=False (RefNotPermitted) for one of these refs.");
  expect(candidate).not.toHaveTextContent("Suggested fix");
  expect(waiting).toHaveTextContent("Waiting for the controller.");
  expect(waiting).not.toHaveTextContent(/fail/i);
});

it("How this backend is exposed lists its paths and links to Traffic with backend=", async () => {
  serveDetail(detail(explainedGolden), `${API}/backends/-/Service/store/cart`);
  mount(`/i/${ID}/backends/-/Service/store/cart`);
  await statusLine();
  const open = screen.getByRole("link", { name: "Open in Traffic" });
  const u = new URL(open.getAttribute("href") ?? "", "http://ui");
  expect(u.pathname).toBe(`/i/${ID}/traffic`);
  expect(u.searchParams.get("backend")).toBe("core/Service/store/cart");
  const table = screen.getByRole("table", { name: "Paths to this backend" });
  expect(within(table).getAllByTestId("path-state")[0]).toHaveTextContent("Error");
  // The backend's own column and endpoints repeat the page, so they are left out; no header sorts.
  expect(within(table).queryByRole("columnheader", { name: /Sends to/ })).toBeNull();
  expect(within(table).queryAllByRole("button")).toHaveLength(0);
});

it("a stale detail shows every state unknown and the status line claims no impact", async () => {
  const d = detail(serviceGolden);
  d.collectedAt = new Date(Date.now() - 60_000).toISOString();
  d.status = [{ kind: "finding", code: "KGWUI-023", state: "error" } as PathSignal];
  d.problem = { state: "error", signals: [] };
  serveDetail(d);
  mount(`/i/${ID}/backends/-/Service/shop/named-svc`);
  const line = await statusLine();
  expect(line).toHaveAttribute("data-state", "unknown");
  expect(line).toHaveTextContent("its state is unknown while the data is out of date");
  expect(line).not.toHaveTextContent("requests fail");
});
