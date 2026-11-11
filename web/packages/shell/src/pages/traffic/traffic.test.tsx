import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes, useLocation } from "react-router-dom";
import pathsGolden from "../../../../../../pkg/api/testdata/golden/paths.json";
import restrictedGolden from "../../../../../../pkg/api/testdata/golden/paths-restricted.json";
import type { PathRow, PathsResponse } from "../../api/types";
import { renderWithTheme } from "../../test/render";
import { matchSummary, shareText } from "./pathText";
import { TrafficPage } from "./TrafficPage";

const ID = "kgw.kgateway";
const fresh = (b: PathsResponse): PathsResponse => ({ ...b, collectedAt: new Date().toISOString() });
const golden = () => fresh(pathsGolden.body as unknown as PathsResponse);

/** Stubs fetch for …/paths with body(url); anything else is a 404. */
function serve(body: (u: URL) => unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const u = new URL(url, "http://ui");
      if (u.pathname !== `/api/v1/installs/${ID}/paths`) return new Response("{}", { status: 404 });
      return new Response(JSON.stringify(body(u)), { status: 200 });
    }),
  );
}
const pathCalls = () =>
  vi
    .mocked(fetch)
    .mock.calls.map((c) => new URL(String(c[0]), "http://ui"))
    .filter((u) => u.pathname.endsWith("/paths"));

function Where() {
  const loc = useLocation();
  return <output data-testid="location">{loc.search}</output>;
}
const search = () => new URLSearchParams(screen.getByTestId("location").textContent ?? "");

function mount(query = "") {
  return renderWithTheme(
    <>
      <Routes>
        <Route path="/i/:id/traffic" element={<TrafficPage />} />
      </Routes>
      <Where />
    </>,
    { path: `/i/${ID}/traffic${query}` },
  );
}

const table = () => screen.getByRole("table", { name: "Paths" });

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("sends every filter, the sort and the cursor from the URL, and a reload restores them", async () => {
  serve(() => ({ ...golden(), next: "n2" }));
  const query =
    "?gateway=infra/edge,infra/other&listener=http&route=gateway.networking.k8s.io/HTTPRoute/shop/backends" +
    "&backend=core/Service/shop/cart-svc&namespace=shop&state=error,warning&kind=HTTPRoute&mirror=exclude&q=cart" +
    "&sort=backend.desc&cursor=c1&x=ignored";
  const first = mount(query);
  await within(table()).findAllByLabelText("shop/cart-svc");
  // Only documented params reach the API; gateway, backend and state as repeated keys.
  expect([...(pathCalls()[0]?.searchParams.entries() ?? [])]).toEqual([
    ["gateway", "infra/edge"],
    ["gateway", "infra/other"],
    ["listener", "http"],
    ["route", "gateway.networking.k8s.io/HTTPRoute/shop/backends"],
    ["backend", "core/Service/shop/cart-svc"],
    ["namespace", "shop"],
    ["state", "error"],
    ["state", "warning"],
    ["kind", "HTTPRoute"],
    ["mirror", "exclude"],
    ["q", "cart"],
    ["sort", "backend.desc"],
    ["cursor", "c1"],
  ]);
  // Every filter shows as a pressed chip or a tag.
  expect(screen.getByRole("button", { name: "infra/edge" })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByRole("button", { name: "shop" })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByRole("button", { name: /^Error$/ })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByRole("button", { name: "HTTPRoute" })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByRole("button", { name: "Hide mirrors" })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByText("Listener: http")).toBeInTheDocument();
  expect(screen.getByText("Route: HTTPRoute shop/backends")).toBeInTheDocument();
  expect(screen.getByText("Backend: Service shop/cart-svc")).toBeInTheDocument();
  expect(screen.getByRole("columnheader", { name: /Sends to/ })).toHaveAttribute("aria-sort", "descending");
  const saved = search().toString();
  first.unmount();
  vi.mocked(fetch).mockClear();
  mount(`?${saved}`);
  await within(table()).findAllByLabelText("shop/cart-svc");
  expect(pathCalls()[0]?.searchParams.toString()).toBe(
    new URLSearchParams([
      ["gateway", "infra/edge"],
      ["gateway", "infra/other"],
      ["listener", "http"],
      ["route", "gateway.networking.k8s.io/HTTPRoute/shop/backends"],
      ["backend", "core/Service/shop/cart-svc"],
      ["namespace", "shop"],
      ["state", "error"],
      ["state", "warning"],
      ["kind", "HTTPRoute"],
      ["mirror", "exclude"],
      ["q", "cart"],
      ["sort", "backend.desc"],
      ["cursor", "c1"],
    ]).toString(),
  );
});

