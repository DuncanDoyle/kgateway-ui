import { useEffect, useRef, useState } from "react";
import useSWR, { useSWRConfig } from "swr";
import type { ApiError } from "./client";
import { apiFetch } from "./client";
import { installApi, live, POLL_MS } from "./hooks";
import type {
  DelegationRow,
  GatewayDetail,
  GatewayListRow,
  ListenerSetDetail,
  ListenerSetRow,
  Page,
  PathRow,
  PathsResponse,
  Ref,
  RouteDetail,
  RouteRow,
  TrailRefs,
} from "./types";

// region Helpers
export const seg = encodeURIComponent;
export const withQuery = (path: string, q: URLSearchParams) => {
  const s = q.toString();
  return s ? `${path}?${s}` : path;
};

/** A response is shown only for the install in the URL, so a switch never shows the previous install's data. */
export const forInstall = <T extends { install: string }>(data: T | undefined, id: string) =>
  data?.install === id ? data : undefined;

/**
 * One list page, polled. A 400 on a request with a cursor sets cursorRejected and loads page one instead (design §8.6);
 * the page should then drop the cursor from its URL. isLaggy marks rows of the previous query while a new one loads.
 */
export function useList<T, P extends { install: string } = Page<T>>(id: string, list: string, query: URLSearchParams) {
  const { cache } = useSWRConfig();
  const path = `${installApi(id)}/${list}`;
  const hasCursor = query.has("cursor");
  const rejects = (e: ApiError | undefined) => hasCursor && e?.status === 400;
  // A rejected cursor is final: its key neither polls nor retries, so it never loops on a 400.
  const [rejected, setRejected] = useState<string>();
  const key = withQuery(path, query);
  const paged = useSWR<P, ApiError>(key, apiFetch, {
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
  const first = useSWR<P, ApiError>(cursorRejected ? withQuery(path, pageOne) : null, apiFetch, live);
  const r = cursorRejected ? first : paged;
  const rKey = cursorRejected ? withQuery(path, pageOne) : key;
  // With keepPreviousData the rows may belong to the previous query. They show only while the new one loads
  // (laggy, marked busy); once the new query fails they are dropped, so its error never sits over another's rows.
  // Relies on SWR keying its cache by the same withQuery string passed to useSWR.
  const own = cache.get(rKey)?.data !== undefined;
  const data = own || !r.error ? forInstall(r.data, id) : undefined;
  return { data, error: r.error, mutate: r.mutate, cursorRejected, isLaggy: !own && data !== undefined };
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

/** GET …/paths: the install's paths, filtered, sorted and paged by the server (Phase 6 design §5). */
export function usePaths(id: string, query: URLSearchParams) {
  return useList<PathRow, PathsResponse>(id, "paths", query);
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

/** GET …/routes/{group}/{kind}/{ns}/{name}/delegation: view B's rows of one parent route, paged (rule, ref, outcome, cursor). */
export function useDelegation(id: string, r: Ref, query: URLSearchParams) {
  return useList<DelegationRow>(id, `routes/${refPath(r)}/delegation`, query);
}

/** A trail ref parameter: "core" for the empty group, "-" for no namespace (GET …/refs). */
const trailParam = (r: Ref) => `${r.group || "core"}/${r.kind}/${r.namespace || "-"}/${r.name}`;

/** The most refs one GET …/refs takes: the server's bound. */
export const REFS_BATCH = 6;

/** GET …/refs for every param, in requests of at most {@link REFS_BATCH}, as one answer. */
async function fetchTrailLinks([path, ...params]: readonly string[]): Promise<TrailRefs> {
  const batches: string[][] = [];
  for (let i = 0; i < params.length; i += REFS_BATCH) batches.push(params.slice(i, i + REFS_BATCH));
  const pages = await Promise.all(
    batches.map((b) => apiFetch<TrailRefs>(withQuery(path as string, new URLSearchParams(b.map((p) => ["ref", p]))))),
  );
  // An answer split across installs (a switch mid-flight) matches none, so forInstall drops it.
  const install = pages.every((p) => p.install === pages[0]?.install) ? (pages[0]?.install ?? "") : "";
  return { install, refs: pages.flatMap((p) => p.refs) };
}

/** A lost connection (status 0) or a server error may pass; a 4xx is the server's final answer. */
const transient = (e: ApiError) => e.status === 0 || e.status >= 500;

/**
 * GET …/refs for the breadcrumb trail's objects: once per page view, never polled, batched within the server's bound.
 * The key is the sorted refs, so the same trail is one lookup; no refs means no request. Only a 4xx is final (failed);
 * a lost connection or a 5xx retries with backoff, and again on reconnect while it is still failing.
 */
export function useTrailLinks(id: string, refs: readonly Ref[]) {
  const params = [...new Set(refs.map(trailParam))].sort();
  const key = params.length ? [`${installApi(id)}/refs`, ...params] : null;
  const [failing, setFailing] = useState(false);
  // The pending retry; a success, a new key or unmount cancels it, so it never replaces a good answer.
  const retry = useRef<ReturnType<typeof setTimeout>>(undefined);
  const cancel = () => clearTimeout(retry.current);
  const keyId = key?.join(",") ?? "";
  // biome-ignore lint/correctness/useExhaustiveDependencies: keyId is the trigger; cancel reads only the ref.
  useEffect(() => cancel, [keyId]);
  const { data, error } = useSWR<TrailRefs, ApiError>(key, fetchTrailLinks, {
    onSuccess: () => {
      cancel();
      setFailing(false);
    },
    revalidateOnFocus: false,
    // Config is read when the event fires, so a settled lookup never refetches on reconnect.
    revalidateOnReconnect: failing,
    onErrorRetry: (e, _key, _config, revalidate, opts) => {
      setFailing(transient(e));
      cancel();
      if (!transient(e)) return;
      retry.current = setTimeout(() => revalidate(opts), Math.min(1000 * 2 ** (opts.retryCount - 1), 30_000));
    },
  });
  return { data: forInstall(data, id), error, failed: error !== undefined && !transient(error) };
}
