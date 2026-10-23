import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { type KeyboardEvent, type ReactNode, useId, useRef } from "react";

// region Styles
/** Height of the tab strip, fixed from the first render so nothing below it moves. */
export const TABS_H = 40;

const List = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: flex;
  gap: ${(p) => p.theme.space(1)};
  height: ${TABS_H}px;
  box-sizing: border-box;
  margin-bottom: ${(p) => p.theme.space(4)};
  border-bottom: 1px solid ${(p) => p.theme.color.border};
  ${(p) => p.stylesOverride}
`;
const Tab = styled.button`
  all: unset;
  box-sizing: border-box;
  display: inline-flex;
  align-items: center;
  height: ${TABS_H}px;
  padding: 0 ${(p) => p.theme.space(3)};
  cursor: pointer;
  font-size: ${(p) => p.theme.font.size.sm};
  color: ${(p) => p.theme.color.textMuted};
  border-bottom: 2px solid transparent;
  border-radius: ${(p) => p.theme.radius.sm} ${(p) => p.theme.radius.sm} 0 0;
  &:hover {
    color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.bg});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 12%, ${(p) => p.theme.color.bg});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: -2px;
  }
  /* Selected: text weight and an underline bar, not colour alone. */
  &[aria-selected="true"] {
    color: ${(p) => p.theme.color.text};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    border-bottom-color: ${(p) => p.theme.color.accentText};
  }
`;

// region Helpers
export interface TabDef {
  id: string;
  label: string;
}

// region Component
interface DetailTabsProps {
  label: string;
  tabs: TabDef[];
  selected: string;
  onSelect: (id: string) => void;
  children: ReactNode;
  stylesOverride?: SerializedStyles;
}

/** WAI-ARIA tabs with automatic activation: arrow keys, Home and End move and select; only the selected tab is a tab stop. */
export function DetailTabs({ label, tabs, selected, onSelect, children, stylesOverride }: DetailTabsProps) {
  const base = useId();
  const els = useRef<(HTMLButtonElement | null)[]>([]);
  const at = Math.max(
    0,
    tabs.findIndex((t) => t.id === selected),
  );
  const onKey = (e: KeyboardEvent) => {
    const last = tabs.length - 1;
    const to = { ArrowRight: at === last ? 0 : at + 1, ArrowLeft: at === 0 ? last : at - 1, Home: 0, End: last }[e.key];
    if (to === undefined) return;
    e.preventDefault();
    const t = tabs[to];
    if (!t) return;
    onSelect(t.id);
    els.current[to]?.focus();
  };
  const current = tabs[at];
  return (
    <>
      <List role="tablist" aria-label={label} stylesOverride={stylesOverride} data-testid="detail-tabs">
        {tabs.map((t, i) => (
          <Tab
            key={t.id}
            ref={(el) => {
              els.current[i] = el;
            }}
            type="button"
            role="tab"
            id={`${base}-tab-${t.id}`}
            aria-selected={i === at}
            aria-controls={`${base}-panel`}
            tabIndex={i === at ? 0 : -1}
            onClick={() => onSelect(t.id)}
            onKeyDown={onKey}
          >
            {t.label}
          </Tab>
        ))}
      </List>
      <div role="tabpanel" id={`${base}-panel`} aria-labelledby={`${base}-tab-${current?.id}`}>
        {children}
      </div>
    </>
  );
}
