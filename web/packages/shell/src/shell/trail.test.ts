import {
  appendVia,
  type Crumb,
  collapse,
  crumbRef,
  kindList,
  leavingVia,
  objectCrumb,
  parseVia,
  viaParam,
} from "./trail";

const ID = "kgw.kgateway";
const GW = "gateway.networking.k8s.io";
const gateway = objectCrumb({
  group: GW,
  kind: "Gateway",
  namespace: "infra",
  name: "edge",
});
const route = objectCrumb({
  group: GW,
  kind: "HTTPRoute",
  namespace: "shop",
  name: "store",
});
const nth = (i: number) =>
  objectCrumb({
    group: GW,
    kind: "HTTPRoute",
    namespace: "shop",
    name: `r${i}`,
  });
const search = (trail: Crumb[], id = ID) => `?${new URLSearchParams({ via: viaParam(trail, id) })}`;

test("append", () => {
  expect(appendVia([], gateway)).toEqual([gateway]);
  expect(appendVia([gateway], route)).toEqual([gateway, route]);
  // The value round-trips through the URL.
  expect(parseVia(search([gateway, route]), ID)).toEqual([gateway, route]);
  expect(crumbRef(route)).toEqual({
    group: GW,
    kind: "HTTPRoute",
    namespace: "shop",
    name: "store",
  });
  // A link from a route reached from a Gateway carries both pages.
  const via = leavingVia(`/i/${ID}/routes/${GW}/HTTPRoute/shop/store`, search([gateway]), ID);
  expect(parseVia(`?${new URLSearchParams({ via: via ?? "" })}`, ID)).toEqual([gateway, route]);
  // A list page starts the trail with its filters; the page's own cursor and via are not part of them.
  const list = leavingVia(`/i/${ID}/routes`, "?state=error&q=shop", ID);
  expect(parseVia(`?${new URLSearchParams({ via: list ?? "" })}`, ID)).toEqual([
    { kind: "list", path: "routes", search: "state=error&q=shop" },
  ]);
  // Links from another install's page, or from the install page, carry no trail.
  expect(leavingVia(`/i/other/routes`, "", ID)).toBeUndefined();
  expect(leavingVia(`/i/${ID}`, "", ID)).toBeUndefined();
});

test("cut back on a cycle", () => {
  // route ↔ Gateway three times: each page's path is appendVia(its via, itself).
  let trail: Crumb[] = [];
  for (let i = 0; i < 3; i++) {
    trail = appendVia(trail, gateway);
    trail = appendVia(trail, route);
  }
  expect(trail).toEqual([gateway, route]);
  expect(appendVia([gateway, route, nth(1)], route)).toEqual([gateway, route]);
});

test("collapse past 6 into an ellipsis menu", () => {
  const six = [1, 2, 3, 4, 5, 6].map(nth);
  expect(collapse(six)).toEqual({ hidden: [], shown: six });
  const nine = [1, 2, 3, 4, 5, 6, 7, 8, 9].map(nth);
  // The "…" crumb takes one of the 6 places.
  expect(collapse(nine)).toEqual({
    hidden: nine.slice(0, 4),
    shown: nine.slice(4),
  });
});

test("a via entry for another install is dropped", () => {
  const mixed = `${viaParam([gateway], "other")},${viaParam([route], ID)}`;
  expect(parseVia(`?${new URLSearchParams({ via: mixed })}`, ID)).toEqual([route]);
});

test("malformed or unresolvable entry is text and the page loads", () => {
  const raw = [
    viaParam([gateway], ID),
    "not-an-entry",
    `${encodeURIComponent(ID)}/secrets`,
    `${encodeURIComponent(ID)}/${GW}/HTTPRoute/shop/Bad_Name`,
    "%E0%A4%A",
  ].join(",");
  const trail = parseVia(`?${new URLSearchParams({ via: raw })}`, ID);
  expect(trail).toHaveLength(5);
  expect(trail[0]).toEqual(gateway);
  // None of the others resolves to an object: they render as text.
  expect(trail.slice(1).map(crumbRef)).toEqual([undefined, undefined, undefined, undefined]);
  // A list entry later than first, or javascript: and absolute URLs, never become crumbs with a page.
  const odd = parseVia(
    `?${new URLSearchParams({ via: [viaParam([gateway], ID), `${encodeURIComponent(ID)}/routes`, "javascript:alert(1)", "//evil.example/x", "https://evil.example"].join(",") })}`,
    ID,
  );
  expect(odd[0]).toEqual(gateway);
  expect(odd.filter((c) => c.kind === "list")).toEqual([]);
  expect(odd.slice(1).every((c) => crumbRef(c) === undefined)).toBe(true);
  // Leaving the page never writes text crumbs back: an encoded comma stays one dropped entry, not junk.
  const comma = [viaParam([gateway], ID), `${encodeURIComponent(ID)}/${GW}/Gateway/infra/a%2Cb`, "not-an-entry"].join(
    ",",
  );
  const out = leavingVia(`/i/${ID}/routes/${GW}/HTTPRoute/shop/store`, `?${new URLSearchParams({ via: comma })}`, ID);
  expect(out).toBe(viaParam([gateway, route], ID));
  expect(out).not.toContain("?");
  expect(parseVia(`?${new URLSearchParams({ via: out ?? "" })}`, ID)).toEqual([gateway, route]);
  expect(viaParam([gateway, { kind: "object", path: "?text" }], ID)).toBe(viaParam([gateway], ID));
});

test("a 4 KiB via is cut to the last 6 valid entries", () => {
  const entries: string[] = [];
  for (let i = 0; i < 300; i++) entries.push(viaParam([nth(i)], ID), "junk");
  const raw = entries.join(",");
  expect(raw.length).toBeGreaterThan(4096);
  expect(parseVia(`?${new URLSearchParams({ via: raw })}`, ID)).toEqual([294, 295, 296, 297, 298, 299].map(nth));
  // Over the limit with no separator in the kept tail: nothing is kept.
  expect(parseVia(`?${new URLSearchParams({ via: `${viaParam([nth(1)], ID)}${"x".repeat(5000)}` })}`, ID)).toEqual([]);
  // Writing never produces more than 4 KiB: the oldest entries go first.
  const long = Array.from({ length: 300 }, (_, i) => nth(i));
  const written = viaParam(long, ID);
  expect(written.length).toBeLessThanOrEqual(4096);
  expect(parseVia(`?${new URLSearchParams({ via: written })}`, ID).at(-1)).toEqual(nth(299));
});

it("every route kind's trail starts at the routes list", () => {
  for (const kind of ["HTTPRoute", "GRPCRoute", "TCPRoute", "TLSRoute"])
    expect(kindList({ group: "gateway.networking.k8s.io", kind, namespace: "shop", name: "r" })).toEqual({
      kind: "list",
      path: "routes",
      search: "",
    });
  expect(kindList({ group: "", kind: "Service", namespace: "shop", name: "s" })).toBeUndefined();
});
