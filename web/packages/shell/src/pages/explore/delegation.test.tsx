import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes, useLocation } from "react-router-dom";
import metaGolden from "../../../../../../pkg/api/testdata/golden/meta.json";
import delegationGolden from "../../../../../../pkg/api/testdata/golden/route-delegation.json";
import cutGolden from "../../../../../../pkg/api/testdata/golden/route-delegation-cut.json";
import delegRestrictedGolden from "../../../../../../pkg/api/testdata/golden/route-delegation-restricted.json";
import routeGolden from "../../../../../../pkg/api/testdata/golden/route-detail.json";
import detailRestrictedGolden from "../../../../../../pkg/api/testdata/golden/route-detail-delegation-restricted.json";
import effectiveGolden from "../../../../../../pkg/api/testdata/golden/route-detail-effective.json";
import selectedGolden from "../../../../../../pkg/api/testdata/golden/route-detail-selected.json";
import droppedGolden from "../../../../../../pkg/api/testdata/golden/routes-dropped.json";
import type { DelegationRow, LinkedRef, MetaResponse, Page, RouteDetail, RouteList, Rule } from "../../api/types";
import {
  columnCopy,
  computedCopy,
  delegationCopy,
  detailCopy,
  effectiveCopy,
  routeCopy,
  routeStateDelegationCopy,
} from "../../copy/explorer";
import { pseudoRules, renderWithTheme } from "../../test/render";
import { controlRoomDark, controlRoomLight } from "../../theme/themes";
import { RoutePage } from "./RoutePage";
import { RoutesPage } from "./RoutesPage";

const ID = "kgw.kgateway";
const GW = "gateway.networking.k8s.io";
const API = `/api/v1/installs/${ID}`;
const now = () => new Date().toISOString();
const page = (path: string) => `/i/${ID}/routes/${GW}/HTTPRoute/${path}`;
const detailApi = (path: string) => `${API}/routes/${GW}/HTTPRoute/${path}`;

const detail = (g: { body: unknown }): RouteDetail =>
  structuredClone({ ...(g.body as RouteDetail), collectedAt: now() });
const rowsOf = (g: { body: unknown }): DelegationRow[] => structuredClone((g.body as Page<DelegationRow>).items);
const meta = metaGolden.body as MetaResponse;
const oldMeta: MetaResponse = { ...meta, filters: { routes: ["listener"] } };

const href = (r: LinkedRef) => ({ group: r.group, kind: r.kind, namespace: r.namespace, name: r.name });
/** A parent route whose rules carry the delegation refs the rows were computed for, one rule per row rule. */
function parentOf(name: string, rows: DelegationRow[]): RouteDetail {
  const d = detail(routeGolden);
  d.ref = { group: GW, kind: "HTTPRoute", namespace: "shop", name, link: "detail" };
  d.effective = undefined;
  const rules: Rule[] = [];
  for (const r of rows) {
    const rule = rules[r.rule] ?? { index: r.rule, matches: [], filters: [], backendRefs: [] };
    rules[r.rule] = rule;
    if (rule.backendRefs[r.ref]) continue;
    const w = r.refAsWritten;
    rule.backendRefs[r.ref] = {
      ref: { group: GW, kind: "HTTPRoute", namespace: w.namespace, name: w.name ?? "*", link: "none" },
      weight: 1,
      filters: [],
      delegation: { ...w },
      ...(r.child ? { ref: { ...href(r.child), link: r.child.link } } : {}),
    };
  }
  d.rules = rules.filter(Boolean);
  return d;
}

type Reply = unknown | [number, unknown];
function serve(fn: (u: URL) => Reply | undefined) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const r = fn(new URL(url, "http://ui"));
      if (r === undefined) return new Response(JSON.stringify({ error: "not found" }), { status: 404 });
      const [status, body] = Array.isArray(r) && typeof r[0] === "number" ? (r as [number, unknown]) : [200, r];
      return new Response(JSON.stringify(body), { status });
    }),
  );
}
const calls = () => vi.mocked(fetch).mock.calls.map((c) => new URL(String(c[0]), "http://ui"));
const delegationCalls = () => calls().filter((u) => u.pathname.endsWith("/delegation"));

