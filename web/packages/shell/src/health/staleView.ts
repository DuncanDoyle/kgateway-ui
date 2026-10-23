import { createContext, useContext } from "react";
import type { HealthState } from "../theme/tokens";

/** True under a detail page whose data is stale or whose last poll failed: every state shows unknown (design §6.3). */
export const StaleView = createContext(false);

/** The state to show: unknown inside a stale view, else as given. */
export function useShownState(state: HealthState): HealthState {
  return useContext(StaleView) ? "unknown" : state;
}
