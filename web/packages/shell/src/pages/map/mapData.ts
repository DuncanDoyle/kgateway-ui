import { useMemo, useRef, useState } from "react";
import useSWR, { useSWRConfig } from "swr";
import { type ApiError, apiFetch } from "../../api/client";
import { forInstall, withQuery } from "../../api/exploreHooks";
import { installApi, live } from "../../api/hooks";
import type { PathRow, PathSummary, PathsResponse } from "../../api/types";
import { apiQuery } from "../../explore/listQuery";
import type { HealthState } from "../../theme/tokens";
import { refParam } from "../explore/detailParts";

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
  /** The result kept changing during the walk, so these rows are the last result that held still (or the last walk). */
  inconsistent: boolean;
}

/**
 * The /paths query for one page of the map: the page URL's path filters with gateway= fixed (design §6.1).
 * listener= is the Overview tab's selector and sort and cursor are the path list's, so none of them is sent.
 */
export function mapQuery(params: URLSearchParams, gateway: string): URLSearchParams {
  const rest = apiQuery("paths", params, params.get("q") ?? "", ["gateway", "listener", "sort", "cursor"]);
  return new URLSearchParams([["gateway", gateway], ...rest, ["limit", String(MAP_PAGE)]]);
}

/** Retries of a walk whose pages disagree on the result's counts. */
const WALK_RETRIES = 2;

/** One walk over the pages; inconsistent when its pages never agreed on the result's counts. */
interface MapResult {
  pages: PathsResponse[];
  inconsistent: boolean;
}

/** True when two pages report the same counts for the whole result, so the result did not change between them. */
const sameResult = (a: PathsResponse, b: PathsResponse) =>
  a.summary.paths === b.summary.paths && JSON.stringify(a.summary.byState) === JSON.stringify(b.summary.byState);

/**
 * Loads the pages of url one after another, up to MAP_MAX_ROWS rows, reporting each as it arrives.
 * The cursor is a keyset anchor, so a row whose state changes mid-walk can move before it and be skipped
 * (D-006). Every page carries the whole result's summary; when one differs from page one's, the walk restarts.
 * After WALK_RETRIES restarts the previous result is kept (or, on a first load, the last walk), marked inconsistent.
 * The walk stops after any page once wanted(page one) is false.
 */
async function fetchPages(
  url: string,
  prev: MapResult | undefined,
  onPage: (pages: PathsResponse[]) => void,
  wanted: (first: PathsResponse) => boolean,
): Promise<MapResult> {
  let pages: PathsResponse[] = [];
  for (let attempt = 0; attempt <= WALK_RETRIES; attempt++) {
    pages = [];
    let rows = 0;
    let consistent = true;
    for (let next: string | undefined = ""; next !== undefined && rows < MAP_MAX_ROWS; ) {
      if (pages[0] && !wanted(pages[0])) return { pages, inconsistent: false };
      const page: PathsResponse = await apiFetch(next ? `${url}&cursor=${encodeURIComponent(next)}` : url);
      if (pages[0] && !sameResult(pages[0], page)) {
        consistent = false;
        break;
      }
      pages.push(page);
      rows += page.rows.length;
      next = page.next;
      onPage([...pages]);
    }
    if (consistent) return { pages, inconsistent: false };
  }
  return { pages: prev?.pages ?? pages, inconsistent: true };
}

/** A path's identity: Gateway, ListenerSet, listener, route chain, rule, backend, port and mirror. */
const rowKey = (r: PathRow) =>
  JSON.stringify([
    refParam(r.gateway),
    r.listenerSet && refParam(r.listenerSet),
    r.listener.name,
    r.routes.map(refParam),
    r.rule,
    r.backend && refParam(r.backend),
    r.port,
    r.mirror,
  ]);

const RANK: Record<HealthState, number> = { healthy: 0, unknown: 1, warning: 2, error: 3 };

/** The rows with each path once, in first-seen order, keeping the copy with the worst state. */
export function dedupeRows(rows: PathRow[]): PathRow[] {
  const at = new Map<string, number>();
  const out: PathRow[] = [];
  for (const r of rows) {
    const k = rowKey(r);
    const i = at.get(k);
    if (i === undefined) {
      at.set(k, out.length);
      out.push(r);
    } else if (RANK[r.problem.state] > RANK[(out[i] as PathRow).problem.state]) out[i] = r;
  }
  return out;
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
 * A walk stops after page one when stopAt says so for its summary, or once the URL has moved to another query.
 */
export function useMapPaths(
  installId: string,
  gateway: string,
  params: URLSearchParams,
  stopAt?: (summary: PathSummary) => boolean,
): MapPaths {
  const { cache } = useSWRConfig();
  const query = useMemo(() => mapQuery(params, gateway), [params, gateway]);
  const key = withQuery(`${installApi(installId)}/paths`, query);
  const [progress, setProgress] = useState<{ key: string; pages: PathsResponse[] }>();
  const shown = useRef({ key, stopAt });
  shown.current = { key, stopAt };
  const { data, error } = useSWR<MapResult, ApiError>(
    key,
    (k: string) =>
      fetchPages(
        k,
        cache.get(k)?.data as MapResult | undefined,
        (pages) => setProgress({ key: k, pages }),
        (first) => shown.current.key === k && !shown.current.stopAt?.(first.summary),
      ),
    // A filter change keeps the old map until the new rows arrive; once the new query fails they are dropped.
    { ...live, keepPreviousData: true },
  );
  // Relies on SWR keying its cache by key, as useList does.
  const own = cache.get(key)?.data !== undefined;
  const loaded = own || !error ? data : undefined;
  const current = loaded?.pages ?? (progress?.key === key ? progress.pages : undefined);
  const inconsistent = !!loaded?.inconsistent;
  return useMemo(() => {
    const pages = current?.filter((p) => forInstall(p, installId));
    const first = pages?.[0];
    const rows = dedupeRows(pages?.flatMap((p) => p.rows) ?? []).slice(0, MAP_MAX_ROWS);
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
      inconsistent,
    };
  }, [current, inconsistent, installId, error]);
}
