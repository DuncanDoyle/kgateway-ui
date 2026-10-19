import { css } from "@emotion/react";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SWRConfig } from "swr";
import { stubApi } from "../test/render";
import { useThemeSwitch } from "../theme/ThemeProvider";
import { THEME_PAINT_KEY } from "../theme/themeStorage";
import { controlRoomDark, controlRoomLight } from "../theme/themes";
import { type CorePages, useInstallId, useShellExtension } from "./extension";
import { Shell } from "./Shell";

const pages: CorePages = {
  ClusterLanding: () => <p>cluster landing</p>,
  InstallPage: () => <p>install page</p>,
  FindingsPage: () => <p>findings page</p>,
  ResourcePage: () => <p>resource page</p>,
};
const meta = { version: "dev", synced: true, kinds: [] };
const inst = (id: string, controller = { ready: 2, desired: 2 }, computedAt = new Date().toISOString()) => ({
  id,
  namespace: id.split("/")[0],
  name: id.split("/")[1],
  version: "2.4.7",
  edition: "kgateway",
  controllerNames: [],
  controller,
  scope: { mode: "all", watched: 3, total: 3 },
  health: { state: "healthy", counts: {} },
  computedAt,
});

function mount(path: string, ext = {}) {
  window.history.pushState({}, "", path);
  return render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <Shell extension={ext} pages={pages} />
    </SWRConfig>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

it("redirects / to the only install", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": meta });
  mount("/");
  await waitFor(() => expect(screen.getByText("install page")).toBeInTheDocument());
  expect(window.location.pathname).toBe("/i/kgw/kgateway");
});

it("shows the cluster landing and an install switcher when there are several installs", async () => {
  stubApi({ "/api/v1/installs": [inst("a/one"), inst("b/two")], "/api/v1/meta": meta });
  mount("/");
  await waitFor(() => expect(screen.getByText("cluster landing")).toBeInTheDocument());
  mount("/i/a/one");
  await userEvent.click(await screen.findByRole("button", { name: /switch install/i }));
  expect(screen.getByRole("menuitem", { name: /b\/two/ })).toBeInTheDocument();
});

it("navigates to the chosen install and closes the menu", async () => {
  stubApi({ "/api/v1/installs": [inst("a/one"), inst("b/two")], "/api/v1/meta": meta });
  mount("/i/a/one");
  await userEvent.click(await screen.findByRole("button", { name: /switch install/i }));
  await userEvent.click(screen.getByRole("menuitem", { name: /b\/two/ }));
  expect(window.location.pathname).toBe("/i/b/two");
  expect(screen.queryByRole("menu")).toBeNull();
});

it("closes a menu on Escape and returns focus to its button", async () => {
  stubApi({ "/api/v1/installs": [inst("a/one"), inst("b/two")], "/api/v1/meta": meta });
  mount("/i/a/one");
  const button = await screen.findByRole("button", { name: /switch install/i });
  await userEvent.click(button);
  expect(screen.getByRole("menu")).toBeInTheDocument();
  await userEvent.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).toBeNull();
  expect(button).toHaveFocus();
});

it("closes a menu on a click outside it", async () => {
  stubApi({ "/api/v1/installs": [inst("a/one"), inst("b/two")], "/api/v1/meta": meta });
  mount("/i/a/one");
  await userEvent.click(await screen.findByRole("button", { name: /switch install/i }));
  await userEvent.click(screen.getByText("install page"));
  expect(screen.queryByRole("menu")).toBeNull();
});

it("shows a designed empty state when no install is found", async () => {
  stubApi({ "/api/v1/installs": [], "/api/v1/meta": meta });
  mount("/");
  await waitFor(() => expect(screen.getByText(/No kgateway installation found/)).toBeInTheDocument());
  expect(screen.getByText(/--install/)).toBeInTheDocument();
});

it("says it is waiting for the cluster while the backend cache is not synced", async () => {
  stubApi({
    "/api/v1/installs": [503, { error: "connecting to cluster: cache not synced" }],
    "/api/v1/meta": { ...meta, synced: false },
  });
  mount("/");
  await waitFor(() => expect(screen.getByText("Waiting for the cluster")).toBeInTheDocument());
});

it("renders extension brand and routes", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": meta });
  mount("/i/kgw/kgateway/waf", {
    brand: { productName: "Acme Edge", logo: <span>A</span> },
    routes: [{ path: "waf", element: <p>waf page</p>, scope: "install" }],
    navEntries: [{ label: "WAF", to: "waf", scope: "install" }],
  });
  await waitFor(() => expect(screen.getByText("waf page")).toBeInTheDocument());
  expect(screen.getByText("Acme Edge")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "WAF" })).toHaveAttribute("href", "/i/kgw/kgateway/waf");
});

