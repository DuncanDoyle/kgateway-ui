import type { ReactElement } from "react";
import { createRoot } from "react-dom/client";
import { defaultThemes } from "../theme/themes";

/** The longest the first render waits for fonts; a slow font then swaps in late rather than blanking the page. */
const FONT_WAIT_MS = 3000;

/**
 * Renders element into root once the given fonts (by default the default themes' UI and mono fonts) are loaded or have
 * failed, so a late font swap never reflows the first render. Apps that embed Shell call it instead of createRoot.
 */
export async function renderWhenFontsReady(
  root: HTMLElement,
  element: ReactElement,
  fonts: string[] = defaultThemes.flatMap((t) => [t.font.ui, t.font.mono]),
): Promise<void> {
  const set = typeof document !== "undefined" ? document.fonts : undefined;
  if (set?.load) {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const cap = new Promise<void>((r) => {
      timer = setTimeout(r, FONT_WAIT_MS);
    });
    const loads = Promise.all([...new Set(fonts)].map((f) => set.load(`1em ${f}`)));
    await Promise.race([loads.catch(() => undefined), cap]);
    clearTimeout(timer);
  }
  createRoot(root).render(element);
}
