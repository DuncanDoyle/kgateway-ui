import { useCallback, useSyncExternalStore } from "react";

// region Helpers
const mediaQuery = (q: string) => (typeof window.matchMedia === "function" ? window.matchMedia(q) : undefined);

// region Component
/** True while the viewport matches q; read synchronously, so the first render already has the right layout. */
export function useMedia(q: string): boolean {
  const subscribe = useCallback(
    (cb: () => void) => {
      const m = mediaQuery(q);
      m?.addEventListener?.("change", cb);
      return () => m?.removeEventListener?.("change", cb);
    },
    [q],
  );
  return useSyncExternalStore(
    subscribe,
    () => mediaQuery(q)?.matches ?? false,
    () => false,
  );
}
