import { type Browser, expect, type Locator, type Page, test } from "@playwright/test";
import type { FindingList } from "../packages/shell/src/api/diagTypes";
import { gatewayCopy, listCopy, navCopy, routeCopy, routeStateCopy } from "../packages/shell/src/copy/explorer";
import { breadcrumbsCopy } from "../packages/shell/src/shell/trail";

// The shell's navigation over test/e2e/fixtures/explorer.yaml: the `via` trail (D-035), the object header and the
// fluid layout (design §17.6, §21).
const id = "kgw-a.kgateway";
const install = `/i/${id}`;
const api = `/api/v1/installs/${id}`;
const GW = "gateway.networking.k8s.io";
const routePage = (ns: string, name: string) => `${install}/routes/${GW}/HTTPRoute/${ns}/${name}`;
const path = (p: string) => new RegExp(`${p.replace(/[.?]/g, "\\$&")}(\\?|$)`);

const trailNav = (page: Page) => page.getByRole("navigation", { name: breadcrumbsCopy.label });
const crumbs = (page: Page) => trailNav(page).locator('[data-part="crumb"]');
const crumb = (page: Page, name: string | RegExp) => trailNav(page).getByRole("link", { name, exact: true });

/** The table's data rows once it is no longer loading. */
async function dataRows(table: Locator): Promise<Locator> {
  await expect(table).toHaveAttribute("aria-busy", "false");
  return table.locator("tbody > tr[data-row]");
}

/** Opens url in a new browser context, as someone pasting a copied link would, and checks the trail. */
async function expectTrailInFreshContext(browser: Browser, url: string, want: (string | RegExp)[]) {
  const ctx = await browser.newContext();
  const fresh = await ctx.newPage();
  await fresh.goto(url);
  await expect(crumbs(fresh)).toHaveText(want);
  await ctx.close();
}

test("Gateway, attached route, child route, then each crumb back", async ({ page, browser }) => {
  await page.goto(`${install}/gateways/infra/edge`);
  // Without via the trail is the hierarchy: install › Gateways › the Gateway.
  await expect(crumbs(page)).toHaveText([/kgw-a/, navCopy.gateways, "Gateway infra/edge"]);
  const attached = page.getByRole("table", { name: gatewayCopy.attachedRoutes, exact: true });
  await (await dataRows(attached)).getByRole("link", { name: "shop/store", exact: true }).click();
  await expect(page).toHaveURL(path(routePage("shop", "store")));
  await expect(crumbs(page)).toHaveText([/kgw-a/, "Gateway infra/edge", "HTTPRoute shop/store"]);

  await page.getByRole("button", { name: routeCopy.toggleChildren(false) }).click();
  const children = page.getByRole("table", { name: routeCopy.childRoutes });
  await (await dataRows(children)).getByRole("link", { name: "shop/c-host", exact: true }).click();
  await expect(page).toHaveURL(path(routePage("shop", "c-host")));
  const full = [/kgw-a/, "Gateway infra/edge", "HTTPRoute shop/store", "HTTPRoute shop/c-host"];
  await expect(crumbs(page)).toHaveText(full);

  // The trail lives in the URL: a reload and a copied link keep it.
  await page.reload();
  await expect(crumbs(page)).toHaveText(full);
  await expectTrailInFreshContext(browser, page.url(), full);

  // Each crumb lands on the earlier page with the trail it had there.
  await crumb(page, "HTTPRoute shop/store").click();
  await expect(page).toHaveURL(path(routePage("shop", "store")));
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(/shop\/store/);
  await expect(crumbs(page)).toHaveText([/kgw-a/, "Gateway infra/edge", "HTTPRoute shop/store"]);
  await crumb(page, "Gateway infra/edge").click();
  await expect(page).toHaveURL(new RegExp(`${install}/gateways/infra/edge$`));
  await expect(crumbs(page)).toHaveText([/kgw-a/, navCopy.gateways, "Gateway infra/edge"]);
  await crumb(page, /kgw-a/).click();
  await expect(page).toHaveURL(new RegExp(`${install}$`));
});

