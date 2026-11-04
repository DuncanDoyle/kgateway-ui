import { useTheme } from "@emotion/react";
import { act, render, screen } from "@testing-library/react";
import { FONT_FALLBACK_CLASS } from "./fontFallback";
import { ThemeProvider, useThemeSwitch } from "./ThemeProvider";
import { readStoredTheme, THEME_PAINT_KEY, THEME_STORAGE_KEY } from "./themeStorage";
import { defaultThemes } from "./themes";

function Probe() {
  const { current, setTheme } = useThemeSwitch();
  return (
    <button type="button" onClick={() => setTheme("control-room-light")}>
      {current.id}
    </button>
  );
}

function mount() {
  return render(
    <ThemeProvider themes={defaultThemes}>
      <Probe />
    </ThemeProvider>,
  );
}

function mockOsLight(light: boolean) {
  vi.stubGlobal("matchMedia", (q: string) => ({ matches: light && q.includes("light") }));
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.clear();
});

it("defaults to dark, switches, and remembers the choice", () => {
  localStorage.clear();
  const { unmount } = mount();
  expect(screen.getByRole("button")).toHaveTextContent("control-room-dark");
  act(() => screen.getByRole("button").click());
  expect(screen.getByRole("button")).toHaveTextContent("control-room-light");
  unmount();
  mount();
  expect(screen.getByRole("button")).toHaveTextContent("control-room-light");
});

it("survives localStorage throwing", () => {
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
    throw new Error("blocked");
  });
  mount();
  expect(screen.getByRole("button")).toHaveTextContent("control-room-dark");
});

it("sets color-scheme on the root to match the theme", () => {
  mount();
  expect(document.documentElement.style.colorScheme).toBe("dark");
  act(() => screen.getByRole("button").click());
  expect(document.documentElement.style.colorScheme).toBe("light");
});

describe("readStoredTheme", () => {
  it("returns only known theme ids", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "control-room-light");
    expect(readStoredTheme(defaultThemes)).toBe("control-room-light");
    localStorage.setItem(THEME_STORAGE_KEY, "retired-theme");
    expect(readStoredTheme(defaultThemes)).toBeNull();
  });
});

it("ignores the OS preference: nothing stored means dark", () => {
  mockOsLight(true);
  mount();
  expect(screen.getByRole("button")).toHaveTextContent("control-room-dark");
});

it("ignores an unknown stored id", () => {
  mockOsLight(true);
  localStorage.setItem(THEME_STORAGE_KEY, "retired-theme");
  mount();
  expect(screen.getByRole("button")).toHaveTextContent("control-room-dark");
});

it("defaultThemeId beats the dark fallback", () => {
  render(
    <ThemeProvider themes={defaultThemes} defaultThemeId="control-room-light">
      <Probe />
    </ThemeProvider>,
  );
  expect(screen.getByRole("button")).toHaveTextContent("control-room-light");
});

it("a stored id beats defaultThemeId", () => {
  localStorage.setItem(THEME_STORAGE_KEY, "control-room-dark");
  render(
    <ThemeProvider themes={defaultThemes} defaultThemeId="control-room-light">
      <Probe />
    </ThemeProvider>,
  );
  expect(screen.getByRole("button")).toHaveTextContent("control-room-dark");
});

it("writes the paint record for the applied theme", () => {
  mount();
  expect(JSON.parse(localStorage.getItem(THEME_PAINT_KEY) ?? "null")).toEqual({ bg: "#0F1220", scheme: "dark" });
  act(() => screen.getByRole("button").click());
  expect(JSON.parse(localStorage.getItem(THEME_PAINT_KEY) ?? "null")).toEqual({ bg: "#FFFFFF", scheme: "light" });
});

it("keeps the fallback fonts across theme switches once the font wait was cut short", async () => {
  /** The mono stack the emotion theme hands components. */
  function Mono() {
    return <output data-testid="mono">{useTheme().font.mono}</output>;
  }
  // Whether the body (UI) and mono stacks name their web fonts.
  const web = () => [
    getComputedStyle(document.body).fontFamily.includes("Figtree Variable"),
    screen.getByTestId("mono").textContent?.includes("JetBrains Mono Variable"),
  ];
  try {
    const plain = render(
      <ThemeProvider themes={defaultThemes}>
        <Mono />
      </ThemeProvider>,
    );
    expect(web()).toEqual([true, true]);
    plain.unmount();
    document.documentElement.classList.add(FONT_FALLBACK_CLASS);
    render(
      <ThemeProvider themes={defaultThemes}>
        <Probe />
        <Mono />
      </ThemeProvider>,
    );
    expect(web()).toEqual([false, false]);
    expect(getComputedStyle(document.body).fontFamily).toBe("Figtree, system-ui, sans-serif");
    await act(async () => screen.getByRole("button").click());
    expect(screen.getByRole("button")).toHaveTextContent("control-room-light");
    expect(web()).toEqual([false, false]);
  } finally {
    document.documentElement.classList.remove(FONT_FALLBACK_CLASS);
  }
});
