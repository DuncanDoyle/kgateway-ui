import { defaultPages, Shell } from "@snellius/shell";
import { render, screen, waitFor, within } from "@testing-library/react";
import symbolUrl from "./assets/snellius/snellius-symbol-dark.svg";
import { snelliusBrand } from "./brand";

const meta = { version: "dev", synced: true, platform: "Kubernetes", capabilities: [], kinds: [] };

beforeEach(() => {
  // No installs: the landing page renders without a cluster.
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => new Response(JSON.stringify(url === "/api/v1/meta" ? meta : []), { status: 200 })),
  );
  window.history.pushState({}, "", "/");
});

afterEach(() => vi.unstubAllGlobals());

it("the app header shows the Snellius symbol and wordmark, and titles the landing page snellius", async () => {
  render(<Shell extension={{ brand: snelliusBrand }} pages={defaultPages} />);
  const home = await screen.findByRole("link", { name: "snellius" });
  const logo = within(home).getByRole("presentation");
  expect(logo).toHaveAttribute("src", symbolUrl);
  expect(logo).toHaveAttribute("width", "24");
  expect(within(home).getByText("snellius")).toBeInTheDocument();
  await waitFor(() => expect(document.title).toBe("snellius"));
});

it("the Snellius brand carries the upstream project links for the About page", () => {
  expect(snelliusBrand.links).toEqual([
    { label: "Source code", href: "https://github.com/DuncanDoyle/snellius" },
    { label: "Documentation", href: "https://github.com/DuncanDoyle/snellius#readme" },
    { label: "Apache License 2.0", href: "https://github.com/DuncanDoyle/snellius/blob/main/LICENSE" },
  ]);
});