/** The rows of rows that the query selects, as the endpoint filters them. */
function rowPage(rows: DelegationRow[], u: URL): Page<DelegationRow> {
  const rule = u.searchParams.get("rule");
  const ref = u.searchParams.get("ref");
  const outcomes = u.searchParams.getAll("outcome");
  const items = rows.filter(
    (r) =>
      (rule === null || r.rule === Number(rule)) &&
      (ref === null || r.ref === Number(ref)) &&
      (!outcomes.length || (r.outcome !== undefined && outcomes.includes(r.outcome))),
  );
  return { install: ID, items, total: items.length, collectedAt: now() };
}

function api(o: { name: string; d: RouteDetail; rows?: (u: URL) => Reply; meta?: MetaResponse }) {
  return (u: URL): Reply | undefined => {
    if (u.pathname === "/api/v1/meta") return o.meta ?? meta;
    if (u.pathname === detailApi(`shop/${o.name}`)) return o.d;
    if (u.pathname === `${detailApi(`shop/${o.name}`)}/delegation`) return o.rows?.(u);
    if (u.pathname === `${API}/routes`) return { install: ID, items: [], total: 0, collectedAt: now() };
    return undefined;
  };
}

function Where() {
  const loc = useLocation();
  return <output data-testid="location">{loc.pathname + loc.search}</output>;
}
const location = () => screen.getByTestId("location").textContent ?? "";

function mount(path: string) {
  return renderWithTheme(
    <>
      <Routes>
        <Route path="/i/:id/routes/:group/:kind/:ns/:name" element={<RoutePage />} />
        <Route path="/i/:id/routes" element={<RoutesPage />} />
        <Route path="*" element={<p>elsewhere</p>} />
      </Routes>
      <Where />
    </>,
    { path },
  );
}
const loaded = () => screen.findByRole("navigation", { name: routeCopy.upChain });
/** The Delegation tab once the detail has loaded. */
const tabLoaded = () => screen.findAllByTestId("delegation-ref");
const effective = new RegExp(`^${effectiveCopy.title}`);
const refSection = (rule: number, ref: number) =>
  screen.getByRole("region", { name: delegationCopy.refTitle(rule + 1, ref + 1) });
