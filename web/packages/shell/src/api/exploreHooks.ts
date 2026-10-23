import { useState } from "react";
import useSWR from "swr";
import type { ApiError } from "./client";
import { apiFetch } from "./client";
import { installApi, live, POLL_MS } from "./hooks";
import type {
  GatewayDetail,
  GatewayListRow,
  ListenerSetDetail,
  ListenerSetRow,
  Page,
  Ref,
  RouteDetail,
  RouteRow,
} from "./types";

// region Helpers
const seg = encodeURIComponent;
const withQuery = (path: string, q: URLSearchParams) => {
  const s = q.toString();
  return s ? `${path}?${s}` : path;
};

/** A response is shown only for the install in the URL, so a switch never shows the previous install's data. */
const forInstall = <T extends { install: string }>(data: T | undefined, id: string) =>
  data?.install === id ? data : undefined;

/**
 * One list page, polled. A 400 on a request with a cursor sets cursorRejected and loads page one instead (design §8.6);
 * the page should then drop the cursor from its URL.
 */
function useList<T>(id: string, list: string, query: URLSearchParams) {
  const path = `${installApi(id)}/${list}`;
  const hasCursor = query.has("cursor");
  const rejects = (e: ApiError | undefined) => hasCursor && e?.status === 400;
  // A rejected cursor is final: its key neither polls nor retries, so it never loops on a 400.
  const [rejected, setRejected] = useState<string>();
  const key = withQuery(path, query);
  const paged = useSWR<Page<T>, ApiError>(key, apiFetch, {
    ...live,
    // A filter or page change keeps the old rows until the new ones arrive; forInstall still hides another install's.
    keepPreviousData: true,
    refreshInterval: rejected === key ? 0 : POLL_MS,
    shouldRetryOnError: (e) => !rejects(e),
    onError: (e) => {
      if (rejects(e)) setRejected(key);
    },
  });
  const cursorRejected = rejects(paged.error);
  const pageOne = new URLSearchParams(query);
  pageOne.delete("cursor");
  const first = useSWR<Page<T>, ApiError>(cursorRejected ? withQuery(path, pageOne) : null, apiFetch, live);
  const r = cursorRejected ? first : paged;
  return { data: forInstall(r.data, id), error: r.error, mutate: r.mutate, cursorRejected };
}

function useDetail<T extends { install: string }>(id: string, path: string) {
  const { data, error, mutate } = useSWR<T, ApiError>(`${installApi(id)}/${path}`, apiFetch, live);
  return { data: forInstall(data, id), error, mutate };
}

const refPath = (r: Ref) => `${seg(r.group)}/${seg(r.kind)}/${seg(r.namespace ?? "")}/${seg(r.name)}`;

/** GET …/routes with the list's query string (filters, sort, cursor). */
export function useRoutes(id: string, query: URLSearchParams) {
  return useList<RouteRow>(id, "routes", query);
}

/** GET …/gateways. */
export function useGateways(id: string, query: URLSearchParams) {
  return useList<GatewayListRow>(id, "gateways", query);
}

/** GET …/listenersets. */
export function useListenerSets(id: string, query: URLSearchParams) {
  return useList<ListenerSetRow>(id, "listenersets", query);
}

/** GET …/gateways/{ns}/{name}. */
export function useGatewayDetail(id: string, ns: string, name: string) {
  return useDetail<GatewayDetail>(id, `gateways/${seg(ns)}/${seg(name)}`);
}

/** GET …/listenersets/{group}/{kind}/{ns}/{name}. */
export function useListenerSetDetail(id: string, r: Ref) {
  return useDetail<ListenerSetDetail>(id, `listenersets/${refPath(r)}`);
}

/** GET …/routes/{group}/{kind}/{ns}/{name}; HTTPRoute only, other kinds 404. */
export function useRouteDetail(id: string, r: Ref) {
  return useDetail<RouteDetail>(id, `routes/${refPath(r)}`);
}
