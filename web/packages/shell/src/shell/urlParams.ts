import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { useLocation, useNavigate, useNavigationType } from "react-router-dom";

// region Helpers
/** Changes to apply to the query string; "" or null removes the key. */
export type ParamChanges = Record<string, string | null>;

// Marks a navigation as this hook's own write, in location.state.
const WRITER = "urlParamsWriter";

function writer(state: unknown): unknown {
  return typeof state === "object" && state !== null ? (state as Record<string, unknown>)[WRITER] : undefined;
}

// region Component
/**
 * Query-string state that never loses an update. Local state is the source of truth, updated synchronously by
 * handlers and mirrored to the URL (replace) after commit. A committed navigation the hook did not write
 * (back/forward, a link) is adopted; its own writes, which may commit late, never overwrite newer local state.
 * One instance per page: two instances would each mirror their own copy of the whole query string.
 */
export function useUrlParams(): [URLSearchParams, (changes: ParamChanges) => void] {
  const location = useLocation();
  const navigate = useNavigate();
  const navType = useNavigationType();
  const id = useId();
  const [params, setParams] = useState(() => new URLSearchParams(location.search));
  // The last query string written or adopted, so each change reaches the URL once.
  const written = useRef(params.toString());
  // Adopt only committed navigations, before paint. Back/forward may return to an entry this hook wrote, so a POP is outside.
  useLayoutEffect(() => {
    if (navType !== "POP" && writer(location.state) === id) return;
    const url = new URLSearchParams(location.search);
    written.current = url.toString();
    setParams((prev) => (prev.toString() === written.current ? prev : url));
  }, [location, navType, id]);
  // Mirrors local state to the URL; it never reads the location, so a late commit of an older write changes nothing.
  useEffect(() => {
    const s = params.toString();
    if (s === written.current) return;
    written.current = s;
    navigate({ search: s ? `?${s}` : "" }, { replace: true, state: { [WRITER]: id } });
  }, [params, navigate, id]);
  const update = useCallback((changes: ParamChanges) => {
    setParams((prev) => {
      const next = new URLSearchParams(prev);
      for (const [k, v] of Object.entries(changes)) {
        if (v) next.set(k, v);
        else next.delete(k);
      }
      // Nothing changed: keep the same object, so nothing re-renders or navigates.
      return next.toString() === prev.toString() ? prev : next;
    });
  }, []);
  return [params, update];
}

/** A text filter mirrored to ?key=. The text is part of the synchronous local state, so no character is lost. */
export function useUrlText(
  params: URLSearchParams,
  update: (changes: ParamChanges) => void,
  key: string,
): [string, (v: string) => void] {
  const set = useCallback((v: string) => update({ [key]: v }), [update, key]);
  return [params.get(key) ?? "", set];
}
