import { readFileSync } from "node:fs";
import { expect, type Locator, type Page, test } from "@playwright/test";
import type {
  Diagnostics,
  EndpointsEvidence,
  FindingDetail,
  FindingList,
  GatewayDetail,
  GroupList,
  ProcessesResponse,
  XDSRejectsEvidence,
} from "../packages/shell/src/api/types";
import {
  dataPlaneCopy,
  diagnosticsCopy,
  findingsCopy,
  fixCopy,
  groupsCopy,
  listCopy,
  navCopy,
  processesCopy,
} from "../packages/shell/src/copy/explorer";

// The 3.2 health pages over test/e2e/fixtures/nack.yaml (Task 53), the 3.3 analyzer pages over
// fixtures/analyzer.yaml (Task 72) and the 3.4 backend pages over fixtures/backends.yaml (Task 82). Every value asserted comes from the API response the page renders
// (R62): the SPA shows the server's states and derives none.
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

/** The dd that follows the dt labelled term in scope. */
const factValue = (scope: Locator, term: string) =>
  scope.locator("dt", { hasText: term }).locator("xpath=following-sibling::dd[1]");

/** The finding with code on namespace/name, from the flat list. */
async function findingOn(page: Page, code: string, id: string) {
  const list = await fetchJSON<FindingList>(page, `/findings?code=${code}&limit=200`);
  const f = list.items.find((x) => x.resource && `${x.resource.namespace}/${x.resource.name}` === id);
  expect(f, `a ${code} on ${id}`).toBeTruthy();
  return f as NonNullable<typeof f>;
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

    // Probe facts come only with a counted failure, so the column follows the server and never reads 0.
    const probes = p.controller.processes.some((x) => x.probe);
    await expect(ctrl.getByRole("columnheader", { name: diagnosticsCopy.probe })).toHaveCount(probes ? 1 : 0);
    await expect(page.getByText(/failed 0 times/)).toHaveCount(0);

    const proxies = page.getByRole("table", { name: processesCopy.gatewayProxies });
    const rows = await dataRows(proxies);
    await expect(rows).toHaveCount(p.proxies.items.length);
    for (const item of p.proxies.items) {
      const id = `${item.gateway.namespace}/${item.gateway.name}`;
      const r = rows.filter({ has: page.getByRole("link", { name: id, exact: true }) });
      if (!item.set.observed) await expect(r).toContainText(diagnosticsCopy.notObserved);
      if (item.rejects !== undefined)
        await expect(r.getByRole("cell", { name: String(item.rejects), exact: true })).toHaveCount(1);
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
    expect(names, "q=nack filters out the other Gateways").not.toContain("infra/edge");
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
    // Read or not read, as the server says; never left out (D-006).
    await expect(factValue(section, dataPlaneCopy.controllerLog)).toHaveText(
      diagnosticsCopy.logState[dp.controllerLogs] as string,
    );

    const proxies = page.getByRole("table", { name: processesCopy.proxiesOf(nack) });
    await expect(proxies.locator("tbody > tr > td:first-child")).toHaveText(gw.proxies.processes.map((x) => x.name));
    const probes = gw.proxies.processes.some((x) => x.probe);
    await expect(proxies.getByRole("columnheader", { name: diagnosticsCopy.probe })).toHaveCount(probes ? 1 : 0);
    await expect(page.getByText(/failed 0 times/)).toHaveCount(0);
    const step = dp.nextSteps[0] as NonNullable<(typeof dp.nextSteps)[0]>;
    expect(step, "the data plane's first next step").toBeTruthy();
    const copy = section.getByRole("button", { name: findingsCopy.copyLabel(step.title), exact: true });
    await copy.hover();
    await shot(page, theme, "dataplane-copy-hover", section);
    await keyboardFocus(page, copy);
    await shot(page, theme, "gateway");
  });
}

