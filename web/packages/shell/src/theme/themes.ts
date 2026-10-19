import type { Theme } from "./tokens";

const font = {
  ui: "'Figtree Variable', Figtree, system-ui, sans-serif",
  mono: "'JetBrains Mono Variable', 'JetBrains Mono', ui-monospace, monospace",
  size: { xs: "10px", sm: "12px", code: "12.5px", md: "13px", base: "14px", lg: "18px", xl: "22px" },
  weight: { regular: 400, semibold: 600, bold: 700 },
};
const radius = { sm: "4px", md: "8px" };
const space = (n: number) => `${n * 4}px`;

/** "Control room" dark: logo ink #151927 and logo blue #002DC9 (D-011). The default theme. */
export const controlRoomDark: Theme = {
  id: "control-room-dark",
  name: "Control room",
  mode: "dark",
  color: {
    bg: "#0F1220",
    surface: "#151927",
    surfaceRaised: "#1B2033",
    border: "#232A44",
    borderStrong: "#636D99",
    text: "#DCE1F2",
    textMuted: "#8B93B3",
    accent: "#002DC9",
    accentText: "#9FB4FF",
    headerBg: "#151927",
    headerText: "#DCE1F2",
    focus: "#9FB4FF",
    status: {
      healthy: { fg: "#7ED6A8", iconBg: "#7ED6A8", iconFg: "#0F1220", stripBg: "#11261C", stripBorder: "#245A3E" },
      warning: { fg: "#F2C46B", iconBg: "#F2C46B", iconFg: "#0F1220", stripBg: "#2A2212", stripBorder: "#5C4A1F" },
      error: { fg: "#FF8C84", iconBg: "#FF8C84", iconFg: "#0F1220", stripBg: "#2A1418", stripBorder: "#5A2A30" },
      unknown: { fg: "#A3AAC4", iconBg: "#A3AAC4", iconFg: "#0F1220", stripBg: "#1B2033", stripBorder: "#343B57" },
    },
  },
  font,
  radius,
  space,
};

/** "Control room" light: light body, with the ink header band kept as the brand. */
export const controlRoomLight: Theme = {
  id: "control-room-light",
  name: "Control room light",
  mode: "light",
  color: {
    bg: "#FFFFFF",
    surface: "#F5F7FC",
    surfaceRaised: "#FFFFFF",
    border: "#DDE2EE",
    borderStrong: "#7A829C",
    text: "#151927",
    textMuted: "#5A6280",
    accent: "#002DC9",
    accentText: "#002DC9",
    headerBg: "#151927",
    headerText: "#FFFFFF",
    focus: "#4F6BFF",
    status: {
      healthy: { fg: "#24693F", iconBg: "#2F855A", iconFg: "#FFFFFF", stripBg: "#EAF6EF", stripBorder: "#B9DEC8" },
      warning: { fg: "#7A4F0D", iconBg: "#9A6514", iconFg: "#FFFFFF", stripBg: "#FFF6E0", stripBorder: "#EBD49A" },
      error: { fg: "#A52A23", iconBg: "#C2372F", iconFg: "#FFFFFF", stripBg: "#FDEEEE", stripBorder: "#F0C2BF" },
      unknown: { fg: "#565B6B", iconBg: "#6B7080", iconFg: "#FFFFFF", stripBg: "#F0F2F7", stripBorder: "#D5DAE6" },
    },
  },
  font,
  radius,
  space,
};

/** Upstream themes; the first is the default. */
export const defaultThemes: Theme[] = [controlRoomDark, controlRoomLight];
