import styled from "@emotion/styled";
import { useCallback, useEffect, useRef } from "react";
import { NavLink, useLocation } from "react-router-dom";
import { useFeatureCheck } from "../api/hooks";
import { navCopy } from "../copy/explorer";
import { HEADER_HOVER_PCT, HEADER_PRESS_PCT, headerTint } from "./headerTint";
import { installPath } from "./paths";

// region Styles
/** Width of the edge fade, px. */
const FADE = 24;

const Nav = styled.nav`
  display: flex;
  flex: none;
  gap: ${(p) => p.theme.space(1)};
  /* Narrow windows scroll the nav sideways rather than let it run over the install label. */
  @media (max-width: 899px) {
    flex: 0 1 auto;
    min-width: 0;
    overflow-x: auto;
    scrollbar-width: thin;
  }
  /* A mask fades the side with entries off-screen; it works on any header colour. */
  &[data-fade="end"] { mask-image: linear-gradient(to right, #000 calc(100% - ${FADE}px), transparent); }
  &[data-fade="start"] { mask-image: linear-gradient(to left, #000 calc(100% - ${FADE}px), transparent); }
  &[data-fade="both"] {
    mask-image: linear-gradient(to right, transparent, #000 ${FADE}px, #000 calc(100% - ${FADE}px), transparent);
  }
  a {
    display: inline-flex;
    align-items: center;
    padding: 6px 10px;
    border-radius: ${(p) => p.theme.radius.sm};
    color: ${(p) => p.theme.color.headerText};
    text-decoration: none;
    white-space: nowrap;
    @media (max-width: 999px) {
      padding: 6px 7px;
    }
    @media (max-width: 799px) {
      padding: 6px 4px;
    }
  }
  a:hover { background: ${(p) => headerTint(p.theme, HEADER_HOVER_PCT)}; }
  a:active { background: ${(p) => headerTint(p.theme, HEADER_PRESS_PCT)}; }
  a:focus-visible { outline: 2px solid ${(p) => p.theme.color.focus}; outline-offset: -2px; }
  a[aria-current="page"] {
    font-weight: ${(p) => p.theme.font.weight.semibold};
    box-shadow: inset 0 -2px 0 ${(p) => p.theme.color.headerText};
  }
`;

// region Helpers
/** An install page in the header nav; to is its route path under /i/:id/, "" for the install's overview. */
export interface InstallNavEntry {
  label: string;
  to: string;
  /** Shown only where the install has this feature (InstallSummary.features). */
  feature?: string;
}

/** The install nav, in order. An entry shows only once its page's route exists. */
export const installNavEntries: InstallNavEntry[] = [
  { label: navCopy.overview, to: "" },
  { label: navCopy.gateways, to: "gateways" },
  { label: navCopy.routes, to: "routes" },
  { label: navCopy.listenerSets, to: "listenersets" },
  { label: navCopy.findings, to: "findings" },
  { label: navCopy.processes, to: "processes", feature: "processes" },
];

// region Component
interface InstallNavProps {
  installId: string;
  /** The core install route paths, as Shell registers them. */
  paths: readonly string[];
  entries?: InstallNavEntry[];
}

/** The install-scoped header nav; aria-current marks the active page. */
export function InstallNav({ installId, paths, entries = installNavEntries }: InstallNavProps) {
  const has = useFeatureCheck(installId);
  const shown = entries.filter((e) => paths.includes(e.to) && (!e.feature || has(e.feature) !== false));
  const ref = useRef<HTMLElement>(null);
  const { pathname } = useLocation();
  // Marks which edges hide entries; set on the element, so scrolling never re-renders the nav.
  const fade = useCallback(() => {
    const el = ref.current;
    if (!el) return;
    const start = el.scrollLeft > 1;
    const end = el.scrollLeft + el.clientWidth < el.scrollWidth - 1;
    const v = start && end ? "both" : start ? "start" : end ? "end" : "";
    if (v) el.setAttribute("data-fade", v);
    else el.removeAttribute("data-fade");
  }, []);
  // biome-ignore lint/correctness/useExhaustiveDependencies: pathname is the trigger; the active link changes with it.
  useEffect(() => {
    const el = ref.current;
    el?.querySelector('[aria-current="page"]')?.scrollIntoView?.({ inline: "nearest", block: "nearest" });
    fade();
  }, [pathname, fade]);
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(fade);
    ro.observe(el);
    return () => ro.disconnect();
  }, [fade]);
  return (
    <Nav aria-label={navCopy.label} ref={ref} onScroll={fade}>
      {shown.map((e) => (
        // The overview is the install's root, so it is active only on its own path.
        <NavLink key={e.to} to={installPath(installId, e.to)} end={e.to === ""}>
          {e.label}
        </NavLink>
      ))}
    </Nav>
  );
}
