import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useNow } from "../api/clock";
import type { Computed } from "../api/exploreTypes";
import { isStale } from "../api/hooks";
import { computedCopy } from "../copy/explorer";
import { StaleView } from "../health/staleView";
import { pseudoRules, renderWithTheme } from "../test/render";
import { controlRoomDark, controlRoomLight } from "../theme/themes";
import { ComputedBadge } from "./ComputedBadge";

const base: Computed = { by: "ui", rules: "kgateway-2.4", rule: "hostname-intersection", state: "checked" };
const tip = "Computed by the UI from kgateway 2.4's attachment rules: hostname intersection";
const badge = () => screen.getByTestId("computed-badge");
const icons = () =>
  Array.from(badge().querySelectorAll('[data-testid^="health-icon-"], [data-testid^="computed-icon-"]')).map((e) =>
    e.getAttribute("data-testid"),
  );

afterEach(() => vi.useRealTimers());

it("renders each coverage with its own text", () => {
  const { unmount } = renderWithTheme(<ComputedBadge computed={{ ...base, coverage: ["count-agrees"] }} />);
  expect(within(badge()).getByText("Count agrees with the controller")).toBeInTheDocument();
  expect(icons()).toEqual(["health-icon-healthy"]);
  unmount();

  const r2 = renderWithTheme(<ComputedBadge computed={{ ...base, coverage: ["status-compatible"] }} />);
  expect(within(badge()).getByText("Status agrees")).toBeInTheDocument();
  expect(icons()).toEqual(["health-icon-healthy"]);
  r2.unmount();

  renderWithTheme(<ComputedBadge computed={{ ...base, coverage: ["not-verified"] }} />);
  expect(within(badge()).getByText("Not checked by the controller")).toBeInTheDocument();
  // not-verified uses a neutral icon, never the healthy one.
  expect(icons()).toEqual(["computed-icon-neutral"]);
});

it("renders several coverage labels side by side, each with its own icon", () => {
  renderWithTheme(<ComputedBadge computed={{ ...base, coverage: ["count-agrees", "not-verified"] }} />);
  expect(within(badge()).getByText("Count agrees with the controller")).toBeInTheDocument();
  expect(within(badge()).getByText("Not checked by the controller")).toBeInTheDocument();
  expect(icons()).toEqual(["health-icon-healthy", "computed-icon-neutral"]);
});

it("a checked fact without coverage reads as not checked, never healthy", () => {
  renderWithTheme(<ComputedBadge computed={base} />);
  expect(within(badge()).getByText("Not checked by the controller")).toBeInTheDocument();
  expect(icons()).not.toContain("health-icon-healthy");
});

it("renders pending, unknown and disagrees with icon and text", () => {
  const p = renderWithTheme(<ComputedBadge computed={{ ...base, state: "pending" }} />);
  expect(within(badge()).getByText("Waiting for the controller")).toBeInTheDocument();
  expect(icons()).toEqual(["computed-icon-pending"]);
  p.unmount();

  const u = renderWithTheme(
    <ComputedBadge computed={{ ...base, state: "unknown", reason: "listenerset-no-status" }} />,
  );
  expect(within(badge()).getByText("Unknown: the ListenerSet has no status yet")).toBeInTheDocument();
  expect(icons()).toEqual(["health-icon-unknown"]);
  u.unmount();

  // A reason this build does not know still names itself.
  const o = renderWithTheme(<ComputedBadge computed={{ ...base, state: "unknown", reason: "brand-new" }} />);
  expect(within(badge()).getByText(computedCopy.unknownOther("brand-new"))).toBeInTheDocument();
  o.unmount();

  renderWithTheme(<ComputedBadge computed={{ ...base, state: "disagrees", coverage: ["count-agrees"] }} />);
  expect(
    within(badge()).getByText(
      "The UI's computation disagrees with the controller. The controller's status is what counts.",
    ),
  ).toBeInTheDocument();
  expect(icons()).toEqual(["health-icon-warning"]);
});

