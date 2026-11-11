import type { ReactNode } from "react";
import { useNow } from "../../api/clock";
import { isStale } from "../../api/hooks";
import { StaleView } from "../../health/staleView";

// region Component
/** Provides the stale view from the app clock; only this re-renders on a tick, not the page or its table. */
export function StaleGate({
  collectedAt,
  failed,
  children,
}: {
  collectedAt?: string;
  failed: boolean;
  children: ReactNode;
}) {
  const now = useNow();
  const stale = !!collectedAt && (failed || isStale(collectedAt, now));
  return <StaleView.Provider value={stale}>{children}</StaleView.Provider>;
}