/** Opens one ref's rows in the Delegation tab. */
async function openRef(rule: number, ref: number) {
  const s = refSection(rule, ref);
  const toggle = within(s).getByRole("button", { name: delegationCopy.toggleRows(false) });
  fireEvent.click(toggle);
  expect(toggle).toHaveAttribute("aria-expanded", "true");
  return s;
}
const rowsIn = (el: HTMLElement) => within(el).findAllByTestId("delegation-row");

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Delegation tab", () => {
  const rows = rowsOf(delegationGolden);
  const m = () => parentOf("m", rows);

  it("Delegation tab lists rows per ref, paged, each with outcome and the controller's signal", async () => {
    // Rule 1 ref 1 has two rows here, one per page.
    const second = { ...structuredClone(rows[0] as DelegationRow), child: { ...rows[0]?.child, name: "kid2" } };
    serve(
      api({
        name: "m",
        d: m(),
        rows: (u) => {
          if (u.searchParams.get("rule") !== "0") return rowPage(rows, u);
          const base = { install: ID, total: 2, collectedAt: now() };
          return u.searchParams.get("cursor") === "n2"
            ? { ...base, items: [second], prev: "p1" }
            : { ...base, items: [rows[0]], next: "n2" };
        },
      }),
    );
    mount(`${page("shop/m")}?tab=delegation`);
    await tabLoaded();
    expect(screen.getByRole("tab", { name: delegationCopy.tab })).toHaveAttribute("aria-selected", "true");
    // One section per ref; nothing is fetched until a ref opens.
    expect(screen.getAllByRole("region", { name: /^Rule \d+, ref \d+$/ })).toHaveLength(10);
    expect(delegationCalls()).toHaveLength(0);
    const s = await openRef(0, 0);
    const [first] = await rowsIn(s);
    const q = delegationCalls()[0] as URL;
    expect(q.searchParams.get("rule")).toBe("0");
    expect(q.searchParams.get("ref")).toBe("0");
    expect(first).toHaveTextContent("shop/kid");
    expect(first).toHaveTextContent(delegationCopy.outcomes.attached);
    // The controller's signal: the child's entry under this parent.
    expect(first).toHaveTextContent(delegationCopy.entry("Accepted", "True"));
    fireEvent.click(within(s).getByRole("button", { name: "Next page" }));
    await waitFor(() => expect(within(s).getByTestId("delegation-row")).toHaveTextContent("shop/kid2"));
    expect(delegationCalls().at(-1)?.searchParams.get("cursor")).toBe("n2");
    // A row with no entry says so.
    const drop = await openRef(1, 0);
    const [d] = await rowsIn(drop);
    expect(d).toHaveTextContent(delegationCopy.noEntry);
  });

  it("every outcome has its icon and text", async () => {
    serve(api({ name: "m", d: m(), rows: (u) => rowPage(rows, u) }));
    mount(`${page("shop/m")}?tab=delegation`);
    await tabLoaded();
    const want: [number, keyof typeof delegationCopy.outcomes, string][] = [
      [0, "attached", ""],
      [1, "dropped", "the route's rule 1: path /other is outside the parent's /api"],
      [2, "forbidden", "The route's parentRefs do not permit this parent"],
      [3, "unresolved", "No such route"],
      [4, "rejected-hostnames", "A delegated route may not set hostnames"],
      [5, "cycle", "A delegation loop fails this ref"],
      [6, "partial", "Attached on some paths only"],
    ];
    for (const [rule, outcome, reason] of want) {
      const [r] = await rowsIn(await openRef(rule, 0));
      const o = within(r as HTMLElement).getByTestId("outcome");
      expect(o).toHaveTextContent(delegationCopy.outcomes[outcome]);
      expect(within(o).getByTestId(/^outcome-icon-/)).toBeInTheDocument();
      if (reason) expect(r).toHaveTextContent(reason);
    }
  });

  it("a missing parent condition is worded apart from a missing entry", async () => {
    const rs = structuredClone(rows) as DelegationRow[];
    delete (rs[1] as DelegationRow).observed.parentCondition;
    serve(api({ name: "m", d: m(), rows: (u) => rowPage(rs, u) }));
    mount(`${page("shop/m")}?tab=delegation`);
    await tabLoaded();
    const [r] = await rowsIn(await openRef(1, 0));
    fireEvent.click(within(r as HTMLElement).getByRole("button", { name: delegationCopy.togglePaths(false, 1) }));
    // S232 under the parent's label; S214 stays the child's.
    expect(r).toHaveTextContent(`${delegationCopy.parentCondition}${delegationCopy.noCondition}`);
    expect(r).toHaveTextContent(`${delegationCopy.childStatus}${delegationCopy.noEntry}`);
  });

  it("an unknown row names its reason", async () => {
    const cut = rowsOf(cutGolden);
    serve(api({ name: "m", d: parentOf("m", cut), rows: (u) => rowPage(cut, u) }));
    mount(`${page("shop/m")}?tab=delegation`);
    await tabLoaded();
    const [r] = await rowsIn(await openRef(9, 0));
    expect(within(r as HTMLElement).getByTestId("outcome")).toHaveTextContent(delegationCopy.outcomes.unknown);
    // The Why cell drops the badge's "Unknown: " prefix (S233).
    expect((r as HTMLElement).querySelector(".reason")).toHaveTextContent(/^Too large to compute$/);
  });

  it("contexts expand with aria-expanded; a restricted context shows the placeholder", async () => {
    const rs = rowsOf(delegRestrictedGolden);
    (rs[0] as DelegationRow).contexts.push({ restricted: true });
    serve(api({ name: "p2", d: parentOf("p2", rs), rows: (u) => rowPage(rs, u) }));
    mount(`${page("shop/p2")}?tab=delegation`);
    await tabLoaded();
    const [r] = await rowsIn(await openRef(0, 0));
    const t = within(r as HTMLElement).getByRole("button", { name: delegationCopy.togglePaths(false, 2) });
    expect(t).toHaveAttribute("aria-expanded", "false");
    expect(within(r as HTMLElement).queryAllByTestId("delegation-context")).toHaveLength(0);
    fireEvent.click(t);
    expect(t).toHaveAttribute("aria-expanded", "true");
    // The open list has its own name, not the toggle's (S217, S231).
    expect(within(r as HTMLElement).getByRole("list", { name: delegationCopy.pathList(2) })).toBeInTheDocument();
    const ctx = within(r as HTMLElement).getAllByTestId("delegation-context");
    expect(ctx).toHaveLength(2);
    expect(ctx[1]).toHaveTextContent(computedCopy.restricted);
    expect(ctx[0]).toHaveTextContent("PathPrefix /api/h/2");
    // The selection-restricted row of the wildcard: hidden, with no outcome.
    const wild = await rowsIn(await openRef(1, 0));
    const hidden = wild.find((w) => !within(w).queryByRole("link"));
    expect(hidden).toHaveTextContent(delegationCopy.notShown);
    expect(hidden).toHaveTextContent(computedCopy.short.restricted);
  });

  it("a hidden ancestor shows its name without a link, and its delegating match", async () => {
    const rs = rowsOf(delegRestrictedGolden);
    serve(api({ name: "p2", d: parentOf("p2", rs), rows: (u) => rowPage(rs, u) }));
    mount(`${page("shop/p2")}?tab=delegation`);
    await tabLoaded();
    const [r] = await rowsIn(await openRef(0, 0));
    fireEvent.click(within(r as HTMLElement).getByRole("button", { name: delegationCopy.togglePaths(false, 1) }));
    const step = within(r as HTMLElement)
      .getAllByTestId("path-step")
      .find((s) => s.textContent?.includes("shop/h")) as HTMLElement;
    expect(within(step).queryByRole("link")).toBeNull();
    expect(step).toHaveTextContent("PathPrefix /api/h");
    // A visible ancestor links.
    expect(within(r as HTMLElement).getByRole("link", { name: /shop\/r/ })).toBeInTheDocument();
  });

  it("contextsTruncated says more paths exist", async () => {
    const cut = rowsOf(cutGolden);
    const some = structuredClone(rows[0] as DelegationRow);
    some.contextsTruncated = true;
    const all = [...cut, some];
    serve(api({ name: "m", d: parentOf("m", all), rows: (u) => rowPage(all, u) }));
    mount(`${page("shop/m")}?tab=delegation`);
    await tabLoaded();
    const [none] = await rowsIn(await openRef(9, 0));
    fireEvent.click(within(none as HTMLElement).getByRole("button", { name: delegationCopy.togglePaths(false, 0) }));
    expect(none).toHaveTextContent(delegationCopy.noPathsKept);
    const [kept] = await rowsIn(await openRef(0, 0));
    fireEvent.click(within(kept as HTMLElement).getByRole("button", { name: delegationCopy.togglePaths(false, 1) }));
    expect(kept).toHaveTextContent(delegationCopy.morePaths);
  });

  it("a row the status cannot confirm says so", async () => {
    serve(api({ name: "m", d: m(), rows: (u) => rowPage(rows, u) }));
    mount(`${page("shop/m")}?tab=delegation`);
    await tabLoaded();
    const [r] = await rowsIn(await openRef(7, 0));
    expect(within(r as HTMLElement).getByTestId("row-note")).toHaveTextContent("Status cannot confirm this path");
  });

  it("outcome, rule and ref filters stay page-local and only ?tab=delegation is in the URL; a 400 cursor reloads page one", async () => {
    serve(
      api({
        name: "m",
        d: m(),
        rows: (u) =>
          u.searchParams.get("cursor") === "bad"
            ? [400, { error: "bad cursor" }]
            : { ...rowPage(rows, u), next: u.searchParams.has("cursor") ? undefined : "bad" },
      }),
    );
    mount(page("shop/m"));
    await loaded();
    await userEvent.click(screen.getByRole("tab", { name: delegationCopy.tab }));
    await waitFor(() => expect(location()).toBe(`${page("shop/m")}?tab=delegation`));
    await userEvent.selectOptions(screen.getByRole("combobox", { name: delegationCopy.ruleFilter }), "1");
    expect(screen.getAllByRole("region", { name: /^Rule \d+, ref \d+$/ })).toHaveLength(1);
    await userEvent.selectOptions(screen.getByRole("combobox", { name: delegationCopy.refFilter }), "0");
    const group = screen.getByRole("group", { name: delegationCopy.outcomeGroup });
    await userEvent.click(within(group).getByRole("button", { name: delegationCopy.outcomes.dropped }));
    expect(location()).toBe(`${page("shop/m")}?tab=delegation`);
    const s = await openRef(1, 0);
    await rowsIn(s);
    const q = delegationCalls().at(-1) as URL;
    expect(q.searchParams.getAll("outcome")).toEqual(["dropped"]);
    expect(q.searchParams.get("rule")).toBe("1");
    // Next carries a cursor the server rejects: page one loads again.
    fireEvent.click(within(s).getByRole("button", { name: "Next page" }));
    await waitFor(() => expect(delegationCalls().some((u) => u.searchParams.get("cursor") === "bad")).toBe(true));
    await waitFor(() => expect(delegationCalls().at(-1)?.searchParams.has("cursor")).toBe(false));
    expect(await rowsIn(s)).toHaveLength(1);
    expect(location()).toBe(`${page("shop/m")}?tab=delegation`);
  });
});

