import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { cloneElement, type ReactElement, useId, useRef, useState } from "react";

type Placement = "start" | "center" | "end";

// region Styles
const Wrap = styled.span<{ stylesOverride?: SerializedStyles }>`
  position: relative;
  display: inline-flex;
  ${(p) => p.stylesOverride}
`;
const Tip = styled.span<{ $placement: Placement; $wrap?: boolean }>`
  position: absolute;
  top: calc(100% + 6px);
  ${(p) => ({ start: "left: 0;", end: "right: 0;", center: "left: 50%; transform: translateX(-50%);" })[p.$placement]}
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
  ${(p) => (p.$wrap ? "white-space: normal; width: max-content; max-width: min(32rem, 80vw); overflow-wrap: anywhere;" : "")}
`;

// region Helpers
// Whether the last input was a pointer press: focus that follows a click (a menu handing focus back) shows no tip.
let pointerInput = false;
if (typeof document !== "undefined") {
  document.addEventListener(
    "pointerdown",
    () => {
      pointerInput = true;
    },
    true,
  );
  document.addEventListener(
    "keydown",
    () => {
      pointerInput = false;
    },
    true,
  );
}

// region Component
interface TooltipProps {
  label: string;
  /** A single focusable element; it gets aria-describedby while the tip shows. */
  children: ReactElement<{ "aria-describedby"?: string }>;
  /** "start" and "end" align the tip's left or right edge with the trigger, for triggers near a window edge. */
  placement?: Placement;
  /** Long labels wrap inside a bounded width instead of one line. */
  wrap?: boolean;
  stylesOverride?: SerializedStyles;
}

/** App tooltip shown on hover and keyboard focus, hidden on press. Native title tooltips are not used. */
export function Tooltip({ label, children, placement = "center", wrap, stylesOverride }: TooltipProps) {
  const [open, setOpen] = useState(false);
  // A press hides the tip until the pointer leaves.
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
      onFocus={() => setOpen(!pointerInput)}
      onBlur={() => setOpen(false)}
      onPointerDown={() => {
        pressed.current = true;
        setOpen(false);
      }}
    >
      {cloneElement(children, { "aria-describedby": open ? id : undefined })}
      {open ? (
        <Tip role="tooltip" id={id} $placement={placement} $wrap={wrap}>
          {label}
        </Tip>
      ) : null}
    </Wrap>
  );
}
