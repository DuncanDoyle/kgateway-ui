import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { cloneElement, type ReactElement, useId, useRef, useState } from "react";

// region Styles
const Wrap = styled.span<{ stylesOverride?: SerializedStyles }>`
  position: relative;
  display: inline-flex;
  ${(p) => p.stylesOverride}
`;
const Tip = styled.span<{ $placement: "center" | "end" }>`
  position: absolute;
  top: calc(100% + 6px);
  ${(p) => (p.$placement === "end" ? "right: 0;" : "left: 50%; transform: translateX(-50%);")}
  z-index: 20;
  padding: 4px 8px;
  border-radius: ${(p) => p.theme.radius.sm};
  background: ${(p) => p.theme.color.surfaceRaised};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.25);
  color: ${(p) => p.theme.color.text};
  font-size: ${(p) => p.theme.font.size.sm};
  font-weight: ${(p) => p.theme.font.weight.regular};
  white-space: nowrap;
  pointer-events: none;
`;

// region Component
interface TooltipProps {
  label: string;
  /** A single focusable element; it gets aria-describedby while the tip shows. */
  children: ReactElement<{ "aria-describedby"?: string }>;
  /** "end" aligns the tip's right edge with the trigger, for triggers at the window edge. */
  placement?: "center" | "end";
  stylesOverride?: SerializedStyles;
}

/** App tooltip shown on hover and focus, hidden on press. Native title tooltips are not used. */
export function Tooltip({ label, children, placement = "center", stylesOverride }: TooltipProps) {
  const [open, setOpen] = useState(false);
  // A press hides the tip until the pointer leaves, so the focus that follows a click does not reopen it.
  const pressed = useRef(false);
  const id = useId();
  return (
    <Wrap
      stylesOverride={stylesOverride}
      onMouseEnter={() => setOpen(!pressed.current)}
      onMouseLeave={() => {
        pressed.current = false;
        setOpen(false);
      }}
      onFocus={() => setOpen(!pressed.current)}
      onBlur={() => setOpen(false)}
      onPointerDown={() => {
        pressed.current = true;
        setOpen(false);
      }}
    >
      {cloneElement(children, { "aria-describedby": open ? id : undefined })}
      {open ? (
        <Tip role="tooltip" id={id} $placement={placement}>
          {label}
        </Tip>
      ) : null}
    </Wrap>
  );
}
