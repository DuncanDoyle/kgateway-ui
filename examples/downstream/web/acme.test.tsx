import { defaultPages, Shell } from "@snellius/shell";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import gatewayGolden from "../../../pkg/api/testdata/golden/gateway-detail.json";
import installsGolden from "../../../pkg/api/testdata/golden/installs.json";
import metaGolden from "../../../pkg/api/testdata/golden/meta.json";
import delegationGolden from "../../../pkg/api/testdata/golden/route-delegation.json";
import routeGolden from "../../../pkg/api/testdata/golden/route-detail.json";
import { acme } from "./acme";

const GW_PATH = "/i/kgw.kgateway/gateways/infra/edge";

/** Serves the golden API bodies by path; anything else is a 404. */
function stubApi(byPath: Record<string, unknown>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const body = byPath[new URL(url, "http://ui").pathname];
      return body === undefined
        ? new Response(JSON.stringify({ error: "not found" }), { status: 404 })
        : new Response(JSON.stringify(body), { status: 200 });
    }),
  );
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

test("the Widgets tab renders on a Gateway page from the packed shell", async () => {
  // Real API bodies by path; the widgets come from the example's own endpoint.
  const byPath: Record<string, unknown> = {
    "/api/v1/meta": metaGolden.body,
    "/api/v1/installs": installsGolden.body,
    "/api/v1/installs/kgw.kgateway/gateways/infra/edge": {
      ...gatewayGolden.body,
      collectedAt: new Date().toISOString(),
    },
    "/api/ext/acme/widgets": {
      widgets: [
        { namespace: "infra", name: "edge-discount" },
        { namespace: "shop", name: "other-namespace" },
      ],
    },
  };
  stubApi(byPath);
  window.history.pushState({}, "", GW_PATH);
  render(<Shell extension={acme} pages={defaultPages} />);

  await userEvent.setup().click(await screen.findByRole("tab", { name: "Widgets" }));
  const panel = screen.getByRole("tabpanel");
  // The tab lists the widgets in the Gateway's namespace only.
  expect(await within(panel).findByText("edge-discount")).toBeTruthy();
  expect(within(panel).queryByText("other-namespace")).toBeNull();
  expect(window.location.search).toBe("?tab=widgets");
});

test("a kgateway route keeps its Delegation tab and ?tab=delegation", async () => {
  // This build serves kgateway (extension.New), so it registers the kgateway web pack.
  const route = "/api/v1/installs/kgw.kgateway/routes/gateway.networking.k8s.io/HTTPRoute/shop/mid";
  stubApi({
    "/api/v1/meta": metaGolden.body,
    "/api/v1/installs": installsGolden.body,
    [route]: { ...routeGolden.body, collectedAt: new Date().toISOString() },
    [`${route}/delegation`]: { ...delegationGolden.body, collectedAt: new Date().toISOString() },
  });
  window.history.pushState(
    {},
    "",
    "/i/kgw.kgateway/routes/gateway.networking.k8s.io/HTTPRoute/shop/mid?tab=delegation",
  );
  render(<Shell extension={acme} pages={defaultPages} />);

  const tab = await screen.findByRole("tab", { name: "Delegation" });
  expect(tab.getAttribute("aria-selected")).toBe("true");
  expect(window.location.search).toBe("?tab=delegation");
});