it("shows the install's version and controller readiness in the header", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway", { ready: 1, desired: 2 })], "/api/v1/meta": meta });
  mount("/i/kgw/kgateway");
  expect(await screen.findByTestId("version")).toHaveTextContent(/^v2\.4\.7$/);
  const ready = screen.getByText("Controller 1/2 ready");
  expect(within(ready).getByTestId("health-icon-error")).toBeInTheDocument();
});

it("shows no edition tag when the edition repeats the product name", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": meta });
  mount("/i/kgw/kgateway");
  await screen.findByTestId("version");
  expect(screen.queryByTestId("edition")).toBeNull();
});

it("shows the edition tag when it differs from the product name", async () => {
  stubApi({ "/api/v1/installs": [{ ...inst("kgw/kgateway"), edition: "acme" }], "/api/v1/meta": meta });
  mount("/i/kgw/kgateway", { brand: { productName: "Acme Edge", logo: null } });
  expect(await screen.findByTestId("edition")).toHaveTextContent(/^acme$/);
});

it("hides the identity shimmer when installs cannot load", async () => {
  stubApi({ "/api/v1/installs": [500, { error: "boom" }], "/api/v1/meta": meta });
  mount("/i/kgw/kgateway");
  await waitFor(() => expect(fetch).toHaveBeenCalledWith("/api/v1/installs", expect.anything()));
  await waitFor(() => expect(screen.queryByTestId("shimmer")).toBeNull());
});

it("resolves the default theme once: the context and the provider agree", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": meta });
  function Probe() {
    const { defaultThemeId } = useShellExtension();
    const { current } = useThemeSwitch();
    return <output data-testid="probe">{`${defaultThemeId}|${current.id}`}</output>;
  }
  // The light theme is listed first; with no explicit default, both pick the dark one (D-011).
  mount("/probe", {
    themes: [controlRoomLight, controlRoomDark],
    routes: [{ path: "probe", element: <Probe />, scope: "top" }],
  });
  expect(await screen.findByTestId("probe")).toHaveTextContent(`${controlRoomDark.id}|${controlRoomDark.id}`);
});

it("useInstallId is empty outside install routes", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": meta });
  function Probe() {
    return <output data-testid="probe">{`[${useInstallId()}]`}</output>;
  }
  mount("/probe", { routes: [{ path: "probe", element: <Probe />, scope: "top" }] });
  expect(await screen.findByTestId("probe")).toHaveTextContent("[]");
});

it("does not double the v of a version that already has one", async () => {
  stubApi({ "/api/v1/installs": [{ ...inst("kgw/kgateway"), version: "v2.4.7" }], "/api/v1/meta": meta });
  mount("/i/kgw/kgateway");
  expect(await screen.findByTestId("version")).toHaveTextContent(/^v2\.4\.7$/);
});

it("renders a stale install summary as unknown in the switcher and the header", async () => {
  const old = new Date(Date.now() - 60_000).toISOString();
  stubApi({
    "/api/v1/installs": [inst("a/one", { ready: 2, desired: 2 }, old), inst("b/two", { ready: 2, desired: 2 }, old)],
    "/api/v1/meta": meta,
  });
  mount("/i/a/one");
  const ready = await screen.findByText("Controller 2/2 ready");
  expect(within(ready).getByTestId("health-icon-unknown")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: /switch install/i }));
  const item = screen.getByRole("menuitem", { name: /b\/two/ });
  expect(within(item).getByTestId("health-icon-unknown")).toBeInTheDocument();
  expect(within(item).queryByTestId("health-icon-healthy")).toBeNull();
});

it("applies stylesOverride to the shell root", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": meta });
  window.history.pushState({}, "", "/i/kgw/kgateway");
  render(
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>
      <Shell
        pages={pages}
        stylesOverride={css`
          outline: 3px solid rgb(1, 2, 3);
        `}
      />
    </SWRConfig>,
  );
  expect(await screen.findByTestId("shell-root")).toHaveStyle({ outline: "3px solid rgb(1, 2, 3)" });
});

