/// <reference path="./theme/emotion.d.ts" />

export { contrastRatio } from "./theme/contrast";
export { ThemeProvider, useThemeSwitch } from "./theme/ThemeProvider";
export { controlRoomDark, controlRoomLight, defaultThemes } from "./theme/themes";
export type { FontSizes, FontWeights, HealthState, StatusTokens, Theme } from "./theme/tokens";

/** Version of the shell's extension contract; bumped on breaking changes (D-003). */
export const SHELL_API_VERSION = 1;
