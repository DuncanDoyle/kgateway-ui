import { act, render, screen } from "@testing-library/react";
import { ThemeProvider, useThemeSwitch } from "./ThemeProvider";
import { readStoredTheme, THEME_STORAGE_KEY } from "./themeStorage";
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

it("falls back to the OS preference for an unknown stored id", () => {
  mockOsLight(true);
  localStorage.setItem(THEME_STORAGE_KEY, "retired-theme");
  mount();
  expect(screen.getByRole("button")).toHaveTextContent("control-room-light");
});

it("uses the OS dark preference when nothing is stored", () => {
  mockOsLight(false);
  mount();
  expect(screen.getByRole("button")).toHaveTextContent("control-room-dark");
});
