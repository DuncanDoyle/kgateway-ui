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
const Wrap = styled.span<{ $stylesOverride?: SerializedStyles; $compact?: boolean }>`
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(2)};
  font-size: ${(p) => p.theme.font.size.sm};
  ${(p) =>
    p.$compact
      ? `flex-wrap: nowrap; max-width: 100%; min-width: 0;
         & > * { flex: none; }
         & > [data-item] { flex: 0 1 auto; min-width: 0; }
         [data-text] { overflow: hidden; white-space: nowrap; text-overflow: ellipsis; min-width: 0; }`
      : ""}
  ${(p) => p.$stylesOverride}
`;
const Chip = styled.span`
  display: inline-flex;
  align-items: center;
  gap: ${(p) => p.theme.space(1)};
  padding: 0 ${(p) => p.theme.space(1.5)};
  height: 20px;
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.sm};
  color: ${(p) => p.theme.color.text};
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
  min-width: 0;
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

/** Parts as sentences ending in one period: a part's own trailing period is dropped, so the join never shows "..". */
export function joinSentences(parts: string[]): string {
  return `${parts.map((p) => p.replace(/\.+$/, "")).join(". ")}.`;
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
interface ComputedChipProps {
  /** The tooltip: which rule computed the fact, and in compact form its state too. */
  label: string;
}

/** The outlined "Computed" chip alone, e.g. in a column header whose cells are computed. */
export function ComputedChip({ label }: ComputedChipProps) {
  return (
    <Tooltip label={label} placement="start" wrap>
      {/* Focusable so keyboard users can read the tooltip. */}
      <Chip tabIndex={0}>
        <Calculator size={12} strokeWidth={2.5} aria-hidden="true" />
        {computedCopy.chip}
      </Chip>
    </Tooltip>
  );
}

/** One short state for a compact badge (design §8.4): the worst of the items, so a table cell fits it. */
function shortItem(c: Computed, items: Shown[], stale: boolean): Shown {
  const first = items[0] as Shown;
  const short = computedCopy.short;
  if (stale || c.state === "unknown") return { ...first, key: "unknown", text: short.unknown };
  if (c.state === "pending") return { ...first, text: short.pending };
  if (c.state === "restricted") return { ...first, text: short.restricted };
  if (c.state === "disagrees") return { ...first, text: short.disagrees };
  // checked: Agrees when any compared label passed, else nothing was checked.
  const agrees = items.find((i) => i.state === "healthy");
  return agrees ? { ...agrees, key: "agrees", text: short.agrees } : { ...first, text: short.notVerified };
}

const Focusable = styled.span`
  display: inline-flex;
  min-width: 0;
  cursor: help;
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    background: ${(p) => p.theme.color.status.unknown.stripBg};
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
`;

interface ComputedBadgeProps {
  computed: Computed;
  /**
   * One line for a table cell: one short state ("Agrees", "Disagrees", …); the tooltip carries the rule, the full
   * states and the note. "long" keeps the full state text, cut with an ellipsis (e.g. an unknown reason column).
   */
  compact?: boolean | "long";
  /** false when the column header already carries the Computed chip; the state then holds the tooltip. */
  chip?: boolean;
  /** More tooltip text in compact form, e.g. that hostnames are not checked. */
  extra?: string;
  stylesOverride?: SerializedStyles;
}

/** Marks a fact the UI computed: an outlined "Computed" chip whose tooltip names the rule, then its check state and note. */
export function ComputedBadge({ computed, compact, chip = true, extra, stylesOverride }: ComputedBadgeProps) {
  const stale = useContext(StaleView);
  const all = stateItems(computed, stale);
  // Restricted carries no note on the wire; never show one even if sent (design §7.4).
  const note = computed.state === "restricted" ? undefined : computed.note;
  const tip = computedTooltip(computed);
  const full = compact
    ? joinSentences([tip, ...all.map((s) => s.text), ...(note ? [note] : []), ...(extra ? [extra] : [])])
    : tip;
  const items = compact === true ? [shortItem(computed, all, stale)] : all;
  const rendered = items.map((s) => (
    <Item key={s.key} $state={s.state} data-item="" data-testid={`computed-state-${s.key}`}>
      {s.icon}
      <span data-text="">{s.text}</span>
    </Item>
  ));
  return (
    <Wrap data-testid="computed-badge" $compact={!!compact} $stylesOverride={stylesOverride}>
      {chip ? <ComputedChip label={full} /> : null}
      {chip ? (
        rendered
      ) : (
        <Tooltip label={full} placement="start" wrap>
          {/* Without a chip the state is the focusable trigger, so keyboard users still reach the tooltip. */}
          <Focusable tabIndex={0} data-item="">
            {rendered}
          </Focusable>
        </Tooltip>
      )}
      {note && !compact ? <Note>{note}</Note> : null}
    </Wrap>
  );
}
