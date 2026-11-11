import { expect, type Locator, type Page, test } from "@playwright/test";
import type { PathsResponse } from "../packages/shell/src/api/types";
import { mapCopy } from "../packages/shell/src/copy/map";

// The Gateway map (Phase 6 sub-phase 6.5) over test/e2e/fixtures/traffic.yaml. Expected nodes and states come from
// the /paths response the map draws.
const install = "/i/kgw-a.kgateway";
const api = "/api/v1/installs/kgw-a.kgateway";
const gw = "traffic/traffic";
const themes = [
  { name: "Control room", mode: "dark" },
  { name: "Control room light", mode: "light" },
] as const;

async function pickTheme(page: Page, theme: (typeof themes)[number]) {
  await page.getByRole("button", { name: /^Theme:/ }).click();
  await page.getByRole("menuitemradio", { name: theme.name, exact: true }).click();
  await expect(page.locator("html")).toHaveCSS("color-scheme", theme.mode);
  await expect(page.getByRole("menu")).toBeHidden();
}

async function paths(page: Page, query: string): Promise<PathsResponse> {
  const res = await page.request.get(`${api}/paths?gateway=${gw}&limit=200${query}`);
  expect(res.ok(), `GET paths${query}`).toBeTruthy();
  return (await res.json()) as PathsResponse;
}

/** The distinct node names the map draws for a result: Gateway, listeners, routes and backends (or actions). */
function nodeNames(d: PathsResponse): Set<string> {
  const ref = (r: { namespace?: string; name: string }) => (r.namespace ? `${r.namespace}/${r.name}` : r.name);
  return new Set(
    d.rows.flatMap((r) => [
      `gateway:${ref(r.gateway)}`,
      `listener:${r.listener.name}`,
      ...r.routes.map((x) => `route:${ref(x)}`),
      ...(r.backend
        ? [`backend:${ref(r.backend)}`]
        : r.action !== "delegate"
          ? [`action:${r.routes.at(-1)?.name}:${r.action}`]
          : []),
    ]),
  );
}

/** Opens the traffic Gateway's Map tab and waits for its nodes. */
async function openMap(page: Page): Promise<Locator> {
  await page.goto(`${install}/gateways/${gw}`);
  await page.getByRole("tab", { name: mapCopy.tab }).click();
  await expect(page).toHaveURL(/tab=map/);
  const map = page.getByTestId("gateway-map");
  await expect(map.getByTestId("map-node").first()).toBeVisible();
  return map;
}

/** The React Flow node whose card is named name. */
const node = (map: Locator, name: string) =>
  map.locator(".react-flow__node").filter({ has: map.page().locator(".name", { hasText: new RegExp(`^${name}$`) }) });

function shot(page: Page, name: string) {
  const dir = process.env.MAP_SHOTS ?? test.info().project.outputDir;
  // Transitions finish first, so the shot never catches a fade half way.
  return page.screenshot({ path: `${dir}/map-${name}.png`, animations: "disabled" });
}

test("hovering a route dims the nodes off its paths", async ({ page }) => {
  const map = await openMap(page);
  await node(map, "traffic/asleep").first().hover();
  await expect(node(map, "traffic/weighted")).toHaveClass(/\bdim\b/);
  await expect(node(map, "traffic/asleep").first()).toHaveClass(/\blit\b/);
  // The backend and listener on its path stay lit.
  await expect(node(map, "traffic/asleep").last()).toHaveClass(/\blit\b/);
  await expect(node(map, "http")).toHaveClass(/\blit\b/);
  await page.mouse.move(0, 0);
  await expect(map.locator(".react-flow__node.dim")).toHaveCount(0);
});

test("a lit edge shows its state on the hovered node's paths, not the worst row on it", async ({ page }) => {
  const map = await openMap(page);
  // The Gateway → http trunk carries the failing cross path, so at rest it is drawn as an error.
  const trunk = map.locator(
    '[data-id="g:gateway.networking.k8s.io/Gateway/traffic/traffic>l:http"] path.react-flow__edge-path',
  );
  await expect(trunk).toHaveClass(/state-error/);
  await node(map, "traffic/weighted").hover();
  await expect(trunk).toHaveClass(/state-healthy/);
  await node(map, "traffic/cross").hover();
  await expect(trunk).toHaveClass(/state-error broken/);
});

