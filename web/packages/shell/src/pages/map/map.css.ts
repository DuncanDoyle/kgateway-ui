import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";

// region Styles
/** The map's height, shared by the loading fallback so nothing moves when the canvas arrives. */
const MAP_H = "clamp(420px, 70vh, 760px)";

/** The map's box: fixed height, themed border. */
export const MapBox = styled.div<{ stylesOverride?: SerializedStyles }>`
  position: relative;
  height: ${MAP_H};
  box-sizing: border-box;
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.bg};
  overflow: hidden;
  ${(p) => p.stylesOverride}
`;

/** React Flow's base styles take their colours from theme tokens through its --xy-* variables (design §6.2). */
export const MapCanvas = styled(MapBox)`
  .react-flow {
    --xy-background-color: ${(p) => p.theme.color.bg};
    --xy-node-color: ${(p) => p.theme.color.text};
    --xy-node-border: 1px solid ${(p) => p.theme.color.borderStrong};
    --xy-node-border-selected: 2px solid ${(p) => p.theme.color.accentText};
    --xy-edge-stroke: ${(p) => p.theme.color.textMuted};
    --xy-edge-stroke-selected: ${(p) => p.theme.color.accentText};
    --xy-edge-stroke-width: 1.5;
    --xy-handle-background-color: ${(p) => p.theme.color.borderStrong};
    --xy-minimap-background-color: ${(p) => p.theme.color.surface};
    --xy-minimap-mask-background-color: color-mix(in srgb, ${(p) => p.theme.color.bg} 60%, transparent);
    --xy-minimap-mask-stroke-color: ${(p) => p.theme.color.borderStrong};
    --xy-minimap-node-background-color: ${(p) => p.theme.color.surfaceRaised};
    --xy-minimap-node-stroke-color: ${(p) => p.theme.color.borderStrong};
    --xy-attribution-background-color: transparent;
    --xy-selection-background-color: color-mix(in srgb, ${(p) => p.theme.color.accent} 12%, transparent);
    --xy-selection-border: 1px dotted ${(p) => p.theme.color.accentText};
    font-family: ${(p) => p.theme.font.ui};
  }
  .react-flow__node:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
  .react-flow__handle {
    opacity: 0;
  }
  .react-flow__minimap {
    border: 1px solid ${(p) => p.theme.color.border};
    border-radius: ${(p) => p.theme.radius.sm};
  }
  .react-flow__controls {
    border: 1px solid ${(p) => p.theme.color.borderStrong};
    border-radius: ${(p) => p.theme.radius.sm};
    overflow: hidden;
  }
  .react-flow__controls-button {
    width: 28px;
    height: 28px;
    background: ${(p) => p.theme.color.surface};
    color: ${(p) => p.theme.color.text};
    border-bottom: 1px solid ${(p) => p.theme.color.border};
    cursor: pointer;
    svg {
      fill: currentColor;
    }
    &:hover {
      background: color-mix(in srgb, ${(p) => p.theme.color.text} 8%, ${(p) => p.theme.color.surface});
    }
    &:active {
      background: color-mix(in srgb, ${(p) => p.theme.color.text} 16%, ${(p) => p.theme.color.surface});
    }
    &:focus-visible {
      outline: 2px solid ${(p) => p.theme.color.focus};
      outline-offset: -2px;
    }
    &:last-of-type {
      border-bottom: none;
    }
  }
  .react-flow__attribution a {
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.xs};
    &:hover {
      color: ${(p) => p.theme.color.text};
    }
    &:active {
      color: ${(p) => p.theme.color.accentText};
    }
  }
`;
