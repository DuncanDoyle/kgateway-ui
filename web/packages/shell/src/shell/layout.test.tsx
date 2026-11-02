import { screen } from "@testing-library/react";
import { ConditionList } from "../explore/ConditionList";
import { renderWithTheme } from "../test/render";
import { DetailColumns, layout } from "./layout";

const css = () => [...document.querySelectorAll("style")].map((s) => s.textContent ?? "").join("");
// Every component source as text, through Vite, so the test needs no Node typings.
const sources = import.meta.glob<string>(["../**/*.tsx", "!../**/*.test.tsx"], {
  query: "?raw",
  import: "default",
  eager: true,
});

it("side column stacks under main below 1600", () => {
  expect(layout).toEqual({ maxWidth: 2400, twoColumnFrom: 1600, proseMax: "80ch" });
  renderWithTheme(<DetailColumns main={<p>main</p>} side={<p>side</p>} />);
  const main = screen.getByText("main");
  const side = screen.getByText("side");
  // Same DOM order as the stacked layout: the side follows the main.
  expect(main.compareDocumentPosition(side) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  const root = screen.getByTestId("detail-columns");
  const cls = [...root.classList].find((c) => c.startsWith("css-")) ?? "";
  const rules = css();
  // One column by default; two only inside the 1600 px query.
  expect(rules).toMatch(new RegExp(`\\.${cls}\\{[^}]*grid-template-columns:minmax\\(0,\\s*1fr\\);`));
  expect(rules).toMatch(
    new RegExp(
      `@media \\(min-width: ${layout.twoColumnFrom}px\\)\\{\\.${cls}\\{[^}]*grid-template-columns:minmax\\(0,\\s*1fr\\) minmax`,
    ),
  );
});

it("prose blocks cap at 80ch", () => {
  renderWithTheme(
    <ConditionList
      conditions={[{ type: "Accepted", status: "True", reason: "ok", message: "A long message." } as never]}
      generation={1}
    />,
  );
  expect(getComputedStyle(screen.getByText("A long message.")).maxWidth).toBe(layout.proseMax);
});

it("no 1400px cap remains", () => {
  const hits = Object.entries(sources).filter(([, text]) => /1400\s*px/.test(text));
  expect(Object.keys(sources).length).toBeGreaterThan(50);
  expect(hits.map(([f]) => f)).toEqual([]);
});

it("one column at every width when there is no side", () => {
  renderWithTheme(<DetailColumns main={<p>main</p>} side={null} />);
  const cls = [...screen.getByTestId("detail-columns").classList].find((c) => c.startsWith("css-")) ?? "";
  expect(css()).not.toMatch(new RegExp(`min-width: ${layout.twoColumnFrom}px\\)\\{\\.${cls}[{ ]`));
  expect(css()).not.toContain(`.${cls}{display:grid;grid-template-columns:minmax(0, 1fr) `);
});

it("renders without a side column when there is no side", () => {
  renderWithTheme(<DetailColumns main={<p>main</p>} side={null} />);
  expect(screen.queryByTestId("detail-side")).not.toBeInTheDocument();
});
