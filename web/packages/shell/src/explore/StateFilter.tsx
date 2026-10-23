import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Check, X } from "lucide-react";
import type { ReactNode } from "react";
import { Chip } from "../pages/FindingsPage";

// region Styles
const Group = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: inline-flex;
  flex-wrap: wrap;
  gap: ${(p) => p.theme.space(1)};
  ${(p) => p.stylesOverride}
`;
const Tag = styled.span`
  display: inline-flex;
  align-items: center;
  gap: 2px;
  height: 32px;
  padding: 0 4px 0 12px;
  box-sizing: border-box;
  font-size: ${(p) => p.theme.font.size.sm};
  border-radius: 16px;
  color: ${(p) => p.theme.color.text};
  background: ${(p) => p.theme.color.surfaceRaised};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  overflow-wrap: anywhere;
  button {
    all: unset;
    display: inline-flex;
    padding: 4px;
    cursor: pointer;
    border-radius: 50%;
  }
  button:hover {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 10%, ${(p) => p.theme.color.surfaceRaised});
  }
  button:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 18%, ${(p) => p.theme.color.surfaceRaised});
  }
  button:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
  }
`;

// region Helpers
export interface FilterOption {
  value: string;
  label: string;
  icon?: ReactNode;
}

function toggle(list: string[], v: string): string[] {
  return list.includes(v) ? list.filter((x) => x !== v) : [...list, v];
}

// region Component
interface StateFilterProps {
  label: string;
  options: FilterOption[];
  selected: string[];
  onChange: (selected: string[]) => void;
  stylesOverride?: SerializedStyles;
}

/** A group of toggle chips over one repeatable filter; selected values are OR-ed by the server. */
export function StateFilter({ label, options, selected, onChange, stylesOverride }: StateFilterProps) {
  return (
    <Group role="group" aria-label={label} stylesOverride={stylesOverride}>
      {options.map((o) => {
        const on = selected.includes(o.value);
        return (
          <Chip key={o.value} type="button" aria-pressed={on} onClick={() => onChange(toggle(selected, o.value))}>
            {on ? <Check size={14} aria-hidden="true" /> : o.icon}
            {o.label}
          </Chip>
        );
      })}
    </Group>
  );
}

interface FilterTagProps {
  text: string;
  onRemove: () => void;
}

/** A filter set through the URL (a link from another page), shown so it can be seen and removed. */
export function FilterTag({ text, onRemove }: FilterTagProps) {
  return (
    <Tag>
      {text}
      <button type="button" aria-label={`Remove filter ${text}`} onClick={onRemove}>
        <X size={14} aria-hidden="true" />
      </button>
    </Tag>
  );
}
