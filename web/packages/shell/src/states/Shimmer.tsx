import { keyframes, type SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";

// region Styles
const pulse = keyframes`from { opacity: .45 } to { opacity: .9 }`;
const Bar = styled.div<{ $h: number; $stylesOverride?: SerializedStyles }>`
  height: ${(p) => p.$h}px;
  border-radius: ${(p) => p.theme.radius.sm};
  background: ${(p) => p.theme.color.surfaceRaised};
  animation: ${pulse} 0.9s ease-in-out infinite alternate;
  ${(p) => p.$stylesOverride}
`;

// region Component
interface ShimmerProps {
  /** Height in px; match the final layout so nothing shifts when data arrives. */
  h?: number;
  stylesOverride?: SerializedStyles;
}

/** Loading placeholder with the final layout's height. */
export function Shimmer({ h = 16, stylesOverride }: ShimmerProps) {
  return <Bar $h={h} $stylesOverride={stylesOverride} aria-hidden="true" data-testid="shimmer" />;
}
