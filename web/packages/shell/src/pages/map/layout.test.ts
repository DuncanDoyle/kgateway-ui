import type { LinkedRef, PathRow } from "../../api/types";
import { buildGraph, type MapGraph } from "./graph";
import { INDENT, layoutColumns, type Point, ROW_H } from "./layout";

const ref = (kind: string, name: string, namespace = "shop"): LinkedRef => ({
  group: kind === "Service" ? "" : "gateway.networking.k8s.io",
  kind,
  namespace,
  name,
  link: "detail",
});
const GW = ref("Gateway", "edge", "infra");

/** A healthy HTTP path row through listener l, routes (root first) and backend b; over overrides any field. */
function row(l: string, routes: string[], b: string | undefined, over: Partial<PathRow> = {}): PathRow {
  return {
    gateway: GW,
    listener: { name: l, protocol: "HTTP", port: 80 },
    hostnames: [],
    routes: routes.map((r) => ref("HTTPRoute", r)),
    delegated: routes.length > 1,
    rule: 0,
    matches: [],
    action: b ? "backend" : "none",
    backend: b ? ref("Service", b) : undefined,
    weight: 1,
    share: 100,
    shareState: "shown",
    mirror: false,
    problem: { state: "healthy", signals: [] },
    ...over,
  };
}

const ids = (g: MapGraph, kind: string) => g.nodes.filter((n) => n.kind === kind).map((n) => n.id);

/** Edge pairs between adjacent columns that cross under positions at. */
function crossings(g: MapGraph, at: Map<string, Point>): number {
  const segs = g.edges.map((e) => [at.get(e.source) as Point, at.get(e.target) as Point] as const);
  let n = 0;
  for (let i = 0; i < segs.length; i++)
    for (let j = i + 1; j < segs.length; j++) {
      const [a1, a2] = segs[i] as readonly [Point, Point];
      const [b1, b2] = segs[j] as readonly [Point, Point];
      if (Math.round(a1.x / 100) !== Math.round(b1.x / 100)) continue;
      if ((a1.y - b1.y) * (a2.y - b2.y) < 0) n++;
    }
  return n;
}

it("one node per distinct Gateway, listener, route and backend, each with the rows through it", () => {
  const rows = [
    row("http", ["api"], "svc-a"),
    row("http", ["api"], "svc-b"),
    row("https", ["api"], "svc-a"),
    row("http", ["web"], "svc-a"),
    // Same listener name on a ListenerSet is another listener.
    row("http", ["web"], "svc-a", { listenerSet: ref("XListenerSet", "ls1") }),
  ];
  const g = buildGraph(rows, new Set());
  expect(ids(g, "gateway")).toHaveLength(1);
  expect(ids(g, "listener")).toHaveLength(3);
  expect(ids(g, "route")).toEqual([
    "r:gateway.networking.k8s.io/HTTPRoute/shop/api",
    "r:gateway.networking.k8s.io/HTTPRoute/shop/web",
  ]);
  expect(ids(g, "backend")).toEqual(["b:/Service/shop/svc-a", "b:/Service/shop/svc-b"]);
  expect(g.nodes.find((n) => n.id === "b:/Service/shop/svc-a")?.rows).toEqual([0, 2, 3, 4]);
  // Every edge joins adjacent columns, once per pair, with the rows on it.
  expect(
    g.edges.find((e) => e.id === "r:gateway.networking.k8s.io/HTTPRoute/shop/api>b:/Service/shop/svc-a")?.rows,
  ).toEqual([0, 2]);
  expect(new Set(g.edges.map((e) => e.id)).size).toBe(g.edges.length);
});

it("delegated routes sit under their root with delegatedUnder, and only the root has a listener edge", () => {
  const rows = [
    row("http", ["parent"], undefined, { action: "delegate", shareState: "not-shown", share: undefined }),
    row("http", ["parent", "child"], "svc-a"),
    row("http", ["other"], "svc-b"),
    row("http", ["parent", "child2"], "svc-b"),
  ];
  const g = buildGraph(rows, new Set());
  const root = "r:gateway.networking.k8s.io/HTTPRoute/shop/parent";
  const child = "r:gateway.networking.k8s.io/HTTPRoute/shop/child";
  const child2 = "r:gateway.networking.k8s.io/HTTPRoute/shop/child2";
  expect(g.nodes.find((n) => n.id === child)?.delegatedUnder).toBe(root);
  expect(g.nodes.find((n) => n.id === root)?.delegatedUnder).toBeUndefined();
  // The root covers its delegated rows too, so hover through it reaches the children's backends.
  expect(g.nodes.find((n) => n.id === root)?.rows).toEqual([0, 1, 3]);
  expect(g.edges.filter((e) => e.target === child)).toEqual([]);
  // A delegating rule draws no backend-column node.
  expect(g.nodes.filter((n) => n.kind === "backend").map((n) => n.id)).toEqual([
    "b:/Service/shop/svc-a",
    "b:/Service/shop/svc-b",
  ]);

  const at = layoutColumns(g);
  const routes = [...ids(g, "route")].sort((a, b) => (at.get(a)?.y ?? 0) - (at.get(b)?.y ?? 0));
  const k = routes.indexOf(root);
  expect(new Set(routes.slice(k + 1, k + 3))).toEqual(new Set([child, child2]));
  expect(at.get(child)?.x).toBe((at.get(root)?.x ?? 0) + INDENT);
});

