import { type ReactNode, useMemo } from "react";
import type { Finding, Ref } from "../api/types";
import { useShellExtension } from "./extension";

// region Types
/** The object a detail page shows, for contributions on that page. */
export interface DetailContext {
  installId: string;
  ref: Ref;
}

/** A tab on a detail page, after the core tabs; its id is its ?tab= value. */
export interface DetailTabContribution {
  id: string;
  label: string;
  render: (ctx: DetailContext) => ReactNode;
}

/** A titled section on a detail page's Overview, after the core content. */
export interface SideSectionContribution {
  id: string;
  title: string;
  render: (ctx: DetailContext) => ReactNode;
}

/** Controls on a finding row, shown where applies(f) is true; label names the group for assistive technology. */
export interface FindingActionContribution {
  id: string;
  label: string;
  applies: (f: Finding) => boolean;
  render: (f: Finding) => ReactNode;
}

/** A titled panel on the install page, below the Gateway table. */
export interface InstallPanelContribution {
  id: string;
  title: string;
  render: (ctx: { installId: string }) => ReactNode;
}

/** In-page slots; per-kind slots are keyed "group/Kind", e.g. "gateway.networking.k8s.io/Gateway". A slot with nothing renders nothing. */
export interface Contributions {
  detailTabs?: Record<string, DetailTabContribution[]>;
  sideSections?: Record<string, SideSectionContribution[]>;
  findingActions?: FindingActionContribution[];
  installPanels?: InstallPanelContribution[];
}

// region Helpers
const NONE: never[] = [];
const kindKey = (r: Ref) => `${r.group}/${r.kind}`;

// region Hooks
/** Contributed tabs for ref's kind. An id that names a core tab, or repeats an earlier one, is ignored with a warning. */
export function useDetailTabContributions(ref: Ref, coreIds: readonly string[]): DetailTabContribution[] {
  const all = useShellExtension().contributions.detailTabs?.[kindKey(ref)] ?? NONE;
  const core = coreIds.join("\n");
  return useMemo(() => {
    const taken = new Set(core.split("\n"));
    return all.filter((t) => {
      if (taken.has(t.id)) {
        console.warn(`kgateway-ui: ignoring contributed tab "${t.id}": the id is already taken.`);
        return false;
      }
      taken.add(t.id);
      return true;
    });
  }, [all, core]);
}

/** Contributed sections for ref's kind. */
export function useSideSectionContributions(ref: Ref): SideSectionContribution[] {
  return useShellExtension().contributions.sideSections?.[kindKey(ref)] ?? NONE;
}

/** The contributed actions that apply to f. */
export function useFindingActions(f: Finding): FindingActionContribution[] {
  const all = useShellExtension().contributions.findingActions ?? NONE;
  return useMemo(() => all.filter((a) => a.applies(f)), [all, f]);
}

/** The contributed install-page panels. */
export function useInstallPanels(): InstallPanelContribution[] {
  return useShellExtension().contributions.installPanels ?? NONE;
}
