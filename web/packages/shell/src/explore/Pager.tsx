import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { listCopy } from "../copy/explorer";

// region Styles
const Nav = styled.nav<{ stylesOverride?: SerializedStyles }>`
  display: inline-flex;
  gap: ${(p) => p.theme.space(1)};
  button {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 28px;
    padding: 0 10px;
    cursor: pointer;
    font: inherit;
    font-size: ${(p) => p.theme.font.size.sm};
    color: ${(p) => p.theme.color.text};
    background: ${(p) => p.theme.color.surface};
    border: 1px solid ${(p) => p.theme.color.borderStrong};
    border-radius: ${(p) => p.theme.radius.sm};
  }
  button:hover:not(:disabled) {
    border-color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.surface});
  }
  button:active:not(:disabled) {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
  button:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 1px;
  }
  button:disabled {
    cursor: default;
    color: ${(p) => p.theme.color.textMuted};
    border-color: ${(p) => p.theme.color.border};
  }
  ${(p) => p.stylesOverride}
`;

// region Component
interface PagerProps {
  /** The server's opaque cursors; absent means no page that way. */
  prev?: string;
  next?: string;
  onPage: (cursor: string) => void;
  stylesOverride?: SerializedStyles;
}

/** Previous and next page buttons over keyset cursors; both render, disabled, while there is no data. */
export function Pager({ prev, next, onPage, stylesOverride }: PagerProps) {
  return (
    <Nav aria-label={listCopy.pages} stylesOverride={stylesOverride}>
      <button type="button" aria-label={listCopy.previousPage} disabled={!prev} onClick={() => prev && onPage(prev)}>
        <ChevronLeft size={14} aria-hidden="true" />
        {listCopy.previous}
      </button>
      <button type="button" aria-label={listCopy.nextPage} disabled={!next} onClick={() => next && onPage(next)}>
        {listCopy.next}
        <ChevronRight size={14} aria-hidden="true" />
      </button>
    </Nav>
  );
}
