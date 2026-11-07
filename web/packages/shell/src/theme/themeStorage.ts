import type { Theme } from "./tokens";

/** localStorage key for the chosen theme id. */
export const THEME_STORAGE_KEY = "snellius.theme";

/**
 * localStorage key for the last painted theme as {"bg":"#RRGGBB","scheme":"light"|"dark"}.
 * app/index.html reads only this record before React loads, so any theme set paints without a flash.
 */
export const THEME_PAINT_KEY = "snellius.theme.paint";

/** Keys from before the rename; read once, moved to the new keys, then removed. */
export const LEGACY_THEME_STORAGE_KEY = "kgateway-ui.theme";
export const LEGACY_THEME_PAINT_KEY = "kgateway-ui.theme.paint";

/** Returns the stored theme id only when it names a known theme; storage may be blocked. */
export function readStoredTheme(themes: Theme[]): string | null {
  let id: string | null;
  try {
    id = localStorage.getItem(THEME_STORAGE_KEY) ?? localStorage.getItem(LEGACY_THEME_STORAGE_KEY);
  } catch {
    return null;
  }
  moveLegacy(THEME_STORAGE_KEY, LEGACY_THEME_STORAGE_KEY);
  return id !== null && themes.some((t) => t.id === id) ? id : null;
}

/** Copies the legacy value to key unless key is set, then drops it; a failed copy keeps the legacy value. */
function moveLegacy(key: string, legacy: string) {
  try {
    const old = localStorage.getItem(legacy);
    if (old === null) return;
    if (localStorage.getItem(key) === null) localStorage.setItem(key, old);
    localStorage.removeItem(legacy);
  } catch {
    // ignore
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
    localStorage.removeItem(LEGACY_THEME_PAINT_KEY);
  } catch {
    // ignore
  }
}
