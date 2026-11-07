import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithTheme } from "../test/render";
import { Tooltip } from "./Tooltip";

// The tip's position rule: the declarations of the injected rule for the tip's own class.
const tipStyle = () => {
  const tip = screen.getByRole("tooltip");
  const css = [...document.querySelectorAll("style")].map((s) => s.textContent ?? "").join("");
  const cls = [...tip.classList].map((c) => `.${c}{`);
  return [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
    .filter(([, sel = ""]) => cls.some((c) => `${sel.trim()}{`.endsWith(c)))
    .map(([, , body = ""]) => body)
    .join();
};

async function open(placement?: "start" | "center" | "end") {
  renderWithTheme(
    <Tooltip label="tip" placement={placement}>
      <button type="button">trigger</button>
    </Tooltip>,
  );
  await userEvent.setup().tab();
}

it("start aligns the tip's left edge with the trigger", async () => {
  await open("start");
  expect(tipStyle()).toContain("left:0");
  expect(tipStyle()).not.toContain("transform");
});

it("centres the tip by default", async () => {
  await open();
  expect(tipStyle()).toContain("left:50%");
  expect(tipStyle()).toContain("transform:translateX(-50%)");
});

describe("inside an overflow container", () => {
  const rect = (top: number, left: number) =>
    ({ top, left, bottom: top + 20, right: left + 80, width: 80, height: 20, x: left, y: top }) as DOMRect;

  async function openIn(top: number) {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(rect(top, 40));
    vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(40);
    renderWithTheme(
      <div data-testid="scroll" style={{ overflow: "auto" }}>
        <Tooltip label="tip">
          <button type="button">trigger</button>
        </Tooltip>
      </div>,
    );
    await userEvent.setup().tab();
    return screen.getByRole("tooltip");
  }
  afterEach(() => vi.restoreAllMocks());

  it("renders the tip outside the container, positioned from the trigger", async () => {
    const tip = await openIn(100);
    expect(screen.getByTestId("scroll")).not.toContainElement(tip);
    expect(document.body).toContainElement(tip);
    // The fixed anchor covers the trigger's rect; the tip hangs below it.
    const anchor = tip.parentElement as HTMLElement;
    const { top, left, width } = anchor.style;
    expect({ top, left, width }).toEqual({ top: "100px", left: "40px", width: "80px" });
    expect(tip).toHaveAttribute("data-side", "below");
    expect(screen.getByRole("button")).toHaveAttribute("aria-describedby", tip.id);
  });

  it("flips above the trigger when there is no room below", async () => {
    const tip = await openIn(window.innerHeight - 30);
    expect(tip).toHaveAttribute("data-side", "above");
  });
});
