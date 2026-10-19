import type { LabelSelector, ScopeSummary } from "../api/types";

// region Helpers
/** The backend's source value for a scope read from the Deployment's pod template, not a running Pod. */
export const DEPLOYMENT_TEMPLATE = "deployment template";

function sourceLabel(source: string): string {
  return source === DEPLOYMENT_TEMPLATE ? "Deployment template" : source;
}

function selectorText(s: LabelSelector): string {
  const labels = Object.entries(s.matchLabels ?? {}).map(([k, v]) => `${k}=${v}`);
  const exprs = (s.matchExpressions ?? []).map((e) =>
    e.values?.length ? `${e.key} ${e.operator} (${e.values.join(", ")})` : `${e.key} ${e.operator}`,
  );
  return [...labels, ...exprs].join(", ") || "any label";
}

/**
 * The scope of an install in words, shared by the landing card ("summary") and the scope tile's tooltip ("detail").
 * Both name a scope error; the summary names only the Deployment-template source, the detail names any source.
 */
export function scopeText(sc: ScopeSummary, form: "summary" | "detail"): string {
  if (sc.mode === "unknown") return sc.error ? `Scope unknown: ${sc.error}` : "Scope unknown";
  if (form === "summary") {
    const which = sc.mode === "all" ? "All namespaces" : `${sc.watched} of ${sc.total} namespaces`;
    return sc.source === DEPLOYMENT_TEMPLATE ? `${which}. Source: ${sourceLabel(sc.source)}` : which;
  }
  const which =
    sc.mode === "all" ? "All namespaces" : `Namespaces matching ${(sc.selectors ?? []).map(selectorText).join(" or ")}`;
  return sc.source ? `${which}. Source: ${sourceLabel(sc.source)}.` : which;
}
