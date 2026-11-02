import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Link, useLocation } from "react-router-dom";
import { useInstalls } from "../api/hooks";
import type { LinkedRef } from "../api/types";
import { objectPath } from "../explore/objectPath";
import { installLabel } from "./InstallName";
import { Anchor, Menu, useMenu } from "./menu";
import { installPath } from "./paths";
import {
  appendVia,
  breadcrumbsCopy,
  type Crumb,
  collapse,
  crumbLabel,
  crumbPagePath,
  crumbRef,
  kindList,
  pageOf,
  parseVia,
  viaParam,
} from "./trail";

// region Styles
/** Height of the trail, fixed from the first render whatever it holds. */
export const TRAIL_H = 20;

const Nav = styled.nav<{ stylesOverride?: SerializedStyles }>`
  height: ${TRAIL_H}px;
  margin-bottom: ${(p) => p.theme.space(1)};
  font-size: ${(p) => p.theme.font.size.sm};
  line-height: ${TRAIL_H}px;
  color: ${(p) => p.theme.color.textMuted};
  ol {
    display: flex;
    align-items: center;
    gap: ${(p) => p.theme.space(1)};
    min-width: 0;
    height: 100%;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    display: flex;
    align-items: center;
    gap: ${(p) => p.theme.space(1)};
    flex: 0 1 auto;
    min-width: 3ch;
  }
  li[data-part="more"],
  li[data-part="install"] {
    flex: none;
    min-width: 0;
  }
  /* The current page and the install keep their names; earlier crumbs shrink first. */
  li[data-part="current"] {
    flex: 0 0 auto;
    max-width: 40%;
  }
  [data-part="sep"] {
    flex: none;
  }
  [data-part="crumb"] {
    display: block;
    min-width: 0;
    max-width: 40ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  a[data-part="crumb"] {
    color: inherit;
    text-decoration: none;
    border-radius: ${(p) => p.theme.radius.sm};
  }
  a[data-part="crumb"]:hover {
    color: ${(p) => p.theme.color.text};
    text-decoration: underline;
  }
  a[data-part="crumb"]:active {
    color: ${(p) => p.theme.color.accentText};
  }
  a[data-part="crumb"]:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 1px;
  }
  [aria-current="page"] {
    color: ${(p) => p.theme.color.text};
  }
  ${(p) => p.stylesOverride}
`;
const MoreButton = styled.button`
  height: ${TRAIL_H}px;
  padding: 0 6px;
  cursor: pointer;
  font: inherit;
  line-height: 1;
  color: inherit;
  background: transparent;
  border: 1px solid transparent;
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    color: ${(p) => p.theme.color.text};
    background: color-mix(
      in srgb,
      ${(p) => p.theme.color.text} 8%,
      transparent
    );
    border-color: ${(p) => p.theme.color.borderStrong};
  }
  &:active,
  &[aria-expanded="true"] {
    color: ${(p) => p.theme.color.text};
    background: color-mix(
      in srgb,
      ${(p) => p.theme.color.text} 16%,
      transparent
    );
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 1px;
  }
`;
const TrailMenu = styled(Menu)`
  max-width: min(480px, calc(100vw - 32px));
  max-height: 320px;
  overflow-y: auto;
  line-height: 1.4;
  [role="menuitem"] {
    text-decoration: none;
    overflow-wrap: anywhere;
  }
`;

// region Helpers
// key is the crumb's place in the trail, which entries may repeat.
type Resolved = { to?: string; label: string; key?: string };

const sameRef = (a: LinkedRef, r: { group: string; kind: string; namespace?: string; name: string }) =>
  a.group === r.group && a.kind === r.kind && (a.namespace ?? "") === (r.namespace ?? "") && a.name === r.name;