it("a chip change drops the cursor; kind and mirror chips hold one value", async () => {
  serve(() => ({ ...golden(), next: "n2" }));
  mount("?cursor=c1&kind=GRPCRoute");
  await within(table()).findAllByLabelText("shop/cart-svc");
  await userEvent.click(screen.getByRole("button", { name: /^Error$/ }));
  await waitFor(() => expect(search().get("state")).toBe("error"));
  expect(search().has("cursor")).toBe(false);
  await waitFor(() => expect(pathCalls().at(-1)?.searchParams.has("cursor")).toBe(false));
  // Pressing another kind replaces the first; pressing it again releases it.
  await userEvent.click(screen.getByRole("button", { name: "TCPRoute" }));
  await waitFor(() => expect(search().get("kind")).toBe("TCPRoute"));
  await userEvent.click(screen.getByRole("button", { name: "TCPRoute" }));
  await waitFor(() => expect(search().has("kind")).toBe(false));
  await userEvent.click(screen.getByRole("button", { name: "Mirrors only" }));
  await waitFor(() => expect(search().get("mirror")).toBe("only"));
  // The Narrow to chips come from the shown rows: one click narrows to a Gateway.
  await userEvent.click(screen.getByRole("button", { name: "infra/other" }));
  await waitFor(() => expect(pathCalls().at(-1)?.searchParams.getAll("gateway")).toEqual(["infra/other"]));
  // The pager writes the server's cursor.
  await userEvent.click(screen.getByRole("button", { name: "Next page" }));
  await waitFor(() => expect(search().get("cursor")).toBe("n2"));
});

it("the summary counts the whole result per state and says when paths are hidden or endpoints not read", async () => {
  serve(() => {
    const b = golden();
    return { ...b, summary: { ...b.summary, endpointsNotRead: true, hidden: true } };
  });
  mount();
  const summary = await screen.findByRole("region", { name: "Path summary" });
  await within(summary).findByText("11 paths · 6 routes · 2 Gateways · 4 backends");
  expect(within(summary).getByText("Some paths are hidden from you.")).toBeInTheDocument();
  expect(within(summary).getByText(/Endpoints were not read/)).toBeInTheDocument();
  const warning = within(summary).getByTestId("summary-warning");
  expect(warning).toHaveTextContent("Warning3 paths");
  expect(warning).toHaveTextContent("1 route · 1 Gateway · 2 backends");
  // The page title counts the filtered paths.
  expect(screen.getByText("11 paths")).toBeInTheDocument();
});

it("a restricted viewer's summary says some paths are hidden", async () => {
  serve(() => fresh(restrictedGolden.body as unknown as PathsResponse));
  mount();
  const summary = await screen.findByRole("region", { name: "Path summary" });
  expect(await within(summary).findByText("Some paths are hidden from you.")).toBeInTheDocument();
  expect(within(summary).queryByText(/Endpoints were not read/)).toBeNull();
});

it("problem rows show their state as icon and text, with the signals behind it", async () => {
  serve(() => golden());
  mount();
  await within(table()).findAllByLabelText("shop/cart-svc");
  const states = within(table()).getAllByTestId("path-state");
  // Problems first, as the server sorted them: warning before unknown before healthy.
  expect(states.map((s) => s.textContent)).toEqual([
    "Warning",
    "Warning",
    "Warning",
    "Unknown",
    "Unknown",
    "Unknown",
    "Healthy",
    "Healthy",
    "Healthy",
    "Healthy",
    "Healthy",
  ]);
  // Never colour alone: each state has its icon next to its text.
  for (const s of states) expect(s.querySelector("svg")).not.toBeNull();
  // A problem's signals are reachable from the keyboard.
  expect(states[0]).toHaveAttribute("tabindex", "0");
  expect(states[6]).not.toHaveAttribute("tabindex");
  // The redirect rule shows its written action, not a backend, and no share.
  const redirect = within(table()).getByText("Redirect").closest("tr") as HTMLElement;
  expect(redirect.querySelector('td[data-col="share"]')).toHaveTextContent(/^$/);
  // The Why column says what the finding is; its code is in the tooltip.
  expect(within(table()).getAllByText(/^Proxy deployment not found \(Gateway infra\/other\)/).length).toBeGreaterThan(
    0,
  );
  expect(within(table()).getAllByText("1/1 ready").length).toBeGreaterThan(0);
  expect(within(table()).getByText("25 %")).toBeInTheDocument();
});

it("an install without computed views explains that no paths are computed, not that there is no traffic", async () => {
  serve(() => {
    const b = golden();
    return { ...b, rows: [], summary: { ...b.summary, paths: 0, notComputed: "no-attachments" } };
  });
  mount();
  expect(await screen.findByText("Paths not available")).toBeInTheDocument();
  expect(screen.getByText(/That does not mean it has no traffic/)).toBeInTheDocument();
  expect(screen.queryByRole("table", { name: "Paths" })).toBeNull();
  expect(screen.queryByTestId("narrow-to")).toBeNull();
});

