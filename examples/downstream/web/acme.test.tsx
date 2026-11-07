import { defaultPages, Shell } from "@snellius/shell";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import gatewayGolden from "../../../pkg/api/testdata/golden/gateway-detail.json";
import installsGolden from "../../../pkg/api/testdata/golden/installs.json";
import metaGolden from "../../../pkg/api/testdata/golden/meta.json";
import { acme } from "./acme";

const GW_PATH = "/i/kgw.kgateway/gateways/infra/edge";

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
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const body = byPath[new URL(url, "http://ui").pathname];
      return body === undefined
        ? new Response(JSON.stringify({ error: "not found" }), { status: 404 })
        : new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  window.history.pushState({}, "", GW_PATH);
  render(<Shell extension={acme} pages={defaultPages} />);

  await userEvent.setup().click(await screen.findByRole("tab", { name: "Widgets" }));
  const panel = screen.getByRole("tabpanel");
  // The tab lists the widgets in the Gateway's namespace only.
  expect(await within(panel).findByText("edge-discount")).toBeTruthy();
  expect(within(panel).queryByText("other-namespace")).toBeNull();
  expect(window.location.search).toBe("?tab=widgets");
});
