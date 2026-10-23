import { useTheme } from "@emotion/react";
import { Check, ChevronsUpDown } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useNow } from "../api/clock";
import { confirmedOrComputed } from "../api/freshness";
import { isStale } from "../api/hooks";
import type { InstallSummary } from "../api/types";
import { HealthIcon } from "../health/HealthIcon";
import { stateLabel } from "../health/state";
import { VisuallyHidden } from "../states/VisuallyHidden";
import { headerResourceName } from "./headerTint";
import { InstallName, installLabel } from "./InstallName";
import { Anchor, Menu, MenuButton, useMenu } from "./menu";
import { installPath } from "./paths";
import { Tooltip } from "./Tooltip";

// region Component
interface InstallSwitcherProps {
  installs: InstallSummary[];
  currentId: string;
  /** The latest installs poll failed: every item shows unknown (D-006). */
  unconfirmed?: boolean;
}

/** Shows the current install and switches between installs (D-009 §5). */
export function InstallSwitcher({ installs, currentId, unconfirmed = false }: InstallSwitcherProps) {
  const now = useNow();
  const navigate = useNavigate();
  const menu = useMenu();
  const theme = useTheme();
  const current = installs.find((i) => i.id === currentId);
  // Used for both the Tooltip label and the button's aria-label.
  const label = `Switch install: ${current ? installLabel(current) : currentId}`;
  return (
    <Anchor ref={menu.anchorRef}>
      <Tooltip label={label}>
        <MenuButton
          ref={menu.buttonRef}
          type="button"
          aria-label={label}
          aria-haspopup="menu"
          aria-expanded={menu.open}
          onClick={menu.toggle}
        >
          {current ? <InstallName install={current} stylesOverride={headerResourceName(theme)} /> : currentId}
          <ChevronsUpDown size={14} aria-hidden="true" />
        </MenuButton>
      </Tooltip>
      {menu.open ? (
        <Menu role="menu" aria-label="Installs" $align="start" ref={menu.menuRef} onKeyDown={menu.onMenuKeyDown}>
          {installs.map((i) => {
            const state = unconfirmed || isStale(confirmedOrComputed(i), now) ? "unknown" : i.health.state;
            return (
              <button
                key={i.id}
                type="button"
                role="menuitem"
                tabIndex={-1}
                aria-current={i.id === currentId}
                onClick={() => {
                  menu.close(false);
                  navigate(installPath(i.id));
                }}
              >
                <HealthIcon state={state} />
                <VisuallyHidden>{stateLabel(state)}: </VisuallyHidden>
                <InstallName install={i} />
                {i.id === currentId ? <Check data-part="check" size={14} aria-hidden="true" /> : null}
              </button>
            );
          })}
        </Menu>
      ) : null}
    </Anchor>
  );
}
