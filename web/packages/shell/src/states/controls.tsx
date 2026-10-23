import styled from "@emotion/styled";
import { Link } from "react-router-dom";
import { CHIP_PRESSED_ACTIVE_PCT, CHIP_PRESSED_HOVER_PCT, CHIP_PRESSED_PCT } from "../shell/constants";

// Shared controls of the core pages, each with hover, active and focus-visible styles in every theme.

// region Styles
/** A toggle filter chip; aria-pressed marks it selected. */
export const Chip = styled.button`
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 12px;
  cursor: pointer;
  user-select: none;
  font: inherit;
  font-size: ${(p) => p.theme.font.size.sm};
  border-radius: 16px;
  color: ${(p) => p.theme.color.text};
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  &:hover {
    border-color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.surface});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
  /* Selected: accent fill and a doubled accent border, plus a check icon and bold text. */
  &[aria-pressed="true"] {
    border-color: ${(p) => p.theme.color.accentText};
    box-shadow: inset 0 0 0 1px ${(p) => p.theme.color.accentText};
    background: color-mix(in srgb, ${(p) => p.theme.color.accentText} ${CHIP_PRESSED_PCT}%, ${(p) => p.theme.color.surface});
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  &[aria-pressed="true"]:hover {
    background: color-mix(in srgb, ${(p) => p.theme.color.accentText} ${CHIP_PRESSED_HOVER_PCT}%, ${(p) => p.theme.color.surface});
  }
  &[aria-pressed="true"]:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.accentText} ${CHIP_PRESSED_ACTIVE_PCT}%, ${(p) => p.theme.color.surface});
  }
`;
/** A text filter with a search icon. */
export const SearchBox = styled.label`
  position: relative;
  display: inline-flex;
  align-items: center;
  svg {
    position: absolute;
    left: 10px;
    color: ${(p) => p.theme.color.textMuted};
    pointer-events: none;
  }
  input {
    box-sizing: border-box;
    height: 32px;
    width: 280px;
    max-width: 100%;
    padding: 0 10px 0 30px;
    font: inherit;
    font-size: ${(p) => p.theme.font.size.sm};
    color: ${(p) => p.theme.color.text};
    background: ${(p) => p.theme.color.surface};
    border: 1px solid ${(p) => p.theme.color.borderStrong};
    border-radius: ${(p) => p.theme.radius.sm};
  }
  input::placeholder {
    color: ${(p) => p.theme.color.textMuted};
  }
  input:hover {
    border-color: ${(p) => p.theme.color.text};
  }
  input:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 1px;
  }
`;
/** A bordered text button, e.g. "Show last-applied config". */
export const TextButton = styled.button`
  height: 28px;
  padding: 0 10px;
  cursor: pointer;
  font: inherit;
  font-size: ${(p) => p.theme.font.size.sm};
  color: ${(p) => p.theme.color.text};
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    border-color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.surface});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
`;
/** A quiet text button; e.g. Clear filters. */
export const QuietButton = styled.button`
  height: 28px;
  padding: 0 8px;
  cursor: pointer;
  font: inherit;
  font-size: ${(p) => p.theme.font.size.sm};
  color: ${(p) => p.theme.color.accentText};
  background: transparent;
  border: 0;
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    text-decoration: underline;
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.surface});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
  }
`;
/** A link back to the install, under a page state. */
export const BackLink = styled(Link)`
  display: inline-block;
  margin-top: ${(p) => p.theme.space(2)};
  padding: 4px 8px;
  border-radius: ${(p) => p.theme.radius.sm};
  color: ${(p) => p.theme.color.accentText};
  font-weight: ${(p) => p.theme.font.weight.semibold};
  text-decoration: none;
  &:hover {
    text-decoration: underline;
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.bg});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 12%, ${(p) => p.theme.color.bg});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
  }
`;
