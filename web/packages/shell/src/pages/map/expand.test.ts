import type { LinkedRef, PathRow } from "../../api/types";
import { focusSubgraph } from "./focus";
import { buildGraph, type MapGraph, throughNode } from "./graph";
import { nextNode } from "./keyboard";
import { COL_W, changedColumns, INDENT, layoutColumns } from "./layout";

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
const prefix = (value: string) => [{ path: { type: "PathPrefix", value } }] as PathRow["matches"];
const R = (name: string) => `r:gateway.networking.k8s.io/HTTPRoute/shop/${name}`;
const chain = (...names: string[]) => `r:${names.map((n) => R(n).slice(2)).join("~")}`;
const B = (name: string) => `b:/Service/shop/${name}`;
const ids = (g: MapGraph, kind: string) => g.nodes.filter((n) => n.kind === kind).map((n) => n.id);
const ERR = { state: "error", signals: [] } as PathRow["problem"];

/** api has two rules: /a splits 30/70 over svc-a and svc-b, /b goes to svc-a; web goes to svc-c on https. */
const twoRules = () => [
  row("http", ["api"], "svc-a", { matches: prefix("/a"), share: 30 }),
  row("http", ["api"], "svc-b", { matches: prefix("/a"), share: 70 }),
  row("http", ["api"], "svc-a", { rule: 1, matches: prefix("/b"), share: 100 }),
  row("https", ["web"], "svc-c"),
];

it("expanding a two-rule route adds two rule nodes and keeps every other node's position", () => {
  const g1 = buildGraph(twoRules());
  const before = layoutColumns(g1);
  const g2 = buildGraph(twoRules(), new Set([R("api")]));
  expect(ids(g2, "rule")).toEqual([`u:${R("api")}#0`, `u:${R("api")}#1`]);
  expect(g2.nodes.filter((n) => n.kind === "rule").map((n) => n.name)).toEqual(["PathPrefix /a", "PathPrefix /b"]);
  // Rule nodes sit between the route and its backends; each rule's edge carries that rule's share.
  const edge = (s: string, t: string) => g2.edges.find((e) => e.source === s && e.target === t);
  expect(edge(R("api"), B("svc-a"))).toBeUndefined();
  expect(edge(R("api"), `u:${R("api")}#0`)?.share).toBeUndefined();
  expect(edge(`u:${R("api")}#0`, B("svc-a"))?.share).toBe(30);
  expect(edge(`u:${R("api")}#0`, B("svc-b"))?.share).toBe(70);
  expect(edge(`u:${R("api")}#1`, B("svc-a"))?.share).toBe(100);
  expect(g2.nodes.find((n) => n.id === R("api"))?.expanded).toBe(true);

  const changed = changedColumns(g1, g2);
  expect([...changed]).toEqual(["rule"]);
  const after = layoutColumns(g2, changed, before);
  // The rule column's place is always kept, so every other node, backends included, stays where it was.
  for (const n of g1.nodes) expect(after.get(n.id)).toEqual(before.get(n.id));
  for (const id of ids(g2, "rule")) expect(after.get(id)?.x).toBe(3 * COL_W);
  for (const id of ids(g1, "backend")) expect(before.get(id)?.x).toBe(4 * COL_W);

  // Collapse restores the graph and every position.
  const g3 = buildGraph(twoRules());
  const back = layoutColumns(g3, changedColumns(g2, g3), after);
  expect(g3.nodes.map((n) => n.id)).toEqual(g1.nodes.map((n) => n.id));
  expect(g3.edges.map((e) => e.id)).toEqual(g1.edges.map((e) => e.id));
  for (const n of g1.nodes) expect(back.get(n.id)).toEqual(before.get(n.id));
});

const sideSignal = {
  kind: "finding",
  code: "x",
  title: "side breaks",
  ref: ref("HTTPRoute", "side"),
  state: "error",
} as const;
const nested = () => [
  row("http", ["root", "mid", "leaf"], "svc-a"),
  row("http", ["root", "mid"], "svc-b"),
  row("http", ["root", "side"], "svc-c", { problem: { state: "error", signals: [sideSignal] } }),
];

