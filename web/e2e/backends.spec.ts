import { expect, type Locator, type Page, test } from "@playwright/test";
import type { BackendDetail, BackendsResponse } from "../packages/shell/src/api/types";
import { backendsCopy, exposureCopy, portChainCopy, statusCopy, usedByCopy } from "../packages/shell/src/copy/backends";
import { stateLabel } from "../packages/shell/src/health/state";

// The Backends list and backend detail (Phase 6 sub-phase 6.3) over test/e2e/fixtures/traffic.yaml: the
// zero-endpoint Service traffic/asleep, the cross-namespace ref traffic/cross → shop/httpbin without a grant, and
// GatewayExtension traffic/authz. States come from the API response the page renders.
const install = "/i/kgw-a.kgateway";
const api = "/api/v1/installs/kgw-a.kgateway";
const themes = [
  { name: "Control room", mode: "dark" },
  { name: "Control room light", mode: "light" },
] as const;

// NACK's Cluster reject reaches every proxy of the install, so traffic/traffic is an error there (KGWUI-001).
test.skip(process.env.NACK === "1", "needs traffic/traffic's proxy to accept its config (no NACK fixture)");

async function pickTheme(page: Page, theme: (typeof themes)[number]) {
  await page.getByRole("button", { name: /^Theme:/ }).click();
  await page.getByRole("menuitemradio", { name: theme.name, exact: true }).click();
  await expect(page.locator("html")).toHaveCSS("color-scheme", theme.mode);
}

async function get<T>(page: Page, path: string): Promise<T> {
  const res = await page.request.get(`${api}/${path}`);
  expect(res.ok(), `GET ${path}`).toBeTruthy();
  return (await res.json()) as T;
}

/** Waits until 023's hold has passed and traffic/asleep is a warning (the controller's ruling). */
async function asleepIsWarning(page: Page) {
  await expect(async () => {
    const d = await get<BackendsResponse>(page, "backends?namespace=traffic");
    expect(d.rows.find((r) => r.ref.name === "asleep")?.problem.state).toBe("warning");
  }).toPass({ timeout: 120_000 });
}

/** The table's data rows once it is no longer loading. */
async function dataRows(page: Page, name: string): Promise<Locator> {
  const table = page.getByRole("table", { name });
  await expect(table).toHaveAttribute("aria-busy", "false");
  return table.locator("tbody > tr[data-row]");
}

function shot(page: Page, name: string) {
  const dir = process.env.BACKENDS_SHOTS ?? test.info().project.outputDir;
  return page.screenshot({ path: `${dir}/backends-${name}.png`, fullPage: true });
}

test("Backends lists the zero-endpoint Service first in its namespace, and its detail names the impact", async ({
  page,
}) => {
  await asleepIsWarning(page);
  await page.goto(install);
  await page.getByRole("navigation", { name: "Install" }).getByRole("link", { name: backendsCopy.title }).click();
  await expect(page).toHaveURL(new RegExp(`${install}/backends$`));
  await page
    .getByRole("group", { name: backendsCopy.namespaces })
    .getByRole("button", { name: "traffic", exact: true })
    .click();
  await expect(page).toHaveURL(/namespace=traffic/);
  const rows = await dataRows(page, backendsCopy.title);
  const d = await get<BackendsResponse>(page, "backends?namespace=traffic");
  expect(d.rows[0]?.ref.name, "problems first").toBe("asleep");
  await expect(rows).toHaveCount(d.rows.length);
  const first = rows.first();
  await expect(first.locator('td[data-col="name"]')).toHaveText("traffic/asleep");
  await expect(first.getByTestId("path-state")).toHaveText(stateLabel("warning"));
  await expect(first.locator('td[data-col="endpoints"]')).toHaveText("Port 8000: 0/0 ready");
  // The namespace facet ignores its own filter, so the other namespaces stay selectable.
  const shop = page
    .getByRole("group", { name: backendsCopy.namespaces })
    .getByRole("button", { name: "shop", exact: true });
  await expect(shop).toBeVisible();

  await first.locator('td[data-col="kind"]').click();
  await expect(page).toHaveURL(new RegExp(`${install}/backends/-/Service/traffic/asleep`));
  const line = page.getByTestId("status-line");
  await expect(line).toHaveText(
    `${stateLabel("warning")}1 route on 1 Gateway sends traffic here; ${statusCopy.noReady("requests fail")}.`,
  );
  // A Service has no YAML tab.
  await expect(page.getByRole("tab", { name: "Overview" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "YAML" })).toHaveCount(0);
  // The complete read found no slice port for the unnamed Service port: a fact, not a judgement.
  await expect(page.getByTestId("port-chain")).toContainText(portChainCopy.noSlicePortUnnamed);
});

