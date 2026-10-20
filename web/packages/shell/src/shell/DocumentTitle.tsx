import { useEffect } from "react";
import { matchPath, useLocation } from "react-router-dom";
import { useInstalls } from "../api/hooks";
import { useShellExtension } from "./extension";
import { installLabel } from "./InstallName";
import { decodeInstallId } from "./paths";

// region Helpers
const CORE_INSTALL = "/i/:id";

/** The page part of the title for pathname, or undefined for the bare product name; label names an install by its ID. */
function pageTitle(
  pathname: string,
  ext: Pick<ReturnType<typeof useShellExtension>, "routes" | "navEntries">,
  label: (id: string) => string,
): string | undefined {
  if (pathname === "/") return undefined;
  // matchPath on the raw pathname leaves the ID encoded, so decode it once here.
  const install = (p: { id?: string }) => label(decodeInstallId(p.id ?? ""));
  const r = matchPath(`${CORE_INSTALL}/r/:group/:kind/:rns/:rname`, pathname);
  if (r) {
    const { kind, rns, rname } = r.params;
    return `${kind} ${rns === "-" ? "" : `${rns}/`}${rname}: ${install(r.params)}`;
  }
  const f = matchPath(`${CORE_INSTALL}/findings`, pathname);
  if (f) return `Findings: ${install(f.params)}`;
  const i = matchPath(CORE_INSTALL, pathname);
  if (i) return install(i.params);
  // An extension page takes the label of the nav entry that links to it.
  for (const c of ext.routes) {
    const m = matchPath(c.scope === "install" ? `${CORE_INSTALL}/${c.path}` : `/${c.path}`, pathname);
    if (!m) continue;
    const nav = ext.navEntries.find((n) => n.scope === c.scope && n.to === c.path)?.label;
    if (c.scope === "install") return nav ? `${nav}: ${install(m.params)}` : install(m.params);
    return nav;
  }
  return "Page not found";
}

// region Component
/** Keeps document.title in step with the route: the page part (e.g. "Findings: <install>"), else the product name. */
export function DocumentTitle() {
  const { pathname } = useLocation();
  const { brand, routes, navEntries } = useShellExtension();
  const { data } = useInstalls();
  const label = (id: string) => {
    const i = data?.find((x) => x.id === id);
    return i ? installLabel(i) : id;
  };
  const page = pageTitle(pathname, { routes, navEntries }, label);
  const title = page ?? brand.productName;
  useEffect(() => {
    document.title = title;
  }, [title]);
  return null;
}
