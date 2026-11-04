import { expect, type Page, type Route, test } from "@playwright/test";
import type { Diagnostics, GroupRow } from "../packages/shell/src/api/types";
import { diagnosticsCopy } from "../packages/shell/src/copy/explorer";
import { diagnostics, groupRow, installSummary, meta, overview, unhealthy } from "../packages/shell/src/test/fixtures";

// The install skeleton reserves Needs attention from InstallSummary.groupRows (D-045): releasing diagnostics
// must move nothing. The API is stubbed, so the rows carry the longest content a collapsed row shows.
const id = "kgw.kgateway";
const api = `/api/v1/installs/${id}`;

/** Groups with every meta field, a restricted one and a bare one, plus long names that must truncate. */
function groups(): GroupRow[] {
  const since = new Date(Date.now() - 3 * 86_400_000).toISOString();
  const long = "a-route-name-long-enough-to-truncate-in-every-layout-this-page-has-at-any-width";
  const step = { title: "Show the rejected config", command: "kubectl get gateway -n prod edge -o yaml" };
  return [
    groupRow(0, {
      title: `Configuration rejected by Envoy on every listener of ${long}`,
      code: "KGWUI-001",
      severity: "error",
      class: 1,
      cause: { group: "gateway.networking.k8s.io", kind: "Gateway", namespace: "prod", name: long, link: "detail" },
      members: 14,
      affectedTotal: 1280,
      since,
      nextSteps: [step],
    }),
    groupRow(1, { severity: "error", restricted: true, title: "", cause: undefined, members: 3 }),
    groupRow(2, { members: 2, affectedTotal: 3, since, nextSteps: [step] }),
    groupRow(3, { severity: "info", cause: undefined }),
  ];
}

/** Stubs the API; the first diagnostics request waits until the returned release is called. */
async function stubApi(page: Page, diag: Diagnostics): Promise<() => void> {
  let release = () => {};
  const released = new Promise<void>((r) => (release = r));
  const json = (body: unknown) => ({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
  const routes: Record<string, () => unknown> = {
    "/api/v1/installs": () => [installSummary(Date.now(), {}, diag.groups.length)],
    "/api/v1/meta": () => meta,
    [`${api}/overview`]: () => overview(),
  };
  // Match on the path: the dev server also serves source modules under paths that contain "/api/".
  await page.route(
    (u) => u.pathname.startsWith("/api/"),
    async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === `${api}/diagnostics`) {
        await released;
        return route.fulfill(json({ ...diag, collectedAt: new Date().toISOString() }));
      }
      const body = routes[path];
      return body ? route.fulfill(json(body())) : route.fulfill({ status: 404, body: `unstubbed ${path}` });
    },
  );
  return release;
}

interface Box {
  top: number;
  bottom: number;
}
interface Layout {
  heading: Box;
  rows: Box[];
  foot: Box;
  processes: number;
}

/** The Needs attention heading, rows (inside their borders) and footer, and the Processes heading's top. */
function layout(page: Page, rowSel: string, footSel: string): Promise<Layout> {
  return page.evaluate(
    ([rowSel, footSel, title]) => {
      const inner = (el: Element): Box => {
        const r = el.getBoundingClientRect();
        const s = getComputedStyle(el);
        return { top: r.top + parseFloat(s.borderTopWidth), bottom: r.bottom - parseFloat(s.borderBottomWidth) };
      };
      const rows = [...document.querySelectorAll(rowSel)];
      const box = rows[0]?.parentElement as HTMLElement;
      const h2 = [...document.querySelectorAll("h2")].find((h) => h.textContent === title) as HTMLElement;
      return {
        heading: inner(box.previousElementSibling as HTMLElement),
        rows: rows.map(inner),
        foot: inner(box.querySelector(footSel) as HTMLElement),
        processes: h2.getBoundingClientRect().top,
      };
    },
    [rowSel, footSel, diagnosticsCopy.processesTitle] as const,
  );
}

/** Asserts every coordinate of got is within 1px (rounding) of want. */
function expectSame(got: Layout, want: Layout) {
  const flat = (l: Layout) => [l.heading, ...l.rows, l.foot].flatMap((b) => [b.top, b.bottom]).concat(l.processes);
  const g = flat(got);
  const w = flat(want);
  expect(g).toHaveLength(w.length);
  for (const [i, v] of g.entries())
    expect(Math.abs(v - (w[i] as number)), `coordinate ${i}: ${v} vs ${w[i]}`).toBeLessThanOrEqual(1);
}

for (const width of [720, 1440]) {
  for (const fonts of ["web", "fallback"] as const) {
    test(`Needs attention keeps its place when diagnostics arrive (${width} px, ${fonts} fonts)`, async ({ page }) => {
      await page.setViewportSize({ width, height: 1200 });
      // Held font files make the first render stop at its font cap and keep the fallback fonts.
      const fontLoads: Route[] = [];
      if (fonts === "fallback") await page.route(/\.woff2(\?|$)/, (r) => void fontLoads.push(r));
      const diag = diagnostics(unhealthy(), groups());
      const release = await stubApi(page, diag);
      // The held font would keep the load event from firing.
      await page.goto(`/i/${id}`, { waitUntil: "domcontentloaded" });
      // The page renders once fonts settle or the wait hits its cap, so the skeleton shows which happened.
      await expect(page.getByTestId("group-row-shimmer")).toHaveCount(diag.groups.length, { timeout: 10_000 });
      if (fonts === "fallback") await expect(page.locator("html")).toHaveClass(/font-fallback/);
      else expect(await page.evaluate(() => document.fonts.check("1em 'Figtree Variable'"))).toBe(true);
      const before = await layout(page, "[data-testid=group-row-shimmer]", "[data-testid=show-all-shimmer]");
      release();
      await expect(page.getByTestId("group-row")).toHaveCount(diag.groups.length);
      await expect(page.getByTestId("needs-attention-shimmer")).toHaveCount(0);
      expectSame(await layout(page, "[data-testid=group-row]", ".more"), before);
      // A late font load still moves nothing: the theme left the web fonts out for the session.
      for (const r of fontLoads) await r.continue();
      await page.evaluate(() => document.fonts.ready);
      expectSame(await layout(page, "[data-testid=group-row]", ".more"), before);
    });
  }
}