it("effective matches per parent path in both modes, marked with the not-verified coverage copy", async () => {
  serve(api({ name: "mid", d: detail(routeGolden) }));
  const first = mount(page("shop/mid"));
  await loaded();
  let s = screen.getByRole("region", { name: effective });
  const edges = within(s).getAllByTestId("effective-edge");
  expect(edges).toHaveLength(6);
  const loop = edges[0] as HTMLElement;
  expect(loop).toHaveTextContent("shop/loop");
  expect(loop).toHaveTextContent(effectiveCopy.modes.default);
  expect(loop).toHaveTextContent(computedCopy.coverage["not-verified"]);
  expect(within(loop).getByTestId("effective-rule-0")).toHaveTextContent(effectiveCopy.kept);
  expect(within(loop).getByTestId("effective-rule-0")).toHaveTextContent("PathPrefix /api");
  expect(within(loop).getByTestId("effective-rule-2")).toHaveTextContent("path / is outside the parent's /api");
  first.unmount();

  serve(api({ name: "x", d: detail(effectiveGolden) }));
  mount(page("shop/x"));
  await loaded();
  s = screen.getByRole("region", { name: effective });
  const [a] = within(s).getAllByTestId("effective-edge");
  expect(a).toHaveTextContent(effectiveCopy.modes.inherit);
  expect(a).toHaveTextContent("PathPrefix /a/x");
  expect(a).toHaveTextContent(delegationCopy.morePaths);
  expect(a).toHaveTextContent(computedCopy.coverage["not-verified"]);
});

