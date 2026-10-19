import { useEffect } from "react";
import { matchPath, useLocation } from "react-router-dom";
import { useShellExtension } from "./extension";

// region Helpers
const CORE_INSTALL = "/i/:ns/:name";

/** The page part of the title for pathname, or undefined for the bare product name. */
function pageTitle(
  pathname: string,
  ext: Pick<ReturnType<typeof useShellExtension>, "routes" | "navEntries">,
): string | undefined {
  if (pathname === "/") return undefined;
  const r = matchPath(`${CORE_INSTALL}/r/:group/:kind/:rns/:rname`, pathname);
  if (r) {
    const { ns, name, kind, rns, rname } = r.params;
    return `${kind} ${rns === "-" ? "" : `${rns}/`}${rname}: ${ns}/${name}`;
  }
  const f = matchPath(`${CORE_INSTALL}/findings`, pathname);
  if (f) return `Findings: ${f.params.ns}/${f.params.name}`;
  const i = matchPath(CORE_INSTALL, pathname);
  if (i) return `${i.params.ns}/${i.params.name}`;
  // An extension page takes the label of the nav entry that links to it.
  for (const c of ext.routes) {
    const m = matchPath(c.scope === "install" ? `${CORE_INSTALL}/${c.path}` : `/${c.path}`, pathname);
    if (!m) continue;
    const label = ext.navEntries.find((n) => n.scope === c.scope && n.to === c.path)?.label;
    if (c.scope === "install")
      return label ? `${label}: ${m.params.ns}/${m.params.name}` : `${m.params.ns}/${m.params.name}`;
    return label;
  }
  return "Page not found";
}

// region Component
/** Keeps document.title in step with the route: "{page} | {product}". */
export function DocumentTitle() {
  const { pathname } = useLocation();
  const { brand, routes, navEntries } = useShellExtension();
  const page = pageTitle(pathname, { routes, navEntries });
  const title = page ? `${page} | ${brand.productName}` : brand.productName;
  useEffect(() => {
    document.title = title;
  }, [title]);
  return null;
}
