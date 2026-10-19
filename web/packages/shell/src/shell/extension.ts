import { type ComponentType, createContext, type ReactNode, useContext } from "react";
import { useParams } from "react-router-dom";
import type { Finding } from "../api/types";
import { defaultThemes } from "../theme/themes";
import type { Theme } from "../theme/tokens";

/** Product identity shown in the header (D-008). */
export interface Brand {
  productName: string;
  logo: ReactNode;
}

/** A page contributed by an extension. Install-scoped paths are relative to /i/:ns/:name/. */
export interface RouteContribution {
  path: string;
  element: ReactNode;
  scope: "install" | "top";
}

/** A navigation entry contributed by an extension. Install-scoped targets are relative to /i/:ns/:name/. */
export interface NavEntry {
  label: string;
  to: string;
  icon?: ReactNode;
  scope: "install" | "top";
}

/** Renders extra detail for findings whose code starts with a registered prefix. */
export type FindingRenderer = (f: Finding) => ReactNode;

/** Everything a downstream app can contribute to the shell (D-003). */
export interface ShellExtension {
  brand?: Brand;
  themes?: Theme[];
  defaultThemeId?: string;
  routes?: RouteContribution[];
  navEntries?: NavEntry[];
  /** Keyed by finding code prefix, e.g. "ACME-". */
  findingRenderers?: Record<string, FindingRenderer>;
}

/** The core pages; the upstream implementations are exported as defaultPages. */
export interface CorePages {
  ClusterLanding: ComponentType;
  InstallPage: ComponentType;
  FindingsPage: ComponentType;
  ResourcePage: ComponentType;
}

export const defaultBrand: Brand = { productName: "kgateway", logo: null };

export const ShellExtensionContext = createContext<Required<ShellExtension>>({
  brand: defaultBrand,
  themes: defaultThemes,
  defaultThemeId: defaultThemes[0]?.id ?? "",
  routes: [],
  navEntries: [],
  findingRenderers: {},
});

/** The extension merged with the upstream defaults. */
export function useShellExtension(): Required<ShellExtension> {
  return useContext(ShellExtensionContext);
}

/** The install ID ("ns/name") of the current /i/:ns/:name route. */
export function useInstallId(): string {
  const { ns, name } = useParams();
  return `${ns}/${name}`;
}
