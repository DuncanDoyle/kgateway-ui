import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes, useLocation } from "react-router-dom";
import detailGolden from "../../../../../../pkg/api/testdata/golden/finding-detail-other.json";
import detailRestrictedGolden from "../../../../../../pkg/api/testdata/golden/finding-detail-restricted.json";
import findingsGolden from "../../../../../../pkg/api/testdata/golden/findings.json";
import findingsRestrictedGolden from "../../../../../../pkg/api/testdata/golden/findings-restricted.json";
import meta from "../../../../../../pkg/api/testdata/golden/meta.json";
import type { FindingDetail, FindingList, LinkedRef } from "../../api/types";
import { diagnosticsCopy, findingsCopy } from "../../copy/explorer";
import { ROW_H } from "../../explore/ObjectTable";
import { pseudoRules, renderWithTheme } from "../../test/render";
import { FindingRow } from "../install/FindingRow";
import { ago } from "../install/HealthStrip";
import { FindingPage } from "./FindingPage";
import { FindingsPage } from "./FindingsPage";

const ID = "kgw.kgateway";
const GW = "gateway.networking.k8s.io";
const now = () => new Date().toISOString();
const findings = (): FindingList => ({ ...(findingsGolden.body as FindingList), collectedAt: now() });
const restricted = (): FindingList => ({ ...(findingsRestrictedGolden.body as FindingList), collectedAt: now() });
const detail = (): FindingDetail => ({ ...(detailGolden.body as FindingDetail), collectedAt: now() });
const FINDING = detailGolden.body.finding.id;

type Reply = unknown | [number, unknown];
/** Stubs fetch with a handler over the parsed request URL; undefined is a 404. */
function serve(fn: (u: URL) => Reply | undefined) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const u = new URL(url, "http://ui");
      const r = u.pathname === "/api/v1/meta" ? meta.body : fn(u);
      if (r === undefined) return new Response(JSON.stringify({ error: "not found" }), { status: 404 });
      const [status, body] = Array.isArray(r) && typeof r[0] === "number" ? (r as [number, unknown]) : [200, r];
      return new Response(JSON.stringify(body), { status });
    }),
  );
}
const calls = () => vi.mocked(fetch).mock.calls.map((c) => new URL(String(c[0]), "http://ui"));
const listCalls = () => calls().filter((u) => u.pathname === `/api/v1/installs/${ID}/findings`);

function Where() {
  const loc = useLocation();
  return <output data-testid="location">{loc.pathname + loc.search}</output>;
}
const location = () => screen.getByTestId("location").textContent ?? "";
const search = () => new URLSearchParams(location().split("?")[1] ?? "");