it("restricted renders the placeholder and no value", () => {
  // The wire never sends reason or note with restricted; even if it did, neither shows.
  renderWithTheme(
    <ComputedBadge computed={{ ...base, state: "restricted", reason: "setting-unknown", note: "secret note" }} />,
  );
  expect(within(badge()).getByText("Hidden: depends on objects you cannot view")).toBeInTheDocument();
  expect(badge()).not.toHaveTextContent("secret note");
  expect(badge()).not.toHaveTextContent("Unknown: a controller setting cannot be read");
  expect(icons()).toEqual(["computed-icon-restricted"]);
});

it("tooltip is reachable by keyboard and part of the accessible name", async () => {
  const user = userEvent.setup();
  renderWithTheme(<ComputedBadge computed={{ ...base, coverage: ["not-verified"] }} />);
  const chip = screen.getByRole("button", { name: `Computed. ${tip}` });
  expect(screen.queryByRole("tooltip")).toBeNull();
  await user.tab();
  expect(chip).toHaveFocus();
  expect(screen.getByRole("tooltip")).toHaveTextContent(tip);
});

it("names the listener-count rule and falls back to the raw ids for unknown rules", () => {
  const a = renderWithTheme(<ComputedBadge computed={{ ...base, rule: "listener-count" }} />);
  expect(
    screen.getByRole("button", {
      name: "Computed. Computed by the UI from kgateway 2.4's attachment rules: listener route count",
    }),
  ).toBeInTheDocument();
  a.unmount();
  renderWithTheme(<ComputedBadge computed={{ ...base, rules: "other", rule: "x-rule" }} />);
  expect(
    screen.getByRole("button", { name: "Computed. Computed by the UI from other's rules: x-rule" }),
  ).toBeInTheDocument();
});

it("shows the selection note", () => {
  const note = "Controller version unknown; using the 2.4 rules";
  renderWithTheme(<ComputedBadge computed={{ ...base, coverage: ["count-agrees"], note }} />);
  expect(within(badge()).getByText(note)).toBeInTheDocument();
});

// The detail page's own staleness rule: data older than 2 × POLL_MS is stale (design §6.3).
function StalePage({ collectedAt, computed }: { collectedAt: string; computed: Computed }) {
  const now = useNow();
  return (
    <StaleView.Provider value={isStale(collectedAt, now)}>
      <ComputedBadge computed={computed} />
    </StaleView.Provider>
  );
}

it("stale data turns the badge unknown", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  const note = "Controller version unknown; using the 2.4 rules";
  renderWithTheme(
    <StalePage collectedAt={new Date().toISOString()} computed={{ ...base, coverage: ["count-agrees"], note }} />,
  );
  expect(within(badge()).getByText("Count agrees with the controller")).toBeInTheDocument();
  expect(icons()).toEqual(["health-icon-healthy"]);

  await act(async () => {
    await vi.advanceTimersByTimeAsync(2 * 5000 + 1500);
  });
  expect(within(badge()).queryByText("Count agrees with the controller")).toBeNull();
  expect(within(badge()).getByText(computedCopy.stale)).toBeInTheDocument();
  expect(icons()).toEqual(["health-icon-unknown"]);
  // The note is a notice, not a state: it stays.
  expect(within(badge()).getByText(note)).toBeInTheDocument();
});

it("stale restricted and disagreeing facts turn unknown too", () => {
  for (const state of ["restricted", "disagrees", "pending"] as const) {
    const r = renderWithTheme(
      <StaleView.Provider value>
        <ComputedBadge computed={{ ...base, state }} />
      </StaleView.Provider>,
    );
    expect(icons()).toEqual(["health-icon-unknown"]);
    expect(within(badge()).getByText(computedCopy.stale)).toBeInTheDocument();
    r.unmount();
  }
});

describe.each([controlRoomDark, controlRoomLight])("in the $name theme", (t) => {
  it("the chip has hover, active and focus-visible styles", () => {
    renderWithTheme(<ComputedBadge computed={base} />, { themeId: t.id });
    const chip = screen.getByRole("button");
    expect(pseudoRules(chip, ":hover").join()).toContain(`border-color:${t.color.text}`);
    expect(pseudoRules(chip, ":active").join()).toContain(t.color.accentText);
    expect(pseudoRules(chip, ":focus-visible").join()).toContain(t.color.focus);
  });
});
