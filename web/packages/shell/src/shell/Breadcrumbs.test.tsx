import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes } from "react-router-dom";
import type { LinkedRef } from "../api/types";
import { breadcrumbsCopy } from "../copy/navigation";
import { pseudoRules, renderWithTheme } from "../test/render";
import { Breadcrumbs } from "./Breadcrumbs";
import { ObjectHeader } from "./ObjectHeader";
import { type Crumb, objectCrumb, viaParam } from "./trail";

const ID = "kgw.kgateway";
const GW = "gateway.networking.k8s.io";
const ROUTE = `/i/${ID}/routes/${GW}/HTTPRoute/shop/store`;
const gateway = objectCrumb({
  group: GW,
  kind: "Gateway",
  namespace: "infra",
  name: "edge",
});
const nth = (i: number) =>
  objectCrumb({
    group: GW,
    kind: "HTTPRoute",
    namespace: "shop",
    name: `r${i}`,
  });
const at = (trail: Crumb[] | string) =>
  `${ROUTE}?${new URLSearchParams({ via: typeof trail === "string" ? trail : viaParam(trail, ID) })}`;

beforeEach(() =>
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("{}", { status: 404 })),
  ),
);
afterEach(() => vi.unstubAllGlobals());

function trail() {
  return within(screen.getByRole("navigation", { name: breadcrumbsCopy.label }));
}

test("hierarchy without via", () => {
  renderWithTheme(<Breadcrumbs />, { path: ROUTE });
  const t = trail();
  expect(t.getByRole("link", { name: ID })).toHaveAttribute("href", `/i/${ID}`);
  expect(t.getByRole("link", { name: "Routes" })).toHaveAttribute("href", `/i/${ID}/routes`);
  expect(t.getByText("HTTPRoute shop/store")).toHaveAttribute("aria-current", "page");
  expect(t.getAllByRole("link")).toHaveLength(2);
});

test("a route reached from a Gateway shows the path taken", () => {
  renderWithTheme(<Breadcrumbs />, { path: at([gateway]) });
  const t = trail();
  // The Gateway crumb links back to its page with its own (empty) trail.
  expect(t.getByRole("link", { name: "Gateway infra/edge" })).toHaveAttribute("href", `/i/${ID}/gateways/infra/edge`);
  expect(t.queryByRole("link", { name: "Routes" })).toBeNull();
  expect(t.getByText("HTTPRoute shop/store")).toHaveAttribute("aria-current", "page");
});

test("a list crumb returns to the list with its filters", () => {
  const list: Crumb = {
    kind: "list",
    path: "routes",
    search: "state=error&q=shop",
  };
  renderWithTheme(<Breadcrumbs />, { path: at([list, gateway]) });
  const t = trail();
  expect(t.getByRole("link", { name: "Routes" })).toHaveAttribute("href", `/i/${ID}/routes?state=error&q=shop`);
  const back = t.getByRole("link", { name: "Gateway infra/edge" }).getAttribute("href") ?? "";
  expect(new URL(back, "http://ui").searchParams.get("via")).toBe(viaParam([list], ID));
});

test("collapse past 6 into an ellipsis menu", async () => {
  const user = userEvent.setup();
  const seven = [1, 2, 3, 4, 5, 6, 7].map(nth);
  renderWithTheme(<Breadcrumbs />, { path: at(seven) });
  const t = trail();
  // Five crumbs stay visible after the "…" crumb, which holds the two oldest.
  expect(t.queryByRole("link", { name: "HTTPRoute shop/r1" })).toBeNull();
  expect(t.getByRole("link", { name: "HTTPRoute shop/r3" })).toBeInTheDocument();
  const more = t.getByRole("button", { name: breadcrumbsCopy.more(2) });
  expect(more).toHaveAttribute("aria-expanded", "false");
  for (const pseudo of [":hover", ":active", ":focus-visible"]) expect(pseudoRules(more, pseudo)).not.toEqual([]);

  more.focus();
  await user.keyboard("{Enter}");
  const menu = screen.getByRole("menu", { name: breadcrumbsCopy.menu });
  const items = within(menu).getAllByRole("menuitem");
  expect(items.map((i) => i.textContent)).toEqual(["HTTPRoute shop/r1", "HTTPRoute shop/r2"]);
  expect(items[0]).toHaveFocus();
  // r2 links back with r1 as its trail.
  expect(new URL(items[1]?.getAttribute("href") ?? "", "http://ui").searchParams.get("via")).toBe(
    viaParam([nth(1)], ID),
  );
  await user.keyboard("{ArrowDown}");
  expect(items[1]).toHaveFocus();
  await user.keyboard("{ArrowDown}");
  expect(items[0]).toHaveFocus();
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).toBeNull();
  expect(more).toHaveFocus();
  await user.keyboard(" ");
  expect(screen.getByRole("menu")).toBeInTheDocument();
  // Space on a link item follows it, as Enter does.
  const first = within(screen.getByRole("menu")).getAllByRole("menuitem")[0] as HTMLElement;
  let clicked = false;
  first.addEventListener("click", (e) => {
    clicked = true;
    e.preventDefault();
  });
  await user.keyboard(" ");
  expect(clicked).toBe(true);
});