it("delegated routes show at rest, each nested under its own parent", () => {
  const g = buildGraph(nested());
  const leaf = chain("root", "mid", "leaf");
  expect(ids(g, "route")).toEqual([R("root"), chain("root", "mid"), leaf, chain("root", "side")]);
  expect(g.nodes.find((n) => n.id === leaf)?.delegatedUnder).toBe(chain("root", "mid"));
  expect(g.nodes.find((n) => n.id === chain("root", "mid"))).toMatchObject({ delegates: true, ends: true });
  const at = layoutColumns(g);
  const order = ids(g, "route").sort((a, b) => (at.get(a)?.y ?? 0) - (at.get(b)?.y ?? 0));
  // leaf sits right under mid, one indent deeper; side stays under root.
  expect(order.indexOf(leaf)).toBe(order.indexOf(chain("root", "mid")) + 1);
  expect(at.get(leaf)?.x).toBe((at.get(R("root"))?.x ?? 0) + 2 * INDENT);
  expect(at.get(chain("root", "side"))?.x).toBe((at.get(R("root"))?.x ?? 0) + INDENT);
  // Keyboard navigation reaches the nested child.
  expect(nextNode(g, B("svc-a"), "ArrowLeft", at)).toBe(leaf);
});

it("a collapsed route hides its delegated routes, counts them, and keeps their problems under their names", () => {
  const g = buildGraph(nested(), new Set(), { collapsed: new Set([R("root")]) });
  expect(ids(g, "route")).toEqual([R("root")]);
  const root = g.nodes.find((n) => n.id === R("root"));
  expect(root).toMatchObject({ collapsed: true, hiddenRoutes: 3, state: "error", problems: [] });
  expect(root?.hiddenProblems).toEqual([{ id: chain("root", "side"), route: "shop/side", signals: [sideSignal] }]);
  expect(g.edges.map((e) => e.id)).toContain(`${R("root")}>${B("svc-a")}`);
  // Collapsing mid only hides leaf; leaf's row ends at mid.
  const mid = buildGraph(nested(), new Set(), { collapsed: new Set([chain("root", "mid")]) });
  expect(ids(mid, "route")).toEqual([R("root"), chain("root", "mid"), chain("root", "side")]);
  expect(mid.nodes.find((n) => n.id === chain("root", "mid"))?.hiddenRoutes).toBe(1);
  expect(mid.edges.map((e) => e.id)).toContain(`${chain("root", "mid")}>${B("svc-a")}`);
});

it("an edge from a collapsed route keys shares by the hidden routes' rules: 30 % and 80 % show no share", () => {
  const rows = [
    row("http", ["root", "c1"], "svc-a", { share: 30 }),
    row("http", ["root", "c1"], "svc-b", { share: 70 }),
    row("http", ["root", "c2"], "svc-a", { share: 80 }),
    row("http", ["root", "c2"], "svc-c", { share: 20 }),
  ];
  const g = buildGraph(rows, new Set(), { collapsed: new Set([R("root")]) });
  const e = g.edges.find((x) => x.id === `${R("root")}>${B("svc-a")}`);
  expect(e?.rows).toEqual([0, 2]);
  expect(e?.share).toBeUndefined();
  // One hidden route's rule alone still shows its share.
  expect(g.edges.find((x) => x.id === `${R("root")}>${B("svc-b")}`)?.share).toBe(70);
});