test("How this backend is exposed opens Traffic with backend=", async ({ page }) => {
  await page.goto(`${install}/backends/-/Service/traffic/web-a`);
  const paths = await dataRows(page, exposureCopy.table);
  const detail = await get<BackendDetail>(page, "backends/-/Service/traffic/web-a");
  await expect(paths).toHaveCount(detail.exposure.rows.length);
  await page.getByRole("link", { name: exposureCopy.openTraffic }).click();
  await expect(page).toHaveURL(new RegExp(`${install}/traffic\\?backend=core%2FService%2Ftraffic%2Fweb-a`));
  await expect(page.getByText("Backend: Service traffic/web-a")).toBeVisible();
  await expect(await dataRows(page, "Paths")).toHaveCount(detail.exposure.summary.paths);
});

test("the cross-namespace ref shows the suggested ReferenceGrant", async ({ page }) => {
  await page.goto(`${install}/backends/-/Service/shop/httpbin`);
  const e = page.getByTestId("explanation").filter({ hasText: "traffic/cross" });
  await expect(e).toContainText("RefNotPermitted");
  await expect(e.getByRole("heading", { name: "Suggested fix" })).toBeVisible();
  await expect(e).toContainText("kind: ReferenceGrant");
  await expect(e).toContainText("namespace: traffic");
  // Port 9999 is written by a route but not on the Service: said so, with no read time.
  const lacks = page.getByTestId("endpoints-source").filter({ hasText: "Port 9999" });
  await expect(lacks.getByTestId("endpoints-summary")).toHaveText(portChainCopy.noSuchPort(9999));
  await expect(lacks).not.toContainText("Read ");
  // Its paths include the error the 014 finding attributes.
  await expect(page.getByTestId("status-line")).toHaveAttribute("data-state", "error");
});

test("a route's backend links to backend detail, and a GatewayExtension shows as a consumer", async ({ page }) => {
  await page.goto(`${install}/routes/gateway.networking.k8s.io/HTTPRoute/traffic/cross`);
  await page.getByTestId("backend-httpbin").getByRole("link", { name: "shop/httpbin" }).click();
  await expect(page).toHaveURL(new RegExp(`${install}/backends/-/Service/shop/httpbin`));
  await expect(page.getByTestId("status-line")).toBeVisible();

  // No route writes traffic/authz, so the list counts the extension as its one other user.
  await page.goto(`${install}/backends?namespace=traffic`);
  const authz = (await dataRows(page, backendsCopy.title)).filter({
    has: page.locator('td[data-col="name"]', { hasText: /^traffic\/authz$/ }),
  });
  await expect(authz.locator('td[data-col="used"]')).toHaveText(usedByCopy.uses(0, 0, 1));
  await authz.locator('td[data-col="kind"]').click();
  await expect(page).toHaveURL(new RegExp(`${install}/backends/-/Service/traffic/authz`));
  await expect(page.getByTestId("consumer")).toContainText("Used by GatewayExtension traffic/authz (ext-auth)");
  await expect(page.getByTestId("status-line")).toContainText("1 other object uses it");
  // A complete read lists its endpoint with the Pod.
  await expect(page.getByTestId("endpoints-summary")).toHaveText("1/1 ready");
  await expect(page.getByRole("table", { name: /Endpoints on Port 9001/ })).toContainText("echo-");
});

for (const theme of themes) {
  test(`list and detail in ${theme.name}, with hover and active styles`, async ({ page }) => {
    await asleepIsWarning(page);
    await page.setViewportSize({ width: 1920, height: 1080 });
    await page.goto(`${install}/backends`);
    await pickTheme(page, theme);
    const rows = await dataRows(page, backendsCopy.title);
    await expect(rows.first()).toBeVisible();
    await shot(page, `list-${theme.mode}`);

    const chip = page.getByRole("group", { name: backendsCopy.show }).getByRole("button", { name: "Problems" });
    const bg = (l: Locator) => l.evaluate((el) => getComputedStyle(el).backgroundColor);
    const rest = await bg(chip);
    await chip.hover();
    const hover = await bg(chip);
    expect(hover, "hover changes the chip background").not.toBe(rest);
    await page.mouse.down();
    expect(await bg(chip), "a press changes it again").not.toBe(hover);
    await page.mouse.move(0, 0);
    await page.mouse.up();

    const row = rows.first();
    const rowRest = await bg(row);
    await row.locator('td[data-col="kind"]').hover();
    expect(await bg(row), "hover changes the row background").not.toBe(rowRest);

    await page.goto(`${install}/backends/-/Service/traffic/asleep`);
    await expect(page.getByTestId("status-line")).toHaveAttribute("data-state", "warning");
    await shot(page, `detail-asleep-${theme.mode}`);
    await page.goto(`${install}/backends/-/Service/shop/httpbin`);
    await expect(page.getByTestId("explanation").first()).toBeVisible();
    await shot(page, `detail-httpbin-${theme.mode}`);
    await page.goto(`${install}/backends/gateway.kgateway.dev/Backend/shop/static-be`);
    await expect(page.getByTestId("status-line")).toBeVisible();
    await shot(page, `detail-backend-${theme.mode}`);
  });
}
