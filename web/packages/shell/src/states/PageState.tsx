import { keyframes, type SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Copy } from "lucide-react";
import type { ReactNode } from "react";
import { HealthIcon } from "../health/HealthIcon";

// region Styles
const spin = keyframes`to { transform: rotate(360deg) }`;
const Center = styled.div<{ stylesOverride?: SerializedStyles }>`
  min-height: 60vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: ${(p) => p.theme.space(3)};
  padding: ${(p) => p.theme.space(4)};
  text-align: center;
  color: ${(p) => p.theme.color.textMuted};
  h2 {
    color: ${(p) => p.theme.color.text};
    font-size: ${(p) => p.theme.font.size.lg};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    margin: 0;
  }
  p {
    max-width: 560px;
    margin: 0;
    line-height: 1.5;
  }
  code {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
    color: ${(p) => p.theme.color.text};
  }
  ${(p) => p.stylesOverride}
`;
const Spinner = styled.div`
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 3px solid ${(p) => p.theme.color.border};
  border-top-color: ${(p) => p.theme.color.accentText};
  animation: ${spin} 0.9s linear infinite;
`;
const CopyButton = styled.button`
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  cursor: pointer;
  font: inherit;
  font-size: ${(p) => p.theme.font.size.sm};
  border-radius: ${(p) => p.theme.radius.sm};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  background: ${(p) => p.theme.color.surface};
  color: ${(p) => p.theme.color.text};
  &:hover {
    border-color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.surface});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
`;

// region Component
interface PageStateProps {
  kind: "loading" | "connecting" | "error" | "empty";
  title: string;
  message?: ReactNode;
  copyText?: string;
  stylesOverride?: SerializedStyles;
}

/** Designed full-page loading, connecting, error and empty states. */
export function PageState({ kind, title, message, copyText, stylesOverride }: PageStateProps) {
  return (
    <Center role={kind === "error" ? "alert" : "status"} stylesOverride={stylesOverride}>
      {kind === "loading" || kind === "connecting" ? <Spinner aria-hidden="true" /> : null}
      {kind === "error" ? <HealthIcon state="error" size={28} /> : null}
      {kind === "empty" ? <HealthIcon state="unknown" size={28} /> : null}
      <h2>{title}</h2>
      {message ? <p>{message}</p> : null}
      {copyText ? (
        <CopyButton type="button" onClick={() => void navigator.clipboard?.writeText(copyText).catch(() => {})}>
          <Copy size={14} aria-hidden="true" /> Copy error
        </CopyButton>
      ) : null}
    </Center>
  );
}
