import type { ReactElement } from "react";
import { createRoot } from "react-dom/client";
import { FONT_FALLBACK_CLASS } from "../theme/fontFallback";
import { defaultThemes } from "../theme/themes";

/** The longest the first render waits for fonts; past it the page keeps the fallback fonts for the session. */
const FONT_WAIT_MS = 3000;

/**
 * Renders element into root once the given fonts (by default the default themes' UI and mono fonts) are loaded or have
 * failed; past the cap it sets FONT_FALLBACK_CLASS instead. Apps that embed Shell call it instead of createRoot.
 */
export async function renderWhenFontsReady(
  root: HTMLElement,
  element: ReactElement,
  fonts: string[] = defaultThemes.flatMap((t) => [t.font.ui, t.font.mono]),
): Promise<void> {
  const set = typeof document !== "undefined" ? document.fonts : undefined;
  if (set?.load) {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const cap = new Promise<"cap">((r) => {
      timer = setTimeout(() => r("cap"), FONT_WAIT_MS);
    });
    // allSettled: a failed font must not cut the wait for the others.
    const loads = Promise.allSettled([...new Set(fonts)].map((f) => set.load(`1em ${f}`)));
    if ((await Promise.race([loads, cap])) === "cap") document.documentElement.classList.add(FONT_FALLBACK_CLASS);
    clearTimeout(timer);
  }
  createRoot(root).render(element);
}
