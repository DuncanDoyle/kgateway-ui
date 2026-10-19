import { css, ThemeProvider as EmotionThemeProvider, Global } from "@emotion/react";
import "@fontsource-variable/figtree";
import "@fontsource-variable/jetbrains-mono";
import { createContext, type ReactNode, useContext, useLayoutEffect, useMemo, useState } from "react";
import { readStoredTheme, storePaint, storeTheme } from "./themeStorage";
import type { Theme } from "./tokens";

// region Helpers
interface ThemeSwitch {
  themes: Theme[];
  current: Theme;
  setTheme: (id: string) => void;
}

const ThemeSwitchContext = createContext<ThemeSwitch | null>(null);

/** Returns the available themes, the current one and a setter. */
export function useThemeSwitch(): ThemeSwitch {
  const ctx = useContext(ThemeSwitchContext);
  if (!ctx) throw new Error("useThemeSwitch must be used inside ThemeProvider");
  return ctx;
}

const globalStyles = (t: Theme) => css`
  html, body, #root { height: 100%; margin: 0; }
  body { background: ${t.color.bg}; color: ${t.color.text}; font-family: ${t.font.ui}; font-size: ${t.font.size.base}; font-weight: ${t.font.weight.regular}; }
  *, *::before, *::after { box-sizing: border-box; }
  :focus-visible { outline: 2px solid ${t.color.focus}; outline-offset: 2px; }
  * { scrollbar-width: thin; scrollbar-color: ${t.color.borderStrong} transparent; }
  @media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
`;

// region Component
interface ThemeProviderProps {
  themes: Theme[];
  defaultThemeId?: string;
  children: ReactNode;
}

/** Provides the emotion theme, global styles and the theme switch. */
export function ThemeProvider({ themes, defaultThemeId, children }: ThemeProviderProps) {
  const [id, setId] = useState(
    () => readStoredTheme(themes) ?? defaultThemeId ?? (themes.find((t) => t.mode === "dark") ?? themes[0])?.id,
  );
  const current = themes.find((t) => t.id === id) ?? (themes[0] as Theme);
  // Native controls and scrollbars follow the theme, matching what index.html set before React loaded.
  useLayoutEffect(() => {
    document.documentElement.style.colorScheme = current.mode;
    document.documentElement.style.background = current.color.bg;
    storePaint(current);
  }, [current]);
  const value = useMemo<ThemeSwitch>(
    () => ({
      themes,
      current,
      setTheme: (next) => {
        setId(next);
        storeTheme(next);
      },
    }),
    [themes, current],
  );
  return (
    <ThemeSwitchContext.Provider value={value}>
      <EmotionThemeProvider theme={current}>
        <Global styles={globalStyles(current)} />
        {children}
      </EmotionThemeProvider>
    </ThemeSwitchContext.Provider>
  );
}
