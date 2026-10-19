import { CHIP_PRESSED_ACTIVE_PCT, CHIP_PRESSED_HOVER_PCT, CHIP_PRESSED_PCT } from "../pages/FindingsPage";
import { HEADER_MUTED_PCT, HEADER_PRESS_PCT } from "../shell/headerTint";
import { contrastRatio } from "./contrast";
import { controlRoomDark, controlRoomLight, defaultThemes } from "./themes";
import type { HealthState } from "./tokens";

const states: HealthState[] = ["healthy", "warning", "error", "unknown"];

it("computes WCAG contrast", () => {
  expect(contrastRatio("#000000", "#ffffff")).toBeCloseTo(21, 0);
  expect(contrastRatio("#777777", "#ffffff")).toBeCloseTo(4.48, 1);
});

describe.each([controlRoomDark, controlRoomLight])("$name theme meets WCAG AA", (t) => {
  const c = t.color;
  const headerMix = (pct: number) => mix(c.headerText, c.headerBg, pct / 100);
  it.each([
    ["text on bg", c.text, c.bg],
    ["text on surface", c.text, c.surface],
    ["text on surfaceRaised", c.text, c.surfaceRaised],
    ["muted on bg", c.textMuted, c.bg],
    ["muted on surface", c.textMuted, c.surface],
    ["muted on surfaceRaised", c.textMuted, c.surfaceRaised],
    ["header text on header band", c.headerText, c.headerBg],
    ["accent text on bg", c.accentText, c.bg],
    ["accent text on surface", c.accentText, c.surface],
    ["accent text on surfaceRaised", c.accentText, c.surfaceRaised],
    ["header text on accent chip", c.headerText, c.accent],
    // Pressed header controls and menu items (the strongest tints the shell applies).
    ["header text on pressed header control", c.headerText, headerMix(HEADER_PRESS_PCT)],
    ["header muted text on header band", headerMix(HEADER_MUTED_PCT), c.headerBg],
    ["header muted text on pressed header control", headerMix(HEADER_MUTED_PCT), headerMix(HEADER_PRESS_PCT)],
    ["text on pressed menu item", c.text, mix(c.text, c.surfaceRaised, 0.16)],
    // Install page tints on surface: muted text sits on rows (pressed 8%); the rest up to 14% (sort, "Show all").
    ["text on pressed surface control", c.text, mix(c.text, c.surface, 0.14)],
    ["muted on pressed table row", c.textMuted, mix(c.text, c.surface, 0.08)],
    ["accent text on pressed surface control", c.accentText, mix(c.text, c.surface, 0.14)],
    // Selected filter chips: accent fill at rest, hover and press.
    ...[CHIP_PRESSED_PCT, CHIP_PRESSED_HOVER_PCT, CHIP_PRESSED_ACTIVE_PCT].map((pct): [string, string, string] => [
      `text on selected chip (${pct}%)`,
      c.text,
      mix(c.accentText, c.surface, pct / 100),
    ]),
    ...states.map((s): [string, string, string] => [
      `${s} status text on pressed row`,
      c.status[s].fg,
      mix(c.text, c.surface, 0.14),
    ]),
  ])("%s >= 4.5", (_, fg, bg) => {
    expect(contrastRatio(fg, bg)).toBeGreaterThanOrEqual(4.5);
  });
  it.each<[string, string, string]>([
    ["focus on bg", c.focus, c.bg],
    ["focus on surface", c.focus, c.surface],
    ["focus on surfaceRaised", c.focus, c.surfaceRaised],
    ["focus on header band", c.focus, c.headerBg],
    ["borderStrong on bg", c.borderStrong, c.bg],
    ["borderStrong on surface", c.borderStrong, c.surface],
    ["borderStrong on surfaceRaised", c.borderStrong, c.surfaceRaised],
    ["borderStrong on header band", c.borderStrong, c.headerBg],
    // A selected chip's accent border shows against the page, the unselected fill and its own fill.
    ["selected chip border on surface", c.accentText, c.surface],
    ["selected chip border on bg", c.accentText, c.bg],
    ["selected chip border on its fill", c.accentText, mix(c.accentText, c.surface, CHIP_PRESSED_ACTIVE_PCT / 100)],
    // The readiness icon sits on the ink band in every theme.
    ...states.map((s): [string, string, string] => [`${s} status icon on header band`, c.status[s].iconBg, c.headerBg]),
  ])("%s >= 3 (non-text)", (_, fg, bg) => {
    expect(contrastRatio(fg, bg)).toBeGreaterThanOrEqual(3);
  });
  it.each(states)("%s status text >= 4.5 on every background it sits on", (s) => {
    const st = c.status[s];
    for (const bg of [c.bg, c.surface, c.surfaceRaised, st.stripBg]) {
      expect(contrastRatio(st.fg, bg)).toBeGreaterThanOrEqual(4.5);
    }
    expect(contrastRatio(c.text, st.stripBg)).toBeGreaterThanOrEqual(4.5);
    expect(contrastRatio(c.textMuted, st.stripBg)).toBeGreaterThanOrEqual(4.5);
  });
  it.each(states)("%s status icon is visible (>= 3 non-text)", (s) => {
    const st = c.status[s];
    expect(contrastRatio(st.iconFg, st.iconBg)).toBeGreaterThanOrEqual(3);
    for (const bg of [c.bg, c.surface, c.surfaceRaised, st.stripBg]) {
      expect(contrastRatio(st.iconBg, bg)).toBeGreaterThanOrEqual(3);
    }
  });
});

it("dark is the default and first theme", () => {
  expect(defaultThemes[0]?.id).toBe("control-room-dark");
});

it.each([controlRoomDark, controlRoomLight])("$name defines the full type scale", (t) => {
  expect(Object.keys(t.font.size).sort()).toEqual(["base", "code", "lg", "md", "sm", "xl", "xs"]);
  expect(Object.keys(t.font.weight).sort()).toEqual(["bold", "regular", "semibold"]);
});

it("names the families the bundled variable fonts register", () => {
  expect(controlRoomDark.font.ui).toContain("'Figtree Variable'");
  expect(controlRoomDark.font.mono).toContain("'JetBrains Mono Variable'");
});

// region Helpers
// Same result as CSS color-mix(in srgb, a w, b) for #RRGGBB inputs.
function mix(a: string, b: string, w: number): string {
  const ch = (h: string, i: number) => Number.parseInt(h.slice(1 + 2 * i, 3 + 2 * i), 16);
  const out = [0, 1, 2].map((i) => Math.round(ch(a, i) * w + ch(b, i) * (1 - w)));
  return `#${out.map((v) => v.toString(16).padStart(2, "0")).join("")}`;
}
