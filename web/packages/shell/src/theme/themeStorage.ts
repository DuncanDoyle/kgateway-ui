import type { Theme } from "./tokens";

/** localStorage key for the chosen theme id; app/index.html reads the same key before React loads. */
export const THEME_STORAGE_KEY = "kgateway-ui.theme";

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

/** The theme whose mode matches the OS colour-scheme preference, if the browser reports one. */
export function themeForOsPreference(themes: Theme[]): Theme | undefined {
  try {
    const light = window.matchMedia("(prefers-color-scheme: light)").matches;
    return themes.find((t) => t.mode === (light ? "light" : "dark"));
  } catch {
    return undefined;
  }
}
