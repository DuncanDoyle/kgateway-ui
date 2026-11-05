import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Check, Copy } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { NextStep } from "../api/types";
import { diagnosticsCopy, findingsCopy } from "../copy/explorer";
import { IconButton } from "../resource/CopyNameButton";
import { layout } from "../shell/layout";
import { Tooltip } from "../shell/Tooltip";
import { VisuallyHidden } from "../states/VisuallyHidden";

// region Styles
const Steps = styled.ol<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  /* minmax(0, …): a long command scrolls in its block instead of widening the list. */
  grid-template-columns: minmax(0, 1fr);
  gap: ${(p) => p.theme.space(3)};
  margin: 0;
  padding-left: ${(p) => p.theme.space(5)};
  max-width: ${layout.proseMax};
  .title {
    margin-bottom: ${(p) => p.theme.space(1)};
  }
  .block {
    display: flex;
    align-items: flex-start;
    gap: ${(p) => p.theme.space(1)};
  }
  pre {
    flex: 1;
    min-width: 0;
    margin: 0;
    padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
    overflow-x: auto;
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.sm};
    color: ${(p) => p.theme.color.text};
    background: ${(p) => p.theme.color.surfaceRaised};
    border: 1px solid ${(p) => p.theme.color.border};
    border-radius: ${(p) => p.theme.radius.sm};
  }
  .note {
    margin-top: ${(p) => p.theme.space(1)};
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
  }
  ${(p) => p.stylesOverride}
`;

// region Helpers
/** How long the copied tick and announcement stay. */
const COPIED_MS = 3000;

// region Component
interface NextStepsProps {
  steps: NextStep[];
  stylesOverride?: SerializedStyles;
}

/** Numbered commands to learn more, each with a copy button; commands are text, never HTML (design §17.2). */
export function NextSteps({ steps, stylesOverride }: NextStepsProps) {
  const [copied, setCopied] = useState<{ index: number; message: string }>();
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const copy = (index: number, text: string) => {
    // Clearing first makes a repeated copy announce again.
    setCopied(undefined);
    clearTimeout(timer.current);
    const done = (message: string) => {
      setCopied({ index, message });
      timer.current = setTimeout(() => setCopied(undefined), COPIED_MS);
    };
    const write = navigator.clipboard?.writeText(text) ?? Promise.reject(new Error("no clipboard"));
    write.then(
      () => done(diagnosticsCopy.copied),
      () => done(diagnosticsCopy.copyFailed),
    );
  };
  return (
    <>
      <Steps stylesOverride={stylesOverride}>
        {steps.map((s, k) => {
          const ok = copied?.index === k && copied.message === diagnosticsCopy.copied;
          const Icon = ok ? Check : Copy;
          return (
            <li key={`${s.title}|${s.command}`}>
              <div className="title">{s.title}</div>
              <div className="block">
                <pre>{s.command}</pre>
                <Tooltip label={findingsCopy.copy} placement="end">
                  <IconButton
                    type="button"
                    aria-label={findingsCopy.copyLabel(s.title)}
                    onClick={() => copy(k, s.command)}
                  >
                    <Icon size={14} aria-hidden="true" />
                  </IconButton>
                </Tooltip>
              </div>
              {s.needs?.length ? <div className="note">{findingsCopy.needs(s.needs)}</div> : null}
              {s.note ? <div className="note">{s.note}</div> : null}
            </li>
          );
        })}
      </Steps>
      <VisuallyHidden role="status" aria-live="polite">
        {copied?.message ?? ""}
      </VisuallyHidden>
    </>
  );
}
