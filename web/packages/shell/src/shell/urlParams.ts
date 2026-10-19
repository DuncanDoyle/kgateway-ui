import { useCallback, useEffect, useRef, useState } from "react";
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
  const seen = useRef(committed);
  const pending = useRef<string[]>([]);
  // A newly committed URL is either one of ours catching up, or an outside navigation (link, back) that wins.
  if (seen.current !== committed) {
    seen.current = committed;
    const i = pending.current.indexOf(committed.toString());
    if (i >= 0) pending.current = pending.current.slice(i + 1);
    else {
      pending.current = [];
      latest.current = committed;
    }
  }
  const update = useCallback(
    (changes: ParamChanges) => {
      const next = new URLSearchParams(latest.current);
      for (const [k, v] of Object.entries(changes)) {
        if (v) next.set(k, v);
        else next.delete(k);
      }
      latest.current = next;
      pending.current.push(next.toString());
      setSearchParams(next, { replace: true });
    },
    [setSearchParams],
  );
  return [latest.current, update];
}

/** A text filter mirrored to ?key=: the input updates synchronously, so no typed character is lost. */
export function useUrlText(
  params: URLSearchParams,
  update: (changes: ParamChanges) => void,
  key: string,
): [string, (v: string) => void] {
  const fromUrl = params.get(key) ?? "";
  const [text, setText] = useState(fromUrl);
  // Follow outside navigations; our own writes already match.
  useEffect(() => setText(fromUrl), [fromUrl]);
  const set = useCallback(
    (v: string) => {
      setText(v);
      update({ [key]: v });
    },
    [update, key],
  );
  return [text, set];
}