test("a filtered routes list, a route, then the list crumb restores the filters", async ({ page }) => {
  await page.goto(`${install}/routes?namespace=shop`);
  const state = page.getByRole("group", { name: listCopy.routeState });
  await state.getByRole("button", { name: routeStateCopy["not-programmed"] }).click();
  await expect(page).toHaveURL(/state=not-programmed/);
  const table = page.getByRole("table", { name: listCopy.routes.title });
  await (await dataRows(table)).getByRole("link", { name: "shop/replaced", exact: true }).click();
  await expect(page).toHaveURL(path(routePage("shop", "replaced")));
  await expect(crumbs(page)).toHaveText([/kgw-a/, navCopy.routes, "HTTPRoute shop/replaced"]);

  await crumb(page, navCopy.routes).click();
  await expect(page).toHaveURL(new RegExp(`${install}/routes\\?`));
  const q = new URL(page.url()).searchParams;
  expect(q.getAll("namespace")).toEqual(["shop"]);
  expect(q.get("state")).toBe("not-programmed");
  expect(q.get("via")).toBeNull();
  await expect(state.getByRole("button", { name: routeStateCopy["not-programmed"] })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await expect((await dataRows(table)).getByRole("link", { name: "shop/replaced", exact: true })).toBeVisible();
});

test("a filtered findings list, a finding, its object, then the list crumb restores the filters", async ({ page }) => {
  await page.goto(`${install}/findings?severity=error&q=broken`);
  const table = page.getByRole("table", { name: navCopy.findings });
  await (await dataRows(table)).getByRole("link", { name: "Backend not found" }).first().click();
  await expect(page).toHaveURL(new RegExp(`${install}/findings/[^/?]+\\?via=`));
  // A finding page hands its trail on to the objects it links.
  await page.getByTestId("detail-header").waitFor();
  await page.getByRole("link", { name: "shop/broken", exact: true }).first().click();
  await expect(page).toHaveURL(path(routePage("shop", "broken")));
  await expect(crumbs(page)).toHaveText([/kgw-a/, navCopy.findings, "HTTPRoute shop/broken"]);

  await crumb(page, navCopy.findings).click();
  await expect(page).toHaveURL(new RegExp(`${install}/findings\\?`));
  const q = new URL(page.url()).searchParams;
  expect(q.get("severity")).toBe("error");
  expect(q.get("q")).toBe("broken");
  await expect(page.getByRole("searchbox", { name: "Filter findings" })).toHaveValue("broken");
});

test("a trail of 7 earlier pages collapses into a menu and keeps the last 5", async ({ page }) => {
  // Seven distinct earlier objects, written as the app writes them.
  const routes = ["store", "c-host", "tree", "t-mid", "t-partial", "replaced", "broken"];
  const via = routes.map((n) => `${id}/${encodeURIComponent(GW)}/HTTPRoute/shop/${n}`).join(",");
  await page.goto(`${routePage("shop", "quirk")}?${new URLSearchParams({ via })}`);
  const more = trailNav(page).getByRole("button", { name: breadcrumbsCopy.more(2) });
  await expect(more).toBeVisible();
  await expect(crumbs(page)).toHaveText([
    /kgw-a/,
    ...routes.slice(2).map((n) => `HTTPRoute shop/${n}`),
    "HTTPRoute shop/quirk",
  ]);
  await more.click();
  const menu = page.getByRole("menu", { name: breadcrumbsCopy.menu });
  await expect(menu.getByRole("menuitem")).toHaveText(["HTTPRoute shop/store", "HTTPRoute shop/c-host"]);
  await menu.getByRole("menuitem", { name: "HTTPRoute shop/c-host" }).click();
  await expect(page).toHaveURL(path(routePage("shop", "c-host")));
  await expect(crumbs(page)).toHaveText([/kgw-a/, "HTTPRoute shop/store", "HTTPRoute shop/c-host"]);
});

// Object header (Task 27): each core detail page renders it from the URL, so data arriving moves nothing.
type Box = { x: number; y: number; width: number; height: number };
const CORE_PAGES = [
  { name: "Gateway", url: `${install}/gateways/infra/edge` },
  { name: "ListenerSet", url: `${install}/listenersets/${GW}/ListenerSet/infra/extra` },
  { name: "HTTPRoute", url: `${routePage("shop", "c-host")}?via=${encodeURIComponent(`${id}/gateways`)}` },
  { name: "resource", url: `${install}/r/gateway.kgateway.dev/Backend/shop/aws-be` },
];
const DETAIL = new RegExp(`^${api}/(gateways/[^/]+/[^/]+|(listenersets|routes|resources)/[^/]+/[^/]+/[^/]+/[^/]+)$`);

function headBoxes(page: Page): Promise<{ trail: Box | null; header: Box | null }> {
  return page.evaluate(() => {
    const box = (sel: string) => {
      const el = document.querySelector(sel);
      if (!el) return null;
      const r = el.getBoundingClientRect();
      return { x: r.x + window.scrollX, y: r.y + window.scrollY, width: r.width, height: r.height };
    };
    return { trail: box("nav[data-trail]"), header: box("[data-object-header]") };
  });
}

for (const p of CORE_PAGES) {
  test(`${p.name} page: the object header holds its place as data arrives`, async ({ page }) => {
    let release = () => {};
    const gate = new Promise<void>((r) => {
      release = r;
    });
    await page.route(
      (u) => DETAIL.test(u.pathname),
      async (route) => {
        await gate;
        await route.continue();
      },
    );
    await page.goto(p.url);
    const header = page.locator("[data-object-header]");
    await expect(header).toHaveCount(1);
    await expect(trailNav(page)).toBeVisible();
    // Installs load on their own request, so the install crumb settles before measuring.
    await expect(crumbs(page).first()).toHaveText(/kgw-a/);
    const before = await headBoxes(page);
    expect(before.header?.height, "header height").toBe(64);
    release();
    if (p.name === "resource") await expect(page.getByTestId("yaml")).toBeVisible();
    else {
      await expect(page.getByTestId("detail-skeleton")).toHaveCount(0);
      await expect(header.getByTestId(/^health-icon-/)).toBeVisible();
    }
    expect(await headBoxes(page)).toEqual(before);
  });
}

// Layout (design §8.7, ruling R30): fluid up to 2400 px, no page scroll, and one column on core pages, which
// contribute no side section; the e2e build has no extension, so no page here has a second column.
const WIDTHS = [720, 1280, 1600, 3008];

/** The page frame's width, the width it may use, and whether the page scrolls sideways. */
function frame(page: Page, inner: string) {
  return page.evaluate((sel) => {
    let el = document.querySelector(sel) as HTMLElement | null;
    while (el && getComputedStyle(el).maxWidth !== "2400px") el = el.parentElement;
    if (!el?.parentElement) throw new Error(`no 2400 px frame around ${sel}`);
    const parent = el.parentElement;
    const pad =
      Number.parseFloat(getComputedStyle(parent).paddingLeft) +
      Number.parseFloat(getComputedStyle(parent).paddingRight);
    return {
      width: el.getBoundingClientRect().width,
      room: parent.clientWidth - pad,
      scrollWidth: document.scrollingElement?.scrollWidth ?? 0,
      innerWidth: window.innerWidth,
    };
  }, inner);
}

for (const width of WIDTHS) {
  test.describe(`layout at ${width} px`, () => {
    test.use({ viewport: { width, height: 1080 } });

    test("findings list and finding detail fill the width up to 2400 px without page scroll", async ({ page }) => {
      const list: FindingList = await (await page.request.get(`${api}/findings?limit=1`)).json();
      await page.goto(`${install}/findings`);
      await dataRows(page.getByRole("table", { name: navCopy.findings }));
      let f = await frame(page, "table");
      expect(f.scrollWidth, "no horizontal page scroll").toBeLessThanOrEqual(f.innerWidth);
      expect(f.width).toBe(Math.min(2400, f.room));

      const first = list.items[0];
      if (!first?.id) throw new Error("the fixture has a finding with an id");
      await page.goto(`${install}/findings/${encodeURIComponent(first.id)}`);
      await expect(page.getByTestId("detail-skeleton")).toHaveCount(0);
      f = await frame(page, '[data-testid="detail-header"]');
      expect(f.scrollWidth, "no horizontal page scroll").toBeLessThanOrEqual(f.innerWidth);
      expect(f.width).toBe(Math.min(2400, f.room));
    });

    for (const p of CORE_PAGES.slice(0, 3)) {
      test(`${p.name} page is one column and fills the width up to 2400 px`, async ({ page }) => {
        await page.goto(p.url);
        await expect(page.getByTestId("detail-skeleton")).toHaveCount(0);
        await expect(page.getByTestId("detail-columns")).toBeVisible();
        await expect(page.getByTestId("detail-side")).toHaveCount(0);
        const [cols, main] = await Promise.all([
          page.getByTestId("detail-columns").boundingBox(),
          page.getByTestId("detail-main").boundingBox(),
        ]);
        expect(main?.width, "the main column takes the whole row").toBe(cols?.width);
        const f = await frame(page, "[data-object-header]");
        expect(f.scrollWidth, "no horizontal page scroll").toBeLessThanOrEqual(f.innerWidth);
        expect(f.width).toBe(Math.min(2400, f.room));
      });
    }
  });
}
