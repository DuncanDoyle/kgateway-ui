// @vitest-environment node
import ts from "typescript";
import * as shell from "./index";

it("exports the shell API", () => {
  expect(shell.SHELL_API_VERSION).toBe(1);
});

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
        "CopyNameButton",
        "FindingRow",
        "HealthBadge",
        "HealthIcon",
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
        "contrastRatio",
        "controlRoomDark",
        "controlRoomLight",
        "defaultPages",
        "defaultThemes",
        "installPath",
        "isLinkable",
        "isStale",
        "resourcePath",
        "severityToState",
        "stateLabel",
        "useCapabilities",
        "useHealth",
        "useInstallId",
        "useInstalls",
        "useMeta",
        "useNow",
        "useOverview",
        "useResource",
        "useShellExtension",
        "useThemeSwitch",
        "useVocabulary",
      ],
      "types": [
        "Access",
        "Brand",
        "Category",
        "CheckResult",
        "CorePages",
        "Count",
        "ErrorResponse",
        "Finding",
        "FindingRenderer",
        "FontSizes",
        "FontWeights",
        "GatewayRow",
        "HealthReport",
        "HealthState",
        "InstallHeader",
        "InstallSummary",
        "LabelSelector",
        "LabelSelectorRequirement",
        "MetaResponse",
        "NavEntry",
        "OverviewResponse",
        "RawHealthReport",
        "Readiness",
        "Ref",
        "RouteContribution",
        "ScopeSummary",
        "ServedKind",
        "Severity",
        "ShellExtension",
        "StatusTokens",
        "Theme",
        "Vocabulary",
      ],
    }
  `);
}, 30_000);
