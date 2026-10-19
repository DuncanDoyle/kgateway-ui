import useSWR from "swr";
import { type ApiError, apiFetch } from "./client";
import type { HealthReport, InstallSummary, MetaResponse, OverviewResponse, Ref } from "./types";

/** Poll interval for live data (design §6.6). */
export const POLL_MS = 5000;

// No keepPreviousData: on an install switch it would show the old install's data under the new URL.
const live = { refreshInterval: POLL_MS };

/** True when data collected at collectedAt is older than two poll intervals; pass useNow() as now. */
export function isStale(collectedAt: string, now: number): boolean {
  return now - Date.parse(collectedAt) > 2 * POLL_MS;
}

/** True when /api/v1/meta lists r's kind, so its YAML view can resolve. */
export function isLinkable(meta: MetaResponse | undefined, r: Ref): boolean {
  return !!meta?.kinds.some((k) => k.group === r.group && k.kind === r.kind);
}

/** GET /api/v1/installs. */
export function useInstalls() {
  return useSWR<InstallSummary[], ApiError>("/api/v1/installs", apiFetch, live);
}

/** GET /api/v1/meta. */
export function useMeta() {
  return useSWR<MetaResponse, ApiError>("/api/v1/meta", apiFetch);
}

/** GET /api/v1/installs/{id}/overview. */
export function useOverview(id: string) {
  return useSWR<OverviewResponse, ApiError>(`/api/v1/installs/${id}/overview`, apiFetch, live);
}

/** GET /api/v1/installs/{id}/health. */
export function useHealth(id: string) {
  return useSWR<HealthReport, ApiError>(`/api/v1/installs/${id}/health`, apiFetch, live);
}

/** GET one resource; "-" stands for an empty group or namespace. */
export function useResource(id: string, r: Ref) {
  const path = `/api/v1/installs/${id}/resources/${r.group || "-"}/${r.kind}/${r.namespace || "-"}/${r.name}`;
  return useSWR<Record<string, unknown>, ApiError>(path, apiFetch);
}
