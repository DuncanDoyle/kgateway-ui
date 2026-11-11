import type { LinkedRef } from "../api/types";
import { linkTarget, objectPath, withVia } from "./objectPath";

const gw = "gateway.networking.k8s.io";
const ref: LinkedRef = { group: gw, kind: "HTTPRoute", namespace: "shop", name: "x", link: "detail" };

test("objectPath follows link only", () => {
  expect(objectPath("a.b", { group: gw, kind: "HTTPRoute", namespace: "shop", name: "x", link: "detail" })).toBe(
    "/i/a.b/routes/gateway.networking.k8s.io/HTTPRoute/shop/x",
  );
  expect(objectPath("a.b", { ...ref, kind: "GRPCRoute", link: "yaml" })).toBe(
    "/i/a.b/r/gateway.networking.k8s.io/GRPCRoute/shop/x",
  );
  expect(objectPath("a.b", { ...ref, link: "none" })).toBeUndefined();
});

test("detail links open the page of the ref's kind", () => {
  expect(objectPath("a.b", { ...ref, kind: "Gateway", namespace: "infra", name: "edge" })).toBe(
    "/i/a.b/gateways/infra/edge",
  );
  expect(objectPath("a.b", { ...ref, kind: "ListenerSet", namespace: "infra", name: "ls1" })).toBe(
    "/i/a.b/listenersets/gateway.networking.k8s.io/ListenerSet/infra/ls1",
  );
  expect(
    objectPath("a.b", {
      group: "gateway.networking.x-k8s.io",
      kind: "XListenerSet",
      namespace: "n",
      name: "e",
      link: "detail",
    }),
  ).toBe("/i/a.b/listenersets/gateway.networking.x-k8s.io/XListenerSet/n/e");
  for (const kind of ["GRPCRoute", "TCPRoute", "TLSRoute"]) {
    expect(objectPath("a.b", { ...ref, kind })).toBe(`/i/a.b/routes/gateway.networking.k8s.io/${kind}/shop/x`);
  }
});

test("a downstream ListenerSet kind with a detail link opens the ListenerSet page", () => {
  expect(objectPath("a.b", { group: "acme.io", kind: "EdgeSet", namespace: "n", name: "e", link: "detail" })).toBe(
    "/i/a.b/listenersets/acme.io/EdgeSet/n/e",
  );
});

test("the install ID and names are encoded, never split", () => {
  expect(objectPath("a/b%c", { ...ref, name: "a b" })).toBe(
    "/i/a%2Fb%25c/routes/gateway.networking.k8s.io/HTTPRoute/shop/a%20b",
  );
  // The YAML view keeps its "-" for an empty group or namespace.
  expect(objectPath("a.b", { group: "", kind: "Namespace", name: "shop", link: "yaml" })).toBe(
    "/i/a.b/r/-/Namespace/-/shop",
  );
});

test("linkTarget adds the trail and params to where the link opens", () => {
  const to = linkTarget("a.b", { ...ref, kind: "Gateway", namespace: "infra", name: "edge" }, "a.b/x", {
    listener: "http",
  });
  expect(to).toBe("/i/a.b/gateways/infra/edge?listener=http&via=a.b%2Fx");
  expect(linkTarget("a.b", ref)).toBe("/i/a.b/routes/gateway.networking.k8s.io/HTTPRoute/shop/x");
  expect(linkTarget("a.b", { ...ref, link: "none" }, "a.b/x")).toBeUndefined();
  expect(withVia("/i/a.b/routes?q=1", "v")).toBe("/i/a.b/routes?q=1&via=v");
});
