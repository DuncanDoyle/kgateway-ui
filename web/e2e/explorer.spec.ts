import { expect, type Locator, type Page, test } from "@playwright/test";
import type { Page as ListPage, RouteDetail, RouteRow } from "../packages/shell/src/api/types";
import { gatewayCopy, listCopy, navCopy, routeCopy, routeStateCopy } from "../packages/shell/src/copy/explorer";

// Walks the explorer over test/e2e/fixtures/explorer.yaml. Expected reasons come from the API,
// which TestExplorerShowsControllerStatus checks against the controller, so no version's wording is hard-coded.
const install = "/i/kgw-a.kgateway";
const api = "/api/v1/installs/kgw-a.kgateway";
const themes = [
  { name: "Control room", mode: "dark" },
  { name: "Control room light", mode: "light" },
] as const;

async function pickTheme(page: Page, theme: (typeof themes)[number]) {
  await page.getByRole("button", { name: /^Theme:/ }).click();
  await page.getByRole("menuitemradio", { name: theme.name, exact: true }).click();
  await expect(page.locator("html")).toHaveCSS("color-scheme", theme.mode);
}

/** The table's data rows once it is no longer loading. */
async function dataRows(table: Locator): Promise<Locator> {
  await expect(table).toHaveAttribute("aria-busy", "false");
  return table.locator("tbody > tr[data-row]");
}

/** The first False condition of the route's entry for parent, as the API reports it. */
async function failing(page: Page, route: string, parent: string) {
  const d: RouteDetail = await (
    await page.request.get(`${api}/routes/gateway.networking.k8s.io/HTTPRoute/${route}`)
  ).json();
  const entry = d.parents.find((p) => `${p.parentRef.namespace}/${p.parentRef.name}` === parent);
  const c = entry?.conditions.find((x) => x.status === "False");
  expect(c, `${route} has a False condition for ${parent}`).toBeTruthy();
  return c as NonNullable<typeof c>;
}

function shot(page: Page, name: string) {
  return page.screenshot({ path: `${test.info().project.outputDir}/explorer-${name}.png`, fullPage: true });
}

test("Gateway, failed route, its parent and back up the chain", async ({ page }) => {
  await page.goto(install);
  await page.getByRole("navigation", { name: navCopy.label }).getByRole("link", { name: navCopy.gateways }).click();
  await expect(page).toHaveURL(new RegExp(`${install}/gateways$`));
  await page.getByRole("table", { name: listCopy.gateways.title }).getByRole("link", { name: "infra/edge" }).click();
  await expect(page).toHaveURL(new RegExp(`${install}/gateways/infra/edge$`));

  // A route the Gateway rejected shows the controller's reason in the failed table and on its page.
  const rejected = await failing(page, "closed/not-allowed", "infra/edge");
  const failed = page.getByRole("table", { name: gatewayCopy.notAttached });
  const row = (await dataRows(failed)).filter({ has: page.getByRole("link", { name: "closed/not-allowed" }) });
  await expect(row).toContainText(rejected.reason);
  await row.getByRole("link", { name: "closed/not-allowed" }).click();
  await expect(page).toHaveURL(/\/routes\/gateway\.networking\.k8s\.io\/HTTPRoute\/closed\/not-allowed$/);
  await expect(page.getByRole("article", { name: routeCopy.parentCard("infra/edge") })).toContainText(rejected.reason);
  await page.goBack();

  // An attached delegating parent, then a child the controller did not accept.
  const attached = page.getByRole("table", { name: gatewayCopy.attachedRoutes });
  await (await dataRows(attached)).getByRole("link", { name: "shop/store" }).click();
  await expect(page).toHaveURL(/\/HTTPRoute\/shop\/store$/);
  await page.getByRole("button", { name: routeCopy.toggleChildren(false) }).click();
  const children = page.getByRole("table", { name: routeCopy.childRoutes });
  await (await dataRows(children)).getByRole("link", { name: "shop/c-host" }).click();
  await expect(page).toHaveURL(/\/HTTPRoute\/shop\/c-host$/);
  const childFailure = await failing(page, "shop/c-host", "shop/store");
  await expect(page.getByRole("article", { name: routeCopy.parentCard("shop/store") })).toContainText(
    childFailure.reason,
  );
  await shot(page, "failed-child");

  // Up the chain: the child's parent, then the parent's Gateway.
  const chain = page.getByRole("navigation", { name: routeCopy.upChain });
  await chain.getByRole("link", { name: "shop/store" }).click();
  await expect(page).toHaveURL(/\/HTTPRoute\/shop\/store$/);
  await page.getByRole("navigation", { name: routeCopy.upChain }).getByRole("link", { name: "infra/edge" }).click();
  await expect(page).toHaveURL(new RegExp(`${install}/gateways/infra/edge$`));
});

test("Routes filtered by replaced or dropped rules", async ({ page }) => {
  const want: ListPage<RouteRow> = await (
    await page.request.get(`${api}/routes?state=not-programmed&limit=200`)
  ).json();
  expect(want.items.map((r) => `${r.ref.namespace}/${r.ref.name}`)).toContain("shop/replaced");
  await page.goto(`${install}/routes`);
  await page
    .getByRole("group", { name: listCopy.routeState })
    .getByRole("button", { name: routeStateCopy["not-programmed"] })
    .click();
  await expect(page).toHaveURL(/state=not-programmed/);
  const rows = await dataRows(page.getByRole("table", { name: listCopy.routes.title }));
  await expect(rows).toHaveCount(want.items.length);
  for (const r of want.items) {
    await expect(rows.getByRole("link", { name: `${r.ref.namespace}/${r.ref.name}` })).toBeVisible();
  }
});

for (const theme of themes) {
  test(`hover, active and focus styles (${theme.name})`, async ({ page }) => {
    await page.goto(`${install}/routes`);
    await pickTheme(page, theme);
    const table = page.getByRole("table", { name: listCopy.routes.title });
    await dataRows(table);
    const chip = page.getByRole("group", { name: listCopy.routeState }).getByRole("button").first();
    const bg = () => chip.evaluate((el) => getComputedStyle(el).backgroundColor);
    const rest = await bg();
    await chip.hover();
    const hover = await bg();
    expect(hover, "hover changes the chip background").not.toBe(rest);
    await shot(page, `hover-${theme.mode}`);
    await page.mouse.down();
    expect(await bg(), "a press changes it again").not.toBe(hover);
    await shot(page, `active-${theme.mode}`);
    // Release off the chip so the press does not toggle the filter.
    await page.mouse.move(0, 0);
    await page.mouse.up();
    await expect(page).not.toHaveURL(/state=/);

    // Keyboard focus shows a visible outline on the search box's next control.
    await page.getByRole("searchbox", { name: listCopy.routes.search }).focus();
    await page.keyboard.press("Tab");
    const focused = page.locator(":focus-visible");
    await expect(focused).toHaveCount(1);
    expect(await focused.evaluate((el) => getComputedStyle(el).outlineStyle)).not.toBe("none");
    await shot(page, `focus-${theme.mode}`);

    // Row links: hover underlines the route name.
    const link = (await dataRows(table)).first().getByRole("link").first();
    const name = link.locator('[data-part="name"]');
    const underline = () => name.evaluate((el) => getComputedStyle(el).textDecorationLine);
    expect(await underline()).toBe("none");
    await link.hover();
    expect(await underline(), "hover underlines the row link").toBe("underline");
    await link.focus();
    await shot(page, `link-${theme.mode}`);
  });
}
