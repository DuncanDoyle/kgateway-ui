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
