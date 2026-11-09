import { defaultPages, Shell } from "@snellius/shell";
import { render, screen } from "@testing-library/react";
import { SWRConfig } from "swr";
import { appExtension } from "./extension";

const meta = { version: "dev", synced: true, platform: "Kubernetes", capabilities: [], kinds: [] };

/** The app as main.tsx mounts it, against a server that runs packs (absent: an older server). */
function mount(packs: string[] | undefined) {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(JSON.stringify(url === "/api/v1/meta" ? { ...meta, packs } : []), { status: 200 }),
    ),
  );
  window.history.pushState({}, "", "/");
  return render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <Shell extension={appExtension} pages={defaultPages} />
    </SWRConfig>,
  );
}

afterEach(() => vi.unstubAllGlobals());

it.each([
  ["the default binary", ["kgateway"]],
  ["an older server", undefined],
])("on %s the empty state is today's kgateway copy", async (_, packs) => {
  mount(packs);
  expect(await screen.findByText("No kgateway install found")).toBeInTheDocument();
  expect(screen.getByText("kgateway=kgateway")).toBeInTheDocument();
});

it("on the core-only binary the empty state names no implementation", async () => {
  mount([]);
  expect(await screen.findByText("No Gateway API controller found")).toBeInTheDocument();
  expect(document.body.textContent).not.toMatch(/kgateway/);
});
