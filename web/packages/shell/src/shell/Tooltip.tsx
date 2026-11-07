import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { cloneElement, type ReactElement, useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

type Placement = "start" | "center" | "end";
type Side = "below" | "above";
interface Spot {
  top: number;
  left: number;
  width: number;
  height: number;
  side: Side;
}
/** Gap between trigger and tip, in px. */
const GAP = 6;

// region Styles
const Wrap = styled.span<{ stylesOverride?: SerializedStyles }>`
  position: relative;
  display: inline-flex;
  ${(p) => p.stylesOverride}
`;
// A fixed box over the trigger, in a portal, so no overflow container clips the tip.
const Anchor = styled.span`
  position: fixed;
  z-index: 1000;
  pointer-events: none;
`;
const Tip = styled.span<{ $placement: Placement; $wrap?: boolean; $side: Side }>`
  position: absolute;
  ${(p) => (p.$side === "above" ? `bottom: calc(100% + ${GAP}px);` : `top: calc(100% + ${GAP}px);`)}
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
  const wrapRef = useRef<HTMLSpanElement>(null);
  const tipRef = useRef<HTMLSpanElement>(null);
  const [spot, setSpot] = useState<Spot | null>(null);
  useLayoutEffect(() => {
    if (!open) return setSpot(null);
    // Below the trigger, or above it when only there does the tip fit.
    const place = () => {
      const r = wrapRef.current?.getBoundingClientRect();
      if (!r) return;
      const h = tipRef.current?.offsetHeight ?? 0;
      const above = r.bottom + GAP + h > window.innerHeight && r.top - GAP - h >= 0;
      setSpot({ top: r.top, left: r.left, width: r.width, height: r.height, side: above ? "above" : "below" });
    };
    place();
    window.addEventListener("scroll", place, true);
    window.addEventListener("resize", place);
    return () => {
      window.removeEventListener("scroll", place, true);
      window.removeEventListener("resize", place);
    };
  }, [open]);
  return (
    <Wrap
      ref={wrapRef}
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
      {open
        ? createPortal(
            <Anchor
              style={
                spot
                  ? { top: spot.top, left: spot.left, width: spot.width, height: spot.height }
                  : { top: 0, left: 0, visibility: "hidden" }
              }
            >
              <Tip
                ref={tipRef}
                role="tooltip"
                id={id}
                $placement={placement}
                $wrap={wrap}
                $side={spot?.side ?? "below"}
                data-side={spot?.side ?? "below"}
              >
                {label}
              </Tip>
            </Anchor>,
            document.body,
          )
        : null}
    </Wrap>
  );
}
