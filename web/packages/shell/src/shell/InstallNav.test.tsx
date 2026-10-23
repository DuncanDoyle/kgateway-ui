import { screen, within } from "@testing-library/react";
import { pseudoRules, renderWithTheme } from "../test/render";
import { controlRoomDark, controlRoomLight } from "../theme/themes";
import { HEADER_HOVER_PCT, HEADER_PRESS_PCT, headerTint } from "./headerTint";
import { InstallNav, installNavEntries } from "./InstallNav";

const paths = ["", "findings"];
const nav = () => screen.getByRole("navigation", { name: "Install" });

it("links each entry with a route under the install", () => {
  renderWithTheme(<InstallNav installId="kgw.kgateway" paths={paths} />, { path: "/i/kgw.kgateway" });
  const links = within(nav()).getAllByRole("link");
  expect(links.map((a) => [a.textContent, a.getAttribute("href")])).toEqual([
    ["Overview", "/i/kgw.kgateway"],
    ["Findings", "/i/kgw.kgateway/findings"],
  ]);
});

it("hides entries with no route", () => {
  const entries = [...installNavEntries, { label: "Gateways", to: "gateways" }];
  renderWithTheme(<InstallNav installId="kgw.kgateway" paths={paths} entries={entries} />, { path: "/i/kgw.kgateway" });
  expect(within(nav()).queryByRole("link", { name: "Gateways" })).toBeNull();
  expect(within(nav()).getAllByRole("link")).toHaveLength(2);
});

it.each([
  ["/i/kgw.kgateway", "Overview"],
  ["/i/kgw.kgateway/findings", "Findings"],
])("marks the active entry on %s", (path, active) => {
  renderWithTheme(<InstallNav installId="kgw.kgateway" paths={paths} />, { path });
  const current = within(nav())
    .getAllByRole("link")
    .filter((a) => a.getAttribute("aria-current") === "page");
  expect(current.map((a) => a.textContent)).toEqual([active]);
});

describe.each([controlRoomDark, controlRoomLight])("in the $name theme", (t) => {
  it("has hover, active, focus-visible and current styles", () => {
    renderWithTheme(<InstallNav installId="kgw.kgateway" paths={paths} />, { path: "/i/kgw.kgateway", themeId: t.id });
    const n = nav();
    const rules = (p: string) => pseudoRules(n, p).join();
    expect(rules(":hover")).toContain(headerTint(t, HEADER_HOVER_PCT));
    expect(rules(":active")).toContain(headerTint(t, HEADER_PRESS_PCT));
    expect(rules(":focus-visible")).toContain(t.color.focus);
    expect(rules('[aria-current="page"]')).toContain(t.color.headerText);
  });
});
