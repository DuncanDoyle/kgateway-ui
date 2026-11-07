import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { type ComponentType, useMemo } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { AboutPage } from "../pages/AboutPage";
import { defaultPages } from "../pages/defaultPages";
import { PageState } from "../states/PageState";
import { ThemeProvider } from "../theme/ThemeProvider";
import { defaultThemeIdFor, defaultThemes } from "../theme/themes";
import { DocumentTitle } from "./DocumentTitle";
import {
  type CorePages,
  CorePagesContext,
  defaultBrand,
  type ResolvedShellExtension,
  type RouteContribution,
  type ShellExtension,
  ShellExtensionContext,
} from "./extension";
import { Header } from "./Header";
import { ABOUT_PATH } from "./paths";
import { RootRedirect } from "./RootRedirect";
import { kubernetesVocabulary } from "./vocabulary";

// region Styles
const Root = styled.div<{ stylesOverride?: SerializedStyles }>`
  min-height: 100%;
  ${(p) => p.stylesOverride}
`;

// region Helpers
const noExtension: ShellExtension = {};

// The explorer's install-scoped paths (design §8.2); the CorePages slots replace their pages.
const RESERVED = ["gateways", "listenersets", "routes"];

/** The reserved path an install-scoped contribution sits at or below, if any; every other path competes by router rank as in Phase 1. */
export function reservedPathOf(r: RouteContribution): string | undefined {
  if (r.scope !== "install") return undefined;
  const first = r.path.split("/").find(Boolean);
  return RESERVED.find((p) => p === first);
}

// region Component
interface ShellProps {
  extension?: ShellExtension;
  pages: CorePages;
  stylesOverride?: SerializedStyles;
}

/** The application shell. Upstream and downstream apps render this with their own extension. */
export function Shell({ extension = noExtension, pages, stylesOverride }: ShellProps) {
  const ext = useMemo<ResolvedShellExtension>(() => {
    const themes = extension.themes ?? defaultThemes;
    return {
      brand: extension.brand ?? defaultBrand,
      themes,
      defaultThemeId: defaultThemeIdFor(themes, extension.defaultThemeId),
      routes: extension.routes ?? [],
      navEntries: extension.navEntries ?? [],
      findingRenderers: extension.findingRenderers ?? {},
      contributions: extension.contributions ?? {},
      vocabulary: { ...kubernetesVocabulary, ...extension.vocabulary },
      findingsDocUrl: extension.findingsDocUrl,
    };
  }, [extension]);
  // A contribution at or below a reserved path is ignored, with one warning, so it cannot shadow an explorer page.
  const contributed = useMemo(
    () =>
      ext.routes.filter((r) => {
        const reserved = reservedPathOf(r);
        if (reserved)
          console.warn(
            `snellius: ignoring contributed route "${r.path}": "${reserved}" is reserved; use its CorePages slot.`,
          );
        return !reserved;
      }),
    [ext.routes],
  );
  const { ClusterLanding, InstallPage, FindingsPage, ResourcePage } = pages;
  // A slot a build leaves out renders the upstream page, so a Phase 1 build still gets the explorer.
  const GatewaysPage = pages.GatewaysPage ?? defaultPages.GatewaysPage;
  const GatewayPage = pages.GatewayPage ?? defaultPages.GatewayPage;
  const ListenerSetsPage = pages.ListenerSetsPage ?? defaultPages.ListenerSetsPage;
  const ListenerSetPage = pages.ListenerSetPage ?? defaultPages.ListenerSetPage;
  const RoutesPage = pages.RoutesPage ?? defaultPages.RoutesPage;
  const RoutePage = pages.RoutePage ?? defaultPages.RoutePage;
  const FindingPage = pages.FindingPage ?? defaultPages.FindingPage;
  const ProcessesPage = pages.ProcessesPage ?? defaultPages.ProcessesPage;
  // Core pages under /i/:id/; the install nav shows an entry only when its path is listed here.
  const installRoutes: [string, ComponentType][] = [
    ["", InstallPage],
    ["gateways", GatewaysPage],
    ["routes", RoutesPage],
    ["listenersets", ListenerSetsPage],
    ["gateways/:ns/:name", GatewayPage],
    ["listenersets/:group/:kind/:ns/:name", ListenerSetPage],
    ["routes/:group/:kind/:ns/:name", RoutePage],
    ["findings", FindingsPage],
    ["findings/:findingId", FindingPage],
    ["processes", ProcessesPage],
    ["r/:group/:kind/:rns/:rname", ResourcePage],
  ];
  return (
    <ShellExtensionContext.Provider value={ext}>
      <CorePagesContext.Provider value={pages}>
        {/* The provider and the context share one resolved default; a stored choice still wins (D-011). */}
        <ThemeProvider themes={ext.themes} defaultThemeId={ext.defaultThemeId}>
          <BrowserRouter>
            <Root data-testid="shell-root" stylesOverride={stylesOverride}>
              <DocumentTitle />
              <Header installPaths={installRoutes.map(([path]) => path)} />
              <main>
                <Routes>
                  <Route path="/" element={<RootRedirect ClusterLanding={ClusterLanding} />} />
                  <Route path={ABOUT_PATH} element={<AboutPage />} />
                  {installRoutes.map(([path, Page]) => (
                    <Route key={path} path={path ? `/i/:id/${path}` : "/i/:id"} element={<Page />} />
                  ))}
                  {contributed.map((r) => (
                    <Route
                      key={`${r.scope}:${r.path}`}
                      path={r.scope === "install" ? `/i/:id/${r.path}` : `/${r.path}`}
                      element={r.element}
                    />
                  ))}
                  <Route path="*" element={<PageState kind="empty" title="Page not found" />} />
                </Routes>
              </main>
            </Root>
          </BrowserRouter>
        </ThemeProvider>
      </CorePagesContext.Provider>
    </ShellExtensionContext.Provider>
  );
}
