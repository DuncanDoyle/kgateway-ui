import { useCallback, useLayoutEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";

// region Helpers
/** Changes to apply to the query string; "" or null removes the key. */
export type ParamChanges = Record<string, string | null>;

// region Component
/**
 * Query-string state that never loses an update. The router applies URL changes in a transition, so
 * setSearchParams(fn) sees the last committed params; this builds each update from the latest params written.
 */
export function useUrlParams(): [URLSearchParams, (changes: ParamChanges) => void] {
  const [committed, setSearchParams] = useSearchParams();
  const latest = useRef(committed);
  const reconciled = useRef(committed);
  const pending = useRef<string[]>([]);
  // Render stays pure (a transition render may be discarded): our writes catching up keep latest; any other URL wins.
  const ours = committed === reconciled.current || pending.current.includes(committed.toString());
  const current = ours ? latest.current : committed;
  useLayoutEffect(() => {
    if (committed === reconciled.current) return;
    reconciled.current = committed;
    const i = pending.current.lastIndexOf(committed.toString());
    if (i >= 0) pending.current = pending.current.slice(i + 1);
    else {
      pending.current = [];
      latest.current = committed;
    }
  }, [committed]);
  const update = useCallback(
    (changes: ParamChanges) => {
      const next = new URLSearchParams(latest.current);
      for (const [k, v] of Object.entries(changes)) {
        if (v) next.set(k, v);
        else next.delete(k);
      }
      // Nothing changed: no navigation, so no pending entry that would never be committed.
      if (next.toString() === latest.current.toString()) return;
      latest.current = next;
      pending.current.push(next.toString());
      setSearchParams(next, { replace: true });
    },
    [setSearchParams],
  );
  return [current, update];
}

/** A text filter mirrored to ?key=: the input updates synchronously, so no typed character is lost. */
export function useUrlText(
  params: URLSearchParams,
  update: (changes: ParamChanges) => void,
  key: string,
): [string, (v: string) => void] {
  const fromUrl = params.get(key) ?? "";
  const [text, setText] = useState(fromUrl);
  const [prevUrl, setPrevUrl] = useState(fromUrl);
  // Follow outside navigations during render (no stale frame); our own writes already match.
  if (fromUrl !== prevUrl) {
    setPrevUrl(fromUrl);
    setText(fromUrl);
  }
  const set = useCallback(
    (v: string) => {
      setText(v);
      update({ [key]: v });
    },
    [update, key],
  );
  return [text, set];
}
