import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import {
  createContext,
  type KeyboardEvent,
  useCallback,
  useContext,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";
import { Link, useLocation } from "react-router-dom";
import { useTrailLinks } from "../api/exploreHooks";
import { useInstalls } from "../api/hooks";
import type { LinkedRef, Ref } from "../api/types";
import { breadcrumbsCopy } from "../copy/navigation";
import { objectPath } from "../explore/objectPath";
import { installLabel } from "./InstallName";
import { Anchor, Menu, useMenu } from "./menu";
import { installPath } from "./paths";
import {
  appendVia,
  type Crumb,
  collapse,
  crumbLabel,
  crumbRef,
  kindList,
  objectCrumb,
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
  /* Holds a crumb's place, at a fixed width, until the server says where it opens. */
  [data-part="crumb"][data-pending] {
    width: 16ch;
    height: 8px;
    border-radius: ${(p) => p.theme.radius.sm};
    background: color-mix(in srgb, ${(p) => p.theme.color.textMuted} 28%, transparent);
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
  [role="menuitem"][aria-disabled="true"] {
    cursor: default;
  }
  [role="menuitem"][aria-disabled="true"]:hover,
  [role="menuitem"][aria-disabled="true"]:active {
    background: transparent;
  }
`;

// region Helpers
// key is the crumb's place in the trail, which entries may repeat; pending waits for the server's link.
type Resolved = { to?: string; label: string; key?: string; pending?: boolean };

const sameRef = (a: LinkedRef, r: Ref) =>
  a.group === r.group && a.kind === r.kind && (a.namespace ?? "") === (r.namespace ?? "") && a.name === r.name;

/** The server's answers for a trail: links from GET …/refs, or failed when it gave none. */
interface Lookup {
  refs?: readonly LinkedRef[];
  failed: boolean;
}

/**
 * Where crumb i of trail opens, with the trail before it, and what it shows. An object crumb takes its link and name
 * from a server LinkedRef only (design §17.6): the page's own, else GET …/refs; a "none" link may mean hidden (D-013).
 */
function resolve(installId: string, trail: Crumb[], i: number, refs: readonly LinkedRef[], lookup: Lookup): Resolved {
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
  const known = refs.find((k) => sameRef(k, r)) ?? lookup.refs?.find((k) => sameRef(k, r));
  if (!known) return lookup.failed ? { label: breadcrumbsCopy.hidden } : { label: "", pending: true };
  const base = objectPath(installId, known);
  if (!base) return { label: breadcrumbsCopy.hidden };
  const via = viaParam(trail.slice(0, i), installId);
  return {
    to: via ? `${base}?${new URLSearchParams({ via })}` : base,
    label: crumbLabel(objectCrumb(known)),
  };
}

function CrumbItem({ r, current }: { r: Resolved; current?: boolean }) {
  if (r.pending) return <span data-part="crumb" data-pending="" aria-hidden="true" />;
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
          onKeyDown={(e: KeyboardEvent<HTMLDivElement>) => {
            // Space activates a link item as Enter does.
            if (e.key === " " && e.target instanceof HTMLAnchorElement) {
              e.preventDefault();
              e.target.click();
            } else menu.onMenuKeyDown(e);
          }}
        >
          {items.map((r) =>
            r.to ? (
              <Link key={r.key} role="menuitem" tabIndex={-1} to={r.to} onClick={() => menu.close(false)}>
                {r.label}
              </Link>
            ) : (
              <span key={r.key} role="menuitem" tabIndex={-1} aria-disabled="true">
                {r.pending ? <span data-part="crumb" data-pending="" aria-hidden="true" /> : r.label}
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

type Report = (key: string, refs: readonly LinkedRef[], sig: string) => void;
const TrailRefsContext = createContext<Report | undefined>(undefined);

/** Lets sub-tables inside an object page report the refs they show to the page's trail. */
export const TrailRefsProvider = TrailRefsContext.Provider;

/** Reports refs shown inside a {@link TrailRefsProvider}, e.g. a sub-table's rows; withdrawn on unmount. */
export function useReportTrailRefs(refs: readonly LinkedRef[] | undefined) {
  const report = useContext(TrailRefsContext);
  const key = useId();
  const latest = useRef(refs);
  latest.current = refs;
  const sig = (refs ?? []).map((r) => `${r.group}/${r.kind}/${r.namespace ?? ""}/${r.name}/${r.link}`).join("|");
  useEffect(() => report?.(key, latest.current ?? [], sig), [report, key, sig]);
  useEffect(() => () => report?.(key, [], ""), [report, key]);
}

/** Collects reported refs for an object page: own refs first, then what its sub-tables report. */
export function useTrailRefsCollector(own: readonly LinkedRef[]): [readonly LinkedRef[], Report] {
  const [reported, setReported] = useState<ReadonlyMap<string, { sig: string; refs: readonly LinkedRef[] }>>(
    () => new Map(),
  );
  const report = useCallback<Report>(
    (key, refs, sig) =>
      setReported((prev) => {
        if ((prev.get(key)?.sig ?? "") === sig) return prev;
        const next = new Map(prev);
        if (sig) next.set(key, { sig, refs });
        else next.delete(key);
        return next;
      }),
    [],
  );
  const all = useMemo(() => [...own, ...[...reported.values()].flatMap((v) => v.refs)], [own, reported]);
  return [all, report];
}

/** The object page at a URL and the crumbs before it: its `via`, or its kind's list without one. */
function trailAt(pathname: string, search: string) {
  const page = pageOf(pathname, search);
  if (page?.crumb?.kind !== "object") return undefined;
  const { installId, crumb } = page;
  let earlier = appendVia(parseVia(search, installId), crumb).slice(0, -1);
  if (earlier.length === 0) {
    const ref = crumbRef(crumb);
    const list = ref ? kindList(ref) : undefined;
    earlier = list ? [list] : [];
  }
  return { installId, crumb, earlier };
}

// region Component
/** Props of {@link Breadcrumbs}. */
export interface BreadcrumbsProps {
  /** Refs the page's data holds; a crumb naming one follows its link, and a "none" link shows the restricted label. */
  refs?: readonly LinkedRef[];
  /** The page's own object: pending until its detail loads, shown once the server returned it. */
  current?: CurrentState;
  stylesOverride?: SerializedStyles;
}

/** What the page knows of its own object, for the current crumb. */
export type CurrentState = "pending" | "shown";

/**
 * The path the user took to this object page, from the URL's `via` (D-035, design §17.6). Without `via` it shows
 * install › kind list › object. Renders nothing outside an install's object pages.
 */
export function Breadcrumbs({ refs = [], current = "pending", stylesOverride }: BreadcrumbsProps) {
  const { pathname, search } = useLocation();
  const { data: installs } = useInstalls();
  const t = trailAt(pathname, search);
  const objects = t?.earlier.flatMap((c) => crumbRef(c) ?? []) ?? [];
  // One lookup per trail, keyed by all its objects, so page refs arriving later never change the key.
  const lookup = useTrailLinks(
    t?.installId ?? "",
    objects.every((r) => refs.some((k) => sameRef(k, r))) ? [] : objects,
  );
  if (!t) return null;
  const { installId, crumb, earlier } = t;
  const { hidden } = collapse(earlier);
  const resolved = earlier.map((_, i) => ({
    ...resolve(installId, earlier, i, refs, { refs: lookup.data?.refs, failed: lookup.failed }),
    key: String(i),
  }));
  const install = installs?.find((x) => x.id === installId);
  return (
    <Nav
      aria-label={breadcrumbsCopy.label}
      aria-busy={current === "pending" || resolved.some((r) => r.pending) || undefined}
      data-trail=""
      data-testid="trail"
      stylesOverride={stylesOverride}
    >
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
          {/* The URL's name shows only once the page's own detail confirms the object (design §17.6). */}
          <CrumbItem r={current === "shown" ? { label: crumbLabel(crumb) } : { label: "", pending: true }} current />
        </li>
      </ol>
    </Nav>
  );
}
