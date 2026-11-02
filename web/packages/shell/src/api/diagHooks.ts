import useSWR, { useSWRConfig } from "swr";
import type { ApiError } from "./client";
import { apiFetch } from "./client";
import { forInstall, seg, useList, withQuery } from "./exploreHooks";
import { installApi, live } from "./hooks";
import type { FindingDetail, FindingItem } from "./types";

/** GET …/findings with the page's query string (filters, sort, cursor); a 400 on a cursor loads page one. */
export function useFindings(id: string, query: URLSearchParams) {
  return useList<FindingItem>(id, "findings", query);
}

/**
 * GET …/findings/{findingId}, polled; affectedCursor pages the affected objects. While another affected page loads the
 * previous one stays, marked laggy. cursorRejected is a 400 on a request with a cursor: the page should drop it.
 */
export function useFindingDetail(id: string, findingId: string, affectedCursor?: string) {
  const { cache } = useSWRConfig();
  const q = new URLSearchParams();
  if (affectedCursor) q.set("affectedCursor", affectedCursor);
  const key = withQuery(`${installApi(id)}/findings/${seg(findingId)}`, q);
  const r = useSWR<FindingDetail, ApiError>(key, apiFetch, { ...live, keepPreviousData: true });
  // Kept data shows only for the same install and finding, and only while the new key loads (as useList).
  const own = cache.get(key)?.data !== undefined;
  const same = r.data?.finding.id === findingId ? forInstall(r.data, id) : undefined;
  const cursorRejected = !!affectedCursor && r.error?.status === 400;
  // A rejected cursor keeps the previous page on screen while page one loads, so the skeleton never flashes.
  const data = own || !r.error || cursorRejected ? same : undefined;
  return {
    data,
    error: r.error,
    mutate: r.mutate,
    cursorRejected,
    isLaggy: !own && data !== undefined,
  };
}
