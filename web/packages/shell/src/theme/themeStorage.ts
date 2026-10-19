import type { Theme } from "./tokens";

/** localStorage key for the chosen theme id. */
export const THEME_STORAGE_KEY = "kgateway-ui.theme";

/**
 * localStorage key for the last painted theme as {"bg":"#RRGGBB","scheme":"light"|"dark"}.
 * app/index.html reads only this record before React loads, so any theme set paints without a flash.
 */
export const THEME_PAINT_KEY = "kgateway-ui.theme.paint";

/** Returns the stored theme id only when it names a known theme; storage may be blocked. */
export function readStoredTheme(themes: Theme[]): string | null {
  try {
    const id = localStorage.getItem(THEME_STORAGE_KEY);
    return id !== null && themes.some((t) => t.id === id) ? id : null;
  } catch {
    return null;
  }
}

/** Persists the theme id; the choice just does not persist when storage is blocked. */
export function storeTheme(id: string) {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, id);
  } catch {
    // ignore
  }
}

/** Records the applied theme's background and scheme for the pre-React paint. */
export function storePaint(theme: Theme) {
  try {
    localStorage.setItem(THEME_PAINT_KEY, JSON.stringify({ bg: theme.color.bg, scheme: theme.mode }));
  } catch {
    // ignore
  }
}
