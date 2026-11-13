import type { LucideIcon } from "lucide-react";
import { Component, type ReactNode, useMemo } from "react";
import { useFeatureCheck } from "../api/hooks";
import type { Finding, Ref } from "../api/types";
import { useShellExtension } from "./extension";

// region Types
/** The object a detail page shows, for contributions on that page. */
export interface DetailContext {
  installId: string;
  ref: Ref;
  /** The page's detail as the API sent it; undefined until it loads. */
  detail?: unknown;
}

/** A tab on a detail page, after the core tabs; its id is its ?tab= value. */
export interface DetailTabContribution {
  id: string;
  label: string;
  /** Drawn before the label; optional. */
  icon?: LucideIcon;
  render: (ctx: DetailContext) => ReactNode;
  /** Shown only where the install has this feature (InstallSummary.features). */
  feature?: string;
  /** Whether the loaded object has the tab; a deep link keeps it while the detail loads. */
  applies?: (ctx: DetailContext) => boolean;
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

/**
 * In-page slots; per-kind slots are keyed "group/Kind", e.g. "gateway.networking.k8s.io/Gateway", and a core-group kind keys as "/Kind".
 * Ids must not contain whitespace: they are used in element ids. A slot with nothing renders nothing.
 */
export interface Contributions {
  detailTabs?: Record<string, DetailTabContribution[]>;
  sideSections?: Record<string, SideSectionContribution[]>;
  findingActions?: FindingActionContribution[];
  installPanels?: InstallPanelContribution[];
}

// region Helpers
const NONE: never[] = [];

/** The slots of all, in order: per-kind lists and the flat lists are concatenated. */
export function mergeContributions(all: (Contributions | undefined)[]): Contributions {
  const keyed = <T,>(pick: (c: Contributions) => Record<string, T[]> | undefined) => {
    const out: Record<string, T[]> = {};
    for (const c of all) for (const [k, v] of Object.entries((c && pick(c)) ?? {})) out[k] = [...(out[k] ?? []), ...v];
    return out;
  };
  return {
    detailTabs: keyed((c) => c.detailTabs),
    sideSections: keyed((c) => c.sideSections),
    findingActions: all.flatMap((c) => c?.findingActions ?? []),
    installPanels: all.flatMap((c) => c?.installPanels ?? []),
  };
}
const kindKey = (r: Ref) => `${r.group}/${r.kind}`;
const warned = new Set<string>();

/** Logs message once per key for the life of the page. */
function warnOnce(key: string, message: string) {
  if (warned.has(key)) return;
  warned.add(key);
  console.warn(message);
}

/** Forgets which warnings were logged; for tests. */
export function resetContributionWarnings() {
  warned.clear();
}

function warnFailed(id: string, err: unknown) {
  warnOnce(`failed:${id}`, `snellius: hiding contribution "${id}": it threw ${String(err)}`);
}

/** Drops entries whose id repeats an earlier one, with a warning. */
function unique<T extends { id: string }>(all: T[], slot: string): T[] {
  const seen = new Set<string>();
  return all.filter((c) => {
    if (seen.has(c.id)) {
      warnOnce(`taken:${slot}:${c.id}`, `snellius: ignoring contributed ${slot} "${c.id}": the id is already taken.`);
      return false;
    }
    seen.add(c.id);
    return true;
  });
}

function Call({ render }: { render: () => ReactNode }) {
  return <>{render()}</>;
}

// region Component
interface SlotProps {
  id: string;
  /** Names what the contribution renders for, e.g. the object; a change retries a failed render. */
  resetKey: string;
  render: () => ReactNode;
}

/** Renders a contribution; when it throws, renders nothing and warns once per id, so the core page stays up. */
export class Slot extends Component<SlotProps, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  componentDidCatch(err: unknown) {
    warnFailed(this.props.id, err);
  }
  componentDidUpdate(prev: SlotProps) {
    if (this.state.failed && prev.resetKey !== this.props.resetKey) this.setState({ failed: false });
  }
  render() {
    return this.state.failed ? null : <Call render={this.props.render} />;
  }
}

/** What a finding is about, for a Slot's resetKey. */
export const findingKey = (f: Finding) => {
  const r = f.resource;
  return [f.install, f.code, r ? `${r.group}/${r.kind}/${r.namespace ?? ""}/${r.name}` : ""].join("|");
};

// region Hooks
/** Contributed tabs for ref's kind. An id that names a core tab, or repeats an earlier one, is ignored with a warning. */
export function useDetailTabContributions(ref: Ref, coreIds: readonly string[]): DetailTabContribution[] {
  const all = useShellExtension().contributions.detailTabs?.[kindKey(ref)] ?? NONE;
  const core = coreIds.join("\n");
  return useMemo(() => {
    const taken = new Set(core.split("\n"));
    return all.filter((t) => {
      if (taken.has(t.id)) {
        warnOnce(`taken:tab:${t.id}`, `snellius: ignoring contributed tab "${t.id}": the id is already taken.`);
        return false;
      }
      taken.add(t.id);
      return true;
    });
  }, [all, core]);
}

/**
 * The contributed tabs shown for ref: a tab's feature must not be missing from the install, and applies must hold for
 * the loaded detail. While either is unknown, only the tab ?tab= asks for keeps its place, so a deep link never jumps.
 */
export function useShownDetailTabs(
  installId: string,
  ref: Ref,
  detail: unknown,
  coreIds: readonly string[],
  asked: string | null,
): DetailTabContribution[] {
  const all = useDetailTabContributions(ref, coreIds);
  const has = useFeatureCheck(installId);
  return useMemo(() => {
    const ctx: DetailContext = { installId, ref, detail };
    return all.filter((t) => {
      const feature = t.feature === undefined ? true : has(t.feature);
      let applies: boolean | undefined = detail === undefined && t.applies ? undefined : true;
      if (t.applies && detail !== undefined) {
        try {
          applies = t.applies(ctx);
        } catch (err) {
          warnFailed(`detail-tab:${t.id}`, err);
          applies = false;
        }
      }
      if (feature === false || applies === false) return false;
      return (feature === true && applies === true) || asked === t.id;
    });
  }, [all, has, installId, ref, detail, asked]);
}

/** Contributed sections for ref's kind. */
export function useSideSectionContributions(ref: Ref): SideSectionContribution[] {
  const all = useShellExtension().contributions.sideSections?.[kindKey(ref)] ?? NONE;
  return useMemo(() => unique(all, "side section"), [all]);
}

/** The contributed actions that apply to f; an applies() that throws counts as false. */
export function useFindingActions(f: Finding): FindingActionContribution[] {
  const all = useShellExtension().contributions.findingActions ?? NONE;
  const actions = useMemo(() => unique(all, "finding action"), [all]);
  return useMemo(
    () =>
      actions.filter((a) => {
        try {
          return a.applies(f);
        } catch (err) {
          warnFailed(`finding-action:${a.id}`, err);
          return false;
        }
      }),
    [actions, f],
  );
}

/** The contributed install-page panels. */
export function useInstallPanels(): InstallPanelContribution[] {
  const all = useShellExtension().contributions.installPanels ?? NONE;
  return useMemo(() => unique(all, "install panel"), [all]);
}
