import { useMemo } from "react";
import useSWR from "swr";
import { type ApiError, apiFetch, apiFetchWithHeaders } from "./client";
import type { HealthReport, InstallSummary, MetaResponse, OverviewResponse, RawHealthReport, Ref } from "./types";

/** Poll interval for live data (design §6.6). */
export const POLL_MS = 5000;

// No keepPreviousData: on an install switch it would show the old install's data under the new URL.
export const live = { refreshInterval: POLL_MS };

/** True when data collected at collectedAt is older than two poll intervals; pass useNow() as now. */
export function isStale(collectedAt: string, now: number): boolean {
  const age = now - Date.parse(collectedAt);
  // An unparsable timestamp gives NaN; treat it as stale.
  return Number.isNaN(age) || age > 2 * POLL_MS;
}

/** True when /api/v1/meta lists r's kind. The shell no longer links by kind: links come from LinkedRef.link. */
export function isLinkable(meta: MetaResponse | undefined, r: Ref): boolean {
  return !!meta?.kinds.some((k) => k.group === r.group && k.kind === r.kind);
}

// The backend sends checks as null when no check ran.
async function fetchHealth(path: string): Promise<HealthReport> {
  const r = await apiFetch<RawHealthReport>(path);
  return { ...r, checks: r.checks ?? [] };
}

/** GET /api/v1/installs. */
export function useInstalls() {
  return useSWR<InstallSummary[], ApiError>("/api/v1/installs", apiFetch, live);
}

/** GET /api/v1/meta. */
export function useMeta() {
  return useSWR<MetaResponse, ApiError>("/api/v1/meta", apiFetch);
}

/** API base of one install; the ID is encoded, never parsed. */
export const installApi = (id: string) => `/api/v1/installs/${encodeURIComponent(id)}`;

/** GET /api/v1/installs/{id}/overview. */
export function useOverview(id: string) {
  return useSWR<OverviewResponse, ApiError>(`${installApi(id)}/overview`, apiFetch, live);
}

/** GET /api/v1/installs/{id}/health. */
export function useHealth(id: string) {
  return useSWR<HealthReport, ApiError>(`${installApi(id)}/health`, fetchHealth, live);
}

interface ResourceData {
  object: Record<string, unknown>;
  collectedAt?: string;
}

// The resource body is the raw object, so the collection time comes in a header (D-017 §6).
async function fetchResource(path: string): Promise<ResourceData> {
  const { body, headers } = await apiFetchWithHeaders<Record<string, unknown>>(path);
  return { object: body, collectedAt: headers.get("Collected-At") ?? undefined };
}

/** GET one resource, polled; "-" stands for an empty group or namespace. collectedAt is undefined until it loads. */
export function useResource(id: string, r: Ref) {
  const path = `${installApi(id)}/resources/${r.group || "-"}/${r.kind}/${r.namespace || "-"}/${r.name}`;
  // Read only these fields: SWR re-renders for each one read, and spreading reads isValidating too.
  const { data, error, mutate } = useSWR<ResourceData, ApiError>(path, fetchResource, live);
  return { data: data?.object, error, mutate, collectedAt: data?.collectedAt };
}

/** The platform's capabilities from /api/v1/meta (D-016); undefined until meta loads. */
export function useCapabilities(): ReadonlySet<string> | undefined {
  const { data } = useMeta();
  return useMemo(() => (data ? new Set(data.capabilities ?? []) : undefined), [data]);
}