function mount(path: string) {
  return renderWithTheme(
    <>
      <Routes>
        <Route path="/i/:id/findings" element={<FindingsPage />} />
        <Route path="/i/:id/findings/:findingId" element={<FindingPage />} />
        <Route path="*" element={null} />
      </Routes>
      <Where />
    </>,
    { path },
  );
}
const rowOf = (text: string) => screen.getAllByRole("row").find((r) => within(r).queryByText(text)) as HTMLElement;

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("filters, sort and cursor round-trip through the URL", async () => {
  serve((u) => (u.pathname.endsWith("/findings") ? { ...findings(), next: "n2", prev: "p0" } : undefined));
  const kind = `${GW}/HTTPRoute`;
  mount(
    `/i/${ID}/findings?severity=error&code=KGWUI-002&kind=${kind}&namespace=shop&sort=since.desc&cursor=c1&x=ignored`,
  );
  await screen.findByText("Backend not found");
  // Only the documented params reach the API, repeatable ones as repeated keys.
  expect([...(listCalls()[0]?.searchParams.entries() ?? [])]).toEqual([
    ["severity", "error"],
    ["code", "KGWUI-002"],
    ["kind", kind],
    ["namespace", "shop"],
    ["sort", "since.desc"],
    ["cursor", "c1"],
  ]);
  expect(screen.getByRole("columnheader", { name: /Since/ })).toHaveAttribute("aria-sort", "descending");
  expect(screen.getByRole("button", { name: /^Error/ })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByText("Code: KGWUI-002")).toBeInTheDocument();
  expect(screen.getByText("Kind: HTTPRoute")).toBeInTheDocument();
  // Sorting starts again at page one; rank is the default and has one direction, so it clears the sort.
  await userEvent.click(screen.getByRole("button", { name: /^Code/ }));
  await waitFor(() => expect(search().get("sort")).toBe("code.asc"));
  expect(search().has("cursor")).toBe(false);
  await userEvent.click(screen.getByRole("button", { name: /^Severity/ }));
  await waitFor(() => expect(search().has("sort")).toBe(false));
  expect(screen.getByRole("columnheader", { name: /Severity/ })).toHaveAttribute("aria-sort", "ascending");
  // The pager writes the server's cursor.
  await userEvent.click(screen.getByRole("button", { name: "Next page" }));
  await waitFor(() => expect(search().get("cursor")).toBe("n2"));
  // A filter change drops the cursor; severity is repeatable.
  await userEvent.click(screen.getByRole("button", { name: /^Warning/ }));
  await waitFor(() => expect(search().get("severity")).toBe("error,warning"));
  expect(search().has("cursor")).toBe(false);
  await waitFor(() => expect(listCalls().at(-1)?.searchParams.getAll("severity")).toEqual(["error", "warning"]));
  await userEvent.click(screen.getByRole("button", { name: "Remove filter Code: KGWUI-002" }));
  await waitFor(() => expect(search().has("code")).toBe(false));
  // Free text reaches the server once typing stops, and starts again at page one.
  await userEvent.type(screen.getByRole("searchbox", { name: "Filter findings" }), "shop/");
  await waitFor(() => expect(listCalls().at(-1)?.searchParams.get("q")).toBe("shop/"));
  expect(search().get("q")).toBe("shop/");
  // A finding opens its own page, carrying the list and its filters as the start of the trail (D-035).
  await userEvent.click(screen.getByRole("link", { name: "Backend not found" }));
  await waitFor(() => expect(location().split("?")[0]).toBe(`/i/${ID}/findings/${findingsGolden.body.items[0]?.id}`));
  const via = new URLSearchParams(location().split("?")[1]).get("via") ?? "";
  expect(via.startsWith(`${ID}/findings?`)).toBe(true);
  expect(new URLSearchParams(via.split("?")[1]).get("q")).toBe("shop/");
});

it("a 400 on a cursor drops it and loads page one", async () => {
  serve((u) => {
    if (!u.pathname.endsWith("/findings")) return undefined;
    return u.searchParams.has("cursor") ? [400, { error: "bad cursor" }] : findings();
  });
  mount(`/i/${ID}/findings?severity=error&cursor=stale`);
  await screen.findByText("Backend not found");
  await waitFor(() => expect(search().has("cursor")).toBe(false));
  expect(search().get("severity")).toBe("error");
  expect(screen.queryByTestId("page-notice")).toBeNull();
});

it("restricted rows show code, severity and the restricted label", async () => {
  serve((u) => (u.pathname.endsWith("/findings") ? restricted() : undefined));
  mount(`/i/${ID}/findings`);
  await screen.findByText("Restricted: ask the owning team");
  const row = rowOf("Restricted: ask the owning team");
  expect(row).toHaveTextContent("KGWUI-002");
  expect(row).toHaveTextContent("Error");
  expect(row).toHaveTextContent("shop/broken");
  // Neither the hidden object nor the finding opens: there is nothing more this caller may see.
  expect(within(row).queryByRole("link")).toBeNull();
  expect(row).not.toHaveAttribute("data-link");
  // The visible finding on the same page links as usual.
  expect(screen.getByRole("link", { name: "Reference not permitted (missing ReferenceGrant)" })).toBeInTheDocument();
});