it("an edge with no kept contexts never looks complete", async () => {
  serve(api({ name: "x", d: detail(effectiveGolden) }));
  mount(page("shop/x"));
  await loaded();
  const [, b] = within(screen.getByRole("region", { name: effective })).getAllByTestId("effective-edge");
  expect(b).toHaveTextContent("shop/b");
  expect(b).toHaveTextContent(delegationCopy.noPathsKept);
  expect(within(b as HTMLElement).queryByTestId(/^effective-rule-/)).toBeNull();
});

it("a hidden ancestor's effective edge has no link; a restricted edge shows only the placeholder", async () => {
  serve(api({ name: "p", d: detail(detailRestrictedGolden) }));
  mount(page("shop/p"));
  await loaded();
  const [h, sEdge] = within(screen.getByRole("region", { name: effective })).getAllByTestId("effective-edge");
  expect(h).toHaveTextContent("shop/h");
  expect(within(h as HTMLElement).queryByRole("link", { name: /shop\/h/ })).toBeNull();
  expect(sEdge).toHaveTextContent(computedCopy.restricted);
  expect(within(sEdge as HTMLElement).queryByTestId(/^effective-rule-/)).toBeNull();
});

it("the up-chain shows a hidden ancestor route by name without a link; a hidden Gateway stays 'A parent you cannot view'", async () => {
  serve(api({ name: "p", d: detail(detailRestrictedGolden) }));
  mount(page("shop/p"));
  const nav = await loaded();
  const paths = within(nav).getAllByTestId("route-path");
  expect(paths[1]).toHaveTextContent(routeCopy.hiddenParent);
  const hidden = paths[2] as HTMLElement;
  expect(hidden).toHaveTextContent("shop/h");
  expect(hidden).toHaveTextContent("PathPrefix /api/h");
  expect(within(hidden).queryAllByRole("link")).toHaveLength(0);
  expect(hidden).not.toHaveTextContent(routeCopy.hiddenParent);
});