it("a route attached directly and also delegated is a root", () => {
  const g = buildGraph([row("http", ["parent", "child"], "svc-a"), row("http", ["child"], "svc-a")], new Set());
  expect(
    g.nodes.find((n) => n.id === "r:gateway.networking.k8s.io/HTTPRoute/shop/child")?.delegatedUnder,
  ).toBeUndefined();
});

it("edges carry share only where every row on them shows it, and are broken only when every row is", () => {
  const rows = [
    row("http", ["r1"], "svc-a", { share: 25 }),
    row("http", ["r1"], "svc-b", { share: 75, shareState: "failing", problem: { state: "error", signals: [] } }),
    row("http", ["r2"], "svc-a", { shareState: "not-shown", share: undefined }),
    row("http", ["r2"], "svc-m", { mirror: true, shareState: "mirror", share: undefined }),
    row("http", ["r3"], "svc-b", { problem: { state: "error", signals: [] }, share: 100 }),
  ];
  const g = buildGraph(rows, new Set());
  const e = (route: string, b: string) =>
    g.edges.find((x) => x.id === `r:gateway.networking.k8s.io/HTTPRoute/shop/${route}>b:/Service/shop/${b}`);
  expect(e("r1", "svc-a")).toMatchObject({ share: 25, broken: false });
  // A failing share is still not "shown": it is drawn broken instead.
  expect(e("r1", "svc-b")?.share).toBeUndefined();
  expect(e("r1", "svc-b")?.broken).toBe(true);
  expect(e("r2", "svc-a")?.share).toBeUndefined();
  expect(e("r2", "svc-m")?.share).toBeUndefined();
  expect(e("r3", "svc-b")).toMatchObject({ share: 100, broken: true });
  // Only backend edges carry a share. The Gateway's edge has healthy rows, so it is not broken; r3's has none.
  expect(g.edges.filter((x) => !x.target.startsWith("b:")).every((x) => x.share === undefined)).toBe(true);
  expect(g.edges.find((x) => x.source.startsWith("g:"))?.broken).toBe(false);
  expect(g.edges.find((x) => x.target.endsWith("/r3"))?.broken).toBe(true);
});

it("rules with no backend end in a node of their written action, one per route", () => {
  const rows = [
    row("http", ["r1"], undefined, { action: "redirect" }),
    row("http", ["r2"], undefined, { action: "redirect" }),
    row("http", ["r2"], undefined, { action: "direct-response" }),
    row("http", ["r2"], undefined, { action: "none" }),
  ];
  const g = buildGraph(rows, new Set());
  const ends = g.nodes.filter((n) => n.kind === "backend");
  expect(ends.map((n) => [n.action, n.name])).toEqual([
    ["redirect", "Redirect"],
    ["redirect", "Redirect"],
    ["direct-response", "Direct response"],
    ["none", "No backend"],
  ]);
  expect(ends.every((n) => n.ref === undefined)).toBe(true);
});

it("a node's state comes from the signals that name it, so a broken backend leaves its route healthy", () => {
  const svc = ref("Service", "svc-a");
  const g = buildGraph(
    [
      row("http", ["r1"], "svc-a", {
        problem: { state: "error", signals: [{ kind: "finding", code: "KGWUI-023", ref: svc, state: "error" }] },
      }),
      row("http", ["r1"], "svc-b", {
        problem: { state: "unknown", signals: [{ kind: "waiting", code: "listener", ref: GW, state: "unknown" }] },
      }),
      // A row state no signal explains lands on its last node.
      row("http", ["r2"], "svc-c", { problem: { state: "warning", signals: [] } }),
    ],
    new Set(),
  );
  const state = (id: string) => g.nodes.find((n) => n.id === id)?.state;
  expect(state("b:/Service/shop/svc-a")).toBe("error");
  expect(state("r:gateway.networking.k8s.io/HTTPRoute/shop/r1")).toBe("healthy");
  expect(state("l:http")).toBe("unknown");
  expect(state("g:gateway.networking.k8s.io/Gateway/infra/edge")).toBe("healthy");
  expect(state("b:/Service/shop/svc-c")).toBe("warning");
});

/** Listener l1 serves r1 and r3, l2 serves r2 and r4: in input order l1→r3 crosses l2→r2. */
const crossingRows = () => [
  row("l1", ["r1"], "b2"),
  row("l2", ["r2"], "b1"),
  row("l1", ["r3"], "b2"),
  row("l2", ["r4"], "b1"),
  row("l2", ["r2"], "b3"),
  row("l1", ["r1"], "b4"),
];

