import { useTheme } from "@emotion/react";
import { Check, ChevronsUpDown } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useNow } from "../api/clock";
import { isStale } from "../api/hooks";
import type { InstallSummary } from "../api/types";
import { HealthIcon } from "../health/HealthIcon";
import { ResourceName } from "../resource/ResourceName";
import { headerResourceName } from "./headerTint";
import { Anchor, Menu, MenuButton, useMenu } from "./menu";
import { Tooltip } from "./Tooltip";

// region Component
interface InstallSwitcherProps {
  installs: InstallSummary[];
  currentId: string;
}

/** Shows the current install and switches between controller installs (D-009 §5). */
export function InstallSwitcher({ installs, currentId }: InstallSwitcherProps) {
  const now = useNow();
  const navigate = useNavigate();
  const menu = useMenu();
  const theme = useTheme();
  const current = installs.find((i) => i.id === currentId);
  return (
    <Anchor ref={menu.anchorRef}>
      <Tooltip label="Switch controller install">
        <MenuButton
          ref={menu.buttonRef}
          type="button"
          aria-label={`Switch install: ${currentId}`}
          aria-haspopup="menu"
          aria-expanded={menu.open}
          onClick={menu.toggle}
        >
          {current ? (
            <ResourceName
              namespace={current.namespace}
              name={current.name}
              stylesOverride={headerResourceName(theme)}
            />
          ) : (
            currentId
          )}
          <ChevronsUpDown size={14} aria-hidden="true" />
        </MenuButton>
      </Tooltip>
      {menu.open ? (
        <Menu
          role="menu"
          aria-label="Controller installs"
          $align="start"
          ref={menu.menuRef}
          onKeyDown={menu.onMenuKeyDown}
        >
          {installs.map((i) => (
            <button
              key={i.id}
              type="button"
              role="menuitem"
              tabIndex={-1}
              aria-current={i.id === currentId}
              onClick={() => {
                menu.close(false);
                navigate(`/i/${i.id}`);
              }}
            >
              <HealthIcon state={isStale(i.computedAt, now) ? "unknown" : i.health.state} />
              <ResourceName namespace={i.namespace} name={i.name} />
              {i.id === currentId ? <Check data-part="check" size={14} aria-hidden="true" /> : null}
            </button>
          ))}
        </Menu>
      ) : null}
    </Anchor>
  );
}
