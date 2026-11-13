import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { pseudoRules, renderWithTheme } from "../test/render";
import { controlRoomDark, controlRoomLight } from "../theme/themes";
import { HealthBadge } from "./HealthBadge";
import { StaleView } from "./staleView";

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

it("does not forward the size prop to the DOM", () => {
  renderWithTheme(<HealthBadge state="healthy" />);
  expect(screen.getByTestId("health-icon-healthy")).not.toHaveAttribute("size");
});

describe("row reason tooltip (D-063)", () => {
  const trigger = () => screen.getByTestId("health-reason");

  it("names the deciding finding's code and message and how many more, on hover", async () => {
    renderWithTheme(
      <HealthBadge
        state="error"
        reason={{ kind: "finding", code: "KGWUI-002", message: "Unsupported value in the route", more: 2 }}
      />,
    );
    await userEvent.hover(trigger());
    expect(screen.getByRole("tooltip")).toHaveTextContent("KGWUI-002: Unsupported value in the route (+2 more)");
    expect(trigger()).toHaveAttribute("aria-describedby", screen.getByRole("tooltip").id);
  });

  it("opens on keyboard focus too", async () => {
    renderWithTheme(
      <HealthBadge state="warning" reason={{ kind: "finding", code: "KGWUI-004", message: "Namespace not watched" }} />,
    );
    await userEvent.tab();
    expect(trigger()).toHaveFocus();
    expect(screen.getByRole("tooltip")).toHaveTextContent(/^KGWUI-004: Namespace not watched$/);
  });

  it.each([
    ["unknown", { kind: "unknown" }, "A required check is not OK, so the state is unknown."],
    ["warning", { kind: "gateway-rejecting" }, "The parent Gateway rejects config."],
    [
      "warning",
      {
        kind: "backend-problem",
        code: "KGWUI-023",
        message: "This backend has no ready endpoints",
        backend: { group: "", kind: "Service", namespace: "demo", name: "reviews", link: "backend" },
      },
      "Backend demo/reviews: This backend has no ready endpoints",
    ],
    // A backend the caller may not view: no name, no message.
    ["warning", { kind: "backend-problem", code: "KGWUI-023" }, "KGWUI-023: A backend you can't view has a problem."],
  ] as const)("a %s row without a finding of its own names the cause", async (state, reason, text) => {
    renderWithTheme(<HealthBadge state={state} reason={reason} />);
    await userEvent.hover(trigger());
    expect(screen.getByRole("tooltip")).toHaveTextContent(text);
  });

  it("a healthy row has no tooltip and no tab stop", async () => {
    renderWithTheme(<HealthBadge state="healthy" />);
    expect(screen.queryByTestId("health-reason")).toBeNull();
    await userEvent.tab();
    expect(document.body).toHaveFocus();
  });

  it("a stale view drops the reason with the state it explains", () => {
    renderWithTheme(
      <StaleView.Provider value={true}>
        <HealthBadge state="error" reason={{ kind: "finding", code: "KGWUI-002", message: "m" }} />
      </StaleView.Provider>,
    );
    expect(screen.getByText("Unknown")).toBeInTheDocument();
    expect(screen.queryByTestId("health-reason")).toBeNull();
  });

  it.each([controlRoomLight, controlRoomDark])("the trigger has hover, active and focus-visible styles ($id)", (t) => {
    renderWithTheme(<HealthBadge state="unknown" reason={{ kind: "unknown" }} />, { themeId: t.id });
    expect(pseudoRules(trigger(), ":hover").join()).not.toBe("");
    expect(pseudoRules(trigger(), ":active").join()).not.toBe("");
    expect(pseudoRules(trigger(), ":focus-visible").join()).toContain(t.color.focus);
  });
});