it("switches theme from the theme menu and applies the new tokens", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": meta });
  mount("/i/kgw/kgateway");
  expect(getComputedStyle(document.body).backgroundColor).toBe(rgb(controlRoomDark.color.bg));
  await userEvent.click(await screen.findByRole("button", { name: /theme/i }));
  expect(screen.getByRole("menuitemradio", { name: /^Control room$/ })).toHaveAttribute("aria-checked", "true");
  await userEvent.click(screen.getByRole("menuitemradio", { name: /Control room light/ }));
  // The light tokens reach the page and the pre-React paint record.
  expect(getComputedStyle(document.body).backgroundColor).toBe(rgb(controlRoomLight.color.bg));
  expect(getComputedStyle(document.body).color).toBe(rgb(controlRoomLight.color.text));
  expect(JSON.parse(localStorage.getItem(THEME_PAINT_KEY) ?? "{}")).toEqual({ bg: "#FFFFFF", scheme: "light" });
  await userEvent.click(screen.getByRole("button", { name: /theme/i }));
  expect(screen.getByRole("menuitemradio", { name: /Control room light/ })).toHaveAttribute("aria-checked", "true");
});

it("shows the app tooltip on hover and links it to the button", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": meta });
  mount("/i/kgw/kgateway");
  const button = await screen.findByRole("button", { name: /theme/i });
  await userEvent.hover(button);
  const tip = screen.getByRole("tooltip");
  expect(tip).toHaveTextContent("Theme: Control room");
  expect(button).toHaveAttribute("aria-describedby", tip.id);
  expect(button).not.toHaveAttribute("title");
});

it("a theme picked with the mouse leaves no tooltip behind", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": meta });
  mount("/i/kgw/kgateway");
  const button = await screen.findByRole("button", { name: /theme/i });
  await userEvent.click(button);
  // The pointer leaves the button for the menu below it, as it does in a browser.
  await userEvent.unhover(button);
  await userEvent.click(screen.getByRole("menuitemradio", { name: /Control room light/ }));
  expect(screen.queryByRole("menu")).toBeNull();
  expect(button).toHaveFocus();
  expect(screen.queryByRole("tooltip")).toBeNull();
});

it("keyboard focus shows a button's tooltip", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": meta });
  mount("/i/kgw/kgateway");
  const button = await screen.findByRole("button", { name: /theme/i });
  await userEvent.click(button);
  await userEvent.keyboard("{Escape}");
  expect(button).toHaveFocus();
  expect(screen.getByRole("tooltip")).toHaveTextContent("Theme: Control room");
});

it("switcher items name each install's state", async () => {
  stubApi({
    "/api/v1/installs": [inst("a/one"), { ...inst("b/two"), health: { state: "error", counts: { error: 1 } } }],
    "/api/v1/meta": meta,
  });
  mount("/i/a/one");
  await userEvent.click(await screen.findByRole("button", { name: /switch install/i }));
  expect(screen.getByRole("menuitem", { name: /^Error: b\/two/ })).toBeInTheDocument();
  expect(screen.getByRole("menuitem", { name: /^Healthy: a\/one/ })).toBeInTheDocument();
});

it.each([
  ["/i/kgw/kgateway", "kgw/kgateway | kgateway"],
  ["/i/kgw/kgateway/findings", "Findings: kgw/kgateway | kgateway"],
  ["/i/kgw/kgateway/r/gateway.networking.k8s.io/Gateway/prod/edge", "Gateway prod/edge: kgw/kgateway | kgateway"],
  [
    "/i/kgw/kgateway/r/gateway.networking.k8s.io/GatewayClass/-/kgateway",
    "GatewayClass kgateway: kgw/kgateway | kgateway",
  ],
  ["/nope", "Page not found | kgateway"],
])("sets the document title on %s", async (path, title) => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway"), inst("b/two")], "/api/v1/meta": meta });
  mount(path);
  await waitFor(() => expect(document.title).toBe(title));
});

it("titles an extension page with its nav label", async () => {
  stubApi({ "/api/v1/installs": [inst("kgw/kgateway")], "/api/v1/meta": meta });
  mount("/i/kgw/kgateway/waf", {
    brand: { productName: "Acme Edge", logo: null },
    routes: [{ path: "waf", element: <p>waf page</p>, scope: "install" }],
    navEntries: [{ label: "WAF", to: "waf", scope: "install" }],
  });
  await waitFor(() => expect(document.title).toBe("WAF: kgw/kgateway | Acme Edge"));
});

// region Helpers
function rgb(hex: string): string {
  const n = Number.parseInt(hex.slice(1), 16);
  return `rgb(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255})`;
}