for (const theme of themes) {
  test(`finding page: 014's ReferenceGrant copies, downloads and names its create command (${theme.name})`, async ({
    page,
    context,
  }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    const f = await findingOn(page, "KGWUI-014", "shop/no-grant");
    const d = await fetchJSON<FindingDetail>(page, `/findings/${encodeURIComponent(f.id)}`);
    const fix = d.suggestedFix;
    expect(fix?.create, "the server's fix creates a ReferenceGrant").toBe(true);
    if (!fix) return;
    await page.goto(`${install}/findings/${encodeURIComponent(f.id)}`);
    await pickTheme(page, theme);

    const section = page.getByRole("region", { name: findingsCopy.suggestedFix });
    await expect(section).toContainText(fix.title.replaceAll("`", ""));
    await expect(factValue(section, fixCopy.object)).toContainText(
      `${fix.target.kind} ${fix.target.namespace}/${fix.target.name}`,
    );
    await expect(factValue(section, fixCopy.basis)).toHaveText(fix.basis.replaceAll("`", ""));
    const [yaml, create] = [section.locator("pre").nth(0), section.locator("pre").nth(1)];
    expect(await yaml.textContent()).toBe(fix.yaml);
    // A new object downloads under its kind and name, the file the create command reads.
    const file = `${fix.target.kind.toLowerCase()}-${fix.target.name}.yaml`;
    await expect(create).toHaveText(`kubectl create -f ${file}`);

    const copy = section.getByRole("button", { name: fixCopy.copyLabel(fix.title), exact: true });
    await copy.hover();
    await shot(page, theme, "fix-copy-hover", section);
    await copy.click();
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(fix.yaml);
    const downloading = page.waitForEvent("download");
    await section.getByRole("button", { name: fixCopy.downloadLabel(file), exact: true }).click();
    const download = await downloading;
    expect(download.suggestedFilename()).toBe(file);
    const saved = await download.path();
    expect(readFileSync(saved, "utf8")).toBe(fix.yaml);
    await keyboardFocus(page, copy);
    await shot(page, theme, "fix-copy-focus", section);
    await shot(page, theme, "finding-fix");
  });

  test(`findings page: the grouped view shows the grant shape and opens its members (${theme.name})`, async ({
    page,
  }) => {
    const f = await findingOn(page, "KGWUI-014", "shop/no-grant");
    const groups = await fetchJSON<GroupList>(page, "/findings?view=groups&limit=200");
    const g = groups.items.find((x) => x.id === f.group);
    expect(g?.grant, "the 014's group is a Not permitted group with its grant shape").toBeTruthy();
    if (!g?.grant) return;
    await page.goto(`${install}/findings`);
    await pickTheme(page, theme);
    const view = page.getByRole("group", { name: groupsCopy.view });
    const toGroups = view.getByRole("button", { name: groupsCopy.groups });
    await toGroups.hover();
    await shot(page, theme, "view-switch-hover", view);
    await toGroups.click();
    await expect(page).toHaveURL(/[?&]view=groups(&|$)/);

    const table = page.getByRole("table", { name: findingsCopy.title });
    const rows = await dataRows(table);
    await expect(rows).toHaveCount(groups.items.length);
    const grant = diagnosticsCopy.grantShape(
      g.grant.fromNamespace,
      g.grant.toNamespace,
      g.grant.toGroup,
      g.grant.toKind,
    );
    const row = rows.filter({ hasText: grant });
    await expect(row).toHaveCount(1);
    await expect(row).toContainText(g.code);
    await expect(row).toContainText(g.title);
    await row.getByRole("link", { name: g.title }).hover();
    await shot(page, theme, "group-row-hover", row);
    await shot(page, theme, "groups");

    // The row opens the flat view of its members.
    await row.getByRole("link", { name: g.title }).click();
    await expect(page).toHaveURL(new RegExp(`[?&]group=${g.id}(&|$)`));
    const members = await dataRows(page.getByRole("table", { name: findingsCopy.title }));
    await expect(members).toHaveCount(g.members);
    await expect(members.getByRole("link", { name: "shop/no-grant", exact: true })).toHaveCount(1);
  });

  test(`finding page: 001's controller log events (${theme.name})`, async ({ page }) => {
    const f = await findingOn(page, "KGWUI-001", nack);
    const d = await fetchJSON<FindingDetail>(page, `/findings/${encodeURIComponent(f.id)}`);
    const ev = d.evidence as XDSRejectsEvidence;
    await page.goto(`${install}/findings/${encodeURIComponent(f.id)}`);
    await pickTheme(page, theme);
    const details = page.getByRole("region", { name: listCopy.details });
    await expect(factValue(details, diagnosticsCopy.controllerLog)).toHaveText(
      diagnosticsCopy.logState[ev.controllerLogs] as string,
    );
    const table = page.getByRole("table", { name: diagnosticsCopy.logEvents });
    const events = ev.logEvents ?? [];
    await expect(table).toHaveCount(events.length ? 1 : 0);
    for (const [i, l] of events.entries()) {
      const cells = table.locator("tbody tr").nth(i).locator("td");
      await expect(cells.nth(0)).toHaveAttribute("title", l.typeUrl);
      await expect(cells.nth(1)).toHaveText(l.classes.join(", "));
      await expect(cells.nth(2)).toHaveText((l.envoyResources ?? []).join(", "));
    }
    await shot(page, theme, "finding-logs");
  });
}

