import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter } from "react-router-dom";
import { SWRConfig, type SWRConfiguration } from "swr";
import { retryEveryPoll } from "../api/hooks";
import { ThemeProvider } from "../theme/ThemeProvider";
import { defaultThemes } from "../theme/themes";

/** SWR for tests: a fresh cache, no deduping, and the app's own error retry, so the two cannot drift. */
export const testSWR = (): SWRConfiguration => ({
  provider: () => new Map(),
  dedupingInterval: 0,
  onErrorRetry: retryEveryPoll,
});

/** Renders ui with the default themes (or themeId), a fresh SWR cache and a router at `path`. */
export function renderWithTheme(ui: ReactElement, opts: { path?: string; themeId?: string } = {}) {
  return render(
    <SWRConfig value={testSWR()}>
      <ThemeProvider themes={defaultThemes} defaultThemeId={opts.themeId}>
        <MemoryRouter initialEntries={[opts.path ?? "/"]}>{ui}</MemoryRouter>
      </ThemeProvider>
    </SWRConfig>,
  );
}

/** Stubs fetch with a map of path → JSON body (status 200) or [status, body]; serverNow sets each response's Date. */
export function stubApi(routes: Record<string, unknown>, serverNow?: () => number) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const r = routes[url];
      const headers = serverNow ? { Date: new Date(serverNow()).toUTCString() } : undefined;
      if (r === undefined) return new Response(JSON.stringify({ error: "not found" }), { status: 404, headers });
      const [status, body] = Array.isArray(r) && typeof r[0] === "number" ? (r as [number, unknown]) : [200, r];
      return new Response(JSON.stringify(body), { status, headers });
    }),
  );
}

/** The declarations of every injected CSS rule that targets one of el's classes with `pseudo`, e.g. ":hover". */
export function pseudoRules(el: Element, pseudo: string): string[] {
  const css = [...document.querySelectorAll("style")].map((s) => s.textContent ?? "").join("");
  const classes = [...el.classList].map((c) => `.${c}`);
  return [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
    .filter(([, sel = ""]) => sel.includes(pseudo) && classes.some((c) => sel.includes(c)))
    .map(([, , body = ""]) => body);
}

/** el's href without the trail's `via`, for tests about where a link opens rather than the path taken (D-035). */
export function hrefPath(el: Element): string | null {
  const href = el.getAttribute("href");
  if (href === null || !href.includes("via=")) return href;
  const [path = "", query = ""] = href.split("?");
  const q = new URLSearchParams(query);
  q.delete("via");
  const rest = q.toString();
  return rest ? `${path}?${rest}` : path;
}
