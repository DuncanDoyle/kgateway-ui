import { screen } from "@testing-library/react";
import { renderWithTheme } from "../test/render";
import { HealthBadge } from "./HealthBadge";

it.each([
  ["healthy", "Healthy"],
  ["warning", "Warning"],
  ["error", "Error"],
  ["unknown", "Unknown"],
] as const)("%s renders an icon and text, never colour alone", (state, text) => {
  renderWithTheme(<HealthBadge state={state} />);
  expect(screen.getByText(text)).toBeInTheDocument();
  expect(screen.getByTestId(`health-icon-${state}`)).toBeInTheDocument();
});

it("uses a custom label in place of the state text", () => {
  renderWithTheme(<HealthBadge state="warning" label="2 warnings" />);
  expect(screen.getByText("2 warnings")).toBeInTheDocument();
  expect(screen.queryByText("Warning")).toBeNull();
});
