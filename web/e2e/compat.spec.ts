import { expect, type Page, test } from "@playwright/test";
import type { InstallSummary } from "../packages/shell/src/api/types";
import { delegationCopy, diagnosticsCopy, navCopy } from "../packages/shell/src/copy/explorer";

// Smoke test of the core-only build against any Gateway API controller (Phase 5 design §12.1).
// Runs only with COMPAT_UI_URL set (playwright.config.ts); inputs as for test/compat.
const env = (name: string): string => {
  const v = process.env[name];
  if (!v) throw new Error(`${name} is required`);
  return v;
};
const controller = env("COMPAT_CONTROLLER");
const gateway = env("COMPAT_GATEWAY");
const route = env("COMPAT_ROUTE");

test.use({ baseURL: env("COMPAT_UI_URL") });

/** The install page path of the controller's class-only install, from the API. */
async function installPath(page: Page): Promise<string> {
  const insts: InstallSummary[] = await (await page.request.get("/api/v1/installs")).json();
  const inst = insts.find((i) => i.controllerNames.includes(controller));
  expect(inst, `an install for ${controller}`).toBeDefined();
  return `/i/${(inst as InstallSummary).id}`;
}

test("install page lists the Gateway, with no process surfaces and no error panel", async ({ page }) => {
  await page.goto(await installPath(page));
  await expect(page.getByRole("table", { name: "Gateways" })).toContainText(gateway.split("/")[1] ?? gateway);
  // No processes feature: neither the Processes block nor its nav entry.
  await expect(page.getByRole("heading", { name: diagnosticsCopy.processesTitle })).toHaveCount(0);
  await expect(
    page.getByRole("navigation", { name: navCopy.label }).getByRole("link", { name: navCopy.processes }),
  ).toHaveCount(0);
  await expect(page.getByRole("alert")).toHaveCount(0);
});

test("route page has no delegation tab and no error panel", async ({ page }) => {
  const [ns, name] = route.split("/");
  await page.goto(`${await installPath(page)}/routes/gateway.networking.k8s.io/HTTPRoute/${ns}/${name}`);
  await expect(page.getByRole("tab", { name: "YAML" })).toBeVisible();
  await expect(page.getByRole("tab", { name: delegationCopy.tab })).toHaveCount(0);
  await expect(page.getByRole("alert")).toHaveCount(0);
});
