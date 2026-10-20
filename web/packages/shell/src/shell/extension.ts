import { type ComponentType, createContext, type ReactNode, useContext } from "react";
import { useParams } from "react-router-dom";
import type { Finding } from "../api/types";
import { defaultThemeIdFor, defaultThemes } from "../theme/themes";
import type { Theme } from "../theme/tokens";
import { kubernetesVocabulary, type Vocabulary } from "./vocabulary";

/** Product identity shown in the header (D-008). */
export interface Brand {
  productName: string;
  logo: ReactNode;
}

/**
 * A page contributed by an extension. Paths are relative, without a leading slash; install-scoped ones resolve
 * under /i/:id/. The core owns "findings" and "r/..." there and "/" at the top; a contribution cannot replace them.
 */
export interface RouteContribution {
  path: string;
  element: ReactNode;
  scope: "install" | "top";
}

/** A navigation entry contributed by an extension. Targets are relative, without a leading slash; install-scoped ones resolve under /i/:id/. */
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
  /** Keyed by finding code prefix, e.g. "ACME-"; when several prefixes match a code, the longest wins. */
  findingRenderers?: Record<string, FindingRenderer>;
  /** Platform copy; entries left out keep the Kubernetes defaults (D-016). */
  vocabulary?: Partial<Vocabulary>;
}

/** The core pages; the upstream implementations are exported as defaultPages. */
export interface CorePages {
  ClusterLanding: ComponentType;
  InstallPage: ComponentType;
  FindingsPage: ComponentType;
  ResourcePage: ComponentType;
}

export const defaultBrand: Brand = { productName: "kgateway", logo: null };

/** The extension merged with the upstream defaults; the vocabulary is complete. */
export type ResolvedShellExtension = Required<Omit<ShellExtension, "vocabulary">> & { vocabulary: Vocabulary };

export const ShellExtensionContext = createContext<ResolvedShellExtension>({
  brand: defaultBrand,
  themes: defaultThemes,
  defaultThemeId: defaultThemeIdFor(defaultThemes),
  routes: [],
  navEntries: [],
  findingRenderers: {},
  vocabulary: kubernetesVocabulary,
});

/** The extension merged with the upstream defaults. */
export function useShellExtension(): ResolvedShellExtension {
  return useContext(ShellExtensionContext);
}

/** The platform copy in effect. */
export function useVocabulary(): Vocabulary {
  return useContext(ShellExtensionContext).vocabulary;
}

/** The opaque install ID of the current /i/:id/... route, decoded by the router; "" outside install routes. Never split it (D-016). */
export function useInstallId(): string {
  return useParams().id ?? "";
}
