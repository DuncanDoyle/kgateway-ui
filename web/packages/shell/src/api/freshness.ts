import type { InstallSummary } from "./types";

/**
 * The timestamp that bounds how old an install summary's health may be: confirmedAt when the
 * backend reports one, else computedAt (an older backend omits confirmedAt).
 */
export function confirmedOrComputed(s: Pick<InstallSummary, "confirmedAt" | "computedAt">): string {
  return s.confirmedAt ?? s.computedAt;
}
