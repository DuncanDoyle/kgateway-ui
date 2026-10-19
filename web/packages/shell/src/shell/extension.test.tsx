import { render, screen, waitFor } from "@testing-library/react";
import { SWRConfig } from "swr";
import { controlRoomDark, defaultPages, Shell, THEME_PAINT_KEY, type Theme } from "../index";
import { overview, unhealthy } from "../test/fixtures";
import { stubApi } from "../test/render";

// A downstream with its own brand, theme, page, nav entry and finding renderer, registered through the public shell API only.
const acmeTheme: Theme = {
  ...controlRoomDark,
  id: "acme",
  name: "Acme",
  color: { ...controlRoomDark.color, accent: "#7A1FA2", bg: "#101820" },
  font: { ...controlRoomDark.font, ui: "Acme Sans", size: { ...controlRoomDark.font.size, sm: "13px" } },
};

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

it("a downstream extension contributes brand, theme, page, nav entry and finding renderer", async () => {
  const now = Date.now();
  stubApi({
    "/api/v1/installs": [
      {
        id: "kgw/kgateway",
        namespace: "kgw",
        name: "kgateway",
        version: "9.9.9",
        edition: "acme",
        controllerNames: [],
        controller: { ready: 1, desired: 1 },
        scope: { mode: "all", watched: 1, total: 1 },
        health: { state: "error", counts: { error: 1 } },
        computedAt: new Date(now).toISOString(),
      },
    ],
    "/api/v1/meta": { version: "dev", synced: true, kinds: [] },
    "/api/v1/installs/kgw/kgateway/overview": overview(now),
    "/api/v1/installs/kgw/kgateway/health": {
      ...unhealthy(now),
      findings: [
        { code: "ACME-001", slug: "demo", severity: "error", install: "kgw/kgateway", message: "Downstream finding" },
      ],
    },
  });
  window.history.pushState({}, "", "/i/kgw/kgateway");
  render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <Shell
        pages={defaultPages}
        extension={{
          brand: { productName: "Acme Edge", logo: <span>A</span> },
          themes: [acmeTheme],
          defaultThemeId: "acme",
          routes: [{ path: "widgets", element: <p>widgets page</p>, scope: "install" }],
          navEntries: [{ label: "Widgets", to: "widgets", scope: "install" }],
          findingRenderers: { "ACME-": () => <span>acme detail</span> },
        }}
      />
    </SWRConfig>,
  );
  expect(await screen.findByText("Acme Edge")).toBeInTheDocument();
  expect(await screen.findByText("acme detail")).toBeInTheDocument();
  expect(screen.getByTestId("edition")).toHaveTextContent("acme");
  // The contributed tokens apply: colour, font family, type scale, and the pre-React paint record.
  expect(screen.getByTestId("version")).toHaveStyle({ backgroundColor: "#7A1FA2", fontSize: "13px" });
  expect(document.body).toHaveStyle({ fontFamily: "Acme Sans" });
  expect(JSON.parse(localStorage.getItem(THEME_PAINT_KEY) ?? "{}")).toEqual({ bg: "#101820", scheme: "dark" });
  // The nav entry links to the contributed page, which renders on navigation.
  expect(screen.getByRole("link", { name: "Widgets" })).toHaveAttribute("href", "/i/kgw/kgateway/widgets");
  window.history.pushState({}, "", "/i/kgw/kgateway/widgets");
  window.dispatchEvent(new PopStateEvent("popstate"));
  await waitFor(() => expect(screen.getByText("widgets page")).toBeInTheDocument());
});