it("finding detail pages the affected objects", async () => {
  const obj = (name: string): LinkedRef => ({ group: GW, kind: "HTTPRoute", namespace: "shop", name, link: "detail" });
  serve((u) => {
    if (!u.pathname.endsWith(`/findings/${FINDING}`)) return undefined;
    const d = detail();
    const second = u.searchParams.get("affectedCursor") === "a2";
    d.affected = {
      ...d.affected,
      items: second ? [obj("route-51")] : [obj("route-1"), obj("route-2")],
      total: 51,
      ...(second ? { prev: "a1" } : { next: "a2" }),
    };
    return d;
  });
  mount(`/i/${ID}/findings/${FINDING}`);
  expect(await screen.findByRole("heading", { level: 1 })).toHaveTextContent(
    "Reference not permitted (missing ReferenceGrant)",
  );
  expect(screen.getByText("KGWUI-002")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "shop/denied" })).toHaveAttribute(
    "href",
    `/i/${ID}/routes/${GW}/HTTPRoute/shop/denied`,
  );
  const table = await screen.findByRole("table", { name: "Affected objects" });
  await within(table).findByRole("link", { name: "shop/route-1" });
  await userEvent.click(screen.getByRole("button", { name: "Next page" }));
  await within(table).findByRole("link", { name: "shop/route-51" });
  expect(calls().at(-1)?.searchParams.get("affectedCursor")).toBe("a2");
  // The affected cursor is local: the page URL stays short.
  expect(location()).toBe(`/i/${ID}/findings/${FINDING}`);
  // A link back to the list.
  expect(screen.getByRole("link", { name: findingsCopy.allFindings })).toHaveAttribute("href", `/i/${ID}/findings`);
});

it("a restricted finding shows its identity only, and an unknown one says so", async () => {
  serve((u) => {
    if (u.pathname.endsWith(`/findings/${detailRestrictedGolden.body.finding.id}`))
      return { ...detailRestrictedGolden.body, collectedAt: now() };
    return undefined;
  });
  const { unmount } = mount(`/i/${ID}/findings/${detailRestrictedGolden.body.finding.id}`);
  expect(await screen.findByRole("heading", { level: 1 })).toHaveTextContent("Restricted: ask the owning team");
  expect(screen.getByText("KGWUI-002")).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: /broken/ })).toBeNull();
  expect(screen.queryByRole("table", { name: "Affected objects" })).toBeNull();
  unmount();
  mount(`/i/${ID}/findings/f1-unknownunknownunknownunkn`);
  expect(await screen.findByText("Finding not found")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: findingsCopy.allFindings })).toHaveAttribute("href", `/i/${ID}/findings`);
});

it("a finding that goes away while open reads as not found, not as a load failure", async () => {
  let gone = false;
  serve((u) => (u.pathname.endsWith(`/findings/${FINDING}`) && !gone ? detail() : undefined));
  vi.useFakeTimers({ shouldAdvanceTime: true });
  mount(`/i/${ID}/findings/${FINDING}`);
  await screen.findByRole("table", { name: "Affected objects" });
  gone = true;
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5100);
  });
  expect(await screen.findByText("Finding not found")).toBeInTheDocument();
  expect(screen.queryByTestId("page-notice")).toBeNull();
  expect(screen.getByRole("link", { name: findingsCopy.allFindings })).toHaveAttribute("href", `/i/${ID}/findings`);
});

it("stale data turns every state unknown", async () => {
  const old = "2020-01-01T00:00:00Z";
  serve((u) => {
    if (u.pathname.endsWith("/findings")) return { ...findings(), collectedAt: old };
    if (u.pathname.endsWith(`/findings/${FINDING}`)) return { ...detail(), collectedAt: old };
    return undefined;
  });
  const { unmount } = mount(`/i/${ID}/findings`);
  await screen.findByText("Backend not found");
  for (const name of ["Backend not found", "Namespace not watched by the controller"]) {
    expect(within(rowOf(name)).getByText("Unknown")).toBeInTheDocument();
    expect(within(rowOf(name)).queryByText(/^(Error|Warning|Info)$/)).toBeNull();
  }
  expect(screen.getByTestId("data-age")).toHaveTextContent("stale");
  unmount();
  mount(`/i/${ID}/findings/${FINDING}`);
  const header = await screen.findByTestId("detail-header");
  await waitFor(() => expect(within(header).getByText("Unknown")).toBeInTheDocument());
  expect(within(header).queryByText("Error")).toBeNull();
  expect(screen.getByTestId("data-age")).toHaveTextContent("stale");
});

