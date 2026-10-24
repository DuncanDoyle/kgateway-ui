import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Calculator, Clock, Lock, type LucideIcon, Minus } from "lucide-react";
import { type ReactNode, useContext } from "react";
import type { Computed } from "../api/exploreTypes";
import { computedCopy } from "../copy/explorer";
import { HealthIcon } from "../health/HealthIcon";
import { StaleView } from "../health/staleView";
import { stateLabel } from "../health/state";
import { Tooltip } from "../shell/Tooltip";
import type { HealthState } from "../theme/tokens";

// region Styles
const Wrap = styled.span<{ $stylesOverride?: SerializedStyles }>`
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(2)};
  font-size: ${(p) => p.theme.font.size.sm};
  ${(p) => p.$stylesOverride}
`;
const Chip = styled.button`
  display: inline-flex;
  align-items: center;
  gap: ${(p) => p.theme.space(1)};
  padding: 0 ${(p) => p.theme.space(1.5)};
  height: 20px;
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.sm};
  background: transparent;
  color: ${(p) => p.theme.color.text};
  font: inherit;
  font-size: ${(p) => p.theme.font.size.xs};
  font-weight: ${(p) => p.theme.font.weight.semibold};
  cursor: help;
  &:hover {
    border-color: ${(p) => p.theme.color.text};
    background: ${(p) => p.theme.color.status.unknown.stripBg};
  }
  &:active {
    border-color: ${(p) => p.theme.color.accentText};
    color: ${(p) => p.theme.color.accentText};
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
`;
const Item = styled.span<{ $state?: HealthState }>`
  display: inline-flex;
  align-items: center;
  gap: ${(p) => p.theme.space(1.5)};
  font-weight: ${(p) => p.theme.font.weight.semibold};
  color: ${(p) => (p.$state ? p.theme.color.status[p.$state].fg : p.theme.color.textMuted)};
`;
// A neutral icon: outlined, so it never reads as a health state's filled chip.
const Neutral = styled.span`
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: 16px;
  height: 16px;
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.sm};
  color: ${(p) => p.theme.color.textMuted};
`;
const Note = styled.span`
  color: ${(p) => p.theme.color.textMuted};
`;

// region Helpers
// View A's rules (rules §4); other views name their own family when they arrive.
const attachmentRules = new Set(["allowed-routes", "section-and-port", "hostname-intersection", "listener-count"]);

/** "kgateway-2.4" reads "kgateway 2.4"; any other id is shown as sent. */
function ruleSetLabel(rules: string): string {
  const m = /^kgateway-(\d+\.\d+)$/.exec(rules);
  return m ? `kgateway ${m[1]}` : rules;
}

/** The chip's tooltip: which rule of which rule set computed the fact (design §8.4). */
export function computedTooltip(c: Computed): string {
  const family = attachmentRules.has(c.rule) ? computedCopy.attachmentRules : computedCopy.otherRules;
  return computedCopy.tooltip(ruleSetLabel(c.rules), family, computedCopy.ruleNames[c.rule] ?? c.rule);
}

function NeutralIcon({ glyph: Glyph, kind }: { glyph: LucideIcon; kind: string }) {
  return (
    <Neutral data-testid={`computed-icon-${kind}`} aria-hidden="true">
      <Glyph size={11} strokeWidth={3} />
    </Neutral>
  );
}

interface Shown {
  key: string;
  icon: ReactNode;
  text: string;
  state?: HealthState;
}

/** The check state as icon + text items (design §5.2, §8.4); a stale view shows only unknown (§6.3). */
function stateItems(c: Computed, stale: boolean): Shown[] {
  const unknown = (text: string): Shown[] => [
    { key: "unknown", icon: <HealthIcon state="unknown" notice />, text, state: "unknown" },
  ];
  if (stale) return unknown(computedCopy.stale);
  switch (c.state) {
    case "pending":
      return [{ key: "pending", icon: <NeutralIcon glyph={Clock} kind="pending" />, text: computedCopy.pending }];
    case "restricted":
      return [
        { key: "restricted", icon: <NeutralIcon glyph={Lock} kind="restricted" />, text: computedCopy.restricted },
      ];
    case "unknown":
      return unknown(
        c.reason
          ? (computedCopy.unknownReasons[c.reason] ?? computedCopy.unknownOther(c.reason))
          : stateLabel("unknown"),
      );
    case "disagrees":
      return [
        {
          key: "disagrees",
          icon: <HealthIcon state="warning" notice />,
          text: computedCopy.disagrees,
          state: "warning",
        },
      ];
    default: {
      // checked: one item per coverage label; no label means nothing was verified.
      const coverage = c.coverage?.length ? c.coverage : (["not-verified"] as const);
      return coverage.map((v) =>
        v === "not-verified"
          ? { key: v, icon: <NeutralIcon glyph={Minus} kind="neutral" />, text: computedCopy.coverage[v] }
          : { key: v, icon: <HealthIcon state="healthy" notice />, text: computedCopy.coverage[v], state: "healthy" },
      );
    }
  }
}

// region Component
interface ComputedBadgeProps {
  computed: Computed;
  stylesOverride?: SerializedStyles;
}

/** Marks a fact the UI computed: an outlined "Computed" chip naming its rule, then its check state and note. */
export function ComputedBadge({ computed, stylesOverride }: ComputedBadgeProps) {
  const stale = useContext(StaleView);
  const tip = computedTooltip(computed);
  // Restricted carries no note on the wire; never show one even if sent (design §7.4).
  const note = computed.state === "restricted" ? undefined : computed.note;
  return (
    <Wrap data-testid="computed-badge" $stylesOverride={stylesOverride}>
      <Tooltip label={tip} placement="start" wrap>
        <Chip type="button" aria-label={`${computedCopy.chip}. ${tip}`}>
          <Calculator size={12} strokeWidth={2.5} aria-hidden="true" />
          {computedCopy.chip}
        </Chip>
      </Tooltip>
      {stateItems(computed, stale).map((s) => (
        <Item key={s.key} $state={s.state} data-testid={`computed-state-${s.key}`}>
          {s.icon}
          {s.text}
        </Item>
      ))}
      {note ? <Note>{note}</Note> : null}
    </Wrap>
  );
}
