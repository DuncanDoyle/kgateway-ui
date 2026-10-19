import type { HealthState } from "../theme/tokens";

/** Display text per state. */
export function stateLabel(s: HealthState): string {
  return { healthy: "Healthy", warning: "Warning", error: "Error", unknown: "Unknown" }[s];
}

/** Maps a finding severity to the state used to render it. Info renders as unknown-coloured. */
export function severityToState(sev: "error" | "warning" | "info"): HealthState {
  return sev === "error" ? "error" : sev === "warning" ? "warning" : "unknown";
}
