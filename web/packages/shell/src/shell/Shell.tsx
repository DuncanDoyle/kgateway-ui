import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { type ComponentType, useMemo } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { defaultPages } from "../pages/defaultPages";
import { PageState } from "../states/PageState";
import { ThemeProvider } from "../theme/ThemeProvider";
import { defaultThemeIdFor, defaultThemes } from "../theme/themes";
import { DocumentTitle } from "./DocumentTitle";
import {
  type CorePages,
  defaultBrand,
  type ResolvedShellExtension,
  type ShellExtension,
  ShellExtensionContext,
} from "./extension";
import { Header } from "./Header";
import { RootRedirect } from "./RootRedirect";
import { kubernetesVocabulary } from "./vocabulary";

// region Styles
const Root = styled.div<{ stylesOverride?: SerializedStyles }>`
  min-height: 100%;
  ${(p) => p.stylesOverride}
`;

// region Helpers
const noExtension: ShellExtension = {};

const segments = (path: string) => path.split("/").filter(Boolean);
const sameSegment = (a: string, b: string) => a === b || a.startsWith(":") || b.startsWith(":");

/** Whether some URL matches both patterns; a trailing "*" matches any rest, including none. */
export function pathsOverlap(a: string, b: string): boolean {
  const [x, y] = [segments(a), segments(b)];
  const splat = (p: string[], q: string[]) => {
    const prefix = p.slice(0, -1);
    return q.length >= prefix.length && prefix.every((s, i) => q[i] === "*" || sameSegment(s, q[i] ?? ""));
  };
  if (x.at(-1) === "*") return splat(x, y);
  if (y.at(-1) === "*") return splat(y, x);
  return x.length === y.length && x.every((s, i) => sameSegment(s, y[i] ?? ""));
}

// The core owns "/" and everything under /i/:id/ at the top.
const coreTopPaths = ["", "i/:id/*"];

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
      vocabulary: { ...kubernetesVocabulary, ...extension.vocabulary },
    };
  }, [extension]);
  const { ClusterLanding, InstallPage, FindingsPage, ResourcePage } = pages;
  // A slot a build leaves out renders the upstream page, so a Phase 1 build still gets the explorer.
  const GatewaysPage = pages.GatewaysPage ?? defaultPages.GatewaysPage;
  const GatewayPage = pages.GatewayPage ?? defaultPages.GatewayPage;
  const ListenerSetsPage = pages.ListenerSetsPage ?? defaultPages.ListenerSetsPage;
  const ListenerSetPage = pages.ListenerSetPage ?? defaultPages.ListenerSetPage;
  const RoutesPage = pages.RoutesPage ?? defaultPages.RoutesPage;
  const RoutePage = pages.RoutePage ?? defaultPages.RoutePage;
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
    ["r/:group/:kind/:rns/:rname", ResourcePage],
  ];
  // A contribution that could match a core URL is ignored: it would replace or shadow a core page.
  const contributed = ext.routes.filter((r) =>
    (r.scope === "install" ? installRoutes.map(([p]) => p) : coreTopPaths).every((core) => !pathsOverlap(r.path, core)),
  );
  return (
    <ShellExtensionContext.Provider value={ext}>
      {/* The provider and the context share one resolved default; a stored choice still wins (D-011). */}
      <ThemeProvider themes={ext.themes} defaultThemeId={ext.defaultThemeId}>
        <BrowserRouter>
          <Root data-testid="shell-root" stylesOverride={stylesOverride}>
            <DocumentTitle />
            <Header installPaths={installRoutes.map(([path]) => path)} />
            <main>
              <Routes>
                <Route path="/" element={<RootRedirect ClusterLanding={ClusterLanding} />} />
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
    </ShellExtensionContext.Provider>
  );
}
