/// <reference types="vite/client" />
import html from "../../../../app/index.html?raw";
import { LEGACY_THEME_PAINT_KEY, THEME_PAINT_KEY } from "./themeStorage";

const script = /<script>([\s\S]*?)<\/script>/.exec(html)?.[1] ?? "";

type Store = { entries?: Record<string, string>; readThrows?: boolean; writeThrows?: boolean };

// Runs the inline pre-React script against a stubbed localStorage and root element.
function run({ entries = {}, readThrows, writeThrows }: Store) {
  const data = { ...entries };
  const root = { style: {} as Record<string, string> };
  const storage = {
    getItem: (k: string) => {
      if (readThrows) throw new Error("blocked");
      return data[k] ?? null;
    },
    setItem: (k: string, v: string) => {
      if (writeThrows) throw new Error("quota");
      data[k] = v;
    },
    removeItem: (k: string) => {
      if (writeThrows) throw new Error("blocked");
      delete data[k];
    },
  };
  new Function("localStorage", "document", script)(storage, { documentElement: root });
  return { painted: { bg: root.style.background, scheme: root.style.colorScheme }, data };
}

const paint = (stored: string | null | "throw") =>
  run(stored === "throw" ? { readThrows: true } : { entries: stored === null ? {} : { [THEME_PAINT_KEY]: stored } })
    .painted;

const dark = { bg: "#0F1220", scheme: "dark" };
const light = JSON.stringify({ bg: "#FFFFFF", scheme: "light" });
const other = JSON.stringify({ bg: "#1a2b3c", scheme: "dark" });

it("has an inline script", () => {
  expect(script).not.toBe("");
});

it("paints a valid paint record", () => {
  expect(paint(light)).toEqual({ bg: "#FFFFFF", scheme: "light" });
  expect(paint(other)).toEqual({ bg: "#1a2b3c", scheme: "dark" });
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

it("paints a record saved under the old key and moves it to the new key", () => {
  const { painted, data } = run({ entries: { [LEGACY_THEME_PAINT_KEY]: light } });
  expect(painted).toEqual({ bg: "#FFFFFF", scheme: "light" });
  expect(data).toEqual({ [THEME_PAINT_KEY]: light });
});

it("paints the new key when both are set", () => {
  const { painted } = run({ entries: { [THEME_PAINT_KEY]: light, [LEGACY_THEME_PAINT_KEY]: other } });
  expect(painted).toEqual({ bg: "#FFFFFF", scheme: "light" });
});

it("still paints the old key when storage writes throw", () => {
  const { painted, data } = run({ entries: { [LEGACY_THEME_PAINT_KEY]: light }, writeThrows: true });
  expect(painted).toEqual({ bg: "#FFFFFF", scheme: "light" });
  expect(data).toEqual({ [LEGACY_THEME_PAINT_KEY]: light });
});

it("names no theme ids", () => {
  expect(script).not.toContain("control-room");
});

it("is titled with the product name", () => {
  expect(/<title>(.*)<\/title>/.exec(html)?.[1]).toBe("snellius");
});

// Vite copies app/public to the site root, so each root-relative href must name a file there.
const publicFiles = Object.keys(import.meta.glob("../../../../app/public/*", { query: "?url" })).map((p) =>
  p.replace("../../../../app/public", ""),
);
const linkHrefs = [...html.matchAll(/<link\b[^>]*\brel="(?:icon|apple-touch-icon|manifest)"[^>]*>/g)].map(
  ([tag]) => /\bhref="([^"]+)"/.exec(tag)?.[1] ?? "",
);

it("links the favicon set, the apple touch icon and the manifest", () => {
  expect(linkHrefs).toEqual(
    expect.arrayContaining(["/favicon.svg", "/favicon.ico", "/apple-touch-icon.png", "/site.webmanifest"]),
  );
});

it.each(linkHrefs)("links %s, which exists in app/public", (href) => {
  expect(publicFiles).toContain(href);
});

it("lists manifest icons that exist in app/public", () => {
  const raw = Object.values(
    import.meta.glob<string>("../../../../app/public/site.webmanifest", {
      query: "?raw",
      import: "default",
      eager: true,
    }),
  )[0];
  const icons: { src: string }[] = JSON.parse(raw ?? "{}").icons ?? [];
  expect(icons.length).toBeGreaterThan(0);
  for (const { src } of icons) expect(publicFiles).toContain(src);
});