it("skeleton rows have the final row height", async () => {
  let release: (() => void) | undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (String(url).includes("/findings")) await gate;
      const body = String(url).includes("/meta") ? meta.body : findings();
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  mount(`/i/${ID}/findings`);
  const skeleton = screen.getAllByTestId("skeleton-row");
  expect(skeleton.length).toBeGreaterThan(0);
  expect(screen.getByRole("table", { name: "Findings" })).toHaveAttribute("aria-busy", "true");
  const h = getComputedStyle(skeleton[0] as HTMLElement).height;
  expect(h).toBe(`${ROW_H}px`);
  await act(async () => release?.());
  await screen.findByText("Backend not found");
  expect(getComputedStyle(rowOf("Backend not found")).height).toBe(h);
  // Rows and their finding links keep hover, active and focus-visible styles in both themes.
  const link = screen.getByRole("link", { name: "Backend not found" });
  for (const pseudo of [":hover", ":active", ":focus-visible"]) expect(pseudoRules(link, pseudo)).not.toEqual([]);
});

it("a finding row elsewhere opens the finding's page when it has an id, never when restricted", async () => {
  serve(() => undefined);
  const [hidden, shown] = findingsRestrictedGolden.body.items.map((f) => ({
    ...f,
    install: ID,
    message: f.message ?? "",
  }));
  renderWithTheme(
    <>
      <FindingRow finding={shown as never} installId={ID} />
      <FindingRow finding={hidden as never} installId={ID} />
    </>,
  );
  expect(screen.getByRole("link", { name: "Reference not permitted (missing ReferenceGrant)" })).toHaveAttribute(
    "href",
    `/i/${ID}/findings/${shown?.id}`,
  );
  // The hidden finding's row (rendered second) links nowhere.
  const restrictedRow = screen.getAllByTestId("finding-row")[1] as HTMLElement;
  expect(restrictedRow).toHaveTextContent("Restricted: ask the owning team");
  expect(within(restrictedRow).queryByRole("link")).toBeNull();
});

it.each([
  [59, "59s ago"],
  [600, "10m ago"],
  [5 * 3600, "5h ago"],
  [16 * 86400, "16d ago"],
])("ages read in the house style: %ss is %s", (secs, text) => {
  const now = Date.parse("2026-11-02T12:00:00Z");
  expect(ago(new Date(now - secs * 1000).toISOString(), now)).toBe(text);
});

it("a finding without an age shows a dash, never the word None", async () => {
  serve((u) => (u.pathname.endsWith("/findings") ? restricted() : undefined));
  mount(`/i/${ID}/findings`);
  await screen.findByText("Restricted: ask the owning team");
  const row = rowOf("Restricted: ask the owning team");
  expect(within(row).getByLabelText("No value")).toHaveTextContent("—");
  expect(row).not.toHaveTextContent("None");
});

it("copying a next step on the finding page announces it", async () => {
  const writeText = vi.fn(async () => {});
  vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
  serve((u) =>
    u.pathname.endsWith(`/findings/${FINDING}`)
      ? {
          ...detail(),
          nextSteps: [{ title: "See the controller log", command: "kubectl logs -n kgw deploy/kgateway" }],
        }
      : undefined,
  );
  mount(`/i/${ID}/findings/${FINDING}`);
  const copy = await screen.findByRole("button", { name: findingsCopy.copyLabel("See the controller log") });
  await userEvent.click(copy);
  expect(writeText).toHaveBeenCalledWith("kubectl logs -n kgw deploy/kgateway");
  await waitFor(() =>
    expect(screen.getAllByRole("status").some((s) => s.textContent === diagnosticsCopy.copied)).toBe(true),
  );
});
