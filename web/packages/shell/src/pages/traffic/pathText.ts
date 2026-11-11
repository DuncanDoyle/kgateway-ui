import type { Endpoints, GRPCMatch, PathRow } from "../../api/types";
import { cellCopy, endpointCopy, shareCopy, signalDetail, signalText } from "../../copy/traffic";
import type { HealthState } from "../../theme/tokens";
import { refText } from "../explore/listParts";
import { grpcParts, matchText } from "../explore/route/MatchLine";

// Text of a path row's cells, shared by the table and its tests (Phase 6 design §4).

/** A row's identity: its Gateway, listener, route chain, rule and target; duplicates get a counter. */
export function baseKey(r: PathRow): string {
  const t = r.backend
    ? refText(r.backend)
    : r.delegation
      ? `${r.delegation.namespace}/${r.delegation.name ?? r.delegation.label}`
      : "";
  return [
    refText(r.gateway),
    r.listenerSet ? refText(r.listenerSet) : "",
    r.listener.name,
    r.routes.map((x) => `${x.kind}/${refText(x)}`).join(">"),
    r.rule,
    r.action,
    t,
    r.port ?? "",
    r.mirror ? "m" : "",
  ].join("|");
}

/** The page's rows keyed once; a repeated key gets "#n", so React and expansion state see unique rows. */
export function keyRows(rows: PathRow[]): Map<PathRow, string> {
  const seen = new Map<string, number>();
  const out = new Map<PathRow, string>();
  for (const r of rows) {
    const k = baseKey(r);
    const n = seen.get(k) ?? 0;
    seen.set(k, n + 1);
    out.set(r, n ? `${k}#${n}` : k);
  }
  return out;
}

const grpcText = (m: GRPCMatch) => grpcParts(m.method, m.headers).join(" ");

/** The rule's match on this path: the root route's kind decides which match fields apply (design §4.1). */
export function matchSummary(r: PathRow): { text: string; full: string; unknown?: boolean } {
  const kind = r.routes[0]?.kind;
  if (kind === "TCPRoute" || kind === "TLSRoute")
    return { text: cellCopy.allConnections, full: cellCopy.allConnections };
  // null is a delegated row whose view C match is unknown; the child's written rule is never shown instead.
  if (r.matches === null) return { text: cellCopy.matchUnknown, full: cellCopy.matchUnknown, unknown: true };
  const parts = kind === "GRPCRoute" ? (r.grpcMatches ?? []).map(grpcText) : r.matches.map(matchText);
  if (!parts.length) return { text: cellCopy.allRequests, full: cellCopy.allRequests };
  const text = parts.length > 1 ? `${parts[0]} ${cellCopy.more(parts.length - 1)}` : (parts[0] ?? "");
  return { text, full: parts.join("\n") };
}

/** TCPRoute and TLSRoute carry connections, not requests. */
export const tcpKind = (kind?: string) => kind === "TCPRoute" || kind === "TLSRoute";
const tcpLike = (r: PathRow) => tcpKind(r.routes[0]?.kind);

/** The share cell's text and the state its flag shows, if any (design §4.4). */
export function shareText(r: PathRow): { text: string; flag?: { state: HealthState; text: string } } {
  // A redirect, direct response or rule without backends has no share.
  if (r.action !== "backend") return { text: "" };
  if (r.shareState === "mirror") return { text: shareCopy.mirror(r.mirrorPercent ?? 0) };
  return refShareText(r, tcpLike(r));
}

/** A backendRef's share and flag; tcp picks "connections fail" over "requests fail". */
export function refShareText(
  r: { share?: number; shareState?: PathRow["shareState"] },
  tcp: boolean,
): { text: string; flag?: { state: HealthState; text: string } } {
  const pct = shareCopy.percent(r.share ?? 0);
  switch (r.shareState) {
    case "shown":
      return { text: pct };
    case "no-traffic":
      return { text: pct, flag: { state: "unknown", text: shareCopy.noTraffic } };
    case "failing":
      return { text: pct, flag: { state: "error", text: tcp ? shareCopy.connectionsFail : shareCopy.requestsFail } };
    case "may-fail":
      return { text: pct, flag: { state: "warning", text: shareCopy.mayFail } };
    default:
      return { text: shareCopy.notShown };
  }
}

/** The endpoints cell's text: a count only when the read established one. */
export function endpointText(r: PathRow): string | undefined {
  return r.endpoints ? endpointsText(r.endpoints) : undefined;
}

/** One endpoints summary in words: a count only when the read established one, never "0" for a read that did not. */
export function endpointsText(e: Endpoints): string {
  switch (e.complete) {
    case "complete":
      return e.ready !== undefined && e.total !== undefined
        ? endpointCopy.ready(e.ready, e.total)
        : endpointCopy.unknown;
    case "partial":
      return endpointCopy.partial;
    case "failed":
      return endpointCopy.failed;
    case "unavailable":
      return endpointCopy.unavailable;
    case "stale":
      return endpointCopy.stale;
    case "timed-out":
      return endpointCopy.timedOut;
    case "busy":
      return endpointCopy.busy;
    default:
      return endpointCopy.notApplicable;
  }
}

/** The problem's signals, one per line, for the state's tooltip and the Why column. */
export const signalLines = (r: Pick<PathRow, "problem">) => r.problem.signals.map(signalText);

/** The signals with their codes, for tooltips. */
export const signalDetails = (r: Pick<PathRow, "problem">) => r.problem.signals.map(signalDetail);
