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

  // An implicit child (empty parentRefs) shows the same rejection on its implicit parent's card.
  const implicitFailure = await failing(page, "shop/c-ihost", "shop/store");
  await page.goto(`${install}/routes/gateway.networking.k8s.io/HTTPRoute/shop/c-ihost`);
  await expect(page.getByRole("article", { name: routeCopy.implicitParentCard("shop/store") })).toContainText(
    implicitFailure.reason,
  );
  await shot(page, "implicit-child");
  await page.goBack();
  await expect(page).toHaveURL(/\/HTTPRoute\/shop\/c-host$/);

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

// Layout (design §8.6–8.7): list responses are held so each table is measured with its skeleton, then loaded.
type Box = { x: number; y: number; width: number; height: number };
type Layout = { footer: Box | null; next: Box | null; rows: number[] };

/** Holds every list response until release is called; detail and YAML requests pass. limit sets the page size. */
async function holdLists(page: Page, limit?: number): Promise<() => void> {
  let release = () => {};
  const gate = new Promise<void>((r) => {
    release = r;
  });
  await page.route(
    (u) => /^\/api\/v1\/installs\/[^/]+\/(routes|gateways|listenersets)$/.test(u.pathname),
    async (route) => {
      await gate;
      const u = new URL(route.request().url());
      if (limit) u.searchParams.set("limit", String(limit));
      await route.continue({ url: u.toString() });
    },
  );
  return release;
}

/** The table's footer, the element after its section and its row heights; boxes in page coordinates. */
function measure(table: Locator): Promise<Layout> {
  return table.evaluate((t) => {
    const box = (el: Element | null | undefined) => {
      if (!el) return null;
      const r = el.getBoundingClientRect();
      return { x: r.x + window.scrollX, y: r.y + window.scrollY, width: r.width, height: r.height };
    };
    const section = t.closest("section");
    return {
      footer: box((section ?? document).querySelector('[data-testid="list-footer"]')),
      next: box(section?.nextElementSibling),
      // A skeleton or data row, or the empty row that takes a skeleton row's place.
      rows: Array.from(
        t.querySelectorAll("tbody > tr:is([data-row], [data-empty])"),
        (r) => r.getBoundingClientRect().height,
      ),
    };
  });
}

/** Measures tables while their skeletons show, releases the lists, and checks nothing moved once loaded. */
async function expectStableLayout(page: Page, release: () => void, names: string[]) {
  const tables = names.map((name) => page.getByRole("table", { name, exact: true }));
  const before: Layout[] = [];
  for (const t of tables) {
    await expect(t).toHaveAttribute("aria-busy", "true");
    await expect(t.getByTestId("skeleton-row").first()).toBeVisible();
    before.push(await measure(t));
  }
  release();
  for (const [i, t] of tables.entries()) {
    await expect(t).toHaveAttribute("aria-busy", "false");
    await expect(t.getByTestId("skeleton-row")).toHaveCount(0);
    const after = await measure(t);
    const b = before[i] as Layout;
    expect.soft(after.rows.length, `${names[i]}: rows`).toBe(b.rows.length);
    for (const h of after.rows)
      expect.soft(h, `${names[i]}: a loaded row keeps the skeleton row's height`).toBe(b.rows[0]);
    expect.soft(after.footer, `${names[i]}: footer`).toEqual(b.footer);
    expect.soft(after.next, `${names[i]}: the section after it`).toEqual(b.next);
  }
}

const LONG = "shop/a-route-with-a-deliberately-long-name-that-must-not-wrap-or-shift-the-table";
for (const width of [1280, 720]) {
  test.describe(`layout at ${width} px`, () => {
    test.use({ viewport: { width, height: 900 } });

    test("Gateway sub-tables do not move when their rows load", async ({ page }) => {
      // Known gap: a wrapped long name grows its row (design §8.6 final row height vs §8.7 wrap long names).
      test.fail(width < 900, "the long route name wraps in the narrow attached-routes table");
      const release = await holdLists(page);
      await page.goto(`${install}/gateways/infra/edge`);
      await expect(page.getByTestId("detail-header")).toBeVisible();
      await expectStableLayout(page, release, [
        gatewayCopy.listenerSets,
        gatewayCopy.attachedRoutes,
        gatewayCopy.notAttached,
      ]);
      const attached = page.getByRole("table", { name: gatewayCopy.attachedRoutes, exact: true });
      await expect(attached.getByRole("link", { name: LONG })).toBeVisible();
      await shot(page, `layout-gateway-${width}`);
    });

    test("ListenerSet sub-tables do not move when their rows load", async ({ page }) => {
      const release = await holdLists(page);
      await page.goto(`${install}/listenersets/gateway.networking.k8s.io/ListenerSet/infra/extra`);
      await expect(page.getByTestId("detail-header")).toBeVisible();
      await expectStableLayout(page, release, [gatewayCopy.attachedRoutes, gatewayCopy.notAttached]);
    });

    test("child routes do not move when their rows load", async ({ page }) => {
      const release = await holdLists(page);
      await page.goto(`${install}/routes/gateway.networking.k8s.io/HTTPRoute/shop/store`);
      await page.getByRole("button", { name: routeCopy.toggleChildren(false) }).click();
      await expectStableLayout(page, release, [routeCopy.childRoutes]);
    });

    test("a full routes page keeps its footer in place, long name included", async ({ page }) => {
      // Known gap: a wrapped long name grows its row (design §8.6 final row height vs §8.7 wrap long names).
      test.fail(true, "the long route name wraps and grows its row");
      // A page of eight fills the list's eight skeleton rows; the long name sorts first in shop.
      const release = await holdLists(page, 8);
      await page.goto(`${install}/routes?namespace=shop`);
      await expectStableLayout(page, release, [listCopy.routes.title]);
      const table = page.getByRole("table", { name: listCopy.routes.title, exact: true });
      await expect(table.getByRole("link", { name: LONG })).toBeVisible();
      await shot(page, `layout-routes-${width}`);
    });
  });
}
