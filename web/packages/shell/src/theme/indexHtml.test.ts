/// <reference types="vite/client" />
import html from "../../../../app/index.html?raw";
import { THEME_PAINT_KEY } from "./themeStorage";

const script = /<script>([\s\S]*?)<\/script>/.exec(html)?.[1] ?? "";

// Runs the inline pre-React script against a stubbed localStorage and root element.
function paint(stored: string | null | "throw") {
  const root = { style: {} as Record<string, string> };
  const storage = {
    getItem: (k: string) => {
      if (stored === "throw") throw new Error("blocked");
      return k === THEME_PAINT_KEY ? stored : null;
    },
  };
  new Function("localStorage", "document", script)(storage, { documentElement: root });
  return { bg: root.style.background, scheme: root.style.colorScheme };
}

const dark = { bg: "#0F1220", scheme: "dark" };

it("has an inline script", () => {
  expect(script).not.toBe("");
});

it("paints a valid paint record", () => {
  expect(paint(JSON.stringify({ bg: "#FFFFFF", scheme: "light" }))).toEqual({ bg: "#FFFFFF", scheme: "light" });
  expect(paint(JSON.stringify({ bg: "#1a2b3c", scheme: "dark" }))).toEqual({ bg: "#1a2b3c", scheme: "dark" });
});

it.each([
  ["missing", null],
  ["corrupt JSON", "{not json"],
  ["invalid hex", JSON.stringify({ bg: "red", scheme: "light" })],
  ["short hex", JSON.stringify({ bg: "#FFF", scheme: "light" })],
  ["invalid scheme", JSON.stringify({ bg: "#FFFFFF", scheme: "sepia" })],
  ["null record", "null"],
  ["blocked storage", "throw" as const],
])("paints dark when the record is %s", (_, stored) => {
  expect(paint(stored)).toEqual(dark);
});

it("names no theme ids", () => {
  expect(script).not.toContain("control-room");
});
