import styled from "@emotion/styled";
import { NavLink } from "react-router-dom";
import { navCopy } from "../copy/explorer";
import { HEADER_HOVER_PCT, HEADER_PRESS_PCT, headerTint } from "./headerTint";
import { installPath } from "./paths";

// region Styles
const Nav = styled.nav`
  display: flex;
  flex: none;
  gap: ${(p) => p.theme.space(1)};
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
}

/** The install nav, in order. An entry shows only once its page's route exists. */
export const installNavEntries: InstallNavEntry[] = [
  { label: navCopy.overview, to: "" },
  { label: navCopy.gateways, to: "gateways" },
  { label: navCopy.routes, to: "routes" },
  { label: navCopy.listenerSets, to: "listenersets" },
  { label: navCopy.findings, to: "findings" },
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
  const shown = entries.filter((e) => paths.includes(e.to));
  return (
    <Nav aria-label={navCopy.label}>
      {shown.map((e) => (
        // The overview is the install's root, so it is active only on its own path.
        <NavLink key={e.to} to={installPath(installId, e.to)} end={e.to === ""}>
          {e.label}
        </NavLink>
      ))}
    </Nav>
  );
}