it("the layout is deterministic and places four columns left to right", () => {
  const a = layoutColumns(buildGraph(crossingRows(), new Set()));
  const b = layoutColumns(buildGraph(crossingRows(), new Set()));
  expect([...a]).toEqual([...b]);
  const g = buildGraph(crossingRows(), new Set());
  const xs = ["gateway", "listener", "route", "backend"].map((k) => new Set(ids(g, k).map((id) => a.get(id)?.x)));
  expect(xs.every((s) => s.size === 1)).toBe(true);
  expect(xs.map((s) => [...s][0])).toEqual([...xs.map((s) => [...s][0] as number)].sort((p, q) => p - q));
  // No two nodes share a slot.
  expect(new Set([...a.values()].map((p) => `${p.x},${p.y}`)).size).toBe(a.size);
});

it("barycentre ordering crosses fewer edges than input order", () => {
  const g = buildGraph(crossingRows(), new Set());
  // With no column allowed to move, every column keeps its input order.
  const input = layoutColumns(g, new Set());
  const sorted = layoutColumns(g);
  expect(crossings(g, input)).toBeGreaterThan(0);
  expect(crossings(g, sorted)).toBeLessThan(crossings(g, input));
});

it("changed={route} re-runs only the routes column: Gateway and listener positions stay", () => {
  const g1 = buildGraph(crossingRows(), new Set());
  const before = layoutColumns(g1);
  // A new route on l1, ahead of the others in input order.
  const g2 = buildGraph([row("l1", ["r0"], "b4"), ...crossingRows()], new Set());
  const after = layoutColumns(g2, new Set(["route"]), before);
  for (const id of [...ids(g2, "gateway"), ...ids(g2, "listener"), ...ids(g2, "backend")])
    expect(after.get(id)).toEqual(before.get(id));
  expect(after.has("r:gateway.networking.k8s.io/HTTPRoute/shop/r0")).toBe(true);
  expect(new Set(ids(g2, "route").map((id) => after.get(id)?.y)).size).toBe(ids(g2, "route").length);
});

it("an edge's state is the worst of its rows, broken or not", () => {
  const g = buildGraph(
    [
      row("http", ["r1"], "svc-a", { problem: { state: "warning", signals: [] } }),
      row("http", ["r1"], "svc-a", { rule: 1, problem: { state: "error", signals: [] } }),
      row("http", ["r2"], "svc-b"),
    ],
    new Set(),
  );
  const e = (route: string, b: string) =>
    g.edges.find((x) => x.id === `r:gateway.networking.k8s.io/HTTPRoute/shop/${route}>b:/Service/shop/${b}`);
  expect(e("r1", "svc-a")).toMatchObject({ state: "error", broken: false });
  expect(e("r2", "svc-b")).toMatchObject({ state: "healthy", broken: false });
  expect(g.edges.find((x) => x.source.startsWith("g:"))?.state).toBe("error");
});

it("an edge over several rules carries a share only when their shares agree", () => {
  const g = buildGraph(
    [
      // r1 reaches svc-a from rules 0 and 1 with different shares: no share.
      row("http", ["r1"], "svc-a", { share: 30 }),
      row("http", ["r1"], "svc-a", { rule: 1, share: 70 }),
      // r2 reaches svc-a from two rules with the same share, and from one rule on two listeners.
      row("http", ["r2"], "svc-a", { share: 50 }),
      row("http", ["r2"], "svc-a", { rule: 1, share: 50 }),
      row("http", ["r3"], "svc-a", { share: 40 }),
      row("https", ["r3"], "svc-a", { share: 40 }),
    ],
    new Set(),
  );
  const share = (route: string) =>
    g.edges.find((x) => x.id === `r:gateway.networking.k8s.io/HTTPRoute/shop/${route}>b:/Service/shop/svc-a`)?.share;
  expect(share("r1")).toBeUndefined();
  expect(share("r2")).toBe(50);
  expect(share("r3")).toBe(40);
});

it("a column with a node prev lacks is re-laid even when it is not in changed", () => {
  const before = layoutColumns(buildGraph(crossingRows(), new Set()));
  // A new listener and route; only routes are named as changed.
  const g = buildGraph([...crossingRows(), row("l3", ["r9"], "b1")], new Set());
  const after = layoutColumns(g, new Set(["route"]), before);
  // The listeners column is laid out again as a whole: three even slots around 0.
  const ys = ids(g, "listener").map((id) => after.get(id)?.y as number);
  expect([...ys].sort((a, b) => a - b)).toEqual([-ROW_H, 0, ROW_H]);
  expect(after.get("g:gateway.networking.k8s.io/Gateway/infra/edge")).toEqual(
    before.get("g:gateway.networking.k8s.io/Gateway/infra/edge"),
  );
});