test("a menu item without a page is inert: no pointer and no hover or press tint", async () => {
  const user = userEvent.setup();
  const trail = ["not-an-entry", ...[1, 2, 3, 4, 5, 6].map((i) => viaParam([nth(i)], ID))].join(",");
  renderWithTheme(<Breadcrumbs />, { path: at(trail) });
  await user.click(screen.getByRole("button", { name: breadcrumbsCopy.more(2) }));
  const inert = within(screen.getByRole("menu")).getByText("not-an-entry");
  expect(inert).toHaveAttribute("aria-disabled", "true");
  const css = [...document.querySelectorAll("style")].map((s) => s.textContent ?? "").join("");
  expect(css).toMatch(/\[role="menuitem"\]\[aria-disabled="true"\]\{cursor:default;\}/);
  expect(css).toMatch(/\[aria-disabled="true"\]:active\{background:transparent;\}/);
});

test("malformed or unresolvable entry is text and the page loads", () => {
  const raw = [
    viaParam([gateway], ID),
    "not-an-entry",
    "javascript:alert(1)",
    `${ID}/${GW}/HTTPRoute/shop/Bad_Name`,
  ].join(",");
  renderWithTheme(
    <Routes>
      <Route
        path="/i/:id/routes/:group/:kind/:ns/:name"
        element={
          <ObjectHeader kind={{ group: GW, kind: "HTTPRoute", label: "HTTPRoute" }} name="store" namespace="shop" />
        }
      />
    </Routes>,
    { path: at(raw) },
  );
  expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("store");
  const t = trail();
  expect(t.getByRole("link", { name: "Gateway infra/edge" })).toBeInTheDocument();
  for (const text of ["not-an-entry", "javascript:alert(1)"]) {
    const el = t.getByText(text);
    expect(el.closest("a")).toBeNull();
  }
  // Install and Gateway are the only links; nothing from the raw value becomes an href.
  expect(t.getAllByRole("link").map((a) => a.getAttribute("href")?.startsWith(`/i/${ID}`))).toEqual([true, true]);
});

test("a via entry for another install is dropped", () => {
  const raw = `${viaParam([nth(1)], "other")},${viaParam([gateway], ID)}`;
  renderWithTheme(<Breadcrumbs />, { path: at(raw) });
  const t = trail();
  expect(t.queryByText("HTTPRoute shop/r1")).toBeNull();
  expect(t.getByRole("link", { name: "Gateway infra/edge" })).toBeInTheDocument();
});

test("a crumb for a hidden object shows the restricted label", () => {
  const hidden: LinkedRef = {
    group: GW,
    kind: "Gateway",
    namespace: "infra",
    name: "edge",
    link: "none",
  };
  renderWithTheme(<Breadcrumbs refs={[hidden]} />, {
    path: at([gateway, nth(1)]),
  });
  const t = trail();
  expect(t.queryByText(/edge/)).toBeNull();
  expect(t.getByText(breadcrumbsCopy.hidden).closest("a")).toBeNull();
  // A page ref that the server links opens where its link says.
  expect(t.getByRole("link", { name: "HTTPRoute shop/r1" })).toBeInTheDocument();
});

test("a 4 KiB via is cut to the last 6 valid entries", () => {
  const entries: string[] = [];
  for (let i = 0; i < 300; i++) entries.push(viaParam([nth(i)], ID), "junk");
  renderWithTheme(<Breadcrumbs />, { path: at(entries.join(",")) });
  const t = trail();
  expect(t.queryByText("junk")).toBeNull();
  // Six entries fit without the "…" crumb.
  expect(t.queryByRole("button")).toBeNull();
  expect(
    t
      .getAllByRole("link")
      .slice(1)
      .map((a) => a.textContent),
  ).toEqual([294, 295, 296, 297, 298, 299].map((i) => `HTTPRoute shop/r${i}`));
});
