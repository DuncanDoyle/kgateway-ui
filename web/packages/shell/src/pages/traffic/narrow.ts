import styled from "@emotion/styled";
import type { FilterOption } from "../../explore/StateFilter";

// The "Narrow to" chip line of the Traffic and Backends lists.

// region Styles
/** One line of chips; it scrolls sideways, so its height never changes. */
export const Narrow = styled.div`
  display: flex;
  align-items: center;
  gap: ${(p) => p.theme.space(2)};
  height: 40px;
  margin: -${(p) => p.theme.space(1)} 0 ${(p) => p.theme.space(2)};
  overflow-x: auto;
  overflow-y: hidden;
  scrollbar-width: thin;
  white-space: nowrap;
  .label {
    flex: none;
    margin-left: ${(p) => p.theme.space(1)};
    font-size: ${(p) => p.theme.font.size.sm};
    color: ${(p) => p.theme.color.textMuted};
  }
  > div {
    flex-wrap: nowrap;
  }
`;

// region Helpers
/** The most chips per group in the Narrow to line; selected values always show. */
const NARROW_MAX = 50;

/** The newly pressed value of a single-valued chip group, or "" when the pressed one was released. */
export const single = (prev: string[], next: string[]) => next.find((v) => !prev.includes(v)) ?? "";

/** values in their given order, the selected ones first so they can always be released. */
export function narrowOptions(values: string[], selected: string[]): FilterOption[] {
  const rest = values.filter((v) => !selected.includes(v));
  return [...selected, ...rest].slice(0, Math.max(NARROW_MAX, selected.length)).map((v) => ({ value: v, label: v }));
}