it("the tab and the dropped filter appear only when /meta advertises filters.delegation", async () => {
  const rows = rowsOf(delegationGolden);
  const d = parentOf("m", rows);
  const routes = () => ({ ...(droppedGolden.body as RouteList), collectedAt: now() });
  const withRoutes = (o: MetaResponse) => (u: URL) =>
    u.pathname === `${API}/routes` ? routes() : api({ name: "m", d, meta: o })(u);
  serve(withRoutes(oldMeta));
  const first = mount(`${page("shop/m")}?tab=delegation`);
  await loaded();
  expect(screen.queryByRole("tab", { name: delegationCopy.tab })).toBeNull();
  // An older server's page falls back to Overview.
  expect(screen.getByRole("tab", { name: detailCopy.overview })).toHaveAttribute("aria-selected", "true");
  first.unmount();
  const list = mount(`/i/${ID}/routes`);
  await screen.findByText("drop");
  expect(
    within(screen.getByRole("group", { name: "Route state" })).queryByRole("button", {
      name: routeStateDelegationCopy.dropped,
    }),
  ).toBeNull();
  list.unmount();

  serve(withRoutes(meta));
  const second = mount(page("shop/m"));
  expect(await screen.findByRole("tab", { name: delegationCopy.tab })).toBeInTheDocument();
  second.unmount();
  mount(`/i/${ID}/routes`);
  await screen.findByText("drop");
  const chip = await within(screen.getByRole("group", { name: "Route state" })).findByRole("button", {
    name: routeStateDelegationCopy.dropped,
  });
  await userEvent.click(chip);
  await waitFor(() =>
    expect(
      calls()
        .filter((u) => u.pathname === `${API}/routes`)
        .at(-1)
        ?.searchParams.getAll("state"),
    ).toEqual(["dropped"]),
  );
});

it("a route with no delegation refs has no Delegation tab", async () => {
  serve(api({ name: "x", d: detail(effectiveGolden) }));
  mount(page("shop/x"));
  await loaded();
  expect(screen.queryByRole("tab", { name: delegationCopy.tab })).toBeNull();
});

it("a delegation ref shows selected and attached counts with a ComputedBadge", async () => {
  serve(api({ name: "mid", d: detail(routeGolden) }));
  mount(page("shop/mid"));
  await loaded();
  const declared = screen.getByTestId("backend-declared");
  expect(declared).toHaveTextContent(delegationCopy.selected(2));
  expect(declared).toHaveTextContent(delegationCopy.attached(1));
  expect(within(declared).getByTestId("computed-badge")).toBeInTheDocument();
  // Selected may include routes passed on from an earlier ref: the badge's tooltip says so (quirk 1).
  expect(declared.textContent).not.toMatch(/this ref names/i);
  const counts = within(declared).getByTestId("ref-counts");
  await userEvent.hover(within(counts).getByText(computedCopy.chip));
  expect(within(counts).getByRole("tooltip")).toHaveTextContent(delegationCopy.selectedHint);
});

it("a restricted delegation ref shows its badge hidden and no attached count", async () => {
  serve(api({ name: "p", d: detail(detailRestrictedGolden) }));
  mount(page("shop/p"));
  await loaded();
  const c = screen.getByTestId("backend-c");
  expect(within(c).getByTestId("computed-badge")).toHaveTextContent(computedCopy.short.restricted);
  expect(c).not.toHaveTextContent(delegationCopy.attached(0));
  // A Hidden ref's count covers visible routes only, and says so (S234).
  expect(c).toHaveTextContent(delegationCopy.visibleSelected(1));
  expect(c).not.toHaveTextContent(delegationCopy.selected(1));
});

it("a selected route's candidate banner names its parent", async () => {
  serve((u) =>
    u.pathname === detailApi("kids/s") ? detail(selectedGolden) : u.pathname === "/api/v1/meta" ? meta : undefined,
  );
  mount(page("kids/s"));
  const banner = await screen.findByTestId("candidate-banner");
  expect(within(banner).getByRole("link", { name: /shop\/w/ })).toHaveAttribute("href", page("shop/w"));
  expect(banner).toHaveTextContent(detailCopy.bannerSelected);
});

