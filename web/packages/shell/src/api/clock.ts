import { useSyncExternalStore } from "react";

// region Helpers
// One interval for the whole app, started by the first subscriber and stopped by the last.
const listeners = new Set<() => void>();
let timer: ReturnType<typeof setInterval> | undefined;

function subscribe(onTick: () => void): () => void {
  listeners.add(onTick);
  timer ??= setInterval(() => {
    for (const l of listeners) l();
  }, 1000);
  return () => {
    listeners.delete(onTick);
    if (listeners.size === 0 && timer !== undefined) {
      clearInterval(timer);
      timer = undefined;
    }
  };
}

// Whole seconds, so the snapshot is stable between ticks (useSyncExternalStore requires it).
function snapshot(): number {
  return Math.floor(Date.now() / 1000) * 1000;
}

// region Component
/** The current time at 1 s resolution. Renders with it re-run every second, so staleness is re-evaluated even when data is unchanged. */
export function useNow(): number {
  return useSyncExternalStore(subscribe, snapshot, snapshot);
}
