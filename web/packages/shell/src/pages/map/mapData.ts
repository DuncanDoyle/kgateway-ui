import { useMemo } from "react";
import type { ApiError } from "../../api/client";
import { usePaths } from "../../api/exploreHooks";
import type { PathRow, PathSummary } from "../../api/types";
import { apiQuery } from "../../explore/listQuery";

// region Helpers
/** Rows per /paths request; the server's maximum limit. */
export const MAP_PAGE = 200;

/** The map's rows so far, the whole filtered result's summary and the data age. */
export interface MapPaths {
  rows: PathRow[];
  summary?: PathSummary;
  collectedAt?: string;
  error?: ApiError;
  /** No page has arrived and none has failed yet. */
  loading: boolean;
  /** Rows have arrived and more pages of the result remain. */
  partial: boolean;
  /** Every row of the result has arrived. */
  done: boolean;
}

/**
 * The /paths query for one page of the map: the page URL's path filters with gateway= fixed (design §6.1).
 * listener= is the Overview tab's selector and sort and cursor are the path list's, so none of them is sent.
 */
export function mapQuery(params: URLSearchParams, gateway: string): URLSearchParams {
  const rest = apiQuery("paths", params, params.get("q") ?? "", ["gateway", "listener", "sort", "cursor"]);
  return new URLSearchParams([["gateway", gateway], ...rest, ["limit", String(MAP_PAGE)]]);
}

// region Component
/** The paths through one Gateway (ns/name) under the page URL's filters; page one of MAP_PAGE rows. */
export function useMapPaths(installId: string, gateway: string, params: URLSearchParams): MapPaths {
  const query = useMemo(() => mapQuery(params, gateway), [params, gateway]);
  const { data, error } = usePaths(installId, query);
  return useMemo(
    () => ({
      rows: data?.rows ?? [],
      summary: data?.summary,
      collectedAt: data?.collectedAt,
      error,
      loading: !data && !error,
      partial: !!data?.next,
      done: !!data && !data.next,
    }),
    [data, error],
  );
}
