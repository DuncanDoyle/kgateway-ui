/// <reference path="./theme/emotion.d.ts" />

export { contrastRatio } from "./theme/contrast";
export { ThemeProvider, useThemeSwitch } from "./theme/ThemeProvider";
export { THEME_PAINT_KEY } from "./theme/themeStorage";
export { controlRoomDark, controlRoomLight, defaultThemes } from "./theme/themes";
export type { FontSizes, FontWeights, HealthState, StatusTokens, Theme } from "./theme/tokens";

/** Version of the shell's extension contract; bumped on breaking changes (D-003). */
export const SHELL_API_VERSION = 1;

export { ApiError, apiFetch } from "./api/client";
export { useNow } from "./api/clock";
export { isLinkable, isStale, POLL_MS, useHealth, useInstalls, useMeta, useOverview, useResource } from "./api/hooks";
export * from "./api/types";
export { HealthBadge } from "./health/HealthBadge";
export { HealthIcon } from "./health/HealthIcon";
export { severityToState, stateLabel } from "./health/state";
export { defaultPages } from "./pages/defaultPages";
export { FindingRow, resourcePath } from "./pages/install/FindingRow";
export { CopyNameButton } from "./resource/CopyNameButton";
export { ResourceName } from "./resource/ResourceName";
export type { Brand, CorePages, FindingRenderer, NavEntry, RouteContribution, ShellExtension } from "./shell/extension";
export { useInstallId, useShellExtension } from "./shell/extension";
export { Shell } from "./shell/Shell";
export { Tooltip } from "./shell/Tooltip";
export { PageState } from "./states/PageState";
export { Shimmer } from "./states/Shimmer";
