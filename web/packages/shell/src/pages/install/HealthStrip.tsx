import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import type { CheckResult, Verdict } from "../../api/types";
import { diagnosticsCopy } from "../../copy/explorer";
import { countsText, plural } from "../../format";
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
/** A span of s seconds, e.g. "5s", "3h", "16d". */
function span(s: number): string {
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.round(s / 60)}m`;
  if (s < 86400) return `${Math.round(s / 3600)}h`;
  return `${Math.round(s / 86400)}d`;
}

/** Relative time, e.g. "5s ago", "3h ago", "16d ago". */
export function ago(iso: string, now: number): string {
  return `${span(Math.max(0, Math.round((now - Date.parse(iso)) / 1000)))} ago`;
}

/** Time until iso, e.g. "in 25s"; "now" once it has passed. */
export function inTime(iso: string, now: number): string {
  const s = Math.round((Date.parse(iso) - now) / 1000);
  return s > 0 ? `in ${span(s)}` : "now";
}

function sentence(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

/** The strip's text. Unknown leads with "Health unknown" and its reasons, then any warnings; it never claims "No known problems". */
function headline(v: Verdict, checks: CheckResult[]): string {
  if (v.state === "healthy") return "All healthy";
  if (v.state === "unknown") {
    const failing = checks.filter((c) => c.required && !c.ok);
    const missing = failing.filter((c) => !c.pending).map((c) => c.name);
    // A pending check was collected; its error says how many resources are being processed.
    const parts = failing.filter((c) => c.pending).map((c) => sentence(c.error ?? `Still reading ${c.name}`));
    if (missing.length) parts.unshift(`Cannot check ${missing.join(", ")}`);
    if (!parts.length) parts.push("Some checks did not finish");
    if (v.counts.warning) parts.push(plural(v.counts.warning, "warning"));
    return `Health unknown: ${parts.join(". ")}`;
  }
  // Counts can be empty when findings are restricted or absent; never leave the strip blank.
  const counts = countsText(v.counts);
  if (!counts) return stateLabel(v.state);
  return v.problems ? `${diagnosticsCopy.problems(v.problems)}: ${counts}` : counts;
}

// region Component
interface HealthStripProps {
  /** The server's verdict over every finding (design §6); the strip never derives a state. */
  verdict: Verdict;
  checks: CheckResult[];
  collectedAt: string;
  stale: boolean;
  now: number;
  /** When the page's data was collected, if older than collectedAt (e.g. a stale overview). */
  checkedAt?: string;
  stylesOverride?: SerializedStyles;
}

/** Full-width health strip; stale data is shown as unknown (D-006). InstallPage decides staleness for the whole page. */
export function HealthStrip({ verdict, checks, collectedAt, stale, now, checkedAt, stylesOverride }: HealthStripProps) {
  const state: HealthState = stale ? "unknown" : verdict.state;
  const at = checkedAt ?? collectedAt;
  const oldest = !stale && verdict.oldestSince && verdict.problems ? verdict.oldestSince : undefined;
  return (
    <Strip $state={state} data-testid="health-strip" stylesOverride={stylesOverride}>
      <HealthIcon state={state} size={18} />
      {/* One live region, updated in place; the ages tick every second and stay outside it. */}
      <b>
        <span role="status">{stale ? "Data is stale" : headline(verdict, checks)}</span>
        {stale ? `. Last checked ${ago(at, now)}` : null}
      </b>
      {stale ? null : (
        <span className="when">
          {oldest ? `${diagnosticsCopy.oldestProblem(ago(oldest, now))} · ` : null}Last checked {ago(at, now)}
        </span>
      )}
    </Strip>
  );
}