for (const theme of themes) {
  test(`finding page: 023 on a scaled-to-zero backend shows its Service, port and ready count (${theme.name})`, async ({
    page,
  }) => {
    // 023 needs a 30 s hold, so poll until the server raises it.
    let found: Awaited<ReturnType<typeof findingOn>> | undefined;
    await expect(async () => {
      found = await findingOn(page, "KGWUI-023", "shop/idle");
    }).toPass({ timeout: 90_000 });
    const f = found as NonNullable<typeof found>;
    const d = await fetchJSON<FindingDetail>(page, `/findings/${encodeURIComponent(f.id)}`);
    const ev = d.evidence as EndpointsEvidence;
    expect(ev.kind).toBe("endpoints");
    await page.goto(`${install}/findings/${encodeURIComponent(f.id)}`);
    await pickTheme(page, theme);

    const details = page.getByRole("region", { name: listCopy.details });
    await expect(details).toContainText((f.detail ?? "").replaceAll("`", ""));
    const svcValue = factValue(details, diagnosticsCopy.service);
    await expect(svcValue).toHaveText("shop/idle");
    // Services are watched for 023 but never served, so the ref has no page: link "none".
    expect(f.resource?.link).toBe("none");
    await expect(svcValue.getByRole("link")).toHaveCount(0);
    await expect(factValue(details, diagnosticsCopy.servicePort)).toHaveText(
      ev.portName ? `${ev.port} (${ev.portName})` : String(ev.port),
    );
    await expect(factValue(details, diagnosticsCopy.ready)).toHaveText(
      diagnosticsCopy.readyEndpoints(ev.ready, ev.total),
    );
    await expect(factValue(details, diagnosticsCopy.endpointSlices)).toHaveText(String(ev.slices));
    // The affected list is the route that names the Service port.
    const affected = page.getByRole("table", { name: findingsCopy.affected });
    await expect(affected.getByRole("link", { name: "shop/idle", exact: true })).toHaveCount(1);
    await shot(page, theme, "finding-endpoints");
  });

  test(`finding page: 013's one-port fix names the port and its location (${theme.name})`, async ({ page }) => {
    const f = await findingOn(page, "KGWUI-013", "shop/wrong-port");
    const d = await fetchJSON<FindingDetail>(page, `/findings/${encodeURIComponent(f.id)}`);
    const fix = d.suggestedFix;
    expect(fix?.create, "the server's fix changes the route in place").toBe(false);
    if (!fix) return;
    await page.goto(`${install}/findings/${encodeURIComponent(f.id)}`);
    await pickTheme(page, theme);

    const details = page.getByRole("region", { name: listCopy.details });
    await expect(details).toContainText((f.detail ?? "").replaceAll("`", ""));
    const section = page.getByRole("region", { name: findingsCopy.suggestedFix });
    await expect(section).toContainText(fix.title.replaceAll("`", ""));
    await expect(factValue(section, fixCopy.location)).toContainText(fix.path ?? "");
    await expect(factValue(section, fixCopy.basis)).toHaveText(fix.basis.replaceAll("`", ""));
    expect(await section.locator("pre").first().textContent()).toBe(fix.yaml);
    // A fragment has no create command.
    await expect(section.locator("pre")).toHaveCount(1);
    const copy = section.getByRole("button", { name: fixCopy.copyLabel(fix.title), exact: true });
    await copy.hover();
    await shot(page, theme, "port-fix-hover", section);
    await keyboardFocus(page, copy);
    await shot(page, theme, "finding-port-fix");
  });
}
