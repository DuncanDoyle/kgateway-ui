import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter } from "react-router-dom";
import { SWRConfig, type SWRConfiguration } from "swr";
import { POLL_MS } from "../api/hooks";
import { ThemeProvider } from "../theme/ThemeProvider";
import { defaultThemes } from "../theme/themes";

/**
 * SWR for tests: a fresh cache, no deduping, and a deterministic error retry. SWR skips interval polls while a key
 * holds an error, so its retry is what recovers; here it runs once per poll interval, without jitter or backoff.
 */
export const testSWR = (): SWRConfiguration => ({
  provider: () => new Map(),
  dedupingInterval: 0,
  onErrorRetry: (_e, _key, _config, revalidate, { retryCount }) => {
    setTimeout(() => revalidate({ retryCount }), POLL_MS);
  },
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

/** Stubs fetch with a map of path → JSON body (status 200) or [status, body]. */
export function stubApi(routes: Record<string, unknown>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const r = routes[url];
      if (r === undefined) return new Response(JSON.stringify({ error: "not found" }), { status: 404 });
      const [status, body] = Array.isArray(r) && typeof r[0] === "number" ? (r as [number, unknown]) : [200, r];
      return new Response(JSON.stringify(body), { status });
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
