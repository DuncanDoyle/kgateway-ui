import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Copy } from "lucide-react";
import type { Ref } from "../api/types";
import { Tooltip } from "../shell/Tooltip";

// region Styles
const IconButton = styled.button<{ stylesOverride?: SerializedStyles }>`
  width: 28px;
  height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  background: transparent;
  color: ${(p) => p.theme.color.textMuted};
  border: 1px solid transparent;
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    border-color: ${(p) => p.theme.color.borderStrong};
    color: ${(p) => p.theme.color.text};
  }
  &:active {
    background: ${(p) => p.theme.color.surfaceRaised};
    border-color: ${(p) => p.theme.color.text};
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
  }
  ${(p) => p.stylesOverride}
`;

// region Helpers
/** "namespace/name", or just the name for a cluster-scoped resource. */
export function copyName(r: Pick<Ref, "namespace" | "name">): string {
  return r.namespace ? `${r.namespace}/${r.name}` : r.name;
}

// region Component
interface CopyNameButtonProps {
  resource: Pick<Ref, "namespace" | "name">;
  placement?: "center" | "end";
  stylesOverride?: SerializedStyles;
}

/** Icon button that copies a resource's namespace/name. */
export function CopyNameButton({ resource, placement = "center", stylesOverride }: CopyNameButtonProps) {
  return (
    <Tooltip label="Copy namespace/name" placement={placement}>
      <IconButton
        type="button"
        aria-label="Copy namespace/name"
        stylesOverride={stylesOverride}
        onClick={() => void navigator.clipboard?.writeText(copyName(resource)).catch(() => {})}
      >
        <Copy size={14} aria-hidden="true" />
      </IconButton>
    </Tooltip>
  );
}