it("a child delegated from two roots appears under each root it is reached from", () => {
  const rows = [row("http", ["p1", "child"], "svc-a"), row("https", ["p2", "child"], "svc-a")];
  const g = buildGraph(rows);
  expect(ids(g, "route")).toEqual([R("p1"), chain("p1", "child"), R("p2"), chain("p2", "child")]);
  expect(g.nodes.find((n) => n.id === chain("p2", "child"))?.delegatedUnder).toBe(R("p2"));
  expect(g.nodes.find((n) => n.id === chain("p1", "child"))?.rows).toEqual([0]);
  // A route attached directly as well is its own root, apart from its delegated copies.
  const direct = buildGraph([...rows, row("http", ["child"], "svc-a")]);
  expect(direct.nodes.find((n) => n.id === R("child"))?.delegatedUnder).toBeUndefined();
});

it("expanding a delegated leaf opens its rules between it and its backends", () => {
  const g = buildGraph(
    [row("http", ["root", "leaf"], "svc-a", { matches: prefix("/x") })],
    new Set([chain("root", "leaf")]),
  );
  const rule = `u:${chain("root", "leaf")}#0`;
  expect(g.nodes.find((n) => n.id === rule)).toMatchObject({ kind: "rule", name: "PathPrefix /x", rule: 0 });
  expect(g.edges.map((e) => e.id)).toEqual(
    expect.arrayContaining([`${chain("root", "leaf")}>${rule}`, `${rule}>${B("svc-a")}`]),
  );
});

it("focus on a backend keeps exactly the Gateways, listeners and routes on its rows", () => {
  const rows = [
    row("http", ["r1"], "svc-a"),
    row("https", ["r2"], "svc-a"),
    row("http", ["r3"], "svc-b"),
    row("grpc", ["r4"], "svc-c"),
  ];
  const g = buildGraph(rows);
  const f = focusSubgraph(g, B("svc-a"));
  expect(ids(f, "gateway")).toEqual(["g:gateway.networking.k8s.io/Gateway/infra/edge"]);
  expect(ids(f, "listener")).toEqual(["l:http", "l:https"]);
  expect(ids(f, "route")).toEqual([R("r1"), R("r2")]);
  expect(ids(f, "backend")).toEqual([B("svc-a")]);
  // Edges and node rows keep only the focused paths.
  expect(f.edges.every((e) => e.rows.every((i) => i === 0 || i === 1))).toBe(true);
  expect(f.nodes.find((n) => n.id === "l:http")?.rows).toEqual([0]);
  // Focus on a route keeps its upstream and downstream.
  const r = focusSubgraph(g, R("r3"));
  expect(r.nodes.map((n) => n.id)).toEqual([
    "g:gateway.networking.k8s.io/Gateway/infra/edge",
    "l:http",
    R("r3"),
    B("svc-b"),
  ]);
  // An id not on the map leaves the map whole.
  expect(focusSubgraph(g, "r:gone")).toBe(g);
});

it("under a state filter an edge is never broken, though every loaded row fails; it keeps its error state", () => {
  const rows = [row("http", ["r1"], "svc-a", { problem: ERR }), row("http", ["r2"], "svc-b", { problem: ERR })];
  const all = buildGraph(rows);
  expect(all.edges.every((e) => e.broken)).toBe(true);
  const filtered = buildGraph(rows, new Set(), { stateFiltered: true });
  expect(filtered.edges.every((e) => !e.broken && e.state === "error")).toBe(true);
  // Lit edges and focus follow the same rule.
  expect([...throughNode(filtered, R("r1")).edges.values()].every((e) => !e.broken && e.state === "error")).toBe(true);
  expect(focusSubgraph(filtered, R("r1")).edges.every((e) => !e.broken)).toBe(true);
  expect(focusSubgraph(all, R("r1")).edges.every((e) => e.broken)).toBe(true);
});

it("a rule node's state is the worst of its rows, though no signal names it", () => {
  const rows = [
    row("http", ["api"], "svc-a", { matches: prefix("/a"), problem: ERR, shareState: "failing" }),
    row("http", ["api"], "svc-b", { matches: prefix("/a") }),
    row("http", ["api"], "svc-a", { rule: 1, matches: prefix("/b") }),
  ];
  const g = buildGraph(rows, new Set([R("api")]));
  expect(g.nodes.find((n) => n.id === `u:${R("api")}#0`)?.state).toBe("error");
  expect(g.nodes.find((n) => n.id === `u:${R("api")}#1`)?.state).toBe("healthy");
});

