import { fireEvent, screen, within } from "@testing-library/react";
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

it("lists Processes after Findings", () => {
  renderWithTheme(<InstallNav installId="kgw.kgateway" paths={[...paths, "processes"]} />, { path: "/i/kgw.kgateway" });
  const links = within(nav()).getAllByRole("link");
  expect(links.map((a) => [a.textContent, a.getAttribute("href")]).slice(-2)).toEqual([
    ["Findings", "/i/kgw.kgateway/findings"],
    ["Processes", "/i/kgw.kgateway/processes"],
  ]);
});

it("lists Traffic after Routes", () => {
  renderWithTheme(<InstallNav installId="kgw.kgateway" paths={["", "routes", "traffic", "listenersets"]} />, {
    path: "/i/kgw.kgateway",
  });
  const links = within(nav()).getAllByRole("link");
  expect(links.map((a) => a.textContent)).toEqual(["Overview", "Routes", "Traffic", "ListenerSets"]);
});

it("scrolls the active entry into view on route change", () => {
  const scroll = vi.fn();
  Element.prototype.scrollIntoView = scroll;
  renderWithTheme(<InstallNav installId="kgw.kgateway" paths={[...paths, "processes"]} />, {
    path: "/i/kgw.kgateway/processes",
  });
  expect(scroll).toHaveBeenCalledWith({ inline: "nearest", block: "nearest" });
  expect(scroll.mock.contexts.at(-1)).toBe(within(nav()).getByRole("link", { name: "Processes" }));
  delete (Element.prototype as Partial<Element>).scrollIntoView;
});

it("fades the edge that has entries off-screen", () => {
  const box = { scrollWidth: 400, clientWidth: 200 };
  const sw = vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockImplementation(() => box.scrollWidth);
  const cw = vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(() => box.clientWidth);
  renderWithTheme(<InstallNav installId="kgw.kgateway" paths={paths} />, { path: "/i/kgw.kgateway" });
  const n = nav();
  expect(n).toHaveAttribute("data-fade", "end");
  n.scrollLeft = 100;
  fireEvent.scroll(n);
  expect(n).toHaveAttribute("data-fade", "both");
  n.scrollLeft = 200;
  fireEvent.scroll(n);
  expect(n).toHaveAttribute("data-fade", "start");
  box.scrollWidth = 200;
  n.scrollLeft = 0;
  fireEvent.scroll(n);
  expect(n).not.toHaveAttribute("data-fade");
  // The fade is a mask, so it works on either theme's header colour.
  expect(pseudoRules(n, '[data-fade="end"]').join()).toContain("mask-image");
  sw.mockRestore();
  cw.mockRestore();
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
