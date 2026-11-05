import type { Theme } from "./tokens";

/** Set on the root element when the first render stopped waiting for fonts; it stays for the session. */
export const FONT_FALLBACK_CLASS = "font-fallback";

/** True when the root element carries FONT_FALLBACK_CLASS. */
export function fontFallbackActive(): boolean {
  return typeof document !== "undefined" && document.documentElement.classList.contains(FONT_FALLBACK_CLASS);
}

// A theme's font stacks lead with their web font; the rest of the stack is the fallback.
const withoutWebFont = (stack: string) => stack.split(",").slice(1).join(",").trim() || stack;

/** t with its UI and mono stacks minus the web fonts, so a font that loads later never swaps in. */
export function withFallbackFonts(t: Theme): Theme {
  return { ...t, font: { ...t.font, ui: withoutWebFont(t.font.ui), mono: withoutWebFont(t.font.mono) } };
}
