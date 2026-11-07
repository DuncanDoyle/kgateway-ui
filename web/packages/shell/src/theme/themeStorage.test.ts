import {
  LEGACY_THEME_PAINT_KEY,
  LEGACY_THEME_STORAGE_KEY,
  readStoredTheme,
  storePaint,
  THEME_PAINT_KEY,
  THEME_STORAGE_KEY,
} from "./themeStorage";
import { controlRoomDark, defaultThemes } from "./themes";

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

it("uses the snellius keys", () => {
  expect(THEME_STORAGE_KEY).toBe("snellius.theme");
  expect(LEGACY_THEME_STORAGE_KEY).toBe("kgateway-ui.theme");
});

it("reads a theme saved under the old key and moves it to the new key", () => {
  localStorage.setItem(LEGACY_THEME_STORAGE_KEY, "control-room-light");
  expect(readStoredTheme(defaultThemes)).toBe("control-room-light");
  expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("control-room-light");
  expect(localStorage.getItem(LEGACY_THEME_STORAGE_KEY)).toBeNull();
});

it("reads a theme saved under the new key", () => {
  localStorage.setItem(THEME_STORAGE_KEY, "control-room-light");
  expect(readStoredTheme(defaultThemes)).toBe("control-room-light");
});

it("prefers the new key when both are set, and drops the old one", () => {
  localStorage.setItem(THEME_STORAGE_KEY, "control-room-light");
  localStorage.setItem(LEGACY_THEME_STORAGE_KEY, "control-room-dark");
  expect(readStoredTheme(defaultThemes)).toBe("control-room-light");
  expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("control-room-light");
  expect(localStorage.getItem(LEGACY_THEME_STORAGE_KEY)).toBeNull();
});

it("still reads the old key when writes throw, and keeps it", () => {
  localStorage.setItem(LEGACY_THEME_STORAGE_KEY, "control-room-light");
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("quota");
  });
  expect(readStoredTheme(defaultThemes)).toBe("control-room-light");
  expect(localStorage.getItem(LEGACY_THEME_STORAGE_KEY)).toBe("control-room-light");
});

it("storing the paint record drops the old paint key", () => {
  localStorage.setItem(LEGACY_THEME_PAINT_KEY, '{"bg":"#FFFFFF","scheme":"light"}');
  storePaint(controlRoomDark);
  expect(JSON.parse(localStorage.getItem(THEME_PAINT_KEY) ?? "null")).toEqual({ bg: "#0F1220", scheme: "dark" });
  expect(localStorage.getItem(LEGACY_THEME_PAINT_KEY)).toBeNull();
});
