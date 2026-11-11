import type { LinkedRef, PathRow } from "../../api/types";
import { buildGraph } from "./graph";
import { nextNode } from "./keyboard";
import { layoutColumns, type Point } from "./layout";

const ref = (kind: string, name: string, namespace = "shop"): LinkedRef => ({
  group: kind === "Service" ? "" : "gateway.networking.k8s.io",
  kind,
  namespace,
  name,
  link: "detail",
});

/** A healthy path row through listener l, routes (root first) and backend b. */
function row(l: string, routes: string[], b: string): PathRow {
  return {
    gateway: ref("Gateway", "edge", "infra"),
    listener: { name: l, protocol: "HTTP", port: 80 },
    hostnames: [],
    routes: routes.map((r) => ref("HTTPRoute", r)),
    delegated: routes.length > 1,
    rule: 0,
    matches: [],
    action: "backend",
    backend: ref("Service", b),
    weight: 1,
    share: 100,
    shareState: "shown",
    mirror: false,
    problem: { state: "healthy", signals: [] },
  };
}

const G = "g:gateway.networking.k8s.io/Gateway/infra/edge";
const R = (n: string) => `r:gateway.networking.k8s.io/HTTPRoute/shop/${n}`;
const B = (n: string) => `b:/Service/shop/${n}`;

// http → a → b1, http → b → b2, https → c → b1.
const g = buildGraph([row("http", ["a"], "b1"), row("http", ["b"], "b2"), row("https", ["c"], "b1")]);

it("left and right follow edges to a connected node only, even when another sits level with it", () => {
  // c and https are level with http's own routes, but not connected to http.
  const at = new Map<string, Point>([
    [G, { x: 0, y: 0 }],
    ["l:http", { x: 340, y: 0 }],
    ["l:https", { x: 340, y: 96 }],
    [R("c"), { x: 680, y: 0 }],
    [R("a"), { x: 680, y: 96 }],
    [R("b"), { x: 680, y: 192 }],
    [B("b1"), { x: 1020, y: 0 }],
    [B("b2"), { x: 1020, y: 96 }],
  ]);
  expect(nextNode(g, "l:http", "ArrowRight", at)).toBe(R("a"));
  expect(nextNode(g, "l:https", "ArrowRight", at)).toBe(R("c"));
  expect(nextNode(g, R("a"), "ArrowLeft", at)).toBe("l:http");
  expect(nextNode(g, R("c"), "ArrowLeft", at)).toBe("l:https");
  // b1 joins a and c: the nearest in height wins.
  expect(nextNode(g, B("b1"), "ArrowLeft", at)).toBe(R("c"));
  expect(nextNode(g, B("b2"), "ArrowLeft", at)).toBe(R("b"));
  // Nothing beyond the ends.
  expect(nextNode(g, G, "ArrowLeft", at)).toBeUndefined();
  expect(nextNode(g, B("b1"), "ArrowRight", at)).toBeUndefined();
});

it("up and down step through the node's own column in layout order, stopping at its ends", () => {
  const at = layoutColumns(g);
  const routes = [R("a"), R("b"), R("c")].sort((x, y) => (at.get(x) as Point).y - (at.get(y) as Point).y);
  expect(nextNode(g, routes[0] as string, "ArrowDown")).toBe(routes[1]);
  expect(nextNode(g, routes[1] as string, "ArrowDown")).toBe(routes[2]);
  expect(nextNode(g, routes[1] as string, "ArrowUp")).toBe(routes[0]);
  expect(nextNode(g, routes[0] as string, "ArrowUp")).toBeUndefined();
  expect(nextNode(g, routes[2] as string, "ArrowDown")).toBeUndefined();
  expect(nextNode(g, G, "ArrowDown")).toBeUndefined();
});

it("an unknown node moves nowhere", () => {
  expect(nextNode(g, "r:missing", "ArrowRight")).toBeUndefined();
});
