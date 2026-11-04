import { expect, type Locator, type Page, test } from "@playwright/test";
import type { Diagnostics, GatewayDetail, ProcessesResponse } from "../packages/shell/src/api/types";
import {
  dataPlaneCopy,
  diagnosticsCopy,
  findingsCopy,
  navCopy,
  processesCopy,
} from "../packages/shell/src/copy/explorer";

// The 3.2 health pages over test/e2e/fixtures/nack.yaml (Task 53). Every value asserted comes from the
// API response the page renders (R62): the SPA shows the server's states and derives none.
const install = "/i/kgw-a.kgateway";
const api = "/api/v1/installs/kgw-a.kgateway";
const nack = "infra/nack";
const themes = [
  { name: "Control room", mode: "dark" },
  { name: "Control room light", mode: "light" },
] as const;
type Theme = (typeof themes)[number];

test.skip(process.env.NACK !== "1", "needs the NACK fixture (NACK=1 test/e2e/setup.sh)");

async function pickTheme(page: Page, theme: Theme) {
  await page.getByRole("button", { name: /^Theme:/ }).click();
  await page.getByRole("menuitemradio", { name: theme.name, exact: true }).click();
  await expect(page.locator("html")).toHaveCSS("color-scheme", theme.mode);
}

async function fetchJSON<T>(page: Page, path: string): Promise<T> {
  const res = await page.request.get(`${api}${path}`);
  expect(res.ok(), `GET ${path}`).toBeTruthy();
  return (await res.json()) as T;
}

/** A table's data rows once it is no longer loading. */
async function dataRows(table: Locator): Promise<Locator> {
  await expect(table).toHaveAttribute("aria-busy", "false");
  return table.locator("tbody > tr[data-row]");
}

/** Moves keyboard focus onto target, so :focus-visible applies as it would for a keyboard user. */
async function keyboardFocus(page: Page, target: Locator) {
  await target.focus();
  await page.keyboard.press("Shift+Tab");
  await page.keyboard.press("Tab");
  await expect(target).toBeFocused();
  await expect(target).toHaveCSS("outline-style", "solid");
}

function shot(page: Page, theme: Theme, name: string, target?: Locator) {
  const path = `${test.info().project.outputDir}/diagnostics-${name}-${theme.name.replace(/ /g, "-")}.png`;
  return target ? target.screenshot({ path }) : page.screenshot({ path, fullPage: true });
}

