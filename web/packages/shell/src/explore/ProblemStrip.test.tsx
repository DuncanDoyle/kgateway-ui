import { screen, within } from "@testing-library/react";
import { hrefPath, renderWithTheme } from "../test/render";
import { ProblemStrip } from "./ProblemStrip";

const ID = "kgw.kgateway";
const reviews = { group: "", kind: "Service", namespace: "demo", name: "reviews", link: "backend" as const };

it("a backend problem links the backend and quotes its finding's message", () => {
  renderWithTheme(
    <ProblemStrip
      installId={ID}
      state="warning"
      reason={{
        kind: "backend-problem",
        code: "KGWUI-023",
        message: "This backend has no ready endpoints",
        backend: reviews,
      }}
    />,
  );
  const line = screen.getByTestId("problem-reason");
  expect(line).toHaveTextContent("Backenddemo/reviewsThis backend has no ready endpoints");
  expect(hrefPath(within(line).getByRole("link", { name: "demo/reviews" }))).toBe(
    `/i/${ID}/backends/-/Service/demo/reviews`,
  );
  expect(within(line).queryByRole("link", { name: "Open finding" })).toBeNull();
});

it("a backend the caller may not view is neither named nor linked", () => {
  renderWithTheme(
    <ProblemStrip installId={ID} state="warning" reason={{ kind: "backend-problem", code: "KGWUI-023" }} />,
  );
  const line = screen.getByTestId("problem-reason");
  expect(line).toHaveTextContent(/^KGWUI-023: A backend you cannot view has a problem\.$/);
  expect(within(line).queryByRole("link")).toBeNull();
});
