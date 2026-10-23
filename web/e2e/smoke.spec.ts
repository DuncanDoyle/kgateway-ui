import { expect, type Page, test } from "@playwright/test";

const second = process.env.SECOND_INSTALL === "1";
const themes = [
  { name: "Control room", mode: "dark" },
  { name: "Control room light", mode: "light" },
] as const;
type Theme = (typeof themes)[number];

/** Picks a theme and asserts the switcher and the document both changed to it. */
async function pickTheme(page: Page, theme: Theme) {
  await page.getByRole("button", { name: /^Theme:/ }).click();
  await page.getByRole("menuitemradio", { name: theme.name, exact: true }).click();
  await expect(page.getByRole("button", { name: `Theme: ${theme.name}`, exact: true })).toBeVisible();
  await expect(page.locator("html")).toHaveCSS("color-scheme", theme.mode);
}

/** Asserts the install page for kgw-a has rendered from live data. */
async function expectInstallPage(page: Page) {
  await expect(page.getByTestId("health-strip")).toContainText(/error/i);
  await expect(page.getByRole("table", { name: "Gateways" })).toContainText("edge");
}

for (const theme of themes) {
  test(`install page, findings, route page and YAML (${theme.name})`, async ({ page }) => {
    await page.goto("/");
    if (second) {
      // The card shows the display name; its link carries the opaque ID.
      await expect(page.getByRole("link", { name: /kgw-b\/kgateway/ })).toBeVisible();
      await page.getByRole("link", { name: /kgw-a\/kgateway/ }).click();
    }
    await expect(page).toHaveURL(/\/i\/kgw-a\.kgateway$/);
    // Switch away first so the target theme is always a real change.
    await pickTheme(page, themes.find((t) => t !== theme) as Theme);
    await pickTheme(page, theme);
    await expectInstallPage(page);
    await page.getByRole("link", { name: /Show all/ }).click();
    await expect(page).toHaveURL(/\/i\/kgw-a\.kgateway\/findings$/);
    await expect(page.getByText("Namespace not watched by the controller")).toBeVisible();
    // The list is virtualised and the explorer fixture adds many findings: filter to the info one, then clear.
    const filter = page.getByRole("textbox", { name: "Filter findings" });
    await filter.fill("KGWUI-010");
    await expect(page.getByText("Self-managed proxy: the UI cannot see its readiness")).toBeVisible();
    await filter.fill("");
    await page
      .getByRole("link", { name: /broken/ })
      .first()
      .click();
    // A route finding opens the route's detail page; its YAML tab shows the status as written.
    await expect(page).toHaveURL(/\/routes\/gateway\.networking\.k8s\.io\/HTTPRoute\/[^/]+\/broken$/);
    await page.getByRole("tab", { name: "YAML" }).click();
    await expect(page.getByTestId("yaml")).toContainText("BackendNotFound");
    // outputDir is resolved from the config, so the path does not depend on the cwd.
    await page.screenshot({
      path: `${test.info().project.outputDir}/smoke-${theme.name.replace(/ /g, "-")}.png`,
      fullPage: true,
    });
  });
}

test("deep link and reload keep the install page", async ({ page }) => {
  await page.goto("/i/kgw-a.kgateway");
  await expectInstallPage(page);
  await page.reload();
  await expect(page).toHaveURL(/\/i\/kgw-a\.kgateway$/);
  await expectInstallPage(page);
});
