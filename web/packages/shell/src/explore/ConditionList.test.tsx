import { screen } from "@testing-library/react";
import type { Condition } from "../api/types";
import { renderWithTheme } from "../test/render";
import { ConditionList, conditionState } from "./ConditionList";

const accepted = (over: Partial<Condition> = {}): Condition => ({
  type: "Accepted",
  status: "True",
  reason: "Accepted",
  message: "",
  lastTransitionTime: "2026-10-17T11:00:00Z",
  observedGeneration: 4,
  stale: false,
  ...over,
});

it("a stale condition shows the unknown icon and keeps its written status", () => {
  renderWithTheme(<ConditionList conditions={[accepted({ observedGeneration: 3, stale: true })]} generation={4} />);
  const c = screen.getByTestId("condition-Accepted");
  expect(c).toHaveTextContent("Accepted: True");
  expect(c).toHaveTextContent("Describes generation 3; the object is at 4.");
  expect(screen.queryByTestId("health-icon-healthy")).toBeNull();
  expect(screen.getAllByTestId("health-icon-unknown").length).toBeGreaterThan(0);
});

it("a current condition keeps its True/False icon", () => {
  renderWithTheme(<ConditionList conditions={[accepted()]} generation={4} />);
  expect(screen.getByTestId("health-icon-healthy")).toBeInTheDocument();
});

it("conditionState reads stale as unknown, whatever the status", () => {
  expect(conditionState({ type: "Accepted", status: "True", stale: true })).toBe("unknown");
  expect(conditionState({ type: "Accepted", status: "False", stale: true })).toBe("unknown");
  expect(conditionState({ type: "Accepted", status: "False" })).toBe("error");
});
