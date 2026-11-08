import { screen, within } from "@testing-library/react";
import { Route, Routes } from "react-router-dom";
import { diagnosticsCopy } from "../../copy/explorer";
import { diagnostics, healthy, installSummary, meta, overview } from "../../test/fixtures";
import { renderWithTheme, stubApi } from "../../test/render";
import { InstallPage } from "./InstallPage";

const ALL = ["delegation", "computed", "dataplane", "processes"];

/** The install page for an install with features (undefined: an older server, which sends none). */
function mount(features: string[] | undefined) {
  stubApi({
    "/api/v1/installs": [{ ...installSummary(), features }],
    "/api/v1/installs/kgw.kgateway/overview": overview(),
    "/api/v1/installs/kgw.kgateway/diagnostics": diagnostics(healthy()),
    "/api/v1/meta": meta,
  });
  return renderWithTheme(
    <Routes>
      <Route path="/i/:id" element={<InstallPage />} />
    </Routes>,
    { path: "/i/kgw.kgateway" },
  );
}
const gateways = () => screen.findByRole("table", { name: "Gateways" });

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it.each([
  ["every feature", ALL],
  ["an older server that sends none", undefined],
])("with %s: the Processes block and the Data plane column", async (_, f) => {
  mount(f);
  const table = await gateways();
  expect(screen.getByRole("heading", { name: diagnosticsCopy.processesTitle })).toBeInTheDocument();
  expect(within(table).getByRole("columnheader", { name: new RegExp(diagnosticsCopy.dataPlane) })).toBeInTheDocument();
});

it("without dataplane and processes: neither, not an Unknown placeholder", async () => {
  mount(["computed"]);
  const table = await gateways();
  expect(screen.queryByRole("heading", { name: diagnosticsCopy.processesTitle })).toBeNull();
  expect(within(table).queryByRole("columnheader", { name: new RegExp(diagnosticsCopy.dataPlane) })).toBeNull();
});
