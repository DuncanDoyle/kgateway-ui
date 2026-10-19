import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter } from "react-router-dom";
import { SWRConfig } from "swr";
import { ThemeProvider } from "../theme/ThemeProvider";
import { defaultThemes } from "../theme/themes";

/** Renders ui with the default themes, a fresh SWR cache and a router at `path`. */
export function renderWithTheme(ui: ReactElement, opts: { path?: string } = {}) {
  return render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <ThemeProvider themes={defaultThemes}>
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
