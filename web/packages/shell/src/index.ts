import "./theme/emotion";

export { contrastRatio } from "./theme/contrast";
export { ThemeProvider, useThemeSwitch } from "./theme/ThemeProvider";
export { THEME_PAINT_KEY } from "./theme/themeStorage";
export { controlRoomDark, controlRoomLight, defaultThemes } from "./theme/themes";
export type { FontSizes, FontWeights, HealthState, StatusTokens, Theme } from "./theme/tokens";

/** Version of the shell's extension contract; bumped on breaking changes (D-003). */
export const SHELL_API_VERSION = 1;

export { ApiError, apiFetch } from "./api/client";
export { useNow } from "./api/clock";
export { useDiagnostics, useFindingDetail, useFindings, useProcesses } from "./api/diagHooks";
export {
  useDelegation,
  useGatewayDetail,
  useGateways,
  useListenerSetDetail,
  useListenerSets,
  useRouteDetail,
  useRoutes,
} from "./api/exploreHooks";
export { confirmedOrComputed } from "./api/freshness";
export {
  isLinkable,
  isStale,
  POLL_MS,
  supportsFilter,
  useCapabilities,
  useCapabilityState,
  useHealth,
  useInstalls,
  useListFilter,
  useMeta,
  useOverview,
  useResource,
} from "./api/hooks";
export * from "./api/types";
export { EvidenceView } from "./diagnostics/EvidenceView";
export { DiagnosticsGroupRow, GROUP_ROW_H } from "./diagnostics/GroupRow";
export { NextSteps } from "./diagnostics/NextSteps";
export { PROCESS_LINE_H, ProcessLine } from "./diagnostics/ProcessLine";
export { ProcessTable } from "./diagnostics/ProcessTable";
export { ComputedBadge } from "./explore/ComputedBadge";
export { CopyCommandButton } from "./explore/CopyCommandButton";
export { LinkedName } from "./explore/LinkedName";
export { linkTarget, objectPath } from "./explore/objectPath";
export { HealthBadge } from "./health/HealthBadge";
export { HealthIcon } from "./health/HealthIcon";
export { severityToState, stateLabel } from "./health/state";
export { defaultPages } from "./pages/defaultPages";
export { FindingRow, resourcePath } from "./pages/install/FindingRow";
export { CopyNameButton } from "./resource/CopyNameButton";
export { ResourceName } from "./resource/ResourceName";
export type { BreadcrumbsProps } from "./shell/Breadcrumbs";
export { Breadcrumbs } from "./shell/Breadcrumbs";
export type {
  Contributions,
  DetailContext,
  DetailTabContribution,
  FindingActionContribution,
  InstallPanelContribution,
  SideSectionContribution,
} from "./shell/contributions";
export type {
  Brand,
  CorePages,
  DiagnosticsSectionProps,
  FindingRenderer,
  NavEntry,
  RouteContribution,
  ShellExtension,
} from "./shell/extension";
export { useInstallId, useShellExtension, useVocabulary } from "./shell/extension";
export { DetailColumns, layout } from "./shell/layout";
export type { ObjectHeaderProps } from "./shell/ObjectHeader";
export { ObjectHeader } from "./shell/ObjectHeader";
export { installPath } from "./shell/paths";
export { renderWhenFontsReady } from "./shell/renderWhenFontsReady";
export { Shell } from "./shell/Shell";
export { Tooltip } from "./shell/Tooltip";
export type { Crumb } from "./shell/trail";
export { useLinkVia } from "./shell/trail";
export type { Vocabulary } from "./shell/vocabulary";
export { PageState } from "./states/PageState";
export { Shimmer } from "./states/Shimmer";
