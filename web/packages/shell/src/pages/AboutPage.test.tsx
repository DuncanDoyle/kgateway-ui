import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SWRConfig } from "swr";
import { type CorePages, defaultBrand, type ShellExtension } from "../shell/extension";
import { Shell } from "../shell/Shell";
import { pseudoRules, stubApi, testSWR } from "../test/render";

const pages: CorePages = {
  ClusterLanding: () => <p>cluster landing</p>,
  InstallPage: () => <p>install page</p>,
  FindingsPage: () => <p>findings page</p>,
  ResourcePage: () => <p>resource page</p>,
};
const meta = { version: "v0.6.0", synced: true, platform: "Kubernetes", capabilities: [], kinds: [] };
const install = {
  id: "kgw.kgateway",
  displayName: "kgw/kgateway",
  namespace: "kgw",
  name: "kgateway",
  version: "2.5.0",
  edition: "kgateway",
  controllerNames: ["kgateway.dev/kgateway"],
  readiness: { state: "ready" },
  health: { state: "healthy", counts: {} },
  groupRows: 0,
  computedAt: new Date().toISOString(),
};

function mount(path: string, ext: ShellExtension = {}) {
  window.history.pushState({}, "", path);
  return render(
    <SWRConfig value={testSWR()}>
      <Shell extension={ext} pages={pages} />
    </SWRConfig>,
  );
}

afterEach(() => vi.unstubAllGlobals());

it("the header About button opens the About page", async () => {
  stubApi({ "/api/v1/installs": [install, { ...install, id: "b.two" }], "/api/v1/meta": meta });
  mount("/");
  const brand = await screen.findByRole("link", { name: "snellius" });
  expect(brand).toHaveAttribute("href", "/");
  const about = screen.getByRole("link", { name: "About" });
  expect(about).toHaveAttribute("href", "/about");
  expect(pseudoRules(about, ":hover")).not.toEqual([]);
  expect(pseudoRules(about, ":active")).not.toEqual([]);
  await userEvent.click(about);
  expect(await screen.findByRole("heading", { level: 1, name: "snellius" })).toBeInTheDocument();
  await waitFor(() => expect(document.title).toBe("About snellius"));
});

it("shows the Snellius name, descriptor, tagline, UI version, watched installs and links", async () => {
  stubApi({ "/api/v1/installs": [install], "/api/v1/meta": meta });
  mount("/about");
  expect(await screen.findByRole("heading", { level: 1, name: "snellius" })).toBeInTheDocument();
  expect(screen.getByText("Kubernetes Gateway Management & Observability")).toBeInTheDocument();
  expect(screen.getByText("See the whole picture.")).toBeInTheDocument();
  expect(await screen.findByText("v0.6.0")).toBeInTheDocument();
  const row = (await screen.findByText("kgateway 2.5.0")).closest("li") as HTMLElement;
  expect(within(row).getByText("kgateway.dev/kgateway")).toBeInTheDocument();
  expect(within(row).getByText("kgw/")).toBeInTheDocument();
  expect(within(row).getByText("kgateway", { selector: "[data-part=name]" })).toBeInTheDocument();
  expect(screen.getByRole("link", { name: /source code/i })).toHaveAttribute(
    "href",
    "https://github.com/DuncanDoyle/snellius",
  );
  expect(screen.getByRole("link", { name: /documentation/i })).toHaveAttribute(
    "href",
    "https://github.com/DuncanDoyle/snellius#readme",
  );
  expect(screen.getByRole("link", { name: /apache license 2\.0/i })).toHaveAttribute(
    "href",
    "https://github.com/DuncanDoyle/snellius/blob/main/LICENSE",
  );
  expect(screen.queryByRole("link", { name: /built on snellius/i })).toBeNull();
});

it("a downstream brand shows its own name and logo, without the Snellius descriptor and tagline", async () => {
  stubApi({ "/api/v1/installs": [install], "/api/v1/meta": meta });
  mount("/about", { brand: { productName: "acme", logo: <img src="/acme.svg" alt="acme logo" /> } });
  const title = await screen.findByRole("heading", { level: 1, name: /acme/ });
  expect(within(title).getByRole("img", { name: "acme logo" })).toHaveAttribute("src", "/acme.svg");
  expect(screen.queryByText("Kubernetes Gateway Management & Observability")).toBeNull();
  expect(screen.queryByText("See the whole picture.")).toBeNull();
  expect(screen.queryByText(/snellius/)).toBeNull();
  expect(await screen.findByText("kgateway 2.5.0")).toBeInTheDocument();
  // No upstream project links; a small credit links the upstream project instead.
  for (const name of [/source code/i, /documentation/i, /apache license/i]) {
    expect(screen.queryByRole("link", { name })).toBeNull();
  }
  expect(screen.getByRole("link", { name: /built on snellius/i })).toHaveAttribute(
    "href",
    "https://github.com/DuncanDoyle/snellius",
  );
});

it("a downstream brand's own links fill the Project section", async () => {
  stubApi({ "/api/v1/installs": [install], "/api/v1/meta": meta });
  const links = [{ label: "Acme docs", href: "https://acme.example/docs" }];
  mount("/about", { brand: { productName: "acme", logo: null, links } });
  expect(await screen.findByRole("link", { name: /acme docs/i })).toHaveAttribute("href", "https://acme.example/docs");
  expect(screen.queryByRole("link", { name: /source code/i })).toBeNull();
  expect(screen.queryByRole("link", { name: /built on snellius/i })).toBeNull();
});

it("the default brand carries the Snellius descriptor and tagline", () => {
  expect(defaultBrand.descriptor).toBe("Kubernetes Gateway Management & Observability");
  expect(defaultBrand.tagline).toBe("See the whole picture.");
  expect(defaultBrand.links?.map((l) => l.label)).toEqual(["Source code", "Documentation", "Apache License 2.0"]);
});

it("says so when no install is watched", async () => {
  stubApi({ "/api/v1/installs": [], "/api/v1/meta": meta });
  mount("/about");
  expect(await screen.findByText("No installs found.")).toBeInTheDocument();
});