for (const theme of themes) {
  test(`install page: the nack group's next steps copy (${theme.name})`, async ({ page, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await page.goto(install);
    await pickTheme(page, themes.find((t) => t !== theme) as Theme);
    await pickTheme(page, theme);

    const d = await fetchJSON<Diagnostics>(page, "/diagnostics");
    const g = d.groups.find(
      (x) => x.code === "KGWUI-001" && x.cause && `${x.cause.namespace}/${x.cause.name}` === nack,
    );
    expect(g, "a KGWUI-001 group caused by infra/nack").toBeTruthy();
    const group = g as NonNullable<typeof g>;
    const steps = group.nextSteps ?? [];
    const first = steps[0] as NonNullable<(typeof steps)[0]>;
    expect(first, "the group's first next step").toBeTruthy();

    const attention = page.getByRole("region", { name: diagnosticsCopy.needsAttention });
    const row = attention.getByTestId("group-row").filter({ has: page.getByRole("link", { name: nack, exact: true }) });
    await expect(row).toHaveCount(1);
    await expect(row).toContainText(group.title);
    await expect(row).toContainText(group.code);

    const expander = row.getByRole("button", { name: diagnosticsCopy.nextStep });
    await expect(expander).toHaveAttribute("aria-expanded", "false");
    await expander.hover();
    await shot(page, theme, "group-hover", row);
    await expander.click();
    await expect(expander).toHaveAttribute("aria-expanded", "true");
    // Commands render as text, in the server's order.
    await expect(row.locator("pre")).toHaveText(steps.map((s) => s.command));

    const copy = row.getByRole("button", { name: findingsCopy.copyLabel(first.title), exact: true });
    await copy.click();
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(first.command);
    await keyboardFocus(page, copy);
    await shot(page, theme, "next-steps-focus", row);
    await shot(page, theme, "install");
  });

  test(`Processes page: server sets and a filter through the URL (${theme.name})`, async ({ page }) => {
    await page.goto(install);
    await pickTheme(page, theme);
    await page.getByRole("navigation", { name: navCopy.label }).getByRole("link", { name: navCopy.processes }).click();
    await expect(page).toHaveURL(new RegExp(`${install}/processes$`));
    const p = await fetchJSON<ProcessesResponse>(page, "/processes");

    const ctrl = page.getByRole("table", { name: processesCopy.controllerTable });
    await expect(ctrl.locator("tbody > tr > td:first-child")).toHaveText(p.controller.processes.map((x) => x.name));
    // A settled rollout has no reason, so nothing reads as stuck.
    const stuck = p.controller.rollout?.reason;
    expect(stuck, "the settled controller rollout has no stuck reason").toBeUndefined();
    await expect(page.getByText(/^Rollout stuck/)).toHaveCount(stuck ? 1 : 0);

    const proxies = page.getByRole("table", { name: processesCopy.gatewayProxies });
    const rows = await dataRows(proxies);
    await expect(rows).toHaveCount(p.proxies.items.length);
    for (const item of p.proxies.items) {
      const id = `${item.gateway.namespace}/${item.gateway.name}`;
      const r = rows.filter({ has: page.getByRole("link", { name: id, exact: true }) });
      if (!item.set.observed) await expect(r).toContainText(diagnosticsCopy.notObserved);
      if (item.rejects !== undefined) await expect(r).toContainText(String(item.rejects));
    }

    const chip = page.getByRole("button", { name: processesCopy.skew });
    await chip.hover();
    await shot(page, theme, "processes-chip-hover", page.getByRole("group", { name: processesCopy.showOnly }));
    await keyboardFocus(page, chip);

    // The search goes to the server through the URL and survives a reload.
    await page.getByRole("searchbox", { name: processesCopy.search }).fill("nack");
    await expect(page).toHaveURL(/[?&]q=nack(&|$)/);
    const filtered = await fetchJSON<ProcessesResponse>(page, "/processes?q=nack");
    const names = filtered.proxies.items.map((x) => `${x.gateway.namespace}/${x.gateway.name}`);
    expect(names).toContain(nack);
    await expect((await dataRows(proxies)).locator("td a")).toHaveText(names);
    await page.reload();
    await expect(page.getByRole("searchbox", { name: processesCopy.search })).toHaveValue("nack");
    await expect((await dataRows(proxies)).locator("td a")).toHaveText(names);
    await shot(page, theme, "processes");
  });

  test(`Gateway page: Data plane and Proxies of infra/nack (${theme.name})`, async ({ page }) => {
    await page.goto(`${install}/gateways/infra/nack`);
    await pickTheme(page, theme);
    const gw = await fetchJSON<GatewayDetail>(page, "/gateways/infra/nack");
    const dp = gw.dataPlane;
    expect(dp?.rejecting, "the server reports infra/nack rejecting").toBe(true);
    if (!dp || !gw.proxies) return;

    const section = page.getByRole("region", { name: dataPlaneCopy.title });
    await expect(section).toContainText(
      dp.statusSaysHealthy ? diagnosticsCopy.statusSaysHealthy : dataPlaneCopy.rejecting,
    );
    const types = section.getByRole("table", { name: diagnosticsCopy.rejectedTypes });
    await expect(types.locator("tbody td:first-child")).toHaveCount(dp.rejects.length);
    for (const [i, t] of dp.rejects.entries()) {
      const cells = types.locator("tbody tr").nth(i).locator("td");
      await expect(cells.nth(0)).toHaveAttribute("title", t.typeUrl);
      await expect(cells.nth(1)).toHaveText(String(t.streams));
    }
    await expect(section.locator("pre")).toHaveText(dp.nextSteps.map((s) => s.command));
    await expect(section).toContainText(
      dp.proxyStats.available ? dataPlaneCopy.proxyStatsOn : diagnosticsCopy.notAvailable,
    );

    const proxies = page.getByRole("table", { name: processesCopy.proxiesOf(nack) });
    await expect(proxies.locator("tbody > tr > td:first-child")).toHaveText(gw.proxies.processes.map((x) => x.name));
    const step = dp.nextSteps[0] as NonNullable<(typeof dp.nextSteps)[0]>;
    expect(step, "the data plane's first next step").toBeTruthy();
    const copy = section.getByRole("button", { name: findingsCopy.copyLabel(step.title), exact: true });
    await copy.hover();
    await shot(page, theme, "dataplane-copy-hover", section);
    await keyboardFocus(page, copy);
    await shot(page, theme, "gateway");
  });
}
