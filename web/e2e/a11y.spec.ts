import AxeBuilder from "@axe-core/playwright";
import { expect, type Locator, type Page, test } from "@playwright/test";
import { backendsCopy } from "../packages/shell/src/copy/backends";
import { gatewayCopy, listCopy, navCopy, processesCopy } from "../packages/shell/src/copy/explorer";
import { mapCopy } from "../packages/shell/src/copy/map";
import { trafficCopy } from "../packages/shell/src/copy/traffic";
import { THEME_STORAGE_KEY } from "../packages/shell/src/theme/themeStorage";

// axe scans of the main pages in each theme. Only serious and critical violations fail; the rest are attached.
const install = "/i/kgw-a.kgateway";
const themes = [
  { id: "control-room-dark", name: "Control room", mode: "dark" },
  { id: "control-room-light", name: "Control room light", mode: "light" },
] as const;
type Theme = (typeof themes)[number];

/** Stores the theme as a saved preference, matches the OS scheme to it, and opens the install page. */
async function openInstall(page: Page, theme: Theme) {
  await page.emulateMedia({ colorScheme: theme.mode });
  await page.addInitScript(([key, id]) => localStorage.setItem(key, id), [THEME_STORAGE_KEY, theme.id] as const);
  await page.goto(install);
  await expect(page.getByRole("button", { name: `Theme: ${theme.name}`, exact: true })).toBeVisible();
  await expect(page.locator("html")).toHaveCSS("color-scheme", theme.mode);
}

const nav = (page: Page, name: string) =>
  page.getByRole("navigation", { name: navCopy.label }).getByRole("link", { name, exact: true }).click();

/** The table's data rows once it is no longer loading. */
async function dataRows(table: Locator): Promise<Locator> {
  await expect(table).toHaveAttribute("aria-busy", "false");
  return table.locator("tbody > tr[data-row]");
}

/** Waits until an explorer detail page has replaced its skeleton with data. */
async function detailLoaded(page: Page) {
  await expect(page.getByTestId("detail-skeleton")).toHaveCount(0);
  await expect(page.getByTestId("detail-header").getByTestId(/^health-icon-/)).toBeVisible();
}

async function openGateway(page: Page) {
  await nav(page, navCopy.gateways);
  const table = page.getByRole("table", { name: listCopy.gateways.title });
  await (await dataRows(table)).getByRole("link", { name: "infra/edge", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${install}/gateways/infra/edge(\\?|$)`));
  await detailLoaded(page);
}

/** Each page is reached from the install page by clicking, then waits for its data. */
const pages: { name: string; open: (page: Page) => Promise<void> }[] = [
  {
    name: "install overview",
    open: async (page) => {
      await expect(page.getByTestId("health-strip")).toBeVisible();
      await expect(page.getByRole("table", { name: "Gateways" })).toContainText("edge");
    },
  },
  {
    name: "Gateways list",
    open: async (page) => {
      await nav(page, navCopy.gateways);
      await expect((await dataRows(page.getByRole("table", { name: listCopy.gateways.title }))).first()).toBeVisible();
    },
  },
  { name: "Gateway detail", open: openGateway },
  {
    name: "Gateway map",
    open: async (page) => {
      await openGateway(page);
      await page.getByRole("tab", { name: mapCopy.tab }).click();
      await expect(page).toHaveURL(/tab=map/);
      await expect(page.getByTestId("gateway-map").getByTestId("map-node").first()).toBeVisible();
    },
  },
  {
    name: "Routes list",
    open: async (page) => {
      await nav(page, navCopy.routes);
      await expect((await dataRows(page.getByRole("table", { name: listCopy.routes.title }))).first()).toBeVisible();
    },
  },
  {
    name: "route detail",
    open: async (page) => {
      // The Routes list also links shop/store as a parent; the attached-routes table lists it once.
      await openGateway(page);
      const attached = page.getByRole("table", { name: gatewayCopy.attachedRoutes, exact: true });
      await (await dataRows(attached)).getByRole("link", { name: "shop/store", exact: true }).click();
      await expect(page).toHaveURL(/\/HTTPRoute\/shop\/store(\?|$)/);
      await detailLoaded(page);
    },
  },
  {
    name: "Traffic",
    open: async (page) => {
      await nav(page, trafficCopy.title);
      await expect((await dataRows(page.getByRole("table", { name: trafficCopy.table }))).first()).toBeVisible();
    },
  },
  {
    name: "Backends list",
    open: async (page) => {
      await nav(page, backendsCopy.title);
      await expect((await dataRows(page.getByRole("table", { name: backendsCopy.title }))).first()).toBeVisible();
    },
  },
  {
    name: "backend detail",
    open: async (page) => {
      await nav(page, backendsCopy.title);
      const rows = await dataRows(page.getByRole("table", { name: backendsCopy.title }));
      await rows.first().locator('td[data-col="kind"]').click();
      await expect(page).toHaveURL(new RegExp(`${install}/backends/[^/]+/[^/]+/[^/]+/[^/?]+`));
      await expect(page.getByTestId("status-line")).toBeVisible();
    },
  },
  {
    name: "Findings",
    open: async (page) => {
      await nav(page, navCopy.findings);
      await expect((await dataRows(page.getByRole("table", { name: navCopy.findings }))).first()).toBeVisible();
    },
  },
  {
    name: "Processes",
    open: async (page) => {
      await nav(page, navCopy.processes);
      await expect(page.getByRole("table", { name: processesCopy.controllerTable })).toBeVisible();
    },
  },
];

/** Runs axe on the page, attaches every violation, and fails on serious or critical ones. */
async function scan(page: Page, label: string) {
  const { violations } = await new AxeBuilder({ page }).analyze();
  await test.info().attach(`axe-${label}.json`, {
    body: JSON.stringify(violations, null, 2),
    contentType: "application/json",
  });
  const line = (v: (typeof violations)[number]) =>
    `${v.impact} ${v.id} (${v.nodes.length}): ${v.nodes
      .slice(0, 5)
      .map((n) => n.target.join(" "))
      .join(" | ")}`;
  const blocking = violations.filter((v) => v.impact === "serious" || v.impact === "critical");
  for (const v of violations.filter((v) => !blocking.includes(v)))
    test.info().annotations.push({ type: "axe", description: line(v) });
  expect(blocking.map(line), `serious or critical axe violations on ${label}`).toEqual([]);
}

for (const theme of themes) {
  test.describe(`axe in ${theme.name}`, () => {
    for (const p of pages) {
      test(p.name, async ({ page }) => {
        await openInstall(page, theme);
        await p.open(page);
        await scan(page, `${p.name.replace(/ /g, "-")}-${theme.mode}`);
      });
    }
  });
}
