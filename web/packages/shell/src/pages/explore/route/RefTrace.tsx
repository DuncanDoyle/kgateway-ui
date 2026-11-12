import styled from "@emotion/styled";
import { useContext } from "react";
import type { BackendRef, PathSignal, TraceAttachment } from "../../../api/types";
import { pathColumnCopy, signalText } from "../../../copy/traffic";
import { HealthIcon } from "../../../health/HealthIcon";
import { StaleView } from "../../../health/staleView";
import type { HealthState } from "../../../theme/tokens";
import { StateCell } from "../../traffic/PathCells";
import { endpointsText, refShareText } from "../../traffic/pathText";
import { attachmentText } from "./Serves";

// region Styles
const Flag = styled.span<{ $state: HealthState }>`
  display: inline-flex;
  align-items: center;
  gap: 4px;
  white-space: nowrap;
  color: ${(p) => p.theme.color.status[p.$state].fg};
`;
const Why = styled.ul`
  display: grid;
  gap: 2px;
  margin: 0;
  padding: 0;
  list-style: none;
  color: ${(p) => p.theme.color.textMuted};
  overflow-wrap: anywhere;
  .on {
    color: ${(p) => p.theme.color.text};
  }
`;

// region Helpers
/** The signals in groups: the ref's own first, then one group per attachment they came from, in serves order. */
function groups(signals: PathSignal[]): { at?: number; signals: PathSignal[] }[] {
  const out: { at?: number; signals: PathSignal[] }[] = [];
  const own = signals.filter((s) => s.attachment === undefined);
  if (own.length) out.push({ signals: own });
  const ats = [...new Set(signals.flatMap((s) => (s.attachment === undefined ? [] : [s.attachment])))].sort(
    (a, b) => a - b,
  );
  for (const at of ats) out.push({ at, signals: signals.filter((s) => s.attachment === at) });
  return out;
}

// region Component
/** A backend line's share, endpoints and state. */
export function RefFacts({ b, tcp }: { b: BackendRef; tcp: boolean }) {
  const stale = useContext(StaleView);
  const share = b.shareState ? refShareText(b, tcp) : undefined;
  // A flag is a state, so a stale view drops it with every other state.
  const flag = stale ? undefined : share?.flag;
  return (
    <>
      {share ? (
        <span data-testid="share">
          {pathColumnCopy.share} {share.text}
          {flag ? (
            <>
              {" "}
              <Flag $state={flag.state}>
                <HealthIcon state={flag.state} size={14} notice />
                {flag.text}
              </Flag>
            </>
          ) : null}
        </span>
      ) : null}
      {b.endpoints ? (
        <span data-testid="endpoints">
          {pathColumnCopy.endpoints} {endpointsText(b.endpoints)}
        </span>
      ) : null}
      {b.problem ? <StateCell r={{ problem: b.problem }} /> : null}
    </>
  );
}

/** Why the ref has its state: each signal in words, under the attachment it came from; blank under a stale view. */
export function RefWhy({ b, serves }: { b: BackendRef; serves: TraceAttachment[] }) {
  if (useContext(StaleView) || !b.problem?.signals.length) return null;
  return (
    <Why data-testid="problem">
      {groups(b.problem.signals).map((g) => {
        const a = g.at === undefined ? undefined : serves[g.at];
        return (
          <li key={g.at ?? "own"}>
            {a ? <span className="on">{attachmentText(a)} </span> : null}
            {g.signals.map(signalText).join(" · ")}
          </li>
        );
      })}
    </Why>
  );
}