test("the broken backend is drawn broken, and its panel links to backend detail", async ({ page }) => {
  const d = await paths(page, "");
  const cross = d.rows.find((r) => r.routes[0]?.name === "cross");
  expect(cross?.problem.state, "the cross-namespace ref fails (014)").toBe("error");
  const map = await openMap(page);
  // Every row on cross → shop/httpbin fails, so the edge is dashed as broken; a healthy edge is solid.
  const broken = map.locator(
    '[data-id="r:gateway.networking.k8s.io/HTTPRoute/traffic/cross>b:/Service/shop/httpbin"] path.react-flow__edge-path',
  );
  await expect(broken).toHaveClass(/state-error broken/);
  expect(await broken.evaluate((el) => getComputedStyle(el).strokeDasharray)).not.toBe("none");
  const healthy = map.locator(".react-flow__edge-path.state-healthy").first();
  expect(await healthy.evaluate((el) => getComputedStyle(el).strokeDasharray)).toBe("none");

  const before = await map.boundingBox();
  await node(map, "shop/httpbin").click();
  const panel = page.getByRole("complementary", { name: mapCopy.panel });
  // The panel takes its own column: the canvas narrows and nothing overlaps.
  await expect(panel).toBeFocused();
  const after = await map.boundingBox();
  const side = await panel.boundingBox();
  expect(after && before && after.width < before.width).toBe(true);
  expect(after && side && after.x + after.width <= side.x).toBe(true);
  // The selected node stays inside the narrowed canvas.
  await expect
    .poll(async () => {
      const sel = await node(map, "shop/httpbin").boundingBox();
      return !!sel && !!after && sel.x + sel.width <= after.x + after.width;
    })
    .toBe(true);
  await expect(panel.getByRole("heading", { level: 2 })).toHaveText("shop/httpbin");
  await expect(panel.getByTestId("map-panel-counts")).toContainText(mapCopy.fail(1, 1));
  const detail = panel.getByRole("link", { name: mapCopy.open });
  await expect(detail).toHaveAttribute("href", `${install}/backends/-/Service/shop/httpbin`);
  await expect(panel.getByRole("link", { name: mapCopy.traffic })).toHaveAttribute(
    "href",
    `${install}/traffic?${new URLSearchParams({ gateway: gw, backend: "core/Service/shop/httpbin" })}`,
  );
  await detail.click();
  await expect(page).toHaveURL(new RegExp(`${install}/backends/-/Service/shop/httpbin`));
});

test("the Problems chip reduces the map to the paths with problems, in the URL", async ({ page }) => {
  const all = nodeNames(await paths(page, ""));
  const problems = nodeNames(await paths(page, "&state=error&state=warning&state=unknown"));
  expect(problems.size).toBeLessThan(all.size);
  const map = await openMap(page);
  await expect(map.getByTestId("map-node")).toHaveCount(all.size);
  await page.getByRole("button", { name: mapCopy.problemsOnly }).click();
  await expect(page).toHaveURL(/state=error%2Cwarning%2Cunknown/);
  await expect(map.getByTestId("map-node")).toHaveCount(problems.size);
  await page.reload();
  await expect(page.getByRole("button", { name: mapCopy.problemsOnly })).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByTestId("gateway-map").getByTestId("map-node")).toHaveCount(problems.size);
});

test("find centres and selects a node by name", async ({ page }) => {
  await openMap(page);
  await page.getByRole("searchbox", { name: mapCopy.find }).fill("asleep");
  const panel = page.getByRole("complementary", { name: mapCopy.panel });
  await expect(panel.getByRole("heading", { level: 2 })).toHaveText("traffic/asleep");
  await expect(page.getByTestId("map-found")).toHaveText(mapCopy.found(1, 2));
  await page.getByRole("searchbox", { name: mapCopy.find }).press("Enter");
  await expect(page.getByTestId("map-found")).toHaveText(mapCopy.found(2, 2));
  await panel.getByRole("button", { name: mapCopy.close }).click();
  await expect(panel).toHaveCount(0);
});

for (const theme of themes) {
  test(`map, hover and side panel in the ${theme.mode} theme, with hover and active styles`, async ({ page }) => {
    await page.setViewportSize({ width: 1920, height: 1080 });
    const map = await openMap(page);
    await pickTheme(page, theme);
    await page.locator(".react-flow__controls-fitview").click();
    await shot(page, `${theme.mode}-map`);

    const card = node(map, "traffic/weighted").getByTestId("map-node");
    const bg = (l: Locator) => l.evaluate((el) => getComputedStyle(el).backgroundColor);
    const rest = await bg(card);
    await card.hover();
    const hover = await bg(card);
    expect(hover, "hover changes the card background").not.toBe(rest);
    await expect(node(map, "traffic/cross")).toHaveClass(/\bdim\b/);
    await shot(page, `${theme.mode}-hover`);
    await page.mouse.down();
    expect(await bg(card), "a press changes it again").not.toBe(hover);
    await page.mouse.up();

    await node(map, "traffic/cross").click();
    const panel = page.getByRole("complementary", { name: mapCopy.panel });
    await expect(panel).toBeVisible();
    const close = panel.getByRole("button", { name: mapCopy.close });
    const closeRest = await bg(close);
    await close.hover();
    expect(await bg(close), "hover changes the close button").not.toBe(closeRest);
    await page.mouse.move(5, 5);
    await shot(page, `${theme.mode}-panel`);
  });
}
