/** The four health states, always rendered as icon + colour + text. */
export type HealthState = "healthy" | "warning" | "error" | "unknown";

/** Colours for one health state. */
export interface StatusTokens {
  /** Status text colour (AA on bg and surface). */
  fg: string;
  /** Background of the status icon chip. */
  iconBg: string;
  /** Glyph colour on iconBg (at least 3:1). */
  iconFg: string;
  /** Background and border of the health strip in this state. */
  stripBg: string;
  stripBorder: string;
}

/** Type scale. Components never use literal font sizes (D-008). */
export interface FontSizes {
  /** Table headers and overlines. */
  xs: string;
  /** Secondary text, badges, chips, tooltips. */
  sm: string;
  /** Mono resource names and YAML. */
  code: string;
  /** Section headings. */
  md: string;
  /** Body text. */
  base: string;
  /** Page-state titles. */
  lg: string;
  /** Count tile numbers. */
  xl: string;
}

/** Font weights. Components never use literal weights (D-008). */
export interface FontWeights {
  regular: number;
  semibold: number;
  bold: number;
}

/** A complete theme. All visual identity flows from these tokens (D-008). */
export interface Theme {
  id: string;
  name: string;
  mode: "dark" | "light";
  color: {
    bg: string;
    surface: string;
    surfaceRaised: string;
    /** Decorative dividers only; not contrast-tested. Use borderStrong where a border marks a control. */
    border: string;
    /** Borders and scrollbar thumbs that must be seen (at least 3:1 on bg, surface and surfaceRaised). */
    borderStrong: string;
    text: string;
    textMuted: string;
    /** Fill behind text or icons only; never text on a page background (use accentText). */
    accent: string;
    accentText: string;
    headerBg: string;
    headerText: string;
    focus: string;
    status: Record<HealthState, StatusTokens>;
  };
  font: { ui: string; mono: string; size: FontSizes; weight: FontWeights };
  radius: { sm: string; md: string };
  space: (n: number) => string;
}