describe("cell text", () => {
  const row = (over: Partial<PathRow>): PathRow => ({ ...(golden().rows[0] as PathRow), ...over });
  it("an unknown delegated match never shows the child's rule", () => {
    expect(matchSummary(row({ delegated: true, matches: null })).text).toBe("Match unknown");
  });
  it("TCP rows match all connections and fail as connections", () => {
    const tcp = row({
      routes: [{ group: "gateway.networking.k8s.io", kind: "TCPRoute", namespace: "a", name: "t", link: "none" }],
      matches: [],
      shareState: "failing",
      share: 50,
    });
    expect(matchSummary(tcp).text).toBe("All connections");
    expect(shareText(tcp).flag).toEqual({ state: "error", text: "connections fail" });
    expect(shareText({ ...tcp, routes: row({}).routes }).flag?.text).toBe("requests fail");
  });
  it("shares follow their state", () => {
    expect(shareText(row({ shareState: "no-traffic", share: 0 }))).toEqual({
      text: "0 %",
      flag: { state: "unknown", text: "no traffic" },
    });
    expect(shareText(row({ shareState: "may-fail", share: 10 })).flag?.text).toBe("may fail");
    expect(shareText(row({ shareState: "not-shown", share: undefined })).text).toBe("Not shown");
    expect(shareText(row({ shareState: "mirror", mirrorPercent: 50 })).text).toBe("Mirror 50 %");
  });
  it("an empty match list matches all requests", () => {
    expect(matchSummary(row({ matches: [] })).text).toBe("All requests");
  });
});

it("stale data turns every state unknown: rows, share flags, the Why column and the summary tiles", async () => {
  serve(() => {
    const b = golden();
    const rows = b.rows.map((r, i) => (i === 0 ? { ...r, shareState: "failing" as const, share: 25 } : r));
    return { ...b, rows, collectedAt: new Date(Date.now() - 10 * 60_000).toISOString() };
  });
  mount();
  await within(table()).findAllByLabelText("shop/cart-svc");
  const states = within(table()).getAllByTestId("path-state");
  expect(new Set(states.map((s) => s.textContent))).toEqual(new Set(["Unknown"]));
  expect(within(table()).queryByText("requests fail")).toBeNull();
  for (const cell of document.querySelectorAll('td[data-col="why"]')) expect(cell).toHaveTextContent(/^$/);
  const summary = screen.getByRole("region", { name: "Path summary" });
  expect(within(summary).getByTestId("summary-warning")).not.toHaveAttribute("data-on");
});

it("fresh data shows the share flag and lights the problem tiles", async () => {
  serve(() => {
    const b = golden();
    return { ...b, rows: b.rows.map((r, i) => (i === 0 ? { ...r, shareState: "failing" as const, share: 25 } : r)) };
  });
  mount();
  await within(table()).findAllByLabelText("shop/cart-svc");
  expect(within(table()).getByText("requests fail")).toBeInTheDocument();
  expect(screen.getByTestId("summary-warning")).toHaveAttribute("data-on", "true");
  expect(screen.getByTestId("summary-healthy")).not.toHaveAttribute("data-on");
});

it("Narrow to chips come from the server's facets, not the shown rows", async () => {
  serve(() => {
    const b = golden();
    const zzz = { ...(b.summary.facets?.gateways[0] as NonNullable<typeof b.summary.facets>["gateways"][number]) };
    zzz.ref = { ...zzz.ref, name: "zzz" };
    return {
      ...b,
      summary: {
        ...b.summary,
        facets: { gateways: [...(b.summary.facets?.gateways ?? []), zzz], namespaces: [{ name: "far", paths: 1 }] },
      },
    };
  });
  mount("?gateway=infra/other");
  await within(table()).findAllByLabelText("shop/cart-svc");
  const gws = screen.getByRole("group", { name: "Gateways" });
  // Selected first, then the server's name order; zzz has no row on this page.
  expect(
    within(gws)
      .getAllByRole("button")
      .map((b) => b.textContent),
  ).toEqual(["infra/other", "infra/edge", "infra/zzz"]);
  await userEvent.click(within(gws).getByRole("button", { name: "infra/zzz" }));
  await waitFor(() => expect(search().get("gateway")).toBe("infra/other,infra/zzz"));
  expect(
    within(screen.getByRole("group", { name: "Route namespaces" })).getByRole("button", { name: "far" }),
  ).toBeInTheDocument();
});

it("an empty result hides the Narrow to line and keeps its filters as removable tags", async () => {
  serve(() => {
    const b = golden();
    const none = { paths: 0, routes: 0, gateways: 0, backends: 0 };
    return {
      ...b,
      rows: [],
      summary: {
        ...none,
        byState: { error: none, warning: none, unknown: none, healthy: none },
        facets: { gateways: [], namespaces: [] },
      },
    };
  });
  mount("?gateway=infra/gone&namespace=x");
  expect(await screen.findByText("No paths match these filters.")).toBeInTheDocument();
  expect(screen.queryByTestId("narrow-to")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "Remove filter Gateway: infra/gone" }));
  await waitFor(() => expect(search().has("gateway")).toBe(false));
  expect(search().get("namespace")).toBe("x");
});
