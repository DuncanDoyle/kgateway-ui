import type { Severity } from "./api/types";

/** "1 error", "2 errors". */
export function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}

/** "{n} errors, {m} warnings"; empty when there are neither. */
export function countsText(c: Partial<Record<Severity, number>>): string {
  const parts = [];
  if (c.error) parts.push(plural(c.error, "error"));
  if (c.warning) parts.push(plural(c.warning, "warning"));
  return parts.join(", ");
}

/** The version label may or may not carry a leading "v"; show exactly one. */
export function versionText(v: string): string {
  return `v${v.replace(/^v/, "")}`;
}
