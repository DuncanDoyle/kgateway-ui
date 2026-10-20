import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useMemo } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { PageState } from "../states/PageState";
import { ThemeProvider } from "../theme/ThemeProvider";
import { defaultThemeIdFor, defaultThemes } from "../theme/themes";
import { DocumentTitle } from "./DocumentTitle";
import { type CorePages, defaultBrand, type ShellExtension, ShellExtensionContext } from "./extension";
import { Header } from "./Header";
import { RootRedirect } from "./RootRedirect";

// region Styles
const Root = styled.div<{ stylesOverride?: SerializedStyles }>`
  min-height: 100%;
  ${(p) => p.stylesOverride}
`;

// region Helpers
const noExtension: ShellExtension = {};

// region Component
interface ShellProps {
  extension?: ShellExtension;
  pages: CorePages;
  stylesOverride?: SerializedStyles;
}

/** The application shell. Upstream and downstream apps render this with their own extension. */
export function Shell({ extension = noExtension, pages, stylesOverride }: ShellProps) {
  const ext = useMemo<Required<ShellExtension>>(() => {
    const themes = extension.themes ?? defaultThemes;
    return {
      brand: extension.brand ?? defaultBrand,
      themes,
      defaultThemeId: defaultThemeIdFor(themes, extension.defaultThemeId),
      routes: extension.routes ?? [],
      navEntries: extension.navEntries ?? [],
      findingRenderers: extension.findingRenderers ?? {},
    };
  }, [extension]);
  const { ClusterLanding, InstallPage, FindingsPage, ResourcePage } = pages;
  return (
    <ShellExtensionContext.Provider value={ext}>
      {/* The provider and the context share one resolved default; a stored choice still wins (D-011). */}
      <ThemeProvider themes={ext.themes} defaultThemeId={ext.defaultThemeId}>
        <BrowserRouter>
          <Root data-testid="shell-root" stylesOverride={stylesOverride}>
            <DocumentTitle />
            <Header />
            <main>
              <Routes>
                <Route path="/" element={<RootRedirect ClusterLanding={ClusterLanding} />} />
                <Route path="/i/:ns/:name" element={<InstallPage />} />
                <Route path="/i/:ns/:name/findings" element={<FindingsPage />} />
                <Route path="/i/:ns/:name/r/:group/:kind/:rns/:rname" element={<ResourcePage />} />
                {ext.routes.map((r) => (
                  <Route
                    key={`${r.scope}:${r.path}`}
                    path={r.scope === "install" ? `/i/:ns/:name/${r.path}` : `/${r.path}`}
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
