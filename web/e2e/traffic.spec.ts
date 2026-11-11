import { expect, type Locator, type Page, test } from "@playwright/test";
import type { PathsResponse } from "../packages/shell/src/api/types";
import { endpointCopy, pathColumnCopy, summaryCopy, trafficCopy } from "../packages/shell/src/copy/traffic";
import { stateLabel } from "../packages/shell/src/health/state";

// The Traffic path list (Phase 6 sub-phase 6.2) over test/e2e/fixtures/traffic.yaml. States and counts come from
// the API response the page renders: the SPA derives none.
const install = "/i/kgw-a.kgateway";
const api = "/api/v1/installs/kgw-a.kgateway";
const gw = "traffic/traffic";
const RANK = { error: 0, warning: 1, unknown: 2, healthy: 3 } as const;
const themes = [
  { name: "Control room", mode: "dark" },
  { name: "Control room light", mode: "light" },
] as const;

async function pickTheme(page: Page, theme: (typeof themes)[number]) {
  await page.getByRole("button", { name: /^Theme:/ }).click();
  await page.getByRole("menuitemradio", { name: theme.name, exact: true }).click();
  await expect(page.locator("html")).toHaveCSS("color-scheme", theme.mode);
}

async function paths(page: Page, query: string): Promise<PathsResponse> {
  const res = await page.request.get(`${api}/paths?${query}`);
  expect(res.ok(), `GET paths?${query}`).toBeTruthy();
  return (await res.json()) as PathsResponse;
}

/** The table's data rows once it is no longer loading. */
async function dataRows(page: Page): Promise<Locator> {
  const table = page.getByRole("table", { name: trafficCopy.table });
  await expect(table).toHaveAttribute("aria-busy", "false");
  return table.locator("tbody > tr[data-row]");
}

/** The row whose route cell names route (namespace/name). */
const rowOf = (rows: Locator, route: string) =>
  rows.filter({ has: rows.page().locator('td[data-col="name"]').getByRole("link", { name: route, exact: true }) });

/** Waits until 023's hold has passed and the scaled-to-zero Service's path is a warning (the controller's ruling). */
async function asleepIsWarning(page: Page) {
  await expect(async () => {
    const d = await paths(page, `gateway=${gw}&q=asleep`);
    expect(d.rows.find((r) => r.routes[0]?.name === "asleep")?.problem.state).toBe("warning");
  }).toPass({ timeout: 120_000 });
}

function shot(page: Page, name: string) {
  const dir = process.env.TRAFFIC_SHOTS ?? test.info().project.outputDir;
  return page.screenshot({ path: `${dir}/traffic-${name}.png`, fullPage: true });
}

test("nav → Traffic lists every Gateway's paths, problems first", async ({ page }) => {
  await page.goto(install);
  await page.getByRole("navigation", { name: "Install" }).getByRole("link", { name: trafficCopy.title }).click();
  await expect(page).toHaveURL(new RegExp(`${install}/traffic$`));
  const rows = await dataRows(page);
  const d = await paths(page, "");
  // The page shows the server's first page in the server's order, which is problem-first.
  const states = await rows.getByTestId("path-state").allTextContents();
  expect(states.length).toBeGreaterThan(0);
  const ranks = d.rows.map((r) => RANK[r.problem.state]);
  expect(ranks).toEqual([...ranks].sort((a, b) => a - b));
  expect(states).toEqual(d.rows.slice(0, states.length).map((r) => stateLabel(r.problem.state)));
  await expect(page.getByTestId("summary-counts")).toHaveText(
    summaryCopy.counts(d.summary.paths, d.summary.routes, d.summary.gateways, d.summary.backends),
  );
});

test("the zero-endpoint backend's path is a warning once 023's hold has passed", async ({ page }) => {
  await asleepIsWarning(page);
  await page.goto(`${install}/traffic?gateway=${gw}&q=asleep`);
  const row = rowOf(await dataRows(page), "traffic/asleep");
  await expect(row.getByTestId("path-state")).toHaveText(stateLabel("warning"));
  const e = (await paths(page, `gateway=${gw}&q=asleep`)).rows.find((r) => r.routes[0]?.name === "asleep")?.endpoints;
  expect(e?.ready, "023's evidence: no ready endpoints").toBe(0);
  await expect(row.locator('td[data-col="endpoints"]')).toHaveText(endpointCopy.ready(0, e?.total ?? 0));
});