describe.each([controlRoomLight, controlRoomDark])("interactive styles in $id", (theme) => {
  it("Delegation tab expanders, filters and pager have hover, active and focus-visible styles", async () => {
    const rows = rowsOf(delegationGolden);
    serve(api({ name: "m", d: parentOf("m", rows), rows: (u) => ({ ...rowPage(rows, u), next: "n2" }) }));
    renderWithTheme(
      <Routes>
        <Route path="/i/:id/routes/:group/:kind/:ns/:name" element={<RoutePage />} />
      </Routes>,
      { path: `${page("shop/m")}?tab=delegation`, themeId: theme.id },
    );
    await tabLoaded();
    const s = await openRef(0, 0);
    const [r] = await rowsIn(s);
    const els = [
      within(s).getByRole("button", { name: delegationCopy.toggleRows(true) }),
      within(r as HTMLElement).getByRole("button", { name: delegationCopy.togglePaths(false, 1) }),
      screen.getByRole("combobox", { name: delegationCopy.ruleFilter }),
      screen.getByRole("combobox", { name: delegationCopy.refFilter }),
      within(screen.getByRole("group", { name: delegationCopy.outcomeGroup })).getByRole("button", {
        name: delegationCopy.outcomes.dropped,
      }),
    ];
    for (const el of els)
      for (const pseudo of [":hover", ":active", ":focus-visible"])
        expect(
          pseudoRules(el, pseudo).join("").length,
          `${el.getAttribute("aria-label") ?? el.textContent} ${pseudo}`,
        ).toBeGreaterThan(0);
  });
});

it("a deep link to the Delegation tab keeps it selected while loading; no delegation refs falls back to Overview", async () => {
  let release: (() => void) | undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  const rows = rowsOf(delegationGolden);
  const reply = api({ name: "m", d: parentOf("m", rows), rows: (u) => rowPage(rows, u) });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      await gate;
      return new Response(JSON.stringify(reply(new URL(url, "http://ui"))), { status: 200 });
    }),
  );
  const first = mount(`${page("shop/m")}?tab=delegation`);
  const tab = screen.getByRole("tab", { name: delegationCopy.tab });
  expect(tab).toHaveAttribute("aria-selected", "true");
  expect(screen.getByTestId("detail-skeleton")).toBeInTheDocument();
  await act(async () => release?.());
  await tabLoaded();
  expect(screen.getByRole("tab", { name: delegationCopy.tab })).toHaveAttribute("aria-selected", "true");
  first.unmount();

  serve(api({ name: "x", d: detail(effectiveGolden) }));
  mount(`${page("shop/x")}?tab=delegation`);
  await loaded();
  expect(screen.queryByRole("tab", { name: delegationCopy.tab })).toBeNull();
  expect(screen.getByRole("tab", { name: detailCopy.overview })).toHaveAttribute("aria-selected", "true");
});

it("a restricted row shows no reason even if one is sent", async () => {
  const rs = rowsOf(delegRestrictedGolden);
  const hidden = rs.find((r) => r.computed.state === "restricted") as DelegationRow;
  hidden.reason = "leaked reason";
  serve(api({ name: "p2", d: parentOf("p2", rs), rows: (u) => rowPage(rs, u) }));
  mount(`${page("shop/p2")}?tab=delegation`);
  await tabLoaded();
  const s = await openRef(1, 0);
  await rowsIn(s);
  expect(s).not.toHaveTextContent("leaked reason");
});

it("rows are a table whose cells sit under named column headers", async () => {
  const rows = rowsOf(delegationGolden);
  serve(api({ name: "m", d: parentOf("m", rows), rows: (u) => rowPage(rows, u) }));
  mount(`${page("shop/m")}?tab=delegation`);
  await tabLoaded();
  const s = await openRef(1, 0);
  const t = await within(s).findByRole("table", { name: delegationCopy.refTitle(2, 1) });
  const heads = within(t).getAllByRole("columnheader");
  expect(heads.map((h) => h.textContent)).toEqual([
    delegationCopy.pathsColumn,
    columnCopy.route,
    delegationCopy.outcomeGroup,
    columnCopy.why,
    delegationCopy.childStatus,
    computedCopy.chip,
  ]);
  const [, row] = within(t).getAllByRole("row");
  const cells = within(row as HTMLElement).getAllByRole("cell");
  expect(cells).toHaveLength(heads.length);
  expect(cells[2]).toHaveTextContent(delegationCopy.outcomes.dropped);
});
