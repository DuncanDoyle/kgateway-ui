import { useCallback, useEffect, useMemo, useState } from "react";
import type { ApiError } from "../api/client";
import type { Page } from "../api/types";
import { useInstallId } from "../shell/extension";
import { type ParamChanges, useUrlParams } from "../shell/urlParams";

// region Helpers
export type ListName = "routes" | "gateways" | "listenersets" | "findings" | "processes" | "paths";

/** The params each list's server documents (design §7.2), in the order they are sent. listener is 2.3 and never sent. */
const API_PARAMS: Record<ListName, string[]> = {
  routes: ["namespace", "kind", "parent", "attachment", "state", "health", "backend", "q", "sort", "cursor"],
  gateways: ["namespace", "health", "q", "sort", "cursor"],
  listenersets: ["namespace", "parent", "health", "q", "sort", "cursor"],
  findings: ["severity", "code", "kind", "namespace", "group", "q", "sort", "cursor"],
  processes: ["health", "q", "skew", "restarts", "cursor"],
  paths: ["gateway", "listener", "route", "backend", "namespace", "state", "kind", "mirror", "q", "sort", "cursor"],
};
/** The server's default sort per list, as the URL writes it. */
const DEFAULT_SORT: Record<ListName, string> = {
  routes: "name.asc",
  gateways: "name.asc",
  listenersets: "name.asc",
  findings: "rank.asc",
  processes: "name.asc",
  paths: "problem.asc",
};
/** Repeatable params; the page URL holds them comma-joined (no value of theirs can contain a comma). */
const REPEATED = new Set(["namespace", "kind", "state", "health", "backend", "severity", "code", "gateway"]);
/** Params a list takes once although other lists repeat them; only the first URL value is sent. */
const SINGLE: Partial<Record<ListName, Set<string>>> = { paths: new Set(["namespace", "kind"]) };
/** Params that filter rows; Clear filters removes these and keeps the sort. */
export const FILTER_PARAMS = [
  "namespace",
  "kind",
  "gateway",
  "listener",
  "route",
  "mirror",
  "parent",
  "attachment",
  "state",
  "health",
  "backend",
  "severity",
  "code",
  "group",
  "skew",
  "restarts",
  "q",
];

const Q_DEBOUNCE_MS = 250;

/** Values of a comma-joined page URL param. */
export function listValues(params: URLSearchParams, key: string): string[] {
  return params.get(key)?.split(",").filter(Boolean) ?? [];
}

/** The API query for list from the page URL: only documented params, repeatable ones expanded; skip is never sent. */
export function apiQuery(list: ListName, params: URLSearchParams, q: string, skip: string[] = []): URLSearchParams {
  const out = new URLSearchParams();
  for (const key of API_PARAMS[list]) {
    if (skip.includes(key)) continue;
    if (key === "q") {
      if (q) out.set("q", q);
      continue;
    }
    // attachment without parent is a 400.
    if (key === "attachment" && !params.get("parent")) continue;
    if (SINGLE[list]?.has(key)) {
      const v = listValues(params, key)[0];
      if (v) out.set(key, v);
    } else if (REPEATED.has(key)) for (const v of listValues(params, key)) out.append(key, v);
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

/** A sort as the URL holds it: key.asc or key.desc; the list's DEFAULT_SORT when absent. */
export interface SortState {
  key: string;
  desc: boolean;
}

// region Component
type ListHook<P> = (
  id: string,
  query: URLSearchParams,
) => { data?: P; error?: ApiError; cursorRejected: boolean; isLaggy: boolean };

/**
 * URL state and data of one list page: filters, sort and cursor live in the query string (design §8.1).
 * skip names params the server does not advertise (/meta filters); they stay in the URL but are never sent.
 */
export function useListPage<T, P extends { install: string } = Page<T>>(
  list: ListName,
  useList: ListHook<P>,
  skip: string[] = [],
) {
  const id = useInstallId();
  const [params, update] = useUrlParams();
  const text = params.get("q") ?? "";
  const q = useDebounced(text, Q_DEBOUNCE_MS);
  // The debounced value starts at the URL's q, so a reload does not wait.
  const skipKey = skip.join(",");
  const query = useMemo(() => apiQuery(list, params, q, skipKey.split(",")), [list, params, q, skipKey]);
  const { data, error, cursorRejected, isLaggy } = useList(id, query);
  // A rejected cursor is dropped (replace) and page one shows (design §8.6).
  useEffect(() => {
    if (cursorRejected) update({ cursor: null });
  }, [cursorRejected, update]);
  // Any filter change starts again at page one: a cursor belongs to one query.
  const setFilter = useCallback((changes: ParamChanges) => update({ ...changes, cursor: null }), [update]);
  const [key = "name", dir] = (params.get("sort") ?? DEFAULT_SORT[list]).split(".");
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
