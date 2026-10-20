import styled from "@emotion/styled";
import { type KeyboardEvent, useCallback, useEffect, useRef, useState } from "react";
import { HEADER_HOVER_PCT, HEADER_PRESS_PCT, headerTint } from "./headerTint";

// region Styles
// Header controls sit on the ink band and use its tints.
export const MenuButton = styled.button`
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 32px;
  min-width: 0;
  padding: 0 10px;
  cursor: pointer;
  font: inherit;
  color: ${(p) => p.theme.color.headerText};
  background: transparent;
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    background: ${(p) => headerTint(p.theme, HEADER_HOVER_PCT)};
    border-color: ${(p) => p.theme.color.headerText};
  }
  &:active,
  &[aria-expanded="true"] {
    background: ${(p) => headerTint(p.theme, HEADER_PRESS_PCT)};
  }
`;

export const Menu = styled.div<{ $align?: "start" | "end" }>`
  position: absolute;
  top: calc(100% + 6px);
  ${(p) => (p.$align === "start" ? "left: 0;" : "right: 0;")}
  z-index: 30;
  min-width: 240px;
  padding: 4px;
  background: ${(p) => p.theme.color.surfaceRaised};
  color: ${(p) => p.theme.color.text};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.md};
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
  [role^="menuitem"] {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-height: 34px;
    padding: 6px 8px;
    cursor: pointer;
    font: inherit;
    text-align: left;
    background: transparent;
    border: 0;
    border-radius: ${(p) => p.theme.radius.sm};
    color: ${(p) => p.theme.color.text};
  }
  [role^="menuitem"]:hover,
  [role^="menuitem"]:focus-visible {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 8%, ${(p) => p.theme.color.surfaceRaised});
  }
  [role^="menuitem"]:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 16%, ${(p) => p.theme.color.surfaceRaised});
  }
  [role^="menuitem"]:focus-visible {
    outline-offset: -2px;
  }
  [data-part="check"] {
    margin-left: auto;
    color: ${(p) => p.theme.color.accentText};
  }
`;

export const Anchor = styled.div`
  position: relative;
  display: inline-flex;
  min-width: 0;
`;

// region Helpers
/**
 * Open state and keyboard handling for a header menu: opening focuses the checked (or first) item,
 * arrows/Home/End move focus, Escape closes and refocuses the button, Tab and outside presses close.
 */
export function useMenu() {
  const [open, setOpen] = useState(false);
  const anchorRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  const close = useCallback((refocus: boolean) => {
    setOpen(false);
    if (refocus) buttonRef.current?.focus();
  }, []);

  useEffect(() => {
    if (!open) return;
    const items = menuItems(menuRef.current);
    (
      items.find((i) => i.getAttribute("aria-checked") === "true" || i.getAttribute("aria-current") === "true") ??
      items[0]
    )?.focus();
    const onPointerDown = (e: PointerEvent) => {
      if (!anchorRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open]);

  const onMenuKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const items = menuItems(menuRef.current);
    const at = items.indexOf(document.activeElement as HTMLElement);
    const move = (i: number) => {
      e.preventDefault();
      items[(i + items.length) % items.length]?.focus();
    };
    if (e.key === "Escape") {
      e.preventDefault();
      close(true);
    } else if (e.key === "Tab") close(false);
    else if (e.key === "ArrowDown") move(at + 1);
    else if (e.key === "ArrowUp") move(at - 1);
    else if (e.key === "Home") move(0);
    else if (e.key === "End") move(items.length - 1);
  };

  return { open, toggle: () => setOpen((o) => !o), close, anchorRef, buttonRef, menuRef, onMenuKeyDown };
}

function menuItems(menu: HTMLElement | null): HTMLElement[] {
  return menu ? Array.from(menu.querySelectorAll<HTMLElement>('[role^="menuitem"]')) : [];
}
