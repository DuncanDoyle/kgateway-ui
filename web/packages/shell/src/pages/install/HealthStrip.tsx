import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import type { HealthReport } from "../../api/types";
import { HealthIcon } from "../../health/HealthIcon";
import { stateLabel } from "../../health/state";
import type { HealthState } from "../../theme/tokens";

// region Styles
/** Fixed strip height, shared with the loading skeleton so nothing shifts. */
export const STRIP_H = 44;

const Strip = styled.div<{ $state: HealthState; stylesOverride?: SerializedStyles }>`
  box-sizing: border-box;
  min-height: ${STRIP_H}px;
  display: flex;
  align-items: center;
  gap: ${(p) => p.theme.space(3)};
  padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.status[p.$state].stripBg};
  border: 1px solid ${(p) => p.theme.color.status[p.$state].stripBorder};
  color: ${(p) => p.theme.color.text};
  b {
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  .when {
    margin-left: auto;
    white-space: nowrap;
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
  }
  ${(p) => p.stylesOverride}
`;

// region Helpers
/** Relative time, e.g. "5s ago". */
export function ago(iso: string, now: number): string {
  const s = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.round(s / 60)}m ago`;
  return `${Math.round(s / 3600)}h ago`;
}

function plural(n: number, w: string) {
  return `${n} ${w}${n === 1 ? "" : "s"}`;
}

/** The strip's text. Unknown leads with "Health unknown" and its reason, then any warnings; it never claims "No known problems". */
function headline(h: HealthReport): string {
  if (h.state === "healthy") return "All healthy";
  if (h.state === "unknown") {
    const failing = h.checks.filter((c) => c.required && !c.ok);
    const missing = failing.filter((c) => !c.pending).map((c) => c.name);
    // A pending check was collected; its error says how many resources are being processed.
    const parts = failing.filter((c) => c.pending).map((c) => c.error ?? `${c.name} is being processed`);
    if (missing.length) parts.unshift(`${missing.join(", ")} could not be checked`);
    if (!parts.length) parts.push("some checks did not complete");
    const warnings = h.counts.warning ? ` · ${plural(h.counts.warning, "warning")}` : "";
    return `Health unknown — ${parts.join("; ")}${warnings}`;
  }
  const parts = [];
  if (h.counts.error) parts.push(plural(h.counts.error, "error"));
  if (h.counts.warning) parts.push(plural(h.counts.warning, "warning"));
  // Counts can be empty when findings are restricted or absent; never leave the strip blank.
  return parts.length ? parts.join(", ") : stateLabel(h.state);
}

// region Component
interface HealthStripProps {
  health: HealthReport;
  stale: boolean;
  now: number;
  /** When the page's data was collected, if older than health.collectedAt (e.g. a stale overview). */
  checkedAt?: string;
  stylesOverride?: SerializedStyles;
}

/** Full-width health strip; stale data is shown as unknown (D-006). InstallPage decides staleness for the whole page. */
export function HealthStrip({ health, stale, now, checkedAt, stylesOverride }: HealthStripProps) {
  const state: HealthState = stale ? "unknown" : health.state;
  const at = checkedAt ?? health.collectedAt;
  return (
    <Strip $state={state} data-testid="health-strip" stylesOverride={stylesOverride}>
      <HealthIcon state={state} size={18} />
      <b>{stale ? `Data is stale — last checked ${ago(at, now)}` : headline(health)}</b>
      {stale ? null : <span className="when">checked {ago(at, now)}</span>}
    </Strip>
  );
}
