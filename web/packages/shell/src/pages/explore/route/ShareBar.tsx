import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useContext } from "react";
import type { BackendRef } from "../../../api/types";
import { traceCopy } from "../../../copy/explorer";
import { pathColumnCopy, shareCopy } from "../../../copy/traffic";
import { HealthIcon } from "../../../health/HealthIcon";
import { StaleView } from "../../../health/staleView";
import type { HealthState } from "../../../theme/tokens";

// region Styles
const BAR_H = 22;

const Wrap = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  gap: 4px;
  min-width: 0;
  font-size: ${(p) => p.theme.font.size.sm};
  ${(p) => p.stylesOverride}
`;
const Caption = styled.div`
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 2px ${(p) => p.theme.space(3)};
  color: ${(p) => p.theme.color.textMuted};
`;
const Flag = styled.span<{ $state: HealthState }>`
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-weight: ${(p) => p.theme.font.weight.semibold};
  color: ${(p) => p.theme.color.status[p.$state].fg};
`;
const Note = styled.span`
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: ${(p) => p.theme.color.textMuted};
`;
const Track = styled.div`
  display: flex;
  gap: 2px;
  height: ${BAR_H}px;
  padding: 2px;
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.sm};
  background: ${(p) => p.theme.color.bg};
`;
/** One ref's part of the bar; its label shows only where it fits, so a narrow part never truncates text. */
const Segment = styled.div<{ $share: number; $state: "shown" | HealthState }>`
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 3px;
  flex: ${(p) => p.$share} 1 0;
  min-width: 3px;
  overflow: hidden;
  container-type: inline-size;
  border-radius: 2px;
  background: ${(p) => (p.$state === "shown" ? p.theme.color.accentText : p.theme.color.status[p.$state].iconBg)};
  color: ${(p) => (p.$state === "shown" ? p.theme.color.surface : p.theme.color.status[p.$state].iconFg)};
  font-size: ${(p) => p.theme.font.size.xs};
  font-weight: ${(p) => p.theme.font.weight.semibold};
  white-space: nowrap;
  /* A pattern tells failing from may-fail at any width, not colour alone; iconFg on iconBg is at least 3:1. */
  &[data-pattern="stripes"] {
    background-image: repeating-linear-gradient(
      135deg,
      ${(p) => p.theme.color.status.error.iconFg} 0 1.5px,
      transparent 1.5px 6px
    );
  }
  &[data-pattern="dots"] {
    background-image: radial-gradient(${(p) => p.theme.color.status.warning.iconFg} 1.2px, transparent 1.6px);
    background-size: 5px 5px;
  }
  /* Label and icon never shrink, so they never overlap; each shows only from the width it needs. */
  .fits {
    display: none;
    flex: none;
    align-items: center;
    gap: 3px;
    padding: 0 3px;
    border-radius: 2px;
    background: inherit;
    background-image: none;
  }
  .icon,
  .pct {
    display: inline-flex;
    flex: none;
  }
  .icon {
    display: none;
  }
  @container (min-width: 48px) {
    .fits {
      display: inline-flex;
    }
  }
  @container (min-width: 76px) {
    .icon {
      display: inline-flex;
    }
  }
`;

// region Helpers
type Bar =
  | { kind: "not-shown" }
  | { kind: "no-traffic" }
  | {
      kind: "split";
      failing: number;
      mayFail: number;
      parts: { i: number; name: string; share: number; state: string }[];
    };

/** The rule's bar from its refs' shares (design §4.4); undefined when the server sent no share for the rule. */
export function ruleBar(refs: BackendRef[]): Bar | undefined {
  // A rule of delegation refs only sends to child routes: it has no split of its own.
  if (refs.every((b) => b.shareState === undefined || b.delegation)) return undefined;
  // A rule mixing delegation and ordinary refs has no share: the split is the controller's.
  if (refs.some((b) => b.delegation || b.shareState === "not-shown")) return { kind: "not-shown" };
  if (refs.every((b) => b.shareState === "no-traffic")) return { kind: "no-traffic" };
  const sum = (s: string) => refs.reduce((n, b) => n + (b.shareState === s ? (b.share ?? 0) : 0), 0);
  const parts = refs
    .map((b, i) => ({ i, name: b.ref.name, share: b.share ?? 0, state: b.shareState ?? "shown" }))
    .filter((p) => p.share > 0);
  return { kind: "split", failing: sum("failing"), mayFail: sum("may-fail"), parts };
}

const segState = (s: string, stale: boolean): "shown" | HealthState =>
  stale ? "unknown" : s === "failing" ? "error" : s === "may-fail" ? "warning" : "shown";

const pattern = { error: "stripes", warning: "dots" } as Partial<Record<string, string>>;

// region Component
interface ShareBarProps {
  refs: BackendRef[];
  /** TCPRoute and TLSRoute: connections fail, not requests. */
  tcp: boolean;
  stylesOverride?: SerializedStyles;
}

/** A rule's stacked share bar, labelled "% of this rule's requests"; the failing and may-fail parts are flagged. */
export function ShareBar({ refs, tcp, stylesOverride }: ShareBarProps) {
  const stale = useContext(StaleView);
  const bar = ruleBar(refs);
  if (!bar) return null;
  return (
    <Wrap data-testid="share-bar" data-state={bar.kind} stylesOverride={stylesOverride}>
      <Caption>
        <span>{tcp ? traceCopy.connectionsHint : pathColumnCopy.shareHint}</span>
        {bar.kind === "not-shown" ? (
          <Note>
            <HealthIcon state="unknown" size={14} notice />
            {shareCopy.notShownHint}
          </Note>
        ) : bar.kind === "no-traffic" ? (
          <Note>
            <HealthIcon state="unknown" size={14} notice />
            {traceCopy.noTraffic}
          </Note>
        ) : null}
        {/* Flags are states, so a stale view drops them with every other state. */}
        {bar.kind === "split" && !stale && bar.failing > 0 ? (
          <Flag $state="error" data-testid="share-failing">
            <HealthIcon state="error" size={14} notice />
            {traceCopy.failShare(shareCopy.percent(bar.failing), tcp)}
          </Flag>
        ) : null}
        {bar.kind === "split" && !stale && bar.mayFail > 0 ? (
          <Flag $state="warning" data-testid="share-may-fail">
            <HealthIcon state="warning" size={14} notice />
            {traceCopy.mayFailShare(shareCopy.percent(bar.mayFail))}
          </Flag>
        ) : null}
      </Caption>
      {bar.kind === "not-shown" ? null : (
        <Track
          role="img"
          aria-label={
            bar.kind === "split"
              ? traceCopy.barLabel(
                  bar.parts.map((p) => `${p.name} ${shareCopy.percent(p.share)}`),
                  tcp,
                )
              : traceCopy.noTraffic
          }
        >
          {bar.kind === "split"
            ? bar.parts.map((p) => {
                const s = segState(p.state, stale);
                return (
                  <Segment
                    key={p.i}
                    $share={p.share}
                    $state={s}
                    data-testid="share-segment"
                    data-state={s}
                    data-pattern={pattern[s]}
                  >
                    <span className="fits">
                      {s === "shown" ? null : (
                        <span className="icon">
                          <HealthIcon state={s} size={12} notice />
                        </span>
                      )}
                      <span className="pct">{shareCopy.percent(p.share)}</span>
                    </span>
                  </Segment>
                );
              })
            : null}
        </Track>
      )}
    </Wrap>
  );
}
