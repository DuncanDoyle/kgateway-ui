import { useMemo, useState } from "react";
import useSWR, { useSWRConfig } from "swr";
import { ApiError, apiFetch } from "../../api/client";
import { forInstall, withQuery } from "../../api/exploreHooks";
import { installApi, live } from "../../api/hooks";
import type { PathRow, PathSummary, PathsResponse } from "../../api/types";
import { apiQuery } from "../../explore/listQuery";

// region Helpers
/** Rows per /paths request; the server's maximum limit. */
export const MAP_PAGE = 200;
/** The most rows the map loads; past it the map says how many paths it leaves out. */
export const MAP_MAX_ROWS = 2000;
/** More routes or backends than this, by the server summary, and the map opens on problems only (design §6.1). */
export const LARGE_GATEWAY = 60;

/** The map's rows so far, the whole filtered result's summary and the data age. */
export interface MapPaths {
  rows: PathRow[];
  summary?: PathSummary;
  /** The oldest loaded page's collection time, so staleness covers every row. */
  collectedAt?: string;
  error?: ApiError;
  /** No page has arrived and none has failed yet. */
  loading: boolean;
  /** Rows have arrived and more pages of the result remain. */
  partial: boolean;
  /** Every row of the result has arrived. */
  done: boolean;
  /** The result has more than MAP_MAX_ROWS paths (summary.paths); the map loads only the first MAP_MAX_ROWS. */
  truncated: boolean;
}

/**
 * The /paths query for one page of the map: the page URL's path filters with gateway= fixed (design §6.1).
 * listener= is the Overview tab's selector and sort and cursor are the path list's, so none of them is sent.
 */
export function mapQuery(params: URLSearchParams, gateway: string): URLSearchParams {
  const rest = apiQuery("paths", params, params.get("q") ?? "", ["gateway", "listener", "sort", "cursor"]);
  return new URLSearchParams([["gateway", gateway], ...rest, ["limit", String(MAP_PAGE)]]);
}

/**
 * Loads the pages of url one after another, up to MAP_MAX_ROWS rows, reporting each as it arrives.
 * A rejected cursor means the result changed under the walk, so the walk restarts from page one, once.
 */
async function fetchPages(url: string, onPage: (pages: PathsResponse[]) => void): Promise<PathsResponse[]> {
  for (let attempt = 0; ; attempt++) {
    const pages: PathsResponse[] = [];
    let rows = 0;
    try {
      for (let next: string | undefined = ""; next !== undefined && rows < MAP_MAX_ROWS; ) {
        const page: PathsResponse = await apiFetch(next ? `${url}&cursor=${encodeURIComponent(next)}` : url);
        pages.push(page);
        rows += page.rows.length;
        next = page.next;
        onPage([...pages]);
      }
      return pages;
    } catch (e) {
      if (attempt > 0 || pages.length === 0 || !(e instanceof ApiError && e.status === 400)) throw e;
    }
  }
}

/** The earliest collectedAt of the pages. */
const oldest = (pages: PathsResponse[]) =>
  pages.reduce<string | undefined>(
    (o, p) => (o === undefined || Date.parse(p.collectedAt) < Date.parse(o) ? p.collectedAt : o),
    undefined,
  );

// region Component
/**
 * The paths through one Gateway (ns/name) under the page URL's filters: MAP_PAGE rows per request, in sequence,
 * up to MAP_MAX_ROWS. Each poll reloads every page; a first load shows its rows as they arrive.
 */
export function useMapPaths(installId: string, gateway: string, params: URLSearchParams): MapPaths {
  const { cache } = useSWRConfig();
  const query = useMemo(() => mapQuery(params, gateway), [params, gateway]);
  const key = withQuery(`${installApi(installId)}/paths`, query);
  const [progress, setProgress] = useState<{ key: string; pages: PathsResponse[] }>();
  const { data, error } = useSWR<PathsResponse[], ApiError>(
    key,
    (k: string) => fetchPages(k, (pages) => setProgress({ key: k, pages })),
    // A filter change keeps the old map until the new rows arrive; once the new query fails they are dropped.
    { ...live, keepPreviousData: true },
  );
  // Relies on SWR keying its cache by key, as useList does.
  const own = cache.get(key)?.data !== undefined;
  const loaded = own || !error ? data : undefined;
  const current = loaded ?? (progress?.key === key ? progress.pages : undefined);
  return useMemo(() => {
    const pages = current?.filter((p) => forInstall(p, installId));
    const first = pages?.[0];
    const rows = pages?.flatMap((p) => p.rows).slice(0, MAP_MAX_ROWS) ?? [];
    const more = !!pages?.[pages.length - 1]?.next;
    return {
      rows,
      summary: first?.summary,
      collectedAt: pages && oldest(pages),
      error,
      loading: !first && !error,
      partial: more && rows.length < MAP_MAX_ROWS,
      done: !!first && !more,
      truncated: !!first && first.summary.paths > MAP_MAX_ROWS,
    };
  }, [current, installId, error]);
}