test("the gateway= chip narrows, and a reload keeps the filters", async ({ page }) => {
  // The error filter keeps the traffic Gateway's broken cross-namespace path on page one.
  await page.goto(`${install}/traffic?state=error`);
  await dataRows(page);
  const chip = page.getByRole("group", { name: trafficCopy.gateways }).getByRole("button", { name: gw, exact: true });
  await chip.click();
  await expect(page).toHaveURL(/gateway=traffic%2Ftraffic/);
  await expect(chip).toHaveAttribute("aria-pressed", "true");
  const d = await paths(page, `state=error&gateway=${gw}`);
  const rows = await dataRows(page);
  await expect(rows).toHaveCount(d.rows.length);
  for (const g of await rows.locator('td[data-col="gateway"] a').allTextContents()) expect(g).toBe(gw);
  await expect(rowOf(rows, "traffic/cross").getByTestId("path-state")).toHaveText(stateLabel("error"));
  // The cross-namespace ref fails as attributed by 014.
  await expect(rowOf(rows, "traffic/cross").locator('td[data-col="share"]')).toContainText("requests fail");

  const url = page.url();
  await page.reload();
  expect(page.url()).toBe(url);
  await expect(
    page.getByRole("group", { name: trafficCopy.gateways }).getByRole("button", { name: gw }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(
    page.getByRole("group", { name: trafficCopy.state }).getByRole("button", { name: "Error" }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(await dataRows(page)).toHaveCount(d.rows.length);
});

test("each rule shows what it does: redirect, weights, gRPC and TCP", async ({ page }) => {
  await page.goto(`${install}/traffic?gateway=${gw}`);
  const rows = await dataRows(page);
  await expect(rowOf(rows, "traffic/moved").locator('td[data-col="target"]')).toHaveText("Redirect");
  const weighted = rowOf(rows, "traffic/weighted");
  await expect(weighted).toHaveCount(2);
  await expect(weighted.locator('td[data-col="share"]')).toHaveText(["90 %", "10 %"]);
  await expect(page.getByRole("columnheader", { name: new RegExp(pathColumnCopy.shareHint) })).toBeVisible();
  await expect(rowOf(rows, "traffic/grpc").locator('td[data-col="match"]')).toHaveText("echo.Echo/Ping");
  await expect(rowOf(rows, "traffic/tcp").locator('td[data-col="match"]')).toHaveText("All connections");
});

for (const theme of themes) {
  test(`chips and rows have hover and active styles (${theme.name})`, async ({ page }) => {
    await asleepIsWarning(page);
    await page.setViewportSize({ width: 1920, height: 1080 });
    await page.goto(`${install}/traffic?gateway=${gw}`);
    await pickTheme(page, theme);
    const rows = await dataRows(page);
    await expect(rowOf(rows, "traffic/asleep").getByTestId("path-state")).toHaveText(stateLabel("warning"));
    await shot(page, theme.mode);

    const chip = page.getByRole("group", { name: trafficCopy.kind }).getByRole("button").first();
    const bg = (l: Locator) => l.evaluate((el) => getComputedStyle(el).backgroundColor);
    const rest = await bg(chip);
    await chip.hover();
    const hover = await bg(chip);
    expect(hover, "hover changes the chip background").not.toBe(rest);
    await page.mouse.down();
    expect(await bg(chip), "a press changes it again").not.toBe(hover);
    await page.mouse.move(0, 0);
    await page.mouse.up();
    await expect(page).not.toHaveURL(/kind=/);

    const row = rows.first();
    const rowRest = await bg(row);
    await row.locator('td[data-col="state"]').hover();
    expect(await bg(row), "hover tints the row").not.toBe(rowRest);
    await shot(page, `hover-${theme.mode}`);
  });
}
