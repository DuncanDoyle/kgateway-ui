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
  ])("%s >= 4.5", (_, fg, bg) => {
    expect(contrastRatio(fg, bg)).toBeGreaterThanOrEqual(4.5);
  });
  it.each([
    ["focus on bg", c.focus, c.bg],
    ["focus on surface", c.focus, c.surface],
    ["focus on surfaceRaised", c.focus, c.surfaceRaised],
    ["focus on header band", c.focus, c.headerBg],
    ["borderStrong on bg", c.borderStrong, c.bg],
    ["borderStrong on surface", c.borderStrong, c.surface],
    ["borderStrong on surfaceRaised", c.borderStrong, c.surfaceRaised],
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