/** Where crumb i of trail opens, with the trail before it, and what it shows. Never reads text from the URL into a link. */
function resolve(installId: string, trail: Crumb[], i: number, refs: readonly LinkedRef[]): Resolved {
  const c = trail[i] as Crumb;
  if (c.kind === "list") {
    // Re-serialized, so the list gets its filters back as query parameters and nothing else.
    const q = new URLSearchParams(c.search);
    q.delete("via");
    const s = q.toString();
    return {
      to: `${installPath(installId, c.path)}${s ? `?${s}` : ""}`,
      label: crumbLabel(c),
    };
  }
  const r = crumbRef(c);
  if (!r) return { label: crumbLabel(c) };
  // A ref the page holds decides the link, as LinkedRef does everywhere; a "none" link may mean hidden (D-013).
  const known = refs.find((k) => sameRef(k, r));
  if (known?.link === "none") return { label: breadcrumbsCopy.hidden };
  const base = known ? objectPath(installId, known) : installPath(installId, crumbPagePath(r));
  const via = viaParam(trail.slice(0, i), installId);
  return {
    to: via ? `${base}?${new URLSearchParams({ via })}` : base,
    label: crumbLabel(c),
  };
}

function CrumbItem({ r, current }: { r: Resolved; current?: boolean }) {
  if (current)
    return (
      <span data-part="crumb" aria-current="page" title={r.label}>
        {r.label}
      </span>
    );
  return r.to ? (
    <Link data-part="crumb" to={r.to} title={r.label}>
      {r.label}
    </Link>
  ) : (
    <span data-part="crumb" title={r.label}>
      {r.label}
    </span>
  );
}

function More({ items }: { items: Resolved[] }) {
  const menu = useMenu();
  return (
    <Anchor ref={menu.anchorRef}>
      <MoreButton
        ref={menu.buttonRef}
        type="button"
        aria-label={breadcrumbsCopy.more(items.length)}
        aria-haspopup="menu"
        aria-expanded={menu.open}
        onClick={menu.toggle}
      >
        …
      </MoreButton>
      {menu.open ? (
        <TrailMenu
          role="menu"
          aria-label={breadcrumbsCopy.menu}
          $align="start"
          ref={menu.menuRef}
          onKeyDown={menu.onMenuKeyDown}
        >
          {items.map((r) =>
            r.to ? (
              <Link key={r.key} role="menuitem" tabIndex={-1} to={r.to} onClick={() => menu.close(false)}>
                {r.label}
              </Link>
            ) : (
              <span key={r.key} role="menuitem" tabIndex={-1} aria-disabled="true">
                {r.label}
              </span>
            ),
          )}
        </TrailMenu>
      ) : null}
    </Anchor>
  );
}

const Sep = () => (
  <span data-part="sep" aria-hidden="true">
    ›
  </span>
);

// region Component
/** Props of {@link Breadcrumbs}. */
export interface BreadcrumbsProps {
  /** Refs the page's data holds; a crumb naming one follows its link, and a "none" link shows the restricted label. */
  refs?: readonly LinkedRef[];
  stylesOverride?: SerializedStyles;
}

/**
 * The path the user took to this object page, from the URL's `via` (D-035, design §17.6). Without `via` it shows
 * install › kind list › object. Renders nothing outside an install's object pages.
 */
export function Breadcrumbs({ refs = [], stylesOverride }: BreadcrumbsProps) {
  const { pathname, search } = useLocation();
  const { data: installs } = useInstalls();
  const page = pageOf(pathname, search);
  if (page?.crumb?.kind !== "object") return null;
  const { installId, crumb } = page;
  const full = appendVia(parseVia(search, installId), crumb);
  let earlier = full.slice(0, -1);
  const ref = crumbRef(crumb);
  if (earlier.length === 0) {
    const list = ref ? kindList(ref) : undefined;
    earlier = list ? [list] : [];
  }
  const { hidden } = collapse(earlier);
  const resolved = earlier.map((_, i) => ({ ...resolve(installId, earlier, i, refs), key: String(i) }));
  const install = installs?.find((x) => x.id === installId);
  return (
    <Nav aria-label={breadcrumbsCopy.label} data-trail="" stylesOverride={stylesOverride}>
      <ol>
        <li data-part="install">
          <CrumbItem
            r={{
              to: installPath(installId),
              label: install ? installLabel(install) : installId,
            }}
          />
        </li>
        {hidden.length ? (
          <li data-part="more">
            <Sep />
            <More items={resolved.slice(0, hidden.length)} />
          </li>
        ) : null}
        {resolved.slice(hidden.length).map((r) => (
          <li key={r.key}>
            <Sep />
            <CrumbItem r={r} />
          </li>
        ))}
        <li data-part="current">
          <Sep />
          <CrumbItem r={{ label: crumbLabel(crumb) }} current />
        </li>
      </ol>
    </Nav>
  );
}