it("two hidden routes of one name are listed apart, by id and by the routes above them", () => {
  const sig = (name: string) =>
    ({ kind: "finding", code: "x", title: "breaks", ref: ref("HTTPRoute", name), state: "error" }) as const;
  const rows = [
    row("http", ["root", "a", "x"], "svc-a", { problem: { state: "error", signals: [sig("x")] } }),
    row("http", ["root", "b", "x"], "svc-b", { problem: { state: "error", signals: [sig("x")] } }),
  ];
  const g = buildGraph(rows, new Set(), { collapsed: new Set([R("root")]) });
  const hidden = g.nodes.find((n) => n.id === R("root"))?.hiddenProblems;
  expect(hidden?.map((h) => [h.id, h.route])).toEqual([
    [chain("root", "a", "x"), "shop/a › shop/x"],
    [chain("root", "b", "x"), "shop/b › shop/x"],
  ]);
});

it("a route whose backend has an error or warning finding is at least warning, as on its page (D-082)", () => {
  const on = (b: string, state: "error" | "warning" | "unknown") =>
    ({ state, signals: [{ kind: "finding", code: "KGWUI-023", ref: ref("Service", b), state }] }) as PathRow["problem"];
  const own = { kind: "finding", code: "KGWUI-013", ref: ref("HTTPRoute", "bad"), state: "error" } as const;
  const rows = [
    row("http", ["api"], "idle", { problem: on("idle", "error") }),
    row("http", ["slow"], "busy", { problem: on("busy", "warning") }),
    row("http", ["copy"], "shadow", { problem: on("shadow", "error"), mirror: true, shareState: "mirror" }),
    row("http", ["wait"], "new", { problem: on("new", "unknown") }),
    row("http", ["bad"], "idle", { problem: { state: "error", signals: [own, ...on("idle", "error").signals] } }),
  ];
  const g = buildGraph(rows);
  const node = (id: string) => g.nodes.find((n) => n.id === id);
  // An error backend makes its route warning, not error; the backend keeps its own state.
  expect([node(R("api"))?.state, node(B("idle"))?.state]).toEqual(["warning", "error"]);
  expect(node(R("api"))?.backendProblems?.map((s) => s.code)).toEqual(["KGWUI-023"]);
  expect(node(R("api"))?.problems).toEqual([]);
  expect(node(R("slow"))?.state).toBe("warning");
  // A mirror target and an unknown backend do not count; a route's own error still shows error.
  expect([node(R("copy"))?.state, node(R("wait"))?.state, node(R("bad"))?.state]).toEqual([
    "healthy",
    "healthy",
    "error",
  ]);
});

it("a hidden route's backend problem raises its collapsed parent and is listed under the hidden route", () => {
  const sig = { kind: "finding", code: "KGWUI-023", ref: ref("Service", "idle"), state: "error" } as const;
  const rows = [row("http", ["root", "child"], "idle", { problem: { state: "error", signals: [sig] } })];
  const open = buildGraph(rows);
  // Expanded, the child route is warning and the delegating root is not (D-082).
  expect(open.nodes.find((n) => n.id === chain("root", "child"))?.state).toBe("warning");
  expect(open.nodes.find((n) => n.id === R("root"))?.state).toBe("healthy");
  const shut = buildGraph(rows, new Set(), { collapsed: new Set([R("root")]) });
  const root = shut.nodes.find((n) => n.id === R("root"));
  expect(root?.state).toBe("warning");
  expect(root?.backendProblems).toBeUndefined();
  expect(root?.hiddenProblems?.map((h) => [h.id, h.signals.map((s) => s.code)])).toEqual([
    [chain("root", "child"), ["KGWUI-023"]],
  ]);
});
