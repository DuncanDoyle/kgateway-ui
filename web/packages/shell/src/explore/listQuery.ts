import { useCallback, useEffect, useMemo, useState } from "react";
import type { ApiError } from "../api/client";
import type { Page } from "../api/types";
import { useInstallId } from "../shell/extension";
import { type ParamChanges, useUrlParams } from "../shell/urlParams";

// region Helpers
export type ListName = "routes" | "gateways" | "listenersets";

/** The params each list's server documents (design §7.2), in the order they are sent. listener is 2.3 and never sent. */
const API_PARAMS: Record<ListName, string[]> = {
  routes: ["namespace", "kind", "parent", "attachment", "state", "health", "backend", "q", "sort", "cursor"],
  gateways: ["namespace", "health", "q", "sort", "cursor"],
  listenersets: ["namespace", "parent", "health", "q", "sort", "cursor"],
};
/** Repeatable params; the page URL holds them comma-joined (no value of theirs can contain a comma). */
const REPEATED = new Set(["namespace", "kind", "state", "health", "backend"]);
/** Params that filter rows; Clear filters removes these and keeps the sort. */
export const FILTER_PARAMS = ["namespace", "kind", "parent", "attachment", "state", "health", "backend", "q"];

const Q_DEBOUNCE_MS = 250;

/** Values of a comma-joined page URL param. */
export function listValues(params: URLSearchParams, key: string): string[] {
  return params.get(key)?.split(",").filter(Boolean) ?? [];
}

/** The API query for list from the page URL: only documented params, repeatable ones expanded. */
export function apiQuery(list: ListName, params: URLSearchParams, q: string): URLSearchParams {
  const out = new URLSearchParams();
  for (const key of API_PARAMS[list]) {
    if (key === "q") {
      if (q) out.set("q", q);
      continue;
    }
    // attachment without parent is a 400.
    if (key === "attachment" && !params.get("parent")) continue;
    if (REPEATED.has(key)) for (const v of listValues(params, key)) out.append(key, v);
    else if (params.get(key)) out.set(key, params.get(key) as string);
  }
  return out;
}

/** v, once it has stopped changing for ms. */
function useDebounced(v: string, ms: number): string {
  const [d, setD] = useState(v);
  useEffect(() => {
    const t = setTimeout(() => setD(v), ms);
    return () => clearTimeout(t);
  }, [v, ms]);
  return d;
}

/** A sort as the URL holds it: key.asc or key.desc; name ascending by default. */
export interface SortState {
  key: string;
  desc: boolean;
}

// region Component
type ListHook<T> = (
  id: string,
  query: URLSearchParams,
) => { data?: Page<T>; error?: ApiError; cursorRejected: boolean; isLaggy: boolean };

/** URL state and data of one list page: filters, sort and cursor live in the query string (design §8.1). */
export function useListPage<T>(list: ListName, useList: ListHook<T>) {
  const id = useInstallId();
  const [params, update] = useUrlParams();
  const text = params.get("q") ?? "";
  const q = useDebounced(text, Q_DEBOUNCE_MS);
  // The debounced value starts at the URL's q, so a reload does not wait.
  const query = useMemo(() => apiQuery(list, params, q), [list, params, q]);
  const { data, error, cursorRejected, isLaggy } = useList(id, query);
  // A rejected cursor is dropped (replace) and page one shows (design §8.6).
  useEffect(() => {
    if (cursorRejected) update({ cursor: null });
  }, [cursorRejected, update]);
  // Any filter change starts again at page one: a cursor belongs to one query.
  const setFilter = useCallback((changes: ParamChanges) => update({ ...changes, cursor: null }), [update]);
  const [key = "name", dir] = (params.get("sort") ?? "name.asc").split(".");
  const sort: SortState = { key, desc: dir === "desc" };
  const filtered = FILTER_PARAMS.some((k) => params.get(k));
  const clearFilters = useCallback(
    () => setFilter(Object.fromEntries(FILTER_PARAMS.map((k) => [k, null]))),
    [setFilter],
  );
  return {
    id,
    params,
    text,
    setText: (v: string) => setFilter({ q: v }),
    setFilter,
    sort,
    setSort: (k: string) => setFilter({ sort: `${k}.${sort.key === k && !sort.desc ? "desc" : "asc"}` }),
    setCursor: (c: string) => update({ cursor: c }),
    filtered,
    clearFilters,
    data,
    /** The rows are the previous query's while this one loads. */
    busy: isLaggy,
    // Under a rejected cursor this is page one's error.
    error,
  };
}
