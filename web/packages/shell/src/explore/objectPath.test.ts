import type { LinkedRef } from "../api/types";
import { objectPath } from "./objectPath";

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
