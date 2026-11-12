import styled from "@emotion/styled";
import { Check, Palette } from "lucide-react";
import { useThemeSwitch } from "../theme/ThemeProvider";
import { Anchor, Menu, MenuButton, useMenu } from "./menu";
import { Tooltip } from "./Tooltip";

// region Styles
/** The icon button keeps its size; the install nav gives way on narrow windows. */
const Fixed = styled(Anchor)`
  flex: none;
`;

// region Component
/** Compact icon dropdown listing every theme (team rule: not a light/dark toggle). */
export function ThemeSwitcher() {
  const { themes, current, setTheme } = useThemeSwitch();
  const menu = useMenu();
  return (
    <Fixed ref={menu.anchorRef}>
      <Tooltip label={`Theme: ${current.name}`} placement="end">
        <MenuButton
          ref={menu.buttonRef}
          type="button"
          aria-label={`Theme: ${current.name}`}
          aria-haspopup="menu"
          aria-expanded={menu.open}
          onClick={menu.toggle}
        >
          <Palette size={14} aria-hidden="true" />
        </MenuButton>
      </Tooltip>
      {menu.open ? (
        <Menu role="menu" aria-label="Theme" ref={menu.menuRef} onKeyDown={menu.onMenuKeyDown}>
          {themes.map((t) => (
            <button
              key={t.id}
              type="button"
              role="menuitemradio"
              tabIndex={-1}
              aria-checked={t.id === current.id}
              onClick={() => {
                setTheme(t.id);
                menu.close(true);
              }}
            >
              {t.name}
              {t.id === current.id ? <Check data-part="check" size={14} aria-hidden="true" /> : null}
            </button>
          ))}
        </Menu>
      ) : null}
    </Fixed>
  );
}
