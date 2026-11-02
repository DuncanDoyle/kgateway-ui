// @vitest-environment node
import ts from "typescript";
import * as shell from "./index";

it("exports the shell API", () => {
  expect(shell.SHELL_API_VERSION).toBe(1);
});

// Runs the package build (tsc -p tsconfig.build.json) first, so the check never sees a stale or missing dist.
it("the built package exports what src exports", async () => {
  const pkg = decodeURIComponent(new URL("..", import.meta.url).pathname);
  const config = ts.getParsedCommandLineOfConfigFile(
    `${pkg}tsconfig.build.json`,
    {},
    {
      ...ts.sys,
      onUnRecoverableConfigFileDiagnostic: (d) => {
        throw new Error(ts.flattenDiagnosticMessageText(d.messageText, "\n"));
      },
    },
  );
  if (!config) throw new Error("tsconfig.build.json not found");
  const program = ts.createProgram(config.fileNames, config.options);
  const diagnostics = [...ts.getPreEmitDiagnostics(program), ...program.emit().diagnostics];
  expect(diagnostics.map((d) => ts.flattenDiagnosticMessageText(d.messageText, "\n"))).toEqual([]);
  const built = await import(/* @vite-ignore */ `${pkg}dist/index.js`);
  expect(Object.keys(built).sort()).toEqual(Object.keys(shell).sort());
}, 60_000);

// The public surface is the extension contract (D-003): any added or removed export shows up here for review.
it("keeps the public export surface", () => {
  const entry = decodeURIComponent(new URL("./index.ts", import.meta.url).pathname);
  const program = ts.createProgram([entry], {
    module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.Bundler,
    jsx: ts.JsxEmit.ReactJSX,
    jsxImportSource: "@emotion/react",
    strict: true,
    skipLibCheck: true,
    noEmit: true,
  });
  const checker = program.getTypeChecker();
  const source = program.getSourceFile(entry);
  const mod = source && checker.getSymbolAtLocation(source);
  if (!mod) throw new Error("index.ts not found");
  const runtime = new Set(Object.keys(shell));
  const types = checker
    .getExportsOfModule(mod)
    .map((s) => s.getName())
    .filter((n) => !runtime.has(n));
  expect({ runtime: [...runtime].sort(), types: types.sort() }).toMatchInlineSnapshot(`
    {
      "runtime": [
        "ApiError",
        "ComputedBadge",
        "CopyCommandButton",
        "CopyNameButton",
        "FindingRow",
        "HealthBadge",
        "HealthIcon",
        "LinkedName",
        "POLL_MS",
        "PageState",
        "ResourceName",
        "SHELL_API_VERSION",
        "Shell",
        "Shimmer",
        "THEME_PAINT_KEY",
        "ThemeProvider",
        "Tooltip",
        "apiFetch",
        "confirmedOrComputed",
        "contrastRatio",
        "controlRoomDark",
        "controlRoomLight",
        "defaultPages",
        "defaultThemes",
        "installPath",
        "isLinkable",
        "isStale",
        "objectPath",
        "resourcePath",
        "severityToState",
        "stateLabel",
        "supportsFilter",
        "useCapabilities",
        "useCapabilityState",
        "useDelegation",
        "useFindingDetail",
        "useFindings",
        "useGatewayDetail",
        "useGateways",
        "useHealth",
        "useInstallId",
        "useInstalls",
        "useListFilter",
        "useListenerSetDetail",
        "useListenerSets",
        "useMeta",
        "useNow",
        "useOverview",
        "useResource",
        "useRouteDetail",
        "useRoutes",
        "useShellExtension",
        "useThemeSwitch",
        "useVocabulary",
      ],
      "types": [
        "Access",
        "AllowedListeners",
        "AllowedRoutes",
        "AncestorPath",
        "AncestorStep",
        "Attachment",
        "BackendRef",
        "BackendState",
        "Brand",
        "Candidate",
        "Capability",
        "Category",
        "CheckResult",
        "Computed",
        "ComputedAttachment",
        "Condition",
        "Contributions",
        "CorePages",
        "Count",
        "DelegationContext",
        "DelegationOutcome",
        "DelegationRef",
        "DelegationRow",
        "DetailContext",
        "DetailTabContribution",
        "EffectiveContext",
        "EffectiveEdge",
        "EffectiveRule",
        "ErrorResponse",
        "Filter",
        "Finding",
        "FindingActionContribution",
        "FindingDetail",
        "FindingList",
        "FindingRenderer",
        "FontSizes",
        "FontWeights",
        "Fraction",
        "GatewayDetail",
        "GatewayList",
        "GatewayListRow",
        "GatewayRow",
        "GroupKind",
        "HealthReport",
        "HealthState",
        "InstallHeader",
        "InstallPanelContribution",
        "InstallSummary",
        "KeyMatch",
        "LabelSelector",
        "LabelSelectorRequirement",
        "Link",
        "LinkedRef",
        "Listener",
        "ListenerAttachment",
        "ListenerSetDetail",
        "ListenerSetList",
        "ListenerSetRow",
        "ListenerStatus",
        "ListenerTLS",
        "Match",
        "MetaResponse",
        "Mirror",
        "NavEntry",
        "NextStep",
        "OverviewResponse",
        "Page",
        "ParentRef",
        "ParentStatus",
        "ParentSummary",
        "PathMatch",
        "PathStep",
        "RawHealthReport",
        "Readiness",
        "Ref",
        "RouteAttachment",
        "RouteContribution",
        "RouteCounts",
        "RouteDetail",
        "RouteList",
        "RouteRow",
        "Rule",
        "ScopeSummary",
        "ServedKind",
        "Severity",
        "ShellExtension",
        "SideSectionContribution",
        "SpecParentRef",
        "StatusTokens",
        "SuggestedFix",
        "Theme",
        "Vocabulary",
      ],
    }
  `);
}, 30_000);
