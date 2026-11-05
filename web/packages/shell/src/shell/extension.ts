import { type ComponentType, createContext, type ReactNode, useContext } from "react";
import { useParams } from "react-router-dom";
import type { Diagnostics, Finding } from "../api/types";
import { defaultThemeIdFor, defaultThemes } from "../theme/themes";
import type { Theme } from "../theme/tokens";
import type { Contributions } from "./contributions";
import { kubernetesVocabulary, type Vocabulary } from "./vocabulary";

/** Product identity shown in the header (D-008). */
export interface Brand {
  productName: string;
  logo: ReactNode;
}

/**
 * A page contributed by an extension, at a relative path (install-scoped ones under /i/:id/). Core paths win; one at or
 * below "gateways", "listenersets" or "routes" is ignored with a warning: use the CorePages slots.
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
  /** In-page slots: detail tabs, side sections, finding actions and install panels. */
  contributions?: Contributions;
  /** Base URL of the findings reference "Learn more" opens, e.g. a build's own copy of docs/findings.md; "#kgwui-0nn" is appended. */
  findingsDocUrl?: string;
}

/**
 * The core pages; the upstream implementations are exported as defaultPages. The explorer slots (sub-phase 2.2)
 * are optional: a slot left out renders the upstream page.
 */
export interface CorePages {
  ClusterLanding: ComponentType;
  InstallPage: ComponentType;
  FindingsPage: ComponentType;
  ResourcePage: ComponentType;
  GatewaysPage?: ComponentType;
  GatewayPage?: ComponentType;
  ListenerSetsPage?: ComponentType;
  ListenerSetPage?: ComponentType;
  RoutesPage?: ComponentType;
  RoutePage?: ComponentType;
  /** The finding detail page at findings/:findingId (sub-phase 3.1). */
  FindingPage?: ComponentType;
  /** The Processes page at processes (sub-phase 3.2). */
  ProcessesPage?: ComponentType;
  /** Replaces the install page's Needs attention and Processes block (sub-phase 3.2). */
  DiagnosticsSection?: ComponentType<DiagnosticsSectionProps>;
}

/** What the install page hands its diagnostics block; stale data must show every state as unknown (D-006). */
export interface DiagnosticsSectionProps {
  installId: string;
  diagnostics: Diagnostics;
  stale: boolean;
}

/** The CorePages the Shell was given, for slots that sit inside a page rather than at a route. */
export const CorePagesContext = createContext<Partial<CorePages>>({});

export const defaultBrand: Brand = { productName: "kgateway", logo: null };

/** The extension merged with the upstream defaults; the vocabulary is complete. */
export type ResolvedShellExtension = Required<Omit<ShellExtension, "vocabulary" | "findingsDocUrl">> & {
  vocabulary: Vocabulary;
  findingsDocUrl?: string;
};

export const ShellExtensionContext = createContext<ResolvedShellExtension>({
  brand: defaultBrand,
  themes: defaultThemes,
  defaultThemeId: defaultThemeIdFor(defaultThemes),
  routes: [],
  navEntries: [],
  findingRenderers: {},
  vocabulary: kubernetesVocabulary,
  contributions: {},
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
